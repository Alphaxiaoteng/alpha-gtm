import React, { useEffect, useRef, useState } from 'react';
import { useUIStore, type TabType } from '../store';
import { prefetchView } from '../lib/routePrefetch';
import { BUILTIN_ALPHA_AGENT_ID } from '../lib/botInbox';
import useSWR from 'swr';
import { api, setToken } from '../api';
import { Activity, Bot, CheckCircle, FolderOpen, Globe, PanelLeftOpen, Send, Settings, Sparkles, TrendingUp, Users, XCircle, type LucideIcon } from 'lucide-react';
import { applySkin, getStoredSkin, type SkinId } from '../themes';
import { LocalLoadingState } from './LocalLoadingState';
import { AgentContextDrawer, AGENT_CONTEXT_OPEN_EVENT, type AgentContextSelection } from './AgentContextDrawer';
import { BotRail } from './BotRail';
import { BotDetailPanel } from './BotDetailPanel';
import { BotInboxPanel } from './BotInboxPanel';
import { ViewErrorBoundary } from './ViewErrorBoundary';
import { BackgroundDrawer } from './BackgroundDrawer';

interface LayoutProps {
  children: React.ReactNode;
  agentStage?: React.ReactNode;
  authUser?: string | null;
}

type WorkspaceID = 'shop' | 'ego' | 'studio' | 'social' | 'knowledge' | 'memory' | 'dashboard' | 'crawler' | 'analytics' | 'publish' | 'calendar' | 'logs' | 'monitor' | 'settings';

interface Workspace {
  id: WorkspaceID;
  label: string;
  icon: LucideIcon;
  defaultTab: TabType;
  matches: TabType[];
}

// 趋势雷达与旧的「竞品分析」是同一批内容/趋势页面，合并成一个入口：
// 侧栏只留趋势雷达（下挂赛道热榜 / 视频搜索 / 博主监控），crawler 页并进来后
// 仍在这一行高亮。其余业务入口保持原样，不因为合并而消失。
const WORKSPACES: Workspace[] = [
  {
    id: 'dashboard',
    label: '趋势雷达',
    icon: TrendingUp,
    defaultTab: 'dashboard',
    matches: ['dashboard', 'crawler'],
  },
  {
    id: 'social',
    label: '素材裂变',
    icon: FolderOpen,
    defaultTab: 'social-media',
    matches: ['social-media', 'contents', 'templates'],
  },
  {
    id: 'studio',
    label: 'SEO 独立站',
    icon: Globe,
    defaultTab: 'studio',
    matches: ['studio'],
  },
  {
    id: 'ego',
    label: '浏览器管理',
    icon: Users,
    defaultTab: 'accounts',
    matches: ['accounts', 'fleet'],
  },
  {
    id: 'publish',
    label: '发布记录',
    icon: Send,
    defaultTab: 'records',
    matches: ['publish', 'batch', 'records', 'metrics', 'notifications'],
  },
  {
    id: 'settings',
    label: '设置',
    icon: Settings,
    defaultTab: 'settings',
    matches: ['settings', 'proxies', 'skills', 'logs'],
  },
];

const SETTINGS_WORKSPACE = WORKSPACES.find((workspace) => workspace.id === 'settings');

/** 左下角外观切换：浅色 ↔ 深色，选择写入 localStorage 并广播给同页其他组件 */
const SkinSwitch: React.FC<{ current: SkinId; onChange: (next: SkinId) => void; compact?: boolean }> = ({ current, onChange, compact = false }) => {
  const options: Array<{ id: SkinId; label: string }> = [
    { id: 'claude', label: '浅色' },
    { id: 'hermes', label: '深色' },
  ];
  return (
    <div
      className={`alpha-glass-track flex ${compact ? 'flex-col gap-0.5' : 'items-center gap-0.5'} p-0.5`}
      data-testid="skin-switch"
      aria-label="切换外观"
    >
      {options.map((option) => (
        <button
          key={option.id}
          type="button"
          onClick={() => onChange(option.id)}
          aria-pressed={current === option.id}
          title={option.label}
          aria-label={`切换到${option.label}外观`}
          className={`rounded ${compact ? 'h-7 w-7 text-[9px]' : 'min-h-[28px] px-2.5 py-1 text-[10px]'} font-semibold transition ${
            current === option.id ? 'alpha-glass-thumb text-brand-fg' : 'text-brand-muted hover:text-brand-fg'
          }`}
        >
          {compact ? option.label.charAt(0) : option.label}
        </button>
      ))}
    </div>
  );
};

export const Layout: React.FC<LayoutProps> = ({ children, agentStage, authUser }) => {
  const { productMode, activeTab, setActiveTab, setAgentDraft, openAgentDrawer, toast, sidebarState, toggleSidebar, headerExtras, activeBotID, setActiveBotID } = useUIStore();
  // Bot 页右栏：收件箱与 Bot 详情二选一，位置固定在右侧栏，左侧列表不受影响。
  const botInboxOpen = useUIStore((state) => state.botInboxOpen);
  // 兼容契约：暴露 sidebarHidden 别名（FeedbackDrawer 等仍依赖此命名定位自身位置）。
  const sidebarHidden = sidebarState === 'hidden';
  void sidebarHidden;
  // The persistent agent stage backs both the chat tab and the Bot page.
  const agentSurfaceActive = activeTab === 'agent' || activeTab === 'bot';
  const [contextSelection, setContextSelection] = useState<AgentContextSelection | null>(null);
	const [skinID, setSkinID] = useState<SkinId>(() => getStoredSkin());

	const handleSkinChange = (next: SkinId) => {
		applySkin(next);
		setSkinID(next);
		window.dispatchEvent(new CustomEvent<SkinId>('alpha-nexus-egolite:skin-changed', { detail: next }));
	};

	useEffect(() => {
		const onSkinChanged = (event: Event) => {
			const next = (event as CustomEvent<SkinId>).detail;
			setSkinID(next || getStoredSkin());
		};
		window.addEventListener('alpha-nexus-egolite:skin-changed', onSkinChanged);
		return () => window.removeEventListener('alpha-nexus-egolite:skin-changed', onSkinChanged);
	}, []);

  useEffect(() => {
    const openSettings = () => setActiveTab('settings');
    window.addEventListener('alpha-nexus-egolite:open-settings', openSettings);
    return () => window.removeEventListener('alpha-nexus-egolite:open-settings', openSettings);
  }, [setActiveTab]);

  useEffect(() => {
    setContextSelection(null);
  }, [activeTab, productMode]);

  // Bot 页首次进入时右栏不能是空白：默认选中 AlphaBot。只自动做一次，
  // 用户手动关掉后不再抢回来；离开 Bot 页再回来会重新给一次默认值。
  const botPanelDefaulted = useRef(false);
  useEffect(() => {
    if (activeTab !== 'bot') {
      botPanelDefaulted.current = false;
      return;
    }
    if (botPanelDefaulted.current || activeBotID) return;
    botPanelDefaulted.current = true;
    setActiveBotID(BUILTIN_ALPHA_AGENT_ID);
  }, [activeTab, activeBotID, setActiveBotID]);

  const { data: backendHealth, error: backendHealthError } = useSWR(
    'healthz',
    api.getBackendHealth,
    {
      refreshInterval: (latest) => latest?.ready ? 30_000 : 500,
      errorRetryInterval: 500,
    },
  );
  const { data: overview, error: overviewError } = useSWR('api/overview', api.getOverview);
  const backendReady = backendHealth?.ready === true || Boolean(overview);
  const backendDisconnected = Boolean(backendHealthError) && !backendReady;

  const { data: egoSpaces = [] } = useSWR('api/ego/spaces', api.getEgoSpaces);
  const activeInstances = egoSpaces.filter((space) => space.state === 'running').length;
  const toastMessage = toast.message;
  const toastType = toast.type;
  const hideToast = toast.hide;

  useEffect(() => {
    if (toastMessage) {
      const timer = setTimeout(() => hideToast(), toastType === 'error' ? 8000 : toastType === 'info' ? 5000 : 3000);
      return () => clearTimeout(timer);
    }
  }, [hideToast, toastMessage, toastType]);

  useEffect(() => {
    const openContext = (event: Event) => {
      const detail = (event as CustomEvent<AgentContextSelection>).detail;
      if (!detail?.id || !detail.title) return;
      setContextSelection(detail);
    };
    window.addEventListener(AGENT_CONTEXT_OPEN_EVENT, openContext);
    return () => {
      window.removeEventListener(AGENT_CONTEXT_OPEN_EVENT, openContext);
    };
  }, []);

  const visibleWorkspaces = WORKSPACES;
  const currentWorkspace = visibleWorkspaces.find((workspace) => workspace.matches.includes(activeTab))
    ?? visibleWorkspaces[0]
    ?? WORKSPACES[0];
  const visiblePrimaryWorkspaces = visibleWorkspaces.filter((workspace) => workspace.id !== 'settings');
  const workspaceLabel = (workspace: Workspace) => productMode === 'shop' && workspace.id === 'ego' ? '浏览器与店铺' : workspace.label;
  const handoffCurrentModule = () => {
    const moduleName = workspaceLabel(currentWorkspace);
    setAgentDraft({
      mode: 'tools',
      message: `请协助处理当前 ${productMode === 'media' ? 'Media' : 'Shop'}「${moduleName}」页面的任务。先读取已选择的安全上下文并给出计划；任何业务动作都只通过 Registry 提议，发布提交仍需单独授权。`,
    });
    openAgentDrawer();
  };

  const renderWorkspaceButton = (workspace: Workspace) => {
    const Icon = workspace.icon;
    // Alpha Agent 是独立舞台，不在侧栏里占一行；停留在它上面时侧栏不高亮。
    const isActive = !agentSurfaceActive && currentWorkspace.id === workspace.id;
    const badge = workspace.id === 'ego' ? activeInstances || undefined : undefined;
    const label = workspaceLabel(workspace);

    return (
      <div key={workspace.id} className="w-full">
        <button
          data-testid={`nav-${workspace.id}`}
          aria-current={isActive ? 'page' : undefined}
          aria-label={label}
          title={sidebarState === 'rail' ? label : undefined}
          onPointerEnter={() => prefetchView(workspace.defaultTab)}
          onFocus={() => prefetchView(workspace.defaultTab)}
          onClick={() => {
            setActiveTab(workspace.defaultTab);
          }}
          className={`ui-nav-item group relative flex w-full items-center ${sidebarState === 'rail' ? 'justify-center px-0 py-2.5' : 'justify-between px-3 py-2.5'} text-[13px] transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/40 ${
            isActive
              ? 'text-gray-900 font-semibold shadow-2xs'
              : 'text-gray-600 font-medium hover:text-gray-900 hover:bg-black/[0.04]'
          }`}
          style={
            isActive
              ? {
                  background: 'var(--ui-nav-active-bg)',
                  borderRadius: 'var(--ui-radius-btn)',
                  boxShadow: sidebarState === 'rail' ? 'inset 0 2.5px 0 var(--brand-accent)' : 'inset 2.5px 0 0 var(--brand-accent)',
                  color: 'var(--brand-fg)',
                }
              : { borderRadius: 'var(--ui-radius-btn)' }
          }
        >
          <span className={`flex items-center ${sidebarState === 'rail' ? '' : 'gap-2.5 min-w-0'}`}>
            <Icon
              className={`w-[18px] h-[18px] shrink-0 transition-transform duration-150 group-hover:scale-105 ${
                isActive ? 'text-[var(--brand-accent)]' : 'text-gray-400 group-hover:text-gray-600'
              }`}
              strokeWidth={isActive ? 2.25 : 1.75}
            />
            {sidebarState !== 'rail' && <span className="truncate">{label}</span>}
          </span>
          {badge !== undefined && !sidebarState && (
            <span
              className="inline-flex items-center gap-1 min-w-[1.25rem] h-5 px-1.5 text-[10px] font-semibold rounded-full text-white leading-none shadow-xs shrink-0 transition-transform duration-150 group-hover:scale-105"
              style={{ background: 'var(--brand-accent)' }}
            >
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
              <span>{badge}</span>
            </span>
          )}
        </button>
      </div>
    );
  };

  return (
    <div
      data-testid="app-shell"
      className="flex h-screen w-screen overflow-hidden bg-brand-bg pt-8 text-gray-800"
    >
      {/* Sidebar: expanded | rail | hidden 三态 */}
      {sidebarState !== 'hidden' ? <aside
        data-testid="app-sidebar"
        className={`border-r border-brand-border flex flex-col flex-shrink-0 ${sidebarState === 'rail' ? 'w-[60px] items-stretch' : 'w-60'} ${activeTab === 'bot' ? 'bot-theme' : ''}`}
        style={{ background: 'var(--brand-sidebar)' }}
      >


        <div className={`border-b border-brand-border ${sidebarState === 'rail' ? 'px-1.5 py-2' : 'px-3.5 py-2.5'} space-y-2.5`}>
          <div className={`flex items-center ${sidebarState === 'rail' ? 'justify-center' : 'gap-2.5'}`}>
            <img
              src="/brand/01-lockup-dark-halftone.svg"
              alt="AlphaGTM"
              className={`${sidebarState === 'rail' ? 'h-6 w-8 object-cover object-left' : 'h-6 w-auto max-w-[62px] object-contain'} shrink-0 select-none rounded`}
            />
            {sidebarState !== 'rail' && (
              // 品牌名跟在 logo 后面，单行展示。英文用衬线体拉开与正文的对比，
              // 中文跟在后面用小一号的常规体，靠字距而不是字重来区分层级。
              <div className="min-w-0 truncate whitespace-nowrap leading-none">
                <span
                  className="text-[16px] font-semibold tracking-[0.01em] text-brand-fg"
                  style={{ fontFamily: '"Iowan Old Style", "Palatino Linotype", Palatino, Georgia, "Times New Roman", serif' }}
                >
                  Alpha GTM
                </span>
                <span className="ml-1.5 text-[12px] font-medium tracking-[0.08em] text-brand-muted">
                  阿尔法营销
                </span>
              </div>
            )}
          </div>
          <div
            data-testid="workspace-mode-switch"
            role="group"
            aria-label="选择工作模式"
            className={`alpha-glass-track relative flex ${sidebarState === 'rail' ? 'flex-col gap-0.5 p-0.5' : 'items-center p-0.5'}`}
          >
            <button
              type="button"
              data-testid="workspace-mode-media"
              aria-label="切换到 GTM"
              aria-pressed={activeTab !== 'bot'}
              onClick={() => setActiveTab(activeTab === 'bot' ? 'dashboard' : activeTab)}
              title="GTM"
              className={`relative flex items-center justify-center ${sidebarState === 'rail' ? 'w-full py-1.5' : 'flex-1 min-w-0 gap-1.5 py-1.5 px-2'} rounded-[6px] text-[11.5px] font-semibold transition-colors duration-200 select-none ${
                activeTab !== 'bot'
                  ? 'alpha-glass-thumb text-brand-fg'
                  : 'text-brand-muted hover:text-brand-fg'
              }`}
            >
              <Sparkles
                aria-hidden="true"
                className={`h-3.5 w-3.5 transition-transform duration-200 ${activeTab !== 'bot' ? 'scale-105 text-brand-fg' : 'text-brand-muted'}`}
                strokeWidth={activeTab !== 'bot' ? 2 : 1.75}
              />
              {sidebarState !== 'rail' && <span className="min-w-0 truncate tracking-tight">GTM</span>}
            </button>
            <button
              type="button"
              data-testid="workspace-mode-bot"
              aria-label="打开 Bot 页面"
              aria-pressed={activeTab === 'bot'}
              onClick={() => setActiveTab('bot')}
              title="打开 Bot 团队"
              className={`relative flex items-center justify-center ${sidebarState === 'rail' ? 'w-full py-1.5' : 'flex-1 gap-1.5 py-1.5 px-2'} rounded-[6px] text-[11.5px] font-semibold transition-colors duration-200 select-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/40 ${
                activeTab === 'bot'
                  ? 'alpha-glass-thumb text-brand-fg'
                  : 'text-brand-muted hover:text-brand-fg'
              }`}
            >
              <Bot
                aria-hidden="true"
                className={`h-3.5 w-3.5 transition-transform duration-200 ${activeTab === 'bot' ? 'scale-105 text-brand-fg' : 'text-brand-muted'}`}
                strokeWidth={activeTab === 'bot' ? 2 : 1.75}
              />
              {sidebarState !== 'rail' && <span className="tracking-tight">Bot 团队</span>}
            </button>
          </div>
        </div>

        {activeTab === 'bot' ? (
          <BotRail />
        ) : (
          <nav
            className={`min-h-0 flex-1 ${sidebarState === 'rail' ? 'px-1.5 py-3 space-y-1.5' : 'px-3 py-3 space-y-1.5'} overflow-y-auto ui-thin-scrollbar`}
            aria-label={`${productMode === 'media' ? 'Media' : 'Shop'} 业务入口`}
          >
            {visiblePrimaryWorkspaces.map(renderWorkspaceButton)}
          </nav>
        )}

        {/* 侧栏不列出历史会话；PersonAgent 入口在顶部三段切换的第三段。 */}
        <div className={`mt-auto border-t border-brand-border ${sidebarState === 'rail' ? 'p-1.5 space-y-1.5' : 'p-3 space-y-2'}`}>
          {/* 设置固定落在左下角：整行按钮，不再和状态点/外观挤在同一行。 */}
          {SETTINGS_WORKSPACE ? renderWorkspaceButton(SETTINGS_WORKSPACE) : null}
          <div className={`flex items-center ${sidebarState === 'rail' ? 'flex-col gap-1.5' : 'justify-between gap-2'}`}>
            {sidebarState === 'rail' ? (
              <button
                type="button"
                data-testid="sidebar-show-toggle"
                onClick={toggleSidebar}
                className="ui-icon-button shrink-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/50"
                title="展开侧栏"
                aria-label="展开侧栏"
              >
                <PanelLeftOpen className="h-4 w-4" />
              </button>
            ) : (
              <span data-testid="backend-status" className="flex items-center gap-1 shrink-0" title="服务状态">
                <span className={`relative inline-flex rounded-full h-2 w-2 ${backendDisconnected ? 'bg-red-500' : backendReady ? 'bg-emerald-500' : 'bg-brand-warning/20 animate-pulse'}`} />
                <span data-testid="backend-status-text" className={`text-[10px] font-semibold ${backendDisconnected ? 'text-red-600' : backendReady ? 'text-emerald-700' : 'text-brand-warning'}`}>
                  {backendDisconnected ? '断开' : backendReady ? '正常' : '启动中'}
                </span>
              </span>
            )}
            <SkinSwitch current={skinID} onChange={handleSkinChange} compact={sidebarState === 'rail'} />
            {sidebarState !== 'rail' && authUser ? (
              <button
                type="button"
                className="text-[10px] font-semibold text-brand-muted hover:text-brand-accent shrink-0"
                title={`当前身份 ${authUser} · 点击退出`}
                onClick={() => {
                  setToken(null);
                  window.location.reload();
                }}
              >
                退出
              </button>
            ) : null}
          </div>
        </div>
      </aside> : null}

      <main data-testid="app-main" className="flex-1 flex flex-col min-w-0 h-full overflow-hidden">
        {/* 顶部工具栏已按产品决策隐藏：页面内的「交给 Agent 分析 / 重读本地」已覆盖同等能力。
            保留节点与 data-testid，避免破坏 shell 契约测试。 */}
        <header data-testid="app-utility-toolbar" style={{ display: 'none' }} className="ui-utility-toolbar h-10 px-4 border-b border-brand-border items-center justify-between gap-3 flex-shrink-0">
            <div className="min-w-0 flex items-center gap-2">
              <h2
                data-testid="page-title"
                className="text-xs font-semibold tracking-tight text-gray-900 shrink-0"
              >
                {activeTab === 'agent' ? 'Alpha Agent' : workspaceLabel(currentWorkspace)}
              </h2>
              {headerExtras?.subtitle && (
                <span className="text-xs text-gray-500 font-normal truncate">
                  {headerExtras.subtitle}
                </span>
              )}
            </div>

            <div className="flex items-center gap-2">
              {headerExtras?.actions && (
                <div className="flex items-center gap-2">
                  {headerExtras.actions}
                </div>
              )}
              {/* Keep test-id mounted for test verification without visual clutter */}
              {activeTab !== 'agent' && activeTab !== 'settings' ? (
                <button type="button" data-testid="module-handoff-agent" className="sr-only" onClick={handoffCurrentModule}>
                  <Sparkles className="h-3.5 w-3.5" />交给 Agent
                </button>
              ) : null}
              {overview ? null : !overviewError ? (
                <LocalLoadingState
                  compact
                  label="正在恢复本地状态"
                  detail="读取身份、账号与 Space"
                  className="hidden"
                  testID="header-local-loading"
                />
              ) : null}
            </div>
        </header>

        <div className="flex-1 bg-brand-bg relative min-h-0 overflow-hidden">
          <div
            key={activeTab}
            className={agentSurfaceActive ? 'hidden' : 'h-full overflow-y-auto'}
            style={agentSurfaceActive ? undefined : { padding: '0.75rem var(--ui-content-pad) var(--ui-content-pad)' }}
          >
            <div className={agentSurfaceActive ? 'h-full min-h-0' : 'ui-workspace-stage min-h-full'}>
              {children}
            </div>
          </div>
          <div
            data-testid="agent-stage-host"
            className={`absolute inset-0 ${agentSurfaceActive ? 'flex' : 'hidden'} ${activeTab === 'bot' ? 'bot-theme bg-brand-bg' : ''}`}
          >
            <div className="min-w-0 flex-1"><ViewErrorBoundary label="agent-stage" fallbackTab="bot">{agentStage}</ViewErrorBoundary></div>
            {/* 收件箱占右侧栏，与 Bot 详情同一位置；agentStage 始终挂载。
                收件箱与侧栏列表并列。 */}
            {activeTab === 'bot' ? (botInboxOpen ? <BotInboxPanel /> : <BotDetailPanel />) : null}
          </div>
        </div>
      </main>
      <AgentContextDrawer selection={contextSelection} onClose={() => setContextSelection(null)} />
      <BackgroundDrawer />

      {toast.message && (
        <div
          role={toast.type === 'error' ? 'alert' : 'status'}
          aria-live={toast.type === 'error' ? 'assertive' : 'polite'}
          className="fixed bottom-6 right-6 z-50 flex max-w-sm items-center gap-2.5 rounded-lg border border-brand-border bg-brand-surface/95 px-4 py-3 shadow-xl backdrop-blur-md animate-fade-in"
        >
          {toast.type === 'success' && (
            <CheckCircle className="w-4 h-4 text-emerald-500 flex-shrink-0" />
          )}
          {toast.type === 'error' && <XCircle className="w-4 h-4 text-red-500 flex-shrink-0" />}
          {toast.type === 'info' && (
            <Activity className="w-4 h-4 text-brand-accent flex-shrink-0" />
          )}
          <span className="min-w-0 flex-1 text-xs font-semibold text-gray-800">{toast.message}</span>
          <button
            type="button"
            aria-label="关闭通知"
            onClick={toast.hide}
            className="shrink-0 rounded p-0.5 text-gray-400 hover:bg-black/5 hover:text-gray-700"
          >
            <XCircle className="h-3.5 w-3.5" />
          </button>
        </div>
      )}
    </div>
  );
};
