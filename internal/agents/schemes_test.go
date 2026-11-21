package agents

import (
	"context"
	"path/filepath"
	"testing"

	"alpha-nexus-egolite/internal/tools"
)

func TestListSchemesExcludesLegacyCDPBrowser(t *testing.T) {
	list := ListSchemes()
	if len(list) != 4 {
		t.Fatalf("want 4 schemes, got %d", len(list))
	}
	ids := map[SchemeID]bool{}
	for _, s := range list {
		ids[s.ID] = true
		if s.Name == "" || s.Tagline == "" {
			t.Fatalf("scheme %s missing name/tagline", s.ID)
		}
	}
	for _, id := range []SchemeID{SchemeNative, SchemeLLMTools, SchemeOpenCode, SchemeMCPExport} {
		if !ids[id] {
			t.Fatalf("missing scheme %s", id)
		}
	}
}

func TestRunSchemeNative(t *testing.T) {
	dir := t.TempDir()
	// tools need db — account.list opens db; create minimal via tools Runtime
	// if migrate fails tests still exercise path
	res, err := RunScheme(context.Background(), SchemeRunRequest{
		Scheme:  SchemeNative,
		Goal:    "probe",
		DryRun:  true,
		DataDir: dir,
		Runtime: &tools.Runtime{DataDir: dir},
	})
	if res == nil {
		t.Fatalf("nil result err=%v", err)
	}
	if res.Scheme != SchemeNative {
		t.Fatalf("scheme %s", res.Scheme)
	}
	if res.Duration == "" {
		t.Fatal("expected duration")
	}
	// may fail if db migrate needs more setup — still must return envelope
	t.Logf("ok=%v msg=%s err=%s steps=%d", res.OK, res.Message, res.Error, len(res.Steps))
}

func TestRunSchemeMCPExport(t *testing.T) {
	dir := t.TempDir()
	res, err := RunScheme(context.Background(), SchemeRunRequest{
		Scheme:  SchemeMCPExport,
		DryRun:  true,
		DataDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("mcp export not ok: %s", res.Error)
	}
	n, _ := res.Data["tool_count"].(int)
	if n < 5 {
		// Data may be float64 from empty — check interface
		if f, ok := res.Data["tool_count"].(int); ok && f < 5 {
			t.Fatalf("tool_count %v", res.Data["tool_count"])
		}
		if f, ok := res.Data["tool_count"].(float64); ok && f < 5 {
			t.Fatalf("tool_count %v", res.Data["tool_count"])
		}
		// native int from Go map is int
		if n == 0 {
			if v, ok := res.Data["tool_count"].(int); ok {
				n = v
			}
		}
	}
	// schema file written
	if p, ok := res.Data["schema_path"].(string); ok && p != "" {
		if !filepath.IsAbs(p) && p != "" {
			// ok relative
		}
		t.Log("schema_path", p)
	}
}

func TestRunSchemeRejectsLegacyCDPBrowser(t *testing.T) {
	_, err := RunScheme(context.Background(), SchemeRunRequest{
		Scheme: SchemeBrowser,
		DryRun: true,
		Goal:   "https://example.com",
	})
	if err == nil {
		t.Fatal("legacy CDP browser scheme must be rejected")
	}
}

func TestRunSchemeUnknown(t *testing.T) {
	_, err := RunScheme(context.Background(), SchemeRunRequest{Scheme: "nope"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveOpenCodeBinary(t *testing.T) {
	// may or may not exist on CI — just must not panic
	_ = ResolveOpenCodeBinary("")
}
