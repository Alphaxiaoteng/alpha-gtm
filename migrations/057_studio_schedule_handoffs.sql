ALTER TABLE contents ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS studio_schedule_handoffs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workflow_id TEXT NOT NULL REFERENCES creation_workflows(id) ON DELETE RESTRICT,
  content_id INTEGER NOT NULL REFERENCES contents(id) ON DELETE RESTRICT,
  script_id INTEGER REFERENCES studio_scripts(id) ON DELETE RESTRICT,
  revision INTEGER NOT NULL,
  media_mode TEXT NOT NULL CHECK (media_mode IN ('article','graph_text','video')),
  media_ids_json TEXT NOT NULL,
  cover_media_id INTEGER,
  native_video_cover INTEGER NOT NULL DEFAULT 0 CHECK (native_video_cover IN (0,1)),
  canonical_hash TEXT NOT NULL,
  planned_at_utc TEXT NOT NULL,
  timezone TEXT NOT NULL,
  original_local_time TEXT NOT NULL,
  intent_hash TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('ready','frozen','superseded','cancelled','expired')),
  frozen_plan_id INTEGER REFERENCES publish_plans(id) ON DELETE RESTRICT,
  snapshot_hash TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

ALTER TABLE publish_plans ADD COLUMN studio_handoff_id INTEGER REFERENCES studio_schedule_handoffs(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_studio_schedule_handoffs_workflow ON studio_schedule_handoffs(workflow_id, updated_at DESC, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_studio_schedule_handoffs_ready_workflow ON studio_schedule_handoffs(workflow_id) WHERE status='ready';
CREATE UNIQUE INDEX IF NOT EXISTS idx_publish_plans_studio_handoff ON publish_plans(studio_handoff_id) WHERE studio_handoff_id IS NOT NULL;
