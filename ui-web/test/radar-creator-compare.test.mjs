import assert from 'node:assert/strict';
import test from 'node:test';
import {
  assignColors,
  CREATOR_COMPARE_PALETTE,
  dayOffset,
  followerPoints,
  interactionPoints,
  interactionTotal,
  isSeriesInsufficient,
  mergeDays,
  seriesPoints,
  windowDays,
} from '../src/radarCreatorCompare.ts';

const day = (overrides) => ({
  date: '2026-11-04',
  followers: null,
  likes: 0,
  collects: 0,
  shares: 0,
  comments: 0,
  observations: 0,
  ...overrides,
});

const creator = (overrides) => ({
  key: 'douyin:甲',
  platform: 'douyin',
  author: '甲',
  works: 1,
  observations: 2,
  follower_points: 0,
  insufficient_followers: true,
  insufficient_series: false,
  days: [],
  ...overrides,
});

test('多博主曲线聚合：mergeDays 合并两天真实日期并升序去重', () => {
  const creators = [
    creator({
      key: 'douyin:甲',
      days: [day({ date: '2026-11-05' }), day({ date: '2026-11-03' })],
    }),
    creator({
      key: 'douyin:乙',
      author: '乙',
      days: [day({ date: '2026-11-05' }), day({ date: '2026-11-04' })],
    }),
  ];
  assert.deepEqual(mergeDays(creators), ['2026-11-03', '2026-11-04', '2026-11-05']);
});

test('窗口内缺口不被补零：中间没有观测的日期不出现在序列里', () => {
  const withGap = creator({
    days: [day({ date: '2026-11-04', observations: 1, likes: 5 }), day({ date: '2026-11-07', observations: 2, likes: 9 })],
  });
  assert.deepEqual(interactionPoints(withGap), [
    { date: '2026-11-04', value: 5 },
    { date: '2026-11-07', value: 9 },
  ]);
  // 11-05、11-06 不出现，也绝不出现 0 值的补点。
  assert.equal(interactionPoints(withGap).some((point) => point.value === 0), false);
  assert.deepEqual(mergeDays([withGap]), ['2026-11-04', '2026-11-07']);
});

test('isSeriesInsufficient 在真实点少于 2 个时为 true，且空输入不崩', () => {
  const onePoint = creator({ days: [day({ date: '2026-11-04', observations: 1, likes: 3, followers: 4241 })] });
  assert.equal(isSeriesInsufficient(onePoint, 'followers'), true);
  assert.equal(isSeriesInsufficient(onePoint, 'interactions'), true);
  const twoPoints = creator({
    days: [day({ date: '2026-11-04', observations: 1, followers: 4241 }), day({ date: '2026-11-05', observations: 1, followers: 4300 })],
  });
  assert.equal(isSeriesInsufficient(twoPoints, 'followers'), false);
  assert.equal(isSeriesInsufficient(twoPoints, 'interactions'), false);
  assert.equal(isSeriesInsufficient(undefined, 'followers'), true);
  assert.equal(isSeriesInsufficient({}, 'interactions'), true);
});

test('followers 为 null 的天不进粉丝曲线（读不到不画成 0）', () => {
  const mixed = creator({
    days: [
      day({ date: '2026-11-04', followers: 4241, observations: 1 }),
      day({ date: '2026-11-05', followers: null, observations: 1 }),
      day({ date: '2026-11-06', followers: 0, observations: 1 }),
    ],
  });
  assert.deepEqual(followerPoints(mixed), [
    { date: '2026-11-04', value: 4241 },
    { date: '2026-11-06', value: 0 },
  ]);
});

test('只有粉丝快照、没有观测的天不进互动曲线', () => {
  const followerOnly = creator({
    days: [day({ date: '2026-11-12', followers: 17895, observations: 0 })],
  });
  assert.deepEqual(interactionPoints(followerOnly), []);
  assert.deepEqual(followerPoints(followerOnly), [{ date: '2026-11-12', value: 17895 }]);
  assert.deepEqual(seriesPoints(followerOnly, 'followers'), followerPoints(followerOnly));
});

test('interactionTotal 把四项互动相加', () => {
  assert.equal(interactionTotal(day({ likes: 589, collects: 100, shares: 2, comments: 16 })), 707);
  assert.equal(interactionTotal(day({})), 0);
});

test('空 creators 时空合并结果为空数组', () => {
  assert.deepEqual(mergeDays([]), []);
  assert.deepEqual(mergeDays(undefined), []);
  assert.deepEqual(assignColors([]), {});
  assert.deepEqual(assignColors(undefined), {});
  assert.deepEqual(windowDays('', ''), []);
  assert.deepEqual(windowDays('2026-11-05', '2026-11-04'), []);
});

test('配色确定性：同一 key 在同一顺序里颜色稳定，列表变短也不串色', () => {
  const a = creator({ key: 'douyin:甲' });
  const b = creator({ key: 'douyin:乙', author: '乙' });
  const c = creator({ key: 'douyin:丙', author: '丙' });
  const colors = assignColors([a, b, c]);
  assert.equal(colors['douyin:甲'], CREATOR_COMPARE_PALETTE[0]);
  assert.equal(colors['douyin:乙'], CREATOR_COMPARE_PALETTE[1]);
  assert.equal(colors['douyin:丙'], CREATOR_COMPARE_PALETTE[2]);
  assert.deepEqual(assignColors([a, b, c]), assignColors([a, b, c]));
});

test('时间窗对齐：dayOffset 与 windowDays 按日界计算，非法日期给 NaN/空', () => {
  assert.equal(dayOffset('2026-11-01', '2026-11-01'), 0);
  assert.equal(dayOffset('2026-11-01', '2026-11-08'), 7);
  assert.equal(Number.isNaN(dayOffset('2026-11-01', 'bad')), true);
  assert.deepEqual(windowDays('2026-11-01', '2026-11-03'), ['2026-11-01', '2026-11-02', '2026-11-03']);
});
