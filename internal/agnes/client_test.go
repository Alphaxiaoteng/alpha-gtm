package agnes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const syntheticAPIKey = "synthetic-agnes-key"

func TestOfficialContractIsFixed(t *testing.T) {
	if APIBaseURL != "https://apihub.agnes-ai.com/v1" {
		t.Fatalf("APIBaseURL=%q", APIBaseURL)
	}
	if ImageGenerationEndpoint != "https://apihub.agnes-ai.com/v1/images/generations" {
		t.Fatalf("ImageGenerationEndpoint=%q", ImageGenerationEndpoint)
	}
	if VideoCreateEndpoint != "https://apihub.agnes-ai.com/v1/videos" {
		t.Fatalf("VideoCreateEndpoint=%q", VideoCreateEndpoint)
	}
	if VideoStatusEndpoint != "https://apihub.agnes-ai.com/agnesapi" {
		t.Fatalf("VideoStatusEndpoint=%q", VideoStatusEndpoint)
	}
	if ImageModel20Flash != "agnes-image-2.0-flash" || ImageModel21Flash != "agnes-image-2.1-flash" || VideoModel20 != "agnes-video-v2.0" {
		t.Fatal("official model IDs changed")
	}
}

func TestNewDirectHTTPClientDisablesProxyAndRedirects(t *testing.T) {
	client := NewDirectHTTPClient(time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport=%T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("Agnes HTTP client must not inherit proxy settings")
	}
	if client.CheckRedirect == nil {
		t.Fatal("Agnes HTTP client must reject redirects")
	}
	if err := client.CheckRedirect(&http.Request{}, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect error=%v", err)
	}
}

func TestGenerateImageBase64UsesOfficialShapeAndReturnsDecodedImage(t *testing.T) {
	artifact := testPNG(t)
	var sawRequest atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest.Store(true)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+syntheticAPIKey {
			t.Error("missing bearer authorization")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if bytes.Contains(body, []byte(syntheticAPIKey)) {
			t.Error("API key leaked into JSON body")
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Error(err)
			return
		}
		if payload["model"] != ImageModel21Flash || payload["prompt"] != "a safe synthetic prompt" || payload["size"] != "1K" || payload["ratio"] != "1:1" || payload["return_base64"] != true {
			t.Errorf("payload=%v", payload)
		}
		extra, ok := payload["extra_body"].(map[string]any)
		if !ok || extra["response_format"] != "b64_json" {
			t.Errorf("extra_body=%v", payload["extra_body"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 123,
			"data": []map[string]any{{
				"url":            nil,
				"b64_json":       base64.StdEncoding.EncodeToString(artifact),
				"revised_prompt": "safe revised prompt",
			}},
		})
	}))
	t.Cleanup(server.Close)

	client := newOfflineClient(t, server.Client())
	client.imageEndpoint = server.URL + "/v1/images/generations"
	result, err := client.GenerateImageBase64(context.Background(), ImageRequest{
		Model:  ImageModel21Flash,
		Prompt: "a safe synthetic prompt",
		Size:   "1K",
		Ratio:  "1:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawRequest.Load() {
		t.Fatal("server did not receive image request")
	}
	if !bytes.Equal(result.Data, artifact) || result.MediaType != "image/png" || result.Width != 2 || result.Height != 2 || result.Created != 123 || result.RevisedPrompt != "safe revised prompt" {
		t.Fatalf("result=%+v data=%d", result, len(result.Data))
	}
}

func TestGenerateImageBase64PlacesReferencesInsideExtraBody(t *testing.T) {
	artifact := testPNG(t)
	reference := "data:image/png;base64," + base64.StdEncoding.EncodeToString(artifact)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if _, exists := payload["image"]; exists {
			t.Error("reference image must not be a top-level field")
		}
		extra, ok := payload["extra_body"].(map[string]any)
		images, imagesOK := extra["image"].([]any)
		if !ok || !imagesOK || len(images) != 1 || images[0] != reference {
			t.Errorf("extra_body=%v", payload["extra_body"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 1,
			"data":    []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(artifact)}},
		})
	}))
	t.Cleanup(server.Close)

	client := newOfflineClient(t, server.Client())
	client.imageEndpoint = server.URL
	_, err := client.GenerateImageBase64(context.Background(), ImageRequest{
		Model:  ImageModel21Flash,
		Prompt: "edit the supplied synthetic reference",
		Size:   "1K",
		Images: []string{reference},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGenerateImageURLUsesOfficialShapeAndReturnsOnePublicURL(t *testing.T) {
	artifact := testPNG(t)
	reference := "data:image/png;base64," + base64.StdEncoding.EncodeToString(artifact)
	const providerURL = "https://platform-outputs.agnes-ai.space/images/derived-first-frame.png"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if _, exists := payload["return_base64"]; exists {
			t.Errorf("URL output must omit return_base64: %v", payload["return_base64"])
		}
		extra, ok := payload["extra_body"].(map[string]any)
		images, imagesOK := extra["image"].([]any)
		if !ok || extra["response_format"] != "url" || !imagesOK || len(images) != 1 || images[0] != reference {
			t.Errorf("extra_body=%v", payload["extra_body"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"created": 2,
			"data":    []map[string]any{{"url": providerURL, "revised_prompt": "derived"}},
		})
	}))
	t.Cleanup(server.Close)

	client := newOfflineClient(t, server.Client())
	client.imageEndpoint = server.URL
	result, err := client.GenerateImageURL(context.Background(), ImageRequest{
		Model: ImageModel21Flash, Prompt: "preserve the supplied frame", Size: "1K", Ratio: "3:2", Images: []string{reference},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != providerURL || result.Created != 2 || result.RevisedPrompt != "derived" {
		t.Fatalf("result=%+v", result)
	}
}

func TestGenerateImageURLRejectsMissingMultipleAndUnsafeProviderURLs(t *testing.T) {
	for _, test := range []struct {
		name string
		data []map[string]any
	}{
		{name: "missing", data: []map[string]any{{"url": nil}}},
		{name: "multiple", data: []map[string]any{{"url": "https://cdn.example.com/a.png"}, {"url": "https://cdn.example.com/b.png"}}},
		{name: "loopback", data: []map[string]any{{"url": "https://127.0.0.1/frame.png"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"created": 2, "data": test.data})
			}))
			t.Cleanup(server.Close)
			client := newOfflineClient(t, server.Client())
			client.imageEndpoint = server.URL
			_, err := client.GenerateImageURL(context.Background(), ImageRequest{Model: ImageModel21Flash, Prompt: "synthetic", Size: "1K"})
			if CodeOf(err) != CodeInvalidResponse {
				t.Fatalf("error=%v code=%s", err, CodeOf(err))
			}
		})
	}
}

func TestGenerateImageBase64RejectsOversizedAndInvalidArtifacts(t *testing.T) {
	t.Run("response too large", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, strings.Repeat("x", 65))
		}))
		t.Cleanup(server.Close)
		client := newOfflineClient(t, server.Client())
		client.imageEndpoint = server.URL
		client.maxImageResponseBytes = 64
		_, err := client.GenerateImageBase64(context.Background(), ImageRequest{Model: ImageModel21Flash, Prompt: "synthetic", Size: "1K"})
		if CodeOf(err) != CodeResponseTooLarge {
			t.Fatalf("error=%v code=%s", err, CodeOf(err))
		}
	})

	t.Run("base64 is not an image", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"created": 1,
				"data":    []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString([]byte("not an image"))}},
			})
		}))
		t.Cleanup(server.Close)
		client := newOfflineClient(t, server.Client())
		client.imageEndpoint = server.URL
		_, err := client.GenerateImageBase64(context.Background(), ImageRequest{Model: ImageModel21Flash, Prompt: "synthetic", Size: "1K"})
		if CodeOf(err) != CodeInvalidImageArtifact {
			t.Fatalf("error=%v code=%s", err, CodeOf(err))
		}
	})
}

func TestProviderErrorsAreStableAndDoNotExposeResponseBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"synthetic-provider-private-detail"}`)
	}))
	t.Cleanup(server.Close)
	client := newOfflineClient(t, server.Client())
	client.imageEndpoint = server.URL
	_, err := client.GenerateImageBase64(context.Background(), ImageRequest{Model: ImageModel21Flash, Prompt: "synthetic", Size: "1K"})
	if CodeOf(err) != CodeAuthFailed {
		t.Fatalf("code=%s error=%v", CodeOf(err), err)
	}
	if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "synthetic-provider") {
		t.Fatalf("provider body leaked in error: %q", err.Error())
	}
}

func TestCreateAndGetVideoStatus(t *testing.T) {
	var createCalls atomic.Int32
	var statusCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+syntheticAPIKey {
			t.Error("missing bearer authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/videos":
			createCalls.Add(1)
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
				return
			}
			if payload["model"] != VideoModel20 || payload["num_frames"] != float64(121) || payload["frame_rate"] != float64(24) {
				t.Errorf("payload=%v", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "task_synthetic", "task_id": "task_synthetic", "video_id": "video_synthetic",
				"object": "video", "model": VideoModel20, "status": "queued", "progress": 0,
				"created_at": 123, "seconds": "5.0", "size": "1152x768",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/agnesapi":
			statusCalls.Add(1)
			if r.URL.Query().Get("video_id") != "video_synthetic" {
				t.Errorf("query=%v", r.URL.Query())
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "task_synthetic", "video_id": "video_synthetic", "object": "video", "model": VideoModel20,
				"status": "completed", "progress": 100, "seconds": "5.0", "size": "1152x768",
				"url": "https://platform-outputs.agnes-ai.space/videos/agnes-video-v2.0/video_synthetic.mp4", "error": nil,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := newOfflineClient(t, server.Client())
	client.videoCreateEndpoint = server.URL + "/v1/videos"
	client.videoStatusEndpoint = server.URL + "/agnesapi"
	task, err := client.CreateVideo(context.Background(), VideoRequest{
		Model: VideoModel20, Prompt: "synthetic video prompt", Width: 1152, Height: 768, NumFrames: 121, FrameRate: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.VideoID != "video_synthetic" || task.Status != VideoQueued || task.Seconds != "5.0" || task.Size != "1152x768" {
		t.Fatalf("task=%+v", task)
	}
	result, err := client.GetVideoStatus(context.Background(), task.VideoID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != VideoCompleted || result.Progress != 100 || !strings.HasSuffix(result.URL, ".mp4") {
		t.Fatalf("result=%+v", result)
	}
	if createCalls.Load() != 1 || statusCalls.Load() != 1 {
		t.Fatalf("create=%d status=%d", createCalls.Load(), statusCalls.Load())
	}
}

func TestVideoRequestValidationAndFailedStatusUseStableErrors(t *testing.T) {
	client := newOfflineClient(t, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid request must not reach network")
		return nil, nil
	})})
	_, err := client.CreateVideo(context.Background(), VideoRequest{
		Model: VideoModel20, Prompt: "synthetic", Width: 1152, Height: 768, NumFrames: 120, FrameRate: 24,
	})
	if CodeOf(err) != CodeInvalidInput {
		t.Fatalf("code=%s error=%v", CodeOf(err), err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "task_synthetic", "video_id": "video_synthetic", "object": "video", "model": VideoModel20,
			"status": "failed", "progress": 23, "seconds": "5.0", "size": "1152x768",
			"error": map[string]any{"message": "synthetic-provider-private-detail"},
		})
	}))
	t.Cleanup(server.Close)
	client = newOfflineClient(t, server.Client())
	client.videoStatusEndpoint = server.URL
	status, err := client.GetVideoStatus(context.Background(), "video_synthetic")
	if status.Status != VideoFailed || CodeOf(err) != CodeVideoFailed {
		t.Fatalf("status=%+v code=%s error=%v", status, CodeOf(err), err)
	}
	if strings.Contains(err.Error(), "private") {
		t.Fatalf("provider failure leaked: %q", err.Error())
	}
}

func TestDownloadVideoRequiresAllowedHTTPSMP4AndDoesNotRedirect(t *testing.T) {
	mp4 := syntheticMP4()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/video.mp4" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("provider credential must never be sent to the artifact host")
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(mp4)
	}))
	t.Cleanup(server.Close)

	client := newOfflineClient(t, server.Client())
	configureTLSTestDownload(t, client, server.URL)
	var out bytes.Buffer
	result, err := client.DownloadVideo(context.Background(), server.URL+"/video.mp4", &out)
	if err != nil {
		t.Fatal(err)
	}
	wantHash := sha256.Sum256(mp4)
	if !bytes.Equal(out.Bytes(), mp4) || result.Bytes != int64(len(mp4)) || result.SHA256 != hex.EncodeToString(wantHash[:]) || result.MediaType != "video/mp4" {
		t.Fatalf("result=%+v bytes=%d", result, out.Len())
	}

	var followed atomic.Bool
	redirectServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final.mp4" {
			followed.Store(true)
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(mp4)
			return
		}
		http.Redirect(w, r, "/final.mp4", http.StatusFound)
	}))
	t.Cleanup(redirectServer.Close)
	client = newOfflineClient(t, redirectServer.Client())
	configureTLSTestDownload(t, client, redirectServer.URL)
	out.Reset()
	_, err = client.DownloadVideo(context.Background(), redirectServer.URL+"/video.mp4", &out)
	if CodeOf(err) != CodeDownloadRedirectRejected || followed.Load() {
		t.Fatalf("code=%s followed=%v error=%v", CodeOf(err), followed.Load(), err)
	}
}

func TestDownloadVideoRejectsUnsafeURLAndOversizedBody(t *testing.T) {
	client := newOfflineClient(t, NewDirectHTTPClient(time.Second))
	for _, rawURL := range []string{
		"http://platform-outputs.agnes-ai.space/video.mp4",
		"https://example.com/video.mp4",
		"https://user:password@platform-outputs.agnes-ai.space/video.mp4",
		"https://platform-outputs.agnes-ai.space/video.txt",
	} {
		if _, err := client.DownloadVideo(context.Background(), rawURL, io.Discard); CodeOf(err) != CodeDownloadRejected {
			t.Errorf("url=%q code=%s error=%v", rawURL, CodeOf(err), err)
		}
	}

	mp4 := syntheticMP4()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(mp4)
	}))
	t.Cleanup(server.Close)
	client = newOfflineClient(t, server.Client())
	configureTLSTestDownload(t, client, server.URL)
	client.maxVideoDownloadBytes = int64(len(mp4) - 1)
	var out bytes.Buffer
	_, err := client.DownloadVideo(context.Background(), server.URL+"/video.mp4", &out)
	if CodeOf(err) != CodeResponseTooLarge || out.Len() != 0 {
		t.Fatalf("code=%s bytes=%d error=%v", CodeOf(err), out.Len(), err)
	}
}

func newOfflineClient(t *testing.T, httpClient *http.Client) *Client {
	t.Helper()
	client, err := NewClient(syntheticAPIKey, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.httpClient = httpClient
	client.httpClient.CheckRedirect = rejectRedirect
	return client
}

func configureTLSTestDownload(t *testing.T, client *Client, rawURL string) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	client.videoDownloadHosts = map[string]struct{}{parsed.Hostname(): {}}
	client.allowNonStandardDownloadPort = true
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func syntheticMP4() []byte {
	return []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2'}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
