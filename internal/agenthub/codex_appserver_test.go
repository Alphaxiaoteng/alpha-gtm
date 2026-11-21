package agenthub

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeCodexAppServerRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type fakeCodexAppServerProcess struct {
	inR  *io.PipeReader
	inW  *io.PipeWriter
	outR *io.PipeReader
	outW *io.PipeWriter
	done chan struct{}

	onRequest func(*fakeCodexAppServerProcess, fakeCodexAppServerRequest)

	writeMu       sync.Mutex
	receivedMu    sync.Mutex
	received      []fakeCodexAppServerRequest
	outputOnce    sync.Once
	terminateOnce sync.Once
}

func newFakeCodexAppServerProcess(onRequest func(*fakeCodexAppServerProcess, fakeCodexAppServerRequest)) *fakeCodexAppServerProcess {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	process := &fakeCodexAppServerProcess{
		inR: inR, inW: inW, outR: outR, outW: outW,
		done: make(chan struct{}), onRequest: onRequest,
	}
	go process.serve()
	return process
}

func (p *fakeCodexAppServerProcess) serve() {
	scanner := bufio.NewScanner(p.inR)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var request fakeCodexAppServerRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			continue
		}
		request.ID = append(json.RawMessage(nil), request.ID...)
		request.Params = append(json.RawMessage(nil), request.Params...)
		request.Result = append(json.RawMessage(nil), request.Result...)
		request.Error = append(json.RawMessage(nil), request.Error...)
		p.receivedMu.Lock()
		p.received = append(p.received, request)
		p.receivedMu.Unlock()
		if p.onRequest != nil {
			p.onRequest(p, request)
		}
	}
	p.closeOutput()
	close(p.done)
}

func (p *fakeCodexAppServerProcess) Stdin() io.WriteCloser { return p.inW }
func (p *fakeCodexAppServerProcess) Stdout() io.ReadCloser { return p.outR }

func (p *fakeCodexAppServerProcess) Wait() error {
	<-p.done
	return nil
}

func (p *fakeCodexAppServerProcess) Terminate() error {
	p.terminateOnce.Do(func() {
		_ = p.inR.Close()
		p.closeOutput()
	})
	return nil
}

func (p *fakeCodexAppServerProcess) closeOutput() {
	p.outputOnce.Do(func() { _ = p.outW.Close() })
}

func (p *fakeCodexAppServerProcess) write(value any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return json.NewEncoder(p.outW).Encode(value)
}

func (p *fakeCodexAppServerProcess) respond(id json.RawMessage, result any) error {
	return p.write(struct {
		ID     json.RawMessage `json:"id"`
		Result any             `json:"result"`
	}{ID: id, Result: result})
}

func (p *fakeCodexAppServerProcess) notify(method string, params any) error {
	return p.write(struct {
		Method string `json:"method"`
		Params any    `json:"params"`
	}{Method: method, Params: params})
}

func (p *fakeCodexAppServerProcess) serverRequest(id uint64, method string, params any) error {
	return p.write(struct {
		ID     uint64 `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{ID: id, Method: method, Params: params})
}

func (p *fakeCodexAppServerProcess) requests() []fakeCodexAppServerRequest {
	p.receivedMu.Lock()
	defer p.receivedMu.Unlock()
	result := make([]fakeCodexAppServerRequest, len(p.received))
	copy(result, p.received)
	return result
}

func appServerInitializeResult() map[string]any {
	return map[string]any{"userAgent": "codex-test"}
}

func TestCodexAppServerRunnerStreamsAndPinsReadOnlyBroker(t *testing.T) {
	workDir := t.TempDir()
	skillRoot := t.TempDir()
	skillDir := filepath.Join(skillRoot, "one-click-publish")
	if err := os.Mkdir(skillDir, 0700); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("---\nname: one-click-publish\ndescription: reviewed\n---\nPlan only."), 0600); err != nil {
		t.Fatal(err)
	}
	const brokerToken = "run-secret-token"
	var process *fakeCodexAppServerProcess
	process = newFakeCodexAppServerProcess(func(p *fakeCodexAppServerProcess, request fakeCodexAppServerRequest) {
		switch request.Method {
		case "initialize":
			_ = p.respond(request.ID, appServerInitializeResult())
		case "thread/start":
			_ = p.respond(request.ID, map[string]any{"thread": map[string]any{"id": "thr_1"}})
		case "turn/start":
			_ = p.respond(request.ID, map[string]any{"turn": map[string]any{"id": "turn_1", "status": "inProgress"}})
			_ = p.notify("item/agentMessage/delta", map[string]any{"threadId": "thr_1", "turnId": "turn_1", "itemId": "item_1", "delta": "READY"})
			_ = p.notify("item/completed", map[string]any{"threadId": "thr_1", "turnId": "turn_1", "item": map[string]any{"id": "item_1", "type": "agentMessage", "text": "READY"}})
			_ = p.notify("turn/completed", map[string]any{"threadId": "thr_1", "turn": map[string]any{"id": "turn_1", "status": "completed"}})
		}
	})
	runner := codexAppServerRunner{
		binary: func() string { return "/opt/codex/bin/codex" },
		processFactory: func(context.Context, string, RunRequest) (codexAppServerProcess, error) {
			return process, nil
		},
	}
	request := RunRequest{
		ProviderID: ProviderCodex, ModelID: "gpt-5", Message: "hello", WorkDir: workDir,
		RunID: "run_1", BrokerEndpoint: "http://127.0.0.1:17321", BrokerToken: brokerToken, Executable: "alpha-nexus",
		SkillRoot: skillRoot, Skills: []SkillInput{{Name: "one-click-publish", Path: skillPath}},
	}
	var text strings.Builder
	sessionID, err := runner.Run(context.Background(), request, func(event ProviderEvent) {
		if event.Type == "message.delta" {
			text.WriteString(event.Payload["text"].(string))
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "thr_1" || text.String() != "READY" {
		t.Fatalf("session=%q text=%q", sessionID, text.String())
	}
	received := process.requests()
	var methods []string
	var threadStart, turnStart fakeCodexAppServerRequest
	for _, request := range received {
		if request.Method != "" {
			methods = append(methods, request.Method)
		}
		switch request.Method {
		case "thread/start":
			threadStart = request
		case "turn/start":
			turnStart = request
		}
	}
	if !reflect.DeepEqual(methods, []string{"initialize", "initialized", "thread/start", "turn/start"}) {
		t.Fatalf("methods=%v", methods)
	}
	var threadParams codexAppServerThreadStartParams
	if err := json.Unmarshal(threadStart.Params, &threadParams); err != nil {
		t.Fatal(err)
	}
	if threadParams.CWD != workDir || threadParams.Model != "gpt-5" || threadParams.ApprovalPolicy != "never" || threadParams.Sandbox != "read-only" {
		t.Fatalf("thread params=%+v", threadParams)
	}
	server, ok := threadParams.Config.MCPServers["alpha_nexus"]
	if !ok || server.Command != "alpha-nexus" || !reflect.DeepEqual(server.Args, []string{"agent", "mcp-broker", "--endpoint", request.BrokerEndpoint, "--run-id", request.RunID}) || !reflect.DeepEqual(server.EnvVars, []string{"ALPHA_NEXUS_AGENT_RUN_TOKEN"}) || !server.Required || server.DefaultToolsApprovalMode != "auto" {
		t.Fatalf("broker config=%+v", threadParams.Config)
	}
	var turnParams codexAppServerTurnStartParams
	if err := json.Unmarshal(turnStart.Params, &turnParams); err != nil {
		t.Fatal(err)
	}
	if turnParams.ThreadID != "thr_1" || turnParams.ClientUserMessageID != "run_1" || len(turnParams.Input) != 2 || turnParams.Input[0].Type != "skill" || turnParams.Input[0].Name != "one-click-publish" || turnParams.Input[0].Path != skillPath || !strings.Contains(turnParams.Input[1].Text, "User: hello") {
		t.Fatalf("turn params=%+v", turnParams)
	}
	var wire strings.Builder
	for _, line := range received {
		wire.Write(line.Params)
	}
	if strings.Contains(wire.String(), brokerToken) {
		t.Fatalf("broker token leaked into app-server params")
	}
}

func TestCodexAppServerRunnerResumesThread(t *testing.T) {
	var process *fakeCodexAppServerProcess
	process = newFakeCodexAppServerProcess(func(p *fakeCodexAppServerProcess, request fakeCodexAppServerRequest) {
		switch request.Method {
		case "initialize":
			_ = p.respond(request.ID, appServerInitializeResult())
		case "thread/resume":
			_ = p.respond(request.ID, map[string]any{"thread": map[string]any{"id": "thr_resumed"}})
		case "turn/start":
			_ = p.respond(request.ID, map[string]any{"turn": map[string]any{"id": "turn_2", "status": "inProgress"}})
			_ = p.notify("item/completed", map[string]any{"threadId": "thr_resumed", "turnId": "turn_2", "item": map[string]any{"id": "item_2", "type": "agent_message", "text": "RESUMED"}})
			_ = p.notify("turn/completed", map[string]any{"threadId": "thr_resumed", "turn": map[string]any{"id": "turn_2", "status": "completed"}})
		}
	})
	runner := codexAppServerRunner{
		binary:         func() string { return "/opt/codex/bin/codex" },
		processFactory: func(context.Context, string, RunRequest) (codexAppServerProcess, error) { return process, nil },
	}
	request := RunRequest{ModelID: "gpt-5", SessionID: "thr_previous", Message: "continue", WorkDir: t.TempDir()}
	var text strings.Builder
	sessionID, err := runner.Run(context.Background(), request, func(event ProviderEvent) {
		text.WriteString(event.Payload["text"].(string))
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "thr_resumed" || text.String() != "RESUMED" {
		t.Fatalf("session=%q text=%q", sessionID, text.String())
	}
	var resume fakeCodexAppServerRequest
	for _, request := range process.requests() {
		if request.Method == "thread/resume" {
			resume = request
		}
	}
	var params codexAppServerThreadResumeParams
	if err := json.Unmarshal(resume.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.ThreadID != "thr_previous" || params.ApprovalPolicy != "never" || params.Sandbox != "read-only" {
		t.Fatalf("resume params=%+v", params)
	}
}

func TestCodexAppServerRunnerDeclinesShellAndFileApprovals(t *testing.T) {
	var process *fakeCodexAppServerProcess
	var repliesMu sync.Mutex
	replies := map[string]string{}
	process = newFakeCodexAppServerProcess(func(p *fakeCodexAppServerProcess, request fakeCodexAppServerRequest) {
		switch request.Method {
		case "initialize":
			_ = p.respond(request.ID, appServerInitializeResult())
		case "thread/start":
			_ = p.respond(request.ID, map[string]any{"thread": map[string]any{"id": "thr_approval"}})
		case "turn/start":
			_ = p.respond(request.ID, map[string]any{"turn": map[string]any{"id": "turn_approval", "status": "inProgress"}})
			_ = p.serverRequest(101, "item/commandExecution/requestApproval", map[string]any{"command": []string{"echo", "no"}})
			_ = p.serverRequest(102, "item/fileChange/requestApproval", map[string]any{"reason": "no"})
		case "":
			var response codexAppServerApprovalDecisionResponse
			if len(request.Result) == 0 || json.Unmarshal(request.Result, &response) != nil {
				return
			}
			repliesMu.Lock()
			replies[string(request.ID)] = response.Decision
			complete := len(replies) == 2
			repliesMu.Unlock()
			if complete {
				_ = p.notify("turn/completed", map[string]any{"threadId": "thr_approval", "turn": map[string]any{"id": "turn_approval", "status": "completed"}})
			}
		}
	})
	runner := codexAppServerRunner{
		binary:         func() string { return "/opt/codex/bin/codex" },
		processFactory: func(context.Context, string, RunRequest) (codexAppServerProcess, error) { return process, nil },
	}
	if _, err := runner.Run(context.Background(), RunRequest{Message: "approval", WorkDir: t.TempDir()}, func(ProviderEvent) {}); err != nil {
		t.Fatal(err)
	}
	repliesMu.Lock()
	defer repliesMu.Unlock()
	if !reflect.DeepEqual(replies, map[string]string{"101": "decline", "102": "decline"}) {
		t.Fatalf("replies=%v", replies)
	}
}

func TestCodexAppServerRunnerInterruptsTurnOnCancellation(t *testing.T) {
	turnStarted := make(chan struct{})
	var turnStartedOnce sync.Once
	interrupts := make(chan codexAppServerTurnInterruptParams, 1)
	process := newFakeCodexAppServerProcess(func(p *fakeCodexAppServerProcess, request fakeCodexAppServerRequest) {
		switch request.Method {
		case "initialize":
			_ = p.respond(request.ID, appServerInitializeResult())
		case "thread/start":
			_ = p.respond(request.ID, map[string]any{"thread": map[string]any{"id": "thr_cancel"}})
		case "turn/start":
			_ = p.respond(request.ID, map[string]any{"turn": map[string]any{"id": "turn_cancel", "status": "inProgress"}})
			turnStartedOnce.Do(func() { close(turnStarted) })
		case "turn/interrupt":
			var params codexAppServerTurnInterruptParams
			if json.Unmarshal(request.Params, &params) == nil {
				interrupts <- params
			}
			_ = p.respond(request.ID, map[string]any{})
		}
	})
	runner := codexAppServerRunner{
		binary:         func() string { return "/opt/codex/bin/codex" },
		processFactory: func(context.Context, string, RunRequest) (codexAppServerProcess, error) { return process, nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan struct {
		session string
		err     error
	}, 1)
	go func() {
		session, err := runner.Run(ctx, RunRequest{Message: "cancel", WorkDir: t.TempDir()}, func(ProviderEvent) {})
		result <- struct {
			session string
			err     error
		}{session: session, err: err}
	}()
	select {
	case <-turnStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not start")
	}
	cancel()
	select {
	case outcome := <-result:
		if outcome.session != "thr_cancel" || !errors.Is(outcome.err, context.Canceled) {
			t.Fatalf("outcome=%+v", outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not return after cancellation")
	}
	select {
	case params := <-interrupts:
		if params.ThreadID != "thr_cancel" || params.TurnID != "turn_cancel" {
			t.Fatalf("interrupt params=%+v", params)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("turn/interrupt was not sent")
	}
}

func TestCodexAppServerRunnerInterruptsWhenCancellationRacesWithTurnStart(t *testing.T) {
	turnStartReceived := make(chan struct{})
	interrupts := make(chan codexAppServerTurnInterruptParams, 1)
	process := newFakeCodexAppServerProcess(func(p *fakeCodexAppServerProcess, request fakeCodexAppServerRequest) {
		switch request.Method {
		case "initialize":
			_ = p.respond(request.ID, appServerInitializeResult())
		case "thread/start":
			_ = p.respond(request.ID, map[string]any{"thread": map[string]any{"id": "thr_race"}})
		case "turn/start":
			_ = p.notify("turn/started", map[string]any{"threadId": "thr_race", "turn": map[string]any{"id": "turn_race", "status": "inProgress"}})
			close(turnStartReceived)
		case "turn/interrupt":
			var params codexAppServerTurnInterruptParams
			if json.Unmarshal(request.Params, &params) == nil {
				interrupts <- params
			}
			_ = p.respond(request.ID, map[string]any{})
		}
	})
	runner := codexAppServerRunner{
		binary:         func() string { return "/opt/codex/bin/codex" },
		processFactory: func(context.Context, string, RunRequest) (codexAppServerProcess, error) { return process, nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, RunRequest{Message: "cancel", WorkDir: t.TempDir()}, func(ProviderEvent) {})
		result <- err
	}()
	select {
	case <-turnStartReceived:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("turn/start was not received")
	}
	select {
	case params := <-interrupts:
		if params.ThreadID != "thr_race" || params.TurnID != "turn_race" {
			t.Fatalf("interrupt params=%+v", params)
		}
	case <-time.After(time.Second):
		t.Fatal("turn/interrupt was not sent for the turn/start cancellation race")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v, want context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not return after interrupting the raced turn")
	}
}

func TestCodexAppServerListsModelsAndSkills(t *testing.T) {
	modelProcess := newFakeCodexAppServerProcess(func(p *fakeCodexAppServerProcess, request fakeCodexAppServerRequest) {
		switch request.Method {
		case "initialize":
			_ = p.respond(request.ID, appServerInitializeResult())
		case "model/list":
			_ = p.respond(request.ID, map[string]any{"data": []map[string]any{
				{"id": "gpt-5", "model": "gpt-5", "hidden": false},
				{"id": "hidden", "model": "hidden", "hidden": true},
				{"id": "gpt-5-duplicate", "model": "gpt-5", "hidden": false},
				{"id": "invalid", "model": "bad model", "hidden": false},
				{"id": "fallback", "hidden": false},
			}})
		}
	})
	modelRunner := codexAppServerRunner{
		binary:         func() string { return "/opt/codex/bin/codex" },
		processFactory: func(context.Context, string, RunRequest) (codexAppServerProcess, error) { return modelProcess, nil },
	}
	models, err := modelRunner.listModels(context.Background(), RunRequest{WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(models, []string{"gpt-5", "fallback"}) {
		t.Fatalf("models=%v", models)
	}

	skillProcess := newFakeCodexAppServerProcess(func(p *fakeCodexAppServerProcess, request fakeCodexAppServerRequest) {
		switch request.Method {
		case "initialize":
			_ = p.respond(request.ID, appServerInitializeResult())
		case "skills/list":
			_ = p.respond(request.ID, map[string]any{"data": []map[string]any{{
				"cwd": "/tmp/work", "skills": []map[string]any{{"name": "alpha", "description": "reviewed", "path": "/tmp/work/SKILL.md", "enabled": true}}, "errors": []any{},
			}}})
		}
	})
	skillRunner := codexAppServerRunner{
		binary:         func() string { return "/opt/codex/bin/codex" },
		processFactory: func(context.Context, string, RunRequest) (codexAppServerProcess, error) { return skillProcess, nil },
	}
	entries, err := skillRunner.listSkills(context.Background(), RunRequest{WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(entries[0].Skills) != 1 || entries[0].Skills[0].Name != "alpha" || entries[0].Skills[0].Description != "reviewed" {
		t.Fatalf("skills=%+v", entries)
	}
}

func TestCodexAppServerRunnerRejectsUnsafeSessionCombinations(t *testing.T) {
	started := false
	runner := codexAppServerRunner{
		binary: func() string { return "/opt/codex/bin/codex" },
		processFactory: func(context.Context, string, RunRequest) (codexAppServerProcess, error) {
			started = true
			return nil, nil
		},
	}
	if _, err := runner.Run(context.Background(), RunRequest{WorkDir: t.TempDir(), SessionID: "existing", Ephemeral: true}, nil); err == nil {
		t.Fatal("expected ephemeral resume rejection")
	}
	if started {
		t.Fatal("process started for rejected request")
	}
	if _, err := runner.Run(context.Background(), RunRequest{WorkDir: "relative", Message: "bad"}, nil); !errors.Is(err, errCodexAppServerWorkDir) {
		t.Fatalf("relative workdir error=%v", err)
	}
	if _, err := runner.Run(context.Background(), RunRequest{WorkDir: t.TempDir(), SkillRoot: t.TempDir(), Skills: []SkillInput{{Name: "bad", Path: "/private/tmp/outside/SKILL.md"}}}, nil); err == nil {
		t.Fatal("expected out-of-root skill rejection")
	}
}
