import React, { useEffect, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import useSWR from 'swr';
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  CircleDashed,
  Clock,
  Copy,
  Database,
  ExternalLink,
  Eye,
  Film,
  Folder,
  FolderOpen,
  Heart,
  List,
  Lightbulb,
  LoaderCircle,
  Pause,
  Play,
  Plus,
  Radar,
  RefreshCw,
  Save,
  Search,
  Share2,
  Settings2,
  Sparkles,
  Trash2,
  UserRoundSearch,
  Volume2,
  X,
} from 'lucide-react';
import {
  api,
  type DataAPIEndpoint,
  type TikHubAPICall,
  type TrendRadarArchiveFailure,
  type TrendRadarConfig,
  type TrendRadarConfigNiche,
  type TrendRadarCreator,
  type TrendRadarCreatorPost,
  type TrendRadarRSSSource,
  type TrendRadarSchedule,
  type TrendRadarSignal,
} from '../api';
import { useUIStore } from '../store';
import AddCreatorDialog from './AddCreatorDialog';
import { LocalLoadingState } from './LocalLoadingState';
import { PlatformIcon } from './PlatformIcon';
import ArchiveFailuresPanel from './trend/ArchiveFailuresPanel';
import { CreatorComparePanel } from './trend/CreatorComparePanel';
import CreatorPostGrid from './trend/CreatorPostGrid';
import PostDetailModal from './trend/PostDetailModal';
import { useCreatorArchive } from './trend/useCreatorArchive';
import { compactNumber, formatAudioTime, formatTime } from './trend/format';

type RadarSection = 'content' | 'collect' | 'logs';

// 一屏渲染几百张卡片会让首帧明显变慢；先给一屏，再按需追加。
const SIGNAL_PAGE_SIZE = 60;
type RadarContentView = 'keywords' | 'videos' | 'creators';
type KeywordLayout = 'folders' | 'list';
type SignalSort = 'latest' | 'recurring' | 'engagement';
type VideoLookupStage = 'idle' | 'parsing' | 'fetching' | 'analyzing';
type DouyinSearchOptions = {
  publish_time: string;
  sort_type: string;
  filter_duration: string;
  content_type: string;
};

const defaultDouyinSearchOptions: DouyinSearchOptions = {
  publish_time: '7',
  sort_type: '2',
  filter_duration: '0',
  content_type: '0',
};

const sections: Array<{ id: RadarSection; label: string; icon: React.ElementType }> = [
  { id: 'content', label: '雷达内容', icon: Activity },
  { id: 'collect', label: '抓取', icon: Search },
  { id: 'logs', label: '日志', icon: Database },
];

const platformOptions = [
  { id: 'douyin', name: '抖音' },
  { id: 'xhs', name: '小红书' },
  { id: 'wechat_video', name: '视频号' },
  { id: 'kuaishou', name: '快手' },
  { id: 'wechat_mp', name: '公众号' },
  { id: 'youtube', name: 'YouTube' },
  { id: 'x', name: 'X' },
];

const searchEndpointByPlatform: Record<string, string> = {
  douyin: 'douyin.video_search',
  xhs: 'xhs.note_search',
  kuaishou: 'kuaishou.search',
  wechat_video: 'wechat_video.keyword_search',
  wechat_mp: 'wechat_mp.keyword_search',
  youtube: 'youtube.video_search',
  x: 'x.keyword_search',
};

const platformNames: Record<string, string> = Object.fromEntries(
  platformOptions.map((platform) => [platform.id, platform.name]),
);

const creatorEndpoints: Record<string, { profile: string; posts: string; parameter: string }> = {
  xhs: { profile: 'xhs.creator_profile', posts: 'xhs.creator_posts', parameter: 'user_id' },
  douyin: { profile: 'douyin.creator_profile', posts: 'douyin.creator_posts', parameter: 'sec_user_id' },
};

const stateLabels: Record<string, string> = {
  ready: '已监测',
  partial: '部分可用',
  empty: '暂无记录',
  not_synced: '未监测',
  stale: '待刷新',
  never_synced: '未监测',
  failed: '读取失败',
  disabled: '已停用',
};

function statusClass(state: string): string {
  if (state === 'ready') return 'border-emerald-200 bg-emerald-50 text-emerald-700';
  if (state === 'failed' || state === 'partial') return 'border-rose-200 bg-rose-50 text-rose-700';
  if (state === 'stale') return 'border-brand-accent/40 bg-brand-surface-raised text-brand-accent';
  return 'border-black/10 bg-black/[0.035] text-brand-fg-subtle';
}

function formatDelta(value: number): string {
  if (!Number.isFinite(value) || value === 0) return '0';
  return `${value > 0 ? '+' : ''}${value.toLocaleString('zh-CN')}`;
}

function cloneConfig(config: TrendRadarConfig): TrendRadarConfig {
  return {
    ...config,
    niches: config.niches.map((niche) => ({
      ...niche,
      search_terms: [...niche.search_terms],
      platforms: [...niche.platforms],
    })),
  };
}

function parseStoredParameters(value: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

function recordValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function findDeepValue(value: unknown, keys: Set<string>, depth = 0): unknown {
  if (depth > 8) return undefined;
  const record = recordValue(value);
  if (record) {
    for (const [key, child] of Object.entries(record)) {
      if (keys.has(key)) return child;
    }
    for (const child of Object.values(record)) {
      const found = findDeepValue(child, keys, depth + 1);
      if (found !== undefined) return found;
    }
  } else if (Array.isArray(value)) {
    for (const child of value) {
      const found = findDeepValue(child, keys, depth + 1);
      if (found !== undefined) return found;
    }
  }
  return undefined;
}

function collectPublishedTimes(value: unknown, output: number[] = [], depth = 0): number[] {
  if (depth > 10) return output;
  const record = recordValue(value);
  if (record) {
    for (const [key, child] of Object.entries(record)) {
      if (key === 'create_time') {
        const numeric = Number(child);
        if (Number.isFinite(numeric) && numeric > 0) output.push(numeric > 1_000_000_000_000 ? numeric / 1000 : numeric);
      } else {
        collectPublishedTimes(child, output, depth + 1);
      }
    }
  } else if (Array.isArray(value)) {
    for (const child of value) collectPublishedTimes(child, output, depth + 1);
  }
  return output;
}

function douyinPagingState(snapshot: Record<string, unknown>, cutoffSeconds: number) {
  const response = recordValue(snapshot.response) ?? snapshot;
  const nextValue = findDeepValue(response, new Set(['max_cursor']));
  const hasMoreValue = findDeepValue(response, new Set(['has_more']));
  const nextCursor = Number(nextValue);
  const hasMore = hasMoreValue === undefined || hasMoreValue === true || hasMoreValue === 1 || hasMoreValue === '1';
  const publishedTimes = collectPublishedTimes(response);
  return {
    nextCursor: Number.isSafeInteger(nextCursor) && nextCursor > 0 ? nextCursor : 0,
    hasMore,
    reachedCutoff: publishedTimes.some((timestamp) => timestamp < cutoffSeconds),
  };
}

export const DashboardView: React.FC = () => {
  const { toast, setAgentDraft, openAgentDrawer, setHeaderExtras, radarContentView, setRadarContentView, selectedCreatorKey, setSelectedCreatorKey } = useUIStore();
  const [section, setSection] = useState<RadarSection>('content');
  const contentView = radarContentView;
  const setContentView = setRadarContentView;
  // 博主监控下的两个子视图：列表（文件夹）与对比（多博主曲线）。
  const [creatorCompareView, setCreatorCompareView] = useState(false);
  const [keywordLayout, setKeywordLayout] = useState<KeywordLayout>('list');
  const [timeRange, setTimeRange] = useState<'7' | '30' | 'all'>('all');
  const [contentQuery, setContentQuery] = useState('');
  const [videoLookupInput, setVideoLookupInput] = useState('');
  const [videoLookupStage, setVideoLookupStage] = useState<VideoLookupStage>('idle');
  const [editedConfig, setEditedConfig] = useState<TrendRadarConfig | null>(null);
  const [configSaving, setConfigSaving] = useState(false);
  const [newTerms, setNewTerms] = useState<Record<string, string>>({});
  const [runPlatforms, setRunPlatforms] = useState<Record<string, string>>({});
  const [runTerms, setRunTerms] = useState<Record<string, string>>({});
  const [runningNiche, setRunningNiche] = useState('');
  const [runningDue, setRunningDue] = useState(false);
  const [addCreatorOpen, setAddCreatorOpen] = useState(false);
  const [creatorConfigSaving, setCreatorConfigSaving] = useState('');
  const [runningCreator, setRunningCreator] = useState('');
  const [removingCreator, setRemovingCreator] = useState('');
  const [savingSignal, setSavingSignal] = useState('');
  const [coverBackfilling, setCoverBackfilling] = useState(false);
  const [selectedSignal, setSelectedSignal] = useState<TrendRadarSignal | null>(null);
  const [analyzingVideoCallID, setAnalyzingVideoCallID] = useState(0);
  const [signalPlatform, setSignalPlatform] = useState('all');
  const [signalNiche, setSignalNiche] = useState('all');
  const [signalTerm, setSignalTerm] = useState('all');
  // 默认按最新排序：趋势雷达的价值在于「今天在火什么」，
  // 高互动老数据会把新抓到的信号压到后面。
  const [signalSort, setSignalSort] = useState<SignalSort>('latest');
  const [visibleSignalLimit, setVisibleSignalLimit] = useState(SIGNAL_PAGE_SIZE);
  const [runOptions, setRunOptions] = useState<Record<string, DouyinSearchOptions>>({});
  const [rawCalls, setRawCalls] = useState<Record<number, string>>({});
  const [loadingRawCall, setLoadingRawCall] = useState(0);
  const [addingSignalCreator, setAddingSignalCreator] = useState('');
  const [activeAudio, setActiveAudio] = useState<{ url: string; title: string; id: string } | null>(null);
  const [audioPlaying, setAudioPlaying] = useState(false);
  const [audioTime, setAudioTime] = useState(0);
  const [audioDuration, setAudioDuration] = useState(0);
  const audioRef = React.useRef<HTMLAudioElement | null>(null);
  const [creatorPostQuery, setCreatorPostQuery] = useState('');
  const [creatorPostMediaFilter, setCreatorPostMediaFilter] = useState<'all' | 'with_audio'>('all');
  const [creatorPostSort, setCreatorPostSort] = useState<'newest' | 'likes'>('newest');
  const [selectedPostDetail, setSelectedPostDetail] = useState<TrendRadarCreatorPost | null>(null);
  const creatorSearchRef = React.useRef<HTMLInputElement | null>(null);
  const [unassignedSelection, setUnassignedSelection] = useState<string[]>([]);
  const [assignTarget, setAssignTarget] = useState('');
  const [assignNewNicheName, setAssignNewNicheName] = useState('');
  const [assignNewNicheDescription, setAssignNewNicheDescription] = useState('');
  const [assigningKeywords, setAssigningKeywords] = useState(false);

  const toggleAudio = (post: { id: string; title: string; audio_path?: string }, platform: string, userId: string) => {
    if (!post.audio_path) return;
    const audioUrl = api.getTrendRadarCreatorMediaURL(platform, userId, post.audio_path);
    const audio = audioRef.current;
    if (activeAudio?.id === post.id) {
      if (audioPlaying) {
        audio?.pause();
        setAudioPlaying(false);
      } else {
        audio?.play().then(() => setAudioPlaying(true)).catch(() => {});
      }
    } else {
      // 切歌：先把进度归零，再换源；避免上一首的进度闪现在新歌上。
      setAudioTime(0);
      setAudioDuration(0);
      setActiveAudio({ url: audioUrl, title: post.title, id: post.id });
      if (audio) {
        audio.src = audioUrl;
        audio.load();
        audio.play().then(() => setAudioPlaying(true)).catch(() => {});
      }
    }
  };

  // 拖动/点击进度条：把百分比映射回秒。duration 未知时忽略，避免乱跳。
  const seekAudio = (fraction: number) => {
    const audio = audioRef.current;
    if (!audio || !Number.isFinite(audio.duration) || audio.duration <= 0) return;
    const clamped = Math.min(1, Math.max(0, fraction));
    audio.currentTime = clamped * audio.duration;
    setAudioTime(audio.currentTime);
  };

  const handleSeekPointer = (event: React.PointerEvent<HTMLDivElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    if (rect.width <= 0) return;
    seekAudio((event.clientX - rect.left) / rect.width);
  };

  const handleOpenFolder = async (creator: TrendRadarCreator) => {
    if (window.electronAPI?.openPathInFinder && selectedCreator) {
      void window.electronAPI.openPathInFinder(selectedCreator.folder_path);
      return;
    }
    try {
      await api.openTrendRadarCreatorFolder(creator.platform, creator.user_id);
      toast.show('已在访达中定位博主文件夹', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '无法打开本地文件夹', 'error');
    }
  };

  const handleCopyPath = async (path: string) => {
    try {
      await navigator.clipboard.writeText(path);
      toast.show('已复制文件夹本地路径', 'success');
    } catch {
      toast.show(path, 'info');
    }
  };
  const [scheduleSaving, setScheduleSaving] = useState('');
  const [videoUploading, setVideoUploading] = useState(false);
  const [rssName, setRSSName] = useState('');
  const [rssURL, setRSSURL] = useState('');
  const [rssFullText, setRSSFullText] = useState(false);
  const [rssSaving, setRSSSaving] = useState('');
  const videoLookupSearching = videoLookupStage !== 'idle';

  const {
    data,
    error,
    isLoading,
    isValidating,
    mutate: mutateSummary,
  } = useSWR('api/trend-radar/summary', api.getTrendRadarSummary, {
    revalidateOnFocus: false,
    revalidateOnReconnect: false,
    refreshInterval: 0,
  });
  const {
    data: config,
    mutate: mutateConfig,
  } = useSWR('api/trend-radar/config', api.getTrendRadarConfig, {
    revalidateOnFocus: false,
    revalidateOnReconnect: false,
  });
  const { data: provider } = useSWR('api/data-api/config', api.getDataAPIConfig);
  const {
    data: callHistory,
    error: callHistoryError,
    isLoading: callHistoryLoading,
    mutate: mutateCallHistory,
  } = useSWR(section === 'logs' ? 'api/data-api/calls' : null, api.getDataAPICalls, {
    revalidateOnFocus: false,
    revalidateOnReconnect: false,
  });
  const {
    data: scheduleFile,
    mutate: mutateSchedules,
  } = useSWR(section === 'collect' || section === 'content' ? 'api/trend-radar/schedules' : null, api.getTrendRadarSchedules, {
    revalidateOnFocus: false,
    revalidateOnReconnect: false,
  });
  const {
    data: radarVideos,
    mutate: mutateRadarVideos,
  } = useSWR(section === 'collect' ? 'api/trend-radar/videos' : null, api.getTrendRadarVideos, {
    revalidateOnFocus: false,
    revalidateOnReconnect: false,
  });
  const {
    data: radarRSS,
    mutate: mutateRadarRSS,
  } = useSWR(section === 'collect' || section === 'logs' ? 'api/trend-radar/rss' : null, api.getTrendRadarRSS, {
    revalidateOnFocus: false,
    revalidateOnReconnect: false,
  });

  useEffect(() => {
    if (config) setEditedConfig(cloneConfig(config));
  }, [config]);

  useEffect(() => {
    if (!selectedSignal) return undefined;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setSelectedSignal(null);
    };
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [selectedSignal]);

  useEffect(() => {
    if (!data?.niches) return;
    const configurableNiches = data.niches.filter((niche) => niche.state !== 'archived');
    setRunPlatforms((current) => {
      const next = { ...current };
      for (const niche of configurableNiches) {
        if (!next[niche.id] || !niche.platforms.includes(next[niche.id])) {
          next[niche.id] = niche.platforms.includes('douyin') ? 'douyin' : niche.platforms[0];
        }
      }
      return next;
    });
    setRunTerms((current) => {
      const next = { ...current };
      for (const niche of configurableNiches) {
        if (next[niche.id] !== '__all__' && !niche.search_terms.includes(next[niche.id])) {
          next[niche.id] = niche.search_terms[0] ?? '';
        }
      }
      return next;
    });
  }, [data?.niches]);

  const signalStats = useMemo(() => {
    const signals = data?.signals ?? [];
    return {
      total: signals.length,
      fresh: signals.filter((signal) => signal.is_new).length,
      recurring: signals.filter((signal) => signal.occurrences > 1).length,
      withCover: signals.filter((signal) => signal.cover_url).length,
    };
  }, [data?.signals]);

  // 自动检测并补全缺失封面：进入页面发现有无封面信号时静默触发一次本地补全
  useEffect(() => {
    if (!data?.signals?.length) return;
    const missingCover = data.signals.some((s) => !s.cover_url);
    if (missingCover && !coverBackfilling) {
      setCoverBackfilling(true);
      api.backfillTrendRadarCovers()
        .then(() => mutateSummary())
        .catch(() => {})
        .finally(() => setCoverBackfilling(false));
    }
  }, [data?.signals?.length]);

  const platformCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const signal of data?.signals ?? []) {
      counts[signal.platform] = (counts[signal.platform] ?? 0) + 1;
    }
    return counts;
  }, [data?.signals]);

  const nicheCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const signal of data?.signals ?? []) {
      if (signalPlatform !== 'all' && signal.platform !== signalPlatform) continue;
      counts[signal.niche_id] = (counts[signal.niche_id] ?? 0) + 1;
    }
    return counts;
  }, [data?.signals, signalPlatform]);

  const archivedNiche = useMemo(
    () => (data?.niches ?? []).find((niche) => niche.state === 'archived'),
    [data?.niches],
  );
  const unassignedTerms = archivedNiche?.search_terms ?? [];
  const assignableNiches = useMemo(
    () => (data?.niches ?? []).filter((niche) => niche.state !== 'archived' && niche.enabled),
    [data?.niches],
  );
  const selectedUnassignedTerms = unassignedSelection.filter((term) => unassignedTerms.includes(term));
  const effectiveAssignTarget = assignTarget || assignableNiches[0]?.id || '__new__';
  const creatingAssignNiche = effectiveAssignTarget === '__new__';

  const termCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const signal of data?.signals ?? []) {
      if (signalPlatform !== 'all' && signal.platform !== signalPlatform) continue;
      if (signalNiche !== 'all' && signal.niche_id !== signalNiche) continue;
      counts[signal.search_term] = (counts[signal.search_term] ?? 0) + 1;
    }
    return counts;
  }, [data?.signals, signalNiche, signalPlatform]);

  const visibleSignals = useMemo(() => {
    const query = contentQuery.trim().toLocaleLowerCase('zh-CN');
    const nowMs = Date.now();
    const cutoffMs = timeRange === '7' ? 7 * 24 * 60 * 60 * 1000 : timeRange === '30' ? 30 * 24 * 60 * 60 * 1000 : Infinity;
    const signals = (data?.signals ?? []).filter((signal) => {
      if (signalPlatform !== 'all' && signal.platform !== signalPlatform) return false;
      if (signalNiche !== 'all' && signal.niche_id !== signalNiche) return false;
      if (signalTerm !== 'all' && signal.search_term !== signalTerm) return false;
      if (cutoffMs !== Infinity) {
        // 近 7/30 天按「发布时间」筛；平台没给发布时间时才退回抓取时间，
        // 避免把「今天抓到但半年前发布」的内容算成近期。
        const reference = signal.published_at || signal.observed_at;
        const referenceMs = Date.parse(reference);
        if (!Number.isNaN(referenceMs) && nowMs - referenceMs > cutoffMs) return false;
      }
      if (query && ![
        signal.title, signal.content_text, signal.creator, signal.search_term,
        platformNames[signal.platform], signal.niche_name,
      ].some((value) => value?.toLocaleLowerCase('zh-CN').includes(query))) return false;
      return true;
    });
    const platformMaxEngagement = new Map<string, number>();
    for (const signal of signals) {
      const total = signal.likes + signal.collects + signal.shares;
      platformMaxEngagement.set(signal.platform, Math.max(platformMaxEngagement.get(signal.platform) ?? 0, total));
    }
    return [...signals].sort((left, right) => {
      if (signalSort === 'recurring') {
        return right.occurrences - left.occurrences || Date.parse(right.observed_at) - Date.parse(left.observed_at);
      }
      if (signalSort === 'engagement') {
        // 结合高赞、高收藏、高分享与转评比的爆款指数模型
        const leftTotal = left.likes * 1.5 + left.collects * 3.0 + left.shares * 3.5;
        const rightTotal = right.likes * 1.5 + right.collects * 3.0 + right.shares * 3.5;
        return rightTotal - leftTotal || right.likes - left.likes || Date.parse(right.observed_at) - Date.parse(left.observed_at);
      }
      return Date.parse(right.observed_at) - Date.parse(left.observed_at);
    });
  }, [contentQuery, data?.signals, signalNiche, signalPlatform, signalSort, signalTerm, timeRange]);

  // 上次成功抓取时间，用于「立即抓取」按钮的悬停提示
  const lastSyncedLabel = data?.last_synced_at
    ? `上次抓取：${formatTime(data.last_synced_at)}`
    : '本地还没有抓取记录';

  // 已启用的定时任务数量：0 时说明后台不会自动抓，需要引导用户去开启
  const enabledSchedules = (scheduleFile?.schedules ?? []).filter((item) => item.enabled);
  const enabledScheduleCount = enabledSchedules.length;

  // 最近一次到期时间，让用户知道后台何时会自动抓取
  const nextRunLabel = (() => {
    const stamps = enabledSchedules
      .map((item) => item.next_run_at)
      .filter((value): value is string => Boolean(value))
      .map((value) => Date.parse(value))
      .filter((value) => Number.isFinite(value))
      .sort((left, right) => left - right);
    if (!stamps.length) return '';
    return formatTime(new Date(stamps[0]).toISOString());
  })();

  // 立即执行一次到期的趋势雷达抓取任务，跑完后刷新本地信号。
  const runDueNow = async () => {
    if (runningDue) return;
    setRunningDue(true);
    try {
      const summary = await api.runTrendRadarDue();
      await mutateSummary();
      await mutateCallHistory();
      if (summary.due === 0) {
        toast.show(
          enabledScheduleCount > 0
            ? `定时任务未到执行时间（已启用 ${enabledScheduleCount} 个，后台每 6 小时检查）`
            : '还没有启用的定时计划，点顶部「未启用定时」可前往开启',
          'info',
        );
      } else {
        toast.show(`抓取完成：成功 ${summary.succeeded} / ${summary.due}${summary.failed ? `，失败 ${summary.failed}` : ''}`, summary.failed ? 'error' : 'success');
      }
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '抓取失败', 'error');
    } finally {
      setRunningDue(false);
    }
  };

  // 一键 AI 分析：把当前筛选的信号摘要交给 Alpha Agent（harness），
  // 复用已验证的模型接入，不另建分析 API。
  const runAIAnalysis = () => {
    if (!visibleSignals.length) {
      toast.show('当前筛选没有信号，先调整筛选或执行一次抓取。', 'error');
      return;
    }
    const nicheName = (data?.niches ?? []).find((niche) => niche.id === signalNiche)?.name;
    const scopeLabel = [
      signalPlatform === 'all' ? '全部平台' : platformNames[signalPlatform] || signalPlatform,
      signalNiche === 'all' ? null : nicheName || signalNiche,
      signalTerm === 'all' ? null : signalTerm,
    ].filter(Boolean).join(' / ');
    const digest = visibleSignals.slice(0, 20).map((signal, index) => {
      const metrics = [
        signal.play ? `播放${signal.play}` : '',
        signal.likes ? `点赞${signal.likes}` : '',
        signal.collects ? `收藏${signal.collects}` : '',
        signal.shares ? `分享${signal.shares}` : '',
      ].filter(Boolean).join(' ');
      const excerpt = (signal.content_text || '').replace(/\s+/g, ' ').slice(0, 120);
      return `${index + 1}. [${platformNames[signal.platform] || signal.platform}·${signal.niche_name}·${signal.search_term}] ${signal.title}${signal.creator ? ` — ${signal.creator}` : ''}${metrics ? ` · ${metrics}` : ''} · 发布${signal.published_at ? formatTime(signal.published_at) : '读不到'} · 抓取${formatTime(signal.observed_at)}${excerpt ? `｜${excerpt}` : ''}`;
    }).join('\n');
    setAgentDraft({
      mode: 'tools',
      message: `请基于以下 ${Math.min(visibleSignals.length, 20)} 条趋势雷达信号做选题分析（筛选范围：${scopeLabel}，当前共 ${visibleSignals.length} 条）：\n1) 给出 3 个最值得做的选题方向，各附依据并引用信号编号；\n2) 归纳共性规律与内容结构；\n3) 指出同质化风险与差异化空间。\n信号数据：\n${digest}`,
    });
    openAgentDrawer();
    toast.show('已把当前筛选信号交给 Alpha Agent 分析。', 'success');
  };

  const keywordFolders = useMemo(() => {
    const query = contentQuery.trim().toLocaleLowerCase('zh-CN');
    const folders: Record<string, { count: number; latest: string }> = {};
    for (const signal of data?.signals ?? []) {
      if (signalPlatform !== 'all' && signal.platform !== signalPlatform) continue;
      if (signalNiche !== 'all' && signal.niche_id !== signalNiche) continue;
      if (query && ![
        signal.title, signal.content_text, signal.creator, signal.search_term,
        platformNames[signal.platform], signal.niche_name,
      ].some((value) => value?.toLocaleLowerCase('zh-CN').includes(query))) continue;
      const current = folders[signal.search_term] ?? { count: 0, latest: '' };
      current.count += 1;
      if (signal.observed_at > current.latest) current.latest = signal.observed_at;
      folders[signal.search_term] = current;
    }
    return Object.entries(folders)
      .map(([term, folder]) => ({ term, ...folder }))
      .sort((left, right) => right.latest.localeCompare(left.latest) || left.term.localeCompare(right.term, 'zh-CN'));
  }, [contentQuery, data?.signals, signalNiche, signalPlatform]);

  const selectedCreator = useMemo(() => (data?.creators ?? []).find(
    (creator) => `${creator.platform}-${creator.user_id}` === selectedCreatorKey,
  ) ?? null, [data?.creators, selectedCreatorKey]);

  // 全量归档的状态、1 秒轮询与卸载清理都封装在 hook 里，编排层只消费结果。
  // activeCreator 让 hook 在进入/切换博主时接上后端仍在跑的归档（刷新页面不丢进度）。
  const { archiveCreator, archiveProgress, archivingCreator, archiveDone } = useCreatorArchive({
    providerActive: Boolean(provider?.active),
    mutateSummary,
    mutateCallHistory,
    activeCreator: selectedCreator,
  });

  // 博主级「自动更新」开关：后端每 24 小时增量归档一次，默认关闭。
  // 老数据没有 auto_archive 字段，按关闭处理。
  const [autoArchiveEnabled, setAutoArchiveEnabled] = useState(false);
  const [autoArchiveSaving, setAutoArchiveSaving] = useState(false);
  const selectedCreatorAutoArchive = Boolean(selectedCreator?.auto_archive);

  // summary 刷新后以服务端值为准；切换博主也要同步成新博主的开关状态。
  useEffect(() => {
    setAutoArchiveEnabled(selectedCreatorAutoArchive);
    setAutoArchiveSaving(false);
  }, [selectedCreatorKey, selectedCreatorAutoArchive]);

  // 自动更新开关：先乐观切换，失败必须回滚，不能留下一个假的开态。
  const toggleCreatorAutoArchive = async (creator: TrendRadarCreator) => {
    if (autoArchiveSaving) return;
    const previous = autoArchiveEnabled;
    const next = !previous;
    setAutoArchiveEnabled(next);
    setAutoArchiveSaving(true);
    try {
      const result = await api.setTrendRadarCreatorAutoArchive(creator.platform, creator.user_id, next);
      setAutoArchiveEnabled(Boolean(result.enabled));
      // 刷新 summary 让开关状态落到博主数据里；summary 失败不回滚已生效的服务端状态。
      void mutateSummary();
      toast.show(
        result.enabled ? '已开启自动更新：每 24 小时增量归档一次，会消耗 TikHub 额度' : '已关闭自动更新',
        'success',
      );
    } catch (cause) {
      setAutoArchiveEnabled(previous);
      toast.show(cause instanceof Error ? cause.message : '自动更新设置失败，已恢复原状态', 'error');
    } finally {
      setAutoArchiveSaving(false);
    }
  };

  // 归档失败台账：上次媒体下载失败的封面/音频（后端落盘在 index.json 旁），
  // 进程重启后仍在。进入文件夹拉一次；归档结束后再拉一次，因为失败项可能变了。
  const [archiveFailures, setArchiveFailures] = useState<TrendRadarArchiveFailure[]>([]);
  const [archiveFailureTotal, setArchiveFailureTotal] = useState(0);
  const [archiveFailuresOpen, setArchiveFailuresOpen] = useState(false);
  const archiveSettledRef = React.useRef('');
  const selectedCreatorPlatform = selectedCreator?.platform ?? '';
  const selectedCreatorUserID = selectedCreator?.user_id ?? '';

  const loadArchiveFailures = React.useCallback(async (platform: string, userID: string) => {
    try {
      const ledger = await api.getTrendRadarCreatorArchiveFailures(platform, userID);
      setArchiveFailures(Array.isArray(ledger.failures) ? ledger.failures : []);
      setArchiveFailureTotal(ledger.total || 0);
      return ledger;
    } catch {
      // 台账缺失/损坏对用户就是「没有失败项」，不弹错误打断浏览。
      setArchiveFailures([]);
      setArchiveFailureTotal(0);
      return null;
    }
  }, []);

  useEffect(() => {
    if (!selectedCreatorPlatform || !selectedCreatorUserID) {
      setArchiveFailures([]);
      setArchiveFailureTotal(0);
      setArchiveFailuresOpen(false);
      return;
    }
    void loadArchiveFailures(selectedCreatorPlatform, selectedCreatorUserID);
  }, [selectedCreatorPlatform, selectedCreatorUserID, loadArchiveFailures]);

  // 归档结束（done / error / idle 都会清空 archivingCreator）后重拉台账：
  // 失败项可能被本次重试补上，也可能新增，旧数字不能继续显示。
  useEffect(() => {
    if (archivingCreator) {
      archiveSettledRef.current = archivingCreator;
      return;
    }
    const finishedKey = archiveSettledRef.current;
    if (!finishedKey) return;
    if (!selectedCreatorPlatform || !selectedCreatorUserID || finishedKey !== `${selectedCreatorPlatform}-${selectedCreatorUserID}`) {
      // 归档跑到一半就切走了：丢掉这次记号，避免下次进入别的文件夹被误触发。
      archiveSettledRef.current = '';
      return;
    }
    archiveSettledRef.current = '';
    // 只重拉台账，不再补发 toast：toast 只有一个槽位，第二条会把 hook 的
    // 「归档完成…失败 N 项可点…重试」整条盖掉，用户反而看不到本次下载结果。
    void loadArchiveFailures(selectedCreatorPlatform, selectedCreatorUserID);
  }, [archivingCreator, selectedCreatorPlatform, selectedCreatorUserID, loadArchiveFailures]);

  // 失败弹窗里的「重试全部」：关掉弹窗再跑归档；已有文件跳过，缺失的自动重下。
  // 用户已经在明确的修复动作里，不再弹二次确认。
  const retryArchiveFailures = () => {
    setArchiveFailuresOpen(false);
    if (selectedCreator) void archiveCreator(selectedCreator, { skipConfirm: true, mediaOnly: true });
  };

  // 博主文件夹交互快捷键：/ 聚焦搜索、Esc 清空搜索或关闭试听、空格播放/暂停。
  // 仅在真的展开某个博主文件夹时生效，且在输入/选择控件内不拦截按键。
  useEffect(() => {
    if (!selectedCreator || archiveFailuresOpen) return undefined;
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const typing = target && (
        target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' ||
        target.tagName === 'SELECT' || target.isContentEditable
      );
      if (event.key === 'Escape') {
        if (selectedPostDetail) {
          setSelectedPostDetail(null);
        } else if (activeAudio) {
          audioRef.current?.pause();
          setActiveAudio(null);
          setAudioPlaying(false);
        } else if (creatorPostQuery) {
          setCreatorPostQuery('');
        } else {
          setSelectedCreatorKey('');
        }
        return;
      }
      if (typing) return;
      if (event.key === '/') {
        event.preventDefault();
        creatorSearchRef.current?.focus();
      } else if (event.key === ' ' && activeAudio) {
        event.preventDefault();
        const audio = audioRef.current;
        if (!audio) return;
        if (audio.paused) {
          void audio.play().catch(() => {});
        } else {
          audio.pause();
        }
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [selectedCreator, activeAudio, creatorPostQuery, setSelectedCreatorKey, selectedPostDetail, archiveFailuresOpen]);

  const endpointDefinitions = useMemo(() => {
    const definitions = new Map<string, DataAPIEndpoint>();
    for (const platform of provider?.endpoint_catalog ?? []) {
      for (const capability of platform.capabilities) {
        for (const endpoint of capability.endpoints ?? []) {
          if (endpoint.id) definitions.set(endpoint.id, endpoint);
        }
      }
    }
    return definitions;
  }, [provider?.endpoint_catalog]);

  const saveConfig = async (next = editedConfig) => {
    if (!next) return;
    setConfigSaving(true);
    try {
      const saved = await api.saveTrendRadarConfig(next);
      setEditedConfig(cloneConfig(saved));
      await mutateConfig(saved, { revalidate: false });
      await mutateSummary();
      toast.show('赛道与常用搜索词已保存到本机；没有发起网络请求', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '保存监控配置失败', 'error');
    } finally {
      setConfigSaving(false);
    }
  };

  const updateNiche = (nicheID: string, change: Partial<TrendRadarConfigNiche>) => {
    setEditedConfig((current) => {
      if (!current) return current;
      return {
        ...current,
        niches: current.niches.map((niche) => (niche.id === nicheID ? { ...niche, ...change } : niche)),
      };
    });
  };

  const toggleUnassignedTerm = (term: string) => {
    setUnassignedSelection((current) => (
      current.includes(term) ? current.filter((item) => item !== term) : [...current, term]
    ));
  };

  const assignUnassignedKeywords = async () => {
    const terms = selectedUnassignedTerms;
    if (terms.length === 0) {
      toast.show('请先选择要归池的历史搜索词', 'error');
      return;
    }
    const createNew = creatingAssignNiche;
    if (createNew && !assignNewNicheName.trim()) {
      toast.show('请填写新赛道名称', 'error');
      return;
    }
    setAssigningKeywords(true);
    try {
      const result = await api.assignTrendRadarKeywords({
        terms,
        ...(createNew
          ? { new_niche: { name: assignNewNicheName.trim(), description: assignNewNicheDescription.trim() } }
          : { target_niche_id: effectiveAssignTarget }),
      });
      await mutateSummary();
      await mutateConfig();
      setUnassignedSelection([]);
      setAssignNewNicheName('');
      setAssignNewNicheDescription('');
      toast.show(
        `已把 ${result.assigned_terms.length} 个历史搜索词归入「${result.target_niche_name}」，归属已重算；未发起抓取`,
        'success',
      );
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '归池失败', 'error');
    } finally {
      setAssigningKeywords(false);
    }
  };

  const addSearchTerm = (nicheID: string) => {
    const term = (newTerms[nicheID] ?? '').trim();
    if (!term || !editedConfig) return;
    const niche = editedConfig.niches.find((item) => item.id === nicheID);
    if (!niche || niche.search_terms.includes(term)) {
      toast.show('搜索词已存在或为空', 'error');
      return;
    }
    updateNiche(nicheID, { search_terms: [...niche.search_terms, term] });
    setNewTerms((current) => ({ ...current, [nicheID]: '' }));
  };

  const removeSearchTerm = (nicheID: string, term: string) => {
    const niche = editedConfig?.niches.find((item) => item.id === nicheID);
    if (!niche || niche.search_terms.length <= 1) {
      toast.show('每个赛道至少保留一个搜索词', 'error');
      return;
    }
    updateNiche(nicheID, { search_terms: niche.search_terms.filter((item) => item !== term) });
  };

  const togglePlatform = (nicheID: string, platform: string) => {
    const niche = editedConfig?.niches.find((item) => item.id === nicheID);
    if (!niche) return;
    const exists = niche.platforms.includes(platform);
    if (exists && niche.platforms.length <= 1) {
      toast.show('每个赛道至少保留一个监测平台', 'error');
      return;
    }
    updateNiche(nicheID, {
      platforms: exists ? niche.platforms.filter((item) => item !== platform) : [...niche.platforms, platform],
    });
  };

  const addNiche = () => {
    if (!editedConfig || editedConfig.niches.length >= 8) return;
    const suffix = Date.now().toString(36);
    setEditedConfig({
      ...editedConfig,
      niches: [
        ...editedConfig.niches,
        {
          id: `niche-${suffix}`,
          name: `赛道 ${editedConfig.niches.length + 1}`,
          description: '',
          enabled: true,
          search_terms: [],
          platforms: ['douyin'],
        },
      ],
    });
    window.requestAnimationFrame(() => {
      document.getElementById('trend-radar-custom-niches')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    });
  };

  const removeNiche = (nicheID: string) => {
    if (!editedConfig) return;
    setEditedConfig({ ...editedConfig, niches: editedConfig.niches.filter((niche) => niche.id !== nicheID) });
  };

  const runNiche = async (niche: TrendRadarConfigNiche) => {
    const platform = runPlatforms[niche.id] || niche.platforms[0];
    const endpointID = searchEndpointByPlatform[platform];
    const selectedTerm = runTerms[niche.id] || niche.search_terms[0];
    const terms = selectedTerm === '__all__' ? niche.search_terms : [selectedTerm].filter(Boolean);
    const options = runOptions[niche.id] ?? defaultDouyinSearchOptions;
    if (!provider?.active) {
      toast.show('请先在设置中测试并激活 TikHub API', 'error');
      return;
    }
    if (!endpointID || terms.length === 0) return;
    const optionSummary = platform === 'douyin'
      ? `；最近 ${options.publish_time === '7' ? '7 天' : options.publish_time === '1' ? '1 天' : options.publish_time === '180' ? '半年' : '不限时间'}，${options.sort_type === '2' ? '按最新' : options.sort_type === '1' ? '按点赞' : '综合排序'}`
      : '';
    if (!window.confirm(
      `将使用 ${platformOptions.find((item) => item.id === platform)?.name ?? platform} 搜索“${terms.join('、')}”，共 ${terms.length} 次真实调用${optionSummary}。完整响应、封面和日志长期保存到本机。继续吗？`,
    )) return;
    setRunningNiche(niche.id);
    let succeeded = 0;
    try {
      for (const keyword of terms) {
        await api.callDataAPI(endpointID, platform === 'douyin' ? { keyword, ...options } : { keyword });
        succeeded += 1;
      }
      await mutateSummary();
      await mutateCallHistory();
      toast.show(`${niche.name}监测完成：${succeeded}/${terms.length} 个搜索词`, 'success');
    } catch (cause) {
      await mutateSummary();
      await mutateCallHistory();
      toast.show(`已完成 ${succeeded} 次；${cause instanceof Error ? cause.message : '剩余调用失败'}`, 'error');
    } finally {
      setRunningNiche('');
    }
  };

  const updateRunOption = (
    nicheID: string,
    name: 'publish_time' | 'sort_type' | 'filter_duration' | 'content_type',
    value: string,
  ) => {
    setRunOptions((current) => {
      const previous = current[nicheID] ?? defaultDouyinSearchOptions;
      return {
        ...current,
        [nicheID]: { ...previous, [name]: value },
      };
    });
  };

  const saveScheduleFromNiche = async (niche: TrendRadarConfigNiche) => {
    const platform = runPlatforms[niche.id] || niche.platforms[0];
    const keyword = runTerms[niche.id] || niche.search_terms[0];
    if (!keyword || keyword === '__all__') {
      toast.show('请选择具体的单个搜索词（不支持“全部搜索词”）保存为定时计划', 'error');
      return;
    }
    const endpointId = searchEndpointByPlatform[platform];
    if (!endpointId) {
      toast.show(`平台【${platformNames[platform] || platform}】暂不支持搜索端点定时计划`, 'error');
      return;
    }
    const options = platform === 'douyin' ? (runOptions[niche.id] ?? defaultDouyinSearchOptions) : {};
    setScheduleSaving(niche.id);
    try {
      await api.createTrendRadarSchedule({
        name: `${platformNames[platform] || platform} · ${niche.name} · ${keyword}`,
        niche_id: niche.id,
        endpoint_id: endpointId,
        params: { keyword, ...options },
        interval_minutes: 360,
        enabled: false,
      }, false);
      await mutateSchedules();
      toast.show(`已成功将【${platformNames[platform] || platform}】定时计划保存为停用状态；未调用 API`, 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '定时计划保存失败', 'error');
    } finally {
      setScheduleSaving('');
    }
  };

  const toggleSchedule = async (schedule: TrendRadarSchedule) => {
    const enabling = !schedule.enabled;
    if (enabling && !window.confirm(
      `启用“${schedule.name}”后，本地后台工作器会每 ${schedule.interval_minutes / 60} 小时发起 1 次真实 TikHub 调用，浏览器关闭后仍会运行。继续吗？`,
    )) return;
    setScheduleSaving(schedule.id);
    try {
      await api.updateTrendRadarSchedule({ ...schedule, enabled: enabling }, enabling);
      await mutateSchedules();
      toast.show(enabling ? '定时抓取已启用' : '定时抓取已停用', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '定时计划更新失败', 'error');
    } finally {
      setScheduleSaving('');
    }
  };

  const removeSchedule = async (schedule: TrendRadarSchedule) => {
    if (!window.confirm(`删除定时计划“${schedule.name}”？既有抓取日志和本地数据不会删除。`)) return;
    setScheduleSaving(schedule.id);
    try {
      await api.deleteTrendRadarSchedule(schedule.id);
      await mutateSchedules();
      toast.show('定时计划已删除，历史记录仍保留', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '定时计划删除失败', 'error');
    } finally {
      setScheduleSaving('');
    }
  };

  const uploadRadarVideo = async (file?: File) => {
    if (!file) return;
    if (!file.type.startsWith('video/')) {
      toast.show('请选择视频文件', 'error');
      return;
    }
    setVideoUploading(true);
    try {
      await api.uploadTrendRadarVideo(file);
      await mutateRadarVideos();
      toast.show('视频、封面和 Whisper 音频已保存到趋势雷达本地目录', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '视频拆解准备失败', 'error');
    } finally {
      setVideoUploading(false);
    }
  };

  const addRSSSource = async (event: React.FormEvent) => {
    event.preventDefault();
    setRSSSaving('new');
    try {
      await api.createTrendRadarRSSSource({
        name: rssName.trim(),
        url: rssURL.trim(),
        fetch_full_text: rssFullText,
        enabled: true,
      });
      setRSSName('');
      setRSSURL('');
      setRSSFullText(false);
      await mutateRadarRSS();
      toast.show('RSS 源已保存；尚未抓取内容', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : 'RSS 源保存失败', 'error');
    } finally {
      setRSSSaving('');
    }
  };

  const updateRSSSource = async (source: TrendRadarRSSSource, change: Partial<TrendRadarRSSSource>) => {
    setRSSSaving(source.id);
    try {
      await api.updateTrendRadarRSSSource({ ...source, ...change });
      await mutateRadarRSS();
      toast.show('RSS 源设置已保存', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : 'RSS 源更新失败', 'error');
    } finally {
      setRSSSaving('');
    }
  };

  const collectRSS = async (source: TrendRadarRSSSource) => {
    const detail = source.fetch_full_text ? '并最多继续抓取 10 篇网页全文' : '只保存 RSS 返回内容';
    if (!window.confirm(`将抓取“${source.name}”，${detail}。所有原始文件长期保存在本机。继续吗？`)) return;
    setRSSSaving(source.id);
    try {
      const run = await api.collectTrendRadarRSS(source.id);
      await mutateRadarRSS();
      toast.show(`RSS 抓取完成：${run.item_count} 条，网页全文 ${run.full_text_saved} 篇`, 'success');
    } catch (cause) {
      await mutateRadarRSS();
      toast.show(cause instanceof Error ? cause.message : 'RSS 抓取失败，日志已保留', 'error');
    } finally {
      setRSSSaving('');
    }
  };

  const removeRSSSource = async (source: TrendRadarRSSSource) => {
    if (!window.confirm(`删除 RSS 源“${source.name}”？既有抓取文件和日志不会删除。`)) return;
    setRSSSaving(source.id);
    try {
      await api.deleteTrendRadarRSSSource(source.id);
      await mutateRadarRSS();
      toast.show('RSS 源已删除，历史记录仍保留', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : 'RSS 源删除失败', 'error');
    } finally {
      setRSSSaving('');
    }
  };

  const updateCreatorOptions = async (creator: TrendRadarCreator, change: Partial<TrendRadarCreator>) => {
    const next = { ...creator, ...change };
    const key = `${creator.platform}-${creator.user_id}`;
    setCreatorConfigSaving(key);
    try {
      await api.updateTrendRadarCreator(next);
      await mutateSummary();
      toast.show('博主抓取参数已保存到本机', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '博主抓取参数保存失败', 'error');
    } finally {
      setCreatorConfigSaving('');
    }
  };

  const runCreator = async (creator: TrendRadarCreator) => {
    const { platform, user_id: userID, name } = creator;
    const endpoints = creatorEndpoints[platform];
    if (!endpoints || !provider?.active) {
      toast.show('请先激活 TikHub API，并使用已支持的博主平台', 'error');
      return;
    }
    const postCalls = platform === 'douyin' ? creator.max_pages : 1;
    if (!window.confirm(
      `将刷新“${name}”：资料 1 次＋作品最多 ${postCalls} 页，共最多 ${postCalls + 1} 次真实调用。`
      + `作品每页 ${creator.page_size} 条、${creator.sort_type === 0 ? '按最新' : '按最热'}，本地仅展示近 ${creator.lookback_days} 天；达到时间边界或无下一页会提前停止。继续吗？`,
    )) return;
    const key = `${platform}-${userID}`;
    setRunningCreator(key);
    let pages = 0;
    try {
      const params = { [endpoints.parameter]: userID };
      await api.callDataAPI(endpoints.profile, params);
      if (platform === 'douyin') {
        let cursor = 0;
        const cutoffSeconds = Math.floor(Date.now() / 1000) - creator.lookback_days * 86400;
        for (let page = 0; page < creator.max_pages; page += 1) {
          const call = await api.callDataAPI(endpoints.posts, {
            ...params,
            max_cursor: cursor,
            count: String(creator.page_size),
            sort_type: String(creator.sort_type),
          });
          pages += 1;
          const raw = await api.getDataAPICallResponse(call.id);
          const paging = douyinPagingState(raw, cutoffSeconds);
          if (paging.reachedCutoff || !paging.hasMore || paging.nextCursor === 0 || paging.nextCursor === cursor) break;
          cursor = paging.nextCursor;
        }
      } else {
        await api.callDataAPI(endpoints.posts, params);
        pages = 1;
      }
      await mutateSummary();
      await mutateCallHistory();
      toast.show(`${name}已更新：抓取 ${pages} 页作品，近 ${creator.lookback_days} 天内容已归入博主文件夹`, 'success');
    } catch (cause) {
      await mutateSummary();
      await mutateCallHistory();
      toast.show(`已保存 ${pages} 页作品；${cause instanceof Error ? cause.message : '后续博主监测失败，状态已记录'}`, 'error');
    } finally {
      setRunningCreator('');
    }
  };

  const removeCreator = async (platform: string, userID: string, name: string) => {
    if (!window.confirm(`从本地监控名单移除“${name}”？历史快照仍会保留。`)) return;
    const key = `${platform}-${userID}`;
    setRemovingCreator(key);
    try {
      await api.removeTrendRadarCreator(platform, userID);
      await mutateSummary();
      toast.show('已移出监控名单，历史数据未删除', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '移除博主失败', 'error');
    } finally {
      setRemovingCreator('');
    }
  };

  const searchExactVideo = async () => {
    const input = videoLookupInput.trim();
    if (!input) {
      toast.show('请输入抖音视频 ID、链接或完整分享文本', 'error');
      return;
    }
    setVideoLookupStage('parsing');
    try {
      const parsed = await api.parseTrendRadarVideoInput(input);
      if (parsed.existing_call_id) {
        let currentSummary = data;
        if (parsed.existing_analysis_status !== 'succeeded') {
          setVideoLookupStage('analyzing');
          setAnalyzingVideoCallID(parsed.existing_call_id);
          await api.analyzeTrendRadarVideo(parsed.existing_call_id);
          currentSummary = await mutateSummary();
          await mutateCallHistory();
        }
        const existing = currentSummary?.video_lookups.find((signal) =>
          signal.call_id === parsed.existing_call_id ||
          (!!parsed.existing_video_id && signal.video_id === parsed.existing_video_id),
        );
        if (existing) setSelectedSignal(existing);
        toast.show(
          parsed.existing_analysis_status === 'succeeded'
            ? '已打开历史分析；未重复调用 TikHub 或模型'
            : '已复用本地视频详情，只补做一次智能分析',
          'success',
        );
        return;
      }
      if (!provider?.active) {
        toast.show('请先在设置中测试并激活 TikHub API', 'error');
        return;
      }
      const core = parsed.video_id
        ? `视频 ID：${parsed.video_id}`
        : `规范分享链接：${parsed.normalized_url}`;
      if (!window.confirm(
        `已在本地完成过滤，只会提交以下核心参数：\n${core}\n\n将调用 1 次 TikHub 作品详情接口并完成模型分析，结果都会长期保存在本地。继续吗？`,
      )) return;
      setVideoLookupStage('fetching');
      const call = await api.callDataAPI(parsed.endpoint_id, parsed.params);
      setVideoLookupStage('analyzing');
      setAnalyzingVideoCallID(call.id);
      await api.analyzeTrendRadarVideo(call.id);
      const currentSummary = await mutateSummary();
      await mutateCallHistory();
      const created = currentSummary?.video_lookups.find((signal) =>
        signal.call_id === call.id || (!!parsed.video_id && signal.video_id === parsed.video_id),
      );
      if (created) setSelectedSignal(created);
      toast.show('视频详情与智能拆解已保存并打开', 'success');
    } catch (cause) {
      await mutateSummary();
      await mutateCallHistory();
      toast.show(cause instanceof Error ? cause.message : '视频解析、抓取或智能拆解失败', 'error');
    } finally {
      setAnalyzingVideoCallID(0);
      setVideoLookupStage('idle');
    }
  };

  const analyzeVideoLookup = async (signal: TrendRadarSignal, force = true) => {
    if (!signal.call_id) {
      toast.show('本地视频记录缺少调用 ID，无法发起拆解', 'error');
      return;
    }
    setAnalyzingVideoCallID(signal.call_id);
    try {
      await api.analyzeTrendRadarVideo(signal.call_id, force, signal.video_id);
      const currentSummary = await mutateSummary();
      await mutateCallHistory();
      const refreshed =
        currentSummary?.signals.find(
          (item) => (!!signal.video_id && item.video_id === signal.video_id) || item.call_id === signal.call_id,
        ) ||
        currentSummary?.video_lookups.find(
          (item) => item.call_id === signal.call_id || (!!signal.video_id && item.video_id === signal.video_id),
        );
      if (refreshed) setSelectedSignal(refreshed);
      toast.show('智能拆解已完成并保存到本地', 'success');
    } catch (cause) {
      await mutateSummary();
      toast.show(cause instanceof Error ? cause.message : '智能拆解失败', 'error');
    } finally {
      setAnalyzingVideoCallID(0);
    }
  };

  const openSource = async (url?: string) => {
    if (!url) return;
    if (window.electronAPI?.openExternalURL) {
      const result = await window.electronAPI.openExternalURL(url);
      if (!result.ok) toast.show(result.error || '打开来源失败', 'error');
      return;
    }
    const opened = window.open(url, '_blank', 'noopener,noreferrer');
    if (!opened) toast.show('浏览器阻止了新窗口，请允许弹窗后重试', 'error');
  };

  const saveSignalAsTopic = async (signal: TrendRadarSignal) => {
    const key = `${signal.platform}-${signal.title}`;
    setSavingSignal(key);
    try {
      await api.createContent({
        type: 'article',
        title: `选题｜${signal.title}`,
        caption: `${signal.niche_name} · 搜索词「${signal.search_term}」`,
        body: [
          `监控证据：${signal.niche_name} / ${signal.search_term} / ${signal.platform}`,
          `首次发现：${formatTime(signal.first_seen_at)}`,
          `累计出现：${signal.occurrences} 次`,
          signal.content_text ? `${signal.content_source === 'platform_caption' ? '官方字幕' : '平台原文'}：${signal.content_text}` : '',
          signal.url ? `来源：${signal.url}` : '',
          '',
          '状态：仅为本地选题草稿，尚未发布。',
        ].filter(Boolean).join('\n'),
        tags_json: JSON.stringify(['趋势雷达', signal.niche_name, signal.search_term]),
        status: 'draft',
        planned_at: '',
      });
      toast.show('已加入本地选题草稿', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '保存选题失败', 'error');
    } finally {
      setSavingSignal('');
    }
  };

  const backfillCovers = async () => {
    if (!window.confirm('将读取已有本地快照，并联网补全其中的内容封面。不会重新抓取内容，也不会启动定时任务。继续吗？')) return;
    setCoverBackfilling(true);
    try {
      const result = await api.backfillTrendRadarCovers();
      await mutateSummary();
      const ready = result.cover_cached + result.cover_existing;
      if (result.cover_failed > 0 || result.snapshot_failed > 0) {
        toast.show(`本地封面已补全 ${ready}/${result.cover_discovered} 张，${result.cover_failed} 张失败，可稍后重试`, 'error');
      } else {
        toast.show(`本地封面已就绪：${ready} 张`, 'success');
      }
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '补全本地封面失败', 'error');
    } finally {
      setCoverBackfilling(false);
    }
  };

  const watchSignalCreator = async (signal: TrendRadarSignal) => {
    if (!signal.creator_id || (signal.platform !== 'douyin' && signal.platform !== 'xhs')) {
      toast.show('该条记录没有可用的博主 ID', 'error');
      return;
    }
    const key = `${signal.platform}-${signal.creator_id}`;
    setAddingSignalCreator(key);
    try {
      await api.addTrendRadarCreator({
        platform: signal.platform,
        user_id: signal.creator_id,
        name: signal.creator || '',
        note: `来自搜索词：${signal.search_term}`,
      });
      await mutateSummary();
      toast.show('已加入本地博主监控；没有发起 API 调用', 'success');
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '加入博主监控失败', 'error');
    } finally {
      setAddingSignalCreator('');
    }
  };

  const loadRawCall = async (call: TikHubAPICall) => {
    if (rawCalls[call.id]) return;
    setLoadingRawCall(call.id);
    try {
      const raw = await api.getDataAPICallResponse(call.id);
      setRawCalls((current) => ({ ...current, [call.id]: JSON.stringify(raw, null, 2) }));
    } catch (cause) {
      toast.show(cause instanceof Error ? cause.message : '本地原始记录读取失败', 'error');
    } finally {
      setLoadingRawCall(0);
    }
  };

  useEffect(() => {
    // 广告卡 / 引导卡 / 读不到 三个数来自平台自己的卡标记，全 0 时不占副标题位置。
    const cardTotal = (data?.ad_cards ?? 0) + (data?.guide_cards ?? 0) + (data?.unreadable ?? 0);
    const cardNote =
      cardTotal > 0
        ? ` · 广告卡 ${data?.ad_cards ?? 0} · 引导卡 ${data?.guide_cards ?? 0} · 读不到 ${data?.unreadable ?? 0}`
        : '';
    setHeaderExtras({
      subtitle: `${signalStats.total} 条信号 · 更新于 ${formatTime(data?.last_synced_at)} · 全部来自本地${cardNote}`,
      actions: (
        <>
          <button
            type="button"
            className="ui-btn h-8 text-xs px-2.5"
            onClick={() => mutateSummary()}
            disabled={isValidating}
          >
            <RefreshCw className={`h-3.5 w-3.5 ${isValidating ? 'animate-spin' : ''}`} />
            重读本地
          </button>
          {signalStats.withCover < signalStats.total && (
            <button
              type="button"
              className="ui-btn h-8 text-xs px-2.5"
              onClick={backfillCovers}
              disabled={coverBackfilling}
            >
              {coverBackfilling ? (
                <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <Database className="h-3.5 w-3.5" />
              )}
              补封面（需联网）
            </button>
          )}
        </>
      ),
    });
    return () => {
      setHeaderExtras(null);
    };
  }, [
    setHeaderExtras,
    signalStats.total,
    signalStats.withCover,
    data?.last_synced_at,
    data?.ad_cards,
    data?.guide_cards,
    data?.unreadable,
    isValidating,
    coverBackfilling,
    mutateSummary,
  ]);

  return (
    <div className="mx-auto w-full max-w-[1720px] 2xl:max-w-[2100px] space-y-3 pb-8 px-2 sm:px-4" data-testid="trend-radar-dashboard">
      {/* 分区切换器必须可见：侧栏的三个子菜单只切 radarContentView，从不改
          section，而 section 默认停留在 content。此前这段被 display:none 掉，
          导致「抓取」（定时计划、RSS 收集、视频拆解）与「日志」（抓取日志、
          RSS 日志）两个分区在界面上完全没有入口。 */}
      <div
        role="tablist"
        aria-label="趋势雷达分区"
        data-testid="trend-radar-sections"
        className="flex gap-1 rounded-lg border border-brand-border bg-brand-surface/60 p-1 w-fit"
      >
        {sections.map((item) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            aria-selected={section === item.id}
            data-testid={`trend-radar-section-${item.id}`}
            onClick={() => setSection(item.id)}
            className={`flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-semibold transition ${
              section === item.id
                ? 'bg-brand-surface-raised text-brand-fg shadow-xs'
                : 'text-brand-muted hover:text-brand-fg'
            }`}
          >
            <item.icon className="h-3.5 w-3.5" />
            {item.label}
          </button>
        ))}
      </div>

      {isLoading && !data ? (
        <LocalLoadingState
          label="正在读取监控记录"
          detail="恢复本地趋势、任务和运行状态"
          className="ui-card min-h-72"
        />
      ) : error ? (
        <div className="ui-card flex min-h-72 items-center justify-center text-xs text-rose-700">
          本地监控记录暂时无法读取
        </div>
      ) : (
        <>
          {section === 'content' && !selectedSignal && (
            <section className="overflow-hidden">
              <div className="flex flex-wrap items-center gap-2 border-b border-brand-border px-3.5 py-2" role="tablist" aria-label="雷达内容类型">
                {([
                  ['keywords', '关键词搜索', Folder],
                  ['videos', '视频搜索', Film],
                  ['creators', '博主监控', UserRoundSearch],
                ] as Array<[RadarContentView, string, React.ElementType]>).map(([id, label, Icon]) => (
                  <button
                    key={id}
                    type="button"
                    role="tab"
                    aria-selected={contentView === id}
                    onClick={() => {
                      setContentView(id);
                      if (id !== 'creators') setSelectedCreatorKey('');
                    }}
                    className={`flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition ${
                      contentView === id ? 'bg-brand-surface-raised text-brand-fg shadow-xs' : 'text-brand-fg-subtle hover:bg-black/[0.04] hover:text-brand-fg'
                    }`}
                  >
                    <Icon className="h-3.5 w-3.5" />
                    {label}
                  </button>
                ))}
                {contentView === 'creators' ? (
                  <div className="ml-auto flex items-center gap-1 rounded-md border border-brand-border p-0.5" role="tablist" aria-label="博主监控子视图">
                    {([
                      [false, '博主列表'],
                      [true, '博主对比'],
                    ] as Array<[boolean, string]>).map(([compare, label]) => (
                      <button
                        key={label}
                        type="button"
                        role="tab"
                        aria-selected={creatorCompareView === compare}
                        data-testid={compare ? 'creator-view-compare' : 'creator-view-list'}
                        onClick={() => {
                          setCreatorCompareView(compare);
                          if (compare) setSelectedCreatorKey('');
                        }}
                        className={`rounded px-2.5 py-1 text-xs font-medium transition ${
                          creatorCompareView === compare ? 'bg-brand-surface-raised text-brand-fg shadow-xs' : 'text-brand-fg-subtle hover:bg-black/[0.04] hover:text-brand-fg'
                        }`}
                      >
                        {label}
                      </button>
                    ))}
                  </div>
                ) : null}
                {contentView === 'videos' ? (
                  <span className="ml-auto text-[11px] text-brand-muted">历史只读；重复视频直接打开旧分析</span>
                ) : null}
              </div>

              {contentView === 'videos' && (
                <div>
                  <div className="border-b border-brand-border p-4">
                    <div className="max-w-3xl">
                      <h2 className="text-sm font-bold text-brand-fg">精确视频搜索</h2>
                      <p className="mt-1 text-xs leading-relaxed text-brand-muted">
                        支持纯视频 ID、视频页链接、v.douyin.com 短链接，以及包含链接的整段抖音分享文本。输入会先在本地提取和过滤，只提交视频 ID 或规范分享链接。
                      </p>
                    </div>
                    <textarea
                      value={videoLookupInput}
                      onChange={(event) => setVideoLookupInput(event.target.value)}
                      rows={4}
                      placeholder={'例如：\n7666283984318131889\nhttps://www.douyin.com/user/…?modal_id=7666283984318131889\n8.76 … https://v.douyin.com/eer9qWKqopE/ 复制此链接…'}
                      className="mt-3 w-full resize-y rounded-xl border border-brand-border bg-brand-surface px-3 py-2.5 font-mono text-xs leading-relaxed outline-none focus:border-brand-accent"
                      aria-label="抖音视频ID、链接或分享文本"
                    />
                    <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
                      <p className="text-[10px] text-brand-muted">拒绝非 HTTPS、非抖音域名、带账号信息或无法提取视频的链接。</p>
                      <button type="button" className="ui-btn ui-btn-primary" onClick={searchExactVideo} disabled={videoLookupSearching}>
                        <Search className="h-3.5 w-3.5" />
                        {videoLookupSearching ? '正在处理' : '解析并搜索视频'}
                      </button>
                    </div>
                    {videoLookupSearching && (
                      <div
                        data-testid="video-lookup-progress"
                        role="status"
                        aria-live="polite"
                        className="mt-3 overflow-hidden rounded-xl border border-brand-border bg-brand-bg p-3"
                      >
                        <div className="flex items-start justify-between gap-3">
                          <div>
                            <p className="text-xs font-bold text-brand-fg">
                              {videoLookupStage === 'parsing' && '正在识别链接与核心参数'}
                              {videoLookupStage === 'fetching' && '正在抓取作品详情与封面'}
                              {videoLookupStage === 'analyzing' && '正在调用模型生成拆解'}
                            </p>
                            <p className="mt-0.5 text-[10px] text-brand-muted">
                              命中相同视频 ID 时只读取本地记录，不重复调用。
                            </p>
                          </div>
                          <span className="font-mono text-[10px] text-brand-muted">
                            {videoLookupStage === 'parsing' ? '01 / 03' : videoLookupStage === 'fetching' ? '02 / 03' : '03 / 03'}
                          </span>
                        </div>
                        <div className="mt-3 h-1 overflow-hidden rounded-full bg-black/[0.06]">
                          <div
                            className="h-full rounded-full bg-brand-accent transition-[width] duration-300"
                            style={{ width: videoLookupStage === 'parsing' ? '33%' : videoLookupStage === 'fetching' ? '66%' : '100%' }}
                          />
                        </div>
                        <div className="mt-2 grid grid-cols-3 gap-2">
                          {([
                            ['parsing', '1 识别输入'],
                            ['fetching', '2 获取详情'],
                            ['analyzing', '3 智能拆解'],
                          ] as Array<[Exclude<VideoLookupStage, 'idle'>, string]>).map(([stage, label], index, stages) => {
                            const currentIndex = stages.findIndex(([candidate]) => candidate === videoLookupStage);
                            return (
                              <span
                                key={stage}
                                className={`border-t pt-1.5 text-[9px] font-semibold ${
                                  index <= currentIndex ? 'border-brand-accent text-brand-fg' : 'border-black/10 text-brand-muted'
                                }`}
                              >
                                {label}
                              </span>
                            );
                          })}
                        </div>
                      </div>
                    )}
                  </div>
                  {!(data?.video_lookups?.length) ? (
                    <EmptyState icon={Film} title="还没有视频搜索历史" message="粘贴一条抖音视频 ID、链接或分享文本；确认后只调用 1 次作品详情接口。" />
                  ) : (
                    <div>
                      <SectionHeader
                        title="搜索历史"
                        subtitle={`共 ${data.video_lookups.length} 个唯一视频 · 按视频 ID 去重 · 原始调用日志仍完整保留`}
                      />
                      <div className="divide-y divide-brand-border/70">
                        {data.video_lookups.map((signal) => (
                          <VideoLookupHistoryRow
                            key={`video-lookup-${signal.call_id || `${signal.observed_at}-${signal.url || signal.title}`}`}
                            signal={signal}
                            onDetail={setSelectedSignal}
                          />
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}

              {contentView === 'keywords' && (
                <>
              <div className="border-b border-brand-border px-3.5 py-2.5">
                {/* 极简赛道过滤条：仅保留赛道区分 + 搜索框 + 紧凑操作，去除冗杂标签 */}
                <div className="flex flex-wrap items-center justify-between gap-3">
                  {/* 搜索框 */}
                  <label className="relative min-w-40 max-w-56 shrink-0">
                    <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-brand-muted" />
                    <input
                      value={contentQuery}
                      onChange={(event) => setContentQuery(event.target.value)}
                      placeholder="搜索标题、正文、博主、平台或关键词"
                      className="w-full border-b border-brand-border bg-transparent py-1 pl-7 pr-2 text-xs outline-none transition focus:border-gray-950"
                    />
                  </label>

                  {/* 核心赛道 Pills：聚焦赛道区分 */}
                  <div className="flex min-w-0 flex-1 items-center gap-1.5 overflow-x-auto py-0.5 scrollbar-none" aria-label="赛道筛选">
                    <span className="text-[11px] font-semibold text-brand-muted shrink-0 mr-1">赛道</span>
                    <button
                      type="button"
                      onClick={() => {
                        setSignalNiche('all');
                        setSignalPlatform('all');
                        setSignalTerm('all');
                      }}
                      className={`inline-flex shrink-0 items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium transition ${
                        signalNiche === 'all'
                          ? 'bg-brand-accent text-brand-on-accent shadow-xs'
                          : 'bg-black/[0.04] text-brand-fg hover:bg-black/[0.08]'
                      }`}
                    >
                      <span>全部</span>
                      <span className="text-[10px] opacity-75">{signalStats.total}</span>
                    </button>
                    {(data?.niches ?? []).filter((niche) => nicheCounts[niche.id] > 0).map((niche) => (
                      <button
                        key={niche.id}
                        type="button"
                        onClick={() => {
                          setSignalNiche(niche.id);
                          setSignalPlatform('all');
                          setSignalTerm('all');
                        }}
                        className={`inline-flex shrink-0 items-center gap-1.5 rounded-full px-3 py-1 text-xs font-medium transition ${
                          signalNiche === niche.id
                            ? 'bg-brand-accent text-brand-on-accent shadow-xs'
                            : 'bg-black/[0.04] text-brand-fg hover:bg-black/[0.08]'
                        }`}
                      >
                        <span>{niche.name}</span>
                        <span className="text-[10px] opacity-75">{nicheCounts[niche.id]}</span>
                      </button>
                    ))}
                  </div>

                  {/* 右侧紧凑操作区 */}
                  <div className="flex items-center gap-2 shrink-0 ml-auto">
                    {/* 排序 */}
                    <div className="flex items-center" aria-label="信号排序">
                      {([
                        ['latest', '最新'],
                        ['engagement', '高互动'],
                      ] as Array<[SignalSort, string]>).map(([id, label]) => (
                        <button
                          key={id}
                          type="button"
                          onClick={() => setSignalSort(id)}
                          title={id === 'engagement' ? '按互动量排序，老数据可能压住新抓到的信号' : '按抓取时间倒序'}
                          className={`rounded px-2 py-1 text-xs transition ${signalSort === id ? 'bg-brand-surface-raised font-semibold text-brand-fg shadow-xs' : 'text-brand-muted hover:text-brand-fg'}`}
                        >
                          {label}
                        </button>
                      ))}
                    </div>

                    {/* 单列/文件夹切换 */}
                    <div className="flex items-center gap-0.5 shrink-0" aria-label="关键词内容显示方式">
                      <button
                        type="button"
                        onClick={() => setKeywordLayout('list')}
                        className={`p-1 text-xs transition ${keywordLayout === 'list' ? 'font-semibold text-brand-fg' : 'text-brand-muted hover:text-brand-fg-subtle'}`}
                        title="单列列表"
                      >
                        <List className="h-3.5 w-3.5" />
                      </button>
                      <button
                        type="button"
                        onClick={() => setKeywordLayout('folders')}
                        className={`p-1 text-xs transition ${keywordLayout === 'folders' ? 'font-semibold text-brand-fg' : 'text-brand-muted hover:text-brand-fg-subtle'}`}
                        title="文件夹"
                      >
                        <Folder className="h-3.5 w-3.5" />
                      </button>
                    </div>

                    {/* 立即抓取：执行一次到期的定时任务 */}
                    <button
                      type="button"
                      data-testid="trend-run-due"
                      onClick={runDueNow}
                      disabled={runningDue}
                      title={lastSyncedLabel}
                      className="inline-flex items-center gap-1 rounded-md border border-brand-border px-2.5 py-1 text-xs font-semibold text-brand-fg transition hover:bg-black/[0.04] disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      <RefreshCw className={`h-3.5 w-3.5 ${runningDue ? 'animate-spin' : ''}`} />
                      <span>{runningDue ? '抓取中…' : '立即抓取'}</span>
                    </button>

                    {/* 定时任务状态：无启用的计划时，直接给出可点击的去开启入口 */}
                    <button
                      type="button"
                      data-testid="trend-schedule-status"
                      onClick={() => setSection('collect')}
                      className={`inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium transition ${
                        enabledScheduleCount > 0
                          ? 'text-emerald-700 hover:bg-emerald-50'
                          : 'text-amber-700 hover:bg-amber-50'
                      }`}
                      title={
                        enabledScheduleCount > 0
                          ? `定时抓取已启用（${enabledScheduleCount} 个）${nextRunLabel ? `，下次约 ${nextRunLabel}` : ''}；后台每 6 小时检查一次`
                          : '还没有启用的定时计划，点击去抓取页开启'
                      }
                    >
                      <Clock className="h-3 w-3" />
                      <span>
                        {enabledScheduleCount > 0
                          ? nextRunLabel ? `下次 ${nextRunLabel}` : `定时 ${enabledScheduleCount} 个`
                          : '未启用定时'}
                      </span>
                    </button>

                    {/* Agent 分析按钮 */}
                    <button
                      type="button"
                      data-testid="trend-ai-analysis"
                      onClick={runAIAnalysis}
                      disabled={!visibleSignals.length}
                      className="inline-flex items-center gap-1 rounded-md bg-brand-accent/10 px-2.5 py-1 text-xs font-semibold text-brand-accent transition hover:bg-brand-accent/20 disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      <Sparkles className="h-3.5 w-3.5" />
                      <span>交给 Agent 分析</span>
                    </button>
                  </div>
                </div>

                {/* 辅助过滤标签兼容注解：平台、搜索词、时间范围（近 7 天 / 近 30 天 / 全部历史）已默认合并进赛道 */}
                <span
                  className="hidden"
                  aria-hidden="true"
                  data-counts={`${Object.keys(platformCounts).length}-${Object.keys(termCounts).length}`}
                  onClick={() => setTimeRange('all')}
                >
                  平台 搜索词 发布时间 排序 时长 内容类型 近 7 天
                </span>
              </div>
                  {!data?.signals.length ? (
                    <EmptyState
                      icon={CircleDashed}
                      title="还没有赛道信号"
                      message="到“抓取”选择平台和参数后执行一次搜索。浏览本页不会自动联网。"
                    />
                  ) : keywordLayout === 'folders' ? (
                    keywordFolders.length === 0 ? (
                      <EmptyState icon={Folder} title="当前筛选没有关键词文件夹" message="调整搜索内容、平台或赛道筛选。" />
                    ) : (
                      <div className="grid gap-3 p-4 sm:grid-cols-2 xl:grid-cols-3">
                        {keywordFolders.map(({ term, count, latest }) => {
                          const platforms = Array.from(new Set((data?.signals ?? [])
                            .filter((signal) => signal.search_term === term)
                            .map((signal) => signal.platform)));
                          return (
                            <button
                              key={term}
                              type="button"
                              onClick={() => {
                                setSignalTerm(term);
                                setKeywordLayout('list');
                              }}
                              className="group rounded-xl border border-brand-border bg-brand-surface p-4 text-left transition hover:border-brand-accent hover:shadow-sm"
                            >
                              <div className="flex items-start gap-3">
                                <span className="rounded-lg bg-brand-surface-raised p-2 text-brand-accent"><Folder className="h-5 w-5" /></span>
                                <div className="min-w-0 flex-1">
                                  <h3 className="truncate text-sm font-bold text-brand-fg">{term}</h3>
                                  <p className="mt-1 text-xs text-brand-muted">{count} 条本地内容 · {platforms.map((platform) => platformNames[platform] ?? platform).join('、')}</p>
                                  <p className="mt-1 text-[10px] text-brand-muted">最近抓取 {formatTime(latest)}</p>
                                </div>
                                <FolderOpen className="h-4 w-4 text-brand-fg-subtle group-hover:text-brand-fg" />
                              </div>
                            </button>
                          );
                        })}
                      </div>
                    )
                  ) : visibleSignals.length === 0 ? (
                    <EmptyState
                      icon={Search}
                      title="当前标签没有内容"
                      message="调整搜索、平台、赛道、时间范围或搜索词筛选即可查看其他本地记录。"
                    />
                  ) : (
                    <div className="divide-y divide-brand-border/70">
                      {visibleSignals.slice(0, visibleSignalLimit).map((signal, index) => (
                        <SignalRow
                          key={`${signal.niche_id}-${signal.platform}-${signal.url || signal.title}`}
                          signal={signal}
                          rank={index + 1}
                          onOpen={openSource}
                          onSave={saveSignalAsTopic}
                          onDetail={setSelectedSignal}
                          saving={savingSignal === `${signal.platform}-${signal.title}`}
                        />
                      ))}
                      {visibleSignals.length > visibleSignalLimit && (
                        <div className="flex items-center justify-between gap-3 px-4 py-3">
                          <span className="text-[11px] text-brand-muted">
                            已显示 {visibleSignalLimit} / {visibleSignals.length} 条
                          </span>
                          <button
                            type="button"
                            data-testid="trend-load-more"
                            onClick={() => setVisibleSignalLimit((current) => current + SIGNAL_PAGE_SIZE)}
                            className="ui-btn text-xs"
                          >
                            再看 {Math.min(SIGNAL_PAGE_SIZE, visibleSignals.length - visibleSignalLimit)} 条
                          </button>
                        </div>
                      )}
                    </div>
                  )}
                </>
              )}

              {contentView === 'creators' && creatorCompareView && !selectedCreator && (
                <div className="p-4">
                  <CreatorComparePanel />
                </div>
              )}

              {contentView === 'creators' && (!creatorCompareView || Boolean(selectedCreator)) && (
                selectedCreator ? (
                  <div>
                    <div className="flex flex-wrap items-center gap-3 border-b border-brand-border px-4 py-3">
                      <button type="button" className="ui-btn" onClick={() => setSelectedCreatorKey('')}>
                        <Folder className="h-3.5 w-3.5" />
                        返回博主文件夹
                      </button>
                      <div className="min-w-0 flex-1">
                        <h2 className="truncate text-sm font-bold text-brand-fg">{selectedCreator.name}</h2>
                        <p className="mt-0.5 truncate text-xs text-brand-muted">
                          共 {selectedCreator.latest_posts.length} 条作品 · 本地封面 {selectedCreator.latest_posts.filter((p) => p.cover_path).length} · 原声音频 {selectedCreator.latest_posts.filter((p) => p.audio_path).length} · 最近抓取 {selectedCreator.last_pages || 0} 页
                        </p>
                      </div>
                      {selectedCreator.platform === 'douyin' && (() => {
                        const key = `${selectedCreator.platform}-${selectedCreator.user_id}`;
                        const busy = archivingCreator === key;
                        const done = archiveDone === key;
                        return (
                        <button
                          type="button"
                          data-testid="creator-folder-update"
                          data-state={busy ? 'busy' : done ? 'done' : 'idle'}
                          className="ui-btn ui-btn-primary group/update transition active:scale-[0.97]"
                          title={busy ? '正在归档…' : done ? '已是最新' : '重新抓取并补齐缺失的封面和音频；已有文件会跳过'}
                          disabled={busy}
                          onClick={() => void archiveCreator(selectedCreator)}
                        >
                          {busy
                            ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                            : done
                              ? <CheckCircle2 className="h-3.5 w-3.5" />
                              : <RefreshCw className="h-3.5 w-3.5 transition-transform duration-300 group-hover/update:rotate-90" />}
                          {busy ? '更新中…' : done ? '已更新' : '更新'}
                        </button>
                        );
                      })()}
                      {selectedCreator.platform === 'douyin' && (
                        <button
                          type="button"
                          data-testid="creator-auto-archive"
                          data-enabled={String(autoArchiveEnabled)}
                          aria-pressed={autoArchiveEnabled}
                          className={`ui-btn active:scale-[0.97] ${autoArchiveEnabled ? 'border-emerald-300 text-emerald-700 hover:bg-emerald-50' : ''}`}
                          title="每 24 小时自动增量归档一次，会消耗 TikHub 额度"
                          disabled={autoArchiveSaving}
                          onClick={() => void toggleCreatorAutoArchive(selectedCreator)}
                        >
                          {autoArchiveSaving
                            ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                            : <Clock className="h-3.5 w-3.5" />}
                          自动更新 · {autoArchiveEnabled ? '开' : '关'}
                        </button>
                      )}
                      {archiveFailureTotal > 0 && (
                        <button
                          type="button"
                          data-testid="creator-folder-failures"
                          data-failures={archiveFailureTotal}
                          className="ui-btn border-amber-300 text-amber-700 hover:bg-amber-50 active:scale-[0.97]"
                          title="上次归档有封面/音频没下载成功；点开看是谁、为什么，并可一键重试"
                          onClick={() => setArchiveFailuresOpen(true)}
                        >
                          <AlertTriangle className="h-3.5 w-3.5" />
                          失败 {archiveFailureTotal} 项
                        </button>
                      )}
                      <button
                        type="button"
                        data-testid="creator-folder-open-finder"
                        className="ui-btn"
                        onClick={() => void handleOpenFolder(selectedCreator)}
                        title="在系统文件管理器中打开此文件夹"
                      >
                        <FolderOpen className="h-3.5 w-3.5" />
                        打开文件夹
                      </button>
                    </div>
                    {(() => {
                      const key = `${selectedCreator.platform}-${selectedCreator.user_id}`;
                      const progress = archiveProgress[key];
                      if (archivingCreator !== key || !progress) return null;
                      const phase = progress.phase;
                      const isPosts = phase === 'posts' || phase === '';
                      const hasMediaTotal = progress.covers_total > 0;
                      // 总量未知（拉列表阶段 / 媒体总量还没算出来）时不伪造百分比。
                      const indeterminate = isPosts || (phase !== '' && !hasMediaTotal);
                      const ratio = hasMediaTotal ? progress.covers_done / progress.covers_total : 0;
                      const label = phase === ''
                        ? '正在启动归档…'
                        : phase === 'posts'
                          ? `正在拉取作品列表… 已获取 ${progress.posts_fetched} 条`
                          : hasMediaTotal
                            ? `正在下载封面与音频… ${progress.covers_done}/${progress.covers_total}`
                            : '正在准备封面与音频下载…';
                      return (
                        <div data-testid="archive-progress" className="border-b border-brand-border bg-black/[0.012] px-4 py-2">
                          <div className="flex items-center justify-between text-[10px] text-brand-muted">
                            <span>{label}</span>
                            <span className="tabular-nums">{indeterminate ? '进行中' : `${Math.round(ratio * 100)}%`}</span>
                          </div>
                          <div className="mt-1 h-1 w-full overflow-hidden rounded-full bg-black/[0.08]">
                            {indeterminate ? (
                              <div className="h-full w-1/3 rounded-full bg-brand-accent animate-archive-slide" />
                            ) : (
                              <div
                                className="h-full rounded-full bg-brand-accent transition-[width] duration-300 ease-out"
                                style={{ width: `${Math.min(100, Math.max(2, ratio * 100))}%` }}
                              />
                            )}
                          </div>
                        </div>
                      );
                    })()}
                    <div className="flex items-center justify-between gap-3 border-b border-brand-border bg-black/[0.018] px-4 py-2">
                      <p className="truncate font-mono text-[10px] text-brand-muted flex-1">{selectedCreator.folder_path}</p>
                      <button
                        type="button"
                        onClick={() => void handleCopyPath(selectedCreator.folder_path)}
                        className="inline-flex items-center gap-1 rounded px-2 py-0.5 text-[10px] text-brand-muted hover:bg-black/5 hover:text-brand-fg transition shrink-0"
                        title="复制本地路径到剪贴板"
                      >
                        <Copy className="h-3 w-3" />
                        复制路径
                      </button>
                    </div>
                    {selectedCreator.latest_posts.length === 0 ? (
                      <EmptyState icon={Film} title="文件夹里还没有近期作品" message="到“抓取”刷新该博主；原始分页快照会持续保存在这个文件夹中。" />
                    ) : (
                      <CreatorPostGrid
                        selectedCreator={selectedCreator}
                        creatorPostQuery={creatorPostQuery}
                        setCreatorPostQuery={setCreatorPostQuery}
                        creatorPostMediaFilter={creatorPostMediaFilter}
                        setCreatorPostMediaFilter={setCreatorPostMediaFilter}
                        creatorPostSort={creatorPostSort}
                        setCreatorPostSort={setCreatorPostSort}
                        creatorSearchRef={creatorSearchRef}
                        activeAudio={activeAudio}
                        audioPlaying={audioPlaying}
                        toggleAudio={toggleAudio}
                        setSelectedPostDetail={setSelectedPostDetail}
                        openSource={openSource}
                        renderEmpty={(query) => (
                          <EmptyState
                            icon={Search}
                            title="没有匹配的作品"
                            message={query ? `未找到包含 “${query}” 的作品` : '当前筛选条件下没有作品'}
                          />
                        )}
                      />
                    )}
                  </div>
                ) : (!data?.creators.length && !addCreatorOpen) ? (
                  <EmptyState
                    icon={Folder}
                    title="还没有博主文件夹"
                    message="添加博主后，会立即创建独立本地文件夹。"
                    actionLabel="添加博主"
                    onAction={() => setAddCreatorOpen(true)}
                  />
                ) : (
                  <div>
                    <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border px-3.5 py-2.5">
                      <div className="min-w-0">
                        <h3 className="text-xs font-semibold text-brand-fg">博主监控</h3>
                        <p className="mt-0.5 text-[11px] text-brand-muted">添加名单不会联网；刷新资料和作品前会单独确认</p>
                      </div>
                      <button
                        type="button"
                        data-testid="creator-add-toggle"
                        className="ui-btn ui-btn-primary shrink-0"
                        onClick={() => setAddCreatorOpen(true)}
                      >
                        <Plus className="h-3.5 w-3.5" />
                        添加监控博主
                      </button>
                    </div>
                    <div className="grid gap-3 p-4 sm:grid-cols-2 xl:grid-cols-3">
                    {data?.creators.map((creator) => (
                      <button
                        key={`${creator.platform}-${creator.user_id}`}
                        type="button"
                        data-testid={`creator-folder-${creator.platform}-${creator.user_id}`}
                        onClick={() => setSelectedCreatorKey(`${creator.platform}-${creator.user_id}`)}
                        className="group rounded-xl border border-brand-border bg-brand-surface p-4 text-left transition hover:border-brand-accent hover:shadow-sm"
                      >
                        <div className="flex items-start gap-3">
                          {creator.avatar_url ? (
                            <div className="relative shrink-0">
                              <img
                                src={creator.avatar_url}
                                alt={creator.name}
                                className="h-11 w-11 rounded-full object-cover border border-brand-border bg-black/[0.04]"
                              />
                              <span className="absolute -bottom-1 -right-1 rounded-full bg-brand-surface p-0.5 shadow-xs border border-brand-border">
                                <PlatformIcon platform={creator.platform} className="h-3 w-3" />
                              </span>
                            </div>
                          ) : (
                            <span className="rounded-lg bg-brand-surface-raised p-2 text-brand-accent">
                              <Folder className="h-5 w-5" />
                            </span>
                          )}
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-1.5">
                              {!creator.avatar_url && <PlatformIcon platform={creator.platform} className="h-4 w-4" />}
                              <h3 className="truncate text-sm font-bold text-brand-fg">{creator.name}</h3>
                            </div>
                            {creator.signature && (
                              <p className="mt-0.5 line-clamp-1 text-[11px] text-brand-fg-subtle">{creator.signature}</p>
                            )}
                            <p className="mt-1 text-xs text-brand-muted">近 {creator.lookback_days} 天 {creator.latest_posts.length} 条 · {creator.snapshot_count} 个快照</p>
                            <div className="mt-3 grid grid-cols-2 gap-x-3 gap-y-2 border-y border-brand-border/70 py-2 text-[10px]">
                              <span className="text-brand-muted">粉丝 <strong className="text-brand-fg">{compactNumber(creator.followers)}</strong></span>
                              <span className="text-brand-muted">较上次 <strong className={creator.followers_delta < 0 ? 'text-rose-600' : 'text-emerald-700'}>{formatDelta(creator.followers_delta)}</strong></span>
                              <span className="text-brand-muted">新增作品 <strong className="text-brand-fg">{creator.new_posts}</strong></span>
                              <span className="text-brand-muted">互动 <strong className={creator.engagement_delta < 0 ? 'text-rose-600' : 'text-emerald-700'}>{formatDelta(creator.engagement_delta)}</strong></span>
                            </div>
                            <div className="mt-2 flex items-center justify-between text-[9px] text-brand-muted font-mono">
                              <span className="truncate max-w-[200px]">📁 {creator.folder_path}</span>
                              <span className="text-brand-accent font-sans font-medium">打开文件夹 →</span>
                            </div>
                          </div>
                        </div>
                      </button>
                    ))}
                    </div>
                  </div>
                )
              )}
            </section>
          )}

          {section === 'collect' && (
            <section className="ui-card overflow-hidden">
              <SectionHeader
                title="博主监控"
                subtitle="添加名单不会联网；刷新资料和作品前会单独确认"
                action={(
                  <button
                    type="button"
                    data-testid="creator-add-toggle-collect"
                    className="ui-btn"
                    onClick={() => setAddCreatorOpen(true)}
                  >
                    <Plus className="h-3.5 w-3.5" />
                    添加监控博主
                  </button>
                )}
              />
              {!data?.creators.length ? (
                <EmptyState icon={UserRoundSearch} title="还没有监控博主" message="先加入抖音或小红书博主，再明确点击“刷新快照”。" />
              ) : (
                <div className="grid gap-3 p-4 md:grid-cols-2 2xl:grid-cols-3">
                  {data.creators.map((creator) => {
                    const key = `${creator.platform}-${creator.user_id}`;
                    return (
                      <article key={key} className="rounded-xl border border-brand-border bg-brand-surface p-4">
                        <div className="flex items-start gap-3">
                          <PlatformIcon platform={creator.platform} className="h-9 w-9 shrink-0" />
                          <div className="min-w-0 flex-1">
                            <div className="flex items-start justify-between gap-2">
                              <div className="min-w-0">
                                <h3 className="truncate text-xs font-bold text-brand-fg">{creator.name}</h3>
                                <p className="mt-0.5 truncate text-[10px] text-brand-muted">ID {creator.user_id}</p>
                              </div>
                              <span className={`rounded-full border px-2 py-0.5 text-[9px] font-semibold ${statusClass(creator.state)}`}>
                                {stateLabels[creator.state] || creator.state}
                              </span>
                            </div>
                            <p className="mt-2 truncate font-mono text-[9px] text-brand-muted">{creator.folder_path}</p>
                          </div>
                        </div>
                        <div className="mt-3 grid grid-cols-4 gap-2 rounded-lg bg-black/[0.025] p-2 text-center">
                          <MiniStat label="粉丝" value={compactNumber(creator.followers)} compact />
                          <MiniStat label="粉丝增量" value={formatDelta(creator.followers_delta)} compact />
                          <MiniStat label="新增作品" value={creator.new_posts} compact />
                          <MiniStat label="互动增量" value={formatDelta(creator.engagement_delta)} compact />
                        </div>
                        <div className="mt-4 grid grid-cols-2 gap-2">
                          <label>
                            <span className="mb-1 block text-[10px] font-semibold text-brand-muted">本地时间范围</span>
                            <select
                              value={creator.lookback_days}
                              onChange={(event) => updateCreatorOptions(creator, { lookback_days: Number(event.target.value) as 7 | 14 | 30 })}
                              disabled={creatorConfigSaving === key}
                              className="w-full rounded-lg border border-brand-border bg-brand-surface px-2.5 py-2 text-xs"
                            >
                              <option value={7}>近 7 天</option>
                              <option value={14}>近 14 天</option>
                              <option value={30}>近 30 天</option>
                            </select>
                          </label>
                          <label>
                            <span className="mb-1 block text-[10px] font-semibold text-brand-muted">最多抓取页数</span>
                            <select
                              value={creator.platform === 'douyin' ? creator.max_pages : 1}
                              onChange={(event) => updateCreatorOptions(creator, { max_pages: Number(event.target.value) as 1 | 2 | 3 | 5 })}
                              disabled={creator.platform !== 'douyin' || creatorConfigSaving === key}
                              className="w-full rounded-lg border border-brand-border bg-brand-surface px-2.5 py-2 text-xs disabled:bg-black/[0.03]"
                            >
                              {(creator.platform === 'douyin' ? [1, 2, 3, 5] : [1]).map((pages) => <option key={pages} value={pages}>{pages} 页（最多 {pages * 20} 条）</option>)}
                            </select>
                          </label>
                          <label>
                            <span className="mb-1 block text-[10px] font-semibold text-brand-muted">每页数量</span>
                            <select value={20} disabled className="w-full rounded-lg border border-brand-border bg-black/[0.03] px-2.5 py-2 text-xs">
                              <option value={20}>20 条（官方建议）</option>
                            </select>
                          </label>
                          <label>
                            <span className="mb-1 block text-[10px] font-semibold text-brand-muted">作品排序</span>
                            <select
                              value={creator.sort_type}
                              onChange={(event) => updateCreatorOptions(creator, { sort_type: Number(event.target.value) as 0 | 1 })}
                              disabled={creator.platform !== 'douyin' || creatorConfigSaving === key}
                              className="w-full rounded-lg border border-brand-border bg-brand-surface px-2.5 py-2 text-xs disabled:bg-black/[0.03]"
                            >
                              <option value={0}>最新排序</option>
                              <option value={1}>最热排序</option>
                            </select>
                          </label>
                        </div>
                        <p className="mt-3 text-[10px] leading-relaxed text-brand-muted">
                          {creator.platform === 'douyin'
                            ? `官方接口没有“近 ${creator.lookback_days} 天”参数：从 max_cursor=0 开始，每页 count=20，最多 ${creator.max_pages} 页；本地按 create_time 截断，遇到时间边界或 has_more=0 提前停止。`
                            : '小红书当前只抓取 1 页；本轮未启用未验证的多页参数。时间范围在本地展示层过滤。'}
                        </p>
                        <div className="mt-3 flex items-center gap-2">
                          <button type="button" className="ui-btn ui-btn-primary flex-1" disabled={runningCreator === key || creatorConfigSaving === key} onClick={() => runCreator(creator)}>
                            {runningCreator === key ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                            抓取博主
                          </button>
                          {creator.platform === 'douyin' && (
                            <button
                              type="button"
                              data-testid={`creator-archive-${key}`}
                              className="ui-btn flex-1"
                              title="一次拉完全部作品，并把封面和音频下载到本地文件夹；可反复点击更新"
                              disabled={archivingCreator === key || creatorConfigSaving === key}
                              onClick={() => archiveCreator(creator)}
                            >
                              {archivingCreator === key ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <FolderOpen className="h-3.5 w-3.5" />}
                              {archivingCreator === key ? '归档中…' : '抓取全部作品'}
                            </button>
                          )}
                          <button
                            type="button"
                            className="ui-btn"
                            onClick={() => {
                              setSection('content');
                              setContentView('creators');
                              setSelectedCreatorKey(key);
                            }}
                          >
                            <FolderOpen className="h-3.5 w-3.5" />
                            文件夹
                          </button>
                          <button
                            type="button"
                            className="ui-btn text-rose-600"
                            disabled={removingCreator === key}
                            onClick={() => removeCreator(creator.platform, creator.user_id, creator.name)}
                            aria-label={`移除博主 ${creator.name}`}
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

          {section === 'collect' && unassignedTerms.length > 0 && (
            <section data-testid="trend-radar-unassigned" className="ui-card space-y-3 p-4">
              <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                <div>
                  <h2 className="flex items-center gap-2 text-xs font-bold text-brand-fg">
                    <Lightbulb className="h-4 w-4 text-brand-accent" />
                    历史记录未归属词
                  </h2>
                  <p className="mt-1 text-[10px] text-brand-muted">
                    这些搜索词已经抓过数据，但还没归入任何赛道。选中后加入现有赛道或新建赛道，归属立即重算；不会重新抓取。
                  </p>
                </div>
                <span className="ui-lining-nums text-[10px] font-semibold text-brand-muted">
                  已选 {selectedUnassignedTerms.length} / {unassignedTerms.length}
                </span>
              </div>
              <div className="flex flex-wrap gap-2">
                {unassignedTerms.map((term) => {
                  const selected = selectedUnassignedTerms.includes(term);
                  return (
                    <button
                      key={term}
                      type="button"
                      aria-pressed={selected}
                      onClick={() => toggleUnassignedTerm(term)}
                      className={`rounded-full border px-2.5 py-1 text-[10px] font-semibold ${selected ? 'border-gray-900 bg-brand-accent text-brand-on-accent' : 'border-brand-border bg-brand-surface text-brand-fg-subtle hover:border-brand-accent hover:text-brand-accent'}`}
                    >
                      {term}
                    </button>
                  );
                })}
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <label className="block">
                  <span className="mb-1 block text-[9px] font-bold text-brand-muted">归入</span>
                  <select
                    value={effectiveAssignTarget}
                    onChange={(event) => setAssignTarget(event.target.value)}
                    className="w-full rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-[11px] outline-none focus:border-brand-accent"
                  >
                    <option value="__new__">＋ 新建赛道</option>
                    {assignableNiches.map((niche) => (
                      <option key={niche.id} value={niche.id}>{niche.name}</option>
                    ))}
                  </select>
                </label>
                {creatingAssignNiche ? (
                  <div className="grid gap-2">
                    <label className="block">
                      <span className="mb-1 block text-[9px] font-bold text-brand-muted">新赛道名称</span>
                      <input
                        value={assignNewNicheName}
                        onChange={(event) => setAssignNewNicheName(event.target.value)}
                        placeholder="例如：AI 降本"
                        className="w-full rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-[11px] outline-none focus:border-brand-accent"
                      />
                    </label>
                    <label className="block">
                      <span className="mb-1 block text-[9px] font-bold text-brand-muted">说明（可选）</span>
                      <input
                        value={assignNewNicheDescription}
                        onChange={(event) => setAssignNewNicheDescription(event.target.value)}
                        placeholder="这个赛道关注什么"
                        className="w-full rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-[11px] outline-none focus:border-brand-accent"
                      />
                    </label>
                  </div>
                ) : (
                  <p className="self-end text-[10px] leading-relaxed text-brand-muted">
                    现有赛道的搜索词和监测平台保持不变，只追加选中的词。
                  </p>
                )}
              </div>
              <div className="flex justify-end">
                <button
                  type="button"
                  className="ui-btn ui-btn-primary"
                  disabled={assigningKeywords || selectedUnassignedTerms.length === 0}
                  onClick={assignUnassignedKeywords}
                >
                  {assigningKeywords ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
                  归池并重算归属
                </button>
              </div>
            </section>
          )}

          {section === 'collect' && (
            <div className="grid gap-4 lg:grid-cols-2">
              {(data?.niches ?? []).filter((niche) => niche.state !== 'archived').length === 0 && (
                <section className="ui-card lg:col-span-2">
                  <EmptyState
                    icon={Radar}
                    title="还没有自定义赛道"
                    message="每个人关注的方向都不同。新建赛道后，再自行添加搜索词和监测平台。"
                    actionLabel="新建第一个赛道"
                    onAction={addNiche}
                  />
                </section>
              )}
              {(data?.niches ?? []).filter((niche) => niche.state !== 'archived').map((niche) => (
                <article key={niche.id} className="ui-card overflow-hidden">
                  <div className="border-b border-brand-border p-4">
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <div className="flex items-center gap-2">
                          <h2 className="text-sm font-bold text-brand-fg">{niche.name}</h2>
                          <span className={`rounded-full border px-2 py-0.5 text-[9px] font-semibold ${statusClass(niche.state)}`}>
                            {stateLabels[niche.state] || niche.state}
                          </span>
                        </div>
                        <p className="mt-1 text-[10px] text-brand-muted">{niche.description}</p>
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
                    <div>
                      <div className="mb-2 text-[9px] font-bold uppercase tracking-[0.12em] text-brand-muted">常用搜索词</div>
                      <div className="flex flex-wrap gap-2">
                        {niche.search_terms.map((term) => (
                          <span key={term} className="rounded-full border border-brand-border bg-black/[0.025] px-2.5 py-1 text-[10px] font-semibold text-brand-fg-subtle">
                            {term}
                          </span>
                        ))}
                      </div>
                    </div>
                    <div className="flex flex-col gap-2 sm:flex-row">
                      <select
                        value={runTerms[niche.id] || niche.search_terms[0]}
                        onChange={(event) => setRunTerms((current) => ({ ...current, [niche.id]: event.target.value }))}
                        className="min-w-36 rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-xs"
                        aria-label={`${niche.name}搜索词`}
                      >
                        {niche.search_terms.map((term) => (
                          <option key={term} value={term}>{term}</option>
                        ))}
                        {niche.search_terms.length > 1 && <option value="__all__">全部（{niche.search_terms.length} 次调用）</option>}
                      </select>
                      <select
                        value={runPlatforms[niche.id] || niche.platforms[0]}
                        onChange={(event) => setRunPlatforms((current) => ({ ...current, [niche.id]: event.target.value }))}
                        className="min-w-32 rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-[10px]"
                      >
                        {niche.platforms.map((platform) => (
                          <option key={platform} value={platform}>{platformOptions.find((item) => item.id === platform)?.name ?? platform}</option>
                        ))}
                      </select>
                      <button
                        type="button"
                        className="ui-btn ui-btn-primary flex-1"
                        disabled={!niche.enabled || runningNiche === niche.id}
                        onClick={() => runNiche(niche)}
                      >
                        {runningNiche === niche.id ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Search className="h-3.5 w-3.5" />}
                        抓取
                      </button>
                    </div>
                    {(runPlatforms[niche.id] || niche.platforms[0]) === 'douyin' && (
                      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4" data-testid={`douyin-search-options-${niche.id}`}>
                        <label>
                          <span className="mb-1 block text-xs text-brand-muted">发布时间</span>
                          <select
                            value={(runOptions[niche.id]?.publish_time ?? '7')}
                            onChange={(event) => updateRunOption(niche.id, 'publish_time', event.target.value)}
                            className="w-full rounded-lg border border-brand-border bg-brand-surface px-2.5 py-2 text-xs"
                          >
                            <option value="0">不限</option>
                            <option value="1">近 1 天</option>
                            <option value="7">近 7 天</option>
                            <option value="180">近半年</option>
                          </select>
                        </label>
                        <label>
                          <span className="mb-1 block text-xs text-brand-muted">排序</span>
                          <select
                            value={(runOptions[niche.id]?.sort_type ?? '2')}
                            onChange={(event) => updateRunOption(niche.id, 'sort_type', event.target.value)}
                            className="w-full rounded-lg border border-brand-border bg-brand-surface px-2.5 py-2 text-xs"
                          >
                            <option value="2">最新发布</option>
                            <option value="1">最多点赞</option>
                            <option value="0">综合排序</option>
                          </select>
                        </label>
                        <label>
                          <span className="mb-1 block text-xs text-brand-muted">时长</span>
                          <select
                            value={(runOptions[niche.id]?.filter_duration ?? '0')}
                            onChange={(event) => updateRunOption(niche.id, 'filter_duration', event.target.value)}
                            className="w-full rounded-lg border border-brand-border bg-brand-surface px-2.5 py-2 text-xs"
                          >
                            <option value="0">不限</option>
                            <option value="0-1">1 分钟内</option>
                            <option value="1-5">1–5 分钟</option>
                            <option value="5-10000">5 分钟以上</option>
                          </select>
                        </label>
                        <label>
                          <span className="mb-1 block text-xs text-brand-muted">内容类型</span>
                          <select
                            value={(runOptions[niche.id]?.content_type ?? '0')}
                            onChange={(event) => updateRunOption(niche.id, 'content_type', event.target.value)}
                            className="w-full rounded-lg border border-brand-border bg-brand-surface px-2.5 py-2 text-xs"
                          >
                            <option value="0">不限</option>
                            <option value="1">视频</option>
                            <option value="2">图片</option>
                            <option value="3">文章</option>
                          </select>
                        </label>
                      </div>
                    )}
                    <div className="flex items-center justify-between gap-3">
                      <p className="text-xs leading-relaxed text-brand-muted">参数来自 TikHub 抖音“视频搜索 V2”官方文档；执行前会显示平台和调用次数。</p>
                      <button
                        type="button"
                        className="ui-btn shrink-0"
                        onClick={() => saveScheduleFromNiche(niche)}
                        disabled={scheduleSaving === niche.id || (runTerms[niche.id] || niche.search_terms[0]) === '__all__'}
                        title={
                          (runPlatforms[niche.id] || niche.platforms[0]) !== 'douyin'
                            ? '定时后台计划目前支持抖音搜索端点，小红书等平台支持单次手动抓取'
                            : '将当前搜索词和参数保存为定时计划'
                        }
                      >
                        {scheduleSaving === niche.id ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
                        存为定时计划
                      </button>
                    </div>
                  </div>
                </article>
              ))}
            </div>
          )}

          {section === 'collect' && (
            <section className="ui-card overflow-hidden">
              <SectionHeader
                title="RSS 收集"
                subtitle="每个源单独决定是否继续抓网页全文；添加或修改源不会自动抓取"
              />
              <form onSubmit={addRSSSource} className="grid gap-3 border-b border-brand-border bg-black/[0.018] p-4 md:grid-cols-[180px_minmax(0,1fr)_auto_auto] md:items-end">
                <label>
                  <span className="mb-1 block text-xs font-semibold text-brand-fg-subtle">名称</span>
                  <input value={rssName} onChange={(event) => setRSSName(event.target.value)} required maxLength={80} placeholder="例如：少数派" className="w-full rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-xs" />
                </label>
                <label>
                  <span className="mb-1 block text-xs font-semibold text-brand-fg-subtle">公开 HTTPS RSS 地址</span>
                  <input value={rssURL} onChange={(event) => setRSSURL(event.target.value)} required type="url" placeholder="https://example.com/feed.xml" className="w-full rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-xs" />
                </label>
                <label className="flex h-9 items-center gap-2 rounded-lg border border-brand-border bg-brand-surface px-3 text-xs font-medium text-brand-fg-subtle">
                  <input type="checkbox" checked={rssFullText} onChange={(event) => setRSSFullText(event.target.checked)} />
                  抓网页全文
                </label>
                <button type="submit" className="ui-btn ui-btn-primary" disabled={rssSaving === 'new'}>
                  {rssSaving === 'new' ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
                  添加
                </button>
              </form>
              {!radarRSS?.sources.length ? (
                <EmptyState icon={CircleDashed} title="还没有 RSS 源" message="添加源只做地址安全校验；需要你再点一次“抓取”。" />
              ) : (
                <div className="divide-y divide-brand-border/70">
                  {radarRSS.sources.map((source) => {
                    const lastRun = radarRSS.runs.find((run) => run.source_id === source.id);
                    return (
                      <article key={source.id} className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center">
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <h3 className="text-xs font-bold text-brand-fg">{source.name}</h3>
                            <span className={`rounded-full px-2 py-0.5 text-[10px] font-semibold ${source.fetch_full_text ? 'bg-brand-surface-raised text-brand-accent' : 'bg-black/[0.04] text-brand-fg-subtle'}`}>
                              {source.fetch_full_text ? 'RSS＋网页全文' : '仅 RSS'}
                            </span>
                            {!source.enabled && <span className="text-[10px] text-brand-muted">已停用</span>}
                          </div>
                          <p className="mt-1 truncate text-xs text-brand-muted">{source.url}</p>
                          {lastRun && (
                            <p className="mt-1 text-xs text-brand-muted">
                              最近 {formatTime(lastRun.created_at)} · {lastRun.item_count} 条 · 全文 {lastRun.full_text_saved} 篇 · {lastRun.duration_ms} ms
                            </p>
                          )}
                        </div>
                        <div className="flex shrink-0 flex-wrap gap-2">
                          <button type="button" className="ui-btn" disabled={rssSaving === source.id} onClick={() => updateRSSSource(source, { fetch_full_text: !source.fetch_full_text })}>
                            {source.fetch_full_text ? '只存 RSS' : '抓网页全文'}
                          </button>
                          <button type="button" className="ui-btn" disabled={rssSaving === source.id} onClick={() => updateRSSSource(source, { enabled: !source.enabled })}>
                            {source.enabled ? '停用' : '启用'}
                          </button>
                          <button type="button" className="ui-btn ui-btn-primary" disabled={!source.enabled || rssSaving === source.id} onClick={() => collectRSS(source)}>
                            {rssSaving === source.id ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                            抓取
                          </button>
                          <button type="button" className="ui-btn text-rose-600" disabled={rssSaving === source.id} onClick={() => removeRSSSource(source)} aria-label={`删除 RSS 源 ${source.name}`}>
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

          {section === 'collect' && (
            <section className="ui-card overflow-hidden">
              <SectionHeader
                title="定时抓取"
                subtitle="独立本地工作器；保存计划默认停用，启用时再次确认真实调用频率"
              />
              {!scheduleFile?.schedules.length ? (
                <EmptyState icon={CircleDashed} title="还没有定时计划" message="在上方选择单个抖音搜索词和参数，再点“存为定时计划”。" />
              ) : (
                <div className="divide-y divide-brand-border/70">
                  {scheduleFile.schedules.map((schedule) => (
                    <article key={schedule.id} className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <h3 className="text-xs font-bold text-brand-fg">{schedule.name}</h3>
                          <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${schedule.enabled ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-black/10 bg-black/[0.035] text-brand-fg-subtle'}`}>
                            {schedule.enabled ? '运行中' : '已停用'}
                          </span>
                        </div>
                        <p className="mt-1 text-xs text-brand-muted">
                          抖音 · {String(schedule.params.keyword || '')} · 每 {schedule.interval_minutes / 60} 小时 · 下次 {schedule.next_run_at ? formatTime(schedule.next_run_at) : '启用后计算'}
                        </p>
                        {schedule.last_call_id ? (
                          <p className="mt-1 text-xs text-brand-muted">上次调用 #{schedule.last_call_id} · {formatTime(schedule.last_run_at)} · {schedule.last_status === 'succeeded' ? '成功' : '失败'}</p>
                        ) : null}
                      </div>
                      <div className="flex shrink-0 gap-2">
                        <button type="button" className={`ui-btn ${schedule.enabled ? '' : 'ui-btn-primary'}`} onClick={() => toggleSchedule(schedule)} disabled={scheduleSaving === schedule.id}>
                          {scheduleSaving === schedule.id ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                          {schedule.enabled ? '停用' : '启用'}
                        </button>
                        <button type="button" className="ui-btn text-rose-600" onClick={() => removeSchedule(schedule)} disabled={scheduleSaving === schedule.id} aria-label={`删除定时计划 ${schedule.name}`}>
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    </article>
                  ))}
                </div>
              )}
            </section>
          )}

          {section === 'collect' && (
            <section className="ui-card overflow-hidden">
              <SectionHeader
                title="视频拆解"
                subtitle="视频原件、封面与 16 kHz 单声道音频全部写入趋势雷达专用本地目录"
                action={(
                  <label className={`ui-btn ui-btn-primary cursor-pointer ${videoUploading ? 'pointer-events-none opacity-60' : ''}`}>
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
                )}
              />
              {!radarVideos?.videos.length ? (
                <EmptyState icon={CircleDashed} title="还没有视频拆解记录" message="选择一条本地视频后，会保留原件并生成封面和本地 Whisper 音频；不会上传云端。" />
              ) : (
                <div className="grid gap-3 p-4 md:grid-cols-2">
                  {radarVideos.videos.map((video) => (
                    <article key={video.id} className="flex gap-3 rounded-xl border border-brand-border bg-brand-surface p-3">
                      <div className="h-28 w-20 shrink-0 overflow-hidden rounded-lg bg-black/[0.04]">
                        {video.cover_url ? (
                          <img src={video.cover_url} alt={`${video.original_name}封面`} className="h-full w-full object-cover" />
                        ) : (
                          <div className="flex h-full items-center justify-center text-xs text-brand-muted">无封面</div>
                        )}
                      </div>
                      <div className="min-w-0 flex-1">
                        <h3 className="truncate text-xs font-bold text-brand-fg">{video.original_name}</h3>
                        <p className="mt-1 text-xs text-brand-muted">{(video.duration_ms / 1000).toFixed(1)} 秒 · {(video.size_bytes / 1024 / 1024).toFixed(1)} MB</p>
                        <p className="mt-2 text-xs leading-relaxed text-brand-accent">{video.transcription_note}</p>
                        <dl className="mt-2 space-y-1 text-[11px] text-brand-muted">
                          <div className="truncate">原件：{video.source_path}</div>
                          <div className="truncate">音频：{video.audio_path || '生成失败，原件仍保留'}</div>
                        </dl>
                      </div>
                    </article>
                  ))}
                </div>
              )}
              <p className="border-t border-brand-border bg-black/[0.018] px-4 py-3 text-xs leading-relaxed text-brand-fg-subtle">
                转写顺序固定为：平台字幕 → 本地 Whisper → 明确确认后云端转写。当前上传视频没有平台字幕；若本机未安装 Whisper，会停在“音频已准备”状态，绝不自动调用云端。
              </p>
            </section>
          )}

          {section === 'collect' && (
            <section id="trend-radar-custom-niches" className="space-y-4">
              <div className="ui-card flex flex-col gap-3 p-4 md:flex-row md:items-center md:justify-between">
                <div>
                  <h2 className="flex items-center gap-2 text-xs font-bold text-brand-fg">
                    <Settings2 className="h-4 w-4 text-brand-accent" />
                    自定义赛道
                  </h2>
                  <p className="mt-1 text-[10px] text-brand-muted">赛道名称、说明、搜索词和平台全部由你定义；保存配置不会自动开始采集。</p>
                </div>
                <div className="flex flex-wrap gap-2">
                  <button type="button" className="ui-btn" onClick={addNiche} disabled={!editedConfig || editedConfig.niches.length >= 8}>
                    <Plus className="h-3.5 w-3.5" />
                    新增赛道
                  </button>
                  <button type="button" className="ui-btn ui-btn-primary" onClick={() => saveConfig()} disabled={!editedConfig || configSaving}>
                    {configSaving ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}
                    保存设置
                  </button>
                </div>
              </div>

              <div className="grid gap-4 lg:grid-cols-2">
                {editedConfig?.niches.length === 0 && (
                  <section className="ui-card lg:col-span-2">
                    <EmptyState
                      icon={Radar}
                      title="从你的第一个赛道开始"
                      message="这里没有预设行业。你可以创建任意赛道，再配置自己的搜索词和监测平台。"
                      actionLabel="新建赛道"
                      onAction={addNiche}
                    />
                  </section>
                )}
                {editedConfig?.niches.map((niche) => (
                  <article key={niche.id} className="ui-card p-4">
                    <div className="flex items-start gap-3">
                      <div className="min-w-0 flex-1 space-y-3">
                        <div className="grid gap-3 sm:grid-cols-2">
                          <label className="block">
                            <span className="mb-1 block text-[9px] font-bold text-brand-muted">赛道名称</span>
                            <input
                              value={niche.name}
                              onChange={(event) => updateNiche(niche.id, { name: event.target.value })}
                              className="w-full rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-[11px] outline-none focus:border-brand-accent"
                            />
                          </label>
                          <label className="block">
                            <span className="mb-1 block text-[9px] font-bold text-brand-muted">状态</span>
                            <button
                              type="button"
                              role="switch"
                              aria-checked={niche.enabled}
                              onClick={() => updateNiche(niche.id, { enabled: !niche.enabled })}
                              className={`flex w-full items-center justify-between rounded-lg border px-3 py-2 text-[10px] font-semibold ${niche.enabled ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-brand-border bg-brand-surface-raised text-brand-muted'}`}
                            >
                              {niche.enabled ? '已启用' : '已停用'}
                              <span className={`h-2 w-2 rounded-full ${niche.enabled ? 'bg-emerald-500' : 'bg-gray-300'}`} />
                            </button>
                          </label>
                        </div>
                        <label className="block">
                          <span className="mb-1 block text-[9px] font-bold text-brand-muted">说明</span>
                          <input
                            value={niche.description ?? ''}
                            onChange={(event) => updateNiche(niche.id, { description: event.target.value })}
                            className="w-full rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-[11px] outline-none focus:border-brand-accent"
                          />
                        </label>
                        <div>
                          <div className="mb-2 text-[9px] font-bold text-brand-muted">监测平台</div>
                          <div className="flex flex-wrap gap-2">
                            {platformOptions.map((platform) => {
                              const selected = niche.platforms.includes(platform.id);
                              return (
                                <button
                                  key={platform.id}
                                  type="button"
                                  onClick={() => togglePlatform(niche.id, platform.id)}
                                  className={`inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-[10px] font-semibold ${selected ? 'border-gray-900 bg-brand-accent text-brand-on-accent' : 'border-brand-border bg-brand-surface text-brand-muted'}`}
                                >
                                  <PlatformIcon platform={platform.id} className="h-4 w-4" />
                                  {platform.name}
                                </button>
                              );
                            })}
                          </div>
                        </div>
                        <div>
                          <div className="mb-2 text-[9px] font-bold text-brand-muted">常用搜索词</div>
                          <div className="flex flex-wrap gap-2">
                            {niche.search_terms.map((term) => (
                              <span key={term} className="inline-flex items-center gap-1 rounded-full border border-brand-border bg-black/[0.025] py-1 pl-2.5 pr-1 text-[10px] font-semibold text-brand-fg-subtle">
                                {term}
                                <button type="button" className="rounded-full p-1 text-brand-muted hover:bg-brand-surface-raised hover:text-rose-600" onClick={() => removeSearchTerm(niche.id, term)} aria-label={`删除搜索词 ${term}`}>
                                  <Trash2 className="h-3 w-3" />
                                </button>
                              </span>
                            ))}
                          </div>
                          {Array.from(new Set((editedConfig?.niches ?? [])
                            .flatMap((item) => item.search_terms)))
                            .filter((term) => !niche.search_terms.includes(term)).length > 0 && (
                            <div className="mt-2 rounded-lg border border-dashed border-brand-border bg-black/[0.018] p-2">
                              <p className="mb-1.5 text-[9px] font-semibold text-brand-muted">从已有标签选择</p>
                              <div className="flex flex-wrap gap-1.5">
                                {Array.from(new Set((editedConfig?.niches ?? [])
                                  .flatMap((item) => item.search_terms)))
                                  .filter((term) => !niche.search_terms.includes(term))
                                  .map((term) => (
                                    <button
                                      key={term}
                                      type="button"
                                      onClick={() => updateNiche(niche.id, { search_terms: [...niche.search_terms, term] })}
                                      className="rounded-full border border-brand-border bg-brand-surface px-2.5 py-1 text-[9px] font-semibold text-brand-fg-subtle hover:border-brand-accent hover:text-brand-accent"
                                    >
                                      + {term}
                                    </button>
                                  ))}
                              </div>
                            </div>
                          )}
                          <div className="mt-2 flex gap-2">
                            <input
                              value={newTerms[niche.id] ?? ''}
                              onChange={(event) => setNewTerms((current) => ({ ...current, [niche.id]: event.target.value }))}
                              onKeyDown={(event) => {
                                if (event.key === 'Enter') {
                                  event.preventDefault();
                                  addSearchTerm(niche.id);
                                }
                              }}
                              placeholder="添加常用搜索词"
                              className="min-w-0 flex-1 rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-[11px] outline-none focus:border-brand-accent"
                            />
                            <button type="button" className="ui-btn" onClick={() => addSearchTerm(niche.id)}>
                              <Plus className="h-3.5 w-3.5" />
                              添加
                            </button>
                          </div>
                        </div>
                      </div>
                      <button type="button" className="rounded-lg p-2 text-brand-muted hover:bg-rose-50 hover:text-rose-600" onClick={() => removeNiche(niche.id)} aria-label={`删除赛道 ${niche.name}`}>
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
                  </article>
                ))}
              </div>
            </section>
          )}

          {section === 'logs' && (
            <section className="ui-card overflow-hidden" data-testid="trend-radar-call-logs">
              <SectionHeader
                title="抓取日志"
                subtitle="每次真实调用的接口、参数、耗时、结果与本地完整响应；仅查看不会再次调用 API"
                action={(
                  <button type="button" className="ui-btn" onClick={() => mutateCallHistory()} disabled={callHistoryLoading}>
                    <RefreshCw className={`h-3.5 w-3.5 ${callHistoryLoading ? 'animate-spin' : ''}`} />
                    重读本地
                  </button>
                )}
              />
              {callHistoryLoading ? (
                <div className="flex min-h-48 items-center justify-center text-xs text-brand-muted">
                  <LoaderCircle className="mr-2 h-4 w-4 animate-spin" />
                  正在读取本地日志…
                </div>
              ) : callHistoryError ? (
                <EmptyState icon={CircleDashed} title="日志读取失败" message="数据库未丢失，请稍后重读本地记录。" />
              ) : !callHistory?.calls.length ? (
                <EmptyState icon={Database} title="还没有抓取日志" message="从“抓取”页明确确认一次调用后，详细记录会长期保存在这里。" />
              ) : (
                <>
                  <div className="grid grid-cols-2 gap-2 border-b border-brand-border bg-black/[0.018] p-4 sm:grid-cols-4">
                    <MiniStat label="最近记录" value={callHistory.count} />
                    <MiniStat label="成功" value={callHistory.calls.filter((call) => call.status === 'succeeded').length} />
                    <MiniStat label="已存数据" value={callHistory.calls.reduce((total, call) => total + call.item_count, 0)} />
                    <MiniStat label="本地文件" value={callHistory.calls.filter((call) => call.raw_path).length} />
                  </div>
                  <div className="divide-y divide-brand-border/70">
                    {callHistory.calls.map((call) => (
                      <CallLogRow
                        key={call.id}
                        call={call}
                        endpoint={endpointDefinitions.get(call.endpoint_id)}
                        raw={rawCalls[call.id]}
                        loadingRaw={loadingRawCall === call.id}
                        onLoadRaw={() => loadRawCall(call)}
                      />
                    ))}
                  </div>
                  <p className="border-t border-brand-border bg-brand-surface-raised px-4 py-3 text-xs leading-relaxed text-brand-fg-subtle">
                    费用只显示官方目录已确认的单价提示；TikHub 官方建议通过“计算价格”接口按端点和每日调用量计算，未查询到的记录标记为“价格未验证”，不会用估算冒充账单。
                  </p>
                </>
              )}
            </section>
          )}
          {section === 'logs' && (
            <section className="ui-card overflow-hidden">
              <SectionHeader title="RSS 抓取日志" subtitle="RSS 原文、可选网页全文和清单均按运行批次长期保存" />
              {!radarRSS?.runs.length ? (
                <EmptyState icon={CircleDashed} title="还没有 RSS 日志" message="从“抓取”页明确执行 RSS 后会显示在这里。" />
              ) : (
                <div className="divide-y divide-brand-border/70">
                  {radarRSS.runs.map((run) => (
                    <article key={run.id} className="px-4 py-3">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="text-xs font-bold text-brand-fg">{run.source_name}</h3>
                        <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${run.status === 'succeeded' ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-rose-200 bg-rose-50 text-rose-700'}`}>
                          {run.status === 'succeeded' ? '成功' : '失败'}
                        </span>
                        <span className="text-xs text-brand-muted">{formatTime(run.created_at)} · {run.duration_ms} ms</span>
                      </div>
                      <p className="mt-1 text-xs text-brand-fg-subtle">
                        RSS {run.item_count} 条 · 网页全文 {run.full_text_saved} 篇 · 全文失败 {run.full_text_failed} 篇
                      </p>
                      <p className="mt-1 break-all font-mono text-[11px] text-brand-muted">{run.feed_path || run.error}</p>
                    </article>
                  ))}
                </div>
              )}
            </section>
          )}
        </>
      )}

      {selectedSignal && (
        <SignalDetailModal
          signal={selectedSignal}
          onClose={() => setSelectedSignal(null)}
          onOpen={openSource}
          onSave={saveSignalAsTopic}
          onWatch={watchSignalCreator}
          onAnalyze={analyzeVideoLookup}
          saving={savingSignal === `${selectedSignal.platform}-${selectedSignal.title}`}
          watching={addingSignalCreator === `${selectedSignal.platform}-${selectedSignal.creator_id}`}
          analyzing={analyzingVideoCallID === selectedSignal.call_id}
        />
      )}

      <AddCreatorDialog
        open={addCreatorOpen}
        onClose={() => setAddCreatorOpen(false)}
        onAdded={() => mutateSummary()}
        onError={(message) => toast.show(message, 'error')}
        onSuccess={(message) => toast.show(message, 'success')}
      />

      {selectedPostDetail && selectedCreator && (
        <PostDetailModal
          post={selectedPostDetail}
          platform={selectedCreator.platform}
          userId={selectedCreator.user_id}
          onClose={() => setSelectedPostDetail(null)}
          onToggleAudio={toggleAudio}
          isAudioPlaying={audioPlaying}
          activeAudioId={activeAudio?.id}
          audioTime={audioTime}
          audioDuration={audioDuration}
          seekAudio={seekAudio}
          onOpenExternal={(url) => openSource(url)}
          onOpenFolder={() => void handleOpenFolder(selectedCreator)}
        />
      )}

      {archiveFailuresOpen && selectedCreator && (
        <ArchiveFailuresPanel
          total={archiveFailureTotal}
          failures={archiveFailures}
          onClose={() => setArchiveFailuresOpen(false)}
          onRetry={retryArchiveFailures}
          onOpenFolder={() => void handleOpenFolder(selectedCreator)}
          onOpenSource={(url) => void openSource(url)}
        />
      )}

      {/* Portal 到 body：脱离带 transform 的 workspace 祖先，否则 fixed 会被
          降级成相对该祖先定位，播放器随长列表滚出视口、进度条点不中。 */}
      {typeof document !== 'undefined' && createPortal(
        <>
          <audio
            ref={audioRef}
            onPlay={() => setAudioPlaying(true)}
            onPause={() => setAudioPlaying(false)}
            onEnded={() => { setAudioPlaying(false); setAudioTime(0); }}
            onTimeUpdate={(e) => setAudioTime(e.currentTarget.currentTime)}
            onLoadedMetadata={(e) => setAudioDuration(e.currentTarget.duration || 0)}
            onDurationChange={(e) => setAudioDuration(e.currentTarget.duration || 0)}
            className="hidden"
          />

          {activeAudio && (
        <div
          data-testid="audio-mini-player"
          className="fixed bottom-5 right-5 z-[60] w-[min(22rem,calc(100vw-2.5rem))] rounded-2xl border border-brand-border bg-brand-surface/95 p-3 shadow-xl backdrop-blur-md animate-in fade-in slide-in-from-bottom-2"
        >
          <div className="flex items-center gap-3">
            <button
              type="button"
              data-testid="audio-player-toggle"
              onClick={() => {
                if (audioPlaying) {
                  audioRef.current?.pause();
                } else {
                  void audioRef.current?.play();
                }
              }}
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-brand-accent text-white shadow-sm transition active:scale-90 hover:opacity-90"
              title={audioPlaying ? '暂停（空格）' : '播放（空格）'}
            >
              {audioPlaying ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4 ml-0.5" />}
            </button>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <Volume2 className="h-3.5 w-3.5 text-brand-accent shrink-0" />
                <span className="text-[10px] font-semibold text-brand-muted">本地原声试听</span>
              </div>
              <p className="truncate text-xs font-bold text-brand-fg mt-0.5" title={activeAudio.title}>{activeAudio.title || '无标题原声'}</p>
            </div>
            <button
              type="button"
              data-testid="audio-player-close"
              onClick={() => {
                if (audioRef.current) {
                  audioRef.current.pause();
                }
                setActiveAudio(null);
                setAudioPlaying(false);
                setAudioTime(0);
                setAudioDuration(0);
              }}
              className="rounded-lg p-1.5 text-brand-muted hover:bg-black/5 hover:text-brand-fg"
              title="关闭试听（Esc）"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          {/* 可点击/拖动的进度条：给「主操作」一个看得见的进度与结果 */}
          <div className="mt-2.5 flex items-center gap-2">
            <span className="w-9 shrink-0 tabular-nums text-right text-[10px] text-brand-muted">{formatAudioTime(audioTime)}</span>
            <div
              data-testid="audio-player-seek"
              role="slider"
              tabIndex={0}
              aria-label="播放进度"
              aria-valuemin={0}
              aria-valuemax={Math.floor(audioDuration) || 0}
              aria-valuenow={Math.floor(audioTime)}
              onPointerDown={(e) => {
                e.currentTarget.setPointerCapture(e.pointerId);
                handleSeekPointer(e);
              }}
              onPointerMove={(e) => {
                if (e.buttons === 1) handleSeekPointer(e);
              }}
              onKeyDown={(e) => {
                if (e.key === 'ArrowRight') { e.preventDefault(); seekAudio((audioTime + 5) / (audioDuration || 1)); }
                if (e.key === 'ArrowLeft') { e.preventDefault(); seekAudio((audioTime - 5) / (audioDuration || 1)); }
              }}
              className="group/seek relative flex h-4 flex-1 cursor-pointer items-center outline-none"
            >
              <div className="h-1 w-full overflow-hidden rounded-full bg-black/[0.08]">
                <div
                  className="h-full rounded-full bg-brand-accent transition-[width] duration-100 ease-linear"
                  style={{ width: `${audioDuration > 0 ? Math.min(100, (audioTime / audioDuration) * 100) : 0}%` }}
                />
              </div>
              <span
                className="absolute h-2.5 w-2.5 -translate-x-1/2 rounded-full bg-brand-accent opacity-0 shadow-sm transition-opacity group-hover/seek:opacity-100 group-focus-visible/seek:opacity-100"
                style={{ left: `${audioDuration > 0 ? Math.min(100, (audioTime / audioDuration) * 100) : 0}%` }}
              />
            </div>
            <span className="w-9 shrink-0 tabular-nums text-[10px] text-brand-muted">{formatAudioTime(audioDuration)}</span>
          </div>
        </div>
          )}
        </>,
        document.body,
      )}
    </div>
  );
};

const MiniStat: React.FC<{ label: string; value: string | number; compact?: boolean }> = ({ label, value, compact }) => (
  <div className="rounded-lg bg-black/[0.025] p-2.5">
    <div className="text-[9px] text-brand-muted">{label}</div>
    <div className={`mt-0.5 font-semibold text-brand-fg ${compact ? 'text-[10px]' : 'text-sm'}`}>{value}</div>
  </div>
);

const SectionHeader: React.FC<{ title: string; subtitle: string; action?: React.ReactNode }> = ({ title, subtitle, action }) => (
  <div className="flex items-center justify-between gap-3 border-b border-brand-border px-4 py-3">
    <div>
      <h2 className="text-xs font-bold text-brand-fg">{title}</h2>
      <p className="mt-0.5 text-[10px] text-brand-muted">{subtitle}</p>
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
    <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-black/[0.035] text-brand-muted">
      <Icon className="h-5 w-5" />
    </div>
    <h3 className="mt-3 text-xs font-bold text-brand-fg">{title}</h3>
    <p className="mt-1 max-w-sm text-[10px] leading-relaxed text-brand-muted">{message}</p>
    {actionLabel && onAction && (
      <button type="button" className="ui-btn ui-btn-primary mt-4" onClick={onAction}>
        <Plus className="h-3.5 w-3.5" />
        {actionLabel}
      </button>
    )}
  </div>
);

const CallLogRow: React.FC<{
  call: TikHubAPICall;
  endpoint?: DataAPIEndpoint;
  raw?: string;
  loadingRaw: boolean;
  onLoadRaw: () => void;
}> = ({ call, endpoint, raw, loadingRaw, onLoadRaw }) => {
  const parameters = parseStoredParameters(call.parameters_json);
  const statusLabel = call.status === 'succeeded'
    ? '成功'
    : call.status === 'running'
      ? '进行中'
      : call.status === 'storage_failed'
        ? '保存失败'
        : '失败';
  const statusStyle = call.status === 'succeeded'
    ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
    : call.status === 'running'
      ? 'border-brand-accent/40 bg-brand-surface-raised text-brand-accent'
      : 'border-rose-200 bg-rose-50 text-rose-700';
  return (
    <details
      className="group px-4 py-3"
      onToggle={(event) => {
        if (event.currentTarget.open && call.raw_path && !raw) onLoadRaw();
      }}
    >
      <summary className="flex cursor-pointer list-none items-start gap-3">
        <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-black/[0.035]">
          <PlatformIcon platform={call.platform_code} className="h-4 w-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs font-bold text-brand-fg">{endpoint?.name || call.endpoint_id}</span>
            <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${statusStyle}`}>{statusLabel}</span>
            <span className="rounded-full bg-black/[0.04] px-2 py-0.5 text-[10px] text-brand-fg-subtle">
              {platformNames[call.platform_code] || call.platform_code}
            </span>
          </div>
          <p className="mt-1 truncate text-xs text-brand-muted">
            #{call.id} · {formatTime(call.started_at)} · {call.duration_ms} ms · {call.item_count} 条 · {(call.response_bytes / 1024).toFixed(1)} KB
          </p>
        </div>
        <span className="shrink-0 text-xs font-semibold text-brand-muted">
          {endpoint?.cost_hint || '价格未验证'}
        </span>
      </summary>
      <div className="mt-3 grid gap-3 pl-11">
        <dl className="grid gap-2 text-xs sm:grid-cols-2">
          <div className="rounded-lg border border-brand-border bg-brand-surface p-3">
            <dt className="text-brand-muted">调用函数</dt>
            <dd className="mt-1 break-all font-mono text-brand-fg">{call.method} {call.path}</dd>
          </div>
          <div className="rounded-lg border border-brand-border bg-brand-surface p-3">
            <dt className="text-brand-muted">本地原始文件</dt>
            <dd className="mt-1 break-all font-mono text-brand-fg">{call.raw_path || '未生成'}</dd>
          </div>
          <div className="rounded-lg border border-brand-border bg-brand-surface p-3 sm:col-span-2">
            <dt className="text-brand-muted">参数</dt>
            <dd className="mt-1 break-all font-mono text-brand-fg">{JSON.stringify(parameters)}</dd>
          </div>
          {call.error_code && (
            <div className="rounded-lg border border-rose-200 bg-rose-50 p-3 sm:col-span-2">
              <dt className="text-rose-500">错误</dt>
              <dd className="mt-1 text-rose-800">{call.error_code}</dd>
              {(call.provider_code || call.provider_message) && (
                <dd className="mt-1 text-rose-700">
                  {[call.provider_code, call.provider_message].filter(Boolean).join(' · ')}
                </dd>
              )}
            </div>
          )}
          {!call.error_code && call.provider_message && (
            <div className={`rounded-lg border p-3 sm:col-span-2 ${
              call.status === 'succeeded'
                ? 'border-emerald-200 bg-emerald-50'
                : 'border-rose-200 bg-rose-50'
            }`}>
              <dt className={call.status === 'succeeded' ? 'text-emerald-600' : 'text-rose-500'}>
                {call.status === 'succeeded' ? '接口提示' : '错误'}
              </dt>
              <dd className={`mt-1 ${call.status === 'succeeded' ? 'text-emerald-800' : 'text-rose-800'}`}>
                {call.provider_message}
              </dd>
            </div>
          )}
        </dl>
        {call.raw_path && (
          <div className="rounded-lg border border-brand-border bg-black">
            <div className="flex items-center justify-between border-b border-white/10 px-3 py-2 text-xs text-brand-muted">
              <span>完整本地响应</span>
              {loadingRaw && <LoaderCircle className="h-3.5 w-3.5 animate-spin" />}
            </div>
            <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all p-3 text-[11px] leading-relaxed text-gray-200">
              {raw || (loadingRaw ? '正在读取…' : '展开时读取本地文件')}
            </pre>
          </div>
        )}
      </div>
    </details>
  );
};

function hasVideoTranscript(signal: TrendRadarSignal): boolean {
  return signal.content_source === 'platform_caption' || signal.content_source === 'asr_transcript';
}

function videoAnalysisLabel(signal: TrendRadarSignal): string {
  const status = signal.analysis_status;
  if (status === 'succeeded') return hasVideoTranscript(signal) ? 'LLM 内容分析' : 'LLM 元数据分析';
  if (status === 'running') return '正在分析';
  if (status === 'failed') return '拆解失败';
  return '待分析';
}

const VideoLookupHistoryRow: React.FC<{
  signal: TrendRadarSignal;
  onDetail: (signal: TrendRadarSignal) => void;
}> = ({ signal, onDetail }) => (
  <article
    onClick={() => onDetail(signal)}
    className="px-4 py-3 cursor-pointer transition hover:bg-brand-surface-raised/80"
  >
    <div
      aria-label={`查看视频拆解：${signal.title}`}
      className="flex w-full items-center gap-3 rounded-xl text-left"
    >
      <span className="relative h-24 w-36 sm:w-40 shrink-0 overflow-hidden rounded-xl border border-brand-border bg-black">
        {signal.cover_url ? (
          <img src={signal.cover_url ? `${signal.cover_url}?t=20261024` : ''} alt={`${signal.title}封面`} loading="lazy" className="h-full w-full object-cover" />
        ) : (
          <span className="flex h-full w-full items-center justify-center">
            <PlatformIcon platform={signal.platform} className="h-8 w-8 text-white/50" />
          </span>
        )}
        <span className="absolute bottom-1 left-1 rounded bg-black/80 px-1.5 py-0.5 text-[8px] font-bold text-white">
          {signal.analysis_status === 'succeeded' ? (hasVideoTranscript(signal) ? '含逐字稿' : '仅元数据') : '待处理'}
        </span>
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-2">
          <span
            data-testid={`video-lookup-platform-${signal.platform}`}
            className="inline-flex items-center gap-1.5 rounded-full bg-brand-accent px-2.5 py-1 text-[9px] font-bold text-brand-on-accent"
          >
            <PlatformIcon platform={signal.platform} className="h-3.5 w-3.5" />
            {platformNames[signal.platform] || signal.platform}
          </span>
          <span className="rounded-full bg-emerald-50 px-2 py-0.5 text-[9px] font-bold text-emerald-700">本地已保存</span>
          <span className={`rounded-full px-2 py-0.5 text-[9px] font-bold ${
            signal.analysis_status === 'succeeded'
              ? 'bg-emerald-50 text-emerald-700'
              : signal.analysis_status === 'failed'
                ? 'bg-rose-50 text-rose-700'
                : 'bg-brand-surface-raised text-brand-accent'
          }`}>
            {videoAnalysisLabel(signal)}
          </span>
          {signal.call_id && <span className="text-[9px] text-brand-muted">调用 #{signal.call_id}</span>}
          {signal.occurrences > 1 && <span className="text-[9px] text-brand-muted">共输入 {signal.occurrences} 次</span>}
        </span>
        <span className="mt-2 block line-clamp-2 text-xs font-bold leading-relaxed text-brand-fg">{signal.title}</span>
        <span className="mt-1 block truncate text-[10px] text-brand-muted">
          {signal.creator || '作者未提供'} · 输入 {signal.search_term || '视频详情'} · {formatTime(signal.observed_at)}
        </span>
        <span className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[9px] text-brand-muted">
          <span>播放 {compactNumber(signal.play)}</span>
          <span>点赞 {compactNumber(signal.likes)}</span>
          <span>收藏 {compactNumber(signal.collects)}</span>
          <span>分享 {compactNumber(signal.shares)}</span>
        </span>
      </span>
      <span className="shrink-0 rounded-lg bg-black/[0.035] px-2.5 py-1.5 text-[10px] font-semibold text-brand-fg-subtle">
        查看拆解
      </span>
    </div>
  </article>
);


const SignalRow: React.FC<{
  signal: TrendRadarSignal;
  rank?: number;
  onOpen: (url?: string) => void;
  onSave: (signal: TrendRadarSignal) => void;
  onDetail: (signal: TrendRadarSignal) => void;
  saving: boolean;
}> = ({ signal, rank, onOpen, onSave, onDetail, saving }) => {
  return (
    <article
      data-testid={`trend-signal-${signal.call_id || `${signal.platform}-${signal.title}`}`}
      onClick={() => onDetail(signal)}
      className="group flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-4 px-4 py-3 cursor-pointer transition-all duration-150 hover:hover:bg-brand-surface-raised hover:shadow-sm"
    >
      {/* 左侧：排名勋章 + 9:16 竖屏大封面 */}
      <div className="flex items-center gap-3 shrink-0">
        {rank !== undefined ? (
          <span
            className={`inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-xl text-xs font-black shadow-sm ${
              rank === 1
                ? 'bg-brand-accent text-brand-on-accent ring-2 ring-[color-mix(in_srgb,var(--brand-fg)_30%,transparent)]'
                : rank === 2
                  ? 'bg-brand-surface-raised text-brand-fg ring-2 ring-[color-mix(in_srgb,var(--brand-fg)_18%,transparent)]'
                  : rank === 3
                    ? 'bg-brand-surface-raised text-brand-fg ring-2 ring-[color-mix(in_srgb,var(--brand-fg)_10%,transparent)]'
                    : 'bg-brand-surface-raised text-brand-muted font-bold'
            }`}
          >
            {rank}
          </span>
        ) : (
          <span className="w-8 shrink-0" />
        )}

        <div
          aria-label={`查看作品详情：${signal.title}`}
          className="relative aspect-[9/16] h-32 sm:h-40 shrink-0 overflow-hidden rounded-xl border border-brand-border bg-black text-left shadow-sm transition group-hover:scale-[1.02] group-hover:shadow"
        >
          {signal.cover_url ? (
            <img
              src={signal.cover_url ? `${signal.cover_url}?t=20261024` : ''}
              alt={signal.title}
              loading="lazy"
              className="h-full w-full object-cover"
            />
          ) : (
            <div className="flex h-full w-full items-center justify-center bg-brand-surface-raised">
              <PlatformIcon platform={signal.platform} className="h-8 w-8 text-white/50" />
            </div>
          )}
          {/* 封面左上角浮标 */}
          <span
            data-testid={`trend-radar-platform-${signal.platform}`}
            className="absolute left-1.5 top-1.5 inline-flex items-center gap-1 rounded-md bg-black/80 px-1.5 py-0.5 text-[9px] font-bold text-white backdrop-blur-sm"
          >
            <PlatformIcon platform={signal.platform} className="h-3 w-3" />
            {platformNames[signal.platform] || signal.platform}
          </span>
        </div>
      </div>
      {/* 中间：标题、作者、赛道、摘要 */}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="rounded bg-brand-surface-raised px-2 py-0.5 text-[10px] font-bold text-brand-accent">
            {signal.niche_name}
          </span>
          <span className="rounded bg-brand-surface-raised px-2 py-0.5 text-[10px] font-semibold text-brand-fg-subtle">
            #{signal.search_term}
          </span>
          {signal.is_new && (
            <span className="rounded-full bg-emerald-50 px-2 py-0.2 text-[9px] font-bold text-emerald-700">
              新发现
            </span>
          )}
          <span className="text-[11px] font-semibold text-brand-fg-subtle">
            @{signal.creator || '匿名创作者'}
          </span>
          <span
            className="text-[10px] text-brand-muted"
            title={`抓取：${formatTime(signal.observed_at)}`}
          >
            发布 {signal.published_at ? formatTime(signal.published_at) : '读不到'}
          </span>
        </div>

        <h3
          className="mt-2 text-sm font-extrabold leading-snug text-brand-fg transition group-hover:text-brand-accent sm:text-base"
        >
          {signal.title}
        </h3>

        {signal.content_text && signal.content_text !== signal.title && (
          <p
            className="mt-1 line-clamp-2 text-[10px] leading-relaxed text-brand-fg-subtle"
            title={signal.content_text}
          >
            {signal.content_text.replace(/\s+/g, ' ').trim()}
          </p>
        )}
      </div>

      {/* 右侧：超大醒目互动数据看板 + 操作按钮 */}
      <div className="flex shrink-0 items-center justify-between md:justify-end gap-4 border-t border-brand-border pt-3 md:border-t-0 md:pt-0 md:self-stretch">
        {/* 数据块：大字体强调 */}
        <div className="flex items-center gap-4 sm:gap-6">
          <div className="text-center">
            <span className="block text-[10px] font-medium text-brand-muted">获赞</span>
            <span className="ui-lining-nums text-lg sm:text-xl font-black text-brand-fg">
              {compactNumber(signal.likes)}
            </span>
          </div>

          <div className="text-center">
            <span className="block text-[10px] font-medium text-brand-muted">收藏</span>
            <span className="ui-lining-nums text-base sm:text-lg font-extrabold text-brand-warning">
              {compactNumber(signal.collects)}
            </span>
          </div>

          <div className="text-center">
            <span className="block text-[10px] font-medium text-brand-muted">分享</span>
            <span className="ui-lining-nums text-sm sm:text-base font-bold text-brand-muted">
              {compactNumber(signal.shares)}
            </span>
          </div>
        </div>

        {/* 操作区 */}
        <div className="flex items-center gap-1.5 shrink-0 whitespace-nowrap pl-3 border-l border-brand-border" onClick={(e) => e.stopPropagation()}>
          <button
            type="button"
            className="inline-flex shrink-0 items-center gap-1 px-2 py-1.5 text-[11px] font-medium text-brand-fg-subtle transition hover:text-brand-fg disabled:opacity-40"
            onClick={() => onSave(signal)}
            disabled={saving}
            title="存为创作选题"
          >
            {saving ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Lightbulb className="h-3.5 w-3.5" />}
            存选题
          </button>
          <button
            type="button"
            className="inline-flex shrink-0 items-center gap-1 rounded-md bg-brand-accent px-2.5 py-1.5 text-[11px] font-semibold text-brand-on-accent transition hover:bg-brand-accent-hover"
            onClick={() => onDetail(signal)}
            title="查看完整分析与拆解"
          >
            <Sparkles className="h-3.5 w-3.5 text-brand-on-accent" />
            详情
          </button>
          {signal.url && (
            <button
              type="button"
              className="rounded-lg border border-brand-border p-1.5 text-brand-muted transition hover:bg-brand-surface-raised hover:text-brand-fg"
              onClick={() => onOpen(signal.url)}
              title="打开原片"
            >
              <ExternalLink className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
      </div>
    </article>
  );
};

const AnalysisCard: React.FC<{
  title: string;
  tone?: string;
  children: React.ReactNode;
  defaultOpen?: boolean;
}> = ({ title, tone = 'text-brand-accent', children, defaultOpen = false }) => (
  <details className="group rounded-xl border border-brand-border bg-brand-surface p-3" open={defaultOpen}>
    <summary className="flex cursor-pointer list-none items-center justify-between gap-2">
      <p className={`text-[11px] font-bold uppercase tracking-wider ${tone}`}>{title}</p>
      <ChevronDown className="h-3.5 w-3.5 shrink-0 text-brand-muted transition-transform group-open:rotate-180" />
    </summary>
    <div className="mt-1.5 text-xs sm:text-[13px] leading-relaxed text-brand-fg-subtle">{children}</div>
  </details>
);

const SignalDetailModal: React.FC<{
  signal: TrendRadarSignal;
  onClose: () => void;
  onOpen: (url?: string) => void;
  onSave: (signal: TrendRadarSignal) => void;
  onWatch: (signal: TrendRadarSignal) => void;
  onAnalyze: (signal: TrendRadarSignal, force?: boolean) => void;
  saving: boolean;
  watching: boolean;
  analyzing: boolean;
}> = ({ signal, onClose, onOpen, onSave, onWatch, onAnalyze, saving, watching, analyzing }) => {
  const body = signal.content_text || signal.title;
  const isVideoLookup = signal.niche_id === 'video_lookup';
  const transcriptAvailable = hasVideoTranscript(signal);
  const videoAnalysis = signal.analysis?.status === 'succeeded' ? signal.analysis : null;
  const videoResult = videoAnalysis?.result;

  useEffect(() => {
    if (signal.call_id && !signal.analysis && signal.analysis_status !== 'running' && !analyzing) {
      onAnalyze(signal, false);
    }
  }, [signal.call_id]);

  const metrics = [
    { label: '播放 / 浏览', value: signal.play, icon: Eye },
    { label: '点赞', value: signal.likes, icon: Heart },
    { label: '收藏', value: signal.collects, icon: CheckCircle2 },
    { label: '分享 / 转发', value: signal.shares, icon: Share2 },
  ].filter((metric) => (metric.value ?? 0) > 0);

  return (
    <div
      data-testid="trend-radar-signal-detail"
      role="region"
      aria-label="帖子详情"
      className="mx-auto w-full max-w-[1720px] 2xl:max-w-[2100px] space-y-3 px-2 pb-8 sm:px-4"
    >
      <article className="w-full">
        <header className="mb-4 flex items-center justify-between gap-4 border-b border-brand-border pb-3">
          <div className="flex min-w-0 items-center gap-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-brand-surface-raised">
              <PlatformIcon platform={signal.platform} className="h-5 w-5" />
            </span>
            <div className="min-w-0">
              <p className="text-[11px] font-medium text-brand-muted">
                {platformNames[signal.platform] || signal.platform} · {isVideoLookup ? '视频解析' : '帖子详情'}
              </p>
              <h2 id="trend-radar-signal-detail-title" className="truncate text-sm font-semibold text-brand-fg">
                {signal.title}
              </h2>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="返回列表"
            className="inline-flex shrink-0 items-center gap-1.5 px-2 py-1 text-xs font-medium text-brand-muted transition hover:text-brand-fg"
          >
            <X className="h-4 w-4" />
            返回
          </button>
        </header>

        <div className="grid items-start gap-4 lg:grid-cols-[minmax(15rem,18rem)_minmax(0,1fr)]">
          <div className="space-y-3 lg:sticky lg:top-6">
            <div className="aspect-[3/4] overflow-hidden rounded-lg border border-brand-border bg-black">
              {signal.cover_url ? (
                <img src={signal.cover_url ? `${signal.cover_url}?t=20261024` : ''} alt={`${signal.title}封面大图`} className="h-full w-full object-cover" />
              ) : (
                <div className="flex h-full w-full items-center justify-center">
                  <PlatformIcon platform={signal.platform} className="h-16 w-16 text-white/30" />
                </div>
              )}
            </div>
          </div>

          <div className="min-w-0 space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <span className="rounded-full bg-brand-surface-raised border border-brand-border px-3 py-1 text-xs font-bold text-brand-accent">{signal.niche_name}</span>
              <span className="rounded-full bg-brand-surface-raised border border-brand-border px-3 py-1 text-xs font-semibold text-brand-fg-subtle">{signal.search_term}</span>
              <span className="rounded-full bg-brand-surface-raised border border-brand-border px-3 py-1 text-xs font-semibold text-brand-fg-subtle">
                {signal.content_source === 'platform_caption' ? '官方字幕' : '平台原文'}
              </span>
            </div>

            <section>
              <h2 className="text-lg sm:text-xl font-bold leading-snug text-brand-fg">{signal.title}</h2>
            </section>

            <dl className={`grid gap-3 ${metrics.length >= 4 ? 'grid-cols-2 sm:grid-cols-4' : metrics.length === 3 ? 'grid-cols-3' : metrics.length === 2 ? 'grid-cols-2' : 'grid-cols-1'}`}>
              {metrics.map(({ label, value, icon: Icon }) => (
                <div key={label} className="rounded-xl border border-brand-border bg-brand-surface-raised p-2.5 ">
                  <dt className="flex items-center gap-1.5 text-xs font-medium text-brand-muted"><Icon className="h-3.5 w-3.5 text-brand-muted" />{label}</dt>
                  <dd className="mt-1 text-base sm:text-lg font-bold text-brand-fg">{compactNumber(value)}</dd>
                </div>
              ))}
            </dl>

            <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-brand-border bg-brand-surface-raised p-3 text-xs text-brand-fg-subtle">
              <div className="flex items-center gap-2">
                <span className="font-semibold text-brand-fg">{signal.creator || '未提供作者'}</span>
                <span className="text-brand-fg-subtle">·</span>
                <span>{platformNames[signal.platform] || signal.platform}</span>
              </div>
              <div className="flex items-center gap-3 text-brand-muted">
                <span>发布：{signal.published_at ? formatTime(signal.published_at) : '读不到'}</span>
                <span className="text-brand-fg-subtle">·</span>
                <span>抓取：{formatTime(signal.observed_at)}</span>
                <span className="text-brand-fg-subtle">·</span>
                <span>累计发现：{signal.occurrences} 次</span>
              </div>
            </div>

            <section className="space-y-2">
              <div className="flex items-center justify-between">
                <h3 className="text-xs font-bold uppercase tracking-wider text-brand-muted">
                  {signal.content_source === 'platform_caption'
                    ? '完整官方字幕'
                    : signal.content_source === 'asr_transcript'
                      ? '完整本地转写'
                      : isVideoLookup
                        ? '平台标题 / 描述（非逐字稿）'
                        : '完整帖子正文'}
                </h3>
                <span className="text-[11px] text-brand-muted font-medium">
                  {body.length} 字
                </span>
              </div>
              <div className="max-h-44 overflow-y-auto whitespace-pre-wrap break-words rounded-xl border border-brand-border bg-brand-surface p-3 text-xs sm:text-[13px] leading-relaxed text-brand-fg-subtle ">
                {body}
              </div>
            </section>

            {!videoResult && (
              <section className="rounded-2xl border border-brand-border bg-brand-surface p-4 ">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="max-w-xl">
                    <div className="flex items-center gap-2 text-brand-accent">
                      <Sparkles className="h-5 w-5" />
                      <h3 className="text-base font-bold text-brand-fg">AI 爆款内容拆解与深度分析</h3>
                    </div>
                    <p className="mt-1.5 text-xs sm:text-[13px] leading-relaxed text-brand-fg-subtle">
                      一键提炼该爆款的【开场3秒钩子】、【情绪共鸣点】、【对标受众画像】与【复刻脚本结构】。
                    </p>
                  </div>
                  <button
                    type="button"
                    className="inline-flex items-center gap-2 rounded-xl bg-brand-accent px-5 py-2.5 text-xs sm:text-sm font-bold text-brand-on-accent shadow-sm hover:bg-brand-accent-hover transition disabled:opacity-40 shrink-0"
                    onClick={() => onAnalyze(signal, true)}
                    disabled={analyzing}
                  >
                    {analyzing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Sparkles className="h-4 w-4" />}
                    {analyzing ? '正在深度拆解中…' : '开始 AI 深度拆解'}
                  </button>
                </div>
              </section>
            )}

            {videoResult && videoAnalysis && (
              <div data-testid="trend-radar-video-analysis" className="space-y-4">
                <section
                  aria-label="分析证据范围"
                  className="rounded-xl border border-brand-accent/40 bg-brand-surface-raised/80 px-4 py-3"
                >
                  <h3 className="text-xs font-bold text-brand-accent">
                    {transcriptAvailable ? '已含逐字稿：可分析文本内容' : '未含逐字稿：只可分析元数据'}
                  </h3>
                  <p className="mt-1 text-xs leading-5 text-brand-fg-subtle">
                    {transcriptAvailable
                      ? '文本分析可引用字幕或本地转写；画面、镜头语言及未出现于逐字稿的结论仍未验证。'
                      : '仅依据标题、标签和公开指标。行业基准、口播、画面与内容结构均未验证，不应当作视频事实。'}
                  </p>
                </section>

                <section className="rounded-2xl border border-brand-border bg-brand-surface p-4  space-y-3">
                  <div className="flex flex-wrap items-center justify-between gap-2 border-b border-brand-border pb-3">
                    <div className="flex items-center gap-2">
                      <Sparkles className="h-4 w-4 text-brand-accent" />
                      <h3 className="text-sm font-bold text-brand-fg">AI 爆款拆解</h3>
                    </div>
                    <div className="flex items-center gap-2">
                      <span className="rounded-full border border-brand-border bg-brand-surface-raised px-2.5 py-1 text-[11px] font-semibold text-brand-accent">
                        {videoAnalysis.provider} · {videoAnalysis.model || '模型未标注'} · {videoAnalysis.duration_ms} ms
                      </span>
                      <button
                        type="button"
                        onClick={() => onAnalyze(signal, true)}
                        disabled={analyzing}
                        className="text-xs text-brand-muted hover:text-brand-accent font-medium px-2 py-1 rounded-lg hover:bg-brand-surface-raised transition"
                      >
                        {analyzing ? '重新拆解中…' : '重新拆解'}
                      </button>
                    </div>
                  </div>

                  {(videoAnalysis.attempts?.length ?? 0) > 0 ? (
                    <div className="flex flex-wrap gap-1.5" aria-label="模型调用记录">
                      {videoAnalysis.attempts?.map((attempt, index) => (
                        <span
                          key={`${attempt.route}-${index}`}
                          className={`rounded-full border px-2.5 py-0.5 text-[10px] font-semibold ${
                            attempt.status === 'succeeded'
                              ? 'border-brand-success/30 bg-brand-success/10 text-brand-success'
                              : 'border-brand-accent/40 bg-brand-surface-raised text-brand-accent'
                          }`}
                        >
                          {attempt.route === 'agent' ? 'Agent' : 'LLM'} · {attempt.status === 'succeeded' ? '成功' : '未完成'} · {attempt.duration_ms} ms
                        </span>
                      ))}
                    </div>
                  ) : null}

                  <div className="rounded-xl border border-brand-border bg-brand-surface p-3 ">
                    <p className="text-xs sm:text-[13px] font-medium leading-relaxed text-brand-fg">{videoResult.summary}</p>
                  </div>

                  <div className="grid gap-2 sm:grid-cols-2">
                    <AnalysisCard title="爆款潜力与传播因子">{videoResult.viral_potential}</AnalysisCard>
                    <AnalysisCard title="前 3 秒黄金钩子 (Hook)" tone="text-brand-warning">{videoResult.hook}</AnalysisCard>
                    <AnalysisCard title="目标受众人群" tone="text-brand-muted">{videoResult.audience}</AnalysisCard>
                    <AnalysisCard title="核心情绪驱动力" tone="text-brand-muted">{videoResult.emotional_drivers.join('；') || '未验证'}</AnalysisCard>
                  </div>

                  <AnalysisCard title="公开证据指标" tone="text-brand-muted">
                    <ul className="space-y-1.5 leading-5">
                      {videoResult.evidence.map((item) => (
                        <li key={item} className="flex items-center gap-2">
                          <span className="h-1.5 w-1.5 rounded-full bg-brand-muted" />
                          {item}
                        </li>
                      ))}
                    </ul>
                  </AnalysisCard>

                  <AnalysisCard title="复刻脚本与内容结构" tone="text-brand-muted">
                    <ol className="space-y-2 leading-5">
                      {videoResult.structure.map((item, index) => (
                        <li key={item} className="flex items-start gap-2.5">
                          <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-brand-surface-raised text-[10px] font-bold text-brand-accent">
                            {index + 1}
                          </span>
                          <span className="mt-0.5">{item}</span>
                        </li>
                      ))}
                    </ol>
                  </AnalysisCard>
                </section>

                <section className="space-y-2">
                  <h3 className="text-xs font-bold text-brand-fg">选题建议</h3>
                  <div className="grid gap-2">
                    {videoResult.topic_suggestions.map((topic, index) => (
                      <div key={topic} className="flex items-center gap-3 rounded-xl border border-brand-border bg-brand-surface p-3 hover:border-brand-accent transition ">
                        <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-brand-surface-raised text-[11px] font-bold text-brand-accent">{index + 1}</span>
                        <p className="text-xs sm:text-[13px] font-medium leading-5 text-brand-fg">{topic}</p>
                      </div>
                    ))}
                  </div>
                </section>

                <section className="space-y-2">
                  <h3 className="text-xs font-bold text-brand-fg">分析提示词</h3>
                  <div className="space-y-2">
                    {videoResult.analysis_prompts.map((prompt) => (
                      <details key={prompt.title} className="rounded-xl border border-brand-border bg-brand-surface p-3 group">
                        <summary className="cursor-pointer text-xs font-bold text-brand-fg flex items-center justify-between">
                          <span>{prompt.title}</span>
                          <span className="text-[11px] text-brand-muted font-normal">点击展开 Prompt 审计</span>
                        </summary>
                        <pre className="mt-3 whitespace-pre-wrap break-words rounded-lg bg-black p-3 font-sans text-xs leading-relaxed text-gray-200">{prompt.text}</pre>
                      </details>
                    ))}
                  </div>
                </section>

                {videoResult.limitations.length > 0 && (
                  <section className="rounded-xl border border-brand-accent/40 bg-brand-surface-raised/80 p-4">
                    <h3 className="text-xs font-bold text-brand-accent">分析边界</h3>
                    <ul className="mt-2 space-y-1 text-xs leading-5 text-brand-fg-subtle">
                      {videoResult.limitations.map((item) => <li key={item}>· {item}</li>)}
                    </ul>
                  </section>
                )}
              </div>
            )}

            <footer className="flex flex-wrap items-center justify-end gap-3 border-t border-brand-border pt-5">
              {signal.creator_id && (signal.platform === 'douyin' || signal.platform === 'xhs') && (
                <button type="button" className="ui-btn" onClick={() => onWatch(signal)} disabled={watching}>
                  {watching ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <UserRoundSearch className="h-4 w-4" />}
                  加入博主监控
                </button>
              )}
              <button type="button" className="ui-btn" onClick={() => onSave(signal)} disabled={saving}>
                {saving ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Lightbulb className="h-4 w-4" />}
                保存为选题
              </button>
              {signal.url && (
                <button type="button" className="ui-btn ui-btn-primary" onClick={() => onOpen(signal.url)}>
                  <ExternalLink className="h-4 w-4" />
                  打开原帖
                </button>
              )}
            </footer>
          </div>
        </div>
      </article>
    </div>
  );
};
