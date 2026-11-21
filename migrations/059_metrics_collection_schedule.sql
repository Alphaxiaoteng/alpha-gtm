-- 059: creator-metrics scheduled auto-collection switch (single row).
-- The Go runtime seeds id=1 and persists monitor run state here.

CREATE TABLE IF NOT EXISTS metrics_collection_schedule (
  id INTEGER PRIMARY KEY CHECK (id=1),
  enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
  interval_hours INTEGER NOT NULL DEFAULT 24 CHECK (interval_hours BETWEEN 1 AND 168),
  state TEXT NOT NULL DEFAULT 'idle' CHECK (state IN ('idle','running')),
  last_started_at TEXT NOT NULL DEFAULT '',
  last_finished_at TEXT NOT NULL DEFAULT '',
  last_error_code TEXT NOT NULL DEFAULT '',
  last_result_json TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO metrics_collection_schedule (id, updated_at) VALUES (1, '2026-01-01T00:00:00Z');
