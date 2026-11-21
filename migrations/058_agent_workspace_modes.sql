ALTER TABLE agent_conversations ADD COLUMN workspace_mode TEXT NOT NULL DEFAULT 'media' CHECK (workspace_mode IN ('media','shop'));

UPDATE agent_conversations
SET workspace_mode='media'
WHERE workspace_mode='' OR workspace_mode IS NULL;

CREATE TABLE IF NOT EXISTS agent_workspace_state (
  workspace_mode TEXT PRIMARY KEY CHECK (workspace_mode IN ('media','shop')),
  current_conversation_id TEXT REFERENCES agent_conversations(id) ON DELETE SET NULL,
  draft TEXT NOT NULL DEFAULT '',
  last_module TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_conversations_workspace_mode_updated
  ON agent_conversations(workspace_mode, updated_at DESC, id);
