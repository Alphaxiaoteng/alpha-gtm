import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

test('live browser settings expose only the EgoLite runtime', async () => {
  const app = await readFile(new URL('../src/App.tsx', import.meta.url), 'utf8');
  const settings = await readFile(new URL('../src/components/EgoSettingsView.tsx', import.meta.url), 'utf8');

  // 页面改为路由级懒加载，仍必须从 App 挂载 EgoSettingsView。
  assert.match(app, /const EgoSettingsView = lazy\(\(\) => import\('\.\/components\/EgoSettingsView'\)/);
  assert.match(app, /<EgoSettingsView/);
  assert.match(settings, /data-testid="ego-settings"/);
  assert.match(settings, /EgoLite 浏览器运行时/);
  assert.match(settings, /ego-browser CLI/);
  assert.match(settings, /Space 并发/);
  assert.doesNotMatch(settings, /from '\.\/SettingsView'/);
  assert.doesNotMatch(settings, /settings-browser-install-help/);
  assert.doesNotMatch(settings, /getBrowserCandidates|scanBrowsers|switchBrowserEngine/);
});
