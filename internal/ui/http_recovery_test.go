package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestHTTPPanicRecoveryReturnsSafeErrorAndEmitsDiagnostic(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	emitter := &captureObserverEmitter{}
	server.toolRuntime.Events = emitter
	server.mux.HandleFunc("GET /panic-fixture", func(http.ResponseWriter, *http.Request) {
		panic("private panic detail")
	})

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic-fixture", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	requestID := response.Header().Get("X-Request-ID")
	if requestID == "" || !strings.Contains(response.Body.String(), `"errorCode":"INTERNAL_ERROR"`) {
		t.Fatalf("request_id=%q body=%s", requestID, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private panic detail") {
		t.Fatalf("panic detail escaped in response: %s", response.Body.String())
	}
	if len(emitter.events) != 1 {
		t.Fatalf("events=%+v", emitter.events)
	}
	event := emitter.events[0]
	if event.Component != "ui.http" || event.Name != "request.panic" || event.RequestID != requestID ||
		event.ErrorCode != "INTERNAL_ERROR" || event.HTTPStatus != http.StatusInternalServerError ||
		!strings.HasPrefix(event.TaskID, "panic-") || len(event.TaskID) != len("panic-")+12 {
		t.Fatalf("event=%+v", event)
	}
	if strings.Contains(fmt.Sprintf("%+v", event), "private panic detail") {
		t.Fatalf("panic diagnostic leaked detail: %+v", event)
	}
}

func TestHTTPDiagnosticsRecordMutationsAndErrorsWithoutURLOrBody(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	emitter := &captureObserverEmitter{}
	server.toolRuntime.Events = emitter
	server.mux.HandleFunc("POST /api/diagnostic-fixture/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server.mux.HandleFunc("GET /api/diagnostic-health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server.mux.HandleFunc("GET /api/diagnostic-error", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "private failure detail", http.StatusBadGateway)
	})

	mutation := httptest.NewRequest(http.MethodPost, "/api/diagnostic-fixture/42?token=private-query", strings.NewReader("password=private-body"))
	mutationResponse := httptest.NewRecorder()
	server.ServeHTTP(mutationResponse, mutation)
	if mutationResponse.Code != http.StatusNoContent {
		t.Fatalf("mutation status=%d", mutationResponse.Code)
	}

	healthResponse := httptest.NewRecorder()
	server.ServeHTTP(healthResponse, httptest.NewRequest(http.MethodGet, "/api/diagnostic-health", nil))

	errorResponse := httptest.NewRecorder()
	server.ServeHTTP(errorResponse, httptest.NewRequest(http.MethodGet, "/api/diagnostic-error?secret=private-error-query", nil))

	if len(emitter.events) != 2 {
		t.Fatalf("events=%+v", emitter.events)
	}
	mutationEvent, errorEvent := emitter.events[0], emitter.events[1]
	if mutationEvent.Name != "request.completed" || mutationEvent.Step != "POST.api.diagnostic-fixture.id" ||
		mutationEvent.Status != "succeeded" || mutationEvent.HTTPStatus != http.StatusNoContent {
		t.Fatalf("mutation event=%+v", mutationEvent)
	}
	if errorEvent.Name != "request.completed" || errorEvent.Step != "GET.api.diagnostic-error" ||
		errorEvent.Status != "failed" || errorEvent.ErrorCode != "HTTP_SERVER_ERROR" || errorEvent.HTTPStatus != http.StatusBadGateway {
		t.Fatalf("error event=%+v", errorEvent)
	}
	encoded := fmt.Sprintf("%+v", emitter.events)
	for _, forbidden := range []string{"private-query", "private-body", "private-error-query", "password", "token"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("diagnostic event leaked %q: %s", forbidden, encoded)
		}
	}
}
