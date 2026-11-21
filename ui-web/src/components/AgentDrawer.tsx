import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { AlertTriangle, Check, CheckCircle2, ChevronDown, Download, FolderOpen, GitFork, History, Monitor, ShieldCheck, X } from 'lucide-react';
import useSWR from 'swr';
import {
  api,
  streamAgentHubEvents,
  type AgentBot,
  type AgentHubContext,
  type AgentHubConversation,
  type AgentHubEvent,
  type AgentHubProviderID,
  type AgentHubRun,
  type AgentHubToolCall,
  type AgentToolCallDetail,
  type AgentKnowledgeTurn,
  type KnowledgeScope,
  type KnowledgeScopeOption,
  type SkillLibraryItem,
  type TrendRadarSignal,
} from '../api';
import {
  AGENT_PANEL_MAX_WIDTH,
  AGENT_PANEL_MIN_WIDTH,
  clampAgentDrawerWidth,
  preferredAgentProvider,
  useUIStore,
} from '../store';
import { openAgentContext } from './AgentContextDrawer';
import { LocalLoadingState } from './LocalLoadingState';
import { PixelAvatar, pixelAvatarRole, platformAvatarID } from './PixelAvatars';
import { AlphaPixelMark } from './AlphaPixelMark';
import { ChatTranscript, Composer, WorkingTimer } from '../vendor/openmausbot';
import { toBot, toMessages, toTranscript, type Message as BridgeMessage } from '../lib/botBridge';
import { BUILTIN_ALPHA_AGENT_ID, isBuiltinAlphaAgent } from '../lib/botInbox';
import { botMentionSuggestions, parseBotDelegation, runningDelegations } from '../lib/botDelegation';

const DEFAULT_PROVIDER: AgentHubProviderID = 'dsh';
const AGENT_PROVIDER_POLL_DELAYS = [500, 1000, 2000];
const PAGE_LABELS: Record<string, string> = {
  accounts: '浏览器管理',
  'social-media': '自媒体',
  'ai-image': 'AI 生图',
  skills: 'Skill 库',
  dashboard: '趋势雷达',
  publish: '一键发布',
  settings: '设置',
};

function ProviderMark({ className = 'h-8 w-8', decorative = false }: { className?: string; decorative?: boolean }) {
  return <AlphaPixelMark className={className} decorative={decorative} />;
}

/**
 * 当前 Bot 的身份头像。顺序：平台 logo → 自定义图 → 职责像素头像 → emoji → 字母兜底。
 * 字母兜底刻意不用像素风：没有选中 Bot 时显示干净的灰底首字母。
 */
function AgentIdentityAvatar({
  bot,
  size = 28,
  className = '',
}: {
  bot?: { id?: string; name: string; avatar?: string } | null;
  size?: number;
  className?: string;
}) {
  // 内置主 Agent 没有 avatar 字段：与侧栏大卡、收件箱、详情面板统一用品牌像素标，
  // 而不是退化成首字母「A」。size 是运行时数字，用内联尺寸固定像素标。
  if (isBuiltinAlphaAgent(bot)) {
    return (
      <span className={`inline-flex shrink-0 ${className}`} style={{ height: size, width: size }}>
        <AlphaPixelMark className="h-full w-full" decorative />
      </span>
    );
  }
  const avatar = (bot?.avatar ?? '').trim();
  const frame = `flex shrink-0 items-center justify-center overflow-hidden rounded-[22%] ${className}`;
  const style = { height: size, width: size };

  const platform = platformAvatarID(avatar);
  if (platform) {
    return (
      <span className={`${frame} bg-brand-surface-raised`} style={style}>
        <img src={avatar} alt="" className="h-full w-full object-contain p-1" />
      </span>
    );
  }
  if (avatar.startsWith('/api/')) {
    return (
      <span className={`${frame} bg-brand-surface-raised`} style={style}>
        <img src={avatar} alt="" className="h-full w-full object-cover [image-rendering:pixelated]" />
      </span>
    );
  }
  const role = pixelAvatarRole(avatar);
  if (role) return <PixelAvatar role={role} size={size} className={`rounded-[22%] ${className}`} />;
  if (avatar) {
    return (
      <span className={`${frame} bg-brand-surface-raised text-brand-muted`} style={{ ...style, fontSize: size * 0.5 }}>
        {avatar}
      </span>
    );
  }
  const initial = (bot?.name ?? 'A').trim().slice(0, 1).toUpperCase() || 'A';
  return (
    <span
      className={`${frame} bg-brand-surface-raised font-semibold text-brand-muted`}
      style={{ ...style, fontSize: Math.round(size * 0.42) }}
      aria-hidden="true"
    >
      {initial}
    </span>
  );
}

function providerModels(_providerID: AgentHubProviderID, models: string[] = []): string[] {
  return [...new Set(models)];
}

function scopeKey(scope: KnowledgeScope): string {
  return `${scope.base_id}:${scope.source_prefix || ''}`;
}

function trendFolderKey(signal: TrendRadarSignal): string {
  return signal.platform || '未分类';
}

function trendFolderSignals(signals: TrendRadarSignal[]): Array<{ label: string; items: TrendRadarSignal[] }> {
  const folders = new Map<string, TrendRadarSignal[]>();
  for (const signal of signals) {
    const key = trendFolderKey(signal);
    folders.set(key, [...(folders.get(key) || []), signal]);
  }
  return [...folders.entries()].map(([label, items]) => ({ label, items }));
}

// 下拉要回答的是「这个模型背后哪个编程工具在跑」：标题写工具名，
// 有品牌图就上图，没有就用中性文字徽标或像素标，不借用模型厂商的品牌。
type ProviderDisplay = {
  name: string;
  src?: string;
  alt?: string;
  badge?: string;
};

const PROVIDER_DISPLAY: Partial<Record<AgentHubProviderID, ProviderDisplay>> = {
  codex: { name: 'Codex', src: '/brand/codex-logo.png', alt: 'Codex' },
  antigravity: { name: 'Antigravity · agy', src: '/brand/antigravity-logo.png', alt: 'Antigravity (agy)' },
  claude: { name: 'Claude Code', src: '/agent-providers/claude-code.ico', alt: 'Claude Code' },
  kimi: { name: 'Kimi Code', src: '/agent-providers/kimi-code.png', alt: 'Kimi Code' },
  codebuddy: { name: 'CodeBuddy Code', src: '/agent-providers/codebuddy.svg', alt: 'CodeBuddy Code' },
  opencode: { name: 'NEXT · DeepSeek', src: '/agent-providers/deepseek-logo.svg', alt: 'DeepSeek' },
  custom: { name: 'next', src: '/agent-providers/deepseek-logo.svg', alt: 'next 供应商' },
  dsh: { name: 'DSH', badge: 'D' },
  alpha: { name: 'Alpha Agent', badge: 'A' },
};

function providerDisplay(providerID: AgentHubProviderID): ProviderDisplay {
  return PROVIDER_DISPLAY[providerID] ?? { name: providerID.toUpperCase() };
}

function ProviderLogo({ providerID, className }: { providerID: AgentHubProviderID; className?: string }) {
  const display = providerDisplay(providerID);
  if (display.src) {
    return <img src={display.src} alt={display.alt || display.name} className={`shrink-0 object-contain ${className ?? ''}`} />;
  }
  if (display.badge) {
    return (
      <span
        aria-hidden="true"
        className="inline-flex h-3.5 shrink-0 items-center justify-center rounded-[3px] bg-brand-surface-raised px-1 text-[8px] font-bold uppercase tracking-wide text-brand-muted"
      >
        {display.badge}
      </span>
    );
  }
  return <ProviderMark decorative className={className} />;
}

function storedProviderID(): AgentHubProviderID | '' {
  const value = preferredAgentProvider();
  return value && /^[a-z0-9][a-z0-9_-]{0,63}$/.test(value) ? value : '';
}

type RunSummary = {
  status: 'completed' | 'failed' | 'cancelled' | 'interrupted';
  elapsedMS: number;
  errorCode?: string;
};


function runErrorMessage(code?: string): string {
  const messages: Record<string, string> = {
    AGENT_PROVIDER_ALPHA_FAILED: '旧版 Alpha Agent 运行时不可用；请新建 Codex 会话。',
    AGENT_PROVIDER_CODEX_FAILED: 'Codex 本次运行失败，请检查设置后重试。',
    AGENT_PROVIDER_OPENCODE_FAILED: 'Alpha Agent 本次运行失败，请检查设置后重试。',
    AGENT_PROVIDER_ANTIGRAVITY_FAILED: 'Alpha Agent 本次运行失败，请检查设置后重试。',
    AGENT_PROVIDER_CLAUDE_FAILED: 'Alpha Agent 本次运行失败，请检查设置后重试。',
    AGENT_PROVIDER_UNAVAILABLE: 'Alpha Agent 当前不可用，请先在设置中完成连接。',
    AGENT_PROVIDER_TIMEOUT: '本次运行超时，已安全停止；可以检查设置后重试。',
    AGENT_EVENT_STREAM_INTERRUPTED: '状态连接已中断，本次回答可能没有完整返回。',
    // 同会话只允许一个前台 run；跨会话可并行，只有超过全局上限才拒绝。
    AGENT_RUN_ACTIVE: '这个对话正在运行；等它结束后再发送这一轮。',
    AGENT_RUN_CAPACITY: '同时进行的对话有点多，先等一两个结束再发。',
    AGENT_MODEL_NOT_VERIFIED: '这个对话绑定的模型还没通过真实调用验证，请到设置里验证后重试。',
    AGENT_PROVIDER_DSH_TURN: 'DSH 运行时返回了错误；如果是 403，说明上游账号或模型不可用。',
    AGENT_MESSAGE_REQUIRED: '消息不能为空。',
    AGENT_MESSAGE_TOO_LONG: '消息太长（上限 16000 字符），请拆分后再发。',
  };
  return messages[code || ''] || '本次运行没有完成，可以直接重试。';
}

function runSummaryMessage(summary: RunSummary): string {
  if (summary.status === 'cancelled') return '已保留停止前收到的内容，可以重新发送这一轮。';
  return runErrorMessage(summary.errorCode);
}

function toolStatusLabel(status: AgentHubToolCall['status']): string {
  const labels: Record<AgentHubToolCall['status'], string> = {
    proposed: '已提议',
    pending: '等待批准',
    approved: '已批准',
    rejected: '已拒绝',
    running: '执行中',
    completed: '已完成',
    failed: '执行失败',
    expired: '已过期',
    cancelled: '已取消',
  };
  return labels[status] || status;
}

// Markdown punctuation is noise in a one-line preview.
function plainPreview(text: string, limit = 120): string {
  return text
    .replace(/[*#>`~|]/g, ' ')
    .replace(/-{3,}|_{2,}/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, limit);
}

// Tool cards take their truth from the database timeline and let SSE events
// override the live status. Without the merge, refreshing the drawer would
// forget every tool the run already used.
function mergeToolCards(history: AgentToolCallDetail[], live: AgentHubToolCall[]): AgentToolCallDetail[] {
  const byID = new Map<string, AgentToolCallDetail>();
  for (const item of history) byID.set(item.id, item);
  for (const item of live) {
    const existing = byID.get(item.id);
    byID.set(item.id, {
      id: item.id,
      tool_name: item.tool_name,
      workspace: item.workspace || existing?.workspace,
      risk_level: item.risk_level || existing?.risk_level,
      status: item.status,
      impact_preview: item.impact_preview || existing?.impact_preview,
      params: existing?.params,
      result: existing?.result ?? item.result,
      error_code: item.error_code || existing?.error_code,
      created_at: existing?.created_at || item.created_at,
      updated_at: item.updated_at || existing?.updated_at || '',
      duration_ms: existing?.duration_ms ?? 0,
    });
  }
  return Array.from(byID.values())
    .sort((left, right) => {
      const leftMs = Date.parse(left.created_at);
      const rightMs = Date.parse(right.created_at);
      if (Number.isNaN(leftMs) || Number.isNaN(rightMs)) return 0;
      return rightMs - leftMs;
    })
    .slice(0, 20);
}

function formatToolDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '—';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  return `${Math.floor(seconds / 60)}m${String(Math.round(seconds % 60)).padStart(2, '0')}s`;
}

function formatToolPayload(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

// The next step a person can actually take after a failed tool.
function toolErrorHint(code: string): string {
  const hints: Record<string, string> = {
    AGENT_TOOL_FAILED: '工具执行失败，可以直接重试这一轮。',
    AGENT_APPROVAL_EXPIRED: '确认已过期，重新发起一次即可。',
    TIKHUB_RATE_LIMIT: '接口限流，等几分钟再试，不会自动重试。',
    AGENT_PROVIDER_TIMEOUT: '本次运行超时，已安全停止。',
  };
  return hints[code] || '看下面的错误详情；修好后重新发这一轮。';
}


export function AgentDrawer({ displayMode = 'drawer' }: { displayMode?: 'drawer' | 'stage' }) {
  const isStage = displayMode === 'stage';
  const {
    activeTab,
    productMode,
    agentDraft,
    requestedAgentConversationID,
    requestAgentConversation,
    agentDrawerOpen,
    agentDrawerWidth,
    clearAgentDraft,
    closeAgentDrawer,
    setAgentDrawerWidth,
    selectedAccountIds,
    searchQuery,
  } = useUIStore();
  const agentVisible = displayMode === 'stage' || agentDrawerOpen;
  const { data: providers, mutate: mutateProviders } = useSWR('api/agent/providers', () => api.agentHubProviders());
  const { data: knowledgeScopeOptions = [] } = useSWR<KnowledgeScopeOption[]>(
    agentVisible ? 'api/knowledge/scopes' : null,
    api.getKnowledgeScopes,
  );
  const { data: trendSummary } = useSWR(
    agentVisible ? 'api/trend-radar/summary' : null,
    api.getTrendRadarSummary,
  );
  const [modelMenuOpen, setModelMenuOpen] = useState(false);
  const modelTriggerRef = useRef<HTMLButtonElement>(null);
  // 会话选择条已移除；列表状态只用于恢复/切换后同步模型选择。
  const [, setConversations] = useState<AgentHubConversation[]>([]);
  const [conversation, setConversation] = useState<AgentHubConversation | null>(null);
  const [providerID, setProviderID] = useState<AgentHubProviderID>(() => storedProviderID() || DEFAULT_PROVIDER);
  const [modelID, setModelID] = useState('');
  const [input, setInput] = useState('');
  const [streamingText, setStreamingText] = useState('');
  const [activeRun, setActiveRun] = useState<AgentHubRun | null>(null);
  const [isStartingRun, setIsStartingRun] = useState(false);
  const [isCancellingRun, setIsCancellingRun] = useState(false);
  const [runStartedAt, setRunStartedAt] = useState(0);
  const [lastRunSummary, setLastRunSummary] = useState<RunSummary | null>(null);
  const [lastSubmittedMessage, setLastSubmittedMessage] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [skills, setSkills] = useState<SkillLibraryItem[]>([]);
  const [selectedKnowledgeScopes, setSelectedKnowledgeScopes] = useState<KnowledgeScope[]>([]);
  const [selectedTrendLabel, setSelectedTrendLabel] = useState('');
  const [selectedTrendContext, setSelectedTrendContext] = useState('');
  const [knowledgeTurns, setKnowledgeTurns] = useState<AgentKnowledgeTurn[]>([]);
  const [pendingApprovals, setPendingApprovals] = useState<AgentHubToolCall[]>([]);
  const [toolTimeline, setToolTimeline] = useState<AgentHubToolCall[]>([]);
  const [resolvingApprovalID, setResolvingApprovalID] = useState('');
  const [rememberApproval, setRememberApproval] = useState<Record<string, boolean>>({});
  const [isBootstrapping, setIsBootstrapping] = useState(false);
  const [windowWidth, setWindowWidth] = useState(() => typeof window === 'undefined' ? 1440 : window.innerWidth);
  const sourceRef = useRef<AbortController | null>(null);
  const messagesScrollRef = useRef<HTMLDivElement>(null);
  const closingRef = useRef(!agentVisible);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const agentDraftSequenceRef = useRef(0);
  // Bot 列表的显式选择优先于较早发起的 bootstrap，避免慢请求把聊天切回旧会话/旧模型。
  const conversationSelectionRevisionRef = useRef(0);
  const visibleDrawerWidth = clampAgentDrawerWidth(agentDrawerWidth, windowWidth);
  const drawerMaxWidth = clampAgentDrawerWidth(AGENT_PANEL_MAX_WIDTH, windowWidth);

  const trendFolders = useMemo(() => trendFolderSignals([
    ...(trendSummary?.signals || []),
    ...(trendSummary?.video_lookups || []),
  ].slice(0, 24)), [trendSummary?.signals, trendSummary?.video_lookups]);

  const context = useMemo<AgentHubContext>(() => {
    const filters: Record<string, string> = {};
    if (searchQuery) filters.search = searchQuery;
    const selections = selectedAccountIds
      .map((id) => ({ id: `account_${id}`, name: `账号 #${id}` }));
    return {
      workspace: 'Alpha-Nexus',
      product_mode: productMode,
      page: PAGE_LABELS[activeTab] || activeTab,
      filters,
      selections,
      attachments: selectedTrendLabel ? [`趋势：${selectedTrendLabel}`] : undefined,
    };
  }, [activeTab, productMode, searchQuery, selectedAccountIds, selectedTrendLabel]);

  // SWR may refresh the shared Provider snapshot while the Drawer remains
  // mounted; this tiny derivation must always read the latest model list.
  const activeProvider = providers?.providers.find((item) => item.id === providerID);
  // Provider identity travels through its logo; the harness name stays out.
  const { data: botRoster } = useSWR(
    'api/agent-bots',
    api.listAgentBots,
    { revalidateOnFocus: false },
  );
  // Bot 之间的交接是后端真实运行：这一条只负责显示「谁在替我做」，终态由
  // 会话消息本身呈现（后端会把结果写回父会话）。
  const { data: delegationResult, mutate: refreshDelegations } = useSWR(
    conversation?.id ? ['api/agent/delegations', conversation.id] : null,
    () => api.listAgentDelegations(conversation!.id),
    {
      // 有交接在跑才 2 秒轮询；空闲时降到 15 秒，别让面板一直发请求。
      refreshInterval: (data) =>
        (data?.delegations ?? []).some((item) => item.status === 'running') ? 2000 : 15000,
      refreshWhenHidden: false,
      shouldRetryOnError: true,
      errorRetryInterval: 3000,
    },
  );
  const activeDelegations = runningDelegations(delegationResult?.delegations);
  const botNameByID = useMemo(
    () => new Map((botRoster ?? []).map((bot) => [bot.id, bot.name] as const)),
    [botRoster],
  );
  const botByID = useMemo(
    () => new Map((botRoster ?? []).map((bot) => [bot.id, bot] as const)),
    [botRoster],
  );
  const activeBotID = useUIStore((state) => state.activeBotID);
  // 会话优先；还没有会话时跟随 Bot 列表里选中的那个，这样点头像就能看到正确身份，
  // 而不是停在上一段会话的 Alpha Agent。
  // 会话绑定了 Bot 就听会话的；否则（没有会话，或会话还没绑 Bot——比如刚点 Bot
  // 但建会话被模型验证挡住）跟随侧栏选中的那个，别退回「Alpha Agent」。
  const currentBot =
    (botRoster ?? []).find((item) => item.id === conversation?.bot_id)
    ?? (!conversation?.bot_id ? (botRoster ?? []).find((item) => item.id === activeBotID) : undefined);
  const providerLabel = activeProvider?.label || 'Alpha Agent';
  const isCodexProvider = providerID === 'codex';
  const probe = providers?.probes.find((item) => item.provider_id === providerID);
  const availableModels = providerModels(providerID, probe?.models);
  // Only real-call verified models enter the switcher; candidates live in
  // Settings, where the user explicitly validates them.
  // 每个 coding agent（DSH / Alpha / OpenCode / Codex / Antigravity / next …）都保留
  // 自己的分组：没有已验证模型时显示「未验证」，而不是整组消失——否则用户根本
  // 看不到 Codex、Antigravity 这些 agent 的存在。
  const probeByID = useMemo(
    () => new Map((providers?.probes || []).map((probe) => [probe.provider_id, probe] as const)),
    [providers],
  );
  const modelGroups = useMemo(() => (providers?.providers || [])
    .filter((provider) => provider.id === 'opencode')
    .map((provider) => ({
      id: provider.id,
      models: providerModels(provider.id, providers?.probes.find((item) => item.provider_id === provider.id)?.models),
    })), [providers]);
  const selectedModelID = modelID.trim();
  // 模型全名带供应商前缀，胶囊里只显示最后一段；完整 id 留在 title 里。
  const selectedModelLabel = selectedModelID
    ? (selectedModelID.split('/').pop() || selectedModelID)
    : '';
  // 所有 CLI 都只允许选择已验证的真实模型；候选目录和 CLI
  // 隐式默认值都不能证明当前账号真的能调用该模型。
  const missingVerifiedModel = !selectedModelID;
  const selectedModelUnavailable = Boolean(selectedModelID) && !availableModels.includes(selectedModelID);
  const modelSelectionUnavailable = missingVerifiedModel || selectedModelUnavailable;
  const workspaceSelection = { workspace_mode: productMode, last_module: activeTab } as const;
  const pendingProviderSignature = (providers?.probes || [])
    .filter((probe) => probe.state === 'probing' || Boolean(probe.validating_model))
    .map((probe) => `${probe.provider_id}:${probe.state}:${probe.validating_model || ''}`)
    .join('|');
  const mentionToken = input.match(/(?:^|\s)@([^\s@]*)$/);
  const mentionQuery = mentionToken?.[1].trim().toLowerCase() || '';
  const skillToken = input.match(/(?:^|\s)\/([a-z0-9-]*)$/);
  const skillSuggestions = skillToken
    ? skills.filter((skill) => skill.enabled && skill.id.startsWith(skillToken[1])).slice(0, 6)
    : [];
  const mentionKnowledgeOptions = mentionToken
    ? knowledgeScopeOptions.filter((option) => !mentionQuery || option.label.toLowerCase().includes(mentionQuery)).slice(0, 6)
    : [];
  const mentionTrendFolders = mentionToken
    ? trendFolders.filter((folder) => !mentionQuery || folder.label.toLowerCase().includes(mentionQuery)).slice(0, 6)
    : [];
  // 只把「不是当前会话这位」的 Bot 放进候选：给自己派活没有意义。
  const mentionBots = mentionToken
    ? botMentionSuggestions(input, (botRoster ?? []).filter((bot) => bot.id !== conversation?.bot_id))
    : [];
  const creatorConversation = (conversation?.messages || []).some(
    (message) => message.role === 'user' && (message.content.includes('/skill-creator') || message.content.includes('@skill-creator')),
  );
  const knowledgeTurnsByRun = useMemo(
    () => new Map(knowledgeTurns.map((turn) => [turn.run_id, turn])),
    [knowledgeTurns],
  );
  const runBusy = isStartingRun || Boolean(activeRun);
  // 工具时间轴以数据库为准：SSE 只补运行中的状态，刷新后历史不会丢。
  const { data: toolCallHistory } = useSWR(
    conversation?.id ? ['api/agent/tool-calls', conversation.id] : null,
    () => api.agentHubConversationToolCalls(conversation!.id),
    {
      refreshInterval: runBusy ? 4000 : 0,
      refreshWhenHidden: false,
      revalidateOnFocus: false,
      shouldRetryOnError: false,
    },
  );
  const toolCards = useMemo(
    () => mergeToolCards(toolCallHistory?.tool_calls ?? [], toolTimeline),
    [toolCallHistory, toolTimeline],
  );
  // 用户手动的展开/收起优先于「按状态默认」，否则状态跃迁会把卡片抢开或抢关。
  const [toolOpenState, setToolOpenState] = useState<Record<string, boolean>>({});
  // 交接结束后保留最近 3 条结果，不立刻从面板消失。
  const recentDelegations = useMemo(
    () => (delegationResult?.delegations ?? []).filter((item) => item.status !== 'running').slice(0, 3),
    [delegationResult],
  );
  // 运行条需要一个自己的时钟：超过 1 分钟提示「仍在运行」。
  const [runClock, setRunClock] = useState(() => Date.now());
  useEffect(() => {
    setRunClock(Date.now());
    if (!runBusy) return undefined;
    const timer = window.setInterval(() => setRunClock(Date.now()), 5000);
    return () => window.clearInterval(timer);
  }, [runBusy]);
  const longRun = runStartedAt > 0 && runClock - runStartedAt > 60_000;
  // 会话历史：Bot 只自动打开最新会话，旧对话需要一个入口才回得去。
  const { data: conversationList } = useSWR(
    ['api/agent/conversations', productMode],
    () => api.agentHubConversations(undefined, productMode),
    { revalidateOnFocus: false, refreshInterval: 20_000 },
  );
  const [historyOpen, setHistoryOpen] = useState(false);
  const conversationHistory = useMemo(() => {
    const all = conversationList?.conversations ?? [];
    const scoped = currentBot
      ? all.filter((item) => item.bot_id === currentBot.id)
      : all.filter((item) => !item.bot_id);
    return scoped.slice(0, 8);
  }, [conversationList, currentBot]);
  // 消息行、日期分隔、分组气泡与进入动画由 vendor 组件负责；发送/停止仍由本组件接管。
  const ombMessages = useMemo<BridgeMessage[]>(() => {
    const rows = conversation ? toMessages(conversation) : [];
    if (streamingText) {
      rows.push({
        id: `streaming_${activeRun?.id || 'pending'}`,
        role: 'bot',
        kind: 'text',
        text: streamingText,
        at: runStartedAt || Date.now(),
        runID: activeRun?.id,
        streaming: true,
        // 流式增量保持纯文本，避免每个 token 重解析 Markdown。
        plain: true,
      });
    }
    return rows;
  }, [activeRun?.id, conversation, runStartedAt, streamingText]);
  const ombItems = useMemo(() => toTranscript(ombMessages), [ombMessages]);
  const transcriptBot = useMemo(() => currentBot ? toBot(currentBot) : {
    id: 'alpha-agent',
    name: 'Alpha Agent',
    color: 'chrome' as const,
    avatarUrl: null,
  }, [currentBot]);
  const resetRunDisplay = () => {
    setStreamingText('');
    setLastRunSummary(null);
    setLastSubmittedMessage('');
    setRunStartedAt(0);
    setError('');
    setNotice('');
  };

  const clearAttachedContext = () => {
    setSelectedKnowledgeScopes([]);
    setSelectedTrendLabel('');
    setSelectedTrendContext('');
  };

  const clearActiveMention = () => {
    setInput((current) => current.replace(/@[^@\s]*$/, ''));
    window.setTimeout(() => inputRef.current?.focus(), 0);
  };

  const toggleKnowledgeScope = (option: KnowledgeScopeOption) => {
    const scope = { base_id: option.base_id, source_prefix: option.source_prefix };
    setSelectedKnowledgeScopes((current) => current.some((item) => scopeKey(item) === scopeKey(scope))
      ? current.filter((item) => scopeKey(item) !== scopeKey(scope))
      : [...current, scope]);
    clearActiveMention();
  };

  const selectTrendFolder = (label: string) => {
    const folder = trendFolders.find((item) => item.label === label);
    const titles = folder?.items.slice(0, 6).map((item) => item.title).filter(Boolean) || [];
    setSelectedTrendLabel(label);
    setSelectedTrendContext([`趋势文件夹：${label}`, ...titles.map((title) => `- ${title}`)].join('\n'));
    clearActiveMention();
  };

  const reloadConversations = async (targetProvider: AgentHubProviderID = providerID) => {
    const result = await api.agentHubConversations(targetProvider, productMode);
    const scoped = result.conversations.filter((item) => (item.workspace_mode || item.context?.product_mode || 'media') === productMode);
    setConversations(scoped);
    window.dispatchEvent(new CustomEvent('alpha-nexus:agent-conversations-changed'));
    return scoped;
  };

  const loadConversation = async (id: string) => {
    const next = await api.agentHubConversation(id);
    // 会话是时间轴的边界：上一段会话的工具卡片、待确认和失败提示都不能跟过来。
    setToolTimeline([]);
    setPendingApprovals([]);
    setToolOpenState({});
    resetRunDisplay();
    setConversation(next);
    setProviderID(next.provider_id);
    setModelID(next.model_id === 'default' ? '' : next.model_id || '');
    try {
      const audit = await api.agentHubKnowledgeTurns(id);
      setKnowledgeTurns(audit.turns || []);
    } catch {
      setKnowledgeTurns([]);
    }
    return next;
  };

  useEffect(() => {
    // A mode switch mid-run must not clobber the conversation the run is
    // streaming into; the next mode change or manual pick re-bootstraps.
    if (!agentVisible || runBusy) return;
    let cancelled = false;
    const bootstrapDraftSequence = agentDraftSequenceRef.current;
    const bootstrapConversationSelectionRevision = conversationSelectionRevisionRef.current;
    setIsBootstrapping(true);
    const initialProvider = storedProviderID();
    void api.agentHubBootstrap(initialProvider || undefined, productMode).then(async (bootstrap) => {
      if (cancelled) return;
      await mutateProviders({ providers: bootstrap.providers, probes: bootstrap.probes }, { revalidate: false });
      // 只有这期间没有显式选会话时，才恢复全局选择。被恢复会话自己的
      // provider/model 是唯一真源，不能再被全局 selection 覆盖。
      if (conversationSelectionRevisionRef.current === bootstrapConversationSelectionRevision) {
        setProviderID(bootstrap.selection.provider_id);
        const scopedConversations = bootstrap.conversations.filter((item) => (item.workspace_mode || item.context?.product_mode || 'media') === productMode);
        setConversations(scopedConversations);
        setModelID(bootstrap.selection.model_id || '');
        if (agentDraftSequenceRef.current === bootstrapDraftSequence) {
          setInput(bootstrap.selection.draft || '');
        }
        setPendingApprovals(bootstrap.pending_approvals || []);
        setToolTimeline(bootstrap.selection.provider_id === 'codex' ? (bootstrap.pending_approvals || []) : []);
        const selected = scopedConversations.find((item) => item.id === bootstrap.selection.conversation_id)
          || scopedConversations[0];
        if (selected) await loadConversation(selected.id);
        else setConversation(null);
      }
      // Bootstrap is intentionally cache-first. Refresh Provider/model state
      // independently so a just-restored Drawer cannot keep a stale model list.
      void mutateProviders();
    }).catch((cause: unknown) => {
      if (!cancelled) setError(cause instanceof Error ? cause.message : '助手服务正在恢复');
    }).finally(() => {
      if (!cancelled) setIsBootstrapping(false);
    });
    void api.skillsList()
      .then((catalog) => { if (!cancelled) setSkills(catalog.skills); })
      .catch(() => undefined);
    window.setTimeout(() => inputRef.current?.focus(), 0);
    return () => { cancelled = true; };
  }, [agentVisible, productMode]);

	useEffect(() => {
		if (!agentVisible || !agentDraft) return;
    agentDraftSequenceRef.current += 1;
    setInput(agentDraft.message);
    setSelectedKnowledgeScopes(agentDraft.knowledgeScopes ?? []);
    setSelectedTrendLabel('');
    setSelectedTrendContext('');
    clearAgentDraft();
    window.setTimeout(() => inputRef.current?.focus(), 0);
	}, [agentVisible, agentDraft, clearAgentDraft]);

	useEffect(() => {
		const handleConversationSelected = (event: Event) => {
			const id = (event as CustomEvent<{ conversationID?: string }>).detail?.conversationID || '';
			if (!id || runBusy) return;
      conversationSelectionRevisionRef.current += 1;
			void loadConversation(id).catch((cause: unknown) => {
				setError(cause instanceof Error ? cause.message : '创作会话加载失败');
			});
		};
		window.addEventListener('alpha-nexus:agent-conversation-selected', handleConversationSelected);
		return () => window.removeEventListener('alpha-nexus:agent-conversation-selected', handleConversationSelected);
  }, [runBusy]);

  useEffect(() => {
    if (!agentVisible || !requestedAgentConversationID || runBusy) return;
    const target = requestedAgentConversationID;
    conversationSelectionRevisionRef.current += 1;
    requestAgentConversation(null);
    void loadConversation(target).catch((cause: unknown) => {
      setError(cause instanceof Error ? cause.message : '会话读取失败，请重试。');
    });
  }, [agentVisible, requestAgentConversation, requestedAgentConversationID, runBusy]);

  useEffect(() => () => {
    sourceRef.current?.abort();
  }, []);

  useEffect(() => {
    closingRef.current = !agentVisible;
    if (agentVisible) return;
    sourceRef.current?.abort();
    sourceRef.current = null;
    if (!activeRun) return;
    const run = activeRun;
    const parsedStartedAt = Date.parse(run.created_at);
    setIsCancellingRun(true);
    void api.agentHubRunCancel(run.id).catch(() => undefined).finally(() => {
      setActiveRun((current) => current?.id === run.id ? null : current);
      setStreamingText('');
      setLastRunSummary({
        status: 'cancelled',
        elapsedMS: Number.isFinite(parsedStartedAt) ? Math.max(0, Date.now() - parsedStartedAt) : 0,
        errorCode: 'AGENT_RUN_CANCELLED',
      });
      setRunStartedAt(0);
      setIsCancellingRun(false);
    });
  }, [activeRun?.id, agentVisible]);

  useEffect(() => {
    if (!agentVisible || !pendingProviderSignature) return;
    const controller = new AbortController();
    const startedAt = Date.now();
    let attempt = 0;
    void (async () => {
      while (!controller.signal.aborted && Date.now() - startedAt < 60_000) {
        const delayMS = AGENT_PROVIDER_POLL_DELAYS[Math.min(attempt, AGENT_PROVIDER_POLL_DELAYS.length - 1)];
        attempt += 1;
        await new Promise<void>((resolve, reject) => {
          const onAbort = () => {
            window.clearTimeout(timer);
            reject(new DOMException('Aborted', 'AbortError'));
          };
          const timer = window.setTimeout(() => {
            controller.signal.removeEventListener('abort', onAbort);
            resolve();
          }, delayMS);
          controller.signal.addEventListener('abort', onAbort, { once: true });
        });
        const next = await api.agentHubProviders(controller.signal);
        await mutateProviders(next, { revalidate: false });
        if (!next.probes.some((probe) => probe.state === 'probing' || Boolean(probe.validating_model))) return;
      }
    })().catch((cause: unknown) => {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : 'Provider 状态刷新失败');
    });
    return () => controller.abort();
  }, [agentVisible, mutateProviders, pendingProviderSignature]);

  useEffect(() => {
    const onResize = () => setWindowWidth(window.innerWidth);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  // Follow the newest content only while the reader is already near the
  // bottom, so scrolling back through history is never yanked mid-stream.
  useEffect(() => {
    const el = messagesScrollRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 120;
    if (nearBottom) el.scrollTop = el.scrollHeight;
  }, [conversation?.messages?.length, streamingText, activeRun?.id, pendingApprovals.length]);

  // A finished run swaps the streaming block for the final message; land on
  // it instead of leaving the reader stranded above the refreshed history.
  const wasRunBusyRef = useRef(false);
  useEffect(() => {
    const el = messagesScrollRef.current;
    if (el && wasRunBusyRef.current && !runBusy) {
      requestAnimationFrame(() => { el.scrollTop = el.scrollHeight; });
    }
    wasRunBusyRef.current = runBusy;
  }, [runBusy, conversation?.messages?.length]);

  // Opening or restoring a conversation starts at the latest message.
  useEffect(() => {
    const el = messagesScrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [conversation?.id]);

  useEffect(() => {
    const onShortcut = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.shiftKey && event.key.toLowerCase() === 'a') {
        event.preventDefault();
        // On the persistent stage the drawer toggle has no visible effect,
        // so the shortcut must navigate back to the conversation instead.
        isStage ? useUIStore.getState().setActiveTab('agent') : useUIStore.getState().toggleAgentDrawer();
        return;
      }
      if ((event.metaKey || event.ctrlKey) && !event.shiftKey && event.key.toLowerCase() === 'k' && agentVisible) {
        // 键盘用户不该先找鼠标：Cmd/Ctrl+K 直接回到输入框。
        event.preventDefault();
        inputRef.current?.focus();
      }
    };
    window.addEventListener('keydown', onShortcut);
    return () => window.removeEventListener('keydown', onShortcut);
  }, [isStage, agentVisible]);

  // 打开菜单后把焦点放进第一个模型，键盘用户不用再按 Tab 找。
  useEffect(() => {
    if (!modelMenuOpen) return;
    const timer = window.setTimeout(() => {
      const list = document.querySelector<HTMLElement>('[role="listbox"][aria-label="选择模型"]');
      const selected = list?.querySelector<HTMLElement>('[role="option"][aria-selected="true"]');
      const target = selected ?? list?.querySelector<HTMLElement>('[role="option"]');
      target?.scrollIntoView({ block: 'nearest' });
      target?.focus({ preventScroll: true });
    }, 0);
    return () => window.clearTimeout(timer);
  }, [modelMenuOpen]);

  // 模型菜单打开时，Esc 在任何焦点位置都能关掉。
  useEffect(() => {
    if (!modelMenuOpen) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      setModelMenuOpen(false);
      window.setTimeout(() => modelTriggerRef.current?.focus(), 0);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [modelMenuOpen]);

  useEffect(() => {
    if (!agentVisible) return;
    const timer = window.setTimeout(() => {
      void api.agentHubProviderSelection(providerID, {
        conversation_id: conversation?.id || '',
        model_id: selectedModelID,
        draft: input,
		...workspaceSelection,
      }).catch(() => undefined);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [agentVisible, conversation?.id, input, providerID, selectedModelID]);

  const selectProvider = async (id: AgentHubProviderID) => {
    if (runBusy) return;
    clearAttachedContext();
    await api.agentHubProviderSelection(providerID, {
      conversation_id: conversation?.id || '',
      model_id: selectedModelID,
      draft: input,
		...workspaceSelection,
    }).catch(() => undefined);
    setProviderID(id);
    resetRunDisplay();
    setConversation(null);
    setConversations([]);
    setInput('');
    await api.agentHubApprovalMemoryClear().catch(() => undefined);
    setPendingApprovals([]);
    setToolTimeline([]);
    try {
      const bootstrap = await api.agentHubBootstrap(id, productMode);
      await mutateProviders({ providers: bootstrap.providers, probes: bootstrap.probes }, { revalidate: false });
      const scopedConversations = bootstrap.conversations.filter((item) => (item.workspace_mode || item.context?.product_mode || 'media') === productMode);
      setConversations(scopedConversations);
      setModelID(bootstrap.selection.model_id || '');
      setInput(bootstrap.selection.draft || '');
      setPendingApprovals(bootstrap.pending_approvals || []);
      setToolTimeline(id === 'codex' ? (bootstrap.pending_approvals || []) : []);
      const selected = scopedConversations.find((item) => item.id === bootstrap.selection.conversation_id)
        || scopedConversations[0];
      if (selected) await loadConversation(selected.id);
      void mutateProviders();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Provider 状态加载失败');
    }
  };

  const selectModel = async (nextModelID: string, providerOverride?: AgentHubProviderID) => {
    if (runBusy) return;
    const targetProvider = providerOverride ?? providerID;
    const previousModelID = modelID;
    setModelID(nextModelID);
    setError('');
    try {
      if (conversation) {
        const updated = await api.agentHubConversationUpdate(conversation.id, { model_id: nextModelID });
        setConversation((current) => current ? { ...current, model_id: updated.model_id } : current);
      }
      await api.agentHubProviderSelection(targetProvider, {
        conversation_id: conversation?.id || '',
        model_id: nextModelID,
        draft: input,
		...workspaceSelection,
      });
    } catch (cause) {
      setModelID(previousModelID);
      setError(cause instanceof Error ? cause.message : '模型切换失败，已保留原选择。');
    }
  };

  // One pick from the switcher: crossing providers re-bootstraps first, then
  // the chosen model is persisted onto the target provider.
  const pickModel = async (targetProvider: AgentHubProviderID, targetModel: string) => {
    setModelMenuOpen(false);
    window.setTimeout(() => modelTriggerRef.current?.focus(), 0);
    if (runBusy) return;
    if (targetProvider !== providerID) await selectProvider(targetProvider);
    if (targetModel !== selectedModelID) await selectModel(targetModel, targetProvider);
  };


  useEffect(() => {
    const handleProviderChange = (event: Event) => {
      const provider = (event as CustomEvent<{ providerID?: string }>).detail?.providerID || '';
      if (!provider || provider === providerID || runBusy) return;
      void selectProvider(provider);
    };
    window.addEventListener('alpha-nexus:agent-provider-changed', handleProviderChange);
    return () => window.removeEventListener('alpha-nexus:agent-provider-changed', handleProviderChange);
  }, [providerID, runBusy, conversation?.id, selectedModelID, input]);

  useEffect(() => {
    const handleVerifiedModel = (event: Event) => {
      const detail = (event as CustomEvent<{ providerID?: AgentHubProviderID; modelID?: string }>).detail;
      if (!detail?.modelID || detail.providerID !== providerID) return;
      void selectModel(detail.modelID);
    };
    window.addEventListener('alpha-nexus:agent-model-verified', handleVerifiedModel);
    return () => window.removeEventListener('alpha-nexus:agent-model-verified', handleVerifiedModel);
  }, [providerID, conversation?.id, runBusy, input]);

  // 「电脑」按钮：打开右侧电脑面板并停在电脑页。没选 Bot 时兜底到内置主 Agent，
  // 保证任何情况下都有反应。右侧详情在本产品里是常驻面板，所以这里不做开关
  // （关了会把 Bot 详情一起关掉），只负责切到画面。
  const openComputerPanel = () => {
    const state = useUIStore.getState();
    const target = currentBot?.id || conversation?.bot_id || state.activeBotID || BUILTIN_ALPHA_AGENT_ID;
    state.setActiveBotID(target);
    state.setBotDetailTab('computer');
    state.setBotInboxOpen(false);
  };

  const insertBotMention = (bot: AgentBot) => {
    setInput((current) => current.replace(/@[^@\s]*$/, `@${bot.name} `));
    window.setTimeout(() => inputRef.current?.focus(), 0);
  };

  const insertSkill = (skill: SkillLibraryItem) => {
    setInput((current) => current.replace(/\/[a-z0-9-]*$/, `/${skill.id} `));
    window.setTimeout(() => inputRef.current?.focus(), 0);
  };

  const installAssistantSkill = async (content: string) => {
    setError('');
    setNotice('');
    try {
      const installed = await api.skillImport(content);
      const catalog = await api.skillsList();
      setSkills(catalog.skills);
      setNotice(`已保存到本机 Skill 库：/${installed.id}（默认停用）`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Agent 输出还不是有效的 SKILL.md');
    }
  };

  const upsertToolTimeline = (call: AgentHubToolCall) => {
    setToolTimeline((current) => [call, ...current.filter((item) => item.id !== call.id)].slice(0, 12));
  };

  const trackCodexToolEvent = (run: AgentHubRun, conversationID: string, decoded: AgentHubEvent) => {
    // dsh runs its own tools and reports them as tool.observed events; the
    // database timeline is the truth source, this only refreshes the live card.
    if (providerID === 'dsh') {
      if (decoded.type !== 'tool.observed') return;
      const externalID = typeof decoded.payload.call_id === 'string' ? decoded.payload.call_id : '';
      if (!externalID) return;
      const call: AgentHubToolCall = {
        id: `dsh:${run.id}:${externalID}`,
        run_id: run.id,
        conversation_id: conversationID,
        provider_id: 'dsh',
        tool_name: String(decoded.payload.tool_name || 'runtime tool'),
        workspace: 'provider',
        risk_level: 'L1',
        params_hash: '',
        impact_preview: 'Provider 运行时自行执行的工具调用',
        status: decoded.payload.status === 'failed' ? 'failed' : decoded.payload.status === 'completed' ? 'completed' : 'running',
        error_code: typeof decoded.payload.error_code === 'string' ? decoded.payload.error_code : undefined,
        created_at: typeof decoded.payload.created_at === 'string' ? decoded.payload.created_at : decoded.timestamp,
        updated_at: decoded.timestamp,
      };
      upsertToolTimeline(call);
      return;
    }
    if (providerID !== 'codex') return;
    const toolCallID = typeof decoded.payload.tool_call_id === 'string' ? decoded.payload.tool_call_id : '';
    if (!toolCallID) return;
    let status: AgentHubToolCall['status'] | null = null;
    switch (decoded.type) {
      case 'tool.proposed': status = 'proposed'; break;
      case 'approval.required': status = 'pending'; break;
      case 'approval.resolved': status = decoded.payload.status === 'rejected' ? 'rejected' : 'approved'; break;
      case 'tool.started': status = 'running'; break;
      case 'tool.completed': status = 'completed'; break;
      case 'tool.failed': status = 'failed'; break;
      default: return;
    }
    setToolTimeline((current) => {
      const previous = current.find((item) => item.id === toolCallID);
      const approvalID = typeof decoded.payload.approval_id === 'string' ? decoded.payload.approval_id : previous?.approval_id;
      const call: AgentHubToolCall = {
        id: toolCallID,
        run_id: run.id,
        conversation_id: conversationID,
        provider_id: 'codex',
        tool_name: String(decoded.payload.tool_name || previous?.tool_name || 'Registry Skill'),
        workspace: String(decoded.payload.workspace || previous?.workspace || ''),
        risk_level: String(decoded.payload.risk_level || previous?.risk_level || 'L3') as AgentHubToolCall['risk_level'],
        params_hash: previous?.params_hash || '',
        impact_preview: String(decoded.payload.impact_preview || previous?.impact_preview || '受控 Registry 操作'),
        status,
        error_code: typeof decoded.payload.error_code === 'string' ? decoded.payload.error_code : previous?.error_code,
        approval_id: approvalID,
        created_at: previous?.created_at || decoded.timestamp,
        updated_at: decoded.timestamp,
      };
      return [call, ...current.filter((item) => item.id !== toolCallID)].slice(0, 12);
    });
  };

  const refreshConversation = async (id: string) => {
    const next = await loadConversation(id);
    await reloadConversations();
    return next;
  };

  // 交接从 running 变成终态时，结果已经写进父会话；拉一次会话就能看到，不用
  // 让用户手动刷新。状态存 ref，避免每轮轮询都重复刷新。
  const delegationStatusRef = useRef<Map<string, string>>(new Map());
  useEffect(() => {
    const rows = delegationResult?.delegations ?? [];
    let finished = false;
    for (const item of rows) {
      const previous = delegationStatusRef.current.get(item.id);
      if (previous === 'running' && item.status !== 'running') finished = true;
      delegationStatusRef.current.set(item.id, item.status);
    }
    if (finished && conversation?.id) void refreshConversation(conversation.id);
  }, [delegationResult, conversation?.id]);

  const openEventStream = (run: AgentHubRun, conversationID: string, startedAt: number) => {
    sourceRef.current?.abort();
    const controller = new AbortController();
    sourceRef.current = controller;
    let settled = false;
    const finishRun = (status: RunSummary['status'], errorCode?: string) => {
      if (settled) return;
      settled = true;
      setLastRunSummary({ status, elapsedMS: Date.now() - startedAt, errorCode });
      setActiveRun(null);
      setIsStartingRun(false);
      setIsCancellingRun(false);
      setRunStartedAt(0);
      if (status === 'completed') setStreamingText('');
      void refreshConversation(conversationID).catch(() => undefined);
    };
    const onEvent = (decoded: AgentHubEvent) => {
      trackCodexToolEvent(run, conversationID, decoded);
      if (decoded.type === 'message.delta') {
        const text = typeof decoded.payload.text === 'string' ? decoded.payload.text : '';
        if (text)        setStreamingText((current) => current + text);
      }
      if (decoded.type === 'run.failed') {
        const code = typeof decoded.payload.code === 'string' ? decoded.payload.code : 'AGENT_RUN_FAILED';
        setError(runErrorMessage(code));
      }
      if (decoded.type === 'approval.required') {
        const approvalID = typeof decoded.payload.approval_id === 'string' ? decoded.payload.approval_id : '';
        const toolCallID = typeof decoded.payload.tool_call_id === 'string' ? decoded.payload.tool_call_id : '';
        if (approvalID && toolCallID) {
          const call: AgentHubToolCall = {
            id: toolCallID,
            run_id: run.id,
            conversation_id: conversationID,
            provider_id: providerID,
            tool_name: String(decoded.payload.tool_name || ''),
            workspace: String(decoded.payload.workspace || ''),
            risk_level: String(decoded.payload.risk_level || 'L3') as AgentHubToolCall['risk_level'],
            params_hash: '',
            impact_preview: String(decoded.payload.impact_preview || '执行一项业务操作'),
            status: 'pending',
            approval_id: approvalID,
            created_at: decoded.timestamp,
            updated_at: decoded.timestamp,
          };
          setPendingApprovals((current) => [...current.filter((item) => item.id !== toolCallID), call]);
        }
      }
      if (decoded.type === 'approval.resolved') {
        const toolCallID = typeof decoded.payload.tool_call_id === 'string' ? decoded.payload.tool_call_id : '';
        if (toolCallID) setPendingApprovals((current) => current.filter((item) => item.id !== toolCallID));
      }
      if (decoded.type === 'tool.completed') {
        setNotice(`${String(decoded.payload.tool_name || '操作')} 已完成`);
      }
      if (decoded.type === 'tool.failed') {
        setError(`${String(decoded.payload.tool_name || '操作')} 执行失败：${String(decoded.payload.error_code || 'AGENT_TOOL_FAILED')}`);
      }
      if (decoded.type === 'run.completed' || decoded.type === 'run.failed' || decoded.type === 'run.cancelled' || decoded.type === 'run.interrupted') {
        controller.abort();
        sourceRef.current = null;
        const status = decoded.type === 'run.completed'
          ? 'completed'
          : decoded.type === 'run.cancelled'
            ? 'cancelled'
            : decoded.type === 'run.interrupted'
              ? 'interrupted'
              : 'failed';
        const code = typeof decoded.payload.code === 'string' ? decoded.payload.code : undefined;
        finishRun(status, code);
        return true;
      }
      return false;
    };
    void streamAgentHubEvents(run.id, onEvent, controller.signal).catch((cause: unknown) => {
      if (settled || controller.signal.aborted) return;
      sourceRef.current = null;
      const code = cause instanceof Error && cause.name === 'AbortError' ? '' : 'AGENT_EVENT_STREAM_INTERRUPTED';
      if (!code) return;
      setError(runErrorMessage(code));
      finishRun('interrupted', code);
    });
  };

  // 真正的 in-app subagent：在主会话里点名另一个 Bot，后端用那个 Bot 自己的
  // 会话和模型跑一轮，并把结果写回这里。主会话本身不起 run，所以不会串话。
  const delegateToBot = async (target: { bot: AgentBot; task: string; mention: string }) => {
    setError('');
    setNotice('');
    setLastRunSummary(null);
    setLastSubmittedMessage(target.mention);
    setInput('');
    try {
      let current = conversation;
      if (!current) {
        current = await api.agentHubConversationCreate({ provider_id: providerID, model_id: selectedModelID, context });
        current = { ...current, messages: [] };
        setConversation(current);
        await api.agentHubProviderSelection(providerID, { conversation_id: current.id, model_id: selectedModelID, draft: '', ...workspaceSelection });
      }
      await api.createAgentDelegation({
        parent_conversation_id: current.id,
        from_bot_id: current.bot_id || BUILTIN_ALPHA_AGENT_ID,
        to_bot_id: target.bot.id,
        task: target.task,
      });
      await refreshConversation(current.id);
      await refreshDelegations();
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : '';
      const known: Record<string, string> = {
        AGENT_DELEGATION_BUSY: '这个 Bot 正在处理上一件任务，等它结束再派。',
        AGENT_DELEGATION_LIMIT: '同时进行的交接最多 3 个，先等一个结束。',
        AGENT_DELEGATION_SELF: '不能把任务派给当前这位 Bot。',
        AGENT_BOT_NOT_FOUND: '这个 Bot 已不存在，刷新后再试。',
        AGENT_MODEL_NOT_VERIFIED: '这个 Bot 绑定的模型还没通过真实调用验证，先到设置里验证。',
      };
      const code = message.split(':', 1)[0];
      setError(known[code] || message || '派给 Bot 失败');
      // 派活失败时把原文还给输入框，用户不用重打一遍。
      setInput(target.mention);
    }
  };

  const cancelDelegation = async (id: string) => {
    try {
      await api.cancelAgentDelegation(id);
      await refreshDelegations();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '停止交接失败');
    }
  };

  const submitMessage = async (message: string) => {
    if (!message.trim() || runBusy) return;
    // 开头点名了另一个 Bot：走交接，而不是把这句话当成当前 Bot 的普通回合。
    const target = parseBotDelegation(message, (botRoster ?? []).filter((bot) => bot.id !== conversation?.bot_id));
    if (target) {
      await delegateToBot(target);
      return;
    }
    if (modelSelectionUnavailable) {
      setError(missingVerifiedModel
        ? (availableModels.length
          ? '请选择一个已通过真实调用验证的模型。'
          : 'Alpha Agent 尚无通过真实调用验证的模型，请先在设置中验证模型。')
        : '原模型不可用，请明确选择已验证模型。');
      return;
    }
    setError('');
    setNotice('');
    setLastRunSummary(null);
    setLastSubmittedMessage(message);
    // Clear only after the guards passed, so a blocked submit keeps the text.
    setInput('');
    const requestStartedAt = Date.now();
    setIsStartingRun(true);
    setRunStartedAt(requestStartedAt);
    let current = conversation;
    try {
      if (!current) {
        current = await api.agentHubConversationCreate({ provider_id: providerID, model_id: selectedModelID, context });
        current = { ...current, messages: [] };
        setConversation(current);
        await api.agentHubProviderSelection(providerID, { conversation_id: current.id, model_id: selectedModelID, draft: '', ...workspaceSelection });
      }
      const optimistic = {
        id: `local_${Date.now()}`,
        role: 'user',
        content: message,
        context,
        created_at: new Date().toISOString(),
      };
      setConversation((existing) => existing ? { ...existing, messages: [...(existing.messages || []), optimistic] } : existing);
      setStreamingText('');
      const run = await api.agentHubRun(current.id, {
        provider_id: providerID,
        model_id: selectedModelID,
        message,
        context,
        knowledge_scopes: selectedKnowledgeScopes,
        trend_context: selectedTrendContext || undefined,
      });
      if (closingRef.current) {
        await api.agentHubRunCancel(run.id).catch(() => undefined);
        setIsStartingRun(false);
        setLastRunSummary({ status: 'cancelled', elapsedMS: Date.now() - requestStartedAt, errorCode: 'AGENT_RUN_CANCELLED' });
        setRunStartedAt(0);
        return;
      }
      const startedAt = Number.isFinite(Date.parse(run.created_at)) ? Date.parse(run.created_at) : requestStartedAt;
      setActiveRun(run);
      setInput('');
      setIsStartingRun(false);
      setRunStartedAt(startedAt);
      void reloadConversations();
      openEventStream(run, current.id, startedAt);
    } catch (cause) {
      const code = cause instanceof Error ? cause.message.split(':', 1)[0] : '';
      setError(runErrorMessage(code));
      setLastRunSummary({ status: 'failed', elapsedMS: Date.now() - requestStartedAt, errorCode: code });
      setIsStartingRun(false);
      setRunStartedAt(0);
    }
  };

  const send = async () => {
    const message = input.trim();
    if (!message || runBusy) return;
    await submitMessage(message);
  };

  const cancel = async () => {
    if (!activeRun || isCancellingRun) return;
    setIsCancellingRun(true);
    try {
      await api.agentHubRunCancel(activeRun.id);
    } catch (cause) {
      setIsCancellingRun(false);
      setError(cause instanceof Error ? cause.message : '停止失败，请再试一次。');
    }
  };

  const retry = async () => {
    if (!lastSubmittedMessage || runBusy) return;
    await submitMessage(lastSubmittedMessage);
  };

  const resolveApproval = async (call: AgentHubToolCall, status: 'approved' | 'rejected') => {
    if (!call.approval_id || resolvingApprovalID) return;
    setResolvingApprovalID(call.approval_id);
    setError('');
    try {
      const response = await api.agentHubApprovalResolve(
        call.approval_id,
        status,
        status === 'approved' && call.risk_level === 'L1' && Boolean(rememberApproval[call.id]),
      );
      setPendingApprovals((current) => current.filter((item) => item.id !== call.id));
      if (response.tool_call?.provider_id === 'codex') upsertToolTimeline(response.tool_call);
      if (status === 'approved') {
        const result = response.tool_call;
        if (result?.status === 'failed') setError(`${call.tool_name} 执行失败：${result.error_code || 'AGENT_TOOL_FAILED'}`);
        else setNotice(`${call.tool_name} 已批准${result?.status === 'completed' ? '并完成' : ''}`);
      } else {
        setNotice(`${call.tool_name} 已拒绝`);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '审批处理失败');
    } finally {
      setResolvingApprovalID('');
    }
  };

  const closeWithCleanup = () => {
    void api.agentHubApprovalMemoryClear().catch(() => undefined);
    setPendingApprovals([]);
    setToolTimeline([]);
    closeAgentDrawer();
  };

  const resizeStart = (event: ReactPointerEvent<HTMLDivElement>) => {
    const initialX = event.clientX;
    const initialWidth = visibleDrawerWidth;
    const resize = (move: PointerEvent) => setAgentDrawerWidth(initialWidth + initialX - move.clientX);
    const end = () => {
      window.removeEventListener('pointermove', resize);
      window.removeEventListener('pointerup', end);
      window.removeEventListener('pointercancel', end);
    };
    window.addEventListener('pointermove', resize);
    window.addEventListener('pointerup', end);
    window.addEventListener('pointercancel', end);
  };

  if (!agentVisible) return null;

  const activeProbe = providers?.probes.find((probe) => probe.provider_id === providerID);
  const providerState = activeProbe?.state || 'unknown';
  const providerBlocked = providerState === 'unavailable' || providerState === 'incompatible';
  return (
    <aside
      data-testid="agent-drawer"
      data-layout={displayMode === 'stage' ? 'stage' : 'docked'}
      aria-label="Alpha Agent 对话"
      className={`relative z-10 flex h-full min-h-0 ${displayMode === 'stage' ? 'w-full min-w-0 bg-brand-bg' : 'shrink-0 border-l border-brand-border bg-brand-surface'}`}
      style={displayMode === 'stage' ? undefined : { width: visibleDrawerWidth }}
    >
      {displayMode === 'drawer' ? <div
        data-testid="agent-drawer-resize"
        onPointerDown={resizeStart}
        onKeyDown={(event) => {
          const nextWidth = {
            ArrowLeft: visibleDrawerWidth + 24,
            ArrowRight: visibleDrawerWidth - 24,
            Home: AGENT_PANEL_MIN_WIDTH,
            End: drawerMaxWidth,
          }[event.key];
          if (nextWidth === undefined) return;
          event.preventDefault();
          setAgentDrawerWidth(nextWidth);
        }}
        role="separator"
        aria-label="调整 Agent 宽度"
        aria-orientation="vertical"
        aria-valuemin={AGENT_PANEL_MIN_WIDTH}
        aria-valuemax={drawerMaxWidth}
        aria-valuenow={visibleDrawerWidth}
        aria-valuetext={`${visibleDrawerWidth} 像素`}
        tabIndex={0}
        className="group absolute -left-1 inset-y-0 z-20 w-2 cursor-col-resize outline-none focus-visible:bg-brand-accent/10"
      >
        <span aria-hidden="true" className="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 bg-transparent group-hover:bg-brand-accent/50 group-focus-visible:bg-brand-accent/70" />
      </div> : null}
      <div className="relative flex min-w-0 flex-1 flex-col">
        {/* 对话区右上角：电脑入口（Monitor 圆形按钮）。运行中带绿点。 */}
        <div aria-hidden="true" className="pointer-events-none absolute inset-x-0 top-0 z-10 h-12 bg-gradient-to-b from-brand-bg to-transparent" />
        <div className="absolute right-2 top-2 z-20 flex items-center gap-1.5">
          <div className="relative">
            <button
              type="button"
              data-testid="agent-history"
              aria-label="最近会话"
              aria-haspopup="menu"
              aria-expanded={historyOpen}
              title="最近会话"
              onClick={() => setHistoryOpen((open) => !open)}
              className="inline-flex h-7 w-7 items-center justify-center rounded-full bg-brand-surface/95 text-brand-muted shadow-sm ring-1 ring-black/5 transition hover:text-brand-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/40"
            >
              <History className="h-3.5 w-3.5" />
            </button>
            {historyOpen ? (
              <>
                <div aria-hidden="true" className="fixed inset-0 z-30" onClick={() => setHistoryOpen(false)} />
                <div
                  role="menu"
                  aria-label="最近会话"
                  className="absolute right-0 top-full z-40 mt-1.5 max-h-80 w-64 overflow-y-auto rounded-xl border border-brand-border bg-brand-surface-raised p-1.5 shadow-2xl"
                >
                  {conversationHistory.length === 0 ? (
                    <p className="px-2 py-3 text-center text-[11px] text-brand-muted">还没有更早的会话。</p>
                  ) : conversationHistory.map((item) => (
                    <button
                      key={item.id}
                      type="button"
                      role="menuitem"
                      data-testid={`agent-history-item-${item.id}`}
                      onClick={() => {
                        setHistoryOpen(false);
                        requestAgentConversation(item.id);
                      }}
                      className={`flex w-full flex-col gap-0.5 rounded-lg px-2 py-1.5 text-left transition hover:bg-brand-fg/[0.05] ${item.id === conversation?.id ? 'bg-brand-fg/[0.04]' : ''}`}
                    >
                      <span className="w-full truncate text-[11px] font-semibold text-brand-fg">{item.title || '未命名会话'}</span>
                      <span className="text-[9px] tabular-nums text-brand-muted">{item.updated_at ? item.updated_at.slice(5, 16).replace('T', ' ') : ''}</span>
                    </button>
                  ))}
                </div>
              </>
            ) : null}
          </div>
          <div className="relative">
            <button
              ref={modelTriggerRef}
              type="button"
              data-testid="agent-model-select"
              aria-label="选择模型"
              title={`${providerDisplay(providerID).name} · ${selectedModelID || '未选择模型'}`}
              aria-haspopup="listbox"
              aria-expanded={modelMenuOpen}
              disabled={runBusy}
              onClick={() => setModelMenuOpen((open) => !open)}
              onKeyDown={(event) => { if (event.key === 'Escape') setModelMenuOpen(false); }}
              className="inline-flex h-7 min-w-0 items-center gap-1.5 rounded-full bg-brand-surface/95 px-2.5 text-[11px] font-semibold text-brand-muted shadow-sm ring-1 ring-black/5 transition hover:text-brand-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/40 disabled:cursor-not-allowed disabled:opacity-50"
            >
              <ProviderLogo providerID={providerID} className="h-3.5 w-3.5" />
              <span className="max-w-48 truncate">{selectedModelUnavailable ? selectedModelLabel : selectedModelLabel || (modelGroups.length ? '选模型' : '未验证')}</span>
              <ChevronDown className="h-3 w-3 shrink-0 text-brand-muted" />
            </button>
            {modelMenuOpen ? (
              <>
                <div aria-hidden="true" className="fixed inset-0 z-30" onClick={() => { setModelMenuOpen(false); window.setTimeout(() => modelTriggerRef.current?.focus(), 0); }} />
                <div
                  role="listbox"
                  aria-label="选择模型"
                  onKeyDown={(event) => {
                    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
                    const options = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="option"]'));
                    if (options.length === 0) return;
                    event.preventDefault();
                    const index = options.indexOf(document.activeElement as HTMLButtonElement);
                    const next = event.key === 'Home'
                      ? options[0]
                      : event.key === 'End'
                        ? options[options.length - 1]
                        : options[Math.min(options.length - 1, Math.max(0, index + (event.key === 'ArrowDown' ? 1 : -1)))];
                    next.focus();
                  }}
                  className="absolute right-0 top-full z-40 mt-1.5 max-h-80 w-64 space-y-1 overflow-y-auto rounded-xl border border-brand-border bg-brand-surface-raised p-2 pb-3 shadow-2xl"
                >
                  {modelGroups.map((group) => (
                    <div key={group.id} role="group" aria-label={providerDisplay(group.id).name} className="border-t border-brand-border/60 py-1.5 first:border-t-0 first:pt-0">
                      <p className="flex items-center gap-1.5 px-2 py-1.5 text-[10px] font-semibold uppercase tracking-wide text-brand-muted">
                        <ProviderLogo providerID={group.id} className="h-3 w-3" />
                        <span className="min-w-0 truncate">{providerDisplay(group.id).name}</span>
                        <span
                          aria-hidden="true"
                          title={probeByID.get(group.id)?.ready ? 'CLI 就绪' : probeByID.get(group.id)?.state === 'unavailable' ? 'CLI 未安装' : 'CLI 受限'}
                          className={`ml-auto h-1.5 w-1.5 shrink-0 rounded-full ${
                            probeByID.get(group.id)?.state === 'ready'
                              ? 'bg-brand-success'
                              : probeByID.get(group.id)?.state === 'unavailable'
                                ? 'bg-brand-danger/60'
                                : 'bg-brand-warning'
                          }`}
                        />
                      </p>
                      {group.models.map((model) => {
                        const active = group.id === providerID && model === selectedModelID;
                        return (
                          <button
                            key={`${group.id}:${model}`}
                            type="button"
                            role="option"
                            aria-selected={active}
                            title={`${providerDisplay(group.id).name} · ${model}`}
                            onClick={() => void pickModel(group.id, model)}
                            className={`flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[11.5px] font-medium ${active ? 'bg-brand-fg/[0.05] text-brand-fg' : 'text-brand-muted hover:bg-brand-fg/[0.04]'}`}
                          >
                            <ProviderLogo providerID={group.id} className="h-3.5 w-3.5" />
                            <span className="min-w-0 flex-1 truncate">{model}</span>
                            {active ? <Check className="h-3 w-3 shrink-0 text-brand-accent" /> : null}
                          </button>
                        );
                      })}
                      {group.models.length === 0 ? (
                        <button
                        type="button"
                        data-testid={`agent-model-group-empty-${group.id}`}
                        onClick={() => {
                          setModelMenuOpen(false);
                          useUIStore.getState().setActiveTab('settings');
                        }}
                        className="flex w-full items-center justify-between gap-2 rounded-lg py-1.5 pl-9 pr-2.5 text-left text-[10px] leading-4 text-brand-muted transition hover:bg-brand-fg/[0.04] hover:text-brand-fg"
                      >
                        <span>
                          {probeByID.get(group.id)?.state === 'unavailable'
                            ? '未安装 · 去设置查看安装方式'
                            : /sign in|login|登录/i.test(probeByID.get(group.id)?.reason || '')
                              ? '未登录 · 先打开对应 CLI 登录'
                              : probeByID.get(group.id)?.model_validation_error === 'AGENT_MODEL_VALIDATION_TIMEOUT'
                                ? '上次验证超时 · 去设置重试'
                                : probeByID.get(group.id)?.model_validation_error === 'AGENT_MODEL_UNREACHABLE'
                                  ? '上次调用失败 · 去设置重试'
                                  : '未验证 · 去设置跑一次真实调用'}
                        </span>
                        <span aria-hidden="true">→</span>
                      </button>
                      ) : null}
                    </div>
                  ))}
                  {modelGroups.length === 0 ? <p className="px-2 py-2 text-[10px] text-brand-muted">暂无已验证模型，请先在设置中验证。</p> : null}
                </div>
              </>
            ) : null}
          </div>
          <button
            type="button"
            data-testid="agent-computer-button"
            onClick={openComputerPanel}
            title="电脑"
            aria-label="打开电脑面板"
            aria-pressed={Boolean(activeBotID)}
            className={`relative flex h-7 w-7 items-center justify-center rounded-full bg-brand-surface/90 shadow-sm ring-1 ring-black/5 transition ${
              activeBotID ? 'text-brand-accent' : 'text-brand-muted hover:text-brand-muted'
            }`}
          >
            <Monitor className="h-4 w-4" />
            {runBusy ? (
              <span aria-hidden="true" data-testid="agent-computer-live" className="absolute right-0.5 top-0.5 h-1.5 w-1.5 animate-pulse rounded-full bg-brand-success/100" />
            ) : null}
          </button>
          {displayMode === 'drawer' ? (
            <button
              type="button"
              onClick={closeWithCleanup}
              title="关闭 Agent"
              aria-label="关闭 Agent"
              className="flex h-7 w-7 items-center justify-center rounded-full bg-brand-surface/90 text-brand-muted shadow-sm ring-1 ring-black/5 transition hover:text-brand-muted"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          ) : null}
        </div>


        <div ref={messagesScrollRef} className="min-h-0 flex-1 overflow-y-auto px-3 pb-5 pt-12">
          {isBootstrapping ? (
            <LocalLoadingState
              testID="agent-bootstrap-loading"
              label="正在恢复 Agent"
              detail="读取会话、模型和待确认操作"
              className="h-full"
            />
          ) : providerBlocked ? (
            <section data-testid="agent-provider-connect" className="mx-auto flex h-full max-w-sm flex-col items-center justify-center text-center">
              <ProviderMark className="h-16 w-16" />
              <h3 className="mt-5 text-lg font-bold text-brand-fg">{providerLabel} 暂不可用</h3>
              <p className="mt-2 text-xs leading-6 text-brand-muted">请先在设置中完成当前模型验证或连接。</p>
              <button data-testid="agent-provider-connect-button" type="button" onClick={() => { if (displayMode === 'drawer') closeAgentDrawer(); useUIStore.getState().setActiveTab('settings'); }} className="mt-5 bg-brand-accent px-4 py-2.5 text-xs font-bold text-brand-on-accent shadow-sm hover:brightness-95">
                在设置中管理连接
              </button>
            </section>
          ) : (
            <>
              {ombMessages.length === 0 ? (
                <div className="flex h-full flex-col items-center justify-center pb-20 text-center">
                  <ProviderMark className="h-16 w-16 opacity-90" />
                  <h3 className="mt-4 text-base font-bold text-brand-fg">{currentBot?.name || 'Alpha Agent'}</h3>
                  <p className="mt-2 max-w-sm text-balance text-xs leading-6 text-brand-muted">从当前页面开始提问。需要执行操作时，Agent 会先向你确认。</p>
                  {!modelSelectionUnavailable ? (
                    <div data-testid="agent-empty-prompts" className="mt-4 flex flex-wrap items-center justify-center gap-1.5">
                      {['总结当前页面', '检查未读与交接', '给我下一步执行计划'].map((prompt) => (
                        <button
                          key={prompt}
                          type="button"
                          onClick={() => {
                            setInput(prompt);
                            window.setTimeout(() => inputRef.current?.focus(), 0);
                          }}
                          className="rounded-full bg-brand-surface-raised px-3 py-1.5 text-[11px] font-medium text-brand-muted transition hover:text-brand-fg"
                        >
                          {prompt}
                        </button>
                      ))}
                    </div>
                  ) : null}
                  {modelSelectionUnavailable ? (
                    <div data-testid="agent-model-validation-hint" className="mt-5 flex flex-col items-center gap-2.5">
                      <p className="max-w-xs text-xs leading-5 text-brand-warning">
                        {(probe?.model_candidates?.length || 0) > 0
                          ? `已检测到 ${probe?.model_candidates?.length} 个候选模型，通过一次真实调用验证后即可开始对话。`
                          : '还没有通过真实调用验证的模型，请先在设置中验证。'}
                      </p>
                      <button
                        type="button"
                        data-testid="agent-go-validate-model"
                        onClick={() => window.dispatchEvent(new CustomEvent('alpha-nexus-egolite:open-settings'))}
                        className="rounded-lg bg-gray-900 px-4 py-2 text-xs font-semibold text-white shadow-sm transition-colors hover:bg-gray-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/50"
                      >
                        去设置验证模型
                      </button>
                    </div>
                  ) : null}
                </div>
              ) : null}
              <div className={`space-y-5 ${isStage ? 'w-full' : ''}`}>
                {ombMessages.length > 0 ? (
                  <ChatTranscript
                    bot={transcriptBot}
                    items={ombItems}
                    working={runBusy}
                    showCopy={false}
                    className="flex-none overflow-visible"
                    contentClassName="flex w-full flex-col gap-4"
                    renderAvatar={() => currentBot ? (
                      <AgentIdentityAvatar bot={currentBot} size={40} className="mt-0.5" />
                    ) : (
                      <AlphaPixelMark className="mt-0.5 h-10 w-10 shrink-0" decorative />
                    )}
                    renderAccessory={(message) => {
                      if (message.role !== 'bot' || message.streaming) return null;
                      const runID = (message as BridgeMessage).runID;
                      const turn = runID ? knowledgeTurnsByRun.get(runID) : undefined;
                      const retrieved = turn?.citations || [];
                      const used = turn?.used_citations || [];
                      const usedIDs = new Set(used.map((citation) => citation.chunk_id));
                      return (
                        <>
                          {turn ? (
                            <details data-testid="agent-knowledge-citations" className="mt-2 border-l-2 border-brand-border pl-2 text-[10px] text-brand-muted">
                              <summary className="cursor-pointer select-none font-semibold text-brand-muted">
                                知识来源 · 已引用 {used.length} / 已检索 {retrieved.length}
                              </summary>
                              <div className="mt-1.5 space-y-1.5">
                                {retrieved.map((citation) => {
                                  const wasUsed = usedIDs.has(citation.chunk_id);
                                  return (
                                    <div key={citation.chunk_id} className="flex items-start justify-between gap-2">
                                      <span className="min-w-0 break-words">[KB:{citation.chunk_id}] {citation.base_name} / {citation.source_name}{citation.heading_path ? ` / ${citation.heading_path}` : ''}</span>
                                      <span className={`shrink-0 ${wasUsed ? 'text-brand-success' : 'text-brand-muted'}`}>{wasUsed ? '已引用' : '未在回答中引用'}</span>
                                    </div>
                                  );
                                })}
                              </div>
                            </details>
                          ) : null}
                          {creatorConversation ? (
                            <div className="mt-2 flex items-center gap-2">
                              <button type="button" onClick={() => void installAssistantSkill(message.text || '')} className="inline-flex items-center gap-1.5 rounded-lg border border-brand-border bg-brand-surface px-2.5 py-1.5 text-[10px] font-semibold text-brand-accent hover:bg-brand-fg/[0.04]">
                                <Download className="h-3.5 w-3.5" />添加到 Skill 库
                              </button>
                            </div>
                          ) : null}
                        </>
                      );
                    }}
                  />
                ) : null}
                {toolCards.length > 0 ? (
                  <section data-testid="agent-tool-cards-section" className="mr-5 border border-brand-border bg-brand-surface-raised p-3 text-[11px] text-brand-fg-subtle">
                    <div className="flex items-center justify-between gap-2">
                      <p className="font-semibold text-brand-fg">工具调用</p>
                      <span className="text-[9px] text-brand-muted">共 {toolCards.length} 次 · 点击展开参数与结果</span>
                    </div>
                    {toolCards.length > 0 ? (
                      <div data-testid="agent-tool-cards" className="mt-2 space-y-1.5">
                        {toolCards.map((call) => {
                          const paramsText = formatToolPayload(call.params);
                          const resultText = formatToolPayload(call.result);
                          const openByDefault = call.status === 'failed' || call.status === 'running' || call.status === 'pending';
                          const dotClass = call.status === 'failed' || call.status === 'rejected'
                            ? 'bg-brand-danger'
                            : call.status === 'completed'
                              ? 'bg-brand-success'
                              : call.status === 'running'
                                ? 'bg-brand-warning animate-pulse'
                                : 'bg-brand-muted';
                          const statusClass = call.status === 'failed' || call.status === 'rejected'
                            ? 'text-brand-danger'
                            : call.status === 'completed'
                              ? 'text-brand-success'
                              : 'text-brand-muted';
                          return (
                            <details
                              key={call.id}
                              data-testid={`agent-tool-card-${call.id}`}
                              open={toolOpenState[call.id] ?? openByDefault}
                              onToggle={(event) => {
                                const opened = event.currentTarget.open;
                                setToolOpenState((current) => (
                                  current[call.id] === opened ? current : { ...current, [call.id]: opened }
                                ));
                              }}
                              className="rounded-md border border-brand-border/70 bg-brand-surface px-2 py-1.5"
                            >
                              <summary className="flex cursor-pointer list-none items-center gap-2 text-[11px]">
                                <span aria-hidden="true" className={`h-1.5 w-1.5 shrink-0 rounded-full ${dotClass}`} />
                                <span className="min-w-0 flex-1 truncate">
                                  <span className="font-mono text-[10px] text-brand-fg">{call.tool_name}</span>
                                  {call.impact_preview ? <span className="text-brand-muted"> · {call.impact_preview}</span> : null}
                                </span>
                                <span className="shrink-0 font-mono text-[9px] tabular-nums text-brand-muted">{formatToolDuration(call.duration_ms)}</span>
                                <span className={`shrink-0 text-[10px] font-semibold ${statusClass}`}>{toolStatusLabel(call.status as AgentHubToolCall['status'])}</span>
                              </summary>
                              <div className="mt-1.5 space-y-1.5 border-t border-brand-border/60 pt-1.5">
                                {paramsText ? (
                                  <div>
                                    <p className="text-[9px] font-semibold uppercase tracking-wide text-brand-muted">参数</p>
                                    <pre className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-all font-mono text-[10px] leading-4 text-brand-fg-subtle">{paramsText}</pre>
                                  </div>
                                ) : null}
                                {resultText ? (
                                  <div>
                                    <p className="text-[9px] font-semibold uppercase tracking-wide text-brand-muted">结果</p>
                                    <pre className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-all font-mono text-[10px] leading-4 text-brand-fg-subtle">{resultText}</pre>
                                  </div>
                                ) : null}
                                {call.error_code ? (
                                  <div className="rounded bg-brand-danger/10 px-2 py-1.5">
                                    <p className="font-mono text-[10px] font-semibold text-brand-danger">{call.error_code}</p>
                                    <p className="mt-0.5 text-[10px] leading-4 text-brand-muted">{toolErrorHint(call.error_code)}</p>
                                  </div>
                                ) : null}
                                {!paramsText && !resultText && !call.error_code ? (
                                  <p className="text-[10px] text-brand-muted">这个工具没有返回可展示的参数或结果。</p>
                                ) : null}
                                <button
                                  type="button"
                                  onClick={() => openAgentContext({
                                    id: call.id,
                                    kind: 'registry_tool',
                                    title: call.tool_name,
                                    summary: call.impact_preview || '受控 Registry 操作',
                                    status: toolStatusLabel(call.status as AgentHubToolCall['status']),
                                    occurredAt: call.updated_at,
                                    fields: [
                                      { label: '工作域', value: call.workspace || productMode },
                                      { label: '风险等级', value: call.risk_level || '—' },
                                      { label: '耗时', value: formatToolDuration(call.duration_ms) },
                                      ...(call.error_code ? [{ label: '错误码', value: call.error_code }] : []),
                                    ],
                                  })}
                                  className="text-[10px] font-semibold text-brand-accent hover:underline"
                                >
                                  在详情面板打开
                                </button>
                              </div>
                            </details>
                          );
                        })}
                      </div>
                    ) : null}
                  </section>
                ) : null}
                {isCodexProvider ? (
                  <section data-testid="agent-codex-tool-status" className="mr-5 border border-brand-border bg-brand-surface-raised p-3 text-[11px] text-brand-fg-subtle">
                    <div className="flex items-center justify-between gap-2">
                      <p className="font-semibold text-brand-fg">受控执行</p>
                      <span className="rounded bg-brand-accent/12 px-1.5 py-0.5 text-[9px] font-semibold text-brand-fg">只读</span>
                    </div>
                    <p className="mt-1 leading-5">仅通过 Registry Skill 提议业务操作；不提供终端、源码写入或浏览器控制权限。</p>
                    <div className="mt-2 flex flex-wrap gap-1">
                      {(activeProvider?.capabilities || []).map((capability) => (
                        <span key={capability} className="rounded bg-brand-surface px-1.5 py-0.5 text-[9px] text-brand-muted">{capability}</span>
                      ))}
                    </div>
                    <p className="mt-2 text-[10px] font-medium text-brand-muted">工具状态：提议 → 等待批准 → 执行中 → 完成 / 失败</p>
                  </section>
                ) : null}
                {pendingApprovals.map((call) => {
                  const strong = call.risk_level === 'L3';
                  const paidOrExternal = call.risk_level === 'L2';
                  return (
                    <section key={call.id} data-testid="agent-approval-card" className={`mr-5 border p-3 ${strong ? 'border-red-300 bg-brand-danger/10' : paidOrExternal ? 'border-brand-warning/30 bg-brand-warning/10' : 'border-brand-border bg-brand-bg'}`}>
                      <div className="flex items-start gap-2.5">
                        <div className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full ${strong ? 'bg-brand-danger/15 text-brand-danger' : 'bg-brand-warning/10 text-brand-warning'}`}>
                          {strong ? <AlertTriangle className="h-4 w-4" /> : <ShieldCheck className="h-4 w-4" />}
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center justify-between gap-2">
                            <p className="truncate text-xs font-bold text-brand-fg">{strong ? '强确认' : '需要确认'} · {call.tool_name}</p>
                            <span className={`shrink-0 px-1.5 py-0.5 font-mono text-[9px] ${strong ? 'bg-brand-danger/15 text-brand-danger' : 'bg-brand-warning/10 text-brand-warning'}`}>{call.risk_level}</span>
                          </div>
                          <p className="mt-1 text-[11px] leading-5 text-brand-muted">{call.impact_preview}</p>
                          {call.risk_level === 'L1' ? (
                            <label className="mt-2 flex items-center gap-2 text-[10px] text-brand-muted">
                              <input
                                type="checkbox"
                                checked={Boolean(rememberApproval[call.id])}
                                onChange={(event) => setRememberApproval((current) => ({ ...current, [call.id]: event.target.checked }))}
                              />
                              本会话内记住同一工具和对象（30 分钟）
                            </label>
                          ) : null}
                          {paidOrExternal ? <p className="mt-1 text-[10px] text-brand-warning">可能启动浏览器、联网或产生费用；不会自动重试。</p> : null}
                          {strong ? <p className="mt-1 text-[10px] font-semibold text-brand-danger">发布、预填或删除类动作不可记住；最终发布仍会单独授权。</p> : null}
                          <div className="mt-3 flex justify-end gap-2">
                            <button type="button" disabled={Boolean(resolvingApprovalID)} onClick={() => void resolveApproval(call, 'rejected')} className="border border-brand-border bg-brand-surface px-3 py-1.5 text-[11px] font-semibold text-brand-muted disabled:opacity-50">拒绝</button>
                            <button type="button" disabled={Boolean(resolvingApprovalID)} onClick={() => void resolveApproval(call, 'approved')} className={`px-3 py-1.5 text-[11px] font-bold text-brand-on-accent disabled:opacity-50 ${strong ? 'bg-red-600' : 'bg-brand-accent'}`}>
                              {resolvingApprovalID === call.approval_id ? '处理中…' : strong ? '确认执行' : '批准'}
                            </button>
                          </div>
                        </div>
                      </div>
                    </section>
                  );
                })}
                {/* 运行中由 transcript 的「正在思考」点表示；成功不再画条。
                    只有失败/停止才留一行极简提示 + 重试，不画边框、不占一条线。 */}
                {!runBusy && lastRunSummary && lastRunSummary.status !== 'completed' ? (
                  <p data-testid="agent-run-progress" role="status" aria-live="polite" className="mr-5 text-[11px] leading-5 text-brand-muted">
                    {lastRunSummary.status === 'cancelled' ? '已停止' : lastRunSummary.status === 'interrupted' ? '连接已中断' : '运行失败'}
                    {runSummaryMessage(lastRunSummary) ? ` · ${runSummaryMessage(lastRunSummary)}` : ''}
                    {lastSubmittedMessage ? (
                      <button type="button" onClick={() => void retry()} className="ml-1.5 font-semibold text-brand-accent hover:underline">重试</button>
                    ) : null}
                  </p>
                ) : null}
              </div>
            </>
          )}
        </div>

        {notice ? <div role="status" className="mx-4 mb-2 border border-emerald-200 bg-brand-success/10 px-2 py-1.5 text-[11px] text-brand-success">{notice}</div> : null}
        {error ? (
          <div role="alert" className="mx-4 mb-2 flex items-start gap-2 border border-red-200 bg-brand-danger/10 px-2 py-1.5 text-[11px] text-brand-danger">
            <span className="min-w-0 flex-1 break-words">{error}</span>
            <button
              type="button"
              aria-label="关闭错误提示"
              onClick={() => setError('')}
              className="shrink-0 rounded p-0.5 text-brand-danger hover:bg-brand-danger/15 hover:text-red-800"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        ) : null}
        {runBusy ? (
          <div data-testid="agent-run-strip" className="flex items-center gap-2 border-t border-brand-border px-5 py-2 text-[11px] text-brand-muted">
            <span aria-hidden="true" className="h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-brand-success" />
            <span className="min-w-0 flex-1 truncate" role="status" aria-live="polite">
              运行中 · 已用 <WorkingTimer since={runStartedAt || Date.now()} />
              {toolCards.length > 0 ? ` · 工具 ${toolCards.length} 次` : ''}
              {longRun ? ' · 仍在运行，可继续等待或停止' : ''}
            </span>
            <button
              type="button"
              data-testid="agent-run-stop"
              disabled={isCancellingRun}
              onClick={() => void cancel()}
              className="flex h-7 shrink-0 items-center rounded-md px-2 text-[10px] font-semibold text-brand-muted transition hover:bg-brand-fg/[0.06] hover:text-brand-fg disabled:opacity-50"
            >
              {isCancellingRun ? '停止中…' : '停止'}
            </button>
          </div>
        ) : null}
        {activeDelegations.length > 0 || recentDelegations.length > 0 ? (
          <div data-testid="agent-delegation-strip" className="flex flex-col gap-1.5 border-t border-brand-border px-5 py-2">
            {activeDelegations.map((item) => {
              const target = botByID.get(item.to_bot_id);
              const since = Date.parse(item.created_at) || Date.now();
              return (
              <div key={item.id} data-testid={`agent-delegation-${item.id}`} className="flex min-w-0 items-center gap-2 text-[11px] text-brand-muted">
                {target ? (
                  <AgentIdentityAvatar bot={target} size={16} className="shrink-0" />
                ) : (
                  <GitFork className="h-3.5 w-3.5 shrink-0 text-brand-accent" aria-hidden="true" />
                )}
                <span className="min-w-0 flex-1 truncate">
                  <span className="font-semibold text-brand-fg">@{botNameByID.get(item.to_bot_id) || 'Bot'}</span>
                  {' '}正在处理：{item.task}
                </span>
                <span className="shrink-0 text-[10px] tabular-nums text-brand-muted" title="已运行时长">
                  <WorkingTimer since={since} />
                </span>
                <button
                  type="button"
                  data-testid={`agent-delegation-cancel-${item.id}`}
                  onClick={() => void cancelDelegation(item.id)}
                  className="flex h-7 shrink-0 items-center rounded-md px-2 text-[10px] font-semibold text-brand-muted transition hover:bg-brand-fg/[0.06] hover:text-brand-fg"
                >
                  停止
                </button>
              </div>
              );
            })}
            {recentDelegations.map((item) => {
              const failed = item.status === 'failed';
              const cancelled = item.status === 'cancelled';
              const elapsed = formatToolDuration(
                (Date.parse(item.updated_at) || 0) - (Date.parse(item.created_at) || 0),
              );
              const preview = failed
                ? item.error_code || '没有返回原因'
                : plainPreview(item.result || '');
              return (
                <div key={item.id} data-testid={`agent-delegation-done-${item.id}`} className="flex min-w-0 items-start gap-2 text-[11px] text-brand-muted">
                  {failed ? (
                    <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-brand-danger" aria-hidden="true" />
                  ) : cancelled ? (
                    <GitFork className="mt-0.5 h-3.5 w-3.5 shrink-0 text-brand-muted" aria-hidden="true" />
                  ) : (
                    <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-brand-success" aria-hidden="true" />
                  )}
                  <span className="min-w-0 flex-1">
                    <span className="font-semibold text-brand-fg">@{botNameByID.get(item.to_bot_id) || 'Bot'}</span>
                    {' '}{failed ? '失败' : cancelled ? '已停止' : '完成'} · 用时 {elapsed}
                    {preview ? (
                      <span className={`mt-0.5 block truncate ${failed ? 'font-mono text-[10px] text-brand-danger' : 'text-brand-muted'}`}>{preview}</span>
                    ) : null}
                  </span>
                </div>
              );
            })}
          </div>
        ) : null}
        {!providerBlocked ? (
          <div className="relative shrink-0 border-t border-brand-border p-5">
            {mentionToken ? (
              <div data-testid="agent-context-suggestions" className="absolute bottom-full left-5 right-5 z-20 mb-1 max-h-72 overflow-y-auto rounded-lg border border-brand-border bg-brand-surface p-2 shadow-xl">
                <p className="px-2 pb-1 text-[10px] font-semibold text-brand-muted">派给 Bot、引用资料或趋势</p>
                {mentionBots.length > 0 ? (
                  <div className="border-b border-brand-border pb-1">
                    <p className="px-2 py-1 text-[9px] text-brand-muted">派给 Bot（会真实跑一轮）</p>
                    {mentionBots.map((bot) => (
                      <button
                        key={bot.id}
                        type="button"
                        data-testid={`agent-bot-mention-${bot.id}`}
                        onClick={() => insertBotMention(bot)}
                        className="flex w-full items-center justify-between gap-2 px-2 py-2 text-left text-xs text-brand-muted hover:bg-brand-fg/[0.03]"
                      >
                        <span className="flex min-w-0 items-center gap-2">
                          <GitFork className="h-3.5 w-3.5 shrink-0" />
                          <span className="truncate font-semibold text-brand-fg">{bot.name}</span>
                        </span>
                        <span className="shrink-0 truncate text-[9px] text-brand-muted">{bot.role || 'Bot'}</span>
                      </button>
                    ))}
                  </div>
                ) : null}
                {mentionKnowledgeOptions.length > 0 ? (
                  <div className="border-b border-brand-border pb-1">
                    <p className="px-2 py-1 text-[9px] text-brand-muted">资料</p>
                    {mentionKnowledgeOptions.map((option) => {
                  const scope = { base_id: option.base_id, source_prefix: option.source_prefix };
                  const selected = selectedKnowledgeScopes.some((item) => scopeKey(item) === scopeKey(scope));
                  return (
                    <button
                      key={scopeKey(scope)}
                      type="button"
                      aria-pressed={selected}
                      onClick={() => toggleKnowledgeScope(option)}
                      className={`flex w-full items-center justify-between gap-2 px-2 py-2 text-left text-xs ${selected ? 'bg-brand-accent/10 text-brand-accent' : 'text-brand-muted hover:bg-brand-fg/[0.03]'}`}
                    >
                      <span className="flex min-w-0 items-center gap-2">
                        <FolderOpen className="h-3.5 w-3.5 shrink-0" />
                        <span className="truncate">{option.label}</span>
                      </span>
                      {selected ? <CheckCircle2 className="h-3.5 w-3.5 shrink-0" /> : null}
                    </button>
                  );
                    })}
                  </div>
                ) : null}
                {mentionTrendFolders.length > 0 ? (
                  <div className="pt-1">
                    <p className="px-2 py-1 text-[9px] text-brand-muted">趋势</p>
                    {mentionTrendFolders.map((folder) => (
                      <button key={folder.label} type="button" onClick={() => selectTrendFolder(folder.label)} className="flex w-full items-center justify-between gap-2 px-2 py-2 text-left text-xs text-brand-muted hover:bg-brand-fg/[0.03]">
                        <span className="flex min-w-0 items-center gap-2"><FolderOpen className="h-3.5 w-3.5 shrink-0" /><span className="truncate">{folder.label}</span></span>
                        <span className="text-[10px] text-brand-muted">{folder.items.length} 条</span>
                      </button>
                    ))}
                  </div>
                ) : null}
                {mentionBots.length === 0 && mentionKnowledgeOptions.length === 0 && mentionTrendFolders.length === 0 ? <p className="p-3 text-xs text-brand-muted">没有匹配的 Bot、资料或趋势</p> : null}
              </div>
            ) : null}
            {skillSuggestions.length > 0 ? (
              <div data-testid="agent-skill-suggestions" className="absolute bottom-full left-5 right-5 z-10 border border-brand-border bg-brand-surface p-1 shadow-xl">
                {skillSuggestions.map((skill) => (
                  <button key={skill.id} type="button" onClick={() => insertSkill(skill)} className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left hover:bg-brand-fg/[0.04]">
                    <span className="min-w-0">
                      <span className="block truncate text-xs font-semibold text-brand-fg">{skill.name}</span>
                      <span className="block truncate font-mono text-[10px] text-brand-accent">/{skill.id}</span>
                    </span>
                    <span className="shrink-0 text-[9px] text-brand-muted">{skill.runtime === 'registry' ? 'Registry' : skill.source === 'system' ? '内置' : '本机'}</span>
                  </button>
                ))}
              </div>
            ) : null}
            <div data-testid="agent-composer" className="space-y-2">
              {selectedKnowledgeScopes.length > 0 || selectedTrendLabel ? <div className="mb-1.5 flex min-w-0 flex-wrap items-center gap-1.5 px-1">
                {selectedKnowledgeScopes.map((scope) => {
                  const label = knowledgeScopeOptions.find((option) => scopeKey(option) === scopeKey(scope))?.label || `资料 ${scope.base_id}`;
                  return <span key={scopeKey(scope)} className="inline-flex max-w-48 items-center gap-1 truncate rounded-md bg-brand-fg/[0.055] px-2 py-1 text-[10px] text-brand-muted">@资料/{label}<button type="button" aria-label={`移除${label}`} onClick={() => setSelectedKnowledgeScopes((current) => current.filter((item) => scopeKey(item) !== scopeKey(scope)))}><X className="h-3 w-3" /></button></span>;
                })}
                {selectedTrendLabel ? <span className="inline-flex max-w-64 items-center gap-1 truncate rounded-md bg-brand-fg/[0.055] px-2 py-1 text-[10px] text-brand-muted">@趋势/{selectedTrendLabel}<button type="button" aria-label="移除趋势" onClick={() => { setSelectedTrendLabel(''); setSelectedTrendContext(''); }}><X className="h-3 w-3" /></button></span> : null}
              </div> : null}
              <Composer
                fieldRef={inputRef}
                value={input}
                onChange={setInput}
                onSend={() => void send()}
                busy={runBusy}
                onStop={() => void cancel()}
                disabled={modelSelectionUnavailable}
                placeholder="输入 @ 派活或引用资料，/ 使用 Skill…"
                sendTitle={runBusy ? '停止当前运行' : '发送（Enter）· 换行（Shift+Enter）'}
                textareaAriaLabel="输入给 Alpha Agent 的消息"
              />
            </div>
          </div>
        ) : null}
      </div>
    </aside>
  );
}
