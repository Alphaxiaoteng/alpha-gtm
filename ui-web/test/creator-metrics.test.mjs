import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';
import { buildCreatorMetricTrend, creatorMetricCollectionState, currentCreatorMetricSnapshots, latestCreatorMetricSnapshots } from '../src/creatorMetrics.ts';

test('current creator metrics exclude yesterday and another browser', () => {
  const base = {
    id: 1, account_name: '账号', platform_code: 'douyin', window_days: 7,
    followers: 0, followers_verified: false, views: 0, likes: 0, comments: 0,
    shares: 0, favorites: 0, posts: 0, evidence_code: 'REVIEWED', collected_at: '2026-08-02T01:00:00Z',
  };
  const current = currentCreatorMetricSnapshots([
    { ...base, account_id: 11, snapshot_date: '2026-08-01', range_verified: true },
    { ...base, id: 2, account_id: 11, snapshot_date: '2026-08-02', range_verified: true },
    { ...base, id: 3, account_id: 21, snapshot_date: '2026-08-02', range_verified: true },
    { ...base, id: 4, account_id: 12, snapshot_date: '2026-08-02', range_verified: false },
  ], new Set([11, 12]), '2026-08-02');

  assert.deepEqual(current.map((item) => item.id), [2]);
  assert.deepEqual(currentCreatorMetricSnapshots([], new Set([11]), 'bad-date'), []);
});

test('latest verified creator metrics remain visible when the window end date has no snapshot', () => {
  const base = {
    account_name: '账号', platform_code: 'douyin', window_days: 7,
    followers: 0, followers_verified: false, views: 0, likes: 0, comments: 0,
    shares: 0, favorites: 0, posts: 0, evidence_code: 'REVIEWED', collected_at: '2026-08-03T01:00:00Z',
  };
  const latest = latestCreatorMetricSnapshots([
    { ...base, id: 1, account_id: 11, snapshot_date: '2026-08-03', range_verified: true },
    { ...base, id: 2, account_id: 11, snapshot_date: '2026-08-02', range_verified: true },
    { ...base, id: 3, account_id: 12, snapshot_date: '2026-08-03', range_verified: false },
    { ...base, id: 4, account_id: 21, snapshot_date: '2026-08-03', range_verified: true },
  ], new Set([11, 12]), '2026-08-01', '2026-08-07');

  assert.deepEqual(latest.map((item) => item.id), [1]);
  assert.deepEqual(latestCreatorMetricSnapshots([], new Set([11]), 'bad-date', '2026-08-07'), []);
});

test('creator metric trend keeps seven dates and excludes another browser', () => {
  const trend = buildCreatorMetricTrend([
    { account_id: 11, platform_code: 'douyin', date: '2026-07-31', views: 100, likes: 10, comments: 2, shares: 1, favorites: 3, posts: 1 },
    { account_id: 12, platform_code: 'wechat_video', date: '2026-07-31', views: 200, likes: 20, comments: 4, shares: 2, favorites: 6, posts: 2 },
    { account_id: 21, platform_code: 'douyin', date: '2026-07-31', views: 999999, likes: 999, comments: 999, shares: 999, favorites: 999, posts: 99 },
    { account_id: 11, platform_code: 'douyin', date: '2026-08-01', views: 400, likes: 40, comments: 8, shares: 4, favorites: 12, posts: 3 },
  ], new Set([11, 12]), '2026-07-26', '2026-08-01');

  assert.equal(trend.length, 7);
  assert.deepEqual(trend[5], {
    date: '2026-07-31', accounts: 2, views: 300, interactions: 48, posts: 3,
  });
  assert.deepEqual(trend[6], {
    date: '2026-08-01', accounts: 1, views: 400, interactions: 64, posts: 3,
  });
  assert.equal(trend[0].views, 0);
});

test('creator metric trend fails closed for an invalid date window', () => {
  assert.deepEqual(buildCreatorMetricTrend([], new Set([1]), 'bad', '2026-08-01'), []);
  assert.deepEqual(buildCreatorMetricTrend([], new Set([1]), '2026-08-01', '2026-07-01'), []);
});

test('creator metric trend supports a 90 day local history window', () => {
  const trend = buildCreatorMetricTrend([], new Set([1]), '2026-05-04', '2026-08-01');
  assert.equal(trend.length, 90);
});

test('creator metrics dashboard exposes compact history settings, dates, and platform marks', async () => {
  const dashboard = await readFile(new URL('../src/components/XHSDataDashboardView.tsx', import.meta.url), 'utf8');

  assert.match(dashboard, /data-testid="creator-metrics-preferences"/);
  assert.match(dashboard, /展示范围/);
  assert.match(dashboard, /记录保留/);
  assert.match(dashboard, /数据截至/);
  assert.match(dashboard, /<PlatformIcon platform=\{post\.platform_code\}/);
  assert.doesNotMatch(dashboard, /grid-cols-7/);
});

test('logged-out state overrides a current snapshot', () => {
  assert.equal(creatorMetricCollectionState({ state: 'observed' }, false, true).code, 'logged_out');
  assert.equal(creatorMetricCollectionState({ state: 'observed' }, true, true).code, 'observed');
  assert.equal(creatorMetricCollectionState({ state: 'observed' }, true, false).code, 'stale');
});

test('metric timeout is shown as a safe skip, not a login failure', () => {
  assert.deepEqual(
    creatorMetricCollectionState({ state: 'skipped', error_code: 'EGO_METRICS_TIMEOUT' }, true, false),
    { code: 'timeout', label: '稍后重试', tone: 'bg-slate-100 text-slate-700' },
  );
});

test('user-controlled browser is shown as a safe skip, not a login failure', () => {
  assert.deepEqual(
    creatorMetricCollectionState({ state: 'skipped', error_code: 'EGO_SPACE_USER_CONTROLLED' }, true, false),
    { code: 'user_controlled', label: '使用中', tone: 'bg-violet-50 text-violet-700' },
  );
});

test('duplicate browser binding is shown as an account conflict', () => {
  assert.deepEqual(
    creatorMetricCollectionState({ state: 'skipped', error_code: 'DUPLICATE_ACCOUNT_BINDING' }, true, false),
    { code: 'duplicate', label: '重复账号', tone: 'bg-amber-50 text-amber-700' },
  );
});

test('persisted not-collected state stays not collected instead of becoming evidence insufficient', () => {
  assert.deepEqual(
    creatorMetricCollectionState({ state: 'not_collected' }, true, false),
    { code: 'not_collected', label: '未更新', tone: 'bg-sky-50 text-sky-700' },
  );
});
