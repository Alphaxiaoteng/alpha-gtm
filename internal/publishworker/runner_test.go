package publishworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func baseInput() Input {
	return Input{
		SchemaVersion: 1, Action: "inspect", Platform: "csdn", CDPURL: "http://127.0.0.1:15106/",
		Content: Content{Type: "article", Title: "title", Body: "body", Tags: []string{}},
		Media:   []Media{}, Settings: map[string]interface{}{}, TimeoutMS: 30000,
	}
}

func TestValidateInputRejectsRemoteCDPAndUnapprovedSubmit(t *testing.T) {
	withoutSlash := baseInput()
	withoutSlash.CDPURL = "http://127.0.0.1:15106"
	if err := validateInput(withoutSlash); err != nil {
		t.Fatalf("equivalent loopback CDP URL rejected: %v", err)
	}
	input := baseInput()
	input.CDPURL = "https://example.com:15106/"
	if err := validateInput(input); err == nil || err.Error() != "CDP_URL_INVALID" {
		t.Fatalf("remote CDP err=%v", err)
	}
	input = baseInput()
	input.Action = "submit"
	if err := validateInput(input); err == nil || err.Error() != "SUBMIT_APPROVAL_REQUIRED" {
		t.Fatalf("submit err=%v", err)
	}
}

func TestValidateInputAllowsBoundedTenMinuteWorker(t *testing.T) {
	input := baseInput()
	input.TimeoutMS = maxWorkerTimeoutMS
	if err := validateInput(input); err != nil {
		t.Fatalf("ten-minute worker rejected: %v", err)
	}
	input.TimeoutMS = maxWorkerTimeoutMS + 1
	if err := validateInput(input); err == nil || err.Error() != "WORKER_INPUT_INVALID" {
		t.Fatalf("above maximum err=%v", err)
	}
}

func TestValidateInputAcceptsFiveHundredthCDPPort(t *testing.T) {
	input := baseInput()
	input.CDPURL = "http://127.0.0.1:15600/"
	if err := validateInput(input); err != nil {
		t.Fatalf("500th CDP port rejected: %v", err)
	}
	input.CDPURL = "http://127.0.0.1:15601/"
	if err := validateInput(input); err == nil || err.Error() != "CDP_URL_INVALID" {
		t.Fatalf("port above pool err=%v", err)
	}
}

func TestValidateInputAcceptsOnlyRFC3339PublishTime(t *testing.T) {
	input := baseInput()
	input.PublishAt = "2026-07-24T18:30:00+08:00"
	if err := validateInput(input); err != nil {
		t.Fatalf("valid publish time rejected: %v", err)
	}
	input.PublishAt = "2026-07-24 18:30"
	if err := validateInput(input); err == nil || err.Error() != "PUBLISH_TIME_INVALID" {
		t.Fatalf("invalid publish time err=%v", err)
	}
}

func TestValidateInputAllowsSettingsInspectionWithoutMedia(t *testing.T) {
	for _, action := range []string{"inspect_settings", "inspect_composer", "resume_topics"} {
		input := baseInput()
		input.Action = action
		if err := validateInput(input); err != nil {
			t.Fatalf("%s rejected: %v", action, err)
		}
	}
}

func TestValidateInputAllowsReviewedArticlePlatformSubmit(t *testing.T) {
	csdn := baseInput()
	csdn.Action, csdn.AllowSubmit = "submit", true
	if err := validateInput(csdn); err != nil {
		t.Fatalf("CSDN submit err=%v", err)
	}
	csdn.Action = "submit_prepared"
	if err := validateInput(csdn); err != nil {
		t.Fatalf("CSDN prepared submit err=%v", err)
	}
	wechatMP := baseInput()
	wechatMP.Platform, wechatMP.Action, wechatMP.AllowSubmit = "wechat_mp", "submit", true
	if err := validateInput(wechatMP); err != nil {
		t.Fatalf("WeChat MP submit err=%v", err)
	}
	wechatMP.Action = "submit_prepared"
	if err := validateInput(wechatMP); err != nil {
		t.Fatalf("WeChat MP prepared submit err=%v", err)
	}
}

func TestVerifyMediaRejectsChangedAndSymlinkFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("image"))
	media := []Media{{Path: path, Kind: "image", Role: "content", SHA256: hex.EncodeToString(digest[:])}}
	if err := verifyMedia(media); err != nil {
		t.Fatal(err)
	}
	media[0].SHA256 = strings.Repeat("0", 64)
	if err := verifyMedia(media); err == nil || err.Error() != "MEDIA_HASH_CHANGED" {
		t.Fatalf("changed err=%v", err)
	}
	link := filepath.Join(t.TempDir(), "link.png")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	media[0].Path, media[0].SHA256 = link, hex.EncodeToString(digest[:])
	if err := verifyMedia(media); err == nil || err.Error() != "MEDIA_FILE_INVALID" {
		t.Fatalf("symlink err=%v", err)
	}
}

func TestRunnerUsesSanitizedSessionEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	node := filepath.Join(dir, "fake-node")
	script := `#!/bin/sh
if [ -n "$ALPHA_NEXUS_TEST_SECRET" ] || [ "$HOME" = "` + os.Getenv("HOME") + `" ] || [ -z "$NODE_PATH" ] || [ "$ELECTRON_RUN_AS_NODE" != "1" ]; then
  printf '%s\n' '{"schemaVersion":1,"ok":false,"status":"permanent_failed","step":"env","errorCode":"ENV_NOT_SANITIZED"}'
  exit 1
fi
printf '%s\n' '{"schemaVersion":1,"ok":true,"status":"inspected","step":"inspect","errorCode":""}'
`
	if err := os.WriteFile(node, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	moduleDir := filepath.Join(dir, "node_modules")
	if err := os.MkdirAll(filepath.Join(moduleDir, "playwright"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALPHA_NEXUS_TEST_SECRET", "must-not-be-inherited")
	t.Setenv("ALPHA_NEXUS_NODE_RUN_AS_NODE", "1")
	output, err := (Runner{NodePath: node, ModuleDir: moduleDir}).Run(context.Background(), baseInput())
	if err != nil || output == nil || !output.OK || output.Status != "inspected" {
		t.Fatalf("output=%+v err=%v", output, err)
	}
}

func TestRunnerBundlesAgentOverlayBesideWorker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	node := filepath.Join(dir, "fake-node")
	script := `#!/bin/sh
overlay="$(dirname "$1")/agent_overlay.cjs"
if [ ! -f "$overlay" ]; then
  printf '%s\n' '{"schemaVersion":1,"ok":false,"status":"permanent_failed","step":"asset","errorCode":"OVERLAY_ASSET_MISSING"}'
  exit 1
fi
printf '%s\n' '{"schemaVersion":1,"ok":true,"status":"inspected","step":"inspect","errorCode":""}'
`
	if err := os.WriteFile(node, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	moduleDir := filepath.Join(dir, "node_modules")
	if err := os.MkdirAll(filepath.Join(moduleDir, "playwright"), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := (Runner{NodePath: node, ModuleDir: moduleDir}).Run(context.Background(), baseInput())
	if err != nil || output == nil || output.Status != "inspected" {
		t.Fatalf("output=%+v err=%v", output, err)
	}
}

func TestRunnerUsesSidecarProvidedNodeRuntime(t *testing.T) {
	node := filepath.Join(t.TempDir(), "electron-node")
	if err := os.WriteFile(node, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALPHA_NEXUS_NODE", node)
	got, err := (Runner{}).nodePath()
	if err != nil {
		t.Fatal(err)
	}
	if got != node {
		t.Fatalf("node path=%q want=%q", got, node)
	}
}

func TestRunnerEnforcesInputTimeoutForStalledWorker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	dir := t.TempDir()
	node := filepath.Join(dir, "stalled-node")
	if err := os.WriteFile(node, []byte("#!/bin/sh\nsleep 3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	moduleDir := filepath.Join(dir, "node_modules")
	if err := os.MkdirAll(filepath.Join(moduleDir, "playwright"), 0o700); err != nil {
		t.Fatal(err)
	}
	input := baseInput()
	input.TimeoutMS = 1000
	started := time.Now()
	_, err := (Runner{NodePath: node, ModuleDir: moduleDir}).Run(context.Background(), input)
	if err == nil || err.Error() != "WORKER_CANCELLED" {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("worker timeout was not enforced, elapsed=%s", elapsed)
	}
}

func TestErrorCodeNeverExposesUnreviewedWorkerText(t *testing.T) {
	if got := ErrorCode(fmt.Errorf("WORKER_CANCELLED")); got != "WORKER_CANCELLED" {
		t.Fatalf("code=%q", got)
	}
	if got := ErrorCode(fmt.Errorf("CDP_CONNECT_FAILED")); got != "CDP_CONNECT_FAILED" {
		t.Fatalf("reviewed CDP code=%q", got)
	}
	if got := ErrorCode(fmt.Errorf("private child process detail")); got != "" {
		t.Fatalf("unreviewed text escaped: %q", got)
	}
}
