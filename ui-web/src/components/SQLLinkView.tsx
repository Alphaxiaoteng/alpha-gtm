import { useEffect, useMemo, useRef, useState } from 'react';
import useSWR from 'swr';
import { CheckCircle2, ChevronRight, Database, FileUp, Play, ShieldCheck, TriangleAlert } from 'lucide-react';
import { api, type DataLinkInspection, type DataLinkPreflight, type DataLinkSnapshot } from '../api';

const stages = ['连接', '选表', '字段映射', '预检', '采集', '质量/指标', '写入知识库'];
const canonicalFields = [
  ['date', '日期'], ['shop', '店铺'], ['platform', '平台'], ['product', '商品'], ['sku', 'SKU'],
  ['content_id', '内容 ID'], ['impressions', '曝光'], ['clicks', '点击'], ['visitors', '访客'],
  ['orders', '订单'], ['gmv', 'GMV'], ['refunds', '退款'], ['ad_spend', '广告消耗'],
] as const;

function readableBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1 << 20) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1 << 20)).toFixed(1)} MB`;
}

export function SQLLinkView() {
  const fileInput = useRef<HTMLInputElement>(null);
  const state = useSWR('foundation/sql-link', api.getDataLinkState, { revalidateOnFocus: false });
  const bases = useSWR('foundation/sql-link/knowledge-bases', api.getKnowledgeBases, { revalidateOnFocus: false });
  const [sourceID, setSourceID] = useState(0);
  const [objectName, setObjectName] = useState('');
  const [query, setQuery] = useState('');
  const [inspection, setInspection] = useState<DataLinkInspection>();
  const [mapping, setMapping] = useState<Record<string, string>>({});
  const [preflight, setPreflight] = useState<DataLinkPreflight>();
  const [snapshot, setSnapshot] = useState<DataLinkSnapshot>();
  const [baseID, setBaseID] = useState(0);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [logs, setLogs] = useState<string[]>([]);

  const appendLog = (message: string) => setLogs((items) => [`${new Date().toLocaleTimeString('zh-CN', { hour12: false })}  ${message}`, ...items].slice(0, 12));
  const run = async <T,>(label: string, task: () => Promise<T>) => {
    setBusy(label); setError(''); appendLog(`${label}开始`);
    try { const result = await task(); appendLog(`${label}完成`); return result; }
    catch (cause) { const message = cause instanceof Error ? cause.message : `${label}失败`; setError(message); appendLog(`${label}失败 · ${message}`); return undefined; }
    finally { setBusy(''); }
  };

  useEffect(() => {
    if (!sourceID && state.data?.sources.length) setSourceID(state.data.sources[0].id);
  }, [sourceID, state.data?.sources]);

  useEffect(() => {
    if (!baseID && bases.data?.length) setBaseID(bases.data[0].id);
  }, [baseID, bases.data]);

  useEffect(() => {
    if (!sourceID) return;
    let active = true;
    void api.inspectDataLink(sourceID).then((result) => { if (active) { setInspection(result); setObjectName(''); setMapping({}); setPreflight(undefined); setSnapshot(undefined); } }).catch((cause) => { if (active) setError(cause instanceof Error ? cause.message : 'DATA_SOURCE_INSPECT_FAILED'); });
    return () => { active = false; };
  }, [sourceID]);

  const columns = useMemo(() => inspection?.objects.find((item) => item.name === objectName)?.columns.map((item) => item.name) ?? inspection?.preview_columns ?? [], [inspection, objectName]);
  const currentSource = state.data?.sources.find((source) => source.id === sourceID);
  const recentSnapshots = state.data?.snapshots ?? [];

  const upload = async (file?: File) => {
    if (!file) return;
    const source = await run('连接 SQLite', () => api.uploadSQLiteSource(file));
    if (!source) return;
    await state.mutate(); setSourceID(source.id); appendLog(`已登记 ${source.original_name}`);
  };

  const preview = async (nextObject = objectName) => {
    if (!sourceID || (!nextObject && !query.trim())) return;
    const result = await run('读取预览', () => api.inspectDataLink(sourceID, nextObject, query.trim()));
    if (!result) return;
    setInspection(result); setObjectName(nextObject); setMapping((current) => Object.keys(current).length ? current : result.suggested_mapping); setPreflight(undefined); setSnapshot(undefined);
  };

  const check = async () => {
    const result = await run('只读预检', () => api.preflightDataLink(sourceID, objectName, query.trim(), mapping));
    if (!result) return;
    setPreflight(result); setMapping((current) => Object.keys(current).length ? current : result.suggested_mapping);
  };

  const collect = async () => {
    const result = await run('一次性采集', () => api.collectDataLink(sourceID, objectName, query.trim(), mapping));
    if (!result) return;
    setSnapshot(result); await state.mutate();
  };

  const confirm = async () => {
    if (!snapshot || !baseID) return;
    const result = await run('写入知识库', () => api.confirmDataLinkReview(snapshot.id, baseID));
    if (!result) return;
    setSnapshot(result); await Promise.all([state.mutate(), bases.mutate()]);
  };

  return (
    <section data-testid="sql-link-view" className="ui-card overflow-hidden">
      <div className="flex flex-wrap items-start justify-between gap-4 border-b border-brand-border px-5 py-4">
        <div>
          <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em] text-brand-accent">SQL Link / Local first</p>
          <h2 className="mt-2 text-lg font-bold tracking-tight text-gray-950">数据接入与复盘</h2>
          <p className="mt-1 text-xs text-gray-500">本地 SQLite 真实采集；只读 SELECT / CTE，单次运行，不保存凭据。</p>
        </div>
        <button type="button" className="ui-btn-primary" disabled={Boolean(busy)} onClick={() => fileInput.current?.click()}><FileUp className="h-3.5 w-3.5" />连接 SQLite</button>
        <input ref={fileInput} className="hidden" type="file" accept=".db,.sqlite,.sqlite3" onChange={(event) => { void upload(event.target.files?.[0]); event.currentTarget.value = ''; }} />
      </div>

      <div className="flex gap-1 overflow-x-auto border-b border-brand-border bg-brand-bg/50 px-4 py-2">
        {stages.map((stage, index) => <div key={stage} className="flex shrink-0 items-center gap-1 text-[10px] font-semibold text-gray-500"><span className={`flex h-5 w-5 items-center justify-center rounded-full ${index <= (snapshot?.confirmed_at ? 6 : snapshot ? 5 : preflight ? 3 : inspection?.preview?.length ? 2 : sourceID ? 1 : 0) ? 'bg-gray-950 text-white' : 'border border-brand-border bg-white'}`}>{index + 1}</span>{stage}{index < stages.length - 1 ? <ChevronRight className="h-3 w-3 text-gray-300" /> : null}</div>)}
      </div>

      <div className="grid gap-px bg-brand-border xl:grid-cols-[17rem_minmax(0,1fr)_19rem]">
        <aside className="bg-brand-surface p-4">
          <p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-gray-400">数据源</p>
          <select className="mt-2 w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs" value={sourceID} onChange={(event) => setSourceID(Number(event.target.value))}>
            <option value={0}>选择 SQLite</option>
            {state.data?.sources.map((source) => <option key={source.id} value={source.id}>{source.name}</option>)}
          </select>
          {currentSource ? <div className="mt-3 rounded-lg border border-brand-border bg-brand-bg/60 p-3 text-[10px] text-gray-500"><p className="truncate font-semibold text-gray-800">{currentSource.original_name}</p><p className="mt-1">{readableBytes(currentSource.file_size)} · {currentSource.sha256.slice(0, 12)}</p></div> : <p className="mt-3 text-[10px] text-gray-400">选择文件后会流式复制到应用受控目录。</p>}
          <p className="mt-5 text-[10px] font-semibold uppercase tracking-[0.14em] text-gray-400">表 / 视图</p>
          <div className="mt-2 max-h-64 space-y-1 overflow-y-auto">
            {(inspection?.objects ?? []).map((object) => <button key={object.name} type="button" onClick={() => void preview(object.name)} className={`flex w-full items-center justify-between rounded-lg px-3 py-2 text-left text-xs ${objectName === object.name ? 'bg-gray-950 text-white' : 'hover:bg-brand-bg'}`}><span className="truncate">{object.name}</span><span className="ml-2 text-[9px] opacity-55">{object.type}</span></button>)}
          </div>
        </aside>

        <div className="min-w-0 bg-brand-surface p-4">
          <div className="flex items-start justify-between gap-3"><div><h3 className="text-sm font-semibold text-gray-900">字段映射与真实预览</h3><p className="mt-1 text-[10px] text-gray-500">未知字段保留在 raw；映射字段同时进入标准指标层。</p></div><ShieldCheck className="h-4 w-4 text-emerald-600" /></div>
          <div className="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {canonicalFields.map(([key, label]) => <label key={key} className="text-[10px] text-gray-500"><span>{label}</span><select className="mt-1 w-full rounded-md border border-brand-border bg-white px-2 py-1.5 text-xs text-gray-800" value={mapping[key] ?? ''} onChange={(event) => setMapping((current) => { const next = { ...current }; if (event.target.value) next[key] = event.target.value; else delete next[key]; return next; })}><option value="">不映射</option>{columns.map((column) => <option key={column} value={column}>{column}</option>)}</select></label>)}
          </div>
          <label className="mt-4 block text-[10px] font-semibold text-gray-600">可选只读 SQL</label>
          <div className="mt-1 flex items-start gap-2"><textarea value={query} onChange={(event) => setQuery(event.target.value)} placeholder="SELECT ... 或 WITH ... SELECT ..." className="min-h-16 flex-1 rounded-lg border border-brand-border bg-gray-950 p-2 font-mono text-[10px] text-gray-100 outline-none focus:border-brand-accent" /><button type="button" className="ui-btn" disabled={!sourceID || Boolean(busy)} onClick={() => void preview()}><Play className="h-3 w-3" />预览</button></div>
          <div className="mt-4 overflow-x-auto rounded-lg border border-brand-border">
            <table className="min-w-full text-left text-[10px]"><thead className="bg-brand-bg text-gray-500"><tr>{(inspection?.preview_columns ?? []).map((column) => <th key={column} className="whitespace-nowrap px-2 py-2 font-semibold">{column}</th>)}</tr></thead><tbody className="divide-y divide-brand-border">{(inspection?.preview ?? []).slice(0, 20).map((row, index) => <tr key={index}>{(inspection?.preview_columns ?? []).map((column) => <td key={column} className="max-w-44 truncate whitespace-nowrap px-2 py-1.5 text-gray-700" title={String(row[column] ?? '')}>{String(row[column] ?? '—')}</td>)}</tr>)}</tbody></table>
            {!inspection?.preview?.length ? <div className="px-3 py-8 text-center text-xs text-gray-400">选择表或输入只读查询后预览真实数据</div> : null}
          </div>
          <div className="mt-4 flex flex-wrap gap-2"><button type="button" className="ui-btn" disabled={!sourceID || (!objectName && !query.trim()) || Boolean(busy)} onClick={() => void check()}>预检</button><button type="button" className="ui-btn-primary" disabled={!preflight || Boolean(busy)} onClick={() => void collect()}><Database className="h-3.5 w-3.5" />采集一次</button></div>
          {preflight ? <div className="mt-3 rounded-lg border border-emerald-200 bg-emerald-50/60 p-3 text-[10px] text-emerald-800"><p className="font-semibold">预检通过 · 约 {preflight.estimated_rows} 行</p><p className="mt-1 font-mono opacity-70">schema {preflight.schema_fingerprint.slice(0, 16)}</p>{(preflight.warnings ?? []).map((warning) => <p key={warning} className="mt-1">{warning}</p>)}</div> : null}
        </div>

        <aside className="bg-brand-surface p-4">
          <h3 className="text-sm font-semibold text-gray-900">质量与复盘</h3>
          {snapshot ? <div className="mt-3 space-y-3"><div className="rounded-lg border border-brand-border p-3"><div className="flex items-center justify-between"><span className="text-xs font-semibold">快照 #{snapshot.id}</span><span className="text-[10px] text-emerald-700">{snapshot.status}</span></div><div className="mt-3 grid grid-cols-2 gap-2 text-[10px]"><div><p className="text-gray-400">行数</p><p className="font-semibold">{snapshot.row_count}</p></div><div><p className="text-gray-400">体积</p><p className="font-semibold">{readableBytes(snapshot.byte_count)}</p></div><div className="col-span-2"><p className="text-gray-400">校验和</p><p className="truncate font-mono" title={snapshot.checksum}>{snapshot.checksum}</p></div></div></div><div className="rounded-lg border border-brand-border p-3"><p className="text-[10px] font-semibold text-gray-700">质量问题</p>{snapshot.quality.issues?.length ? <ul className="mt-2 space-y-1 text-[10px] text-brand-warning">{snapshot.quality.issues.map((issue) => <li key={issue}>· {issue}</li>)}</ul> : <p className="mt-2 flex items-center gap-1 text-[10px] text-emerald-700"><CheckCircle2 className="h-3 w-3" />未发现基础质量问题</p>}<p className="mt-2 text-[10px] text-gray-400">日期 {snapshot.quality.date_min || '—'} → {snapshot.quality.date_max || '—'}</p></div><select className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs" value={baseID} onChange={(event) => setBaseID(Number(event.target.value))}><option value={0}>选择知识库</option>{bases.data?.map((base) => <option key={base.id} value={base.id}>{base.name}</option>)}</select><button type="button" className="ui-btn-primary w-full justify-center" disabled={!baseID || Boolean(snapshot.confirmed_at) || Boolean(busy)} onClick={() => void confirm()}>{snapshot.confirmed_at ? '已写入知识库' : '确认写入知识库'}</button></div> : <p className="mt-3 rounded-lg border border-dashed border-brand-border px-3 py-8 text-center text-[10px] text-gray-400">采集完成后显示快照、质量问题与复盘入口</p>}
          {error ? <div className="mt-3 flex gap-2 rounded-lg border border-rose-200 bg-rose-50 p-3 text-[10px] text-rose-700"><TriangleAlert className="h-3.5 w-3.5 shrink-0" /><span className="break-all">{error}</span></div> : null}
          <div className="mt-4"><p className="text-[10px] font-semibold uppercase tracking-[0.14em] text-gray-400">本次运行日志</p><div className="mt-2 max-h-36 space-y-1 overflow-y-auto rounded-lg bg-gray-950 p-3 font-mono text-[9px] text-white/60">{logs.length ? logs.map((line) => <p key={line}>{line}</p>) : <p>等待用户操作</p>}</div></div>
          {recentSnapshots.length ? <p className="mt-3 text-[9px] text-gray-400">最近快照 #{recentSnapshots[0].id} · {recentSnapshots[0].status}</p> : null}
        </aside>
      </div>
    </section>
  );
}
