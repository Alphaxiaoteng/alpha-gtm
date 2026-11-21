import { useMemo } from 'react';
import useSWR from 'swr';
import { Activity, CircleDot, Clock3, RefreshCw, ShieldCheck } from 'lucide-react';
import { api, type Account, type OperationEvent } from '../api';

const toolLabels: Record<string, string> = {
  'account.check': '检查账号状态',
  'browser.status': '读取浏览器状态',
  'fleet.run': '执行浏览器 Fleet 操作',
  'meta.login': '打开 Meta 人工登录',
  'meta.discover': '发现 Meta 发布目标',
  'media.import': '导入本地素材',
  'studio.clip.plan': '生成剪辑方案',
  'studio.clip.execute': '执行本地剪辑',
  'publish.run': '创建发布任务',
  'publish.plan.freeze': '冻结发布计划',
  'publish.plan.prepare': '预填发布表单',
  'publish.plan.submit': '提交并核验发布',
  'publish.creator_backend_verified': '发布已验证',
};

const statusLabels: Record<string, string> = {
  running: '执行中', succeeded: '已完成', failed: '失败', cancelled: '已取消', published: '已发布',
};

function toolLabel(step?: string) {
  if (!step) return '准备操作';
  return toolLabels[step] || step;
}

function statusLabel(status?: string) {
  return statusLabels[status || ''] || status || '未知';
}

function formatTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '刚刚' : date.toLocaleTimeString();
}

interface OperationActivityPanelProps { accounts: Account[] }

export function OperationActivityPanel({ accounts }: OperationActivityPanelProps) {
  const { data: events, mutate } = useSWR('api/operations/events', api.getOperationEvents);
  const accountNames = useMemo(() => new Map(accounts.map((account) => [account.id, account.name])), [accounts]);
  const active = useMemo(() => {
    const latest = new Map<string, OperationEvent>();
    for (const event of events ?? []) {
      const key = `${event.trace_id}:${event.step}`;
      if (!latest.has(key)) latest.set(key, event);
    }
    return [...latest.values()].filter((event) => event.status === 'running');
  }, [events]);

  return <section className="ui-card overflow-hidden" data-testid="operation-activity">
    <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border p-4">
      <div className="flex items-center gap-2 text-sm font-semibold text-gray-900"><Activity className="h-4 w-4 text-brand-accent" />Agent 操作现场</div>
      <div className="flex items-center gap-3 text-[11px] text-gray-550">
        <span className="flex items-center gap-1.5"><CircleDot className="h-3.5 w-3.5 text-emerald-600" />{active.length} 项执行中</span>
        <button type="button" data-testid="operation-refresh" onClick={() => void mutate()} className="flex items-center gap-1 rounded-md border border-brand-border bg-brand-surface px-2 py-1 font-semibold text-gray-700">
          <RefreshCw className="h-3 w-3" />手动刷新
        </button>
      </div>
    </div>
    <div className="grid gap-px bg-brand-border lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
      <div className="bg-brand-surface p-4">
        <div className="mb-3 flex items-center gap-2 text-xs font-semibold text-gray-700"><Clock3 className="h-3.5 w-3.5" />当前步骤</div>
        <div className="space-y-2" data-testid="operation-active-list">
          {active.length ? active.slice(0, 4).map((event) => <div key={event.id} className="rounded-lg border border-brand-border bg-brand-bg p-3">
            <div className="flex items-center justify-between gap-3"><span className="truncate text-xs font-semibold text-gray-900">{toolLabel(event.step)}</span><span className="shrink-0 text-[10px] font-semibold text-brand-accent">执行中</span></div>
            <div className="mt-1 text-[10px] text-gray-500">{event.account_id ? accountNames.get(event.account_id) || `账号 #${event.account_id}` : event.task_id || '本地工具'}{event.platform_code ? ` · ${event.platform_code}` : ''}</div>
          </div>) : <div className="rounded-lg border border-dashed border-brand-border px-3 py-5 text-center text-xs text-gray-500">暂无正在执行的 Agent 操作</div>}
        </div>
      </div>
      <div className="bg-brand-surface p-4">
        <div className="mb-3 flex items-center justify-between gap-3"><span className="text-xs font-semibold text-gray-700">最近操作</span><span className="text-[10px] text-gray-500">只显示语义步骤</span></div>
        <div className="max-h-56 divide-y divide-brand-border overflow-y-auto" data-testid="operation-timeline">
          {events?.length ? events.slice(0, 20).map((event) => <div key={event.id} className="flex items-start gap-3 py-2.5 text-xs">
            <span className={`mt-1 h-1.5 w-1.5 shrink-0 rounded-full ${event.status === 'failed' ? 'bg-red-500' : event.status === 'running' ? 'bg-brand-accent' : 'bg-emerald-500'}`} />
            <div className="min-w-0 flex-1"><div className="flex flex-wrap items-center gap-x-2 gap-y-0.5"><span className="font-semibold text-gray-900">{toolLabel(event.step)}</span><span className="text-[10px] text-gray-500">{statusLabel(event.status)}</span>{event.duration_ms > 0 ? <span className="text-[10px] text-gray-500">{event.duration_ms}ms</span> : null}</div><div className="mt-0.5 truncate text-[10px] text-gray-500">{event.account_id ? accountNames.get(event.account_id) || `账号 #${event.account_id}` : event.task_id || '本地工具'}{event.error_code ? ` · ${event.error_code}` : ''}</div></div>
            <time className="shrink-0 text-[10px] text-gray-400">{formatTime(event.created_at)}</time>
          </div>) : <div className="py-8 text-center text-xs text-gray-500">等待 Agent 或工具开始操作</div>}
        </div>
      </div>
    </div>
    <div className="flex items-center gap-2 border-t border-brand-border bg-brand-bg/50 px-4 py-2.5 text-[10px] text-gray-550"><ShieldCheck className="h-3.5 w-3.5 text-emerald-600" />不记录工具参数、完整网址、CDP 包、Cookie、凭证或截图；截图只应由未来的用户显式证据操作创建。</div>
  </section>;
}
