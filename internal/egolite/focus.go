package egolite

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// FocusDesktopApp brings the already-running EgoLite app to the foreground.
// It never opens a browser, reads page data, or takes control of a Space.
func FocusDesktopApp(ctx context.Context) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("EGO_APP_FOCUS_UNSUPPORTED")
	}
	if err := exec.CommandContext(ctx, "osascript", "-e", `tell application "ego lite" to activate`).Run(); err != nil {
		return fmt.Errorf("EGO_APP_FOCUS_FAILED")
	}
	return nil
}
