import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';

test('runtime monitor is a first-class workspace backed by real local status APIs', async () => {
  const app = await readFile(new URL('../src/App.tsx', import.meta.url), 'utf8');
  const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
  const store = await readFile(new URL('../src/store.ts', import.meta.url), 'utf8');
  const view = await readFile(new URL('../src/components/RuntimeMonitorView.tsx', import.meta.url), 'utf8');
  const viteConfig = await readFile(new URL('../vite.config.ts', import.meta.url), 'utf8');

  assert.doesNotMatch(layout, /label: '运行监控'/);
  assert.match(app, /case 'monitor'/);
  assert.match(app, /<RuntimeMonitorView/);
  assert.match(app, /const view = new URLSearchParams\(window\.location\.search\)\.get\('view'\);/);
  assert.match(app, /if \(view === 'foundation'\) setActiveTab\('monitor'\);/);
  assert.match(store, /'monitor'/);

  for (const apiCall of [
    'getBackendHealth',
    'getEgoDoctor',
    'getEgoSpaces',
    'getAccounts',
    'getEgoSessionMonitor',
    // /api/account-observer/status 在当前构建里未注册（legacy_chromium 专属），
    // 监控页改用已在服务的 /api/ego/session-monitor，不再调用它。
    'agentHubProviders',
  ]) {
    assert.match(view, new RegExp(`api\\.${apiCall}`));
  }

  assert.match(view, /STATUS_REFRESH_INTERVAL_MS = 5_000/);
  assert.match(view, /refreshInterval: STATUS_REFRESH_INTERVAL_MS/);
  assert.match(view, /PROVIDER_PROBE_INTERVAL_MS = 30_000/);
  assert.match(view, /unknown.*待确认/s);
  assert.match(view, /不发起远程模型调用/);
  assert.match(view, /data-testid="runtime-monitor"/);
  assert.doesNotMatch(view, /data-testid="runtime-event-log"/);
  const settings = await readFile(new URL('../src/components/EgoSettingsView.tsx', import.meta.url), 'utf8');
  assert.match(settings, /api\.getOperationEvents/);
  assert.match(settings, /data-testid="settings-operation-log"/);
  assert.match(view, /01 \/ Foundation/);
  assert.match(view, /内容与电商底座/);
  assert.match(view, /data-testid="anc-foundation-architecture"/);
  assert.match(view, /data-testid="anc-architecture-view"/);
  assert.match(view, /get\('section'\) === 'anc'/);
  assert.match(view, /从业务数据到可执行能力/);
  assert.match(view, /tools\.Registry/);
  assert.match(view, /SQLite 资产真源/);
  assert.match(view, /受控执行/);
  assert.match(view, /证据与复盘/);
  assert.match(view, /查看运行监控/);
  assert.match(view, /ANC 底座架构/);
  assert.match(view, /数据接入/);
  assert.match(view, /全流程复盘/);
  assert.match(view, /知识主干/);
  assert.match(view, /渐进加载/);
  assert.match(view, /跨会话状态/);
  assert.match(view, /github\.com\/HA7CH\/ha7ch-school/);
  assert.doesNotMatch(view, /Cookie|credential|api_key|password/i);
  assert.match(viteConfig, /modulePreload:\s*\{\s*polyfill:\s*false\s*\}/);
});
