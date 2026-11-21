package egolite

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type RuntimeProbe struct {
	runner ProgramRunner
	serial atomic.Uint64
}

func NewRuntimeProbe(runner ProgramRunner) *RuntimeProbe {
	return &RuntimeProbe{runner: runner}
}

func (probe *RuntimeProbe) Probe(ctx context.Context, concurrency int) (ProbeResult, error) {
	if probe == nil || probe.runner == nil || concurrency <= 0 {
		return ProbeResult{}, fmt.Errorf("EGO_CALIBRATION_INPUT_INVALID")
	}
	result := ProbeResult{Latencies: make([]time.Duration, concurrency)}
	var wait sync.WaitGroup
	var mu sync.Mutex
	for index := range concurrency {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			name, _ := json.Marshal(fmt.Sprintf("alpha-nexus-calibration-%d", probe.serial.Add(1)))
			program := fmt.Sprintf(`const started = Date.now()
const task = await useOrCreateTaskSpace(%s)
await openOrReuseTab("data:text/html,<form><input aria-label='calibration'/></form>", { wait: true, timeout: 10 })
await snapshotText({ interactive: true })
await completeTaskSpace(task.id, { keep: false })
cliLog(%q + JSON.stringify({ ok: true, latencyMs: Date.now() - started }))
`, name, resultPrefix)
			var output struct {
				OK        bool  `json:"ok"`
				LatencyMS int64 `json:"latencyMs"`
			}
			started := time.Now()
			err := probe.runner.Run(ctx, program, &output)
			latency := time.Since(started)
			if output.LatencyMS > 0 {
				latency = time.Duration(output.LatencyMS) * time.Millisecond
			}
			mu.Lock()
			defer mu.Unlock()
			result.Latencies[index] = latency
			if err != nil || !output.OK {
				result.Failures++
			}
		}(index)
	}
	wait.Wait()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}
