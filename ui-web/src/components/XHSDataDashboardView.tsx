import { useMemo, useState } from 'react';
import useSWR from 'swr';
import {
  BarChart3,
  CheckCircle2,
  Eye,
  LoaderCircle,
  MousePointerClick,
  RefreshCw,
  Settings2,
  ShieldCheck,
  Sparkles,
} from 'lucide-react';
import { api, type CreatorMetricBatchResult, type MetricsCollectionRunResult } from '../api';
import { buildCreatorMetricTrend, creatorMetricCollectionState, latestCreatorMetricSnapshots } from '../creatorMetrics';
import { PlatformIcon } from './PlatformIcon';
import { BrowserIdentityMark } from './BrowserIdentityMark';

const PLATFORM_NAMES: Record<string, string> = {
	acfun: 'AcFun', amazon_seller: 'Amazon Seller', baijiahao: '百家号', bilibili: '哔哩哔哩',
	csdn: 'CSDN', douyin: '抖音', douyin_shop: '抖音电商', facebook: 'Facebook', instagram: 'Instagram',
	jianshu: '简书', juejin: '掘金', kuaishou: '快手', meta: 'Meta', omtencent: '腾讯内容开放平台',
	pdd: '拼多多', shopee: 'Shopee', shopify: 'Shopify', sina: '新浪微博', taobao: '淘宝',
	tiktok: 'TikTok', tiktok_shop: 'TikTok Shop', toutiao: '今日头条', wechat_mp: '微信公众号',
	wechat_video: '视频号', weishi: '微视', x: 'X', xhs: '小红书', youtube: 'YouTube', zhihu: '知乎',
};

const PLATFORM_SORT: Record<string, number> = {
	douyin: 1, wechat_video: 2, kuaishou: 3, xhs: 4, tiktok: 5, youtube: 6, meta: 7, wechat_mp: 8, csdn: 9,
};

function formatNumber(value: number | undefined) {
  return new Intl.NumberFormat('zh-CN', { notation: 'compact', maximumFractionDigits: 1 }).format(value ?? 0);
}

function collectionLabel(row: { state?: 'observed' | 'skipped' | 'unknown' | 'manual_takeover' | 'not_collected'; range_verified?: boolean; error_code?: string }) {
  if (row.state === 'observed') return row.range_verified ? '已采集近 7 天' : '已采集，时间范围未确认';
  if (row.state === 'skipped' && row.error_code === 'EGO_METRICS_TIMEOUT') return '页面响应超时，已跳过';
  if (row.state === 'skipped' && row.error_code === 'EGO_SPACE_USER_CONTROLLED') return '浏览器正在手动操作，等待交还控制';
  if (row.state === 'skipped' && row.error_code === 'DUPLICATE_ACCOUNT_BINDING') return '与其他浏览器绑定了同一账号，已跳过';
  if (row.state === 'skipped') return '未登录，已跳过';
  if (row.state === 'not_collected') return '尚未采集';
  const labels: Record<string, string> = {
    METRICS_DOM_UNBOUND: '需要重新更新',
	METRICS_RESOURCE_PROVENANCE_MISSING: '需要重新更新',
    METRICS_DOM_UNRECOGNIZED: '暂时无法读取，请稍后重试',
    METRICS_PAGE_REDIRECTED: '页面还没准备好，请稍后重试',
    METRICS_RANGE_UNVERIFIED: '时间范围不明确，请重新更新',
    LOGIN_REQUIRED: '登录已失效',
	EGO_METRICS_PLATFORM_UNSUPPORTED: '暂时无法自动更新',
	EGO_METRICS_MANUAL_HANDOFF: '需要手动查看',
	EGO_METRICS_TIMEOUT: '页面响应超时，已跳过',
	EGO_SPACE_USER_CONTROLLED: '浏览器正在手动操作，等待交还控制',
	DUPLICATE_ACCOUNT_BINDING: '与其他浏览器绑定了同一账号，已跳过',
    EGO_METRICS_COLLECTION_FAILED: '本次采集未完成',
    EGO_SPACE_NOT_FOUND: '绑定空间已失效，请重新绑定',
    EGO_SPACE_BINDING_MISSING: '还没绑定浏览器空间，请先绑定',
    EGO_SPACE_REBIND_FAILED: '绑定空间已失效，重新绑定失败',
  };
  return labels[row.error_code || ''] || '页面暂未识别';
}

export function XHSDataDashboardView() {
  const { data: accounts = [], error: accountsError } = useSWR('api/accounts', api.getAccounts);
  const { data: identities = [], error: identitiesError } = useSWR('api/ego/identities', api.getEgoIdentities);
  const { data: preferences, mutate: refreshPreferences } = useSWR('tool/social.metrics.preferences', api.getCreatorMetricPreferences);
  const historyDays = preferences?.history_days ?? 30;
  const {
    data: overview,
    error: overviewError,
    isLoading: overviewLoading,
    mutate: refreshOverview,
  } = useSWR(['tool/social.metrics.overview', historyDays], () => api.getCreatorMetricsOverview(historyDays), { refreshInterval: 0 });
  const { data: schedule, mutate: refreshSchedule } = useSWR('api/creator-metrics/schedule', api.getMetricsCollectionSchedule);
  const [collecting, setCollecting] = useState(false);
  const [collection, setCollection] = useState<CreatorMetricBatchResult | null>(null);
  const [collectionError, setCollectionError] = useState('');
  const [selectedIdentityID, setSelectedIdentityID] = useState(0);
  const [reviewingWithModel, setReviewingWithModel] = useState(false);
  const [modelReview, setModelReview] = useState('');
  const [modelReviewError, setModelReviewError] = useState('');
  const [manageOpen, setManageOpen] = useState(false);
  const [preferencesOpen, setPreferencesOpen] = useState(false);
  const [historyDaysDraft, setHistoryDaysDraft] = useState(30);
  const [retentionDaysDraft, setRetentionDaysDraft] = useState(90);
  const [savingPreferences, setSavingPreferences] = useState(false);
  const [preferencesError, setPreferencesError] = useState('');

  const sortedIdentities = useMemo(
    () => identities.filter((identity) => identity.enabled).slice().sort((a, b) => a.sort - b.sort || a.id - b.id),
    [identities],
  );
  // 默认选中第一个启用的矩阵；矩阵现在按市场命名（越南、印尼…），不再按编号，
  // 所以不能靠名字匹配，直接取排序后的第一个。
  const defaultIdentity = useMemo(() => sortedIdentities[0], [sortedIdentities]);
  const selectedIdentity = useMemo(
    () => sortedIdentities.find((identity) => identity.id === selectedIdentityID) ?? defaultIdentity,
    [defaultIdentity, selectedIdentityID, sortedIdentities],
  );
  const targetAccounts = useMemo(() => {
    if (!selectedIdentity) return [];
    return accounts
      .filter((account) => account.enabled && account.identityId === selectedIdentity.id)
      .sort((a, b) => (PLATFORM_SORT[a.platformCode] ?? 99) - (PLATFORM_SORT[b.platformCode] ?? 99) || a.platformCode.localeCompare(b.platformCode) || a.id - b.id);
  }, [accounts, selectedIdentity]);
  const targetAccountIDs = useMemo(() => new Set(targetAccounts.map((account) => account.id)), [targetAccounts]);
  const visibleCollections = useMemo(
    () => (overview?.collections ?? []).filter((item) => targetAccountIDs.has(item.account_id)),
    [overview?.collections, targetAccountIDs],
  );
  const visibleTopPosts = useMemo(
    () => (overview?.top_posts ?? []).filter((item) => targetAccountIDs.has(item.account_id)),
    [overview?.top_posts, targetAccountIDs],
  );
  const currentLatest = useMemo(
    () => latestCreatorMetricSnapshots(overview?.latest ?? [], targetAccountIDs, overview?.from_date ?? '', overview?.to_date ?? ''),
    [overview?.from_date, overview?.latest, overview?.to_date, targetAccountIDs],
  );
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai' }).format(new Date());
  const collectedToday = currentLatest.some((item) => item.snapshot_date === today);
  const currentSnapshotIDs = useMemo(() => new Set(currentLatest.map((item) => item.id)), [currentLatest]);
  const currentTopPosts = useMemo(
    () => visibleTopPosts.filter((item) => currentSnapshotIDs.has(item.snapshot_id)),
    [currentSnapshotIDs, visibleTopPosts],
  );
  const collectionByAccountID = useMemo(() => {
    const rows = new Map<number, { state?: 'observed' | 'skipped' | 'unknown' | 'manual_takeover' | 'not_collected'; error_code?: string }>(
      visibleCollections.map((item) => [item.account_id, item]),
    );
    for (const item of collection?.accounts ?? []) rows.set(item.account_id, item);
    return rows;
  }, [collection?.accounts, visibleCollections]);
  const currentSnapshotAccountIDs = useMemo(
    () => new Set(currentLatest.map((item) => item.account_id)),
    [currentLatest],
  );
  const statusCounts = useMemo(() => targetAccounts.reduce((counts, account) => {
    const state = creatorMetricCollectionState(
      collectionByAccountID.get(account.id),
      account.loginState === 'logged_in',
      currentSnapshotAccountIDs.has(account.id),
    ).code;
    if (state === 'observed') counts.verified += 1;
    else if (state === 'stale') counts.stale += 1;
    else if (state === 'manual_takeover') counts.manual += 1;
    else if (state === 'timeout') counts.timeout += 1;
    else if (state === 'user_controlled') counts.userControlled += 1;
    else if (state === 'duplicate') counts.duplicate += 1;
    else if (state === 'logged_out') counts.loggedOut += 1;
    else if (state === 'not_collected') counts.notCollected += 1;
    else counts.insufficient += 1;
    return counts;
  }, { verified: 0, stale: 0, manual: 0, timeout: 0, userControlled: 0, duplicate: 0, insufficient: 0, loggedOut: 0, notCollected: 0 }), [collectionByAccountID, currentSnapshotAccountIDs, targetAccounts]);
  const verifiedCount = currentLatest.length;
  const verifiedFollowerCount = currentLatest.filter((item) => item.followers_verified).length;
  const visibleTrend = useMemo(
    () => buildCreatorMetricTrend(overview?.account_daily ?? [], targetAccountIDs, overview?.from_date ?? '', overview?.to_date ?? ''),
    [overview?.account_daily, overview?.from_date, overview?.to_date, targetAccountIDs],
  );
  const trendPoints = visibleTrend.filter((item) => item.accounts > 0);
  const trendHasEvidence = trendPoints.length >= 2;
  const trendMaxViews = Math.max(0, ...trendPoints.map((item) => item.views));
  const trendChartWidth = Math.max(640, trendPoints.length * 72);
  const trendChartPadding = { top: 18, right: 24, bottom: 28, left: 24 };
  const trendChartPlotWidth = trendChartWidth - trendChartPadding.left - trendChartPadding.right;
  const trendChartPlotHeight = 160 - trendChartPadding.top - trendChartPadding.bottom;
  const trendChartPoints = trendPoints.map((item, index) => ({
    item,
    x: trendPoints.length === 1 ? trendChartWidth / 2 : trendChartPadding.left + (index * trendChartPlotWidth) / (trendPoints.length - 1),
    y: trendMaxViews > 0
      ? trendChartPadding.top + ((trendMaxViews - item.views) / trendMaxViews) * trendChartPlotHeight
      : trendChartPadding.top + trendChartPlotHeight / 2,
  }));
  const totals = currentLatest.reduce(
    (sum, item) => ({
      followers: sum.followers + (item.followers_verified ? item.followers : 0),
      views: sum.views + item.views,
      interactions: sum.interactions + item.likes + item.comments + item.shares + item.favorites,
      posts: sum.posts + item.posts,
    }),
    { followers: 0, views: 0, interactions: 0, posts: 0 },
  );
  const localInsights = useMemo(() => {
    if (!currentLatest.length) return [];
    const topPlatform = [...currentLatest].sort((left, right) => right.views - left.views)[0];
    const latestDate = currentLatest.map((item) => item.snapshot_date).sort().at(-1) ?? '';
    const insights = [`截至 ${topPlatform.snapshot_date}，${PLATFORM_NAMES[topPlatform.platform_code] ?? topPlatform.platform_code}近 7 天浏览最高：${formatNumber(topPlatform.views)}。`];
    const topPost = currentTopPosts[0];
    if (topPost) insights.push(`表现最佳作品：${topPost.title || '未命名作品'}，${topPost.published_at ? `发布于 ${topPost.published_at}，` : ''}截至 ${latestDate} 浏览 ${formatNumber(topPost.views)}。`);
    if (totals.views > 0) insights.push(`截至 ${latestDate}，平台整体互动率约 ${((totals.interactions / totals.views) * 100).toFixed(1)}%。`);
    return insights;
  }, [currentLatest, currentTopPosts, totals.interactions, totals.views]);

  const openPreferences = () => {
    setHistoryDaysDraft(preferences?.history_days ?? 30);
    setRetentionDaysDraft(preferences?.retention_days ?? 90);
    setPreferencesError('');
    setPreferencesOpen((open) => !open);
  };

  const savePreferences = async () => {
    if (savingPreferences) return;
    setSavingPreferences(true);
    setPreferencesError('');
    try {
      const updated = await api.updateCreatorMetricPreferences(historyDaysDraft, retentionDaysDraft);
      await refreshPreferences(updated, { revalidate: false });
      setPreferencesOpen(false);
    } catch (error) {
      setPreferencesError('暂时无法保存，请重试');
    } finally {
      setSavingPreferences(false);
    }
  };

  const [scheduleBusy, setScheduleBusy] = useState(false);
  const [scheduleError, setScheduleError] = useState('');
  const [lastRunResult, setLastRunResult] = useState<MetricsCollectionRunResult | null>(null);

  const freshness = (snapshotDate: string): { label: string; tone: 'ok' | 'warn' | 'bad' } => {
    if (!snapshotDate) return { label: '未采集', tone: 'bad' };
    const today = new Date();
    const then = new Date(`${snapshotDate}T00:00:00`);
    const days = Math.floor((today.setHours(0, 0, 0, 0) - then.getTime()) / 86_400_000);
    if (days <= 0) return { label: '今日已更新', tone: 'ok' };
    if (days <= 3) return { label: `${days} 天前`, tone: 'warn' };
    return { label: '已过期', tone: 'bad' };
  };

  // 采集证据与登录态冲突：浏览器页显示已登录，但最近一次采集返回人工接管/需登录/用户占用。
  const conflictAccountIDs = useMemo(() => {
    const loggedIn = new Set(accounts.filter((account) => account.loginState === 'logged_in').map((account) => account.id));
    return new Set(
      (overview?.collections ?? [])
        .filter((item) => loggedIn.has(item.account_id))
        .filter((item) => item.state === 'manual_takeover' || (item.error_code ?? '').includes('LOGIN_REQUIRED') || (item.error_code ?? '').includes('USER_CONTROLLED'))
        .map((item) => item.account_id),
    );
  }, [accounts, overview?.collections]);

  const updateSchedule = async (input: { enabled?: boolean; interval_hours?: number }) => {
    if (scheduleBusy) return;
    setScheduleBusy(true);
    setScheduleError('');
    try {
      await api.updateMetricsCollectionSchedule(input);
      await refreshSchedule();
    } catch (error) {
      setScheduleError(error instanceof Error ? error.message : '保存失败');
    } finally {
      setScheduleBusy(false);
    }
  };

  const runScheduleNow = async () => {
    if (scheduleBusy) return;
    setScheduleBusy(true);
    setScheduleError('');
    try {
      const result = await api.runMetricsCollectionSchedule();
      setLastRunResult(result);
      await refreshSchedule();
      await refreshOverview();
    } catch (error) {
      setScheduleError(error instanceof Error ? error.message : '运行失败');
    } finally {
      setScheduleBusy(false);
    }
  };

  const collect = async () => {
    if (collecting || targetAccounts.length === 0) return;
    setCollecting(true);
    setCollection(null);
    setCollectionError('');
    try {
      const result = await api.collectCreatorMetrics(targetAccounts.map((account) => account.id));
      setCollection(result);
      await refreshOverview();
    } catch (error) {
      setCollectionError(error instanceof Error ? error.message : '采集失败');
    } finally {
      setCollecting(false);
    }
  };

  const reviewMetricsWithModel = async () => {
    if (reviewingWithModel || currentLatest.length === 0) return;
    setReviewingWithModel(true);
    setModelReview('');
    setModelReviewError('');
    try {
      const result = await api.assistContent({
        action: 'metrics_review',
        title: `${selectedIdentity?.name || '当前浏览器'}近 7 天内容运营复盘`,
        caption: '只分析已验证快照；零值和缺失值不得推断为真实表现。',
        body: JSON.stringify({
          identity: selectedIdentity?.name || '',
          window_days: overview?.window_days ?? 7,
          from_date: overview?.from_date ?? '',
          to_date: overview?.to_date ?? '',
          snapshots: currentLatest.map((item) => ({
            platform_code: item.platform_code,
            snapshot_date: item.snapshot_date,
            range_verified: item.range_verified,
            followers: item.followers_verified ? item.followers : null,
            followers_verified: item.followers_verified,
            views: item.views,
            likes: item.likes,
            comments: item.comments,
            shares: item.shares,
            favorites: item.favorites,
            posts: item.posts,
            evidence_code: item.evidence_code,
          })),
          top_posts: currentTopPosts.slice(0, 10).map((item) => ({
            platform_code: item.platform_code,
            title: item.title,
            published_at: item.published_at,
            views: item.views,
            likes: item.likes,
            comments: item.comments,
            shares: item.shares,
            favorites: item.favorites,
          })),
          limitations: visibleCollections
            .filter((item) => item.state !== 'observed' || !item.range_verified)
            .map((item) => ({ platform_code: item.platform_code, state: item.state, error_code: item.error_code })),
        }),
        allow_external: true,
        use_knowledge: false,
      });
      setModelReview(result.result);
    } catch (error) {
      setModelReviewError('建议生成失败，请稍后重试');
    } finally {
      setReviewingWithModel(false);
    }
  };

  const selectIdentity = (identityID: number) => {
    if (collecting) return;
    setSelectedIdentityID(identityID);
    setCollection(null);
    setCollectionError('');
    setModelReview('');
    setModelReviewError('');
  };

  return (
    <section className="space-y-4" data-testid="creator-metrics-dashboard">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border pb-4">
        <div role="tablist" aria-label="选择浏览器" className="flex flex-wrap items-center gap-1 rounded-lg border border-brand-border bg-brand-surface p-1">
          {sortedIdentities.map((identity, index) => {
            const selected = identity.id === selectedIdentity?.id;
            return (
              <button
                key={identity.id}
                type="button"
                role="tab"
                aria-selected={selected}
                data-testid={`creator-browser-switch-${identity.id}`}
                onClick={() => selectIdentity(identity.id)}
                disabled={collecting}
                className={`flex h-9 items-center gap-2 rounded-md px-2.5 text-xs font-semibold transition-colors ${selected ? 'bg-gray-950 text-white' : 'text-gray-600 hover:bg-black/[0.04] hover:text-gray-900'}`}
              >
                <BrowserIdentityMark identity={identity} index={index} className="h-6 w-6" inverted={selected} />
                <span>{identity.name}</span>
              </button>
            );
          })}
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <button
            type="button"
            data-testid="creator-metrics-preferences-toggle"
            onClick={openPreferences}
            className="ui-btn"
          >
            <Settings2 className="h-3.5 w-3.5" />数据设置
          </button>
          <button
            type="button"
            data-testid="collect-current-browser-metrics"
            onClick={() => void collect()}
            disabled={collecting || targetAccounts.length === 0}
            aria-busy={collecting}
            className="ui-btn ui-btn-primary disabled:cursor-wait disabled:opacity-50"
          >
            {collecting ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
            {collecting ? '更新中…' : '更新数据'}
          </button>
          <button
            type="button"
            data-testid="manage-creator-data"
            onClick={() => setManageOpen(true)}
            className="ui-btn"
          >
            <MousePointerClick className="h-3.5 w-3.5" />查看状态
          </button>
        </div>
      </header>

      {preferencesOpen ? (
        <div data-testid="creator-metrics-preferences" className="ui-section flex flex-wrap items-end gap-3 px-4 py-3">
          <label className="text-[11px] font-semibold text-gray-600">
            展示范围
            <select
              value={historyDaysDraft}
              onChange={(event) => {
                const next = Number(event.target.value);
                setHistoryDaysDraft(next);
                if (retentionDaysDraft < next) setRetentionDaysDraft(next === 90 ? 90 : 30);
              }}
              className="mt-1 block h-8 rounded-md border border-brand-border bg-brand-surface px-2 text-xs text-gray-900"
            >
              <option value={7}>近 7 天</option><option value={30}>近 30 天</option><option value={90}>近 90 天</option>
            </select>
          </label>
          <label className="text-[11px] font-semibold text-gray-600">
            记录保留
            <select value={retentionDaysDraft} onChange={(event) => setRetentionDaysDraft(Number(event.target.value))} className="mt-1 block h-8 rounded-md border border-brand-border bg-brand-surface px-2 text-xs text-gray-900">
              {[30, 90, 180, 365].filter((days) => days >= historyDaysDraft).map((days) => <option key={days} value={days}>{days} 天</option>)}
            </select>
          </label>
          <p className="min-w-48 flex-1 text-[10px] leading-4 text-gray-500">每天每个账号保留一份记录，超过期限后自动清理。</p>
          {preferencesError ? <span className="text-[10px] text-red-600">{preferencesError}</span> : null}
          <button type="button" onClick={() => void savePreferences()} disabled={savingPreferences} className="ui-btn ui-btn-primary">{savingPreferences ? '保存中…' : '保存'}</button>
        </div>
      ) : null}

      <div data-testid="metrics-schedule-settings" className="ui-section flex flex-wrap items-center gap-3 px-4 py-3">
        <label className="flex items-center gap-2 text-[11px] font-semibold text-gray-700">
          <input
            type="checkbox"
            data-testid="metrics-schedule-enabled"
            checked={schedule?.enabled ?? false}
            onChange={(event) => void updateSchedule({ enabled: event.target.checked })}
            disabled={scheduleBusy}
          />
          自动采集
        </label>
        <label className="text-[11px] font-semibold text-gray-600">
          间隔
          <select
            value={schedule?.interval_hours ?? 24}
            onChange={(event) => void updateSchedule({ interval_hours: Number(event.target.value) })}
            disabled={scheduleBusy || !schedule?.enabled}
            className="ml-1 block h-8 rounded-md border border-brand-border bg-brand-surface px-2 text-xs text-gray-900"
          >
            <option value={12}>12 小时</option><option value={24}>24 小时</option><option value={48}>48 小时</option><option value={72}>72 小时</option>
          </select>
        </label>
        <button type="button" data-testid="metrics-schedule-run" onClick={() => void runScheduleNow()} disabled={scheduleBusy} className="ui-btn">
          {scheduleBusy ? '运行中…' : '立即采集'}
        </button>
        {schedule?.last_finished_at ? (
          <span className="text-[10px] text-gray-500">
            上次运行 {schedule.last_finished_at.slice(0, 16).replace('T', ' ')}
            {schedule.last_result?.collected !== undefined ? ` · 更新 ${schedule.last_result.collected} / 跳过 ${schedule.last_result.skipped ?? 0} / 失败 ${schedule.last_result.failed ?? 0}` : ''}
          </span>
        ) : (
          <span className="text-[10px] text-gray-400">尚未运行</span>
        )}
        {lastRunResult ? <span className="text-[10px] text-emerald-700">本次：更新 {lastRunResult.collected} / 跳过 {lastRunResult.skipped} / 失败 {lastRunResult.failed}</span> : null}
        {scheduleError ? <span className="text-[10px] text-red-600">{scheduleError}</span> : null}
      </div>

      <div className="flex flex-wrap items-center gap-2 border-y border-brand-border px-1 py-3 text-[11px] text-gray-600">
        <ShieldCheck className="h-4 w-4 text-emerald-600" />
        <span className="font-semibold text-gray-800">{schedule?.enabled ? `每 ${schedule.interval_hours} 小时自动更新` : collectedToday ? '今天已更新' : '手动更新'} · 掉线平台自动跳过</span>
        <strong data-testid="creator-metrics-coverage" className="ml-auto text-gray-800">已有 {verifiedCount} 个平台 · 数据截至 {currentLatest.length ? currentLatest.map((item) => item.snapshot_date).sort().at(-1) : '暂无'}</strong>
      </div>

      {accountsError || identitiesError ? (
        <div className="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-xs text-red-700">无法读取浏览器账号配置，本地数据未改变。</div>
      ) : null}
      {overviewError ? (
        <div className="rounded-lg border border-brand-warning/30 bg-brand-warning/10 px-4 py-3 text-xs text-brand-warning">数据暂时无法显示，请稍后重试。</div>
      ) : null}
      {collectionError ? (
        <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-xs text-red-700">更新未完成，原有数据已保留。</div>
      ) : null}

      <div data-testid="creator-metrics-summary" className="ui-data-strip">
        <article data-testid="creator-metrics-followers-total"><CheckCircle2 className="h-3.5 w-3.5 text-emerald-600" /><p className="mt-2 text-[10px] text-gray-500">粉丝</p><strong className="mt-0.5 block text-xl text-gray-900">{overviewLoading ? '…' : verifiedFollowerCount > 0 ? formatNumber(totals.followers) : '—'}</strong></article>
        <article><Eye className="h-3.5 w-3.5 text-gray-400" /><p className="mt-2 text-[10px] text-gray-500">近 7 天浏览</p><strong className="mt-0.5 block text-xl text-gray-900">{overviewLoading ? '…' : verifiedCount > 0 ? formatNumber(totals.views) : '—'}</strong></article>
        <article><Sparkles className="h-3.5 w-3.5 text-gray-400" /><p className="mt-2 text-[10px] text-gray-500">互动</p><strong className="mt-0.5 block text-xl text-gray-900">{overviewLoading ? '…' : verifiedCount > 0 ? formatNumber(totals.interactions) : '—'}</strong></article>
        <article><BarChart3 className="h-3.5 w-3.5 text-gray-400" /><p className="mt-2 text-[10px] text-gray-500">作品</p><strong className="mt-0.5 block text-xl text-gray-900">{overviewLoading ? '…' : verifiedCount > 0 ? formatNumber(totals.posts) : '—'}</strong></article>
      </div>

      <article className="ui-section overflow-hidden" data-testid="creator-metrics-platform-table">
        <div className="flex items-center justify-between gap-2 border-b border-brand-border px-4 py-3">
          <h3 className="text-sm font-semibold text-gray-900">平台数据</h3>
          <span className="text-[10px] text-gray-500">只显示已有数据</span>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs">
            <thead className="bg-black/[0.02] text-[10px] text-gray-500">
              <tr>
                <th className="px-4 py-2 font-medium">平台</th>
                <th className="px-3 py-2 text-right font-medium">浏览</th>
                <th className="px-3 py-2 text-right font-medium">粉丝</th>
                <th className="px-3 py-2 text-right font-medium">互动</th>
                <th className="px-3 py-2 text-right font-medium">作品</th>
                <th className="px-4 py-2 text-right font-medium">数据截至</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-brand-border">
              {targetAccounts.filter((account) => currentSnapshotAccountIDs.has(account.id)).map((account) => {
                const code = account.platformCode;
                const snapshot = currentLatest.find((item) => item.account_id === account.id);
                const interactions = snapshot ? snapshot.likes + snapshot.comments + snapshot.shares + snapshot.favorites : 0;
                const fresh = freshness(snapshot?.snapshot_date ?? '');
                const hasConflict = conflictAccountIDs.has(account.id);
                return (
                  <tr key={account.id} data-testid={`creator-account-${code}`} data-metric-state={hasConflict ? 'conflict' : 'observed'}>
                    <td className="px-4 py-3">
                      <div className="flex min-w-0 items-center gap-2.5">
                        <PlatformIcon platform={code} title={PLATFORM_NAMES[code] ?? code} className="h-5 w-5" />
                        <span className="min-w-0">
                          <strong className="block text-gray-900">{PLATFORM_NAMES[code] ?? code}</strong>
                          <span className="block truncate text-[10px] text-gray-400">{account.name || account.accountIdentifier || '尚未识别账号'}</span>
                        </span>
                      </div>
                    </td>
                    <td className="px-3 py-3 text-right font-semibold text-gray-900">{snapshot ? formatNumber(snapshot.views) : '-'}</td>
                    <td className="px-3 py-3 text-right text-gray-700">{snapshot?.followers_verified ? formatNumber(snapshot.followers) : '-'}</td>
                    <td className="px-3 py-3 text-right text-gray-700">{snapshot ? formatNumber(interactions) : '-'}</td>
                    <td className="px-3 py-3 text-right text-gray-700">{snapshot ? formatNumber(snapshot.posts) : '-'}</td>
                    <td className="px-4 py-3 text-right text-[10px]">
                      <span className={`inline-flex items-center rounded-full px-2 py-0.5 font-semibold ${fresh.tone === 'ok' ? 'bg-emerald-50 text-emerald-700' : fresh.tone === 'warn' ? 'bg-brand-warning/10 text-brand-warning' : 'bg-red-50 text-red-600'}`} data-testid={`creator-freshness-${code}`}>{fresh.label}</span>
                      {hasConflict ? <span className="ml-1 inline-flex items-center rounded-full bg-brand-warning/10 px-2 py-0.5 font-semibold text-brand-warning" title="浏览器页显示已登录，但最近一次采集遇到登录/占用问题；以采集证据为准，去浏览器与账号页处理">采集冲突</span> : null}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {!currentLatest.length ? (
            <div className="px-4 py-8 text-center text-xs text-gray-500">当前浏览器还没有数据，点击“更新数据”开始。</div>
          ) : null}
        </div>
      </article>

      <article className="ui-section p-4" data-testid="creator-metrics-trend">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div>
            <h3 className="text-sm font-semibold text-gray-900">近 {historyDays} 天变化</h3>
            <p className="mt-0.5 text-[10px] text-gray-500">只显示实际有数据的日期，未更新的日期不会显示为 0。</p>
          </div>
          <span className="text-[10px] font-semibold text-gray-500">{overview?.from_date || '—'} 至 {overview?.to_date || '—'}</span>
        </div>
        {trendHasEvidence ? (
          <div className="mt-4 overflow-x-auto pb-1">
            <svg
              data-testid="creator-metrics-line-chart"
              className="h-40 w-full min-w-[640px] overflow-visible text-gray-900"
              viewBox={`0 0 ${trendChartWidth} 160`}
              role="img"
              aria-label={`${selectedIdentity?.name || '当前浏览器'}近 ${historyDays} 天历史浏览趋势，仅显示真实采集日期`}
            >
              <title>{`${selectedIdentity?.name || '当前浏览器'}近 ${historyDays} 天历史浏览趋势，仅显示真实采集日期`}</title>
              <line
                x1={trendChartPadding.left}
                y1={trendChartPadding.top + trendChartPlotHeight}
                x2={trendChartWidth - trendChartPadding.right}
                y2={trendChartPadding.top + trendChartPlotHeight}
                stroke="currentColor"
                strokeOpacity="0.12"
              />
              <polyline
                fill="none"
                stroke="currentColor"
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="2"
                points={trendChartPoints.map(({ x, y }) => `${x},${y}`).join(' ')}
              />
              {trendChartPoints.map(({ item, x, y }) => (
                <g key={item.date} role="group" aria-label={`${item.date}：浏览 ${item.views}`}>
                  <title>{`${item.date} · 浏览 ${item.views} · 互动 ${item.interactions} · 作品 ${item.posts}`}</title>
                  <circle cx={x} cy={y} r="4" fill="currentColor" />
                  <text x={x} y={Math.max(12, y - 10)} textAnchor="middle" className="fill-current text-[10px] font-semibold">{formatNumber(item.views)}</text>
                  <text x={x} y={154} textAnchor="middle" className="fill-gray-400 text-[10px]">{item.date.slice(5).replace('-', '/')}</text>
                </g>
              ))}
            </svg>
          </div>
        ) : (
          <p className="mt-4 rounded-lg border border-dashed border-brand-border px-4 py-8 text-center text-xs text-gray-500">还没有足够数据，完成两次更新后显示变化。</p>
        )}
      </article>

      <article className="ui-section overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-brand-border px-4 py-3">
          <div><h3 className="text-sm font-semibold text-gray-900">数据建议</h3><p className="mt-0.5 text-[10px] text-gray-500">点击后使用你选择的智能助手，只分析当前数据。</p></div>
          <button
            type="button"
            data-testid="review-selected-browser-metrics"
            onClick={() => void reviewMetricsWithModel()}
            disabled={reviewingWithModel || currentLatest.length === 0}
            className="ui-btn disabled:opacity-40"
          >
            <Sparkles className="h-3.5 w-3.5" />{reviewingWithModel ? '生成中…' : '生成建议'}
          </button>
        </div>
        <div className="space-y-3 p-4">
          {localInsights.map((insight, index) => <p key={`${insight}-${index}`} className="rounded-lg bg-black/[0.025] px-3 py-2 text-xs leading-5 text-gray-700">{insight}</p>)}
          {!localInsights.length ? <p className="py-8 text-center text-xs text-gray-500">有数据后显示建议，不会自动生成。</p> : null}
          {modelReviewError ? <p className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">{modelReviewError}</p> : null}
          {modelReview ? <pre data-testid="creator-metrics-model-review" className="whitespace-pre-wrap rounded-md border border-brand-border bg-brand-surface-raised px-3 py-3 text-xs leading-6 text-gray-700">{modelReview}</pre> : null}
        </div>
      </article>

      {currentTopPosts.length ? (
        <article className="ui-section overflow-hidden">
          <div className="flex items-center justify-between border-b border-brand-border px-4 py-3"><span className="text-sm font-semibold text-gray-900">表现较好的作品</span><span className="text-[10px] text-gray-500">数据截至 {currentLatest.map((item) => item.snapshot_date).sort().at(-1)}</span></div>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="bg-black/[0.02] text-[10px] text-gray-500"><tr><th className="px-4 py-2 font-medium">平台 / 标题</th><th className="px-3 py-2 font-medium">发布时间</th><th className="px-3 py-2 text-right font-medium">浏览</th><th className="px-3 py-2 text-right font-medium">赞</th><th className="px-4 py-2 text-right font-medium">评论</th></tr></thead>
              <tbody className="divide-y divide-brand-border">{currentTopPosts.slice(0, 10).map((post) => <tr key={post.id}><td className="max-w-[30rem] px-4 py-3"><span className="flex items-center gap-2"><PlatformIcon platform={post.platform_code} title={PLATFORM_NAMES[post.platform_code]} className="h-4 w-4" /><span className="text-gray-900">{post.title || '未命名作品'}</span></span></td><td className="whitespace-nowrap px-3 py-3 text-[10px] text-gray-500">{post.published_at || '未提供'}</td><td className="px-3 py-3 text-right">{formatNumber(post.views)}</td><td className="px-3 py-3 text-right">{formatNumber(post.likes)}</td><td className="px-4 py-3 text-right">{formatNumber(post.comments)}</td></tr>)}</tbody>
            </table>
          </div>
        </article>
      ) : null}

      {manageOpen ? (
        <div className="fixed inset-0 z-40 flex justify-end bg-black/20" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setManageOpen(false); }}>
          <aside className="flex h-full w-full max-w-lg flex-col bg-brand-surface shadow-2xl" role="dialog" aria-modal="true" aria-labelledby="manage-creator-data-title">
            <header className="flex items-start justify-between border-b border-brand-border px-5 py-4">
              <div><h3 id="manage-creator-data-title" className="text-base font-bold text-gray-900">平台状态</h3><p className="mt-1 text-xs text-gray-500">查看哪些平台需要更新或手动处理。</p></div>
              <button type="button" aria-label="关闭管理数据" onClick={() => setManageOpen(false)} className="rounded-lg p-1.5 text-gray-500 hover:bg-black/[0.04]">×</button>
            </header>
            <div className="flex-1 overflow-y-auto px-5 py-4">
              <div data-testid="creator-metrics-status-summary" className="mb-4 flex flex-wrap gap-2 text-[10px] font-semibold">
                <span className="rounded-full bg-emerald-50 px-2.5 py-1 text-emerald-700">已更新 {statusCounts.verified}</span>
                <span className="rounded-full bg-gray-100 px-2.5 py-1 text-gray-700">待更新 {statusCounts.stale + statusCounts.notCollected}</span>
                <span className="rounded-full bg-brand-warning/10 px-2.5 py-1 text-brand-warning">需处理 {statusCounts.manual + statusCounts.timeout + statusCounts.userControlled + statusCounts.duplicate + statusCounts.insufficient + statusCounts.loggedOut}</span>
              </div>
              <div className="divide-y divide-brand-border border-y border-brand-border">
                {targetAccounts.map((account) => {
                  const persistedRow = visibleCollections.find((item) => item.account_id === account.id);
                  const row = collectionByAccountID.get(account.id) ?? persistedRow;
                  const state = creatorMetricCollectionState(row, account.loginState === 'logged_in', currentSnapshotAccountIDs.has(account.id));
                  return <div key={account.id} className="py-3" data-testid={`manage-creator-account-${account.platformCode}`}><div className="flex items-start justify-between gap-3"><div className="flex min-w-0 items-center gap-2.5"><PlatformIcon platform={account.platformCode} title={PLATFORM_NAMES[account.platformCode]} className="h-5 w-5" /><div className="min-w-0"><strong className="text-sm text-gray-900">{PLATFORM_NAMES[account.platformCode] ?? account.platformCode}</strong><p className="mt-1 truncate text-[10px] text-gray-500">{account.name || account.accountIdentifier || '尚未识别账号'} · {collectionLabel(row ?? {})}</p></div></div><span className={`shrink-0 rounded-full px-1.5 py-0.5 text-[9px] font-semibold ${state.tone}`}>{state.label}</span></div></div>;
                })}
              </div>
            </div>
            <footer className="border-t border-brand-border px-5 py-4"><button type="button" data-testid="collect-selected-browser-metrics" onClick={() => void collect()} disabled={collecting || targetAccounts.length === 0} aria-busy={collecting} className="inline-flex w-full items-center justify-center gap-1.5 rounded-lg bg-brand-accent px-3 py-2.5 text-xs font-bold text-brand-on-accent disabled:cursor-wait disabled:opacity-50">{collecting ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <MousePointerClick className="h-3.5 w-3.5" />}{collecting ? '更新中…' : `更新${selectedIdentity?.name || '当前浏览器'}数据`}</button></footer>
          </aside>
        </div>
      ) : null}
    </section>
  );
}
