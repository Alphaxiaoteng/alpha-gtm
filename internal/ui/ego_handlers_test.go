package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
)

type egoBookmarkImporter struct{ calls int }

type egoBookmarkSelectionImporter struct {
	calls    int
	requests []egolite.BookmarkImportRequest
}

func (importer *egoBookmarkSelectionImporter) Import(_ context.Context, request egolite.BookmarkImportRequest) (string, error) {
	importer.calls++
	importer.requests = append(importer.requests, request)
	return "automatic", nil
}

func TestEgoLiteProfileAvatarPathUsesOfficialProfileSelection(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Avatars"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"avatar_origami_cat.png", "avatar_origami_dragon.png", "avatar_origami_panda.png"} {
		if err := os.WriteFile(filepath.Join(root, "Avatars", name), []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	localState := `{"profile":{"info_cache":{"Profile 2":{"name":"浏览器1","avatar_icon":"chrome://theme/IDR_PROFILE_AVATAR_27"},"Profile 5":{"name":"浏览器2","avatar_icon":"chrome://theme/IDR_PROFILE_AVATAR_29"},"Profile 7":{"name":"浏览器3","avatar_icon":"chrome://theme/IDR_PROFILE_AVATAR_33"}}}}`
	if err := os.WriteFile(filepath.Join(root, "Local State"), []byte(localState), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := map[string]string{
		"浏览器1": "avatar_origami_cat.png",
		"浏览器2": "avatar_origami_dragon.png",
		"浏览器3": "avatar_origami_panda.png",
	}
	for identityName, filename := range tests {
		got, err := egoLiteProfileAvatarPath(root, identityName)
		if err != nil {
			t.Fatalf("%s: %v", identityName, err)
		}
		if got != filepath.Join(root, "Avatars", filename) {
			t.Fatalf("%s avatar=%q want %q", identityName, got, filename)
		}
	}
}

func (importer *egoBookmarkImporter) Import(_ context.Context, request egolite.BookmarkImportRequest) (string, error) {
	importer.calls++
	_, err := os.Stat(request.Path)
	return "automatic", err
}

type egoHandlerRunner struct {
	programs       []string
	openedURLs     []string
	boundSpace     map[string]any
	prepareResults []map[string]any
	publishInspect map[string]any
	publishDelete  map[string]any
	loginQRPayload string
}

func (runner *egoHandlerRunner) Run(_ context.Context, program string, result any) error {
	runner.programs = append(runner.programs, program)
	for _, match := range regexp.MustCompile(`openOrReuseTab\(("(?:\\.|[^"])*")`).FindAllStringSubmatch(program, -1) {
		var openedURL string
		if json.Unmarshal([]byte(match[1]), &openedURL) == nil {
			runner.openedURLs = append(runner.openedURLs, openedURL)
		}
	}
	payload := `{"id":701,"taskId":"task-701","name":"identity-2/account-1/xhs","ownership":"agent","done":true}`
	if strings.Contains(program, "const profiles = []") {
		profiles := []map[string]any{{
			"platformCode": "douyin", "authState": "logged_in", "name": "抖音品牌号",
			"identifier": "dy-88", "pageOrigin": "https://creator.douyin.com/",
		}}
		controlAction := "complete"
		for _, profile := range profiles {
			if state, _ := profile["authState"].(string); state == "logged_out" || state == "manual_takeover" {
				controlAction = "handoff"
				break
			}
		}
		encoded, _ := json.Marshal(map[string]any{
			"profiles": profiles, "controlAction": controlAction,
			"controlAttempted": true, "controlDone": true,
		})
		payload = string(encoded)
	}
	if strings.Contains(program, "const loginQR =") {
		payload = runner.loginQRPayload
		if payload == "" {
			payload = `{"state":"qr_ready","imageBase64":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB","pageOrigin":"https://creator.douyin.com/"}`
		}
	}
	if strings.Contains(program, "const boundSpace =") && runner.boundSpace != nil {
		raw, _ := json.Marshal(runner.boundSpace)
		payload = string(raw)
	}
	if strings.Contains(program, "takeOverTaskSpace") {
		if !strings.Contains(program, "const profiles = []") && !strings.Contains(program, "const loginQR =") {
			if strings.Contains(program, "const claimedSpaces =") {
				payload = `{"id":701,"taskId":"task-701","name":"identity-2/account-1/xhs","ownership":"agent","done":true}`
			} else {
				payload = `{"done":true,"ownership":"agent"}`
			}
		}
	}
	if strings.Contains(program, "const preparationResults = []") {
		results := runner.prepareResults
		if len(results) == 0 {
			platform := "douyin"
			if strings.Contains(program, `"platformCode":"csdn"`) {
				platform = "csdn"
			}
			results = []map[string]any{{"platformCode": platform, "status": "awaiting_review"}}
		}
		encoded, _ := json.Marshal(map[string]any{
			"done": true, "completionAttempted": true, "completionDone": true, "results": results,
		})
		payload = string(encoded)
	}
	if strings.Contains(program, "const submitResults = []") {
		payload = `{"done":true,"completionAttempted":true,"completionDone":true,"results":[{"platformCode":"douyin","status":"result_unknown","errorCode":"EGO_SUBMIT_RESULT_UNKNOWN","clicked":true}]}`
	}
	if strings.Contains(program, "const managerInspection =") && runner.publishInspect != nil {
		raw, _ := json.Marshal(runner.publishInspect)
		payload = string(raw)
	}
	if strings.Contains(program, "let deletionResult =") && runner.publishDelete != nil {
		raw, _ := json.Marshal(runner.publishDelete)
		payload = string(raw)
	}
	return json.Unmarshal([]byte(payload), result)
}

func TestEgoLoginQRHTTPIsPrivateAndNeverStored(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	runner := &egoHandlerRunner{}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)
	identity, err := server.db.CreateEgoIdentity("二维码测试浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := server.db.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/api/accounts/"+jsonNumber(account.ID)+"/login-qr",
		nil,
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), `"state":"qr_ready"`) {
		t.Fatalf("headers=%v body=%s", response.Header(), response.Body.String())
	}
	session, err := server.db.ActiveLoginSession(account.ID)
	if err != nil || session == nil || session.Status != "awaiting_login" {
		t.Fatalf("QR login did not persist a bounded verification lease: session=%+v err=%v", session, err)
	}
	if matches, err := filepath.Glob(filepath.Join(dataDir, "**", "*qr*")); err != nil || len(matches) != 0 {
		t.Fatalf("QR material was persisted: matches=%v err=%v", matches, err)
	}
	release := httptest.NewRecorder()
	server.mux.ServeHTTP(release, httptest.NewRequest(
		http.MethodPost,
		"/api/accounts/"+jsonNumber(account.ID)+"/login-qr",
		bytes.NewBufferString(`{"release":true}`),
	))
	if release.Code != http.StatusOK {
		t.Fatalf("release status=%d body=%s", release.Code, release.Body.String())
	}
	if session, err := server.db.ActiveLoginSession(account.ID); err != nil || session == nil || session.Status != "awaiting_login" {
		t.Fatalf("closing QR dialog stopped the bounded login verifier: session=%+v err=%v", session, err)
	}
}

func TestEgoFocusHTTPForegroundsEgoLiteWithoutTouchingLoginState(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	focused := false
	server.focusEgoLite = func(context.Context) error {
		focused = true
		return nil
	}

	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/ego/focus", nil))
	if response.Code != http.StatusOK || !focused || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("status=%d focused=%t body=%s", response.Code, focused, response.Body.String())
	}
	if sessions, err := server.db.ListLoginSessions(10); err != nil || len(sessions) != 0 {
		t.Fatalf("focusing EgoLite changed login state: sessions=%+v err=%v", sessions, err)
	}
}

func TestEgoLoginQRExplainsWhenTheCurrentPageHasNoQRCode(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.egoSpaces = egolite.NewSpaceService(server.db, &egoHandlerRunner{
		loginQRPayload: `{"state":"manual_takeover","pageOrigin":"https://channels.weixin.qq.com/"}`,
	})
	identity, err := server.db.CreateEgoIdentity("视频号二维码测试")
	if err != nil {
		t.Fatal(err)
	}
	account, err := server.db.CreateAccountForIdentity(identity.ID, "wechat_video", "视频号", "")
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/api/accounts/"+jsonNumber(account.ID)+"/login-qr",
		nil,
	))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"manual_takeover"`) ||
		!strings.Contains(response.Body.String(), `"reason_code":"QR_UNAVAILABLE"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEgoLoginQRAvoidsQueuingDuplicateOpenWhileAnotherLoginOperationRuns(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	runner := &egoHandlerRunner{}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)
	identity, err := server.db.CreateEgoIdentity("防重复登录测试")
	if err != nil {
		t.Fatal(err)
	}
	account, err := server.db.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}

	server.loginOperationMu.Lock()
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		server.mux.ServeHTTP(response, httptest.NewRequest(
			http.MethodPost,
			"/api/accounts/"+jsonNumber(account.ID)+"/login-qr",
			nil,
		))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		server.loginOperationMu.Unlock()
		<-done
		if response.Code != http.StatusConflict {
			t.Fatalf("duplicate request waited for the active login operation: status=%d body=%s", response.Code, response.Body.String())
		}
		return
	}
	server.loginOperationMu.Unlock()

	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"error":"EGO_LOGIN_OPERATION_IN_PROGRESS"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(runner.programs) != 0 {
		t.Fatalf("duplicate request reached EgoLite runtime: calls=%d", len(runner.programs))
	}
}

func TestEnsureActiveLoginSessionReplacesExpiredLease(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	identity, err := server.db.CreateEgoIdentity("过期登录测试")
	if err != nil {
		t.Fatal(err)
	}
	account, err := server.db.CreateAccountForIdentity(identity.ID, "wechat_mp", "微信公众号", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := server.ensureActiveLoginSession(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.Exec(`UPDATE login_sessions SET expires_at='2020-01-01T00:00:00Z' WHERE id=?`, first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := server.ensureActiveLoginSession(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.Status != "awaiting_login" {
		t.Fatalf("expired login lease was reused: first=%+v second=%+v", first, second)
	}
}

func TestEgoIdentityAndSpaceHTTPFlow(t *testing.T) {
	server, err := NewServer(config.Config{
		DataDir:           t.TempDir(),
		UIPort:            0,
		EgoBrowserCommand: "ego-browser-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	runner := &egoHandlerRunner{}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)

	createIdentity := httptest.NewRecorder()
	server.mux.ServeHTTP(createIdentity, httptest.NewRequest(
		http.MethodPost,
		"/api/ego/identities",
		bytes.NewBufferString(`{"name":"用户2"}`),
	))
	if createIdentity.Code != http.StatusCreated {
		t.Fatalf("create identity status=%d body=%s", createIdentity.Code, createIdentity.Body.String())
	}
	var identity struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(createIdentity.Body.Bytes(), &identity); err != nil {
		t.Fatal(err)
	}

	account, err := server.db.CreateAccountForIdentity(identity.ID, "xhs", "小红书主号", "")
	if err != nil {
		t.Fatal(err)
	}

	ensure := httptest.NewRecorder()
	server.mux.ServeHTTP(ensure, httptest.NewRequest(
		http.MethodPost,
		"/api/accounts/"+jsonNumber(account.ID)+"/space/ensure",
		nil,
	))
	if ensure.Code != http.StatusOK {
		t.Fatalf("ensure space status=%d body=%s", ensure.Code, ensure.Body.String())
	}

	login := httptest.NewRecorder()
	server.mux.ServeHTTP(login, httptest.NewRequest(
		http.MethodPost,
		"/api/accounts/"+jsonNumber(account.ID)+"/space/login",
		nil,
	))
	if login.Code != http.StatusOK {
		t.Fatalf("login handoff status=%d body=%s", login.Code, login.Body.String())
	}

	resume := httptest.NewRecorder()
	server.mux.ServeHTTP(resume, httptest.NewRequest(
		http.MethodPost,
		"/api/accounts/"+jsonNumber(account.ID)+"/space/resume",
		nil,
	))
	if resume.Code != http.StatusOK {
		t.Fatalf("resume status=%d body=%s", resume.Code, resume.Body.String())
	}

	spaces := httptest.NewRecorder()
	server.mux.ServeHTTP(spaces, httptest.NewRequest(http.MethodGet, "/api/ego/spaces", nil))
	if spaces.Code != http.StatusOK || !strings.Contains(spaces.Body.String(), `"state":"ready"`) {
		t.Fatalf("list spaces status=%d body=%s", spaces.Code, spaces.Body.String())
	}
	if len(runner.programs) != 4 {
		t.Fatalf("runtime calls=%d want=4 (including live Space binding validation)", len(runner.programs))
	}
}

func TestEgoIdentityRejectsUnknownFields(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/api/ego/identities",
		bytes.NewBufferString(`{"name":"用户1","unexpected":true}`),
	))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEgoIdentityRenameHTTPKeepsIdentityIDStable(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	identity, err := server.db.CreateEgoIdentity("用户1")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(
		http.MethodPatch,
		"/api/ego/identities/"+jsonNumber(identity.ID),
		bytes.NewBufferString(`{"name":" 用户一 · 国内品牌 ","sort":-2}`),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var renamed struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Sort int    `json:"sort"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &renamed); err != nil {
		t.Fatal(err)
	}
	if renamed.ID != identity.ID || renamed.Name != "用户一 · 国内品牌" || renamed.Sort != -2 {
		t.Fatalf("renamed identity=%+v", renamed)
	}
}

func TestEgoIdentityPlatformsAndLoginBatchHTTPFlow(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	runner := &egoHandlerRunner{}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)
	identity, err := server.db.CreateEgoIdentity("国内矩阵")
	if err != nil {
		t.Fatal(err)
	}

	configure := httptest.NewRecorder()
	server.mux.ServeHTTP(configure, httptest.NewRequest(
		http.MethodPut,
		"/api/ego/identities/"+jsonNumber(identity.ID)+"/platforms",
		bytes.NewBufferString(`{"platform_codes":["douyin","wechat_video","kuaishou","xhs"]}`),
	))
	if configure.Code != http.StatusOK {
		t.Fatalf("configure status=%d body=%s", configure.Code, configure.Body.String())
	}

	login := httptest.NewRecorder()
	server.mux.ServeHTTP(login, httptest.NewRequest(
		http.MethodPost,
		"/api/ego/identities/"+jsonNumber(identity.ID)+"/login-batches",
		bytes.NewBufferString(`{"platform_codes":["douyin","wechat_video","kuaishou","xhs"]}`),
	))
	if login.Code != http.StatusOK {
		t.Fatalf("login batch status=%d body=%s", login.Code, login.Body.String())
	}
	var batch egolite.LoginBatch
	if err := json.Unmarshal(login.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.PlatformCodes) != 3 || batch.State != "manual_takeover" {
		t.Fatalf("login batch=%+v", batch)
	}
	configuredAccounts, err := server.db.ListAccountsByIdentity(identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	activeSessions := 0
	for _, account := range configuredAccounts {
		if session, sessionErr := server.db.ActiveLoginSession(account.ID); sessionErr != nil {
			t.Fatal(sessionErr)
		} else if session != nil {
			activeSessions++
		}
	}
	if activeSessions != 3 {
		t.Fatalf("login batch active sessions=%d, want 3", activeSessions)
	}
	program := runner.programs[len(runner.programs)-1]
	if strings.Count(program, "openOrReuseTab(") != 3 {
		t.Fatalf("program=%s", program)
	}

	verify := httptest.NewRecorder()
	server.mux.ServeHTTP(verify, httptest.NewRequest(
		http.MethodPost,
		"/api/ego/identities/"+jsonNumber(identity.ID)+"/login-verify",
		bytes.NewBufferString(`{"platform_codes":["douyin"]}`),
	))
	if verify.Code != http.StatusOK || !strings.Contains(verify.Body.String(), `"loginState":"logged_in"`) {
		t.Fatalf("verify status=%d body=%s", verify.Code, verify.Body.String())
	}
	for _, account := range configuredAccounts {
		if account.PlatformCode != "douyin" {
			continue
		}
		if session, sessionErr := server.db.ActiveLoginSession(account.ID); sessionErr != nil || session != nil {
			t.Fatalf("verified login session remained active: session=%+v err=%v", session, sessionErr)
		}
	}
}

func TestEgoIdentityOpenHTTPReusesSpaceAndSkipsLoggedOutPlatforms(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	runner := &egoHandlerRunner{boundSpace: map[string]any{
		"id": 88, "taskId": "task-88", "name": "alpha-nexus/matrix-open", "ownership": "user",
	}}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)
	identity, err := server.db.CreateEgoIdentity("打开平台测试浏览器")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		code, state string
	}{
		{code: "douyin", state: "logged_in"},
		{code: "youtube", state: "logged_in"},
		{code: "csdn", state: "logged_out"},
	} {
		account, createErr := server.db.CreateAccountForIdentity(identity.ID, item.code, item.code, "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, updateErr := server.db.Exec(`UPDATE accounts SET login_state=? WHERE id=?`, item.state, account.ID); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	if _, err := server.db.CreateEgoSpaceForIdentity(identity.ID, 88, "task-88", "alpha-nexus/matrix-open"); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/api/ego/identities/"+jsonNumber(identity.ID)+"/open",
		bytes.NewBufferString(`{"offset":0}`),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result egolite.BrowserOpenResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Reused || result.SpaceID != 88 || result.Total != 2 || result.Remaining != 0 {
		t.Fatalf("result=%+v", result)
	}
	if len(runner.programs) != 2 || len(runner.openedURLs) != 2 {
		t.Fatalf("programs=%d urls=%v", len(runner.programs), runner.openedURLs)
	}
	for _, openedURL := range runner.openedURLs {
		if strings.Contains(openedURL, "csdn") {
			t.Fatalf("logged-out platform was opened: %v", runner.openedURLs)
		}
	}
}

func TestEgoIdentityOpenHTTPAcceptsOptionalPlatformCodes(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	runner := &egoHandlerRunner{boundSpace: map[string]any{
		"id": 90, "taskId": "task-90", "name": "alpha-nexus/matrix-mode", "ownership": "user",
	}}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)
	identity, err := server.db.CreateEgoIdentity("模式请求浏览器")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"douyin", "shopify"} {
		account, createErr := server.db.CreateAccountForIdentity(identity.ID, code, code, "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, updateErr := server.db.Exec(`UPDATE accounts SET login_state=? WHERE id=?`, "logged_in", account.ID); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	if _, err := server.db.CreateEgoSpaceForIdentity(identity.ID, 90, "task-90", "alpha-nexus/matrix-mode"); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"/api/ego/identities/"+jsonNumber(identity.ID)+"/open",
		bytes.NewBufferString(`{"offset":0,"platform_codes":["shopify"]}`),
	))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"shopify"`) || strings.Contains(response.Body.String(), `"douyin"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEgoBackgroundLoginSyncPersistsProfileAndCompletesSession(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	runner := &egoHandlerRunner{}
	server.egoSpaces = egolite.NewSpaceService(server.db, runner)
	identity, err := server.db.CreateEgoIdentity("后台同步浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := server.db.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateEgoSpaceForIdentity(identity.ID, 701, "task-701", "alpha-nexus/matrix-sync"); err != nil {
		t.Fatal(err)
	}
	session, err := server.ensureActiveLoginSession(account.ID)
	if err != nil {
		t.Fatal(err)
	}

	result, err := server.loginSync.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Verified != 1 || result.Pending != 0 {
		t.Fatalf("sync result=%+v", result)
	}
	updated, err := server.db.GetAccount(account.ID)
	if err != nil || updated.LoginState != "logged_in" || updated.Name != "抖音品牌号" || updated.AccountIdentifier != "dy-88" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	if active, activeErr := server.db.ActiveLoginSession(account.ID); activeErr != nil || active != nil {
		t.Fatalf("session %d remained active: active=%+v err=%v", session.ID, active, activeErr)
	}
}

func TestEgoBookmarkImportHTTPIsIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	account, err := server.db.CreateAccountForIdentity(1, "douyin", "书签测试账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := server.db.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	importer := &egoBookmarkImporter{}
	server.egoBookmarks = egolite.NewBookmarkService(server.db, dataDir, importer)

	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/ego/bookmarks/import", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("attempt=%d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if importer.calls != 2 {
		t.Fatalf("bookmark importer calls=%d", importer.calls)
	}
}

func TestEgoBookmarkImportHTTPDoesNotClaimDefaultWindowImportSucceeded(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	account, err := server.db.CreateAccountForIdentity(1, "douyin", "书签测试账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := server.db.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	server.egoBookmarks = egolite.NewBookmarkService(server.db, dataDir, nil)

	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/ego/bookmarks/import", nil))
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "EGO_BOOKMARK_SPACE_IMPORT_UNSUPPORTED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEgoIdentityPlatformSelectionAutomaticallySyncsMediaAndShopBookmarks(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	importer := &egoBookmarkSelectionImporter{}
	server.egoBookmarks = egolite.NewBookmarkService(server.db, dataDir, importer)

	request := httptest.NewRequest(http.MethodPut, "/api/ego/identities/1/platforms", strings.NewReader(`{"platform_codes":["douyin","taobao"]}`))
	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || importer.calls != 1 || len(importer.requests) != 1 {
		t.Fatalf("status=%d calls=%d requests=%+v", response.Code, importer.calls, importer.requests)
	}
	bookmarks := importer.requests[0].Bookmarks
	if len(bookmarks) != 2 || bookmarks[0].ToolbarTitle != "抖音" || bookmarks[1].ToolbarTitle != "淘宝" {
		t.Fatalf("bookmarks=%+v", bookmarks)
	}
}

func TestEgoSpacesEmptyListIsJSONArray(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/ego/spaces", nil))
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEgoCalibrationEndpointUpdatesSafeDefault(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.calibrateEgo = func(context.Context) (*egolite.CalibrationResult, error) {
		return &egolite.CalibrationResult{MeasuredCapacity: 6, DefaultConcurrency: 3}, nil
	}

	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/ego/concurrency/calibrate", nil))
	if response.Code != http.StatusOK || server.cfg.MaxConcurrentSpaces != 3 {
		t.Fatalf("status=%d cfg=%+v body=%s", response.Code, server.cfg, response.Body.String())
	}
}

func TestEgoSessionMonitorHTTPConfigurationAndManualRun(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	get := httptest.NewRecorder()
	server.mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/ego/session-monitor", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"interval_minutes":60`) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}

	update := httptest.NewRecorder()
	server.mux.ServeHTTP(update, httptest.NewRequest(
		http.MethodPut,
		"/api/ego/session-monitor",
		bytes.NewBufferString(`{"enabled":false,"interval_minutes":90}`),
	))
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), `"enabled":false`) {
		t.Fatalf("update status=%d body=%s", update.Code, update.Body.String())
	}

	run := httptest.NewRecorder()
	server.mux.ServeHTTP(run, httptest.NewRequest(http.MethodPost, "/api/ego/session-monitor/run", nil))
	if run.Code != http.StatusOK || !strings.Contains(run.Body.String(), `"checked":0`) {
		t.Fatalf("run status=%d body=%s", run.Code, run.Body.String())
	}
}

func TestEgoSessionMonitorSkipsRuntimeWhileLoginOperationOwnsTheSpace(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	identity, err := server.db.CreateEgoIdentity("登录中矩阵")
	if err != nil {
		t.Fatal(err)
	}
	account, err := server.db.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.db.CreateEgoSpaceForIdentity(identity.ID, 701, "task-login", "alpha-nexus/matrix-login"); err != nil {
		t.Fatal(err)
	}
	if err := server.db.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}

	server.loginOperationMu.Lock()
	result, runErr := server.sessionMonitor.Run(context.Background())
	server.loginOperationMu.Unlock()
	if runErr != nil {
		t.Fatal(runErr)
	}
	if result.Checked != 1 || result.Unknown != 1 || result.Failed != 0 {
		t.Fatalf("result=%+v", result)
	}
	updated, err := server.db.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LoginState != db.AccountAuthLoggedIn {
		t.Fatalf("busy monitor demoted reliable login state to %q", updated.LoginState)
	}
	event, err := server.db.LatestAccountLoginEvent(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || event.AuthState != db.AccountAuthUnknown || event.ReasonCode != db.LoginReasonBrowserOperationBusy || event.EvidenceCode != db.LoginEvidenceOperationLeaseBusy {
		t.Fatalf("busy observation=%+v", event)
	}
}

// The full AI image workspace stays behind AIImageEnabled. Only the minimal
// PersonAgent surface (provider config, credential, generate, reference upload,
// asset read) is always mounted, because Bot avatars must work on a default
// install — see TestAIImagePersonAgentSurfaceWorksWithoutWorkspaceFlag.
func TestAIImageWorkspaceRoutesAreHiddenByDefault(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	for _, path := range []string{
		"/api/ai-image/workspace",
		"/api/ai-image/projects",
		"/api/ai-image/generations",
	} {
		response := httptest.NewRecorder()
		server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("AI image workspace route %s must stay hidden, status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}
