import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const settings = await readFile(
  new URL('../src/components/DataAPISettingsSection.tsx', import.meta.url),
  'utf8',
).catch(() => '');
const settingsView = await readFile(
  new URL('../src/components/SettingsView.tsx', import.meta.url),
  'utf8',
);
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

test('settings provides a Chinese TikHub data API configuration surface', () => {
  assert.match(settingsView, /DataAPISettingsSection/);
  assert.match(settings, /TikHub 数据 API/);
  assert.match(settings, /测试并激活/);
  assert.match(settings, /中国大陆加速/);
  assert.match(settings, /关键词搜索/);
  assert.match(settings, /内容详情/);
  assert.match(settings, /博主资料/);
  assert.match(settings, /博主作品/);
  assert.match(settings, /选题/);
  assert.match(settings, /对标/);
  assert.match(settings, /圈层/);
  assert.match(settings, /5 个 Nexus 核心平台/);
  assert.doesNotMatch(settings, /日榜/);
  assert.doesNotMatch(settings, /评论/);
});

test('credential is sent separately and never rendered from the server response', () => {
  assert.match(api, /\/api\/data-api\/credential/);
  assert.match(api, /\/api\/data-api\/test/);
  assert.match(settings, /type=\{showKey \? 'text' : 'password'\}/);
  assert.doesNotMatch(api, /export interface DataAPIConfig \{[^}]*api_key/s);
  assert.match(settings, /不会回显/);
});

test('provider endpoints are fixed catalog entries rather than arbitrary URL inputs', () => {
  assert.match(settings, /endpoint_catalog/);
  assert.doesNotMatch(settings, /name=["']base_url["']/);
  assert.match(settings, /接口目录由应用固定维护/);
});

test('settings keeps one key and moves calls and durable history to Trend Radar', () => {
  assert.match(api, /\/api\/data-api\/calls/);
  assert.equal((settings.match(/type=\{showKey \? 'text' : 'password'\}/g) || []).length, 1);
  assert.doesNotMatch(settings, /接口调用工作台/);
  assert.doesNotMatch(settings, /调用并保存/);
  assert.doesNotMatch(settings, /长期记录/);
  assert.doesNotMatch(settings, /api\.callDataAPI|api\.getDataAPICalls|api\.getDataAPICallResponse/);
  assert.match(settings, /趋势雷达/);
  assert.match(settings, /抓取 \/ 日志/);
});
