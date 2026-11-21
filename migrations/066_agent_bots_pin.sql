ALTER TABLE agent_bots ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agent_bots ADD COLUMN is_primary INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_agent_bots_pinned ON agent_bots(pinned, created_at DESC);
