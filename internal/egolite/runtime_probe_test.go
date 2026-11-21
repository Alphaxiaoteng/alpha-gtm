package egolite

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

type probeRunner struct {
	mu    sync.Mutex
	calls int
}

func (runner *probeRunner) Run(_ context.Context, _ string, result any) error {
	runner.mu.Lock()
	runner.calls++
	runner.mu.Unlock()
	return json.Unmarshal([]byte(`{"ok":true,"latencyMs":12}`), result)
}

func TestRuntimeProbeRunsEachSpaceIndependently(t *testing.T) {
	runner := &probeRunner{}
	result, err := NewRuntimeProbe(runner).Probe(context.Background(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if result.Failures != 0 || len(result.Latencies) != 4 || runner.calls != 4 {
		t.Fatalf("result=%+v calls=%d", result, runner.calls)
	}
}
