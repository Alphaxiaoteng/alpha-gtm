import { useEffect, useMemo, useState } from 'react';
import useSWR from 'swr';
import {
  FilePlus2,
  Film,
  FolderOpen,
  Save,
  Scissors,
  Sparkles,
  Trash2,
  Video,
} from 'lucide-react';
import { api, apiErrorText, type ClipJob, type KnowledgeSearchResult, type MediaLibraryItem, type StudioScript } from '../api';
import { useUIStore } from '../store';

type ContentFamily = StudioScript['content_type'];
type MediaPurpose = MediaLibraryItem['purpose'];
type LibraryFilter = 'all' | MediaPurpose;
type ScriptFilter = 'all' | ContentFamily;
type ScriptDraft = Pick<StudioScript, 'title' | 'body' | 'content_type'>;
type StudioSection = 'media' | 'scripts' | 'clip';

const emptyScript = (contentType: ContentFamily = 'video'): ScriptDraft => ({ title: '', body: '', content_type: contentType });
const contentFamilyLabels: Record<ContentFamily, string> = { video: '视频', graph_text: '图文', article: '文章' };
const mediaPurposeLabels: Record<MediaPurpose, string> = { shared: '未分类', video: '视频', graph_text: '图文', article: '文章' };
const userMediaPurposes = ['video', 'graph_text', 'article'] as const;
const studioSections: Array<{ id: StudioSection; label: string }> = [
  { id: 'media', label: '素材' },
  { id: 'scripts', label: '脚本' },
  { id: 'clip', label: '视频粗剪' },
];

function fileName(path: string) {
  const segments = path.split('/');
  return segments[segments.length - 1] || path;
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

function formatDuration(value: number) {
  if (value <= 0) return '时长未探测';
  const total = Math.round(value / 1000);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`;
}

function statusTone(status: ClipJob['status']) {
  if (status === 'completed') return 'border-emerald-400/30 bg-emerald-500/10 text-emerald-700';
  if (status === 'failed') return 'border-red-400/30 bg-red-500/10 text-red-700';
  return 'border-brand-border bg-brand-surface-raised text-brand-muted';
}

function planPreview(raw: string): { title: string; reason: string } | null {
  try {
    const value = JSON.parse(raw) as { title?: unknown; reason?: unknown };
    if (typeof value.title !== 'string' || typeof value.reason !== 'string') return null;
    return { title: value.title, reason: value.reason };
  } catch {
    return null;
  }
}

export function StudioView() {
  const { toast } = useUIStore();
  const { data: media, error: mediaError, mutate: mutateMedia } = useSWR('api/media/library', api.getMediaLibrary);
  const { data: scripts, error: scriptsError, mutate: mutateScripts } = useSWR('api/scripts', api.getStudioScripts);
  const { data: jobs, error: jobsError, mutate: mutateJobs } = useSWR('api/clip-jobs', api.getClipJobs);
  const { data: knowledgeBases = [] } = useSWR('api/knowledge/bases', api.getKnowledgeBases);
  const [section, setSection] = useState<StudioSection>('media');
  const [mediaPath, setMediaPath] = useState('');
  const [mediaPurpose, setMediaPurpose] = useState<MediaPurpose>('video');
  const [mediaFilter, setMediaFilter] = useState<LibraryFilter>('all');
  const [scriptFilter, setScriptFilter] = useState<ScriptFilter>('all');
  const [selectedMediaID, setSelectedMediaID] = useState<number | null>(null);
  const [selectedScriptID, setSelectedScriptID] = useState<number | null>(null);
  const [scriptDraft, setScriptDraft] = useState<ScriptDraft>(emptyScript);
  const [targetDurationMS, setTargetDurationMS] = useState(60_000);
  const [importing, setImporting] = useState(false);
  const [savingScript, setSavingScript] = useState(false);
  const [planning, setPlanning] = useState(false);
  const [executingID, setExecutingID] = useState<number | null>(null);
  const [knowledgeBaseID, setKnowledgeBaseID] = useState(0);
  const [knowledgeQuery, setKnowledgeQuery] = useState('');
  const [knowledgeResults, setKnowledgeResults] = useState<KnowledgeSearchResult[]>([]);

  const videoMedia = useMemo(() => (media ?? []).filter((item) => item.mime.toLowerCase().startsWith('video/')), [media]);
  const visibleMedia = useMemo(() => (media ?? []).filter((item) => mediaFilter === 'all' || item.purpose === mediaFilter), [media, mediaFilter]);
  const visibleScripts = useMemo(() => (scripts ?? []).filter((item) => scriptFilter === 'all' || item.content_type === scriptFilter), [scriptFilter, scripts]);
  const videoScripts = useMemo(() => (scripts ?? []).filter((item) => item.content_type === 'video'), [scripts]);
  const selectedScript = useMemo(() => scripts?.find((item) => item.id === selectedScriptID) ?? null, [scripts, selectedScriptID]);
  const scriptDirty = scriptDraft.title !== (selectedScript?.title ?? '') || scriptDraft.body !== (selectedScript?.body ?? '') || scriptDraft.content_type !== (selectedScript?.content_type ?? 'video');

  useEffect(() => {
    if (selectedScript) setScriptDraft({ title: selectedScript.title, body: selectedScript.body, content_type: selectedScript.content_type });
    if (!selectedScript && selectedScriptID !== null) setSelectedScriptID(null);
  }, [selectedScript, selectedScriptID]);

  useEffect(() => {
    if (selectedMediaID !== null && !videoMedia.some((item) => item.id === selectedMediaID)) setSelectedMediaID(null);
  }, [selectedMediaID, videoMedia]);

  const pickMedia = async () => {
    try {
      if (!window.electronAPI?.selectMediaFile) {
        toast.show('请填写本地素材绝对路径后加入素材库', 'info');
        return;
      }
      const selected = await window.electronAPI?.selectMediaFile?.();
      if (selected) setMediaPath(selected);
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, '选择文件失败'), 'error');
    }
  };

  const importMedia = async () => {
    const path = mediaPath.trim();
    if (!path) {
      toast.show('先选择或填写本地素材路径', 'error');
      return;
    }
    setImporting(true);
    try {
      const created = await api.importMediaLibrary(path, mediaPurpose);
      setMediaPath('');
      await mutateMedia();
      // The media-list effect clears selections not present in its current
      // list. Refresh first so a newly imported video is never dropped.
      setSelectedMediaID(created.mime.toLowerCase().startsWith('video/') ? created.id : null);
      toast.show('素材已添加，原文件仍在电脑原位置', 'success');
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, '导入素材失败'), 'error');
    } finally {
      setImporting(false);
    }
  };

  const updateMediaPurpose = async (item: MediaLibraryItem, purpose: MediaPurpose) => {
    try {
      await api.updateMediaLibraryPurpose(item.id, purpose);
      await mutateMedia();
      toast.show(`素材已归类为${mediaPurposeLabels[purpose]}`, 'success');
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, '素材归类失败'), 'error');
    }
  };

  const removeMedia = async (item: { id: number; file_path: string }) => {
    if (!window.confirm(`从素材库移除“${fileName(item.file_path)}”？不会删除电脑上的原文件；已有剪辑方案引用时会被安全拒绝。`)) return;
    try {
      await api.deleteMediaLibrary(item.id);
      if (selectedMediaID === item.id) setSelectedMediaID(null);
      await mutateMedia();
      toast.show('已从素材库移除，电脑上的原文件未删除', 'success');
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, '删除素材索引失败'), 'error');
    }
  };

  const clearScriptDraft = (contentType: ContentFamily = scriptFilter === 'all' ? 'video' : scriptFilter) => {
    setSelectedScriptID(null);
    setScriptDraft(emptyScript(contentType));
  };

  const confirmDiscardScriptDraft = () =>
    !scriptDirty || window.confirm('有未保存的脚本修改。要放弃这些本地修改吗？');

  const selectNewScript = () => {
    if (!confirmDiscardScriptDraft()) return;
    clearScriptDraft();
  };

  const selectScript = (nextID: number | null) => {
    if (nextID === selectedScriptID || !confirmDiscardScriptDraft()) return;
    setSelectedScriptID(nextID);
  };

  const saveScript = async () => {
    setSavingScript(true);
    try {
      const saved = selectedScriptID === null
        ? await api.createStudioScript(scriptDraft)
        : await api.updateStudioScript(selectedScriptID, scriptDraft);
      await mutateScripts();
      setSelectedScriptID(saved.id);
      toast.show(selectedScriptID === null ? '脚本已存入本地' : '脚本已保存', 'success');
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, '保存脚本失败'), 'error');
    } finally {
      setSavingScript(false);
    }
  };

  const searchScriptKnowledge = async () => {
    const query = knowledgeQuery.trim() || scriptDraft.title.trim();
    if (!query) {
      toast.show('输入脚本主题或先填写标题', 'info');
      return;
    }
    try {
      const response = await api.searchKnowledge({ base_ids: knowledgeBaseID ? [knowledgeBaseID] : undefined, query, top_k: 6 });
      setKnowledgeResults(response.results);
      if (response.results.length === 0) toast.show('知识库没有匹配片段', 'info');
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, '知识库检索失败'), 'error');
    }
  };

  const insertKnowledgeExcerpt = (result: KnowledgeSearchResult) => {
    const citation = `[KB:${result.chunk_id}] ${result.source_name}${result.heading_path ? ` / ${result.heading_path}` : ''}`;
    setScriptDraft((current) => ({
      ...current,
      body: `${current.body.trim()}${current.body.trim() ? '\n\n' : ''}${citation}\n${result.content}`.slice(0, 32768),
    }));
  };

  const removeScript = async () => {
    if (selectedScriptID === null || !window.confirm('删除此脚本？已有剪辑方案引用时会被安全拒绝。')) return;
    try {
      await api.deleteStudioScript(selectedScriptID);
      await mutateScripts();
      clearScriptDraft();
      toast.show('脚本已删除', 'success');
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, '删除脚本失败'), 'error');
    }
  };

  const createPlan = async () => {
    if (!selectedMediaID || !selectedScriptID || selectedScript?.content_type !== 'video') {
      toast.show('选择一个视频素材和已保存脚本', 'error');
      return;
    }
    if (!window.confirm('将把所选素材的元数据和脚本文本发送给固定豆包 API 生成剪辑方案；不会执行剪辑。继续吗？')) return;
    setPlanning(true);
    try {
      const job = await api.planClip({ sourceMediaId: selectedMediaID, scriptId: selectedScriptID, targetDurationMs: targetDurationMS });
      await mutateJobs();
      toast.show(`已生成可审阅建议 #${job.id}，尚未生成剪辑文件`, 'success');
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, '未能生成剪辑建议'), 'error');
    } finally {
      setPlanning(false);
    }
  };

  const execute = async (job: ClipJob) => {
    setExecutingID(job.id);
    try {
      await api.executeClip(job.id);
      await Promise.all([mutateJobs(), mutateMedia()]);
      toast.show('剪辑已在本机生成并加入素材库', 'success');
    } catch (reason: unknown) {
      await mutateJobs();
      toast.show(apiErrorText(reason, '本地剪辑失败'), 'error');
    } finally {
      setExecutingID(null);
    }
  };

  const revealOutput = async (outputPath: string) => {
    try {
      const opened = await window.electronAPI?.openPathInFinder?.(outputPath);
      toast.show(opened ? '已在 Finder 中定位剪辑产物' : '产物不存在或当前不是桌面端', opened ? 'success' : 'error');
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, '无法定位剪辑产物'), 'error');
    }
  };

  return (
    <section className="mx-auto max-w-7xl space-y-5">
      <header>
        <h3 className="ui-page-title">素材与脚本</h3>
        <p className="ui-page-sub">统一管理视频、图片和文章附件，文件仍保留在电脑原位置。</p>
      </header>

      <div role="tablist" aria-label="素材与脚本" className="inline-flex gap-1 rounded-lg border border-brand-border bg-brand-surface p-1">
        {studioSections.map(({ id, label }) => <button key={id} id={`studio-tab-${id}`} type="button" role="tab" data-testid={`studio-section-${id}`} aria-selected={section === id} aria-controls={`studio-panel-${id}`} onClick={() => setSection(id)} className={`rounded-md px-4 py-2 text-xs font-semibold ${section === id ? 'bg-brand-accent text-brand-on-accent' : 'text-gray-600 hover:bg-black/[0.04] hover:text-gray-900'}`}>{label}</button>)}
      </div>

      <div role="tabpanel" id={`studio-panel-${section}`} aria-labelledby={`studio-tab-${section}`}>
        {section === 'media' ? <article className="ui-card overflow-hidden">
          <div className="border-b border-brand-border p-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div><div className="flex items-center gap-2"><Video className="h-4 w-4 text-brand-accent" /><h4 className="text-sm font-bold text-gray-900">素材库</h4></div><p className="mt-1 text-xs text-gray-500">配置本机绝对路径后直接索引；发布时由后台从冻结路径静默上传，不打开平台文件选择器。</p></div>
              <button type="button" onClick={pickMedia} className="ui-btn ui-btn-primary" data-testid="studio-pick-media"><FolderOpen className="h-3.5 w-3.5" />添加素材</button>
            </div>
            <div className="mt-4 flex flex-wrap gap-1" role="group" aria-label="素材分类筛选">
              {(['all', ...userMediaPurposes] as const).map((value) => <button key={value} type="button" data-testid={`studio-media-filter-${value}`} aria-pressed={mediaFilter === value} onClick={() => { setMediaFilter(value); if (value !== 'all') setMediaPurpose(value); }} className={`rounded border px-3 py-1.5 text-xs ${mediaFilter === value ? 'border-brand-accent bg-brand-accent/10 text-brand-accent' : 'border-brand-border text-gray-500'}`}>{value === 'all' ? '全部' : mediaPurposeLabels[value]}</button>)}
            </div>
            <div className="mt-4 rounded-lg border border-brand-border bg-black/[0.02] p-3" data-testid="studio-direct-media-path">
              <label className="block text-xs font-semibold text-gray-600">本地素材绝对路径
                <input value={mediaPath} onChange={(event) => setMediaPath(event.target.value)} className="mt-1.5 w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 font-mono text-xs text-gray-900" placeholder="/Users/.../cover.png 或 video.mp4" aria-label="本地素材路径" data-testid="studio-media-path" />
              </label>
              <p className="mt-1.5 text-[11px] text-gray-500">仅索引并校验文件；不会复制原文件。素材目录可在“设置”中配置。</p>
            </div>
            {mediaPath ? <div className="mt-3 rounded-lg border border-brand-border bg-black/[0.02] p-3" data-testid="studio-pending-media">
              <p className="truncate text-xs font-semibold text-gray-900">{fileName(mediaPath)}</p>
              <div className="mt-3 flex flex-wrap items-end gap-2">
                <label className="min-w-36 flex-1 text-xs font-semibold text-gray-600">归入
                  <select value={mediaPurpose} onChange={(event) => setMediaPurpose(event.target.value as MediaPurpose)} className="mt-1 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-xs" data-testid="studio-media-purpose">
                    {userMediaPurposes.map((value) => <option key={value} value={value}>{mediaPurposeLabels[value]}</option>)}
                  </select>
                </label>
                <button type="button" disabled={importing} onClick={importMedia} className="ui-btn ui-btn-primary disabled:opacity-50" data-testid="studio-import-media">{importing ? '添加中…' : '加入素材库'}</button>
              </div>
            </div> : null}
          </div>
          {mediaError ? <p className="p-4 text-xs text-red-600">素材库加载失败：{apiErrorText(mediaError, '未知错误')}</p> : null}
          <div className="max-h-[520px] divide-y divide-brand-border overflow-y-auto">
            {visibleMedia.map((item) => {
              const isVideo = item.mime.toLowerCase().startsWith('video/');
              const kind = isVideo ? '视频' : item.mime.toLowerCase().startsWith('image/') ? '图片' : '文件';
              return <div key={item.id} className="flex items-center gap-2 px-3 py-3 hover:bg-black/[0.03]">
                <Film className={`h-4 w-4 shrink-0 ${isVideo ? 'text-brand-accent' : 'text-gray-400'}`} />
                <div className="min-w-0 flex-1"><p className="truncate text-xs font-semibold text-gray-900">{fileName(item.file_path)}</p><p className="mt-1 truncate text-[11px] text-gray-500">{kind}{isVideo ? ` · ${formatDuration(item.duration_ms)}` : ''} · {formatBytes(item.size_bytes)}</p></div>
                <select aria-label={`${fileName(item.file_path)}分类`} value={item.purpose} onChange={(event) => void updateMediaPurpose(item, event.target.value as MediaPurpose)} className="w-24 rounded border border-brand-border bg-transparent px-2 py-1.5 text-xs" data-testid={`studio-media-purpose-${item.id}`}>
                  {item.purpose === 'shared' ? <option value="shared">未分类</option> : null}
                  {userMediaPurposes.map((value) => <option key={value} value={value}>{mediaPurposeLabels[value]}</option>)}
                </select>
                <button type="button" onClick={() => removeMedia(item)} className="ui-btn shrink-0 text-red-600" title="从素材库移除，不删除原文件" aria-label={`从素材库移除 ${fileName(item.file_path)}`}><Trash2 className="h-3.5 w-3.5" /></button>
              </div>;
            })}
            {visibleMedia.length === 0 ? <p className="p-10 text-center text-xs text-gray-400">当前分类暂无素材。</p> : null}
          </div>
        </article> : section === 'scripts' ? <article className="ui-card flex min-h-[540px] flex-col overflow-hidden">
          <div className="flex items-center justify-between border-b border-brand-border p-4">
            <div><div className="flex items-center gap-2"><FilePlus2 className="h-4 w-4 text-brand-accent" /><h4 className="text-sm font-bold text-gray-900">脚本库</h4>{scriptDirty ? <span className="rounded border border-brand-warning/30/30 bg-brand-warning/20/10 px-1.5 py-0.5 text-[10px] font-semibold text-brand-warning">未保存</span> : null}</div><p className="mt-1 text-xs text-gray-500">保存常用文案、旁白和镜头安排，之后可以继续编辑。</p></div>
            <button type="button" onClick={selectNewScript} className="ui-btn" title="新建脚本"><FilePlus2 className="h-3.5 w-3.5" />新建</button>
          </div>
          <div className="flex min-h-0 flex-1 flex-col p-4">
            {scriptsError ? <p className="mb-3 text-xs text-red-600">脚本加载失败：{apiErrorText(scriptsError, '未知错误')}</p> : null}
            <div className="mb-3 flex flex-wrap gap-1" role="group" aria-label="脚本分类筛选">{(['all', 'video', 'graph_text', 'article'] as const).map((value) => <button key={value} type="button" data-testid={`studio-script-filter-${value}`} aria-pressed={scriptFilter === value} onClick={() => { if (confirmDiscardScriptDraft()) { setScriptFilter(value); clearScriptDraft(value === 'all' ? 'video' : value); } }} className={`rounded border px-3 py-1.5 text-xs ${scriptFilter === value ? 'border-brand-accent bg-brand-accent/10 text-brand-accent' : 'border-brand-border text-gray-500'}`}>{value === 'all' ? '全部' : contentFamilyLabels[value]}</button>)}</div>
            <select aria-label="选择已有脚本" value={selectedScriptID ?? ''} onChange={(event) => selectScript(event.target.value ? Number(event.target.value) : null)} className="rounded-lg border border-brand-border bg-transparent px-3 py-2 text-xs">
              <option value="">选择已有脚本</option>
              {visibleScripts.map((item) => <option key={item.id} value={item.id}>{contentFamilyLabels[item.content_type]} · {item.title}</option>)}
            </select>
            <label className="mt-3 text-xs font-semibold text-gray-600">内容类型
              <select value={scriptDraft.content_type} onChange={(event) => setScriptDraft({ ...scriptDraft, content_type: event.target.value as ContentFamily })} className="mt-1 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-xs" data-testid="studio-script-content-type"><option value="video">视频脚本</option><option value="graph_text">图文脚本</option><option value="article">文章脚本</option></select>
            </label>
            <input value={scriptDraft.title} onChange={(event) => setScriptDraft({ ...scriptDraft, title: event.target.value })} className="mt-3 rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm" placeholder="脚本标题" aria-label="脚本标题" maxLength={120} data-testid="studio-script-title" />
            <textarea value={scriptDraft.body} onChange={(event) => setScriptDraft({ ...scriptDraft, body: event.target.value })} className="mt-3 min-h-56 flex-1 resize-y rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm leading-relaxed" placeholder="写下要表达的内容、镜头节奏或旁白…" aria-label="脚本正文" maxLength={32768} data-testid="studio-script-body" />
            <div className="mt-3 rounded-lg border border-brand-border p-3">
              <div className="flex flex-wrap gap-2"><select aria-label="脚本知识库" value={knowledgeBaseID} onChange={(event) => setKnowledgeBaseID(Number(event.target.value))} className="rounded border border-brand-border bg-transparent px-2 py-1.5 text-xs"><option value={0}>全部知识库</option>{knowledgeBases.map((base) => <option key={base.id} value={base.id}>{base.name}</option>)}</select><input value={knowledgeQuery} onChange={(event) => setKnowledgeQuery(event.target.value)} placeholder="脚本主题或关键字" className="min-w-40 flex-1 rounded border border-brand-border bg-transparent px-2 py-1.5 text-xs" /><button type="button" onClick={() => void searchScriptKnowledge()} className="ui-btn">检索知识</button></div>
              {knowledgeResults.length ? <div className="mt-2 space-y-1.5">{knowledgeResults.map((result) => <button key={result.chunk_id} type="button" onClick={() => insertKnowledgeExcerpt(result)} className="block w-full rounded bg-black/[0.03] px-2.5 py-2 text-left text-xs hover:bg-black/[0.06]"><span className="font-semibold text-brand-accent">[KB:{result.chunk_id}] {result.source_name}</span><span className="mt-1 block line-clamp-2 text-gray-600">{result.content}</span></button>)}</div> : null}
            </div>
            <div className="mt-3 flex justify-between gap-2"><button type="button" disabled={selectedScriptID === null} onClick={removeScript} className="ui-btn text-red-600 disabled:opacity-40"><Trash2 className="h-3.5 w-3.5" />删除</button><button type="button" disabled={savingScript} onClick={saveScript} className="ui-btn ui-btn-primary disabled:opacity-50" data-testid="studio-save-script"><Save className="h-3.5 w-3.5" />{savingScript ? '保存中…' : '保存脚本'}</button></div>
          </div>
        </article> : <article className="ui-card overflow-hidden">
          <div className="border-b border-brand-border p-4">
            <div className="flex items-center gap-2"><Scissors className="h-4 w-4 text-brand-accent" /><h4 className="text-sm font-bold text-gray-900">视频粗剪</h4></div>
            <p className="mt-1 text-xs text-gray-500">选择视频和脚本，先生成建议，确认后再生成一个新视频文件。</p>
            <div className="mt-4 grid gap-3 sm:grid-cols-3">
              <label className="text-xs font-semibold text-gray-600">视频<select aria-label="选择视频" value={selectedMediaID ?? ''} onChange={(event) => setSelectedMediaID(event.target.value ? Number(event.target.value) : null)} className="mt-1 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-xs"><option value="">请选择</option>{videoMedia.map((item) => <option key={item.id} value={item.id}>{fileName(item.file_path)}</option>)}</select></label>
              <label className="text-xs font-semibold text-gray-600">脚本<select aria-label="选择视频脚本" value={selectedScript?.content_type === 'video' ? selectedScriptID ?? '' : ''} onChange={(event) => selectScript(event.target.value ? Number(event.target.value) : null)} className="mt-1 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-xs"><option value="">请选择</option>{videoScripts.map((item) => <option key={item.id} value={item.id}>{item.title}</option>)}</select></label>
              <label className="text-xs font-semibold text-gray-600">成片时长<select aria-label="成片时长" value={targetDurationMS} onChange={(event) => setTargetDurationMS(Number(event.target.value))} className="mt-1 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-xs"><option value={30_000}>30 秒以内</option><option value={60_000}>60 秒以内</option><option value={90_000}>90 秒以内</option></select></label>
            </div>
            <button type="button" disabled={planning || !selectedMediaID || !selectedScriptID || selectedScript?.content_type !== 'video'} onClick={createPlan} className="ui-btn ui-btn-primary mt-4 disabled:opacity-50" data-testid="studio-create-clip-plan"><Sparkles className="h-3.5 w-3.5" />{planning ? '正在生成建议…' : '生成剪辑建议'}</button>
            <p className="mt-2 text-xs text-gray-500">这一步只生成建议，不会修改原视频。</p>
          </div>
          {jobsError ? <p className="p-4 text-xs text-red-600">剪辑记录加载失败：{apiErrorText(jobsError, '未知错误')}</p> : null}
          <div className="max-h-[520px] divide-y divide-brand-border overflow-y-auto">
            {jobs?.map((job) => {
              const preview = planPreview(job.plan_json);
              const canExecute = job.status === 'planned' || job.status === 'failed';
              return <div key={job.id} className="p-4">
                <div className="flex items-start justify-between gap-3"><div className="min-w-0"><div className="flex items-center gap-2"><Scissors className="h-3.5 w-3.5 text-brand-accent" /><strong className="truncate text-xs text-gray-900">{preview?.title || `建议 #${job.id}`}</strong></div><p className="mt-1 text-[11px] text-gray-500">{fileName(job.source_path)} · {formatDuration(job.start_ms)}–{formatDuration(job.end_ms)}</p></div><span className={`shrink-0 rounded border px-1.5 py-0.5 text-[10px] font-semibold ${statusTone(job.status)}`}>{job.status === 'planned' ? '待生成' : job.status === 'completed' ? '已完成' : '失败'}</span></div>
                {preview?.reason ? <p className="mt-2 text-xs leading-relaxed text-gray-600">{preview.reason}</p> : null}
                {job.error_code ? <details className="mt-2 text-xs text-red-600"><summary className="cursor-pointer">技术详情</summary><code className="mt-1 block text-[10px]">{job.error_code}</code></details> : null}
                {job.output_path ? <div className="mt-2 flex items-center justify-between gap-2"><p className="min-w-0 truncate text-[11px] text-emerald-700">文件：{fileName(job.output_path)}</p>{window.electronAPI?.openPathInFinder ? <button type="button" onClick={() => void revealOutput(job.output_path)} className="ui-btn shrink-0 text-[11px]" data-testid={`studio-open-output-${job.id}`}><FolderOpen className="h-3.5 w-3.5" />在 Finder 中查看</button> : null}</div> : null}
                {canExecute ? <button type="button" disabled={executingID !== null} onClick={() => execute(job)} className="ui-btn ui-btn-primary mt-3 disabled:opacity-50"><Scissors className="h-3.5 w-3.5" />{executingID === job.id ? '正在生成…' : '生成剪辑文件'}</button> : null}
              </div>;
            })}
            {jobs?.length === 0 ? <p className="p-10 text-center text-xs text-gray-400">还没有剪辑建议。</p> : null}
          </div>
        </article>}
      </div>
    </section>
  );
}
