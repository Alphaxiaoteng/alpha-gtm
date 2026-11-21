-- T-E01: multi-browser runtime / lease tables
-- Mirrors internal/db schemaBrowserRuntimeInstances / schemaProfileLeases / schemaBrowserEvents
-- Applied by Go db.Migrate() at runtime; this file is for empty-DB / init-dev-db parity.

PRAGMA foreign_keys = ON;

-- Running browser instances (process-local truth; Supervisor clears on restart)
CREATE TABLE IF NOT EXISTS browser_runtime_instances (
  profile_id    TEXT PRIMARY KEY,
  account_id    INTEGER,
  pid           INTEGER NOT NULL,
  cdp_port      INTEGER NOT NULL,
  cdp_url       TEXT NOT NULL,
  state         TEXT NOT NULL DEFAULT 'starting',
  active_job_id INTEGER,
  lease_owner   TEXT,
  started_at    TEXT NOT NULL,
  last_seen_at  TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

-- Cross-process profile mutual exclusion (TTL lease)
CREATE TABLE IF NOT EXISTS profile_leases (
  profile_id    TEXT PRIMARY KEY,
  holder_type   TEXT NOT NULL,
  holder_id     TEXT NOT NULL,
  acquired_at   TEXT NOT NULL,
  expires_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL
);

-- Browser lifecycle events (start/stop/crash/heartbeat)
CREATE TABLE IF NOT EXISTS browser_events (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  profile_id    TEXT NOT NULL,
  event_type    TEXT NOT NULL,
  message       TEXT,
  pid           INTEGER,
  cdp_port      INTEGER,
  job_id        INTEGER,
  created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_browser_runtime_cdp_port ON browser_runtime_instances(cdp_port);
CREATE INDEX IF NOT EXISTS idx_profile_leases_expires ON profile_leases(expires_at);
CREATE INDEX IF NOT EXISTS idx_browser_events_profile ON browser_events(profile_id);
