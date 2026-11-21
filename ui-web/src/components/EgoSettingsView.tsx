import { useEffect, useState } from 'react';
import useSWR from 'swr';
import { Activity, Blocks, Bot, Brain, CheckCircle2, Clock3, Gauge, HardDrive, Info, Keyboard, LogOut, Palette, RefreshCw, ScrollText, ShieldCheck, Table2, TerminalSquare, UserRound, XCircle } from 'lucide-react';
import { api, setToken } from '../api';
import { useUIStore } from '../store';
import { applySkin, getStoredSkin, type SkinId } from '../themes';
import { AlphaAgentSettingsSection } from './AlphaAgentSettingsSection';
import { BotSettingsPanel } from './BotSettingsPanel';
import { SkillLibraryView } from './SkillLibraryView';
import { StorageSettingsSection } from './StorageSettingsSection';
import { CognitiveMemoryView } from './CognitiveMemoryView';

type SettingsSection = 'account' | 'appearance' | 'shortcuts' | 'agent' | 'personagent' | 'runtime' | 'skills' | 'feishu' | 'memory' | 'storage' | 'logs' | 'about';

const SETTINGS_GROUPS: Array<{ title: string; items: Array<{ id: SettingsSection; label: string; icon: typeof Gauge }> }> = [
  { title: '个人', items: [
    { id: 'account', label: '账户与身份', icon: UserRound },
    { id: 'appearance', label: '外观', icon: Palette },
    { id: 'shortcuts', label: '快捷键', icon: Keyboard },
  ] },
  { title: '模型', items: [
    { id: 'agent', label: '模型与供应商', icon: Bot },
    { id: 'personagent', label: 'Bot 团队', icon: Bot },
  ] },
  { title: '集成', items: [
    { id: 'runtime', label: '浏览器运行时', icon: Gauge },
    { id: 'skills', label: 'Skill 库', icon: Blocks },
    { id: 'memory', label: '认知记忆', icon: Brain },
    { id: 'feishu', label: '飞书同步', icon: Table2 },
  ] },
  { title: '高级', items: [
    { id: 'storage', label: '存储与清理', icon: HardDrive },
    { id: 'logs', label: '日志与诊断', icon: ScrollText },
    { id: 'about', label: '关于与数据', icon: Info },
  ] },
];

const SETTINGS_SECTION_SET = new Set<SettingsSection>(SETTINGS_GROUPS.flatMap((group) => group.items.map((item) => item.id)));

function sectionFromLocation(): SettingsSection | null {
  if (typeof window === 'undefined') return null;
  const parts = window.location.pathname.replace(/\/+$/, '').split('/');
  const index = parts.lastIndexOf('settings');
  const candidate = index >= 0 ? parts[index + 1] : '';
  // 兼容旧入口：proxies 没有独立面板，落到运行时；skills 直接进 Skill 库。
  if (candidate === 'proxies') return 'runtime';
  return SETTINGS_SECTION_SET.has(candidate as SettingsSection) ? (candidate as SettingsSection) : null;
}

function settingsPathFor(section: SettingsSection): string {
  const prefix = typeof window !== 'undefined' && window.location.pathname.startsWith('/shop') ? '/shop' : '/SM';
  return `${prefix}/settings/${section}`;
}

const SECTION_COPY: Record<SettingsSection, { title: string; detail: string }> = {
  account: { title: '账户与身份', detail: '本机身份、登录状态与退出。' },
  appearance: { title: '外观', detail: '浅色或深色；只影响这台机器。' },
  shortcuts: { title: '快捷键', detail: '常用操作的键盘入口。' },
  agent: { title: '模型与供应商', detail: '选择已验证的模型，管理 provider。' },
  personagent: { title: 'Bot 团队', detail: 'Bot 的角色、指令与置顶关系。' },
  runtime: { title: '浏览器运行时', detail: 'EgoLite 运行时、Space 并发与安全边界。' },
  skills: { title: 'Skill 库', detail: '本机 Skill 的审查与启用。' },
  memory: { title: '认知记忆', detail: '本机保存的长期偏好与事实；只在这台机器上生效。' },
  feishu: { title: '飞书同步', detail: '把本地各板块同步到飞书多维表格；本地 SQLite 仍是真源。' },
  storage: { title: '存储与清理', detail: '数据目录各类占用；只清理可安全删除的文件。' },
  logs: { title: '日志与诊断', detail: '脱敏运行事件；不包含 Cookie、凭据与正文。' },
  about: { title: '关于与数据', detail: '版本、数据目录与本地存储边界。' },
};

export function EgoSettingsView({ initialSection = 'account' }: { initialSection?: SettingsSection }) {
  const toast = useUIStore((state) => state.toast);
  const [section, setSection] = useState<SettingsSection>(() => sectionFromLocation() ?? initialSection);
  const [skin, setSkin] = useState<SkinId>(() => getStoredSkin());
  const [syncing, setSyncing] = useState(false);
  const feishuStatus = useSWR(section === 'feishu' ? 'api/feishu/status' : null, api.getFeishuSyncStatus, { refreshInterval: 0, shouldRetryOnError: false });
  const { data: health } = useSWR('healthz', api.getBackendHealth, { revalidateOnFocus: false });
  const { data: doctor, error, mutate } = useSWR(section === 'runtime' ? 'api/ego/doctor' : null, api.getEgoDoctor);
  const { data: spaces = [] } = useSWR(section === 'runtime' ? 'api/ego/spaces' : null, api.getEgoSpaces);
  // 事件列表渲染在「日志与诊断」分节，key 必须跟着 logs 走。
  // 之前绑在 runtime 上，进 logs 时 key 为 null，请求根本不发，
  // 这一组点进去永远是「暂无运行事件」。
  const operationEvents = useSWR(section === 'logs' ? 'api/operations/events' : null, api.getOperationEvents, { revalidateOnFocus: false });
  const [calibrating, setCalibrating] = useState(false);
  const [capacity, setCapacity] = useState<{ measured: number; current: number } | null>(null);

  useEffect(() => {
    const fromURL = sectionFromLocation();
    setSection(fromURL ?? initialSection);
  }, [initialSection]);

  // 浏览器后退要跟着 URL 回到上一个分节，否则地址栏和界面错位。
  useEffect(() => {
    const onPopState = () => {
      setSection(sectionFromLocation() ?? initialSection);
    };
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, [initialSection]);

  const runFeishuSync = async () => {
    setSyncing(true);
    try {
      // 409 表示上一轮还没跑完：不是失败，直接进入轮询看它的结果。
      let alreadyRunning = false;
      try {
        await api.syncFeishu('all');
      } catch (reason) {
        const message = reason instanceof Error ? reason.message : '';
        if (!message.includes('FEISHU_SYNC_RUNNING')) throw reason;
        alreadyRunning = true;
      }
      let finished = false;
      for (let attempt = 0; attempt < 90; attempt += 1) {
        await new Promise((resolve) => window.setTimeout(resolve, 2000));
        const next = await api.getFeishuSyncStatus();
        if (next.syncing) continue;
        const failed = (next.last_results || []).filter((item) => item.error);
        const created = (next.last_results || []).reduce((sum, item) => sum + item.created, 0);
        const updated = (next.last_results || []).reduce((sum, item) => sum + (item.updated || 0), 0);
        const counts = `新增 ${created} 条${updated > 0 ? `，更新 ${updated} 条` : ''}`;
        toast.show(
          failed.length > 0
            ? `同步完成：${counts}，${failed.length} 个模块失败`
            : `同步完成：${counts}`,
          failed.length > 0 ? 'info' : 'success',
        );
        finished = true;
        break;
      }
      if (!finished) {
        toast.show(alreadyRunning ? '同步还在进行，稍后在设置里查看结果' : '同步超过 3 分钟仍未结束，稍后查看结果', 'info');
      }
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '同步失败', 'error');
    } finally {
      setSyncing(false);
      void feishuStatus.mutate();
    }
  };

  const changeSection = (next: SettingsSection) => {
    setSection(next);
    // pushState：分节切换可后退；初始 URL 由首次 replaceState 定锚。
    if (typeof window !== 'undefined') window.history.pushState(null, '', settingsPathFor(next));
  };

  async function calibrate() {
    setCalibrating(true);
    try {
      const result = await api.calibrateEgoConcurrency();
      setCapacity({ measured: result.measured_capacity, current: result.default_concurrency });
      toast.show(`校准完成：默认 ${result.default_concurrency} 路，上限 ${result.measured_capacity} 路`, 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '并发校准失败', 'error');
    } finally {
      setCalibrating(false);
    }
  }

  return (
    <section className="mx-auto flex w-full max-w-6xl flex-col gap-6 lg:flex-row" data-testid="ego-settings">
      <aside className="w-full shrink-0 lg:w-56">
        <div className="space-y-5 lg:sticky lg:top-4">
          <div>
            <p className="text-[10px] font-semibold uppercase tracking-[0.2em] text-brand-muted">Settings</p>
            <h3 className="mt-1 text-lg font-bold text-brand-fg">设置</h3>
          </div>
          <nav aria-label="设置分类" className="space-y-4">
            {SETTINGS_GROUPS.map((group) => (
              <div key={group.title}>
                <p className="px-2 pb-1 text-[10px] font-semibold uppercase tracking-wide text-brand-muted">{group.title}</p>
                <div className="space-y-0.5">
                  {group.items.map((item) => {
                    const Icon = item.icon;
                    const active = section === item.id;
                    return (
                      <button
                        key={item.id}
                        type="button"
                        data-testid={`settings-section-${item.id}`}
                        aria-current={active ? 'page' : undefined}
                        onClick={() => changeSection(item.id)}
                        className={`flex h-9 w-full items-center gap-2.5 rounded-lg px-2.5 text-left text-[12px] transition-colors ${active ? 'bg-brand-fg/[0.07] font-semibold text-brand-fg' : 'text-brand-muted hover:bg-brand-fg/[0.04] hover:text-brand-fg'}`}
                      >
                        <Icon className="h-3.5 w-3.5 shrink-0" />
                        <span className="min-w-0 truncate">{item.label}</span>
                      </button>
                    );
                  })}
                </div>
              </div>
            ))}
          </nav>
        </div>
      </aside>

      <div className="min-w-0 flex-1 space-y-5">
        <header className="border-b border-brand-border pb-4">
          <h4 className="text-base font-bold text-brand-fg">{SECTION_COPY[section].title}</h4>
          <p className="mt-1 text-xs leading-5 text-brand-muted">{SECTION_COPY[section].detail}</p>
        </header>

        {section === 'account' ? (
          <div className="ui-card space-y-4 p-5" data-testid="settings-account">
            <div>
              <h5 className="text-sm font-bold text-brand-fg">本机应用</h5>
              <p className="mt-1 text-xs leading-5 text-brand-muted">默认只在这台机器上使用；没有云端账户也能完整工作。数据保存在本地 SQLite 与私有目录。</p>
            </div>
            <div className="rounded-xl border border-brand-border bg-brand-surface-raised px-3 py-2.5 text-xs text-brand-muted">
              登录状态：{health?.ready ? '本机服务正常' : '本机服务未就绪'}
            </div>
            <button
              type="button"
              onClick={() => {
                setToken(null);
                window.location.reload();
              }}
              className="inline-flex h-8 items-center gap-1.5 rounded-md border border-brand-border px-3 text-xs font-semibold text-brand-fg transition hover:bg-brand-fg/[0.05]"
            >
              <LogOut className="h-3.5 w-3.5" />
              清除本机登录状态
            </button>
          </div>
        ) : null}

        {section === 'appearance' ? (
          <div className="ui-card space-y-4 p-5" data-testid="settings-appearance">
            <p className="text-xs leading-5 text-brand-muted">选择后立即生效，并写入本机偏好；不会同步到其他设备。</p>
            <div className="flex gap-2">
              {([
                { id: 'claude' as SkinId, label: '浅色' },
                { id: 'hermes' as SkinId, label: '深色' },
              ]).map((option) => (
                <button
                  key={option.id}
                  type="button"
                  onClick={() => {
                    applySkin(option.id);
                    setSkin(option.id);
                    window.dispatchEvent(new CustomEvent<SkinId>('alpha-nexus-egolite:skin-changed', { detail: option.id }));
                  }}
                  aria-pressed={skin === option.id}
                  className={`h-9 rounded-lg border px-4 text-xs font-semibold transition ${skin === option.id ? 'border-brand-accent bg-brand-accent text-brand-on-accent' : 'border-brand-border text-brand-muted hover:text-brand-fg'}`}
                >
                  {option.label}
                </button>
              ))}
            </div>
          </div>
        ) : null}

        {section === 'shortcuts' ? (
          <div className="ui-card divide-y divide-brand-border" data-testid="settings-shortcuts">
            {[
              ['⌘ / Ctrl + K', '把焦点放回对话输入框'],
              ['⌘ / Ctrl + Shift + A', '打开或回到 Agent'],
              ['Enter', '发送消息（Shift + Enter 换行）'],
              ['Esc', '关闭模型菜单或浮层'],
              ['↑ / ↓', '在 Bot 列表与模型菜单中移动'],
            ].map(([keys, action]) => (
              <div key={keys} className="flex items-center justify-between gap-4 px-5 py-3">
                <span className="font-mono text-[11px] text-brand-fg">{keys}</span>
                <span className="text-right text-[11px] text-brand-muted">{action}</span>
              </div>
            ))}
          </div>
        ) : null}

              {section === 'runtime' ? <div className="space-y-5">
        <div>
          <h4 className="text-sm font-bold text-gray-900">EgoLite 浏览器运行时</h4>
          <p className="mt-1 text-xs leading-5 text-gray-500">只使用 ego-lite，不提供 Chrome、Chromium、BrowserOS、CDP 或 profile 选择。</p>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
        <article className="ui-card p-5">
          <div className="flex items-start justify-between gap-3">
            <div className="flex items-center gap-3">
              <span className="alpha-glass-tile h-11 w-11" aria-hidden="true">α</span>
              <div><h4 className="text-sm font-bold text-gray-900">ego-browser CLI</h4><p className="text-[10px] text-gray-500">标准 stdin 程序 · 结构化输出</p></div>
            </div>
            {doctor?.available ? <CheckCircle2 className="h-5 w-5 text-emerald-500" /> : <XCircle className="h-5 w-5 text-rose-500" />}
          </div>
          <div className={`mt-4 rounded-xl border px-3 py-2.5 text-xs ${doctor?.available ? 'border-brand-border bg-brand-surface-raised text-brand-success' : 'border-brand-border bg-brand-surface-raised text-brand-danger'}`}>
            {doctor?.available ? '所需 helper 已通过能力握手' : error ? 'ego-browser 不可用或契约不兼容' : '正在检查安装状态'}
          </div>
          <button type="button" className="ui-btn mt-3 w-full justify-center" onClick={() => void mutate()}><RefreshCw className="h-4 w-4" />重新诊断</button>
        </article>

        <article className="ui-card p-5">
          <div className="flex items-center gap-3"><Gauge className="h-5 w-5 text-brand-muted" /><div><h4 className="text-sm font-bold text-gray-900">Space 并发</h4><p className="text-[10px] text-gray-500">不同 Space 并行，同一 Space 永远互斥</p></div></div>
          <div className="mt-4 grid grid-cols-3 gap-2 text-center">
            <div className="rounded-xl bg-black/[0.03] p-3"><strong className="block text-lg">{spaces.length}</strong><span className="text-[9px] text-gray-500">已绑定</span></div>
            <div className="rounded-xl bg-black/[0.03] p-3"><strong className="block text-lg">{capacity?.current ?? 1}</strong><span className="text-[9px] text-gray-500">默认并发</span></div>
            <div className="rounded-xl bg-black/[0.03] p-3"><strong className="block text-lg">{capacity?.measured ?? '—'}</strong><span className="text-[9px] text-gray-500">实测上限</span></div>
          </div>
          <button type="button" disabled={calibrating || !doctor?.available} className="ui-btn ui-btn-primary mt-3 w-full justify-center disabled:opacity-50" onClick={() => void calibrate()}><Activity className={`h-4 w-4 ${calibrating ? 'animate-pulse' : ''}`} />{calibrating ? '校准中…' : '运行本机校准'}</button>
        </article>
        </div>

        <article className="ui-card p-5">
        <div className="flex items-center gap-3"><ShieldCheck className="h-5 w-5 text-emerald-500" /><h4 className="text-sm font-bold text-gray-900">固定安全边界</h4></div>
        <div className="mt-4 grid gap-3 text-xs text-gray-600 sm:grid-cols-2">
          <p className="rounded-xl border border-brand-border p-3">验证码、扫码和 2FA 只在交给用户的 Space 内完成。</p>
          <p className="rounded-xl border border-brand-border p-3">应用不读取、导出或记录平台登录存储与密码。</p>
          <p className="rounded-xl border border-brand-border p-3">最终提交只接受冻结批次的一次性明确授权。</p>
          <p className="rounded-xl border border-brand-border p-3">结果证据不足时记录 result_unknown，禁止自动重试。</p>
        </div>
        </article>


        <div className="flex items-center gap-2 text-[10px] text-gray-500"><TerminalSquare className="h-3.5 w-3.5" />CLI 诊断：alpha-nexus-egolite ego doctor</div>
      </div> : null}



        {section === 'feishu' ? (
          <div className="space-y-4" data-testid="settings-feishu">
            <div className="ui-card space-y-3 p-5">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <div>
                  <h5 className="text-sm font-bold text-brand-fg">Azimuth GTM Harness</h5>
                  <p className="mt-1 text-[10px] leading-4 text-brand-muted">
                    {feishuStatus.data?.configured ? '已连接飞书 Base；本地写入后按模块同步。' : '还没有配置飞书 Base。'}
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  {feishuStatus.data?.url ? (
                    <a
                      href={feishuStatus.data.url}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex h-8 items-center rounded-md border border-brand-border px-3 text-xs font-semibold text-brand-fg transition hover:bg-brand-fg/[0.05]"
                    >
                      打开表
                    </a>
                  ) : null}
                  <button
                    type="button"
                    data-testid="settings-feishu-sync"
                    disabled={syncing || !feishuStatus.data?.configured}
                    onClick={() => void runFeishuSync()}
                    className="inline-flex h-8 items-center rounded-md bg-brand-accent px-3 text-xs font-semibold text-brand-on-accent transition hover:brightness-110 disabled:opacity-50"
                  >
                    {syncing ? '同步中…' : '立即同步'}
                  </button>
                </div>
              </div>
              {feishuStatus.data?.error ? (
                <p className="rounded-lg bg-brand-surface-raised px-3 py-2 text-[10px] text-brand-warning">{feishuStatus.data.error}</p>
              ) : null}
            </div>

            <div className="ui-card divide-y divide-brand-border" data-testid="settings-feishu-modules">
              {(feishuStatus.data?.modules || []).map((module) => {
                // 只报「已同步 N 条」会让用户以为云端就是本地全量。本地总量
                // 与已同步数不一致时，必须把差额和截断上限一起说出来。
                const total = module.local_total ?? module.synced;
                const pending = Math.max(0, total - module.synced);
                return (
                  <div key={module.id} className="flex items-center justify-between gap-3 px-5 py-3">
                    <div className="min-w-0">
                      <p className="text-xs font-semibold text-brand-fg">{module.label}</p>
                      <p className="mt-0.5 truncate font-mono text-[9px] text-brand-muted">{module.table_id || '未配置表'}</p>
                      {pending > 0 ? (
                        <p className="mt-0.5 text-[10px] text-brand-warning">
                          还有 {pending} 条未同步
                          {module.caps_at ? `（单轮最多 ${module.caps_at} 条，会自动向后补齐）` : ''}
                        </p>
                      ) : null}
                    </div>
                    <span className="shrink-0 text-[11px] tabular-nums text-brand-muted">
                      {module.synced} / {total} 条
                    </span>
                  </div>
                );
              })}
              {feishuStatus.data && feishuStatus.data.modules.length === 0 ? (
                <p className="px-5 py-6 text-center text-xs text-brand-muted">还没有可同步的模块。</p>
              ) : null}
            </div>
            <p className="text-[10px] leading-4 text-brand-muted">
              同步是单向的：本地 SQLite → 飞书 Base；飞书里的改动不会覆盖本地。凭据与 token 由 lark-cli 保管，不进入前端或日志。
            </p>
          </div>
        ) : null}

        {section === 'agent' ? <div><AlphaAgentSettingsSection /></div> : null}
        {section === 'personagent' ? <div><BotSettingsPanel /></div> : null}
        {section === 'skills' ? <div><SkillLibraryView embedded /></div> : null}

        {section === 'memory' ? <CognitiveMemoryView /> : null}

        {section === 'storage' ? <StorageSettingsSection /> : null}

        {section === 'logs' ? (
          <div className="space-y-4">
                    <article className="ui-card overflow-hidden" data-testid="settings-operation-log">
          <div className="flex items-start justify-between gap-3 border-b border-brand-border px-5 py-4">
            <div><h4 className="text-sm font-bold text-gray-900">运行日志</h4><p className="mt-1 text-[10px] text-gray-500">最近 30 条脱敏语义事件；日志不包含 Cookie、凭据、正文或浏览器画像。</p></div>
            <button type="button" className="ui-btn" onClick={() => void operationEvents.mutate()}><RefreshCw className="h-3.5 w-3.5" />刷新</button>
          </div>
          <div className="max-h-80 divide-y divide-brand-border overflow-y-auto font-mono">
            {operationEvents.data?.length ? operationEvents.data.slice(0, 30).map((event) => {
              const failed = event.status === 'failed' || ['error', 'ERROR'].includes(event.level);
              const running = event.status === 'running';
              const tone = failed ? 'text-rose-700' : running ? 'text-brand-muted' : 'text-emerald-700';
              return <div key={event.id} className="grid grid-cols-[6rem_minmax(0,1fr)_auto] items-start gap-3 px-5 py-2.5 text-[10px]">
                <time className="text-gray-400">{new Date(event.created_at).toLocaleTimeString('zh-CN', { hour12: false })}</time>
                <div className="min-w-0"><p className="truncate font-semibold text-gray-800">{event.step || event.event_type || event.component}</p><p className="mt-0.5 truncate text-gray-400">{event.component}{event.error_code ? ` · ${event.error_code}` : ''}</p></div>
                <span className={`font-sans font-semibold ${tone}`}>{event.status || event.level}</span>
              </div>;
            }) : <div className="flex items-center justify-center gap-2 px-5 py-10 font-sans text-xs text-gray-500"><Clock3 className="h-4 w-4" />暂无运行事件</div>}
          </div>
        </article>

            <p className="text-[10px] leading-4 text-brand-muted">日志目录：~/.alpha-nexus-egolite/logs · 固定大小轮转，权限 0600。</p>
          </div>
        ) : null}

        {section === 'about' ? (
          <div className="space-y-4" data-testid="settings-about">
            <div className="ui-card space-y-3 p-5">
              <div className="flex items-center justify-between gap-3">
                <span className="text-xs font-semibold text-brand-fg">版本</span>
                <span className="font-mono text-[11px] text-brand-muted">{health?.version || '读取中…'}</span>
              </div>
              <div className="flex items-center justify-between gap-3">
                <span className="text-xs font-semibold text-brand-fg">数据目录</span>
                <span className="font-mono text-[11px] text-brand-muted">~/.alpha-nexus-egolite</span>
              </div>
              <div className="flex items-center justify-between gap-3">
                <span className="text-xs font-semibold text-brand-fg">存储</span>
                <span className="text-[11px] text-brand-muted">SQLite + 本地文件</span>
              </div>
            </div>
            <p className="text-[10px] leading-4 text-brand-muted">本应用不读取、导出或记录平台 Cookie、密码与浏览器画像；最终发布提交只接受一次性明确授权。</p>
          </div>
        ) : null}
      </div>
    </section>
  );
}
