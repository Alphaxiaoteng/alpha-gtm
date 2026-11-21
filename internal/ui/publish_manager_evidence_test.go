package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/publishplan"
)

func TestRecordPublishedManagerInspectionRequiresCompleteYouTubeRowEvidence(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("video", "αgamer｜阿尔法玩家，正式入驻", "", "", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("youtube", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	job, err := database.CreatePublishJob(0, content.ID, account.ID, 0, "youtube", "video")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs
		SET status='awaiting_review', current_step='creator_backend_verification',
		    submit_attempted_at=?, submit_outcome='attempted'
		WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), job.ID); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(database.DataDir, "youtube-public-source.png")
	if err := os.WriteFile(source, []byte("youtube public row screenshot"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database}
	incomplete := &publishplan.Inspection{
		PlanID: 12, JobID: job.ID, PlatformCode: "youtube",
		Evidence: map[string]interface{}{
			"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
			"observedStatus": "Public", "screenshotPath": source,
		},
	}
	if record, err := server.recordPublishedManagerInspection(incomplete); err == nil || record != nil {
		t.Fatalf("incomplete evidence accepted: record=%+v err=%v", record, err)
	}
	complete := &publishplan.Inspection{
		PlanID: 12, JobID: job.ID, PlatformCode: "youtube",
		Evidence: map[string]interface{}{
			"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
			"observedStatus": "Public", "postURL": "https://www.youtube.com/shorts/AbCdEf12345",
			"externalPostID": "AbCdEf12345", "screenshotPath": source,
		},
	}
	record, err := server.recordPublishedManagerInspection(complete)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.Status != "published" || record.ExternalPostID != "AbCdEf12345" {
		t.Fatalf("record=%+v", record)
	}
	if _, err := os.Stat(filepath.Join(database.DataDir, "screenshots", "publish", fmt.Sprintf("plan-12-job-%d-published.png", job.ID))); err != nil {
		t.Fatal(err)
	}
}
