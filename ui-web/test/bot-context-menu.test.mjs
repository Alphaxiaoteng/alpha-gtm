import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');
const railPath = '../src/components/BotRail.tsx';

/**
 * 右键菜单的纯逻辑（文案、下一个置顶状态、视口收边、真实置顶调用）都在
 * `#region bot-rail-menu` 里：转译后在 Node 里真跑，用假依赖断言真实调用参数，
 * 而不是只比对源码字面。
 */
async function menuModule() {
  const source = await read(railPath);
  const start = source.indexOf('// #region bot-rail-menu');
  const end = source.indexOf('// #endregion bot-rail-menu');
  assert.ok(start > 0 && end > start, 'BotRail 必须保留可执行的右键菜单区域');
  const { outputText } = ts.transpileModule(
    `${source.slice(start, end)}\nexport {
      BOT_MENU_SIZE,
      botPinMenuTitle,
      botRowPointerIntent,
      clampMenuPosition,
      nextPinnedState,
      toggleBotPinned,
    };\n`,
    { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } },
  );
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}

test('左侧 Bot 行可以右键显示菜单：右键只弹菜单，不打开会话', async () => {
  const source = await read(railPath);

  assert.match(source, /onContextMenu=\{\(event\) => \{\s*\n\s*event\.preventDefault\(\);\s*\n\s*event\.stopPropagation\(\);\s*\n\s*setBotMenu\(\{ bot, x: event\.clientX, y: event\.clientY \}\);/);
  // 右键事件本身不进入打开流程：只有 click 才算「打开」。
  assert.match(source, /if \(botRowPointerIntent\(event\.type\) === 'open'\) void openBot\(bot\);/);
  assert.match(source, /aria-haspopup="menu"/);
  // 键盘路径：ContextMenu / Shift+F10 用行中心当锚点。
  assert.match(source, /event\.key === 'ContextMenu' \|\| \(event\.shiftKey && event\.key === 'F10'\)/);
});

test('右键菜单不合法嵌套修复：行是 role=button 的 div，里面仍可放免打扰按钮', async () => {
  const source = await read(railPath);
  const rowStart = source.indexOf('data-testid={`bot-rail-item-${bot.id}`}');
  assert.ok(rowStart > 0, 'Bot 行必须有 testid');
  const before = source.slice(Math.max(0, rowStart - 400), rowStart);
  // 行根节点不再是 <button>：嵌套 button 是非法 HTML。
  assert.match(before, /role="button"\s*\n\s*tabIndex=\{0\}/);
  assert.doesNotMatch(before, /<button\s*\n\s*key=\{bot\.id\}/);
  // 免打扰仍是真按钮，testid 不变。
  assert.match(source, /data-testid=\{bot\.muted \? `bot-rail-muted-\$\{bot\.id\}` : `bot-rail-unmuted-\$\{bot\.id\}`\}/);
  assert.match(source, /aria-disabled=\{openingBotID \? true : undefined\}/);
});

test('菜单在视口内收边，不越出窗口', async () => {
  const { clampMenuPosition, BOT_MENU_SIZE } = await menuModule();
  const viewport = { width: 1280, height: 800 };

  assert.deepEqual(clampMenuPosition({ x: 100, y: 200 }, BOT_MENU_SIZE, viewport), { left: 100, top: 200 });
  const bottomRight = clampMenuPosition({ x: 1270, y: 790 }, BOT_MENU_SIZE, viewport);
  assert.equal(bottomRight.left, 1280 - BOT_MENU_SIZE.width - 8);
  assert.equal(bottomRight.top, 800 - BOT_MENU_SIZE.height - 8);
  const offscreen = clampMenuPosition({ x: -40, y: -10 }, BOT_MENU_SIZE, viewport);
  assert.deepEqual(offscreen, { left: 8, top: 8 });
});

test('未置顶显示「置顶」、已置顶显示「取消置顶」，目标状态取反', async () => {
  const { botPinMenuTitle, nextPinnedState, botRowPointerIntent } = await menuModule();

  assert.equal(botPinMenuTitle({ pinned: false }), '置顶');
  assert.equal(botPinMenuTitle({}), '置顶');
  assert.equal(botPinMenuTitle({ pinned: true }), '取消置顶');
  assert.equal(nextPinnedState({ pinned: true }), false);
  assert.equal(nextPinnedState({ pinned: false }), true);

  // 右键（contextmenu）永远不是「打开会话」。
  assert.equal(botRowPointerIntent('click'), 'open');
  assert.equal(botRowPointerIntent('contextmenu'), 'ignore');
});

test('置顶切换调用真实 API 参数并刷新两套缓存', async () => {
  const { toggleBotPinned } = await menuModule();

  const calls = [];
  const deps = {
    setPinned: async (botID, pinned) => {
      calls.push(['setPinned', botID, pinned]);
    },
    revalidate: async () => {
      calls.push(['revalidate']);
    },
    toast: (message, type) => calls.push(['toast', message, type]),
  };

  const pinned = await toggleBotPinned({ id: 'dev-frontend', name: '前端', pinned: false }, deps);
  assert.equal(pinned, true);
  assert.deepEqual(calls, [
    ['setPinned', 'dev-frontend', true],
    ['revalidate'],
    ['toast', '已置顶', 'success'],
  ]);

  const unpinCalls = [];
  const unpinned = await toggleBotPinned(
    { id: 'dev-frontend', name: '前端', pinned: true },
    {
      setPinned: async (botID, pinned) => {
        unpinCalls.push(['setPinned', botID, pinned]);
      },
      revalidate: async () => {
        unpinCalls.push(['revalidate']);
      },
      toast: (message, type) => unpinCalls.push(['toast', message, type]),
    },
  );
  assert.equal(unpinned, false);
  assert.deepEqual(unpinCalls, [
    ['setPinned', 'dev-frontend', false],
    ['revalidate'],
    ['toast', '已取消置顶', 'success'],
  ]);
});

test('置顶失败只报错，不假装成功、不刷新缓存', async () => {
  const { toggleBotPinned } = await menuModule();
  const calls = [];

  const result = await toggleBotPinned(
    { id: 'dev-frontend', name: '前端', pinned: true },
    {
      setPinned: async () => {
        throw new Error('AGENT_BOT_NOT_FOUND');
      },
      revalidate: async () => {
        calls.push('revalidate');
      },
      toast: (message, type) => calls.push([message, type]),
    },
  );

  assert.equal(result, true, '失败时保持原来的置顶状态');
  assert.deepEqual(calls, [['AGENT_BOT_NOT_FOUND', 'error']]);
});

test('菜单接到真实依赖上，并在点外部/Escape/滚动/切换 Bot 时关闭', async () => {
  const source = await read(railPath);

  assert.match(source, /setPinned: api\.setAgentBotPinned,/);
  assert.match(source, /revalidate: revalidateBotCaches,/);
  assert.match(source, /data-testid="bot-rail-menu-pin"/);
  assert.match(source, /data-testid="bot-rail-menu-mute"/);
  assert.match(source, /data-bot-row-menu=""/);
  // 关闭路径
  assert.match(source, /event\.target\.closest\('\[data-bot-row-menu\]'\)/);
  assert.match(source, /window\.addEventListener\('mousedown', onPointerDown\)/);
  assert.match(source, /if \(event\.key === 'Escape'\) close\(\);/);
  assert.match(source, /window\.addEventListener\('scroll', close, true\)/);
  assert.match(source, /useEffect\(\(\) => \{\s*\n\s*setBotMenu\(null\);\s*\n\s*\}, \[activeBotID\]\);/);
  // 点击选项后关闭
  assert.match(source, /async function toggleBotPin\(bot: AgentBot\) \{\s*\n\s*setBotMenu\(null\);/);
  // 样式：极简白、圆角
  assert.match(source, /rounded-xl border border-brand-border bg-brand-surface p-1/);
});
