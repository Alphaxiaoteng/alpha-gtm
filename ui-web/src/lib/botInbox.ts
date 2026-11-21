import type { AgentBot, AgentBotRail, AgentHubConversation } from '../api';

// 收件箱的数据与打开逻辑都在这一段：右侧 `BotInboxPanel` 渲染它，`BotRail` 的
// 团队列表点 Bot / 点主 Agent 也复用同一套打开流程，避免两处各写一遍。
// 收件箱采用独立列表 + 独立面板：
// 收件箱是独立右栏，从侧栏
// 行打开后占用右侧面板，侧栏团队列表不受它过滤。这里只搬「列表 + 打开」两件事。
//
// 这一段是纯函数区域（无 import、无 JSX），`ui-web/test/bot-inbox.test.mjs` 会把
// 它转译后在 Node 里真跑：给假依赖即可断言真实会话 id、真实创建与已读写入。
// #region bot-inbox-logic
/** 主 Agent 现在是 agent_bots 表里的一条真实内置行；历史空 bot_id 会话仍由收件箱单独一行兜底。 */
const MAIN_AGENT_ROW_KEY = 'main-agent';
const MAIN_AGENT_NAME = 'AlphaBot';
/** 与后端 agenthub.BuiltinAlphaAgentID 完全一致：内置主 Agent 的稳定 ID。 */
const BUILTIN_ALPHA_AGENT_ID = 'bot_builtin_alpha_agent';
/** 内置主 Agent：真实 bot 记录，但头像用品牌像素标、且不重复进部门分组。 */
function isBuiltinAlphaAgent(bot: { id?: string } | null | undefined): boolean {
  return bot?.id === BUILTIN_ALPHA_AGENT_ID;
}
/** 没写团队名的 Bot 归到「其他」。 */
const UNGROUPED_BOT_GROUP = '其他';

export interface BotInboxRow {
  key: string;
  name: string;
  group: string;
  unread: number;
  preview: string;
  at: string;
  bot?: AgentBot;
}

function botGroupName(bot: { group_name?: string | null }): string {
  return (bot.group_name ?? '').trim() || UNGROUPED_BOT_GROUP;
}

/** 收件箱的时间只用来排序扫读：一小时内看相对时间，更早看日期，不引新依赖。 */
function inboxTimeLabel(value?: string, now = Date.now()): string {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const elapsed = now - date.getTime();
  if (elapsed >= 0 && elapsed < 60_000) return '刚刚';
  if (elapsed >= 0 && elapsed < 3_600_000) return `${Math.floor(elapsed / 60_000)} 分钟前`;
  if (elapsed >= 0 && elapsed < 86_400_000) return `${Math.floor(elapsed / 3_600_000)} 小时前`;
  return date.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' });
}

/**
 * 收件箱只列有未读的会话：Bot 行来自 rail 聚合请求（未读 / 最后一行 / 时间），
 * 主 Agent 的未读单独一行。按最后消息时间倒序，最近的排最上面。
 */
function buildInboxRows(rail?: AgentBotRail | null): BotInboxRow[] {
  if (!rail) return [];
  const railItems = rail.bots ?? [];
  const mainAgentUnread = rail.agent?.unread ?? 0;
  // 主 Bot 占了主 Agent 位时，AlphaBot 那一行不再跟着标成「主 Agent」，
  // 否则收件箱里会有两条看着都像主 Agent 的条目。内置主 Agent 是顶部大卡，
  // 不在普通行里再列一遍，也不参与这里的 primary 判定。
  const primaryBot = railItems.find((item) => item.is_primary && !isBuiltinAlphaAgent(item));
  const rows: BotInboxRow[] = [
    ...railItems
      .filter((item) => (item.unread ?? 0) > 0 && !isBuiltinAlphaAgent(item))
      .map((item) => ({
        key: item.id,
        name: item.name,
        group: botGroupName(item),
        unread: item.unread ?? 0,
        preview: item.last_message || item.role || '有未读消息',
        at: item.last_message_at ?? '',
        bot: item,
      })),
    ...(mainAgentUnread > 0
      ? [{
          key: MAIN_AGENT_ROW_KEY,
          name: MAIN_AGENT_NAME,
          group: primaryBot ? MAIN_AGENT_NAME : '主 Agent',
          unread: mainAgentUnread,
          preview: rail.agent?.last_message || '有未读回复',
          at: rail.agent?.last_message_at ?? '',
        }]
      : []),
  ];
  return rows.sort((left, right) => (right.at ?? '').localeCompare(left.at ?? ''));
}

/** 标题上的未读总数：直接取 rail 的聚合值，不在前端重算。 */
function inboxUnreadTotal(rail?: AgentBotRail | null): number {
  return rail?.unread_total ?? 0;
}

function byUpdatedDesc(left: AgentHubConversation, right: AgentHubConversation): number {
  return (right.updated_at ?? '').localeCompare(left.updated_at ?? '');
}

/** 一个 Bot 可能有多条线程：打开最近更新的那条，未读最可能就在这里。 */
function latestConversationForBot(
  conversations: ReadonlyArray<AgentHubConversation>,
  botID: string,
): AgentHubConversation | undefined {
  return conversations.filter((item) => item.bot_id === botID).sort(byUpdatedDesc)[0];
}

/** 主 Agent 的线程就是会话列表里最新一条没有 bot_id 的会话。 */
function latestMainAgentConversation(
  conversations: ReadonlyArray<AgentHubConversation>,
): AgentHubConversation | undefined {
  return conversations.filter((item) => !item.bot_id).sort(byUpdatedDesc)[0];
}

/** 打开会话要用的真实依赖：组件传 zustand setter 与 api，测试传假的。 */
export interface BotThreadOpener {
  productMode: 'media' | 'shop';
  conversations: ReadonlyArray<AgentHubConversation>;
  requestAgentConversation: (conversationID: string | null) => void;
  setActiveBotID: (botID: string) => void;
  createConversation: (body: {
    bot_id: string;
    title: string;
    context: { workspace: string; page: string; product_mode: 'media' | 'shop' };
  }) => Promise<{ id: string }>;
  markRead: (conversationID: string) => Promise<unknown>;
  /** 清掉整行角标（该 Bot 的全部线程）；主 Agent 传空字符串。 */
  markAllRead?: (botID: string) => Promise<unknown>;
  refreshConversations: () => Promise<unknown> | void;
  refreshRail: () => Promise<unknown> | void;
}

/**
 * 打开某个 Bot 的线程：优先复用最近会话，没有才真实创建一条。
 * 写已读失败只是角标不清零，绝不阻断打开。
 */
async function openBotThread(bot: AgentBot, deps: BotThreadOpener): Promise<string> {
  deps.setActiveBotID(bot.id);
  const existing = latestConversationForBot(deps.conversations, bot.id);
  let conversationID = existing?.id ?? '';
  if (!conversationID) {
    const created = await deps.createConversation({
      bot_id: bot.id,
      title: bot.name,
      context: { workspace: 'bot', page: 'bot-chat', product_mode: deps.productMode },
    });
    conversationID = created.id;
    await deps.refreshConversations();
  }
  deps.requestAgentConversation(conversationID);
  try {
    await deps.markRead(conversationID);
    // 同一 Bot 可能有多条历史线程；只清当前这条会留下永远点不掉的角标。
    if (deps.markAllRead) await deps.markAllRead(bot.id);
    await deps.refreshRail();
  } catch {
    // Keep the dot; the conversation is already open.
  }
  return conversationID;
}

/**
 * 打开主 Agent 的会话：历史无归属会话优先（保住既有上下文），一条都没有时
 * 回落到内置主 bot 的真实会话（真实创建/复用，绝不编 id）。builtinBot 是后端
 * EnsureBuiltinAlphaAgent 保证存在的那条真实行；不传时保持旧行为（只开舞台）。
 * 大卡与收件箱共用这个函数，主 Agent 只有一处入口、一组会话。
 */
async function openMainAgentThread(deps: BotThreadOpener, builtinBot?: AgentBot): Promise<string | null> {
  const latest = latestMainAgentConversation(deps.conversations);
  if (latest) {
    deps.setActiveBotID('');
    deps.requestAgentConversation(latest.id);
    try {
      await deps.markRead(latest.id);
      if (deps.markAllRead) await deps.markAllRead('');
      await deps.refreshRail();
    } catch {
      // 写已读失败只是角标不清零，会话已经打开。
    }
    await deps.refreshConversations();
    return latest.id;
  }
  if (builtinBot) {
    return openBotThread(builtinBot, deps);
  }
  deps.setActiveBotID('');
  deps.requestAgentConversation(null);
  await deps.refreshConversations();
  return null;
}

/** 打开失败时的中文提示；模型没验证是唯一需要把用户送去设置页的错误。 */
function botOpenFailure(reason: unknown): { message: string; blockedReason: string } {
  const message = reason instanceof Error ? reason.message : '';
  if (message.includes('AGENT_MODEL_NOT_VERIFIED')) {
    return {
      message: '先到设置 → AlphaBot 验证一个模型，再开始 Bot 对话',
      blockedReason: '需要先验证一个模型',
    };
  }
  return { message: message || '打开 Bot 失败', blockedReason: '' };
}
// #endregion bot-inbox-logic

export {
  BUILTIN_ALPHA_AGENT_ID,
  MAIN_AGENT_NAME,
  MAIN_AGENT_ROW_KEY,
  UNGROUPED_BOT_GROUP,
  isBuiltinAlphaAgent,
  botGroupName,
  botOpenFailure,
  buildInboxRows,
  inboxTimeLabel,
  inboxUnreadTotal,
  latestConversationForBot,
  latestMainAgentConversation,
  openBotThread,
  openMainAgentThread,
};
