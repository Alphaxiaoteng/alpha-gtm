package db

import (
	"testing"
	"time"
)

func TestFeedbackReceiptRoundTripAndStatusSync(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	created := FeedbackReceipt{
		FeedbackID: "rec_test",
		ReadToken:  "local-query-token",
		Content:    "希望增加批量重试",
		Status:     FeedbackStatusPending,
		Category:   "未分类",
		CreatedAt:  "2026-07-26T10:00:00Z",
		UpdatedAt:  "2026-07-26T10:00:00Z",
	}
	if err := database.CreateFeedbackReceipt(created); err != nil {
		t.Fatal(err)
	}

	items, err := database.ListFeedbackReceipts(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].FeedbackID != created.FeedbackID || items[0].ReadToken != created.ReadToken {
		t.Fatalf("items=%+v", items)
	}

	syncedAt := time.Now().UTC().Format(time.RFC3339)
	if err := database.UpdateFeedbackReceiptStatus(
		created.FeedbackID,
		FeedbackStatusDone,
		"Bug",
		"已经修复，请更新后重试。",
		"2026-07-26T11:00:00Z",
		syncedAt,
	); err != nil {
		t.Fatal(err)
	}
	items, err = database.ListFeedbackReceipts(20)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Status != FeedbackStatusDone || items[0].DeveloperReply == "" || items[0].LastSyncedAt != syncedAt {
		t.Fatalf("synced item=%+v", items[0])
	}
}

func TestFeedbackReceiptRejectsUnknownStatus(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	err = database.CreateFeedbackReceipt(FeedbackReceipt{
		FeedbackID: "rec_invalid",
		ReadToken:  "token",
		Content:    "有效反馈",
		Status:     "随便写的状态",
	})
	if err == nil {
		t.Fatal("expected invalid status to be rejected")
	}
}
