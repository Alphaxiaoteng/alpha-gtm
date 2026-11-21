package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"alpha-nexus-egolite/internal/agents"
)

func TestEnrichAgentSchemesOpenCodeBinaryIsNotReadyOrVerified(t *testing.T) {
	schemes := enrichAgentSchemes(agents.ListSchemes(), "/tmp/fake-opencode")
	for _, scheme := range schemes {
		if scheme.ID != agents.SchemeOpenCode {
			continue
		}
		if scheme.Ready {
			t.Fatal("binary discovery alone must not make OpenCode ready")
		}
		if scheme.Availability != openCodeAvailableUnverified {
			t.Fatalf("availability = %q, want %q", scheme.Availability, openCodeAvailableUnverified)
		}
		return
	}
	t.Fatal("OpenCode scheme missing")
}

func TestEnrichAgentSchemesOpenCodeMissing(t *testing.T) {
	schemes := enrichAgentSchemes(agents.ListSchemes(), "")
	for _, scheme := range schemes {
		if scheme.ID != agents.SchemeOpenCode {
			continue
		}
		if scheme.Ready {
			t.Fatal("missing OpenCode binary must not be ready")
		}
		if scheme.Availability != openCodeNotFound {
			t.Fatalf("availability = %q, want %q", scheme.Availability, openCodeNotFound)
		}
		return
	}
	t.Fatal("OpenCode scheme missing")
}

func TestAgentSchemesEndpointNeverAutoVerifiesOpenCode(t *testing.T) {
	server := &Server{}
	recorder := httptest.NewRecorder()
	server.handleAPIAgentSchemes(recorder, httptest.NewRequest(http.MethodGet, "/api/agent/schemes", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Schemes              []agentSchemeAvailability `json:"schemes"`
		OpenCodeAvailability string                    `json:"opencode_availability"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.OpenCodeAvailability != openCodeNotFound && response.OpenCodeAvailability != openCodeAvailableUnverified {
		t.Fatalf("unexpected unpersisted availability %q", response.OpenCodeAvailability)
	}
	for _, scheme := range response.Schemes {
		if scheme.ID == agents.SchemeOpenCode {
			if scheme.Ready || scheme.Availability == openCodeVerifiedReadOnly {
				t.Fatalf("endpoint auto-verified OpenCode: %+v", scheme)
			}
			return
		}
	}
	t.Fatal("OpenCode scheme missing")
}

func TestAgentSchemesRejectsNonGET(t *testing.T) {
	server := &Server{}
	recorder := httptest.NewRecorder()
	server.handleAPIAgentSchemes(recorder, httptest.NewRequest(http.MethodPost, "/api/agent/schemes", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}
