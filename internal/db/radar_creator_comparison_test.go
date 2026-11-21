package db_test

import (
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

// localDate turns a fixed RFC3339 instant into the calendar date the store
// compares against, so the expectations follow the host time zone instead of
// hard-coding one.
func localDate(t *testing.T, value string) string {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.In(time.Local).Format("2006-01-02")
}

func recordCompareCapture(t *testing.T, database *db.DB, platform, workID, author, observedAt string, likes, collects, shares, comments int64) {
	t.Helper()
	kept, err := database.RecordRadarCapture(db.RadarCapture{
		Platform: platform, WorkID: workID, Author: author, Title: "t-" + workID,
		ObservedAt: observedAt, Likes: likes, Collects: collects, Shares: shares, Comments: comments,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !kept {
		t.Fatalf("capture %s/%s was dropped", platform, workID)
	}
}

func TestRadarCreatorComparisonAggregatesMultipleCreatorsPerDay(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	// 闲吃寿司蛋操心：同一作品两天观测，第二天计数增长；另一作品只出现一天。
	recordCompareCapture(t, database, "douyin", "w1", "闲吃寿司蛋操心", "2026-11-04T13:20:12+08:00", 589, 100, 0, 16)
	recordCompareCapture(t, database, "douyin", "w1", "闲吃寿司蛋操心", "2026-11-05T13:22:14+08:00", 828, 154, 0, 29)
	recordCompareCapture(t, database, "douyin", "w2", "闲吃寿司蛋操心", "2026-11-05T14:00:00+08:00", 10, 1, 2, 3)
	// 慢半拍：只出现一天，用于验证排序与并行聚合。
	recordCompareCapture(t, database, "douyin", "w3", "慢半拍", "2026-11-05T01:20:16+08:00", 129, 8, 0, 8)

	series, err := database.RadarCreatorComparison("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("want 2 creators, got %+v", series)
	}
	byAuthor := map[string]db.CreatorCompareSeries{}
	for _, entry := range series {
		byAuthor[entry.Author] = entry
	}
	xianchi, ok := byAuthor["闲吃寿司蛋操心"]
	if !ok {
		t.Fatalf("missing 闲吃寿司蛋操心 in %+v", series)
	}
	if xianchi.Platform != "douyin" || xianchi.Works != 2 || xianchi.Observations != 3 || len(xianchi.Days) != 2 {
		t.Fatalf("unexpected series: %+v", xianchi)
	}
	day1, day2 := xianchi.Days[0], xianchi.Days[1]
	if day1.Date != localDate(t, "2026-11-04T13:20:12+08:00") || day1.Likes != 589 || day1.Collects != 100 || day1.Comments != 16 || day1.Observations != 1 {
		t.Fatalf("day1=%+v", day1)
	}
	if day2.Date != localDate(t, "2026-11-05T13:22:14+08:00") {
		t.Fatalf("day2 date=%s", day2.Date)
	}
	if day2.Likes != 838 || day2.Collects != 155 || day2.Shares != 2 || day2.Comments != 32 || day2.Observations != 2 {
		t.Fatalf("day2 aggregates must sum both works on that day: %+v", day2)
	}
	slow := byAuthor["慢半拍"]
	if slow.Works != 1 || slow.Observations != 1 || len(slow.Days) != 1 || slow.Days[0].Likes != 129 {
		t.Fatalf("慢半拍=%+v", slow)
	}
}

func TestRadarCreatorComparisonWindowIsInclusiveAndExcludesOutside(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	recordCompareCapture(t, database, "douyin", "w1", "边界博主", "2026-11-03T23:00:00+08:00", 1, 0, 0, 0) // 窗外（前）
	recordCompareCapture(t, database, "douyin", "w1", "边界博主", "2026-11-04T12:00:00+08:00", 2, 0, 0, 0) // 窗口起点
	recordCompareCapture(t, database, "douyin", "w1", "边界博主", "2026-11-05T12:00:00+08:00", 4, 0, 0, 0) // 窗口终点
	recordCompareCapture(t, database, "douyin", "w1", "边界博主", "2026-11-06T01:00:00+08:00", 8, 0, 0, 0) // 窗外（后）

	from := localDate(t, "2026-11-04T12:00:00+08:00")
	to := localDate(t, "2026-11-05T12:00:00+08:00")
	series, err := database.RadarCreatorComparison(from, to, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || len(series[0].Days) != 2 {
		t.Fatalf("want exactly the two in-window days, got %+v", series)
	}
	if series[0].Days[0].Likes != 2 || series[0].Days[1].Likes != 4 {
		t.Fatalf("window bounds must be inclusive: %+v", series[0].Days)
	}
}

func TestRadarCreatorComparisonKeepsGapsWithoutFillingDays(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	recordCompareCapture(t, database, "douyin", "w1", "空档博主", "2026-11-04T12:00:00+08:00", 5, 0, 0, 0)
	recordCompareCapture(t, database, "douyin", "w1", "空档博主", "2026-11-07T12:00:00+08:00", 7, 0, 0, 0)

	series, err := database.RadarCreatorComparison("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || len(series[0].Days) != 2 {
		t.Fatalf("a day without observations must not be invented: %+v", series)
	}
	if series[0].Days[0].Date == series[0].Days[1].Date {
		t.Fatalf("days must stay distinct: %+v", series[0].Days)
	}
}

func TestRadarCreatorComparisonEmptyStoreReturnsEmptySlice(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	series, err := database.RadarCreatorComparison("", "", "")
	if err != nil {
		t.Fatalf("empty store must not error: %v", err)
	}
	if len(series) != 0 {
		t.Fatalf("want empty slice, got %+v", series)
	}
	// 带窗口和平台过滤同样不许崩。
	if series, err = database.RadarCreatorComparison("2026-11-01", "2026-11-30", "xhs"); err != nil || len(series) != 0 {
		t.Fatalf("filtered empty store: series=%+v err=%v", series, err)
	}
}

func TestRadarCreatorComparisonPlatformFilterAndUnnamedSkip(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	recordCompareCapture(t, database, "douyin", "w1", "抖音博主", "2026-11-04T12:00:00+08:00", 3, 0, 0, 0)
	recordCompareCapture(t, database, "xhs", "n1", "小红书博主", "2026-11-04T12:00:00+08:00", 9, 0, 0, 0)
	recordCompareCapture(t, database, "xhs", "n2", "", "2026-11-04T12:00:00+08:00", 4, 0, 0, 0)

	series, err := database.RadarCreatorComparison("", "", "xhs")
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Author != "小红书博主" || series[0].Days[0].Likes != 9 {
		t.Fatalf("platform filter must keep only the named xhs creator: %+v", series)
	}
	all, err := database.RadarCreatorComparison("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range all {
		if entry.Author == "" {
			t.Fatalf("unnamed captures must be skipped, got %+v", all)
		}
	}
	if len(all) != 2 {
		t.Fatalf("want the two named creators, got %+v", all)
	}
}

func TestRadarCreatorComparisonWorksCountsDistinctAcrossDays(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	// 同一作品两天都被看到：Works 记 1，不是 2。
	recordCompareCapture(t, database, "douyin", "w1", "去重博主", "2026-11-04T12:00:00+08:00", 1, 0, 0, 0)
	recordCompareCapture(t, database, "douyin", "w1", "去重博主", "2026-11-05T12:00:00+08:00", 2, 0, 0, 0)

	series, err := database.RadarCreatorComparison("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Works != 1 || series[0].Observations != 2 {
		t.Fatalf("works must count distinct works over the window: %+v", series)
	}
}
