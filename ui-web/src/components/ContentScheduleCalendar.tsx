import { useMemo, useState } from 'react';
import { CalendarDays, Clock3, X } from 'lucide-react';
import useSWR from 'swr';
import { api, type ContentItem } from '../api';
import { ContentCover } from './ContentCover';

type Range = 'today' | 'week' | 'all';

function scheduled(contents: ContentItem[]) {
  return contents.flatMap((content) => {
    const date = new Date(content.planned_at || '');
    return Number.isNaN(date.getTime()) ? [] : [{ content, date }];
  }).sort((left, right) => left.date.getTime() - right.date.getTime());
}

function sameLocalDay(left: Date, right: Date) {
  return left.getFullYear() === right.getFullYear()
    && left.getMonth() === right.getMonth()
    && left.getDate() === right.getDate();
}

export function ContentScheduleCalendar({ onClose }: { onClose: () => void }) {
  const { data: contents } = useSWR('api/contents', api.getContents);
  const [range, setRange] = useState<Range>('week');
  const items = useMemo(() => {
    const now = new Date();
    const weekEnd = new Date(now);
    weekEnd.setDate(weekEnd.getDate() + 7);
    return scheduled(contents ?? []).filter(({ date }) => {
      if (range === 'today') return sameLocalDay(date, now);
      if (range === 'week') return date >= now && date <= weekEnd;
      return true;
    });
  }, [contents, range]);

  return (
    <section className="fixed right-5 top-24 z-[70] w-[min(28rem,calc(100vw-2.5rem))] rounded-xl border border-brand-border bg-brand-surface p-4 shadow-2xl" aria-label="内容排期" data-testid="content-schedule-calendar">
      <header className="flex items-center justify-between gap-3">
        <div><h3 className="flex items-center gap-2 text-sm font-bold text-gray-900"><CalendarDays className="h-4 w-4 text-brand-accent" />内容排期</h3><p className="mt-1 text-[11px] text-gray-500">简单查看提醒；不会自动发布。</p></div>
        <button type="button" onClick={onClose} className="rounded-md p-1.5 text-gray-500 hover:bg-black/[0.05]" aria-label="关闭内容排期"><X className="h-4 w-4" /></button>
      </header>
      <div className="mt-4 grid grid-cols-3 gap-1 rounded-lg bg-brand-bg p-1">
        {([['today', '今天'], ['week', '未来 7 天'], ['all', '全部']] as const).map(([value, label]) => <button key={value} type="button" aria-pressed={range === value} onClick={() => setRange(value)} className={`rounded-md px-2 py-2 text-xs font-semibold ${range === value ? 'bg-white text-brand-accent shadow-sm' : 'text-gray-500 hover:text-gray-900'}`}>{label}</button>)}
      </div>
      {items.length ? <ol className="mt-3 max-h-[30rem] space-y-2 overflow-y-auto">
        {items.map(({ content, date }) => <li key={content.id} className="flex items-center gap-3 rounded-lg border border-brand-border p-2">
          <ContentCover content={content} className="h-14 w-20 shrink-0 rounded-md" />
          <span className="min-w-0 flex-1">
            <strong className="block truncate text-xs text-gray-900">{content.title || '未命名内容'}</strong>
            <span className="mt-1 flex items-center gap-1 text-[11px] text-gray-500"><Clock3 className="h-3 w-3" />{date.toLocaleString([], { month: 'long', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</span>
          </span>
        </li>)}
      </ol> : <div className="mt-3 rounded-lg border border-dashed border-brand-border py-10 text-center text-xs text-gray-500">这个时间范围没有排期。</div>}
    </section>
  );
}
