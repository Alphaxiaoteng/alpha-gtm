//go:build !darwin && legacy_chromium

package browser

// FocusByPID 非 macOS 平台的 no-op 实现。
// ponytail: 只有 macOS 有实际实现。升级路径：Linux 用 xdotool，Windows 用 SetForegroundWindow。
func FocusByPID(pid int) error {
	return nil
}

// ResizeAndMoveByPID 非 macOS 平台的 no-op 实现。
func ResizeAndMoveByPID(pid int, x, y, width, height int) error {
	return nil
}

// GetAvailableDisplays returns no preferred secondary display off macOS.
func GetAvailableDisplays() ([]DisplayBounds, error) {
	return []DisplayBounds{{X: 0, Y: 0, Width: 1920, Height: 1080}}, nil
}

// GetScreenResolution 非 macOS 平台的 no-op 实现。
func GetScreenResolution() (int, int, error) {
	return 1920, 1080, nil
}
