// Package agentcfg stores verified Agent API model preferences under data dir.
package agentcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	ProviderTypeVolcanoArk = "volcano_ark"
	ProviderTypeCustom     = "custom"

	ProtocolResponses       = "responses"
	ProtocolChatCompletions = "chat_completions"

	// DoubaoModelID remains the runtime fallback identifier. It is not added to
	// the enabled model list until a real Ark Responses probe succeeds.
	DoubaoModelID    = "doubao-seed-2-0-pro-260215"
	DoubaoModelLabel = "豆包 API · Seed 2.0 Pro"

	// These are candidate identifiers supplied by the user. Candidate presence
	// is not availability evidence; the UI service must probe each one before
	// passing it to Save.
	DoubaoSeed21TurboModelID = "doubao-seed-2.1-turbo"
	DoubaoSeed21ProModelID   = "doubao-seed-2.1-pro"
	DoubaoSeed20LiteModelID  = "doubao-seed-2-0-lite-260428"
)

var candidateModels = []ModelEntry{
	{ID: DoubaoSeed21TurboModelID, Label: "豆包 Seed 2.1 Turbo", Source: "volcano_ark"},
	{ID: DoubaoSeed21ProModelID, Label: "豆包 Seed 2.1 Pro", Source: "volcano_ark"},
	{ID: DoubaoModelID, Label: DoubaoModelLabel, Source: "volcano_ark"},
	{ID: DoubaoSeed20LiteModelID, Label: "豆包 Seed 2.0 Lite", Source: "volcano_ark"},
}

// ModelEntry remains in the API response for backward-compatible clients.
type ModelEntry struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Enabled     bool   `json:"enabled"`
	Source      string `json:"source,omitempty"`
	ValidatedAt string `json:"validated_at,omitempty"`
}

// Config is ~/.alpha-nexus/agent.json. It stores presentation preferences, not secrets.
type Config struct {
	ProviderType  string       `json:"provider_type"`
	ProviderName  string       `json:"provider_name,omitempty"`
	BaseURL       string       `json:"base_url,omitempty"`
	Protocol      string       `json:"protocol"`
	Engine        string       `json:"engine"`
	DefaultModel  string       `json:"default_model"`
	DefaultEffort string       `json:"default_effort"`
	Models        []ModelEntry `json:"models"`
	UpdatedAt     string       `json:"updated_at,omitempty"`
}

var (
	mu    sync.Mutex
	cache *Config
)

func path(dataDir string) string {
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".alpha-nexus")
	}
	return filepath.Join(dataDir, "agent.json")
}

// Load reads agent.json. Unknown or disabled model entries are discarded.
func Load(dataDir string) (*Config, error) {
	mu.Lock()
	defer mu.Unlock()
	p := path(dataDir)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := defaultConfig()
			_ = writeUnlocked(dataDir, cfg)
			cache = cfg
			return clone(cfg), nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	normalize(&cfg)
	cache = &cfg
	return clone(&cfg), nil
}

// Save writes presentation preferences. Callers must pass only models that were
// successfully probed; this package additionally rejects anything outside the
// explicit candidate allowlist.
func Save(dataDir string, cfg *Config) error {
	mu.Lock()
	defer mu.Unlock()
	normalize(cfg)
	cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := writeUnlocked(dataDir, cfg); err != nil {
		return err
	}
	cache = clone(cfg)
	return nil
}

func writeUnlocked(dataDir string, cfg *Config) error {
	p := path(dataDir)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

func normalize(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.ProviderType = strings.ToLower(strings.TrimSpace(cfg.ProviderType))
	if cfg.ProviderType != ProviderTypeCustom {
		cfg.ProviderType = ProviderTypeVolcanoArk
	}
	cfg.ProviderName = strings.TrimSpace(cfg.ProviderName)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Protocol = strings.ToLower(strings.TrimSpace(cfg.Protocol))
	if cfg.DefaultEffort == "" {
		cfg.DefaultEffort = "medium"
	}
	if cfg.ProviderType == ProviderTypeCustom {
		normalizeCustom(cfg)
		return
	}
	cfg.ProviderName = "火山方舟"
	cfg.BaseURL = ""
	cfg.Protocol = ProtocolResponses
	cfg.Engine = "doubao_api"
	normalized := make([]ModelEntry, 0, len(cfg.Models))
	seen := make(map[string]struct{}, len(cfg.Models))
	for _, model := range cfg.Models {
		if !model.Enabled || !validValidationTime(model.ValidatedAt) {
			continue
		}
		candidate, ok := CandidateModel(model.ID)
		if !ok {
			continue
		}
		if _, exists := seen[candidate.ID]; exists {
			continue
		}
		seen[candidate.ID] = struct{}{}
		candidate.Enabled = true
		candidate.ValidatedAt = model.ValidatedAt
		normalized = append(normalized, candidate)
	}
	cfg.Models = normalized
	if candidate, ok := CandidateModel(cfg.DefaultModel); ok && ModelEnabled(cfg, candidate.ID) {
		cfg.DefaultModel = candidate.ID
	} else {
		cfg.DefaultModel = ""
		if len(cfg.Models) > 0 {
			cfg.DefaultModel = cfg.Models[0].ID
		}
	}
}

func normalizeCustom(cfg *Config) {
	cfg.Engine = "opencode_llm"
	if cfg.ProviderName == "" {
		cfg.ProviderName = "自定义 LLM"
	}
	if cfg.Protocol != ProtocolResponses {
		cfg.Protocol = ProtocolChatCompletions
	}
	normalized := make([]ModelEntry, 0, len(cfg.Models))
	seen := make(map[string]struct{}, len(cfg.Models))
	for _, model := range cfg.Models {
		model.ID = strings.TrimSpace(model.ID)
		if !model.Enabled || !validValidationTime(model.ValidatedAt) || !validCustomModelID(model.ID) {
			continue
		}
		if _, exists := seen[model.ID]; exists {
			continue
		}
		seen[model.ID] = struct{}{}
		model.Label = strings.TrimSpace(model.Label)
		if model.Label == "" {
			model.Label = model.ID
		}
		model.Source = ProviderTypeCustom
		normalized = append(normalized, model)
	}
	cfg.Models = normalized
	if !containsModel(cfg.Models, cfg.DefaultModel) {
		cfg.DefaultModel = ""
		if len(cfg.Models) > 0 {
			cfg.DefaultModel = cfg.Models[0].ID
		}
	}
}

func containsModel(models []ModelEntry, id string) bool {
	id = strings.TrimSpace(id)
	for _, model := range models {
		if model.ID == id {
			return true
		}
	}
	return false
}

func validCustomModelID(model string) bool {
	if model == "" || len(model) > 96 {
		return false
	}
	for _, value := range model {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' {
			continue
		}
		switch value {
		case '.', '_', '-', '/', ':':
			continue
		default:
			return false
		}
	}
	return true
}

func clone(c *Config) *Config {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Models = append([]ModelEntry(nil), c.Models...)
	return &cp
}

func defaultConfig() *Config {
	cfg := &Config{DefaultEffort: "medium", UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	normalize(cfg)
	return cfg
}

func validValidationTime(value string) bool {
	_, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	return err == nil
}

// CandidateModelIDs returns a copy of the fixed unverified candidate allowlist.
func CandidateModelIDs() []string {
	ids := make([]string, 0, len(candidateModels))
	for _, model := range candidateModels {
		ids = append(ids, model.ID)
	}
	return ids
}

// CandidateModel resolves a candidate case-insensitively while returning the
// canonical identifier and server-controlled display metadata.
func CandidateModel(id string) (ModelEntry, bool) {
	id = strings.TrimSpace(id)
	for _, model := range candidateModels {
		if strings.EqualFold(model.ID, id) {
			return model, true
		}
	}
	return ModelEntry{}, false
}

// ModelEnabled reports whether a normalized configuration contains the model.
func ModelEnabled(cfg *Config, id string) bool {
	if cfg == nil {
		return false
	}
	candidate, ok := CandidateModel(id)
	if !ok {
		return false
	}
	for _, model := range cfg.Models {
		if model.Enabled && model.ID == candidate.ID {
			return true
		}
	}
	return false
}

// EnabledModels returns a copy of the successfully saved model list.
func EnabledModels(cfg *Config) []ModelEntry {
	if cfg == nil {
		return nil
	}
	return append([]ModelEntry(nil), cfg.Models...)
}
