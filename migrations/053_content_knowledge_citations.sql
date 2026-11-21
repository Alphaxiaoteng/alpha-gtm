CREATE TABLE IF NOT EXISTS content_knowledge_citations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER NOT NULL REFERENCES contents(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
  chunk_id INTEGER NOT NULL CHECK (chunk_id > 0),
  base_id INTEGER NOT NULL CHECK (base_id > 0),
  document_sha256 TEXT NOT NULL CHECK (length(document_sha256) = 64),
  source_name TEXT NOT NULL,
  heading_path TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(content_id, ordinal),
  UNIQUE(content_id, chunk_id, document_sha256)
);

CREATE INDEX IF NOT EXISTS idx_content_knowledge_citations_content
  ON content_knowledge_citations(content_id, ordinal);
