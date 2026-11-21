-- Safe browser lifecycle diagnostics. Raw browser messages remain private;
-- local UI/API consumers receive only these structured fields.
ALTER TABLE browser_events ADD COLUMN reason_code TEXT NOT NULL DEFAULT '';
ALTER TABLE browser_events ADD COLUMN exit_code INTEGER;
ALTER TABLE browser_events ADD COLUMN term_signal INTEGER;
