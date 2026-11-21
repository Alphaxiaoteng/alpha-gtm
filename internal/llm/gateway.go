// Package llm is the OpenAI-compatible chat gateway (AgentPush AP-501).
// Supports the fixed Doubao runtime and test-only OpenAI-style HTTP fixtures.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Gateway is LiteLLM / OpenAI-compatible chat completion.
type Gateway struct {
	BaseURL string // e.g. https://dashscope.aliyuncs.com/compatible-mode/v1
	// FallbackBaseURL is tried once when primary fails with a network/transport error
	// (e.g. local codex-dashscope proxy on :18686 when fake-ip blocks outbound).
	FallbackBaseURL string
	APIKey          string
	Model           string
	HTTPProxy       string // optional explicit proxy URL for Transport
	Client          *http.Client
	// Strict: when true, Complete returns HTTP/parse errors instead of localFallback.
	// Used by smoke tests so success is distinguishable from offline templates.
	Strict bool
}

// FileConfig is optional ~/.alpha-nexus/llm.json (or $ALPHA_NEXUS_DATA_DIR/llm.json).
// Never commit this file; it holds secrets.
type FileConfig struct {
	BaseURL         string `json:"base_url"`
	FallbackBaseURL string `json:"fallback_base_url"`
	APIKey          string `json:"api_key"`
	Model           string `json:"model"`
	HTTPProxy       string `json:"http_proxy"`
}

// Config holds resolved runtime settings (safe to log after Redact).
type Config struct {
	BaseURL         string
	FallbackBaseURL string
	APIKey          string
	Model           string
	HTTPProxy       string
	Source          string // env | file | default
}

// Default loads env, then local llm.json, then unconfigured Doubao API defaults.
func Default() *Gateway {
	cfg := ResolveConfig()
	return &Gateway{
		BaseURL:         cfg.BaseURL,
		FallbackBaseURL: cfg.FallbackBaseURL,
		APIKey:          cfg.APIKey,
		Model:           cfg.Model,
		HTTPProxy:       cfg.HTTPProxy,
		Client:          newHTTPClient(cfg.HTTPProxy, 60*time.Second),
	}
}

func newHTTPClient(proxyURL string, timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	// Prefer explicit proxy, else env (HTTP_PROXY/HTTPS_PROXY)
	if proxyURL != "" {
		if u, err := parseProxyURL(proxyURL); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	} else {
		tr.Proxy = http.ProxyFromEnvironment
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

func parseProxyURL(raw string) (*url.URL, error) {
	return url.Parse(raw)
}

// ResolveConfig merges AGENTPUSH_LLM_* / OPENAI_* env with optional llm.json.
// Precedence: env > file > built-in defaults.
func ResolveConfig() Config {
	cfg := Config{
		BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		Model:   "doubao-seed-2-0-pro-260215",
		Source:  "default",
	}

	// 1) local file (if present)
	if fc, path, ok := loadFileConfig(); ok {
		cfg.Source = "file:" + path
		if fc.BaseURL != "" {
			cfg.BaseURL = fc.BaseURL
		}
		if fc.FallbackBaseURL != "" {
			cfg.FallbackBaseURL = fc.FallbackBaseURL
		}
		if fc.APIKey != "" {
			cfg.APIKey = fc.APIKey
		}
		if fc.Model != "" {
			cfg.Model = fc.Model
		}
		if fc.HTTPProxy != "" {
			cfg.HTTPProxy = fc.HTTPProxy
		}
	}

	// 2) env overrides (highest)
	srcEnv := false
	if v := firstEnv("AGENTPUSH_LLM_BASE", "OPENAI_BASE_URL"); v != "" {
		cfg.BaseURL = v
		srcEnv = true
	}
	if v := firstEnv("AGENTPUSH_LLM_FALLBACK_BASE", "AGENTPUSH_LLM_PROXY_BASE"); v != "" {
		cfg.FallbackBaseURL = v
		srcEnv = true
	}
	if v := firstEnv("AGENTPUSH_LLM_KEY", "OPENAI_API_KEY", "ARK_API_KEY", "DASHSCOPE_API_KEY"); v != "" {
		cfg.APIKey = v
		srcEnv = true
	}
	if v := firstEnv("AGENTPUSH_LLM_MODEL", "OPENAI_MODEL"); v != "" {
		cfg.Model = v
		srcEnv = true
	}
	if v := firstEnv("AGENTPUSH_LLM_HTTP_PROXY", "HTTPS_PROXY", "HTTP_PROXY"); v != "" {
		// only treat as llm proxy if AGENTPUSH_LLM_HTTP_PROXY set; env HTTP_PROXY still used by Transport
		if os.Getenv("AGENTPUSH_LLM_HTTP_PROXY") != "" {
			cfg.HTTPProxy = v
		}
		srcEnv = true
	}
	// auto-detect local codex-dashscope proxy when CODEX_DASHSCOPE_PROXY_PORT is set
	if cfg.FallbackBaseURL == "" {
		if p := os.Getenv("CODEX_DASHSCOPE_PROXY_PORT"); p != "" {
			cfg.FallbackBaseURL = "http://127.0.0.1:" + p + "/v1"
		}
	}
	if srcEnv {
		if strings.HasPrefix(cfg.Source, "file:") {
			cfg.Source = "env+file"
		} else {
			cfg.Source = "env"
		}
	}

	cfg.BaseURL = normalizeBaseURL(cfg.BaseURL)
	cfg.FallbackBaseURL = normalizeBaseURL(cfg.FallbackBaseURL)
	return cfg
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func normalizeBaseURL(base string) string {
	base = strings.TrimSpace(base)
	base = strings.TrimRight(base, "/")
	// avoid .../v1/v1 if user pasted trailing chat path
	base = strings.TrimSuffix(base, "/chat/completions")
	base = strings.TrimRight(base, "/")
	return base
}

func loadFileConfig() (FileConfig, string, bool) {
	var paths []string
	if d := os.Getenv("ALPHA_NEXUS_DATA_DIR"); d != "" {
		paths = append(paths, filepath.Join(d, "llm.json"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".alpha-nexus", "llm.json"))
	}
	// also project-local data dir convention
	paths = append(paths, filepath.Join(".alpha-nexus", "llm.json"))

	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var fc FileConfig
		if err := json.Unmarshal(b, &fc); err != nil {
			continue
		}
		if fc.BaseURL == "" && fc.APIKey == "" && fc.Model == "" {
			continue
		}
		return fc, p, true
	}
	return FileConfig{}, "", false
}

// RedactedStatus returns config safe for logs (key masked).
func (c Config) RedactedStatus() map[string]string {
	proxy := c.HTTPProxy
	if proxy != "" {
		// hide credentials in proxy URL user:pass@host
		if u, err := url.Parse(proxy); err == nil {
			if u.User != nil {
				u.User = url.UserPassword("***", "***")
			}
			proxy = u.String()
		}
	}
	return map[string]string{
		"base_url":          c.BaseURL,
		"fallback_base_url": c.FallbackBaseURL,
		"model":             c.Model,
		"api_key":           MaskKey(c.APIKey),
		"http_proxy":        proxy,
		"source":            c.Source,
		"key_set":           fmt.Sprintf("%v", c.APIKey != ""),
		"chat_url":          strings.TrimRight(c.BaseURL, "/") + "/chat/completions",
	}
}

// MaskKey shows only prefix/suffix of a secret.
func MaskKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "(empty)"
	}
	if len(key) <= 10 {
		return key[:2] + "***"
	}
	return key[:6] + "..." + key[len(key)-4:]
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Complete returns assistant text; falls back to local template if request fails (unless Strict).
func (g *Gateway) Complete(ctx context.Context, system, user string) (string, error) {
	if g == nil {
		return localFallback(user), fmt.Errorf("nil gateway")
	}
	bases := []string{normalizeBaseURL(g.BaseURL)}
	if fb := normalizeBaseURL(g.FallbackBaseURL); fb != "" && fb != bases[0] {
		bases = append(bases, fb)
	}
	if bases[0] == "" {
		if g.Strict {
			return "", fmt.Errorf("empty base URL")
		}
		return localFallback(user), nil
	}

	var lastErr error
	for i, base := range bases {
		// Prefer snappy fail-over when a fallback is configured (fake-ip / blocked outbound).
		attemptCtx := ctx
		var cancel context.CancelFunc
		if len(bases) > 1 && i < len(bases)-1 {
			attemptCtx, cancel = context.WithTimeout(ctx, 8*time.Second)
		}
		out, err := g.completeOnce(attemptCtx, base, system, user)
		if cancel != nil {
			cancel()
		}
		if err == nil {
			return out, nil
		}
		lastErr = err
		// only chain to fallback on transport / network style errors
		if i+1 < len(bases) && isTransportErr(err) {
			continue
		}
		break
	}
	if g.Strict {
		return "", lastErr
	}
	return localFallback(user), nil
}

func isTransportErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "timeout") ||
		strings.Contains(s, "deadline exceeded") ||
		strings.Contains(s, "canceled") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "no such host") ||
		strings.Contains(s, "network is unreachable") ||
		strings.Contains(s, "i/o timeout") ||
		strings.Contains(s, "tls") ||
		strings.Contains(s, "eof") ||
		strings.Contains(s, "llm request:")
}

func (g *Gateway) completeOnce(ctx context.Context, base, system, user string) (string, error) {
	endpoint := base + "/chat/completions"
	body := map[string]interface{}{
		"model": g.Model,
		"messages": []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		"temperature": 0.4,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if g.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+g.APIKey)
	}
	client := g.Client
	if client == nil {
		client = newHTTPClient(g.HTTPProxy, 90*time.Second)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm HTTP %d: %s", resp.StatusCode, truncate(string(b), 400))
	}
	var parsed struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return "", fmt.Errorf("llm parse: %w body=%s", err, truncate(string(b), 200))
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("llm empty choices: %s", truncate(string(b), 200))
	}
	out := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if out == "" {
		return "", fmt.Errorf("llm empty content")
	}
	return out, nil
}

// IsLocalTemplate reports whether text is the offline fallback marker.
func IsLocalTemplate(s string) bool {
	return strings.Contains(s, "[local-template]")
}

func localFallback(user string) string {
	u := strings.TrimSpace(user)
	if len(u) > 280 {
		u = u[:277] + "..."
	}
	return fmt.Sprintf("[local-template] %s\n\n#tags #agentpush", u)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
