package radar

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/tikhub"
)

func TestScheduleValidationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	schedule, err := ValidateSchedule(Schedule{
		ID: "radar-test", Name: "AI 工具", NicheID: "ai",
		EndpointID: "douyin.video_search", IntervalMinutes: 360, Enabled: false,
		Params: map[string]any{
			"keyword": "AI工具", "publish_time": "7", "sort_type": "2",
			"filter_duration": "0", "content_type": "0",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	file := ScheduleFile{Schedules: []Schedule{schedule}}
	if err := SaveSchedules(dir, file); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSchedules(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Schedules) != 1 || loaded.Schedules[0].Params["publish_time"] != "7" {
		t.Fatalf("unexpected schedules: %#v", loaded)
	}
	info, err := os.Stat(schedulePath(dir))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("schedule file mode: %v %v", info, err)
	}
	// Verify that supported endpoints (including XHS and YouTube) are accepted
	xhsSched, err := ValidateSchedule(Schedule{
		ID: "radar-xhs", Name: "小红书AI工具", NicheID: "ai",
		EndpointID: "xhs.note_search", IntervalMinutes: 60,
		Params: map[string]any{"keyword": "AI工具"},
	})
	if err != nil {
		t.Fatalf("xhs schedule unexpectedly rejected: %v", err)
	}
	if xhsSched.EndpointID != "xhs.note_search" {
		t.Fatalf("unexpected endpoint: %s", xhsSched.EndpointID)
	}

	// Verify unsupported endpoint is rejected
	if _, err := ValidateSchedule(Schedule{
		EndpointID: "unsupported.search", IntervalMinutes: 60,
		Params: map[string]any{"keyword": "AI工具"},
	}); err == nil || !strings.Contains(err.Error(), "not enabled for automated schedules") {
		t.Fatal("unsupported schedule unexpectedly accepted")
	}
}

func TestWorkerRecoversOnlyStaleScopedLock(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "trend-radar", "worker.lock")
	if err := os.MkdirAll(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-31 * time.Minute)
	if err := os.Chtimes(lock, stale, stale); err != nil {
		t.Fatal(err)
	}
	summary, err := RunDue(context.Background(), dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Due != 0 {
		t.Fatalf("unexpected due schedules: %#v", summary)
	}
}

func TestUpdateSchedulesPreservesLatestUserEdits(t *testing.T) {
	dir := t.TempDir()
	if err := SaveSchedules(dir, ScheduleFile{Schedules: []Schedule{{
		ID: "radar-test", Name: "原名称", Enabled: true,
	}}}); err != nil {
		t.Fatal(err)
	}

	if err := UpdateSchedules(dir, func(file *ScheduleFile) error {
		file.Schedules[0].Name = "用户刚修改的名称"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := MergeScheduleRuntime(dir, []Schedule{{
		ID: "radar-test", LastStatus: "succeeded", LastCallID: 42,
	}}); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadSchedules(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Schedules[0]
	if got.Name != "用户刚修改的名称" || got.LastStatus != "succeeded" || got.LastCallID != 42 {
		t.Fatalf("latest edit or runtime state was lost: %#v", got)
	}
}

// TestAllowedScheduleEndpointsExistInCatalog prevents a silent regression where
// the unattended worker allowlist drifts from the real TikHub catalog. An unknown
// or non-search endpoint here would only fail inside the LaunchAgent, where the
// error is easy to miss.
func TestAllowedScheduleEndpointsExistInCatalog(t *testing.T) {
	if len(allowedScheduleEndpoints) == 0 {
		t.Fatal("schedule endpoint allowlist must not be empty")
	}
	for id := range allowedScheduleEndpoints {
		endpoint, ok := tikhub.FindEndpoint(id)
		if !ok {
			t.Fatalf("allowlisted endpoint %q does not exist in the TikHub catalog", id)
		}
		if endpoint.Capability != "keyword_search" {
			t.Fatalf("allowlisted endpoint %q capability = %q, want keyword_search", id, endpoint.Capability)
		}
	}
}
