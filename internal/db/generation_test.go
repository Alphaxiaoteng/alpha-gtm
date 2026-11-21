package db_test

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestGenerationReferenceDomainHasNoProviderURLField(t *testing.T) {
	if _, exists := reflect.TypeOf(db.GenerationReference{}).FieldByName("PublicURL"); exists {
		t.Fatal("GenerationReference still carries provider handoff URLs")
	}
}

func TestGenerationRequestIDIsIdempotentAndConflictsOnChangedInput(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	requestID := "gen-request-idempotent-0001"
	first, created, err := database.CreateGenerationJobIdempotent(requestID, "image", "text_to_image", "agnes", "image-model", "same prompt", nil)
	if err != nil || !created {
		t.Fatalf("first create job=%+v created=%v err=%v", first, created, err)
	}
	second, created, err := database.CreateGenerationJobIdempotent(requestID, "image", "text_to_image", "agnes", "image-model", "same prompt", nil)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("idempotent create job=%+v created=%v err=%v", second, created, err)
	}
	if _, _, err := database.CreateGenerationJobIdempotent(requestID, "image", "text_to_image", "agnes", "image-model", "changed prompt", nil); !errors.Is(err, db.ErrGenerationRequestConflict) {
		t.Fatalf("changed input error=%v", err)
	}
	jobs, err := database.ListRecentGenerationJobs(10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
}

func TestVideoGenerationDurationIsPersistedAndPartOfIdempotencySnapshot(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	requestID := "gen-video-duration-0001"
	first, created, err := database.CreateGenerationJobIdempotentWithOutputAndDuration(
		requestID, "video", "text_to_video", "agnes", "video-model", "9:16", 10, "same prompt", nil,
	)
	if err != nil || !created {
		t.Fatalf("first create job=%+v created=%v err=%v", first, created, err)
	}
	if first.DurationSeconds != 10 {
		t.Fatalf("created duration=%d", first.DurationSeconds)
	}
	stored, err := database.GetGenerationJob(first.ID)
	if err != nil || stored.DurationSeconds != 10 {
		t.Fatalf("stored job=%+v err=%v", stored, err)
	}
	replayed, created, err := database.CreateGenerationJobIdempotentWithOutputAndDuration(
		requestID, "video", "text_to_video", "agnes", "video-model", "9:16", 10, "same prompt", nil,
	)
	if err != nil || created || replayed.ID != first.ID {
		t.Fatalf("replay job=%+v created=%v err=%v", replayed, created, err)
	}
	if _, _, err := database.CreateGenerationJobIdempotentWithOutputAndDuration(
		requestID, "video", "text_to_video", "agnes", "video-model", "9:16", 5, "same prompt", nil,
	); !errors.Is(err, db.ErrGenerationRequestConflict) {
		t.Fatalf("changed duration error=%v", err)
	}
	for _, duration := range []int{1, 6, 11} {
		if _, _, err := database.CreateGenerationJobIdempotentWithOutputAndDuration(
			fmt.Sprintf("gen-video-invalid-duration-%04d", duration),
			"video", "text_to_video", "agnes", "video-model", "9:16", duration, "prompt", nil,
		); err == nil {
			t.Fatalf("accepted invalid video duration %d", duration)
		}
	}
	if _, _, err := database.CreateGenerationJobIdempotentWithOutputAndDuration(
		"gen-image-duration-invalid-0001",
		"image", "text_to_image", "agnes", "image-model", "1:1", 10, "prompt", nil,
	); err == nil {
		t.Fatal("image generation accepted video duration")
	}
}

func TestGenerationJobLifecyclePersistsWithoutExposingPrompt(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	prompt := "一只纸船漂在清晨的湖面"
	job, err := database.CreateGenerationJob("image", "agnes", "image-model", "  "+prompt+"  ")
	if err != nil {
		t.Fatal(err)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte(prompt)))
	if job.Kind != "image" || job.Status != "queued" || job.Progress != 0 || job.Prompt != prompt || job.PromptHash != wantHash {
		t.Fatalf("created job=%+v", job)
	}
	if job.OutputMediaID != nil || job.CreatedAt == "" || job.UpdatedAt == "" {
		t.Fatalf("created job timestamps/output=%+v", job)
	}
	encoded, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), prompt) || strings.Contains(string(encoded), `"prompt"`) {
		t.Fatalf("generation job JSON exposed prompt: %s", encoded)
	}

	recoverable, err := database.ListRecoverableGenerationJobs(10)
	if err != nil || len(recoverable) != 1 || recoverable[0].ID != job.ID {
		t.Fatalf("recoverable=%+v err=%v", recoverable, err)
	}
	if err := database.MarkGenerationJobRunning(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.MarkGenerationJobRemote(job.ID, "task-123", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateGenerationJobProgress(job.ID, 42); err != nil {
		t.Fatal(err)
	}

	media, err := database.UpsertMediaLibrary("/tmp/generated-image.png", "generated-image", 128, "image/png", 1024, 1024, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MarkGenerationJobSucceeded(job.ID, media.ID); err != nil {
		t.Fatal(err)
	}

	stored, err := database.GetGenerationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "succeeded" || stored.Progress != 100 || stored.ExternalTaskID != "task-123" || stored.StartedAt == "" || stored.CompletedAt == "" || stored.OutputMediaID == nil || *stored.OutputMediaID != media.ID {
		t.Fatalf("stored job=%+v", stored)
	}
	recoverable, err = database.ListRecoverableGenerationJobs(10)
	if err != nil || len(recoverable) != 0 {
		t.Fatalf("terminal job remained recoverable: %+v err=%v", recoverable, err)
	}
	recent, err := database.ListRecentGenerationJobs(10)
	if err != nil || len(recent) != 1 || recent[0].ID != job.ID {
		t.Fatalf("recent=%+v err=%v", recent, err)
	}
}

func TestGenerationJobsRecoverAndFailClosed(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	queued, err := database.CreateGenerationJob("image", "agnes", "image-model", "生成静物图")
	if err != nil {
		t.Fatal(err)
	}
	running, err := database.CreateGenerationJob("video", "agnes", "video-model", "生成短镜头")
	if err != nil {
		t.Fatal(err)
	}
	if running.DurationSeconds != 5 {
		t.Fatalf("default video duration=%d", running.DurationSeconds)
	}
	if err := database.MarkGenerationJobRunning(running.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.MarkGenerationJobRemote(running.ID, "task-video-1", "video-1"); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateGenerationJobProgress(running.ID, 25); err != nil {
		t.Fatal(err)
	}

	recoverable, err := database.ListRecoverableGenerationJobs(10)
	if err != nil || len(recoverable) != 2 || recoverable[0].ID != queued.ID || recoverable[1].ID != running.ID {
		t.Fatalf("recoverable=%+v err=%v", recoverable, err)
	}
	if err := database.MarkGenerationJobFailed(running.ID, "PROVIDER_TIMEOUT"); err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetGenerationJob(running.ID)
	if err != nil || stored.Status != "failed" || stored.ErrorCode != "PROVIDER_TIMEOUT" || stored.Progress != 25 || stored.CompletedAt == "" {
		t.Fatalf("failed job=%+v err=%v", stored, err)
	}
	if err := database.MarkGenerationJobRunning(running.ID); err == nil {
		t.Fatal("failed job returned to running")
	}
}

func TestGenerationJobsRejectInvalidInputsAndTransitions(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	for _, input := range []struct {
		kind, provider, model, prompt string
	}{
		{"audio", "agnes", "model", "prompt"},
		{"image", "", "model", "prompt"},
		{"image", "agnes", "", "prompt"},
		{"image", "agnes", "model", ""},
	} {
		if _, err := database.CreateGenerationJob(input.kind, input.provider, input.model, input.prompt); err == nil {
			t.Fatalf("accepted invalid generation input: %+v", input)
		}
	}

	job, err := database.CreateGenerationJob("image", "agnes", "image-model", "合法提示词")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateGenerationJobProgress(job.ID, 10); err == nil {
		t.Fatal("queued job accepted progress")
	}
	if err := database.MarkGenerationJobRemote(job.ID, "https://example.invalid/task", ""); err == nil {
		t.Fatal("remote URL was accepted as a task ID")
	}
	if err := database.MarkGenerationJobRunning(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateGenerationJobProgress(job.ID, 101); err == nil {
		t.Fatal("progress above 100 was accepted")
	}
	if err := database.UpdateGenerationJobProgress(job.ID, 60); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateGenerationJobProgress(job.ID, 50); err == nil {
		t.Fatal("generation progress regressed")
	}
	if err := database.MarkGenerationJobFailed(job.ID, "remote body: unauthorized"); err == nil {
		t.Fatal("free-form provider error was accepted as a stable code")
	}
	if err := database.MarkGenerationJobSucceeded(job.ID, 999999); err == nil {
		t.Fatal("unknown output media was accepted")
	}
}

func TestGenerationJobPersistsCanonicalManagedReferenceSnapshot(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	hash := strings.Repeat("a", 64)
	media, err := database.UpsertMediaLibrary(filepath.Join(t.TempDir(), hash+".png"), hash, 128, "image/png", 8, 8, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	refs := []db.GenerationReference{{MediaID: media.ID, AssetName: hash + ".png", Role: "product", SHA256: hash}}
	job, err := database.CreateGenerationJobWithReferences("image", "image_to_image", "agnes", "image-model", "preserve the product", refs)
	if err != nil {
		t.Fatal(err)
	}
	if job.Mode != "image_to_image" || len(job.References) != 1 || job.References[0] != refs[0] {
		t.Fatalf("created job=%+v", job)
	}

	stored, err := database.GetGenerationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Mode != "image_to_image" || len(stored.References) != 1 || stored.References[0] != refs[0] {
		t.Fatalf("stored job=%+v", stored)
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), filepath.Dir(media.FilePath)) || !strings.Contains(string(encoded), hash) {
		t.Fatalf("reference JSON leaked a local path or omitted SHA: %s", encoded)
	}
}

func TestGenerationJobPersistsImageToVideoLocalSnapshotWithoutProviderURL(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	hash := strings.Repeat("c", 64)
	media, err := database.UpsertMediaLibrary(filepath.Join(t.TempDir(), hash+".png"), hash, 128, "image/png", 8, 8, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	ref := db.GenerationReference{MediaID: media.ID, AssetName: hash + ".png", Role: "first_frame", SHA256: hash}
	job, err := database.CreateGenerationJobWithReferences("video", "image_to_video", "agnes", "video-model", "subtle motion", []db.GenerationReference{ref})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetGenerationJob(job.ID)
	if err != nil || len(stored.References) != 1 || stored.References[0] != ref {
		t.Fatalf("stored image-to-video reference=%+v err=%v", stored, err)
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "public_url") {
		t.Fatalf("provider handoff URL field was persisted: %s", encoded)
	}

}

func TestGenerationMigrationScrubsLegacyProviderURLsFromReferenceJSON(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	prompt := "legacy local first frame"
	promptHash := fmt.Sprintf("%x", sha256.Sum256([]byte(prompt)))
	legacy := `[{"media_id":7,"asset_name":"` + strings.Repeat("d", 64) + `.png","role":"first_frame","sha256":"` + strings.Repeat("d", 64) + `","public_url":"https://provider.invalid/temporary.png"}]`
	if _, err := database.Exec(`INSERT INTO generation_jobs (
		kind, mode, provider, model, prompt, prompt_hash, references_json, status,
		created_at, updated_at
	) VALUES ('video','image_to_video','agnes','video-model',?,?,?,'failed','now','now')`, prompt, promptHash, legacy); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := database.QueryRow(`SELECT references_json FROM generation_jobs WHERE id=1`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "public_url") || strings.Contains(raw, "provider.invalid") {
		t.Fatalf("legacy provider URL survived migration: %s", raw)
	}
	job, err := database.GetGenerationJob(1)
	if err != nil || len(job.References) != 1 || job.References[0].Role != "first_frame" {
		t.Fatalf("scrubbed job=%+v err=%v", job, err)
	}
}

func TestGenerationJobRejectsReferenceModeAndSnapshotConflicts(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	valid := db.GenerationReference{MediaID: 1, AssetName: strings.Repeat("b", 64) + ".png", Role: "product", SHA256: strings.Repeat("b", 64)}

	tests := []struct {
		kind string
		mode string
		refs []db.GenerationReference
	}{
		{kind: "image", mode: "text_to_image", refs: []db.GenerationReference{valid}},
		{kind: "image", mode: "image_to_image"},
		{kind: "video", mode: "image_to_video", refs: []db.GenerationReference{valid}},
		{kind: "video", mode: "image_to_video", refs: []db.GenerationReference{{MediaID: 1, AssetName: valid.AssetName, Role: "first_frame", SHA256: "not-a-sha"}}},
		{kind: "video", mode: "image_to_video", refs: []db.GenerationReference{{MediaID: 1, AssetName: valid.AssetName, Role: "first_frame", SHA256: valid.SHA256}}},
		{kind: "image", mode: "image_to_image", refs: []db.GenerationReference{{MediaID: 1, AssetName: "../escape.png", Role: "product", SHA256: valid.SHA256}}},
	}
	for _, test := range tests {
		if _, err := database.CreateGenerationJobWithReferences(test.kind, test.mode, "agnes", "model", "prompt", test.refs); err == nil {
			t.Fatalf("accepted invalid generation input: %+v", test)
		}
	}
}

func TestGenerationMigrationBackfillsLegacyModes(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	_, err := database.Exec(`CREATE TABLE generation_jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		prompt TEXT NOT NULL,
		prompt_hash TEXT NOT NULL,
		status TEXT NOT NULL,
		external_task_id TEXT NOT NULL DEFAULT '',
		external_video_id TEXT NOT NULL DEFAULT '',
		output_media_id INTEGER,
		progress INTEGER NOT NULL DEFAULT 0,
		error_code TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		started_at TEXT,
		completed_at TEXT
	)`)
	if err != nil {
		t.Fatal(err)
	}
	prompt := "legacy video"
	promptHash := fmt.Sprintf("%x", sha256.Sum256([]byte(prompt)))
	if _, err := database.Exec(`INSERT INTO generation_jobs (kind, provider, model, prompt, prompt_hash, status, created_at, updated_at) VALUES ('video','agnes','video-model',?,?,'queued','now','now')`, prompt, promptHash); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	job, err := database.GetGenerationJob(1)
	if err != nil {
		t.Fatal(err)
	}
	if job.Mode != "text_to_video" || job.DurationSeconds != 5 || len(job.References) != 0 {
		t.Fatalf("legacy job=%+v", job)
	}
}

func TestGenerationJobsSchemaHasNoCredentialOrRemoteURLColumns(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	rows, err := database.Query(`PRAGMA table_info(generation_jobs)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(name)
		for _, forbidden := range []string{"secret", "key", "credential", "remote_url", "output_url", "download_url"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("generation_jobs contains forbidden column %q", name)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestMediaLibraryKeepsContentAddressedPathOnOrdinaryDuplicateImport(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	hash := strings.Repeat("a", 64)
	managedPath := filepath.Join(t.TempDir(), hash+".png")
	ordinaryPath := filepath.Join(t.TempDir(), "ordinary.png")
	if _, err := database.UpsertMediaLibrary(managedPath, hash, 128, "image/png", 8, 8, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpsertMediaLibrary(ordinaryPath, hash, 128, "image/png", 8, 8, 0, ""); err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetMediaLibraryItemBySHA256(hash)
	if err != nil {
		t.Fatal(err)
	}
	if stored.FilePath != managedPath {
		t.Fatalf("ordinary duplicate replaced content-addressed path: got=%q want=%q", stored.FilePath, managedPath)
	}

	secondHash := strings.Repeat("b", 64)
	secondOrdinary := filepath.Join(t.TempDir(), "source.png")
	secondManaged := filepath.Join(t.TempDir(), secondHash+".png")
	if _, err := database.UpsertMediaLibrary(secondOrdinary, secondHash, 256, "image/png", 16, 16, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpsertMediaLibrary(secondManaged, secondHash, 256, "image/png", 16, 16, 0, ""); err != nil {
		t.Fatal(err)
	}
	stored, err = database.GetMediaLibraryItemBySHA256(secondHash)
	if err != nil {
		t.Fatal(err)
	}
	if stored.FilePath != secondManaged {
		t.Fatalf("managed duplicate did not take path ownership: got=%q want=%q", stored.FilePath, secondManaged)
	}
}
