import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';

test('workspace navigation exposes the Ego hierarchy and keeps removed products unreachable', async () => {
  const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
  const app = await readFile(new URL('../src/App.tsx', import.meta.url), 'utf8');
  const egoWorkspace = await readFile(new URL('../src/components/BrowserWorkspaceView.tsx', import.meta.url), 'utf8');
  const egoSettings = await readFile(new URL('../src/components/EgoSettingsView.tsx', import.meta.url), 'utf8');
  const publishWorkspace = await readFile(new URL('../src/components/PublishWorkspaceView.tsx', import.meta.url), 'utf8');
  const socialWorkspace = await readFile(new URL('../src/components/SocialMediaWorkspaceView.tsx', import.meta.url), 'utf8');
  const localContent = await readFile(new URL('../src/components/LocalContentView.tsx', import.meta.url), 'utf8');

  for (const id of ['ego', 'studio', 'social', 'dashboard', 'publish', 'settings']) {
    assert.match(layout, new RegExp(`id: '${id}'`));
  }
  assert.doesNotMatch(layout, /id: 'creation'/);
  assert.doesNotMatch(layout, /id: 'skills'/);
  for (const removed of ['browser', 'agent', 'ai-image']) {
    assert.doesNotMatch(layout, new RegExp(`id: '${removed}'`));
  }
  assert.match(layout, /data-testid={`nav-\$\{workspace\.id\}`}/);
  assert.match(layout, /label: '浏览器与账号'/);
  assert.match(layout, /label: '内容库'/);
  assert.match(layout, /matches: \['settings', 'proxies', 'skills', 'logs'\]/);
  assert.match(layout, /label: '趋势雷达'/);
  assert.doesNotMatch(layout, /subnav-|const navItems =/);
  assert.doesNotMatch(layout, /data-testid="agent-conversation-sidebar"/);
  assert.match(layout, /<AgentContextDrawer selection=/);
  assert.doesNotMatch(layout, /data-testid="content-calendar-toggle"/);
  assert.doesNotMatch(layout, /data-testid="feedback-drawer-toggle"/);

  assert.match(egoWorkspace, /data-testid="ego-workspace"/);
  assert.match(egoWorkspace, /api\.getEgoIdentities/);
  assert.doesNotMatch(egoWorkspace, /api\.getEgoSpaces/);
  assert.match(egoWorkspace, /api\.calibrateEgoConcurrency/);
  assert.match(egoWorkspace, /api\.getEgoSessionMonitor/);
  assert.match(egoWorkspace, /api\.updateEgoSessionMonitor/);
  assert.match(egoWorkspace, /api\.runEgoSessionMonitor/);
  assert.match(egoWorkspace, /登录状态监控/);
  assert.match(egoWorkspace, /unknown 保留原状态/);
  assert.match(egoWorkspace, /CHECKPOINT_REQUIRED/);
  assert.match(egoWorkspace, /needsLoginAttention\(account\)/);
  assert.match(egoWorkspace, /account\.identityId === identity\.id && account\.enabled/);
  // 汇总按三种登录状态分别计数，而不是只数已登录。
  assert.match(egoWorkspace, /item\.loginState === 'logged_in'/);
  assert.match(egoWorkspace, /item\.loginState === 'unknown'/);
  assert.match(egoWorkspace, /个待处理/);
  assert.match(egoWorkspace, /}, 2_000\);/);
  assert.doesNotMatch(egoWorkspace, /}, 1_200\);/);
  assert.doesNotMatch(egoWorkspace, /CDP|fingerprint|BrowserFacade/);

  assert.match(egoSettings, /data-testid="ego-settings"/);
  assert.match(egoSettings, /AlphaAgentSettingsSection/);
  assert.match(egoSettings, /SkillLibraryView/);
  assert.match(egoSettings, /\{ id: 'skills', label: 'Skill 库'/);
  assert.match(egoSettings, /data-testid=\{`settings-section-\$\{item\.id\}`\}/);
  assert.doesNotMatch(egoSettings, /from '\.\/SettingsView'/);
  assert.doesNotMatch(egoSettings, /AI 图片生成配置|浏览器路径/);
  assert.doesNotMatch(app, /case 'ai-image'/);
  assert.match(app, /StudioWorkspaceView/);
  assert.match(app, /case 'studio'/);
  assert.match(app, /case 'agent':[\s\S]{0,200}case 'bot':\s*return null;/);
  assert.match(app, /agentStage=\{<AgentDrawer displayMode="stage" \/>\}/);

  assert.match(publishWorkspace, /data-testid="publish-workspace"/);
  assert.match(publishWorkspace, /<PublishView embedded/);
  for (const section of ['video', 'graph_text', 'article']) {
    assert.match(socialWorkspace, new RegExp(`id: '${section}'`));
  }
  assert.match(socialWorkspace, /<LocalContentView kind=\{section\} domain=\{domain\}[^>]*\/>/);
  assert.match(localContent, /setCreateType\(kind\)/);
  assert.match(localContent, /data-testid="local-content-title"/);
  assert.match(localContent, /data-testid="local-content-video-file"/);
  assert.match(localContent, /data-testid="local-content-body"/);
  assert.match(localContent, /data-testid="content-library-board"/);
});
