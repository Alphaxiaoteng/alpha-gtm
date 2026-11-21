CREATE TABLE IF NOT EXISTS ego_session_monitor (
  id INTEGER PRIMARY KEY CHECK (id=1),
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  interval_minutes INTEGER NOT NULL DEFAULT 60 CHECK (interval_minutes BETWEEN 1 AND 1440),
  concurrency INTEGER NOT NULL DEFAULT 1 CHECK (concurrency=1),
  state TEXT NOT NULL DEFAULT 'idle' CHECK (state IN ('idle','running')),
  last_started_at TEXT NOT NULL DEFAULT '',
  last_finished_at TEXT NOT NULL DEFAULT '',
  last_error_code TEXT NOT NULL DEFAULT '',
  last_result_json TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO ego_session_monitor (id, updated_at) VALUES (1, CURRENT_TIMESTAMP);
