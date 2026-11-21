import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const settings = await readFile(
  new URL('../src/components/SettingsView.tsx', import.meta.url),
  'utf8',
);
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

test('Ark Agent models live with image and video models and use the same accordion style', () => {
  assert.match(settings, /data-testid="settings-media-api"/);
  assert.match(settings, /<LLMConfigPanel \/>/);
  assert.match(settings, /data-testid="settings-llm-api"/);
  assert.match(settings, /LLM 模型配置/);
  assert.match(settings, /火山方舟 API（用于对话与工具规划）/);
  assert.match(settings, /bg-brand-surface border border-brand-border rounded-xl overflow-hidden transition shadow-xs/);
  assert.match(settings, /AI 图片生成配置/);
  assert.match(settings, /AI 视频生成配置/);
  assert.match(settings, /type="password"/);
  assert.match(settings, /界面不会读取或显示已存密钥/);
  assert.match(settings, /api\.verifyAndSaveAgentModels/);
  assert.match(api, /\/api\/agent\/models/);
});

test('only verified models are presented and failed candidates stay out', () => {
  assert.match(settings, /const verifiedModels = data\?\.models \|\| \[\]/);
  assert.match(settings, /验证失败 · 未加入/);
  assert.match(settings, /尚无通过真实调用验证的模型/);
  assert.match(settings, /验证并保存/);
  assert.doesNotMatch(settings, /FALLBACK_LLM_PRESETS/);
});

test('media cards use secure backend status and successful checks persist configuration', () => {
  assert.match(settings, /aiConfig\?\.image_api_configured/);
  assert.match(settings, /aiConfig\?\.video_api_configured/);
  assert.match(settings, /api\.saveAgnesAPIKey/);
  assert.match(settings, /await mutateAIConfig\(\)/);
  assert.doesNotMatch(settings, /imageAPIKey \? 'API Key 已配置'/);
  assert.doesNotMatch(settings, /videoAPIKey \? 'API Key 已配置'/);
});
