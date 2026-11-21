import React, { useEffect, useRef, useState } from 'react';
import { ExternalLink, Pause, Play, Search, X } from 'lucide-react';
import { api, type TrendRadarCreator, type TrendRadarCreatorPost } from '../../api';
import PostCoverThumb from './PostCoverThumb';
import { compactNumber, formatTime } from './format';

// 博主文件夹里的作品区：搜索 / 筛选 / 排序工具条 + 竖屏作品网格。
// 状态仍由编排层持有（快捷键与路径同步都在那里），这里只负责渲染与筛选。
// 一个博主可能有上百条作品，首屏只画前 24 条，滚动到网格底部再按批追加，
// 避免一次性渲染几百张竖屏大图 + 4 维指标造成明显卡顿。
const CREATOR_POST_FIRST_BATCH = 24;
const CREATOR_POST_BATCH_SIZE = 24;

const CreatorPostGrid: React.FC<{
  selectedCreator: TrendRadarCreator;
  creatorPostQuery: string;
  setCreatorPostQuery: (query: string) => void;
  creatorPostMediaFilter: 'all' | 'with_audio';
  setCreatorPostMediaFilter: (filter: 'all' | 'with_audio') => void;
  creatorPostSort: 'newest' | 'likes';
  setCreatorPostSort: (sort: 'newest' | 'likes') => void;
  creatorSearchRef: React.RefObject<HTMLInputElement | null>;
  activeAudio: { id: string } | null;
  audioPlaying: boolean;
  toggleAudio: (post: { id: string; title: string; audio_path?: string }, platform: string, userId: string) => void;
  setSelectedPostDetail: (post: TrendRadarCreatorPost) => void;
  openSource: (url?: string) => void;
  renderEmpty: (query: string) => React.ReactNode;
}> = ({
  selectedCreator,
  creatorPostQuery,
  setCreatorPostQuery,
  creatorPostMediaFilter,
  setCreatorPostMediaFilter,
  creatorPostSort,
  setCreatorPostSort,
  creatorSearchRef,
  activeAudio,
  audioPlaying,
  toggleAudio,
  setSelectedPostDetail,
  openSource,
  renderEmpty,
}) => {
  let list = selectedCreator.latest_posts;
  if (creatorPostQuery.trim()) {
    const q = creatorPostQuery.trim().toLowerCase();
    list = list.filter((p) => p.title.toLowerCase().includes(q) || p.id.includes(q));
  }
  if (creatorPostMediaFilter === 'with_audio') {
    list = list.filter((p) => Boolean(p.audio_path));
  }
  if (creatorPostSort === 'likes') {
    list = [...list].sort((a, b) => b.likes - a.likes);
  }

  // 用作品 id 序列 + 长度判断“列表变了”，而不是数组引用：
  // 同一实例收到新数组（换博主 / 搜索结果变化）时也能把已渲染数量复位。
  const listSignature = `${list.length}:${list.map((post) => post.id).join(',')}`;
  const [pagination, setPagination] = useState({ signature: listSignature, count: CREATOR_POST_FIRST_BATCH });
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  // 签名对不上说明列表刚换、复位副作用还没跑：先按首批画，避免多渲染一屏。
  const renderedCount = pagination.signature === listSignature ? pagination.count : CREATOR_POST_FIRST_BATCH;
  const rendered = list.slice(0, renderedCount);
  const hasMore = renderedCount < list.length;

  useEffect(() => {
    setPagination((prev) => (prev.signature === listSignature ? prev : { signature: listSignature, count: CREATOR_POST_FIRST_BATCH }));
  }, [listSignature]);

  useEffect(() => {
    if (!hasMore) return;
    const sentinel = sentinelRef.current;
    if (!sentinel) return;
    // 没有 IntersectionObserver 的旧环境直接全量渲染：宁可慢，也不白屏。
    if (typeof IntersectionObserver === 'undefined') {
      setPagination({ signature: listSignature, count: list.length });
      return;
    }
    const observer = new IntersectionObserver((entries) => {
      if (!entries.some((entry) => entry.isIntersecting)) return;
      setPagination((prev) => {
        const base = prev.signature === listSignature ? prev.count : CREATOR_POST_FIRST_BATCH;
        return { signature: listSignature, count: Math.min(base + CREATOR_POST_BATCH_SIZE, list.length) };
      });
    }, { rootMargin: '200px' });
    observer.observe(sentinel);
    // 卸载 / 列表重挂载 / 批次变化都会走到这里，observer 不会悬挂泄漏。
    return () => observer.disconnect();
  }, [hasMore, list.length, listSignature, renderedCount]);

  return (
  <>
    <div className="flex flex-wrap items-center justify-between gap-2.5 border-b border-brand-border px-4 py-2.5 bg-brand-surface">
      <div className="flex items-center gap-2 flex-1 min-w-[200px] max-w-md">
        <div className="relative flex-1">
          <Search className="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-brand-muted" />
          <input
            ref={creatorSearchRef}
            type="text"
            value={creatorPostQuery}
            onChange={(e) => setCreatorPostQuery(e.target.value)}
            placeholder={`在 ${selectedCreator.latest_posts.length} 条作品中搜索… 按 / 聚焦`}
            className="w-full rounded-lg border border-brand-border bg-brand-surface pl-8 pr-16 py-1 text-xs text-brand-fg placeholder:text-brand-muted outline-none focus:border-brand-accent transition-colors"
          />
          {creatorPostQuery ? (
            <button
              type="button"
              onClick={() => setCreatorPostQuery('')}
              className="absolute right-2 top-2 text-brand-muted hover:text-brand-fg"
              title="清空（Esc）"
            >
              <X className="h-3 w-3" />
            </button>
          ) : (
            <kbd className="absolute right-2 top-1.5 rounded border border-brand-border bg-black/[0.03] px-1 text-[10px] leading-4 text-brand-muted">/</kbd>
          )}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2 text-xs">
        {/* 分段控件：一块会移动的底板滑到新位置，而不是旧的熄灭新的点亮 */}
        <div className="relative grid grid-cols-2 rounded-lg border border-brand-border bg-brand-surface-raised p-0.5 text-[11px]">
          <span
            aria-hidden
            data-testid="creator-filter-slider"
            className="absolute inset-y-0.5 left-0.5 w-[calc(50%-0.125rem)] rounded-md bg-brand-surface shadow-xs transition-transform duration-200"
            style={{ transform: creatorPostMediaFilter === 'with_audio' ? 'translateX(100%)' : 'translateX(0)', transitionTimingFunction: 'var(--motion-ease-standard)' }}
          />
          <button
            type="button"
            onClick={() => setCreatorPostMediaFilter('all')}
            className={`relative z-10 rounded-md px-2.5 py-1 font-medium transition-colors ${creatorPostMediaFilter === 'all' ? 'text-brand-fg font-semibold' : 'text-brand-muted hover:text-brand-fg'}`}
          >
            全部 ({selectedCreator.latest_posts.length})
          </button>
          <button
            type="button"
            onClick={() => setCreatorPostMediaFilter('with_audio')}
            className={`relative z-10 rounded-md px-2.5 py-1 font-medium transition-colors ${creatorPostMediaFilter === 'with_audio' ? 'text-brand-fg font-semibold' : 'text-brand-muted hover:text-brand-fg'}`}
          >
            有原声 ({selectedCreator.latest_posts.filter((p) => p.audio_path).length})
          </button>
        </div>
        <select
          value={creatorPostSort}
          onChange={(e) => setCreatorPostSort(e.target.value as 'newest' | 'likes')}
          className="rounded-lg border border-brand-border bg-brand-surface px-2.5 py-1 text-xs text-brand-fg outline-none focus:border-brand-accent"
        >
          <option value="newest">最新发布</option>
          <option value="likes">点赞最高</option>
        </select>
      </div>
    </div>

    {list.length === 0 ? renderEmpty(creatorPostQuery) : (
      <div
        key={`${creatorPostMediaFilter}-${creatorPostSort}-${creatorPostQuery}`}
        data-testid="creator-post-grid"
        data-rendered-count={rendered.length}
        className="grid grid-cols-2 gap-3 p-4 pt-2.5 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 animate-fade-in"
      >
        {rendered.map((post) => {
          const coverSrc = post.cover_path
            ? api.getTrendRadarCreatorMediaURL(selectedCreator.platform, selectedCreator.user_id, post.cover_path)
            : post.cover_url;
          const isAudioActive = activeAudio?.id === post.id && audioPlaying;
          return (
            <div
              key={post.id || post.url || post.title}
              data-testid={`creator-post-${post.id}`}
              onClick={() => setSelectedPostDetail(post)}
              className="group relative overflow-hidden rounded-xl border border-brand-border bg-brand-surface text-left transition hover:border-brand-accent hover:shadow-xs active:scale-[0.98] flex flex-col cursor-pointer"
            >
              <div className="relative">
                <PostCoverThumb
                  src={coverSrc}
                  fallbackURL={post.cover_url}
                  title={post.title}
                  hasLocalCover={Boolean(post.cover_path)}
                  durationMs={post.duration_ms}
                />
                {/* 单独的跳转按钮：在平台打开原视频 */}
                {post.url && (
                  <button
                    type="button"
                    data-testid={`post-open-external-${post.id}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      openSource(post.url);
                    }}
                    className="absolute right-1.5 top-1.5 hidden rounded-full bg-black/60 p-1 text-white backdrop-blur-xs transition hover:bg-black/80 group-hover:block"
                    title="单独在平台打开原视频 ↗"
                  >
                    <ExternalLink className="h-3 w-3" />
                  </button>
                )}
              </div>
              <div className="flex flex-1 flex-col justify-between gap-1.5 p-2.5">
                <h3
                  className="line-clamp-2 text-[11px] font-bold leading-snug text-brand-fg group-hover:text-brand-accent"
                  title={post.title}
                >
                  {post.title}
                </h3>

                {/* 紧凑 4 维真实指标：优先展示有数据的维度（赞/藏/评/转；若有播放量则展示播/赞/评/藏） */}
                <div className="grid grid-cols-4 gap-0.5 rounded-md bg-black/[0.03] py-1 px-1 text-center font-mono text-[9px]">
                  {post.play > 0 ? (
                    <div title={`播放量: ${post.play.toLocaleString()}`}>
                      <span className="text-brand-muted text-[8px]">播</span>
                      <span className="font-semibold text-brand-fg ml-0.5">{compactNumber(post.play)}</span>
                    </div>
                  ) : (
                    <div title={`点赞数: ${post.likes.toLocaleString()}`}>
                      <span className="text-brand-muted text-[8px]">赞</span>
                      <span className="font-semibold text-brand-fg ml-0.5">{compactNumber(post.likes)}</span>
                    </div>
                  )}
                  {post.play > 0 ? (
                    <div title={`点赞数: ${post.likes.toLocaleString()}`}>
                      <span className="text-brand-muted text-[8px]">赞</span>
                      <span className="font-semibold text-brand-fg ml-0.5">{compactNumber(post.likes)}</span>
                    </div>
                  ) : (
                    <div title={`收藏数: ${post.collects.toLocaleString()}`}>
                      <span className="text-brand-muted text-[8px]">藏</span>
                      <span className="font-semibold text-brand-fg ml-0.5">{compactNumber(post.collects)}</span>
                    </div>
                  )}
                  <div title={`评论数: ${(post.comments || 0).toLocaleString()}`}>
                    <span className="text-brand-muted text-[8px]">评</span>
                    <span className="font-semibold text-brand-fg ml-0.5">{compactNumber(post.comments || 0)}</span>
                  </div>
                  {post.play > 0 ? (
                    <div title={`收藏数: ${post.collects.toLocaleString()}`}>
                      <span className="text-brand-muted text-[8px]">藏</span>
                      <span className="font-semibold text-brand-fg ml-0.5">{compactNumber(post.collects)}</span>
                    </div>
                  ) : (
                    <div title={`分享数: ${post.shares.toLocaleString()}`}>
                      <span className="text-brand-muted text-[8px]">转</span>
                      <span className="font-semibold text-brand-fg ml-0.5">{compactNumber(post.shares)}</span>
                    </div>
                  )}
                </div>

                <div className="flex items-center justify-between text-[10px] text-brand-muted">
                  <span className="truncate">{formatTime(post.observed_at)}</span>
                  {/* 播放量可用时四宫格是 播/赞/评/藏，这里补上分享，避免维度缺口 */}
                  {post.play > 0 && post.shares > 0 && (
                    <span title={`分享: ${post.shares.toLocaleString()}`}>转 {compactNumber(post.shares)}</span>
                  )}
                </div>

                {post.audio_path && (
                  <button
                    type="button"
                    data-testid={`post-audio-${post.id}`}
                    onClick={(e) => {
                      e.stopPropagation();
                      toggleAudio(post, selectedCreator.platform, selectedCreator.user_id);
                    }}
                    className={`inline-flex w-full items-center justify-center gap-1 rounded-md px-2 py-1 text-[10px] font-semibold transition ${
                                          isAudioActive
                                            ? 'bg-brand-accent text-white shadow-xs'
                                            : 'bg-black/[0.04] text-brand-fg hover:bg-brand-accent hover:text-white'
                                        }`}
                    title="点击试听本地原声音频"
                  >
                    {isAudioActive ? (
                      <Pause className="h-2.5 w-2.5 animate-pulse" />
                    ) : (
                      <Play className="h-2.5 w-2.5" />
                    )}
                    {isAudioActive ? '播放中' : '试听原声'}
                  </button>
                )}
              </div>
            </div>
          );
        })}
        {hasMore && (
          // 哨兵：进入视口就追加下一批。本身只占 1px，不参与卡片栅格视觉。
          <div
            ref={sentinelRef}
            data-testid="creator-post-grid-sentinel"
            aria-hidden
            className="col-span-full h-px w-full"
          />
        )}
      </div>
    )}
  </>
  );
};

export default CreatorPostGrid;
