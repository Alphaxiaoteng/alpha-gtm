package agenthub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// dshRunner drives a DeepSeek Harness runtime (`dsh --profile <name>`) over
// the SDK's newline-delimited JSON-RPC stdio protocol.
//
// The runtime only emits live session events for sessions created inside its
// own process: re-attaching a persisted session from a fresh process silently
// drops every notification, including the idle transition that ends a turn.
// Continuation therefore requires one persistent runtime process per session,
// so the runner keeps a process pool keyed by session id and routes every
// turn of a conversation through the same process. Processes idle past
// dshProcessIdleTTL are recycled on the next Run.
type dshRunner struct {
	binary  func() string
	profile func() string
	command func(context.Context, string, ...string) *exec.Cmd
	now     func() time.Time

	poolOnce sync.Once
	pool     *dshProcessPool
}

const (
	dshProcessIdleTTL     = 15 * time.Minute
	dshFrameForwardExpiry = 30 * time.Second
	dshProcessExitTimeout = 2 * time.Second
)

// dshStdinWriteTimeout bounds one stdin frame write. A runtime that stops
// draining stdin would otherwise block the write indefinitely and hold a lock
// hostage, so on expiry the pipe is closed and the write fails instead of
// wedging the pool sweeper and CloseProcesses. Variable only so tests can
// shorten it.
var dshStdinWriteTimeout = 10 * time.Second

// eventText extracts the streamed text payload from a provider event.
func eventText(event ProviderEvent) string {
	if event.Type != "message.delta" {
		return ""
	}
	text, _ := event.Payload["text"].(string)
	return text
}

type dshFrame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *dshRPCError    `json:"error"`
	Params json.RawMessage `json:"params"`
}

type dshRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type dshSessionEventParams struct {
	SessionID string `json:"sessionId"`
	Event     struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	} `json:"event"`
}

type dshStatusParams struct {
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
}

type dshAssistantData struct {
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// dshAttemptData is the streaming shape the SDK actually emits per step:
// assistant/attempt carries a list of chunks rather than a finished message.
type dshAttemptData struct {
	Stream []struct {
		Type  string `json:"type"`
		Chunk struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"chunk"`
	} `json:"stream"`
}

// dshTurnEndData closes one prompt turn and carries the terminal reason.
type dshTurnEndData struct {
	Reason struct {
		Kind  string `json:"kind"`
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
		Failure struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"failure"`
	} `json:"reason"`
}

// dshToolCallData is the harness's own tool invocation: the runtime runs the
// tool itself and only reports that it happened.
type dshToolCallData struct {
	CallID    string `json:"callId"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// dshToolResultData settles one tool invocation; the result text arrives in
// the message content blocks.
type dshToolResultData struct {
	Message struct {
		ToolCallID string `json:"toolCallId"`
		Name       string `json:"name"`
		IsError    bool   `json:"isError"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// dshToolResultJSON renders the result content blocks as a compact JSON
// string; empty content yields an empty string so the row stays lean.
func dshToolResultJSON(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	texts := make([]string, 0, len(content))
	for _, block := range content {
		if block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	if len(texts) == 0 {
		return ""
	}
	encoded, err := json.Marshal(map[string]any{"text": strings.Join(texts, "\n")})
	if err != nil {
		return ""
	}
	return string(encoded)
}

type dshSubscriber struct {
	frames chan dshFrame
}

// dshProcess is one persistent runtime process serving a single session.
type dshProcess struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	session  string
	mu       sync.Mutex
	sub      *dshSubscriber
	lastUsed time.Time
	closed   bool
	// resumedSessions tracks which session ids this process has attached via
	// session/resume, so a resumed conversation is only re-attached once.
	resumedSessions map[string]bool
	// writeMu serialises stdin frame writes. It is never held together with
	// mu, so a stalled pipe write cannot block state readers.
	writeMu sync.Mutex
	// reaped is closed once cmd.Wait has returned, i.e. the child is no longer
	// a zombie; waitErr then holds its exit status.
	reaped  chan struct{}
	waitErr error
}

func (p *dshProcess) forward(frame dshFrame) {
	p.mu.Lock()
	sub := p.sub
	p.mu.Unlock()
	if sub == nil {
		return
	}
	timer := time.NewTimer(dshFrameForwardExpiry)
	defer timer.Stop()
	select {
	case sub.frames <- frame:
	case <-timer.C:
	}
}

func (p *dshProcess) subscribe() *dshSubscriber {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sub = &dshSubscriber{frames: make(chan dshFrame, 64)}
	return p.sub
}

func (p *dshProcess) unsubscribe() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sub = nil
}

func (p *dshProcess) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *dshProcess) writeFrame(payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if p.isClosed() {
		return errors.New("AGENT_PROVIDER_DSH_FAILED: runtime already closed")
	}
	// The write is serialised by writeMu, not mu: holding the state lock across
	// a pipe write lets a runtime that stops reading stdin stall forward,
	// subscribe and close indefinitely, and any sweeper holding the pool lock
	// behind them.
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	stdin := p.stdin
	var timedOut atomic.Bool
	timer := time.AfterFunc(dshStdinWriteTimeout, func() {
		timedOut.Store(true)
		p.close() // closing stdin unblocks the pending write
	})
	_, err = stdin.Write(append(encoded, '\n'))
	timer.Stop()
	if err != nil {
		if timedOut.Load() {
			return fmt.Errorf("AGENT_PROVIDER_DSH_FAILED: stdin write timed out after %s", dshStdinWriteTimeout)
		}
		return err
	}
	return nil
}

func (p *dshProcess) close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	stdin := p.stdin
	p.mu.Unlock()
	// stdin EOF triggers the runtime's bounded successful shutdown flow.
	_ = stdin.Close()
}

// finish records the child's exit status. The stdout pump calls it exactly once
// after cmd.Wait returns, which is what reaps the process instead of leaving a
// zombie for the lifetime of the pool.
func (p *dshProcess) finish(err error) {
	p.mu.Lock()
	p.waitErr = err
	p.mu.Unlock()
	if p.reaped != nil {
		close(p.reaped)
	}
}

// exitResult reports the recorded wait error and whether the child was reaped.
func (p *dshProcess) exitResult() (error, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	reaped := false
	select {
	case <-p.reaped:
		reaped = true
	default:
	}
	return p.waitErr, reaped
}

// waitExit blocks until the child is reaped or the timeout expires.
func (p *dshProcess) waitExit(timeout time.Duration) bool {
	if p.reaped == nil {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.reaped:
		return true
	case <-timer.C:
		return false
	}
}

// shutdown closes stdin and waits (bounded) for the runtime to exit so its
// process is reaped; a runtime that ignores stdin EOF is killed.
func (p *dshProcess) shutdown(timeout time.Duration) {
	p.close()
	if p.waitExit(timeout) {
		return
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	p.waitExit(timeout)
}

type dshProcessPool struct {
	mu       sync.Mutex
	sessions map[string]*dshProcess
	// spawnMu serialises the lookup-spawn-write-back window of acquire so two
	// concurrent turns for the same new session cannot both spawn (the loser's
	// write-back would overwrite the winner and leak a runtime).
	spawnMu sync.Mutex
}

func (p *dshProcessPool) sweep(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, proc := range p.sessions {
		proc.mu.Lock()
		stale := now.Sub(proc.lastUsed) > dshProcessIdleTTL
		proc.mu.Unlock()
		if stale {
			proc.close()
			delete(p.sessions, id)
		}
	}
}

func (r *dshRunner) poolOf() *dshProcessPool {
	r.poolOnce.Do(func() {
		r.pool = &dshProcessPool{sessions: map[string]*dshProcess{}}
	})
	return r.pool
}

// CloseProcesses shuts every pooled runtime process (test and shutdown hook).
func (r *dshRunner) CloseProcesses() {
	if r.pool == nil {
		return
	}
	r.pool.mu.Lock()
	processes := make([]*dshProcess, 0, len(r.pool.sessions))
	for id, proc := range r.pool.sessions {
		processes = append(processes, proc)
		delete(r.pool.sessions, id)
	}
	r.pool.mu.Unlock()
	for _, proc := range processes {
		// Wait for the exit so shutdown does not leave zombies behind.
		proc.shutdown(dshProcessExitTimeout)
	}
}

func (r *dshRunner) Run(ctx context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	// Chat-only: the dsh runtime never receives broker credentials.
	request.BrokerEndpoint = ""
	request.BrokerToken = ""
	request.Executable = ""
	binary := strings.TrimSpace(r.binary())
	if binary == "" {
		return "", errProviderUnavailable
	}
	now := r.now
	if now == nil {
		now = time.Now
	}

	// The dsh runtime rejects unknown provider names at initialize, so a
	// "provider/model" id is only honoured when the DSH catalog publishes that
	// exact route; anything else falls back to the deployment default.
	provider, model := dshRouteForModel(request.ModelID)
	if provider == "" {
		provider = string(request.ProviderID)
	}
	if model == "" {
		model = request.ModelID
	}

	pool := r.poolOf()
	pool.sweep(now())

	if request.Ephemeral {
		return r.runEphemeral(ctx, binary, r.profileValue(), provider, model, request, emit)
	}

	sessionID := request.SessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("alpha-%s-%d", request.RunID, now().UnixNano())
	}
	proc, err := pool.acquire(r, sessionID, request.WorkDir, provider, model)
	if err != nil {
		return sessionID, err
	}
	proc.mu.Lock()
	proc.lastUsed = now()
	proc.mu.Unlock()

	if err := proc.turn(ctx, sessionID, request.Message, emit); err != nil {
		return sessionID, err
	}
	return sessionID, nil
}

// runEphemeral keeps model validation hermetic: one dedicated process, one
// fresh session, torn down as soon as the turn settles.
func (r *dshRunner) runEphemeral(ctx context.Context, binary, profile, provider, model string, request RunRequest, emit func(ProviderEvent)) (string, error) {
	sessionID := fmt.Sprintf("alpha-ephemeral-%d", time.Now().UnixNano())
	proc, err := spawnDshProcess(r, binary, profile, sessionID, request.WorkDir, provider, model)
	if err != nil {
		return sessionID, err
	}
	// Tear the dedicated runtime down and reap it: an ephemeral process must not
	// outlive the run or linger as a zombie.
	defer proc.shutdown(dshProcessExitTimeout)
	if err := proc.turn(ctx, sessionID, request.Message, emit); err != nil {
		return sessionID, err
	}
	return sessionID, nil
}

func spawnDshProcess(r *dshRunner, binary, profile, sessionID, workDir, provider, model string) (*dshProcess, error) {
	commandFactory := r.command
	if commandFactory == nil {
		commandFactory = exec.CommandContext
	}
	// The pooled process outlives a single run: it must not die with one
	// request's context, so spawn on a background context.
	command := commandFactory(context.Background(), binary, "--profile", profile)
	configureProcess(command)
	command.Dir = workDir
	if len(command.Env) == 0 {
		command.Env = os.Environ()
	} else {
		command.Env = append(command.Env, os.Environ()...)
	}
	stdin, stdinErr := command.StdinPipe()
	if stdinErr != nil {
		return nil, stdinErr
	}
	stdout, stdoutErr := command.StdoutPipe()
	if stdoutErr != nil {
		return nil, stdoutErr
	}
	// The child writes stderr from its own goroutine while the pump reads it on
	// exit, so the buffer must be lock-protected: a bytes.Buffer copied by value
	// shares its backing array and races.
	stderr := &lockedBuffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return nil, errProviderUnavailable
	}
	proc := &dshProcess{
		cmd:      command,
		stdin:    stdin,
		session:  sessionID,
		lastUsed: time.Now(),
		reaped:   make(chan struct{}),
	}
	go r.pump(stdout, proc, stderr)
	if err := proc.handshake(provider, model); err != nil {
		proc.close()
		return nil, err
	}
	return proc, nil
}

// lockedBuffer is a concurrency-safe bytes.Buffer for child process stderr.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (r *dshRunner) pump(stdout io.Reader, proc *dshProcess, stderr *lockedBuffer) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var frame dshFrame
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		proc.forward(frame)
	}
	// Stdout is drained, so cmd.Wait may now run: exec documents that Wait must
	// not be called before all reads from the pipe complete. This is the only
	// place the child is reaped; without it every runtime stays a zombie.
	if proc.cmd != nil {
		proc.finish(proc.cmd.Wait())
	}
	// Runtime exited: wake any waiting subscriber with a synthetic exit frame
	// so a pending turn fails instead of hanging on the deadline.
	encodedStderr, _ := json.Marshal(stderr.String())
	proc.forward(dshFrame{Method: "dsh/exit", Params: json.RawMessage(`{"stderr":` + string(encodedStderr) + `}`)})
}

func (p *dshProcess) handshake(provider, model string) error {
	sub := p.subscribe()
	defer p.unsubscribe()
	if err := p.writeFrame(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"cwd":      "",
			"provider": provider,
			"model":    model,
		},
	}); err != nil {
		return err
	}
	for {
		select {
		case frame := <-sub.frames:
			if frame.Method == "dsh/exit" {
				return errors.New("AGENT_PROVIDER_DSH_FAILED: runtime exited during initialize")
			}
			if frame.Error != nil {
				return fmt.Errorf("AGENT_PROVIDER_DSH_HANDSHAKE: %s", frame.Error.Message)
			}
			if len(frame.ID) > 0 && string(frame.ID) != "null" {
				return nil
			}
		case <-time.After(dshFrameForwardExpiry):
			return errors.New("AGENT_PROVIDER_DSH_HANDSHAKE: initialize timed out")
		}
	}
}

// turn streams one prompt through the persistent process until idle. The first
// turn of a session inside this process resumes it first: persisted sessions
// must be re-attached after the runtime restarts ("session ... already exists"
// otherwise), and brand-new sessions answer "not found", which is harmless —
// the following prompt creates them either way.
func (p *dshProcess) turn(ctx context.Context, sessionID, message string, emit func(ProviderEvent)) error {
	if !p.resumed(sessionID) {
		// A failed resume is deliberately not fatal: unknown sessions simply
		// have nothing to attach, and the prompt below reports real failures.
		_ = p.resumeSession(ctx, sessionID)
	}
	return p.turnOnce(ctx, sessionID, message, emit)
}

// resumed reports whether this process has already attached the session.
func (p *dshProcess) resumed(sessionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resumedSessions[sessionID]
}

// resumeSession re-attaches a persisted session inside this runtime process.
// Failure is not fatal for a brand-new session that nothing persisted yet, so
// only transport-level errors propagate; the runtime's own error answer (e.g.
// unknown session) surfaces on the subsequent prompt, if any.
func (p *dshProcess) resumeSession(ctx context.Context, sessionID string) error {
	sub := p.subscribe()
	defer p.unsubscribe()
	if err := p.writeFrame(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "session/resume",
		"params":  map[string]any{"sessionId": sessionID},
	}); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame := <-sub.frames:
			if frame.Method == "dsh/exit" {
				return errors.New("AGENT_PROVIDER_DSH_FAILED: runtime exited during session resume")
			}
			if frame.Error != nil {
				// Unknown sessions are expected for brand-new conversations;
				// marking the session as seen keeps later turns from repeating
				// the futile resume.
				p.mu.Lock()
				if p.resumedSessions == nil {
					p.resumedSessions = map[string]bool{}
				}
				p.resumedSessions[sessionID] = true
				p.mu.Unlock()
				return fmt.Errorf("AGENT_PROVIDER_DSH_RESUME: %s", frame.Error.Message)
			}
			if len(frame.ID) > 0 && string(frame.ID) != "null" {
				p.mu.Lock()
				if p.resumedSessions == nil {
					p.resumedSessions = map[string]bool{}
				}
				p.resumedSessions[sessionID] = true
				p.mu.Unlock()
				return nil
			}
		case <-time.After(dshFrameForwardExpiry):
			return errors.New("AGENT_PROVIDER_DSH_RESUME: session resume timed out")
		}
	}
}

func (p *dshProcess) turnOnce(ctx context.Context, sessionID, message string, emit func(ProviderEvent)) error {
	sub := p.subscribe()
	defer p.unsubscribe()

	if err := p.writeFrame(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "session/prompt",
		"params": map[string]any{
			"sessionId":     sessionID,
			"contentBlocks": []map[string]any{{"type": "text", "text": message}},
		},
	}); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame := <-sub.frames:
			if frame.Method == "dsh/exit" {
				return errors.New("AGENT_PROVIDER_DSH_FAILED: runtime exited mid-turn")
			}
			if frame.Error != nil {
				return fmt.Errorf("AGENT_PROVIDER_DSH_TURN: %s", frame.Error.Message)
			}
			switch frame.Method {
			case "session.event":
				var params dshSessionEventParams
				if err := json.Unmarshal(frame.Params, &params); err != nil {
					continue
				}
					switch params.Event.Type {
				case "assistant/message":
					var data dshAssistantData
					if err := json.Unmarshal(params.Event.Data, &data); err == nil && data.Message.Role == "assistant" {
						for _, block := range data.Message.Content {
							if block.Type == "text" && block.Text != "" {
								emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": block.Text}})
							}
						}
					}
				case "tool/call":
					// The dsh runtime executes tools with its own harness (bash,
					// file access, ...) instead of proposing through the broker.
					// Surface what actually ran so the conversation timeline and
					// the agent_tool_calls read model stay truthful.
					var data dshToolCallData
					if err := json.Unmarshal(params.Event.Data, &data); err == nil && data.Name != "" {
						emit(ProviderEvent{Type: "tool.observed", Payload: map[string]any{
							"call_id":     data.CallID,
							"tool_name":   data.Name,
							"params_json": data.Arguments,
							"status":      "running",
							"created_at":  time.Now().UTC().Format(time.RFC3339Nano),
						}})
					}
				case "tool/result":
					var data dshToolResultData
					if err := json.Unmarshal(params.Event.Data, &data); err == nil && data.Message.ToolCallID != "" {
						status := "completed"
						if data.Message.IsError {
							status = "failed"
						}
						emit(ProviderEvent{Type: "tool.observed", Payload: map[string]any{
							"call_id":     data.Message.ToolCallID,
							"tool_name":   data.Message.Name,
							"params_json": "",
							"result_json": dshToolResultJSON(data.Message.Content),
							"status":      status,
							"error_code":  "",
						}})
					}
				case "assistant/attempt":
					// Streamed text arrives chunk by chunk; the terminal result
					// is signalled by turn/end below.
					var attempt dshAttemptData
					if err := json.Unmarshal(params.Event.Data, &attempt); err == nil {
						for _, item := range attempt.Stream {
							if item.Chunk.Type == "text" && item.Chunk.Text != "" {
								emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": item.Chunk.Text}})
							}
						}
					}
				case "turn/end":
					var end dshTurnEndData
					if err := json.Unmarshal(params.Event.Data, &end); err != nil {
						return nil
					}
					message := end.Reason.Error.Message
					if message == "" {
						message = end.Reason.Failure.Message
					}
					if end.Reason.Kind == "error" && message != "" {
						// Provider messages may embed upstream bodies but never
						// our credentials, so surface them for diagnosis.
						return fmt.Errorf("AGENT_PROVIDER_DSH_TURN: %s", message)
					}
					return nil
				}
			case "session.status":
				var status dshStatusParams
				if err := json.Unmarshal(frame.Params, &status); err == nil && status.Status == "idle" {
					return nil
				}
			}
		}
	}
}

func (p *dshProcessPool) acquire(r *dshRunner, sessionID, workDir, provider, model string) (*dshProcess, error) {
	if existing := p.openProcess(sessionID); existing != nil {
		return existing, nil
	}
	// Only one caller may create a runtime for a session at a time; the rest
	// wait here and pick up the process it published.
	p.spawnMu.Lock()
	defer p.spawnMu.Unlock()
	if existing := p.openProcess(sessionID); existing != nil {
		return existing, nil
	}
	proc, err := spawnDshProcess(r, strings.TrimSpace(r.binary()), r.profileValue(), sessionID, workDir, provider, model)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.sessions == nil {
		p.sessions = map[string]*dshProcess{}
	}
	p.sessions[sessionID] = proc
	p.mu.Unlock()
	return proc, nil
}

// openProcess returns the pooled, still-open runtime for a session, dropping a
// closed one from the pool.
func (p *dshProcessPool) openProcess(sessionID string) *dshProcess {
	p.mu.Lock()
	existing := p.sessions[sessionID]
	p.mu.Unlock()
	if existing == nil || existing.isClosed() {
		if existing != nil {
			p.mu.Lock()
			if p.sessions[sessionID] == existing {
				delete(p.sessions, sessionID)
			}
			p.mu.Unlock()
		}
		return nil
	}
	return existing
}

func (r *dshRunner) profileValue() string {
	if r.profile != nil && strings.TrimSpace(r.profile()) != "" {
		return strings.TrimSpace(r.profile())
	}
	return "alpha"
}
