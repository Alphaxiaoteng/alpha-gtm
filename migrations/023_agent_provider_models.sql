CREATE TABLE IF NOT EXISTS agent_provider_models (
  provider_id TEXT NOT NULL,
  model_id TEXT NOT NULL,
  provider_version TEXT NOT NULL DEFAULT '',
  verified_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  PRIMARY KEY (provider_id, model_id)
);
