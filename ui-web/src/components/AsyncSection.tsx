import type { ReactNode } from 'react';

/**
 * 统一的加载 / 空 / 错误三态容器。
 *
 * 背景：全仓只有 LocalLoadingState 一个加载组件，空态与错误态各页面自造，
 * 表现不一致；错误提示散落在 428 处 toast.show 里，且没有统一的重试入口。
 *
 * 用法：<AsyncSection loading={...} error={...} onRetry={reload} isEmpty={...}>{children}</AsyncSection>
 * 三态互斥：loading > error > empty > content（错误优先于空，避免掩盖故障）。
 */

type AsyncSectionProps = {
  loading: boolean;
  error?: string;
  /** 数据已加载且为空时展示空态；未加载完成不展示，避免闪空。 */
  isEmpty?: boolean;
  emptyText?: string;
  emptyHint?: string;
  /** 传入则错误态展示"重试"按钮。 */
  onRetry?: () => void;
  retryLabel?: string;
  loadingLabel?: string;
  /** 错误态里附带的请求号，便于用户报障时对照服务端日志。 */
  requestId?: string;
  className?: string;
  children: ReactNode;
};

export function AsyncSection({
  loading,
  error,
  isEmpty = false,
  emptyText = '暂无数据',
  emptyHint = '',
  onRetry,
  retryLabel = '重试',
  loadingLabel = '加载中',
  requestId,
  className = '',
  children,
}: AsyncSectionProps) {
  if (loading && !error) {
    return (
      <div
        role="status"
        aria-live="polite"
        aria-busy="true"
        className={`ui-panel flex items-center justify-center gap-2 px-4 py-8 text-xs text-brand-muted ${className}`}
      >
        <Spinner />
        <span>{loadingLabel}</span>
      </div>
    );
  }

  if (error) {
    return (
      <div
        role="alert"
        className={`ui-panel flex flex-col items-start gap-2 border-brand-danger/40 px-4 py-4 ${className}`}
      >
        <p className="text-xs font-semibold text-brand-danger">{error}</p>
        {requestId ? (
          <p className="text-[10px] text-brand-muted">请求号 {requestId}</p>
        ) : null}
        {onRetry ? (
          <button
            type="button"
            onClick={onRetry}
            className="ui-btn ui-btn-secondary mt-1 h-8 px-3 text-xs"
          >
            {retryLabel}
          </button>
        ) : null}
      </div>
    );
  }

  if (isEmpty) {
    return (
      <div className={`ui-panel px-4 py-8 text-center ${className}`}>
        <p className="text-xs font-semibold text-brand-fg-subtle">{emptyText}</p>
        {emptyHint ? <p className="mt-1 text-[10px] text-brand-muted">{emptyHint}</p> : null}
      </div>
    );
  }

  return <>{children}</>;
}

/** 与 LocalLoadingState 的三点动画一致，但更小、可内联在按钮与行内。 */
export function Spinner({ className = '' }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={`inline-flex items-center gap-0.5 ${className}`}
    >
      <span className="h-1 w-1 animate-pulse rounded-full bg-brand-muted [animation-delay:-0.3s]" />
      <span className="h-1 w-1 animate-pulse rounded-full bg-brand-muted [animation-delay:-0.15s]" />
      <span className="h-1 w-1 animate-pulse rounded-full bg-brand-muted" />
    </span>
  );
}

/** 行内错误提示，用于表单字段旁等不适合整块替换的场景。 */
export function InlineError({ message, className = '' }: { message: string; className?: string }) {
  if (!message) return null;
  return (
    <p role="alert" className={`text-[11px] text-brand-danger ${className}`}>
      {message}
    </p>
  );
}
