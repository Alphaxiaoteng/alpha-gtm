export interface BrowserCandidate {
  id: string;
  label: string;
  engine: string;
  path: string;
  found: boolean;
  primary?: boolean;
  description?: string;
  icon?: string;
}

export interface Overview {
  version: string;
  data_dir: string;
  cdp_port: number;
  ui_port: number;
  platforms: number;
  accounts: number;
  browser_path: string;
  browser_found: boolean;
  browseros_path?: string;
  browser_engine?: string;
  browsers?: BrowserCandidate[];
  active_browser_id?: string;
  running: number;
  max_concurrent_browsers?: number;
  logged_in: number;
  logged_out: number;
  goos: string;
  goarch: string;
  server_time: string;
}

export interface BackendHealth {
  schema_version: number;
  ready: boolean;
  version: string;
}

export interface LocalDashboardSummary {
  contents: number;
  draft_contents: number;
  accounts: number;
  logged_in_accounts: number;
  publish_records: number;
  successful_records: number;
  pending_jobs: number;
  failed_jobs: number;
}

export interface Account {
  id: number;
  identityId: number;
  platformCode: string;
  kind: string;
  name: string;
  avatar: string;
  avatarSource?: 'observed' | 'platform_fallback' | 'legacy_remote' | string;
  avatarSha256?: string;
  avatarRefreshedAt?: string;
	avatarErrorCode?: string;
  accountIdentifier: string;
  loginState: string; // 'logged_in' | 'logged_out' | 'unknown'
  loginMode?: 'browser_profile_session' | 'manual_browser' | 'unknown' | string;
  authMethod?: 'qr' | 'phone' | 'oauth' | 'unknown' | string;
  lastCheckAt?: string;
  lastFocusedAt?: string;
  loginReasonCode?: string;
  loginEvidenceCode?: string;
  lastObservedAt?: string;
  remark: string;
  accountGroup: string;
  enabled: boolean;
  browserRunning: boolean;
  browserState?: string;
  cdpPort?: number;
  leaseOwner?: string;
  profileId?: string;
  browserType?: string;
  fingerprintSeed: number;
  fingerprintConfig: string;
  proxyServer?: string;
  proxyId?: number;
	tags: AccountTag[];
  spaceId?: number;
  spaceOwnership?: string;
  spaceState?: string;
  activeJobId?: number;
}

export interface EgoIdentity {
  id: number;
  name: string;
  avatar_url?: string;
  enabled: boolean;
  sort: number;
  profile_id?: string;
  profile_name?: string;
  created_at: string;
  updated_at: string;
}

export interface EgoProfile {
  id: string;
  name: string;
  isDefault: boolean;
}

export interface EgoProfilesResponse {
  available: boolean;
  profiles: EgoProfile[];
  error?: string;
}

/** Mirrors `db.EgoSpace` returned verbatim by GET /api/ego/spaces. */
export interface EgoSpace {
  id: number;
  identity_id: number;
  /** EgoLite runtime Space id (0 is a valid zero-based id). */
  space_id: number;
  /** EgoLite task/session id that bound this Space. */
  task_id: string;
  name: string;
  ownership: string;
  state: string;
  control_epoch: number;
  active_job_id: number;
  /** Last state write, not a heartbeat. */
  last_seen_at: string;
  error_code: string;
  created_at: string;
  updated_at: string;
}

/** One read-only frame of an Ego Space's current page (live preview). */
export interface EgoSpaceScreen {
  space_id: number;
  ownership: string;
  /** True when the user holds the Space: no frame is captured, never stolen back. */
  user_controlled: boolean;
  /** Base64 JPEG (no data: prefix); empty when user_controlled. */
  png: string;
  url: string;
  mime: string;
  captured_at: string;
}

/** One Space exactly as EgoLite has it now (the DB copy may be stale). */
export interface EgoLiveSpace {
  id: number;
  name: string;
  ownership: string;
  state: string;
}

export interface EgoSpaceTab {
  id: string;
  title: string;
  url: string;
}

export interface EgoSpaceTabs {
  space_id: number;
  ownership: string;
  user_controlled: boolean;
  current: string;
  tabs: EgoSpaceTab[];
}

export interface EgoLoginBatch {
  identity_id: number;
  platform_codes: string[];
  state: string;
}

export interface EgoBrowserOpenResult {
  identity_id: number;
  space_id: number;
  platform_codes: string[];
  total: number;
  remaining: number;
  next_offset: number;
  state: string;
  reused: boolean;
}

export interface EgoLoginVerification {
  identity_id: number;
  state: string;
  accounts: Array<Pick<Account, 'id' | 'identityId' | 'platformCode' | 'name' | 'avatar' | 'accountIdentifier' | 'loginState' | 'lastCheckAt'>>;
}

export interface EgoLoginQR {
  account_id: number;
  platform_code: string;
  state: 'qr_ready' | 'logged_in' | 'manual_takeover';
  reason_code?: 'QR_UNAVAILABLE';
  image_data_url?: string;
  expires_at?: string;
  page_origin?: string;
}

export interface EgoBookmarkImport {
  status: 'imported' | 'already_imported';
  mode: 'automatic' | 'manual' | 'none';
  path: string;
  version: number;
}

export interface EgoDoctor {
  available: boolean;
  missing: string[];
}

export interface EgoLegacyAccountPreview {
  available: boolean;
  count: number;
  items: Array<{
    platform_code: string;
    name: string;
    account_identifier: string;
    historical_login_state: string;
  }>;
}

export interface EgoLegacyAccountImport {
  imported: number;
  identities: EgoIdentity[];
  login_state: 'unknown';
  requires_egolite_login: true;
}

export interface EgoCalibrationResult {
  measured_capacity: number;
  default_concurrency: number;
  levels: Array<{ concurrency: number; failures: number; p95: number; stable: boolean }>;
}

export interface EgoSessionMonitorResult {
  checked: number;
  logged_in: number;
  logged_out: number;
  unknown: number;
  manual_takeover: number;
  failed: number;
}

export interface EgoPublishCapability {
	content_type: 'video' | 'graph_text' | 'article';
	composer_url: string;
	prepare_mode: 'manual_handoff' | 'automated';
	prepare_adapter_supported: boolean;
	submit_adapter_supported: boolean;
	verify_adapter_supported: boolean;
	submit_verified: boolean;
	verify_verified: boolean;
	native_schedule_adapter_supported: boolean;
	native_schedule_verified: boolean;
	native_schedule_min_lead_minutes: number;
}

export interface EgoPublishManifest {
	platform_code: string;
	name: string;
	creator_url: string;
	first_release: boolean;
	capabilities: EgoPublishCapability[];
	status: 'verified' | 'unverified';
}

export interface EgoSessionMonitorSettings {
  enabled: boolean;
  interval_minutes: number;
  concurrency: number;
  state: 'idle' | 'running';
  last_started_at: string;
  last_finished_at: string;
  last_error_code: string;
  last_result: Partial<EgoSessionMonitorResult>;
  updated_at: string;
}

export interface AccountTag {
	id: number;
	name: string;
	color: string;
	position: number;
	accountCount: number;
}

export interface AccountLoginObservation {
  accountId: number;
  platformCode: string;
  authState: 'logged_in' | 'logged_out' | 'unknown' | string;
  persistedLoginState: 'logged_in' | 'logged_out' | 'unknown' | string;
  browserState: string;
  loginMode: string;
  authMethod: string;
  reasonCode: string;
  evidenceCode: string;
  name?: string;
  accountIdentifier?: string;
  avatar?: string;
  avatarSource: string;
  avatarSha256?: string;
  avatarRefreshedAt?: string;
	avatarErrorCode?: string;
  profileChanged: boolean;
  pageOrigin?: string;
  observedAt: string;
  traceId: string;
	 sessionBackupState?: 'saved' | 'unavailable' | string;
}

export interface AccountLoginEvent extends Omit<AccountLoginObservation, 'platformCode' | 'avatar' | 'avatarRefreshedAt'> {
  id: number;
  createdAt: string;
}

export interface BrowserLifecycleEvent {
  id: number;
  eventType: string;
  reasonCode?: string;
  exitCode?: number;
  termSignal?: number;
  pid?: number;
  cdpPort?: number;
  createdAt: string;
}

export interface AccountObserverStatus {
  running: boolean;
  observing: boolean;
  intervalSeconds: number;
  lastStartedAt?: string;
  lastFinishedAt?: string;
  lastErrorCode?: string;
  lastCount: number;
}

export interface AccountSessionBackup {
  account_id: number;
  created_at: string;
  expires_at: string;
}

export interface AccountSessionBackupList {
  retentionDays: number;
  backups: AccountSessionBackup[];
}

export interface AccountSessionImportResult {
  accountId: number;
  platformCode: string;
  sessionImported: boolean;
  verificationRequired: boolean;
  message: string;
}

export interface ProxyEndpoint {
  id: number;
  name: string;
  server: string;
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}
export interface SafeSnapshot {
  schema_version: number;
  exported_at: string;
  accounts: { platform_code: string; name: string; account_group: string; remark: string }[];
  contents: ContentItem[];
  templates: PlatformTemplate[];
  publish_records: PublishRecord[];
  proxy_endpoints: ProxyEndpoint[];
  metric_summary: MetricSummary;
}

export interface DiagnosticSnapshotResult {
  ok: boolean;
  path: string;
  generated_at: string;
  counts: {
    accounts_total: number;
    accounts_logged_in: number;
    browser_instances_running: number;
    publish_jobs_active: number;
    publish_jobs_failed: number;
    queue_active: number;
    queue_failed: number;
    agent_runs_active: number;
    agent_runs_failed: number;
    notifications_unread: number;
    operation_events_total: number;
    browser_events_total: number;
    account_events_total: number;
    job_events_total: number;
    audit_events_total: number;
  };
}

export interface DiagnosticLogDirectory {
  path: string;
}

/** 设置页「存储」区：一类可回收的磁盘占用。 */
export interface StorageCategory {
  key: string;
  label: string;
  description: string;
  files: number;
  bytes: number;
  reclaimable: boolean;
  requires_confirm: boolean;
}

export interface StorageUsage {
  data_dir: string;
  total_bytes: number;
  categories: StorageCategory[];
}

export interface StorageCleanupResult {
  key: string;
  removed: number;
  bytes: number;
  detail: string;
}

export interface Platform {
  code: string;
  name: string;
  loginUrl: string;
  managerUrl: string;
  publishMode: 'unavailable' | 'manual_handoff' | 'api_credentials' | 'browser_prepare' | 'browser_automation';
  canCreatePublishJob: boolean;
  canAttemptAutoSubmit: boolean;
  publishSummary: string;
  category?: string;
  settingsSchema?: PlatformSettingsSchema;
}

export interface PlatformSettingsOption {
  value: string;
  label: string;
}

export interface PlatformSettingsField {
  key: string;
  label: string;
  type: 'enum' | 'boolean' | 'string' | 'manual';
  required: boolean;
  remember: boolean;
  default: string | boolean | null;
  description?: string;
  content_types?: Array<'video' | 'graph_text' | 'article'>;
  options?: PlatformSettingsOption[];
}

export interface PlatformSettingsSchema {
  platform_code: string;
  version: number;
  verification: 'console_verified' | 'contract_verified' | 'fixture_only';
  verified_at?: string;
  fields: PlatformSettingsField[];
}

export interface FingerprintConfig {
  platform: string;
  platformVersion: string;
  brand: string;
  brandVersion: string;
  hardwareConcurrency: number;
  gpuVendor: string;
  gpuRenderer: string;
  lang: string;
  acceptLang: string;
  timezone: string;
}

export interface XHSAccountSummary {
  account_name: string;
  platform_code: string;
  total_notes: number;
  last_collected: string;
  last_notes_count: number;
  collection_count: number;
  notes: XHSNote[];
}

export interface XHSNote {
  title: string;
  publish_time: string;
  view_count: string;
  like_count: string;
  comment_count: string;
  collect_count: string;
  share_count: string;
  note_id: string;
  url: string;
}

export interface PlatformStat {
  platform_code: string;
  accounts: number;
  total_notes: number;
  collections: number;
}

export interface XHSDashboardSummary {
  total_accounts: number;
  total_notes: number;
  total_collections: number;
  platform_breakdown?: PlatformStat[];
}

export interface TrendRadarSignal {
  niche_id: string;
  niche_name: string;
  search_term: string;
  platform: string;
  title: string;
  content_text?: string;
  content_source?: 'provider_text' | 'platform_caption' | 'asr_transcript' | string;
  creator?: string;
  creator_id?: string;
  url?: string;
  cover_url?: string;
  observed_at: string;
  /** 平台发布时间；缺失表示平台没给，不用抓取时间顶替。 */
  published_at?: string;
  first_seen_at: string;
  occurrences: number;
  is_new: boolean;
  play: number;
  likes: number;
  collects: number;
  shares: number;
  call_id?: number;
  video_id?: string;
  analysis_status?: 'pending' | 'running' | 'succeeded' | 'failed' | string;
  analysis?: TrendRadarVideoAnalysis;
}

export interface TrendRadarVideoAnalysisPrompt {
  title: string;
  text: string;
}

export interface TrendRadarVideoAnalysisResult {
  summary: string;
  viral_potential: string;
  hook: string;
  evidence: string[];
  structure: string[];
  audience: string;
  emotional_drivers: string[];
  topic_suggestions: string[];
  analysis_prompts: TrendRadarVideoAnalysisPrompt[];
  limitations: string[];
}

export interface TrendRadarVideoAnalysis {
  schema_version: number;
  video_id: string;
  source_call_id: number;
  status: 'running' | 'succeeded' | 'failed' | string;
  route?: 'agent' | 'settings_llm_fallback' | 'model' | string;
  provider: string;
  model: string;
  attempts?: Array<{
    route: string;
    provider: string;
    model?: string;
    task_id?: string;
    status: string;
    error_code?: string;
    duration_ms: number;
  }>;
  source_sha256: string;
  started_at: string;
  finished_at?: string;
  duration_ms: number;
  error_code?: string;
  error_message?: string;
  result: TrendRadarVideoAnalysisResult;
}

export interface TrendRadarNiche {
  id: string;
  name: string;
  description?: string;
  enabled: boolean;
  search_terms: string[];
  platforms: string[];
  /** 每个平台每轮最多入库的条数；xhs 默认 10，其余默认 30。 */
  platform_limits: Record<string, number>;
  state: 'ready' | 'not_synced' | 'disabled' | string;
  result_count: number;
  search_runs: number;
  last_synced_at?: string;
  last_run?: TrendRadarRun;
}

export interface TrendRadarRunCall {
  call_id?: number;
  term: string;
  platform: string;
  endpoint_id: string;
  status: 'succeeded' | 'failed' | string;
  error_code?: string;
  error_message?: string;
  item_count: number;
  kept_count: number;
  // 平台明确标了推广位的卡（xhs model_type=ads、抖音 commerce_info.is_ad），
  // 与引导卡、读不到分开计数：卡不是作品，也不再记成「读不到」。
  ad_count?: number;
  guide_count?: number;
  unreadable_count?: number;
  duration_ms: number;
  raw_path?: string;
}

export interface TrendRadarRun {
  id: string;
  niche_id: string;
  niche_name: string;
  platform: string;
  endpoint_id: string;
  terms: string[];
  state: 'running' | 'succeeded' | 'partial' | 'failed' | string;
  started_at: string;
  finished_at?: string;
  last_retry_at?: string;
  item_cap: number;
  max_calls: number;
  estimated_cost_usd: number;
  cost_basis?: string;
  total_calls: number;
  succeeded: number;
  failed: number;
  skipped: number;
  items: number;
  kept: number;
  ad_cards?: number;
  guide_cards?: number;
  unreadable?: number;
  calls: TrendRadarRunCall[];
}

export interface TrendRadarConfigNiche {
  id: string;
  name: string;
  description?: string;
  enabled: boolean;
  search_terms: string[];
  platforms: string[];
  platform_limits?: Record<string, number>;
}

export interface TrendRadarConfig {
  version: number;
  niches: TrendRadarConfigNiche[];
  updated_at?: string;
}

export interface TrendRadarAssignKeywordInput {
  terms: string[];
  target_niche_id?: string;
  new_niche?: {
    name: string;
    description?: string;
    platforms?: string[];
  };
}

export interface TrendRadarAssignKeywordResult {
  ok: boolean;
  config: TrendRadarConfig;
  assigned_terms: string[];
  target_niche_id: string;
  target_niche_name: string;
}

export interface TrendRadarCreatorPost {
  id: string;
  title: string;
  url?: string;
  cover_url?: string;
  cover_path?: string;
  audio_path?: string;
  play: number;
  likes: number;
  collects: number;
  shares: number;
  comments?: number;
  duration_ms?: number;
  observed_at?: string;
}

export interface TrendRadarArchiveProgress {
  phase: string;
  posts_fetched: number;
  posts_total_estimate: number;
  covers_done: number;
  covers_total: number;
  audio_done: number;
  audio_total: number;
  error?: string;
  started_at?: string;
  updated_at?: string;
}

export interface TrendRadarArchiveResult {
  ok: boolean;
  folder: string;
  index_path: string;
  pages: number;
  posts: number;
  // 本次实际抓到的条数。受限抓取（页数上限/游标卡住）会让它小于 posts，
  // 因为索引会与历史合并，而不是删掉没见到的旧作品。
  fetched?: number;
  new_posts: number;
  stopped?: string;
  call_ids: number[];
  media: { total: number; covers: number; audio: number; skipped: number; failed: number };
}

export interface TrendRadarArchiveStatus {
  status: 'idle' | 'running' | 'done' | 'error';
  progress: TrendRadarArchiveProgress | null;
  result: TrendRadarArchiveResult | null;
  error: string;
}

// 归档失败台账：落盘在 index.json 旁（archive-failures.json），进程重启后仍可查。
export interface TrendRadarArchiveFailure {
  aweme_id: string;
  kind: string;
  reason: string;
  attempted_at?: string;
  // 作品标题与本地封面：只给 aweme_id 的话用户根本认不出是哪条，
  // 得复制 ID 回去搜。后端从 index.json 回填，缺失时为空。
  title?: string;
  cover_path?: string;
}

export interface TrendRadarCreator {
  platform: string;
  user_id: string;
  name: string;
  avatar_url?: string;
  signature?: string;
  note?: string;
  folder_path: string;
  lookback_days: 7 | 14 | 30;
  page_size: 20;
  max_pages: 1 | 2 | 3 | 5;
  sort_type: 0 | 1;
  snapshot_count: number;
  last_pages: number;
  profile_url?: string;
  profile_checked_at?: string;
  followers: number;
  followers_delta: number;
  total_likes: number;
  total_likes_delta: number;
  last_check?: string;
  last_count: number;
  new_posts: number;
  total_engagement: number;
  engagement_delta: number;
  latest_posts: TrendRadarCreatorPost[];
  // 后台 worker 的博主级增量归档开关（每 24h 一次，默认关闭）。
  auto_archive?: boolean;
  state: 'ready' | 'stale' | 'never_synced' | 'unsupported' | 'failed' | string;
  message: string;
}

export interface TrendRadarSource {
  platform: string;
  name: string;
  mode: string;
  state: 'ready' | 'not_synced' | 'available_not_bound' | 'manual' | string;
  message: string;
}

// 博主对比视图：后端按天聚合的真实观测 + 归档快照里的真实粉丝点位。
// followers 为 null 表示那天没有可读到的粉丝快照（读不到 ≠ 0）。
export interface RadarCreatorComparisonDay {
  date: string;
  followers: number | null;
  likes: number;
  collects: number;
  shares: number;
  comments: number;
  observations: number;
}

export interface RadarCreatorComparisonEntry {
  key: string;
  platform: string;
  author: string;
  works: number;
  observations: number;
  follower_points: number;
  insufficient_followers: boolean;
  insufficient_series: boolean;
  days: RadarCreatorComparisonDay[];
}

export interface RadarCreatorComparison {
  window: { from: string; to: string; days: number };
  creators: RadarCreatorComparisonEntry[];
  empty: boolean;
}

export interface TrendRadarSummary {
  generated_at: string;
  last_synced_at?: string;
  snapshot_state: 'ready' | 'partial' | 'empty' | string;
  storage: string;
  config_updated_at?: string;
  search_runs: number;
  // 本轮所有快照里平台返回的卡：广告卡 / 引导卡 / 读不到（既不是作品也没有明确卡标记）。
  // 计数按整份响应算，不受各池保留名额（item_cap）影响。
  ad_cards?: number;
  guide_cards?: number;
  unreadable?: number;
  signals: TrendRadarSignal[];
  video_lookups: TrendRadarSignal[];
  niches: TrendRadarNiche[];
  creators: TrendRadarCreator[];
  sources: TrendRadarSource[];
}

export interface TrendRadarSchedule {
  id: string;
  name: string;
  niche_id: string;
  endpoint_id: string;
  params: Record<string, unknown>;
  interval_minutes: 60 | 360 | 1440;
  enabled: boolean;
  last_run_at?: string;
  next_run_at?: string;
  last_status?: string;
  last_call_id?: number;
  last_error?: string;
  created_at?: string;
  updated_at?: string;
}

export interface TrendRadarVideo {
  id: string;
  original_name: string;
  source_path: string;
  cover_path?: string;
  cover_url?: string;
  audio_path?: string;
  transcript_path?: string;
  transcription_state: string;
  transcription_note: string;
  sha256: string;
  size_bytes: number;
  duration_ms: number;
  created_at: string;
}

export interface TrendRadarRSSSource {
  id: string;
  name: string;
  url: string;
  fetch_full_text: boolean;
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
}

export interface TrendRadarRSSRun {
  id: string;
  source_id: string;
  source_name: string;
  feed_url: string;
  feed_path: string;
  item_count: number;
  full_text_saved: number;
  full_text_failed: number;
  duration_ms: number;
  status: string;
  error?: string;
  created_at: string;
}

export interface BatchRun {
  id: number;
  operationType: string;
  platformCode: string;
  status: string;
  totalItems: number;
  pendingCount: number;
  runningCount: number;
  successCount: number;
  failedCount: number;
  skippedCount: number;
  concurrency: number;
  dryRun: boolean;
}

export interface BatchItem {
  id: number;
  batchId: number;
  accountId: number;
  contentId: number;
  status: string;
  errorMessage: string;
  startedAt: string;
  finishedAt: string;
}

export interface FleetRun {
  id: number;
  operation: string;
  status: string;
  total: number;
  pending: number;
  running: number;
  success: number;
  failed: number;
  skipped: number;
  concurrency: number;
}

export interface FleetItem {
  id: number;
  account_id: number;
  status: string;
  current_step: string;
  error_code?: string;
}

export interface FleetRunDetail { run: FleetRun; items: FleetItem[]; }

// Safe operation timeline payload. The backend never includes tool arguments,
// URLs, CDP packets, credentials, or filesystem paths in these events.
export interface OperationEvent {
  id: number;
  trace_id: string;
  request_id?: string;
  task_id?: string;
  account_id?: number;
  platform_code?: string;
  component: string;
  event_type: string;
  step?: string;
  status?: string;
  level: string;
  error_code?: string;
  duration_ms: number;
  created_at: string;
}

export type ContentAssistAction = 'title' | 'summary' | 'rewrite' | 'check' | 'platform_adapt' | 'topic' | 'script' | 'storyboard' | 'metrics_review';

// Safe Fleet observatory payload. The backend intentionally omits CDP URL,
// profile path, PID, and lease owner from this local display contract.
export interface FleetInstance {
  accountId: number;
  accountName: string;
  platformCode: string;
  state: string;
  cdpPort: number;
  startedAt?: string;
  lastSeenAt?: string;
}

export interface ContentAssistResponse {
  result: string;
  source: 'local' | 'configured_model';
  external: boolean;
  citations?: KnowledgeCitation[];
  review_decision?: 'pass' | 'revise' | 'unknown';
}

export interface CreationDraftCommitResponse {
  content: ContentItem;
  review_decision: 'pass' | 'revise';
  review_snapshot_hash: string;
}

export type CreationModelRoleID = 'conversation' | 'copywriter' | 'reviewer';

export interface CreationModelRole {
  role: CreationModelRoleID;
  provider_id: AgentHubProviderID;
  model_id: string;
  updated_at: string;
}

export interface CreationModelRolesResponse {
  roles: CreationModelRole[];
  verified_models: Array<{ provider_id: AgentHubProviderID; model_id: string }>;
}

export interface CreationQualityReport {
  decision: 'pass' | 'revise' | 'unknown';
  issues: string[];
  source: 'local' | 'configured_model' | 'deterministic';
}

export interface CreationRun {
  id: string;
  session_id: string;
  action: 'topic' | 'draft' | 'review' | 'revise';
  provider_id: AgentHubProviderID;
  model_id: string;
  used_fallback: boolean;
  prompt_summary: string;
  result: string;
  citations_json: string;
  quality_json: string;
  created_at: string;
}

export interface CreationSession {
  id: string;
  title: string;
  topic_brief: string;
  style: string;
  platform: string;
  sop_stage: 'topic' | 'evidence' | 'copy' | 'script' | 'review' | 'save';
  knowledge_base_ids: number[];
  created_at: string;
  updated_at: string;
  runs: CreationRun[];
}

export interface CreationRunResponse {
  run_id: string;
  source: 'local' | 'configured_model';
  provider_id?: AgentHubProviderID;
  model_id?: string;
  used_fallback: boolean;
  result: string;
  citations: KnowledgeCitation[];
  quality: CreationQualityReport;
}

export type CreationWorkflowStage = 'topic' | 'copy' | 'script' | 'review';

export interface CreationWorkflow {
	 id: string;
	 conversation_id: string;
	 title: string;
	 platform?: string;
	 style?: string;
	 stage: CreationWorkflowStage;
	 knowledge_scopes: KnowledgeScope[];
	 accepted_artifacts: Record<string, string>;
	 created_at: string;
	 updated_at: string;
}

export interface CreationWorkflowView {
	 workflow: CreationWorkflow;
	 conversation: AgentHubConversation;
}

export interface CreationWorkflowRunResponse {
	 workflow: CreationWorkflow;
	 run: AgentHubRun;
	 citations: KnowledgeCitation[];
}

export interface KnowledgeBase {
  id: number;
  name: string;
  description: string;
  is_default: boolean;
  top_k: number;
  max_context_chars: number;
	document_count: number;
	chunk_count: number;
	latest_import?: KnowledgeImportRun;
  created_at: string;
  updated_at: string;
}

export interface KnowledgeImportRun {
	id: number;
	base_id: number;
	status: 'running' | 'completed' | 'completed_with_errors' | string;
	total_files: number;
	imported_files: number;
	skipped_files: number;
	failed_files: number;
	created_at: string;
	completed_at: string;
}

export interface KnowledgeCitation {
  chunk_id: number;
  base_name: string;
  source_name: string;
  heading_path: string;
  document_sha?: string;
}

export interface KnowledgeScope {
  base_id: number;
  source_prefix?: string;
}

export interface KnowledgeScopeOption extends KnowledgeScope {
  base_name: string;
  label: string;
}

export interface AgentKnowledgeTurn {
  id: number;
  conversation_id: string;
  run_id: string;
  query: string;
  scopes: KnowledgeScope[];
  citations: KnowledgeCitation[];
  used_citations: KnowledgeCitation[];
  output?: string;
  created_at: string;
}

export interface KnowledgeSearchResult extends KnowledgeCitation {
  base_id: number;
  document_id: number;
  content: string;
  score: number;
}

export interface KnowledgeImportItem {
  name: string;
  status: 'imported' | 'skipped' | 'failed';
  document_id?: number;
  error_code?: string;
  message?: string;
}

export interface KnowledgeImportResult {
  run_id: number;
  total: number;
  imported: number;
  skipped: number;
  failed: number;
  items: KnowledgeImportItem[];
}

export interface KnowledgeDocument {
  id: number;
  base_id: number;
  import_run_id: number;
  source_name: string;
  source_type: string;
  status: 'ready' | 'failed' | string;
  error_code: string;
  error_message: string;
  created_at: string;
  updated_at: string;
}

export interface JobEvent {
  id: number;
  job_id: number;
  level: 'debug' | 'info' | 'warn' | 'error' | string;
  step?: string;
  event_type: string;
  message?: string;
  screenshot_path?: string;
  dom_snapshot_path?: string;
  trace_id?: string;
  request_id?: string;
  task_id?: string;
  duration_ms: number;
  error_code?: string;
  error_class?: string;
  retryable: boolean;
  schema_version: number;
  created_at: string;
}

export interface PublishJobSummary {
  id: number;
  batch_id: number;
  plan_id: number;
  plan_target_id: number;
  content_id: number;
  account_id: number;
  asset_id: number;
  platform_code: string;
  content_type: string;
  mode: string;
  status: string;
  current_step: string;
  retry_count: number;
  max_retries: number;
  error_code: string;
  error_message: string;
  trace_id: string;
  cancel_requested_at: string;
  snapshot_hash?: string;
  submit_attempted: boolean;
  submit_outcome?: string;
  created_at: string;
  can_cancel: boolean;
}

export interface PublishJobDetail extends PublishJobSummary {
  can_retry: boolean;
  retry_blocked_by_submit: boolean;
  creator_backend_verified: boolean;
  evidence_screenshot_saved: boolean;
  events: JobEvent[];
}

export interface PublishJobAction {
  action: 'cancelled' | 'cancel_requested' | 'queued';
  queue_task_id?: number;
  job: PublishJobDetail;
}

export interface PublishBackendVerificationResult {
  status: string;
  step: string;
  error_code: string;
}

export interface BrowserInstance {
  profileId: string;
  accountId: number;
  state: string; // 'starting' | 'ready' | 'busy' | 'degraded' | 'crashed' | 'stopped'
  cdpPort: number;
  cdpUrl: string;
  pid: number;
  startedAt?: string;
  lastSeenAt?: string;
  leaseOwner?: string;
  running?: boolean;
}

export interface BrowserBatchStopResponse {
  requested: number;
  stopped: number;
  failed: number;
  results: Array<{
    account_id: number;
    stopped: boolean;
    error_code?: string;
  }>;
}

export interface Notification {
  id: number;
  type: string;
  level: 'debug' | 'info' | 'warn' | 'error' | string;
  title: string;
  body: string;
  read: boolean;
  related_type: string;
  related_id: number;
  created_at: string;
}

export type FeedbackStatus = '待处理' | '处理中' | '已完成' | '不采纳';

export interface FeedbackItem {
  feedback_id: string;
  content: string;
  status: FeedbackStatus;
  category: '未分类' | 'Bug' | '建议' | '想法' | '其他' | string;
  developer_reply: string;
  created_at: string;
  updated_at: string;
  last_synced_at: string;
}

export interface FeedbackListResponse {
  items: FeedbackItem[];
  configured: boolean;
  sync_error?: string;
}

export type NotificationChannelKind = 'feishu' | 'dingtalk' | 'webhook' | 'email';

export interface NotificationChannelSetting {
  kind: NotificationChannelKind;
  enabled: boolean;
  configured: boolean;
  smtp_host?: string;
  smtp_port?: number;
  smtp_security?: 'tls' | 'starttls';
  smtp_username?: string;
  email_from?: string;
  email_to?: string[];
}

export interface NotificationSettings {
  reminder_interval_hours: 12 | 24;
  channels: NotificationChannelSetting[];
}

export type NotificationChannelUpdate = {
  enabled: boolean;
  webhook_url?: string;
  smtp_host?: string;
  smtp_port?: number;
  smtp_security?: 'tls' | 'starttls';
  smtp_username?: string;
  password?: string;
  email_from?: string;
  email_to?: string[];
};

export interface ContentItem {
  id: number;
  type: 'article' | 'graph_text' | 'video';
  title: string;
  caption: string;
  body: string;
  tags_json: string;
  status: 'draft' | 'published' | 'archived';
  publish_count: number;
  planned_at?: string;
  cover_media_id?: number;
  cover_kind?: string;
  cover_mime_type?: string;
}

// An editorial Studio → Media handoff. It is never a local scheduled task and
// contains no account, platform, approval, or browser state.
export interface StudioScheduleHandoff {
  id: number;
  workflow_id: string;
  content_id: number;
  script_id?: number;
  revision: number;
  media_mode: 'article' | 'graph_text' | 'video';
  media_ids: number[];
  cover_media_id?: number;
  native_video_cover?: boolean;
  canonical_hash: string;
  planned_at_utc: string;
  timezone: string;
  original_local_time: string;
  intent_hash: string;
  status: 'ready' | 'frozen' | 'superseded' | 'cancelled' | 'expired';
  frozen_plan_id?: number;
  snapshot_hash?: string;
  created_at: string;
  updated_at: string;
}

export interface AccountAsset {
  id: number;
  account_id: number;
  platform_code: string;
  asset_type: string;
  name: string;
  external_id: string;
  avatar: string;
  connected_asset_id: number;
  connected_asset_name: string;
  enabled: boolean;
  last_seen_at: string;
}

export interface MetaTargetDiscovery {
  accountId: number;
  facebookPages: string[];
  instagramAccounts: string[];
  warnings: string[];
  complete: boolean;
}

export interface PlatformTemplate {
  id: number;
  platform_code: string;
  name: string;
  description: string;
  settings_json: string;
  enabled: boolean;
  is_default: boolean;
  source: string;
}

export interface MediaAsset {
  id: number;
  content_id: number;
	library_id: number;
  role: 'content' | 'cover';
  position: number;
  kind: 'image' | 'video' | 'audio' | string;
  file_path: string;
  original_path: string;
  mime_type: string;
  size_bytes: number;
  sha256: string;
  width: number;
  height: number;
  duration_ms: number;
}

export interface PublishPlanTargetSnapshot {
  account_id: number;
  asset_id: number;
  platform_code: string;
  variant_id?: number;
  title: string;
  caption: string;
  body: string;
  tags_json: string;
  settings_json: string;
  publish_at?: string;
}

export interface PublishPlanMediaSnapshot {
  id: number;
  kind: string;
  role: string;
  position: number;
  sha256: string;
  size_bytes: number;
}

export interface CompliancePolicy {
  ai_generated: boolean;
  ai_disclosure: string;
  commercial_content: boolean;
  visibility: 'public';
  comments: boolean;
  download: boolean;
  duet: boolean;
  stitch: boolean;
  youtube_audience: 'not_made_for_kids';
  retention: 'keep';
}

export interface PublishPlanComplianceField {
  policy_key: string;
  platform_key?: string;
  value: unknown;
  status: 'applied' | 'manual' | 'unsupported';
  reason?: string;
}

export interface PublishPlan {
  id: number;
  contentId: number;
  mediaMode: 'article' | 'graph_text' | 'video';
  snapshotHash: string;
  status: string;
  concurrency: 1;
  snapshot: {
    version: number;
    media_mode: string;
    concurrency: 1;
    media: PublishPlanMediaSnapshot[];
    targets: (PublishPlanTargetSnapshot & { compliance_fields?: PublishPlanComplianceField[] })[];
    compliance_policy?: CompliancePolicy;
    content: { id: number; type: string; title: string; caption: string; body: string; tags_json: string; planned_at?: string };
  };
  targetStates?: {
    accountId: number;
    assetId: number;
    platformCode: string;
    jobId?: number;
    status: string;
    errorCode?: string;
    submitAttempted: boolean;
  }[];
}

export interface PublishPlanSummary {
  id: number;
  contentId: number;
  contentTitle: string;
  mediaMode: 'article' | 'graph_text' | 'video';
  status: string;
  targetCount: number;
  platformCodes: string[];
  createdAt: string;
}

export interface PublishPlanApproval {
  approvalId: string;
  nonce: string;
  expiresAt: string;
}

export interface PublishPlanExecution {
  planId: number;
  status: string;
  trace: string;
  targets: { accountId: number; platformCode: string; jobId: number; status: string; step: string; errorCode?: string }[];
}

export interface PublishPlanManagerVerification {
  inspection: {
    planId: number;
    jobId: number;
    accountId: number;
    platformCode: string;
    location: string;
    evidence: {
      inspectionStatus?: string;
      matchingTitle?: boolean;
      publishedState?: boolean;
      observedStatus?: string;
      postURL?: string;
      externalPostID?: string;
    };
  };
  publish_record?: PublishRecord;
}

export interface MediaLibraryItem {
  id: number;
  file_path: string;
  sha256: string;
  size_bytes: number;
  mime: string;
  width: number;
  height: number;
  duration_ms: number;
  purpose: 'shared' | 'video' | 'graph_text' | 'article';
  created_at: string;
}

export interface ContentLibraryConfig {
  root_dir: string;
  video_dir: string;
	image_dir: string;
}

export interface AIConfigPayload {
  agent_api_provider: string;
  agent_api_base_url: string;
  agent_api_key: string;
  agent_model: string;
  agent_api_verified?: boolean;
  coding_model: string;
  plan_model: string;

  image_api_provider: string;
  image_api_base_url: string;
  image_api_key: string;
  image_api_configured?: boolean;
  image_model: string;

  video_api_provider: string;
  video_api_base_url: string;
  video_api_key: string;
  video_api_configured?: boolean;
  video_model: string;

  media_api_provider?: string;
  media_api_base_url?: string;
  media_api_key?: string;
}

export interface AITestResult {
  ok: boolean;
  status_code?: number;
  latency_ms?: number;
  message?: string;
  error?: string;
}

export type DataAPIBinding =
  | 'bound'
  | 'local_derived'
  | 'available_not_bound'
  | 'blocked_by_binding'
  | 'browser_only';

export interface DataAPIEndpoint {
  id?: string;
  name?: string;
  method?: string;
  path?: string;
  docs_url?: string;
  cost_hint?: string;
  fixed_params?: Record<string, unknown>;
  params?: DataAPIEndpointParameter[];
}

export interface DataAPIEndpointParameter {
  name: string;
  label: string;
  type: 'string' | 'integer' | 'boolean' | 'select';
  required?: boolean;
  default?: unknown;
  options?: string[];
  placeholder?: string;
  help?: string;
}

export interface DataAPICapability {
  capability:
    | 'keyword_search'
    | 'content_detail'
    | 'creator_profile'
    | 'creator_posts'
    | 'topic_selection'
    | 'benchmark'
    | 'circle_analysis';
  label: string;
  binding: DataAPIBinding;
  message: string;
  endpoints?: DataAPIEndpoint[];
}

export interface DataAPIPlatform {
  platform: string;
  name: string;
  capabilities: DataAPICapability[];
}

export interface DataAPIConfig {
  provider: 'tikhub';
  base_url: string;
  base_options: { region: 'mainland' | 'global'; label: string; base_url: string }[];
  configured: boolean;
  active: boolean;
  state: 'credential_missing' | 'awaiting_test' | 'test_failed' | 'active';
  test_status: 'untested' | 'passed' | 'failed';
  tested_at?: string;
  error_code?: string;
  test_endpoint: DataAPIEndpoint;
  endpoint_catalog: DataAPIPlatform[];
}

export interface TikHubAPICall {
  id: number;
  trace_id: string;
  platform_code: string;
  endpoint_id: string;
  capability: string;
  method: string;
  path: string;
  parameters_json: string;
  status: 'running' | 'succeeded' | 'failed' | 'storage_failed';
  error_code?: string;
  provider_code?: string;
  provider_message?: string;
  item_count: number;
  response_bytes: number;
  raw_path?: string;
  started_at: string;
  finished_at?: string;
  duration_ms: number;
  created_at: string;
}

export interface AIImageConfig {
  provider: string;
  model: string;
  image_model: 'agnes-image-2.0-flash' | 'agnes-image-2.1-flash';
  video_model: 'agnes-video-v2.0';
  asset_dir: string;
  provider_status: 'credential_missing' | 'configured_unverified' | 'prompt_only' | string;
  credential_configured: boolean;
  /** Optional until the custom-provider migration lands; never includes a key. */
  base_url?: string;
}

/** Custom image provider settings written by the connection card. */
export interface AIImageProviderConfigInput {
  provider?: string;
  base_url?: string;
  model?: string;
  image_model?: string;
  video_model?: string;
  asset_dir?: string;
}

export interface AIImageGenerateResult {
  ok: boolean;
  name: string;
  url: string;
  mime: string;
  bytes: number;
}

/**
 * A persona for the in-app agent. `capabilities` stays empty in this round:
 * tool grants need a reviewed approval bridge before the UI may expose them.
 */
/** One Bot-to-Bot handoff: a real child conversation + run. */
/** Local → Feishu Base mirror status (tokens stay in lark-cli). */
export interface FeishuSyncModule {
  id: string;
  label: string;
  table_id: string;
  synced: number;
  /** 本地可同步的总行数；大于 synced 说明还有没进飞书的。 */
  local_total?: number;
  /** 单轮同步上限；0 或缺失表示这个模块不截断。 */
  caps_at?: number;
}

export interface FeishuSyncResult {
  module: string;
  created: number;
  updated?: number;
  skipped: number;
  error?: string;
}

export interface FeishuSyncStatus {
  configured: boolean;
  url?: string;
  app_token?: string;
  modules: FeishuSyncModule[];
  syncing: boolean;
  last_results: FeishuSyncResult[];
  last_error?: string;
  error?: string;
}

export interface AgentDelegation {
  id: string;
  parent_conversation_id: string;
  from_bot_id?: string;
  to_bot_id: string;
  child_conversation_id: string;
  child_run_id: string;
  task: string;
  status: 'running' | 'completed' | 'failed' | 'cancelled';
  result?: string;
  error_code?: string;
  created_at: string;
  updated_at: string;
}

/** Conversation timeline entry for one tool call (params/results redacted server-side). */
export interface AgentToolCallDetail {
  id: string;
  tool_name: string;
  workspace?: string;
  risk_level?: string;
  status: string;
  impact_preview?: string;
  params?: unknown;
  result?: unknown;
  error_code?: string;
  created_at: string;
  updated_at: string;
  duration_ms: number;
}

export interface AgentBot {
  id: string;
  name: string;
  role?: string;
  instructions?: string;
  model?: string;
  avatar?: string;
  color?: string;
  effort?: string;
  kickoff?: string;
  capabilities?: string[];
  /** Floats the bot to the top of the rail. */
  pinned?: boolean;
  /** The roster's default bot; at most one bot carries it. */
  is_primary?: boolean;
  /** Rail section ("团队"). Empty means the rail's 其他 bucket. */
  group_name?: string;
  /** Do-not-disturb marker shown in the rail; conversations are unaffected. */
  muted?: boolean;
  created_at?: string;
  updated_at?: string;
}

/** One rail row: the persona plus its unread/last-message summary. */
export interface AgentBotRailItem extends AgentBot {
  unread?: number;
  last_message?: string;
  last_message_at?: string;
}

/** The default Alpha Agent threads (conversations without a bot persona). */
export interface AgentMainRail {
  unread: number;
  last_message: string;
  last_message_at: string;
}

export interface AgentBotRail {
  bots: AgentBotRailItem[];
  agent: AgentMainRail;
  unread_total: number;
}

export type AgentBotInput = Pick<AgentBot, 'name'> & Partial<Omit<AgentBot, 'id' | 'created_at' | 'updated_at'>>;

/** 1:1 mirror of the local DSH provider/model catalog (~/.dsh/cordis.patch.yml). */
export interface DshCatalogModel {
  id: string;
  name?: string;
  context_window?: number;
  max_tokens?: number;
}

export interface DshCatalogProvider {
  id: string;
  label: string;
  api?: string;
  base_url?: string;
  api_key_env?: string;
  models: DshCatalogModel[];
}

export interface DshCatalog {
  default_provider: string;
  default_model: string;
  providers: DshCatalogProvider[];
  model_ids: string[];
  source_path: string;
}

export interface AIImageAsset {
  id?: number;
  name: string;
  kind: 'image' | 'video';
  url?: string;
  sha256: string;
  mime_type: string;
  /** Compatibility with asset records created before image/video unification. */
  mime?: string;
  width: number;
  height: number;
  duration_ms: number;
  size_bytes: number;
  created_at: string;
}

export interface AIImagePromptPreset {
  id: string;
  title: string;
  text: string;
}

export interface AIImageCanvasNode {
  id: string;
  kind: 'prompt' | 'image' | 'video';
  text?: string;
  asset_name?: string;
  ref_label?: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

export type AIImageGenerationMode = 'text_to_image' | 'image_to_image' | 'text_to_video' | 'image_to_video';
export type AIImageReferenceRole = 'product' | 'model' | 'style' | 'first_frame';

export interface AIImageGenerationReferenceInput {
  asset_name: string;
  role: AIImageReferenceRole;
}

export interface AIImageGenerationReference extends AIImageGenerationReferenceInput {
  /** Frozen by the backend after resolving the managed local asset. */
  sha256: string;
}

export interface AIImageGenerationRequest {
	request_id: string;
  kind: 'image' | 'video';
  mode: AIImageGenerationMode;
  model: string;
  output_size: string;
  duration_seconds?: 5 | 10;
  prompt: string;
  references: AIImageGenerationReferenceInput[];
}

export interface AIImageBatchGenerationRequest extends AIImageGenerationRequest {
  count: number;
}

export interface AIImageBatchGeneration {
  batch_id: string;
  count: number;
  items: AIImageGeneration[];
}

export interface AIImageGeneration {
  id: number;
	request_id: string;
  kind: 'image' | 'video';
  mode: AIImageGenerationMode;
  model: string;
  output_size: string;
  duration_seconds: 0 | 5 | 10;
  references: AIImageGenerationReference[];
  status: 'queued' | 'running' | 'succeeded' | 'failed' | string;
  progress: number;
  error_code?: string;
  output_asset?: AIImageAsset;
  created_at?: string;
  updated_at?: string;
}

export interface AIImageWorkspace {
  prompts: AIImagePromptPreset[];
  nodes: AIImageCanvasNode[];
  viewport: { x: number; y: number; zoom: number };
}

export interface AIImageProjectSummary {
  id: number;
  name: string;
  created_at: string;
  updated_at: string;
}

export interface AIImageProject extends AIImageProjectSummary {
  workspace: AIImageWorkspace;
}

export interface AIImagePlanRequest {
  mode: 'single' | 'grid-9';
  category: 'earring' | 'ring' | 'bracelet' | 'necklace';
  composition?: 'worn' | 'still_life';
  variant?: 'single' | 'integrated_double';
  product_asset_name: string;
  model_asset_name?: string;
  prompt?: string;
  attachment_type?: 'pierced' | 'clip_on';
  bracelet_type?: 'closed' | 'open' | 'cuff';
}

export interface AIImagePlanResult {
  status: 'prompt_only' | 'invalid_input' | 'provider_unavailable' | 'generation_failed' | 'generated' | 'manual_review';
  mode: AIImagePlanRequest['mode'] | null;
  category: AIImagePlanRequest['category'] | null;
  composition: 'worn' | 'still_life' | null;
  variant: 'single' | 'integrated_double' | null;
  template_id: string | null;
  template_version: string | null;
  reference_count: number;
  prompt: string | null;
  negative_prompt: string | null;
  provider: string | null;
  artifact_paths: string[];
  error_code: string | null;
  manual_action: string | null;
}

export interface StudioScript {
  id: number;
  title: string;
  body: string;
  content_type: 'video' | 'graph_text' | 'article';
  created_at: string;
  updated_at: string;
}

export interface ClipJob {
  id: number;
  source_media_id: number;
  script_id: number;
  source_path: string;
  output_path: string;
  start_ms: number;
  end_ms: number;
  plan_json: string;
  status: 'planned' | 'completed' | 'failed' | string;
  error_code: string;
  error_message: string;
  created_at: string;
  updated_at: string;
  completed_at: string;
}

export interface LoginSession { id: number; account_id: number; status: string; error_code: string; started_at: string; completed_at: string; expires_at: string; }
export interface MetricPlatform {
  platform_code: string;
  samples: number;
  views: number;
  likes: number;
  comments: number;
  shares: number;
  favorites: number;
  followers: number;
  posts: number;
  engagement_rate: number;
}
export interface MetricSummary {
  samples: number;
  views: number;
  likes: number;
  comments: number;
  shares: number;
  favorites: number;
  followers: number;
  posts: number;
  interactions: number;
  engagement_rate: number;
  platforms: MetricPlatform[];
}
export interface PublishRecord { id:number; job_id:number; content_id:number; account_id:number; asset_id:number; platform_code:string; content_type:string; status:string; source:string; post_url:string; external_post_id:string; }
export interface PublishContentRecord {
  id: number;
  job_id: number;
  account_id: number;
  platform_code: string;
  content_type: string;
  account_name: string;
  title: string;
  remote_state: 'scheduled' | 'published' | 'delete_pending' | 'delete_failed' | 'deleted';
  scheduled_at: string;
  post_url: string;
  external_post_id: string;
  error_code: string;
  verified_at: string;
  deleted_at: string;
}
export interface ManualPublishJob { id:number; content_id:number; account_id:number; asset_id:number; platform_code:string; content_type:string; status:'manual_takeover'|'awaiting_review'; current_step:string; created_at:string; }

export interface ToolCallResult {
  schema_version: number;
  request_id: string;
  result: { ok: boolean; tool: string; message?: string; error?: string; data?: Record<string, unknown> };
}

export interface ShopListingPlanSummary {
  plan_id: number;
  identity_id: number;
  account_id: number;
  platform_code: string;
  snapshot_hash: string;
  status: string;
  concurrency: number;
  item_count: number;
  created_at: string;
  frozen_at: string;
  updated_at: string;
  error_code: string;
}

export interface ShopListingPlanList {
  plans: ShopListingPlanSummary[];
  count: number;
}

export interface DataLinkSource {
  id: number;
  kind: 'sqlite';
  name: string;
  original_name: string;
  file_size: number;
  sha256: string;
  status: 'ready' | 'failed';
  error_code: string;
  created_at: string;
  updated_at: string;
}

export interface DataLinkColumn { name: string; declared_type: string; not_null: boolean; primary_key: boolean }
export interface DataLinkObject { name: string; type: 'table' | 'view'; columns: DataLinkColumn[] }
export interface DataLinkInspection {
  source: DataLinkSource;
  objects: DataLinkObject[];
  preview: Array<Record<string, unknown>>;
  preview_columns: string[];
  schema_fingerprint: string;
  suggested_mapping: Record<string, string>;
}
export interface DataLinkPreflight {
  schema_fingerprint: string;
  estimated_rows: number;
  columns: string[];
  suggested_mapping: Record<string, string>;
  warnings: string[];
}
export interface DataLinkQuality {
  missing: Record<string, number>;
  type_mismatch: Record<string, number>;
  negative: Record<string, number>;
  duplicates: number;
  date_min?: string;
  date_max?: string;
  issues: string[];
}
export interface DataLinkSnapshot {
  id: number;
  source_id: number;
  source_name: string;
  object_name: string;
  schema_fingerprint: string;
  mapping: Record<string, string>;
  status: 'running' | 'completed' | 'failed';
  row_count: number;
  byte_count: number;
  checksum: string;
  quality: DataLinkQuality;
  truncated: boolean;
  knowledge_base_id: number;
  knowledge_document_id: number;
  confirmed_at: string;
  collected_at: string;
  error_code: string;
}
export interface DataLinkState { sources: DataLinkSource[]; snapshots: DataLinkSnapshot[] }

export interface CreatorMetricSnapshot {
  id: number;
  account_id: number;
  account_name: string;
  platform_code: 'douyin' | 'xhs' | 'wechat_video' | 'kuaishou' | 'tiktok' | 'youtube' | 'meta' | 'wechat_mp' | 'csdn';
  snapshot_date: string;
  window_days: number;
  range_verified: boolean;
  followers: number;
  followers_verified: boolean;
  views: number;
  likes: number;
  comments: number;
  shares: number;
  favorites: number;
  posts: number;
  evidence_code: string;
  collected_at: string;
}

export interface CreatorMetricDay {
  date: string;
  accounts: number;
  views: number;
  likes: number;
  comments: number;
  shares: number;
  favorites: number;
  posts: number;
}

export interface CreatorMetricAccountDay {
  account_id: number;
  platform_code: CreatorMetricSnapshot['platform_code'];
  date: string;
  views: number;
  likes: number;
  comments: number;
  shares: number;
  favorites: number;
  posts: number;
}

export interface CreatorMetricPlatformSummary {
  platform_code: CreatorMetricSnapshot['platform_code'];
  accounts: number;
  views: number;
  interactions: number;
  posts: number;
  engagement_rate: number;
}

export interface CreatorPostMetric {
  id: number;
  snapshot_id: number;
  account_id: number;
  platform_code: CreatorMetricSnapshot['platform_code'];
  external_post_id: string;
  title: string;
  published_at: string;
  post_url: string;
  views: number;
  likes: number;
  comments: number;
  shares: number;
  favorites: number;
}

export interface CreatorMetricOverview {
  window_days: number;
  from_date: string;
  to_date: string;
  latest: CreatorMetricSnapshot[];
  daily: CreatorMetricDay[];
  account_daily?: CreatorMetricAccountDay[];
  platforms: CreatorMetricPlatformSummary[];
  top_posts: CreatorPostMetric[];
  collections: CreatorMetricCollectionState[];
  insights: string[];
}

export interface MetricsCollectionSchedule {
  enabled: boolean;
  interval_hours: number;
  state: string;
  last_started_at: string;
  last_finished_at: string;
  last_result: { eligible?: number; collected?: number; skipped?: number; failed?: number };
  updated_at: string;
}

export interface MetricsCollectionRunResult {
  eligible: number;
  collected: number;
  skipped: number;
  failed: number;
}

export interface CreatorMetricPreferences {
  history_days: 7 | 30 | 90;
  retention_days: 30 | 90 | 180 | 365;
  updated_at: string;
}

export interface CreatorMetricCollectionState {
  account_id: number;
  account_name: string;
  platform_code: CreatorMetricSnapshot['platform_code'];
  state: 'observed' | 'skipped' | 'unknown' | 'manual_takeover' | 'not_collected';
  error_code?: string;
  evidence_code?: string;
  range_verified: boolean;
  post_count: number;
  collected_at: string;
  updated_at: string;
}

export interface CreatorMetricCollectionRow {
  account_id: number;
  platform_code?: CreatorMetricSnapshot['platform_code'];
  ok: boolean;
  state?: 'observed' | 'skipped' | 'unknown' | 'manual_takeover';
  error_code?: string;
  range_verified?: boolean;
  post_count?: number;
}

export interface CreatorMetricBatchResult {
  ok: boolean;
  error?: string;
  all_observed: boolean;
  accounts: CreatorMetricCollectionRow[];
}

export interface SocialPlatformWorkflow {
  platform_code: string;
  platform_name: string;
  data_workflow: 'automated_snapshot' | 'manual_handoff';
  data_steps: string[];
  publish_workflow: 'prepare_then_submit_approval' | 'freeze_then_manual_handoff' | 'manual_handoff';
  publish_steps: string[];
  submit_verified: boolean;
}

const TOKEN_KEY = 'alpha_nexus_token';

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY);
  } catch {
    return null;
  }
}

export function setToken(token: string | null) {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    /* ignore */
  }
}

/**
 * 统一 API 错误。
 *
 * 后端已通过 json_error.go 把所有 4xx/5xx plain text 响应统一成
 * {ok:false,errorCode,error_code,message,error,requestId}，本类负责消费它。
 *
 * 保持 extends Error 是为了兼容既有 428 处 `error.message` 读取：
 * message 仍是人类可读文案，额外挂上 errorCode / requestId / status 供分支处理。
 */
export class ApiError extends Error {
  readonly errorCode: string;
  readonly status: number;
  readonly requestId: string;
  readonly url: string;
  /** 是否值得重试（网络错误 / 5xx / 429），4xx 参数类错误不重试。 */
  readonly retryable: boolean;

  constructor(params: {
    message: string;
    errorCode: string;
    status: number;
    requestId?: string;
    url: string;
    retryable?: boolean;
  }) {
    super(params.message);
    this.name = 'ApiError';
    this.errorCode = params.errorCode;
    this.status = params.status;
    this.requestId = params.requestId || '';
    this.url = params.url;
    this.retryable = params.retryable ?? isRetryableStatus(params.status);
    // 保持原型链（TS 降级编译时 instanceof 需要）
    Object.setPrototypeOf(this, ApiError.prototype);
  }
}

/** 给使用者展示的文案：有 requestId 时附上，便于对照后端日志排障。
 *  fallback 用于 reason 不是 Error/无 message 时的兜底（各视图原 errorMessage()
 *  就是这种 (reason, fallback) 形态，收敛到这里统一）。 */
export function apiErrorText(error: unknown, fallback = '未知错误'): string {
  if (error instanceof ApiError) {
    const text = error.requestId ? `${error.message}（请求号 ${error.requestId}）` : error.message;
    return text || fallback;
  }
  if (error instanceof Error) return error.message || fallback;
  if (typeof error === 'string' && error.trim()) return error;
  return fallback;
}

/** 供调用方判断是否值得提示"重试"而不必比对文案。 */
export function isRetryableStatus(status: number): boolean {
  if (status === 429) return true;
  return status >= 500 && status <= 599;
}

/** 网络层失败（超时/断网）统一成 0，与 HTTP 状态区分。 */
const NETWORK_ERROR_STATUS = 0;

function defaultErrorCode(status: number): string {
  if (status === NETWORK_ERROR_STATUS) return 'NETWORK_ERROR';
  if (status === 408) return 'REQUEST_TIMEOUT';
  return isRetryableStatus(status) ? 'SERVER_ERROR' : 'REQUEST_FAILED';
}

type RequestOptions = RequestInit & {
  /** 超时毫秒数，默认 30s。0 表示不限制。 */
  timeoutMs?: number;
  /** 失败自动重试次数，默认 0（保持既有行为；读接口可显式开启）。 */
  retries?: number;
  /** 重试基线退避毫秒，默认 400ms，指数退避。 */
  retryBaseMs?: number;
};

async function request<T>(url: string, options?: RequestOptions): Promise<T> {
  const token = getToken();
  const { timeoutMs = 30_000, retries = 0, retryBaseMs = 400, ...init } = options || {};

  let lastError: ApiError | null = null;
  for (let attempt = 0; attempt <= retries; attempt++) {
    if (attempt > 0) {
      // 指数退避，避免雪崩
      await new Promise((resolve) => setTimeout(resolve, retryBaseMs * 2 ** (attempt - 1)));
    }
    // 每次尝试独立 AbortController：超时与调用方取消都要能中断在途请求
    const controller = new AbortController();
    const timer = timeoutMs > 0 ? setTimeout(() => controller.abort(), timeoutMs) : null;
    let response: Response;
    try {
      response = await fetch(url, {
        ...init,
        signal: controller.signal,
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
          ...(init.headers || {}),
        },
      });
    } catch (error) {
      const aborted = error instanceof DOMException && error.name === 'AbortError';
      // timer 存在且被 abort = 我们自己的超时触发；否则是调用方取消或真网络错误。
      const timedOut = aborted && timer !== null;
      lastError = new ApiError({
        message: timedOut ? `请求超时（${Math.round(timeoutMs / 1000)}s）` : '网络请求失败',
        errorCode: timedOut ? 'REQUEST_TIMEOUT' : 'NETWORK_ERROR',
        status: NETWORK_ERROR_STATUS,
        url,
        // 超时不自动重试：请求可能已到达服务端，重试会重复提交（对 POST 尤其危险）。
        // 连接层失败（断网 / DNS）未产生副作用，可安全重试。
        retryable: !timedOut,
      });
      if (timer) clearTimeout(timer);
      if (!lastError.retryable || attempt === retries) throw lastError;
      continue;
    }
    if (timer) clearTimeout(timer);

    if (response.ok) {
      return response.json() as Promise<T>;
    }

    const text = await response.text();
    if (response.status === 401 && !url.includes('/api/auth/')) {
      setToken(null);
      window.dispatchEvent(new CustomEvent('alpha-nexus:auth-required'));
    }

    // 后端统一契约：{ok:false,errorCode,error_code,message,error,requestId}
    // 老端点可能仍是 plain text，做兜底。
    let code = '';
    let detail = '';
    let requestId = '';
    try {
      const j = JSON.parse(text) as {
        errorCode?: string;
        error_code?: string;
        message?: string;
        error?: string;
        requestId?: string;
        request_id?: string;
      };
      code = j.errorCode || j.error_code || '';
      detail = j.message || j.error || '';
      requestId = j.requestId || j.request_id || '';
    } catch {
      // 非 JSON：保持既有行为，把原文当 message
      detail = text;
    }
    const message = detail || `请求失败（${response.status}）`;
    const error = new ApiError({
      message,
      errorCode: code || defaultErrorCode(response.status),
      status: response.status,
      requestId,
      url,
    });
    if (!error.retryable || attempt === retries) throw error;
    lastError = error;
  }
  throw lastError ?? new ApiError({ message: '请求失败', errorCode: 'REQUEST_FAILED', status: 0, url });
}

async function requestBlob(url: string): Promise<Blob> {
  const token = getToken();
  const response = await fetch(url, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  if (!response.ok) {
    if (response.status === 401) {
      setToken(null);
      window.dispatchEvent(new CustomEvent('alpha-nexus:auth-required'));
    }
    throw new Error((await response.text()) || `请求失败（${response.status}）`);
  }
  return response.blob();
}

function callStructuredTool(name: string, args: Record<string, unknown>) {
  return request<ToolCallResult>('/api/v1/tools/call', {
    method: 'POST',
    body: JSON.stringify({ name, args }),
  });
}

async function getShopListingPlans(): Promise<ShopListingPlanList> {
  const response = await callStructuredTool('shop.listing.list', { input: { limit: 20 } });
  const plans = response.result.data?.plans;
  if (!response.result.ok || !Array.isArray(plans)) throw new Error(response.result.error || 'SHOP_LISTING_LIST_FAILED');
  return { plans: plans as ShopListingPlanSummary[], count: plans.length };
}

async function dataLinkTool<T>(name: string, args: Record<string, unknown>, key: string): Promise<T> {
  const response = await callStructuredTool(name, args);
  const value = response.result.data?.[key];
  if (!response.result.ok || !value) throw new Error(response.result.error || 'DATA_LINK_FAILED');
  return value as T;
}

async function getDataLinkState(): Promise<DataLinkState> {
  const response = await callStructuredTool('data.sql.list', {});
  if (!response.result.ok) throw new Error(response.result.error || 'DATA_LINK_FAILED');
  return {
    sources: (response.result.data?.sources ?? []) as DataLinkSource[],
    snapshots: (response.result.data?.snapshots ?? []) as DataLinkSnapshot[],
  };
}

function inspectDataLink(sourceID: number, objectName = '', query = '') {
  return dataLinkTool<DataLinkInspection>('data.sql.inspect', { source_id: sourceID, object_name: objectName, query, limit: 20 }, 'inspection');
}

function preflightDataLink(sourceID: number, objectName: string, query: string, mapping: Record<string, string>) {
  return dataLinkTool<DataLinkPreflight>('data.sql.preflight', { source_id: sourceID, object_name: objectName, query, mapping, max_rows: 10_000 }, 'preflight');
}

function collectDataLink(sourceID: number, objectName: string, query: string, mapping: Record<string, string>) {
  return dataLinkTool<DataLinkSnapshot>('data.sql.collect', { source_id: sourceID, object_name: objectName, query, mapping, max_rows: 10_000, max_bytes: 32 << 20, timeout_seconds: 8 }, 'snapshot');
}

function confirmDataLinkReview(snapshotID: number, baseID: number) {
  return dataLinkTool<DataLinkSnapshot>('data.sql.review.confirm', { snapshot_id: snapshotID, base_id: baseID, confirm: true }, 'snapshot');
}

function uploadSQLiteSource(file: File) {
  const form = new FormData();
  form.append('file', file, file.name);
  return requestForm<{ source: DataLinkSource }>('/api/data/sqlite-sources', form).then((result) => result.source);
}

async function getCreatorMetricsOverview(historyDays = 30): Promise<CreatorMetricOverview> {
  const response = await callStructuredTool('social.metrics.overview', { history_days: historyDays });
  const overview = response.result.data?.overview;
  if (!response.result.ok || !overview) {
    throw new Error(response.result.error || 'CREATOR_METRICS_OVERVIEW_FAILED');
  }
  return overview as CreatorMetricOverview;
}

async function getCreatorMetricPreferences(): Promise<CreatorMetricPreferences> {
  const response = await callStructuredTool('social.metrics.preferences', {});
  const preferences = response.result.data?.preferences;
  if (!response.result.ok || !preferences) {
    throw new Error(response.result.error || 'CREATOR_METRIC_PREFERENCES_FAILED');
  }
  return preferences as CreatorMetricPreferences;
}

async function updateCreatorMetricPreferences(historyDays: number, retentionDays: number): Promise<CreatorMetricPreferences> {
  const response = await callStructuredTool('social.metrics.preferences.update', {
    history_days: historyDays,
    retention_days: retentionDays,
  });
  const preferences = response.result.data?.preferences;
  if (!response.result.ok || !preferences) {
    throw new Error(response.result.error || 'CREATOR_METRIC_PREFERENCES_INVALID');
  }
  return preferences as CreatorMetricPreferences;
}

async function collectCreatorMetrics(accountIds: number[]): Promise<CreatorMetricBatchResult> {
  const response = await callStructuredTool('social.metrics.collect-batch', { account_ids: accountIds });
  const accounts = response.result.data?.accounts;
  if (!Array.isArray(accounts)) {
    throw new Error(response.result.error || 'CREATOR_METRICS_COLLECTION_FAILED');
  }
  return {
    ok: response.result.ok,
    error: response.result.error,
    all_observed: response.result.data?.all_observed === true,
    accounts: accounts as CreatorMetricCollectionRow[],
  };
}

async function getSocialWorkflowCatalog(): Promise<SocialPlatformWorkflow[]> {
  const response = await callStructuredTool('social.workflow.catalog', {});
  const platforms = response.result.data?.platforms;
  if (!response.result.ok || !Array.isArray(platforms)) {
    throw new Error(response.result.error || 'SOCIAL_WORKFLOW_CATALOG_FAILED');
  }
  return platforms as SocialPlatformWorkflow[];
}

export async function streamAgentHubEvents(
  runID: string,
  onEvent: (event: AgentHubEvent) => boolean | void,
  signal: AbortSignal,
): Promise<void> {
  let lastEventID = '';
  let reconnectAttempt = 0;
  const delays = [250, 500, 1000, 2000, 4000];
  while (!signal.aborted) {
    const token = getToken();
    const response = await fetch(`/api/agent/runs/${encodeURIComponent(runID)}/events`, {
      headers: {
        Accept: 'text/event-stream',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...(lastEventID ? { 'Last-Event-ID': lastEventID } : {}),
      },
      signal,
    });
    if (!response.ok || !response.body) {
      throw new Error(`AGENT_EVENT_STREAM_HTTP_${response.status}`);
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let terminal = false;
    for (;;) {
      const { value, done } = await reader.read();
      buffer += decoder.decode(value, { stream: !done });
      let boundary = buffer.indexOf('\n\n');
      while (boundary >= 0) {
        const block = buffer.slice(0, boundary).replace(/\r/g, '');
        buffer = buffer.slice(boundary + 2);
        let data = '';
        for (const line of block.split('\n')) {
          if (line.startsWith('id:')) lastEventID = line.slice(3).trim();
          if (line.startsWith('data:')) data += line.slice(5).trim();
        }
        if (data) {
          const event = JSON.parse(data) as AgentHubEvent;
          terminal = onEvent(event) === true;
          if (terminal) {
            await reader.cancel();
            return;
          }
        }
        boundary = buffer.indexOf('\n\n');
      }
      if (done) break;
    }
    if (terminal || signal.aborted) return;
    const delay = delays[Math.min(reconnectAttempt, delays.length - 1)];
    reconnectAttempt += 1;
    await new Promise<void>((resolve, reject) => {
      const timer = window.setTimeout(resolve, delay);
      signal.addEventListener('abort', () => {
        window.clearTimeout(timer);
        reject(new DOMException('Aborted', 'AbortError'));
      }, { once: true });
    });
  }
}

async function requestBlobPost(url: string, body: unknown): Promise<Blob> {
  const token = getToken();
  const response = await fetch(url, {
    method: 'POST',
    body: JSON.stringify(body),
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
  });
  if (!response.ok) {
    const text = await response.text();
    if (response.status === 401) {
      setToken(null);
      window.dispatchEvent(new CustomEvent('alpha-nexus:auth-required'));
    }
    try {
      const result = JSON.parse(text) as { errorCode?: string; error_code?: string; message?: string; error?: string };
      throw new Error(result.errorCode || result.error_code || result.message || result.error || text);
    } catch (error) {
      if (error instanceof SyntaxError) throw new Error(text || `Request failed with status ${response.status}`);
      throw error;
    }
  }
  return response.blob();
}

async function requestForm<T>(url: string, form: FormData): Promise<T> {
  const token = getToken();
  const response = await fetch(url, {
    method: 'POST',
    body: form,
    // Let fetch set the multipart boundary; setting Content-Type here breaks uploads.
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  if (!response.ok) {
    const text = await response.text();
    if (response.status === 401) {
      setToken(null);
      window.dispatchEvent(new CustomEvent('alpha-nexus:auth-required'));
    }
    try {
      const body = JSON.parse(text) as { error_code?: string; message?: string; error?: string };
      throw new Error(body.error_code || body.message || body.error || text);
    } catch (error) {
      if (error instanceof SyntaxError) throw new Error(text || `Request failed with status ${response.status}`);
      throw error;
    }
  }
  return response.json() as Promise<T>;
}

function agentHubWorkspaceQuery(providerID?: AgentHubProviderID, productMode: 'media' | 'shop' = 'media'): string {
  const params = new URLSearchParams({ workspace_mode: productMode });
  if (providerID) params.set('provider_id', providerID);
  return `?${params.toString()}`;
}

export const api = {
	getLocalDashboardSummary: () => request<LocalDashboardSummary>('/api/dashboard/summary'),
  getFeedback: (refresh = false) =>
    request<FeedbackListResponse>(`/api/feedback${refresh ? '?refresh=1' : ''}`),
  submitFeedback: (content: string, currentModule: string) =>
    request<FeedbackItem>('/api/feedback', {
      method: 'POST',
      body: JSON.stringify({ content, current_module: currentModule }),
    }),
  getAuthStatus: () =>
    request<{ auth_required: boolean; user_count: number }>('/api/auth/status'),
  login: (username: string, password: string) =>
    request<{ token: string; expires_at: string; username: string; role: string }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  me: () => request<{ username: string; role: string }>('/api/auth/me'),
  getBackendHealth: () => request<BackendHealth>('/healthz'),
  getOverview: () => request<Overview>('/api/overview'),
  getEgoDoctor: () => request<EgoDoctor>('/api/ego/doctor'),
  getEgoLegacyAccounts: () => request<EgoLegacyAccountPreview>('/api/ego/legacy-accounts'),
  importEgoLegacyAccounts: (params?: { identity_id?: number; platform_codes?: string[] }) =>
    request<EgoLegacyAccountImport>('/api/ego/legacy-accounts/import', {
      method: 'POST',
      body: params ? JSON.stringify(params) : undefined,
    }),
  getEgoIdentities: () => request<EgoIdentity[]>('/api/ego/identities'),
  getEgoProfiles: () => request<EgoProfilesResponse>('/api/ego/profiles'),
  updateEgoIdentityProfile: (identityId: number, profileId: string, profileName = '') =>
    request<{ ok: boolean; identity?: EgoIdentity }>(`/api/ego/identities/${identityId}/profile`, {
      method: 'PUT',
      body: JSON.stringify({ profileId, profileName }),
    }),
  verifyEgoIdentity: (identityId: number) =>
    request<{ identity_id: number; state: string; accounts: any[] }>(`/api/ego/identities/${identityId}/verify`, {
      method: 'POST',
    }),
  createEgoIdentity: (name = '') =>
    request<EgoIdentity>('/api/ego/identities', { method: 'POST', body: JSON.stringify({ name }) }),
  updateEgoIdentity: (identityId: number, input: { name?: string; sort?: number }) =>
    request<EgoIdentity>(`/api/ego/identities/${identityId}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    }),
  renameEgoIdentity: (identityId: number, name: string) =>
    request<EgoIdentity>(`/api/ego/identities/${identityId}`, {
      method: 'PATCH',
      body: JSON.stringify({ name }),
    }),
  configureEgoIdentityPlatforms: (identityId: number, platformCodes: string[], expectedPlatformCodes?: string[]) =>
    request<Account[]>(`/api/ego/identities/${identityId}/platforms`, {
      method: 'PUT',
      body: JSON.stringify({ platform_codes: platformCodes, expected_platform_codes: expectedPlatformCodes }),
    }),
  openEgoLoginBatch: (identityId: number, platformCodes: string[]) =>
    request<EgoLoginBatch>(`/api/ego/identities/${identityId}/login-batches`, {
      method: 'POST',
      body: JSON.stringify({ platform_codes: platformCodes }),
    }),
  openEgoBrowser: (identityId: number, offset = 0, platformCodes?: string[]) =>
    request<EgoBrowserOpenResult>(`/api/ego/identities/${identityId}/open`, {
      method: 'POST',
      body: JSON.stringify({ offset, platform_codes: platformCodes }),
    }),
  verifyEgoLoginBatch: (identityId: number, platformCodes: string[]) =>
    request<EgoLoginVerification>(`/api/ego/identities/${identityId}/login-verify`, {
      method: 'POST',
      body: JSON.stringify({ platform_codes: platformCodes }),
    }),
  openEgoLoginQR: (accountId: number, refresh = false, navigate = false) =>
    request<EgoLoginQR>(`/api/accounts/${accountId}/login-qr`, {
      method: 'POST',
      body: JSON.stringify({ refresh, navigate }),
    }),
  focusEgoLite: () => request<{ ok: boolean }>('/api/ego/focus', { method: 'POST' }),
  releaseEgoLoginQR: (accountId: number) =>
    request<EgoLoginQR>(`/api/accounts/${accountId}/login-qr`, {
      method: 'POST',
      body: JSON.stringify({ release: true }),
    }),
  importEgoBookmarks: () =>
    request<EgoBookmarkImport>('/api/ego/bookmarks/import', { method: 'POST' }),
  getEgoSpaces: () => request<EgoSpace[]>('/api/ego/spaces', { cache: 'no-store' }),
  // 真实 Space（直接问 EgoLite，不读可能过期的 DB 绑定）。
  getEgoSpacesLive: () => request<{ spaces: EgoLiveSpace[] }>('/api/ego/spaces/live', { cache: 'no-store' }),
  getEgoSpaceTabs: (spaceID: number) =>
    request<EgoSpaceTabs>(`/api/ego/spaces/${encodeURIComponent(spaceID)}/tabs`, { cache: 'no-store' }),
  activateEgoSpaceTab: (spaceID: number, tabID: string) =>
    request<EgoSpaceTabs>(`/api/ego/spaces/${encodeURIComponent(spaceID)}/tabs`, {
      method: 'POST',
      body: JSON.stringify({ tab_id: tabID }),
    }),
  // This Mac's screen. Needs macOS Screen Recording; 403 carries the auth hint.
  getDesktopScreen: () =>
    request<{ png: string; mime: string; captured_at: string }>('/api/desktop/screen', { cache: 'no-store' }),
  openDesktopScreenPermission: () =>
    request<{ opened: boolean }>('/api/desktop/screen/permission', { method: 'POST' }),
  // 屏幕录制要用户在系统设置里用「+」选中本应用；在 Finder 里高亮它，省得找路径。
  revealDesktopScreenBinary: () =>
    request<{ revealed: boolean; path: string }>('/api/desktop/screen/reveal', { method: 'POST' }),
  // Read-only live frame of one Space's current page; never takes control.
  getEgoSpaceScreen: (spaceID: number) =>
    request<EgoSpaceScreen>(`/api/ego/spaces/${encodeURIComponent(spaceID)}/screen`, { cache: 'no-store' }),
  // Explicit user action: take control back and grab one frame.
  takeOverEgoSpaceScreen: (spaceID: number) =>
    request<EgoSpaceScreen>(`/api/ego/spaces/${encodeURIComponent(spaceID)}/screen/takeover`, { method: 'POST' }),
  ensureEgoSpace: (accountId: number) =>
    request<EgoSpace>(`/api/accounts/${accountId}/space/ensure`, { method: 'POST' }),
  loginEgoSpace: (accountId: number) =>
    request<EgoSpace>(`/api/accounts/${accountId}/space/login`, { method: 'POST' }),
  resumeEgoSpace: (accountId: number) =>
    request<EgoSpace>(`/api/accounts/${accountId}/space/resume`, { method: 'POST' }),
  calibrateEgoConcurrency: () =>
    request<EgoCalibrationResult>('/api/ego/concurrency/calibrate', { method: 'POST' }),
  getEgoSessionMonitor: () =>
    request<EgoSessionMonitorSettings>('/api/ego/session-monitor'),
  updateEgoSessionMonitor: (input: { enabled?: boolean; interval_minutes?: number }) =>
    request<EgoSessionMonitorSettings>('/api/ego/session-monitor', {
      method: 'PUT',
      body: JSON.stringify(input),
    }),
  runEgoSessionMonitor: () =>
    request<EgoSessionMonitorResult>('/api/ego/session-monitor/run', { method: 'POST' }),
  getAccounts: () => request<Account[]>('/api/accounts', { cache: 'no-store' }),
	getAccountTags: () => request<AccountTag[]>('/api/account-tags'),
	createAccountTag: (name: string, color: string) =>
		request<AccountTag>('/api/account-tags', { method: 'POST', body: JSON.stringify({ name, color }) }),
	reorderAccountTags: (tagIds: number[]) =>
		request<{ tag_count: number }>('/api/account-tags/order', {
			method: 'PUT', body: JSON.stringify({ tagIds }),
		}),
	bulkUpdateAccountTags: (accountIds: number[], tagIds: number[], mode: 'append' | 'remove' | 'replace') =>
		request<{ account_count: number; tag_count: number; mode: string }>('/api/account-tags/bulk', {
			method: 'POST', body: JSON.stringify({ accountIds, tagIds, mode }),
		}),
	resolveAccountTags: (tagIds: number[], platform = '') =>
		request<{ count: number; accounts: Array<{ id: number; platform: string; name: string }> }>('/api/account-tags/resolve', {
			method: 'POST', body: JSON.stringify({ tagIds, platform }),
		}),
  getPlatforms: () => request<Platform[]>('/api/platforms'),

  addAccount: (
    platform: string,
    name: string,
    remark: string,
    opts?: { startNow?: boolean; identityId?: number },
  ) =>
	request<Account & { started?: boolean; startError?: string }>(
      '/api/accounts/add',
      {
        method: 'POST',
        body: JSON.stringify({
          platform,
          name,
          remark,
          startNow: opts?.startNow ?? false,
          identityId: opts?.identityId ?? 1,
        }),
      },
    ),

  updateAccountRemark: (accountId: number, remark: string) =>
    request<{ accountId: number; remark: string }>(`/api/accounts/${accountId}/remark`, {
      method: 'PATCH',
      body: JSON.stringify({ remark }),
    }),

  updateAccountGroup: (accountId: number, group: string) =>
    request<{ accountId: number; group: string }>(`/api/accounts/${accountId}/group`, {
      method: 'PATCH',
      body: JSON.stringify({ group }),
    }),

  deleteAccount: (id: number) =>
    request<{ deleted: boolean }>('/api/accounts/delete', {
      method: 'POST',
      body: JSON.stringify({ id }),
    }),

  verifyAccount: (accountId: number) =>
    request<any>('/api/accounts/verify', {
      method: 'POST',
      body: JSON.stringify({ accountId }),
    }),

  observeAccount: (accountId: number) =>
    request<AccountLoginObservation>(`/api/accounts/${accountId}/observe`, { method: 'POST' }),
  observeAccounts: () =>
    request<{ observations: AccountLoginObservation[] }>('/api/accounts/observe', { method: 'POST' }),
  getAccountAvatarBlob: (avatarURL: string) => requestBlob(avatarURL),
  getAccountSessionBackups: (accountId: number) =>
    request<AccountSessionBackupList>(`/api/accounts/${accountId}/session-backups`),
  createAccountSessionBackup: (accountId: number) =>
    request<{ backup: AccountSessionBackup; retentionDays: number }>(`/api/accounts/${accountId}/session-backups`, { method: 'POST' }),
  restoreAccountSessionBackup: (accountId: number, date: string) =>
    request<{ accountId: number; restored: boolean; verificationRequired: boolean; message: string }>(`/api/accounts/${accountId}/session-backups/${encodeURIComponent(date)}/restore`, { method: 'POST' }),
  createAllAccountSessionBackups: () =>
    request<{ retentionDays: number; results: Array<{ accountId: number; status: string; errorCode?: string }> }>('/api/accounts/session-backups', { method: 'POST' }),
  exportAccountSession: (accountId: number, passphrase: string) =>
    requestBlobPost(`/api/accounts/${accountId}/session-export`, { passphrase }),
  importAccountSession: (archive: File, passphrase: string) => {
    const form = new FormData();
    form.set('archive', archive);
    form.set('passphrase', passphrase);
    return requestForm<AccountSessionImportResult>('/api/accounts/session-import', form);
  },
  getAccountLoginEvents: (accountId: number, limit = 50) =>
    request<AccountLoginEvent[]>(`/api/accounts/${accountId}/login-events?limit=${limit}`),
  getAccountBrowserEvents: (accountId: number, limit = 50) =>
    request<BrowserLifecycleEvent[]>(`/api/accounts/${accountId}/browser-events?limit=${limit}`),
  getAccountObserverStatus: () =>
    request<AccountObserverStatus>('/api/account-observer/status'),

  setFingerprint: (accountId: number, seed: number) =>
    request<{ ok: boolean }>('/api/accounts/set-fingerprint', {
      method: 'POST',
      body: JSON.stringify({ accountId, seed }),
    }),

  setFingerprintConfig: (accountId: number, config: string) =>
    request<{ ok: boolean }>('/api/accounts/set-fingerprint-config', {
      method: 'POST',
      body: JSON.stringify({ accountId, config }),
    }),

  setAccountEngine: (accountId: number, engine: string) =>
    request<{ ok: boolean; accountId: number; engine: string }>('/api/accounts/set-engine', {
      method: 'POST',
      body: JSON.stringify({ accountId, engine }),
    }),

  smartFingerprint: (accountId: number, region: string, platformTarget?: string) =>
    request<any>('/api/accounts/smart-fingerprint', {
      method: 'POST',
      body: JSON.stringify({ accountId, region, platformTarget }),
    }),

  collectData: (accountId: number) =>
    request<any>('/api/data/collect', {
      method: 'POST',
      body: JSON.stringify({ accountId }),
    }),

  getDataAccounts: () => request<XHSAccountSummary[]>('/api/data/accounts'),
  getDataNotes: (account: string) => request<XHSNote[]>(`/api/data/notes?account=${encodeURIComponent(account)}`),
  getDataDates: () => request<{ date: string; accounts: number; total_notes: number }[]>('/api/data/dates'),
  getDataDashboard: () => request<XHSDashboardSummary>('/api/data/dashboard'),
  getTrendRadarSummary: async () => {
    const summary = await request<TrendRadarSummary>('/api/trend-radar/summary');
    // 新建但尚未抓取的博主，后端返回 latest_posts: null。
    // 在这里统一成数组，避免每个消费方都要判空（否则渲染会直接崩）。
    for (const creator of summary.creators ?? []) {
      if (!Array.isArray(creator.latest_posts)) creator.latest_posts = [];
    }
    return summary;
  },
  parseTrendRadarVideoInput: (input: string) =>
    request<{
      kind: 'video_id' | 'share_url';
      endpoint_id: 'douyin.video_detail' | 'douyin.video_by_share_url';
      video_id?: string;
      extracted_url?: string;
      normalized_url?: string;
      params: Record<string, unknown>;
      existing_call_id?: number;
      existing_video_id?: string;
      existing_analysis_status?: string;
    }>('/api/trend-radar/video-search/parse', {
      method: 'POST',
      body: JSON.stringify({ input }),
    }),
  analyzeTrendRadarVideo: (callID: number, force = false, videoID?: string) =>
    request<{ analysis: TrendRadarVideoAnalysis; reused: boolean }>(
      `/api/trend-radar/video-search/${callID}/analyze`,
      {
        method: 'POST',
        body: JSON.stringify({ confirm: true, force, video_id: videoID }),
      },
    ),
  runTrendRadarNiche: (
    nicheID: string,
    input: { platform: string; terms?: string[]; all?: boolean },
  ) =>
    request<TrendRadarRun>(`/api/trend-radar/niches/${encodeURIComponent(nicheID)}/run`, {
      method: 'POST',
      body: JSON.stringify({ ...input, confirm: true }),
      // 一轮最多 8 次真实调用，单次最长 75s，给足时间再判超时。
      timeoutMs: 10 * 60_000,
    }),
  getTrendRadarRuns: (params?: { niche_id?: string; limit?: number }) => {
    const query = new URLSearchParams();
    if (params?.niche_id) query.set('niche_id', params.niche_id);
    if (params?.limit) query.set('limit', String(params.limit));
    const suffix = query.toString();
    return request<{ runs: TrendRadarRun[]; count: number }>(`/api/trend-radar/runs${suffix ? `?${suffix}` : ''}`);
  },
  // 只重跑这一轮的失败调用：成功项不会再发请求，不会重复计费。
  retryTrendRadarRun: (runID: string) =>
    request<{
      ok: boolean;
      run: TrendRadarRun;
      retried_terms: string[];
      recovered_terms: string[];
      pending_terms: string[];
    }>(`/api/trend-radar/runs/${encodeURIComponent(runID)}/retry`, {
      method: 'POST',
      body: JSON.stringify({ confirm: true }),
      timeoutMs: 10 * 60_000,
    }),
  saveTrendRadarNicheLimits: (nicheID: string, platformLimits: Record<string, number>) =>
    request<TrendRadarConfigNiche>(
      `/api/trend-radar/niches/${encodeURIComponent(nicheID)}/limits`,
      { method: 'PUT', body: JSON.stringify({ platform_limits: platformLimits }) },
    ),
  exportTrendRadar: (params?: { niche_id?: string; format?: 'csv' | 'json' }) => {
    const query = new URLSearchParams();
    if (params?.niche_id) query.set('niche_id', params.niche_id);
    query.set('format', params?.format ?? 'csv');
    return requestBlob(`/api/trend-radar/export?${query.toString()}`);
  },
  getTrendRadarConfig: () => request<TrendRadarConfig>('/api/trend-radar/config'),
  saveTrendRadarConfig: (config: TrendRadarConfig) =>
    request<TrendRadarConfig>('/api/trend-radar/config', {
      method: 'PUT',
      body: JSON.stringify(config),
    }),
  assignTrendRadarKeywords: (input: TrendRadarAssignKeywordInput) =>
    request<TrendRadarAssignKeywordResult>('/api/trend-radar/assign-keyword', {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  addTrendRadarCreator: (input: { platform: 'xhs' | 'douyin'; user_id: string; name?: string; note?: string }) =>
    request<{ ok: boolean }>('/api/trend-radar/creators', {
      method: 'POST',
      body: JSON.stringify(input),
    }),
  getRadarCreatorComparison: (days: 7 | 30 | 90 = 30) =>
    request<RadarCreatorComparison>(`/api/trend-radar/creators/compare?days=${days}`),
  removeTrendRadarCreator: (platform: string, userID: string) =>
    request<{ ok: boolean }>(`/api/trend-radar/creators/${encodeURIComponent(platform)}/${encodeURIComponent(userID)}`, {
      method: 'DELETE',
    }),
  archiveTrendRadarCreator: (platform: string, userID: string, options?: { max_pages?: number; skip_media?: boolean; media_only?: boolean }) =>
    request<{ ok: boolean; key: string; accepted?: boolean }>(`/api/trend-radar/creators/${encodeURIComponent(platform)}/${encodeURIComponent(userID)}/archive`, {
      method: 'POST',
      body: JSON.stringify(options ?? {}),
    }),
  getTrendRadarCreatorArchiveStatus: (platform: string, userID: string) =>
    request<TrendRadarArchiveStatus>(`/api/trend-radar/creators/${encodeURIComponent(platform)}/${encodeURIComponent(userID)}/archive-status`),
  // 没有台账时后端返回 total:0 与空数组，不是错误。
  getTrendRadarCreatorArchiveFailures: (platform: string, userID: string) =>
    request<{ ok: boolean; total: number; failures: TrendRadarArchiveFailure[] }>(
      `/api/trend-radar/creators/${encodeURIComponent(platform)}/${encodeURIComponent(userID)}/archive-failures`,
    ),
  // 博主级自动增量归档开关：默认关闭，只有显式打开才会让后台 worker 消耗 TikHub 额度。
  setTrendRadarCreatorAutoArchive: (platform: string, userID: string, enabled: boolean) =>
    request<{ ok: boolean; enabled: boolean }>(
      `/api/trend-radar/creators/${encodeURIComponent(platform)}/${encodeURIComponent(userID)}/auto-archive`,
      { method: 'PUT', body: JSON.stringify({ enabled }) },
    ),
  getTrendRadarCreatorMediaURL: (platform: string, userID: string, relativePath: string) => {
    const clean = relativePath.replace(/^media\//, '');
    return `/api/trend-radar/creators/${encodeURIComponent(platform)}/${encodeURIComponent(userID)}/media/${clean}`;
  },
  openTrendRadarCreatorFolder: (platform: string, userID: string) =>
    request<{ ok: boolean; folder: string }>(`/api/trend-radar/creators/${encodeURIComponent(platform)}/${encodeURIComponent(userID)}/open-folder`, {
      method: 'POST',
    }),
  updateTrendRadarCreator: (creator: Pick<TrendRadarCreator, 'platform' | 'user_id' | 'name' | 'note' | 'lookback_days' | 'page_size' | 'max_pages' | 'sort_type'>) =>
    request<{ ok: boolean }>(`/api/trend-radar/creators/${encodeURIComponent(creator.platform)}/${encodeURIComponent(creator.user_id)}`, {
      method: 'PUT',
      body: JSON.stringify({
        name: creator.name,
        note: creator.note ?? '',
        lookback_days: creator.lookback_days,
        page_size: creator.page_size,
        max_pages: creator.max_pages,
        sort_type: creator.sort_type,
      }),
    }),
  backfillTrendRadarCovers: () =>
    request<{
      ok: boolean;
      snapshot_count: number;
      snapshot_failed: number;
      cover_discovered: number;
      cover_cached: number;
      cover_existing: number;
      cover_failed: number;
    }>('/api/trend-radar/covers/backfill', {
      method: 'POST',
      body: JSON.stringify({ confirm: true }),
    }),
  getTrendRadarSchedules: () =>
    request<{ version: number; schedules: TrendRadarSchedule[]; updated_at?: string }>('/api/trend-radar/schedules'),
  // 一轮可能跑多个到期计划，每个都是真实付费调用；30s 默认超时会在任务
  // 还在继续时就让界面报超时。
  runTrendRadarDue: () =>
    request<{ due: number; succeeded: number; failed: number; call_ids: number[] }>('/api/trend-radar/schedules/run-due', {
      method: 'POST',
      timeoutMs: 10 * 60_000,
    }),
  createTrendRadarSchedule: (schedule: Omit<TrendRadarSchedule, 'id'>, confirm: boolean) =>
    request<TrendRadarSchedule>('/api/trend-radar/schedules', {
      method: 'POST',
      body: JSON.stringify({ schedule, confirm }),
    }),
  updateTrendRadarSchedule: (schedule: TrendRadarSchedule, confirm: boolean) =>
    request<TrendRadarSchedule>(`/api/trend-radar/schedules/${encodeURIComponent(schedule.id)}`, {
      method: 'PUT',
      body: JSON.stringify({ schedule, confirm }),
    }),
  deleteTrendRadarSchedule: (id: string) =>
    request<{ ok: boolean; history_preserved: boolean }>(`/api/trend-radar/schedules/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
  getTrendRadarVideos: () =>
    request<{ videos: TrendRadarVideo[]; count: number }>('/api/trend-radar/videos'),
  uploadTrendRadarVideo: async (file: File) => {
    const form = new FormData();
    form.append('file', file);
    const token = getToken();
    const response = await fetch('/api/trend-radar/videos', {
      method: 'POST',
      headers: token ? { Authorization: `Bearer ${token}` } : {},
      body: form,
    });
    if (!response.ok) throw new Error(await response.text() || `视频处理失败 (${response.status})`);
    return response.json() as Promise<TrendRadarVideo>;
  },
  getTrendRadarRSS: () =>
    request<{ sources: TrendRadarRSSSource[]; runs: TrendRadarRSSRun[] }>('/api/trend-radar/rss/sources'),
  createTrendRadarRSSSource: (source: Omit<TrendRadarRSSSource, 'id'>) =>
    request<TrendRadarRSSSource>('/api/trend-radar/rss/sources', {
      method: 'POST',
      body: JSON.stringify(source),
    }),
  updateTrendRadarRSSSource: (source: TrendRadarRSSSource) =>
    request<TrendRadarRSSSource>(`/api/trend-radar/rss/sources/${encodeURIComponent(source.id)}`, {
      method: 'PUT',
      body: JSON.stringify(source),
    }),
  deleteTrendRadarRSSSource: (id: string) =>
    request<{ ok: boolean; history_preserved: boolean }>(`/api/trend-radar/rss/sources/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
  collectTrendRadarRSS: (id: string) =>
    request<TrendRadarRSSRun>(`/api/trend-radar/rss/sources/${encodeURIComponent(id)}/collect`, {
      method: 'POST',
      body: JSON.stringify({ confirm: true }),
    }),
  getDataAPIConfig: () => request<DataAPIConfig>('/api/data-api/config'),
  saveTikHubCredential: (apiKey: string, region: 'mainland' | 'global') =>
    request<DataAPIConfig>('/api/data-api/credential', {
      method: 'PUT',
      body: JSON.stringify({ api_key: apiKey, region }),
    }),
  testAndActivateDataAPI: () =>
    request<DataAPIConfig>('/api/data-api/test', {
      method: 'POST',
      body: JSON.stringify({ confirm: true }),
    }),
  getDataAPICalls: () =>
    request<{ calls: TikHubAPICall[]; count: number }>('/api/data-api/calls?limit=50'),
  // 单次付费抓取实测最长 33s，超过默认 30s 超时。超时只是前端放弃等待，
  // 后端仍会跑完并计费，所以给足时间避免「界面说失败、实际已扣费」。
  callDataAPI: (endpointId: string, params: Record<string, unknown>) =>
    request<TikHubAPICall>('/api/data-api/calls', {
      method: 'POST',
      body: JSON.stringify({ endpoint_id: endpointId, params, confirm: true }),
      timeoutMs: 3 * 60_000,
    }),
  getDataAPICallResponse: (id: number) =>
    request<Record<string, unknown>>(`/api/data-api/calls/${id}/response`),

  getBatchRuns: () => request<BatchRun[]>('/api/batch/runs'),
  getBatchItems: (batchId: number) => request<BatchItem[]>(`/api/batch/items?batchId=${batchId}`),
	getFleetRuns: () => request<FleetRun[]>('/api/fleet/runs'),
	getFleetRun: (runID: number) => request<FleetRunDetail>(`/api/fleet/runs/${runID}`),
	getFleetInstances: () =>
		request<{ instances: FleetInstance[]; count: number }>('/api/fleet/instances').then((result) => result.instances ?? []),
	getOperationEvents: () =>
		request<{ events: OperationEvent[]; count: number }>('/api/operations/events').then((result) => result.events ?? []),
	createDryRunBatch: (body: { contentId: number; assetIds: number[]; concurrency: number; dryRun: true }) =>
		request<BatchRun>('/api/batch/publish', { method: 'POST', body: JSON.stringify(body) }),
  getContents: () => request<ContentItem[]>('/api/contents'),
  getShopContents: () => request<ContentItem[]>('/api/shop/contents'),
  createContent: (body: Omit<ContentItem, 'id' | 'publish_count'>) =>
    request<ContentItem>('/api/contents', { method: 'POST', body: JSON.stringify(body) }),
  createShopContent: (body: Omit<ContentItem, 'id' | 'publish_count'>) =>
    request<ContentItem>('/api/shop/contents', { method: 'POST', body: JSON.stringify(body) }),
	commitCreationDraft: (body: {
		type: ContentItem['type'];
		title: string;
		caption: string;
		body: string;
		tags_json: string;
		review_result: string;
		manual_review_confirmed: boolean;
	}) => request<CreationDraftCommitResponse>('/api/creation/drafts', { method: 'POST', body: JSON.stringify(body) }),
	getCreationModelRoles: () => request<CreationModelRolesResponse>('/api/creation/model-roles'),
	putCreationModelRole: (role: CreationModelRoleID, body: { provider_id: AgentHubProviderID; model_id: string }) =>
		request<CreationModelRole>(`/api/creation/model-roles/${encodeURIComponent(role)}`, { method: 'PUT', body: JSON.stringify(body) }),
	createCreationSession: (body: { title: string; topic_brief?: string; style?: string; platform?: string; sop_stage?: CreationSession['sop_stage']; knowledge_base_ids: number[] }) =>
		request<CreationSession>('/api/creation/sessions', { method: 'POST', body: JSON.stringify(body) }),
	getCreationSession: (id: string) => request<CreationSession>(`/api/creation/sessions/${encodeURIComponent(id)}`),
	runCreationSession: (id: string, body: { action: CreationRun['action']; input?: string; allow_external: boolean }) =>
		request<CreationRunResponse>(`/api/creation/sessions/${encodeURIComponent(id)}/runs`, { method: 'POST', body: JSON.stringify(body) }),
	getCreationWorkflows: async () => {
		const response = await request<{ items: CreationWorkflowView[] }>('/api/creation/workflows');
		return response.items ?? [];
	},
	createCreationWorkflow: (body: { title?: string; platform?: string; style?: string; stage?: CreationWorkflowStage; knowledge_scopes?: KnowledgeScope[] }) =>
		request<CreationWorkflowView>('/api/creation/workflows', { method: 'POST', body: JSON.stringify(body) }),
	getCreationWorkflow: (id: string) =>
		request<CreationWorkflowView>(`/api/creation/workflows/${encodeURIComponent(id)}`),
	updateCreationWorkflow: (id: string, body: { title?: string; platform?: string; style?: string; stage?: CreationWorkflowStage; knowledge_scopes?: KnowledgeScope[]; accepted_artifacts?: Record<string, string> }) =>
		request<CreationWorkflowView>(`/api/creation/workflows/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(body) }),
	advanceCreationWorkflow: (id: string, body: { from_stage: CreationWorkflowStage; artifact: string }) =>
		request<CreationWorkflowView>(`/api/creation/workflows/${encodeURIComponent(id)}/advance`, { method: 'POST', body: JSON.stringify(body) }),
	runCreationWorkflow: (id: string, body: { message: string; stage: CreationWorkflowStage; knowledge_scopes?: KnowledgeScope[]; trend_context?: string }) =>
		request<CreationWorkflowRunResponse>(`/api/creation/workflows/${encodeURIComponent(id)}/runs`, { method: 'POST', body: JSON.stringify(body) }),
  updateContent: (id: number, body: Omit<ContentItem, 'id' | 'publish_count'>) =>
    request<ContentItem>(`/api/contents/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  updateShopContent: (id: number, body: Omit<ContentItem, 'id' | 'publish_count'>) =>
    request<ContentItem>(`/api/shop/contents/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  cloneContent: (id: number) =>
    request<ContentItem>(`/api/contents/${id}/clone`, { method: 'POST' }),
  deleteContent: (id: number) =>
    request<{ deleted: boolean }>(`/api/contents/${id}`, { method: 'DELETE' }),
  deleteShopContent: (id: number) =>
    request<{ deleted: boolean }>(`/api/shop/contents/${id}`, { method: 'DELETE' }),
  getContentMedia: (contentID: number) => request<MediaAsset[]>(`/api/contents/${contentID}/media`),
  getShopContentMedia: (contentID: number) => request<MediaAsset[]>(`/api/shop/contents/${contentID}/media`),
	createPublishPlan: (body: {
		contentId: number;
		mediaMode: 'article' | 'graph_text' | 'video';
		mediaIds: number[];
		coverMediaId?: number;
		compliancePolicy?: CompliancePolicy;
		concurrency?: number;
		targets: { account_id: number; asset_id?: number; variant_id?: number; settings_json?: string }[];
	}) => request<PublishPlan>('/api/publish-plans', { method: 'POST', body: JSON.stringify(body) }),
	getStudioScheduleHandoffs: (limit = 100) =>
		request<StudioScheduleHandoff[]>(`/api/studio/schedule-handoffs?limit=${encodeURIComponent(limit)}`),
	getStudioScheduleHandoff: (id: number) =>
		request<StudioScheduleHandoff>(`/api/studio/schedule-handoffs/${id}`),
	createStudioScheduleHandoff: (body: { workflowId: string; contentId: number; scriptId?: number; originalLocalTime: string; timezone: string }) =>
		request<StudioScheduleHandoff>('/api/studio/schedule-handoffs', { method: 'POST', body: JSON.stringify(body) }),
	cancelStudioScheduleHandoff: (id: number) =>
		request<StudioScheduleHandoff>(`/api/studio/schedule-handoffs/${id}/cancel`, { method: 'POST' }),
	freezePublishPlanHandoff: (body: {
		handoffId: number;
		compliancePolicy?: CompliancePolicy;
		concurrency?: number;
		targets: { account_id: number; asset_id?: number; variant_id?: number; settings_json?: string }[];
	}) => request<PublishPlan>('/api/publish-plans/freeze-handoff', { method: 'POST', body: JSON.stringify(body) }),
	getPublishPlans: () => request<PublishPlanSummary[]>('/api/publish-plans'),
	getPublishPlan: (id: number) => request<PublishPlan>(`/api/publish-plans/${id}`),
	approvePublishPlan: (id: number, snapshotHash: string, action: 'prepare' | 'submit' = 'prepare') => request<PublishPlanApproval>(`/api/publish-plans/${id}/approve`, { method: 'POST', body: JSON.stringify({ snapshotHash, action }) }),
	preparePublishPlan: (id: number, body: { snapshotHash: string; approvalId: string; nonce: string }) => request<PublishPlanExecution>(`/api/publish-plans/${id}/prepare`, { method: 'POST', body: JSON.stringify(body) }),
	submitPublishPlan: (id: number, body: { snapshotHash: string; approvalId: string; nonce: string }) => request<PublishPlanExecution>(`/api/publish-plans/${id}/submit`, { method: 'POST', body: JSON.stringify(body) }),
	verifyPublishPlanManager: (id: number, target?: { accountId: number; platformCode: string }) => {
		const query = target ? `?account_id=${encodeURIComponent(target.accountId)}&platform=${encodeURIComponent(target.platformCode)}` : '';
		return request<PublishPlanManagerVerification>(`/api/publish-plans/${id}/verify-manager${query}`, { method: 'POST' });
	},
	  attachContentMediaFromLibrary: (contentID: number, libraryID: number, role: 'content' | 'cover' = 'content', position = 0) =>
    request<MediaAsset>(`/api/contents/${contentID}/media`, { method: 'POST', body: JSON.stringify({ library_id: libraryID, role, position }) }),
  attachShopContentMediaFromLibrary: (contentID: number, libraryID: number, role: 'content' | 'cover' = 'content', position = 0) =>
    request<MediaAsset>(`/api/shop/contents/${contentID}/media`, { method: 'POST', body: JSON.stringify({ library_id: libraryID, role, position }) }),
  deleteContentMedia: (id: number) => request<{ deleted: boolean }>(`/api/media/${id}`, { method: 'DELETE' }),
  getContentLibraryConfig: () => request<ContentLibraryConfig>('/api/content-library/config'),
  updateContentLibraryConfig: (root_dir: string) =>
    request<ContentLibraryConfig>('/api/content-library/config', {
      method: 'PUT', body: JSON.stringify({ root_dir }),
    }),
  uploadContentVideo: (file: File) => {
    const form = new FormData();
    form.append('file', file);
    return requestForm<MediaLibraryItem>('/api/content-library/videos', form);
  },
	uploadContentImage: (file: File, purpose: 'graph_text' | 'article' = 'graph_text') => {
		const form = new FormData();
		form.append('file', file);
		form.append('purpose', purpose);
		return requestForm<MediaLibraryItem>('/api/content-library/images', form);
	},
  getMediaLibrary: () => request<MediaLibraryItem[]>('/api/media/library?limit=200'),
  importMediaLibrary: (path: string, purpose: MediaLibraryItem['purpose']) =>
    request<MediaLibraryItem>('/api/media/library', { method: 'POST', body: JSON.stringify({ path, purpose }) }),
  updateMediaLibraryPurpose: (id: number, purpose: MediaLibraryItem['purpose']) =>
    request<MediaLibraryItem>(`/api/media/library/${id}`, { method: 'PUT', body: JSON.stringify({ purpose }) }),
  deleteMediaLibrary: (id: number) => request<{ deleted: boolean }>(`/api/media/library/${id}`, { method: 'DELETE' }),
  getAIConfig: () => request<AIConfigPayload>('/api/ai/config'),
  updateAIConfig: (body: AIConfigPayload) =>
    request<{ ok: boolean; message: string }>('/api/ai/config', { method: 'POST', body: JSON.stringify(body) }),
  verifyAndSaveAgentConfig: (body: Pick<AIConfigPayload, 'agent_api_provider' | 'agent_api_base_url' | 'agent_api_key' | 'agent_model' | 'coding_model' | 'plan_model'> & { thinking_mode?: string }) =>
    request<{ ok: boolean; verified: boolean; latency_ms: number; message: string }>('/api/ai/config/agent', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  testAIConnection: (body: { category: 'agent' | 'image' | 'video' | 'media'; provider: string; base_url: string; api_key: string; model?: string }) =>
    request<AITestResult>('/api/ai/test', { method: 'POST', body: JSON.stringify(body) }),
  getAIImageConfig: () => request<AIImageConfig>('/api/ai-image/config'),
  updateAIImageConfig: (body: Pick<AIImageConfig, 'provider' | 'model' | 'image_model' | 'video_model' | 'asset_dir'>) =>
    request<AIImageConfig>('/api/ai-image/config', { method: 'PUT', body: JSON.stringify(body) }),
  saveAgnesAPIKey: (apiKey: string) =>
    request<{ key_updated: boolean }>('/api/ai-image/credential', {
      method: 'PUT',
      body: JSON.stringify({ api_key: apiKey }),
    }),
  /** Custom image provider settings. The key goes through saveAgnesAPIKey only. */
  saveAIImageProviderConfig: (body: AIImageProviderConfigInput) =>
    request<AIImageConfig>('/api/ai-image/config', { method: 'PUT', body: JSON.stringify(body) }),
  generateAIImage: (body: { prompt: string; reference_image?: string }) =>
    request<AIImageGenerateResult>('/api/ai-image/generate', { method: 'POST', body: JSON.stringify(body) }),
  getDshCatalog: () => request<DshCatalog>('/api/agent/dsh-catalog'),
  getAgentBot: async (id: string) => {
    const result = await request<{ bot?: AgentBot }>(`/api/agent-bots/${encodeURIComponent(id)}`);
    return result.bot as AgentBot;
  },
  listAgentBots: async () => {
    const result = await request<{ bots?: AgentBot[] | null }>('/api/agent-bots');
    return result.bots ?? [];
  },
  /** Rail view: one request for grouping, mute state, unread and last message. */
  agentBotRail: async (): Promise<AgentBotRail> => {
    const result = await request<{
      bots?: AgentBotRailItem[] | null;
      agent?: Partial<AgentMainRail> | null;
      unread_total?: number;
    }>('/api/agent-bots/rail');
    return {
      bots: result.bots ?? [],
      agent: {
        unread: result.agent?.unread ?? 0,
        last_message: result.agent?.last_message ?? '',
        last_message_at: result.agent?.last_message_at ?? '',
      },
      unread_total: result.unread_total ?? 0,
    };
  },
  createAgentBot: async (body: AgentBotInput) => {
    const result = await request<{ bot?: AgentBot } & Partial<AgentBot>>('/api/agent-bots', {
      method: 'POST',
      body: JSON.stringify(body),
    });
    return (result.bot ?? result) as AgentBot;
  },
  updateAgentBot: async (id: string, body: Partial<AgentBotInput>) => {
    const result = await request<{ bot?: AgentBot } & Partial<AgentBot>>(`/api/agent-bots/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    });
    return (result.bot ?? result) as AgentBot;
  },
  deleteAgentBot: (id: string) =>
    request<{ deleted?: boolean; ok?: boolean }>(`/api/agent-bots/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  setAgentBotPinned: async (id: string, pinned: boolean) => {
    const result = await request<{ bot?: AgentBot }>(`/api/agent-bots/${encodeURIComponent(id)}/pin`, {
      method: 'PUT',
      body: JSON.stringify({ pinned }),
    });
    return result.bot as AgentBot;
  },
  setAgentBotPrimary: async (id: string) => {
    const result = await request<{ bot?: AgentBot }>(`/api/agent-bots/${encodeURIComponent(id)}/primary`, {
      method: 'PUT',
      body: JSON.stringify({}),
    });
    return result.bot as AgentBot;
  },
  /** 把主 Agent 位交回内置的 Alpha Agent。 */
  clearAgentBotPrimary: async (id: string) => {
    const result = await request<{ cleared?: boolean }>(`/api/agent-bots/${encodeURIComponent(id)}/primary`, {
      method: 'PUT',
      body: JSON.stringify({ primary: false }),
    });
    return result.cleared === true;
  },
  /** One persona's Markdown memory file. Missing memory reads as "". */
  getBotMemory: async (id: string) => {
    const result = await request<{ memory?: string }>(`/api/agent-bots/${encodeURIComponent(id)}/memory`);
    return result.memory ?? '';
  },
  updateBotMemory: async (id: string, content: string) => {
    const result = await request<{ memory?: string }>(`/api/agent-bots/${encodeURIComponent(id)}/memory`, {
      method: 'PUT',
      body: JSON.stringify({ memory: content }),
    });
    return result.memory ?? '';
  },
  clearBotMemory: (id: string) =>
    request<{ cleared?: boolean; memory?: string }>(`/api/agent-bots/${encodeURIComponent(id)}/memory`, {
      method: 'DELETE',
    }),
  getAIImageAssets: () => request<AIImageAsset[]>('/api/ai-image/assets'),
  importAIImageAsset: (path: string) =>
    request<AIImageAsset>('/api/ai-image/assets', { method: 'POST', body: JSON.stringify({ path }) }),
  uploadAIImageAsset: (file: File) => {
    const form = new FormData();
    form.append('file', file);
    return requestForm<AIImageAsset>('/api/ai-image/assets/upload', form);
  },
  getAIImageAssetBlob: (name: string) => requestBlob(`/api/ai-image/assets/${encodeURIComponent(name)}`),
  getAIImageWorkspace: () => request<AIImageWorkspace>('/api/ai-image/workspace'),
  updateAIImageWorkspace: (body: AIImageWorkspace) =>
    request<AIImageWorkspace>('/api/ai-image/workspace', { method: 'PUT', body: JSON.stringify(body) }),
  getAIImageProjects: () => request<AIImageProjectSummary[]>('/api/ai-image/projects'),
  createAIImageProject: (name: string) =>
    request<AIImageProject>('/api/ai-image/projects', { method: 'POST', body: JSON.stringify({ name }) }),
  getAIImageProject: (id: number) => request<AIImageProject>(`/api/ai-image/projects/${id}`),
  updateAIImageProject: (id: number, body: Pick<AIImageProject, 'name' | 'workspace'>) =>
    request<AIImageProject>(`/api/ai-image/projects/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  planAIImage: (body: AIImagePlanRequest) =>
    request<AIImagePlanResult>('/api/ai-image/plan', { method: 'POST', body: JSON.stringify(body) }),
  createAIImageGeneration: (body: AIImageGenerationRequest) =>
    request<AIImageGeneration>('/api/ai-image/generations', { method: 'POST', body: JSON.stringify(body) }),
  createAIImageGenerationBatch: (body: AIImageBatchGenerationRequest) =>
    request<AIImageBatchGeneration>('/api/ai-image/generations/batch', { method: 'POST', body: JSON.stringify(body) }),
  getAIImageGenerations: () => request<AIImageGeneration[]>('/api/ai-image/generations'),
  getAIImageGeneration: (id: number) =>
    request<AIImageGeneration>(`/api/ai-image/generations/${id}`),
  getStudioScripts: () => request<StudioScript[]>('/api/scripts'),
  createStudioScript: (body: Pick<StudioScript, 'title' | 'body' | 'content_type'>) =>
    request<StudioScript>('/api/scripts', { method: 'POST', body: JSON.stringify(body) }),
  updateStudioScript: (id: number, body: Pick<StudioScript, 'title' | 'body' | 'content_type'>) =>
    request<StudioScript>(`/api/scripts/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteStudioScript: (id: number) => request<{ deleted: boolean }>(`/api/scripts/${id}`, { method: 'DELETE' }),
  getClipJobs: () => request<ClipJob[]>('/api/clip-jobs'),
  planClip: (body: { sourceMediaId: number; scriptId: number; targetDurationMs: number }) =>
    request<ClipJob>('/api/clip-jobs/plan', { method: 'POST', body: JSON.stringify(body) }),
  executeClip: (id: number) => request<ClipJob>(`/api/clip-jobs/${id}/execute`, { method: 'POST' }),
  getTemplates: () => request<PlatformTemplate[]>('/api/templates'),
  createTemplate: (body: Omit<PlatformTemplate, 'id' | 'source'>) =>
    request<PlatformTemplate>('/api/templates', { method: 'POST', body: JSON.stringify(body) }),
  updateTemplate: (id: number, body: Omit<PlatformTemplate, 'id' | 'source'>) =>
    request<PlatformTemplate>(`/api/templates/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteTemplate: (id: number) =>
    request<{ deleted: boolean }>(`/api/templates/${id}`, { method: 'DELETE' }),
  getAccountAssets: () => request<AccountAsset[]>('/api/account-assets', { cache: 'no-store' }),
  createMetaPublishTarget: (accountId: number, body: { platform?: 'facebook' | 'instagram'; name: string; externalId: string }) =>
    request<AccountAsset>(`/api/accounts/${accountId}/assets`, { method: 'POST', body: JSON.stringify(body) }),
  discoverMetaPublishTargets: (accountId: number) =>
    request<MetaTargetDiscovery>(`/api/accounts/${accountId}/assets/discover`, { method: 'POST' }),
  callTool: callStructuredTool,
  getShopListingPlans,
  getDataLinkState,
  uploadSQLiteSource,
  inspectDataLink,
  preflightDataLink,
  collectDataLink,
  confirmDataLinkReview,
  getCreatorMetricsOverview,
  getCreatorMetricPreferences,
  updateCreatorMetricPreferences,
  collectCreatorMetrics,
  getMetricsCollectionSchedule: () =>
    request<MetricsCollectionSchedule>('/api/creator-metrics/schedule', { cache: 'no-store' }),
  updateMetricsCollectionSchedule: (input: { enabled?: boolean; interval_hours?: number }) =>
    request<MetricsCollectionSchedule>('/api/creator-metrics/schedule', { method: 'PUT', body: JSON.stringify(input) }),
  // 一次 sweep 是逐账号串行采集，每账号最长 3 分钟。默认 30s 超时会在采集
  // 还在继续时就让界面报「请求超时」，用户以为失败了其实在跑。
  runMetricsCollectionSchedule: () =>
    request<MetricsCollectionRunResult>('/api/creator-metrics/schedule/run', {
      method: 'POST',
      timeoutMs: 10 * 60_000,
    }),
  getSocialWorkflowCatalog,
  getNotifications: () => request<Notification[]>('/api/notifications'),
  markNotificationRead: (id: number) =>
    request<{ changed: boolean }>(`/api/notifications/${id}/read`, { method: 'POST' }),
  markAllNotificationsRead: () =>
    request<{ changed: number }>('/api/notifications/read-all', { method: 'POST' }),
  getNotificationSettings: () =>
    request<NotificationSettings>('/api/notification-settings'),
  updateNotificationPolicy: (reminderIntervalHours: 12 | 24) =>
    request<NotificationSettings>('/api/notification-settings', {
      method: 'PUT',
      body: JSON.stringify({ reminder_interval_hours: reminderIntervalHours }),
    }),
  updateNotificationChannel: (kind: NotificationChannelKind, body: NotificationChannelUpdate) =>
    request<NotificationChannelSetting>(`/api/notification-settings/${kind}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  testNotificationChannel: (kind: NotificationChannelKind) =>
    request<{ ok: boolean; delivery_id: number }>(`/api/notification-settings/${kind}/test`, {
      method: 'POST',
      body: '{}',
    }),

  // Canonical running list (T-C01 / T-C02)
  getBrowsers: () =>
    request<{ instances: BrowserInstance[]; count?: number }>('/api/browsers').then(
      (r) => r.instances ?? [],
    ),

  // Legacy aliases → same payload
  getBrowserTabs: () =>
    request<{ instances: BrowserInstance[] }>('/api/browser/tabs').then((r) => r.instances ?? []),
  getBrowserInstances: () =>
    request<{ instances: BrowserInstance[] }>('/api/browsers').then((r) => r.instances ?? []),

  // Prefer path-style Facade routes
  startBrowser: (accountId: number) =>
    request<any>(`/api/accounts/${accountId}/browser/start`, { method: 'POST' }),

  startLoginSession: (accountId: number) => request<{ session: LoginSession; browser: any }>(`/api/accounts/${accountId}/login/start`, { method: 'POST' }),
  getMetricSummary: () => request<MetricSummary>('/api/metrics/summary'),
  // 内容保存时可以带知识库引用，但此前没有任何前端代码读取已存的引用。
  getContentKnowledgeCitations: (contentId: number) =>
    request<KnowledgeCitation[]>(`/api/contents/${contentId}/knowledge-citations`),
  getJobs: () => request<PublishJobSummary[]>('/api/jobs/list'),
  getAllJobEvents: () => request<JobEvent[]>('/api/jobs/events/all'),
  getJobEvents: (jobId: number) => request<JobEvent[]>(`/api/jobs/events?jobId=${jobId}`),
  getJob: (id: number) => request<PublishJobDetail>(`/api/jobs/${id}`),
  cancelJob: (id: number) =>
    request<PublishJobAction>(`/api/jobs/${id}/cancel`, { method: 'POST' }),
  retryJob: (id: number) =>
    request<PublishJobAction>(`/api/jobs/${id}/retry`, { method: 'POST' }),
  verifyPublishJobBackend: (id: number) =>
    request<PublishBackendVerificationResult>(`/api/publish-jobs/${id}/verify-published`, { method: 'POST' }),
  getPublishRecords: () => request<PublishRecord[]>('/api/publish-records', { cache: 'no-store' }),
  getPublishContentRecords: () => request<PublishContentRecord[]>('/api/publish-content-records'),
  deletePublishContentRecord: (id: number) => request<{ deleted: boolean }>(`/api/publish-content-records/${id}/delete`, {
    method: 'POST',
    body: JSON.stringify({ confirmPermanentDelete: true }),
  }),
  getManualPublishJobs: () => request<ManualPublishJob[]>('/api/publish-jobs/manual'),
  getProxyEndpoints: () => request<ProxyEndpoint[]>('/api/proxies'),
  getSafeSnapshot: () => request<SafeSnapshot>('/api/export/snapshot'),
  createDiagnosticSnapshot: () =>
    request<DiagnosticSnapshotResult>('/api/diagnostics/snapshot', { method: 'POST' }),
  getDiagnosticLogDirectory: () => request<DiagnosticLogDirectory>('/api/diagnostics/log-directory'),
  getStorageUsage: () => request<StorageUsage>('/api/storage/usage'),
  cleanupStorage: (key: string) =>
    request<StorageCleanupResult>('/api/storage/cleanup', { method: 'POST', body: JSON.stringify({ key }) }),
  createProxyEndpoint: (body: Pick<ProxyEndpoint, 'name' | 'server' | 'enabled'>) =>
    request<ProxyEndpoint>('/api/proxies', { method: 'POST', body: JSON.stringify(body) }),
  updateProxyEndpoint: (id: number, body: Pick<ProxyEndpoint, 'name' | 'server' | 'enabled'>) =>
    request<ProxyEndpoint>(`/api/proxies/${id}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteProxyEndpoint: (id: number) =>
    request<{ deleted: boolean }>(`/api/proxies/${id}`, { method: 'DELETE' }),
  bindProxyEndpoint: (accountId: number, proxyEndpointId: number) =>
    request<{ ok: boolean }>('/api/proxies/bind', {
      method: 'POST',
      body: JSON.stringify({ accountId, proxyEndpointId }),
    }),

  stopBrowser: (accountId: number) =>
    request<{ stopped: boolean }>(`/api/accounts/${accountId}/browser/stop`, { method: 'POST' }),

  stopBrowsers: (accountIds: number[]) =>
    request<BrowserBatchStopResponse>('/api/browsers/stop-batch', {
      method: 'POST',
      body: JSON.stringify({ account_ids: accountIds }),
    }),

  focusBrowser: (accountId: number) =>
    request<{ ok: boolean }>(`/api/accounts/${accountId}/browser/focus`, { method: 'POST' }),

  tileBrowsers: () =>
    request<{ ok: boolean }>('/api/browser/layout', { method: 'POST' }),

  updateBrowserPath: (browserPath: string) =>
    request<{ ok: boolean }>('/api/config/update', {
      method: 'POST',
      body: JSON.stringify({ browserPath }),
    }),

  updateConfig: (params: {
    browserPath: string;
    maxConcurrentBrowsers?: number;
    browserosPath?: string;
    browserEngine?: string;
  }) =>
    request<{ ok: boolean; maxConcurrentBrowsers?: number; browsers?: BrowserCandidate[] }>(
      '/api/config/update',
      {
        method: 'POST',
        body: JSON.stringify(params),
      },
    ),

  /** Scan local browsers (BrowserOS / Chromium / Chrome / fingerprint) */
  listBrowsers: () =>
    request<{
      browsers: BrowserCandidate[];
      browser_path: string;
      browseros_path?: string;
      browser_engine?: string;
      active_browser_id?: string;
    }>('/api/config/browsers'),

  /** Quick-switch: pass id (browseros|chromium|chrome|fingerprint) or auto-pick */
  selectBrowser: (opts?: { id?: string; path?: string; engine?: string }) =>
    request<{ ok: boolean; browserPath: string; engine?: string; id?: string }>(
      '/api/config/browsers',
      {
        method: 'POST',
        body: JSON.stringify(opts || {}),
      },
    ),

  assistContent: (body: { action: ContentAssistAction; title: string; caption: string; body: string; platform?: string; allow_external: boolean; use_knowledge?: boolean; knowledge_base_ids?: number[]; knowledge_query?: string }) =>
    request<ContentAssistResponse>('/api/content-assist', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  getKnowledgeBases: async () => {
    const response = await request<{ items: KnowledgeBase[] }>('/api/knowledge/bases');
    return response.items ?? [];
  },

  getShopKnowledgeBases: async () => {
    const response = await request<{ items: KnowledgeBase[] }>('/api/shop/knowledge/bases');
    return response.items ?? [];
  },

  getKnowledgeDocuments: async (baseID: number) => {
    const response = await request<{ items: KnowledgeDocument[] }>(`/api/knowledge/documents?base_id=${encodeURIComponent(baseID)}`);
    return response.items ?? [];
  },

  getShopKnowledgeDocuments: async (baseID: number) => {
    const response = await request<{ items: KnowledgeDocument[] }>(`/api/shop/knowledge/documents?base_id=${encodeURIComponent(baseID)}`);
    return response.items ?? [];
  },

  getKnowledgeScopes: async () => {
    const response = await request<{ items: KnowledgeScopeOption[] }>('/api/knowledge/scopes');
    return response.items ?? [];
  },

  createKnowledgeBase: (body: { name: string; description?: string }) =>
    request<KnowledgeBase>('/api/knowledge/bases', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  createShopKnowledgeBase: (body: { name: string; description?: string }) =>
    request<KnowledgeBase>('/api/shop/knowledge/bases', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  updateKnowledgeBase: (id: number, body: { name: string; description: string; top_k: number; max_context_chars: number }) =>
    request<KnowledgeBase>(`/api/knowledge/bases/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),

  importKnowledge: (baseID: number, files: File[]) => {
    const form = new FormData();
    form.append('base_id', String(baseID));
    for (const file of files) {
      form.append('source_name', file.webkitRelativePath || file.name);
      form.append('files', file, file.name);
    }
    return requestForm<KnowledgeImportResult>('/api/knowledge/imports', form);
  },

  importShopKnowledge: (baseID: number, files: File[]) => {
    const form = new FormData();
    form.append('base_id', String(baseID));
    for (const file of files) form.append('files', file, file.name);
    return requestForm<KnowledgeImportResult>('/api/shop/knowledge/imports', form);
  },

  importAlphaNotesKnowledge: () =>
	request<{ base: KnowledgeBase; import: KnowledgeImportResult }>('/api/knowledge/presets/alpha-notes/import', {
	  method: 'POST',
	  body: JSON.stringify({ base_id: 0 }),
	}),

  searchKnowledge: (body: { base_ids?: number[]; scopes?: KnowledgeScope[]; query: string; top_k?: number }) =>
    request<{ results: KnowledgeSearchResult[]; count: number }>('/api/knowledge/search', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  searchShopKnowledge: (body: { base_ids?: number[]; query: string; top_k?: number }) =>
    request<{ results: KnowledgeSearchResult[]; count: number }>('/api/shop/knowledge/search', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  getEgoPublishManifests: async () => {
    const response = await request<{ items: EgoPublishManifest[] }>('/api/ego/publish-manifests');
    return response.items ?? [];
  },

  layoutBrowsers: () =>
    request<{ ok: boolean }>('/api/browser/layout', { method: 'POST' }),

  browseFiles: (path?: string) =>
    request<{
      currentPath: string;
      parentPath: string;
      directories: { name: string; path: string; isDir: boolean }[];
      files: { name: string; path: string; isDir: boolean }[];
    }>(`/api/config/browse?path=${path ? encodeURIComponent(path) : ''}`),

  /** Agent multi-scheme hub */
  getAgentSchemes: () =>
    request<{
      schemes: AgentScheme[];
      opencode?: string;
      opencode_availability?: OpenCodeAvailability;
      server_time?: string;
    }>('/api/agent/schemes'),

  runAgentScheme: (body: {
    scheme: string;
    goal?: string;
    dry_run?: boolean;
    account_id?: number;
    max_steps?: number;
    model?: string;
  }) =>
    request<AgentSchemeResult>('/api/agent/scheme/run', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  agentHubProviders: (signal?: AbortSignal) =>
    request<AgentHubProviders>('/api/agent/providers', { signal }),

  agentHubProviderCatalog: () =>
    request<AgentHubProviderCatalog>('/api/agent/provider-catalog'),

  agentHubProviderCatalogProbe: (
    providerID: AgentHubProviderID,
    body: { executable_path?: string; launch_mode?: 'cc_switch' | 'claude_router' },
  ) =>
    request<AgentHubProbe>(`/api/agent/provider-catalog/${encodeURIComponent(providerID)}/probe`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  agentHubProviderCatalogConnect: (
    providerID: AgentHubProviderID,
    body: { executable_path?: string; launch_mode?: 'cc_switch' | 'claude_router' },
  ) =>
    request<{ status: 'started' }>(`/api/agent/provider-catalog/${encodeURIComponent(providerID)}/connect`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  agentHubProviderAdd: (body: { provider_id: AgentHubProviderID; executable_path?: string; launch_mode?: 'cc_switch' | 'claude_router'; model_id: string }) =>
    request<AgentHubProvider>('/api/agent/providers', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  agentHubProviderRemove: (providerID: AgentHubProviderID) =>
    request<void>(`/api/agent/providers/${encodeURIComponent(providerID)}`, {
      method: 'DELETE',
    }),

  agentHubBootstrap: (providerID?: AgentHubProviderID, productMode: 'media' | 'shop' = 'media') =>
    request<AgentHubBootstrap>(`/api/agent/bootstrap${agentHubWorkspaceQuery(providerID, productMode)}`),

  agentHubProviderProbe: (providerID: AgentHubProviderID, signal?: AbortSignal) =>
    request<{ provider_id: AgentHubProviderID; status: 'probing' }>(`/api/agent/providers/${encodeURIComponent(providerID)}/probe`, {
      method: 'POST',
      signal,
    }),

  agentHubProviderModelValidate: (providerID: AgentHubProviderID, modelID: string, signal?: AbortSignal) =>
    request<{ provider_id: AgentHubProviderID; model_id: string; status: 'validating' }>(`/api/agent/providers/${encodeURIComponent(providerID)}/models/validate`, {
      method: 'POST',
      body: JSON.stringify({ model_id: modelID }),
      signal,
    }),

  agentHubProviderModelValidateBatch: (providerID: AgentHubProviderID, modelIDs: string[], signal?: AbortSignal) =>
    request<{ provider_id: AgentHubProviderID; total: number; status: 'validating' }>(`/api/agent/providers/${encodeURIComponent(providerID)}/models/validate-batch`, {
      method: 'POST',
      body: JSON.stringify({ model_ids: modelIDs }),
      signal,
    }),

  agentHubProviderConnect: (providerID: AgentHubProviderID) =>
    request<{ status: 'started' }>(`/api/agent/providers/${encodeURIComponent(providerID)}/connect`, {
      method: 'POST',
    }),

  agentHubProviderSelection: (providerID: AgentHubProviderID, body: { conversation_id?: string; model_id?: string; draft?: string; workspace_mode?: 'media' | 'shop'; last_module?: string }) =>
    request<AgentHubProviderSelection>(`/api/agent/providers/${encodeURIComponent(providerID)}/selection`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),

  agentHubConversations: (providerID?: AgentHubProviderID, productMode: 'media' | 'shop' = 'media') =>
    request<{ conversations: AgentHubConversation[] }>(`/api/agent/conversations${agentHubWorkspaceQuery(providerID, productMode)}`),

  agentHubActivities: (runID: string, afterID = 0) =>
    request<{ activities: AgentActivity[] }>(`/api/agent/runs/${encodeURIComponent(runID)}/activities?after_id=${Math.max(0, afterID)}`),

  agentHubActivity: (id: number) =>
    request<AgentActivity>(`/api/agent/activities/${encodeURIComponent(String(id))}`),

  // Feishu Base mirror: local SQLite remains the source of truth.
  getFeishuSyncStatus: () =>
    request<FeishuSyncStatus>('/api/feishu/status', { cache: 'no-store' }),
  syncFeishu: (module = 'all') =>
    request<FeishuSyncStatus>('/api/feishu/sync', { method: 'POST', body: JSON.stringify({ module }) }),
  // In-app subagents: ask one Bot to do a bounded task for another.
  agentHubConversationToolCalls: (conversationID: string, limit = 50) =>
    request<{ tool_calls?: AgentToolCallDetail[] | null }>(
      `/api/agent/conversations/${encodeURIComponent(conversationID)}/tool-calls?limit=${limit}`,
      { cache: 'no-store' },
    ),

  listAgentDelegations: (conversationID: string) =>
    request<{ delegations?: AgentDelegation[] | null }>(`/api/agent/delegations?conversation_id=${encodeURIComponent(conversationID)}`, { cache: 'no-store' }),
  createAgentDelegation: (body: { parent_conversation_id: string; from_bot_id?: string; to_bot_id: string; task: string }) =>
    request<AgentDelegation>('/api/agent/delegations', { method: 'POST', body: JSON.stringify(body) }),
  getAgentDelegation: (id: string) =>
    request<AgentDelegation>(`/api/agent/delegations/${encodeURIComponent(id)}`, { cache: 'no-store' }),
  cancelAgentDelegation: (id: string) =>
    request<AgentDelegation>(`/api/agent/delegations/${encodeURIComponent(id)}/cancel`, { method: 'POST' }),
  agentHubConversationCreate: (body: { provider_id?: AgentHubProviderID; model_id?: string; title?: string; context?: AgentHubContext; bot_id?: string }) =>
    request<AgentHubConversation>('/api/agent/conversations', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  deleteAgentHubConversation: (id: string) =>
    request<{ deleted?: boolean; id?: string }>(`/api/agent/conversations/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  /** Clears the rail's unread badge for one conversation. */
  // Clear a whole rail row: every thread behind that Bot (or the main agent
  // when botID is empty). Opening a Bot must not leave a badge nobody can clear.
  markAgentHubConversationsReadAll: (botID?: string) =>
    request<{ updated: number }>('/api/agent/conversations/read-all', {
      method: 'POST',
      body: JSON.stringify({ bot_id: botID || '' }),
    }),
  markAgentHubConversationRead: (id: string) =>
    request<{ read?: boolean; conversation_id?: string; last_read_at?: string }>(
      `/api/agent/conversations/${encodeURIComponent(id)}/read`,
      { method: 'POST' },
    ),
  agentHubConversation: (id: string) =>
    request<AgentHubConversation>(`/api/agent/conversations/${encodeURIComponent(id)}`),

  agentHubConversationUpdate: (id: string, body: { title?: string; model_id?: string }) =>
    request<AgentHubConversation>(`/api/agent/conversations/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),

  agentHubRun: (conversationID: string, body: { provider_id?: AgentHubProviderID; model_id?: string; message: string; context: AgentHubContext; knowledge_scopes?: KnowledgeScope[]; trend_context?: string }) =>
    request<AgentHubRun>(`/api/agent/conversations/${encodeURIComponent(conversationID)}/runs`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  agentHubKnowledgeTurns: (conversationID: string) =>
    request<{ turns: AgentKnowledgeTurn[] }>(`/api/agent/conversations/${encodeURIComponent(conversationID)}/knowledge-turns`),

  agentHubRunCancel: (runID: string) =>
    request<AgentHubRun>(`/api/agent/runs/${encodeURIComponent(runID)}/cancel`, {
      method: 'POST',
    }),

  agentHubApprovalResolve: (approvalID: string, status: 'approved' | 'rejected', remember = false) =>
    request<{ approval: unknown; tool_call?: AgentHubToolCall }>(`/api/agent/approvals/${encodeURIComponent(approvalID)}/resolve`, {
      method: 'POST',
      body: JSON.stringify({ status, remember }),
    }),

  agentHubApprovalMemoryClear: () =>
    request<void>('/api/agent/approval-memory/clear', { method: 'POST' }),

  skillsList: () =>
    request<{ skills: SkillLibraryItem[] }>('/api/skills'),

  skillGet: (id: string) =>
    request<SkillLibraryItem>(`/api/skills/${encodeURIComponent(id)}`),

  skillCreate: (body: SkillCreateInput) =>
    request<SkillLibraryItem>('/api/skills', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  skillImport: (content: string) =>
    request<SkillLibraryItem>('/api/skills/import', {
      method: 'POST',
      body: JSON.stringify({ content }),
    }),

  skillSetEnabled: (id: string, enabled: boolean) =>
    request<SkillLibraryItem>(`/api/skills/${encodeURIComponent(id)}/enabled`, {
      method: 'PUT',
      body: JSON.stringify({ enabled }),
    }),

  /** In-app terminal (Claude Code / OpenCode / shell) */
  terminalProbe: () =>
    request<{
      probe: {
        shell?: { binary?: string; available?: boolean };
        claude?: { binary?: string; available?: boolean; note?: string };
        opencode?: {
          binary?: string;
          available?: boolean;
          ready?: boolean;
          availability?: OpenCodeAvailability;
          note?: string;
        };
      };
      work_dir?: string;
      data_dir?: string;
    }>('/api/terminal/probe'),

  terminalStart: (body: { kind: string; work_dir?: string; args?: string[] }) =>
    request<{
      ok: boolean;
      error?: string;
      session?: {
        id: string;
        kind: string;
        cmd_line: string;
        binary: string;
        work_dir: string;
        alive: boolean;
      };
      ws_path?: string;
    }>('/api/terminal/sessions', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  terminalStop: (id: string) =>
    request<{ ok: boolean }>('/api/terminal/stop', {
      method: 'POST',
      body: JSON.stringify({ id }),
    }),

  /** Doubao API multi-session chat */
  codingStatus: () =>
    request<{
      work_dir?: string;
      workspace?: string;
      automations?: string;
      works?: string;
      mode?: string;
      sandbox?: boolean;
      engine?: string;
      model?: string;
      default_effort?: string;
      api_configured?: boolean;
      note?: string;
      efforts?: { id: string; label: string }[];
    }>('/api/coding/status'),

  /** Updates only the fixed Doubao credential in the macOS Keychain. */
  saveDoubaoAPIKey: (apiKey: string) =>
    request<{ key_updated: boolean }>('/api/agent/models', {
      method: 'PUT',
      body: JSON.stringify({ api_key: apiKey }),
    }),

  getAgentModels: () =>
    request<AgentModelConfig>('/api/agent/models'),

  verifyAndSaveAgentModels: (body: AgentModelUpdateInput) =>
    request<AgentModelUpdateResult>('/api/agent/models', {
      method: 'PUT',
      body: JSON.stringify(body),
    }),

  codingSessions: () =>
    request<{ sessions: CodingSessionSummary[] }>('/api/coding/sessions'),

  codingSessionGet: (id: string) =>
    request<CodingSession>(`/api/coding/sessions?id=${encodeURIComponent(id)}`),

  codingSessionCreate: (body?: { title?: string; engine?: string; model?: string; effort?: string }) =>
    request<CodingSession>('/api/coding/sessions', {
      method: 'POST',
      body: JSON.stringify(body || {}),
    }),

  codingSessionDelete: (id: string) =>
    request<{ ok: boolean }>(`/api/coding/sessions?id=${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),

  codingChat: (body: {
    session_id?: string;
    engine: string;
    message: string;
    work_dir?: string;
    continue?: boolean;
    model?: string;
    effort?: string;
    thinking?: boolean;
    permission_mode?: string;
    files?: string[];
    mode?: 'chat' | 'tools';
  }) =>
    request<{
      ok: boolean;
      engine: string;
      binary?: string;
      work_dir?: string;
      model?: string;
      effort?: string;
      reply: string;
      duration?: string;
      error?: string;
      session_id?: string;
      session_title?: string;
    }>('/api/coding/chat', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  codingUpload: async (sessionId: string, file: File) => {
    const fd = new FormData();
    fd.append('session_id', sessionId);
    fd.append('file', file);
    const token = getToken();
    const headers: Record<string, string> = {};
    if (token) headers.Authorization = `Bearer ${token}`;
    const res = await fetch('/api/coding/upload', { method: 'POST', body: fd, headers });
    if (!res.ok) throw new Error(await res.text());
    return res.json() as Promise<{ ok: boolean; path: string; rel: string; name: string; size: number }>;
  },

  codingWorks: () =>
    request<{
      works_dir: string;
      items: { name: string; path: string; files: string[] }[];
      note?: string;
    }>('/api/coding/works'),

  codingWorksUpload: async (files: FileList | File[], folder?: string) => {
    const fd = new FormData();
    if (folder) fd.append('folder', folder);
    const list = Array.from(files as FileList);
    list.forEach((f) => fd.append('files', f));
    const token = getToken();
    const headers: Record<string, string> = {};
    if (token) headers.Authorization = `Bearer ${token}`;
    const res = await fetch('/api/coding/works', { method: 'POST', body: fd, headers });
    if (!res.ok) throw new Error(await res.text());
    return res.json() as Promise<{ ok: boolean; folder: string; saved: string[]; count: number; indexed_media: number }>;
  },

  browserAIChat: (body: {
    account_id: number;
    provider: string;
    url?: string;
    message: string;
  }) =>
    request<{
      ok: boolean;
      url?: string;
      provider?: string;
      step?: string;
      message?: string;
      error?: string;
    }>('/api/browser/ai-chat', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
};

export interface CodingMsg {
  id: string;
  role: string;
  text: string;
  files?: string[];
  engine?: string;
  model?: string;
  effort?: string;
  duration?: string;
  error?: boolean;
  created_at?: string;
}

export type AgentHubProviderID = string;

export interface AgentModelEntry {
  id: string;
  label?: string;
  enabled?: boolean;
  source?: string;
}

export interface AgentModelFailure {
  id?: string;
  model_id?: string;
  error_code?: string;
}

export interface AgentModelConfig {
  provider_type?: 'volcano_ark' | 'custom';
  provider_name?: string;
  base_url?: string;
  protocol?: 'responses' | 'chat_completions';
  engine: string;
  default_model: string;
  default_effort: string;
  api_configured?: boolean;
  /** The server returns only models that completed a real provider call. */
  models: AgentModelEntry[];
  updated_at?: string;
}

export interface AgentModelUpdateInput {
  api_key?: string;
  provider_type?: 'volcano_ark' | 'custom';
  provider_name?: string;
  base_url?: string;
  protocol?: 'responses' | 'chat_completions';
  /** Canonical model IDs; omit to validate the server's fixed candidate list. */
  models?: string[];
  default_model?: string;
  default_effort?: string;
}

export interface AgentModelUpdateResult extends AgentModelConfig {
  key_updated?: boolean;
  failures?: AgentModelFailure[];
  validation?: {
    tested?: number;
    passed?: number;
    failed?: AgentModelFailure[];
  };
}

export interface AgentHubContext {
  workspace?: string;
  product_mode?: 'media' | 'shop';
  page?: string;
  filters?: Record<string, string>;
  selections?: { id: string; name: string }[];
  attachments?: string[];
}

export interface AgentHubProvider {
  id: AgentHubProviderID;
  label: string;
  experimental?: boolean;
  built_in?: boolean;
  removable?: boolean;
  capabilities: string[];
}

export interface AgentHubProviderCatalogItem {
  id: AgentHubProviderID;
  label: string;
  description: string;
  install_url?: string;
  supported: boolean;
  executable_configurable: boolean;
  unavailable_reason?: string;
  integration_kind: 'right_agent';
  primary_command: string;
  command_aliases?: string[];
  version_args: string[];
  transport: string;
  auth_mode: string;
  model_source: string;
  mcp_injection: string;
  session_resume: boolean;
}

export interface AgentHubProviderCatalog {
  providers: AgentHubProviderCatalogItem[];
}

export interface AgentHubProbe {
  provider_id: AgentHubProviderID;
  state: 'unknown' | 'probing' | 'ready' | 'limited' | 'unavailable' | 'incompatible';
  available: boolean;
  ready: boolean;
  version?: string;
  /** Models that completed a real connectivity check. Drawer must only use this list. */
  models?: string[];
  /** Discovered catalog entries; visible only in Settings as validation suggestions. */
  model_candidates?: string[];
  validating_model?: string;
  model_validation_error?: string;
  model_validation_total?: number;
  model_validation_completed?: number;
  model_validation_passed?: number;
  model_validation_failures?: Array<{ model_id: string; error_code: string }>;
  reason?: string;
  cached?: boolean;
  checked_at?: string;
}

export interface AgentHubProviders {
  providers: AgentHubProvider[];
  probes: AgentHubProbe[];
}

export interface AgentHubProviderSelection {
  provider_id: AgentHubProviderID;
  conversation_id?: string;
  model_id?: string;
  draft?: string;
  updated_at?: string;
}

export interface AgentHubBootstrap extends AgentHubProviders {
  selection: AgentHubProviderSelection;
  conversations: AgentHubConversation[];
  pending_approvals: AgentHubToolCall[];
  workspace_state?: AgentWorkspaceState;
}

export interface AgentWorkspaceState {
  workspace_mode: 'media' | 'shop';
  current_conversation_id?: string;
  draft?: string;
  last_module?: string;
  updated_at?: string;
}

export type AgentActivityKind = 'skill' | 'registry_tool' | 'egolite' | 'content' | 'knowledge' | 'publish_plan' | 'approval' | 'evidence' | 'subagent' | 'system';

export interface AgentActivity {
  id: number;
  runId: string;
  kind: AgentActivityKind;
  title: string;
  summary: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';
  detailRef?: { type: string; id: string };
  occurredAt: string;
}

export interface AgentHubToolCall {
  id: string;
  run_id: string;
  conversation_id: string;
  provider_id: AgentHubProviderID;
  tool_name: string;
  workspace: string;
  risk_level: 'L0' | 'L1' | 'L2' | 'L3';
  params_hash: string;
  impact_preview: string;
  status: 'proposed' | 'pending' | 'approved' | 'rejected' | 'running' | 'completed' | 'failed' | 'expired' | 'cancelled';
  result?: Record<string, unknown>;
  error_code?: string;
  approval_id?: string;
  expires_at?: string;
  created_at: string;
  updated_at: string;
}

export interface AgentHubMessage {
  id: string;
  provider_id?: AgentHubProviderID;
  run_id?: string;
  role: 'user' | 'assistant' | 'system' | string;
  content: string;
  context?: AgentHubContext;
  created_at: string;
}

export interface AgentHubConversation {
  id: string;
  title: string;
  provider_id: AgentHubProviderID;
  model_id?: string;
  /** Persona this conversation belongs to; empty for legacy conversations. */
  bot_id?: string;
  workspace_mode?: 'media' | 'shop';
  context: AgentHubContext;
  created_at: string;
  updated_at: string;
  messages?: AgentHubMessage[];
}

export interface AgentHubRun {
  id: string;
  conversation_id: string;
  provider_id: AgentHubProviderID;
  model_id?: string;
  provider_session_id?: string;
  status: 'running' | 'completed' | 'failed' | 'cancelled' | 'interrupted' | string;
  error_code?: string;
  created_at: string;
  updated_at: string;
}

export interface AgentHubEvent {
  id: number;
  runId: string;
  conversationId: string;
  providerId: AgentHubProviderID;
  seq: number;
  timestamp: string;
  type: string;
  payload: Record<string, unknown>;
}

export interface SkillLibraryItem {
  id: string;
  name: string;
  description: string;
  instructions?: string;
  source: 'system' | 'local';
  status: 'reviewed' | 'ready' | 'review_required';
  runtime: 'instructions' | 'registry';
  runtime_ref?: string;
  enabled: boolean;
  has_scripts: boolean;
  created_at: string;
  updated_at: string;
}

export interface SkillCreateInput {
  id: string;
  name: string;
  description: string;
  instructions: string;
  automation?: boolean;
}

export interface CodingSessionSummary {
  id: string;
  title: string;
  engine: string;
  model?: string;
  effort?: string;
  created_at: string;
  updated_at: string;
  messages?: CodingMsg[];
  dir?: string;
}

export interface CodingSession extends CodingSessionSummary {
  messages: CodingMsg[];
}

export interface AgentScheme {
  id: string;
  name: string;
  tagline: string;
  description: string;
  capabilities: string[];
  requires: string[];
  weight: string;
  recommended?: boolean;
  ready?: boolean;
  ready_note?: string;
  availability?: OpenCodeAvailability;
}

export type OpenCodeAvailability =
  | 'not_found'
  | 'available_unverified'
  | 'verified_read_only'
  | 'version_mismatch';

export interface AgentSchemeResult {
  ok: boolean;
  scheme: string;
  goal: string;
  dry_run: boolean;
  message: string;
  trace_id: string;
  duration: string;
  output?: string;
  error?: string;
  steps?: Record<string, unknown>[];
  data?: Record<string, unknown>;
}
