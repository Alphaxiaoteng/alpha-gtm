package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/trace"
)

func TestJSONEmitterUsesTypedContextAndAllowlist(t *testing.T) {
	var out bytes.Buffer
	now := time.Date(2026, 7, 16, 9, 30, 0, 0, time.UTC)
	emitter := NewJSONEmitter(&out, Options{
		Service: "alpha-sidecar",
		Level:   slog.LevelDebug,
		Clock:   func() time.Time { return now },
		PID:     42,
	})
	ctx := trace.WithID(context.Background(), trace.ID("trace-1"))
	ctx = WithCorrelation(ctx, Correlation{RequestID: "req-1", TaskID: "task-1", JobID: 7, BatchID: 9})

	err := emitter.Emit(ctx, Event{
		Level:      slog.LevelInfo,
		Component:  "tools.registry",
		Name:       "tool.call.completed",
		Platform:   "xhs",
		Attempt:    1,
		Step:       "account.list",
		Status:     "succeeded",
		DurationMS: 12,
	})
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &got); err != nil {
		t.Fatalf("decode event: %v\n%s", err, out.String())
	}
	for key, want := range map[string]any{
		"schema_version": float64(1),
		"service":        "alpha-sidecar",
		"component":      "tools.registry",
		"event":          "tool.call.completed",
		"trace_id":       "trace-1",
		"request_id":     "req-1",
		"task_id":        "task-1",
		"job_id":         float64(7),
		"batch_id":       float64(9),
		"platform":       "xhs",
		"step":           "account.list",
		"status":         "succeeded",
		"duration_ms":    float64(12),
		"pid":            float64(42),
	} {
		if got[key] != want {
			t.Errorf("%s=%v, want %v", key, got[key], want)
		}
	}
	for _, forbidden := range []string{"msg", "message", "args", "error", "path", "content", "authorization"} {
		if _, ok := got[forbidden]; ok {
			t.Errorf("forbidden field %q present: %v", forbidden, got[forbidden])
		}
	}
}

func TestJSONEmitterDropsUnsafeCorrelationValues(t *testing.T) {
	var out bytes.Buffer
	emitter := NewJSONEmitter(&out, Options{Service: "alpha-sidecar"})
	secret := "Bearer secret-token /tmp/private?access_token=abc"
	ctx := WithCorrelation(context.Background(), Correlation{RequestID: secret, TaskID: secret})
	if err := emitter.Emit(ctx, Event{Component: "tools.registry", Name: "tool.call.completed", Step: secret}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "secret-token") || strings.Contains(out.String(), "/tmp/private") || strings.Contains(out.String(), "access_token") {
		t.Fatalf("unsafe value leaked: %s", out.String())
	}
}

func TestWithCorrelationMergesNonZeroFields(t *testing.T) {
	ctx := WithCorrelation(context.Background(), Correlation{RequestID: "request-parent", JobID: 7, BatchID: 9})
	ctx = WithCorrelation(ctx, Correlation{TaskID: "task-child", JobID: 8})
	got := CorrelationFromContext(ctx)
	if got.RequestID != "request-parent" || got.TaskID != "task-child" || got.JobID != 8 || got.BatchID != 9 {
		t.Fatalf("merged correlation=%+v", got)
	}
}

func TestStableErrorCodeNeverReturnsRawError(t *testing.T) {
	if got := StableErrorCode("MISSING_PARAM:password"); got != "MISSING_PARAM" {
		t.Fatalf("got %q", got)
	}
	secret := "request failed for /tmp/private?token=abc"
	if got := StableErrorCode(secret); got != "INTERNAL_ERROR" || strings.Contains(got, "private") {
		t.Fatalf("raw error escaped: %q", got)
	}
	if got := StableErrorCode("SECRETTOKEN"); got != "INTERNAL_ERROR" {
		t.Fatalf("unknown uppercase value escaped: %q", got)
	}
	if got := StableErrorCode("INTERNAL_PRIVATE_SENTINEL"); got != "INTERNAL_ERROR" {
		t.Fatalf("prefix-only pseudo-code escaped: %q", got)
	}
}

func TestStableErrorCodePreservesReviewedPublishWorkerCodes(t *testing.T) {
	for _, code := range []string{"TAGS_INVALID", "FORM_REVIEW_REQUIRED", "PLATFORM_VERIFICATION_REQUIRED", "SUBMIT_BUTTON_NOT_FOUND", "PUBLISH_CLICK_OUTCOME_UNKNOWN", "PUBLISH_SUCCESS_UNVERIFIED", "PLAYWRIGHT_WORKER_FAILED", "WORKER_OUTPUT_INVALID", "CDP_CONNECT_FAILED", "CDP_PAGE_LIST_FAILED", "CDP_TARGET_CLOSED", "COMPOSER_NAVIGATION_FAILED", "WORKER_EXECUTION_FAILED", "WORKER_TARGET_PAGE_FAILED", "WORKER_FORM_PREPARE_FAILED", "SETTINGS_UNSUPPORTED"} {
		if got := StableErrorCode(code); got != code {
			t.Fatalf("code=%q got=%q", code, got)
		}
	}
}

func TestStableErrorCodePreservesReviewedMetricsBatchFailure(t *testing.T) {
	for _, code := range []string{
		"EGO_METRICS_BATCH_FAILED",
		"EGO_METRICS_COLLECTION_FAILED",
		"EGO_RUNTIME_EXECUTION_FAILED",
		"EGO_SPACE_BINDING_MISSING",
		"EGO_SPACE_INACTIVE",
	} {
		if got := StableErrorCode(code); got != code {
			t.Fatalf("code=%q got=%q", code, got)
		}
	}
}

func TestStableErrorCodePreservesReviewedMetaPublishCodes(t *testing.T) {
	for _, code := range []string{
		"COMPOSER_UNCONFIRMED",
		"MEDIA_FILE_UNAVAILABLE",
		"MEDIA_UPLOAD_UNCONFIRMED",
		"META_COVER_UPLOAD_UNVERIFIED",
		"META_NATIVE_SCHEDULE_UNVERIFIED",
		"TARGET_SELECTION_UNCONFIRMED",
	} {
		if got := StableErrorCode(code); got != code {
			t.Fatalf("StableErrorCode(%q)=%q", code, got)
		}
	}
}

func TestFileEmitterUses0600AndBoundedRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "events.jsonl")
	emitter, err := NewFileEmitter(path, FileOptions{
		Options:  Options{Service: "alpha-sidecar", Clock: func() time.Time { return time.Unix(0, 0).UTC() }},
		MaxBytes: 320,
		MaxFiles: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		if err := emitter.Emit(context.Background(), Event{
			Component: "tools.registry",
			Name:      "tool.call.completed",
			Step:      "account.list",
			Status:    "succeeded",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := emitter.Close(); err != nil {
		t.Fatal(err)
	}

	for _, file := range []string{path, path + ".1"} {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatalf("stat %s: %v", file, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode=%o, want 600", file, got)
		}
		if info.Size() > 320 {
			t.Errorf("%s size=%d exceeds max", file, info.Size())
		}
	}
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Fatalf("rotation exceeded max files: %v", err)
	}
}

func TestFileEmitterDoesNotChangeExistingDirectoryMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	emitter, err := NewFileEmitter(filepath.Join(dir, "events.jsonl"), FileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("existing directory mode changed to %o", got)
	}
}

func TestFileEmitterTruncatesPreexistingOversizedActiveFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 1024), 0o600); err != nil {
		t.Fatal(err)
	}
	emitter, err := NewFileEmitter(path, FileOptions{MaxBytes: 320, MaxFiles: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(context.Background(), Event{Component: "runtime", Name: "startup.completed", Status: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	if err := emitter.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 0 || info.Size() > 320 {
		t.Fatalf("active log size=%d", info.Size())
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatalf("oversized backup survived: %v", err)
	}
}
