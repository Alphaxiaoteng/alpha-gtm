package ui

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"alpha-nexus-egolite/internal/agnes"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/media"
	"alpha-nexus-egolite/internal/tools"
)

const (
	maxAIMediaPromptBytes           = 32 << 10
	maxAIMediaRetryableStatusErrors = 5
	maxAIMediaRetryBackoff          = 30 * time.Second
	generationProviderStateUnknown  = "GENERATION_PROVIDER_STATE_UNKNOWN"
	derivedFirstFramePrompt         = "Create one frame that matches the supplied reference as closely as possible. Preserve the subject, composition, colors, and framing as much as possible. Do not add text, logos, watermarks, or new objects."
)

type agnesMediaClient interface {
	GenerateImageBase64(context.Context, agnes.ImageRequest) (agnes.ImageResult, error)
	GenerateImageURL(context.Context, agnes.ImageRequest) (agnes.ImageURLResult, error)
	CreateVideo(context.Context, agnes.VideoRequest) (agnes.VideoTask, error)
	GetVideoStatus(context.Context, string) (agnes.VideoTask, error)
	DownloadVideo(context.Context, string, io.Writer) (agnes.DownloadResult, error)
}

type aiMediaGenerationResponse struct {
	ID              int64                                `json:"id"`
	RequestID       string                               `json:"request_id"`
	Kind            string                               `json:"kind"`
	Mode            string                               `json:"mode"`
	Provider        string                               `json:"provider"`
	Model           string                               `json:"model"`
	OutputSize      string                               `json:"output_size"`
	DurationSeconds int                                  `json:"duration_seconds"`
	PromptHash      string                               `json:"prompt_hash"`
	References      []aiMediaGenerationReferenceResponse `json:"references"`
	Status          string                               `json:"status"`
	Progress        int                                  `json:"progress"`
	ErrorCode       string                               `json:"error_code,omitempty"`
	OutputAsset     *aiImageAsset                        `json:"output_asset,omitempty"`
	CreatedAt       string                               `json:"created_at"`
	UpdatedAt       string                               `json:"updated_at"`
}

type aiMediaGenerationBatchResponse struct {
	BatchID string                      `json:"batch_id"`
	Count   int                         `json:"count"`
	Items   []aiMediaGenerationResponse `json:"items"`
}

type aiMediaGenerationReferenceInput struct {
	AssetName string `json:"asset_name"`
	Role      string `json:"role"`
}

type aiMediaGenerationReferenceResponse struct {
	AssetName string `json:"asset_name"`
	Role      string `json:"role"`
	SHA256    string `json:"sha256"`
}

func (s *Server) handleAPIAIImageCredential(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeAIMediaError(w, http.StatusForbidden, "CREDENTIAL_ORIGIN_DENIED")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAIMediaError(w, http.StatusUnsupportedMediaType, "JSON_CONTENT_TYPE_REQUIRED")
		return
	}
	var body struct {
		APIKey string `json:"api_key"`
	}
	if err := decodeAIImageJSON(w, r, &body); err != nil {
		writeAIMediaError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	s.mu.Lock()
	activeProvider := strings.TrimSpace(s.cfg.AIImageProvider)
	if activeProvider == "" {
		activeProvider = strings.TrimSpace(s.cfg.ImageAPIProvider)
	}
	s.mu.Unlock()

	// zexapi keys are not Agnes keychain entries: the generate handler reads
	// cfg.ImageAPIKey, so a keychain write would leave the provider permanently
	// unconfigured. Persist into the image_api_key config field instead.
	if isZexAPIImageProvider(activeProvider) {
		key := strings.TrimSpace(body.APIKey)
		if key == "" {
			writeAIMediaError(w, http.StatusBadRequest, "VALIDATION_FAILED")
			return
		}
		dataDir, err := s.cfg.ResolvedDataDir()
		if err != nil {
			writeAIMediaError(w, http.StatusInternalServerError, "CONFIG_UNAVAILABLE")
			return
		}
		s.mu.Lock()
		_, persistErr := s.updatePersistedConfigLocked(dataDir, func(existing map[string]any) {
			existing["image_api_provider"] = aiImageProviderZexAPI
			existing["image_api_key"] = key
		})
		if persistErr == nil {
			s.cfg.ImageAPIProvider = aiImageProviderZexAPI
			s.cfg.ImageAPIKey = key
		}
		s.mu.Unlock()
		if persistErr != nil {
			writeAIMediaError(w, http.StatusInternalServerError, "CONFIG_SAVE_FAILED")
			return
		}
		writeJSON(w, map[string]bool{"key_updated": true})
		return
	}

	if s.agnesSaveAPIKey == nil {
		writeAIMediaError(w, http.StatusServiceUnavailable, string(agnes.CodeKeychainUnavailable))
		return
	}
	if err := s.agnesSaveAPIKey(body.APIKey); err != nil {
		writeAIMediaError(w, http.StatusBadRequest, string(agnes.CodeOf(err)))
		return
	}
	writeJSON(w, map[string]bool{"key_updated": true})
}

func sameOriginLoopbackRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Path != "" {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	requestHost := r.Host
	if parsed.Host == requestHost {
		return true
	}
	requestName, requestPort, err := net.SplitHostPort(requestHost)
	if err != nil {
		return false
	}
	originPort := parsed.Port()
	if originPort == "" || originPort != requestPort {
		return false
	}
	if requestName == "localhost" {
		requestName = "127.0.0.1"
	}
	requestIP := net.ParseIP(strings.Trim(requestName, "[]"))
	return requestIP != nil && requestIP.IsLoopback()
}

func (s *Server) handleAPIAIMediaGenerations(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method == http.MethodGet {
		jobs, err := s.db.ListRecentGenerationJobs(50)
		if err != nil {
			writeAIMediaError(w, http.StatusInternalServerError, "GENERATION_LIST_FAILED")
			return
		}
		responses := make([]aiMediaGenerationResponse, 0, len(jobs))
		for index := range jobs {
			responses = append(responses, s.aiMediaGenerationResponse(&jobs[index]))
		}
		writeJSON(w, responses)
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeAIMediaError(w, http.StatusForbidden, "GENERATION_ORIGIN_DENIED")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAIMediaError(w, http.StatusUnsupportedMediaType, "JSON_CONTENT_TYPE_REQUIRED")
		return
	}
	var body struct {
		RequestID       string                            `json:"request_id"`
		Kind            string                            `json:"kind"`
		Mode            string                            `json:"mode"`
		Model           string                            `json:"model"`
		OutputSize      string                            `json:"output_size"`
		DurationSeconds int                               `json:"duration_seconds"`
		Prompt          string                            `json:"prompt"`
		References      []aiMediaGenerationReferenceInput `json:"references"`
	}
	if err := decodeAIImageJSON(w, r, &body); err != nil {
		writeAIMediaError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	if strings.TrimSpace(body.RequestID) == "" {
		body.RequestID, err = newAIMediaRequestID()
		if err != nil {
			writeAIMediaError(w, http.StatusInternalServerError, "GENERATION_RUNTIME_FAILED")
			return
		}
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeAIMediaError(w, http.StatusInternalServerError, "GENERATION_RUNTIME_FAILED")
		return
	}
	runtime := &tools.Runtime{
		DataDir: dataDir, TrustedLocalUI: true, GenerateMedia: s.enqueueAIMediaGeneration,
	}
	result, _ := tools.Default.Call(r.Context(), runtime, "media.generate", map[string]interface{}{
		"request_id": body.RequestID, "kind": body.Kind, "mode": body.Mode, "model": body.Model,
		"output_size": body.OutputSize, "duration_seconds": body.DurationSeconds,
		"prompt": body.Prompt, "references": body.References, "confirm": true,
	})
	_ = runtime.CloseEvents()
	if result == nil || !result.OK {
		code := "GENERATION_QUEUE_FAILED"
		if result != nil && result.Error != "" {
			code = result.Error
		}
		status := http.StatusBadRequest
		if code == string(agnes.CodeKeyMissing) || code == "AGNES_CREDENTIAL_MISSING" {
			status = http.StatusConflict
		} else if code == "GENERATION_REQUEST_CONFLICT" {
			status = http.StatusConflict
		} else if code == string(agnes.CodeKeychainUnavailable) || code == string(agnes.CodeKeychainReadFailed) {
			status = http.StatusServiceUnavailable
		}
		writeAIMediaError(w, status, code)
		return
	}
	jobID, ok := result.Data["job_id"].(int64)
	if !ok || jobID <= 0 {
		writeAIMediaError(w, http.StatusInternalServerError, "GENERATION_QUEUE_FAILED")
		return
	}
	job, err := s.db.GetGenerationJob(jobID)
	if err != nil {
		writeAIMediaError(w, http.StatusInternalServerError, "GENERATION_READ_FAILED")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, s.aiMediaGenerationResponse(job))
}

func (s *Server) handleAPIAIMediaGenerationBatch(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeAIMediaError(w, http.StatusForbidden, "GENERATION_ORIGIN_DENIED")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAIMediaError(w, http.StatusUnsupportedMediaType, "JSON_CONTENT_TYPE_REQUIRED")
		return
	}
	var body struct {
		RequestID       string                            `json:"request_id"`
		Kind            string                            `json:"kind"`
		Mode            string                            `json:"mode"`
		Model           string                            `json:"model"`
		OutputSize      string                            `json:"output_size"`
		DurationSeconds int                               `json:"duration_seconds"`
		Prompt          string                            `json:"prompt"`
		References      []aiMediaGenerationReferenceInput `json:"references"`
		Count           int                               `json:"count"`
	}
	if err := decodeAIImageJSON(w, r, &body); err != nil {
		writeAIMediaError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	if strings.TrimSpace(body.RequestID) == "" {
		body.RequestID, err = newAIMediaRequestID()
		if err != nil {
			writeAIMediaError(w, http.StatusInternalServerError, "GENERATION_RUNTIME_FAILED")
			return
		}
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeAIMediaError(w, http.StatusInternalServerError, "GENERATION_RUNTIME_FAILED")
		return
	}
	runtime := &tools.Runtime{
		DataDir: dataDir, TrustedLocalUI: true, GenerateMedia: s.enqueueAIMediaGeneration,
	}
	result, _ := tools.Default.Call(r.Context(), runtime, "media.generate.batch", map[string]interface{}{
		"request_id": body.RequestID, "kind": body.Kind, "mode": body.Mode, "prompt": body.Prompt,
		"model": body.Model, "output_size": body.OutputSize, "duration_seconds": body.DurationSeconds,
		"references": body.References, "count": body.Count, "confirm": true,
	})
	_ = runtime.CloseEvents()
	if result == nil || !result.OK {
		code := "GENERATION_QUEUE_FAILED"
		if result != nil && result.Error != "" {
			code = result.Error
		}
		status := http.StatusBadRequest
		if code == string(agnes.CodeKeyMissing) || code == "AGNES_CREDENTIAL_MISSING" || code == "GENERATION_REQUEST_CONFLICT" {
			status = http.StatusConflict
		} else if code == string(agnes.CodeKeychainUnavailable) || code == string(agnes.CodeKeychainReadFailed) {
			status = http.StatusServiceUnavailable
		}
		writeAIMediaError(w, status, code)
		return
	}
	jobIDs, ok := result.Data["job_ids"].([]int64)
	if !ok || len(jobIDs) != body.Count {
		writeAIMediaError(w, http.StatusInternalServerError, "GENERATION_QUEUE_FAILED")
		return
	}
	items := make([]aiMediaGenerationResponse, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		job, readErr := s.db.GetGenerationJob(jobID)
		if readErr != nil {
			writeAIMediaError(w, http.StatusInternalServerError, "GENERATION_READ_FAILED")
			return
		}
		items = append(items, s.aiMediaGenerationResponse(job))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, aiMediaGenerationBatchResponse{BatchID: body.RequestID, Count: len(items), Items: items})
}

func newAIMediaRequestID() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return "gen-" + hex.EncodeToString(nonce[:]), nil
}

func (s *Server) handleAPIAIMediaGeneration(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeAIMediaError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	job, err := s.db.GetGenerationJob(id)
	if err != nil {
		writeAIMediaError(w, http.StatusNotFound, "GENERATION_NOT_FOUND")
		return
	}
	writeJSON(w, s.aiMediaGenerationResponse(job))
}

func (s *Server) enqueueAIMediaGeneration(_ context.Context, args map[string]interface{}) (*tools.Result, error) {
	requestID := strings.TrimSpace(fmt.Sprint(args["request_id"]))
	kind := strings.TrimSpace(fmt.Sprint(args["kind"]))
	mode, _ := args["mode"].(string)
	mode = strings.TrimSpace(mode)
	if mode == "" {
		if kind == "video" {
			mode = db.GenerationModeTextToVideo
		} else {
			mode = db.GenerationModeTextToImage
		}
	}
	prompt := strings.TrimSpace(fmt.Sprint(args["prompt"]))
	if (kind != "image" && kind != "video") || prompt == "" || len(prompt) > maxAIMediaPromptBytes || !utf8.ValidString(prompt) {
		return &tools.Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("VALIDATION_FAILED")
	}
	inputs, ok := decodeAIMediaGenerationReferenceInputs(args["references"])
	if !ok {
		return &tools.Result{OK: false, Error: "GENERATION_REFERENCE_INVALID"}, fmt.Errorf("GENERATION_REFERENCE_INVALID")
	}
	references, code := s.resolveAIMediaGenerationReferences(kind, mode, prompt, inputs)
	if code != "" {
		return &tools.Result{OK: false, Error: code}, fmt.Errorf("%s", code)
	}
	if s.agnesAPIKeyConfigured == nil {
		return &tools.Result{OK: false, Error: string(agnes.CodeKeychainUnavailable)}, fmt.Errorf("%s", agnes.CodeKeychainUnavailable)
	}
	configured, err := s.agnesAPIKeyConfigured()
	if err != nil {
		code := string(agnes.CodeOf(err))
		return &tools.Result{OK: false, Error: code}, fmt.Errorf("%s", code)
	}
	if !configured {
		return &tools.Result{OK: false, Error: string(agnes.CodeKeyMissing)}, fmt.Errorf("%s", agnes.CodeKeyMissing)
	}
	cfg, err := s.currentAIImageConfig()
	if err != nil {
		return &tools.Result{OK: false, Error: "GENERATION_CONFIG_FAILED"}, fmt.Errorf("GENERATION_CONFIG_FAILED")
	}
	model, _ := args["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		model = cfg.ImageModel
		if kind == "video" {
			model = cfg.VideoModel
		}
	}
	if (kind == "image" && model != agnes.ImageModel20Flash && model != agnes.ImageModel21Flash) ||
		(kind == "video" && model != agnes.VideoModel20) {
		return &tools.Result{OK: false, Error: string(agnes.CodeUnsupportedModel)}, fmt.Errorf("%s", agnes.CodeUnsupportedModel)
	}
	outputSize, _ := args["output_size"].(string)
	durationSeconds, ok := aiMediaDurationSeconds(args["duration_seconds"])
	if !ok {
		return &tools.Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("VALIDATION_FAILED")
	}
	job, created, err := s.db.CreateGenerationJobIdempotentWithOutputAndDuration(requestID, kind, mode, cfg.Provider, model, outputSize, durationSeconds, prompt, references)
	if err != nil {
		if errors.Is(err, db.ErrGenerationRequestConflict) {
			return &tools.Result{OK: false, Error: "GENERATION_REQUEST_CONFLICT"}, fmt.Errorf("GENERATION_REQUEST_CONFLICT")
		}
		return &tools.Result{OK: false, Error: "GENERATION_QUEUE_FAILED"}, fmt.Errorf("GENERATION_QUEUE_FAILED")
	}
	if created {
		s.startAIMediaGeneration(job.ID)
	}
	return &tools.Result{OK: true, Message: "generation queued", Data: map[string]interface{}{"job_id": job.ID, "created": created}}, nil
}

func aiMediaDurationSeconds(value interface{}) (int, bool) {
	if value == nil {
		return 0, true
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), int64(int(typed)) == typed
	case float64:
		value := int(typed)
		return value, typed == float64(value)
	case json.Number:
		value, err := typed.Int64()
		return int(value), err == nil && int64(int(value)) == value
	default:
		return 0, false
	}
}

func decodeAIMediaGenerationReferenceInputs(value interface{}) ([]aiMediaGenerationReferenceInput, bool) {
	if value == nil {
		return nil, true
	}
	if typed, ok := value.([]aiMediaGenerationReferenceInput); ok {
		return append([]aiMediaGenerationReferenceInput(nil), typed...), true
	}
	raw, ok := value.([]interface{})
	if !ok {
		return nil, false
	}
	inputs := make([]aiMediaGenerationReferenceInput, 0, len(raw))
	for _, entry := range raw {
		fields, ok := entry.(map[string]interface{})
		if !ok || len(fields) != 2 {
			return nil, false
		}
		for name := range fields {
			if name != "asset_name" && name != "role" {
				return nil, false
			}
		}
		assetName, assetOK := fields["asset_name"].(string)
		role, roleOK := fields["role"].(string)
		if !assetOK || !roleOK {
			return nil, false
		}
		inputs = append(inputs, aiMediaGenerationReferenceInput{AssetName: assetName, Role: role})
	}
	return inputs, true
}

func (s *Server) resolveAIMediaGenerationReferences(kind, mode, prompt string, inputs []aiMediaGenerationReferenceInput) ([]db.GenerationReference, string) {
	switch mode {
	case db.GenerationModeTextToImage:
		if kind != "image" || len(inputs) != 0 {
			return nil, "GENERATION_MODE_INVALID"
		}
	case db.GenerationModeImageToImage:
		if kind != "image" || len(inputs) == 0 || len(inputs) > agnes.MaxReferenceImages {
			return nil, "GENERATION_MODE_INVALID"
		}
	case db.GenerationModeTextToVideo:
		if kind != "video" || len(inputs) != 0 {
			return nil, "GENERATION_MODE_INVALID"
		}
	case db.GenerationModeImageToVideo:
		if kind != "video" || len(inputs) != 1 || strings.TrimSpace(inputs[0].Role) != "first_frame" {
			return nil, "GENERATION_MODE_INVALID"
		}
	default:
		return nil, "GENERATION_MODE_INVALID"
	}
	if len(inputs) == 0 {
		return []db.GenerationReference{}, ""
	}
	refs := make([]db.GenerationReference, 0, len(inputs))
	seen := make(map[int64]struct{}, len(inputs))
	totalRequestBytes := int64(len(prompt) + 1024)
	for _, input := range inputs {
		name := strings.TrimSpace(input.AssetName)
		role := strings.TrimSpace(input.Role)
		if filepath.Base(name) != name {
			return nil, "GENERATION_REFERENCE_INVALID"
		}
		ext := strings.ToLower(filepath.Ext(name))
		hash := strings.TrimSuffix(name, ext)
		if len(hash) != 64 || (ext != ".png" && ext != ".jpg" && ext != ".gif") {
			return nil, "GENERATION_REFERENCE_INVALID"
		}
		item, err := s.db.GetMediaLibraryItemBySHA256(hash)
		if err != nil {
			return nil, "GENERATION_REFERENCE_NOT_FOUND"
		}
		ref := db.GenerationReference{MediaID: item.ID, AssetName: name, Role: role, SHA256: hash}
		_, _, size, err := s.verifyManagedGenerationReference(ref)
		if err != nil {
			return nil, "GENERATION_REFERENCE_CHANGED"
		}
		if _, exists := seen[item.ID]; exists {
			return nil, "GENERATION_REFERENCE_INVALID"
		}
		seen[item.ID] = struct{}{}
		if mode == db.GenerationModeImageToImage && role != "product" && role != "model" && role != "style" {
			return nil, "GENERATION_REFERENCE_INVALID"
		}
		totalRequestBytes += 4*((size+2)/3) + 64
		if (mode == db.GenerationModeImageToImage || mode == db.GenerationModeImageToVideo) && totalRequestBytes > agnes.MaxImageRequestBytes {
			return nil, "GENERATION_REFERENCE_TOO_LARGE"
		}
		refs = append(refs, ref)
	}
	return refs, ""
}

func (s *Server) verifyManagedGenerationReference(ref db.GenerationReference) (string, string, int64, error) {
	item, err := s.db.GetMediaLibraryItem(ref.MediaID)
	if err != nil || item.SHA256 != ref.SHA256 || !strings.HasPrefix(item.Mime, "image/") {
		return "", "", 0, fmt.Errorf("managed reference metadata changed")
	}
	dir, err := s.ensureAIImageAssetDir()
	if err != nil {
		return "", "", 0, err
	}
	name, ok := aiImageManagedName(dir, item.FilePath)
	if !ok || name != ref.AssetName {
		return "", "", 0, fmt.Errorf("managed reference path changed")
	}
	info, err := os.Lstat(item.FilePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > agnes.MaxReferenceImageBytes {
		return "", "", 0, fmt.Errorf("managed reference file is invalid")
	}
	width, height, format, err := decodeAIImage(item.FilePath)
	if err != nil || width != item.Width || height != item.Height {
		return "", "", 0, fmt.Errorf("managed reference decode changed")
	}
	mimeType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif"}[format]
	if mimeType == "" || mimeType != item.Mime {
		return "", "", 0, fmt.Errorf("managed reference media type changed")
	}
	hash, size, err := media.HashFile(item.FilePath)
	if err != nil || hash != ref.SHA256 || size != info.Size() || size != item.SizeBytes {
		return "", "", 0, fmt.Errorf("managed reference SHA changed")
	}
	return item.FilePath, mimeType, size, nil
}

func (s *Server) generationReferenceDataURI(ref db.GenerationReference) (string, error) {
	path, mimeType, size, err := s.verifyManagedGenerationReference(ref)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("managed reference read failed")
	}
	if err := validateGenerationReferenceBytes(data, ref, mimeType, size); err != nil {
		return "", err
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func validateGenerationReferenceBytes(data []byte, ref db.GenerationReference, expectedMIME string, expectedSize int64) error {
	if len(data) == 0 || int64(len(data)) != expectedSize || int64(len(data)) > agnes.MaxReferenceImageBytes {
		return fmt.Errorf("managed reference byte size changed")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return fmt.Errorf("managed reference byte SHA changed")
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("managed reference bytes are not decodable")
	}
	mimeType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif"}[format]
	bounds := decoded.Bounds()
	if mimeType == "" || mimeType != expectedMIME || bounds.Dx() <= 0 || bounds.Dy() <= 0 || int64(bounds.Dx())*int64(bounds.Dy()) > maxAIImagePixels {
		return fmt.Errorf("managed reference byte metadata changed")
	}
	return nil
}

func (s *Server) startAIMediaGeneration(jobID int64) {
	if jobID <= 0 || s.batchCtx == nil {
		return
	}
	s.batchWG.Add(1)
	go func() {
		defer s.batchWG.Done()
		s.generationMu.Lock()
		defer s.generationMu.Unlock()
		s.executeAIMediaGeneration(s.batchCtx, jobID)
	}()
}

func (s *Server) executeAIMediaGeneration(ctx context.Context, jobID int64) {
	job, err := s.db.GetGenerationJob(jobID)
	if err != nil {
		return
	}
	if job.Status == db.GenerationJobQueued {
		if err := s.db.MarkGenerationJobRunning(job.ID); err != nil {
			return
		}
		job, err = s.db.GetGenerationJob(job.ID)
		if err != nil {
			return
		}
	}
	if job.Status != db.GenerationJobRunning {
		return
	}
	if job.Mode == db.GenerationModeImageToVideo {
		if len(job.References) != 1 {
			s.failAIMediaGeneration(job, "GENERATION_REFERENCE_INVALID", nil)
			return
		}
		if _, _, _, err := s.verifyManagedGenerationReference(job.References[0]); err != nil {
			s.failAIMediaGeneration(job, "GENERATION_REFERENCE_CHANGED", err)
			return
		}
	}
	if s.agnesClient == nil {
		s.failAIMediaGeneration(job, "AGNES_CLIENT_UNAVAILABLE", nil)
		return
	}
	client, err := s.agnesClient(agnes.DefaultTimeout)
	if err != nil {
		s.failAIMediaGeneration(job, string(agnes.CodeOf(err)), err)
		return
	}
	if job.Kind == "image" {
		s.executeAgnesImageGeneration(ctx, client, job)
		return
	}
	s.executeAgnesVideoGeneration(ctx, client, job)
}

func (s *Server) executeAgnesImageGeneration(ctx context.Context, client agnesMediaClient, job *db.GenerationJob) {
	size, ratio := "1K", job.OutputSize
	if job.Model == agnes.ImageModel20Flash {
		size, ratio = agnesImage20Dimensions(job.OutputSize), ""
	}
	images := make([]string, 0, len(job.References))
	for _, reference := range job.References {
		dataURI, err := s.generationReferenceDataURI(reference)
		if err != nil {
			s.failAIMediaGeneration(job, "GENERATION_REFERENCE_CHANGED", err)
			return
		}
		images = append(images, dataURI)
	}
	result, err := client.GenerateImageBase64(ctx, agnes.ImageRequest{Model: job.Model, Prompt: job.Prompt, Size: size, Ratio: ratio, Images: images})
	if err != nil {
		s.failAIMediaGeneration(job, providerSubmissionErrorCode(err), err)
		return
	}
	ext := ".png"
	if result.MediaType == "image/jpeg" {
		ext = ".jpg"
	} else if result.MediaType == "image/gif" {
		ext = ".gif"
	}
	asset, err := s.persistGeneratedBytes(result.Data, ext)
	if err != nil {
		s.failAIMediaGeneration(job, "GENERATION_ARTIFACT_INVALID", err)
		return
	}
	for _, reference := range job.References {
		if asset.SHA256 == reference.SHA256 {
			s.failAIMediaGeneration(job, "GENERATION_ARTIFACT_UNCHANGED", nil)
			return
		}
	}
	if err := s.db.MarkGenerationJobSucceeded(job.ID, asset.ID); err != nil {
		s.failAIMediaGeneration(job, generationProviderStateUnknown, err)
	}
}

func agnesImage20Dimensions(ratio string) string {
	switch ratio {
	case "3:4":
		return "768x1024"
	case "4:3":
		return "1024x768"
	case "16:9":
		return "1024x576"
	case "9:16":
		return "576x1024"
	case "2:3":
		return "672x1008"
	case "3:2":
		return "1008x672"
	case "21:9":
		return "1344x576"
	default:
		return "1024x1024"
	}
}

func agnesVideoDimensions(ratio string) (int, int) {
	switch ratio {
	case "1:1":
		return 1024, 1024
	case "3:4":
		return 768, 1024
	case "4:3":
		return 1024, 768
	case "16:9":
		return 1024, 576
	case "9:16":
		return 576, 1024
	case "2:3":
		return 768, 1152
	default:
		return 1152, 768
	}
}

func (s *Server) executeAgnesVideoGeneration(ctx context.Context, client agnesMediaClient, job *db.GenerationJob) {
	videoID := job.ExternalVideoID
	if videoID == "" {
		imageURL := ""
		if job.Mode == db.GenerationModeImageToVideo {
			dataURI, err := s.generationReferenceDataURI(job.References[0])
			if err != nil {
				s.failAIMediaGeneration(job, "GENERATION_REFERENCE_CHANGED", err)
				return
			}
			// Agnes Video 2.0 accepts only public HTTPS images. Generate a
			// provider-hosted derivative and hand it off immediately; never
			// persist or expose the provider URL.
			frame, err := client.GenerateImageURL(ctx, agnes.ImageRequest{
				Model: agnes.ImageModel21Flash, Prompt: derivedFirstFramePrompt,
				Size: "1K", Ratio: job.OutputSize, Images: []string{dataURI},
			})
			if err != nil {
				s.failAIMediaGeneration(job, providerSubmissionErrorCode(err), err)
				return
			}
			if err := agnes.ValidatePublicHTTPSImageURL(frame.URL); err != nil {
				s.failAIMediaGeneration(job, string(agnes.CodeInvalidResponse), nil)
				return
			}
			imageURL = frame.URL
		}
		width, height := agnesVideoDimensions(job.OutputSize)
		task, err := client.CreateVideo(ctx, agnes.VideoRequest{
			Model: job.Model, Prompt: job.Prompt, ImageURL: imageURL, Width: width, Height: height,
			NumFrames: videoGenerationFrames(job.DurationSeconds), FrameRate: 24,
		})
		if err != nil {
			s.failAIMediaGeneration(job, providerSubmissionErrorCode(err), err)
			return
		}
		if err := s.db.MarkGenerationJobRemote(job.ID, task.TaskID, task.VideoID); err != nil {
			s.failAIMediaGeneration(job, generationProviderStateUnknown, err)
			return
		}
		job.ExternalTaskID, job.ExternalVideoID = task.TaskID, task.VideoID
		videoID = task.VideoID
	}
	retryableErrors := 0
	for {
		if err := ctx.Err(); err != nil {
			return // keep a remote task recoverable across service shutdown
		}
		task, err := client.GetVideoStatus(ctx, videoID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if retryableAgnesError(err) {
				retryableErrors++
				backoffStep := min(retryableErrors, maxAIMediaRetryableStatusErrors)
				if !waitAIMediaPoll(ctx, s.generationPoll, backoffStep) {
					return // leave running; startup recovery can resume by external_video_id
				}
				continue
			}
			s.failAIMediaGeneration(job, string(agnes.CodeOf(err)), err)
			return
		}
		retryableErrors = 0
		if task.Progress > job.Progress {
			_ = s.db.UpdateGenerationJobProgress(job.ID, task.Progress)
			job.Progress = task.Progress
		}
		if task.Status == agnes.VideoCompleted {
			var asset aiImageAsset
			downloadRetries := 0
			for {
				asset, err = s.downloadGeneratedVideo(ctx, client, task.URL)
				if err == nil {
					break
				}
				if ctx.Err() != nil {
					return // remote task remains recoverable; do not create another video
				}
				if !retryableAgnesError(err) {
					s.failAIMediaGeneration(job, stableAIMediaErrorCode(err), err)
					return
				}
				downloadRetries++
				backoffStep := min(downloadRetries, maxAIMediaRetryableStatusErrors)
				if !waitAIMediaPoll(ctx, s.generationPoll, backoffStep) {
					return
				}
			}
			if err := s.db.MarkGenerationJobSucceeded(job.ID, asset.ID); err != nil {
				s.failAIMediaGeneration(job, generationProviderStateUnknown, err)
			}
			return
		}
		if !waitAIMediaPoll(ctx, s.generationPoll, 0) {
			return
		}
	}
}

func videoGenerationFrames(durationSeconds int) int {
	if durationSeconds == 10 {
		return 241
	}
	return 121
}

func retryableAgnesError(err error) bool {
	var providerError *agnes.Error
	return errors.As(err, &providerError) && providerError.Retryable
}

func providerSubmissionErrorCode(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || retryableAgnesError(err) {
		return generationProviderStateUnknown
	}
	return string(agnes.CodeOf(err))
}

func waitAIMediaPoll(ctx context.Context, base time.Duration, retryNumber int) bool {
	if base <= 0 {
		base = 5 * time.Second
	}
	interval := base
	for index := 0; index < retryNumber && interval < maxAIMediaRetryBackoff; index++ {
		interval *= 2
		if interval > maxAIMediaRetryBackoff {
			interval = maxAIMediaRetryBackoff
		}
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Server) persistGeneratedBytes(data []byte, ext string) (aiImageAsset, error) {
	if len(data) == 0 {
		return aiImageAsset{}, fmt.Errorf("empty generated artifact")
	}
	dir, err := s.ensureAIImageAssetDir()
	if err != nil {
		return aiImageAsset{}, err
	}
	temp, err := os.CreateTemp(dir, ".generated-*"+ext)
	if err != nil {
		return aiImageAsset{}, err
	}
	name := temp.Name()
	defer os.Remove(name)
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
	return s.importAIImageAsset(name)
}

func (s *Server) downloadGeneratedVideo(ctx context.Context, client agnesMediaClient, rawURL string) (aiImageAsset, error) {
	dir, err := s.ensureAIImageAssetDir()
	if err != nil {
		return aiImageAsset{}, err
	}
	temp, err := os.CreateTemp(dir, ".generated-*.mp4")
	if err != nil {
		return aiImageAsset{}, err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return aiImageAsset{}, err
	}
	if _, err := client.DownloadVideo(ctx, rawURL, temp); err != nil {
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
	return s.importAIImageAsset(name)
}

func (s *Server) failAIMediaGeneration(job *db.GenerationJob, code string, cause error) {
	if job == nil {
		return
	}
	if cause != nil && job.Kind == "video" && job.ExternalVideoID != "" && (errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) || retryableAgnesError(cause)) {
		return // keep remote video running so startup recovery can resume polling/download
	}
	if code == "" {
		code = stableAIMediaErrorCode(cause)
	}
	if code == "" {
		code = "GENERATION_FAILED"
	}
	_ = s.db.MarkGenerationJobFailed(job.ID, code)
}

func stableAIMediaErrorCode(err error) string {
	if err == nil {
		return "GENERATION_FAILED"
	}
	if code := agnes.CodeOf(err); code != agnes.CodeInternal {
		return string(code)
	}
	if err == context.Canceled || err == context.DeadlineExceeded {
		return "GENERATION_INTERRUPTED"
	}
	return "GENERATION_ARTIFACT_INVALID"
}

func (s *Server) resumeAIMediaGenerationJobs() {
	jobs, err := s.db.ListRecoverableGenerationJobs(100)
	if err != nil {
		return
	}
	for index := range jobs {
		job := &jobs[index]
		if job.Status == db.GenerationJobRunning && (job.Kind == "image" || job.ExternalVideoID == "") {
			// 标记失败失败时会话就永远停在 running，界面上那个转圈不会再停。
			// 至少让启动日志留下痕迹。
			if markErr := s.db.MarkGenerationJobFailed(job.ID, generationProviderStateUnknown); markErr != nil {
				log.Printf("resume ai media generation: job %d could not be marked failed: %v", job.ID, markErr)
			}
			continue
		}
		s.startAIMediaGeneration(job.ID)
	}
}

func (s *Server) aiMediaGenerationResponse(job *db.GenerationJob) aiMediaGenerationResponse {
	references := make([]aiMediaGenerationReferenceResponse, 0, len(job.References))
	for _, reference := range job.References {
		references = append(references, aiMediaGenerationReferenceResponse{AssetName: reference.AssetName, Role: reference.Role, SHA256: reference.SHA256})
	}
	response := aiMediaGenerationResponse{
		ID: job.ID, RequestID: job.RequestID, Kind: job.Kind, Mode: job.Mode, Provider: job.Provider, Model: job.Model, OutputSize: job.OutputSize, DurationSeconds: job.DurationSeconds, PromptHash: job.PromptHash, References: references,
		Status: job.Status, Progress: job.Progress, ErrorCode: job.ErrorCode, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
	if job.OutputMediaID == nil {
		return response
	}
	item, err := s.db.GetMediaLibraryItem(*job.OutputMediaID)
	if err != nil {
		return response
	}
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	dir, err := cfg.ResolvedAIImageAssetDir()
	if err != nil {
		return response
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return response
	}
	name, ok := aiImageManagedName(dir, item.FilePath)
	if !ok {
		return response
	}
	asset := aiImageAssetFromItem(item.ID, name, item.SHA256, item.Mime, item.Width, item.Height, item.DurationMS, item.SizeBytes, item.CreatedAt)
	response.OutputAsset = &asset
	return response
}

func writeAIMediaError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error_code": code})
}
