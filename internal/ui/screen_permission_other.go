//go:build !darwin || !cgo

package ui

// Without CoreGraphics there is no way to ask macOS for screen recording, so the
// caller falls back to opening System Settings.
func screenCaptureGranted() bool {
	return false
}

func requestScreenCaptureGrant() bool {
	return false
}

func screenPreflightAvailable() bool {
	return false
}
