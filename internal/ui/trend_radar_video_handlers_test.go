package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

func TestTrendRadarVideoUploadUsesDedicatedAppendOnlyWorkspace(t *testing.T) {
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
	recorder := httptest.NewRecorder()
	server.handleAPITrendRadarVideos(recorder, contentLibraryVideoUploadRequest(t, fixture, "radar.mp4"))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var record radarVideoRecord
	if err := json.Unmarshal(recorder.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dataDir, "trend-radar", "video-deconstruct")
	if record.ID == "" || !strings.HasPrefix(record.SourcePath, root+string(os.PathSeparator)) ||
		record.TranscriptionState != "waiting_local_whisper" {
		t.Fatalf("unexpected record: %#v", record)
	}
	for _, path := range []string{record.SourcePath, filepath.Join(root, record.ID, "manifest.json")} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("private video artifact %q: %v mode=%v", path, err, info)
		}
	}
	list := httptest.NewRecorder()
	server.handleAPITrendRadarVideos(list, httptest.NewRequest(http.MethodGet, "/api/trend-radar/videos", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), record.ID) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	duplicate := httptest.NewRecorder()
	server.handleAPITrendRadarVideos(duplicate, contentLibraryVideoUploadRequest(t, fixture, "same.mp4"))
	if duplicate.Code != http.StatusOK || !strings.Contains(duplicate.Body.String(), record.ID) {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
}
