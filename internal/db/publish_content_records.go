package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type PublishContentRecord struct {
	ID                 int64  `json:"id"`
	JobID              int64  `json:"job_id"`
	PlanID             int64  `json:"plan_id"`
	PlanTargetID       int64  `json:"plan_target_id"`
	ContentID          int64  `json:"content_id"`
	AccountID          int64  `json:"account_id"`
	AssetID            int64  `json:"asset_id"`
	PlatformCode       string `json:"platform_code"`
	ContentType        string `json:"content_type"`
	AccountName        string `json:"account_name"`
	Title              string `json:"title"`
	CoverPath          string `json:"cover_path"`
	RemoteState        string `json:"remote_state"`
	ScheduledAt        string `json:"scheduled_at"`
	PostURL            string `json:"post_url"`
	ExternalPostID     string `json:"external_post_id"`
	EvidencePath       string `json:"evidence_path"`
	DeleteEvidencePath string `json:"delete_evidence_path"`
	ErrorCode          string `json:"error_code"`
	VerifiedAt         string `json:"verified_at"`
	DeletedAt          string `json:"deleted_at"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

type publishContentSnapshot struct {
	Media []struct {
		FilePath string `json:"file_path"`
		Kind     string `json:"kind"`
		Role     string `json:"role"`
	} `json:"media"`
	Targets []struct {
		AccountID    int64  `json:"account_id"`
		PlatformCode string `json:"platform_code"`
		Title        string `json:"title"`
		Caption      string `json:"caption"`
		Body         string `json:"body"`
	} `json:"targets"`
}

func firstNonEmptyPublishLine(values ...string) string {
	for _, value := range values {
		for _, line := range strings.Split(value, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
	}
	return ""
}

func publishContentPresentation(snapshotJSON, fallbackTitle string, accountID int64, platformCode string) (string, string) {
	title := strings.TrimSpace(fallbackTitle)
	cover := ""
	var snapshot publishContentSnapshot
	if json.Unmarshal([]byte(snapshotJSON), &snapshot) != nil {
		return title, cover
	}
	for _, target := range snapshot.Targets {
		if target.AccountID == accountID && target.PlatformCode == platformCode {
			if platformCode == "tiktok" || platformCode == "kuaishou" || platformCode == "wechat_video" || platformCode == "facebook" {
				title = firstNonEmptyPublishLine(target.Caption, target.Body, target.Title, title)
			} else if strings.TrimSpace(target.Title) != "" {
				title = strings.TrimSpace(target.Title)
			}
			break
		}
	}
	for _, media := range snapshot.Media {
		if media.Role == "cover" && media.Kind == "image" && strings.TrimSpace(media.FilePath) != "" {
			return title, strings.TrimSpace(media.FilePath)
		}
	}
	for _, media := range snapshot.Media {
		if media.Kind == "image" && strings.TrimSpace(media.FilePath) != "" {
			return title, strings.TrimSpace(media.FilePath)
		}
	}
	return title, cover
}

func upsertPublishedPublishContentRecord(
	tx *sql.Tx,
	job PublishJob,
	postURL, externalPostID, evidencePath, verifiedAt string,
) error {
	var accountName, fallbackTitle, snapshotJSON string
	if err := tx.QueryRow(`SELECT a.name, c.title,
			COALESCE((SELECT snapshot_json FROM publish_plans WHERE id=?),'')
		FROM accounts AS a, contents AS c
		WHERE a.id=? AND c.id=?`, job.PlanID, job.AccountID, job.ContentID).
		Scan(&accountName, &fallbackTitle, &snapshotJSON); err != nil {
		return err
	}
	title, coverPath := publishContentPresentation(snapshotJSON, fallbackTitle, job.AccountID, job.PlatformCode)
	_, err := tx.Exec(`INSERT INTO publish_content_records
		(job_id, plan_id, plan_target_id, content_id, account_id, asset_id, platform_code,
		 content_type, account_name, title, cover_path, remote_state, post_url,
		 external_post_id, evidence_path, verified_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'published', ?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET
		  remote_state='published',
		  post_url=excluded.post_url,
		  external_post_id=excluded.external_post_id,
		  evidence_path=excluded.evidence_path,
		  error_code='',
		  verified_at=excluded.verified_at,
		  updated_at=excluded.updated_at
		WHERE publish_content_records.remote_state IN ('scheduled','published','delete_failed')`,
		job.ID, job.PlanID, job.PlanTargetID, job.ContentID, job.AccountID, job.AssetID,
		job.PlatformCode, job.ContentType, accountName, title, coverPath, postURL,
		externalPostID, evidencePath, verifiedAt, verifiedAt, verifiedAt)
	return err
}

func (db *DB) RecordScheduledPublishContent(jobID int64, scheduledAt, evidencePath string) (*PublishContentRecord, error) {
	if jobID <= 0 {
		return nil, fmt.Errorf("publish job id is required")
	}
	scheduledAt = strings.TrimSpace(scheduledAt)
	if _, err := time.Parse(time.RFC3339, scheduledAt); err != nil {
		return nil, fmt.Errorf("scheduled_at must be RFC3339")
	}
	evidencePath, _, err := db.validateEvidenceScreenshot(evidencePath)
	if err != nil {
		return nil, err
	}
	job, err := db.GetPublishJob(jobID)
	if err != nil {
		return nil, err
	}
	if job.SubmitAttemptedAt == "" || job.PlanID <= 0 || job.PlanTargetID <= 0 {
		return nil, fmt.Errorf("%w: scheduled record requires one frozen submit attempt", ErrPublishJobConflict)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	record, err := recordScheduledPublishContentWithTx(tx, *job, scheduledAt, evidencePath, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return record, nil
}

// recordScheduledPublishContentWithTx stores the creator-manager schedule card
// in the caller's transaction, so a verified schedule can be committed with
// the immutable submit marker and all plan/job status changes together.
func recordScheduledPublishContentWithTx(tx *sql.Tx, job PublishJob, scheduledAt, evidencePath, now string) (*PublishContentRecord, error) {
	var record PublishContentRecord
	var fallbackTitle, snapshotJSON string
	if err := tx.QueryRow(`SELECT a.name, c.title,
			COALESCE((SELECT snapshot_json FROM publish_plans WHERE id=?),'')
		FROM accounts AS a, contents AS c
		WHERE a.id=? AND c.id=?`, job.PlanID, job.AccountID, job.ContentID).
		Scan(&record.AccountName, &fallbackTitle, &snapshotJSON); err != nil {
		return nil, err
	}
	record.JobID = job.ID
	record.PlanID = job.PlanID
	record.PlanTargetID = job.PlanTargetID
	record.ContentID = job.ContentID
	record.AccountID = job.AccountID
	record.AssetID = job.AssetID
	record.PlatformCode = job.PlatformCode
	record.ContentType = job.ContentType
	record.Title, record.CoverPath = publishContentPresentation(snapshotJSON, fallbackTitle, record.AccountID, record.PlatformCode)
	_, err := tx.Exec(`INSERT INTO publish_content_records
		(job_id, plan_id, plan_target_id, content_id, account_id, asset_id, platform_code,
		 content_type, account_name, title, cover_path, remote_state, scheduled_at,
		 evidence_path, verified_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'scheduled', ?, ?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET
		  evidence_path=excluded.evidence_path,
		  scheduled_at=excluded.scheduled_at,
		  verified_at=excluded.verified_at,
		  updated_at=excluded.updated_at
		WHERE publish_content_records.remote_state='scheduled'`,
		record.JobID, record.PlanID, record.PlanTargetID, record.ContentID, record.AccountID,
		record.AssetID, record.PlatformCode, record.ContentType, record.AccountName, record.Title,
		record.CoverPath, scheduledAt, evidencePath, now, now, now)
	if err != nil {
		return nil, err
	}
	return scanPublishContentRecord(tx.QueryRow(publishContentRecordSelect+` WHERE job_id=?`, job.ID))
}

func (db *DB) MarkPublishContentDeleted(id int64, evidencePath string) error {
	if id <= 0 {
		return fmt.Errorf("publish content record id is required")
	}
	evidencePath, _, err := db.validateEvidenceScreenshot(evidencePath)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`UPDATE publish_content_records
		SET remote_state='deleted', delete_evidence_path=?, error_code='',
		    deleted_at=?, updated_at=?
		WHERE id=? AND remote_state IN ('scheduled','published','delete_pending','delete_failed')`,
		evidencePath, now, now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("publish content record is not deletable")
	}
	return nil
}

func (db *DB) MarkPublishContentDeletePending(id int64) error {
	if id <= 0 {
		return fmt.Errorf("publish content record id is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`UPDATE publish_content_records
		SET remote_state='delete_pending', error_code='', updated_at=?
		WHERE id=? AND remote_state IN ('scheduled','published','delete_failed')`, now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("publish content record is not deletable")
	}
	return nil
}

func (db *DB) MarkPublishContentDeleteFailed(id int64, errorCode string) error {
	if id <= 0 {
		return fmt.Errorf("publish content record id is required")
	}
	errorCode = strings.TrimSpace(errorCode)
	if errorCode == "" {
		errorCode = "PUBLISH_CONTENT_DELETE_FAILED"
	}
	if len(errorCode) > 128 {
		errorCode = errorCode[:128]
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`UPDATE publish_content_records
		SET remote_state='delete_failed', error_code=?, updated_at=?
		WHERE id=? AND remote_state='delete_pending'`, errorCode, now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("publish content record delete is not pending")
	}
	return nil
}

func (db *DB) getPublishContentRecordByJob(jobID int64) (*PublishContentRecord, error) {
	return scanPublishContentRecord(db.QueryRow(publishContentRecordSelect+` WHERE job_id=?`, jobID))
}

func (db *DB) GetPublishContentRecord(id int64) (*PublishContentRecord, error) {
	if id <= 0 {
		return nil, fmt.Errorf("publish content record id is required")
	}
	return scanPublishContentRecord(db.QueryRow(publishContentRecordSelect+` WHERE id=?`, id))
}

const publishContentRecordSelect = `SELECT id, job_id, plan_id, plan_target_id, content_id,
	account_id, asset_id, platform_code, content_type, account_name, title, cover_path,
	remote_state, scheduled_at, post_url, external_post_id, evidence_path,
	delete_evidence_path, error_code, verified_at, deleted_at, created_at, updated_at
	FROM publish_content_records`

type rowScanner interface {
	Scan(...any) error
}

func scanPublishContentRecord(row rowScanner) (*PublishContentRecord, error) {
	var record PublishContentRecord
	err := row.Scan(&record.ID, &record.JobID, &record.PlanID, &record.PlanTargetID,
		&record.ContentID, &record.AccountID, &record.AssetID, &record.PlatformCode,
		&record.ContentType, &record.AccountName, &record.Title, &record.CoverPath,
		&record.RemoteState, &record.ScheduledAt, &record.PostURL, &record.ExternalPostID,
		&record.EvidencePath, &record.DeleteEvidencePath, &record.ErrorCode,
		&record.VerifiedAt, &record.DeletedAt, &record.CreatedAt, &record.UpdatedAt)
	return &record, err
}

func (db *DB) ListPublishContentRecords(limit int) ([]PublishContentRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(publishContentRecordSelect+` ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]PublishContentRecord, 0)
	for rows.Next() {
		record, err := scanPublishContentRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}
	return records, rows.Err()
}
