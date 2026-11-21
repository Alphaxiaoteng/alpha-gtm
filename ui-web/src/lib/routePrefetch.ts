// 点击导航时才开始下载页面 chunk，用户会看到一段「正在打开页面」。悬停或键盘
// 聚焦时提前拉取同一个模块，Vite 会复用同一个 chunk，点击时直接命中缓存。
//
// 这里只做预取：失败静默并在下次悬停重试，绝不改变导航本身的行为。
// #region route-prefetch-logic
type Loader = () => Promise<unknown>;

const ROUTE_LOADERS: Record<string, Loader> = {
  shop: () => import('../components/ShopWorkspaceView'),
  dashboard: () => import('../components/DashboardView'),
  terminal: () => import('../components/DashboardView'),
  niches: () => import('../components/DashboardView'),
  creators: () => import('../components/DashboardView'),
  rss: () => import('../components/DashboardView'),
  schedules: () => import('../components/DashboardView'),
  videos: () => import('../components/DashboardView'),
  runs: () => import('../components/DashboardView'),
  crawler: () => import('../components/CrawlerView'),
  logs: () => import('../components/SystemLogsView'),
  memory: () => import('../components/CognitiveMemoryView'),
  contents: () => import('../components/SocialMediaWorkspaceView'),
  templates: () => import('../components/SocialMediaWorkspaceView'),
  'social-media': () => import('../components/SocialMediaWorkspaceView'),
  studio: () => import('../components/StudioWorkspaceView'),
  publish: () => import('../components/PublishWorkspaceView'),
  metrics: () => import('../components/PublishWorkspaceView'),
  records: () => import('../components/PublishWorkspaceView'),
  batch: () => import('../components/PublishWorkspaceView'),
  notifications: () => import('../components/PublishWorkspaceView'),
  accounts: () => import('../components/BrowserWorkspaceView'),
  fleet: () => import('../components/BrowserWorkspaceView'),
  proxies: () => import('../components/EgoSettingsView'),
  skills: () => import('../components/EgoSettingsView'),
  settings: () => import('../components/EgoSettingsView'),
  knowledge: () => import('../components/KnowledgeView'),
  calendar: () => import('../components/ContentCalendarView'),
  'xhs-data': () => import('../components/XHSDataDashboardView'),
  monitor: () => import('../components/RuntimeMonitorView'),
};

const requested = new Set<string>();

/** 预热一张页面的 chunk；同一个 tab 只触发一次，失败后允许重试。 */
function prefetchView(tab: string): void {
  const key = (tab ?? '').trim();
  if (!key || requested.has(key)) return;
  const loader = ROUTE_LOADERS[key];
  if (!loader) return;
  requested.add(key);
  void loader().catch(() => {
    requested.delete(key);
  });
}

/** 测试用：列出所有可预热的 tab，不触发任何 import。 */
function prefetchableTabs(): string[] {
  return Object.keys(ROUTE_LOADERS).sort();
}
// #endregion route-prefetch-logic

export { prefetchView, prefetchableTabs };
