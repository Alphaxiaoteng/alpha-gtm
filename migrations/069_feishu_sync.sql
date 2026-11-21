CREATE TABLE IF NOT EXISTS feishu_sync (
  module TEXT NOT NULL,
  local_key TEXT NOT NULL,
  table_id TEXT NOT NULL,
  record_id TEXT NOT NULL,
  synced_at TEXT NOT NULL,
  PRIMARY KEY (module, local_key)
);
CREATE INDEX IF NOT EXISTS idx_feishu_sync_module ON feishu_sync(module, synced_at DESC);
