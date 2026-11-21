package agenthub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const dshCatalogPatch = `- id: llm-pi-ai
  name: "@deepseek-ai/dsh-llm-pi-ai"
  config:
    providers:
      agy:
        displayName: Antigravity
        api: openai-completions
        baseURL: http://127.0.0.1:8045/v1
        apiKeyEnv: AGY_API_KEY
        models:
          - id: gemini-3.8-flash
            name: Gemini 3.8 Flash
            contextWindow: 1048576
            maxTokens: 65536
          - id: deepseek-v4.1
            name: DeepSeek V4.1
      bailian:
        displayName: Bailian
        api: openai-completions
        baseURL: https://dashscope.aliyuncs.com/compatible-mode/v1
        apiKeyEnv: BAILIAN_API_KEY
        models:
          - id: qwen3.8-max
            name: Qwen 3.8 Max
- id: agent-default-model
  name: "@deepseek-ai/dsh-agent-default-model"
  config:
    provider: agy
    model: gemini-3.8-flash
`

func writeDshPatch(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".dsh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cordis.patch.yml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestReadDshCatalogMirrorsProvidersAndDefault(t *testing.T) {
	home := writeDshPatch(t, dshCatalogPatch)
	catalog, err := ReadDshCatalog(home)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultProvider != "agy" || catalog.DefaultModel != "gemini-3.8-flash" {
		t.Fatalf("default route = %q/%q", catalog.DefaultProvider, catalog.DefaultModel)
	}
	if len(catalog.Providers) != 2 {
		t.Fatalf("providers = %d", len(catalog.Providers))
	}
	if catalog.Providers[0].ID != "agy" || catalog.Providers[0].Label != "Antigravity" {
		t.Fatalf("first provider = %#v", catalog.Providers[0])
	}
	if catalog.Providers[0].BaseURL != "http://127.0.0.1:8045/v1" || catalog.Providers[0].APIKeyEnv != "AGY_API_KEY" {
		t.Fatalf("provider transport fields = %#v", catalog.Providers[0])
	}
	wantModels := []string{"agy/deepseek-v4.1", "agy/gemini-3.8-flash", "bailian/qwen3.8-max"}
	if !reflect.DeepEqual(catalog.ModelIDs, wantModels) {
		t.Fatalf("model ids = %#v, want %#v", catalog.ModelIDs, wantModels)
	}
}

func TestReadDshCatalogMissingFileIsNotAnError(t *testing.T) {
	catalog, err := ReadDshCatalog(t.TempDir())
	if err != nil {
		t.Fatalf("missing dsh patch must not fail: %v", err)
	}
	if len(catalog.Providers) != 0 || len(catalog.ModelIDs) != 0 {
		t.Fatalf("expected empty catalog, got %#v", catalog)
	}
}

func TestDshRouteFromModelID(t *testing.T) {
	provider, model, ok := dshRouteFromModelID("agy/gemini-3.8-flash")
	if !ok || provider != "agy" || model != "gemini-3.8-flash" {
		t.Fatalf("route = %q/%q ok=%v", provider, model, ok)
	}
	for _, bad := range []string{"", "gemini-3.8-flash", "agy/", "/model", "agy/a/b"} {
		if _, _, ok := dshRouteFromModelID(bad); ok {
			t.Fatalf("%q should not parse as a route", bad)
		}
	}
}

func TestReadDshCatalogPrefersProfilePatch(t *testing.T) {
	home := writeDshPatch(t, dshCatalogPatch)
	profileDir := filepath.Join(home, ".dsh", "profiles", "alpha")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	profilePatch := `- id: llm-pi-ai
  config:
    providers:
      agy:
        displayName: Antigravity (profile)
        api: openai-completions
        baseURL: http://127.0.0.1:9000/v1
        apiKeyEnv: AGY_API_KEY
        models:
          - id: gemini-4-argon
- id: agent-default-model
  config:
    provider: agy
    model: gemini-4-argon
`
	if err := os.WriteFile(filepath.Join(profileDir, "cordis.patch.yml"), []byte(profilePatch), 0o600); err != nil {
		t.Fatal(err)
	}

	catalog, err := ReadDshCatalogForProfile(home, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultModel != "gemini-4-argon" {
		t.Fatalf("profile default model = %q", catalog.DefaultModel)
	}
	if len(catalog.Providers) != 2 {
		t.Fatalf("providers = %d, want merged 2", len(catalog.Providers))
	}
	var agy DshProvider
	for _, provider := range catalog.Providers {
		if provider.ID == "agy" {
			agy = provider
		}
	}
	if agy.Label != "Antigravity (profile)" || agy.BaseURL != "http://127.0.0.1:9000/v1" {
		t.Fatalf("profile provider did not override global: %#v", agy)
	}
	if len(agy.Models) != 1 || agy.Models[0].ID != "gemini-4-argon" {
		t.Fatalf("profile models = %#v", agy.Models)
	}
}

// The SDK streams text as assistant/attempt chunks and ends with turn/end;
// parsing must follow that shape instead of waiting for assistant/message.
func TestDshAttemptAndTurnEndShape(t *testing.T) {
	attemptJSON := `{"turn":1,"step":1,"stream":[{"type":"chunk","chunk":{"type":"text","text":"收到"}},{"type":"chunk","chunk":{"type":"usage","usage":{"totalTokens":3}}}]}`
	var attempt dshAttemptData
	if err := json.Unmarshal([]byte(attemptJSON), &attempt); err != nil {
		t.Fatal(err)
	}
	var text string
	for _, item := range attempt.Stream {
		if item.Chunk.Type == "text" {
			text += item.Chunk.Text
		}
	}
	if text != "收到" {
		t.Fatalf("streamed text = %q", text)
	}

	endJSON := `{"turn":1,"reason":{"kind":"error","error":{"message":"403 forbidden","code":"AUTH"}}}`
	var end dshTurnEndData
	if err := json.Unmarshal([]byte(endJSON), &end); err != nil {
		t.Fatal(err)
	}
	if end.Reason.Kind != "error" || end.Reason.Error.Message != "403 forbidden" {
		t.Fatalf("turn end = %#v", end.Reason)
	}
}
