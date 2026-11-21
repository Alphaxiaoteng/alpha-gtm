// Package terminal runs interactive shell / Claude Code / OpenCode sessions
// for the in-app Terminal panel (WebSocket + xterm.js).
package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"alpha-nexus-egolite/internal/cliresolver"
)

// Kind selects which program to start.
type Kind string

const (
	KindShell    Kind = "shell"
	KindClaude   Kind = "claude" // Claude Code (user may call it "cc")
	KindOpenCode Kind = "opencode"
)

var (
	// ErrOpenCodeTUIDisabled keeps legacy terminal/TUI entry points fail-closed.
	// OpenCode may only run through the brokered agents.OpenCodeBridge path.
	ErrOpenCodeTUIDisabled = errors.New("OPENCODE_TUI_DISABLED")
	// ErrOpenCodeUnbrokeredDisabled rejects the legacy non-interactive CLI wrapper.
	ErrOpenCodeUnbrokeredDisabled = errors.New("OPENCODE_UNBROKERED_DISABLED")
)

// Session is one interactive process attached to the UI terminal.
type Session struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	CmdLine   string    `json:"cmd_line"`
	Binary    string    `json:"binary"`
	WorkDir   string    `json:"work_dir"`
	StartedAt time.Time `json:"started_at"`
	Alive     bool      `json:"alive"`

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	mu     sync.Mutex
	closed atomic.Bool
}

// Manager holds live sessions (localhost UI only).
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	seq      atomic.Uint64
	DataDir  string
	WorkDir  string // default cwd for agents (project or data dir)
}

func NewManager(dataDir, workDir string) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		DataDir:  dataDir,
		WorkDir:  workDir,
	}
}

// Probe reports whether claude / opencode / shell are available.
func Probe() map[string]any {
	claude, claudeOK := ResolveClaude()
	oc, ocOK := ResolveOpenCode()
	shell := defaultShell()
	ocAvailability := "not_found"
	ocNote := "OpenCode not found"
	if ocOK {
		ocAvailability = "available_unverified"
		ocNote = "Binary found; brokered read-only smoke unverified"
	}
	return map[string]any{
		"shell": map[string]any{
			"binary": shell, "available": shell != "",
		},
		"claude": map[string]any{
			"binary": claude, "available": claudeOK,
			"note": "Claude Code CLI (not /usr/bin/cc C compiler)",
		},
		"opencode": map[string]any{
			"binary": oc, "available": false, "ready": false,
			"availability": ocAvailability, "note": ocNote,
		},
	}
}

// Start launches a new session.
func (m *Manager) Start(kind Kind, workDir string, extraArgs []string) (*Session, error) {
	if kind == KindOpenCode {
		return nil, ErrOpenCodeTUIDisabled
	}
	if workDir == "" {
		workDir = m.WorkDir
	}
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	if st, err := os.Stat(workDir); err != nil || !st.IsDir() {
		workDir = m.DataDir
		if workDir == "" {
			home, _ := os.UserHomeDir()
			workDir = home
		}
	}

	bin, args, label, err := buildCommand(kind, extraArgs)
	if err != nil {
		return nil, err
	}

	id := randomSessionID(m.seq.Add(1))
	// Safety: refuse path traversal / unconstrained cwd
	if err := validateWorkDir(workDir, m.DataDir); err != nil {
		return nil, err
	}
	sess, err := spawn(id, kind, bin, args, workDir, label)
	if err != nil {
		return nil, err
	}
	// ensure metadata even if spawn path mutates
	sess.Binary = bin
	sess.CmdLine = label
	sess.Kind = kind
	sess.WorkDir = workDir

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	// reap when process exits
	go func() {
		_ = sess.cmd.Wait()
		sess.closed.Store(true)
		sess.Alive = false
		_ = sess.stdout.Close()
		_ = sess.stdin.Close()
	}()

	return sess, nil
}

func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	s := m.sessions[id]
	if s != nil {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("session not found")
	}
	return s.Kill()
}

// StopAll terminates every managed process group and removes all live sessions.
func (m *Manager) StopAll() error {
	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for id, session := range m.sessions {
		sessions = append(sessions, session)
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	var errs []error
	for _, session := range sessions {
		if err := session.Kill(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func randomSessionID(n uint64) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("t%d-%s", n, hex.EncodeToString(b[:]))
}

// validateWorkDir keeps terminal cwd inside home, data dir, or /tmp (no arbitrary system roots).
func validateWorkDir(workDir, dataDir string) error {
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return fmt.Errorf("work_dir: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return fmt.Errorf("work_dir not a directory")
	}
	home, _ := os.UserHomeDir()
	allowed := []string{home, dataDir, os.TempDir()}
	// also allow project cwd if under home
	for _, root := range allowed {
		if root == "" {
			continue
		}
		rabs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if abs == rabs || strings.HasPrefix(abs, rabs+string(os.PathSeparator)) {
			return nil
		}
	}
	return fmt.Errorf("work_dir not allowed (must be under home, data dir, or temp)")
}

// Write sends bytes to session stdin.
func (s *Session) Write(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, fmt.Errorf("session closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stdin.Write(p)
}

// ReadStdout reads from process output (blocking).
func (s *Session) ReadStdout(p []byte) (int, error) {
	return s.stdout.Read(p)
}

// Kill terminates the process tree (process group when available).
func (s *Session) Kill() error {
	if s.closed.Swap(true) {
		return nil
	}
	s.Alive = false
	var killErr error
	if s.cmd != nil && s.cmd.Process != nil {
		killErr = killProcessGroup(s.cmd.Process)
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.stdout != nil {
		_ = s.stdout.Close()
	}
	if killErr != nil {
		return fmt.Errorf("kill terminal session %s: %w", s.ID, killErr)
	}
	return nil
}

func buildCommand(kind Kind, extra []string) (string, []string, string, error) {
	switch kind {
	case KindShell, "":
		bin := defaultShell()
		return bin, []string{"-il"}, bin + " -il", nil
	case KindClaude:
		bin, ok := ResolveClaude()
		if !ok {
			return "", nil, "", fmt.Errorf("Claude Code not found (install: https://claude.ai/code — binary `claude`, not /usr/bin/cc)")
		}
		args := append([]string{}, extra...)
		return bin, args, strings.TrimSpace(bin + " " + strings.Join(args, " ")), nil
	case KindOpenCode:
		return "", nil, "", ErrOpenCodeTUIDisabled
	default:
		return "", nil, "", fmt.Errorf("unknown kind %q", kind)
	}
}

// ResolveClaude finds Claude Code CLI. Never returns system C compiler /usr/bin/cc.
func ResolveClaude() (string, bool) {
	return cliresolver.Claude()
}

func isSystemCCompiler(path string) bool {
	return cliresolver.IsSystemCCompiler(path)
}

// ResolveOpenCode finds opencode CLI.
func ResolveOpenCode() (string, bool) {
	if p := os.Getenv("ALPHA_NEXUS_OPENCODE"); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	if p, err := exec.LookPath("opencode"); err == nil {
		return p, true
	}
	home, _ := os.UserHomeDir()
	for _, c := range []string{
		filepath.Join(home, ".local/bin/opencode"),
		filepath.Join(home, ".opencode/bin/opencode"),
		filepath.Join(home, ".hermes/node/bin/opencode"),
	} {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, true
		}
	}
	return "", false
}

func defaultShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if p, err := exec.LookPath("zsh"); err == nil {
		return p
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return "/bin/sh"
}
