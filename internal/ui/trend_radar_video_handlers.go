package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/media"
)

type radarVideoRecord struct {
	ID                 string `json:"id"`
	OriginalName       string `json:"original_name"`
	SourcePath         string `json:"source_path"`
	CoverPath          string `json:"cover_path,omitempty"`
	CoverURL           string `json:"cover_url,omitempty"`
	AudioPath          string `json:"audio_path,omitempty"`
	TranscriptPath     string `json:"transcript_path,omitempty"`
	TranscriptionState string `json:"transcription_state"`
	TranscriptionNote  string `json:"transcription_note"`
	SHA256             string `json:"sha256"`
	SizeBytes          int64  `json:"size_bytes"`
	DurationMS         int    `json:"duration_ms"`
	CreatedAt          string `json:"created_at"`
}

func (s *Server) handleAPITrendRadarVideos(w http.ResponseWriter, r *http.Request) {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, "trend radar data directory unavailable", http.StatusInternalServerError)
		return
	}
	root := filepath.Join(dataDir, "trend-radar", "video-deconstruct")
	if r.Method == http.MethodGet {
		records, err := listRadarVideos(root)
		if err != nil {
			http.Error(w, "video records unavailable", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"videos": records, "count": len(records)})
		return
	}
	if !sameOriginLoopbackRequest(r) {
		http.Error(w, "video upload denied", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxContentLibraryVideoBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, "invalid video upload", http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
		http.Error(w, "exactly one video file is required", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "video file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !strings.HasPrefix(media.GuessMime("upload"+ext), "video/") {
		http.Error(w, "unsupported video format", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		http.Error(w, "create video workspace failed", http.StatusInternalServerError)
		return
	}
	temp, err := os.CreateTemp(root, ".upload-*"+ext)
	if err != nil {
		http.Error(w, "create video file failed", http.StatusInternalServerError)
		return
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		http.Error(w, "store video failed", http.StatusInternalServerError)
		return
	}
	written, copyErr := io.Copy(temp, io.LimitReader(file, maxContentLibraryVideoBytes+1))
	if syncErr := temp.Sync(); copyErr != nil || syncErr != nil || written <= 0 || written > maxContentLibraryVideoBytes {
		_ = temp.Close()
		http.Error(w, "invalid video file", http.StatusBadRequest)
		return
	}
	if err := temp.Close(); err != nil {
		http.Error(w, "store video failed", http.StatusInternalServerError)
		return
	}
	meta, err := media.ProbeDecodableVideo(tempPath)
	if err != nil || meta.SHA256 == "" {
		http.Error(w, "video verification failed", http.StatusUnprocessableEntity)
		return
	}
	id := meta.SHA256[:20]
	itemDir := filepath.Join(root, id)
	manifestPath := filepath.Join(itemDir, "manifest.json")
	if existing, err := readRadarVideoManifest(manifestPath); err == nil {
		writeJSON(w, existing)
		return
	}
	if err := os.Mkdir(itemDir, 0o700); err != nil {
		http.Error(w, "create video workspace failed", http.StatusInternalServerError)
		return
	}
	sourcePath := filepath.Join(itemDir, "source"+ext)
	if err := os.Link(tempPath, sourcePath); err != nil {
		_ = os.Remove(itemDir)
		http.Error(w, "store video failed", http.StatusInternalServerError)
		return
	}
	record := radarVideoRecord{
		ID: id, OriginalName: filepath.Base(header.Filename), SourcePath: sourcePath,
		SHA256: meta.SHA256, SizeBytes: meta.SizeBytes, DurationMS: meta.DurationMS,
		CreatedAt:          time.Now().UTC().Format(time.RFC3339Nano),
		TranscriptionState: "waiting_local_whisper",
		TranscriptionNote:  "平台字幕不可用；音频已在本地准备，等待安装本地 Whisper。云端转写不会自动调用。",
	}
	coverPath := filepath.Join(itemDir, "cover.jpg")
	if _, coverErr := media.ExtractFirstFrame(sourcePath, coverPath); coverErr == nil {
		record.CoverPath = coverPath
		record.CoverURL = "/api/trend-radar/videos/" + id + "/cover"
	}
	audioPath := filepath.Join(itemDir, "audio.wav")
	if _, audioErr := media.ExtractWhisperAudio(sourcePath, audioPath); audioErr == nil {
		record.AudioPath = audioPath
	}
	if _, err := exec.LookPath("whisper-cli"); err == nil {
		record.TranscriptionNote = "本地 whisper-cli 已检测到；自动转写执行仍需后续显式操作。"
	} else if _, err := exec.LookPath("whisper"); err == nil {
		record.TranscriptionNote = "本地 whisper 已检测到；自动转写执行仍需后续显式操作。"
	}
	if err := writeRadarVideoManifest(manifestPath, record); err != nil {
		http.Error(w, "save video manifest failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, record)
}

func (s *Server) handleAPITrendRadarVideoCover(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if len(id) != 20 || strings.Trim(id, "0123456789abcdef") != "" {
		http.NotFound(w, r)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(dataDir, "trend-radar", "video-deconstruct", id, "cover.jpg"))
}

func listRadarVideos(root string) ([]radarVideoRecord, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []radarVideoRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := make([]radarVideoRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		record, err := readRadarVideoManifest(filepath.Join(root, entry.Name(), "manifest.json"))
		if err == nil {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt > records[j].CreatedAt })
	return records, nil
}

func readRadarVideoManifest(path string) (radarVideoRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return radarVideoRecord{}, err
	}
	var record radarVideoRecord
	err = json.Unmarshal(raw, &record)
	return record, err
}

func writeRadarVideoManifest(path string, record radarVideoRecord) error {
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".manifest-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Link(tempPath, path)
}
