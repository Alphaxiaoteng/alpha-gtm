import assert from 'node:assert/strict';
import { test } from 'node:test';
import { extractContentTags } from '../src/contentTags.ts';

test('extracts closed, open, and continuous hashtags in input order', () => {
  assert.deepEqual(
    extractContentTags('#标签1#  #标签2#\n#增长#效率工具\n#本地优先'),
    ['标签1', '标签2', '增长', '效率工具', '本地优先'],
  );
});

test('uses spaces, newlines, and Chinese punctuation as hashtag boundaries', () => {
  assert.deepEqual(
    extractContentTags('正文 #AI创作，#内容运营；#效率工具、#本地优先。\n#第二行'),
    ['AI创作', '内容运营', '效率工具', '本地优先', '第二行'],
  );
});

test('deduplicates tags and ignores Markdown headings or embedded word fragments', () => {
  assert.deepEqual(
    extractContentTags('# 文章标题\n## 二级标题\nC#语言不是标签\n#产品 #产品#教程'),
    ['产品', '教程'],
  );
});
