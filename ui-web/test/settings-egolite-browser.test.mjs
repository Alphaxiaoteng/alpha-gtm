import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

test('settings provides the official EgoLite browser download and setup path', async () => {
  const source = await readFile(new URL('../src/components/SettingsView.tsx', import.meta.url), 'utf8');

  assert.match(source, /EgoLite 浏览器/);
  assert.match(source, /openOfficialURL\('https:\/\/lite\.ego\.app\/'\)/);
  assert.match(source, /打开 EgoLite 下载页/);
  assert.match(source, /https:\/\/www\.egolite\.app\/document\/en\/docs\/quick-start/);
});
