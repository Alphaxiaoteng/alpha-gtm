import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const accountsSource = await readFile(
  new URL('../src/components/AccountsView.tsx', import.meta.url),
  'utf8',
);
const apiSource = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');
const accountAvatarSource = await readFile(
  new URL('../src/components/AccountAvatar.tsx', import.meta.url),
  'utf8',
);
const accountCardSource = accountsSource.slice(
  accountsSource.indexOf('data-testid={`account-card-'),
  accountsSource.indexOf('{/* Batch Action Bar */}'),
);
const historySource = accountsSource.slice(
  accountsSource.indexOf('{/* MODAL: durable login observation history */}'),
  accountsSource.indexOf('{/* MODAL: Add Account */}'),
);

test('account workspace exposes durable passive observation without publish actions', () => {
  assert.match(apiSource, /observeAccount:/);
  assert.match(apiSource, /observeAccounts:/);
  assert.match(apiSource, /getAccountLoginEvents:/);
  assert.match(apiSource, /getAccountBrowserEvents:/);
  assert.match(apiSource, /getAccountObserverStatus:/);

  assert.match(accountsSource, /立即串行读取已打开平台页面的昵称、账号头像与登录证据；不会发布内容/);
  assert.match(accountsSource, /isObservingAll \|\| observerStatus\?\.observing \? '监测中' : '检测'/);
  assert.match(accountsSource, /状态记录/);
  assert.doesNotMatch(accountCardSource, /登录模式|最近证据|具体原因|真实头像已存本地|平台图标占位/);
  assert.doesNotMatch(accountCardSource, /CDP \{acct\.cdpPort\}|lease \{acct\.leaseOwner\}/);
  assert.match(accountCardSource, /account-action-hint-/);
  assert.match(historySource, /浏览器运行记录/);
  assert.match(historySource, /平台登录证据/);
  assert.match(historySource, /登录模式/);
  assert.match(historySource, /首次发现/);
  assert.match(historySource, /最后确认/);
  assert.match(historySource, /证据码：\{event\.reasonCode\}/);
  assert.match(historySource, /头像来源：\{event\.avatarSource/);
	assert.match(apiSource, /avatarErrorCode\?: string/);
	assert.match(accountsSource, /头像待更新，不影响账号登录/);
  assert.doesNotMatch(accountsSource, /publishAccount|createPublishJob|submitPublish/);
});

test('account avatars use one circular visual contract and never send local auth to remote CDNs', () => {
  assert.match(apiSource, /getAccountAvatarBlob: \(avatarURL: string\) => requestBlob\(avatarURL\)/);
  assert.match(accountAvatarSource, /isManagedAvatarSource/);
  assert.match(accountAvatarSource, /api\.getAccountAvatarBlob\(src\)/);
  assert.match(accountAvatarSource, /URL\.createObjectURL\(blob\)/);
  assert.match(accountAvatarSource, /URL\.revokeObjectURL\(createdURL\)/);
  assert.match(accountAvatarSource, /avatarRetryLimit/);
  assert.match(accountAvatarSource, /setRetryVersion/);
  assert.match(accountAvatarSource, /object-cover object-center/);
  assert.match(accountAvatarSource, /rounded-full/);
  assert.doesNotMatch(accountAvatarSource, /border-2 border-brand-surface/);
  assert.match(accountAvatarSource, /referrerPolicy="no-referrer"/);
  assert.match(accountAvatarSource, /role="img"/);
  assert.match(accountAvatarSource, /aria-label=\{alt\}/);
  assert.match(accountsSource, /<AccountAvatar/);
  assert.doesNotMatch(accountCardSource, /<img[^>]+src=\{(?:acct|identity)/);
  assert.doesNotMatch(accountCardSource, /object-cover/);
});

test('missing portraits use a plain platform mark without a white avatar disc', () => {
  assert.match(accountAvatarSource, /const hasPortrait = Boolean\(displaySource\)/);
  assert.match(accountAvatarSource, /hasPortrait \? 'rounded-full bg-brand-bg' : 'bg-transparent'/);
  assert.match(accountAvatarSource, /platform=\{platform\} className="h-full w-full"/);
});

test('account selection is accessible from the avatar corner and exits cleanly', () => {
  assert.match(accountCardSource, /type="checkbox"/);
  assert.match(accountCardSource, /aria-label=\{`选择账号 \$\{acct\.name\}`\}/);
  assert.match(accountCardSource, /title=\{accountSelected \? '取消选择账号' : '选择账号'\}/);
  const avatarArea = accountCardSource.slice(
    accountCardSource.indexOf('<div className="relative flex-shrink-0">'),
    accountCardSource.indexOf('<div className="min-w-0">'),
  );
  assert.match(avatarArea, /type="checkbox"/);
  assert.match(avatarArea, /absolute left-0 top-0 -translate-x-1\/2 -translate-y-1\/2/);
  assert.match(accountsSource, /if \(historyAccount\)[\s\S]*else if \(isTagsOpen\)[\s\S]*else \{\s*exitBatchSelection\(\)/);
  assert.match(accountsSource, /取消（Esc）/);
});

test('account status refreshes quickly without continuously animated circles', () => {
  assert.match(accountsSource, /const accountStateRefreshInterval = 1_000/);
  assert.doesNotMatch(accountsSource, /animate-ping/);

  for (const testID of [
    'account-observer-indicator',
    'running-browser-indicator',
    'account-login-indicator-',
  ]) {
    const start = accountsSource.indexOf(testID);
    assert.notEqual(start, -1, `missing ${testID}`);
    assert.doesNotMatch(accountsSource.slice(start, start + 420), /animate-(?:ping|pulse)/);
  }
});

test('browser health copy does not present focus failure as durable degradation', () => {
  assert.doesNotMatch(accountsSource, /窗口控制受限/);
  assert.match(accountsSource, /浏览器连接异常，但账号仍已登录/);
});

test('account authentication labels distinguish login, dropout, and unverified states', () => {
  assert.match(accountsSource, /state === 'logged_in'\) return '已登录'/);
  assert.match(accountsSource, /state === 'logged_out'\) return '已掉线'/);
  assert.match(accountsSource, /return '待验证'/);
  assert.match(accountsSource, /PLATFORM_UNREACHABLE: '平台当前不可访问，保留上次登录状态'/);
  assert.doesNotMatch(accountsSource, /已保存登录态|已登出/);
});
