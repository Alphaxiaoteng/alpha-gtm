CREATE TABLE IF NOT EXISTS skills (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL CHECK (source IN ('system','local')),
  status TEXT NOT NULL CHECK (status IN ('reviewed','ready','review_required')),
  runtime TEXT NOT NULL CHECK (runtime IN ('instructions','registry')),
  runtime_ref TEXT NOT NULL DEFAULT '',
  directory TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 0,
  has_scripts INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_skills_enabled ON skills(enabled, status, id);
