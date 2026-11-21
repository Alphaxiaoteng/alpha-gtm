//go:build darwin

package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// openCodeBridgeTestDeadline is generous on purpose: the cancellation tests run
// sandbox-exec plus a process-group kill, and the full test suite runs many
// packages in parallel. Tight deadlines made this test flaky under load.
const openCodeBridgeTestDeadline = 15 * time.Second

func TestOpenCodeBridgeUsesIsolatedSessionAndMinimalEnvironment(t *testing.T) {
	dataDir := t.TempDir()
	bin := writeFakeOpenCode(t, `
if [ "$1" = "--version" ]; then
  printf 'opencode-test 1.0\n'
  exit 0
fi
printf 'cwd=%s\n' "$PWD"
printf 'home=%s\n' "$HOME"
printf 'tmpdir=%s\n' "$TMPDIR"
printf 'run_id=%s\n' "$ALPHA_NEXUS_RUN_ID"
printf 'telemetry=%s\n' "$OPENCODE_DISABLE_TELEMETRY"
printf 'sentinel=%s\n' "${OPENCODE_TEST_PARENT_SENTINEL:-}"
if [ -e internal/agents/opencode_bridge.go ]; then
  printf 'repo_relative_visible=yes\n'
else
  printf 'repo_relative_visible=no\n'
fi
if [ -f .alpha-nexus-agent-context.md ]; then
  printf 'context_begin\n'
  /bin/cat .alpha-nexus-agent-context.md
  printf '\ncontext_end\n'
fi
`)
	t.Setenv("OPENCODE_TEST_PARENT_SENTINEL", "must-not-cross-boundary")

	res, err := OpenCodeBridge(context.Background(), SchemeRunRequest{
		Goal:     "read-only probe",
		OpenCode: bin,
		DataDir:  dataDir,
	}, "run-test-123")
	if err != nil {
		t.Fatalf("OpenCodeBridge: %v", err)
	}
	if !res.OK {
		t.Fatalf("result not OK: %s", res.Error)
	}
	if got := fmt.Sprint(res.Data["availability"]); got != "available_unverified" {
		t.Fatalf("fake binary must remain available_unverified, got %q", got)
	}
	if strings.Contains(res.Output, "must-not-cross-boundary") || strings.Contains(res.Output, "sentinel=must") {
		t.Fatal("parent-only environment value reached OpenCode")
	}
	for _, want := range []string{
		"run_id=run-test-123",
		"telemetry=1",
		"repo_relative_visible=no",
		"context_begin",
	} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("missing isolated wrapper evidence %q", want)
		}
	}
	for _, forbidden := range []string{
		dataDir,
		"SQLite",
		"CDP",
		"browser start",
		"publish",
	} {
		if strings.Contains(res.Output, forbidden) {
			t.Fatalf("OpenCode context/output exposed forbidden value %q", forbidden)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, ".alpha-nexus-agent-context.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("context must not be written into data dir: %v", statErr)
	}

	workdir := outputValue(res.Output, "cwd")
	home := outputValue(res.Output, "home")
	tmpDir := outputValue(res.Output, "tmpdir")
	if workdir == "" || home == "" || tmpDir == "" {
		t.Fatalf("missing isolated paths: workdir=%q home=%q tmpdir=%q", workdir, home, tmpDir)
	}
	if workdir == dataDir || !pathInside(workdir, home) || !pathInside(workdir, tmpDir) {
		t.Fatalf("paths are not isolated under broker session: workdir=%q home=%q tmpdir=%q", workdir, home, tmpDir)
	}
	if _, statErr := os.Stat(workdir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("ephemeral session root was not cleaned: %v", statErr)
	}
}

func TestOpenCodeBridgeVersionProbeDoesNotInheritParentEnvironment(t *testing.T) {
	bin := writeFakeOpenCode(t, `
if [ "$1" = "--version" ]; then
  if [ -n "${OPENCODE_TEST_PARENT_SENTINEL:-}" ]; then
    printf 'leaked-parent-env\n'
  else
    printf 'opencode-test 1.0\n'
  fi
  exit 0
fi
printf 'ok\n'
`)
	t.Setenv("OPENCODE_TEST_PARENT_SENTINEL", "must-not-cross-boundary")

	res, err := OpenCodeBridge(context.Background(), SchemeRunRequest{
		DryRun:   true,
		Goal:     "probe",
		OpenCode: bin,
	}, "run-version-test")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(res.Data["version"]); got != "opencode-test 1.0" {
		t.Fatalf("version probe inherited parent environment: %q", got)
	}
	if got := fmt.Sprint(res.Data["availability"]); got != "available_unverified" {
		t.Fatalf("probe must not claim verified integration, got %q", got)
	}
}

func TestOpenCodeBridgeNativeSandboxRejectsAbsoluteSensitivePaths(t *testing.T) {
	repoRoot := findGoModuleRoot(".")
	if repoRoot == "" {
		t.Fatal("repository root not found")
	}
	repoFile := filepath.Join(repoRoot, "internal", "agents", "opencode_bridge.go")
	dataDir := t.TempDir()
	dataMarker := filepath.Join(dataDir, "private-marker.txt")
	if err := os.WriteFile(dataMarker, []byte("not-for-agent"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideWrite := filepath.Join(t.TempDir(), "must-not-write.txt")
	bin := writeFakeOpenCode(t, fmt.Sprintf(`
if [ "$1" = "--version" ]; then
  printf 'opencode-test 1.0\n'
  exit 0
fi
if /bin/cat %s >/dev/null 2>&1; then
  printf 'repo_absolute_read=yes\n'
else
  printf 'repo_absolute_read=no\n'
fi
if /bin/cat %s >/dev/null 2>&1; then
  printf 'data_absolute_read=yes\n'
else
  printf 'data_absolute_read=no\n'
fi
if /bin/ln -s %s repo-link >/dev/null 2>&1 && /bin/cat repo-link/internal/agents/opencode_bridge.go >/dev/null 2>&1; then
  printf 'repo_symlink_read=yes\n'
else
  printf 'repo_symlink_read=no\n'
fi
if printf 'escape' > %s 2>/dev/null; then
  printf 'outside_write=yes\n'
else
  printf 'outside_write=no\n'
fi
`, shellSingleQuote(repoFile), shellSingleQuote(dataMarker), shellSingleQuote(repoRoot), shellSingleQuote(outsideWrite)))

	res, err := OpenCodeBridge(context.Background(), SchemeRunRequest{
		Goal:     "filesystem sandbox probe",
		OpenCode: bin,
		DataDir:  dataDir,
	}, "run-native-sandbox-test")
	if err != nil {
		t.Fatalf("OpenCodeBridge: %v", err)
	}
	for _, want := range []string{
		"repo_absolute_read=no",
		"data_absolute_read=no",
		"repo_symlink_read=no",
		"outside_write=no",
	} {
		if !strings.Contains(res.Output, want) {
			t.Fatalf("missing native sandbox evidence %q", want)
		}
	}
	if _, statErr := os.Stat(outsideWrite); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("sandbox allowed an external write: %v", statErr)
	}
}

func TestValidateOpenCodeSessionRootRejectsEmptyAndEscapingSymlink(t *testing.T) {
	base := t.TempDir()
	if _, err := validateOpenCodeSessionRoot("", base); err == nil {
		t.Fatal("empty workdir must fail closed")
	}
	outside := t.TempDir()
	link := filepath.Join(base, openCodeSessionPrefix+"escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := validateOpenCodeSessionRoot(link, base); err == nil {
		t.Fatal("symlink workdir must fail closed")
	}
}

func TestMinimalOpenCodeEnvironmentHasExactAllowlist(t *testing.T) {
	session := &openCodeSession{
		home: filepath.Join(t.TempDir(), "home"),
		tmp:  filepath.Join(t.TempDir(), "tmp"),
	}
	env := minimalOpenCodeEnv("/usr/local/bin/opencode", session, "run-id")
	want := map[string]bool{
		"PATH":                       true,
		"LANG":                       true,
		"LC_ALL":                     true,
		"HOME":                       true,
		"TMPDIR":                     true,
		"ALPHA_NEXUS_RUN_ID":         true,
		"OPENCODE_DISABLE_TELEMETRY": true,
		"DO_NOT_TRACK":               true,
	}
	got := make(map[string]bool, len(env))
	for _, pair := range env {
		name, _, ok := strings.Cut(pair, "=")
		if !ok || name == "" {
			t.Fatalf("malformed environment entry %q", pair)
		}
		got[name] = true
	}
	if len(got) != len(want) {
		t.Fatalf("environment names = %v, want exact allowlist %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Fatalf("missing environment entry %s", name)
		}
	}
}

func TestOpenCodeBridgeCancellationKillsProcessGroup(t *testing.T) {
	session, err := newOpenCodeSession("")
	if err != nil {
		t.Fatal(err)
	}
	defer session.close()
	pidFile := filepath.Join(session.root, "child.pid")
	bin := writeFakeOpenCode(t, fmt.Sprintf(`
/bin/sleep 60 &
child=$!
printf '%%s\n' "$child" > %s
wait "$child"
`, shellSingleQuote(pidFile)))
	bin, err = validateOpenCodeBinary(bin, session.forbidden)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	type commandResult struct {
		stdout string
		stderr string
		err    error
	}
	done := make(chan commandResult, 1)
	go func() {
		stdout, stderr, err := runOpenCodeCommand(ctx, bin, []string{"run", "wait"}, session, "run-cancel-test")
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		done <- commandResult{stdout: stdout, stderr: stderr, err: err}
	}()

	childPID := 0
	deadline := time.Now().Add(openCodeBridgeTestDeadline)
	for childPID == 0 && time.Now().Before(deadline) {
		select {
		case result := <-done:
			t.Fatalf("sandboxed child exited before pid capture: err=%v stdout=%q stderr=%q", result.err, result.stdout, result.stderr)
		default:
		}
		data, readErr := os.ReadFile(pidFile)
		if readErr == nil {
			childPID, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("parse child pid: %v", err)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatalf("child pid file was not created: %s", pidFile)
	}
	defer func() { _ = syscall.Kill(childPID, syscall.SIGKILL) }()
	cancel()

	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("want context cancellation, got %v", result.err)
		}
	case <-time.After(openCodeBridgeTestDeadline):
		t.Fatal("OpenCodeBridge did not stop after cancellation")
	}

	deadline = time.Now().Add(openCodeBridgeTestDeadline)
	for time.Now().Before(deadline) {
		err := syscall.Kill(childPID, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child process %d survived cancellation", childPID)
}

func writeFakeOpenCode(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode-fake")
	script := "#!/bin/sh\nset -eu\n" + strings.TrimSpace(body) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func outputValue(output, key string) string {
	prefix := key + "="
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}

func pathInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func waitForPIDFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr != nil {
				t.Fatalf("parse child pid: %v", convErr)
			}
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child pid file was not created: %s", path)
	return 0
}
