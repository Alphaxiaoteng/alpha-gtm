//go:build legacy_chromium

package cli

import (
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestResetBatchPublishItemForResumeResetsHumanStatesOnlyExplicitly(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	run, err := database.CreateBatchRun("publish", "xhs", "waiting_human", 3, 1, `{"dry_run":true}`)
	if err != nil {
		t.Fatal(err)
	}
	manual, err := database.CreateBatchItem(run.ID, "publish", 0, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	awaiting, err := database.CreateBatchItem(run.ID, "publish", 0, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	running, err := database.CreateBatchItem(run.ID, "publish", 0, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []*db.BatchItem{manual, awaiting, running} {
		status := "manual_takeover"
		if item.ID == awaiting.ID {
			status = "awaiting_review"
		} else if item.ID == running.ID {
			status = "running"
		}
		if err := database.UpdateBatchItemStatus(item.ID, status, "review", "AWAITING_HUMAN", "needs user"); err != nil {
			t.Fatal(err)
		}
	}

	for _, testCase := range []struct {
		item   *db.BatchItem
		status string
	}{
		{item: manual, status: "manual_takeover"},
		{item: awaiting, status: "awaiting_review"},
	} {
		reset, err := resetBatchPublishItemForResume(database, testCase.item.ID, testCase.status)
		if err != nil || !reset {
			t.Fatalf("human item %d reset=%v err=%v", testCase.item.ID, reset, err)
		}
		items, err := database.ListBatchItems(run.ID)
		if err != nil {
			t.Fatal(err)
		}
		var status string
		for _, got := range items {
			if got.ID == testCase.item.ID {
				status = got.Status
				break
			}
		}
		if status != "pending" {
			t.Fatalf("item %d status=%q, want pending", testCase.item.ID, status)
		}
	}

	if reset, err := resetBatchPublishItemForResume(database, running.ID, "running"); err == nil || reset {
		t.Fatalf("running item was reset: reset=%v err=%v", reset, err)
	}
}
