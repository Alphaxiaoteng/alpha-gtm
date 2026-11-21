package agenthub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const acpProtocolVersion = 1

type acpSessionResult struct {
	SessionID     string            `json:"sessionId"`
	ConfigOptions []acpConfigOption `json:"configOptions"`
}

type acpConfigOption struct {
	ID           string            `json:"id"`
	Category     string            `json:"category"`
	Type         string            `json:"type"`
	CurrentValue string            `json:"currentValue"`
	Options      []acpConfigChoice `json:"options"`
}

type acpConfigChoice struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

type acpRPCMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type acpProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  bytes.Buffer
	nextID  int
}

type kimiACPRunner struct {
	binary  func() string
	command func(context.Context, string, ...string) *exec.Cmd
}

func acpModelsFromSessionResult(raw []byte) []string {
	var result acpSessionResult
	if json.Unmarshal(raw, &result) != nil {
		return nil
	}
	seen := make(map[string]bool)
	models := make([]string, 0)
	for _, option := range result.ConfigOptions {
		if option.Category != "model" || option.Type != "select" {
			continue
		}
		for _, choice := range option.Options {
			modelID := strings.TrimSpace(choice.Value)
			if modelID == "" || modelID == "default-model" || seen[modelID] || ValidateModelID(modelID) != nil {
				continue
			}
			seen[modelID] = true
			models = append(models, modelID)
		}
	}
	return models
}

func discoverACPModelsFromCommand(ctx context.Context, command *exec.Cmd, workDir string) ([]string, error) {
	process, err := startACPProcess(command, workDir)
	if err != nil {
		return nil, err
	}
	defer process.close()
	if _, err := process.initialize(); err != nil {
		return nil, err
	}
	result, err := process.call("session/new", map[string]any{
		"cwd":        workDir,
		"mcpServers": []any{},
	}, nil)
	if err != nil {
		return nil, err
	}
	return acpModelsFromSessionResult(result), nil
}

func (r kimiACPRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	binary := strings.TrimSpace(r.binary())
	if binary == "" {
		return "", errProviderUnavailable
	}
	commandFn := r.command
	if commandFn == nil {
		commandFn = exec.CommandContext
	}
	command := commandFn(ctx, binary, "acp")
	if command.Env == nil {
		command.Env = providerEnvironmentForRequest(binary, false, request)
	}
	command.Env = append(command.Env,
		"KIMI_DISABLE_TELEMETRY=1",
		"KIMI_CODE_NO_AUTO_UPDATE=1",
		"KIMI_CODE_BACKGROUND_KEEP_ALIVE_ON_EXIT=0",
	)
	process, err := startACPProcess(command, request.WorkDir)
	if err != nil {
		return request.SessionID, err
	}
	defer process.close()
	if _, err := process.initialize(); err != nil {
		return request.SessionID, errors.New("AGENT_PROVIDER_KIMI_FAILED")
	}
	mcpServers := acpMCPServers(request)
	method := "session/new"
	params := map[string]any{"cwd": request.WorkDir, "mcpServers": mcpServers}
	if request.SessionID != "" && !request.Ephemeral {
		method = "session/load"
		params["sessionId"] = request.SessionID
	}
	sessionRaw, err := process.call(method, params, nil)
	if err != nil {
		return request.SessionID, errors.New("AGENT_PROVIDER_KIMI_FAILED")
	}
	var session acpSessionResult
	if json.Unmarshal(sessionRaw, &session) != nil || strings.TrimSpace(session.SessionID) == "" {
		return request.SessionID, errors.New("AGENT_PROVIDER_KIMI_FAILED")
	}
	if request.ModelID != "" {
		configID, ok := acpModelConfigID(session.ConfigOptions, request.ModelID)
		if !ok {
			return session.SessionID, ErrModelNotVerified
		}
		if _, err := process.call("session/set_config_option", map[string]any{
			"sessionId": session.SessionID,
			"configId":  configID,
			"value":     request.ModelID,
		}, nil); err != nil {
			return session.SessionID, errors.New("AGENT_PROVIDER_KIMI_FAILED")
		}
	}
	receivedText := false
	onMessage := func(message acpRPCMessage) error {
		if message.Method != "session/update" {
			return nil
		}
		var update struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content       struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		if json.Unmarshal(message.Params, &update) != nil ||
			update.Update.SessionUpdate != "agent_message_chunk" ||
			update.Update.Content.Type != "text" ||
			update.Update.Content.Text == "" {
			return nil
		}
		receivedText = true
		emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": update.Update.Content.Text}})
		return nil
	}
	if _, err := process.call("session/prompt", map[string]any{
		"sessionId": session.SessionID,
		"prompt": []map[string]string{{
			"type": "text",
			"text": claudePrompt(request),
		}},
	}, onMessage); err != nil || !receivedText {
		return session.SessionID, errors.New("AGENT_PROVIDER_KIMI_FAILED")
	}
	return session.SessionID, nil
}

func acpModelConfigID(options []acpConfigOption, modelID string) (string, bool) {
	for _, option := range options {
		if option.Category != "model" || option.Type != "select" {
			continue
		}
		for _, choice := range option.Options {
			if strings.TrimSpace(choice.Value) == modelID {
				return option.ID, true
			}
		}
	}
	return "", false
}

func acpMCPServers(request RunRequest) []any {
	command := brokerCommand(request)
	if len(command) == 0 {
		return []any{}
	}
	return []any{map[string]any{
		"name":    "alpha_nexus",
		"command": command[0],
		"args":    command[1:],
		"env": []map[string]string{{
			"name":  "ALPHA_NEXUS_AGENT_RUN_TOKEN",
			"value": request.BrokerToken,
		}},
	}}
}

func startACPProcess(command *exec.Cmd, workDir string) (*acpProcess, error) {
	if command == nil || !filepath.IsAbs(workDir) {
		return nil, fmt.Errorf("AGENT_ACP_RUNTIME_UNAVAILABLE")
	}
	configureProcess(command)
	command.Dir = workDir
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	process := &acpProcess{
		command: command,
		stdin:   stdin,
		scanner: bufio.NewScanner(stdout),
		nextID:  1,
	}
	process.scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	command.Stderr = &process.stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	return process, nil
}

func (p *acpProcess) initialize() ([]byte, error) {
	return p.call("initialize", map[string]any{
		"protocolVersion": acpProtocolVersion,
		"clientCapabilities": map[string]any{
			"fs": map[string]bool{
				"readTextFile":  false,
				"writeTextFile": false,
			},
			"terminal": false,
		},
		"clientInfo": map[string]string{
			"name":    "alpha-nexus",
			"title":   "Alpha-Nexus",
			"version": "1",
		},
	}, nil)
}

func (p *acpProcess) call(method string, params any, onMessage func(acpRPCMessage) error) ([]byte, error) {
	requestID := p.nextID
	p.nextID++
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}
	if _, err := p.stdin.Write(append(payload, '\n')); err != nil {
		return nil, err
	}
	for p.scanner.Scan() {
		var message acpRPCMessage
		if json.Unmarshal(p.scanner.Bytes(), &message) != nil {
			continue
		}
		if message.Method != "" {
			if len(message.ID) > 0 && string(message.ID) != "null" {
				if err := p.replyToAgent(message); err != nil {
					return nil, err
				}
			}
			if onMessage != nil {
				if err := onMessage(message); err != nil {
					return nil, err
				}
			}
			continue
		}
		if strings.TrimSpace(string(message.ID)) != strconv.Itoa(requestID) {
			continue
		}
		if message.Error != nil {
			return nil, fmt.Errorf("ACP_%d", message.Error.Code)
		}
		return append([]byte(nil), message.Result...), nil
	}
	if err := p.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func (p *acpProcess) replyToAgent(message acpRPCMessage) error {
	response := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(message.ID),
	}
	if message.Method == "session/request_permission" {
		var request struct {
			Options []struct {
				OptionID string `json:"optionId"`
				Kind     string `json:"kind"`
			} `json:"options"`
		}
		_ = json.Unmarshal(message.Params, &request)
		for _, option := range request.Options {
			if option.Kind == "allow_once" && option.OptionID != "" {
				// ACP can only reach the per-run Alpha-Nexus MCP server here.
				// Business authorization remains enforced by ToolBroker.
				response["result"] = map[string]any{"outcome": map[string]string{
					"outcome":  "selected",
					"optionId": option.OptionID,
				}}
				return p.write(response)
			}
		}
		response["result"] = map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}
		return p.write(response)
	}
	response["error"] = map[string]any{
		"code":    -32601,
		"message": "client capability unavailable",
	}
	return p.write(response)
}

func (p *acpProcess) write(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = p.stdin.Write(append(raw, '\n'))
	return err
}

func (p *acpProcess) close() {
	if p == nil || p.command == nil {
		return
	}
	_ = p.stdin.Close()
	if p.command.Process != nil {
		_ = p.command.Process.Kill()
	}
	_ = p.command.Wait()
}
