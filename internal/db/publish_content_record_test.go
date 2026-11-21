package db_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScheduledPublishContentRecordIsSeparateFromPublishedRecords(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, _, _, _ := createFrozenLinkedJob(t, database, "content-record", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "content-record"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	evidenceDir := filepath.Join(database.DataDir, "screenshots", "publish")
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	evidencePath := filepath.Join(evidenceDir, "scheduled.png")
	if err := os.WriteFile(evidencePath, []byte("scheduled"), 0o600); err != nil {
		t.Fatal(err)
	}
	record, err := database.RecordScheduledPublishContent(job.ID, "2026-07-31T19:26:00+08:00", evidencePath)
	if err != nil {
		t.Fatal(err)
	}
	if record.RemoteState != "scheduled" || record.Title != "frozen title" || record.JobID != job.ID {
		t.Fatalf("record=%+v", record)
	}
	records, err := database.ListPublishContentRecords(10)
	if err != nil || len(records) != 1 || records[0].ID != record.ID {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	var published int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&published); err != nil || published != 0 {
		t.Fatalf("published=%d err=%v", published, err)
	}
}

func TestPublishContentRecordDeletionKeepsTheAuditCard(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, _, _, _ := createFrozenLinkedJob(t, database, "content-delete", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "content-delete"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	evidenceDir := filepath.Join(database.DataDir, "screenshots", "publish")
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	scheduledPath := filepath.Join(evidenceDir, "scheduled.png")
	deletedPath := filepath.Join(evidenceDir, "deleted.png")
	if err := os.WriteFile(scheduledPath, []byte("scheduled"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deletedPath, []byte("deleted"), 0o600); err != nil {
		t.Fatal(err)
	}
	record, err := database.RecordScheduledPublishContent(job.ID, time.Now().Add(24*time.Hour).Format(time.RFC3339), scheduledPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MarkPublishContentDeleted(record.ID, deletedPath); err != nil {
		t.Fatal(err)
	}
	records, err := database.ListPublishContentRecords(10)
	if err != nil || len(records) != 1 || records[0].RemoteState != "deleted" || records[0].DeletedAt == "" {
		t.Fatalf("records=%+v err=%v", records, err)
	}
}

func TestPublishContentRecordDeleteFailureIsVisibleAndRetryable(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, _, _, _ := createFrozenLinkedJob(t, database, "content-delete-failed", "submit")
	if claimed, err := database.ClaimPublishJob(job.ID, "content-delete-failed"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if err := database.MarkPublishSubmitAttempt(job.ID); err != nil {
		t.Fatal(err)
	}
	evidenceDir := filepath.Join(database.DataDir, "screenshots", "publish")
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	scheduledPath := filepath.Join(evidenceDir, "scheduled.png")
	if err := os.WriteFile(scheduledPath, []byte("scheduled"), 0o600); err != nil {
		t.Fatal(err)
	}
	record, err := database.RecordScheduledPublishContent(job.ID, time.Now().Add(24*time.Hour).Format(time.RFC3339), scheduledPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MarkPublishContentDeletePending(record.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.MarkPublishContentDeleteFailed(record.ID, "PUBLISH_CONTENT_DELETE_TARGET_UNVERIFIED"); err != nil {
		t.Fatal(err)
	}
	got, err := database.GetPublishContentRecord(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RemoteState != "delete_failed" || got.ErrorCode != "PUBLISH_CONTENT_DELETE_TARGET_UNVERIFIED" {
		t.Fatalf("record=%+v", got)
	}
}
