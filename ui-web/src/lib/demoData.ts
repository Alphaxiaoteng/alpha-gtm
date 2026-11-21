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
import type { Account, PublishContentRecord, TrendRadarSignal } from '../api';

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

/** 演示矩阵统一用处理成方形的品牌 logo，避免横版图被裁成色块。 */
const LOGO = '/demo/kooying-logo-square.jpg';

/**
 * 每个市场一组出口 IP 池。
 *
 * 真实矩阵里同一国家的多个账号通常走同一批落地 IP，避免账号在地区之间跳。
 * 这里给每个市场一组固定演示 IP，界面用来展示「账号归属哪个池」。
 * 地址取自文档专用保留段，不会指向真实主机。
 */
export const DEMO_IP_POOLS: Record<string, { label: string; subnet: string; exits: string[] }> = {
  '-9001': { label: '印尼 · 雅加达', subnet: '198.51.100.0/24', exits: ['198.51.100.11', '198.51.100.12'] },
  '-9002': { label: '沙特 · 达曼', subnet: '198.51.100.0/24', exits: ['198.51.100.21', '198.51.100.22'] },
  '-9003': { label: '巴西 · 桑托斯', subnet: '198.51.100.0/24', exits: ['198.51.100.31', '198.51.100.32'] },
  '-9004': { label: '尼日利亚 · 拉各斯', subnet: '203.0.113.0/24', exits: ['203.0.113.41'] },
  '-9005': { label: '俄罗斯 · 莫斯科', subnet: '203.0.113.0/24', exits: ['203.0.113.51'] },
};

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
  // —— 越南：4 个实测 TikTok 账号（挂在默认矩阵上） ——
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

  // —— 其余市场：每个矩阵固定 4 个平台，TikTok / YouTube / Facebook / Instagram，
  // 与越南的账号数一致，也覆盖实际投放的四个主力渠道。 ——
  { id: -1101, identityID: -9001, platformCode: 'tiktok', name: 'kooying.indonesia', identifier: '@kooying.id', avatar: LOGO, remark: 'TikTok · 演示', measured: false },
  { id: -1102, identityID: -9001, platformCode: 'youtube', name: 'Kooying Parts Indonesia', identifier: '演示占位', avatar: LOGO, remark: 'YouTube · 演示', measured: false },
  { id: -1103, identityID: -9001, platformCode: 'facebook', name: 'Kooying Rem Indonesia', identifier: '演示占位', avatar: LOGO, remark: 'Facebook 主页 · 演示', measured: false },
  { id: -1104, identityID: -9001, platformCode: 'instagram', name: 'kooying.indonesia', identifier: '@kooying.indonesia', avatar: LOGO, remark: 'Instagram · 演示', measured: false },

  { id: -1201, identityID: -9002, platformCode: 'tiktok', name: 'kooying.arabia', identifier: '@kooying.arabia', avatar: LOGO, remark: 'TikTok · 演示', measured: false },
  { id: -1202, identityID: -9002, platformCode: 'youtube', name: 'Kooying Middle East', identifier: '演示占位', avatar: LOGO, remark: 'YouTube · 演示', measured: false },
  { id: -1203, identityID: -9002, platformCode: 'facebook', name: 'Kooying Middle East', identifier: '演示占位', avatar: LOGO, remark: 'Facebook 主页 · 演示', measured: false },
  { id: -1204, identityID: -9002, platformCode: 'instagram', name: 'kooying.me', identifier: '@kooying.me', avatar: LOGO, remark: 'Instagram · 演示', measured: false },

  { id: -1301, identityID: -9003, platformCode: 'tiktok', name: 'kooying.brasil', identifier: '@kooying.brasil', avatar: LOGO, remark: 'TikTok · 演示', measured: false },
  { id: -1302, identityID: -9003, platformCode: 'youtube', name: 'Kooying Brasil', identifier: '演示占位', avatar: LOGO, remark: 'YouTube · 演示', measured: false },
  { id: -1303, identityID: -9003, platformCode: 'facebook', name: 'Kooying Brasil', identifier: '演示占位', avatar: LOGO, remark: 'Facebook 主页 · 演示', measured: false },
  { id: -1304, identityID: -9003, platformCode: 'instagram', name: 'kooying.brasil', identifier: '@kooying.brasil', avatar: LOGO, remark: 'Instagram · 演示', measured: false },

  { id: -1401, identityID: -9004, platformCode: 'tiktok', name: 'kooying.ng', identifier: '@kooying.ng', avatar: LOGO, remark: 'TikTok · 演示', measured: false },
  { id: -1402, identityID: -9004, platformCode: 'youtube', name: 'Kooying Nigeria', identifier: '演示占位', avatar: LOGO, remark: 'YouTube · 演示', measured: false },
  { id: -1403, identityID: -9004, platformCode: 'facebook', name: 'Kooying Nigeria', identifier: '演示占位', avatar: LOGO, remark: 'Facebook 主页 · 演示', measured: false },
  { id: -1404, identityID: -9004, platformCode: 'instagram', name: 'kooying.ng', identifier: '@kooying.ng', avatar: LOGO, remark: 'Instagram · 演示', measured: false },

  { id: -1501, identityID: -9005, platformCode: 'tiktok', name: 'kooying.russia', identifier: '@kooying.ru', avatar: LOGO, remark: 'TikTok · 演示', measured: false },
  { id: -1502, identityID: -9005, platformCode: 'youtube', name: 'Kooying Russia', identifier: '演示占位', avatar: LOGO, remark: 'YouTube · 演示', measured: false },
  { id: -1503, identityID: -9005, platformCode: 'facebook', name: 'Kooying Russia', identifier: '演示占位', avatar: LOGO, remark: 'Facebook 主页 · 演示', measured: false },
  { id: -1504, identityID: -9005, platformCode: 'instagram', name: 'kooying.ru', identifier: '@kooying.ru', avatar: LOGO, remark: 'Instagram · 演示', measured: false },
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
  // 按矩阵分配出口 IP：同矩阵下的账号共享一组池，各自错开一条线路。
  const pool = DEMO_IP_POOLS[String(spec.identityID)];
  const exit = pool ? pool.exits[Math.abs(spec.id) % pool.exits.length] : '';
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
    proxyServer: exit ? `http://${exit}:8080` : undefined,
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
  /** 封面用真实产品/工厂实拍，不用平台图标凑数。 */
  cover: string;
}

/**
 * 发布记录演示数据：只用国外主流媒体，围绕冠英真实产品线。
 *
 * 标题写得像真实外贸运营会发的，而不是「示例标题 1」。状态都标成 published，
 * 但整页带演示标记——它们不是平台核验过的发布证据。
 */
const DEMO_RECORD_SPECS: DemoRecordSpec[] = [
  // 覆盖视频 / 图文 / 文章三种内容类型，平台只用国外主流媒体。
  {
    platform: 'youtube',
    account: 'KOOYING Brake Parts',
    title: 'HOWO 14-hole vs 8-hole brake lining — how to avoid the wrong shipment',
    state: 'published',
    offsetDays: -1,
    url: 'https://www.youtube.com/watch?v=demo-howo-14h',
    type: 'video',
    cover: '/demo/media-covers/lining-manufacture.jpg',
  },
  {
    platform: 'tiktok',
    account: '@kooying.autoparts',
    title: 'Krauss 400°C bench test: 0.040mm wear in 60 seconds',
    state: 'published',
    offsetDays: -2,
    url: 'https://www.tiktok.com/@kooying.autoparts/video/demo-krauss',
    type: 'video',
    cover: '/demo/media-covers/source-factory.jpg',
  },
  {
    platform: 'facebook',
    account: 'Zhejiang Kooying Auto Parts',
    title: 'BPW 19032 8-hole trailer lining — 2026 price list update',
    state: 'published',
    offsetDays: -3,
    url: 'https://www.facebook.com/kooying/posts/demo-bpw19032',
    type: 'image_text',
    cover: '/demo/media-covers/workshop.jpg',
  },
  {
    platform: 'instagram',
    account: 'kooying.brake',
    title: 'Behind the lining: 20 years on the Nantaihu production line',
    state: 'published',
    offsetDays: -4,
    url: 'https://www.instagram.com/p/demo-nantaihu',
    type: 'image_text',
    cover: '/demo/media-covers/boss-factory-intro.jpg',
  },
  {
    platform: 'youtube',
    account: 'KOOYING Brake Parts',
    title: 'Measuring without an OE code — caliper 240 × 220mm to Hande 35T in one pass',
    state: 'published',
    offsetDays: -6,
    url: 'https://www.youtube.com/watch?v=demo-hande-35t',
    type: 'video',
    cover: '/demo/media-covers/canton-fair.jpg',
  },
  {
    platform: 'instagram',
    account: 'kooying.brake',
    title: 'WVA 29087 air disc pad — dimension chart carousel',
    state: 'published',
    offsetDays: -8,
    url: 'https://www.instagram.com/p/demo-wva29087',
    type: 'image_text',
    cover: '/demo/media-covers/boss-factory-intro.jpg',
  },
  {
    platform: 'facebook',
    account: 'Zhejiang Kooying Auto Parts',
    title: 'WVA 29030 / 29087 disc pad — IATF16949 & ECE R90 documents',
    state: 'published',
    offsetDays: -10,
    url: 'https://www.facebook.com/kooying/posts/demo-wva29030',
    type: 'image_text',
    cover: '/demo/media-covers/workshop.jpg',
  },
  {
    platform: 'youtube',
    account: 'KOOYING Brake Parts',
    title: 'Batching and mixing: how friction material consistency is held',
    state: 'published',
    offsetDays: -12,
    url: 'https://www.youtube.com/watch?v=demo-batching',
    type: 'video',
    cover: '/demo/media-covers/source-factory.jpg',
  },
  {
    platform: 'tiktok',
    account: '@kooying.brake.fact',
    title: 'Canton Fair 2026 booth walkthrough with Sophia',
    state: 'scheduled',
    offsetDays: 2,
    url: '',
    type: 'video',
    cover: '/demo/media-covers/canton-fair.jpg',
  },
  {
    platform: 'instagram',
    account: 'kooying.brake',
    title: 'WVA 29087 air disc pad — fitment chart carousel',
    state: 'scheduled',
    offsetDays: 4,
    url: '',
    type: 'image_text',
    cover: '/demo/media-covers/lining-manufacture.jpg',
  },
];

/** 相对 now 生成确定的时间戳：同一次演示刷新不会跳来跳去。 */
function offsetISO(now: Date, days: number): string {
  return new Date(now.getTime() + days * 86_400_000).toISOString();
}

/**
 * 演示记录在 PublishContentRecord 之上多带一个本地封面路径。
 *
 * 真实记录的封面走 /api/publish-content-records/{id}/cover，演示行没有后端记录，
 * 所以直接指向 public/demo/covers 下的真实产品/工厂实拍图。
 */
export interface DemoPublishRecord extends PublishContentRecord {
  demo_cover?: string;
}

/** 生成演示发布记录；已验证时间与排期时间按偏移算好。 */
export function demoPublishContentRecords(now: Date = new Date()): DemoPublishRecord[] {
  return DEMO_RECORD_SPECS.map((spec, index): DemoPublishRecord => ({
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
    demo_cover: spec.cover,
  }));
}

/** 演示记录同样没有真实 id。 */
export function isDemoRecordID(id: number): boolean {
  return id < 0;
}

/**
 * 趋势雷达演示信号：热榜榜单常见的内容形态。
 *
 * 标题按「运营真会点开的那类选题」写：具体型号、具体问题、具体工况，而不是
 * 「爆款视频 1」。平台只用国外主流媒体，数据字段留空表示没有实测值，不用编造
 * 播放量冒充真实采集结果。
 */
export function demoRadarSignals(now: Date = new Date()): TrendRadarSignal[] {
  const specs: Array<{
    term: string;
    platform: string;
    title: string;
    creator: string;
    play: number;
    likes: number;
    daysAgo: number;
    cover: string;
  }> = [
    { term: 'WVA 29087', platform: 'youtube', title: 'WVA 29087 vs 29108 — what actually differs on the Knorr SB7000', creator: 'Brake Tech Talks', play: 184_000, likes: 6_200, daysAgo: 1, cover: 'lab' },
    { term: 'HOWO brake', platform: 'youtube', title: 'HOWO 14-hole vs 8-hole lining: the mistake behind full-container returns', creator: 'Heavy Fleet Notes', play: 96_400, likes: 3_100, daysAgo: 2, cover: 'lining-manufacture' },
    { term: 'BPW 19032', platform: 'tiktok', title: 'BPW 19032 in 20 seconds — trailer axle lining swap', creator: 'trailerparts.eu', play: 412_000, likes: 18_900, daysAgo: 1, cover: 'workshop' },
    { term: 'brake lining', platform: 'tiktok', title: 'Why cheap lining wears through in two months on long downhill grades', creator: 'minefleet.cl', play: 731_000, likes: 41_200, daysAgo: 3, cover: 'source-factory' },
    { term: 'brake pad factory', platform: 'youtube', title: 'Inside a friction material batching line — how consistency is held', creator: 'Made In Asia', play: 57_800, likes: 1_450, daysAgo: 5, cover: 'canton-fair' },
    { term: 'ECE R90', platform: 'facebook', title: 'ECE R90 vs IATF16949: which document buyers actually ask for', creator: 'Commercial Parts Weekly', play: 24_300, likes: 780, daysAgo: 2, cover: 'boss-factory-intro' },
    { term: 'brake pad', platform: 'instagram', title: 'Caliper measurement without an OE code — 240 × 220mm to Hande 35T', creator: 'axleworks', play: 88_500, likes: 5_600, daysAgo: 4, cover: 'workshop' },
    { term: 'mercedes 17276', platform: 'youtube', title: 'Mercedes 17276 drum lining — 140mm vs 170mm shoe width', creator: 'Drum Brake Lab', play: 33_200, likes: 890, daysAgo: 6, cover: 'lining-manufacture' },
  ];
  const iso = (days: number) => new Date(now.getTime() - days * 86_400_000).toISOString();
  return specs.map((spec, index) => ({
    niche_id: 'demo-niche',
    niche_name: '重卡刹车片 · 演示热榜',
    search_term: spec.term,
    platform: spec.platform,
    title: spec.title,
    creator: spec.creator,
    observed_at: iso(spec.daysAgo),
    published_at: iso(spec.daysAgo),
    cover_url: `/demo/media-covers/${spec.cover}.jpg`,
    first_seen_at: iso(spec.daysAgo),
    occurrences: 1,
    is_new: spec.daysAgo <= 2,
    play: spec.play,
    likes: spec.likes,
    collects: 0,
    shares: 0,
    demo_rank: index + 1,
  } as TrendRadarSignal & { demo_rank: number }));
}
