//go:build darwin && legacy_chromium

package browser

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// Cold osascript startup can exceed 750ms on a busy Mac even when the
	// accessibility operation itself is immediate.
	appleScriptTimeout      = 2 * time.Second
	displayQueryTimeout     = 2 * time.Second
	focusAppleScriptTimeout = 5 * time.Second
)

// runCommandWithTimeout keeps macOS accessibility calls from blocking a UI
// request forever when System Events is waiting for user permission.
func runCommandWithTimeout(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}

func outputCommandWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

// FocusByPID 将指定 PID 的窗口置前（macOS 实现）。
func FocusByPID(pid int) error {
	script := fmt.Sprintf(`tell application "System Events" to set frontmost of (first process whose unix id is %d) to true`, pid)
	return runCommandWithTimeout(focusAppleScriptTimeout, "osascript", "-e", script)
}

// ResizeAndMoveByPID 调整指定 PID 的窗口位置和大小（macOS 实现）。
func ResizeAndMoveByPID(pid int, x, y, width, height int) error {
	script := fmt.Sprintf(`tell application "System Events"
		tell (first process whose unix id is %d)
			if (count of windows) > 0 then
				set position of window 1 to {%d, %d}
				set size of window 1 to {%d, %d}
			end if
		end tell
	end tell`, pid, x, y, width, height)
	return runCommandWithTimeout(appleScriptTimeout, "osascript", "-e", script)
}

// GetAvailableDisplays returns NSScreen visible frames in the global top-left
// coordinate space used by Chromium window flags. NSScreen.screens[0] is the
// primary display; callers can safely fall back when this query is unavailable.
func GetAvailableDisplays() ([]DisplayBounds, error) {
	const script = `ObjC.import("AppKit");
(function () {
	const screens = $.NSScreen.screens;
	if (screens.count < 1) return "";
	const primaryHeight = Number(screens.objectAtIndex(0).frame.size.height);
	const rows = [];
	for (let index = 0; index < screens.count; index++) {
		const frame = screens.objectAtIndex(index).visibleFrame;
		rows.push([
			Math.round(Number(frame.origin.x)),
			Math.round(primaryHeight - Number(frame.origin.y) - Number(frame.size.height)),
			Math.round(Number(frame.size.width)),
			Math.round(Number(frame.size.height))
		].join(","));
	}
	return rows.join("\n");
})();`
	out, err := outputCommandWithTimeout(displayQueryTimeout, "osascript", "-l", "JavaScript", "-e", script)
	if err != nil {
		return nil, err
	}
	return parseDisplayBounds(string(out))
}

func parseDisplayBounds(raw string) ([]DisplayBounds, error) {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	displays := make([]DisplayBounds, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var display DisplayBounds
		if _, err := fmt.Sscanf(line, "%d,%d,%d,%d", &display.X, &display.Y, &display.Width, &display.Height); err != nil {
			return nil, fmt.Errorf("parse display bounds: %w", err)
		}
		if display.Width <= 0 || display.Height <= 0 {
			continue
		}
		displays = append(displays, display)
	}
	if len(displays) == 0 {
		return nil, fmt.Errorf("no usable displays")
	}
	return displays, nil
}

// GetScreenResolution 获取 macOS 主显示器屏幕分辨率。
func GetScreenResolution() (int, int, error) {
	out, err := outputCommandWithTimeout(appleScriptTimeout, "osascript", "-e", `tell application "Finder" to get bounds of window of desktop`)
	if err != nil {
		return 1920, 1080, err // fallback
	}
	// 返回格式为 "0, 0, 1920, 1080" 或 "{0, 0, 1920, 1080}"
	var x1, y1, x2, y2 int
	s := string(out)
	// 去除换行和空格
	s = strings.TrimSpace(s)
	// 尝试解析
	_, err = fmt.Sscanf(s, "%d, %d, %d, %d", &x1, &y1, &x2, &y2)
	if err != nil {
		_, err = fmt.Sscanf(s, "{%d, %d, %d, %d}", &x1, &y1, &x2, &y2)
	}
	if err != nil {
		return 1920, 1080, nil // fallback
	}
	w := x2 - x1
	h := y2 - y1
	if w <= 0 || h <= 0 {
		return 1920, 1080, nil
	}
	return w, h, nil
}
