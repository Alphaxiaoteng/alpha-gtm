import React from 'react';
import { createPortal } from 'react-dom';
import {
  Activity,
  Bookmark,
  Copy,
  ExternalLink,
  Eye,
  Film,
  FolderOpen,
  Heart,
  MessageCircle,
  Pause,
  Play,
  Share2,
  Sparkles,
  Volume2,
  X,
} from 'lucide-react';
import { api, type TrendRadarCreatorPost } from '../../api';
import { useUIStore } from '../../store';
import PostCoverThumb from './PostCoverThumb';
import { compactNumber, formatAudioTime, formatTime } from './format';

// 作品居中详情弹窗：展示完整 6 大数据维度矩阵、大图、原声播放与独立外跳按钮
const PostDetailModal: React.FC<{
  post: TrendRadarCreatorPost;
  platform: string;
  userId: string;
  onClose: () => void;
  onToggleAudio: (post: { id: string; title: string; audio_path?: string }, platform: string, userId: string) => void;
  isAudioPlaying: boolean;
  activeAudioId?: string;
  audioTime: number;
  audioDuration: number;
  seekAudio: (fraction: number) => void;
  onOpenExternal: (url?: string) => void;
  onOpenFolder: () => void;
}> = ({ post, platform, userId, onClose, onToggleAudio, isAudioPlaying, activeAudioId, audioTime, audioDuration, seekAudio, onOpenExternal, onOpenFolder }) => {
  const { toast } = useUIStore();
  const coverSrc = post.cover_path
    ? api.getTrendRadarCreatorMediaURL(platform, userId, post.cover_path)
    : post.cover_url;
  const isThisAudioPlaying = activeAudioId === post.id && isAudioPlaying;
  const totalEngagement = post.likes + (post.comments || 0) + post.collects + post.shares;
  const engagementRate = post.play > 0 ? `${((totalEngagement / post.play) * 100).toFixed(1)}%` : null;
  const collectRate = post.likes > 0 ? `${((post.collects / post.likes) * 100).toFixed(1)}%` : null;
  const durationSec = audioDuration > 0 ? audioDuration : (post.duration_ms ? post.duration_ms / 1000 : 0);

  return createPortal(
    <div
      data-testid="post-detail-modal-overlay"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm animate-fade-in"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        data-testid="post-detail-modal"
        className="relative flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-brand-border bg-brand-surface shadow-2xl"
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-brand-border px-4 py-3">
          <div className="flex items-center gap-2">
            <Film className="h-4 w-4 text-brand-accent" />
            <h2 className="text-xs font-bold text-brand-fg">作品详细数据</h2>
            <button
              type="button"
              onClick={() => {
                void navigator.clipboard.writeText(post.id);
                toast.show(`已复制作品 ID: ${post.id}`, 'success');
              }}
              className="inline-flex items-center gap-1 rounded bg-black/[0.04] px-1.5 py-0.5 font-mono text-[10px] text-brand-muted hover:bg-black/10 hover:text-brand-fg transition"
              title="点击复制作品 ID"
            >
              <span>ID: {post.id}</span>
              <Copy className="h-2.5 w-2.5" />
            </button>
          </div>
          <button
            type="button"
            data-testid="post-detail-close"
            onClick={onClose}
            className="rounded-lg p-1 text-brand-muted hover:bg-black/5 hover:text-brand-fg transition"
            title="关闭（Esc）"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Body */}
        <div className="grid flex-1 grid-cols-1 gap-4 overflow-y-auto p-4 sm:grid-cols-12 sm:gap-5">
          {/* Left Column: Vertical Cover & Audio Preview */}
          <div className="flex flex-col gap-2.5 sm:col-span-5">
            <div className="relative aspect-[9/16] w-full overflow-hidden rounded-xl bg-black">
              <PostCoverThumb
                src={coverSrc}
                fallbackURL={post.cover_url}
                title={post.title}
                hasLocalCover={Boolean(post.cover_path)}
                durationMs={post.duration_ms}
              />
            </div>

            {/* 音频试听播放器 */}
            {post.audio_path && (
              isThisAudioPlaying ? (
                <div className="flex flex-col gap-1.5 rounded-lg border border-brand-border bg-black/[0.02] p-2">
                  <div className="flex items-center justify-between text-[10px]">
                    <span className="font-semibold text-brand-accent flex items-center gap-1">
                      <Volume2 className="h-3 w-3 animate-pulse" />
                      正在试听原声
                    </span>
                    <button
                      type="button"
                      onClick={() => onToggleAudio(post, platform, userId)}
                      className="rounded p-0.5 text-brand-muted hover:text-brand-fg"
                      title="暂停试听"
                    >
                      <Pause className="h-3.5 w-3.5" />
                    </button>
                  </div>
                  {/* 可拖动进度条 */}
                  <div className="flex items-center gap-1.5 text-[9px] text-brand-muted tabular-nums">
                    <span>{formatAudioTime(audioTime)}</span>
                    <div
                      role="slider"
                      tabIndex={0}
                      aria-label="播放进度"
                      aria-valuemin={0}
                      aria-valuemax={Math.floor(durationSec) || 0}
                      aria-valuenow={Math.floor(audioTime)}
                      onClick={(e) => {
                        const rect = e.currentTarget.getBoundingClientRect();
                        if (rect.width > 0) seekAudio((e.clientX - rect.left) / rect.width);
                      }}
                      className="relative flex h-3 flex-1 cursor-pointer items-center outline-none"
                    >
                      <div className="h-1 w-full overflow-hidden rounded-full bg-black/[0.08]">
                        <div
                          className="h-full rounded-full bg-brand-accent transition-[width] duration-100 ease-linear"
                          style={{ width: `${durationSec > 0 ? Math.min(100, (audioTime / durationSec) * 100) : 0}%` }}
                        />
                      </div>
                    </div>
                    <span>{formatAudioTime(durationSec)}</span>
                  </div>
                </div>
              ) : (
                <button
                  type="button"
                  onClick={() => onToggleAudio(post, platform, userId)}
                  className="inline-flex w-full items-center justify-center gap-1.5 rounded-lg py-2 text-xs font-semibold bg-black/[0.05] text-brand-fg hover:bg-brand-accent hover:text-white transition"
                >
                  <Play className="h-3.5 w-3.5" />
                  试听本地声音频
                </button>
              )
            )}
          </div>

          {/* Right Column: Title, Metadata, 6 Metrics, Actions */}
          <div className="flex flex-col justify-between gap-3 sm:col-span-7">
            <div className="flex flex-col gap-3">
              {/* Full Title / Description */}
              <div>
                <span className="text-[10px] font-semibold text-brand-muted">文案标题</span>
                <p className="mt-1 rounded-lg border border-brand-border bg-black/[0.015] p-2.5 text-xs font-medium leading-relaxed text-brand-fg select-text">
                  {post.title || '（该作品无文案标题）'}
                </p>
              </div>

              {/* Metadata Bar */}
              <div className="grid grid-cols-2 gap-2 text-[11px]">
                <div className="rounded-lg border border-brand-border bg-brand-surface p-2">
                  <span className="text-[10px] text-brand-muted">发布时间</span>
                  <p className="mt-0.5 font-medium text-brand-fg">{post.observed_at ? formatTime(post.observed_at) : '未知'}</p>
                </div>
                <div className="rounded-lg border border-brand-border bg-brand-surface p-2">
                  <span className="text-[10px] text-brand-muted">视频时长</span>
                  <p className="mt-0.5 font-medium text-brand-fg">
                    {post.duration_ms && post.duration_ms > 0 ? formatAudioTime(post.duration_ms / 1000) : '未知'}
                  </p>
                </div>
              </div>

              {/* 6 Data Dimensions Grid: 充实有洞察，拒绝无效暂无 */}
              <div>
                <span className="text-[10px] font-semibold text-brand-muted">数据维度</span>
                <div className="mt-1.5 grid grid-cols-3 gap-2">
                  {post.play > 0 ? (
                    <div className="rounded-lg border border-brand-border bg-brand-surface p-2 text-center" title={`播放量: ${post.play.toLocaleString()}`}>
                      <div className="flex items-center justify-center gap-1 text-[10px] text-brand-muted">
                        <Eye className="h-3 w-3" />
                        <span>播放量</span>
                      </div>
                      <p className="mt-1 font-bold text-xs text-brand-fg tabular-nums">
                        {compactNumber(post.play)}
                      </p>
                    </div>
                  ) : (
                    <div className="rounded-lg border border-brand-border bg-brand-surface p-2 text-center" title={`干货收藏率: 收藏数 / 点赞数 = ${collectRate ?? '0%'}`}>
                      <div className="flex items-center justify-center gap-1 text-[10px] text-amber-500">
                        <Sparkles className="h-3 w-3" />
                        <span>干货收藏率</span>
                      </div>
                      <p className="mt-1 font-bold text-xs text-amber-600 tabular-nums">
                        {collectRate ?? '0%'}
                      </p>
                    </div>
                  )}

                  <div className="rounded-lg border border-brand-border bg-brand-surface p-2 text-center" title={`点赞数: ${post.likes.toLocaleString()}`}>
                    <div className="flex items-center justify-center gap-1 text-[10px] text-rose-500">
                      <Heart className="h-3 w-3 fill-rose-500" />
                      <span>点赞数</span>
                    </div>
                    <p className="mt-1 font-bold text-xs text-brand-fg tabular-nums">
                      {compactNumber(post.likes)}
                    </p>
                  </div>

                  <div className="rounded-lg border border-brand-border bg-brand-surface p-2 text-center" title={`评论数: ${(post.comments || 0).toLocaleString()}`}>
                    <div className="flex items-center justify-center gap-1 text-[10px] text-blue-500">
                      <MessageCircle className="h-3 w-3" />
                      <span>评论数</span>
                    </div>
                    <p className="mt-1 font-bold text-xs text-brand-fg tabular-nums">
                      {compactNumber(post.comments || 0)}
                    </p>
                  </div>

                  <div className="rounded-lg border border-brand-border bg-brand-surface p-2 text-center" title={`收藏数: ${post.collects.toLocaleString()}`}>
                    <div className="flex items-center justify-center gap-1 text-[10px] text-amber-500">
                      <Bookmark className="h-3 w-3 fill-amber-500" />
                      <span>收藏数</span>
                    </div>
                    <p className="mt-1 font-bold text-xs text-brand-fg tabular-nums">
                      {compactNumber(post.collects)}
                    </p>
                  </div>

                  <div className="rounded-lg border border-brand-border bg-brand-surface p-2 text-center" title={`分享数: ${post.shares.toLocaleString()}`}>
                    <div className="flex items-center justify-center gap-1 text-[10px] text-emerald-500">
                      <Share2 className="h-3 w-3" />
                      <span>分享数</span>
                    </div>
                    <p className="mt-1 font-bold text-xs text-brand-fg tabular-nums">
                      {compactNumber(post.shares)}
                    </p>
                  </div>

                  <div className="rounded-lg border border-brand-border bg-brand-surface p-2 text-center" title={`互动总量: ${totalEngagement.toLocaleString()}`}>
                    <div className="flex items-center justify-center gap-1 text-[10px] text-indigo-500">
                      <Activity className="h-3 w-3" />
                      <span>{engagementRate ? '互动率' : '互动总量'}</span>
                    </div>
                    <p className="mt-1 font-bold text-xs text-brand-fg tabular-nums">
                      {engagementRate ?? compactNumber(totalEngagement)}
                    </p>
                  </div>
                </div>
                {post.play === 0 && (
                  <p className="mt-1.5 text-[9px] text-brand-muted">
                    * 抖音公域对所有人隐藏播放量；已完整统计点赞、收藏、评论、分享全部公开互动数据。
                  </p>
                )}
              </div>
            </div>

            {/* Bottom Actions: Standalone External Open & Open Folder */}
            <div className="flex items-center gap-2 pt-2 border-t border-brand-border">
              {post.url && (
                <button
                  type="button"
                  data-testid="post-detail-open-external"
                  onClick={() => onOpenExternal(post.url)}
                  className="ui-btn ui-btn-primary flex-1 justify-center py-2 text-xs"
                  title="单独在平台打开原视频网页"
                >
                  <ExternalLink className="h-3.5 w-3.5" />
                  在平台打开原视频 ↗
                </button>
              )}
              <button
                type="button"
                onClick={onOpenFolder}
                className="ui-btn justify-center py-2 text-xs"
                title="在系统文件管理器中打开本地作品文件夹"
              >
                <FolderOpen className="h-3.5 w-3.5" />
                本地文件夹
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
};

export default PostDetailModal;
