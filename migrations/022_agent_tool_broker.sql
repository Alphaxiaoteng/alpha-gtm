CREATE TABLE IF NOT EXISTS agent_settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_tool_calls (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  conversation_id TEXT NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
  provider_id TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  workspace TEXT NOT NULL,
  risk_level TEXT NOT NULL CHECK (risk_level IN ('L0','L1','L2','L3')),
  params_json TEXT NOT NULL,
  params_hash TEXT NOT NULL,
  impact_preview TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('proposed','pending','approved','rejected','running','completed','failed','expired','cancelled')),
  result_json TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  consumed_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_agent_tool_calls_hash
  ON agent_tool_calls(run_id, params_hash);
CREATE INDEX IF NOT EXISTS idx_agent_tool_calls_status
  ON agent_tool_calls(status, expires_at);
