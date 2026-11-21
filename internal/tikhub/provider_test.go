package tikhub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderConfigDefaultsToMainlandAndRequiresActivation(t *testing.T) {
	dataDir := t.TempDir()

	cfg, err := LoadProviderConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "tikhub" || cfg.Region != RegionMainland || cfg.BaseURL != MainlandBase {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	if cfg.Active || cfg.TestStatus != TestStatusUntested {
		t.Fatalf("new provider must be inactive and untested: %#v", cfg)
	}
}

func TestSaveProviderCredentialNeverPersistsSecretInProviderMetadata(t *testing.T) {
	dataDir := t.TempDir()
	secret := "synthetic-tikhub-token-123456"

	cfg, err := SaveProviderCredential(dataDir, secret, RegionGlobal)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Active || cfg.TestStatus != TestStatusUntested || cfg.BaseURL != DefaultBase {
		t.Fatalf("saving a credential must reset activation: %#v", cfg)
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "tikhub", "provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("provider metadata leaked API key")
	}
	keyInfo, err := os.Stat(filepath.Join(dataDir, "tikhub_api_key"))
	if err != nil {
		t.Fatal(err)
	}
	if keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("credential permissions=%#o want 0600", keyInfo.Mode().Perm())
	}
	if err := MarkProviderTest(dataDir, true, ""); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadProviderConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Active || reloaded.TestStatus != TestStatusPassed || reloaded.TestedAt == "" {
		t.Fatalf("successful test was not persisted: %#v", reloaded)
	}
}

func TestProviderRejectsArbitraryBaseURLAndInvalidCredential(t *testing.T) {
	if _, err := BaseURLForRegion("https://example.com"); err == nil {
		t.Fatal("arbitrary provider URL accepted")
	}
	if _, err := SaveProviderCredential(t.TempDir(), "short", RegionMainland); err == nil {
		t.Fatal("short credential accepted")
	}
}
