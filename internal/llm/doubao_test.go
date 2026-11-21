package llm

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestResolveDoubaoConfigIsFixedAndUsesOnlyKeychain(t *testing.T) {
	previous := loadDoubaoKeychainKey
	loadDoubaoKeychainKey = func() string { return "synthetic-key" }
	t.Cleanup(func() { loadDoubaoKeychainKey = previous })
	t.Setenv("AGENTPUSH_LLM_BASE", "https://example.invalid/v1")
	t.Setenv("AGENTPUSH_LLM_KEY", "env-key-must-not-be-used")
	t.Setenv("AGENTPUSH_LLM_MODEL", "another-model")
	t.Setenv("AGENTPUSH_LLM_HTTP_PROXY", "http://127.0.0.1:9188")

	cfg := ResolveDoubaoConfig()
	if cfg.APIKey != "synthetic-key" || cfg.Source != "keychain" {
		t.Fatalf("cfg=%+v", cfg)
	}
	if cfg.BaseURL != DoubaoBaseURL || cfg.Model != DoubaoModelID {
		t.Fatalf("runtime contract must stay fixed: %+v", cfg)
	}
	if cfg.FallbackBaseURL != "" || cfg.HTTPProxy != "" {
		t.Fatalf("unexpected routing config: %+v", cfg)
	}
}

func TestNewDoubaoHTTPClientDisablesProxy(t *testing.T) {
	client := NewDoubaoHTTPClient(time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport=%T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("Doubao client must not use a proxy")
	}
}

func TestNewDoubaoGatewayFailsClosed(t *testing.T) {
	previous := loadDoubaoKeychainKey
	loadDoubaoKeychainKey = func() string { return "synthetic-key" }
	t.Cleanup(func() { loadDoubaoKeychainKey = previous })

	gateway := NewDoubaoGateway(time.Second)
	if gateway == nil {
		t.Fatal("gateway is nil")
	}
	if !gateway.Strict {
		t.Fatal("Doubao gateway must fail closed instead of returning a local fallback")
	}
}

func TestSaveDoubaoAPIKeyWritesOnlyTrimmedKeyToSecureStore(t *testing.T) {
	previous := saveDoubaoKeychainKey
	var stored string
	saveDoubaoKeychainKey = func(key string) error {
		stored = key
		return nil
	}
	t.Cleanup(func() { saveDoubaoKeychainKey = previous })

	if err := SaveDoubaoAPIKey("  synthetic-new-key  "); err != nil {
		t.Fatal(err)
	}
	if stored != "synthetic-new-key" {
		t.Fatalf("stored=%q", stored)
	}
}

func TestSaveDoubaoAPIKeyRejectsBlankKey(t *testing.T) {
	previous := saveDoubaoKeychainKey
	called := false
	saveDoubaoKeychainKey = func(string) error {
		called = true
		return errors.New("must not run")
	}
	t.Cleanup(func() { saveDoubaoKeychainKey = previous })

	if err := SaveDoubaoAPIKey(" \n\t "); err == nil {
		t.Fatal("expected blank key rejection")
	}
	if called {
		t.Fatal("blank key must not reach secure storage")
	}
}

func TestDoubaoKeychainInputConfirmsKeyTwice(t *testing.T) {
	if got, want := doubaoKeychainInput("synthetic-key"), "synthetic-key\nsynthetic-key\n"; got != want {
		t.Fatalf("stdin=%q want=%q", got, want)
	}
}
