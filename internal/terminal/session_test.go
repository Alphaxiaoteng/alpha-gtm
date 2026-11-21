package terminal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveClaudeSkipsSystemCC(t *testing.T) {
	if isSystemCCompiler("/usr/bin/cc") != true {
		t.Fatal("expected /usr/bin/cc to be treated as C compiler")
	}
	if isSystemCCompiler("/usr/bin/clang") != true {
		t.Fatal("expected clang as C compiler")
	}
	if isSystemCCompiler(filepath.Join("/Users/me/.local/bin/claude")) {
		t.Fatal("claude path should not be C compiler")
	}
}

func TestProbe(t *testing.T) {
	p := Probe()
	if p["shell"] == nil {
		t.Fatal("missing shell")
	}
}

func TestProbeOpenCodeBinaryRemainsUnavailableUntilBrokerVerification(t *testing.T) {
	bin := writeUnbrokeredOpenCodeFixture(t)
	t.Setenv("ALPHA_NEXUS_OPENCODE", bin)

	p := Probe()
	got, ok := p["opencode"].(map[string]any)
	if !ok {
		t.Fatalf("opencode probe type = %T", p["opencode"])
	}
	if got["binary"] != bin {
		t.Fatalf("binary = %v, want %q", got["binary"], bin)
	}
	if got["available"] != false || got["ready"] != false {
		t.Fatalf("unbrokered binary must not be ready/available: %#v", got)
	}
	if got["availability"] != "available_unverified" {
		t.Fatalf("availability = %v, want available_unverified", got["availability"])
	}
}

func TestBuildCommandShell(t *testing.T) {
	bin, args, label, err := buildCommand(KindShell, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bin == "" || label == "" {
		t.Fatal("empty")
	}
	if len(args) == 0 {
		t.Fatal("shell should have -il")
	}
}

func TestBuildCommandOpenCodeFailsClosed(t *testing.T) {
	bin := writeUnbrokeredOpenCodeFixture(t)
	t.Setenv("ALPHA_NEXUS_OPENCODE", bin)

	gotBin, args, label, err := buildCommand(KindOpenCode, []string{"run"})
	if !errors.Is(err, ErrOpenCodeTUIDisabled) {
		t.Fatalf("err = %v, want %v", err, ErrOpenCodeTUIDisabled)
	}
	if gotBin != "" || args != nil || label != "" {
		t.Fatalf("disabled command leaked launch metadata: bin=%q args=%v label=%q", gotBin, args, label)
	}
}

func TestManagerOpenCodeFailsBeforeFakeBinaryExec(t *testing.T) {
	bin := writeUnbrokeredOpenCodeFixture(t)
	t.Setenv("ALPHA_NEXUS_OPENCODE", bin)
	marker := filepath.Join(filepath.Dir(bin), "executed")

	dir := t.TempDir()
	m := NewManager(dir, dir)
	if session, err := m.Start(KindOpenCode, dir, nil); !errors.Is(err, ErrOpenCodeTUIDisabled) {
		if session != nil {
			_ = session.Kill()
		}
		t.Fatalf("err = %v, want %v", err, ErrOpenCodeTUIDisabled)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fake OpenCode executed before rejection: %v", err)
	}
}

func TestStartShellAndWrite(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, dir)
	s, err := m.Start(KindShell, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop(s.ID)

	// write a simple command
	_, _ = s.Write([]byte("echo alpha-nexus-term-ok\n"))
	// read some output (best effort)
	buf := make([]byte, 4096)
	n, _ := s.ReadStdout(buf)
	t.Logf("stdout sample (%d bytes): %q", n, string(buf[:n]))
}

func TestValidateWorkDirRejectsRoot(t *testing.T) {
	if err := validateWorkDir("/", t.TempDir()); err == nil {
		t.Fatal("expected reject /")
	}
	dir := t.TempDir()
	if err := validateWorkDir(dir, dir); err != nil {
		t.Fatal(err)
	}
}

func TestStopAllTerminatesSessions(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, dir)
	session, err := m.Start(KindShell, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.StopAll(); err != nil {
		t.Fatal(err)
	}
	if len(m.List()) != 0 {
		t.Fatal("sessions remain after StopAll")
	}
	if !session.closed.Load() {
		t.Fatal("session process was not terminated")
	}
}

func writeUnbrokeredOpenCodeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "opencode")
	script := "#!/bin/sh\nprintf executed > \"$(dirname \"$0\")/executed\"\nprintf fake-opencode\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}
