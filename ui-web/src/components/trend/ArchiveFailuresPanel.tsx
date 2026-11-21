import React, { useEffect, useMemo } from 'react';
import { createPortal } from 'react-dom';
import { AlertTriangle, Copy, FolderOpen, RefreshCw, X } from 'lucide-react';
import type { TrendRadarArchiveFailure } from '../../api';
import { useUIStore } from '../../store';
import { formatTime } from './format';

// 一次最多渲染 50 条：全量归档失败可能上千条，整表渲染会卡住弹窗。
const MAX_RENDERED_FAILURES = 50;

const failureKindLabels: Record<string, string> = {
  cover: '封面',
  audio: '音频',
};

// 全量归档目前只有抖音，所以可以直接由 aweme_id 还原作品页地址。
// 失败项大多是封面/音频下载失败，作品本身在平台上仍然能看。
function douyinVideoURL(awemeID: string): string {
  return `https://www.douyin.com/video/${encodeURIComponent(awemeID)}`;
}

// 归档失败台账弹窗：谁失败、为什么失败、怎么补。
// 「重试全部」不是新接口——媒体下载的跳过条件是「文件存在且 size>0」，
// 再跑一次归档就会自动重下失败的封面/音频。
const ArchiveFailuresPanel: React.FC<{
  total: number;
  failures: TrendRadarArchiveFailure[];
  onClose: () => void;
  onRetry: () => void;
  onOpenFolder?: () => void;
  onOpenSource?: (url: string) => void;
}> = ({ total, failures, onClose, onRetry, onOpenFolder, onOpenSource }) => {
  const { toast } = useUIStore();
  const visible = useMemo(() => failures.slice(0, MAX_RENDERED_FAILURES), [failures]);
  const hidden = Math.max(0, (total || failures.length) - visible.length);

  useEffect(() => {
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [onClose]);

  const copyAwemeID = (awemeID: string) => {
    void navigator.clipboard.writeText(awemeID);
    toast.show(`已复制作品 ID: ${awemeID}`, 'success');
  };

  return createPortal(
    <div
      data-testid="archive-failures-overlay"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm animate-fade-in"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        data-testid="archive-failures-panel"
        className="relative flex max-h-[85vh] w-full max-w-xl flex-col overflow-hidden rounded-2xl border border-brand-border bg-brand-surface shadow-2xl"
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-brand-border px-4 py-3">
          <div className="flex items-center gap-2">
            <AlertTriangle className="h-4 w-4 text-brand-warning" />
            <h2 className="text-xs font-bold text-brand-fg">归档失败 {total} 项</h2>
          </div>
          <button
            type="button"
            data-testid="archive-failures-close"
            onClick={onClose}
            className="rounded-lg p-1 text-brand-muted hover:bg-black/5 hover:text-brand-fg transition"
            title="关闭（Esc）"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <p className="border-b border-brand-border bg-black/[0.015] px-4 py-2 text-[10px] leading-relaxed text-brand-muted">
          封面与音频下载失败时不会写入作品索引，重试会再跑一次归档：已有文件跳过，缺失的自动重下。
        </p>

        {/* Body */}
        <div data-testid="archive-failures-list" className="flex-1 overflow-y-auto">
          {visible.length === 0 ? (
            <p className="px-4 py-6 text-center text-xs text-brand-muted">没有失败记录</p>
          ) : (
            <ul className="divide-y divide-brand-border/70">
              {visible.map((failure, index) => (
                <li
                  key={`${failure.aweme_id}-${failure.kind}-${index}`}
                  data-testid={`archive-failure-row-${index}`}
                  className="flex flex-col gap-1 px-4 py-2.5"
                >
                  {/* 标题放最前：只给一个 ID 的话用户根本认不出是哪条作品。 */}
                  <div className="flex items-start justify-between gap-2">
                    {failure.title ? (
                      <button
                        type="button"
                        data-testid={`archive-failure-open-${index}`}
                        onClick={() => onOpenSource?.(douyinVideoURL(failure.aweme_id))}
                        className="line-clamp-2 text-left text-[11px] font-semibold leading-snug text-brand-fg hover:text-brand-accent transition"
                        title="在平台打开原视频 ↗"
                      >
                        {failure.title}
                      </button>
                    ) : (
                      <span className="text-[11px] leading-snug text-brand-muted">未记录标题的作品</span>
                    )}
                    <span className="shrink-0 rounded bg-amber-50 px-1.5 py-0.5 text-[10px] font-semibold text-amber-700">
                      {failureKindLabels[failure.kind] ?? failure.kind}
                    </span>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <button
                      type="button"
                      data-testid={`archive-failure-copy-${index}`}
                      onClick={() => copyAwemeID(failure.aweme_id)}
                      className="inline-flex items-center gap-1 rounded bg-black/[0.04] px-1.5 py-0.5 font-mono text-[10px] text-brand-muted hover:bg-black/10 hover:text-brand-fg transition"
                      title="点击复制作品 ID"
                    >
                      <span>ID: {failure.aweme_id}</span>
                      <Copy className="h-2.5 w-2.5" />
                    </button>
                    <span className="text-[10px] text-brand-muted">
                      {failure.attempted_at ? formatTime(failure.attempted_at) : '时间未知'}
                    </span>
                  </div>
                  <p className="text-[11px] leading-relaxed text-brand-fg break-words">{failure.reason || '未记录原因'}</p>
                </li>
              ))}
            </ul>
          )}
          {hidden > 0 && (
            <p data-testid="archive-failures-truncated" className="border-t border-brand-border px-4 py-2 text-[10px] text-brand-muted">
              还有 {hidden} 条未显示（一次最多显示 {MAX_RENDERED_FAILURES} 条）
            </p>
          )}
        </div>

        {/* Bottom Actions */}
        <div className="flex items-center gap-2 border-t border-brand-border px-4 py-3">
          <button
            type="button"
            data-testid="archive-failures-retry-all"
            onClick={onRetry}
            className="ui-btn ui-btn-primary flex-1 justify-center py-2 text-xs"
            title="再跑一次归档，只补下缺失的封面与音频"
          >
            <RefreshCw className="h-3.5 w-3.5" />
            重试全部
          </button>
          {onOpenFolder && (
            <button
              type="button"
              data-testid="archive-failures-open-folder"
              onClick={onOpenFolder}
              className="ui-btn justify-center py-2 text-xs"
              title="在系统文件管理器中打开博主文件夹"
            >
              <FolderOpen className="h-3.5 w-3.5" />
              打开本地文件夹
            </button>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
};

export default ArchiveFailuresPanel;
