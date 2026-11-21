package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/media"
	"alpha-nexus-egolite/internal/studio"
	"alpha-nexus-egolite/internal/trace"
)

var newDoubaoStudioAgent = studio.NewDoubaoAgent

func handleStudioMediaList(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	items, err := database.ListMediaLibrary(argInt(args, "limit", 200))
	if err != nil {
		return nil, err
	}
	return okResult("local media indexes listed", map[string]interface{}{"count": len(items), "items": items}), nil
}

func handleStudioScriptCreate(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	contentType := argString(args, "content_type", "video")
	script, err := database.CreateStudioScriptForType(argString(args, "title", ""), argString(args, "body", ""), contentType)
	if err != nil {
		return &Result{OK: false, Error: "STUDIO_SCRIPT_INVALID"}, err
	}
	_, _ = database.CreateAuditEvent(string(trace.New()), "info", "studio", "script created by tool", "studio_scripts", script.ID, "")
	return okResult("local script created", map[string]interface{}{"script": script}), nil
}

func handleStudioScriptList(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	scripts, err := database.ListStudioScripts(argInt(args, "limit", 200))
	if err != nil {
		return nil, err
	}
	return okResult("local scripts listed", map[string]interface{}{"count": len(scripts), "scripts": scripts}), nil
}

func handleStudioClipPlan(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	sourceMediaID, scriptID := int64(argInt(args, "source_media_id", 0)), int64(argInt(args, "script_id", 0))
	if sourceMediaID <= 0 || scriptID <= 0 {
		return &Result{OK: false, Error: "MISSING_PARAM"}, fmt.Errorf("source_media_id and script_id are required")
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	mediaItem, err := database.GetMediaLibraryItem(sourceMediaID)
	if err != nil {
		return &Result{OK: false, Error: "STUDIO_MEDIA_NOT_FOUND"}, err
	}
	script, err := database.GetStudioScript(scriptID)
	if err != nil {
		return &Result{OK: false, Error: "STUDIO_SCRIPT_NOT_FOUND"}, err
	}
	if script.ContentType != "video" {
		return &Result{OK: false, Error: "STUDIO_SCRIPT_TYPE_INVALID"}, fmt.Errorf("clip plans require a video script")
	}
	plan, err := newDoubaoStudioAgent().Plan(ctx, *mediaItem, *script, argInt(args, "target_duration_ms", 0))
	if err != nil {
		return &Result{OK: false, Error: studioToolErrorCode(err)}, err
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("encode clip plan: %w", err)
	}
	job, err := database.CreateClipJob(mediaItem.ID, script.ID, mediaItem.FilePath, plan.StartMS, plan.EndMS, string(planJSON))
	if err != nil {
		return nil, err
	}
	_, _ = database.CreateAuditEvent(string(trace.New()), "info", "studio", "clip planned by Doubao Agent", "clip_jobs", job.ID, "")
	return okResult("Doubao clip plan created; confirm local execution separately", map[string]interface{}{"clip_job": job, "plan": plan}), nil
}

func handleStudioClipExecute(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	jobID := int64(argInt(args, "clip_job_id", 0))
	if jobID <= 0 {
		return &Result{OK: false, Error: "MISSING_PARAM:clip_job_id"}, fmt.Errorf("clip_job_id is required")
	}
	if !argBool(args, "confirm", false) {
		return &Result{OK: false, Error: "STUDIO_CLIP_CONFIRMATION_REQUIRED"}, fmt.Errorf("local clip execution requires confirm=true")
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	job, err := database.GetClipJob(jobID)
	if err != nil {
		return &Result{OK: false, Error: "STUDIO_CLIP_NOT_FOUND"}, err
	}
	if job.Status != "planned" && job.Status != "failed" {
		return &Result{OK: false, Error: "STUDIO_CLIP_NOT_EXECUTABLE"}, fmt.Errorf("clip job %d is already complete", job.ID)
	}
	outputPath, err := studio.OutputPath(dataDir(rt), job.ID)
	if err != nil {
		return nil, fmt.Errorf("prepare clip output: %w", err)
	}
	clipCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := studio.ExecuteClip(clipCtx, job.SourcePath, outputPath, job.StartMS, job.EndMS); err != nil {
		return failStudioClip(database, job.ID, studioToolErrorCode(err), err)
	}
	completed, err := indexCompletedStudioClip(database, job.ID, outputPath)
	if err != nil {
		return nil, err
	}
	_, _ = database.CreateAuditEvent(string(trace.New()), "info", "studio", "clip executed by local tool", "clip_jobs", completed.ID, "")
	return okResult("local clip output created", map[string]interface{}{"clip_job": completed}), nil
}

func indexCompletedStudioClip(database *db.DB, jobID int64, outputPath string) (*db.ClipJob, error) {
	meta, err := media.Probe(outputPath)
	if err != nil {
		markStudioClipFailed(database, jobID, "CLIP_OUTPUT_PROBE_FAILED", err)
		return nil, err
	}
	if _, err := database.UpsertMediaLibrary(meta.Path, meta.SHA256, meta.SizeBytes, meta.MimeGuess, meta.Width, meta.Height, meta.DurationMS, meta.ProbeJSON); err != nil {
		markStudioClipFailed(database, jobID, "CLIP_OUTPUT_INDEX_FAILED", err)
		return nil, err
	}
	if err := database.FinishClipJob(jobID, "completed", outputPath, "", ""); err != nil {
		return nil, err
	}
	return database.GetClipJob(jobID)
}

func failStudioClip(database *db.DB, jobID int64, code string, cause error) (*Result, error) {
	markStudioClipFailed(database, jobID, code, cause)
	return &Result{OK: false, Error: code}, cause
}

func markStudioClipFailed(database *db.DB, jobID int64, code string, cause error) {
	_ = database.FinishClipJob(jobID, "failed", "", code, cause.Error())
}

func studioToolErrorCode(err error) string {
	message := err.Error()
	for _, code := range []string{"DOUBAO_API_NOT_CONFIGURED", "DOUBAO_API_UNAVAILABLE", "DOUBAO_API_INVALID_PLAN", "FFMPEG_NOT_FOUND", "FFMPEG_CAPABILITY_UNAVAILABLE", "FFMPEG_MP4_MUXER_UNAVAILABLE", "FFMPEG_CLIP_FAILED"} {
		if strings.Contains(message, code) {
			return code
		}
	}
	return "STUDIO_CLIP_EXECUTION_FAILED"
}
