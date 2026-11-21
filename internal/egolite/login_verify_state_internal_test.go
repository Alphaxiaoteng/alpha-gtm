package egolite

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

// loginStateRecordingRunner captures generated programs and replays canned
// payloads so the caller can execute the captured program under node with
// page-level stubs.
type loginStateRecordingRunner struct {
	db             *db.DB
	programs       []string
	verifyPayload  map[string]any
	sessionPayload map[string]any
}

func (runner *loginStateRecordingRunner) BindProgramRunnerDB(database *db.DB) {
	runner.db = database
}

func (runner *loginStateRecordingRunner) Run(_ context.Context, program string, result any) error {
	runner.programs = append(runner.programs, program)
	var payload any
	switch {
	case strings.Contains(program, "const profileResult = await listProfiles()"):
		payload = map[string]any{
			"id": 321, "taskId": "task-321", "name": "alpha-nexus/matrix-login-page",
			"ownership": "agent", "done": true,
		}
	case strings.Contains(program, "const profiles = []"):
		if runner.verifyPayload == nil {
			return errors.New("EGO_TEST_VERIFY_PAYLOAD_MISSING")
		}
		payload = runner.verifyPayload
	case strings.Contains(program, "let sessionCheck ="):
		if runner.sessionPayload == nil {
			return errors.New("EGO_TEST_SESSION_PAYLOAD_MISSING")
		}
		payload = runner.sessionPayload
	default:
		return errors.New("EGO_TEST_UNEXPECTED_PROGRAM")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, result)
}

// A dead platform session that lands on the platform's login page must be
// reported as logged_out: douyin's QR login page itself contains 扫码验证/验证码
// copy, so challenge text must never outrank the explicit credential surface.
func TestLoginVerifyProgramClassifiesLoginPageAsLoggedOut(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.CreateEgoIdentity("登录页判定浏览器")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateAccountForIdentity(identity.ID, "douyin", "埃尔法玩家", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 321, "task-321", "alpha-nexus/matrix-login-page"); err != nil {
		t.Fatal(err)
	}
	runner := &loginStateRecordingRunner{
		verifyPayload: map[string]any{
			"profiles": []map[string]any{{
				"platformCode": "douyin", "authState": "logged_out",
				"pageOrigin": "https://www.douyin.com/login",
			}},
			"controlAction": "handoff", "controlAttempted": true, "controlDone": true,
		},
		sessionPayload: map[string]any{"authState": "unknown"},
	}
	service := NewSpaceService(database, runner)
	if _, err := service.VerifyLoginBatch(context.Background(), identity.ID, []string{"douyin"}); err != nil {
		t.Fatal(err)
	}
	var verifyProgram string
	for _, program := range runner.programs {
		if strings.Contains(program, "const profiles = []") {
			verifyProgram = program
			break
		}
	}
	if verifyProgram == "" {
		t.Fatal("login verify program was never generated")
	}
	// Both the verify program and the background session-check program must
	// classify an explicit login page before any challenge text hint.
	if _, err := service.CheckAccountSession(context.Background(), db.Account{ID: 1, IdentityID: identity.ID, PlatformCode: "douyin"}); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, program := range runner.programs {
		authStateIndex := strings.Index(program, "const authState =")
		if authStateIndex < 0 {
			continue
		}
		checked++
		chain := program[authStateIndex:]
		explicit := strings.Index(chain, `explicitLogin ? "logged_out"`)
		challenge := strings.Index(chain, `challenge ? "manual_takeover"`)
		if explicit < 0 || challenge < 0 || explicit > challenge {
			t.Fatalf("authState chain must test explicitLogin before challenge hints: %s", chain[:min(len(chain), 240)])
		}
	}
	if checked != 2 {
		t.Fatalf("checked authState chains=%d, want 2 (verify + session check)", checked)
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the generated EgoLite program")
	}
	prelude := `
const takeOverTaskSpace = async () => ({ done: true, ownership: "agentDelegatedToUser" })
const listTabs = async () => []
const openOrReuseTab = async () => ({})
const switchTab = async () => ({})
const pageInfo = async () => ({ url: "https://www.douyin.com/login" })
const js = async (source) => {
  if (String(source).includes("hasVisiblePassword")) {
    return {
      name: "", identifier: "", avatar: "", avatarClip: null,
      wechatMPName: "", csdnProfileHref: "", douyinName: "",
      youtubeAvatar: "", youtubeAvatarClip: null, wechatMPAvatar: "", wechatMPAvatarClip: null,
      metaAssetCandidates: [],
      bodyText: "扫码登录\n扫码验证\n验证码\n登录\n注册",
      hasVisiblePassword: false, hasVisibleLoginControl: true, hasVisibleQR: true
    }
  }
  return {}
}
const cdp = async () => ({ data: "" })
const snapshotText = async () => ""
const waitForNetworkIdle = async () => ({})
let handedOff = false
let completed = false
const handOffTaskSpace = async () => { handedOff = true; return { done: true } }
const completeTaskSpace = async () => { completed = true; return { done: true } }
const cliLog = (value) => console.log(value)
`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(prelude + "\n" + verifyProgram)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated login verify program failed: %v\n%s", err, output)
	}
	var emitted struct {
		Profiles []struct {
			PlatformCode string `json:"platformCode"`
			AuthState    string `json:"authState"`
		} `json:"profiles"`
		ControlAction string `json:"controlAction"`
	}
	found := false
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.HasPrefix(line, resultPrefix) {
			continue
		}
		found = true
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, resultPrefix)), &emitted); err != nil {
			t.Fatalf("decode verify program result: %v; output=%s", err, output)
		}
	}
	if !found {
		t.Fatalf("generated login verify program emitted no result: %s", output)
	}
	if len(emitted.Profiles) != 1 || emitted.Profiles[0].PlatformCode != "douyin" {
		t.Fatalf("unexpected profiles: %+v", emitted.Profiles)
	}
	if emitted.Profiles[0].AuthState != db.AccountAuthLoggedOut {
		t.Fatalf("login page classified as %q, want logged_out (dead session must not pose as a checkpoint)", emitted.Profiles[0].AuthState)
	}
	if emitted.ControlAction != "handoff" {
		t.Fatalf("control action %q, want handoff for re-login", emitted.ControlAction)
	}
}
