import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

test('侧栏顶部不再放品牌标：打包成应用后由窗口/应用图标承载', async () => {
  const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
  // 顶部栏已删除，不放 wordmark、不放像素标、不放返回首页
  assert.doesNotMatch(layout, /alpha-intelligence-logo\.png/);
  assert.doesNotMatch(layout, /<AlphaPixelMark/);
  assert.doesNotMatch(layout, /data-testid="app-brand-mark"/);
  assert.doesNotMatch(layout, /<Bot className=/);
  assert.doesNotMatch(layout, /alpha-nexus-app-icon-focused\.png/);
  assert.doesNotMatch(layout, /currentSkin\.blurb/);
  assert.doesNotMatch(layout, /global-content-search/);
  assert.doesNotMatch(layout, /搜索内容 ⌘K/);
});

test('the left sidebar can be hidden and restored', async () => {
  const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
  const store = await readFile(new URL('../src/store.ts', import.meta.url), 'utf8');
  // 收起/展开是同一个按钮，靠 sidebarState 切换 testid 与图标
  assert.match(layout, /sidebarHidden/);
  assert.match(store, /toggleSidebar/);
});

test('the single Alpha Agent uses a black-and-white pixel alpha mark', async () => {
  const drawer = await readFile(new URL('../src/components/AgentDrawer.tsx', import.meta.url), 'utf8');
  const mark = await readFile(new URL('../src/components/AlphaPixelMark.tsx', import.meta.url), 'utf8');
  assert.match(drawer, /AlphaPixelMark/);
  assert.doesNotMatch(drawer, /alpha-intelligence-logo\.png/);
  assert.match(mark, /shapeRendering="crispEdges"/);
  assert.match(mark, /PIXEL_ALPHA/);
});
