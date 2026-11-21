// 博主对比视图的纯函数层：不依赖 React，测试直接跑本文件。
// 口径与后端一致——只画真实采集到的点，缺日不补零、不插值；
// 「数据点不足」是对真实点数的如实标注，不是可以抹平的展示细节。

export interface CreatorCompareDay {
  date: string;
  /** null = 当天没有可读到的粉丝快照（读不到 ≠ 0）。 */
  followers: number | null;
  likes: number;
  collects: number;
  shares: number;
  comments: number;
  /** 当天真实观测条数；0 表示这天只有粉丝快照、没有作品观测。 */
  observations: number;
}

export interface CreatorCompareEntry {
  key: string;
  platform: string;
  author: string;
  works: number;
  observations: number;
  follower_points: number;
  insufficient_followers: boolean;
  insufficient_series: boolean;
  days: CreatorCompareDay[];
}

export interface CreatorCompareWindow {
  from: string;
  to: string;
  days: number;
}

export interface CreatorCompareResponse {
  window: CreatorCompareWindow;
  creators: CreatorCompareEntry[];
  empty: boolean;
}

export interface CreatorComparePoint {
  date: string;
  value: number;
}

export type CreatorCompareSeriesKind = 'followers' | 'interactions';

// 固定调色板：八个色相在浅色底上互相可分；顺序固定 → 同一批博主配色稳定。
export const CREATOR_COMPARE_PALETTE = [
  '#111111',
  '#2563eb',
  '#dc2626',
  '#059669',
  '#d97706',
  '#7c3aed',
  '#0891b2',
  '#db2777',
];

/** 一天里的互动合计：点赞 + 收藏 + 分享 + 评论。 */
export function interactionTotal(day: CreatorCompareDay): number {
  return (day?.likes ?? 0) + (day?.collects ?? 0) + (day?.shares ?? 0) + (day?.comments ?? 0);
}

/** 粉丝曲线：只取真实读到的粉丝点，null 的天直接跳过（不画、不补零）。 */
export function followerPoints(creator: CreatorCompareEntry | undefined): CreatorComparePoint[] {
  const points: CreatorComparePoint[] = [];
  for (const day of creator?.days ?? []) {
    if (day.followers === null || day.followers === undefined) continue;
    points.push({ date: day.date, value: day.followers });
  }
  return points;
}

/** 互动曲线：只取真有观测的天；没有观测的天不画成 0。 */
export function interactionPoints(creator: CreatorCompareEntry | undefined): CreatorComparePoint[] {
  const points: CreatorComparePoint[] = [];
  for (const day of creator?.days ?? []) {
    if (!day || day.observations <= 0) continue;
    points.push({ date: day.date, value: interactionTotal(day) });
  }
  return points;
}

/** 系列点集：粉丝曲线或互动曲线的真实点。 */
export function seriesPoints(
  creator: CreatorCompareEntry | undefined,
  kind: CreatorCompareSeriesKind,
): CreatorComparePoint[] {
  return kind === 'followers' ? followerPoints(creator) : interactionPoints(creator);
}

/**
 * 是否数据点不足：真实点少于 2 个就画不出走势，如实标注而不是连一条假线。
 * 后端已给出标记；这里再算一遍，保证前端对任何输入（含空对象）都不崩。
 */
export function isSeriesInsufficient(
  creator: CreatorCompareEntry | undefined,
  kind: CreatorCompareSeriesKind,
): boolean {
  return seriesPoints(creator, kind).length < 2;
}

/** 选中博主们出现过的所有真实日期，按升序去重（缺日保留为缺口，不补齐）。 */
export function mergeDays(creators: CreatorCompareEntry[] | undefined): string[] {
  const dates = new Set<string>();
  for (const creator of creators ?? []) {
    for (const day of creator?.days ?? []) {
      if (day?.date) dates.add(day.date);
    }
  }
  return [...dates].sort();
}

/** YYYY-MM-DD → UTC 零点毫秒数；非法输入返回 NaN。 */
export function dateToUTC(date: string): number {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date ?? '');
  if (!match) return Number.NaN;
  return Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
}

/** 相对窗口起点的天数偏移；非法输入返回 NaN，调用方负责跳过该点。 */
export function dayOffset(from: string, date: string): number {
  const start = dateToUTC(from);
  const target = dateToUTC(date);
  if (Number.isNaN(start) || Number.isNaN(target)) return Number.NaN;
  return Math.round((target - start) / 86_400_000);
}

/** 窗口内的天序（from→to，含两端）；窗口非法时返回空数组。 */
export function windowDays(from: string, to: string): string[] {
  const start = dateToUTC(from);
  const end = dateToUTC(to);
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return [];
  const days: string[] = [];
  for (let timestamp = start; timestamp <= end; timestamp += 86_400_000) {
    days.push(new Date(timestamp).toISOString().slice(0, 10));
  }
  return days;
}

/** 确定性配色：按传入顺序循环取色，同一 key 在同一份列表里颜色稳定。 */
export function assignColors(
  creators: CreatorCompareEntry[] | undefined,
  palette: string[] = CREATOR_COMPARE_PALETTE,
): Record<string, string> {
  const colors: Record<string, string> = {};
  const usable = palette.length > 0 ? palette : CREATOR_COMPARE_PALETTE;
  (creators ?? []).forEach((creator, index) => {
    if (!creator?.key) return;
    colors[creator.key] = usable[index % usable.length];
  });
  return colors;
}

/** 折线几何：把真实点投影到画布；跳过无法定位（非法日期）的点。 */
export function projectPoints(
  points: CreatorComparePoint[],
  options: {
    from: string;
    width: number;
    height: number;
    padding: { top: number; right: number; bottom: number; left: number };
    maxValue: number;
  },
): Array<{ date: string; value: number; x: number; y: number }> {
  const { from, width, height, padding, maxValue } = options;
  const span = Math.max(1, maxValue);
  const plotWidth = Math.max(1, width - padding.left - padding.right);
  const plotHeight = Math.max(1, height - padding.top - padding.bottom);
  const offsets = points
    .map((point) => ({ point, offset: dayOffset(from, point.date) }))
    .filter((entry) => !Number.isNaN(entry.offset));
  const maxOffset = Math.max(1, ...offsets.map((entry) => entry.offset));
  return offsets.map(({ point, offset }) => ({
    date: point.date,
    value: point.value,
    x: padding.left + (plotWidth * offset) / maxOffset,
    y: padding.top + plotHeight - (plotHeight * Math.max(0, point.value)) / span,
  }));
}
