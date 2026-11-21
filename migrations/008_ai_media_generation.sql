-- Durable state for explicit image/video generation requests. Credentials and
-- remote/download URLs are deliberately excluded from this local table.
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS generation_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id TEXT NOT NULL DEFAULT '',
  request_hash TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL CHECK (kind IN ('image', 'video')),
  mode TEXT NOT NULL CHECK (mode IN ('text_to_image', 'image_to_image', 'text_to_video', 'image_to_video')),
  provider TEXT NOT NULL,
  model TEXT NOT NULL,
  prompt TEXT NOT NULL,
  prompt_hash TEXT NOT NULL CHECK (length(prompt_hash) = 64),
  references_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
  external_task_id TEXT NOT NULL DEFAULT '',
  external_video_id TEXT NOT NULL DEFAULT '',
  output_media_id INTEGER,
  progress INTEGER NOT NULL DEFAULT 0 CHECK (progress >= 0 AND progress <= 100),
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  started_at TEXT,
  completed_at TEXT,
  FOREIGN KEY (output_media_id) REFERENCES media_library(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_generation_jobs_recoverable
  ON generation_jobs(status, created_at, id);
CREATE INDEX IF NOT EXISTS idx_generation_jobs_recent
  ON generation_jobs(created_at DESC, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_generation_jobs_request_id
  ON generation_jobs(request_id) WHERE request_id <> '';

-- Upgrade hygiene for pre-release rows that carried a provider handoff URL in
-- references_json. Provider URLs are transient and must not survive locally.
UPDATE generation_jobs
SET references_json = (
  SELECT COALESCE(json_group_array(json_remove(value, '$.public_url')), '[]')
  FROM json_each(generation_jobs.references_json)
)
WHERE json_valid(references_json)
  AND json_type(references_json) = 'array'
  AND instr(references_json, '"public_url"') > 0;
