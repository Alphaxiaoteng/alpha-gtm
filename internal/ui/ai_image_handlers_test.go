package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/imagegen"
)

func writeAIImageFixture(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 20), B: 180, A: 255})
		}
	}
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
}

func callAIImageAPI(t *testing.T, server *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, reader)
	request.RemoteAddr = "127.0.0.1:41000"
	request.Host = "127.0.0.1:17321"
	request.Header.Set("Origin", "http://127.0.0.1:17321")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	server.ServeHTTP(recorder, request)
	return recorder
}

func TestAIImageConfigImportsManagedAssetsAndPlansHonestly(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.agnesAPIKeyConfigured = func() (bool, error) { return false, nil }

	configRecorder := callAIImageAPI(t, server, http.MethodGet, "/api/ai-image/config", nil)
	if configRecorder.Code != http.StatusOK {
		t.Fatalf("GET config status=%d body=%s", configRecorder.Code, configRecorder.Body.String())
	}
	var defaultConfig map[string]any
	if err := json.Unmarshal(configRecorder.Body.Bytes(), &defaultConfig); err != nil {
		t.Fatal(err)
	}
	if defaultConfig["provider"] != "agnes" || defaultConfig["image_model"] != "agnes-image-2.1-flash" || defaultConfig["video_model"] != "agnes-video-v2.0" || defaultConfig["provider_status"] != "credential_missing" || defaultConfig["asset_dir"] == "" {
		t.Fatalf("unexpected default config: %#v", defaultConfig)
	}

	assetDir := filepath.Join(dataDir, "creative-assets")
	updateRecorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/config", map[string]any{
		"provider":    "agnes",
		"image_model": "agnes-image-2.0-flash",
		"video_model": "agnes-video-v2.0",
		"asset_dir":   assetDir,
	})
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("PUT config status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	if info, err := os.Stat(assetDir); err != nil || !info.IsDir() {
		t.Fatalf("asset directory not created: info=%v err=%v", info, err)
	}

	sourcePath := filepath.Join(t.TempDir(), "product.png")
	writeAIImageFixture(t, sourcePath)
	importRecorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/assets", map[string]any{"path": sourcePath})
	if importRecorder.Code != http.StatusCreated {
		t.Fatalf("POST asset status=%d body=%s", importRecorder.Code, importRecorder.Body.String())
	}
	var asset map[string]any
	if err := json.Unmarshal(importRecorder.Body.Bytes(), &asset); err != nil {
		t.Fatal(err)
	}
	name, _ := asset["name"].(string)
	if name == "" || strings.Contains(importRecorder.Body.String(), sourcePath) || strings.Contains(importRecorder.Body.String(), assetDir) {
		t.Fatalf("asset response leaks paths or lacks name: %s", importRecorder.Body.String())
	}
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("source file was moved or removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(assetDir, name)); err != nil {
		t.Fatalf("managed copy missing: %v", err)
	}

	listRecorder := callAIImageAPI(t, server, http.MethodGet, "/api/ai-image/assets", nil)
	if listRecorder.Code != http.StatusOK || !strings.Contains(listRecorder.Body.String(), name) {
		t.Fatalf("GET assets status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	fileRecorder := callAIImageAPI(t, server, http.MethodGet, "/api/ai-image/assets/"+name, nil)
	if fileRecorder.Code != http.StatusOK || fileRecorder.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("GET asset file status=%d content-type=%q", fileRecorder.Code, fileRecorder.Header().Get("Content-Type"))
	}

	invalidPlan := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/plan", map[string]any{
		"mode": "single", "category": "ring", "composition": "still_life", "product_asset_name": "",
	})
	if invalidPlan.Code != http.StatusBadRequest || !strings.Contains(invalidPlan.Body.String(), "missing_product_reference") {
		t.Fatalf("invalid plan status=%d body=%s", invalidPlan.Code, invalidPlan.Body.String())
	}
	validPlan := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/plan", map[string]any{
		"mode": "grid-9", "category": "ring", "product_asset_name": name, "prompt": "黑色丝绒背景，真实金属质感",
	})
	if validPlan.Code != http.StatusOK || !strings.Contains(validPlan.Body.String(), `"status":"prompt_only"`) || !strings.Contains(validPlan.Body.String(), `"artifact_paths":[]`) {
		t.Fatalf("valid plan status=%d body=%s", validPlan.Code, validPlan.Body.String())
	}
	if !strings.Contains(validPlan.Body.String(), "6 格真实佩戴图") || !strings.Contains(validPlan.Body.String(), "3 格完整静物图") {
		t.Fatalf("ring grid plan lost its fixed composition: %s", validPlan.Body.String())
	}
}

func TestAIImageWorkspacePersistsPromptsAndCanvas(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	body := map[string]any{
		"prompts":  []map[string]any{{"id": "prompt-1", "title": "静物棚拍", "text": "完整产品，四周留白"}},
		"nodes":    []map[string]any{{"id": "node-1", "kind": "prompt", "text": "完整产品，四周留白", "x": 120, "y": 80, "width": 260, "height": 160}},
		"viewport": map[string]any{"x": 20, "y": -10, "zoom": 1.1},
	}
	putRecorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/workspace", body)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT workspace status=%d body=%s", putRecorder.Code, putRecorder.Body.String())
	}
	getRecorder := callAIImageAPI(t, server, http.MethodGet, "/api/ai-image/workspace", nil)
	if getRecorder.Code != http.StatusOK || !strings.Contains(getRecorder.Body.String(), "静物棚拍") || !strings.Contains(getRecorder.Body.String(), "node-1") {
		t.Fatalf("GET workspace status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dataDir, "ai-image", "workspace.json")); err != nil {
		t.Fatalf("workspace file missing: %v", err)
	}
	unknownRecorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/workspace", map[string]any{
		"prompts": []any{}, "nodes": []any{}, "viewport": map[string]any{"x": 0, "y": 0, "zoom": 1}, "script": "ignored",
	})
	if unknownRecorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown workspace field status=%d body=%s", unknownRecorder.Code, unknownRecorder.Body.String())
	}
}

func TestAIImageProjectsPersistIndependentCanvasesAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}

	listRecorder := callAIImageAPI(t, server, http.MethodGet, "/api/ai-image/projects", nil)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list projects status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var projects []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &projects); err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID <= 0 {
		t.Fatalf("default projects=%+v", projects)
	}

	createRecorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/projects", map[string]string{"name": "第二个项目"})
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create project status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID <= 0 || created.ID == projects[0].ID {
		t.Fatalf("created project=%+v default=%+v", created, projects[0])
	}

	secondWorkspace := map[string]any{
		"name": "第二个项目",
		"workspace": map[string]any{
			"prompts":  []any{},
			"nodes":    []map[string]any{{"id": "project-two-node", "kind": "prompt", "text": "只属于第二个项目", "x": 20, "y": 30, "width": 260, "height": 160}},
			"viewport": map[string]any{"x": 5, "y": -8, "zoom": 1.2},
		},
	}
	saveRecorder := callAIImageAPI(t, server, http.MethodPut, fmt.Sprintf("/api/ai-image/projects/%d", created.ID), secondWorkspace)
	if saveRecorder.Code != http.StatusOK {
		t.Fatalf("save project status=%d body=%s", saveRecorder.Code, saveRecorder.Body.String())
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	getRecorder := callAIImageAPI(t, restarted, http.MethodGet, fmt.Sprintf("/api/ai-image/projects/%d", created.ID), nil)
	if getRecorder.Code != http.StatusOK || !strings.Contains(getRecorder.Body.String(), "project-two-node") || !strings.Contains(getRecorder.Body.String(), "只属于第二个项目") {
		t.Fatalf("restarted project status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	defaultRecorder := callAIImageAPI(t, restarted, http.MethodGet, fmt.Sprintf("/api/ai-image/projects/%d", projects[0].ID), nil)
	if defaultRecorder.Code != http.StatusOK || strings.Contains(defaultRecorder.Body.String(), "project-two-node") {
		t.Fatalf("default project leaked second canvas status=%d body=%s", defaultRecorder.Code, defaultRecorder.Body.String())
	}
}

func TestAIImageWorkspaceValidatesStableCanvasReferenceLabels(t *testing.T) {
	workspace := aiImageWorkspace{
		Nodes: []aiImageCanvasNode{
			{ID: "image-1", Kind: "image", AssetName: "one.png", RefLabel: "图01", Width: 260, Height: 260},
			{ID: "video-1", Kind: "video", AssetName: "one.mp4", RefLabel: "视频01", Width: 320, Height: 220},
		},
		Viewport: aiImageViewport{Zoom: 1},
	}
	if err := validateAIImageWorkspace(&workspace); err != nil {
		t.Fatalf("valid reference labels rejected: %v", err)
	}
	workspace.Nodes[1].RefLabel = "图01"
	if err := validateAIImageWorkspace(&workspace); err == nil {
		t.Fatal("wrong-kind duplicate reference label accepted")
	}
}

func TestAIImageRejectsProtectedSourceAndPreservesIndependentFields(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	assetDir := filepath.Join(dataDir, "assets")
	configRecorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/config", map[string]any{"provider": "", "model": "", "asset_dir": assetDir})
	if configRecorder.Code != http.StatusOK {
		t.Fatalf("PUT config status=%d body=%s", configRecorder.Code, configRecorder.Body.String())
	}
	protectedDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(protectedDir, 0700); err != nil {
		t.Fatal(err)
	}
	protectedImage := filepath.Join(protectedDir, "reference.png")
	writeAIImageFixture(t, protectedImage)
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/assets", map[string]any{"path": protectedImage})
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "protected") {
		t.Fatalf("protected import status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	result, status := server.prepareAIImagePlan("single", "necklace", "invalid", "integrated_double", "", "", "", "", "")
	if status != http.StatusBadRequest || result.ErrorCode == nil || *result.ErrorCode != "unsupported_composition" || result.Variant == nil || *result.Variant != "integrated_double" {
		t.Fatalf("independent validated variant was not preserved: %#v", result)
	}
}

func TestAIImageConfigSurvivesRestart(t *testing.T) {
	dataDir := t.TempDir()
	assetDir := filepath.Join(dataDir, "creative-assets")
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	recorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/config", map[string]any{"provider": "agnes", "image_model": "agnes-image-2.0-flash", "video_model": "agnes-video-v2.0", "asset_dir": assetDir})
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT config status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	getRecorder := callAIImageAPI(t, restarted, http.MethodGet, "/api/ai-image/config", nil)
	if getRecorder.Code != http.StatusOK || !strings.Contains(getRecorder.Body.String(), `"provider":"agnes"`) || !strings.Contains(getRecorder.Body.String(), `"image_model":"agnes-image-2.0-flash"`) || !strings.Contains(getRecorder.Body.String(), `"video_model":"agnes-video-v2.0"`) {
		t.Fatalf("restarted config status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
}

func uploadAIImageAsset(t *testing.T, server *Server, sourcePath string) *httptest.ResponseRecorder {
	t.Helper()
	file, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(sourcePath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, file); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/ai-image/assets/upload", &body)
	req.RemoteAddr = "127.0.0.1:41000"
	req.Host = "127.0.0.1:17321"
	req.Header.Set("Origin", "http://127.0.0.1:17321")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	return recorder
}

func TestAIImageMultipartUploadRequiresLoopbackSameOrigin(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	imagePath := filepath.Join(t.TempDir(), "reference.png")
	writeAIImageFixture(t, imagePath)
	data, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}

	request := func(remoteAddr, origin string) *http.Request {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "reference.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/ai-image/assets/upload", &body)
		req.RemoteAddr = remoteAddr
		req.Host = "127.0.0.1:17321"
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return req
	}

	for _, req := range []*http.Request{
		request("192.0.2.10:41000", "http://127.0.0.1:17321"),
		request("127.0.0.1:41000", "https://malicious.example"),
	} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("untrusted upload status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	assets, err := server.listAIImageAssets()
	if err != nil || len(assets) != 0 {
		t.Fatalf("untrusted upload persisted assets=%+v err=%v", assets, err)
	}
}

func TestAIImageMultipartUploadsImageAndRealVideo(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	imagePath := filepath.Join(t.TempDir(), "reference.png")
	writeAIImageFixture(t, imagePath)
	imageRecorder := uploadAIImageAsset(t, server, imagePath)
	if imageRecorder.Code != http.StatusCreated || !strings.Contains(imageRecorder.Body.String(), `"kind":"image"`) {
		t.Fatalf("image upload status=%d body=%s", imageRecorder.Code, imageRecorder.Body.String())
	}

	videoPath := filepath.Join("..", "..", "e2e", "fixtures", "local-video.mp4")
	videoRecorder := uploadAIImageAsset(t, server, videoPath)
	if videoRecorder.Code != http.StatusCreated || !strings.Contains(videoRecorder.Body.String(), `"kind":"video"`) || !strings.Contains(videoRecorder.Body.String(), `"mime_type":"video/mp4"`) {
		t.Fatalf("video upload status=%d body=%s", videoRecorder.Code, videoRecorder.Body.String())
	}
	var videoAsset struct {
		Name       string `json:"name"`
		DurationMS int    `json:"duration_ms"`
	}
	if err := json.Unmarshal(videoRecorder.Body.Bytes(), &videoAsset); err != nil {
		t.Fatal(err)
	}
	if videoAsset.Name == "" || videoAsset.DurationMS <= 0 {
		t.Fatalf("video asset missing durable metadata: %+v", videoAsset)
	}
	fileRecorder := callAIImageAPI(t, server, http.MethodGet, "/api/ai-image/assets/"+videoAsset.Name, nil)
	if fileRecorder.Code != http.StatusOK || fileRecorder.Header().Get("Content-Type") != "video/mp4" || fileRecorder.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("GET video status=%d content-type=%q ranges=%q", fileRecorder.Code, fileRecorder.Header().Get("Content-Type"), fileRecorder.Header().Get("Accept-Ranges"))
	}

	workspace := map[string]any{
		"prompts":  []any{},
		"nodes":    []map[string]any{{"id": "video-1", "kind": "video", "asset_name": videoAsset.Name, "x": 20, "y": 20, "width": 480, "height": 270}},
		"viewport": map[string]any{"x": 0, "y": 0, "zoom": 1},
	}
	workspaceRecorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/workspace", workspace)
	if workspaceRecorder.Code != http.StatusOK {
		t.Fatalf("video workspace status=%d body=%s", workspaceRecorder.Code, workspaceRecorder.Body.String())
	}
}

func TestAIImageConfigRejectsUnknownModels(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	assetDir := filepath.Join(t.TempDir(), "assets")
	for _, body := range []map[string]any{
		{"provider": "other", "image_model": "agnes-image-2.1-flash", "video_model": "agnes-video-v2.0", "asset_dir": assetDir},
		{"provider": "agnes", "image_model": "arbitrary-image-model", "video_model": "agnes-video-v2.0", "asset_dir": assetDir},
		{"provider": "agnes", "image_model": "agnes-image-2.1-flash", "video_model": "arbitrary-video-model", "asset_dir": assetDir},
	} {
		recorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/config", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("unsafe provider/model accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}

// The settings panel saves a custom image provider as {provider, base_url}.
// The zexapi base_url must reach the image_api_base_url key that the generate
// handler actually reads, because /api/ai/config is a wholesale replace and
// must not be touched by the provider card.
func TestAIImageConfigPersistsZexAPIBaseURL(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	assetDir := filepath.Join(dataDir, "creative-assets")
	okRecorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/config", map[string]any{
		"provider":    "zexapi",
		"base_url":    "https://api.zexapi.com/",
		"image_model": "gemini-3.1-flash-image",
		"video_model": "agnes-video-v2.0",
		"asset_dir":   assetDir,
	})
	if okRecorder.Code != http.StatusOK {
		t.Fatalf("zexapi PUT status=%d body=%s", okRecorder.Code, okRecorder.Body.String())
	}
	if got := server.cfg.ImageAPIBaseURL; got != "https://api.zexapi.com" {
		t.Fatalf("in-memory image_api_base_url not updated: %q", got)
	}

	// A fresh server on the same data dir proves the base_url was persisted
	// under the image_api_base_url key, not just mutated in memory.
	restarted, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if got := restarted.cfg.ImageAPIBaseURL; got != "https://api.zexapi.com" {
		t.Fatalf("persisted image_api_base_url missing after restart: %q", got)
	}

	// Remote http base URLs and malformed URLs fail closed.
	for _, bad := range []string{"http://evil.example.test", "notaurl", "https://api.zexapi.com?q=1", "https://api.zexapi.com/path#x"} {
		recorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/config", map[string]any{
			"provider":    "zexapi",
			"base_url":    bad,
			"image_model": "gemini-3.1-flash-image",
			"video_model": "agnes-video-v2.0",
			"asset_dir":   assetDir,
		})
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("unsafe base_url %q accepted: status=%d body=%s", bad, recorder.Code, recorder.Body.String())
		}
	}
	// Loopback http is allowed for local test providers.
	loopback := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/config", map[string]any{
		"provider":    "zexapi",
		"base_url":    "http://127.0.0.1:9999",
		"image_model": "gemini-3.1-flash-image",
		"video_model": "agnes-video-v2.0",
		"asset_dir":   assetDir,
	})
	if loopback.Code != http.StatusOK {
		t.Fatalf("loopback http base_url rejected: status=%d body=%s", loopback.Code, loopback.Body.String())
	}
}

// PersonAgent only needs the minimal image surface (provider config, credential,
// generate, reference upload, asset read). It must not depend on the full AI
// image workspace flag: a default install has AIImageEnabled=false, and every
// PersonAgent card would 404 without this split.
func TestAIImagePersonAgentSurfaceWorksWithoutWorkspaceFlag(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	getRecorder := callAIImageAPI(t, server, http.MethodGet, "/api/ai-image/config", nil)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET config status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}

	// Exactly the shape the settings panel sends: provider + base_url only.
	// image_model / video_model / asset_dir must fall back safely.
	putRecorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/config", map[string]any{
		"provider": "zexapi",
		"base_url": "https://api.zexapi.com",
	})
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT config status=%d body=%s", putRecorder.Code, putRecorder.Body.String())
	}
	var saved map[string]any
	if err := json.Unmarshal(putRecorder.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved["provider"] != "zexapi" || saved["image_model"] != imagegen.DefaultModel {
		t.Fatalf("partial zexapi update fell back to the wrong model: %#v", saved)
	}

	// The route must exist and fail closed on the missing key, not 404.
	genRecorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generate", map[string]any{"prompt": "a cup"})
	if genRecorder.Code == http.StatusNotFound {
		t.Fatalf("generate route missing when workspace flag is off: %s", genRecorder.Body.String())
	}
	if genRecorder.Code != http.StatusBadRequest {
		t.Fatalf("generate should fail closed without key, status=%d body=%s", genRecorder.Code, genRecorder.Body.String())
	}
}

// zexapi's generate handler reads cfg.ImageAPIKey. Storing the key in the Agnes
// keychain would leave the provider permanently unconfigured, so the credential
// endpoint must branch on the active provider.
func TestZexAPICredentialLandsInConfigNotKeychain(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	keychainCalls := 0
	server.agnesSaveAPIKey = func(string) error {
		keychainCalls++
		return nil
	}

	putRecorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/config", map[string]any{
		"provider":    "zexapi",
		"base_url":    "https://api.zexapi.com",
		"image_model": "gemini-3.1-flash-image",
		"video_model": "agnes-video-v2.0",
		"asset_dir":   filepath.Join(dataDir, "zexapi-assets"),
	})
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT config status=%d body=%s", putRecorder.Code, putRecorder.Body.String())
	}

	credentialRecorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/credential", map[string]string{
		"api_key": "sk-test-offline-zexapi",
	})
	if credentialRecorder.Code != http.StatusOK {
		t.Fatalf("PUT credential status=%d body=%s", credentialRecorder.Code, credentialRecorder.Body.String())
	}
	if keychainCalls != 0 {
		t.Fatalf("zexapi key was written to the Agnes keychain (%d calls)", keychainCalls)
	}
	if got := server.cfg.ImageAPIKey; got != "sk-test-offline-zexapi" {
		t.Fatalf("cfg.ImageAPIKey = %q, want the zexapi key", got)
	}

	restarted, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if got := restarted.cfg.ImageAPIKey; got != "sk-test-offline-zexapi" {
		t.Fatalf("zexapi key did not persist: %q", got)
	}
}
