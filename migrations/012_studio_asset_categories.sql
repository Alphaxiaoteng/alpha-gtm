-- Independent local material/script categories. Existing video files are
-- backfilled as video; other legacy assets remain shared until classified.
PRAGMA foreign_keys = ON;

ALTER TABLE media_library ADD COLUMN purpose TEXT NOT NULL DEFAULT 'shared'
  CHECK (purpose IN ('shared','video','graph_text','article'));
ALTER TABLE studio_scripts ADD COLUMN content_type TEXT NOT NULL DEFAULT 'video'
  CHECK (content_type IN ('video','graph_text','article'));

UPDATE media_library
SET purpose = 'video'
WHERE purpose = 'shared' AND lower(COALESCE(mime, '')) LIKE 'video/%';

CREATE INDEX IF NOT EXISTS idx_media_library_purpose ON media_library(purpose, id DESC);
CREATE INDEX IF NOT EXISTS idx_studio_scripts_type ON studio_scripts(content_type, updated_at DESC, id DESC);
