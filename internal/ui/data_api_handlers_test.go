package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/tikhub"
)

func callDataAPI(t *testing.T, server *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var input []byte
	if body != nil {
		var err error
		input, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(input))
	request.RemoteAddr = "127.0.0.1:41000"
	request.Host = "127.0.0.1:17321"
	request.Header.Set("Origin", "http://127.0.0.1:17321")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	server.ServeHTTP(recorder, request)
	return recorder
}

func TestDataAPIMutationsRejectCrossOriginRequests(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	for _, test := range []struct {
		method string
		path   string
		body   map[string]any
	}{
		{http.MethodPut, "/api/data-api/credential", map[string]any{"api_key": "synthetic-token", "region": tikhub.RegionMainland}},
		{http.MethodPost, "/api/data-api/test", map[string]any{"confirm": true}},
		{http.MethodPost, "/api/data-api/calls", map[string]any{"endpoint_id": "douyin.video_search", "confirm": true, "params": map[string]any{"keyword": "AI"}}},
	} {
		input, err := json.Marshal(test.body)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(test.method, test.path, bytes.NewReader(input))
		request.RemoteAddr = "127.0.0.1:41000"
		request.Host = "127.0.0.1:17321"
		request.Header.Set("Origin", "https://attacker.example")
		request.Header.Set("Content-Type", "text/plain")
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestDataAPIConfigSavesSecretWithoutEchoAndRequiresTestBeforeActivation(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	get := callDataAPI(t, server, http.MethodGet, "/api/data-api/config", nil)
	if get.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}
	if !strings.Contains(get.Body.String(), `"provider":"tikhub"`) ||
		!strings.Contains(get.Body.String(), tikhub.MainlandBase) ||
		!strings.Contains(get.Body.String(), `"configured":false`) ||
		strings.Contains(get.Body.String(), "api_key") {
		t.Fatalf("unexpected safe config response: %s", get.Body.String())
	}

	secret := "synthetic-tikhub-token-123456"
	put := callDataAPI(t, server, http.MethodPut, "/api/data-api/credential", map[string]any{
		"api_key": secret,
		"region":  tikhub.RegionMainland,
	})
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", put.Code, put.Body.String())
	}
	if strings.Contains(put.Body.String(), secret) || !strings.Contains(put.Body.String(), `"configured":true`) ||
		!strings.Contains(put.Body.String(), `"active":false`) {
		t.Fatalf("credential response leaked secret or activated without test: %s", put.Body.String())
	}

	unconfirmed := callDataAPI(t, server, http.MethodPost, "/api/data-api/test", map[string]any{"confirm": false})
	if unconfirmed.Code != http.StatusBadRequest || !strings.Contains(unconfirmed.Body.String(), "confirmation_required") {
		t.Fatalf("unconfirmed test status=%d body=%s", unconfirmed.Code, unconfirmed.Body.String())
	}

	var gotKey, gotBase string
	server.tikhubTestConnection = func(_ context.Context, key, baseURL string) error {
		gotKey, gotBase = key, baseURL
		return nil
	}
	tested := callDataAPI(t, server, http.MethodPost, "/api/data-api/test", map[string]any{"confirm": true})
	if tested.Code != http.StatusOK || !strings.Contains(tested.Body.String(), `"active":true`) ||
		strings.Contains(tested.Body.String(), secret) {
		t.Fatalf("successful test status=%d body=%s", tested.Code, tested.Body.String())
	}
	if gotKey != secret || gotBase != tikhub.MainlandBase {
		t.Fatalf("test connection got key/base %q %q", gotKey, gotBase)
	}
}

func TestDataAPIFailedTestStaysInactiveAndCatalogMarksBindingsHonestly(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	put := callDataAPI(t, server, http.MethodPut, "/api/data-api/credential", map[string]any{
		"api_key": "synthetic-tikhub-token-123456",
		"region":  tikhub.RegionGlobal,
	})
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", put.Code, put.Body.String())
	}
	server.tikhubTestConnection = func(context.Context, string, string) error {
		return errors.New("synthetic provider rejection")
	}
	tested := callDataAPI(t, server, http.MethodPost, "/api/data-api/test", map[string]any{"confirm": true})
	if tested.Code != http.StatusBadGateway || !strings.Contains(tested.Body.String(), `"active":false`) ||
		strings.Contains(tested.Body.String(), "synthetic provider rejection") {
		t.Fatalf("failed test status=%d body=%s", tested.Code, tested.Body.String())
	}

	get := callDataAPI(t, server, http.MethodGet, "/api/data-api/config", nil)
	body := get.Body.String()
	for _, expected := range []string{
		`"platform":"douyin"`,
		`"platform":"xhs"`,
		`"platform":"wechat_video"`,
		`"platform":"kuaishou"`,
		`"platform":"wechat_mp"`,
		`"platform":"youtube"`,
		`"platform":"x"`,
		`"capability":"keyword_search"`,
		`"capability":"content_detail"`,
		`"capability":"content_transcript"`,
		`"capability":"creator_profile"`,
		`"capability":"creator_posts"`,
		`"capability":"topic_selection"`,
		`"capability":"benchmark"`,
		`"capability":"circle_analysis"`,
		`/api/v1/wechat_search/v2/fetch_search`,
		`/api/v1/tikhub/user/get_user_info`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("catalog missing %q: %s", expected, body)
		}
	}
	for _, removed := range []string{
		`"platform":"toutiao"`,
		`"platform":"zhihu"`,
		`"platform":"baijiahao"`,
		`"capability":"daily_rank"`,
		`"capability":"comments"`,
	} {
		if strings.Contains(body, removed) {
			t.Fatalf("catalog retained removed article platform %q: %s", removed, body)
		}
	}
}

func TestDataAPICallRequiresConfirmationAndPersistsResponse(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := tikhub.SaveProviderCredential(dataDir, "synthetic-tikhub-token-123456", tikhub.RegionMainland); err != nil {
		t.Fatal(err)
	}
	if err := tikhub.MarkProviderTest(dataDir, true, ""); err != nil {
		t.Fatal(err)
	}

	unconfirmed := callDataAPI(t, server, http.MethodPost, "/api/data-api/calls", map[string]any{
		"endpoint_id": "xhs.note_search",
		"confirm":     false,
		"params":      map[string]any{"keyword": "AI工具"},
	})
	if unconfirmed.Code != http.StatusBadRequest || !strings.Contains(unconfirmed.Body.String(), "confirmation_required") {
		t.Fatalf("unconfirmed status=%d body=%s", unconfirmed.Code, unconfirmed.Body.String())
	}

	server.tikhubCallEndpoint = func(_ context.Context, endpoint tikhub.EndpointDefinition, params map[string]any) (map[string]any, error) {
		if endpoint.ID != "xhs.note_search" || params["keyword"] != "AI工具" || params["page"] != 1 {
			t.Fatalf("unexpected call %s %#v", endpoint.ID, params)
		}
		return map[string]any{"code": json.Number("200"), "data": map[string]any{"items": []any{map[string]any{"title": "AI工具"}}}}, nil
	}
	called := callDataAPI(t, server, http.MethodPost, "/api/data-api/calls", map[string]any{
		"endpoint_id": "xhs.note_search",
		"confirm":     true,
		"params":      map[string]any{"keyword": "AI工具"},
	})
	if called.Code != http.StatusOK || !strings.Contains(called.Body.String(), `"status":"succeeded"`) ||
		!strings.Contains(called.Body.String(), `"item_count":1`) || !strings.Contains(called.Body.String(), `"raw_path"`) {
		t.Fatalf("call status=%d body=%s", called.Code, called.Body.String())
	}
	history := callDataAPI(t, server, http.MethodGet, "/api/data-api/calls?limit=10", nil)
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), `"endpoint_id":"xhs.note_search"`) ||
		!strings.Contains(history.Body.String(), `\"keyword\":\"AI工具\"`) {
		t.Fatalf("history status=%d body=%s", history.Code, history.Body.String())
	}
}

func TestDataAPICallPersistsSafeFailureDiagnostics(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := tikhub.SaveProviderCredential(dataDir, "synthetic-tikhub-token-123456", tikhub.RegionMainland); err != nil {
		t.Fatal(err)
	}
	if err := tikhub.MarkProviderTest(dataDir, true, ""); err != nil {
		t.Fatal(err)
	}
	server.tikhubCallEndpoint = func(context.Context, tikhub.EndpointDefinition, map[string]any) (map[string]any, error) {
		return nil, errors.New("synthetic provider rejection that must not reach the UI")
	}

	called := callDataAPI(t, server, http.MethodPost, "/api/data-api/calls", map[string]any{
		"endpoint_id": "douyin.video_search",
		"confirm":     true,
		"params": map[string]any{
			"keyword": "男性情感", "publish_time": "7", "sort_type": "2",
			"filter_duration": "0", "content_type": "0",
		},
	})
	if called.Code != http.StatusBadGateway ||
		!strings.Contains(called.Body.String(), "TikHub request failed before receiving a provider status") ||
		strings.Contains(called.Body.String(), "synthetic provider rejection") {
		t.Fatalf("failed call status=%d body=%s", called.Code, called.Body.String())
	}
	history := callDataAPI(t, server, http.MethodGet, "/api/data-api/calls?limit=10", nil)
	body := history.Body.String()
	if !strings.Contains(body, `"error_code":"provider_request_failed"`) ||
		!strings.Contains(body, `"provider_code":"request_error"`) ||
		!strings.Contains(body, `"provider_message":"TikHub request failed before receiving a provider status"`) ||
		strings.Contains(body, "synthetic provider rejection") {
		t.Fatalf("safe failure details were not persisted: %s", body)
	}
}

func TestDataAPICallPersistsAfterRendererRequestCancellation(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := tikhub.SaveProviderCredential(dataDir, "synthetic-tikhub-token-123456", tikhub.RegionMainland); err != nil {
		t.Fatal(err)
	}
	if err := tikhub.MarkProviderTest(dataDir, true, ""); err != nil {
		t.Fatal(err)
	}
	server.tikhubCallEndpoint = func(ctx context.Context, _ tikhub.EndpointDefinition, _ map[string]any) (map[string]any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return map[string]any{"code": json.Number("200"), "data": map[string]any{"items": []any{map[string]any{"title": "男性情感"}}}}, nil
	}

	input, err := json.Marshal(map[string]any{
		"endpoint_id": "douyin.video_search",
		"confirm":     true,
		"params": map[string]any{
			"keyword": "男性情感", "publish_time": "7", "sort_type": "2",
			"filter_duration": "0", "content_type": "0",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/data-api/calls", bytes.NewReader(input)).WithContext(requestContext)
	request.RemoteAddr = "127.0.0.1:41000"
	request.Host = "127.0.0.1:17321"
	request.Header.Set("Origin", "http://127.0.0.1:17321")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"succeeded"`) {
		t.Fatalf("cancelled renderer request lost the paid collection: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestLiveDouyinTrendRadarCall(t *testing.T) {
	if os.Getenv("ALPHA_NEXUS_LIVE_TIKHUB") != "1" {
		t.Skip("set ALPHA_NEXUS_LIVE_TIKHUB=1 for one explicit paid Douyin call")
	}
	dataDir := filepath.Clean(os.Getenv("ALPHA_NEXUS_LIVE_DATA_DIR"))
	if !filepath.IsAbs(dataDir) {
		t.Fatal("ALPHA_NEXUS_LIVE_DATA_DIR must be an absolute path")
	}
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	called := callDataAPI(t, server, http.MethodPost, "/api/data-api/calls", map[string]any{
		"endpoint_id": "douyin.video_search",
		"confirm":     true,
		"params": map[string]any{
			"keyword":         "AI工具",
			"publish_time":    "7",
			"sort_type":       "2",
			"filter_duration": "0",
			"content_type":    "0",
		},
	})
	if called.Code != http.StatusOK {
		t.Fatalf("live Douyin call status=%d body=%s", called.Code, called.Body.String())
	}
	var result struct {
		ID             int64  `json:"id"`
		Status         string `json:"status"`
		ItemCount      int64  `json:"item_count"`
		ResponseBytes  int64  `json:"response_bytes"`
		RawPath        string `json:"raw_path"`
		ParametersJSON string `json:"parameters_json"`
	}
	if err := json.Unmarshal(called.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID <= 0 || result.Status != "succeeded" || result.ResponseBytes <= 0 || result.RawPath == "" {
		t.Fatalf("incomplete live result: %#v", result)
	}
	if !strings.Contains(result.ParametersJSON, `"publish_time":"7"`) || !strings.Contains(result.ParametersJSON, `"sort_type":"2"`) {
		t.Fatalf("official Douyin options not persisted: %s", result.ParametersJSON)
	}
	root := filepath.Join(dataDir, "tikhub", "calls")
	relative, err := filepath.Rel(root, result.RawPath)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		t.Fatalf("snapshot escaped local call root: %q", result.RawPath)
	}
	info, err := os.Stat(result.RawPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot is not an owner-only regular file: %v mode=%v", err, info)
	}
	t.Logf("Douyin call %d succeeded: items=%d bytes=%d", result.ID, result.ItemCount, result.ResponseBytes)
}
