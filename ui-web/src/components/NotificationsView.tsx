import useSWR from 'swr';
import { Bell, CheckCheck, CircleAlert, CircleCheck, Info } from 'lucide-react';
import { api, type Notification } from '../api';
import { useUIStore } from '../store';

function iconFor(level: Notification['level']) {
  if (level === 'error') return <CircleAlert className="h-4 w-4 text-red-500" />;
  if (level === 'warn') return <CircleAlert className="h-4 w-4 text-brand-warning" />;
  if (level === 'info') return <CircleCheck className="h-4 w-4 text-emerald-500" />;
  return <Info className="h-4 w-4 text-gray-400" />;
}

function formatTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false });
}

export function NotificationsView() {
  const { toast, setActiveTab, requestPublishRecord } = useUIStore();
  const { data: items, error, mutate } = useSWR('api/notifications', api.getNotifications);
  const unread = items?.filter((item) => !item.read).length ?? 0;

  const markRead = async (id: number) => {
    try {
      await api.markNotificationRead(id);
      await mutate();
    } catch {
      toast.show('标记通知失败', 'error');
    }
  };

  const markAllRead = async () => {
    try {
      await api.markAllNotificationsRead();
      await mutate();
    } catch {
      toast.show('标记全部已读失败', 'error');
    }
  };

  const openItem = async (item: Notification) => {
    if (!item.read) await markRead(item.id);
    if ((item.related_type === 'publish_job' || item.related_type === 'publish_plan') && item.related_id > 0) {
      requestPublishRecord({ relatedType: item.related_type, relatedID: item.related_id });
      setActiveTab('records');
    }
  };

  return (
    <section className="mx-auto w-full max-w-4xl space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-base font-bold text-gray-900">通知中心</h3>
          <p className="mt-1 text-xs text-gray-500">登录异常、平台失败和发布核验结果均以本地数据库为准。</p>
        </div>
        <button
          type="button"
          disabled={unread === 0}
          onClick={markAllRead}
          className="inline-flex items-center gap-1.5 rounded-lg bg-brand-accent px-3 py-2 text-xs font-semibold text-brand-on-accent disabled:cursor-not-allowed disabled:opacity-45"
        >
          <CheckCheck className="h-4 w-4" /> 全部已读
        </button>
      </div>
      {error ? <p className="rounded-lg bg-red-50 p-4 text-sm text-red-700">通知加载失败。</p> : null}
      {!error && items?.length === 0 ? (
        <div className="rounded-xl border border-dashed border-brand-border bg-brand-surface p-12 text-center">
          <Bell className="mx-auto h-7 w-7 text-gray-300" />
          <p className="mt-3 text-sm font-medium text-gray-500">暂无通知</p>
        </div>
      ) : null}
      <div className="overflow-hidden rounded-xl border border-brand-border bg-brand-surface shadow-sm">
        {items?.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={() => void openItem(item)}
            className={`flex w-full gap-3 border-b border-brand-border px-4 py-4 text-left last:border-b-0 ${item.read ? 'opacity-65' : 'bg-brand-accent/[0.03]'}`}
          >
            <span className="mt-0.5 shrink-0">{iconFor(item.level)}</span>
            <span className="min-w-0 flex-1">
              <span className="flex items-center justify-between gap-3">
                <strong className="truncate text-sm text-gray-900">{item.title}</strong>
                <time className="shrink-0 text-[11px] text-gray-400">{formatTime(item.created_at)}</time>
              </span>
              {item.body ? <span className="mt-1 block text-xs leading-5 text-gray-600">{item.body}</span> : null}
            </span>
            {!item.read ? <span className="mt-1 h-2 w-2 shrink-0 rounded-full bg-brand-accent" /> : null}
          </button>
        ))}
      </div>
    </section>
  );
}
