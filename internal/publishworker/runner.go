package publishworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	maxWorkerPayloadBytes  = 1 << 20
	workerScriptName       = "publisher-worker.cjs"
	agentOverlayScriptName = "agent_overlay.cjs"
	cdpPortMin             = 15101
	cdpPortMax             = 15600
	maxWorkerTimeoutMS     = 10 * 60 * 1000
)

//go:embed worker.cjs
var workerScript []byte

//go:embed agent_overlay.cjs
var agentOverlayScript []byte

type Content struct {
	Type    string   `json:"type"`
	Title   string   `json:"title"`
	Caption string   `json:"caption"`
	Body    string   `json:"body"`
	Tags    []string `json:"tags"`
}

type Media struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Role     string `json:"role"`
	Position int    `json:"position"`
	SHA256   string `json:"sha256"`
}

type Input struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Action        string                 `json:"action"`
	Platform      string                 `json:"platform"`
	CDPURL        string                 `json:"cdpUrl"`
	Content       Content                `json:"content"`
	Media         []Media                `json:"media"`
	Settings      map[string]interface{} `json:"settings"`
	PublishAt     string                 `json:"publishAt"`
	AllowSubmit   bool                   `json:"allowSubmit"`
	AllowDelete   bool                   `json:"allowDelete"`
	TimeoutMS     int                    `json:"timeoutMs"`
}

type Output struct {
	SchemaVersion int                    `json:"schemaVersion"`
	OK            bool                   `json:"ok"`
	Status        string                 `json:"status"`
	Step          string                 `json:"step"`
	ErrorCode     string                 `json:"errorCode"`
	Location      string                 `json:"location,omitempty"`
	Evidence      map[string]interface{} `json:"evidence,omitempty"`
}

type Runner struct {
	NodePath  string
	ModuleDir string
}

var stableRunnerErrorCodes = map[string]bool{
	"ACTION_UNSUPPORTED": true, "CDP_URL_INVALID": true, "CONTENT_INVALID": true, "CONTENT_TYPE_INVALID": true, "CONTENT_TYPE_UNSUPPORTED": true,
	"CONTENT_UNKNOWN_FIELD": true, "INPUT_TOO_LARGE": true, "INPUT_UNKNOWN_FIELD": true, "MEDIA_FILE_INVALID": true, "MEDIA_HASH_CHANGED": true,
	"MEDIA_INVALID": true, "MEDIA_ITEM_INVALID": true, "MEDIA_ITEM_UNKNOWN_FIELD": true,
	"CDP_CONNECT_FAILED": true, "CDP_PAGE_LIST_FAILED": true, "CDP_TARGET_CLOSED": true, "ACTIVE_PAGE_NOT_FOUND": true,
	"COMPOSER_NAVIGATION_FAILED": true,
	"DELETE_APPROVAL_REQUIRED":   true, "DELETE_FLAG_INVALID": true,
	"NODE_RUNTIME_UNAVAILABLE": true, "PLAYWRIGHT_RUNTIME_UNAVAILABLE": true, "SUBMIT_APPROVAL_REQUIRED": true,
	"AUTO_SUBMIT_NOT_SUPPORTED": true, "SUBMIT_FLAG_INVALID": true, "WORKER_ACTION_INVALID": true, "WORKER_CANCELLED": true,
	"WORKER_ACTION_TIMEOUT": true, "WORKER_EXECUTION_FAILED": true, "WORKER_FORM_PREPARE_FAILED": true, "WORKER_INPUT_INVALID": true,
	"WORKER_OUTPUT_INVALID": true, "WORKER_RUNTIME_TYPE_ERROR": true, "WORKER_SESSION_FAILED": true, "WORKER_START_FAILED": true,
	"WORKER_TARGET_PAGE_FAILED": true,
	"PLATFORM_UNSUPPORTED":      true, "PUBLISH_TIME_INVALID": true, "SCHEMA_VERSION_UNSUPPORTED": true, "SETTINGS_INVALID": true,
	"PUBLISH_CLICK_OUTCOME_UNKNOWN": true,
	"SETTINGS_UNKNOWN_FIELD":        true, "SETTINGS_UNSUPPORTED": true, "TAGS_INVALID": true, "TIMEOUT_INVALID": true,
}

// ErrorCode returns only reviewed process-boundary codes. It never exposes raw
// Node stderr or unstructured errors to the publish event stream.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	code := strings.TrimSpace(err.Error())
	if stableRunnerErrorCodes[code] {
		return code
	}
	return ""
}

func validateCDPURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.User != nil {
		return fmt.Errorf("CDP_URL_INVALID")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < cdpPortMin || port > cdpPortMax {
		return fmt.Errorf("CDP_URL_INVALID")
	}
	return nil
}

func verifyMedia(media []Media) error {
	if len(media) > 10 {
		return fmt.Errorf("MEDIA_INVALID")
	}
	for _, item := range media {
		info, err := os.Lstat(item.Path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("MEDIA_FILE_INVALID")
		}
		file, err := os.Open(item.Path)
		if err != nil {
			return fmt.Errorf("MEDIA_FILE_INVALID")
		}
		digest := sha256.New()
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		actual := hex.EncodeToString(digest.Sum(nil))
		if copyErr != nil || closeErr != nil || !strings.EqualFold(actual, item.SHA256) {
			return fmt.Errorf("MEDIA_HASH_CHANGED")
		}
	}
	return nil
}

func validateInput(input Input) error {
	if input.SchemaVersion != 1 || input.TimeoutMS < 1000 || input.TimeoutMS > maxWorkerTimeoutMS {
		return fmt.Errorf("WORKER_INPUT_INVALID")
	}
	if input.Action != "inspect" && input.Action != "inspect_settings" && input.Action != "inspect_composer" && input.Action != "inspect_manager" && input.Action != "delete_content" && input.Action != "resume_topics" && input.Action != "prepare" && input.Action != "submit" && input.Action != "submit_prepared" {
		return fmt.Errorf("WORKER_ACTION_INVALID")
	}
	if (input.Action == "submit" || input.Action == "submit_prepared") && !input.AllowSubmit {
		return fmt.Errorf("SUBMIT_APPROVAL_REQUIRED")
	}
	if input.Action == "delete_content" && !input.AllowDelete {
		return fmt.Errorf("DELETE_APPROVAL_REQUIRED")
	}
	if input.Action != "delete_content" && input.AllowDelete {
		return fmt.Errorf("DELETE_FLAG_INVALID")
	}
	if input.Action != "submit" && input.Action != "submit_prepared" && input.AllowSubmit {
		return fmt.Errorf("SUBMIT_FLAG_INVALID")
	}
	if strings.TrimSpace(input.PublishAt) != "" {
		if _, err := time.Parse(time.RFC3339, input.PublishAt); err != nil {
			return fmt.Errorf("PUBLISH_TIME_INVALID")
		}
	}
	if err := validateCDPURL(input.CDPURL); err != nil {
		return err
	}
	if input.Action != "inspect" && input.Action != "inspect_settings" && input.Action != "inspect_composer" && input.Action != "inspect_manager" {
		if err := verifyMedia(input.Media); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) nodePath() (string, error) {
	if strings.TrimSpace(r.NodePath) != "" {
		info, err := os.Stat(r.NodePath)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("NODE_RUNTIME_UNAVAILABLE")
		}
		return r.NodePath, nil
	}
	if path := strings.TrimSpace(os.Getenv("ALPHA_NEXUS_NODE")); path != "" {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("NODE_RUNTIME_UNAVAILABLE")
		}
		return path, nil
	}
	path, err := exec.LookPath("node")
	if err != nil {
		return "", fmt.Errorf("NODE_RUNTIME_UNAVAILABLE")
	}
	return path, nil
}

func (r Runner) moduleDir() (string, error) {
	dir := strings.TrimSpace(r.ModuleDir)
	if dir != "" {
		if info, err := os.Stat(filepath.Join(dir, "playwright")); err == nil && info.IsDir() {
			return dir, nil
		}
		return "", fmt.Errorf("PLAYWRIGHT_RUNTIME_UNAVAILABLE")
	}
	var roots []string
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(executable), filepath.Join(filepath.Dir(executable), "resources"))
	}
	seen := map[string]bool{}
	for _, root := range roots {
		current := root
		for depth := 0; depth < 6 && current != "" && !seen[current]; depth++ {
			seen[current] = true
			for _, candidate := range []string{filepath.Join(current, "e2e", "node_modules"), filepath.Join(current, "playwright", "node_modules")} {
				if info, err := os.Stat(filepath.Join(candidate, "playwright")); err == nil && info.IsDir() {
					return candidate, nil
				}
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return "", fmt.Errorf("PLAYWRIGHT_RUNTIME_UNAVAILABLE")
}

func (r Runner) Run(ctx context.Context, input Input) (*Output, error) {
	if err := validateInput(input); err != nil {
		return nil, err
	}
	// The Worker timeout is a hard process boundary, not merely a timeout passed
	// into individual Playwright calls. A stalled CDP command must not keep a
	// one-key publish browser open until the enclosing UI request expires.
	workerCtx, cancel := context.WithTimeout(ctx, time.Duration(input.TimeoutMS)*time.Millisecond)
	defer cancel()
	nodePath, err := r.nodePath()
	if err != nil {
		return nil, err
	}
	moduleDir, err := r.moduleDir()
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(input)
	if err != nil || len(payload) > maxWorkerPayloadBytes {
		return nil, fmt.Errorf("WORKER_INPUT_INVALID")
	}
	sessionRoot, err := os.MkdirTemp("", "alpha-nexus-publisher-")
	if err != nil {
		return nil, fmt.Errorf("WORKER_SESSION_FAILED")
	}
	defer os.RemoveAll(sessionRoot)
	scriptPath := filepath.Join(sessionRoot, workerScriptName)
	if err := os.WriteFile(scriptPath, workerScript, 0o600); err != nil {
		return nil, fmt.Errorf("WORKER_SESSION_FAILED")
	}
	overlayPath := filepath.Join(sessionRoot, agentOverlayScriptName)
	if err := os.WriteFile(overlayPath, agentOverlayScript, 0o600); err != nil {
		return nil, fmt.Errorf("WORKER_SESSION_FAILED")
	}

	command := exec.Command(nodePath, scriptPath)
	command.Dir = sessionRoot
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"), "LANG=zh_CN.UTF-8", "LC_ALL=zh_CN.UTF-8",
		"HOME=" + sessionRoot, "TMPDIR=" + sessionRoot, "NODE_PATH=" + moduleDir,
		"NO_UPDATE_NOTIFIER=1", "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1",
	}
	if os.Getenv("ALPHA_NEXUS_NODE_RUN_AS_NODE") == "1" {
		command.Env = append(command.Env, "ELECTRON_RUN_AS_NODE=1")
	}
	command.Stdin = bytes.NewReader(append(payload, '\n'))
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("WORKER_START_FAILED")
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	select {
	case <-workerCtx.Done():
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-wait
		return nil, fmt.Errorf("WORKER_CANCELLED")
	case err = <-wait:
	}
	if stdout.Len() > maxWorkerPayloadBytes || stderr.Len() > maxWorkerPayloadBytes {
		return nil, fmt.Errorf("WORKER_OUTPUT_INVALID")
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	var output Output
	if decodeErr := decoder.Decode(&output); decodeErr != nil {
		return nil, fmt.Errorf("WORKER_OUTPUT_INVALID")
	}
	if decoder.Decode(&struct{}{}) != io.EOF || output.SchemaVersion != 1 {
		return nil, fmt.Errorf("WORKER_OUTPUT_INVALID")
	}
	if err != nil || !output.OK {
		code := output.ErrorCode
		if code == "" {
			code = "WORKER_FAILED"
		}
		return &output, fmt.Errorf("%s", code)
	}
	return &output, nil
}
