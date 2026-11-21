-- Content media is a traceable association to the local media library.
-- Existing rows remain readable with library_id=NULL; every new row must link
-- to a live library item.
PRAGMA foreign_keys = ON;

ALTER TABLE media_assets ADD COLUMN library_id INTEGER
  REFERENCES media_library(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_media_assets_library ON media_assets(library_id);

CREATE TRIGGER IF NOT EXISTS trg_media_assets_library_insert
BEFORE INSERT ON media_assets
WHEN NEW.library_id IS NULL
  OR NOT EXISTS (SELECT 1 FROM media_library WHERE id=NEW.library_id)
BEGIN SELECT RAISE(ABORT, 'media_assets library link required'); END;

CREATE TRIGGER IF NOT EXISTS trg_media_assets_library_update
BEFORE UPDATE OF library_id ON media_assets
WHEN NEW.library_id IS NULL
  OR NOT EXISTS (SELECT 1 FROM media_library WHERE id=NEW.library_id)
BEGIN SELECT RAISE(ABORT, 'media_assets library link required'); END;
