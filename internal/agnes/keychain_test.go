package agnes

import (
	"errors"
	"testing"
	"time"
)

func TestSaveAPIKeyUsesOnlySecureStoreAndRejectsUnsafeInput(t *testing.T) {
	previous := saveKeychainKey
	var stored string
	saveKeychainKey = func(key string) error {
		stored = key
		return nil
	}
	t.Cleanup(func() { saveKeychainKey = previous })

	if err := SaveAPIKey("  synthetic-agnes-key  "); err != nil {
		t.Fatal(err)
	}
	if stored != "synthetic-agnes-key" {
		t.Fatalf("stored=%q", stored)
	}
	for _, invalid := range []string{"", " \t ", "synthetic\nsecond-line", "synthetic\x00key"} {
		stored = ""
		if err := SaveAPIKey(invalid); CodeOf(err) != CodeInvalidKey {
			t.Errorf("key=%q code=%s error=%v", invalid, CodeOf(err), err)
		}
		if stored != "" {
			t.Errorf("invalid key reached secure store: %q", stored)
		}
	}
}

func TestLoadAPIKeyAndNewClientFromKeychainFailClosed(t *testing.T) {
	previous := loadKeychainKey
	loadKeychainKey = func() (string, error) { return "synthetic-agnes-key\n", nil }
	t.Cleanup(func() { loadKeychainKey = previous })

	key, err := LoadAPIKey()
	if err != nil || key != "synthetic-agnes-key" {
		t.Fatalf("key=%q error=%v", key, err)
	}
	client, err := NewClientFromKeychain(time.Second)
	if err != nil || client == nil {
		t.Fatalf("client=%v error=%v", client, err)
	}

	loadKeychainKey = func() (string, error) { return "", errors.New("synthetic unavailable") }
	client, err = NewClientFromKeychain(time.Second)
	if client != nil || CodeOf(err) != CodeKeychainReadFailed {
		t.Fatalf("client=%v code=%s error=%v", client, CodeOf(err), err)
	}
}

func TestAPIKeyConfiguredReturnsMetadataOnly(t *testing.T) {
	previous := keychainKeyConfigured
	keychainKeyConfigured = func() (bool, error) { return true, nil }
	t.Cleanup(func() { keychainKeyConfigured = previous })

	configured, err := APIKeyConfigured()
	if err != nil || !configured {
		t.Fatalf("configured=%v error=%v", configured, err)
	}

	keychainKeyConfigured = func() (bool, error) { return false, errors.New("synthetic unavailable") }
	configured, err = APIKeyConfigured()
	if configured || CodeOf(err) != CodeKeychainReadFailed {
		t.Fatalf("configured=%v code=%s error=%v", configured, CodeOf(err), err)
	}
}
