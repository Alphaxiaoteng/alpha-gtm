import { useEffect, useMemo, useRef, useState } from 'react';
import useSWR from 'swr';
import { ClipboardCopy, Copy, Download, Eye, FilePlus2, Link, Save, Trash2, Upload, X } from 'lucide-react';
import { api, type ContentAssistAction, type ContentItem, type KnowledgeCitation } from '../api';
import { useUIStore } from '../store';

type Draft = Omit<ContentItem, 'id' | 'publish_count'>;

type AssistResult = {
	text: string;
	selectedID: number | null;
	action: ContentAssistAction;
	platform: string;
	draftRevision: string;
	citations: KnowledgeCitation[];
	useKnowledge: boolean;
	knowledgeBaseID: number;
};

const emptyDraft = (): Draft => ({ type: 'article', title: '', caption: '', body: '', tags_json: '[]', status: 'draft' });

function toDraft(item: ContentItem): Draft {
  return { type: item.type, title: item.title, caption: item.caption, body: item.body, tags_json: item.tags_json || '[]', status: item.status };
}

function revisionOf(draft: Draft): string {
	return JSON.stringify([draft.type, draft.title, draft.caption, draft.body, draft.tags_json, draft.status]);
}

export function ContentView() {
  const { toast, searchQuery, setSearchQuery, setActiveTab } = useUIStore();
  const { data: contents, error, mutate } = useSWR('api/contents', api.getContents);
	const { data: platforms, error: platformsError } = useSWR('api/platforms', api.getPlatforms);
	const { data: knowledgeBases = [] } = useSWR('api/knowledge/bases', api.getKnowledgeBases);
  const [selectedID, setSelectedID] = useState<number | null>(null);
	const [typeFilter, setTypeFilter] = useState<'all' | ContentItem['type']>('all');
	const [statusFilter, setStatusFilter] = useState<'all' | ContentItem['status']>('all');
  const selected = useMemo(() => contents?.find((item) => item.id === selectedID) ?? null, [contents, selectedID]);
	const normalizedQuery = searchQuery.trim().toLocaleLowerCase();
	const filteredContents = useMemo(() => (contents ?? []).filter((item) => {
		if (typeFilter !== 'all' && item.type !== typeFilter) return false;
		if (statusFilter !== 'all' && item.status !== statusFilter) return false;
		if (!normalizedQuery) return true;
		return [item.title, item.caption, item.body, item.tags_json].some((value) => value.toLocaleLowerCase().includes(normalizedQuery));
	}), [contents, normalizedQuery, statusFilter, typeFilter]);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [selectedLibraryID, setSelectedLibraryID] = useState(0);
	const [preview, setPreview] = useState(false);
	const [assistAction, setAssistAction] = useState<ContentAssistAction>('title');
	const [assistPlatform, setAssistPlatform] = useState('');
	const [allowExternalAssist, setAllowExternalAssist] = useState(false);
	const [useKnowledge, setUseKnowledge] = useState(false);
	const [knowledgeBaseID, setKnowledgeBaseID] = useState(0);
	const [assistResult, setAssistResult] = useState<AssistResult | null>(null);
  const [assisting, setAssisting] = useState(false);
	const draftRevision = useMemo(() => revisionOf(draft), [draft]);
	const visibleAssistResult = assistResult?.selectedID === selectedID
		&& assistResult.action === assistAction
		&& assistResult.platform === (assistAction === 'platform_adapt' ? assistPlatform : '')
		&& assistResult.useKnowledge === useKnowledge
		&& assistResult.knowledgeBaseID === (useKnowledge ? knowledgeBaseID : 0)
		&& assistResult.draftRevision === draftRevision
		? assistResult
		: null;
	const wordCount = useMemo(() => draft.body.trim() ? draft.body.trim().split(/\s+/).length : 0, [draft.body]);
	const readingMinutes = Math.max(1, Math.ceil(wordCount / 400));
  const importRef = useRef<HTMLInputElement>(null);
  const { data: media, mutate: mutateMedia } = useSWR(selectedID ? `api/contents/${selectedID}/media` : null, () => api.getContentMedia(selectedID!));
  const { data: mediaLibrary } = useSWR('api/media/library', api.getMediaLibrary);
  const libraryCandidates = useMemo(() => (mediaLibrary ?? []).filter((item) => {
    if (item.purpose !== 'shared' && item.purpose !== draft.type) return false;
    const isVideo = item.mime.toLowerCase().startsWith('video/');
    const isImage = item.mime.toLowerCase().startsWith('image/');
    return draft.type === 'video' ? isVideo || isImage : isImage;
  }), [draft.type, mediaLibrary]);

  useEffect(() => {
    if (selected) setDraft(toDraft(selected));
    if (!selected && selectedID !== null) setSelectedID(null);
  }, [selected, selectedID]);

  useEffect(() => {
    if (selectedLibraryID && !libraryCandidates.some((item) => item.id === selectedLibraryID)) setSelectedLibraryID(0);
  }, [libraryCandidates, selectedLibraryID]);

	useEffect(() => {
		if (!platforms?.length || platforms.some((platform) => platform.code === assistPlatform)) return;
		setAssistPlatform(platforms[0].code);
	}, [platforms, assistPlatform]);

  const selectNew = () => {
    setSelectedID(null);
    setDraft(emptyDraft());
		setAssistResult(null);
  };

  const save = async () => {
    try {
      if (selectedID === null) {
        const created = await api.createContent(draft);
        await mutate();
        setSelectedID(created.id);
        toast.show('内容草稿已创建', 'success');
      } else {
        await api.updateContent(selectedID, draft);
        await mutate();
        toast.show('内容已保存', 'success');
      }
    } catch (err) {
      toast.show(err instanceof Error ? `保存失败: ${err.message}` : '保存失败', 'error');
    }
  };

  const clone = async () => {
    if (selectedID === null) return;
    try {
      const created = await api.cloneContent(selectedID);
      await mutate();
      setSelectedID(created.id);
      toast.show('已创建副本', 'success');
    } catch {
      toast.show('复制失败', 'error');
    }
  };

  const remove = async () => {
    if (selectedID === null || !window.confirm('删除此内容及其关联媒体？已有发布任务的内容不能删除。')) return;
    try {
      await api.deleteContent(selectedID);
      await mutate();
      selectNew();
      toast.show('内容已删除', 'success');
    } catch (err) {
      toast.show(err instanceof Error ? `删除失败: ${err.message}` : '删除失败', 'error');
    }
  };

  const attachLibraryMedia = async () => {
    if (selectedID === null || selectedLibraryID <= 0) return;
    try {
      await api.attachContentMediaFromLibrary(selectedID, selectedLibraryID);
      await mutateMedia();
      toast.show('素材库项目已关联到内容', 'success');
    } catch (err) {
      toast.show(err instanceof Error ? `关联失败: ${err.message}` : '关联失败', 'error');
    }
  };

  const detachMedia = async (id: number) => {
    if (!window.confirm('仅解除内容关联，不会删除原始文件。')) return;
    try {
      await api.deleteContentMedia(id);
      await mutateMedia();
      toast.show('媒体关联已解除', 'success');
    } catch (err) {
      toast.show(err instanceof Error ? `解除失败: ${err.message}` : '解除失败', 'error');
    }
  };

  const exportDocument = (format: 'md' | 'html') => {
    const title = draft.title || 'untitled';
    const body = format === 'md'
      ? `# ${title}\n\n${draft.caption ? `${draft.caption}\n\n` : ''}${draft.body}`
      : `<!doctype html><meta charset="utf-8"><title>${escapeHTML(title)}</title><h1>${escapeHTML(title)}</h1>${draft.caption ? `<p>${escapeHTML(draft.caption)}</p>` : ''}<pre>${escapeHTML(draft.body)}</pre>`;
    const blob = new Blob([body], { type: format === 'md' ? 'text/markdown;charset=utf-8' : 'text/html;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a'); link.href = url; link.download = `${safeFileName(title)}.${format}`; link.click(); URL.revokeObjectURL(url);
    toast.show(`已导出 ${format.toUpperCase()} 文件`, 'success');
  };

  const importMarkdown = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) return;
    try {
      const body = await file.text();
      const title = body.match(/^#\s+(.+)$/m)?.[1]?.trim() || file.name.replace(/\.(md|markdown)$/i, '');
      setDraft({ ...draft, title, body });
      toast.show('Markdown 已载入，保存后生效', 'success');
    } catch { toast.show('读取 Markdown 失败', 'error'); }
    event.target.value = '';
  };

	const copyPlainText = async () => {
		const text = [draft.title, draft.caption, draft.body].filter(Boolean).join('\n\n');
		if (!text) return;
		try {
			await navigator.clipboard.writeText(text);
			toast.show('已复制纯文本', 'success');
		} catch {
			toast.show('复制失败，请检查系统剪贴板权限', 'error');
		}
	};

	const runAssist = async () => {
		const platform = assistAction === 'platform_adapt' ? assistPlatform.trim() : '';
		if (assistAction === 'platform_adapt' && !platform) {
			toast.show(platformsError ? '平台列表加载失败，暂时无法适配' : '请选择适配平台', 'error');
			return;
		}
		if (useKnowledge && !knowledgeBaseID) {
			toast.show('请选择知识库', 'error');
			return;
		}
		const requestContext = { selectedID, action: assistAction, platform, draftRevision, useKnowledge, knowledgeBaseID: useKnowledge ? knowledgeBaseID : 0 };
		setAssistResult(null);
		setAssisting(true);
		try {
			const response = await api.assistContent({
				...draft,
				action: assistAction,
				platform,
				allow_external: allowExternalAssist,
				use_knowledge: useKnowledge,
				knowledge_base_ids: useKnowledge ? [knowledgeBaseID] : undefined,
				knowledge_query: [draft.title, draft.caption].filter(Boolean).join(' '),
			});
			setAssistResult({ text: response.result, citations: response.citations ?? [], ...requestContext });
			toast.show(response.external ? '已收到配置模型的建议' : '已生成本地建议（未发送内容）', 'success');
		} catch (err) {
			toast.show(err instanceof Error ? `生成建议失败: ${err.message}` : '生成建议失败', 'error');
		} finally {
			setAssisting(false);
		}
	};

	const applyAssistResult = () => {
		if (!visibleAssistResult || visibleAssistResult.action === 'check') return;
		const result = visibleAssistResult;
		setDraft((current) => {
			if (revisionOf(current) !== result.draftRevision) return current;
			switch (result.action) {
			case 'title':
				return { ...current, title: result.text.replace(/^建议标题：/, '') };
			case 'summary':
				return { ...current, caption: result.text };
			case 'rewrite':
			case 'platform_adapt':
				return { ...current, body: result.text };
			case 'check':
				return current;
			default:
				return current;
			}
		});
	};

  return (
    <section className="flex h-full min-h-[620px] overflow-hidden rounded-xl border border-brand-border bg-brand-surface shadow-sm">
      <aside className="flex w-72 shrink-0 flex-col border-r border-brand-border">
        <div className="flex items-center justify-between border-b border-brand-border p-3">
          <div>
            <h3 className="text-sm font-bold text-gray-900">内容中心</h3>
            <p className="mt-0.5 text-[11px] text-gray-500">文章 · 图文 · 视频</p>
          </div>
          <button type="button" onClick={selectNew} className="rounded-lg bg-brand-accent p-2 text-brand-on-accent" title="新建内容">
            <FilePlus2 className="h-4 w-4" />
          </button>
        </div>
			<div className="border-b border-brand-border p-3 space-y-2">
				<div className="flex items-center gap-2">
					<input value={searchQuery} onChange={(event) => setSearchQuery(event.target.value)} placeholder="搜索标题、文案、正文或标签" className="min-w-0 flex-1 rounded-lg border border-brand-border bg-transparent px-2.5 py-1.5 text-xs" />
					{searchQuery ? <button type="button" onClick={() => setSearchQuery('')} className="rounded p-1 text-gray-500 hover:bg-black/5" title="清除搜索"><X className="h-3.5 w-3.5" /></button> : null}
				</div>
				<div className="grid grid-cols-2 gap-2"><select data-testid="content-type-filter" value={typeFilter} onChange={(event) => setTypeFilter(event.target.value as typeof typeFilter)} className="w-full rounded-lg border border-brand-border bg-transparent px-2 py-1.5 text-xs"><option value="all">全部类型</option><option value="article">文章</option><option value="graph_text">图文</option><option value="video">视频</option></select><select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value as typeof statusFilter)} className="w-full rounded-lg border border-brand-border bg-transparent px-2 py-1.5 text-xs"><option value="all">全部状态</option><option value="draft">草稿</option><option value="published">已发布</option><option value="archived">归档</option></select></div>
				<p className="text-[11px] text-gray-500">{normalizedQuery ? `匹配 ${filteredContents.length} 篇内容` : `${filteredContents.length} 篇内容`}</p>
			</div>
        {error ? <p className="p-3 text-xs text-red-600">内容加载失败</p> : null}
        <div className="min-h-0 flex-1 overflow-y-auto">
			{filteredContents.map((item) => (
            <button key={item.id} type="button" onClick={() => setSelectedID(item.id)} className={`block w-full border-b border-brand-border px-3 py-3 text-left ${item.id === selectedID ? 'bg-brand-accent/10' : 'hover:bg-black/[0.03]'}`}>
              <div className="truncate text-sm font-semibold text-gray-900">{item.title || '未命名内容'}</div>
              <div className="mt-1 flex items-center justify-between text-[11px] text-gray-500"><span>{item.type}</span><span>{item.status}</span></div>
            </button>
          ))}
			{contents?.length === 0 ? <p className="p-4 text-center text-xs text-gray-400">尚无内容，创建第一篇草稿。</p> : null}
			{contents && contents.length > 0 && filteredContents.length === 0 ? <p className="p-4 text-center text-xs text-gray-400">没有匹配的内容。</p> : null}
        </div>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex items-center justify-between border-b border-brand-border px-4 py-3">
			<div className="flex items-center gap-3"><span className="text-xs text-gray-500">{selectedID ? `内容 #${selectedID}` : '新内容'}</span><span className="text-[11px] text-gray-400">{wordCount} 词 · 约 {readingMinutes} 分钟阅读</span></div>
          <div className="flex gap-2">
            <input ref={importRef} type="file" accept=".md,.markdown,text/markdown" onChange={importMarkdown} className="hidden" />
            <button type="button" onClick={() => importRef.current?.click()} className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-2.5 py-1.5 text-xs font-semibold"><Upload className="h-3.5 w-3.5" />导入</button>
            <button type="button" onClick={() => setPreview(!preview)} className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-2.5 py-1.5 text-xs font-semibold"><Eye className="h-3.5 w-3.5" />{preview ? '编辑' : '预览'}</button>
            <button type="button" onClick={() => exportDocument('md')} className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-2.5 py-1.5 text-xs font-semibold"><Download className="h-3.5 w-3.5" />MD</button>
            <button type="button" onClick={() => exportDocument('html')} className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-2.5 py-1.5 text-xs font-semibold"><Download className="h-3.5 w-3.5" />HTML</button>
				<button type="button" disabled={!draft.title && !draft.caption && !draft.body} onClick={copyPlainText} className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-2.5 py-1.5 text-xs font-semibold disabled:opacity-40"><ClipboardCopy className="h-3.5 w-3.5" />纯文本</button>
            <button type="button" disabled={selectedID === null} onClick={clone} className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-2.5 py-1.5 text-xs font-semibold disabled:opacity-40"><Copy className="h-3.5 w-3.5" />副本</button>
            <button type="button" disabled={selectedID === null} onClick={remove} className="inline-flex items-center gap-1 rounded-lg border border-red-200 px-2.5 py-1.5 text-xs font-semibold text-red-600 disabled:opacity-40"><Trash2 className="h-3.5 w-3.5" />删除</button>
            <button type="button" onClick={save} className="inline-flex items-center gap-1 rounded-lg bg-brand-accent px-3 py-1.5 text-xs font-semibold text-brand-on-accent"><Save className="h-3.5 w-3.5" />保存</button>
          </div>
        </div>
        {preview ? <article className="min-h-0 flex-1 overflow-y-auto p-6"><h1 className="text-2xl font-bold text-gray-900">{draft.title || '未命名内容'}</h1>{draft.caption ? <p className="mt-3 text-sm text-gray-600">{draft.caption}</p> : null}<pre className="mt-6 whitespace-pre-wrap break-words font-sans text-sm leading-7 text-gray-800">{draft.body || '暂无正文'}</pre></article> : <div className="grid min-h-0 flex-1 grid-cols-2 gap-4 overflow-y-auto p-4">
          <label className="text-xs font-semibold text-gray-700">类型<select value={draft.type} onChange={(event) => setDraft({ ...draft, type: event.target.value as Draft['type'] })} className="mt-1.5 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm font-normal"><option value="article">文章</option><option value="graph_text">图文</option><option value="video">视频</option></select></label>
          <label className="text-xs font-semibold text-gray-700">状态<select value={draft.status} onChange={(event) => setDraft({ ...draft, status: event.target.value as Draft['status'] })} className="mt-1.5 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm font-normal"><option value="draft">草稿</option><option value="archived">归档</option><option value="published">已发布</option></select></label>
          <label className="col-span-2 text-xs font-semibold text-gray-700">标题<input value={draft.title} onChange={(event) => setDraft({ ...draft, title: event.target.value })} className="mt-1.5 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm font-normal" placeholder="输入标题" /></label>
          <label className="col-span-2 text-xs font-semibold text-gray-700">文案摘要<textarea value={draft.caption} onChange={(event) => setDraft({ ...draft, caption: event.target.value })} className="mt-1.5 block min-h-24 w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm font-normal" placeholder="图文/视频描述" /></label>
          <label className="col-span-2 text-xs font-semibold text-gray-700">正文（Markdown）<textarea value={draft.body} onChange={(event) => setDraft({ ...draft, body: event.target.value })} className="mt-1.5 block min-h-64 w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 font-mono text-sm font-normal" placeholder="文章正文" /></label>
          <label className="col-span-2 text-xs font-semibold text-gray-700">标签 JSON<input value={draft.tags_json} onChange={(event) => setDraft({ ...draft, tags_json: event.target.value })} className="mt-1.5 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 font-mono text-sm font-normal" /></label>
			<div className="col-span-2 rounded-lg border border-brand-border p-3">
				<div className="flex flex-wrap items-center justify-between gap-2"><div><p className="text-xs font-semibold text-gray-700">AI 创作辅助</p><p className="mt-0.5 text-[11px] text-gray-500">可引用本地知识库；外部模型仍需本次明确允许。</p></div><select value={assistAction} onChange={(event) => setAssistAction(event.target.value as ContentAssistAction)} className="rounded border border-brand-border bg-transparent px-2 py-1.5 text-xs"><option value="title">生成标题</option><option value="summary">生成摘要</option><option value="rewrite">改写正文</option><option value="check">内容检查</option><option value="platform_adapt">平台文案适配</option></select></div>
				{assistAction === 'platform_adapt' ? <label className="mt-3 block text-xs font-semibold text-gray-700">适配平台<select value={assistPlatform} onChange={(event) => setAssistPlatform(event.target.value)} disabled={!platforms?.length} className="mt-1.5 block w-full rounded border border-brand-border bg-transparent px-2 py-1.5 font-normal disabled:opacity-50"><option value="">{platformsError ? '平台列表加载失败' : '请选择平台'}</option>{platforms?.map((platform) => <option key={platform.code} value={platform.code}>{platform.name}</option>)}</select></label> : null}
				<div className="mt-3 flex flex-wrap items-center gap-3"><label className="flex items-center gap-2 text-xs text-gray-600"><input type="checkbox" checked={useKnowledge} onChange={(event) => setUseKnowledge(event.target.checked)} />引用本地知识库</label>{useKnowledge ? <select aria-label="选择知识库" value={knowledgeBaseID} onChange={(event) => setKnowledgeBaseID(Number(event.target.value))} className="rounded border border-brand-border bg-transparent px-2 py-1.5 text-xs"><option value={0}>请选择知识库</option>{knowledgeBases.map((base) => <option key={base.id} value={base.id}>{base.name}</option>)}</select> : null}</div>
				<label className="mt-3 flex items-start gap-2 text-xs text-gray-600"><input type="checkbox" checked={allowExternalAssist} onChange={(event) => setAllowExternalAssist(event.target.checked)} className="mt-0.5" />允许本次操作将当前草稿发送给已配置的模型服务；可能产生费用。未勾选时只生成本地建议。</label>
				<div className="mt-3 flex gap-2"><button type="button" disabled={assisting || !(draft.title || draft.caption || draft.body) || (assistAction === 'platform_adapt' && !assistPlatform) || (useKnowledge && !knowledgeBaseID)} onClick={runAssist} className="rounded-lg border border-brand-border px-3 py-1.5 text-xs font-semibold disabled:opacity-40">{assisting ? '生成中…' : allowExternalAssist ? '发送并生成' : '生成本地建议'}</button>{visibleAssistResult && visibleAssistResult.action !== 'check' ? <button type="button" onClick={applyAssistResult} className="rounded-lg border border-brand-border px-3 py-1.5 text-xs font-semibold">应用结果</button> : null}</div>
				{visibleAssistResult ? <pre className="mt-3 whitespace-pre-wrap rounded bg-black/[0.03] p-2.5 text-xs leading-5 text-gray-700">{visibleAssistResult.text}</pre> : null}
				{visibleAssistResult?.citations.length ? <div className="mt-2 flex flex-wrap gap-1.5">{visibleAssistResult.citations.map((citation) => <span key={citation.chunk_id} className="rounded-full border border-brand-border px-2 py-1 text-[10px] text-gray-600">[KB:{citation.chunk_id}] {citation.source_name}{citation.heading_path ? ` / ${citation.heading_path}` : ''}</span>)}</div> : null}
			</div>
          <div className="col-span-2 rounded-lg border border-brand-border p-3">
            <div className="flex items-center justify-between"><div><p className="text-xs font-semibold text-gray-700">关联素材库</p><p className="mt-0.5 text-[11px] text-gray-500">这里只选择“素材与脚本”中已归类的本地素材；导入和管理在独立素材库完成。</p></div><span className="text-[11px] text-gray-500">{media?.length ?? 0} 个</span></div>
            {selectedID ? <div className="mt-3 flex gap-2"><select data-testid="content-library-media-select" value={selectedLibraryID} onChange={(event) => setSelectedLibraryID(Number(event.target.value))} className="min-w-0 flex-1 rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm"><option value={0}>选择{draft.type === 'video' ? '视频或封面' : '图片'}素材</option>{libraryCandidates.map((item) => <option key={item.id} value={item.id}>{item.purpose === 'shared' ? '通用' : item.purpose === 'graph_text' ? '图文' : item.purpose === 'article' ? '文章' : '视频'} · {item.file_path.split(/[\\/]/).pop()}</option>)}</select><button type="button" onClick={() => setActiveTab('social-media')} className="rounded-lg border border-brand-border px-3 py-2 text-xs font-semibold">管理素材</button><button type="button" data-testid="content-attach-library-media" onClick={() => void attachLibraryMedia()} disabled={!selectedLibraryID} className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-3 py-2 text-xs font-semibold disabled:opacity-40"><Link className="h-3.5 w-3.5" />关联</button></div> : <p className="mt-3 text-xs text-gray-500">先保存内容后，再从独立素材库选择并关联。</p>}
            <div className="mt-3 space-y-1.5">{media?.map((item) => <div key={item.id} className="flex items-center gap-2 rounded bg-black/[0.03] px-2.5 py-2 text-xs"><span className="rounded bg-brand-accent/10 px-1.5 py-0.5 text-brand-accent">{item.kind}</span><span className="min-w-0 flex-1 truncate font-mono text-gray-600" title={item.file_path}>{item.file_path}</span><span className="text-gray-400">{Math.ceil(item.size_bytes / 1024)} KB</span><button type="button" onClick={() => detachMedia(item.id)} className="text-red-600" title="解除关联"><Trash2 className="h-3.5 w-3.5" /></button></div>)}</div>
          </div>
        </div>}
      </div>
    </section>
  );
}

function safeFileName(value: string) { return value.replace(/[\\/:*?"<>|]/g, '-').slice(0, 80) || 'untitled'; }
function escapeHTML(value: string) { return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;'); }
