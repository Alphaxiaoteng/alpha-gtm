import React, { useEffect, useState } from 'react';
import useSWR from 'swr';
import {
  CircleDashed,
  Download,
  FileJson,
  FolderOpen,
  History,
  LoaderCircle,
  Plus,
  Radar,
  Radio,
  RefreshCw,
  Search,
  ScrollText,
  Trash2,
  Upload,
  UserRoundSearch,
} from 'lucide-react';
import {
  api,
  type TrendRadarCreator,
  type TrendRadarNiche,
  type TrendRadarRSSSource,
  type TrendRadarRun,
  type TrendRadarSchedule,
} from '../api';
import { PlatformIcon } from './PlatformIcon';

const platformOptions = [
  { id: 'douyin', name: '抖音' },
  { id: 'xhs', name: '小红书' },
  { id: 'wechat_video', name: '视频号' },
  { id: 'kuaishou', name: '快手' },
  { id: 'wechat_mp', name: '公众号' },
  { id: 'youtube', name: 'YouTube' },
  { id: 'x', name: 'X' },
];

const platformNames: Record<string, string> = Object.fromEntries(
  platformOptions.map((platform) => [platform.id, platform.name]),
);

const stateLabels: Record<string, string> = {
  active: '监测中',
  idle: '待更新',
  paused: '已暂停',
  archived: '已归档',
  error: '异常',
};

function statusClass(state: string) {
  switch (state) {
    case 'active':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700';
    case 'paused':
      return 'border-brand-warning/30 bg-brand-warning/10 text-brand-warning';
    case 'archived':
      return 'border-black/10 bg-black/[0.035] text-gray-600';
    case 'error':
      return 'border-rose-200 bg-rose-50 text-rose-700';
    default:
      return 'border-brand-border bg-white text-gray-700';
  }
}

function formatTime(value?: string) {
  if (!value) return '未记录';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '时间未知' : date.toLocaleString('zh-CN', { hour12: false });
}

function formatDelta(value: number) {
  if (!value) return '0';
  return value > 0 ? `+${value.toLocaleString()}` : value.toLocaleString();
}

function compactNumber(value?: number) {
  if (!value) return '0';
  if (value >= 10000) return `${(value / 10000).toFixed(1)}w`;
  return value.toLocaleString();
}

// 与后端 Run.PendingRetryTerms 同一口径：按词取最后一次尝试的状态，
// 成功过的词永远不进重试清单，所以重试不会重复扣额度。
function pendingRunTerms(run: TrendRadarRun): string[] {
  const lastStatus = new Map<string, string>();
  for (const call of run.calls ?? []) lastStatus.set(call.term, call.status);
  return (run.terms ?? []).filter((term) => {
    const status = lastStatus.get(term);
    return status !== undefined && status !== 'succeeded';
  });
}

// 第几次尝试：同一搜索词在 calls 里按出现顺序编号，重试项一眼可见。
function callAttemptNumbers(run: TrendRadarRun): number[] {
  const seen = new Map<string, number>();
  return (run.calls ?? []).map((call) => {
    const next = (seen.get(call.term) ?? 0) + 1;
    seen.set(call.term, next);
    return next;
  });
}

const MiniStat: React.FC<{ label: string; value: React.ReactNode; compact?: boolean }> = ({ label, value, compact }) => (
  <div className={`rounded-lg border border-brand-border bg-white ${compact ? 'px-2 py-1.5' : 'px-3 py-2.5'} shadow-2xs`}>
    <div className="text-[10px] font-medium text-gray-500">{label}</div>
    <div className={`mt-0.5 ${compact ? 'text-xs' : 'text-base'} font-bold text-gray-900`}>{value}</div>
  </div>
);

const SectionHeader: React.FC<{ title: string; subtitle: string; action?: React.ReactNode }> = ({ title, subtitle, action }) => (
  <div className="flex items-center justify-between gap-3 border-b border-brand-border px-4 py-3">
    <div>
      <h2 className="text-xs font-bold text-gray-950">{title}</h2>
      <p className="mt-0.5 text-[10px] text-gray-500">{subtitle}</p>
    </div>
    {action}
  </div>
);

const EmptyState: React.FC<{
  icon: React.ElementType;
  title: string;
  message: string;
  actionLabel?: string;
  onAction?: () => void;
}> = ({ icon: Icon, title, message, actionLabel, onAction }) => (
  <div className="flex min-h-48 flex-col items-center justify-center p-8 text-center">
    <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-black/[0.035] text-gray-400">
      <Icon className="h-5 w-5" />
    </div>
    <h3 className="mt-3 text-xs font-bold text-gray-900">{title}</h3>
    <p className="mt-1 max-w-sm text-[10px] leading-relaxed text-gray-500">{message}</p>
    {actionLabel && onAction && (
      <button type="button" className="ui-btn ui-btn-primary mt-4" onClick={onAction}>
        <Plus className="h-3.5 w-3.5" />
        {actionLabel}
      </button>
    )}
  </div>
);

export const CrawlerView: React.FC = () => {
  const [activeTab, setActiveTab] = useState<'niches' | 'creators' | 'schedules' | 'rss' | 'videos' | 'runs'>('niches');
  const [runningNiche, setRunningNiche] = useState<string | null>(null);
  const [runTerms, setRunTerms] = useState<Record<string, string>>({});
  const [runPlatforms, setRunPlatforms] = useState<Record<string, string>>({});
  const [runFeedback, setRunFeedback] = useState<Record<string, TrendRadarRun>>({});
  const [limitDrafts, setLimitDrafts] = useState<Record<string, Record<string, number>>>({});
  const [savingLimits, setSavingLimits] = useState<string | null>(null);
  const [exporting, setExporting] = useState<string | null>(null);
  const [runs, setRuns] = useState<TrendRadarRun[] | null>(null);
  const [runsLoading, setRunsLoading] = useState(false);
  const [runsError, setRunsError] = useState('');
  const [openRunID, setOpenRunID] = useState<string | null>(null);
  const [retryingRunID, setRetryingRunID] = useState<string | null>(null);
  const [retryFeedback, setRetryFeedback] = useState<Record<string, string>>({});

  const loadRuns = async () => {
    setRunsLoading(true);
    try {
      const data = await api.getTrendRadarRuns({ limit: 30 });
      setRuns(data.runs ?? []);
      setRunsError('');
    } catch (error) {
      setRunsError(error instanceof Error ? error.message : '抓取记录读取失败');
    } finally {
      setRunsLoading(false);
    }
  };

  const retryFailedCalls = async (run: TrendRadarRun) => {
    const failedTerms = pendingRunTerms(run);
    if (!failedTerms.length) return;
    const confirmed = window.confirm(
      `重试【${run.niche_name || run.niche_id}】这一轮的失败项？\n失败搜索词：${failedTerms.join('、')}\n只会重跑失败项，成功项不会再调用（不重复扣额度）；重试仍按次计费。`,
    );
    if (!confirmed) return;
    setRetryingRunID(run.id);
    try {
      const result = await api.retryTrendRadarRun(run.id);
      setRuns((current) => (current ?? []).map((item) => (item.id === result.run.id ? result.run : item)));
      setRetryFeedback((current) => ({
        ...current,
        [run.id]: `已重试 ${result.retried_terms.length} 项：补齐 ${result.recovered_terms.length} 项；仍有 ${result.pending_terms.length} 项失败`,
      }));
      setOpenRunID(run.id);
    } catch (error) {
      setRetryFeedback((current) => ({
        ...current,
        [run.id]: error instanceof Error ? error.message : '重试失败',
      }));
    } finally {
      setRetryingRunID(null);
    }
  };

  useEffect(() => {
    if (activeTab === 'runs' && runs === null && !runsLoading) void loadRuns();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTab]);

  // Creator monitoring form
  const [addCreatorOpen, setAddCreatorOpen] = useState(false);
  const [creatorPlatform, setCreatorPlatform] = useState<'xhs' | 'douyin'>('xhs');
  const [creatorID, setCreatorID] = useState('');
  const [creatorName, setCreatorName] = useState('');
  const [creatorSaving, setCreatorSaving] = useState(false);
  const [syncingCreator, setSyncingCreator] = useState<string | null>(null);

  // RSS form
  const [rssName, setRSSName] = useState('');
  const [rssURL, setRSSURL] = useState('');
  const [rssFullText, setRSSFullText] = useState(false);
  const [rssSaving, setRssSaving] = useState<string | null>(null);

  // Video parsing
  const [videoUploading, setVideoUploading] = useState(false);

  // Schedules
  const [scheduleSaving, setScheduleSaving] = useState<string | null>(null);

  // SWR data - using trend radar summary for full state
  const { data: summary, mutate: mutateSummary } = useSWR('api/trend-radar/summary', api.getTrendRadarSummary, {
    revalidateOnFocus: false,
  });

  const { data: radarSchedules, mutate: mutateSchedules } = useSWR(
    'api/trend-radar/schedules',
    api.getTrendRadarSchedules,
    { revalidateOnFocus: false },
  );

  const { data: radarVideos, mutate: mutateVideos } = useSWR(
    'api/trend-radar/videos',
    api.getTrendRadarVideos,
    { revalidateOnFocus: false },
  );

  const { data: radarRSS, mutate: mutateRSS } = useSWR(
    'api/trend-radar/rss',
    api.getTrendRadarRSS,
    { revalidateOnFocus: false },
  );

  const openPathInFinder = async (path?: string) => {
    if (!path) return;
    if (window.electronAPI?.openPathInFinder) {
      await window.electronAPI.openPathInFinder(path);
      return;
    }
    window.alert(`本地路径：${path}`);
  };

  const platformCap = (niche: TrendRadarNiche, platform: string) =>
    niche.platform_limits?.[platform] ?? (platform === 'xhs' ? 10 : 30);

  const runNiche = async (niche: TrendRadarNiche) => {
    const selectedTerm = runTerms[niche.id] || niche.search_terms[0];
    const selectedPlatform = runPlatforms[niche.id] || niche.platforms[0];
    const terms = selectedTerm === '__all__' ? niche.search_terms : [selectedTerm];
    if (terms.length > 8) {
      window.alert('单轮最多 8 次真实调用，请减少搜索词或逐个执行。');
      return;
    }
    const cap = platformCap(niche, selectedPlatform);
    const ok = window.confirm(
      `确认抓取推荐池【${niche.name}】？\n平台：${platformNames[selectedPlatform] || selectedPlatform}\n搜索词：${terms.join('、')}\n共 ${terms.length} 次真实调用（每次都会计费），本平台每轮最多入库 ${cap} 条。\n原始 JSON 会保留在本地，结果可导出。`,
    );
    if (!ok) return;

    setRunningNiche(niche.id);
    try {
      const run = await api.runTrendRadarNiche(niche.id, {
        platform: selectedPlatform,
        terms: selectedTerm === '__all__' ? undefined : terms,
        all: selectedTerm === '__all__',
      });
      setRunFeedback((current) => ({ ...current, [niche.id]: run }));
      await mutateSummary();
    } catch (err) {
      window.alert(`抓取失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setRunningNiche(null);
    }
  };

  const saveLimits = async (niche: TrendRadarNiche) => {
    const draft = limitDrafts[niche.id] ?? niche.platform_limits ?? {};
    setSavingLimits(niche.id);
    try {
      await api.saveTrendRadarNicheLimits(niche.id, draft);
      await mutateSummary();
      setLimitDrafts((current) => {
        const next = { ...current };
        delete next[niche.id];
        return next;
      });
    } catch (err) {
      window.alert(`保存上限失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setSavingLimits(null);
    }
  };

  const downloadBlob = (blob: Blob, filename: string) => {
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = filename;
    link.click();
    URL.revokeObjectURL(url);
  };

  const exportNiche = async (niche?: TrendRadarNiche) => {
    const key = niche?.id ?? 'all';
    setExporting(key);
    try {
      const blob = await api.exportTrendRadar({ niche_id: niche?.id, format: 'csv' });
      downloadBlob(blob, `trend-radar-${niche?.id ?? 'all'}-${new Date().toISOString().slice(0, 10)}.csv`);
    } catch (err) {
      window.alert(`导出失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setExporting(null);
    }
  };

  const downloadRawCall = async (callID?: number) => {
    if (!callID) return;
    try {
      const raw = await api.getDataAPICallResponse(callID);
      downloadBlob(
        new Blob([JSON.stringify(raw, null, 2)], { type: 'application/json;charset=utf-8' }),
        `tikhub-call-${callID}.json`,
      );
    } catch (err) {
      window.alert(`读取原始结果失败: ${err instanceof Error ? err.message : String(err)}`);
    }
  };

  const addCreator = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!creatorID.trim()) return;
    setCreatorSaving(true);
    try {
      await api.addTrendRadarCreator({
        platform: creatorPlatform,
        user_id: creatorID.trim(),
        name: creatorName.trim() || creatorID.trim(),
      });
      setCreatorID('');
      setCreatorName('');
      setAddCreatorOpen(false);
      await mutateSummary();
    } catch (err) {
      window.alert(`添加博主失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setCreatorSaving(false);
    }
  };

  const refreshCreator = async (creator: TrendRadarCreator) => {
    const ok = window.confirm(
      `确认刷新博主【${creator.name}】（${platformNames[creator.platform]}）？\n将发起真实数据 API 获取主页资料与最新作品。`,
    );
    if (!ok) return;

    const key = `${creator.platform}-${creator.user_id}`;
    setSyncingCreator(key);
    try {
      // 每个平台只认自己的参数名：抖音是 sec_user_id，小红书是 user_id。
      // 同时传两个会被后端判成「不支持的参数」，刷新必失败。
      const endpoint = creator.platform === 'douyin' ? 'douyin.creator_profile' : 'xhs.creator_profile';
      const params = creator.platform === 'douyin'
        ? { sec_user_id: creator.user_id }
        : { user_id: creator.user_id };
      await api.callDataAPI(endpoint, params);
      await mutateSummary();
    } catch (err) {
      window.alert(`刷新博主失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setSyncingCreator(null);
    }
  };

  const removeCreator = async (creator: TrendRadarCreator) => {
    const ok = window.confirm(`确认删除博主【${creator.name}】监测？历史抓取数据将保留。`);
    if (!ok) return;
    try {
      await api.removeTrendRadarCreator(creator.platform, creator.user_id);
      await mutateSummary();
    } catch (err) {
      window.alert(`删除失败: ${err instanceof Error ? err.message : String(err)}`);
    }
  };

  const addRSSSource = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!rssURL.trim()) return;
    setRssSaving('new');
    try {
      await api.createTrendRadarRSSSource({
        name: rssName.trim() || rssURL.trim(),
        url: rssURL.trim(),
        fetch_full_text: rssFullText,
        enabled: true,
      });
      setRSSName('');
      setRSSURL('');
      setRSSFullText(false);
      await mutateRSS();
    } catch (err) {
      window.alert(`添加 RSS 源失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setRssSaving(null);
    }
  };

  const collectRSS = async (source: TrendRadarRSSSource) => {
    setRssSaving(source.id);
    try {
      await api.collectTrendRadarRSS(source.id);
      await mutateRSS();
    } catch (err) {
      window.alert(`抓取 RSS 失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setRssSaving(null);
    }
  };

  const removeRSSSource = async (source: TrendRadarRSSSource) => {
    const ok = window.confirm(`确认删除 RSS 源【${source.name}】？历史内容保留。`);
    if (!ok) return;
    setRssSaving(source.id);
    try {
      await api.deleteTrendRadarRSSSource(source.id);
      await mutateRSS();
    } catch (err) {
      window.alert(`删除 RSS 源失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setRssSaving(null);
    }
  };

  const toggleSchedule = async (schedule: TrendRadarSchedule) => {
    setScheduleSaving(schedule.id);
    try {
      const nextEnabled = !schedule.enabled;
      await api.updateTrendRadarSchedule(
        {
          ...schedule,
          enabled: nextEnabled,
        },
        nextEnabled,
      );
      await mutateSchedules();
    } catch (err) {
      window.alert(`更新定时任务失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setScheduleSaving(null);
    }
  };

  const removeSchedule = async (schedule: TrendRadarSchedule) => {
    const ok = window.confirm(`确认删除定时计划【${schedule.name}】？`);
    if (!ok) return;
    setScheduleSaving(schedule.id);
    try {
      await api.deleteTrendRadarSchedule(schedule.id);
      await mutateSchedules();
    } catch (err) {
      window.alert(`删除失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setScheduleSaving(null);
    }
  };

  const uploadRadarVideo = async (file?: File) => {
    if (!file) return;
    setVideoUploading(true);
    try {
      await api.uploadTrendRadarVideo(file);
      await mutateVideos();
    } catch (err) {
      window.alert(`上传拆解视频失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setVideoUploading(false);
    }
  };

  return (
    <div className="mx-auto w-full max-w-[1440px] space-y-5 pb-12 px-3 sm:px-6" data-testid="crawler-view">
      {/* 头部标题与统计概览 */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-brand-border pb-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-gray-950 flex items-center gap-2">
            <Radio className="h-5 w-5 text-brand-accent" />
            抓取与数据采集控制台
          </h1>
          <p className="mt-1 text-xs text-gray-500">
            自定义赛道采集、对标博主监控、RSS 订阅全文抓取与本地视频拆解；所有抓取均需手动触发或明确启用定时器。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            className="ui-btn"
            onClick={() => {
              void mutateSummary();
              void mutateSchedules();
              void mutateVideos();
              void mutateRSS();
            }}
          >
            <RefreshCw className="h-3.5 w-3.5" />
            刷新状态
          </button>
        </div>
      </div>

      {/* 选项卡导航 */}
      <div className="flex border-b border-brand-border">
        <button
          type="button"
          onClick={() => setActiveTab('niches')}
          className={`flex items-center gap-2 border-b-2 px-4 py-2.5 text-xs font-semibold transition ${
            activeTab === 'niches'
              ? 'border-brand-accent text-brand-accent'
              : 'border-transparent text-gray-500 hover:text-gray-900'
          }`}
        >
          <Radar className="h-4 w-4" />
          赛道采集 ({(summary?.niches ?? []).filter((n) => n.state !== 'archived').length})
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('creators')}
          className={`flex items-center gap-2 border-b-2 px-4 py-2.5 text-xs font-semibold transition ${
            activeTab === 'creators'
              ? 'border-brand-accent text-brand-accent'
              : 'border-transparent text-gray-500 hover:text-gray-900'
          }`}
        >
          <UserRoundSearch className="h-4 w-4" />
          博主监控 ({(summary?.creators ?? []).length})
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('rss')}
          className={`flex items-center gap-2 border-b-2 px-4 py-2.5 text-xs font-semibold transition ${
            activeTab === 'rss'
              ? 'border-brand-accent text-brand-accent'
              : 'border-transparent text-gray-500 hover:text-gray-900'
          }`}
        >
          <Radio className="h-4 w-4" />
          RSS 源 ({(radarRSS?.sources ?? []).length})
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('schedules')}
          className={`flex items-center gap-2 border-b-2 px-4 py-2.5 text-xs font-semibold transition ${
            activeTab === 'schedules'
              ? 'border-brand-accent text-brand-accent'
              : 'border-transparent text-gray-500 hover:text-gray-900'
          }`}
        >
          <History className="h-4 w-4" />
          定时任务 ({(radarSchedules?.schedules ?? []).length})
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('videos')}
          className={`flex items-center gap-2 border-b-2 px-4 py-2.5 text-xs font-semibold transition ${
            activeTab === 'videos'
              ? 'border-brand-accent text-brand-accent'
              : 'border-transparent text-gray-500 hover:text-gray-900'
          }`}
        >
          <Upload className="h-4 w-4" />
          本地视频拆解 ({(radarVideos?.videos ?? []).length})
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('runs')}
          className={`flex items-center gap-2 border-b-2 px-4 py-2.5 text-xs font-semibold transition ${
            activeTab === 'runs'
              ? 'border-brand-accent text-brand-accent'
              : 'border-transparent text-gray-500 hover:text-gray-900'
          }`}
        >
          <ScrollText className="h-4 w-4" />
          抓取记录
        </button>
      </div>

      {/* 赛道抓取面板 */}
      {activeTab === 'niches' && (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500">
            <span>赛道由名称、搜索词与监测平台组成。保存配置不会自动联网，点击“抓取”进行受控执行。</span>
            <button
              type="button"
              className="ui-btn whitespace-nowrap"
              disabled={exporting === 'all'}
              onClick={() => void exportNiche()}
            >
              {exporting === 'all' ? (
                <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Download className="h-3.5 w-3.5" />
              )}
              导出全部 CSV
            </button>
          </div>
          <div className="grid gap-4 lg:grid-cols-2">
            {(summary?.niches ?? []).filter((niche) => niche.state !== 'archived').length === 0 ? (
              <section className="ui-card lg:col-span-2">
                <EmptyState
                  icon={Radar}
                  title="还没有配置赛道"
                  message="添加自定义赛道后，可指定关键词和监测平台进行一键抓取。"
                />
              </section>
            ) : (
              (summary?.niches ?? [])
                .filter((niche) => niche.state !== 'archived')
                .map((niche) => (
                  <article key={niche.id} className="ui-card overflow-hidden">
                    <div className="border-b border-brand-border p-4">
                      <div className="flex items-start justify-between gap-3">
                        <div>
                          <div className="flex items-center gap-2">
                            <h2 className="text-sm font-bold text-gray-950">{niche.name}</h2>
                            <span className={`rounded-full border px-2 py-0.5 text-[9px] font-semibold ${statusClass(niche.state)}`}>
                              {stateLabels[niche.state] || niche.state}
                            </span>
                          </div>
                          <p className="mt-1 text-[11px] text-gray-500">{niche.description}</p>
                        </div>
                        <Radar className="h-5 w-5 text-brand-accent" />
                      </div>
                    </div>
                    <div className="space-y-4 p-4">
                      <div className="grid grid-cols-3 gap-2">
                        <MiniStat label="已存信号" value={niche.result_count} />
                        <MiniStat label="搜索批次" value={niche.search_runs} />
                        <MiniStat label="最近监测" value={formatTime(niche.last_synced_at)} compact />
                      </div>
                      <div className="rounded-lg border border-brand-border bg-black/[0.018] p-3">
                        <div className="flex items-center justify-between">
                          <div className="text-[9px] font-bold uppercase tracking-[0.12em] text-gray-400">每轮条数上限</div>
                          <button
                            type="button"
                            className="text-[10px] font-semibold text-brand-accent hover:underline disabled:opacity-50"
                            disabled={savingLimits === niche.id}
                            onClick={() => void saveLimits(niche)}
                          >
                            {savingLimits === niche.id ? '保存中…' : '保存上限'}
                          </button>
                        </div>
                        <div className="mt-2 flex flex-wrap gap-2">
                          {niche.platforms.map((platform) => {
                            const draft = limitDrafts[niche.id] ?? niche.platform_limits ?? {};
                            const value = draft[platform] ?? platformCap(niche, platform);
                            return (
                              <label key={platform} className="flex items-center gap-1.5 text-[10px] text-gray-600">
                                <span>{platformNames[platform] ?? platform}</span>
                                <input
                                  type="number"
                                  min={1}
                                  max={100}
                                  value={value}
                                  aria-label={`${niche.name}${platformNames[platform] ?? platform}每轮上限`}
                                  onChange={(event) => {
                                    const next = Math.max(1, Math.min(100, Number(event.target.value) || 1));
                                    setLimitDrafts((current) => ({
                                      ...current,
                                      [niche.id]: { ...(current[niche.id] ?? niche.platform_limits ?? {}), [platform]: next },
                                    }));
                                  }}
                                  className="w-14 rounded-md border border-brand-border bg-white px-1.5 py-1 text-center text-[10px]"
                                />
                              </label>
                            );
                          })}
                        </div>
                      </div>
                      <div>
                        <div className="mb-2 text-[9px] font-bold uppercase tracking-[0.12em] text-gray-400">常用搜索词</div>
                        <div className="flex flex-wrap gap-1.5">
                          {niche.search_terms.map((term) => (
                            <span
                              key={term}
                              className="rounded-full border border-brand-border bg-black/[0.025] px-2.5 py-1 text-[10px] font-semibold text-gray-700"
                            >
                              {term}
                            </span>
                          ))}
                        </div>
                      </div>
                      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap">
                        <select
                          value={runTerms[niche.id] || niche.search_terms[0]}
                          onChange={(event) => setRunTerms((current) => ({ ...current, [niche.id]: event.target.value }))}
                          className="min-w-36 rounded-lg border border-brand-border bg-white px-3 py-2 text-xs"
                          aria-label={`${niche.name}搜索词`}
                        >
                          {niche.search_terms.map((term) => (
                            <option key={term} value={term}>
                              {term}
                            </option>
                          ))}
                          {niche.search_terms.length > 1 && (
                            <option value="__all__" disabled={niche.search_terms.length > 8}>
                              全部（{niche.search_terms.length} 次调用{niche.search_terms.length > 8 ? '，超过单轮 8 次上限' : ''}）
                            </option>
                          )}
                        </select>
                        <select
                          value={runPlatforms[niche.id] || niche.platforms[0]}
                          onChange={(event) => setRunPlatforms((current) => ({ ...current, [niche.id]: event.target.value }))}
                          className="min-w-32 rounded-lg border border-brand-border bg-white px-3 py-2 text-xs"
                        >
                          {niche.platforms.map((platform) => (
                            <option key={platform} value={platform}>
                              {platformOptions.find((item) => item.id === platform)?.name ?? platform}
                            </option>
                          ))}
                        </select>
                        <button
                          type="button"
                          className="ui-btn ui-btn-primary flex-1 whitespace-nowrap"
                          disabled={!niche.enabled || runningNiche === niche.id}
                          onClick={() => void runNiche(niche)}
                        >
                          {runningNiche === niche.id ? (
                            <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <Search className="h-3.5 w-3.5" />
                          )}
                          执行抓取
                        </button>
                        <button
                          type="button"
                          className="ui-btn whitespace-nowrap"
                          disabled={exporting === niche.id}
                          onClick={() => void exportNiche(niche)}
                        >
                          {exporting === niche.id ? (
                            <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                          ) : (
                            <Download className="h-3.5 w-3.5" />
                          )}
                          导出 CSV
                        </button>
                      </div>
                      {(() => {
                        const freshRun = runFeedback[niche.id];
                        const run =
                          freshRun && (!niche.last_run || freshRun.started_at >= niche.last_run.started_at)
                            ? freshRun
                            : niche.last_run;
                        if (!run) {
                          return (
                            <p className="text-[10px] leading-relaxed text-gray-400">
                              还没有显式抓取记录。点“执行抓取”会真实调用接口并计费，跑完在这里显示本轮调用数与失败原因。
                            </p>
                          );
                        }
                        const failedCall = run.calls?.find((call) => call.status !== 'succeeded');
                        const tone =
                          run.state === 'succeeded'
                            ? 'border-emerald-200 bg-emerald-50 text-emerald-800'
                            : run.state === 'running'
                              ? 'border-brand-border bg-white text-gray-700'
                              : 'border-rose-200 bg-rose-50 text-rose-800';
                        return (
                          <div className={`rounded-lg border p-3 text-[10px] leading-relaxed ${tone}`}>
                            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 font-semibold">
                              <span>
                                最近一轮：{run.state === 'succeeded' ? '成功' : run.state === 'partial' ? '部分成功' : run.state === 'failed' ? '失败' : '进行中'}
                              </span>
                              <span>调用 {run.total_calls}/{run.terms.length} 次</span>
                              <span>入库 {run.kept} 条</span>
                              <span>
                                {run.estimated_cost_usd > 0
                                  ? `预估 $${run.estimated_cost_usd.toFixed(2)}`
                                  : '单价未登记'}
                              </span>
                              <span className="text-gray-500">{formatTime(run.started_at)}</span>
                            </div>
                            {run.cost_basis && <div className="mt-1 text-gray-500">{run.cost_basis}</div>}
                            {failedCall && (
                              <div className="mt-1">
                                失败原因：{failedCall.error_code ?? 'unknown'}
                                {failedCall.error_message ? ` · ${failedCall.error_message}` : ''}
                              </div>
                            )}
                            {run.failed > 0 && (
                              <div className="mt-1">共 {run.failed} 次调用失败，后续搜索词已停止，避免继续扣额度。</div>
                            )}
                            <div className="mt-2 flex flex-wrap items-center gap-2">
                              {run.calls
                                ?.filter((call) => call.call_id)
                                .slice(-3)
                                .map((call) => (
                                  <button
                                    key={`${run.id}-${call.call_id}`}
                                    type="button"
                                    className="inline-flex items-center gap-1 rounded-md border border-current/20 px-2 py-1 text-[9px] font-semibold hover:underline"
                                    onClick={() => void downloadRawCall(call.call_id)}
                                  >
                                    <FileJson className="h-3 w-3" />
                                    原始结果 #{call.call_id}
                                  </button>
                                ))}
                            </div>
                          </div>
                        );
                      })()}
                    </div>
                  </article>
                ))
            )}
          </div>
        </div>
      )}

      {/* 博主监控面板 */}
      {activeTab === 'creators' && (
        <section className="ui-card overflow-hidden">
          <SectionHeader
            title="博主监控"
            subtitle="添加名单不会联网；刷新只会拉取主页资料，不含作品列表，且会先弹出显式确认"
            action={
              <button type="button" className="ui-btn ui-btn-primary" onClick={() => setAddCreatorOpen((open) => !open)}>
                <Plus className="h-3.5 w-3.5" />
                添加监控博主
              </button>
            }
          />
          {addCreatorOpen && (
            <form
              onSubmit={addCreator}
              className="grid gap-3 border-b border-brand-border bg-black/[0.018] p-4 lg:grid-cols-[160px_minmax(0,1fr)_minmax(0,1fr)_auto] lg:items-end"
            >
              <label className="block">
                <span className="mb-1 block text-[10px] font-semibold text-gray-600">平台</span>
                <select
                  value={creatorPlatform}
                  onChange={(event) => setCreatorPlatform(event.target.value as 'xhs' | 'douyin')}
                  className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs"
                >
                  <option value="xhs">小红书</option>
                  <option value="douyin">抖音</option>
                </select>
              </label>
              <label className="block">
                <span className="mb-1 block text-xs font-semibold text-gray-600">主页链接或 ID</span>
                <input
                  value={creatorID}
                  onChange={(event) => setCreatorID(event.target.value)}
                  placeholder={creatorPlatform === 'xhs' ? '粘贴主页链接或 user_id' : '粘贴主页链接或 sec_user_id'}
                  className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs outline-none focus:border-brand-accent"
                />
              </label>
              <label className="block">
                <span className="mb-1 block text-[10px] font-semibold text-gray-600">识别名称</span>
                <input
                  value={creatorName}
                  onChange={(event) => setCreatorName(event.target.value)}
                  placeholder="例如：AI 效率研究所"
                  className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs outline-none focus:border-brand-accent"
                />
              </label>
              <button type="submit" className="ui-btn ui-btn-primary" disabled={creatorSaving}>
                {creatorSaving ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
                加入名单
              </button>
            </form>
          )}
          {!(summary?.creators ?? []).length ? (
            <EmptyState
              icon={UserRoundSearch}
              title="还没有监控博主"
              message="先加入抖音或小红书博主，再明确点击“刷新快照”。"
            />
          ) : (
            <div className="grid gap-3 p-4 md:grid-cols-2 2xl:grid-cols-3">
              {(summary?.creators ?? []).map((creator: TrendRadarCreator) => {
                const key = `${creator.platform}-${creator.user_id}`;
                const syncing = syncingCreator === key;
                return (
                  <article key={key} className="rounded-xl border border-brand-border bg-white p-4">
                    <div className="flex items-start gap-3">
                      <PlatformIcon platform={creator.platform} className="h-9 w-9 shrink-0" />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-start justify-between gap-2">
                          <div className="min-w-0">
                            <h3 className="truncate text-xs font-bold text-gray-950">{creator.name}</h3>
                            <p className="mt-0.5 truncate text-[10px] text-gray-500">ID {creator.user_id}</p>
                          </div>
                          <span className={`rounded-full border px-2 py-0.5 text-[9px] font-semibold ${statusClass(creator.state)}`}>
                            {stateLabels[creator.state] || creator.state}
                          </span>
                        </div>
                        <p className="mt-2 truncate font-mono text-[9px] text-gray-400">{creator.folder_path}</p>
                      </div>
                    </div>
                    <div className="mt-3 grid grid-cols-4 gap-2 rounded-lg bg-black/[0.025] p-2 text-center">
                      <MiniStat label="粉丝" value={compactNumber(creator.followers)} compact />
                      <MiniStat label="粉丝增量" value={formatDelta(creator.followers_delta)} compact />
                      <MiniStat label="新增作品" value={creator.new_posts} compact />
                      <MiniStat label="互动增量" value={formatDelta(creator.engagement_delta)} compact />
                    </div>
                    <div className="mt-3 flex items-center justify-between gap-2 border-t border-brand-border/60 pt-3">
                      <button
                        type="button"
                        className="text-xs text-gray-500 hover:text-gray-900 inline-flex items-center gap-1"
                        onClick={() => void openPathInFinder(creator.folder_path)}
                      >
                        <FolderOpen className="h-3.5 w-3.5" />
                        本地目录
                      </button>
                      <div className="flex items-center gap-2">
                        <button
                          type="button"
                          className="ui-btn"
                          disabled={syncing}
                          onClick={() => void refreshCreator(creator)}
                        >
                          {syncing ? <LoaderCircle className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />}
                          刷新快照
                        </button>
                        <button
                          type="button"
                          className="ui-btn text-rose-600"
                          onClick={() => void removeCreator(creator)}
                          aria-label={`删除博主 ${creator.name}`}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    </div>
                  </article>
                );
              })}
            </div>
          )}
        </section>
      )}

      {/* RSS 采集面板 */}
      {activeTab === 'rss' && (
        <section className="ui-card overflow-hidden">
          <SectionHeader
            title="RSS 收集与网页全文"
            subtitle="每个源单独决定是否继续抓网页全文；添加或修改源不会自动抓取"
          />
          <form
            onSubmit={addRSSSource}
            className="grid gap-3 border-b border-brand-border bg-black/[0.018] p-4 md:grid-cols-[180px_minmax(0,1fr)_auto_auto] md:items-end"
          >
            <label>
              <span className="mb-1 block text-xs font-semibold text-gray-600">名称</span>
              <input
                value={rssName}
                onChange={(event) => setRSSName(event.target.value)}
                required
                maxLength={80}
                placeholder="例如：少数派"
                className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs"
              />
            </label>
            <label>
              <span className="mb-1 block text-xs font-semibold text-gray-600">公开 HTTPS RSS 地址</span>
              <input
                value={rssURL}
                onChange={(event) => setRSSURL(event.target.value)}
                required
                type="url"
                placeholder="https://example.com/feed.xml"
                className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs"
              />
            </label>
            <label className="flex h-9 items-center gap-2 rounded-lg border border-brand-border bg-white px-3 text-xs font-medium text-gray-700">
              <input
                type="checkbox"
                checked={rssFullText}
                onChange={(event) => setRSSFullText(event.target.checked)}
              />
              抓网页全文
            </label>
            <button type="submit" className="ui-btn ui-btn-primary" disabled={rssSaving === 'new'}>
              {rssSaving === 'new' ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
              添加源
            </button>
          </form>
          {!(radarRSS?.sources ?? []).length ? (
            <EmptyState icon={CircleDashed} title="还没有 RSS 源" message="添加源只做地址安全校验；需要你再点一次“抓取”。" />
          ) : (
            <div className="divide-y divide-brand-border/70">
              {radarRSS?.sources.map((source) => {
                const lastRun = radarRSS.runs.find((run) => run.source_id === source.id);
                return (
                  <article key={source.id} className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="text-xs font-bold text-gray-950">{source.name}</h3>
                        <span
                          className={`rounded-full px-2 py-0.5 text-[10px] font-semibold ${
                            source.fetch_full_text ? 'bg-brand-surface-raised text-brand-fg' : 'bg-black/[0.04] text-gray-600'
                          }`}
                        >
                          {source.fetch_full_text ? 'RSS＋网页全文' : '仅 RSS'}
                        </span>
                        {!source.enabled && <span className="text-[10px] text-gray-400">已停用</span>}
                      </div>
                      <p className="mt-1 truncate text-xs text-gray-500">{source.url}</p>
                      {lastRun && (
                        <p className="mt-1 text-xs text-gray-400">
                          最近 {formatTime(lastRun.created_at)} · {lastRun.item_count} 条 · 全文 {lastRun.full_text_saved} 篇 · {lastRun.duration_ms} ms
                        </p>
                      )}
                    </div>
                    <div className="flex shrink-0 flex-wrap gap-2">
                      <button
                        type="button"
                        className="ui-btn"
                        disabled={rssSaving === source.id}
                        onClick={() => {
                          void api
                            .updateTrendRadarRSSSource({ ...source, fetch_full_text: !source.fetch_full_text })
                            .then(() => mutateRSS());
                        }}
                      >
                        {source.fetch_full_text ? '只存 RSS' : '抓网页全文'}
                      </button>
                      <button
                        type="button"
                        className="ui-btn"
                        disabled={rssSaving === source.id}
                        onClick={() => {
                          void api
                            .updateTrendRadarRSSSource({ ...source, enabled: !source.enabled })
                            .then(() => mutateRSS());
                        }}
                      >
                        {source.enabled ? '停用' : '启用'}
                      </button>
                      <button
                        type="button"
                        className="ui-btn ui-btn-primary"
                        disabled={!source.enabled || rssSaving === source.id}
                        onClick={() => void collectRSS(source)}
                      >
                        {rssSaving === source.id ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                        立即抓取
                      </button>
                      <button
                        type="button"
                        className="ui-btn text-rose-600"
                        disabled={rssSaving === source.id}
                        onClick={() => void removeRSSSource(source)}
                        aria-label={`删除 RSS 源 ${source.name}`}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  </article>
                );
              })}
            </div>
          )}
        </section>
      )}

      {/* 定时抓取计划面板 */}
      {activeTab === 'schedules' && (
        <section className="ui-card overflow-hidden">
          <SectionHeader
            title="定时抓取任务"
            subtitle="后台定时执行；默认停用，启用时确认频率，不会静默持续调用"
          />
          {!(radarSchedules?.schedules ?? []).length ? (
            <EmptyState icon={CircleDashed} title="还没有定时抓取计划" message="在赛道或搜索中保存计划后，将显示在此处。" />
          ) : (
            <div className="divide-y divide-brand-border/70">
              {radarSchedules?.schedules.map((schedule) => (
                <article key={schedule.id} className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="text-xs font-bold text-gray-950">{schedule.name}</h3>
                      <span
                        className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${
                          schedule.enabled
                            ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                            : 'border-black/10 bg-black/[0.035] text-gray-600'
                        }`}
                      >
                        {schedule.enabled ? '运行中' : '已停用'}
                      </span>
                    </div>
                    <p className="mt-1 text-xs text-gray-500">
                      抖音 · {String(schedule.params.keyword || '')} · 每 {schedule.interval_minutes / 60} 小时 · 下次{' '}
                      {schedule.next_run_at ? formatTime(schedule.next_run_at) : '启用后计算'}
                    </p>
                    {schedule.last_call_id ? (
                      <p className="mt-1 text-xs text-gray-400">
                        上次调用 #{schedule.last_call_id} · {formatTime(schedule.last_run_at)} ·{' '}
                        {schedule.last_status === 'succeeded' ? '成功' : '失败'}
                      </p>
                    ) : null}
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <button
                      type="button"
                      className={`ui-btn ${schedule.enabled ? '' : 'ui-btn-primary'}`}
                      onClick={() => void toggleSchedule(schedule)}
                      disabled={scheduleSaving === schedule.id}
                    >
                      {scheduleSaving === schedule.id ? (
                        <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                      ) : (
                        <RefreshCw className="h-3.5 w-3.5" />
                      )}
                      {schedule.enabled ? '停用' : '启用'}
                    </button>
                    <button
                      type="button"
                      className="ui-btn text-rose-600"
                      onClick={() => void removeSchedule(schedule)}
                      disabled={scheduleSaving === schedule.id}
                      aria-label={`删除定时计划 ${schedule.name}`}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                </article>
              ))}
            </div>
          )}
        </section>
      )}

      {/* 视频拆解面板 */}
      {activeTab === 'videos' && (
        <section className="ui-card overflow-hidden">
          <SectionHeader
            title="本地视频拆解"
            subtitle="视频原件、封面与 16 kHz 单声道音频全部写入本地目录；支持平台字幕与本地 Whisper"
            action={
              <label
                className={`ui-btn ui-btn-primary cursor-pointer ${videoUploading ? 'pointer-events-none opacity-60' : ''}`}
              >
                {videoUploading ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
                选择视频
                <input
                  type="file"
                  accept="video/*"
                  className="sr-only"
                  disabled={videoUploading}
                  onChange={(event) => {
                    void uploadRadarVideo(event.target.files?.[0]);
                    event.currentTarget.value = '';
                  }}
                />
              </label>
            }
          />
          {!(radarVideos?.videos ?? []).length ? (
            <EmptyState
              icon={CircleDashed}
              title="还没有视频拆解记录"
              message="选择一条本地视频后，会保留原件并生成封面和本地 Whisper 音频；不会上传云端。"
            />
          ) : (
            <div className="grid gap-3 p-4 md:grid-cols-2">
              {radarVideos?.videos.map((video) => (
                <article key={video.id} className="flex gap-3 rounded-xl border border-brand-border bg-white p-3">
                  <div className="h-28 w-20 shrink-0 overflow-hidden rounded-lg bg-black/[0.04]">
                    {video.cover_url ? (
                      <img src={video.cover_url} alt={`${video.original_name}封面`} className="h-full w-full object-cover" />
                    ) : (
                      <div className="flex h-full items-center justify-center text-xs text-gray-400">无封面</div>
                    )}
                  </div>
                  <div className="min-w-0 flex-1">
                    <h3 className="truncate text-xs font-bold text-gray-950">{video.original_name}</h3>
                    <p className="mt-1 text-xs text-gray-500">
                      {(video.duration_ms / 1000).toFixed(1)} 秒 · {(video.size_bytes / 1024 / 1024).toFixed(1)} MB
                    </p>
                    <p className="mt-2 text-xs leading-relaxed text-brand-warning">{video.transcription_note}</p>
                    <dl className="mt-2 space-y-1 text-[11px] text-gray-500">
                      <div className="truncate">原件：{video.source_path}</div>
                      <div className="truncate">音频：{video.audio_path || '生成失败，原件仍保留'}</div>
                    </dl>
                  </div>
                </article>
              ))}
            </div>
          )}
        </section>
      )}

      {/* 抓取记录面板 */}
      {activeTab === 'runs' && (
        <section className="ui-card overflow-hidden">
          <SectionHeader
            title="抓取记录"
            subtitle="每次真实抓取属于哪个池、调了几次、抓回多少、哪条失败；记录只存本地"
            action={
              <button type="button" className="ui-btn" onClick={() => void loadRuns()} disabled={runsLoading}>
                {runsLoading ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                刷新
              </button>
            }
          />
          {runsError ? (
            <div className="px-4 py-6 text-xs text-brand-warning">{runsError}</div>
          ) : runsLoading && runs === null ? (
            <div className="flex items-center gap-2 px-4 py-6 text-xs text-gray-500">
              <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> 正在读取抓取记录
            </div>
          ) : !(runs ?? []).length ? (
            <EmptyState
              icon={CircleDashed}
              title="还没有抓取记录"
              message="在赛道采集里点一次「立即抓取」后，这里会出现这次抓取的调用明细。"
            />
          ) : (
            <div className="divide-y divide-brand-border/70">
              {(runs ?? []).map((run) => {
                const expanded = openRunID === run.id;
                const retriedCalls = Math.max(0, run.total_calls - (run.succeeded + run.failed));
                const attempts = callAttemptNumbers(run);
                return (
                  <article key={run.id} className="px-4 py-3">
                    <div className="flex flex-wrap items-center gap-2">
                      <h3 className="text-xs font-bold text-gray-950">{run.niche_name || run.niche_id}</h3>
                      <span className="rounded-full border border-black/10 bg-black/[0.035] px-2 py-0.5 text-[10px] font-semibold text-gray-600">
                        {platformNames[run.platform] || run.platform}
                      </span>
                      <span
                        className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${
                          run.state === 'succeeded'
                            ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                            : run.state === 'partial'
                              ? 'border-amber-200 bg-amber-50 text-amber-700'
                              : run.state === 'running'
                                ? 'border-sky-200 bg-sky-50 text-sky-700'
                                : 'border-rose-200 bg-rose-50 text-rose-700'
                        }`}
                      >
                        {run.state === 'succeeded'
                          ? '成功'
                          : run.state === 'partial'
                            ? '部分成功'
                            : run.state === 'running'
                              ? '进行中'
                              : '失败'}
                      </span>
                      <span className="text-[11px] text-gray-400">{formatTime(run.started_at)}</span>
                      {run.state !== 'running' && run.failed > 0 ? (
                        <button
                          type="button"
                          className="ui-btn ml-auto"
                          data-testid={`retry-run-${run.id}`}
                          disabled={retryingRunID === run.id}
                          onClick={() => void retryFailedCalls(run)}
                        >
                          {retryingRunID === run.id ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                          重试失败项
                        </button>
                      ) : null}
                      <button
                        type="button"
                        className={`ui-btn ${run.state !== 'running' && run.failed > 0 ? '' : 'ml-auto'}`}
                        onClick={() => setOpenRunID(expanded ? null : run.id)}
                      >
                        {expanded ? '收起明细' : '调用明细'}
                      </button>
                    </div>
                    <p className="mt-1 text-xs text-gray-500">
                      搜索词 {run.terms.join('、') || '—'} · 调用 {run.total_calls} 次
                      {retriedCalls > 0 ? `（含重试 ${retriedCalls} 次）` : ''} · 结果 成功 {run.succeeded} / 失败 {run.failed}
                      {run.skipped > 0 ? ` / 未执行 ${run.skipped}` : ''} · 抓回 {run.items} 条、留 {run.kept} 条 · 广告卡{' '}
                      {run.ad_cards ?? 0} 条、引导卡 {run.guide_cards ?? 0} 条、读不到 {run.unreadable ?? 0} 条 · 约 $
                      {run.estimated_cost_usd.toFixed(2)}
                      {run.last_retry_at ? ` · 重试于 ${formatTime(run.last_retry_at)}` : ''}
                    </p>
                    {retryFeedback[run.id] ? (
                      <p className="mt-1 text-[11px] text-gray-600" data-testid={`retry-feedback-${run.id}`}>
                        {retryFeedback[run.id]}
                      </p>
                    ) : null}
                    {expanded ? (
                      <div className="mt-3 space-y-2">
                        <p className="text-[10px] text-gray-400">批次（导出「批次」列）：{run.id}</p>
                        <div className="overflow-x-auto rounded-lg border border-brand-border">
                          <table className="w-full min-w-[720px] text-left text-[11px]">
                            <thead className="bg-black/[0.02] text-gray-500">
                              <tr>
                                <th className="px-2 py-1.5 font-semibold">搜索词</th>
                                <th className="px-2 py-1.5 font-semibold">尝试</th>
                                <th className="px-2 py-1.5 font-semibold">接口</th>
                                <th className="px-2 py-1.5 font-semibold">状态</th>
                                <th className="px-2 py-1.5 font-semibold">抓回</th>
                                <th className="px-2 py-1.5 font-semibold">保留</th>
                                <th className="px-2 py-1.5 font-semibold">广告卡</th>
                                <th className="px-2 py-1.5 font-semibold">引导卡</th>
                                <th className="px-2 py-1.5 font-semibold">读不到</th>
                                <th className="px-2 py-1.5 font-semibold">耗时</th>
                                <th className="px-2 py-1.5 font-semibold">失败原因</th>
                              </tr>
                            </thead>
                            <tbody className="divide-y divide-brand-border/70">
                              {run.calls.map((call, index) => (
                                <tr key={`${run.id}-${index}`}>
                                  <td className="px-2 py-1.5 text-gray-700">{call.term || '—'}</td>
                                  <td className="px-2 py-1.5 text-gray-500">第 {attempts[index] ?? 1} 次</td>
                                  <td className="px-2 py-1.5 text-gray-500">{call.endpoint_id}</td>
                                  <td className={`px-2 py-1.5 font-semibold ${call.status === 'succeeded' ? 'text-emerald-700' : 'text-rose-600'}`}>
                                    {call.status === 'succeeded' ? '成功' : '失败'}
                                  </td>
                                  <td className="px-2 py-1.5 text-gray-700">{call.item_count}</td>
                                  <td className="px-2 py-1.5 text-gray-700">{call.kept_count}</td>
                                  {/* 失败的调用没读到任何条目，三个卡计数显示「—」而不是 0。 */}
                                  <td className="px-2 py-1.5 text-gray-700">
                                    {call.status === 'succeeded' ? (call.ad_count ?? 0) : '—'}
                                  </td>
                                  <td className="px-2 py-1.5 text-gray-700">
                                    {call.status === 'succeeded' ? (call.guide_count ?? 0) : '—'}
                                  </td>
                                  <td className="px-2 py-1.5 text-gray-700">
                                    {call.status === 'succeeded' ? (call.unreadable_count ?? 0) : '—'}
                                  </td>
                                  <td className="px-2 py-1.5 text-gray-500">{call.duration_ms} ms</td>
                                  <td className="px-2 py-1.5 text-rose-600">
                                    {call.status === 'succeeded' ? '—' : call.error_message || call.error_code || '未知原因'}
                                  </td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        </div>
                      </div>
                    ) : null}
                  </article>
                );
              })}
            </div>
          )}
        </section>
      )}
    </div>
  );
};
