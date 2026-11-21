-- Incremental Feishu sync: a local row whose fields changed must update its
-- Feishu record instead of being skipped as "already synced".
ALTER TABLE feishu_sync ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';
