import assert from 'node:assert/strict';
import { access, readFile } from 'node:fs/promises';
import test from 'node:test';

const source = await readFile(new URL('../src/components/PlatformIcon.tsx', import.meta.url), 'utf8');
const officialPlatformCodes = [
  'instagram', 'facebook', 'xhs', 'douyin', 'wechat_video', 'kuaishou', 'wechat_mp',
  'youtube', 'tiktok', 'x', 'toutiao', 'omtencent', 'weishi', 'bilibili', 'acfun',
  'baijiahao', 'sina', 'jianshu', 'zhihu', 'juejin', 'csdn', 'pdd',
  'douyin_shop', 'taobao', 'tiktok_shop', 'shopee', 'shopify', 'amazon_seller',
  'qianchuan', 'jd_shop', 'pinduoduo_shop', 'xhs_shop', 'kuaishou_shop',
  'wechat_shop', 'alibaba_1688', 'aliexpress', 'xianyu',
];

test('every official platform renders from a bundled application icon', async () => {
  for (const code of officialPlatformCodes) {
    assert.match(source, new RegExp(`${code}: '/platforms/${code}\\.png'`));
    await access(new URL(`../public/platforms/${code}.png`, import.meta.url));
  }
  assert.doesNotMatch(source, /case 'instagram'|case 'douyin'|case 'wechat_mp'/);
});

test('shop platforms use bundled brand images instead of text placeholders', () => {
  assert.doesNotMatch(source, /const shopFallbackMarks/);
  assert.doesNotMatch(source, /<text[\s\S]*fill="currentColor"/);
  assert.match(source, /aria-label=\{label\}/);
});

test('Meta renders the bundled Facebook and Instagram marks as one composite icon', () => {
  assert.match(source, /code === 'meta'/);
  assert.match(source, /data-testid="platform-icon-meta"/);
  assert.match(source, /officialAppIcon\.facebook/);
  assert.match(source, /officialAppIcon\.instagram/);
});

test('platform marks stay secondary, transparent, and distinguish Douyin from TikTok', () => {
  assert.match(source, /douyin: '\/platforms\/douyin\.png'/);
  assert.match(source, /tiktok: '\/platforms\/tiktok\.png'/);
  assert.match(source, /data-platform=\{code\}/);
  assert.match(source, /object-contain/);
  assert.doesNotMatch(source, /overflow-hidden rounded-\[22%\]/);
});

test('bundled image failures degrade to a generic icon instead of a broken image', () => {
  assert.match(source, /onError=/);
  assert.match(source, /failedSources/);
  assert.match(source, /platform-icon-load-fallback-\$\{code\}/);
});
