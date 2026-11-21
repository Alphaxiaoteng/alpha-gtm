import { lazy, Suspense, useEffect, useState } from 'react';
import { useUIStore } from './store';
import { Layout } from './components/Layout';
const EgoSettingsView = lazy(() => import('./components/EgoSettingsView').then((m) => ({ default: m.EgoSettingsView })));
import { LoginView } from './components/LoginView';
const BrowserWorkspaceView = lazy(() => import('./components/BrowserWorkspaceView').then((m) => ({ default: m.BrowserWorkspaceView })));
const PublishWorkspaceView = lazy(() => import('./components/PublishWorkspaceView').then((m) => ({ default: m.PublishWorkspaceView })));
const SocialMediaWorkspaceView = lazy(() => import('./components/SocialMediaWorkspaceView').then((m) => ({ default: m.SocialMediaWorkspaceView })));
const DashboardView = lazy(() => import('./components/GuanyingWorkspaceViews').then((m) => ({ default: m.GuanyingDataFeishuView })));
const XHSDataDashboardView = lazy(() => import('./components/XHSDataDashboardView').then((m) => ({ default: m.XHSDataDashboardView })));
const ContentCalendarView = lazy(() => import('./components/ContentCalendarView').then((m) => ({ default: m.ContentCalendarView })));
const ShopWorkspaceView = lazy(() => import('./components/ShopWorkspaceView').then((m) => ({ default: m.ShopWorkspaceView })));
const StudioWorkspaceView = lazy(() => import('./components/GuanyingWorkspaceViews').then((m) => ({ default: m.GuanyingSeoBuilderView })));
const RuntimeMonitorView = lazy(() => import('./components/RuntimeMonitorView').then((m) => ({ default: m.RuntimeMonitorView })));
const ShopWorkspaceView = lazy(() => import('./components/ShopWorkspaceView').then((m) => ({ default: m.ShopWorkspaceView })));
const CrawlerView = lazy(() => import('./components/GuanyingWorkspaceViews').then((m) => ({ default: m.GuanyingDatabaseView })));
const CognitiveMemoryView = lazy(() => import('./components/CognitiveMemoryView').then((m) => ({ default: m.CognitiveMemoryView })));
import { LocalLoadingState } from './components/LocalLoadingState';
import { ViewErrorBoundary } from './components/ViewErrorBoundary';
import { AgentDrawer } from './components/AgentDrawer';
import { api, getToken, setToken } from './api';

function App() {
  const { activeTab, setActiveTab, requestPublishRecord } = useUIStore();
  const [authReady, setAuthReady] = useState(false);
  const [needLogin, setNeedLogin] = useState(false);
  const [user, setUser] = useState<string | null>(null);

  useEffect(() => {
    const view = new URLSearchParams(window.location.search).get('view');
    if (view === 'foundation') setActiveTab('monitor');
    if (view === 'shop') setActiveTab('shop');
  }, [setActiveTab]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const st = await api.getAuthStatus();
        if (!st.auth_required) {
          if (!cancelled) {
            setNeedLogin(false);
            setUser(null);
            setAuthReady(true);
          }
          return;
        }
        const tok = getToken();
        if (!tok) {
          if (!cancelled) {
            setNeedLogin(true);
            setAuthReady(true);
          }
          return;
        }
        try {
          const me = await api.me();
          if (!cancelled) {
            setUser(me.username);
            setNeedLogin(false);
          }
        } catch {
          setToken(null);
          if (!cancelled) {
            setNeedLogin(true);
          }
        }
      } catch {
        // backend down: show app shell errors later
        if (!cancelled) setNeedLogin(false);
      } finally {
        if (!cancelled) setAuthReady(true);
      }
    })();

    const onAuth = () => {
      setToken(null);
      setNeedLogin(true);
      setUser(null);
    };
    window.addEventListener('alpha-nexus-egolite:auth-required', onAuth);
    return () => {
      cancelled = true;
      window.removeEventListener('alpha-nexus-egolite:auth-required', onAuth);
    };
  }, []);

  useEffect(() => {
    const desktop = window.electronAPI;
    if (!desktop?.isElectron || !desktop.showPublishNotification) return;
    let cancelled = false;
    let initialized = false;
    const seen = new Set<number>();
    const allowed = new Set(['publish.login_required', 'publish.failed', 'publish.result_unknown', 'publish.completed']);
    const poll = async () => {
      try {
        const items = await api.getNotifications();
        if (cancelled) return;
        if (!initialized) {
          items.forEach((item) => seen.add(item.id));
          initialized = true;
          return;
        }
        for (const item of [...items].reverse()) {
          if (seen.has(item.id)) continue;
          seen.add(item.id);
          if (!item.read && allowed.has(item.type)) {
            await desktop.showPublishNotification?.(item.id);
          }
        }
      } catch {
        // System notifications are presentation-only and never block publish.
      }
    };
    void poll();
    const timer = window.setInterval(() => void poll(), 5000);
    const unsubscribe = desktop.onOpenPublishRecord?.((target) => {
      if (!target || !Number.isSafeInteger(target.relatedID) || target.relatedID <= 0) return;
      requestPublishRecord(target);
      setActiveTab('records');
    });
    return () => {
      cancelled = true;
      window.clearInterval(timer);
      unsubscribe?.();
    };
  }, [requestPublishRecord, setActiveTab]);

  if (!authReady) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-brand-bg">
        <LocalLoadingState
          label="正在打开 Alpha-Nexus EgoLite"
          detail="连接本地工作区与安全服务"
          className="w-full max-w-md"
        />
      </div>
    );
  }

  if (needLogin) {
    return (
      <LoginView
        onSuccess={(username) => {
          setUser(username);
          setNeedLogin(false);
        }}
      />
    );
  }

  const renderView = () => {
    switch (activeTab) {
      case 'shop':
        return <ShopWorkspaceView />;
      case 'dashboard':
        return <DashboardView />;
      case 'crawler':
        return <CrawlerView />;
      case 'logs':
        return <EgoSettingsView initialSection="logs" />;
      case 'memory':
        return <CognitiveMemoryView />;
      case 'contents':
        return <SocialMediaWorkspaceView key="social-contents" initialSection="article" />;
      case 'studio':
        return <StudioWorkspaceView />;
      case 'templates':
        return <SocialMediaWorkspaceView key="social-content-templates" initialSection="article" />;
      case 'publish':
        return <PublishWorkspaceView key="publish-run" initialEntry="publish" />;
      case 'metrics':
        return <PublishWorkspaceView key="publish-metrics" initialEntry="metrics" />;
      case 'records':
        return <PublishWorkspaceView key="publish-records" initialEntry="records" />;
      case 'accounts':
        return <BrowserWorkspaceView key="ego-accounts" initialSection="accounts" />;
      case 'fleet':
        return <BrowserWorkspaceView key="ego-spaces" initialSection="fleet" />;
      case 'proxies':
        return <EgoSettingsView />;
      case 'batch':
        return <PublishWorkspaceView key="publish-batch" initialEntry="batch" />;
      case 'notifications':
        return <PublishWorkspaceView key="publish-notifications" initialEntry="notifications" />;
      case 'social-media':
        return <SocialMediaWorkspaceView />;
      case 'skills':
        return <EgoSettingsView initialSection="skills" />;
      case 'knowledge':
        return <CrawlerView />;
      case 'calendar':
        return <ContentCalendarView />;
      case 'xhs-data':
        return <XHSDataDashboardView />;
      // The agent conversation stage is mounted persistently via the
      // agentStage prop; switching tabs must never unmount a running run.
      case 'agent':
      // The Bot page shares the persistent stage: BotRail + the same agent
      // surface, so switching between chat and a persona never remounts a run.
      case 'bot':
        return null;
      case 'terminal':
        return <DashboardView />;
      case 'monitor':
        return <RuntimeMonitorView />;
      case 'settings':
        return <EgoSettingsView />;
      default:
        return <BrowserWorkspaceView key="ego-default" initialSection="overview" />;
    }
  };

  return (
    <Layout
      authUser={user}
      agentStage={<AgentDrawer displayMode="stage" />}
    >
      {/* 页面级懒加载：首屏只加载当前页，其余按需分块。错误边界只包页面，
          侧栏与常驻对话不会因为一个页面崩溃而一起消失。 */}
      <ViewErrorBoundary label={activeTab}>
        <Suspense fallback={<LocalLoadingState label="正在打开页面" detail="按需加载这个工作区" className="h-full" />}>
          {renderView()}
        </Suspense>
      </ViewErrorBoundary>
    </Layout>
  );
}

export default App;
