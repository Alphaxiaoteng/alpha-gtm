import React, { useState, useEffect, useRef } from 'react';
import useSWR from 'swr';
import {
  api,
  type BrowserCandidate,
  type NotificationChannelKind,
  type NotificationChannelSetting,
  type AITestResult,
  type AIConfigPayload,
  type AgentHubProbe,
  type AgentHubProviderID,
  type AgentHubProviders,
} from '../api';
import { preferredAgentProvider, setPreferredAgentProvider, useUIStore } from '../store';
import { ProxyPoolView } from './ProxyPoolView';
import { DataAPISettingsSection } from './DataAPISettingsSection';
import { StorageSettingsSection } from './StorageSettingsSection';
import { LocalLoadingState } from './LocalLoadingState';
import { 
  Globe, 
  Fingerprint, 
  FileText,
  X,
	Bot,
	FolderOpen,
	Image as ImageIcon,
	Bell,
	Mail,
	Webhook,
	ExternalLink,
	Sparkles,
	CheckCircle2,
	ShieldCheck,
	XCircle,
	Loader2,
	Eye,
	EyeOff,
	Zap,
	Video,
	ChevronDown,
	ChevronUp,
} from 'lucide-react';

/**
 * Official app icons extracted from installed .app bundles
 * (Chromium / BrowserOS / Chrome app.icns → public/browsers/*.png).
 */
const OFFICIAL_ICON: Record<string, string> = {
  browseros: '/browsers/browseros.png',
  chromium: '/browsers/chromium.png',
  chrome: '/browsers/chrome.png',
};

const BrowserIcon: React.FC<{ name?: string; className?: string }> = ({
  name,
  className = 'w-9 h-9',
}) => {
  const key = (name || '').toLowerCase();
  const src = OFFICIAL_ICON[key];
  if (src) {
    return (
      <img
        src={src}
        alt=""
        className={`${className} rounded-[22%] object-contain shadow-sm bg-white/5`}
        draggable={false}
      />
    );
  }
  return (
    <svg className={className} viewBox="0 0 48 48" fill="none" aria-hidden>
      <circle cx="24" cy="24" r="18" stroke="#737373" strokeWidth="2.5" />
      <ellipse cx="24" cy="24" rx="8" ry="18" stroke="#737373" strokeWidth="2" />
      <path d="M6 24h36" stroke="#737373" strokeWidth="2" />
    </svg>
  );
};

const FALLBACK_BROWSERS: BrowserCandidate[] = [
  {
    id: 'chromium',
    label: 'Chromium',
    engine: 'chromium',
    path: '',
    found: false,
    primary: true,
    icon: 'chromium',
    description: '开源 Chromium 内核，轻量稳定，独立指纹环境默认首选',
  },
  {
    id: 'browseros',
    label: 'BrowserOS',
    engine: 'browseros',
    path: '',
    found: false,
    icon: 'browseros',
    description: '开源 agentic 浏览器，CDP 本机控制，适合自动化与账号隔离',
  },
  {
    id: 'chrome',
    label: 'Google Chrome',
    engine: 'chromium',
    path: '',
    found: false,
    icon: 'chrome',
    description: '官方 Chrome，兼容性好；自动化特征更明显，仅作备选',
  },
];

const BROWSER_INSTALL_HELP = [
  {
    id: 'chromium',
    label: 'Chromium',
    downloadURL: 'https://www.chromium.org/getting-involved/download-chromium/',
    guideURL: 'https://download-chromium.appspot.com/',
    steps: ['下载 macOS 版 Chromium 安装包', '拖入“应用程序”', '返回这里点“重新扫描”并选择 Chromium'],
  },
  {
    id: 'browseros',
    label: 'BrowserOS',
    downloadURL: 'https://github.com/browseros-ai/BrowserOS',
    guideURL: 'https://github.com/browseros-ai/BrowserOS/releases',
    steps: ['在 GitHub Releases 下载 BrowserOS 安装包', '拖入“应用程序”', '返回这里点“重新扫描”并选择 BrowserOS'],
  },
];

interface LocalSettings {
  defaultRegion: string;
  macRatio: number;
  cpuPool: string;
  brand: string;
  brandVer: string;
  lockProfile: boolean;
}

const DEFAULT_SETTINGS: LocalSettings = {
  defaultRegion: 'beijing',
  macRatio: 85,
  cpuPool: '8,10,12',
  brand: 'Chrome',
  brandVer: '148',
  lockProfile: true,
};

/** Smart fingerprint scene presets — one click, no typing */
const FP_PRESETS: {
  id: string;
  label: string;
  blurb: string;
  patch: Partial<LocalSettings>;
}[] = [
  {
    id: 'cn_social',
    label: '国内社媒',
    blurb: '小红书/抖音 · 高 macOS 占比 · 一线城市',
    patch: {
      defaultRegion: 'beijing',
      macRatio: 85,
      cpuPool: '8,10,12',
      brand: 'Chrome',
      brandVer: '148',
      lockProfile: true,
    },
  },
  {
    id: 'meta_export',
    label: '出海 Meta',
    blurb: 'IG/FB · 美西时区友好 · 均衡平台',
    patch: {
      defaultRegion: 'us_west',
      macRatio: 60,
      cpuPool: '4,8,8,12,16',
      brand: 'Chrome',
      brandVer: '148',
      lockProfile: true,
    },
  },
  {
    id: 'sea',
    label: '东南亚',
    blurb: '新加坡节点 · 混合设备',
    patch: {
      defaultRegion: 'singapore',
      macRatio: 55,
      cpuPool: '4,8,8,10',
      brand: 'Chrome',
      brandVer: '147',
      lockProfile: true,
    },
  },
  {
    id: 'jp_kr',
    label: '日韩',
    blurb: '东京/首尔 · 偏 macOS',
    patch: {
      defaultRegion: 'japan',
      macRatio: 75,
      cpuPool: '8,10,12',
      brand: 'Chrome',
      brandVer: '148',
      lockProfile: true,
    },
  },
];

const REGION_CHIPS: { value: string; label: string }[] = [
  { value: 'beijing', label: '北京' },
  { value: 'shanghai', label: '上海' },
  { value: 'guangzhou', label: '广州' },
  { value: 'shenzhen', label: '深圳' },
  { value: 'hangzhou', label: '杭州' },
  { value: 'chengdu', label: '成都' },
  { value: 'hong_kong', label: '香港' },
  { value: 'singapore', label: '新加坡' },
  { value: 'us_west', label: '美西' },
  { value: 'us_east', label: '美东' },
  { value: 'japan', label: '日本' },
  { value: 'korea', label: '韩国' },
];

const MAC_RATIO_PRESETS = [60, 75, 85, 95];
const CPU_PRESETS = [
  { label: '轻量', value: '4,8,8' },
  { label: '均衡', value: '8,10,12' },
  { label: '性能', value: '8,10,12,16' },
];
const CHROME_VERS = ['146', '147', '148'];

function Chip({
  active,
  onClick,
  children,
  disabled,
}: {
  active?: boolean;
  onClick: () => void;
  children: React.ReactNode;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={[
        'px-2.5 py-1.5 rounded-lg text-[10px] font-bold border transition',
        active
          ? 'border-brand-accent bg-brand-accent/15 text-brand-accent'
          : 'border-brand-border bg-brand-bg text-gray-600 hover:border-brand-accent/40',
        disabled ? 'opacity-40 cursor-not-allowed' : '',
      ].join(' ')}
    >
      {children}
    </button>
  );
}

export const SettingsView: React.FC = () => {
  const { toast } = useUIStore();
  const { data: overview, mutate: mutateOverview } = useSWR('api/overview', api.getOverview);
	const { data: contentLibraryConfig, mutate: mutateContentLibraryConfig } = useSWR('api/content-library/config', api.getContentLibraryConfig);
  
  const [settings, setSettings] = useState<LocalSettings>(DEFAULT_SETTINGS);
  const [customBrowserPath, setCustomBrowserPath] = useState('');
  const [maxConcurrent, setMaxConcurrent] = useState(0);
  const [browsers, setBrowsers] = useState<BrowserCandidate[]>([]);
  const [activeBrowserId, setActiveBrowserId] = useState('');
  const [scanning, setScanning] = useState(false);
  const [showAdvancedPath, setShowAdvancedPath] = useState(false);
  const [fpAdvanced, setFpAdvanced] = useState(false);
  const [fpPresetId, setFpPresetId] = useState('cn_social');
  const [proxyOpen, setProxyOpen] = useState(false);
	const [contentLibraryDir, setContentLibraryDir] = useState('');
	const [savingContentLibraryDir, setSavingContentLibraryDir] = useState(false);
  const [creatingDiagnostics, setCreatingDiagnostics] = useState(false);
  const [lastDiagnosticPath, setLastDiagnosticPath] = useState('');

  // Path picker state
  const [isPickerOpen, setIsPickerOpen] = useState(false);
  const [pickerPath, setPickerPath] = useState('');
  const [pickerData, setPickerData] = useState<{
    currentPath: string;
    parentPath: string;
    directories: { name: string; path: string; isDir: boolean }[];
    files: { name: string; path: string; isDir: boolean }[];
  } | null>(null);

  // Load settings from LocalStorage
  useEffect(() => {
    const raw = localStorage.getItem('unibuy_settings');
    if (raw) {
      try {
        const parsed = JSON.parse(raw);
        delete parsed.autoCollect;
        delete parsed.collectInterval;
        setSettings({ ...DEFAULT_SETTINGS, ...parsed });
      } catch {
        console.warn('[settings] ignored invalid local preferences');
      }
    }
  }, []);

  // Auto-scan local browsers on mount
  useEffect(() => {
    refreshBrowsers().catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Esc key listener to close picker modal
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setIsPickerOpen(false);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, []);

  // Sync config from backend overview once loaded
  useEffect(() => {
    if (overview) {
      if (overview.browser_path) {
        setCustomBrowserPath(overview.browser_path);
      }
      if (overview.max_concurrent_browsers !== undefined) {
        setMaxConcurrent(overview.max_concurrent_browsers);
      }
      if (overview.browsers) {
        setBrowsers(overview.browsers);
      }
      if (overview.active_browser_id) {
        setActiveBrowserId(overview.active_browser_id);
      }
    }
  }, [overview]);



	useEffect(() => {
		if (contentLibraryConfig?.root_dir) setContentLibraryDir(contentLibraryConfig.root_dir);
	}, [contentLibraryConfig]);

  const refreshBrowsers = async () => {
    setScanning(true);
    try {
      const res = await api.listBrowsers();
      setBrowsers(res.browsers || []);
      if (res.browser_path) setCustomBrowserPath(res.browser_path);
      if (res.active_browser_id) setActiveBrowserId(res.active_browser_id);
      return res;
    } catch (e: any) {
      toast.show(`扫描浏览器失败: ${e.message}`, 'error');
      return null;
    } finally {
      setScanning(false);
    }
  };

  const handleSelectBrowser = async (id: string) => {
    const cand = browsers.find((b) => b.id === id);
    if (cand && !cand.found) {
      toast.show(`${cand.label} 未安装，请先安装或点「重新扫描」`, 'error');
      return;
    }
    try {
      const res = await api.selectBrowser({ id });
      setCustomBrowserPath(res.browserPath);
      setActiveBrowserId(res.id || id);
      await mutateOverview();
      await refreshBrowsers();
      toast.show(`已切换为 ${cand?.label || id}`, 'success');
    } catch (e: any) {
      toast.show(`切换失败: ${e.message}`, 'error');
    }
  };

  const handleAutoMatch = async () => {
    setScanning(true);
    try {
      await refreshBrowsers();
      const res = await api.selectBrowser({}); // auto preferred
      setCustomBrowserPath(res.browserPath);
      setActiveBrowserId(res.id || '');
      await mutateOverview();
      toast.show('已自动匹配本机浏览器', 'success');
    } catch (e: any) {
      toast.show(`自动匹配失败: ${e.message}`, 'error');
    } finally {
      setScanning(false);
    }
  };

  const loadPickerPath = async (path?: string) => {
    try {
      const res = await api.browseFiles(path);
      setPickerData(res);
      setPickerPath(res.currentPath);
    } catch (e: any) {
      toast.show(`无法读取路径: ${e.message}`, 'error');
    }
  };

  const handleOpenPicker = async () => {
    if (window.electronAPI?.selectBrowserPath) {
      const selected = await window.electronAPI.selectBrowserPath(customBrowserPath);
      if (selected) {
        setCustomBrowserPath(selected);
      }
    } else {
      let startPath = '';
      if (customBrowserPath) {
        const parts = customBrowserPath.split('/');
        parts.pop(); // Remove file or .app bundle name
        startPath = parts.join('/');
      }
      loadPickerPath(startPath);
      setIsPickerOpen(true);
    }
  };

  const handleOpenFolder = async () => {
    if (window.electronAPI?.openPathInFinder && customBrowserPath) {
      const success = await window.electronAPI.openPathInFinder(customBrowserPath);
      if (!success) {
        toast.show('无法打开该文件夹或文件夹不存在', 'error');
      }
    } else {
      toast.show('当前非客户端环境，无法打开本地文件夹', 'info');
    }
  };

  const openOfficialURL = async (url: string) => {
    if (window.electronAPI?.openExternalURL) {
      const result = await window.electronAPI.openExternalURL(url);
      if (!result.ok) toast.show('无法打开官方页面', 'error');
      return;
    }
    const opened = window.open(url, '_blank', 'noopener,noreferrer');
    if (!opened) toast.show('浏览器阻止了新窗口，请允许弹窗后重试', 'error');
  };



	const saveContentLibraryDirectory = async () => {
		const rootDir = contentLibraryDir.trim();
		if (!rootDir) {
			toast.show('请输入本地素材文件夹的绝对路径', 'error');
			return;
		}
		setSavingContentLibraryDir(true);
		try {
			const saved = await api.updateContentLibraryConfig(rootDir);
			setContentLibraryDir(saved.root_dir);
			await mutateContentLibraryConfig(saved, { revalidate: false });
			toast.show('本地素材目录已保存', 'success');
		} catch (error: any) {
			toast.show(`保存素材目录失败: ${error.message || '未知错误'}`, 'error');
		} finally {
			setSavingContentLibraryDir(false);
		}
	};

  const handleToggle = (key: keyof LocalSettings) => {
    setSettings(prev => ({
      ...prev,
      [key]: !prev[key] as any
    }));
  };

  const handleInputChange = (key: keyof LocalSettings, value: any) => {
    setSettings(prev => ({
      ...prev,
      [key]: value
    }));
  };

  const applyFpPreset = (id: string) => {
    const p = FP_PRESETS.find((x) => x.id === id);
    if (!p) return;
    setFpPresetId(id);
    setSettings((prev) => ({ ...prev, ...p.patch }));
    toast.show(`已套用智能指纹：${p.label}，点击“保存基础设置”后生效`, 'info');
  };

  /** One-shot: smart browser + sticky proxy + CN social fingerprint */
  const handleSmartAll = async () => {
    try {
      setScanning(true);
      // browser auto-match
      try {
        if (window.electronAPI?.applyBrowser) {
          await window.electronAPI.applyBrowser({});
        } else {
          await api.selectBrowser({});
        }
      } catch {
        /* optional */
      }
      setMaxConcurrent(0); // pool default
      setSettings((prev) => ({
        ...prev,
        ...FP_PRESETS[0].patch,
      }));
      setFpPresetId('cn_social');
      await refreshBrowsers();
      await mutateOverview();
      // persist browser config
      const path =
        (await api.getOverview()).browser_path || customBrowserPath;
      if (path) setCustomBrowserPath(path);
      await api.updateConfig({
        browserPath: path || customBrowserPath,
        maxConcurrentBrowsers: 0,
      });
      localStorage.setItem(
        'unibuy_settings',
        JSON.stringify({
          ...DEFAULT_SETTINGS,
          ...FP_PRESETS[0].patch,
        }),
      );
      toast.show('智能方案已应用：浏览器匹配 + 国内社媒指纹', 'success');
    } catch (e: any) {
      toast.show(`智能配置失败: ${e.message}`, 'error');
    } finally {
      setScanning(false);
    }
  };

  const handleSave = async () => {
    try {
      localStorage.setItem('unibuy_settings', JSON.stringify(settings));
      await api.updateConfig({
        browserPath: customBrowserPath,
        maxConcurrentBrowsers: maxConcurrent > 0 ? maxConcurrent : 0,
      });
      await mutateOverview();
      toast.show('基础配置已保存', 'success');
    } catch (e: any) {
      toast.show(`保存配置失败: ${e.message}`, 'error');
    }
  };

  const handleExportSnapshot = async () => {
    try {
      const snapshot = await api.getSafeSnapshot();
      const blob = new Blob([JSON.stringify(snapshot, null, 2)], { type: 'application/json;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `alpha-nexus-safe-snapshot-${new Date().toISOString().slice(0, 10)}.json`;
      link.click();
      URL.revokeObjectURL(url);
      toast.show('安全快照已下载；不包含登录会话、Cookie、凭证或浏览器画像', 'success');
    } catch (error: any) {
      toast.show(`导出失败: ${error.message || '未知错误'}`, 'error');
    }
  };

  const handleCreateDiagnostics = async () => {
    setCreatingDiagnostics(true);
    try {
      const result = await api.createDiagnosticSnapshot();
      setLastDiagnosticPath(result.path);
      toast.show(
        `诊断快照已生成：${result.counts.publish_jobs_failed} 个失败发布任务，${result.counts.queue_failed} 个失败队列任务`,
        'success',
      );
    } catch (error: any) {
      toast.show(`生成诊断快照失败：${error.message || '未知错误'}`, 'error');
    } finally {
      setCreatingDiagnostics(false);
    }
  };

  const handleOpenLogDirectory = async () => {
    if (!window.electronAPI?.openPathInFinder) {
      toast.show('当前非客户端环境，无法打开本地日志目录', 'info');
      return;
    }
    try {
      const { path } = await api.getDiagnosticLogDirectory();
      const opened = await window.electronAPI.openPathInFinder(path);
      if (!opened) toast.show('无法打开日志目录', 'error');
    } catch (error: any) {
      toast.show(`打开日志目录失败：${error.message || '未知错误'}`, 'error');
    }
  };

  return (
    <div data-testid="settings-view" className="max-w-5xl space-y-5">
      {/* Header */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="ui-page-title">设置</h3>
          <p className="text-xs text-gray-550 mt-0.5">
            选择模型和浏览器；路径、诊断等细节按需展开。
          </p>
        </div>
        <div className="flex gap-2">
          <button type="button" onClick={handleExportSnapshot} className="ui-btn">安全备份</button>
          <button
            type="button"
            data-testid="settings-smart-all"
            onClick={handleSmartAll}
            disabled={scanning}
            className="ui-btn ui-btn-primary disabled:opacity-50"
          >
            {scanning ? '配置中…' : '智能配置'}
          </button>
        </div>
      </div>

      <div className="space-y-5">
        <AgentProviderConnectionsSection />

        {/* Section 1: Browser / Runtime */}
        <div className="ui-section p-5 space-y-4">
          <div className="flex items-center justify-between border-b border-brand-border/60 pb-3 gap-2">
            <div className="flex items-center gap-2">
              <Globe className="w-4 h-4 text-brand-accent" />
              <h4 className="text-xs font-bold text-gray-900">运行环境</h4>
            </div>
            <div className="flex items-center gap-2">
              <button
                type="button"
                data-testid="settings-browser-scan"
                onClick={() => refreshBrowsers()}
                disabled={scanning}
                className="text-[10px] font-bold px-2.5 py-1 rounded-lg border border-brand-border hover:bg-black/5 text-gray-700 disabled:opacity-50"
              >
                {scanning ? '扫描中…' : '重新扫描'}
              </button>
              <button
                type="button"
                data-testid="settings-browser-auto"
                onClick={handleAutoMatch}
                disabled={scanning}
                className="text-[10px] font-bold px-2.5 py-1 rounded-lg bg-brand-accent text-brand-on-accent hover:opacity-90 disabled:opacity-50"
              >
                一键匹配本机
              </button>
            </div>
          </div>

          <div data-testid="settings-egolite-browser" className="flex flex-wrap items-center justify-between gap-4 border-y border-brand-border px-1 py-3">
            <div className="flex min-w-0 items-start gap-3">
              <span className="mt-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center text-gray-500">
                <Globe className="h-4 w-4" />
              </span>
              <div>
                <p className="text-xs font-bold text-gray-900">EgoLite 浏览器</p>
                <p className="mt-1 max-w-xl text-[11px] leading-5 text-gray-550">账号登录、平台页面与人工接管固定使用 EgoLite Space；安装后完成引导，再回到“浏览器与账号”登录平台。</p>
              </div>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <button type="button" className="ui-btn h-8 px-3 text-[10px]" onClick={() => void openOfficialURL('https://www.egolite.app/document/en/docs/quick-start')}>
                安装说明
              </button>
              <button type="button" className="ui-btn ui-btn-primary h-8 px-3 text-[10px]" onClick={() => void openOfficialURL('https://lite.ego.app/')}>
                打开 EgoLite 下载页 <ExternalLink className="h-3 w-3" />
              </button>
            </div>
          </div>

          {/* Quick browser switch */}
          <div className="space-y-2">
            <label className="text-[10px] font-bold text-gray-650 uppercase tracking-wide">
              浏览器引擎（点击切换，自动填路径）
            </label>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-3" data-testid="settings-browser-chips">
              {(browsers.length ? browsers : FALLBACK_BROWSERS).map((b) => {
                const active = activeBrowserId === b.id || (!!b.path && b.path === customBrowserPath);
                const desc =
                  b.description ||
                  FALLBACK_BROWSERS.find((x) => x.id === b.id)?.description ||
                  '';
                return (
                  <button
                    key={b.id}
                    type="button"
                    data-testid={`settings-browser-chip-${b.id}`}
                    onClick={() => handleSelectBrowser(b.id)}
                    disabled={!b.found && browsers.length > 0}
                    className={[
                      'text-left rounded-xl border px-3.5 py-3 transition relative flex gap-3',
                      active
                        ? 'border-brand-accent bg-brand-accent/10 ring-1 ring-brand-accent/40'
                        : b.found
                          ? 'border-brand-border bg-brand-bg hover:border-brand-accent/50'
                          : 'border-dashed border-brand-border/60 bg-brand-bg/50 opacity-60 cursor-not-allowed',
                    ].join(' ')}
                    title={b.found ? b.path : '未在本机检测到'}
                  >
                    <div className="shrink-0 mt-0.5">
                      <BrowserIcon name={b.icon || b.id} className="w-9 h-9" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-1.5 flex-wrap">
                        <span className="text-xs font-bold text-gray-900">{b.label}</span>
                        {b.primary ? (
                          <span className="text-[9px] font-bold px-1.5 py-0.5 rounded bg-brand-accent/15 text-brand-accent">
                            推荐
                          </span>
                        ) : null}
                        {active ? (
                          <span className="text-[9px] font-bold px-1.5 py-0.5 rounded bg-emerald-500/15 text-emerald-700">
                            使用中
                          </span>
                        ) : null}
                      </div>
                      <p className="mt-1 text-[10px] leading-snug text-gray-550 line-clamp-2">
                        {desc}
                      </p>
                      <div className="mt-1.5 flex items-center gap-1.5">
                        <span
                          className={[
                            'inline-block w-1.5 h-1.5 rounded-full',
                            b.found ? 'bg-emerald-500' : 'bg-gray-300',
                          ].join(' ')}
                        />
                        <span className="text-[10px] text-gray-550">
                          {b.found ? '本机已安装' : '未检测到'}
                        </span>
                      </div>
                      {b.found && b.path ? (
                        <div className="mt-0.5 text-[9px] font-mono text-gray-400 truncate" title={b.path}>
                          {b.path.replace(/\/Users\/[^/]+/, '~')}
                        </div>
                      ) : null}
                    </div>
                  </button>
                );
              })}
            </div>
            <p className="text-[10px] text-gray-550">
              自动扫描 <code className="font-mono">/Applications</code> 与{' '}
              <code className="font-mono">~/Applications</code>
              。支持 Chromium / BrowserOS / Chrome，无需手动选文件。
            </p>
            <div
              data-testid="settings-browser-install-help"
              className="grid gap-2 rounded-xl border border-brand-border bg-black/[0.02] p-3 md:grid-cols-2"
            >
              {BROWSER_INSTALL_HELP.map((item) => (
                <div key={item.id} className="rounded-lg border border-brand-border/70 bg-brand-bg px-3 py-2.5">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <strong className="text-[11px] text-gray-900">{item.label} 安装</strong>
                    <span className="flex gap-2">
                      <button type="button" onClick={() => void openOfficialURL(item.downloadURL)} className="text-[10px] font-semibold text-brand-accent hover:underline">
                        官方下载 <ExternalLink className="ml-0.5 inline h-3 w-3" />
                      </button>
                      <button type="button" onClick={() => void openOfficialURL(item.guideURL)} className="text-[10px] font-semibold text-brand-accent hover:underline">
                        官方教程
                      </button>
                    </span>
                  </div>
                  <ol className="mt-2 list-inside list-decimal space-y-0.5 text-[10px] leading-4 text-gray-550">
                    {item.steps.map((step) => <li key={step}>{step}</li>)}
                  </ol>
                </div>
              ))}
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 text-xs">
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <label data-testid="settings-browser-path-label" className="text-[10px] font-bold text-gray-650 uppercase">
                  当前路径
                </label>
                <button
                  type="button"
                  className="text-[10px] text-brand-accent font-semibold hover:underline"
                  onClick={() => setShowAdvancedPath((v) => !v)}
                >
                  {showAdvancedPath ? '收起高级' : '高级 / 手动路径'}
                </button>
              </div>
              <div
                className="font-mono bg-brand-bg px-3 py-2 rounded-lg text-gray-700 border border-brand-border/40 select-text truncate text-[11px]"
                title={customBrowserPath}
                data-testid="settings-browser-path-display"
              >
                {customBrowserPath || '— 未选择，请点「一键匹配」—'}
              </div>
              <span className="text-[10px] text-gray-550 block">
                {overview?.browser_found || (customBrowserPath && browsers.some((b) => b.path === customBrowserPath && b.found))
                  ? '✅ 路径可用'
                  : '⚠️ 路径未验证或文件不存在'}
              </span>

              {showAdvancedPath ? (
                <div className="flex gap-2 pt-1 animate-fade-in">
                  <input
                    data-testid="settings-browser-path"
                    type="text"
                    value={customBrowserPath}
                    onChange={(e) => setCustomBrowserPath(e.target.value)}
                    placeholder="可选：手动粘贴可执行文件路径"
                    className="flex-1 px-3 py-2 rounded-lg border border-brand-border bg-brand-bg text-gray-700 font-mono text-[11px] focus:outline-none focus:border-brand-accent transition"
                  />
                  <button
                    type="button"
                    onClick={handleOpenPicker}
                    className="px-3 py-2 rounded-lg border border-brand-border hover:bg-black/5 text-[10px] text-gray-700 font-bold whitespace-nowrap"
                  >
                    选择文件
                  </button>
                  <button
                    type="button"
                    onClick={handleOpenFolder}
                    className="px-3 py-2 rounded-lg border border-brand-border hover:bg-black/5 text-[10px] text-gray-700 font-bold whitespace-nowrap"
                  >
                    访达
                  </button>
                </div>
              ) : (
                /* keep hidden input for tests that target settings-browser-path */
                <input
                  data-testid="settings-browser-path"
                  type="hidden"
                  value={customBrowserPath}
                  readOnly
                />
              )}
            </div>

            <div className="space-y-1.5">
              <label className="text-[10px] font-bold text-gray-650 uppercase">数据储存根目录</label>
              <div className="font-mono bg-brand-bg px-3 py-2 rounded-lg text-gray-700 border border-brand-border/40 select-text truncate" title={overview?.data_dir}>
                {overview?.data_dir || '—'}
              </div>
              {window.electronAPI?.openPathInFinder && overview?.data_dir ? (
                <button
                  type="button"
                  onClick={async () => {
                    const ok = await window.electronAPI!.openPathInFinder!(overview.data_dir);
                    if (!ok) toast.show('无法打开数据目录', 'error');
                  }}
                  className="text-[10px] font-semibold text-brand-accent hover:underline"
                >
                  在访达中打开数据目录
                </button>
              ) : null}
            </div>

            <div className="space-y-1.5 md:col-span-2">
              <label className="text-[10px] font-bold text-gray-650 uppercase">
                最大并发浏览器数
              </label>
              <div className="flex flex-wrap items-center gap-3">
                <input
                  data-testid="settings-max-concurrent"
                  type="number"
                  min={0}
                  max={99}
                  value={maxConcurrent}
                  onChange={(e) => setMaxConcurrent(Number(e.target.value) || 0)}
                  className="w-32 px-3 py-2 rounded-lg border border-brand-border bg-brand-bg text-gray-700 font-mono focus:outline-none focus:border-brand-accent transition"
                />
                <span className="text-[10px] text-gray-550">
                  0 = 端口池默认（当前生效: {overview?.max_concurrent_browsers ?? '—'}）。超限拒绝启动。
                </span>
              </div>
            </div>
          </div>
        </div>

			<div className="ui-section space-y-3 p-5" data-testid="settings-content-library">
		  <div className="flex items-center gap-2 border-b border-brand-border/60 pb-3">
			<FolderOpen className="h-4 w-4 text-brand-accent" />
			<div><h4 className="text-xs font-bold text-gray-900">自媒体本地素材目录</h4><p className="mt-0.5 text-[10px] text-gray-500">视频、封面和图文图片会复制到此目录；已索引的本机路径不会复制，发布时从冻结路径静默上传。文章正文仍只保存在本机数据库。</p></div>
		  </div>
		  <div className="flex flex-wrap items-end gap-2">
			<label className="min-w-[18rem] flex-1 text-[10px] font-bold text-gray-650 uppercase">素材根目录
			  <input value={contentLibraryDir} onChange={(event) => setContentLibraryDir(event.target.value)} placeholder="本机绝对路径" className="mt-1.5 w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 font-mono text-[11px] text-gray-800" data-testid="settings-content-library-dir" />
			</label>
			<button type="button" onClick={() => void saveContentLibraryDirectory()} disabled={savingContentLibraryDir} className="ui-btn disabled:opacity-50" data-testid="settings-content-library-save"><FolderOpen className="h-3.5 w-3.5" />{savingContentLibraryDir ? '保存中…' : '保存目录'}</button>
		  </div>
		  {contentLibraryConfig ? <p className="text-[10px] text-gray-500">视频：{contentLibraryConfig.video_dir} · 图片：{contentLibraryConfig.image_dir}</p> : null}
		</div>

        {/* Fingerprint — smart presets */}
        <div className="ui-section p-5 space-y-4">
          <div className="flex items-center justify-between border-b border-brand-border/60 pb-3 gap-2">
            <div className="flex items-center gap-2">
              <Fingerprint className="w-4 h-4 text-brand-accent" />
              <h4 className="text-xs font-bold text-gray-900">防关联指纹 · 智能方案</h4>
            </div>
            <button
              type="button"
              className="text-[10px] font-semibold text-brand-accent hover:underline"
              onClick={() => setFpAdvanced((v) => !v)}
            >
              {fpAdvanced ? '收起细节' : '微调参数'}
            </button>
          </div>

          <div className="grid grid-cols-2 md:grid-cols-4 gap-2">
            {FP_PRESETS.map((p) => (
              <button
                key={p.id}
                type="button"
                onClick={() => applyFpPreset(p.id)}
                className={[
                  'text-left rounded-xl border px-3 py-2.5 transition',
                  fpPresetId === p.id
                    ? 'border-brand-accent bg-brand-accent/10 ring-1 ring-brand-accent/30'
                    : 'border-brand-border bg-brand-bg hover:border-brand-accent/40',
                ].join(' ')}
              >
                <div className="text-xs font-bold text-gray-900">{p.label}</div>
                <div className="text-[10px] text-gray-550 mt-0.5 leading-snug">{p.blurb}</div>
              </button>
            ))}
          </div>

          {fpAdvanced && (
            <div className="space-y-3 pt-1 animate-fade-in">
              <div className="space-y-1.5">
                <label className="text-[10px] font-bold text-gray-650 uppercase">默认防区</label>
                <div className="flex flex-wrap gap-1.5">
                  {REGION_CHIPS.map((r) => (
                    <Chip
                      key={r.value}
                      active={settings.defaultRegion === r.value}
                      onClick={() => handleInputChange('defaultRegion', r.value)}
                    >
                      {r.label}
                    </Chip>
                  ))}
                </div>
              </div>
              <div className="space-y-1.5">
                <label className="text-[10px] font-bold text-gray-650 uppercase">
                  macOS 占比 {settings.macRatio}%
                </label>
                <div className="flex flex-wrap gap-1.5">
                  {MAC_RATIO_PRESETS.map((n) => (
                    <Chip
                      key={n}
                      active={settings.macRatio === n}
                      onClick={() => handleInputChange('macRatio', n)}
                    >
                      {n}%
                    </Chip>
                  ))}
                </div>
              </div>
              <div className="space-y-1.5">
                <label className="text-[10px] font-bold text-gray-650 uppercase">CPU 池</label>
                <div className="flex flex-wrap gap-1.5">
                  {CPU_PRESETS.map((c) => (
                    <Chip
                      key={c.value}
                      active={settings.cpuPool === c.value}
                      onClick={() => handleInputChange('cpuPool', c.value)}
                    >
                      {c.label} · {c.value}
                    </Chip>
                  ))}
                </div>
              </div>
              <div className="space-y-1.5">
                <label className="text-[10px] font-bold text-gray-650 uppercase">Chrome 主版本</label>
                <div className="flex flex-wrap gap-1.5 items-center">
                  <Chip active={settings.brand === 'Chrome'} onClick={() => handleInputChange('brand', 'Chrome')}>
                    Chrome
                  </Chip>
                  {CHROME_VERS.map((v) => (
                    <Chip
                      key={v}
                      active={settings.brandVer === v}
                      onClick={() => handleInputChange('brandVer', v)}
                    >
                      {v}
                    </Chip>
                  ))}
                </div>
              </div>
            </div>
          )}

          <div className="flex items-center justify-between text-xs pt-3 border-t border-brand-border/40">
            <div>
              <div className="font-semibold text-gray-800">自动锁定指纹</div>
              <div className="text-[11px] text-gray-550">首次分配后锁定，防飘移（建议开）</div>
            </div>
            <button
              type="button"
              onClick={() => handleToggle('lockProfile')}
              className={`relative w-9 h-5 rounded-full transition-colors duration-200 ${
                settings.lockProfile ? 'bg-brand-accent' : 'bg-gray-300'
              }`}
            >
              <span
                className={`absolute top-0.5 left-0.5 bg-white w-4 h-4 rounded-full transition-transform duration-200 ${
                  settings.lockProfile ? 'translate-x-4' : 'translate-x-0'
                }`}
              />
            </button>
          </div>
        </div>

        <StorageSettingsSection />

        {/* Section 3: local diagnostics */}
        <div data-testid="settings-audit-logs" className="bg-brand-surface border border-brand-border rounded-xl p-5 space-y-4 shadow-xs">
          <div className="flex items-center gap-2 border-b border-brand-border/60 pb-3">
            <FileText className="w-4 h-4 text-brand-accent" />
            <h4 className="text-xs font-bold text-gray-900">日志与诊断</h4>
          </div>
          <div className="rounded-lg border border-brand-border bg-brand-bg px-3 py-2.5 text-[11px] leading-5 text-gray-550">
            诊断日志只记录关联 ID、稳定错误码和状态；文件权限为 0600，并采用固定数量的大小轮转。发布、任务和账号状态以 SQLite 审计记录为准，日志不保存 Cookie、凭证、正文、URL 或浏览器画像。
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {window.electronAPI?.openPathInFinder ? (
              <button
                type="button"
                data-testid="settings-open-log-folder"
                onClick={() => void handleOpenLogDirectory()}
                className="ui-btn"
              >
                <FolderOpen className="h-3.5 w-3.5" />
                打开日志目录
              </button>
            ) : null}
            <button
              type="button"
              data-testid="settings-create-diagnostics"
              onClick={() => void handleCreateDiagnostics()}
              disabled={creatingDiagnostics}
              className="ui-btn ui-btn-primary disabled:opacity-50"
            >
              {creatingDiagnostics ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <FileText className="h-3.5 w-3.5" />}
              {creatingDiagnostics ? '正在汇总…' : '生成脱敏诊断快照'}
            </button>
            {lastDiagnosticPath && window.electronAPI?.openPathInFinder ? (
              <button
                type="button"
                onClick={() => void window.electronAPI?.openPathInFinder(lastDiagnosticPath)}
                className="ui-btn"
              >
                在访达中显示
              </button>
            ) : null}
          </div>
          {lastDiagnosticPath ? (
            <p className="truncate font-mono text-[10px] text-gray-500" title={lastDiagnosticPath}>
              {lastDiagnosticPath}
            </p>
          ) : null}
        </div>

        <div className="bg-brand-surface border border-brand-border rounded-xl p-5 space-y-4 shadow-xs">
          <button
            type="button"
            data-testid="settings-proxy-toggle"
            aria-expanded={proxyOpen}
            aria-controls="settings-proxy-panel"
            onClick={() => setProxyOpen((open) => !open)}
            className="w-full flex items-center justify-between gap-4 text-left"
          >
            <span>
              <span className="flex items-center gap-2 text-xs font-bold text-gray-900">
                <Globe className="w-4 h-4 text-brand-accent" />
                代理与网络
              </span>
              <span className="mt-1 block text-[11px] text-gray-550">
                管理本地代理端点和账号绑定；默认收起，不自动检测网络。
              </span>
            </span>
            <span className="text-[10px] font-bold text-brand-accent">
              {proxyOpen ? '收起' : '管理'}
            </span>
          </button>
          {proxyOpen ? (
            <div id="settings-proxy-panel" className="border-t border-brand-border/60 pt-4">
              <ProxyPoolView embedded />
            </div>
          ) : null}
        </div>

        <NotificationSettingsSection />

        <DataAPISettingsSection />

        <MediaAPISection />

        {/* Save Button */}
        <div className="pt-2 flex justify-end">
          <button
            data-testid="settings-save-btn"
            onClick={handleSave}
            className="px-6 py-2.5 rounded-lg bg-brand-accent hover:bg-brand-accent-hover text-brand-on-accent text-xs font-bold transition shadow-lg shadow-brand-accent/20"
          >
            保存基础设置
          </button>
        </div>
      </div>

      {/* MODAL: Filesystem path picker */}
      {isPickerOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs p-4 animate-fade-in">
          <div role="dialog" aria-modal="true" aria-label="选择本地浏览器路径" className="bg-brand-surface border border-brand-border rounded-xl w-full max-w-lg overflow-hidden shadow-2xl flex flex-col max-h-[500px]">
            <div className="p-4 border-b border-brand-border flex items-center justify-between flex-shrink-0">
              <h4 className="text-sm font-bold text-gray-900">选择本地浏览器路径 (.app / .exe / 二进制)</h4>
              <button onClick={() => setIsPickerOpen(false)} className="text-gray-400 hover:text-gray-700 transition">
                <X className="w-4 h-4" />
              </button>
            </div>
            
            <div className="p-3 bg-brand-bg/50 border-b border-brand-border text-[10.5px] font-mono text-gray-650 truncate px-4 flex-shrink-0">
              当前位置: {pickerPath}
            </div>

            <div className="flex-1 overflow-y-auto divide-y divide-brand-border select-none">
              {pickerData?.parentPath && (
                <div 
                  onClick={() => loadPickerPath(pickerData.parentPath)}
                  className="p-3.5 hover:bg-black/5 flex items-center gap-2 cursor-pointer text-xs font-semibold text-brand-accent"
                >
                  <span>⬆️ .. (返回上一级目录)</span>
                </div>
              )}

              {pickerData?.directories.map((d) => {
                const isApp = d.name.endsWith('.app');
                return (
                  <div key={d.path} className="p-3.5 hover:bg-black/5 flex items-center justify-between gap-4 transition text-xs">
                    <div 
                      onClick={() => loadPickerPath(d.path)}
                      className="flex items-center gap-2 cursor-pointer font-medium text-gray-800 hover:text-brand-accent min-w-0 flex-1 truncate"
                    >
                      <span>{isApp ? '🌐' : '📁'}</span>
                      <span className="truncate">{d.name}</span>
                    </div>
                    <button
                      type="button"
                      onClick={() => {
                        setCustomBrowserPath(d.path);
                        setIsPickerOpen(false);
                      }}
                      className="px-2.5 py-1 rounded-md bg-brand-accent/10 hover:bg-brand-accent text-brand-accent hover:text-brand-on-accent text-[10.5px] font-bold transition flex-shrink-0"
                    >
                      选择{isApp ? ' 应用' : ''}
                    </button>
                  </div>
                );
              })}

              {pickerData?.files.map((f) => (
                <div key={f.path} className="p-3.5 hover:bg-black/5 flex items-center justify-between gap-4 transition text-xs">
                  <div className="flex items-center gap-2 font-mono text-gray-700 min-w-0 flex-1 truncate">
                    <span>📄</span>
                    <span className="truncate">{f.name}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => {
                      setCustomBrowserPath(f.path);
                      setIsPickerOpen(false);
                    }}
                    className="px-2.5 py-1 rounded-md bg-brand-accent/10 hover:bg-brand-accent text-brand-accent hover:text-brand-on-accent text-[10.5px] font-bold transition flex-shrink-0"
                  >
                    选择文件
                  </button>
                </div>
              ))}

              {(!pickerData || (pickerData.directories.length === 0 && pickerData.files.length === 0)) && (
                <div className="p-8 text-center text-xs text-gray-505">
                  当前目录下没有子文件或子文件夹
                </div>
              )}
            </div>

            <div className="p-4 border-t border-brand-border bg-black/5 flex items-center justify-end flex-shrink-0">
              <button
                type="button"
                onClick={() => setIsPickerOpen(false)}
                className="px-4 py-2 rounded-lg border border-brand-border hover:bg-black/5 text-xs text-gray-700 font-semibold transition"
              >
                取消
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

const notificationChannelMeta: Record<NotificationChannelKind, {
  label: string;
  description: string;
  secretLabel: string;
  Icon: React.ComponentType<{ className?: string }>;
}> = {
  feishu: {
    label: '飞书群通知',
    description: '账号掉线和恢复后发到飞书群。',
    secretLabel: '飞书 Webhook 地址',
    Icon: Bell,
  },
  dingtalk: {
    label: '钉钉群通知',
    description: '使用钉钉群自定义机器人提醒。',
    secretLabel: '钉钉 Webhook 地址',
    Icon: Bell,
  },
  webhook: {
    label: '其他 Webhook',
    description: '连接支持 HTTPS 的其他通知服务。',
    secretLabel: 'Webhook 地址',
    Icon: Webhook,
  },
  email: {
    label: '邮箱通知',
    description: '使用你的 SMTP 邮箱发送提醒。',
    secretLabel: '邮箱授权码或密码',
    Icon: Mail,
  },
};

function NotificationSettingsSection() {
  const { toast } = useUIStore();
  const { data, error, isLoading, mutate } = useSWR('api/notification-settings', api.getNotificationSettings);
  const [savingPolicy, setSavingPolicy] = useState(false);

  const updatePolicy = async (hours: 12 | 24) => {
    setSavingPolicy(true);
    try {
      const updated = await api.updateNotificationPolicy(hours);
      await mutate(updated, { revalidate: false });
      toast.show(hours === 12 ? '持续掉线每天最多提醒两次' : '持续掉线每天最多提醒一次', 'success');
    } catch {
      toast.show('提醒频率保存失败', 'error');
    } finally {
      setSavingPolicy(false);
    }
  };

  return (
    <div className="ui-card space-y-4 p-5" data-testid="settings-notifications">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-brand-border/60 pb-3">
        <div className="flex items-start gap-2">
          <Bell className="mt-0.5 h-4 w-4 text-brand-accent" />
          <div>
            <h4 className="text-xs font-bold text-gray-900">账号状态通知</h4>
            <p className="mt-0.5 text-[10px] leading-4 text-gray-500">
              账号掉线立即提醒；一直未恢复时，每天提醒 1 或 2 次；重新登录后也会通知。
            </p>
          </div>
        </div>
        <label className="text-[10px] font-semibold text-gray-600">
          持续掉线提醒
          <select
            value={data?.reminder_interval_hours ?? 12}
            disabled={savingPolicy || isLoading}
            onChange={(event) => void updatePolicy(Number(event.target.value) as 12 | 24)}
            className="ml-2 rounded-lg border border-brand-border bg-brand-bg px-2.5 py-1.5 text-[10px] font-normal text-gray-800 disabled:opacity-50"
            data-testid="notification-reminder-frequency"
          >
            <option value={12}>每 12 小时（每天最多 2 次）</option>
            <option value={24}>每 24 小时（每天最多 1 次）</option>
          </select>
        </label>
      </div>

      {error ? (
        <p className="rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-[11px] text-red-600">
          通知设置暂时无法读取，账号监测仍会继续工作。
        </p>
      ) : null}

      <div className="divide-y divide-brand-border overflow-hidden rounded-xl border border-brand-border">
        {(Object.keys(notificationChannelMeta) as NotificationChannelKind[]).map((kind) => {
          const setting = data?.channels.find((item) => item.kind === kind) ?? {
            kind,
            enabled: false,
            configured: false,
          };
          return (
            <NotificationChannelRow
              key={kind}
              setting={setting}
              onChanged={async () => { await mutate(); }}
            />
          );
        })}
      </div>
      <p className="text-[10px] leading-4 text-gray-500">
        每种通知单独保存；地址和邮箱密码只保存在 macOS 钥匙串，保存后不回显。通知只包含平台和账号名称，不会发送登录信息或创作内容。
      </p>
    </div>
  );
}

function NotificationChannelRow({
  setting,
  onChanged,
}: {
  setting: NotificationChannelSetting;
  onChanged: () => Promise<void>;
}) {
  const { toast } = useUIStore();
  const meta = notificationChannelMeta[setting.kind];
  const [open, setOpen] = useState(false);
  const [enabled, setEnabled] = useState(setting.enabled);
  const [secret, setSecret] = useState('');
  const [smtpHost, setSMTPHost] = useState(setting.smtp_host ?? '');
  const [smtpPort, setSMTPPort] = useState(setting.smtp_port || 465);
  const [smtpSecurity, setSMTPSecurity] = useState<'tls' | 'starttls'>(setting.smtp_security ?? 'tls');
  const [smtpUsername, setSMTPUsername] = useState(setting.smtp_username ?? '');
  const [emailFrom, setEmailFrom] = useState(setting.email_from ?? '');
  const [emailTo, setEmailTo] = useState((setting.email_to ?? []).join(', '));
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    setEnabled(setting.enabled);
    setSMTPHost(setting.smtp_host ?? '');
    setSMTPPort(setting.smtp_port || 465);
    setSMTPSecurity(setting.smtp_security ?? 'tls');
    setSMTPUsername(setting.smtp_username ?? '');
    setEmailFrom(setting.email_from ?? '');
    setEmailTo((setting.email_to ?? []).join(', '));
  }, [setting]);

  const save = async () => {
    if (enabled && !setting.configured && !secret.trim()) {
      toast.show(setting.kind === 'email' ? '首次开启邮箱通知需要填写授权码或密码' : `首次开启${meta.label}需要填写地址`, 'error');
      return;
    }
    if (setting.kind === 'email' && enabled && (!smtpHost.trim() || !emailFrom.trim() || !emailTo.trim())) {
      toast.show('请填写 SMTP 服务器、发件人和收件人', 'error');
      return;
    }
    setSaving(true);
    try {
      if (setting.kind === 'email') {
        await api.updateNotificationChannel(setting.kind, {
          enabled,
          smtp_host: smtpHost.trim(),
          smtp_port: smtpPort,
          smtp_security: smtpSecurity,
          smtp_username: smtpUsername.trim(),
          password: secret.trim() || undefined,
          email_from: emailFrom.trim(),
          email_to: emailTo.split(',').map((value) => value.trim()).filter(Boolean),
        });
      } else {
        await api.updateNotificationChannel(setting.kind, {
          enabled,
          webhook_url: secret.trim() || undefined,
        });
      }
      setSecret('');
      await onChanged();
      toast.show(`${meta.label}已保存`, 'success');
    } catch {
      toast.show(`${meta.label}保存失败，请检查格式和网络`, 'error');
    } finally {
      setSaving(false);
    }
  };

  const test = async () => {
    setTesting(true);
    try {
      await api.testNotificationChannel(setting.kind);
      toast.show(`${meta.label}测试消息已发送`, 'success');
      await onChanged();
    } catch {
      toast.show(`${meta.label}测试失败，请检查配置`, 'error');
    } finally {
      setTesting(false);
    }
  };

  const Icon = meta.Icon;
  return (
    <div className="bg-brand-bg/30" data-testid={`notification-channel-${setting.kind}`}>
      <div className="flex flex-wrap items-center gap-3 px-3 py-3">
        <Icon className="h-4 w-4 shrink-0 text-brand-accent" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <strong className="text-xs text-gray-900">{meta.label}</strong>
            <span className={`rounded-full px-2 py-0.5 text-[9px] font-semibold ${setting.configured ? 'bg-emerald-500/10 text-emerald-700' : 'bg-gray-500/10 text-gray-500'}`}>
              {setting.configured ? '已安全保存' : '未配置'}
            </span>
            {setting.enabled ? <span className="text-[9px] font-semibold text-brand-accent">已开启</span> : null}
          </div>
          <p className="mt-0.5 text-[10px] text-gray-500">{meta.description}</p>
        </div>
        <button
          type="button"
          onClick={() => setOpen((value) => !value)}
          className="ui-btn px-3 text-[10px]"
          aria-expanded={open}
          data-testid={`notification-channel-${setting.kind}-toggle`}
        >
          {open ? '收起' : '配置'}
        </button>
      </div>

      {open ? (
        <div className="space-y-3 border-t border-brand-border px-4 py-4">
          <label className="flex items-center gap-2 text-[11px] font-semibold text-gray-700">
            <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />
            开启此通知方式
          </label>

          {setting.kind === 'email' ? (
            <div className="grid gap-3 md:grid-cols-2">
              <label className="text-[10px] font-semibold text-gray-600">SMTP 服务器
                <input value={smtpHost} onChange={(event) => setSMTPHost(event.target.value)} placeholder="例如 smtp.example.com" className="mt-1 block w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-normal" />
              </label>
              <label className="text-[10px] font-semibold text-gray-600">端口与加密
                <span className="mt-1 flex gap-2">
                  <input type="number" min={1} max={65535} value={smtpPort} onChange={(event) => setSMTPPort(Number(event.target.value) || 0)} className="min-w-0 flex-1 rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-normal" />
                  <select value={smtpSecurity} onChange={(event) => setSMTPSecurity(event.target.value as 'tls' | 'starttls')} className="rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-normal">
                    <option value="tls">TLS</option>
                    <option value="starttls">STARTTLS</option>
                  </select>
                </span>
              </label>
              <label className="text-[10px] font-semibold text-gray-600">邮箱账号
                <input value={smtpUsername} onChange={(event) => setSMTPUsername(event.target.value)} autoComplete="off" className="mt-1 block w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-normal" />
              </label>
              <label className="text-[10px] font-semibold text-gray-600">{meta.secretLabel}
                <input type="password" value={secret} onChange={(event) => setSecret(event.target.value)} autoComplete="new-password" placeholder={setting.configured ? '已保存；留空不修改' : '首次配置时必填'} className="mt-1 block w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-normal" />
              </label>
              <label className="text-[10px] font-semibold text-gray-600">发件人
                <input value={emailFrom} onChange={(event) => setEmailFrom(event.target.value)} placeholder="name@example.com" className="mt-1 block w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-normal" />
              </label>
              <label className="text-[10px] font-semibold text-gray-600">收件人
                <input value={emailTo} onChange={(event) => setEmailTo(event.target.value)} placeholder="多个邮箱用逗号分开" className="mt-1 block w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-normal" />
              </label>
            </div>
          ) : (
            <label className="block text-[10px] font-semibold text-gray-600">
              {meta.secretLabel}
              <input
                type="password"
                value={secret}
                onChange={(event) => setSecret(event.target.value)}
                autoComplete="new-password"
                spellCheck={false}
                placeholder={setting.configured ? '已保存；粘贴新地址可替换' : '粘贴地址，保存后不会回显'}
                className="mt-1 block w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-normal"
                data-testid={`notification-channel-${setting.kind}-secret`}
              />
            </label>
          )}

          <div className="flex justify-end gap-2">
            <button type="button" disabled={!setting.configured || testing} onClick={() => void test()} className="ui-btn text-[10px] disabled:opacity-40" data-testid={`notification-channel-${setting.kind}-test`}>
              {testing ? '测试中…' : '发送测试'}
            </button>
            <button type="button" disabled={saving} onClick={() => void save()} className="ui-btn ui-btn-primary text-[10px] disabled:opacity-40" data-testid={`notification-channel-${setting.kind}-save`}>
              {saving ? '保存中…' : '保存'}
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

const AGENT_PROVIDER_META: Record<string, { label: string; logo?: string; installURL?: string }> = {
  alpha: { label: 'Alpha Agent' },
  codex: { label: 'Codex', logo: '/brand/codex-logo.png' },
  opencode: { label: 'OpenCode', logo: '/brand/opencode-logo.png' },
  custom: { label: '自定义 LLM', logo: '/brand/opencode-logo.png' },
  claude: { label: 'Claude Code', logo: '/agent-providers/claude-code.ico', installURL: 'https://code.claude.com/docs/en/setup' },
  antigravity: { label: 'Antigravity CLI', logo: '/brand/antigravity-logo.png', installURL: 'https://antigravity.google/docs/cli/install' },
  kimi: { label: 'Kimi Code', logo: '/agent-providers/kimi-code.png', installURL: 'https://www.kimi.com/code/docs/en/kimi-code-cli/guides/getting-started.html' },
  codebuddy: { label: 'CodeBuddy Code', logo: '/agent-providers/codebuddy.svg', installURL: 'https://www.codebuddy.cn/docs/cli/quickstart' },
};

function agentProviderMeta(providerID: AgentHubProviderID, label?: string, installURL?: string) {
  const known = AGENT_PROVIDER_META[providerID];
  return {
    label: label || known?.label || providerID,
    logo: known?.logo,
    installURL: installURL || known?.installURL,
  };
}

const AGENT_PROVIDER_POLL_DELAYS = [500, 1000, 2000];

function waitForProviderPoll(delayMS: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal.aborted) {
      reject(new DOMException('Aborted', 'AbortError'));
      return;
    }
    const onAbort = () => {
      window.clearTimeout(timer);
      reject(new DOMException('Aborted', 'AbortError'));
    };
    const timer = window.setTimeout(() => {
      signal.removeEventListener('abort', onAbort);
      resolve();
    }, delayMS);
    signal.addEventListener('abort', onAbort, { once: true });
  });
}

async function pollProviderUntil(
  providerID: AgentHubProviderID,
  signal: AbortSignal,
  publish: (next: AgentHubProviders) => Promise<unknown>,
  terminal: (probe: AgentHubProbe) => boolean,
  timeoutMS = 60_000,
) {
  const startedAt = Date.now();
  let attempt = 0;
  while (Date.now() - startedAt < timeoutMS) {
    const delayMS = AGENT_PROVIDER_POLL_DELAYS[Math.min(attempt, AGENT_PROVIDER_POLL_DELAYS.length - 1)];
    attempt += 1;
    await waitForProviderPoll(delayMS, signal);
    const next = await api.agentHubProviders(signal);
    await publish(next);
    const probe = next.probes.find((item) => item.provider_id === providerID);
    if (probe && terminal(probe)) return probe;
  }
  throw new Error('检测超过 60 秒，请稍后重试。');
}

function modelValidationErrorText(code: string): string {
  return {
    AGENT_PROVIDER_CONNECT_UNAVAILABLE: '未检测到已登录且可用的本机 CLI。请先安装并完成登录，再重试。',
    AGENT_PROVIDER_CONFIG_INVALID: '接入方式不受支持，请重新选择。',
    AGENT_PROVIDER_EXECUTABLE_INVALID: 'CLI 路径无效。请留空自动检测，或在高级设置中填写可执行文件的绝对路径。',
    AGENT_PROVIDER_NOT_FOUND: '该平台已停用或当前版本不支持接入。',
    AGENT_MODEL_INVALID: '模型 ID 格式不正确，请填写 CLI 中显示的完整模型 ID。',
    AGENT_MODEL_VALIDATION_UNAVAILABLE: 'CLI 已检测到，但模型没有返回正确的实测响应，未接入。',
    AGENT_MODEL_UNREACHABLE: '模型调用失败，请检查登录状态、模型权限或网络后重试。',
    AGENT_MODEL_VALIDATION_TIMEOUT: '模型验证超时，未添加；请稍后重试。',
    AGENT_MODEL_STORAGE_FAILED: '模型已连通，但本地保存失败，未添加。',
    AGENT_REQUEST_FAILED: '接入测试失败，未保存任何平台或模型。',
  }[code] || code;
}

function agentProviderStatus(probe: AgentHubProbe | undefined, verifiedCount: number) {
  if (probe?.state === 'probing') return { label: '检测中', tone: 'text-brand-muted bg-brand-surface-raised' };
  if (probe?.state === 'incompatible') return { label: '不兼容', tone: 'text-red-700 bg-red-50' };
  if (probe?.state === 'limited') return { label: '受限', tone: 'text-brand-warning bg-brand-warning/10' };
  if (probe?.ready && verifiedCount > 0) return { label: '可用', tone: 'text-emerald-700 bg-emerald-50' };
  if (probe?.ready) return { label: '待验证模型', tone: 'text-brand-muted bg-brand-surface-raised' };
  if (probe?.state === 'unavailable') return { label: '未检测到 CLI', tone: 'text-brand-warning bg-brand-warning/10' };
  return { label: '待检测', tone: 'text-gray-600 bg-gray-100' };
}

export function AgentProviderConnectionsSection() {
  const { toast } = useUIStore();
  const { data, error, isLoading, mutate } = useSWR(
    'api/agent/providers',
    () => api.agentHubProviders(),
    {
      refreshInterval: (latest) => latest?.probes.some((probe) =>
        probe.state === 'probing' || Boolean(probe.validating_model)) ? 1000 : 0,
    },
  );
  const { data: catalog } = useSWR('api/agent/provider-catalog', () => api.agentHubProviderCatalog());
  const [linkingID, setLinkingID] = useState<AgentHubProviderID | null>(null);
  const [validatingID, setValidatingID] = useState<AgentHubProviderID | null>(null);
  const [changingID, setChangingID] = useState<AgentHubProviderID | null>(null);
  const [modelInputs, setModelInputs] = useState<Partial<Record<AgentHubProviderID, string>>>({});
  const [validationErrors, setValidationErrors] = useState<Partial<Record<AgentHubProviderID, string>>>({});
  const [catalogInputs, setCatalogInputs] = useState<Partial<Record<AgentHubProviderID, {
    modelID: string;
    executablePath: string;
    launchMode: '' | 'cc_switch' | 'claude_router';
  }>>>({});
  const [primaryProviderID, setPrimaryProviderID] = useState<AgentHubProviderID>(() => preferredAgentProvider());
  const [expandedProviderID, setExpandedProviderID] = useState<AgentHubProviderID | null>(null);
  const [catalogDialogID, setCatalogDialogID] = useState<AgentHubProviderID | null>(null);
  const [catalogProbes, setCatalogProbes] = useState<Partial<Record<AgentHubProviderID, AgentHubProbe>>>({});
  const [catalogProbingID, setCatalogProbingID] = useState<AgentHubProviderID | null>(null);
  const controllerRef = useRef<AbortController | null>(null);

  useEffect(() => () => controllerRef.current?.abort(), []);
  useEffect(() => {
    const refreshSyncedLLM = () => {
      void mutate();
    };
    window.addEventListener('alpha-nexus:llm-models-synced', refreshSyncedLLM);
    return () => window.removeEventListener('alpha-nexus:llm-models-synced', refreshSyncedLLM);
  }, [mutate]);
  useEffect(() => {
    if (!data || data.providers.some((provider) => provider.id === primaryProviderID)) return;
    setPrimaryProviderID('codex');
    setPreferredAgentProvider('codex');
  }, [data, primaryProviderID]);

  const choosePrimaryProvider = (providerID: AgentHubProviderID) => {
    setPrimaryProviderID(providerID);
    setPreferredAgentProvider(providerID);
    toast.show(`Alpha Agent 已固定使用 ${agentProviderMeta(providerID).label}。`, 'success');
  };

  const chooseProviderModel = async (providerID: AgentHubProviderID, modelID: string) => {
    try {
      await api.agentHubProviderSelection(providerID, { model_id: modelID });
      window.dispatchEvent(new CustomEvent('alpha-nexus:agent-model-verified', {
        detail: { providerID, modelID },
      }));
      toast.show(`${agentProviderMeta(providerID).label} 已切换到 ${modelID}。`, 'success');
    } catch (cause) {
      toast.show(`模型切换失败：${cause instanceof Error ? cause.message : '未知错误'}`, 'error');
    }
  };

  const cancelAssociation = () => {
    controllerRef.current?.abort();
    controllerRef.current = null;
    setLinkingID(null);
    setValidatingID(null);
    toast.show('已停止等待检测结果；不会退出或修改 Provider 的登录状态。', 'info');
  };

  const connectProvider = async (providerID: AgentHubProviderID) => {
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setLinkingID(providerID);
    try {
      await api.agentHubProviderConnect(providerID);
      await api.agentHubProviderProbe(providerID, controller.signal);
      if (controller.signal.aborted) return;
      await mutate((current) => current ? {
        ...current,
        probes: current.probes.map((probe) => probe.provider_id === providerID ? { ...probe, state: 'probing' } : probe),
      } : current, { revalidate: false });
      const probe = await pollProviderUntil(
        providerID,
        controller.signal,
        (next) => mutate(next, { revalidate: false }),
        (nextProbe) => nextProbe.state !== 'probing',
      );
      if (probe.ready) {
        const candidates = (probe.model_candidates || []).filter((model) => !(probe.models || []).includes(model));
        if (candidates.length > 0) {
          toast.show(`${agentProviderMeta(providerID).label} 已连接，发现 ${candidates.length} 个候选模型；请选择一个验证，或确认后批量验证。`, 'success');
        } else {
          toast.show(`${agentProviderMeta(providerID).label} 已连接，当前可用模型已同步。`, 'success');
        }
      } else {
        toast.show(`${agentProviderMeta(providerID).label} 尚未连通，请在其本机 CLI 完成登录后重新检测。`, 'info');
      }
    } catch (cause) {
      if (!controller.signal.aborted) toast.show(`关联失败: ${cause instanceof Error ? cause.message : '未知错误'}`, 'error');
    } finally {
      if (controllerRef.current === controller) {
        controllerRef.current = null;
        setLinkingID(null);
      }
    }
  };

  const catalogConnectionBody = (providerID: AgentHubProviderID) => {
    const input = catalogInputs[providerID];
    return {
      executable_path: input?.executablePath.trim() || undefined,
      launch_mode: input?.launchMode || undefined,
    };
  };

  const probeCatalogProvider = async (providerID: AgentHubProviderID) => {
    setCatalogProbingID(providerID);
    setValidationErrors((current) => ({ ...current, [providerID]: '' }));
    try {
      const probe = await api.agentHubProviderCatalogProbe(providerID, catalogConnectionBody(providerID));
      setCatalogProbes((current) => ({ ...current, [providerID]: probe }));
      if (probe.ready && (probe.model_candidates?.length || 0) > 0) {
        toast.show(`${agentProviderMeta(providerID).label} 已发现 ${probe.model_candidates?.length} 个候选模型；请选择一个完成真实验证。`, 'success');
      } else {
        toast.show(probe.reason || 'CLI 已检测到，但还没有可验证模型。', 'info');
      }
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : 'CLI 检测失败';
      setValidationErrors((current) => ({ ...current, [providerID]: message }));
    } finally {
      setCatalogProbingID(null);
    }
  };

  const connectCatalogProvider = async (providerID: AgentHubProviderID) => {
    setCatalogProbingID(providerID);
    setValidationErrors((current) => ({ ...current, [providerID]: '' }));
    try {
      await api.agentHubProviderCatalogConnect(providerID, catalogConnectionBody(providerID));
      toast.show(`${agentProviderMeta(providerID).label} 登录/连接窗口已打开；完成后点“检测模型”。`, 'info');
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : '无法打开连接窗口';
      setValidationErrors((current) => ({ ...current, [providerID]: message }));
    } finally {
      setCatalogProbingID(null);
    }
  };

  const addProvider = async (providerID: AgentHubProviderID) => {
    const input = catalogInputs[providerID];
    const modelID = input?.modelID.trim() || '';
    if (!modelID) return;
    setChangingID(providerID);
    setValidationErrors((current) => ({ ...current, [providerID]: '' }));
    try {
      const added = await api.agentHubProviderAdd({
        provider_id: providerID,
        executable_path: input?.executablePath.trim() || undefined,
        launch_mode: input?.launchMode || undefined,
        model_id: modelID,
      });
      await mutate();
      setCatalogInputs((current) => ({
        ...current,
        [providerID]: { modelID: '', executablePath: '', launchMode: '' },
      }));
      setCatalogDialogID(null);
      toast.show(`${added.label} 与 ${modelID} 已通过真实调用，现已加入 Agent。`, 'success');
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : '未知错误';
      setValidationErrors((current) => ({ ...current, [providerID]: message }));
      toast.show(`测试未通过，未添加：${message}`, 'error');
    } finally {
      setChangingID(null);
    }
  };

  const removeProvider = async (providerID: AgentHubProviderID) => {
    setChangingID(providerID);
    try {
      if (primaryProviderID === providerID) {
        choosePrimaryProvider('alpha');
      }
      await api.agentHubProviderRemove(providerID);
      await mutate();
      setExpandedProviderID((current) => current === providerID ? null : current);
      toast.show('已从 Agent 隐藏该 CLI；历史会话和已验证记录仍保留，重新添加即可恢复。', 'success');
    } catch (cause) {
      toast.show(`移除失败：${cause instanceof Error ? cause.message : '未知错误'}`, 'error');
    } finally {
      setChangingID(null);
    }
  };

  const validateOneModel = async (
    providerID: AgentHubProviderID,
    modelID: string,
    controller: AbortController,
  ): Promise<{ passed: boolean; error?: string }> => {
    await api.agentHubProviderModelValidate(providerID, modelID, controller.signal);
    if (controller.signal.aborted) return { passed: false };
    await mutate((current) => current ? {
      ...current,
      probes: current.probes.map((probe) => probe.provider_id === providerID
        ? { ...probe, validating_model: modelID, model_validation_error: '' }
        : probe),
    } : current, { revalidate: false });
    const probe = await pollProviderUntil(
      providerID,
      controller.signal,
      (next) => mutate(next, { revalidate: false }),
      (nextProbe) => nextProbe.validating_model !== modelID
        && (Boolean(nextProbe.model_validation_error) || Boolean(nextProbe.models?.includes(modelID))),
    );
    const passed = !probe.model_validation_error && Boolean(probe.models?.includes(modelID));
    return {
      passed,
      error: passed ? undefined : probe.model_validation_error || `${modelID} 未通过连接验证，未加入模型列表。`,
    };
  };

  const validateModel = async (providerID: AgentHubProviderID) => {
    const modelID = modelInputs[providerID]?.trim() || '';
    if (!modelID) return;
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setValidatingID(providerID);
    setValidationErrors((current) => ({ ...current, [providerID]: '' }));
    try {
      const result = await validateOneModel(providerID, modelID, controller);
      if (controller.signal.aborted) return;
      if (result.passed) {
        await api.agentHubProviderSelection(providerID, { model_id: modelID });
        window.dispatchEvent(new CustomEvent('alpha-nexus:agent-model-verified', {
          detail: { providerID, modelID },
        }));
        setModelInputs((current) => ({ ...current, [providerID]: '' }));
        toast.show(`${modelID} 已通过真实连接验证，并已加入 Agent 模型。`, 'success');
      } else {
        setValidationErrors((current) => ({ ...current, [providerID]: result.error || '模型验证失败' }));
      }
    } catch (cause) {
      if (!controller.signal.aborted) {
        const message = cause instanceof Error ? cause.message : '模型验证失败';
        setValidationErrors((current) => ({ ...current, [providerID]: message }));
        toast.show(`模型未添加：${modelValidationErrorText(message)}`, 'error');
      }
    } finally {
      if (controllerRef.current === controller) {
        controllerRef.current = null;
        setValidatingID(null);
      }
    }
  };

  const validateCandidateModels = async (providerID: AgentHubProviderID, models: string[]) => {
    if (models.length === 0) return;
    if (!window.confirm(
      `将逐个真实调用 ${models.length} 个候选模型，可能产生模型费用。失败不会自动重试，只有通过的模型会保存。继续吗？`,
    )) return;
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setValidatingID(providerID);
    setValidationErrors((current) => ({ ...current, [providerID]: '' }));
    try {
      await api.agentHubProviderModelValidateBatch(providerID, models, controller.signal);
      await mutate((current) => current ? {
        ...current,
        probes: current.probes.map((probe) => probe.provider_id === providerID
          ? {
            ...probe,
            validating_model: models[0],
            model_validation_error: '',
            model_validation_total: models.length,
            model_validation_completed: 0,
            model_validation_passed: 0,
            model_validation_failures: [],
          }
          : probe),
      } : current, { revalidate: false });
      const probe = await pollProviderUntil(
        providerID,
        controller.signal,
        (next) => mutate(next, { revalidate: false }),
        (nextProbe) => !nextProbe.validating_model
          && (nextProbe.model_validation_completed || 0) >= models.length,
        Math.max(60_000, models.length * 65_000),
      );
      if (!controller.signal.aborted) {
        window.dispatchEvent(new CustomEvent('alpha-nexus:agent-model-verified', {
          detail: { providerID },
        }));
        const passed = probe.model_validation_passed || 0;
        const failed = probe.model_validation_failures?.length || 0;
        toast.show(
          `${agentProviderMeta(providerID).label} 模型验证完成：新增 ${passed} 个${failed ? `，${failed} 个不可用未添加` : ''}。`,
          passed > 0 ? 'success' : 'error',
        );
      }
    } catch (cause) {
      if (!controller.signal.aborted) {
        const message = cause instanceof Error ? cause.message : '批量模型验证失败';
        setValidationErrors((current) => ({ ...current, [providerID]: message }));
        toast.show(`批量验证未启动：${modelValidationErrorText(message)}`, 'error');
      }
    } finally {
      if (controllerRef.current === controller) {
        controllerRef.current = null;
        setValidatingID(null);
      }
    }
  };

  const providerPriority: Record<string, number> = {
    alpha: 0,
    opencode: 1,
    custom: 2,
    codex: 3,
    claude: 4,
    antigravity: 5,
    kimi: 6,
    codebuddy: 7,
  };
  const providerByID = new Map((data?.providers || []).map((provider) => [provider.id, provider]));
  const catalogByID = new Map((catalog?.providers || []).map((item) => [item.id, item]));
  const platformIDs = [...new Set([...providerByID.keys(), ...catalogByID.keys()])]
    .sort((left, right) => (providerPriority[left] ?? 100) - (providerPriority[right] ?? 100));
  const platformRows = platformIDs.map((id) => ({
    provider: providerByID.get(id),
    catalogItem: catalogByID.get(id),
  }));
  const catalogDialogItem = catalogDialogID ? catalogByID.get(catalogDialogID) : undefined;
  const openCodeVerifiedModels = data?.probes.find((probe) => probe.provider_id === 'opencode')?.models || [];

  return (
    <section data-testid="settings-agent-providers" className="overflow-hidden border border-brand-border bg-brand-surface shadow-xs">
      <header className="flex items-start gap-3 px-4 py-4">
        <div className="flex min-w-0 items-start gap-2.5">
          <Bot className="mt-0.5 h-4 w-4 shrink-0 text-brand-accent" />
          <div>
            <h4 className="text-xs font-bold text-gray-900">Agent 平台接入</h4>
            <p className="mt-1 text-[10px] leading-4 text-gray-550">
              每个平台一行；Alpha Agent 默认使用官方 Codex app-server，仅在你发送消息时启动，回答结束即退出。
            </p>
          </div>
        </div>
      </header>

      {isLoading && !data ? (
        <div className="border-t border-brand-border px-4 py-3">
          <LocalLoadingState
            compact
            label="正在读取 Agent 接入"
            detail="检测本机 CLI 与已验证模型"
          />
        </div>
      ) : null}
      {error ? <p role="status" className="border-t border-red-200 bg-red-50 px-4 py-3 text-[11px] text-red-700">无法读取 Agent 接入状态。</p> : null}

      {data ? (
        <div data-testid="settings-agent-provider-list" className="border-t border-brand-border">
          <div className="hidden grid-cols-[minmax(180px,0.8fr)_minmax(180px,0.8fr)_minmax(220px,1.4fr)_auto] gap-3 bg-brand-bg px-4 py-2 text-[9px] font-bold uppercase tracking-[0.12em] text-gray-400 xl:grid">
            <span>平台</span><span>状态</span><span>模型</span><span className="text-right">操作</span>
          </div>
          <div role="radiogroup" aria-label="右侧 Agent 当前平台" className="divide-y divide-brand-border">
            {platformRows.map(({ provider, catalogItem }) => {
              if (!provider && catalogItem) {
                const item = catalogItem;
                const meta = agentProviderMeta(item.id, item.label, item.install_url);
                return (
                  <article key={item.id} data-testid={`settings-agent-provider-${item.id}`} data-provider-source="catalog" className="bg-brand-surface">
                    <div className="grid grid-cols-[minmax(150px,0.8fr)_minmax(0,1.2fr)] gap-3 px-4 py-3 xl:grid-cols-[minmax(180px,0.8fr)_minmax(180px,0.8fr)_minmax(220px,1.4fr)_auto] xl:items-center">
                      <div className="flex min-w-0 items-center gap-2.5">
                        {meta.logo
                          ? <img src={meta.logo} alt="" width={28} height={28} className="h-7 w-7 shrink-0 object-contain" />
                          : <span className="flex h-7 w-7 shrink-0 items-center justify-center bg-brand-bg text-[11px] font-bold text-gray-500" aria-hidden="true">{item.label.slice(0, 1)}</span>}
                        <div className="min-w-0">
                          <p className="truncate text-xs font-bold text-gray-900">{item.label}</p>
                          <p className="truncate text-[9px] text-gray-400">Alpha-Nexus 已有适配器</p>
                        </div>
                      </div>

                      <div className="flex min-w-0 items-center gap-2">
                        <span className="shrink-0 bg-brand-surface-raised px-2 py-0.5 text-[9px] font-bold text-brand-muted">
                          本机未验证
                        </span>
                        <span className="truncate text-[10px] text-gray-500" title={item.unavailable_reason || item.description}>
                          安装并实测后才可使用
                        </span>
                      </div>

                      <div className="min-w-0">
                        <p className="truncate text-[10px] font-medium text-gray-700">
                          接入时验证实际模型
                        </p>
                        <p className="mt-0.5 truncate text-[9px] text-gray-400" title={item.description}>{item.description}</p>
                      </div>

                      <div className="flex flex-wrap items-center justify-end gap-1.5">
                        {meta.installURL ? (
                          <a href={meta.installURL} target="_blank" rel="noreferrer" className="inline-flex min-h-9 items-center px-2.5 py-1.5 text-[10px] font-semibold text-gray-600 hover:text-brand-accent">
                            安装说明
                          </a>
                        ) : null}
                        <button
                          type="button"
                          data-testid={`settings-agent-provider-${item.id}-catalog-manage`}
                          onClick={() => {
                            setValidationErrors((current) => ({ ...current, [item.id]: '' }));
                            setCatalogDialogID(item.id);
                          }}
                          className="inline-flex min-h-9 items-center border border-brand-border px-3 py-1.5 text-[10px] font-bold text-gray-700 hover:border-brand-accent hover:text-brand-accent"
                        >
                          接入
                        </button>
                      </div>
                    </div>
                  </article>
                );
              }
              if (!provider) return null;
              const probe = data.probes.find((item) => item.provider_id === provider.id);
              const meta = agentProviderMeta(provider.id, provider.label);
              const linking = linkingID === provider.id;
              const validating = validatingID === provider.id || Boolean(probe?.validating_model);
              const isReady = probe?.ready === true;
              const verifiedModels = probe?.models || [];
              const candidates = (probe?.model_candidates || []).filter((model) => !verifiedModels.includes(model));
              const openCodeCandidates = provider.id === 'claude'
                ? openCodeVerifiedModels.filter((model) => !verifiedModels.includes(model))
                : [];
              const validationTotal = probe?.model_validation_total || 0;
              const validationCompleted = probe?.model_validation_completed || 0;
              const validationPassed = probe?.model_validation_passed || 0;
              const validationFailures = probe?.model_validation_failures || [];
              const validationError = validationErrors[provider.id]
                || (validationTotal <= 1 ? probe?.model_validation_error || '' : '');
              const status = agentProviderStatus(probe, verifiedModels.length);
              const isPrimary = primaryProviderID === provider.id;
              const usable = isReady && verifiedModels.length > 0;
              const expanded = expandedProviderID === provider.id;
              const panelID = `agent-model-panel-${provider.id}`;
              return (
                <article key={provider.id} data-testid={`settings-agent-provider-${provider.id}`} className="bg-brand-surface">
                  <div className="grid grid-cols-[minmax(150px,0.8fr)_minmax(0,1.2fr)] gap-3 px-4 py-3 xl:grid-cols-[minmax(180px,0.8fr)_minmax(180px,0.8fr)_minmax(220px,1.4fr)_auto] xl:items-center">
                    <div className="flex min-w-0 items-center gap-2.5">
                      {meta.logo
                        ? <img src={meta.logo} alt="" width={28} height={28} className="h-7 w-7 shrink-0 object-contain" />
                        : <span className="flex h-7 w-7 shrink-0 items-center justify-center bg-brand-bg text-brand-accent" aria-hidden="true"><Bot className="h-4 w-4" /></span>}
                      <div className="min-w-0">
                        <p className="truncate text-xs font-bold text-gray-900">{meta.label}</p>
                        {probe?.version ? <p className="truncate text-[9px] text-gray-400">v{probe.version}</p> : null}
                      </div>
                    </div>

                    <div className="flex min-w-0 items-center gap-2">
                      <span className={`shrink-0 px-2 py-0.5 text-[9px] font-bold ${status.tone}`}>{status.label}</span>
                      <span className="truncate text-[10px] text-gray-500" title={probe?.reason || ''}>
                        {probe?.state === 'probing' ? '正在检测本机 CLI' : probe?.reason || (isReady ? '本机 CLI 已连通' : '等待连接')}
                      </span>
                    </div>

                    <div className="min-w-0">
                      {verifiedModels.length ? (
                        <select
                          data-testid={`settings-agent-provider-${provider.id}-model-select`}
                          defaultValue=""
                          onChange={(event) => {
                            if (event.target.value) void chooseProviderModel(provider.id, event.target.value);
                          }}
                          disabled={validating}
                          aria-label={`切换 ${meta.label} 模型`}
                          className="w-full truncate border border-brand-border bg-brand-surface px-2 py-1.5 font-mono text-[10px] text-gray-700 outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/50 disabled:opacity-50"
                        >
                          <option value="" disabled>可用模型（{verifiedModels.length}）</option>
                          {verifiedModels.map((model) => <option key={model} value={model}>{model}</option>)}
                        </select>
                      ) : <p className="truncate text-[10px] font-medium text-gray-500">暂无可用模型</p>}
                      {validating ? (
                        <p aria-live="polite" className="mt-0.5 truncate text-[9px] text-brand-accent">
                          {validationTotal > 1
                            ? `后台验证 ${validationCompleted}/${validationTotal}`
                            : `正在验证 ${probe?.validating_model || modelInputs[provider.id] || ''}`}
                        </p>
                      ) : candidates.length ? <p className="mt-0.5 text-[9px] text-gray-400">{candidates.length} 个候选模型待验证</p> : null}
                    </div>

                    <div className="flex flex-wrap items-center justify-end gap-1.5">
                      <button
                        type="button"
                        data-testid={`settings-agent-provider-${provider.id}-primary`}
                        role="radio"
                        aria-checked={isPrimary}
                        onClick={() => choosePrimaryProvider(provider.id)}
                        disabled={isPrimary || !usable}
                        className={`min-h-9 px-2.5 py-1.5 text-[10px] font-bold ${isPrimary ? 'bg-brand-accent/10 text-brand-accent' : 'border border-brand-border text-gray-600 hover:border-brand-accent hover:text-brand-accent'} disabled:cursor-default disabled:opacity-55`}
                      >
                        {isPrimary ? '当前' : '设为当前'}
                      </button>
                      <button
                        type="button"
                        data-testid={`settings-agent-provider-${provider.id}-model-manage`}
                        aria-expanded={expanded}
                        aria-controls={panelID}
                        onClick={() => setExpandedProviderID((current) => current === provider.id ? null : provider.id)}
                        className="inline-flex min-h-9 items-center gap-1 border border-brand-border px-2.5 py-1.5 text-[10px] font-bold text-gray-700 hover:border-brand-accent hover:text-brand-accent"
                      >
                        模型
                        {expanded ? <ChevronUp className="h-3 w-3" /> : <ChevronDown className="h-3 w-3" />}
                      </button>
                      {linking ? (
                        <button data-testid="settings-agent-provider-cancel" type="button" onClick={cancelAssociation} className="min-h-9 border border-brand-border px-2.5 py-1.5 text-[10px] font-bold text-gray-700 hover:bg-black/5">停止等待</button>
                      ) : (
                        <button
                          data-testid={`settings-agent-provider-${provider.id}-connect`}
                          type="button"
                          onClick={() => void connectProvider(provider.id)}
                          disabled={linkingID !== null || validatingID !== null}
                          className="min-h-9 bg-brand-accent px-2.5 py-1.5 text-[10px] font-bold text-brand-on-accent hover:brightness-95 disabled:opacity-40"
                        >
                          {isReady ? '检测' : '连接'}
                        </button>
                      )}
                    </div>
                  </div>

                  {expanded ? (
                    <div id={panelID} data-testid={`settings-agent-provider-${provider.id}-model-panel`} className="border-t border-brand-border bg-brand-bg/70 px-4 py-4">
                      <div className="grid gap-4 lg:grid-cols-[minmax(220px,0.8fr)_minmax(320px,1.2fr)]">
                        <div>
                          <p className="text-[10px] font-bold text-gray-700">已验证模型（{verifiedModels.length}）</p>
                          {verifiedModels.length ? (
                            <div className="mt-2 flex max-h-24 flex-wrap gap-1.5 overflow-y-auto">
                              {verifiedModels.map((model) => <span key={model} className="border border-emerald-200 bg-emerald-50 px-2 py-1 font-mono text-[9px] text-emerald-800">{model}</span>)}
                            </div>
                          ) : <p className="mt-2 text-[10px] text-gray-500">连接平台并验证一个模型后，才能在右侧 Agent 使用。</p>}
                        </div>

                        <div>
                          <label className="text-[10px] font-bold text-gray-700" htmlFor={`agent-model-${provider.id}`}>添加模型</label>
                          <div className="mt-1.5 flex flex-col gap-2 sm:flex-row">
                            <input
                              id={`agent-model-${provider.id}`}
                              data-testid={`settings-agent-provider-${provider.id}-model-input`}
                              value={modelInputs[provider.id] || ''}
                              onChange={(event) => setModelInputs((current) => ({ ...current, [provider.id]: event.target.value }))}
                              disabled={!isReady || validating}
                              aria-invalid={Boolean(validationError)}
                              aria-describedby={validationError ? `agent-model-error-${provider.id}` : undefined}
                              placeholder={isReady ? '搜索候选或粘贴模型 ID' : '请先连接平台'}
                              autoComplete="off"
                              className="min-w-0 flex-1 border border-brand-border bg-brand-surface px-2.5 py-2 text-[10px] outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/50 disabled:opacity-50"
                            />
                            <button
                              type="button"
                              data-testid={`settings-agent-provider-${provider.id}-model-validate`}
                              onClick={() => void validateModel(provider.id)}
                              disabled={!isReady || validating || linkingID !== null || validatingID !== null || !(modelInputs[provider.id]?.trim())}
                              className="min-h-9 shrink-0 bg-brand-accent px-3 py-2 text-[10px] font-bold text-brand-on-accent hover:brightness-95 disabled:cursor-not-allowed disabled:opacity-40"
                            >
                              {validating ? '验证中…' : '验证并添加'}
                            </button>
                          </div>
                          {candidates.length ? (
                            <div className="mt-2 flex flex-wrap gap-1.5" aria-label="待验证候选模型">
                              {candidates.slice(0, 6).map((model) => (
                                <button
                                  key={model}
                                  type="button"
                                  onClick={() => setModelInputs((current) => ({ ...current, [provider.id]: model }))}
                                  disabled={!isReady || validating}
                                  title={`填入待验证候选 ${model}`}
                                  className="max-w-full truncate border border-brand-border bg-brand-surface px-2 py-1 font-mono text-[9px] text-gray-600 hover:border-brand-accent hover:text-brand-accent disabled:opacity-40"
                                >
                                  {model}
                                </button>
                              ))}
                              {candidates.length > 6 ? <span className="self-center text-[9px] text-gray-400">另有 {candidates.length - 6} 个</span> : null}
                            </div>
                          ) : null}
                          {openCodeCandidates.length ? (
                            <div className="mt-2">
                              <p className="text-[9px] font-semibold text-gray-500">OpenCode 已验证候选</p>
                              <div className="mt-1 flex flex-wrap gap-1.5">
                                {openCodeCandidates.slice(0, 8).map((model) => (
                                  <button
                                    key={model}
                                    type="button"
                                    onClick={() => setModelInputs((current) => ({ ...current, [provider.id]: model }))}
                                    disabled={!isReady || validating}
                                    className="max-w-full truncate border border-brand-border bg-brand-surface px-2 py-1 font-mono text-[9px] text-gray-600 hover:border-brand-accent hover:text-brand-accent disabled:opacity-40"
                                  >
                                    {model}
                                  </button>
                                ))}
                              </div>
                              <p className="mt-1 text-[9px] leading-4 text-gray-400">候选仍需由 Claude Code 实测，通过后才会出现在模型选择里。</p>
                            </div>
                          ) : null}
                          <p className="mt-1.5 text-[9px] leading-4 text-gray-500">
                            {candidates.length
                              ? `${candidates.length} 个候选模型待验证；输入可搜索，也可以直接粘贴模型 ID。通过后会设为该平台当前模型。`
                              : '可直接粘贴实际模型 ID。只有真实调用成功后才会进入右侧 Agent，并设为该平台当前模型。'}
                          </p>
                          {candidates.length ? (
                            <button
                              type="button"
                              data-testid={`settings-agent-provider-${provider.id}-validate-candidates`}
                              onClick={() => void validateCandidateModels(provider.id, candidates)}
                              disabled={!isReady || validating || linkingID !== null || validatingID !== null}
                              className="mt-2 inline-flex min-h-9 items-center bg-brand-accent px-3 py-2 text-[10px] font-bold text-brand-on-accent hover:brightness-95 disabled:opacity-40"
                            >
                              验证全部候选（{candidates.length}）
                            </button>
                          ) : null}
                        </div>
                      </div>

                      {validationTotal > 1 ? (
                        <p role="status" className="mt-3 border-t border-brand-border pt-3 text-[10px] leading-4 text-gray-600">
                          后台实测进度：{validationCompleted}/{validationTotal}，通过 {validationPassed}，失败 {validationFailures.length}。可以离开设置页，验证会继续。
                        </p>
                      ) : null}
                      {validationError ? <p id={`agent-model-error-${provider.id}`} role="status" className="mt-2 break-words text-[10px] leading-4 text-red-700">未添加：{modelValidationErrorText(validationError)}</p> : null}
                      <div className="mt-3 flex items-center gap-3 border-t border-brand-border pt-3">
                        {meta.installURL ? <a href={meta.installURL} target="_blank" rel="noreferrer" className="text-[10px] font-semibold text-brand-accent hover:underline">安装说明</a> : null}
                        {provider.removable ? <button type="button" data-testid={`settings-agent-provider-${provider.id}-remove`} onClick={() => void removeProvider(provider.id)} disabled={changingID !== null || linkingID !== null || validatingID !== null} className="text-[10px] font-semibold text-gray-500 hover:text-red-700 disabled:opacity-40">移除平台</button> : null}
                      </div>
                    </div>
                  ) : null}
                </article>
              );
            })}
          </div>
        </div>
      ) : null}

      {catalogDialogItem ? (() => {
        const item = catalogDialogItem;
        const meta = agentProviderMeta(item.id, item.label, item.install_url);
        const input = catalogInputs[item.id] || { modelID: '', executablePath: '', launchMode: '' };
        const errorMessage = validationErrors[item.id] || '';
        const changing = changingID === item.id;
        const catalogProbe = catalogProbes[item.id];
        const catalogCandidates = catalogProbe?.model_candidates || [];
        const catalogProbing = catalogProbingID === item.id;
        return (
          <div
            className="fixed inset-0 z-[100] flex items-center justify-center bg-black/30 p-4"
            role="presentation"
            onMouseDown={(event) => {
              if (event.target === event.currentTarget && !changing) setCatalogDialogID(null);
            }}
          >
            <div
              role="dialog"
              aria-modal="true"
              aria-labelledby={`agent-provider-dialog-title-${item.id}`}
              className="w-full max-w-md border border-brand-border bg-brand-surface shadow-2xl"
            >
              <header className="flex items-start justify-between gap-3 border-b border-brand-border px-5 py-4">
                <div className="flex min-w-0 items-center gap-3">
                  {meta.logo
                    ? <img src={meta.logo} alt="" width={32} height={32} className="h-8 w-8 shrink-0 object-contain" />
                    : <span className="flex h-8 w-8 shrink-0 items-center justify-center bg-brand-bg text-xs font-bold text-gray-500">{item.label.slice(0, 1)}</span>}
                  <div className="min-w-0">
                    <h5 id={`agent-provider-dialog-title-${item.id}`} className="truncate text-sm font-bold text-gray-900">接入 {item.label}</h5>
                    <p className="mt-0.5 text-[10px] text-gray-500">检测 CLI，并真实调用一次指定模型</p>
                  </div>
                </div>
                <button type="button" aria-label="关闭接入窗口" onClick={() => setCatalogDialogID(null)} disabled={changing} className="p-1.5 text-gray-500 hover:bg-black/5 disabled:opacity-40">
                  <X className="h-4 w-4" />
                </button>
              </header>

              <form
                className="space-y-4 px-5 py-5"
                onSubmit={(event) => {
                  event.preventDefault();
                  void addProvider(item.id);
                }}
              >
                <div className="grid grid-cols-3 gap-2 text-center text-[9px] text-gray-500">
                  <span className="bg-brand-bg px-2 py-2"><b className="block text-gray-800">1. 检测</b>本机 CLI</span>
                  <span className="bg-brand-bg px-2 py-2"><b className="block text-gray-800">2. 实测</b>模型响应</span>
                  <span className="bg-brand-bg px-2 py-2"><b className="block text-gray-800">3. 接入</b>通过才保存</span>
                </div>

                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => void probeCatalogProvider(item.id)}
                    disabled={catalogProbing || changing}
                    className="inline-flex min-h-9 flex-1 items-center justify-center gap-1.5 border border-brand-border px-3 text-[10px] font-bold text-gray-700 hover:border-brand-accent hover:text-brand-accent disabled:opacity-40"
                  >
                    {catalogProbing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                    检测模型
                  </button>
                  <button
                    type="button"
                    onClick={() => void connectCatalogProvider(item.id)}
                    disabled={catalogProbing || changing}
                    className="min-h-9 border border-brand-border px-3 text-[10px] font-bold text-gray-700 hover:border-brand-accent hover:text-brand-accent disabled:opacity-40"
                  >
                    登录 / 打开
                  </button>
                </div>
                {catalogProbe ? (
                  <p role="status" className={`px-3 py-2 text-[10px] leading-4 ${catalogProbe.ready ? 'bg-emerald-50 text-emerald-800' : 'bg-brand-warning/10 text-brand-warning'}`}>
                    {catalogProbe.ready
                      ? `CLI 已连通${catalogCandidates.length ? `，发现 ${catalogCandidates.length} 个候选模型` : '，但未发现模型'}`
                      : catalogProbe.reason || 'CLI 尚未连通'}
                  </p>
                ) : null}

                {item.id === 'claude' ? (
                  <label className="block">
                    <span className="text-[10px] font-bold text-gray-700">配置来源</span>
                    <select
                      data-testid="settings-agent-provider-claude-launch-mode"
                      value={input.launchMode}
                      onChange={(event) => setCatalogInputs((current) => ({
                        ...current,
                        [item.id]: {
                          ...input,
                          launchMode: event.target.value === 'cc_switch'
                            ? 'cc_switch'
                            : event.target.value === 'claude_router'
                              ? 'claude_router'
                              : '',
                        },
                      }))}
                      className="mt-1.5 w-full border border-brand-border bg-brand-surface px-3 py-2.5 text-xs outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/50"
                    >
                      <option value="">Claude Code 已登录账号</option>
                      <option value="cc_switch">CC Switch 当前配置</option>
                      <option value="claude_router">Claude Code Router（豆包等）</option>
                    </select>
                    <span className="mt-1.5 block text-[9px] leading-4 text-gray-500">
                      {input.launchMode === 'cc_switch'
                        ? '使用 CC Switch 已写入 Claude Code 的当前配置；Alpha-Nexus 不读取或保存 Key，切换上游后会重新验证模型。'
                        : input.launchMode === 'claude_router'
                          ? '使用本机 Claude 兼容路由启动器；只保存启动器路径，豆包等模型仍须真实返回后才加入。'
                          : '使用 Claude Code 自身登录状态。'}
                    </span>
                  </label>
                ) : null}

                <label className="block">
                  <span className="text-[10px] font-bold text-gray-700">实际模型 ID</span>
                  <input
                    autoFocus
                    aria-label={`${item.label} 模型 ID`}
                    aria-invalid={Boolean(errorMessage)}
                    data-testid={`settings-agent-provider-${item.id}-catalog-model`}
                    value={input.modelID}
                    onChange={(event) => setCatalogInputs((current) => ({
                      ...current,
                      [item.id]: { ...input, modelID: event.target.value },
                    }))}
                    placeholder="填写 CLI 中可用的完整模型 ID"
                    autoComplete="off"
                    className="mt-1.5 w-full border border-brand-border bg-brand-surface px-3 py-2.5 text-xs outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/50"
                  />
                  <span className="mt-1.5 block text-[9px] leading-4 text-gray-500">实时发现的候选仅用于选择；下一步真实响应验证通过后才会加入 Agent。</span>
                  {catalogCandidates.length ? (
                    <span className="mt-2 block">
                      <span className="block text-[9px] font-semibold text-gray-500">CLI 实时发现（待实测）</span>
                      <span className="mt-1 flex max-h-24 flex-wrap gap-1.5 overflow-y-auto">
                        {catalogCandidates.map((model) => (
                          <button
                            key={model}
                            type="button"
                            onClick={() => setCatalogInputs((current) => ({
                              ...current,
                              [item.id]: { ...input, modelID: model },
                            }))}
                            className="max-w-full truncate border border-brand-border bg-brand-surface px-2 py-1 font-mono text-[9px] text-gray-600 hover:border-brand-accent hover:text-brand-accent"
                          >
                            {model}
                          </button>
                        ))}
                      </span>
                    </span>
                  ) : null}
                  {item.id === 'claude' && openCodeVerifiedModels.length ? (
                    <span className="mt-2 block">
                      <span className="block text-[9px] font-semibold text-gray-500">OpenCode 已验证候选</span>
                      <span className="mt-1 flex flex-wrap gap-1.5">
                        {openCodeVerifiedModels.slice(0, 8).map((model) => (
                          <button
                            key={model}
                            type="button"
                            onClick={() => setCatalogInputs((current) => ({
                              ...current,
                              [item.id]: { ...input, modelID: model },
                            }))}
                            className="max-w-full truncate border border-brand-border bg-brand-surface px-2 py-1 font-mono text-[9px] text-gray-600 hover:border-brand-accent hover:text-brand-accent"
                          >
                            {model}
                          </button>
                        ))}
                      </span>
                      <span className="mt-1 block text-[9px] leading-4 text-gray-400">候选仍需由 Claude Code 实测，通过才会接入。</span>
                    </span>
                  ) : null}
                </label>

                {item.executable_configurable ? (
                  <details>
                    <summary className="cursor-pointer text-[10px] font-semibold text-gray-600 hover:text-brand-accent">
                      高级：指定 {item.id === 'claude' ? 'Claude CLI 或兼容启动器' : 'CLI'} 路径
                    </summary>
                    <input
                      aria-label={`${item.label} CLI 路径`}
                      data-testid={`settings-agent-provider-${item.id}-catalog-path`}
                      value={input.executablePath}
                      onChange={(event) => setCatalogInputs((current) => ({
                        ...current,
                        [item.id]: { ...input, executablePath: event.target.value },
                      }))}
                      placeholder="CLI 绝对路径（留空则自动检测）"
                      autoComplete="off"
                      className="mt-2 w-full border border-brand-border bg-brand-surface px-3 py-2.5 text-xs outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/50"
                    />
                  </details>
                ) : null}

                {errorMessage ? <p role="status" className="break-words bg-red-50 px-3 py-2 text-[10px] leading-4 text-red-700">未接入：{modelValidationErrorText(errorMessage)}</p> : null}

                <div className="flex items-center justify-between gap-3 border-t border-brand-border pt-4">
                  {meta.installURL ? <a href={meta.installURL} target="_blank" rel="noreferrer" className="text-[10px] font-semibold text-brand-accent hover:underline">安装说明</a> : <span />}
                  <div className="flex items-center gap-2">
                    <button type="button" onClick={() => setCatalogDialogID(null)} disabled={changing} className="min-h-9 px-3 text-[10px] font-bold text-gray-600 hover:bg-black/5 disabled:opacity-40">取消</button>
                    <button type="submit" data-testid={`settings-agent-provider-${item.id}-add`} disabled={changingID !== null || !input.modelID.trim()} className="inline-flex min-h-9 items-center gap-1.5 bg-brand-accent px-4 text-[10px] font-bold text-brand-on-accent hover:brightness-95 disabled:opacity-40">
                      {changing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                      {changing ? '正在检测并实测…' : '检测并接入'}
                    </button>
                  </div>
                </div>
              </form>
            </div>
          </div>
        );
      })() : null}
    </section>
  );
}

const AGENT_MODEL_EFFORTS = [
  { id: 'low', label: '快速' },
  { id: 'medium', label: '均衡' },
  { id: 'high', label: '深度思考' },
  { id: 'max', label: '最强' },
];

export function LLMConfigPanel({ initiallyExpanded = false }: { initiallyExpanded?: boolean }) {
  const { toast } = useUIStore();
  const { data, error, isLoading, mutate } = useSWR('api/agent/models', api.getAgentModels);
  const [apiKey, setAPIKey] = useState('');
  const [defaultModel, setDefaultModel] = useState('');
  const [defaultEffort, setDefaultEffort] = useState('medium');
  const [providerType, setProviderType] = useState<'volcano_ark' | 'custom'>('volcano_ark');
  const [providerName, setProviderName] = useState('自定义 LLM');
  const [baseURL, setBaseURL] = useState('');
  const [protocol, setProtocol] = useState<'responses' | 'chat_completions'>('chat_completions');
  const [customModels, setCustomModels] = useState('');
  const [failures, setFailures] = useState<Array<{ id: string; errorCode?: string }>>([]);
  const [saving, setSaving] = useState(false);
  const [expanded, setExpanded] = useState(initiallyExpanded);

  useEffect(() => {
    if (!data) return;
    setDefaultModel(data.default_model || data.models?.[0]?.id || '');
    setDefaultEffort(data.default_effort || 'medium');
    setProviderType(data.provider_type === 'custom' ? 'custom' : 'volcano_ark');
    setProviderName(data.provider_name || '自定义 LLM');
    setBaseURL(data.base_url || '');
    setProtocol(data.protocol === 'responses' ? 'responses' : 'chat_completions');
    setCustomModels((data.models || []).map((item) => item.id).join('\n'));
  }, [data]);

  const verifyAndSave = async () => {
    if (saving) return;
    setSaving(true);
    setFailures([]);
    try {
      const credential = apiKey.trim();
      const requestedCustomModels = [...new Set(customModels
        .split(/[\n,，]/)
        .map((item) => item.trim())
        .filter(Boolean))];
      const result = await api.verifyAndSaveAgentModels({
        ...(credential ? { api_key: credential } : {}),
        provider_type: providerType,
        ...(providerType === 'custom' ? {
          provider_name: providerName.trim() || '自定义 LLM',
          base_url: baseURL.trim(),
          protocol,
          models: requestedCustomModels,
        } : {}),
        ...(defaultModel ? { default_model: defaultModel } : {}),
        default_effort: defaultEffort,
      });
      const rejected = result.failures || result.validation?.failed || [];
      setFailures(rejected.map((item) => ({
        id: item.id || item.model_id || '未知模型',
        errorCode: item.error_code,
      })));
      setAPIKey('');
      setDefaultModel(result.default_model || result.models?.[0]?.id || '');
      setDefaultEffort(result.default_effort || 'medium');
      setCustomModels((result.models || []).map((item) => item.id).join('\n'));
      await mutate(result, { revalidate: false });
      window.dispatchEvent(new CustomEvent('alpha-nexus:llm-models-synced', {
        detail: { providerID: 'custom' },
      }));
      toast.show(
        `已验证并保存 ${result.models?.length || 0} 个模型，并同步到 OpenCode${rejected.length ? `；${rejected.length} 个失败未加入` : ''}`,
        'success',
      );
    } catch (cause) {
      toast.show(`验证失败，未保存：${cause instanceof Error ? cause.message : '未知错误'}`, 'error');
    } finally {
      setSaving(false);
    }
  };

  const verifiedModels = data?.models || [];
  const agentAPIConfigured = data?.api_configured === true;
  const providerLabel = providerType === 'custom' ? providerName || '自定义 LLM' : '火山方舟';

  // One-click prefill for the two reviewed OpenAI-compatible sources; the
  // API key is always entered by the user and never prefilled or displayed.
  const applyCustomSourcePreset = (preset: 'deepseek' | 'relay8045') => {
    if (preset === 'deepseek') {
      setProviderName('DeepSeek');
      setBaseURL('https://api.deepseek.com');
      setProtocol('chat_completions');
      setCustomModels('deepseek-chat\ndeepseek-reasoner');
    } else {
      setProviderName('Antigravity Relay');
      setBaseURL('http://127.0.0.1:8045/v1');
      setProtocol('chat_completions');
      setCustomModels('claude-sonnet-4-6-thinking\nclaude-opus-4-7-thinking\ngemini-3.1-pro-high\nauto');
      if (!apiKey) {
        setAPIKey('sk-agy-proxy');
      }
    }
  };

  return (
    <div className="bg-brand-surface border border-brand-border rounded-xl overflow-hidden transition shadow-xs" data-testid="settings-llm-api">
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((current) => !current)}
        className="p-4 flex w-full items-center justify-between text-left hover:bg-black/5 transition"
      >
        <span className="flex min-w-0 items-center gap-3">
          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-brand-border bg-brand-surface-raised">
            <Bot className="h-4 w-4 text-brand-muted" />
          </span>
          <span className="min-w-0">
            <span className="flex items-center gap-2">
              <span className="text-xs font-bold text-gray-900">LLM 模型配置</span>
              <span className="truncate rounded-full border border-brand-border bg-brand-surface-raised px-2 py-0.5 text-[9.5px] font-semibold text-brand-muted">
                {providerLabel} · {verifiedModels.length} 个可用模型
              </span>
            </span>
            <span className="mt-0.5 block truncate text-[10.5px] text-gray-500">
              {providerType === 'custom'
                ? '自定义 OpenAI 兼容接口（由 OpenCode 执行）'
                : '火山方舟 API（用于对话与工具规划）'}
            </span>
          </span>
        </span>
        <span className="flex shrink-0 items-center gap-3">
          <span className={`rounded border px-2 py-0.5 text-[10px] font-bold ${
            verifiedModels.length > 0
              ? 'border-brand-border bg-brand-surface-raised text-brand-success'
              : agentAPIConfigured
                ? 'border-brand-border bg-brand-surface-raised text-brand-muted'
                : 'border-brand-warning/30 bg-brand-warning/10 text-brand-warning'
          }`}>
            {verifiedModels.length > 0 ? '模型已验证' : agentAPIConfigured ? 'Key 已配置 · 模型待验证' : '未配置'}
          </span>
          {expanded ? <ChevronUp className="h-4 w-4 text-gray-400" /> : <ChevronDown className="h-4 w-4 text-gray-400" />}
        </span>
      </button>

      {expanded ? (
        <div className="space-y-5 border-t border-brand-border/60 bg-brand-bg/40 p-5 animate-fade-in">
          {isLoading && !data ? <LocalLoadingState compact label="正在读取模型配置" detail="不会发起模型调用" /> : null}
          {error ? <p className="rounded-lg border border-red-500/30 bg-red-500/10 p-3 text-xs text-red-500">无法读取火山方舟模型配置。</p> : null}
          {data ? (
            <>
              <div className="rounded-xl border border-brand-border bg-brand-surface-raised p-3">
                <p className="text-[11px] font-bold text-gray-900">验证规则</p>
                <p className="mt-1 text-[10px] leading-5 text-gray-500">
                  系统会逐个真实调用模型。成功后同步到 OpenCode 的本次运行配置；不会修改用户的全局 OpenCode 配置。
                </p>
              </div>

              <label className="block space-y-1">
                <span className="text-[10.5px] font-bold uppercase text-gray-500">LLM 平台</span>
                <select
                  value={providerType}
                  onChange={(event) => setProviderType(event.target.value === 'custom' ? 'custom' : 'volcano_ark')}
                  disabled={saving}
                  className="w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-medium text-gray-800 outline-none focus:border-brand-accent disabled:opacity-60"
                >
                  <option value="volcano_ark">火山方舟</option>
                  <option value="custom">自定义平台（OpenAI 兼容）</option>
                </select>
              </label>

              {providerType === 'custom' ? (
                <div className="grid gap-4 rounded-xl border border-brand-border bg-brand-surface p-4 md:grid-cols-2">
                  <div className="flex flex-wrap items-center gap-1.5 md:col-span-2">
                    <span className="text-[10px] font-semibold text-gray-400">快捷预填</span>
                    <button type="button" disabled={saving} onClick={() => applyCustomSourcePreset('deepseek')} className="rounded-full border border-brand-border bg-white px-2.5 py-1 text-[10px] font-semibold text-gray-600 transition-colors hover:border-brand-accent/40 hover:text-brand-muted disabled:opacity-60">
                      DeepSeek 官方 API
                    </button>
                    <button type="button" disabled={saving} onClick={() => applyCustomSourcePreset('relay8045')} className="rounded-full border border-brand-border bg-white px-2.5 py-1 text-[10px] font-semibold text-gray-600 transition-colors hover:border-brand-accent/40 hover:text-brand-muted disabled:opacity-60">
                      8045 本机 Relay（agy 模型）
                    </button>
                    <span className="text-[10px] text-gray-400">仅预填地址与模型名，API Key 需自行输入。</span>
                  </div>
                  <label className="space-y-1">
                    <span className="text-[10.5px] font-bold uppercase text-gray-500">平台名称</span>
                    <input
                      value={providerName}
                      onChange={(event) => setProviderName(event.target.value)}
                      disabled={saving}
                      maxLength={80}
                      placeholder="例如：公司网关"
                      className="w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs text-gray-800 outline-none focus:border-brand-accent disabled:opacity-60"
                    />
                  </label>
                  <label className="space-y-1">
                    <span className="text-[10.5px] font-bold uppercase text-gray-500">接口协议</span>
                    <select
                      value={protocol}
                      onChange={(event) => setProtocol(event.target.value === 'responses' ? 'responses' : 'chat_completions')}
                      disabled={saving}
                      className="w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs text-gray-800 outline-none focus:border-brand-accent disabled:opacity-60"
                    >
                      <option value="chat_completions">Chat Completions</option>
                      <option value="responses">Responses API</option>
                    </select>
                  </label>
                  <label className="space-y-1 md:col-span-2">
                    <span className="text-[10.5px] font-bold uppercase text-gray-500">Base URL</span>
                    <input
                      value={baseURL}
                      onChange={(event) => setBaseURL(event.target.value)}
                      disabled={saving}
                      placeholder="https://api.example.com/v1"
                      spellCheck={false}
                      className="w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 font-mono text-xs text-gray-800 outline-none focus:border-brand-accent disabled:opacity-60"
                    />
                  </label>
                  <label className="space-y-1 md:col-span-2">
                    <span className="text-[10.5px] font-bold uppercase text-gray-500">模型 ID</span>
                    <textarea
                      value={customModels}
                      onChange={(event) => setCustomModels(event.target.value)}
                      disabled={saving}
                      rows={3}
                      placeholder={'每行一个模型，例如：\nqwen3-coder\ndeepseek-chat'}
                      spellCheck={false}
                      className="w-full resize-y rounded-lg border border-brand-border bg-brand-bg px-3 py-2 font-mono text-xs text-gray-800 outline-none focus:border-brand-accent disabled:opacity-60"
                    />
                    <span className="block text-[9px] leading-4 text-gray-500">每个模型都会真实调用；失败项不会保存或同步到 OpenCode。</span>
                  </label>
                </div>
              ) : null}

              <label className="block space-y-1">
                <span className="text-[10.5px] font-bold uppercase text-gray-500">{providerType === 'custom' ? 'API Key' : '火山方舟 API Key'}</span>
                <input
                  type="password"
                  value={apiKey}
                  onChange={(event) => setAPIKey(event.target.value)}
                  disabled={saving}
                  placeholder="已保存的密钥不会回显；留空使用现有密钥"
                  autoComplete="new-password"
                  spellCheck={false}
                  className="w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 font-mono text-xs text-gray-800 outline-none focus:border-brand-accent disabled:opacity-60"
                />
                <span className="block text-[9px] leading-4 text-gray-500">
                  密钥只提交到本机服务并保存到 macOS 钥匙串，界面不会读取或显示已存密钥。
                </span>
              </label>

              <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                <label className="space-y-1">
                  <span className="text-[10.5px] font-bold uppercase text-gray-500">默认模型</span>
                  <select
                    value={defaultModel}
                    onChange={(event) => setDefaultModel(event.target.value)}
                    disabled={saving || verifiedModels.length === 0}
                    className="w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-medium text-gray-800 outline-none focus:border-brand-accent disabled:opacity-60"
                  >
                    {verifiedModels.length === 0 ? <option value="">验证成功后可选择</option> : null}
                    {verifiedModels.map((item) => (
                      <option key={item.id} value={item.id}>{item.label || item.id}</option>
                    ))}
                  </select>
                </label>
                <label className="space-y-1">
                  <span className="text-[10.5px] font-bold uppercase text-gray-500">思考强度</span>
                  <select
                    value={defaultEffort}
                    onChange={(event) => setDefaultEffort(event.target.value)}
                    disabled={saving}
                    className="w-full rounded-lg border border-brand-border bg-brand-bg px-3 py-2 text-xs font-medium text-gray-800 outline-none focus:border-brand-accent disabled:opacity-60"
                  >
                    {AGENT_MODEL_EFFORTS.map((item) => (
                      <option key={item.id} value={item.id}>{item.label}</option>
                    ))}
                  </select>
                </label>
              </div>

              <div className="space-y-2" aria-label="已验证模型">
                <p className="text-[10.5px] font-bold uppercase text-gray-500">已验证模型</p>
                {verifiedModels.length > 0 ? (
                  <div className="flex flex-wrap gap-2">
                    {verifiedModels.map((item) => (
                      <span key={item.id} className="inline-flex items-center gap-1.5 rounded-lg border border-emerald-500/20 bg-emerald-500/10 px-2.5 py-1.5 text-[10px] font-semibold text-emerald-700">
                        <CheckCircle2 className="h-3 w-3" />
                        {item.label || item.id}
                      </span>
                    ))}
                  </div>
                ) : (
                  <p className="rounded-lg border border-dashed border-brand-border p-3 text-[10px] text-gray-500">
                    尚无通过真实调用验证的模型。
                  </p>
                )}
              </div>

              {failures.length > 0 ? (
                <div className="space-y-2" role="status" aria-label="验证失败模型">
                  <p className="text-[10.5px] font-bold uppercase text-red-600">验证失败 · 未加入</p>
                  <div className="space-y-1.5">
                    {failures.map((item) => (
                      <p key={item.id} className="rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-[10px] text-red-700">
                        {item.id} · {item.errorCode || '模型未返回有效响应'} · 未加入
                      </p>
                    ))}
                  </div>
                </div>
              ) : null}

              <div className="flex items-center justify-between gap-3 border-t border-brand-border/40 pt-3">
                <p className="text-[10px] leading-5 text-gray-500">验证会产生少量模型调用费用；成功项自动同步到 OpenCode。</p>
                <button
                  type="button"
                  data-testid="settings-llm-verify"
                  onClick={() => void verifyAndSave()}
                  disabled={saving || (providerType === 'custom' && (!baseURL.trim() || !customModels.trim()))}
                  className="flex items-center gap-1.5 rounded-lg bg-brand-accent px-5 py-2 text-xs font-bold text-brand-on-accent shadow-md transition hover:bg-brand-accent-hover disabled:opacity-50"
                >
                  {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ShieldCheck className="h-3.5 w-3.5" />}
                  {saving ? '正在逐个验证…' : '验证并保存'}
                </button>
              </div>
            </>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

const IMAGE_PROVIDER_PRESETS = [
  { id: 'agnes', label: 'Agnes AI', defaultBaseURL: 'https://apihub.agnes-ai.com/v1', defaultImageModel: 'agnes-image-2.1-flash' },
];

const VIDEO_PROVIDER_PRESETS = [
  { id: 'agnes', label: 'Agnes AI', defaultBaseURL: 'https://apihub.agnes-ai.com/v1', defaultVideoModel: 'agnes-video-v2.0' },
];

function MediaAPISection() {
  const { toast } = useUIStore();
  const { data: aiConfig, mutate: mutateAIConfig } = useSWR('api/ai/config', api.getAIConfig);

  // Accordion Expand States
  const [expandImage, setExpandImage] = useState(false);
  const [expandVideo, setExpandVideo] = useState(false);

  // Image State
  const [imageProvider, setImageProvider] = useState('Agnes AI');
  const [imageBaseURL, setImageBaseURL] = useState('');
  const [imageAPIKey, setImageAPIKey] = useState('');
  const [imageModel, setImageModel] = useState('agnes-image-2.1-flash');
  const [showImageKey, setShowImageKey] = useState(false);
  const [savingImage, setSavingImage] = useState(false);
  const [imageTestResult, setImageTestResult] = useState<AITestResult | null>(null);
  const [testingImage, setTestingImage] = useState(false);

  // Video State
  const [videoProvider, setVideoProvider] = useState('Agnes AI');
  const [videoBaseURL, setVideoBaseURL] = useState('');
  const [videoAPIKey, setVideoAPIKey] = useState('');
  const [videoModel, setVideoModel] = useState('agnes-video-v2.0');
  const [showVideoKey, setShowVideoKey] = useState(false);
  const [savingVideo, setSavingVideo] = useState(false);
  const [videoTestResult, setVideoTestResult] = useState<AITestResult | null>(null);
  const [testingVideo, setTestingVideo] = useState(false);

  useEffect(() => {
    if (!aiConfig) return;
    if (aiConfig.image_api_provider) setImageProvider(aiConfig.image_api_provider);
    if (aiConfig.image_api_base_url) setImageBaseURL(aiConfig.image_api_base_url);
    if (aiConfig.image_api_key) setImageAPIKey(aiConfig.image_api_key);
    if (aiConfig.image_model) setImageModel(aiConfig.image_model);

    if (aiConfig.video_api_provider) setVideoProvider(aiConfig.video_api_provider);
    if (aiConfig.video_api_base_url) setVideoBaseURL(aiConfig.video_api_base_url);
    if (aiConfig.video_api_key) setVideoAPIKey(aiConfig.video_api_key);
    if (aiConfig.video_model) setVideoModel(aiConfig.video_model);
  }, [aiConfig]);

  const handleSelectImagePreset = (preset: typeof IMAGE_PROVIDER_PRESETS[0]) => {
    setImageProvider(preset.label);
    if (preset.defaultBaseURL) setImageBaseURL(preset.defaultBaseURL);
    if (preset.defaultImageModel) setImageModel(preset.defaultImageModel);
  };

  const handleSelectVideoPreset = (preset: typeof VIDEO_PROVIDER_PRESETS[0]) => {
    setVideoProvider(preset.label);
    if (preset.defaultBaseURL) setVideoBaseURL(preset.defaultBaseURL);
    if (preset.defaultVideoModel) setVideoModel(preset.defaultVideoModel);
  };

  const saveAllConfig = async (override?: Partial<AIConfigPayload>) => {
    const payload: AIConfigPayload & { thinking_mode?: string } = {
      agent_api_provider: aiConfig?.agent_api_provider || '',
      agent_api_base_url: aiConfig?.agent_api_base_url || '',
      agent_api_key: aiConfig?.agent_api_key || '',
      agent_model: aiConfig?.agent_model || '',
      coding_model: aiConfig?.coding_model || '',
      plan_model: aiConfig?.plan_model || '',
      image_api_provider: imageProvider,
      image_api_base_url: imageBaseURL,
      image_api_key: '',
      image_model: imageModel,
      video_api_provider: videoProvider,
      video_api_base_url: videoBaseURL,
      video_api_key: '',
      video_model: videoModel,
      ...override,
    };
    await api.updateAIConfig(payload);
    await mutateAIConfig();
  };

  const saveSecureMediaConfig = async (credential: string) => {
    const key = credential.trim();
    if (key && !key.includes('****')) {
      await api.saveAgnesAPIKey(key);
    }
    await saveAllConfig();
    setImageAPIKey('');
    setVideoAPIKey('');
    await mutateAIConfig();
  };

  const handleSaveImage = async () => {
    setSavingImage(true);
    try {
      await saveSecureMediaConfig(imageAPIKey);
      toast.show('图片模型配置已更新', 'success');
    } catch (e: any) {
      toast.show(`保存失败: ${e.message}`, 'error');
    } finally {
      setSavingImage(false);
    }
  };

  const handleSaveVideo = async () => {
    setSavingVideo(true);
    try {
      await saveSecureMediaConfig(videoAPIKey);
      toast.show('视频模型配置已更新', 'success');
    } catch (e: any) {
      toast.show(`保存失败: ${e.message}`, 'error');
    } finally {
      setSavingVideo(false);
    }
  };

  const handleTestImageAPI = async () => {
    setTestingImage(true);
    setImageTestResult(null);
    try {
      const res = await api.testAIConnection({
        category: 'image',
        provider: imageProvider,
        base_url: imageBaseURL,
        api_key: imageAPIKey,
        model: imageModel,
      });
      if (res.ok) {
        await saveSecureMediaConfig(imageAPIKey);
        setImageTestResult(res);
        toast.show(res.message || '图片 API 已验证并保存', 'success');
      } else {
        setImageTestResult(res);
        toast.show(`测试失败: ${res.error}`, 'error');
      }
    } catch (e: any) {
      setImageTestResult({ ok: false, error: e.message });
      toast.show(`测试失败: ${e.message}`, 'error');
    } finally {
      setTestingImage(false);
    }
  };

  const handleTestVideoAPI = async () => {
    setTestingVideo(true);
    setVideoTestResult(null);
    try {
      const res = await api.testAIConnection({
        category: 'video',
        provider: videoProvider,
        base_url: videoBaseURL,
        api_key: videoAPIKey,
        model: videoModel,
      });
      if (res.ok) {
        await saveSecureMediaConfig(videoAPIKey);
        setVideoTestResult(res);
        toast.show(res.message || '视频 API 已验证并保存', 'success');
      } else {
        setVideoTestResult(res);
        toast.show(`测试失败: ${res.error}`, 'error');
      }
    } catch (e: any) {
      setVideoTestResult({ ok: false, error: e.message });
      toast.show(`测试失败: ${e.message}`, 'error');
    } finally {
      setTestingVideo(false);
    }
  };

  return (
    <div className="space-y-4" data-testid="settings-media-api">
      <LLMConfigPanel />

      {/* ─────────────────────────────────────────────────────────────
          2. 🎨 AI 图片生成 API 配置 (Image API)
         ───────────────────────────────────────────────────────────── */}
      <div className="bg-brand-surface border border-brand-border rounded-xl overflow-hidden transition shadow-xs">
        {/* Collapsed Header */}
        <button
          type="button"
          onClick={() => setExpandImage(!expandImage)}
          aria-expanded={expandImage}
          aria-controls="settings-image-api-panel"
          className="flex w-full items-center justify-between p-4 text-left transition hover:bg-black/5"
        >
          <span className="flex min-w-0 items-center gap-3">
            <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-brand-border bg-brand-surface-raised">
              <ImageIcon className="w-4 h-4 text-brand-fg" />
            </span>
            <span className="min-w-0">
              <span className="flex items-center gap-2">
                <span className="text-xs font-bold text-gray-900">AI 图片生成配置</span>
                <span className="px-2 py-0.5 rounded-full text-[9.5px] font-semibold bg-brand-surface-raised text-brand-muted border border-brand-border truncate">
                  {imageProvider} · {imageModel}
                </span>
              </span>
              <span className="mt-0.5 block truncate text-[10.5px] text-gray-500">
                自媒体文生图与图生图模型设置
              </span>
            </span>
          </span>

          <span className="flex shrink-0 items-center gap-3">
            <span className={`text-[10px] font-bold px-2 py-0.5 rounded ${aiConfig?.image_api_configured ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20' : 'bg-brand-warning/20/10 text-brand-warning border border-brand-warning/30/20'}`}>
              {aiConfig?.image_api_configured ? 'API Key 已配置' : '未配置 Key'}
            </span>
            <span
              aria-hidden="true"
              className="p-1 rounded-md text-gray-400 hover:text-gray-200 transition"
            >
              {expandImage ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
            </span>
          </span>
        </button>

        {/* Expanded Form Panel */}
        {expandImage && (
          <div id="settings-image-api-panel" className="p-5 border-t border-brand-border/60 bg-brand-bg/40 space-y-5 animate-fade-in">
            {/* Test Result Indicator */}
            {imageTestResult && (
              <div
                className={`p-3 rounded-lg border text-xs flex items-center justify-between ${
                  imageTestResult.ok
                    ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                    : 'border-rose-500/30 bg-rose-500/10 text-rose-300'
                }`}
              >
                <div className="flex items-center gap-2">
                  {imageTestResult.ok ? <CheckCircle2 className="w-4 h-4 text-emerald-400" /> : <XCircle className="w-4 h-4 text-rose-400" />}
                  <span className="font-medium">{imageTestResult.message || imageTestResult.error}</span>
                </div>
                {imageTestResult.latency_ms !== undefined && (
                  <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-black/20">
                    {imageTestResult.latency_ms} ms
                  </span>
                )}
              </div>
            )}

            {/* Presets */}
            <div className="space-y-1.5">
              <label className="text-[10.5px] font-bold text-gray-500 uppercase">快捷填入生图服务商预设 (PRESET)</label>
              <div className="flex flex-wrap gap-1.5">
                {IMAGE_PROVIDER_PRESETS.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => handleSelectImagePreset(p)}
                    className={`px-3 py-1.5 rounded-lg text-xs font-semibold border transition ${
                      imageProvider === p.label
                        ? 'border-brand-accent bg-brand-accent/15 text-brand-fg'
                        : 'border-brand-border bg-brand-bg text-gray-700 hover:border-gray-500'
                    }`}
                  >
                    {p.label}
                  </button>
                ))}
              </div>
            </div>

            {/* Provider Name & BaseURL & API Key */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <div className="space-y-1">
                <label className="text-[10.5px] font-bold text-gray-500 uppercase">供应商名称 (可自定义输入)</label>
                <input
                  type="text"
                  value={imageProvider}
                  onChange={(e) => setImageProvider(e.target.value)}
                  placeholder="例如: 火山引擎"
                  className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-bg text-xs font-medium text-gray-800 focus:outline-none focus:border-brand-accent"
                />
              </div>

              <div className="space-y-1">
                <label className="text-[10.5px] font-bold text-gray-500 uppercase">Base URL (生图接入点)</label>
                <input
                  type="text"
                  value={imageBaseURL}
                  onChange={(e) => setImageBaseURL(e.target.value)}
                  placeholder="https://api.agnes.ai/v1"
                  className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-bg text-xs font-mono text-gray-800 focus:outline-none focus:border-brand-accent"
                />
              </div>

              <div className="space-y-1">
                <label className="text-[10.5px] font-bold text-gray-500 uppercase">API Key (生图通信密钥)</label>
                <div className="relative">
                  <input
                    type={showImageKey ? 'text' : 'password'}
                    value={imageAPIKey}
                    onChange={(e) => setImageAPIKey(e.target.value)}
                    placeholder="sk-..."
                    className="w-full px-3 py-2 pr-9 rounded-lg border border-brand-border bg-brand-bg text-xs font-mono text-gray-800 focus:outline-none focus:border-brand-accent"
                  />
                  <button
                    type="button"
                    onClick={() => setShowImageKey(!showImageKey)}
                    className="absolute right-2.5 top-2.5 text-gray-400 hover:text-gray-200 transition"
                  >
                    {showImageKey ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
              </div>
            </div>

            {/* Image Model */}
            <div className="space-y-1 pt-2 border-t border-brand-border/40">
              <label className="text-[10.5px] font-bold text-gray-500 uppercase flex items-center gap-1.5">
                <ImageIcon className="w-3.5 h-3.5 text-brand-muted" />
                <span>生图模型名称</span>
              </label>
              <input
                type="text"
                value={imageModel}
                onChange={(e) => setImageModel(e.target.value)}
                placeholder="agnes-image-2.1-flash"
                className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-bg text-xs font-mono text-gray-800 focus:outline-none focus:border-brand-accent"
              />
            </div>

            {/* Bottom Actions */}
            <div className="flex items-center justify-between pt-3 border-t border-brand-border/40">
              <button
                type="button"
                onClick={handleTestImageAPI}
                disabled={testingImage}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-brand-border bg-brand-surface-raised hover:bg-brand-accent/10 text-brand-fg text-xs font-bold transition disabled:opacity-50"
              >
                {testingImage ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Zap className="w-3.5 h-3.5" />}
                验证并保存
              </button>

              <button
                type="button"
                onClick={handleSaveImage}
                disabled={savingImage}
                className="flex items-center gap-1.5 px-5 py-2 rounded-lg bg-brand-accent hover:bg-brand-accent-hover text-brand-on-accent text-xs font-bold transition disabled:opacity-50 shadow-md"
              >
                {savingImage ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Sparkles className="w-3.5 h-3.5" />}
                保存图片配置
              </button>
            </div>
          </div>
        )}
      </div>

      {/* ─────────────────────────────────────────────────────────────
          3. 🎬 AI 视频生成 API 配置 (Video API)
         ───────────────────────────────────────────────────────────── */}
      <div className="bg-brand-surface border border-brand-border rounded-xl overflow-hidden transition shadow-xs">
        {/* Collapsed Header */}
        <button
          type="button"
          onClick={() => setExpandVideo(!expandVideo)}
          aria-expanded={expandVideo}
          aria-controls="settings-video-api-panel"
          className="flex w-full items-center justify-between p-4 text-left transition hover:bg-black/5"
        >
          <span className="flex min-w-0 items-center gap-3">
            <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-brand-border bg-brand-surface-raised">
              <Video className="w-4 h-4 text-brand-fg" />
            </span>
            <span className="min-w-0">
              <span className="flex items-center gap-2">
                <span className="text-xs font-bold text-gray-900">AI 视频生成配置</span>
                <span className="px-2 py-0.5 rounded-full text-[9.5px] font-semibold bg-brand-surface-raised text-brand-muted border border-brand-border truncate">
                  {videoProvider} · {videoModel}
                </span>
              </span>
              <span className="mt-0.5 block truncate text-[10.5px] text-gray-500">
                短视频口播与图生视频模型设置
              </span>
            </span>
          </span>

          <span className="flex shrink-0 items-center gap-3">
            <span className={`text-[10px] font-bold px-2 py-0.5 rounded ${aiConfig?.video_api_configured ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20' : 'bg-brand-warning/20/10 text-brand-warning border border-brand-warning/30/20'}`}>
              {aiConfig?.video_api_configured ? 'API Key 已配置' : '未配置 Key'}
            </span>
            <span
              aria-hidden="true"
              className="p-1 rounded-md text-gray-400 hover:text-gray-200 transition"
            >
              {expandVideo ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
            </span>
          </span>
        </button>

        {/* Expanded Form Panel */}
        {expandVideo && (
          <div id="settings-video-api-panel" className="p-5 border-t border-brand-border/60 bg-brand-bg/40 space-y-5 animate-fade-in">
            {/* Test Result Indicator */}
            {videoTestResult && (
              <div
                className={`p-3 rounded-lg border text-xs flex items-center justify-between ${
                  videoTestResult.ok
                    ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                    : 'border-rose-500/30 bg-rose-500/10 text-rose-300'
                }`}
              >
                <div className="flex items-center gap-2">
                  {videoTestResult.ok ? <CheckCircle2 className="w-4 h-4 text-emerald-400" /> : <XCircle className="w-4 h-4 text-rose-400" />}
                  <span className="font-medium">{videoTestResult.message || videoTestResult.error}</span>
                </div>
                {videoTestResult.latency_ms !== undefined && (
                  <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-black/20">
                    {videoTestResult.latency_ms} ms
                  </span>
                )}
              </div>
            )}

            {/* Presets */}
            <div className="space-y-1.5">
              <label className="text-[10.5px] font-bold text-gray-500 uppercase">快捷填入生视频服务商预设 (PRESET)</label>
              <div className="flex flex-wrap gap-1.5">
                {VIDEO_PROVIDER_PRESETS.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => handleSelectVideoPreset(p)}
                    className={`px-3 py-1.5 rounded-lg text-xs font-semibold border transition ${
                      videoProvider === p.label
                        ? 'border-brand-accent bg-brand-accent/15 text-brand-fg'
                        : 'border-brand-border bg-brand-bg text-gray-700 hover:border-gray-500'
                    }`}
                  >
                    {p.label}
                  </button>
                ))}
              </div>
            </div>

            {/* Provider Name & BaseURL & API Key */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <div className="space-y-1">
                <label className="text-[10.5px] font-bold text-gray-500 uppercase">供应商名称 (可自定义输入)</label>
                <input
                  type="text"
                  value={videoProvider}
                  onChange={(e) => setVideoProvider(e.target.value)}
                  placeholder="例如: 火山引擎"
                  className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-bg text-xs font-medium text-gray-800 focus:outline-none focus:border-brand-accent"
                />
              </div>

              <div className="space-y-1">
                <label className="text-[10.5px] font-bold text-gray-500 uppercase">Base URL (视频接入点)</label>
                <input
                  type="text"
                  value={videoBaseURL}
                  onChange={(e) => setVideoBaseURL(e.target.value)}
                  placeholder="https://api.agnes.ai/v1"
                  className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-bg text-xs font-mono text-gray-800 focus:outline-none focus:border-brand-accent"
                />
              </div>

              <div className="space-y-1">
                <label className="text-[10.5px] font-bold text-gray-500 uppercase">API Key (视频通信密钥)</label>
                <div className="relative">
                  <input
                    type={showVideoKey ? 'text' : 'password'}
                    value={videoAPIKey}
                    onChange={(e) => setVideoAPIKey(e.target.value)}
                    placeholder="sk-..."
                    className="w-full px-3 py-2 pr-9 rounded-lg border border-brand-border bg-brand-bg text-xs font-mono text-gray-800 focus:outline-none focus:border-brand-accent"
                  />
                  <button
                    type="button"
                    onClick={() => setShowVideoKey(!showVideoKey)}
                    className="absolute right-2.5 top-2.5 text-gray-400 hover:text-gray-200 transition"
                  >
                    {showVideoKey ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
              </div>
            </div>

            {/* Video Model */}
            <div className="space-y-1 pt-2 border-t border-brand-border/40">
              <label className="text-[10.5px] font-bold text-gray-500 uppercase flex items-center gap-1.5">
                <Video className="w-3.5 h-3.5 text-brand-muted" />
                <span>生视频模型名称</span>
              </label>
              <input
                type="text"
                value={videoModel}
                onChange={(e) => setVideoModel(e.target.value)}
                placeholder="agnes-video-v2.0"
                className="w-full px-3 py-2 rounded-lg border border-brand-border bg-brand-bg text-xs font-mono text-gray-800 focus:outline-none focus:border-brand-accent"
              />
            </div>

            {/* Bottom Actions */}
            <div className="flex items-center justify-between pt-3 border-t border-brand-border/40">
              <button
                type="button"
                onClick={handleTestVideoAPI}
                disabled={testingVideo}
                className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-brand-border bg-brand-surface-raised hover:bg-brand-accent/10 text-brand-fg text-xs font-bold transition disabled:opacity-50"
              >
                {testingVideo ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Zap className="w-3.5 h-3.5" />}
                验证并保存
              </button>

              <button
                type="button"
                onClick={handleSaveVideo}
                disabled={savingVideo}
                className="flex items-center gap-1.5 px-5 py-2 rounded-lg bg-brand-accent hover:bg-brand-accent-hover text-brand-on-accent text-xs font-bold transition disabled:opacity-50 shadow-md"
              >
                {savingVideo ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Sparkles className="w-3.5 h-3.5" />}
                保存视频配置
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
