package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/tools"
)

func TestListPlatformsViaHandler(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := &handlers{db: database, rt: &tools.Runtime{DataDir: dir}, dataDir: dir}
	s := NewServer("alpha-nexus", "1.0.0", "")
	registerFriendlyTools(s, h)
	registerRegistryTools(s, h)

	if _, ok := s.tools["list_accounts"]; !ok {
		t.Fatal("missing list_accounts")
	}
	if _, ok := s.tools["create_video"]; !ok {
		t.Fatal("missing create_video")
	}
	if _, ok := s.tools["tools_list"]; !ok {
		t.Fatal("missing tools_list")
	}

	out, err := s.tools["list_platforms"].Handler(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !json.Valid(b) {
		t.Fatal("invalid json")
	}
	m := out.(map[string]any)
	if m["count"].(int) < 4 {
		t.Fatalf("count %v", m["count"])
	}
	var xhs map[string]any
	for _, row := range m["platforms"].([]map[string]any) {
		if row["code"] == "xhs" {
			xhs = row
			break
		}
	}
	if xhs == nil {
		t.Fatal("xhs platform missing")
	}
	if xhs["publish_mode"] != "browser_automation" || xhs["can_create_job"] != true || xhs["can_attempt_auto_submit"] != false || xhs["supports_article"] != false || xhs["supports_graph_text"] != true {
		t.Fatalf("stale xhs readiness: %+v", xhs)
	}
}

func TestCreateArticleFlow(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := &handlers{db: database, rt: &tools.Runtime{DataDir: dir}, dataDir: dir}
	s := NewServer("t", "1", "")
	registerFriendlyTools(s, h)

	out, err := s.tools["create_article"].Handler(context.Background(), map[string]any{
		"title": "t1", "markdown": "# hi", "desc": "d",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	id := m["article_id"]
	list, err := s.tools["list_articles"].Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	lm := list.(map[string]any)
	if lm["count"].(int) != 1 {
		t.Fatalf("want 1 article, got %v id=%v", lm["count"], id)
	}
}

func TestMCPSideEffectGuard(t *testing.T) {
	t.Setenv("ALPHA_NEXUS_MCP_ALLOW_WRITE", "1")
	if err := guardMCPSideEffect("browser.start", "browser", map[string]any{}); err == nil {
		t.Fatal("expected deny")
	}
	if err := guardMCPSideEffect("browser.start", "browser", map[string]any{"confirm": true}); err == nil || err.Error() != "MCP_SIDE_EFFECT_DENIED" {
		t.Fatalf("model confirmation and write env must not authorize MCP side effects: %v", err)
	}
	if err := guardMCPSideEffect("account.list", "read", nil); err != nil {
		t.Fatal(err)
	}
	if err := guardMCPSideEffect("unclassified", "", nil); err == nil || err.Error() != "MCP_SIDE_EFFECT_DENIED" {
		t.Fatalf("unclassified MCP tool must fail closed: %v", err)
	}
}

func TestToolListPublishesReadOnlyAnnotations(t *testing.T) {
	s := NewServer("test", "1", "")
	s.AddTool(ToolDef{
		Name: "read_summary", SideEffect: "read",
		Handler: func(context.Context, map[string]any) (any, error) { return "ok", nil },
	})
	response := s.handle(context.Background(), &rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "tools/list",
	})
	if response == nil || response.Result == nil {
		t.Fatal("missing tools/list response")
	}
	result, ok := response.Result.(map[string]any)
	if !ok {
		t.Fatalf("result=%T", response.Result)
	}
	list, ok := result["tools"].([]map[string]any)
	if !ok || len(list) != 1 {
		t.Fatalf("tools=%#v", result["tools"])
	}
	annotations, ok := list[0]["annotations"].(map[string]any)
	if !ok || annotations["readOnlyHint"] != true || annotations["destructiveHint"] != false {
		t.Fatalf("annotations=%#v", list[0]["annotations"])
	}
}

func TestMCPConfirmationIsRemovedWithoutMutatingCallerArgs(t *testing.T) {
	original := map[string]any{"confirm": true, "name": "safe"}
	clean := withoutMCPConfirmation(original)
	if _, ok := clean["confirm"]; ok {
		t.Fatalf("MCP confirmation reached Registry args: %+v", clean)
	}
	if clean["name"] != "safe" {
		t.Fatalf("safe args changed: %+v", clean)
	}
	if original["confirm"] != true {
		t.Fatalf("caller args mutated: %+v", original)
	}
}

func TestPublishDouyinDryRunIsDeniedByReadOnlyMCP(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := &handlers{db: database, rt: &tools.Runtime{DataDir: dir}, dataDir: dir}
	s := NewServer("t", "1", "")
	registerFriendlyTools(s, h)

	acc, err := h.db.CreateAccount("douyin", "douyin", "")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := h.db.CreateAccountAsset(acc.ID, "douyin", "douyin_account", "douyin", "douyin-1", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := h.db.CreateContent("graph_text", "note", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.tools["publish_graph_text"].Handler(context.Background(), map[string]any{
		"article_id": float64(c.ID),
		"account_id": float64(acc.ID),
		"target_id":  float64(asset.ID),
		"platform":   "douyin",
		"dry_run":    true,
		"confirm":    true,
	})
	if err == nil || err.Error() != "MCP_SIDE_EFFECT_DENIED" {
		t.Fatalf("model confirmation authorized MCP publish: %v", err)
	}
	jobs, err := h.db.ListPublishJobs(10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
}

func TestFriendlyPublishMissingTargetFailsInsteadOfPlanning(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := &handlers{db: database, rt: &tools.Runtime{DataDir: dir}, dataDir: dir}
	s := NewServer("t", "1", "")
	registerFriendlyTools(s, h)
	acc, _ := h.db.CreateAccount("xhs", "xhs", "")
	if _, err := h.db.Exec(`DELETE FROM account_assets WHERE account_id=?`, acc.ID); err != nil {
		t.Fatal(err)
	}
	c, _ := h.db.CreateContent("graph_text", "note", "caption", "body", "[]", "draft")

	_, err = s.tools["publish_graph_text"].Handler(context.Background(), map[string]any{
		"article_id": c.ID,
		"account_id": acc.ID,
		"platform":   "xhs",
		"dry_run":    true,
		"confirm":    true,
	})
	if err == nil || !strings.Contains(err.Error(), "TARGET_ASSET_REQUIRED") {
		t.Fatalf("want TARGET_ASSET_REQUIRED, got %v", err)
	}
}

func TestFriendlyLivePublishRequiresStableIdempotencyKey(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := &handlers{db: database, rt: &tools.Runtime{DataDir: dir}, dataDir: dir}
	s := NewServer("t", "1", "")
	registerFriendlyTools(s, h)
	acc, _ := h.db.CreateAccount("xhs", "xhs", "")
	asset, _ := h.db.CreateAccountAsset(acc.ID, "xhs", "xhs_account", "xhs", "xhs-1", "", 0, "")
	c, _ := h.db.CreateContent("graph_text", "note", "caption", "body", "[]", "draft")

	_, err = s.tools["publish_graph_text"].Handler(context.Background(), map[string]any{
		"article_id": c.ID,
		"account_id": acc.ID,
		"target_id":  asset.ID,
		"platform":   "xhs",
		"dry_run":    false,
		"confirm":    true,
	})
	if err == nil || !strings.Contains(err.Error(), "IDEMPOTENCY_KEY_REQUIRED") {
		t.Fatalf("want IDEMPOTENCY_KEY_REQUIRED, got %v", err)
	}
}

func TestFriendlyPublishBindsContentAccountAndAsset(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := &handlers{db: database, rt: &tools.Runtime{DataDir: dir}, dataDir: dir}
	s := NewServer("t", "1", "")
	registerFriendlyTools(s, h)
	accountA, _ := h.db.CreateAccount("xhs", "a", "")
	accountB, _ := h.db.CreateAccount("xhs", "b", "")
	assetB, _ := h.db.CreateAccountAsset(accountB.ID, "xhs", "xhs_account", "b", "xhs-b", "", 0, "")
	article, _ := h.db.CreateContent("article", "article", "caption", "body", "[]", "draft")

	_, err = s.tools["publish_video"].Handler(context.Background(), map[string]any{
		"article_id": article.ID,
		"account_id": accountA.ID,
		"target_id":  assetB.ID,
		"platform":   "xhs",
		"dry_run":    true,
		"confirm":    true,
	})
	if err == nil || !strings.Contains(err.Error(), "CONTENT_TYPE_MISMATCH") {
		t.Fatalf("want CONTENT_TYPE_MISMATCH first, got %v", err)
	}

	graph, _ := h.db.CreateContent("graph_text", "graph", "caption", "body", "[]", "draft")
	_, err = s.tools["publish_graph_text"].Handler(context.Background(), map[string]any{
		"article_id": graph.ID,
		"account_id": accountA.ID,
		"target_id":  assetB.ID,
		"platform":   "xhs",
		"dry_run":    true,
		"confirm":    true,
	})
	if err == nil || !strings.Contains(err.Error(), "TARGET_ACCOUNT_MISMATCH") {
		t.Fatalf("want TARGET_ACCOUNT_MISMATCH, got %v", err)
	}
}

func TestToolsListRPC(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := &handlers{db: database, rt: &tools.Runtime{DataDir: dir}, dataDir: dir}
	s := NewServer("alpha-nexus", "1.0.0", "test")
	registerFriendlyTools(s, h)
	registerRegistryTools(s, h)

	resp := s.handle(context.Background(), &rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "tools/list", Params: json.RawMessage(`{}`),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("bad resp %+v", resp)
	}
	result := resp.Result.(map[string]any)
	tools := result["tools"].([]map[string]any)
	if len(tools) < 15 {
		t.Fatalf("too few tools: %d", len(tools))
	}
	foundReadProjection := false
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		if name == "registry_studio_clip_plan" || name == "registry_browser_start" || name == "registry_account_add" {
			t.Fatalf("MCP projected side-effect tool %q", name)
		}
		foundReadProjection = foundReadProjection || name == "registry_studio_media_list"
	}
	if !foundReadProjection {
		t.Fatal("missing read-only Studio media list registry tool")
	}
}

func TestRegistryToolsListAndCallAreReadOnly(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := &handlers{db: database, rt: &tools.Runtime{DataDir: dir}, dataDir: dir}
	s := NewServer("alpha-nexus", "1.0.0", "test")
	registerRegistryTools(s, h)

	out, err := s.tools["tools_list"].Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	rows := out.(map[string]any)["tools"].([]map[string]any)
	if len(rows) == 0 {
		t.Fatal("expected read-only registry tools")
	}
	for _, row := range rows {
		if row["side_effect"] != "read" {
			t.Fatalf("tools_list exposed non-read tool: %+v", row)
		}
	}
	if _, ok := s.tools["registry_ego_space_ensure"]; ok {
		t.Fatal("full registry projection exposed ego.space.ensure")
	}
	if _, ok := s.tools["registry_account_list"]; !ok {
		t.Fatal("full registry projection omitted account.list")
	}
	if _, ok := s.tools["registry_ego_space_list"]; !ok {
		t.Fatal("full registry projection omitted ego.space.list")
	}

	t.Setenv("ALPHA_NEXUS_MCP_ALLOW_WRITE", "1")
	_, err = s.tools["tools_call"].Handler(context.Background(), map[string]any{
		"name":    "account.add",
		"args":    map[string]any{"platform": "xhs", "profile": "must-not-run"},
		"confirm": true,
	})
	if err == nil || err.Error() != "MCP_SIDE_EFFECT_DENIED" {
		t.Fatalf("tools_call bypassed read-only boundary: %v", err)
	}
	accounts, listErr := database.ListAccounts()
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(accounts) != 0 {
		t.Fatalf("denied MCP call mutated SQLite: %+v", accounts)
	}

	listed, err := s.tools["tools_call"].Handler(context.Background(), map[string]any{"name": "tools.list"})
	if err != nil {
		t.Fatal(err)
	}
	listedResult, ok := listed.(*tools.Result)
	if !ok || !listedResult.OK {
		t.Fatalf("unexpected tools.list result: %#v", listed)
	}
	names, ok := listedResult.Data["tools"].([]string)
	if !ok || len(names) == 0 {
		t.Fatalf("unexpected MCP tools.list names: %#v", listedResult.Data["tools"])
	}
	for _, name := range names {
		def, exists := tools.Default.Get(name)
		if !exists || !isMCPReadSideEffect(def.SideEffect) {
			t.Fatalf("MCP tools.list exposed %q (%q)", name, def.SideEffect)
		}
	}
}

func TestMCPReadOnlyProjectionHidesFriendlyMutationTools(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := &handlers{db: database, rt: &tools.Runtime{DataDir: dir}, dataDir: dir}
	s := newReadOnlyMCPServer(h, func(s *Server) {
		s.AddTool(ToolDef{Name: "extra_read", SideEffect: "read", Handler: func(context.Context, map[string]any) (any, error) { return "ok", nil }})
		s.AddTool(ToolDef{Name: "extra_write", SideEffect: "write", Handler: func(context.Context, map[string]any) (any, error) { return "bad", nil }})
		s.AddTool(ToolDef{Name: "extra_unclassified", Handler: func(context.Context, map[string]any) (any, error) { return "bad", nil }})
	})

	for _, name := range []string{"create_article", "publish_graph_text", "browser_start", "browser_stop", "browser_status", "registry_account_add", "extra_write", "extra_unclassified"} {
		if _, ok := s.tools[name]; ok {
			t.Fatalf("read-only MCP projection exposed %s", name)
		}
	}
	for _, name := range []string{"list_accounts", "ego_space_list", "tools_list", "tools_call", "registry_account_list", "registry_ego_space_list", "extra_read"} {
		if _, ok := s.tools[name]; !ok {
			t.Fatalf("read-only MCP projection omitted %s", name)
		}
	}
	for _, name := range s.order {
		if !isMCPReadSideEffect(s.tools[name].SideEffect) {
			t.Fatalf("projected MCP tool %s is not classified read", name)
		}
	}
}

func TestMCPResponseSanitizesRuntimeInternalsAndRawArguments(t *testing.T) {
	s := NewServer("alpha-nexus", "1.0.0", "test")
	s.AddTool(ToolDef{
		Name:       "runtime_fixture",
		SideEffect: "read",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{
				"account_id":  7,
				"running":     true,
				"pid":         4242,
				"cdp_port":    15101,
				"cdp_url":     "http://127.0.0.1:15101",
				"lease":       "ui:secret-owner",
				"profile_dir": "/private/tmp/profiles/acct-7",
				"nested": map[string]any{
					"args":       args,
					"path":       "/private/tmp/media/source.png",
					"file_path":  "/private/tmp/media/source.mp4",
					"data_dir":   "/private/tmp/alpha-data",
					"key_masked": "abcd...wxyz",
					"masked_key": "wxyz...abcd",
					"location":   "/private/tmp/media/untyped.mov",
					"items": []any{
						map[string]any{"source_path": "/private/tmp/media/nested.png"},
					},
				},
			}, nil
		},
	})
	params, err := json.Marshal(map[string]any{
		"name": "runtime_fixture",
		"arguments": map[string]any{
			"prompt": "RAW_MODEL_ARGUMENT_MUST_NOT_RETURN",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := s.handle(context.Background(), &rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params,
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("bad response: %+v", resp)
	}
	result := resp.Result.(map[string]any)
	content := result["content"].([]map[string]any)
	text, _ := content[0]["text"].(string)
	for _, forbidden := range []string{
		"4242", "15101", "cdp_port", "cdp_url", "secret-owner",
		"/private/tmp/profiles", "/private/tmp/media", "/private/tmp/alpha-data",
		"abcd...wxyz", "wxyz...abcd", "key_masked", "RAW_MODEL_ARGUMENT_MUST_NOT_RETURN", `"args"`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("MCP response leaked %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"account_id": 7`) || !strings.Contains(text, `"running": true`) {
		t.Fatalf("sanitizer removed safe status fields: %s", text)
	}
}

func TestProductionMCPEgoSpaceListIsReadOnlySnapshot(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	account, err := database.CreateAccount("xhs", "read-only-status", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpace(account.ID, 73, "task-read-only", "identity-1/account-1/xhs"); err != nil {
		t.Fatal(err)
	}
	appDBPath := filepath.Join(dir, "app.db")
	beforeDB, err := os.ReadFile(appDBPath)
	if err != nil {
		t.Fatal(err)
	}
	var beforeChanges int64
	if err := database.QueryRow(`SELECT total_changes()`).Scan(&beforeChanges); err != nil {
		t.Fatal(err)
	}

	h := &handlers{db: database, rt: &tools.Runtime{DataDir: dir}, dataDir: dir}
	s := newReadOnlyMCPServer(h, nil)
	params, err := json.Marshal(map[string]any{
		"name":      "registry_ego_space_list",
		"arguments": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := s.handle(context.Background(), &rpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})
	if resp == nil || resp.Error != nil {
		t.Fatalf("bad ego Space list response: %+v", resp)
	}
	result := resp.Result.(map[string]any)
	if result["isError"] != false {
		t.Fatalf("ego Space list failed: %+v", result)
	}
	text := result["content"].([]map[string]any)[0]["text"].(string)
	for _, forbidden := range []string{"cookie", "password", "cdp", "profile"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			t.Fatalf("ego Space list leaked %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"space_id": 73`) || !strings.Contains(text, `"ownership": "agent"`) {
		t.Fatalf("ego Space list omitted safe status: %s", text)
	}
	var afterChanges int64
	if err := database.QueryRow(`SELECT total_changes()`).Scan(&afterChanges); err != nil {
		t.Fatal(err)
	}
	if afterChanges != beforeChanges {
		t.Fatalf("read-only Space list changed SQLite: before=%d after=%d", beforeChanges, afterChanges)
	}
	afterDB, err := os.ReadFile(appDBPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterDB, beforeDB) {
		t.Fatal("read-only Space list changed app.db bytes")
	}
}

func TestProductionMCPAccountListDoesNotRunWriteMigrations(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	account, err := database.CreateAccount("xhs", "read-only-account-list", "")
	if err != nil {
		t.Fatal(err)
	}
	const legacyAvatar = "https://example.invalid/avatar.png?signature=must-remain"
	if _, err := database.Exec(`UPDATE accounts SET avatar=? WHERE id=?`, legacyAvatar, account.ID); err != nil {
		t.Fatal(err)
	}

	h := &handlers{db: database, rt: &tools.Runtime{DataDir: dir}, dataDir: dir}
	s := newReadOnlyMCPServer(h, nil)
	resp := s.handle(context.Background(), &rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"registry_account_list","arguments":{}}`),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("bad account list response: %+v", resp)
	}
	result := resp.Result.(map[string]any)
	if result["isError"] != false {
		t.Fatalf("account list failed: %+v", result)
	}
	var afterAvatar string
	if err := database.QueryRow(`SELECT avatar FROM accounts WHERE id=?`, account.ID).Scan(&afterAvatar); err != nil {
		t.Fatal(err)
	}
	if afterAvatar != legacyAvatar {
		t.Fatalf("MCP read ran a write migration: avatar=%q", afterAvatar)
	}
}

func TestMCPToolErrorDoesNotEchoRuntimeDetailsOrArguments(t *testing.T) {
	s := NewServer("alpha-nexus", "1.0.0", "test")
	s.AddTool(ToolDef{
		Name:       "error_fixture",
		SideEffect: "read",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return nil, errors.New("connect http://127.0.0.1:15101 for RAW_ARGUMENT failed")
		},
	})
	resp := s.handle(context.Background(), &rpcRequest{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: json.RawMessage(`{"name":"error_fixture","arguments":{"prompt":"RAW_ARGUMENT"}}`),
	})
	result := resp.Result.(map[string]any)
	content := result["content"].([]map[string]any)
	if text := content[0]["text"]; text != "MCP_TOOL_CALL_FAILED" {
		t.Fatalf("unsafe public MCP error: %v", text)
	}
}

func TestProductionMCPInitializationDoesNotChangeDataDir(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logs", "existing.log"), []byte("do-not-touch"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotDataDir(t, dir)
	runtime := &tools.Runtime{DataDir: dir}
	_, readDB, err := prepareReadOnlyMCP(Config{DataDir: dir, Runtime: runtime})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Events != nil {
		t.Fatal("MCP initialization installed a writable event emitter")
	}
	if err := readDB.Close(); err != nil {
		t.Fatal(err)
	}
	after := snapshotDataDir(t, dir)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("MCP initialization changed data dir: %s", snapshotDataDirDiff(before, after))
	}
}

func TestProductionMCPInitializationFailsClosedWithoutTouchingSchemaOnlyInWAL(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.CreateAccount("xhs", "wal-fixture", ""); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		info, err := os.Stat(filepath.Join(dir, "app.db") + suffix)
		if err != nil {
			t.Fatalf("expected existing SQLite sidecar %s: %v", suffix, err)
		}
		if info.Size() == 0 {
			t.Fatalf("expected non-empty SQLite sidecar %s", suffix)
		}
	}
	before := snapshotDataDir(t, dir)
	if _, _, err := prepareReadOnlyMCP(Config{DataDir: dir}); err == nil || err.Error() != "MCP_DATABASE_NOT_INITIALIZED" {
		t.Fatalf("expected stable fail-closed error for schema only in WAL, got %v", err)
	}
	if after := snapshotDataDir(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatalf("fail-closed MCP initialization changed SQLite sidecars: %s", snapshotDataDirDiff(before, after))
	}
}

func TestProductionMCPInitializationFailsClosedWithoutMigratedDatabase(t *testing.T) {
	root := t.TempDir()
	missingDir := filepath.Join(root, "missing")
	beforeMissing := snapshotDataDir(t, root)
	if _, _, err := prepareReadOnlyMCP(Config{DataDir: missingDir}); err == nil {
		t.Fatal("missing MCP database unexpectedly initialized")
	}
	if after := snapshotDataDir(t, root); !reflect.DeepEqual(after, beforeMissing) {
		t.Fatalf("missing MCP database path was created: %s", snapshotDataDirDiff(beforeMissing, after))
	}

	unmigratedDir := filepath.Join(root, "unmigrated")
	if err := os.Mkdir(unmigratedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unmigratedDir, "app.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	beforeUnmigrated := snapshotDataDir(t, unmigratedDir)
	if _, _, err := prepareReadOnlyMCP(Config{DataDir: unmigratedDir}); err == nil {
		t.Fatal("unmigrated MCP database unexpectedly initialized")
	}
	if after := snapshotDataDir(t, unmigratedDir); !reflect.DeepEqual(after, beforeUnmigrated) {
		t.Fatalf("unmigrated MCP database was changed: %s", snapshotDataDirDiff(beforeUnmigrated, after))
	}
}

func TestProductionMCPTikHubReadsDoNotChangeDataDir(t *testing.T) {
	dir := t.TempDir()
	database, err := openDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := os.MkdirAll(filepath.Join(dir, "tikhub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tikhub_api_key"), []byte("synthetic-fixture-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	watch := `{"version":1,"users":[{"platform":"douyin","user_id":"fixture-user","name":"fixture","note":"read-only"}]}`
	if err := os.WriteFile(filepath.Join(dir, "tikhub", "watchlist.json"), []byte(watch), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotDataDir(t, dir)
	runtime := &tools.Runtime{DataDir: dir}
	h := &handlers{db: database, rt: runtime, dataDir: dir}
	s := newReadOnlyMCPServer(h, nil)
	for id, name := range []string{"registry_tikhub_config_status", "registry_tikhub_user_list"} {
		params, err := json.Marshal(map[string]any{"name": name, "arguments": map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		resp := s.handle(context.Background(), &rpcRequest{JSONRPC: "2.0", ID: id + 1, Method: "tools/call", Params: params})
		if resp == nil || resp.Error != nil || resp.Result.(map[string]any)["isError"] != false {
			t.Fatalf("%s failed: %+v", name, resp)
		}
	}
	after := snapshotDataDir(t, dir)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("TikHub MCP reads changed data dir: %s", snapshotDataDirDiff(before, after))
	}
	if runtime.Events != nil {
		_ = runtime.CloseEvents()
		t.Fatal("TikHub MCP read installed a writable event emitter")
	}
}

type dataDirSnapshotEntry struct {
	Mode uint32
	Data string
}

func snapshotDataDir(t *testing.T, root string) map[string]dataDirSnapshotEntry {
	t.Helper()
	out := make(map[string]dataDirSnapshotEntry)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := dataDirSnapshotEntry{Mode: uint32(info.Mode())}
		if entry.Type().IsRegular() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			item.Data = string(contents)
		} else if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			item.Data = target
		}
		out[rel] = item
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func snapshotDataDirDiff(before, after map[string]dataDirSnapshotEntry) string {
	paths := make(map[string]struct{}, len(before)+len(after))
	for path := range before {
		paths[path] = struct{}{}
	}
	for path := range after {
		paths[path] = struct{}{}
	}
	changed := make([]string, 0)
	for path := range paths {
		beforeEntry, beforeOK := before[path]
		afterEntry, afterOK := after[path]
		if beforeOK == afterOK && reflect.DeepEqual(beforeEntry, afterEntry) {
			continue
		}
		changed = append(changed, fmt.Sprintf(
			"%s before(present=%t mode=%#o bytes=%d) after(present=%t mode=%#o bytes=%d)",
			path, beforeOK, beforeEntry.Mode, len(beforeEntry.Data), afterOK, afterEntry.Mode, len(afterEntry.Data),
		))
	}
	sort.Strings(changed)
	return strings.Join(changed, "; ")
}
