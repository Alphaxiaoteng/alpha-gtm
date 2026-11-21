/**
 * Pixel-art Bot avatars, embedded as SVG elements (no external files, no
 * network request, no asset loader). One avatar per responsibility with four
 * female and four male variants.
 *
 * The legacy `/bot-avatars/<role>.svg` paths stay valid as the stored value so
 * existing bots keep rendering; `pixelAvatarRole()` maps them back to a role.
 */

import type { ReactNode } from 'react';

const FEMALE = [
  '................',
  '......HHHH......',
  '....HHHHHHHH....',
  '...HHHHHHHHHH...',
  '...HHHHHHHHHH...',
  '..HHHHHHHHHHHH..',
  '..HHSSSSSSSSHH..',
  '..HSSSSSSSSSSH..',
  '..HSWESSSSEWSH..',
  '..HSSSSSSSSSSH..',
  '..HCSSSMMSSSCH..',
  '..HHSSSSSSSSHH..',
  '...HHBBBBBBHH...',
  '..BBBBBBBBBBBB..',
  '.BBBBBBBBBBBBBB.',
  '.BBBBBBBBBBBBBB.',
];

const MALE = [
  '................',
  '................',
  '.....HHHHHH.....',
  '...HHHHHHHHHH...',
  '..HHHHHHHHHHHH..',
  '..HHHHHHHHHHHH..',
  '..HHSSSSSSSSHH..',
  '..HSSSSSSSSSSH..',
  '..HSWESSSSEWSH..',
  '..HSSSSSSSSSSH..',
  '..HCSSSMMSSSCH..',
  '..HHSSSSSSSSHH..',
  '...HSSBBBBSSH...',
  '..BBBBBBBBBBBB..',
  '.BBBBBBBBBBBBBB.',
  '.BBBBBBBBBBBBBB.',
];

const SKIN = '#FFD9B8';
const EYE = '#2B2B3A';
const MOUTH = '#C96B6B';
const CHEEK = '#FFB3C6';
const HIGHLIGHT = '#FFFFFF';

export type PixelAvatarRole =
  | 'planner'
  | 'writer'
  | 'designer'
  | 'support'
  | 'reviewer'
  | 'analyst'
  | 'engineer'
  | 'manager';

type PixelArt = { label: string; gender: 'female' | 'male'; bg: string; hair: string; shirt: string };

export const PIXEL_AVATARS: Record<PixelAvatarRole, PixelArt> = {
  planner: { label: '选题策划', gender: 'female', bg: '#FFF0F5', hair: '#F06C9B', shirt: '#8E6FD8' },
  writer: { label: '文案写作', gender: 'female', bg: '#FFF6E5', hair: '#E8894A', shirt: '#4E8FD8' },
  designer: { label: '视觉设计', gender: 'female', bg: '#F3ECFF', hair: '#9B6BE8', shirt: '#E86A9B' },
  support: { label: '客服运营', gender: 'female', bg: '#FFF9DB', hair: '#E0B040', shirt: '#4FB88A' },
  reviewer: { label: '脚本审核', gender: 'male', bg: '#EAF2FF', hair: '#3A5FA8', shirt: '#5E7BB8' },
  analyst: { label: '数据分析', gender: 'male', bg: '#E6F7F9', hair: '#2F6B7A', shirt: '#4A9BAA' },
  engineer: { label: '技术开发', gender: 'male', bg: '#EAF7EE', hair: '#3F6B4A', shirt: '#5AA878' },
  manager: { label: '项目管理', gender: 'male', bg: '#F0F1F4', hair: '#4A4F5A', shirt: '#6B7280' },
};

export const PIXEL_AVATAR_ROLES = Object.keys(PIXEL_AVATARS) as PixelAvatarRole[];

/** `/bot-avatars/planner.svg` -> `planner`; anything else -> ''. */
export function pixelAvatarRole(avatar?: string): PixelAvatarRole | '' {
  if (!avatar) return '';
  const match = /^\/bot-avatars\/([a-z0-9_-]+)\.svg$/.exec(avatar);
  if (!match) return '';
  return (match[1] in PIXEL_AVATARS ? (match[1] as PixelAvatarRole) : '');
}

export function pixelAvatarSrc(role: PixelAvatarRole): string {
  return `/bot-avatars/${role}.svg`;
}

export function PixelAvatar({
  role,
  size = 32,
  className = '',
}: {
  role: PixelAvatarRole;
  size?: number;
  className?: string;
}) {
  const art = PIXEL_AVATARS[role];
  if (!art) return null;
  const grid = art.gender === 'female' ? FEMALE : MALE;
  const colors: Record<string, string> = {
    H: art.hair,
    S: SKIN,
    E: EYE,
    W: HIGHLIGHT,
    M: MOUTH,
    C: CHEEK,
    B: art.shirt,
  };

  const rects: ReactNode[] = [];
  for (let y = 0; y < grid.length; y += 1) {
    const row = grid[y];
    let x = 0;
    while (x < row.length) {
      const ch = row[x];
      let run = 1;
      while (x + run < row.length && row[x + run] === ch) run += 1;
      const fill = colors[ch];
      if (fill) {
        rects.push(<rect key={`${x}-${y}`} x={x} y={y} width={run} height={1} fill={fill} />);
      }
      x += run;
    }
  }

  return (
    <svg
      viewBox="0 0 16 16"
      width={size}
      height={size}
      shapeRendering="crispEdges"
      className={className}
      aria-hidden="true"
      focusable="false"
    >
      <rect width="16" height="16" fill={art.bg} />
      {rects}
    </svg>
  );
}

/**
 * 单色线条头像：保留职责语义，去掉颜色和像素块，和极简白界面一致。
 * 存储值仍是 `/bot-avatars/<role>.svg`，所以老数据不用迁移。
 */
const ROLE_ICONS: Record<PixelAvatarRole, ReactNode> = {
  planner: <><path d="M9 18h6" /><path d="M10 22h4" /><path d="M12 2a7 7 0 0 0-4 12.7V17h8v-2.3A7 7 0 0 0 12 2Z" /></>,
  writer: <><path d="M12 20h9" /><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z" /></>,
  designer: <><circle cx="13.5" cy="6.5" r="2.5" /><circle cx="17.5" cy="10.5" r="2.5" /><circle cx="8.5" cy="7.5" r="2.5" /><circle cx="6.5" cy="12.5" r="2.5" /><path d="M12 22a10 10 0 1 1 10-10c0 2-1.5 3-3 3h-2a2 2 0 0 0-1 3.7A2 2 0 0 1 14 22Z" /></>,
  support: <><path d="M3 14v-2a9 9 0 0 1 18 0v2" /><path d="M21 16a2 2 0 0 1-2 2h-1v-6h1a2 2 0 0 1 2 2Z" /><path d="M3 16a2 2 0 0 0 2 2h1v-6H5a2 2 0 0 0-2 2Z" /><path d="M18 18v1a3 3 0 0 1-3 3h-3" /></>,
  reviewer: <><path d="M9 11l2 2 4-4" /><path d="M20 12v6a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h9" /><path d="M16 2h6v6" /></>,
  analyst: <><path d="M3 21h18" /><rect x="4" y="12" width="4" height="7" rx="1" /><rect x="10" y="7" width="4" height="12" rx="1" /><rect x="16" y="3" width="4" height="16" rx="1" /></>,
  engineer: <><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M8 10l2.5 2.5L8 15" /><path d="M13 15h4" /></>,
  manager: <><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" /></>,
};

export function LineAvatar({
  role,
  size = 32,
  className = '',
}: {
  role: PixelAvatarRole;
  size?: number;
  className?: string;
}) {
  const glyph = ROLE_ICONS[role];
  if (!glyph) return null;
  return (
    <span
      className={`flex shrink-0 items-center justify-center bg-[#f4f4f4] text-[#4a4a4a] ${className}`}
      style={{ height: size, width: size, borderRadius: Math.max(4, Math.round(size * 0.22)) }}
      aria-hidden="true"
    >
      <svg
        viewBox="0 0 24 24"
        width={Math.round(size * 0.54)}
        height={Math.round(size * 0.54)}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.6}
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        {glyph}
      </svg>
    </span>
  );
}

/**
 * OpenMausBot 风格吉祥物头像：blob 身形 + 每个职责一个品牌色 + 简单表情。
 *
 * 身形路径与配色取自 OpenMausBot (Apache License 2.0, © 2026 Milind Soni and
 * OpenMausBot contributors) 的 `packages/phone-protocol/src/mascot-bodies.ts`
 * 与 `src/lib/mascot.ts`。这里只取 blob 一种身形、静态表情，不引入其动画引擎。
 * 存储值仍然是 `/bot-avatars/<role>.svg`，所以既有数据无需迁移。
 */
const MASCOT_BODY =
  'M198.91 113.13C198.3 121.88 196.38 130.38 193.14 138.61C189.89 146.85 185.49 154.4 179.93 161.28C174.37 168.15 168 174.11 160.82 179.13C153.63 184.16 145.99 188.22 137.88 191.31C129.77 194.41 121.43 196.55 112.86 197.75C104.29 198.94 95.66 199.13 86.99 198.31C78.32 197.49 69.87 195.54 61.63 192.48C53.38 189.41 45.74 185.21 38.71 179.87C31.67 174.52 25.61 168.26 20.53 161.06C15.46 153.87 11.54 146.16 8.79 137.91C6.04 129.67 4.37 121.29 3.8 112.77C3.23 104.26 3.56 95.83 4.8 87.49C6.04 79.16 8.08 71 10.94 63.02C13.8 55.05 17.53 47.39 22.14 40.05C26.74 32.72 32.3 26.03 38.81 19.99C45.32 13.95 52.65 9.01 60.81 5.16C68.96 1.32 77.54 -1.1 86.52 -2.09C95.51 -3.09 104.37 -2.64 113.12 -0.77C121.87 1.11 130.1 4.13 137.8 8.28C145.5 12.44 152.54 17.36 158.9 23.03C165.26 28.71 170.97 34.92 176.01 41.65C181.06 48.38 185.4 55.57 189.02 63.22C192.65 70.86 195.36 78.91 197.15 87.34C198.93 95.78 199.52 104.38 198.91 113.13Z';

/** 职责 → 吉祥物颜色。取自 OpenMausBot 的 MAUS_COLORS 调色板。 */
export const MASCOT_COLORS: Record<PixelAvatarRole, string> = {
  planner: '#D84F8B',
  writer: '#E78531',
  designer: '#8057C8',
  support: '#D8A729',
  reviewer: '#377FE6',
  analyst: '#0EA5C6',
  engineer: '#009957',
  manager: '#01A492',
};

export function MascotAvatar({
  role,
  size = 32,
  className = '',
}: {
  role: PixelAvatarRole;
  size?: number;
  className?: string;
}) {
  const color = MASCOT_COLORS[role];
  if (!color) return null;
  const gradientID = `maus-${role}-${Math.round(size)}`;
  return (
    <svg
      viewBox="-10 -12 220 224"
      width={size}
      height={size}
      className={`shrink-0 ${className}`}
      aria-hidden="true"
      focusable="false"
    >
      <defs>
        <linearGradient id={gradientID} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0%" stopColor={color} />
          <stop offset="100%" stopColor={color} stopOpacity="0.72" />
        </linearGradient>
      </defs>
      <path d={MASCOT_BODY} fill={`url(#${gradientID})`} />
      {/* 左上高光，让团块看起来是立体的 */}
      <ellipse cx="66" cy="44" rx="30" ry="21" fill="#FFFFFF" opacity="0.22" transform="rotate(-24 66 44)" />
      {/* 眼睛：大黑瞳 + 白高光，小尺寸下也能读出表情 */}
      <ellipse cx="84" cy="102" rx="14" ry="19" fill="#111820" />
      <ellipse cx="128" cy="102" rx="14" ry="19" fill="#111820" />
      <circle cx="79" cy="95" r="5" fill="#FFFFFF" opacity="0.92" />
      <circle cx="123" cy="95" r="5" fill="#FFFFFF" opacity="0.92" />
      {/* 嘴：一道浅弧 */}
      <path
        d="M96 133 Q106 144 116 133"
        fill="none"
        stroke="#111820"
        strokeOpacity="0.9"
        strokeWidth="6"
        strokeLinecap="round"
      />
    </svg>
  );
}

/**
 * Platform logos for channel-facing personas (小红书博主/抖音编导/...).
 * These are the same brand assets the publishing surface already uses.
 */
export const PLATFORM_AVATARS = [
  { id: 'xhs', src: '/platforms/xhs.png', label: '小红书' },
  { id: 'douyin', src: '/platforms/douyin.png', label: '抖音' },
  { id: 'bilibili', src: '/platforms/bilibili.png', label: 'B站' },
  { id: 'zhihu', src: '/platforms/zhihu.png', label: '知乎' },
  { id: 'wechat_mp', src: '/platforms/wechat_mp.png', label: '公众号' },
  { id: 'wechat_video', src: '/platforms/wechat_video.png', label: '视频号' },
  { id: 'x', src: '/platforms/x.png', label: 'X / 推特' },
  { id: 'toutiao', src: '/platforms/toutiao.png', label: '头条' },
  { id: 'kuaishou', src: '/platforms/kuaishou.png', label: '快手' },
  { id: 'youtube', src: '/platforms/youtube.png', label: 'YouTube' },
  { id: 'tiktok', src: '/platforms/tiktok.png', label: 'TikTok' },
  { id: 'sina', src: '/platforms/sina.png', label: '微博' },
] as const;

/** `/platforms/xhs.png` -> `xhs`; anything else -> ''. */
export function platformAvatarID(avatar?: string): string {
  if (!avatar) return '';
  const match = /^\/platforms\/([a-z0-9_-]{1,32})\.png$/.exec(avatar);
  if (!match) return '';
  return PLATFORM_AVATARS.some((item) => item.id === match[1]) ? match[1] : '';
}
