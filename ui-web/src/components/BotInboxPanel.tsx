import { useState } from 'react';
import useSWR from 'swr';
import { Inbox, Loader2, X } from 'lucide-react';
import { api } from '../api';
import { useUIStore } from '../store';
import {
  botOpenFailure,
  buildInboxRows,
  inboxTimeLabel,
  inboxUnreadTotal,
  openBotThread,
  openMainAgentThread,
  type BotInboxRow,
  type BotThreadOpener,
} from '../lib/botInbox';
import { AlphaPixelMark } from './AlphaPixelMark';
import { BotAvatar } from './BotRail';

/**
 * Bot 页右侧的收件箱面板。
 *
 * 收件箱是独立右栏：
 * 侧栏的「收件箱」只切换面板，列表占用右侧栏而不是侧栏内联，团队列表原样保留。
 * 数据仍然只来自 `api/agent-bots/rail` 一个聚合请求，主 Agent 的未读用它自己的行。
 */
export function BotInboxPanel() {
  const setBotInboxOpen = useUIStore((state) => state.setBotInboxOpen);
  const productMode = useUIStore((state) => state.productMode);
  const requestAgentConversation = useUIStore((state) => state.requestAgentConversation);
  const setActiveBotID = useUIStore((state) => state.setActiveBotID);
  const toast = useUIStore((state) => state.toast);
  const [openingKey, setOpeningKey] = useState('');

  const { data: rail, mutate: refreshRail } = useSWR('api/agent-bots/rail', api.agentBotRail);
  const { data: conversationResult, mutate: refreshConversations } = useSWR(
    ['api/agent/conversations', productMode],
    () => api.agentHubConversations(undefined, productMode),
  );

  const rows = buildInboxRows(rail);
  const unreadTotal = inboxUnreadTotal(rail);

  const threadDeps: BotThreadOpener = {
    productMode,
    conversations: conversationResult?.conversations ?? [],
    requestAgentConversation,
    setActiveBotID,
    createConversation: api.agentHubConversationCreate,
    markRead: api.markAgentHubConversationRead,
    markAllRead: api.markAgentHubConversationsReadAll,
    refreshConversations,
    refreshRail,
  };

  async function openRow(row: BotInboxRow) {
    if (openingKey) return;
    setOpeningKey(row.key);
    try {
      if (row.key === 'main-agent') {
        // 历史无归属会话（旧 Alpha Agent 线程）：打开并清掉它们的未读。
        await openMainAgentThread(threadDeps);
      } else if (row.bot) {
        await openBotThread(row.bot, threadDeps);
      } else {
        await openMainAgentThread(threadDeps);
      }
      // 打开即离开收件箱：右栏回到这个 Bot 的详情，团队列表一直在左侧。
      setBotInboxOpen(false);
    } catch (reason) {
      toast.show(botOpenFailure(reason).message, 'error');
    } finally {
      setOpeningKey('');
    }
  }

  return (
    <>
      {/* 窄屏下浮在对话上，scrim 是退出路径；和 BotDetailPanel 用同一套断点。 */}
      <button
        type="button"
        data-testid="bot-inbox-backdrop"
        aria-label="关闭收件箱"
        tabIndex={-1}
        onClick={() => setBotInboxOpen(false)}
        className="absolute inset-0 z-20 hidden bg-black/25 max-[1100px]:block"
      />
      <aside
        data-testid="bot-inbox-panel"
        aria-label="收件箱"
        className="flex w-[340px] shrink-0 flex-col border-l border-brand-border bg-brand-surface text-brand-fg max-[1100px]:absolute max-[1100px]:inset-y-0 max-[1100px]:right-0 max-[1100px]:z-30 max-[1100px]:w-[86vw] max-[1100px]:max-w-[340px] max-[1100px]:shadow-[-12px_0_32px_rgba(0,0,0,0.14)]"
      >
        <div className="flex items-center gap-2 px-3 pb-2 pt-3">
          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-brand-surface-raised">
            <Inbox className="h-4 w-4" aria-hidden="true" />
          </span>
          <h3 data-testid="bot-inbox-title" className="min-w-0 flex-1 truncate text-[15px] font-semibold">
            收件箱
          </h3>
          <span
            data-testid="bot-inbox-total"
            title={`${unreadTotal} 条未读`}
            className="shrink-0 rounded-full bg-brand-surface-raised px-2 py-0.5 text-[11px] font-semibold tabular-nums text-brand-muted"
          >
            {unreadTotal} 条未读
          </span>
          <button
            type="button"
            data-testid="bot-inbox-close"
            aria-label="关闭收件箱"
            title="关闭收件箱"
            onClick={() => setBotInboxOpen(false)}
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-brand-muted transition hover:bg-brand-fg/[0.05] hover:text-brand-fg"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto ui-thin-scrollbar px-2 pb-3">
          {rows.length === 0 ? (
            <p data-testid="bot-inbox-empty" className="px-2 py-8 text-center text-[12px] leading-5 text-brand-muted">
              没有未读消息
            </p>
          ) : (
            <div className="space-y-1">
              {rows.map((row) => (
                <div
                  key={row.key}
                  role="button"
                  tabIndex={0}
                  data-testid={`bot-inbox-row-${row.key}`}
                  aria-disabled={Boolean(openingKey) || undefined}
                  title={`${row.name} · ${row.group} · ${row.unread} 条未读`}
                  onClick={() => void openRow(row)}
                  onKeyDown={(event) => {
                    if (event.target !== event.currentTarget) return;
                    if (event.key !== 'Enter' && event.key !== ' ') return;
                    event.preventDefault();
                    void openRow(row);
                  }}
                  className={`flex w-full cursor-pointer items-center gap-2.5 rounded-xl px-2 py-2.5 text-left transition-colors hover:bg-brand-fg/[0.035] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/40 ${
                    openingKey ? 'opacity-60' : ''
                  }`}
                >
                  <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-brand-surface-raised">
                    {row.bot ? <BotAvatar bot={row.bot} size="md" /> : <AlphaPixelMark className="h-7 w-7" decorative />}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-baseline gap-1.5">
                      <span className="min-w-0 truncate text-[14px] font-semibold leading-5 text-brand-fg">{row.name}</span>
                      <span className="min-w-0 shrink truncate text-[11px] text-brand-muted">{row.group}</span>
                      <span className="ms-auto shrink-0 text-[11px] tabular-nums text-brand-muted">{inboxTimeLabel(row.at)}</span>
                    </span>
                    <span className="mt-0.5 flex items-center gap-1.5">
                      <span className="min-w-0 flex-1 truncate text-[12px] leading-4 text-brand-muted">{row.preview}</span>
                      <span
                        data-testid={`bot-inbox-unread-${row.key}`}
                        title={`${row.unread} 条未读`}
                        className="flex h-[18px] min-w-[18px] shrink-0 items-center justify-center rounded-full bg-brand-accent px-[5px] text-[10px] font-bold leading-none text-brand-on-accent"
                      >
                        {row.unread}
                      </span>
                      {openingKey === row.key ? (
                        <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-brand-accent" />
                      ) : null}
                    </span>
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </aside>
    </>
  );
}
