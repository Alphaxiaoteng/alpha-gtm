package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestLegacyBrowserBatchStopRouteIsUnavailable(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/browsers/stop-batch", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
