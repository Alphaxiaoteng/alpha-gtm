-- Preserve data-workflow outcomes for platforms that do not yet have a
-- reviewed automatic seven-day extractor. These rows are not metric snapshots.
ALTER TABLE creator_metric_collection_states RENAME TO creator_metric_collection_states_legacy;

CREATE TABLE creator_metric_collection_states (
  account_id INTEGER PRIMARY KEY,
  platform_code TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('observed','skipped','unknown','manual_takeover')),
  error_code TEXT NOT NULL DEFAULT '',
  evidence_code TEXT NOT NULL DEFAULT '',
  range_verified INTEGER NOT NULL DEFAULT 0,
  post_count INTEGER NOT NULL DEFAULT 0,
  collected_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);

INSERT INTO creator_metric_collection_states
  (account_id,platform_code,state,error_code,evidence_code,range_verified,post_count,collected_at,updated_at)
SELECT account_id,platform_code,state,error_code,evidence_code,range_verified,post_count,collected_at,updated_at
FROM creator_metric_collection_states_legacy;

DROP TABLE creator_metric_collection_states_legacy;

CREATE INDEX IF NOT EXISTS idx_creator_metric_collection_states_platform
  ON creator_metric_collection_states(platform_code, collected_at DESC);
