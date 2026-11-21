// Package tikhub wraps TikHub HTTP API (api.tikhub.io) for ranks, search, user monitor.
// Key: env TIKHUB_API_KEY or data-dir/tikhub_api_key file.
package tikhub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBase      = "https://api.tikhub.io"
	maxResponseBytes = 64 << 20
)

// Client is a minimal TikHub REST client (Bearer auth).
type Client struct {
	BaseURL         string
	FallbackBaseURL string
	APIKey          string
	HTTP            *http.Client
}

type requestFailure struct {
	code    string
	message string
}

func (e *requestFailure) Error() string {
	return e.message
}

// SafeRequestFailure returns diagnostics that are safe to persist in local
// logs. Provider bodies are intentionally excluded because they may echo the
// submitted bearer token.
func SafeRequestFailure(err error) (string, string) {
	var failure *requestFailure
	if errors.As(err, &failure) {
		return failure.code, failure.message
	}
	return "request_error", "TikHub request failed before receiving a provider status"
}

// ProviderFailureCode reports the failure code and whether the error actually
// originated in the TikHub request path. Local failures (database writes,
// snapshot storage) return ok=false so callers never retry a deterministic
// error, while genuine provider hiccups stay distinguishable.
func ProviderFailureCode(err error) (string, bool) {
	var failure *requestFailure
	if errors.As(err, &failure) {
		return failure.code, true
	}
	return "", false
}

func New(apiKey string) *Client {
	return &Client{
		BaseURL: DefaultBase,
		APIKey:  strings.TrimSpace(apiKey),
		HTTP:    &http.Client{Timeout: 45 * time.Second},
	}
}

func NewWithBaseURL(apiKey, baseURL string) (*Client, error) {
	region := RegionGlobal
	if strings.TrimRight(strings.TrimSpace(baseURL), "/") == MainlandBase {
		region = RegionMainland
	}
	allowed, err := BaseURLForRegion(region)
	if err != nil || strings.TrimRight(strings.TrimSpace(baseURL), "/") != allowed {
		return nil, fmt.Errorf("unsupported TikHub base URL")
	}
	client := New(apiKey)
	client.BaseURL = allowed
	return client, nil
}

// LoadAPIKey from env or dataDir/tikhub_api_key.
func LoadAPIKey(dataDir string) (string, error) {
	if k := strings.TrimSpace(os.Getenv("TIKHUB_API_KEY")); k != "" {
		return k, nil
	}
	if dataDir != "" {
		b, err := os.ReadFile(filepath.Join(dataDir, "tikhub_api_key"))
		if err == nil {
			k := strings.TrimSpace(string(b))
			if k != "" {
				return k, nil
			}
		}
	}
	return "", fmt.Errorf("TIKHUB_API_KEY not set (env or %s/tikhub_api_key)", dataDir)
}

func SaveAPIKey(dataDir, key string) error {
	key = strings.TrimSpace(key)
	if dataDir == "" {
		return fmt.Errorf("data directory is required")
	}
	if key == "" || len(key) > 4096 || strings.IndexFunc(key, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}) >= 0 {
		return fmt.Errorf("invalid TikHub API key")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dataDir, ".tikhub-key-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(key + "\n"); err != nil {
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
	return os.Rename(tmpPath, filepath.Join(dataDir, "tikhub_api_key"))
}

func (c *Client) get(ctx context.Context, path string, q url.Values) (map[string]interface{}, error) {
	bases := []string{c.BaseURL}
	if c.FallbackBaseURL != "" && c.FallbackBaseURL != c.BaseURL {
		bases = append(bases, c.FallbackBaseURL)
	}
	for index, baseURL := range bases {
		u := strings.TrimRight(baseURL, "/") + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		response, err := c.do(req)
		if err == nil || index == len(bases)-1 || !isTransportTimeout(err) {
			return response, err
		}
	}
	return nil, &requestFailure{code: "network_error", message: "TikHub network request failed"}
}

func (c *Client) postJSON(ctx context.Context, path string, body map[string]interface{}) (map[string]interface{}, error) {
	raw, _ := json.Marshal(body)
	bases := []string{c.BaseURL}
	if c.FallbackBaseURL != "" && c.FallbackBaseURL != c.BaseURL {
		bases = append(bases, c.FallbackBaseURL)
	}
	for index, baseURL := range bases {
		u := strings.TrimRight(baseURL, "/") + path
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		response, err := c.do(req)
		if err == nil || index == len(bases)-1 || !isTransportTimeout(err) {
			return response, err
		}
	}
	return nil, &requestFailure{code: "network_error", message: "TikHub network request failed"}
}

func isTransportTimeout(err error) bool {
	var failure *requestFailure
	return errors.As(err, &failure) && failure.code == "transport_timeout"
}

func (c *Client) do(req *http.Request) (map[string]interface{}, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("empty API key")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if errors.Is(req.Context().Err(), context.Canceled) {
			return nil, &requestFailure{code: "request_cancelled", message: "TikHub request was cancelled by the local caller"}
		}
		if errors.Is(req.Context().Err(), context.DeadlineExceeded) {
			return nil, &requestFailure{code: "request_timeout", message: "TikHub request exceeded the local request deadline"}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &requestFailure{code: "transport_timeout", message: "TikHub connection timed out before a provider response"}
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, &requestFailure{code: "transport_timeout", message: "TikHub connection timed out before a provider response"}
		}
		return nil, &requestFailure{code: "network_error", message: "TikHub network request failed"}
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, &requestFailure{code: "response_read_failed", message: "TikHub response could not be read"}
	}
	if len(b) > maxResponseBytes {
		return nil, &requestFailure{code: "response_too_large", message: "TikHub response exceeded the local safety limit"}
	}
	var out map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if resp.StatusCode >= 300 {
		// Provider error bodies may echo the submitted token. Never include them
		// in logs, tool results, or UI responses.
		_ = decoder.Decode(&out)
		statusText := http.StatusText(resp.StatusCode)
		message := fmt.Sprintf("TikHub HTTP %d", resp.StatusCode)
		if statusText != "" {
			message += " " + statusText
		}
		return out, &requestFailure{
			code:    fmt.Sprintf("http_%d", resp.StatusCode),
			message: message,
		}
	}
	if err := decoder.Decode(&out); err != nil {
		return nil, &requestFailure{
			code:    "invalid_json",
			message: fmt.Sprintf("TikHub HTTP %d returned invalid JSON", resp.StatusCode),
		}
	}
	if failure := providerEnvelopeFailure(out); failure != nil {
		// The body is returned with the error, matching the HTTP-error path above.
		return out, failure
	}
	return out, nil
}

// providerEnvelopeFailure reports a business failure carried inside an HTTP 200
// envelope. TikHub answers a rejected request with {"code":400,"message":"…"}
// while still returning 200, so checking only the status code marked a failed
// (already paid) call as a success. The success codes are 200 for the envelope
// and 0 for the upstream platforms that reuse the field.
//
// The provider message is deliberately dropped: error bodies may echo the
// submitted bearer token, and SafeRequestFailure diagnostics get persisted.
func providerEnvelopeFailure(body map[string]interface{}) *requestFailure {
	if body == nil {
		return nil
	}
	code, ok := envelopeCode(body["code"])
	if !ok || code == 0 || code == 200 {
		return nil
	}
	return &requestFailure{
		code:    fmt.Sprintf("provider_%d", code),
		message: fmt.Sprintf("TikHub provider rejected the request with code %d", code),
	}
}

// envelopeCode reads the envelope status code in every shape it can arrive in:
// json.Number from UseNumber decoding, the plain Go numbers a replayed snapshot
// carries, and the string form some endpoints emit.
func envelopeCode(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return parsed, true
		}
		if parsed, err := typed.Float64(); err == nil {
			return int64(parsed), true
		}
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// GetTikHubUserInfo validates the API key and returns provider account metadata.
// Callers should discard the response unless account details are explicitly needed.
func (c *Client) GetTikHubUserInfo(ctx context.Context) (map[string]interface{}, error) {
	return c.get(ctx, "/api/v1/tikhub/user/get_user_info", nil)
}

// CalculatePrice asks TikHub's official pricing endpoint for the current
// endpoint unit price and tier calculation. It never estimates locally.
func (c *Client) CalculatePrice(ctx context.Context, endpoint string, requestsPerDay int) (map[string]interface{}, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" || !strings.HasPrefix(endpoint, "/api/") || requestsPerDay <= 0 || requestsPerDay > 1_000_000 {
		return nil, fmt.Errorf("invalid TikHub price query")
	}
	query := url.Values{}
	query.Set("endpoint", endpoint)
	query.Set("request_per_day", fmt.Sprintf("%d", requestsPerDay))
	return c.get(ctx, "/api/v1/tikhub/user/calculate_price", query)
}

// --- Douyin billboards (daily top) ---

// RankBoard maps CLI name → API path (POST JSON).
var RankBoards = map[string]string{
	"video":     "/api/v1/douyin/billboard/fetch_hot_total_video_list",
	"low_fan":   "/api/v1/douyin/billboard/fetch_hot_total_low_fan_list",
	"high_like": "/api/v1/douyin/billboard/fetch_hot_total_high_like_list",
	"high_fan":  "/api/v1/douyin/billboard/fetch_hot_total_high_fan_list",
	"high_play": "/api/v1/douyin/billboard/fetch_hot_total_high_play_list",
}

func (c *Client) FetchDouyinRank(ctx context.Context, board string, page, pageSize, dateWindow int) (map[string]interface{}, error) {
	path, ok := RankBoards[board]
	if !ok {
		return nil, fmt.Errorf("unknown board %q (video|low_fan|high_like|high_fan|high_play)", board)
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if dateWindow <= 0 {
		dateWindow = 24
	}
	return c.postJSON(ctx, path, map[string]interface{}{
		"page": page, "page_size": pageSize, "date_window": dateWindow,
	})
}

// FetchDouyinHotSearch GET app v3 hot search list.
func (c *Client) FetchDouyinHotSearch(ctx context.Context) (map[string]interface{}, error) {
	return c.get(ctx, "/api/v1/douyin/app/v3/fetch_hot_search_list", url.Values{"board_type": {"0"}, "board_sub_type": {""}})
}

// --- Xiaohongshu ---

func (c *Client) SearchXHSNotes(ctx context.Context, keyword string, page int) (map[string]interface{}, error) {
	if page <= 0 {
		page = 1
	}
	q := url.Values{}
	q.Set("keyword", keyword)
	q.Set("page", fmt.Sprintf("%d", page))
	return c.get(ctx, "/api/v1/xiaohongshu/app_v2/search_notes", q)
}

func (c *Client) GetXHSUserInfo(ctx context.Context, userID string) (map[string]interface{}, error) {
	q := url.Values{}
	q.Set("user_id", userID)
	return c.get(ctx, "/api/v1/xiaohongshu/app_v2/get_user_info", q)
}

func (c *Client) GetXHSUserNotes(ctx context.Context, userID, cursor string) (map[string]interface{}, error) {
	q := url.Values{}
	q.Set("user_id", userID)
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	return c.get(ctx, "/api/v1/xiaohongshu/app_v2/get_user_posted_notes", q)
}

// SearchDouyinVideos keyword search (app v3).
func (c *Client) SearchDouyinVideos(ctx context.Context, keyword string, offset, count int) (map[string]interface{}, error) {
	if count <= 0 {
		count = 10
	}
	if offset < 0 {
		offset = 0
	}
	q := url.Values{}
	q.Set("keyword", keyword)
	q.Set("offset", fmt.Sprintf("%d", offset))
	q.Set("count", fmt.Sprintf("%d", count))
	q.Set("sort_type", "0")
	q.Set("publish_time", "7")
	return c.get(ctx, "/api/v1/douyin/app/v3/fetch_video_search_result", q)
}

// ExtractRankItems tries to pull objs/list from nested TikHub responses.
func ExtractRankItems(resp map[string]interface{}) []map[string]interface{} {
	if resp == nil {
		return nil
	}
	// data.data.objs | data.objs | data.list
	if data, ok := resp["data"].(map[string]interface{}); ok {
		if inner, ok := data["data"].(map[string]interface{}); ok {
			if objs := asMapSlice(inner["objs"]); len(objs) > 0 {
				return objs
			}
			if objs := asMapSlice(inner["list"]); len(objs) > 0 {
				return objs
			}
			if objs := asMapSlice(inner["word_list"]); len(objs) > 0 {
				return objs
			}
		}
		if objs := asMapSlice(data["objs"]); len(objs) > 0 {
			return objs
		}
		if objs := asMapSlice(data["list"]); len(objs) > 0 {
			return objs
		}
		if objs := asMapSlice(data["items"]); len(objs) > 0 {
			return objs
		}
	}
	return nil
}

func asMapSlice(v interface{}) []map[string]interface{} {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, it := range arr {
		if m, ok := it.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

// AsMapSlice exports list coercion for CLI parsers.
func AsMapSlice(v interface{}) []map[string]interface{} { return asMapSlice(v) }

// SaveJSON writes pretty JSON under dataDir/tikhub/subpath.
func SaveJSON(dataDir, subpath string, v interface{}) (string, error) {
	dir := filepath.Join(dataDir, "tikhub", filepath.Dir(subpath))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fp := filepath.Join(dataDir, "tikhub", subpath)
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(fp, b, 0o644); err != nil {
		return "", err
	}
	return fp, nil
}

// Today returns YYYY-MM-DD local.
func Today() string {
	return time.Now().Format("2006-01-02")
}
