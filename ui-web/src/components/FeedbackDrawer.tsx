import { useCallback, useEffect, useState } from 'react';
import { Copy, RefreshCw, Send, X } from 'lucide-react';
import { api, type FeedbackItem, type FeedbackStatus } from '../api';

const FEEDBACK_DRAFT_STORAGE_KEY = 'alpha-nexus-egolite:feedback-draft';

const STATUS_STYLE: Record<FeedbackStatus, { dot: string; text: string }> = {
  待处理: { dot: 'bg-brand-warning/20', text: 'text-brand-warning' },
  处理中: { dot: 'bg-brand-muted', text: 'text-brand-muted' },
  已完成: { dot: 'bg-emerald-500', text: 'text-emerald-700' },
  不采纳: { dot: 'bg-gray-400', text: 'text-gray-500' },
};

function formatTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? '刚刚'
    : date.toLocaleString([], { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

function feedbackError(error: unknown) {
  const code = error instanceof Error ? error.message : '';
  if (code.includes('FEEDBACK_NOT_CONFIGURED')) {
    return '开发者配置未完成，内容仅保存在本机，尚未提交。';
  }
  if (code.includes('FEEDBACK_SCHEMA_MISMATCH')) {
    return '反馈表结构需要开发者更新，内容已保留。';
  }
  if (code.includes('FEEDBACK_REMOTE_UNAVAILABLE')) {
    return '反馈服务暂时无法连接，内容已保留，请稍后重试。';
  }
  if (code.includes('LOCAL_STORE_FAILED')) {
    return '本机记录保存失败，内容已保留，请重试。';
  }
  if (code.includes('FEEDBACK_INVALID_INPUT')) return '请输入 2–2000 个字。';
  return '暂时无法提交，内容已保留，请稍后重试。';
}

function readFeedbackDraft() {
  try {
    return window.localStorage.getItem(FEEDBACK_DRAFT_STORAGE_KEY) ?? '';
  } catch {
    return '';
  }
}

function persistFeedbackDraft(value: string) {
  try {
    if (value) window.localStorage.setItem(FEEDBACK_DRAFT_STORAGE_KEY, value);
    else window.localStorage.removeItem(FEEDBACK_DRAFT_STORAGE_KEY);
  } catch {
    // Draft persistence is best effort; keep the in-memory draft usable.
  }
}

export function FeedbackDrawer({
  open,
  currentModule,
  sidebarHidden,
  onClose,
}: {
  open: boolean;
  currentModule: string;
  sidebarHidden: boolean;
  onClose: () => void;
}) {
  const [items, setItems] = useState<FeedbackItem[]>([]);
  const [draft, setDraft] = useState('');
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [configured, setConfigured] = useState(false);
  const [message, setMessage] = useState('');

  const load = useCallback(async (refresh = true) => {
    setLoading(true);
    try {
      const result = await api.getFeedback(refresh);
      setItems(result.items ?? []);
      setConfigured(result.configured === true);
      setMessage(
        result.configured !== true
          ? '开发者反馈通道未启用，内容仅保存在本机。'
          : result.sync_error
            ? '状态暂未同步，稍后可刷新。'
            : '',
      );
    } catch {
      setConfigured(false);
      setMessage('记录暂时无法同步，请稍后刷新。');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!open) return undefined;
    setDraft(readFeedbackDraft());
    void load(true);
    const timer = window.setInterval(() => void load(true), 30_000);
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', closeOnEscape);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener('keydown', closeOnEscape);
    };
  }, [load, onClose, open]);

  const length = Array.from(draft.trim()).length;
  const valid = length >= 2 && length <= 2000;

  const updateDraft = (value: string) => {
    setDraft(value);
    persistFeedbackDraft(value);
  };

  const submit = async () => {
    if (!valid || submitting || !configured) return;
    setSubmitting(true);
    setMessage('');
    try {
      await api.submitFeedback(draft.trim(), currentModule);
      setDraft('');
      persistFeedbackDraft('');
      await load(true);
    } catch (error) {
      setMessage(feedbackError(error));
    } finally {
      setSubmitting(false);
    }
  };

  const copyDraft = async () => {
    if (!valid || submitting) return;
    setSubmitting(true);
    setMessage('');
    try {
      if (!navigator.clipboard?.writeText) throw new Error('CLIPBOARD_UNAVAILABLE');
      await navigator.clipboard.writeText(draft.trim());
      setMessage('内容已复制到剪贴板，仅保存在本机，尚未提交。');
    } catch {
      setMessage('复制失败，内容仍保存在本机草稿中，请重试。');
    } finally {
      setSubmitting(false);
    }
  };

  if (!open) return null;

  return (
    <section
      data-testid="feedback-drawer"
      role="dialog"
      aria-label="想法与 Bug 反馈"
      aria-busy={loading || submitting}
      className="fixed bottom-4 top-12 z-[75] flex flex-col overflow-hidden rounded-2xl border border-brand-border bg-brand-surface shadow-2xl"
      style={{
        left: sidebarHidden ? '0.75rem' : 'calc(var(--ui-sidebar-w) + 0.75rem)',
        width: sidebarHidden ? 'min(26rem, calc(100vw - 1.5rem))' : 'min(26rem, calc(100vw - var(--ui-sidebar-w) - 1.5rem))',
      }}
    >
      <header className="flex items-center justify-between gap-3 border-b border-brand-border px-5 py-4">
        <h2 className="text-base font-bold tracking-tight text-gray-900">想法</h2>
        <div className="flex items-center gap-1">
          <button
            type="button"
            aria-label="刷新反馈状态"
            disabled={loading}
            onClick={() => void load(true)}
            className="rounded-lg p-2 text-gray-400 transition-colors hover:bg-black/[0.05] hover:text-gray-700 disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
          <button
            type="button"
            aria-label="关闭想法"
            onClick={onClose}
            className="rounded-lg p-2 text-gray-400 transition-colors hover:bg-black/[0.05] hover:text-gray-700"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </header>

      <div
        data-testid="feedback-history"
        className="min-h-0 flex-1 overflow-y-auto bg-brand-bg/40 px-4 py-4"
      >
        {items.length ? (
          <ol className="space-y-3">
            {items.map((item) => {
              const style = STATUS_STYLE[item.status] ?? STATUS_STYLE.待处理;
              return (
                <li
                  key={item.feedback_id}
                  className="rounded-2xl border border-brand-border bg-brand-surface px-4 py-3.5 shadow-sm"
                >
                  <p className="whitespace-pre-wrap break-words text-[13px] font-medium leading-6 text-gray-800">
                    {item.content}
                  </p>
                  <div className="mt-2.5 flex items-center gap-2 text-[11px]">
                    <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${style.dot}`} aria-hidden="true" />
                    <span className={`font-semibold ${style.text}`}>{item.status}</span>
                    <span className="text-gray-300" aria-hidden="true">·</span>
                    <time className="text-gray-400">{formatTime(item.created_at)}</time>
                  </div>
                  {item.developer_reply ? (
                    <div className="mt-3 rounded-xl bg-brand-bg px-3 py-2.5">
                      <div className="text-[10px] font-semibold text-brand-accent">开发者回复</div>
                      <p className="mt-1 whitespace-pre-wrap break-words text-[11px] leading-5 text-gray-600">
                        {item.developer_reply}
                      </p>
                    </div>
                  ) : null}
                </li>
              );
            })}
          </ol>
        ) : (
          <div className="flex h-full min-h-44 items-center justify-center px-8 text-center text-xs leading-6 text-gray-400">
            提交后可在这里查看处理进度。
          </div>
        )}
      </div>

      <form
        data-testid="feedback-composer"
        className="border-t border-brand-border bg-brand-surface p-4"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <label htmlFor="feedback-draft" className="sr-only">说说你的想法</label>
        <div className="rounded-2xl border border-brand-border bg-brand-bg p-3 transition-colors focus-within:border-brand-accent">
          <textarea
            id="feedback-draft"
            value={draft}
            onChange={(event) => updateDraft(event.target.value)}
            rows={4}
            maxLength={2000}
            placeholder="说说你的想法、建议或遇到的 Bug…"
            className="feedback-draft-input w-full resize-none text-[13px] leading-6 text-gray-900 outline-none placeholder:text-gray-400"
          />
          <div className="mt-2 flex items-center justify-between gap-3">
            <span className="text-[10px] tabular-nums text-gray-400">{length}/2000</span>
            <button
              type={configured ? 'submit' : 'button'}
              disabled={!valid || submitting}
              onClick={configured ? undefined : () => void copyDraft()}
              aria-label={configured ? '提交反馈' : '复制反馈内容'}
              className="inline-flex shrink-0 items-center gap-1.5 rounded-xl bg-brand-accent px-3.5 py-2 text-xs font-semibold text-brand-on-accent transition-opacity disabled:cursor-not-allowed disabled:opacity-40"
            >
              {configured ? <Send className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              {submitting ? (configured ? '提交中' : '复制中') : configured ? '提交' : '复制内容'}
            </button>
          </div>
        </div>
        <p className="mt-2 text-[10px] text-gray-500">
          {configured ? '匿名提交，内容将发送给 Alpha-Nexus 开发者' : '开发者反馈通道未启用，内容仅保存在本机，尚未提交。'}
        </p>
        {message ? (
          <p role="status" aria-live="polite" className="mt-1 text-[10px] text-brand-warning">
            {message}
          </p>
        ) : null}
      </form>
    </section>
  );
}
