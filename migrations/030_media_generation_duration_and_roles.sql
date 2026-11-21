ALTER TABLE generation_jobs ADD COLUMN duration_seconds INTEGER NOT NULL DEFAULT 0;

UPDATE generation_jobs
SET duration_seconds = 5
WHERE kind = 'video' AND duration_seconds = 0;

ALTER TABLE media_assets ADD COLUMN role TEXT NOT NULL DEFAULT 'content';
ALTER TABLE media_assets ADD COLUMN position INTEGER NOT NULL DEFAULT 0;
