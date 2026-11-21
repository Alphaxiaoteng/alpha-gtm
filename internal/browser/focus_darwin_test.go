//go:build darwin && legacy_chromium

package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunCommandWithTimeoutStopsBlockedWindowControl(t *testing.T) {
	startedAt := time.Now()
	err := runCommandWithTimeout(30*time.Millisecond, "sh", "-c", "sleep 1")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("window command exceeded bounded timeout: %s", elapsed)
	}
}

func TestFocusByPIDAllowsColdAppleScriptStartup(t *testing.T) {
	binDir := t.TempDir()
	fakeOSAScript := filepath.Join(binDir, "osascript")
	if err := os.WriteFile(fakeOSAScript, []byte("#!/bin/sh\n/bin/sleep 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	if err := FocusByPID(os.Getpid()); err != nil {
		t.Fatalf("FocusByPID should tolerate a one-second AppleScript startup: %v", err)
	}
}

func TestResizeAndMoveByPIDDoesNotActivateBrowser(t *testing.T) {
	binDir := t.TempDir()
	scriptLog := filepath.Join(t.TempDir(), "script.txt")
	fakeOSAScript := filepath.Join(binDir, "osascript")
	script := "#!/bin/sh\n/usr/bin/printf '%s' \"$2\" > " + scriptLog + "\n"
	if err := os.WriteFile(fakeOSAScript, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	if err := ResizeAndMoveByPID(os.Getpid(), 1920, 25, 1280, 720); err != nil {
		t.Fatal(err)
	}
	recorded, err := os.ReadFile(scriptLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(recorded), "frontmost") {
		t.Fatalf("window placement activated the browser: %s", recorded)
	}
}

func TestParseDisplayBoundsPreservesExtendedDesktopCoordinates(t *testing.T) {
	displays, err := parseDisplayBounds("0,25,2560,1335\n2560,0,1920,1080\n-1440,120,1440,900\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(displays) != 3 ||
		displays[1] != (DisplayBounds{X: 2560, Y: 0, Width: 1920, Height: 1080}) ||
		displays[2] != (DisplayBounds{X: -1440, Y: 120, Width: 1440, Height: 900}) {
		t.Fatalf("displays=%+v", displays)
	}
	if _, err := parseDisplayBounds("not-display-bounds"); err == nil {
		t.Fatal("invalid display output was accepted")
	}
}
