package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// StudioScript 是本地创作脚本，不关联任何远程模型或知识库。
type StudioScript struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	ContentType string `json:"content_type"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func normalizeStudioContentType(contentType string) (string, error) {
	contentType = strings.TrimSpace(contentType)
	if contentType != "video" && contentType != "graph_text" && contentType != "article" {
		return "", fmt.Errorf("script content type must be video, graph_text, or article")
	}
	return contentType, nil
}

func normalizeStudioScript(title, body string) (string, string, error) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" || len([]rune(title)) > 120 {
		return "", "", fmt.Errorf("script title must be 1-120 characters")
	}
	if body == "" || len([]rune(body)) > 32<<10 {
		return "", "", fmt.Errorf("script body must be 1-32768 characters")
	}
	return title, body, nil
}

func (db *DB) CreateStudioScript(title, body string) (*StudioScript, error) {
	return db.CreateStudioScriptForType(title, body, "video")
}

func (db *DB) CreateStudioScriptForType(title, body, contentType string) (*StudioScript, error) {
	title, body, err := normalizeStudioScript(title, body)
	if err != nil {
		return nil, err
	}
	contentType, err = normalizeStudioContentType(contentType)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`INSERT INTO studio_scripts (title, body, content_type, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, title, body, contentType, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return &StudioScript{ID: id, Title: title, Body: body, ContentType: contentType, CreatedAt: now, UpdatedAt: now}, nil
}

func (db *DB) GetStudioScript(id int64) (*StudioScript, error) {
	var script StudioScript
	err := db.QueryRow(`SELECT id, title, body, content_type, created_at, updated_at FROM studio_scripts WHERE id=?`, id).
		Scan(&script.ID, &script.Title, &script.Body, &script.ContentType, &script.CreatedAt, &script.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("script %d not found", id)
	}
	if err != nil {
		return nil, err
	}
	return &script, nil
}

func (db *DB) ListStudioScripts(limit int) ([]StudioScript, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := db.Query(`SELECT id, title, body, content_type, created_at, updated_at FROM studio_scripts ORDER BY updated_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scripts []StudioScript
	for rows.Next() {
		var script StudioScript
		if err := rows.Scan(&script.ID, &script.Title, &script.Body, &script.ContentType, &script.CreatedAt, &script.UpdatedAt); err != nil {
			return nil, err
		}
		scripts = append(scripts, script)
	}
	return scripts, rows.Err()
}

func (db *DB) ReplaceStudioScript(id int64, title, body string) error {
	current, err := db.GetStudioScript(id)
	if err != nil {
		return err
	}
	return db.ReplaceStudioScriptForType(id, title, body, current.ContentType)
}

func (db *DB) ReplaceStudioScriptForType(id int64, title, body, contentType string) error {
	title, body, err := normalizeStudioScript(title, body)
	if err != nil {
		return err
	}
	contentType, err = normalizeStudioContentType(contentType)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE studio_scripts SET title=?, body=?, content_type=?, updated_at=? WHERE id=?`, title, body, contentType, now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("script %d not found", id)
	}
	return nil
}

func (db *DB) DeleteStudioScript(id int64) error {
	if id <= 0 {
		return fmt.Errorf("script id must be positive")
	}
	var references int
	if err := db.QueryRow(`SELECT COUNT(*) FROM clip_jobs WHERE script_id=?`, id).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return fmt.Errorf("script %d has clip jobs and cannot be deleted", id)
	}
	result, err := db.Exec(`DELETE FROM studio_scripts WHERE id=?`, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("script %d not found", id)
	}
	return nil
}

func (db *DB) GetMediaLibraryItem(id int64) (*MediaLibraryItem, error) {
	var item MediaLibraryItem
	err := db.QueryRow(`SELECT id, file_path, COALESCE(sha256,''), COALESCE(size_bytes,0), COALESCE(mime,''), COALESCE(width,0), COALESCE(height,0), COALESCE(duration_ms,0), COALESCE(probe_json,''), COALESCE(purpose,'shared'), COALESCE(created_at,'') FROM media_library WHERE id=?`, id).
		Scan(&item.ID, &item.FilePath, &item.SHA256, &item.SizeBytes, &item.Mime, &item.Width, &item.Height, &item.DurationMS, &item.ProbeJSON, &item.Purpose, &item.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("media library item %d not found", id)
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ClipJob 保存一次 Agent 的剪辑方案和本地实际产物，不会自动执行。
type ClipJob struct {
	ID            int64  `json:"id"`
	SourceMediaID int64  `json:"source_media_id"`
	ScriptID      int64  `json:"script_id"`
	SourcePath    string `json:"source_path"`
	OutputPath    string `json:"output_path"`
	StartMS       int    `json:"start_ms"`
	EndMS         int    `json:"end_ms"`
	PlanJSON      string `json:"plan_json"`
	Status        string `json:"status"`
	ErrorCode     string `json:"error_code"`
	ErrorMessage  string `json:"error_message"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	CompletedAt   string `json:"completed_at"`
}

func (db *DB) CreateClipJob(sourceMediaID, scriptID int64, sourcePath string, startMS, endMS int, planJSON string) (*ClipJob, error) {
	if sourceMediaID <= 0 || strings.TrimSpace(sourcePath) == "" || startMS < 0 || endMS <= startMS || strings.TrimSpace(planJSON) == "" {
		return nil, fmt.Errorf("invalid clip job")
	}
	if _, err := db.GetMediaLibraryItem(sourceMediaID); err != nil {
		return nil, err
	}
	if scriptID > 0 {
		script, err := db.GetStudioScript(scriptID)
		if err != nil {
			return nil, err
		}
		if script.ContentType != "video" {
			return nil, fmt.Errorf("clip jobs require a video script")
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`INSERT INTO clip_jobs (source_media_id, script_id, source_path, output_path, start_ms, end_ms, plan_json, status, error_code, error_message, created_at, updated_at, completed_at) VALUES (?, NULLIF(?, 0), ?, '', ?, ?, ?, 'planned', '', '', ?, ?, '')`,
		sourceMediaID, scriptID, sourcePath, startMS, endMS, planJSON, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return &ClipJob{ID: id, SourceMediaID: sourceMediaID, ScriptID: scriptID, SourcePath: sourcePath, StartMS: startMS, EndMS: endMS, PlanJSON: planJSON, Status: "planned", CreatedAt: now, UpdatedAt: now}, nil
}

func scanClipJob(scanner interface{ Scan(dest ...any) error }) (*ClipJob, error) {
	var job ClipJob
	var scriptID sql.NullInt64
	err := scanner.Scan(&job.ID, &job.SourceMediaID, &scriptID, &job.SourcePath, &job.OutputPath, &job.StartMS, &job.EndMS, &job.PlanJSON, &job.Status, &job.ErrorCode, &job.ErrorMessage, &job.CreatedAt, &job.UpdatedAt, &job.CompletedAt)
	if err != nil {
		return nil, err
	}
	if scriptID.Valid {
		job.ScriptID = scriptID.Int64
	}
	return &job, nil
}

const clipJobColumns = `id, source_media_id, script_id, source_path, COALESCE(output_path,''), start_ms, end_ms, plan_json, status, COALESCE(error_code,''), COALESCE(error_message,''), created_at, updated_at, COALESCE(completed_at,'')`

func (db *DB) GetClipJob(id int64) (*ClipJob, error) {
	job, err := scanClipJob(db.QueryRow(`SELECT `+clipJobColumns+` FROM clip_jobs WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("clip job %d not found", id)
	}
	return job, err
}

func (db *DB) ListClipJobs(limit int) ([]ClipJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := db.Query(`SELECT `+clipJobColumns+` FROM clip_jobs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []ClipJob
	for rows.Next() {
		job, err := scanClipJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	return jobs, rows.Err()
}

func (db *DB) FinishClipJob(id int64, status, outputPath, errorCode, errorMessage string) error {
	if status != "completed" && status != "failed" {
		return fmt.Errorf("invalid clip job status")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE clip_jobs SET status=?, output_path=?, error_code=?, error_message=?, completed_at=?, updated_at=? WHERE id=?`, status, outputPath, errorCode, errorMessage, now, now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("clip job %d not found", id)
	}
	return nil
}
