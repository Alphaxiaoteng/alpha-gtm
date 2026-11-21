package tools

import (
	"context"
	"testing"
)

func TestAgentSurfaceEveryWorkspaceHasReadAndApprovedAction(t *testing.T) {
	defs, err := Default.AgentList()
	if err != nil {
		t.Fatal(err)
	}
	type coverage struct{ read, approved bool }
	got := map[string]coverage{}
	for _, def := range defs {
		item := got[def.AgentWorkspace]
		if def.AgentRisk == AgentRiskL0 {
			item.read = true
		} else {
			item.approved = true
		}
		got[def.AgentWorkspace] = item
	}
	for _, workspace := range []string{"browser", "content", "dashboard", "publish", "shop"} {
		item := got[workspace]
		if !item.read || !item.approved {
			t.Errorf("workspace %s coverage=%+v", workspace, item)
		}
	}
}

func TestAgentSurfaceExposesShopReadAndApprovedAction(t *testing.T) {
	for _, want := range []struct {
		name string
		risk AgentRisk
	}{
		{name: "shop.listing.get", risk: AgentRiskL0},
		{name: "shop.listing.prepare", risk: AgentRiskL3},
	} {
		def, ok := Default.Get(want.name)
		if !ok || !def.AgentVisible || def.AgentWorkspace != "shop" || def.AgentRisk != want.risk {
			t.Fatalf("%s Agent metadata=%+v", want.name, def)
		}
	}
}

func TestAgentSurfaceExposesApprovedLocalMediaImport(t *testing.T) {
	def, ok := Default.Get("media.import")
	if !ok {
		t.Fatal("media.import is not registered")
	}
	if !def.AgentVisible || def.AgentWorkspace != "content" || def.AgentRisk != AgentRiskL2 {
		t.Fatalf("media.import Agent metadata=%+v", def)
	}
	if def.AgentIdempotency != "content-hash" {
		t.Fatalf("media.import idempotency=%q", def.AgentIdempotency)
	}
}

func TestAgentSurfaceExposesOnlyTopLevelFrozenPublishPlanControls(t *testing.T) {
	for _, want := range []struct {
		name string
		risk AgentRisk
	}{
		{name: "publish.plan.list", risk: AgentRiskL0},
		{name: "publish.plan.freeze", risk: AgentRiskL3},
		{name: "publish.plan.prepare", risk: AgentRiskL3},
		{name: "publish.plan.submit", risk: AgentRiskL3},
	} {
		def, ok := Default.Get(want.name)
		if !ok || !def.AgentVisible || def.AgentWorkspace != "publish" || def.AgentRisk != want.risk {
			t.Fatalf("%s Agent metadata=%+v", want.name, def)
		}
	}
	for _, name := range []string{"skill.run", "social.publish-prepare", "social.publish-submit-approved"} {
		def, ok := Default.Get(name)
		if !ok {
			t.Fatalf("missing non-Agent publish wrapper %q", name)
		}
		if def.AgentVisible {
			t.Fatalf("publish wrapper %q must not be exposed to Agent: %+v", name, def)
		}
	}
}

func TestAgentSurfaceDoesNotExposeLegacyPublishRun(t *testing.T) {
	def, ok := Default.Get("publish.run")
	if !ok {
		t.Fatal("publish.run dry-run compatibility tool is not registered")
	}
	if def.AgentVisible {
		t.Fatalf("legacy publish.run must not be exposed to Agent: %+v", def)
	}
}

func TestAgentSettingsAllowlistAndVersionConflict(t *testing.T) {
	rt := &Runtime{DataDir: t.TempDir()}
	result, err := Default.Call(context.Background(), rt, "settings.patch", map[string]interface{}{
		"changes":  map[string]interface{}{"ui.theme": "dark", "ego.max_concurrency": float64(2)},
		"versions": map[string]interface{}{"ui.theme": float64(0), "ego.max_concurrency": float64(0)},
	})
	if err != nil || !result.OK {
		t.Fatalf("settings.patch: result=%+v err=%v", result, err)
	}
	if _, err := Default.Call(context.Background(), rt, "settings.patch", map[string]interface{}{
		"changes":  map[string]interface{}{"ui.theme": "light"},
		"versions": map[string]interface{}{"ui.theme": float64(0)},
	}); err == nil {
		t.Fatal("stale setting version must be rejected")
	}
	if _, err := Default.Call(context.Background(), rt, "settings.patch", map[string]interface{}{
		"changes":  map[string]interface{}{"agent_api_key": "secret"},
		"versions": map[string]interface{}{"agent_api_key": float64(0)},
	}); err == nil {
		t.Fatal("secret setting must not be allowlisted")
	}
}

func TestWorkflowRiskUsesHighestNestedStep(t *testing.T) {
	risk, err := Default.AgentRiskFor("workflow.run", map[string]interface{}{"steps": []interface{}{
		map[string]interface{}{"tool": "dashboard.summary", "args": map[string]interface{}{}},
		map[string]interface{}{"tool": "ego.space.login", "args": map[string]interface{}{"account_id": float64(1)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if risk != AgentRiskL2 {
		t.Fatalf("risk=%s want L2", risk)
	}
}
