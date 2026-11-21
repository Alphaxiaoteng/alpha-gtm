package db

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	GenerationJobQueued    = "queued"
	GenerationJobRunning   = "running"
	GenerationJobSucceeded = "succeeded"
	GenerationJobFailed    = "failed"
)

var ErrGenerationRequestConflict = errors.New("generation request id conflicts with different input")

const (
	GenerationModeTextToImage  = "text_to_image"
	GenerationModeImageToImage = "image_to_image"
	GenerationModeTextToVideo  = "text_to_video"
	GenerationModeImageToVideo = "image_to_video"
)

// GenerationReference is the immutable, non-secret snapshot of one managed
// local image used by a generation job. Local file paths and provider handoff
// URLs are deliberately excluded; execution resolves MediaID again and
// verifies SHA256.
type GenerationReference struct {
	MediaID   int64  `json:"media_id"`
	AssetName string `json:"asset_name"`
	Role      string `json:"role"`
	SHA256    string `json:"sha256"`
}

// GenerationJob is durable local state for one explicit image or video
// generation request. Prompt is persisted for crash recovery but deliberately
// omitted from JSON so list/status APIs cannot expose it accidentally.
type GenerationJob struct {
	ID              int64                 `json:"id"`
	RequestID       string                `json:"request_id,omitempty"`
	RequestHash     string                `json:"-"`
	Kind            string                `json:"kind"`
	Mode            string                `json:"mode"`
	Provider        string                `json:"provider"`
	Model           string                `json:"model"`
	OutputSize      string                `json:"output_size"`
	DurationSeconds int                   `json:"duration_seconds"`
	Prompt          string                `json:"-"`
	PromptHash      string                `json:"prompt_hash"`
	References      []GenerationReference `json:"references"`
	Status          string                `json:"status"`
	ExternalTaskID  string                `json:"external_task_id,omitempty"`
	ExternalVideoID string                `json:"external_video_id,omitempty"`
	OutputMediaID   *int64                `json:"output_media_id"`
	Progress        int                   `json:"progress"`
	ErrorCode       string                `json:"error_code,omitempty"`
	CreatedAt       string                `json:"created_at"`
	UpdatedAt       string                `json:"updated_at"`
	StartedAt       string                `json:"started_at,omitempty"`
	CompletedAt     string                `json:"completed_at,omitempty"`
}

func defaultGenerationMode(kind string) string {
	if strings.TrimSpace(kind) == "video" {
		return GenerationModeTextToVideo
	}
	return GenerationModeTextToImage
}

func normalizeGenerationInput(kind, provider, model, prompt string) (string, string, string, string, error) {
	kind = strings.TrimSpace(kind)
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	prompt = strings.TrimSpace(prompt)
	if kind != "image" && kind != "video" {
		return "", "", "", "", fmt.Errorf("generation kind must be image or video")
	}
	if provider == "" || len([]rune(provider)) > 100 {
		return "", "", "", "", fmt.Errorf("generation provider must be 1-100 characters")
	}
	if model == "" || len([]rune(model)) > 160 {
		return "", "", "", "", fmt.Errorf("generation model must be 1-160 characters")
	}
	if prompt == "" || len(prompt) > 64<<10 || !utf8.ValidString(prompt) {
		return "", "", "", "", fmt.Errorf("generation prompt must be valid UTF-8 up to 65536 bytes")
	}
	return kind, provider, model, prompt, nil
}

func (db *DB) CreateGenerationJob(kind, provider, model, prompt string) (*GenerationJob, error) {
	return db.CreateGenerationJobWithReferences(kind, defaultGenerationMode(kind), provider, model, prompt, nil)
}

func (db *DB) CreateGenerationJobWithReferences(kind, mode, provider, model, prompt string, references []GenerationReference) (*GenerationJob, error) {
	job, _, err := db.createGenerationJob("", kind, mode, provider, model, defaultGenerationOutputSize(kind), 0, prompt, references)
	return job, err
}

// CreateGenerationJobIdempotent atomically binds one caller-generated request
// ID to a canonical request snapshot. A replay with the same snapshot returns
// the original job; reusing the ID for different input fails closed.
func (db *DB) CreateGenerationJobIdempotent(requestID, kind, mode, provider, model, prompt string, references []GenerationReference) (*GenerationJob, bool, error) {
	return db.createGenerationJob(requestID, kind, mode, provider, model, defaultGenerationOutputSize(kind), 0, prompt, references)
}

func (db *DB) CreateGenerationJobIdempotentWithOutput(requestID, kind, mode, provider, model, outputSize, prompt string, references []GenerationReference) (*GenerationJob, bool, error) {
	return db.createGenerationJob(requestID, kind, mode, provider, model, outputSize, 0, prompt, references)
}

func (db *DB) CreateGenerationJobIdempotentWithOutputAndDuration(requestID, kind, mode, provider, model, outputSize string, durationSeconds int, prompt string, references []GenerationReference) (*GenerationJob, bool, error) {
	return db.createGenerationJob(requestID, kind, mode, provider, model, outputSize, durationSeconds, prompt, references)
}

func (db *DB) createGenerationJob(requestID, kind, mode, provider, model, outputSize string, durationSeconds int, prompt string, references []GenerationReference) (*GenerationJob, bool, error) {
	kind, provider, model, prompt, err := normalizeGenerationInput(kind, provider, model, prompt)
	if err != nil {
		return nil, false, err
	}
	outputSize, err = normalizeGenerationOutputSize(kind, outputSize)
	if err != nil {
		return nil, false, err
	}
	durationSeconds, err = normalizeGenerationDuration(kind, durationSeconds)
	if err != nil {
		return nil, false, err
	}
	mode = strings.TrimSpace(mode)
	references, err = db.normalizeGenerationReferences(kind, mode, references)
	if err != nil {
		return nil, false, err
	}
	referencesJSON, err := json.Marshal(references)
	if err != nil {
		return nil, false, fmt.Errorf("encode generation references: %w", err)
	}
	promptHash := fmt.Sprintf("%x", sha256.Sum256([]byte(prompt)))
	requestHash := ""
	if requestID != "" {
		requestID, err = normalizeGenerationRequestID(requestID)
		if err != nil {
			return nil, false, err
		}
		requestHash, err = hashGenerationRequest(kind, mode, provider, model, outputSize, durationSeconds, promptHash, referencesJSON)
		if err != nil {
			return nil, false, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`INSERT INTO generation_jobs (
		request_id, request_hash, kind, mode, provider, model, output_size, duration_seconds, prompt, prompt_hash, references_json, status,
		external_task_id, external_video_id, output_media_id, progress, error_code,
		created_at, updated_at, started_at, completed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'queued', '', '', NULL, 0, '', ?, ?, NULL, NULL)`,
		requestID, requestHash, kind, mode, provider, model, outputSize, durationSeconds, prompt, promptHash, string(referencesJSON), now, now)
	if err != nil {
		if requestID == "" {
			return nil, false, err
		}
		existing, lookupErr := db.getGenerationJobByRequestID(requestID)
		if lookupErr != nil {
			return nil, false, err
		}
		if existing.RequestHash != requestHash {
			return nil, false, ErrGenerationRequestConflict
		}
		return existing, false, nil
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, false, err
	}
	return &GenerationJob{
		ID: id, RequestID: requestID, RequestHash: requestHash,
		Kind: kind, Mode: mode, Provider: provider, Model: model, OutputSize: outputSize, DurationSeconds: durationSeconds, Prompt: prompt,
		PromptHash: promptHash, References: references, Status: GenerationJobQueued, CreatedAt: now, UpdatedAt: now,
	}, true, nil
}

func normalizeGenerationRequestID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 16 || len(value) > 128 {
		return "", fmt.Errorf("generation request id must be 16-128 characters")
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return "", fmt.Errorf("generation request id contains invalid characters")
	}
	return value, nil
}

func hashGenerationRequest(kind, mode, provider, model, outputSize string, durationSeconds int, promptHash string, referencesJSON []byte) (string, error) {
	payload := struct {
		Kind            string          `json:"kind"`
		Mode            string          `json:"mode"`
		Provider        string          `json:"provider"`
		Model           string          `json:"model"`
		OutputSize      string          `json:"output_size"`
		DurationSeconds int             `json:"duration_seconds"`
		PromptHash      string          `json:"prompt_hash"`
		References      json.RawMessage `json:"references"`
	}{kind, mode, provider, model, outputSize, durationSeconds, promptHash, referencesJSON}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode generation request snapshot: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (db *DB) normalizeGenerationReferences(kind, mode string, references []GenerationReference) ([]GenerationReference, error) {
	refs := append([]GenerationReference(nil), references...)
	switch mode {
	case GenerationModeTextToImage:
		if kind != "image" || len(refs) != 0 {
			return nil, fmt.Errorf("text-to-image requires image kind and no references")
		}
	case GenerationModeImageToImage:
		if kind != "image" || len(refs) == 0 || len(refs) > 8 {
			return nil, fmt.Errorf("image-to-image requires one to eight image references")
		}
	case GenerationModeTextToVideo:
		if kind != "video" || len(refs) != 0 {
			return nil, fmt.Errorf("text-to-video requires video kind and no references")
		}
	case GenerationModeImageToVideo:
		if kind != "video" || len(refs) != 1 || refs[0].Role != "first_frame" {
			return nil, fmt.Errorf("image-to-video requires exactly one first-frame reference")
		}
	default:
		return nil, fmt.Errorf("unsupported generation mode")
	}
	seen := make(map[int64]struct{}, len(refs))
	for index := range refs {
		ref := &refs[index]
		ref.AssetName = strings.TrimSpace(ref.AssetName)
		ref.Role = strings.TrimSpace(ref.Role)
		ref.SHA256 = strings.ToLower(strings.TrimSpace(ref.SHA256))
		if ref.MediaID <= 0 || filepath.Base(ref.AssetName) != ref.AssetName || strings.TrimSuffix(ref.AssetName, strings.ToLower(filepath.Ext(ref.AssetName))) != ref.SHA256 {
			return nil, fmt.Errorf("invalid generation reference identity")
		}
		if _, err := hex.DecodeString(ref.SHA256); err != nil || len(ref.SHA256) != 64 {
			return nil, fmt.Errorf("invalid generation reference SHA256")
		}
		switch strings.ToLower(filepath.Ext(ref.AssetName)) {
		case ".png", ".jpg", ".gif":
		default:
			return nil, fmt.Errorf("unsupported generation reference image type")
		}
		if mode == GenerationModeImageToImage && ref.Role != "product" && ref.Role != "model" && ref.Role != "style" {
			return nil, fmt.Errorf("unsupported image reference role")
		}
		if _, exists := seen[ref.MediaID]; exists {
			return nil, fmt.Errorf("duplicate generation reference")
		}
		seen[ref.MediaID] = struct{}{}
		item, err := db.GetMediaLibraryItem(ref.MediaID)
		if err != nil || item.SHA256 != ref.SHA256 || !strings.HasPrefix(item.Mime, "image/") {
			return nil, fmt.Errorf("generation reference does not match media library")
		}
	}
	if refs == nil {
		refs = []GenerationReference{}
	}
	return refs, nil
}

func defaultGenerationOutputSize(kind string) string {
	if strings.TrimSpace(kind) == "video" {
		return "3:2"
	}
	return "1:1"
}

func normalizeGenerationOutputSize(kind, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultGenerationOutputSize(kind)
	}
	valid := map[string]bool{"1:1": true, "3:4": true, "4:3": true, "16:9": true, "9:16": true, "2:3": true, "3:2": true, "21:9": true}
	if !valid[value] || (kind == "video" && value == "21:9") {
		return "", fmt.Errorf("unsupported generation output size")
	}
	return value, nil
}

func normalizeGenerationDuration(kind string, durationSeconds int) (int, error) {
	if kind == "image" {
		if durationSeconds != 0 {
			return 0, fmt.Errorf("duration seconds is only supported for video generation")
		}
		return 0, nil
	}
	if durationSeconds == 0 {
		return 5, nil
	}
	if durationSeconds != 5 && durationSeconds != 10 {
		return 0, fmt.Errorf("video generation duration must be 5 or 10 seconds")
	}
	return durationSeconds, nil
}

const generationJobColumns = `id, COALESCE(request_id,''), COALESCE(request_hash,''), kind, mode, provider, model, COALESCE(output_size,''), COALESCE(duration_seconds,0), prompt, prompt_hash, references_json, status,
	COALESCE(external_task_id,''), COALESCE(external_video_id,''), output_media_id,
	progress, COALESCE(error_code,''), created_at, updated_at,
	COALESCE(started_at,''), COALESCE(completed_at,'')`

func scanGenerationJob(scanner interface{ Scan(dest ...any) error }) (*GenerationJob, error) {
	var job GenerationJob
	var outputMediaID sql.NullInt64
	var referencesJSON string
	err := scanner.Scan(
		&job.ID, &job.RequestID, &job.RequestHash, &job.Kind, &job.Mode, &job.Provider, &job.Model, &job.OutputSize, &job.DurationSeconds, &job.Prompt, &job.PromptHash, &referencesJSON,
		&job.Status, &job.ExternalTaskID, &job.ExternalVideoID, &outputMediaID,
		&job.Progress, &job.ErrorCode, &job.CreatedAt, &job.UpdatedAt,
		&job.StartedAt, &job.CompletedAt,
	)
	if err != nil {
		return nil, err
	}
	references, err := decodeGenerationReferences(referencesJSON)
	if err != nil {
		return nil, err
	}
	job.References = references
	if outputMediaID.Valid {
		id := outputMediaID.Int64
		job.OutputMediaID = &id
	}
	return &job, nil
}

func (db *DB) getGenerationJobByRequestID(requestID string) (*GenerationJob, error) {
	job, err := scanGenerationJob(db.QueryRow(`SELECT `+generationJobColumns+` FROM generation_jobs WHERE request_id=?`, requestID))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("generation request not found")
	}
	return job, err
}

func decodeGenerationReferences(raw string) ([]GenerationReference, error) {
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	var references []GenerationReference
	if err := decoder.Decode(&references); err != nil {
		return nil, fmt.Errorf("decode generation references: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode generation references: trailing data")
	}
	if references == nil {
		references = []GenerationReference{}
	}
	return references, nil
}

func (db *DB) GetMediaLibraryItemBySHA256(hash string) (*MediaLibraryItem, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if _, err := hex.DecodeString(hash); err != nil || len(hash) != 64 {
		return nil, fmt.Errorf("invalid media SHA256")
	}
	var item MediaLibraryItem
	err := db.QueryRow(`SELECT id, file_path, COALESCE(sha256,''), COALESCE(size_bytes,0), COALESCE(mime,''), COALESCE(width,0), COALESCE(height,0), COALESCE(duration_ms,0), COALESCE(probe_json,''), COALESCE(purpose,'shared'), COALESCE(created_at,'') FROM media_library WHERE sha256=?`, hash).
		Scan(&item.ID, &item.FilePath, &item.SHA256, &item.SizeBytes, &item.Mime, &item.Width, &item.Height, &item.DurationMS, &item.ProbeJSON, &item.Purpose, &item.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("media library item not found")
	}
	return &item, err
}

func (db *DB) GetGenerationJob(id int64) (*GenerationJob, error) {
	if id <= 0 {
		return nil, fmt.Errorf("generation job id must be positive")
	}
	job, err := scanGenerationJob(db.QueryRow(`SELECT `+generationJobColumns+` FROM generation_jobs WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("generation job %d not found", id)
	}
	return job, err
}

func normalizeGenerationListLimit(limit, defaultLimit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func (db *DB) ListRecoverableGenerationJobs(limit int) ([]GenerationJob, error) {
	limit = normalizeGenerationListLimit(limit, 100)
	return db.listGenerationJobs(`WHERE status IN ('queued','running') ORDER BY created_at, id LIMIT ?`, limit)
}

func (db *DB) ListRecentGenerationJobs(limit int) ([]GenerationJob, error) {
	limit = normalizeGenerationListLimit(limit, 50)
	return db.listGenerationJobs(`ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
}

func (db *DB) listGenerationJobs(suffix string, limit int) ([]GenerationJob, error) {
	rows, err := db.Query(`SELECT `+generationJobColumns+` FROM generation_jobs `+suffix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]GenerationJob, 0)
	for rows.Next() {
		job, err := scanGenerationJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	return jobs, rows.Err()
}

func (db *DB) MarkGenerationJobRunning(id int64) error {
	if id <= 0 {
		return fmt.Errorf("generation job id must be positive")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`UPDATE generation_jobs
		SET status='running', started_at=?, updated_at=?
		WHERE id=? AND status='queued'`, now, now, id)
	return db.finishGenerationTransition(result, err, id, "mark running")
}

func normalizeGenerationExternalID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > 200 {
		return "", fmt.Errorf("generation external id is too long")
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return "", fmt.Errorf("generation external id has invalid characters")
	}
	return value, nil
}

func (db *DB) MarkGenerationJobRemote(id int64, externalTaskID, externalVideoID string) error {
	if id <= 0 {
		return fmt.Errorf("generation job id must be positive")
	}
	taskID, err := normalizeGenerationExternalID(externalTaskID)
	if err != nil {
		return err
	}
	videoID, err := normalizeGenerationExternalID(externalVideoID)
	if err != nil {
		return err
	}
	if taskID == "" && videoID == "" {
		return fmt.Errorf("generation remote task or video id is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`UPDATE generation_jobs SET
		external_task_id=CASE WHEN ?<>'' THEN ? ELSE external_task_id END,
		external_video_id=CASE WHEN ?<>'' THEN ? ELSE external_video_id END,
		updated_at=?
		WHERE id=? AND status='running'`, taskID, taskID, videoID, videoID, now, id)
	return db.finishGenerationTransition(result, err, id, "record remote id")
}

func (db *DB) UpdateGenerationJobProgress(id int64, progress int) error {
	if id <= 0 {
		return fmt.Errorf("generation job id must be positive")
	}
	if progress < 0 || progress > 100 {
		return fmt.Errorf("generation progress must be between 0 and 100")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`UPDATE generation_jobs SET progress=?, updated_at=?
		WHERE id=? AND status='running' AND progress<=?`, progress, now, id, progress)
	return db.finishGenerationTransition(result, err, id, "update progress")
}

func (db *DB) MarkGenerationJobSucceeded(id, outputMediaID int64) error {
	if id <= 0 || outputMediaID <= 0 {
		return fmt.Errorf("generation job and output media ids must be positive")
	}
	if _, err := db.GetMediaLibraryItem(outputMediaID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`UPDATE generation_jobs SET
		status='succeeded', output_media_id=?, progress=100, error_code='',
		completed_at=?, updated_at=?
		WHERE id=? AND status='running'`, outputMediaID, now, now, id)
	return db.finishGenerationTransition(result, err, id, "mark succeeded")
}

func normalizeGenerationErrorCode(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 100 {
		return "", fmt.Errorf("generation error code must be 1-100 characters")
	}
	for _, char := range value {
		if (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' {
			continue
		}
		return "", fmt.Errorf("generation error code must be a stable uppercase code")
	}
	return value, nil
}

func (db *DB) MarkGenerationJobFailed(id int64, errorCode string) error {
	if id <= 0 {
		return fmt.Errorf("generation job id must be positive")
	}
	errorCode, err := normalizeGenerationErrorCode(errorCode)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`UPDATE generation_jobs SET
		status='failed', error_code=?, completed_at=?, updated_at=?
		WHERE id=? AND status IN ('queued','running')`, errorCode, now, now, id)
	return db.finishGenerationTransition(result, err, id, "mark failed")
}

func (db *DB) finishGenerationTransition(result sql.Result, err error, id int64, action string) error {
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		return nil
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM generation_jobs WHERE id=?`, id).Scan(&status); err == sql.ErrNoRows {
		return fmt.Errorf("generation job %d not found", id)
	} else if err != nil {
		return err
	}
	return fmt.Errorf("generation job %d cannot %s from status %s", id, action, status)
}
