package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"alpha-nexus-egolite/internal/terminal"
)

func TestCodingOpenCodeStartAndOpenRejectBeforeExec(t *testing.T) {
	bin, marker := writeFakeOpenCodeCLI(t)
	t.Setenv("ALPHA_NEXUS_OPENCODE", bin)

	previous := codingExecCommand
	execCalls := 0
	codingExecCommand = func(name string, args ...string) *exec.Cmd {
		execCalls++
		return exec.Command("/usr/bin/false")
	}
	t.Cleanup(func() { codingExecCommand = previous })

	for _, args := range [][]string{
		{"coding", "start", "--engine", "opencode"},
		{"coding", "open", "--engine", "opencode"},
	} {
		_, err := executeLocalCLI(t, args...)
		if !errors.Is(err, terminal.ErrOpenCodeTUIDisabled) {
			t.Fatalf("args=%v err=%v, want %v", args, err, terminal.ErrOpenCodeTUIDisabled)
		}
	}
	if execCalls != 0 {
		t.Fatalf("exec hook called %d time(s)", execCalls)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fake OpenCode executed before rejection: %v", err)
	}
}

func TestCodingAndAgentChatUseCentralOpenCodeRejection(t *testing.T) {
	bin, marker := writeFakeOpenCodeCLI(t)
	t.Setenv("ALPHA_NEXUS_OPENCODE", bin)
	dataDir := filepath.Join(t.TempDir(), "data")

	for _, args := range [][]string{
		{"--data-dir", dataDir, "coding", "chat", "--engine", "opencode", "--message", "hello"},
		{"--data-dir", dataDir, "agent", "chat", "--engine", "opencode", "--message", "hello"},
	} {
		_, err := executeLocalCLI(t, args...)
		if !errors.Is(err, terminal.ErrOpenCodeUnbrokeredDisabled) {
			t.Fatalf("args=%v err=%v, want %v", args, err, terminal.ErrOpenCodeUnbrokeredDisabled)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fake OpenCode executed before rejection: %v", err)
	}
}

func executeLocalCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs(args)
	err := root.Execute()
	return output.String(), err
}

func writeFakeOpenCodeCLI(t *testing.T) (bin, marker string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "opencode")
	marker = filepath.Join(dir, "executed")
	script := "#!/bin/sh\nprintf executed > \"$(dirname \"$0\")/executed\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, marker
}
