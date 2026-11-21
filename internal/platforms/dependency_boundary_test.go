package platforms_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDefaultPlatformsPackageDoesNotDependOnCDP(t *testing.T) {
	command := exec.Command("go", "list", "-deps", ".")
	command.Dir = "."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list default platforms dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "github.com/chromedp/chromedp" || strings.HasPrefix(dependency, "github.com/chromedp/cdproto") {
			t.Fatalf("default EgoLite platforms package includes forbidden CDP dependency %q", dependency)
		}
	}
}
