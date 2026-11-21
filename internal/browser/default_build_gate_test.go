//go:build !legacy_chromium

package browser

import (
	"go/build"
	"slices"
	"testing"
)

func TestDefaultBuildExcludesLegacyChromiumProcessControl(t *testing.T) {
	pkg, err := build.Default.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}

	for _, legacyFile := range []string{"browser.go", "facade.go", "supervisor.go"} {
		if slices.Contains(pkg.GoFiles, legacyFile) {
			t.Errorf("default build includes legacy Chromium implementation %s", legacyFile)
		}
	}
	if slices.Contains(pkg.Imports, "os/exec") {
		t.Error("default browser package must not import os/exec")
	}
}
