import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const drawer = await readFile(new URL('../src/components/FeedbackDrawer.tsx', import.meta.url), 'utf8');
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');
const styles = await readFile(new URL('../src/index.css', import.meta.url), 'utf8');

test('feedback drawer remains available as a component but is removed from the primary shell', () => {
  assert.doesNotMatch(layout, /data-testid="feedback-drawer-toggle"/);
  assert.doesNotMatch(layout, /<FeedbackDrawer/);
  assert.match(drawer, /data-testid="feedback-drawer"/);
  assert.match(drawer, /data-testid="feedback-history"/);
  assert.match(drawer, /data-testid="feedback-composer"/);
  assert.match(drawer, /left: sidebarHidden \? '0\.75rem' : 'calc\(var\(--ui-sidebar-w\) \+ 0\.75rem\)'/);
  assert.doesNotMatch(drawer, /left-\[15\.75rem\]/);
  assert.doesNotMatch(layout, /setCalendarOpen/);
  assert.doesNotMatch(layout, /setFeedbackOpen/);
  assert.match(drawer, /内容将发送给 Alpha-Nexus 开发者/);
  assert.match(drawer, /开发者回复/);
  for (const status of ['待处理', '处理中', '已完成', '不采纳']) {
    assert.match(drawer, new RegExp(status));
  }
});

test('feedback refreshes while open and preserves the draft when submission fails', () => {
  assert.match(drawer, /30_000/);
  assert.match(drawer, /getFeedback\(refresh\)/);
  assert.match(api, /configured:\s*boolean/);
  assert.match(drawer, /setConfigured\(result\.configured === true\)/);
  assert.doesNotMatch(drawer, /disabled=\{!valid \|\| submitting \|\| !configured\}/);
  assert.match(drawer, /开发者反馈通道未启用，内容仅保存在本机/);
  assert.match(drawer, /submitFeedback/);
  assert.match(drawer, /setDraft\(''\)/);
  assert.match(drawer, /2–2000/);
  assert.match(api, /getFeedback:/);
  assert.match(api, /submitFeedback:/);
  assert.match(api, /developer_reply/);
});

test('feedback drawer explains each stable failure without clearing the draft', () => {
  assert.match(drawer, /FEEDBACK_NOT_CONFIGURED/);
  assert.match(drawer, /开发者配置未完成/);
  assert.match(drawer, /FEEDBACK_SCHEMA_MISMATCH/);
  assert.match(drawer, /FEEDBACK_REMOTE_UNAVAILABLE/);
  assert.match(drawer, /LOCAL_STORE_FAILED/);
  assert.match(drawer, /FEEDBACK_INVALID_INPUT/);
  assert.match(drawer, /内容已保留/);
  assert.doesNotMatch(drawer, /catch \(error\) \{[\s\S]{0,160}setDraft\(/);
  assert.match(drawer, /role="status"/);
  assert.match(drawer, /aria-live="polite"/);
});

test('feedback draft survives drawer close and refresh through app-scoped local storage', () => {
  assert.match(drawer, /FEEDBACK_DRAFT_STORAGE_KEY\s*=\s*['"]alpha-nexus-egolite:feedback-draft['"]/);
  assert.match(drawer, /localStorage\.getItem\(FEEDBACK_DRAFT_STORAGE_KEY\)/);
  assert.match(drawer, /localStorage\.setItem\(FEEDBACK_DRAFT_STORAGE_KEY/);
  assert.match(drawer, /localStorage\.removeItem\(FEEDBACK_DRAFT_STORAGE_KEY\)/);
});

test('unconfigured feedback offers honest clipboard fallback and preserves the draft on failure', () => {
  assert.match(drawer, /navigator\.clipboard\.writeText/);
  assert.match(drawer, /复制内容/);
  assert.match(drawer, /尚未提交/);
  assert.match(drawer, /复制失败/);
  assert.match(drawer, /!configured/);
  assert.match(drawer, /内容仍保存在本机草稿中/);
});

test('feedback composer has one quiet helper row and a textarea immune to global input chrome', () => {
  assert.match(drawer, /feedback-draft-input/);
  assert.match(styles, /#root textarea\.feedback-draft-input\s*\{[^}]*background(?:-color)?:\s*transparent\s*!important;[^}]*border:\s*0\s*!important;[^}]*border-radius:\s*0\s*!important;[^}]*box-shadow:\s*none\s*!important;[^}]*padding:\s*0\s*!important;/s);
  assert.match(styles, /#root textarea\.feedback-draft-input:focus\s*\{[^}]*box-shadow:\s*none\s*!important;/s);
  assert.doesNotMatch(drawer, /message \|\| ['"]输入至少 2 个字/);
  assert.match(drawer, /\{message \? \(/);
});
