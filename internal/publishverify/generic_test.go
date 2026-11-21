//go:build legacy_chromium

package publishverify

import (
	"context"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/browser"
)

func TestGenericVerifierRecordsOnePublishedBackendItem(t *testing.T) {
	database, job, submitted, coverHash := setupXHSVerificationJob(t, 1)
	defer database.Close()

	observer := &xhsSequenceObserver{snapshots: []XHSBackendSnapshot{
		xhsPublishedSnapshot(submitted.Add(time.Minute), coverHash),
	}}
	verifier := &GenericVerifier{
		DB: database,
		Browsers: fakeBrowserStatus{status: &browser.AccountStatus{
			AccountID: job.AccountID, Running: true, CDPPort: 15124,
			CDPURL: "http://127.0.0.1:15124",
		}},
		PlatformCode: "xhs",
		Observer:     observer,
		Now:          func() time.Time { return submitted },
	}
	result, err := verifier.Verify(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != VerificationPublished || result.Record == nil || result.Record.ExternalPostID != testXHSNoteID {
		t.Fatalf("result=%+v", result)
	}
}

func TestGenericVerifierFailsClosedWithoutAReviewedDOMObserver(t *testing.T) {
	database, job, submitted, _ := setupXHSVerificationJob(t, 1)
	defer database.Close()

	verifier := &GenericVerifier{
		DB: database,
		Browsers: fakeBrowserStatus{status: &browser.AccountStatus{
			AccountID: job.AccountID, Running: true, CDPPort: 15124,
			CDPURL: "http://127.0.0.1:15124",
		}},
		PlatformCode: "xhs",
		Now:          func() time.Time { return submitted },
	}
	result, err := verifier.Verify(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != VerificationManualTakeover || result.ErrorCode != "XHS_BACKEND_DOM_UNVERIFIED" {
		t.Fatalf("result=%+v", result)
	}
	var records int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=?`, job.ID).Scan(&records); err != nil || records != 0 {
		t.Fatalf("records=%d err=%v", records, err)
	}
}
