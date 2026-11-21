package radar

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tikhub"
)

// 这些测试全部打在本地 fake 上：真实 TikHub 额度只在人工链路验收时使用。
const creatorSyncTestAPIKey = "test-key-0123456789"

// 抖音资料响应形状取自 tikhub/calls/2026-11-12/187_douyin_creator_profile.json：
// response.code=200，粉丝数在 data.user.follower_count。
const douyinProfilePath = "/api/v1/douyin/app/v3/handler_user_profile"

// 小红书资料响应形状取自 tikhub/calls/2026-11-12/196_xhs_creator_profile.json：
// 粉丝数在 data.data.fans。
const xhsProfilePath = "/api/v1/xiaohongshu/app_v2/get_user_info"

const douyinPostsPath = "/api/v1/douyin/app/v3/fetch_user_post_videos"

const xhsPostsPath = "/api/v1/xiaohongshu/app_v2/get_user_posted_notes"

func douyinProfileFixture(followers int) string {
	return fmt.Sprintf(`{"code":200,"router":"%s","message":"Request successful.","data":{`+
		`"status_code":0,"user":{"nickname":"单车酒吧搞机社","unique_id":"58831880373",`+
		`"follower_count":%d,"max_follower_count":%d,"mplatform_followers_count":274572}}}`,
		douyinProfilePath, followers, followers)
}

func xhsProfileFixture(followers int) string {
	return fmt.Sprintf(`{"code":200,"data":{"code":0,"success":true,"msg":"成功",`+
		`"data":{"nickname":"瓜瓜是老伙计","red_id":"7436148901","fans":%d,"follows":170}}}`,
		followers)
}

func douyinPostsFixture(hasMore bool) string {
	more := "0"
	if hasMore {
		more = "1"
	}
	return fmt.Sprintf(`{"code":200,"data":{"aweme_list":[],"has_more":%s,"max_cursor":1765347316000}}`, more)
}

func xhsPostsFixture(hasMore bool) string {
	more := "false"
	if hasMore {
		more = "true"
	}
	return fmt.Sprintf(`{"code":200,"data":{"code":0,"success":true,"data":{"has_more":%s,"notes":[],"tags":[]}}}`, more)
}

type creatorFakeRequest struct {
	Path  string
	Query url.Values
}

// creatorFakeServer answers archive calls locally and records every request, so
// a test can prove which endpoints and parameters the pass actually used.
type creatorFakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []creatorFakeRequest
	replies  map[string]string
	statuses map[string]int
}

func newCreatorFakeServer(t *testing.T) *creatorFakeServer {
	t.Helper()
	fake := &creatorFakeServer{replies: map[string]string{}, statuses: map[string]int{}}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.requests = append(fake.requests, creatorFakeRequest{Path: r.URL.Path, Query: r.URL.Query()})
		body, ok := fake.replies[r.URL.Path]
		status := fake.statuses[r.URL.Path]
		fake.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail":"unexpected path"}`))
			return
		}
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *creatorFakeServer) reply(path, body string) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.replies[path] = body
}

func (fake *creatorFakeServer) replyStatus(path string, status int, body string) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.replies[path] = body
	fake.statuses[path] = status
}

func (fake *creatorFakeServer) recorded() []creatorFakeRequest {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]creatorFakeRequest(nil), fake.requests...)
}

func (fake *creatorFakeServer) paths() []string {
	out := make([]string, 0)
	for _, request := range fake.recorded() {
		out = append(out, request.Path)
	}
	return out
}

// newCreatorSyncTestEnv builds one isolated archive environment: temp data dir,
// real migrations on a temp SQLite file, an activated provider with a fake key,
// and an archive client pointed at the local fake server.
func newCreatorSyncTestEnv(t *testing.T) (*creatorFakeServer, string, *db.DB) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := tikhub.SaveProviderCredential(dir, creatorSyncTestAPIKey, "global"); err != nil {
		t.Fatal(err)
	}
	if err := tikhub.MarkProviderTest(dir, true, ""); err != nil {
		t.Fatal(err)
	}
	fake := newCreatorFakeServer(t)
	previous := creatorSyncClientFactory
	creatorSyncClientFactory = func(string) (*tikhub.Client, error) {
		client := tikhub.New(creatorSyncTestAPIKey)
		client.BaseURL = fake.URL
		return client, nil
	}
	t.Cleanup(func() { creatorSyncClientFactory = previous })
	return fake, dir, database
}

// addCreatorSyncTestAccount links a creator to a metric account the same way the
// product does: by the platform identifier (douyin sec_user_id / xhs user_id).
func addCreatorSyncTestAccount(t *testing.T, database *db.DB, platform, identifier, name string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := database.Exec(`INSERT INTO accounts
		(identity_id, platform_code, kind, name, account_identifier, login_state, enabled, created_at, updated_at)
		VALUES (1, ?, 'browser_login', ?, ?, 'logged_in', 1, ?, ?)`,
		platform, name, identifier, now, now); err != nil {
		t.Fatal(err)
	}
}

type creatorFollowerSnapshotRow struct {
	AccountID         int64
	PlatformCode      string
	SnapshotDate      string
	WindowDays        int
	RangeVerified     bool
	Followers         int64
	FollowersVerified bool
	EvidenceCode      string
	CollectedAt       string
}

func loadCreatorFollowerSnapshotRows(t *testing.T, database *db.DB) []creatorFollowerSnapshotRow {
	t.Helper()
	rows, err := database.Query(`SELECT account_id,platform_code,snapshot_date,window_days,
		range_verified,followers,followers_verified,evidence_code,collected_at
		FROM creator_metric_snapshots ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := make([]creatorFollowerSnapshotRow, 0)
	for rows.Next() {
		var row creatorFollowerSnapshotRow
		if err := rows.Scan(&row.AccountID, &row.PlatformCode, &row.SnapshotDate, &row.WindowDays,
			&row.RangeVerified, &row.Followers, &row.FollowersVerified, &row.EvidenceCode, &row.CollectedAt); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func archivedCreatorSnapshotFiles(t *testing.T, dir, platform, userID, capability string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(tikhub.CreatorFolder(dir, platform, userID), "snapshots", "*_"+capability+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// 场景 1：真实资料响应里的粉丝数要以 TIKHUB_CREATOR_PROFILE 落库，作品照旧归档。
func TestSyncCreatorAllSavesDouyinFollowerSnapshot(t *testing.T) {
	fake, dir, database := newCreatorSyncTestEnv(t)
	const creatorID = "MS4wLjABAAAAtest-douyin-creator"
	fake.reply(douyinProfilePath, douyinProfileFixture(12345))
	fake.reply(douyinPostsPath, douyinPostsFixture(false))
	addCreatorSyncTestAccount(t, database, "douyin", creatorID, "测试博主")

	result, err := SyncCreatorAll(context.Background(), database, dir,
		tikhub.WatchUser{Platform: "douyin", UserID: creatorID, Name: "测试博主"}, CreatorSyncOptions{})
	if err != nil {
		t.Fatalf("SyncCreatorAll: %v", err)
	}
	if len(result.CallIDs) != 2 {
		t.Fatalf("call ids=%v want 2 (profile + posts)", result.CallIDs)
	}

	rows := loadCreatorFollowerSnapshotRows(t, database)
	if len(rows) != 1 {
		t.Fatalf("snapshot rows=%d want 1: %#v", len(rows), rows)
	}
	row := rows[0]
	if row.PlatformCode != "douyin" || row.Followers != 12345 || !row.FollowersVerified ||
		row.EvidenceCode != db.CreatorProfileEvidenceCode || row.WindowDays != 7 || row.RangeVerified {
		t.Fatalf("unexpected follower snapshot: %#v", row)
	}
	if row.SnapshotDate != time.Now().UTC().Format("2006-01-02") {
		t.Fatalf("snapshot date=%q want today (UTC)", row.SnapshotDate)
	}

	for _, capability := range []string{"creator_profile", "creator_posts"} {
		if files := archivedCreatorSnapshotFiles(t, dir, "douyin", creatorID, capability); len(files) != 1 {
			t.Fatalf("%s snapshots=%v want exactly 1", capability, files)
		}
	}
	for _, request := range fake.recorded() {
		if got := request.Query.Get("sec_user_id"); got != creatorID {
			t.Fatalf("request %s sec_user_id=%q want %q", request.Path, got, creatorID)
		}
	}
}

// 场景 2：同一天再跑一次要 UPSERT 同一行，不能出现第二行。
func TestSyncCreatorAllUpdatesSameDaySnapshotInsteadOfDuplicating(t *testing.T) {
	fake, dir, database := newCreatorSyncTestEnv(t)
	const creatorID = "MS4wLjABAAAAtest-douyin-creator"
	fake.reply(douyinPostsPath, douyinPostsFixture(false))
	addCreatorSyncTestAccount(t, database, "douyin", creatorID, "测试博主")
	user := tikhub.WatchUser{Platform: "douyin", UserID: creatorID, Name: "测试博主"}

	fake.reply(douyinProfilePath, douyinProfileFixture(12345))
	if _, err := SyncCreatorAll(context.Background(), database, dir, user, CreatorSyncOptions{}); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	fake.reply(douyinProfilePath, douyinProfileFixture(12360))
	if _, err := SyncCreatorAll(context.Background(), database, dir, user, CreatorSyncOptions{}); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	rows := loadCreatorFollowerSnapshotRows(t, database)
	if len(rows) != 1 {
		t.Fatalf("snapshot rows=%d want 1 after same-day re-sync: %#v", len(rows), rows)
	}
	if rows[0].Followers != 12360 || rows[0].EvidenceCode != db.CreatorProfileEvidenceCode {
		t.Fatalf("same-day re-sync must keep the newest verified value: %#v", rows[0])
	}
	profileCalls := 0
	for _, request := range fake.recorded() {
		if request.Path == douyinProfilePath {
			profileCalls++
		}
	}
	if profileCalls != 2 {
		t.Fatalf("profile calls=%d want 2", profileCalls)
	}
}

// 场景 3：资料里没有粉丝字段时不许写 0 值「已验证」快照。
func TestSyncCreatorAllWithoutFollowerFieldWritesNoVerifiedSnapshot(t *testing.T) {
	fake, dir, database := newCreatorSyncTestEnv(t)
	const creatorID = "MS4wLjABAAAAtest-douyin-creator"
	fake.reply(douyinProfilePath, `{"code":200,"data":{"status_code":0,"user":{"nickname":"没有粉丝字段"}}}`)
	fake.reply(douyinPostsPath, douyinPostsFixture(false))
	addCreatorSyncTestAccount(t, database, "douyin", creatorID, "测试博主")

	_, err := SyncCreatorAll(context.Background(), database, dir,
		tikhub.WatchUser{Platform: "douyin", UserID: creatorID, Name: "测试博主"}, CreatorSyncOptions{})
	if err == nil {
		t.Fatal("missing follower field must be reported, not silently stored")
	}
	if !strings.Contains(err.Error(), "follower") {
		t.Fatalf("error must name the missing follower value: %v", err)
	}
	if rows := loadCreatorFollowerSnapshotRows(t, database); len(rows) != 0 {
		t.Fatalf("no verified snapshot may be written: %#v", rows)
	}
	// 资料失败不影响作品归档。
	if files := archivedCreatorSnapshotFiles(t, dir, "douyin", creatorID, "creator_posts"); len(files) != 1 {
		t.Fatalf("posts snapshots=%v want exactly 1", files)
	}
}

// 场景 4：小红书走 xhs 的两个端点与 xhs 的参数名，粉丝快照 platform_code=xhs；
// 分页读不到时如实标注，不假装列表已结束。
func TestSyncCreatorAllUsesXHSEndpointsWithXHSParams(t *testing.T) {
	fake, dir, database := newCreatorSyncTestEnv(t)
	const creatorID = "6474251d000000001c029d9f"
	fake.reply(xhsProfilePath, xhsProfileFixture(3068))
	fake.reply(xhsPostsPath, xhsPostsFixture(true))
	addCreatorSyncTestAccount(t, database, "xhs", creatorID, "测试博主")

	result, err := SyncCreatorAll(context.Background(), database, dir,
		tikhub.WatchUser{Platform: "xhs", UserID: creatorID, Name: "测试博主"}, CreatorSyncOptions{MaxPages: 2})
	if err != nil {
		t.Fatalf("SyncCreatorAll: %v", err)
	}
	if result.Stopped != "paging_unavailable" {
		t.Fatalf("stopped=%q want paging_unavailable when the platform reports more pages", result.Stopped)
	}

	for _, request := range fake.recorded() {
		if strings.Contains(request.Path, "/douyin/") {
			t.Fatalf("xhs creator must never call a douyin endpoint: %s", request.Path)
		}
		if got := request.Query.Get("user_id"); got != creatorID {
			t.Fatalf("request %s user_id=%q want %q", request.Path, got, creatorID)
		}
		if request.Query.Has("sec_user_id") || request.Query.Has("max_cursor") || request.Query.Has("sort_type") {
			t.Fatalf("request %s carries douyin-only parameters: %v", request.Path, request.Query)
		}
	}
	profileRequests := 0
	for _, request := range fake.recorded() {
		if request.Path == xhsProfilePath {
			profileRequests++
		}
		if request.Path == xhsPostsPath && !request.Query.Has("cursor") {
			t.Fatalf("xhs posts must carry the endpoint's fixed cursor: %v", request.Query)
		}
	}
	if profileRequests != 1 {
		t.Fatalf("xhs profile calls=%d want 1", profileRequests)
	}

	rows := loadCreatorFollowerSnapshotRows(t, database)
	if len(rows) != 1 || rows[0].PlatformCode != "xhs" || rows[0].Followers != 3068 ||
		!rows[0].FollowersVerified || rows[0].EvidenceCode != db.CreatorProfileEvidenceCode {
		t.Fatalf("unexpected xhs follower snapshot: %#v", rows)
	}
	for _, capability := range []string{"creator_profile", "creator_posts"} {
		if files := archivedCreatorSnapshotFiles(t, dir, "xhs", creatorID, capability); len(files) != 1 {
			t.Fatalf("%s snapshots=%v want exactly 1", capability, files)
		}
	}

	// 列表真的到底时标注 end_of_list，不沿用 paging_unavailable。
	fake.reply(xhsPostsPath, xhsPostsFixture(false))
	second, err := SyncCreatorAll(context.Background(), database, dir,
		tikhub.WatchUser{Platform: "xhs", UserID: creatorID, Name: "测试博主"}, CreatorSyncOptions{MaxPages: 2})
	if err != nil {
		t.Fatalf("second SyncCreatorAll: %v", err)
	}
	if second.Stopped != "end_of_list" {
		t.Fatalf("stopped=%q want end_of_list", second.Stopped)
	}
}

// 场景 5：未放行平台显式失败，一个请求都不发。
func TestSyncCreatorAllRejectsUnsupportedPlatform(t *testing.T) {
	fake, dir, database := newCreatorSyncTestEnv(t)

	_, err := SyncCreatorAll(context.Background(), database, dir,
		tikhub.WatchUser{Platform: "kuaishou", UserID: "kuaishou-user", Name: "未放行"}, CreatorSyncOptions{})
	if err == nil {
		t.Fatal("unsupported platform must fail explicitly")
	}
	if !strings.Contains(err.Error(), "supports douyin and xhs creators only") {
		t.Fatalf("error must name the unsupported platform: %v", err)
	}
	if requests := fake.recorded(); len(requests) != 0 {
		t.Fatalf("unsupported platform must not call the provider: %#v", requests)
	}
	if rows := loadCreatorFollowerSnapshotRows(t, database); len(rows) != 0 {
		t.Fatalf("unsupported platform must not write snapshots: %#v", rows)
	}
	if _, statErr := filepath.Glob(tikhub.CreatorFolder(dir, "kuaishou", "kuaishou-user") + "/*"); statErr != nil {
		t.Fatal(statErr)
	}
	if matches, _ := filepath.Glob(tikhub.CreatorFolder(dir, "kuaishou", "kuaishou-user") + "/*"); len(matches) != 0 {
		t.Fatalf("unsupported platform must not create a creator folder: %v", matches)
	}
}

// 场景 7：资料请求失败不阻断作品归档，也不留假粉丝行。
func TestSyncCreatorAllKeepsPostsWhenProfileFails(t *testing.T) {
	fake, dir, database := newCreatorSyncTestEnv(t)
	const creatorID = "MS4wLjABAAAAtest-douyin-creator"
	fake.replyStatus(douyinProfilePath, http.StatusInternalServerError, `{"detail":"provider exploded"}`)
	fake.reply(douyinPostsPath, douyinPostsFixture(false))
	addCreatorSyncTestAccount(t, database, "douyin", creatorID, "测试博主")

	_, err := SyncCreatorAll(context.Background(), database, dir,
		tikhub.WatchUser{Platform: "douyin", UserID: creatorID, Name: "测试博主"}, CreatorSyncOptions{})
	if err == nil {
		t.Fatal("a failed profile request must be reported")
	}
	if !strings.Contains(err.Error(), "creator profile request failed") {
		t.Fatalf("error must name the profile failure: %v", err)
	}
	if rows := loadCreatorFollowerSnapshotRows(t, database); len(rows) != 0 {
		t.Fatalf("failed profile must not write a snapshot: %#v", rows)
	}
	if files := archivedCreatorSnapshotFiles(t, dir, "douyin", creatorID, "creator_posts"); len(files) != 1 {
		t.Fatalf("posts snapshots=%v want exactly 1", files)
	}
}
