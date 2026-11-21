import { useCallback, useEffect, useRef, useState } from 'react';
import {
  api,
  type TrendRadarArchiveProgress,
  type TrendRadarArchiveResult,
  type TrendRadarCreator,
} from '../../api';
import { useUIStore } from '../../store';

type CreatorArchiveOptions = {
  providerActive: boolean;
  mutateSummary: () => unknown;
  mutateCallHistory: () => unknown;
  // 当前打开的博主文件夹。切换博主时先停掉上一个的轮询，再尝试接上后端仍在跑的任务。
  activeCreator?: TrendRadarCreator | null;
};

export type CreatorArchiveRunOptions = {
  // 「重试全部」这类入口本身已经是明确的修复动作，不再弹二次确认。
  skipConfirm?: boolean;
  // 只补缺失的封面/音频，不再抓一遍列表（重试失败项时省掉全部分页调用）。
  mediaOnly?: boolean;
};

export type CreatorArchiveState = {
  archiveCreator: (creator: TrendRadarCreator, runOptions?: CreatorArchiveRunOptions) => Promise<void>;
  // 进入博主文件夹时接上后端仍在跑的归档（刷新页面后的续接入口）。
  resumeCreatorArchive: (creator: TrendRadarCreator) => Promise<void>;
  archiveProgress: Record<string, TrendRadarArchiveProgress>;
  archivingCreator: string;
  archiveDone: string;
};

// 续接时后端还没写进度的占位：进度条显示「正在启动归档…」而不是空白。
const EMPTY_ARCHIVE_PROGRESS: TrendRadarArchiveProgress = {
  phase: '', posts_fetched: 0, posts_total_estimate: 0,
  covers_done: 0, covers_total: 0, audio_done: 0, audio_total: 0,
};

// 博主全量归档：POST 启动异步任务 + 1 秒轮询 archive-status 展示实时进度，
// 卸载时清理所有轮询定时器。状态与定时器绑定在同一个 hook 里，避免泄漏。
export function useCreatorArchive(options: CreatorArchiveOptions): CreatorArchiveState {
  const { providerActive, mutateSummary, mutateCallHistory, activeCreator } = options;
  const { toast } = useUIStore();
  const [archivingCreator, setArchivingCreator] = useState('');
  const [archiveDone, setArchiveDone] = useState('');
  const [archiveProgress, setArchiveProgress] = useState<Record<string, TrendRadarArchiveProgress>>({});
  const archivePollRef = useRef<Record<string, number>>({});
  const activeKey = activeCreator ? `${activeCreator.platform}-${activeCreator.user_id}` : '';

  // 「已更新」完成态短暂显示后淡出，不给按钮留下永久的假状态。
  useEffect(() => {
    if (!archiveDone) return undefined;
    const timer = window.setTimeout(() => setArchiveDone(''), 2400);
    return () => window.clearTimeout(timer);
  }, [archiveDone]);

  // 卸载时清理所有归档轮询定时器，避免切页后继续打接口。
  useEffect(() => () => {
    Object.values(archivePollRef.current).forEach((timer) => window.clearInterval(timer));
    archivePollRef.current = {};
  }, []);

  const stopPolling = useCallback((key: string) => {
    const timer = archivePollRef.current[key];
    if (!timer) return;
    window.clearInterval(timer);
    delete archivePollRef.current[key];
  }, []);

  // 收尾：清定时器 + 清进度 + 统一 toast 与 summary 刷新。
  // 「点击启动」和「刷新续上」共用这一条路径，完成后的表现保持一致。
  const finish = useCallback((
    key: string,
    status: 'done' | 'error' | 'idle',
    result?: TrendRadarArchiveResult | null,
    errorText?: string,
  ) => {
    stopPolling(key);
    setArchivingCreator('');
    setArchiveProgress((prev) => { const next = { ...prev }; delete next[key]; return next; });
    if (status === 'done' && result) {
      const media = result.media;
      // 已有文件会被跳过，所以「本次下载 0」是正常的；把跳过数一起说清楚，
      // 否则会让人误以为封面/音频都没落盘。
      const skippedNote = media.skipped > 0 ? `，${media.skipped} 项已存在跳过` : '';
      // 只补媒体时「本次抓到 0 条」会让人以为出错了，单独给一句文案。
      const mediaOnly = result.stopped === 'media_only';
      // 受限抓取时 posts 会比 fetched 大（索引与历史合并）。说清这两个数，
      // 否则用户会以为「113 个作品」里有些是这次重复抓的。
      const fetchedNote = !mediaOnly && typeof result.fetched === 'number' && result.fetched < result.posts
        ? `，本次抓到 ${result.fetched} 条`
        : '';
      // 有失败项时把「去哪儿补」一起说完：toast 只有一个槽位，补发第二条会把它盖掉。
      const retryHint = media.failed > 0 ? `，失败 ${media.failed} 项可点「失败 ${media.failed} 项」重试` : '';
      const headline = mediaOnly
        ? `媒体补齐完成：${result.posts} 个作品`
        : `归档完成：${result.posts} 个作品（新增 ${result.new_posts}${fetchedNote}）`;
      toast.show(
        `${headline}，本次下载 封面 ${media.covers} / 音频 ${media.audio}${skippedNote}${retryHint}`,
        media.failed ? 'error' : 'success',
      );
      if (!media.failed) setArchiveDone(key);
      void mutateSummary();
      void mutateCallHistory();
    } else if (status === 'error') {
      toast.show(errorText || '全量归档失败', 'error');
    }
  }, [mutateCallHistory, mutateSummary, stopPolling, toast]);

  // 1 秒轮询 archive-status；同一博主只允许一个定时器，重复调用直接复用。
  const startPolling = useCallback((platform: string, userID: string, key: string) => {
    if (archivePollRef.current[key]) return;
    const poll = async () => {
      try {
        const status = await api.getTrendRadarCreatorArchiveStatus(platform, userID);
        if (status.progress) {
          setArchiveProgress((prev) => ({ ...prev, [key]: status.progress as TrendRadarArchiveProgress }));
        }
        if (status.status === 'done') finish(key, 'done', status.result);
        else if (status.status === 'error') finish(key, 'error', null, status.error);
        else if (status.status === 'idle') finish(key, 'idle');
      } catch {
        // 网络抖动不中断轮询；下一次继续。
      }
    };
    archivePollRef.current[key] = window.setInterval(() => { void poll(); }, 1000);
    void poll();
  }, [finish]);

  // 刷新页面 / 重新进入文件夹时接上后端仍在跑的归档：只有 running 才接管，
  // 静默接上（不弹 toast），跑完仍走 finish() 的完成提示与 summary 刷新。
  const resumeCreatorArchive = useCallback(async (creator: TrendRadarCreator) => {
    const { platform, user_id: userID } = creator;
    if (platform !== 'douyin') return;
    const key = `${platform}-${userID}`;
    if (archivePollRef.current[key]) return;
    try {
      const status = await api.getTrendRadarCreatorArchiveStatus(platform, userID);
      if (status.status !== 'running') return;
      // 查询期间可能已经点了「更新」或重复触发：再确认一次才开定时器。
      if (archivePollRef.current[key]) return;
      setArchiveDone('');
      setArchivingCreator(key);
      setArchiveProgress((prev) => ({ ...prev, [key]: status.progress ?? EMPTY_ARCHIVE_PROGRESS }));
      startPolling(platform, userID, key);
    } catch {
      // 续接进度属于兜底体验：接口失败不打断浏览，也不弹 toast。
    }
  }, [startPolling]);

  // 切博主 / 退出文件夹：停掉不属于当前博主的定时器，别在后台继续打接口；
  // 再静默接上当前博主可能正在跑的任务。只在「打开哪个博主」变化时同步一次。
  useEffect(() => {
    Object.entries(archivePollRef.current).forEach(([key, timer]) => {
      if (key === activeKey) return;
      window.clearInterval(timer);
      delete archivePollRef.current[key];
    });
    setArchivingCreator((current) => (current && current !== activeKey ? '' : current));
    if (!activeKey || !activeCreator) return;
    void resumeCreatorArchive(activeCreator);
    // activeCreator 的其它字段（auto_archive 等）变化不需要重新接进度。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeKey]);

  // 全量归档：异步启动 + 1 秒轮询，实时展示抓取/下载进度。
  const archiveCreator = async (creator: TrendRadarCreator, runOptions?: CreatorArchiveRunOptions) => {
    const { platform, user_id: userID, name } = creator;
    if (platform !== 'douyin') {
      toast.show('全量归档当前支持抖音博主；小红书可先用「刷新快照」', 'error');
      return;
    }
    if (!providerActive) {
      toast.show('请先激活 TikHub API', 'error');
      return;
    }
    if (!runOptions?.skipConfirm && !window.confirm(
      `将拉取「${name}」的全部作品（每 20 条 1 次真实调用，直到列表结束），`
      + '并把封面和音频并发下载到本地文件夹。已有文件会跳过，可反复点击更新。继续吗？',
    )) return;
    const key = `${platform}-${userID}`;
    setArchiveDone('');
    setArchivingCreator(key);
    setArchiveProgress((prev) => ({ ...prev, [key]: {
      phase: 'posts', posts_fetched: 0, posts_total_estimate: 0,
      covers_done: 0, covers_total: 0, audio_done: 0, audio_total: 0,
    } }));

    try {
      await api.archiveTrendRadarCreator(platform, userID, runOptions?.mediaOnly ? { media_only: true } : undefined);
    } catch (cause) {
      if (archivePollRef.current[key]) {
        // 已经通过 archive-status 接上同一个任务（POST 409 = 重复启动），不打扰用户。
        return;
      }
      setArchivingCreator('');
      setArchiveProgress((prev) => { const next = { ...prev }; delete next[key]; return next; });
      toast.show(cause instanceof Error ? cause.message : '全量归档启动失败', 'error');
      return;
    }

    startPolling(platform, userID, key);
  };

  return { archiveCreator, resumeCreatorArchive, archiveProgress, archivingCreator, archiveDone };
}
