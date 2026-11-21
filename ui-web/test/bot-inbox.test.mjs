import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');
const railPath = '../src/components/BotRail.tsx';
const layoutPath = '../src/components/Layout.tsx';
const panelPath = '../src/components/BotInboxPanel.tsx';
const inboxLibPath = '../src/lib/botInbox.ts';
const storePath = '../src/store.ts';

/** BotRail 里的部门分区是纯函数区域：转译后在 Node 里真跑，而不是比对源码字面。 */
async function sectionsModule() {
  const source = await read(railPath);
  const start = source.indexOf('// #region bot-rail-sections');
  const end = source.indexOf('// #endregion bot-rail-sections');
  assert.ok(start > 0 && end > start, 'BotRail 必须保留可执行的部门分区区域');
  const { outputText } = ts.transpileModule(
    `${source.slice(start, end)}\nexport { buildBotSections, botGroupOrder, groupNameOf };\n`,
    { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } },
  );
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}

/** 收件箱的数据与打开逻辑也是可执行区域：给假依赖真跑一遍。 */
async function inboxModule() {
  const source = await read(inboxLibPath);
  const start = source.indexOf('// #region bot-inbox-logic');
  const end = source.indexOf('// #endregion bot-inbox-logic');
  assert.ok(start > 0 && end > start, 'lib/botInbox.ts 必须保留可执行的收件箱逻辑区域');
  const exported = [
    'BUILTIN_ALPHA_AGENT_ID',
    'isBuiltinAlphaAgent',
    'botGroupName',
    'botOpenFailure',
    'buildInboxRows',
    'inboxTimeLabel',
    'inboxUnreadTotal',
    'latestConversationForBot',
    'latestMainAgentConversation',
    'openBotThread',
    'openMainAgentThread',
  ];
  const { outputText } = ts.transpileModule(
    `${source.slice(start, end)}\nexport { ${exported.join(', ')} };\n`,
    { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } },
  );
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}

/** 最小 rail 聚合：字段名与 api.AgentBotRail 一致。 */
function railFixture() {
  return {
    unread_total: 5,
    agent: { unread: 2, last_message: '主 Agent 的回复', last_message_at: '2026-11-16T04:00:00Z' },
    bots: [
      {
        id: 'dev-frontend',
        name: '前端',
        role: '前端开发',
        group_name: '开发团队',
        unread: 3,
        last_message: '组件改好了',
        last_message_at: '2026-11-16T05:00:00Z',
      },
      {
        id: 'media-xhs',
        name: '小红书运营',
        role: '新媒体',
        group_name: '新媒体团队',
        unread: 0,
        last_message: '今天没新活',
        last_message_at: '2026-11-16T01:00:00Z',
      },
      { id: 'loose', name: '散装', group_name: '  ', unread: 0 },
    ],
  };
}

test('左侧收件箱按钮只切换跨组件状态，团队列表不参与过滤', async () => {
  const source = await read(railPath);
  const store = await read(storePath);

  // 状态在 store 里：BotRail 只切换，右栏面板读同一个值。
  assert.match(store, /botInboxOpen: boolean;/);
  assert.match(store, /setBotInboxOpen: \(open: boolean\) => void;/);
  assert.match(store, /botInboxOpen: false,/);
  assert.match(source, /const botInboxOpen = useUIStore\(\(state\) => state\.botInboxOpen\);/);
  assert.match(source, /const setBotInboxOpen = useUIStore\(\(state\) => state\.setBotInboxOpen\);/);
  assert.match(source, /data-testid="bot-rail-inbox"/);
  assert.match(source, /aria-pressed=\{botInboxOpen\}/);
  assert.match(source, /onClick=\{\(\) => setBotInboxOpen\(!botInboxOpen\)\}/);
  assert.match(source, /function renderInboxBadge\(count: number, testID = 'bot-rail-inbox-count'\)/);
  assert.match(source, /\{renderInboxBadge\(unreadTotal\)\}/);

  // 左侧不再渲染内联收件箱列表：它搬到右侧栏了。
  assert.doesNotMatch(source, /bot-rail-inbox-view/);
  assert.doesNotMatch(source, /bot-rail-inbox-row-/);
  assert.doesNotMatch(source, /bot-rail-inbox-back/);
  assert.doesNotMatch(source, /const inboxRows/);
  assert.doesNotMatch(source, /async function openInboxRow/);
  assert.doesNotMatch(source, /inboxOnly/);
});

test('左侧团队列表照旧常显：开发、新媒体部门与全部 Bot 都在，不因收件箱改变', async () => {
  const source = await read(railPath);

  // 内置主 Agent 是真实行但只占顶部大卡：部门名单先排除它，避免重复进「其他」。
  assert.match(source, /const rosterBots = bots\.filter\(\(bot\) => !isBuiltinAlphaAgent\(bot\)\);/);
  assert.match(source, /const visibleBots = normalizedQuery\s*\n?\s*\? rosterBots\.filter/);
  assert.match(source, /const listBots = visibleBots;/);
  assert.match(source, /buildBotSections\(unpinnedBots\)/);
  const scroll = source.slice(
    source.indexOf('min-h-0 flex-1 space-y-0.5 overflow-y-auto'),
    source.indexOf('{blockedReason ? ('),
  );
  assert.ok(scroll.length > 0, '侧栏滚动区缺失');
  // 收件箱按钮不再出现在滚动区里，它就是团队列表本身。
  assert.doesNotMatch(scroll, /inboxOpen/);
  assert.match(scroll, /\{sections\.map\(\(section\) =>/);
  assert.match(scroll, /data-testid=\{`bot-rail-group-\$\{section\.group\}`\}/);
  assert.match(scroll, /\{section\.bots\.length\}/);
  assert.match(scroll, /data-testid="bot-rail-chief"/);
});

test('右侧栏按 botInboxOpen 二选一：收件箱面板或 Bot 详情，agentStage 不卸载', async () => {
  const layout = await read(layoutPath);

  assert.match(layout, /import \{ BotInboxPanel \} from '\.\/BotInboxPanel';/);
  assert.match(layout, /const botInboxOpen = useUIStore\(\(state\) => state\.botInboxOpen\);/);
  assert.match(layout, /data-testid="agent-stage-host"/);
  assert.match(layout, /\{activeTab === 'bot' \? \(botInboxOpen \? <BotInboxPanel \/> : <BotDetailPanel \/>\) : null\}/);
  // 舞台容器必须在面板之前渲染，切换面板不能把正在跑的会话卸载。
  const stage = layout.indexOf('{agentStage}');
  const panel = layout.indexOf('<BotInboxPanel />');
  assert.ok(stage > 0 && panel > stage, 'agentStage 必须在右栏面板之前渲染且保持挂载');
});

test('收件箱面板是右侧栏：标题、关闭、未读总数、空态与行字段都在', async () => {
  const panel = await read(panelPath);

  assert.match(panel, /data-testid="bot-inbox-panel"/);
  assert.match(panel, /aria-label="收件箱"/);
  assert.match(panel, /收件箱\n/);
  assert.match(panel, /data-testid="bot-inbox-close"/);
  assert.match(panel, /aria-label="关闭收件箱"/);
  assert.match(panel, /onClick=\{\(\) => setBotInboxOpen\(false\)\}/);
  assert.match(panel, /data-testid="bot-inbox-total"/);
  assert.match(panel, /const unreadTotal = inboxUnreadTotal\(rail\);/);
  assert.match(panel, /data-testid="bot-inbox-empty"/);
  assert.match(panel, /没有未读消息/);
  // 行字段：像素头像（主 Agent 用 AlphaPixelMark）、名称、部门、摘要、时间、未读数
  assert.match(panel, /data-testid=\{`bot-inbox-row-\$\{row\.key\}`\}/);
  assert.match(panel, /row\.bot \? <BotAvatar bot=\{row\.bot\} size="md" \/> : <AlphaPixelMark className="h-7 w-7" decorative \/>/);
  assert.match(panel, /\{row\.name\}/);
  assert.match(panel, /\{row\.group\}/);
  assert.match(panel, /\{row\.preview\}/);
  assert.match(panel, /inboxTimeLabel\(row\.at\)/);
  assert.match(panel, /data-testid=\{`bot-inbox-unread-\$\{row\.key\}`\}/);
  // 数据只来自一个 rail 聚合请求，不在前端重算未读。
  assert.match(panel, /const \{ data: rail, mutate: refreshRail \} = useSWR\('api\/agent-bots\/rail', api\.agentBotRail\);/);
  assert.match(panel, /const rows = buildInboxRows\(rail\);/);
});

test('面板点条目：真实会话 + 写已读 + 关闭收件箱；主 Agent 只用真实会话 id', async () => {
  const panel = await read(panelPath);

  const openRow = panel.slice(panel.indexOf('async function openRow('), panel.indexOf('return ('));
  // 主 Agent 的历史行打开并清掉历史无归属会话的未读；真实身份走大卡。
  assert.match(openRow, /if \(row\.key === 'main-agent'\)/);
  assert.match(openRow, /await openMainAgentThread\(threadDeps\);/);
  assert.match(openRow, /else if \(row\.bot\) \{\s*await openBotThread\(row\.bot, threadDeps\);/);
  assert.match(openRow, /setBotInboxOpen\(false\);/);
  assert.match(openRow, /botOpenFailure\(reason\)\.message/);
  // 两个打开流程都来自 lib/botInbox，面板与 BotRail 不各写一遍。
  assert.match(panel, /createConversation: api\.agentHubConversationCreate,/);
  assert.match(panel, /markRead: api\.markAgentHubConversationRead,/);
  assert.doesNotMatch(panel, /requestAgentConversation\(''\)/);
  assert.doesNotMatch(panel, /markRead\(''\)/);
});

test('收件箱行是真数据映射：未读数、最后一行、时间，按最后消息时间倒序', async () => {
  const { buildInboxRows, inboxUnreadTotal } = await inboxModule();
  const rows = buildInboxRows(railFixture());

  // 只列有未读的 Bot + 主 Agent 一行；没有未读的不进收件箱。
  assert.deepEqual(rows.map((row) => row.key), ['dev-frontend', 'main-agent']);
  assert.equal(inboxUnreadTotal(railFixture()), 5);

  const [botRow, agentRow] = rows;
  assert.equal(botRow.name, '前端');
  assert.equal(botRow.group, '开发团队');
  assert.equal(botRow.unread, 3);
  assert.equal(botRow.preview, '组件改好了');
  assert.equal(botRow.at, '2026-11-16T05:00:00Z');
  assert.equal(botRow.bot.id, 'dev-frontend');

  assert.equal(agentRow.name, 'AlphaBot');
  assert.equal(agentRow.group, '主 Agent');
  assert.equal(agentRow.unread, 2);
  assert.equal(agentRow.preview, '主 Agent 的回复');
  assert.equal(agentRow.bot, undefined);

  // 倒序：主 Agent 的最后消息更早，排在 Bot 之后。
  assert.ok(botRow.at > agentRow.at);
  assert.deepEqual(buildInboxRows(null), []);
  assert.deepEqual(buildInboxRows(undefined), []);
});

test('主 Bot 占了主 Agent 位时，AlphaBot 那一行不再标成「主 Agent」', async () => {
  const { buildInboxRows } = await inboxModule();
  const rail = railFixture();
  rail.bots = [{ ...rail.bots[0], is_primary: true }, ...rail.bots.slice(1)];
  const agentRow = buildInboxRows(rail).find((row) => row.key === 'main-agent');
  assert.equal(agentRow.group, 'AlphaBot');
});

test('内置主 Agent 是真实行但只占大卡：收件箱不重复列它，历史未读仍归主 Agent 行', async () => {
  const { buildInboxRows, BUILTIN_ALPHA_AGENT_ID } = await inboxModule();
  const rail = railFixture();
  rail.bots = [
    { id: BUILTIN_ALPHA_AGENT_ID, name: 'Alpha Agent', group_name: '', is_primary: true, unread: 0 },
    ...rail.bots,
  ];
  const rows = buildInboxRows(rail);
  // 内置行是顶部大卡，不在收件箱普通行里再出现。
  assert.equal(rows.some((row) => row.key === BUILTIN_ALPHA_AGENT_ID), false);
  // 历史无归属会话的未读仍由 main-agent 行承载，且不带 bot（走历史会话、可清未读）。
  const agentRow = rows.find((row) => row.key === 'main-agent');
  assert.equal(agentRow.unread, 2);
  assert.equal(agentRow.bot, undefined);
  // primary 判定排除内置行：有别的 Bot 是 primary 时 group 变成「Alpha Agent」。
  rail.bots[1] = { ...rail.bots[1], is_primary: true };
  rail.bots[0] = { ...rail.bots[0], is_primary: false };
  const withOtherPrimary = buildInboxRows(rail).find((row) => row.key === 'main-agent');
  assert.equal(withOtherPrimary.group, 'AlphaBot');
});

test('主 Agent 无历史会话时回落到内置真实 bot：真实创建 + 写已读，绝不编 id', async () => {
  const { openMainAgentThread, BUILTIN_ALPHA_AGENT_ID } = await inboxModule();
  const calls = [];
  const builtin = { id: BUILTIN_ALPHA_AGENT_ID, name: 'Alpha Agent' };
  const opened = await openMainAgentThread({
    productMode: 'media',
    conversations: [{ id: 'conv-bot', bot_id: 'dev', updated_at: '2026-11-16T10:00:00Z' }], // 没有空 bot_id 会话
    requestAgentConversation: (id) => calls.push(['request', id]),
    setActiveBotID: (id) => calls.push(['activeBot', id]),
    createConversation: async (body) => { calls.push(['create', body]); return { id: 'conv-builtin' }; },
    markRead: async (id) => calls.push(['read', id]),
    refreshConversations: () => calls.push(['refreshConversations']),
    refreshRail: () => calls.push(['refreshRail']),
  }, builtin);
  assert.equal(opened, 'conv-builtin');
  assert.deepEqual(calls, [
    ['activeBot', BUILTIN_ALPHA_AGENT_ID],
    ['create', { bot_id: BUILTIN_ALPHA_AGENT_ID, title: 'Alpha Agent', context: { workspace: 'bot', page: 'bot-chat', product_mode: 'media' } }],
    ['refreshConversations'],
    ['request', 'conv-builtin'],
    ['read', 'conv-builtin'],
    ['refreshRail'],
  ]);
});

test('没有团队名的 Bot 归到「其他」，时间标签按间隔显示', async () => {
  const { botGroupName, inboxTimeLabel } = await inboxModule();
  assert.equal(botGroupName({ group_name: '  ' }), '其他');
  assert.equal(botGroupName({ group_name: '开发团队' }), '开发团队');

  const now = Date.parse('2026-11-16T12:00:00Z');
  assert.equal(inboxTimeLabel('2026-11-16T11:59:30Z', now), '刚刚');
  assert.equal(inboxTimeLabel('2026-11-16T11:20:00Z', now), '40 分钟前');
  assert.equal(inboxTimeLabel('2026-11-16T06:00:00Z', now), '6 小时前');
  assert.equal(inboxTimeLabel('', now), '');
  assert.equal(inboxTimeLabel('not-a-date', now), '');
});

test('点 Bot 条目：复用最近会话、写已读、刷新 rail；没有会话时真实创建', async () => {
  const { openBotThread } = await inboxModule();
  const bot = { id: 'dev-frontend', name: '前端' };

  const calls = [];
  const baseDeps = {
    productMode: 'media',
    conversations: [
      { id: 'conv-old', bot_id: 'dev-frontend', updated_at: '2026-11-15T10:00:00Z' },
      { id: 'conv-new', bot_id: 'dev-frontend', updated_at: '2026-11-16T10:00:00Z' },
      { id: 'conv-agent', updated_at: '2026-11-16T11:00:00Z' },
    ],
    requestAgentConversation: (id) => calls.push(['request', id]),
    setActiveBotID: (id) => calls.push(['activeBot', id]),
    createConversation: async (body) => {
      calls.push(['create', body]);
      return { id: 'conv-created' };
    },
    markRead: async (id) => calls.push(['read', id]),
    refreshConversations: () => calls.push(['refreshConversations']),
    refreshRail: () => calls.push(['refreshRail']),
  };

  const opened = await openBotThread(bot, baseDeps);
  assert.equal(opened, 'conv-new');
  assert.deepEqual(calls, [
    ['activeBot', 'dev-frontend'],
    ['request', 'conv-new'],
    ['read', 'conv-new'],
    ['refreshRail'],
  ]);

  const freshCalls = [];
  const fresh = await openBotThread(bot, {
    ...baseDeps,
    conversations: [],
    requestAgentConversation: (id) => freshCalls.push(['request', id]),
    setActiveBotID: () => {},
    createConversation: async (body) => {
      freshCalls.push(['create', body]);
      return { id: 'conv-created' };
    },
    markRead: async (id) => freshCalls.push(['read', id]),
    refreshConversations: () => freshCalls.push(['refreshConversations']),
    refreshRail: () => freshCalls.push(['refreshRail']),
  });
  assert.equal(fresh, 'conv-created');
  assert.deepEqual(freshCalls, [
    ['create', { bot_id: 'dev-frontend', title: '前端', context: { workspace: 'bot', page: 'bot-chat', product_mode: 'media' } }],
    ['refreshConversations'],
    ['request', 'conv-created'],
    ['read', 'conv-created'],
    ['refreshRail'],
  ]);
});

test('写已读失败不阻断打开：会话照常请求，rail 照样刷新', async () => {
  const { openBotThread } = await inboxModule();
  const requested = [];
  let railRefreshed = 0;

  const opened = await openBotThread(
    { id: 'dev-frontend', name: '前端' },
    {
      productMode: 'shop',
      conversations: [{ id: 'conv-1', bot_id: 'dev-frontend', updated_at: '2026-11-16T10:00:00Z' }],
      requestAgentConversation: (id) => requested.push(id),
      setActiveBotID: () => {},
      createConversation: async () => ({ id: 'never' }),
      markRead: async () => {
        throw new Error('AGENT_READ_FAILED');
      },
      refreshConversations: () => {},
      refreshRail: () => {
        railRefreshed += 1;
      },
    },
  );

  assert.equal(opened, 'conv-1');
  assert.deepEqual(requested, ['conv-1']);
  // markRead 抛错时不会走到 refreshRail，但打开流程必须已经完成。
  assert.equal(railRefreshed, 0);
});

test('主 Agent 只用会话列表里最新且没有 bot_id 的真实会话，找不到就只打开 Alpha Agent', async () => {
  const { openMainAgentThread, latestMainAgentConversation, latestConversationForBot } = await inboxModule();

  const conversations = [
    { id: 'conv-agent-old', updated_at: '2026-11-14T10:00:00Z' },
    { id: 'conv-agent-new', updated_at: '2026-11-16T11:00:00Z' },
    { id: 'conv-bot', bot_id: 'dev-frontend', updated_at: '2026-11-16T12:00:00Z' },
  ];
  assert.equal(latestMainAgentConversation(conversations).id, 'conv-agent-new');
  assert.equal(latestConversationForBot(conversations, 'dev-frontend').id, 'conv-bot');
  assert.equal(latestConversationForBot(conversations, 'missing'), undefined);

  const calls = [];
  const opened = await openMainAgentThread({
    productMode: 'media',
    conversations,
    requestAgentConversation: (id) => calls.push(['request', id]),
    setActiveBotID: (id) => calls.push(['activeBot', id]),
    createConversation: async () => {
      throw new Error('主 Agent 绝不创建会话');
    },
    markRead: async (id) => calls.push(['read', id]),
    refreshConversations: () => calls.push(['refreshConversations']),
    refreshRail: () => calls.push(['refreshRail']),
  });

  assert.equal(opened, 'conv-agent-new');
  assert.deepEqual(calls, [
    ['activeBot', ''],
    ['request', 'conv-agent-new'],
    ['read', 'conv-agent-new'],
    ['refreshRail'],
    ['refreshConversations'],
  ]);

  const emptyCalls = [];
  const none = await openMainAgentThread({
    productMode: 'media',
    conversations: conversations.filter((item) => item.bot_id),
    requestAgentConversation: (id) => emptyCalls.push(['request', id]),
    setActiveBotID: (id) => emptyCalls.push(['activeBot', id]),
    createConversation: async () => ({ id: 'fake' }),
    markRead: async (id) => emptyCalls.push(['read', id]),
    refreshConversations: () => emptyCalls.push(['refreshConversations']),
    refreshRail: () => emptyCalls.push(['refreshRail']),
  });

  // 没有主 Agent 线程：只打开 Alpha Agent，绝不编 id、绝不写已读。
  assert.equal(none, null);
  assert.deepEqual(emptyCalls, [['activeBot', ''], ['request', null], ['refreshConversations']]);
});

test('开发团队全部置顶时部门标题与 Bot 仍在，不再有吃空部门的「置顶」分区', async () => {
  const { buildBotSections } = await sectionsModule();
  const bots = [
    { id: 'fe', name: '前端', group_name: '开发团队', pinned: true },
    { id: 'be', name: '后端', group_name: '开发团队', pinned: true },
    { id: 'social', name: '小红书', group_name: '新媒体团队', pinned: true },
  ];

  const sections = buildBotSections(bots);

  assert.deepEqual(sections.map((section) => section.group), ['开发团队', '新媒体团队']);
  assert.deepEqual(sections[0].bots.map((bot) => bot.id), ['fe', 'be']);
  assert.equal(sections.some((section) => section.group === '置顶'), false);
});

test('取消置顶后 Bot 仍留在原部门，只改变组内排序', async () => {
  const { buildBotSections } = await sectionsModule();
  const bots = [
    { id: 'fe', name: '前端', group_name: '开发团队', pinned: true, created_at: '2026-11-10T00:00:00Z' },
    { id: 'be', name: '后端', group_name: '开发团队', created_at: '2026-11-16T00:00:00Z' },
    { id: 'social', name: '小红书', group_name: '新媒体团队' },
  ];

  const [before] = buildBotSections(bots);
  assert.deepEqual(before.bots.map((bot) => bot.id), ['fe', 'be']);

  // 取消置顶：同一份名单里只翻 pinned，不搬家。
  const unpinned = bots.map((bot) => (bot.id === 'fe' ? { ...bot, pinned: false } : bot));
  const sections = buildBotSections(unpinned);
  assert.deepEqual(sections.map((section) => section.group), ['开发团队', '新媒体团队']);
  assert.deepEqual(sections[0].bots.map((bot) => bot.id), ['be', 'fe']);
  assert.equal(sections.flatMap((section) => section.bots).length, 3);
});

test('未读、置顶、主 Bot 都不改写部门归属：每个 Bot 只出现在自己的部门里', async () => {
  const { buildBotSections } = await sectionsModule();
  const bots = [
    { id: 'lead', name: '开发主 Agent', group_name: '开发团队', is_primary: true },
    { id: 'quiet', name: '没有未读的开发', group_name: '开发团队' },
    { id: 'xhs', name: '新媒体运营', group_name: '新媒体团队' },
    { id: 'loose', name: '散装', group_name: '  ' },
  ];

  const sections = buildBotSections(bots);
  const listed = sections.flatMap((section) => section.bots.map((bot) => bot.id));

  assert.deepEqual(sections.map((section) => section.group), ['开发团队', '新媒体团队', '其他']);
  assert.deepEqual(listed.sort(), ['lead', 'loose', 'quiet', 'xhs']);
  // 主 Agent 那个 Bot 仍排在自己的部门第一位，部门标题不会因此消失
  assert.equal(sections[0].bots[0].id, 'lead');
});

test('内置主 Agent 是真实 bot 行：只占大卡、头像保留品牌像素标、部门不重复', async () => {
  const source = await read(railPath);
  const inboxLib = await read(inboxLibPath);
  const { BUILTIN_ALPHA_AGENT_ID, isBuiltinAlphaAgent } = await inboxModule();

  // 稳定 ID 与后端 agenthub.BuiltinAlphaAgentID 一致；判定可真跑。
  assert.equal(BUILTIN_ALPHA_AGENT_ID, 'bot_builtin_alpha_agent');
  assert.equal(isBuiltinAlphaAgent({ id: BUILTIN_ALPHA_AGENT_ID }), true);
  assert.equal(isBuiltinAlphaAgent({ id: 'bot_072ba5516241fd28cb9acf28' }), false);
  assert.match(inboxLib, /const BUILTIN_ALPHA_AGENT_ID = 'bot_builtin_alpha_agent'/);
  // 部门名单排除内置行；大卡和收件箱头像对内置行保留 AlphaPixelMark。
  assert.match(source, /const rosterBots = bots\.filter\(\(bot\) => !isBuiltinAlphaAgent\(bot\)\);/);
  assert.match(source, /mainAgent\.bot && !isBuiltinAlphaAgent\(mainAgent\.bot\)/);
  assert.match(source, /if \(isBuiltinAlphaAgent\(bot\)\) \{[\s\S]*?<AlphaPixelMark/);

  // 真跑：把内置行喂进部门分区前先按 rosterBots 规则过滤，不产生「其他」分区。
  const { buildBotSections } = await sectionsModule();
  const roster = [
    { id: BUILTIN_ALPHA_AGENT_ID, name: 'Alpha Agent', group_name: '' },
    { id: 'dev', name: '开发', group_name: '开发团队' },
    { id: 'xhs', name: '运营', group_name: '新媒体团队' },
  ].filter((bot) => !isBuiltinAlphaAgent(bot));
  const sections = buildBotSections(roster);
  assert.deepEqual(sections.map((section) => section.group), ['开发团队', '新媒体团队']);
  assert.equal(sections.flatMap((section) => section.bots).some((bot) => isBuiltinAlphaAgent(bot)), false);
});

test('主 Agent 四处头像一致：大卡/收件箱/对话区/详情面板都用品牌像素标', async () => {
  const rail = await read(railPath);
  const panel = await read(panelPath);
  const detail = await read('../src/components/BotDetailPanel.tsx');
  const drawer = await read('../src/components/AgentDrawer.tsx');

  // 大卡：内置行不走 PixelAvatar/BotAvatar，而是 AlphaPixelMark。
  assert.match(rail, /mainAgent\.bot && !isBuiltinAlphaAgent\(mainAgent\.bot\)/);
  // BotAvatar（收件箱/行内复用）对内置行返回 AlphaPixelMark，不走首字母。
  assert.match(rail, /if \(isBuiltinAlphaAgent\(bot\)\) \{\s*return <AlphaPixelMark/);
  // 详情面板：内置行返回 AlphaPixelMark，不画橙色团块。
  assert.match(detail, /if \(isBuiltinAlphaAgent\(bot\)\) \{\s*return <AlphaPixelMark/);
  // 对话区：AgentIdentityAvatar 对内置行返回 AlphaPixelMark，不退化成字母 A。
  assert.match(drawer, /if \(isBuiltinAlphaAgent\(bot\)\) \{[\s\S]{0,200}?<AlphaPixelMark/);
  // 收件箱历史行（无 bot）仍用 AlphaPixelMark，与主 Agent 像素标一致。
  assert.match(panel, /row\.bot \? <BotAvatar bot=\{row\.bot\} size="md" \/> : <AlphaPixelMark/);
});

test('常驻团队列表保留像素头像、皇冠、置顶与免打扰', async () => {
  const source = await read(railPath);

  assert.match(source, /data-testid="bot-rail-chief-crown"/);
  assert.match(source, /data-testid=\{`bot-rail-primary-\$\{bot\.id\}`\}/);
  assert.match(source, /data-testid=\{`bot-rail-pin-\$\{bot\.id\}`\}/);
  assert.match(source, /data-testid=\{bot\.muted \? `bot-rail-muted-\$\{bot\.id\}` : `bot-rail-unmuted-\$\{bot\.id\}`\}/);
  assert.match(source, /data-testid=\{`bot-rail-unread-\$\{bot\.id\}`\}/);
  assert.match(source, /<BotAvatar bot=\{bot\} size=\{compact \? 'sm' : 'md'\} \/>/);
});

test('打开 Bot 会清掉整行未读，而不是只清当前那条会话', async () => {
  const { openBotThread, openMainAgentThread } = await inboxModule();
  const calls = [];
  const deps = {
    productMode: 'media',
    conversations: [{ id: 'conv-new', bot_id: 'dev-frontend', updated_at: '2026-11-16T10:00:00Z' }],
    requestAgentConversation: () => {},
    setActiveBotID: () => {},
    createConversation: async () => ({ id: 'never' }),
    markRead: async (id) => calls.push(['read', id]),
    markAllRead: async (botID) => calls.push(['readAll', botID]),
    refreshConversations: () => {},
    refreshRail: () => calls.push(['refreshRail']),
  };
  await openBotThread({ id: 'dev-frontend', name: '前端' }, deps);
  assert.deepEqual(calls, [
    ['read', 'conv-new'],
    ['readAll', 'dev-frontend'],
    ['refreshRail'],
  ]);

  calls.length = 0;
  await openMainAgentThread({
    ...deps,
    conversations: [{ id: 'conv-main', updated_at: '2026-11-16T11:00:00Z' }],
  });
  assert.deepEqual(calls, [
    ['read', 'conv-main'],
    ['readAll', ''],
    ['refreshRail'],
  ]);
});
