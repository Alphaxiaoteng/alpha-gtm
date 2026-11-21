// Package cliresolver locates supported local Agent CLI binaries without
// loading any business or publishing packages.
package cliresolver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Claude finds Claude Code CLI. It never returns the system C compiler.
func Claude() (string, bool) {
	if path := os.Getenv("ALPHA_NEXUS_CLAUDE"); executableFile(path) {
		return path, true
	}
	candidates := make([]string, 0, 7)
	for _, name := range []string{"claude", "claude-code"} {
		if path, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, path)
		}
	}
	home, _ := os.UserHomeDir()
	candidates = append(candidates,
		filepath.Join(home, ".local/bin/claude"),
		filepath.Join(home, ".claude/bin/claude"),
		filepath.Join(home, ".hermes/node/bin/claude"),
	)
	if path, err := exec.LookPath("cc"); err == nil && !IsSystemCCompiler(path) {
		candidates = append(candidates, path)
	}
	for _, candidate := range candidates {
		if executableFile(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func executableFile(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

func IsSystemCCompiler(path string) bool {
	path = filepath.Clean(path)
	base := filepath.Base(path)
	if base == "clang" || base == "gcc" {
		return true
	}
	if path == "/usr/bin/cc" || path == "/usr/bin/clang" || path == "/usr/bin/gcc" {
		return true
	}
	if target, err := filepath.EvalSymlinks(path); err == nil {
		target = filepath.Clean(target)
		return strings.Contains(target, "clang") || target == "/usr/bin/cc"
	}
	return false
}
