/**
 * 演示数据集：在后端还没有真实数据时，让账号页和发布记录页有内容可看。
 *
 * 这里刻意区分「实测」和「演示」：
 *
 * - **TikTok 四个账号是真实的**。handle、主页名称、粉丝/获赞数来自 2026-11-21 对
 *   tiktok.com 的实测采集（见 hackathon/docs/TIKTOK_ACCOUNTS_AUDIT_REPORT.md），
 *   头像从主页原图下载到 public/demo/，不依赖会过期的 CDN 链接。
 * - **其余国家矩阵与全部发布记录是演示数据**。冠英实际出口 30+ 国家，但本机数据库
 *   里只有这些账号的占位，为了让界面能演示「按市场分矩阵」的样子才造出来。它们
 *   不代表平台已开通，也不构成发布成功证据。
 *
 * 只有真实数据为空时才启用，绝不覆盖已有数据；界面上带「演示数据」标记。
 */
import type { Account, PublishContentRecord } from '../api';

/** 页面顶部与卡片上统一的演示标签。 */
export const DEMO_BADGE = '演示数据';

/** 演示模式的开关值；写进 localStorage 后下次打开仍生效。 */
const DEMO_FLAG_KEY = 'alpha-nexus-gtm-demo-mode';

/**
 * 演示数据必须显式打开，默认关闭。
 *
 * 理由：空的账号库和「有数据」是两种状态，界面上必须能区分。默认塞满演示数据
 * 会让「还没有账号」这句话永远不出现，也无法判断本机到底有没有真账号——这正是
 * 现有测试在守的行为。演示时用 `?demo=1` 或设置里的开关打开。
 *
 * 服务端渲染与没有 window 的环境一律视为关闭。
 */
export function isDemoModeEnabled(): boolean {
  if (typeof window === 'undefined') return false;
  try {
    const flag = new URLSearchParams(window.location.search).get('demo');
    if (flag === '1') {
      window.localStorage.setItem(DEMO_FLAG_KEY, '1');
      return true;
    }
    if (flag === '0') {
      window.localStorage.removeItem(DEMO_FLAG_KEY);
      return false;
    }
    return window.localStorage.getItem(DEMO_FLAG_KEY) === '1';
  } catch {
    // localStorage 被禁用时退回只看 URL 参数。
    return new URLSearchParams(window.location.search).get('demo') === '1';
  }
}

/**
 * 演示用的市场矩阵。
 *
 * 按国家命名而不是「浏览器1」：矩阵是按市场分的，名字本身就说明这一列服务哪个
 * 市场。名单取自冠英对外披露的主要出口区域（东南亚、中东、南美、非洲、俄语区）。
 *
 * 第一个矩阵是「越南」，因为本机默认那条矩阵已经按这个名字建好了；下面各条由
 * 演示数据临时补出，id 为负数，不与真实矩阵冲突。
 */
export interface DemoIdentity {
  id: number;
  name: string;
}

export const DEMO_IDENTITIES: DemoIdentity[] = [
  { id: -9001, name: '印尼' },
  { id: -9002, name: '沙特' },
  { id: -9003, name: '巴西' },
  { id: -9004, name: '尼日利亚' },
  { id: -9005, name: '俄罗斯' },
];

interface DemoAccountSpec {
  id: number;
  identityID: number;
  platformCode: string;
  name: string;
  identifier: string;
  avatar: string;
  accountGroup?: string;
  remark: string;
  /** 实测账号填 true；布局占位用 false，界面据此标注。 */
  measured: boolean;
}

/**
 * 泰国/越南等市场的账号名按真实外贸习惯起：账号名多半就是业务员名字或品牌词，
 * 不用「示例账号」这种一眼假的写法。
 */
const DEMO_ACCOUNTS: DemoAccountSpec[] = [
  // —— 越南：四个实测 TikTok 账号（挂在默认矩阵上） ——
  {
    id: -1001,
    identityID: 0,
    platformCode: 'tiktok',
    name: 'KOOYING',
    identifier: '@kooying.autoparts',
    avatar: '/demo/tiktok-kooying.autoparts.jpg',
    remark: '主力英文号 · 2423 粉 / 2.66M 播放（2023-10 后停更）',
    measured: true,
  },
  {
    id: -1002,
    identityID: 0,
    platformCode: 'tiktok',
    name: 'Kooying brake factory boss',
    identifier: '@kooying.brake.fact',
    avatar: '/demo/tiktok-kooying.brake.fact.jpg',
    remark: '老板娘 Sophia · 73 粉 · 含 WhatsApp',
    measured: true,
  },
  {
    id: -1003,
    identityID: 0,
    platformCode: 'tiktok',
    name: 'kooying_judy26',
    identifier: '@kooying_judy26',
    avatar: '/demo/tiktok-kooying_judy26.jpg',
    remark: '外贸 Judy · 3 粉 · Bio 挂官网',
    measured: true,
  },
  {
    id: -1004,
    identityID: 0,
    platformCode: 'tiktok',
    name: 'Kooying brake lining',
    identifier: '@kooying.brake.lin',
    avatar: '/demo/tiktok-kooying.brake.lin.jpg',
    remark: '备用号 · 0 粉 · 1 条作品',
    measured: true,
  },

  // —— 印尼：东南亚主力，TikTok + Facebook ——
  { id: -1101, identityID: -9001, platformCode: 'tiktok', name: 'kooying.indonesia', identifier: '@kooying.id', avatar: '/demo/kooying-logo.png', remark: '印尼语号 · 演示', measured: false },
  { id: -1102, identityID: -9001, platformCode: 'meta', name: 'Kooying Rem Indonesia', identifier: '演示占位', avatar: '/demo/kooying-logo.png', remark: 'Meta 主页 · 演示', measured: false },
  { id: -1103, identityID: -9001, platformCode: 'youtube', name: 'Kooying Parts ID', identifier: '演示占位', avatar: '/demo/kooying-logo.png', remark: 'YouTube 频道 · 演示', measured: false },

  // —— 沙特：中东高温市场，主打矿山长坡配方 ——
  { id: -1201, identityID: -9002, platformCode: 'meta', name: 'Kooying Middle East', identifier: '演示占位', avatar: '/demo/kooying-logo.png', remark: 'Meta 主页 · 演示', measured: false },
  { id: -1202, identityID: -9002, platformCode: 'x', name: 'kooying_gcc', identifier: '@kooying_gcc', avatar: '/demo/kooying-logo.png', remark: 'X 帐号 · 演示', measured: false },
  { id: -1203, identityID: -9002, platformCode: 'youtube', name: 'Kooying GCC', identifier: '演示占位', avatar: '/demo/kooying-logo.png', remark: 'YouTube 频道 · 演示', measured: false },

  // —— 巴西：南美，葡语 ——
  { id: -1301, identityID: -9003, platformCode: 'tiktok', name: 'kooying.brasil', identifier: '@kooying.brasil', avatar: '/demo/kooying-logo.png', remark: 'TikTok · 演示', measured: false },
  { id: -1302, identityID: -9003, platformCode: 'meta', name: 'Kooying Brasil', identifier: '演示占位', avatar: '/demo/kooying-logo.png', remark: 'Meta 主页 · 演示', measured: false },
  { id: -1303, identityID: -9003, platformCode: 'youtube', name: 'Kooying Brasil', identifier: '演示占位', avatar: '/demo/kooying-logo.png', remark: 'YouTube 频道 · 演示', measured: false },

  // —— 尼日利亚：非洲西海岸 ——
  { id: -1401, identityID: -9004, platformCode: 'meta', name: 'Kooying Nigeria', identifier: '演示占位', avatar: '/demo/kooying-logo.png', remark: 'Meta 主页 · 演示', measured: false },
  { id: -1402, identityID: -9004, platformCode: 'tiktok', name: 'kooying.ng', identifier: '@kooying.ng', avatar: '/demo/kooying-logo.png', remark: 'TikTok · 演示', measured: false },

  // —— 俄罗斯：俄语区，HOWO / Shacman 为主 ——
  { id: -1501, identityID: -9005, platformCode: 'youtube', name: 'Kooying Russia', identifier: '演示占位', avatar: '/demo/kooying-logo.png', remark: 'YouTube 频道 · 演示', measured: false },
  { id: -1502, identityID: -9005, platformCode: 'meta', name: 'Kooying Russia', identifier: '演示占位', avatar: '/demo/kooying-logo.png', remark: 'Meta 主页 · 演示', measured: false },
];

/**
 * 把演示矩阵挂到真实矩阵之后。
 *
 * 真实矩阵（本机已建的那条，通常按市场命名，例如「越南」）保留原 id，四个实测
 * TikTok 账号挂到它上面；其余市场用负数 id 追加，界面上带演示标记。
 * 没有真实矩阵时只返回空数组——不该为了演示凭空造一个默认浏览器。
 */
export function demoIdentitiesFor<T extends { id: number }>(real: T[], fallbackName: string): Array<T | DemoIdentity> {
  if (real.length === 0) return real;
  const primary = real[0];
  void fallbackName;
  return [primary, ...DEMO_IDENTITIES];
}

export function demoAccountsFor(identities: Array<{ id: number }>): Account[] {
  const primaryIdentityID = identities[0]?.id;
  if (!primaryIdentityID) return [];
  const known = new Set(identities.map((identity) => identity.id));
  return DEMO_ACCOUNTS
    // 实测账号挂在真实矩阵上；演示矩阵必须真的存在才显示。
    .filter((spec) => spec.identityID === 0 || known.has(spec.identityID))
    .map((spec) => toAccount(spec, spec.identityID === 0 ? primaryIdentityID : spec.identityID));
}

function toAccount(spec: DemoAccountSpec, identityId: number): Account {
  return {
    id: spec.id,
    identityId,
    platformCode: spec.platformCode,
    kind: 'social',
    name: spec.name,
    avatar: spec.avatar,
    // 只有实测账号的头像来自主页抓取；演示矩阵用的是官网品牌 logo，
    // 标成 platform_fallback，免得被读成「从平台观察到」。
    avatarSource: spec.avatar ? (spec.measured ? 'observed' : 'platform_fallback') : undefined,
    accountIdentifier: spec.identifier,
    loginState: spec.measured ? 'logged_in' : 'unknown',
    loginMode: 'browser_profile_session',
    authMethod: 'unknown',
    remark: spec.remark,
    accountGroup: spec.accountGroup ?? '',
    enabled: true,
    browserRunning: false,
    browserState: 'stopped',
    fingerprintSeed: 0,
    fingerprintConfig: '',
    tags: [],
  };
}

/** 演示行没有真实 id，用它判断界面该不该隐藏写操作。 */
export function isDemoAccountID(id: number): boolean {
  return id < 0;
}

interface DemoRecordSpec {
  platform: string;
  account: string;
  title: string;
  state: 'published' | 'scheduled';
  /** 相对今天的偏移天数，负数表示过去。 */
  offsetDays: number;
  url: string;
  type: string;
}

/**
 * 发布记录演示数据：只用国外主流媒体，围绕冠英真实产品线。
 *
 * 标题写得像真实外贸运营会发的，而不是「示例标题 1」。状态都标成 published，
 * 但整页带演示标记——它们不是平台核验过的发布证据。
 */
const DEMO_RECORD_SPECS: DemoRecordSpec[] = [
  {
    platform: 'youtube',
    account: 'KOOYING Brake Parts',
    title: 'HOWO 14-hole vs 8-hole brake lining — how to avoid the wrong shipment',
    state: 'published',
    offsetDays: -1,
    url: 'https://www.youtube.com/watch?v=demo-howo-14h',
    type: 'video',
  },
  {
    platform: 'tiktok',
    account: '@kooying.autoparts',
    title: 'Krauss 400°C bench test: 0.040mm wear in 60 seconds',
    state: 'published',
    offsetDays: -2,
    url: 'https://www.tiktok.com/@kooying.autoparts/video/demo-krauss',
    type: 'video',
  },
  {
    platform: 'facebook',
    account: 'Zhejiang Kooying Auto Parts',
    title: 'BPW 19032 8-hole trailer lining — 2026 price list update',
    state: 'published',
    offsetDays: -3,
    url: 'https://www.facebook.com/kooying/posts/demo-bpw19032',
    type: 'image_text',
  },
  {
    platform: 'instagram',
    account: 'kooying.brake',
    title: 'Behind the lining: 20 years on the Nantaihu production line',
    state: 'published',
    offsetDays: -4,
    url: 'https://www.instagram.com/p/demo-nantaihu',
    type: 'image_text',
  },
  {
    platform: 'youtube',
    account: 'KOOYING Brake Parts',
    title: 'Measuring without an OE code — caliper 240 × 220mm to Hande 35T in one pass',
    state: 'published',
    offsetDays: -6,
    url: 'https://www.youtube.com/watch?v=demo-hande-35t',
    type: 'video',
  },
  {
    platform: 'tiktok',
    account: '@kooying.brake.fact',
    title: 'Canton Fair 2026 booth walkthrough with Sophia',
    state: 'scheduled',
    offsetDays: 2,
    url: '',
    type: 'video',
  },
  {
    platform: 'instagram',
    account: 'kooying.brake',
    title: 'WVA 29087 air disc pad — fitment chart carousel',
    state: 'scheduled',
    offsetDays: 4,
    url: '',
    type: 'image_text',
  },
];

/** 相对 now 生成确定的时间戳：同一次演示刷新不会跳来跳去。 */
function offsetISO(now: Date, days: number): string {
  return new Date(now.getTime() + days * 86_400_000).toISOString();
}

/** 生成演示发布记录；已验证时间与排期时间按偏移算好。 */
export function demoPublishContentRecords(now: Date = new Date()): PublishContentRecord[] {
  return DEMO_RECORD_SPECS.map((spec, index) => ({
    id: -2001 - index,
    job_id: 0,
    account_id: 0,
    platform_code: spec.platform,
    content_type: spec.type,
    account_name: spec.account,
    title: spec.title,
    remote_state: spec.state,
    scheduled_at: offsetISO(now, spec.offsetDays),
    post_url: spec.url,
    external_post_id: spec.url ? `demo-${index}` : '',
    error_code: '',
    verified_at: spec.state === 'published' ? offsetISO(now, spec.offsetDays) : '',
    deleted_at: '',
  }));
}

/** 演示记录同样没有真实 id。 */
export function isDemoRecordID(id: number): boolean {
  return id < 0;
}
