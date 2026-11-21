import type { CreatorMetricAccountDay, CreatorMetricSnapshot } from './api';

export interface CreatorMetricTrendPoint {
  date: string;
  accounts: number;
  views: number;
  interactions: number;
  posts: number;
}

export function creatorMetricCollectionState(
  row: { state?: 'observed' | 'skipped' | 'unknown' | 'manual_takeover' | 'not_collected'; error_code?: string } | undefined,
  loggedIn: boolean,
  hasCurrentSnapshot: boolean,
) {
	if (row?.state === 'skipped' && row.error_code === 'EGO_METRICS_TIMEOUT') return { code: 'timeout', label: '稍后重试', tone: 'bg-slate-100 text-slate-700' };
	if (row?.state === 'skipped' && row.error_code === 'EGO_SPACE_USER_CONTROLLED') return { code: 'user_controlled', label: '使用中', tone: 'bg-violet-50 text-violet-700' };
	if (row?.error_code === 'EGO_SPACE_NOT_FOUND' || row?.error_code === 'EGO_SPACE_BINDING_MISSING' || row?.error_code === 'EGO_SPACE_REBIND_FAILED') return { code: 'space_missing', label: '需重新绑定', tone: 'bg-rose-50 text-rose-700' };
	if (row?.state === 'skipped' && row.error_code === 'DUPLICATE_ACCOUNT_BINDING') return { code: 'duplicate', label: '重复账号', tone: 'bg-amber-50 text-amber-700' };
	if (row?.state === 'skipped' || !loggedIn) return { code: 'logged_out', label: '未登录', tone: 'bg-amber-50 text-amber-700' };
  if (row?.state === 'observed' && hasCurrentSnapshot) return { code: 'observed', label: '已更新', tone: 'bg-emerald-50 text-emerald-700' };
  if (row?.state === 'observed') return { code: 'stale', label: '待更新', tone: 'bg-orange-50 text-orange-700' };
  if (row?.state === 'manual_takeover') return { code: 'manual_takeover', label: '需手动', tone: 'bg-violet-50 text-violet-700' };
  if (row?.error_code === 'EGO_METRICS_PLATFORM_UNSUPPORTED') return { code: 'unsupported', label: '暂不可用', tone: 'bg-gray-100 text-gray-600' };
  if (row?.state === 'not_collected') return { code: 'not_collected', label: '未更新', tone: 'bg-sky-50 text-sky-700' };
  if (!row) return { code: 'not_collected', label: '未更新', tone: 'bg-sky-50 text-sky-700' };
  return { code: 'unknown', label: '未知', tone: 'bg-gray-100 text-gray-600' };
}

function parseISODate(value: string): number | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return null;
  const timestamp = Date.parse(`${value}T00:00:00Z`);
  return Number.isFinite(timestamp) && new Date(timestamp).toISOString().slice(0, 10) === value ? timestamp : null;
}

export function currentCreatorMetricSnapshots(
  rows: CreatorMetricSnapshot[],
  accountIDs: ReadonlySet<number>,
  currentDate: string,
): CreatorMetricSnapshot[] {
  if (parseISODate(currentDate) === null) return [];
  return rows.filter((row) => accountIDs.has(row.account_id) && row.range_verified && row.snapshot_date === currentDate);
}

export function latestCreatorMetricSnapshots(
  rows: CreatorMetricSnapshot[],
  accountIDs: ReadonlySet<number>,
  fromDate: string,
  toDate: string,
): CreatorMetricSnapshot[] {
  const from = parseISODate(fromDate);
  const to = parseISODate(toDate);
  if (from === null || to === null || to < from || to - from > 89 * 86_400_000) return [];

  const latestByAccount = new Map<number, CreatorMetricSnapshot>();
  for (const row of rows) {
    if (!accountIDs.has(row.account_id) || !row.range_verified) continue;
    const snapshotDate = parseISODate(row.snapshot_date);
    if (snapshotDate === null || snapshotDate < from || snapshotDate > to) continue;
    const current = latestByAccount.get(row.account_id);
    if (!current || row.snapshot_date > current.snapshot_date || (row.snapshot_date === current.snapshot_date && row.id > current.id)) {
      latestByAccount.set(row.account_id, row);
    }
  }
  return [...latestByAccount.values()].sort((left, right) => left.account_id - right.account_id);
}

export function buildCreatorMetricTrend(
  rows: CreatorMetricAccountDay[],
  accountIDs: ReadonlySet<number>,
  fromDate: string,
  toDate: string,
): CreatorMetricTrendPoint[] {
  const from = parseISODate(fromDate);
  const to = parseISODate(toDate);
  if (from === null || to === null || to < from || to - from > 89 * 86_400_000) return [];

  const byDate = new Map<string, { accounts: Set<number>; views: number; interactions: number; posts: number }>();
  for (const row of rows) {
    if (!accountIDs.has(row.account_id) || parseISODate(row.date) === null || row.date < fromDate || row.date > toDate) continue;
    const point = byDate.get(row.date) ?? { accounts: new Set<number>(), views: 0, interactions: 0, posts: 0 };
    point.accounts.add(row.account_id);
    point.views += row.views;
    point.interactions += row.likes + row.comments + row.shares + row.favorites;
    point.posts += row.posts;
    byDate.set(row.date, point);
  }

  const result: CreatorMetricTrendPoint[] = [];
  for (let timestamp = from; timestamp <= to; timestamp += 86_400_000) {
    const date = new Date(timestamp).toISOString().slice(0, 10);
    const point = byDate.get(date);
    result.push({
      date,
      accounts: point?.accounts.size ?? 0,
      views: point?.views ?? 0,
      interactions: point?.interactions ?? 0,
      posts: point?.posts ?? 0,
    });
  }
  return result;
}
