import type { AgentBot } from '../api';

// 群聊语义只在「开头点名」这一种形态上成立：`@开发 帮我看登录`。
// 不做全文扫描——正文里提到 Bot 名不该变成派活，否则用户写的「问一下 @开发」
// 会意外触发一次真实运行。这一段是纯函数，`ui-web/test/bot-delegation.test.mjs`
// 会真跑它。
// #region bot-delegation-logic
export interface BotDelegationTarget {
  bot: AgentBot;
  task: string;
  /** 写回输入框的原文（`@名字 任务`），保证父会话里的消息和用户看到的一致。 */
  mention: string;
}

/** 名字里有空格也能正确匹配：长的先试，避免 `@开发 助手` 被 `@开发` 截断。 */
function botsByNameLength(bots: AgentBot[]): AgentBot[] {
  return [...bots].sort((left, right) => (right.name ?? '').length - (left.name ?? '').length);
}

/**
 * 解析一条消息是否在开头点名了某个 Bot。
 * 只认「@名字 + 空白 + 任务」；只写 `@名字` 没有任务时返回 null，让用户继续输入。
 */
export function parseBotDelegation(input: string, bots: AgentBot[]): BotDelegationTarget | null {
  const text = (input ?? '').trimStart();
  if (!text.startsWith('@') || bots.length === 0) return null;
  for (const bot of botsByNameLength(bots)) {
    const name = (bot.name ?? '').trim();
    if (!name) continue;
    const prefix = `@${name}`;
    if (!text.startsWith(prefix)) continue;
    const rest = text.slice(prefix.length);
    // `@开发助手` 不能命中 `@开发`：名字后面必须是空白或结束。
    if (rest && !/^\s/.test(rest)) continue;
    const task = rest.trim();
    if (!task) return null;
    return { bot, task, mention: `${prefix} ${task}` };
  }
  return null;
}

/** 输入框里正在输入 `@…` 时，给出可派活的 Bot 候选。 */
export function botMentionSuggestions(input: string, bots: AgentBot[], limit = 4): AgentBot[] {
  const match = input.match(/(?:^|\s)@([^\s@]*)$/);
  if (!match) return [];
  const query = match[1].trim().toLowerCase();
  return bots
    .filter((bot) => (bot.name ?? '').trim())
    .filter((bot) => {
      if (!query) return true;
      const name = (bot.name ?? '').toLowerCase();
      const role = (bot.role ?? '').toLowerCase();
      return name.includes(query) || role.includes(query);
    })
    .slice(0, limit);
}

/** 正在跑的交接，用于对话上方的状态条；终态由会话消息呈现。 */
export function runningDelegations<T extends { status: string }>(delegations: T[] | null | undefined): T[] {
  return (delegations ?? []).filter((item) => item.status === 'running');
}
// #endregion bot-delegation-logic
