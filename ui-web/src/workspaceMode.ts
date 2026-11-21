export type ProductMode = 'media' | 'shop';

export const DEFAULT_PRODUCT_MODE: ProductMode = 'media';
// Shop 功能先隐藏：代码与路由保留，模式入口不显示，等店铺流程补齐后再打开。
export const SHOP_MODE_ENABLED = false;
export const PRODUCT_MODE_STORAGE_KEY = 'alpha-nexus-egolite-product-mode-v1';

export const SHOP_PLATFORM_CODES = [
  'douyin_shop',
  'taobao',
  'tiktok_shop',
  'shopee',
  'shopify',
  'amazon_seller',
  'qianchuan',
  'jd_shop',
  'pinduoduo_shop',
  'xhs_shop',
  'kuaishou_shop',
  'wechat_shop',
  'alibaba_1688',
  'aliexpress',
  'xianyu',
] as const;

const SHOP_PLATFORM_SET = new Set<string>(SHOP_PLATFORM_CODES);
// 'bot' is a cross-mode page: participants in Shop must be able to open it too,
// otherwise setActiveTab silently falls back to 'agent'.
const SHOP_TABS = new Set(['shop', 'accounts', 'fleet', 'social-media', 'contents', 'templates', 'knowledge', 'memory', 'agent', 'bot', 'settings', 'proxies', 'skills', 'logs', 'crawler']);

export function isShopPlatform(platformCode: string): boolean {
  return SHOP_PLATFORM_SET.has(platformCode);
}

export function platformCodesForMode(platformCodes: readonly string[], mode: ProductMode): string[] {
  return [...new Set(platformCodes)].filter((platformCode) => {
    // 闲鱼作为变现与个人店铺兼具的平台，在「自媒体」与「店铺」双模式下均可用
    if (platformCode === 'xianyu') return true;
    return mode === 'shop' ? isShopPlatform(platformCode) : !isShopPlatform(platformCode);
  });
}

export function isTabAvailableForMode(mode: ProductMode, tab: string): boolean {
  return mode === 'media' ? tab !== 'shop' : SHOP_TABS.has(tab);
}

export function getStoredProductMode(): ProductMode {
  if (typeof window === 'undefined') return DEFAULT_PRODUCT_MODE;
  if (!SHOP_MODE_ENABLED) return DEFAULT_PRODUCT_MODE;
  if (new URLSearchParams(window.location.search).get('mode') === 'shop') return 'shop';
  return window.localStorage.getItem(PRODUCT_MODE_STORAGE_KEY) === 'shop' ? 'shop' : DEFAULT_PRODUCT_MODE;
}

export function persistProductMode(mode: ProductMode): void {
  if (typeof window !== 'undefined') window.localStorage.setItem(PRODUCT_MODE_STORAGE_KEY, mode);
}
