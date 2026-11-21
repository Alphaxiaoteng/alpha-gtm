import { useMemo, useState } from 'react';
import useSWR from 'swr';
import { requestWorkspaceChange } from '../store';
import { useUIStore } from '../store';
import { api, type PublishRecord } from '../api';
import { Clock, Copy, ExternalLink, Film } from 'lucide-react';
import { LocalContentView, type ContentPublishInfo, type LocalContentKind } from './LocalContentView';
import type { GuanyingVideoItem } from './GuanyingWorkspaceViews';

type SocialMediaSection = LocalContentKind;

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
              <span>老板娘 Sophia 实拍短视频素材（TikTok @kooying.brake.fact + 抖音 · ≤60 秒）</span>
            </div>
            <span className="text-[11px] text-brand-muted">点击播放预览 · 一键复制外贸配文发布</span>
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
