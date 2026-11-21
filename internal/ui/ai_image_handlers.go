package ui

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/imagegen"
	"alpha-nexus-egolite/internal/media"
	"alpha-nexus-egolite/internal/trace"
)

const (
	maxAIImageRequestBytes = 1 << 20
	maxAIImageAssetBytes   = 100 << 20
	maxAIVideoAssetBytes   = int64(2 << 30)
	maxAIImagePixels       = 40_000_000
	defaultAIProvider      = "agnes"
	defaultAIImageModel    = "agnes-image-2.1-flash"
	defaultAIVideoModel    = "agnes-video-v2.0"

	// zexapi (章鱼哥 API) speaks the Gemini native image endpoint instead of the
	// Agnes contract, so it keeps its own provider name and model catalogue.
	aiImageProviderZexAPI         = "zexapi"
	maxAIImageGeneratePromptChars = 2000
	maxAIImageGenerateBytes       = 32 << 20
	aiImageGenerateTimeout        = imagegen.DefaultTimeout + 30*time.Second
)

type aiImageConfigResponse struct {
	Provider             string `json:"provider"`
	Model                string `json:"model"` // compatibility alias for image_model
	ImageModel           string `json:"image_model"`
	VideoModel           string `json:"video_model"`
	AssetDir             string `json:"asset_dir"`
	ProviderStatus       string `json:"provider_status"`
	CredentialConfigured bool   `json:"credential_configured"`
}

type aiImageAsset struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	Mime       string `json:"mime"`
	MimeType   string `json:"mime_type"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMS int    `json:"duration_ms"`
	SizeBytes  int64  `json:"size_bytes"`
	CreatedAt  string `json:"created_at"`
}

type aiImagePromptPreset struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type aiImageCanvasNode struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Text      string  `json:"text,omitempty"`
	AssetName string  `json:"asset_name,omitempty"`
	RefLabel  string  `json:"ref_label,omitempty"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Width     float64 `json:"width"`
	Height    float64 `json:"height"`
}

type aiImageViewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}

type aiImageWorkspace struct {
	Prompts  []aiImagePromptPreset `json:"prompts"`
	Nodes    []aiImageCanvasNode   `json:"nodes"`
	Viewport aiImageViewport       `json:"viewport"`
}

type aiImageProjectResponse struct {
	ID        int64            `json:"id"`
	Name      string           `json:"name"`
	Workspace aiImageWorkspace `json:"workspace"`
	CreatedAt string           `json:"created_at"`
	UpdatedAt string           `json:"updated_at"`
}

type aiImageProjectSummary struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type aiImagePlanResponse struct {
	Status          string   `json:"status"`
	Mode            *string  `json:"mode"`
	Category        *string  `json:"category"`
	Composition     *string  `json:"composition"`
	Variant         *string  `json:"variant"`
	TemplateID      *string  `json:"template_id"`
	TemplateVersion *string  `json:"template_version"`
	ReferenceCount  int      `json:"reference_count"`
	Prompt          *string  `json:"prompt"`
	NegativePrompt  *string  `json:"negative_prompt"`
	Provider        *string  `json:"provider"`
	ArtifactPaths   []string `json:"artifact_paths"`
	ErrorCode       *string  `json:"error_code"`
	ManualAction    *string  `json:"manual_action"`
}

func decodeAIImageJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxAIImageRequestBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid request body")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("invalid request body")
	}
	return nil
}

func (s *Server) currentAIImageConfig() (aiImageConfigResponse, error) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	assetDir, err := cfg.ResolvedAIImageAssetDir()
	if err != nil {
		return aiImageConfigResponse{}, err
	}
	provider := strings.TrimSpace(cfg.AIImageProvider)
	if provider == "" {
		provider = defaultAIProvider
	}
	imageModel := strings.TrimSpace(cfg.AIImageModel)
	if imageModel == "" {
		if isZexAPIImageProvider(provider) {
			imageModel = imagegen.DefaultModel
		} else {
			imageModel = defaultAIImageModel
		}
	}
	videoModel := strings.TrimSpace(cfg.AIVideoModel)
	if videoModel == "" {
		videoModel = defaultAIVideoModel
	}
	credentialConfigured := false
	status := "credential_missing"
	if isZexAPIImageProvider(provider) {
		// zexapi keys live in the existing image_api_key config field; the Agnes
		// keychain is a different provider and must not be treated as a match.
		credentialConfigured = strings.TrimSpace(cfg.ImageAPIKey) != ""
	} else if s.agnesAPIKeyConfigured != nil {
		credentialConfigured = s.secureMediaCredentialConfigured()
	}
	if credentialConfigured {
		status = "configured_unverified"
	}
	return aiImageConfigResponse{
		Provider: provider, Model: imageModel, ImageModel: imageModel, VideoModel: videoModel,
		AssetDir: assetDir, ProviderStatus: status, CredentialConfigured: credentialConfigured,
	}, nil
}

func (s *Server) handleAPIAIImageConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		cfg, err := s.currentAIImageConfig()
		if err != nil {
			http.Error(w, "resolve AI image configuration failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, cfg)
		return
	}
	var body struct {
		Provider   string `json:"provider"`
		BaseURL    string `json:"base_url"`
		Model      string `json:"model"`
		ImageModel string `json:"image_model"`
		VideoModel string `json:"video_model"`
		AssetDir   string `json:"asset_dir"`
	}
	if err := decodeAIImageJSON(w, r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body.Provider = strings.TrimSpace(body.Provider)
	if body.Provider == "" {
		body.Provider = defaultAIProvider
	}
	body.BaseURL = strings.TrimSpace(body.BaseURL)
	if isZexAPIImageProvider(body.Provider) && body.BaseURL != "" {
		if err := validateAIImageBaseURL(body.BaseURL); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	body.ImageModel = strings.TrimSpace(body.ImageModel)
	if body.ImageModel == "" {
		body.ImageModel = strings.TrimSpace(body.Model)
	}
	if body.ImageModel == "" {
		// A provider-only update must not fall back to the Agnes catalogue when
		// the caller already switched to zexapi.
		if isZexAPIImageProvider(body.Provider) {
			body.ImageModel = imagegen.DefaultModel
		} else {
			body.ImageModel = defaultAIImageModel
		}
	}
	body.VideoModel = strings.TrimSpace(body.VideoModel)
	if body.VideoModel == "" {
		body.VideoModel = defaultAIVideoModel
	}
	if !validAIImageProviderModel(body.Provider, body.ImageModel, body.VideoModel) {
		http.Error(w, "unsupported AI media provider or model", http.StatusBadRequest)
		return
	}
	// A partial update such as {"provider":"zexapi","base_url":"..."} must not
	// be rejected for omitting asset_dir: fall back to the currently resolved
	// managed folder instead of forcing every caller to re-send it.
	assetDirInput := body.AssetDir
	if strings.TrimSpace(assetDirInput) == "" {
		s.mu.Lock()
		current, resolveErr := s.cfg.ResolvedAIImageAssetDir()
		s.mu.Unlock()
		if resolveErr != nil {
			http.Error(w, "resolve asset directory failed", http.StatusInternalServerError)
			return
		}
		assetDirInput = current
	}
	assetDir, err := validateAIImageAssetDir(assetDirInput)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(assetDir, 0700); err != nil {
		http.Error(w, "create AI image asset directory failed", http.StatusInternalServerError)
		return
	}
	assetDir, err = filepath.EvalSymlinks(assetDir)
	if err != nil {
		http.Error(w, "resolve AI image asset directory failed", http.StatusInternalServerError)
		return
	}
	if err := s.persistAIImageConfig(body.Provider, body.BaseURL, body.ImageModel, body.VideoModel, assetDir); err != nil {
		http.Error(w, "save AI image configuration failed", http.StatusInternalServerError)
		return
	}
	response, _ := s.currentAIImageConfig()
	writeJSON(w, response)
}

// validAIImageProviderModel keeps the original Agnes contract unchanged and
// adds the zexapi provider, which accepts every published image model. Video
// stays on the Agnes catalogue because zexapi is image-only here.
func validAIImageProviderModel(provider, imageModel, videoModel string) bool {
	if isZexAPIImageProvider(provider) {
		return imagegen.IsSupportedModel(imageModel) && videoModel == defaultAIVideoModel
	}
	return provider == defaultAIProvider &&
		(imageModel == "agnes-image-2.0-flash" || imageModel == defaultAIImageModel) &&
		videoModel == defaultAIVideoModel
}

func isZexAPIImageProvider(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), aiImageProviderZexAPI)
}

// validateAIImageBaseURL mirrors the imagegen base-url contract: https (or
// http for loopback) with a non-empty host, no query, no fragment.
func validateAIImageBaseURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid base_url")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "https" {
		return nil
	}
	if scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") {
		return nil
	}
	return fmt.Errorf("invalid base_url")
}

func validateAIImageAssetDir(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("asset directory is required")
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve asset directory failed")
	}
	abs = filepath.Clean(abs)
	if aiImageProtectedPath(abs) {
		return "", fmt.Errorf("asset directory is protected")
	}
	if abs == string(filepath.Separator) {
		return "", fmt.Errorf("asset directory is too broad")
	}
	if home, err := os.UserHomeDir(); err == nil && abs == filepath.Clean(home) {
		return "", fmt.Errorf("asset directory cannot be the home directory")
	}

	existing := abs
	for {
		info, statErr := os.Lstat(existing)
		if statErr == nil {
			if !info.IsDir() && existing == abs {
				return "", fmt.Errorf("asset directory is not a directory")
			}
			resolved, resolveErr := filepath.EvalSymlinks(existing)
			if resolveErr != nil {
				return "", fmt.Errorf("resolve asset directory failed")
			}
			remainder, relErr := filepath.Rel(existing, abs)
			if relErr != nil {
				return "", fmt.Errorf("resolve asset directory failed")
			}
			candidate := filepath.Clean(filepath.Join(resolved, remainder))
			if aiImageProtectedPath(candidate) {
				return "", fmt.Errorf("asset directory is protected")
			}
			return candidate, nil
		}
		if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect asset directory failed")
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("resolve asset directory failed")
		}
		existing = parent
	}
}

func aiImageProtectedPath(path string) bool {
	for _, segment := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		lower := strings.ToLower(segment)
		if lower == "1key" || lower == ".ssh" || lower == ".git" || lower == ".aws" || lower == ".gnupg" || lower == "credentials" || lower == ".credentials" || lower == "keychain" || lower == "profile" || lower == "profiles" || lower == "cookie" || lower == "cookies" || lower == "log" || lower == "logs" || lower == "cache" || lower == ".cache" || lower == "output" || lower == "outputs" || lower == "node_modules" || lower == ".venv" || lower == ".env" || strings.HasPrefix(lower, ".env.") {
			return true
		}
	}
	return false
}

func (s *Server) persistAIImageConfig(provider, baseURL, imageModel, videoModel, assetDir string) error {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.updatePersistedConfigLocked(dataDir, func(values map[string]any) {
		values["ai_image_provider"] = provider
		values["ai_image_model"] = imageModel
		values["ai_video_model"] = videoModel
		values["ai_image_asset_dir"] = assetDir
		// zexapi's base_url lives in the image_api_base_url config key that the
		// generate handler reads; the agnes media face stores nothing here.
		if isZexAPIImageProvider(provider) && strings.TrimSpace(baseURL) != "" {
			values["image_api_base_url"] = strings.TrimRight(strings.TrimSpace(baseURL), "/")
		}
	}); err != nil {
		return err
	}
	s.cfg.AIImageProvider = provider
	s.cfg.AIImageModel = imageModel
	s.cfg.AIVideoModel = videoModel
	s.cfg.AIImageAssetDir = assetDir
	if isZexAPIImageProvider(provider) && strings.TrimSpace(baseURL) != "" {
		s.cfg.ImageAPIBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
		s.cfg.ImageAPIProvider = provider
	}
	return nil
}

func (s *Server) handleAPIAIImageAssets(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		items, err := s.listAIImageAssets()
		if err != nil {
			http.Error(w, "list AI image assets failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, items)
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeAIMediaError(w, http.StatusForbidden, "AI_MEDIA_ORIGIN_DENIED")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := decodeAIImageJSON(w, r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	source, err := media.ValidateLocalPath(body.Path)
	if err != nil {
		http.Error(w, "invalid image asset", http.StatusBadRequest)
		return
	}
	if aiImageProtectedPath(source) {
		http.Error(w, "media asset is in a protected location", http.StatusBadRequest)
		return
	}
	asset, err := s.importAIImageAsset(source)
	if err != nil {
		http.Error(w, "media asset is not a supported decodable image or video", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, asset)
}

// handleAPIAIImageAssetUpload gives the localhost web UI a normal file picker.
// The upload is streamed into the managed folder and never buffered as a whole
// video in memory.
func (s *Server) handleAPIAIImageAssetUpload(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeAIMediaError(w, http.StatusForbidden, "AI_MEDIA_ORIGIN_DENIED")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAIVideoAssetBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid multipart upload", http.StatusBadRequest)
		return
	}
	assetDir, err := s.ensureAIImageAssetDir()
	if err != nil {
		http.Error(w, "prepare AI media asset directory failed", http.StatusInternalServerError)
		return
	}
	var tempPath string
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			http.Error(w, "invalid multipart upload", http.StatusBadRequest)
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		if tempPath != "" {
			_ = part.Close()
			os.Remove(tempPath)
			http.Error(w, "upload exactly one media file", http.StatusBadRequest)
			return
		}
		ext := strings.ToLower(filepath.Ext(filepath.Base(part.FileName())))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif", ".mp4", ".mov", ".webm", ".mkv":
		default:
			_ = part.Close()
			http.Error(w, "unsupported media file type", http.StatusBadRequest)
			return
		}
		temp, createErr := os.CreateTemp(assetDir, ".upload-*"+ext)
		if createErr != nil {
			_ = part.Close()
			http.Error(w, "create upload file failed", http.StatusInternalServerError)
			return
		}
		tempPath = temp.Name()
		if chmodErr := temp.Chmod(0600); chmodErr != nil {
			_ = temp.Close()
			_ = part.Close()
			os.Remove(tempPath)
			http.Error(w, "prepare upload file failed", http.StatusInternalServerError)
			return
		}
		written, copyErr := io.Copy(temp, io.LimitReader(part, maxAIVideoAssetBytes+1))
		closeErr := temp.Close()
		_ = part.Close()
		if copyErr != nil || closeErr != nil || written <= 0 || written > maxAIVideoAssetBytes {
			os.Remove(tempPath)
			http.Error(w, "media upload is empty or too large", http.StatusRequestEntityTooLarge)
			return
		}
	}
	if tempPath == "" {
		http.Error(w, "media file is required", http.StatusBadRequest)
		return
	}
	defer os.Remove(tempPath)
	asset, err := s.importAIImageAsset(tempPath)
	if err != nil {
		http.Error(w, "media asset is not a supported decodable image or video", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, asset)
}

func (s *Server) importAIImageAsset(source string) (aiImageAsset, error) {
	source, err := media.ValidateLocalPath(source)
	if err != nil {
		return aiImageAsset{}, err
	}
	info, err := os.Stat(source)
	if err != nil {
		return aiImageAsset{}, err
	}
	mimeType := media.GuessMime(source)
	kind := ""
	width, height, durationMS := 0, 0, 0
	probeJSON := ""
	ext := ""
	if strings.HasPrefix(mimeType, "image/") {
		if info.Size() > maxAIImageAssetBytes {
			return aiImageAsset{}, fmt.Errorf("image asset is too large")
		}
		var format string
		width, height, format, err = decodeAIImage(source)
		if err != nil {
			return aiImageAsset{}, err
		}
		ext = map[string]string{"jpeg": ".jpg", "png": ".png", "gif": ".gif"}[format]
		mimeType = map[string]string{"jpeg": "image/jpeg", "png": "image/png", "gif": "image/gif"}[format]
		kind = "image"
	} else if strings.HasPrefix(mimeType, "video/") {
		if info.Size() > maxAIVideoAssetBytes {
			return aiImageAsset{}, fmt.Errorf("video asset is too large")
		}
		meta, probeErr := media.ProbeDecodableVideo(source)
		if probeErr != nil {
			return aiImageAsset{}, probeErr
		}
		width, height, durationMS, probeJSON = meta.Width, meta.Height, meta.DurationMS, meta.ProbeJSON
		ext = strings.ToLower(filepath.Ext(source))
		kind = "video"
	} else {
		return aiImageAsset{}, fmt.Errorf("unsupported media type")
	}
	if ext == "" {
		return aiImageAsset{}, fmt.Errorf("unsupported media format")
	}
	hash, size, err := media.HashFile(source)
	if err != nil {
		return aiImageAsset{}, err
	}
	assetDir, err := s.ensureAIImageAssetDir()
	if err != nil {
		return aiImageAsset{}, err
	}
	name := hash + ext
	destination := filepath.Join(assetDir, name)
	destinationValid := false
	if _, statErr := os.Stat(destination); statErr == nil {
		existingHash, _, hashErr := media.HashFile(destination)
		if kind == "image" {
			_, _, _, err = decodeAIImage(destination)
		} else {
			_, err = media.ProbeDecodableVideo(destination)
		}
		destinationValid = hashErr == nil && err == nil && existingHash == hash
	}
	if !destinationValid {
		if err := copyAIImageAssetAtomic(source, destination); err != nil {
			return aiImageAsset{}, err
		}
	}
	item, err := s.db.UpsertMediaLibrary(destination, hash, size, mimeType, width, height, durationMS, probeJSON)
	if err != nil {
		return aiImageAsset{}, err
	}
	_, _ = s.db.CreateAuditEvent(string(trace.New()), "info", "ai-image", "media asset imported", "media_library", item.ID, "")
	return aiImageAssetFromItem(item.ID, name, item.SHA256, item.Mime, item.Width, item.Height, item.DurationMS, item.SizeBytes, item.CreatedAt), nil
}

func decodeAIImage(path string) (int, int, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, "", err
	}
	config, format, err := image.DecodeConfig(file)
	_ = file.Close()
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxAIImagePixels {
		return 0, 0, "", fmt.Errorf("invalid image dimensions")
	}
	file, err = os.Open(path)
	if err != nil {
		return 0, 0, "", err
	}
	decoded, decodedFormat, err := image.Decode(file)
	_ = file.Close()
	if err != nil || decodedFormat != format {
		return 0, 0, "", fmt.Errorf("decode image failed")
	}
	bounds := decoded.Bounds()
	return bounds.Dx(), bounds.Dy(), format, nil
}

func copyAIImageAssetAtomic(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	temp, err := os.CreateTemp(filepath.Dir(destination), ".asset-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := io.Copy(temp, in); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, destination)
}

func (s *Server) ensureAIImageAssetDir() (string, error) {
	cfg, err := s.currentAIImageConfig()
	if err != nil {
		return "", err
	}
	dir, err := validateAIImageAssetDir(cfg.AssetDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(dir)
}

func (s *Server) listAIImageAssets() ([]aiImageAsset, error) {
	cfg, err := s.currentAIImageConfig()
	if err != nil {
		return nil, err
	}
	dir, err := validateAIImageAssetDir(cfg.AssetDir)
	if err != nil {
		return nil, err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if os.IsNotExist(err) {
		return []aiImageAsset{}, nil
	}
	if err != nil {
		return nil, err
	}
	items, err := s.db.ListMediaLibrary(500)
	if err != nil {
		return nil, err
	}
	out := make([]aiImageAsset, 0)
	for _, item := range items {
		name, ok := aiImageManagedName(dir, item.FilePath)
		if !ok || (!strings.HasPrefix(item.Mime, "image/") && !strings.HasPrefix(item.Mime, "video/")) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			continue
		}
		out = append(out, aiImageAssetFromItem(item.ID, name, item.SHA256, item.Mime, item.Width, item.Height, item.DurationMS, item.SizeBytes, item.CreatedAt))
	}
	return out, nil
}

func aiImageManagedName(dir, path string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(dir, resolved)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) || filepath.Base(relative) != relative {
		return "", false
	}
	return relative, true
}

func aiImageAssetFromItem(id int64, name, hash, mime string, width, height, durationMS int, size int64, created string) aiImageAsset {
	kind := "image"
	if strings.HasPrefix(mime, "video/") {
		kind = "video"
	}
	return aiImageAsset{ID: id, Name: name, Kind: kind, URL: "/api/ai-image/assets/" + url.PathEscape(name), SHA256: hash, Mime: mime, MimeType: mime, Width: width, Height: height, DurationMS: durationMS, SizeBytes: size, CreatedAt: created}
}

func (s *Server) handleAPIAIImageAssetFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name {
		http.NotFound(w, r)
		return
	}
	dir, err := s.ensureAIImageAssetDir()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(dir, name)
	managedName, ok := aiImageManagedName(dir, path)
	if !ok || managedName != name {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", media.GuessMime(path))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func defaultAIImageWorkspace() aiImageWorkspace {
	return aiImageWorkspace{
		Prompts: []aiImagePromptPreset{
			{ID: "still-life", Title: "静物棚拍", Text: "完整展示产品，四周留白，真实材质与自然光影，不改变产品细节。"},
			{ID: "worn-detail", Title: "佩戴特写", Text: "自然佩戴特写，真实皮肤纹理与接触关系，产品参考图是细节唯一真源。"},
			{ID: "social-grid", Title: "社媒九宫格", Text: "统一视觉语言的完整九宫格，兼顾佩戴与静物构图，每格主体清晰。"},
		},
		Nodes: []aiImageCanvasNode{}, Viewport: aiImageViewport{Zoom: 1},
	}
}

func (s *Server) aiImageWorkspacePath() (string, error) {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "ai-image", "workspace.json"), nil
}

func (s *Server) handleAPIAIImageWorkspace(w http.ResponseWriter, r *http.Request) {
	path, err := s.aiImageWorkspacePath()
	if err != nil {
		http.Error(w, "resolve AI image workspace failed", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet {
		workspace := defaultAIImageWorkspace()
		data, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			writeJSON(w, workspace)
			return
		}
		if readErr != nil || json.Unmarshal(data, &workspace) != nil || validateAIImageWorkspace(&workspace) != nil {
			http.Error(w, "read AI image workspace failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, workspace)
		return
	}
	var workspace aiImageWorkspace
	if err := decodeAIImageJSON(w, r, &workspace); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateAIImageWorkspace(&workspace); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := writeAIImageJSONAtomic(path, workspace); err != nil {
		http.Error(w, "save AI image workspace failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, workspace)
}

func (s *Server) ensureAIImageProjects() ([]db.AIImageProject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	projects, err := s.db.ListAIImageProjects()
	if err != nil || len(projects) > 0 {
		return projects, err
	}
	workspace := defaultAIImageWorkspace()
	path, err := s.aiImageWorkspacePath()
	if err != nil {
		return nil, err
	}
	data, readErr := os.ReadFile(path)
	if readErr == nil {
		if err := json.Unmarshal(data, &workspace); err != nil {
			return nil, fmt.Errorf("decode legacy AI image workspace: %w", err)
		}
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	}
	if err := validateAIImageWorkspace(&workspace); err != nil {
		return nil, err
	}
	data, err = json.Marshal(workspace)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.CreateAIImageProject("默认项目", string(data)); err != nil {
		return nil, err
	}
	return s.db.ListAIImageProjects()
}

func aiImageProjectFromDB(project *db.AIImageProject) (aiImageProjectResponse, error) {
	var workspace aiImageWorkspace
	if err := json.Unmarshal([]byte(project.WorkspaceJSON), &workspace); err != nil {
		return aiImageProjectResponse{}, err
	}
	if err := validateAIImageWorkspace(&workspace); err != nil {
		return aiImageProjectResponse{}, err
	}
	return aiImageProjectResponse{
		ID: project.ID, Name: project.Name, Workspace: workspace,
		CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
	}, nil
}

func (s *Server) handleAPIAIImageProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.ensureAIImageProjects()
	if err != nil {
		http.Error(w, "read AI image projects failed", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet {
		summaries := make([]aiImageProjectSummary, 0, len(projects))
		for _, project := range projects {
			summaries = append(summaries, aiImageProjectSummary{
				ID: project.ID, Name: project.Name,
				CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
			})
		}
		writeJSON(w, summaries)
		return
	}
	if len(projects) >= 100 {
		http.Error(w, "AI image project limit exceeded", http.StatusConflict)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeAIImageJSON(w, r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" || len(body.Name) > 100 {
		http.Error(w, "invalid AI image project name", http.StatusBadRequest)
		return
	}
	workspace := defaultAIImageWorkspace()
	data, err := json.Marshal(workspace)
	if err != nil {
		http.Error(w, "create AI image project failed", http.StatusInternalServerError)
		return
	}
	project, err := s.db.CreateAIImageProject(body.Name, string(data))
	if err != nil {
		http.Error(w, "create AI image project failed", http.StatusInternalServerError)
		return
	}
	response, err := aiImageProjectFromDB(project)
	if err != nil {
		http.Error(w, "create AI image project failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, response)
}

func (s *Server) handleAPIAIImageProject(w http.ResponseWriter, r *http.Request) {
	if _, err := s.ensureAIImageProjects(); err != nil {
		http.Error(w, "read AI image projects failed", http.StatusInternalServerError)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid AI image project", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodGet {
		project, err := s.db.GetAIImageProject(id)
		if err != nil {
			if err == sql.ErrNoRows {
				http.NotFound(w, r)
			} else {
				http.Error(w, "read AI image project failed", http.StatusInternalServerError)
			}
			return
		}
		response, err := aiImageProjectFromDB(project)
		if err != nil {
			http.Error(w, "read AI image project failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, response)
		return
	}
	var body struct {
		Name      string           `json:"name"`
		Workspace aiImageWorkspace `json:"workspace"`
	}
	if err := decodeAIImageJSON(w, r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" || len(body.Name) > 100 || validateAIImageWorkspace(&body.Workspace) != nil {
		http.Error(w, "invalid AI image project", http.StatusBadRequest)
		return
	}
	data, err := json.Marshal(body.Workspace)
	if err != nil {
		http.Error(w, "save AI image project failed", http.StatusInternalServerError)
		return
	}
	project, err := s.db.UpdateAIImageProject(id, body.Name, string(data))
	if err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
		} else {
			http.Error(w, "save AI image project failed", http.StatusInternalServerError)
		}
		return
	}
	response, err := aiImageProjectFromDB(project)
	if err != nil {
		http.Error(w, "save AI image project failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, response)
}

func validateAIImageWorkspace(workspace *aiImageWorkspace) error {
	if len(workspace.Prompts) > 100 || len(workspace.Nodes) > 300 {
		return fmt.Errorf("workspace limit exceeded")
	}
	ids := map[string]bool{}
	refLabels := map[string]bool{}
	for _, prompt := range workspace.Prompts {
		if prompt.ID == "" || len(prompt.ID) > 100 || strings.TrimSpace(prompt.Title) == "" || len(prompt.Title) > 100 || strings.TrimSpace(prompt.Text) == "" || len(prompt.Text) > 4000 || ids[prompt.ID] {
			return fmt.Errorf("invalid prompt preset")
		}
		ids[prompt.ID] = true
	}
	for _, node := range workspace.Nodes {
		if node.ID == "" || len(node.ID) > 100 || ids[node.ID] || (node.Kind != "prompt" && node.Kind != "image" && node.Kind != "video") || node.Width < 80 || node.Width > 2000 || node.Height < 60 || node.Height > 2000 || len(node.Text) > 8000 {
			return fmt.Errorf("invalid canvas node")
		}
		if (node.Kind == "image" || node.Kind == "video") && (node.AssetName == "" || filepath.Base(node.AssetName) != node.AssetName) {
			return fmt.Errorf("invalid canvas media")
		}
		if node.RefLabel != "" {
			prefix := "图"
			if node.Kind == "video" {
				prefix = "视频"
			}
			digits := strings.TrimPrefix(node.RefLabel, prefix)
			if node.Kind == "prompt" || digits == node.RefLabel || (len(digits) != 2 && len(digits) != 3) || refLabels[node.RefLabel] {
				return fmt.Errorf("invalid canvas reference label")
			}
			for _, digit := range digits {
				if digit < '0' || digit > '9' {
					return fmt.Errorf("invalid canvas reference label")
				}
			}
			refLabels[node.RefLabel] = true
		}
		ids[node.ID] = true
	}
	if workspace.Viewport.Zoom < 0.1 || workspace.Viewport.Zoom > 4 {
		return fmt.Errorf("invalid canvas viewport")
	}
	return nil
}

func writeAIImageJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".json-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func (s *Server) handleAPIAIImagePlan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode             string `json:"mode"`
		Category         string `json:"category"`
		Composition      string `json:"composition"`
		Variant          string `json:"variant"`
		ProductAssetName string `json:"product_asset_name"`
		ModelAssetName   string `json:"model_asset_name"`
		Prompt           string `json:"prompt"`
		AttachmentType   string `json:"attachment_type"`
		BraceletType     string `json:"bracelet_type"`
	}
	if err := decodeAIImageJSON(w, r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, status := s.prepareAIImagePlan(body.Mode, body.Category, body.Composition, body.Variant, body.ProductAssetName, body.ModelAssetName, body.Prompt, body.AttachmentType, body.BraceletType)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, result)
}

func (s *Server) prepareAIImagePlan(modeRaw, categoryRaw, compositionRaw, variantRaw, productName, modelName, extraPrompt, attachmentType, braceletType string) (aiImagePlanResponse, int) {
	result := aiImagePlanResponse{Status: "invalid_input", ArtifactPaths: []string{}}
	if len(extraPrompt) > 4000 {
		return invalidAIImagePlan(result, "prompt_too_long"), http.StatusBadRequest
	}
	mode, category := strings.TrimSpace(modeRaw), strings.TrimSpace(categoryRaw)
	validMode := mode == "single" || mode == "grid-9"
	validCategory := category == "earring" || category == "ring" || category == "bracelet" || category == "necklace"
	if validMode {
		result.Mode = stringPointer(mode)
	}
	if validCategory {
		result.Category = stringPointer(category)
	}

	composition := strings.TrimSpace(compositionRaw)
	validComposition := false
	if validMode {
		if mode == "single" {
			validComposition = composition == "worn" || composition == "still_life"
			if validComposition {
				result.Composition = stringPointer(composition)
			}
		} else {
			validComposition = composition == ""
		}
	}

	variant := strings.TrimSpace(variantRaw)
	validVariant := false
	if validCategory {
		if category == "necklace" {
			validVariant = variant == "single" || variant == "integrated_double"
			if validVariant {
				result.Variant = stringPointer(variant)
			}
		} else {
			validVariant = variant == ""
		}
	}
	if !validMode {
		return invalidAIImagePlan(result, "unsupported_mode"), http.StatusBadRequest
	}
	if !validCategory {
		return invalidAIImagePlan(result, "unsupported_category"), http.StatusBadRequest
	}
	if !validComposition {
		return invalidAIImagePlan(result, "unsupported_composition"), http.StatusBadRequest
	}
	if !validVariant {
		return invalidAIImagePlan(result, "unsupported_variant"), http.StatusBadRequest
	}
	if category == "earring" && strings.TrimSpace(attachmentType) == "clip_on" && (mode != "single" || composition != "still_life") {
		return invalidAIImagePlan(result, "unsupported_attachment_type"), http.StatusBadRequest
	}
	if category == "bracelet" && (strings.TrimSpace(braceletType) == "open" || strings.TrimSpace(braceletType) == "cuff") {
		return invalidAIImagePlan(result, "unsupported_open_bracelet"), http.StatusBadRequest
	}
	result.TemplateID = stringPointer("jewelry-" + category)
	result.TemplateVersion = stringPointer("1.0.0")
	if !s.validManagedAIImageReference(strings.TrimSpace(productName)) {
		return invalidAIImagePlan(result, "missing_product_reference"), http.StatusBadRequest
	}
	result.ReferenceCount = 1
	if modelName != "" {
		if !s.validManagedAIImageReference(strings.TrimSpace(modelName)) {
			return invalidAIImagePlan(result, "invalid_model_reference"), http.StatusBadRequest
		}
		result.ReferenceCount++
	}
	prompt := buildAIImagePrompt(mode, category, composition, variant, strings.TrimSpace(extraPrompt), modelName != "")
	negative := "人脸、可识别面部特征、文字、水印、Logo、额外首饰、悬浮产品、裁切产品、改变产品结构或细节、过度磨皮"
	manual := "请配置并授权真实生图服务后执行，或将这份提示词复制到已授权工具中；生成结果仍需与产品参考图人工核对。"
	errorCode := "provider_not_selected"
	result.Status = "prompt_only"
	result.Prompt = &prompt
	result.NegativePrompt = &negative
	result.ErrorCode = &errorCode
	result.ManualAction = &manual
	return result, http.StatusOK
}

func invalidAIImagePlan(result aiImagePlanResponse, code string) aiImagePlanResponse {
	result.ErrorCode = &code
	result.ArtifactPaths = []string{}
	return result
}

func (s *Server) validManagedAIImageReference(name string) bool {
	if name == "" || filepath.Base(name) != name {
		return false
	}
	dir, err := s.ensureAIImageAssetDir()
	if err != nil {
		return false
	}
	path := filepath.Join(dir, name)
	managed, ok := aiImageManagedName(dir, path)
	if !ok || managed != name {
		return false
	}
	items, err := s.db.ListMediaLibrary(500)
	if err != nil {
		return false
	}
	indexed := false
	for _, item := range items {
		if itemName, itemOK := aiImageManagedName(dir, item.FilePath); itemOK && itemName == name && strings.HasPrefix(item.Mime, "image/") {
			indexed = true
			break
		}
	}
	if !indexed {
		return false
	}
	_, _, _, err = decodeAIImage(path)
	return err == nil
}

// loadAIImageReference turns one already-uploaded asset name into the bytes and
// mime type handed to imagegen.Simple. The name must be a bare file name that
// resolves inside the managed asset directory, so absolute paths, "..",
// escaping symlinks and non-image files are all rejected before any byte is
// read. An empty name means "text to image".
func (s *Server) loadAIImageReference(name string) ([]byte, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", nil
	}
	if name == "." || name == ".." || filepath.Base(name) != name {
		return nil, "", imagegen.ErrInvalidReference
	}
	dir, err := s.ensureAIImageAssetDir()
	if err != nil {
		return nil, "", imagegen.ErrInvalidReference
	}
	path := filepath.Join(dir, name)
	managed, ok := aiImageManagedName(dir, path)
	if !ok || managed != name {
		return nil, "", imagegen.ErrInvalidReference
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > imagegen.MaxImageBytes {
		return nil, "", imagegen.ErrInvalidReference
	}
	if !isAIImageReferenceMime(media.GuessMime(path)) {
		return nil, "", imagegen.ErrInvalidReference
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", imagegen.ErrInvalidReference
	}
	// The stored extension is only a hint; the provider needs the real mime.
	detected := http.DetectContentType(data)
	if !isAIImageReferenceMime(detected) {
		return nil, "", imagegen.ErrInvalidReference
	}
	return data, detected, nil
}

func isAIImageReferenceMime(mime string) bool {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png", "image/jpeg", "image/webp":
		return true
	}
	return false
}

func buildAIImagePrompt(mode, category, composition, variant, extra string, hasModelReference bool) string {
	categoryNames := map[string]string{"earring": "耳饰", "ring": "戒指", "bracelet": "闭合手链", "necklace": "项链"}
	physical := map[string]string{
		"earring":  "佩戴构图中耳饰与耳部接触必须符合真实结构；静物构图必须完整、不裁切并留白。",
		"ring":     "佩戴构图中戒指必须真实环绕手指；静物构图必须完整、不裁切并留白。",
		"bracelet": "手链必须始终是闭合、完整的环，佩戴时真实环绕手腕，静物不得裁切。",
		"necklace": "项链必须连续环绕颈部，静物必须完整、不裁切并留白。",
	}
	parts := []string{"以已导入的产品参考图作为产品外观、结构和可见细节的唯一真源，创作" + categoryNames[category] + "社媒视觉；不得猜测或改变材质、宝石数量、刻字、扣头、Logo 或尺寸。"}
	if mode == "grid-9" {
		parts = append(parts, "输出一个有统一视觉语言的完整九宫格构图，严格包含 6 格真实佩戴图和 3 格完整静物图；不是九张互不相关的单图。")
	} else if composition == "worn" {
		parts = append(parts, "输出一张自然佩戴特写，保留真实皮肤纹理和产品接触关系。")
	} else {
		parts = append(parts, "输出一张完整静物棚拍，产品四周保留呼吸空间。")
	}
	parts = append(parts, physical[category])
	if category == "necklace" {
		parts = append(parts, "项链款式必须保持为明确变体："+variant+"。")
	}
	if hasModelReference {
		parts = append(parts, "模特参考图只用于姿势和构图，不得覆盖产品参考图中的产品证据。")
	}
	if extra != "" {
		runes := []rune(extra)
		if len(runes) > 2000 {
			extra = string(runes[:2000])
		}
		parts = append(parts, "用户创意方向："+extra)
	}
	parts = append(parts, "画面不得出现人脸、可识别面部特征、文字、水印、Logo 或额外首饰。")
	return strings.Join(parts, " ")
}

func stringPointer(value string) *string { return &value }

const (
	codeAIImageOriginDenied        = "AI_IMAGE_ORIGIN_DENIED"
	codeAIImageInvalidRequest      = "AI_IMAGE_INVALID_REQUEST"
	codeAIImagePromptRequired      = "AI_IMAGE_PROMPT_REQUIRED"
	codeAIImagePromptTooLong       = "AI_IMAGE_PROMPT_TOO_LONG"
	codeAIImageProviderUnsupported = "AI_IMAGE_PROVIDER_UNSUPPORTED"
	codeAIImageNotConfigured       = "AI_IMAGE_NOT_CONFIGURED"
	codeAIImageModelUnsupported    = "AI_IMAGE_MODEL_UNSUPPORTED"
	codeAIImageInvalidReference    = "AI_IMAGE_INVALID_REFERENCE"
	codeAIImageResultRejected      = "AI_IMAGE_RESULT_REJECTED"
	codeAIImageGenerationFailed    = "AI_IMAGE_GENERATION_FAILED"
	codeAIImageSaveFailed          = "AI_IMAGE_SAVE_FAILED"
)

type aiImageGenerateResponse struct {
	OK    bool   `json:"ok"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Mime  string `json:"mime"`
	Bytes int    `json:"bytes"`
	Model string `json:"model"`
}

// handleAPIAIImageGenerate performs one synchronous provider call and writes
// the returned bytes into the managed asset folder immediately. Provider URLs
// are never returned to the client because they expire.
func (s *Server) handleAPIAIImageGenerate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeAIMediaError(w, http.StatusForbidden, codeAIImageOriginDenied)
		return
	}
	var body struct {
		Prompt string `json:"prompt"`
		Model  string `json:"model"`
		// Deprecated knobs: still parsed so older callers keep working, but both
		// values are now pinned inside imagegen.Simple and ignored here.
		AspectRatio string `json:"aspect_ratio"`
		ImageSize   string `json:"image_size"`
		// ReferenceImage is the managed asset name returned by
		// POST /api/ai-image/assets/upload. Never a URL, dataURL or raw bytes.
		ReferenceImage string `json:"reference_image"`
	}
	if err := decodeAIImageJSON(w, r, &body); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, codeAIImageInvalidRequest, "invalid request body")
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" || !utf8.ValidString(prompt) {
		writeDataAPIError(w, http.StatusBadRequest, codeAIImagePromptRequired, "prompt is required")
		return
	}
	if utf8.RuneCountInString(prompt) > maxAIImageGeneratePromptChars {
		writeDataAPIError(w, http.StatusBadRequest, codeAIImagePromptTooLong, "prompt is too long")
		return
	}

	s.mu.Lock()
	provider := strings.TrimSpace(s.cfg.ImageAPIProvider)
	baseURL := strings.TrimSpace(s.cfg.ImageAPIBaseURL)
	apiKey := strings.TrimSpace(s.cfg.ImageAPIKey)
	configuredModel := strings.TrimSpace(s.cfg.AIImageModel)
	s.mu.Unlock()

	if !isZexAPIImageProvider(provider) {
		writeDataAPIError(w, http.StatusBadRequest, codeAIImageProviderUnsupported, "active image provider does not support this endpoint")
		return
	}
	if baseURL == "" || apiKey == "" {
		writeDataAPIError(w, http.StatusBadRequest, codeAIImageNotConfigured, "image provider is not configured")
		return
	}
	model := strings.TrimSpace(body.Model)
	if model == "" {
		model = configuredModel
	}
	if model == "" {
		model = imagegen.DefaultModel
	}
	if !imagegen.IsSupportedModel(model) {
		writeDataAPIError(w, http.StatusBadRequest, codeAIImageModelUnsupported, "unsupported image model")
		return
	}
	refImage, refMime, err := s.loadAIImageReference(strings.TrimSpace(body.ReferenceImage))
	if err != nil {
		writeDataAPIError(w, http.StatusBadRequest, codeAIImageInvalidReference, "reference image is not usable")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), aiImageGenerateTimeout)
	defer cancel()
	client := &imagegen.Client{BaseURL: baseURL, APIKey: apiKey, Model: model}
	data, mime, err := client.Simple(ctx, prompt, refImage, refMime)
	if err != nil {
		status, code := aiImageGenerateError(err)
		// The provider message is intentionally discarded: it may echo the key
		// or upstream internals.
		writeDataAPIError(w, status, code, "image generation failed")
		return
	}
	if len(data) == 0 || len(data) > maxAIImageGenerateBytes {
		writeDataAPIError(w, http.StatusBadGateway, codeAIImageResultRejected, "provider image was rejected")
		return
	}
	asset, err := s.saveGeneratedAIImage(data, mime)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, codeAIImageSaveFailed, "save generated image failed")
		return
	}
	writeJSON(w, aiImageGenerateResponse{OK: true, Name: asset.Name, URL: asset.URL, Mime: asset.Mime, Bytes: int(asset.SizeBytes), Model: model})
}

func aiImageGenerateError(err error) (int, string) {
	switch {
	case errors.Is(err, imagegen.ErrKeyMissing):
		return http.StatusBadRequest, codeAIImageNotConfigured
	case errors.Is(err, imagegen.ErrInvalidBaseURL), errors.Is(err, imagegen.ErrInvalidModel):
		return http.StatusBadRequest, codeAIImageNotConfigured
	case errors.Is(err, imagegen.ErrPromptRequired), errors.Is(err, imagegen.ErrPromptTooLong):
		return http.StatusBadRequest, codeAIImageInvalidRequest
	case errors.Is(err, imagegen.ErrInvalidReference):
		return http.StatusBadRequest, codeAIImageInvalidReference
	case errors.Is(err, imagegen.ErrUnsupportedMime), errors.Is(err, imagegen.ErrImageTooLarge), errors.Is(err, imagegen.ErrEmptyResult), errors.Is(err, imagegen.ErrUnsafeImageURL):
		return http.StatusBadGateway, codeAIImageResultRejected
	default:
		return http.StatusBadGateway, codeAIImageGenerationFailed
	}
}

// saveGeneratedAIImage performs the durable step: content-addressed name inside
// the managed asset directory, atomic write, then a media-library row so the
// existing list and file endpoints both see it.
func (s *Server) saveGeneratedAIImage(data []byte, mime string) (aiImageAsset, error) {
	ext, ok := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[strings.ToLower(strings.TrimSpace(mime))]
	if !ok {
		return aiImageAsset{}, fmt.Errorf("unsupported generated image mime")
	}
	dir, err := s.ensureAIImageAssetDir()
	if err != nil {
		return aiImageAsset{}, err
	}
	temp, err := os.CreateTemp(dir, ".generated-*"+ext)
	if err != nil {
		return aiImageAsset{}, err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return aiImageAsset{}, err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return aiImageAsset{}, err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return aiImageAsset{}, err
	}
	if err := temp.Close(); err != nil {
		return aiImageAsset{}, err
	}
	hash, size, err := media.HashFile(tempName)
	if err != nil {
		return aiImageAsset{}, err
	}
	name := hash + ext
	destination := filepath.Join(dir, name)
	if err := os.Rename(tempName, destination); err != nil {
		return aiImageAsset{}, err
	}
	// The name is derived from a hex digest plus a fixed extension, so it can
	// never contain a separator; the managed-name check is a second guard.
	if managed, ok := aiImageManagedName(dir, destination); !ok || managed != name {
		os.Remove(destination)
		return aiImageAsset{}, fmt.Errorf("generated image name is not managed")
	}
	width, height := 0, 0
	if file, openErr := os.Open(destination); openErr == nil {
		if config, _, decodeErr := image.DecodeConfig(file); decodeErr == nil {
			width, height = config.Width, config.Height
		}
		file.Close()
	}
	if int64(width)*int64(height) > maxAIImagePixels {
		os.Remove(destination)
		return aiImageAsset{}, fmt.Errorf("generated image is too large")
	}
	item, err := s.db.UpsertMediaLibrary(destination, hash, size, mime, width, height, 0, "")
	if err != nil {
		return aiImageAsset{}, err
	}
	_, _ = s.db.CreateAuditEvent(string(trace.New()), "info", "ai-image", "media asset generated", "media_library", item.ID, "")
	return aiImageAssetFromItem(item.ID, name, item.SHA256, item.Mime, item.Width, item.Height, item.DurationMS, item.SizeBytes, item.CreatedAt), nil
}
