package tikhub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	MainlandBase = "https://api.tikhub.dev"

	RegionMainland = "mainland"
	RegionGlobal   = "global"

	TestStatusUntested = "untested"
	TestStatusPassed   = "passed"
	TestStatusFailed   = "failed"
)

type ProviderConfig struct {
	Provider   string `json:"provider"`
	Region     string `json:"region"`
	BaseURL    string `json:"base_url"`
	Active     bool   `json:"active"`
	TestStatus string `json:"test_status"`
	TestedAt   string `json:"tested_at,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
}

func defaultProviderConfig() ProviderConfig {
	return ProviderConfig{
		Provider:   "tikhub",
		Region:     RegionMainland,
		BaseURL:    MainlandBase,
		TestStatus: TestStatusUntested,
	}
}

func providerConfigPath(dataDir string) string {
	return filepath.Join(dataDir, "tikhub", "provider.json")
}

func BaseURLForRegion(region string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(region)) {
	case RegionMainland:
		return MainlandBase, nil
	case RegionGlobal:
		return DefaultBase, nil
	default:
		return "", fmt.Errorf("unsupported TikHub region")
	}
}

func LoadProviderConfig(dataDir string) (ProviderConfig, error) {
	cfg := defaultProviderConfig()
	raw, err := os.ReadFile(providerConfigPath(dataDir))
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ProviderConfig{}, fmt.Errorf("decode TikHub provider config: %w", err)
	}
	baseURL, err := BaseURLForRegion(cfg.Region)
	if err != nil {
		return ProviderConfig{}, err
	}
	cfg.Provider = "tikhub"
	cfg.BaseURL = baseURL
	if cfg.TestStatus != TestStatusPassed {
		cfg.Active = false
	}
	if cfg.TestStatus == "" {
		cfg.TestStatus = TestStatusUntested
	}
	return cfg, nil
}

func SaveProviderCredential(dataDir, key, region string) (ProviderConfig, error) {
	key = strings.TrimSpace(key)
	if len(key) < 12 || len(key) > 4096 || strings.IndexFunc(key, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}) >= 0 {
		return ProviderConfig{}, fmt.Errorf("invalid TikHub API key")
	}
	baseURL, err := BaseURLForRegion(region)
	if err != nil {
		return ProviderConfig{}, err
	}
	cfg := defaultProviderConfig()
	cfg.Region = strings.TrimSpace(strings.ToLower(region))
	cfg.BaseURL = baseURL
	if err := saveProviderConfig(dataDir, cfg); err != nil {
		return ProviderConfig{}, err
	}
	// Persist the inactive metadata first. If credential storage fails, an old
	// activation can never authorize a newly attempted credential.
	if err := SaveAPIKey(dataDir, key); err != nil {
		return ProviderConfig{}, err
	}
	return cfg, nil
}

func MarkProviderTest(dataDir string, passed bool, errorCode string) error {
	cfg, err := LoadProviderConfig(dataDir)
	if err != nil {
		return err
	}
	cfg.TestedAt = time.Now().UTC().Format(time.RFC3339)
	cfg.Active = passed
	cfg.ErrorCode = strings.TrimSpace(errorCode)
	if passed {
		cfg.TestStatus = TestStatusPassed
		cfg.ErrorCode = ""
	} else {
		cfg.TestStatus = TestStatusFailed
	}
	return saveProviderConfig(dataDir, cfg)
}

func ProviderConfigured(dataDir string) bool {
	key, err := LoadAPIKey(dataDir)
	return err == nil && strings.TrimSpace(key) != ""
}

func RequireActiveProvider(dataDir string) (ProviderConfig, error) {
	cfg, err := LoadProviderConfig(dataDir)
	if err != nil {
		return ProviderConfig{}, err
	}
	if !ProviderConfigured(dataDir) {
		return cfg, fmt.Errorf("TIKHUB_CREDENTIAL_MISSING")
	}
	if !cfg.Active || cfg.TestStatus != TestStatusPassed {
		return cfg, fmt.Errorf("TIKHUB_PROVIDER_NOT_ACTIVE")
	}
	return cfg, nil
}

func NewActiveClient(dataDir string) (*Client, ProviderConfig, error) {
	cfg, err := RequireActiveProvider(dataDir)
	if err != nil {
		return nil, cfg, err
	}
	key, err := LoadAPIKey(dataDir)
	if err != nil {
		return nil, cfg, fmt.Errorf("TIKHUB_CREDENTIAL_MISSING")
	}
	client, err := NewWithBaseURL(key, cfg.BaseURL)
	if err != nil {
		return nil, cfg, fmt.Errorf("TIKHUB_PROVIDER_CONFIG_INVALID")
	}
	if cfg.Region == RegionMainland {
		// The official global host is used only when the mainland accelerator
		// cannot complete transport setup. Provider HTTP errors are never retried.
		client.FallbackBaseURL = DefaultBase
	}
	return client, cfg, nil
}

func saveProviderConfig(dataDir string, cfg ProviderConfig) error {
	dir := filepath.Join(dataDir, "tikhub")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".provider-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, providerConfigPath(dataDir))
}
