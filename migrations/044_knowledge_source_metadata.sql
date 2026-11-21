ALTER TABLE knowledge_documents ADD COLUMN source_size_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE knowledge_documents ADD COLUMN source_modified_ns INTEGER NOT NULL DEFAULT 0;
