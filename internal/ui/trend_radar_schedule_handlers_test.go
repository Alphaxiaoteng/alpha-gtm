package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/radar"
)

func TestTrendRadarScheduleRequiresConfirmationAndPreservesHistoryOnDelete(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	installed := 0
	server.installRadarWorker = func(context.Context, string) error {
		installed++
		return nil
	}
	schedule := radar.Schedule{
		Name: "AI 工具", NicheID: "ai", EndpointID: "douyin.video_search",
		IntervalMinutes: 360, Enabled: false,
		Params: map[string]any{
			"keyword": "AI工具", "publish_time": "7", "sort_type": "2",
			"filter_duration": "0", "content_type": "0",
		},
	}
	created := callDataAPI(t, server, http.MethodPost, "/api/trend-radar/schedules", map[string]any{
		"schedule": schedule, "confirm": false,
	})
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var stored radar.Schedule
	if err := json.Unmarshal(created.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	stored.Enabled = true
	rejected := callDataAPI(t, server, http.MethodPut, "/api/trend-radar/schedules/"+stored.ID, map[string]any{
		"schedule": stored, "confirm": false,
	})
	if rejected.Code != http.StatusBadRequest || !strings.Contains(rejected.Body.String(), "confirmation") {
		t.Fatalf("unconfirmed enable status=%d body=%s", rejected.Code, rejected.Body.String())
	}
	enabled := callDataAPI(t, server, http.MethodPut, "/api/trend-radar/schedules/"+stored.ID, map[string]any{
		"schedule": stored, "confirm": true,
	})
	if enabled.Code != http.StatusOK || !strings.Contains(enabled.Body.String(), `"enabled":true`) ||
		!strings.Contains(enabled.Body.String(), `"next_run_at"`) {
		t.Fatalf("enable status=%d body=%s", enabled.Code, enabled.Body.String())
	}
	if installed != 1 {
		t.Fatalf("worker installs=%d", installed)
	}
	deleted := callDataAPI(t, server, http.MethodDelete, "/api/trend-radar/schedules/"+stored.ID, nil)
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), `"history_preserved":true`) {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

// TestTrendRadarRunDueRejectsUnsafeRequests pins the safety contract of the
// manual refresh endpoint: it mutates schedule runtime state and may spend
// TikHub credit, so it must only answer same-origin POSTs.
func TestTrendRadarRunDueRejectsUnsafeRequests(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	// Only POST is registered for this path, so a GET must not reach the worker.
	// Go's mux reports 404 here (no GET route exists for the sub-path); accept
	// either 404 or 405 as long as it is not a 2xx.
	getRecorder := callDataAPI(t, server, http.MethodGet, "/api/trend-radar/schedules/run-due", nil)
	if getRecorder.Code < 400 {
		t.Fatalf("GET status=%d, want a 4xx rejection", getRecorder.Code)
	}

	crossSite := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/trend-radar/schedules/run-due", nil)
	request.RemoteAddr = "127.0.0.1:41000"
	request.Host = "127.0.0.1:17321"
	request.Header.Set("Origin", "https://evil.example.com")
	server.ServeHTTP(crossSite, request)
	if crossSite.Code != http.StatusForbidden {
		t.Fatalf("cross-site status=%d, want 403", crossSite.Code)
	}
}

// TestTrendRadarRunDueReportsEmptyDueSet proves the endpoint is wired to the real
// worker: with no schedules configured it must report a zero due pass instead of
// erroring, so the UI can render "nothing scheduled" without a scary failure.
func TestTrendRadarRunDueReportsEmptyDueSet(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	recorder := callDataAPI(t, server, http.MethodPost, "/api/trend-radar/schedules/run-due", map[string]any{})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var summary struct {
		Due       int     `json:"due"`
		Succeeded int     `json:"succeeded"`
		Failed    int     `json:"failed"`
		CallIDs   []int64 `json:"call_ids"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v body=%s", err, recorder.Body.String())
	}
	if summary.Due != 0 || summary.Succeeded != 0 || summary.Failed != 0 {
		t.Fatalf("empty due pass = %+v, want all zero", summary)
	}
	if summary.CallIDs == nil {
		t.Fatal("call_ids must be an empty array, not null, for stable UI decoding")
	}
}
