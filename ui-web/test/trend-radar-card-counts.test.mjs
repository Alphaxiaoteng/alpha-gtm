import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');
const crawler = await readFile(new URL('../src/components/CrawlerView.tsx', import.meta.url), 'utf8');
const dashboard = await readFile(new URL('../src/components/DashboardView.tsx', import.meta.url), 'utf8');

// 平台返回的卡（广告卡/引导卡）与「读不到」分开计数：抓取记录页与榜单页都要能看到，
// 字段名与后端 radar.RunCall / radar.Run / trendRadarSummary 的 JSON 保持一致。

test('api types carry the card counts on calls, runs and summary', () => {
  for (const field of ['ad_count?: number;', 'guide_count?: number;', 'unreadable_count?: number;']) {
    assert.ok(api.includes(field), `TrendRadarRunCall 缺字段 ${field}`);
  }
  assert.match(api, /export interface TrendRadarRun \{[\s\S]*ad_cards\?: number;[\s\S]*guide_cards\?: number;[\s\S]*unreadable\?: number;/);
  assert.match(api, /export interface TrendRadarSummary \{[\s\S]*ad_cards\?: number;[\s\S]*guide_cards\?: number;[\s\S]*unreadable\?: number;/);
});

test('crawler run rows show ads, guide cards and unreadable separately', () => {
  for (const header of ['广告卡', '引导卡', '读不到']) {
    assert.ok(crawler.includes(`>${header}</th>`), `调用明细缺表头 ${header}`);
  }
  for (const field of ['run.ad_cards', 'run.guide_cards', 'run.unreadable']) {
    assert.ok(crawler.includes(field), `每轮摘要缺 ${field}`);
  }
  for (const field of ['call.ad_count', 'call.guide_count', 'call.unreadable_count']) {
    assert.ok(crawler.includes(field), `调用明细缺 ${field}`);
  }
  // 失败的调用没读到任何条目，卡计数显示「—」，不能写成 0。
  assert.match(crawler, /call\.status === 'succeeded' \? \(call\.ad_count \?\? 0\) : '—'/);
  assert.match(crawler, /call\.status === 'succeeded' \? \(call\.unreadable_count \?\? 0\) : '—'/);
});

test('dashboard subtitle surfaces the card counts only when there are any', () => {
  assert.ok(dashboard.includes('ad_cards'), '榜单页副标题缺广告卡计数');
  assert.ok(dashboard.includes('guide_cards'), '榜单页副标题缺引导卡计数');
  assert.match(dashboard, /cardTotal > 0/);
  assert.match(dashboard, /读不到 \$\{data\?\.unreadable \?\? 0\}/);
});
