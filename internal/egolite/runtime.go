package egolite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	resultPrefix   = "ALPHA_EGO_RESULT:"
	maxProgramSize = 1 << 20
	maxOutputSize  = 1 << 20
)

var requiredHelpers = []string{
	"listProfiles",
	"listTaskSpaces",
	"useOrCreateTaskSpace",
	"claimTaskSpace",
	"takeOverTaskSpace",
	"listTabs",
	"openOrReuseTab",
	"gotoAndWait",
	"switchTab",
	"currentTab",
	"closeTab",
	"pageInfo",
	"js",
	"cdp",
	"snapshotText",
	"waitForNetworkIdle",
	"drainEvents",
	"browserFetch",
	"uploadFile",
	"handOffTaskSpace",
	"completeTaskSpace",
}

type Runtime struct {
	command string
	timeout time.Duration
}

type outputBudget struct {
	mu    sync.Mutex
	used  int
	limit int
}

type boundedOutput struct {
	budget   *outputBudget
	data     bytes.Buffer
	overflow bool
}

func newBoundedOutput(budget *outputBudget) *boundedOutput {
	return &boundedOutput{budget: budget}
}

func (output *boundedOutput) Write(data []byte) (int, error) {
	if output == nil || output.budget == nil {
		return len(data), nil
	}
	output.budget.mu.Lock()
	defer output.budget.mu.Unlock()
	remaining := output.budget.limit - output.budget.used
	if remaining > 0 {
		keep := len(data)
		if keep > remaining {
			keep = remaining
		}
		_, _ = output.data.Write(data[:keep])
		output.budget.used += keep
		if keep < len(data) {
			output.overflow = true
		}
	} else if len(data) > 0 {
		output.overflow = true
	}
	// Keep consuming the pipe after the cap is reached. Returning an error here
	// can make the child block while the exec copy goroutine exits, defeating the
	// context timeout that is meant to stop a stuck EgoLite invocation.
	return len(data), nil
}

func (output *boundedOutput) String() string {
	if output == nil {
		return ""
	}
	return output.data.String()
}

func (output *boundedOutput) Len() int {
	if output == nil {
		return 0
	}
	return output.data.Len()
}

func (output *boundedOutput) Overflowed() bool {
	return output != nil && output.overflow
}

type DoctorResult struct {
	Available bool     `json:"available"`
	Missing   []string `json:"missing"`
}

func NewRuntime(command string, timeout time.Duration) *Runtime {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Runtime{command: strings.TrimSpace(command), timeout: timeout}
}

func (r *Runtime) Run(parent context.Context, program string, result any) error {
	if r == nil || r.command == "" {
		return fmt.Errorf("EGO_RUNTIME_UNAVAILABLE")
	}
	if strings.TrimSpace(program) == "" || len(program) > maxProgramSize || result == nil {
		return fmt.Errorf("EGO_PROGRAM_INVALID")
	}
	ctx, cancel := context.WithTimeout(parent, r.timeout)
	defer cancel()

	commandPath, runtimePath, err := resolveRuntimeCommand(r.command)
	if err != nil {
		return fmt.Errorf("EGO_RUNTIME_EXECUTION_FAILED")
	}
	command := exec.CommandContext(ctx, commandPath, "nodejs")
	command.Stdin = strings.NewReader(program)
	command.Env = []string{
		"PATH=" + runtimePath,
		"HOME=" + os.Getenv("HOME"),
		"LANG=zh_CN.UTF-8",
		"LC_ALL=zh_CN.UTF-8",
	}
	budget := &outputBudget{limit: maxOutputSize}
	stdout := newBoundedOutput(budget)
	stderr := newBoundedOutput(budget)
	command.Stdout = stdout
	command.Stderr = stderr
	runErr := command.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("EGO_RUNTIME_TIMEOUT")
	}
	if ctx.Err() == context.Canceled {
		return fmt.Errorf("EGO_RUNTIME_CANCELLED")
	}
	if runErr != nil {
		return fmt.Errorf("%s", runtimeExecutionError(stderr.String()))
	}
	if stdout.Overflowed() || stderr.Overflowed() || stdout.Len()+stderr.Len() > maxOutputSize {
		return fmt.Errorf("EGO_OUTPUT_LIMIT")
	}
	payload := structuredPayload(stdout.String())
	if payload == "" {
		payload = structuredPayload(stderr.String())
	}
	if payload == "" {
		return fmt.Errorf("EGO_OUTPUT_INVALID")
	}
	if err := json.Unmarshal([]byte(payload), result); err != nil {
		return fmt.Errorf("EGO_OUTPUT_INVALID")
	}
	return nil
}

// runtimeExecutionError maps EgoLite failures to stable, non-sensitive codes.
// Raw stderr can contain page content or local paths and must never cross the
// API boundary.
func runtimeExecutionError(stderr string) string {
	message := strings.ToLower(stderr)
	switch {
	case strings.Contains(message, "ego_profile_not_found"):
		return "EGO_PROFILE_NOT_FOUND"
	case strings.Contains(message, "ego_profile_binding_unavailable"):
		return "EGO_PROFILE_BINDING_UNAVAILABLE"
	case strings.Contains(message, "ego_space_binding_missing"):
		return "EGO_SPACE_BINDING_MISSING"
	case strings.Contains(message, "space not found"),
		strings.Contains(message, "task space not found"):
		// The binding row exists but the Space itself is gone; callers must
		// re-bind instead of reporting a missing local binding.
		return "EGO_SPACE_NOT_FOUND"
	case strings.Contains(message, "ego_login_qr_platform_tab_mismatch"),
		strings.Contains(message, "ego_login_qr_platform_tab_missing"),
		strings.Contains(message, "no target with given id"):
		return "EGO_LOGIN_QR_PLATFORM_MISMATCH"
	case strings.Contains(message, "ownership"),
		strings.Contains(message, "owned by user"),
		strings.Contains(message, "user-owned"),
		strings.Contains(message, "user controlled"),
		strings.Contains(message, "user is controlling"),
		strings.Contains(message, "user has taken control"):
		return "EGO_SPACE_USER_CONTROLLED"
	case strings.Contains(message, "inactive"), strings.Contains(message, "not assigned"):
		return "EGO_SPACE_INACTIVE"
	default:
		return "EGO_RUNTIME_EXECUTION_FAILED"
	}
}

func resolveRuntimeCommand(command string) (string, string, error) {
	runtimePath := os.Getenv("PATH")
	if resolved, err := exec.LookPath(command); err == nil {
		return resolved, runtimePath, nil
	}
	if filepath.Base(command) != command {
		return "", runtimePath, fmt.Errorf("command not found")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", runtimePath, fmt.Errorf("command not found")
	}
	localBin := filepath.Join(home, ".local", "bin")
	candidate := filepath.Join(localBin, command)
	info, err := os.Stat(candidate)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", runtimePath, fmt.Errorf("command not found")
	}
	if runtimePath == "" {
		runtimePath = localBin
	} else {
		runtimePath = localBin + string(os.PathListSeparator) + runtimePath
	}
	return candidate, runtimePath, nil
}

func (r *Runtime) Doctor(ctx context.Context) (*DoctorResult, error) {
	required, err := json.Marshal(requiredHelpers)
	if err != nil {
		return nil, fmt.Errorf("EGO_PROGRAM_INVALID")
	}
	program := fmt.Sprintf(`const required = %s
const missing = required.filter((name) => typeof globalThis[name] !== 'function')
if (typeof ego === 'undefined' || typeof ego.createTaskSpace !== 'function') missing.push('ego.createTaskSpace')
cliLog(%q + JSON.stringify({ available: missing.length === 0, missing }))
`, required, resultPrefix)
	var result DoctorResult
	if err := r.Run(ctx, program, &result); err != nil {
		return nil, err
	}
	if !result.Available || len(result.Missing) > 0 {
		return &result, fmt.Errorf("EGO_CAPABILITY_MISSING")
	}
	return &result, nil
}

func structuredPayload(output string) string {
	lines := strings.Split(output, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if strings.HasPrefix(line, resultPrefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, resultPrefix))
		}
	}
	return ""
}
