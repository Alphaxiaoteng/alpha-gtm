package egolite_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
)

// programFailingRunner fails exactly the program a test wants to die, the way a
// killed or cancelled runtime call does, and records every program so the
// deferred release can be observed.
type programFailingRunner struct {
	*fakeProgramRunner
	failOn string
	err    error
}

func (runner *programFailingRunner) Run(ctx context.Context, program string, result any) error {
	if strings.Contains(program, runner.failOn) {
		runner.calls = append(runner.calls, program)
		return runner.err
	}
	return runner.fakeProgramRunner.Run(ctx, program, result)
}

func lastProgramMatching(t *testing.T, programs []string, fragment string) string {
	t.Helper()
	for index := len(programs) - 1; index >= 0; index-- {
		if strings.Contains(programs[index], fragment) {
			return programs[index]
		}
	}
	t.Fatalf("no runtime program contains %q: programs=%d", fragment, len(programs))
	return ""
}

// The QR program claims or takes over the Space before it reads the page, so a
// failure in the page step used to skip the handoff and leave the Space
// agent-occupied. The injected failure proves the release now runs anyway.
func TestLoginQRHandsOffAfterAnInjectedPageFailure(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("二维码失败也要释放")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{loginQR: map[string]any{
		"state":       "qr_ready",
		"imageBase64": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB",
		"pageOrigin":  "https://creator.douyin.com/",
	}}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.OpenLoginQR(context.Background(), account.ID, true); err != nil {
		t.Fatal(err)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	program := lastProgramMatching(t, runner.calls, "const loginQR =")
	assertJavaScriptSyntax(t, program)
	assertLoginProgramHandsOffAfterInjectedFailure(t, program, space.SpaceID)
}

// A runtime that never answers cannot run the generated release, so the service
// releases the Space itself instead of returning the error and leaving it stuck.
func TestLoginQRReleasesSpaceWhenTheRuntimeCallDies(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("二维码运行被中断")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &programFailingRunner{
		fakeProgramRunner: &fakeProgramRunner{},
		failOn:            "const loginQR =",
		err:               errors.New("runtime killed"),
	}
	service := egolite.NewSpaceService(database, runner)
	result, err := service.OpenLoginQR(context.Background(), account.ID, true)
	if result != nil || err == nil || err.Error() != "runtime killed" {
		t.Fatalf("result=%+v err=%v, want the original runtime failure", result, err)
	}
	release := lastProgramMatching(t, runner.calls, "const stuckSpace =")
	if !strings.Contains(release, "await handOffTaskSpace(") {
		t.Fatalf("the deferred release never hands the Space back: %s", release)
	}
}

func TestLoginBatchHandsOffAfterAnInjectedTabFailure(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("批量登录失败也要释放")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", ""); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.OpenLoginBatch(context.Background(), identity.ID, []string{"xhs"}); err != nil {
		t.Fatal(err)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	program := runner.calls[len(runner.calls)-1]
	if !strings.Contains(program, "openOrReuseTab(") {
		t.Fatalf("picked the wrong runtime program for the login batch: %s", program)
	}
	assertJavaScriptSyntax(t, program)
	assertLoginProgramHandsOffAfterInjectedFailure(t, program, space.SpaceID)
}

func TestOpenLoggedInPlatformsHandsOffAfterAnInjectedTabFailure(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("打开已登录平台失败也要释放")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 89, "task-89", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{spaceProbe: map[string]any{
		"id": 89, "taskId": "task-89", "name": "alpha-nexus/matrix-1", "ownership": "agent",
	}}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.OpenLoggedInPlatformsForCodes(context.Background(), identity.ID, 0, []string{"douyin"}); err != nil {
		t.Fatal(err)
	}
	program := runner.calls[len(runner.calls)-1]
	if !strings.Contains(program, "openOrReuseTab(") {
		t.Fatalf("picked the wrong runtime program for the open-logged-in batch: %s", program)
	}
	assertJavaScriptSyntax(t, program)
	assertLoginProgramHandsOffAfterInjectedFailure(t, program, 89)
}

// racySpaceRunner models the real bind program: it lists the Spaces it knows,
// then waits long enough for a second caller to read the same listing, and only
// then creates. Two unserialized binders therefore get a Space each, which is
// the orphan runtime Space the ensure lock prevents.
type racySpaceRunner struct {
	mu       sync.Mutex
	delay    time.Duration
	taskName string
	spaces   map[string]int64
	nextID   int64
	created  int
}

func (runner *racySpaceRunner) Run(_ context.Context, program string, result any) error {
	if !strings.Contains(program, "const boundSpace =") {
		return fmt.Errorf("unexpected program: %s", program)
	}
	runner.mu.Lock()
	known, listed := runner.spaces[runner.taskName]
	runner.mu.Unlock()
	time.Sleep(runner.delay)
	runner.mu.Lock()
	defer runner.mu.Unlock()
	id := known
	if !listed {
		runner.nextID++
		id = 40 + runner.nextID
		runner.spaces[runner.taskName] = id
		runner.created++
	}
	payload := map[string]any{
		"id": id, "taskId": fmt.Sprintf("task-%d", id), "name": runner.taskName, "ownership": "agent",
	}
	raw, _ := json.Marshal(payload)
	return json.Unmarshal(raw, result)
}

func TestEnsureIdentitySpaceCreatesOneSpaceForConcurrentCallers(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("并发建空间")
	if err != nil {
		t.Fatal(err)
	}
	taskName := fmt.Sprintf("alpha-nexus/matrix-%d", identity.ID)
	runner := &racySpaceRunner{delay: 60 * time.Millisecond, taskName: taskName, spaces: map[string]int64{}}
	service := egolite.NewSpaceService(database, runner)

	type outcome struct {
		space *db.EgoSpace
		err   error
	}
	outcomes := make([]outcome, 2)
	var waitGroup sync.WaitGroup
	for index := range outcomes {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			space, ensureErr := service.EnsureIdentitySpace(context.Background(), identity.ID)
			outcomes[index] = outcome{space: space, err: ensureErr}
		}(index)
	}
	waitGroup.Wait()

	for index, got := range outcomes {
		if got.err != nil || got.space == nil {
			t.Fatalf("caller %d failed: space=%+v err=%v, want one shared Space", index, got.space, got.err)
		}
	}
	if outcomes[0].space.SpaceID != outcomes[1].space.SpaceID {
		t.Fatalf("callers bound different Spaces: %d != %d", outcomes[0].space.SpaceID, outcomes[1].space.SpaceID)
	}
	if runner.created != 1 {
		t.Fatalf("runtime Spaces created=%d, want exactly one (an extra one is orphaned)", runner.created)
	}
	stored, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SpaceID != outcomes[0].space.SpaceID {
		t.Fatalf("persisted Space=%d, want the shared %d", stored.SpaceID, outcomes[0].space.SpaceID)
	}
}

// ResumeAfterConfirmation persists an agent claim before the submit run starts.
// When that run dies the row used to keep reporting a live agent Space, so the
// claim is handed back with the failure recorded instead.
func TestSubmitAfterResumeRestoresSpaceStateWhenTheRunDies(t *testing.T) {
	database, identity, page, _ := automatedPublishContractFixture(t, "csdn", "article")
	page.TagsJSON = `["AlphaNexus"]`
	taskName := fmt.Sprintf("alpha-nexus/matrix-%d", identity.ID)
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 6, "task-6", taskName); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpdateEgoSpaceStateForIdentity(identity.ID, "agentDelegatedToUser", "manual_takeover", ""); err != nil {
		t.Fatal(err)
	}
	runner := &programFailingRunner{
		fakeProgramRunner: &fakeProgramRunner{claimResult: map[string]any{
			"id": 6, "taskId": "task-6", "name": taskName, "ownership": "agent", "done": true,
		}},
		failOn: "const submitResults = []",
		err:    errors.New("runtime killed"),
	}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.SubmitAcceptancePublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
	if batch != nil || err == nil || err.Error() != "runtime killed" {
		t.Fatalf("batch=%+v err=%v, want the original submit failure", batch, err)
	}
	if !strings.Contains(strings.Join(runner.calls, "\n"), "claimTaskSpace(") {
		t.Fatal("the resumed claim was never exercised, so the failure window was not entered")
	}
	stored, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Ownership != "agentDelegatedToUser" || stored.State != "manual_takeover" ||
		stored.ErrorCode != "EGO_PUBLISH_SPACE_RELEASE_FAILED" {
		t.Fatalf("space=%+v, want the resumed claim handed back with the failure recorded", stored)
	}
}
