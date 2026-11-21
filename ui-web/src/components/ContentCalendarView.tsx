import { useMemo, useState } from 'react';
import useSWR from 'swr';
import { CalendarDays, ChevronLeft, ChevronRight, Clock, Loader2 } from 'lucide-react';
import { api, type PublishPlan, type PublishContentRecord } from '../api';

const WEEKDAYS = ['周一', '周二', '周三', '周四', '周五', '周六', '周日'];

type CalendarEntry = {
  kind: 'scheduled' | 'published';
  date: string;
  title: string;
  platform: string;
  detail: string;
};

// 排期时间是 UTC instant，日历格子是本地日期：用本地年月日归位，
// 否则东八区 00:00–07:59 的排期会落到前一天。
function localIsoDate(value: string | Date): string {
  const date = typeof value === 'string' ? new Date(value) : value;
  if (Number.isNaN(date.getTime())) return '';
  const pad = (part: number) => String(part).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function monthBounds(year: number, month: number): { first: Date; last: Date } {
  const first = new Date(Date.UTC(year, month, 1));
  const last = new Date(Date.UTC(year, month + 1, 0));
  return { first, last };
}

function isoDate(date: Date): string {
  return date.toISOString().slice(0, 10);
}

// 内容日历：把冻结的发布排期（publish plan targets 的 publish_at）与平台侧
// 内容记录（scheduled/published）落到当月网格上。数据全部来自既有只读 API，
// 本视图不新增任何后端面。
export function ContentCalendarView() {
  const today = new Date();
  const [cursor, setCursor] = useState({ year: today.getFullYear(), month: today.getMonth() });

  const { data: planSummaries = [], isLoading: plansLoading, error: plansError } = useSWR('api/publish-plans', api.getPublishPlans);
  const { data: records = [], error: recordsError } = useSWR('api/publish-content-records', api.getPublishContentRecords);

  const planIds = useMemo(
    () => planSummaries.map((plan) => plan.id),
    [planSummaries],
  );

  const { data: planBundle, isValidating: detailsLoading } = useSWR(
    ['api/publish-plans/details', planIds.join(',')],
    async () => {
      const results = await Promise.allSettled(planIds.map((id) => api.getPublishPlan(id)));
      return {
        plans: results
          .filter((entry): entry is PromiseFulfilledResult<PublishPlan> => entry.status === 'fulfilled')
          .map((entry) => entry.value),
        failed: results.filter((entry) => entry.status === 'rejected').length,
      };
    },
    { revalidateOnFocus: false },
  );
  const planDetails = planBundle?.plans;
  const failedPlans = planBundle?.failed ?? 0;

  const { first } = monthBounds(cursor.year, cursor.month);
  const gridStart = new Date(first);
  gridStart.setUTCDate(gridStart.getUTCDate() - ((first.getUTCDay() + 6) % 7));
  const gridDays = 42;

  const entries = useMemo((): CalendarEntry[] => {
    const list: CalendarEntry[] = [];
    for (const plan of planDetails ?? []) {
      for (const target of plan.snapshot?.targets ?? []) {
        if (!target.publish_at) continue;
        const date = localIsoDate(target.publish_at);
        if (!date) continue;
        list.push({
          kind: 'scheduled',
          date,
          title: target.title || plan.snapshot?.content?.title || `计划 #${plan.id}`,
          platform: target.platform_code,
          detail: plan.status,
        });
      }
    }
    for (const record of records as PublishContentRecord[]) {
      const date = localIsoDate(record.scheduled_at || '');
      if (!date) continue;
      list.push({
        kind: record.remote_state === 'published' ? 'published' : 'scheduled',
        date,
        title: record.title || record.account_name || `记录 #${record.id}`,
        platform: record.platform_code,
        detail: record.remote_state,
      });
    }
    return list;
  }, [planDetails, records]);

  const byDate = useMemo(() => {
    const map = new Map<string, CalendarEntry[]>();
    for (const entry of entries) {
      const list = map.get(entry.date) ?? [];
      list.push(entry);
      map.set(entry.date, list);
    }
    return map;
  }, [entries]);

  const monthStats = useMemo(() => {
    const prefix = `${cursor.year}-${String(cursor.month + 1).padStart(2, '0')}`;
    const monthEntries = entries.filter((entry) => entry.date.startsWith(prefix));
    return {
      scheduled: monthEntries.filter((entry) => entry.kind === 'scheduled').length,
      published: monthEntries.filter((entry) => entry.kind === 'published').length,
    };
  }, [entries, cursor]);

  const shiftMonth = (delta: number) => {
    setCursor((current) => {
      const next = new Date(Date.UTC(current.year, current.month + delta, 1));
      return { year: next.getUTCFullYear(), month: next.getUTCMonth() };
    });
  };

  const days = Array.from({ length: gridDays }, (_, index) => {
    const date = new Date(gridStart);
    date.setUTCDate(gridStart.getUTCDate() + index);
    return date;
  });
  const todayIso = localIsoDate(today);
  const currentMonth = cursor.month;

  return (
    <section data-testid="content-calendar" className="flex h-full min-h-0 flex-col overflow-y-auto bg-brand-bg">
      <header className="flex flex-wrap items-center gap-3 border-b border-brand-border px-5 py-4">
        <CalendarDays className="h-5 w-5 text-[var(--brand-accent)]" />
        <h2 className="text-sm font-bold text-gray-900">内容日历</h2>
        <span className="text-[11px] text-gray-500">冻结排期与平台侧记录按日归位；只读展示，发布动作仍在发布工作台执行。</span>
        <div className="ml-auto flex items-center gap-2">
          <button type="button" aria-label="上一月" onClick={() => shiftMonth(-1)} className="ui-icon-button"><ChevronLeft className="h-4 w-4" /></button>
          <strong data-testid="calendar-month" className="min-w-28 text-center text-sm text-gray-900">{cursor.year} 年 {cursor.month + 1} 月</strong>
          <button type="button" aria-label="下一月" onClick={() => shiftMonth(1)} className="ui-icon-button"><ChevronRight className="h-4 w-4" /></button>
          <button type="button" onClick={() => setCursor({ year: today.getFullYear(), month: today.getMonth() })} className="ui-btn">今天</button>
        </div>
      </header>

      <div className="flex flex-wrap items-center gap-4 px-5 py-2 text-[11px] text-gray-600">
        <span className="inline-flex items-center gap-1.5"><Clock className="h-3.5 w-3.5 text-brand-warning" />本月排期 <strong className="text-gray-900">{monthStats.scheduled}</strong></span>
        <span className="inline-flex items-center gap-1.5"><span className="h-2 w-2 rounded-full bg-emerald-500" />本月已发 <strong className="text-gray-900">{monthStats.published}</strong></span>
        {detailsLoading || plansLoading ? <span className="inline-flex items-center gap-1 text-gray-400"><Loader2 className="h-3 w-3 animate-spin" />加载中…</span> : null}
        {plansError || recordsError ? <span className="inline-flex items-center gap-1 font-semibold text-red-600">排期加载失败，当前网格不完整</span> : null}
        {failedPlans > 0 ? <span className="inline-flex items-center gap-1 font-semibold text-red-600">{failedPlans} 个计划的排期读取失败</span> : null}
      </div>

      <div className="grid grid-cols-7 gap-px border-t border-brand-border bg-brand-border px-5 pb-6 pt-1 text-[11px]">
        {WEEKDAYS.map((weekday) => (
          <div key={weekday} className="bg-brand-bg py-1.5 text-center font-semibold text-gray-500">{weekday}</div>
        ))}
        {days.map((date) => {
          const key = isoDate(date);
          const dayEntries = (byDate.get(key) ?? []).sort((left, right) => left.kind.localeCompare(right.kind));
          const inMonth = date.getUTCMonth() === currentMonth;
          const isToday = key === todayIso;
          return (
            <div
              key={key}
              data-testid={`calendar-day-${key}`}
              className={`min-h-24 bg-brand-surface p-1.5 ${inMonth ? '' : 'opacity-40'} ${isToday ? 'ring-1 ring-inset ring-[var(--brand-accent)]' : ''}`}
            >
              <div className={`mb-1 text-right text-[10px] font-semibold ${isToday ? 'text-[var(--brand-accent)]' : 'text-gray-400'}`}>{date.getUTCDate()}</div>
              <div className="space-y-1">
                {dayEntries.slice(0, 4).map((entry, index) => (
                  <div
                    key={`${key}-${index}`}
                    title={`${entry.title} · ${entry.platform} · ${entry.detail}`}
                    className={`truncate rounded px-1.5 py-0.5 text-[10px] ${entry.kind === 'published' ? 'bg-emerald-50 text-emerald-700' : 'bg-brand-warning/10 text-brand-warning'}`}
                  >
                    <span className="font-semibold">{entry.platform}</span> {entry.title}
                  </div>
                ))}
                {dayEntries.length > 4 ? <div className="px-1.5 text-[10px] text-gray-400">还有 {dayEntries.length - 4} 条…</div> : null}
              </div>
            </div>
          );
        })}
      </div>

      {(planDetails ?? []).length === 0 && !plansLoading && !plansError && failedPlans === 0 ? (
        <p className="px-5 pb-8 text-xs text-gray-400">暂无冻结的发布计划。在发布工作台冻结计划并填写发布时间后，排期会出现在这里。</p>
      ) : null}
    </section>
  );
}
