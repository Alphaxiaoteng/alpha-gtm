package agents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const openCodeSessionPrefix = "alpha-nexus-opencode-"

type openCodeSession struct {
	root      string
	home      string
	tmp       string
	tempBase  string
	forbidden []string
}

// OpenCodeBridge runs OpenCode in a broker-created ephemeral session.
// It does not expose the application worktree, data directory, or parent env.
func OpenCodeBridge(ctx context.Context, req SchemeRunRequest, tid string) (*SchemeRunResult, error) {
	bin := ResolveOpenCodeBinary(req.OpenCode)
	if bin == "" {
		return &SchemeRunResult{
			OK:      false,
			Message: "opencode binary not found",
			Error:   "OPENCODE_NOT_FOUND",
			Data: map[string]interface{}{
				"availability": "not_found",
				"install":      "curl -fsSL https://opencode.ai/install | bash",
				"hint":         "or put opencode on PATH (~/.local/bin)",
			},
		}, fmt.Errorf("OPENCODE_NOT_FOUND")
	}

	forbidden := openCodeForbiddenRoots(req.DataDir)
	validatedBin, err := validateOpenCodeBinary(bin, forbidden)
	if err != nil {
		return &SchemeRunResult{
			OK:      false,
			Message: "opencode binary rejected by sandbox",
			Error:   "OPENCODE_BINARY_REJECTED",
			Data: map[string]interface{}{
				"availability": "available_unverified",
			},
		}, fmt.Errorf("OPENCODE_BINARY_REJECTED: %w", err)
	}
	if err := openCodeSandboxStatus(); err != nil {
		return &SchemeRunResult{
			OK:      false,
			Message: "opencode native filesystem sandbox unavailable",
			Error:   "OPENCODE_SANDBOX_UNAVAILABLE",
			Data: map[string]interface{}{
				"availability": "available_unverified",
				"sandbox":      "unavailable",
			},
		}, fmt.Errorf("OPENCODE_SANDBOX_UNAVAILABLE: %w", err)
	}

	session, err := newOpenCodeSession(req.DataDir)
	if err != nil {
		return &SchemeRunResult{
			OK:      false,
			Message: "opencode sandbox unavailable",
			Error:   "OPENCODE_SANDBOX_FAILED",
			Data: map[string]interface{}{
				"availability": "available_unverified",
			},
		}, fmt.Errorf("OPENCODE_SANDBOX_FAILED: %w", err)
	}
	defer session.close()

	ver, verErr := runOpenCodeVersion(ctx, validatedBin, session, tid)
	if verErr != nil || ver == "" {
		if verErr == nil {
			verErr = errors.New("empty version response")
		}
		return &SchemeRunResult{
			OK:      false,
			Message: "opencode binary found but --version failed",
			Error:   "OPENCODE_VERSION_UNVERIFIED",
			Data: map[string]interface{}{
				"availability": "version_mismatch",
				"version":      ver,
				"mode":         "probe",
			},
		}, fmt.Errorf("OPENCODE_VERSION_UNVERIFIED: %w", verErr)
	}

	if req.DryRun {
		return &SchemeRunResult{
			OK:      true,
			Message: "opencode wrapper probe passed; real read-only integration is unverified",
			Data: map[string]interface{}{
				"availability":     "available_unverified",
				"binary":           validatedBin,
				"version":          ver,
				"mode":             "probe",
				"session_isolated": true,
			},
		}, nil
	}

	timeout := 90 * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"run", "--format", "json", "--dir", session.root}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	args = append(args, req.Goal)

	stdout, stderr, runErr := runOpenCodeCommand(runCtx, validatedBin, args, session, tid)
	if runCtx.Err() != nil {
		runErr = runCtx.Err()
	}
	outStr := tailString(strings.TrimSpace(stdout), 8000)
	errStr := tailString(strings.TrimSpace(stderr), 4000)

	ok := runErr == nil
	msg := "opencode run finished in isolated broker session with read-only Alpha-Nexus boundary"
	if runErr != nil {
		msg = "opencode run failed"
		if strings.Contains(errStr, "SQLiteError") || strings.Contains(outStr, "SQLiteError") {
			msg = "opencode isolated local state error"
		}
	}

	return &SchemeRunResult{
		OK:      ok,
		Message: msg,
		Output:  outStr,
		Error:   firstNonEmpty(errString(runErr), extractOpenCodeError(outStr)),
		Data: map[string]interface{}{
			"availability":     "available_unverified",
			"binary":           validatedBin,
			"version":          ver,
			"workdir":          "ephemeral_session",
			"session_isolated": true,
			"stderr":           errStr,
			"timeout":          timeout.String(),
			"exit_ok":          runErr == nil,
			"trace_id":         tid,
		},
	}, runErr
}

func newOpenCodeSession(dataDir string) (_ *openCodeSession, err error) {
	tempBase, err := canonicalExistingDir(os.TempDir())
	if err != nil {
		return nil, fmt.Errorf("temporary base: %w", err)
	}
	for _, forbidden := range openCodeForbiddenRoots(dataDir) {
		if openCodePathWithin(forbidden, tempBase) {
			return nil, fmt.Errorf("temporary base overlaps forbidden root")
		}
	}

	root, err := os.MkdirTemp(tempBase, openCodeSessionPrefix)
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	root, err = validateOpenCodeSessionRoot(root, tempBase)
	if err != nil {
		return nil, err
	}
	for _, forbidden := range openCodeForbiddenRoots(dataDir) {
		if openCodePathWithin(forbidden, root) {
			return nil, fmt.Errorf("session root overlaps forbidden root")
		}
	}

	session := &openCodeSession{
		root:      root,
		home:      filepath.Join(root, "home"),
		tmp:       filepath.Join(root, "tmp"),
		tempBase:  tempBase,
		forbidden: openCodeForbiddenRoots(dataDir),
	}
	for _, dir := range []string{session.home, session.tmp} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return nil, err
		}
	}
	contextPath := filepath.Join(root, ".alpha-nexus-agent-context.md")
	if err := os.WriteFile(contextPath, []byte(openCodeContextDoc()), 0o600); err != nil {
		return nil, err
	}
	cleanup = false
	return session, nil
}

func (s *openCodeSession) close() {
	if s == nil {
		return
	}
	root, err := validateOpenCodeSessionRoot(s.root, s.tempBase)
	if err != nil || root != s.root {
		return
	}
	_ = os.RemoveAll(root)
}

func validateOpenCodeSessionRoot(root, tempBase string) (string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(tempBase) == "" {
		return "", errors.New("empty session workdir")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("session workdir must be a real directory")
	}
	if !strings.HasPrefix(filepath.Base(root), openCodeSessionPrefix) {
		return "", errors.New("session workdir was not broker-created")
	}
	resolvedRoot, err := canonicalExistingDir(root)
	if err != nil {
		return "", err
	}
	resolvedBase, err := canonicalExistingDir(tempBase)
	if err != nil {
		return "", err
	}
	if resolvedRoot == resolvedBase || !openCodePathWithin(resolvedBase, resolvedRoot) {
		return "", errors.New("session workdir escapes temporary base")
	}
	return resolvedRoot, nil
}

func validateOpenCodeBinary(bin string, forbidden []string) (string, error) {
	if strings.TrimSpace(bin) == "" {
		return "", errors.New("empty binary path")
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("binary is not an executable regular file")
	}
	for _, root := range forbidden {
		if openCodePathWithin(root, resolved) {
			return "", errors.New("binary is inside a forbidden root")
		}
	}
	return resolved, nil
}

func runOpenCodeVersion(ctx context.Context, bin string, session *openCodeSession, tid string) (string, error) {
	versionCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	stdout, stderr, err := runOpenCodeCommand(versionCtx, bin, []string{"--version"}, session, tid)
	if versionCtx.Err() != nil {
		return "", versionCtx.Err()
	}
	combined := strings.TrimSpace(stdout)
	if combined == "" {
		combined = strings.TrimSpace(stderr)
	}
	if err != nil && strings.TrimSpace(stderr) != "" {
		err = fmt.Errorf("%w: %s", err, tailString(strings.TrimSpace(stderr), 500))
	}
	return tailString(combined, 1000), err
}

func runOpenCodeCommand(ctx context.Context, bin string, args []string, session *openCodeSession, tid string) (string, string, error) {
	if session == nil {
		return "", "", errors.New("missing broker session")
	}
	workdir, err := validateOpenCodeSessionRoot(session.root, session.tempBase)
	if err != nil || workdir != session.root {
		return "", "", errors.New("invalid broker session workdir")
	}

	sandboxBin, sandboxArgs, err := openCodeSandboxCommand(bin, args, session)
	if err != nil {
		return "", "", err
	}
	cmd := exec.CommandContext(ctx, sandboxBin, sandboxArgs...)
	cmd.Dir = workdir
	cmd.Env = minimalOpenCodeEnv(bin, session, tid)
	configureOpenCodeProcess(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.String(), stderr.String(), err
}

func minimalOpenCodeEnv(bin string, session *openCodeSession, tid string) []string {
	pathDirs := []string{
		filepath.Dir(bin),
		"/usr/local/bin",
		"/opt/homebrew/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}
	seen := make(map[string]bool, len(pathDirs))
	cleanPath := make([]string, 0, len(pathDirs))
	for _, dir := range pathDirs {
		dir = filepath.Clean(dir)
		if filepath.IsAbs(dir) && !seen[dir] {
			seen[dir] = true
			cleanPath = append(cleanPath, dir)
		}
	}
	locale := safeOpenCodeLocale(os.Getenv("LC_ALL"))
	if locale == "" {
		locale = safeOpenCodeLocale(os.Getenv("LANG"))
	}
	if locale == "" {
		locale = "C.UTF-8"
	}
	return []string{
		"PATH=" + strings.Join(cleanPath, string(os.PathListSeparator)),
		"LANG=" + locale,
		"LC_ALL=" + locale,
		"HOME=" + session.home,
		"TMPDIR=" + session.tmp,
		"ALPHA_NEXUS_RUN_ID=" + safeOpenCodeRunID(tid),
		"OPENCODE_DISABLE_TELEMETRY=1",
		"DO_NOT_TRACK=1",
	}
}

func safeOpenCodeLocale(value string) string {
	if value == "" || len(value) > 64 {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_.@-", r) {
			continue
		}
		return ""
	}
	return value
}

func safeOpenCodeRunID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 128 {
		value = value[:128]
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_.:-", r) {
			return r
		}
		return '-'
	}, value)
}

func openCodeForbiddenRoots(dataDir string) []string {
	candidates := []string{dataDir}
	if cwd, err := os.Getwd(); err == nil {
		if root := findGoModuleRoot(cwd); root != "" {
			candidates = append(candidates, root)
		}
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		if root := findGoModuleRoot(filepath.Dir(file)); root != "" {
			candidates = append(candidates, root)
		}
	}
	seen := map[string]bool{}
	roots := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
			abs = resolved
		}
		abs = filepath.Clean(abs)
		if !seen[abs] {
			seen[abs] = true
			roots = append(roots, abs)
		}
	}
	return roots
}

func findGoModuleRoot(start string) string {
	current, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for {
		if info, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil && info.Mode().IsRegular() {
			if resolved, resolveErr := filepath.EvalSymlinks(current); resolveErr == nil {
				return resolved
			}
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func canonicalExistingDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("empty directory")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("not a directory")
	}
	return filepath.Clean(resolved), nil
}

func openCodePathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))))
}

func openCodeContextDoc() string {
	return `# Alpha-Nexus read-only agent context

This process runs inside an ephemeral broker session. Treat that session as the
entire accessible workspace. Do not look for or access application source,
application storage, browser debugging endpoints, profiles, cookies, or
credentials outside it.

When a trusted host explicitly configures Alpha-Nexus MCP, use only its
read-only tools.Registry projection for inspection. A successful local wrapper
probe does not mean that MCP or a real OpenCode provider has been verified.
`
}

func tailString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return "…" + value[len(value)-limit:]
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func extractOpenCodeError(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, `"type":"error"`) || strings.Contains(line, `"type": "error"`) {
			if len(line) > 500 {
				return line[:500]
			}
			return line
		}
	}
	return ""
}
