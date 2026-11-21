package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func newMobileTestServer(t *testing.T) *Server {
	t.Helper()
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server
}

// 手机端摘要必须只返回聚合数字，不能带出 profile 路径、CDP 地址或凭据引用。
func TestMobileSummaryIsAggregateOnly(t *testing.T) {
	server := newMobileTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/mobile/summary", nil)
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"accounts", "contents", "publish", "notifications", "metrics", "generated_at"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("summary missing %q: %s", key, response.Body.String())
		}
	}
	body := response.Body.String()
	for _, forbidden := range []string{"profile_dir", "cookie", "password", "cdp_url", "ref_key", "account_identifier"} {
		if jsonContainsKey(body, forbidden) {
			t.Fatalf("mobile summary leaked %q: %s", forbidden, body)
		}
	}
}

func jsonContainsKey(body, key string) bool {
	return len(body) > 0 && (containsFold(body, `"`+key+`"`))
}

func containsFold(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// 指标没有任何已验证快照时必须明确报 available=false，不能让手机把「没采到」
// 显示成真实的 0。
func TestMobileSummaryReportsMetricsUnavailableWhenEmpty(t *testing.T) {
	server := newMobileTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/mobile/summary", nil)
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	var payload struct {
		Metrics struct {
			Available bool `json:"available"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Metrics.Available {
		t.Fatalf("empty database must report metrics unavailable: %s", response.Body.String())
	}
}

// 手机端接口只接受 JSON：有人拿浏览器直连 /api/mobile/* 时必须被挡掉，
// 页面只能由 mobile serve 托管。
func TestMobileAPIsRejectBrowserNavigation(t *testing.T) {
	server := newMobileTestServer(t)
	for _, path := range []string{"/api/mobile/summary", "/api/mobile/accounts", "/api/mobile/notifications"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept", "text/html,application/xhtml+xml")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d want 403", path, response.Code)
		}
	}
}

// 手机端不得提供任何写入口：POST 必须被拒绝。
func TestMobileAPIsAreReadOnly(t *testing.T) {
	server := newMobileTestServer(t)
	for _, path := range []string{"/api/mobile/summary", "/api/mobile/accounts", "/api/mobile/notifications"} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Set("Accept", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed && response.Code != http.StatusNotFound {
			t.Fatalf("POST %s status=%d want 405/404", path, response.Code)
		}
	}
}

// 账号投影只带「平台、名字、登录态」，不带内部标识。
func TestMobileAccountsProjectionOmitsInternals(t *testing.T) {
	server := newMobileTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/mobile/accounts", nil)
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, forbidden := range []string{"avatar_sha256", "account_identifier", "manager_addr", "profile"} {
		if jsonContainsKey(body, forbidden) {
			t.Fatalf("mobile accounts leaked %q: %s", forbidden, body)
		}
	}
}
