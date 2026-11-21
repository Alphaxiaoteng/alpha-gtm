import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');

test('对话区右上角有电脑按钮（OMB 同款 Monitor 圆形），点击打开右侧电脑面板', async () => {
  const drawer = await read('../src/components/AgentDrawer.tsx');
  const store = await read('../src/store.ts');
  const detail = await read('../src/components/BotDetailPanel.tsx');

  // 按钮存在、可访问、带运行脉冲点。
  assert.match(drawer, /data-testid="agent-computer-button"/);
  assert.match(drawer, /aria-label="打开电脑面板"/);
  assert.match(drawer, /<Monitor className="h-4 w-4" \/>/);
  assert.match(drawer, /data-testid="agent-computer-live"/);
  // 点击切到电脑页并收起收件箱，而不是跳转。
  assert.match(drawer, /const openComputerPanel = \(\) => \{/);
  assert.match(drawer, /setBotDetailTab\('computer'\)/);
  assert.match(drawer, /setBotInboxOpen\(false\)/);
  assert.match(drawer, /onClick=\{openComputerPanel\}/);

  // tab 由 store 托管，对话区按钮才能真正切换详情面板的页。
  assert.match(store, /botDetailTab: 'computer' \| 'advanced';/);
  assert.match(store, /setBotDetailTab: \(tab: 'computer' \| 'advanced'\) => void;/);
  assert.match(store, /botDetailTab: 'computer',/);
  assert.match(detail, /useUIStore\(\(state\) => state\.botDetailTab\)/);
  assert.match(detail, /useUIStore\(\(state\) => state\.setBotDetailTab\)/);
});
