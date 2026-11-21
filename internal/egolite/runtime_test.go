package egolite_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/egolite"
)

const testRuntimeTimeout = 10 * time.Second

func writeFakeEgoBrowser(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	path := filepath.Join(t.TempDir(), "ego-browser")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRuntimeParsesSentinelResultAndIgnoresNotices(t *testing.T) {
	command := writeFakeEgoBrowser(t, `
cat >/dev/null
printf '%s\n' '[ego-browser:notice] update available'
printf '%s\n' 'ALPHA_EGO_RESULT:{"available":true,"count":2}'
`)
	runtime := egolite.NewRuntime(command, testRuntimeTimeout)

	var result struct {
		Available bool `json:"available"`
		Count     int  `json:"count"`
	}
	if err := runtime.Run(context.Background(), "cliLog('probe')", &result); err != nil {
		t.Fatal(err)
	}
	if !result.Available || result.Count != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRuntimeFindsEgoBrowserInUserLocalBinWithSanitizedPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	home := t.TempDir()
	localBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o700); err != nil {
		t.Fatal(err)
	}
	command := filepath.Join(localBin, "ego-browser")
	if err := os.WriteFile(command, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' 'ALPHA_EGO_RESULT:{\"available\":true}'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")

	var result struct {
		Available bool `json:"available"`
	}
	if err := egolite.NewRuntime("ego-browser", testRuntimeTimeout).Run(context.Background(), "cliLog('probe')", &result); err != nil {
		t.Fatal(err)
	}
	if !result.Available {
		t.Fatalf("result = %+v", result)
	}
}

func TestRuntimeParsesSentinelWrittenByEgoBrowserToStderr(t *testing.T) {
	command := writeFakeEgoBrowser(t, `
cat >/dev/null
printf '%s\n' 'ALPHA_EGO_RESULT:{"available":true}' >&2
`)
	runtime := egolite.NewRuntime(command, testRuntimeTimeout)

	var result struct {
		Available bool `json:"available"`
	}
	if err := runtime.Run(context.Background(), "cliLog('probe')", &result); err != nil {
		t.Fatal(err)
	}
	if !result.Available {
		t.Fatalf("result = %+v", result)
	}
}

func TestRuntimeFailsClosedWithoutStructuredResult(t *testing.T) {
	command := writeFakeEgoBrowser(t, `
cat >/dev/null
printf '%s\n' 'ordinary output only'
`)
	runtime := egolite.NewRuntime(command, testRuntimeTimeout)

	var result map[string]any
	err := runtime.Run(context.Background(), "cliLog('probe')", &result)
	if err == nil || !strings.Contains(err.Error(), "EGO_OUTPUT_INVALID") {
		t.Fatalf("error = %v, want EGO_OUTPUT_INVALID", err)
	}
}

func TestRuntimeRejectsOutputThatExceedsTheBoundBeforeParsing(t *testing.T) {
	command := writeFakeEgoBrowser(t, `
cat >/dev/null
dd if=/dev/zero bs=1048577 count=1 2>/dev/null
`)
	runtime := egolite.NewRuntime(command, testRuntimeTimeout)

	var result map[string]any
	err := runtime.Run(context.Background(), "cliLog('probe')", &result)
	if err == nil || err.Error() != "EGO_OUTPUT_LIMIT" {
		t.Fatalf("error = %v, want EGO_OUTPUT_LIMIT", err)
	}
}

func TestRuntimeMapsSpaceOwnershipFailureWithoutLeakingStderr(t *testing.T) {
	command := writeFakeEgoBrowser(t, `
cat >/dev/null
printf '%s\n' 'operation denied: task space is owned by user at /private/sensitive/path' >&2
exit 1
`)
	runtime := egolite.NewRuntime(command, testRuntimeTimeout)

	var result map[string]any
	err := runtime.Run(context.Background(), "cliLog('probe')", &result)
	if err == nil || err.Error() != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("error = %v, want EGO_SPACE_USER_CONTROLLED", err)
	}
	if strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("runtime leaked stderr: %v", err)
	}
}

func TestRuntimeMapsOfficialUserControlMessages(t *testing.T) {
	for _, message := range []string{"the user is controlling this Space", "user has taken control of the Space"} {
		t.Run(message, func(t *testing.T) {
			command := writeFakeEgoBrowser(t, "cat >/dev/null\nprintf '%s\\n' '"+message+"' >&2\nexit 1")
			var result map[string]any
			err := egolite.NewRuntime(command, testRuntimeTimeout).Run(context.Background(), "cliLog('probe')", &result)
			if err == nil || err.Error() != "EGO_SPACE_USER_CONTROLLED" {
				t.Fatalf("error = %v, want EGO_SPACE_USER_CONTROLLED", err)
			}
		})
	}
}

func TestRuntimeMapsMissingBoundSpace(t *testing.T) {
	command := writeFakeEgoBrowser(t, `
cat >/dev/null
printf '%s\n' 'Error: EGO_SPACE_BINDING_MISSING' >&2
exit 1
`)
	runtime := egolite.NewRuntime(command, testRuntimeTimeout)

	var result map[string]any
	err := runtime.Run(context.Background(), "cliLog('probe')", &result)
	if err == nil || err.Error() != "EGO_SPACE_BINDING_MISSING" {
		t.Fatalf("error = %v, want EGO_SPACE_BINDING_MISSING", err)
	}
}

func TestDoctorRejectsMissingRequiredHelper(t *testing.T) {
	command := writeFakeEgoBrowser(t, `
cat >/dev/null
printf '%s\n' 'ALPHA_EGO_RESULT:{"available":false,"missing":["uploadFile"]}'
`)
	runtime := egolite.NewRuntime(command, testRuntimeTimeout)

	result, err := runtime.Doctor(context.Background())
	if err == nil || !strings.Contains(err.Error(), "EGO_CAPABILITY_MISSING") {
		t.Fatalf("error = %v, want EGO_CAPABILITY_MISSING", err)
	}
	if result == nil || len(result.Missing) != 1 || result.Missing[0] != "uploadFile" {
		t.Fatalf("doctor result = %+v", result)
	}
}

func TestRuntimeTimeoutStopsHungEgoBrowser(t *testing.T) {
	command := writeFakeEgoBrowser(t, `
cat >/dev/null
sleep 2
`)
	runtime := egolite.NewRuntime(command, 50*time.Millisecond)

	var result map[string]any
	started := time.Now()
	err := runtime.Run(context.Background(), "cliLog('probe')", &result)
	if err == nil || !strings.Contains(err.Error(), "EGO_RUNTIME_TIMEOUT") {
		t.Fatalf("error = %v, want EGO_RUNTIME_TIMEOUT", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timeout took %v", elapsed)
	}
}
