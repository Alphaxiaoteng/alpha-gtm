package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTerminalProbeOpenCodeBinaryStaysUnverified(t *testing.T) {
	probe := terminalProbeForUI(map[string]any{
		"opencode": map[string]any{
			"binary":    "/tmp/fake-opencode",
			"available": true,
		},
	})
	openCode, ok := probe["opencode"].(map[string]any)
	if !ok {
		t.Fatalf("opencode probe type = %T", probe["opencode"])
	}
	if openCode["available"] != false || openCode["ready"] != false {
		t.Fatalf("binary discovery must not be ready/available: %#v", openCode)
	}
	if openCode["availability"] != openCodeAvailableUnverified {
		t.Fatalf("availability = %q, want %q", openCode["availability"], openCodeAvailableUnverified)
	}
}

func TestTerminalProbeOpenCodeMissing(t *testing.T) {
	probe := terminalProbeForUI(map[string]any{
		"opencode": map[string]any{"binary": "", "available": false},
	})
	openCode := probe["opencode"].(map[string]any)
	if openCode["availability"] != openCodeNotFound {
		t.Fatalf("availability = %q, want %q", openCode["availability"], openCodeNotFound)
	}
}

func TestTerminalSessionPolicyRejectsUnbrokeredOpenCode(t *testing.T) {
	if code, rejected := rejectUnbrokeredTerminalKind("opencode"); !rejected || code != "OPENCODE_TUI_DISABLED" {
		t.Fatalf("rejected = %v, code = %q", rejected, code)
	}
	if code, rejected := rejectUnbrokeredTerminalKind("claude"); rejected || code != "" {
		t.Fatalf("Claude unexpectedly rejected: rejected = %v, code = %q", rejected, code)
	}
}

func TestTerminalOpenCodeTUIEndpointFailsClosed(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/terminal/sessions", bytes.NewBufferString(`{"kind":"opencode"}`))
	request.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()

	server.handleAPITerminalSessions(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		OK        bool   `json:"ok"`
		ErrorCode string `json:"errorCode"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.OK || response.ErrorCode != "OPENCODE_TUI_DISABLED" {
		t.Fatalf("response = %+v", response)
	}
}
