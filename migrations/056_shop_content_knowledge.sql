CREATE TABLE IF NOT EXISTS shop_contents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL CHECK (type IN ('article','graph_text','video')),
  title TEXT NOT NULL DEFAULT '',
  caption TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  tags_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
  planned_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS shop_content_media (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER NOT NULL REFERENCES shop_contents(id) ON DELETE CASCADE,
  library_id INTEGER NOT NULL REFERENCES media_library(id) ON DELETE RESTRICT,
  role TEXT NOT NULL DEFAULT 'content' CHECK (role IN ('content','cover')),
  position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
  created_at TEXT NOT NULL,
  UNIQUE(content_id, library_id, role)
);

CREATE TABLE IF NOT EXISTS shop_knowledge_bases (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  is_default INTEGER NOT NULL DEFAULT 0,
  top_k INTEGER NOT NULL DEFAULT 6,
  max_context_chars INTEGER NOT NULL DEFAULT 12000,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS shop_knowledge_import_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES shop_knowledge_bases(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'running',
  total_files INTEGER NOT NULL DEFAULT 0,
  imported_files INTEGER NOT NULL DEFAULT 0,
  skipped_files INTEGER NOT NULL DEFAULT 0,
  failed_files INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  completed_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS shop_knowledge_documents (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES shop_knowledge_bases(id) ON DELETE CASCADE,
  import_run_id INTEGER REFERENCES shop_knowledge_import_runs(id) ON DELETE SET NULL,
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

CREATE TABLE IF NOT EXISTS shop_knowledge_chunks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  base_id INTEGER NOT NULL REFERENCES shop_knowledge_bases(id) ON DELETE CASCADE,
  document_id INTEGER NOT NULL REFERENCES shop_knowledge_documents(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  heading_path TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  char_count INTEGER NOT NULL,
  created_at TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS shop_knowledge_chunks_fts USING fts5(
  chunk_id UNINDEXED,
  base_id UNINDEXED,
  document_id UNINDEXED,
  heading_path,
  content,
  tokenize='trigram'
);

CREATE INDEX IF NOT EXISTS idx_shop_contents_planned ON shop_contents(planned_at, id DESC);
CREATE INDEX IF NOT EXISTS idx_shop_content_media_content ON shop_content_media(content_id, role, position, id);
CREATE INDEX IF NOT EXISTS idx_shop_knowledge_documents_base ON shop_knowledge_documents(base_id, updated_at DESC, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_shop_knowledge_documents_base_sha ON shop_knowledge_documents(base_id, sha256);
CREATE UNIQUE INDEX IF NOT EXISTS idx_shop_knowledge_chunks_document_ordinal ON shop_knowledge_chunks(document_id, ordinal);
CREATE INDEX IF NOT EXISTS idx_shop_knowledge_import_runs_base ON shop_knowledge_import_runs(base_id, created_at DESC, id DESC);
