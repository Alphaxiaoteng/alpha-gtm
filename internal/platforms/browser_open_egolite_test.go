//go:build !legacy_chromium

package platforms

import (
	"context"
	"strings"
	"testing"
)

func TestOpenURLFailsClosedWhenLegacyChromiumIsDisabled(t *testing.T) {
	err := OpenURL(context.Background(), "http://127.0.0.1:1", "https://example.com")
	if err == nil {
		t.Fatal("default EgoLite build must reject the legacy CDP browser path")
	}
	message := err.Error()
	if !strings.HasPrefix(message, "CDP_CONNECT:") || !strings.Contains(message, "disabled") {
		t.Fatalf("unexpected fail-closed error %q", message)
	}
}
