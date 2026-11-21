import type { ProductMode } from '../workspaceMode';

// 地址栏必须能独立说明「哪个模式 + 哪个板块」：
//   /media/accounts  → Media 模式 · 浏览器与账号
//   /shop/accounts   → Shop 模式 · 浏览器与账号
//   /media/content   → Media 模式 · 内容
//   /shop/content    → Shop 模式 · 内容
//   /bot             → 跨模式，保持独立
//
// 只有 bot / agent 不套模式前缀；其余板块都跟着当前模式走。
const SECTION_SEGMENT: Record<string, string> = {
  home: '',
  shop: '',
  'social-media': '',
  contents: 'content',
  templates: 'templates',
  studio: 'seo',
  knowledge: 'knowledge',
  memory: 'memory',
  dashboard: 'trend',
  crawler: 'crawler',
  'xhs-data': 'analytics',
  publish: 'publish',
  records: 'publish/records',
  metrics: 'publish/metrics',
  batch: 'publish/batch',
  notifications: 'publish/notifications',
  calendar: 'calendar',
  logs: 'logs',
  monitor: 'monitor',
  settings: 'settings',
  proxies: 'settings/proxies',
  skills: 'settings/skills',
  accounts: 'accounts',
  fleet: 'accounts/spaces',
};

const SEGMENT_TAB: Record<string, string> = Object.fromEntries(
  Object.entries(SECTION_SEGMENT)
    .filter(([, segment]) => segment !== '')
    .map(([tab, segment]) => [segment, tab]),
);
SEGMENT_TAB.media = 'social-media';
SEGMENT_TAB.studio = 'studio';
SEGMENT_TAB.seo = 'studio';

// 旧地址仍然可用，避免历史书签失效。
const LEGACY_ROUTES: Record<string, string> = {
  // Media 首页已删：/ 与 /home 落到趋势雷达（新的默认工作区）。
  '/': 'dashboard',
  '/home': 'dashboard',
  '/trend': 'dashboard',
  '/trend/keywords': 'dashboard',
  '/trend/videos': 'dashboard',
  '/trend/creators': 'dashboard',
  '/trend-radar': 'dashboard',
  '/dashboard': 'dashboard',
  '/crawler': 'crawler',
  '/logs': 'logs',
  '/monitor': 'monitor',
  '/memory': 'memory',
  '/cognitive-memory': 'memory',
  '/accounts': 'accounts',
  '/accounts/spaces': 'fleet',
  '/fleet': 'fleet',
  '/media': 'social-media',
  '/media/content': 'contents',
  '/content': 'contents',
  '/contents': 'contents',
  '/media/templates': 'templates',
  '/templates': 'templates',
  '/knowledge': 'dashboard',
  '/analytics': 'xhs-data',
  '/xhs-data': 'xhs-data',
  '/metrics': 'metrics',
  '/publish/metrics': 'metrics',
  '/records': 'records',
  '/publish/records': 'records',
  '/publish/batch': 'batch',
  '/batch': 'batch',
  '/publish/notifications': 'notifications',
  '/notifications': 'notifications',
  '/calendar': 'calendar',
  '/settings': 'settings',
  // 设置分节走同一条 tab；具体哪一节由 EgoSettingsView 读地址栏决定。
  '/settings/storage': 'settings',
  '/settings/memory': 'settings',
  '/settings/logs': 'settings',
  '/settings/about': 'settings',
  '/settings/runtime': 'settings',
  '/settings/feishu': 'settings',
  '/settings/agent': 'settings',
  '/settings/personagent': 'settings',
  '/settings/appearance': 'settings',
  '/settings/shortcuts': 'settings',
  '/settings/account': 'settings',
  '/settings/proxies': 'proxies',
  '/proxies': 'proxies',
  '/settings/skills': 'skills',
  '/skills': 'skills',
  '/agent': 'agent',
  '/bot': 'bot',
  '/shop': 'shop',
  '/studio': 'studio',
  '/seo': 'studio',
  '/publish': 'publish',
};

export interface ParsedRoute {
  tab: string;
  mode?: ProductMode;
}

/** 当前 tab + 模式 → 地址栏路径。 */
export function routeForTab(tab: string, mode: ProductMode): string {
  // Bot 是跨模式入口：/bot 列表 + /bot/{id} 会话；不再单独开 /agent。
  if (tab === 'bot' || tab === 'agent') return '/bot';
  if (mode === 'shop') {
    if (tab === 'shop') return '/shop';
    // 店铺模式下的「内容库」和「内容」必须分开。
    if (tab === 'social-media') return '/shop/media';
    const segment = SECTION_SEGMENT[tab];
    return segment ? `/shop/${segment}` : '/shop';
  }
  if (tab === 'social-media') return '/GTM';
  const segment = SECTION_SEGMENT[tab];
  return segment ? `/GTM/${segment}` : '/';
}

/** 地址栏路径 → tab（能带出模式就带出）。 */
export function parseRoute(pathname: string): ParsedRoute | null {
  const raw = (pathname || '/').replace(/\/+$/, '') || '/';
  // 旧 /media 与 /SM 前缀仍可打开，新地址统一写 /GTM。
  const path = raw
    .replace(/^\/media(?=\/|$)/i, '/GTM')
    .replace(/^\/sm(?=\/|$)/i, '/GTM')
    .replace(/^\/gtm(?=\/|$)/i, '/GTM');

  if (path === '/bot') return { tab: 'bot' };
  if (path === '/agent') return { tab: 'bot' };

  if (path === '/shop' || path.startsWith('/shop/')) {
    if (path === '/shop') return { tab: 'shop', mode: 'shop' };
    const segment = path.slice('/shop/'.length);
    if (segment === 'settings' || segment.startsWith('settings/')) return { tab: 'settings', mode: 'shop' };
    const tab = SEGMENT_TAB[segment] ?? (segment.startsWith('trend') ? 'dashboard' : undefined);
    return tab ? { tab, mode: 'shop' } : null;
  }
  if (path === '/GTM' || path.startsWith('/GTM/')) {
    if (path === '/GTM') return { tab: 'social-media', mode: 'media' };
    const segment = path.slice('/GTM/'.length);
    if (segment === 'settings' || segment.startsWith('settings/')) return { tab: 'settings', mode: 'media' };
    const tab = SEGMENT_TAB[segment] ?? (segment.startsWith('trend') ? 'dashboard' : undefined);
    return tab ? { tab, mode: 'media' } : null;
  }

  const legacy = LEGACY_ROUTES[raw] ?? LEGACY_ROUTES[path];
  return legacy ? { tab: legacy } : null;
}
