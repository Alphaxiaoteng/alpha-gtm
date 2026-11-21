CREATE TABLE IF NOT EXISTS creator_metric_preferences (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  history_days INTEGER NOT NULL DEFAULT 30 CHECK (history_days IN (7,30,90)),
  retention_days INTEGER NOT NULL DEFAULT 90 CHECK (retention_days IN (30,90,180,365)),
  updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO creator_metric_preferences (id, history_days, retention_days, updated_at)
VALUES (1, 30, 90, CURRENT_TIMESTAMP);
