package ui

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/radar"
	"alpha-nexus-egolite/internal/tikhub"
)

func newRadarRunTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	if _, err := tikhub.SaveProviderCredential(dataDir, "synthetic-tikhub-token-123456", tikhub.RegionMainland); err != nil {
		t.Fatal(err)
	}
	if err := tikhub.MarkProviderTest(dataDir, true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tikhub.SaveRadarConfig(dataDir, tikhub.RadarConfig{Niches: []tikhub.RadarNiche{{
		ID: "niche-test", Name: "测试池", Enabled: true,
		SearchTerms: []string{"词一", "词二"}, Platforms: []string{"xhs", "douyin"},
	}}}); err != nil {
		t.Fatal(err)
	}
	return server, dataDir
}

func doRadarRequest(t *testing.T, server *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var input []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		input = encoded
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(input))
	request.RemoteAddr = "127.0.0.1:41000"
	request.Host = "127.0.0.1:17321"
	request.Header.Set("Origin", "http://127.0.0.1:17321")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func TestRadarNicheRunCapsItemsAndExports(t *testing.T) {
	server, dataDir := newRadarRunTestServer(t)
	server.tikhubCallEndpoint = func(ctx context.Context, _ tikhub.EndpointDefinition, params map[string]any) (map[string]any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		items := make([]any, 25)
		for index := range items {
			items[index] = map[string]any{
				"note_id": fmt.Sprintf("note%06d", index),
				"title":   fmt.Sprintf("%v-%d", params["keyword"], index),
			}
		}
		return map[string]any{"code": json.Number("200"), "data": map[string]any{"items": items}}, nil
	}

	response := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/niches/niche-test/run", map[string]any{
		"platform": "xhs", "terms": []string{"词一"}, "confirm": true,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", response.Code, response.Body.String())
	}
	var run radar.Run
	if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.State != radar.RunStateSucceeded || run.Succeeded != 1 || run.Failed != 0 {
		t.Fatalf("unexpected run: %+v", run)
	}
	if run.ItemCap != tikhub.RadarXHSItemCap {
		t.Fatalf("xhs cap = %d, want %d", run.ItemCap, tikhub.RadarXHSItemCap)
	}
	if run.Items != 25 || run.Kept != tikhub.RadarXHSItemCap {
		t.Fatalf("item totals = items %d kept %d", run.Items, run.Kept)
	}
	if run.Calls[0].CallID <= 0 || run.Calls[0].RawPath == "" {
		t.Fatalf("call must reference its persisted snapshot: %+v", run.Calls[0])
	}
	if run.EstimatedCostUSD != 0 || !strings.Contains(run.CostBasis, "未登记") {
		t.Fatalf("xhs price is not registered and must not be invented: %+v", run)
	}

	summaryResponse := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/summary", nil)
	if summaryResponse.Code != http.StatusOK {
		t.Fatalf("summary status=%d", summaryResponse.Code)
	}
	var summary trendRadarSummary
	if err := json.Unmarshal(summaryResponse.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, niche := range summary.Niches {
		if niche.ID != "niche-test" {
			continue
		}
		found = true
		if niche.ResultCount != tikhub.RadarXHSItemCap {
			t.Fatalf("summary kept %d signals, want the xhs cap %d", niche.ResultCount, tikhub.RadarXHSItemCap)
		}
		if niche.LastRun == nil || niche.LastRun.ID != run.ID {
			t.Fatalf("summary must expose the last run: %+v", niche.LastRun)
		}
		if niche.PlatformLimits["xhs"] != tikhub.RadarXHSItemCap {
			t.Fatalf("summary must expose platform limits: %+v", niche.PlatformLimits)
		}
	}
	if !found {
		t.Fatal("test niche missing from summary")
	}

	export := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/export?niche_id=niche-test&format=csv", nil)
	if export.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", export.Code, export.Body.String())
	}
	body := export.Body.String()
	if !strings.HasPrefix(body, "\uFEFF") {
		t.Fatal("csv export must start with a UTF-8 BOM")
	}
	lines := strings.Split(strings.TrimSpace(strings.TrimPrefix(body, "\uFEFF")), "\n")
	if len(lines) != tikhub.RadarXHSItemCap+1 {
		t.Fatalf("csv rows = %d, want header + %d", len(lines), tikhub.RadarXHSItemCap)
	}
	if !strings.Contains(lines[0], "作品链接") || !strings.Contains(lines[0], "抓取时间") || !strings.Contains(lines[0], "来源池") {
		t.Fatalf("csv header missing hand-off columns: %s", lines[0])
	}
	if !strings.Contains(lines[0], "搜索词池") || !strings.Contains(lines[0], "作品ID") {
		t.Fatalf("csv header missing pool / work id columns: %s", lines[0])
	}
	// 每条作品都要有去重用的作品 ID；搜索词不在任何池子里时写「未归属」。
	for _, line := range lines[1:] {
		fields := strings.Split(line, ",")
		if len(fields) < 17 {
			t.Fatalf("csv row has %d fields, want at least 17: %s", len(fields), line)
		}
		if strings.TrimSpace(fields[16]) == "" {
			t.Fatalf("csv row missing work id: %s", line)
		}
	}
	if got := export.Header().Get("X-Nexus-Export-Rows"); got != strconv.Itoa(tikhub.RadarXHSItemCap) {
		t.Fatalf("export row header = %q", got)
	}

	// The saved config on disk is untouched until the person edits limits.
	config, err := tikhub.LoadRadarConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if config.Niches[0].ItemLimit("xhs") != tikhub.RadarXHSItemCap {
		t.Fatalf("unexpected persisted limits: %+v", config.Niches[0].PlatformLimits)
	}
}

func TestRadarNicheRunStopsAfterFailureAndReportsIt(t *testing.T) {
	server, _ := newRadarRunTestServer(t)
	server.tikhubCallEndpoint = func(context.Context, tikhub.EndpointDefinition, map[string]any) (map[string]any, error) {
		return nil, errors.New("provider returned 402: insufficient credits")
	}
	response := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/niches/niche-test/run", map[string]any{
		"platform": "douyin", "all": true, "confirm": true,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", response.Code, response.Body.String())
	}
	var run radar.Run
	if err := json.Unmarshal(response.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.State != radar.RunStateFailed || run.Failed != 1 {
		t.Fatalf("unexpected failed run: %+v", run)
	}
	if run.TotalCalls != 1 || run.Skipped != 1 {
		t.Fatalf("a failing provider must stop the round after one call: %+v", run)
	}
	if run.Calls[0].ErrorMessage == "" || run.Calls[0].ErrorCode != "provider_request_failed" {
		t.Fatalf("failure reason must be recorded: %+v", run.Calls[0])
	}
}

func TestRadarNicheRunRequiresConfirmationAndKnownTerms(t *testing.T) {
	server, _ := newRadarRunTestServer(t)
	server.tikhubCallEndpoint = func(context.Context, tikhub.EndpointDefinition, map[string]any) (map[string]any, error) {
		t.Fatal("no paid call may run without confirmation")
		return nil, nil
	}
	unconfirmed := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/niches/niche-test/run", map[string]any{
		"platform": "xhs", "terms": []string{"词一"},
	})
	if unconfirmed.Code != http.StatusBadRequest || !strings.Contains(unconfirmed.Body.String(), "confirmation_required") {
		t.Fatalf("unconfirmed run must be refused: %d %s", unconfirmed.Code, unconfirmed.Body.String())
	}
	unknownTerm := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/niches/niche-test/run", map[string]any{
		"platform": "xhs", "terms": []string{"不在配置里的词"}, "confirm": true,
	})
	if unknownTerm.Code != http.StatusBadRequest || !strings.Contains(unknownTerm.Body.String(), "invalid_terms") {
		t.Fatalf("free-form terms must be refused: %d %s", unknownTerm.Code, unknownTerm.Body.String())
	}
	wrongPlatform := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/niches/niche-test/run", map[string]any{
		"platform": "youtube", "terms": []string{"词一"}, "confirm": true,
	})
	if wrongPlatform.Code != http.StatusBadRequest || !strings.Contains(wrongPlatform.Body.String(), "platform_not_allowed") {
		t.Fatalf("platform outside the niche must be refused: %d %s", wrongPlatform.Code, wrongPlatform.Body.String())
	}
}

func TestRadarNicheLimitsUpdatePersists(t *testing.T) {
	server, dataDir := newRadarRunTestServer(t)
	response := doRadarRequest(t, server, http.MethodPut, "/api/trend-radar/niches/niche-test/limits", map[string]any{
		"platform_limits": map[string]int{"xhs": 5},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("limits status=%d body=%s", response.Code, response.Body.String())
	}
	config, err := tikhub.LoadRadarConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.Niches[0].ItemLimit("xhs"); got != 5 {
		t.Fatalf("xhs limit = %d, want 5", got)
	}
	if got := config.Niches[0].ItemLimit("douyin"); got != tikhub.RadarDefaultItemCap {
		t.Fatalf("douyin limit = %d, want default %d", got, tikhub.RadarDefaultItemCap)
	}
	invalid := doRadarRequest(t, server, http.MethodPut, "/api/trend-radar/niches/niche-test/limits", map[string]any{
		"platform_limits": map[string]int{"xhs": 0},
	})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("zero limit must be refused: %d %s", invalid.Code, invalid.Body.String())
	}
}

// TestTrendRadarWritesRejectCrossOrigin pins the CSRF guard on the trend radar
// write endpoints that shipped without one. With auth off (the default) any web
// page could POST a simple request to these paths, so a foreign Origin must be
// refused before the body is decoded and before anything is written or spent.
func TestTrendRadarWritesRejectCrossOrigin(t *testing.T) {
	server, dataDir := newRadarRunTestServer(t)

	for _, test := range []struct {
		name   string
		method string
		path   string
		body   map[string]any
	}{
		{"config", http.MethodPut, "/api/trend-radar/config", map[string]any{"niches": []any{}}},
		{"assign-keyword", http.MethodPost, "/api/trend-radar/assign-keyword", map[string]any{"terms": []string{"词一"}, "target_niche_id": "niche-test"}},
		{"creator-add", http.MethodPost, "/api/trend-radar/creators", map[string]any{"platform": "douyin", "user_id": "some-user", "name": "某人"}},
		{"cover-backfill", http.MethodPost, "/api/trend-radar/covers/backfill", map[string]any{"confirm": true}},
	} {
		encoded, err := json.Marshal(test.body)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(test.method, test.path, bytes.NewReader(encoded))
		request.RemoteAddr = "127.0.0.1:41000"
		request.Host = "127.0.0.1:17321"
		request.Header.Set("Origin", "https://attacker.example")
		request.Header.Set("Content-Type", "text/plain")
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "origin_denied") {
			t.Fatalf("%s cross-origin status=%d body=%s", test.name, recorder.Code, recorder.Body.String())
		}
	}

	// 反证：同源写入仍必须可用，否则等于把功能锁死。
	sameOrigin := doRadarRequest(t, server, http.MethodPut, "/api/trend-radar/config", map[string]any{
		"niches": []any{map[string]any{
			"id": "niche-test", "name": "测试池", "enabled": true,
			"search_terms": []string{"词一"}, "platforms": []string{"xhs"},
		}},
	})
	if sameOrigin.Code != http.StatusOK {
		t.Fatalf("same-origin config status=%d body=%s", sameOrigin.Code, sameOrigin.Body.String())
	}
	config, err := tikhub.LoadRadarConfig(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Niches) != 1 || len(config.Niches[0].SearchTerms) != 1 {
		t.Fatalf("cross-origin request must not have rewritten the config: %+v", config.Niches)
	}
}

// TestRadarNicheRunIsMutuallyExclusivePerNiche covers the credit guard: one
// round can spend up to maxRadarRunCalls paid calls, so a second concurrent run
// for the same niche must be refused with 409 instead of double-billing the same
// search terms. The lock must also be released when the first round finishes.
func TestRadarNicheRunIsMutuallyExclusivePerNiche(t *testing.T) {
	server, _ := newRadarRunTestServer(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	server.tikhubCallEndpoint = func(context.Context, tikhub.EndpointDefinition, map[string]any) (map[string]any, error) {
		once.Do(func() { close(entered) })
		// 有界等待：万一互斥失效、第二个请求也进到这一层，测试也不能挂死。
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
		return map[string]any{"code": json.Number("200"), "data": map[string]any{"items": []any{
			map[string]any{"note_id": "note-1", "title": "标题"},
		}}}, nil
	}

	first := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		first <- doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/niches/niche-test/run", map[string]any{
			"platform": "xhs", "terms": []string{"词一"}, "confirm": true,
		})
	}()
	<-entered

	second := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/niches/niche-test/run", map[string]any{
		"platform": "xhs", "terms": []string{"词二"}, "confirm": true,
	})
	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), "AGENT_RUN_ACTIVE") {
		t.Fatalf("concurrent run status=%d body=%s", second.Code, second.Body.String())
	}

	close(release)
	if recorder := <-first; recorder.Code != http.StatusOK {
		t.Fatalf("first run status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// 第一轮结束后锁必须释放，否则该赛道再也抓不了。
	after := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/niches/niche-test/run", map[string]any{
		"platform": "xhs", "terms": []string{"词一"}, "confirm": true,
	})
	if after.Code != http.StatusOK {
		t.Fatalf("run after release status=%d body=%s", after.Code, after.Body.String())
	}
}
func TestRadarRunRetryRefillsOnlyFailedCalls(t *testing.T) {
	server, _ := newRadarRunTestServer(t)
	attempts := map[string]int{}
	server.tikhubCallEndpoint = func(ctx context.Context, _ tikhub.EndpointDefinition, params map[string]any) (map[string]any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		term := fmt.Sprint(params["keyword"])
		attempts[term]++
		if term == "词二" && attempts[term] == 1 {
			return nil, errors.New("provider hiccup")
		}
		return map[string]any{"code": json.Number("200"), "data": map[string]any{"items": []any{
			map[string]any{"note_id": fmt.Sprintf("note%s%04d", term, attempts[term]), "title": term + " 作品"},
		}}}, nil
	}
	round := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/niches/niche-test/run", map[string]any{
		"platform": "xhs", "terms": []string{"词一", "词二"}, "confirm": true,
	})
	if round.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", round.Code, round.Body.String())
	}
	var first radar.Run
	if err := json.Unmarshal(round.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.State != radar.RunStatePartial || first.TotalCalls != 2 || first.Succeeded != 1 || first.Failed != 1 || first.Skipped != 0 {
		t.Fatalf("round after one failure = %+v", first)
	}
	if attempts["词一"] != 1 || attempts["词二"] != 1 {
		t.Fatalf("first round attempts = %v", attempts)
	}

	retry := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/runs/"+first.ID+"/retry", map[string]any{"confirm": true})
	if retry.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	var payload struct {
		OK             bool      `json:"ok"`
		Run            radar.Run `json:"run"`
		RetriedTerms   []string  `json:"retried_terms"`
		RecoveredTerms []string  `json:"recovered_terms"`
		PendingTerms   []string  `json:"pending_terms"`
	}
	if err := json.Unmarshal(retry.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	// 只补失败项：成功过的词一次都没有再调用。
	if attempts["词一"] != 1 {
		t.Fatalf("successful term must not be re-called: %v", attempts)
	}
	if attempts["词二"] != 2 {
		t.Fatalf("failed term must be retried exactly once: %v", attempts)
	}
	if !payload.OK || len(payload.RetriedTerms) != 1 || payload.RetriedTerms[0] != "词二" {
		t.Fatalf("retry payload = %+v", payload)
	}
	if len(payload.RecoveredTerms) != 1 || payload.RecoveredTerms[0] != "词二" || len(payload.PendingTerms) != 0 {
		t.Fatalf("retry outcome = %+v", payload)
	}
	if payload.Run.State != radar.RunStateSucceeded || payload.Run.TotalCalls != 3 ||
		payload.Run.Succeeded != 2 || payload.Run.Failed != 0 || payload.Run.Skipped != 0 {
		t.Fatalf("run after retry = %+v", payload.Run)
	}
	if payload.Run.LastRetryAt == "" || payload.Run.FinishedAt != first.FinishedAt {
		t.Fatalf("retry must keep finished_at and stamp last_retry_at: %+v", payload.Run)
	}
	callsPerTerm := map[string]int{}
	for _, call := range payload.Run.Calls {
		callsPerTerm[call.Term]++
	}
	if callsPerTerm["词一"] != 1 || callsPerTerm["词二"] != 2 {
		t.Fatalf("call ledger = %v", callsPerTerm)
	}

	// 页面读的是数据库那条路径：同样的数字，包括新增的 skipped 列。
	listed := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/runs?limit=10", nil)
	var listPayload struct {
		Runs  []radar.Run `json:"runs"`
		Store string      `json:"store"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listPayload); err != nil {
		t.Fatal(err)
	}
	if listPayload.Store != "sqlite" || len(listPayload.Runs) != 1 {
		t.Fatalf("stored runs = %+v", listPayload)
	}
	if listPayload.Runs[0].TotalCalls != 3 || listPayload.Runs[0].Succeeded != 2 ||
		listPayload.Runs[0].Failed != 0 || listPayload.Runs[0].Skipped != 0 {
		t.Fatalf("stored run totals = %+v", listPayload.Runs[0])
	}
	if listPayload.Runs[0].LastRetryAt == "" {
		t.Fatalf("the SQLite mirror must carry the retry stamp: %+v", listPayload.Runs[0])
	}

	// 没有失败项了：再点一次必须被拒，且不许再调用 provider。
	again := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/runs/"+first.ID+"/retry", map[string]any{"confirm": true})
	if again.Code != http.StatusConflict || !strings.Contains(again.Body.String(), "nothing_to_retry") {
		t.Fatalf("second retry must be refused: %d %s", again.Code, again.Body.String())
	}
	if attempts["词一"] != 1 || attempts["词二"] != 2 {
		t.Fatalf("refused retry must not call the provider: %v", attempts)
	}
}

func TestRadarRunRetryGuards(t *testing.T) {
	server, dataDir := newRadarRunTestServer(t)
	missing := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/runs/run-20260101t000009-nope/retry", map[string]any{"confirm": true})
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing run status=%d body=%s", missing.Code, missing.Body.String())
	}
	noConfirm := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/runs/run-20260101t000009-nope/retry", map[string]any{})
	if noConfirm.Code != http.StatusBadRequest || !strings.Contains(noConfirm.Body.String(), "confirmation_required") {
		t.Fatalf("retry must require confirmation: %d %s", noConfirm.Code, noConfirm.Body.String())
	}
	clean := radar.Run{
		ID: "run-20260101t000009-clean", NicheID: "niche-test", NicheName: "测试池", Platform: "xhs",
		EndpointID: "xhs.note_search", Terms: []string{"词一"},
	}
	if _, err := radar.StartRun(dataDir, clean); err != nil {
		t.Fatal(err)
	}
	if _, err := radar.AppendRunCall(dataDir, clean.ID, radar.RunCall{Term: "词一", Status: "succeeded", ItemCount: 3, KeptCount: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := radar.FinishRun(dataDir, clean.ID); err != nil {
		t.Fatal(err)
	}
	nothing := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/runs/"+clean.ID+"/retry", map[string]any{"confirm": true})
	if nothing.Code != http.StatusConflict || !strings.Contains(nothing.Body.String(), "nothing_to_retry") {
		t.Fatalf("a round without failures must not be retried: %d %s", nothing.Code, nothing.Body.String())
	}
	running := radar.Run{
		ID: "run-20260101t000009-runng", NicheID: "niche-test", Platform: "xhs",
		EndpointID: "xhs.note_search", Terms: []string{"词一"},
	}
	if _, err := radar.StartRun(dataDir, running); err != nil {
		t.Fatal(err)
	}
	busy := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/runs/"+running.ID+"/retry", map[string]any{"confirm": true})
	if busy.Code != http.StatusConflict || !strings.Contains(busy.Body.String(), "run_running") {
		t.Fatalf("a running round must not be retried: %d %s", busy.Code, busy.Body.String())
	}
}

func TestRadarExportSplitsBatchPoolWorkIDAndTags(t *testing.T) {
	server, _ := newRadarRunTestServer(t)
	server.tikhubCallEndpoint = func(ctx context.Context, _ tikhub.EndpointDefinition, params map[string]any) (map[string]any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return map[string]any{"code": json.Number("200"), "data": map[string]any{"business_data": []any{
			map[string]any{"data": map[string]any{"aweme_info": map[string]any{
				"aweme_id": "7300000000000000001",
				"desc":     fmt.Sprintf("%v 的抖音作品", params["keyword"]),
				"text_extra": []any{
					map[string]any{"hashtag_name": "编程"},
					map[string]any{"hashtag_name": "AI工具"},
				},
				"cha_list":   []any{map[string]any{"cha_name": "编程"}},
				"statistics": map[string]any{"play_count": json.Number("1200"), "digg_count": json.Number("34")},
			}}},
		}}}, nil
	}
	round := doRadarRequest(t, server, http.MethodPost, "/api/trend-radar/niches/niche-test/run", map[string]any{
		"platform": "douyin", "terms": []string{"词一"}, "confirm": true,
	})
	if round.Code != http.StatusOK {
		t.Fatalf("douyin run status=%d body=%s", round.Code, round.Body.String())
	}
	var run radar.Run
	if err := json.Unmarshal(round.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	export := doRadarRequest(t, server, http.MethodGet, "/api/trend-radar/export?niche_id=niche-test&format=csv", nil)
	if export.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", export.Code, export.Body.String())
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(export.Body.String(), "\uFEFF"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("csv rows = %d, want header + 1 row: %v", len(records), records)
	}
	column := map[string]int{}
	for index, name := range records[0] {
		column[name] = index
	}
	for _, name := range []string{"批次", "搜索词池", "作品ID", "标签"} {
		if _, ok := column[name]; !ok {
			t.Fatalf("export header is missing %q: %v", name, records[0])
		}
	}
	row := records[1]
	want := map[string]string{
		"批次":   run.ID,
		"搜索词池": "测试池",
		"作品ID": "7300000000000000001",
		"标签":   "编程、AI工具",
	}
	for name, value := range want {
		if got := row[column[name]]; got != value {
			t.Fatalf("export %s = %q, want %q (row=%v)", name, got, value, row)
		}
	}
}

func TestRadarPlatformTagsReadOnlyProviderTags(t *testing.T) {
	douyin := map[string]interface{}{"aweme_info": map[string]interface{}{
		"text_extra": []interface{}{
			map[string]interface{}{"hashtag_name": "编程"},
			map[string]interface{}{"hashtag_name": "编程"},
		},
		"cha_list": []interface{}{map[string]interface{}{"cha_name": "AI"}},
	}}
	if got := strings.Join(radarPlatformTags("douyin", douyin), "、"); got != "编程、AI" {
		t.Fatalf("douyin tags = %q", got)
	}
	xPost := map[string]interface{}{"entities": map[string]interface{}{"hashtags": []interface{}{
		map[string]interface{}{"text": "buildinpublic"},
		map[string]interface{}{"text": "#独立开发"},
	}}}
	if got := strings.Join(radarPlatformTags("x", xPost), "、"); got != "buildinpublic、独立开发" {
		t.Fatalf("x tags = %q", got)
	}
	xhsNote := map[string]interface{}{"tag_info": map[string]interface{}{"title": "副业"}}
	if got := strings.Join(radarPlatformTags("xhs", xhsNote), "、"); got != "副业" {
		t.Fatalf("xhs tags = %q", got)
	}
	if got := radarPlatformTags("youtube", map[string]interface{}{"title": "视频"}); len(got) != 0 {
		t.Fatalf("platforms without tags must stay empty: %v", got)
	}
	empty := map[string]interface{}{"aweme_info": map[string]interface{}{
		"text_extra": []interface{}{map[string]interface{}{}},
	}}
	if got := radarPlatformTags("douyin", empty); len(got) != 0 {
		t.Fatalf("entries without a hashtag name must not produce a tag: %v", got)
	}
}
