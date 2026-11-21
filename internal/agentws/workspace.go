// Package agentws is the sandboxed agent workspace.
// Agents may read/write automation scripts here — never the Alpha-Nexus app source tree.
package agentws

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DirName under data dir
	DirName = "agent-workspace"
	// Automations subfolder — preferred for scripts the agent may edit
	AutomationsName = "automations"
	// WorksName — batch publish materials (videos, captions, titles)
	WorksName = "works"
)

// Path returns ~/.alpha-nexus/agent-workspace (or $ALPHA_NEXUS_DATA_DIR/...).
func Path(dataDir string) string {
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".alpha-nexus")
	}
	return filepath.Join(dataDir, DirName)
}

// AutomationsPath is workspace/automations.
func AutomationsPath(dataDir string) string {
	return filepath.Join(Path(dataDir), AutomationsName)
}

// WorksPath is workspace/works — batch content / media for publish automation.
func WorksPath(dataDir string) string {
	return filepath.Join(Path(dataDir), WorksName)
}

// Ensure creates workspace + automations + works + README. Safe to call repeatedly.
func Ensure(dataDir string) (string, error) {
	root := Path(dataDir)
	auto := AutomationsPath(dataDir)
	works := WorksPath(dataDir)
	for _, d := range []string{root, auto, works, filepath.Join(works, "_inbox"), filepath.Join(root, "notes"), filepath.Join(root, "exports"), filepath.Join(root, "sessions")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	readme := filepath.Join(root, "README.md")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		_ = os.WriteFile(readme, []byte(defaultReadme), 0o644)
	}
	gitignore := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(gitignore); os.IsNotExist(err) {
		_ = os.WriteFile(gitignore, []byte("exports/\n*.log\n.tmp/\n"), 0o644)
	}
	// sample automation stub
	sample := filepath.Join(auto, "README.md")
	if _, err := os.Stat(sample); os.IsNotExist(err) {
		_ = os.WriteFile(sample, []byte(autoReadme), 0o644)
	}
	return root, nil
}

// IsInside reports whether path is under the workspace root.
func IsInside(dataDir, target string) bool {
	root, err := filepath.Abs(Path(dataDir))
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	return abs == root || strings.HasPrefix(abs, root+string(os.PathSeparator))
}

// ResolveWorkDir returns workspace path if empty/invalid; rejects paths outside sandbox.
func ResolveWorkDir(dataDir, want string) (string, error) {
	root, err := Ensure(dataDir)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(want) == "" {
		return root, nil
	}
	abs, err := filepath.Abs(want)
	if err != nil {
		return "", err
	}
	if !IsInside(dataDir, abs) {
		return "", fmt.Errorf("AGENT_WORKSPACE_ONLY: path %q is outside sandbox %q (app source is forbidden)", abs, root)
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("work_dir not a directory: %s", abs)
	}
	return abs, nil
}

const defaultReadme = `# Alpha-Nexus Agent Workspace

This folder is the **only** directory Coding/MCP agents may freely edit.

## Allowed
- Automation scripts under automations/
- Notes, exports, drafts for social ops
- MCP tool configs and experiment files

## Forbidden
- Alpha-Nexus application source (internal/, ui-web/, …)
- System paths outside this sandbox

## CLI

    alpha-nexus-egolite agent workspace path
    alpha-nexus-egolite agent chat --message "list tools"
    alpha-nexus-egolite browser ai-chat --message "hello" --url https://chatgpt.com
`

const autoReadme = `# Automations

Put browser macros, publish helpers, and one-off scripts **here**.
Agents may refine these files; they must not rewrite the main app.
`
