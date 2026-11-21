package publishplan

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDefaultBuildDoesNotCompileLegacyChromiumRuntime(t *testing.T) {
	output, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("list publishplan dependencies: %v\n%s", err, output)
	}

	forbidden := []string{
		"alpha-nexus-egolite/internal/browser",
		"alpha-nexus-egolite/internal/logincheck",
		"github.com/chromedp/cdproto",
		"github.com/chromedp/chromedp",
	}
	for _, dependency := range strings.Fields(string(output)) {
		for _, prefix := range forbidden {
			if dependency == prefix || strings.HasPrefix(dependency, prefix+"/") {
				t.Fatalf("default publishplan build still compiles legacy Chromium dependency %q", dependency)
			}
		}
	}
}
