package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestEgoSessionCheckReusesLongLivedSerializedMonitor(t *testing.T) {
	called := 0
	result, err := Default.Call(context.Background(), &Runtime{
		CheckEgoSessions: func(context.Context) (*Result, error) {
			called++
			return okResult("session check completed", map[string]interface{}{"checked": 0}), nil
		},
	}, "ego.session.check", nil)
	if err != nil || result == nil || !result.OK || called != 1 {
		t.Fatalf("result=%+v err=%v callback_calls=%d", result, err, called)
	}
}

func TestOpenEgoToolDBReusesRuntimeDatabaseWithoutClosingIt(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	runtime := &Runtime{DataDir: dir, Database: database}
	opened, closeDB, failure := openEgoToolDB(runtime)
	if failure != nil || opened != database {
		t.Fatalf("opened=%p want=%p failure=%+v", opened, database, failure)
	}
	closeDB()
	if _, err := database.ListAccounts(); err != nil {
		t.Fatalf("shared runtime database was closed: %v", err)
	}
}

type egoToolRunner struct{}

func (egoToolRunner) Run(_ context.Context, program string, result any) error {
	payload := `{"id":801,"taskId":"task-801","name":"identity-1/account-1/xhs","ownership":"agent","done":true}`
	if strings.Contains(program, "required.filter") {
		payload = `{"available":true,"missing":[]}`
	}
	if strings.Contains(program, "snapshotText") {
		payload = `{"available":true,"length":64}`
	}
	return json.Unmarshal([]byte(payload), result)
}

func TestCatalogUsesEgoSpaceToolsInsteadOfBrowserProcessTools(t *testing.T) {
	for _, name := range []string{
		"ego.doctor", "ego.identity.list", "ego.matrix.create", "ego.matrix.configure-platforms",
		"ego.space.list", "ego.space.ensure", "ego.space.open", "ego.space.login", "ego.space.resume",
		"ego.bookmarks.import", "ego.login.batch", "ego.login.verify", "ego.session.check",
	} {
		definition, ok := Default.Get(name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if !definition.AgentVisible {
			t.Fatalf("%s must be Agent-visible through the Registry", name)
		}
	}
	for _, name := range []string{
		"browser.start", "browser.stop", "browser.attach", "account.check",
		"meta.login", "meta.discover", "ops.collect", "batch.publish",
		"publish.plan.inspect", "publish.plan.inspect_composer", "publish.plan.inspect_manager",
		"publish.plan.reveal_settings", "publish.plan.resume_topics",
	} {
		if _, ok := Default.Get(name); ok {
			t.Fatalf("legacy process tool %s must not be registered", name)
		}
	}
}

func TestEgoSpaceOpenUsesRegistryWithoutElectron(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.CreateEgoIdentity("浏览器 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ConfigureIdentityPlatforms(identity.ID, []string{"douyin"}); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()

	result, err := Default.Call(context.Background(), &Runtime{
		DataDir:   dir,
		EgoRunner: egoToolRunner{},
	}, "ego.space.open", map[string]interface{}{"identity_id": identity.ID})
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Data["identity_id"] != float64(identity.ID) || result.Data["state"] != "manual_takeover" {
		t.Fatalf("unexpected open result: %+v", result.Data)
	}
	platformCodes, ok := result.Data["platform_codes"].([]interface{})
	if !ok || len(platformCodes) != 1 || platformCodes[0] != "douyin" {
		t.Fatalf("unexpected platform codes: %#v", result.Data["platform_codes"])
	}
}

func TestEgoMatrixToolsCreateAndConfigurePlatformRows(t *testing.T) {
	dir := t.TempDir()
	created, err := Default.Call(context.Background(), &Runtime{DataDir: dir}, "ego.matrix.create", map[string]interface{}{})
	if err != nil || created == nil || !created.OK {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	identity := created.Data["identity"].(map[string]interface{})
	identityID := identity["id"].(float64)
	configured, err := Default.Call(context.Background(), &Runtime{DataDir: dir}, "ego.matrix.configure-platforms", map[string]interface{}{
		"identity_id": identityID, "platform_codes": []interface{}{"douyin", "youtube"},
	})
	if err != nil || configured == nil || !configured.OK || configured.Data["count"] != 2 {
		t.Fatalf("configured=%+v err=%v", configured, err)
	}
}

func TestEgoSpaceEnsurePersistsAccountBinding(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "主账号", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = database.Close()

	result, err := Default.Call(context.Background(), &Runtime{
		DataDir:   dir,
		EgoRunner: egoToolRunner{},
	}, "ego.space.ensure", map[string]interface{}{"account_id": account.ID})
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
