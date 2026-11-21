-- Tables added after the original bootstrap. Keep this file additive so an
-- existing development database can be upgraded without rebuilding it.
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS proxy_endpoints (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  server TEXT NOT NULL UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS login_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  status TEXT NOT NULL,
  error_code TEXT,
  started_at TEXT NOT NULL,
  completed_at TEXT,
  expires_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id)
);

CREATE TABLE IF NOT EXISTS audit_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  trace_id TEXT,
  level TEXT NOT NULL DEFAULT 'info',
  category TEXT,
  message TEXT,
  related_type TEXT,
  related_id INTEGER,
  raw_json TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS post_metrics (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  publish_record_id INTEGER,
  account_id INTEGER,
  platform_code TEXT,
  content_id INTEGER,
  views INTEGER NOT NULL DEFAULT 0,
  likes INTEGER NOT NULL DEFAULT 0,
  comments INTEGER NOT NULL DEFAULT 0,
  shares INTEGER NOT NULL DEFAULT 0,
  favorites INTEGER NOT NULL DEFAULT 0,
  raw_json TEXT,
  collected_at TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS content_versions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER NOT NULL,
  lang TEXT,
  title TEXT,
  caption TEXT,
  body TEXT,
  tags_json TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS credentials (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER,
  platform_code TEXT,
  kind TEXT NOT NULL,
  ref_key TEXT NOT NULL,
  meta_json TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS media_library (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  file_path TEXT NOT NULL,
  sha256 TEXT UNIQUE,
  size_bytes INTEGER,
  mime TEXT,
  width INTEGER,
  height INTEGER,
  duration_ms INTEGER,
  probe_json TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS app_users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'admin',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_browser_profiles_proxy ON browser_profiles(proxy_id);
CREATE INDEX IF NOT EXISTS idx_proxy_endpoints_enabled ON proxy_endpoints(enabled, id);
CREATE INDEX IF NOT EXISTS idx_audit_events_trace ON audit_events(trace_id, id);
CREATE INDEX IF NOT EXISTS idx_login_sessions_account_status ON login_sessions(account_id, status, id);
