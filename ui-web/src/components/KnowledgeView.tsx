import { useEffect, useMemo, useRef, useState } from 'react';
import useSWR from 'swr';
import { Bot, BookOpen, FilePlus2, FolderOpen, Search, Settings2, Upload, X } from 'lucide-react';
import { api, type KnowledgeImportResult, type KnowledgeSearchResult } from '../api';
import { KNOWLEDGE_FILE_ACCEPT, citationLabel, knowledgeAgentDraft, knowledgeDocumentStatusLabel, knowledgeImportItemLabel, summarizeKnowledgeImport } from '../knowledge';
import { useUIStore } from '../store';

type Panel = 'create' | 'import' | 'documents' | 'search' | null;

function dateLabel(value?: string) {
  if (!value) return '尚未同步';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleDateString('zh-CN');
}

export function KnowledgeView() {
  const { toast, setAgentDraft, openAgentDrawer, productMode } = useUIStore();
  const isShop = productMode === 'shop';
  const knowledgeAPI = isShop ? {
    listBases: api.getShopKnowledgeBases,
    createBase: api.createShopKnowledgeBase,
    importFiles: api.importShopKnowledge,
    listDocuments: api.getShopKnowledgeDocuments,
    search: api.searchShopKnowledge,
  } : {
    listBases: api.getKnowledgeBases,
    createBase: api.createKnowledgeBase,
    importFiles: api.importKnowledge,
    listDocuments: api.getKnowledgeDocuments,
    search: api.searchKnowledge,
  };
  const [busy, setBusy] = useState(false);
  const [panel, setPanel] = useState<Panel>(null);
  const { data: bases = [], error, mutate } = useSWR(`api/${productMode}/knowledge/bases`, knowledgeAPI.listBases, { refreshInterval: busy ? 1000 : 0 });
  const [selectedBaseID, setSelectedBaseID] = useState(0);
  const [newName, setNewName] = useState('');
  const [newDescription, setNewDescription] = useState('');
  const [files, setFiles] = useState<File[]>([]);
  const [importResult, setImportResult] = useState<KnowledgeImportResult | null>(null);
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<KnowledgeSearchResult[]>([]);
  const [documentQuery, setDocumentQuery] = useState('');
  const fileInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const preferredBase = isShop ? undefined : bases.find((base) => base.name === 'Alpha-Notes 编导与短视频');
  const activeBaseID = selectedBaseID || preferredBase?.id || bases.find((base) => base.is_default)?.id || bases[0]?.id || 0;
  const activeBase = bases.find((base) => base.id === activeBaseID);
  const { data: documents = [], mutate: mutateDocuments, error: documentsError } = useSWR(
    panel === 'documents' && activeBaseID ? [`api/${productMode}/knowledge/documents`, activeBaseID] : null,
    () => knowledgeAPI.listDocuments(activeBaseID),
  );
  const totals = useMemo(() => bases.reduce((sum, base) => sum + base.document_count, 0), [bases]);
  const visibleDocuments = useMemo(() => {
    const needle = documentQuery.trim().toLocaleLowerCase();
    return (needle ? documents.filter((document) => document.source_name.toLocaleLowerCase().includes(needle)) : documents).slice(0, 120);
  }, [documentQuery, documents]);

  useEffect(() => {
    folderInput.current?.setAttribute('webkitdirectory', '');
  }, []);

  const closePanel = () => setPanel(null);
  const createBase = async () => {
    if (!newName.trim()) return;
    setBusy(true);
    try {
      const created = await knowledgeAPI.createBase({ name: newName.trim(), description: newDescription.trim() });
      setSelectedBaseID(created.id);
      setNewName('');
      setNewDescription('');
      await mutate();
      closePanel();
      toast.show('资料库已创建', 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '创建资料库失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  const importFiles = async () => {
    if (!activeBaseID || files.length === 0) return;
    setBusy(true);
    try {
      const result = await knowledgeAPI.importFiles(activeBaseID, files);
      setImportResult(result);
      setFiles([]);
      if (fileInput.current) fileInput.current.value = '';
      if (folderInput.current) folderInput.current.value = '';
      await Promise.all([mutate(), mutateDocuments()]);
      toast.show(summarizeKnowledgeImport(result), result.failed ? 'info' : 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '导入资料失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  const importAlphaNotes = async () => {
    setBusy(true);
    try {
      const response = await api.importAlphaNotesKnowledge();
      setSelectedBaseID(response.base.id);
      setImportResult(response.import);
      await mutate();
      toast.show(`Alpha-Notes：${summarizeKnowledgeImport(response.import)}`, response.import.failed ? 'info' : 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '同步 Alpha-Notes 失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  const search = async () => {
    if (!query.trim()) return;
    setBusy(true);
    try {
      const response = await knowledgeAPI.search({ base_ids: activeBaseID ? [activeBaseID] : undefined, query: query.trim(), top_k: 6 });
      setResults(response.results);
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '检索失败', 'error');
    } finally {
      setBusy(false);
    }
  };

  const handoffToAgent = () => {
    if (!activeBaseID || !activeBase || activeBase.chunk_count === 0) return;
    setAgentDraft(knowledgeAgentDraft(activeBaseID, activeBase.name));
    openAgentDrawer();
  };

  return (
    <section data-testid="knowledge-workspace" className="h-full overflow-y-auto px-1 py-1">
      <div className="mx-auto max-w-5xl space-y-4">
        <header className="flex flex-wrap items-end justify-between gap-3 border-b border-brand-border pb-3">
          <div><h3 className="ui-page-title">{isShop ? '店铺知识库' : '资料管理'}</h3><p className="mt-1 text-sm text-gray-500">{isShop ? '商品、客服、渠道与经营资料单独存储，不与 Media 知识库混用。' : '把自己的资料交给 Alpha Agent，使用时在对话里明确选择范围。'}</p></div>
          <div className="flex gap-2"><button type="button" className="ui-btn" onClick={() => setPanel('create')}><FilePlus2 className="h-3.5 w-3.5" />新建资料库</button><button type="button" className="ui-btn ui-btn-primary" onClick={() => setPanel('import')}><Upload className="h-3.5 w-3.5" />导入资料</button></div>
        </header>

        <div className="flex flex-wrap items-center gap-3 border-b border-brand-border pb-4 text-xs text-gray-500"><span className="font-semibold text-gray-900">{bases.length} 个资料库</span><span>{totals.toLocaleString()} 篇资料</span><span>只在对话中按需引用</span>{error ? <span className="text-red-600">资料库暂不可用</span> : null}</div>

        <div data-testid="knowledge-base-list" className="ui-section overflow-hidden">
          {bases.map((base) => <button key={base.id} type="button" onClick={() => setSelectedBaseID(base.id)} aria-pressed={activeBaseID === base.id} className={`ui-row grid w-full grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-4 px-4 py-3 text-left ${activeBaseID === base.id ? 'bg-black/[0.035]' : ''}`}><div className="flex min-w-0 items-center gap-3"><span className="flex h-8 w-8 shrink-0 items-center justify-center text-gray-500"><BookOpen className="h-4 w-4" /></span><span className="min-w-0"><strong className="block truncate text-sm text-gray-900">{base.name}</strong><span className="mt-0.5 block truncate text-[11px] text-gray-500">{base.description || '本地创作资料'}</span></span></div><span className="text-right text-xs text-gray-600"><strong className="block text-sm text-gray-900">{base.document_count.toLocaleString()}</strong><span>篇资料</span></span><span className="min-w-24 text-right text-[11px] text-gray-500">{dateLabel(base.latest_import?.completed_at || base.latest_import?.created_at)}<span className="block">最近同步</span></span></button>)}
          {bases.length === 0 ? <div className="ui-empty m-3 text-sm">还没有资料库，从导入资料开始。</div> : null}
        </div>

        {activeBase ? <div data-testid="knowledge-current-base" className="flex flex-wrap items-center justify-between gap-3 border-y border-brand-border px-1 py-3 text-xs"><div><strong className="text-gray-900">当前资料库：{activeBase.name}</strong><span className="ml-2 text-gray-500">{activeBase.document_count.toLocaleString()} 篇资料 · 最近同步 {dateLabel(activeBase.latest_import?.completed_at || activeBase.latest_import?.created_at)}</span></div><div className="flex gap-2"><button type="button" className="ui-btn h-8" onClick={() => setPanel('documents')}><FolderOpen className="h-3.5 w-3.5" />资料目录</button><button type="button" className="ui-btn h-8" onClick={() => setPanel('search')}><Search className="h-3.5 w-3.5" />高级检索</button><button type="button" data-testid="knowledge-handoff-agent" className="ui-btn h-8" disabled={activeBase.chunk_count === 0} title={activeBase.chunk_count === 0 ? '先导入可检索资料' : '将当前资料库作为明确引用范围'} onClick={handoffToAgent}><Bot className="h-3.5 w-3.5" />交给 Agent</button></div></div> : null}
      </div>

      {panel ? <div className="fixed inset-0 z-40 flex justify-end bg-black/20" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) closePanel(); }}><aside role="dialog" aria-modal="true" className="flex h-full w-full max-w-xl flex-col bg-brand-surface shadow-2xl"><header className="flex items-center justify-between border-b border-brand-border px-5 py-4"><h4 className="text-base font-bold text-gray-900">{panel === 'create' ? '新建资料库' : panel === 'import' ? '导入资料' : panel === 'documents' ? '资料目录' : '高级检索'}</h4><button type="button" aria-label="关闭资料面板" onClick={closePanel} className="p-1 text-gray-500"><X className="h-4 w-4" /></button></header>
        <div className="flex-1 overflow-y-auto px-5 py-5">
          {panel === 'create' ? <div className="space-y-3"><label className="block text-xs font-semibold text-gray-600">名称<input value={newName} onChange={(event) => setNewName(event.target.value)} maxLength={120} className="mt-1.5 w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm" placeholder="品牌资料、产品资料…" /></label><label className="block text-xs font-semibold text-gray-600">说明<textarea value={newDescription} onChange={(event) => setNewDescription(event.target.value)} maxLength={2000} className="mt-1.5 min-h-28 w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm" placeholder="这个资料库用于什么创作？" /></label></div> : null}
          {panel === 'import' ? <div className="space-y-4"><p className="text-sm text-gray-600">导入到 <strong className="text-gray-900">{activeBase?.name || '当前资料库'}</strong>。支持 Markdown、HTML、TXT、PDF 和 DOCX。</p><div className="flex flex-wrap gap-2"><button type="button" className="ui-btn" onClick={() => fileInput.current?.click()}>选择文件</button><button type="button" className="ui-btn" onClick={() => folderInput.current?.click()}>选择文件夹</button>{!isShop ? <button type="button" data-testid="knowledge-import-alpha-notes" className="ui-btn" disabled={busy} onClick={() => void importAlphaNotes()}>同步 Alpha-Notes</button> : null}<input ref={fileInput} data-testid="knowledge-file-input" type="file" multiple accept={KNOWLEDGE_FILE_ACCEPT} onChange={(event) => setFiles(Array.from(event.target.files ?? []))} className="hidden" /><input ref={folderInput} data-testid="knowledge-folder-input" type="file" multiple accept={KNOWLEDGE_FILE_ACCEPT} onChange={(event) => setFiles(Array.from(event.target.files ?? []))} className="hidden" /></div>{files.length ? <p className="rounded-lg bg-black/[0.025] px-3 py-2 text-xs text-gray-600">已选 {files.length} 个文件</p> : null}{importResult ? <div className="rounded-lg border border-brand-border p-3 text-xs"><strong>{summarizeKnowledgeImport(importResult)}</strong><ul className="mt-2 max-h-72 space-y-1 overflow-y-auto">{importResult.items.slice(0, 50).map((item, index) => <li key={`${item.name}-${index}`} className={item.status === 'failed' ? 'text-red-600' : item.status === 'skipped' ? 'text-brand-warning' : 'text-emerald-700'}>{item.name} · {knowledgeImportItemLabel(item)}</li>)}</ul></div> : null}</div> : null}
          {panel === 'documents' ? <div><div className="mb-3 flex items-center gap-2"><FolderOpen className="h-4 w-4 text-brand-accent" /><input value={documentQuery} onChange={(event) => setDocumentQuery(event.target.value)} placeholder="筛选文件名或目录" className="min-w-0 flex-1 rounded-lg border border-brand-border bg-transparent px-3 py-2 text-xs" /></div><div className="divide-y divide-brand-border border-y border-brand-border">{visibleDocuments.map((document) => <div key={document.id} className="flex items-center gap-3 px-2 py-3 text-xs"><span className={`h-2 w-2 rounded-full ${document.status === 'ready' ? 'bg-emerald-500' : document.status === 'failed' ? 'bg-red-500' : 'bg-brand-warning/20'}`} /><span className="min-w-0 flex-1 truncate" title={document.source_name}>{document.source_name}</span><span className={`text-[10px] ${document.status === 'failed' ? 'text-red-600' : 'text-gray-400'}`}>{knowledgeDocumentStatusLabel(document)}</span></div>)}{documentsError ? <p className="py-10 text-center text-xs font-semibold text-red-600">资料目录加载失败，请刷新重试</p> : visibleDocuments.length === 0 ? <p className="py-10 text-center text-xs text-gray-500">暂无资料</p> : null}</div></div> : null}
          {panel === 'search' ? <div><div className="flex gap-2"><input autoFocus value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void search(); }} placeholder="输入关键词查看引用片段" className="min-w-0 flex-1 rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm" /><button type="button" className="ui-btn" disabled={busy || !query.trim()} onClick={() => void search()}>检索</button></div><div className="mt-4 space-y-3">{results.map((result) => <article key={result.chunk_id} className="border-b border-brand-border pb-3"><p className="text-xs font-semibold text-brand-accent">[KB:{result.chunk_id}] {citationLabel(result)}</p><p className="mt-2 whitespace-pre-wrap text-xs leading-5 text-gray-700">{result.content}</p></article>)}{query && results.length === 0 ? <p className="py-8 text-center text-xs text-gray-500">暂无匹配片段</p> : null}</div></div> : null}
        </div>
        <footer className="border-t border-brand-border px-5 py-4">{panel === 'create' ? <button type="button" className="ui-btn ui-btn-primary w-full" disabled={busy || !newName.trim()} onClick={() => void createBase()}>创建资料库</button> : panel === 'import' ? <button type="button" className="ui-btn ui-btn-primary w-full" disabled={busy || !activeBaseID || !files.length} onClick={() => void importFiles()}>{busy ? '正在同步…' : '导入并建立索引'}</button> : panel === 'documents' ? <button type="button" className="ui-btn w-full" onClick={() => setPanel('import')}><Upload className="h-3.5 w-3.5" />继续导入</button> : <p className="flex items-center gap-2 text-[11px] text-gray-500"><Settings2 className="h-3.5 w-3.5" />高级检索仅用于人工确认，Agent 只使用对话中明确选择的范围。</p>}</footer>
      </aside></div> : null}
    </section>
  );
}
