package db_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

func TestCreatorMetricsUpsertDailySnapshotAndBuildSevenDayOverview(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.CreateEgoIdentity("数据浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音主号", "")
	if err != nil {
		t.Fatal(err)
	}
	input := db.CreatorMetricSnapshotInput{
		AccountID: account.ID, PlatformCode: "douyin", SnapshotDate: "2026-08-01", WindowDays: 7,
		RangeVerified: true, Views: 1200, Likes: 80, Comments: 12, Shares: 9, Favorites: 18, Posts: 2,
		EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{"range":"7d"}`, CollectedAt: "2026-08-01T01:00:00Z",
	}
	snapshot, err := database.UpsertCreatorMetricSnapshot(input, []db.CreatorPostMetricInput{{
		ExternalPostID: "post-1", Title: "第一条", PublishedAt: "2026-07-30", PostURL: "https://www.douyin.com/video/post-1",
		Views: 1000, Likes: 70, Comments: 10, Shares: 8, Favorites: 16, RawJSON: `{}`,
	}})
	if err != nil || snapshot.ID == 0 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	input.Views = 1500
	input.CollectedAt = "2026-08-01T02:00:00Z"
	if _, err := database.UpsertCreatorMetricSnapshot(input, nil); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Latest) != 1 || overview.Latest[0].Views != 1500 || len(overview.Daily) != 1 {
		t.Fatalf("overview=%+v", overview)
	}
	if len(overview.Collections) != 1 || overview.Collections[0].State != "observed" || overview.Collections[0].CollectedAt != input.CollectedAt {
		t.Fatalf("legacy verified snapshot was not backfilled: %+v", overview.Collections)
	}
	if len(overview.TopPosts) != 0 {
		t.Fatalf("same-day replacement must replace stale post rows: %+v", overview.TopPosts)
	}
	joinedInsights := strings.Join(overview.Insights, "\n")
	if !strings.Contains(joinedInsights, "抖音 当前浏览量最高：1500") || strings.Contains(joinedInsights, "douyin 当前浏览量最高") {
		t.Fatalf("insights must use the user-facing platform name: %v", overview.Insights)
	}
}

func TestCreatorMetricPreferencesControlHistoryAndPruneExpiredSnapshots(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	preferences, err := database.CreatorMetricPreferences()
	if err != nil {
		t.Fatal(err)
	}
	if preferences.HistoryDays != 30 || preferences.RetentionDays != 90 {
		t.Fatalf("default preferences=%+v", preferences)
	}
	if _, err := database.UpdateCreatorMetricPreferences(90, 30); err == nil {
		t.Fatal("retention shorter than history was accepted")
	}
	preferences, err = database.UpdateCreatorMetricPreferences(30, 30)
	if err != nil {
		t.Fatal(err)
	}
	if preferences.HistoryDays != 30 || preferences.RetentionDays != 30 {
		t.Fatalf("updated preferences=%+v", preferences)
	}

	identity, err := database.CreateEgoIdentity("保留期浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "保留期账号", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, date := range []string{"2026-06-01", "2026-07-15", "2026-08-01"} {
		if _, err := database.UpsertCreatorMetricSnapshot(db.CreatorMetricSnapshotInput{
			AccountID: account.ID, PlatformCode: "douyin", SnapshotDate: date, WindowDays: 7,
			RangeVerified: true, Views: 100, EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{}`,
			CollectedAt: date + "T01:00:00Z",
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var dates string
	if err := database.QueryRow(`SELECT GROUP_CONCAT(snapshot_date, ',') FROM (SELECT snapshot_date FROM creator_metric_snapshots ORDER BY snapshot_date)`).Scan(&dates); err != nil {
		t.Fatal(err)
	}
	if dates != "2026-07-15,2026-08-01" {
		t.Fatalf("expired snapshots were not pruned: %q", dates)
	}

	overview, err := database.CreatorMetricOverview(30, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if overview.WindowDays != 30 || overview.FromDate != "2026-07-03" || len(overview.Daily) != 2 {
		t.Fatalf("30 day overview=%+v", overview)
	}
}

func TestCreatorMetricRetentionChangePrunesImmediately(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("即时清理浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "douyin", "即时清理账号", "")
	for _, date := range []string{"2026-06-01", "2026-08-01"} {
		if _, err := database.UpsertCreatorMetricSnapshot(db.CreatorMetricSnapshotInput{
			AccountID: account.ID, PlatformCode: "douyin", SnapshotDate: date, WindowDays: 7,
			RangeVerified: true, Views: 100, EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{}`,
			CollectedAt: date + "T01:00:00Z",
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.UpdateCreatorMetricPreferences(30, 30); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM creator_metric_snapshots WHERE snapshot_date='2026-06-01'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("lowering retention did not prune expired snapshots immediately")
	}
}

func TestCreatorMetricsOverviewKeepsDailyRowsAttributableToBrowserAccounts(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	firstIdentity, err := database.CreateEgoIdentity("趋势浏览器甲")
	if err != nil {
		t.Fatal(err)
	}
	secondIdentity, err := database.CreateEgoIdentity("趋势浏览器乙")
	if err != nil {
		t.Fatal(err)
	}
	firstAccount, err := database.CreateAccountForIdentity(firstIdentity.ID, "douyin", "浏览器1抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	secondAccount, err := database.CreateAccountForIdentity(secondIdentity.ID, "douyin", "浏览器2抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		accountID int64
		views     int64
	}{
		{accountID: firstAccount.ID, views: 100},
		{accountID: secondAccount.ID, views: 900},
	} {
		if _, err := database.UpsertCreatorMetricSnapshot(db.CreatorMetricSnapshotInput{
			AccountID: fixture.accountID, PlatformCode: "douyin", SnapshotDate: "2026-08-01", WindowDays: 7,
			RangeVerified: true, Views: fixture.views, EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{}`,
			CollectedAt: "2026-08-01T03:00:00Z",
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.AccountDaily) != 2 {
		t.Fatalf("account daily=%+v", overview.AccountDaily)
	}
	viewsByAccount := map[int64]int64{}
	for _, day := range overview.AccountDaily {
		viewsByAccount[day.AccountID] += day.Views
	}
	if viewsByAccount[firstAccount.ID] != 100 || viewsByAccount[secondAccount.ID] != 900 {
		t.Fatalf("daily rows lost account attribution: %+v", overview.AccountDaily)
	}
	if len(overview.Daily) != 1 || overview.Daily[0].Views != 1000 {
		t.Fatalf("legacy global daily summary changed: %+v", overview.Daily)
	}
}

func TestCreatorMetricsDoesNotInventZeroViewRankingInsight(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("粉丝数据浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "kuaishou", "快手主号", "")
	_, err := database.UpsertCreatorMetricSnapshot(db.CreatorMetricSnapshotInput{
		AccountID: account.ID, PlatformCode: "kuaishou", SnapshotDate: "2026-08-01", WindowDays: 7,
		RangeVerified: true, Followers: 119000, EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{}`,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, insight := range overview.Insights {
		if strings.Contains(insight, "浏览量最高：0") {
			t.Fatalf("zero-view ranking insight must be omitted: %q", insight)
		}
	}
}

func TestCreatorMetricsMigrationInvalidatesUnboundDOMFollowerSnapshots(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("错误粉丝数据浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "kuaishou", "快手主号", "")
	snapshot, err := database.UpsertCreatorMetricSnapshot(db.CreatorMetricSnapshotInput{
		AccountID: account.ID, PlatformCode: "kuaishou", SnapshotDate: "2026-08-01", WindowDays: 7,
		RangeVerified: true, Followers: 124000, EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{"matched_labels":["粉丝"]}`,
		CollectedAt: "2026-08-01T01:00:00Z",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	var rangeVerified bool
	var followers int64
	var evidenceCode string
	if err := database.QueryRow(`SELECT range_verified,followers,evidence_code FROM creator_metric_snapshots WHERE id=?`, snapshot.ID).Scan(&rangeVerified, &followers, &evidenceCode); err != nil {
		t.Fatal(err)
	}
	if rangeVerified || followers != 0 || evidenceCode != "INVALIDATED_KUAISHOU_DOM" {
		t.Fatalf("snapshot range=%v followers=%d evidence=%q", rangeVerified, followers, evidenceCode)
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Latest) != 0 || len(overview.Collections) != 1 || overview.Collections[0].State != "unknown" || overview.Collections[0].ErrorCode != "METRICS_DOM_UNBOUND" {
		t.Fatalf("invalid DOM follower snapshot leaked into overview: %+v", overview)
	}
}

func TestCreatorMetricsMigrationInvalidatesAllKuaishouPageWideDOMMetrics(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("有效快手数据浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "kuaishou", "快手主号", "")
	snapshot, err := database.UpsertCreatorMetricSnapshot(db.CreatorMetricSnapshotInput{
		AccountID: account.ID, PlatformCode: "kuaishou", SnapshotDate: "2026-08-01", WindowDays: 7,
		RangeVerified: true, Followers: 124000, Views: 321, Likes: 12,
		EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{"matched_labels":["粉丝","作品播放量","点赞量"]}`,
		CollectedAt: "2026-08-01T01:00:00Z",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	var rangeVerified bool
	var followers, views, likes int64
	var evidenceCode string
	if err := database.QueryRow(`SELECT range_verified,followers,views,likes,evidence_code FROM creator_metric_snapshots WHERE id=?`, snapshot.ID).
		Scan(&rangeVerified, &followers, &views, &likes, &evidenceCode); err != nil {
		t.Fatal(err)
	}
	if rangeVerified || followers != 0 || views != 0 || likes != 0 || evidenceCode != "INVALIDATED_KUAISHOU_DOM" {
		t.Fatalf("snapshot range=%v followers=%d views=%d likes=%d evidence=%q", rangeVerified, followers, views, likes, evidenceCode)
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Latest) != 0 || len(overview.Collections) != 1 || overview.Collections[0].State != "unknown" ||
		overview.Collections[0].ErrorCode != "METRICS_DOM_UNBOUND" {
		t.Fatalf("invalidated Kuaishou DOM snapshot leaked into overview: %+v", overview)
	}
}

func TestCreatorMetricsMigrationInvalidatesResourceSnapshotWithoutProvenance(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("旧小红书资源浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	now := "2026-08-01T01:00:00Z"
	result, err := database.Exec(`INSERT INTO creator_metric_snapshots
		(account_id,platform_code,snapshot_date,window_days,range_verified,views,evidence_code,raw_json,collected_at,created_at,updated_at)
		VALUES (?,?,?,7,1,0,'REVIEWED_ANALYTICS_RESOURCE','{"range_verified":true}',?,?,?)`,
		account.ID, "xhs", "2026-08-01", now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	snapshotID, _ := result.LastInsertId()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	var rangeVerified bool
	var evidenceCode string
	if err := database.QueryRow(`SELECT range_verified,evidence_code FROM creator_metric_snapshots WHERE id=?`, snapshotID).Scan(&rangeVerified, &evidenceCode); err != nil {
		t.Fatal(err)
	}
	if rangeVerified || evidenceCode != "INVALIDATED_RESOURCE_PROVENANCE" {
		t.Fatalf("legacy resource snapshot range=%v evidence=%q", rangeVerified, evidenceCode)
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Latest) != 0 || len(overview.Collections) != 1 || overview.Collections[0].State != "unknown" ||
		overview.Collections[0].ErrorCode != "METRICS_RESOURCE_PROVENANCE_MISSING" {
		t.Fatalf("unproven resource snapshot leaked into overview: %+v", overview)
	}
}

func TestCreatorMetricsOverviewExcludesLegacyUnverifiedSnapshots(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("旧数据浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音旧数据", "")
	now := "2026-08-01T01:00:00Z"
	result, err := database.Exec(`INSERT INTO creator_metric_snapshots
		(account_id,platform_code,snapshot_date,window_days,range_verified,views,evidence_code,raw_json,collected_at,created_at,updated_at)
		VALUES (?,?,?,7,0,999,'LEGACY_UNVERIFIED','{}',?,?,?)`, account.ID, "douyin", "2026-08-01", now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	snapshotID, _ := result.LastInsertId()
	if _, err := database.Exec(`INSERT INTO creator_post_metric_snapshots
		(snapshot_id,account_id,platform_code,post_key,title,views,raw_json,created_at)
		VALUES (?,?,?,?,?,999,'{}',?)`, snapshotID, account.ID, "douyin", "legacy", "不应出现", now); err != nil {
		t.Fatal(err)
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Latest) != 0 || len(overview.Daily) != 0 || len(overview.Platforms) != 0 || len(overview.TopPosts) != 0 {
		t.Fatalf("legacy unverified metrics leaked into overview: %+v", overview)
	}
}

func TestCreatorMetricsRejectWrongPlatformSensitivePayloadAndUnsafeURL(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("安全数据浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	base := db.CreatorMetricSnapshotInput{
		AccountID: account.ID, PlatformCode: "douyin", SnapshotDate: "2026-08-01", WindowDays: 7,
		EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{}`,
	}
	if _, err := database.UpsertCreatorMetricSnapshot(base, nil); err == nil {
		t.Fatal("platform mismatch accepted")
	}
	base.PlatformCode = "xhs"
	base.Views = 1
	if _, err := database.UpsertCreatorMetricSnapshot(base, nil); err == nil {
		t.Fatal("unverified seven-day range accepted")
	}
	base.RangeVerified = true
	base.RawJSON = `{"session_token":"secret"}`
	if _, err := database.UpsertCreatorMetricSnapshot(base, nil); err == nil {
		t.Fatal("sensitive payload accepted")
	}
	base.RawJSON = `{}`
	if _, err := database.UpsertCreatorMetricSnapshot(base, []db.CreatorPostMetricInput{{Title: "作品", PostURL: "https://example.com/post?token=secret", RawJSON: `{}`}}); err == nil {
		t.Fatal("query-bearing post URL accepted")
	}
	if _, err := database.UpsertCreatorMetricSnapshot(base, []db.CreatorPostMetricInput{{Title: "作品", PostURL: "https://example.com/post", Views: 1, RawJSON: `{}`}}); err == nil {
		t.Fatal("cross-platform post URL accepted")
	}
	base.EvidenceCode = "REVIEWED_CONTENT_DOM"
	if _, err := database.UpsertCreatorMetricSnapshot(base, nil); err == nil {
		t.Fatal("content evidence without a measured post accepted")
	}
	if _, err := database.UpsertCreatorMetricSnapshot(base, []db.CreatorPostMetricInput{{Title: "表头", RawJSON: `{}`}}); err == nil {
		t.Fatal("zero-metric table row accepted as a post")
	}
	base.EvidenceCode = "REVIEWED_ANALYTICS_RESOURCE"
	base.RangeVerified = true
	if _, err := database.UpsertCreatorMetricSnapshot(base, nil); err == nil {
		t.Fatal("resource snapshot without provenance accepted")
	}
	base.RawJSON = `{"resource_diagnostics":[{"resource":"https://creator.xiaohongshu.com/api/galaxy/v2/creator/datacenter/livedata/overview","range_hints":["resource_explicit_7_day_bucket"],"numeric_keys":["data.seven.view_count"]}]}`
	if _, err := database.UpsertCreatorMetricSnapshot(base, nil); err != nil {
		t.Fatalf("explicit seven-day zero-value resource snapshot rejected: %v", err)
	}
	base.EvidenceCode = "REVIEWED_CONTENT_RESOURCE"
	if _, err := database.UpsertCreatorMetricSnapshot(base, []db.CreatorPostMetricInput{{
		ExternalPostID: "resource-post", Title: "资源作品", Views: 1, RawJSON: `{"source":"reviewed_resource"}`,
	}}); err != nil {
		t.Fatalf("reviewed read-only resource evidence rejected: %v", err)
	}
	base.EvidenceCode = "REVIEWED_CONTENT_RESOURCE_EMPTY"
	if _, err := database.UpsertCreatorMetricSnapshot(base, nil); err == nil {
		t.Fatal("empty resource-window evidence was accepted for a non-Douyin platform")
	}
	base.PlatformCode = "douyin"
	if _, err := database.UpsertCreatorMetricSnapshot(base, nil); err == nil {
		t.Fatal("account/platform mismatch accepted while testing empty resource evidence")
	}
}

func TestCreatorMetricsAcceptsCompleteZeroValueKuaishouWindow(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("快手空窗口")
	account, _ := database.CreateAccountForIdentity(identity.ID, "kuaishou", "快手", "")
	input := db.CreatorMetricSnapshotInput{
		AccountID: account.ID, PlatformCode: "kuaishou", SnapshotDate: "2026-08-01", WindowDays: 7,
		RangeVerified: true, EvidenceCode: "REVIEWED_CONTENT_RESOURCE_EMPTY",
		RawJSON: `{"resource_diagnostics":[{"resource":"https://cp.kuaishou.com/rest/cp/creator/analysis/pc/home/photo/list","range_hints":["resource_complete_7_day_window"],"numeric_keys":[]}]}`,
	}
	snapshot, err := database.UpsertCreatorMetricSnapshot(input, nil)
	if err != nil {
		t.Fatalf("complete zero-value Kuaishou window rejected: %v", err)
	}
	if snapshot.PlatformCode != "kuaishou" || snapshot.Posts != 0 || snapshot.Views != 0 || !snapshot.RangeVerified {
		t.Fatalf("unexpected Kuaishou zero snapshot: %+v", snapshot)
	}
}

func TestCreatorMetricCollectionStatePersistsUnknownWithoutInventingSnapshot(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("采集状态浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "wechat_video", "视频号", "")

	state, err := database.UpsertCreatorMetricCollectionState(db.CreatorMetricCollectionStateInput{
		AccountID: account.ID, PlatformCode: "wechat_video", State: "unknown",
		ErrorCode: "METRICS_DOM_UNRECOGNIZED", CollectedAt: "2026-08-01T03:00:00Z",
	})
	if err != nil || state.State != "unknown" || state.ErrorCode != "METRICS_DOM_UNRECOGNIZED" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Collections) != 1 || overview.Collections[0].AccountID != account.ID || overview.Collections[0].State != "unknown" {
		t.Fatalf("overview collections=%+v", overview.Collections)
	}
	if len(overview.Latest) != 0 {
		t.Fatalf("unknown collection must not invent a metric snapshot: %+v", overview.Latest)
	}
}

func TestCreatorMetricCollectionStateRecordsManualHandoffForUnadaptedPlatform(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("全平台数据浏览器")
	account, err := database.CreateAccountForIdentity(identity.ID, "bilibili", "B 站", "")
	if err != nil {
		t.Fatal(err)
	}

	state, err := database.UpsertCreatorMetricCollectionState(db.CreatorMetricCollectionStateInput{
		AccountID: account.ID, PlatformCode: "bilibili", State: "manual_takeover",
		ErrorCode: "EGO_METRICS_MANUAL_HANDOFF", CollectedAt: "2026-08-01T03:00:00Z",
	})
	if err != nil || state.State != "manual_takeover" || state.RangeVerified {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Collections) != 1 || overview.Collections[0].PlatformCode != "bilibili" || len(overview.Latest) != 0 {
		t.Fatalf("manual handoff must be visible without a fabricated snapshot: %+v", overview)
	}
}

func TestCreatorMetricCollectionStateMigrationAcceptsManualHandoffForExistingDatabase(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DROP TABLE creator_metric_collection_states`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE creator_metric_collection_states (
		account_id INTEGER PRIMARY KEY, platform_code TEXT NOT NULL,
		state TEXT NOT NULL CHECK (state IN ('observed','skipped','unknown')),
		error_code TEXT NOT NULL DEFAULT '', evidence_code TEXT NOT NULL DEFAULT '',
		range_verified INTEGER NOT NULL DEFAULT 0, post_count INTEGER NOT NULL DEFAULT 0,
		collected_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("旧数据浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "bilibili", "B 站", "")
	if _, err := database.UpsertCreatorMetricCollectionState(db.CreatorMetricCollectionStateInput{
		AccountID: account.ID, PlatformCode: "bilibili", State: "manual_takeover", ErrorCode: "EGO_METRICS_MANUAL_HANDOFF",
	}); err != nil {
		t.Fatalf("existing database must accept manual handoff after migrate: %v", err)
	}
}

func TestCreatorMetricsOverviewCountsLoggedInAccountsWithoutSnapshots(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("四平台浏览器")
	accounts := map[string]*db.Account{}
	for _, code := range []string{"douyin", "wechat_video", "kuaishou", "xhs"} {
		account, err := database.CreateAccountForIdentity(identity.ID, code, code, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
			t.Fatal(err)
		}
		accounts[code] = account
	}
	for _, code := range []string{"douyin", "kuaishou", "xhs"} {
		if _, err := database.UpsertCreatorMetricCollectionState(db.CreatorMetricCollectionStateInput{
			AccountID: accounts[code].ID, PlatformCode: code, State: "observed",
			EvidenceCode: "REVIEWED_ANALYTICS_DOM", RangeVerified: true,
			CollectedAt: "2026-08-01T03:00:00Z",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := database.UpsertCreatorMetricSnapshot(db.CreatorMetricSnapshotInput{
			AccountID: accounts[code].ID, PlatformCode: code, SnapshotDate: "2026-08-01", WindowDays: 7,
			RangeVerified: true, EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{}`,
			CollectedAt: "2026-08-01T03:00:00Z",
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Collections) != 4 {
		t.Fatalf("collections=%+v", overview.Collections)
	}
	var missing *db.CreatorMetricCollectionState
	for index := range overview.Collections {
		if overview.Collections[index].PlatformCode == "wechat_video" {
			missing = &overview.Collections[index]
		}
	}
	if missing == nil || missing.State != "not_collected" || missing.RangeVerified {
		t.Fatalf("missing video account state=%+v", missing)
	}
	if !strings.Contains(strings.Join(overview.Insights, "\n"), "3/4 个账号") {
		t.Fatalf("insights=%v", overview.Insights)
	}
}

func TestCreatorMetricsOverviewMarksDuplicateBrowserBindingsAndExcludesThemFromCoverage(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 3; index++ {
		identity, err := database.CreateEgoIdentity(fmt.Sprintf("重复数据浏览器%d", index))
		if err != nil {
			t.Fatal(err)
		}
		account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "同一账号", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`UPDATE accounts SET account_identifier='DS911_',login_state='logged_in' WHERE id=?`, account.ID); err != nil {
			t.Fatal(err)
		}
		if index == 1 {
			if _, err := database.UpsertCreatorMetricSnapshot(db.CreatorMetricSnapshotInput{
				AccountID: account.ID, PlatformCode: "douyin", SnapshotDate: "2026-08-01", WindowDays: 7,
				RangeVerified: true, EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{}`,
				CollectedAt: "2026-08-01T03:00:00Z",
			}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}

	overview, err := database.CreatorMetricOverview(7, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	duplicateCount := 0
	for _, item := range overview.Collections {
		if item.ErrorCode == "DUPLICATE_ACCOUNT_BINDING" && item.State == "skipped" {
			duplicateCount++
		}
	}
	if duplicateCount != 2 {
		t.Fatalf("collections=%+v", overview.Collections)
	}
	if !strings.Contains(strings.Join(overview.Insights, "\n"), "1/1 个账号") {
		t.Fatalf("insights=%v", overview.Insights)
	}
}

// profile 快照只证明「当前关注数」，对区间指标一无所知。它当天后写时不能把
// 已验证的七天指标清零，否则账号数据页会显示 0 浏览而概览页仍是真实值。
func TestCreatorProfileSnapshotDoesNotWipeVerifiedDailyMetrics(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("关注数浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音关注", "")
	verified := db.CreatorMetricSnapshotInput{
		AccountID: account.ID, PlatformCode: "douyin", SnapshotDate: "2026-08-01", WindowDays: 7,
		RangeVerified: true, Views: 994, Likes: 20, Comments: 3, Shares: 1, Favorites: 5, Posts: 4,
		EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{}`, CollectedAt: "2026-08-01T01:00:00Z",
	}
	if _, err := database.UpsertCreatorMetricSnapshot(verified, nil); err != nil {
		t.Fatal(err)
	}
	profile := db.CreatorMetricSnapshotInput{
		AccountID: account.ID, PlatformCode: "douyin", SnapshotDate: "2026-08-01", WindowDays: 7,
		RangeVerified: false, Followers: 12345, FollowersVerified: true,
		EvidenceCode: db.CreatorProfileEvidenceCode, RawJSON: `{}`, CollectedAt: "2026-08-01T06:00:00Z",
	}
	if _, err := database.UpsertCreatorMetricSnapshot(profile, nil); err != nil {
		t.Fatal(err)
	}

	var rangeVerified, views, followers int
	if err := database.QueryRow(`SELECT range_verified,views,followers FROM creator_metric_snapshots
		WHERE account_id=? AND snapshot_date='2026-08-01' AND window_days=7`, account.ID).
		Scan(&rangeVerified, &views, &followers); err != nil {
		t.Fatal(err)
	}
	if rangeVerified != 1 || views != 994 {
		t.Fatalf("profile snapshot wiped verified metrics: range_verified=%d views=%d", rangeVerified, views)
	}
	if followers != 12345 {
		t.Fatalf("profile snapshot must still record followers: %d", followers)
	}

	summary, err := database.GetMetricSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Views != 994 || summary.Followers != 12345 {
		t.Fatalf("summary=%+v", summary)
	}
}

// 未验证行之间仍应互相覆盖，否则第二次 profile 更新就丢关注数。
func TestCreatorProfileSnapshotOverwritesEarlierProfileRow(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("关注数浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书关注", "")
	makeProfile := func(followers int64, at string) db.CreatorMetricSnapshotInput {
		return db.CreatorMetricSnapshotInput{
			AccountID: account.ID, PlatformCode: "xhs", SnapshotDate: "2026-08-01", WindowDays: 7,
			RangeVerified: false, Followers: followers, FollowersVerified: true,
			EvidenceCode: db.CreatorProfileEvidenceCode, RawJSON: `{}`, CollectedAt: at,
		}
	}
	if _, err := database.UpsertCreatorMetricSnapshot(makeProfile(100, "2026-08-01T01:00:00Z"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpsertCreatorMetricSnapshot(makeProfile(250, "2026-08-01T05:00:00Z"), nil); err != nil {
		t.Fatal(err)
	}
	var followers int
	if err := database.QueryRow(`SELECT followers FROM creator_metric_snapshots WHERE account_id=?`, account.ID).Scan(&followers); err != nil {
		t.Fatal(err)
	}
	if followers != 250 {
		t.Fatalf("later profile snapshot must win: %d", followers)
	}
}

// GetMetricSummary 必须与 CreatorMetricOverview 同口径：不把别的窗口天数
// 和未验证行当作「每账号最新一天」。
func TestGetMetricSummaryIgnoresNonDefaultWindowAndUnverifiedRows(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, _ := database.CreateEgoIdentity("口径浏览器")
	account, _ := database.CreateAccountForIdentity(identity.ID, "csdn", "CSDN", "")
	base := db.CreatorMetricSnapshotInput{
		AccountID: account.ID, PlatformCode: "csdn", SnapshotDate: "2026-08-01", WindowDays: 7,
		RangeVerified: true, Views: 500, EvidenceCode: "REVIEWED_ANALYTICS_DOM", RawJSON: `{}`,
		CollectedAt: "2026-08-01T01:00:00Z",
	}
	if _, err := database.UpsertCreatorMetricSnapshot(base, nil); err != nil {
		t.Fatal(err)
	}
	// 直接插入一行 30 天窗口，Upsert 不接受非默认窗口。
	if _, err := database.Exec(`INSERT INTO creator_metric_snapshots
		(account_id,platform_code,snapshot_date,window_days,range_verified,views,evidence_code,raw_json,collected_at,created_at,updated_at)
		VALUES (?,?,?,30,1,99999,'REVIEWED_ANALYTICS_DOM','{}',?,?,?)`,
		account.ID, "csdn", "2026-08-01", "2026-08-01T02:00:00Z", "2026-08-01T02:00:00Z", "2026-08-01T02:00:00Z"); err != nil {
		t.Fatal(err)
	}
	summary, err := database.GetMetricSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Views != 500 || summary.Samples != 1 {
		t.Fatalf("summary counted a non-default window: %+v", summary)
	}
}
