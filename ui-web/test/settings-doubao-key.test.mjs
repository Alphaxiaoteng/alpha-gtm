import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';

test('settings does not expose a model API key to the Agent-facing connection surface', async () => {
  const settings = await readFile(new URL('../src/components/SettingsView.tsx', import.meta.url), 'utf8');
  const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

  assert.doesNotMatch(settings, /data-testid="settings-doubao-key"/);
  assert.doesNotMatch(settings, /api\.saveDoubaoAPIKey/);
  assert.match(settings, /data-testid="settings-agent-providers"/);
  assert.match(settings, /Codex/);
  assert.match(settings, /OpenCode/);
  assert.match(settings, /Antigravity CLI/);
  assert.match(api, /saveDoubaoAPIKey/);
  assert.match(api, /\/api\/agent\/models/);
  assert.doesNotMatch(settings, /local\/llama|127\.0\.0\.1:11434/);
});
