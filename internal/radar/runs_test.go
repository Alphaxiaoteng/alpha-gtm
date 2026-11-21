package radar

import (
	"path/filepath"
	"testing"
)

func TestRunLifecyclePersistsCallsAndDerivesState(t *testing.T) {
	dataDir := t.TempDir()
	run := Run{
		ID: "run-20260101t000000-abcd", NicheID: "niche-a", NicheName: "A", Platform: "xhs",
		EndpointID: "xhs.note_search", Terms: []string{"t1", "t2"}, ItemCap: 10, MaxCalls: 8,
	}
	started, err := StartRun(dataDir, run)
	if err != nil {
		t.Fatal(err)
	}
	if started.State != RunStateRunning || started.TotalCalls != 0 {
		t.Fatalf("unexpected start state: %+v", started)
	}
	if _, err := StartRun(dataDir, run); err == nil {
		t.Fatal("starting the same run twice must be refused")
	}
	if _, err := AppendRunCall(dataDir, run.ID, RunCall{
		CallID: 7, Term: "t1", Platform: "xhs", EndpointID: "xhs.note_search",
		Status: "succeeded", ItemCount: 25, KeptCount: 10, DurationMS: 900,
	}); err != nil {
		t.Fatal(err)
	}
	finished, err := FinishRun(dataDir, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != RunStatePartial {
		t.Fatalf("one skipped term must be partial, got %s", finished.State)
	}
	if finished.Succeeded != 1 || finished.Failed != 0 || finished.Skipped != 1 {
		t.Fatalf("unexpected counters: %+v", finished)
	}
	if finished.Items != 25 || finished.Kept != 10 {
		t.Fatalf("unexpected item totals: %+v", finished)
	}
	loaded, err := LoadRun(dataDir, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Calls) != 1 || loaded.Calls[0].CallID != 7 {
		t.Fatalf("call was not persisted: %+v", loaded.Calls)
	}
	latest, ok, err := LatestRunForNiche(dataDir, "niche-a")
	if err != nil || !ok || latest.ID != run.ID {
		t.Fatalf("latest run lookup failed: %+v ok=%v err=%v", latest, ok, err)
	}
	runs, err := ListRuns(dataDir, 0)
	if err != nil || len(runs) != 1 {
		t.Fatalf("list runs failed: %d %v", len(runs), err)
	}
}

func TestRunRejectsPathTraversalIDs(t *testing.T) {
	dataDir := t.TempDir()
	for _, id := range []string{"../escape", "Run-UPPER", "short", "run/../x", ""} {
		if _, err := StartRun(dataDir, Run{ID: id, Terms: []string{"a"}}); err == nil {
			t.Fatalf("run id %q must be rejected", id)
		}
	}
	if _, err := LoadRun(dataDir, "../app.db"); err == nil {
		t.Fatal("loading with a traversal id must be rejected")
	}
}

func TestRunDerivesFailedWhenEveryCallFails(t *testing.T) {
	dataDir := t.TempDir()
	run := Run{ID: "run-20260101t000001-abcd", NicheID: "n", Platform: "xhs", Terms: []string{"t1"}}
	if _, err := StartRun(dataDir, run); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendRunCall(dataDir, run.ID, RunCall{Term: "t1", Status: "failed", ErrorCode: "provider_request_failed"}); err != nil {
		t.Fatal(err)
	}
	finished, err := FinishRun(dataDir, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != RunStateFailed || finished.Failed != 1 {
		t.Fatalf("unexpected failed run: %+v", finished)
	}
	if filepath.Dir(RunDir(dataDir)) == "" {
		t.Fatal("run directory must sit under the data dir")
	}
}

func TestRunRecountTracksLatestAttemptPerTerm(t *testing.T) {
	run := Run{
		ID: "run-20260101t000002-abcd", Terms: []string{"t1", "t2", "t3"},
		Calls: []RunCall{
			{Term: "t1", Status: "succeeded", ItemCount: 20, KeptCount: 10},
			{Term: "t2", Status: "failed", ErrorCode: "provider_request_failed"},
			{Term: "t2", Status: "succeeded", ItemCount: 25, KeptCount: 10},
		},
	}
	run.recount()
	if run.TotalCalls != 3 || run.Succeeded != 2 || run.Failed != 0 || run.Skipped != 1 {
		t.Fatalf("term-level totals = %+v", run)
	}
	if run.Items != 45 || run.Kept != 20 {
		t.Fatalf("attempt totals = %+v", run)
	}
	if state := run.deriveState(); state != RunStatePartial {
		t.Fatalf("state = %s, want %s because t3 never ran", state, RunStatePartial)
	}
	if pending := run.PendingRetryTerms(); len(pending) != 0 {
		t.Fatalf("a failure that a retry fixed must not stay pending: %v", pending)
	}
}

func TestRunPendingRetryTermsKeepsOnlyUnresolvedFailures(t *testing.T) {
	run := Run{Terms: []string{"t1", "t2", "t3"}, Calls: []RunCall{
		{Term: "t1", Status: "succeeded"},
		{Term: "t2", Status: "failed"},
		{Term: "t3", Status: "failed"},
		{Term: "t3", Status: "succeeded"},
	}}
	pending := run.PendingRetryTerms()
	if len(pending) != 1 || pending[0] != "t2" {
		t.Fatalf("pending terms = %v, want [t2]", pending)
	}
}

func TestFinishRetryKeepsFinishTimeAndAddsRetryCost(t *testing.T) {
	dataDir := t.TempDir()
	run := Run{
		ID: "run-20260101t000003-wxyz", NicheID: "n", NicheName: "N", Platform: "xhs",
		EndpointID: "xhs.note_search", Terms: []string{"t1", "t2"}, ItemCap: 10,
	}
	if _, err := StartRun(dataDir, run); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendRunCall(dataDir, run.ID, RunCall{Term: "t1", Status: "succeeded", ItemCount: 5, KeptCount: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendRunCall(dataDir, run.ID, RunCall{Term: "t2", Status: "failed", ErrorCode: "provider_request_failed"}); err != nil {
		t.Fatal(err)
	}
	first, err := FinishRun(dataDir, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != RunStatePartial {
		t.Fatalf("first state = %s, want partial", first.State)
	}
	if _, err := AppendRunCall(dataDir, run.ID, RunCall{Term: "t2", Status: "succeeded", ItemCount: 9, KeptCount: 9}); err != nil {
		t.Fatal(err)
	}
	sealed, err := FinishRetry(dataDir, run.ID, 0.01)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.FinishedAt != first.FinishedAt {
		t.Fatalf("retry must keep the original finish time: %q vs %q", sealed.FinishedAt, first.FinishedAt)
	}
	if sealed.LastRetryAt == "" {
		t.Fatal("retry stamp missing")
	}
	if sealed.State != RunStateSucceeded || sealed.Succeeded != 2 || sealed.Failed != 0 || sealed.Skipped != 0 {
		t.Fatalf("sealed state = %+v", sealed)
	}
	if sealed.TotalCalls != 3 || sealed.Items != 14 || sealed.Kept != 14 {
		t.Fatalf("sealed totals = %+v", sealed)
	}
	if sealed.EstimatedCostUSD != 0.01 {
		t.Fatalf("retry cost = %v, want 0.01", sealed.EstimatedCostUSD)
	}
}
