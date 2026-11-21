package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
)

func createUIJob(t *testing.T, server *Server, suffix string) *db.PublishJob {
	t.Helper()
	account, err := server.db.CreateAccount("douyin", "ui-job-"+suffix, "")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := server.db.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := server.db.CreateContent("graph_text", "job", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	job, err := server.db.CreatePublishJobWithIdempotencyKey(0, content.ID, account.ID, assets[0].ID, "douyin", content.Type, "ui-job:"+suffix+":0001")
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func serveJSON(t *testing.T, server *Server, method, path string) (*httptest.ResponseRecorder, map[string]json.RawMessage) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	var body map[string]json.RawMessage
	if strings.Contains(recorder.Header().Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
			t.Fatalf("decode %s %s: %v body=%s", method, path, err, recorder.Body.String())
		}
	}
	return recorder, body
}

func TestPublishJobDetailUsesStableSnakeCaseAndIncludesEvents(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	job := createUIJob(t, server, "detail")
	if _, err := server.db.Exec(`UPDATE publish_jobs SET mode='dry_run' WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	if err := server.db.UpdatePublishJob(job.ID, "failed", "browser_attached", "CDP_CONNECT", "safe failure", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateJobEventDetailed(db.JobEventInput{
		JobID: job.ID, Level: "error", Step: "browser_attached", Type: "job.retryable_failed",
		Message: "safe failure", ScreenshotPath: "/tmp/safe-shot.png", DOMSnapshotPath: "/tmp/safe-dom.html",
		ErrorCode: "CDP_CONNECT", Retryable: true,
		RawJSON: `{"missingRequiredControls":["content.body"],"unverifiedFields":["settings.comment_enabled"],"unsafe":"not returned"}`,
	}); err != nil {
		t.Fatal(err)
	}

	recorder, body := serveJSON(t, server, http.MethodGet, fmt.Sprintf("/api/jobs/%d", job.ID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	for _, key := range []string{"id", "batch_id", "plan_id", "plan_target_id", "content_id", "account_id", "asset_id", "platform_code", "content_type", "mode", "status", "current_step", "retry_count", "max_retries", "error_code", "error_message", "trace_id", "cancel_requested_at", "submit_attempted", "created_at", "can_cancel", "can_retry", "retry_blocked_by_submit", "creator_backend_verified", "evidence_screenshot_saved", "events"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("detail missing %q: keys=%v", key, objectKeys(body))
		}
	}
	if _, leaked := body["ID"]; leaked {
		t.Fatalf("detail leaked PascalCase fields: %s", recorder.Body.String())
	}
	var canRetry bool
	if err := json.Unmarshal(body["can_retry"], &canRetry); err != nil || !canRetry {
		t.Fatalf("can_retry=%s err=%v", body["can_retry"], err)
	}
	var events []map[string]json.RawMessage
	if err := json.Unmarshal(body["events"], &events); err != nil || len(events) != 1 {
		t.Fatalf("events=%s err=%v", body["events"], err)
	}
	if _, leaked := events[0]["screenshot_path"]; leaked {
		t.Fatalf("event leaked screenshot path: %+v", events[0])
	}
	if _, leaked := events[0]["dom_snapshot_path"]; leaked {
		t.Fatalf("event leaked DOM snapshot path: %+v", events[0])
	}
	if string(events[0]["screenshot_saved"]) != `true` || string(events[0]["dom_snapshot_saved"]) != `true` {
		t.Fatalf("event evidence flags missing: %+v", events[0])
	}
	if _, leaked := events[0]["raw_json"]; leaked {
		t.Fatal("job detail leaked raw_json")
	}
	if string(events[0]["diagnostics"]) != `{"missing_required_controls":["content.body"],"unverified_fields":["settings.comment_enabled"]}` {
		t.Fatalf("safe diagnostics=%s", events[0]["diagnostics"])
	}
	listRequest := httptest.NewRequest(http.MethodGet, "/api/jobs/list", nil)
	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, listRequest)
	var listed []map[string]json.RawMessage
	if listRecorder.Code != http.StatusOK || json.NewDecoder(listRecorder.Body).Decode(&listed) != nil || len(listed) != 1 {
		t.Fatalf("list status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	if _, ok := listed[0]["can_cancel"]; !ok {
		t.Fatalf("list missing can_cancel: %+v", listed[0])
	}
	if _, leaked := listed[0]["ID"]; leaked {
		t.Fatalf("list leaked PascalCase fields: %+v", listed[0])
	}

	missing, _ := serveJSON(t, server, http.MethodGet, "/api/jobs/999999")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d", missing.Code)
	}
	bad, _ := serveJSON(t, server, http.MethodGet, "/api/jobs/not-an-id")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad id status=%d", bad.Code)
	}
}

func TestPublishJobCancelAndRetryAPIsDriveDurableStateAndExecution(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	pending := createUIJob(t, server, "cancel")
	cancelRecorder, cancelBody := serveJSON(t, server, http.MethodPost, fmt.Sprintf("/api/jobs/%d/cancel", pending.ID))
	if cancelRecorder.Code != http.StatusOK || string(cancelBody["action"]) != `"cancelled"` {
		t.Fatalf("cancel status=%d body=%s", cancelRecorder.Code, cancelRecorder.Body.String())
	}
	var cancelledJob map[string]json.RawMessage
	if err := json.Unmarshal(cancelBody["job"], &cancelledJob); err != nil || string(cancelledJob["status"]) != `"cancelled"` {
		t.Fatalf("cancelled job=%s err=%v", cancelBody["job"], err)
	}
	conflict, _ := serveJSON(t, server, http.MethodPost, fmt.Sprintf("/api/jobs/%d/cancel", pending.ID))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("repeat cancel status=%d", conflict.Code)
	}
	running := createUIJob(t, server, "cancel-running")
	if claimed, err := server.db.ClaimPublishJob(running.ID, "ui-running"); err != nil || !claimed {
		t.Fatalf("claim running=%v err=%v", claimed, err)
	}
	runningRecorder, runningBody := serveJSON(t, server, http.MethodPost, fmt.Sprintf("/api/jobs/%d/cancel", running.ID))
	if runningRecorder.Code != http.StatusAccepted || string(runningBody["action"]) != `"cancel_requested"` {
		t.Fatalf("running cancel status=%d body=%s", runningRecorder.Code, runningRecorder.Body.String())
	}

	retryJob := createUIJob(t, server, "retry")
	if _, err := server.db.Exec(`UPDATE publish_jobs SET mode='dry_run' WHERE id=?`, retryJob.ID); err != nil {
		t.Fatal(err)
	}
	if err := server.db.UpdatePublishJob(retryJob.ID, "failed", "browser_attached", "CDP_CONNECT", "safe failure", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateJobEventDetailed(db.JobEventInput{
		JobID: retryJob.ID, Level: "error", Step: "browser_attached", Type: "job.retryable_failed",
		Message: "safe failure", ErrorCode: "CDP_CONNECT", Retryable: true,
	}); err != nil {
		t.Fatal(err)
	}
	retryRecorder, retryBody := serveJSON(t, server, http.MethodPost, fmt.Sprintf("/api/jobs/%d/retry", retryJob.ID))
	if retryRecorder.Code != http.StatusAccepted || string(retryBody["action"]) != `"queued"` {
		t.Fatalf("retry status=%d body=%s", retryRecorder.Code, retryRecorder.Body.String())
	}
	var taskID int64
	if err := json.Unmarshal(retryBody["queue_task_id"], &taskID); err != nil || taskID <= 0 {
		t.Fatalf("queue_task_id=%s err=%v", retryBody["queue_task_id"], err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		updated, getErr := server.db.GetPublishJob(retryJob.ID)
		if getErr == nil && updated.Status == "success" {
			items, listErr := server.db.ListTaskQueue(10)
			if listErr != nil || len(items) != 1 || items[0].ID != taskID || items[0].Status != "done" {
				t.Fatalf("queue items=%+v err=%v", items, listErr)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	updated, _ := server.db.GetPublishJob(retryJob.ID)
	t.Fatalf("retry did not execute: job=%+v", updated)
}

func TestPublishQueueTaskRejectsStoppedTargetBrowserBeforeRunner(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	job := createUIJob(t, server, "stopped-browser")
	task := &db.TaskQueueItem{ID: 91, PayloadJSON: fmt.Sprintf(`{"existing_job_id":%d}`, job.ID), TraceID: "ui-live-safety"}

	runErr := server.runPublishQueueTask(context.Background(), task)
	if runErr == nil || runErr.Error() != "EGO_SPACE_REQUIRED" {
		t.Fatalf("live publish without an Ego worker must fail closed before Runner: %v", runErr)
	}
	updated, err := server.db.GetPublishJob(job.ID)
	if err != nil || updated.Status != "failed" || updated.CurrentStep != "ego_space_preflight" {
		t.Fatalf("Ego preflight must enter failure center: job=%+v err=%v", updated, err)
	}
	events, err := server.db.ListJobEventsByJob(job.ID)
	if err != nil || len(events) != 1 || !events[0].Retryable || events[0].EventType != "job.retryable_failed" {
		t.Fatalf("stopped-browser failure must remain retryable: events=%+v err=%v", events, err)
	}
}

func TestPublishQueueTaskFailsClosedOnBrowserOperationLease(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	job := createUIJob(t, server, "operation-lease")
	if acquired, err := server.db.AcquireBrowserOperationLease(fmt.Sprintf("acct-%d", job.AccountID), "observe", "test:queued-publish-blocker", time.Now(), time.Minute); err != nil || !acquired {
		t.Fatalf("blocker acquire=%v err=%v", acquired, err)
	}
	task := &db.TaskQueueItem{ID: 93, PayloadJSON: fmt.Sprintf(`{"existing_job_id":%d}`, job.ID), TraceID: "ui-operation-lease"}

	runErr := server.runPublishQueueTask(context.Background(), task)
	if runErr == nil || runErr.Error() != "EGO_SPACE_REQUIRED" {
		t.Fatalf("queued live publish bypassed Ego-only preflight: %v", runErr)
	}
	updated, err := server.db.GetPublishJob(job.ID)
	if err != nil || updated.Status != "failed" || updated.CurrentStep != "ego_space_preflight" || updated.ErrorCode != "EGO_SPACE_REQUIRED" {
		t.Fatalf("Ego-only publish job changed state: job=%+v err=%v", updated, err)
	}
	events, err := server.db.ListJobEventsByJob(job.ID)
	if err != nil || len(events) != 1 || !events[0].Retryable || events[0].ErrorCode != "EGO_SPACE_REQUIRED" {
		t.Fatalf("Ego preflight did not create stable retryable event: events=%+v err=%v", events, err)
	}

	dryJob := createUIJob(t, server, "operation-lease-dry-run")
	if _, err := server.db.Exec(`UPDATE publish_jobs SET mode='dry_run' WHERE id=?`, dryJob.ID); err != nil {
		t.Fatal(err)
	}
	if acquired, err := server.db.AcquireBrowserOperationLease(fmt.Sprintf("acct-%d", dryJob.AccountID), "observe", "test:queued-dry-run-blocker", time.Now(), time.Minute); err != nil || !acquired {
		t.Fatalf("dry blocker acquire=%v err=%v", acquired, err)
	}
	dryTask := &db.TaskQueueItem{ID: 94, PayloadJSON: fmt.Sprintf(`{"existing_job_id":%d}`, dryJob.ID), TraceID: "ui-operation-lease-dry"}
	if runErr := server.runPublishQueueTask(context.Background(), dryTask); runErr != nil {
		t.Fatalf("dry-run was blocked by browser operation lease: %v", runErr)
	}
	dryUpdated, err := server.db.GetPublishJob(dryJob.ID)
	if err != nil || dryUpdated.Status != "success" {
		t.Fatalf("dry-run job=%+v err=%v", dryUpdated, err)
	}
}

func TestPublishQueueTaskAcceptsMultipleReviewedMedia(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	job := createUIJob(t, server, "multiple-media")
	for index := 0; index < 2; index++ {
		if _, err := server.db.CreateMediaAsset(job.ContentID, "image", fmt.Sprintf("/tmp/ui-media-%d.png", index), "", "image/png", 1, fmt.Sprintf("ui-sha-%d", index), 1, 1, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := server.db.Exec(`UPDATE publish_jobs SET mode='dry_run' WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	task := &db.TaskQueueItem{ID: 92, PayloadJSON: fmt.Sprintf(`{"existing_job_id":%d,"dry_run":true}`, job.ID), TraceID: "ui-media-safety"}

	runErr := server.runPublishQueueTask(context.Background(), task)
	if runErr != nil {
		t.Fatalf("multiple reviewed media must reach the runner: %v", runErr)
	}
	updated, err := server.db.GetPublishJob(job.ID)
	if err != nil || updated.Status != "success" {
		t.Fatalf("multiple-media dry-run job=%+v err=%v", updated, err)
	}
	events, err := server.db.ListJobEventsByJob(job.ID)
	if err != nil || len(events) == 0 {
		t.Fatalf("multiple-media dry-run events=%+v err=%v", events, err)
	}
}
