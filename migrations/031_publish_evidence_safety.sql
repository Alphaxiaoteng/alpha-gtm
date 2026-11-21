ALTER TABLE publish_jobs ADD COLUMN plan_id INTEGER;
ALTER TABLE publish_jobs ADD COLUMN plan_target_id INTEGER;
ALTER TABLE publish_jobs ADD COLUMN snapshot_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE publish_jobs ADD COLUMN submit_attempted_at TEXT NOT NULL DEFAULT '';
ALTER TABLE publish_jobs ADD COLUMN submit_outcome TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_publish_jobs_plan_target
ON publish_jobs(plan_id, plan_target_id);

CREATE TRIGGER IF NOT EXISTS trg_publish_jobs_frozen_link_insert
BEFORE INSERT ON publish_jobs
WHEN (
  (COALESCE(NEW.plan_id,0)=0 OR COALESCE(NEW.plan_target_id,0)=0 OR COALESCE(NEW.snapshot_hash,'')='')
  AND NOT (COALESCE(NEW.plan_id,0)=0 AND COALESCE(NEW.plan_target_id,0)=0 AND COALESCE(NEW.snapshot_hash,'')='')
) OR (
  COALESCE(NEW.plan_id,0)>0 AND NOT EXISTS (
    SELECT 1 FROM publish_plans AS plan
    JOIN publish_plan_targets AS target ON target.plan_id=plan.id
    WHERE plan.id=NEW.plan_id AND target.id=NEW.plan_target_id
      AND plan.content_id=NEW.content_id AND plan.snapshot_hash=NEW.snapshot_hash
      AND target.account_id=NEW.account_id
      AND target.asset_id=COALESCE(NEW.asset_id,0)
      AND target.platform_code=NEW.platform_code
  )
)
BEGIN SELECT RAISE(ABORT, 'publish_jobs frozen snapshot link'); END;

CREATE TRIGGER IF NOT EXISTS trg_publish_jobs_frozen_link_immutable
BEFORE UPDATE OF plan_id, plan_target_id, snapshot_hash ON publish_jobs
WHEN COALESCE(OLD.plan_id,0)<>COALESCE(NEW.plan_id,0)
  OR COALESCE(OLD.plan_target_id,0)<>COALESCE(NEW.plan_target_id,0)
  OR COALESCE(OLD.snapshot_hash,'')<>COALESCE(NEW.snapshot_hash,'')
BEGIN SELECT RAISE(ABORT, 'publish_jobs frozen snapshot link is immutable'); END;

CREATE TRIGGER IF NOT EXISTS trg_publish_jobs_submit_marker_immutable
BEFORE UPDATE OF submit_attempted_at ON publish_jobs
WHEN COALESCE(OLD.submit_attempted_at,'')<>''
  AND COALESCE(NEW.submit_attempted_at,'')<>COALESCE(OLD.submit_attempted_at,'')
  AND NOT (
    OLD.mode='submit' AND NEW.mode='submit'
    AND OLD.status='running' AND NEW.status='manual_takeover'
    AND OLD.current_step='submit_approved' AND NEW.current_step='submit_not_clicked'
    AND OLD.submit_outcome='attempted' AND NEW.submit_outcome='not_clicked'
    AND COALESCE(NEW.submit_attempted_at,'')=''
  )
BEGIN SELECT RAISE(ABORT, 'publish_jobs submit marker is immutable'); END;

CREATE TRIGGER IF NOT EXISTS trg_publish_plans_snapshot_immutable
BEFORE UPDATE OF content_id, media_mode, snapshot_json, snapshot_hash, concurrency ON publish_plans
WHEN OLD.content_id<>NEW.content_id
  OR OLD.media_mode<>NEW.media_mode
  OR OLD.snapshot_json<>NEW.snapshot_json
  OR OLD.snapshot_hash<>NEW.snapshot_hash
  OR OLD.concurrency<>NEW.concurrency
BEGIN SELECT RAISE(ABORT, 'publish plan snapshot is immutable'); END;

CREATE TRIGGER IF NOT EXISTS trg_publish_records_verified_insert
BEFORE INSERT ON publish_records
WHEN NEW.job_id IS NULL
  OR NEW.status<>'published'
  OR NOT EXISTS (
    SELECT 1 FROM publish_jobs
    WHERE id=NEW.job_id AND status='published'
      AND current_step='creator_backend_verified'
      AND submit_outcome='verified_published'
  )
BEGIN SELECT RAISE(ABORT, 'publish record requires creator backend evidence'); END;
