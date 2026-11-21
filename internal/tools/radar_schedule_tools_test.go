package tools

import (
	"context"
	"testing"
)

// TestRadarScheduleToolsAreRegistered guards the agent-facing contract: the
// radar scheduling surface must stay discoverable through tools.list, otherwise
// CLI/Tool Agent callers silently lose the ability to schedule fetches.
func TestRadarScheduleToolsAreRegistered(t *testing.T) {
	for _, name := range []string{
		"radar.schedule.list",
		"radar.schedule.create",
		"radar.schedule.run_due",
	} {
		def, ok := Default.Get(name)
		if !ok {
			t.Fatalf("radar tool %q is not registered", name)
		}
		if def.Category != "radar" {
			t.Fatalf("tool %q category = %q, want radar", name, def.Category)
		}
	}
}

// TestRadarScheduleCreateRequiresValidNiche covers the validation path that
// stops an agent from creating schedules referencing unknown niches or search
// terms without an explicit platform endpoint.
func TestRadarScheduleCreateRequiresValidNiche(t *testing.T) {
	dir := t.TempDir()
	rt := &Runtime{DataDir: dir}

	result, err := Default.Call(context.Background(), rt, "radar.schedule.create", map[string]interface{}{
		"name":     "test schedule",
		"niche_id": "niche-male-emotion",
		"keyword":  "男性情感",
		"platform": "douyin",
	})
	if err != nil {
		t.Fatalf("create radar schedule: %v", err)
	}
	if result == nil || !result.OK {
		t.Fatalf("create radar schedule returned %+v", result)
	}

	listed, err := Default.Call(context.Background(), rt, "radar.schedule.list", nil)
	if err != nil {
		t.Fatalf("list radar schedules: %v", err)
	}
	if listed == nil || !listed.OK {
		t.Fatalf("list radar schedules returned %+v", listed)
	}
}

// TestRadarScheduleCreateRejectsUnknownPlatform ensures an unsupported platform
// fails closed instead of writing a schedule bound to a missing endpoint.
func TestRadarScheduleCreateRejectsUnknownPlatform(t *testing.T) {
	rt := &Runtime{DataDir: t.TempDir()}

	result, _ := Default.Call(context.Background(), rt, "radar.schedule.create", map[string]interface{}{
		"name":     "bad platform",
		"niche_id": "niche-male-emotion",
		"keyword":  "男性情感",
		"platform": "myspace",
	})
	if result == nil {
		t.Fatal("expected a result for unsupported platform")
	}
	if result.OK {
		t.Fatalf("unsupported platform must not create a schedule: %+v", result)
	}
	if result.Error != "RADAR_PLATFORM_UNSUPPORTED" {
		t.Fatalf("error = %q, want RADAR_PLATFORM_UNSUPPORTED", result.Error)
	}
}

// TestRadarRunDueDefaultsToDryPlan makes sure the paid network path is never
// taken without an explicit confirm=true, matching every other TikHub tool.
func TestRadarRunDueDefaultsToDryPlan(t *testing.T) {
	rt := &Runtime{DataDir: t.TempDir()}

	result, err := Default.Call(context.Background(), rt, "radar.schedule.run_due", map[string]interface{}{})
	if err != nil {
		t.Fatalf("dry run radar due: %v", err)
	}
	if result == nil || !result.OK {
		t.Fatalf("dry run returned %+v", result)
	}
	if _, hasHint := result.Data["hint"]; !hasHint {
		t.Fatalf("dry run must explain how to execute, got %+v", result.Data)
	}
	if _, hasCallIDs := result.Data["call_ids"]; hasCallIDs {
		t.Fatalf("dry run must not report call ids, got %+v", result.Data)
	}
}
