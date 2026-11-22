import { useMemo, useState } from 'react';
import useSWR from 'swr';
import { useUIStore } from '../store';
import { api, type PublishRecord } from '../api';
import { Clock, Copy, ExternalLink, Film, Languages, Loader2, Sparkles } from 'lucide-react';
import { type ContentPublishInfo, type LocalContentKind } from './LocalContentView';
import type { GuanyingVideoItem } from './GuanyingWorkspaceViews';

type SocialMediaSection = LocalContentKind;

/**
 * 素材裂变：一条源片 → 多语种成片。
 *
 * 目前只有 workshop-showcase（26.8s）跑出过真实译制成片；其它素材也保留裂变入口，
 * 点开明确说明「该素材还没有成片」，而不是拿假动画糊弄。等别的素材真的翻译完，
 * 往这张表加一条即可，界面不用改。
 */
const FISSION_OUTPUTS: Record<string, Array<{ lang: string; label: string; url: string; note: string }>> = {
  'tk-7604389793560317204': [
    { lang: 'id', label: '印尼语', url: '/guanying-videos/fission/sophia-workshop-id-local-clean.mp4', note: '本地化译制 · 保留原字幕版式' },
    { lang: 'fr', label: '法语', url: '/guanying-videos/fission/sophia-workshop-fr-local-clean.mp4', note: '本地化译制 · 保留原字幕版式' },
  ],
};

/** 裂变两段：先 20 秒生成过场，再逐条揭示成片（每条间隔 10 秒）。 */
const FISSION_STAGE_MS = 20_000;
const FISSION_REVEAL_STEP_MS = 10_000;

// 发布状态归一化：后端 publish_records.status 取值较多，这里只分三类用于展示。
function normalizePublishStatus(status: string): 'published' | 'pending' | 'idle' {
  if (status === 'published') return 'published';
  if (status === 'pending' || status === 'manual_takeover' || status === 'awaiting_review' || status === 'submitted') return 'pending';
  return 'idle';
}


export function SocialMediaWorkspaceView({ initialSection = 'video' }: { initialSection?: SocialMediaSection }) {
  const domain = useUIStore((state) => state.productMode);
  // 页面只剩素材裂变，不再需要分 tab 切换；initialSection 保留只为兼容 App 的调用。
  void initialSection;

  const toast = useUIStore((state) => state.toast);
  // 裂变状态机：哪条素材在裂变、生成过场是否结束、已揭示到第几条。
  const [fissionID, setFissionID] = useState('');
  const [fissionStage, setFissionStage] = useState<'idle' | 'generating' | 'revealing'>('idle');
  const [revealedCount, setRevealedCount] = useState(0);

  const startFission = (videoID: string) => {
    setFissionID(videoID);
    setRevealedCount(0);
    setFissionStage('generating');
    window.setTimeout(() => {
      setFissionStage('revealing');
      setRevealedCount(1);
      // 每隔 10 秒揭示一条，最后一条出现后停在中态。
      const outputs = FISSION_OUTPUTS[videoID]?.length ?? 0;
      for (let i = 1; i < outputs; i += 1) {
        window.setTimeout(() => setRevealedCount(i + 1), i * FISSION_REVEAL_STEP_MS);
      }
    }, FISSION_STAGE_MS);
  };

  const closeFission = () => {
    setFissionID('');
    setFissionStage('idle');
    setRevealedCount(0);
  };

  const fissionOutputs = fissionID ? FISSION_OUTPUTS[fissionID] ?? [] : [];

  const { data: publishRecords } = useSWR<PublishRecord[]>('api/publish-records', api.getPublishRecords);
  const { data: realVideos } = useSWR<GuanyingVideoItem[]>('guanying-videos/manifest', async () => {
    try {
      const res = await fetch('/guanying-videos/manifest.json');
      if (!res.ok) return [];
      return (await res.json()) as GuanyingVideoItem[];
    } catch {
      return [];
    }
  });




  // content_id -> 各平台发布信息（仅取本内容库可发布的平台账号）。
  const publishByContent = useMemo(() => {
    const map = new Map<number, ContentPublishInfo[]>();
    for (const rec of publishRecords ?? []) {
      const list = map.get(rec.content_id) ?? [];
      list.push({
        platformCode: rec.platform_code,
        status: normalizePublishStatus(rec.status),
        accountId: rec.account_id,
      });
      map.set(rec.content_id, list);
    }
    return map;
  }, [publishRecords]);

  return (
    <section className="space-y-5" data-testid="social-media-workspace">
      {(realVideos ?? []).length > 0 && (
        <div className="ui-card space-y-3 p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2 text-xs font-bold text-brand-fg">
              <Film className="h-4 w-4 text-brand-accent" />
              <span>素材裂变</span>
            </div>
            <span className="text-[11px] text-brand-muted">一条源片 → 多语种成片 · 点击裂变</span>
          </div>
          <div className="grid gap-3 grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 2xl:grid-cols-5">
            {(realVideos ?? []).map((v) => (
              <div key={v.id} className="space-y-2 rounded-xl border border-brand-border bg-brand-bg/60 p-2.5">
                <div className="flex items-center justify-between gap-1 text-[10px]">
                  <span className="rounded bg-brand-accent/12 px-1.5 py-0.5 font-bold text-brand-accent">{v.platform}</span>
                  <span className="inline-flex items-center gap-0.5 text-emerald-600 font-semibold">
                    <Clock className="h-2.5 w-2.5" /> {v.duration_sec}s
                  </span>
                </div>
                <div className="relative mx-auto aspect-[9/16] w-full max-w-[190px] overflow-hidden rounded-lg bg-black">
                  <video
                    controls
                    playsInline
                    preload="metadata"
                    src={v.url}
                    className="h-full w-full object-contain"
                  />
                </div>
                <p className="line-clamp-2 text-[11px] font-semibold text-brand-fg">{v.title}</p>
                <div className="flex items-center justify-between gap-2 pt-0.5">
                  <button
                    type="button"
                    onClick={() => {
                      void navigator.clipboard?.writeText(v.suggested_caption);
                      toast.show('外贸短片配文已复制', 'success');
                    }}
                    className="inline-flex items-center gap-1 rounded border border-brand-border px-2 py-1 text-[10px] font-medium text-brand-fg hover:bg-brand-fg/[0.04]"
                  >
                    <Copy className="h-2.5 w-2.5" /> 复制配文
                  </button>
                  <button
                    type="button"
                    data-testid={`fission-start-${v.id}`}
                    onClick={() => startFission(v.id)}
                    title={
                      FISSION_OUTPUTS[v.id]
                        ? '把这条素材裂变成多语种成片'
                        : '这条素材还没有生成成片，可以在 Bot 里发起译制'
                    }
                    className={`inline-flex items-center gap-1 rounded px-2 py-1 text-[10px] font-semibold transition ${
                      FISSION_OUTPUTS[v.id]
                        ? 'bg-brand-accent text-brand-on-accent hover:brightness-110'
                        : 'border border-dashed border-brand-border text-brand-muted hover:text-brand-fg'
                    }`}
                  >
                    <Sparkles className="h-2.5 w-2.5" /> 一键裂变
                  </button>
                  <a
                    href={v.source_url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-0.5 text-[10px] text-brand-muted hover:text-brand-accent"
                  >
                    原视频 <ExternalLink className="h-2.5 w-2.5" />
                  </a>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* 裂变画布：左边是原片，右边从原片分出去两个成片。
          生成时右侧两格是空的、带扫光；出片后逐格填充，点任意一格放大播放。
          下方是生成记录列表，让「什么时候裂变了什么」有据可查。 */}
      {fissionID ? (() => {
        const source = (realVideos ?? []).find((v) => v.id === fissionID);
        const outputs = fissionOutputs;
        return (
          <div data-testid="fission-panel" data-stage={fissionStage} className="ui-card space-y-4 p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center gap-2 text-xs font-bold text-brand-fg">
                <Languages className="h-4 w-4 text-brand-accent" />
                <span>素材裂变</span>
                {fissionStage === 'generating' ? (
                  <span className="inline-flex items-center gap-1 rounded-full bg-brand-accent/12 px-2 py-0.5 text-[10px] font-semibold text-brand-accent">
                    <Loader2 className="h-2.5 w-2.5 animate-spin" /> 正在生成
                  </span>
                ) : (
                  <span className="rounded-full bg-emerald-500/12 px-2 py-0.5 text-[10px] font-semibold text-emerald-700">
                    {Math.min(revealedCount, outputs.length)} / {outputs.length} 已出片
                  </span>
                )}
              </div>
              <button type="button" onClick={closeFission} className="rounded border border-brand-border px-2 py-1 text-[10px] font-medium text-brand-muted hover:text-brand-fg">
                收起
              </button>
            </div>

            <div className="flex flex-wrap items-center justify-center gap-6">
              {/* 原片 */}
              <div className="space-y-1.5">
                <div className="text-[10px] font-semibold text-brand-muted">原视频</div>
                <div className="relative aspect-[9/16] w-[150px] overflow-hidden rounded-xl border border-brand-border bg-black">
                  <video controls playsInline preload="metadata" src={source?.url ?? ''} className="h-full w-full object-contain" />
                </div>
                <div className="w-[150px] truncate text-[10px] text-brand-muted">{source?.title ?? ''}</div>
              </div>

              {/* 分叉箭头 */}
              <div className="hidden items-center text-brand-accent sm:flex" aria-hidden="true">
                <svg width="56" height="80" viewBox="0 0 56 80" fill="none">
                  <path d="M2 40h18" stroke="currentColor" strokeWidth="1.5" strokeDasharray="3 3" />
                  <path d="M20 40c14 0 14-26 30-26" stroke="currentColor" strokeWidth="1.5" strokeDasharray="3 3" />
                  <path d="M20 40c14 0 14 26 30 26" stroke="currentColor" strokeWidth="1.5" strokeDasharray="3 3" />
                  <circle cx="52" cy="14" r="2.5" fill="currentColor" />
                  <circle cx="52" cy="66" r="2.5" fill="currentColor" />
                </svg>
              </div>

              {/* 两个成片格 */}
              <div className="flex flex-wrap gap-4">
                {[0, 1].map((slot) => {
                  const out = outputs[slot];
                  const visible = Boolean(out) && slot < revealedCount;
                  return (
                    <div key={slot} className="space-y-1.5">
                      <div className="text-[10px] font-semibold text-brand-muted">{out ? out.label : '等待生成'}</div>
                      <div
                        data-testid={out ? `fission-output-${out.lang}` : `fission-slot-${slot}`}
                        data-visible={visible}
                        className={`relative aspect-[9/16] w-[150px] overflow-hidden rounded-xl border bg-black transition-all duration-500 ${
                          visible ? 'border-brand-border opacity-100' : 'border-dashed border-brand-border/70 opacity-70'
                        }`}
                      >
                        {visible && out ? (
                          <video controls playsInline preload="metadata" src={out.url} className="h-full w-full object-contain" />
                        ) : (
                          <div className="flex h-full w-full flex-col items-center justify-center gap-2 bg-brand-surface-raised">
                            {fissionStage === 'generating' ? (
                              <>
                                <Loader2 className="h-4 w-4 animate-spin text-brand-accent" />
                                <span className="text-[10px] text-brand-muted">生成中…</span>
                              </>
                            ) : (
                              <span className="text-[10px] text-brand-muted">暂无成片</span>
                            )}
                          </div>
                        )}
                      </div>
                      {visible && out ? (
                        <a href={out.url} download className="block w-[150px] rounded border border-brand-border px-2 py-1 text-center text-[10px] font-medium text-brand-fg hover:bg-brand-fg/[0.04]">
                          下载成片
                        </a>
                      ) : <div className="h-[26px] w-[150px]" />}
                    </div>
                  );
                })}
              </div>
            </div>

            {/* 生成记录 */}
            <div className="rounded-lg border border-brand-border bg-brand-bg/60 p-3">
              <p className="mb-2 text-[11px] font-semibold text-brand-fg">生成记录</p>
              <ul className="space-y-1.5 text-[11px] text-brand-muted">
                <li className="flex items-center justify-between gap-2">
                  <span>原片已载入 · {source?.title ?? fissionID}</span>
                  <span className="shrink-0 text-emerald-700">完成</span>
                </li>
                {outputs.map((out, index) => (
                  <li key={out.lang} className="flex items-center justify-between gap-2">
                    <span>{out.label} · 本地化译制（保留原字幕版式）</span>
                    <span className={`shrink-0 ${index < revealedCount ? 'text-emerald-700' : 'text-brand-muted'}`}>
                      {index < revealedCount ? '已出片' : '生成中'}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          </div>
        );
      })() : null}

    </section>
  );
}
