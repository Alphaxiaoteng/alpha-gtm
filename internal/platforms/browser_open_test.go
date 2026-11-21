//go:build legacy_chromium

package platforms

import (
	"os"
	"strings"
	"testing"
)

func TestOpenURLDetachesWithoutClosingBrowserFacadeOwnedTarget(t *testing.T) {
	source, err := os.ReadFile("browser_open.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Contains(text, "NewRemoteAllocator(ctx,") {
		t.Fatal("caller cancellation must not own and close an attached browser target")
	}
	for _, required := range []string{"NewRemoteAllocator(ownershipCtx,", `Target.TargetID = ""`, "context.AfterFunc(ctx,"} {
		if !strings.Contains(text, required) {
			t.Fatalf("browser OpenURL is missing non-closing attachment guard %q", required)
		}
	}
}
