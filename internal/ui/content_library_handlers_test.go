package ui

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestContentLibraryConfigDefaultsToDataDir(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/content-library/config", nil)
	req.RemoteAddr = "127.0.0.1:41000"
	rec := httptest.NewRecorder()
	server.handleAPIContentLibraryConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var got struct {
		RootDir  string `json:"root_dir"`
		VideoDir string `json:"video_dir"`
		ImageDir string `json:"image_dir"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.RootDir != filepath.Join(dataDir, "content") {
		t.Fatalf("root_dir=%q", got.RootDir)
	}
	if got.VideoDir != filepath.Join(dataDir, "content", "video") {
		t.Fatalf("video_dir=%q", got.VideoDir)
	}
	if got.ImageDir != filepath.Join(dataDir, "content", "image") {
		t.Fatalf("image_dir=%q", got.ImageDir)
	}
}

func TestContentLibraryConfigPersistsAndRejectsProtectedDirectory(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	contentDir := filepath.Join(t.TempDir(), "my-content")
	body, err := json.Marshal(map[string]string{"root_dir": contentDir})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/content-library/config", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:41000"
	req.Host = "127.0.0.1:17321"
	req.Header.Set("Origin", "http://127.0.0.1:17321")
	rec := httptest.NewRecorder()
	server.handleAPIContentLibraryConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, name := range []string{"video", "image"} {
		if _, err := os.Stat(filepath.Join(contentDir, name)); err != nil {
			t.Fatalf("%s directory missing: %v", name, err)
		}
	}
	resolvedContentDir, err := filepath.EvalSymlinks(contentDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	got, err := restarted.contentLibraryConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.RootDir != resolvedContentDir {
		t.Fatalf("persisted root_dir=%q want %q", got.RootDir, resolvedContentDir)
	}

	protected, err := json.Marshal(map[string]string{"root_dir": filepath.Join(t.TempDir(), "profiles")})
	if err != nil {
		t.Fatal(err)
	}
	badReq := httptest.NewRequest(http.MethodPut, "/api/content-library/config", bytes.NewReader(protected))
	badReq.RemoteAddr = "127.0.0.1:41000"
	badReq.Host = "127.0.0.1:17321"
	badReq.Header.Set("Origin", "http://127.0.0.1:17321")
	badRec := httptest.NewRecorder()
	restarted.handleAPIContentLibraryConfig(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("protected directory status=%d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestContentLibraryDirectorySurvivesOtherConfigUpdates(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	contentDir := filepath.Join(t.TempDir(), "local-content")
	if err := server.persistContentLibraryDir(contentDir); err != nil {
		t.Fatal(err)
	}
	browserPath := filepath.Join(t.TempDir(), "fingerprint-chromium")
	if err := os.WriteFile(browserPath, []byte("test browser"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := server.persistBrowserConfig(browserPath, "fingerprint-chromium"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if got, _ := saved["content_library_dir"].(string); got != contentDir {
		t.Fatalf("content_library_dir=%q", got)
	}
	if got, _ := saved["browser_path"].(string); got != browserPath {
		t.Fatalf("browser_path=%q", got)
	}
}

func TestContentLibraryVideoUploadCopiesIntoManagedFolder(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	fixturePath := filepath.Join("..", "..", "e2e", "fixtures", "local-video.mp4")
	fixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}

	req := contentLibraryVideoUploadRequest(t, fixture, "fixture.mp4")
	rec := httptest.NewRecorder()
	server.handleAPIContentLibraryVideoUpload(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var item struct {
		ID       int64  `json:"id"`
		FilePath string `json:"file_path"`
		SHA256   string `json:"sha256"`
		Purpose  string `json:"purpose"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.ID <= 0 || len(item.SHA256) != 64 || item.Purpose != "video" {
		t.Fatalf("unexpected item: %#v", item)
	}
	resolvedDataDir, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(item.FilePath, filepath.Join(resolvedDataDir, "content", "video")+string(filepath.Separator)) {
		t.Fatalf("file_path=%q", item.FilePath)
	}
	copied, err := os.ReadFile(item.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(copied, fixture) {
		t.Fatal("managed video bytes differ from uploaded fixture")
	}

	second := httptest.NewRecorder()
	server.handleAPIContentLibraryVideoUpload(second, contentLibraryVideoUploadRequest(t, fixture, "duplicate.mp4"))
	if second.Code != http.StatusCreated {
		t.Fatalf("duplicate status=%d body=%s", second.Code, second.Body.String())
	}
	var duplicate struct {
		ID       int64  `json:"id"`
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != item.ID || duplicate.FilePath != item.FilePath {
		t.Fatalf("duplicate=%#v first=%#v", duplicate, item)
	}

	bad := httptest.NewRecorder()
	server.handleAPIContentLibraryVideoUpload(bad, contentLibraryVideoUploadRequest(t, []byte("not a video"), "notes.txt"))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("non-video status=%d body=%s", bad.Code, bad.Body.String())
	}

	disguised := httptest.NewRecorder()
	server.handleAPIContentLibraryVideoUpload(disguised, contentLibraryVideoUploadRequest(t, []byte("not a video"), "fake.mp4"))
	if disguised.Code != http.StatusUnprocessableEntity {
		t.Fatalf("disguised video status=%d body=%s", disguised.Code, disguised.Body.String())
	}
}

func TestContentLibraryVideoUploadRejectsExistingSymlink(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "e2e", "fixtures", "local-video.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(fixture)
	videoDir := filepath.Join(dataDir, "content", "video")
	if err := os.MkdirAll(videoDir, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(videoDir, fmt.Sprintf("%x.mp4", hash))
	if err := os.Symlink(outside, destination); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	server.handleAPIContentLibraryVideoUpload(rec, contentLibraryVideoUploadRequest(t, fixture, "fixture.mp4"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("symlink status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestContentLibraryImageUploadCopiesIntoManagedFolder(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	fixture := contentLibraryPNG(t)
	rec := httptest.NewRecorder()
	server.handleAPIContentLibraryImageUpload(rec, contentLibraryImageUploadRequest(t, fixture, "cover.png"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var item struct {
		ID       int64  `json:"id"`
		FilePath string `json:"file_path"`
		SHA256   string `json:"sha256"`
		Purpose  string `json:"purpose"`
		Mime     string `json:"mime"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.ID <= 0 || len(item.SHA256) != 64 || item.Purpose != "shared" || item.Mime != "image/png" {
		t.Fatalf("unexpected item: %#v", item)
	}
	resolvedDataDir, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(item.FilePath, filepath.Join(resolvedDataDir, "content", "image")+string(filepath.Separator)) {
		t.Fatalf("file_path=%q", item.FilePath)
	}
	copied, err := os.ReadFile(item.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(copied, fixture) {
		t.Fatal("managed image bytes differ from uploaded fixture")
	}

	second := httptest.NewRecorder()
	server.handleAPIContentLibraryImageUpload(second, contentLibraryImageUploadRequest(t, fixture, "duplicate.png"))
	if second.Code != http.StatusCreated {
		t.Fatalf("duplicate status=%d body=%s", second.Code, second.Body.String())
	}
	var duplicate struct {
		ID       int64  `json:"id"`
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != item.ID || duplicate.FilePath != item.FilePath {
		t.Fatalf("duplicate=%#v first=%#v", duplicate, item)
	}

	bad := httptest.NewRecorder()
	server.handleAPIContentLibraryImageUpload(bad, contentLibraryImageUploadRequest(t, []byte("not an image"), "fake.png"))
	if bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid image status=%d body=%s", bad.Code, bad.Body.String())
	}
}

func TestContentLibraryImageUploadSharesImageAcrossContentTypes(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	rec := httptest.NewRecorder()
	server.handleAPIContentLibraryImageUpload(rec, contentLibraryImageUploadRequestWithPurpose(t, contentLibraryPNG(t), "cover.png", "article"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var item struct {
		Purpose string `json:"purpose"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.Purpose != "shared" {
		t.Fatalf("purpose=%q", item.Purpose)
	}
}

func TestContentLibraryImageUploadRejectsExistingSymlink(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	fixture := contentLibraryPNG(t)
	hash := sha256.Sum256(fixture)
	imageDir := filepath.Join(dataDir, "content", "image")
	if err := os.MkdirAll(imageDir, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(imageDir, fmt.Sprintf("%x.png", hash))
	if err := os.Symlink(outside, destination); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	server.handleAPIContentLibraryImageUpload(rec, contentLibraryImageUploadRequest(t, fixture, "fixture.png"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("symlink status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func contentLibraryVideoUploadRequest(t *testing.T, content []byte, filename string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/content-library/videos", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.RemoteAddr = "127.0.0.1:41000"
	req.Host = "127.0.0.1:17321"
	req.Header.Set("Origin", "http://127.0.0.1:17321")
	return req
}

func contentLibraryImageUploadRequest(t *testing.T, content []byte, filename string) *http.Request {
	return contentLibraryImageUploadRequestWithPurpose(t, content, filename, "")
}

func contentLibraryImageUploadRequestWithPurpose(t *testing.T, content []byte, filename, purpose string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if purpose != "" {
		if err := form.WriteField("purpose", purpose); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/content-library/images", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.RemoteAddr = "127.0.0.1:41000"
	req.Host = "127.0.0.1:17321"
	req.Header.Set("Origin", "http://127.0.0.1:17321")
	return req
}

func contentLibraryPNG(t *testing.T) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.RGBA{R: 32, G: 160, B: 240, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
