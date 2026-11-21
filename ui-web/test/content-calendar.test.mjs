import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';

const view = await readFile(new URL('../src/components/ContentCalendarView.tsx', import.meta.url), 'utf8');
const app = await readFile(new URL('../src/App.tsx', import.meta.url), 'utf8');
const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const store = await readFile(new URL('../src/store.ts', import.meta.url), 'utf8');

test('content calendar is a read-only month grid over existing publish APIs', async () => {
  assert.match(view, /data-testid="content-calendar"/);
  assert.match(view, /data-testid="calendar-month"/);
  // 只读：数据来自既有发布 API，日历自身不触发任何写操作。
  assert.match(view, /api\.getPublishPlans/);
  assert.match(view, /api\.getPublishPlan\(id\)/);
  assert.match(view, /api\.getPublishContentRecords/);
  assert.doesNotMatch(view, /api\.publishPlan|api\.freezePublish|method: 'POST'|method: 'PUT'|method: 'DELETE'/);
  // 排期来自 plan targets 的 publish_at；平台侧记录区分已发/待发。
  assert.match(view, /publish_at/);
  assert.match(view, /remote_state === 'published'/);
  // 月份切换与今日锚点。
  assert.match(view, /shiftMonth\(-1\)/);
  assert.match(view, /shiftMonth\(1\)/);
  // 每月条目上限防止单格溢出。
  assert.match(view, /dayEntries\.slice\(0, 4\)/);
});

test('content calendar registers as its own workspace without breaking removal gates', () => {
  assert.match(layout, /id: 'calendar'/);
  assert.match(layout, /label: '内容日历'/);
  assert.match(app, /case 'calendar':\s*return <ContentCalendarView \/>;/);
  assert.match(store, /'xhs-data', 'calendar', 'agent',/);
  // 历史移除决策保持有效：不以 header toggle 形态回归，也不复活已移除产品。
  assert.doesNotMatch(layout, /content-calendar-toggle/);
  for (const removed of ['creation', 'browser', 'ai-image']) {
    assert.doesNotMatch(layout, new RegExp(`id: '${removed}'`));
  }
});
