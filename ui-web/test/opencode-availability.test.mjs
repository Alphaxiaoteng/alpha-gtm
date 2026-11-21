import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

const terminalView = await readFile(new URL('../src/components/TerminalView.tsx', import.meta.url), 'utf8');
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

test('OpenCode binary discovery is shown as unverified and cannot start the legacy TUI', () => {
  assert.match(api, /available_unverified/);
  assert.match(api, /verified_read_only/);
  assert.match(terminalView, /openCodeAvailability/);
  assert.match(terminalView, /未验证/);
  assert.match(terminalView, /enabled: false/);
  assert.match(terminalView, /disabled=\{starting \|\| !e\.enabled\}/);
  assert.doesNotMatch(terminalView, /ready: probe\?\.opencode\?\.available/);
});
