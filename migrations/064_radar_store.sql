-- Trend Radar persistence: the collected work stops living only inside the
-- raw provider JSON. Five tables make one work traceable back to the pool, the
-- search term, the paid call and the day it was first seen.

-- The pool vocabulary is snapshotted so attribution is a join, never a string
-- match over a config file that keeps changing.
CREATE TABLE IF NOT EXISTS radar_pool_terms (
	pool_id TEXT NOT NULL,
	pool_name TEXT NOT NULL DEFAULT '',
	term TEXT NOT NULL,
	normalized TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (pool_id, normalized)
);

-- One explicit collection round for one pool and one platform.
CREATE TABLE IF NOT EXISTS radar_capture_runs (
	id TEXT PRIMARY KEY,
	pool_id TEXT NOT NULL DEFAULT '',
	pool_name TEXT NOT NULL DEFAULT '',
	platform TEXT NOT NULL DEFAULT '',
	endpoint_id TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT '',
	started_at TEXT NOT NULL DEFAULT '',
	finished_at TEXT NOT NULL DEFAULT '',
	last_retry_at TEXT NOT NULL DEFAULT '',
	terms_json TEXT NOT NULL DEFAULT '[]',
	item_cap INTEGER NOT NULL DEFAULT 0,
	max_calls INTEGER NOT NULL DEFAULT 0,
	estimated_cost_usd REAL NOT NULL DEFAULT 0,
	cost_basis TEXT NOT NULL DEFAULT '',
	total_calls INTEGER NOT NULL DEFAULT 0,
	succeeded INTEGER NOT NULL DEFAULT 0,
	failed INTEGER NOT NULL DEFAULT 0,
	skipped INTEGER NOT NULL DEFAULT 0,
	kept INTEGER NOT NULL DEFAULT 0,
	items INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);

-- One paid provider call, including what it cost and why it failed.
CREATE TABLE IF NOT EXISTS radar_capture_calls (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	run_id TEXT NOT NULL DEFAULT '',
	call_id INTEGER NOT NULL DEFAULT 0,
	term TEXT NOT NULL DEFAULT '',
	platform TEXT NOT NULL DEFAULT '',
	endpoint_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	error_code TEXT NOT NULL DEFAULT '',
	error_message TEXT NOT NULL DEFAULT '',
	item_count INTEGER NOT NULL DEFAULT 0,
	kept_count INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	raw_path TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);

-- One collected work, deduplicated by platform + provider work id.
CREATE TABLE IF NOT EXISTS radar_works (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	platform TEXT NOT NULL,
	work_id TEXT NOT NULL,
	pool_id TEXT NOT NULL DEFAULT '',
	pool_name TEXT NOT NULL DEFAULT '',
	term TEXT NOT NULL DEFAULT '',
	author TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	url TEXT NOT NULL DEFAULT '',
	first_seen_at TEXT NOT NULL,
	last_seen_at TEXT NOT NULL,
	seen_count INTEGER NOT NULL DEFAULT 1,
	UNIQUE(platform, work_id)
);

-- One metric snapshot per work per paid call: "抓了几次""哪天涨了多少"都在这。
CREATE TABLE IF NOT EXISTS radar_observations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	work_pk INTEGER NOT NULL REFERENCES radar_works(id) ON DELETE CASCADE,
	platform TEXT NOT NULL DEFAULT '',
	work_id TEXT NOT NULL DEFAULT '',
	pool_id TEXT NOT NULL DEFAULT '',
	pool_name TEXT NOT NULL DEFAULT '',
	term TEXT NOT NULL DEFAULT '',
	call_id INTEGER NOT NULL DEFAULT 0,
	run_id TEXT NOT NULL DEFAULT '',
	play INTEGER NOT NULL DEFAULT 0,
	likes INTEGER NOT NULL DEFAULT 0,
	collects INTEGER NOT NULL DEFAULT 0,
	shares INTEGER NOT NULL DEFAULT 0,
	comments INTEGER NOT NULL DEFAULT 0,
	observed_at TEXT NOT NULL,
	UNIQUE(work_pk, call_id, observed_at)
);

CREATE INDEX IF NOT EXISTS idx_radar_observations_work ON radar_observations(work_pk, observed_at);
CREATE INDEX IF NOT EXISTS idx_radar_observations_term ON radar_observations(pool_id, term);
CREATE INDEX IF NOT EXISTS idx_radar_calls_run ON radar_capture_calls(run_id);
CREATE INDEX IF NOT EXISTS idx_radar_works_pool ON radar_works(pool_id, first_seen_at);
