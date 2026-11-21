CREATE TABLE IF NOT EXISTS creation_workflows (
  id TEXT PRIMARY KEY,
  conversation_id TEXT NOT NULL UNIQUE REFERENCES agent_conversations(id) ON DELETE CASCADE,
  title TEXT NOT NULL,
  platform TEXT NOT NULL DEFAULT '',
  style TEXT NOT NULL DEFAULT '',
  stage TEXT NOT NULL DEFAULT 'topic' CHECK (stage IN ('topic','copy','script','review')),
  knowledge_scopes_json TEXT NOT NULL DEFAULT '[]',
  accepted_artifacts_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_creation_workflows_updated
  ON creation_workflows(updated_at DESC, id DESC);
