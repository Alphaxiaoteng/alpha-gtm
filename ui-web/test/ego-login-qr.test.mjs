import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

test('EgoLite login sync survives dialog close without duplicate browser polling in Electron', async () => {
  const source = await readFile(new URL('../src/components/BrowserWorkspaceView.tsx', import.meta.url), 'utf8');
	const apiSource = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');
	const openLoginSource = source.slice(source.indexOf('async function openPlatformLogin'), source.indexOf('async function refreshLoginQR'));
	const activeBatchSource = source.slice(source.indexOf('const activeLoginBatches = Object.entries(activeBatches)'), source.indexOf('async function verifyLoginBatch'));

	assert.match(source, /useSWR\('api\/accounts', api\.getAccounts, \{ refreshInterval: 1_000 \}\)/);
	assert.match(openLoginSource, /if \(result\.state === 'logged_in'\)/);
	assert.match(openLoginSource, /api\.openEgoLoginQR\(account\.id, false, true\)/);
	assert.match(openLoginSource, /await api\.focusEgoLite\(\)/);
	assert.match(openLoginSource, /api\.verifyEgoLoginBatch\(identity\.id, \[account\.platformCode\]\)/);
	assert.match(openLoginSource, /await refresh\(\)/);
  assert.match(source, /api\.openEgoLoginQR\(loginQR\.account\.id, true, false\)/);
  assert.match(source, /Date\.now\(\) >= expiresAt - 3_000/);
	assert.match(source, /loginQR\?\.result\.state !== 'qr_ready' && loginQR\?\.result\.state !== 'manual_takeover'/);
	assert.match(source, /}, 2_000\)/);
	assert.match(source, /loginQRRequestInFlight/);
	assert.match(source, /autoRefreshFailures >= 3/);
	assert.match(source, /Math\.min\(60_000, 10_000 \* 2 \*\* autoRefreshFailures\)/);
	assert.match(source, /自动刷新暂时失败/);
	assert.match(source, /liveLoginQRState !== 'logged_in'/);
  assert.match(source, /if \(result\.state === 'logged_in'\)/);
  assert.match(source, /api\.verifyEgoLoginBatch\(loginQR\.identity\.id, \[loginQR\.account\.platformCode\]\)/);
  assert.match(source, /await refresh\(\)/);
	assert.match(source, /api\.releaseEgoLoginQR\(accountID\)/);
	assert.match(source, /loginQR\?\.account\.id === account\.id \? '登录中'/);
	assert.match(source, /const activeLoginBatches = Object\.entries\(activeBatches\)/);
	// 登录会话列表接口在当前构建里被有意断言为 404（见
	// internal/ui/browser_config_test.go），此前前端每 2 秒轮询一次、永远拿
	// 404。批量登录状态改由本页 activeBatches 维护，这里固定住「不再轮询」。
	assert.doesNotMatch(source, /api\/login-sessions/);
	assert.doesNotMatch(source, /mutateLoginSessions/);
	assert.match(source, /platformCodes\.filter\(\(code\) => visiblePlatformCodes\.includes\(code\)\)/);
	assert.doesNotMatch(activeBatchSource, /api\.verifyEgoLoginBatch/);
	assert.match(activeBatchSource, /modeAccounts\.find\(\(item\) => item\.identityId === identityID && item\.platformCode === platformCode\)/);
	assert.match(source, /announcedLoginAccounts/);
	assert.match(source, /account\.loginState !== 'logged_in'/);
	assert.match(apiSource, /getAccounts: \(\) => request<Account\[]>\('\/api\/accounts', \{ cache: 'no-store' \}\)/);
});

test('single-platform login dialog can verify immediately instead of waiting for the hourly monitor', async () => {
  const source = await readFile(new URL('../src/components/BrowserWorkspaceView.tsx', import.meta.url), 'utf8');
  const completeSource = source.slice(
    source.indexOf('async function completeLoginQR'),
    source.indexOf('function closeLoginQR'),
  );

  assert.match(completeSource, /api\.verifyEgoLoginBatch\(current\.identity\.id, \[current\.account\.platformCode\]\)/);
  assert.match(completeSource, /await refresh\(\)/);
  assert.match(completeSource, /result\.accounts\.find/);
  assert.match(completeSource, /loginState !== 'logged_in'/);
  assert.match(source, /完成登录并同步/);
  assert.match(source, /同步中…/);
  assert.doesNotMatch(source, /回到这里点击本批登录完成/);
});

test('manual login dialog focuses EgoLite and re-syncs the current page QR', async () => {
  const source = await readFile(new URL('../src/components/BrowserWorkspaceView.tsx', import.meta.url), 'utf8');
  const manualLoginSource = source.slice(
    source.indexOf('此平台需要在 EgoLite 完成登录'),
    source.indexOf('完成登录并同步', source.indexOf('此平台需要在 EgoLite 完成登录')),
  );

  assert.match(manualLoginSource, /在 EgoLite 显示并同步二维码/);
  assert.match(manualLoginSource, /onClick=\{\(\) => void refreshLoginQR\(true\)\}/);
});
