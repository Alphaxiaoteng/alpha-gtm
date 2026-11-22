import { useState } from 'react';
import useSWR from 'swr';
import { CheckCircle2, ExternalLink, RefreshCw, Trash2 } from 'lucide-react';
import {
  api,
  apiErrorText,
  type PublishContentRecord,
  type PublishPlanSummary,
} from '../api';
import { useUIStore } from '../store';
import { DEMO_BADGE, demoPublishContentRecords, isDemoModeEnabled, isDemoRecordID, type DemoPublishRecord } from '../lib/demoData';
import { PlatformIcon } from './PlatformIcon';

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

function planStatusTone(status: string) {
  if (completedPlanStatuses.has(status)) return 'bg-emerald-500/10 text-emerald-700';
  if (status === 'manual_takeover' || status === 'login_required' || status === 'awaiting_review' || status === 'submitted_pending_verification' || status === 'result_unknown') return 'bg-brand-warning/15 text-brand-warning';
  if (status === 'failed' || status === 'partial_failed') return 'bg-red-500/10 text-red-700';
  return 'bg-brand-surface-raised text-brand-muted';
}

const scheduledDeletePlatforms = new Set(['douyin', 'youtube', 'tiktok', 'kuaishou', 'wechat_video', 'facebook']);

function canDeleteRemoteContent(item: PublishContentRecord) {
  // 演示行没有真实远端记录，删除只会 404。
  if (isDemoRecordID(item.id)) return false;
  if (item.remote_state === 'scheduled') return scheduledDeletePlatforms.has(item.platform_code);
  return item.platform_code === 'kuaishou'
    && item.remote_state === 'published'
    && Boolean(item.post_url?.trim())
    && Boolean(item.external_post_id?.trim());
}

/**
 * 封面：演示行读本地 demo_cover，真实行走后端 cover 接口。
 * 图片缺失或接口 404 时保留灰底 + 平台图标占位，不让整块消失（行高也不跳）。
 */
function RecordCover({ src, platform }: { src: string; platform: string }) {
  const [failed, setFailed] = useState(false);
  if (!src || failed) {
    return (
      <div className="flex h-16 w-16 shrink-0 items-center justify-center border border-brand-border bg-brand-surface-raised">
        <PlatformIcon platform={platform} className="h-6 w-6 opacity-60" />
      </div>
    );
  }
  return (
    <div className="flex h-16 w-16 shrink-0 items-center justify-center overflow-hidden border border-brand-border bg-brand-bg">
      <img src={src} alt="" loading="lazy" className="h-full w-full object-cover" onError={() => setFailed(true)} />
    </div>
  );
}

function PublishPlanRow({ plan }: { plan: PublishPlanSummary }) {
  return (
    <article className="grid gap-2 border-b border-brand-border p-3 last:border-b-0 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <PlatformIcon platform={plan.platformCodes[0] ?? ''} className="h-4 w-4" />
          <span className="truncate text-sm font-semibold text-gray-900">{plan.platformCodes.join(' · ') || '未指定平台'}</span>
          <span className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${planStatusTone(plan.status)}`}>
            {planStatusLabels[plan.status] ?? '状态待核对'}
          </span>
        </div>
        <p className="mt-1 truncate text-xs text-gray-500">{plan.contentTitle || `内容 #${plan.contentId}`}</p>
      </div>
      <p className="shrink-0 text-[10px] text-gray-400">{formatTime(plan.createdAt)}</p>
    </article>
  );
}

export function PublishRecordsView() {
  const { toast, setActiveTab } = useUIStore();
  const [deletingRecordID, setDeletingRecordID] = useState<number | null>(null);

  const { data: manualJobs, mutate: mutateManual } = useSWR('api/publish-jobs/manual', api.getManualPublishJobs);
  const { data: contentRecordsRaw, error: contentRecordsError, mutate: mutateContentRecords } = useSWR('api/publish-content-records', api.getPublishContentRecords);
  // 后端还没有发布记录时补演示数据；有真实记录就完全让位。
  // 与账号页同一开关：演示数据需要 ?demo=1 显式开启，默认如实显示空。
  const usingDemoRecords = isDemoModeEnabled() && contentRecordsRaw !== undefined && contentRecordsRaw.length === 0;
  const contentRecords = usingDemoRecords ? demoPublishContentRecords() : contentRecordsRaw;
  const { data: plans, error: plansError, isLoading: plansLoading, mutate: mutatePlans } = useSWR('api/publish-plans', api.getPublishPlans);

  const refresh = async () => {
    await Promise.all([mutateManual(), mutateContentRecords(), mutatePlans()]);
    toast.show('记录与计划已刷新', 'success');
  };

  const deleteRemoteContent = async (item: PublishContentRecord) => {
    if (!window.confirm(`永久删除 ${item.account_name} 的“${item.title}”？应用会打开对应账号后台并执行删除；此操作无法撤销。`)) return;
    setDeletingRecordID(item.id);
    try {
      await api.deletePublishContentRecord(item.id);
      await mutateContentRecords();
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
                <RecordCover
                  platform={item.platform_code}
                  src={
                    isDemoRecordID(item.id)
                      ? (item as DemoPublishRecord).demo_cover ?? ''
                      : `/api/publish-content-records/${item.id}/cover`
                  }
                />
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
                {item.post_url?.trim() ? (
                  <button
                    type="button"
                    data-testid={`publish-record-open-${item.id}`}
                    onClick={() => window.open(item.post_url!, '_blank', 'noopener')}
                    className="ui-btn shrink-0"
                  >
                    <ExternalLink className="h-3.5 w-3.5" />打开原链接
                  </button>
                ) : null}
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

      {manualJobs?.length ? (
        <div className="ui-card space-y-3 p-4">
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
    </section>
  );
}
