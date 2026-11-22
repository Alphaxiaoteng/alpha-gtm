import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import useSWR from 'swr';
import { Bell, BellOff, Bot as BotIcon, ChevronRight, Copy, Crown, Inbox, Loader2, Pencil, Pin, PinOff, Plus, Trash2, X } from 'lucide-react';
import { api, apiErrorText, type AgentBot } from '../api';
import { useUIStore } from '../store';
import { revalidateBotCaches } from '../lib/botCache';
import { botOpenFailure, isBuiltinAlphaAgent, openBotThread, openMainAgentThread, type BotThreadOpener } from '../lib/botInbox';
import { PixelAvatar, pixelAvatarRole, pixelAvatarSrc, platformAvatarID, type PixelAvatarRole } from './PixelAvatars';
import { AlphaPixelMark } from './AlphaPixelMark';

/** Unread dot stays blue: the rail accent is black, a dot must read as a signal. */
const UNREAD_DOT = '#2f6fed';
/** 主 Agent 显示名；真实身份是 agent_bots 里的内置行，后端未就绪时用它兜底。 */
const MAIN_AGENT_NAME = 'AlphaBot';

// 部门分区：
// 名单按 `agent_bots.group_name` 分组平铺，不切独立的「置顶」分区；置顶只影响组内
// 排序与卡片上的图钉标记。本应用没有独立部门表，部门名只写在 `group_name` 上——
// 一旦把置顶的 Bot 从部门里摘走，那个部门就等于被删掉（用户报的「开发团队怎么
// 没有了」）。只有一个部门时连部门标题都不画，整份名单直接平铺。
//
// 下面这段是纯函数区域（无 import、无 JSX），`ui-web/test/bot-inbox.test.mjs` 会把
// 它转译后在 Node 里真跑，防止「全部置顶 → 部门消失」回归。
// #region bot-rail-sections
/** Rail section for personas that were never given a 团队 name. */
const UNGROUPED_LABEL = '其他';

/** 分区只读这几个字段，测试可以直接喂最小对象。 */
interface RailSectionBot {
  group_name?: string | null;
  is_primary?: boolean;
  pinned?: boolean;
  created_at?: string;
}

function groupNameOf(bot: { group_name?: string | null }): string {
  return (bot.group_name ?? '').trim() || UNGROUPED_LABEL;
}

/** 部门顺序：主 Bot 所在的部门领跑，其余按名字，未分组的「其他」永远最后。 */
function botGroupOrder(bots: ReadonlyArray<{ group_name?: string | null; is_primary?: boolean }>): string[] {
  const names = Array.from(new Set(bots.map((bot) => groupNameOf(bot))));
  const primaryGroup = (bots.find((bot) => bot.is_primary)?.group_name ?? '').trim();
  return names.sort((left, right) => {
    if (primaryGroup && left !== right) {
      if (left === primaryGroup) return -1;
      if (right === primaryGroup) return 1;
    }
    if (left === UNGROUPED_LABEL && right !== UNGROUPED_LABEL) return 1;
    if (right === UNGROUPED_LABEL && left !== UNGROUPED_LABEL) return -1;
    return left.localeCompare(right, 'zh-Hans-CN');
  });
}

/**
 * 部门就是列表本身：每个部门都带着自己的全部 Bot 出场，置顶只在部门内排到最前。
 * 部门名取自整份名单，所以「全部置顶」「全部已读」都不会让部门标题消失。
 * 组内顺序沿用后端契约：主 Bot → 置顶 → 新建在前。
 */
function buildBotSections<T extends RailSectionBot>(bots: T[]): Array<{ group: string; bots: T[] }> {
  const order = botGroupOrder(bots);
  const sorted = [...bots].sort((left, right) => {
    const groupDiff = order.indexOf(groupNameOf(left)) - order.indexOf(groupNameOf(right));
    if (groupDiff !== 0) return groupDiff;
    if (Boolean(left.is_primary) !== Boolean(right.is_primary)) return left.is_primary ? -1 : 1;
    if (Boolean(left.pinned) !== Boolean(right.pinned)) return left.pinned ? -1 : 1;
    return (right.created_at ?? '').localeCompare(left.created_at ?? '');
  });
  return order
    .map((group) => ({ group, bots: sorted.filter((bot) => groupNameOf(bot) === group) }))
    .filter((section) => section.bots.length > 0);
}
// #endregion bot-rail-sections

// Bot 行的右键菜单：
// 右键只弹菜单（`preventDefault`），不打开会话；
// 菜单在鼠标位置弹出并在视口内收边；「置顶 / 取消置顶」按 `bot.pinned` 切换，
// 写的是真实后端（`api.setAgentBotPinned`），成功后让两套投影缓存失效。
// 取消置顶只改组内排序：Bot 仍留在自己的部门里，部门标题照旧渲染。
//
// 这一段是纯函数区域（无 import、无 JSX），`ui-web/test/bot-context-menu.test.mjs`
// 会把它转译后在 Node 里真跑，用假依赖断言真实调用参数与缓存刷新。
// #region bot-rail-menu
interface BotRowMenuState {
  bot: AgentBot;
  x: number;
  y: number;
}

/** 分组标题的右键菜单：只对整组生效，不针对某一个 Bot。 */
interface GroupMenuState {
  group: string;
  bots: AgentBot[];
  x: number;
  y: number;
}

/** 菜单的估算尺寸：两条 34px 选项 + 上下内边距，用于视口收边。 */
const BOT_MENU_SIZE = { width: 184, height: 2 * 34 + 2 * 4 };

/** 把菜单夹在视口内：靠近右/下边缘弹出时向内收，不越出窗口。 */
function clampMenuPosition(
  anchor: { x: number; y: number },
  size: { width: number; height: number },
  viewport: { width: number; height: number },
): { left: number; top: number } {
  const margin = 8;
  return {
    left: Math.max(margin, Math.min(anchor.x, viewport.width - size.width - margin)),
    top: Math.max(margin, Math.min(anchor.y, viewport.height - size.height - margin)),
  };
}

/** 右键只开菜单：只有左键 click 才打开会话。 */
function botRowPointerIntent(eventType: string): 'open' | 'ignore' {
  return eventType === 'click' ? 'open' : 'ignore';
}

/** 菜单第一项文案只看 `bot.pinned`。 */
function botPinMenuTitle(bot: { pinned?: boolean }): string {
  return bot.pinned ? '取消置顶' : '置顶';
}

function nextPinnedState(bot: { pinned?: boolean }): boolean {
  return !bot.pinned;
}

interface BotPinDeps {
  setPinned: (botID: string, pinned: boolean) => Promise<unknown>;
  revalidate: () => Promise<void>;
  toast: (message: string, type?: 'success' | 'error') => void;
}

/** 真实置顶切换：写后端 → 刷新名册与侧栏两套缓存 → 反馈；失败不假装成功。 */
async function toggleBotPinned(bot: AgentBot, deps: BotPinDeps): Promise<boolean> {
  const pinned = nextPinnedState(bot);
  try {
    await deps.setPinned(bot.id, pinned);
    await deps.revalidate();
    deps.toast(pinned ? '已置顶' : '已取消置顶', 'success');
    return pinned;
  } catch (reason) {
    deps.toast(reason instanceof Error ? reason.message : '置顶失败', 'error');
    return Boolean(bot.pinned);
  }
}
// #endregion bot-rail-menu

/**
 * Three ready-made personas for the empty rail. Picking one POSTs a real bot
 * through the same endpoint the settings form uses, then opens it: the user
 * never has to leave the page or fill a form to get a first reply.
 */
const RECOMMENDED_BOTS: Array<{
  key: string;
  name: string;
  tagline: string;
  role: string;
  avatar: PixelAvatarRole;
  group: string;
  instructions: string;
  kickoff: string;
}> = [
  {
    key: 'director',
    name: '内容编导',
    tagline: '把选题拆成标题、钩子和分镜',
    role: '内容编导',
    avatar: 'planner',
    group: '内容团队',
    instructions: '内容编导。把一句模糊的选题拆成：3 个标题、1 个开头钩子、按镜头排的分镜和可直接开拍的台词。先给方案，再解释理由，不说空话。',
    kickoff: '想做哪条内容？给我一句话选题，我拆成可拍的结构。',
  },
  {
    key: 'analyst',
    name: '数据复盘',
    tagline: '看数据、找原因、给下一步',
    role: '数据复盘',
    avatar: 'analyst',
    group: '内容团队',
    instructions: '数据复盘。看播放、完播、互动、涨粉这些数，先说结论再说依据，指出哪条掉得最狠、最可能的原因，并给出一个可执行的改动。',
    kickoff: '把这段时间的数据贴给我，我按结论 → 依据 → 下一步的顺序说。',
  },
  {
    key: 'xhs',
    name: '小红书运营',
    tagline: '标题、封面文案和发布节奏',
    role: '小红书运营',
    avatar: 'support',
    group: '内容团队',
    instructions: '小红书运营。熟悉小红书的标题口吻、封面文案和话题标签，输出可以直接复制粘贴的正文，注意平台对广告词和引流方式的限制。',
    kickoff: '今天想发什么内容？我先给三个标题方向，你挑一个我再写正文。',
  },
];

/**
 * The third workspace mode is a Bot page, not a drawer: this rail lists every
 * stored persona grouped by team, message-app style (inbox + unread + last
 * line), and the agent stage renders the selected bot's conversation on the
 * right. Selecting a bot reuses its most recent conversation and only creates
 * one when the persona has never been opened.
 */
export function BotRail() {
  const productMode = useUIStore((state) => state.productMode);
  const sidebarState = useUIStore((state) => state.sidebarState);
  const compact = sidebarState === 'rail';
  const requestedConversationID = useUIStore((state) => state.requestedAgentConversationID);
  const requestAgentConversation = useUIStore((state) => state.requestAgentConversation);
  const setActiveTab = useUIStore((state) => state.setActiveTab);
  const setActiveBotID = useUIStore((state) => state.setActiveBotID);
  // 收件箱是跨组件状态：点击只切换右栏面板（`Layout` 渲染），左侧团队列表
  // 从不因它折叠或过滤，所以开发团队、新媒体团队等标题始终在。
  const botInboxOpen = useUIStore((state) => state.botInboxOpen);
  const setBotInboxOpen = useUIStore((state) => state.setBotInboxOpen);
  const toast = useUIStore((state) => state.toast);

  const { data: bots = [], isLoading, error, mutate: mutateBots } = useSWR('api/agent-bots', api.listAgentBots);
  // The rail view is a second, read-only projection of the same roster: group,
  // mute, unread and the last message line in one request.
  const { data: rail, mutate: refreshRail } = useSWR('api/agent-bots/rail', api.agentBotRail, {
    // 交接结果和未读是后台写入的：不轮询就会一直停在旧角标。
    refreshInterval: 15_000,
    refreshWhenHidden: false,
  });
  const { data: conversationResult, mutate: refreshConversations } = useSWR(
    ['api/agent/conversations', productMode],
    () => api.agentHubConversations(undefined, productMode),
  );
  const [openingBotID, setOpeningBotID] = useState('');
  // Empty-rail shortcuts: which recommended persona is being created right now.
  const [creatingPresetKey, setCreatingPresetKey] = useState('');
  const [blockedReason, setBlockedReason] = useState('');
  // Optimistic selection: the row highlights on click even when the backend
  // refuses to open a conversation (e.g. model not verified yet).
  const [selectedBotID, setSelectedBotID] = useState('');
  const [query, setQuery] = useState('');
  // 右键菜单：位置是鼠标的视口坐标，机器人本身也存着，点击选项时用它的 pinned。
  const [botMenu, setBotMenu] = useState<BotRowMenuState | null>(null);
  const [groupMenu, setGroupMenu] = useState<GroupMenuState | null>(null);
  // 分组重命名走行内输入，避免再叠一层弹窗。
  const [renamingGroup, setRenamingGroup] = useState('');
  const [renameDraft, setRenameDraft] = useState('');

  const conversations = conversationResult?.conversations ?? [];
  const activeBotID = conversations.find((item) => item.id === requestedConversationID)?.bot_id ?? selectedBotID;
  const mainAgentUnread = rail?.agent?.unread ?? 0;
  const railItems = rail?.bots ?? [];
  const summaryByBotID = new Map(railItems.map((item) => [item.id, item]));
  const unreadTotal = rail?.unread_total ?? 0;
  const unreadOf = (botID: string) => summaryByBotID.get(botID)?.unread ?? 0;

  // 团队列表永远从完整名单出发：收件箱不参与这里的过滤，所以开发团队、新媒体
  // 团队这些没有未读的部门不会被一次点击从列表里抹掉。
  const normalizedQuery = query.trim().toLowerCase();
  // 内置主 Agent 是真实 bot 行，但它占据顶部大卡；部门名单里排除，避免又作为
  // 「其他」分组里一条重复出现（默认 bot 不在普通分区里再画一遍）。
  const rosterBots = bots.filter((bot) => !isBuiltinAlphaAgent(bot));
  const visibleBots = normalizedQuery
    ? rosterBots.filter((bot) =>
        `${bot.name} ${bot.role ?? ''} ${groupNameOf(bot)}`.toLowerCase().includes(normalizedQuery))
    : rosterBots;
  // 主 Agent 位：真实身份是 bots 表里的内置行（后端 EnsureBuiltinAlphaAgent 保证
  // 存在）。用户把别的 Bot 设为主 Agent 时才让位给它；内置行自己就是 primary。
  // 大卡点开的是这个真实 bot 的会话（next 模型），历史无归属会话只留在收件箱里
  // 作为历史入口，不再抢占大卡——否则点主 Agent 会回到旧的 DSH 验证会话。
  const builtinBot = bots.find((item) => isBuiltinAlphaAgent(item));
  const primaryBot = bots.find((item) => item.is_primary);
  const mainAgentRealBot = primaryBot && !isBuiltinAlphaAgent(primaryBot) ? primaryBot : builtinBot;
  const mainAgent = mainAgentRealBot
    ? {
        name: mainAgentRealBot.name,
        bot: mainAgentRealBot,
        role: pixelAvatarRole(mainAgentRealBot.avatar) as PixelAvatarRole | '',
        // 角标只算这条真实 bot 自己的未读；历史空归属未读由收件箱「主 Agent」行体现。
        unread: unreadOf(mainAgentRealBot.id),
      }
    : { name: MAIN_AGENT_NAME, bot: undefined as AgentBot | undefined, role: '' as PixelAvatarRole | '', unread: mainAgentUnread };
  const listBots = visibleBots;
  // 全部 Bot 按 group_name 平铺：不再单独切出「置顶」分区，置顶只影响组内排序
  // 和卡片上的图钉标记。只有一个部门时连部门标题也不画，五张卡直接连排。
  const sections = buildBotSections(visibleBots);
  const showGroupHeader = sections.length > 1;
  // 「置顶」是旧版在界面上人造的分区，从来不是后端的真实部门。若历史数据里残留
  // 同名分组，重命名会把它写成一个真实部门，所以它不绑分组右键菜单。
  const syntheticGroups = new Set(['置顶']);
  // 移动分组的目标列表：现有分组去掉 Bot 所在的当前分组。
  const moveTargets = Array.from(new Set(sections.map((section) => section.group)))
    .filter((group) => group !== groupNameOf(botMenu?.bot ?? {}));

  // 打开会话的真实依赖：与右侧收件箱面板共用 lib/botInbox 里的同一套流程，
  // 两处不会各自写一遍「建会话 → 请求 → 写已读 → 刷新」。
  const threadDeps: BotThreadOpener = {
    productMode,
    conversations,
    requestAgentConversation,
    setActiveBotID,
    createConversation: api.agentHubConversationCreate,
    markRead: api.markAgentHubConversationRead,
    markAllRead: api.markAgentHubConversationsReadAll,
    refreshConversations,
    refreshRail,
  };

  async function openBot(bot: AgentBot) {
    if (openingBotID) return;
    setSelectedBotID(bot.id);
    setBlockedReason('');
    setOpeningBotID(bot.id);
    try {
      await openBotThread(bot, threadDeps);
      // 打开会话后右栏回到这个 Bot 的详情：收件箱只在用户主动点开时占位。
      setBotInboxOpen(false);
    } catch (reason) {
      // Creating a conversation is gated on a verified model. Say so instead of
      // leaving the click with no visible result.
      const failure = botOpenFailure(reason);
      setBlockedReason(failure.blockedReason);
      toast.show(failure.message, 'error');
    } finally {
      setOpeningBotID('');
    }
  }

  /**
   * 大卡入口：打开当前主 Agent 的那条真实 bot 会话（内置行 = next 模型）。
   * 用户把别的 Bot 设为主 Agent 时，大卡走那个 Bot 的会话。只有后端还没建
   * 内置行（老数据/未 ensure）时，才回退到历史无归属会话，绝不编 id。
   */
  async function openMainAgent() {
    if (openingBotID) return;
    setSelectedBotID('');
    setBlockedReason('');
    setBotInboxOpen(false);
    if (mainAgentRealBot) {
      await openBot(mainAgentRealBot);
      return;
    }
    await openMainAgentThread(threadDeps);
  }

  /** 右键菜单的关闭路径：点外部、Escape、滚动、切换 Bot 都会收起。 */
  useEffect(() => {
    if (!botMenu) return;
    const close = () => setBotMenu(null);
    const onPointerDown = (event: MouseEvent) => {
      if (event.target instanceof Element && event.target.closest('[data-bot-row-menu]')) return;
      close();
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
    };
    window.addEventListener('mousedown', onPointerDown);
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('resize', close);
    // capture=true：侧栏内部滚动也要关，避免菜单跟着漂走。
    window.addEventListener('scroll', close, true);
    return () => {
      window.removeEventListener('mousedown', onPointerDown);
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('resize', close);
      window.removeEventListener('scroll', close, true);
    };
  }, [botMenu]);

  // 切换选中 Bot（含从收件箱打开）时菜单不留在屏幕上。
  useEffect(() => {
    setBotMenu(null);
  }, [activeBotID]);

  async function toggleBotPin(bot: AgentBot) {
    setBotMenu(null);
    await toggleBotPinned(bot, {
      setPinned: api.setAgentBotPinned,
      revalidate: revalidateBotCaches,
      toast: toast.show,
    });
  }

  /**
   * 找一个已验证的模型，供推荐卡创建的 Bot 使用。
   *
   * 推荐卡此前不带 model，而服务端只有在 Bot 有 model 时才会把它带进会话；
   * 于是「点推荐卡 → 建出 Bot → 打开会话」会在最后一步报
   * AGENT_MODEL_NOT_VERIFIED，提示用户「先去验证模型」——可他已经验证过了。
   */
  async function firstVerifiedRoute(): Promise<{ provider: string; model: string } | null> {
    try {
      const providers = await api.agentHubProviders();
      for (const probe of providers.probes ?? []) {
        const model = (probe.models ?? []).find((item) => item.trim() !== '');
        if (probe.ready && model) {
          return { provider: probe.provider_id, model };
        }
      }
    } catch {
      // 取不到模型时仍然创建 Bot：让它先存在于名册里，用户可以在设置里补模型。
    }
    return null;
  }

  async function createRecommended(preset: (typeof RECOMMENDED_BOTS)[number]) {
    if (creatingPresetKey || openingBotID) return;
    setBlockedReason('');
    setCreatingPresetKey(preset.key);
    try {
      const route = await firstVerifiedRoute();
      const created = await api.createAgentBot({
        name: preset.name,
        role: preset.role,
        instructions: preset.instructions,
        avatar: pixelAvatarSrc(preset.avatar),
        kickoff: preset.kickoff,
        group_name: preset.group,
        // 带上已验证模型，会话才能建起来；没有可用模型时留空，
        // 由下面的错误分支引导用户去验证。
        ...(route ? { model: route.model } : {}),
      });
      // Refresh both projections before opening so the new row is in place the
      // moment the conversation request lands.
      await Promise.all([mutateBots(), refreshRail()]);
      await openBot(created);
      toast.show(`已创建「${preset.name}」`, 'success');
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : '';
      if (message.includes('AGENT_MODEL_NOT_VERIFIED')) {
        setBlockedReason('需要先验证一个模型');
        toast.show('先到设置 → AlphaBot 验证一个模型，再开始 Bot 对话', 'error');
      } else {
        toast.show(message || '创建 Bot 失败', 'error');
      }
    } finally {
      setCreatingPresetKey('');
    }
  }

  function renderInboxBadge(count: number, testID = 'bot-rail-inbox-count') {
    if (count <= 0) return null;
    return (
      <span
        data-testid={testID}
        title={`${count} 条未读`}
        className="flex h-[22px] min-w-[22px] shrink-0 items-center justify-center rounded-full bg-brand-accent px-[6px] text-[11px] font-bold leading-none text-brand-on-accent"
      >
        {count}
      </span>
    );
  }

  /**
   * 分组重命名。后端没有分组实体，分组名只是每个 Bot 的 group_name，
   * 所以重命名等于把这一组里每个 Bot 的 group_name 一起改掉。
   * 后端的 Bot 更新是按 id 的 PUT，改多个只能逐个发。
   */
  async function renameGroup(group: string, nextName: string) {
    const trimmed = nextName.trim();
    if (!trimmed || trimmed === group) {
      setRenamingGroup('');
      return;
    }
    const members = groupMenu?.bots ?? visibleBots.filter((bot) => groupNameOf(bot) === group);
    setGroupMenu(null);
    setRenamingGroup('');
    try {
      await Promise.all(members.map((bot) => api.updateAgentBot(bot.id, { group_name: trimmed } as never)));
      await revalidateBotCaches();
      toast.show(`分组已改名为 ${trimmed}（${members.length} 个 Bot）`, 'success');
    } catch (reason) {
      toast.show(`重命名失败：${apiErrorText(reason)}`, 'error');
    }
  }

  /**
   * 删除一个 Bot。会话不删，只解绑——历史记录仍然可查。
   *
   * 内置主 Agent 由后端拦（403）；这里也先挡一次，避免用户点了才被拒。
   */
  async function deleteBot(bot: AgentBot) {
    setBotMenu(null);
    if (isBuiltinAlphaAgent(bot)) {
      toast.show('内置主 Agent 不能删除', 'error');
      return;
    }
    if (typeof window !== 'undefined' && !window.confirm(`删除 Bot「${bot.name}」？历史会话会保留，但不再挂在它名下。`)) {
      return;
    }
    try {
      await api.deleteAgentBot(bot.id);
      await Promise.all([mutateBots(), refreshRail()]);
      toast.show(`已删除「${bot.name}」`, 'success');
    } catch (reason) {
      toast.show(`删除失败：${apiErrorText(reason)}`, 'error');
    }
  }

  /**
   * 把 Bot 移到另一个分组。
   *
   * 后端没有分组实体，分组只是 Bot 上的 group_name 字段，所以移动就是
   * 改这一个字段。目标分组名来自现有分组列表，避免拼错造出碎组。
   */
  async function moveBotToGroup(bot: AgentBot, target: string) {
    setBotMenu(null);
    if (groupNameOf(bot) === target) return;
    try {
      await api.updateAgentBot(bot.id, { group_name: target === UNGROUPED_LABEL ? '' : target } as never);
      await Promise.all([mutateBots(), refreshRail()]);
      toast.show(`已移到「${target}」`, 'success');
    } catch (reason) {
      toast.show(`移动失败：${apiErrorText(reason)}`, 'error');
    }
  }

  /**
   * 复制一个 Bot：新建一条同配置的人格。
   *
   * 后端没有 clone 端点，所以这里读原 Bot 的字段再 POST 一份新的。
   * 刻意不复制 is_primary（主 Agent 只能有一个）与 pinned（新副本不该
   * 立刻插到顶部打乱现有顺序）；名称加「副本」后缀便于区分。
   */
  async function duplicateBot(bot: AgentBot) {
    setBotMenu(null);
    try {
      const created = await api.createAgentBot({
        name: `${bot.name} 副本`,
        role: bot.role ?? '',
        instructions: bot.instructions ?? '',
        model: bot.model ?? '',
        avatar: bot.avatar ?? '',
        kickoff: bot.kickoff ?? '',
        group_name: bot.group_name ?? '',
        capabilities: bot.capabilities ?? [],
      });
      await Promise.all([mutateBots(), refreshRail()]);
      toast.show(`已复制为「${created.name}」`, 'success');
    } catch (reason) {
      toast.show(`复制失败：${apiErrorText(reason)}`, 'error');
    }
  }

  async function toggleMute(bot: AgentBot) {
    try {
      await api.updateAgentBot(bot.id, { muted: !bot.muted });
      await revalidateBotCaches();
      toast.show(bot.muted ? '已取消免打扰' : '已设为免打扰', 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '操作失败', 'error');
    }
  }

  return (
    <aside
      data-testid="bot-rail"
      aria-label="Bot 列表"
      className="flex min-h-0 flex-1 flex-col"
    >


      {!compact && bots.length > 5 ? (
        <div className="relative px-2.5 pt-2">
          <input
            data-testid="bot-rail-search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key !== 'Escape') return;
              event.preventDefault();
              setQuery('');
              event.currentTarget.blur();
            }}
            placeholder="搜索 Bot"
            autoComplete="off"
            className="w-full rounded-full border border-brand-border bg-brand-surface-raised py-1.5 pl-3 pr-8 text-[12px] text-brand-muted outline-none placeholder:text-brand-muted focus:border-brand-border focus:bg-brand-surface"
          />
          {query ? (
            <button
              type="button"
              data-testid="bot-rail-search-clear"
              aria-label="清空搜索"
              title="清空搜索（Esc）"
              onClick={() => setQuery('')}
              className="absolute right-3 top-1/2 flex h-5 w-5 -translate-y-1/2 items-center justify-center rounded-full text-brand-muted transition hover:bg-brand-fg/[0.08] hover:text-brand-fg"
            >
              <X className="h-3 w-3" aria-hidden="true" />
            </button>
          ) : null}
        </div>
      ) : null}

      <div className={`border-b border-brand-border/70 ${compact ? 'px-1 py-2 space-y-1' : 'px-2.5 py-2.5 flex items-center gap-1.5'}`}>
        <button
          type="button"
          data-testid="bot-rail-create"
          title="新建 Bot（在设置里填写人格）"
          onClick={() => {
            if (typeof window !== 'undefined') {
              window.history.pushState(null, '', '/GTM/settings/personagent');
            }
            useUIStore.getState().setActiveTab('settings');
          }}
          className={`flex items-center rounded-lg border border-brand-border/80 bg-brand-surface text-left text-brand-fg transition-colors hover:bg-brand-fg/[0.04] ${
            compact ? 'w-full justify-center py-1.5' : 'flex-1 justify-center gap-1.5 px-2 py-1.5'
          }`}
        >
          <Plus className="h-3.5 w-3.5 shrink-0 text-brand-muted" aria-hidden="true" />
          {!compact && <span className="truncate text-[12px] font-medium">新建</span>}
        </button>
        <button
          type="button"
          data-testid="bot-rail-inbox"
          aria-pressed={botInboxOpen}
          title={botInboxOpen ? '收起收件箱' : '收件箱（未读）'}
          onClick={() => setBotInboxOpen(!botInboxOpen)}
          className={`flex items-center rounded-lg border border-brand-border/80 text-left transition-colors ${
            compact ? 'w-full justify-center py-1.5' : 'flex-1 justify-center gap-1.5 px-2 py-1.5'
          } ${botInboxOpen ? 'bg-brand-fg/[0.07] text-brand-fg' : 'bg-brand-surface text-brand-fg hover:bg-brand-fg/[0.04]'}`}
        >
          <Inbox className="h-3.5 w-3.5 shrink-0 text-brand-muted" aria-hidden="true" />
          {compact ? (
            unreadTotal > 0 ? (
              <span
                aria-label={`${unreadTotal} 条未读`}
                className="ml-0.5 h-1.5 w-1.5 rounded-full"
                style={{ background: UNREAD_DOT }}
              />
            ) : null
          ) : (
            <>
              <span className="truncate text-[12px] font-medium">收件箱</span>
              {renderInboxBadge(unreadTotal)}
            </>
          )}
        </button>
      </div>

      <div className={`min-h-0 flex-1 space-y-1.5 overflow-y-auto overflow-x-hidden ui-thin-scrollbar ${compact ? 'p-1.5' : 'px-2.5 py-3'}`}>
        {isLoading ? (
          // Skeleton rows, not a spinner: the list shape stays stable while the
          // roster loads, so the rows do not jump when data arrives.
          <ul data-testid="bot-rail-skeleton" aria-label="正在读取 Bot 列表" className="space-y-1">
            {[0, 1, 2].map((row) => (
              <li
                key={row}
                className={`flex items-center gap-2 rounded-lg ${compact ? 'justify-center py-2' : 'px-2 py-1.5'}`}
              >
                <span className="h-8 w-8 shrink-0 animate-pulse rounded-[3px] bg-brand-fg/[0.06]" />
                {compact ? null : (
                  <span className="flex min-w-0 flex-1 flex-col gap-1.5">
                    <span className="h-2 w-2/3 animate-pulse rounded-full bg-brand-fg/[0.07]" />
                    <span className="h-2 w-1/2 animate-pulse rounded-full bg-brand-fg/[0.04]" />
                  </span>
                )}
              </li>
            ))}
          </ul>
        ) : error && bots.length === 0 ? (
          <div data-testid="bot-rail-error" role="status" className={compact ? 'px-1 py-3 text-center' : 'px-2 py-3'}>
            <p className="text-[11px] leading-5 text-brand-muted">
              Bot 列表没读到 ·{' '}
              <button
                type="button"
                data-testid="bot-rail-retry"
                onClick={() => {
                  void mutateBots();
                  void refreshRail();
                }}
                className="font-semibold text-brand-fg underline underline-offset-2 hover:text-brand-accent"
              >
                重试
              </button>
            </p>
          </div>
        ) : bots.length === 0 ? (
          <div data-testid="bot-rail-empty" className={compact ? 'px-1 py-2' : 'px-1 py-2'}>
            {compact ? null : (
              <p className="px-1 pb-1.5 text-[11px] leading-4 text-brand-muted">
                还没有 Bot。挑一个现成的，点一下就能聊。
              </p>
            )}
            <div className="space-y-1">
              {RECOMMENDED_BOTS.map((preset) => (
                <button
                  key={preset.key}
                  type="button"
                  data-testid={`bot-rail-recommend-${preset.key}`}
                  title={preset.name}
                  disabled={Boolean(creatingPresetKey) || Boolean(openingBotID)}
                  onClick={() => void createRecommended(preset)}
                  className={`flex w-full items-center rounded-lg border border-brand-border bg-brand-surface text-left transition-colors hover:bg-brand-fg/[0.03] disabled:opacity-60 ${
                    compact ? 'justify-center py-1.5' : 'gap-2 px-2 py-1.5'
                  }`}
                >
                  <PixelAvatar role={preset.avatar} size={compact ? 24 : 28} className="shrink-0 rounded-[22%]" />
                  {compact ? null : (
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-[11px] font-semibold text-brand-fg">{preset.name}</span>
                      <span className="block truncate text-[10px] leading-4 text-brand-muted">{preset.tagline}</span>
                    </span>
                  )}
                  {creatingPresetKey === preset.key ? (
                    <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-brand-accent" />
                  ) : compact ? null : (
                    <Plus className="h-3.5 w-3.5 shrink-0 text-brand-muted" />
                  )}
                </button>
              ))}
            </div>
          </div>
        ) : listBots.length === 0 ? (
          <p data-testid="bot-rail-no-match" className="px-2 py-6 text-center text-[11px] text-brand-muted">
            没有匹配的 Bot
          </p>
        ) : (
          <>
            {/* 主 Agent 位：极简横向卡片 */}
            <button
              type="button"
              data-testid="bot-rail-chief"
              aria-label={mainAgent.name}
              title={`${mainAgent.name}（主 Agent）`}
              onClick={() => {
                void openMainAgent();
              }}
              className={`bot-rail-card flex w-full items-center rounded-xl border border-brand-border/70 bg-brand-surface-raised/65 text-left transition-colors hover:bg-brand-fg/[0.045] ${
                compact ? 'justify-center py-2' : 'gap-2.5 px-2.5 py-2.5'
              }`}
            >
              <span className="relative shrink-0">
                <span className={`flex items-center justify-center rounded-xl bg-brand-surface ${compact ? 'h-8 w-8' : 'h-9 w-9'}`}>
                  {mainAgent.bot && !isBuiltinAlphaAgent(mainAgent.bot) ? (
                    mainAgent.role ? (
                      <PixelAvatar role={mainAgent.role} size={28} className="rounded-lg" />
                    ) : (
                      <BotAvatar bot={mainAgent.bot} size="sm" />
                    )
                  ) : (
                    <AlphaPixelMark className="h-6 w-6" decorative />
                  )}
                </span>
                {mainAgent.unread > 0 ? (
                  <span
                    data-testid="bot-rail-agent-unread"
                    aria-label={`${mainAgent.unread} 条未读`}
                    className="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full ring-2 ring-brand-surface"
                    style={{ background: UNREAD_DOT }}
                  />
                ) : null}
                <span
                  data-testid="bot-rail-chief-crown"
                  role="img"
                  aria-label="主 Agent"
                  title="主 Agent"
                  className="absolute -bottom-1 -right-1 flex h-4 w-4 items-center justify-center rounded-full bg-brand-surface ring-1 ring-brand-border"
                >
                  <Crown className="h-2.5 w-2.5 text-[#c08a1e]" strokeWidth={2.2} />
                </span>
              </span>
              {compact ? null : (
                <span className="min-w-0 flex-1">
                  <span className="flex items-center justify-between gap-1.5">
                    <span className="truncate text-[13px] font-semibold text-brand-fg">{mainAgent.name}</span>
                    <span className="shrink-0 rounded bg-brand-fg/[0.06] px-1.5 py-0.5 text-[9px] font-medium text-brand-muted">总控</span>
                  </span>
                  <span className="mt-0.5 block truncate text-[11px] text-brand-muted">
                    {mainAgent.bot?.role || '全域业务与内容调度'}
                  </span>
                </span>
              )}
            </button>
            {sections.map((section) => (
            <div
              key={section.group}
              data-testid={`bot-rail-group-${section.group}`}
              className={showGroupHeader ? 'space-y-1 pt-2' : 'space-y-1'}
            >
              {compact || !showGroupHeader ? null : (
                <div
                  data-testid={`bot-rail-group-header-${section.group}`}
                  onContextMenu={(event) => {
                    if (syntheticGroups.has(section.group)) return;
                    event.preventDefault();
                    event.stopPropagation();
                    setGroupMenu({ group: section.group, bots: section.bots, x: event.clientX, y: event.clientY });
                  }}
                  className="flex items-center justify-between gap-2 px-2 pb-1 pt-1"
                >
                  {renamingGroup === section.group ? (
                    <input
                      autoFocus
                      data-testid={`bot-rail-group-rename-${section.group}`}
                      value={renameDraft}
                      onChange={(event) => setRenameDraft(event.target.value)}
                      onBlur={() => void renameGroup(section.group, renameDraft)}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter') void renameGroup(section.group, renameDraft);
                        if (event.key === 'Escape') setRenamingGroup('');
                      }}
                      className="w-full rounded border border-brand-accent/40 bg-brand-surface px-1.5 py-0.5 text-[12px] text-brand-fg outline-none"
                      aria-label="分组新名称"
                    />
                  ) : (
                    <span className="truncate text-[11px] font-medium text-brand-muted/85">{section.group}</span>
                  )}
                  <span className="shrink-0 text-[11px] tabular-nums text-brand-muted/70">{section.bots.length}</span>
                </div>
              )}
              {section.bots.map((bot) => {
                const active = bot.id === activeBotID;
                const unread = unreadOf(bot.id);
                const preview = bot.role || summaryByBotID.get(bot.id)?.last_message || '点击开启对话';
                return (
                  // 这一行原先是最外层 <button> 里再嵌免打扰 <button>（非法 HTML）。
                  // 改成 role="button" 的 div 后嵌套合法，点击、键盘与 testid 全部保留；
                  // 顺带获得右键菜单：右键只弹菜单，不打开会话。
                  <div
                    key={bot.id}
                    role="button"
                    tabIndex={0}
                    aria-haspopup="menu"
                    data-testid={`bot-rail-item-${bot.id}`}
                    aria-current={active ? 'true' : undefined}
                    aria-disabled={openingBotID ? true : undefined}
                    onClick={(event) => {
                      if (botRowPointerIntent(event.type) === 'open') void openBot(bot);
                    }}
                    onKeyDown={(event) => {
                      if (event.target !== event.currentTarget) return;
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault();
                        void openBot(bot);
                        return;
                      }
                      // ContextMenu 键 / Shift+F10 弹出同一份菜单，位置取行的中心。
                      if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
                        event.preventDefault();
                        const rect = event.currentTarget.getBoundingClientRect();
                        setBotMenu({ bot, x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 });
                        return;
                      }
                      // 列表内的方向键：不用 Tab 逐个跳，工具型界面更顺手。
                      if (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Home' || event.key === 'End') {
                        event.preventDefault();
                        const rows = Array.from(document.querySelectorAll<HTMLElement>('[data-testid^="bot-rail-item-"]'));
                        const index = rows.indexOf(event.currentTarget as HTMLElement);
                        const target = event.key === 'Home'
                          ? rows[0]
                          : event.key === 'End'
                            ? rows[rows.length - 1]
                            : rows[index + (event.key === 'ArrowDown' ? 1 : -1)];
                        target?.focus();
                      }
                    }}
                    onContextMenu={(event) => {
                      event.preventDefault();
                      event.stopPropagation();
                      setBotMenu({ bot, x: event.clientX, y: event.clientY });
                    }}
                    title={compact ? bot.name : `${bot.name}${bot.role ? ` · ${bot.role}` : ''}`}
                    className={`bot-rail-card group relative flex w-full cursor-pointer items-center rounded-xl text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/40 ${
                      openingBotID ? 'pointer-events-none opacity-60' : ''
                    } ${
                      compact ? 'justify-center py-2' : 'gap-2.5 px-2.5 py-2.5'
                    } ${
                      active
                        ? 'bg-brand-fg/[0.06] text-brand-fg'
                        : bot.pinned
                          ? 'bg-brand-surface-raised text-brand-fg hover:bg-brand-fg/[0.04]'
                          : 'text-brand-fg hover:bg-brand-fg/[0.035]'
                    }`}
                  >
                    {active ? (
                      <span aria-hidden="true" className="absolute left-0 top-1/2 h-5 w-0.5 -translate-y-1/2 rounded-full bg-brand-accent" />
                    ) : null}
                    {!compact && bot.is_primary ? (
                      <span
                        data-testid={`bot-rail-primary-${bot.id}`}
                        role="img"
                        aria-label="主 Bot"
                        title="主 Bot"
                        className="absolute -top-0.5 left-0 z-10 text-[#c08a1e]"
                      >
                        <Crown className="h-3 w-3" />
                      </span>
                    ) : null}
                    <span
                      className={`flex shrink-0 items-center justify-center rounded-lg bg-brand-surface-raised ${
                        compact ? 'h-8 w-8' : 'h-8 w-8'
                      }`}
                    >
                      <BotAvatar bot={bot} size="sm" />
                    </span>
                    {compact ? null : (
                      <span className="min-w-0 flex-1">
                        <span className="flex items-center gap-1.5">
                          <span className="min-w-0 truncate text-[13px] font-semibold leading-5">{bot.name}</span>
                          {unread > 0 ? (
                            <span
                              data-testid={`bot-rail-unread-${bot.id}`}
                              role="img"
                              aria-label={`${unread} 条未读`}
                              title={`${unread} 条未读`}
                              className="h-1.5 w-1.5 shrink-0 rounded-full"
                              style={{ background: UNREAD_DOT }}
                            />
                          ) : null}
                        </span>
                        <span className="mt-0.5 block truncate text-[11px] leading-4 text-brand-muted">
                          {preview}
                        </span>
                      </span>
                    )}
                    {compact || !bot.pinned ? null : (
                      <span
                        data-testid={`bot-rail-pin-${bot.id}`}
                        role="img"
                        aria-label="已置顶"
                        title="已置顶"
                        className="flex h-4 w-4 items-center justify-center text-[#c08a1e]"
                      >
                        <Pin className="h-4 w-4" strokeWidth={2.4} fill="currentColor" />
                      </span>
                    )}
                    {compact ? null : (
                      <span className="flex shrink-0 items-center gap-1">
                        {!compact ? (
                          <button
                            type="button"
                            data-testid={bot.muted ? `bot-rail-muted-${bot.id}` : `bot-rail-unmuted-${bot.id}`}
                            aria-label={bot.muted ? '取消免打扰' : '设为免打扰'}
                            title={bot.muted ? '取消免打扰' : '设为免打扰'}
                            onClick={(event) => {
                              event.stopPropagation();
                              void toggleMute(bot);
                            }}
                            className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full transition ${
                              bot.muted ? 'text-brand-accent opacity-100 hover:bg-brand-fg/[0.06]' : 'text-brand-muted opacity-0 group-hover:opacity-100 hover:bg-brand-fg/[0.04] hover:text-brand-fg'
                            }`}
                          >
                            {bot.muted ? <BellOff className="h-3.5 w-3.5" /> : <Bell className="h-3.5 w-3.5" />}
                          </button>
                        ) : null}
                        {openingBotID === bot.id ? (
                          <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-brand-accent" />
                        ) : null}
                      </span>
                    )}
                  </div>
                );
              })}
            </div>
            ))}
          </>
        )}
      </div>

      {blockedReason ? (
        <button
          type="button"
          data-testid="bot-rail-model-gate"
          title={compact ? blockedReason : undefined}
          onClick={() => setActiveTab('settings')}
          className={`m-1.5 rounded-lg border border-brand-warning/30 bg-brand-warning/10 text-left text-[10px] font-semibold leading-4 text-brand-warning transition-colors hover:bg-brand-warning/15 ${
            compact ? 'px-1 py-2 text-center' : 'px-2 py-2'
          }`}
        >
          {compact ? '!' : blockedReason}
          {compact ? null : <span className="mt-0.5 block font-normal text-brand-muted">点此去验证模型</span>}
        </button>
      ) : null}

      {/* 右键菜单用 portal 挂在 body 上：侧栏滚动容器不会把它裁掉，位置按鼠标
          坐标并在视口内收边。 */}
      {botMenu
        ? createPortal(
            <div
              data-bot-row-menu=""
              role="menu"
              aria-label={`${botMenu.bot.name} 的操作`}
              style={{
                position: 'fixed',
                // 颜色交给 .bot-theme 的 token（浅色白底黑字 / 深色深底浅字）；
                // 不要在这里写死浅色，否则深色皮肤下会变成深底黑字。
                ...clampMenuPosition(
                  { x: botMenu.x, y: botMenu.y },
                  BOT_MENU_SIZE,
                  { width: window.innerWidth, height: window.innerHeight },
                ),
              }}
              className="bot-theme z-[80] w-[184px] rounded-xl border border-brand-border bg-brand-surface p-1 text-left shadow-[0_10px_30px_rgba(0,0,0,0.14)]"
            >
              <button
                type="button"
                role="menuitem"
                data-testid="bot-rail-menu-pin"
                aria-pressed={Boolean(botMenu.bot.pinned)}
                onClick={() => void toggleBotPin(botMenu.bot)}
                className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-[12px] font-medium text-brand-fg transition-colors hover:bg-brand-fg/[0.05]"
              >
                {botMenu.bot.pinned ? (
                  <PinOff className="h-3.5 w-3.5 shrink-0 text-brand-muted" />
                ) : (
                  <Pin className="h-3.5 w-3.5 shrink-0 text-brand-muted" />
                )}
                <span className="truncate">{botPinMenuTitle(botMenu.bot)}</span>
              </button>
              <button
                type="button"
                role="menuitem"
                data-testid="bot-rail-menu-mute"
                aria-pressed={Boolean(botMenu.bot.muted)}
                onClick={() => {
                  const target = botMenu.bot;
                  setBotMenu(null);
                  void toggleMute(target);
                }}
                className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-[12px] font-medium text-brand-fg transition-colors hover:bg-brand-fg/[0.05]"
              >
                {botMenu.bot.muted ? (
                  <Bell className="h-3.5 w-3.5 shrink-0 text-brand-muted" />
                ) : (
                  <BellOff className="h-3.5 w-3.5 shrink-0 text-brand-muted" />
                )}
                <span className="truncate">{botMenu.bot.muted ? '取消免打扰' : '设为免打扰'}</span>
              </button>
              {/* 移动分组：列出除当前分组外的所有目标，点一下就移动。 */}
              {moveTargets.length > 0 ? (
                <div className="mt-1 border-t border-brand-border/60 pt-1">
                  <p className="px-2 pb-0.5 text-[10px] font-semibold uppercase tracking-wide text-brand-muted">移到分组</p>
                  {moveTargets.map((target) => (
                    <button
                      key={target}
                      type="button"
                      role="menuitem"
                      data-testid={`bot-rail-menu-move-${target}`}
                      onClick={() => void moveBotToGroup(botMenu.bot, target)}
                      className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-[12px] font-medium text-brand-fg transition-colors hover:bg-brand-fg/[0.05]"
                    >
                      <ChevronRight className="h-3.5 w-3.5 shrink-0 text-brand-muted" />
                      <span className="truncate">{target}</span>
                    </button>
                  ))}
                </div>
              ) : null}
              <button
                type="button"
                role="menuitem"
                data-testid="bot-rail-menu-duplicate"
                onClick={() => void duplicateBot(botMenu.bot)}
                className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-[12px] font-medium text-brand-fg transition-colors hover:bg-brand-fg/[0.05]"
              >
                <Copy className="h-3.5 w-3.5 shrink-0 text-brand-muted" />
                <span className="truncate">复制人格</span>
              </button>
              <button
                type="button"
                role="menuitem"
                data-testid="bot-rail-menu-edit"
                onClick={() => {
                  const target = botMenu.bot;
                  setBotMenu(null);
                  // 复用设置页的 Bot 团队分节：那里有完整的编辑表单。
                  if (typeof window !== 'undefined') {
                    window.history.pushState(null, '', '/GTM/settings/personagent');
                  }
                  useUIStore.getState().setActiveTab('settings');
                  void target;
                }}
                className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-[12px] font-medium text-brand-fg transition-colors hover:bg-brand-fg/[0.05]"
              >
                <Pencil className="h-3.5 w-3.5 shrink-0 text-brand-muted" />
                <span className="truncate">编辑资料</span>
              </button>
              <button
                type="button"
                role="menuitem"
                data-testid="bot-rail-menu-delete"
                onClick={() => void deleteBot(botMenu.bot)}
                className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-[12px] font-medium text-rose-600 transition-colors hover:bg-rose-50"
              >
                <Trash2 className="h-3.5 w-3.5 shrink-0" />
                <span className="truncate">删除 Bot</span>
              </button>
            </div>,
            document.body,
          )
        : null}

      {/* 分组菜单：与 Bot 行菜单同一套定位与视觉，只是作用对象是整组。 */}
      {groupMenu
        ? createPortal(
            <div
              data-bot-group-menu=""
              role="menu"
              aria-label={`${groupMenu.group} 分组的操作`}
              style={{
                position: 'fixed',
                ...clampMenuPosition(
                  { x: groupMenu.x, y: groupMenu.y },
                  BOT_MENU_SIZE,
                  { width: window.innerWidth, height: window.innerHeight },
                ),
              }}
              className="bot-theme z-[80] w-[184px] rounded-xl border border-brand-border bg-brand-surface p-1 text-left shadow-[0_10px_30px_rgba(0,0,0,0.14)]"
            >
              <button
                type="button"
                role="menuitem"
                data-testid="bot-group-menu-rename"
                onClick={() => {
                  setRenameDraft(groupMenu.group);
                  setRenamingGroup(groupMenu.group);
                  setGroupMenu(null);
                }}
                className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-[12px] font-medium text-brand-fg transition-colors hover:bg-brand-fg/[0.05]"
              >
                <Pencil className="h-3.5 w-3.5 shrink-0 text-brand-muted" />
                <span className="truncate">重命名分组</span>
              </button>
              <button
                type="button"
                role="menuitem"
                data-testid="bot-group-menu-mute"
                onClick={() => {
                  const targets = groupMenu.bots;
                  setGroupMenu(null);
                  void Promise.all(targets.map((bot) => api.updateAgentBot(bot.id, { muted: true } as never)))
                    .then(() => revalidateBotCaches())
                    .then(() => toast.show(`已把 ${targets.length} 个 Bot 设为免打扰`, 'success'))
                    .catch((reason) => toast.show(`操作失败：${apiErrorText(reason)}`, 'error'));
                }}
                className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-[12px] font-medium text-brand-fg transition-colors hover:bg-brand-fg/[0.05]"
              >
                <BellOff className="h-3.5 w-3.5 shrink-0 text-brand-muted" />
                <span className="truncate">整组免打扰</span>
              </button>
            </div>,
            document.body,
          )
        : null}
    </aside>
  );
}

/** 收件箱面板复用同一套像素头像，避免两处各写一份回退逻辑。 */
export function BotAvatar({ bot, size = 'md' }: { bot: AgentBot; size?: 'sm' | 'md' }) {
  const box = size === 'sm' ? 'h-7 w-7' : 'h-8 w-8';
  // 内置主 Agent 没有 avatar 字段，保留原来的品牌像素标，不用首字母兜底。
  if (isBuiltinAlphaAgent(bot)) {
    return <AlphaPixelMark className={size === 'sm' ? 'h-6 w-6' : 'h-7 w-7'} decorative />;
  }
  // Bundled avatars are inlined SVG (no request); generated ones are local
  // asset URLs served by the app.
  const role = pixelAvatarRole(bot.avatar);
  if (role) {
    return <PixelAvatar role={role} size={size === 'sm' ? 28 : 32} className="shrink-0 rounded-[22%]" />;
  }
  if (platformAvatarID(bot.avatar)) {
    return (
      <span className={`${box} flex shrink-0 items-center justify-center rounded-[22%] border border-brand-border bg-brand-surface`}>
        <img src={bot.avatar} alt="" decoding="async" className="h-5 w-5 object-contain" />
      </span>
    );
  }
  if (bot.avatar && bot.avatar.startsWith('/api/')) {
    return <img src={bot.avatar} alt="" decoding="async" className={`${box} shrink-0 rounded-[22%] object-cover [image-rendering:pixelated]`} />;
  }
  // A stored path that no longer resolves (e.g. a removed preset) must fall
  // back to the initial, never render the raw path as text.
  const glyph = bot.avatar && !bot.avatar.startsWith('/') ? bot.avatar : '';
  return (
    <span
      className={`flex ${box} shrink-0 items-center justify-center rounded-[3px] bg-brand-accent/12 text-brand-accent ${
        glyph ? 'text-[15px] leading-none' : 'text-[11px] font-bold'
      }`}
    >
      {glyph || bot.name.slice(0, 1) || <BotIcon className="h-3.5 w-3.5" />}
    </span>
  );
}
