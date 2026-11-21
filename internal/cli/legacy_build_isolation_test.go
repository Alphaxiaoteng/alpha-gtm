package cli_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDefaultCLIBuildExcludesLegacyChromiumSourcesAndImports(t *testing.T) {
	output, err := exec.Command(
		"go", "list", "-f",
		`{{join .GoFiles "\n"}}{{"\n-- imports --\n"}}{{join .Imports "\n"}}`,
		"alpha-nexus-egolite/internal/cli",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("go list default CLI: %v\n%s", err, output)
	}
	text := string(output)
	sections := strings.SplitN(text, "\n-- imports --\n", 2)
	if len(sections) != 2 {
		t.Fatalf("unexpected go list output:\n%s", text)
	}
	defaultFiles := make(map[string]struct{})
	for _, source := range strings.Split(sections[0], "\n") {
		defaultFiles[source] = struct{}{}
	}
	for _, source := range []string{
		"browser_commands.go",
		"commands.go",
		"ops_commands.go",
		"publish_frozen_run.go",
	} {
		if _, exists := defaultFiles[source]; exists {
			t.Fatalf("default CLI still compiles legacy source %q:\n%s", source, text)
		}
	}
	for _, legacyImport := range []string{
		"alpha-nexus-egolite/internal/browser",
		"alpha-nexus-egolite/internal/fleet",
		"alpha-nexus-egolite/internal/logincheck",
		"alpha-nexus-egolite/internal/platforms/meta",
		"alpha-nexus-egolite/internal/sessionbackup",
		"alpha-nexus-egolite/internal/xhs",
	} {
		if strings.Contains(sections[1], legacyImport) {
			t.Fatalf("default CLI directly imports legacy package %q:\n%s", legacyImport, text)
		}
	}
}

func TestLegacyChromiumTagRetainsCompatibilitySources(t *testing.T) {
	output, err := exec.Command(
		"go", "list", "-tags", "legacy_chromium", "-f", `{{join .GoFiles "\n"}}`,
		"alpha-nexus-egolite/internal/cli",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("go list legacy CLI: %v\n%s", err, output)
	}
	text := string(output)
	legacyFiles := make(map[string]struct{})
	for _, source := range strings.Split(text, "\n") {
		legacyFiles[source] = struct{}{}
	}
	for _, source := range []string{
		"browser_commands.go",
		"commands.go",
		"ops_commands.go",
		"publish_frozen_run.go",
	} {
		if _, exists := legacyFiles[source]; !exists {
			t.Fatalf("legacy CLI tag lost compatibility source %q:\n%s", source, text)
		}
	}
}
