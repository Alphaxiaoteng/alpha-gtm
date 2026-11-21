package imagegen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 20), B: 120, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestGenerateBase64FromCandidates(t *testing.T) {
	pngBytes := testPNG(t)
	encoded := base64.StdEncoding.EncodeToString(pngBytes)
	const key = "sk-test-offline-000"

	var seenPath, seenAuth, seenFormat, seenText, seenRatio string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		var body struct {
			Contents []struct {
				Role  string `json:"role"`
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
			GenerationConfig struct {
				ResponseModalities []string `json:"responseModalities"`
				ImageConfig        struct {
					AspectRatio string `json:"aspectRatio"`
					ImageSize   string `json:"imageSize"`
				} `json:"imageConfig"`
			} `json:"generationConfig"`
			ResponseFormat string `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		seenFormat = body.ResponseFormat
		if len(body.Contents) == 1 && len(body.Contents[0].Parts) == 1 {
			seenText = body.Contents[0].Parts[0].Text
		}
		seenRatio = body.GenerationConfig.ImageConfig.AspectRatio
		if len(body.GenerationConfig.ResponseModalities) != 1 || body.GenerationConfig.ResponseModalities[0] != "IMAGE" {
			t.Errorf("unexpected modalities: %#v", body.GenerationConfig.ResponseModalities)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":%q}}]}}]}`, encoded)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: key, Model: "gemini-3.1-flash-image"}
	data, mime, err := client.Generate(context.Background(), "a ceramic cup", "4:3", "2K")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !bytes.Equal(data, pngBytes) {
		t.Fatalf("image bytes mismatch: got %d bytes", len(data))
	}
	if mime != "image/png" {
		t.Fatalf("mime=%q", mime)
	}
	if seenPath != "/v1beta/models/gemini-3.1-flash-image:generateContent" {
		t.Fatalf("path=%q", seenPath)
	}
	if seenAuth != "Bearer "+key {
		t.Fatalf("authorization header not forwarded as bearer")
	}
	if seenFormat != "b64_json" || seenText != "a ceramic cup" || seenRatio != "4:3" {
		t.Fatalf("unexpected payload format=%q text=%q ratio=%q", seenFormat, seenText, seenRatio)
	}
}

func TestGenerateAcceptsDataB64JSONShape(t *testing.T) {
	pngBytes := testPNG(t)
	encoded := base64.StdEncoding.EncodeToString(pngBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, encoded)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL + "/v1", APIKey: "sk-offline", Model: "gpt-image-2"}
	data, mime, err := client.Generate(context.Background(), "cup", "", "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !bytes.Equal(data, pngBytes) || mime != "image/png" {
		t.Fatalf("unexpected result mime=%q bytes=%d", mime, len(data))
	}
}

func TestGenerateDownloadsURLFallback(t *testing.T) {
	pngBytes := testPNG(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/out.png" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
			return
		}
		fmt.Fprintf(w, `{"data":[{"url":%q}]}`, server.URL+"/out.png")
	}))
	defer server.Close()
	// Loopback downloads are refused by the SSRF guard, so this test stands in a
	// public address for the local server's host. The guard itself is covered by
	// TestDownloadRejectsNonPublicHosts below.
	allowHostForDownload(t, server.URL)

	client := &Client{BaseURL: server.URL, APIKey: "sk-offline", Model: "gemini-3.1-flash-image"}
	data, mime, err := client.Generate(context.Background(), "cup", "1:1", "1K")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !bytes.Equal(data, pngBytes) {
		t.Fatalf("downloaded bytes mismatch: %d", len(data))
	}
	if mime != "image/png" {
		t.Fatalf("mime=%q", mime)
	}
}

// allowHostForDownload makes the download guard accept rawURL's host for the
// duration of one test. Without it the guard cannot be exercised end to end,
// because every httptest server lives on loopback.
func allowHostForDownload(t *testing.T, rawURL string) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test url: %v", err)
	}
	original := resolveDownloadIPs
	resolveDownloadIPs = func(host string) ([]net.IP, error) {
		if host == parsed.Hostname() {
			return []net.IP{net.ParseIP("203.0.113.10")}, nil
		}
		return original(host)
	}
	t.Cleanup(func() { resolveDownloadIPs = original })
}

func TestGenerateRejectsUnsafeURLFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"url":"https://169.254.169.254/latest/meta-data/iam/security-credentials/"}]}`))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "sk-offline", Model: "gemini-3.1-flash-image"}
	_, _, err := client.Generate(context.Background(), "cup", "", "")
	if !errors.Is(err, ErrUnsafeImageURL) {
		t.Fatalf("expected ErrUnsafeImageURL, got %v", err)
	}
}

func TestDownloadRejectsNonPublicHosts(t *testing.T) {
	cases := []string{
		"http://169.254.169.254/latest/meta-data/",  // EC2 metadata, link-local
		"https://169.254.169.254/latest/meta-data/", // same over TLS
		"http://127.0.0.1:8080/admin",               // loopback
		"https://localhost:3000/internal",           // loopback by name
		"https://[::1]/x.png",                       // IPv6 loopback
		"https://[fe80::1]/x.png",                   // IPv6 link-local
		"http://10.1.2.3/x.png",                     // private 10/8
		"http://172.16.5.4/x.png",                   // private 172.16/12
		"http://172.31.255.1/x.png",                 // private 172.16/12 upper bound
		"http://192.168.0.7/x.png",                  // private 192.168/16
		"https://[fc00::1]/x.png",                   // IPv6 unique local
		"http://0.0.0.0:9000/x.png",                 // unspecified
		"file:///etc/passwd",                        // scheme
		"ftp://example.com/x.png",                   // scheme
	}
	client := &Client{BaseURL: "https://image.example", APIKey: "sk-offline", Model: "gpt-image-2"}
	for _, rawURL := range cases {
		t.Run(rawURL, func(t *testing.T) {
			if _, _, err := client.download(context.Background(), nil, rawURL, ""); !errors.Is(err, ErrUnsafeImageURL) {
				t.Fatalf("expected ErrUnsafeImageURL for %q, got %v", rawURL, err)
			}
		})
	}
}

func TestDownloadRejectsUnresolvableHost(t *testing.T) {
	original := resolveDownloadIPs
	resolveDownloadIPs = func(string) ([]net.IP, error) {
		return nil, errors.New("no such host")
	}
	t.Cleanup(func() { resolveDownloadIPs = original })
	client := &Client{BaseURL: "https://image.example", APIKey: "sk-offline", Model: "gpt-image-2"}
	if _, _, err := client.download(context.Background(), nil, "https://cdn.invalid/x.png", ""); !errors.Is(err, ErrUnsafeImageURL) {
		t.Fatalf("expected ErrUnsafeImageURL for a DNS failure, got %v", err)
	}
}

func TestDownloadAllowsPublicHost(t *testing.T) {
	if err := checkDownloadHost("8.8.8.8"); err != nil {
		t.Fatalf("public address rejected: %v", err)
	}
	if err := checkDownloadHost("203.0.113.10"); err != nil {
		t.Fatalf("public address rejected: %v", err)
	}
}

func TestSimpleRejectsReferenceImageOverRequestBudget(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "sk-offline", Model: "gpt-image-2"}
	// 800 KiB base64-expands to ~1.07 MiB, i.e. past the 1 MiB body budget.
	ref := bytes.Repeat([]byte("a"), 800<<10)
	_, _, err := client.Simple(context.Background(), "cup", ref, "image/png")
	if !errors.Is(err, ErrReferenceTooLarge) {
		t.Fatalf("expected ErrReferenceTooLarge, got %v", err)
	}
	if !strings.Contains(err.Error(), "参考图过大，请压缩后重试") {
		t.Fatalf("error does not tell the caller what to do: %v", err)
	}
	// A reference the caller can act on must stay a 400-class error, never an
	// upstream outage.
	if !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("expected the error to be classified as an invalid reference: %v", err)
	}
	if calls != 0 {
		t.Fatalf("oversized reference reached the provider: %d calls", calls)
	}
}

func TestSimpleSendsReferenceImageWithinBudget(t *testing.T) {
	pngBytes := testPNG(t)
	encoded := base64.StdEncoding.EncodeToString(pngBytes)
	var parts int
	var seenRefLen int
	var seenText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Contents []struct {
				Parts []struct {
					Text       string `json:"text"`
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"contents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(body.Contents) == 1 {
			parts = len(body.Contents[0].Parts)
			for _, part := range body.Contents[0].Parts {
				if part.InlineData != nil {
					seenRefLen = len(part.InlineData.Data)
				}
				if part.Text != "" {
					seenText = part.Text
				}
			}
		}
		fmt.Fprintf(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":%q}}]}}]}`, encoded)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "sk-offline", Model: "gemini-3.1-flash-image"}
	data, mime, err := client.Simple(context.Background(), "a ceramic cup", pngBytes, "image/png")
	if err != nil {
		t.Fatalf("Simple: %v", err)
	}
	if !bytes.Equal(data, pngBytes) || mime != "image/png" {
		t.Fatalf("unexpected result mime=%q bytes=%d", mime, len(data))
	}
	if parts != 2 || seenRefLen != len(encoded) || seenText != "a ceramic cup" {
		t.Fatalf("reference image not forwarded ahead of the prompt: parts=%d refLen=%d text=%q", parts, seenRefLen, seenText)
	}
}

func TestGenerateProviderErrorDoesNotLeakKeyOrBody(t *testing.T) {
	const key = "sk-test-offline-do-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"upstream echo %s with secret-provider-body"}`, key)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: key, Model: "gpt-image-2"}
	_, _, err := client.Generate(context.Background(), "cup", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	message := err.Error()
	if strings.Contains(message, key) || strings.Contains(message, "secret-provider-body") {
		t.Fatalf("error leaks secret material: %s", message)
	}
	if !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("expected ErrProviderFailure, got %v", err)
	}
}

func TestGenerateRedactsKeyFromTransportError(t *testing.T) {
	const key = "sk-test-offline-transport"
	client := &Client{
		BaseURL: "https://image.invalid", APIKey: key, Model: "gpt-image-2",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("dial failed while using %s", key)
		})},
	}
	_, _, err := client.Generate(context.Background(), "cup", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("error leaks api key: %s", err.Error())
	}
}

func TestGenerateRejectsInvalidBaseURLAndMissingKey(t *testing.T) {
	cases := []struct {
		name   string
		client *Client
		want   error
	}{
		{name: "empty", client: &Client{APIKey: "sk-x", Model: "gpt-image-2"}, want: ErrInvalidBaseURL},
		{name: "ftp", client: &Client{BaseURL: "ftp://example.com", APIKey: "sk-x", Model: "gpt-image-2"}, want: ErrInvalidBaseURL},
		{name: "file", client: &Client{BaseURL: "file:///etc/passwd", APIKey: "sk-x", Model: "gpt-image-2"}, want: ErrInvalidBaseURL},
		{name: "userinfo", client: &Client{BaseURL: "https://user:pass@example.com", APIKey: "sk-x", Model: "gpt-image-2"}, want: ErrInvalidBaseURL},
		{name: "missing-key", client: &Client{BaseURL: "https://example.com", Model: "gpt-image-2"}, want: ErrKeyMissing},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, _, err := testCase.client.Generate(context.Background(), "cup", "", ""); !errors.Is(err, testCase.want) {
				t.Fatalf("expected %v, got %v", testCase.want, err)
			}
		})
	}
}

func TestGenerateTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := &Client{
		BaseURL: server.URL, APIKey: "sk-offline", Model: "gpt-image-2",
		HTTPClient: &http.Client{Timeout: 50 * time.Millisecond},
	}
	_, _, err := client.Generate(context.Background(), "cup", "", "")
	if !errors.Is(err, ErrProviderFailure) {
		t.Fatalf("expected ErrProviderFailure on timeout, got %v", err)
	}
}

func TestGenerateRetriesWithPreviewSuffix(t *testing.T) {
	pngBytes := testPNG(t)
	encoded := base64.StdEncoding.EncodeToString(pngBytes)
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		index := len(paths)
		mu.Unlock()
		if index == 1 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"model_not_found"}`))
			return
		}
		fmt.Fprintf(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":%q}}]}}]}`, encoded)
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "sk-offline", Model: "gemini-3.1-flash-image"}
	data, _, err := client.Generate(context.Background(), "cup", "", "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !bytes.Equal(data, pngBytes) {
		t.Fatalf("bytes mismatch")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 {
		t.Fatalf("expected 2 attempts, got %v", paths)
	}
	if paths[0] != "/v1beta/models/gemini-3.1-flash-image:generateContent" ||
		paths[1] != "/v1beta/models/gemini-3.1-flash-image-preview:generateContent" {
		t.Fatalf("unexpected retry paths: %v", paths)
	}
}

func TestGenerateRejectsEmptyAndUnsupportedMime(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[]}`))
	}))
	defer empty.Close()
	client := &Client{BaseURL: empty.URL, APIKey: "sk-offline", Model: "gpt-image-2"}
	if _, _, err := client.Generate(context.Background(), "cup", "", ""); !errors.Is(err, ErrEmptyResult) {
		t.Fatalf("expected ErrEmptyResult, got %v", err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("not an image"))
	text := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"text/plain","data":%q}}]}}]}`, encoded)
	}))
	defer text.Close()
	client = &Client{BaseURL: text.URL, APIKey: "sk-offline", Model: "gpt-image-2"}
	if _, _, err := client.Generate(context.Background(), "cup", "", ""); !errors.Is(err, ErrUnsupportedMime) {
		t.Fatalf("expected ErrUnsupportedMime, got %v", err)
	}
}

func TestGenerateRequiresPrompt(t *testing.T) {
	client := &Client{BaseURL: "https://example.com", APIKey: "sk-offline", Model: "gpt-image-2"}
	if _, _, err := client.Generate(context.Background(), "   ", "", ""); !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("expected ErrPromptRequired, got %v", err)
	}
}

func TestIsSupportedModelAcceptsPreviewVariants(t *testing.T) {
	for _, model := range []string{"gpt-image-2", "gpt-image-2.6", "gemini-3-pro-image", "gemini-3.1-flash-image", "gemini-3.1-flash-image-preview", "gemini-3-pro-image-preview"} {
		if !IsSupportedModel(model) {
			t.Fatalf("expected %q to be supported", model)
		}
	}
	for _, model := range []string{"", "agnes-image-2.1-flash", "deepseek-v4-pro"} {
		if IsSupportedModel(model) {
			t.Fatalf("expected %q to be rejected", model)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
