import { useEffect, useMemo, useState } from 'react';
import useSWR from 'swr';
import {
  AlertTriangle,
  Ban,
  CheckCircle2,
  ExternalLink,
  FolderOpen,
  RefreshCw,
  RotateCcw,
  Trash2,
} from 'lucide-react';
import {
  api,
  apiErrorText,
  type JobEvent,
  type PublishBackendVerificationResult,
  type PublishContentRecord,
  type PublishJobDetail,
  type PublishJobSummary,
  type PublishPlanSummary,
} from '../api';
import { canVerifyXHSCreatorBackend } from '../publishPlanReadiness';
import { describePublishIntent, type PublishIntent } from '../publishOutcome';
import { useUIStore } from '../store';
import { DEMO_BADGE, demoPublishContentRecords, isDemoModeEnabled, isDemoRecordID, type DemoPublishRecord } from '../lib/demoData';
import { PlatformIcon } from './PlatformIcon';

const attentionStatuses = new Set([
  'failed',
  'retryable_failed',
  'permanent_failed',
  'login_required',
  'manual_takeover',
  'awaiting_review',
  'awaiting_verification',
  'submitted_pending_verification',
  'result_unknown',
]);

const statusLabels: Record<string, string> = {
  pending: '待执行',
  queued: '已排队',
  running: '执行中',
  failed: '失败',
  login_required: '需要登录',
  retryable_failed: '可重试',
  permanent_failed: '永久失败',
  manual_takeover: '历史人工状态',
  awaiting_review: '等待确认',
  awaiting_verification: '等待后台核验',
  submitted_pending_verification: '已提交待核验',
  result_unknown: '结果待核验',
  cancelled: '已取消',
  success: '成功',
  published: '已发布',
  published_verified: '发布已核验',
  scheduled_verified: '排期已核验',
};

const remoteStateLabels: Record<PublishContentRecord['remote_state'], string> = {
  scheduled: '排期成功',
  published: '发布成功',
  delete_pending: '删除中',
  delete_failed: '删除失败',
  deleted: '已删除',
};

const completedPlanStatuses = new Set(['completed', 'success', 'published']);

const planStatusLabels: Record<string, string> = {
  frozen: '已冻结未执行',
  manual_takeover: '历史人工状态',
  awaiting_review: '等待确认',
  preparing: '准备中',
  prepared: '已准备未发布',
  running: '执行中',
  completed: '已完成',
  success: '已完成',
  published: '已完成',
  failed: '失败',
  login_required: '需要登录',
  partial_failed: '部分失败',
  submitted_pending_verification: '已提交待核验',
  result_unknown: '结果待核验',
  published_verified: '发布已核验',
  scheduled_verified: '排期已核验',
  cancelled: '已取消',
};

function formatTime(value?: string) {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function statusTone(status: string) {
  if (status === 'manual_takeover' || status === 'login_required' || status === 'awaiting_review' || status === 'awaiting_verification' || status === 'submitted_pending_verification' || status === 'result_unknown') return 'border-brand-warning/30/30 bg-brand-warning/20/10 text-brand-warning';
  if (status === 'failed' || status === 'retryable_failed' || status === 'permanent_failed') return 'border-red-400/30 bg-red-500/10 text-red-700';
  if (status === 'running' || status === 'queued' || status === 'pending') return 'border-brand-border bg-brand-surface-raised text-brand-muted';
  if (status === 'success' || status === 'published' || status === 'published_verified' || status === 'scheduled_verified') return 'border-emerald-400/30 bg-emerald-500/10 text-emerald-700';
  return 'border-brand-border bg-brand-bg text-gray-600';
}

function planStatusTone(status: string) {
  if (completedPlanStatuses.has(status)) return 'border-emerald-400/30 bg-emerald-500/10 text-emerald-700';
  if (status === 'manual_takeover' || status === 'login_required' || status === 'awaiting_review' || status === 'submitted_pending_verification' || status === 'result_unknown') return 'border-brand-warning/30/30 bg-brand-warning/20/10 text-brand-warning';
  if (status === 'failed' || status === 'partial_failed') return 'border-red-400/30 bg-red-500/10 text-red-700';
  if (status === 'frozen' || status === 'preparing' || status === 'prepared' || status === 'running') return 'border-brand-border bg-brand-surface-raised text-brand-muted';
  return 'border-brand-border bg-brand-bg text-gray-600';
}

function planStatusLabel(status: string) {
  return planStatusLabels[status] ?? '状态待核对';
}

const scheduledDeletePlatforms = new Set(['douyin', 'youtube', 'tiktok', 'kuaishou', 'wechat_video', 'facebook']);

function canDeleteRemoteContent(item: PublishContentRecord) {
  // 演示行没有真实远端记录，删除只会 404。
  if (isDemoRecordID(item.id)) return false;
  if (item.remote_state === 'scheduled') {
    return scheduledDeletePlatforms.has(item.platform_code);
  }
  return item.platform_code === 'kuaishou'
    && item.remote_state === 'published'
    && Boolean(item.post_url?.trim())
    && Boolean(item.external_post_id?.trim());
}

function PublishPlanRow({ plan }: { plan: PublishPlanSummary }) {
  return (
    <article className="grid gap-2 border-b border-brand-border p-3 last:border-b-0 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs font-semibold text-gray-900">计划 #{plan.id}</span>
          <span className={`rounded border px-1.5 py-0.5 text-[10px] font-semibold ${planStatusTone(plan.status)}`}>
            {planStatusLabel(plan.status)}
          </span>
          {completedPlanStatuses.has(plan.status) ? <span className="text-[10px] text-emerald-700">已确认发布记录另行统计</span> : null}
        </div>
        <p className="mt-1 truncate text-sm font-semibold text-gray-900">{plan.contentTitle || `内容 #${plan.contentId}`}</p>
        <p className="mt-1 text-[11px] text-gray-500">
          内容 #{plan.contentId} · {plan.platformCodes.length ? plan.platformCodes.join('、') : '未记录平台'} · 目标 {plan.targetCount} 个 · 创建于 {formatTime(plan.createdAt)}
        </p>
      </div>
      <p className="text-[11px] text-gray-400 sm:text-right">{plan.mediaMode === 'video' ? '视频' : plan.mediaMode === 'graph_text' ? '图文' : '文章'}</p>
    </article>
  );
}

function intentTone(tone: PublishIntent['tone']) {
  if (tone === 'safe') return 'border-brand-border bg-brand-surface-raised text-brand-muted';
  if (tone === 'review') return 'border-brand-warning/30/30 bg-brand-warning/20/10 text-brand-warning';
  return 'border-brand-border bg-brand-bg text-gray-600';
}

function JobRow({ job, selected, onSelect }: { job: PublishJobSummary; selected: boolean; onSelect: () => void }) {
  const intent = describePublishIntent(job.mode);
  return (
    <button
      type="button"
      onClick={onSelect}
      className={`w-full border-l-2 px-3 py-3 text-left transition ${selected ? 'border-brand-accent bg-brand-accent/5' : 'border-transparent hover:bg-black/[0.03]'}`}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <PlatformIcon platform={job.platform_code} className="h-[18px] w-[18px]" />
          <span className="truncate text-sm font-semibold text-gray-900">{job.platform_code} · 任务 #{job.id}</span>
        </div>
        <span className={`shrink-0 rounded border px-1.5 py-0.5 text-[10px] font-semibold ${statusTone(job.status)}`}>
          {statusLabels[job.status] ?? job.status}
        </span>
      </div>
      <p className="mt-1 text-xs text-gray-500">内容 #{job.content_id} · 账号 #{job.account_id} · {job.current_step || '尚未开始'}</p>
      <p className="mt-1 text-[11px] text-gray-500"><span className={`rounded border px-1 py-0.5 font-semibold ${intentTone(intent.tone)}`}>{intent.label}</span></p>
      {job.error_code ? <p className="mt-1 truncate font-mono text-[11px] text-red-600">{job.error_code}</p> : null}
    </button>
  );
}

function JobEventRow({ event, onOpenPath }: { event: JobEvent; onOpenPath: (path: string) => void }) {
  const diagnosticPath = event.screenshot_path || event.dom_snapshot_path;
  return (
    <li className="relative border-l border-brand-border pb-4 pl-4 last:pb-0">
      <span className={`absolute -left-1 top-1 h-2 w-2 rounded-sm ${event.level === 'error' ? 'bg-red-500' : event.level === 'warn' ? 'bg-brand-warning/20' : 'bg-brand-accent'}`} />
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <strong className="text-xs text-gray-800">{event.event_type}</strong>
        {event.step ? <span className="font-mono text-[10px] text-gray-500">{event.step}</span> : null}
        <time className="text-[10px] text-gray-400">{formatTime(event.created_at)}</time>
      </div>
      {event.message ? <p className="mt-1 text-xs leading-relaxed text-gray-600">{event.message}</p> : null}
      <div className="mt-1 flex flex-wrap gap-2 text-[10px] text-gray-500">
        {event.error_code ? <code className="text-red-600">{event.error_code}</code> : null}
        {event.retryable ? <span className="text-brand-warning">可重试</span> : null}
        {event.duration_ms > 0 ? <span>{event.duration_ms}ms</span> : null}
        {diagnosticPath ? (
          <button type="button" onClick={() => onOpenPath(diagnosticPath)} className="inline-flex items-center gap-1 font-semibold text-brand-accent hover:underline">
            <FolderOpen className="h-3 w-3" />诊断文件
          </button>
        ) : null}
      </div>
    </li>
  );
}

function JobDetailPanel({
  job,
  loading,
  action,
  onCancel,
  onRetry,
  onVerify,
  verificationResult,
  onOpenPath,
}: {
  job?: PublishJobDetail;
  loading: boolean;
  action: 'cancel' | 'retry' | 'verify' | null;
  onCancel: () => void;
  onRetry: () => void;
  onVerify: () => void;
  verificationResult: (PublishBackendVerificationResult & { jobID: number }) | null;
  onOpenPath: (path: string) => void;
}) {
  if (loading) return <div className="ui-card-flat p-6 text-sm text-gray-500">正在读取任务详情…</div>;
  if (!job) return <div className="ui-card-flat p-6 text-sm text-gray-500">选择任务查看错误、事件与诊断证据。</div>;
  const intent = describePublishIntent(job.mode);
  return (
    <article className="ui-card-flat p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <PlatformIcon platform={job.platform_code} className="h-5 w-5" />
            <h4 className="text-sm font-bold text-gray-900">任务 #{job.id}</h4>
            <span className={`rounded border px-1.5 py-0.5 text-[10px] font-semibold ${statusTone(job.status)}`}>{statusLabels[job.status] ?? job.status}</span>
          </div>
          <p className="mt-1 text-xs text-gray-500">{job.current_step || '尚未开始'} · 重试 {job.retry_count}/{job.max_retries}</p>
        </div>
        <div className="flex gap-2">
          {job.can_cancel ? (
            <button type="button" disabled={action !== null} onClick={onCancel} className="ui-btn text-red-600 disabled:opacity-50">
              <Ban className="h-3.5 w-3.5" />{action === 'cancel' ? '取消中…' : '取消任务'}
            </button>
          ) : null}
          {job.can_retry ? (
            <button type="button" disabled={action !== null} onClick={onRetry} className="ui-btn ui-btn-primary disabled:opacity-50">
              <RotateCcw className="h-3.5 w-3.5" />{action === 'retry' ? '入队中…' : '重试'}
            </button>
          ) : null}
          {canVerifyXHSCreatorBackend(job) ? (
            <button data-testid="publish-backend-verify" type="button" disabled={action !== null} onClick={onVerify} className="ui-btn border-brand-warning/30 text-brand-warning disabled:opacity-50">
              <RefreshCw className={`h-3.5 w-3.5 ${action === 'verify' ? 'animate-spin' : ''}`} />
              {action === 'verify' ? '后台核验中…' : '仅核验后台（不会再次发布）'}
            </button>
          ) : null}
        </div>
      </div>
      {job.error_code || job.error_message ? (
        <div className="mt-4 rounded-lg border border-red-400/25 bg-red-500/5 p-3">
          {job.error_code ? <code className="text-xs font-semibold text-red-600">{job.error_code}</code> : null}
          {job.error_message ? <p className="mt-1 text-xs leading-relaxed text-gray-700">{job.error_message}</p> : null}
        </div>
      ) : null}
      {verificationResult?.jobID === job.id ? (
        <div data-testid="publish-backend-verification-result" className="mt-4 rounded-lg border border-brand-warning/30 bg-brand-warning/10 p-3 text-xs text-brand-warning">
          <strong>后台核验结果</strong>
          <dl className="mt-2 grid gap-2 sm:grid-cols-3">
            <div><dt className="text-brand-warning">状态</dt><dd className="mt-0.5 font-semibold">{statusLabels[verificationResult.status] ?? verificationResult.status}</dd></div>
            <div><dt className="text-brand-warning">阶段</dt><dd className="mt-0.5 font-mono">{verificationResult.step || '—'}</dd></div>
            <div><dt className="text-brand-warning">错误码</dt><dd className="mt-0.5 font-mono">{verificationResult.error_code || '无'}</dd></div>
          </dl>
        </div>
      ) : null}
      <dl className="mt-4 grid gap-2 text-xs sm:grid-cols-2">
        <div><dt className="text-gray-400">创建时间</dt><dd className="mt-0.5 text-gray-700">{formatTime(job.created_at)}</dd></div>
        <div><dt className="text-gray-400">执行意图</dt><dd className="mt-0.5 text-gray-700"><span className={`rounded border px-1 py-0.5 text-[11px] font-semibold ${intentTone(intent.tone)}`}>{intent.label}</span><span className="mt-1 block text-[11px] leading-relaxed text-gray-500">{intent.description}</span></dd></div>
        <div><dt className="text-gray-400">Trace</dt><dd className="mt-0.5 truncate font-mono text-gray-700">{job.trace_id || '—'}</dd></div>
        <div><dt className="text-gray-400">冻结快照</dt><dd className="mt-0.5 font-mono text-gray-700">{job.snapshot_hash ? `${job.snapshot_hash.slice(0, 12)}…` : '—'}</dd></div>
        <div><dt className="text-gray-400">最终点击</dt><dd className="mt-0.5 text-gray-700">{job.submit_attempted ? '已尝试一次，禁止重放' : '尚未点击'}</dd></div>
        <div><dt className="text-gray-400">后台证据</dt><dd className="mt-0.5 text-gray-700">{job.creator_backend_verified && job.evidence_screenshot_saved ? 'Published/Public + 截图已保存' : '尚未验证'}</dd></div>
        {job.cancel_requested_at ? <div><dt className="text-gray-400">取消请求</dt><dd className="mt-0.5 text-gray-700">{formatTime(job.cancel_requested_at)}</dd></div> : null}
      </dl>
      {job.retry_blocked_by_submit ? <p className="mt-3 rounded-lg border border-brand-warning/30/25 bg-brand-warning/20/5 p-2 text-xs text-brand-warning">最终发布已经尝试；必须先检查创作者后台，不能重试。</p> : null}
      <div className="mt-5 border-t border-brand-border pt-4">
        <h5 className="mb-3 text-xs font-bold text-gray-800">执行时间线</h5>
        {job.events.length ? <ol>{job.events.map((event) => <JobEventRow key={event.id} event={event} onOpenPath={onOpenPath} />)}</ol> : <p className="text-xs text-gray-400">尚无事件。</p>}
      </div>
    </article>
  );
}

export function PublishRecordsView() {
  const { toast, setActiveTab, requestedPublishRecord, requestPublishRecord } = useUIStore();
  const [filter, setFilter] = useState<'attention' | 'all'>('attention');
  const [selectedJobID, setSelectedJobID] = useState<number | null>(null);
  const [action, setAction] = useState<'cancel' | 'retry' | 'verify' | null>(null);
  const [verificationResult, setVerificationResult] = useState<(PublishBackendVerificationResult & { jobID: number }) | null>(null);
  const [deletingRecordID, setDeletingRecordID] = useState<number | null>(null);
  const { data: jobs, error: jobsError, isLoading: jobsLoading, mutate: mutateJobs } = useSWR('api/jobs/list', api.getJobs);
  const { data: selectedJob, error: detailError, isLoading: detailLoading, mutate: mutateDetail } = useSWR(
    selectedJobID ? `api/jobs/${selectedJobID}` : null,
    () => api.getJob(selectedJobID!),
  );
  const { data: manualJobs, mutate: mutateManual } = useSWR('api/publish-jobs/manual', api.getManualPublishJobs);
  const { data: records, error: recordsError, mutate: mutateRecords } = useSWR('api/publish-records', api.getPublishRecords);
  const { data: contentRecordsRaw, error: contentRecordsError, mutate: mutateContentRecords } = useSWR('api/publish-content-records', api.getPublishContentRecords);
  // 后端还没有发布记录时补演示数据；有真实记录就完全让位。
  // 与账号页同一开关：演示数据需要 ?demo=1 显式开启，默认如实显示空。
  const usingDemoRecords = isDemoModeEnabled() && contentRecordsRaw !== undefined && contentRecordsRaw.length === 0;
  const contentRecords = usingDemoRecords ? demoPublishContentRecords() : contentRecordsRaw;
  const { data: plans, error: plansError, isLoading: plansLoading, mutate: mutatePlans } = useSWR('api/publish-plans', api.getPublishPlans);
  const visibleJobs = useMemo(
    () => (filter === 'all' ? jobs : jobs?.filter((job) => attentionStatuses.has(job.status))),
    [filter, jobs],
  );

  useEffect(() => {
    if (!requestedPublishRecord) return;
    setFilter('all');
    if (requestedPublishRecord.relatedType === 'publish_job') {
      if (!jobs?.some((job) => job.id === requestedPublishRecord.relatedID)) return;
      setSelectedJobID(requestedPublishRecord.relatedID);
    }
    requestPublishRecord(null);
  }, [jobs, requestPublishRecord, requestedPublishRecord]);

  useEffect(() => {
	if (visibleJobs === undefined) return;
    if (!visibleJobs?.length) {
      if (selectedJobID !== null) setSelectedJobID(null);
      return;
    }
    if (selectedJobID === null || !visibleJobs.some((job) => job.id === selectedJobID)) {
      setSelectedJobID(visibleJobs[0].id);
    }
  }, [selectedJobID, visibleJobs]);

  const refresh = async () => {
    await Promise.all([mutateJobs(), mutateManual(), mutateRecords(), mutateContentRecords(), mutatePlans(), selectedJobID ? mutateDetail() : Promise.resolve()]);
    toast.show('任务、计划与记录已刷新', 'success');
  };

  const runAction = async (kind: 'cancel' | 'retry') => {
    if (!selectedJobID) return;
    setAction(kind);
    try {
      const result = kind === 'cancel' ? await api.cancelJob(selectedJobID) : await api.retryJob(selectedJobID);
      await Promise.all([mutateJobs(), mutateDetail(result.job, { revalidate: false }), mutateManual()]);
      toast.show(kind === 'cancel' ? (result.action === 'cancelled' ? '任务已取消' : '已请求取消运行中的任务') : `任务已重新入队${result.queue_task_id ? ` #${result.queue_task_id}` : ''}`, 'success');
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, kind === 'cancel' ? '取消失败' : '重试失败'), 'error');
    } finally {
      setAction(null);
    }
  };

  const verifyCreatorBackend = async () => {
    if (!selectedJobID) return;
    setAction('verify');
    try {
      const result = await api.verifyPublishJobBackend(selectedJobID);
      setVerificationResult({ ...result, jobID: selectedJobID });
      toast.show(
        result.status === 'published'
          ? '创作者后台已确认发布'
          : '后台暂未确认发布；本次仅核验，没有再次提交',
        result.status === 'published' ? 'success' : 'info',
      );
    } catch (reason: unknown) {
      const message = apiErrorText(reason, '后台核验失败');
      setVerificationResult({
        jobID: selectedJobID,
        status: 'error',
        step: 'creator_backend_verification',
        error_code: message,
      });
      toast.show(message, 'error');
    } finally {
      await Promise.allSettled([mutateJobs(), mutateDetail(), mutateManual(), mutateRecords()]);
      setAction(null);
    }
  };

  const openDiagnostic = async (path: string) => {
    const opened = await window.electronAPI?.openPathInFinder?.(path);
    if (!opened) toast.show('诊断文件不存在或当前不是桌面端', 'error');
  };

  const openExternalURL = async (url: string) => {
    try {
      const result = await window.electronAPI?.openExternalURL?.(url);
      if (!result?.ok) toast.show(result?.error || '仅 Electron 桌面端支持打开外部链接', 'error');
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, '无法打开外部链接'), 'error');
    }
  };

  const deleteRemoteContent = async (item: PublishContentRecord) => {
    if (!window.confirm(`永久删除 ${item.account_name} 的“${item.title}”？应用会打开对应账号后台并执行删除；此操作无法撤销。`)) return;
    setDeletingRecordID(item.id);
    try {
      await api.deletePublishContentRecord(item.id);
      await Promise.all([mutateContentRecords(), mutateJobs()]);
      toast.show('平台内容已删除，记录已保留为“已删除”', 'success');
    } catch (reason: unknown) {
      toast.show(apiErrorText(reason, '平台内容删除失败'), 'error');
    } finally {
      setDeletingRecordID(null);
    }
  };

  return (
    <section className="mx-auto max-w-6xl space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="ui-page-title">发布记录</h3>
          <p className="ui-page-sub">排期成功与发布成功分开记录；平台删除结果也会同步保留。</p>
        </div>
        <div className="flex gap-2">
          <button type="button" onClick={() => setActiveTab('publish')} className="ui-btn">返回发布</button>
          <button type="button" onClick={refresh} className="ui-btn"><RefreshCw className="h-3.5 w-3.5" />刷新</button>
        </div>
      </div>

      <div className="ui-card overflow-hidden" data-testid="publish-content-records">
        <div className="border-b border-brand-border p-4">
          <div className="flex items-center gap-2">
            <h4 className="text-sm font-bold text-gray-900">发布内容记录</h4>
            {usingDemoRecords && (
              <span
                data-testid="publish-records-demo-badge"
                title="本机还没有发布记录，这里展示的是演示数据，不代表平台已核验发布"
                className="rounded-full border border-amber-300 bg-amber-50 px-2 py-0.5 text-[10px] font-semibold text-amber-700"
              >
                {DEMO_BADGE}
              </span>
            )}
          </div>
          <p className="mt-1 text-xs text-gray-500">这里只显示账号、封面、标题和远端状态；最多展示最近 100 条。</p>
        </div>
        {contentRecordsError ? <p className="p-4 text-sm text-red-600">发布内容记录加载失败</p> : null}
        {contentRecords?.length ? (
          <div className="divide-y divide-brand-border">
            {contentRecords.map((item) => (
              <article key={item.id} className="flex items-center gap-3 p-3">
                <div className="flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden border border-brand-border bg-brand-bg">
                  {/* 演示行也放真实产品/工厂实拍封面，不用平台图标凑数。 */}
                  <img
                    src={
                      isDemoRecordID(item.id)
                        ? (item as DemoPublishRecord).demo_cover ?? ''
                        : `/api/publish-content-records/${item.id}/cover`
                    }
                    alt=""
                    loading="lazy"
                    className="h-full w-full object-cover"
                    onError={(event) => { event.currentTarget.style.visibility = 'hidden'; }}
                  />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <PlatformIcon platform={item.platform_code} className="h-4 w-4" />
                    <span className="truncate text-xs text-gray-500">{item.account_name}</span>
                    <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-semibold ${item.remote_state === 'published' ? 'bg-emerald-500/10 text-emerald-700' : item.remote_state === 'scheduled' ? 'bg-brand-surface-raised text-brand-muted' : 'bg-gray-500/10 text-gray-600'}`}>
                      {remoteStateLabels[item.remote_state]}
                    </span>
                  </div>
                  <h5 className="mt-1 truncate text-sm font-semibold text-gray-900">{item.title}</h5>
                  <p className="mt-1 text-[10px] text-gray-400">{item.remote_state === 'scheduled' ? formatTime(item.scheduled_at) : formatTime(item.verified_at)}</p>
                  {item.error_code ? <p className="mt-1 truncate text-[10px] text-red-600">{item.error_code}</p> : null}
                </div>
                {canDeleteRemoteContent(item) ? (
                  <button type="button" onClick={() => void deleteRemoteContent(item)} disabled={deletingRecordID !== null} className="ui-btn shrink-0 text-red-600 disabled:opacity-40">
                    <Trash2 className="h-3.5 w-3.5" />{deletingRecordID === item.id ? '删除中…' : '删除'}
                  </button>
                ) : null}
              </article>
            ))}
          </div>
        ) : <p className="p-8 text-center text-sm text-gray-400">还没有已确认发布内容记录；发布计划见下方。</p>}
      </div>

      <div className="ui-card overflow-hidden" data-testid="publish-plans">
        <div className="border-b border-brand-border p-4">
          <h4 className="text-sm font-bold text-gray-900">发布计划</h4>
          <p className="mt-1 text-xs text-gray-500">计划、平台异常与历史状态都是审计轨迹；真正发布成功仍以“已确认发布”记录为准。</p>
        </div>
        {plansError ? <p className="p-4 text-sm text-red-600">发布计划加载失败：{apiErrorText(plansError, '未知错误')}</p> : null}
        {plansLoading ? <p className="p-4 text-sm text-gray-500">正在读取发布计划…</p> : null}
        {plans?.length ? <div>{plans.map((plan) => <PublishPlanRow key={plan.id} plan={plan} />)}</div> : !plansLoading && !plansError ? <p className="p-8 text-center text-sm text-gray-400">暂无发布计划。</p> : null}
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(260px,0.8fr)_minmax(0,1.4fr)]">
        <div className="ui-card overflow-hidden">
          <div className="flex items-center justify-between border-b border-brand-border p-3">
            <div className="flex items-center gap-2 text-sm font-bold text-gray-900"><AlertTriangle className="h-4 w-4 text-brand-warning" />任务中心</div>
            <div className="flex rounded-lg border border-brand-border p-0.5 text-[11px]">
              <button type="button" onClick={() => setFilter('attention')} className={`rounded px-2 py-1 ${filter === 'attention' ? 'bg-brand-accent/10 font-semibold text-brand-accent' : 'text-gray-500'}`}>需处理</button>
              <button type="button" onClick={() => setFilter('all')} className={`rounded px-2 py-1 ${filter === 'all' ? 'bg-brand-accent/10 font-semibold text-brand-accent' : 'text-gray-500'}`}>全部</button>
            </div>
          </div>
          {jobsError ? <p className="p-4 text-sm text-red-600">任务加载失败：{apiErrorText(jobsError, '未知错误')}</p> : null}
          {jobsLoading ? <p className="p-4 text-sm text-gray-500">正在读取任务…</p> : null}
          {visibleJobs?.length ? <div className="divide-y divide-brand-border">{visibleJobs.map((job) => <JobRow key={job.id} job={job} selected={selectedJobID === job.id} onSelect={() => setSelectedJobID(job.id)} />)}</div> : !jobsLoading && !jobsError ? <p className="p-8 text-center text-sm text-gray-400">{filter === 'attention' ? '当前没有需要处理的任务。' : '尚无发布任务。'}</p> : null}
        </div>
        <div>
          {detailError ? <div className="ui-card-flat p-4 text-sm text-red-600">详情加载失败：{apiErrorText(detailError, '未知错误')}</div> : <JobDetailPanel job={selectedJob} loading={detailLoading} action={action} onCancel={() => runAction('cancel')} onRetry={() => runAction('retry')} onVerify={verifyCreatorBackend} verificationResult={verificationResult} onOpenPath={openDiagnostic} />}
        </div>
      </div>

      {manualJobs?.length ? (
        <div className="ui-card space-y-3 border-brand-warning/30/25 p-4">
          <div className="flex items-center gap-2 text-sm font-semibold text-brand-warning"><CheckCircle2 className="h-4 w-4" />等待创作者后台核验</div>
          <p className="text-xs text-gray-600">用户填写链接不能证明发布成功。系统只有在内容管理页读取到 Published/Public（或已发布/已发表），同时保存匹配账号、内容 ID、公开链接与后台截图后，才会写入发布记录。</p>
          {manualJobs.map((job) => (
            <div key={job.id} className="rounded-lg border border-brand-border bg-brand-bg p-3 text-xs text-gray-700">
              <strong>{job.platform_code}</strong> · {job.status === 'manual_takeover' ? '历史人工状态，等待核验' : '最终点击后等待核验'}
              <br />内容 #{job.content_id} · 账号 #{job.account_id} · 任务 #{job.id}
              <br /><span className="text-gray-500">{job.current_step || 'creator_backend_verification'}</span>
            </div>
          ))}
        </div>
      ) : null}

      <div className="ui-card overflow-hidden">
        <div className="border-b border-brand-border p-4"><h4 className="text-sm font-bold text-gray-900">已确认发布</h4><p className="mt-1 text-xs text-gray-500">演练模式和尚未获得后台证据的任务不会出现在这里。</p></div>
        {recordsError ? <p className="p-4 text-sm text-red-600">记录加载失败</p> : null}
        {records?.length ? (
          <div className="divide-y divide-brand-border">
            {records.map((item) => (
              <div key={item.id} className="grid grid-cols-[1fr_auto] gap-3 p-4">
                <div><div className="flex items-center gap-2"><PlatformIcon platform={item.platform_code} className="h-[18px] w-[18px]" /><span className="font-semibold text-gray-900">{item.platform_code}</span><span className="rounded bg-emerald-500/10 px-1.5 py-0.5 text-[11px] text-emerald-700">{item.status}</span>{item.source === 'user_confirmed' ? <span className="text-[11px] text-gray-500">用户确认</span> : null}</div><p className="mt-1 text-xs text-gray-500">内容 #{item.content_id} · 账号 #{item.account_id} · {item.content_type} · 任务 #{item.job_id}</p>{item.external_post_id ? <p className="mt-1 font-mono text-[11px] text-gray-500">{item.external_post_id}</p> : null}</div>
                {item.post_url ? <button type="button" onClick={() => openExternalURL(item.post_url!)} className="ui-btn h-fit"><ExternalLink className="h-3.5 w-3.5" />打开</button> : null}
              </div>
            ))}
          </div>
        ) : <p className="p-10 text-center text-sm text-gray-400">尚无已确认的发布记录。</p>}
      </div>
    </section>
  );
}
