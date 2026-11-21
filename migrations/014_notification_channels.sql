CREATE TABLE IF NOT EXISTS notification_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_channels (
  kind TEXT PRIMARY KEY CHECK (kind IN ('feishu','dingtalk','webhook','email')),
  enabled INTEGER NOT NULL DEFAULT 0,
  smtp_host TEXT NOT NULL DEFAULT '',
  smtp_port INTEGER NOT NULL DEFAULT 0,
  smtp_security TEXT NOT NULL DEFAULT '',
  smtp_username TEXT NOT NULL DEFAULT '',
  email_from TEXT NOT NULL DEFAULT '',
  email_to_json TEXT NOT NULL DEFAULT '[]',
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_deliveries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,
  event_type TEXT NOT NULL,
  account_id INTEGER,
  status TEXT NOT NULL CHECK (status IN ('pending','delivered','failed')),
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  delivered_at TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_account
  ON notification_deliveries(account_id, kind, status, created_at DESC, id DESC);

INSERT OR IGNORE INTO notification_settings(key, value, updated_at)
VALUES ('reminder_interval_hours', '12', CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO notification_channels(kind, updated_at) VALUES
  ('feishu', CURRENT_TIMESTAMP),
  ('dingtalk', CURRENT_TIMESTAMP),
  ('webhook', CURRENT_TIMESTAMP),
  ('email', CURRENT_TIMESTAMP);
