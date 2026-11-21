package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/feedback"
)

type fakeFeedbackClient struct {
	submitRequest feedback.SubmitRequest
	submitResult  feedback.Receipt
	statusResult  []feedback.Status
	submitErr     error
	statusErr     error
	configured    *bool
	configuredErr error
}

type idempotentFeedbackClient struct {
	requests      []feedback.SubmitRequest
	receipts      map[string]feedback.Receipt
	remoteCreates int
}

func (f *idempotentFeedbackClient) Configured() (bool, error) { return true, nil }

func (f *idempotentFeedbackClient) Submit(_ context.Context, request feedback.SubmitRequest) (feedback.Receipt, error) {
	f.requests = append(f.requests, request)
	if request.ClientToken == "" || request.FeedbackNumber == "" || request.ReadToken == "" {
		return feedback.Receipt{}, errors.New("missing stable submission credentials")
	}
	if receipt, ok := f.receipts[request.ClientToken]; ok {
		return receipt, nil
	}
	f.remoteCreates++
	receipt := feedback.Receipt{
		FeedbackID: fmt.Sprintf("rec_retry_%d", f.remoteCreates),
		ReadToken:  request.ReadToken,
		Status:     "待处理",
		Category:   "未分类",
		CreatedAt:  "2026-07-26T10:00:00Z",
		UpdatedAt:  "2026-07-26T10:00:00Z",
	}
	f.receipts[request.ClientToken] = receipt
	return receipt, nil
}

func (f *idempotentFeedbackClient) Status(_ context.Context, _ []feedback.Credential) ([]feedback.Status, error) {
	return nil, nil
}

func (f *fakeFeedbackClient) Configured() (bool, error) {
	if f.configuredErr != nil {
		return false, f.configuredErr
	}
	if f.configured != nil {
		return *f.configured, nil
	}
	return true, nil
}

func (f *fakeFeedbackClient) Submit(_ context.Context, request feedback.SubmitRequest) (feedback.Receipt, error) {
	f.submitRequest = request
	return f.submitResult, f.submitErr
}

func TestFeedbackSubmitStopsBeforeCreatingIntentWhenNotConfigured(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	configured := false
	client := &fakeFeedbackClient{configured: &configured}
	server.feedbackClient = client

	response := httptest.NewRecorder()
	server.handleAPIFeedback(response, httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewBufferString(
		`{"content":"未配置时不能创建待提交记录"}`,
	)))
	if response.Code != http.StatusServiceUnavailable ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"error_code":"FEEDBACK_NOT_CONFIGURED"`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var pending int
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM feedback_submission_intents`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 || client.submitRequest.Content != "" {
		t.Fatalf("pending=%d remote request=%+v", pending, client.submitRequest)
	}
}

func TestFeedbackListReportsNonSecretConfiguredState(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	configured := false
	server.feedbackClient = &fakeFeedbackClient{configured: &configured}

	response := httptest.NewRecorder()
	server.handleAPIFeedback(response, httptest.NewRequest(http.MethodGet, "/api/feedback", nil))
	if response.Code != http.StatusOK ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"configured":false`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("app_secret")) {
		t.Fatalf("configuration response leaked a credential field: %s", response.Body.String())
	}
}

func TestFeedbackSubmitRetryReusesRemoteRecordAfterLocalReceiptWriteFails(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	client := &idempotentFeedbackClient{receipts: make(map[string]feedback.Receipt)}
	server.feedbackClient = client
	if _, err := server.db.Exec(`
		CREATE TRIGGER fail_feedback_receipt_insert
		BEFORE INSERT ON feedback_receipts
		BEGIN
			SELECT RAISE(FAIL, 'forced receipt write failure');
		END
	`); err != nil {
		t.Fatal(err)
	}
	body := `{"content":"本地回执失败后重试不能重复提交","current_module":"想法"}`

	first := httptest.NewRecorder()
	server.handleAPIFeedback(first, httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewBufferString(body)))
	if first.Code != http.StatusInternalServerError ||
		!bytes.Contains(first.Body.Bytes(), []byte(`"error_code":"FEEDBACK_LOCAL_STORE_FAILED"`)) {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	if _, err := server.db.Exec(`DROP TRIGGER fail_feedback_receipt_insert`); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	server, err = NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.feedbackClient = client

	second := httptest.NewRecorder()
	server.handleAPIFeedback(second, httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewBufferString(body)))
	if second.Code != http.StatusCreated {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	if client.remoteCreates != 1 || len(client.requests) != 2 {
		t.Fatalf("remote creates=%d requests=%d", client.remoteCreates, len(client.requests))
	}
	firstRequest, secondRequest := client.requests[0], client.requests[1]
	if firstRequest.ClientToken != secondRequest.ClientToken ||
		firstRequest.FeedbackNumber != secondRequest.FeedbackNumber ||
		firstRequest.ReadToken != secondRequest.ReadToken {
		t.Fatalf("submission credentials changed across retry: first=%+v second=%+v", firstRequest, secondRequest)
	}
	items, err := server.db.ListFeedbackReceipts(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].FeedbackID != "rec_retry_1" || items[0].ReadToken != firstRequest.ReadToken {
		t.Fatalf("items=%+v", items)
	}
	var pending int
	if err := server.db.QueryRow(`SELECT COUNT(*) FROM feedback_submission_intents`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("pending intents=%d", pending)
	}
}

func (f *fakeFeedbackClient) Status(_ context.Context, _ []feedback.Credential) ([]feedback.Status, error) {
	return f.statusResult, f.statusErr
}

func TestNewServerUsesLocalFeishuClient(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, ok := server.feedbackClient.(*feedback.FeishuClient); !ok {
		t.Fatalf("feedback client=%T", server.feedbackClient)
	}
}

func TestFeedbackSubmitValidatesAndStoresAnonymousReceipt(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := &fakeFeedbackClient{submitResult: feedback.Receipt{
		FeedbackID: "rec_123",
		ReadToken:  "query-token",
		Status:     "待处理",
		Category:   "未分类",
		CreatedAt:  "2026-07-26T10:00:00Z",
		UpdatedAt:  "2026-07-26T10:00:00Z",
	}}
	server.feedbackClient = client
	server.health.Version = "0.5.0-dev"

	request := httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewBufferString(
		`{"content":"  希望增加批量重试  ","current_module":"一键发布"}`,
	))
	response := httptest.NewRecorder()
	server.handleAPIFeedback(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if client.submitRequest.Content != "希望增加批量重试" ||
		client.submitRequest.CurrentModule != "一键发布" ||
		client.submitRequest.AppVersion != "0.5.0-dev" ||
		client.submitRequest.OperatingSystem == "" {
		t.Fatalf("submit request=%+v", client.submitRequest)
	}

	var item map[string]any
	if err := json.NewDecoder(response.Body).Decode(&item); err != nil {
		t.Fatal(err)
	}
	if item["feedback_id"] != "rec_123" {
		t.Fatalf("response=%v", item)
	}
	if _, leaked := item["read_token"]; leaked {
		t.Fatalf("query credential leaked to UI: %v", item)
	}
}

func TestFeedbackSubmitRejectsInvalidLengthWithoutCallingRemote(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := &fakeFeedbackClient{}
	server.feedbackClient = client

	request := httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewBufferString(`{"content":"a"}`))
	response := httptest.NewRecorder()
	server.handleAPIFeedback(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if client.submitRequest.Content != "" {
		t.Fatal("gateway was called for invalid input")
	}
}

func TestFeedbackRefreshMapsRemoteStatus(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := &fakeFeedbackClient{submitResult: feedback.Receipt{
		FeedbackID: "rec_456", ReadToken: "query-token", Status: "待处理",
		CreatedAt: "2026-07-26T10:00:00Z", UpdatedAt: "2026-07-26T10:00:00Z",
	}}
	server.feedbackClient = client
	create := httptest.NewRecorder()
	server.handleAPIFeedback(create, httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewBufferString(
		`{"content":"这是一个可以复现的 Bug"}`,
	)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	client.statusResult = []feedback.Status{{
		FeedbackID: "rec_456",
		Status:     "已完成",
		Category:   "Bug",
		Reply:      "已在开发版修复。",
		UpdatedAt:  "2026-07-26T11:00:00Z",
	}}

	response := httptest.NewRecorder()
	server.handleAPIFeedback(response, httptest.NewRequest(http.MethodGet, "/api/feedback?refresh=1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"status":"已完成"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"developer_reply":"已在开发版修复。"`)) {
		t.Fatalf("body=%s", response.Body.String())
	}
}

func TestFeedbackRemoteFailureDoesNotCreateLocalSuccess(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.feedbackClient = &fakeFeedbackClient{submitErr: errors.New("upstream unavailable")}

	response := httptest.NewRecorder()
	server.handleAPIFeedback(response, httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewBufferString(
		`{"content":"这条反馈不能伪造成功"}`,
	)))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	items, err := server.db.ListFeedbackReceipts(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("failed remote submission was stored as success: %+v", items)
	}
}

func TestFeedbackRefreshKeepsCachedHistoryWhenRemoteFails(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := &fakeFeedbackClient{submitResult: feedback.Receipt{
		FeedbackID: "rec_cached", ReadToken: "query-token", Status: "待处理",
	}}
	server.feedbackClient = client
	create := httptest.NewRecorder()
	server.handleAPIFeedback(create, httptest.NewRequest(http.MethodPost, "/api/feedback", bytes.NewBufferString(
		`{"content":"保留在本机的反馈"}`,
	)))
	client.statusErr = errors.New("timeout")

	response := httptest.NewRecorder()
	server.handleAPIFeedback(response, httptest.NewRequest(http.MethodGet, "/api/feedback?refresh=1", nil))
	if response.Code != http.StatusOK ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"feedback_id":"rec_cached"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"sync_error":"FEEDBACK_SYNC_UNAVAILABLE"`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte("query-token")) {
		t.Fatalf("query credential leaked: %s", response.Body.String())
	}
}
