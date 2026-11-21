import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');
const drawerPath = '../src/components/BackgroundDrawer.tsx';
const layoutPath = '../src/components/Layout.tsx';

test('后台抽屉只在打开时才轮询任务与事件', async () => {
  const source = await read(drawerPath);

  // 关闭时 key 为 null，SWR 不发请求，避免抽屉收起后还在后台空转。
  assert.match(source, /open \? 'api\/jobs\/list' : null/);
  assert.match(source, /open && tab === 'history' \? 'api\/jobs\/events\/all' : null/);
});

test('抽屉角标区分进行中、失败与空闲三种状态', async () => {
  const source = await read(drawerPath);

  assert.match(source, /data-testid="background-drawer-trigger"/);
  assert.match(source, /进行中/);
  assert.match(source, /失败/);
  assert.match(source, /animate-spin/);
  assert.match(source, /AlertTriangle/);
});

test('抽屉展开状态持久化，并可用 Esc 收起', async () => {
  const source = await read(drawerPath);

  assert.match(source, /const OPEN_KEY = 'alpha\.backgroundDrawer\.open'/);
  assert.match(source, /localStorage\.setItem\(OPEN_KEY/);
  assert.match(source, /event\.key === 'Escape'/);
  assert.match(source, /aria-expanded=\{open\}/);
});

test('事件列表用真实字段名渲染，不猜 camelCase', async () => {
  const source = await read(drawerPath);

  assert.match(source, /event\.event_type/);
  assert.match(source, /event\.error_code/);
  assert.match(source, /event\.created_at/);
  assert.doesNotMatch(source, /event\.eventType/);
  assert.doesNotMatch(source, /event\.errorCode/);
  assert.doesNotMatch(source, /event\.createdAt/);
});

test('任务行展示当前步骤而不是空状态', async () => {
  const source = await read(drawerPath);

  assert.match(source, /job\.platform_code/);
  assert.match(source, /job\.current_step/);
  assert.doesNotMatch(source, /job\.platformCode/);
});

test('Layout 挂载抽屉，且不再把日志放进一级导航', async () => {
  const layout = await read(layoutPath);

  assert.match(layout, /import \{ BackgroundDrawer \} from '\.\/BackgroundDrawer'/);
  assert.match(layout, /<BackgroundDrawer \/>/);
  // 日志入口只保留在设置分组里。
  assert.doesNotMatch(layout, /label: '系统日志'/);
  assert.match(layout, /matches: \['settings', 'proxies', 'skills', 'logs'\]/);
});
