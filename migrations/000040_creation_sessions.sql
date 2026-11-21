CREATE TABLE IF NOT EXISTS creation_model_roles (
  role TEXT PRIMARY KEY CHECK (role IN ('conversation','copywriter','reviewer')),
  provider_id TEXT NOT NULL,
  model_id TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS creation_sessions (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  topic_brief TEXT NOT NULL DEFAULT '',
  style TEXT NOT NULL DEFAULT '',
  platform TEXT NOT NULL DEFAULT '',
  sop_stage TEXT NOT NULL DEFAULT 'topic' CHECK (sop_stage IN ('topic','evidence','copy','script','review','save')),
  knowledge_base_ids_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS creation_runs (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES creation_sessions(id) ON DELETE CASCADE,
  action TEXT NOT NULL CHECK (action IN ('topic','draft','review','revise')),
  provider_id TEXT NOT NULL DEFAULT '',
  model_id TEXT NOT NULL DEFAULT '',
  used_fallback INTEGER NOT NULL DEFAULT 0 CHECK (used_fallback IN (0,1)),
  prompt_summary TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL,
  citations_json TEXT NOT NULL DEFAULT '{}',
  quality_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_creation_runs_session ON creation_runs(session_id, created_at, id);
