package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Live frames are polled every few seconds, so the full-resolution JPEG is the
// biggest recurring payload in the app (a 5K desktop capture is ~300 KB). The
// optional Rust filter shrinks it to a panel-sized frame (~40 KB, -87%).
//
// It is strictly an optimization: a missing binary, a decoder error or a slow
// run all return the original frame. Capture correctness never depends on it.
const (
	screenEncoderMaxWidth = "1280"
	screenEncoderQuality  = "55"
	// Below this size the base64 frame already fits a panel; re-encoding would
	// burn a process for nothing.
	screenEncoderMinBytes = 80 << 10
	screenEncoderTimeout  = 1500 * time.Millisecond
)

var (
	screenEncoderOnce sync.Once
	screenEncoderPath string
)

// screenEncoderBinary resolves the optional filter in this order: explicit env
// override, the installed app bin directory, next to the running executable,
// then PATH. The result is cached because this runs on every frame.
func screenEncoderBinary() string {
	screenEncoderOnce.Do(func() {
		if override := os.Getenv("ALPHA_SCREEN_ENCODER"); override != "" {
			if info, err := os.Stat(override); err == nil && !info.IsDir() {
				screenEncoderPath = override
				return
			}
		}
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".alpha-nexus-egolite", "bin", "screen-encode")
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				screenEncoderPath = candidate
				return
			}
		}
		if executable, err := os.Executable(); err == nil {
			candidate := filepath.Join(filepath.Dir(executable), "screen-encode")
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				screenEncoderPath = candidate
				return
			}
		}
		if found, err := exec.LookPath("screen-encode"); err == nil {
			screenEncoderPath = found
		}
	})
	return screenEncoderPath
}

// shrinkScreenFrameJPEG takes the base64 JPEG the browser produced and returns
// a smaller base64 JPEG, or the input unchanged when anything is off. It must
// stay silent: a failed optimization is not a user-visible error.
func shrinkScreenFrameJPEG(encoded string) string {
	if encoded == "" {
		return encoded
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < screenEncoderMinBytes {
		return encoded
	}
	binary := screenEncoderBinary()
	if binary == "" {
		return encoded
	}
	ctx, cancel := context.WithTimeout(context.Background(), screenEncoderTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "--max-width", screenEncoderMaxWidth, "--quality", screenEncoderQuality)
	command.Stdin = bytes.NewReader(raw)
	var output bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &output
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return encoded
	}
	shrunk := output.Bytes()
	// A JPEG starts with SOI (FF D8). Anything else means the filter is not what
	// we think it is, so keep the frame we already have.
	if len(shrunk) < 2 || shrunk[0] != 0xFF || shrunk[1] != 0xD8 {
		return encoded
	}
	if len(shrunk) >= len(raw) {
		return encoded
	}
	return base64.StdEncoding.EncodeToString(shrunk)
}
