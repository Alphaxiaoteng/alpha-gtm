package tools

import (
	"context"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestAgentBotListToolReadsRoster(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO agent_bots(id,name,role,model,avatar,capabilities_json,created_at,updated_at)
		VALUES('bot_t1','选题助手','选题','agy/gemini-3.8-flash','📕','[]','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	runtime := &Runtime{Database: database}
	result, err := runAgentBotList(context.Background(), runtime, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result=%+v", result)
	}
	bots, _ := result.Data["bots"].([]map[string]interface{})
	if len(bots) != 1 || bots[0]["name"] != "选题助手" {
		t.Fatalf("bots=%#v", result.Data["bots"])
	}
}

func TestAgentBotListToolOpensDataDirOnDemand(t *testing.T) {
	// A one-shot CLI runtime has no Database; the tool must open DataDir.
	// Point it at a path that cannot be a directory to force the error branch.
	if _, err := runAgentBotList(context.Background(), &Runtime{DataDir: "/dev/null/blocked"}, nil); err == nil {
		t.Fatal("expected NO_DATABASE error for an unopenable data dir")
	}
}

func TestAgentBotListToolIsRegistered(t *testing.T) {
	registry := NewRegistry()
	registerBotTools(registry)
	def, ok := registry.Get("agent.bot.list")
	if !ok {
		t.Fatal("agent.bot.list is not registered")
	}
	if def.SideEffect != "read" {
		t.Fatalf("side effect = %q, want read", def.SideEffect)
	}
	if def.Category != "agent" {
		t.Fatalf("category = %q", def.Category)
	}
}
