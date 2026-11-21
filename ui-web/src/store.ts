import type React from 'react';
import { create } from 'zustand';
import { getStoredProductMode, isTabAvailableForMode, persistProductMode, SHOP_MODE_ENABLED, type ProductMode } from './workspaceMode';
import { parseRoute, routeForTab } from './lib/routes';
import type { KnowledgeScope } from './api';

declare global {
  interface Window {
    electronAPI?: {
      // Legacy type-only surface for the unreachable pre-EgoLite settings component.
      selectBrowserPath?: (defaultPath?: string) => Promise<string | null>;
      selectMediaFile?: () => Promise<string | null>;
	  selectAssetFolder?: (defaultPath?: string) => Promise<string | null>;
      openPathInFinder: (fullPath: string) => Promise<boolean>;
      openExternalURL?: (url: string) => Promise<{ ok: boolean; error?: string }>;
      showPublishNotification?: (notificationID: number) => Promise<{ ok: boolean; duplicate?: boolean; error?: string }>;
      onOpenPublishRecord?: (cb: (target: { relatedType: 'publish_job' | 'publish_plan'; relatedID: number }) => void) => () => void;
      applyBrowser?: (opts?: { id?: string; path?: string; engine?: string }) => Promise<any>;
      isElectron: boolean;
    };
  }
}


// 单个博主文件夹的深链：/trend/creators/{platform}/{user_id}
// user_id（抖音 sec_user_id）大小写敏感，解析时不能整体小写化。
const CREATOR_PATH_PREFIX = '/trend/creators/';

export function isCreatorPath(pathname: string): boolean {
  return pathname.toLowerCase().startsWith(CREATOR_PATH_PREFIX);
}

// 从 /trend/creators/{platform}/{user_id} 还原 `${platform}-${user_id}`。
export function getCreatorKeyFromPathname(): string | null {
  if (typeof window === 'undefined') return null;
  const raw = window.location.pathname.replace(/\/+$/, '');
  if (!isCreatorPath(raw)) return null;
  const parts = raw.slice(CREATOR_PATH_PREFIX.length).split('/');
  if (parts.length !== 2) return null;
  const platform = parts[0].toLowerCase();
  if (platform !== 'douyin' && platform !== 'xhs') return null;
  let userID: string;
  try {
    userID = decodeURIComponent(parts[1]);
  } catch {
    return null;
  }
  return userID ? `${platform}-${userID}` : null;
}

// 进入单个博主时同步 URL（pushState，浏览器后退可回到列表）。
// 退出（key 为空）时只把「博主深链」收回 /trend/creators，且用 replaceState，
// 不新压历史项；若当前已经在别的 tab（如 /trend/keywords），保持 URL 不动，
// 避免 tab 切换刚写好的路径被清空操作冲掉。
export function syncCreatorURL(key: string): void {
  if (typeof window === 'undefined') return;
  const rawPath = window.location.pathname;
  const current = rawPath.replace(/\/+$/, '') || '/';
  const suffix = window.location.search;

  if (!key) {
    if (isCreatorPath(rawPath)) {
      window.history.replaceState(null, '', '/trend/creators' + suffix);
    }
    return;
  }

  const platform = key.startsWith('xhs-') ? 'xhs' : 'douyin';
  const userID = key.slice(platform.length + 1);
  const target = `${CREATOR_PATH_PREFIX}${platform}/${encodeURIComponent(userID)}`;
  if (current !== target) {
    window.history.pushState(null, '', target + suffix);
  }
}

export function getTabFromPathname(): TabType | undefined {
  if (typeof window === 'undefined') return undefined;
  const parsed = parseRoute(window.location.pathname);
  if (parsed) return parsed.tab as TabType;
  if (isCreatorPath(window.location.pathname)) return 'dashboard';
  return undefined;
}

export type RadarContentView = 'keywords' | 'videos' | 'creators';

const RADAR_VIEW_SEGMENT: Record<RadarContentView, string> = {
  keywords: 'keywords',
  videos: 'videos',
  creators: 'creators',
};

const RADAR_PATH_VIEW: Record<string, RadarContentView> = {
  '/trend': 'keywords',
  '/trend-radar': 'keywords',
  '/dashboard': 'keywords',
  '/trend/keywords': 'keywords',
  '/trend/videos': 'videos',
  '/trend/creators': 'creators',
  '/gtm/trend': 'keywords',
  '/gtm/trend/keywords': 'keywords',
  '/gtm/trend/videos': 'videos',
  '/gtm/trend/creators': 'creators',
  '/SM/trend': 'keywords',
  '/SM/trend/keywords': 'keywords',
  '/SM/trend/videos': 'videos',
  '/SM/trend/creators': 'creators',
  '/sm/trend': 'keywords',
  '/sm/trend/keywords': 'keywords',
  '/sm/trend/videos': 'videos',
  '/sm/trend/creators': 'creators',
  '/media/trend': 'keywords',
  '/media/trend/keywords': 'keywords',
  '/media/trend/videos': 'videos',
  '/media/trend/creators': 'creators',
};

export function getRadarViewFromPathname(): RadarContentView | undefined {
  if (typeof window === 'undefined') return undefined;
  const path = window.location.pathname.toLowerCase().replace(/\/+$/, '') || '/';
  const exact = RADAR_PATH_VIEW[path];
  if (exact) return exact;
  if (isCreatorPath(path)) return 'creators';
  return undefined;
}

export function syncRadarViewURL(view: RadarContentView): void {
  if (typeof window === 'undefined') return;
  const targetPath = `/GTM/trend/${RADAR_VIEW_SEGMENT[view]}`;
  const currentPath = window.location.pathname.toLowerCase().replace(/\/+$/, '') || '/';
  if (currentPath !== targetPath.toLowerCase()) {
    window.history.replaceState(null, '', targetPath + window.location.search);
  }
}

export function syncURLToTab(tab: TabType, mode: ProductMode = getStoredProductMode()): void {
  if (typeof window === 'undefined') return;
  const targetPath = routeForTab(tab, mode);
  const currentPath = window.location.pathname.toLowerCase().replace(/\/+$/, '') || '/';
  if (currentPath === targetPath.toLowerCase()) return;
  // 保留趋势雷达子视图路径（/trend/keywords|videos|creators）与单个博主深链，
  // 避免父级 tab 同步把子路由冲掉（尤其是刷新 /trend/creators/{platform}/{id} 时）。
  if (tab === 'dashboard') {
    if (RADAR_PATH_VIEW[currentPath] !== undefined) return;
    if (/^\/(?:(?:gtm|sm|media)\/)?trend\//.test(currentPath)) return;
    if (isCreatorPath(window.location.pathname)) return;
  }
  // 设置子页（/GTM/settings/logs）不能被父级同步冲回 /settings。
  if (tab === 'settings' && /^\/(?:(?:gtm|sm|shop)\/)?settings\//.test(currentPath)) return;
  window.history.replaceState(null, '', targetPath + window.location.search);
}

export type TabType = 'shop' | 'dashboard' | 'crawler' | 'contents' | 'studio' | 'templates' | 'knowledge' | 'memory' | 'publish' | 'records' | 'metrics' | 'accounts' | 'fleet' | 'proxies' | 'batch' | 'notifications' | 'social-media' | 'skills' | 'xhs-data' | 'calendar' | 'agent' | 'bot' | 'terminal' | 'monitor' | 'logs' | 'settings';

export type AgentDraft = {
  message: string;
  mode: 'chat' | 'tools';
  knowledgeScopes?: KnowledgeScope[];
};

const AGENT_PANEL_WIDTH_KEY = 'alpha-nexus-egolite-agent-panel-width-v3';
// 侧栏状态：expanded 展开 / rail 图标收纳栏 / hidden 全隐藏。
// v2 从旧的布尔 hidden 迁移到三态，旧 key 仍读取以兼容历史取值。
const SIDEBAR_STATE_KEY = 'alpha-nexus-egolite-sidebar-state-v2';
const SIDEBAR_HIDDEN_KEY = 'alpha-nexus-egolite-sidebar-hidden-v1';
// v3 keeps the product Agent on the DSH harness without overwriting the
// user's historical provider preference key.
const AGENT_PRIMARY_PROVIDER_KEY = 'alpha-nexus-egolite-agent-primary-provider-v4';
const DEFAULT_AGENT_PROVIDER = 'opencode';
export const AGENT_PANEL_MIN_WIDTH = 320;
export const AGENT_PANEL_MAX_WIDTH = 560;
const AGENT_PANEL_DEFAULT_WIDTH = 420;
// 240px navigation + a preferred 720px workspace. At Electron's 1100px
// minimum window width the panel stays at 320px and the workspace keeps 540px.
const AGENT_PANEL_RESERVED_WIDTH = 960;
const UI_RECOVERY_PREFIX = 'alpha-nexus-egolite-ui-recovery:';
export const BEFORE_WORKSPACE_CHANGE_EVENT = 'alpha-nexus-egolite:before-workspace-change';

export function requestWorkspaceChange(detail?: Record<string, unknown>): boolean {
  if (typeof window === 'undefined') return true;
  return window.dispatchEvent(new CustomEvent(BEFORE_WORKSPACE_CHANGE_EVENT, {
    cancelable: true,
    detail,
  }));
}

type UIRecoveryState = {
  activeTab?: TabType;
  agentDrawerOpen?: boolean;
  agentProvider?: string;
};

const VALID_TABS = new Set<TabType>([
  'shop', 'dashboard', 'crawler', 'contents', 'studio', 'templates', 'publish', 'records', 'metrics',
  'accounts', 'fleet', 'proxies', 'batch', 'notifications', 'social-media',
  'knowledge', 'memory', 'skills', 'xhs-data', 'calendar', 'agent', 'bot', 'terminal', 'monitor', 'logs', 'settings',
]);

function readRecoveryState(): UIRecoveryState {
  if (typeof window === 'undefined' || !window.name.startsWith(UI_RECOVERY_PREFIX)) return {};
  try {
    const parsed = JSON.parse(window.name.slice(UI_RECOVERY_PREFIX.length)) as UIRecoveryState;
    return {
      activeTab: parsed.activeTab && VALID_TABS.has(parsed.activeTab) ? parsed.activeTab : undefined,
      agentDrawerOpen: typeof parsed.agentDrawerOpen === 'boolean' ? parsed.agentDrawerOpen : undefined,
      agentProvider: typeof parsed.agentProvider === 'string' ? parsed.agentProvider : undefined,
    };
  } catch {
    return {};
  }
}

function writeRecoveryState(patch: UIRecoveryState): void {
  if (typeof window === 'undefined') return;
  window.name = `${UI_RECOVERY_PREFIX}${JSON.stringify({ ...readRecoveryState(), ...patch })}`;
}

export function recoveredAgentProvider(): string {
  return readRecoveryState().agentProvider || '';
}

export function rememberAgentProvider(agentProvider: string): void {
  writeRecoveryState({ agentProvider });
}

export function preferredAgentProvider(): string {
  if (typeof window === 'undefined') return DEFAULT_AGENT_PROVIDER;
  const value = window.localStorage.getItem(AGENT_PRIMARY_PROVIDER_KEY) || '';
  return /^[a-z0-9][a-z0-9_-]{0,63}$/.test(value) ? value : DEFAULT_AGENT_PROVIDER;
}

export function setPreferredAgentProvider(agentProvider: string): void {
  if (typeof window === 'undefined' || !/^[a-z0-9][a-z0-9_-]{0,63}$/.test(agentProvider)) return;
  window.localStorage.setItem(AGENT_PRIMARY_PROVIDER_KEY, agentProvider);
  rememberAgentProvider(agentProvider);
  window.dispatchEvent(new CustomEvent('alpha-nexus-egolite:agent-provider-changed', {
    detail: { providerID: agentProvider },
  }));
}

export function clampAgentDrawerWidth(width: number, viewportWidth: number): number {
  const requested = Number.isFinite(width) ? Math.round(width) : AGENT_PANEL_DEFAULT_WIDTH;
  const viewportCap = Math.max(
    AGENT_PANEL_MIN_WIDTH,
    Math.min(AGENT_PANEL_MAX_WIDTH, Math.round(viewportWidth) - AGENT_PANEL_RESERVED_WIDTH),
  );
  return Math.max(AGENT_PANEL_MIN_WIDTH, Math.min(viewportCap, requested));
}

function defaultAgentPanelWidth(): number {
  const viewportWidth = typeof window === 'undefined' ? 1440 : window.innerWidth;
  return clampAgentDrawerWidth(AGENT_PANEL_DEFAULT_WIDTH, viewportWidth);
}

function storedAgentDrawerWidth(): number {
  if (typeof window === 'undefined') return defaultAgentPanelWidth();
  const stored = window.localStorage.getItem(AGENT_PANEL_WIDTH_KEY);
  const value = stored === null ? AGENT_PANEL_DEFAULT_WIDTH : Number(stored);
  return clampAgentDrawerWidth(value, window.innerWidth);
}

export type SidebarState = 'expanded' | 'rail' | 'hidden';

export const SIDEBAR_STATES: SidebarState[] = ['expanded', 'rail', 'hidden'];

function storedSidebarState(): SidebarState {
  if (typeof window === 'undefined') return 'expanded';
  const stored = window.localStorage.getItem(SIDEBAR_STATE_KEY);
  // 只保留两态：展开 / 图标栏。历史值 hidden 一律收敛到 rail，
  // 否则用户点一次折叠就整列消失（被认为「侧栏没了」）。
  if (stored === 'expanded' || stored === 'rail') return stored;
  if (stored === 'hidden') return 'rail';
  if (window.localStorage.getItem(SIDEBAR_HIDDEN_KEY) === 'true') return 'rail';
  return 'expanded';
}

interface ToastState {
  message: string;
  type: 'success' | 'error' | 'info' | '';
  show: (message: string, type?: 'success' | 'error' | 'info') => void;
  hide: () => void;
}

export interface HeaderExtras {
  subtitle?: React.ReactNode;
  actions?: React.ReactNode;
}

interface UIStore {
  productMode: ProductMode;
  setProductMode: (mode: ProductMode) => void;
  activeTab: TabType;
  setActiveTab: (tab: TabType) => void;
  headerExtras: HeaderExtras | null;
  setHeaderExtras: (extras: HeaderExtras | null) => void;
  requestedPublishRecord: { relatedType: 'publish_job' | 'publish_plan'; relatedID: number } | null;
  requestPublishRecord: (target: { relatedType: 'publish_job' | 'publish_plan'; relatedID: number } | null) => void;
  // 手动交接发布：社交内容库把某条内容（可选带上当前筛选账号）交给人工驱动的发布流程预选，
  // 只做预选与跳转，不做任何自动提交。
  requestedPublishHandoff: { contentID: number; accountID: number | null } | null;
  requestPublishHandoff: (target: { contentID: number; accountID: number | null } | null) => void;
  agentDraft: AgentDraft | null;
  setAgentDraft: (draft: AgentDraft) => void;
  clearAgentDraft: () => void;
  /** Bot 页面当前选中的 Bot（右侧详情面板用）。 */
  activeBotID: string;
  setActiveBotID: (id: string) => void;
  /** Bot 页右栏是否显示收件箱；左侧列表永不因此折叠。 */
  botInboxOpen: boolean;
  setBotInboxOpen: (open: boolean) => void;
  /** Bot 详情右栏当前的页：对话区「电脑」按钮要能直接切到电脑页。 */
  botDetailTab: 'computer' | 'advanced';
  setBotDetailTab: (tab: 'computer' | 'advanced') => void;
  requestedAgentConversationID: string | null;
  requestAgentConversation: (conversationID: string | null) => void;
  agentDrawerOpen: boolean;
  agentDrawerWidth: number;
  sidebarState: SidebarState;
  toggleSidebar: () => void;
  openAgentDrawer: () => void;
  closeAgentDrawer: () => void;
  toggleAgentDrawer: () => void;
  setAgentDrawerWidth: (width: number) => void;
  searchQuery: string;
  setSearchQuery: (query: string) => void;
  selectedAccountIds: number[];
  setSelectedAccountIds: (ids: number[]) => void;
  toggleAccountSelection: (id: number) => void;
  clearSelection: () => void;
  toast: ToastState;
  radarContentView: 'keywords' | 'videos' | 'creators';
  setRadarContentView: (view: 'keywords' | 'videos' | 'creators') => void;
  selectedCreatorKey: string;
  setSelectedCreatorKey: (key: string) => void;
}

const recoveryState = readRecoveryState();
const initialRoute = typeof window !== 'undefined' ? parseRoute(window.location.pathname) : null;
const initialProductMode = (!SHOP_MODE_ENABLED && initialRoute?.mode === 'shop')
  ? 'media'
  : initialRoute?.mode ?? getStoredProductMode();

export const useUIStore = create<UIStore>((set) => ({
  productMode: initialProductMode,
  setProductMode: (mode) => set((state) => {
    if (mode === 'shop' && !SHOP_MODE_ENABLED) return state;
    // Leaving the Bot page: a product-mode click must return to that mode's
    // default surface, not stay on the persona list. Clicking the mode that is
    // already active still has to leave Bot, so the early return checks it too.
    const leavingBot = state.activeTab === 'bot';
    if (state.productMode === mode && !leavingBot) return state;
    const nextTab = mode === 'shop' ? 'shop'
      : leavingBot ? 'dashboard'
        : isTabAvailableForMode(mode, state.activeTab) ? state.activeTab : 'dashboard';
    if (nextTab !== state.activeTab && typeof window !== 'undefined') {
      if (!requestWorkspaceChange({ currentTab: state.activeTab, nextTab, productMode: mode })) return state;
    }
    persistProductMode(mode);
    syncURLToTab(nextTab, mode);
    writeRecoveryState({ activeTab: nextTab });
    return { productMode: mode, activeTab: nextTab, headerExtras: null };
  }),
  // URL 优先：直接访问 /trend/creators 等深链时必须落在该页面，
  // 只有 URL 无有效路由时才回退到 window.name 的恢复状态。
  activeTab: (!SHOP_MODE_ENABLED && initialRoute?.mode === 'shop')
    ? 'dashboard'
    : (initialRoute?.tab as TabType) || recoveryState.activeTab || 'dashboard',
  setActiveTab: (tab) => set((state) => {
    const requestedTab = tab === 'terminal' ? 'settings' : tab;
    const nextTab = isTabAvailableForMode(state.productMode, requestedTab)
      ? requestedTab
      : state.productMode === 'shop' ? 'shop' : 'dashboard';
    if (nextTab !== state.activeTab && typeof window !== 'undefined') {
      if (!requestWorkspaceChange({ currentTab: state.activeTab, nextTab })) return state;
    }
    const agentDrawerOpen = tab === 'agent'
      ? true
      : state.agentDrawerOpen;
    syncURLToTab(nextTab, state.productMode);
    writeRecoveryState({ activeTab: nextTab, agentDrawerOpen });
    return {
      activeTab: nextTab,
      agentDrawerOpen,
      ...(nextTab !== state.activeTab ? { headerExtras: null } : {}),
    };
  }),
  headerExtras: null,
  setHeaderExtras: (headerExtras) => set({ headerExtras }),
  requestedPublishRecord: null,
  requestPublishRecord: (requestedPublishRecord) => set({ requestedPublishRecord }),
  requestedPublishHandoff: null,
  requestPublishHandoff: (requestedPublishHandoff) => set({ requestedPublishHandoff }),
  agentDraft: null,
  setAgentDraft: (agentDraft) => set({ agentDraft }),
  clearAgentDraft: () => set({ agentDraft: null }),
  requestedAgentConversationID: null,
  requestAgentConversation: (requestedAgentConversationID) => set({ requestedAgentConversationID }),
  activeBotID: '',
  setActiveBotID: (activeBotID) => set({ activeBotID }),
  botInboxOpen: false,
  setBotInboxOpen: (botInboxOpen) => set({ botInboxOpen }),
  botDetailTab: 'computer',
  setBotDetailTab: (botDetailTab) => set({ botDetailTab }),
  agentDrawerOpen: recoveryState.agentDrawerOpen || false,
  agentDrawerWidth: storedAgentDrawerWidth(),
  // 收纳键循环：expanded -> rail -> hidden -> expanded
  sidebarState: storedSidebarState(),
  toggleSidebar: () => set((state) => {
    // 两态：展开 ⇄ 图标栏。不再有第三态「完全隐藏」，避免误点后整栏消失。
    const order: SidebarState[] = ['expanded', 'rail'];
    const next = order[(order.indexOf(state.sidebarState) + 1) % order.length];
    if (typeof window !== 'undefined') window.localStorage.setItem(SIDEBAR_STATE_KEY, next);
    return { sidebarState: next };
  }),
  openAgentDrawer: () => {
    writeRecoveryState({ activeTab: 'agent', agentDrawerOpen: true });
    set({ activeTab: 'agent', agentDrawerOpen: true });
  },
  closeAgentDrawer: () => {
    writeRecoveryState({ agentDrawerOpen: false });
    set({ agentDrawerOpen: false });
  },
  toggleAgentDrawer: () => set((state) => {
    const agentDrawerOpen = !state.agentDrawerOpen;
    writeRecoveryState({ agentDrawerOpen });
    return { agentDrawerOpen };
  }),
  setAgentDrawerWidth: (width) => {
    const viewportWidth = typeof window === 'undefined' ? 1440 : window.innerWidth;
    const next = clampAgentDrawerWidth(width, viewportWidth);
    if (typeof window !== 'undefined') window.localStorage.setItem(AGENT_PANEL_WIDTH_KEY, String(next));
    set({ agentDrawerWidth: next });
  },
  searchQuery: '',
  setSearchQuery: (query) => set({ searchQuery: query }),
  selectedAccountIds: [],
  setSelectedAccountIds: (ids) => set({ selectedAccountIds: ids }),
  toggleAccountSelection: (id) =>
    set((state) => ({
      selectedAccountIds: state.selectedAccountIds.includes(id)
        ? state.selectedAccountIds.filter((x) => x !== id)
        : [...state.selectedAccountIds, id],
    })),
  clearSelection: () => set({ selectedAccountIds: [] }),
  toast: {
    message: '',
    type: '',
    // 轮询/重试会重复报同一条错误：1.5 秒内相同文案只显示一次，避免闪烁。
    show: (message, type = 'info') => {
      const now = Date.now();
      if (message === lastToastMessage && now - lastToastAt < 1500) return;
      lastToastMessage = message;
      lastToastAt = now;
      set((state) => ({
        toast: { ...state.toast, message, type },
      }));
    },
    hide: () =>
      set((state) => ({
        toast: { ...state.toast, message: '', type: '' },
      })),
  },
  radarContentView: getRadarViewFromPathname() ?? 'keywords',
  setRadarContentView: (view) => {
    syncRadarViewURL(view);
    // 离开博主监控页时清空选中，避免下次回来还停在某个文件夹。
    set({ radarContentView: view, ...(view !== 'creators' ? { selectedCreatorKey: '' } : {}) });
  },
  // 深链直达 /trend/creators/{platform}/{id}：启动时即从 URL 还原选中博主。
  selectedCreatorKey: getCreatorKeyFromPathname() ?? '',
  setSelectedCreatorKey: (key) => {
    syncCreatorURL(key);
    set({ selectedCreatorKey: key });
  },
}));


if (typeof window !== 'undefined') {
  window.addEventListener('popstate', () => {
    const tabFromURL = getTabFromPathname();
    if (tabFromURL) {
      useUIStore.getState().setActiveTab(tabFromURL);
    }
    const radarView = getRadarViewFromPathname();
    // 前进/后退到某个博主深链时，从 URL 还原选中；回到列表则清空。
    useUIStore.setState({
      ...(radarView ? { radarContentView: radarView } : {}),
      selectedCreatorKey: getCreatorKeyFromPathname() ?? '',
    });
  });
  // Initial sync on page boot
  const radarViewFromURL = getRadarViewFromPathname();
  if (radarViewFromURL) {
    useUIStore.setState({ radarContentView: radarViewFromURL });
  }
  const routeFromURL = parseRoute(window.location.pathname);
  if (routeFromURL) {
    // Shop 未开放：/shop/* 直接回到 Media 首页，不把用户留在没有入口的模式里。
    if (!SHOP_MODE_ENABLED && routeFromURL.mode === 'shop') {
      persistProductMode('media');
      useUIStore.setState({ activeTab: 'dashboard', productMode: 'media' });
      syncURLToTab('dashboard', 'media');
    } else {
      const mode = (routeFromURL.mode ?? useUIStore.getState().productMode) as ProductMode;
      persistProductMode(mode);
      useUIStore.setState({ activeTab: routeFromURL.tab as TabType, productMode: mode });
      syncURLToTab(routeFromURL.tab as TabType, mode);
    }
  } else {
    syncURLToTab(useUIStore.getState().activeTab, useUIStore.getState().productMode);
  }
}

let lastToastMessage = '';
let lastToastAt = 0;
