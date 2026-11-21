package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenMausBotBotListToolReadsAFakeHarness(t *testing.T) {
	harness := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/bots" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bots":[{"id":"b1","name":"DEV"},{"id":"b2","name":"UI"}]}`))
	}))
	defer harness.Close()

	registry := NewRegistry()
	registerOpenMausBotTools(registry)
	result, err := registry.Call(context.Background(), &Runtime{}, "openmausbot.bot.list", map[string]interface{}{
		"base_url": harness.URL,
	})
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Data["count"] != 2 || result.Data["total"] != 2 {
		t.Fatalf("data=%+v, want both bots", result.Data)
	}
	if result.Data["base_url"] != harness.URL {
		t.Fatalf("base_url=%v, want the harness origin", result.Data["base_url"])
	}

	limited, err := registry.Call(context.Background(), &Runtime{}, "openmausbot.bot.list", map[string]interface{}{
		"base_url": harness.URL,
		"limit":    1,
	})
	if err != nil || limited == nil || !limited.OK {
		t.Fatalf("limited=%+v err=%v", limited, err)
	}
	if limited.Data["count"] != 1 || limited.Data["total"] != 2 {
		t.Fatalf("data=%+v, want one shown of two", limited.Data)
	}
}

func TestOpenMausBotBotListToolReportsAuthRequired(t *testing.T) {
	harness := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("must come from the desktop app or a paired device"))
	}))
	defer harness.Close()

	registry := NewRegistry()
	registerOpenMausBotTools(registry)
	result, err := registry.Call(context.Background(), &Runtime{}, "openmausbot.bot.list", map[string]interface{}{
		"base_url": harness.URL,
	})
	if err == nil {
		t.Fatal("call succeeded against a refusing harness")
	}
	if result == nil || result.OK {
		t.Fatalf("result=%+v, want a refusal envelope", result)
	}
	if result.Error != "OPENMAUSBOT_AUTH_REQUIRED" {
		t.Fatalf("error=%q, want OPENMAUSBOT_AUTH_REQUIRED", result.Error)
	}
}

func TestOpenMausBotBotListIsRegisteredReadOnly(t *testing.T) {
	def, ok := NewRegistry().tools["openmausbot.bot.list"]
	if ok {
		t.Fatalf("fresh registry already has the tool: %+v", def)
	}
	registry := NewRegistry()
	registerOpenMausBotTools(registry)
	def, ok = registry.tools["openmausbot.bot.list"]
	if !ok {
		t.Fatal("tool was not registered")
	}
	if def.Category != "agent" || def.SideEffect != "read" || def.AgentVisible {
		t.Fatalf("def=%+v, want agent/read and not agent-visible", def)
	}
}
