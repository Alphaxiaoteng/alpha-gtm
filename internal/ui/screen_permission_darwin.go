//go:build darwin && cgo

package ui

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
*/
import "C"

// screenCaptureGranted answers the preflight without prompting. It is the only
// way to know the real TCC state: trying `screencapture` conflates "not granted"
// with "capture failed for another reason".
func screenCaptureGranted() bool {
	return bool(C.CGPreflightScreenCaptureAccess())
}

// requestScreenCaptureGrant shows macOS's own permission prompt and registers
// this binary in System Settings → Screen Recording. macOS owns the consent;
// this call only asks, and returns false when consent is still missing.
func requestScreenCaptureGrant() bool {
	return bool(C.CGRequestScreenCaptureAccess())
}

// screenPreflightAvailable reports that the two functions above are real.
func screenPreflightAvailable() bool {
	return true
}
