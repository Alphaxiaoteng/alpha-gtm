import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');

const panelPath = '../src/components/BotSettingsPanel.tsx';
const apiPath = '../src/api.ts';
const agentSectionPath = '../src/components/AlphaAgentSettingsSection.tsx';

test('连接页是三张卡，渠道卡是明确的未启用占位', async () => {
  const panel = await read(panelPath);

  assert.match(panel, /data-testid="settings-connect"/);
  assert.match(panel, /data-testid="connect-card-bots"/);
  assert.match(panel, /data-testid="connect-card-provider"/);
  assert.match(panel, /data-testid="connect-card-channel"/);

  // 三张卡各自的「一句价值说明」
  assert.match(panel, /给助手一个身份，它会记住自己是谁。/);
  assert.match(panel, /换一个模型，不用换整个工具。/);
  assert.match(panel, /微信 · 即将支持/);

  // 渠道卡只能是灰态占位：没有假开关、没有假成功文案
  assert.match(panel, /data-testid="connect-channel-placeholder"/);
  assert.match(panel, /下一轮启用/);
  assert.doesNotMatch(panel, /已连接/);
  // 渠道卡区块内不得出现任何开关；Bot 表单自己的置顶/主控 checkbox 不算渠道卡
  const channelBlock = panel.slice(
    panel.indexOf('data-testid="connect-channel-placeholder"'),
    panel.indexOf('本轮只登记身份和模型'),
  );
  assert.ok(channelBlock.length > 0, 'channel placeholder block should exist');
  assert.doesNotMatch(channelBlock, /type="checkbox"|type="radio"/);
  assert.doesNotMatch(panel, /role="switch"/);
  assert.doesNotMatch(panel, /connect-channel-(toggle|connect|switch)/);
  assert.doesNotMatch(panel, /toggleChannel|connectChannel/);
});

test('Bot 卡覆盖空状态、内联表单、行内编辑与二次确认删除', async () => {
  const panel = await read(panelPath);

  assert.match(panel, /还没有 Bot，建一个试试/);
  assert.match(panel, /data-testid="connect-bot-empty"/);
  assert.match(panel, /data-testid="connect-bot-create"/);
  assert.match(panel, /data-testid="connect-bot-form"/);
  assert.match(panel, /data-testid="connect-bot-form-name"/);
  assert.match(panel, /data-testid="connect-bot-form-instructions"/);
  assert.match(panel, /data-testid="connect-bot-form-model"/);

  // 行内编辑/删除 + window.confirm 二次确认
  assert.match(panel, /data-testid="connect-bot-edit"/);
  assert.match(panel, /data-testid="connect-bot-delete"/);
  assert.match(panel, /if \(!confirmed\) return;/);
  assert.match(panel, /window\.confirm\(/);

  // 本轮不暴露工具能力
  assert.doesNotMatch(panel, /capabilities:\s*\[/);
  assert.doesNotMatch(panel, /connect-bot-form-capabilities/);

  // PersonAgent 现在是 EgoSettingsView 的独立子页,不再塞在 Alpha Agent 分区里
  const egoSettings = await read(new URL('../src/components/EgoSettingsView.tsx', import.meta.url));
  assert.match(egoSettings, /import \{ BotSettingsPanel \} from '\.\/BotSettingsPanel';/);
  assert.match(egoSettings, /'personagent'/);
  assert.match(egoSettings, /<BotSettingsPanel \/>/);
  const agentSection = await read(agentSectionPath);
  assert.doesNotMatch(agentSection, /<BotSettingsPanel \/>/);
  assert.match(agentSection, /data-testid="alpha-agent-settings"/);
});

test('生成头像极简：只有提示词 + 可选参考图，成功后写入 avatar', async () => {
  const panel = await read(panelPath);

  assert.match(panel, /function buildAvatarPrompt\(name: string, role: string\)/);
  assert.match(panel, /Cute chibi anime avatar for an AI assistant named \$\{name\}/);
  assert.match(panel, /setAvatarPrompt\(buildAvatarPrompt\(draft\.name\.trim\(\), roleOf\(draft\.instructions\)\)\)/);
  assert.match(panel, /data-testid="connect-bot-avatar-prompt"/);

  // 参考图复用已有上传端点，只把 name 当 reference_image
  assert.match(panel, /data-testid="connect-bot-avatar-reference"/);
  assert.match(panel, /await api\.uploadAIImageAsset\(file\)/);
  assert.match(panel, /reference_image: reference\.name/);

  assert.match(panel, /await api\.generateAIImage\(\{/);
  assert.match(panel, /avatar: result\.url/);
  // 生成中禁用 + Loading，失败不留半截状态
  assert.match(panel, /disabled=\{generatingAvatar \|\| uploadingReference\}/);
  assert.match(panel, /生成中…/);
  assert.match(panel, /头像生成失败/);
  assert.match(panel, /没有返回图片地址/);
});

test('Bot 列表把内置头像内嵌渲染成 SVG，而不是请求图片', async () => {
  const rail = await read(new URL('../src/components/BotRail.tsx', import.meta.url));
  const avatars = await read(new URL('../src/components/PixelAvatars.tsx', import.meta.url));
  assert.match(rail, /pixelAvatarRole\(bot\.avatar\)/);
  assert.match(rail, /<PixelAvatar role=\{role\}/);
  // Embedded: the avatar renders <rect> primitives, never an <img> for presets.
  assert.match(avatars, /shapeRendering="crispEdges"/);
  assert.match(avatars, /<rect key=/);
  assert.doesNotMatch(avatars, /<img[^>]*bot-avatars/);
});

test('内置像素头像：8 个职责，内嵌渲染，点选即写入 avatar', async () => {
  const panel = await read(panelPath);
  const avatars = await read(new URL('../src/components/PixelAvatars.tsx', import.meta.url));

  assert.match(panel, /const BOT_AVATAR_PRESETS = PIXEL_AVATAR_ROLES\.map/);
  assert.match(panel, /data-testid="connect-bot-avatar-presets"/);
  assert.match(panel, /data-testid=\{`connect-bot-avatar-preset-\$\{preset\.id\}`\}/);
  assert.match(panel, /<PixelAvatar role=\{preset\.id\}/);
  assert.match(panel, /setDraft\(\(current\) => \(\{ \.\.\.current, avatar: preset\.src \}\)\)/);

  // Eight responsibilities, four female and four male.
  const female = (avatars.match(/gender: 'female', bg:/g) || []).length;
  const male = (avatars.match(/gender: 'male', bg:/g) || []).length;
  assert.equal(female, 4, 'expected four female avatars');
  assert.equal(male, 4, 'expected four male avatars');
});

test('生图不暴露任何参数控件：没有 model / 宽高比 / 尺寸 / 分辨率', async () => {
  const panel = await read(panelPath);

  assert.doesNotMatch(panel, /aspect_ratio|image_size|image_size\b/i);
  assert.doesNotMatch(panel, /宽高比|分辨率|尺寸/);
  // 模型选择器是允许的（Bot 绑定已验收模型），但生图块内部不能有任何参数控件。
  const avatarBlock = panel.match(/data-testid="connect-bot-avatar-block"([\s\S]*?)data-testid="connect-bot-form-save"/)?.[1] || '';
  assert.doesNotMatch(avatarBlock, /<select/);
  assert.doesNotMatch(panel, /<datalist/);
  assert.doesNotMatch(panel, /advanced/i);
  // 生图块里只有提示词与参考图两个输入
  const block = panel.slice(panel.indexOf('data-testid="connect-bot-avatar-block"'), panel.indexOf('data-testid="connect-bot-form-save"'));
  assert.match(block, /connect-bot-avatar-prompt/);
  assert.match(block, /connect-bot-avatar-reference/);
  assert.equal((block.match(/<input/g) || []).length, 1); // 只有隐藏的 file input
  assert.equal((block.match(/<textarea/g) || []).length, 1); // 只有提示词
});

test('API Key 只走 credential 端点，不进 store/localStorage，输入框是 password', async () => {
  const panel = await read(panelPath);
  const api = await read(apiPath);

  assert.match(panel, /type="password"/);
  assert.match(panel, /data-testid="connect-provider-key"/);
  assert.match(panel, /await api\.saveAgnesAPIKey\(key\)/);
  assert.match(api, /saveAgnesAPIKey: \(apiKey: string\)/);
  assert.match(api, /'\/api\/ai-image\/credential'/);

  // 提交后清空 + 不落任何前端持久化
  assert.match(panel, /setAPIKey\(''\);/);
  assert.doesNotMatch(panel, /localStorage|sessionStorage|document\.cookie/);
  assert.doesNotMatch(panel, /useUIStore\.setState|useUIStore\(\(state\) => state\.apiKey/);
  // 源码里不得出现硬编码的密钥
  assert.doesNotMatch(panel, /sk-[A-Za-z0-9]/);
});

test('Bot CRUD 与生图 API 契约都在 api.ts 里，生图只发 prompt / reference_image', async () => {
  const api = await read(apiPath);

  assert.match(api, /export interface AgentBot \{/);
  assert.match(api, /export type AgentBotInput =/);
  assert.match(api, /export interface AIImageGenerateResult \{/);
  assert.match(api, /export interface AIImageProviderConfigInput \{/);

  for (const name of ['listAgentBots', 'createAgentBot', 'updateAgentBot', 'deleteAgentBot', 'generateAIImage']) {
    assert.match(api, new RegExp(`${name}:`));
  }
  assert.match(api, /'\/api\/agent-bots'/);
  assert.match(api, /`\/api\/agent-bots\/\$\{encodeURIComponent\(id\)\}`/);
  assert.match(api, /uploadAIImageAsset: \(file: File\)/);

  // 生成接口请求体收敛：只有 prompt + 可选 reference_image
  assert.match(api, /generateAIImage: \(body: \{ prompt: string; reference_image\?: string \}\) =>/);
  const start = api.indexOf('generateAIImage:');
  const generateBlock = api.slice(start, start + 240);
  assert.doesNotMatch(generateBlock, /aspect_ratio|image_size|model\?|model:/);
});

test('供应商卡只留 base_url + API Key + 验证', async () => {
  const panel = await read(panelPath);

  assert.match(panel, /data-testid="connect-provider-base-url"/);
  assert.match(panel, /https:\/\/api\.zexapi\.com/);
  assert.match(panel, /data-testid="connect-provider-key"/);
  assert.match(panel, /data-testid="connect-provider-verify"/);
  assert.match(panel, /data-testid="connect-provider-message"/);

  // 去掉模型输入、datalist 与高级折叠
  assert.doesNotMatch(panel, /connect-provider-model/);
  assert.doesNotMatch(panel, /MODEL_SUGGESTIONS|datalist|advancedOpen/);

  // 验证成功才亮绿点；失败红点 + 人话错误
  assert.match(panel, /verifyState === 'ok' \? 'success' : verifyState === 'error' \? 'danger' : 'muted'/);
  assert.match(panel, /模型与出图参数由服务端自动选择。/);
});

test('供应商卡 1:1 展示 DSH 目录，只读不写', async () => {
  const panel = await read(panelPath);
  assert.match(panel, /data-testid="connect-dsh-sync"/);
  assert.match(panel, /api\.getDshCatalog/);
  assert.match(panel, /1:1 已同步/);
  // 只读：面板不得向 DSH 目录写任何东西
  assert.doesNotMatch(panel, /writeFile|fs\.write|renameSync/);
});

test('Agent 运行时候选模型可筛选，grok 这类末尾候选可见', async () => {
  const section = await read(new URL('../src/components/AlphaAgentSettingsSection.tsx', import.meta.url));
  assert.match(section, /data-testid="alpha-agent-candidate-filter"/);
  assert.match(section, /candidateQuery/);
  assert.match(section, /filter\(\(model\) => !query \|\| model\.toLowerCase\(\)\.includes\(query\)\)/);
  // 已验证的模型不再重复出现在候选里
  assert.match(section, /filter\(\(model\) => !models\.includes\(model\)\)/);
});

test('运行错误码有明确中文提示，不只回落通用文案', async () => {
  const drawer = await read(new URL('../src/components/AgentDrawer.tsx', import.meta.url));
  assert.match(drawer, /AGENT_RUN_ACTIVE: '这个对话正在运行/);
  assert.match(drawer, /AGENT_RUN_CAPACITY: '同时进行的对话有点多/);
  assert.match(drawer, /AGENT_MODEL_NOT_VERIFIED:/);
  assert.match(drawer, /AGENT_PROVIDER_DSH_TURN:/);
});

test('Bot 表单用已验证模型下拉，而不是自由文本', async () => {
  const panel = await read(panelPath);
  assert.match(panel, /api\.agentHubProviders/);
  assert.match(panel, /data-testid="connect-bot-form-model"/);
  assert.match(panel, /<option value="">跟随当前 Agent 模型<\/option>/);
  assert.match(panel, /connect-bot-model-unverified/);
  // 只从 probe.models（已验证）取，不取 candidates
  assert.match(panel, /for \(const model of probe\.models \?\? \[\]\)/);
});

test('Bot 列表支持按名字和角色搜索', async () => {
  const rail = await read(new URL('../src/components/BotRail.tsx', import.meta.url));
  assert.match(rail, /data-testid="bot-rail-search"/);
  assert.match(rail, /const visibleBots = normalizedQuery/);
  assert.match(rail, /\$\{bot\.name\} \$\{bot\.role \?\? ''\}/);
  assert.match(rail, /data-testid="bot-rail-no-match"/);
});

test('Bot 列表每项两行：最后一条消息预览 + 未读/免打扰/主 Bot 标记', async () => {
  const rail = await read(new URL('../src/components/BotRail.tsx', import.meta.url));
  // 第二行是灰色单行预览，没有消息时回落到身份，不会留空
  assert.match(rail, /const preview = summaryByBotID\.get\(bot\.id\)\?\.last_message \|\| bot\.role \|\| '还没有消息'/);
  assert.match(rail, /data-testid=\{`bot-rail-unread-\$\{bot\.id\}`\}/);
  // 铃铛常驻右侧，可直接切换免打扰（参考图每行右侧都有铃铛）
  assert.match(rail, /data-testid=\{bot\.muted \? `bot-rail-muted-\$\{bot\.id\}` : `bot-rail-unmuted-\$\{bot\.id\}`\}/);
  assert.match(rail, /async function toggleMute\(bot: AgentBot\)/);
  assert.match(rail, /data-testid=\{`bot-rail-primary-\$\{bot\.id\}`\}/);
});

test('Bot 列表按 group_name 分组，组头带数量，未分组归到「其他」', async () => {
  const rail = await read(new URL('../src/components/BotRail.tsx', import.meta.url));
  assert.match(rail, /const UNGROUPED_LABEL = '其他'/);
  assert.match(rail, /function groupNameOf\(bot: \{ group_name\?: string \| null \}\): string/);
  assert.match(rail, /function botGroupOrder\(/);
  // 主 Agent 不是 Bot，不占用列表槽位
  assert.match(rail, /const listBots = visibleBots;/);
  assert.match(rail, /data-testid="bot-rail-chief"/);
  assert.match(rail, /<AlphaPixelMark/);
  // 全部 Bot 按 group_name 平铺：不再切独立的「置顶」分区，置顶只影响组内排序。
  assert.match(rail, /const sections = buildBotSections\(visibleBots\);/);
  assert.doesNotMatch(rail, /group: '置顶'/);
  assert.match(rail, /data-testid=\{`bot-rail-group-\$\{section\.group\}`\}/);
  // 只有一个部门时连部门标题都不画，卡片直接平铺。
  assert.match(rail, /const showGroupHeader = sections\.length > 1;/);
  assert.match(rail, /compact \|\| !showGroupHeader \? null :/);
  assert.match(rail, /\{section\.bots\.length\}/);
  // 组内顺序沿用后端契约：主 Bot → 置顶 → 新建在前
  assert.match(rail, /if \(Boolean\(left\.is_primary\) !== Boolean\(right\.is_primary\)\)/);
  assert.match(rail, /if \(Boolean\(left\.pinned\) !== Boolean\(right\.pinned\)\)/);
});

test('收件箱是独立未读聚合视图：不按未读过滤团队，点条目打开会话并写已读', async () => {
  const rail = await read(new URL('../src/components/BotRail.tsx', import.meta.url));
  const inboxLib = await read(new URL('../src/lib/botInbox.ts', import.meta.url));
  assert.match(rail, /data-testid="bot-rail-inbox"/);
  assert.match(rail, /function renderInboxBadge\(count: number, testID = 'bot-rail-inbox-count'\)/);
  assert.match(rail, /\{renderInboxBadge\(unreadTotal\)\}/);
  // 收件箱只切换右栏面板（跨组件状态），不再过滤团队列表
  assert.match(rail, /onClick=\{\(\) => setBotInboxOpen\(!botInboxOpen\)\}/);
  assert.doesNotMatch(rail, /inboxOnly/);
  assert.doesNotMatch(rail, /orderedBots\.filter\(\(bot\) => unreadOf\(bot\.id\) > 0\)/);
  // 聚合视图来自一个请求，不是让前端自己拼
  assert.match(rail, /useSWR\('api\/agent-bots\/rail', api\.agentBotRail/);
  // 打开流程抽到 lib/botInbox 复用：写已读 + 刷新 rail 都在那里。
  assert.match(rail, /await openBotThread\(bot, threadDeps\);/);
  assert.match(inboxLib, /await deps\.markRead\(conversationID\);/);
  assert.match(inboxLib, /await deps\.refreshRail\(\);/);
});

test('Bot 错误码有中文提示', async () => {
  const panel = await read(panelPath);
  assert.match(panel, /const BOT_ERROR_MESSAGES/);
  assert.match(panel, /AGENT_BOT_NAME_REQUIRED: 'Bot 名字不能为空。/);
  assert.match(panel, /AGENT_BOT_NOT_FOUND: '这个 Bot 已被删除。/);
});

test('Bot 对话时 Drawer 显示当前 Bot 身份', async () => {
  const drawer = await read(new URL('../src/components/AgentDrawer.tsx', import.meta.url));
  assert.match(drawer, /const currentBot =\s*\n?\s*\(botRoster \?\? \[\]\)\.find/);
  // 还没有会话时也要跟随 Bot 列表里选中的那个
  assert.match(drawer, /find\(\(item\) => item\.id === activeBotID\)/);
  assert.match(drawer, /AgentIdentityAvatar bot=\{currentBot\}/);
});

test('Bot 表单支持开场语（kickoff）字段', async () => {
  const panel = await read(panelPath);
  assert.match(panel, /kickoff: string;/);
  assert.match(panel, /data-testid="connect-bot-form-kickoff"/);
  assert.match(panel, /kickoff: draft\.kickoff\.trim\(\)/);
  assert.match(panel, /kickoff: bot\.kickoff \|\| ''/);
});

test('Bot 表单支持团队/分组与免打扰', async () => {
  const panel = await read(panelPath);
  assert.match(panel, /groupName: string;/);
  assert.match(panel, /data-testid="connect-bot-form-group"/);
  assert.match(panel, /group_name: draft\.groupName\.trim\(\)/);
  assert.match(panel, /groupName: bot\.group_name \|\| ''/);

  assert.match(panel, /muted: boolean;/);
  assert.match(panel, /data-testid="connect-bot-form-muted"/);
  assert.match(panel, /muted: draft\.muted/);
  assert.match(panel, /muted: Boolean\(bot\.muted\)/);
  // 免打扰只是展示标记，不能变成渠道开关
  assert.doesNotMatch(panel, /muted[^\n]*localStorage/);
});

test('会话可删除（API 层保留）', async () => {
  const apiSrc = await read(new URL('../src/api.ts', import.meta.url));
  assert.match(apiSrc, /deleteAgentHubConversation/);
});

test('平台类 Bot 使用平台 logo，与像素职责头像并存', async () => {
  const avatars = await read(new URL('../src/components/PixelAvatars.tsx', import.meta.url));
  const panel = await read(panelPath);
  assert.match(avatars, /export const PLATFORM_AVATARS/);
  const platforms = avatars.match(/src: '\/platforms\/[a-z0-9_]+\.png'/g) || [];
  assert.equal(platforms.length, 12, 'expected 12 platform logos');
  assert.match(avatars, /export function platformAvatarID/);
  assert.match(panel, /data-testid=\{`connect-bot-avatar-platform-\$\{preset\.id\}`\}/);
  assert.match(panel, /平台频道/);
  assert.match(panel, /按职责选形象/);
});
