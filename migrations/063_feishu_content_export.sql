-- Feishu content library: one binding row plus the record-id map that keeps
-- repeated exports updating the same Base rows instead of duplicating them.

CREATE TABLE IF NOT EXISTS feishu_content_export (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
	base_token TEXT NOT NULL DEFAULT '',
	base_url TEXT NOT NULL DEFAULT '',
	post_table_id TEXT NOT NULL DEFAULT '',
	metrics_table_id TEXT NOT NULL DEFAULT '',
	last_export_at TEXT NOT NULL DEFAULT '',
	last_result_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS feishu_content_record_links (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	table_id TEXT NOT NULL,
	business_key TEXT NOT NULL,
	record_id TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(table_id, business_key)
);
