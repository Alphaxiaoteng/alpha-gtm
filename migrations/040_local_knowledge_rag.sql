CREATE TABLE IF NOT EXISTS knowledge_bases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  is_default INTEGER NOT NULL DEFAULT 0,
  top_k INTEGER NOT NULL DEFAULT 6,
  max_context_chars INTEGER NOT NULL DEFAULT 12000,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS knowledge_import_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'running',
  total_files INTEGER NOT NULL DEFAULT 0,
  imported_files INTEGER NOT NULL DEFAULT 0,
  skipped_files INTEGER NOT NULL DEFAULT 0,
  failed_files INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  completed_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS knowledge_documents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
  import_run_id INTEGER REFERENCES knowledge_import_runs(id) ON DELETE SET NULL,
  source_name TEXT NOT NULL,
  source_type TEXT NOT NULL,
  sha256 TEXT NOT NULL,
  status TEXT NOT NULL,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  original_path TEXT NOT NULL DEFAULT '',
  normalized_path TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS knowledge_chunks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
  document_id INTEGER NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  heading_path TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  char_count INTEGER NOT NULL,
  created_at TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_chunks_fts USING fts5(
  chunk_id UNINDEXED,
  base_id UNINDEXED,
  document_id UNINDEXED,
  heading_path,
  content,
  tokenize='trigram'
);

CREATE INDEX IF NOT EXISTS idx_knowledge_documents_base ON knowledge_documents(base_id, updated_at DESC, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_documents_base_sha ON knowledge_documents(base_id, sha256);
CREATE UNIQUE INDEX IF NOT EXISTS idx_knowledge_chunks_document_ordinal ON knowledge_chunks(document_id, ordinal);
CREATE INDEX IF NOT EXISTS idx_knowledge_import_runs_base ON knowledge_import_runs(base_id, created_at DESC, id DESC);
