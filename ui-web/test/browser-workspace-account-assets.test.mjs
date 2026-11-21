import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const source = await readFile(new URL('../src/components/BrowserWorkspaceView.tsx', import.meta.url), 'utf8');
const identityMark = await readFile(new URL('../src/components/BrowserIdentityMark.tsx', import.meta.url), 'utf8');

test('simplified browser workspace renders Meta assets and explicit platform names', () => {
  assert.match(source, /useSWR\('api\/account-assets', api\.getAccountAssets/);
  assert.match(source, /accountAssetsByAccount/);
  assert.match(source, /asset\.asset_type === 'browser_account'/);
  assert.match(source, /meta-browser-assets-/);
  assert.match(source, /src=\{asset\.avatar\}/);
  assert.match(source, /platformNames\[asset\.platform_code\]/);
  assert.match(source, /platformNames\[account\.platformCode\]/);
  assert.match(source, /testId=\{`ego-account-avatar-\$\{account\.id\}`\}/);
  assert.match(source, /data-testid=\{`ego-account-platform-\$\{account\.id\}`\}>\s*<PlatformIcon platform=\{account\.platformCode\} className="h-3 w-3"/);
});

test('every browser identity uses its official EgoLite profile avatar', () => {
  assert.match(source, /<BrowserIdentityMark identity=\{identity\} index=\{identityIndex\}/);
  assert.match(identityMark, /identity\.avatar_url/);
  assert.doesNotMatch(identityMark, /\/browsers\/chromium\.png/);
  assert.match(identityMark, /data-testid=\{`browser-identity-mark-\$\{identity\.id\}`\}/);
});

test('TikTok account cards display the account ID instead of the profile bio', () => {
  assert.match(source, /const primaryIdentity = account\.platformCode === 'tiktok'/);
  assert.match(source, /account\.accountIdentifier \|\| account\.name/);
  assert.match(source, /const secondaryIdentity = account\.platformCode === 'tiktok' \? '' : account\.accountIdentifier/);
  assert.match(source, /account\.loginState === 'logged_in' \|\| account\.accountIdentifier \? primaryIdentity : '登录后自动识别'/);
});
