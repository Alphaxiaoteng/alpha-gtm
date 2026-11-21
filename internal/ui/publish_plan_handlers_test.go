package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/tools"
)

type userControlledPublishPrepareRunner struct {
	programs []string
}

func (runner *userControlledPublishPrepareRunner) Run(_ context.Context, program string, result any) error {
	runner.programs = append(runner.programs, program)
	if strings.Contains(program, "const preparationResults = []") {
		return errors.New("EGO_SPACE_USER_CONTROLLED")
	}
	payload := `{"id":14,"taskId":"task-14","name":"alpha-nexus/matrix-1","ownership":"user","done":true}`
	if err := json.Unmarshal([]byte(payload), result); err != nil {
		return err
	}
	return nil
}

type delegatedPublishPrepareRunner struct {
	programs []string
}

func (runner *delegatedPublishPrepareRunner) Run(_ context.Context, program string, result any) error {
	runner.programs = append(runner.programs, program)
	payload := `{"id":14,"taskId":"task-14","name":"alpha-nexus/matrix-1","ownership":"agentDelegatedToUser","done":true}`
	if strings.Contains(program, "const preparationResults = []") {
		payload = `{"done":true,"completionAttempted":true,"completionDone":true,"results":[{"platformCode":"douyin","status":"awaiting_review"}]}`
	} else if strings.Contains(program, "completeTaskSpace(") {
		payload = `{"done":true}`
	}
	return json.Unmarshal([]byte(payload), result)
}

func TestPublishPlanExecutionContextIsBoundedAndIndependent(t *testing.T) {
	requestContext, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	if requestContext.Err() == nil {
		t.Fatal("request fixture was not cancelled")
	}
	executionContext, cancelExecution := publishPlanExecutionContext()
	defer cancelExecution()
	if executionContext.Err() != nil {
		t.Fatalf("background execution inherited request cancellation: %v", executionContext.Err())
	}
	deadline, ok := executionContext.Deadline()
	if !ok || time.Until(deadline) > publishPlanExecutionTimeout || time.Until(deadline) < publishPlanExecutionTimeout-time.Second {
		t.Fatalf("unexpected execution deadline: %v", deadline)
	}
}

func TestPublishPlanPrepareDiagnosticPropagatesToResponseTargets(t *testing.T) {
	diagnostic := &egolite.PublishPrepareDiagnostic{
		Branch: "suggestion_click", Stage: "native_commit", BodyTail: "正文 #αgamer",
		Nodes: []egolite.PublishPrepareDiagnosticNode{{
			Tag: "a", Text: "#αgamer",
			Attributes: egolite.PublishPrepareDiagnosticAttributes{DataMention: "#", ContentEditable: "false"},
		}},
	}
	targets := []egoPublishTargetResult{{
		AccountID: 7, Platform: "douyin", Status: "manual_takeover",
		ErrorCode: "EGO_DOUYIN_TOPIC_NATIVE_COMMIT_FAILED", Diagnostic: diagnostic,
	}}
	execution := publishPlanExecution(3, "manual_takeover", targets)
	if execution.Targets[0].Diagnostic != diagnostic {
		t.Fatalf("execution diagnostic=%+v", execution.Targets[0].Diagnostic)
	}
	payload, err := json.Marshal(map[string]any{"targets": targets, "execution": execution})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"diagnostic"`, `"branch":"suggestion_click"`, `"stage":"native_commit"`, `"data-mention":"#"`} {
		if !bytes.Contains(payload, []byte(required)) {
			t.Fatalf("prepare response missing %s: %s", required, payload)
		}
	}
	for _, forbidden := range []string{"outerHTML", "cookie", "className", `"id"`} {
		if bytes.Contains(payload, []byte(forbidden)) {
			t.Fatalf("prepare response leaked %q: %s", forbidden, payload)
		}
	}
}

func TestPublishPlanSubmitReturnsPersistedExecutionForResultUnknown(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database, mux: http.NewServeMux()}
	server.toolRuntime = &tools.Runtime{
		DataDir: dataDir,
		SubmitPublishPlan: func(_ context.Context, planID int64, _, _, _ string) (*tools.Result, error) {
			execution := publishPlanExecution(planID, "result_unknown", []egoPublishTargetResult{{
				AccountID: 7, Platform: "douyin", JobID: 19, Status: "result_unknown", ErrorCode: "EGO_SUBMIT_RESULT_UNKNOWN",
			}})
			return &tools.Result{OK: false, Tool: "publish.plan.submit", Error: "EGO_SUBMIT_RESULT_UNKNOWN", Data: map[string]interface{}{"execution": execution}}, fmt.Errorf("EGO_SUBMIT_RESULT_UNKNOWN")
		},
	}
	server.routes()
	body := bytes.NewBufferString(`{"snapshotHash":"hash","approvalId":"approval","nonce":"nonce"}`)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/publish-plans/3/submit", body))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"result_unknown"`) ||
		!strings.Contains(response.Body.String(), `"step":"creator_backend_verification"`) ||
		!strings.Contains(response.Body.String(), `"errorCode":"EGO_SUBMIT_RESULT_UNKNOWN"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEgoPrepareMarksAccountLoginRequiredAndContinues(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("graph_text", "title", "caption", "body", `[]`, "draft")
	loggedIn, _ := database.CreateAccount("kuaishou", "快手", "")
	loggedOut, _ := database.CreateAccount("douyin", "抖音", "")
	for _, accountID := range []int64{loggedIn.ID, loggedOut.ID} {
		if err := database.UpdateAccountLoginState(accountID, db.AccountAuthLoggedIn); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "image.png")
	body := []byte("image")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(body)
	media, _ := database.CreateMediaAsset(content.ID, "image", path, path, "image/png", int64(len(body)), hex.EncodeToString(digest[:]), 100, 100, 0)
	service := &publishplan.Service{DB: database}
	plan, err := service.Freeze(publishplan.FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID},
		Targets: []publishplan.TargetInput{{AccountID: loggedIn.ID}, {AccountID: loggedOut.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(loggedOut.ID, db.AccountAuthLoggedOut); err != nil {
		t.Fatal(err)
	}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "test-session", publishplan.PublishPlanActionPrepare, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runner := &egoHandlerRunner{prepareResults: []map[string]any{{
		"platformCode": "kuaishou", "status": "awaiting_review",
	}}}
	server := &Server{
		db: database, cfg: config.Config{MaxConcurrentSpaces: 3},
		egoSpaces: egolite.NewSpaceService(database, runner),
	}
	result, err := server.preparePublishPlan(context.Background(), plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce)
	if err == nil || result == nil || result.OK || result.Error != "EGO_PREPARE_PARTIAL_FAILED" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	reloaded, err := service.Get(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	states := make(map[int64]publishplan.TargetState)
	for _, state := range reloaded.TargetStates {
		states[state.AccountID] = state
	}
	if states[loggedOut.ID].Status != "login_required" || states[loggedOut.ID].ErrorCode != "LOGIN_REQUIRED" {
		t.Fatalf("logged-out target=%+v", states[loggedOut.ID])
	}
	if states[loggedIn.ID].Status != "awaiting_review" {
		t.Fatalf("logged-in target=%+v", states[loggedIn.ID])
	}
	for _, program := range runner.programs {
		if strings.Contains(program, `"platformCode":"douyin"`) {
			t.Fatalf("logged-out platform was opened: %s", program)
		}
	}
	composerOpened := false
	for _, program := range runner.programs {
		if strings.Contains(program, "https://cp.kuaishou.com/article/publish/video") {
			composerOpened = true
		}
	}
	if !composerOpened {
		t.Fatalf("logged-in graph-text target did not open the reviewed composer: %v", runner.programs)
	}
}

func TestEgoCSDNPreparePersistsAwaitingReviewWithoutTerminalSubmit(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("article", "CSDN 冻结标题", "", "# 冻结正文", `["AlphaNexus"]`, "draft")
	account, _ := database.CreateAccount("csdn", "CSDN", "")
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	service := &publishplan.Service{DB: database}
	plan, err := service.Freeze(publishplan.FreezeInput{
		ContentID: content.ID, MediaMode: "article",
		Targets: []publishplan.TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "test-session", publishplan.PublishPlanActionPrepare, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runner := &egoHandlerRunner{}
	server := &Server{db: database, cfg: config.Config{MaxConcurrentSpaces: 1}, egoSpaces: egolite.NewSpaceService(database, runner), mux: http.NewServeMux()}
	server.toolRuntime = &tools.Runtime{DataDir: dataDir, PreparePublishPlan: server.PrepareApprovedPublishPlan}
	server.routes()
	prepareBody, _ := json.Marshal(map[string]string{"snapshotHash": plan.SnapshotHash, "approvalId": grant.ID, "nonce": grant.Nonce})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/publish-plans/"+jsonNumber(plan.ID)+"/prepare", bytes.NewReader(prepareBody)))
	if response.Code != http.StatusOK {
		t.Fatalf("prepare status=%d body=%s", response.Code, response.Body.String())
	}
	var execution struct {
		PlanID  int64  `json:"planId"`
		Status  string `json:"status"`
		Targets []struct {
			AccountID    int64  `json:"accountId"`
			PlatformCode string `json:"platformCode"`
			JobID        int64  `json:"jobId"`
			Status       string `json:"status"`
			Step         string `json:"step"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &execution); err != nil || execution.PlanID != plan.ID ||
		execution.Status != "awaiting_review" || len(execution.Targets) != 1 ||
		execution.Targets[0].AccountID != account.ID || execution.Targets[0].PlatformCode != "csdn" ||
		execution.Targets[0].JobID <= 0 || execution.Targets[0].Status != "awaiting_review" || execution.Targets[0].Step != "ready_to_submit" {
		t.Fatalf("execution=%+v body=%s err=%v", execution, response.Body.String(), err)
	}
	reloaded, err := service.Get(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != "awaiting_review" || len(reloaded.TargetStates) != 1 || reloaded.TargetStates[0].Status != "awaiting_review" {
		t.Fatalf("plan=%+v targets=%+v", reloaded, reloaded.TargetStates)
	}
	program := ""
	for _, candidate := range runner.programs {
		if strings.Contains(candidate, "const preparationResults = []") {
			program = candidate
		}
	}
	if program == "" {
		t.Fatal("CSDN prepare runtime program was not called")
	}
	if !strings.Contains(program, "button.btn.btn-publish:not(.btn-b-red)") || strings.Contains(program, "submit approved publish") {
		t.Fatalf("CSDN prepare crossed submit boundary: %s", program)
	}
}

func TestEgoPrepareProcessesSameIdentityTargetsSeriallyWithoutQueueing(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.CreateEgoIdentity("串行发布浏览器")
	if err != nil {
		t.Fatal(err)
	}
	accounts := make([]*db.Account, 0, 4)
	for _, code := range []string{"douyin", "kuaishou", "juejin", "baijiahao"} {
		account, createErr := database.CreateAccountForIdentity(identity.ID, code, code, "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		if updateErr := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); updateErr != nil {
			t.Fatal(updateErr)
		}
		accounts = append(accounts, account)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 23, "alpha-nexus/matrix-queue", "alpha-nexus/matrix-queue"); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "队列冻结标题", "", "队列冻结正文", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "queue-image.png")
	mediaBytes := []byte("queue-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	media, err := database.CreateMediaAsset(content.ID, "image", mediaPath, mediaPath, "image/png", int64(len(mediaBytes)), hex.EncodeToString(digest[:]), 1080, 1440, 0)
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]publishplan.TargetInput, 0, len(accounts))
	for _, account := range accounts {
		targets = append(targets, publishplan.TargetInput{AccountID: account.ID})
	}
	service := &publishplan.Service{DB: database}
	plan, err := service.Freeze(publishplan.FreezeInput{ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID}, Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "test-session", publishplan.PublishPlanActionPrepare, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runner := &egoHandlerRunner{
		boundSpace: map[string]any{"id": 23, "taskId": "alpha-nexus/matrix-queue", "name": "alpha-nexus/matrix-queue", "ownership": "agent"},
		prepareResults: []map[string]any{
			{"platformCode": "douyin", "status": "awaiting_review"},
			{"platformCode": "kuaishou", "status": "awaiting_review"},
			{"platformCode": "juejin", "status": "failed", "errorCode": "EGO_PUBLISH_AUTOMATION_UNSUPPORTED"},
		},
	}
	server := &Server{db: database, cfg: config.Config{MaxConcurrentSpaces: 3}, egoSpaces: egolite.NewSpaceService(database, runner)}
	result, err := server.preparePublishPlan(context.Background(), plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce)
	if err == nil || result == nil || result.OK || result.Error != "EGO_PREPARE_PARTIAL_FAILED" {
		t.Fatalf("prepare=%+v err=%v", result, err)
	}
	reloaded, err := service.Get(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	var fourth publishplan.TargetState
	for _, state := range reloaded.TargetStates {
		if state.AccountID == accounts[3].ID {
			fourth = state
		}
	}
	if fourth.Status != "failed" || fourth.ErrorCode != "EGO_PUBLISH_AUTOMATION_UNSUPPORTED" || fourth.JobID != 0 {
		t.Fatalf("fourth target=%+v, want isolated unsupported result without a job", fourth)
	}
	preparePrograms := 0
	for _, program := range runner.programs {
		if strings.Contains(program, "const preparationResults = []") {
			preparePrograms++
		}
	}
	if preparePrograms != 4 {
		t.Fatalf("prepare programs=%d, want four serial one-target runs", preparePrograms)
	}
}

func TestEgoPrepareMapsUserControlledSpaceToFailedWithoutPublishTakeover(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "冻结标题", "", "冻结正文", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen-image.png")
	mediaBytes := []byte("frozen-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	media, err := database.CreateMediaAsset(content.ID, "image", mediaPath, mediaPath, "image/png", int64(len(mediaBytes)), hex.EncodeToString(digest[:]), 100, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	identity, err := database.GetEgoIdentity(account.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 14, "task-14", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	plan, err := (&publishplan.Service{DB: database}).Freeze(publishplan.FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID},
		Targets: []publishplan.TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := (&publishplan.Service{DB: database}).ApproveForAction(plan.ID, plan.SnapshotHash, "test-session", publishplan.PublishPlanActionPrepare, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runner := &userControlledPublishPrepareRunner{}
	server := &Server{
		db: database, cfg: config.Config{MaxConcurrentSpaces: 1},
		egoSpaces: egolite.NewSpaceService(database, runner),
	}
	result, err := server.preparePublishPlan(context.Background(), plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce)
	if err == nil || result == nil || result.OK || result.Error != "EGO_PREPARE_PARTIAL_FAILED" {
		t.Fatalf("result=%+v err=%v, want a persisted publish failure", result, err)
	}
	reloaded, err := (&publishplan.Service{DB: database}).Get(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != "failed" || len(reloaded.TargetStates) != 1 ||
		reloaded.TargetStates[0].Status != "failed" ||
		reloaded.TargetStates[0].ErrorCode != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("plan=%+v, want failed target without a publish takeover", reloaded)
	}
	var jobs int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE plan_id=?`, plan.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatalf("created %d publish jobs for a user-controlled Space", jobs)
	}
	var prepareProgram string
	for _, program := range runner.programs {
		if strings.Contains(program, "const preparationResults = []") {
			prepareProgram = program
		}
	}
	if !strings.Contains(prepareProgram, `if (existingSpace.ownership === "user")`) ||
		!strings.Contains(prepareProgram, `throw new Error("EGO_SPACE_USER_CONTROLLED")`) {
		t.Fatalf("user-controlled Space was not guarded by the runtime before preparation: %s", prepareProgram)
	}
	if strings.Contains(prepareProgram, "completeTaskSpace(") {
		t.Fatalf("prepare must report before the dedicated completion runtime call: %s", prepareProgram)
	}
}

func TestEgoPrepareReclaimsDelegatedSpaceForApprovedPrepare(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "冻结标题", "", "冻结正文", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "frozen-image.png")
	mediaBytes := []byte("frozen-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	media, err := database.CreateMediaAsset(content.ID, "image", mediaPath, mediaPath, "image/png", int64(len(mediaBytes)), hex.EncodeToString(digest[:]), 100, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	identity, err := database.GetEgoIdentity(account.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 14, "task-14", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	plan, err := (&publishplan.Service{DB: database}).Freeze(publishplan.FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID},
		Targets: []publishplan.TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := (&publishplan.Service{DB: database}).ApproveForAction(plan.ID, plan.SnapshotHash, "test-session", publishplan.PublishPlanActionPrepare, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runner := &delegatedPublishPrepareRunner{}
	server := &Server{
		db: database, cfg: config.Config{MaxConcurrentSpaces: 1},
		egoSpaces: egolite.NewSpaceService(database, runner),
	}
	result, err := server.preparePublishPlan(context.Background(), plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce)
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v, want delegated Space prepared for review", result, err)
	}
	reloaded, err := (&publishplan.Service{DB: database}).Get(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != "awaiting_review" || len(reloaded.TargetStates) != 1 || reloaded.TargetStates[0].Status != "awaiting_review" {
		t.Fatalf("plan=%+v, want delegated target awaiting review", reloaded)
	}
	space, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if space.Ownership != "agent" || space.State != "ready" {
		t.Fatalf("Space=%+v, want retained agent-ready Space after prepare", space)
	}
	var jobs int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE plan_id=?`, plan.ID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("created %d publish jobs, want one awaiting-review job", jobs)
	}
	var prepareProgram string
	for _, program := range runner.programs {
		if strings.Contains(program, "const preparationResults = []") {
			prepareProgram = program
		}
	}
	if !strings.Contains(prepareProgram, "await takeOverTaskSpace(14)") {
		t.Fatalf("delegated Space did not enter the approved prepare runtime through takeover: %s", prepareProgram)
	}
	if !strings.Contains(strings.Join(runner.programs, "\n"), "completeTaskSpace(14, { keep: true })") {
		t.Fatalf("delegated prepare did not retain the Space through completeTaskSpace keep:true: %s", strings.Join(runner.programs, "\n"))
	}
}

func TestEgoDouyinScheduledPrepareFlowsFromFrozenPlanToZeroBasedSpace(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.GetEgoIdentity(1)
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
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 0, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "定时发布冻结标题", "", "定时发布冻结正文", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "scheduled-image.png")
	mediaBytes := []byte("scheduled-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	media, err := database.CreateMediaAsset(content.ID, "image", mediaPath, mediaPath, "image/png", int64(len(mediaBytes)), hex.EncodeToString(digest[:]), 1080, 1440, 0)
	if err != nil {
		t.Fatal(err)
	}
	publishAt := time.Now().Add(24 * time.Hour).Truncate(time.Minute).Format(time.RFC3339)
	planService := &publishplan.Service{DB: database}
	plan, err := planService.Freeze(publishplan.FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID}, PublishAt: publishAt,
		Targets: []publishplan.TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := planService.ApproveForAction(plan.ID, plan.SnapshotHash, "test-session", publishplan.PublishPlanActionPrepare, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runner := &egoHandlerRunner{boundSpace: map[string]any{
		"id": 0, "taskId": "alpha-nexus/matrix-1", "name": "alpha-nexus/matrix-1", "ownership": "agent",
	}}
	server := &Server{db: database, cfg: config.Config{MaxConcurrentSpaces: 1}, egoSpaces: egolite.NewSpaceService(database, runner)}
	result, err := server.preparePublishPlan(context.Background(), plan.ID, plan.SnapshotHash, grant.ID, grant.Nonce)
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	reloaded, err := planService.Get(plan.ID)
	if err != nil || len(reloaded.TargetStates) != 1 || reloaded.TargetStates[0].Status != "awaiting_review" {
		t.Fatalf("plan=%+v err=%v", reloaded, err)
	}
	program := ""
	for _, candidate := range runner.programs {
		if strings.Contains(candidate, "const preparationResults = []") {
			program = candidate
		}
	}
	if program == "" {
		t.Fatal("Douyin prepare runtime program was not called")
	}
	frozenPublishAt := reloaded.Snapshot.Targets[0].PublishAt
	if frozenPublishAt == "" {
		t.Fatal("frozen scheduled target lost publish_at")
	}
	for _, required := range []string{frozenPublishAt, "douyin_schedule_mode_selector", "douyin_schedule_time_readback_mismatch"} {
		if !strings.Contains(program, required) {
			t.Fatalf("scheduled prepare did not reach EgoLite worker contract %q: %s", required, program)
		}
	}
	if strings.Contains(program, "submit approved publish") {
		t.Fatal("scheduled prepare crossed final submit boundary")
	}
}

func TestInspectEgoPublishManagerPlanRejectsUnverifiedCapabilityBeforeRuntime(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.CreateEgoIdentity("定时发布证据浏览器")
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
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 93, "alpha-nexus/douyin-scheduled-evidence", "定时发布证据"); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "冻结定时标题", "", "冻结定时正文", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(t.TempDir(), "scheduled-evidence.png")
	mediaBytes := []byte("scheduled-evidence")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	media, err := database.CreateMediaAsset(content.ID, "image", mediaPath, mediaPath, "image/png", int64(len(mediaBytes)), hex.EncodeToString(digest[:]), 1080, 1440, 0)
	if err != nil {
		t.Fatal(err)
	}
	publishAt := "2030-08-15T11:48:00Z"
	planService := &publishplan.Service{DB: database}
	plan, err := planService.Freeze(publishplan.FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID}, PublishAt: publishAt,
		Targets: []publishplan.TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var targetID int64
	if err := database.QueryRow(`SELECT id FROM publish_plan_targets WHERE plan_id=?`, plan.ID).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	target := plan.Snapshot.Targets[0]
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`UPDATE publish_plans SET status='approved', approved_action='prepare', approved_at=?, updated_at=? WHERE id=?`, now, now, plan.ID); err != nil {
		t.Fatal(err)
	}
	prepareJob, created, err := database.CreatePublishPlanJobWithIntentResult(
		plan.ID, targetID, plan.SnapshotHash, 0, content.ID, account.ID, target.AssetID,
		"douyin", "graph_text", "scheduled-evidence:prepare", "prepare", target.SettingsJSON,
	)
	if err != nil || !created {
		t.Fatalf("prepare job=%+v created=%v err=%v", prepareJob, created, err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET status='awaiting_review', current_step='ready_to_submit' WHERE id=?`, prepareJob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plan_targets SET status='awaiting_review' WHERE id=?`, targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plans SET status='approved', approved_action='submit', updated_at=? WHERE id=?`, now, plan.ID); err != nil {
		t.Fatal(err)
	}
	submitJob, created, err := database.CreatePublishPlanJobWithIntentResult(
		plan.ID, targetID, plan.SnapshotHash, 0, content.ID, account.ID, target.AssetID,
		"douyin", "graph_text", "scheduled-evidence:submit", "submit", target.SettingsJSON,
	)
	if err != nil || !created {
		t.Fatalf("submit job=%+v created=%v err=%v", submitJob, created, err)
	}
	if _, err := database.Exec(`UPDATE publish_jobs SET status='awaiting_review', current_step='creator_backend_verification' WHERE id=?`, submitJob.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plan_targets SET status='submitted_pending_verification', publish_job_id=? WHERE id=?`, submitJob.ID, targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE publish_plans SET status='submitted_pending_verification', updated_at=? WHERE id=?`, now, plan.ID); err != nil {
		t.Fatal(err)
	}
	runner := &egoHandlerRunner{publishInspect: map[string]any{
		"done": true, "completionAttempted": true, "completionDone": true,
		"location":         "https://creator.douyin.com/creator-micro/content/manage",
		"inspectionStatus": "inspected", "matchingTitle": true,
		"publishedState": false, "scheduledState": true, "observedStatus": "定时发布",
		"effectiveScheduledAt": "2030-08-15T11:48:00Z",
		"screenshotBase64":     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	}}
	server := &Server{db: database, cfg: config.Config{MaxConcurrentSpaces: 1}, egoSpaces: egolite.NewSpaceService(database, runner)}
	inspection, err := server.inspectEgoPublishManagerPlan(context.Background(), plan.ID)
	if err == nil || err.Error() != "EGO_PUBLISH_VERIFY_UNVERIFIED" || inspection != nil {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
	if len(runner.programs) != 0 {
		t.Fatalf("unverified manager inspection reached EgoLite runtime: programs=%d", len(runner.programs))
	}
}

func TestEgoDouyinSubmitCannotBeApprovedOrClickedWithoutVerifiedCapability(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.GetEgoIdentity(1)
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
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 14, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("graph_text", "冻结标题", "", "冻结正文", `[]`, "draft")
	mediaPath := filepath.Join(t.TempDir(), "frozen-image.png")
	mediaBytes := []byte("frozen-image")
	if err := os.WriteFile(mediaPath, mediaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(mediaBytes)
	media, err := database.CreateMediaAsset(
		content.ID, "image", mediaPath, mediaPath, "image/png", int64(len(mediaBytes)), hex.EncodeToString(digest[:]), 1080, 1440, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	service := &publishplan.Service{DB: database}
	plan, err := service.Freeze(publishplan.FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID},
		Targets: []publishplan.TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := &egoHandlerRunner{boundSpace: map[string]any{
		"id": 14, "taskId": "alpha-nexus/matrix-1", "name": "alpha-nexus/matrix-1", "ownership": "agent",
	}}
	server := &Server{db: database, cfg: config.Config{MaxConcurrentSpaces: 1}, egoSpaces: egolite.NewSpaceService(database, runner), mux: http.NewServeMux()}
	server.routes()
	prepareGrant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "test-session", publishplan.PublishPlanActionPrepare, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := server.preparePublishPlan(context.Background(), plan.ID, plan.SnapshotHash, prepareGrant.ID, prepareGrant.Nonce)
	if err != nil || prepared == nil || !prepared.OK {
		t.Fatalf("prepare=%+v err=%v", prepared, err)
	}
	submitGrant, err := service.ApproveForAction(plan.ID, plan.SnapshotHash, "test-session", publishplan.PublishPlanActionSubmit, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	submitted, err := server.submitPublishPlan(context.Background(), plan.ID, plan.SnapshotHash, submitGrant.ID, submitGrant.Nonce)
	if err == nil || submitted == nil || submitted.Error != "EGO_SUBMIT_PARTIAL_FAILED" {
		t.Fatalf("submit=%+v err=%v", submitted, err)
	}
	currentPlan, currentPlanErr := service.Get(plan.ID)
	if currentPlanErr != nil || len(currentPlan.TargetStates) != 1 || currentPlan.TargetStates[0].SubmitAttempted || currentPlan.TargetStates[0].ErrorCode != "EGO_PUBLISH_SUBMIT_UNVERIFIED" {
		t.Fatalf("unverified submit plan=%+v err=%v", currentPlan, currentPlanErr)
	}
	clickPrograms := 0
	for _, program := range runner.programs {
		if strings.Contains(program, "submit approved publish") {
			clickPrograms++
			if strings.Count(program, "await click(selector") != 1 || strings.Contains(program, "Default") {
				t.Fatalf("unsafe submit program: %s", program)
			}
		}
	}
	if clickPrograms != 0 {
		t.Fatalf("final submit program count=%d, programs=%d", clickPrograms, len(runner.programs))
	}
	reloaded, reloadErr := service.Get(plan.ID)
	if reloadErr != nil || len(reloaded.TargetStates) != 1 || reloaded.TargetStates[0].JobID <= 0 {
		t.Fatalf("reloaded plan=%+v err=%v", reloaded, reloadErr)
	}
	var published int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=? AND status='published'`, reloaded.TargetStates[0].JobID).Scan(&published); err != nil || published != 0 {
		t.Fatalf("publish record count=%d err=%v, want no promotion without verified capability", published, err)
	}
}

func TestFacebookScheduledManagerUsesVisiblePostCopyAsExactMarker(t *testing.T) {
	target := publishplan.TargetSnapshot{
		PlatformCode: "facebook",
		PublishAt:    "2030-08-15T11:48:00Z",
		Title:        "仅供本地内容库使用的标题",
		Caption:      "",
		Body:         "FB-SCHEDULE-EXACT-20300815\n正文第二行",
	}
	if got := publishManagerMarker(target); got != "FB-SCHEDULE-EXACT-20300815" {
		t.Fatalf("facebook manager marker=%q", got)
	}
}

func TestPublishPlanAPIFreezesReadsAndApprovesLocalSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("graph_text", "title", "caption", "body", `[]`, "draft")
	account, _ := database.CreateAccount("xhs", "creator", "")
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 || assets[0].AssetType != "browser_account" {
		t.Fatalf("default assets=%+v err=%v", assets, err)
	}
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("image"))
	media, _ := database.CreateMediaAsset(content.ID, "image", path, path, "image/png", 5, hex.EncodeToString(digest[:]), 100, 100, 0)

	server := &Server{db: database, mux: http.NewServeMux(), toolRuntime: &tools.Runtime{DataDir: dataDir}}
	server.routes()
	body, _ := json.Marshal(map[string]interface{}{
		"contentId": content.ID, "mediaMode": "graph_text", "mediaIds": []int64{media.ID},
		// A plan detail reuses this resolved default asset; the UI must not
		// turn it into an unsupported explicit non-Meta target selection.
		"targets": []map[string]interface{}{{"account_id": account.ID, "asset_id": assets[0].ID}},
	})
	created := httptest.NewRecorder()
	server.mux.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/publish-plans", bytes.NewReader(body)))
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var plan struct {
		ID           int64  `json:"id"`
		SnapshotHash string `json:"snapshotHash"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &plan); err != nil || plan.ID <= 0 || plan.SnapshotHash == "" || plan.Status != "frozen" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}

	listed := httptest.NewRecorder()
	server.mux.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/api/publish-plans", nil))
	if listed.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	var summaries []publishplan.PlanSummary
	if err := json.Unmarshal(listed.Body.Bytes(), &summaries); err != nil ||
		len(summaries) != 1 ||
		summaries[0].ID != plan.ID ||
		summaries[0].TargetCount != 1 ||
		len(summaries[0].PlatformCodes) != 1 ||
		summaries[0].PlatformCodes[0] != "xhs" {
		t.Fatalf("summaries=%+v err=%v", summaries, err)
	}
	for _, privateField := range [][]byte{[]byte(`"file_path"`), []byte(`"settings_json"`), []byte(`"body"`), []byte(`"snapshotHash"`)} {
		if bytes.Contains(listed.Body.Bytes(), privateField) {
			t.Fatalf("safe plan list leaked %s: %s", privateField, listed.Body.String())
		}
	}

	read := httptest.NewRecorder()
	server.mux.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/publish-plans/"+jsonNumber(plan.ID), nil))
	if read.Code != http.StatusOK || !bytes.Contains(read.Body.Bytes(), []byte(plan.SnapshotHash)) {
		t.Fatalf("read status=%d body=%s", read.Code, read.Body.String())
	}

	for _, action := range []string{"inspect", "inspect-composer", "inspect-manager", "resume-topics", "reveal-settings"} {
		response := httptest.NewRecorder()
		server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/publish-plans/"+jsonNumber(plan.ID)+"/"+action, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d body=%s", action, response.Code, response.Body.String())
		}
	}

	approveBody, _ := json.Marshal(map[string]string{"snapshotHash": plan.SnapshotHash})
	approved := httptest.NewRecorder()
	server.mux.ServeHTTP(approved, httptest.NewRequest(http.MethodPost, "/api/publish-plans/"+jsonNumber(plan.ID)+"/approve", bytes.NewReader(approveBody)))
	if approved.Code != http.StatusOK || !bytes.Contains(approved.Body.Bytes(), []byte(`"nonce"`)) {
		t.Fatalf("approve status=%d body=%s", approved.Code, approved.Body.String())
	}
	var grant struct {
		ApprovalID string `json:"approvalId"`
		Nonce      string `json:"nonce"`
	}
	if err := json.Unmarshal(approved.Body.Bytes(), &grant); err != nil || grant.ApprovalID == "" || grant.Nonce == "" {
		t.Fatalf("grant=%+v err=%v", grant, err)
	}
	server.toolRuntime.PreparePublishPlan = func(_ context.Context, gotPlanID int64, gotHash, gotApprovalID, gotNonce string) (*tools.Result, error) {
		if gotPlanID != plan.ID || gotHash != plan.SnapshotHash || gotApprovalID != grant.ApprovalID || gotNonce != grant.Nonce {
			t.Fatalf("prepare args plan=%d hash=%s approval=%s nonce=%s", gotPlanID, gotHash, gotApprovalID, gotNonce)
		}
		return &tools.Result{OK: true, Tool: "publish.plan.prepare", Data: map[string]interface{}{"execution": map[string]interface{}{"planId": plan.ID, "status": "awaiting_review"}}}, nil
	}
	prepareBody, _ := json.Marshal(map[string]string{"snapshotHash": plan.SnapshotHash, "approvalId": grant.ApprovalID, "nonce": grant.Nonce})
	prepared := httptest.NewRecorder()
	server.mux.ServeHTTP(prepared, httptest.NewRequest(http.MethodPost, "/api/publish-plans/"+jsonNumber(plan.ID)+"/prepare", bytes.NewReader(prepareBody)))
	if prepared.Code != http.StatusOK || !bytes.Contains(prepared.Body.Bytes(), []byte(`"status":"awaiting_review"`)) {
		t.Fatalf("prepare status=%d body=%s", prepared.Code, prepared.Body.String())
	}

	rejected := httptest.NewRecorder()
	server.mux.ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/api/publish-plans/"+jsonNumber(plan.ID)+"/approve", bytes.NewBufferString(`{"snapshotHash":"wrong"}`)))
	if rejected.Code != http.StatusConflict {
		t.Fatalf("wrong hash status=%d body=%s", rejected.Code, rejected.Body.String())
	}
}

func TestPublishPlanAPIRejectsUnknownFields(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	server := &Server{db: database, mux: http.NewServeMux(), toolRuntime: &tools.Runtime{DataDir: dataDir}}
	server.routes()
	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/publish-plans", bytes.NewBufferString(`{"contentId":1,"unknown":true}`)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPublishPlanAPIFreezesExplicitCompliancePolicy(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent(
		"graph_text",
		"αgamer｜阿尔法玩家，正式入驻",
		"",
		"你好，这里是 αgamer｜阿尔法玩家。\n"+publishplan.AlphaGamerAIDisclosure,
		`["αgamer","阿尔法玩家","游戏内容"]`,
		"draft",
	)
	account, _ := database.CreateAccount("xhs", "creator", "")
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("image"))
	asset, _ := database.CreateMediaAsset(content.ID, "image", path, path, "image/png", 5, hex.EncodeToString(digest[:]), 1080, 1440, 0)

	server := &Server{db: database, mux: http.NewServeMux(), toolRuntime: &tools.Runtime{DataDir: dataDir}}
	server.routes()
	body, _ := json.Marshal(map[string]interface{}{
		"contentId": content.ID, "mediaMode": "graph_text", "mediaIds": []int64{asset.ID},
		"compliancePolicy": publishplan.AlphaGamerCompliancePolicy(),
		"targets":          []map[string]interface{}{{"account_id": account.ID}},
	})
	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/publish-plans", bytes.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var plan struct {
		Snapshot struct {
			Version          int                           `json:"version"`
			CompliancePolicy *publishplan.CompliancePolicy `json:"compliance_policy"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Snapshot.Version != publishplan.PublishPlanSnapshotVersion || plan.Snapshot.CompliancePolicy == nil || !plan.Snapshot.CompliancePolicy.AIGenerated {
		t.Fatalf("snapshot=%+v", plan.Snapshot)
	}
}

func TestPublishPlanAPISubmitUsesSubmitBoundApproval(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, _ := database.CreateContent("graph_text", "title", "caption", "body", `[]`, "draft")
	account, _ := database.CreateAccount("xhs", "creator", "")
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("image"))
	media, _ := database.CreateMediaAsset(content.ID, "image", path, path, "image/png", 5, hex.EncodeToString(digest[:]), 100, 100, 0)

	server := &Server{db: database, mux: http.NewServeMux(), toolRuntime: &tools.Runtime{DataDir: dataDir}}
	server.routes()
	createBody, _ := json.Marshal(map[string]interface{}{
		"contentId": content.ID, "mediaMode": "graph_text", "mediaIds": []int64{media.ID},
		"targets": []map[string]interface{}{{"account_id": account.ID}},
	})
	created := httptest.NewRecorder()
	server.mux.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/publish-plans", bytes.NewReader(createBody)))
	var plan struct {
		ID           int64  `json:"id"`
		SnapshotHash string `json:"snapshotHash"`
	}
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	if err := json.Unmarshal(created.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}

	approveBody, _ := json.Marshal(map[string]string{"snapshotHash": plan.SnapshotHash, "action": "submit"})
	approved := httptest.NewRecorder()
	server.mux.ServeHTTP(approved, httptest.NewRequest(http.MethodPost, "/api/publish-plans/"+jsonNumber(plan.ID)+"/approve", bytes.NewReader(approveBody)))
	if approved.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approved.Code, approved.Body.String())
	}
	var grant struct {
		ApprovalID string `json:"approvalId"`
		Nonce      string `json:"nonce"`
	}
	if err := json.Unmarshal(approved.Body.Bytes(), &grant); err != nil {
		t.Fatal(err)
	}
	called := false
	server.toolRuntime.SubmitPublishPlan = func(_ context.Context, gotPlanID int64, gotHash, gotApprovalID, gotNonce string) (*tools.Result, error) {
		called = true
		if gotPlanID != plan.ID || gotHash != plan.SnapshotHash || gotApprovalID != grant.ApprovalID || gotNonce != grant.Nonce {
			t.Fatalf("submit args plan=%d hash=%s approval=%s nonce=%s", gotPlanID, gotHash, gotApprovalID, gotNonce)
		}
		return &tools.Result{OK: true, Tool: "publish.plan.submit", Data: map[string]interface{}{"execution": map[string]interface{}{"planId": plan.ID, "status": "published"}}}, nil
	}
	submitBody, _ := json.Marshal(map[string]string{"snapshotHash": plan.SnapshotHash, "approvalId": grant.ApprovalID, "nonce": grant.Nonce})
	submitted := httptest.NewRecorder()
	server.mux.ServeHTTP(submitted, httptest.NewRequest(http.MethodPost, "/api/publish-plans/"+jsonNumber(plan.ID)+"/submit", bytes.NewReader(submitBody)))
	if submitted.Code != http.StatusOK || !called || !bytes.Contains(submitted.Body.Bytes(), []byte(`"status":"published"`)) {
		t.Fatalf("submit status=%d called=%v body=%s", submitted.Code, called, submitted.Body.String())
	}
}
