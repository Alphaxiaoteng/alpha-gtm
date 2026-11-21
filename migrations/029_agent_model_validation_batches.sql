CREATE TABLE IF NOT EXISTS agent_model_validation_batches (
  provider_id TEXT PRIMARY KEY,
  provider_version TEXT NOT NULL DEFAULT '',
  remaining_models_json TEXT NOT NULL DEFAULT '[]',
  total INTEGER NOT NULL DEFAULT 0,
  completed INTEGER NOT NULL DEFAULT 0,
  passed INTEGER NOT NULL DEFAULT 0,
  failures_json TEXT NOT NULL DEFAULT '[]',
  updated_at TEXT NOT NULL
);
