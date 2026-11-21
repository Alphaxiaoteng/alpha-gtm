CREATE TABLE IF NOT EXISTS agent_provider_configs (
  provider_id TEXT PRIMARY KEY,
  position INTEGER NOT NULL DEFAULT 0,
  added_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
