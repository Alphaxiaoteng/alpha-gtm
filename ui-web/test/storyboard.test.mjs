import assert from 'node:assert/strict';
import test from 'node:test';

import { parseStoryboardMarkdownTable } from '../src/storyboard.ts';

test('parses a storyboard markdown table into ordered cards', () => {
  const cards = parseStoryboardMarkdownTable(`
| 镜号 | 时长 | 画面 | 景别 | 旁白 | 素材 |
| --- | --- | --- | --- | --- | --- |
| 1 | 0-3s | 手机弹出提醒 | 特写 | 你也遇到过吗？ | 手机录屏 |
| 2 | 3-8s | 主播看镜头 | 中景 | 先别急着重装。 | 口播 |
`);

  assert.deepEqual(cards, [
    { id: '1', duration: '0-3s', visual: '手机弹出提醒', shot: '特写', narration: '你也遇到过吗？', asset: '手机录屏' },
    { id: '2', duration: '3-8s', visual: '主播看镜头', shot: '中景', narration: '先别急着重装。', asset: '口播' },
  ]);
});

test('accepts common column aliases and keeps missing cells empty', () => {
  const cards = parseStoryboardMarkdownTable(`
| 序号 | 时间 | 场景 | 镜头 | 台词 |
| :--- | ---: | --- | --- | --- |
| A | 5 秒 | 工作台 | 全景 | 开始吧 |
`);

  assert.deepEqual(cards, [
    { id: 'A', duration: '5 秒', visual: '工作台', shot: '全景', narration: '开始吧', asset: '' },
  ]);
});

test('parses the exact local Agent storyboard headers', () => {
  const cards = parseStoryboardMarkdownTable(`
| 镜号 | 时长 | 画面 | 景别/运动 | 旁白/字幕 | 所需素材 |
|---|---:|---|---|---|---|
| 1 | 0–3s | 呈现核心冲突 | 近景/快速切入 | 提出问题 | 真实场景素材 |
`);

  assert.deepEqual(cards[0], {
    id: '1',
    duration: '0–3s',
    visual: '呈现核心冲突',
    shot: '近景/快速切入',
    narration: '提出问题',
    asset: '真实场景素材',
  });
});

test('returns no cards for prose or an incomplete table', () => {
  assert.deepEqual(parseStoryboardMarkdownTable('先拍开场，再切到产品界面。'), []);
  assert.deepEqual(parseStoryboardMarkdownTable('| 镜号 | 画面 |\n| --- | --- |'), []);
});

test('caps an oversized storyboard to a safe local preview size', () => {
  const rows = Array.from({ length: 120 }, (_, index) => `| ${index + 1} | 1s | 画面 ${index + 1} | 特写 | 台词 | 素材 |`).join('\n');
  const cards = parseStoryboardMarkdownTable(`| 镜号 | 时长 | 画面 | 景别 | 旁白 | 素材 |\n| --- | --- | --- | --- | --- | --- |\n${rows}`);

  assert.equal(cards.length, 100);
  assert.equal(cards.at(-1)?.id, '100');
});
