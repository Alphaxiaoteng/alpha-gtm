import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('../src/lib/routePrefetch.ts', import.meta.url), 'utf8');
const appSource = await readFile(new URL('../src/App.tsx', import.meta.url), 'utf8');
const layoutSource = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');

async function prefetchModule() {
  const start = source.indexOf('// #region route-prefetch-logic');
  const end = source.indexOf('// #endregion route-prefetch-logic');
  assert.ok(start > 0 && end > start, 'routePrefetch.ts 必须保留可执行区域');
  const { outputText } = ts.transpileModule(
    `${source.slice(start, end)}\nexport { prefetchView, prefetchableTabs };\n`,
    { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } },
  );
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}

// 从 renderView 里取出所有真正渲染页面的 tab；agent/bot 复用常驻舞台，没有 chunk。
function renderedTabs() {
  const body = appSource.slice(appSource.indexOf('const renderView = () => {'));
  const tabs = [];
  const pattern = /case '([a-z0-9-]+)':\s*\n\s*return (?:<(\w+)|null)/g;
  for (const match of body.matchAll(pattern)) {
    if (match[2]) tabs.push(match[1]);
  }
  return tabs;
}

test('每个懒加载页面都有预热入口', async () => {
  const { prefetchableTabs } = await prefetchModule();
  const prefetchable = new Set(prefetchableTabs());
  const tabs = renderedTabs();
  assert.ok(tabs.length >= 20, `renderView 解析到的 tab 太少：${tabs.length}`);
  const missing = tabs.filter((tab) => !prefetchable.has(tab));
  assert.deepEqual(missing, [], `缺少预热映射的页面：${missing.join(', ')}`);
});

test('预热列表覆盖除常驻舞台外的全部 tab', async () => {
  const { prefetchableTabs } = await prefetchModule();
  const prefetchable = prefetchableTabs();
  // 常驻舞台不需要 chunk；其余页面必须能预热。
  assert.ok(!prefetchable.includes('bot'));
  assert.ok(!prefetchable.includes('agent'));
  for (const tab of ['dashboard', 'publish', 'accounts', 'settings', 'knowledge', 'monitor', 'xhs-data']) {
    assert.ok(prefetchable.includes(tab), `${tab} 应可预热`);
  }
});

test('导航按钮在悬停和聚焦时预热，点击行为不变', () => {
  assert.match(layoutSource, /import \{ prefetchView \} from '\.\.\/lib\/routePrefetch';/);
  assert.match(layoutSource, /onPointerEnter=\{\(\) => prefetchView\(workspace\.defaultTab\)\}/);
  assert.match(layoutSource, /onFocus=\{\(\) => prefetchView\(workspace\.defaultTab\)\}/);
  // 点击仍然只切 tab，不等待预取。
  assert.match(layoutSource, /onClick=\{\(\) => \{\s*setActiveTab\(workspace\.defaultTab\);/);
});
