CREATE TABLE IF NOT EXISTS feedback_submission_intents (
  request_hash TEXT PRIMARY KEY,
  client_token TEXT NOT NULL,
  feedback_number TEXT NOT NULL,
  read_token TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
