-- 跨进程 CDP 端口预约。运行时表写入前短暂持有，避免多个进程分到同一端口。
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS cdp_port_leases (
  cdp_port      INTEGER PRIMARY KEY,
  profile_id    TEXT NOT NULL UNIQUE,
  owner         TEXT NOT NULL,
  acquired_at   TEXT NOT NULL,
  expires_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cdp_port_leases_expiry ON cdp_port_leases(expires_at, cdp_port);
