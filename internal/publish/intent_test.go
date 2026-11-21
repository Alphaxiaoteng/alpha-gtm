package publish

import "testing"

func TestExecutionIntentRoundTripAndLegacyFallback(t *testing.T) {
	tests := []struct {
		dryRun, autoSubmit bool
		mode               string
	}{
		{dryRun: true, autoSubmit: true, mode: ModeDryRun},
		{mode: ModePrepare},
		{autoSubmit: true, mode: ModeSubmit},
	}
	for _, test := range tests {
		if got := ExecutionMode(test.dryRun, test.autoSubmit); got != test.mode {
			t.Fatalf("ExecutionMode(%v,%v)=%q", test.dryRun, test.autoSubmit, got)
		}
		gotDryRun, gotAutoSubmit := IntentFromMode(test.mode)
		wantAutoSubmit := test.autoSubmit && !test.dryRun
		if gotDryRun != test.dryRun || gotAutoSubmit != wantAutoSubmit {
			t.Fatalf("IntentFromMode(%q)=(%v,%v)", test.mode, gotDryRun, gotAutoSubmit)
		}
	}
	if dryRun, autoSubmit := IntentFromMode("browser"); dryRun || autoSubmit {
		t.Fatalf("legacy browser mode must remain review-only: (%v,%v)", dryRun, autoSubmit)
	}
}
