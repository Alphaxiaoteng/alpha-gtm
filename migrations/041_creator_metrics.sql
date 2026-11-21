CREATE TABLE IF NOT EXISTS creator_metric_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL,
  snapshot_date TEXT NOT NULL,
  window_days INTEGER NOT NULL DEFAULT 7 CHECK (window_days BETWEEN 1 AND 31),
  range_verified INTEGER NOT NULL DEFAULT 0 CHECK (range_verified IN (0,1)),
  followers INTEGER NOT NULL DEFAULT 0 CHECK (followers >= 0),
  followers_verified INTEGER NOT NULL DEFAULT 0 CHECK (followers_verified IN (0,1)),
  views INTEGER NOT NULL DEFAULT 0 CHECK (views >= 0),
  likes INTEGER NOT NULL DEFAULT 0 CHECK (likes >= 0),
  comments INTEGER NOT NULL DEFAULT 0 CHECK (comments >= 0),
  shares INTEGER NOT NULL DEFAULT 0 CHECK (shares >= 0),
  favorites INTEGER NOT NULL DEFAULT 0 CHECK (favorites >= 0),
  posts INTEGER NOT NULL DEFAULT 0 CHECK (posts >= 0),
  evidence_code TEXT NOT NULL,
  raw_json TEXT NOT NULL DEFAULT '{}',
  collected_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(account_id, snapshot_date, window_days),
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);

CREATE TABLE IF NOT EXISTS creator_post_metric_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  snapshot_id INTEGER NOT NULL,
  account_id INTEGER NOT NULL,
  platform_code TEXT NOT NULL,
  post_key TEXT NOT NULL,
  external_post_id TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  published_at TEXT NOT NULL DEFAULT '',
  post_url TEXT NOT NULL DEFAULT '',
  views INTEGER NOT NULL DEFAULT 0 CHECK (views >= 0),
  likes INTEGER NOT NULL DEFAULT 0 CHECK (likes >= 0),
  comments INTEGER NOT NULL DEFAULT 0 CHECK (comments >= 0),
  shares INTEGER NOT NULL DEFAULT 0 CHECK (shares >= 0),
  favorites INTEGER NOT NULL DEFAULT 0 CHECK (favorites >= 0),
  raw_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  UNIQUE(snapshot_id, post_key),
  FOREIGN KEY (snapshot_id) REFERENCES creator_metric_snapshots(id) ON DELETE CASCADE,
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
  FOREIGN KEY (platform_code) REFERENCES platforms(code)
);

CREATE INDEX IF NOT EXISTS idx_creator_metric_snapshots_date ON creator_metric_snapshots(snapshot_date DESC, account_id);
CREATE INDEX IF NOT EXISTS idx_creator_metric_snapshots_platform ON creator_metric_snapshots(platform_code, snapshot_date DESC);
CREATE INDEX IF NOT EXISTS idx_creator_post_metric_snapshots_snapshot ON creator_post_metric_snapshots(snapshot_id, views DESC, id);
CREATE INDEX IF NOT EXISTS idx_creator_post_metric_snapshots_account ON creator_post_metric_snapshots(account_id, platform_code, published_at DESC);
