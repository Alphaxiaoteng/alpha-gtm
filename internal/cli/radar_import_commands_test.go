package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRadarImportLegacyRequiresExplicitConfirmation(t *testing.T) {
	command := NewRootCmd()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{
		"--data-dir", t.TempDir(),
		"radar", "import-legacy",
		"--source-data-dir", t.TempDir(),
	})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "confirmation required") {
		t.Fatalf("Execute() error = %v, want confirmation required", err)
	}
}
