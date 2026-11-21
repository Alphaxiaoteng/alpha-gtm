package ui

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"alpha-nexus-egolite/internal/media"
)

const maxContentLibraryVideoBytes int64 = 4 << 30
const maxContentLibraryImageBytes int64 = 100 << 20
const maxContentLibraryImagePixels int64 = 40_000_000

type contentLibraryConfigResponse struct {
	RootDir  string `json:"root_dir"`
	VideoDir string `json:"video_dir"`
	ImageDir string `json:"image_dir"`
}

func (s *Server) contentLibraryConfig() (contentLibraryConfigResponse, error) {
	dir, err := s.cfg.ResolvedContentLibraryDir()
	if err != nil {
		return contentLibraryConfigResponse{}, err
	}
	return contentLibraryConfigResponse{RootDir: dir, VideoDir: filepath.Join(dir, "video"), ImageDir: filepath.Join(dir, "image")}, nil
}

func (s *Server) handleAPIContentLibraryConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		response, err := s.contentLibraryConfig()
		if err != nil {
			http.Error(w, "resolve content directory failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, response)
	case http.MethodPut:
		if !sameOriginLoopbackRequest(r) {
			http.Error(w, "content directory update denied", http.StatusForbidden)
			return
		}
		var body struct {
			RootDir string `json:"root_dir"`
		}
		if err := decodeContentLibraryJSON(w, r, &body); err != nil {
			http.Error(w, "invalid content directory body", http.StatusBadRequest)
			return
		}
		dir, err := validateContentLibraryDir(body.RootDir)
		if err != nil {
			http.Error(w, "invalid content directory", http.StatusBadRequest)
			return
		}
		if err := ensureContentLibraryDirs(dir); err != nil {
			http.Error(w, "create content directory failed", http.StatusInternalServerError)
			return
		}
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil || contentLibraryProtectedPath(resolved) {
			http.Error(w, "invalid content directory", http.StatusBadRequest)
			return
		}
		if err := s.persistContentLibraryDir(resolved); err != nil {
			http.Error(w, "save content directory failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, contentLibraryConfigResponse{RootDir: resolved, VideoDir: filepath.Join(resolved, "video"), ImageDir: filepath.Join(resolved, "image")})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func ensureContentLibraryDirs(root string) error {
	for _, name := range []string{"video", "image"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			return err
		}
	}
	return nil
}

// handleAPIContentLibraryVideoUpload stores a user-selected video in the
// configured local content folder. It never moves the browser's source file.
func (s *Server) handleAPIContentLibraryVideoUpload(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
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
	response, err := s.contentLibraryConfig()
	if err != nil {
		http.Error(w, "resolve content directory failed", http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(response.VideoDir, 0700); err != nil {
		http.Error(w, "create content directory failed", http.StatusInternalServerError)
		return
	}
	videoDir, err := filepath.EvalSymlinks(response.VideoDir)
	if err != nil || contentLibraryProtectedPath(videoDir) {
		http.Error(w, "invalid content directory", http.StatusBadRequest)
		return
	}
	temporary, err := os.CreateTemp(videoDir, ".upload-*"+ext)
	if err != nil {
		http.Error(w, "create video file failed", http.StatusInternalServerError)
		return
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		http.Error(w, "store video failed", http.StatusInternalServerError)
		return
	}
	written, copyErr := io.Copy(temporary, io.LimitReader(file, maxContentLibraryVideoBytes+1))
	if syncErr := temporary.Sync(); copyErr != nil || syncErr != nil || written <= 0 || written > maxContentLibraryVideoBytes {
		temporary.Close()
		http.Error(w, "invalid video file", http.StatusBadRequest)
		return
	}
	if err := temporary.Close(); err != nil {
		http.Error(w, "store video failed", http.StatusInternalServerError)
		return
	}
	meta, err := media.ProbeDecodableVideo(temporaryPath)
	if err != nil || !strings.HasPrefix(meta.MimeGuess, "video/") || meta.SHA256 == "" {
		http.Error(w, "video verification failed", http.StatusUnprocessableEntity)
		return
	}
	destination := filepath.Join(videoDir, meta.SHA256+ext)
	createdDestination := false
	if err := os.Link(temporaryPath, destination); err != nil {
		if !os.IsExist(err) {
			http.Error(w, "store video failed", http.StatusInternalServerError)
			return
		}
		info, statErr := os.Lstat(destination)
		if statErr != nil || !info.Mode().IsRegular() {
			http.Error(w, "existing video verification failed", http.StatusConflict)
			return
		}
		existingHash, _, hashErr := media.HashFile(destination)
		if hashErr != nil || !strings.EqualFold(existingHash, meta.SHA256) {
			http.Error(w, "existing video verification failed", http.StatusConflict)
			return
		}
	} else {
		createdDestination = true
	}
	item, err := s.db.UpsertMediaLibrary(destination, meta.SHA256, meta.SizeBytes, meta.MimeGuess, meta.Width, meta.Height, meta.DurationMS, meta.ProbeJSON)
	if err == nil {
		err = s.db.SetMediaLibraryPurpose(item.ID, "video")
	}
	if err != nil {
		if createdDestination {
			_ = os.Remove(destination)
		}
		http.Error(w, "index video failed", http.StatusInternalServerError)
		return
	}
	item.Purpose = "video"
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, item)
}

// handleAPIContentLibraryImageUpload stores a user-selected image in the
// configured local content folder. It decodes the image before indexing it;
// file extensions alone are never treated as image evidence.
func (s *Server) handleAPIContentLibraryImageUpload(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if !sameOriginLoopbackRequest(r) {
		http.Error(w, "image upload denied", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxContentLibraryImageBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, "invalid image upload", http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
		http.Error(w, "exactly one image file is required", http.StatusBadRequest)
		return
	}
	purpose := strings.TrimSpace(r.FormValue("purpose"))
	if purpose == "" {
		purpose = "graph_text"
	}
	if purpose != "graph_text" && purpose != "article" {
		http.Error(w, "invalid image purpose", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "image file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	response, err := s.contentLibraryConfig()
	if err != nil {
		http.Error(w, "resolve content directory failed", http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(response.ImageDir, 0700); err != nil {
		http.Error(w, "create content directory failed", http.StatusInternalServerError)
		return
	}
	imageDir, err := filepath.EvalSymlinks(response.ImageDir)
	if err != nil || contentLibraryProtectedPath(imageDir) {
		http.Error(w, "invalid content directory", http.StatusBadRequest)
		return
	}
	temporary, err := os.CreateTemp(imageDir, ".upload-*")
	if err != nil {
		http.Error(w, "create image file failed", http.StatusInternalServerError)
		return
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		http.Error(w, "store image failed", http.StatusInternalServerError)
		return
	}
	written, copyErr := io.Copy(temporary, io.LimitReader(file, maxContentLibraryImageBytes+1))
	if syncErr := temporary.Sync(); copyErr != nil || syncErr != nil || written <= 0 || written > maxContentLibraryImageBytes {
		temporary.Close()
		http.Error(w, "invalid image file", http.StatusBadRequest)
		return
	}
	if err := temporary.Close(); err != nil {
		http.Error(w, "store image failed", http.StatusInternalServerError)
		return
	}
	meta, ext, err := probeContentLibraryImage(temporaryPath)
	if err != nil || meta.SHA256 == "" {
		http.Error(w, "image verification failed", http.StatusUnprocessableEntity)
		return
	}
	destination := filepath.Join(imageDir, meta.SHA256+ext)
	createdDestination := false
	if err := os.Link(temporaryPath, destination); err != nil {
		if !os.IsExist(err) {
			http.Error(w, "store image failed", http.StatusInternalServerError)
			return
		}
		info, statErr := os.Lstat(destination)
		if statErr != nil || !info.Mode().IsRegular() {
			http.Error(w, "existing image verification failed", http.StatusConflict)
			return
		}
		existing, _, verifyErr := probeContentLibraryImage(destination)
		if verifyErr != nil || existing.SHA256 != meta.SHA256 {
			http.Error(w, "existing image verification failed", http.StatusConflict)
			return
		}
	} else {
		createdDestination = true
	}
	item, err := s.db.UpsertMediaLibrary(destination, meta.SHA256, meta.SizeBytes, meta.MimeGuess, meta.Width, meta.Height, 0, "")
	if err == nil {
		err = s.db.SetMediaLibraryPurpose(item.ID, "shared")
	}
	if err != nil {
		if createdDestination {
			_ = os.Remove(destination)
		}
		http.Error(w, "index image failed", http.StatusInternalServerError)
		return
	}
	item.Purpose = "shared"
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, item)
}

func probeContentLibraryImage(path string) (*media.FileMeta, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	config, format, err := image.DecodeConfig(file)
	_ = file.Close()
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxContentLibraryImagePixels {
		return nil, "", fmt.Errorf("invalid image")
	}
	file, err = os.Open(path)
	if err != nil {
		return nil, "", err
	}
	decoded, decodedFormat, err := image.Decode(file)
	_ = file.Close()
	if err != nil || decodedFormat != format || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return nil, "", fmt.Errorf("image decode failed")
	}
	ext, mime := "", ""
	switch format {
	case "jpeg":
		ext, mime = ".jpg", "image/jpeg"
	case "png":
		ext, mime = ".png", "image/png"
	case "gif":
		ext, mime = ".gif", "image/gif"
	default:
		return nil, "", fmt.Errorf("unsupported image format")
	}
	hash, size, err := media.HashFile(path)
	if err != nil {
		return nil, "", err
	}
	return &media.FileMeta{Path: path, SHA256: hash, SizeBytes: size, MimeGuess: mime, Width: config.Width, Height: config.Height}, ext, nil
}

func decodeContentLibraryJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("multiple request values")
	}
	return nil
}

func validateContentLibraryDir(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("content directory is required")
	}
	return validateAIImageAssetDir(value)
}

func contentLibraryProtectedPath(path string) bool {
	return aiImageProtectedPath(path)
}

func (s *Server) persistContentLibraryDir(dir string) error {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.updatePersistedConfigLocked(dataDir, func(values map[string]any) {
		values["content_library_dir"] = dir
	}); err != nil {
		return err
	}
	s.cfg.ContentLibraryDir = dir
	return nil
}
