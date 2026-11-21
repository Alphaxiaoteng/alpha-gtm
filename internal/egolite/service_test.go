package egolite_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/egopublish"
	"alpha-nexus-egolite/internal/platforms"
)

func assertJavaScriptSyntax(t *testing.T, program string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for generated EgoLite program syntax checks")
	}
	command := exec.Command(node, "--input-type=module", "--check")
	command.Stdin = strings.NewReader(program)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated EgoLite program is invalid JavaScript: %v\n%s", err, output)
	}
}

func assertLoginProgramHandsOffAfterInjectedFailure(t *testing.T, program string, spaceID int64) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for generated EgoLite failure-path checks")
	}
	bootstrap := `const events = []
globalThis.listTaskSpaces = async () => [{ id: ` + jsonNumber(spaceID) + `, ownership: "user" }]
globalThis.takeOverTaskSpace = async () => { events.push("takeover"); return { done: true } }
globalThis.useOrCreateTaskSpace = async () => { events.push("claim"); return { done: true } }
globalThis.listTabs = async () => { throw new Error("injected_after_claim") }
globalThis.handOffTaskSpace = async () => { events.push("handoff"); return { done: true } }
globalThis.cliLog = () => {}
try {
` + program + `
} catch {}
process.stdout.write(JSON.stringify(events))`
	output, err := exec.Command(node, "--input-type=module", "--eval", bootstrap).CombinedOutput()
	if err != nil {
		t.Fatalf("generated EgoLite failure-path program failed: %v\n%s", err, output)
	}
	var events []string
	if err := json.Unmarshal(output, &events); err != nil {
		t.Fatalf("decode failure-path events: %v output=%q", err, output)
	}
	if strings.Join(events, ",") != "takeover,handoff" {
		t.Fatalf("claimed login Space was not returned after failure: events=%v", events)
	}
}

func observedAvatarBase64(t *testing.T) string {
	t.Helper()
	avatar := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			avatar.SetNRGBA(x, y, color.NRGBA{R: uint8(30 + x*4), G: uint8(60 + y*3), B: 180, A: 255})
		}
	}
	var payload bytes.Buffer
	if err := png.Encode(&payload, avatar); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(payload.Bytes())
}

type fakeProgramRunner struct {
	db                     *db.DB
	calls                  []string
	verifyProfiles         []map[string]any
	verifyControlAction    string
	verifyControlAttempted *bool
	verifyControlDone      *bool
	sessionCheck           map[string]any
	loginQR                map[string]any
	spaceProbe             map[string]any
	managerErr             error
	publishPrepare         map[string]any
	publishPrepareErr      error
	publishCompletion      map[string]any
	publishSubmit          map[string]any
	publishInspect         map[string]any
	publishDelete          map[string]any
	claimResult            map[string]any
}

type ownershipBoundaryProgramRunner struct {
	db        *db.DB
	spaceID   int64
	ownership string
	calls     []string
	events    [][]string
}

func boolPointer(value bool) *bool { return &value }

func (runner *ownershipBoundaryProgramRunner) Run(ctx context.Context, program string, result any) error {
	runner.calls = append(runner.calls, program)
	if handled, err := bindingProbeAnswer(runner.db, program, result); handled {
		return err
	}
	if strings.Contains(program, "const profileResult = await listProfiles()") {
		payload := map[string]any{
			"id":        runner.spaceID,
			"taskId":    "task-ownership-boundary",
			"name":      "alpha-nexus/ownership-boundary",
			"ownership": runner.ownership,
		}
		raw, _ := json.Marshal(payload)
		return json.Unmarshal(raw, result)
	}
	if !strings.Contains(program, "const spaces = await listTaskSpaces()") {
		raw, _ := json.Marshal(map[string]any{"available": true, "length": 42})
		return json.Unmarshal(raw, result)
	}

	events, payload, err := runner.execute(ctx, program)
	runner.events = append(runner.events, events)
	if err != nil {
		return err
	}
	if payload == nil {
		return fmt.Errorf("ownership boundary program emitted no result: %v", events)
	}
	return json.Unmarshal(payload, result)
}

func (runner *ownershipBoundaryProgramRunner) execute(ctx context.Context, program string) ([]string, []byte, error) {
	ownershipJSON, _ := json.Marshal(runner.ownership)
	bootstrap := `const events = []
globalThis.listTaskSpaces = async () => [{ id: ` + jsonNumber(runner.spaceID) + `, ownership: ` + string(ownershipJSON) + ` }]
globalThis.useOrCreateTaskSpace = async () => { events.push("useOrCreateTaskSpace"); return { id: ` + jsonNumber(runner.spaceID) + `, taskId: "task-ownership-boundary" } }
globalThis.takeOverTaskSpace = async () => { events.push("takeOverTaskSpace"); return { done: true } }
globalThis.snapshotText = async () => { events.push("snapshotText"); return { observed: true } }
globalThis.listTabs = async () => []
globalThis.currentTab = async () => ({ targetId: "tab-1" })
globalThis.openOrReuseTab = async () => { events.push("openOrReuseTab"); return {} }
globalThis.closeTab = async () => { events.push("closeTab") }
globalThis.switchTab = async () => { events.push("switchTab") }
globalThis.js = async () => ({ location: "", inspectionStatus: "observed", matchingTitle: false, publishedState: false, observedStatus: "", postURL: "", externalPostID: "" })
globalThis.cdp = async () => ({ data: "" })
globalThis.handOffTaskSpace = async () => { events.push("handOffTaskSpace"); return { done: true } }
globalThis.setTimeout = (resolve) => resolve()
globalThis.cliLog = (line) => events.push("result:" + String(line))
try {
` + program + `
} catch (error) {
  events.push(String(error?.message ?? error))
}
process.stdout.write(JSON.stringify(events))`
	output, err := exec.CommandContext(ctx, "node", "--input-type=module", "--eval", bootstrap).CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("ownership boundary program failed: %v\n%s", err, output)
	}
	var events []string
	if err := json.Unmarshal(output, &events); err != nil {
		return nil, nil, fmt.Errorf("decode ownership boundary events: %v output=%q", err, output)
	}
	var payload []byte
	for _, event := range events {
		if strings.HasPrefix(event, "result:ALPHA_EGO_RESULT:") {
			payload = []byte(strings.TrimPrefix(event, "result:ALPHA_EGO_RESULT:"))
		}
		if event == "EGO_SPACE_USER_CONTROLLED" {
			return events, nil, errors.New(event)
		}
	}
	return events, payload, nil
}

func (runner *fakeProgramRunner) BindProgramRunnerDB(database *db.DB) {
	if runner.db == nil {
		runner.db = database
	}
}

func (runner *fakeProgramRunner) Run(_ context.Context, program string, result any) error {
	runner.calls = append(runner.calls, program)
	// A binding pre-flight asks the runtime which Spaces it knows; answer with
	// the binding row the test wrote so "live" means live, not an empty list.
	if strings.Contains(program, `cliLog(JSON.stringify({ spaces }))`) && !strings.Contains(program, "const boundSpace =") && !strings.Contains(program, "const currentSpace =") && !strings.Contains(program, "const managerSpace =") && !strings.Contains(program, "const existingSpace =") {
		var listing []map[string]any
		if runner.db != nil {
			for identityID := int64(1); identityID <= 8; identityID++ {
				if space, err := runner.db.GetEgoSpaceByIdentity(identityID); err == nil && space != nil {
					listing = append(listing, map[string]any{"id": space.SpaceID, "ownership": space.Ownership})
				}
			}
		}
		raw, _ := json.Marshal(map[string]any{"spaces": listing})
		return json.Unmarshal(raw, result)
	}
	if strings.Contains(program, "const managerSpace =") && runner.managerErr != nil {
		return runner.managerErr
	}
	if strings.Contains(program, "const preparationResults = []") && runner.publishPrepareErr != nil {
		return runner.publishPrepareErr
	}
	payload := any(map[string]any{
		"id":        6,
		"taskId":    "task-6",
		"name":      "identity-1/account-1/xhs",
		"ownership": "agent",
		"done":      true,
	})
	if strings.Contains(program, "handOffTaskSpace") {
		payload = map[string]any{"done": true, "ownership": "agentDelegatedToUser"}
	}
	if strings.Contains(program, "takeOverTaskSpace") {
		payload = map[string]any{"done": true, "ownership": "agent"}
	}
	if strings.Contains(program, "claimTaskSpace") {
		payload = map[string]any{"id": 6, "taskId": "task-6", "name": "identity-1/account-1/xhs", "done": true, "ownership": "agent"}
		if runner.claimResult != nil {
			payload = runner.claimResult
		}
	}
	if strings.Contains(program, "snapshotText") {
		payload = map[string]any{"available": true, "length": 42}
	}
	if strings.Contains(program, "const profiles = []") {
		controlAction := "complete"
		for _, profile := range runner.verifyProfiles {
			authState, _ := profile["authState"].(string)
			if authState == db.AccountAuthLoggedOut || authState == "manual_takeover" {
				controlAction = "handoff"
				break
			}
		}
		if runner.verifyControlAction != "" {
			controlAction = runner.verifyControlAction
		}
		controlAttempted := true
		if runner.verifyControlAttempted != nil {
			controlAttempted = *runner.verifyControlAttempted
		}
		controlDone := true
		if runner.verifyControlDone != nil {
			controlDone = *runner.verifyControlDone
		}
		payload = map[string]any{
			"profiles": runner.verifyProfiles, "controlAction": controlAction,
			"controlAttempted": controlAttempted, "controlDone": controlDone,
		}
	}
	if strings.Contains(program, "let sessionCheck =") {
		payload = runner.sessionCheck
	}
	if strings.Contains(program, "const loginQR =") {
		payload = runner.loginQR
	}
	if strings.Contains(program, "const boundSpace =") && runner.spaceProbe != nil {
		payload = runner.spaceProbe
	}
	if strings.Contains(program, "const preparationResults = []") && runner.publishPrepare != nil {
		payload = runner.publishPrepare
	}
	if strings.Contains(program, "const completed = await completeTaskSpace(") && runner.publishCompletion != nil {
		payload = runner.publishCompletion
	}
	if strings.Contains(program, "const submitResults = []") && runner.publishSubmit != nil {
		payload = runner.publishSubmit
	}
	if strings.Contains(program, "const managerInspection =") && runner.publishInspect != nil {
		payload = runner.publishInspect
	}
	if strings.Contains(program, "let deletionResult =") && runner.publishDelete != nil {
		payload = runner.publishDelete
	}
	raw, _ := json.Marshal(payload)
	return json.Unmarshal(raw, result)
}

// publishPrepareProgram returns the single atomic runtime program that fills
// one frozen composer and retains its Space before logging the result.
func publishPrepareProgram(t *testing.T, runner *fakeProgramRunner) string {
	t.Helper()
	for index := len(runner.calls) - 1; index >= 0; index-- {
		if strings.Contains(runner.calls[index], "const preparationResults = []") {
			return runner.calls[index]
		}
	}
	t.Fatalf("publish prepare runtime program was not called: calls=%d", len(runner.calls))
	return ""
}

// publishInspectionProgram returns the read-only creator-manager program. As
// with prepare and submit, inspection now completes its operation separately
// while keeping the bound Space available for the next publish operation.
func publishInspectionProgram(t *testing.T, runner *fakeProgramRunner) string {
	t.Helper()
	for index := len(runner.calls) - 1; index >= 0; index-- {
		if strings.Contains(runner.calls[index], "const managerInspection =") {
			return runner.calls[index]
		}
	}
	t.Fatalf("publish inspection runtime program was not called: calls=%d", len(runner.calls))
	return ""
}

func TestPublishPrepareStopsForUserControlledSpaceWithoutTakingItOver(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("用户接管发布浏览器")
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
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 14, "task-14", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{
		spaceProbe: map[string]any{
			"id": 14, "taskId": "task-14", "name": "alpha-nexus/matrix-1", "ownership": "user",
		},
		publishPrepareErr: errors.New("EGO_SPACE_USER_CONTROLLED"),
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen-image.png")
	mediaBytes := []byte("frozen-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	batch, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(
		context.Background(), identity.ID, []egolite.PublishPage{{
			PlatformCode: "douyin", ContentType: "graph_text", Title: "冻结标题", Body: "冻结正文",
			Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "image"}},
		}},
	)
	if err != nil || batch == nil || batch.State != "failed" {
		t.Fatalf("batch=%+v err=%v, want failed safety stop", batch, err)
	}
	if len(batch.PreparedPlatformCodes) != 0 || batch.PlatformErrors["douyin"] != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("batch=%+v, want unprepared user-control target", batch)
	}
	program := publishPrepareProgram(t, runner)
	if !strings.Contains(program, `if (existingSpace.ownership === "user")`) ||
		!strings.Contains(program, `throw new Error("EGO_SPACE_USER_CONTROLLED")`) {
		t.Fatalf("publish prepare lost its fail-closed user-controlled guard: %s", program)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.Ownership != "user" || space.State != "ready" || space.ErrorCode != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("space=%+v, want persisted user-controlled safety stop without publish handoff", space)
	}
}

func TestPublishPrepareCompletionFailureReturnsStableReleaseError(t *testing.T) {
	database, identity, page, _ := automatedPublishContractFixture(t, "csdn", "article")
	runner := &fakeProgramRunner{
		publishPrepare: map[string]any{
			"done": true, "results": []map[string]any{{"platformCode": "csdn", "status": "awaiting_review"}},
		},
		publishCompletion: map[string]any{"done": false},
	}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
	if batch != nil || err == nil || err.Error() != "EGO_PUBLISH_SPACE_RELEASE_FAILED" {
		t.Fatalf("batch=%+v err=%v, want stable release failure", batch, err)
	}
	prepareCalls := 0
	completionCalls := 0
	var prepareProgram string
	for _, program := range runner.calls {
		if strings.Contains(program, "const preparationResults = []") {
			prepareCalls++
			prepareProgram = program
		}
		if strings.Contains(program, "const completed = await completeTaskSpace(") {
			completionCalls++
		}
	}
	if prepareCalls != 1 || completionCalls != 1 {
		t.Fatalf("prepare runtime calls=%d completion calls=%d, want one of each", prepareCalls, completionCalls)
	}
	if strings.Contains(prepareProgram, "submit approved publish") || strings.Contains(prepareProgram, "await click(selector") {
		t.Fatalf("failed prepare crossed final-click boundary: %s", prepareProgram)
	}
	var jobs, records int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 || records != 0 {
		t.Fatalf("completion failure persisted publish state: jobs=%d records=%d", jobs, records)
	}
}

func TestPublishPrepareRequiresSuccessfulDedicatedCompletion(t *testing.T) {
	database, identity, page, _ := automatedPublishContractFixture(t, "csdn", "article")
	runner := &fakeProgramRunner{
		publishPrepare: map[string]any{
			"done": true, "results": []map[string]any{{"platformCode": "csdn", "status": "awaiting_review"}},
		},
		publishCompletion: map[string]any{"done": false},
	}
	batch, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
	if batch != nil || err == nil || err.Error() != "EGO_PUBLISH_SPACE_RELEASE_FAILED" {
		t.Fatalf("batch=%+v err=%v, want missing completion markers to fail closed", batch, err)
	}
}

func TestPublishPrepareTreatsUserOwnedCompletionAsRuntimeHandoff(t *testing.T) {
	database, identity, page, _ := automatedPublishContractFixture(t, "csdn", "article")
	runner := &fakeProgramRunner{
		publishPrepare: map[string]any{
			"done": true, "results": []map[string]any{{"platformCode": "csdn", "status": "awaiting_review"}},
		},
		publishCompletion: map[string]any{"done": false, "skipped": "user-owned"},
	}
	batch, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
	if err != nil || batch == nil || batch.State != "awaiting_review" || len(batch.PreparedPlatformCodes) != 1 {
		t.Fatalf("batch=%+v err=%v, want prepared runtime handoff", batch, err)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.Ownership != "agentDelegatedToUser" || space.State != "ready" || space.ErrorCode != "" {
		t.Fatalf("space=%+v, want safe runtime handoff without manual publish state", space)
	}
}

func TestPublishPrepareRejectsMultiTargetBatchBeforeRuntime(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("partial publish batch")
	if err != nil {
		t.Fatal(err)
	}
	for _, platformCode := range []string{"douyin", "csdn"} {
		account, createErr := database.CreateAccountForIdentity(identity.ID, platformCode, platformCode, "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		if loginErr := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); loginErr != nil {
			t.Fatal(loginErr)
		}
	}
	runner := &fakeProgramRunner{}
	batch, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{
		{PlatformCode: "douyin", ContentType: "graph_text", Title: "抖音标题", Body: "抖音正文"},
		{PlatformCode: "csdn", ContentType: "article", Title: "CSDN 标题", Body: "CSDN 正文", TagsJSON: `["AlphaNexus"]`},
	})
	if err == nil || err.Error() != "EGO_PUBLISH_BATCH_INVALID" || batch != nil {
		t.Fatalf("batch=%+v err=%v, want one-target fail-closed validation", batch, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("multi-target publish batch must not touch EgoLite: calls=%d", len(runner.calls))
	}
}

func TestCSDNPublishManagerInspectionRequiresExactRowEvidenceAndPrivateScreenshot(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("CSDN 核验浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "csdn", "CSDN", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 64, "alpha-nexus/matrix-csdn", "浏览器 CSDN"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishInspect: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"location":         "https://mp.csdn.net/mp_blog/manage/article",
		"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
		"observedStatus": "已发布", "postURL": "https://blog.csdn.net/example/article/details/123",
		"externalPostID": "123", "screenshotBase64": observedAvatarBase64(t),
	}}

	inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(context.Background(), account.ID, "精确文章标题")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.InspectionStatus != "inspected" || !inspection.MatchingTitle || !inspection.PublishedState || inspection.ScreenshotPath == "" {
		t.Fatalf("inspection=%+v", inspection)
	}
	if info, statErr := os.Stat(inspection.ScreenshotPath); statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private screenshot info=%+v err=%v", info, statErr)
	}
	wantDir := filepath.Join(database.DataDir, "screenshots", "publish-inspections") + string(os.PathSeparator)
	if !strings.HasPrefix(inspection.ScreenshotPath, wantDir) {
		t.Fatalf("screenshot escaped private evidence directory: %q", inspection.ScreenshotPath)
	}
	program := publishInspectionProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://mp.csdn.net/mp_blog/manage/article", "精确文章标题", "const managerInspection =", "Page.captureScreenshot",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("inspection program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "handOffTaskSpace") {
		t.Fatalf("inspection must not hand off the publish Space: %s", program)
	}
	completion := runner.calls[len(runner.calls)-1]
	if !strings.Contains(completion, "completeTaskSpace") || !strings.Contains(completion, "{ keep: true }") {
		t.Fatalf("inspection must retain the completed agent Space: %s", completion)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil || space.Ownership != "agent" || space.State != "ready" {
		t.Fatalf("space=%+v err=%v, want agent/ready after inspection", space, err)
	}
}

func TestPublishManagerInspectionCompletesInInspectionRuntimeAfterTabRestore(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("发布核验原子运行时")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "csdn", "CSDN", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 64, "alpha-nexus/matrix-csdn", "浏览器 CSDN"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishInspect: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"location":         "https://mp.csdn.net/mp_blog/manage/article",
		"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
		"observedStatus": "已发布", "postURL": "https://blog.csdn.net/example/article/details/123",
		"externalPostID": "123", "screenshotBase64": observedAvatarBase64(t),
	}}

	inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(context.Background(), account.ID, "精确文章标题")
	if err != nil || inspection == nil {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("inspection should use one binding call and one inspection runtime, calls=%d", len(runner.calls))
	}
	inspectionProgram := publishInspectionProgram(t, runner)
	if strings.Count(inspectionProgram, "await completeTaskSpace(") != 1 {
		t.Fatalf("inspection runtime should contain one completion call: %s", inspectionProgram)
	}
	if strings.Contains(runner.calls[0], "const managerInspection =") {
		t.Fatalf("binding runtime unexpectedly contained manager inspection: %s", runner.calls[0])
	}
	closeAt := strings.Index(inspectionProgram, "await closeTab(managerID)")
	switchAt := strings.Index(inspectionProgram, "await switchTab(priorID)")
	completeAt := strings.Index(inspectionProgram, "await completeTaskSpace(")
	logAt := strings.Index(inspectionProgram, "cliLog(")
	if closeAt < 0 || switchAt < 0 || completeAt <= switchAt || logAt <= completeAt {
		t.Fatalf("inspection cleanup/completion/log order invalid: close=%d switch=%d complete=%d log=%d", closeAt, switchAt, completeAt, logAt)
	}
}

func TestPublishManagerInspectionCompletionThrowIsCaughtWithoutRawError(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for generated inspection completion checks")
	}
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("发布核验释放异常")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "csdn", "CSDN", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 64, "alpha-nexus/matrix-csdn", "浏览器 CSDN"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishInspect: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"location":         "https://mp.csdn.net/mp_blog/manage/article",
		"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
		"observedStatus": "已发布", "postURL": "https://blog.csdn.net/example/article/details/123",
		"externalPostID": "123", "screenshotBase64": observedAvatarBase64(t),
	}}
	if _, err := egolite.NewSpaceService(database, runner).InspectPublishManager(context.Background(), account.ID, "精确文章标题"); err != nil {
		t.Fatal(err)
	}
	program := publishInspectionProgram(t, runner)
	const secret = "secret completion failure"
	prelude := `
const events = []
let currentTabCalls = 0
const listTaskSpaces = async () => [{ id: 6, ownership: "agent" }]
const useOrCreateTaskSpace = async () => { events.push("use"); return { done: true } }
const listTabs = async () => [{ id: "prior", url: "https://mp.csdn.net/mp_blog/manage/article" }]
const currentTab = async () => ({ targetId: currentTabCalls++ === 0 ? "prior" : "manager" })
const openOrReuseTab = async () => { events.push("open"); return {} }
const closeTab = async () => { events.push("close") }
const switchTab = async () => { events.push("switch") }
const js = async () => ({ location: "https://mp.csdn.net/mp_blog/manage/article", inspectionStatus: "inspected", matchingTitle: true, publishedState: true, scheduledState: false, observedStatus: "已发布", postURL: "https://blog.csdn.net/example/article/details/123", externalPostID: "123" })
const cdp = async () => ({ data: "" })
const completeTaskSpace = async () => { events.push("complete"); throw new Error("` + secret + `") }
const cliLog = (value) => events.push("log:" + String(value))
globalThis.setTimeout = (callback) => callback()
` + program + `
process.stdout.write(JSON.stringify(events))
`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(prelude)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated inspection completion program failed: %v\n%s", err, output)
	}
	if strings.Contains(string(output), secret) {
		t.Fatalf("raw completion error leaked: %s", output)
	}
	var events []string
	if err := json.Unmarshal(output, &events); err != nil {
		t.Fatalf("decode inspection completion events: %v\n%s", err, output)
	}
	if len(events) != 6 || strings.Join(events[:5], ",") != "use,open,close,switch,complete" || !strings.HasPrefix(events[5], "log:ALPHA_EGO_RESULT:") {
		t.Fatalf("inspection completion events=%v, want cleanup then one swallowed completion before log", events)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(events[5], "log:ALPHA_EGO_RESULT:")), &payload); err != nil {
		t.Fatalf("decode inspection completion payload: %v", err)
	}
	if payload["done"] != true || payload["completionAttempted"] != true || payload["completionDone"] != false {
		t.Fatalf("inspection completion payload=%v, want done=true attempted=true done=false", payload)
	}
}

func TestPublishManagerInspectionCompletionFailureDoesNotPersistEvidence(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload map[string]any
	}{
		{
			name: "false",
			payload: map[string]any{
				"done": true, "completionAttempted": true, "completionDone": false,
				"location":         "https://mp.csdn.net/mp_blog/manage/article",
				"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
				"observedStatus": "已发布", "postURL": "https://blog.csdn.net/example/article/details/123",
				"externalPostID": "123", "screenshotBase64": "not-persisted",
			},
		},
		{
			name: "missing markers",
			payload: map[string]any{
				"done":             true,
				"location":         "https://mp.csdn.net/mp_blog/manage/article",
				"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
				"observedStatus": "已发布", "postURL": "https://blog.csdn.net/example/article/details/123",
				"externalPostID": "123", "screenshotBase64": "not-persisted",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity("发布核验释放失败 " + test.name)
			if err != nil {
				t.Fatal(err)
			}
			account, err := database.CreateAccountForIdentity(identity.ID, "csdn", "CSDN", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
				t.Fatal(err)
			}
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 64, "alpha-nexus/matrix-csdn", "浏览器 CSDN"); err != nil {
				t.Fatal(err)
			}
			runner := &fakeProgramRunner{publishInspect: test.payload}
			inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(context.Background(), account.ID, "精确文章标题")
			if inspection != nil || err == nil || err.Error() != "EGO_PUBLISH_SPACE_RELEASE_FAILED" {
				t.Fatalf("inspection=%+v err=%v, want stable release failure", inspection, err)
			}
			space, err := database.GetEgoSpaceByIdentity(identity.ID)
			if err != nil {
				t.Fatal(err)
			}
			if space.Ownership != "agentDelegatedToUser" || space.State != "verification_failed" || space.ErrorCode != "EGO_PUBLISH_SPACE_RELEASE_FAILED" {
				t.Fatalf("space=%+v, want delegated verification failure", space)
			}
			screenshotDir := filepath.Join(database.DataDir, "screenshots", "publish-inspections")
			if entries, readErr := os.ReadDir(screenshotDir); readErr == nil && len(entries) != 0 {
				t.Fatalf("completion failure persisted screenshot evidence: entries=%v", entries)
			}
		})
	}
}

func TestWechatMPPublishManagerInspectionUsesReviewedManagerDOMContractButRemainsUnverified(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("公众号核验浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "wechat_mp", "微信公众号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 67, "alpha-nexus/matrix-wechat-mp", "公众号核验"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishInspect: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"location":         "https://mp.weixin.qq.com/cgi-bin/appmsg",
		"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
		"observedStatus": "已发表", "postURL": "https://mp.weixin.qq.com/s/alpha-article",
		"externalPostID": "alpha-article", "screenshotBase64": observedAvatarBase64(t),
	}}

	inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(
		context.Background(), account.ID, "精确公众号标题",
	)
	if err != nil || inspection == nil {
		t.Fatalf("inspection=%+v err=%v, want read-only manager inspection", inspection, err)
	}
	if !inspection.MatchingTitle || !inspection.PublishedState || inspection.ObservedStatus != "已发表" ||
		inspection.PostURL != "https://mp.weixin.qq.com/s/alpha-article" || inspection.ExternalPostID != "alpha-article" || inspection.ScreenshotPath == "" {
		t.Fatalf("inspection=%+v", inspection)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("manager inspection should use binding plus one atomic read-only runtime, calls=%d", len(runner.calls))
	}
	program := publishInspectionProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://mp.weixin.qq.com/cgi-bin/appmsg", "action=list_card", "精确公众号标题",
		"weui-desktop-masonry-list", "weui-desktop-publish__cover__title",
		"已发表", "定时发表", "matchingEffectiveScheduledAt", "completeTaskSpace", "Page.captureScreenshot",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("WeChat Official Account inspection program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "handOffTaskSpace") || strings.Contains(program, "takeOverTaskSpace") {
		t.Fatalf("manager inspection must retain the agent Space without handoff/takeover: %s", program)
	}
	if egopublish.SubmitVerifiedFor("wechat_mp", "article") || egopublish.VerifyVerifiedFor("wechat_mp", "article") {
		t.Fatal("WeChat Official Account live submit and verify must remain fail-closed pending real account evidence")
	}
}

func TestDouyinScheduledManagerInspectionRequiresExactFrozenLocalMinute(t *testing.T) {
	tests := []struct {
		name                 string
		effectiveScheduledAt string
		wantError            bool
	}{
		{name: "exact local minute", effectiveScheduledAt: "2030-08-15T11:48:00Z"},
		{name: "wrong local minute", effectiveScheduledAt: "2030-08-15T11:49:00Z", wantError: true},
		{name: "missing effective time", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity("抖音定时发布核验")
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
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 92, "alpha-nexus/douyin-scheduled", "定时发布核验"); err != nil {
				t.Fatal(err)
			}
			runner := &fakeProgramRunner{publishInspect: map[string]any{
				"done": true, "completionAttempted": true, "completionDone": true,
				"location":         "https://creator.douyin.com/creator-micro/content/manage",
				"inspectionStatus": "inspected", "matchingTitle": true,
				"publishedState": false, "scheduledState": true, "observedStatus": "定时发布",
				"effectiveScheduledAt": test.effectiveScheduledAt,
				"screenshotBase64":     observedAvatarBase64(t),
			}}
			publishAt := "2030-08-15T11:48:00Z"
			inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(
				context.Background(), account.ID, "精确冻结标题", publishAt,
			)
			if test.wantError {
				if inspection != nil || err == nil || err.Error() != egopublish.ScheduleReadbackMismatch {
					t.Fatalf("inspection=%+v err=%v, want fail-closed schedule evidence", inspection, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if inspection == nil || !inspection.MatchingTitle || !inspection.ScheduledState || inspection.PublishedState ||
				inspection.EffectiveScheduledAt != test.effectiveScheduledAt || inspection.ScreenshotPath == "" {
				t.Fatalf("inspection=%+v", inspection)
			}
			program := publishInspectionProgram(t, runner)
			assertJavaScriptSyntax(t, program)
			for _, required := range []string{
				publishAt, "2030-08-15 19:48", "matchingEffectiveScheduledAt", "scheduledState", "effectiveScheduledAt",
			} {
				if !strings.Contains(program, required) {
					t.Fatalf("scheduled manager program missing %q: %s", required, program)
				}
			}
			if strings.Contains(program, "定时发表") || strings.Contains(program, "定时发布中") == false {
				t.Fatalf("scheduled manager status contract is not restricted to accepted labels: %s", program)
			}
		})
	}
}

func TestYouTubeScheduledManagerInspectionRequiresExactFrozenLocalMinute(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("YouTube 定时发布核验")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "youtube", "YouTube", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 97, "alpha-nexus/youtube-scheduled", "YouTube 定时发布核验"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishInspect: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"location":         "https://studio.youtube.com/",
		"inspectionStatus": "inspected", "matchingTitle": true,
		"publishedState": false, "scheduledState": true, "observedStatus": "Scheduled",
		"effectiveScheduledAt": "2030-08-15T11:48:00Z",
		"screenshotBase64":     observedAvatarBase64(t),
	}}
	publishAt := "2030-08-15T11:48:00Z"

	inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(
		context.Background(), account.ID, "精确 YouTube 标题", publishAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	if inspection == nil || !inspection.MatchingTitle || !inspection.ScheduledState || inspection.PublishedState ||
		inspection.EffectiveScheduledAt != "2030-08-15T11:48:00Z" || inspection.ScreenshotPath == "" {
		t.Fatalf("inspection=%+v", inspection)
	}
	program := publishInspectionProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		publishAt, "2030-08-15 19:48", "youtubeScheduleMinute", "scheduledState", "effectiveScheduledAt",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("YouTube scheduled manager program missing %q: %s", required, program)
		}
	}
}

func TestTikTokScheduledManagerInspectionRequiresRoundedFiveMinuteEvidence(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("TikTok 定时发布核验")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "tiktok", "TikTok", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 99, "alpha-nexus/tiktok-scheduled", "TikTok 定时发布核验"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishInspect: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"location":         "https://www.tiktok.com/tiktokstudio/content",
		"inspectionStatus": "inspected", "matchingTitle": true,
		"publishedState": false, "scheduledState": true, "observedStatus": "Scheduled",
		"effectiveScheduledAt": "2030-08-15T11:50:00Z",
		"screenshotBase64":     observedAvatarBase64(t),
	}}
	publishAt := "2030-08-15T11:48:00Z"

	inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(
		context.Background(), account.ID, "精确 TikTok 定时文案", publishAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	if inspection == nil || !inspection.MatchingTitle || !inspection.ScheduledState || inspection.PublishedState ||
		inspection.EffectiveScheduledAt != "2030-08-15T11:50:00Z" || inspection.ScreenshotPath == "" {
		t.Fatalf("inspection=%+v", inspection)
	}
	program := publishInspectionProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		publishAt, "2030-08-15 19:50", "tiktokScheduleMinute", "scheduledState", "effectiveScheduledAt",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("TikTok scheduled manager program missing %q: %s", required, program)
		}
	}
}

func TestKuaishouScheduledManagerInspectionRequiresExactFrozenLocalMinute(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("快手定时发布核验")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "kuaishou", "快手", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 101, "alpha-nexus/kuaishou-scheduled", "快手定时发布核验"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishInspect: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"location":         "https://cp.kuaishou.com/article/manage/video",
		"inspectionStatus": "inspected", "matchingTitle": true,
		"publishedState": false, "scheduledState": true, "observedStatus": "定时发布",
		"effectiveScheduledAt": "2030-08-15T11:48:00Z",
		"screenshotBase64":     observedAvatarBase64(t),
	}}
	publishAt := "2030-08-15T11:48:00Z"
	inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(
		context.Background(), account.ID, "精确快手定时文案", publishAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	if inspection == nil || !inspection.ScheduledState || inspection.PublishedState || inspection.ScreenshotPath == "" {
		t.Fatalf("inspection=%+v", inspection)
	}
	program := publishInspectionProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://cp.kuaishou.com/article/manage/video", publishAt, "2030-08-15 19:48",
		"kuaishouScheduleMinute", "scheduledState", "effectiveScheduledAt",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Kuaishou scheduled manager program missing %q: %s", required, program)
		}
	}
}

func TestWechatVideoScheduledManagerInspectionRequiresExactFrozenLocalMinute(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("视频号定时发布核验")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "wechat_video", "视频号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 103, "alpha-nexus/wechat-video-scheduled", "视频号定时发布核验"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishInspect: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"location":         "https://channels.weixin.qq.com/platform/post/list",
		"inspectionStatus": "inspected", "matchingTitle": true,
		"publishedState": false, "scheduledState": true, "observedStatus": "定时发表",
		"effectiveScheduledAt": "2030-08-15T11:48:00Z",
		"screenshotBase64":     observedAvatarBase64(t),
	}}
	publishAt := "2030-08-15T11:48:00Z"
	inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(
		context.Background(), account.ID, "精确视频号定时文案", publishAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	if inspection == nil || !inspection.ScheduledState || inspection.PublishedState || inspection.ScreenshotPath == "" {
		t.Fatalf("inspection=%+v", inspection)
	}
	program := publishInspectionProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://channels.weixin.qq.com/platform/post/list", "https://channels.weixin.qq.com/micro/content/post/list",
		publishAt, "2030-08-15 19:48", "wechatVideoScheduleMinute", "定时发表", "scheduledState", "effectiveScheduledAt",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("WeChat Video scheduled manager program missing %q: %s", required, program)
		}
	}
}

func TestDeleteScheduledDouyinContentRequiresExactRecordAndPersistsPrivateEvidence(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("抖音定时内容删除")
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
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 93, "alpha-nexus/douyin-delete", "抖音删除核验"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true,
		"screenshotBase64":    observedAvatarBase64(t),
	}}
	record := db.PublishContentRecord{
		ID: 77, AccountID: account.ID, PlatformCode: "douyin", RemoteState: "delete_pending",
		Title: "精确定时标题", ScheduledAt: "2030-08-15T11:48:00Z",
	}

	deleted, err := egolite.NewSpaceService(database, runner).DeleteScheduledPublishContent(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if deleted == nil || !deleted.Deleted || deleted.ScreenshotPath == "" {
		t.Fatalf("delete result=%+v", deleted)
	}
	if info, statErr := os.Stat(deleted.ScreenshotPath); statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private delete screenshot info=%+v err=%v", info, statErr)
	}
	program := runner.calls[len(runner.calls)-1]
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://creator.douyin.com/creator-micro/content/manage", "精确定时标题",
		"2030-08-15 19:48", "let deletionResult =", "Page.captureScreenshot",
		"handOffTaskSpace(93)", "rowCandidateCount", "confirmButtonCount",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("delete program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "takeOverTaskSpace") || strings.Contains(program, "closeTab") {
		t.Fatalf("delete program must not take over or close tabs: %s", program)
	}
}

func TestDeleteScheduledYouTubeContentRequiresExactRecordAndPersistsPrivateEvidence(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("YouTube 定时内容删除")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "youtube", "YouTube", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 98, "alpha-nexus/youtube-delete", "YouTube 删除核验"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true, "screenshotBase64": observedAvatarBase64(t),
	}}
	record := db.PublishContentRecord{
		ID: 80, AccountID: account.ID, PlatformCode: "youtube", RemoteState: "delete_pending",
		Title: "精确 YouTube 定时标题", ScheduledAt: "2030-08-15T11:48:00Z",
	}

	deleted, err := egolite.NewSpaceService(database, runner).DeleteScheduledPublishContent(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if deleted == nil || !deleted.Deleted || deleted.PlatformCode != "youtube" || deleted.ScreenshotPath == "" {
		t.Fatalf("delete result=%+v", deleted)
	}
	program := runner.calls[len(runner.calls)-1]
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://studio.youtube.com/", "精确 YouTube 定时标题", "2030-08-15 19:48",
		"Delete forever", "永久删除", "Content deleted", "handOffTaskSpace(98)",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("YouTube delete program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "takeOverTaskSpace") || strings.Contains(program, "closeTab") {
		t.Fatalf("YouTube delete program must not take over or close tabs: %s", program)
	}
}

func TestDeleteScheduledFacebookPageContentRequiresExactAssetMarkerAndMinute(t *testing.T) {
	database, identity, meta, page, _ := newMetaPublishFixture(t, "")
	space, err := database.CreateEgoSpaceForIdentity(identity.ID, 105, "alpha-nexus/facebook-delete", "Facebook Page 删除核验")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true, "screenshotBase64": observedAvatarBase64(t),
	}}
	record := db.PublishContentRecord{
		ID: 83, AccountID: meta.ID, PlatformCode: "facebook", RemoteState: "delete_pending",
		Title: "FB-MARKER", ScheduledAt: "2030-08-15T11:48:00Z",
	}

	deleted, err := egolite.NewSpaceService(database, runner).DeleteScheduledPublishContent(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if deleted == nil || !deleted.Deleted || deleted.PlatformCode != "facebook" || deleted.ScreenshotPath == "" {
		t.Fatalf("delete result=%+v", deleted)
	}
	program := runner.calls[len(runner.calls)-1]
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://business.facebook.com/latest/planner/?asset_id=" + page.ExternalID,
		page.ExternalID, page.Name, "FB-MARKER", "2030-08-15 19:48",
		"Delete post", "删除帖子", fmt.Sprintf("handOffTaskSpace(%d)", space.SpaceID),
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Facebook delete program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "takeOverTaskSpace") || strings.Contains(program, "closeTab") {
		t.Fatalf("Facebook delete program must not take over or close tabs: %s", program)
	}
}

func TestDeleteScheduledTikTokContentRequiresExactRecordAndPersistsPrivateEvidence(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("TikTok 定时内容删除")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "tiktok", "TikTok", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 100, "alpha-nexus/tiktok-delete", "TikTok 删除核验"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true, "screenshotBase64": observedAvatarBase64(t),
	}}
	record := db.PublishContentRecord{
		ID: 81, AccountID: account.ID, PlatformCode: "tiktok", RemoteState: "delete_pending",
		Title: "精确 TikTok 定时文案", ScheduledAt: "2030-08-15T11:50:00Z",
	}

	deleted, err := egolite.NewSpaceService(database, runner).DeleteScheduledPublishContent(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if deleted == nil || !deleted.Deleted || deleted.PlatformCode != "tiktok" || deleted.ScreenshotPath == "" {
		t.Fatalf("delete result=%+v", deleted)
	}
	program := runner.calls[len(runner.calls)-1]
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://www.tiktok.com/tiktokstudio/content", "精确 TikTok 定时文案", "2030-08-15 19:50",
		"Delete", "删除", "handOffTaskSpace(100)",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("TikTok delete program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "takeOverTaskSpace") || strings.Contains(program, "closeTab") {
		t.Fatalf("TikTok delete program must not take over or close tabs: %s", program)
	}
}

func TestDeleteScheduledWechatVideoContentRequiresExactRecordAndPersistsPrivateEvidence(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("视频号定时内容删除")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "wechat_video", "视频号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 106, "alpha-nexus/wechat-video-delete", "视频号删除核验"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true, "screenshotBase64": observedAvatarBase64(t),
	}}
	record := db.PublishContentRecord{
		ID: 84, AccountID: account.ID, PlatformCode: "wechat_video", RemoteState: "delete_pending",
		Title: "精确视频号定时文案", ScheduledAt: "2030-08-15T11:52:00Z",
	}

	deleted, err := egolite.NewSpaceService(database, runner).DeleteScheduledPublishContent(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if deleted == nil || !deleted.Deleted || deleted.PlatformCode != "wechat_video" || deleted.ScreenshotPath == "" {
		t.Fatalf("delete result=%+v", deleted)
	}
	program := runner.calls[len(runner.calls)-1]
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://channels.weixin.qq.com/platform/post/list", "精确视频号定时文案", "2030-08-15 19:52",
		"定时发表", "删除视频", "确认删除", "handOffTaskSpace(106)",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("WeChat Video delete program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "takeOverTaskSpace") || strings.Contains(program, "closeTab") {
		t.Fatalf("WeChat Video delete program must not take over or close tabs: %s", program)
	}
}

func TestDeletePublishedKuaishouContentRequiresExactPublicIdentityAndPersistsEvidence(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("快手已发布内容删除")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "kuaishou", "快手", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 102, "alpha-nexus/kuaishou-delete", "快手删除核验"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": false, "matchingScheduledMinute": false,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true, "screenshotBase64": observedAvatarBase64(t),
	}}
	record := db.PublishContentRecord{
		ID: 82, AccountID: account.ID, PlatformCode: "kuaishou", RemoteState: "delete_pending",
		Title: "精确快手文案", PostURL: "https://www.kuaishou.com/short-video/3xqxmjkthzpckus",
		ExternalPostID: "3xqxmjkthzpckus",
	}
	deleted, err := egolite.NewSpaceService(database, runner).DeleteScheduledPublishContent(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if deleted == nil || !deleted.Deleted || deleted.PlatformCode != "kuaishou" || deleted.ScreenshotPath == "" {
		t.Fatalf("delete result=%+v", deleted)
	}
	program := runner.calls[len(runner.calls)-1]
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://cp.kuaishou.com/article/manage/video", "精确快手文案",
		"3xqxmjkthzpckus", "https://www.kuaishou.com/short-video/3xqxmjkthzpckus",
		"确认删除", "handOffTaskSpace(102)",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Kuaishou delete program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "takeOverTaskSpace") || strings.Contains(program, "closeTab") {
		t.Fatalf("Kuaishou delete program must not take over or close tabs: %s", program)
	}
}

func TestDeleteScheduledKuaishouContentRequiresExactRecordAndPersistsPrivateEvidence(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("快手定时内容删除")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "kuaishou", "快手", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 107, "alpha-nexus/kuaishou-scheduled-delete", "快手定时删除核验"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishDelete: map[string]any{
		"done": true, "deleted": true, "matchingTitle": true,
		"matchingScheduledState": true, "matchingScheduledMinute": true,
		"rowCandidateCount": 1, "deleteMenuCount": 1, "confirmButtonCount": 1,
		"finalClickAttempted": true, "screenshotBase64": observedAvatarBase64(t),
	}}
	record := db.PublishContentRecord{
		ID: 85, AccountID: account.ID, PlatformCode: "kuaishou", RemoteState: "delete_pending",
		Title: "精确快手定时文案", ScheduledAt: "2030-08-15T11:54:00Z",
	}

	deleted, err := egolite.NewSpaceService(database, runner).DeleteScheduledPublishContent(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if deleted == nil || !deleted.Deleted || deleted.PlatformCode != "kuaishou" || deleted.ScreenshotPath == "" {
		t.Fatalf("delete result=%+v", deleted)
	}
	program := runner.calls[len(runner.calls)-1]
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://cp.kuaishou.com/article/manage/video", "精确快手定时文案", "2030-08-15 19:54",
		"定时发布", "删除作品", "确认删除", "handOffTaskSpace(107)",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Kuaishou scheduled delete program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "takeOverTaskSpace") || strings.Contains(program, "closeTab") {
		t.Fatalf("Kuaishou scheduled delete program must not take over or close tabs: %s", program)
	}
}

func TestDeleteScheduledDouyinContentFailsClosedBeforeOrAfterFinalClick(t *testing.T) {
	tests := []struct {
		name      string
		platform  string
		state     string
		runtime   map[string]any
		wantCode  string
		wantCalls int
	}{
		{name: "wrong platform", platform: "instagram", state: "delete_pending", wantCode: "EGO_REMOTE_DELETE_UNSUPPORTED"},
		{name: "wrong state", platform: "douyin", state: "published", wantCode: "EGO_REMOTE_DELETE_STATE_INVALID"},
		{name: "ambiguous row", platform: "douyin", state: "delete_pending", runtime: map[string]any{
			"done": true, "deleted": false, "matchingTitle": true, "matchingScheduledState": true,
			"matchingScheduledMinute": true, "rowCandidateCount": 2, "errorCode": "EGO_REMOTE_DELETE_AMBIGUOUS",
		}, wantCode: "EGO_REMOTE_DELETE_AMBIGUOUS", wantCalls: 1},
		{name: "unknown after click", platform: "douyin", state: "delete_pending", runtime: map[string]any{
			"done": true, "deleted": false, "matchingTitle": true, "matchingScheduledState": true,
			"matchingScheduledMinute": true, "rowCandidateCount": 1, "deleteMenuCount": 1,
			"confirmButtonCount": 1, "finalClickAttempted": true, "errorCode": "EGO_REMOTE_DELETE_RESULT_UNKNOWN",
		}, wantCode: "EGO_REMOTE_DELETE_RESULT_UNKNOWN", wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity("删除拒绝")
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
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 94, "alpha-nexus/douyin-delete-reject", "删除拒绝"); err != nil {
				t.Fatal(err)
			}
			runner := &fakeProgramRunner{publishDelete: test.runtime}
			record := db.PublishContentRecord{
				ID: 78, AccountID: account.ID, PlatformCode: test.platform, RemoteState: test.state,
				Title: "精确定时标题", ScheduledAt: "2030-08-15T11:48:00Z",
			}
			deleted, err := egolite.NewSpaceService(database, runner).DeleteScheduledPublishContent(context.Background(), record)
			if deleted != nil || err == nil || err.Error() != test.wantCode {
				t.Fatalf("deleted=%+v err=%v want=%s", deleted, err, test.wantCode)
			}
			if len(runner.calls) != test.wantCalls {
				t.Fatalf("runtime calls=%d want=%d", len(runner.calls), test.wantCalls)
			}
		})
	}
}

func TestDeleteScheduledDouyinContentRejectsUserControlledSpaceWithoutSideEffects(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("用户接管删除")
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
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 95, "task-ownership-boundary", "alpha-nexus/ownership-boundary"); err != nil {
		t.Fatal(err)
	}
	runner := &ownershipBoundaryProgramRunner{spaceID: 95, ownership: "agentDelegatedToUser"}
	record := db.PublishContentRecord{
		ID: 79, AccountID: account.ID, PlatformCode: "douyin", RemoteState: "delete_pending",
		Title: "精确定时标题", ScheduledAt: "2030-08-15T11:48:00Z",
	}

	deleted, err := egolite.NewSpaceService(database, runner).DeleteScheduledPublishContent(context.Background(), record)
	if deleted != nil || err == nil || err.Error() != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("deleted=%+v err=%v", deleted, err)
	}
	if len(runner.events) != 1 || strings.Join(runner.events[0], ",") != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("user-controlled deletion performed a side effect: events=%v", runner.events)
	}
}

func TestPublishManagerInspectionRejectsInconsistentPublishedRuntimeResult(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("CSDN 核验边界")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "csdn", "CSDN", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 65, "alpha-nexus/matrix-csdn-invalid", "浏览器 CSDN"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishInspect: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"location":         "https://mp.csdn.net/mp_blog/manage/article",
		"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
		"observedStatus": "", "screenshotBase64": observedAvatarBase64(t),
	}}

	inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(context.Background(), account.ID, "精确文章标题")
	if err == nil || inspection != nil || err.Error() != "EGO_PUBLISH_INSPECTION_RESULT_INVALID" {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
}

func TestPublishManagerInspectionSupportsTikTokAndYouTubeEvidence(t *testing.T) {
	tests := []struct {
		name       string
		platform   string
		label      string
		managerURL string
		postURL    string
		externalID string
		status     string
	}{
		{
			name:       "tiktok",
			platform:   "tiktok",
			label:      "TikTok",
			managerURL: "https://www.tiktok.com/tiktokstudio/content",
			postURL:    "https://www.tiktok.com/@alpha_gamer/video/1234567890123",
			externalID: "1234567890123",
			status:     "Public",
		},
		{
			name:       "youtube",
			platform:   "youtube",
			label:      "YouTube",
			managerURL: "https://studio.youtube.com/",
			postURL:    "https://www.youtube.com/shorts/xYz_12-AB",
			externalID: "xYz_12-AB",
			status:     "Published",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity(test.label + " 核验浏览器")
			if err != nil {
				t.Fatal(err)
			}
			account, err := database.CreateAccountForIdentity(identity.ID, test.platform, test.label, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
				t.Fatal(err)
			}
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 90, "alpha-nexus/matrix-"+test.name, "发布核验浏览器"); err != nil {
				t.Fatal(err)
			}
			runner := &fakeProgramRunner{publishInspect: map[string]any{
				"done": true, "completionAttempted": true, "completionDone": true,
				"location":         test.managerURL,
				"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
				"observedStatus": test.status, "postURL": test.postURL,
				"externalPostID": test.externalID, "screenshotBase64": observedAvatarBase64(t),
			}}

			inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(
				context.Background(), account.ID, "精确标题",
			)
			if err != nil {
				t.Fatal(err)
			}
			if inspection == nil || !inspection.PublishedState || inspection.PostURL != test.postURL || inspection.ExternalPostID != test.externalID {
				t.Fatalf("inspection=%+v", inspection)
			}
			program := publishInspectionProgram(t, runner)
			assertJavaScriptSyntax(t, program)
			for _, required := range []string{test.managerURL, "精确标题"} {
				if !strings.Contains(program, required) {
					t.Fatalf("%s inspection program missing %q: %s", test.platform, required, program)
				}
			}
			if strings.Contains(program, "takeOverTaskSpace") {
				t.Fatalf("%s inspection attempted to take over the user Space: %s", test.platform, program)
			}
			if test.platform == "tiktok" {
				for _, required := range []string{"tiktokstudio/content", "www.tiktok.com", "/video/"} {
					if !strings.Contains(program, required) {
						t.Fatalf("TikTok inspection program missing %q: %s", required, program)
					}
				}
			} else {
				for _, required := range []string{"Content", "内容", "ytcp-video-row", "video", "shorts/"} {
					if !strings.Contains(program, required) {
						t.Fatalf("YouTube inspection program missing %q: %s", required, program)
					}
				}
			}
		})
	}
}

func TestFacebookPublishManagerTargetRejectsDirectLegacyAccount(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("Facebook 旧账号核验")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "facebook", "旧 Facebook 账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{}
	inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManagerTarget(
		context.Background(), account.ID, "facebook", "FB-MARKER", "2030-08-15T11:48:00Z",
	)
	if inspection != nil || err == nil || err.Error() != "EGO_META_ACCOUNT_UNAVAILABLE" {
		t.Fatalf("inspection=%+v err=%v, want direct Facebook account rejection", inspection, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("direct legacy Facebook account touched EgoLite: calls=%d", len(runner.calls))
	}
}

func TestFacebookPublishManagerTargetUsesExactMetaPageScheduleEvidence(t *testing.T) {
	database, identity, meta, page, _ := newMetaPublishFixture(t, "")
	if meta.PlatformCode != "meta" || !page.Enabled || strings.TrimSpace(page.ExternalID) == "" || strings.TrimSpace(page.Name) == "" {
		t.Fatalf("fixture does not contain a valid Meta Page target: meta=%+v page=%+v", meta, page)
	}
	_, err := database.CreateEgoSpaceForIdentity(identity.ID, 104, "alpha-nexus/facebook-manager", "Facebook Page 后台核验")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishInspect: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"location":         "https://business.facebook.com/latest/planner/",
		"inspectionStatus": "inspected", "matchingTitle": true,
		"publishedState": false, "scheduledState": true,
		"effectiveScheduledAt": "2030-08-15T11:48:00Z", "observedStatus": "Scheduled",
		"screenshotBase64": observedAvatarBase64(t),
	}}
	inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManagerTarget(
		context.Background(), meta.ID, "facebook", "FB-MARKER", "2030-08-15T11:48:00Z",
	)
	if err != nil {
		t.Fatal(err)
	}
	if inspection == nil || !inspection.ScheduledState || inspection.PublishedState || inspection.PlatformCode != "facebook" || inspection.ScreenshotPath == "" {
		t.Fatalf("inspection=%+v", inspection)
	}
	program := publishInspectionProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://business.facebook.com/latest/planner/?asset_id=1241410172381837",
		"Alpha Page", "1241410172381837", "FB-MARKER", "2030-08-15 19:48",
		"Scheduled", "已排期",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Facebook manager program missing %q: %s", required, program)
		}
	}
}

func TestPublishManagerInspectionRejectsMalformedTikTokAndYouTubePositiveResult(t *testing.T) {
	tests := []struct {
		name       string
		platform   string
		label      string
		managerURL string
		postURL    string
		externalID string
	}{
		{
			name:       "tiktok malformed public URL",
			platform:   "tiktok",
			label:      "TikTok",
			managerURL: "https://www.tiktok.com/tiktokstudio/content",
			postURL:    "https://www.tiktok.com/@alpha_gamer/video/not-a-number",
			externalID: "",
		},
		{
			name:       "youtube malformed edit identity",
			platform:   "youtube",
			label:      "YouTube",
			managerURL: "https://studio.youtube.com/",
			postURL:    "https://www.youtube.com/watch?v=not-a-short",
			externalID: "not a valid id",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity(test.label + " 无效核验")
			if err != nil {
				t.Fatal(err)
			}
			account, err := database.CreateAccountForIdentity(identity.ID, test.platform, test.label, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
				t.Fatal(err)
			}
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 91, "alpha-nexus/matrix-invalid-"+test.platform, "发布核验浏览器"); err != nil {
				t.Fatal(err)
			}
			runner := &fakeProgramRunner{publishInspect: map[string]any{
				"done": true, "completionAttempted": true, "completionDone": true,
				"location":         test.managerURL,
				"inspectionStatus": "inspected", "matchingTitle": true, "publishedState": true,
				"observedStatus": "Public", "postURL": test.postURL,
				"externalPostID": test.externalID, "screenshotBase64": observedAvatarBase64(t),
			}}

			inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(
				context.Background(), account.ID, "精确标题",
			)
			if inspection != nil || err == nil || err.Error() != "EGO_PUBLISH_INSPECTION_RESULT_INVALID" {
				t.Fatalf("inspection=%+v err=%v, want malformed positive result rejection", inspection, err)
			}
		})
	}
}

func TestLoginQROpensOnePlatformAndReturnsOnlyAnEphemeralCrop(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("二维码抖音浏览器")
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

	result, err := service.OpenLoginQR(context.Background(), account.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "qr_ready" || result.ImageDataURL != "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB" {
		t.Fatalf("login QR result=%+v", result)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("QR capture must reuse one short-lived runtime call after Space creation, calls=%d", len(runner.calls))
	}
	program := runner.calls[len(runner.calls)-1]
	assertJavaScriptSyntax(t, program)
	if strings.Count(program, "openOrReuseTab(") != 1 || !strings.Contains(program, "Page.captureScreenshot") {
		t.Fatalf("login QR must open one page and crop via CDP: %s", program)
	}
	if !strings.Contains(program, "await gotoAndWait(") {
		t.Fatalf("an explicit QR refresh must reload the platform login page before reading its canvas: %s", program)
	}
	if !strings.Contains(program, "https://creator.douyin.com") {
		t.Fatalf("login QR must open the requested platform creator backend: %s", program)
	}
	if !strings.Contains(program, "takeOverTaskSpace") {
		t.Fatalf("an explicit login click must reclaim a previously handed-off Space: %s", program)
	}
	if !strings.Contains(program, "while (staleTabs.length > 1)") || strings.Contains(program, "await closeTab(anchorId)") {
		t.Fatalf("login QR must preserve one anchor and keep the opened platform active: %s", program)
	}
	if !strings.Contains(program, `scrollIntoView({ block: "center", inline: "center" })`) ||
		!strings.Contains(program, "if (width <= 0 || height <= 0)") {
		t.Fatalf("login QR must scroll off-screen codes into view before capture: %s", program)
	}
	if !strings.Contains(program, "window.__alphaNexusQRRefreshTimer = setInterval") ||
		!strings.Contains(program, "}, 3000)") ||
		!strings.Contains(program, "二维码\\s*(?:已)?\\s*(?:失效|过期)") ||
		!strings.Contains(program, "clickResult.refreshed ? 1200 : 700") {
		t.Fatalf("login QR must refresh expired codes in-page even after user handoff: %s", program)
	}
	if !strings.Contains(program, "const loginConfirmed = observed?.dashboard && (loginRule?.host || !observed?.loginForm)") ||
		!strings.Contains(program, "if (!directQR && !capturedQR && !loginConfirmed)") {
		t.Fatalf("login polling must report a confirmed dashboard before the slow cross-frame QR scan: %s", program)
	}
	for _, required := range []string{`"host":"creator.douyin.com"`, `"pathPrefixes":["/creator-micro/"]`, `"requiredText":["作品发布","内容管理","数据中心"]`} {
		if !strings.Contains(program, required) {
			t.Fatalf("login polling must include reviewed Douyin dashboard evidence %q: %s", required, program)
		}
	}
	if !strings.Contains(program, "while (staleTabs.length > 1)") || !strings.Contains(program, "closeTab") {
		t.Fatalf("login QR must cap the Space at three tabs: %s", program)
	}
	for _, forbidden := range []string{"document.cookie", "localStorage", "sessionStorage"} {
		if strings.Contains(program, forbidden) {
			t.Fatalf("login QR program contains forbidden %q: %s", forbidden, program)
		}
	}
	if !strings.Contains(program, "await handOffTaskSpace") {
		t.Fatal("QR capture must return control immediately")
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.Ownership != "agentDelegatedToUser" || space.State != "manual_takeover" || space.ErrorCode != "" {
		t.Fatalf("space was not returned after QR capture: %+v", space)
	}
}

func TestLoginQRMapsCrossFrameJPEGDataImage(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("CSDN浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "csdn", "CSDN", "")
	if err != nil {
		t.Fatal(err)
	}
	jpeg := base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 0xe0, 'J', 'F', 'I', 'F'})
	runner := &fakeProgramRunner{loginQR: map[string]any{
		"state":       "qr_ready",
		"imageBase64": jpeg,
		"imageMime":   "image/jpeg",
		"pageOrigin":  "https://mp.csdn.net/",
	}}

	result, err := egolite.NewSpaceService(database, runner).OpenLoginQR(context.Background(), account.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "qr_ready" || result.ImageDataURL != "data:image/jpeg;base64,"+jpeg {
		t.Fatalf("login QR result=%+v", result)
	}
	program := runner.calls[len(runner.calls)-1]
	if !strings.Contains(program, "backendNodeId: candidate.backendNodeId") {
		t.Fatalf("cross-frame QR must measure iframe nodes by backendNodeId: %s", program)
	}
	for _, required := range []string{
		`platformCode === "wechat_mp"`, `source.hostname === "open.weixin.qq.com"`,
		`source.pathname === "/connect/qrconnect"`, `candidate.kind === "wechat_qr_frame"`,
		`captureBeyondViewport: true, clip: embeddedQR.clip`,
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("cross-frame QR program missing %q: %s", required, program)
		}
	}
	if !strings.Contains(program, "for (let attempt = 0; attempt < 25 && !embeddedQR; attempt++)") ||
		!strings.Contains(program, "DOM.resolveNode") || !strings.Contains(program, "二维码失效|点击重试") {
		t.Fatalf("cross-frame QR must wait for asynchronous codes and refresh an expired code: %s", program)
	}
}

func TestLoginQRRejectsAnotherPlatformsPage(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("平台匹配浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "csdn", "CSDN", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{loginQR: map[string]any{
		"state":       "qr_ready",
		"imageBase64": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB",
		"pageOrigin":  "https://creator.douyin.com/",
	}}

	_, err = egolite.NewSpaceService(database, runner).OpenLoginQR(context.Background(), account.ID, true)
	if err == nil || err.Error() != "EGO_LOGIN_QR_PLATFORM_MISMATCH" {
		t.Fatalf("error=%v, want EGO_LOGIN_QR_PLATFORM_MISMATCH", err)
	}
}

func TestLoginQRPollSelectsOnlyTheRequestedPlatformTab(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("二维码释放测试浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "csdn", "CSDN", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 6, "task-6", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	jpeg := base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 0xe0, 'J', 'F', 'I', 'F'})
	runner := &fakeProgramRunner{loginQR: map[string]any{
		"state": "qr_ready", "imageBase64": jpeg, "imageMime": "image/jpeg",
		"pageOrigin": "https://passport.csdn.net/",
	}}

	_, err = egolite.NewSpaceService(database, runner).OpenLoginQR(context.Background(), account.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	program := runner.calls[len(runner.calls)-1]
	if strings.Contains(program, "creator.douyin.com") || !strings.Contains(program, `const allowedSuffixes = ["csdn.net"]`) ||
		!strings.Contains(program, "await switchTab(matchingTargetId)") || !strings.Contains(program, "takeOverTaskSpace") {
		t.Fatalf("poll did not bind to the requested platform tab: %s", program)
	}
}

func TestLoginQRHandsOffWhenPlatformHasNoScannableCode(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("二维码视频浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "youtube", "YouTube", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{loginQR: map[string]any{
		"state":      "manual_takeover",
		"ownership":  "agentDelegatedToUser",
		"pageOrigin": "https://accounts.google.com/",
	}}
	service := egolite.NewSpaceService(database, runner)

	result, err := service.OpenLoginQR(context.Background(), account.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "manual_takeover" || result.ImageDataURL != "" {
		t.Fatalf("login QR result=%+v", result)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.State != "manual_takeover" || space.Ownership != "agentDelegatedToUser" {
		t.Fatalf("space=%+v", space)
	}
	if !strings.Contains(runner.calls[len(runner.calls)-1], "handOffTaskSpace") {
		t.Fatal("manual fallback did not hand off the Space")
	}
}

func TestEcommerceLoginQRUsesBoundSpaceAndStrictEvidence(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("电商浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "shopify", "Shopify", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{loginQR: map[string]any{
		"state": "manual_takeover", "ownership": "agentDelegatedToUser",
		"pageOrigin": "https://admin.shopify.com/",
	}}
	result, err := egolite.NewSpaceService(database, runner).OpenLoginQR(context.Background(), account.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "manual_takeover" || result.PlatformCode != "shopify" {
		t.Fatalf("result=%+v", result)
	}
	program := runner.calls[len(runner.calls)-1]
	if !strings.Contains(program, "https://admin.shopify.com/") ||
		!strings.Contains(program, "const strictLoginEvidence = true") ||
		!strings.Contains(program, "if (strictLoginEvidence && !loginRule?.host)") ||
		!strings.Contains(program, "handOffTaskSpace") {
		t.Fatalf("e-commerce login program is not bound and fail-closed: %s", program)
	}
}

func TestReleaseLoginQRHandsOffWithoutNavigation(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("二维码轮询测试浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 6, "task-6", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{}
	result, err := egolite.NewSpaceService(database, runner).ReleaseLoginQR(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "manual_takeover" {
		t.Fatalf("result=%+v", result)
	}
	program := runner.calls[0]
	if !strings.Contains(program, "handOffTaskSpace(6)") || strings.Contains(program, "openOrReuseTab") {
		t.Fatalf("release must only hand off the bound Space: %s", program)
	}
}

func TestBackgroundSessionCheckBrieflyReclaimsDelegatedSpaceAndReturnsControl(t *testing.T) {
	database := openServiceDB(t)
	account, err := database.CreateAccount("youtube", "频道", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpace(account.ID, 88, "task-88", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{sessionCheck: map[string]any{
		"authState": "logged_in", "name": "Alpha Channel", "identifier": "UC-88",
		"reasonCode": "VERIFIED_PROFILE_DOM", "evidenceCode": "REVIEWED_PROFILE_DOM",
		"pageOrigin": "https://studio.youtube.com/",
	}}
	service := egolite.NewSpaceService(database, runner)

	result, err := service.CheckAccountSession(context.Background(), *account)
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthState != db.AccountAuthLoggedIn || result.Name != "Alpha Channel" || result.AccountIdentifier != "UC-88" {
		t.Fatalf("result=%+v", result)
	}
	program := runner.calls[0]
	if !strings.Contains(program, "takeOverTaskSpace") || !strings.Contains(program, "handOffTaskSpace") {
		t.Fatalf("background check did not reclaim and return the delegated Space: %s", program)
	}
	if !strings.Contains(program, "listTaskSpaces") || !strings.Contains(program, "closeTab") {
		t.Fatalf("background check did not guard ownership and clean its tab: %s", program)
	}
	if !strings.Contains(program, "waitForNetworkIdle") || !strings.Contains(program, "setTimeout(resolve, 1200)") {
		t.Fatalf("background check must wait for the creator dashboard to render: %s", program)
	}
}

func TestSessionChecksRequireVisibleCredentialSurfaceBeforeLoggingOut(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("保守掉线判定浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "youtube", "频道", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 188, "task-188", "alpha-nexus/matrix-login-state"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{
		sessionCheck: map[string]any{"authState": "unknown"},
		verifyProfiles: []map[string]any{{
			"platformCode": "youtube", "authState": "unknown",
		}},
	}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.CheckAccountSession(context.Background(), *account); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"youtube"}); err != nil {
		t.Fatal(err)
	}

	checkedPrograms := 0
	for _, program := range runner.calls {
		if !strings.Contains(program, "const explicitLogin =") {
			continue
		}
		checkedPrograms++
		assertJavaScriptSyntax(t, program)
		for _, required := range []string{
			"const isVisible = (element) =>",
			"getClientRects().length > 0",
			"hasVisiblePassword",
			"hasVisibleLoginControl",
			"hasVisibleQR",
			"const loginURLHint = /\\/login|signin|passport|\\/auth(?:\\/|$)/i.test(pageURL)",
			"const visibleCredentialSurface = dom.hasVisiblePassword || dom.hasVisibleQR || (loginURLHint && dom.hasVisibleLoginControl)",
			"const explicitLogin = visibleCredentialSurface && loginTextHint",
		} {
			if !strings.Contains(program, required) {
				t.Fatalf("session login evidence is missing %q: %s", required, program)
			}
		}
		if strings.Contains(program, "hasPassword: Boolean(document.querySelector('input[type=\"password\"]'))") ||
			strings.Contains(program, "const explicitLogin = /\\/login|signin|passport|\\/auth(?:\\/|$)/i.test(pageURL) ||") {
			t.Fatalf("ambiguous or hidden login UI can still mark the account logged out: %s", program)
		}
	}
	if checkedPrograms != 2 {
		t.Fatalf("checked login evidence programs=%d, want 2", checkedPrograms)
	}
}

func TestBackgroundSessionCheckUsesReviewedXHSDashboardInsteadOfLoginRedirect(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("小红书检查浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 89, "task-89", "alpha-nexus/matrix-xhs"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{sessionCheck: map[string]any{
		"authState": "logged_in", "reasonCode": "VERIFIED_PROFILE_DOM",
		"evidenceCode": "REVIEWED_PROFILE_DOM", "pageOrigin": "https://creator.xiaohongshu.com/new/home",
	}}
	if _, err := egolite.NewSpaceService(database, runner).CheckAccountSession(context.Background(), *account); err != nil {
		t.Fatal(err)
	}
	program := runner.calls[0]
	if !strings.Contains(program, `const targetURL = "https://creator.xiaohongshu.com/new/home"`) ||
		!strings.Contains(program, `openOrReuseTab(targetURL`) {
		t.Fatalf("session check must use the reviewed XHS dashboard entry: %s", program)
	}
	for _, selector := range []string{`img[alt*="头像"]`, `blog.csdn.net/`} {
		if !strings.Contains(program, selector) {
			t.Fatalf("session profile extraction missing reviewed selector %q", selector)
		}
	}
}

func TestBackgroundSessionCheckReusesAuthenticatedPlatformTabAndUsesReviewedWeChatEntries(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("微信检查浏览器")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 91, "task-91", "alpha-nexus/matrix-wechat"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{sessionCheck: map[string]any{
		"authState": "logged_in", "reasonCode": "VERIFIED_PROFILE_DOM",
		"evidenceCode": "REVIEWED_PROFILE_DOM", "pageOrigin": "https://channels.weixin.qq.com/platform",
	}}
	service := egolite.NewSpaceService(database, runner)

	video, err := database.CreateAccountForIdentity(identity.ID, "wechat_video", "视频号", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CheckAccountSession(context.Background(), *video); err != nil {
		t.Fatal(err)
	}
	videoProgram := runner.calls[len(runner.calls)-1]
	for _, want := range []string{
		`"https://channels.weixin.qq.com/platform"`,
		"const matchingTabs = (await listTabs()).filter",
		"await switchTab(matchingTab.targetId || matchingTab.id)",
		"const videoNameCandidate = dom.bodyText.match",
		"!/^\\d+$/.test(videoNameCandidate)",
		"dom.name = videoName || dom.identifier || \"\"",
	} {
		if !strings.Contains(videoProgram, want) {
			t.Fatalf("video session check missing %q: %s", want, videoProgram)
		}
	}

	mp, err := database.CreateAccountForIdentity(identity.ID, "wechat_mp", "公众号", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CheckAccountSession(context.Background(), *mp); err != nil {
		t.Fatal(err)
	}
	mpProgram := runner.calls[len(runner.calls)-1]
	if !strings.Contains(mpProgram, `"https://mp.weixin.qq.com/cgi-bin/home"`) {
		t.Fatalf("official account session check must use reviewed dashboard: %s", mpProgram)
	}
}

func TestTikTokSessionProfileUsesAccountIdentifierAsDisplayName(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("TikTok 检查浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "tiktok", "旧简介", "agame911")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 92, "task-92", "alpha-nexus/matrix-tiktok"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{sessionCheck: map[string]any{
		"authState": "logged_in", "name": "agame911", "identifier": "agame911",
		"reasonCode": "VERIFIED_PROFILE_DOM", "evidenceCode": "REVIEWED_PROFILE_DOM",
	}}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.CheckAccountSession(context.Background(), *account); err != nil {
		t.Fatal(err)
	}
	program := runner.calls[len(runner.calls)-1]
	if !strings.Contains(program, `dom.name = dom.identifier || ""`) {
		t.Fatalf("TikTok profile must display the account ID, not a bio: %s", program)
	}

	runner.calls = nil
	if _, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"tiktok"}); err != nil {
		t.Fatal(err)
	}
	verifyProgram := runner.calls[len(runner.calls)-1]
	if !strings.Contains(verifyProgram, `dom.name = dom.identifier || ""`) {
		t.Fatalf("TikTok login verification must use the same account-ID contract: %s", verifyProgram)
	}
}

func TestCSDNSessionChecksUsePublicProfileNameInsteadOfNavigationLabel(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("CSDN 名称核验浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "csdn", "旧昵称", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 90, "task-90", "alpha-nexus/matrix-csdn-name"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{
		sessionCheck: map[string]any{
			"authState": "logged_in", "name": "阿尔法智能", "identifier": "weixin_49486457",
			"reasonCode": "VERIFIED_PROFILE_DOM", "evidenceCode": "REVIEWED_PROFILE_DOM",
			"pageOrigin": "https://mp.csdn.net/",
		},
		verifyProfiles: []map[string]any{{
			"platformCode": "csdn", "authState": "logged_in", "name": "阿尔法智能",
			"identifier": "weixin_49486457", "pageOrigin": "https://mp.csdn.net/",
		}},
	}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.CheckAccountSession(context.Background(), *account); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"csdn"}); err != nil {
		t.Fatal(err)
	}
	for _, program := range []string{runner.calls[0], runner.calls[len(runner.calls)-1]} {
		assertJavaScriptSyntax(t, program)
		for _, required := range []string{
			`csdnProfileHref: attr('[class*="user"] a[href*="blog.csdn.net/"], [class*="profile"] a[href*="blog.csdn.net/"]', 'href')`,
			`profileURL.hostname !== "blog.csdn.net"`,
			`"https://blog.csdn.net/" + encodeURIComponent(storedProfilePath)`,
			`await serverFetch(profileURL.href)`,
			`replace(/-CSDN博客$/, "")`,
			`if (publicTitle) dom.name = publicTitle`,
			`dom.name = dom.name || ""`,
		} {
			if !strings.Contains(program, required) {
				t.Fatalf("CSDN profile-name verification missing %q: %s", required, program)
			}
		}
		if strings.Contains(program, `.el_mcm-sub-menu__title`) {
			t.Fatalf("CSDN navigation labels must not be used as an account name: %s", program)
		}
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "阿尔法智能" || updated.AccountIdentifier != "weixin_49486457" {
		t.Fatalf("CSDN profile did not persist the verified public identity: %+v", updated)
	}
}

func TestLoginVerifyResumesOnlyAfterConfirmationAndPersistsProfile(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("用户1")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{verifyProfiles: []map[string]any{{
		"platformCode": "xhs",
		"authState":    "logged_in",
		"name":         "品牌主号",
		"identifier":   "xhs-1024",
		"avatar":       "https://cdn.example.com/avatar.png?token=secret#profile",
		"pageOrigin":   "https://creator.xiaohongshu.com/",
		"evidenceCode": "REVIEWED_PROFILE_DOM",
		"reasonCode":   "VERIFIED_PROFILE_DOM",
	}}}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.OpenLoginBatch(context.Background(), identity.ID, []string{"xhs"}); err != nil {
		t.Fatal(err)
	}

	verification, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"xhs"})
	if err != nil {
		t.Fatal(err)
	}
	if len(verification.Accounts) != 1 || verification.Accounts[0].LoginState != "logged_in" {
		t.Fatalf("verification=%+v", verification)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "品牌主号" || updated.AccountIdentifier != "xhs-1024" || updated.LoginState != "logged_in" {
		t.Fatalf("updated account=%+v", updated)
	}
	if updated.Avatar != "https://cdn.example.com/avatar.png" || updated.AvatarSource != db.AvatarSourcePlatformFallback {
		t.Fatalf("avatar was not safely persisted: %+v", updated)
	}
	program := runner.calls[len(runner.calls)-1]
	assertJavaScriptSyntax(t, program)
	if !strings.HasPrefix(strings.TrimSpace(program), "await takeOverTaskSpace(") {
		t.Fatalf("verification did not start with explicit takeover: %s", program)
	}
	if strings.Contains(program, "openOrReuseTab(targets[0].url") {
		t.Fatalf("verification must inspect an existing platform tab before navigating: %s", program)
	}
	if strings.Contains(program, "snapshotText()") {
		t.Fatalf("verification must reuse the reviewed DOM text instead of taking a second full-page snapshot: %s", program)
	}
	if !strings.Contains(program, "completeTaskSpace") || !strings.Contains(program, "{ keep: true }") {
		t.Fatalf("verification did not retain the Agent-controlled Space: %s", program)
	}
	for _, required := range []string{
		"for (const target of targets)",
		"const reviewedDashboard = matchesReviewedDashboard(target.rule, pageURL, dom.bodyText)",
		`target.platformCode === "douyin"`,
		`douyinName: text('[class*="account-trigger"] [class*="name"]')`,
		`target.platformCode === "csdn"`,
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("verification program is missing reviewed platform login evidence %q: %s", required, program)
		}
	}
	if strings.Contains(program, "while (staleTabs.length > 1)") || strings.Contains(program, "await closeTab(anchorId)") {
		t.Fatal("profile verification must preserve unrelated platform tabs")
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.Ownership != "agent" || space.State != "ready" || space.ErrorCode != "" {
		t.Fatalf("verification space=%+v", space)
	}
}

func TestVerifyLoginBatchControlStateFollowsObservedAuth(t *testing.T) {
	tests := []struct {
		name          string
		authState     string
		wantState     string
		wantOwnership string
		wantError     string
		wantLogin     string
	}{
		{name: "logged in", authState: db.AccountAuthLoggedIn, wantState: "ready", wantOwnership: "agent", wantLogin: db.AccountAuthLoggedIn},
		{name: "unknown", authState: db.AccountAuthUnknown, wantState: "ready", wantOwnership: "agent", wantLogin: db.AccountAuthUnknown},
		{name: "logged out", authState: db.AccountAuthLoggedOut, wantState: "manual_takeover", wantOwnership: "agentDelegatedToUser", wantError: "LOGIN_REQUIRED", wantLogin: db.AccountAuthLoggedOut},
		{name: "checkpoint", authState: "manual_takeover", wantState: "manual_takeover", wantOwnership: "agentDelegatedToUser", wantError: "CHECKPOINT_REQUIRED", wantLogin: db.AccountAuthUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity("登录控制状态 " + test.name)
			if err != nil {
				t.Fatal(err)
			}
			account, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
			if err != nil {
				t.Fatal(err)
			}
			runner := &fakeProgramRunner{verifyProfiles: []map[string]any{{
				"platformCode": "xhs", "authState": test.authState,
				"name": "品牌主号", "identifier": "xhs-control", "pageOrigin": "https://creator.xiaohongshu.com/",
			}}}
			verification, err := egolite.NewSpaceService(database, runner).VerifyLoginBatch(context.Background(), identity.ID, []string{"xhs"})
			if err != nil {
				t.Fatal(err)
			}
			if verification.State != test.wantState {
				t.Fatalf("verification state=%q, want %q", verification.State, test.wantState)
			}
			updated, err := database.GetAccount(account.ID)
			if err != nil {
				t.Fatal(err)
			}
			if updated.LoginState != test.wantLogin {
				t.Fatalf("account login state=%q, want %q", updated.LoginState, test.wantLogin)
			}
			space, err := database.GetEgoSpaceByIdentity(identity.ID)
			if err != nil {
				t.Fatal(err)
			}
			if space.Ownership != test.wantOwnership || space.State != test.wantState || space.ErrorCode != test.wantError {
				t.Fatalf("space=%+v, want ownership=%q state=%q error=%q", space, test.wantOwnership, test.wantState, test.wantError)
			}
			program := runner.calls[len(runner.calls)-1]
			if !strings.Contains(program, `const controlAction = profiles.some`) ||
				!strings.Contains(program, `const completed = await completeTaskSpace`) ||
				!strings.Contains(program, `const handoff = await handOffTaskSpace`) {
				t.Fatalf("verification control program missing guarded actions: %s", program)
			}
		})
	}
}

func TestVerifyLoginBatchControlFailureDoesNotPersistProfiles(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("登录控制失败浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{
		verifyProfiles: []map[string]any{{
			"platformCode": "xhs", "authState": db.AccountAuthLoggedIn,
			"name": "不应持久化", "identifier": "xhs-control-failure", "pageOrigin": "https://creator.xiaohongshu.com/",
		}},
		verifyControlAttempted: boolPointer(true),
		verifyControlDone:      boolPointer(false),
	}
	verification, err := egolite.NewSpaceService(database, runner).VerifyLoginBatch(context.Background(), identity.ID, []string{"xhs"})
	if verification != nil || err == nil || err.Error() != "EGO_LOGIN_CONTROL_FAILED" {
		t.Fatalf("verification=%+v err=%v, want fail-closed control error", verification, err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LoginState != db.AccountAuthUnknown || updated.Name != "小红书" {
		t.Fatalf("profile was persisted before control completion: %+v", updated)
	}
	event, err := database.LatestAccountLoginEvent(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Fatalf("login observation persisted before control completion: %+v", event)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.Ownership != "agent" || space.State != "ready" {
		t.Fatalf("control failure changed Space state: %+v", space)
	}
}

func TestVerifyLoginBatchRejectsMismatchedControlAction(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("登录控制动作不匹配浏览器")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", ""); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{
		verifyProfiles:      []map[string]any{{"platformCode": "xhs", "authState": db.AccountAuthLoggedIn}},
		verifyControlAction: "handoff",
	}
	verification, err := egolite.NewSpaceService(database, runner).VerifyLoginBatch(context.Background(), identity.ID, []string{"xhs"})
	if verification != nil || err == nil || err.Error() != "EGO_LOGIN_CONTROL_FAILED" {
		t.Fatalf("verification=%+v err=%v, want action mismatch to fail closed", verification, err)
	}
}

func TestVerifyLoginBatchRebindsStaleLiveSpaceBeforeTakeover(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("失效绑定登录核验浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if err != nil {
		t.Fatal(err)
	}
	stableName := "alpha-nexus/matrix-" + jsonNumber(identity.ID)
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 77, "stale-task", stableName); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{
		spaceProbe: map[string]any{
			"id": 88, "taskId": "task-88", "name": stableName, "ownership": "agent",
		},
		verifyProfiles: []map[string]any{{
			"platformCode": "xhs", "authState": "logged_in", "name": "品牌主号",
			"identifier": "xhs-88", "pageOrigin": "https://creator.xiaohongshu.com/",
			"evidenceCode": "REVIEWED_PROFILE_DOM", "reasonCode": "VERIFIED_PROFILE_DOM",
		}},
	}

	verification, err := egolite.NewSpaceService(database, runner).VerifyLoginBatch(
		context.Background(), identity.ID, []string{"xhs"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(verification.Accounts) != 1 || verification.Accounts[0].ID != account.ID || verification.Accounts[0].LoginState != db.AccountAuthLoggedIn {
		t.Fatalf("verification=%+v, want logged-in configured account", verification)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("runtime calls=%d, want binding reconciliation then verification", len(runner.calls))
	}
	if !strings.Contains(runner.calls[0], `const taskName = "`+stableName+`"`) {
		t.Fatalf("binding reconciliation did not use stable Space name: %s", runner.calls[0])
	}
	if !strings.HasPrefix(strings.TrimSpace(runner.calls[1]), "await takeOverTaskSpace(88)") ||
		strings.Contains(runner.calls[1], "takeOverTaskSpace(77)") {
		t.Fatalf("verification did not take over the rebound live Space: %s", runner.calls[1])
	}
	if !strings.Contains(runner.calls[1], "completeTaskSpace(88, { keep: true })") {
		t.Fatalf("verification did not retain the rebound live Space: %s", runner.calls[1])
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.SpaceID != 88 || space.TaskID != "task-88" || space.Name != stableName {
		t.Fatalf("space=%+v, want rebound live Space", space)
	}
}

func TestShopLoginVerificationUsesObservedXHSAndXianyuDashboardEvidence(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("店铺登录核验浏览器")
	if err != nil {
		t.Fatal(err)
	}
	for _, platformCode := range []string{"xhs_shop", "xianyu"} {
		if _, err := database.CreateAccountForIdentity(identity.ID, platformCode, platformCode, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 66, "alpha-nexus/matrix-shop", "alpha-nexus/matrix-shop"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{verifyProfiles: []map[string]any{
		{"platformCode": "xhs_shop", "authState": "logged_in", "pageOrigin": "https://ark.xiaohongshu.com/app-system/home"},
		{"platformCode": "xianyu", "authState": "logged_in", "pageOrigin": "https://www.goofish.com/personal"},
	}}
	verification, err := egolite.NewSpaceService(database, runner).VerifyLoginBatch(context.Background(), identity.ID, []string{"xhs_shop", "xianyu"})
	if err != nil {
		t.Fatal(err)
	}
	if len(verification.Accounts) != 2 || verification.Accounts[0].LoginState != db.AccountAuthLoggedIn || verification.Accounts[1].LoginState != db.AccountAuthLoggedIn {
		t.Fatalf("verification=%+v", verification)
	}
	program := runner.calls[len(runner.calls)-1]
	for _, required := range []string{
		`"host":"ark.xiaohongshu.com"`,
		`"requiredText":["店铺管理","商品管理","订单管理"]`,
		`"host":"www.goofish.com"`,
		`"requiredText":["我卖出的","我买到的"]`,
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("shop verification rule missing %q: %s", required, program)
		}
	}
}

func TestLoginVerifyPersistsEgoAvatarCropAsManagedLocalSnapshot(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("头像测试浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{verifyProfiles: []map[string]any{{
		"platformCode": "douyin",
		"authState":    "logged_in",
		"name":         "文",
		"identifier":   "DS911_",
		"avatarBase64": observedAvatarBase64(t),
		"pageOrigin":   "https://creator.douyin.com/creator-micro/home",
	}}}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.OpenLoginBatch(context.Background(), identity.ID, []string{"douyin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"douyin"}); err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.AvatarSource != db.AvatarSourceObserved || len(updated.AvatarSHA256) != 64 || !strings.HasPrefix(updated.Avatar, "account-avatars/account-") {
		t.Fatalf("managed avatar was not persisted: %+v", updated)
	}
	payload, err := os.ReadFile(filepath.Join(database.DataDir, filepath.FromSlash(updated.Avatar)))
	if err != nil || len(payload) == 0 {
		t.Fatalf("read managed avatar: bytes=%d err=%v", len(payload), err)
	}
	program := runner.calls[len(runner.calls)-1]
	for _, required := range []string{"avatarClip", `cdp("Page.captureScreenshot"`, "avatarBase64", `ytcp-navigation-drawer img.thumbnail`, `platformCode === "youtube"`} {
		if !strings.Contains(program, required) {
			t.Fatalf("verification program is missing %q: %s", required, program)
		}
	}
	if got := strings.Count(program, "const avatarClip ="); got != 1 {
		t.Fatalf("verification program redeclared avatarClip %d times", got)
	}
	if strings.Contains(program, "while (staleTabs.length > 1)") {
		t.Fatal("login verification must not close unrelated platform tabs")
	}
}

func TestMetaLoginVerifyPersistsSeparateFacebookAndInstagramAvatars(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("Meta 头像浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "meta", "AlphaIntelligence", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{verifyProfiles: []map[string]any{{
		"platformCode": "meta", "authState": "logged_in", "name": "AlphaIntelligence",
		"identifier": "1241410172381837", "avatarBase64": observedAvatarBase64(t),
		"pageOrigin": "https://business.facebook.com/latest/home?asset_id=1241410172381837",
		"metaAssets": []map[string]any{
			{"platformCode": "facebook", "name": "Alpha Page", "externalID": "1241410172381837", "connectedPlatformCode": "instagram", "avatarBase64": observedAvatarBase64(t)},
			{"platformCode": "instagram", "name": "Alpha IG", "externalID": "987654321", "connectedPlatformCode": "facebook", "avatarBase64": observedAvatarBase64(t)},
		},
	}}}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.OpenLoginBatch(context.Background(), identity.ID, []string{"meta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"meta"}); err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]db.AccountAsset{}
	for _, asset := range assets {
		if asset.PlatformCode == "facebook" || asset.PlatformCode == "instagram" {
			seen[asset.PlatformCode] = asset
		}
	}
	for _, code := range []string{"facebook", "instagram"} {
		asset, ok := seen[code]
		if !ok || !strings.HasPrefix(asset.Avatar, "account-asset-avatars/asset-") {
			t.Fatalf("%s asset avatar not persisted: %+v", code, asset)
		}
	}
	if seen["facebook"].ExternalID != "1241410172381837" || seen["instagram"].ExternalID != "987654321" {
		t.Fatalf("Meta public IDs were not persisted: %+v", seen)
	}
	if seen["facebook"].ConnectedAssetID != seen["instagram"].ID ||
		seen["instagram"].ConnectedAssetID != seen["facebook"].ID ||
		seen["facebook"].ConnectedAssetName != seen["instagram"].Name ||
		seen["instagram"].ConnectedAssetName != seen["facebook"].Name {
		t.Fatalf("Meta assets were not connected bidirectionally: %+v", seen)
	}
	program := runner.calls[len(runner.calls)-1]
	for _, required := range []string{"metaAssetCandidates", "metaAssets", "externalID", "connectedPlatformCode", `nextBrand === "Facebook"`, `nextBrand === "Instagram"`, "searchParams.get(key)", "alpha-meta-avatar-capture"} {
		if !strings.Contains(program, required) {
			t.Fatalf("Meta verifier missing %q", required)
		}
	}
}

func TestMetaLoginVerifyUpgradesNameOnlyIDsAndConnectsOnlyWithTheSameEvidence(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("Meta ID 升级浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "meta", "AlphaIntelligence", "")
	if err != nil {
		t.Fatal(err)
	}
	facebook, err := database.CreateAccountAsset(account.ID, "facebook", "facebook_page", "Alpha Page", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	instagram, err := database.CreateAccountAsset(account.ID, "instagram", "instagram_account", "Alpha IG", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{verifyProfiles: []map[string]any{{
		"platformCode": "meta", "authState": "logged_in", "name": "AlphaIntelligence",
		"pageOrigin": "https://business.facebook.com/latest/home",
		"metaAssets": []map[string]any{
			{"platformCode": "facebook", "name": "Alpha Page", "externalID": "333333333"},
			{"platformCode": "instagram", "name": "Alpha IG", "externalID": "444444444"},
		},
	}}}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.OpenLoginBatch(context.Background(), identity.ID, []string{"meta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"meta"}); err != nil {
		t.Fatal(err)
	}
	updatedFacebook, err := database.GetAccountAsset(facebook.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedInstagram, err := database.GetAccountAsset(instagram.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedFacebook.ExternalID != "333333333" || updatedInstagram.ExternalID != "444444444" ||
		updatedFacebook.ConnectedAssetID != updatedInstagram.ID || updatedInstagram.ConnectedAssetID != updatedFacebook.ID {
		t.Fatalf("name-only Meta assets were not safely upgraded: facebook=%+v instagram=%+v", updatedFacebook, updatedInstagram)
	}
}

func TestMetaLoginVerifyDoesNotConnectNameOnlyAssets(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("Meta 无 ID 浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "meta", "AlphaIntelligence", "")
	if err != nil {
		t.Fatal(err)
	}
	facebook, err := database.CreateAccountAsset(account.ID, "facebook", "facebook_page", "Alpha Page", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	instagram, err := database.CreateAccountAsset(account.ID, "instagram", "instagram_account", "Alpha IG", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{verifyProfiles: []map[string]any{{
		"platformCode": "meta", "authState": "logged_in", "name": "AlphaIntelligence",
		"pageOrigin": "https://business.facebook.com/latest/home",
		"metaAssets": []map[string]any{
			{"platformCode": "facebook", "name": "Alpha Page"},
			{"platformCode": "instagram", "name": "Alpha IG"},
		},
	}}}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.OpenLoginBatch(context.Background(), identity.ID, []string{"meta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"meta"}); err != nil {
		t.Fatal(err)
	}
	updatedFacebook, err := database.GetAccountAsset(facebook.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedInstagram, err := database.GetAccountAsset(instagram.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedFacebook.ConnectedAssetID != 0 || updatedInstagram.ConnectedAssetID != 0 {
		t.Fatalf("name-only Meta assets were connected without public IDs: facebook=%+v instagram=%+v", updatedFacebook, updatedInstagram)
	}
}

func TestMetaLoginVerifyRejectsPublicIDConflict(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("Meta ID 冲突浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "meta", "AlphaIntelligence", "")
	if err != nil {
		t.Fatal(err)
	}
	facebook, err := database.CreateAccountAsset(account.ID, "facebook", "facebook_page", "Alpha Page", "111111111", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{verifyProfiles: []map[string]any{{
		"platformCode": "meta", "authState": "logged_in", "name": "AlphaIntelligence",
		"identifier": "1241410172381837", "pageOrigin": "https://business.facebook.com/latest/home",
		"metaAssets": []map[string]any{{
			"platformCode": "facebook", "name": "Alpha Page", "externalID": "222222222",
		}},
	}}}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.OpenLoginBatch(context.Background(), identity.ID, []string{"meta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"meta"}); err == nil || err.Error() != "EGO_META_ASSET_ID_CONFLICT" {
		t.Fatalf("err=%v, want EGO_META_ASSET_ID_CONFLICT", err)
	}
	unchanged, err := database.GetAccountAsset(facebook.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.ExternalID != "111111111" || unchanged.ConnectedAssetID != 0 {
		t.Fatalf("conflicting Meta asset was mutated: %+v", unchanged)
	}
}

func TestMetaPublishUsesLoggedInMetaContainerInsteadOfLegacyAccounts(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("Meta 发布容器浏览器")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := database.CreateAccountForIdentity(identity.ID, "meta", "Meta operator", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(meta.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	page, err := database.CreateAccountAsset(meta.ID, "facebook", "facebook_page", "Alpha Page", "1241410172381837", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := database.CreateAccountForIdentity(identity.ID, "facebook", "Legacy Facebook", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(legacy.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "facebook", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "facebook", ContentType: "graph_text", AssetExternalID: page.ExternalID, AssetName: page.Name, Title: "Meta 标题", Body: "Meta 正文",
		Media: []egolite.PublishMedia{writeMetaPublishMedia(t)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.AccountIDs) != 1 || batch.AccountIDs[0] != meta.ID {
		t.Fatalf("Meta publish selected legacy account: batch=%+v legacy=%d meta=%d", batch, legacy.ID, meta.ID)
	}
}

func TestMetaSubmitValidationRejectsLegacyOnlyAccount(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("Meta 提交边界浏览器")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := database.CreateAccountForIdentity(identity.ID, "instagram", "Legacy Instagram", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(legacy.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	service := egolite.NewSpaceService(database, &fakeProgramRunner{})
	err = service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{{
		PlatformCode: "instagram", ContentType: "graph_text", Title: "Meta 标题", Body: "Meta 正文",
	}})
	if err == nil || err.Error() != "EGO_PUBLISH_ACCOUNT_UNAVAILABLE" {
		t.Fatalf("legacy-only Meta submit err=%v, want EGO_PUBLISH_ACCOUNT_UNAVAILABLE", err)
	}
}

func TestWeChatOfficialLoginVerifierUsesReviewedDashboardAndAccountPortrait(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("公众号浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "wechat_mp", "微信公众号", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{verifyProfiles: []map[string]any{{
		"platformCode": "wechat_mp", "authState": "logged_in", "name": "AlphaIntelligence",
		"avatarBase64": observedAvatarBase64(t),
		"pageOrigin":   "https://mp.weixin.qq.com/cgi-bin/home?t=home/index&lang=zh_CN",
	}}}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.OpenLoginBatch(context.Background(), identity.ID, []string{"wechat_mp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"wechat_mp"}); err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LoginState != db.AccountAuthLoggedIn || updated.Name != "AlphaIntelligence" || updated.AvatarSource != db.AvatarSourceObserved {
		t.Fatalf("公众号资料未同步: %+v", updated)
	}
	program := runner.calls[len(runner.calls)-1]
	for _, required := range []string{
		`"host":"mp.weixin.qq.com"`, `"pathPrefixes":["/cgi-bin/"]`,
		`"requiredText":["内容管理","互动管理","数据分析","设置与开发"]`,
		`.acount_box-nickname`, `.weui-desktop_name`, `.weui-desktop-account__img`, `target.platformCode === "wechat_mp"`,
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("公众号验证程序缺少 %q", required)
		}
	}
}

func TestSafeAvatarURLRejectsLocalAndCredentialBearingReferences(t *testing.T) {
	for _, raw := range []string{
		"http://cdn.example.com/avatar.png",
		"https://localhost/avatar.png",
		"https://127.0.0.1/avatar.png",
		"https://user:pass@cdn.example.com/avatar.png",
		"data:image/png;base64,secret",
	} {
		if got := egolite.SafeAvatarURL(raw); got != "" {
			t.Fatalf("SafeAvatarURL(%q)=%q", raw, got)
		}
	}
	if got := egolite.SafeAvatarURL("https://cdn.example.com/avatar.png?token=secret#x"); got != "https://cdn.example.com/avatar.png" {
		t.Fatalf("sanitized avatar=%q", got)
	}
}

func TestEnsureIdentitySpaceIsSharedByMediaAndShopAccounts(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("用户1")
	if err != nil {
		t.Fatal(err)
	}
	first, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.CreateAccountForIdentity(identity.ID, "shopify", "Shopify", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)

	space, err := service.EnsureIdentitySpace(context.Background(), identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantName := "alpha-nexus/matrix-" + jsonNumber(identity.ID)
	if space.Name != wantName || space.SpaceID != 6 || space.TaskID != "task-6" {
		t.Fatalf("space = %+v, want name %q", space, wantName)
	}

	reused, err := service.EnsureAccountSpace(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reused.ID != space.ID || reused.SpaceID != space.SpaceID || len(runner.calls) != 2 {
		t.Fatalf("reused = %+v, runtime calls = %d", reused, len(runner.calls))
	}
	firstSpace, err := database.GetEgoSpaceByAccount(first.ID)
	if err != nil || firstSpace.ID != space.ID || firstSpace.SpaceID != space.SpaceID {
		t.Fatalf("first platform binding = %+v err=%v", firstSpace, err)
	}
}

func TestEnsureIdentitySpaceRebindsMissingLiveSpaceByStableName(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("稳定名称恢复测试浏览器")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 77, "stale-task", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)

	space, err := service.EnsureIdentitySpace(context.Background(), identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.SpaceID != 6 || space.TaskID != "task-6" || space.Name != "alpha-nexus/matrix-1" {
		t.Fatalf("rebound space=%+v", space)
	}
	if !strings.Contains(runner.calls[0], `const taskName = "alpha-nexus/matrix-1"`) ||
		!strings.Contains(runner.calls[0], "ego.createTaskSpace(taskName, profile.id)") {
		t.Fatalf("missing Space was not recovered by stable name: %s", runner.calls[0])
	}
}

func TestEnsureIdentitySpaceRejectsUnrelatedStoredTaskAndBindsNamedProfile(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("专属 Profile 绑定测试浏览器")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 1, "google-flow-cli", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{spaceProbe: map[string]any{
		"id": 8, "taskId": "alpha-nexus/matrix-1", "name": "alpha-nexus/matrix-1", "ownership": "agent",
	}}

	space, err := egolite.NewSpaceService(database, runner).EnsureIdentitySpace(context.Background(), identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.SpaceID != 8 || space.TaskID != "alpha-nexus/matrix-1" || space.Name != "alpha-nexus/matrix-1" {
		t.Fatalf("space=%+v, want dedicated Alpha-Nexus Space", space)
	}
	program := runner.calls[0]
	for _, required := range []string{
		"await listProfiles()",
		"item.profileId",
		"profile.id",
		"ego.createTaskSpace(taskName, profile.id)",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("profile-bound Space recovery missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "Number(item.id) === 1") || strings.Contains(program, "boundTaskId") {
		t.Fatalf("unrelated stored task can still win by stale id/task binding: %s", program)
	}
	assertJavaScriptSyntax(t, program)
}

func TestEnsureIdentitySpaceRebindsStableTaskToZeroBasedLiveID(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("零号 Space 恢复测试浏览器")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 77, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{spaceProbe: map[string]any{
		"id": 0, "taskId": "alpha-nexus/matrix-1", "name": "alpha-nexus/matrix-1", "ownership": "user",
	}}
	space, err := egolite.NewSpaceService(database, runner).EnsureIdentitySpace(context.Background(), identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.SpaceID != 0 || space.TaskID != "alpha-nexus/matrix-1" {
		t.Fatalf("space=%+v, want stable task rebound to live ID 0", space)
	}
	if !strings.Contains(runner.calls[0], "item.taskId") {
		t.Fatalf("runtime binding did not prefer stable taskId: %s", runner.calls[0])
	}
}

func TestLoginHandoffAndExplicitResumeUpdateSpaceOwnership(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("用户2")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "youtube", "频道", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.EnsureAccountSpace(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}

	handedOff, err := service.OpenLoginAndHandoff(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if handedOff.Ownership != "agentDelegatedToUser" || handedOff.State != "manual_takeover" {
		t.Fatalf("handed off = %+v", handedOff)
	}

	resumed, err := service.ResumeAfterConfirmation(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Ownership != "agent" || resumed.State != "ready" || resumed.ControlEpoch != 3 {
		t.Fatalf("resumed = %+v", resumed)
	}
	resumeProgram := runner.calls[len(runner.calls)-1]
	if !strings.Contains(resumeProgram, "const spaces = await listTaskSpaces()") ||
		!strings.Contains(resumeProgram, `boundSpace.ownership === "user"`) ||
		!strings.Contains(resumeProgram, `boundSpace.ownership === "agentDelegatedToUser"`) ||
		!strings.Contains(resumeProgram, "await takeOverTaskSpace(") ||
		!strings.Contains(resumeProgram, "await claimTaskSpace(") {
		t.Fatalf("explicit resume must reclaim user control or claim an inactive exact EgoLite Space: %s", resumeProgram)
	}
	if !strings.Contains(resumeProgram, `Number(item.id) ===`) ||
		!strings.Contains(resumeProgram, `ownership === "agent"`) {
		t.Fatalf("explicit resume must read back exact agent ownership: %s", resumeProgram)
	}
}

func TestExplicitResumeFailsClosedWithoutExactAgentOwnership(t *testing.T) {
	tests := []struct {
		name   string
		result map[string]any
	}{
		{name: "claim did not transfer ownership", result: map[string]any{"id": 6, "done": false, "ownership": "user"}},
		{name: "claim returned another Space", result: map[string]any{"id": 99, "done": true, "ownership": "agent"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity("显式归还失败")
			if err != nil {
				t.Fatal(err)
			}
			account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
			if err != nil {
				t.Fatal(err)
			}
			runner := &fakeProgramRunner{claimResult: test.result}
			service := egolite.NewSpaceService(database, runner)
			if _, err := service.EnsureAccountSpace(context.Background(), account.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := database.UpdateEgoSpaceStateForIdentity(identity.ID, "agentDelegatedToUser", "manual_takeover", ""); err != nil {
				t.Fatal(err)
			}

			resumed, err := service.ResumeAfterConfirmation(context.Background(), account.ID)
			if resumed != nil || err == nil || err.Error() != "EGO_RESUME_FAILED" {
				t.Fatalf("resumed=%+v err=%v, want exact claim failure", resumed, err)
			}
			stored, err := database.GetEgoSpaceByIdentity(identity.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Ownership != "agentDelegatedToUser" || stored.State != "manual_takeover" {
				t.Fatalf("failed claim changed persisted Space: %+v", stored)
			}
		})
	}
}

func TestLoginBatchOpensAtMostThreeCreatorBackendsInOneSpace(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("国内矩阵")
	if err != nil {
		t.Fatal(err)
	}
	codes := []string{"douyin", "wechat_video", "kuaishou", "xhs"}
	for _, code := range codes {
		if _, err := database.CreateAccountForIdentity(identity.ID, code, code, ""); err != nil {
			t.Fatal(err)
		}
	}
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)

	batch, err := service.OpenLoginBatch(context.Background(), identity.ID, codes)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.PlatformCodes) != 3 || strings.Join(batch.PlatformCodes, ",") != "douyin,wechat_video,kuaishou" {
		t.Fatalf("batch platforms = %v", batch.PlatformCodes)
	}
	if batch.State != "manual_takeover" {
		t.Fatalf("batch state = %q", batch.State)
	}
	program := runner.calls[len(runner.calls)-1]
	if strings.Count(program, "openOrReuseTab(") != 3 {
		t.Fatalf("open calls = %d, program=%s", strings.Count(program, "openOrReuseTab("), program)
	}
	if strings.Contains(program, "localhost") || strings.Contains(program, "127.0.0.1") {
		t.Fatalf("login batch opened Alpha-Nexus UI: %s", program)
	}
	if !strings.Contains(program, "takeOverTaskSpace(") || !strings.Contains(program, "handOffTaskSpace(") {
		t.Fatalf("login batch must reclaim only after the UI click and hand control back: %s", program)
	}
	if strings.Count(program, "setTimeout(resolve, 800)") < 2 {
		t.Fatalf("multi-platform navigation must use a fixed settle interval between pages: %s", program)
	}
}

func TestXHSPrepareUsesFrozenMediaAndNeverClicksFinalPublish(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("小红书准备浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen-xhs-image.png")
	mediaBytes := []byte("frozen-xhs-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "xhs", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)

	page := egolite.PublishPage{
		PlatformCode: "xhs", ContentType: "graph_text", Title: "冻结小红书标题", Body: "冻结小红书正文",
		TagsJSON: `["AI", "内容"]`,
		Media:    []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "image"}},
	}
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(batch.PreparedPlatformCodes, ",") != "xhs" || len(batch.PlatformErrors) != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"creator.xiaohongshu.com/publish/publish?from=menu", "target=image",
		"uploadFile(xhsImageInputSelector", "冻结小红书标题", "冻结小红书正文",
		"#topicBtn", "pressKey(\"SPACE\")", "a.tiptap-topic[data-topic]",
		"xhs_topic_readback_mismatch", "xhs-publish-btn", "xhs_final_button_missing_or_ambiguous",
		"awaiting_review",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("XHS prepare program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "click(xhsFinalSelector") || strings.Contains(program, "click(finalSelector") || strings.Contains(program, "submit approved publish") {
		t.Fatalf("XHS prepare crossed final publish boundary: %s", program)
	}
	if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{page}); err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
		t.Fatalf("XHS submit must remain fail-closed, err=%v", err)
	}
}

func TestXHSPrepareRejectsUnsafeFrozenInputsBeforeEgoLite(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("小红书约束浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "xhs-image.png")
	imageBytes := []byte("xhs-image")
	if err := os.WriteFile(imagePath, imageBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	imageDigest := sha256.Sum256(imageBytes)
	videoPath := filepath.Join(t.TempDir(), "xhs-video.mp4")
	videoBytes := []byte("xhs-video")
	if err := os.WriteFile(videoPath, videoBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	videoDigest := sha256.Sum256(videoBytes)
	image := egolite.PublishMedia{FilePath: imagePath, SHA256: hex.EncodeToString(imageDigest[:]), Kind: "image", Role: "image"}
	video := egolite.PublishMedia{FilePath: videoPath, SHA256: hex.EncodeToString(videoDigest[:]), Kind: "video", Role: "video"}
	base := egolite.PublishPage{
		PlatformCode: "xhs", ContentType: "graph_text", Title: "冻结标题", Body: "冻结正文",
		SettingsJSON: `{"visibility":"public","comment_enabled":true,"declare_original":false,"location":"","collection":"","group":"","related_media":"","content_source":"none","allow_duet":false,"allow_copy":false}`,
		Media:        []egolite.PublishMedia{image},
	}
	cases := []struct {
		name string
		page egolite.PublishPage
		want string
	}{
		{name: "non-default settings", page: func() egolite.PublishPage {
			value := base
			value.SettingsJSON = `{"visibility":"private"}`
			return value
		}(), want: "EGO_XHS_SETTINGS_UNVERIFIED"},
		{name: "wrong image role", page: func() egolite.PublishPage {
			value := base
			value.Media = []egolite.PublishMedia{{FilePath: imagePath, SHA256: hex.EncodeToString(imageDigest[:]), Kind: "image", Role: "body"}}
			return value
		}(), want: "EGO_XHS_MEDIA_INVALID"},
		{name: "wrong video kind", page: func() egolite.PublishPage {
			value := base
			value.ContentType = "video"
			value.Media = []egolite.PublishMedia{{FilePath: videoPath, SHA256: hex.EncodeToString(videoDigest[:]), Kind: "image", Role: "video"}}
			return value
		}(), want: "EGO_XHS_MEDIA_INVALID"},
		{name: "extra video media", page: func() egolite.PublishPage {
			value := base
			value.ContentType = "video"
			value.Media = []egolite.PublishMedia{video, video}
			return value
		}(), want: "EGO_XHS_MEDIA_INVALID"},
		{name: "too many tags", page: func() egolite.PublishPage {
			value := base
			value.TagsJSON = `["one","two","three","four"]`
			return value
		}(), want: "EGO_XHS_TAGS_INVALID"},
		{name: "missing title", page: func() egolite.PublishPage { value := base; value.Title = ""; return value }(), want: "EGO_PUBLISH_CONTENT_INVALID"},
		{name: "missing body", page: func() egolite.PublishPage { value := base; value.Body = ""; value.Caption = ""; return value }(), want: "EGO_PUBLISH_CONTENT_INVALID"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeProgramRunner{}
			_, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{test.page})
			if err == nil || err.Error() != test.want {
				t.Fatalf("err=%v, want %s", err, test.want)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("unsafe XHS input touched EgoLite: calls=%d", len(runner.calls))
			}
		})
	}
}

func TestDouyinPrepareUsesFrozenMediaAndNeverClicksFinalPublish(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("抖音发布浏览器")
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
	mediaPath := filepath.Join(t.TempDir(), "cover.png")
	mediaBytes := []byte("frozen-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "douyin", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "douyin", ContentType: "graph_text", Title: "冻结标题",
		Body: "冻结正文", TagsJSON: `["AlphaNexus"]`,
		Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "body"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(batch.PreparedPlatformCodes, ",") != "douyin" || len(batch.PlatformErrors) != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{"uploadFile(inputSelector", "冻结标题", "冻结正文", "final_review_missing"} {
		if !strings.Contains(program, required) {
			t.Fatalf("prepare program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "submit approved publish") || strings.Contains(program, "await click(selector") {
		t.Fatalf("prepare program reached final submit: %s", program)
	}
}

func TestDouyinPublishMediaContractAllowsTwoFrozenImagesAndRejectsInvalidShapes(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("抖音多图发布浏览器")
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
	writeMedia := func(name, kind string) egolite.PublishMedia {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		payload := []byte(name)
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(payload)
		return egolite.PublishMedia{FilePath: path, SHA256: hex.EncodeToString(digest[:]), Kind: kind, Role: "content"}
	}
	first := writeMedia("first.png", "image")
	second := writeMedia("second.png", "image")
	page := egolite.PublishPage{
		PlatformCode: "douyin", ContentType: "graph_text", Title: "冻结标题", Body: "冻结正文",
		Media: []egolite.PublishMedia{first, second},
	}
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "douyin", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
	if err != nil || batch == nil || strings.Join(batch.PreparedPlatformCodes, ",") != "douyin" {
		t.Fatalf("prepare batch=%+v err=%v", batch, err)
	}
	if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{page}); err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
		t.Fatalf("two frozen images must pass submit media validation before the verified-submit gate, err=%v", err)
	}

	tooMany := make([]egolite.PublishMedia, 31)
	for index := range tooMany {
		tooMany[index] = first
	}
	video := writeMedia("video.mp4", "video")
	cases := []struct {
		name string
		page egolite.PublishPage
		want string
	}{
		{name: "graph text without media", page: egolite.PublishPage{PlatformCode: "douyin", ContentType: "graph_text", Title: "标题", Body: "正文"}, want: "EGO_PUBLISH_MEDIA_REQUIRED"},
		{name: "graph text with 31 images", page: egolite.PublishPage{PlatformCode: "douyin", ContentType: "graph_text", Title: "标题", Body: "正文", Media: tooMany}, want: "EGO_DOUYIN_MEDIA_INVALID"},
		{name: "graph text with video", page: egolite.PublishPage{PlatformCode: "douyin", ContentType: "graph_text", Title: "标题", Body: "正文", Media: []egolite.PublishMedia{video}}, want: "EGO_DOUYIN_MEDIA_INVALID"},
		{name: "video with image", page: egolite.PublishPage{PlatformCode: "douyin", ContentType: "video", Title: "标题", Body: "正文", Media: []egolite.PublishMedia{first}}, want: "EGO_DOUYIN_MEDIA_INVALID"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			prepareRunner := &fakeProgramRunner{}
			if _, err := egolite.NewSpaceService(database, prepareRunner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{test.page}); err == nil || err.Error() != test.want {
				t.Fatalf("prepare err=%v, want %s", err, test.want)
			}
			if len(prepareRunner.calls) != 0 {
				t.Fatalf("invalid Douyin media reached prepare runtime: calls=%d", len(prepareRunner.calls))
			}
			if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{test.page}); err == nil || err.Error() != test.want {
				t.Fatalf("submit err=%v, want %s", err, test.want)
			}
		})
	}
}

func TestSanitizePublishPrepareDiagnostic(t *testing.T) {
	run := func(t *testing.T, diagnostic map[string]any) *egolite.LoginBatch {
		t.Helper()
		database := openServiceDB(t)
		identity, _ := database.CreateEgoIdentity("抖音诊断浏览器")
		account, _ := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
		if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
			t.Fatal(err)
		}
		runner := &fakeProgramRunner{publishPrepare: map[string]any{
			"done": true, "completionAttempted": true, "completionDone": true,
			"results": []map[string]any{{
				"platformCode": "douyin", "status": "manual_takeover",
				"errorCode": "EGO_DOUYIN_TOPIC_NATIVE_COMMIT_FAILED", "diagnostic": diagnostic,
			}},
		}}
		mediaPath := filepath.Join(t.TempDir(), "frozen-image.png")
		mediaBytes := []byte("frozen-image")
		if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(mediaBytes)
		batch, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
			PlatformCode: "douyin", ContentType: "graph_text", Title: "标题", Body: "正文", TagsJSON: `[]`,
			Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "image"}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return batch
	}

	valid := run(t, map[string]any{
		"branch": "verified_space", "stage": "native_commit", "bodyTail": strings.Repeat("尾", 90),
		"topics":    []string{" \u200btopic-one\u00a0\ufeff ", strings.Repeat("话", 90), " topic\tthree\nwith spaces ", "topic-four"},
		"outerHTML": "secret", "cookie": "secret",
		"nodes": []map[string]any{{
			"tag": "a", "text": strings.Repeat("话", 90), "className": "secret", "id": "secret",
			"attributes": map[string]any{"data-mention": "#", "data-topic": "αgamer", "contenteditable": "false", "onclick": "secret"},
		}},
	})
	diagnostic, ok := valid.PlatformDiagnostics["douyin"]
	if !ok || diagnostic.Branch != "verified_space" || diagnostic.Stage != "native_commit" || len([]rune(diagnostic.BodyTail)) != 80 || len(diagnostic.Nodes) != 1 || len(diagnostic.Topics) != 4 {
		t.Fatalf("diagnostic=%+v", diagnostic)
	}
	expectedTopics := []string{"topic-one", strings.Repeat("话", 80), "topic three with spaces", "topic-four"}
	for index, expected := range expectedTopics {
		if diagnostic.Topics[index] != expected {
			t.Fatalf("topics[%d]=%q, want %q", index, diagnostic.Topics[index], expected)
		}
	}
	encoded, _ := json.Marshal(diagnostic)
	if len(encoded) > 4096 || bytes.Contains(encoded, []byte("outerHTML")) || bytes.Contains(encoded, []byte("cookie")) ||
		bytes.Contains(encoded, []byte("className")) || bytes.Contains(encoded, []byte("onclick")) || bytes.Contains(encoded, []byte(`"id"`)) {
		t.Fatalf("unsafe diagnostic=%s", encoded)
	}

	oversized := run(t, map[string]any{"branch": "verified_space", "stage": "native_commit", "bodyTail": strings.Repeat("x", 5000)})
	if len(oversized.PlatformDiagnostics) != 0 {
		t.Fatalf("oversized diagnostic passed through: %+v", oversized.PlatformDiagnostics)
	}
	tooManyTopics := run(t, map[string]any{
		"branch": "verified_space", "stage": "native_commit",
		"topics": []string{"one", "two", "three", "four", "five"},
	})
	if len(tooManyTopics.PlatformDiagnostics) != 0 {
		t.Fatalf("too many topics passed through: %+v", tooManyTopics.PlatformDiagnostics)
	}
	malformed := run(t, map[string]any{"branch": "unknown", "stage": "native_commit", "bodyTail": "tail"})
	if len(malformed.PlatformDiagnostics) != 0 {
		t.Fatalf("malformed diagnostic passed through: %+v", malformed.PlatformDiagnostics)
	}
	malformedStage := run(t, map[string]any{"branch": "verified_space", "stage": "unknown", "bodyTail": "tail"})
	if len(malformedStage.PlatformDiagnostics) != 0 {
		t.Fatalf("malformed stage diagnostic passed through: %+v", malformedStage.PlatformDiagnostics)
	}
}

func TestDouyinScheduledPrepareAndSubmitAllowOnlyNativeVideoAndGraphText(t *testing.T) {
	for _, contentType := range []string{"graph_text", "video"} {
		t.Run(contentType, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity("抖音原生排期浏览器")
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
			extension := ".png"
			kind, role := "image", "body"
			if contentType == "video" {
				extension, kind, role = ".mp4", "video", "video"
			}
			mediaPath := filepath.Join(t.TempDir(), "frozen-douyin"+extension)
			mediaBytes := []byte("frozen-douyin-" + contentType)
			if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(mediaBytes)
			page := egolite.PublishPage{
				PlatformCode: "douyin", ContentType: contentType, Title: "冻结抖音排期标题", Body: "冻结抖音排期正文",
				PublishAt: time.Now().Add(2 * time.Hour).Truncate(time.Minute).Format(time.RFC3339),
				Media:     []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: kind, Role: role}},
			}
			runner := &fakeProgramRunner{publishPrepare: map[string]any{
				"done": true, "completionAttempted": true, "completionDone": true,
				"results": []map[string]any{{"platformCode": "douyin", "status": "awaiting_review"}},
			}}
			service := egolite.NewSpaceService(database, runner)
			batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(batch.PreparedPlatformCodes, ",") != "douyin" || len(batch.PlatformErrors) != 0 {
				t.Fatalf("batch=%+v", batch)
			}
			program := publishPrepareProgram(t, runner)
			assertJavaScriptSyntax(t, program)
			for _, required := range []string{"定时发布", "日期和时间", "douyin_schedule_time_readback_mismatch", "douyin_final_button_missing_or_ambiguous"} {
				if !strings.Contains(program, required) {
					t.Fatalf("scheduled %s prepare missing %q", contentType, required)
				}
			}
			if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{page}); err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
				t.Fatalf("scheduled %s submit must remain fail-closed, err=%v", contentType, err)
			}
		})
	}
}

func TestDouyinScheduledPublishRejectsMalformedOrPastPublishAtBeforeEgoLite(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("抖音排期时间约束")
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
	mediaPath := filepath.Join(t.TempDir(), "frozen-douyin.png")
	mediaBytes := []byte("frozen-douyin-time")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	base := egolite.PublishPage{
		PlatformCode: "douyin", ContentType: "graph_text", Title: "冻结标题", Body: "冻结正文",
		Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "body"}},
	}
	for _, test := range []struct {
		name      string
		publishAt string
	}{
		{name: "malformed", publishAt: "2030-01-02 13:00:00+08:00"},
		{name: "seconds not supported by native minute control", publishAt: "2030-01-02T13:00:01+08:00"},
		{name: "past", publishAt: time.Now().Add(-time.Hour).Truncate(time.Minute).Format(time.RFC3339)},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeProgramRunner{}
			page := base
			page.PublishAt = test.publishAt
			_, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
			if err == nil || err.Error() != "EGO_PUBLISH_SCHEDULE_INVALID" {
				t.Fatalf("err=%v, want EGO_PUBLISH_SCHEDULE_INVALID", err)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("invalid schedule touched EgoLite: calls=%d", len(runner.calls))
			}
		})
	}
}

func TestKuaishouPrepareUsesFrozenMediaAndNeverClicksFinalPublish(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("快手发布浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "kuaishou", "快手", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen-image.png")
	mediaBytes := []byte("frozen-kuaishou-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "kuaishou", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "kuaishou", ContentType: "graph_text", Title: "冻结快手标题",
		Body: "冻结快手文案", TagsJSON: `[]`,
		Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "video", Role: "body"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(batch.PreparedPlatformCodes, ",") != "kuaishou" || len(batch.PlatformErrors) != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"cp.kuaishou.com/article/publish/video",
		"uploadFile(inputSelector",
		"冻结快手标题",
		"冻结快手文案",
		"EGO_KUAISHOU_UPLOAD_TIMEOUT",
		"final_button_missing",
		"final_button_disabled",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Kuaishou prepare program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "submit approved publish") || strings.Contains(program, "await click(selector") {
		t.Fatalf("Kuaishou prepare reached terminal submit: %s", program)
	}

	if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{{
		PlatformCode: "kuaishou", ContentType: "graph_text", Title: "冻结快手标题",
		Body: "冻结快手文案", Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Role: "body"}},
	}}); err == nil || err.Error() != "EGO_PUBLISH_SCHEDULE_REQUIRED" {
		t.Fatalf("Kuaishou immediate submit must remain fail-closed, err=%v", err)
	}
	if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{{
		PlatformCode: "kuaishou", ContentType: "graph_text", Title: "冻结快手标题",
		PublishAt: time.Now().Add(2 * time.Hour).Truncate(time.Minute).Format(time.RFC3339),
		Body:      "冻结快手文案", Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Role: "body"}},
	}}); err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
		t.Fatalf("scheduled Kuaishou submit must remain fail-closed, err=%v", err)
	}
}

func TestYouTubePrepareUsesFrozenVideoAndNeverClicksFinalPublish(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("YouTube 发布浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "youtube", "频道", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(t.TempDir(), "frozen-youtube.mp4")
	videoBytes := []byte("frozen-youtube-video")
	if err := os.WriteFile(videoPath, videoBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	videoDigest := sha256.Sum256(videoBytes)
	coverPath := filepath.Join(t.TempDir(), "frozen-youtube-cover.png")
	coverBytes := []byte("frozen-youtube-cover")
	if err := os.WriteFile(coverPath, coverBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	coverDigest := sha256.Sum256(coverBytes)
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "youtube", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "youtube", ContentType: "video", Title: "冻结YouTube标题",
		Body: "冻结YouTube描述", TagsJSON: `["AlphaNexus","EgoLite"]`,
		SettingsJSON: `{"audience":"not_made_for_kids","visibility":"unlisted","playlist":"AlphaNexus","allow_comments":false,"altered_content":true}`,
		Media: []egolite.PublishMedia{
			{FilePath: videoPath, SHA256: hex.EncodeToString(videoDigest[:]), Kind: "video", Role: "video"},
			{FilePath: coverPath, SHA256: hex.EncodeToString(coverDigest[:]), Kind: "image", Role: "cover"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(batch.PreparedPlatformCodes, ",") != "youtube" || len(batch.PlatformErrors) != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://studio.youtube.com/",
		"Upload videos",
		"uploadFile(videoInputSelector",
		"冻结YouTube标题",
		"冻结YouTube描述",
		"frozen-youtube-cover.png",
		"Upload thumbnail",
		"AlphaNexus",
		"EgoLite",
		"not_made_for_kids",
		"unlisted",
		"allow_comments",
		"altered_content",
		"target.altered_content",
		"playlist",
		"Next",
		"youtube_post_advance_dialog_ambiguous",
		"youtube_final_button_missing_or_disabled",
		"Save",
		"Publish",
		"Schedule",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("YouTube prepare program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "submit approved publish") || strings.Contains(program, "click(finalSelector") || strings.Contains(program, "target.alteredContent") || strings.Contains(program, "EGO_YOUTUBE_SETTINGS_REVIEW_REQUIRED") {
		t.Fatalf("YouTube prepare reached terminal submit: %s", program)
	}

	if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{{
		PlatformCode: "youtube", ContentType: "video", Title: "冻结YouTube标题",
		SettingsJSON: `{"audience":"not_made_for_kids","visibility":"unlisted","playlist":"AlphaNexus","allow_comments":false,"altered_content":true}`,
		Media: []egolite.PublishMedia{
			{FilePath: videoPath, SHA256: hex.EncodeToString(videoDigest[:]), Kind: "video", Role: "video"},
			{FilePath: coverPath, SHA256: hex.EncodeToString(coverDigest[:]), Kind: "image", Role: "cover"},
		},
	}}); err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
		t.Fatalf("YouTube submit must remain fail-closed, err=%v", err)
	}
}

func TestYouTubePrepareRejectsMalformedMediaRoles(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("YouTube 媒体约束")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "youtube", "频道", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen-youtube-media.bin")
	mediaBytes := []byte("frozen-youtube-media")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	media := func(kind, role string) []egolite.PublishMedia {
		return []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: kind, Role: role}}
	}
	base := egolite.PublishPage{
		PlatformCode: "youtube", ContentType: "video", Title: "冻结YouTube标题",
		SettingsJSON: `{"audience":"not_made_for_kids","visibility":"unlisted","playlist":"","allow_comments":true,"altered_content":false}`,
	}
	tests := []struct {
		name  string
		media []egolite.PublishMedia
	}{
		{name: "missing video", media: nil},
		{name: "unknown role", media: media("video", "body")},
		{name: "video role with image kind", media: media("image", "video")},
		{name: "cover role with video kind", media: append(media("video", "video"), media("video", "cover")...)},
		{name: "duplicate video", media: append(media("video", "video"), media("video", "video")...)},
		{name: "multiple covers", media: append(media("video", "video"), append(media("image", "cover"), media("image", "cover")...)...)},
		{name: "cover without video", media: media("image", "cover")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeProgramRunner{}
			service := egolite.NewSpaceService(database, runner)
			page := base
			page.Media = test.media
			if _, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page}); err == nil {
				t.Fatal("malformed YouTube media unexpectedly accepted")
			}
			if len(runner.calls) != 0 {
				t.Fatalf("malformed YouTube media touched EgoLite: calls=%d", len(runner.calls))
			}
		})
	}
}

func TestTikTokPrepareUsesVideoOnlyContractAndNeverUploadsFrozenCover(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("TikTok 发布浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "tiktok", "TikTok", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(t.TempDir(), "frozen-tiktok.mp4")
	videoBytes := []byte("frozen-tiktok-video")
	if err := os.WriteFile(videoPath, videoBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	videoDigest := sha256.Sum256(videoBytes)
	coverPath := filepath.Join(t.TempDir(), "frozen-tiktok-cover.png")
	coverBytes := []byte("frozen-tiktok-cover")
	if err := os.WriteFile(coverPath, coverBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	coverDigest := sha256.Sum256(coverBytes)
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "tiktok", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "tiktok", ContentType: "video", Title: "TikTok 标题",
		Caption: "TikTok caption", TagsJSON: `["AlphaNexus","EgoLite"]`,
		PublishAt:    "2030-01-02T05:03:00Z",
		SettingsJSON: `{"visibility":"friends","allow_comment":false,"allow_duet":true,"allow_stitch":true,"ai_generated":true,"allow_download":false}`,
		Media: []egolite.PublishMedia{
			{FilePath: videoPath, SHA256: hex.EncodeToString(videoDigest[:]), Kind: "video", Role: "video"},
			{FilePath: coverPath, SHA256: hex.EncodeToString(coverDigest[:]), Kind: "image", Role: "cover"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(batch.PreparedPlatformCodes, ",") != "tiktok" || len(batch.PlatformErrors) != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	if !strings.Contains(program, `"prepareMode":"automated"`) {
		t.Fatalf("TikTok video did not carry the automated capability into the runtime: %s", program)
	}
	start := strings.Index(program, `} else if (target.platformCode === "tiktok") {`)
	end := strings.Index(program[start:], `target.platformCode === "csdn"`)
	if start < 0 || end <= 0 {
		t.Fatalf("TikTok prepare branch missing: %s", program)
	}
	tiktokProgram := program[start : start+end]
	for _, required := range []string{
		"Select video", "input[type='file'][accept*='video' i]", "uploadFile(videoInputSelector",
		"fileSelected", "tiktok_video_upload_unverified", "video", "preview",
		"Past edited video was not saved", "Discard", "Cancel", "Got it",
		"Edit cover", "data-alpha-tiktok-cover-edit", "Save", "native_cover_not_committed",
		"caption", "captionReadback", "Hashtags", "话题标签", "typeText(tag)", "pressKey(\"SPACE\")",
		"data-testid", "mention", "visibility", "allow_comment", "allow_duet",
		"allow_stitch", "ai_generated", "allow_download", "Reuse content",
		"tiktok_reuse_content_split_unavailable", "tiktok_allow_download_absence_unreviewed",
		"Post", "tiktok_final_post_missing_or_disabled", "awaiting_review",
		"tiktokScheduleInputSelectors", "tiktok_schedule_time_readback_mismatch",
	} {
		if !strings.Contains(tiktokProgram, required) {
			t.Fatalf("TikTok prepare program missing %q: %s", required, tiktokProgram)
		}
	}
	if strings.Contains(tiktokProgram, "uploadFile(cover") || strings.Contains(tiktokProgram, "uploadFile(coverMedia") || strings.Contains(tiktokProgram, "click(finalSelector") {
		t.Fatalf("TikTok prepare crossed a forbidden boundary: %s", tiktokProgram)
	}
	if !strings.Contains(tiktokProgram, `"video-input", false`) {
		t.Fatalf("TikTok hidden native file input is not supported: %s", tiktokProgram)
	}
	if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{{
		PlatformCode: "tiktok", ContentType: "video", Title: "TikTok 标题", Caption: "TikTok caption",
		PublishAt:    "2030-01-02T05:03:00Z",
		SettingsJSON: `{"visibility":"friends","allow_comment":false,"allow_duet":true,"allow_stitch":true,"ai_generated":true,"allow_download":false}`,
		Media:        []egolite.PublishMedia{{FilePath: videoPath, SHA256: hex.EncodeToString(videoDigest[:]), Kind: "video", Role: "video"}},
	}}); err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
		t.Fatalf("scheduled TikTok submit must remain fail-closed, err=%v", err)
	}
}

func TestTikTokGraphTextManualHandoffOpensOnlyOfficialStudioURL(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("TikTok 图文人工接管浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "tiktok", "TikTok", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen-tiktok-photo.png")
	mediaBytes := []byte("frozen-tiktok-photo")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "tiktok", "status": "failed", "errorCode": "EGO_PUBLISH_AUTOMATION_UNSUPPORTED"}},
	}}
	batch, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "tiktok", ContentType: "graph_text", Title: "TikTok Photo Post 标题", Body: "冻结图文正文",
		Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "image"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.State != "failed" || strings.Join(batch.PlatformCodes, ",") != "tiktok" || len(batch.PreparedPlatformCodes) != 0 || batch.PlatformErrors["tiktok"] != "EGO_PUBLISH_AUTOMATION_UNSUPPORTED" {
		t.Fatalf("batch=%+v", batch)
	}
	if len(runner.calls) == 0 {
		t.Fatal("manual handoff did not reach the EgoLite runtime")
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{"EGO_PUBLISH_AUTOMATION_UNSUPPORTED", "const preparationResults = []"} {
		if !strings.Contains(program, required) {
			t.Fatalf("manual handoff program missing %q: %s", required, program)
		}
	}
	for _, forbidden := range []string{"tiktokstudio/upload", "submit approved publish", "const submitResults = []"} {
		if strings.Contains(program, forbidden) {
			t.Fatalf("TikTok graph_text manual handoff crossed automation boundary %q: %s", forbidden, program)
		}
	}
}

func TestTikTokPrepareRejectsMalformedVideoRolesAndSettings(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("TikTok 媒体约束")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "tiktok", "TikTok", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen-tiktok-media.bin")
	mediaBytes := []byte("frozen-tiktok-media")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	media := func(kind, role string) []egolite.PublishMedia {
		return []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: kind, Role: role}}
	}
	base := egolite.PublishPage{
		PlatformCode: "tiktok", ContentType: "video", Caption: "caption",
		SettingsJSON: `{"visibility":"private","allow_comment":true,"allow_duet":false,"allow_stitch":false,"ai_generated":false,"allow_download":false}`,
	}
	tests := []struct {
		name  string
		media []egolite.PublishMedia
	}{
		{name: "missing video", media: nil},
		{name: "unknown role", media: media("video", "body")},
		{name: "video role with image kind", media: media("image", "video")},
		{name: "cover role with video kind", media: media("video", "cover")},
		{name: "duplicate video", media: append(media("video", "video"), media("video", "video")...)},
		{name: "multiple covers", media: append(media("video", "video"), append(media("image", "cover"), media("image", "cover")...)...)},
		{name: "cover without video", media: media("image", "cover")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeProgramRunner{}
			service := egolite.NewSpaceService(database, runner)
			page := base
			page.Media = test.media
			if _, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page}); err == nil {
				t.Fatal("malformed TikTok media unexpectedly accepted")
			}
			if len(runner.calls) != 0 {
				t.Fatalf("malformed TikTok media touched EgoLite: calls=%d", len(runner.calls))
			}
		})
	}
	for _, settingsJSON := range []string{
		`{"visibility":"unlisted","allow_comment":true,"allow_duet":false,"allow_stitch":false,"ai_generated":false,"allow_download":false}`,
		`{"visibility":"public","allow_comment":true,"allow_duet":false,"allow_stitch":false,"ai_generated":false,"allow_download":false,"unknown":true}`,
	} {
		runner := &fakeProgramRunner{}
		service := egolite.NewSpaceService(database, runner)
		page := base
		page.SettingsJSON = settingsJSON
		page.Media = media("video", "video")
		if _, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page}); err == nil {
			t.Fatalf("invalid TikTok settings unexpectedly accepted: %s", settingsJSON)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("invalid TikTok settings touched EgoLite: calls=%d", len(runner.calls))
		}
	}
}

func TestCSDNPrepareStopsAtReviewedFinalButton(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("CSDN 发布浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "csdn", "CSDN", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "csdn", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "csdn", ContentType: "article", Title: "冻结文章标题",
		Body: "# 冻结正文", TagsJSON: `["AlphaNexus"]`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(batch.PreparedPlatformCodes, ",") != "csdn" {
		t.Fatalf("batch=%+v", batch)
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{"article-bar__title-display", "pre.editor__inner", "button.btn.btn-publish:not(.btn-b-red)", "button.btn-b-red", "添加文章标签"} {
		if !strings.Contains(program, required) {
			t.Fatalf("CSDN prepare program missing %q: %s", required, program)
		}
	}
	for _, required := range []string{"EGO_CSDN_TAG_REQUIRED", "final_button_missing", "final_button_disabled"} {
		if !strings.Contains(program, required) {
			t.Fatalf("CSDN prepare program missing tag-gated final review %q: %s", required, program)
		}
	}
	if strings.Index(program, "final_button_disabled") < strings.Index(program, "tags_readback_failed") {
		t.Fatalf("CSDN final button was checked before required tags were read back: %s", program)
	}
	if strings.Contains(program, "submit approved publish") || strings.Contains(program, "await click(selector") {
		t.Fatalf("CSDN prepare reached terminal submit: %s", program)
	}
}

func TestBrowserOnePublishPrepareStopsAtUserControlledBoundSpaceWithoutDefault(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.GetEgoIdentity(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, platformCode := range []string{"douyin"} {
		account, createErr := database.CreateAccountForIdentity(identity.ID, platformCode, platformCode, "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		if updateErr := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 14, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen-video.mp4")
	mediaBytes := []byte("frozen-video")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	runner := &fakeProgramRunner{
		spaceProbe: map[string]any{
			"id": 14, "taskId": "alpha-nexus/matrix-1", "name": "alpha-nexus/matrix-1", "ownership": "user",
		},
		publishPrepareErr: errors.New("EGO_SPACE_USER_CONTROLLED"),
	}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "douyin", ContentType: "video", Title: "冻结视频标题", Body: "冻结视频正文",
		Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "video", Role: "body"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if batch.SpaceID != 14 || batch.State != "failed" || len(batch.PreparedPlatformCodes) != 0 ||
		batch.PlatformErrors["douyin"] != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("batch=%+v", batch)
	}
	for _, program := range runner.calls {
		if strings.Contains(program, "Default") {
			t.Fatalf("Browser1 prepare referenced Default: %s", program)
		}
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"Number(item.id) === 14", `throw new Error("EGO_SPACE_USER_CONTROLLED")`, "creator.douyin.com",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Browser1 prepare program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "submit approved publish") || strings.Contains(program, "await click(selector") {
		t.Fatalf("Browser1 prepare crossed the final-submit boundary: %s", program)
	}
	if strings.Contains(program, "handOffTaskSpace") {
		t.Fatalf("Browser1 publish prepare must not hand off user Space: %s", program)
	}
	if !strings.Contains(program, `if (existingSpace.ownership === "user")`) ||
		!strings.Contains(program, `throw new Error("EGO_SPACE_USER_CONTROLLED")`) {
		t.Fatalf("Browser1 publish prepare lost its fail-closed user-controlled guard: %s", program)
	}
}

func TestDouyinSubmitVerifiedGatePreventsRuntimeAndFinalClick(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("抖音提交浏览器")
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
	mediaPath := filepath.Join(t.TempDir(), "cover.png")
	mediaBytes := []byte("frozen-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.SubmitPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "douyin", ContentType: "graph_text", Title: "冻结标题", Body: "冻结正文",
		Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "body"}},
	}})
	if err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" || batch != nil {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unverified submit reached runtime: calls=%d", len(runner.calls))
	}
}

func TestDouyinAcceptanceSubmitAllowsOneExplicitUnverifiedTarget(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("抖音验收浏览器")
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
	mediaPath := filepath.Join(t.TempDir(), "cover.png")
	mediaBytes := []byte("frozen-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	runner := &fakeProgramRunner{publishSubmit: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{
			"platformCode": "douyin", "status": "submitted_pending_verification", "clicked": true,
		}},
	}}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.SubmitAcceptancePublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "douyin", ContentType: "graph_text", Title: "冻结标题", Body: "冻结正文",
		Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "body"}},
	}})
	if err != nil || batch == nil || batch.State != "submitted_pending_verification" || !batch.PlatformClicked["douyin"] {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
	if len(runner.calls) == 0 || !strings.Contains(strings.Join(runner.calls, "\n"), "const submitResults = []") {
		t.Fatal("explicit acceptance submit did not reach the reviewed runtime")
	}
}

func TestSubmitAtomicCompletionSuccessUsesOneRuntimeCompletion(t *testing.T) {
	database, identity, page, _ := automatedPublishContractFixture(t, "csdn", "article")
	page.TagsJSON = `["AlphaNexus"]`
	runner := &fakeProgramRunner{
		publishSubmit: map[string]any{
			"done":    true,
			"results": []map[string]any{{"platformCode": "csdn", "status": "submitted_pending_verification", "clicked": true}},
		},
		publishCompletion: map[string]any{"done": true},
	}
	batch, err := egolite.NewSpaceService(database, runner).SubmitAcceptancePublishBatchAndHandoff(
		context.Background(), identity.ID, []egolite.PublishPage{page},
	)
	if err != nil || batch == nil || batch.State != "submitted_pending_verification" || !batch.PlatformClicked["csdn"] {
		t.Fatalf("batch=%+v err=%v, want successful atomic submit", batch, err)
	}
	completionCalls := 0
	for _, program := range runner.calls {
		if strings.Contains(program, "completeTaskSpace(") {
			completionCalls++
		}
	}
	if completionCalls != 1 {
		t.Fatalf("completion runtime calls=%d, want one completion in submit runtime", completionCalls)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.Ownership != "agent" || space.State != "ready" || space.ErrorCode != "" {
		t.Fatalf("space=%+v, want agent/ready after completion success", space)
	}
}

func TestSubmitAtomicCompletionFailureAfterClickedReturnsResultUnknown(t *testing.T) {
	for _, completion := range []string{"false", "throw"} {
		t.Run(completion, func(t *testing.T) {
			database, identity, page, _ := automatedPublishContractFixture(t, "csdn", "article")
			page.TagsJSON = `["AlphaNexus"]`
			runner := &fakeProgramRunner{
				publishSubmit: map[string]any{
					"done":    true,
					"results": []map[string]any{{"platformCode": "csdn", "status": "submitted_pending_verification", "clicked": true}},
				},
				publishCompletion: map[string]any{"done": false},
			}
			batch, err := egolite.NewSpaceService(database, runner).SubmitAcceptancePublishBatchAndHandoff(
				context.Background(), identity.ID, []egolite.PublishPage{page},
			)
			if err != nil || batch == nil || batch.State != "result_unknown" || batch.PlatformStatuses["csdn"] != "result_unknown" ||
				batch.PlatformErrors["csdn"] != "EGO_SUBMIT_RESULT_UNKNOWN" || !batch.PlatformClicked["csdn"] {
				t.Fatalf("batch=%+v err=%v, want non-retryable result_unknown with click evidence", batch, err)
			}
			space, err := database.GetEgoSpaceByIdentity(identity.ID)
			if err != nil {
				t.Fatal(err)
			}
			if space.Ownership != "agentDelegatedToUser" || space.State != "result_unknown" || space.ErrorCode != "EGO_PUBLISH_SPACE_RELEASE_FAILED" {
				t.Fatalf("space=%+v, want persisted delegated/result_unknown release failure", space)
			}
		})
	}
}

func TestSubmitAtomicCompletionFailureWithInvalidResultReturnsResultUnknown(t *testing.T) {
	database, identity, page, _ := automatedPublishContractFixture(t, "csdn", "article")
	page.TagsJSON = `["AlphaNexus"]`
	runner := &fakeProgramRunner{
		publishSubmit: map[string]any{
			"done":    true,
			"results": []map[string]any{},
		},
		publishCompletion: map[string]any{"done": false},
	}
	batch, err := egolite.NewSpaceService(database, runner).SubmitAcceptancePublishBatchAndHandoff(
		context.Background(), identity.ID, []egolite.PublishPage{page},
	)
	if err != nil || batch == nil || batch.State != "result_unknown" || batch.PlatformStatuses["csdn"] != "result_unknown" ||
		batch.PlatformErrors["csdn"] != "EGO_SUBMIT_RESULT_UNKNOWN" {
		t.Fatalf("batch=%+v err=%v, want invalid result to fail closed as result_unknown", batch, err)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.Ownership != "agentDelegatedToUser" || space.State != "result_unknown" || space.ErrorCode != "EGO_PUBLISH_SPACE_RELEASE_FAILED" {
		t.Fatalf("space=%+v, want persisted delegated/result_unknown release failure", space)
	}
}

func TestSubmitAtomicCompletionFailureWithProvenUnclickedReturnsReleaseError(t *testing.T) {
	database, identity, page, _ := automatedPublishContractFixture(t, "csdn", "article")
	page.TagsJSON = `["AlphaNexus"]`
	runner := &fakeProgramRunner{
		publishSubmit: map[string]any{
			"done":    true,
			"results": []map[string]any{{"platformCode": "csdn", "status": "failed", "clicked": false}},
		},
		publishCompletion: map[string]any{"done": false},
	}
	batch, err := egolite.NewSpaceService(database, runner).SubmitAcceptancePublishBatchAndHandoff(
		context.Background(), identity.ID, []egolite.PublishPage{page},
	)
	if batch != nil || err == nil || err.Error() != "EGO_PUBLISH_SPACE_RELEASE_FAILED" {
		t.Fatalf("batch=%+v err=%v, want stable release error for proven unclicked target", batch, err)
	}
	var jobs, records int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 || records != 0 {
		t.Fatalf("completion failure created publish state: jobs=%d records=%d", jobs, records)
	}
}

func TestAcceptanceSubmitRejectsTargetsWithoutSubmitAndVerifyAdapters(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("小红书验收浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "xhs", "小红书", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "cover.png")
	mediaBytes := []byte("frozen-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.SubmitAcceptancePublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "xhs", ContentType: "graph_text", Title: "冻结标题", Body: "冻结正文",
		Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Role: "body"}},
	}})
	if err == nil || err.Error() != "EGO_PUBLISH_ACCEPTANCE_UNAVAILABLE" || batch != nil {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unsupported acceptance target reached runtime: calls=%d", len(runner.calls))
	}
}

func TestSubmitPreflightRejectsTargetsThatCouldReuseAnUnrelatedPreparedForm(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("发布预检浏览器")
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"douyin", "csdn"} {
		account, createErr := database.CreateAccountForIdentity(identity.ID, platform, platform, "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		if updateErr := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen.png")
	mediaBytes := []byte("frozen-media")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	media := egolite.PublishMedia{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "body"}
	tooManyMedia := make([]egolite.PublishMedia, 31)
	for index := range tooManyMedia {
		tooManyMedia[index] = media
	}
	service := egolite.NewSpaceService(database, nil)

	tests := []struct {
		name string
		page egolite.PublishPage
		want string
	}{
		{name: "douyin without media", page: egolite.PublishPage{PlatformCode: "douyin", ContentType: "video", Title: "冻结标题", Body: "冻结正文"}, want: "EGO_PUBLISH_MEDIA_REQUIRED"},
		{name: "douyin with invalid image count", page: egolite.PublishPage{PlatformCode: "douyin", ContentType: "graph_text", Title: "冻结标题", Body: "冻结正文", Media: tooManyMedia}, want: "EGO_DOUYIN_MEDIA_INVALID"},
		{name: "csdn without tags", page: egolite.PublishPage{PlatformCode: "csdn", ContentType: "article", Title: "冻结标题", Body: "冻结正文"}, want: "EGO_CSDN_TAG_REQUIRED"},
		{name: "csdn with unreviewed cover", page: egolite.PublishPage{PlatformCode: "csdn", ContentType: "article", Title: "冻结标题", Body: "冻结正文", TagsJSON: `["EgoLite"]`, Media: []egolite.PublishMedia{media}}, want: "EGO_CSDN_COVER_REQUIRES_REVIEW"},
		{name: "csdn without body", page: egolite.PublishPage{PlatformCode: "csdn", ContentType: "article", Title: "冻结标题", TagsJSON: `["EgoLite"]`}, want: "EGO_PUBLISH_CONTENT_INVALID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{test.page})
			if err == nil || err.Error() != test.want {
				t.Fatalf("error=%v, want %s", err, test.want)
			}
		})
	}
}

func TestCSDNSubmitVerifiedGatePreventsRuntimeAndFinalClick(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("CSDN 提交浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "csdn", "CSDN", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.SubmitPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{{
		PlatformCode: "csdn", ContentType: "article", Title: "冻结文章标题",
		Body: "# 冻结正文", TagsJSON: `["AlphaNexus","EgoLite"]`,
	}})
	if err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" || batch != nil {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unverified submit reached runtime: calls=%d", len(runner.calls))
	}
}

func TestSubmitRejectsMultiTargetBatchBeforeRuntime(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("异常提交结果浏览器")
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"douyin", "csdn"} {
		account, createErr := database.CreateAccountForIdentity(identity.ID, platform, platform, "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		if updateErr := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	mediaPath := filepath.Join(t.TempDir(), "cover.png")
	mediaBytes := []byte("frozen-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)
	batch, err := service.SubmitPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{
		{
			PlatformCode: "douyin", ContentType: "graph_text", Title: "冻结标题", Body: "冻结正文",
			Media: []egolite.PublishMedia{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "body"}},
		},
		{PlatformCode: "csdn", ContentType: "article", Title: "冻结文章标题", Body: "# 冻结正文", TagsJSON: `["EgoLite"]`},
	})
	if err == nil || err.Error() != "EGO_PUBLISH_SINGLE_TARGET_REQUIRED" || batch != nil {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unverified multi-target submit reached runtime: calls=%d", len(runner.calls))
	}
}

func TestOpenLoggedInPlatformsReusesExistingSpaceAndReturnsNextBatch(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("已登录平台浏览器")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"youtube", "csdn", "douyin", "xhs", "facebook"} {
		account, createErr := database.CreateAccountForIdentity(identity.ID, code, code, "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		state := "logged_in"
		if code == "facebook" {
			state = "logged_out"
		}
		if _, updateErr := database.Exec(`UPDATE accounts SET login_state=? WHERE id=?`, state, account.ID); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 77, "task-77", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{spaceProbe: map[string]any{
		"id": 77, "taskId": "task-77", "name": "alpha-nexus/matrix-1", "ownership": "user",
	}}
	service := egolite.NewSpaceService(database, runner)

	result, err := service.OpenLoggedInPlatforms(context.Background(), identity.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reused || result.SpaceID != 77 || result.Total != 4 || result.Remaining != 1 || result.NextOffset != 3 {
		t.Fatalf("result=%+v", result)
	}
	if strings.Join(result.PlatformCodes, ",") != "douyin,xhs,youtube" {
		t.Fatalf("platforms=%v", result.PlatformCodes)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("existing Space must be validated then reused without creating another, calls=%d", len(runner.calls))
	}
	program := runner.calls[len(runner.calls)-1]
	if !strings.Contains(program, "takeOverTaskSpace(77)") || strings.Count(program, "openOrReuseTab(") != 3 ||
		!strings.Contains(program, "handOffTaskSpace(77)") {
		t.Fatalf("program=%s", program)
	}
	if strings.Contains(program, "for (const tab of await listTabs()) await closeTab") ||
		!strings.Contains(program, "while (staleTabs.length > 1)") ||
		!strings.Contains(program, "afterFirstTabs.length > 1") {
		t.Fatalf("program can destroy the Space by closing its last tab: %s", program)
	}
	if strings.Contains(program, "facebook.com") || strings.Contains(program, "useOrCreateTaskSpace(\"alpha-nexus") {
		t.Fatalf("program opened an unavailable platform or created a second Space: %s", program)
	}

	next, err := service.OpenLoggedInPlatforms(context.Background(), identity.ID, result.NextOffset)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(next.PlatformCodes, ",") != "csdn" || next.Remaining != 0 || next.NextOffset != 0 {
		t.Fatalf("next=%+v", next)
	}
}

func TestOpenLoggedInPlatformsBatchesEcommerceConsolesWithoutPublishing(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("电商浏览器")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"amazon_seller", "shopify", "taobao", "tiktok_shop"} {
		account, createErr := database.CreateAccountForIdentity(identity.ID, code, code, "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		if updateErr := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 88, "task-88", "alpha-nexus/matrix-commerce"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{spaceProbe: map[string]any{
		"id": 88, "taskId": "task-88", "name": "alpha-nexus/matrix-commerce", "ownership": "user",
	}}
	service := egolite.NewSpaceService(database, runner)
	result, err := service.OpenLoggedInPlatforms(context.Background(), identity.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.PlatformCodes, ",") != "taobao,tiktok_shop,shopify" || result.NextOffset != 3 || result.Remaining != 1 {
		t.Fatalf("result=%+v", result)
	}
	program := runner.calls[len(runner.calls)-1]
	if strings.Count(program, "openOrReuseTab(") != 3 || !strings.Contains(program, "https://admin.shopify.com/") ||
		strings.Contains(program, "publish") || !strings.Contains(program, "handOffTaskSpace(88)") {
		t.Fatalf("e-commerce browser batch is unsafe: %s", program)
	}
}

func TestOpenLoggedInPlatformsKeepsMediaBeforeShopPlatformsInBoundSpace(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("媒体与店铺浏览器")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"acfun", "baijiahao", "bilibili", "jianshu", "juejin", "omtencent", "pdd", "sina", "toutiao", "weishi", "x", "zhihu",
		"douyin_shop", "taobao", "tiktok_shop", "shopee", "shopify", "amazon_seller",
		"qianchuan", "jd_shop", "pinduoduo_shop", "xhs_shop", "kuaishou_shop", "wechat_shop", "alibaba_1688", "aliexpress", "xianyu",
	}
	for index := len(want) - 1; index >= 0; index-- {
		code := want[index]
		account, createErr := database.CreateAccountForIdentity(identity.ID, code, code, "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		if updateErr := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	const spaceID int64 = 190
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, spaceID, "task-190", "alpha-nexus/matrix-media-shop"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{spaceProbe: map[string]any{
		"id": spaceID, "taskId": "task-190", "name": "alpha-nexus/matrix-media-shop", "ownership": "agent",
	}}
	service := egolite.NewSpaceService(database, runner)

	var got []string
	for offset := 0; ; {
		result, openErr := service.OpenLoggedInPlatforms(context.Background(), identity.ID, offset)
		if openErr != nil {
			t.Fatal(openErr)
		}
		if !result.Reused || result.SpaceID != spaceID {
			t.Fatalf("result=%+v, want the bound Space", result)
		}
		program := runner.calls[len(runner.calls)-1]
		for _, code := range result.PlatformCodes {
			platform, findErr := platforms.Find(code)
			if findErr != nil {
				t.Fatal(findErr)
			}
			if !strings.Contains(program, platform.ManagerURL) {
				t.Fatalf("platform=%s opened the wrong manager URL: %s", code, program)
			}
		}
		if strings.Contains(strings.ToLower(program), "click") || strings.Contains(strings.ToLower(program), "submit") {
			t.Fatalf("logged-in platform opening must not click or submit: %s", program)
		}
		got = append(got, result.PlatformCodes...)
		if result.Remaining == 0 {
			break
		}
		offset = result.NextOffset
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("platform order=%v, want %v", got, want)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.SpaceID != spaceID {
		t.Fatalf("bound Space changed: %+v", space)
	}
}

func TestManagedPlatformLoginQRUsesOfficialOriginsAndShopEvidence(t *testing.T) {
	tests := []struct {
		code     string
		origin   string
		suffixes []string
		strict   bool
	}{
		{code: "acfun", origin: "https://member.acfun.cn/", suffixes: []string{"acfun.cn"}},
		{code: "baijiahao", origin: "https://baijiahao.baidu.com/", suffixes: []string{"baidu.com"}},
		{code: "bilibili", origin: "https://member.bilibili.com/", suffixes: []string{"bilibili.com"}},
		{code: "jianshu", origin: "https://www.jianshu.com/", suffixes: []string{"jianshu.com"}},
		{code: "juejin", origin: "https://juejin.cn/creator", suffixes: []string{"juejin.cn"}},
		{code: "omtencent", origin: "https://om.qq.com/", suffixes: []string{"qq.com"}},
		{code: "pdd", origin: "https://mms.pinduoduo.com/", suffixes: []string{"pinduoduo.com"}},
		{code: "sina", origin: "https://weibo.com/", suffixes: []string{"weibo.com"}},
		{code: "toutiao", origin: "https://mp.toutiao.com/", suffixes: []string{"toutiao.com"}},
		{code: "weishi", origin: "https://weishi.qq.com/", suffixes: []string{"qq.com"}},
		{code: "x", origin: "https://x.com/", suffixes: []string{"x.com"}},
		{code: "zhihu", origin: "https://www.zhihu.com/creator", suffixes: []string{"zhihu.com"}},
		{code: "douyin_shop", origin: "https://fxg.jinritemai.com/", suffixes: []string{"jinritemai.com"}, strict: true},
		{code: "taobao", origin: "https://myseller.taobao.com/home.htm", suffixes: []string{"taobao.com"}, strict: true},
		{code: "tiktok_shop", origin: "https://seller.tiktokglobalshop.com/", suffixes: []string{"tiktokglobalshop.com"}, strict: true},
		{code: "shopee", origin: "https://seller.shopee.cn/", suffixes: []string{"shopee.cn", "shopee.com"}, strict: true},
		{code: "shopify", origin: "https://admin.shopify.com/", suffixes: []string{"shopify.com", "myshopify.com"}, strict: true},
		{code: "amazon_seller", origin: "https://sellercentral.amazon.com/", suffixes: []string{"amazon.com"}, strict: true},
		{code: "qianchuan", origin: "https://qianchuan.jinritemai.com/", suffixes: []string{"jinritemai.com"}, strict: true},
		{code: "jd_shop", origin: "https://seller-v10.shop.jd.com/", suffixes: []string{"shop.jd.com", "jd.com"}, strict: true},
		{code: "pinduoduo_shop", origin: "https://mms.pinduoduo.com/", suffixes: []string{"pinduoduo.com"}, strict: true},
		{code: "xhs_shop", origin: "https://ark.xiaohongshu.com/ark", suffixes: []string{"xiaohongshu.com"}, strict: true},
		{code: "kuaishou_shop", origin: "https://s.kwaixiaodian.com/", suffixes: []string{"kwaixiaodian.com"}, strict: true},
		{code: "wechat_shop", origin: "https://channels.weixin.qq.com/shop", suffixes: []string{"weixin.qq.com", "qq.com"}, strict: true},
		{code: "alibaba_1688", origin: "https://work.1688.com/", suffixes: []string{"1688.com"}, strict: true},
		{code: "aliexpress", origin: "https://gsp.aliexpress.com/", suffixes: []string{"aliexpress.com"}, strict: true},
		{code: "xianyu", origin: "https://www.goofish.com/personal", suffixes: []string{"goofish.com", "taobao.com"}, strict: true},
	}
	for index, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity("平台登录证据")
			if err != nil {
				t.Fatal(err)
			}
			account, err := database.CreateAccountForIdentity(identity.ID, test.code, test.code, "")
			if err != nil {
				t.Fatal(err)
			}
			spaceID := int64(300 + index)
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, spaceID, "task-"+jsonNumber(spaceID), "alpha-nexus/matrix-login-evidence"); err != nil {
				t.Fatal(err)
			}
			runner := &fakeProgramRunner{
				spaceProbe: map[string]any{
					"id": spaceID, "taskId": "task-" + jsonNumber(spaceID), "name": "alpha-nexus/matrix-login-evidence", "ownership": "agent",
				},
				loginQR: map[string]any{"state": "manual_takeover", "pageOrigin": test.origin},
			}

			result, err := egolite.NewSpaceService(database, runner).OpenLoginQR(context.Background(), account.ID, false)
			if err != nil {
				t.Fatal(err)
			}
			if result.State != "manual_takeover" || result.PageOrigin != test.origin {
				t.Fatalf("result=%+v", result)
			}
			program := runner.calls[len(runner.calls)-1]
			suffixesJSON, _ := json.Marshal(test.suffixes)
			for _, required := range []string{
				"const strictLoginEvidence = " + map[bool]string{true: "true", false: "false"}[test.strict],
				"const allowedSuffixes = " + string(suffixesJSON),
				"if (strictLoginEvidence && !loginRule?.host)",
			} {
				if !strings.Contains(program, required) {
					t.Fatalf("platform=%s login program missing %q: %s", test.code, required, program)
				}
			}
		})
	}
}

func TestAlibaba1688PlatformOriginAllowsOfficialLoginRedirectOnly(t *testing.T) {
	tests := []struct {
		name           string
		pageOrigin     string
		wantPageOrigin string
		wantError      bool
	}{
		{name: "official_login_redirect", pageOrigin: "https://login.taobao.com/jump?target=https%3A%2F%2Fwork.1688.com%2F", wantPageOrigin: "https://login.taobao.com/jump"},
		{name: "evil_taobao_subdomain", pageOrigin: "https://evil.taobao.com/", wantError: true},
		{name: "ordinary_taobao_subdomain", pageOrigin: "https://www.taobao.com/", wantError: true},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity("1688登录跳转边界")
			if err != nil {
				t.Fatal(err)
			}
			account, err := database.CreateAccountForIdentity(identity.ID, "alibaba_1688", "1688", "")
			if err != nil {
				t.Fatal(err)
			}
			spaceID := int64(900 + index)
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, spaceID, "task-"+jsonNumber(spaceID), "alpha-nexus/matrix-1688-origin"); err != nil {
				t.Fatal(err)
			}
			runner := &fakeProgramRunner{
				spaceProbe: map[string]any{
					"id": spaceID, "taskId": "task-" + jsonNumber(spaceID), "name": "alpha-nexus/matrix-1688-origin", "ownership": "agent",
				},
				loginQR: map[string]any{"state": "manual_takeover", "pageOrigin": test.pageOrigin},
			}

			result, err := egolite.NewSpaceService(database, runner).OpenLoginQR(context.Background(), account.ID, false)
			if test.wantError {
				if result != nil || err == nil || err.Error() != "EGO_LOGIN_QR_PLATFORM_MISMATCH" {
					t.Fatalf("result=%+v err=%v, want EGO_LOGIN_QR_PLATFORM_MISMATCH", result, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || result.PageOrigin != test.wantPageOrigin {
				t.Fatalf("result=%+v, want accepted official login redirect", result)
			}
			program := runner.calls[len(runner.calls)-1]
			if !strings.Contains(program, `const allowedHosts = ["login.taobao.com"]`) ||
				!strings.Contains(program, `const allowedSuffixes = ["1688.com"]`) {
				t.Fatalf("1688 login QR did not use exact Taobao login host plus 1688 suffix: %s", program)
			}
		})
	}
}

func TestNewManagedPlatformsFailClosedForUserOwnedBoundSpace(t *testing.T) {
	tests := []struct {
		code string
		host string
	}{
		{code: "acfun", host: "acfun.cn"},
		{code: "baijiahao", host: "baidu.com"},
		{code: "bilibili", host: "bilibili.com"},
		{code: "jianshu", host: "jianshu.com"},
		{code: "juejin", host: "juejin.cn"},
		{code: "omtencent", host: "qq.com"},
		{code: "pdd", host: "pinduoduo.com"},
		{code: "sina", host: "weibo.com"},
		{code: "toutiao", host: "toutiao.com"},
		{code: "weishi", host: "qq.com"},
		{code: "x", host: "x.com"},
		{code: "zhihu", host: "zhihu.com"},
		{code: "qianchuan", host: "jinritemai.com"},
		{code: "jd_shop", host: "shop.jd.com"},
		{code: "pinduoduo_shop", host: "pinduoduo.com"},
		{code: "xhs_shop", host: "xiaohongshu.com"},
		{code: "kuaishou_shop", host: "kwaixiaodian.com"},
		{code: "wechat_shop", host: "weixin.qq.com"},
		{code: "alibaba_1688", host: "1688.com"},
		{code: "aliexpress", host: "aliexpress.com"},
		{code: "xianyu", host: "goofish.com"},
	}
	for index, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity("人工接管边界")
			if err != nil {
				t.Fatal(err)
			}
			account, err := database.CreateAccountForIdentity(identity.ID, test.code, test.code, "")
			if err != nil {
				t.Fatal(err)
			}
			spaceID := int64(600 + index)
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, spaceID, "task-"+jsonNumber(spaceID), "alpha-nexus/matrix-manual"); err != nil {
				t.Fatal(err)
			}
			if _, err := database.UpdateEgoSpaceStateForIdentity(identity.ID, "user", "ready", ""); err != nil {
				t.Fatal(err)
			}
			runner := &fakeProgramRunner{managerErr: errors.New("EGO_SPACE_USER_CONTROLLED")}

			space, err := egolite.NewSpaceService(database, runner).OpenManagerAndHandoff(context.Background(), account.ID)
			if space != nil || err == nil || err.Error() != "EGO_SPACE_USER_CONTROLLED" {
				t.Fatalf("platform=%s space=%+v err=%v, want fail-closed ownership error", test.code, space, err)
			}
			if len(runner.calls) != 1 {
				t.Fatalf("platform=%s calls=%d, want one guarded program", test.code, len(runner.calls))
			}
			program := runner.calls[0]
			if !strings.Contains(program, "Number(item.id) === "+jsonNumber(spaceID)) || !strings.Contains(program, test.host) {
				t.Fatalf("platform=%s did not target the bound official manager: %s", test.code, program)
			}
			forbidden := []string{"takeOverTaskSpace", "click", "submit"}
			for _, token := range forbidden {
				if strings.Contains(strings.ToLower(program), strings.ToLower(token)) {
					t.Fatalf("platform=%s user-owned program contains forbidden %q: %s", test.code, token, program)
				}
			}
			assertManagerUserControlledProgramHasNoSideEffects(t, program, spaceID)
			updated, err := database.GetEgoSpaceByIdentity(identity.ID)
			if err != nil {
				t.Fatal(err)
			}
			if updated.SpaceID != spaceID || updated.Ownership != "agentDelegatedToUser" || updated.State != "manual_takeover" || updated.ErrorCode != "EGO_SPACE_USER_CONTROLLED" {
				t.Fatalf("platform=%s space=%+v, want unchanged binding with persisted safety stop", test.code, updated)
			}
		})
	}
}

func TestOpenLoggedInPlatformsForCodesFiltersConfiguredIdentityPlatforms(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("模式过滤浏览器")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"douyin", "taobao", "shopify"} {
		account, createErr := database.CreateAccountForIdentity(identity.ID, code, code, "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		if updateErr := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 89, "task-89", "alpha-nexus/matrix-mode"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{spaceProbe: map[string]any{
		"id": 89, "taskId": "task-89", "name": "alpha-nexus/matrix-mode", "ownership": "user",
	}}
	service := egolite.NewSpaceService(database, runner)
	result, err := service.OpenLoggedInPlatformsForCodes(context.Background(), identity.ID, 0, []string{"taobao", "shopify"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.PlatformCodes, ",") != "taobao,shopify" {
		t.Fatalf("platforms=%v", result.PlatformCodes)
	}
	program := runner.calls[len(runner.calls)-1]
	if strings.Contains(program, "creator.douyin.com") || strings.Count(program, "openOrReuseTab(") != 2 {
		t.Fatalf("media platform leaked into shop request: %s", program)
	}
	if _, err := service.OpenLoggedInPlatformsForCodes(context.Background(), identity.ID, 0, []string{"unknown_shop"}); err == nil || !strings.Contains(err.Error(), "EGO_PLATFORM_NOT_CONFIGURED") {
		t.Fatalf("unknown requested platform must fail closed, err=%v", err)
	}
}

func TestVerifiedMetaBusinessRouteIsReusedForBrowserOpen(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("Meta 浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "meta", "Meta", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 77, "task-77", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{
		spaceProbe: map[string]any{"id": 77, "taskId": "task-77", "name": "alpha-nexus/matrix-1", "ownership": "user"},
		verifyProfiles: []map[string]any{{
			"platformCode": "meta", "authState": "logged_in", "name": "AlphaIntelligence",
			"identifier": "1241410172381837",
			"pageOrigin": "https://business.facebook.com/latest/home?nav_ref=bm_home_redirect&business_id=1708507717147261&asset_id=1241410172381837&utm_source=ignored",
		}},
	}
	service := egolite.NewSpaceService(database, runner)
	if _, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"meta"}); err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://business.facebook.com/latest/home?asset_id=1241410172381837&business_id=1708507717147261"
	if updated.ManagerAddr != want {
		t.Fatalf("manager address=%q, want %q", updated.ManagerAddr, want)
	}
	if _, err := service.OpenLoggedInPlatforms(context.Background(), identity.ID, 0); err != nil {
		t.Fatal(err)
	}
	program := runner.calls[len(runner.calls)-1]
	if !strings.Contains(program, "https://business.facebook.com/latest/home?asset_id=1241410172381837") ||
		!strings.Contains(program, "business_id=1708507717147261") || strings.Contains(program, "utm_source") || strings.Contains(program, "nav_ref") {
		t.Fatalf("Meta open did not reuse the allow-listed Business route: %s", program)
	}
	if _, err := service.OpenLoginBatch(context.Background(), identity.ID, []string{"meta"}); err != nil {
		t.Fatal(err)
	}
	program = runner.calls[len(runner.calls)-1]
	if !strings.Contains(program, "https://business.facebook.com/latest/home?asset_id=1241410172381837") ||
		!strings.Contains(program, "business_id=1708507717147261") || strings.Contains(program, "utm_source") || strings.Contains(program, "nav_ref") {
		t.Fatalf("Meta login did not reuse the allow-listed Business route: %s", program)
	}
}

func TestMetaFacebookPrepareUsesFormalPageAssetAndNeverClicksFinalPublish(t *testing.T) {
	database, identity, meta, pageAsset, _ := newMetaPublishFixture(t, "")
	media := writeMetaPublishMedia(t)
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "facebook", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)
	page := egolite.PublishPage{
		PlatformCode: "facebook", ContentType: "graph_text",
		AssetExternalID: pageAsset.ExternalID, AssetName: pageAsset.Name,
		Title: "Meta 页面标题", Body: "Meta 页面正文", Media: []egolite.PublishMedia{media},
	}
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
	if err != nil {
		t.Fatal(err)
	}
	if batch.State != "awaiting_review" || len(batch.PreparedPlatformCodes) != 1 || batch.PreparedPlatformCodes[0] != "facebook" ||
		len(batch.AccountIDs) != 1 || batch.AccountIDs[0] != meta.ID || len(batch.PlatformErrors) != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://business.facebook.com/latest/composer/",
		"asset_id=1241410172381837",
		"metaPageID",
		"Alpha Page",
		"meta_caption_readback",
		"meta_final_button_missing_or_disabled",
		"awaiting_review",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Facebook prepare program missing %q: %s", required, program)
		}
	}
	for _, forbidden := range []string{"submit approved publish", "click(metaFinalSelector", "click(metaFinalButtonSelector)"} {
		if strings.Contains(program, forbidden) {
			t.Fatalf("Facebook prepare crossed final publish boundary %q: %s", forbidden, program)
		}
	}
	if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{page}); err == nil || err.Error() != "EGO_PUBLISH_SCHEDULE_REQUIRED" {
		t.Fatalf("Facebook submit must require a native schedule, err=%v", err)
	}
}

func TestFacebookScheduledPublishUsesNativeMetaPageAndSubmitValidation(t *testing.T) {
	database, identity, meta, pageAsset, _ := newMetaPublishFixture(t, "")
	publishAt := time.Now().Add(2 * time.Hour).Truncate(time.Minute).Format(time.RFC3339)
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "facebook", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)
	page := egolite.PublishPage{
		PlatformCode: "facebook", ContentType: "graph_text", AssetExternalID: pageAsset.ExternalID,
		AssetName: pageAsset.Name, PublishAt: publishAt, Caption: "冻结 Facebook 文案", Body: "冻结 Facebook 文案",
		Media: []egolite.PublishMedia{writeMetaPublishMedia(t)},
	}
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
	if err != nil {
		t.Fatal(err)
	}
	if batch.State != "awaiting_review" || len(batch.PreparedPlatformCodes) != 1 || batch.PreparedPlatformCodes[0] != "facebook" ||
		len(batch.AccountIDs) != 1 || batch.AccountIDs[0] != meta.ID || len(batch.PlatformErrors) != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"Scheduling options", "Set date and time", "Asia/Shanghai", publishAt,
		"asset_id=1241410172381837", "metaPageID", "Alpha Page", "meta_caption_readback",
		"meta_schedule_readback_mismatch", "awaiting_review",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("Facebook scheduled prepare missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "submit approved publish") || strings.Contains(program, "click(metaFinalSelector") {
		t.Fatalf("Facebook scheduled prepare crossed final publish boundary: %s", program)
	}
	if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{page}); err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
		t.Fatalf("Facebook scheduled submit must remain fail-closed, err=%v", err)
	}
}

func TestFacebookScheduledPublishAtRequiresMinuteAndTwentyToTwentyNineDayWindow(t *testing.T) {
	now := time.Now()
	valid := now.Add(2 * time.Hour).Truncate(time.Minute).Format(time.RFC3339)
	lowerBound := now.Truncate(time.Minute).Add(20 * time.Minute).Format(time.RFC3339)
	upperBound := now.Truncate(time.Minute).Add(29 * 24 * time.Hour).Format(time.RFC3339)
	tests := []struct {
		name      string
		platform  string
		content   string
		publishAt string
		want      string
	}{
		{name: "valid", platform: "facebook", content: "graph_text", publishAt: valid},
		{name: "exact lower bound", platform: "facebook", content: "graph_text", publishAt: lowerBound},
		{name: "exact upper bound", platform: "facebook", content: "graph_text", publishAt: upperBound},
		{name: "seconds", platform: "facebook", content: "graph_text", publishAt: "2030-01-02T13:00:01+08:00", want: "EGO_PUBLISH_SCHEDULE_INVALID"},
		{name: "too soon", platform: "facebook", content: "graph_text", publishAt: now.Add(19 * time.Minute).Truncate(time.Minute).Format(time.RFC3339), want: "EGO_PUBLISH_SCHEDULE_INVALID"},
		{name: "too far", platform: "facebook", content: "graph_text", publishAt: now.Add(30 * 24 * time.Hour).Truncate(time.Minute).Format(time.RFC3339), want: "EGO_PUBLISH_SCHEDULE_INVALID"},
		{name: "Facebook video", platform: "facebook", content: "video", publishAt: valid},
		{name: "instagram remains rejected", platform: "instagram", content: "graph_text", publishAt: valid, want: "EGO_PUBLISH_SCHEDULE_UNVERIFIED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := egolite.ValidateScheduledPublishAt(test.platform, test.content, test.publishAt)
			if test.want == "" {
				if err != nil {
					t.Fatalf("ValidateScheduledPublishAt()=%v, want valid", err)
				}
				return
			}
			if err == nil || err.Error() != test.want {
				t.Fatalf("ValidateScheduledPublishAt()=%v, want %s", err, test.want)
			}
		})
	}
}

func TestMetaPrepareAndSubmitRejectMixedMediaBeforeOpeningEgoLite(t *testing.T) {
	database, identity, _, pageAsset, instagramAsset := newMetaPublishFixture(t, "17841416733781085")
	if err := database.UpdateAccountAssetConnection(instagramAsset.ID, pageAsset.ID, "facebook", "facebook_page", pageAsset.Name, pageAsset.ExternalID); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountAssetConnection(pageAsset.ID, instagramAsset.ID, "instagram", "instagram_account", instagramAsset.Name, instagramAsset.ExternalID); err != nil {
		t.Fatal(err)
	}
	image := writeMetaPublishMedia(t)
	video := image
	video.Kind, video.Role = "video", "video"
	video.FilePath = filepath.Join(t.TempDir(), "frozen-meta-video.mp4")
	videoPayload := []byte("frozen-meta-video")
	if err := os.WriteFile(video.FilePath, videoPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	videoDigest := sha256.Sum256(videoPayload)
	video.SHA256 = hex.EncodeToString(videoDigest[:])

	tests := []struct {
		name string
		page egolite.PublishPage
	}{
		{
			name: "Facebook graph text rejects video", page: egolite.PublishPage{
				PlatformCode: "facebook", ContentType: "graph_text", AssetExternalID: pageAsset.ExternalID, AssetName: pageAsset.Name,
				Title: "Meta 图文", Body: "Meta 图文正文", PublishAt: time.Now().Add(2 * time.Hour).Truncate(time.Minute).Format(time.RFC3339), Media: []egolite.PublishMedia{image, video},
			},
		},
		{
			name: "Facebook video rejects image before video", page: egolite.PublishPage{
				PlatformCode: "facebook", ContentType: "video", AssetExternalID: pageAsset.ExternalID, AssetName: pageAsset.Name,
				Title: "Meta 视频", Body: "Meta 视频正文", PublishAt: time.Now().Add(2 * time.Hour).Truncate(time.Minute).Format(time.RFC3339), Media: []egolite.PublishMedia{image, video},
			},
		},
		{
			name: "Instagram video rejects non cover image", page: egolite.PublishPage{
				PlatformCode: "instagram", ContentType: "video", AssetExternalID: instagramAsset.ExternalID, AssetName: instagramAsset.Name,
				Title: "IG 视频", Body: "IG 视频正文", Media: []egolite.PublishMedia{video, image},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeProgramRunner{}
			service := egolite.NewSpaceService(database, runner)
			if _, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{test.page}); err == nil || err.Error() != "EGO_META_MEDIA_INVALID" {
				t.Fatalf("prepare err=%v, want EGO_META_MEDIA_INVALID", err)
			}
			if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{test.page}); err == nil || err.Error() != "EGO_META_MEDIA_INVALID" {
				t.Fatalf("submit validation err=%v, want EGO_META_MEDIA_INVALID", err)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("invalid Meta media opened EgoLite: calls=%d", len(runner.calls))
			}
		})
	}
}

func TestMetaInstagramPrepareRequiresVerifiedBidirectionalSameAccountPair(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, database *db.DB, page, instagram *db.AccountAsset)
		instagramID string
		want        string
	}{
		{name: "missing Instagram public id", instagramID: "", want: "EGO_META_INSTAGRAM_ASSET_ID_REQUIRED"},
		{name: "missing connection", instagramID: "17841416733781085", want: "EGO_META_INSTAGRAM_CONNECTION_REQUIRED"},
		{name: "single direction connection", instagramID: "17841416733781085", setup: func(t *testing.T, database *db.DB, page, instagram *db.AccountAsset) {
			if err := database.UpdateAccountAssetConnection(instagram.ID, page.ID, "facebook", "facebook_page", page.Name, page.ExternalID); err != nil {
				t.Fatal(err)
			}
		}, want: "EGO_META_INSTAGRAM_CONNECTION_NOT_BIDIRECTIONAL"},
		{name: "wrong account connection", instagramID: "17841416733781085", setup: func(t *testing.T, database *db.DB, page, instagram *db.AccountAsset) {
			metaAccount, err := database.GetAccount(instagram.AccountID)
			if err != nil {
				t.Fatal(err)
			}
			otherAccount, err := database.CreateAccountForIdentity(metaAccount.IdentityID, "facebook", "Other Facebook", "")
			if err != nil {
				t.Fatal(err)
			}
			otherPage, err := database.CreateAccountAsset(otherAccount.ID, "facebook", "facebook_page", "Other Page", "999999999999999", "", 0, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec("UPDATE account_assets SET connected_asset_id=?, connected_asset_name=? WHERE id=?", otherPage.ID, otherPage.Name, instagram.ID); err != nil {
				t.Fatal(err)
			}
		}, want: "EGO_META_INSTAGRAM_CONNECTION_ACCOUNT_MISMATCH"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, identity, _, page, instagram := newMetaPublishFixture(t, test.instagramID)
			if test.setup != nil {
				test.setup(t, database, page, instagram)
			}
			runner := &fakeProgramRunner{}
			publishPage := egolite.PublishPage{
				PlatformCode: "instagram", ContentType: "graph_text",
				AssetExternalID: instagram.ExternalID, AssetName: instagram.Name,
				Title: "Meta IG 标题", Body: "Meta IG 正文", Media: []egolite.PublishMedia{writeMetaPublishMedia(t)},
			}
			_, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{publishPage})
			if err == nil || err.Error() != test.want {
				t.Fatalf("err=%v, want %s", err, test.want)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("invalid Meta asset touched EgoLite: calls=%d", len(runner.calls))
			}
		})
	}
}

func TestMetaInstagramPrepareUsesFacebookPageIDInUnifiedComposer(t *testing.T) {
	database, identity, meta, pageAsset, instagramAsset := newMetaPublishFixture(t, "17841416733781085")
	if err := database.UpdateAccountAssetConnection(instagramAsset.ID, pageAsset.ID, "facebook", "facebook_page", pageAsset.Name, pageAsset.ExternalID); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountAssetConnection(pageAsset.ID, instagramAsset.ID, "instagram", "instagram_account", instagramAsset.Name, instagramAsset.ExternalID); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "instagram", "status": "awaiting_review"}},
	}}
	page := egolite.PublishPage{
		PlatformCode: "instagram", ContentType: "graph_text",
		AssetExternalID: instagramAsset.ExternalID, AssetName: instagramAsset.Name,
		Title: "Instagram 标题", Body: "Instagram 正文", Media: []egolite.PublishMedia{writeMetaPublishMedia(t)},
	}
	batch, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.PreparedPlatformCodes) != 1 || batch.PreparedPlatformCodes[0] != "instagram" || len(batch.AccountIDs) != 1 || batch.AccountIDs[0] != meta.ID {
		t.Fatalf("batch=%+v", batch)
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	if !strings.Contains(program, "asset_id=1241410172381837") || strings.Contains(program, "asset_id=17841416733781085") {
		t.Fatalf("Instagram composer did not use the Facebook Page public ID: %s", program)
	}
	for _, required := range []string{"metaPageID", "metaInstagramID", "Alpha Page", "Alpha IG", "meta_target_pair", "meta_caption_readback"} {
		if !strings.Contains(program, required) {
			t.Fatalf("Instagram prepare program missing %q: %s", required, program)
		}
	}
	if err := egolite.NewSpaceService(database, runner).ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{page}); err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
		t.Fatalf("Instagram submit must remain fail-closed, err=%v", err)
	}
}

func TestMetaVideoPrepareUsesTypeSpecificComposerAndKeepsSubmitFailClosed(t *testing.T) {
	database, identity, _, pageAsset, instagramAsset := newMetaPublishFixture(t, "17841416733781085")
	if err := database.UpdateAccountAssetConnection(instagramAsset.ID, pageAsset.ID, "facebook", "facebook_page", pageAsset.Name, pageAsset.ExternalID); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountAssetConnection(pageAsset.ID, instagramAsset.ID, "instagram", "instagram_account", instagramAsset.Name, instagramAsset.ExternalID); err != nil {
		t.Fatal(err)
	}
	video := writeMetaPublishVideo(t)
	cover := writeMetaPublishMedia(t)
	cover.Role = "cover"
	tests := []struct {
		name             string
		page             egolite.PublishPage
		composerFragment string
		requireIGPair    bool
	}{
		{
			name: "Facebook Reel", page: egolite.PublishPage{
				PlatformCode: "facebook", ContentType: "video", AssetExternalID: pageAsset.ExternalID, AssetName: pageAsset.Name,
				Title: "Facebook Reel", Body: "冻结 Facebook Reel", PublishAt: time.Now().Add(2 * time.Hour).Truncate(time.Minute).Format(time.RFC3339),
				Media: []egolite.PublishMedia{video, cover},
			}, composerFragment: "/latest/reels_composer?asset_id=1241410172381837",
		},
		{
			name: "Instagram Reel", page: egolite.PublishPage{
				PlatformCode: "instagram", ContentType: "video", AssetExternalID: instagramAsset.ExternalID, AssetName: instagramAsset.Name,
				Title: "Instagram Reel", Body: "冻结 Instagram Reel", Media: []egolite.PublishMedia{video, cover},
			}, composerFragment: "/latest/instagram_reels_composer?asset_id=1241410172381837", requireIGPair: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeProgramRunner{publishPrepare: map[string]any{
				"done": true, "completionAttempted": true, "completionDone": true,
				"results": []map[string]any{{"platformCode": test.page.PlatformCode, "status": "awaiting_review"}},
			}}
			service := egolite.NewSpaceService(database, runner)
			batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{test.page})
			if err != nil {
				t.Fatal(err)
			}
			if batch.State != "awaiting_review" || len(batch.PreparedPlatformCodes) != 1 || batch.PreparedPlatformCodes[0] != test.page.PlatformCode {
				t.Fatalf("batch=%+v", batch)
			}
			program := publishPrepareProgram(t, runner)
			assertJavaScriptSyntax(t, program)
			for _, required := range []string{test.composerFragment, `"contentType":"video"`, "meta_composer_content_type_mismatch", "meta_media_contract_invalid", "meta_caption_readback"} {
				if !strings.Contains(program, required) {
					t.Fatalf("prepare program missing %q: %s", required, program)
				}
			}
			if test.requireIGPair {
				for _, required := range []string{"metaInstagramID", "17841416733781085", "meta_target_pair_readback_mismatch"} {
					if !strings.Contains(program, required) {
						t.Fatalf("Instagram prepare program missing %q: %s", required, program)
					}
				}
			}
			if strings.Contains(program, "submit approved publish") || strings.Contains(program, "click(metaFinalSelector") {
				t.Fatalf("Meta video prepare crossed final-click boundary: %s", program)
			}
			if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{test.page}); err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
				t.Fatalf("Meta video submit must remain fail-closed, err=%v", err)
			}
		})
	}
}

func newMetaPublishFixture(t *testing.T, instagramID string) (*db.DB, *db.EgoIdentity, *db.Account, *db.AccountAsset, *db.AccountAsset) {
	t.Helper()
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("Meta 自动准备浏览器")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := database.CreateAccountForIdentity(identity.ID, "meta", "Meta 容器", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(meta.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	page, err := database.CreateAccountAsset(meta.ID, "facebook", "facebook_page", "Alpha Page", "1241410172381837", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	instagram, err := database.CreateAccountAsset(meta.ID, "instagram", "instagram_account", "Alpha IG", instagramID, "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	return database, identity, meta, page, instagram
}

func writeMetaPublishMedia(t *testing.T) egolite.PublishMedia {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meta-image.png")
	payload := []byte("frozen-meta-image")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	return egolite.PublishMedia{FilePath: path, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "content"}
}

func writeMetaPublishVideo(t *testing.T) egolite.PublishMedia {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meta-video.mp4")
	payload := []byte("meta-video-fixture")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	return egolite.PublishMedia{FilePath: path, SHA256: hex.EncodeToString(digest[:]), Kind: "video", Role: "video"}
}

func TestSpaceServiceObservesExistingSpaceWithoutNavigation(t *testing.T) {
	database := openServiceDB(t)
	account, err := database.CreateAccount("xhs", "主账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpace(account.ID, 77, "task-77", "identity-1/account-1/xhs"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)

	observation, err := service.ObserveAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !observation.SnapshotAvailable || observation.Verified || observation.EvidenceCode != "EGO_SNAPSHOT_UNVERIFIED" {
		t.Fatalf("observation=%+v", observation)
	}
	if strings.Contains(runner.calls[0], "openOrReuseTab") {
		t.Fatal("read-only account observation must not navigate")
	}
}

func TestSpaceServiceObserveAccountRejectsUserControlledOwnership(t *testing.T) {
	for _, ownership := range []string{"user", "agentDelegatedToUser"} {
		t.Run(ownership, func(t *testing.T) {
			database := openServiceDB(t)
			account, err := database.CreateAccount("xhs", "主账号", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.CreateEgoSpace(account.ID, 78, "task-ownership-boundary", "alpha-nexus/ownership-boundary"); err != nil {
				t.Fatal(err)
			}
			runner := &ownershipBoundaryProgramRunner{spaceID: 78, ownership: ownership}

			observation, err := egolite.NewSpaceService(database, runner).ObserveAccount(context.Background(), account.ID)
			if observation != nil || err == nil || err.Error() != "EGO_SPACE_USER_CONTROLLED" {
				t.Fatalf("observation=%+v err=%v, want user-controlled safety stop", observation, err)
			}
			if len(runner.calls) != 1 {
				t.Fatalf("ObserveAccount should use one ownership-guarded program, calls=%d", len(runner.calls))
			}
			if len(runner.events) != 1 || strings.Join(runner.events[0], ",") != "EGO_SPACE_USER_CONTROLLED" {
				t.Fatalf("user-controlled observation performed a side effect: events=%v", runner.events)
			}
		})
	}
}

func TestSpaceServiceOpensManagerThenHandsControlToUser(t *testing.T) {
	database := openServiceDB(t)
	account, err := database.CreateAccount("youtube", "频道", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpace(account.ID, 77, "task-77", "alpha-nexus/matrix-youtube"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)

	space, err := service.OpenManagerAndHandoff(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.SpaceID != 77 || space.State != "manual_takeover" || len(runner.calls) != 1 {
		t.Fatalf("space=%+v programs=%v", space, runner.calls)
	}
	program := runner.calls[0]
	for _, required := range []string{
		"const spaces = await listTaskSpaces()",
		"Number(item.id) === 77",
		"await useOrCreateTaskSpace(77)",
		"https://studio.youtube.com",
		"handOffTaskSpace(77)",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("manager program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "takeOverTaskSpace") || strings.Contains(program, "closeTab") {
		t.Fatalf("manager handoff must not take over or close tabs: %s", program)
	}
}

func assertManagerUserControlledProgramHasNoSideEffects(t *testing.T, program string, spaceID int64) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for generated EgoLite ownership checks")
	}
	bootstrap := `const events = []
globalThis.listTaskSpaces = async () => [{ id: ` + jsonNumber(spaceID) + `, ownership: "user" }]
globalThis.useOrCreateTaskSpace = async () => { events.push("claim") }
globalThis.openOrReuseTab = async () => { events.push("open") }
globalThis.handOffTaskSpace = async () => { events.push("handoff") }
globalThis.cliLog = () => {}
try {
` + program + `
} catch (error) {
  events.push(String(error?.message ?? error))
}
process.stdout.write(JSON.stringify(events))`
	output, err := exec.Command(node, "--input-type=module", "--eval", bootstrap).CombinedOutput()
	if err != nil {
		t.Fatalf("generated manager ownership program failed: %v\n%s", err, output)
	}
	var events []string
	if err := json.Unmarshal(output, &events); err != nil {
		t.Fatalf("decode manager ownership events: %v output=%q", err, output)
	}
	if strings.Join(events, ",") != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("user-controlled manager program performed an action: events=%v", events)
	}
}

func TestSpaceServiceDoesNotNavigateOrTakeOverUserControlledSpace(t *testing.T) {
	database := openServiceDB(t)
	account, err := database.CreateAccount("youtube", "频道", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpace(account.ID, 78, "task-78", "alpha-nexus/matrix-youtube-user"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpdateEgoSpaceState(account.ID, "user", "ready", ""); err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{managerErr: errors.New("EGO_SPACE_USER_CONTROLLED")}
	service := egolite.NewSpaceService(database, runner)

	space, err := service.OpenManagerAndHandoff(context.Background(), account.ID)
	if space != nil || err == nil || err.Error() != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("space=%+v err=%v, want safe user-control error", space, err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("manager should use one ownership-guarded program, calls=%d", len(runner.calls))
	}
	program := runner.calls[0]
	if strings.Contains(program, "takeOverTaskSpace") || strings.Contains(program, "closeTab") {
		t.Fatalf("user-controlled manager program must not take over or close tabs: %s", program)
	}
	assertManagerUserControlledProgramHasNoSideEffects(t, program, 78)
	updated, err := database.GetEgoSpaceByIdentity(account.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Ownership != "agentDelegatedToUser" || updated.State != "manual_takeover" || updated.ErrorCode != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("space=%+v, want persisted user-controlled safety stop", updated)
	}
}

func TestSpaceServiceInspectPublishManagerRejectsUserControlledOwnership(t *testing.T) {
	for _, ownership := range []string{"user", "agentDelegatedToUser"} {
		t.Run(ownership, func(t *testing.T) {
			database := openServiceDB(t)
			identity, err := database.CreateEgoIdentity("发布核验用户接管浏览器")
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
			if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 79, "task-ownership-boundary", "alpha-nexus/ownership-boundary"); err != nil {
				t.Fatal(err)
			}
			runner := &ownershipBoundaryProgramRunner{spaceID: 79, ownership: ownership}

			inspection, err := egolite.NewSpaceService(database, runner).InspectPublishManager(context.Background(), account.ID, "精确标题")
			if inspection != nil || err == nil || err.Error() != "EGO_SPACE_USER_CONTROLLED" {
				t.Fatalf("inspection=%+v err=%v, want user-controlled safety stop", inspection, err)
			}
			if len(runner.calls) != 2 {
				t.Fatalf("inspection should check binding then inspect, calls=%d", len(runner.calls))
			}
			managerProgram := runner.calls[1]
			if !strings.Contains(managerProgram, "const spaces = await listTaskSpaces()") {
				t.Fatalf("manager inspection must check real Space ownership: %s", managerProgram)
			}
			if len(runner.events) != 1 || strings.Join(runner.events[0], ",") != "EGO_SPACE_USER_CONTROLLED" {
				t.Fatalf("user-controlled inspection performed a side effect: events=%v", runner.events)
			}
		})
	}
}

func TestSpaceServiceReturnsBindingMissingWithoutCreatingSpace(t *testing.T) {
	database := openServiceDB(t)
	account, err := database.CreateAccount("youtube", "频道", "")
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeProgramRunner{}
	service := egolite.NewSpaceService(database, runner)

	space, err := service.OpenManagerAndHandoff(context.Background(), account.ID)
	if space != nil || err == nil || err.Error() != "EGO_SPACE_BINDING_MISSING" {
		t.Fatalf("space=%+v err=%v, want missing binding", space, err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("missing DB binding must not invoke EgoLite or create a Space: calls=%d", len(runner.calls))
	}
}

func TestWechatVideoPrepareUsesReviewedIframeAndNeverClicksFinalPublish(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("视频号发布浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "wechat_video", "视频号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(t.TempDir(), "frozen-wechat-video.mp4")
	videoBytes := []byte("frozen-wechat-video")
	if err := os.WriteFile(videoPath, videoBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(videoBytes)
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "wechat_video", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)
	page := egolite.PublishPage{
		PlatformCode: "wechat_video", ContentType: "video", Title: "冻结视频号标题",
		Body: "冻结视频号正文", TagsJSON: `["AlphaNexus","EgoLite"]`,
		PublishAt:    "2030-08-15T11:48:00Z",
		SettingsJSON: `{"link_type":"none","declare_original":false}`,
		Media:        []egolite.PublishMedia{{FilePath: videoPath, SHA256: hex.EncodeToString(digest[:]), Kind: "video", Role: "video"}},
	}
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(batch.PreparedPlatformCodes, ",") != "wechat_video" || len(batch.PlatformErrors) != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://channels.weixin.qq.com/platform/post/create",
		"https://channels.weixin.qq.com/micro/content/post/create",
		"wechatVideoInputSelector", "uploadFile(wechatVideoInputSelector",
		"fileSelected", "frameReady", "expectedWechatBody", "冻结视频号标题", "冻结视频号正文",
		"typeText(\"#\" + tag)", "pressKey(\"SPACE\")", "data-topic",
		"wechat_video_topic_token_missing", "data-alpha-wechat-video-final",
		"wechat_video_final_button_missing_or_disabled", "awaiting_review",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("video channel prepare program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "click(finalSelector") || strings.Contains(program, "submit approved publish") || strings.Contains(program, "确认发表") {
		t.Fatalf("video channel prepare crossed final publish boundary: %s", program)
	}
	if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{page}); err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
		t.Fatalf("video channel scheduled submit must remain fail-closed, err=%v", err)
	}
}

func TestWechatVideoPrepareRejectsUnsafeFrozenInputsBeforeEgoLite(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("视频号约束浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "wechat_video", "视频号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(t.TempDir(), "frozen-wechat-video.mp4")
	videoBytes := []byte("frozen-wechat-video-constraints")
	if err := os.WriteFile(videoPath, videoBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(videoBytes)
	base := egolite.PublishPage{
		PlatformCode: "wechat_video", ContentType: "video", Title: "冻结标题", Body: "冻结正文",
		Media: []egolite.PublishMedia{{FilePath: videoPath, SHA256: hex.EncodeToString(digest[:]), Kind: "video", Role: "video"}},
	}
	imagePath := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(imagePath, []byte("cover"), 0o600); err != nil {
		t.Fatal(err)
	}
	imageDigest := sha256.Sum256([]byte("cover"))
	cases := []struct {
		name string
		page egolite.PublishPage
		want string
	}{
		{name: "missing video", page: func() egolite.PublishPage { value := base; value.Media = nil; return value }(), want: "EGO_WECHAT_VIDEO_MEDIA_INVALID"},
		{name: "frozen cover", page: func() egolite.PublishPage {
			value := base
			value.Media = append(value.Media, egolite.PublishMedia{FilePath: imagePath, SHA256: hex.EncodeToString(imageDigest[:]), Kind: "image", Role: "cover"})
			return value
		}(), want: "EGO_WECHAT_VIDEO_MEDIA_INVALID"},
		{name: "extra role", page: func() egolite.PublishPage {
			value := base
			value.Media = append(value.Media, egolite.PublishMedia{FilePath: videoPath, SHA256: hex.EncodeToString(digest[:]), Kind: "video", Role: "extra"})
			return value
		}(), want: "EGO_WECHAT_VIDEO_MEDIA_INVALID"},
		{name: "non-default location", page: func() egolite.PublishPage { value := base; value.SettingsJSON = `{"location":"上海"}`; return value }(), want: "EGO_WECHAT_VIDEO_SETTINGS_UNSUPPORTED"},
		{name: "non-default annotation schema field", page: func() egolite.PublishPage {
			value := base
			value.SettingsJSON = `{"video_annotation":"none"}`
			return value
		}(), want: "EGO_WECHAT_VIDEO_SETTINGS_UNSUPPORTED"},
		{name: "past schedule", page: func() egolite.PublishPage { value := base; value.PublishAt = "2026-08-10T13:00:00+08:00"; return value }(), want: "EGO_PUBLISH_SCHEDULE_INVALID"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeProgramRunner{}
			_, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{test.page})
			if err == nil || err.Error() != test.want {
				t.Fatalf("err=%v, want %s", err, test.want)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("unsafe frozen input touched EgoLite: calls=%d", len(runner.calls))
			}
		})
	}
}

func TestWechatMPPrepareUsesNewEditorCoverReadbackAndNeverClicksFinalPublish(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("公众号发布浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "wechat_mp", "微信公众号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "frozen-wechat-article.png")
	imageBytes := []byte("frozen-wechat-article-image")
	if err := os.WriteFile(imagePath, imageBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(imageBytes)
	runner := &fakeProgramRunner{publishPrepare: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"results": []map[string]any{{"platformCode": "wechat_mp", "status": "awaiting_review"}},
	}}
	service := egolite.NewSpaceService(database, runner)
	page := egolite.PublishPage{
		PlatformCode: "wechat_mp", ContentType: "article", Title: "冻结公众号标题", Body: "冻结公众号正文",
		SettingsJSON: `{"author":"Alpha Nexus","original_link":"","comment_enabled":false,"declare_original":false,"allow_reprint":false,"publish_type":"publish","collection":"","content_source":"none"}`,
		Media:        []egolite.PublishMedia{{FilePath: imagePath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "content"}},
	}
	batch, err := service.OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{page})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(batch.PreparedPlatformCodes, ",") != "wechat_mp" || len(batch.PlatformErrors) != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	program := publishPrepareProgram(t, runner)
	assertJavaScriptSyntax(t, program)
	for _, required := range []string{
		"https://mp.weixin.qq.com/", "beforeTabs", "/cgi-bin/appmsg", "switchTab",
		".title-editor__input .ProseMirror", ".mock-iframe-body .ProseMirror[contenteditable='true']",
		"wechatMPSetField", "li.jsInsertIcon.img", "uploadFile(imageInputSelector",
		".js_cover_btn_area", "从正文选择", ".appmsg_content_img_item", ".js_cover_preview_new",
		"input.js_interaction_setting", ".js_edit_ori input.js_ori_setting_checkbox", ".js_unset_original_title",
		".js_article_tags_content", ".js_claim_source_selected", ".new_mass_send_dialog .weui-desktop-dialog",
		"群发通知", "分组通知", "定时发表", "publish_type", "data-alpha-wechat-mp-final",
		"wechat_mp_final_button_missing_or_disabled", "awaiting_review",
	} {
		if !strings.Contains(program, required) {
			t.Fatalf("WeChat article prepare program missing %q: %s", required, program)
		}
	}
	for _, forbidden := range []string{
		"click(finalSelector", "无需声明并发表", "click(publishSelector",
		"[role='combobox'][aria-label*='内容来源']", "[role='combobox'][aria-label*='发表类型']",
		"wechatMPSetField(collectionSelector", "wechatMPSetField(sourceSelector", "wechatMPSetField(publishTypeSelector",
	} {
		if strings.Contains(program, forbidden) {
			t.Fatalf("WeChat article prepare crossed final publish boundary %q: %s", forbidden, program)
		}
	}
	if err := service.ValidateSubmitPublishBatch(identity.ID, []egolite.PublishPage{page}); err == nil || err.Error() != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
		t.Fatalf("WeChat article submit must remain fail-closed, err=%v", err)
	}
}

func TestWechatMPPrepareRejectsUnsafeFrozenInputsBeforeEgoLite(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("公众号约束浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "wechat_mp", "微信公众号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "frozen-wechat-article.png")
	imageBytes := []byte("frozen-wechat-article-constraints")
	if err := os.WriteFile(imagePath, imageBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(imageBytes)
	base := egolite.PublishPage{
		PlatformCode: "wechat_mp", ContentType: "article", Title: "冻结标题", Body: "冻结正文",
		Media: []egolite.PublishMedia{{FilePath: imagePath, SHA256: hex.EncodeToString(digest[:]), Kind: "image", Role: "content"}},
	}
	videoPath := filepath.Join(t.TempDir(), "wrong-kind.mp4")
	videoBytes := []byte("not-an-image")
	if err := os.WriteFile(videoPath, videoBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	videoDigest := sha256.Sum256(videoBytes)
	tooLongAuthor := strings.Repeat("a", 65)
	cases := []struct {
		name string
		page egolite.PublishPage
		want string
	}{
		{name: "missing image", page: func() egolite.PublishPage { value := base; value.Media = nil; return value }(), want: "EGO_WECHAT_MP_MEDIA_INVALID"},
		{name: "multiple images", page: func() egolite.PublishPage {
			value := base
			value.Media = append(value.Media, value.Media[0])
			return value
		}(), want: "EGO_WECHAT_MP_MEDIA_INVALID"},
		{name: "video media", page: func() egolite.PublishPage {
			value := base
			value.Media = []egolite.PublishMedia{{FilePath: videoPath, SHA256: hex.EncodeToString(videoDigest[:]), Kind: "video", Role: "content"}}
			return value
		}(), want: "EGO_WECHAT_MP_MEDIA_INVALID"},
		{name: "unsupported role", page: func() egolite.PublishPage {
			value := base
			value.Media = append([]egolite.PublishMedia(nil), value.Media...)
			value.Media[0].Role = "body"
			return value
		}(), want: "EGO_WECHAT_MP_MEDIA_INVALID"},
		{name: "tags", page: func() egolite.PublishPage { value := base; value.TagsJSON = `["标签"]`; return value }(), want: "EGO_WECHAT_MP_TAGS_UNSUPPORTED"},
		{name: "unknown setting", page: func() egolite.PublishPage { value := base; value.SettingsJSON = `{"unknown":false}`; return value }(), want: "EGO_WECHAT_MP_SETTINGS_UNSUPPORTED"},
		{name: "non-default publish type", page: func() egolite.PublishPage {
			value := base
			value.SettingsJSON = `{"publish_type":"mass"}`
			return value
		}(), want: "EGO_WECHAT_MP_SETTINGS_UNSUPPORTED"},
		{name: "unsafe author", page: func() egolite.PublishPage {
			value := base
			value.SettingsJSON = `{"author":"` + tooLongAuthor + `"}`
			return value
		}(), want: "EGO_WECHAT_MP_SETTINGS_INVALID"},
		{name: "scheduled", page: func() egolite.PublishPage { value := base; value.PublishAt = "2026-08-10T13:00:00+08:00"; return value }(), want: "EGO_PUBLISH_SCHEDULE_UNVERIFIED"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeProgramRunner{}
			_, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(context.Background(), identity.ID, []egolite.PublishPage{test.page})
			if err == nil || err.Error() != test.want {
				t.Fatalf("err=%v, want %s", err, test.want)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("unsafe frozen input touched EgoLite: calls=%d", len(runner.calls))
			}
		})
	}
}

func openServiceDB(t *testing.T) *db.DB {
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

func jsonNumber(value int64) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

// bindingProbeAnswer answers the membership pre-flight (a bare listTaskSpaces
// read) with the Spaces the test wrote to the database. It reports handled=false
// for every other program so each runner keeps its own fixtures.
func bindingProbeAnswer(database *db.DB, program string, result any) (bool, error) {
	if database == nil || !strings.Contains(program, "alpha-space-probe") {
		return false, nil
	}
	listing := []map[string]any{}
	for identityID := int64(1); identityID <= 8; identityID++ {
		if space, err := database.GetEgoSpaceByIdentity(identityID); err == nil && space != nil {
			listing = append(listing, map[string]any{"id": space.SpaceID, "ownership": space.Ownership})
		}
	}
	raw, _ := json.Marshal(map[string]any{"spaces": listing})
	return true, json.Unmarshal(raw, result)
}

func (runner *manualPublishContractRunner) BindProgramRunnerDB(database *db.DB) {
	runner.db = database
}

func (runner *ownershipBoundaryProgramRunner) BindProgramRunnerDB(database *db.DB) {
	runner.db = database
}

func (runner *creatorMetricsRunner) BindProgramRunnerDB(database *db.DB) {
	runner.db = database
}
