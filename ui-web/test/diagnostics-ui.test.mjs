import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';

const settings = fs.readFileSync(new URL('../src/components/SettingsView.tsx', import.meta.url), 'utf8');
const api = fs.readFileSync(new URL('../src/api.ts', import.meta.url), 'utf8');
const clientDiagnostics = fs.readFileSync(new URL('../src/clientDiagnostics.ts', import.meta.url), 'utf8');
const main = fs.readFileSync(new URL('../src/main.tsx', import.meta.url), 'utf8');

test('settings can create a local redacted diagnostic snapshot', () => {
  assert.match(settings, /settings-create-diagnostics/);
  assert.match(settings, /createDiagnosticSnapshot/);
  assert.match(settings, /生成脱敏诊断快照/);
  assert.ok(api.includes("'/api/diagnostics/snapshot'"));
});

test('settings makes the local log directory available without creating a snapshot first', () => {
  assert.match(settings, /data-testid="settings-audit-logs"/);
  assert.match(settings, /日志与诊断/);
  assert.match(settings, /data-testid="settings-open-log-folder"/);
  assert.match(settings, /打开日志目录/);
  assert.match(settings, /getDiagnosticLogDirectory/);
  assert.ok(api.includes("'/api/diagnostics/log-directory'"));
});

test('renderer failures report only a local fingerprint', () => {
  assert.match(main, /installClientDiagnostics/);
  assert.match(main, /ClientErrorBoundary/);
  assert.ok(clientDiagnostics.includes("'/api/diagnostics/client-event'"));
  assert.match(clientDiagnostics, /fingerprint/);
  assert.ok(clientDiagnostics.includes('body: JSON.stringify({ kind, fingerprint })'));
  assert.doesNotMatch(clientDiagnostics, /body:\s*JSON\.stringify\(\{[^)]*(message|stack|filename)/s);
});
