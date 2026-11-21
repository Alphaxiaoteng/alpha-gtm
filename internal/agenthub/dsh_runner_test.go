package agenthub

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// The dsh runner speaks the newline-delimited JSON-RPC SDK protocol with a
// persistent `dsh --profile <name>` process per session: initialize handshake
// once, one session/prompt per turn, streamed assistant text, idle ends the
// turn while the process stays pooled for continuation.

const dshPersistentFake = `
const fs = require('fs');
const chunks = [];
let turns = 0;
process.stdin.setEncoding('utf8');
let buffer = '';
process.stdin.on('data', (chunk) => {
  buffer += chunk;
  let index;
  while ((index = buffer.indexOf('\n')) >= 0) {
    const line = buffer.slice(0, index).trim();
    buffer = buffer.slice(index + 1);
    if (!line) continue;
    let frame;
    try { frame = JSON.parse(line); } catch { continue; }
    if (frame.method === 'initialize') {
      process.stdout.write(JSON.stringify({jsonrpc: '2.0', id: frame.id, result: {result: {serverInfo: {name: 'deepseek-harness-sdk-runtime', version: 'test'}}}}) + '\n');
    } else if (frame.method === 'session/prompt') {
      const sessionId = frame.params.sessionId;
      turns += 1;
      process.stdout.write(JSON.stringify({jsonrpc: '2.0', id: frame.id, result: {messageId: 'm-' + turns}}) + '\n');
      process.stdout.write(JSON.stringify({jsonrpc: '2.0', method: 'session.status', params: {sessionId, status: 'running'}}) + '\n');
      process.stdout.write(JSON.stringify({jsonrpc: '2.0', method: 'session.event', params: {sessionId, event: {type: 'assistant/message', data: {message: {role: 'assistant', content: [{type: 'text', text: 'TURN-' + turns}]}}}}}) + '\n');
      process.stdout.write(JSON.stringify({jsonrpc: '2.0', method: 'session.status', params: {sessionId, status: 'idle'}}) + '\n');
    }
  }
});
process.stdin.on('end', () => {
  if (process.env.DSH_STDIN_CAPTURE) {
    fs.writeFileSync(process.env.DSH_STDIN_CAPTURE, Buffer.concat(chunks));
    process.stderr.write('FAKE_DEBUG chunks=' + chunks.length + '\n');
  }
  process.exit(0);
});
process.on('exit', () => {
  if (process.env.DSH_STDIN_CAPTURE && chunks.length > 0) {
    try { fs.writeFileSync(process.env.DSH_STDIN_CAPTURE, Buffer.concat(chunks)); } catch {}
  }
});
setInterval(() => {}, 1000);
`

func dshTestRunner(stdinPath string, spawnCount *int, mu *sync.Mutex) *dshRunner {
	return &dshRunner{
		binary:  func() string { return "dsh" },
		profile: func() string { return "alpha" },
		command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			mu.Lock()
			*spawnCount += 1
			mu.Unlock()
			command := exec.CommandContext(ctx, "node", "-e", dshPersistentFake)
			if stdinPath != "" {
				command.Env = append(os.Environ(), "DSH_STDIN_CAPTURE="+stdinPath)
			}
			return command
		},
	}
}

func waitForFile(t *testing.T, path string) []byte {
	t.Helper()
	for attempt := 0; attempt < 30; attempt++ {
		raw, err := os.ReadFile(path)
		if err == nil {
			return raw
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("stdin capture never appeared: %s", path)
	return nil
}

func TestDshRunnerHandshakesAndStreamsAssistantText(t *testing.T) {
	var spawnCount int
	var mu sync.Mutex
	var text strings.Builder
	workDir := t.TempDir()
	runner := dshTestRunner("", &spawnCount, &mu)
	defer runner.CloseProcesses()

	sessionID, err := runner.Run(context.Background(), RunRequest{
		WorkDir:   workDir,
		Message:   "列出浏览器身份",
		RunID:     "run-9",
		ModelID:   "deepseek-v4.1-flash",
		SessionID: "alpha-prev",
	}, func(event ProviderEvent) {
		text.WriteString(eventText(event))
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessionID != "alpha-prev" {
		t.Fatalf("session id %q, want the requested continuation id", sessionID)
	}
	if text.String() != "TURN-1" {
		t.Fatalf("streamed text %q, want TURN-1", text.String())
	}
	if spawnCount != 1 {
		t.Fatalf("spawn count %d, want 1", spawnCount)
	}
	runner.CloseProcesses()
	mu.Lock()
	spawns := spawnCount
	mu.Unlock()
	if spawns != 1 {
		t.Fatalf("spawn count %d, want 1", spawns)
	}
}

// Continuation must reuse the pooled runtime process: a fresh process would
// re-attach the persisted session and never observe any events (the idle
// transition would never arrive and the run would hang).
func TestDshRunnerReusesProcessForContinuation(t *testing.T) {
	var spawnCount int
	var mu sync.Mutex
	workDir := t.TempDir()
	runner := dshTestRunner("", &spawnCount, &mu)
	defer runner.CloseProcesses()

	var firstText strings.Builder
	firstID, err := runner.Run(context.Background(), RunRequest{
		WorkDir: workDir,
		Message: "第一轮",
		RunID:   "run-1",
	}, func(event ProviderEvent) {
		firstText.WriteString(eventText(event))
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstID == "" || firstText.String() != "TURN-1" {
		t.Fatalf("first turn id=%q text=%q", firstID, firstText.String())
	}

	var secondText strings.Builder
	secondID, err := runner.Run(context.Background(), RunRequest{
		WorkDir:   workDir,
		Message:   "第二轮",
		RunID:     "run-2",
		SessionID: firstID,
	}, func(event ProviderEvent) {
		secondText.WriteString(eventText(event))
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondID != firstID {
		t.Fatalf("continuation id %q, want %q", secondID, firstID)
	}
	if secondText.String() != "TURN-2" {
		t.Fatalf("continuation text %q, want TURN-2", secondText.String())
	}
	mu.Lock()
	spawns := spawnCount
	mu.Unlock()
	if spawns != 1 {
		t.Fatalf("spawn count %d, want a single persistent process", spawns)
	}
}

func TestDshRunnerEphemeralRunIsHermetic(t *testing.T) {
	var spawnCount int
	var mu sync.Mutex
	workDir := t.TempDir()
	runner := dshTestRunner("", &spawnCount, &mu)

	sessionID, err := runner.Run(context.Background(), RunRequest{
		WorkDir:   workDir,
		Message:   "Reply with exactly OK-1 and nothing else.",
		Ephemeral: true,
	}, func(event ProviderEvent) {})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sessionID, "alpha-ephemeral-") {
		t.Fatalf("ephemeral session id %q", sessionID)
	}
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	spawns := spawnCount
	mu.Unlock()
	if spawns != 1 {
		t.Fatalf("spawn count %d", spawns)
	}
}

func TestDshRunnerFailsClosedWithoutRuntime(t *testing.T) {
	runner := dshRunner{
		binary: func() string { return "" },
	}
	if _, err := runner.Run(context.Background(), RunRequest{WorkDir: t.TempDir(), Message: "hi"}, func(ProviderEvent) {}); err == nil {
		t.Fatal("expected provider unavailable without an entry")
	}
}

// Every spawned runtime must be reaped: after the stdout pump finishes,
// cmd.Wait has to run, otherwise each pooled session leaves a zombie behind and
// CloseProcesses returns without the child ever being collected.
func TestDshRunnerReapsRuntimeProcess(t *testing.T) {
	var spawnCount int
	var mu sync.Mutex
	runner := dshTestRunner("", &spawnCount, &mu)
	defer runner.CloseProcesses()

	sessionID, err := runner.Run(context.Background(), RunRequest{
		WorkDir:   t.TempDir(),
		Message:   "第一轮",
		RunID:     "run-reap",
		SessionID: "alpha-reap",
	}, func(ProviderEvent) {})
	if err != nil {
		t.Fatal(err)
	}

	pool := runner.poolOf()
	pool.mu.Lock()
	proc := pool.sessions[sessionID]
	pool.mu.Unlock()
	if proc == nil {
		t.Fatalf("session %q is not pooled", sessionID)
	}
	if _, reaped := proc.exitResult(); reaped {
		t.Fatal("runtime reported as reaped before it was asked to close")
	}

	runner.CloseProcesses()

	if _, reaped := proc.exitResult(); !reaped {
		t.Fatal("CloseProcesses returned without reaping the runtime (zombie left behind)")
	}
	if proc.cmd.ProcessState == nil {
		t.Fatal("cmd.ProcessState is nil after CloseProcesses: cmd.Wait never ran")
	}
	if !proc.cmd.ProcessState.Exited() {
		t.Fatalf("runtime state %v, want a normal exit", proc.cmd.ProcessState)
	}
}

// stallingStdin models a runtime that never drains its stdin: Write blocks until
// Close, exactly like a full pipe with no reader.
type stallingStdin struct {
	started chan struct{}
	unblock chan struct{}
	once    sync.Once
}

func (s *stallingStdin) Write(p []byte) (int, error) {
	s.once.Do(func() { close(s.started) })
	<-s.unblock
	return 0, io.ErrClosedPipe
}

func (s *stallingStdin) Close() error {
	select {
	case <-s.unblock:
	default:
		close(s.unblock)
	}
	return nil
}

// A stdin write must never hold the process state lock for good: a runtime that
// stops reading would otherwise wedge forward/subscribe/close, and any sweeper
// blocked behind them, forever.
func TestDshProcessWriteFrameTimesOutWithoutWedgingStateLock(t *testing.T) {
	previousTimeout := dshStdinWriteTimeout
	dshStdinWriteTimeout = 150 * time.Millisecond
	defer func() { dshStdinWriteTimeout = previousTimeout }()

	stdin := &stallingStdin{started: make(chan struct{}), unblock: make(chan struct{})}
	proc := &dshProcess{stdin: stdin, session: "alpha-stalled", lastUsed: time.Now(), reaped: make(chan struct{})}

	writeErr := make(chan error, 1)
	go func() {
		writeErr <- proc.writeFrame(map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "session/prompt",
		})
	}()

	select {
	case <-stdin.started:
	case <-time.After(2 * time.Second):
		t.Fatal("write never reached stdin")
	}

	stateFree := make(chan struct{})
	go func() {
		proc.subscribe()
		close(stateFree)
	}()
	select {
	case <-stateFree:
	case <-time.After(2 * time.Second):
		t.Fatal("process state lock wedged behind a stalled stdin write")
	}
	proc.unsubscribe()

	select {
	case err := <-writeErr:
		if err == nil {
			t.Fatal("stalled stdin write returned no error")
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("write error %q, want a stdin timeout", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stalled stdin write never timed out")
	}
	if !proc.isClosed() {
		t.Fatal("a timed-out runtime must be marked closed so the pool recycles it")
	}
}

// dshSlowInitRunner delays the initialize reply so concurrent acquires overlap
// inside the spawn window instead of the first one finishing before the next
// starts.
func dshSlowInitRunner(delayMS int, spawnCount *int, mu *sync.Mutex) *dshRunner {
	runner := dshTestRunner("", spawnCount, mu)
	fake := strings.Replace(
		dshPersistentFake,
		"if (frame.method === 'initialize') {",
		fmt.Sprintf("if (frame.method === 'initialize') { const until = Date.now() + %d; while (Date.now() < until) {}", delayMS),
		1,
	)
	runner.command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		mu.Lock()
		*spawnCount += 1
		mu.Unlock()
		return exec.CommandContext(ctx, "node", "-e", fake)
	}
	return runner
}

// Concurrent turns for the same new session must share one runtime: an
// unsynchronised lookup-spawn-write-back lets both callers spawn, and the later
// write-back overwrites the earlier process, leaking a runtime nobody closes.
func TestDshProcessPoolAcquireSpawnsOneRuntimePerSession(t *testing.T) {
	var spawnCount int
	var mu sync.Mutex
	runner := dshSlowInitRunner(250, &spawnCount, &mu)
	defer runner.CloseProcesses()
	pool := runner.poolOf()
	workDir := t.TempDir()

	const workers = 6
	procs := make([]*dshProcess, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			procs[index], errs[index] = pool.acquire(runner, "alpha-shared", workDir, "deepseek", "deepseek-v4.1-flash")
		}(index)
	}
	close(start)
	wg.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("acquire %d: %v", index, err)
		}
		if procs[index] != procs[0] {
			t.Fatalf("acquire %d returned a different runtime than acquire 0", index)
		}
	}
	mu.Lock()
	spawns := spawnCount
	mu.Unlock()
	if spawns != 1 {
		t.Fatalf("spawned %d runtimes for one session, want 1 (leaked process)", spawns)
	}
}
func TestDefaultRegistryIncludesDshProvider(t *testing.T) {
	registry, err := DefaultRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := registry.Get(ProviderDsh)
	if !ok {
		t.Fatal("dsh provider missing from the default registry")
	}
	if !provider.Descriptor.BuiltIn {
		t.Fatal("dsh provider must be built-in")
	}
	if provider.SupportsBroker {
		t.Fatal("dsh provider must not accept broker credentials")
	}
	if provider.Runner == nil || provider.Probe == nil {
		t.Fatal("dsh provider must carry a runner and probe")
	}
}
