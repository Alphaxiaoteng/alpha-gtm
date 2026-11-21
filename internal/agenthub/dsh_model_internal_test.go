package agenthub

import (
	"os"
	"path/filepath"
	"testing"
)

// writeDshHome builds a temporary home directory containing a DSH settings
// file at one of the candidate paths, so the route lookup is exercised without
// depending on the machine's real ~/.dsh contents.
func writeDshHome(t *testing.T, subpath, contents string) string {
	t.Helper()
	home := t.TempDir()
	target := filepath.Join(home, subpath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", target, err)
	}
	return home
}

const dshPatchYAML = `- id: permission
  name: "@deepseek-ai/dsh-permission-presets"
  config:
    defaultPreset: danger-full-access
- id: agent-default-model
  name: "@deepseek-ai/dsh-agent-default-model"
  config:
    provider: agy
    model: gemini-3.8-flash
`

const dshSettingsYAML = `agent-default-model:
  provider: agy
  model: claude-opus-5.5
`

// TestDshDefaultRouteParsesPatchYAML covers the cordis.patch.yml layout, which
// is a top-level array of id-targeted patch entries.
func TestDshDefaultRouteParsesPatchYAML(t *testing.T) {
	home := writeDshHome(t, filepath.Join(".dsh", "cordis.patch.yml"), dshPatchYAML)
	provider, model := dshDefaultRouteFrom(home)
	if provider != "agy" || model != "gemini-3.8-flash" {
		t.Fatalf("provider=%q model=%q, want agy/gemini-3.8-flash", provider, model)
	}
}

// TestDshDefaultRouteParsesSettingsYAML covers the settings.yaml layout, which
// uses a top-level agent-default-model mapping.
func TestDshDefaultRouteParsesSettingsYAML(t *testing.T) {
	home := writeDshHome(t, filepath.Join(".dsh", "settings.yaml"), dshSettingsYAML)
	provider, model := dshDefaultRouteFrom(home)
	if provider != "agy" || model != "claude-opus-5.5" {
		t.Fatalf("provider=%q model=%q, want agy/claude-opus-5.5", provider, model)
	}
}

// TestDshDefaultRoutePrefersProfilePatch verifies the documented precedence:
// the alpha profile patch wins over the global patch and settings.yaml.
func TestDshDefaultRoutePrefersProfilePatch(t *testing.T) {
	home := t.TempDir()
	profilePath := filepath.Join(home, ".dsh", "profiles", "alpha", "cordis.patch.yml")
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(profilePath, []byte(dshPatchYAML), 0o644); err != nil {
		t.Fatalf("write profile patch: %v", err)
	}
	// A conflicting global patch must lose to the profile patch.
	conflicting := `- id: agent-default-model
  name: "@deepseek-ai/dsh-agent-default-model"
  config:
    provider: other
    model: should-not-win
`
	globalPath := filepath.Join(home, ".dsh", "cordis.patch.yml")
	if err := os.WriteFile(globalPath, []byte(conflicting), 0o644); err != nil {
		t.Fatalf("write global patch: %v", err)
	}
	provider, model := dshDefaultRouteFrom(home)
	if provider != "agy" || model != "gemini-3.8-flash" {
		t.Fatalf("provider=%q model=%q, want profile patch agy/gemini-3.8-flash", provider, model)
	}
}

// TestDshDefaultRouteEmptyWithoutConfig guards the no-configuration fallback:
// an empty home must not invent a route.
func TestDshDefaultRouteEmptyWithoutConfig(t *testing.T) {
	provider, model := dshDefaultRouteFrom(t.TempDir())
	if provider != "" || model != "" {
		t.Fatalf("provider=%q model=%q, want empty route without config", provider, model)
	}
}

// TestDshDefaultModelParsesSettings keeps the original machine-level probe but
// makes the machine dependency explicit. On a machine without ~/.dsh config the
// lookup legitimately returns nothing, which must not fail the suite.
func TestDshDefaultModelParsesSettings(t *testing.T) {
	if os.Getenv("DSH_REAL_HOME_PROBE") != "1" {
		t.Skip("set DSH_REAL_HOME_PROBE=1 to probe the machine's real ~/.dsh route")
	}
	model := dshDefaultModel()
	t.Logf("parsed model: %q", model)
	if model == "" {
		t.Fatal("expected agent-default-model.model from ~/.dsh/settings.yaml")
	}
}
