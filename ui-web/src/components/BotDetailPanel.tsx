import { useState } from 'react';
import useSWR from 'swr';
import { Crown, Pin, X } from 'lucide-react';
import { api, type AgentBot } from '../api';
import { useUIStore } from '../store';
import { PixelAvatar, pixelAvatarRole, platformAvatarID } from './PixelAvatars';
import { BotWorkspacePanel } from './BotWorkspacePanel';
import { AlphaPixelMark } from './AlphaPixelMark';
import { revalidateBotCaches } from '../lib/botCache';
import { isBuiltinAlphaAgent } from '../lib/botInbox';

type DetailTab = 'computer' | 'advanced';


/**
 * 右侧 Bot 详情：头像 + 名字 + 两页。电脑页只保留「实时画面 + 置顶/设为主
 * Agent」这类能用的东西；模型、对话数、优化建议这些是开发视角，不摆给用户。
 */
export function BotDetailPanel() {
  const activeBotID = useUIStore((state) => state.activeBotID);
  const setActiveBotID = useUIStore((state) => state.setActiveBotID);
  const productMode = useUIStore((state) => state.productMode);
  const setActiveTab = useUIStore((state) => state.setActiveTab);
  const toast = useUIStore((state) => state.toast);
  // tab 由 store 托管：对话区右上角的「电脑」按钮要能直接切到电脑页。
  const tab = useUIStore((state) => state.botDetailTab) as DetailTab;
  const setTab = useUIStore((state) => state.setBotDetailTab) as (tab: DetailTab) => void;
  const [busyAction, setBusyAction] = useState('');

  const { data: bots = [], isLoading } = useSWR('api/agent-bots', api.listAgentBots, {
    revalidateOnFocus: false,
  });
  const { data: conversationResult } = useSWR(
    ['api/agent/conversations', productMode],
    () => api.agentHubConversations(undefined, productMode),
  );

  const bot = bots.find((item) => item.id === activeBotID);
  const conversations = (conversationResult?.conversations ?? []).filter(
    (item) => item.bot_id === activeBotID,
  );

  async function togglePin() {
    if (!bot || busyAction) return;
    setBusyAction('pin');
    try {
      await api.setAgentBotPinned(bot.id, !bot.pinned);
      await revalidateBotCaches();
      toast.show(bot.pinned ? '已取消置顶' : '已置顶', 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '置顶失败', 'error');
    } finally {
      setBusyAction('');
    }
  }

  async function promotePrimary() {
    if (!bot || busyAction || bot.is_primary) return;
    setBusyAction('primary');
    try {
      await api.setAgentBotPrimary(bot.id);
      await revalidateBotCaches();
      toast.show('已设为主 Bot', 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '设置失败', 'error');
    } finally {
      setBusyAction('');
    }
  }

  async function demotePrimary() {
    if (!bot || busyAction || !bot.is_primary) return;
    setBusyAction('primary');
    try {
      await api.clearAgentBotPrimary(bot.id);
      await revalidateBotCaches();
      toast.show('主 Agent 已交回 AlphaBot', 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '设置失败', 'error');
    } finally {
      setBusyAction('');
    }
  }

  if (!activeBotID) return null;

  // The roster is what all four tabs read from. Until it resolves, the body
  // shows the panel's shape instead of rows filled with "未选择".
  const showSkeleton = isLoading && !bot;

  return (
    <>
      {/* Under 1100px the inspector floats over the chat instead of squeezing
          it; the scrim is the way out and only exists at that width. */}
      <button
        type="button"
        data-testid="bot-detail-backdrop"
        aria-label="关闭详情"
        tabIndex={-1}
        onClick={() => setActiveBotID('')}
        className="absolute inset-0 z-20 hidden bg-black/25 max-[1100px]:block"
      />
      <aside
        data-testid="bot-detail-panel"
        aria-label="Bot 详情"
        aria-busy={showSkeleton || undefined}
        className="flex w-[340px] shrink-0 flex-col border-l border-brand-border bg-brand-surface text-brand-fg max-[1100px]:absolute max-[1100px]:inset-y-0 max-[1100px]:right-0 max-[1100px]:z-30 max-[1100px]:w-[86vw] max-[1100px]:max-w-[340px] max-[1100px]:shadow-[-12px_0_32px_rgba(0,0,0,0.14)]"
      >
      {showSkeleton ? (
        <div className="min-h-0 flex-1 overflow-y-auto p-4">
          <DetailSkeleton />
        </div>
      ) : tab === 'advanced' ? (
        <>
          <div className="flex items-center justify-between px-2 pt-2">
            <button
              type="button"
              data-testid="bot-detail-back-computer"
              onClick={() => setTab('computer')}
              className="flex h-7 items-center rounded-md px-2 text-[11px] font-semibold text-brand-muted transition hover:bg-brand-fg/[0.05] hover:text-brand-fg"
            >
              ← 电脑
            </button>
            <button
              type="button"
              data-testid="bot-detail-close"
              onClick={() => setActiveBotID('')}
              className="flex h-7 w-7 items-center justify-center rounded-md text-brand-muted transition hover:bg-brand-fg/[0.05] hover:text-brand-fg"
              aria-label="关闭详情"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
          <div className="flex flex-col items-center px-5 pb-3">
            <BotBlobAvatar bot={bot} />
            <h3 data-testid="bot-detail-name" className="mt-2 text-base font-bold">
              {bot?.name || '未选择 Bot'}
            </h3>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-4">
            <div className="space-y-2.5" data-testid="bot-detail-advanced">
              <DetailRow label="指令" value={bot?.instructions ? `${bot.instructions.slice(0, 160)}${bot.instructions.length > 160 ? '…' : ''}` : '未设置'} />
              {bot ? <BotMemorySection botID={bot.id} /> : null}
              <button
                type="button"
                data-testid="bot-detail-manage"
                onClick={() => setActiveTab('settings')}
                className="mt-2 w-full rounded-lg border border-brand-border px-3 py-2 text-[11px] font-semibold text-brand-fg transition hover:bg-brand-fg/\[0.06\]"
              >
                在设置里管理这个 Bot
              </button>
            </div>
          </div>
        </>
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto p-2.5" data-testid="bot-detail-computer">
          {/* 电脑面板：直接一层 屏幕 | 浏览器 | 数据 | 文件 + 关闭，
              不再套 Bot 头像和二级 tab。设置收进面板右上角齿轮。 */}
          <BotWorkspacePanel
            bot={bot}
            conversations={conversations}
            onClose={() => setActiveBotID('')}
            onOpenAdvanced={() => setTab('advanced')}
          />
          <div className="mt-2.5 flex gap-2 px-1">
            <button
              type="button"
              data-testid="bot-detail-pin"
              disabled={!bot || Boolean(busyAction)}
              onClick={() => void togglePin()}
              aria-pressed={Boolean(bot?.pinned)}
              className="flex flex-1 items-center justify-center gap-1 rounded-lg bg-brand-surface-raised px-2 py-2 text-[11px] font-semibold text-brand-fg transition hover:brightness-95 disabled:opacity-50"
            >
              <Pin className="h-3 w-3" />
              {bot?.pinned ? '取消置顶' : '置顶'}
            </button>
            <button
              type="button"
              data-testid="bot-detail-primary"
              disabled={!bot || Boolean(busyAction)}
              onClick={() => void (bot?.is_primary ? demotePrimary() : promotePrimary())}
              title={bot?.is_primary ? '把主 Agent 位交回内置的 AlphaBot' : '把这个 Bot 顶到主 Agent 位'}
              className="flex flex-1 items-center justify-center gap-1 rounded-lg bg-brand-surface-raised px-2 py-2 text-[11px] font-semibold text-brand-fg transition hover:brightness-95 disabled:opacity-50"
            >
              <Crown className="h-3 w-3" />
              {bot?.is_primary ? '改回 AlphaBot' : '设为主 Agent'}
            </button>
          </div>
        </div>
      )}
      </aside>
    </>
  );
}

/** Placeholder blocks that match the real rows, so nothing shifts on load. */
function DetailSkeleton() {
  return (
    <div className="space-y-2" data-testid="bot-detail-skeleton" aria-label="正在读取 Bot 详情">
      <div className="rounded-xl bg-brand-surface-raised p-4">
        <div className="mx-auto h-10 w-10 animate-pulse rounded-full bg-brand-fg/[0.06]" />
        <div className="mx-auto mt-2 h-2.5 w-16 animate-pulse rounded-full bg-brand-fg/[0.07]" />
        <div className="mx-auto mt-2 h-2 w-24 animate-pulse rounded-full bg-brand-fg/[0.04]" />
      </div>
      {[0, 1, 2].map((row) => (
        <div key={row} className="rounded-lg bg-brand-surface-raised px-3 py-2">
          <div className="h-2 w-10 animate-pulse rounded-full bg-brand-fg/[0.05]" />
          <div className="mt-1.5 h-2.5 w-2/3 animate-pulse rounded-full bg-brand-fg/[0.07]" />
        </div>
      ))}
    </div>
  );
}

function DetailRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="rounded-lg bg-brand-surface-raised px-3 py-2">
      <p className="text-[10px] text-brand-muted">{label}</p>
      <p className={`mt-0.5 break-words text-[11px] leading-4 text-brand-fg ${mono ? 'font-mono' : ''}`}>
        {value}
      </p>
    </div>
  );
}

/** Editable Markdown memory for one persona. The file is the source of truth:
 *  the textarea is a draft until 保存 writes it back through the local API. */
function BotMemorySection({ botID }: { botID: string }) {
  const { data, isLoading, mutate } = useSWR(
    ['api/agent-bots/memory', botID],
    () => api.getBotMemory(botID),
    { revalidateOnFocus: false },
  );
  const [draft, setDraft] = useState<string | null>(null);
  const [status, setStatus] = useState('');
  const [saving, setSaving] = useState(false);
  const value = draft ?? data ?? '';

  async function save() {
    setSaving(true);
    setStatus('');
    try {
      const saved = await api.updateBotMemory(botID, value);
      setDraft(null);
      await mutate(saved, false);
      setStatus('已保存');
    } catch {
      // The draft stays in the textarea so the user can retry without retyping.
      setStatus('保存失败');
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="rounded-lg bg-brand-surface-raised px-3 py-2" data-testid="bot-detail-memory">
      <div className="flex items-center justify-between">
        <p className="text-[10px] text-brand-muted">Bot 记忆</p>
        <span className="text-[10px] text-brand-muted">{status || (isLoading ? '读取中…' : '')}</span>
      </div>
      <textarea
        data-testid="bot-memory-input"
        value={value}
        onChange={(event) => {
          setDraft(event.target.value);
          setStatus('');
        }}
        spellCheck={false}
        placeholder={'# Bot 记忆\n\n## 用户偏好\n- …'}
        className="mt-1 h-40 w-full resize-y rounded-md border border-brand-border bg-brand-bg px-2 py-1.5 font-mono text-[10px] leading-4 text-brand-fg outline-none focus:border-brand-fg/30"
      />
      <div className="mt-1 flex items-center gap-2">
        <button
          type="button"
          data-testid="bot-memory-save"
          onClick={save}
          disabled={saving || draft === null}
          className="h-7 rounded-md bg-brand-fg/\[0.08\] px-2.5 text-[10px] font-semibold text-brand-fg transition hover:bg-brand-fg/\[0.14\] disabled:opacity-40"
        >
          {saving ? '保存中…' : '保存'}
        </button>
        <button
          type="button"
          data-testid="bot-memory-reset"
          onClick={() => {
            setDraft(null);
            setStatus('');
          }}
          disabled={draft === null}
          className="h-7 rounded-md px-2 text-[10px] text-brand-muted transition hover:text-brand-fg disabled:opacity-40"
        >
          撤销修改
        </button>
      </div>
    </div>
  );
}

/** Orange blob mark from the reference: rounded star with two vertical slots. */
function BotBlobAvatar({ bot }: { bot?: AgentBot }) {
  // 内置主 Agent 的四处头像统一成品牌像素标（大卡 / 收件箱 / 对话区已如此），
  // 详情面板不能再画橙色团块或首字母，否则同一主 Agent 会「换脸」。
  if (isBuiltinAlphaAgent(bot)) {
    return <AlphaPixelMark className="h-16 w-16" decorative />;
  }
  const role = pixelAvatarRole(bot?.avatar);
  const platform = platformAvatarID(bot?.avatar);
  if (platform) {
    return (
      <span className="flex h-16 w-16 items-center justify-center rounded-[22%] bg-brand-surface-raised">
        <img src={bot?.avatar} alt="" className="h-11 w-11 object-contain" />
      </span>
    );
  }
  if (role) {
    return <PixelAvatar role={role} size={64} className="rounded-[22%]" />;
  }
  return (
    <span className="flex h-16 w-16 items-center justify-center rounded-[22%]" aria-hidden="true">
      <svg viewBox="0 0 64 64" className="h-16 w-16">
        <path
          d="M32 4c4.2 0 7.2 2.4 9.4 5.4 2 2.7 5 3.6 8.4 3 3.9-.7 7.5 1.3 9.1 4.9 1.6 3.5.6 6.9-1.3 9.8-1.7 2.6-1.4 5.8.6 8.4 2.4 3.1 2.7 6.6.6 9.7-2.1 3.1-5.6 4.2-9.5 3.6-3.3-.5-6.2 1-8 3.9-2 3.1-5 4.9-9.3 4.9s-7.3-1.8-9.3-4.9c-1.8-2.9-4.7-4.4-8-3.9-3.9.6-7.4-.5-9.5-3.6-2.1-3.1-1.8-6.6.6-9.7 2-2.6 2.3-5.8.6-8.4-1.9-2.9-2.9-6.3-1.3-9.8 1.6-3.6 5.2-5.6 9.1-4.9 3.4.6 6.4-.3 8.4-3C24.8 6.4 27.8 4 32 4Z"
          fill="#ff6a2b"
        />
        <rect x="24.5" y="24" width="4.5" height="16" rx="2.25" fill="#0a0a0a" />
        <rect x="35" y="24" width="4.5" height="16" rx="2.25" fill="#0a0a0a" />
      </svg>
    </span>
  );
}
