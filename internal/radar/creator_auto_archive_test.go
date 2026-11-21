package radar

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/tikhub"
)

func TestSelectCreatorAutoArchiveCreatorsRequiresSwitchAndCadence(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	candidates := []CreatorAutoArchiveCandidate{
		{User: tikhub.WatchUser{Platform: "douyin", UserID: "never", AutoArchive: true}},
		{User: tikhub.WatchUser{Platform: "douyin", UserID: "due", AutoArchive: true}, LastAttemptAt: now.Add(-25 * time.Hour)},
		{User: tikhub.WatchUser{Platform: "douyin", UserID: "exactly", AutoArchive: true}, LastAttemptAt: now.Add(-CreatorAutoArchiveInterval)},
		{User: tikhub.WatchUser{Platform: "douyin", UserID: "too-soon", AutoArchive: true}, LastAttemptAt: now.Add(-23 * time.Hour)},
		{User: tikhub.WatchUser{Platform: "douyin", UserID: "future-clock", AutoArchive: true}, LastAttemptAt: now.Add(time.Hour)},
		{User: tikhub.WatchUser{Platform: "douyin", UserID: "off"}, LastAttemptAt: now.Add(-48 * time.Hour)},
		{User: tikhub.WatchUser{Platform: "kuaishou", UserID: "kuaishou-on", AutoArchive: true}},
	}

	selected := SelectCreatorAutoArchiveCreators(candidates, now)
	got := make([]string, 0, len(selected))
	for _, user := range selected {
		got = append(got, user.UserID)
	}
	want := []string{"never", "due", "exactly"}
	if len(got) != len(want) {
		t.Fatalf("selected=%v want=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("selected=%v want=%v", got, want)
		}
	}
}

func TestCreatorArchiveInProgressIgnoresStaleProgress(t *testing.T) {
	dir := t.TempDir()
	platform, userID := "douyin", "mutex-user"
	if err := SaveCreatorArchiveProgress(dir, platform, userID, CreatorArchiveProgress{
		Phase: "posts", StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	if !CreatorArchiveInProgress(dir, platform, userID, time.Now()) {
		t.Fatal("fresh archive-progress.json must block the worker")
	}

	stale := CreatorArchiveProgress{
		Phase:     "posts",
		UpdatedAt: time.Now().Add(-4 * time.Minute).UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(creatorArchiveProgressPath(dir, platform, userID), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if CreatorArchiveInProgress(dir, platform, userID, time.Now()) {
		t.Fatal("stale crash remnant must not block the worker forever")
	}
	if CreatorArchiveInProgress(dir, platform, "no-progress", time.Now()) {
		t.Fatal("missing progress file must not report in-progress")
	}
}

func TestCreatorAutoArchiveStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	platform, userID := "douyin", "state-user"
	if _, ok := LoadCreatorAutoArchiveState(dir, platform, userID); ok {
		t.Fatal("missing state must report not found")
	}
	attempt := time.Date(2026, 11, 5, 10, 30, 0, 0, time.UTC)
	if err := SaveCreatorAutoArchiveState(dir, CreatorAutoArchiveState{
		Platform: platform, UserID: userID,
		LastAttemptAt: attempt.Format(time.RFC3339Nano),
		LastStatus:    "succeeded", LastNewPosts: 3,
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	loaded, ok := LoadCreatorAutoArchiveState(dir, platform, userID)
	if !ok {
		t.Fatal("state not found after save")
	}
	if loaded.SchemaVersion != 1 || loaded.LastStatus != "succeeded" || loaded.LastNewPosts != 3 {
		t.Fatalf("unexpected state: %#v", loaded)
	}
	if !loaded.LastAttempt().Equal(attempt) {
		t.Fatalf("last attempt=%v want=%v", loaded.LastAttempt(), attempt)
	}
	info, err := os.Stat(filepath.Join(tikhub.CreatorFolder(dir, platform, userID), "auto-archive.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode: %v %v", info, err)
	}
}

// TestRunDueIgnoresCreatorsWithoutSwitch proves the default-off contract: a
// watchlist entry that never opted in produces no auto-archive pass at all.
func TestRunDueIgnoresCreatorsWithoutSwitch(t *testing.T) {
	dir := t.TempDir()
	if err := tikhub.AddWatch(dir, "douyin", "manual-only", "纯手动", ""); err != nil {
		t.Fatal(err)
	}
	summary, err := RunDue(context.Background(), dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if summary.CreatorsDue != 0 || summary.CreatorsSucceeded != 0 || summary.CreatorsFailed != 0 || len(summary.CreatorCallIDs) != 0 {
		t.Fatalf("switch-off creator must not run: %#v", summary)
	}
	if _, ok := LoadCreatorAutoArchiveState(dir, "douyin", "manual-only"); ok {
		t.Fatal("switch-off creator must not get an auto-archive state file")
	}
}

// TestRunDueSkipsCreatorWithManualArchiveInProgress covers the mutex: a fresh
// archive-progress.json means a manual pass owns the folder, so the worker must
// skip it and must not record an attempt (the manual run is not an auto run).
func TestRunDueSkipsCreatorWithManualArchiveInProgress(t *testing.T) {
	dir := t.TempDir()
	if err := tikhub.AddWatch(dir, "douyin", "manual-user", "手动中", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tikhub.SetWatchAutoArchive(dir, "douyin", "manual-user", true); err != nil {
		t.Fatal(err)
	}
	if err := SaveCreatorArchiveProgress(dir, "douyin", "manual-user", CreatorArchiveProgress{
		Phase: "posts", StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}

	summary, err := RunDue(context.Background(), dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if summary.CreatorsDue != 1 || summary.CreatorsSkipped != 1 || summary.CreatorsSucceeded != 0 || summary.CreatorsFailed != 0 {
		t.Fatalf("manual archive must be skipped: %#v", summary)
	}
	if _, ok := LoadCreatorAutoArchiveState(dir, "douyin", "manual-user"); ok {
		t.Fatal("skipped creator must not record an auto attempt")
	}
}

// TestRunDueRecordsFailedAttemptAndWaits24h proves one failure does not retry
// every minute: the failed attempt is timestamped, so the next tick sees the
// creator as not due. No provider is configured here, so no request is sent.
func TestRunDueRecordsFailedAttemptAndWaits24h(t *testing.T) {
	dir := t.TempDir()
	if err := tikhub.AddWatch(dir, "douyin", "no-provider", "无供应商", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tikhub.SetWatchAutoArchive(dir, "douyin", "no-provider", true); err != nil {
		t.Fatal(err)
	}

	first, err := RunDue(context.Background(), dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if first.CreatorsDue != 1 || first.CreatorsFailed != 1 || first.CreatorsSucceeded != 0 {
		t.Fatalf("first pass should fail once without a provider: %#v", first)
	}
	state, ok := LoadCreatorAutoArchiveState(dir, "douyin", "no-provider")
	if !ok || state.LastStatus != "failed" || state.LastAttemptAt == "" {
		t.Fatalf("failed attempt must be recorded: %#v ok=%v", state, ok)
	}

	second, err := RunDue(context.Background(), dir, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.CreatorsDue != 0 || second.CreatorsFailed != 0 {
		t.Fatalf("failed creator must wait 24h before retrying: %#v", second)
	}
}

// 场景 6（选择层）：抖音与小红书在开关打开时都应被选中，未支持平台仍被跳过。
func TestSelectCreatorAutoArchiveCreatorsSupportsDouyinAndXHS(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	candidates := []CreatorAutoArchiveCandidate{
		{User: tikhub.WatchUser{Platform: "douyin", UserID: "douyin-on", AutoArchive: true}},
		{User: tikhub.WatchUser{Platform: "xhs", UserID: "xhs-on", AutoArchive: true}},
		{User: tikhub.WatchUser{Platform: "douyin", UserID: "douyin-off"}},
		{User: tikhub.WatchUser{Platform: "xhs", UserID: "xhs-off"}},
		{User: tikhub.WatchUser{Platform: "kuaishou", UserID: "kuaishou-on", AutoArchive: true}},
		{User: tikhub.WatchUser{Platform: "wechat_video", UserID: "wechat-on", AutoArchive: true}},
	}

	selected := SelectCreatorAutoArchiveCreators(candidates, now)
	got := make([]string, 0, len(selected))
	for _, user := range selected {
		got = append(got, user.Platform+"/"+user.UserID)
	}
	want := []string{"douyin/douyin-on", "xhs/xhs-on"}
	if len(got) != len(want) {
		t.Fatalf("selected=%v want=%v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("selected=%v want=%v", got, want)
		}
	}
}

// 场景 6（执行层）：worker 的第二个平台卡点也必须放行小红书——只修查询的话，
// 这里 due 会是 0，而且 kuaishou 即使打开开关也不该产生任何尝试记录。
func TestRunDueAttemptsXHSCreatorAndSkipsUnsupportedPlatform(t *testing.T) {
	dir := t.TempDir()
	if err := tikhub.AddWatch(dir, "xhs", "xhs-auto", "小红书博主", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tikhub.SetWatchAutoArchive(dir, "xhs", "xhs-auto", true); err != nil {
		t.Fatal(err)
	}
	if err := tikhub.AddWatch(dir, "kuaishou", "kuaishou-auto", "快手博主", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tikhub.SetWatchAutoArchive(dir, "kuaishou", "kuaishou-auto", true); err != nil {
		t.Fatal(err)
	}

	summary, err := RunDue(context.Background(), dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// 没有配置 provider，所以这次尝试会失败——但它必须真的被尝试过。
	if summary.CreatorsDue != 1 || summary.CreatorsFailed != 1 || summary.CreatorsSucceeded != 0 {
		t.Fatalf("xhs creator must be attempted by the worker loop: %#v", summary)
	}
	if state, ok := LoadCreatorAutoArchiveState(dir, "xhs", "xhs-auto"); !ok || state.LastStatus != "failed" {
		t.Fatalf("xhs attempt must be recorded: %#v ok=%v", state, ok)
	}
	if _, ok := LoadCreatorAutoArchiveState(dir, "kuaishou", "kuaishou-auto"); ok {
		t.Fatal("unsupported platform must not get an auto-archive attempt")
	}
}
