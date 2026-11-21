import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');

test('对话区右上角不再出现「电脑」按钮，电脑页改由右侧详情面板切换', async () => {
  const drawer = await read('../src/components/AgentDrawer.tsx');
  const store = await read('../src/store.ts');
  const detail = await read('../src/components/BotDetailPanel.tsx');

  // 这个按钮曾被用来打开右侧电脑面板，但它和「后台」按钮重叠、也挤占输入区，
  // 产品上判定为噪音，已移除。
  assert.doesNotMatch(drawer, /data-testid="agent-computer-button"/);
  assert.doesNotMatch(drawer, /aria-label="打开电脑面板"/);

  // 电脑页本身仍然存在，由 store 托管、在详情面板里切换，不影响已有能力。
  assert.match(store, /botDetailTab: 'computer' \| 'advanced';/);
  assert.match(store, /setBotDetailTab: \(tab: 'computer' \| 'advanced'\) => void;/);
  assert.match(store, /botDetailTab: 'computer',/);
  assert.match(detail, /useUIStore\(\(state\) => state\.botDetailTab\)/);
  assert.match(detail, /useUIStore\(\(state\) => state\.setBotDetailTab\)/);
});
