import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');

test('workspace mode defines a persisted media/shop contract', async () => {
  const source = await read('../src/workspaceMode.ts');
  assert.match(source, /export type ProductMode = 'media' \| 'shop'/);
  assert.match(source, /DEFAULT_PRODUCT_MODE(?:\s*:\s*ProductMode)?\s*=\s*'media'/);
  assert.match(source, /alpha-nexus-egolite-product-mode-v1/);
  assert.match(source, /douyin_shop.*taobao.*tiktok_shop.*shopee.*shopify.*amazon_seller.*qianchuan.*jd_shop.*pinduoduo_shop.*xhs_shop.*kuaishou_shop.*wechat_shop.*alibaba_1688.*aliexpress.*xianyu/s);
  assert.match(source, /platformCodesForMode/);
});

test('store and layout expose a shared media/shop switch with safe shop navigation', async () => {
  const store = await read('../src/store.ts');
  const layout = await read('../src/components/Layout.tsx');
  assert.match(store, /productMode/);
  assert.match(store, /setProductMode/);
  assert.match(store, /isTabAvailableForMode/);
  assert.match(layout, /data-testid="workspace-mode-switch"/);
  assert.match(layout, /'Media'/);
  assert.match(layout, /'Shop'/);
  assert.match(layout, /Clapperboard/);
  assert.match(layout, /Store/);
  assert.match(layout, /aria-label=\{`切换到 \$\{modeLabel\} 模式`\}/);
  assert.match(layout, /setProductMode/);
  assert.match(layout, /浏览器与店铺/);
  assert.match(layout, /经营总览/);
  assert.match(layout, /workspace\.id === 'social'/);
  assert.match(layout, /workspace\.id === 'knowledge'/);
  assert.doesNotMatch(layout, /label: '运行监控'/);
  assert.match(store, /mode === 'shop' \? 'shop'/);
});

test('browser workspace filters the current mode and merges hidden platform configuration', async () => {
  const source = await read('../src/components/BrowserWorkspaceView.tsx');
  const api = await read('../src/api.ts');
  assert.match(source, /platformCodesForMode/);
  assert.match(source, /productMode/);
  assert.match(source, /visibleAccounts|modeAccounts/);
  assert.match(source, /platformCodesForMode\(activeBatches/);
  assert.match(source, /preserved.*platform|platformsToSave|merge/i);
  assert.match(source, /店铺管理/);
  // 头部不再只说「N 个店铺已登录」：三种状态分开列，避免把「待确认」
  // 误读成「没登录」。
  assert.match(source, /data-testid="ego-login-summary"/);
  assert.match(source, /已登录/);
  assert.match(source, /待确认/);
  assert.match(source, /api\.openEgoBrowser\([^;]+identityAccounts\.map/s);
  assert.match(api, /openEgoBrowser: \(identityId: number, offset = 0, platformCodes\?/);
  assert.match(api, /platform_codes: platformCodes/);
});
