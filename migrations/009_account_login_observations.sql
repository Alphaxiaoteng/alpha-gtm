CREATE TABLE IF NOT EXISTS account_login_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  auth_state TEXT NOT NULL CHECK (auth_state IN ('logged_in', 'logged_out', 'unknown')),
  persisted_login_state TEXT NOT NULL CHECK (persisted_login_state IN ('logged_in', 'logged_out', 'unknown')),
  browser_state TEXT NOT NULL,
  login_mode TEXT NOT NULL DEFAULT 'unknown',
  auth_method TEXT NOT NULL DEFAULT 'unknown',
  reason_code TEXT NOT NULL,
  evidence_code TEXT NOT NULL,
  page_origin TEXT NOT NULL DEFAULT '',
  profile_name TEXT NOT NULL DEFAULT '',
  account_identifier TEXT NOT NULL DEFAULT '',
  avatar_source TEXT NOT NULL DEFAULT 'platform_fallback',
  avatar_sha256 TEXT NOT NULL DEFAULT '',
  avatar_error_code TEXT NOT NULL DEFAULT '',
  profile_changed INTEGER NOT NULL DEFAULT 0,
  trace_id TEXT NOT NULL,
  observed_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_account_login_events_account_observed
  ON account_login_events(account_id, observed_at DESC, id DESC);

ALTER TABLE accounts ADD COLUMN avatar_source TEXT NOT NULL DEFAULT 'platform_fallback';
ALTER TABLE accounts ADD COLUMN avatar_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN avatar_refreshed_at TEXT NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN login_mode TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE accounts ADD COLUMN auth_method TEXT NOT NULL DEFAULT 'unknown';
