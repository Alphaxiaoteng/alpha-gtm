//go:build !darwin

package agenthub

import "os/exec"

// ponytail: the first release is macOS-only. Other platforms retain Go's
// CommandContext cancellation until their process-group semantics are tested.
func configureProcess(_ *exec.Cmd) {}
