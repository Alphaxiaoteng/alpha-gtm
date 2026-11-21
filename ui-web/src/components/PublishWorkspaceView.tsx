import { MetricsView } from './MetricsView';
import { NotificationsView } from './NotificationsView';
import { PublishRecordsView } from './PublishRecordsView';
import { PublishView } from './PublishView';
import { useUIStore } from '../store';
import { useEffect } from 'react';
import useSWR from 'swr';
import { api } from '../api';

export type PublishWorkspaceEntry =
  | 'contents'
  | 'studio'
  | 'templates'
  | 'publish'
  | 'batch'
  | 'records'
  | 'metrics'
  | 'notifications';

export function PublishWorkspaceView({ initialEntry = 'publish' }: { initialEntry?: PublishWorkspaceEntry }) {
  const { setActiveTab } = useUIStore();
  // 社交内容库的「手动交接发布」入口：预选待发布内容与来源账号，交给人工驱动的发布流程。
  const requestedPublishHandoff = useUIStore((state) => state.requestedPublishHandoff);
  const requestPublishHandoff = useUIStore((state) => state.requestPublishHandoff);
  useEffect(() => {
    // 消费一次即清空，避免后续再次进入发布页时重复预选。
    if (requestedPublishHandoff !== null) requestPublishHandoff(null);
  }, [requestedPublishHandoff, requestPublishHandoff]);
  const { data: manifests = [] } = useSWR('api/ego/publish-manifests', api.getEgoPublishManifests);
  const { data: publishRecords = [] } = useSWR('api/publish-records', api.getPublishRecords, { refreshInterval: 5_000, refreshWhenHidden: false });
	const firstReleaseManifests = manifests.filter((manifest) => manifest.first_release);
	const availablePlatforms = new Set(firstReleaseManifests.map((manifest) => manifest.platform_code)).size;
	const capabilityCount = firstReleaseManifests.reduce((count, manifest) => count + manifest.capabilities.length, 0);
	const automatedPrepare = firstReleaseManifests.reduce((count, manifest) => count + manifest.capabilities.filter((capability) => capability.prepare_mode === 'automated' && capability.prepare_adapter_supported).length, 0);
	const localSubmitVerifyAdapters = firstReleaseManifests.reduce((count, manifest) => count + manifest.capabilities.filter((capability) => capability.submit_adapter_supported && capability.verify_adapter_supported).length, 0);
	const verified = firstReleaseManifests.reduce((count, manifest) => count + manifest.capabilities.filter((capability) => capability.submit_verified && capability.verify_verified).length, 0);
	const automatedGaps = Math.max(0, automatedPrepare - localSubmitVerifyAdapters);
  const publishedPlatforms = new Set(publishRecords.map((record) => record.platform_code)).size;
  if (initialEntry === 'records') return <PublishRecordsView />;
  if (initialEntry === 'metrics') return <MetricsView />;
  if (initialEntry === 'notifications') return <NotificationsView />;
  return (
    <section className="mx-auto w-full max-w-[88rem] space-y-4" data-testid="publish-workspace">
      <header className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3 border-b border-brand-border pb-3">
        <div className="min-w-0">
          <h2 className="ui-page-title text-xl">发布状态统计与分发</h2>
          <p className="ui-page-sub max-w-3xl">内容分发前先冻结审核计划；平台以安全人工接管与状态统计为主，预填、人工核对与记录追踪严格隔离。</p>
        </div>
        <button
          type="button"
          data-testid="publish-records-entry"
          onClick={() => setActiveTab('records')}
          className="ui-btn shrink-0"
        >
          发布记录 <span className="tabular-nums text-brand-fg-subtle">{publishRecords.length}</span>
        </button>
      </header>
      {firstReleaseManifests.length ? <details className="border-b border-brand-border pb-4 text-xs" data-testid="publish-coverage-summary">
        <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-gray-600 marker:hidden">
          <span><strong className="text-gray-900">覆盖与验收</strong><span className="ml-2 text-gray-500">首版 {availablePlatforms} 个平台 / {capabilityCount} 种形态</span></span>
          <span className="shrink-0 text-[11px] text-gray-400">展开详情</span>
        </summary>
        <div className="mt-3 grid divide-y divide-brand-border border-y border-brand-border sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4">
          <div className="px-3 py-2.5"><span className="block text-[11px] text-gray-500">自动预填</span><strong className="mt-0.5 block text-sm text-gray-900">{automatedPrepare} / {capabilityCount}</strong></div>
          <div className="px-3 py-2.5"><span className="block text-[11px] text-gray-500">本地提交+核验适配</span><strong className="mt-0.5 block text-sm text-gray-900">{localSubmitVerifyAdapters} / {capabilityCount}</strong></div>
          <div className="px-3 py-2.5"><span className="block text-[11px] text-gray-500">自动预填待补适配</span><strong className="mt-0.5 block text-sm text-brand-warning">{automatedGaps} / {capabilityCount}</strong></div>
          <div className="px-3 py-2.5"><span className="block text-[11px] text-gray-500">实号完整验收</span><strong className="mt-0.5 block text-sm text-gray-900">{verified} / {capabilityCount}</strong></div>
        </div>
        <p className="mt-3 text-[11px] leading-relaxed text-gray-500">首版 {availablePlatforms} 个平台 / {capabilityCount} 种形态 · 自动预填 {automatedPrepare} 项 · 本地提交+核验适配 {localSubmitVerifyAdapters} 项 · 自动预填中仍有 {automatedGaps} 项缺少提交/核验适配 · 真正发布成功 {publishRecords.length} 条 / {publishedPlatforms} 个平台 · 实号完整验收 {verified} 项。TikTok 图文暂不支持自动发布；平台异常会通知并继续其余目标。</p>
      </details> : null}
      <PublishView embedded initialContentID={requestedPublishHandoff?.contentID ?? 0} initialAccountID={requestedPublishHandoff?.accountID ?? 0} />
    </section>
  );
}
