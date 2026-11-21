import React, { useCallback, useEffect, useMemo, useState } from 'react';
import useSWR from 'swr';
import { Activity, AlertTriangle, CheckCircle2, ChevronRight, Loader2, X } from 'lucide-react';
import { api, type JobEvent, type PublishJobSummary } from '../api';

/**
 * 后台任务抽屉：把任务与事件流从一级导航收进一个常驻抽屉。
 *
 * 之前「系统日志」和「运行监控」各占一个导航位，用户浏览内容时也被日志挤占；
 * 抽屉只在需要时展开，角标只提示"有东西在跑或出错"，不打断当前工作。
 */

const OPEN_KEY = 'alpha.backgroundDrawer.open';
const REFRESH_INTERVAL_MS = 8000;

type DrawerTab = 'running' | 'history';

function readOpenPreference(): boolean {
  if (typeof window === 'undefined') return false;
  return window.localStorage.getItem(OPEN_KEY) === '1';
}

const TERMINAL_STATUSES = new Set(['succeeded', 'failed', 'cancelled', 'canceled']);

function isRunning(job: PublishJobSummary): boolean {
  return !TERMINAL_STATUSES.has(String(job.status ?? '').toLowerCase());
}

function statusTone(status?: string): string {
  const value = String(status ?? '').toLowerCase();
  if (value === 'failed') return 'text-rose-600';
  if (value === 'succeeded') return 'text-emerald-600';
  if (TERMINAL_STATUSES.has(value)) return 'text-gray-500';
  return 'text-brand-accent';
}

function formatClock(value?: string): string {
  if (!value) return '';
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? ''
    : date.toLocaleTimeString('zh-CN', { hour12: false });
}

export const BackgroundDrawer: React.FC = () => {
  const [open, setOpen] = useState(readOpenPreference);
  const [tab, setTab] = useState<DrawerTab>('running');

  useEffect(() => {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(OPEN_KEY, open ? '1' : '0');
    }
  }, [open]);

  // 抽屉关闭时不轮询，避免后台空转；打开后按固定间隔刷新。
  const { data: jobs } = useSWR<PublishJobSummary[]>(open ? 'api/jobs/list' : null, api.getJobs, {
    refreshInterval: REFRESH_INTERVAL_MS,
  });
  const { data: events } = useSWR<JobEvent[]>(open && tab === 'history' ? 'api/jobs/events/all' : null, api.getAllJobEvents, {
    refreshInterval: REFRESH_INTERVAL_MS,
  });

  const running = useMemo(() => (jobs ?? []).filter(isRunning), [jobs]);
  const failedCount = useMemo(
    () => (jobs ?? []).filter((job) => String(job.status ?? '').toLowerCase() === 'failed').length,
    [jobs],
  );

  const close = useCallback(() => setOpen(false), []);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open]);

  return (
    <>
      <button
        type="button"
        data-testid="background-drawer-trigger"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        title="后台任务与日志"
        className="fixed bottom-4 right-4 z-40 flex items-center gap-2 rounded-full border border-brand-border bg-brand-surface px-3.5 py-2 text-[11px] font-bold text-gray-700 shadow-lg transition hover:border-brand-accent/50"
      >
        {running.length > 0 ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin text-brand-accent" />
        ) : failedCount > 0 ? (
          <AlertTriangle className="h-3.5 w-3.5 text-rose-500" />
        ) : (
          <Activity className="h-3.5 w-3.5 text-gray-400" />
        )}
        <span>
          后台
          {running.length > 0 ? ` · ${running.length} 进行中` : failedCount > 0 ? ` · ${failedCount} 失败` : ''}
        </span>
      </button>

      {open ? (
        <div
          role="dialog"
          aria-modal="false"
          aria-label="后台任务与日志"
          data-testid="background-drawer"
          className="fixed bottom-16 right-4 z-40 flex h-[min(520px,70vh)] w-[min(480px,calc(100vw-2rem))] flex-col overflow-hidden rounded-xl border border-brand-border bg-brand-surface shadow-2xl"
        >
          <div className="flex items-center justify-between border-b border-brand-border px-4 py-3">
            <div className="flex gap-1">
              <button
                type="button"
                data-testid="background-drawer-tab-running"
                onClick={() => setTab('running')}
                className={`rounded-md px-2.5 py-1 text-[11px] font-bold transition ${
                  tab === 'running' ? 'bg-brand-accent/10 text-brand-accent' : 'text-gray-500 hover:text-gray-800'
                }`}
              >
                进行中{running.length > 0 ? ` (${running.length})` : ''}
              </button>
              <button
                type="button"
                data-testid="background-drawer-tab-history"
                onClick={() => setTab('history')}
                className={`rounded-md px-2.5 py-1 text-[11px] font-bold transition ${
                  tab === 'history' ? 'bg-brand-accent/10 text-brand-accent' : 'text-gray-500 hover:text-gray-800'
                }`}
              >
                最近事件
              </button>
            </div>
            <button type="button" onClick={close} aria-label="收起后台抽屉" className="text-gray-400 hover:text-gray-700">
              <X className="h-4 w-4" />
            </button>
          </div>

          <div className="flex-1 overflow-y-auto px-2 py-2">
            {tab === 'running' ? (
              running.length === 0 ? (
                <p data-testid="background-drawer-empty" className="px-2 py-8 text-center text-[11px] text-gray-500">
                  当前没有进行中的任务。
                </p>
              ) : (
                <ul className="space-y-1">
                  {running.map((job) => (
                    <li key={job.id} className="flex items-center gap-2 rounded-lg px-2 py-2 hover:bg-brand-bg">
                      <ChevronRight className="h-3 w-3 shrink-0 text-gray-300" />
                      <span className="min-w-0 flex-1 truncate text-[11px] text-gray-800">
                        {job.platform_code || '任务'} #{job.id}
                      </span>
                      <span className={`shrink-0 text-[10px] font-bold ${statusTone(job.status)}`}>
                        {job.current_step || job.status || 'running'}
                      </span>
                    </li>
                  ))}
                </ul>
              )
            ) : (events ?? []).length === 0 ? (
              <p className="px-2 py-8 text-center text-[11px] text-gray-500">还没有事件记录。</p>
            ) : (
              <ul className="space-y-1">
                {(events ?? []).map((event, index) => (
                  <li key={`${event.id ?? index}`} className="rounded-lg px-2 py-1.5 hover:bg-brand-bg">
                    <div className="flex items-center gap-2">
                      {String(event.level ?? '').toUpperCase() === 'ERROR' ? (
                        <AlertTriangle className="h-3 w-3 shrink-0 text-rose-500" />
                      ) : (
                        <CheckCircle2 className="h-3 w-3 shrink-0 text-gray-300" />
                      )}
                      <span className="min-w-0 flex-1 truncate text-[11px] text-gray-800">
                        {event.event_type || event.step || '事件'}
                      </span>
                      <span className="shrink-0 font-mono text-[10px] text-gray-400">
                        {formatClock(event.created_at)}
                      </span>
                    </div>
                    {event.error_code ? (
                      <p className="mt-0.5 pl-5 font-mono text-[10px] text-rose-500">{event.error_code}</p>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      ) : null}
    </>
  );
};
