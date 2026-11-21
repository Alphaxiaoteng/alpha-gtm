package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/studio"
)

func TestStudioAPIsKeepMediaScriptAndPlanLocal(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.studioAgent = studio.NewDoubaoAgentWithCompletion(func(context.Context, string, string) (string, error) {
		return `{"start_ms":0,"end_ms":59000,"title":"开场","reason":"匹配脚本开场"}`, nil
	})

	mediaPath := filepath.Join(t.TempDir(), "source.mp4")
	if err := os.WriteFile(mediaPath, []byte("local-video-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	mediaRecorder := httptest.NewRecorder()
	server.ServeHTTP(mediaRecorder, httptest.NewRequest(http.MethodPost, "/api/media/library", bytes.NewBufferString(`{"path":"`+mediaPath+`"}`)))
	if mediaRecorder.Code != http.StatusCreated {
		t.Fatalf("media status=%d body=%s", mediaRecorder.Code, mediaRecorder.Body.String())
	}
	var mediaItem struct {
		ID   int64  `json:"id"`
		Mime string `json:"mime"`
	}
	if err := json.NewDecoder(mediaRecorder.Body).Decode(&mediaItem); err != nil || mediaItem.ID == 0 || mediaItem.Mime != "video/mp4" {
		t.Fatalf("media=%+v err=%v", mediaItem, err)
	}

	scriptRecorder := httptest.NewRecorder()
	server.ServeHTTP(scriptRecorder, httptest.NewRequest(http.MethodPost, "/api/scripts", bytes.NewBufferString(`{"title":"开场脚本","body":"第一句开场"}`)))
	if scriptRecorder.Code != http.StatusCreated {
		t.Fatalf("script status=%d body=%s", scriptRecorder.Code, scriptRecorder.Body.String())
	}
	var script struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(scriptRecorder.Body).Decode(&script); err != nil || script.ID == 0 {
		t.Fatalf("script=%+v err=%v", script, err)
	}

	planBody, _ := json.Marshal(map[string]any{"sourceMediaId": mediaItem.ID, "scriptId": script.ID, "targetDurationMs": 60_000})
	planRecorder := httptest.NewRecorder()
	server.ServeHTTP(planRecorder, httptest.NewRequest(http.MethodPost, "/api/clip-jobs/plan", bytes.NewReader(planBody)))
	if planRecorder.Code != http.StatusCreated {
		t.Fatalf("plan status=%d body=%s", planRecorder.Code, planRecorder.Body.String())
	}
	var job struct {
		SourceMediaID int64  `json:"source_media_id"`
		ScriptID      int64  `json:"script_id"`
		Status        string `json:"status"`
		PlanJSON      string `json:"plan_json"`
	}
	if err := json.NewDecoder(planRecorder.Body).Decode(&job); err != nil || job.SourceMediaID != mediaItem.ID || job.ScriptID != script.ID || job.Status != "planned" || job.PlanJSON == "" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestStudioMediaDeleteRemovesOnlyUnreferencedIndex(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	sourcePath := filepath.Join(t.TempDir(), "unreferenced.mp4")
	if err := os.WriteFile(sourcePath, []byte("local-video-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"path": sourcePath})
	importRecorder := httptest.NewRecorder()
	server.ServeHTTP(importRecorder, httptest.NewRequest(http.MethodPost, "/api/media/library", bytes.NewReader(body)))
	if importRecorder.Code != http.StatusCreated {
		t.Fatalf("import status=%d body=%s", importRecorder.Code, importRecorder.Body.String())
	}
	var item struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(importRecorder.Body).Decode(&item); err != nil || item.ID == 0 {
		t.Fatalf("decode item=%+v err=%v", item, err)
	}

	deleteRecorder := httptest.NewRecorder()
	server.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/media/library/"+strconv.FormatInt(item.ID, 10), nil))
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("deleting index must not delete source: %v", err)
	}
	if _, err := server.db.GetMediaLibraryItem(item.ID); err == nil {
		t.Fatal("deleted media index is still present")
	}
}

func TestStudioCategoriesPersistAcrossServerRestart(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "categorized.mp4")
	if err := os.WriteFile(sourcePath, []byte("local-video-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	mediaBody, _ := json.Marshal(map[string]string{"path": sourcePath, "purpose": "graph_text"})
	mediaRecorder := httptest.NewRecorder()
	server.ServeHTTP(mediaRecorder, httptest.NewRequest(http.MethodPost, "/api/media/library", bytes.NewReader(mediaBody)))
	if mediaRecorder.Code != http.StatusCreated {
		t.Fatalf("media status=%d body=%s", mediaRecorder.Code, mediaRecorder.Body.String())
	}
	var mediaItem struct {
		ID      int64  `json:"id"`
		Purpose string `json:"purpose"`
	}
	if err := json.NewDecoder(mediaRecorder.Body).Decode(&mediaItem); err != nil || mediaItem.Purpose != "graph_text" {
		t.Fatalf("media=%+v err=%v", mediaItem, err)
	}
	scriptRecorder := httptest.NewRecorder()
	server.ServeHTTP(scriptRecorder, httptest.NewRequest(http.MethodPost, "/api/scripts", bytes.NewBufferString(`{"title":"文章脚本","body":"本地正文","content_type":"article"}`)))
	if scriptRecorder.Code != http.StatusCreated {
		t.Fatalf("script status=%d body=%s", scriptRecorder.Code, scriptRecorder.Body.String())
	}
	server.Close()

	restarted, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	storedMedia, err := restarted.db.GetMediaLibraryItem(mediaItem.ID)
	if err != nil || storedMedia.Purpose != "graph_text" {
		t.Fatalf("stored media=%+v err=%v", storedMedia, err)
	}
	scripts, err := restarted.db.ListStudioScripts(10)
	if err != nil || len(scripts) != 1 || scripts[0].ContentType != "article" {
		t.Fatalf("scripts=%+v err=%v", scripts, err)
	}

	updateRecorder := httptest.NewRecorder()
	restarted.ServeHTTP(updateRecorder, httptest.NewRequest(http.MethodPut, "/api/media/library/"+strconv.FormatInt(mediaItem.ID, 10), bytes.NewBufferString(`{"purpose":"article"}`)))
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	updated, _ := restarted.db.GetMediaLibraryItem(mediaItem.ID)
	if updated.Purpose != "article" {
		t.Fatalf("updated media=%+v", updated)
	}
}

func TestStudioClipExecutionCreatesAndIndexesLocalOutput(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required for local clip smoke test")
	}
	formats, err := exec.Command(ffmpegPath, "-hide_banner", "-formats").Output()
	if err != nil || !strings.Contains(string(formats), "mp4") {
		t.Skip("a full ffmpeg build with MP4 muxing is required for local clip smoke test")
	}
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.studioAgent = studio.NewDoubaoAgentWithCompletion(func(context.Context, string, string) (string, error) {
		return `{"start_ms":0,"end_ms":1000,"title":"本地片段","reason":"覆盖脚本开场"}`, nil
	})

	framePath := filepath.Join(t.TempDir(), "frame.ppm")
	if err := os.WriteFile(framePath, append([]byte("P6\n2 2\n255\n"), make([]byte, 12)...), 0o600); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "source.mp4")
	createSource := exec.Command(ffmpegPath, "-hide_banner", "-loglevel", "error", "-loop", "1", "-framerate", "25", "-i", framePath, "-t", "2", "-c:v", "mpeg4", "-y", sourcePath)
	if output, err := createSource.CombinedOutput(); err != nil {
		t.Skipf("local ffmpeg cannot build source fixture: %v: %s", err, output)
	}

	mediaBody, _ := json.Marshal(map[string]string{"path": sourcePath})
	mediaRecorder := httptest.NewRecorder()
	server.ServeHTTP(mediaRecorder, httptest.NewRequest(http.MethodPost, "/api/media/library", bytes.NewReader(mediaBody)))
	if mediaRecorder.Code != http.StatusCreated {
		t.Fatalf("media status=%d body=%s", mediaRecorder.Code, mediaRecorder.Body.String())
	}
	var mediaItem struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(mediaRecorder.Body).Decode(&mediaItem); err != nil || mediaItem.ID == 0 {
		t.Fatalf("decode media=%+v err=%v", mediaItem, err)
	}

	scriptRecorder := httptest.NewRecorder()
	server.ServeHTTP(scriptRecorder, httptest.NewRequest(http.MethodPost, "/api/scripts", bytes.NewBufferString(`{"title":"本地脚本","body":"第一秒作为开场"}`)))
	if scriptRecorder.Code != http.StatusCreated {
		t.Fatalf("script status=%d body=%s", scriptRecorder.Code, scriptRecorder.Body.String())
	}
	var script struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(scriptRecorder.Body).Decode(&script); err != nil || script.ID == 0 {
		t.Fatalf("decode script=%+v err=%v", script, err)
	}

	planBody, _ := json.Marshal(map[string]any{"sourceMediaId": mediaItem.ID, "scriptId": script.ID, "targetDurationMs": 30_000})
	planRecorder := httptest.NewRecorder()
	server.ServeHTTP(planRecorder, httptest.NewRequest(http.MethodPost, "/api/clip-jobs/plan", bytes.NewReader(planBody)))
	if planRecorder.Code != http.StatusCreated {
		t.Fatalf("plan status=%d body=%s", planRecorder.Code, planRecorder.Body.String())
	}
	var plan struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(planRecorder.Body).Decode(&plan); err != nil || plan.ID == 0 {
		t.Fatalf("decode plan=%+v err=%v", plan, err)
	}

	executeRecorder := httptest.NewRecorder()
	server.ServeHTTP(executeRecorder, httptest.NewRequest(http.MethodPost, "/api/clip-jobs/"+strconv.FormatInt(plan.ID, 10)+"/execute", nil))
	if executeRecorder.Code != http.StatusOK {
		t.Fatalf("execute status=%d body=%s", executeRecorder.Code, executeRecorder.Body.String())
	}
	var completed struct {
		Status     string `json:"status"`
		OutputPath string `json:"output_path"`
	}
	if err := json.NewDecoder(executeRecorder.Body).Decode(&completed); err != nil || completed.Status != "completed" {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	if completed.OutputPath != filepath.Join(dataDir, "edits", "clip-1.mp4") {
		t.Fatalf("output path=%q", completed.OutputPath)
	}
	info, err := os.Stat(completed.OutputPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("output stat=%v size=%d", err, info.Size())
	}

	libraryRecorder := httptest.NewRecorder()
	server.ServeHTTP(libraryRecorder, httptest.NewRequest(http.MethodGet, "/api/media/library", nil))
	if libraryRecorder.Code != http.StatusOK {
		t.Fatalf("library status=%d body=%s", libraryRecorder.Code, libraryRecorder.Body.String())
	}
	var library []struct {
		FilePath string `json:"file_path"`
	}
	if err := json.NewDecoder(libraryRecorder.Body).Decode(&library); err != nil {
		t.Fatal(err)
	}
	indexedOutput := false
	for _, item := range library {
		indexedOutput = indexedOutput || item.FilePath == completed.OutputPath
	}
	if !indexedOutput {
		t.Fatalf("clip output was not indexed: %+v", library)
	}
}

func TestStudioClipExecutionFollowsLocalFFmpegContract(t *testing.T) {
	installStudioFakeFFMPEG(t)
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.studioAgent = studio.NewDoubaoAgentWithCompletion(func(context.Context, string, string) (string, error) {
		return `{"start_ms":0,"end_ms":1000,"title":"本地片段","reason":"覆盖脚本开场"}`, nil
	})

	sourcePath := filepath.Join(t.TempDir(), "source.mp4")
	if err := os.WriteFile(sourcePath, []byte("local-video-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	mediaBody, _ := json.Marshal(map[string]string{"path": sourcePath})
	mediaRecorder := httptest.NewRecorder()
	server.ServeHTTP(mediaRecorder, httptest.NewRequest(http.MethodPost, "/api/media/library", bytes.NewReader(mediaBody)))
	if mediaRecorder.Code != http.StatusCreated {
		t.Fatalf("media status=%d body=%s", mediaRecorder.Code, mediaRecorder.Body.String())
	}
	var mediaItem struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(mediaRecorder.Body).Decode(&mediaItem); err != nil || mediaItem.ID == 0 {
		t.Fatalf("decode media=%+v err=%v", mediaItem, err)
	}

	scriptRecorder := httptest.NewRecorder()
	server.ServeHTTP(scriptRecorder, httptest.NewRequest(http.MethodPost, "/api/scripts", bytes.NewBufferString(`{"title":"本地脚本","body":"第一秒作为开场"}`)))
	if scriptRecorder.Code != http.StatusCreated {
		t.Fatalf("script status=%d body=%s", scriptRecorder.Code, scriptRecorder.Body.String())
	}
	var script struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(scriptRecorder.Body).Decode(&script); err != nil || script.ID == 0 {
		t.Fatalf("decode script=%+v err=%v", script, err)
	}

	planBody, _ := json.Marshal(map[string]any{"sourceMediaId": mediaItem.ID, "scriptId": script.ID, "targetDurationMs": 30_000})
	planRecorder := httptest.NewRecorder()
	server.ServeHTTP(planRecorder, httptest.NewRequest(http.MethodPost, "/api/clip-jobs/plan", bytes.NewReader(planBody)))
	if planRecorder.Code != http.StatusCreated {
		t.Fatalf("plan status=%d body=%s", planRecorder.Code, planRecorder.Body.String())
	}
	var plan struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(planRecorder.Body).Decode(&plan); err != nil || plan.ID == 0 {
		t.Fatalf("decode plan=%+v err=%v", plan, err)
	}

	executeRecorder := httptest.NewRecorder()
	server.ServeHTTP(executeRecorder, httptest.NewRequest(http.MethodPost, "/api/clip-jobs/"+strconv.FormatInt(plan.ID, 10)+"/execute", nil))
	if executeRecorder.Code != http.StatusOK {
		t.Fatalf("execute status=%d body=%s", executeRecorder.Code, executeRecorder.Body.String())
	}
	var completed struct {
		Status     string `json:"status"`
		OutputPath string `json:"output_path"`
	}
	if err := json.NewDecoder(executeRecorder.Body).Decode(&completed); err != nil || completed.Status != "completed" {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	if completed.OutputPath != filepath.Join(dataDir, "edits", "clip-1.mp4") {
		t.Fatalf("output path=%q", completed.OutputPath)
	}
	if output, err := os.ReadFile(completed.OutputPath); err != nil || string(output) != "local-video-fixture" {
		t.Fatalf("output=%q err=%v", output, err)
	}

	libraryRecorder := httptest.NewRecorder()
	server.ServeHTTP(libraryRecorder, httptest.NewRequest(http.MethodGet, "/api/media/library", nil))
	if libraryRecorder.Code != http.StatusOK {
		t.Fatalf("library status=%d body=%s", libraryRecorder.Code, libraryRecorder.Body.String())
	}
	var library []struct {
		FilePath string `json:"file_path"`
	}
	if err := json.NewDecoder(libraryRecorder.Body).Decode(&library); err != nil {
		t.Fatal(err)
	}
	for _, item := range library {
		if item.FilePath == completed.OutputPath {
			return
		}
	}
	t.Fatalf("clip output was not indexed: %+v", library)
}

func installStudioFakeFFMPEG(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ffmpeg")
	script := `#!/bin/sh
case " $* " in
  *" -formats "*) printf '%s\n' 'Formats:' ' E mp4 MP4'; exit 0 ;;
esac
source=''
previous=''
last=''
for argument in "$@"; do
  if [ "$previous" = '-i' ]; then source="$argument"; fi
  previous="$argument"
  last="$argument"
done
exec /bin/cp "$source" "$last"
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}
