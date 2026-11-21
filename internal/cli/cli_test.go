package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/cli"
	"alpha-nexus-egolite/internal/db"
)

func executeCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := cli.NewRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestRootHelp(t *testing.T) {
	output, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "alpha-nexus") {
		t.Errorf("help should contain command name")
	}
}

func TestRootDoesNotExposeScheduling(t *testing.T) {
	root := cli.NewRootCmd()
	for _, command := range root.Commands() {
		if command.Name() == "schedule" {
			t.Fatal("schedule command must not be registered")
		}
	}
}

func TestMediaDoesNotExposeBackgroundWatch(t *testing.T) {
	root := cli.NewRootCmd()
	media, _, err := root.Find([]string{"media"})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range media.Commands() {
		if command.Name() == "watch" {
			t.Fatal("media watch must not be registered; use one-shot media index")
		}
	}
}

func TestAgentPublishPlanDoesNotPretendToCreateJob(t *testing.T) {
	for _, command := range [][]string{
		{"agent", "publish-plan", "--summary", "launch post"},
		{"agent", "publish", "--summary", "launch post"},
	} {
		output, err := executeCommand(t, command...)
		if err == nil || !strings.Contains(err.Error(), "PUBLISH_AGENT_REQUIRES_EXECUTABLE_JOB") {
			t.Fatalf("command=%v err=%v output=%s", command, err, output)
		}
		for _, expected := range []string{"\"ok\": false", "\"status\": \"not_executed\"", "\"error_code\": \"PUBLISH_AGENT_REQUIRES_EXECUTABLE_JOB\""} {
			if !strings.Contains(output, expected) {
				t.Fatalf("command=%v missing %q: %s", command, expected, output)
			}
		}
	}
}

func TestPlatformList(t *testing.T) {
	output, err := executeCommand(t, "platform", "list")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"meta", "xhs", "douyin", "wechat_video", "kuaishou"}
	for _, code := range expected {
		if !strings.Contains(output, code) {
			t.Errorf("platform list should contain %q", code)
		}
	}
}

func TestPlatformListShowsPublishSupport(t *testing.T) {
	output, err := executeCommand(t, "platform", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "meta") || !strings.Contains(output, "yes") {
		t.Errorf("Meta should show publish=yes, got: %s", output)
	}
	// 小红书登录和只读采集保留，但自动发布明确禁用。
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "xhs") && !strings.Contains(line, "no") {
			t.Errorf("xhs should show publish=no, got: %s", line)
		}
	}
	for _, code := range []string{"youtube", "tiktok", "wechat_mp", "csdn"} {
		if !strings.Contains(output, code) {
			t.Errorf("platform list should contain %q", code)
		}
	}
}

func TestInit(t *testing.T) {
	dir, err := os.MkdirTemp("", "alpha-nexus-init-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	output, err := executeCommand(t, "init", "--data-dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "Initialized Alpha-Nexus") {
		t.Errorf("init output should confirm initialization, got: %s", output)
	}

	// Check directories created
	for _, sub := range []string{"media", "screenshots", "logs"} {
		info, err := os.Stat(filepath.Join(dir, sub))
		if err != nil {
			t.Errorf("directory %s should exist: %v", sub, err)
		} else if !info.IsDir() {
			t.Errorf("%s should be a directory", sub)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "profiles")); !os.IsNotExist(err) {
		t.Fatalf("init must not create a Chromium profile directory: %v", err)
	}

	// Check db created
	if _, err := os.Stat(filepath.Join(dir, "app.db")); err != nil {
		t.Errorf("app.db should exist: %v", err)
	}
}

func TestInitTwiceIdempotent(t *testing.T) {
	dir, err := os.MkdirTemp("", "alpha-nexus-init2-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	_, err = executeCommand(t, "init", "--data-dir", dir)
	if err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand(t, "init", "--data-dir", dir)
	if err != nil {
		t.Fatalf("second init should be idempotent: %v", err)
	}
	if !strings.Contains(output, "Initialized Alpha-Nexus") {
		t.Errorf("second init should confirm, got: %s", output)
	}
}

func TestInitDefaultDataDir(t *testing.T) {
	output, err := executeCommand(t, "init", "--data-dir", t.TempDir())
	if err != nil || !strings.Contains(output, "Alpha-Nexus EgoLite") {
		t.Fatalf("init output=%s err=%v", output, err)
	}
}

func initTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "unibuy-cli-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	_, err = executeCommand(t, "init", "--data-dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAccountAdd(t *testing.T) {
	dir := initTestDir(t)
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := database.CreateEgoIdentity("用户2")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	output, err := executeCommand(t, "account", "add", "--identity-id", "2", "--platform", "meta", "--name", "test-meta", "--data-dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "Account created") {
		t.Errorf("should confirm account creation, got: %s", output)
	}
	if !strings.Contains(output, "meta") {
		t.Errorf("should show platform, got: %s", output)
	}
	if !strings.Contains(output, "unknown") {
		t.Errorf("initial state should be unknown, got: %s", output)
	}
	if strings.Contains(output, "Profile:") {
		t.Fatalf("account add exposed legacy profile: %s", output)
	}
	database, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	accounts, err := database.ListAccounts()
	if err != nil || len(accounts) != 1 || accounts[0].IdentityID != identity.ID {
		t.Fatalf("accounts=%+v identity=%+v err=%v", accounts, identity, err)
	}
	if profile, err := database.GetBrowserProfileByAccount(accounts[0].ID); err == nil {
		t.Fatalf("legacy browser profile created: %+v", profile)
	}
}

func TestAccountAddInvalidPlatform(t *testing.T) {
	dir := initTestDir(t)
	_, err := executeCommand(t, "account", "add", "--platform", "nonexistent", "--data-dir", dir)
	if err == nil {
		t.Fatal("expected error for invalid platform")
	}
}

func TestAccountAddAutoName(t *testing.T) {
	dir := initTestDir(t)
	output, err := executeCommand(t, "account", "add", "--platform", "xhs", "--data-dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "Account created") {
		t.Errorf("should create account with auto name, got: %s", output)
	}
}

func TestAccountList(t *testing.T) {
	dir := initTestDir(t)

	executeCommand(t, "account", "add", "--platform", "meta", "--name", "meta1", "--data-dir", dir)
	executeCommand(t, "account", "add", "--platform", "xhs", "--name", "xhs1", "--data-dir", dir)

	output, err := executeCommand(t, "account", "list", "--data-dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "meta1") || !strings.Contains(output, "xhs1") {
		t.Errorf("should list all accounts, got: %s", output)
	}
	if !strings.Contains(output, "meta") || !strings.Contains(output, "xhs") {
		t.Errorf("should show platforms, got: %s", output)
	}
}

func TestAccountListFilter(t *testing.T) {
	dir := initTestDir(t)

	executeCommand(t, "account", "add", "--platform", "meta", "--name", "meta1", "--data-dir", dir)
	executeCommand(t, "account", "add", "--platform", "xhs", "--name", "xhs1", "--data-dir", dir)

	output, err := executeCommand(t, "account", "list", "--platform", "meta", "--data-dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output, "xhs1") {
		t.Errorf("should not contain xhs account when filtering by instagram, got: %s", output)
	}
	if !strings.Contains(output, "meta1") {
		t.Errorf("should contain meta1, got: %s", output)
	}
}

func TestAccountListEmpty(t *testing.T) {
	dir := initTestDir(t)
	output, err := executeCommand(t, "account", "list", "--data-dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "No accounts found") {
		t.Errorf("should show empty message, got: %s", output)
	}
}

func TestToolsCallUsesExplicitDataDir(t *testing.T) {
	dir := initTestDir(t)
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.CreateAccount("xhs", "registry-data-dir", ""); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand(t, "--data-dir", dir, "tools", "call", "account.list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "registry-data-dir") {
		t.Fatalf("tools call did not use --data-dir: %s", output)
	}
}

func TestToolsListShopCategory(t *testing.T) {
	help, err := executeCommand(t, "tools", "list", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(help, "shop") {
		t.Fatalf("tools list category help should include shop: %s", help)
	}

	output, err := executeCommand(t, "tools", "list", "--category", "shop")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"shop.listing.dry-run",
		"shop.listing.freeze",
		"shop.listing.get",
		"shop.listing.prepare",
	} {
		if !strings.Contains(output, name) {
			t.Fatalf("shop tool %q missing from CLI list: %s", name, output)
		}
	}
	for _, name := range []string{"shop.listing.submit", "shop.listing.verify"} {
		if strings.Contains(output, name) {
			t.Fatalf("forbidden shop tool %q appeared in CLI list: %s", name, output)
		}
	}
}

func TestAccountCheckRequiresBoundEgoSpace(t *testing.T) {
	dir := initTestDir(t)

	_, err := executeCommand(t, "account", "add", "--platform", "meta", "--name", "meta1", "--remark", "work account", "--data-dir", dir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = executeCommand(t, "account", "check", "--id", "1", "--data-dir", dir)
	if err == nil || !strings.Contains(err.Error(), "EGO_ACCOUNT_CHECK_FAILED") {
		t.Fatalf("account check must not report stored state as verified: %v", err)
	}
}

func TestBatchDiscoverRejectsUnsupportedPlatform(t *testing.T) {
	dir := initTestDir(t)
	_, err := executeCommand(t, "batch", "discover", "--platform", "xhs", "--data-dir", dir)
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("batch discover error=%v", err)
	}
}

func TestBatchDiscoverIsUnavailable(t *testing.T) {
	dir := initTestDir(t)
	if _, err := executeCommand(t, "account", "add", "--platform", "meta", "--name", "meta-discover", "--data-dir", dir); err != nil {
		t.Fatal(err)
	}
	output, err := executeCommand(t, "batch", "discover", "--platform", "meta", "--data-dir", dir)
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("legacy batch discover remains available: err=%v output=%s", err, output)
	}
}

func TestAccountDisableAndEnablePersistsState(t *testing.T) {
	dir := initTestDir(t)
	if _, err := executeCommand(t, "account", "add", "--platform", "meta", "--name", "disabled", "--data-dir", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := executeCommand(t, "account", "disable", "--id", "1", "--data-dir", dir); err != nil {
		t.Fatal(err)
	}
	if _, err := executeCommand(t, "account", "enable", "--id", "1", "--data-dir", dir); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.GetAccount(1)
	if err != nil || !account.Enabled {
		t.Fatalf("account=%+v err=%v", account, err)
	}
}

func TestAccountCheckNotFound(t *testing.T) {
	dir := initTestDir(t)
	_, err := executeCommand(t, "account", "check", "--id", "999", "--data-dir", dir)
	if err == nil {
		t.Fatal("expected error for nonexistent account")
	}
}

func TestLegacyBrowserCommandIsUnavailable(t *testing.T) {
	dir := initTestDir(t)
	_, err := executeCommand(t, "browser", "status", "--account-id", "1", "--data-dir", dir)
	if err == nil {
		t.Fatal("legacy browser command must not be registered")
	}
	if !strings.Contains(err.Error(), `unknown command "browser"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAccountLoginHandoffMissingID(t *testing.T) {
	dir := initTestDir(t)
	_, err := executeCommand(t, "account", "login-handoff", "--data-dir", dir)
	if err == nil {
		t.Fatal("expected error for missing --id")
	}
}

func TestVersion(t *testing.T) {
	output, err := executeCommand(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "alpha-nexus-egolite v") {
		t.Errorf("should show version, got: %s", output)
	}
	if !strings.Contains(output, "commit:") || !strings.Contains(output, "built:") || !strings.Contains(output, "platform:") {
		t.Errorf("should show build info, got: %s", output)
	}
}
