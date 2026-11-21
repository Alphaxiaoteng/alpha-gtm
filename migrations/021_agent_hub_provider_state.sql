ALTER TABLE agent_messages ADD COLUMN provider_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_messages ADD COLUMN run_id TEXT NOT NULL DEFAULT '';

UPDATE agent_messages
SET provider_id = COALESCE((
  SELECT provider_id FROM agent_conversations
  WHERE agent_conversations.id = agent_messages.conversation_id
), '')
WHERE provider_id = '';

CREATE TABLE IF NOT EXISTS agent_provider_selections (
  provider_id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL DEFAULT '',
  model_id TEXT NOT NULL DEFAULT '',
  draft TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_provider_cache (
  provider_id TEXT PRIMARY KEY,
  state TEXT NOT NULL,
  available INTEGER NOT NULL DEFAULT 0,
  ready INTEGER NOT NULL DEFAULT 0,
  version TEXT NOT NULL DEFAULT '',
  models_json TEXT NOT NULL DEFAULT '[]',
  reason TEXT NOT NULL DEFAULT '',
  checked_at TEXT NOT NULL DEFAULT '',
  model_expires_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_agent_conversations_provider_updated
  ON agent_conversations(provider_id, updated_at DESC, id);
