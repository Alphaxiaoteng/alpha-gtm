CREATE TABLE IF NOT EXISTS data_link_sources (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL CHECK (kind IN ('sqlite')),
  name TEXT NOT NULL,
  original_name TEXT NOT NULL,
  file_path TEXT NOT NULL,
  file_size INTEGER NOT NULL CHECK (file_size > 0),
  sha256 TEXT NOT NULL CHECK (length(sha256) = 64),
  status TEXT NOT NULL CHECK (status IN ('ready','failed')),
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(kind, sha256)
);

CREATE TABLE IF NOT EXISTS data_link_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES data_link_sources(id) ON DELETE RESTRICT,
  object_name TEXT NOT NULL DEFAULT '',
  query_sha256 TEXT NOT NULL CHECK (length(query_sha256) = 64),
  schema_fingerprint TEXT NOT NULL CHECK (length(schema_fingerprint) = 64),
  mapping_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL CHECK (status IN ('running','completed','failed')),
  row_count INTEGER NOT NULL DEFAULT 0,
  byte_count INTEGER NOT NULL DEFAULT 0,
  checksum TEXT NOT NULL DEFAULT '',
  data_path TEXT NOT NULL DEFAULT '',
  review_path TEXT NOT NULL DEFAULT '',
  quality_json TEXT NOT NULL DEFAULT '{}',
  truncated INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0,1)),
  error_code TEXT NOT NULL DEFAULT '',
  knowledge_base_id INTEGER REFERENCES knowledge_bases(id) ON DELETE SET NULL,
  knowledge_document_id INTEGER REFERENCES knowledge_documents(id) ON DELETE SET NULL,
  confirmed_at TEXT NOT NULL DEFAULT '',
  collected_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_data_link_sources_updated ON data_link_sources(updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_data_link_snapshots_source ON data_link_snapshots(source_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_data_link_snapshots_status ON data_link_snapshots(status, id DESC);
