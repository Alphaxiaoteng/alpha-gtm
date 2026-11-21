/**
 * Unified design languages — 风格 = 颜色 + 结构 + 字体 一整套。
 *
 * 默认：黑白浅色；历史彩色与深色选择在 v5 统一迁移到黑白工作台。
 */
import type { FontId } from './fonts';
import { applyFont } from './fonts';

export type SkinId = 'hermes' | 'pixel' | 'mario' | 'anime' | 'meishi' | 'claude' | 'alpha';

export interface SkinMeta {
  id: SkinId;
  nameZh: string;
  mode: 'dark' | 'light';
  blurb: string;
  /** 绑定字体包 — 选风格即选字体 */
  font: FontId;
  swatch: { bg: string; surface: string; accent: string };
}

export const SKINS: SkinMeta[] = [
  {
    id: 'hermes',
    nameZh: 'Hermes 官方',
    mode: 'dark',
    blurb: '纯黑 · 中性灰白 · 官方 Agent 前端（默认）',
    font: 'system',
    swatch: { bg: '#111111', surface: '#1a1a1a', accent: '#f3f4f6' },
  },
  {
    id: 'alpha',
    nameZh: '深空玻璃',
    mode: 'dark',
    blurb: '近黑 OLED · 低饱和点缀',
    font: 'atelier',
    swatch: { bg: '#050505', surface: '#0c0c0e', accent: '#8b5cf6' },
  },
  {
    id: 'pixel',
    nameZh: '像素极客',
    mode: 'dark',
    blurb: '纯黑硬边 · 等宽 · 荧光黄点缀',
    font: 'noir',
    swatch: { bg: '#000000', surface: '#121212', accent: '#FFD700' },
  },
  {
    id: 'mario',
    nameZh: '暗黑 Game',
    mode: 'dark',
    blurb: '克制黑灰 · 像素硬边 · 冷色 HUD',
    font: 'noir',
    swatch: { bg: '#050506', surface: '#0d0f12', accent: '#78dce8' },
  },
  {
    id: 'claude',
    nameZh: '黑白工作台',
    mode: 'light',
    blurb: '黑白底 · 清晰层级 · 默认',
    font: 'editorial',
    swatch: { bg: '#FFFFFF', surface: '#FFFFFF', accent: '#111111' },
  },
  {
    id: 'anime',
    nameZh: '动漫软萌',
    mode: 'light',
    blurb: '软粉紫 · 大圆角 · 可选',
    font: 'editorial',
    swatch: { bg: '#FFF0F5', surface: '#FFFFFF', accent: '#E86A9B' },
  },
  {
    id: 'meishi',
    nameZh: '美食手帐',
    mode: 'light',
    blurb: '蜜桃珊瑚 · 可选',
    font: 'editorial',
    swatch: { bg: '#FBF4EC', surface: '#FFF9F3', accent: '#E86F5C' },
  },
];

const STORAGE_KEY = 'alpha-nexus-egolite-skin';
const DEFAULT_SKIN: SkinId = 'claude';
/** v6 retired the forced-light migration and lets the user keep any choice. */
const SKIN_REV_KEY = 'alpha-nexus-egolite-skin-rev';
const SKIN_REV = '6';

const LEGACY_MAP: Record<string, SkinId> = {
  void: 'hermes',
  carbon: 'hermes',
  midnight: 'alpha',
  obsidian: 'hermes',
  neon: 'pixel',
  paper: 'claude',
  apple: 'alpha',
  linear: 'alpha',
  stripe: 'hermes',
  vercel: 'hermes',
  pixelgeek: 'pixel',
  vibecafe: 'pixel',
  mario: 'mario',
  claudepaper: 'claude',
  styleseed: 'claude',
  // 旧「爱马仕奶油橙」误用 id=hermes 的用户：升级到真正 Hermes Agent 黑金
};

export function getStoredSkin(): SkinId {
  try {
    let v = localStorage.getItem(STORAGE_KEY) as string | null;
    if (v && LEGACY_MAP[v]) v = LEGACY_MAP[v];
    const valid = v && SKINS.some((s) => s.id === v) ? (v as SkinId) : null;
    // v6: 不再强制任何颜色。用户显式选择的风格被尊重，
    // 没有记录时才回落到默认（浅色）。
    if (valid) return valid;
    localStorage.setItem(SKIN_REV_KEY, SKIN_REV);
    localStorage.setItem(STORAGE_KEY, DEFAULT_SKIN);
  } catch {
    /* ignore */
  }
  return DEFAULT_SKIN;
}

/** 应用风格：颜色 + 结构 + 字体一次绑死 */
export function applySkin(id: SkinId) {
  const meta = SKINS.find((s) => s.id === id) || SKINS.find((s) => s.id === DEFAULT_SKIN)!;
  // Freeze transitions for the two frames the variables swap in. Without this,
  // dark→light briefly paints near-white text on the new white background.
  const root = document.documentElement;
  root.setAttribute('data-skin-switching', '');
  window.requestAnimationFrame(() => {
    window.requestAnimationFrame(() => root.removeAttribute('data-skin-switching'));
  });
  document.documentElement.setAttribute('data-skin', meta.id);
  document.body.setAttribute('data-skin', meta.id);
  if (meta.mode === 'dark') {
    document.body.classList.add('dark');
    document.documentElement.classList.add('dark');
  } else {
    document.body.classList.remove('dark');
    document.documentElement.classList.remove('dark');
  }
  document.documentElement.style.colorScheme = meta.mode;
  applyFont(meta.font);
  try {
    localStorage.setItem(STORAGE_KEY, meta.id);
  } catch {
    /* ignore */
  }
}

export function skinMeta(id: SkinId): SkinMeta {
  return SKINS.find((s) => s.id === id) || SKINS.find((s) => s.id === DEFAULT_SKIN)!;
}
