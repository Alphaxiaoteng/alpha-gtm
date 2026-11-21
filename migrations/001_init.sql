PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS platforms (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  icon TEXT,
  login_url TEXT,
  manager_url TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  supports_article INTEGER NOT NULL DEFAULT 0,
  supports_graph_text INTEGER NOT NULL DEFAULT 0,
  supports_video INTEGER NOT NULL DEFAULT 0,
  sort INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT 'system',
  source_platform_id INTEGER,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS accounts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  platform_code TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'browser_login',
  name TEXT NOT NULL,
  avatar TEXT,
  host TEXT,
  account_identifier TEXT,
  manager_addr TEXT,
  login_state TEXT NOT NULL DEFAULT 'unknown',
  login_time TEXT,
  last_check_at TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort INTEGER NOT NULL DEFAULT 0,
  remark TEXT,
  source TEXT NOT NULL DEFAULT 'manual',
  source_account_id INTEGER,
  account_group TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);

CREATE TABLE IF NOT EXISTS browser_profiles (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  profile_dir TEXT NOT NULL,
  browser_type TEXT NOT NULL DEFAULT 'fingerprint-chromium',
  fingerprint_seed INTEGER NOT NULL,
  lang TEXT NOT NULL DEFAULT 'en-US',
  accept_lang TEXT NOT NULL DEFAULT 'en-US,en',
  timezone TEXT NOT NULL DEFAULT 'America/Los_Angeles',
  proxy_id INTEGER,
  cookie_encrypted TEXT,
  cookie_import_state TEXT NOT NULL DEFAULT 'none',
  last_cdp_port INTEGER,
  last_cdp_url TEXT,
  fingerprint_config TEXT NOT NULL DEFAULT '',
  last_pid INTEGER,
  proxy_server TEXT NOT NULL DEFAULT '',
  is_proxy_custom INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (account_id) REFERENCES accounts(id)
);

CREATE TABLE IF NOT EXISTS account_assets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL,
  asset_type TEXT NOT NULL,
  name TEXT NOT NULL,
  external_id TEXT,
  avatar TEXT,
  connected_asset_id INTEGER,
  connected_asset_name TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  last_seen_at TEXT,
  raw_json TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);

CREATE TABLE IF NOT EXISTS platform_settings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  platform_code TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT,
  settings_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  is_default INTEGER NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT 'manual',
  source_setting_id INTEGER,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);

CREATE TABLE IF NOT EXISTS contents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL,
  title TEXT,
  caption TEXT,
  body TEXT,
  tags_json TEXT,
  status TEXT NOT NULL DEFAULT 'draft',
  publish_count INTEGER NOT NULL DEFAULT 0,
  raw_publish_data TEXT,
  planned_at TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'manual',
  source_content_id INTEGER,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS media_assets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER,
  kind TEXT NOT NULL,
  file_path TEXT NOT NULL,
  original_path TEXT,
  mime_type TEXT,
  size_bytes INTEGER,
  sha256 TEXT,
  width INTEGER,
  height INTEGER,
  duration_ms INTEGER,
  source TEXT NOT NULL DEFAULT 'manual',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (content_id) REFERENCES contents(id)
);

CREATE TABLE IF NOT EXISTS batch_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  operation_type TEXT NOT NULL,
  platform_code TEXT,
  status TEXT NOT NULL,
  total_items INTEGER NOT NULL DEFAULT 0,
  pending_count INTEGER NOT NULL DEFAULT 0,
  running_count INTEGER NOT NULL DEFAULT 0,
  success_count INTEGER NOT NULL DEFAULT 0,
  failed_count INTEGER NOT NULL DEFAULT 0,
  skipped_count INTEGER NOT NULL DEFAULT 0,
  concurrency INTEGER NOT NULL DEFAULT 1,
  settings_json TEXT,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS batch_items (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER NOT NULL,
  item_type TEXT NOT NULL,
  item_id INTEGER,
  account_id INTEGER,
  asset_id INTEGER,
  content_id INTEGER,
  job_id INTEGER,
  status TEXT NOT NULL,
  current_step TEXT,
  error_code TEXT,
  error_message TEXT,
  screenshot_path TEXT,
  dom_snapshot_path TEXT,
  raw_json TEXT,
  started_at TEXT,
  finished_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (batch_id) REFERENCES batch_runs(id)
);

CREATE TABLE IF NOT EXISTS publish_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id INTEGER,
  content_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  asset_id INTEGER,
  platform_code TEXT NOT NULL,
  content_type TEXT NOT NULL,
  mode TEXT NOT NULL DEFAULT 'browser',
  status TEXT NOT NULL,
  current_step TEXT,
  settings_json TEXT,
  started_at TEXT,
  finished_at TEXT,
  retry_count INTEGER NOT NULL DEFAULT 0,
  max_retries INTEGER NOT NULL DEFAULT 2,
  error_code TEXT,
  error_message TEXT,
  trace_id TEXT,
  idempotency_key TEXT,
  cancel_requested_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (batch_id) REFERENCES batch_runs(id),
  FOREIGN KEY (content_id) REFERENCES contents(id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);

CREATE TABLE IF NOT EXISTS job_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id INTEGER NOT NULL,
  level TEXT NOT NULL,
  step TEXT,
  event_type TEXT NOT NULL,
  message TEXT,
  screenshot_path TEXT,
  dom_snapshot_path TEXT,
  raw_json TEXT,
  trace_id TEXT,
  request_id TEXT,
  task_id TEXT,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  error_code TEXT,
  error_class TEXT,
  retryable INTEGER NOT NULL DEFAULT 0,
  schema_version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (job_id) REFERENCES publish_jobs(id)
);

CREATE TABLE IF NOT EXISTS publish_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id INTEGER,
  content_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  asset_id INTEGER,
  platform_code TEXT NOT NULL,
  content_type TEXT NOT NULL,
  status TEXT NOT NULL,
  post_url TEXT,
  external_post_id TEXT,
  publish_data TEXT,
  published_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (job_id) REFERENCES publish_jobs(id),
  FOREIGN KEY (content_id) REFERENCES contents(id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);

CREATE TABLE IF NOT EXISTS record_infos (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  publish_record_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  asset_id INTEGER,
  platform_code TEXT NOT NULL,
  link TEXT,
  read_num INTEGER,
  comment_num INTEGER,
  share_num INTEGER,
  collect_num INTEGER,
  like_num INTEGER,
  success INTEGER NOT NULL DEFAULT 0,
  setting TEXT,
  reason TEXT,
  since TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (publish_record_id) REFERENCES publish_records(id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);

CREATE TABLE IF NOT EXISTS notifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL,
  level TEXT NOT NULL DEFAULT 'info',
  title TEXT NOT NULL,
  body TEXT,
  read INTEGER NOT NULL DEFAULT 0,
  related_type TEXT,
  related_id INTEGER,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS task_queue (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  payload_json TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  priority INTEGER NOT NULL DEFAULT 0,
  run_after TEXT,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  trace_id TEXT,
  lease_owner TEXT,
  lease_expires_at TEXT,
  cancel_requested_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_accounts_platform ON accounts(platform_code);
CREATE INDEX IF NOT EXISTS idx_accounts_login_state ON accounts(login_state);
CREATE INDEX IF NOT EXISTS idx_browser_profiles_account ON browser_profiles(account_id);
CREATE INDEX IF NOT EXISTS idx_account_assets_account ON account_assets(account_id);
CREATE INDEX IF NOT EXISTS idx_account_assets_platform ON account_assets(platform_code);
CREATE INDEX IF NOT EXISTS idx_platform_settings_platform ON platform_settings(platform_code);
CREATE INDEX IF NOT EXISTS idx_media_assets_content ON media_assets(content_id);
CREATE INDEX IF NOT EXISTS idx_batch_items_batch ON batch_items(batch_id);
CREATE INDEX IF NOT EXISTS idx_publish_jobs_batch ON publish_jobs(batch_id);
CREATE INDEX IF NOT EXISTS idx_publish_jobs_content ON publish_jobs(content_id);
CREATE INDEX IF NOT EXISTS idx_publish_jobs_account ON publish_jobs(account_id);
CREATE INDEX IF NOT EXISTS idx_publish_jobs_platform ON publish_jobs(platform_code);
CREATE INDEX IF NOT EXISTS idx_publish_jobs_trace ON publish_jobs(trace_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_publish_jobs_idempotency ON publish_jobs(idempotency_key) WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';
CREATE INDEX IF NOT EXISTS idx_job_events_job ON job_events(job_id, id);
CREATE INDEX IF NOT EXISTS idx_job_events_trace ON job_events(trace_id, id);
CREATE INDEX IF NOT EXISTS idx_publish_records_job ON publish_records(job_id);
CREATE INDEX IF NOT EXISTS idx_publish_records_content ON publish_records(content_id);
CREATE INDEX IF NOT EXISTS idx_publish_records_account ON publish_records(account_id);
CREATE INDEX IF NOT EXISTS idx_publish_records_platform ON publish_records(platform_code);
CREATE INDEX IF NOT EXISTS idx_record_infos_record ON record_infos(publish_record_id);
CREATE INDEX IF NOT EXISTS idx_record_infos_account ON record_infos(account_id);
CREATE INDEX IF NOT EXISTS idx_record_infos_platform ON record_infos(platform_code);
CREATE INDEX IF NOT EXISTS idx_task_queue_ready ON task_queue(status, run_after, priority, id);
CREATE INDEX IF NOT EXISTS idx_task_queue_lease ON task_queue(status, lease_expires_at, id);
CREATE TRIGGER IF NOT EXISTS trg_accounts_platform_insert
BEFORE INSERT ON accounts
WHEN NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
BEGIN SELECT RAISE(ABORT, 'accounts platform foreign key'); END;

CREATE TRIGGER IF NOT EXISTS trg_accounts_platform_update
BEFORE UPDATE OF platform_code ON accounts
WHEN NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
BEGIN SELECT RAISE(ABORT, 'accounts platform foreign key'); END;

CREATE TRIGGER IF NOT EXISTS trg_publish_jobs_refs_insert
BEFORE INSERT ON publish_jobs
WHEN NOT EXISTS (SELECT 1 FROM contents WHERE id=NEW.content_id)
  OR NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.account_id)
  OR NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
  OR (NEW.batch_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM batch_runs WHERE id=NEW.batch_id))
BEGIN SELECT RAISE(ABORT, 'publish_jobs foreign key'); END;

CREATE TRIGGER IF NOT EXISTS trg_publish_jobs_refs_update
BEFORE UPDATE OF batch_id, content_id, account_id, platform_code ON publish_jobs
WHEN NOT EXISTS (SELECT 1 FROM contents WHERE id=NEW.content_id)
  OR NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.account_id)
  OR NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
  OR (NEW.batch_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM batch_runs WHERE id=NEW.batch_id))
BEGIN SELECT RAISE(ABORT, 'publish_jobs foreign key'); END;

CREATE TRIGGER IF NOT EXISTS trg_job_events_job_insert
BEFORE INSERT ON job_events
WHEN NOT EXISTS (SELECT 1 FROM publish_jobs WHERE id=NEW.job_id)
BEGIN SELECT RAISE(ABORT, 'job_events job foreign key'); END;

CREATE TRIGGER IF NOT EXISTS trg_job_events_job_update
BEFORE UPDATE OF job_id ON job_events
WHEN NOT EXISTS (SELECT 1 FROM publish_jobs WHERE id=NEW.job_id)
BEGIN SELECT RAISE(ABORT, 'job_events job foreign key'); END;

CREATE TRIGGER IF NOT EXISTS trg_publish_records_refs_insert
BEFORE INSERT ON publish_records
WHEN (NEW.job_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM publish_jobs WHERE id=NEW.job_id))
  OR NOT EXISTS (SELECT 1 FROM contents WHERE id=NEW.content_id)
  OR NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.account_id)
  OR NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
BEGIN SELECT RAISE(ABORT, 'publish_records foreign key'); END;

CREATE TRIGGER IF NOT EXISTS trg_publish_records_refs_update
BEFORE UPDATE OF job_id, content_id, account_id, platform_code ON publish_records
WHEN (NEW.job_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM publish_jobs WHERE id=NEW.job_id))
  OR NOT EXISTS (SELECT 1 FROM contents WHERE id=NEW.content_id)
  OR NOT EXISTS (SELECT 1 FROM accounts WHERE id=NEW.account_id)
  OR NOT EXISTS (SELECT 1 FROM platforms WHERE code=NEW.platform_code)
BEGIN SELECT RAISE(ABORT, 'publish_records foreign key'); END;

CREATE TRIGGER IF NOT EXISTS trg_publish_records_unique_job
BEFORE INSERT ON publish_records
WHEN NEW.job_id IS NOT NULL AND EXISTS (SELECT 1 FROM publish_records WHERE job_id=NEW.job_id)
BEGIN SELECT RAISE(ABORT, 'duplicate publish record job'); END;

INSERT INTO platforms (code, name, icon, login_url, manager_url, supports_article, supports_graph_text, supports_video, sort, source)
VALUES
  ('instagram', 'Instagram', '📷', 'https://www.instagram.com', 'https://business.facebook.com', 0, 1, 1, 1, 'system'),
  ('facebook', 'Facebook', '👍', 'https://www.facebook.com', 'https://business.facebook.com', 1, 1, 1, 2, 'system'),
  ('xhs', '小红书', '📕', 'https://www.xiaohongshu.com', 'https://creator.xiaohongshu.com', 1, 1, 1, 3, 'system'),
  ('douyin', '抖音', '🎵', 'https://www.douyin.com', 'https://creator.douyin.com', 0, 0, 1, 4, 'system'),
  ('wechat_video', '视频号', '💬', 'https://channels.weixin.qq.com', 'https://channels.weixin.qq.com', 0, 0, 1, 5, 'system'),
  ('kuaishou', '快手', '🎬', 'https://passport.kuaishou.com/pc/account/login/?sid=kuaishou.web.cp.api&callback=https%3A%2F%2Fcp.kuaishou.com%2Frest%2Finfra%2Fsts%3FfollowUrl%3Dhttps%253A%252F%252Fcp.kuaishou.com%252Fprofile%26setRootDomain%3Dtrue', 'https://cp.kuaishou.com', 0, 1, 1, 6, 'system'),
  ('wechat_mp', '微信公众号', '📰', 'https://mp.weixin.qq.com', 'https://mp.weixin.qq.com/cgi-bin/home', 1, 0, 0, 7, 'system'),
  ('youtube', 'YouTube', '▶️', 'https://www.youtube.com', 'https://studio.youtube.com', 0, 0, 1, 8, 'system'),
  ('tiktok', 'TikTok', '🎞️', 'https://www.tiktok.com', 'https://www.tiktok.com/tiktokstudio', 0, 0, 1, 9, 'system'),
  ('x', 'X', '𝕏', 'https://x.com', 'https://x.com', 0, 1, 1, 10, 'system'),
  ('toutiao', '今日头条', '📰', 'https://www.toutiao.com', 'https://mp.toutiao.com', 1, 1, 1, 11, 'system'),
  ('omtencent', '腾讯内容开放平台', '企鹅', 'https://om.qq.com', 'https://om.qq.com', 1, 0, 1, 12, 'system'),
  ('weishi', '微视', '🎞️', 'https://weishi.qq.com', 'https://weishi.qq.com', 0, 0, 1, 13, 'system'),
  ('bilibili', '哔哩哔哩', '📺', 'https://www.bilibili.com', 'https://member.bilibili.com/platform/home', 1, 0, 1, 14, 'system'),
  ('acfun', 'AcFun', '🅰️', 'https://www.acfun.cn', 'https://member.acfun.cn', 1, 0, 1, 15, 'system'),
  ('baijiahao', '百家号', '百', 'https://baijiahao.baidu.com', 'https://baijiahao.baidu.com', 1, 1, 1, 16, 'system'),
  ('sina', '新浪微博', '微博', 'https://weibo.com', 'https://weibo.com', 1, 1, 1, 17, 'system'),
  ('jianshu', '简书', '简', 'https://www.jianshu.com', 'https://www.jianshu.com', 1, 0, 0, 18, 'system'),
  ('zhihu', '知乎', '知', 'https://www.zhihu.com', 'https://www.zhihu.com/creator', 1, 1, 1, 19, 'system'),
  ('juejin', '掘金', '掘', 'https://juejin.cn', 'https://juejin.cn/creator', 1, 1, 0, 20, 'system'),
  ('csdn', 'CSDN', 'C', 'https://www.csdn.net', 'https://mp.csdn.net', 1, 0, 1, 21, 'system'),
  ('pdd', '拼多多', '拼', 'https://mms.pinduoduo.com', 'https://mms.pinduoduo.com', 0, 0, 1, 22, 'system'),
  ('custom', '自定义平台', '⚙️', '', '', 1, 1, 1, 23, 'system')
ON CONFLICT(code) DO UPDATE SET
  name = excluded.name,
  icon = excluded.icon,
  login_url = excluded.login_url,
  manager_url = excluded.manager_url,
  supports_article = excluded.supports_article,
  supports_graph_text = excluded.supports_graph_text,
  supports_video = excluded.supports_video,
  sort = excluded.sort,
  source = excluded.source,
  updated_at = CURRENT_TIMESTAMP;
