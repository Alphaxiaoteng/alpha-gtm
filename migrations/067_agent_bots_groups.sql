ALTER TABLE agent_bots ADD COLUMN group_name TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_bots ADD COLUMN muted INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_conversations ADD COLUMN last_read_at TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_agent_bots_group ON agent_bots(group_name);
