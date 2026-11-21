import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const accountsSource = await readFile(
  new URL('../src/components/AccountsView.tsx', import.meta.url),
  'utf8',
);

test('stopped account card uses a play button to start that account browser', () => {
  assert.match(
    accountsSource,
    /onClick=\{\(\) => handleStartBrowser\(acct\.id\)\}[\s\S]*?aria-label=\{`启动 \$\{acct\.name\} 的账号浏览器`\}[\s\S]*?<Play className="w-3\.5 h-3\.5 fill-current" \/>/,
  );
});

test('account cards do not expose the temporary XHS collection action', () => {
  assert.doesNotMatch(accountsSource, /handleCollectData/);
  assert.doesNotMatch(accountsSource, /同步小红书笔记|需启动浏览器后才能采集/);
});

test('XHS account ID display removes the duplicated platform label', () => {
  assert.match(
    accountsSource,
    /function displayAccountIdentifier\(account: Account\): string \{[\s\S]*?account\.platformCode !== 'xhs'[\s\S]*?replace\(\/\^小红书账号\\s\*\[:：\]\\s\*\/, ''\)/,
  );
  assert.match(accountsSource, /const accountIdentifier = displayAccountIdentifier\(acct\);/);
  assert.match(accountsSource, /title=\{`平台用户ID: \$\{accountIdentifier\}`\}[\s\S]*?ID: \{accountIdentifier\}/);
});

test('account selection lives on the avatar and stays hidden until needed', () => {
  assert.match(accountsSource, /data-testid=\{`account-selection-\$\{acct\.id\}`\}/);
  assert.match(accountsSource, /absolute left-0 top-0 -translate-x-1\/2 -translate-y-1\/2 z-10 w-4 h-4 rounded-full/);
  assert.match(
    accountsSource,
    /isBatchSelecting \|\| accountSelected[\s\S]*?'opacity-100'[\s\S]*?'opacity-0 group-hover:opacity-100 group-focus-within:opacity-100'/,
  );
  assert.match(accountsSource, /onClick=\{\(e\) => \{[\s\S]*?toggleCardSelection\(\);/);
  assert.doesNotMatch(accountsSource, /\/\* Actions Header \*\/\s*<div[^>]*>\s*<label/);
});

test('workspace exposes batch selection and keeps batch management actions', () => {
  assert.match(accountsSource, /data-testid="account-batch-select-entry"/);
  assert.match(accountsSource, /data-testid="account-batch-close-running"/);
  assert.match(accountsSource, /批量管理账号/);
  assert.match(accountsSource, /确认关闭 \$\{targets\.length\} 个正在运行的账号浏览器/);
  assert.match(accountsSource, /Profile 和登录数据会保留/);
  assert.match(accountsSource, /failedIDs/);
  for (const action of ['批量打开', '批量关闭', '批量检测', '批量标签']) {
    assert.match(accountsSource, new RegExp(action));
  }
});

test('toolbar counts unique running browser profiles and moves low-frequency tools under More', () => {
  assert.match(accountsSource, /const runningCount = new Set\([\s\S]*?account\.profileId\?\.trim\(\) \|\| `account:\$\{account\.id\}`/);
  assert.match(accountsSource, /data-testid="accounts-more-menu-trigger"/);
  assert.match(accountsSource, /data-testid="cdp-monitor-entry"/);
  assert.match(accountsSource, /data-testid="account-tags-entry"/);
  assert.match(accountsSource, /data-testid="account-session-transfer-entry"/);
  assert.doesNotMatch(accountsSource, /账号工作区快捷键/);
});

test('account remark is editable beside the verified account name', () => {
  assert.match(accountsSource, /data-testid=\{`account-remark-\$\{acct\.id\}`\}/);
  assert.match(accountsSource, /api\.updateAccountRemark\(accountID, remarkDraft\)/);
  assert.match(accountsSource, /acct\.remark \|\| '添加备注'/);
  assert.doesNotMatch(accountsSource, />备注 \/ 标签</);
});

test('Meta login identities stay combined while explicit groups merge legacy split records', () => {
  assert.match(accountsSource, /function buildAccountCards/);
  assert.match(accountsSource, /\(account\.accountGroup \|\| ''\)\.trim\(\)/);
  assert.match(accountsSource, /groupedMeta\.get\(group\)/);
  assert.match(accountsSource, /result\.push\(\{ primary: account, members: \[account\], isMeta: isMetaPlatform\(account\.platformCode\) \}\)/);
  assert.match(accountsSource, /result\.push\(\{ primary, members, isMeta: true \}\)/);
	assert.match(accountsSource, /const platformName = isMetaCard \? 'Meta'/);
	assert.match(accountsSource, /font-semibold text-gray-600">\{platformName\}/);
  assert.match(accountsSource, /data-testid=\{`meta-identities-\$\{acct\.id\}`\}/);
  assert.match(accountsSource, /cardMembers\.map\(\(member\) =>/);
  assert.match(accountsSource, /title=\{`打开 \$\{member\.platformCode\} 会话`\}/);
  assert.doesNotMatch(accountsSource, /groupedMeta\.get\(account\.name\)/);
});

test('Meta card renders separate Facebook and Instagram identities', () => {
  assert.match(accountsSource, /getAccountAssets/);
  assert.match(accountsSource, /meta-asset-identities-/);
  assert.match(accountsSource, /metaAssets\.map\(\(asset\) =>/);
  assert.match(accountsSource, /src=\{asset\.avatar\}/);
  assert.match(accountsSource, /asset\.platform_code === 'facebook' \? 'Facebook' : 'Instagram'/);
  assert.doesNotMatch(accountsSource, /Meta 登录头像/);
});

test('avatar selection control stays outside the visible portrait center', () => {
  assert.match(accountsSource, /left-0 top-0 -translate-x-1\/2 -translate-y-1\/2 z-10 w-4 h-4/);
});

test('add-account presents Facebook and Instagram as one Meta login identity', () => {
  assert.match(accountsSource, /const addPlatformOptions = React\.useMemo/);
  assert.match(accountsSource, /platform\.code !== 'facebook' && platform\.code !== 'instagram'/);
  assert.match(accountsSource, /code: 'meta', name: 'Meta'/);
  assert.match(accountsSource, /data-testid=\{`add-account-platform-\$\{plat\.code\}`\}/);
  assert.match(accountsSource, /<PlatformIcon platform=\{plat\.code\}/);
});
