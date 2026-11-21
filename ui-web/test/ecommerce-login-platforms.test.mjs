import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';

test('account configuration exposes login-only e-commerce consoles after social platforms', async () => {
  const source = await readFile(new URL('../src/components/BrowserWorkspaceView.tsx', import.meta.url), 'utf8');
  const codes = [
    'douyin_shop', 'taobao', 'tiktok_shop', 'shopee', 'shopify', 'amazon_seller',
    'qianchuan', 'jd_shop', 'pinduoduo_shop', 'xhs_shop', 'kuaishou_shop', 'wechat_shop',
    'alibaba_1688', 'aliexpress', 'xianyu',
  ];
  let previous = source.indexOf("'csdn'");
  assert.ok(previous >= 0);
  for (const code of codes) {
    const index = source.indexOf(`'${code}'`, previous + 1);
    assert.ok(index > previous, `${code} is missing or out of order`);
    previous = index;
  }
  for (const name of [
    '抖音电商', '淘宝', 'TikTok Shop', 'Shopee', 'Shopify', 'Amazon Seller',
    '巨量千川', '京东店铺', '拼多多店铺', '小红书店铺', '快手小店', '微信小店', '1688', 'AliExpress', '闲鱼',
  ]) {
    assert.match(source, new RegExp(name));
  }
});
