-- 每个 profile 同一时刻只允许一个 Fleet/Agent 浏览器动作。
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS browser_operation_leases (
  profile_id    TEXT PRIMARY KEY,
  operation     TEXT NOT NULL,
  owner         TEXT NOT NULL,
  acquired_at   TEXT NOT NULL,
  expires_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_browser_operation_leases_expiry ON browser_operation_leases(expires_at, profile_id);
