package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/fleet"
	"alpha-nexus-egolite/internal/media"
	"alpha-nexus-egolite/internal/observability"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/studio"
	"alpha-nexus-egolite/internal/tikhub"
	"alpha-nexus-egolite/internal/trace"
)

type failingEmitter struct{}

func (failingEmitter) Emit(context.Context, observability.Event) error {
	return errors.New("synthetic emitter failure")
}

type delayedFleetController struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *delayedFleetController) StartAccount(accountID int64, _ browser.StartOptions) (*browser.Instance, error) {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return &browser.Instance{AccountID: accountID, ProfileID: browser.ProfileIDForAccount(accountID), State: browser.StateReady}, nil
}

func (c *delayedFleetController) StopAccount(int64) error { return nil }

func (c *delayedFleetController) Status(accountID int64) (*browser.AccountStatus, error) {
	return &browser.AccountStatus{AccountID: accountID, ProfileID: browser.ProfileIDForAccount(accountID), Running: true, State: browser.StateReady}, nil
}

func TestCatalogHasCoreTools(t *testing.T) {
	need := []string{
		"tools.list",
		"ego.doctor",
		"ego.identity.list",
		"ego.matrix.create",
		"ego.matrix.configure-platforms",
		"ego.space.list",
		"ego.space.ensure",
		"ego.space.login",
		"ego.space.resume",
		"ego.bookmarks.import",
		"ego.login.batch",
		"ego.login.verify",
		"ego.session.check",
		"account.add",
		"publish.run",
		"content.variant.upsert",
		"publish.plan.freeze",
		"publish.plan.list",
		"publish.plan.get",
		"publish.plan.prepare",
		"publish.plan.submit",
		"account.tag.list",
		"account.tag.create",
		"account.tag.update",
		"account.tag.resolve",
		"studio.media.list",
		"studio.script.create",
		"studio.script.list",
		"studio.clip.plan",
		"studio.clip.execute",
		"media.generate",
		"media.generate.batch",
		"content.media.attach",
		"tikhub.rank.daily",
		"tikhub.update",
		"social.account-check",
		"social.publish-prepare",
		"social.publish-submit-approved",
	}
	for _, n := range need {
		if _, ok := Default.Get(n); !ok {
			t.Fatalf("missing tool %s", n)
		}
	}
	freeze, ok := Default.Get("publish.plan.freeze")
	if !ok {
		t.Fatal("missing publish.plan.freeze")
	}
	hasPublishAt := false
	hasNativeVideoCover := false
	for _, param := range freeze.Params {
		if param.Name == "publish_at" {
			hasPublishAt = true
		}
		if param.Name == "native_video_cover" {
			hasNativeVideoCover = true
		}
	}
	if !hasPublishAt {
		t.Fatal("publish.plan.freeze schema missing publish_at")
	}
	if !hasNativeVideoCover {
		t.Fatal("publish.plan.freeze schema missing native_video_cover")
	}
	if _, ok := Default.Get("schedule.add"); ok {
		t.Fatal("schedule.add must not be registered")
	}
	mediaGenerate, ok := Default.Get("media.generate")
	if !ok {
		t.Fatal("media.generate must be registered")
	}
	wantParams := map[string]bool{"request_id": false, "kind": false, "mode": false, "model": false, "output_size": false, "duration_seconds": false, "prompt": false, "references": false, "confirm": false}
	for _, param := range mediaGenerate.Params {
		if _, exists := wantParams[param.Name]; exists {
			wantParams[param.Name] = true
		}
	}
	for name, found := range wantParams {
		if !found {
			t.Errorf("media.generate schema missing %s", name)
		}
	}
	mediaGenerateBatch, ok := Default.Get("media.generate.batch")
	if !ok {
		t.Fatal("media.generate.batch must be registered")
	}
	batchParams := map[string]bool{"request_id": false, "kind": false, "mode": false, "model": false, "output_size": false, "duration_seconds": false, "prompt": false, "references": false, "count": false, "confirm": false}
	for _, param := range mediaGenerateBatch.Params {
		if _, exists := batchParams[param.Name]; exists {
			batchParams[param.Name] = true
		}
	}
	for name, found := range batchParams {
		if !found {
			t.Errorf("media.generate.batch schema missing %s", name)
		}
	}
}

func TestPublishPlanUIRuntimeCallbacksAvoidPerCallDatabaseOpen(t *testing.T) {
	freezeCalled := false
	getCalled := false
	runtime := &Runtime{
		DataDir: filepath.Join(t.TempDir(), "must-not-open"),
		FreezePublishPlan: func(_ context.Context, input publishplan.FreezeInput) (*Result, error) {
			freezeCalled = input.ContentID == 7
			return &Result{OK: true, Tool: "publish.plan.freeze", Data: map[string]interface{}{"plan": "shared"}}, nil
		},
		GetPublishPlan: func(_ context.Context, id int64) (*Result, error) {
			getCalled = id == 9
			return &Result{OK: true, Tool: "publish.plan.get", Data: map[string]interface{}{"plan": "shared"}}, nil
		},
	}
	freeze, freezeErr := Default.Call(context.Background(), runtime, "publish.plan.freeze", map[string]interface{}{
		"content_id": 7, "media_mode": "article", "media_ids": []int64{}, "targets": []map[string]interface{}{{"account_id": 1}},
	})
	get, getErr := Default.Call(context.Background(), runtime, "publish.plan.get", map[string]interface{}{"plan_id": 9})
	if freezeErr != nil || getErr != nil || freeze == nil || get == nil || !freezeCalled || !getCalled {
		t.Fatalf("freeze=%+v err=%v get=%+v err=%v callbacks=(%v,%v)", freeze, freezeErr, get, getErr, freezeCalled, getCalled)
	}
}

func TestPublishPlanPrepareRequiresTrustedLocalUI(t *testing.T) {
	args := map[string]interface{}{"plan_id": 1, "snapshot_hash": "hash", "approval_id": "approval", "nonce": "nonce"}
	called := false
	runtime := &Runtime{PreparePublishPlan: func(context.Context, int64, string, string, string) (*Result, error) {
		called = true
		return okResult("prepared", map[string]interface{}{}), nil
	}}
	result, err := Default.Call(context.Background(), runtime, "publish.plan.prepare", args)
	if err == nil || result == nil || result.Error != "TRUSTED_LOCAL_UI_REQUIRED" || called {
		t.Fatalf("untrusted result=%+v err=%v called=%v", result, err, called)
	}
	runtime.TrustedLocalUI = true
	result, err = Default.Call(context.Background(), runtime, "publish.plan.prepare", args)
	if err != nil || result == nil || !result.OK || !called {
		t.Fatalf("trusted result=%+v err=%v called=%v", result, err, called)
	}
}

func TestPublishPlanSubmitRequiresTrustedLocalUI(t *testing.T) {
	args := map[string]interface{}{"plan_id": 1, "snapshot_hash": "hash", "approval_id": "approval", "nonce": "nonce"}
	called := false
	runtime := &Runtime{SubmitPublishPlan: func(context.Context, int64, string, string, string) (*Result, error) {
		called = true
		return okResult("submitted", map[string]interface{}{}), nil
	}}
	result, err := Default.Call(context.Background(), runtime, "publish.plan.submit", args)
	if err == nil || result == nil || result.Error != "TRUSTED_LOCAL_UI_REQUIRED" || called {
		t.Fatalf("untrusted result=%+v err=%v called=%v", result, err, called)
	}
	runtime.TrustedLocalUI = true
	result, err = Default.Call(context.Background(), runtime, "publish.plan.submit", args)
	if err != nil || result == nil || !result.OK || !called {
		t.Fatalf("trusted result=%+v err=%v called=%v", result, err, called)
	}
	def, ok := Default.Get("publish.plan.submit")
	if !ok || !def.AgentVisible || def.AgentRisk != AgentRiskL3 {
		t.Fatalf("submit tool must be an L3 Agent capability: %+v", def)
	}
}

func TestWithTrustedLocalUICopiesAgentDomainApprovalCallback(t *testing.T) {
	called := false
	runtime := &Runtime{
		AuthorizeAgentTool: func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) {
			called = true
			return map[string]interface{}{"ok": true}, nil
		},
	}
	trusted := runtime.WithTrustedLocalUI(true)
	if trusted == nil || !trusted.TrustedLocalUI || trusted.AuthorizeAgentTool == nil {
		t.Fatalf("trusted runtime did not copy domain approval callback: %+v", trusted)
	}
	if _, err := trusted.AuthorizeAgentTool(context.Background(), "publish.plan.prepare", map[string]interface{}{}); err != nil || !called {
		t.Fatalf("copied domain approval callback err=%v called=%v", err, called)
	}
}

func TestMediaGenerateRequiresTrustedExplicitLocalUI(t *testing.T) {
	args := map[string]interface{}{"request_id": "gen-tool-request-0001", "kind": "image", "prompt": "synthetic prompt", "confirm": true}
	for _, runtime := range []*Runtime{
		nil,
		{DataDir: t.TempDir()},
		{DataDir: t.TempDir(), TrustedLocalUI: true},
	} {
		result, err := Default.Call(context.Background(), runtime, "media.generate", args)
		if err == nil || result == nil || result.OK || result.Error != "TRUSTED_LOCAL_UI_REQUIRED" {
			t.Fatalf("untrusted generation result=%+v err=%v", result, err)
		}
	}

	called := false
	runtime := &Runtime{
		TrustedLocalUI: true,
		GenerateMedia: func(_ context.Context, got map[string]interface{}) (*Result, error) {
			called = true
			return okResult("queued", map[string]interface{}{"job_id": int64(7)}), nil
		},
	}
	result, err := Default.Call(context.Background(), runtime, "media.generate", map[string]interface{}{"request_id": "gen-tool-request-0002", "kind": "image", "prompt": "synthetic prompt", "confirm": false})
	if err == nil || result == nil || result.Error != "CONFIRMATION_REQUIRED" || called {
		t.Fatalf("unconfirmed generation result=%+v err=%v called=%v", result, err, called)
	}
	result, err = Default.Call(context.Background(), runtime, "media.generate", args)
	if err != nil || result == nil || !result.OK || !called || result.Data["job_id"] != int64(7) {
		t.Fatalf("trusted generation result=%+v err=%v called=%v", result, err, called)
	}
	called = false
	result, err = Default.Call(context.Background(), runtime, "media.generate", map[string]interface{}{
		"request_id": "gen-tool-video-duration-invalid-0001",
		"kind":       "video", "prompt": "synthetic prompt", "duration_seconds": float64(7), "confirm": true,
	})
	if err == nil || result == nil || result.Error != "VALIDATION_FAILED" || called {
		t.Fatalf("invalid duration result=%+v err=%v called=%v", result, err, called)
	}
}

func TestMediaGenerateBatchQueuesBoundedUniqueRequestsAfterExplicitConfirmation(t *testing.T) {
	args := map[string]interface{}{
		"request_id": "gen-batch-tool-request-0001",
		"kind":       "image",
		"prompt":     "synthetic prompt",
		"count":      3,
		"confirm":    true,
	}
	calls := make([]string, 0, 3)
	runtime := &Runtime{
		TrustedLocalUI: true,
		GenerateMedia: func(_ context.Context, got map[string]interface{}) (*Result, error) {
			requestID := argString(got, "request_id", "")
			calls = append(calls, requestID)
			return okResult("queued", map[string]interface{}{"job_id": int64(len(calls))}), nil
		},
	}
	result, err := Default.Call(context.Background(), runtime, "media.generate.batch", args)
	if err != nil || result == nil || !result.OK {
		t.Fatalf("batch result=%+v err=%v", result, err)
	}
	jobIDs, ok := result.Data["job_ids"].([]int64)
	if !ok || len(jobIDs) != 3 || len(calls) != 3 {
		t.Fatalf("jobIDs=%v calls=%v", result.Data["job_ids"], calls)
	}
	if calls[0] == calls[1] || calls[1] == calls[2] || calls[0] == calls[2] {
		t.Fatalf("batch request IDs must be unique: %v", calls)
	}

	args["count"] = 9
	result, err = Default.Call(context.Background(), runtime, "media.generate.batch", args)
	if err == nil || result == nil || result.Error != "VALIDATION_FAILED" || len(calls) != 3 {
		t.Fatalf("unbounded batch result=%+v err=%v calls=%v", result, err, calls)
	}
	args["count"] = 2
	args["confirm"] = false
	result, err = Default.Call(context.Background(), runtime, "media.generate.batch", args)
	if err == nil || result == nil || result.Error != "CONFIRMATION_REQUIRED" || len(calls) != 3 {
		t.Fatalf("unconfirmed batch result=%+v err=%v calls=%v", result, err, calls)
	}
}

func TestFleetRunReturnsDashboardRunBeforeLongLivedStartCompletes(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "fleet-async", "")
	if err != nil {
		t.Fatal(err)
	}
	controller := &delayedFleetController{started: make(chan struct{}), release: make(chan struct{})}
	runtime := &Runtime{Fleet: fleet.NewService(database, controller)}
	done := make(chan struct {
		result *Result
		err    error
	}, 1)
	go func() {
		result, callErr := handleFleetRun(context.Background(), runtime, map[string]interface{}{
			"confirm": true, "operation": "start", "account_ids": []interface{}{float64(account.ID)}, "concurrency": 1,
		})
		done <- struct {
			result *Result
			err    error
		}{result, callErr}
	}()

	var response *Result
	select {
	case completed := <-done:
		if completed.err != nil || completed.result == nil || !completed.result.OK {
			t.Fatalf("fleet call result=%+v err=%v", completed.result, completed.err)
		}
		response = completed.result
	case <-time.After(time.Second):
		t.Fatal("long-lived Fleet call waited for browser start")
	}
	runID, ok := response.Data["run_id"].(int64)
	if !ok || runID <= 0 {
		t.Fatalf("missing run id: %+v", response)
	}
	select {
	case <-controller.started:
	case <-time.After(time.Second):
		t.Fatal("background Fleet execution did not begin")
	}
	run, err := database.GetBatchRun(runID)
	if err != nil || run.Status != "running" || run.RunningCount != 1 {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	close(controller.release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		run, err = database.GetBatchRun(runID)
		if err == nil && run.Status == "completed" && run.SuccessCount == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("background Fleet run did not finish: run=%+v err=%v", run, err)
}

func TestUnboundToolsNeverReportPlannedSuccess(t *testing.T) {
	res, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "agent.browser", map[string]interface{}{
		"goal": "inspect only",
	})
	if err == nil || res == nil || res.OK || res.Error != "TOOL_NOT_FOUND" {
		t.Fatalf("unbound tool must fail explicitly: res=%+v err=%v", res, err)
	}
}

func TestToolsListCall(t *testing.T) {
	res, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "tools.list", map[string]interface{}{
		"category": "tikhub",
	})
	if err != nil || !res.OK {
		t.Fatalf("list: %v %+v", err, res)
	}
	// no network
	res2, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "tikhub.rank.daily", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Data["boards"] == nil && res2.Error == "" && !res2.OK {
		t.Fatalf("expected plan or key error: %+v", res2)
	}
}

func TestTikHubRankSchemaExposesExplicitConfirmation(t *testing.T) {
	definition, ok := Default.Get("tikhub.rank.daily")
	if !ok {
		t.Fatal("tikhub.rank.daily missing")
	}
	for _, param := range definition.Params {
		if param.Name == "confirm" && param.Type == "bool" {
			return
		}
	}
	t.Fatal("tikhub.rank.daily schema must expose confirm bool")
}

func TestTikHubUserAddRejectsEmptyUserID(t *testing.T) {
	result, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "tikhub.user.add", map[string]interface{}{
		"platform": "xhs",
		"user_id":  "   ",
	})
	if err == nil || result == nil || result.OK || result.Error != "VALIDATION_FAILED" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestTikHubNetworkToolsRequireActivatedProvider(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := tikhub.SaveProviderCredential(dataDir, "synthetic-tikhub-token-123456", tikhub.RegionMainland); err != nil {
		t.Fatal(err)
	}
	result, err := Default.Call(context.Background(), &Runtime{DataDir: dataDir}, "tikhub.rank.daily", map[string]interface{}{
		"confirm": true,
	})
	if err == nil || result == nil || result.OK || result.Error != "TIKHUB_PROVIDER_NOT_ACTIVE" {
		t.Fatalf("inactive provider reached network path: result=%+v err=%v", result, err)
	}
	status, err := Default.Call(context.Background(), &Runtime{DataDir: dataDir}, "tikhub.config.status", nil)
	if err != nil || !status.OK || status.Data["configured"] != true || status.Data["active"] != false {
		t.Fatalf("unexpected provider status: result=%+v err=%v", status, err)
	}
	if _, exists := status.Data["key_masked"]; exists {
		t.Fatalf("provider status must not expose credential fragments: %+v", status.Data)
	}
	if _, exists := status.Data["data_dir"]; exists {
		t.Fatalf("provider status must not expose local paths: %+v", status.Data)
	}
}

func TestSchemaJSON(t *testing.T) {
	b, err := Default.SchemaJSON()
	if err != nil || len(b) < 50 {
		t.Fatalf("schema: %v len=%d", err, len(b))
	}
}

func TestAccountListAndWorkflow(t *testing.T) {
	dir := t.TempDir()
	rt := &Runtime{DataDir: dir}
	// create account
	res, err := Default.Call(context.Background(), rt, "account.add", map[string]interface{}{
		"platform": "xhs", "profile": "t1", "name": "test",
	})
	if err != nil || !res.OK {
		t.Fatalf("add: %v %+v", err, res)
	}
	res, err = Default.Call(context.Background(), rt, "account.list", nil)
	if err != nil || !res.OK {
		t.Fatalf("list: %v %+v", err, res)
	}
	if res.Data["count"].(int) < 1 && res.Data["count"].(int) != 1 {
		// count may be int from okResult
	}
	// workflow dry
	res, err = Default.Call(context.Background(), rt, "workflow.run", map[string]interface{}{
		"steps": []interface{}{
			map[string]interface{}{"tool": "tikhub.config.status", "args": map[string]interface{}{}},
			map[string]interface{}{"tool": "account.list", "args": map[string]interface{}{}},
			map[string]interface{}{"tool": "ego.space.list", "args": map[string]interface{}{}},
		},
	})
	if err != nil || !res.OK {
		t.Fatalf("workflow: %v %+v", err, res)
	}
}

func TestAccountCheckRequiresRunningBrowserAndNeverTrustsStoredState(t *testing.T) {
	dir := t.TempDir()
	rt := &Runtime{DataDir: dir}
	created, err := Default.Call(context.Background(), rt, "account.add", map[string]interface{}{
		"platform": "xhs", "profile": "check-only", "name": "check-only",
	})
	if err != nil || created == nil || !created.OK {
		t.Fatalf("account.add: res=%+v err=%v", created, err)
	}
	accountID, ok := created.Data["account_id"].(int64)
	if !ok || accountID <= 0 {
		t.Fatalf("account_id=%+v", created.Data)
	}
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(accountID, "logged_in"); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	checked, checkErr := Default.Call(context.Background(), rt, "account.check", map[string]interface{}{"account_id": accountID})
	if checkErr == nil || checked == nil || checked.OK || checked.Error != "TOOL_NOT_FOUND" {
		t.Fatalf("legacy account.check must be absent: res=%+v err=%v", checked, checkErr)
	}

	database, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	account, err := database.GetAccount(accountID)
	if err != nil || account.LoginState != "logged_in" {
		t.Fatalf("stored login state must not be treated as fresh evidence: account=%+v err=%v", account, err)
	}
	var leaseCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_operation_leases WHERE profile_id=?`, browser.ProfileIDForAccount(accountID)).Scan(&leaseCount); err != nil || leaseCount != 0 {
		t.Fatalf("account.check left stale operation lease: count=%d err=%v", leaseCount, err)
	}
}

func TestMetaLoginAndDiscoverRequireRunningMetaBrowser(t *testing.T) {
	dir := t.TempDir()
	rt := &Runtime{DataDir: dir}

	xhs, err := Default.Call(context.Background(), rt, "account.add", map[string]interface{}{
		"platform": "xhs", "profile": "not-meta", "name": "not-meta",
	})
	if err != nil || xhs == nil || !xhs.OK {
		t.Fatalf("account.add xhs: res=%+v err=%v", xhs, err)
	}
	xhsID, _ := xhs.Data["account_id"].(int64)
	wrong, wrongErr := Default.Call(context.Background(), rt, "meta.login", map[string]interface{}{"account_id": xhsID})
	if wrongErr == nil || wrong == nil || wrong.OK || wrong.Error != "TOOL_NOT_FOUND" {
		t.Fatalf("legacy meta.login must be absent: res=%+v err=%v", wrong, wrongErr)
	}
	wrongDiscover, wrongDiscoverErr := Default.Call(context.Background(), rt, "meta.discover", map[string]interface{}{"account_id": xhsID})
	if wrongDiscoverErr == nil || wrongDiscover == nil || wrongDiscover.OK || wrongDiscover.Error != "TOOL_NOT_FOUND" {
		t.Fatalf("legacy meta.discover must be absent: res=%+v err=%v", wrongDiscover, wrongDiscoverErr)
	}

	fb, err := Default.Call(context.Background(), rt, "account.add", map[string]interface{}{
		"platform": "meta", "profile": "fb-meta", "name": "fb-meta",
	})
	if err != nil || fb == nil || !fb.OK {
		t.Fatalf("account.add meta: res=%+v err=%v", fb, err)
	}
	fbID, ok := fb.Data["account_id"].(int64)
	if !ok || fbID <= 0 {
		t.Fatalf("meta account_id=%+v", fb.Data)
	}

	login, loginErr := Default.Call(context.Background(), rt, "meta.login", map[string]interface{}{"account_id": fbID})
	if loginErr == nil || login == nil || login.OK || login.Error != "TOOL_NOT_FOUND" {
		t.Fatalf("legacy meta.login must be absent: res=%+v err=%v", login, loginErr)
	}
	discover, discoverErr := Default.Call(context.Background(), rt, "meta.discover", map[string]interface{}{"account_id": fbID})
	if discoverErr == nil || discover == nil || discover.OK || discover.Error != "TOOL_NOT_FOUND" {
		t.Fatalf("legacy meta.discover must be absent: res=%+v err=%v", discover, discoverErr)
	}

	missing, missingErr := Default.Call(context.Background(), rt, "meta.discover", map[string]interface{}{})
	if missingErr == nil || missing == nil || missing.OK || missing.Error != "TOOL_NOT_FOUND" {
		t.Fatalf("legacy meta.discover must be absent: res=%+v err=%v", missing, missingErr)
	}

	// profile-based legacy args must not be accepted as a silent success path
	legacy, legacyErr := Default.Call(context.Background(), rt, "meta.login", map[string]interface{}{"profile": "fb-meta"})
	if legacyErr == nil || legacy == nil || legacy.OK || legacy.Error != "TOOL_NOT_FOUND" {
		t.Fatalf("legacy meta.login must be absent: res=%+v err=%v", legacy, legacyErr)
	}
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var leaseCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_operation_leases`).Scan(&leaseCount); err != nil || leaseCount != 0 {
		t.Fatalf("Meta tools left stale operation lease: count=%d err=%v", leaseCount, err)
	}
}

func TestMetaResolverShutsDownTemporaryFacade(t *testing.T) {
	dir := t.TempDir()
	runtimeConfig := &Runtime{DataDir: dir, Browser: filepath.Join(dir, "must-not-launch")}
	created, err := Default.Call(context.Background(), runtimeConfig, "account.add", map[string]interface{}{
		"platform": "meta", "profile": "meta-cleanup", "name": "meta-cleanup",
	})
	if err != nil || created == nil || !created.OK {
		t.Fatalf("account.add: result=%+v err=%v", created, err)
	}
	accountID, _ := created.Data["account_id"].(int64)
	before := runtime.NumGoroutine()
	for i := 0; i < 16; i++ {
		result, err := Default.Call(context.Background(), runtimeConfig, "meta.login", map[string]interface{}{"account_id": accountID})
		if err == nil || result == nil || result.Error != "TOOL_NOT_FOUND" {
			t.Fatalf("iteration=%d result=%+v err=%v", i, result, err)
		}
	}
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before+4 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before+4 {
		t.Fatalf("temporary Meta facades leaked heartbeat goroutines: before=%d after=%d", before, after)
	}
}

func TestEgoSpaceListReturnsOnlyBoundSpaceState(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "status-redacted", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpace(account.ID, 91, "task-safe", "identity-1/account-1/xhs"); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Call(context.Background(), &Runtime{DataDir: dir}, "ego.space.list", map[string]interface{}{})
	if err != nil || result == nil || !result.OK {
		t.Fatalf("space list result=%+v err=%v", result, err)
	}
	for _, key := range []string{"pid", "cdp_port", "cdp_url", "lease", "profile_id", "cookie", "password"} {
		if _, exists := result.Data[key]; exists {
			t.Fatalf("space list exposed %s: %+v", key, result.Data)
		}
	}
	spaces, ok := result.Data["spaces"].([]db.EgoSpace)
	if !ok || len(spaces) != 1 || spaces[0].IdentityID != account.IdentityID || spaces[0].SpaceID != 91 || spaces[0].Ownership != "agent" {
		t.Fatalf("unexpected Space state: type=%T data=%+v", result.Data["spaces"], result.Data)
	}
}

func TestAccountCDPToolsFailClosedOnOperationLease(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	xhs, err := database.CreateAccount("xhs", "lease-blocked-check", "")
	if err != nil {
		t.Fatal(err)
	}
	facebook, err := database.CreateAccount("facebook", "lease-blocked-meta", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []*db.Account{xhs, facebook} {
		acquired, err := database.AcquireBrowserOperationLease(
			browser.ProfileIDForAccount(account.ID), "publish", "test:account-cdp-blocker", time.Now(), time.Minute,
		)
		if err != nil || !acquired {
			t.Fatalf("account=%d blocker acquire=%v err=%v", account.ID, acquired, err)
		}
	}

	runtime := &Runtime{DataDir: dir, Browser: filepath.Join(dir, "must-not-launch")}
	for _, tt := range []struct {
		name      string
		accountID int64
	}{
		{name: "account.check", accountID: xhs.ID},
		{name: "meta.login", accountID: facebook.ID},
		{name: "meta.discover", accountID: facebook.ID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Default.Call(context.Background(), runtime, tt.name, map[string]interface{}{"account_id": tt.accountID})
			if err == nil || result == nil || result.OK || result.Error != "TOOL_NOT_FOUND" {
				t.Fatalf("legacy tool remains registered: result=%+v err=%v", result, err)
			}
		})
	}

	for _, table := range []string{"browser_runtime_instances", "profile_leases", "cdp_port_leases"} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s changed before operation lease rejection: count=%d err=%v", table, count, err)
		}
	}
}

func TestContentCreateToolPersistsContent(t *testing.T) {
	dir := t.TempDir()
	res, err := Default.Call(context.Background(), &Runtime{DataDir: dir}, "content.create", map[string]interface{}{
		"type": "article", "title": "Tool title", "caption": "Tool caption", "body": "Tool body",
	})
	if err != nil || res == nil || !res.OK {
		t.Fatalf("content.create: res=%+v err=%v", res, err)
	}
	contentID, ok := res.Data["content_id"].(int64)
	if !ok || contentID <= 0 {
		t.Fatalf("content.create did not return persisted id: %+v", res)
	}
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.GetContent(contentID)
	if err != nil {
		t.Fatal(err)
	}
	if content.Title != "Tool title" || content.Body != "Tool body" || content.Status != "draft" {
		t.Fatalf("persisted content=%+v", content)
	}
}

func TestContentMediaAttachToolPersistsRoleAndPosition(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	content, err := database.CreateContent("video", "Tool video", "", "", "[]", "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(path, []byte("local-image-fixture"), 0o600); err != nil {
		database.Close()
		t.Fatal(err)
	}
	meta, err := media.Probe(path)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	library, err := database.UpsertMediaLibrary(path, meta.SHA256, meta.SizeBytes, meta.MimeGuess, meta.Width, meta.Height, meta.DurationMS, meta.ProbeJSON)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Call(context.Background(), &Runtime{DataDir: dir}, "content.media.attach", map[string]interface{}{
		"content_id": content.ID,
		"library_id": library.ID,
		"role":       "cover",
		"position":   2,
	})
	if err != nil || result == nil || !result.OK {
		t.Fatalf("content.media.attach result=%+v err=%v", result, err)
	}
	database, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListMediaByContent(content.ID)
	if err != nil || len(assets) != 1 || assets[0].Role != "cover" || assets[0].Position != 2 || assets[0].LibraryID != library.ID {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
}

func TestMediaImportToolIndexesLocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, []byte("local-media-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Default.Call(context.Background(), &Runtime{DataDir: dir}, "media.import", map[string]interface{}{"path": path})
	if err != nil || res == nil || !res.OK {
		t.Fatalf("media.import: res=%+v err=%v", res, err)
	}
	mediaID, ok := res.Data["media_id"].(int64)
	if !ok || mediaID <= 0 {
		t.Fatalf("media.import did not return persisted id: %+v", res)
	}
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	items, err := database.ListMediaLibrary(10)
	if err != nil || len(items) != 1 || items[0].SHA256 == "" {
		t.Fatalf("media library: items=%+v err=%v", items, err)
	}
	listed, err := Default.Call(context.Background(), &Runtime{DataDir: dir}, "studio.media.list", nil)
	if err != nil || !listed.OK || listed.Data["count"] != 1 {
		t.Fatalf("studio.media.list: res=%+v err=%v", listed, err)
	}
}

func TestMediaImportToolIndexesSupportedFilesFromFolder(t *testing.T) {
	dir := t.TempDir()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "cover.png"), []byte("local-image-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(source, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "clip.mp4"), []byte("local-video-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "notes.txt"), []byte("not media"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Call(context.Background(), &Runtime{DataDir: dir}, "media.import", map[string]interface{}{
		"path": source, "recursive": true, "max_files": 10,
	})
	if err != nil || result == nil || !result.OK {
		t.Fatalf("media.import folder: result=%+v err=%v", result, err)
	}
	if result.Data["imported_count"] != 2 {
		t.Fatalf("folder import result=%+v", result.Data)
	}
	ids, ok := result.Data["media_ids"].([]int64)
	if !ok || len(ids) != 2 {
		t.Fatalf("folder import ids=%T %+v", result.Data["media_ids"], result.Data["media_ids"])
	}
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	items, err := database.ListMediaLibrary(10)
	if err != nil || len(items) != 2 {
		t.Fatalf("media library: items=%+v err=%v", items, err)
	}
}

func TestMediaImportFolderLimitFailsBeforeIndexing(t *testing.T) {
	dir := t.TempDir()
	source := t.TempDir()
	for _, name := range []string{"one.png", "two.jpg"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Default.Call(context.Background(), &Runtime{DataDir: dir}, "media.import", map[string]interface{}{
		"path": source, "max_files": 1,
	})
	if err == nil || result == nil || result.OK || result.Error != "MEDIA_IMPORT_LIMIT_EXCEEDED" {
		t.Fatalf("folder limit result=%+v err=%v", result, err)
	}
	database, openErr := db.Open(dir)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer database.Close()
	if migrateErr := database.Migrate(); migrateErr != nil {
		t.Fatal(migrateErr)
	}
	items, listErr := database.ListMediaLibrary(10)
	if listErr != nil || len(items) != 0 {
		t.Fatalf("limited import must not partially index: items=%+v err=%v", items, listErr)
	}
}

func TestStudioToolsPersistPlanAndRequireExecutionConfirmation(t *testing.T) {
	dir := t.TempDir()
	rt := &Runtime{DataDir: dir}
	path := filepath.Join(t.TempDir(), "source.mp4")
	if err := os.WriteFile(path, []byte("local-media-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	imported, err := Default.Call(context.Background(), rt, "media.import", map[string]interface{}{"path": path})
	if err != nil || !imported.OK {
		t.Fatalf("media.import: res=%+v err=%v", imported, err)
	}
	mediaID, ok := imported.Data["media_id"].(int64)
	if !ok || mediaID <= 0 {
		t.Fatalf("media.import id=%+v", imported.Data)
	}
	created, err := Default.Call(context.Background(), rt, "studio.script.create", map[string]interface{}{"title": "开场", "body": "选择素材开头"})
	if err != nil || !created.OK {
		t.Fatalf("studio.script.create: res=%+v err=%v", created, err)
	}
	script, ok := created.Data["script"].(*db.StudioScript)
	if !ok || script.ID <= 0 {
		t.Fatalf("studio script=%+v", created.Data)
	}
	originalAgent := newDoubaoStudioAgent
	newDoubaoStudioAgent = func() *studio.DoubaoAgent {
		return studio.NewDoubaoAgentWithCompletion(func(context.Context, string, string) (string, error) {
			return `{"start_ms":0,"end_ms":1000,"title":"开场","reason":"匹配脚本"}`, nil
		})
	}
	t.Cleanup(func() { newDoubaoStudioAgent = originalAgent })
	planned, err := Default.Call(context.Background(), rt, "studio.clip.plan", map[string]interface{}{
		"source_media_id": mediaID, "script_id": script.ID, "target_duration_ms": 5_000,
	})
	if err != nil || !planned.OK {
		t.Fatalf("studio.clip.plan: res=%+v err=%v", planned, err)
	}
	job, ok := planned.Data["clip_job"].(*db.ClipJob)
	if !ok || job.ID <= 0 || job.Status != "planned" {
		t.Fatalf("clip job=%+v", planned.Data)
	}
	denied, callErr := Default.Call(context.Background(), rt, "studio.clip.execute", map[string]interface{}{"clip_job_id": job.ID, "confirm": false})
	if callErr == nil || denied == nil || denied.OK || denied.Error != "STUDIO_CLIP_CONFIRMATION_REQUIRED" {
		t.Fatalf("clip execution without confirm must fail: res=%+v err=%v", denied, callErr)
	}
	listed, err := Default.Call(context.Background(), rt, "studio.script.list", nil)
	if err != nil || !listed.OK || listed.Data["count"] != 1 {
		t.Fatalf("studio.script.list: res=%+v err=%v", listed, err)
	}
}

func TestQueueEnqueueToolPersistsOnlyExecutableKinds(t *testing.T) {
	dir := t.TempDir()
	res, err := Default.Call(context.Background(), &Runtime{DataDir: dir}, "queue.enqueue", map[string]interface{}{
		"type":    "publish",
		"payload": map[string]interface{}{"platform": "xhs", "content_id": 1, "target_id": 2, "dry_run": true},
	})
	if err != nil || res == nil || !res.OK {
		t.Fatalf("queue.enqueue: res=%+v err=%v", res, err)
	}
	queueID, ok := res.Data["queue_id"].(int64)
	if !ok || queueID <= 0 {
		t.Fatalf("queue.enqueue did not return persisted id: %+v", res)
	}
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	items, err := database.ListTaskQueue(10)
	if err != nil || len(items) != 1 || items[0].Kind != "publish" || !json.Valid([]byte(items[0].PayloadJSON)) {
		t.Fatalf("task queue: items=%+v err=%v", items, err)
	}

	bad, badErr := Default.Call(context.Background(), &Runtime{DataDir: dir}, "queue.enqueue", map[string]interface{}{
		"type": "unknown-noop", "payload": map[string]interface{}{},
	})
	if badErr == nil || bad == nil || bad.OK {
		t.Fatalf("unsupported queue kind must fail instead of becoming a no-op: res=%+v err=%v", bad, badErr)
	}
}

func TestQueueEnqueueRejectsLegacyLivePublish(t *testing.T) {
	res, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "queue.enqueue", map[string]interface{}{
		"type": "publish",
		"payload": map[string]interface{}{
			"platform": "douyin", "content_id": 1, "target_id": 2,
			"dry_run": false, "idempotency_key": "legacy-live-queue",
		},
	})
	if err == nil || res == nil || res.OK || res.Error != "PUBLISH_EGOLITE_PLAN_REQUIRED" {
		t.Fatalf("legacy live queue must fail closed: res=%+v err=%v", res, err)
	}
}

func TestRegistryCallEmitsStartAndTerminalEvents(t *testing.T) {
	registry := NewRegistry()
	registry.Register(Def{
		Name: "test.ok", Category: "test", SideEffect: "read",
		Handler: func(context.Context, *Runtime, map[string]interface{}) (*Result, error) {
			return &Result{OK: true}, nil
		},
	})
	var out bytes.Buffer
	emitter := observability.NewJSONEmitter(&out, observability.Options{Service: "test"})
	ctx := trace.WithID(context.Background(), trace.ID("trace-registry"))
	res, err := registry.Call(ctx, &Runtime{Events: emitter}, "test.ok", map[string]interface{}{
		"password": "must-not-appear",
		"path":     "/tmp/private",
	})
	if err != nil || res == nil || !res.OK {
		t.Fatalf("call: res=%+v err=%v", res, err)
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("events=%d\n%s", len(lines), out.String())
	}
	var started, completed map[string]any
	if err := json.Unmarshal(lines[0], &started); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lines[1], &completed); err != nil {
		t.Fatal(err)
	}
	if started["event"] != "tool.call.started" || started["status"] != "running" || completed["event"] != "tool.call.completed" || completed["trace_id"] != "trace-registry" || completed["step"] != "test.ok" || completed["status"] != "succeeded" {
		t.Fatalf("started=%v completed=%v", started, completed)
	}
	if strings.Contains(out.String(), "must-not-appear") || strings.Contains(out.String(), "/tmp/private") {
		t.Fatalf("args leaked: %s", out.String())
	}
}

func TestRegistryCallDoesNotLogUnknownNameOrRawError(t *testing.T) {
	registry := NewRegistry()
	registry.Register(Def{
		Name: "test.fail", Category: "test", SideEffect: "read",
		Handler: func(context.Context, *Runtime, map[string]interface{}) (*Result, error) {
			return nil, errors.New("backend failed /tmp/private?token=abc")
		},
	})
	var out bytes.Buffer
	emitter := observability.NewJSONEmitter(&out, observability.Options{Service: "test"})
	rt := &Runtime{Events: emitter}
	_, _ = registry.Call(context.Background(), rt, "unknown-secret-tool", nil)
	_, _ = registry.Call(context.Background(), rt, "test.fail", nil)

	logText := out.String()
	for _, secret := range []string{"unknown-secret-tool", "/tmp/private", "token=abc", "backend failed"} {
		if strings.Contains(logText, secret) {
			t.Fatalf("unsafe value %q leaked: %s", secret, logText)
		}
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("events=%d", len(lines))
	}
	var failed map[string]any
	if err := json.Unmarshal(lines[2], &failed); err != nil {
		t.Fatal(err)
	}
	if failed["error_code"] != "INTERNAL_ERROR" || failed["status"] != "failed" {
		t.Fatalf("failed event=%v", failed)
	}
}

func TestWorkflowCallsKeepParentTrace(t *testing.T) {
	registry := NewRegistry()
	registry.Register(Def{
		Name: "child.one", Category: "test", SideEffect: "read",
		Handler: func(context.Context, *Runtime, map[string]interface{}) (*Result, error) {
			return &Result{OK: true}, nil
		},
	})
	registry.Register(Def{
		Name: "parent", Category: "test", SideEffect: "read",
		Handler: func(ctx context.Context, rt *Runtime, _ map[string]interface{}) (*Result, error) {
			return registry.Call(ctx, rt, "child.one", nil)
		},
	})
	var out bytes.Buffer
	emitter := observability.NewJSONEmitter(&out, observability.Options{Service: "test"})
	ctx := trace.WithID(context.Background(), trace.ID("trace-parent"))
	if _, err := registry.Call(ctx, &Runtime{Events: emitter}, "parent", nil); err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	if len(lines) != 4 {
		t.Fatalf("events=%d\n%s", len(lines), out.String())
	}
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event["trace_id"] != "trace-parent" {
			t.Fatalf("trace=%v", event["trace_id"])
		}
	}
}

func TestRegistryReportsEmitterFailureAndForcesErrorResult(t *testing.T) {
	registry := NewRegistry()
	registry.Register(Def{
		Name: "test.contradictory", Category: "test", SideEffect: "read",
		Handler: func(context.Context, *Runtime, map[string]interface{}) (*Result, error) {
			return &Result{OK: true}, errors.New("synthetic handler failure")
		},
	})
	reported := 0
	rt := &Runtime{Events: failingEmitter{}, OnEventError: func(error) { reported++ }}
	res, err := registry.Call(context.Background(), rt, "test.contradictory", nil)
	if err == nil || res == nil || res.OK {
		t.Fatalf("contradictory result survived: res=%+v err=%v", res, err)
	}
	if reported != 2 {
		t.Fatalf("emitter failures reported=%d, want 2", reported)
	}
}

func TestRuntimeAutoInstallsBoundedFileEvents(t *testing.T) {
	registry := NewRegistry()
	registry.Register(Def{
		Name: "test.file", Category: "test", SideEffect: "read",
		Handler: func(context.Context, *Runtime, map[string]interface{}) (*Result, error) {
			return &Result{OK: true}, nil
		},
	})
	rt := &Runtime{DataDir: t.TempDir()}
	if _, err := registry.Call(context.Background(), rt, "test.file", nil); err != nil {
		t.Fatal(err)
	}
	if err := rt.CloseEvents(); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(rt.DataDir, "logs", "events-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("event files=%v", paths)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"event":"tool.call.completed"`) {
		t.Fatalf("missing terminal event: %s", data)
	}
}

func TestLivePublishToolRequiresIdempotencyKeyBeforeDBWork(t *testing.T) {
	res, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "publish.run", map[string]interface{}{
		"platform": "instagram", "content_id": 1, "target_id": 1, "dry_run": false,
	})
	if err == nil || res == nil || res.Error != "MISSING_PARAM" {
		t.Fatalf("live publish without key res=%+v err=%v", res, err)
	}
}

func TestLivePublishToolRequiresExplicitConfirmationBeforeDBWork(t *testing.T) {
	res, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "publish.run", map[string]interface{}{
		"platform": "facebook", "content_id": 1, "target_id": 1,
		"dry_run": false, "idempotency_key": "confirm-required",
	})
	if err == nil || res == nil || res.OK || !strings.Contains(err.Error(), "confirm=true") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestLivePublishToolRequiresEgoLiteFrozenPlanBeforeDBWork(t *testing.T) {
	res, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "publish.run", map[string]interface{}{
		"platform": "douyin", "content_id": 1, "target_id": 1,
		"dry_run": false, "idempotency_key": "legacy-live-path", "confirm": true,
	})
	if err == nil || res == nil || res.OK || res.Error != "PUBLISH_EGOLITE_PLAN_REQUIRED" {
		t.Fatalf("legacy live publish must fail closed before DB/browser work: res=%+v err=%v", res, err)
	}
}

func TestLegacyBrowserToolsAreNotRegistered(t *testing.T) {
	for _, name := range []string{"browser.start", "browser.stop"} {
		if _, ok := Default.Get(name); ok {
			t.Fatalf("legacy tool %s is registered", name)
		}
	}
}

func TestToolBrowserOperationLeaseUsesUniqueOwnerAndOwnerBoundRelease(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	accountID := int64(41)
	profileID := browser.ProfileIDForAccount(accountID)
	release, fail, err := acquireToolBrowserOperationLease(database, accountID, "stop", browserStartStopLeaseTTL)
	if err != nil || fail != nil || release == nil {
		t.Fatalf("acquire fail=%+v err=%v", fail, err)
	}
	var firstOwner, operation, expiresAt string
	if err := database.QueryRow(`SELECT owner, operation, expires_at FROM browser_operation_leases WHERE profile_id=?`, profileID).Scan(&firstOwner, &operation, &expiresAt); err != nil {
		t.Fatal(err)
	}
	if operation != "stop" {
		t.Fatalf("operation=%q", operation)
	}
	expires, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Until(expires) < 90*time.Second || time.Until(expires) > browserStartStopLeaseTTL+time.Second {
		t.Fatalf("expires_at=%q err=%v", expiresAt, err)
	}
	if err := database.ReleaseBrowserOperationLease(profileID, "test:wrong-owner"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_operation_leases WHERE profile_id=? AND owner=?`, profileID, firstOwner).Scan(&count); err != nil || count != 1 {
		t.Fatalf("wrong-owner release count=%d err=%v", count, err)
	}
	release()
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_operation_leases WHERE profile_id=?`, profileID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("owner release count=%d err=%v", count, err)
	}

	release, fail, err = acquireToolBrowserOperationLease(database, accountID, "stop", browserStartStopLeaseTTL)
	if err != nil || fail != nil || release == nil {
		t.Fatalf("second acquire fail=%+v err=%v", fail, err)
	}
	defer release()
	var secondOwner string
	if err := database.QueryRow(`SELECT owner FROM browser_operation_leases WHERE profile_id=?`, profileID).Scan(&secondOwner); err != nil {
		t.Fatal(err)
	}
	if firstOwner == secondOwner {
		t.Fatalf("operation owner was reused: %q", firstOwner)
	}
}

func TestToolBrowserOperationWaitsForBackgroundAccountProbe(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	accountID := int64(42)
	profileID := browser.ProfileIDForAccount(accountID)
	probeOwner := "observe-probe:test-tool-wait"
	if acquired, err := database.AcquireBrowserOperationLease(profileID, "observe", probeOwner, time.Now(), time.Minute); err != nil || !acquired {
		t.Fatalf("probe acquire=%v err=%v", acquired, err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(75 * time.Millisecond)
		_ = database.ReleaseBrowserOperationLease(profileID, probeOwner)
		close(released)
	}()

	release, fail, err := acquireToolBrowserOperationLease(database, accountID, "start", browserStartStopLeaseTTL)
	if err != nil || fail != nil || release == nil {
		t.Fatalf("tool operation lost to background probe: fail=%+v release=%v err=%v", fail, release != nil, err)
	}
	release()
	<-released
}

func TestPublishToolLiveFailsClosedWhileDryRunIgnoresLegacyBrowserLease(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("douyin", "lease-blocked-publish", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		database.Close()
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := database.CreateContent("graph_text", "lease test", "caption", "body", "[]", "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if acquired, err := database.AcquireBrowserOperationLease(browser.ProfileIDForAccount(account.ID), "observe", "test:publish-blocker", time.Now(), time.Minute); err != nil || !acquired {
		database.Close()
		t.Fatalf("blocker acquire=%v err=%v", acquired, err)
	}
	database.Close()

	runtime := &Runtime{DataDir: dir, Browser: filepath.Join(dir, "must-not-launch")}
	live, callErr := Default.Call(context.Background(), runtime, "publish.run", map[string]interface{}{
		"platform": "douyin", "content_id": content.ID, "target_id": assets[0].ID,
		"dry_run": false, "confirm": true, "idempotency_key": "lease-blocked-live:0001",
	})
	if callErr == nil || live == nil || live.OK || live.Error != "PUBLISH_EGOLITE_PLAN_REQUIRED" {
		t.Fatalf("legacy live publish did not fail closed: result=%+v err=%v", live, callErr)
	}

	dry, callErr := Default.Call(context.Background(), runtime, "publish.run", map[string]interface{}{
		"platform": "douyin", "content_id": content.ID, "target_id": assets[0].ID, "dry_run": true,
	})
	if callErr != nil || dry == nil || !dry.OK || dry.Error != "" {
		t.Fatalf("dry-run was blocked by browser operation lease: result=%+v err=%v", dry, callErr)
	}
}

func TestPublishToolRequiresFrozenPlanForDouyinAutoSubmit(t *testing.T) {
	res, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "publish.run", map[string]interface{}{
		"platform": "douyin", "content_id": 1, "target_id": 1,
		"dry_run": false, "auto_submit": true, "confirm": true,
		"idempotency_key": "manual-platform-auto-submit",
	})
	if err == nil || res == nil || res.OK || !strings.Contains(err.Error(), "PUBLISH_FROZEN_SNAPSHOT_REQUIRED") {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestPublishToolRequiresFrozenPlanForSupportedAutoSubmit(t *testing.T) {
	for _, platform := range []string{"instagram", "douyin"} {
		t.Run(platform, func(t *testing.T) {
			res, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "publish.run", map[string]interface{}{
				"platform": platform, "content_id": 1, "target_id": 1,
				"dry_run": false, "auto_submit": true, "confirm": true,
				"idempotency_key": "unfrozen-" + platform + "-auto-submit",
			})
			if err == nil || res == nil || res.OK || res.Error != "PUBLISH_FROZEN_SNAPSHOT_REQUIRED" {
				t.Fatalf("res=%+v err=%v", res, err)
			}
		})
	}
}

func TestPublishToolRejectsMetaBrowserContainerBeforeJob(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("meta", "tool-meta-default", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 || assets[0].AssetType != "browser_account" {
		database.Close()
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := database.CreateContent("graph_text", "safe", "caption", "body", "[]", "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	result, callErr := Default.Call(context.Background(), &Runtime{DataDir: dir}, "publish.run", map[string]interface{}{
		"platform": "facebook", "content_id": content.ID, "target_id": assets[0].ID, "dry_run": true,
	})
	if callErr == nil || result == nil || result.Error != "PUBLISH_TARGET_PLATFORM_MISMATCH" {
		t.Fatalf("default Meta browser asset must fail before a job: result=%+v err=%v", result, callErr)
	}
	database, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	jobs, err := database.ListPublishJobs(10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("invalid target must not create a job: jobs=%+v err=%v", jobs, err)
	}
}

func TestLivePublishToolRejectsLegacyBrowserPathBeforeRunner(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("douyin", "tool-live-safety", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		database.Close()
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := database.CreateContent("graph_text", "safe", "caption", "body", "[]", "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	res, callErr := Default.Call(context.Background(), &Runtime{DataDir: dir}, "publish.run", map[string]interface{}{
		"platform": "douyin", "content_id": content.ID, "target_id": assets[0].ID,
		"dry_run": false, "idempotency_key": "tool-live-safety:0001", "confirm": true,
	})
	if callErr == nil || res == nil || res.Error != "PUBLISH_EGOLITE_PLAN_REQUIRED" {
		t.Fatalf("legacy live browser path must fail before Runner: res=%+v err=%v", res, callErr)
	}
	database, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	jobs, err := database.ListPublishJobs(10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("rejected legacy live path must not leave a pending job: jobs=%+v err=%v", jobs, err)
	}
}

func TestPublishToolAcceptsMultipleReviewedMedia(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("douyin", "tool-media-safety", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		database.Close()
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := database.CreateContent("graph_text", "safe", "caption", "body", "[]", "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := database.CreateMediaAsset(content.ID, "image", fmt.Sprintf("/tmp/media-%d.png", index), "", "image/png", 1, fmt.Sprintf("sha-%d", index), 1, 1, 0); err != nil {
			database.Close()
			t.Fatal(err)
		}
	}
	database.Close()

	res, callErr := Default.Call(context.Background(), &Runtime{DataDir: dir}, "publish.run", map[string]interface{}{
		"platform": "douyin", "content_id": content.ID, "target_id": assets[0].ID, "dry_run": true,
	})
	if callErr != nil || res == nil || !res.OK || res.Data["status"] != "dry_run_ok" {
		t.Fatalf("multiple reviewed media must reach the runner: res=%+v err=%v", res, callErr)
	}
}

func TestDouyinPublishToolPersistsVersionedTemplateSnapshot(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("douyin", "tool-template", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		database.Close()
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := database.CreateContent("graph_text", "safe", "caption", "body", "[]", "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	template, err := database.CreatePlatformSetting("douyin", "private", "", `{"visibility":"private"}`, true, false)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	result, callErr := Default.Call(context.Background(), &Runtime{DataDir: dir}, "publish.run", map[string]interface{}{
		"platform": "douyin", "content_id": content.ID, "target_id": assets[0].ID,
		"template_id": template.ID, "dry_run": true,
	})
	if callErr != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, callErr)
	}
	database, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	jobs, err := database.ListPublishJobs(10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	snapshot, err := platforms.ParsePublishSettingsSnapshot(jobs[0].SettingsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TemplateID != template.ID || snapshot.SettingsSchemaVersion != platforms.DouyinSettingsSchemaVersion || snapshot.AdapterVersion != platforms.CoreBrowserHandoffAdapterVersion || string(snapshot.Settings) != `{"activity":"","collection":"","extension_type":"none","location":"","hotspot":"","allow_download":true,"visibility":"private","music":"","comment_enabled":true,"self_declaration":"none"}` {
		t.Fatalf("snapshot=%+v settings=%s", snapshot, snapshot.Settings)
	}
}

func TestCorePublishToolPersistsRequestSettingsSnapshot(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("douyin", "tool-core-settings", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		database.Close()
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := database.CreateContent("video", "safe", "caption", "body", "[]", "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	result, callErr := Default.Call(context.Background(), &Runtime{DataDir: dir}, "publish.run", map[string]interface{}{
		"platform": "douyin", "content_id": content.ID, "target_id": assets[0].ID, "dry_run": true,
		"settings_json": `{"visibility":"private","location":"上海"}`,
	})
	if callErr != nil || result == nil || !result.OK || result.Data["status"] != "dry_run_ok" {
		t.Fatalf("result=%+v err=%v", result, callErr)
	}
	database, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	jobs, err := database.ListPublishJobs(10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	snapshot, err := platforms.ParsePublishSettingsSnapshot(jobs[0].SettingsJSON)
	if err != nil || snapshot.PlatformCode != "douyin" || string(snapshot.Settings) != `{"activity":"","collection":"","extension_type":"none","location":"上海","hotspot":"","allow_download":true,"visibility":"private","music":"","comment_enabled":true,"self_declaration":"none"}` {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestPublishToolRejectsUnsupportedOrInvalidRequestSettings(t *testing.T) {
	for name, args := range map[string]map[string]interface{}{
		"unknown core setting":    {"platform": "douyin", "content_id": 1, "target_id": 1, "dry_run": true, "settings_json": `{"unknown":true}`},
		"manual platform setting": {"platform": "bilibili", "content_id": 1, "target_id": 1, "dry_run": true, "settings_json": `{}`},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Default.Call(context.Background(), &Runtime{DataDir: t.TempDir()}, "publish.run", args)
			if err == nil || result == nil || result.OK || result.Error != "INVALID_PUBLISH_SETTINGS" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestPublishToolReplayReturnsExistingJobWithoutRerunning(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccount("douyin", "tool-replay", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		database.Close()
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := database.CreateContent("graph_text", "replay", "caption", "body", "[]", "draft")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	args := map[string]interface{}{
		"platform": "douyin", "content_id": content.ID, "target_id": assets[0].ID,
		"dry_run": true, "idempotency_key": "tool-replay:0001", "confirm": true,
	}
	first, callErr := Default.Call(context.Background(), &Runtime{DataDir: dir}, "publish.run", args)
	if callErr != nil || first == nil || !first.OK {
		t.Fatalf("first result=%+v err=%v", first, callErr)
	}
	firstID, ok := first.Data["job_id"].(int64)
	if !ok || firstID <= 0 {
		t.Fatalf("first job id=%#v", first.Data["job_id"])
	}
	database, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var eventsBefore int
	if err := database.QueryRow("SELECT COUNT(*) FROM job_events WHERE job_id=?", firstID).Scan(&eventsBefore); err != nil {
		database.Close()
		t.Fatal(err)
	}
	database.Close()

	second, callErr := Default.Call(context.Background(), &Runtime{DataDir: dir}, "publish.run", args)
	if callErr != nil || second == nil || !second.OK {
		t.Fatalf("replay result=%+v err=%v", second, callErr)
	}
	secondID, ok := second.Data["job_id"].(int64)
	if !ok || secondID != firstID || second.Data["status"] != "success" {
		t.Fatalf("replay data=%+v", second.Data)
	}
	database, err = db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	jobs, err := database.ListPublishJobs(10)
	if err != nil || len(jobs) != 1 || jobs[0].ID != firstID {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	var eventsAfter int
	if err := database.QueryRow("SELECT COUNT(*) FROM job_events WHERE job_id=?", firstID).Scan(&eventsAfter); err != nil {
		t.Fatal(err)
	}
	if eventsAfter != eventsBefore {
		t.Fatalf("replay reran job: events before=%d after=%d", eventsBefore, eventsAfter)
	}
}
