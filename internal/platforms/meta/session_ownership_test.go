//go:build legacy_chromium

package meta

import (
	"os"
	"strings"
	"testing"
)

func TestMetaCDPAttachmentsDoNotClosePersistentSessionTabs(t *testing.T) {
	for _, file := range []string{"publisher.go", "discover.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(source)
		if strings.Contains(text, "NewRemoteAllocator(parent,") || strings.Contains(text, "NewRemoteAllocator(ctx,") {
			t.Fatalf("%s lets caller cancellation own a persistent Meta tab", file)
		}
		for _, required := range []string{"NewRemoteAllocator(ownershipCtx,", `Target.TargetID = ""`, "context.AfterFunc("} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s is missing non-closing attachment guard %q", file, required)
			}
		}
	}
}
