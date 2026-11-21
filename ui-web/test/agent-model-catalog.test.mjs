import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const settings = await readFile(new URL('../src/components/SettingsView.tsx', import.meta.url), 'utf8');
const drawer = await readFile(new URL('../src/components/AgentDrawer.tsx', import.meta.url), 'utf8');
const agentSection = await readFile(new URL('../src/components/AlphaAgentSettingsSection.tsx', import.meta.url), 'utf8');

test('every connected Agent platform can validate its complete discovered model catalog', () => {
  assert.match(settings, /const validateCandidateModels = async/);
  assert.match(settings, /settings-agent-provider-\$\{provider\.id\}-validate-candidates/);
  assert.match(settings, /验证全部候选/);
  assert.match(settings, /可能产生模型费用/);
  assert.match(settings, /window\.confirm/);
  assert.doesNotMatch(settings, /开始验证 \$\{candidates\.length\} 个候选模型/);
  assert.doesNotMatch(settings, /officialOpenCodeCandidates/);
  assert.doesNotMatch(settings, /validateOfficialOpenCodeModels/);
});

test('verified models are all visible and switchable in Settings and the Agent drawer', () => {
  assert.match(settings, /settings-agent-provider-\$\{provider\.id\}-model-select/);
  assert.match(settings, /verifiedModels\.map\(\(model\) => <option/);
  assert.match(drawer, /modelGroups\.map\(\(group\)/);
  assert.match(drawer, /role="option"/);
  assert.match(drawer, /probe\?\.models/);
});

test('Claude can use CC Switch while OpenCode models remain validation-only candidates', () => {
  assert.match(settings, /CC Switch 当前配置/);
  assert.match(settings, /launch_mode:/);
  assert.match(settings, /OpenCode 已验证候选/);
  assert.match(settings, /候选仍需由 Claude Code 实测/);
  // Candidate model IDs stay Settings-only. The drawer's validation hint may
  // reference the candidate count, but must never render candidate IDs as
  // selectable options.
  assert.doesNotMatch(drawer, /model_candidates\??\s*\.\s*map/);
  assert.doesNotMatch(drawer, /value=\{[^}]*model_candidates/);
});

test('the reachable settings section can validate one CLI candidate model', () => {
  assert.match(agentSection, /agentHubProviderModelValidate\(/);
  assert.match(agentSection, /data-testid=\{`alpha-agent-validate-\$\{provider\.id\}-\$\{model\}`\}/);
  assert.match(agentSection, /validating_model/);
  assert.match(agentSection, /agent-model-verified/);
});

test('Settings exposes one Agent platform list without a separate external Agent concept', () => {
  assert.doesNotMatch(settings, /settings-agent-external-clients/);
  assert.doesNotMatch(settings, /agentHubExternalClients/);
  assert.doesNotMatch(settings, /WorkBuddy|ZCode|外部 Agent/);
  assert.doesNotMatch(drawer, /workbuddy|zcode/i);
});
