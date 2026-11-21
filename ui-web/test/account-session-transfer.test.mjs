import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const accountsSource = await readFile(new URL('../src/components/AccountsView.tsx', import.meta.url), 'utf8');
const apiSource = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

test('account session transfer keeps local backup and device migration explicit', () => {
  for (const apiName of [
    'getAccountSessionBackups',
    'createAccountSessionBackup',
		'restoreAccountSessionBackup',
    'createAllAccountSessionBackups',
    'exportAccountSession',
    'importAccountSession',
  ]) {
    assert.match(apiSource, new RegExp(apiName));
  }
  assert.match(accountsSource, /账号状态备份与迁移/);
  assert.match(accountsSource, /备份全部已登录账号/);
  assert.match(accountsSource, /导出加密迁移包/);
	assert.match(accountsSource, /恢复最近备份/);
  assert.match(accountsSource, /\.anxsession/);
  assert.match(accountsSource, /不导出密码、Profile 路径或代理/);
  assert.match(accountsSource, /不会伪造“已登录”/);
});
