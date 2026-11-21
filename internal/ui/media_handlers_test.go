package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/media"
)

func TestValidateLocalMediaPathRejectsProtectedAndUnsupportedInputs(t *testing.T) {
	if _, err := validateLocalMediaPath("/tmp/.turbopush/session.jpg"); err == nil {
		t.Fatal("protected location was accepted")
	}
	if _, err := validateLocalMediaPath("/tmp/not-media.txt"); err == nil {
		t.Fatal("unsupported extension was accepted")
	}
}

func TestContentCanAttachACompatibleExistingLibraryItem(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	path := filepath.Join(t.TempDir(), "library-video.mp4")
	if err := os.WriteFile(path, []byte("local-video-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	importBody, _ := json.Marshal(map[string]string{"path": path, "purpose": "video"})
	importRecorder := httptest.NewRecorder()
	server.ServeHTTP(importRecorder, httptest.NewRequest(http.MethodPost, "/api/media/library", bytes.NewReader(importBody)))
	if importRecorder.Code != http.StatusCreated {
		t.Fatalf("import status=%d body=%s", importRecorder.Code, importRecorder.Body.String())
	}
	var library struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(importRecorder.Body).Decode(&library); err != nil || library.ID == 0 {
		t.Fatalf("library=%+v err=%v", library, err)
	}
	content, _ := server.db.CreateContent("video", "视频", "", "正文", `[]`, "draft")
	rawPathBody, _ := json.Marshal(map[string]string{"path": path})
	rawPathRecorder := httptest.NewRecorder()
	server.ServeHTTP(rawPathRecorder, httptest.NewRequest(http.MethodPost, "/api/contents/"+strconv.FormatInt(content.ID, 10)+"/media", bytes.NewReader(rawPathBody)))
	if rawPathRecorder.Code != http.StatusBadRequest {
		t.Fatalf("raw path attach status=%d body=%s", rawPathRecorder.Code, rawPathRecorder.Body.String())
	}
	attachBody, _ := json.Marshal(map[string]any{"library_id": library.ID, "role": "content", "position": 3})
	mixedBody, _ := json.Marshal(map[string]any{"library_id": library.ID, "path": path})
	mixedRecorder := httptest.NewRecorder()
	server.ServeHTTP(mixedRecorder, httptest.NewRequest(http.MethodPost, "/api/contents/"+strconv.FormatInt(content.ID, 10)+"/media", bytes.NewReader(mixedBody)))
	if mixedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("mixed attach status=%d body=%s", mixedRecorder.Code, mixedRecorder.Body.String())
	}
	attachRecorder := httptest.NewRecorder()
	server.ServeHTTP(attachRecorder, httptest.NewRequest(http.MethodPost, "/api/contents/"+strconv.FormatInt(content.ID, 10)+"/media", bytes.NewReader(attachBody)))
	if attachRecorder.Code != http.StatusCreated {
		t.Fatalf("attach status=%d body=%s", attachRecorder.Code, attachRecorder.Body.String())
	}
	assets, err := server.db.ListMediaByContent(content.ID)
	if err != nil || len(assets) != 1 || assets[0].LibraryID != library.ID || assets[0].SHA256 == "" || assets[0].Role != "content" || assets[0].Position != 3 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}

	article, _ := server.db.CreateContent("article", "文章", "", "正文", `[]`, "draft")
	mismatchRecorder := httptest.NewRecorder()
	server.ServeHTTP(mismatchRecorder, httptest.NewRequest(http.MethodPost, "/api/contents/"+strconv.FormatInt(article.ID, 10)+"/media", bytes.NewReader(attachBody)))
	if mismatchRecorder.Code != http.StatusBadRequest {
		t.Fatalf("mismatch status=%d body=%s", mismatchRecorder.Code, mismatchRecorder.Body.String())
	}
}

func TestVideoContentCanAttachSeparateLibraryCover(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	payload := contentLibraryPNG(t)
	path := filepath.Join(t.TempDir(), "video-cover.png")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := media.Probe(path)
	if err != nil {
		t.Fatal(err)
	}
	library, err := server.db.UpsertMediaLibrary(path, meta.SHA256, meta.SizeBytes, meta.MimeGuess, meta.Width, meta.Height, meta.DurationMS, meta.ProbeJSON)
	if err != nil {
		t.Fatal(err)
	}
	content, err := server.db.CreateContent("video", "视频", "", "", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(t.TempDir(), "video-body.mp4")
	if err := os.WriteFile(videoPath, []byte("local-video-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	videoMeta, err := media.Probe(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	videoLibrary, err := server.db.UpsertMediaLibrary(videoPath, videoMeta.SHA256, videoMeta.SizeBytes, videoMeta.MimeGuess, videoMeta.Width, videoMeta.Height, videoMeta.DurationMS, videoMeta.ProbeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateMediaAssetFromLibraryWithRole(content.ID, videoLibrary.ID, "content", 0); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"library_id": library.ID, "role": "cover", "position": 0})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/contents/"+strconv.FormatInt(content.ID, 10)+"/media", bytes.NewReader(body)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("attach cover status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	assets, err := server.db.ListMediaByContent(content.ID)
	if err != nil || len(assets) != 2 || assets[0].Kind != "image" || assets[0].Role != "cover" {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	storedContent, err := server.db.GetContent(content.ID)
	if err != nil || storedContent.CoverMediaID != assets[0].ID || storedContent.CoverKind != "image" {
		t.Fatalf("content=%+v assets=%+v err=%v", storedContent, assets, err)
	}
}

func TestContentMediaListReturnsEmptyArrayForTextOnlyContent(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	content, err := server.db.CreateContent("article", "无素材文章", "", "正文", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/contents/"+strconv.FormatInt(content.ID, 10)+"/media", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "[]\n" {
		t.Fatalf("empty content media must be an array, got %q", recorder.Body.String())
	}
}

func TestContentMediaPreviewAndListCoverMetadata(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	payload := contentLibraryPNG(t)
	path := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := server.db.CreateContent("graph_text", "带封面的内容", "", "", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := server.db.CreateMediaAsset(content.ID, "image", path, path, "image/png", int64(len(payload)), "", 2, 2, 0)
	if err != nil {
		t.Fatal(err)
	}

	preview := httptest.NewRecorder()
	server.ServeHTTP(preview, httptest.NewRequest(http.MethodGet, "/api/media/"+strconv.FormatInt(asset.ID, 10)+"/content", nil))
	if preview.Code != http.StatusOK || preview.Header().Get("Content-Type") != "image/png" || preview.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("preview status=%d type=%q nosniff=%q body=%s", preview.Code, preview.Header().Get("Content-Type"), preview.Header().Get("X-Content-Type-Options"), preview.Body.String())
	}
	if !bytes.Equal(preview.Body.Bytes(), payload) {
		t.Fatal("preview payload changed")
	}

	contents := httptest.NewRecorder()
	server.ServeHTTP(contents, httptest.NewRequest(http.MethodGet, "/api/contents", nil))
	if contents.Code != http.StatusOK || !strings.Contains(contents.Body.String(), `"cover_media_id":`+strconv.FormatInt(asset.ID, 10)) || !strings.Contains(contents.Body.String(), `"cover_kind":"image"`) {
		t.Fatalf("contents status=%d body=%s", contents.Code, contents.Body.String())
	}
}
