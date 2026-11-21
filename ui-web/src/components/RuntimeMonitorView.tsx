import { useCallback, useEffect, useMemo, useState } from 'react';
import useSWR from 'swr';
import {
  Activity,
  ArrowRight,
  BookOpen,
  Bot,
  CheckCircle2,
  CircleDashed,
  Database,
  GitBranch,
  Layers3,
  RefreshCw,
  RadioTower,
  Server,
  ShieldCheck,
  WifiOff,
} from 'lucide-react';
import { api, type AgentHubProbe } from '../api';
import { SQLLinkView } from './SQLLinkView';

const STATUS_REFRESH_INTERVAL_MS = 5_000;
const PROVIDER_PROBE_INTERVAL_MS = 30_000;

type Tone = 'ok' | 'warn' | 'error' | 'neutral' | 'busy';

const toneClasses: Record<Tone, { dot: string; text: string; surface: string }> = {
  ok: { dot: 'bg-emerald-500', text: 'text-emerald-700', surface: 'border-emerald-200 bg-emerald-50/70' },
  warn: { dot: 'bg-brand-warning/20', text: 'text-brand-warning', surface: 'border-brand-warning/30 bg-brand-warning/10/70' },
  error: { dot: 'bg-rose-500', text: 'text-rose-700', surface: 'border-rose-200 bg-rose-50/70' },
  neutral: { dot: 'bg-gray-400', text: 'text-gray-600', surface: 'border-brand-border bg-brand-bg/60' },
  busy: { dot: 'bg-brand-accent animate-pulse', text: 'text-brand-fg', surface: 'border-brand-border bg-brand-surface-raised' },
};

function formatTime(value?: string) {
  if (!value) return '尚无记录';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '时间未知' : date.toLocaleString('zh-CN', { hour12: false });
}

function providerStatus(probe?: AgentHubProbe): { label: string; tone: Tone } {
  switch (probe?.state) {
    case 'ready': return { label: '在线', tone: 'ok' };
    case 'limited': return { label: '可用受限', tone: 'warn' };
    case 'probing': return { label: '探测中', tone: 'busy' };
    case 'unavailable':
    case 'incompatible': return { label: '不可用', tone: 'error' };
    default: return { label: '待确认', tone: 'neutral' };
  }
}

interface StatusRowProps {
  title: string;
  detail: string;
  label: string;
  tone: Tone;
  icon: typeof Server;
}

function StatusRow({ title, detail, label, tone, icon: Icon }: StatusRowProps) {
  const classes = toneClasses[tone];
  return (
    <div className="flex min-w-0 items-center gap-3 border-b border-brand-border px-4 py-3 last:border-b-0">
      <div className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border ${classes.surface}`}>
        <Icon className={`h-4 w-4 ${classes.text}`} strokeWidth={1.8} />
      </div>
      <div className="min-w-0 flex-1">
        <p className="truncate text-xs font-semibold text-gray-900">{title}</p>
        <p className="mt-0.5 truncate text-[10px] text-gray-500" title={detail}>{detail}</p>
      </div>
      <span className={`inline-flex shrink-0 items-center gap-1.5 text-[10px] font-semibold ${classes.text}`}>
        <span className={`h-1.5 w-1.5 rounded-full ${classes.dot}`} />
        {label}
      </span>
    </div>
  );
}

interface ANCArchitectureViewProps {
  backendOnline: boolean;
  onOpenMonitor: () => void;
}

function ANCArchitectureView({ backendOnline, onOpenMonitor }: ANCArchitectureViewProps) {
  const stages = [
    ['01', '数据接入', '账号、内容、素材、商品、店铺与平台数据进入同一资产域。', Database],
    ['02', '能力编排', 'tools.Registry 统一暴露能力，Workflow 与 Agent 不绕过控制面。', Layers3],
    ['03', '受控执行', '本地服务、EgoLite Space 与人工接管共同完成真实动作。', ShieldCheck],
    ['04', '证据与复盘', '围绕目标、动作、结果和偏差完成全流程复盘。', GitBranch],
    ['05', '知识主干', '验证过的方法沉淀为知识、Skill 与下一次可调用流程。', BookOpen],
  ] as const;

  return (
    <div data-testid="anc-architecture-view" className="mx-auto w-full max-w-[1440px] space-y-4 pb-8">
      <section data-testid="anc-foundation-architecture" className="ui-card overflow-hidden">
        <div className="grid gap-px bg-brand-border xl:grid-cols-[minmax(0,1.4fr)_minmax(22rem,0.6fr)]">
          <div className="bg-brand-surface px-6 py-6 sm:px-8 sm:py-7">
            <div className="flex items-center gap-2 font-mono text-[10px] font-semibold uppercase tracking-[0.2em] text-brand-accent">
              <RadioTower className="h-3.5 w-3.5" /> 01 / ANC 底座架构
            </div>
            <h1 className="mt-4 max-w-3xl text-3xl font-bold leading-tight text-gray-950 sm:text-4xl">从业务数据到可执行能力</h1>
            <p className="mt-3 max-w-3xl text-sm leading-6 text-gray-600">ANC（AI Native Core）不是单个 Agent，而是一套本地优先的业务底座：统一接入数据、调度工具、控制真实执行、保留证据，再把有效经验写回知识主干。</p>
            <div className="mt-6 flex flex-wrap items-center gap-2">
              <span className={`inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-[10px] font-semibold ${backendOnline ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-brand-warning/30 bg-brand-warning/10 text-brand-warning'}`}>
                <span className={`h-1.5 w-1.5 rounded-full ${backendOnline ? 'bg-emerald-500' : 'bg-brand-warning/20'}`} />
                {backendOnline ? '本地控制面已连接' : '正在连接本地控制面'}
              </span>
              <span className="rounded-full border border-brand-border bg-brand-bg px-3 py-1.5 text-[10px] font-semibold text-gray-600">SQLite 资产真源</span>
              <span className="rounded-full border border-brand-border bg-brand-bg px-3 py-1.5 text-[10px] font-semibold text-gray-600">人工确认边界</span>
            </div>
          </div>

          <div className="px-6 py-5 text-white sm:px-7 sm:py-6" style={{ background: '#09090b' }}>
            <p className="font-mono text-[9px] font-semibold uppercase tracking-[0.18em] text-white/45">One-line architecture</p>
            <p className="mt-4 text-base font-semibold leading-7">业务场景 → tools.Registry → 受控执行 → 证据复盘 → 知识主干</p>
            <p className="mt-3 text-[11px] leading-5 text-white/55">同一能力同时服务 UI、CLI、Workflow 与 Agent，避免每个入口各写一套业务逻辑。</p>
            <div className="mt-6 flex flex-wrap gap-2">
              <button type="button" className="inline-flex min-h-9 items-center gap-2 rounded-md bg-white px-3 py-2 text-[11px] font-semibold text-gray-950 hover:bg-gray-100" onClick={onOpenMonitor}>
                <Activity className="h-3.5 w-3.5" />查看运行监控
              </button>
              <a className="inline-flex min-h-9 items-center gap-2 rounded-md border border-white/15 px-3 py-2 text-[11px] font-semibold text-white/75 hover:bg-white/[0.06] hover:text-white" href="https://github.com/HA7CH/ha7ch-school" target="_blank" rel="noreferrer">
                <BookOpen className="h-3.5 w-3.5" />方法参考
              </a>
            </div>
          </div>
        </div>
      </section>

      <section className="ui-card overflow-hidden">
        <div className="border-b border-brand-border px-5 py-4">
          <p className="font-mono text-[9px] font-semibold uppercase tracking-[0.18em] text-brand-accent">Architecture loop</p>
          <h2 className="mt-1.5 text-base font-bold text-gray-950">五层闭环</h2>
          <p className="mt-1 text-[11px] text-gray-500">每一层都有明确职责，失败和人工接管同样进入复盘。</p>
        </div>
        <div className="grid gap-px bg-brand-border md:grid-cols-5">
          {stages.map(([number, title, detail, Icon], index) => (
            <article key={title} className="relative min-w-0 bg-brand-surface p-5">
              <div className="flex items-center justify-between gap-3">
                <span className="font-mono text-[10px] font-semibold text-brand-accent">{number}</span>
                <Icon className="h-4 w-4 text-gray-400" strokeWidth={1.7} />
              </div>
              <h3 className="mt-8 text-sm font-semibold text-gray-950">{title}</h3>
              <p className="mt-2 text-[11px] leading-5 text-gray-500">{detail}</p>
              {index < stages.length - 1 ? <ArrowRight className="absolute -right-2.5 top-1/2 z-10 hidden h-5 w-5 -translate-y-1/2 rounded-full bg-brand-surface p-1 text-brand-accent md:block" /> : null}
            </article>
          ))}
        </div>
      </section>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.25fr)_minmax(20rem,0.75fr)]">
        <section className="ui-card overflow-hidden">
          <div className="border-b border-brand-border px-5 py-4">
            <h2 className="text-sm font-semibold text-gray-950">真实数据怎样流过 ANC</h2>
            <p className="mt-1 text-[10px] text-gray-500">不复制业务系统，只保存可追溯的资产、状态与证据。</p>
          </div>
          <div className="grid gap-px bg-brand-border sm:grid-cols-2">
            {[
              ['业务资产', '账号会话、内容素材、商品店铺、平台指标', '统一身份与来源'],
              ['控制平面', 'Registry、Workflow、审批与运行队列', '一次定义，多入口复用'],
              ['执行平面', 'EgoLite、平台适配器、本地 CLI、人工接管', '真实动作，失败关闭'],
              ['证据平面', 'Operation Event、Trace、截图、error_code', '可核验、可继续、可复盘'],
            ].map(([title, detail, value]) => (
              <div key={title} className="min-w-0 bg-brand-surface p-5">
                <div className="flex items-start justify-between gap-4">
                  <div className="min-w-0"><h3 className="text-xs font-semibold text-gray-950">{title}</h3><p className="mt-2 text-[11px] leading-5 text-gray-500">{detail}</p></div>
                  <span className="shrink-0 rounded border border-brand-border bg-brand-bg px-2 py-1 font-mono text-[8px] uppercase tracking-[0.1em] text-gray-500">{value}</span>
                </div>
              </div>
            ))}
          </div>
        </section>

        <div className="overflow-hidden rounded-xl border border-gray-900 text-white" style={{ background: '#09090b' }}>
          <div className="border-b border-white/10 px-5 py-4">
            <p className="font-mono text-[9px] font-semibold uppercase tracking-[0.18em] text-white/40">Knowledge Trunk</p>
            <h2 className="mt-2 text-base font-semibold">知识库接入主干，不是堆文档</h2>
          </div>
          <div className="space-y-2 p-5">
            {[
              ['渐进加载', '只读取当前任务需要的知识与 Skill'],
              ['跨会话状态', '保存进度、证据、结论和下一步入口'],
              ['复盘回写', '成功方法与失败原因都形成可继续资产'],
              ['能力再调用', 'Agent 与工作流从主干获取已验证方法'],
            ].map(([title, detail]) => (
              <div key={title} className="rounded-lg border border-white/10 bg-white/[0.04] px-3.5 py-3">
                <div className="flex items-center gap-2"><span className="h-1.5 w-1.5 rounded-full bg-emerald-400" /><h3 className="text-xs font-semibold">{title}</h3></div>
                <p className="mt-1.5 pl-3.5 text-[10px] leading-4 text-white/45">{detail}</p>
              </div>
            ))}
          </div>
        </div>
      </div>

      <section className="ui-card overflow-hidden">
        <div className="grid gap-px bg-brand-border sm:grid-cols-2 lg:grid-cols-4">
          {[
            ['Local first', '数据、日志与资产默认留在本机。'],
            ['One source of truth', 'SQLite 保存状态，磁盘保存大文件。'],
            ['Human in the loop', '登录、验证码与最终提交交给人工确认。'],
            ['Evidence over claims', '没有真实结果，就不写成成功。'],
          ].map(([title, detail]) => (
            <div key={title} className="bg-brand-surface px-5 py-4"><p className="font-mono text-[9px] font-semibold uppercase tracking-[0.12em] text-brand-accent">{title}</p><p className="mt-2 text-[11px] leading-5 text-gray-600">{detail}</p></div>
          ))}
        </div>
      </section>
    </div>
  );
}

export function RuntimeMonitorView() {
  const [showArchitecture, setShowArchitecture] = useState(() => new URLSearchParams(window.location.search).get('section') === 'anc');
  const [probePending, setProbePending] = useState(false);
  const [lastRefreshAt, setLastRefreshAt] = useState(() => new Date());

  const health = useSWR('runtime/health', api.getBackendHealth, {
    refreshInterval: STATUS_REFRESH_INTERVAL_MS,
    shouldRetryOnError: false,
    onSuccess: () => setLastRefreshAt(new Date()),
  });
  const doctor = useSWR('runtime/ego/doctor', api.getEgoDoctor, { refreshInterval: STATUS_REFRESH_INTERVAL_MS, shouldRetryOnError: false });
  const spaces = useSWR('runtime/ego/spaces', api.getEgoSpaces, { refreshInterval: STATUS_REFRESH_INTERVAL_MS, shouldRetryOnError: false });
  const accounts = useSWR('runtime/accounts', api.getAccounts, { refreshInterval: STATUS_REFRESH_INTERVAL_MS, shouldRetryOnError: false });
  const sessionMonitor = useSWR('runtime/ego/session-monitor', api.getEgoSessionMonitor, { refreshInterval: STATUS_REFRESH_INTERVAL_MS, shouldRetryOnError: false });
  const { data: providerData, mutate: mutateProviders } = useSWR('runtime/agent/providers', () => api.agentHubProviders(), { refreshInterval: STATUS_REFRESH_INTERVAL_MS, shouldRetryOnError: false });

  const providerIDs = useMemo(() => providerData?.providers.map((provider) => provider.id) ?? [], [providerData?.providers]);
  const providerKey = providerIDs.join(',');

  const probeProviders = useCallback(async () => {
    const ids = providerKey ? providerKey.split(',') : [];
    if (!ids.length) return;
    setProbePending(true);
    try {
      await Promise.allSettled(ids.map((id) => api.agentHubProviderProbe(id)));
      await mutateProviders();
    } finally {
      setProbePending(false);
    }
  }, [providerKey, mutateProviders]);

  useEffect(() => {
    if (!providerKey) return;
    void probeProviders();
    const timer = window.setInterval(() => void probeProviders(), PROVIDER_PROBE_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [providerKey, probeProviders]);

  const refreshAll = async () => {
    await Promise.all([
      health.mutate(), doctor.mutate(), spaces.mutate(), accounts.mutate(),
      sessionMonitor.mutate(), mutateProviders(),
    ]);
    setLastRefreshAt(new Date());
  };

  const accountItems = accounts.data ?? [];
  const loggedIn = accountItems.filter((account) => account.loginState === 'logged_in').length;
  const loggedOut = accountItems.filter((account) => account.loginState === 'logged_out');
  const unknown = accountItems.filter((account) => account.loginState === 'unknown');
  const runningSpaces = (spaces.data ?? []).filter((space) => space.state === 'running').length;
  const probeMap = new Map((providerData?.probes ?? []).map((probe) => [probe.provider_id, probe]));
  const unavailableProviders = (providerData?.probes ?? []).filter((probe) => probe.state === 'unavailable' || probe.state === 'incompatible').length;
  const verifiedModels = (providerData?.probes ?? []).reduce((total, probe) => total + (probe.models?.length ?? 0), 0);
  const backendOnline = health.data?.ready === true;
  const backendTone: Tone = health.error ? 'error' : backendOnline ? 'ok' : 'busy';

  const selectSection = (architecture: boolean) => {
    const url = new URL(window.location.href);
    if (architecture) url.searchParams.set('section', 'anc');
    else url.searchParams.delete('section');
    window.history.replaceState(null, '', url);
    setShowArchitecture(architecture);
  };

  if (showArchitecture) {
    return <ANCArchitectureView backendOnline={backendOnline} onOpenMonitor={() => selectSection(false)} />;
  }

  return (
    <div data-testid="runtime-monitor" className="mx-auto w-full max-w-[1440px] space-y-4 pb-8">
      <section className="ui-card overflow-hidden">
        <div className="flex flex-wrap items-start justify-between gap-4 border-b border-brand-border px-5 py-4">
          <div>
            <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.18em] text-brand-accent">
              <RadioTower className="h-3.5 w-3.5" /> 01 / Foundation
            </div>
            <h1 className="mt-2 text-xl font-bold tracking-tight text-gray-950">内容与电商底座</h1>
            <p className="mt-1 text-xs text-gray-500">内容、商品、品牌、多平台运营与 Agent 运行态统一底座。</p>
          </div>
          <div className="flex items-center gap-2">
            <span className="hidden text-[10px] text-gray-500 sm:inline">5 秒刷新 · {lastRefreshAt.toLocaleTimeString('zh-CN', { hour12: false })}</span>
            <button type="button" className="ui-btn" onClick={() => selectSection(true)}>
              <Layers3 className="h-3.5 w-3.5" />ANC 架构
            </button>
            <button type="button" className="ui-btn" onClick={() => void refreshAll()}>
              <RefreshCw className="h-3.5 w-3.5" />刷新
            </button>
            <button type="button" className="ui-btn-primary" disabled={probePending || !providerIDs.length} onClick={() => void probeProviders()}>
              <Activity className={`h-3.5 w-3.5 ${probePending ? 'animate-pulse' : ''}`} />探测 Agent
            </button>
          </div>
        </div>

        <div className="grid gap-px bg-brand-border sm:grid-cols-2 xl:grid-cols-4">
          {[
            ['后端服务', backendOnline ? '在线' : health.error ? '已断开' : '连接中', backendOnline ? '本机 API 正常响应' : '等待本机 API', backendTone],
            ['EgoLite Space', `${runningSpaces} / ${(spaces.data ?? []).length}`, '正在运行 / 已绑定', doctor.data?.available ? 'ok' : doctor.error ? 'error' : 'warn'],
            ['账号会话', `${loggedIn} 在线`, `${loggedOut.length} 掉线 · ${unknown.length} 待确认`, loggedOut.length ? 'error' : unknown.length ? 'warn' : 'ok'],
            ['Agent 模型', `${verifiedModels} 已实测`, `${unavailableProviders} 个 Provider 不可用`, unavailableProviders ? 'error' : verifiedModels ? 'ok' : 'neutral'],
          ].map(([title, value, detail, tone]) => {
            const classes = toneClasses[tone as Tone];
            return <div key={title as string} className="bg-brand-surface px-5 py-4">
              <div className="flex items-center justify-between gap-3"><span className="text-[10px] font-semibold uppercase tracking-[0.14em] text-gray-500">{title as string}</span><span className={`h-2 w-2 rounded-full ${classes.dot}`} /></div>
              <p className="mt-3 text-2xl font-bold tracking-tight text-gray-950">{value as string}</p>
              <p className="mt-1 text-[10px] text-gray-500">{detail as string}</p>
            </div>;
          })}
        </div>
      </section>

      <SQLLinkView />

      <div className="grid gap-4 xl:grid-cols-[minmax(0,0.86fr)_minmax(0,1.14fr)]">
        <section className="ui-card overflow-hidden">
          <div className="border-b border-brand-border px-4 py-3"><h2 className="text-sm font-semibold text-gray-900">基础链路</h2><p className="mt-0.5 text-[10px] text-gray-500">空闲不是故障，只有明确失败才标红。</p></div>
          <StatusRow title="Alpha-Nexus API" detail={health.data ? `版本 ${health.data.version || 'dev'}` : '本机端口暂无响应'} label={backendOnline ? '正常' : health.error ? '断开' : '连接中'} tone={backendTone} icon={Server} />
          <StatusRow title="EgoLite Runtime" detail={doctor.data?.missing?.length ? `缺少：${doctor.data.missing.join('、')}` : '浏览器运行能力已就绪'} label={doctor.data?.available ? '可用' : doctor.error ? '不可用' : '检查中'} tone={doctor.data?.available ? 'ok' : doctor.error ? 'error' : 'warn'} icon={RadioTower} />
          <StatusRow title="账号会话监控" detail={sessionMonitor.data?.last_finished_at ? `上次完成 ${formatTime(sessionMonitor.data.last_finished_at)}` : '尚未完成一次持续检查'} label={sessionMonitor.data?.state === 'running' ? '检查中' : sessionMonitor.data?.enabled ? '已启用' : '未启用'} tone={sessionMonitor.data?.state === 'running' ? 'busy' : sessionMonitor.data?.enabled ? 'ok' : 'neutral'} icon={ShieldCheck} />
          <StatusRow
            title="后台登录观察"
            detail={sessionMonitor.data?.last_finished_at ? `上次完成 ${formatTime(sessionMonitor.data.last_finished_at)}` : '等待观察记录'}
            label={sessionMonitor.data?.state === 'running' ? '观察中' : '待机'}
            tone={sessionMonitor.data?.state === 'running' ? 'busy' : 'neutral'}
            icon={Activity}
          />
        </section>

        <section className="ui-card overflow-hidden">
          <div className="flex items-start justify-between gap-3 border-b border-brand-border px-4 py-3">
            <div><h2 className="text-sm font-semibold text-gray-900">Agent Provider 与模型</h2><p className="mt-0.5 text-[10px] text-gray-500">每 30 秒检查本地 CLI 与模型目录，不发起远程模型调用。</p></div>
            <Bot className="h-4 w-4 text-brand-accent" />
          </div>
          <div className="grid gap-px bg-brand-border md:grid-cols-2">
            {providerData?.providers.length ? providerData.providers.map((provider) => {
              const probe = probeMap.get(provider.id);
              const state = providerStatus(probe);
              const classes = toneClasses[state.tone];
              return <article key={provider.id} className="min-w-0 bg-brand-surface p-4">
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0"><p className="truncate text-xs font-semibold text-gray-900">{provider.label}</p><p className="mt-0.5 truncate font-mono text-[9px] text-gray-400">{provider.id}{probe?.version ? ` · ${probe.version}` : ''}</p></div>
                  <span className={`inline-flex shrink-0 items-center gap-1.5 text-[10px] font-semibold ${classes.text}`}><span className={`h-1.5 w-1.5 rounded-full ${classes.dot}`} />{state.label}</span>
                </div>
                <div className="mt-3 flex min-h-6 flex-wrap gap-1.5">
                  {probe?.models?.length ? probe.models.slice(0, 5).map((model) => <span key={model} className="max-w-full truncate rounded border border-brand-border bg-brand-bg px-1.5 py-0.5 font-mono text-[9px] text-gray-600" title={model}>{model}</span>) : <span className="text-[10px] text-gray-400">暂无已实测模型</span>}
                </div>
                <div className="mt-3 flex items-center justify-between gap-3 text-[9px] text-gray-400"><span className="truncate" title={probe?.reason}>{probe?.reason || '本地状态已读取'}</span><span className="shrink-0">{formatTime(probe?.checked_at)}</span></div>
              </article>;
            }) : <div className="col-span-full bg-brand-surface px-4 py-8 text-center text-xs text-gray-500">尚未配置 Agent Provider</div>}
          </div>
        </section>
      </div>

      <div className="grid gap-4">
        <section className="ui-card overflow-hidden">
          <div className="border-b border-brand-border px-4 py-3"><h2 className="text-sm font-semibold text-gray-900">会话异常</h2><p className="mt-0.5 text-[10px] text-gray-500">unknown 仅标为待确认，不视为掉线。</p></div>
          <div className="max-h-80 divide-y divide-brand-border overflow-y-auto">
            {[...loggedOut, ...unknown].length ? [...loggedOut, ...unknown].slice(0, 20).map((account) => {
              const offline = account.loginState === 'logged_out';
              return <div key={account.id} className="flex items-center gap-3 px-4 py-3">
                {offline ? <WifiOff className="h-4 w-4 shrink-0 text-rose-500" /> : <CircleDashed className="h-4 w-4 shrink-0 text-brand-warning" />}
                <div className="min-w-0 flex-1"><p className="truncate text-xs font-semibold text-gray-900">{account.name || account.platformCode}</p><p className="mt-0.5 truncate text-[10px] text-gray-500">{account.platformCode} · {account.loginReasonCode || '暂无明确证据'}</p></div>
                <span className={`shrink-0 text-[10px] font-semibold ${offline ? 'text-rose-700' : 'text-brand-warning'}`}>{offline ? '已掉线' : '待确认'}</span>
              </div>;
            }) : <div className="flex items-center justify-center gap-2 px-4 py-10 text-xs text-gray-500"><CheckCircle2 className="h-4 w-4 text-emerald-600" />未检测到明确掉线账号</div>}
          </div>
        </section>

      </div>
    </div>
  );
}
