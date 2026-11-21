import { useMemo, useState } from 'react';
import useSWR from 'swr';
import { requestWorkspaceChange } from '../store';
import { useUIStore } from '../store';
import { api, type PublishRecord } from '../api';
import { LocalContentView, type ContentPublishInfo, type LocalContentKind } from './LocalContentView';

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

  const { data: publishRecords } = useSWR<PublishRecord[]>('api/publish-records', api.getPublishRecords);




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
