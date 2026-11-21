CREATE TABLE IF NOT EXISTS agent_bots (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT '',
  instructions TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  avatar TEXT NOT NULL DEFAULT '',
  color TEXT NOT NULL DEFAULT '',
  effort TEXT NOT NULL DEFAULT '',
  kickoff TEXT NOT NULL DEFAULT '',
  capabilities_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

ALTER TABLE agent_conversations ADD COLUMN bot_id TEXT NOT NULL DEFAULT '';

UPDATE agent_conversations
SET bot_id=''
WHERE bot_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_agent_conversations_bot_updated
  ON agent_conversations(bot_id, updated_at DESC, id);
