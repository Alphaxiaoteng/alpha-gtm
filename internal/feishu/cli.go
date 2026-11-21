// Package feishu mirrors collected content into a Feishu (Lark) Base, the
// multi-dimensional table product. It drives the official `lark-cli` instead
// of calling the Open Platform directly, so one install of the CLI carries the
// credentials and scope handling for every command here.
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Identity selects whose credentials the CLI acts with. Base operations
// normally belong to the person, so user is the default.
const (
	IdentityUser = "user"
	IdentityBot  = "bot"
)

// Runner is the one seam to the outside world: a test swaps in a fake that
// returns canned CLI envelopes, so no test needs a real Feishu tenant.
type Runner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// ExecRunner runs the real binary.
type ExecRunner struct {
	Path    string
	Profile string
	Timeout time.Duration
}

func (runner ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	if runner.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, runner.Timeout)
		defer cancel()
	}
	full := args
	if runner.Profile != "" {
		full = append([]string{"--profile", runner.Profile}, args...)
	}
	command := exec.CommandContext(ctx, runner.Path, full...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	if runErr != nil {
		// A non-zero exit is a failure even when the CLI managed to print a JSON
		// envelope: treating that body as success would hide every hard failure
		// (bad credentials, network error, crash) behind a parse error or, worse,
		// a half-written payload. The exit status wins; the stderr summary and a
		// short stdout excerpt travel with it so the cause stays diagnosable.
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = runErr.Error()
		}
		if excerpt := stdoutExcerpt(stdout.String()); excerpt != "" && !strings.Contains(message, excerpt) {
			message = message + " (stdout: " + excerpt + ")"
		}
		return nil, fmt.Errorf("lark-cli %s: %s", strings.Join(args, " "), message)
	}
	return stdout.Bytes(), nil
}

// stdoutExcerpt keeps an error message readable: the first line only, capped,
// because a failing CLI can dump a whole envelope.
func stdoutExcerpt(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if len(line) > 200 {
				return line[:200]
			}
			return line
		}
	}
	return ""
}

// APIError is a structured failure from the CLI's JSON envelope.
type APIError struct {
	Code          int
	Subtype       string
	Message       string
	MissingScopes []string
	ConsoleURL    string
}

func (err *APIError) Error() string {
	if len(err.MissingScopes) > 0 {
		return fmt.Sprintf("feishu %s (%d): %s; missing scopes %s",
			err.Subtype, err.Code, err.Message, strings.Join(err.MissingScopes, ","))
	}
	if err.Subtype != "" {
		return fmt.Sprintf("feishu %s (%d): %s", err.Subtype, err.Code, err.Message)
	}
	return fmt.Sprintf("feishu error (%d): %s", err.Code, err.Message)
}

// IsScopeMissing reports whether an error is a missing app scope, which only a
// human can grant in the developer console.
func IsScopeMissing(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Subtype == "app_scope_not_applied" || apiErr.Code == 99991672
	}
	return false
}

// Client is a thin typed wrapper over `lark-cli`.
type Client struct {
	Runner   Runner
	Identity string
	Now      func() time.Time
}

// NewClient returns a client that runs the real CLI, resolving the binary from
// ALPHA_FEISHU_CLI, then PATH, then the default per-user install.
func NewClient() *Client {
	return &Client{Runner: ExecRunner{Path: ResolveBinary(), Timeout: 120 * time.Second}, Identity: IdentityUser, Now: time.Now}
}

// ResolveBinary finds lark-cli without requiring the caller to set PATH.
func ResolveBinary() string {
	if configured := strings.TrimSpace(os.Getenv("ALPHA_FEISHU_CLI")); configured != "" {
		return configured
	}
	if path, err := exec.LookPath("lark-cli"); err == nil {
		return path
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".local", "bin", "lark-cli")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate
		}
	}
	return "lark-cli"
}

// envelope is the CLI's uniform JSON wrapper.
type envelope struct {
	OK       bool            `json:"ok"`
	Identity string          `json:"identity"`
	Data     json.RawMessage `json:"data"`
	Error    *struct {
		Type          string   `json:"type"`
		Subtype       string   `json:"subtype"`
		Code          int      `json:"code"`
		Message       string   `json:"message"`
		MissingScopes []string `json:"missing_scopes"`
		ConsoleURL    string   `json:"console_url"`
	} `json:"error"`
}

// Call runs one CLI command and returns its `data` payload.
func (client *Client) Call(ctx context.Context, args ...string) (json.RawMessage, error) {
	if client == nil || client.Runner == nil {
		return nil, errors.New("feishu client unavailable")
	}
	identity := client.Identity
	if identity == "" {
		identity = IdentityUser
	}
	full := append([]string{}, args...)
	full = append(full, "--format", "json")
	if !hasFlag(full, "--as") {
		full = append(full, "--as", identity)
	}
	raw, err := client.Runner.Run(ctx, full...)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return json.RawMessage("{}"), nil
	}
	var decoded envelope
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return nil, fmt.Errorf("feishu returned non-JSON output: %w", err)
	}
	if !decoded.OK {
		if decoded.Error != nil {
			return nil, &APIError{
				Code: decoded.Error.Code, Subtype: decoded.Error.Subtype,
				Message: decoded.Error.Message, MissingScopes: decoded.Error.MissingScopes,
				ConsoleURL: decoded.Error.ConsoleURL,
			}
		}
		return nil, errors.New("feishu reported failure without an error body")
	}
	if len(decoded.Data) == 0 {
		return json.RawMessage("{}"), nil
	}
	return decoded.Data, nil
}

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

// findString returns the first value stored under key anywhere in a decoded
// JSON tree. CLI payloads wrap results differently per command (base.create
// nests under base, record.create nests under record), so callers read by key
// instead of hard-coding each shape.
func findString(payload any, key string) string {
	switch node := payload.(type) {
	case map[string]any:
		if value, ok := node[key]; ok {
			if text, isText := value.(string); isText && strings.TrimSpace(text) != "" {
				return text
			}
		}
		for _, value := range node {
			if found := findString(value, key); found != "" {
				return found
			}
		}
	case []any:
		for _, value := range node {
			if found := findString(value, key); found != "" {
				return found
			}
		}
	}
	return ""
}

// decode reads a raw payload into a generic tree for key lookups.
func decode(raw json.RawMessage) (any, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode feishu payload: %w", err)
	}
	return payload, nil
}
