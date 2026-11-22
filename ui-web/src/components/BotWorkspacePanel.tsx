import { useEffect, useState } from 'react';
import useSWR from 'swr';
import {
  Database,
  ExternalLink,
  FolderOpen,
  Globe,
  Loader2,
  Monitor,
  RotateCcw,
  SlidersHorizontal,
  X,
  type LucideIcon,
} from 'lucide-react';
import { api, type Account, type AgentBot, type AgentHubConversation, type EgoIdentity } from '../api';
import { useUIStore } from '../store';

type WorkspaceTab = 'screen' | 'browser' | 'data' | 'files';

/**
 * Live Space list cadence. SWR owns the timer: it stops while the tab is
 * hidden (`refreshWhenHidden: false`) and revalidates on focus, so this panel
 * never keeps a background poller of its own.
 */
const EGOSPACE_REFRESH_MS = 5_000;
/** Browser names / platform login state change far less often than Space state. */
const EGOSPACE_META_REFRESH_MS = 15_000;
/** Live Space frame cadence; polling pauses while the document is hidden. */
const SCREEN_REFRESH_MS = 3_000;

/** Real values written by internal/egolite and internal/db; anything else is echoed raw. */
const SPACE_STATE_LABELS: Record<string, { label: string; tone: 'ok' | 'warn' | 'idle' }> = {
  ready: { label: '就绪', tone: 'ok' },
  starting: { label: '启动中', tone: 'idle' },
  manual_takeover: { label: '人工接管', tone: 'warn' },
  result_unknown: { label: '结果未知', tone: 'warn' },
  verification_failed: { label: '核验失败', tone: 'warn' },
  unbound: { label: '未绑定', tone: 'idle' },
};

const SPACE_TONE_CLASS: Record<'ok' | 'warn' | 'idle', string> = {
  ok: 'bg-brand-success/10 text-brand-success',
  warn: 'bg-brand-warning/10 text-brand-warning',
  idle: 'bg-brand-fg/[0.04] text-brand-muted',
};

function spaceState(state: string) {
  return SPACE_STATE_LABELS[state] ?? { label: state || '未知', tone: 'idle' as const };
}


/**
 * The computer panel's top row (屏幕 | 浏览器 | 数据 | 文件) with the close
 * on the right. `computer` is the "screen" tab; here the tab is called
 * 屏幕 because that is what this panel shows first.
 */
const TABS: Array<{ id: WorkspaceTab; label: string; icon: LucideIcon }> = [
  { id: 'screen', label: '屏幕', icon: Monitor },
  { id: 'browser', label: '浏览器', icon: Globe },
  { id: 'data', label: '数据', icon: Database },
  { id: 'files', label: '文件', icon: FolderOpen },
];


/**
 * The computer panel inside the Bot inspector's 电脑 tab: the screen card,
 * and the four sub-panels. Only surfaces that really exist
 * here are interactive — the built-in EgoLite browser and this Mac's runtime
 * status. Cloud desktop, private desktop, auto routing and screen capture have
 * no backend in this project, so they say so and stay disabled instead of
 * pretending to connect.
 */
export function BotWorkspacePanel({
  bot,
  conversations,
  onClose,
  onOpenAdvanced,
}: {
  bot?: AgentBot;
  conversations: AgentHubConversation[];
  onClose: () => void;
  /** 打开 Bot 的记忆/指令（设置放在面板右上角）。 */
  onOpenAdvanced?: () => void;
}) {
  const setActiveTab = useUIStore((state) => state.setActiveTab);
  const toast = useUIStore((state) => state.toast);
  const [tab, setTab] = useState<WorkspaceTab>('screen');

  // Three read-only calls that already exist: this Mac's runtime, and the
  // EgoLite runtime that carries the built-in browser.
  const { data: health, error: healthError } = useSWR('healthz', api.getBackendHealth);
  const { data: doctor, error: doctorError, mutate: mutateDoctor } = useSWR('api/ego/doctor', api.getEgoDoctor);
  // GET /api/ego/spaces returns db.EgoSpace rows verbatim; polling keeps the
  // list current without a second transport. Hidden tabs stop polling.
  const {
    data: spaces = [],
    error: spacesError,
    isLoading: spacesLoading,
    mutate: mutateSpaces,
  } = useSWR('api/ego/spaces', api.getEgoSpaces, {
    refreshInterval: EGOSPACE_REFRESH_MS,
    refreshWhenHidden: false,
    shouldRetryOnError: false,
  });
  // Labels for the Space rows: browser name + bound profile live on the
  // identity, platform counts live on the accounts. Both are existing reads.
  const { data: identities = [] } = useSWR('api/ego/identities', api.getEgoIdentities, {
    refreshInterval: EGOSPACE_META_REFRESH_MS,
    refreshWhenHidden: false,
    shouldRetryOnError: false,
  });
  const { data: accounts = [] } = useSWR('api/accounts', api.getAccounts, {
    refreshInterval: EGOSPACE_META_REFRESH_MS,
    refreshWhenHidden: false,
    shouldRetryOnError: false,
  });

  const identityByID = new Map<number, EgoIdentity>(identities.map((identity) => [identity.id, identity]));
  const accountsByIdentity = new Map<number, Account[]>();
  for (const account of accounts) {
    const list = accountsByIdentity.get(account.identityId) ?? [];
    list.push(account);
    accountsByIdentity.set(account.identityId, list);
  }

  const readySpaces = spaces.filter((space) => space.state === 'ready').length;
  const waitingSpaces = spaces.filter((space) => space.state !== 'ready').length;

  // 实时画面：<img> + 轮询、页面切后台暂停、只读。
  // 取帧是只读的：用户手动接管 Space 时后端回 user_controlled，不抢控制权。
  // 界面直接镜像 EgoLite 的真实 Space：DB 里的绑定可能已过期（Space 被关掉）。
  const { data: liveSpacesResult, mutate: mutateLiveSpaces, isLoading: liveSpacesLoading } = useSWR(
    'api/ego/spaces/live',
    api.getEgoSpacesLive,
    {
      refreshInterval: EGOSPACE_REFRESH_MS,
      refreshWhenHidden: false,
      // 应用刚启动时 Ego 运行时可能还在初始化：一次失败不能把列表永久钉死。
      shouldRetryOnError: true,
      errorRetryInterval: 2000,
      errorRetryCount: 10,
    },
  );
  const liveSpaces = liveSpacesResult?.spaces ?? [];
  // 打开电脑面板/切换 tab 时主动拉一次，不等 15 秒轮询。
  useEffect(() => {
    if (tab !== 'browser' && tab !== 'screen') return;
    void mutateLiveSpaces();
  }, [tab, mutateLiveSpaces]);
  // 用户点过哪个 Space 就固定看哪个；没点过才自动挑一个能实时预览的。
  const [selectedLiveSpaceID, setSelectedLiveSpaceID] = useState(0);
  const namedLiveSpaces = liveSpaces.filter((item) => item.name.trim());
  const previewableLiveSpaces = namedLiveSpaces.filter((item) => item.ownership === 'agent');
  const requestedLiveSpace = selectedLiveSpaceID > 0
    ? liveSpaces.find((item) => item.id === selectedLiveSpaceID)
    : undefined;
  // 「屏幕」tab 是直接看的：不被用户手动选中的控制中 Space 拖住，自动落回可实时预览的那个。
  const activeSpace = (tab === 'screen' && requestedLiveSpace?.ownership !== 'agent' ? undefined : requestedLiveSpace)
    ?? liveSpaces.find((item) => item.ownership === 'agent' && item.name)
    ?? liveSpaces.find((item) => item.ownership === 'agent')
    ?? namedLiveSpaces[0]
    ?? liveSpaces[0]
    ?? null;
  const activeSpaceID = activeSpace?.id ?? 0;
  const { data: spaceTabs, mutate: mutateSpaceTabs } = useSWR(
    activeSpaceID ? ['api/ego/spaces/tabs', activeSpaceID] : null,
    () => api.getEgoSpaceTabs(activeSpaceID),
    { refreshInterval: EGOSPACE_META_REFRESH_MS, refreshWhenHidden: false, shouldRetryOnError: false },
  );

  const activateTab = async (tabID: string) => {
    if (!activeSpaceID) return;
    try {
      await api.activateEgoSpaceTab(activeSpaceID, tabID);
      await mutateSpaceTabs();
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '切换标签页失败', 'error');
    }
  };

  const [screenFrame, setScreenFrame] = useState<{ src: string; url: string; ownership: string; userControlled: boolean; capturedAt: string } | null>(null);
  const [screenError, setScreenError] = useState('');
  const [screenLoading, setScreenLoading] = useState(false);
  const [screenPaused, setScreenPaused] = useState(false);
  useEffect(() => {
    if ((tab !== 'browser' && tab !== 'screen') || !activeSpaceID || screenPaused) return;
    let alive = true;
    let timer: number | undefined;
    const shoot = async () => {
      if (document.visibilityState === 'hidden') {
        timer = window.setTimeout(shoot, SCREEN_REFRESH_MS);
        return;
      }
      setScreenLoading(true);
      try {
        const frame = await api.getEgoSpaceScreen(activeSpaceID);
        if (!alive) return;
        setScreenError('');
        setScreenFrame({
          src: frame.png ? `data:${frame.mime || 'image/jpeg'};base64,${frame.png}` : '',
          url: frame.url,
          ownership: frame.ownership,
          userControlled: frame.user_controlled,
          capturedAt: frame.captured_at || '',
        });
      } catch (reason) {
        if (alive) setScreenError(reason instanceof Error ? reason.message : '读取画面失败');
      } finally {
        if (alive) setScreenLoading(false);
      }
      if (alive) timer = window.setTimeout(shoot, SCREEN_REFRESH_MS);
    };
    void shoot();
    return () => {
      alive = false;
      if (timer) window.clearTimeout(timer);
    };
  }, [tab, activeSpaceID, screenPaused]);

  const [doctorRefreshing, setDoctorRefreshing] = useState(false);
  async function refreshBrowserStatus() {
    setDoctorRefreshing(true);
    try {
      await Promise.all([mutateDoctor(), mutateSpaces()]);
    } finally {
      setDoctorRefreshing(false);
    }
  }

  // Single-flight: EgoLite serialises platform work per Space, so the panel
  // never fires two open calls at once even when the list has several rows.
  const [openingIdentityID, setOpeningIdentityID] = useState<number | null>(null);
  const [focusing, setFocusing] = useState(false);

  async function openSpaceBrowser(identityID: number) {
    if (openingIdentityID !== null) return;
    setOpeningIdentityID(identityID);
    try {
      // POST /api/ego/identities/{id}/open reuses the bound Space, opens at
      // most three creator pages and hands control back to you afterwards.
      const result = await api.openEgoBrowser(identityID, 0);
      await Promise.all([mutateSpaces(), mutateDoctor()]);
      toast.show(
        result.platform_codes.length > 0
          ? `${result.reused ? '沿用' : '新建'} Space #${result.space_id} 打开了 ${result.platform_codes.length} 个页面`
          : `Space #${result.space_id} 没有可打开的平台页面`,
        result.platform_codes.length > 0 ? 'success' : 'info',
      );
    } catch (error) {
      toast.show(error instanceof Error ? error.message : '打开 Ego 浏览器失败', 'error');
    } finally {
      setOpeningIdentityID(null);
    }
  }

  // 用户显式点击才接管：这是唯一允许 takeOverTaskSpace 的路径。
  async function takeBackAndPreview() {
    if (!activeSpaceID || screenLoading) return;
    setScreenLoading(true);
    try {
      const frame = await api.takeOverEgoSpaceScreen(activeSpaceID);
      setScreenError('');
      setScreenFrame({
        src: frame.png ? `data:${frame.mime || 'image/jpeg'};base64,${frame.png}` : '',
        url: frame.url,
        ownership: frame.ownership,
        userControlled: frame.user_controlled,
        capturedAt: frame.captured_at || '',
      });
      await mutateSpaces();
    } catch (reason) {
      setScreenError(reason instanceof Error ? reason.message : '接管失败');
    } finally {
      setScreenLoading(false);
    }
  }

  async function focusEgoLiteWindow() {
    if (focusing) return;
    setFocusing(true);
    try {
      // POST /api/ego/focus only brings the EgoLite window forward.
      await api.focusEgoLite();
      toast.show('EgoLite 窗口已置前', 'success');
    } catch (error) {
      toast.show(error instanceof Error ? error.message : '无法置前 EgoLite', 'error');
    } finally {
      setFocusing(false);
    }
  }

  const lastActivity = conversations
    .map((item) => item.updated_at)
    .filter(Boolean)
    .sort()
    .pop();

  const modelUsage = new Map<string, number>();
  for (const item of conversations) {
    const model = (item.model_id ?? '').trim();
    if (!model) continue;
    modelUsage.set(model, (modelUsage.get(model) ?? 0) + 1);
  }
  const topModel = [...modelUsage.entries()].sort((left, right) => right[1] - left[1])[0];

  const runtimeLine = healthError
    ? '本机运行时：没连上'
    : health
      ? `本机运行时：${health.ready ? '就绪' : '启动中'} · v${health.version}`
      : '本机运行时：读取中…';

  const browserLine = doctorError
    ? '没读到 EgoLite 运行时状态'
    : doctor
      ? doctor.available
        ? `EgoLite 就绪 · ${liveSpaces.length || spaces.length} 个 Space（可实时看 ${previewableLiveSpaces.length}${waitingSpaces ? ` · 待处理 ${waitingSpaces}` : ''}）`
        : `EgoLite 不可用${doctor.missing.length ? ` · 缺 ${doctor.missing.join('、')}` : ''}`
      : '读取中…';

  const liveSpacePicker = namedLiveSpaces.length > 0 ? (
      <div className="rounded-xl bg-brand-surface-raised p-2.5" data-testid="bot-workspace-live-spaces">
        <div className="flex items-center justify-between gap-2 px-0.5">
          <p className="text-[11px] font-semibold">EgoLite Space</p>
          <p className="text-[9px] font-semibold text-brand-muted">
            {namedLiveSpaces.length} 个 · 可实时看 {previewableLiveSpaces.length}
          </p>
        </div>
        <div className="mt-1.5 flex flex-wrap gap-1">
          {namedLiveSpaces.map((space) => {
            const selected = space.id === activeSpace?.id;
            const previewable = space.ownership === 'agent';
            return (
              <button
                key={space.id}
                type="button"
                data-testid={`bot-workspace-live-space-${space.id}`}
                aria-pressed={selected}
                title={previewable ? '实时查看这个 Space' : '这个 Space 由你控制，不会抢控制'}
                onClick={() => setSelectedLiveSpaceID(space.id)}
                className={`flex max-w-full items-center gap-1 rounded-full px-2.5 py-1.5 text-[10px] font-medium transition ${
                  selected
                    ? 'bg-brand-accent text-brand-on-accent'
                    : 'bg-brand-surface text-brand-muted hover:text-brand-fg'
                }`}
              >
                <span className="max-w-[130px] truncate">{space.name}</span>
                {previewable ? null : (
                  <span
                    className={`shrink-0 rounded-full px-1 py-px text-[8px] font-semibold ${
                      selected ? 'bg-brand-on-accent/20 text-brand-on-accent' : 'bg-brand-fg/[0.08] text-brand-muted'
                    }`}
                  >
                    你控制中
                  </span>
                )}
              </button>
            );
          })}
        </div>
        <p className="mt-1.5 px-0.5 text-[9px] leading-4 text-brand-muted">
          点一个 Space 看它的实时画面；标「你控制中」的只显示状态，不会抢你的控制。
        </p>
      </div>
  ) : null;

  return (
    <div className="rounded-xl border border-brand-border" data-testid="bot-workspace-panel">
      <div className="flex items-center gap-1 border-b border-brand-border px-1.5 py-1.5">
        <div className="flex-1" />
        <div
          role="tablist"
          aria-label="电脑面板视图"
          className="flex min-w-0 gap-0.5 overflow-x-auto rounded-full bg-brand-surface-raised p-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
        >
          {TABS.map((item) => {
            const Icon = item.icon;
            const active = tab === item.id;
            return (
              <button
                key={item.id}
                type="button"
                role="tab"
                aria-selected={tab === item.id}
                data-testid={`bot-workspace-tab-${item.id}`}
                onClick={() => setTab(item.id)}
                className={`flex h-7 shrink-0 items-center gap-1 rounded-full px-2.5 text-[10px] font-semibold transition ${
                  active ? 'bg-brand-fg/[0.08] text-brand-fg shadow-sm' : 'text-brand-muted hover:bg-brand-fg/[0.04] hover:text-brand-fg'
                }`}
              >
                <Icon className="h-3 w-3" />
                {item.label}
              </button>
            );
          })}
        </div>
        <div className="flex-1" />
        {onOpenAdvanced ? (
          <button
            type="button"
            data-testid="bot-workspace-settings"
            onClick={onOpenAdvanced}
            aria-label="Bot 设置"
            title="Bot 设置"
            className="flex h-7 w-7 items-center justify-center rounded-md text-brand-muted transition hover:bg-brand-fg/[0.05] hover:text-brand-fg"
          >
            <SlidersHorizontal className="h-3.5 w-3.5" />
          </button>
        ) : null}
        <button
          type="button"
          data-testid="bot-workspace-close"
          onClick={onClose}
          aria-label="关闭电脑面板"
          className="flex h-7 w-7 items-center justify-center rounded-md text-brand-muted transition hover:bg-brand-fg/[0.05] hover:text-brand-fg"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="space-y-3 px-2.5 pb-3">
        {tab === 'screen' ? (
          <>
            <div className="rounded-xl bg-brand-surface-raised p-3" data-testid="bot-workspace-screen">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <p className="min-w-0 truncate text-[11px] font-semibold">
                    {activeSpace?.name || (liveSpacesLoading ? '正在读取 Space…' : `${bot?.name || 'Bot'} 的 Space`)}
                  </p>
                  <p data-testid="bot-workspace-runtime-state" className="mt-0.5 text-[10px] leading-4 text-brand-muted">
                    {screenFrame?.url || runtimeLine}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <span className="rounded-full bg-brand-fg/[0.06] px-2 py-0.5 text-[9px] font-semibold text-brand-fg">
                    EgoLite · {screenPaused ? '已暂停' : '实时'}
                  </span>
                  <button
                    type="button"
                    data-testid="bot-workspace-screen-pause"
                    aria-pressed={screenPaused}
                    title={screenPaused ? '继续刷新画面' : '暂停刷新画面'}
                    onClick={() => setScreenPaused((paused) => !paused)}
                    className="h-7 rounded-full border border-brand-border bg-brand-surface px-2.5 text-[9px] font-semibold text-brand-fg transition hover:bg-brand-fg/[0.06]"
                  >
                    {screenPaused ? '继续' : '暂停'}
                  </button>
                </div>
              </div>
              <div className="mt-2 flex aspect-[16/10] w-full items-center justify-center overflow-hidden rounded-lg border border-brand-border/60 bg-brand-fg/[0.04]">
                {screenFrame?.userControlled ? (
                  <div className="flex flex-col items-center gap-2 px-4 text-center">
                    <p data-testid="bot-workspace-screen-user-controlled" className="text-[10px] leading-4 text-brand-muted">
                      当前没有可实时预览的 Space；由你控制的 Space 不会被抢。
                    </p>
                    <button
                      type="button"
                      data-testid="bot-workspace-screen-fallback"
                      onClick={() => setTab('browser')}
                      className="h-7 rounded-md bg-brand-accent px-2.5 text-[10px] font-semibold text-brand-on-accent transition hover:brightness-110"
                    >
                      去浏览器 tab 查看
                    </button>
                  </div>
                ) : screenFrame?.src ? (
                  <img data-testid="bot-workspace-screen-frame" src={screenFrame.src} alt="EgoLite Space 实时画面" className="h-full w-full object-contain" />
                ) : screenError ? (
                  <p role="alert" data-testid="bot-workspace-screen-error" className="px-4 text-center text-[10px] leading-4 text-brand-warning">{screenError}</p>
                ) : (
                  <p data-testid="bot-workspace-screen-loading" className="px-4 text-center text-[10px] leading-4 text-brand-muted">
                    {activeSpace ? '正在取 Space 画面…' : liveSpacesLoading ? '正在读取 EgoLite Space…' : 'EgoLite 里还没有可用的 Space'}
                  </p>
                )}
              </div>
              {/* 只留一句状态：截图来源与刷新机制对用户不重要，说多了反而是噪音。 */}
              <p data-testid="bot-workspace-screen-note" className="mt-2 text-[9px] leading-4 text-brand-muted">
                {screenPaused ? '已暂停' : '实时'}
                {screenFrame?.capturedAt ? ` · ${new Date(screenFrame.capturedAt).toLocaleTimeString('zh-CN', { hour12: false })}` : ''}
              </p>
            </div>


          </>
        ) : null}

        {tab === 'browser' ? (
          <div className="space-y-2" data-testid="bot-workspace-browser">
            {liveSpacePicker}
            {spaceTabs && spaceTabs.tabs.length > 0 && !spaceTabs.user_controlled ? (
              <div className="flex flex-wrap gap-1" data-testid="bot-workspace-tabs">
                {spaceTabs.tabs.map((tab) => {
                  const active = tab.id === spaceTabs.current;
                  return (
                    <button
                      key={tab.id}
                      type="button"
                      data-testid={`bot-workspace-tab-chip-${tab.id}`}
                      onClick={() => void activateTab(tab.id)}
                      title={tab.url || tab.title}
                      className={`max-w-full truncate rounded-full px-2.5 py-1.5 text-[10px] font-medium transition ${
                        active ? 'bg-brand-accent text-brand-on-accent' : 'bg-brand-surface-raised text-brand-muted hover:text-brand-fg'
                      }`}
                    >
                      {tab.title || tab.url || '标签页'}
                    </button>
                  );
                })}
              </div>
            ) : null}
            <div className="rounded-xl bg-brand-surface-raised p-2.5" data-testid="bot-workspace-browser-live-view">
              <div className="flex items-center justify-between gap-2 px-0.5">
                <p className="min-w-0 truncate text-[11px] font-semibold">
                  {/* 现在在用的 profile 名，极简一行 */}
                  {activeSpace?.name || 'EgoLite 浏览器'}
                </p>
                <span
                  data-testid="bot-workspace-browser-live"
                  className="shrink-0 rounded-full bg-brand-surface px-2 py-0.5 text-[9px] font-semibold text-brand-muted"
                >
                  {screenLoading ? '取帧…' : screenFrame?.userControlled ? '你正在控制' : '实时'}
                </span>
              </div>
              {/* 画面：只读实时帧；用户接管时显示状态，不截图、不抢控制 */}
              <div className="mt-1.5 flex aspect-[16/10] w-full items-center justify-center overflow-hidden rounded-lg border border-brand-border/60 bg-brand-surface">
                {screenFrame?.userControlled ? (
                  <div className="flex flex-col items-center gap-2 px-4 text-center">
                    <p data-testid="bot-workspace-screen-user-controlled" className="text-[10px] leading-4 text-brand-muted">
                      你正在浏览器里手动操作这个 Space，预览已暂停。
                    </p>
                    <div className="flex flex-wrap items-center justify-center gap-1.5">
                      <button
                        type="button"
                        data-testid="bot-workspace-screen-fallback"
                        onClick={() => setTab('screen')}
                        className="h-7 rounded-md bg-brand-accent px-2.5 text-[10px] font-semibold text-brand-on-accent transition hover:brightness-110"
                      >
                        看这台电脑的屏幕
                      </button>
                      <button
                        type="button"
                        data-testid="bot-workspace-screen-takeover"
                        disabled={screenLoading}
                        onClick={() => void takeBackAndPreview()}
                        className="h-7 rounded-md px-2 text-[10px] font-semibold text-brand-muted transition hover:bg-brand-fg/[0.06] hover:text-brand-fg disabled:opacity-45"
                      >
                        {screenLoading ? '接管中…' : '交回控制并看画面'}
                      </button>
                    </div>
                  </div>
                ) : screenFrame?.src ? (
                  <img
                    data-testid="bot-workspace-screen-frame"
                    src={screenFrame.src}
                    alt="浏览器实时画面"
                    className="h-full w-full object-contain"
                  />
                ) : screenError ? (
                  <p role="alert" data-testid="bot-workspace-screen-error" className="px-4 text-center text-[10px] leading-4 text-brand-warning">
                    {screenError}
                  </p>
                ) : (
                  <p data-testid="bot-workspace-screen-loading" className="px-4 text-center text-[10px] leading-4 text-brand-muted">
                    {activeSpace ? '正在取当前页面画面…' : 'EgoLite 里还没有可用的 Space'}
                  </p>
                )}
              </div>
              {screenFrame?.url ? (
                <p className="mt-1 truncate px-0.5 font-mono text-[10px] leading-4 text-brand-muted">{screenFrame.url}</p>
              ) : null}
              <p
                data-testid="bot-workspace-browser-runtime"
                className={`mt-0.5 px-0.5 text-[9px] leading-4 ${doctor?.available ? 'text-brand-muted' : 'text-brand-warning'}`}
              >
                {browserLine}
              </p>
            </div>

            <div className="rounded-xl bg-brand-surface-raised p-2.5" data-testid="bot-workspace-browser-spaces">
              <div className="flex items-center justify-between gap-2 px-0.5">
                <p className="text-[11px] font-semibold">Ego 浏览器 Space</p>
                <p data-testid="bot-workspace-spaces-count" className="text-[9px] font-semibold text-brand-muted">
                  {spacesLoading ? '读取中…' : `已绑定 ${spaces.length} 个 · 就绪 ${readySpaces}`}
                </p>
              </div>

              {spacesError && spaces.length === 0 ? (
                <p
                  role="alert"
                  data-testid="bot-workspace-spaces-error"
                  className="mt-1.5 rounded-lg bg-brand-surface px-2 py-1.5 text-[9px] leading-4 text-brand-warning"
                >
                  没读到 Space 列表。这不代表 Space 已被删除，稍后会按轮询重试。
                </p>
              ) : spacesLoading && spaces.length === 0 ? (
                <p
                  role="status"
                  data-testid="bot-workspace-spaces-loading"
                  className="mt-1.5 rounded-lg bg-brand-surface px-2 py-1.5 text-[9px] leading-4 text-brand-muted"
                >
                  读取 Space 列表…
                </p>
              ) : spaces.length === 0 ? (
                <p
                  data-testid="bot-workspace-spaces-empty"
                  className="mt-1.5 rounded-lg bg-brand-surface px-2 py-1.5 text-[9px] leading-4 text-brand-muted"
                >
                  还没有绑定的 Ego Space。在浏览器工作区里添加浏览器并打开一次后，它的 Space 会出现在这里。
                </p>
              ) : (
                <ul className="mt-1.5 space-y-1.5">
                  {spaces.map((space) => {
                    const identity = identityByID.get(space.identity_id);
                    const identityAccounts = accountsByIdentity.get(space.identity_id) ?? [];
                    const enabledAccounts = identityAccounts.filter((account) => account.enabled);
                    const loggedInAccounts = enabledAccounts.filter((account) => account.loginState === 'logged_in');
                    const state = spaceState(space.state);
                    const opening = openingIdentityID === space.identity_id;
                    const openable = Boolean(doctor?.available) && enabledAccounts.length > 0;
                    return (
                      <li
                        key={space.id}
                        data-testid={`bot-workspace-space-${space.id}`}
                        className="rounded-lg bg-brand-surface px-2 py-1.5"
                      >
                        {/* 极简一行：谁（profile）+ 什么状态 + 登录了几家。
                            Space #/task/ownership/时间戳是运维信息，不摆给用户。 */}
                        <div className="flex items-center justify-between gap-2">
                          <p className="min-w-0 truncate text-[11px] font-semibold">
                            {identity?.profile_name || identity?.name || `浏览器 #${space.identity_id}`}
                          </p>
                          <span
                            data-testid={`bot-workspace-space-state-${space.id}`}
                            className={`shrink-0 rounded-full px-1.5 py-0.5 text-[9px] font-semibold ${SPACE_TONE_CLASS[state.tone]}`}
                          >
                            {state.label}
                          </span>
                        </div>
                        <p className="mt-0.5 text-[9px] leading-4 text-brand-muted">
                          {`${loggedInAccounts.length}/${enabledAccounts.length} 平台已登录`}
                          {space.error_code ? ` · ${space.error_code}` : ''}
                        </p>
                        <div className="mt-1 flex items-center gap-1.5">
                          <button
                            type="button"
                            data-testid={`bot-workspace-space-open-${space.id}`}
                            disabled={!openable || openingIdentityID !== null}
                            title={
                              !doctor?.available
                                ? 'EgoLite 不可用，无法打开浏览器'
                                : enabledAccounts.length === 0
                                  ? '这个浏览器还没有配置平台'
                                  : '复用同一个 Space，最多打开 3 个创作者后台'
                            }
                            onClick={() => void openSpaceBrowser(space.identity_id)}
                            className="inline-flex items-center gap-1 rounded-md border border-brand-border bg-brand-surface px-2 py-1 text-[10px] font-semibold text-brand-fg transition hover:brightness-95 disabled:opacity-45"
                          >
                            {opening ? <Loader2 className="h-3 w-3 animate-spin" /> : <ExternalLink className="h-3 w-3" />}
                            {opening ? '打开中…' : '打开'}
                          </button>
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>

            <div className="flex gap-2">
              <button
                type="button"
                data-testid="bot-workspace-open-browser"
                disabled={!activeSpaceID || screenLoading}
                onClick={() => void takeBackAndPreview()}
                className="flex flex-1 items-center justify-center gap-1 rounded-lg bg-brand-surface-raised px-2 py-1.5 text-[11px] font-semibold text-brand-fg transition hover:brightness-95 disabled:opacity-45"
              >
                <Globe className="h-3 w-3" />
                接管并看画面
              </button>
              <button
                type="button"
                data-testid="bot-workspace-browser-refresh"
                onClick={() => void refreshBrowserStatus()}
                disabled={doctorRefreshing}
                className="flex items-center justify-center gap-1 rounded-lg bg-brand-surface-raised px-2 py-1.5 text-[11px] font-semibold text-brand-fg transition hover:brightness-95 disabled:opacity-50"
              >
                {doctorRefreshing ? <Loader2 className="h-3 w-3 animate-spin" /> : <RotateCcw className="h-3 w-3" />}
                刷新状态
              </button>
            </div>
            <button
              type="button"
              data-testid="bot-workspace-focus-egolite"
              onClick={() => void focusEgoLiteWindow()}
              disabled={focusing || !doctor?.available}
              title={doctor?.available ? '把 EgoLite 窗口带到最前' : 'EgoLite 不可用'}
              className="flex w-full items-center justify-center gap-1 rounded-lg bg-brand-surface-raised px-2 py-1.5 text-[11px] font-semibold text-brand-fg transition hover:brightness-95 disabled:opacity-45"
            >
              {focusing ? <Loader2 className="h-3 w-3 animate-spin" /> : <Monitor className="h-3 w-3" />}
              置前 EgoLite
            </button>
            <p className="px-1 text-[9px] leading-4 text-brand-muted">
              接管、登录和最终提交仍然只在 EgoLite 里做；面板不会读 Cookie、profile 内容或平台密码，也不会替你点发布。
            </p>
          </div>
        ) : null}

        {tab === 'data' ? (
          <div className="space-y-2" data-testid="bot-workspace-data">
            <div className="rounded-lg bg-brand-surface-raised px-3 py-2">
              <p className="text-[10px] text-brand-muted">对话数</p>
              <p className="mt-0.5 text-[11px] leading-4 text-brand-fg">{conversations.length}</p>
            </div>
            <div className="rounded-lg bg-brand-surface-raised px-3 py-2">
              <p className="text-[10px] text-brand-muted">最近活动</p>
              <p className="mt-0.5 text-[11px] leading-4 text-brand-fg">
                {lastActivity ? new Date(lastActivity).toLocaleString('zh-CN', { hour12: false }) : '暂无'}
              </p>
            </div>
            <div className="rounded-lg bg-brand-surface-raised px-3 py-2">
              <p className="text-[10px] text-brand-muted">常用模型</p>
              <p className="mt-0.5 break-words font-mono text-[11px] leading-4 text-brand-fg">
                {topModel ? `${topModel[0]}（${topModel[1]} 次）` : bot?.model || '还没有对话记录'}
              </p>
            </div>
            <p className="px-1 text-[9px] leading-4 text-brand-muted">
              数据卡片未接入：这里只有存储里的对话，没有采集数据集。
            </p>
          </div>
        ) : null}

        {tab === 'files' ? (
          <div className="space-y-2" data-testid="bot-workspace-files">
            <div className="rounded-xl bg-brand-surface-raised p-3">
              <div className="flex items-center gap-1.5">
                <FolderOpen className="h-3.5 w-3.5 text-brand-muted" />
                <p className="text-[11px] font-semibold">文件</p>
              </div>
              <p className="mt-1 text-[10px] leading-4 text-brand-muted">
                没有 per-Bot 文件面板。这个 Bot 的产出落在本机内容库，不在这里。
              </p>
            </div>
            <button
              type="button"
              data-testid="bot-workspace-open-contents"
              onClick={() => setActiveTab('contents')}
              className="flex w-full items-center justify-center gap-1 rounded-lg bg-brand-surface-raised px-2 py-1.5 text-[11px] font-semibold text-brand-fg transition hover:brightness-95"
            >
              <FolderOpen className="h-3 w-3" />
              打开内容库
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
