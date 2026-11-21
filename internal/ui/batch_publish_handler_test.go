package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
)

var batchRunJSONKeys = []string{
	"id", "operationType", "platformCode", "status", "totalItems",
	"pendingCount", "runningCount", "successCount", "failedCount",
	"skippedCount", "concurrency", "dryRun",
}

var batchItemJSONKeys = []string{
	"id", "batchId", "accountId", "contentId", "status", "errorMessage",
	"startedAt", "finishedAt",
}

func assertExactJSONKeys(t *testing.T, object map[string]json.RawMessage, expected []string) {
	t.Helper()
	if len(object) != len(expected) {
		t.Fatalf("JSON keys=%v; want exactly %v", objectKeys(object), expected)
	}
	for _, key := range expected {
		if _, ok := object[key]; !ok {
			t.Fatalf("JSON keys=%v; missing %q", objectKeys(object), key)
		}
	}
}

func objectKeys(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	return keys
}

func TestBatchPublishHandlerCreatesAndRunsDryBatch(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	content, err := server.db.CreateContent("graph_text", "batch", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	var assetIDs []int64
	for _, name := range []string{"batch-a", "batch-b"} {
		account, err := server.db.CreateAccount("douyin", name, "")
		if err != nil {
			t.Fatal(err)
		}
		assets, err := server.db.ListAccountAssetsByAccount(account.ID)
		if err != nil || len(assets) != 1 {
			t.Fatalf("assets=%+v err=%v", assets, err)
		}
		assetIDs = append(assetIDs, assets[0].ID)
	}
	body, _ := json.Marshal(map[string]any{"contentId": content.ID, "assetIds": assetIDs, "concurrency": 2, "dryRun": true})
	req := httptest.NewRequest(http.MethodPost, "/api/batch/publish", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	server.handleAPIBatchPublish(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var created map[string]json.RawMessage
	if err := json.NewDecoder(recorder.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	assertExactJSONKeys(t, created, batchRunJSONKeys)
	var runID int64
	if err := json.Unmarshal(created["id"], &runID); err != nil || runID <= 0 {
		t.Fatalf("invalid created run id: %s err=%v", created["id"], err)
	}
	var dryRun bool
	if err := json.Unmarshal(created["dryRun"], &dryRun); err != nil || !dryRun {
		t.Fatalf("dryRun=%s err=%v; want true", created["dryRun"], err)
	}

	deadline := time.Now().Add(3 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		items, err := server.db.ListBatchItems(runID)
		if err == nil && len(items) == 2 && items[0].Status == "success" && items[1].Status == "success" {
			for _, item := range items {
				if item.JobID <= 0 {
					t.Fatalf("batch item missing publish job: %+v", item)
				}
				if item.StartedAt == "" || item.FinishedAt == "" {
					t.Fatalf("batch item missing lifecycle timestamps: %+v", item)
				}
			}
			completed = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !completed {
		t.Fatal("dry-run batch did not finish")
	}

	runsRequest := httptest.NewRequest(http.MethodGet, "/api/batch/runs", nil)
	runsRecorder := httptest.NewRecorder()
	server.handleAPIBatchRuns(runsRecorder, runsRequest)
	if runsRecorder.Code != http.StatusOK {
		t.Fatalf("runs status=%d body=%s", runsRecorder.Code, runsRecorder.Body.String())
	}
	var runs []map[string]json.RawMessage
	if err := json.NewDecoder(runsRecorder.Body).Decode(&runs); err != nil || len(runs) != 1 {
		t.Fatalf("decode runs: len=%d err=%v body=%s", len(runs), err, runsRecorder.Body.String())
	}
	assertExactJSONKeys(t, runs[0], batchRunJSONKeys)
	if err := json.Unmarshal(runs[0]["dryRun"], &dryRun); err != nil || !dryRun {
		t.Fatalf("listed dryRun=%s err=%v; want true", runs[0]["dryRun"], err)
	}

	itemsRequest := httptest.NewRequest(http.MethodGet, "/api/batch/items?batchId="+fmt.Sprint(runID), nil)
	itemsRecorder := httptest.NewRecorder()
	server.handleAPIBatchItems(itemsRecorder, itemsRequest)
	if itemsRecorder.Code != http.StatusOK {
		t.Fatalf("items status=%d body=%s", itemsRecorder.Code, itemsRecorder.Body.String())
	}
	var itemResponses []map[string]json.RawMessage
	if err := json.NewDecoder(itemsRecorder.Body).Decode(&itemResponses); err != nil || len(itemResponses) != 2 {
		t.Fatalf("decode items: len=%d err=%v body=%s", len(itemResponses), err, itemsRecorder.Body.String())
	}
	for _, item := range itemResponses {
		assertExactJSONKeys(t, item, batchItemJSONKeys)
		var startedAt, finishedAt string
		if err := json.Unmarshal(item["startedAt"], &startedAt); err != nil || startedAt == "" {
			t.Fatalf("startedAt=%s err=%v; want lifecycle timestamp", item["startedAt"], err)
		}
		if err := json.Unmarshal(item["finishedAt"], &finishedAt); err != nil || finishedAt == "" {
			t.Fatalf("finishedAt=%s err=%v; want lifecycle timestamp", item["finishedAt"], err)
		}
	}
}

func TestBatchPublishHandlerRejectsLiveRequest(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/batch/publish", bytes.NewBufferString(`{"contentId":1,"assetIds":[1],"dryRun":false}`))
	recorder := httptest.NewRecorder()
	server.handleAPIBatchPublish(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestBatchPublishHandlerRejectsMetaBrowserContainer(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	content, err := server.db.CreateContent("graph_text", "batch", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := server.db.CreateAccount("facebook", "batch-meta-default", "")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := server.db.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	body, _ := json.Marshal(map[string]any{"contentId": content.ID, "assetIds": []int64{assets[0].ID}, "dryRun": true})
	req := httptest.NewRequest(http.MethodPost, "/api/batch/publish", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	server.handleAPIBatchPublish(recorder, req)
	if recorder.Code != http.StatusBadRequest || recorder.Body.String() != "PUBLISH_TARGET_ASSET_TYPE_INVALID\n" {
		t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	count, err := server.db.CountRows("batch_runs")
	if err != nil || count != 0 {
		t.Fatalf("invalid target must not create a batch: count=%d err=%v", count, err)
	}
}
