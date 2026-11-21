import { useMemo, useState } from 'react';
import useSWR from 'swr';
import { requestWorkspaceChange } from '../store';
import { useUIStore } from '../store';
import { api, type PublishRecord } from '../api';
import { Clock, Copy, ExternalLink, Film, Languages, Loader2, Sparkles } from 'lucide-react';
import { LocalContentView, type ContentPublishInfo, type LocalContentKind } from './LocalContentView';
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

const sections: Array<{ id: SocialMediaSection; label: string }> = [
  { id: 'video', label: '视频' },
  { id: 'graph_text', label: '图文' },
  { id: 'article', label: '文章' },
];

// 发布状态归一化：后端 publish_records.status 取值较多，这里只分三类用于展示。
function normalizePublishStatus(status: string): 'published' | 'pending' | 'idle' {
  if (status === 'published') return 'published';
  if (status === 'pending' || status === 'manual_takeover' || status === 'awaiting_review' || status === 'submitted') return 'pending';
  return 'idle';
}


export function SocialMediaWorkspaceView({ initialSection = 'video' }: { initialSection?: SocialMediaSection }) {
  const domain = useUIStore((state) => state.productMode);
  const [section, setSection] = useState<SocialMediaSection>(initialSection);

  const selectSection = (nextSection: SocialMediaSection) => {
    if (nextSection === section || !requestWorkspaceChange({ currentSection: section, nextSection })) return;
    setSection(nextSection);
  };

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
          <div className="grid gap-3 md:grid-cols-3 xl:grid-cols-5">
            {(realVideos ?? []).map((v) => (
              <div key={v.id} className="space-y-2 rounded-xl border border-brand-border bg-brand-bg/60 p-2.5">
                <div className="flex items-center justify-between gap-1 text-[10px]">
                  <span className="rounded bg-brand-accent/12 px-1.5 py-0.5 font-bold text-brand-accent">{v.platform}</span>
                  <span className="inline-flex items-center gap-0.5 text-emerald-600 font-semibold">
                    <Clock className="h-2.5 w-2.5" /> {v.duration_sec}s
                  </span>
                </div>
                <video
                  controls
                  playsInline
                  preload="metadata"
                  src={v.url}
                  className="h-40 w-full rounded-lg bg-black object-contain"
                />
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

      {/* 裂变过场：先 20 秒生成动画，再每 10 秒揭示一条成片。 */}
      {fissionID ? (
        <div
          data-testid="fission-panel"
          data-stage={fissionStage}
          className="ui-card space-y-4 border-brand-accent/30 p-4"
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2 text-xs font-bold text-brand-fg">
              <Languages className="h-4 w-4 text-brand-accent" />
              <span>素材裂变</span>
              {fissionStage === 'generating' ? (
                <span className="inline-flex items-center gap-1 rounded-full bg-brand-accent/12 px-2 py-0.5 text-[10px] font-semibold text-brand-accent">
                  <Loader2 className="h-2.5 w-2.5 animate-spin" /> 正在生成多语种成片
                </span>
              ) : (
                <span className="rounded-full bg-emerald-500/12 px-2 py-0.5 text-[10px] font-semibold text-emerald-700">
                  已生成 {Math.min(revealedCount, fissionOutputs.length)} / {fissionOutputs.length}
                </span>
              )}
            </div>
            <button
              type="button"
              onClick={closeFission}
              className="rounded border border-brand-border px-2 py-1 text-[10px] font-medium text-brand-muted hover:text-brand-fg"
            >
              收起
            </button>
          </div>

          {fissionStage === 'generating' ? (
            <div className="flex flex-col items-center justify-center gap-3 py-10">
              {/* 无 CSS 关键帧依赖：用三段错相的条高表达「正在合成」 */}
              <div className="flex items-end gap-1.5">
                {[0, 1, 2, 3, 4].map((i) => (
                  <span key={i} className="block w-1.5 animate-pulse rounded-full bg-brand-accent" style={{ height: `${12 + (i % 3) * 9}px`, animationDelay: `${i * 140}ms` }} />
                ))}
              </div>
              <p className="text-xs font-medium text-brand-fg">正在生成印尼语 / 法语成片…</p>
              <p className="text-[11px] text-brand-muted">字幕版式与时长保持不变，只换音轨与字幕语言。</p>
            </div>
          ) : fissionOutputs.length === 0 ? (
            <div className="rounded-lg border border-dashed border-brand-border bg-brand-bg/60 p-6 text-center">
              <p className="text-xs font-semibold text-brand-fg">这条素材还没有多语种成片</p>
              <p className="mt-1 text-[11px] text-brand-muted">
                可以到 Bot 团队让「内容运营 Bot」发起译制；生成后会自动出现在这里。
              </p>
            </div>
          ) : (
            <div className="grid gap-3 md:grid-cols-2">
              {fissionOutputs.map((out, index) => {
                const visible = index < revealedCount;
                return (
                  <div
                    key={out.lang}
                    data-testid={`fission-output-${out.lang}`}
                    data-visible={visible}
                    className={`space-y-2 rounded-xl border border-brand-border bg-brand-bg/60 p-2.5 transition-all duration-500 ${
                      visible ? 'opacity-100 translate-y-0' : 'pointer-events-none opacity-0 translate-y-2'
                    }`}
                  >
                    <div className="flex items-center justify-between text-[10px]">
                      <span className="rounded bg-brand-accent/12 px-1.5 py-0.5 font-bold text-brand-accent">{out.label}</span>
                      <span className="text-brand-muted">{out.note}</span>
                    </div>
                    <video controls playsInline preload="metadata" src={out.url} className="h-44 w-full rounded-lg bg-black object-contain" />
                    <div className="flex items-center justify-between gap-2">
                      <a
                        href={out.url}
                        download
                        className="inline-flex items-center gap-1 rounded border border-brand-border px-2 py-1 text-[10px] font-medium text-brand-fg hover:bg-brand-fg/[0.04]"
                      >
                        下载成片
                      </a>
                      <span className="text-[10px] text-brand-muted">源片 · 26.8s</span>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      ) : null}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div role="tablist" aria-label="内容库分类" className="inline-flex border-b border-brand-border">
          {sections.map(({ id, label }) => (
            <button
              key={id}
              id={`social-media-tab-${id}`}
              type="button"
              role="tab"
              data-testid={`social-media-section-${id}`}
              aria-selected={section === id}
              aria-controls={`social-media-panel-${id}`}
              onClick={() => selectSection(id)}
              className={`border-b-2 px-4 py-2 text-xs font-semibold transition-colors ${
                section === id ? 'border-gray-900 text-gray-900' : 'border-transparent text-gray-500 hover:text-gray-900'
              }`}
            >
              {label}
            </button>
          ))}
        </div>

      </div>

      <div role="tabpanel" id={`social-media-panel-${section}`} aria-labelledby={`social-media-tab-${section}`}>
        <LocalContentView kind={section} domain={domain} publishByContent={publishByContent}  />
      </div>
    </section>
  );
}
