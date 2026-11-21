package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

// fakeRunner answers lark-cli invocations from a script keyed by the joined
// arguments, and records every call so a test can prove what was asked.
type fakeRunner struct {
	mu      sync.Mutex
	calls   [][]string
	handler func(args []string) (string, error)
}

func (runner *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	runner.mu.Lock()
	runner.calls = append(runner.calls, append([]string{}, args...))
	runner.mu.Unlock()
	out, err := runner.handler(args)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func (runner *fakeRunner) callCount() int {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return len(runner.calls)
}

func okEnvelope(data string) string {
	return `{"ok":true,"identity":"user","data":` + data + `}`
}

func TestCallSurfacesMissingScope(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) (string, error) {
		return `{"ok":false,"identity":"bot","error":{"type":"authorization","subtype":"app_scope_not_applied","code":99991672,"message":"access denied","missing_scopes":["base:app:create"]}}`, nil
	}}
	client := &Client{Runner: runner, Identity: IdentityBot}
	_, err := client.Call(context.Background(), "base", "+base-create", "--name", "x")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsScopeMissing(err) {
		t.Fatalf("scope failure not recognised: %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 99991672 || len(apiErr.MissingScopes) != 1 {
		t.Fatalf("api error not decoded: %#v", err)
	}
	if !strings.Contains(err.Error(), "base:app:create") {
		t.Fatalf("missing scope not reported: %v", err)
	}
}

func TestCallAppendsIdentityAndJSONFormat(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) (string, error) { return okEnvelope(`{"id":"ou_1"}`), nil }}
	client := &Client{Runner: runner, Identity: IdentityUser}
	if _, err := client.Call(context.Background(), "whoami"); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(runner.calls[0], " ")
	for _, want := range []string{"--as user", "--format json"} {
		if !strings.Contains(args, want) {
			t.Fatalf("call %q missing %q", args, want)
		}
	}
}

func TestCreateContentBaseReadsTokensAndAddsSecondTable(t *testing.T) {
	runner := &fakeRunner{handler: func(args []string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "+base-create"):
			return okEnvelope(`{"base":{"base_token":"bascn123","url":"https://acme.feishu.cn/base/bascn123"},"table":{"table_id":"tblpost"}}`), nil
		case strings.Contains(joined, "+table-create"):
			if !strings.Contains(joined, "账号日报") {
				return "", fmt.Errorf("second table name missing: %s", joined)
			}
			return okEnvelope(`{"table_id":"tblmetrics"}`), nil
		}
		return "", fmt.Errorf("unexpected call: %s", joined)
	}}
	client := &Client{Runner: runner, Identity: IdentityUser}
	base, err := client.CreateContentBase(context.Background(), "Alpha-Nexus 内容库")
	if err != nil {
		t.Fatal(err)
	}
	if base.Token != "bascn123" || base.PostTableID != "tblpost" || base.MetricsTableID != "tblmetrics" {
		t.Fatalf("base=%+v", base)
	}
	if base.URL != "https://acme.feishu.cn/base/bascn123" {
		t.Fatalf("url=%q", base.URL)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls=%d", len(runner.calls))
	}
}

func TestUpsertRecordReturnsExistingIDOnUpdate(t *testing.T) {
	runner := &fakeRunner{handler: func([]string) (string, error) { return okEnvelope(`{"record":{"record_id":"rec_new"}}`), nil }}
	client := &Client{Runner: runner}
	created, err := client.UpsertRecord(context.Background(), "bascn", "tblpost", "", map[string]any{"标题": "x"})
	if err != nil || created != "rec_new" {
		t.Fatalf("created=%q err=%v", created, err)
	}
	updated, err := client.UpsertRecord(context.Background(), "bascn", "tblpost", "rec_old", map[string]any{"标题": "y"})
	if err != nil || updated != "rec_old" {
		t.Fatalf("updated=%q err=%v", updated, err)
	}
	if !strings.Contains(strings.Join(runner.calls[1], " "), "--record-id rec_old") {
		t.Fatalf("update did not address the known record: %v", runner.calls[1])
	}
}

func openExportDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	return database
}

func bindExport(t *testing.T, database *db.DB, enabled bool) {
	t.Helper()
	if err := database.SaveFeishuContentExport(db.FeishuContentExport{
		Enabled: enabled, BaseToken: "bascn", BaseURL: "https://acme.feishu.cn/base/bascn",
		PostTableID: "tblpost", MetricsTableID: "tblmetrics",
	}); err != nil {
		t.Fatal(err)
	}
}

func seedCollectedContent(t *testing.T, database *db.DB) {
	t.Helper()
	identity, err := database.CreateEgoIdentity("飞书内容库")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "品牌主号", "")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := database.UpsertCreatorMetricSnapshot(db.CreatorMetricSnapshotInput{
		AccountID: account.ID, PlatformCode: "douyin", SnapshotDate: "2026-11-11",
		WindowDays: 7, RangeVerified: true, Followers: 1200, Views: 34000,
		Likes: 900, Comments: 40, Shares: 12, Favorites: 30, Posts: 2,
		EvidenceCode: "REVIEWED_ANALYTICS_DOM", CollectedAt: "2026-11-11T10:00:00Z",
	}, []db.CreatorPostMetricInput{
		{ExternalPostID: "a1", Title: "第一条", Views: 30000, Likes: 800},
		{ExternalPostID: "b2", Title: "第二条", Views: 4000, Likes: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil {
		t.Fatal("snapshot not written")
	}
}

func TestRunSkipsWhenDisabledOrUnconfigured(t *testing.T) {
	database := openExportDB(t)
	runner := &fakeRunner{handler: func([]string) (string, error) { return okEnvelope(`{}`), nil }}
	exporter := &Exporter{DB: database, Client: &Client{Runner: runner}, Now: func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) }}

	result, err := exporter.Run(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != "disabled" || runner.callCount() != 0 {
		t.Fatalf("disabled run called feishu: result=%+v calls=%d", result, runner.callCount())
	}

	if err := database.SaveFeishuContentExport(db.FeishuContentExport{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	result, err = exporter.Run(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != "not_configured" || runner.callCount() != 0 {
		t.Fatalf("unconfigured run called feishu: result=%+v calls=%d", result, runner.callCount())
	}
}

func TestRunMirrorsPostsAndMetricsIdempotently(t *testing.T) {
	database := openExportDB(t)
	seedCollectedContent(t, database)
	bindExport(t, database, true)

	var created int
	runner := &fakeRunner{handler: func(args []string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "+record-upsert") && strings.Contains(joined, "--record-id") {
			return okEnvelope(`{"record":{"record_id":"rec_known"}}`), nil
		}
		created++
		return okEnvelope(fmt.Sprintf(`{"record":{"record_id":"rec_%d"}}`, created)), nil
	}}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	exporter := &Exporter{DB: database, Client: &Client{Runner: runner}, Now: func() time.Time { return now }}

	first, err := exporter.Run(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if first.Posts != 2 || first.Metrics != 1 || first.Created != 3 || first.Updated != 0 {
		t.Fatalf("first run=%+v", first)
	}
	if first.Failed != 0 {
		t.Fatalf("first run failed rows: %+v", first)
	}

	// The second sweep must update the same records, not create duplicates.
	second, err := exporter.Run(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if second.Created != 0 || second.Updated != 3 {
		t.Fatalf("second run=%+v", second)
	}
	settings, err := database.FeishuContentExportSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.LastExportAt == "" || !strings.Contains(settings.LastResultJSON, `"updated":3`) {
		t.Fatalf("run result not persisted: %+v", settings)
	}
}

func TestRunRecordsPerRowFailuresWithoutAborting(t *testing.T) {
	database := openExportDB(t)
	seedCollectedContent(t, database)
	bindExport(t, database, true)

	var upserts int
	runner := &fakeRunner{handler: func(args []string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "+record-upsert") {
			upserts++
			if upserts == 1 {
				return `{"ok":false,"error":{"type":"api","subtype":"invalid_request","code":10001,"message":"bad cell"}}`, nil
			}
			return okEnvelope(fmt.Sprintf(`{"record":{"record_id":"rec_%d"}}`, upserts)), nil
		}
		return okEnvelope(`{}`), nil
	}}
	exporter := &Exporter{DB: database, Client: &Client{Runner: runner}, Now: func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) }}
	result, err := exporter.Run(context.Background(), 7)
	if err != nil {
		t.Fatalf("one bad row must not abort the sweep: %v", err)
	}
	if result.Failed != 1 || len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "bad cell") {
		t.Fatalf("result=%+v", result)
	}
	if result.Created != 2 {
		t.Fatalf("remaining rows not written: %+v", result)
	}
}

func TestConfigureBindsBaseAndEnablesMirror(t *testing.T) {
	database := openExportDB(t)
	runner := &fakeRunner{handler: func(args []string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "+base-create") {
			return okEnvelope(`{"base":{"base_token":"bascn9","url":"https://acme.feishu.cn/base/bascn9"},"table":{"table_id":"tblp"}}`), nil
		}
		return okEnvelope(`{"table_id":"tblm"}`), nil
	}}
	exporter := &Exporter{DB: database, Client: &Client{Runner: runner}, Now: time.Now}
	base, err := exporter.Configure(context.Background(), "内容库")
	if err != nil {
		t.Fatal(err)
	}
	if base.Token != "bascn9" {
		t.Fatalf("base=%+v", base)
	}
	settings, err := database.FeishuContentExportSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Enabled || !settings.Configured() || settings.MetricsTableID != "tblm" {
		t.Fatalf("settings=%+v", settings)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(settings.LastResultJSON), &decoded); err != nil {
		t.Fatalf("last result is not JSON: %v", err)
	}
}

func TestPreviewBuildsRowsWithoutCallingFeishu(t *testing.T) {
	database := openExportDB(t)
	seedCollectedContent(t, database)
	runner := &fakeRunner{handler: func([]string) (string, error) { return okEnvelope(`{}`), nil }}
	exporter := &Exporter{DB: database, Client: &Client{Runner: runner}, Now: func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) }}

	preview, err := exporter.Preview(7, 20)
	if err != nil {
		t.Fatal(err)
	}
	if runner.callCount() != 0 {
		t.Fatalf("a preview must not call Feishu: calls=%d", runner.callCount())
	}
	if preview.PostRows != 2 || preview.MetricRows != 1 {
		t.Fatalf("preview counts=%+v", preview)
	}
	if preview.Posts[0]["平台"] != "douyin" || preview.Metrics[0]["账号"] != "品牌主号" {
		t.Fatalf("preview rows missing fields: posts=%+v metrics=%+v", preview.Posts[0], preview.Metrics[0])
	}
}

func TestPreviewWindowHonoursLargeBackfill(t *testing.T) {
	database := openExportDB(t)
	seedCollectedContent(t, database)
	exporter := &Exporter{DB: database, Client: &Client{Runner: &fakeRunner{handler: func([]string) (string, error) { return okEnvelope(`{}`), nil }}}, Now: func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) }}

	// A 120-day window must not be clamped back to the 7-day default.
	preview, err := exporter.Preview(120, 20)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Since != "2026-07-14" {
		t.Fatalf("since=%q, want the requested 120-day window", preview.Since)
	}
	if preview.PostRows != 2 {
		t.Fatalf("post rows=%d", preview.PostRows)
	}
}

func TestPreviewTruncatesWithAnExplicitFlag(t *testing.T) {
	database := openExportDB(t)
	seedCollectedContent(t, database)
	exporter := &Exporter{DB: database, Client: &Client{Runner: &fakeRunner{handler: func([]string) (string, error) { return okEnvelope(`{}`), nil }}}, Now: func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC) }}
	preview, err := exporter.Preview(7, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Posts) != 1 || !preview.Truncated || preview.PostRows != 2 {
		t.Fatalf("preview=%+v", preview)
	}
}

func shPath(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	return path
}

// A CLI that writes a JSON envelope to stdout and still exits non-zero has
// failed: the exit status must win, with stderr kept in the error.
func TestExecRunnerPropagatesNonZeroExitEvenWithStdout(t *testing.T) {
	runner := ExecRunner{Path: shPath(t)}
	out, err := runner.Run(context.Background(), "-c",
		`printf '{"ok":true,"data":{"id":"ou_1"}}\n'; printf 'boom: auth failed\n' >&2; exit 7`)
	if err == nil {
		t.Fatalf("non-zero exit reported as success: out=%q", out)
	}
	if out != nil {
		t.Fatalf("stdout returned alongside a failure: %q", out)
	}
	message := err.Error()
	if !strings.Contains(message, "auth failed") {
		t.Fatalf("stderr summary missing from error: %v", message)
	}
	if !strings.Contains(message, "stdout:") {
		t.Fatalf("stdout excerpt missing, the JSON envelope is no longer diagnosable: %v", message)
	}
}

func TestExecRunnerPropagatesNonZeroExitWithoutStderr(t *testing.T) {
	runner := ExecRunner{Path: shPath(t)}
	out, err := runner.Run(context.Background(), "-c", `printf '{"ok":true,"data":{}}\n'; exit 2`)
	if err == nil {
		t.Fatalf("non-zero exit reported as success: out=%q", out)
	}
	if !strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("exit status missing from error: %v", err)
	}
}

func TestExecRunnerReturnsStdoutOnSuccess(t *testing.T) {
	runner := ExecRunner{Path: shPath(t)}
	out, err := runner.Run(context.Background(), "-c", `printf '{"ok":true,"data":{"id":"ou_1"}}\n'`)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(string(out), `"ou_1"`) {
		t.Fatalf("stdout not returned on success: %q", out)
	}
}
