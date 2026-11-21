import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
const layout = readFileSync(join(here, '..', 'src', 'components', 'Layout.tsx'), 'utf8');
const workspaceMode = readFileSync(join(here, '..', 'src', 'workspaceMode.ts'), 'utf8');

test('the Bot page is reachable from both product modes', () => {
  // setActiveTab downgrades to 'agent' when the tab is not allowed for the
  // current mode; Shop must therefore list 'bot'.
  assert.match(workspaceMode, /const SHOP_TABS = new Set\(\[[^\]]*'bot'/);
});

test('clicking Media or Shop leaves the Bot page', () => {
  const store = readFileSync(join(here, '..', 'src', 'store.ts'), 'utf8');
  assert.match(store, /const leavingBot = state\.activeTab === 'bot'/);
  // Leaving Bot returns to the mode's own landing surface: Media -> 趋势雷达,
  // Shop -> 经营总览. It must not drop back onto the Alpha Agent chat page.
  assert.match(store, /: leavingBot \? 'dashboard'/);
  assert.match(store, /mode === 'shop' \? 'shop'/);
});

test('Bot is the third segment and opens a real Bot page', () => {
  const matches = layout.match(/data-testid="workspace-mode-bot"/g) || [];
  assert.equal(matches.length, 1, 'workspace-mode-bot should be unique');
  // The segment switches the workspace tab; it must not just reopen a drawer.
  assert.match(layout, /data-testid="workspace-mode-bot"[\s\S]{0,400}onClick=\{\(\) => setActiveTab\('bot'\)\}/);
  assert.match(layout, /\{\(\['media', 'shop'\] as ProductMode\[\]\)\.filter\(\(mode\) => mode !== 'shop' \|\| SHOP_MODE_ENABLED\)\.map/);
  // Label stays short so three segments fit; the full name lives in the tooltip.
  assert.match(layout, /tracking-tight">Bot</);
  assert.doesNotMatch(layout, /tracking-tight">PersonAgent</);
});

test('the three segments highlight exclusively', () => {
  // On the Bot page neither Media nor Shop may keep the active thumb, or the
  // user sees "still Media" after clicking Bot.
  assert.match(layout, /const isCurrent = activeTab !== 'bot' && productMode === mode/);
  assert.match(layout, /activeTab === 'bot'\s*\n?\s*\?\s*'alpha-glass-thumb text-brand-fg'/);
  assert.match(layout, /aria-pressed=\{activeTab === 'bot'\}/);
});

test('the Bot page replaces the sidebar navigation with the bot list', () => {
  assert.match(layout, /import \{ BotRail \} from '\.\/BotRail'/);
  assert.match(layout, /\{activeTab === 'bot' \? \(\s*<BotRail \/>/);
  // The stage is shared by chat and bot so a running conversation never remounts.
  assert.match(layout, /const agentSurfaceActive = activeTab === 'agent' \|\| activeTab === 'bot'/);
});

test('the sidebar navigation is not height-capped', () => {
  // max-h-[50vh] used to clip the workspace list once the conversation section
  // was removed; the nav must fill the remaining column height instead.
  assert.doesNotMatch(layout, /max-h-\[50vh\]/);
  assert.match(layout, /min-h-0 flex-1[^"]*overflow-y-auto ui-thin-scrollbar/);
});

test('the old bottom-left PersonAgent card is gone', () => {
  assert.doesNotMatch(layout, /data-testid="personagent-entry"/);
  assert.doesNotMatch(layout, /你的私人助手/);
});

test('settings sits in the sidebar footer as its own full-width row', () => {
  // Settings renders before the status/skin row, i.e. as a standalone row in
  // the footer rather than squeezed into the same flex line.
  const settingsIdx = layout.indexOf('renderWorkspaceButton(SETTINGS_WORKSPACE)');
  const statusIdx = layout.indexOf('data-testid="backend-status"');
  assert.ok(settingsIdx > 0, 'settings entry missing');
  assert.ok(statusIdx > settingsIdx, 'settings must render before the status row');
});

test('the sidebar keeps service status, skin switch and sign-out', () => {
  assert.match(layout, /data-testid="backend-status-text"/);
  assert.match(layout, /<SkinSwitch/);
  assert.match(layout, /退出/);
});

test('the sidebar no longer lists conversations', () => {
  assert.doesNotMatch(layout, /data-testid="agent-conversation-sidebar"/);
});
