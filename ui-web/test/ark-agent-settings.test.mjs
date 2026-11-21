import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const settings = await readFile(
  new URL('../src/components/SettingsView.tsx', import.meta.url),
  'utf8',
);
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

test('LLM settings configure only verified Volcano Ark Agent models', () => {
  assert.match(settings, /火山方舟 API（用于对话与工具规划）/);
  assert.match(settings, /useSWR\('api\/agent\/models', api\.getAgentModels\)/);
  assert.match(settings, /api\.verifyAndSaveAgentModels/);
  assert.match(settings, /验证并保存/);
  assert.match(settings, /type="password"/);
  assert.match(settings, /autoComplete="new-password"/);
  assert.match(settings, /未加入/);
  assert.match(settings, /disabled=\{saving/);
  assert.doesNotMatch(settings, /FALLBACK_LLM_PRESETS/);
  assert.doesNotMatch(settings, /显示 LLM API Key|隐藏 LLM API Key/);
});

test('Agent model API never expects an echoed credential', () => {
  assert.match(api, /export interface AgentModelConfig/);
  assert.match(api, /getAgentModels:/);
  assert.match(api, /verifyAndSaveAgentModels:/);
  assert.match(api, /\/api\/agent\/models/);
  assert.match(api, /api_key\?: string/);
  const configType = api.slice(
    api.indexOf('export interface AgentModelConfig'),
    api.indexOf('export interface AgentModelUpdateInput'),
  );
  assert.doesNotMatch(configType, /api_key/);
});
