package agnes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

const (
	APIOrigin               = "https://apihub.agnes-ai.com"
	APIBaseURL              = APIOrigin + "/v1"
	ImageGenerationEndpoint = APIBaseURL + "/images/generations"
	VideoCreateEndpoint     = APIBaseURL + "/videos"
	VideoStatusEndpoint     = APIOrigin + "/agnesapi"

	ImageModel20Flash = "agnes-image-2.0-flash"
	ImageModel21Flash = "agnes-image-2.1-flash"
	VideoModel20      = "agnes-video-v2.0"

	DefaultTimeout                 = 6 * time.Minute
	MaxImageRequestBytes           = 32 << 20
	MaxImageResponseBytes          = 128 << 20
	MaxImageArtifactBytes          = 64 << 20
	MaxMetadataRequestBytes        = 1 << 20
	MaxMetadataResponseBytes       = 1 << 20
	MaxVideoDownloadBytes    int64 = 512 << 20
)

var rejectRedirect = func(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

// Client has fixed production endpoints. Endpoint fields are unexported so
// only same-package offline tests can replace them with httptest servers.
type Client struct {
	apiKey string

	httpClient          *http.Client
	imageEndpoint       string
	videoCreateEndpoint string
	videoStatusEndpoint string

	maxImageRequestBytes     int64
	maxImageResponseBytes    int64
	maxImageArtifactBytes    int64
	maxMetadataRequestBytes  int64
	maxMetadataResponseBytes int64
	maxVideoDownloadBytes    int64

	videoDownloadHosts           map[string]struct{}
	allowNonStandardDownloadPort bool
}

// NewClient creates a fixed, direct Agnes client. It never reads proxy or
// provider settings from environment variables.
func NewClient(apiKey string, timeout time.Duration) (*Client, error) {
	key, err := normalizeKey(apiKey)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		apiKey:                   key,
		httpClient:               NewDirectHTTPClient(timeout),
		imageEndpoint:            ImageGenerationEndpoint,
		videoCreateEndpoint:      VideoCreateEndpoint,
		videoStatusEndpoint:      VideoStatusEndpoint,
		maxImageRequestBytes:     MaxImageRequestBytes,
		maxImageResponseBytes:    MaxImageResponseBytes,
		maxImageArtifactBytes:    MaxImageArtifactBytes,
		maxMetadataRequestBytes:  MaxMetadataRequestBytes,
		maxMetadataResponseBytes: MaxMetadataResponseBytes,
		maxVideoDownloadBytes:    MaxVideoDownloadBytes,
		videoDownloadHosts: map[string]struct{}{
			"platform-outputs.agnes-ai.space": {},
		},
	}, nil
}

// NewClientFromKeychain fails closed when secure storage is unavailable.
func NewClientFromKeychain(timeout time.Duration) (*Client, error) {
	key, err := LoadAPIKey()
	if err != nil {
		return nil, err
	}
	return NewClient(key, timeout)
}

// NewDirectHTTPClient disables HTTP_PROXY/HTTPS_PROXY and all redirects.
func NewDirectHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Timeout:       timeout,
		Transport:     transport,
		CheckRedirect: rejectRedirect,
	}
}

func (c *Client) doJSON(ctx context.Context, method, endpoint string, payload any, maxRequest, maxResponse int64, dst any) error {
	if c == nil || c.httpClient == nil || strings.TrimSpace(c.apiKey) == "" {
		return newError(CodeKeyMissing, 0, false, nil)
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return newError(CodeInvalidInput, 0, false, err)
		}
		if int64(len(encoded)) > maxRequest {
			return newError(CodeInvalidInput, 0, false, nil)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return newError(CodeInvalidInput, 0, false, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		retryable := !errors.Is(err, context.Canceled)
		return newError(CodeRequestFailed, 0, retryable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return providerStatusError(resp.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return newError(CodeInvalidResponse, resp.StatusCode, false, err)
	}
	data, err := readLimited(resp.Body, maxResponse)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(dst); err != nil {
		return newError(CodeInvalidResponse, resp.StatusCode, false, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return newError(CodeInvalidResponse, resp.StatusCode, false, err)
	}
	return nil
}

func readLimited(r io.Reader, max int64) ([]byte, error) {
	if max <= 0 {
		return nil, newError(CodeResponseTooLarge, 0, false, nil)
	}
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, newError(CodeRequestFailed, 0, true, err)
	}
	if int64(len(data)) > max {
		return nil, newError(CodeResponseTooLarge, 0, false, nil)
	}
	return data, nil
}

func providerStatusError(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return newError(CodeAuthFailed, status, false, nil)
	case http.StatusPaymentRequired:
		return newError(CodeQuotaExhausted, status, false, nil)
	case http.StatusTooManyRequests:
		return newError(CodeRateLimited, status, true, nil)
	case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return newError(CodeProviderUnavailable, status, true, nil)
	default:
		return newError(CodeProviderRejected, status, false, nil)
	}
}
