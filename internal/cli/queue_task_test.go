package cli_test

import (
	"strconv"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestQueueEnqueueRejectsUnsupportedNoopKind(t *testing.T) {
	dir := initTestDir(t)
	out, err := executeCommand(t, "queue", "enqueue", "--kind", "unknown-noop", "--payload", `{}`, "--data-dir", dir)
	if err == nil || !strings.Contains(err.Error(), "unsupported queue kind") {
		t.Fatalf("unsupported kind must fail: err=%v out=%s", err, out)
	}
}

func TestQueueRunOnceFailsLegacyUnknownKind(t *testing.T) {
	dir := initTestDir(t)
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	item, err := database.EnqueueTask("legacy-noop", `{}`, 0, "", "trace-legacy-noop")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	out, runErr := executeCommand(t, "queue", "run-once", "--data-dir", dir)
	if runErr == nil || !strings.Contains(runErr.Error(), "unsupported queue kind") {
		t.Fatalf("legacy unknown task must fail: err=%v out=%s", runErr, out)
	}
	database, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	items, err := database.ListTaskQueue(10)
	if err != nil || len(items) != 1 || items[0].ID != item.ID || items[0].Status != "failed" {
		t.Fatalf("legacy task must persist failed state: items=%+v err=%v", items, err)
	}
}

func TestQueueRunOnceRejectsLegacyLivePublishBeforeBrowser(t *testing.T) {
	dir := initTestDir(t)
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("douyin", "legacy-live-queue", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		database.Close()
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := database.CreateContent("graph_text", "legacy live", "caption", "body", "[]", "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	payload := `{"platform":"douyin","content_id":` + strconv.FormatInt(content.ID, 10) + `,"target_id":` + strconv.FormatInt(assets[0].ID, 10) + `,"dry_run":false}`
	if _, err := database.EnqueueTask("publish", payload, 0, "", "trace-legacy-live"); err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	out, runErr := executeCommand(t, "queue", "run-once", "--data-dir", dir)
	if runErr == nil || !strings.Contains(runErr.Error(), "PUBLISH_EGOLITE_PLAN_REQUIRED") {
		t.Fatalf("legacy live queue must fail closed: err=%v out=%s", runErr, out)
	}
}

func TestQueueRunOnceExecutesExistingPublishJobRetry(t *testing.T) {
	dir := initTestDir(t)
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("douyin", "retry-worker", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		database.Close()
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := database.CreateContent("graph_text", "retry", "caption", "body", "[]", "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	job, err := database.CreatePublishJobWithIdempotencyKey(0, content.ID, account.ID, assets[0].ID, "douyin", content.Type, "cli-retry:0001")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET mode='dry_run' WHERE id=?`, job.ID); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.UpdatePublishJob(job.ID, "failed", "browser_attached", "CDP_CONNECT", "safe failure", 0); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := database.CreateJobEventDetailed(db.JobEventInput{JobID: job.ID, Level: "error", Type: "job.retryable_failed", ErrorCode: "CDP_CONNECT", Retryable: true}); err != nil {
		database.Close()
		t.Fatal(err)
	}
	_, task, err := database.RetryPublishJob(job.ID, "cli-retry-trace")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	out, runErr := executeCommand(t, "queue", "run-once", "--data-dir", dir)
	if runErr != nil {
		t.Fatalf("retry queue execution failed: err=%v out=%s", runErr, out)
	}
	database, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetPublishJob(job.ID)
	if err != nil || updated.Status != "success" || updated.RetryCount != 1 {
		t.Fatalf("job=%+v err=%v", updated, err)
	}
	items, err := database.ListTaskQueue(10)
	if err != nil || len(items) != 1 || items[0].ID != task.ID || items[0].Status != "done" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
