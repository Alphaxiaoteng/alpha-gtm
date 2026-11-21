ALTER TABLE radar_works ADD COLUMN published_at TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_radar_works_published ON radar_works(published_at);
