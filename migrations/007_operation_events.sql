-- User-visible Agent / Tool Registry timeline. This table only carries
-- allowlisted operation metadata; tool arguments and browser/CDP payloads
-- must never be stored here.
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS operation_events (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  trace_id      TEXT NOT NULL,
  request_id    TEXT,
  task_id       TEXT,
  account_id    INTEGER,
  platform_code TEXT,
  component     TEXT NOT NULL,
  event_type    TEXT NOT NULL,
  step          TEXT,
  status        TEXT,
  level         TEXT NOT NULL DEFAULT 'info',
  error_code    TEXT,
  duration_ms   INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_operation_events_trace ON operation_events(trace_id, id);
CREATE INDEX IF NOT EXISTS idx_operation_events_account ON operation_events(account_id, id);
