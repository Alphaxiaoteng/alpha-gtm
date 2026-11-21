PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS tikhub_api_calls (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  trace_id         TEXT NOT NULL,
  platform_code    TEXT NOT NULL,
  endpoint_id      TEXT NOT NULL,
  capability       TEXT NOT NULL,
  method           TEXT NOT NULL,
  path             TEXT NOT NULL,
  parameters_json  TEXT NOT NULL DEFAULT '{}',
  status           TEXT NOT NULL DEFAULT 'running',
  error_code       TEXT NOT NULL DEFAULT '',
  provider_code    TEXT NOT NULL DEFAULT '',
  provider_message TEXT NOT NULL DEFAULT '',
  item_count       INTEGER NOT NULL DEFAULT 0,
  response_bytes   INTEGER NOT NULL DEFAULT 0,
  raw_path         TEXT NOT NULL DEFAULT '',
  started_at       TEXT NOT NULL,
  finished_at      TEXT NOT NULL DEFAULT '',
  duration_ms      INTEGER NOT NULL DEFAULT 0,
  created_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tikhub_api_calls_platform_created
  ON tikhub_api_calls(platform_code, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_tikhub_api_calls_endpoint_created
  ON tikhub_api_calls(endpoint_id, created_at DESC, id DESC);
