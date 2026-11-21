import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const drawer = await readFile(new URL('../src/components/AgentDrawer.tsx', import.meta.url), 'utf8');

test('model switcher names the real source (next) instead of the DeepSeek whale', () => {
  // `custom` 是 openainext14（next）供应商的 alpha-nexus-llm/* 模型，运行时由
  // OpenCode 适配器执行；下拉里写 next + 工具名，不再借用 DeepSeek 品牌。
  assert.match(drawer, /custom: \{ name: 'next'/);
  assert.doesNotMatch(drawer, /deepseek-logo\.svg/);
  assert.doesNotMatch(drawer, /alt: 'DeepSeek'/);
  assert.match(drawer, /custom: \{ name: 'next', badge: 'N'/);
  assert.match(drawer, /dsh: \{ name: 'DSH', badge: 'D' \}/);
  assert.match(drawer, /alpha: \{ name: 'Alpha Agent', badge: 'A' \}/);
});

test('every provider group says which coding tool runs it', () => {
  assert.match(drawer, /codex: \{ name: 'Codex'/);
  assert.match(drawer, /antigravity: \{ name: 'Antigravity · agy'/);
  assert.match(drawer, /claude: \{ name: 'Claude Code'/);
  assert.match(drawer, /kimi: \{ name: 'Kimi Code'/);
  assert.match(drawer, /codebuddy: \{ name: 'CodeBuddy Code'/);
  assert.match(drawer, /opencode: \{ name: 'OpenCode'/);
});

test('each coding agent keeps its own group even before a model is verified', () => {
  // 不再过滤空组：Codex / Antigravity 没验证模型时也要可见，只是标注未验证。
  assert.doesNotMatch(drawer, /filter\(\(group\) => group\.models\.length > 0\)/);
  assert.match(drawer, /agent-model-group-empty-\$\{group\.id\}/);
});

test('group header and closed trigger both render the tool identity', () => {
  assert.match(drawer, /aria-label=\{providerDisplay\(group\.id\)\.name\}/);
  assert.match(drawer, /<ProviderLogo providerID=\{group\.id\}/);
  assert.match(drawer, /<ProviderLogo providerID=\{providerID\}/);
  assert.match(drawer, /providerDisplay\(providerID\)\.name/);
  // 既有契约不回退：关闭态仍显示模型 ID，菜单仍是 listbox + option。
  assert.match(drawer, /data-testid="agent-model-select"/);
  assert.match(drawer, /role="listbox"/);
  assert.match(drawer, /role="option"/);
  assert.match(drawer, /const pickModel = async/);
  assert.match(drawer, /conversationSelectionRevisionRef/);
});
