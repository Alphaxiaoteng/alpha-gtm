import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';

const settings = await readFile(new URL('../src/components/SettingsView.tsx', import.meta.url), 'utf8');
const drawer = await readFile(new URL('../src/components/AgentDrawer.tsx', import.meta.url), 'utf8');

test('custom LLM is a selectable OpenCode-backed Agent platform', () => {
  assert.match(settings, /custom: \{ label: '自定义 LLM'/);
  assert.match(settings, /已验证并保存.*OpenCode/);
  assert.match(settings, /alpha-nexus:llm-models-synced/);
  assert.match(drawer, /Alpha Agent/);
  assert.doesNotMatch(drawer, /custom: \{ alt: '自定义 LLM'/);
});
