CREATE TABLE IF NOT EXISTS ego_identities (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL COLLATE NOCASE UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO ego_identities (id, name, enabled, sort, created_at, updated_at)
VALUES (1, '默认身份', 1, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

ALTER TABLE accounts ADD COLUMN identity_id INTEGER;
UPDATE accounts SET identity_id=1 WHERE identity_id IS NULL;

CREATE TABLE IF NOT EXISTS ego_spaces (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL UNIQUE,
  space_id INTEGER NOT NULL UNIQUE,
  task_id TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  ownership TEXT NOT NULL DEFAULT 'agent',
  state TEXT NOT NULL DEFAULT 'ready',
  control_epoch INTEGER NOT NULL DEFAULT 1,
  active_job_id INTEGER,
  last_seen_at TEXT NOT NULL DEFAULT '',
  error_code TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
  FOREIGN KEY (active_job_id) REFERENCES publish_jobs(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_accounts_identity ON accounts(identity_id, platform_code, id);
CREATE INDEX IF NOT EXISTS idx_ego_spaces_state ON ego_spaces(state, id);
