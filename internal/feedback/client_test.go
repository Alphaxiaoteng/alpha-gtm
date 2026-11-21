package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryConfigStore struct {
	config Config
	err    error
}

func (store memoryConfigStore) Load() (Config, error) { return store.config, store.err }
func (store memoryConfigStore) Save(Config) error     { return nil }
func (store memoryConfigStore) Configured() (bool, error) {
	return store.err == nil, store.err
}

func TestFeishuClientSubmitsAndBatchRefreshesFromBase(t *testing.T) {
	config := Config{
		AppID:     "cli_test",
		AppSecret: "synthetic-secret",
		WikiToken: "wik_test",
		TableID:   "tbl_test",
	}
	var mu sync.Mutex
	calls := map[string]int{}
	var credentialDigest string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["app_id"] != config.AppID || body["app_secret"] != config.AppSecret {
				t.Fatalf("auth body=%v", body)
			}
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0, "tenant_access_token": "tenant-token", "expire": 7200,
			})
		case "/open-apis/wiki/v2/spaces/get_node":
			if r.URL.Query().Get("token") != config.WikiToken {
				t.Fatalf("wiki token=%q", r.URL.Query().Get("token"))
			}
			assertBearer(t, r)
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0,
				"data": map[string]any{"node": map[string]any{
					"obj_type": "bitable", "obj_token": "base-token",
				}},
			})
		case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/fields":
			assertBearer(t, r)
			items := make([]map[string]string, 0)
			for _, name := range []string{
				"需求意见", "反馈编号", "查询凭证摘要", "反馈类型", "处理状态",
				"开发者回复", "应用版本", "当前模块", "操作系统", "发布渠道",
			} {
				items = append(items, map[string]string{"field_name": name})
			}
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0, "data": map[string]any{"items": items},
			})
		case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/records":
			assertBearer(t, r)
			if token := r.URL.Query().Get("client_token"); !looksLikeUUID(token) {
				t.Fatalf("client_token=%q", token)
			}
			var body struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Fields["需求意见"] != "希望支持一键重试" ||
				body.Fields["当前模块"] != "一键发布" ||
				body.Fields["处理状态"] != "待处理" {
				t.Fatalf("fields=%v", body.Fields)
			}
			credentialDigest, _ = body.Fields["查询凭证摘要"].(string)
			if len(credentialDigest) != 64 || strings.Contains(credentialDigest, "query") {
				t.Fatalf("credential digest=%q", credentialDigest)
			}
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0,
				"data": map[string]any{"record": map[string]any{
					"record_id":          "rec_test",
					"created_time":       "1785027600000",
					"last_modified_time": "1785027600000",
				}},
			})
		case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/records/batch_get":
			assertBearer(t, r)
			var body struct {
				RecordIDs      []string `json:"record_ids"`
				WithSharedURL  bool     `json:"with_shared_url"`
				AutomaticField bool     `json:"automatic_fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.RecordIDs) != 1 || body.RecordIDs[0] != "rec_test" ||
				body.WithSharedURL || body.AutomaticField {
				t.Fatalf("batch body=%+v", body)
			}
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0,
				"data": map[string]any{"records": []any{map[string]any{
					"record_id":          "rec_test",
					"last_modified_time": "1785031200000",
					"fields": map[string]any{
						"查询凭证摘要": credentialDigest,
						"处理状态":   "处理中",
						"反馈类型":   "Bug",
						"开发者回复":  []any{map[string]any{"text": "正在修复。"}},
					},
				}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newFeishuClientForTest(
		memoryConfigStore{config: config},
		server.URL,
		&http.Client{Timeout: time.Second},
	)
	receipt, err := client.Submit(context.Background(), SubmitRequest{
		Content:         "希望支持一键重试",
		AppVersion:      "0.5.0-dev",
		CurrentModule:   "一键发布",
		OperatingSystem: "darwin",
		Channel:         "electron",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.FeedbackID != "rec_test" || receipt.ReadToken == "" ||
		receipt.Status != "待处理" || receipt.CreatedAt == "" {
		t.Fatalf("receipt=%+v", receipt)
	}

	statuses, err := client.Status(context.Background(), []Credential{{
		FeedbackID: receipt.FeedbackID,
		ReadToken:  receipt.ReadToken,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status != "处理中" ||
		statuses[0].Category != "Bug" || statuses[0].Reply != "正在修复。" {
		t.Fatalf("statuses=%+v", statuses)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls["/open-apis/auth/v3/tenant_access_token/internal"] != 1 ||
		calls["/open-apis/wiki/v2/spaces/get_node"] != 1 ||
		calls["/open-apis/bitable/v1/apps/base-token/tables/tbl_test/fields"] != 1 {
		t.Fatalf("cache-sensitive calls=%v", calls)
	}
}

func TestFeishuClientReusesPreparedSubmissionCredentials(t *testing.T) {
	config := Config{
		AppID: "cli_test", AppSecret: "synthetic-secret",
		BaseToken: "base-token", TableID: "tbl_test",
	}
	const (
		clientToken    = "8b45aaba-cbba-4b8f-8897-4a24fa75fb91"
		feedbackNumber = "ANX-stable"
		readToken      = "stable-local-query-token"
	)
	recordCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0, "tenant_access_token": "tenant-token", "expire": 7200,
			})
		case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/fields":
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0,
				"data": map[string]any{"items": []any{
					map[string]any{"field_name": "需求意见"},
					map[string]any{"field_name": "反馈编号"},
					map[string]any{"field_name": "查询凭证摘要"},
				}},
			})
		case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/records":
			recordCalls++
			if got := r.URL.Query().Get("client_token"); got != clientToken {
				t.Fatalf("client_token=%q", got)
			}
			var body struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Fields["反馈编号"] != feedbackNumber ||
				body.Fields["查询凭证摘要"] != tokenDigest(readToken) {
				t.Fatalf("fields=%v", body.Fields)
			}
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0,
				"data": map[string]any{"record": map[string]any{"record_id": "rec_stable"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newFeishuClientForTest(
		memoryConfigStore{config: config},
		server.URL,
		&http.Client{Timeout: time.Second},
	)
	request := SubmitRequest{
		Content:     "同一次提交重试",
		ClientToken: clientToken, FeedbackNumber: feedbackNumber, ReadToken: readToken,
	}
	for i := 0; i < 2; i++ {
		receipt, err := client.Submit(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.FeedbackID != "rec_stable" || receipt.ReadToken != readToken {
			t.Fatalf("receipt=%+v", receipt)
		}
	}
	if recordCalls != 2 {
		t.Fatalf("record calls=%d", recordCalls)
	}
}

func TestFeishuClientUsesOnlyFieldsThatExist(t *testing.T) {
	config := Config{
		AppID: "cli_test", AppSecret: "synthetic-secret",
		BaseToken: "base-token", TableID: "tbl_test",
	}
	var createdFields map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0, "tenant_access_token": "tenant-token", "expire": 7200,
			})
		case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/fields":
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0,
				"data": map[string]any{"items": []any{
					map[string]any{"field_name": "需求意见"},
				}},
			})
		case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/records":
			var body struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			createdFields = body.Fields
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0,
				"data": map[string]any{"record": map[string]any{"record_id": "rec_minimal"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newFeishuClientForTest(
		memoryConfigStore{config: config},
		server.URL,
		&http.Client{Timeout: time.Second},
	)
	if _, err := client.Submit(context.Background(), SubmitRequest{Content: "只写真实字段"}); err != nil {
		t.Fatal(err)
	}
	if len(createdFields) != 1 || createdFields["需求意见"] != "只写真实字段" {
		t.Fatalf("created fields=%v", createdFields)
	}
}

func TestFeishuClientRejectsMissingRequiredFieldAndNonBaseWiki(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		objectType string
		fields     []any
	}{
		{name: "wiki is not base", objectType: "docx"},
		{name: "required field missing", objectType: "bitable", fields: []any{
			map[string]any{"field_name": "其他字段"},
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			config := Config{
				AppID: "cli_test", AppSecret: "synthetic-secret",
				WikiToken: "wik_test", TableID: "tbl_test",
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/open-apis/auth/v3/tenant_access_token/internal":
					writeFeedbackJSON(t, w, map[string]any{
						"code": 0, "tenant_access_token": "tenant-token", "expire": 7200,
					})
				case "/open-apis/wiki/v2/spaces/get_node":
					writeFeedbackJSON(t, w, map[string]any{
						"code": 0,
						"data": map[string]any{"node": map[string]any{
							"obj_type": testCase.objectType, "obj_token": "base-token",
						}},
					})
				case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/fields":
					writeFeedbackJSON(t, w, map[string]any{
						"code": 0, "data": map[string]any{"items": testCase.fields},
					})
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := newFeishuClientForTest(
				memoryConfigStore{config: config},
				server.URL,
				&http.Client{Timeout: time.Second},
			)
			if _, err := client.Submit(context.Background(), SubmitRequest{Content: "反馈"}); !errors.Is(err, ErrSchemaMismatch) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestFeishuClientRejectsWrongLocalCredential(t *testing.T) {
	config := Config{
		AppID: "cli_test", AppSecret: "synthetic-secret",
		BaseToken: "base-token", TableID: "tbl_test",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0, "tenant_access_token": "tenant-token", "expire": 7200,
			})
		case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/fields":
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0,
				"data": map[string]any{"items": []any{
					map[string]any{"field_name": "需求意见"},
					map[string]any{"field_name": "查询凭证摘要"},
				}},
			})
		case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/records/batch_get":
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0,
				"data": map[string]any{"records": []any{map[string]any{
					"record_id": "rec_test",
					"fields": map[string]any{
						"查询凭证摘要": tokenDigest("different-token"),
						"处理状态":   "待处理",
					},
				}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newFeishuClientForTest(
		memoryConfigStore{config: config},
		server.URL,
		&http.Client{Timeout: time.Second},
	)
	_, err := client.Status(context.Background(), []Credential{{
		FeedbackID: "rec_test", ReadToken: "local-token",
	}})
	if !errors.Is(err, ErrRemoteUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestFeishuClientBatchStatusKeepsValidRecordsWhenOthersAreUnavailable(t *testing.T) {
	config := Config{
		AppID: "cli_test", AppSecret: "synthetic-secret",
		BaseToken: "base-token", TableID: "tbl_test",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0, "tenant_access_token": "tenant-token", "expire": 7200,
			})
		case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/fields":
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0,
				"data": map[string]any{"items": []any{
					map[string]any{"field_name": "查询凭证摘要"},
					map[string]any{"field_name": "处理状态"},
					map[string]any{"field_name": "反馈类型"},
				}},
			})
		case "/open-apis/bitable/v1/apps/base-token/tables/tbl_test/records/batch_get":
			var body struct {
				RecordIDs []string `json:"record_ids"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if strings.Join(body.RecordIDs, ",") != "rec_valid,rec_absent,rec_forbidden,rec_wrong,rec_bad_status" {
				t.Fatalf("record ids=%v", body.RecordIDs)
			}
			writeFeedbackJSON(t, w, map[string]any{
				"code": 0,
				"data": map[string]any{
					"records": []any{
						map[string]any{
							"record_id":          "rec_valid",
							"last_modified_time": "1785031200000",
							"fields": map[string]any{
								"查询凭证摘要": tokenDigest("valid-token"),
								"处理状态":   "已完成",
								"反馈类型":   "Bug",
							},
						},
						map[string]any{
							"record_id": "rec_wrong",
							"fields": map[string]any{
								"查询凭证摘要": tokenDigest("different-token"),
								"处理状态":   "处理中",
							},
						},
						map[string]any{
							"record_id": "rec_bad_status",
							"fields": map[string]any{
								"查询凭证摘要": tokenDigest("bad-status-token"),
								"处理状态":   "平台自定义未知状态",
							},
						},
					},
					"absent_record_ids":    []string{"rec_absent"},
					"forbidden_record_ids": []string{"rec_forbidden"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := newFeishuClientForTest(
		memoryConfigStore{config: config},
		server.URL,
		&http.Client{Timeout: time.Second},
	)

	statuses, err := client.Status(context.Background(), []Credential{
		{FeedbackID: "rec_valid", ReadToken: "valid-token"},
		{FeedbackID: "rec_absent", ReadToken: "absent-token"},
		{FeedbackID: "rec_forbidden", ReadToken: "forbidden-token"},
		{FeedbackID: "rec_wrong", ReadToken: "wrong-token"},
		{FeedbackID: "rec_bad_status", ReadToken: "bad-status-token"},
		{FeedbackID: "", ReadToken: "incomplete-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].FeedbackID != "rec_valid" ||
		statuses[0].Status != "已完成" || statuses[0].Category != "Bug" {
		t.Fatalf("statuses=%+v", statuses)
	}
}

func TestFeishuClientTimeoutAndErrorsDoNotLeakPayloadOrSecrets(t *testing.T) {
	const (
		secret  = "synthetic-secret-must-not-leak"
		content = "private-feedback-content-must-not-leak"
	)
	config := Config{
		AppID: "cli_test", AppSecret: secret,
		BaseToken: "base-token", TableID: "tbl_test",
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()
	client := newFeishuClientForTest(
		memoryConfigStore{config: config},
		server.URL,
		&http.Client{Timeout: 10 * time.Millisecond},
	)
	_, err := client.Submit(context.Background(), SubmitRequest{Content: content})
	if !errors.Is(err, ErrRemoteUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), content) {
		t.Fatalf("sensitive data leaked through error: %v", err)
	}
}

func TestNormalizeStatusSupportsBooleanCompletion(t *testing.T) {
	status, err := normalizeStatus(map[string]any{"是否处理完成": true})
	if err != nil || status != "已完成" {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func writeFeedbackJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func assertBearer(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
		t.Fatalf("authorization=%q", got)
	}
}

func looksLikeUUID(value string) bool {
	return len(value) == 36 &&
		value[8] == '-' && value[13] == '-' &&
		value[18] == '-' && value[23] == '-'
}
