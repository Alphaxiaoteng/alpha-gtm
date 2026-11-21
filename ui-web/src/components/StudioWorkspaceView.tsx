import { useEffect, useMemo, useState } from 'react';
import useSWR from 'swr';
import { CalendarClock, Clapperboard, FileText, Sparkles } from 'lucide-react';
import { api, apiErrorText, type StudioScheduleHandoff } from '../api';
import { useUIStore } from '../store';
import { CreationWorkbenchView } from './CreationWorkbenchView';
import { StudioView } from './StudioView';

type StudioWorkspaceSection = 'creation' | 'assets' | 'handoff';

const sections: Array<{ id: StudioWorkspaceSection; label: string; icon: typeof Sparkles }> = [
  { id: 'creation', label: '创作工作流', icon: Sparkles },
  { id: 'assets', label: '素材与剪辑', icon: Clapperboard },
  { id: 'handoff', label: '排期交接', icon: CalendarClock },
];

const handoffStatus: Record<StudioScheduleHandoff['status'], string> = {
  ready: '待 Media 冻结',
  frozen: '已冻结',
  superseded: '已被新交接替代',
  cancelled: '已取消',
  expired: '已过期',
};

function browserTimezone() {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
}

function nextLocalDateTime() {
  const date = new Date();
  date.setDate(date.getDate() + 1);
  date.setHours(20, 0, 0, 0);
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function scheduleLabel(handoff: StudioScheduleHandoff) {
  return `${handoff.original_local_time.replace('T', ' ')} · ${handoff.timezone}`;
}

function StudioScheduleHandoffPanel() {
  const { toast } = useUIStore();
  const { data: workflowViews = [] } = useSWR('api/creation/workflows', api.getCreationWorkflows);
  const { data: contents = [] } = useSWR('api/contents', api.getContents);
  const { data: scripts = [] } = useSWR('api/scripts', api.getStudioScripts);
  const { data: handoffs = [], error, mutate } = useSWR('api/studio/schedule-handoffs', api.getStudioScheduleHandoffs);
  const [workflowID, setWorkflowID] = useState('');
  const [contentID, setContentID] = useState(0);
  const [scriptID, setScriptID] = useState(0);
  const [originalLocalTime, setOriginalLocalTime] = useState(nextLocalDateTime);
  const [timezone] = useState(browserTimezone);
  const [saving, setSaving] = useState(false);
  const [cancellingID, setCancellingID] = useState<number | null>(null);

  const reviewWorkflows = useMemo(
    () => workflowViews.filter((item) => item.workflow.stage === 'review'),
    [workflowViews],
  );
  const selectedContent = useMemo(
    () => contents.find((item) => item.id === contentID) ?? null,
    [contents, contentID],
  );
  const matchingScripts = useMemo(
    () => selectedContent ? scripts.filter((script) => script.content_type === selectedContent.type) : [],
    [scripts, selectedContent],
  );

  useEffect(() => {
    if (!reviewWorkflows.some((item) => item.workflow.id === workflowID)) setWorkflowID(reviewWorkflows[0]?.workflow.id || '');
  }, [reviewWorkflows, workflowID]);

  useEffect(() => {
    if (!matchingScripts.some((item) => item.id === scriptID)) setScriptID(0);
  }, [matchingScripts, scriptID]);

  const create = async () => {
    const workflow = reviewWorkflows.find((item) => item.workflow.id === workflowID)?.workflow;
    if (!workflow || workflow.stage !== 'review') {
      toast.show('仅审校阶段的创作工作流可以交接排期', 'error');
      return;
    }
    if (!contentID || !originalLocalTime || !timezone) {
      toast.show('请选择已保存内容，并填写排期时间与时区', 'error');
      return;
    }
    setSaving(true);
    try {
      await api.createStudioScheduleHandoff({
        workflowId: workflow.id,
        contentId: contentID,
        scriptId: scriptID || undefined,
        originalLocalTime,
        timezone,
      });
      await mutate();
      toast.show('Studio 排期交接已保存，等待 Media 选择平台与账号', 'success');
    } catch (reason) {
      toast.show(apiErrorText(reason, '排期交接保存失败'), 'error');
    } finally {
      setSaving(false);
    }
  };

  const cancel = async (handoff: StudioScheduleHandoff) => {
    setCancellingID(handoff.id);
    try {
      await api.cancelStudioScheduleHandoff(handoff.id);
      await mutate();
      toast.show('排期交接已取消', 'success');
    } catch (reason) {
      toast.show(apiErrorText(reason, '取消排期交接失败'), 'error');
    } finally {
      setCancellingID(null);
    }
  };

  return <section className="mx-auto max-w-6xl space-y-5" data-testid="studio-schedule-handoff">
    <header>
      <h3 className="ui-page-title">排期交接</h3>
      <p className="ui-page-sub">保存已审校内容的本地墙钟时间；Media 再选择账号与平台，并只在平台原生排期中处理。</p>
    </header>

    <article className="ui-card p-4">
      <div className="flex items-start gap-3">
        <CalendarClock className="mt-0.5 h-5 w-5 shrink-0 text-brand-accent" />
        <div><h4 className="text-sm font-bold text-gray-900">创建交接</h4><p className="mt-1 text-xs leading-5 text-gray-500">没有账号、平台或审批操作。时间由后端校验并冻结为交接意图。</p></div>
      </div>
      <div className="mt-4 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <label className="text-xs font-semibold text-gray-700">已审校工作流
          <select data-testid="studio-handoff-workflow" aria-label="已审校工作流" value={workflowID} onChange={(event) => setWorkflowID(event.target.value)} className="mt-1.5 w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-sm" required>
            <option value="">选择审校工作流</option>
            {reviewWorkflows.map(({ workflow }) => <option key={workflow.id} value={workflow.id}>{workflow.title || workflow.id}</option>)}
          </select>
        </label>
        <label className="text-xs font-semibold text-gray-700">Media 内容
          <select data-testid="studio-handoff-content" aria-label="Media 内容" value={contentID || ''} onChange={(event) => setContentID(Number(event.target.value) || 0)} className="mt-1.5 w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-sm" required>
            <option value="">选择已保存内容</option>
            {contents.filter((item) => item.status !== 'archived').map((item) => <option key={item.id} value={item.id}>{item.title || `内容 #${item.id}`} · {item.type}</option>)}
          </select>
        </label>
        <label className="text-xs font-semibold text-gray-700">可选脚本
          <select data-testid="studio-handoff-script" aria-label="可选脚本" value={scriptID || ''} onChange={(event) => setScriptID(Number(event.target.value) || 0)} className="mt-1.5 w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-sm">
            <option value="">不附加脚本</option>
            {matchingScripts.map((script) => <option key={script.id} value={script.id}>{script.title || `脚本 #${script.id}`}</option>)}
          </select>
        </label>
        <label className="text-xs font-semibold text-gray-700">原始排期时间
          <input data-testid="studio-handoff-local-time" aria-label="原始排期时间" type="datetime-local" value={originalLocalTime} onChange={(event) => setOriginalLocalTime(event.target.value)} className="mt-1.5 w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-sm" required />
        </label>
      </div>
      <div className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-brand-border bg-brand-bg px-3 py-2.5">
        <span className="text-xs text-gray-600">浏览器时区：<strong data-testid="studio-handoff-timezone" className="font-mono text-gray-900">{timezone}</strong></span>
        <button type="button" data-testid="studio-handoff-create" onClick={() => void create()} disabled={saving || !reviewWorkflows.length} className="ui-btn ui-btn-primary disabled:opacity-40">{saving ? '保存中…' : '保存排期交接'}</button>
      </div>
      {!reviewWorkflows.length ? <p className="mt-3 text-xs text-brand-warning">暂无审校阶段工作流。请先在创作工作流完成选题、文案、脚本与审校。</p> : null}
    </article>

    <article className="ui-card overflow-hidden">
      <div className="flex items-center justify-between border-b border-brand-border px-4 py-3"><div><h4 className="text-sm font-bold text-gray-900">已保存交接</h4><p className="mt-1 text-xs text-gray-500">状态仅反映本地交接与冻结审计，不代表平台原生排期已成功。</p></div><FileText className="h-4 w-4 text-gray-400" /></div>
      {error ? <p className="p-4 text-xs text-red-700">交接列表读取失败：{apiErrorText(error, '未知错误')}</p> : null}
      <div className="divide-y divide-brand-border">
        {handoffs.map((handoff) => <div key={handoff.id} data-testid={`studio-handoff-${handoff.id}`} className="flex flex-wrap items-center gap-3 px-4 py-3 text-xs">
          <div className="min-w-0 flex-1"><strong className="block truncate text-sm text-gray-900">内容 #{handoff.content_id} · 工作流 {handoff.workflow_id}</strong><span className="mt-1 block text-gray-500">{scheduleLabel(handoff)}</span></div>
          <span className="rounded border border-brand-border bg-brand-bg px-2 py-1 font-semibold text-gray-700">{handoffStatus[handoff.status]}</span>
          {handoff.status === 'ready' ? <button type="button" onClick={() => void cancel(handoff)} disabled={cancellingID === handoff.id} className="ui-btn text-red-700 disabled:opacity-40">{cancellingID === handoff.id ? '取消中…' : '取消交接'}</button> : null}
        </div>)}
        {!handoffs.length ? <p className="px-4 py-10 text-center text-xs text-gray-400">还没有排期交接。</p> : null}
      </div>
    </article>
  </section>;
}

export function StudioWorkspaceView() {
  const [section, setSection] = useState<StudioWorkspaceSection>('creation');
  return <section className="space-y-5" data-testid="studio-workspace">
    <header><h3 className="ui-page-title">Studio</h3><p className="ui-page-sub">完成选题、文案、脚本和审校，保存本地内容版本后交接给 Media。</p></header>
    <div role="tablist" aria-label="Studio 工作区" className="inline-flex gap-1 rounded-lg border border-brand-border bg-brand-surface p-1">
      {sections.map(({ id, label, icon: Icon }) => <button key={id} type="button" role="tab" aria-selected={section === id} aria-controls={`studio-workspace-${id}`} onClick={() => setSection(id)} className={`inline-flex items-center gap-2 rounded-md px-3 py-2 text-xs font-semibold ${section === id ? 'bg-brand-accent text-brand-on-accent' : 'text-gray-600 hover:bg-black/[0.04]'}`}><Icon className="h-3.5 w-3.5" />{label}</button>)}
    </div>
    <div role="tabpanel" id={`studio-workspace-${section}`}>
      {section === 'creation' ? <CreationWorkbenchView /> : null}
      {section === 'assets' ? <StudioView /> : null}
      {section === 'handoff' ? <StudioScheduleHandoffPanel /> : null}
    </div>
  </section>;
}
