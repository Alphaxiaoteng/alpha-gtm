package terminal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestChatRejectsUnbrokeredOpenCodeBeforeExecOrParentEnvironment(t *testing.T) {
	bin := writeUnbrokeredOpenCodeFixture(t)
	marker := filepath.Join(filepath.Dir(bin), "executed")
	t.Setenv("ALPHA_NEXUS_OPENCODE", bin)
	t.Setenv("ALPHA_NEXUS_DATA_DIR", filepath.Join(t.TempDir(), "must-not-be-created"))
	t.Setenv("OPENCODE_TEST_PARENT_SENTINEL", "must-not-reach-child")

	result, err := Chat(context.Background(), ChatRequest{
		Engine:  "opencode",
		Message: "do not execute",
	})
	if !errors.Is(err, ErrOpenCodeUnbrokeredDisabled) {
		t.Fatalf("err = %v, want %v", err, ErrOpenCodeUnbrokeredDisabled)
	}
	if result == nil || result.OK || result.Engine != "opencode" || result.Error != ErrOpenCodeUnbrokeredDisabled.Error() {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fake OpenCode executed before rejection: %v", err)
	}
}

func TestChatReturnsStableOpenCodeRejectionBeforeRequestValidation(t *testing.T) {
	result, err := Chat(context.Background(), ChatRequest{Engine: "claude"})
	if !errors.Is(err, ErrOpenCodeUnbrokeredDisabled) {
		t.Fatalf("err = %v, want %v", err, ErrOpenCodeUnbrokeredDisabled)
	}
	if result == nil || result.Engine != "opencode" || result.Error != "OPENCODE_UNBROKERED_DISABLED" {
		t.Fatalf("result = %+v", result)
	}
}
