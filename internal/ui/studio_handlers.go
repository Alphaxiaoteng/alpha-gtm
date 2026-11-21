package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/media"
	"alpha-nexus-egolite/internal/studio"
	"alpha-nexus-egolite/internal/trace"
)

const maxStudioRequestBytes = 96 << 10

func decodeStudioJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxStudioRequestBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("invalid request body")
	}
	return nil
}

// handleAPIMediaLibrary 只管理用户明确导入的本地素材索引，不复制原文件。
func (s *Server) handleAPIMediaLibrary(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, err := s.db.ListMediaLibrary(limit)
		if err != nil {
			http.Error(w, "list media library failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, items)
	case http.MethodPost:
		var body struct {
			Path    string `json:"path"`
			Purpose string `json:"purpose"`
		}
		if err := decodeStudioJSON(w, r, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		path, err := media.ValidateLocalPath(body.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		meta, err := media.Probe(path)
		if err != nil {
			http.Error(w, "probe media failed", http.StatusBadRequest)
			return
		}
		item, err := s.db.UpsertMediaLibrary(meta.Path, meta.SHA256, meta.SizeBytes, meta.MimeGuess, meta.Width, meta.Height, meta.DurationMS, meta.ProbeJSON)
		if err != nil {
			http.Error(w, "index media failed", http.StatusInternalServerError)
			return
		}
		if strings.TrimSpace(body.Purpose) != "" {
			if err := s.db.SetMediaLibraryPurpose(item.ID, body.Purpose); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			item.Purpose = body.Purpose
		}
		_, _ = s.db.CreateAuditEvent(string(trace.New()), "info", "studio", "media imported", "media_library", item.ID, "")
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAPIMediaLibraryItem 只删除素材索引；被内容或剪辑方案引用的素材必须保留。
func (s *Server) handleAPIMediaLibraryItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "media library id required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var body struct {
			Purpose string `json:"purpose"`
		}
		if err := decodeStudioJSON(w, r, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.db.SetMediaLibraryPurpose(id, body.Purpose); err != nil {
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, "media library item not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		item, _ := s.db.GetMediaLibraryItem(id)
		writeJSON(w, item)
	case http.MethodDelete:
		if err := s.db.DeleteMediaLibrary(id); err != nil {
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, "media library item not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		_, _ = s.db.CreateAuditEvent(string(trace.New()), "info", "studio", "media index removed", "media_library", id, "")
		writeJSON(w, map[string]bool{"deleted": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIScripts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		scripts, err := s.db.ListStudioScripts(200)
		if err != nil {
			http.Error(w, "list scripts failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, scripts)
	case http.MethodPost:
		var body struct {
			Title       string `json:"title"`
			Body        string `json:"body"`
			ContentType string `json:"content_type"`
		}
		if err := decodeStudioJSON(w, r, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.ContentType == "" {
			body.ContentType = "video"
		}
		script, err := s.db.CreateStudioScriptForType(body.Title, body.Body, body.ContentType)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = s.db.CreateAuditEvent(string(trace.New()), "info", "studio", "script created", "studio_scripts", script.ID, "")
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, script)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIScript(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "script id required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		script, err := s.db.GetStudioScript(id)
		if err != nil {
			http.Error(w, "script not found", http.StatusNotFound)
			return
		}
		writeJSON(w, script)
	case http.MethodPut:
		var body struct {
			Title       string `json:"title"`
			Body        string `json:"body"`
			ContentType string `json:"content_type"`
		}
		if err := decodeStudioJSON(w, r, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.ContentType == "" {
			body.ContentType = "video"
		}
		if err := s.db.ReplaceStudioScriptForType(id, body.Title, body.Body, body.ContentType); err != nil {
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, "script not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		script, _ := s.db.GetStudioScript(id)
		writeJSON(w, script)
	case http.MethodDelete:
		if err := s.db.DeleteStudioScript(id); err != nil {
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, "script not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, map[string]bool{"deleted": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIClipJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.db.ListClipJobs(200)
	if err != nil {
		http.Error(w, "list clip jobs failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, jobs)
}

func (s *Server) handleAPIClipPlan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourceMediaID    int64 `json:"sourceMediaId"`
		ScriptID         int64 `json:"scriptId"`
		TargetDurationMS int   `json:"targetDurationMs"`
	}
	if err := decodeStudioJSON(w, r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mediaItem, err := s.db.GetMediaLibraryItem(body.SourceMediaID)
	if err != nil {
		http.Error(w, "source media not found", http.StatusNotFound)
		return
	}
	script, err := s.db.GetStudioScript(body.ScriptID)
	if err != nil {
		http.Error(w, "script not found", http.StatusNotFound)
		return
	}
	if script.ContentType != "video" {
		http.Error(w, "clip plans require a video script", http.StatusBadRequest)
		return
	}
	if s.studioAgent == nil {
		http.Error(w, "DOUBAO_API_NOT_CONFIGURED", http.StatusServiceUnavailable)
		return
	}
	plan, err := s.studioAgent.Plan(r.Context(), *mediaItem, *script, body.TargetDurationMS)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "DOUBAO_API_NOT_CONFIGURED") || strings.Contains(err.Error(), "DOUBAO_API_UNAVAILABLE") {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		http.Error(w, "encode clip plan failed", http.StatusInternalServerError)
		return
	}
	job, err := s.db.CreateClipJob(mediaItem.ID, script.ID, mediaItem.FilePath, plan.StartMS, plan.EndMS, string(planJSON))
	if err != nil {
		http.Error(w, "save clip plan failed", http.StatusInternalServerError)
		return
	}
	_, _ = s.db.CreateAuditEvent(string(trace.New()), "info", "studio", "clip planned by Doubao Agent", "clip_jobs", job.ID, "")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, job)
}

func (s *Server) handleAPIClipExecute(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "clip job id required", http.StatusBadRequest)
		return
	}
	job, err := s.db.GetClipJob(id)
	if err != nil {
		http.Error(w, "clip job not found", http.StatusNotFound)
		return
	}
	if job.Status != "planned" && job.Status != "failed" {
		http.Error(w, "clip job is already complete", http.StatusConflict)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, "resolve data directory failed", http.StatusInternalServerError)
		return
	}
	outputPath, err := studio.OutputPath(dataDir, job.ID)
	if err != nil {
		http.Error(w, "prepare output path failed", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	if err := studio.ExecuteClip(ctx, job.SourcePath, outputPath, job.StartMS, job.EndMS); err != nil {
		_ = s.db.FinishClipJob(job.ID, "failed", "", studioErrorCode(err), err.Error())
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	meta, err := media.Probe(outputPath)
	if err != nil {
		_ = s.db.FinishClipJob(job.ID, "failed", "", "CLIP_OUTPUT_PROBE_FAILED", err.Error())
		http.Error(w, "probe clip output failed", http.StatusInternalServerError)
		return
	}
	if _, err := s.db.UpsertMediaLibrary(meta.Path, meta.SHA256, meta.SizeBytes, meta.MimeGuess, meta.Width, meta.Height, meta.DurationMS, meta.ProbeJSON); err != nil {
		_ = s.db.FinishClipJob(job.ID, "failed", "", "CLIP_OUTPUT_INDEX_FAILED", err.Error())
		http.Error(w, "index clip output failed", http.StatusInternalServerError)
		return
	}
	if err := s.db.FinishClipJob(job.ID, "completed", outputPath, "", ""); err != nil {
		http.Error(w, "save clip result failed", http.StatusInternalServerError)
		return
	}
	job, _ = s.db.GetClipJob(job.ID)
	_, _ = s.db.CreateAuditEvent(string(trace.New()), "info", "studio", "clip executed locally", "clip_jobs", job.ID, "")
	writeJSON(w, job)
}

func studioErrorCode(err error) string {
	message := err.Error()
	for _, code := range []string{"FFMPEG_NOT_FOUND", "FFMPEG_CAPABILITY_UNAVAILABLE", "FFMPEG_MP4_MUXER_UNAVAILABLE", "FFMPEG_CLIP_FAILED", "CLIP_SOURCE_MUST_BE_VIDEO", "DOUBAO_API_NOT_CONFIGURED", "DOUBAO_API_UNAVAILABLE", "DOUBAO_API_INVALID_PLAN"} {
		if strings.Contains(message, code) {
			return code
		}
	}
	return "CLIP_EXECUTION_FAILED"
}
