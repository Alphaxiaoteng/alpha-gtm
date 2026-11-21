package agenthub

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DshModel is one model published by a DSH provider.
type DshModel struct {
	ID            string `json:"id" yaml:"id"`
	Name          string `json:"name,omitempty" yaml:"name"`
	ContextWindow int    `json:"context_window,omitempty" yaml:"contextWindow"`
	MaxTokens     int    `json:"max_tokens,omitempty" yaml:"maxTokens"`
}

// DshProvider is one provider entry from the DSH profile patch.
type DshProvider struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	API       string     `json:"api,omitempty"`
	BaseURL   string     `json:"base_url,omitempty"`
	APIKeyEnv string     `json:"api_key_env,omitempty"`
	Models    []DshModel `json:"models"`
}

// DshCatalog is the DSH agent surface mirrored 1:1 from ~/.dsh.
type DshCatalog struct {
	DefaultProvider string        `json:"default_provider"`
	DefaultModel    string        `json:"default_model"`
	Providers       []DshProvider `json:"providers"`
	// ModelIDs is every "provider/model" route the DSH runtime accepts, which
	// is exactly what the Alpha-Nexus model picker must offer.
	ModelIDs   []string `json:"model_ids"`
	SourcePath string   `json:"source_path"`
	// Warning is set when one patch file is unreadable or not valid YAML. The
	// catalog then reports what could still be parsed instead of failing the
	// whole settings page: a hand-edited typo must not look like "no providers".
	Warning string `json:"warning,omitempty"`
}

type dshPatchProvider struct {
	DisplayName string     `yaml:"displayName"`
	API         string     `yaml:"api"`
	BaseURL     string     `yaml:"baseURL"`
	APIKeyEnv   string     `yaml:"apiKeyEnv"`
	Models      []DshModel `yaml:"models"`
}

type dshPatchEntry struct {
	ID     string `yaml:"id"`
	Config struct {
		Provider  string                      `yaml:"provider"`
		Model     string                      `yaml:"model"`
		Providers map[string]dshPatchProvider `yaml:"providers"`
	} `yaml:"config"`
}

// ReadDshCatalog parses the DSH profile patch that the dsh runtime itself
// loads. An absent file is not an error: callers get an empty catalog and fall
// back to the runtime default route.
func ReadDshCatalog(home string) (DshCatalog, error) {
	return ReadDshCatalogForProfile(home, "")
}

// ReadDshCatalogForProfile mirrors the DSH load order: the profile patch wins
// over the user-level patch. Later entries override earlier ones per provider.
func ReadDshCatalogForProfile(home, profile string) (DshCatalog, error) {
	catalog := DshCatalog{}
	if strings.TrimSpace(home) == "" {
		return catalog, nil
	}
	// Read user-level first, profile last: later entries override earlier ones,
	// and DSH itself gives the profile patch precedence.
	candidates := []string{filepath.Join(home, ".dsh", "cordis.patch.yml")}
	if strings.TrimSpace(profile) != "" {
		candidates = append(candidates, filepath.Join(home, ".dsh", "profiles", profile, "cordis.patch.yml"))
	}

	byID := make(map[string]DshProvider)
	for _, path := range candidates {
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return catalog, err
		}
		catalog.SourcePath = path
		var entries []dshPatchEntry
		if err := yaml.Unmarshal(raw, &entries); err != nil {
			catalog.Warning = path + ": " + err.Error()
			continue
		}
		for _, entry := range entries {
			switch entry.ID {
			case "llm-pi-ai":
				for id, provider := range entry.Config.Providers {
					id = strings.TrimSpace(id)
					if id == "" || len(provider.Models) == 0 {
						continue
					}
					label := strings.TrimSpace(provider.DisplayName)
					if label == "" {
						label = id
					}
					byID[id] = DshProvider{
						ID:        id,
						Label:     label,
						API:       strings.TrimSpace(provider.API),
						BaseURL:   strings.TrimSpace(provider.BaseURL),
						APIKeyEnv: strings.TrimSpace(provider.APIKeyEnv),
						Models:    provider.Models,
					}
				}
			case "agent-default-model":
				catalog.DefaultProvider = strings.TrimSpace(entry.Config.Provider)
				catalog.DefaultModel = strings.TrimSpace(entry.Config.Model)
			}
		}
	}

	catalog.Providers = make([]DshProvider, 0, len(byID))
	for _, provider := range byID {
		catalog.Providers = append(catalog.Providers, provider)
	}
	sort.Slice(catalog.Providers, func(i, j int) bool { return catalog.Providers[i].ID < catalog.Providers[j].ID })
	for _, provider := range catalog.Providers {
		for _, model := range provider.Models {
			if route, ok := dshRouteID(provider.ID, model.ID); ok {
				catalog.ModelIDs = append(catalog.ModelIDs, route)
			}
		}
	}
	sort.Strings(catalog.ModelIDs)
	return catalog, nil
}

// dshRouteID renders the "provider/model" identifier used in the app.
func dshRouteID(provider, model string) (string, bool) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" || model == "" || strings.Contains(model, "/") {
		return "", false
	}
	return provider + "/" + model, true
}

// dshRouteFromModelID splits an app model id back into the DSH provider/model
// pair. A value without a provider segment falls back to the deployment route.
func dshRouteFromModelID(modelID string) (string, string, bool) {
	provider, model, found := strings.Cut(strings.TrimSpace(modelID), "/")
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if !found || provider == "" || model == "" || strings.Contains(model, "/") {
		return "", "", false
	}
	return provider, model, true
}

// DshCatalog mirrors the DSH agent surface for the current user. "alpha" is the
// profile the dsh runner launches (see dshRunner.profileValue).
func (s *Service) DshCatalog() (DshCatalog, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return DshCatalog{}, err
	}
	return ReadDshCatalogForProfile(home, "alpha")
}

// currentDshCatalog reads the catalog for the profile the runner launches.
func currentDshCatalog() (DshCatalog, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return DshCatalog{}, err
	}
	return ReadDshCatalogForProfile(home, "alpha")
}
