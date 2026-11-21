package agenthub

// This file contains the deliberately small wire adapter used by the Codex
// provider.  The app-server protocol is JSON-RPC-shaped JSONL (the official
// stdio transport omits the jsonrpc header), so this adapter keeps the wire
// types local instead of coupling the Agent Hub contract to Codex's schema.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	codexAppServerClientName    = "alpha-nexus"
	codexAppServerClientTitle   = "Alpha-Nexus"
	codexAppServerClientVersion = "0.4.0"
	codexAppServerShutdownWait  = 2 * time.Second
)

var (
	errCodexAppServerUnavailable = errors.New("AGENT_PROVIDER_CODEX_APP_SERVER_UNAVAILABLE")
	errCodexAppServerProtocol    = errors.New("AGENT_PROVIDER_CODEX_APP_SERVER_PROTOCOL")
	errCodexAppServerWorkDir     = errors.New("AGENT_PROVIDER_CODEX_WORKDIR_INVALID")
)

// The following types intentionally model only the app-server methods used by
// Agent Hub.  Fields not needed by Alpha-Nexus are left out and are therefore
// safely ignored when Codex adds protocol fields.
type codexAppServerRPCRequest struct {
	ID     uint64 `json:"id,omitempty"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type codexAppServerRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type codexAppServerWireMessage struct {
	ID     json.RawMessage         `json:"id"`
	Method string                  `json:"method"`
	Params json.RawMessage         `json:"params"`
	Result json.RawMessage         `json:"result"`
	Error  *codexAppServerRPCError `json:"error"`
}

type codexAppServerInitializeParams struct {
	ClientInfo   codexAppServerClientInfo    `json:"clientInfo"`
	Capabilities *codexAppServerCapabilities `json:"capabilities,omitempty"`
}

type codexAppServerClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type codexAppServerCapabilities struct {
	ExperimentalAPI bool `json:"experimentalApi,omitempty"`
}

type codexAppServerInitializedParams struct{}

type codexAppServerMCPServer struct {
	Command                  string   `json:"command"`
	Args                     []string `json:"args,omitempty"`
	EnvVars                  []string `json:"env_vars,omitempty"`
	Required                 bool     `json:"required,omitempty"`
	DefaultToolsApprovalMode string   `json:"default_tools_approval_mode,omitempty"`
}

type codexAppServerThreadConfig struct {
	MCPServers map[string]codexAppServerMCPServer `json:"mcp_servers,omitempty"`
}

type codexAppServerThreadStartParams struct {
	Model                 string                      `json:"model,omitempty"`
	CWD                   string                      `json:"cwd,omitempty"`
	ApprovalPolicy        string                      `json:"approvalPolicy"`
	Sandbox               string                      `json:"sandbox"`
	DeveloperInstructions string                      `json:"developerInstructions,omitempty"`
	Ephemeral             bool                        `json:"ephemeral,omitempty"`
	Config                *codexAppServerThreadConfig `json:"config,omitempty"`
}

type codexAppServerThreadResumeParams struct {
	ThreadID              string                      `json:"threadId"`
	Model                 string                      `json:"model,omitempty"`
	CWD                   string                      `json:"cwd,omitempty"`
	ApprovalPolicy        string                      `json:"approvalPolicy"`
	Sandbox               string                      `json:"sandbox"`
	DeveloperInstructions string                      `json:"developerInstructions,omitempty"`
	Config                *codexAppServerThreadConfig `json:"config,omitempty"`
}

type codexAppServerThread struct {
	ID string `json:"id"`
}

type codexAppServerThreadStartResponse struct {
	Thread codexAppServerThread `json:"thread"`
}

type codexAppServerThreadResumeResponse struct {
	Thread codexAppServerThread `json:"thread"`
}

type codexAppServerTurnStartParams struct {
	ThreadID            string                    `json:"threadId"`
	ClientUserMessageID string                    `json:"clientUserMessageId,omitempty"`
	Input               []codexAppServerUserInput `json:"input"`
	Model               string                    `json:"model,omitempty"`
}

type codexAppServerUserInput struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

type codexAppServerTurnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type codexAppServerTurnStartResponse struct {
	Turn codexAppServerTurn `json:"turn"`
}

type codexAppServerTurn struct {
	ID     string                   `json:"id"`
	Status string                   `json:"status"`
	Error  *codexAppServerTurnError `json:"error,omitempty"`
}

type codexAppServerTurnError struct {
	Message string `json:"message"`
}

type codexAppServerInitializeResponse struct {
	UserAgent      string `json:"userAgent"`
	CodexHome      string `json:"codexHome"`
	PlatformFamily string `json:"platformFamily"`
	PlatformOS     string `json:"platformOs"`
}

type codexAppServerAgentMessageDeltaParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type codexAppServerItemLifecycleParams struct {
	ThreadID string                   `json:"threadId"`
	TurnID   string                   `json:"turnId"`
	Item     codexAppServerThreadItem `json:"item"`
}

type codexAppServerThreadItem struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
}

type codexAppServerTurnNotificationParams struct {
	ThreadID string             `json:"threadId"`
	Turn     codexAppServerTurn `json:"turn"`
}

type codexAppServerThreadStartedParams struct {
	Thread codexAppServerThread `json:"thread"`
}

type codexAppServerErrorNotificationParams struct {
	ThreadID  string                  `json:"threadId"`
	TurnID    string                  `json:"turnId"`
	Error     codexAppServerTurnError `json:"error"`
	WillRetry bool                    `json:"willRetry"`
}

type codexAppServerModelListParams struct {
	Cursor        *string `json:"cursor,omitempty"`
	Limit         *uint32 `json:"limit,omitempty"`
	IncludeHidden *bool   `json:"includeHidden,omitempty"`
}

type codexAppServerModelListResponse struct {
	Data       []codexAppServerModel `json:"data"`
	NextCursor *string               `json:"nextCursor"`
}

type codexAppServerModel struct {
	ID                        string                                `json:"id"`
	Model                     string                                `json:"model"`
	DisplayName               string                                `json:"displayName"`
	Description               string                                `json:"description"`
	Hidden                    bool                                  `json:"hidden"`
	DefaultReasoningEffort    string                                `json:"defaultReasoningEffort"`
	SupportedReasoningEfforts []codexAppServerReasoningEffortOption `json:"supportedReasoningEfforts"`
	IsDefault                 bool                                  `json:"isDefault"`
}

type codexAppServerReasoningEffortOption struct {
	ReasoningEffort string `json:"reasoningEffort"`
}

type codexAppServerSkillsListParams struct {
	CWDs        []string `json:"cwds,omitempty"`
	ForceReload bool     `json:"forceReload,omitempty"`
}

type codexAppServerSkillsListResponse struct {
	Data []codexAppServerSkillsListEntry `json:"data"`
}

type codexAppServerSkillsListEntry struct {
	CWD    string                     `json:"cwd"`
	Skills []codexAppServerSkill      `json:"skills"`
	Errors []codexAppServerSkillError `json:"errors"`
}

type codexAppServerSkill struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Path        string  `json:"path"`
	Enabled     bool    `json:"enabled"`
	PluginID    *string `json:"pluginId"`
}

type codexAppServerSkillError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type codexAppServerApprovalDecisionResponse struct {
	Decision string `json:"decision"`
}

type codexAppServerDeniedPermissionsResponse struct {
	Permissions codexAppServerDeniedPermissions `json:"permissions"`
	Scope       string                          `json:"scope"`
}

type codexAppServerDeniedPermissions struct {
	FileSystem any `json:"fileSystem"`
	Network    any `json:"network"`
}

type codexAppServerDynamicToolResponse struct {
	ContentItems []any `json:"contentItems"`
	Success      bool  `json:"success"`
}

// codexAppServerProcess is deliberately tiny so tests can provide io.Pipe
// backed processes without starting a model or a real Codex binary.
type codexAppServerProcess interface {
	Stdin() io.WriteCloser
	Stdout() io.ReadCloser
	Wait() error
	Terminate() error
}

type codexAppServerProcessFactory func(context.Context, string, RunRequest) (codexAppServerProcess, error)

type codexAppServerExecProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
}

func (p *codexAppServerExecProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *codexAppServerExecProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *codexAppServerExecProcess) Wait() error           { return p.command.Wait() }
func (p *codexAppServerExecProcess) Terminate() error {
	if p.command == nil || p.command.Process == nil {
		return os.ErrProcessDone
	}
	if p.command.Cancel != nil {
		return p.command.Cancel()
	}
	return p.command.Process.Kill()
}

// codexAppServerRunner starts one app-server process for one Agent run.  The
// process is never shared between turns; Codex persists the thread itself and
// the next run reopens it through thread/resume.
type codexAppServerRunner struct {
	binary         func() string
	command        func(context.Context, string, ...string) *exec.Cmd
	processFactory codexAppServerProcessFactory
}

func (r codexAppServerRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	if request.Ephemeral && strings.TrimSpace(request.SessionID) != "" {
		return "", errors.New("AGENT_PROVIDER_CODEX_EPHEMERAL_RESUME_REJECTED")
	}
	workDir := strings.TrimSpace(request.WorkDir)
	if workDir == "" || !filepath.IsAbs(workDir) {
		return "", errCodexAppServerWorkDir
	}
	if err := validateCodexSkillInputs(request); err != nil {
		return "", err
	}
	binary := ""
	if r.binary != nil {
		binary = strings.TrimSpace(r.binary())
	}
	if binary == "" && r.processFactory == nil {
		return "", errCodexAppServerUnavailable
	}
	processCtx, cancelProcess := context.WithCancel(context.Background())
	defer cancelProcess()
	process, err := r.startProcess(processCtx, binary, request)
	if err != nil {
		return "", err
	}
	connection := newCodexAppServerConnection(process)
	defer connection.close()

	if err := connection.initialize(ctx); err != nil {
		return "", err
	}
	threadID, err := connection.startOrResumeThread(ctx, request)
	if err != nil {
		return threadID, err
	}
	state := codexAppServerTurnState{threadID: threadID, deltas: make(map[string]bool)}
	interruptTurn := func() {
		interruptCtx, cancel := context.WithTimeout(context.Background(), codexAppServerShutdownWait)
		_ = connection.interrupt(interruptCtx, state.threadID, state.turnID, &state)
		cancel()
	}
	turnID, err := connection.startTurn(ctx, request, threadID, &state, emit)
	if err != nil {
		if ctx.Err() != nil && state.turnID != "" {
			interruptTurn()
		}
		return threadID, err
	}
	state.turnID = turnID
	// A compliant server normally sends the turn response before streaming
	// notifications.  Keeping this check makes the adapter correct for fakes
	// and older builds that flush a terminal notification first.
	if state.completed {
		if state.status == "completed" {
			return threadID, nil
		}
		return threadID, errors.New("AGENT_PROVIDER_CODEX_TURN_FAILED")
	}
	for {
		select {
		case <-ctx.Done():
			// The app-server has a first-class interrupt RPC. Give it a bounded
			// chance to persist the interrupted turn before terminating the
			// process in the deferred cleanup path.
			interruptTurn()
			return threadID, ctx.Err()
		case inbound, ok := <-connection.messages:
			if !ok {
				if inbound.err != nil {
					return threadID, inbound.err
				}
				return threadID, errCodexAppServerProtocol
			}
			if inbound.err != nil {
				return threadID, inbound.err
			}
			if err := connection.handle(inbound.message, &state, emit); err != nil {
				return threadID, err
			}
			if state.completed {
				switch state.status {
				case "completed":
					return threadID, nil
				case "interrupted":
					return threadID, errors.New("AGENT_PROVIDER_CODEX_TURN_INTERRUPTED")
				default:
					return threadID, errors.New("AGENT_PROVIDER_CODEX_TURN_FAILED")
				}
			}
		}
	}
}

func validateCodexSkillInputs(request RunRequest) error {
	if len(request.Skills) == 0 {
		return nil
	}
	root := filepath.Clean(strings.TrimSpace(request.SkillRoot))
	if root == "." || !filepath.IsAbs(root) {
		return errors.New("AGENT_PROVIDER_CODEX_SKILL_INVALID")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return errors.New("AGENT_PROVIDER_CODEX_SKILL_INVALID")
	}
	for _, skill := range request.Skills {
		path := filepath.Clean(strings.TrimSpace(skill.Path))
		rel, relErr := filepath.Rel(root, path)
		if strings.TrimSpace(skill.Name) == "" || !filepath.IsAbs(path) || relErr != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) || filepath.Base(path) != "SKILL.md" || filepath.Base(filepath.Dir(path)) != skill.Name {
			return errors.New("AGENT_PROVIDER_CODEX_SKILL_INVALID")
		}
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("AGENT_PROVIDER_CODEX_SKILL_INVALID")
		}
	}
	return nil
}

func (r codexAppServerRunner) startProcess(ctx context.Context, binary string, request RunRequest) (codexAppServerProcess, error) {
	if r.processFactory != nil {
		process, err := r.processFactory(ctx, binary, request)
		if err != nil {
			return nil, err
		}
		if process == nil || process.Stdin() == nil || process.Stdout() == nil {
			return nil, errCodexAppServerUnavailable
		}
		return process, nil
	}
	commandFactory := r.command
	if commandFactory == nil {
		commandFactory = exec.CommandContext
	}
	command := commandFactory(ctx, binary, "app-server", "--stdio")
	if command == nil {
		return nil, errCodexAppServerUnavailable
	}
	configureProcess(command)
	command.Dir = request.WorkDir
	command.Env = providerEnvironmentForRequest(binary, false, request)
	command.Stderr = io.Discard
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	return &codexAppServerExecProcess{command: command, stdin: stdin, stdout: stdout}, nil
}

type codexAppServerInbound struct {
	message codexAppServerWireMessage
	err     error
}

type codexAppServerConnection struct {
	process  codexAppServerProcess
	encoder  *json.Encoder
	messages chan codexAppServerInbound
	stop     chan struct{}
	stopOnce sync.Once
	writeMu  sync.Mutex
	nextID   uint64
}

func newCodexAppServerConnection(process codexAppServerProcess) *codexAppServerConnection {
	connection := &codexAppServerConnection{
		process:  process,
		encoder:  json.NewEncoder(process.Stdin()),
		messages: make(chan codexAppServerInbound, 32),
		stop:     make(chan struct{}),
		nextID:   1,
	}
	go connection.readLoop()
	return connection
}

func (c *codexAppServerConnection) readLoop() {
	scanner := bufio.NewScanner(c.process.Stdout())
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var message codexAppServerWireMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			// Stdio diagnostics are not protocol messages and must never reach
			// the durable Agent event stream.
			continue
		}
		select {
		case c.messages <- codexAppServerInbound{message: message}:
		case <-c.stop:
			return
		}
	}
	inbound := codexAppServerInbound{err: scanner.Err()}
	select {
	case c.messages <- inbound:
	case <-c.stop:
	}
	c.stopOnce.Do(func() { close(c.stop) })
	close(c.messages)
}

func (c *codexAppServerConnection) close() {
	c.stopOnce.Do(func() { close(c.stop) })
	if c.process == nil {
		return
	}
	if stdin := c.process.Stdin(); stdin != nil {
		_ = stdin.Close()
	}
	done := make(chan error, 1)
	go func() { done <- c.process.Wait() }()
	select {
	case <-done:
		return
	case <-time.After(codexAppServerShutdownWait):
		_ = c.process.Terminate()
		<-done
	}
}

func (c *codexAppServerConnection) send(method string, params any) (uint64, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	id := c.nextID
	c.nextID++
	if err := c.encoder.Encode(codexAppServerRPCRequest{ID: id, Method: method, Params: params}); err != nil {
		return 0, err
	}
	return id, nil
}

func (c *codexAppServerConnection) notify(method string, params any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.encoder.Encode(codexAppServerRPCRequest{Method: method, Params: params})
}

func (c *codexAppServerConnection) reply(id json.RawMessage, result any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.encoder.Encode(struct {
		ID     json.RawMessage `json:"id"`
		Result any             `json:"result,omitempty"`
	}{ID: id, Result: result})
}

func (c *codexAppServerConnection) replyError(id json.RawMessage, code int, message string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.encoder.Encode(struct {
		ID    json.RawMessage        `json:"id"`
		Error codexAppServerRPCError `json:"error"`
	}{ID: id, Error: codexAppServerRPCError{Code: code, Message: message}})
}

func (c *codexAppServerConnection) initialize(ctx context.Context) error {
	var response codexAppServerInitializeResponse
	_, err := c.call(ctx, "initialize", codexAppServerInitializeParams{
		ClientInfo:   codexAppServerClientInfo{Name: codexAppServerClientName, Title: codexAppServerClientTitle, Version: codexAppServerClientVersion},
		Capabilities: &codexAppServerCapabilities{},
	}, &response, nil)
	if err != nil {
		return err
	}
	return c.notify("initialized", codexAppServerInitializedParams{})
}

func (c *codexAppServerConnection) startOrResumeThread(ctx context.Context, request RunRequest) (string, error) {
	config := codexAppServerThreadConfigForRequest(request)
	if strings.TrimSpace(request.SessionID) == "" {
		var response codexAppServerThreadStartResponse
		_, err := c.call(ctx, "thread/start", codexAppServerThreadStartParams{
			Model: request.ModelID, CWD: request.WorkDir,
			ApprovalPolicy: "never", Sandbox: "read-only",
			DeveloperInstructions: codexAppServerDeveloperInstructions(), Ephemeral: request.Ephemeral,
			Config: config,
		}, &response, nil)
		if err != nil {
			return "", err
		}
		if response.Thread.ID == "" {
			return "", errCodexAppServerProtocol
		}
		return response.Thread.ID, nil
	}
	var response codexAppServerThreadResumeResponse
	_, err := c.call(ctx, "thread/resume", codexAppServerThreadResumeParams{
		ThreadID: request.SessionID, Model: request.ModelID, CWD: request.WorkDir,
		ApprovalPolicy: "never", Sandbox: "read-only",
		DeveloperInstructions: codexAppServerDeveloperInstructions(), Config: config,
	}, &response, nil)
	if err != nil {
		return request.SessionID, err
	}
	if response.Thread.ID == "" {
		// Older app-server builds may omit the nested thread object on resume;
		// the requested id remains authoritative in that compatibility case.
		return request.SessionID, nil
	}
	return response.Thread.ID, nil
}

func (c *codexAppServerConnection) startTurn(ctx context.Context, request RunRequest, threadID string, state *codexAppServerTurnState, emit func(ProviderEvent)) (string, error) {
	var response codexAppServerTurnStartResponse
	inputs := make([]codexAppServerUserInput, 0, len(request.Skills)+1)
	for _, skill := range request.Skills {
		inputs = append(inputs, codexAppServerUserInput{Type: "skill", Name: skill.Name, Path: skill.Path})
	}
	inputs = append(inputs, codexAppServerUserInput{Type: "text", Text: codexPrompt(request)})
	_, err := c.call(ctx, "turn/start", codexAppServerTurnStartParams{
		ThreadID: threadID, ClientUserMessageID: request.RunID,
		Input: inputs, Model: request.ModelID,
	}, &response, func(message codexAppServerWireMessage) error {
		return c.handle(message, state, emit)
	})
	if err != nil {
		return "", err
	}
	if response.Turn.ID == "" {
		return "", errCodexAppServerProtocol
	}
	return response.Turn.ID, nil
}

func (c *codexAppServerConnection) interrupt(ctx context.Context, threadID, turnID string, state *codexAppServerTurnState) error {
	if threadID == "" || turnID == "" {
		return errCodexAppServerProtocol
	}
	var response map[string]any
	_, err := c.call(ctx, "turn/interrupt", codexAppServerTurnInterruptParams{ThreadID: threadID, TurnID: turnID}, &response, func(message codexAppServerWireMessage) error {
		return c.handle(message, state, nil)
	})
	return err
}

func (c *codexAppServerConnection) call(ctx context.Context, method string, params any, result any, onMessage func(codexAppServerWireMessage) error) (uint64, error) {
	id, err := c.send(method, params)
	if err != nil {
		return 0, err
	}
	for {
		select {
		case <-ctx.Done():
			if method == "turn/start" {
				// If cancellation races with the server accepting turn/start, wait
				// briefly for the response so the caller can issue turn/interrupt
				// with the authoritative turn id before process cleanup.
				grace := time.NewTimer(250 * time.Millisecond)
				defer grace.Stop()
				for {
					select {
					case inbound, ok := <-c.messages:
						if !ok || inbound.err != nil {
							return id, ctx.Err()
						}
						matched, consumeErr := c.consumeCallMessage(inbound.message, id, method, result, onMessage)
						if consumeErr != nil {
							return id, consumeErr
						}
						if matched {
							return id, nil
						}
					case <-grace.C:
						return id, ctx.Err()
					}
				}
			}
			return id, ctx.Err()
		case inbound, ok := <-c.messages:
			if !ok {
				if inbound.err != nil {
					return id, inbound.err
				}
				return id, errCodexAppServerProtocol
			}
			if inbound.err != nil {
				return id, inbound.err
			}
			matched, consumeErr := c.consumeCallMessage(inbound.message, id, method, result, onMessage)
			if consumeErr != nil {
				return id, consumeErr
			}
			if matched {
				return id, nil
			}
		}
	}
}

func (c *codexAppServerConnection) consumeCallMessage(message codexAppServerWireMessage, id uint64, method string, result any, onMessage func(codexAppServerWireMessage) error) (bool, error) {
	if message.idMatches(id) {
		if message.Error != nil {
			return false, fmt.Errorf("%w: %s", errCodexAppServerProtocol, safeCodexAppServerError(message.Error))
		}
		if result != nil && len(message.Result) > 0 && string(message.Result) != "null" {
			if err := json.Unmarshal(message.Result, result); err != nil {
				return false, fmt.Errorf("%w: decode %s response", errCodexAppServerProtocol, method)
			}
		}
		return true, nil
	}
	if onMessage != nil {
		return false, onMessage(message)
	}
	return false, c.handleServerRequest(message)
}

type codexAppServerTurnState struct {
	threadID  string
	turnID    string
	status    string
	completed bool
	deltas    map[string]bool
}

func (c *codexAppServerConnection) handle(message codexAppServerWireMessage, state *codexAppServerTurnState, emit func(ProviderEvent)) error {
	if message.Method == "" {
		return nil
	}
	if message.ID != nil && len(message.ID) > 0 && string(message.ID) != "null" {
		return c.handleServerRequest(message)
	}
	params := message.Params
	switch message.Method {
	case "thread/started":
		var notification codexAppServerThreadStartedParams
		if err := json.Unmarshal(params, &notification); err != nil {
			return fmt.Errorf("%w: thread/started", errCodexAppServerProtocol)
		}
		if state != nil && notification.Thread.ID != "" {
			state.threadID = notification.Thread.ID
		}
	case "turn/started":
		var notification codexAppServerTurnNotificationParams
		if err := json.Unmarshal(params, &notification); err != nil {
			return fmt.Errorf("%w: turn/started", errCodexAppServerProtocol)
		}
		if state != nil {
			if notification.ThreadID != "" {
				state.threadID = notification.ThreadID
			}
			if notification.Turn.ID != "" {
				state.turnID = notification.Turn.ID
			}
		}
	case "item/agentMessage/delta":
		var notification codexAppServerAgentMessageDeltaParams
		if err := json.Unmarshal(params, &notification); err != nil {
			return fmt.Errorf("%w: item/agentMessage/delta", errCodexAppServerProtocol)
		}
		if state != nil && notification.ThreadID != "" && notification.ThreadID != state.threadID {
			return nil
		}
		if state != nil && notification.TurnID != "" && state.turnID != "" && notification.TurnID != state.turnID {
			return nil
		}
		if state != nil && notification.ItemID != "" {
			state.deltas[notification.ItemID] = true
		}
		if emit != nil && notification.Delta != "" {
			emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": notification.Delta}})
		}
	case "item/completed":
		var notification codexAppServerItemLifecycleParams
		if err := json.Unmarshal(params, &notification); err != nil {
			return fmt.Errorf("%w: item/completed", errCodexAppServerProtocol)
		}
		if state != nil && (notification.Item.Type == "agentMessage" || notification.Item.Type == "agent_message") && notification.Item.Text != "" && !state.deltas[notification.Item.ID] && emit != nil {
			emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": notification.Item.Text}})
		}
	case "turn/completed":
		var notification codexAppServerTurnNotificationParams
		if err := json.Unmarshal(params, &notification); err != nil {
			return fmt.Errorf("%w: turn/completed", errCodexAppServerProtocol)
		}
		if state != nil && notification.ThreadID != "" && notification.ThreadID != state.threadID {
			return nil
		}
		if state != nil && notification.Turn.ID != "" && state.turnID != "" && notification.Turn.ID != state.turnID {
			return nil
		}
		if state != nil {
			state.status = notification.Turn.Status
			state.completed = true
		}
	case "error":
		var notification codexAppServerErrorNotificationParams
		if err := json.Unmarshal(params, &notification); err != nil {
			return fmt.Errorf("%w: error", errCodexAppServerProtocol)
		}
		if state != nil && !notification.WillRetry {
			state.status = "failed"
			state.completed = true
		}
	}
	return nil
}

func (c *codexAppServerConnection) handleServerRequest(message codexAppServerWireMessage) error {
	if len(message.ID) == 0 || string(message.ID) == "null" {
		return nil
	}
	switch message.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "applyPatchApproval", "execCommandApproval":
		// Shell and file approvals are intentionally unavailable to Codex's
		// in-app adapter, even if a future Codex build ignores approvalPolicy.
		return c.reply(message.ID, codexAppServerApprovalDecisionResponse{Decision: "decline"})
	case "item/permissions/requestApproval":
		return c.reply(message.ID, codexAppServerDeniedPermissionsResponse{
			Permissions: codexAppServerDeniedPermissions{FileSystem: nil, Network: nil}, Scope: "turn",
		})
	case "item/tool/requestUserInput":
		return c.reply(message.ID, map[string]any{"answers": map[string]any{}})
	case "item/tool/call":
		return c.reply(message.ID, codexAppServerDynamicToolResponse{ContentItems: []any{}, Success: false})
	default:
		return c.replyError(message.ID, -32601, "method not supported")
	}
}

func (m codexAppServerWireMessage) idMatches(id uint64) bool {
	if len(m.ID) == 0 || string(m.ID) == "null" {
		return false
	}
	var numeric uint64
	if json.Unmarshal(m.ID, &numeric) == nil {
		return numeric == id
	}
	return strings.Trim(string(m.ID), `"`) == strconv.FormatUint(id, 10)
}

func safeCodexAppServerError(err *codexAppServerRPCError) string {
	if err == nil {
		return "request failed"
	}
	if strings.TrimSpace(err.Message) == "" {
		return "request failed"
	}
	// Provider errors are intentionally not copied into Agent events. Keep
	// this diagnostic bounded for local callers while avoiding raw payloads.
	return strings.TrimSpace(err.Message)[:min(len(strings.TrimSpace(err.Message)), 160)]
}

func codexAppServerDeveloperInstructions() string {
	return "You are the Alpha-Nexus in-app business assistant. Reply only with the requested answer. " +
		"You are in an isolated runtime workspace, not the Alpha-Nexus source checkout. " +
		"Never read or modify source code and never use shell, file writes, or arbitrary network access. " +
		"Only the run-scoped alpha_nexus MCP broker may expose reviewed Registry Skills; shell and file approvals are disabled."
}

func codexAppServerThreadConfigForRequest(request RunRequest) *codexAppServerThreadConfig {
	command := brokerCommand(request)
	if len(command) == 0 {
		return nil
	}
	return &codexAppServerThreadConfig{MCPServers: map[string]codexAppServerMCPServer{
		"alpha_nexus": {
			Command: command[0], Args: command[1:], EnvVars: []string{"ALPHA_NEXUS_AGENT_RUN_TOKEN"},
			Required: true, DefaultToolsApprovalMode: "auto",
		},
	}}
}

// codexAppServerModels is shared by the provider model probe and is kept
// separate from the runner so probing remains a one-process, no-turn action.
func codexAppServerModels(ctx context.Context, binary string) ([]string, error) {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		return nil, errCodexAppServerUnavailable
	}
	workDir := filepath.Dir(binary)
	if absolute, err := filepath.Abs(workDir); err == nil {
		workDir = absolute
	}
	runner := codexAppServerRunner{binary: func() string { return binary }}
	return runner.listModels(ctx, RunRequest{WorkDir: workDir})
}

func (r codexAppServerRunner) listModels(ctx context.Context, request RunRequest) ([]string, error) {
	binary := ""
	if r.binary != nil {
		binary = strings.TrimSpace(r.binary())
	}
	if binary == "" && r.processFactory == nil {
		return nil, errCodexAppServerUnavailable
	}
	processCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	process, err := r.startProcess(processCtx, binary, request)
	if err != nil {
		return nil, err
	}
	connection := newCodexAppServerConnection(process)
	defer connection.close()
	if err := connection.initialize(ctx); err != nil {
		return nil, err
	}
	var response codexAppServerModelListResponse
	limit := uint32(128)
	includeHidden := false
	models := make([]string, 0)
	seen := make(map[string]bool)
	var cursor *string
	for page := 0; page < 16; page++ {
		response = codexAppServerModelListResponse{}
		if _, err := connection.call(ctx, "model/list", codexAppServerModelListParams{Cursor: cursor, Limit: &limit, IncludeHidden: &includeHidden}, &response, nil); err != nil {
			return nil, err
		}
		for _, model := range response.Data {
			value := strings.TrimSpace(model.Model)
			if value == "" {
				value = strings.TrimSpace(model.ID)
			}
			if model.Hidden || value == "" || seen[value] || ValidateModelID(value) != nil {
				continue
			}
			seen[value] = true
			models = append(models, value)
		}
		if response.NextCursor == nil || strings.TrimSpace(*response.NextCursor) == "" {
			break
		}
		if cursor != nil && *cursor == *response.NextCursor {
			break
		}
		next := strings.TrimSpace(*response.NextCursor)
		cursor = &next
	}
	return models, nil
}

func (r codexAppServerRunner) listSkills(ctx context.Context, request RunRequest) ([]codexAppServerSkillsListEntry, error) {
	if strings.TrimSpace(request.WorkDir) == "" || !filepath.IsAbs(request.WorkDir) {
		return nil, errCodexAppServerWorkDir
	}
	binary := ""
	if r.binary != nil {
		binary = strings.TrimSpace(r.binary())
	}
	if binary == "" && r.processFactory == nil {
		return nil, errCodexAppServerUnavailable
	}
	processCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	process, err := r.startProcess(processCtx, binary, request)
	if err != nil {
		return nil, err
	}
	connection := newCodexAppServerConnection(process)
	defer connection.close()
	if err := connection.initialize(ctx); err != nil {
		return nil, err
	}
	var response codexAppServerSkillsListResponse
	if _, err := connection.call(ctx, "skills/list", codexAppServerSkillsListParams{CWDs: []string{request.WorkDir}}, &response, nil); err != nil {
		return nil, err
	}
	return response.Data, nil
}
