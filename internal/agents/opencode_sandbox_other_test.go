//go:build !darwin

package agents

import "testing"

func TestOpenCodeSandboxFailsClosedWithoutDarwinNativeSandbox(t *testing.T) {
	if err := openCodeSandboxStatus(); err == nil {
		t.Fatal("non-Darwin OpenCode execution must fail closed without a reliable native filesystem sandbox")
	}
	if _, _, err := openCodeSandboxCommand("opencode", nil, nil); err == nil {
		t.Fatal("non-Darwin OpenCode command must not execute unsandboxed")
	}
}
