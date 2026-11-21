package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1/", "https://dashscope.aliyuncs.com/compatible-mode/v1"},
		{"https://x/v1/chat/completions", "https://x/v1"},
		{"  https://example.test/v1/  ", "https://example.test/v1"},
	}
	for _, c := range cases {
		got := normalizeBaseURL(c.in)
		if got != c.want {
			t.Fatalf("normalize(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestMaskKey(t *testing.T) {
	if MaskKey("") != "(empty)" {
		t.Fatal("empty")
	}
	// synthetic key only — never put real secrets in the git tree
	const synthetic = "sk-test-aaaaaaaaaaaaaaaaaaaaaaaa"
	m := MaskKey(synthetic)
	if strings.Contains(m, "aaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatalf("mask leaked full key: %s", m)
	}
	if !strings.HasPrefix(m, "sk-tes") {
		t.Fatalf("prefix: %s", m)
	}
}

func TestResolveConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llm.json")
	body := map[string]string{
		"base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
		"api_key":  "sk-test-key-abcdefghij",
		"model":    "deepseek-v4-flash",
	}
	b, _ := json.Marshal(body)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALPHA_NEXUS_DATA_DIR", dir)
	// clear env overrides so file wins
	t.Setenv("AGENTPUSH_LLM_BASE", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("AGENTPUSH_LLM_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ARK_API_KEY", "")
	t.Setenv("DASHSCOPE_API_KEY", "")
	t.Setenv("AGENTPUSH_LLM_MODEL", "")
	t.Setenv("OPENAI_MODEL", "")
	// Proxy variables also feed ResolveConfig; an inherited HTTPS_PROXY would
	// turn a file-only config into "env+file".
	for _, key := range []string{
		"AGENTPUSH_LLM_HTTP_PROXY", "HTTPS_PROXY", "HTTP_PROXY",
		"https_proxy", "http_proxy", "ALL_PROXY", "all_proxy",
		"NO_PROXY", "no_proxy",
	} {
		t.Setenv(key, "")
	}

	cfg := ResolveConfig()
	if cfg.Model != "deepseek-v4-flash" {
		t.Fatalf("model %q", cfg.Model)
	}
	if !strings.HasSuffix(cfg.BaseURL, "/compatible-mode/v1") {
		t.Fatalf("base %q", cfg.BaseURL)
	}
	if strings.Contains(cfg.BaseURL, "//compatible") {
		t.Fatalf("double slash: %q", cfg.BaseURL)
	}
	if cfg.APIKey != "sk-test-key-abcdefghij" {
		t.Fatalf("key %q", cfg.APIKey)
	}
	if !strings.HasPrefix(cfg.Source, "file:") {
		t.Fatalf("source %q", cfg.Source)
	}
	st := cfg.RedactedStatus()
	if st["api_key"] == cfg.APIKey {
		t.Fatal("redacted still full key")
	}
	if !strings.HasSuffix(st["chat_url"], "/v1/chat/completions") {
		t.Fatalf("chat_url %q", st["chat_url"])
	}
}

func TestResolveConfigEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "llm.json"), []byte(`{"base_url":"http://file/v1","api_key":"filekey","model":"file-model"}`), 0o600)
	t.Setenv("ALPHA_NEXUS_DATA_DIR", dir)
	t.Setenv("AGENTPUSH_LLM_BASE", "https://dashscope.aliyuncs.com/compatible-mode/v1")
	t.Setenv("AGENTPUSH_LLM_KEY", "sk-env-key-1234567890")
	t.Setenv("AGENTPUSH_LLM_MODEL", "deepseek-v4-flash")

	cfg := ResolveConfig()
	if cfg.Model != "deepseek-v4-flash" || cfg.APIKey != "sk-env-key-1234567890" {
		t.Fatalf("%+v", cfg)
	}
	if !strings.Contains(cfg.Source, "env") {
		t.Fatalf("source %q", cfg.Source)
	}
}

func TestCompleteStrictAgainstMock(t *testing.T) {
	var sawAuth, sawURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawURL = r.URL.Path
		sawAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/compatible-mode/v1/chat/completions" && r.URL.Path != "/chat/completions" {
			// server base includes path prefix when using Path
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "pong-from-mock"}},
			},
		})
	}))
	defer srv.Close()

	g := &Gateway{
		BaseURL: srv.URL + "/compatible-mode/v1",
		APIKey:  "sk-mock",
		Model:   "deepseek-v4-flash",
		Client:  srv.Client(),
		Strict:  true,
	}
	out, err := g.Complete(context.Background(), "sys", "ping")
	if err != nil {
		t.Fatal(err)
	}
	if out != "pong-from-mock" {
		t.Fatalf("out %q", out)
	}
	if IsLocalTemplate(out) {
		t.Fatal("should not be local template")
	}
	if sawAuth != "Bearer sk-mock" {
		t.Fatalf("auth %q", sawAuth)
	}
	if !strings.HasSuffix(sawURL, "/chat/completions") {
		t.Fatalf("url path %q", sawURL)
	}
}

func TestCompleteStrictHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()
	g := &Gateway{BaseURL: srv.URL + "/v1", APIKey: "x", Model: "m", Client: srv.Client(), Strict: true}
	_, err := g.Complete(context.Background(), "s", "u")
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestLiveDoubaoComplete is opt-in because it bills the configured provider.
// Default test/build commands must never send a paid model request.
func TestLiveDoubaoComplete(t *testing.T) {
	if os.Getenv("ALPHA_NEXUS_REAL_DOUBAO_SMOKE") != "1" {
		t.Skip("set ALPHA_NEXUS_REAL_DOUBAO_SMOKE=1 after approving one paid Doubao request")
	}
	if testing.Short() {
		t.Skip("short")
	}
	cfg := ResolveDoubaoConfig()
	if cfg.APIKey == "" {
		t.Skip("no Doubao API key in macOS Keychain")
	}
	g := NewDoubaoGateway(15 * time.Second)
	if g == nil {
		t.Fatal("configured key must create a Doubao gateway")
	}
	g.Strict = true
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := g.Complete(ctx, "Reply with exactly one word: ok", "ping")
	if err != nil {
		t.Fatalf("live complete: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("empty response")
	}
	if IsLocalTemplate(out) {
		t.Fatalf("got local-template fallback: %q", out)
	}
	t.Logf("live response: %q model=%s base=%s", out, g.Model, g.BaseURL)
}

func TestFallbackOnTransportError(t *testing.T) {
	// primary is blackhole; fallback is mock
	var hitFallback bool
	fb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitFallback = true
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": "via-fallback"}},
			},
		})
	}))
	defer fb.Close()

	g := &Gateway{
		BaseURL:         "http://127.0.0.1:1", // connection refused
		FallbackBaseURL: fb.URL + "/v1",
		APIKey:          "k",
		Model:           "deepseek-v4-flash",
		Client:          &http.Client{Timeout: 2 * time.Second},
		Strict:          true,
	}
	out, err := g.Complete(context.Background(), "s", "u")
	if err != nil {
		t.Fatal(err)
	}
	if out != "via-fallback" || !hitFallback {
		t.Fatalf("out=%q hit=%v", out, hitFallback)
	}
}
