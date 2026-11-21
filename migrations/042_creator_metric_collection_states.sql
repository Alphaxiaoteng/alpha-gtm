CREATE TABLE IF NOT EXISTS creator_metric_collection_states (
  account_id INTEGER PRIMARY KEY,
  platform_code TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('observed','skipped','unknown')),
  error_code TEXT NOT NULL DEFAULT '',
  evidence_code TEXT NOT NULL DEFAULT '',
  range_verified INTEGER NOT NULL DEFAULT 0 CHECK (range_verified IN (0,1)),
  post_count INTEGER NOT NULL DEFAULT 0 CHECK (post_count >= 0),
  collected_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);

CREATE INDEX IF NOT EXISTS idx_creator_metric_collection_states_platform
  ON creator_metric_collection_states(platform_code, collected_at DESC);

INSERT OR IGNORE INTO creator_metric_collection_states
  (account_id,platform_code,state,error_code,evidence_code,range_verified,post_count,collected_at,updated_at)
SELECT snapshot.account_id,snapshot.platform_code,'observed','',snapshot.evidence_code,
  snapshot.range_verified,snapshot.posts,snapshot.collected_at,strftime('%Y-%m-%dT%H:%M:%fZ','now')
FROM creator_metric_snapshots snapshot
JOIN (
  SELECT account_id,MAX(id) AS id
  FROM creator_metric_snapshots
  WHERE range_verified=1
  GROUP BY account_id
) latest ON latest.id=snapshot.id;
