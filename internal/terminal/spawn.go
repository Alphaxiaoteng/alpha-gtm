package terminal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// spawn starts the program under a pseudo-TTY when possible (macOS/Linux `script`),
// so Claude Code / OpenCode TUIs work. Falls back to plain pipes.
func spawn(id string, kind Kind, bin string, args []string, workDir, label string) (*Session, error) {
	cmd, stdin, stdout, err := startWithTTY(bin, args, workDir)
	if err != nil {
		return nil, err
	}
	return &Session{
		ID:        id,
		Kind:      kind,
		CmdLine:   label,
		Binary:    bin,
		WorkDir:   workDir,
		StartedAt: time.Now().UTC(),
		Alive:     true,
		cmd:       cmd,
		stdin:     stdin,
		stdout:    stdout,
	}, nil
}

func startWithTTY(bin string, args []string, workDir string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, error) {
	// Prefer script(1) for a real TTY without CGO / creack.pty
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if scriptPath, err := exec.LookPath("script"); err == nil {
			// macOS: script -q /dev/null command args...
			// Linux: script -q -c "cmd" /dev/null
			var cmd *exec.Cmd
			if runtime.GOOS == "darwin" {
				full := append([]string{"-q", "/dev/null", bin}, args...)
				cmd = exec.Command(scriptPath, full...)
			} else {
				// linux util-linux script
				joined := shellJoin(bin, args)
				cmd = exec.Command(scriptPath, "-q", "-c", joined, "/dev/null")
			}
			return startPipes(cmd, workDir, bin)
		}
	}
	// plain pipes (no TTY — still OK for line-mode CLIs)
	cmd := exec.Command(bin, args...)
	return startPipes(cmd, workDir, bin)
}

func startPipes(cmd *exec.Cmd, workDir, bin string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, error) {
	cmd.Dir = workDir
	cmd.Env = enrichEnv(os.Environ(), workDir)
	// Own process group so Kill can signal the whole tree (script + child).
	cmd.SysProcAttr = sysProcAttrNewGroup()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, nil, nil, err
	}
	cmd.Stderr = cmd.Stdout // merge stderr

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, nil, nil, fmt.Errorf("start %s: %w", bin, err)
	}
	return cmd, stdin, stdout, nil
}

func enrichEnv(base []string, workDir string) []string {
	env := append([]string{}, base...)
	// ensure user local bins visible (claude / opencode often here)
	home, _ := os.UserHomeDir()
	pathExtra := filepath.Join(home, ".local/bin") + string(os.PathListSeparator) +
		filepath.Join(home, ".hermes/node/bin")
	found := false
	for i, e := range env {
		if len(e) > 5 && e[:5] == "PATH=" {
			env[i] = "PATH=" + pathExtra + string(os.PathListSeparator) + e[5:]
			found = true
			break
		}
	}
	if !found {
		env = append(env, "PATH="+pathExtra)
	}
	// terminal hints for agents
	env = append(env,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"ALPHA_NEXUS_TERMINAL=1",
	)
	if workDir != "" {
		env = append(env, "PWD="+workDir)
	}
	if d := os.Getenv("ALPHA_NEXUS_DATA_DIR"); d != "" {
		env = append(env, "ALPHA_NEXUS_DATA_DIR="+d)
	}
	return env
}

func shellJoin(bin string, args []string) string {
	// crude but enough for script -c
	out := quote(bin)
	for _, a := range args {
		out += " " + quote(a)
	}
	return out
}

func quote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
