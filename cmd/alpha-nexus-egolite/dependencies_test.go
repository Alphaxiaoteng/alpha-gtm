package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDefaultProductBuildDoesNotCompileLegacyChromiumRuntime(t *testing.T) {
	output, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("list default product dependencies: %v\n%s", err, output)
	}

	dependencies := strings.Fields(string(output))
	for _, dependency := range dependencies {
		if strings.HasPrefix(dependency, "github.com/chromedp/") {
			t.Fatalf("default EgoLite product still compiles legacy Chromium dependency %q", dependency)
		}
	}
}
