package logincheck

import (
	"context"
	"strings"
	"testing"
)

func TestVerifyPassiveRejectsInvalidInputsBeforeCDP(t *testing.T) {
	if _, err := VerifyPassive(context.Background(), "xhs", 0); err == nil || !strings.Contains(err.Error(), "CDP port is required") {
		t.Fatalf("err=%v", err)
	}
	if _, err := VerifyPassive(context.Background(), "not-a-platform", 15101); err == nil || !strings.Contains(err.Error(), "platform not found") {
		t.Fatalf("err=%v", err)
	}
}
