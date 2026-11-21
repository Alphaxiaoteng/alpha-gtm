package agenthub

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Manual integration probe: runs only when DSH_INTEGRATION=1 so CI and the
// default suite stay hermetic.
func TestDshRunnerRealRuntimeManual(t *testing.T) {
	if os.Getenv("DSH_INTEGRATION") != "1" {
		t.Skip("set DSH_INTEGRATION=1 to run the real dsh runtime probe")
	}
	resolve := func() string { return resolvePath("dsh") }
	runner := dshRunner{binary: resolve}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var response strings.Builder
	sessionID, err := runner.Run(ctx, RunRequest{
		ProviderID: ProviderDsh,
		ModelID:    "gemini-3.8-flash",
		Message:    "Reply with exactly ALPHA-TEST-123 and nothing else.",
		WorkDir:    t.TempDir(),
		Ephemeral:  true,
	}, func(event ProviderEvent) {
		if text := eventText(event); text != "" {
			response.WriteString(text)
			t.Logf("delta: %q", text)
		}
	})
	t.Logf("sessionID=%q response=%q err=%v", sessionID, response.String(), err)
	if err != nil {
		t.Fatalf("dsh run failed: %v", err)
	}
	if strings.TrimSpace(response.String()) != "ALPHA-TEST-123" {
		t.Fatalf("response %q", response.String())
	}
}

// Manual: proves a continuation turn runs on the pooled process with prior
// context, instead of hanging on the cross-process attach black hole.
func TestDshRunnerRealContinuationManual(t *testing.T) {
	if os.Getenv("DSH_INTEGRATION") != "1" {
		t.Skip("set DSH_INTEGRATION=1 to run the real dsh runtime probe")
	}
	runner := &dshRunner{binary: func() string { return resolvePath("dsh") }}
	defer runner.CloseProcesses()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var first strings.Builder
	sessionID, err := runner.Run(ctx, RunRequest{
		ProviderID: ProviderDsh,
		ModelID:    "gemini-3.8-flash",
		Message:    "记住暗号：菠萝披萨。只回复：已记住",
		WorkDir:    t.TempDir(),
	}, func(event ProviderEvent) {
		first.WriteString(eventText(event))
	})
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	t.Logf("first session=%q reply=%q", sessionID, strings.TrimSpace(first.String()))
	var second strings.Builder
	_, err = runner.Run(ctx, RunRequest{
		ProviderID: ProviderDsh,
		ModelID:    "gemini-3.8-flash",
		Message:    "我刚才说的暗号是什么？只回复暗号本身",
		WorkDir:    t.TempDir(),
		SessionID:  sessionID,
	}, func(event ProviderEvent) {
		second.WriteString(eventText(event))
	})
	if err != nil {
		t.Fatalf("continuation turn: %v", err)
	}
	t.Logf("continuation reply=%q", strings.TrimSpace(second.String()))
	if !strings.Contains(second.String(), "菠萝披萨") {
		t.Fatalf("continuation lost context: %q", second.String())
	}
}
