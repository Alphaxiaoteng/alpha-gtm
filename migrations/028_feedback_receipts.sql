CREATE TABLE IF NOT EXISTS feedback_receipts (
  feedback_id TEXT PRIMARY KEY,
  read_token TEXT NOT NULL,
  content TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT '待处理' CHECK (status IN ('待处理','处理中','已完成','不采纳')),
  category TEXT NOT NULL DEFAULT '未分类',
  developer_reply TEXT NOT NULL DEFAULT '',
  remote_created_at TEXT NOT NULL DEFAULT '',
  remote_updated_at TEXT NOT NULL DEFAULT '',
  last_synced_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_feedback_receipts_created ON feedback_receipts(created_at DESC);
