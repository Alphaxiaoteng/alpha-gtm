package agentcfg

import (
	"reflect"
	"testing"
)

func TestNormalizeRejectsUnverifiedUnknownModels(t *testing.T) {
	cfg := &Config{
		Engine:       "opencode",
		DefaultModel: "proxy/example",
		Models: []ModelEntry{
			{ID: "proxy/example", Label: "Example", Enabled: true},
			{ID: "other/model", Label: "Other", Enabled: true},
		},
	}
	normalize(cfg)

	if cfg.Engine != "doubao_api" || cfg.DefaultModel != "" {
		t.Fatalf("config=%+v", cfg)
	}
	if len(cfg.Models) != 0 {
		t.Fatalf("models=%+v", cfg.Models)
	}
}

const validatedAtFixture = "2026-07-26T10:00:00Z"

func TestDefaultConfigHasNoUnverifiedModels(t *testing.T) {
	cfg := defaultConfig()
	if cfg.DefaultModel != "" || len(cfg.Models) != 0 {
		t.Fatalf("config=%+v", cfg)
	}
}

func TestCandidateModelsAreTheExplicitArkAllowlist(t *testing.T) {
	got := CandidateModelIDs()
	want := []string{
		DoubaoSeed21TurboModelID,
		DoubaoSeed21ProModelID,
		DoubaoModelID,
		DoubaoSeed20LiteModelID,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates=%q want=%q", got, want)
	}
	got[0] = "mutated"
	if CandidateModelIDs()[0] == "mutated" {
		t.Fatal("candidate ids must be returned as a copy")
	}
}

func TestSaveKeepsOnlyEnabledAllowedModelsAndValidDefault(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &Config{
		Engine:       "other",
		DefaultModel: DoubaoSeed21ProModelID,
		Models: []ModelEntry{
			{ID: DoubaoSeed21TurboModelID, Enabled: true, Label: "client label", ValidatedAt: validatedAtFixture},
			{ID: "unknown/model", Enabled: true, ValidatedAt: validatedAtFixture},
			{ID: DoubaoSeed21ProModelID, Enabled: true, ValidatedAt: validatedAtFixture},
			{ID: DoubaoSeed21TurboModelID, Enabled: true, ValidatedAt: validatedAtFixture},
			{ID: DoubaoSeed20LiteModelID, Enabled: false, ValidatedAt: validatedAtFixture},
		},
	}
	if err := Save(dataDir, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Engine != "doubao_api" || loaded.DefaultModel != DoubaoSeed21ProModelID {
		t.Fatalf("config=%+v", loaded)
	}
	if len(loaded.Models) != 2 ||
		loaded.Models[0].ID != DoubaoSeed21TurboModelID ||
		loaded.Models[0].Label != "豆包 Seed 2.1 Turbo" ||
		loaded.Models[1].ID != DoubaoSeed21ProModelID ||
		loaded.Models[1].Label != "豆包 Seed 2.1 Pro" {
		t.Fatalf("models=%+v", loaded.Models)
	}
}

func TestSaveFallsBackToFirstVerifiedModelWhenDefaultWasNotVerified(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &Config{
		DefaultModel: DoubaoSeed21ProModelID,
		Models: []ModelEntry{
			{ID: DoubaoSeed20LiteModelID, Enabled: true, ValidatedAt: validatedAtFixture},
		},
	}
	if err := Save(dataDir, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != DoubaoSeed20LiteModelID {
		t.Fatalf("default=%q", cfg.DefaultModel)
	}
	if !ModelEnabled(cfg, DoubaoSeed20LiteModelID) || ModelEnabled(cfg, DoubaoSeed21ProModelID) {
		t.Fatalf("models=%+v", cfg.Models)
	}
}

func TestSaveDropsLegacyModelWithoutValidationEvidence(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &Config{
		DefaultModel: DoubaoModelID,
		Models:       []ModelEntry{{ID: DoubaoModelID, Enabled: true}},
	}
	if err := Save(dataDir, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "" || len(cfg.Models) != 0 {
		t.Fatalf("unverified legacy model survived: %+v", cfg)
	}
}

func TestSaveKeepsVerifiedCustomOpenAICompatibleModels(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &Config{
		ProviderType:  ProviderTypeCustom,
		ProviderName:  "My Gateway",
		BaseURL:       "https://gateway.example.test/v1",
		Protocol:      ProtocolChatCompletions,
		DefaultModel:  "vendor/coder-model",
		DefaultEffort: "high",
		Models: []ModelEntry{{
			ID:          "vendor/coder-model",
			Label:       "Coder Model",
			Enabled:     true,
			Source:      "custom",
			ValidatedAt: validatedAtFixture,
		}},
	}
	if err := Save(dataDir, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ProviderType != ProviderTypeCustom ||
		loaded.ProviderName != "My Gateway" ||
		loaded.BaseURL != "https://gateway.example.test/v1" ||
		loaded.Protocol != ProtocolChatCompletions ||
		loaded.DefaultModel != "vendor/coder-model" {
		t.Fatalf("custom config=%+v", loaded)
	}
	if len(loaded.Models) != 1 || loaded.Models[0].ID != "vendor/coder-model" {
		t.Fatalf("custom models=%+v", loaded.Models)
	}
}
