CREATE TABLE IF NOT EXISTS content_variants (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL,
  account_id INTEGER,
  title TEXT NOT NULL DEFAULT '',
  caption TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  tags_json TEXT NOT NULL DEFAULT '[]',
  revision INTEGER NOT NULL DEFAULT 1,
  source TEXT NOT NULL DEFAULT 'manual',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (content_id) REFERENCES contents(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code),
  FOREIGN KEY (account_id) REFERENCES accounts(id)
);

CREATE TABLE IF NOT EXISTS publish_plans (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content_id INTEGER NOT NULL,
  media_mode TEXT NOT NULL,
  snapshot_json TEXT NOT NULL,
  snapshot_hash TEXT NOT NULL,
  status TEXT NOT NULL,
  concurrency INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  frozen_at TEXT NOT NULL,
  approved_at TEXT,
  approved_action TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  FOREIGN KEY (content_id) REFERENCES contents(id)
);

CREATE TABLE IF NOT EXISTS publish_plan_targets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  asset_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL,
  variant_id INTEGER,
  settings_json TEXT NOT NULL DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'pending',
  publish_job_id INTEGER,
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (plan_id) REFERENCES publish_plans(id),
  FOREIGN KEY (account_id) REFERENCES accounts(id),
  FOREIGN KEY (asset_id) REFERENCES account_assets(id),
  FOREIGN KEY (platform_code) REFERENCES platforms(code),
  FOREIGN KEY (variant_id) REFERENCES content_variants(id),
  FOREIGN KEY (publish_job_id) REFERENCES publish_jobs(id),
  UNIQUE (plan_id, asset_id)
);

CREATE TABLE IF NOT EXISTS approvals (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  skill_version TEXT NOT NULL DEFAULT '',
  tool_name TEXT NOT NULL,
  args_hash TEXT NOT NULL,
  capability TEXT NOT NULL,
  target_resource TEXT NOT NULL,
  cost_limit INTEGER NOT NULL DEFAULT 0,
  expires_at TEXT NOT NULL,
  nonce_hash TEXT NOT NULL,
  single_use INTEGER NOT NULL DEFAULT 1,
  consumed_at TEXT,
  created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_content_variants_platform_default ON content_variants(content_id, platform_code) WHERE account_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_content_variants_platform_account ON content_variants(content_id, platform_code, account_id) WHERE account_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_publish_plan_targets_plan ON publish_plan_targets(plan_id, id);
CREATE INDEX IF NOT EXISTS idx_approvals_expiry ON approvals(expires_at, consumed_at);
