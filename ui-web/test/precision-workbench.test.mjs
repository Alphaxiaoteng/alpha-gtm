import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

const source = (path) => readFile(new URL(path, import.meta.url), 'utf8');

test('the default skin is a cold-white precision workbench with semantic colors', async () => {
  const css = await source('../src/index.css');
  assert.match(css, /--brand-bg:\s*#fcfcfb/i);
  // 侧栏改为纯白：与主内容区同底，靠分隔线与留白分栏，而不是靠灰底。
  assert.match(css, /--brand-sidebar:\s*#ffffff/i);
  assert.match(css, /--brand-border:\s*#e5e5e1/i);
  assert.match(css, /--font-body-runtime:[^;]*PingFang SC/);
  assert.match(css, /--brand-success:\s*#15803d/);
  assert.match(css, /--brand-danger:\s*#dc2626/);
  assert.doesNotMatch(css, /--color-red-500:\s*var\(--color-gray-500\)/);
  assert.doesNotMatch(css, /--color-amber-500:\s*var\(--color-gray-500\)/);
  assert.match(css, /\.ui-section\s*\{/);
  assert.match(css, /\.ui-row\s*\{/);
  assert.match(css, /\.ui-status-success\s*\{/);
  assert.match(css, /\.ui-empty\s*\{/);
});

test('the shell uses a restrained utility toolbar and compact brand lockup', async () => {
  const layout = await source('../src/components/Layout.tsx');
  assert.match(layout, /data-testid="app-utility-toolbar"/);
  assert.match(layout, /className="ui-utility-toolbar [^"]+"/);
  // 顶部品牌区已移除字标，工具条保持紧凑
  assert.doesNotMatch(layout, /alpha-intelligence-logo\.png/);
  assert.doesNotMatch(layout, /data-testid="app-utility-toolbar"[\s\S]{0,1600}shadow-sm/);
  assert.doesNotMatch(layout, /backendHealth\?\.version \|\| overview\?\.version/);
});

test('priority workspaces use rows and a compact data strip instead of card soup', async () => {
  const knowledge = await source('../src/components/KnowledgeView.tsx');
  const accounts = await source('../src/components/AccountsView.tsx');
  const metrics = await source('../src/components/XHSDataDashboardView.tsx');
  assert.match(knowledge, /className=\{`ui-row/);
  assert.match(knowledge, /data-testid="knowledge-current-base"/);
  assert.match(accounts, /data-testid="accounts-toolbar"[^>]*ui-section/);
  assert.match(accounts, /data-testid=\{`account-card-\$\{acct\.id\}`\}/);
  assert.match(accounts, /className=\{`ui-card-flat/);
  assert.match(metrics, /data-testid="creator-metrics-summary"[^>]*ui-data-strip/);
  assert.doesNotMatch(metrics, /text-cyan-600/);
  assert.doesNotMatch(metrics, /text-violet-600/);
});

test('Alpha Agent uses flat assistant messages and one restrained composer', async () => {
  const drawer = await source('../src/components/AgentDrawer.tsx');
  const bubble = await source('../src/vendor/openmausbot/components/Bubble.tsx');
  assert.match(drawer, /data-testid="agent-composer"/);
  assert.match(drawer, /<Composer/);
  assert.match(bubble, /agent-message-assistant/);
  assert.match(bubble, /agent-message-user/);
  assert.doesNotMatch(bubble, /agent-message-user[^\n]*bg-brand-accent/);
  assert.doesNotMatch(drawer, /data-testid="agent-composer"[^>]*shadow-sm/);
  // 对话区不是一堆白：助手与用户消息都有可见的浅灰表面。
  const css = await source('../src/index.css');
  assert.match(css, /\.bot-theme \.agent-message-assistant\s*\{\s*background: var\(--brand-surface-raised\)/);
  assert.match(css, /\.bot-theme \.agent-message-user\s*\{\s*background: color-mix/);
});
