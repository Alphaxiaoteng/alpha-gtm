import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('../src/lib/routes.ts', import.meta.url), 'utf8');

async function routes() {
  const { outputText } = ts.transpileModule(
    `${source}\nexport { routeForTab, parseRoute };\n`,
    { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } },
  );
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}

test('自媒体统一 /SM，店铺统一 /shop，Bot 独立', async () => {
  const { routeForTab } = await routes();
  assert.equal(routeForTab('social-media', 'media'), '/SM');
  assert.equal(routeForTab('contents', 'media'), '/SM/content');
  assert.equal(routeForTab('templates', 'media'), '/SM/templates');
  assert.equal(routeForTab('accounts', 'media'), '/SM/accounts');
  assert.equal(routeForTab('fleet', 'media'), '/SM/accounts/spaces');
  assert.equal(routeForTab('xhs-data', 'media'), '/SM/analytics');
  assert.equal(routeForTab('dashboard', 'media'), '/SM/trend');
  assert.equal(routeForTab('batch', 'media'), '/SM/publish/batch');

  assert.equal(routeForTab('accounts', 'shop'), '/shop/accounts');
  assert.equal(routeForTab('contents', 'shop'), '/shop/content');
  assert.equal(routeForTab('knowledge', 'shop'), '/shop/knowledge');
  assert.equal(routeForTab('shop', 'shop'), '/shop');

  // 不再单独开 /agent：两个入口都落在 /bot。
  assert.equal(routeForTab('bot', 'media'), '/bot');
  assert.equal(routeForTab('agent', 'media'), '/bot');
});

test('地址能反向解析出板块和模式', async () => {
  const { parseRoute } = await routes();
  assert.deepEqual(parseRoute('/SM'), { tab: 'social-media', mode: 'media' });
  assert.deepEqual(parseRoute('/SM/accounts'), { tab: 'accounts', mode: 'media' });
  assert.deepEqual(parseRoute('/SM/content'), { tab: 'contents', mode: 'media' });
  assert.deepEqual(parseRoute('/shop'), { tab: 'shop', mode: 'shop' });
  assert.deepEqual(parseRoute('/shop/accounts'), { tab: 'accounts', mode: 'shop' });
  assert.deepEqual(parseRoute('/bot'), { tab: 'bot' });
  // 旧 /agent 与旧 /media 前缀仍然可打开。
  assert.deepEqual(parseRoute('/agent'), { tab: 'bot' });
  assert.deepEqual(parseRoute('/media/accounts'), { tab: 'accounts', mode: 'media' });
});

test('每个板块在两种模式下都能往返', async () => {
  const { routeForTab, parseRoute } = await routes();
  const tabs = ['accounts', 'fleet', 'social-media', 'contents', 'templates', 'studio', 'knowledge', 'memory', 'dashboard', 'crawler', 'xhs-data', 'publish', 'records', 'metrics', 'batch', 'notifications', 'calendar', 'logs', 'monitor', 'settings', 'proxies', 'skills'];
  for (const tab of tabs) {
    for (const mode of ['media', 'shop']) {
      const path = routeForTab(tab, mode);
      const parsed = parseRoute(path);
      assert.ok(parsed, `${mode}/${tab} → ${path} 无法解析`);
      // proxies / skills 已经是设置页里的子页，解析到 settings 是预期行为。
      const expected = (tab === 'proxies' || tab === 'skills') ? 'settings' : tab;
      assert.equal(parsed.tab, expected, `${mode}/${tab} → ${path} 解析成了 ${parsed.tab}`);
    }
  }
});

// 设置分节必须能被直接访问：裸 /settings/<section> 之前不在路由表里，
// 打开后会被当成未知路径落到首页。
for (const section of ['storage', 'memory', 'logs', 'about', 'runtime', 'feishu', 'agent', 'appearance']) {
  test(`设置分节 /settings/${section} 解析到 settings`, async () => {
    const { parseRoute } = await routes();
    const parsed = parseRoute(`/settings/${section}`);
    assert.ok(parsed, `/${section} should resolve`);
    assert.equal(parsed.tab, 'settings');
  });
}
