CREATE TABLE IF NOT EXISTS publish_content_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id INTEGER NOT NULL,
  plan_id INTEGER NOT NULL,
  plan_target_id INTEGER NOT NULL,
  content_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  asset_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL,
  content_type TEXT NOT NULL,
  account_name TEXT NOT NULL,
  title TEXT NOT NULL,
  cover_path TEXT NOT NULL DEFAULT '',
  remote_state TEXT NOT NULL CHECK (remote_state IN ('scheduled','published','delete_pending','delete_failed','deleted')),
  scheduled_at TEXT NOT NULL DEFAULT '',
  post_url TEXT NOT NULL DEFAULT '',
  external_post_id TEXT NOT NULL DEFAULT '',
  evidence_path TEXT NOT NULL DEFAULT '',
  delete_evidence_path TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  verified_at TEXT NOT NULL,
  deleted_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (job_id) REFERENCES publish_jobs(id),
  FOREIGN KEY (content_id) REFERENCES contents(id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_publish_content_records_job ON publish_content_records(job_id);
CREATE INDEX IF NOT EXISTS idx_publish_content_records_state
  ON publish_content_records(remote_state, updated_at DESC, id DESC);
