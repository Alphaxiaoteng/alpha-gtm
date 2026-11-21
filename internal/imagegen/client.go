// Package imagegen is a small, generic client for the Gemini-compatible
// image generation endpoint exposed by zexapi (章鱼哥 API).
//
// The provider contract is:
//
//	POST {base_url}/v1beta/models/{model}:generateContent
//	Authorization: Bearer <key>
//	{"contents":[{"role":"user","parts":[{"text":"..."}]}],
//	 "generationConfig":{"responseModalities":["IMAGE"],"imageConfig":{...}},
//	 "response_format":"b64_json"}
//
// The response shape is not stable across upstreams, so parsing accepts
// candidates[].content.parts[].inlineData.data, data[].b64_json and
// data[].url. URLs are downloaded immediately because the provider explicitly
// does not guarantee long-lived links.
//
// Secrets never appear in returned errors: response bodies are not echoed and
// any occurrence of the API key is redacted.
package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultTimeout matches the provider guidance for synchronous image
	// generation (>= 120s).
	DefaultTimeout = 120 * time.Second

	DefaultModel       = "gpt-image-2.6"
	DefaultAspectRatio = "1:1"
	DefaultImageSize   = "1K"
	maxPromptBytes     = 8 << 10
	// maxRequestBytes caps the whole JSON body (reference image + prompt), so
	// there is no second, independent per-image limit to trip over.
	maxRequestBytes     = 1 << 20
	maxResponseBytes    = 96 << 20
	MaxImageBytes       = 32 << 20
	maxModelNameBytes   = 128
	generationCodecPath = "/v1beta/models/"
)

var (
	ErrNotConfigured   = errors.New("imagegen: client is not configured")
	ErrKeyMissing      = errors.New("imagegen: api key is missing")
	ErrPromptRequired  = errors.New("imagegen: prompt is required")
	ErrPromptTooLong   = errors.New("imagegen: prompt is too long")
	ErrInvalidBaseURL  = errors.New("imagegen: base url must be http or https")
	ErrInvalidModel    = errors.New("imagegen: model name is invalid")
	ErrUnsafeImageURL  = errors.New("imagegen: provider returned an unsafe image url")
	ErrProviderFailure = errors.New("imagegen: provider request failed")
	ErrEmptyResult     = errors.New("imagegen: provider returned no image")
	ErrImageTooLarge   = errors.New("imagegen: provider image exceeds the size limit")
	ErrUnsupportedMime = errors.New("imagegen: provider image mime type is not supported")
	// ErrInvalidReference marks a caller-supplied reference image that cannot be
	// used: unknown file, not an image, or too large. Handlers map it to 400 so
	// a bad reference never looks like an upstream outage.
	ErrInvalidReference = errors.New("imagegen: reference image is not usable")
	// ErrReferenceTooLarge marks a reference image that base64-expands past
	// maxRequestBytes once the prompt is added, i.e. the whole request body no
	// longer fits. There is no separate per-image cap: this is the only limit,
	// and it wraps ErrInvalidReference so callers still answer 400 — the caller
	// must shrink the image, not retry against the provider.
	ErrReferenceTooLarge = fmt.Errorf("%w: 参考图过大，请压缩后重试", ErrInvalidReference)
)

// supportedImageModels are the image models published by the provider on
// GET /v1/models. A "-preview" suffix is accepted for every entry because the
// live catalogue and generated model names disagree on that suffix.
var supportedImageModels = map[string]struct{}{
	"gpt-image-2":                    {},
	"gpt-image-2.6":                  {},
	"gemini-3-pro-image":             {},
	"gemini-3.1-flash-image":         {},
	"gemini-3-pro-image-preview":     {},
	"gemini-3.1-flash-image-preview": {},
}

// IsSupportedModel reports whether model is a known image model. Both the bare
// and "-preview" spellings are accepted.
func IsSupportedModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || len(model) > maxModelNameBytes {
		return false
	}
	if _, ok := supportedImageModels[model]; ok {
		return true
	}
	if strings.HasSuffix(model, "-preview") {
		_, ok := supportedImageModels[strings.TrimSuffix(model, "-preview")]
		return ok
	}
	return false
}

// Client talks to one provider base URL. HTTPClient may be nil, in which case
// a direct (proxy-less) client with DefaultTimeout is used.
type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

var rejectRedirect = func(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

// Simple is the product-facing entry: a prompt plus one optional reference
// image. Every generation parameter (model, aspect ratio, resolution) is chosen
// by the server so callers never have to expose technical knobs in the UI.
func (c *Client) Simple(ctx context.Context, prompt string, refImage []byte, refMime string) ([]byte, string, error) {
	return c.generate(ctx, prompt, refImage, refMime, DefaultAspectRatio, DefaultImageSize)
}

// Generate requests one image and returns its decoded bytes plus mime type.
// The image is fully materialised in memory; callers must persist it
// immediately because provider URLs are not durable.
func (c *Client) Generate(ctx context.Context, prompt, aspectRatio, imageSize string) ([]byte, string, error) {
	return c.generate(ctx, prompt, nil, "", aspectRatio, imageSize)
}

func (c *Client) generate(ctx context.Context, prompt string, refImage []byte, refMime, aspectRatio, imageSize string) ([]byte, string, error) {
	if c == nil {
		return nil, "", ErrNotConfigured
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, "", ErrPromptRequired
	}
	if len(prompt) > maxPromptBytes {
		return nil, "", ErrPromptTooLong
	}
	root, err := normalizeBaseURL(c.BaseURL)
	if err != nil {
		return nil, "", err
	}
	key := strings.TrimSpace(c.APIKey)
	if key == "" {
		return nil, "", ErrKeyMissing
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = DefaultModel
	}
	if len(model) > maxModelNameBytes {
		return nil, "", ErrInvalidModel
	}
	ratio := strings.TrimSpace(aspectRatio)
	if ratio == "" {
		ratio = DefaultAspectRatio
	}
	size := strings.TrimSpace(imageSize)
	if size == "" {
		size = DefaultImageSize
	}

	parts := []generatePart{}
	if len(refImage) > 0 {
		// One budget for the whole body: a reference image is base64-expanded
		// to 4/3 of its size, so the only meaningful limit is maxRequestBytes.
		// Refuse early on an obviously oversized file instead of encoding it.
		if len(refImage) > maxRequestBytes {
			return nil, "", fmt.Errorf("%w（参考图 %d 字节，整体请求上限 %d 字节）", ErrReferenceTooLarge, len(refImage), maxRequestBytes)
		}
		mime := strings.TrimSpace(refMime)
		if mime == "" {
			mime = "image/png"
		}
		parts = append(parts, generatePart{InlineData: &inlineData{
			MimeType: mime,
			Data:     base64.StdEncoding.EncodeToString(refImage),
		}})
	}
	// text 是必填,且要放在参考图之后(图生图惯例)。
	parts = append(parts, generatePart{Text: prompt})
	payload, err := json.Marshal(generateRequest{
		Contents: []generateContent{{Role: "user", Parts: parts}},
		GenerationConfig: generationConfig{
			ResponseModalities: []string{"IMAGE"},
			ImageConfig:        imageConfig{AspectRatio: ratio, ImageSize: size},
		},
		ResponseFormat: "b64_json",
	})
	if err != nil {
		return nil, "", fmt.Errorf("%w: encode request", ErrProviderFailure)
	}
	if len(payload) > maxRequestBytes {
		if len(refImage) > 0 {
			// Blaming the prompt here would be wrong and unactionable: the
			// caller cannot shrink the prompt enough to fit a large photo.
			return nil, "", fmt.Errorf("%w（参考图 %d 字节 + prompt 后请求体 %d 字节，上限 %d 字节）",
				ErrReferenceTooLarge, len(refImage), len(payload), maxRequestBytes)
		}
		return nil, "", ErrPromptTooLong
	}

	client := c.httpClient()
	result, status, err := c.request(ctx, client, root, model, key, payload)
	if err != nil && retryableStatus(status) {
		if alt, ok := alternateModel(model); ok {
			result, status, err = c.request(ctx, client, root, alt, key, payload)
		}
	}
	if err != nil {
		return nil, "", err
	}

	data, mime, err := c.resolve(ctx, client, result)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", ErrEmptyResult
	}
	if len(data) > MaxImageBytes {
		return nil, "", ErrImageTooLarge
	}
	normalized, mimeErr := normalizeMime(mime, data)
	if mimeErr != nil {
		return nil, "", mimeErr
	}
	return data, normalized, nil
}

type generateRequest struct {
	Contents         []generateContent `json:"contents"`
	GenerationConfig generationConfig  `json:"generationConfig"`
	ResponseFormat   string            `json:"response_format"`
}

type generateContent struct {
	Role  string         `json:"role"`
	Parts []generatePart `json:"parts"`
}

type generatePart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inlineData,omitempty"`
}

type generationConfig struct {
	ResponseModalities []string    `json:"responseModalities"`
	ImageConfig        imageConfig `json:"imageConfig"`
}

type imageConfig struct {
	AspectRatio string `json:"aspectRatio,omitempty"`
	ImageSize   string `json:"imageSize,omitempty"`
}

type inlineData struct {
	MimeType    string `json:"mimeType"`
	MimeTypeAlt string `json:"mime_type,omitempty"`
	Data        string `json:"data"`
}

type responsePart struct {
	Text       string      `json:"text"`
	InlineData *inlineData `json:"inlineData"`
	InlineAlt  *inlineData `json:"inline_data"`
}

type responseCandidate struct {
	Content struct {
		Parts []responsePart `json:"parts"`
	} `json:"content"`
}

type responseData struct {
	B64JSON string `json:"b64_json"`
	B64Alt  string `json:"b64"`
	URL     string `json:"url"`
	Mime    string `json:"mime_type"`
}

type generateResponse struct {
	Candidates []responseCandidate `json:"candidates"`
	Data       []responseData      `json:"data"`
}

type generated struct {
	base64 string
	url    string
	mime   string
}

func (c *Client) request(ctx context.Context, client *http.Client, root, model, key string, payload []byte) (generated, int, error) {
	endpoint := root + generationCodecPath + url.PathEscape(model) + ":generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return generated{}, 0, ErrInvalidBaseURL
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return generated{}, 0, fmt.Errorf("%w: %s", ErrProviderFailure, redact(err.Error(), key))
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if readErr != nil {
		return generated{}, resp.StatusCode, fmt.Errorf("%w: read response", ErrProviderFailure)
	}
	if int64(len(body)) > maxResponseBytes {
		return generated{}, resp.StatusCode, ErrProviderFailure
	}
	if resp.StatusCode != http.StatusOK {
		return generated{}, resp.StatusCode, fmt.Errorf("%w: HTTP %d", ErrProviderFailure, resp.StatusCode)
	}

	var decoded generateResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return generated{}, resp.StatusCode, fmt.Errorf("%w: decode response", ErrProviderFailure)
	}

	out := generated{}
	for _, candidate := range decoded.Candidates {
		for _, part := range candidate.Content.Parts {
			if data := part.InlineData; data != nil && strings.TrimSpace(data.Data) != "" {
				out.base64 = strings.TrimSpace(data.Data)
				out.mime = firstNonEmpty(data.MimeType, data.MimeTypeAlt)
				break
			}
			if data := part.InlineAlt; data != nil && strings.TrimSpace(data.Data) != "" {
				out.base64 = strings.TrimSpace(data.Data)
				out.mime = firstNonEmpty(data.MimeType, data.MimeTypeAlt)
				break
			}
		}
		if out.base64 != "" {
			break
		}
	}
	if out.base64 == "" {
		for _, item := range decoded.Data {
			if b64 := firstNonEmpty(item.B64JSON, item.B64Alt); strings.TrimSpace(b64) != "" {
				out.base64 = strings.TrimSpace(b64)
				out.mime = item.Mime
				break
			}
			if strings.TrimSpace(item.URL) != "" {
				out.url = strings.TrimSpace(item.URL)
				out.mime = item.Mime
				break
			}
		}
	}
	if out.base64 == "" && out.url == "" {
		return generated{}, resp.StatusCode, ErrEmptyResult
	}
	return out, resp.StatusCode, nil
}

func (c *Client) resolve(ctx context.Context, client *http.Client, result generated) ([]byte, string, error) {
	if result.base64 != "" {
		data, err := decodeBase64(result.base64)
		if err != nil {
			return nil, "", fmt.Errorf("%w: decode base64 image", ErrProviderFailure)
		}
		return data, result.mime, nil
	}
	return c.download(ctx, client, result.url, result.mime)
}

func (c *Client) download(ctx context.Context, client *http.Client, rawURL, mime string) ([]byte, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return nil, "", ErrUnsafeImageURL
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http":
	default:
		return nil, "", ErrUnsafeImageURL
	}
	// The provider chooses this URL, so treat it as untrusted input: a host that
	// resolves to loopback, link-local or private space (cloud metadata, local
	// admin ports, LAN services) is refused before any connection is attempted.
	if err := checkDownloadHost(parsed.Hostname()); err != nil {
		return nil, "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", ErrUnsafeImageURL
	}
	resp, err := withoutRedirects(client).Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: download image", ErrProviderFailure)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w: image download HTTP %d", ErrProviderFailure, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("%w: read image", ErrProviderFailure)
	}
	if len(body) > MaxImageBytes {
		return nil, "", ErrImageTooLarge
	}
	if strings.TrimSpace(mime) == "" {
		mime = resp.Header.Get("Content-Type")
	}
	return body, mime, nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Timeout: DefaultTimeout, Transport: transport, CheckRedirect: rejectRedirect}
}

func withoutRedirects(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{Timeout: DefaultTimeout, CheckRedirect: rejectRedirect}
	}
	clone := *client
	clone.CheckRedirect = rejectRedirect
	return &clone
}

// normalizeBaseURL accepts https://host, https://host/, https://host/v1 and
// returns the origin plus any non-API path prefix.
func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalidBaseURL
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", ErrInvalidBaseURL
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", ErrInvalidBaseURL
	}
	if parsed.Host == "" || parsed.User != nil {
		return "", ErrInvalidBaseURL
	}
	path := strings.TrimRight(parsed.Path, "/")
	for _, suffix := range []string{"/v1", "/v1beta"} {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
			break
		}
	}
	path = strings.TrimRight(path, "/")
	return scheme + "://" + parsed.Host + path, nil
}

func alternateModel(model string) (string, bool) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", false
	}
	if strings.HasSuffix(model, "-preview") {
		return strings.TrimSuffix(model, "-preview"), true
	}
	candidate := model + "-preview"
	if len(candidate) > maxModelNameBytes {
		return "", false
	}
	return candidate, true
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusServiceUnavailable:
		return true
	default:
		return false
	}
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if comma := strings.IndexByte(value, ','); comma >= 0 && strings.Contains(value[:comma], "base64") {
		value = value[comma+1:]
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid base64 payload")
}

func normalizeMime(raw string, data []byte) (string, error) {
	mime := strings.ToLower(strings.TrimSpace(raw))
	if index := strings.IndexByte(mime, ';'); index >= 0 {
		mime = strings.TrimSpace(mime[:index])
	}
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	if mime == "image/jpg" {
		mime = "image/jpeg"
	}
	switch mime {
	case "image/png", "image/jpeg", "image/webp":
		return mime, nil
	default:
		return "", ErrUnsupportedMime
	}
}

// resolveDownloadIPs resolves a hostname for the download guard. Tests swap it
// so the download path can be exercised against a local server without a real
// public address; production always uses net.LookupIP.
var resolveDownloadIPs = net.LookupIP

// checkDownloadHost refuses any host that does not resolve exclusively to
// public addresses. Resolution failure is refused too, so an unresolvable name
// never falls through to the dialer with a different answer.
func checkDownloadHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return ErrUnsafeImageURL
	}
	addresses, err := resolveDownloadIPs(host)
	if err != nil || len(addresses) == 0 {
		return ErrUnsafeImageURL
	}
	for _, address := range addresses {
		if !isPublicDownloadIP(address) {
			return ErrUnsafeImageURL
		}
	}
	return nil
}

// isPublicDownloadIP reports whether ip is routable on the public internet.
// Loopback (127/8, ::1), link-local (169.254/16, fe80::/10), private
// (10/8, 172.16/12, 192.168/16, fc00::/7), unspecified (0.0.0.0, ::) and
// multicast addresses are all rejected.
func isPublicDownloadIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	switch {
	case ip.IsLoopback(),
		ip.IsUnspecified(),
		ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(),
		ip.IsInterfaceLocalMulticast(),
		ip.IsMulticast(),
		ip.IsPrivate():
		return false
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func redact(message, secret string) string {
	message = strings.TrimSpace(message)
	secret = strings.TrimSpace(secret)
	if secret == "" || !strings.Contains(message, secret) {
		return message
	}
	return strings.ReplaceAll(message, secret, "[redacted]")
}
