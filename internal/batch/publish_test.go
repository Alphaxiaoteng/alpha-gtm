package batch

import (
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestItemIsResumable(t *testing.T) {
	yes := []string{"pending", "failed", "interrupted", "retryable", "retryable_failed", "manual_takeover", "awaiting_review", "PENDING"}
	for _, s := range yes {
		if !ItemIsResumable(s) {
			t.Fatalf("expected resumable: %s", s)
		}
	}
	no := []string{
		"success", "skipped", "cancelled", "completed", "running",
		"login_required", "submitted_pending_verification", "result_unknown",
		"published_verified", "scheduled_verified",
	}
	for _, s := range no {
		// cancelled is NOT in META_AUTOMATION --resume set (pending/failed/interrupted)
		if s == "cancelled" {
			if ItemIsResumable(s) {
				t.Fatalf("cancelled should not be auto-resumed by ItemIsResumable: use batch retry")
			}
			continue
		}
		if ItemIsResumable(s) {
			t.Fatalf("expected not resumable: %s", s)
		}
	}
}

func TestFilterResumableItems(t *testing.T) {
	items := []db.BatchItem{
		{ID: 1, Status: "success"},
		{ID: 2, Status: "failed"},
		{ID: 3, Status: "pending"},
		{ID: 4, Status: "skipped"},
		{ID: 5, Status: "interrupted"},
		{ID: 6, Status: "manual_takeover"},
		{ID: 7, Status: "awaiting_review"},
		{ID: 8, Status: "running"},
	}
	got := FilterResumableItems(items)
	if len(got) != 5 {
		t.Fatalf("got %d items: %+v", len(got), got)
	}
	ids := map[int64]bool{}
	for _, it := range got {
		ids[it.ID] = true
	}
	for _, id := range []int64{2, 3, 5, 6, 7} {
		if !ids[id] {
			t.Fatalf("missing id %d", id)
		}
	}
	if ids[1] || ids[4] || ids[8] {
		t.Fatalf("terminal/running item was selected: %+v", got)
	}
}

func TestClassifyPublishStatus(t *testing.T) {
	tests := []struct {
		status       string
		itemStatus   string
		event        string
		success      bool
		waitingHuman bool
	}{
		{status: "dry_run_ok", itemStatus: "success", event: "batch.item.success", success: true},
		{status: "published", itemStatus: "success", event: "batch.item.success", success: true},
		{status: "success", itemStatus: "success", event: "batch.item.success", success: true},
		{status: "published_verified", itemStatus: "success", event: "batch.item.success", success: true},
		{status: "scheduled_verified", itemStatus: "success", event: "batch.item.success", success: true},
		{status: "manual_takeover", itemStatus: "manual_takeover", event: "batch.item.handoff", waitingHuman: true},
		{status: "awaiting_review", itemStatus: "awaiting_review", event: "batch.item.awaiting_review", waitingHuman: true},
		{status: "login_required", itemStatus: "login_required", event: "batch.item.login_required", waitingHuman: true},
		{status: "submitted_pending_verification", itemStatus: "submitted_pending_verification", event: "batch.item.verification_pending"},
		{status: "result_unknown", itemStatus: "result_unknown", event: "batch.item.result_unknown"},
	}
	for _, tt := range tests {
		got := ClassifyPublishStatus(tt.status)
		if got.ItemStatus != tt.itemStatus || got.Event != tt.event || got.Success != tt.success || got.WaitingHuman != tt.waitingHuman {
			t.Fatalf("status=%q got=%+v want item=%q event=%q success=%v waiting=%v", tt.status, got, tt.itemStatus, tt.event, tt.success, tt.waitingHuman)
		}
	}
	unknown := ClassifyPublishStatus("permanent_failed")
	if unknown.ItemStatus != "failed" || unknown.Event != "batch.item.failed" || unknown.Success || unknown.WaitingHuman {
		t.Fatalf("unknown failure classification=%+v", unknown)
	}
}

func TestRecountPublishBatchItemsKeepsLoginAndVerificationStatesUnfinished(t *testing.T) {
	counts := RecountPublishBatchItems([]db.BatchItem{
		{Status: "success"},
		{Status: "dry_run_ok"},
		{Status: "published"},
		{Status: "published_verified"},
		{Status: "scheduled_verified"},
		{Status: "manual_takeover"},
		{Status: "awaiting_review"},
		{Status: "login_required"},
		{Status: "submitted_pending_verification"},
		{Status: "result_unknown"},
		{Status: "pending"},
		{Status: "running"},
		{Status: "failed"},
	})
	if counts.Pending != 7 || counts.Success != 5 || counts.Failed != 1 ||
		counts.WaitingCount != 3 || !counts.WaitingHuman || counts.LoginRequired != 1 ||
		counts.VerificationPending != 1 || counts.ResultUnknown != 1 {
		t.Fatalf("counts=%+v", counts)
	}
	if got := PublishBatchSummaryEvent(counts); got != "batch.waiting_human" {
		t.Fatalf("waiting counts event=%q, want batch.waiting_human", got)
	}
	verificationOnly := RecountPublishBatchItems([]db.BatchItem{
		{Status: "submitted_pending_verification"},
		{Status: "result_unknown"},
	})
	if got := PublishBatchSummaryEvent(verificationOnly); got != "batch.verification_pending" {
		t.Fatalf("verification summary event=%q", got)
	}
	finished := RecountPublishBatchItems([]db.BatchItem{{Status: "success"}, {Status: "failed"}})
	if got := PublishBatchSummaryEvent(finished); got != "batch.finished" {
		t.Fatalf("finished counts event=%q, want batch.finished", got)
	}
}

func TestParsePublishCSV(t *testing.T) {
	raw := `content_id,platform,target_id,account_id
	1,instagram,10,3
	2,facebook,11,4
	`
	rows, err := ParsePublishCSV(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0].ContentID != 1 || rows[0].Platform != "instagram" || rows[0].TargetID != 10 || rows[0].AccountID != 3 {
		t.Fatalf("row0=%+v", rows[0])
	}
	if rows[1].ContentID != 2 || rows[1].Platform != "facebook" || rows[1].TargetID != 11 || rows[1].AccountID != 4 {
		t.Fatalf("row1=%+v", rows[1])
	}
}

func TestParsePublishCSVInvalid(t *testing.T) {
	_, err := ParsePublishCSV(strings.NewReader("content_id,platform,target_id\nbad,ig,1\n"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFindLatestPublishBatch(t *testing.T) {
	runs := []db.BatchRun{
		{ID: 5, OperationType: "publish", PlatformCode: "instagram", SettingsJSON: `{"content_file":"/tmp/a.csv"}`},
		{ID: 4, OperationType: "publish", PlatformCode: "facebook", SettingsJSON: `{"content_file":"/tmp/b.csv"}`},
		{ID: 3, OperationType: "account-check", PlatformCode: "instagram"},
	}
	got := FindLatestPublishBatch(runs, "instagram", "/tmp/a.csv")
	if got == nil || got.ID != 5 {
		t.Fatalf("got %+v", got)
	}
	got2 := FindLatestPublishBatch(runs, "facebook", "")
	if got2 == nil || got2.ID != 4 {
		t.Fatalf("got2 %+v", got2)
	}
	if FindLatestPublishBatch(runs, "xhs", "") != nil {
		t.Fatal("expected nil for missing platform")
	}
}

func TestJobIsResumable(t *testing.T) {
	if !JobIsResumable("manual_takeover") || !JobIsResumable("awaiting_review") || !JobIsResumable("failed") {
		t.Fatal("manual_takeover/awaiting_review/failed should resume")
	}
	if JobIsResumable("success") || JobIsResumable("published") || JobIsResumable("running") ||
		JobIsResumable("login_required") || JobIsResumable("submitted_pending_verification") ||
		JobIsResumable("result_unknown") || JobIsResumable("published_verified") ||
		JobIsResumable("scheduled_verified") {
		t.Fatal("terminal, login, and verification states should not auto-resume")
	}
}
