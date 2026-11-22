import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const trendSourceFiles = [
  '../src/components/DashboardView.tsx',
  '../src/components/trend/ArchiveFailuresPanel.tsx',
  '../src/components/trend/CreatorPostGrid.tsx',
  '../src/components/trend/PostCoverThumb.tsx',
  '../src/components/trend/PostDetailModal.tsx',
  '../src/components/trend/useCreatorArchive.ts',
  '../src/components/trend/format.ts',
];
// 趋势雷达 UI 已按模块拆分（编排层 + trend/ 子模块），断言针对全部相关源码的拼接结果。
const dashboard = (
  await Promise.all(
    trendSourceFiles.map((file) => readFile(new URL(file, import.meta.url), 'utf8')),
  )
).join('\n');
const archiveFailuresPanel = await readFile(
  new URL('../src/components/trend/ArchiveFailuresPanel.tsx', import.meta.url),
  'utf8',
);
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');
const store = await readFile(new URL('../src/store.ts', import.meta.url), 'utf8');
const addCreatorDialog = await readFile(
  new URL('../src/components/AddCreatorDialog.tsx', import.meta.url),
  'utf8',
);

test('dashboard keeps exactly three Chinese trend radar pages', () => {
  for (const section of ['雷达内容', '抓取', '日志']) {
    assert.match(dashboard, new RegExp(section));
  }
  assert.match(dashboard, /type RadarSection = 'content' \| 'collect' \| 'logs'/);
  assert.doesNotMatch(dashboard, /label: '视频拆解'/);
  assert.doesNotMatch(dashboard, /恢复 AI \/ 情感/);
  assert.doesNotMatch(dashboard, /AI赛道|情感赛道/);
  assert.doesNotMatch(dashboard, /至少保留一个赛道/);
  assert.match(dashboard, /还没有自定义赛道/);
  assert.match(dashboard, /search_terms: \[\]/);
  assert.match(dashboard, /赛道名称、说明、搜索词和平台全部由你定义/);
  assert.doesNotMatch(dashboard, /评论洞察/);
  assert.doesNotMatch(dashboard, /日榜/);
  assert.match(dashboard, /data-testid="trend-radar-dashboard"/);
  assert.ok(api.includes("'/api/trend-radar/summary'"));
  assert.ok(api.includes("'/api/trend-radar/config'"));
  assert.ok(api.includes("'/api/trend-radar/covers/backfill'"));
});

test('niche and creator monitoring require explicit confirmation', () => {
  assert.match(dashboard, /api\.callDataAPI/);
  assert.match(dashboard, /api\.addTrendRadarCreator/);
  assert.match(dashboard, /window\.confirm/);
  assert.match(dashboard, /没有发起 API 调用/);
  assert.match(dashboard, /共 \$\{terms\.length\} 次真实调用/);
});

test('all collected signals and local covers are visible', () => {
  // 渲染全部信号，但分批追加：一次渲染 500+ 张卡片会让首帧卡住。
  assert.match(dashboard, /visibleSignals\.slice\(0, visibleSignalLimit\)\.map/);
  assert.match(dashboard, /const SIGNAL_PAGE_SIZE = \d+;/);
  assert.match(dashboard, /data-testid="trend-load-more"/);
  assert.doesNotMatch(dashboard, /signals\.slice/);
  assert.match(dashboard, /signal\.cover_url/);
  assert.match(dashboard, /signal\.content_text/);
  assert.match(dashboard, /onDetail=\{setSelectedSignal\}/);
  assert.match(dashboard, /data-testid="trend-radar-signal-detail"/);
  assert.match(dashboard, /data-testid=\{`trend-radar-platform-\$\{signal\.platform\}`\}/);
  assert.match(dashboard, /platformNames\[signal\.platform\]/);
  assert.match(dashboard, /完整官方字幕/);
  assert.match(dashboard, /平台原文/);
  assert.match(dashboard, /官方字幕/);
  assert.match(dashboard, /补全本地封面/);
  assert.match(dashboard, /不会启动定时任务/);
});

test('content labels and Douyin official parameters are selectable', () => {
  for (const label of ['平台', '赛道', '搜索词', '发布时间', '排序', '时长', '内容类型']) {
    assert.match(dashboard, new RegExp(label));
  }
  assert.match(dashboard, /publish_time/);
  assert.match(dashboard, /sort_type/);
  assert.match(dashboard, /filter_duration/);
  assert.match(dashboard, /content_type/);
  assert.match(dashboard, /近 7 天/);
});

test('local call log exposes parameters, raw files, cost verification and no polling', () => {
  assert.match(dashboard, /api\.getDataAPICalls/);
  assert.match(dashboard, /api\.getDataAPICallResponse/);
  assert.match(dashboard, /完整本地响应/);
  assert.match(dashboard, /价格未验证/);
  assert.match(dashboard, /call\.provider_code/);
  assert.match(dashboard, /call\.provider_message/);
  assert.match(dashboard, /仅查看不会再次调用 API/);
  assert.match(dashboard, /refreshInterval:\s*0/);
});

test('collect page includes explicit schedules RSS and local video deconstruction', () => {
  assert.match(dashboard, /存为定时计划/);
  assert.match(dashboard, /浏览器关闭后仍会运行/);
  assert.match(dashboard, /api\.getTrendRadarSchedules/);
  assert.match(dashboard, /RSS 收集/);
  assert.match(dashboard, /抓网页全文/);
  assert.match(dashboard, /api\.collectTrendRadarRSS/);
  assert.match(dashboard, /视频拆解/);
  assert.match(dashboard, /平台字幕 → 本地 Whisper → 明确确认后云端转写/);
  assert.match(dashboard, /api\.uploadTrendRadarVideo/);
});

test('Douyin YouTube and X use curated keyword search endpoints', () => {
  assert.match(dashboard, /douyin:\s*'douyin\.video_search'/);
  assert.match(dashboard, /youtube:\s*'youtube\.video_search'/);
  assert.match(dashboard, /x:\s*'x\.keyword_search'/);
  assert.match(dashboard, /\{ id: 'youtube', name: 'YouTube' \}/);
  assert.match(dashboard, /\{ id: 'x', name: 'X' \}/);
});

test('search terms can be edited and persisted without automatic network calls', () => {
  assert.match(dashboard, /api\.saveTrendRadarConfig/);
  assert.match(dashboard, /搜索词/);
  assert.match(dashboard, /从已有标签选择/);
  assert.match(dashboard, /flatMap\(\(item\) => item\.search_terms\)/);
  assert.doesNotMatch(dashboard, /label:\s*'提示词'/);
});

test('radar content shows one flat list, without the keyword/video/creator switcher', () => {
  // 关键词搜索 / 视频搜索 / 博主监控 三个视图切换已按产品要求去掉：
  // 雷达内容统一在热榜里展示，不再要求用户先选一种视图。
  for (const view of ['关键词搜索', '视频搜索', '博主监控']) {
    assert.doesNotMatch(dashboard, new RegExp(`'${view}'`), `${view} 不应再作为切换项出现`);
  }
  assert.doesNotMatch(dashboard, /type RadarContentView = 'keywords'/);
  // 博主文件夹相关能力仍在（只是不再由顶部 tab 进入）。
  assert.match(dashboard, /creator\.folder_path/);
  assert.match(dashboard, /返回博主文件夹/);
});

test('keyword results switch between folder and one-column list with search filters and sorting', () => {
  assert.match(dashboard, /type KeywordLayout = 'folders' \| 'list'/);
  assert.match(dashboard, /useState<KeywordLayout>\('list'\)/);
  assert.match(dashboard, /文件夹/);
  assert.match(dashboard, /单列列表/);
  assert.match(dashboard, /最近抓取 \{formatTime\(latest\)\}/);
  assert.match(dashboard, /搜索标题、正文、博主、平台或关键词/);
  assert.match(dashboard, /signalSort === 'recurring'/);
  assert.match(dashboard, /signalSort === 'engagement'/);
  assert.match(dashboard, /setKeywordLayout\('list'\)/);
});

test('exact video search deduplicates locally and uses Agent with one Settings LLM fallback', () => {
  assert.match(dashboard, /api\.parseTrendRadarVideoInput/);
  assert.match(dashboard, /纯视频 ID、视频页链接、v\.douyin\.com 短链接/);
  assert.match(dashboard, /只会提交以下核心参数/);
  assert.match(dashboard, /调用 1 次 TikHub 作品详情接口并完成模型分析/);
  assert.match(dashboard, /parsed\.existing_call_id/);
  assert.match(dashboard, /已打开历史分析；未重复调用 TikHub 或模型/);
  assert.match(dashboard, /data\.video_lookups\.map/);
  assert.match(dashboard, /type VideoLookupStage = 'idle' \| 'parsing' \| 'fetching' \| 'analyzing'/);
  assert.match(dashboard, /data-testid="video-lookup-progress"/);
  assert.match(dashboard, /正在识别链接与核心参数/);
  assert.match(dashboard, /正在抓取作品详情与封面/);
  assert.match(dashboard, /正在调用模型生成拆解/);
  assert.match(dashboard, /命中相同视频 ID 时只读取本地记录，不重复调用/);
  assert.doesNotMatch(dashboard, /animate-ping/);
  assert.doesNotMatch(dashboard, /border-violet-200 bg-violet-50\/80/);
  assert.match(dashboard, /title="搜索历史"/);
  assert.match(dashboard, /VideoLookupHistoryRow/);
  assert.match(dashboard, /查看视频拆解/);
  assert.match(dashboard, /data-testid="trend-radar-video-analysis"/);
  assert.match(dashboard, /AI 爆款拆解/);
  assert.doesNotMatch(dashboard, /备用 LLM/);
  assert.match(dashboard, /videoAnalysis\.attempts/);
  assert.match(dashboard, /选题建议/);
  assert.match(dashboard, /分析提示词/);
  assert.doesNotMatch(dashboard, /buildVideoLookupAnalysis/);
  assert.match(dashboard, /分析边界/);
  assert.match(api, /'\/api\/trend-radar\/video-search\/parse'/);
  assert.match(api, /\/api\/trend-radar\/video-search\/\$\{callID\}\/analyze/);
});

test('radar labels evidence-only analysis honestly and keeps success messages out of error UI', () => {
  assert.match(dashboard, /LLM 元数据分析/);
  assert.match(dashboard, /未含逐字稿：只可分析元数据/);
  assert.match(dashboard, /仅依据标题、标签和公开指标/);
  assert.match(dashboard, /行业基准、口播、画面与内容结构均未验证/);
  assert.doesNotMatch(dashboard, /智能拆解完成/);
  assert.match(dashboard, /接口提示/);
  assert.match(dashboard, /call\.status === 'succeeded'/);
});

test('signal list stays lean and one click hands the filter to the agent', () => {
  // 一键把当前筛选结果交给 Alpha Agent 分析（复用 harness，不新建分析 API）
  assert.match(dashboard, /data-testid="trend-ai-analysis"/);
  assert.match(dashboard, /const runAIAnalysis = \(\) => \{/);
  assert.match(dashboard, /setAgentDraft\(\{[\s\S]*?mode: 'tools'/);
  assert.match(dashboard, /openAgentDrawer\(\)/);
  // 卡片降噪：正文两行截断，赛道与搜索词并入元信息行
  // 正文两行截断。不能用 whitespace-pre-wrap：它保留换行，会让
  // line-clamp 失效、整段正文塞满卡片。
  assert.match(dashboard, /line-clamp-2 text-\[10px\]/);
  assert.doesNotMatch(dashboard, /line-clamp-2 whitespace-pre-wrap/);
  assert.doesNotMatch(dashboard, /line-clamp-3/);
});

test('trend radar exposes an explicit freshness control wired to the due worker', () => {
  // 立即抓取：调用 run-due API，不是前端伪造刷新
  assert.match(dashboard, /data-testid="trend-run-due"/);
  assert.match(dashboard, /const runDueNow = async \(\) => \{/);
  assert.match(dashboard, /api\.runTrendRadarDue\(\)/);
  assert.match(dashboard, /await mutateSummary\(\)/);
  // 定时状态可点击前往开启，且能区分「已启用」与「未启用」
  assert.match(dashboard, /data-testid="trend-schedule-status"/);
  assert.match(dashboard, /const enabledScheduleCount = /);
  assert.match(dashboard, /setSection\('collect'\)/);
  // 默认按最新排序，避免高互动老数据压住新抓到的信号
  assert.match(dashboard, /useState<SignalSort>\('latest'\)/);
  // API 客户端必须真的打到 run-due 端点
  assert.match(api, /runTrendRadarDue: \(\) =>/);
  assert.match(api, /'\/api\/trend-radar\/schedules\/run-due'/);
});

test('creator monitoring empty state opens the add dialog without leaving the view', () => {
  assert.match(dashboard, /actionLabel="添加博主"/);
  // 添加博主必须弹窗，不能把用户切到其它分区，也不能内联展开
  assert.match(dashboard, /onAction=\{\(\) => setAddCreatorOpen\(true\)\}/);
  assert.doesNotMatch(dashboard, /onAction=\{\(\) => setSection\('collect'\)\}/);
  assert.match(dashboard, /data-testid="creator-add-toggle"/);
  // 两个入口共用同一个弹窗组件，避免重复实现
  assert.match(dashboard, /import AddCreatorDialog from '\.\/AddCreatorDialog'/);
  assert.match(dashboard, /<AddCreatorDialog/);
  assert.match(dashboard, /open=\{addCreatorOpen\}/);
  // 内联表单已移除
  assert.doesNotMatch(dashboard, /onSubmit=\{addCreator\}/);
});

test('add creator dialog collects name, platform and identifier in order', () => {
  const dialog = addCreatorDialog;
  // 名称优先，其次平台，最后链接/ID —— 与用户心智顺序一致
  const nameAt = dialog.indexOf('博主名称');
  const platformAt = dialog.indexOf('>平台<');
  const identifierAt = dialog.indexOf('主页链接或 ID');
  assert.ok(nameAt > -1, 'dialog must render 博主名称');
  assert.ok(platformAt > -1, 'dialog must render 平台');
  assert.ok(identifierAt > -1, 'dialog must render 主页链接或 ID');
  assert.ok(nameAt < platformAt, 'name must come before platform');
  assert.ok(platformAt < identifierAt, 'platform must come before identifier');
  // 必须是真正的模态框
  assert.match(dialog, /role="dialog"/);
  assert.match(dialog, /aria-modal="true"/);
  assert.match(dialog, /data-testid="add-creator-dialog"/);
  // 抖音 / 小红书 二选一，且 testid 可被端到端测试定位
  assert.match(dialog, /id: 'douyin', label: '抖音'/);
  assert.match(dialog, /id: 'xhs', label: '小红书'/);
  assert.match(dialog, /data-testid=\{`add-creator-platform-\$\{option\.id\}`\}/);
  assert.match(dialog, /data-testid="add-creator-name"/);
  assert.match(dialog, /data-testid="add-creator-identifier"/);
  // Escape 关闭 + 提交前校验
  assert.match(dialog, /event\.key === 'Escape'/);
  assert.match(dialog, /data-testid="add-creator-error"/);
  assert.match(dialog, /api\.addTrendRadarCreator\(/);
  // 粘贴链接时自动切换平台，避免链接与所选平台不一致
  assert.match(dialog, /platformFromURL/);
});

test('summary client normalizes nullable latest_posts so cards never crash', () => {
  // 后端对从未抓取的博主返回 latest_posts: null，客户端必须归一成数组
  assert.match(api, /getTrendRadarSummary: async \(\) => \{/);
  assert.match(api, /if \(!Array\.isArray\(creator\.latest_posts\)\) creator\.latest_posts = \[\]/);
});

test('trend radar sub-views keep distinct child routes', () => {
  // 侧栏收敛成单一「趋势雷达」后，子视图统一挂在 /GTM/trend/* 下；
  // 历史 /SM、/media、/trend 前缀仍可解析，避免旧书签失效。
  assert.match(store, /'\/GTM\/trend\/keywords': 'keywords'/i);
  assert.match(store, /'\/GTM\/trend\/videos': 'videos'/i);
  assert.match(store, /'\/GTM\/trend\/creators': 'creators'/i);
  assert.match(store, /'\/SM\/trend\/keywords': 'keywords'/i);
  assert.match(store, /'\/media\/trend\/keywords': 'keywords'/i);
  assert.match(store, /const targetPath = `\/GTM\/trend\/\$\{RADAR_VIEW_SEGMENT\[view\]\}`/);
  assert.match(store, /getRadarViewFromPathname/);
  assert.match(store, /syncRadarViewURL/);
});

test('Douyin creator monitoring explains and enforces official paging controls', () => {
  assert.match(dashboard, /每页数量/);
  assert.match(dashboard, /20 条（官方建议）/);
  assert.match(dashboard, /max_cursor=0/);
  assert.match(dashboard, /has_more=0/);
  assert.match(dashboard, /本地按 create_time 截断/);
  assert.match(dashboard, /creator\.max_pages/);
  assert.match(dashboard, /creator\.lookback_days/);
  assert.match(dashboard, /api\.updateTrendRadarCreator/);
  assert.match(dashboard, /douyinPagingState/);
});

test('creator folders expose a one-click full archive and update action', () => {
  // 全量归档：一次拉完所有作品 + 并发下载封面/音频，且可反复点击更新
  assert.match(dashboard, /const archiveCreator = async \(creator: TrendRadarCreator, runOptions\?: CreatorArchiveRunOptions\) => \{/);
  assert.match(dashboard, /api\.archiveTrendRadarCreator\(platform, userID/);
  assert.match(dashboard, /setArchivingCreator\(key\)/);
  assert.match(dashboard, /await mutateSummary\(\)/);
  // 抓取页按钮
  assert.match(dashboard, /data-testid=\{`creator-archive-\$\{key\}`\}/);
  assert.match(dashboard, /抓取全部作品/);
  // 博主文件夹里的「更新」按钮
  assert.match(dashboard, /data-testid="creator-folder-update"/);
  assert.match(dashboard, /已有文件会跳过/);
  // 小红书暂不支持全量归档，必须给出明确提示而不是静默失败
  assert.match(dashboard, /全量归档当前支持抖音博主/);
  // API 客户端必须打到 archive 端点，并带上失败计数用于提示
  assert.match(api, /archiveTrendRadarCreator: \(platform: string, userID: string/);
  assert.match(api, /\/archive`/);
  assert.match(api, /media: \{ total: number; covers: number; audio: number; skipped: number; failed: number \}/);
});

test('creator posts serve local covers and expose instant audio previews', () => {
  // 媒体 API 客户端生成正确的相对路径转写
  assert.match(api, /getTrendRadarCreatorMediaURL: \(platform: string, userID: string, relativePath: string\) => \{/);
  assert.match(api, /relativePath\.replace\(\/\^media\\\/\//);
  // 封面优先走本地缓存并保留已归档标记
  assert.match(dashboard, /api\.getTrendRadarCreatorMediaURL\(selectedCreator\.platform, selectedCreator\.user_id, post\.cover_path\)/);
  assert.match(dashboard, /本地封面/);
  // 音频提供即时试听与播放器控制
  assert.match(dashboard, /data-testid=\{`post-audio-\$\{post\.id\}`\}/);
  assert.match(dashboard, /试听原声/);
  assert.match(dashboard, /toggleAudio/);
  assert.match(dashboard, /data-testid="audio-mini-player"/);
  assert.match(dashboard, /data-testid="audio-player-toggle"/);
  assert.match(dashboard, /data-testid="audio-player-close"/);
});

test('single creator folder has its own deep link', () => {
  // store 提供深链解析与 URL 同步；user_id 大小写敏感，不能整体小写化
  assert.match(store, /const CREATOR_PATH_PREFIX = '\/trend\/creators\/'/);
  assert.match(store, /export function getCreatorKeyFromPathname\(\)/);
  assert.match(store, /export function syncCreatorURL\(key: string\)/);
  assert.match(store, /decodeURIComponent\(parts\[1\]\)/);
  assert.match(store, /encodeURIComponent\(userID\)/);
  // 深链启动即还原选中博主，前进/后退也要还原（初值 + popstate 各一处）
  const restores = store.match(/selectedCreatorKey: getCreatorKeyFromPathname\(\) \?\? ''/g) ?? [];
  assert.ok(restores.length >= 2, 'boot 与 popstate 都要从 URL 还原选中博主');
  // tab/view 解析把博主深链识别为 dashboard + creators，避免被父级同步冲掉
  assert.match(store, /if \(isCreatorPath\(window\.location\.pathname\)\) return 'dashboard'/);
  assert.match(store, /if \(isCreatorPath\(path\)\) return 'creators'/);
  assert.match(store, /if \(isCreatorPath\(window\.location\.pathname\)\) return;/);
  // DashboardView 直接使用 store 全局态，不再用组件内 state
  assert.match(dashboard, /selectedCreatorKey, setSelectedCreatorKey \} = useUIStore\(\)/);
  assert.doesNotMatch(dashboard, /useState\(''\);\s*\/\/ selectedCreatorKey/);
  // 打开文件夹按钮不再有乱码文案
  assert.doesNotMatch(dashboard, /\ufffd/);
});

test('creator post cards are vertical and degrade black source covers honestly', () => {
  // 竖屏卡片：9:16 容器 + object-contain 不裁切，网格列数随竖卡增加
  assert.match(dashboard, /aspect-\[9\/16\]/);
  assert.match(dashboard, /object-contain/);
  assert.match(dashboard, /grid-cols-2 gap-3[\s\S]*?sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5/);
  // 黑帧兜底：亮度检测 + 叠标题字幕 + 明确标注原因
  assert.match(dashboard, /const PostCoverThumb:/);
  assert.match(dashboard, /inspectBrightness/);
  assert.match(dashboard, /setDarkFrame\(max < 18\)/);
  assert.match(dashboard, /源封面为黑帧/);
  assert.doesNotMatch(dashboard, /aspect-video/);
});

test('creator folder interactions give observable feedback and keyboard access', () => {
  // 主操作反馈：更新按钮有 busy / done / idle 三种可观察状态，完成后短暂显示「已更新」
  assert.match(dashboard, /data-state=\{busy \? 'busy' : done \? 'done' : 'idle'\}/);
  assert.match(dashboard, /setArchiveDone\(key\)/);
  assert.match(dashboard, /'已更新'/);
  assert.match(dashboard, /active:scale-\[0\.97\]/);
  // 音频播放器：可拖动进度 + 时间 + 空格键 + 按下反馈，是这一页最高频操作
  assert.match(dashboard, /data-testid="audio-player-seek"/);
  assert.match(dashboard, /role="slider"/);
  assert.match(dashboard, /handleSeekPointer/);
  assert.match(dashboard, /seekAudio/);
  assert.match(dashboard, /formatAudioTime/);
  assert.match(dashboard, /onTimeUpdate/);
  assert.match(dashboard, /onLoadedMetadata/);
  assert.match(dashboard, /active:scale-90/);
  // 播放器 portal 到 body，避开 workspace transform 祖先让 fixed 降级
  assert.match(dashboard, /createPortal\(/);
  assert.match(dashboard, /document\.body,/);
  // 键盘可达：/ 聚焦、Esc 逐层退出、空格播放/暂停、方向键快进
  assert.match(dashboard, /creatorSearchRef/);
  assert.match(dashboard, /event\.key === '\/'/);
  assert.match(dashboard, /event\.key === ' '/);
  assert.match(dashboard, /ArrowRight/);
  assert.match(dashboard, /kbd/);
});

test('filter and sort changes have a visible transition origin', () => {
  // 分段控件用一块会移动的滑块，而非旧灭新亮
  assert.match(dashboard, /data-testid="creator-filter-slider"/);
  assert.match(dashboard, /translateX\(100%\)/);
  // 筛选/排序/搜索变化时列表重新淡入，让状态切换有来路
  assert.match(dashboard, /data-testid="creator-post-grid"/);
  assert.match(dashboard, /key=\{`\$\{creatorPostMediaFilter\}-\$\{creatorPostSort\}-\$\{creatorPostQuery\}`\}/);
  assert.match(dashboard, /animate-fade-in/);
});

test('creator post card shows 4 data dimensions and opens detailed modal without jumping', () => {
  // 点击不是直接外跳：卡片主点击触发详情弹窗
  assert.match(dashboard, /onClick=\{\(\) => setSelectedPostDetail\(post\)\}/);
  // 跳转到平台是单独的按钮，并且阻止事件冒泡
  assert.match(dashboard, /data-testid=\{`post-open-external-\$\{post\.id\}`\}/);
  assert.match(dashboard, /e\.stopPropagation\(\);\s*openSource\(post\.url\);/);
  // 卡片展示紧凑 4 维指标（播、赞、评、藏）与时长标签
  assert.match(dashboard, />播</);
  assert.match(dashboard, />赞</);
  assert.match(dashboard, />评</);
  assert.match(dashboard, />藏</);
  assert.match(dashboard, /durationMs=\{post\.duration_ms\}/);
  // 居中详情弹窗包含 6 大核心数据维度矩阵、文案全文与��立外跳
  assert.match(dashboard, /data-testid="post-detail-modal"/);
  assert.match(dashboard, /data-testid="post-detail-open-external"/);
  assert.match(dashboard, /<span>干货收藏率<\/span>/);
  assert.match(dashboard, /<span>点赞数<\/span>/);
  assert.match(dashboard, /<span>评论数<\/span>/);
  assert.match(dashboard, /<span>收藏数<\/span>/);
  assert.match(dashboard, /<span>分享数<\/span>/);
  assert.match(dashboard, /<span>\{engagementRate \? '互动率' : '互动总量'\}<\/span>/);
  // API 类型定义包含 comments 与 duration_ms
  assert.match(api, /export interface TrendRadarCreatorPost/);
  assert.match(api, /comments\?: number;/);
  assert.match(api, /duration_ms\?: number;/);
});

test('creator archive streams live progress instead of blocking', () => {
  // 归档改为异步启动 + 轮询：POST 立即返回，进度来自 archive-status
  assert.match(api, /getTrendRadarCreatorArchiveStatus/);
  assert.match(api, /accepted\?: boolean/);
  assert.match(dashboard, /archivePollRef/);
  assert.match(dashboard, /getTrendRadarCreatorArchiveStatus/);
  assert.match(dashboard, /window\.setInterval/);
  // 进度条按阶段给可读文案；总量未知时不伪造百分比
  assert.match(dashboard, /data-testid="archive-progress"/);
  assert.match(dashboard, /正在拉取作品列表… 已获取/);
  assert.match(dashboard, /正在下载封面与音频…/);
  assert.match(dashboard, /animate-archive-slide/);
  // 卸载时清理定时器，切页后不再打接口
  assert.match(dashboard, /clearInterval/);
});

test('archive failure ledger client keeps the persisted contract', () => {
  // 台账落盘在 index.json 旁；没有台账时后端返回 total:0，不是错误。
  assert.match(api, /export interface TrendRadarArchiveFailure \{/);
  assert.match(api, /aweme_id: string;/);
  assert.match(api, /kind: string;/);
  assert.match(api, /reason: string;/);
  assert.match(api, /attempted_at\?: string;/);
  assert.match(api, /getTrendRadarCreatorArchiveFailures: \(platform: string, userID: string\) =>/);
  assert.match(api, /\/archive-failures`/);
  assert.match(api, /failures: TrendRadarArchiveFailure\[\]/);
});

test('archive failures panel lists who failed why and caps rendering at 50 rows', () => {
  // 弹窗沿用详情弹窗惯例：portal 到 body，Esc / 遮罩 / X 三路关闭
  assert.match(archiveFailuresPanel, /createPortal\(/);
  assert.match(archiveFailuresPanel, /document\.body,/);
  assert.match(archiveFailuresPanel, /event\.key === 'Escape'/);
  assert.match(archiveFailuresPanel, /event\.target === event\.currentTarget/);
  assert.match(archiveFailuresPanel, /data-testid="archive-failures-close"/);
  // 每条：作品 ID 可复制、类型中文、失败原因、时间
  assert.match(archiveFailuresPanel, /navigator\.clipboard\.writeText\(awemeID\)/);
  assert.match(archiveFailuresPanel, /cover: '封面'/);
  assert.match(archiveFailuresPanel, /audio: '音频'/);
  assert.match(archiveFailuresPanel, /failure\.reason \|\| '未记录原因'/);
  assert.match(archiveFailuresPanel, /formatTime\(failure\.attempted_at\)/);
  // 上限 50 条 + 明确的「还有 N 条未显示」
  assert.match(archiveFailuresPanel, /const MAX_RENDERED_FAILURES = 50;/);
  assert.match(archiveFailuresPanel, /failures\.slice\(0, MAX_RENDERED_FAILURES\)/);
  assert.match(archiveFailuresPanel, /还有 \{hidden\} 条未显示/);
  // 底部两个动作：重试全部 + 打开本地文件夹
  assert.match(archiveFailuresPanel, /data-testid="archive-failures-retry-all"/);
  assert.match(archiveFailuresPanel, /data-testid="archive-failures-open-folder"/);
  assert.match(archiveFailuresPanel, /重试全部/);
});

test('creator post grid renders in batches instead of the whole archive at once', () => {
  // 一个博主可能有几百条作品：首屏只画前 24 条，滚动到网格底部再按批追加。
  assert.match(dashboard, /const CREATOR_POST_FIRST_BATCH = 24;/);
  assert.match(dashboard, /const CREATOR_POST_BATCH_SIZE = 24;/);
  assert.match(dashboard, /const rendered = list\.slice\(0, renderedCount\);/);
  assert.match(dashboard, /data-rendered-count=\{rendered\.length\}/);
  // 哨兵进入视口触发下一批，并在卸载/重挂载时断开，避免 observer 悬挂
  assert.match(dashboard, /data-testid="creator-post-grid-sentinel"/);
  assert.match(dashboard, /new IntersectionObserver\(/);
  assert.match(dashboard, /return \(\) => observer\.disconnect\(\);/);
  // 没有 IntersectionObserver 的旧环境降级为全量渲染，而不是白屏
  assert.match(dashboard, /typeof IntersectionObserver === 'undefined'/);
  // 列表变化（换博主 / 筛选 / 搜索）必须把已渲染数量复位到首批
  assert.match(dashboard, /const listSignature = /);
  assert.match(dashboard, /prev\.signature === listSignature \? prev : \{ signature: listSignature, count: CREATOR_POST_FIRST_BATCH \}/);
});

test('creator auto archive is a douyin-only opt-in with rollback', () => {
  assert.match(api, /setTrendRadarCreatorAutoArchive/);
  assert.match(api, /\/auto-archive`/);
  assert.match(api, /auto_archive\?: boolean;/);
  assert.match(dashboard, /data-testid="creator-auto-archive"/);
  assert.match(dashboard, /data-enabled=\{String\(autoArchiveEnabled\)\}/);
  // 增量归档只支持抖音
  assert.match(dashboard, /\{selectedCreator\.platform === 'douyin' && \(\s*<button[\s\S]{0,200}creator-auto-archive/);
  // 失败必须回滚，不能留下一个假的开态
  assert.match(dashboard, /setAutoArchiveEnabled\(previous\)/);
  // 文案要说清代价：打开它会真的消耗 TikHub 额度
  assert.match(dashboard, /每 24 小时自动增量归档一次，会消耗 TikHub 额度/);
  // 请求进行中禁用，避免连点
  assert.match(dashboard, /disabled=\{autoArchiveSaving\}/);
});

test('a running archive is picked up again after a reload', () => {
  // 只靠点击才轮询的话，刷新后界面完全没有进度，再点「更新」只会拿到 409。
  assert.match(dashboard, /resumeCreatorArchive/);
  assert.match(dashboard, /activeCreator/);
  assert.match(dashboard, /const startPolling = useCallback/);
  // 只有 running 才接管，且静默接上不弹 toast
  assert.match(dashboard, /if \(status\.status !== 'running'\) return;/);
  // 同一个 key 不能开出第二个定时器
  assert.match(dashboard, /if \(archivePollRef\.current\[key\]\) return;/);
  // 接上之后仍走同一套收尾逻辑（toast + 刷新 summary）
  assert.match(dashboard, /const finish = useCallback/);
});

test('failure rows lead with the post title so users can tell which post failed', () => {
  assert.match(archiveFailuresPanel, /failure\.title/);
  assert.match(archiveFailuresPanel, /archive-failure-open-/);
  assert.match(archiveFailuresPanel, /douyinVideoURL/);
  // 没有标题时优雅退化，而不是留一片空白
  assert.match(archiveFailuresPanel, /未记录标题的作品/);
  // 后端把 title/cover_path 透传出来，前端类型必须收得住
  assert.match(api, /title\?: string;/);
  assert.match(api, /cover_path\?: string;/);
});

test('retrying failed media skips the list fetch entirely', () => {
  // 补 1 张封面不该再花 6 次分页调用：索引里已经存了每条作品的原始 URL。
  assert.match(dashboard, /mediaOnly: true/);
  assert.match(dashboard, /mediaOnly\?: boolean;/);
  assert.match(dashboard, /media_only: true/);
  assert.match(api, /media_only\?: boolean/);
  // 只补媒体时不能显示「本次抓到 0 条」这种像出错的文案
  assert.match(dashboard, /媒体补齐完成/);
  assert.match(dashboard, /result\.stopped === 'media_only'/);
});

test('creator folder shows the failure entry only when the ledger has rows', () => {
  assert.match(dashboard, /import ArchiveFailuresPanel from '\.\/trend\/ArchiveFailuresPanel'/);
  assert.match(dashboard, /<ArchiveFailuresPanel/);
  // 入口仅在 total > 0 时出现，并带 data-failures 供端到端测试定位
  assert.match(dashboard, /\{archiveFailureTotal > 0 && \(/);
  assert.match(dashboard, /data-testid="creator-folder-failures"/);
  assert.match(dashboard, /data-failures=\{archiveFailureTotal\}/);
  assert.match(dashboard, /失败 \{archiveFailureTotal\} 项/);
  // 进入文件夹拉一次台账
  assert.match(dashboard, /api\.getTrendRadarCreatorArchiveFailures\(platform, userID\)/);
  assert.match(dashboard, /void loadArchiveFailures\(selectedCreatorPlatform, selectedCreatorUserID\)/);
  // 归档结束后重拉台账，进度条/失败数都不能停在旧值
  assert.match(dashboard, /archiveSettledRef/);
  assert.match(dashboard, /finishedKey !== `\$\{selectedCreatorPlatform\}-\$\{selectedCreatorUserID\}`/);
  // 失败引导必须写在 hook 的完成 toast 里：toast 只有一个槽位，
  // 在 DashboardView 补发第二条会把「本次下载 封面/音频」整条盖掉。
  assert.match(dashboard, /失败 \$\{media\.failed\} 项可点「失败 \$\{media\.failed\} 项」重试/);
  assert.doesNotMatch(dashboard, /可点「失败 \$\{failed\} 项」重试/);
  // 重试全部 = 关弹窗 + 复用已有 archiveCreator 轮询与进度条，不额外确认
  assert.match(dashboard, /const retryArchiveFailures = \(\) => \{/);
  assert.match(dashboard, /onRetry=\{retryArchiveFailures\}/);
  assert.match(dashboard, /setArchiveFailuresOpen\(false\);[\s\S]*?void archiveCreator\(selectedCreator, \{ skipConfirm: true, mediaOnly: true \}\)/);
  // 用户已经在明确的修复动作里，不再弹二次原生确认
  assert.match(dashboard, /runOptions\?\.skipConfirm/);
});

test('unassigned history keywords can be moved into a pool without refetching', () => {
  // 入口只出现在有历史记录未归属词时，词可多选
  assert.match(dashboard, /trend-radar-unassigned/);
  assert.match(dashboard, /历史记录未归属词/);
  assert.match(dashboard, /toggleUnassignedTerm/);
  // 现有池或新建池二选一
  assert.match(dashboard, /＋ 新建赛道/);
  assert.match(dashboard, /assignNewNicheName/);
  // 提交走归池接口，成功后重算 summary/config，绝不发起抓取
  assert.match(api, /assignTrendRadarKeywords/);
  assert.match(api, /'\/api\/trend-radar\/assign-keyword'/);
  assert.match(dashboard, /api\.assignTrendRadarKeywords/);
  assert.match(dashboard, /await mutateSummary\(\);[\s\S]{0,200}await mutateConfig\(\);/);
  assert.match(dashboard, /未发起抓取/);
  // 归池函数体内不得出现抓取调用
  const assignBlock = dashboard.slice(
    dashboard.indexOf('const assignUnassignedKeywords'),
    dashboard.indexOf('const addSearchTerm'),
  );
  assert.doesNotMatch(assignBlock, /api\.callDataAPI/);
});
