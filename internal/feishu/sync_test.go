package feishu

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCLI writes a script that records its stdin and answers with two record ids.
func fakeCLI(t *testing.T) (binary string, captured string) {
	t.Helper()
	dir := t.TempDir()
	captured = filepath.Join(dir, "request.json")
	binary = filepath.Join(dir, "lark-cli")
	script := "#!/bin/sh\n" +
		"cat > " + captured + "\n" +
		`printf '{"ok":true,"identity":"bot","data":{"records":[{"record_id":"recA"},{"record_id":"recB"}]}}'` + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return binary, captured
}

// The Base batch_create endpoint requires each record to be wrapped in
// {"fields": {...}}. Sending bare field maps is rejected with
// "field validation failed", so this asserts the exact wire shape.
func TestBatchCreateWrapsFields(t *testing.T) {
	binary, captured := fakeCLI(t)
	records := []map[string]any{
		{"名称": "主账号", "平台": "douyin"},
		{"名称": "副账号", "平台": "xiaohongshu"},
	}

	ids, err := batchCreate(context.Background(), binary, "appToken", "tbl1", records)
	if err != nil {
		t.Fatalf("batchCreate: %v", err)
	}
	if len(ids) != 2 || ids[0] != "recA" || ids[1] != "recB" {
		t.Fatalf("ids = %v, want [recA recB]", ids)
	}

	raw, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("read captured request: %v", err)
	}
	var payload struct {
		Records []struct {
			Fields map[string]any `json:"fields"`
		} `json:"records"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal request: %v (%s)", err, raw)
	}
	if len(payload.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(payload.Records))
	}
	if got := payload.Records[0].Fields["名称"]; got != "主账号" {
		t.Fatalf("fields were not wrapped under \"fields\": %s", raw)
	}
}

func TestFieldsHashIsStableAndSensitive(t *testing.T) {
	base := map[string]any{"名称": "主账号", "状态": "logged_in"}
	if fieldsHash(base) == "" {
		t.Fatal("hash must not be empty")
	}
	same := map[string]any{"状态": "logged_in", "名称": "主账号"}
	if fieldsHash(base) != fieldsHash(same) {
		t.Fatal("map order must not change the hash")
	}
	changed := map[string]any{"名称": "主账号", "状态": "logged_out"}
	if fieldsHash(base) == fieldsHash(changed) {
		t.Fatal("a changed field must change the hash")
	}
}

// batch_update must send record ids alongside fields, and a rejected call must
// be an error the caller turns into "retry later".
func TestBatchUpdatePayload(t *testing.T) {
	binary, captured := fakeCLI(t)
	records := []map[string]any{
		{"record_id": "recA", "fields": map[string]any{"名称": "主账号"}},
	}

	if err := batchUpdate(context.Background(), binary, "appToken", "tbl1", records); err != nil {
		t.Fatalf("batchUpdate: %v", err)
	}
	raw, err := os.ReadFile(captured)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Records []struct {
			RecordID string         `json:"record_id"`
			Fields   map[string]any `json:"fields"`
		} `json:"records"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, raw)
	}
	if len(payload.Records) != 1 || payload.Records[0].RecordID != "recA" {
		t.Fatalf("record id lost: %s", raw)
	}
	if payload.Records[0].Fields["名称"] != "主账号" {
		t.Fatalf("fields lost: %s", raw)
	}
}

// A rejected call must surface the CLI error body, otherwise the UI can only
// show "exit status 1" and the operator cannot tell what failed.
func TestBatchCreateSurfacesError(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "lark-cli")
	script := "#!/bin/sh\ncat > /dev/null\n" +
		`printf '{"ok":false,"error":{"type":"api","message":"field validation failed"}}'` + "\n" +
		"exit 1\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	_, err := batchCreate(context.Background(), binary, "appToken", "tbl1", []map[string]any{{"名称": "x"}})
	if err == nil {
		t.Fatal("expected error for rejected batch_create")
	}
	if got := err.Error(); !strings.Contains(got, "field validation failed") {
		t.Fatalf("error = %q, want it to include the CLI message", got)
	}
}

// 模块 Query 自带 LIMIT，最新那批永远占住名额；已同步的会被跳过，更早的行
// 原本永远轮不到。offsetQuery 让同步能向后补齐。
func TestOffsetQueryAppendsOffsetToExistingLimit(t *testing.T) {
	module := moduleDef{
		ID:    "knowledge",
		Query: "SELECT id FROM knowledge_documents ORDER BY updated_at DESC LIMIT 300",
	}
	if got := offsetQuery(module, 0); got != module.Query {
		t.Fatalf("offset 0 must leave the query untouched, got %q", got)
	}
	got := offsetQuery(module, 600)
	want := "SELECT id FROM knowledge_documents ORDER BY updated_at DESC LIMIT 300 OFFSET 600"
	if got != want {
		t.Fatalf("offsetQuery = %q, want %q", got, want)
	}
	// 没有 LIMIT 的模块无法安全偏移，必须原样返回，否则会跨轮重复读。
	noLimit := moduleDef{ID: "bots", Query: "SELECT id FROM agent_bots"}
	if got := offsetQuery(noLimit, 300); got != noLimit.Query {
		t.Fatalf("module without LIMIT must not be offset: %q", got)
	}
}

// 上限必须从真实 Query 解析，不能另写一份常量——两份会漂移。
func TestCapPerRunReadsTheRealLimit(t *testing.T) {
	cases := map[string]int{
		"SELECT id FROM t LIMIT 300":  300,
		"SELECT id FROM t LIMIT 200;": 200,
		"SELECT id FROM t":            0,
		"SELECT id FROM t LIMIT abc":  0,
	}
	for query, want := range cases {
		if got := (moduleDef{Query: query}).capPerRun(); got != want {
			t.Errorf("capPerRun(%q) = %d, want %d", query, got, want)
		}
	}
	// 真实模块要么有可解析的上限，要么整表读（无 LIMIT，天然不截断）。
	// 只有「有 LIMIT 却解析不出来」才是 bug——那会让向后补齐失效。
	for _, module := range modules {
		if module.Collect != nil || module.Query == "" {
			continue
		}
		upper := strings.ToUpper(module.Query)
		hasLimit := strings.Contains(upper, " LIMIT ")
		if hasLimit && module.capPerRun() <= 0 {
			t.Errorf("module %q has a LIMIT the parser cannot read: %q", module.ID, module.Query)
		}
	}
}

// 每个模块都必须能报出本地总量，否则界面又只能显示「已同步 N 条」。
func TestEveryModuleReportsLocalTotal(t *testing.T) {
	for _, module := range modules {
		if module.Total == "" {
			t.Errorf("module %q has no Total query; the UI cannot show what is missing", module.ID)
		}
	}
}
