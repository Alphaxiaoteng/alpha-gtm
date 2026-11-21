package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/tools"
)

func TestOperationEventsExposeRegistryTimelineWithoutSensitiveFields(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	result, err := tools.Default.Call(context.Background(), server.toolRuntime, "account.list", nil)
	if err != nil || result == nil || !result.OK {
		t.Fatalf("registry call result=%+v err=%v", result, err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/operations/events", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !containsAll(body, `"event_type":"tool.call.completed"`, `"event_type":"tool.call.started"`, `"step":"account.list"`) {
		t.Fatalf("timeline incomplete=%s", body)
	}
	for _, forbidden := range []string{"raw_json", "cdp_url", "profile_dir", "cookie", "password", "args"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("unsafe timeline field %q in %s", forbidden, body)
		}
	}
	invalid := httptest.NewRequest(http.MethodGet, "/api/operations/events?limit=999", nil)
	invalidResponse := httptest.NewRecorder()
	server.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit status=%d", invalidResponse.Code)
	}
}

func TestServerStartupWritesLifecycleAndDatabaseHealthEvents(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	events, err := server.db.ListOperationEvents(20)
	if err != nil {
		t.Fatal(err)
	}
	var startup, databaseHealth bool
	for _, event := range events {
		switch event.EventType {
		case "startup.completed":
			startup = event.Component == "app.lifecycle" && event.Status == "succeeded"
		case "quick_check.completed":
			databaseHealth = event.Component == "database" && event.Status == "succeeded"
		}
	}
	if !startup || !databaseHealth {
		t.Fatalf("startup=%v database_health=%v events=%+v", startup, databaseHealth, events)
	}
}
