ALTER TABLE ego_spaces RENAME TO ego_spaces_account_owned;

CREATE TABLE ego_spaces (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  identity_id INTEGER NOT NULL UNIQUE,
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
  FOREIGN KEY (identity_id) REFERENCES ego_identities(id) ON DELETE CASCADE,
  FOREIGN KEY (active_job_id) REFERENCES publish_jobs(id) ON DELETE SET NULL
);

INSERT INTO ego_spaces (
  id, identity_id, space_id, task_id, name, ownership, state, control_epoch,
  active_job_id, last_seen_at, error_code, created_at, updated_at
)
SELECT legacy.id, accounts.identity_id, legacy.space_id, legacy.task_id, legacy.name,
  legacy.ownership, legacy.state, legacy.control_epoch, legacy.active_job_id,
  legacy.last_seen_at, legacy.error_code, legacy.created_at, legacy.updated_at
FROM ego_spaces_account_owned legacy
JOIN accounts ON accounts.id=legacy.account_id
WHERE legacy.id=(
  SELECT MIN(candidate.id)
  FROM ego_spaces_account_owned candidate
  JOIN accounts candidate_account ON candidate_account.id=candidate.account_id
  WHERE candidate_account.identity_id=accounts.identity_id
);

DROP TABLE ego_spaces_account_owned;

CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_identity_platform
ON accounts(identity_id, platform_code);

CREATE INDEX IF NOT EXISTS idx_ego_spaces_state
ON ego_spaces(state, id);
