import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';

test('studio makes direct local-path indexing the primary material entry', async () => {
  const source = await readFile(new URL('../src/components/StudioView.tsx', import.meta.url), 'utf8');

  assert.match(source, /studio-direct-media-path/);
  assert.match(source, /本地素材绝对路径/);
  assert.match(source, /仅索引并校验文件；不会复制原文件/);
  assert.match(source, /发布时由后台从冻结路径静默上传，不打开平台文件选择器/);
  assert.doesNotMatch(source, /高级：手动输入路径/);
});
