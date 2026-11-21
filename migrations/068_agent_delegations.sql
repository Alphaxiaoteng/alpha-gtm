-- Bot-to-Bot handoff: one Bot asks another Bot to run a bounded task.
-- Both sides stay real conversations and runs; this table only records the
-- handoff, its terminal state and the returned result.
CREATE TABLE IF NOT EXISTS agent_delegations (
  id TEXT PRIMARY KEY,
  parent_conversation_id TEXT NOT NULL,
  from_bot_id TEXT NOT NULL DEFAULT '',
  to_bot_id TEXT NOT NULL,
  child_conversation_id TEXT NOT NULL,
  child_run_id TEXT NOT NULL,
  task TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('running','completed','failed','cancelled')),
  result TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_delegations_parent ON agent_delegations(parent_conversation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_delegations_status ON agent_delegations(status, updated_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_delegations_running ON agent_delegations(parent_conversation_id, to_bot_id) WHERE status='running';
