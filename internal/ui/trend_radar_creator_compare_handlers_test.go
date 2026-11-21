package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
)

func newCreatorCompareTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server, dataDir
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

// writeCreatorProfileSnapshot archives one douyin creator_profile response the
// way the collector does, so the handler reads it exactly like production data.
func writeCreatorProfileSnapshot(t *testing.T, dataDir, dirName, nickname string, followers int64, fetchedAt time.Time) {
	t.Helper()
	snapshotDir := filepath.Join(dataDir, "trend-radar", "creators", "douyin", dirName, "snapshots")
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		t.Fatal(err)
	}
	snapshot := map[string]any{
		"schema_version": 1,
		"call_id":        1,
		"platform":       "douyin",
		"endpoint_id":    "douyin.creator_profile",
		"capability":     "creator_profile",
		"fetched_at":     fetchedAt.UTC().Format(time.RFC3339Nano),
		"response": map[string]any{
			"code": 200,
			"data": map[string]any{
				"user": map[string]any{
					"nickname":       nickname,
					"follower_count": followers,
				},
			},
		},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, "1_creator_profile.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func decodeCompareResponse(t *testing.T, body []byte) creatorCompareResponse {
	t.Helper()
	var response creatorCompareResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode %s: %v", string(body), err)
	}
	return response
}

func compareEntryByAuthor(t *testing.T, response creatorCompareResponse, author string) creatorCompareEntry {
	t.Helper()
	for _, entry := range response.Creators {
		if entry.Author == author {
			return entry
		}
	}
	t.Fatalf("missing creator %q in %+v", author, response.Creators)
	return creatorCompareEntry{}
}

func TestTrendRadarCreatorsCompareRejectsInvalidDays(t *testing.T) {
	server, _ := newCreatorCompareTestServer(t)
	for _, raw := range []string{"5", "0", "-1", "abc", "31"} {
		recorder := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/creators/compare?days="+raw, nil)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("days=%s must be rejected, got %d %s", raw, recorder.Code, recorder.Body.String())
		}
	}
}

func TestTrendRadarCreatorsCompareDefaultWindow(t *testing.T) {
	server, _ := newCreatorCompareTestServer(t)
	recorder := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/creators/compare", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeCompareResponse(t, recorder.Body.Bytes())
	if response.Window.Days != 30 {
		t.Fatalf("default window must be 30 days: %+v", response.Window)
	}
	now := time.Now()
	if response.Window.To != now.Format("2006-01-02") {
		t.Fatalf("to=%s want %s", response.Window.To, now.Format("2006-01-02"))
	}
	if response.Window.From != now.AddDate(0, 0, -29).Format("2006-01-02") {
		t.Fatalf("from=%s want %s", response.Window.From, now.AddDate(0, 0, -29).Format("2006-01-02"))
	}
	// 空库：200、empty=true、creators 是空数组而不是 null。
	if !response.Empty || response.Creators == nil || len(response.Creators) != 0 {
		t.Fatalf("empty store must return empty creators array: %+v", response)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["creators"]) != "[]" {
		t.Fatalf("creators must marshal as []: %s", string(raw["creators"]))
	}
}

func TestTrendRadarCreatorsCompareSeriesFollowerPointsAndInsufficiency(t *testing.T) {
	server, dataDir := newCreatorCompareTestServer(t)
	day1 := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	day2 := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	observed1 := day1 + "T10:00:00+08:00"
	observed2 := day2 + "T10:00:00+08:00"
	recordCompareCapture(t, server.db, "douyin", "w1", "闲吃寿司蛋操心", observed1, 589, 100, 0, 16)
	recordCompareCapture(t, server.db, "douyin", "w1", "闲吃寿司蛋操心", observed2, 828, 154, 0, 29)
	recordCompareCapture(t, server.db, "douyin", "w2", "慢半拍", observed2, 129, 8, 0, 8)

	// 闲吃寿司蛋操心 有一份真实资料快照（落在 day2 之外的 day1），慢半拍没有。
	fetched, err := time.Parse(time.RFC3339, day1+"T18:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	writeCreatorProfileSnapshot(t, dataDir, "MS4wLjABAAAA-test", "闲吃寿司蛋操心", 4241, fetched)

	recorder := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/creators/compare?days=7", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeCompareResponse(t, recorder.Body.Bytes())
	if response.Empty || len(response.Creators) != 2 {
		t.Fatalf("want two creators, got %+v", response)
	}
	xianchi := compareEntryByAuthor(t, response, "闲吃寿司蛋操心")
	if xianchi.Key != "douyin:闲吃寿司蛋操心" || xianchi.Platform != "douyin" {
		t.Fatalf("key/platform: %+v", xianchi)
	}
	if xianchi.Works != 1 || xianchi.Observations != 2 || len(xianchi.Days) != 2 {
		t.Fatalf("series shape: %+v", xianchi)
	}
	if xianchi.FollowerPoints != 1 || !xianchi.InsufficientFollowers || xianchi.InsufficientSeries {
		t.Fatalf("flags: %+v", xianchi)
	}
	first := xianchi.Days[0]
	if first.Date != day1 || first.Followers == nil || *first.Followers != 4241 {
		t.Fatalf("day1 follower point must be the archived 4241: %+v", first)
	}
	if first.Likes != 589 || first.Collects != 100 || first.Comments != 16 || first.Observations != 1 {
		t.Fatalf("day1 interactions: %+v", first)
	}
	if second := xianchi.Days[1]; second.Followers != nil {
		t.Fatalf("day2 has no snapshot and must stay null: %+v", second)
	}

	slow := compareEntryByAuthor(t, response, "慢半拍")
	if slow.FollowerPoints != 0 || slow.Days[0].Followers != nil {
		t.Fatalf("慢半拍 has no profile snapshot and must report null followers: %+v", slow)
	}
	if !slow.InsufficientSeries {
		t.Fatalf("one observed day must be flagged insufficient: %+v", slow)
	}
}

func TestTrendRadarCreatorsCompareFollowerOnlyCreatorAppears(t *testing.T) {
	server, dataDir := newCreatorCompareTestServer(t)
	fetched := time.Now().AddDate(0, 0, -1)
	writeCreatorProfileSnapshot(t, dataDir, "MS4wLjABAAAA-arthur", "19岁实战派Arthur", 17895, fetched)

	recorder := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/creators/compare?days=7", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeCompareResponse(t, recorder.Body.Bytes())
	arthur := compareEntryByAuthor(t, response, "19岁实战派Arthur")
	if arthur.FollowerPoints != 1 || arthur.Observations != 0 {
		t.Fatalf("follower-only creator: %+v", arthur)
	}
	if !arthur.InsufficientFollowers || !arthur.InsufficientSeries {
		t.Fatalf("single follower point must be flagged: %+v", arthur)
	}
	day := arthur.Days[0]
	if day.Followers == nil || *day.Followers != 17895 || day.Observations != 0 {
		t.Fatalf("follower day: %+v", day)
	}
}

func TestTrendRadarCreatorsCompareSkipsUnreadableSnapshots(t *testing.T) {
	server, dataDir := newCreatorCompareTestServer(t)
	// 快照存在但读不到粉丝数（响应形态不认识）→ 不许编一个值出来。
	snapshotDir := filepath.Join(dataDir, "trend-radar", "creators", "douyin", "MS4wLjABAAAA-null", "snapshots")
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`{"platform":"douyin","fetched_at":%q,"response":{"data":{"user":{"nickname":"读不到粉丝"}}}}`, time.Now().UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(filepath.Join(snapshotDir, "1_creator_profile.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/creators/compare?days=7", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeCompareResponse(t, recorder.Body.Bytes())
	if len(response.Creators) != 0 || !response.Empty {
		t.Fatalf("unreadable snapshot must not create a creator: %+v", response)
	}
}
