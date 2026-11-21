-- 本地素材、脚本与剪辑任务。没有远程存储、队列或定时器。
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS studio_scripts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS clip_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_media_id INTEGER NOT NULL,
  script_id INTEGER,
  source_path TEXT NOT NULL,
  output_path TEXT,
  start_ms INTEGER NOT NULL,
  end_ms INTEGER NOT NULL,
  plan_json TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'planned',
  error_code TEXT,
  error_message TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT,
  FOREIGN KEY (source_media_id) REFERENCES media_library(id),
  FOREIGN KEY (script_id) REFERENCES studio_scripts(id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_clip_jobs_created ON clip_jobs(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_clip_jobs_source_media ON clip_jobs(source_media_id, id);
