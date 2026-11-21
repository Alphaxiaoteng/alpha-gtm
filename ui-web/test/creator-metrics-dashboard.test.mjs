import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const dashboard = await readFile(new URL('../src/components/XHSDataDashboardView.tsx', import.meta.url), 'utf8');
const metrics = await readFile(new URL('../src/creatorMetrics.ts', import.meta.url), 'utf8');
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');
const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');

test('creator metrics dashboard keeps registry collection explicit and scoped to one browser', () => {
  assert.match(api, /social\.metrics\.collect-batch/);
  assert.match(api, /social\.metrics\.overview/);
  assert.match(dashboard, /collectCreatorMetrics/);
  assert.match(dashboard, /selectedIdentity\?\.name/);
  assert.match(dashboard, /account\.identityId === selectedIdentity\.id/);
	assert.match(dashboard, /data-testid="collect-current-browser-metrics"/);
	// 更新节奏来自调度设置：自动采集开启显示间隔，关闭显示手动。
	assert.match(dashboard, /schedule\?\.enabled \? `每 \$\{schedule\.interval_hours\} 小时自动更新`/);
	assert.match(dashboard, /掉线平台自动跳过/);
  assert.doesNotMatch(dashboard, /setInterval|setTimeout|refreshInterval:\s*[1-9]/);
});

test('dashboard shows latest verified data in the window and moves failures to management', () => {
  assert.match(metrics, /latestCreatorMetricSnapshots/);
  assert.match(dashboard, /latestCreatorMetricSnapshots/);
  assert.match(dashboard, /data-testid="manage-creator-data"/);
  assert.match(dashboard, /data-testid="creator-metrics-status-summary"/);
  assert.match(dashboard, /只显示已有数据/);
  assert.match(dashboard, /数据截至/);
  assert.match(dashboard, /trendPoints\.length >= 2/);
  assert.match(dashboard, /METRICS_DOM_UNRECOGNIZED/);
  assert.match(dashboard, /EGO_METRICS_MANUAL_HANDOFF/);
  assert.match(dashboard, /未登录，已跳过/);
  assert.match(dashboard, /<PlatformIcon/);
  assert.doesNotMatch(dashboard, /今日近 7 天已验证/);
});

test('trend uses an accessible native SVG line chart for real collection dates only', () => {
  assert.match(dashboard, /data-testid="creator-metrics-line-chart"/);
  assert.match(dashboard, /<line/);
  assert.match(dashboard, /<svg[\s\S]*<polyline/);
  assert.match(dashboard, /<circle/);
  assert.match(dashboard, /filter\(\(item\) => item\.accounts > 0\)/);
  assert.match(dashboard, /trendPoints\.length >= 2/);
  assert.match(dashboard, /<title>/);
  assert.match(dashboard, /item\.date/);
  assert.match(dashboard, /item\.views/);
  assert.match(dashboard, /只显示实际有数据的日期/);
  assert.doesNotMatch(dashboard, /每个日期显示当次更新到的近 7 天浏览量/);
  assert.doesNotMatch(dashboard, /grid h-40 items-end/);
});

test('account data UI uses browser identity marks and hides implementation language', () => {
  assert.match(dashboard, /data-testid=\{`creator-browser-switch-\$\{identity\.id\}`\}/);
  assert.match(dashboard, /<BrowserIdentityMark identity=\{identity\} index=\{index\}/);
  assert.doesNotMatch(dashboard, /\/browsers\/chromium\.png/);
  for (const copy of [
    '只读页面 HTML/DOM',
    '不读取 Cookie',
    'SQLite 会复用空间',
    '近 {historyDays} 天快照趋势',
    '使用配置模型复盘',
    '自动快照',
    '冻结后人工接管',
  ]) {
    assert.doesNotMatch(dashboard, new RegExp(copy.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
  }
  assert.doesNotMatch(dashboard, /\{row\?\.error_code \? <p/);
  assert.doesNotMatch(dashboard, /aria-label="数据浏览器"/);
  assert.match(dashboard, /数据建议/);
  assert.match(dashboard, /生成建议/);
});

test('AI review receives only selected browser verified evidence after explicit click', () => {
  assert.match(api, /'metrics_review'/);
  assert.match(dashboard, /reviewMetricsWithModel/);
  assert.match(dashboard, /currentLatest\.map/);
  assert.match(dashboard, /allow_external:\s*true/);
  assert.match(dashboard, /data-testid="review-selected-browser-metrics"/);
  assert.match(dashboard, /不会自动生成/);
  assert.match(dashboard, /followers_verified\s*\?\s*item\.followers\s*:\s*null/);
});

test('account data has a dedicated workspace', () => {
  assert.match(layout, /id: 'analytics'/);
  assert.match(layout, /label: '账号数据'/);
  assert.match(layout, /defaultTab: 'xhs-data'/);
});
