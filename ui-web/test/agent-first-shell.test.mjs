import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');

test('agent-first shell keeps domain tools, scoped conversations and contextual detail separate', async () => {
  const layout = await read('../src/components/Layout.tsx');
  const drawer = await read('../src/components/AgentDrawer.tsx');
  const context = await read('../src/components/AgentContextDrawer.tsx');
  const store = await read('../src/store.ts');

  assert.match(layout, /data-testid="workspace-mode-switch"/);
  assert.match(layout, /aria-label=\{`\$\{productMode === 'media' \? 'Media' : 'Shop'\} 业务入口`\}/);
  assert.doesNotMatch(layout, /data-testid="agent-conversation-sidebar"/);
  // 会话列表已从侧栏移除,resume 走 Agent Drawer 自身;Layout 不再持有这两个钩子。
  assert.match(layout, /<AgentContextDrawer selection=\{contextSelection\}/);
  assert.doesNotMatch(layout, /<AgentDrawer \/>/);

  assert.match(drawer, /displayMode = 'drawer'/);
  assert.match(drawer, /data-layout=\{displayMode === 'stage' \? 'stage' : 'docked'\}/);
  assert.match(drawer, /product_mode: productMode/);
  assert.match(drawer, /openAgentContext/);
  // Harness wording stays out of the UI: provider identity is a logo or a
  // neutral text badge, the product name is just Alpha Agent.
  assert.doesNotMatch(drawer, /Alpha Agent · Codex/);
  assert.match(drawer, /PROVIDER_DISPLAY/);
  // `custom` 是 next 供应商（OpenCode 适配器执行），不再借 DeepSeek 鲸鱼标。
  assert.doesNotMatch(drawer, /deepseek-logo\.svg/);

  assert.match(context, /AGENT_CONTEXT_OPEN_EVENT/);
  assert.match(context, /if \(!selection\) return null/);
  assert.match(context, /在 EgoLite 中打开/);
  assert.doesNotMatch(context, /setInterval|refreshInterval|screenshotBase64/);

  assert.match(store, /DEFAULT_AGENT_PROVIDER = 'dsh'/);
  assert.match(store, /requestedAgentConversationID/);
  assert.match(store, /activeTab: 'agent'/);
});

test('fresh Media opens land on the home page while /agent keeps the conversation stage', async () => {
  const store = await read('../src/store.ts');

  // URL 深链优先，其次才是 window.name 恢复状态；Media 的兜底是趋势雷达，
  // 不再是 Alpha Agent 对话页。直接访问 /agent 仍然落在对话舞台。
  assert.match(store, /activeTab: \(!SHOP_MODE_ENABLED && initialRoute\?\.mode === 'shop'\)/);
  assert.match(store, /state\.productMode === 'shop' \? 'shop' : 'dashboard'/);
  assert.match(store, /: leavingBot \? 'dashboard'/);
  const routes = await read('../src/lib/routes.ts');
  // /agent 不再是独立入口，直接落到 /bot。
  assert.match(routes, /tab === 'agent'\) return '\/bot'/);
  assert.match(routes, /path === '\/agent'\) return \{ tab: 'bot' \}/);
});

test('agent stage stays mounted while workspaces switch around it', async () => {
  const app = await read('../src/App.tsx');
  const layout = await read('../src/components/Layout.tsx');
  const drawer = await read('../src/components/AgentDrawer.tsx');

  // Exactly one persistent agent surface exists: the stage host in App.
  assert.equal(app.match(/<AgentDrawer /g)?.length, 1);
  assert.match(app, /agentStage=\{<AgentDrawer displayMode="stage" \/>\}/);
  assert.match(app, /case 'agent':[\s\S]{0,200}case 'bot':\s*return null;/);
  assert.doesNotMatch(layout, /<AgentDrawer/);

  // Switching workspaces hides the stage, it never unmounts mid-run.
  assert.match(layout, /data-testid="agent-stage-host"/);
  assert.match(layout, /\{agentStage\}/);
  assert.doesNotMatch(app, /key=\{activeTab\}[^>]*agentStage/s);
});

test('stage empty state guides users to verify a model before chatting', async () => {
  const drawer = await read('../src/components/AgentDrawer.tsx');

  assert.match(drawer, /data-testid="agent-model-validation-hint"/);
  assert.match(drawer, /data-testid="agent-go-validate-model"/);
  assert.match(drawer, /alpha-nexus-egolite:open-settings/);
  assert.match(drawer, /model_candidates/);
});

test('stage reads as a centered canvas with live-follow streaming', async () => {
  const drawer = await read('../src/components/AgentDrawer.tsx');

  // Bot 对话使用完整舞台宽度，不再把消息列半宽居中；OMB 保留原生行排版。
  assert.match(drawer, /const isStage = displayMode === 'stage'/);
  assert.match(drawer, /isStage \? 'w-full' : ''/);
  assert.doesNotMatch(drawer, /isStage \? 'mx-auto max-w-3xl' : ''/);
  assert.match(drawer, /<ChatTranscript/);
  assert.match(drawer, /contentClassName="flex w-full flex-col gap-4"/);
  assert.match(drawer, /size=\{40\}/);
  // Streaming deltas stay plain text in the OMB bubble; markdown is reserved for the final message.
  assert.doesNotMatch(drawer, /<AssistantMarkdown content=\{streamingText\} \/>/);
  assert.match(drawer, /text: streamingText/);
  assert.match(drawer, /plain: true/);
  const bubble = await read('../src/vendor/openmausbot/components/Bubble.tsx');
  assert.match(bubble, /message\.plain/);
  assert.match(bubble, /whitespace-pre-wrap/);
  // Auto-follow keeps the newest content visible without yanking history reads.
  assert.match(drawer, /messagesScrollRef/);
  assert.match(drawer, /nearBottom/);
  // The shortcut navigates to the stage; the drawer-mode toggle stays for docked use.
  assert.match(drawer, /isStage \? useUIStore\.getState\(\)\.setActiveTab\('agent'\) : useUIStore\.getState\(\)\.toggleAgentDrawer\(\)/);
  // Blocked-provider CTA must not mutate drawer-open state while on stage.
  assert.match(drawer, /if \(displayMode === 'drawer'\) closeAgentDrawer\(\)/);
});

test('composer and conversation state survive the sharp edges', async () => {
  const drawer = await read('../src/components/AgentDrawer.tsx');

  // A blocked submit must not erase the typed message: the composer clears
  // only after the guards pass inside submitMessage.
  assert.doesNotMatch(drawer, /const send = async \(\) => \{\s*const message = input\.trim\(\);\s*if \(!message \|\| runBusy\) return;\s*setInput\(''\);/);
  assert.match(drawer, /setLastSubmittedMessage\(message\);[\s\S]*?setInput\(''\);/);

  // Opening a conversation lands on the latest message, not the top of history.
  assert.match(drawer, /\[conversation\?\.id\]\);/);

  // Switching product mode mid-run must not clobber the running conversation.
  assert.match(drawer, /if \(!agentVisible \|\| runBusy\) return;/);
  // A slow bootstrap cannot overwrite a Bot selected meanwhile, and a restored
  // conversation keeps its own provider/model instead of the global selection.
  assert.match(drawer, /conversationSelectionRevisionRef/);
  assert.match(drawer, /conversationSelectionRevisionRef\.current === bootstrapConversationSelectionRevision/);
  assert.doesNotMatch(drawer, /setModelID\(bootstrap\.selection\.model_id \?\? (selected|loaded)\.model_id/);
});

test('model selector shows provider logo plus model name and switches in place', async () => {
  const drawer = await read('../src/components/AgentDrawer.tsx');

  // Custom listbox replaces the native select: logo + model name per option.
  assert.match(drawer, /data-testid="agent-model-select"/);
  assert.match(drawer, /role="listbox"/);
  assert.match(drawer, /providerDisplay\(group\.id\)/);
  // Picking a model from another provider switches the provider first.
  assert.match(drawer, /const pickModel = async/);
  // The switcher lists only real-call verified models; candidates stay in
  // Settings and never leak into the composer menu.
  assert.doesNotMatch(drawer, /待验证/);
  // The stage identity stays the Alpha bot mark; provider logos are selector-only.
  // 头部头像跟随当前 Bot，而不是固定像素标
  assert.match(drawer, /AgentIdentityAvatar bot=\{currentBot\} size=\{40\}/);
  // Both verified sources are one-click prefillable in settings.
  const settings = await read('../src/components/SettingsView.tsx');
  assert.match(settings, /api\.deepseek\.com/);
  assert.match(settings, /127\.0\.0\.1:8045/);
});

test('computer use is not exposed in the v1 activity contract', async () => {
  const context = await read('../src/components/AgentContextDrawer.tsx');
  assert.doesNotMatch(context, /'computer_use'/);
  assert.doesNotMatch(context, /Responses API|computer-use-preview/);
});
