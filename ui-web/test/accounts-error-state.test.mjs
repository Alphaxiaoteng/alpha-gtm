import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

test('account loading failure has an explicit retry state', async () => {
  const source = await readFile(new URL('../src/components/AccountsView.tsx', import.meta.url), 'utf8');

  assert.match(source, /error: accountsError/);
  assert.match(source, /data-testid="accounts-error"/);
  assert.match(source, /onClick=\{\(\) => void mutateAccounts\(\)\}/);
});

test('fingerprint preset says it still needs saving', async () => {
  const source = await readFile(new URL('../src/components/SettingsView.tsx', import.meta.url), 'utf8');

  assert.match(source, /点击“保存基础设置”后生效/);
});
