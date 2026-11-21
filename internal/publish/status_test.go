package publish

import "testing"

func TestCanTransitionHappyPath(t *testing.T) {
	path := []Status{
		StatusQueued, StatusPreflight, StatusBrowserAttached,
		StatusAssetProcessing, StatusAssetUploaded, StatusMetadataFilled,
		StatusSubmitting, StatusPublished,
	}
	for i := 0; i < len(path)-1; i++ {
		if !CanTransition(path[i], path[i+1]) {
			t.Fatalf("expected %s → %s", path[i], path[i+1])
		}
	}
}

func TestCanTransitionScheduleAndAPIPath(t *testing.T) {
	if !CanTransition(StatusPreflight, StatusAPIAuthenticated) {
		t.Fatal("preflight → api_authenticated")
	}
	if !CanTransition(StatusAPIAuthenticated, StatusAssetProcessing) {
		t.Fatal("api_authenticated → asset_processing")
	}
	if !CanTransition(StatusMetadataFilled, StatusAwaitingReview) {
		t.Fatal("metadata_filled → awaiting_review")
	}
	if !CanTransition(StatusAwaitingReview, StatusSubmitting) {
		t.Fatal("awaiting_review → submitting")
	}
	if !CanTransition(StatusAwaitingReview, StatusCancelled) {
		t.Fatal("awaiting_review → cancelled")
	}
	if !CanTransition(StatusAwaitingReview, StatusManualTakeover) {
		t.Fatal("awaiting_review → manual_takeover")
	}
}

func TestCanTransitionDryRun(t *testing.T) {
	if !CanTransition(StatusPreflight, StatusDryRunOK) {
		t.Fatal("preflight → dry_run_ok")
	}
}

func TestCanTransitionReject(t *testing.T) {
	if CanTransition(StatusQueued, StatusPublished) {
		t.Fatal("queued → published should be illegal")
	}
	if _, err := Transition(StatusPublished, StatusQueued); err == nil {
		t.Fatal("published is terminal for re-queue without explicit resume path")
	}
}

func TestIsTerminal(t *testing.T) {
	if !IsTerminal(StatusDryRunOK) || !IsTerminal(StatusManualTakeover) ||
		!IsTerminal(StatusResultUnknown) || !IsTerminal(StatusPublishedVerified) ||
		!IsTerminal(StatusScheduledVerified) || !IsTerminal(StatusLoginRequired) {
		t.Fatal("expected terminals")
	}
	if IsTerminal(StatusPreflight) {
		t.Fatal("preflight not terminal")
	}
	if IsTerminal(StatusAwaitingReview) {
		t.Fatal("awaiting_review is not terminal (can still submit)")
	}
	if IsTerminal(StatusSubmittedPendingVerification) {
		t.Fatal("submitted_pending_verification requires creator-backend reconciliation")
	}
}

func TestCreatorBackendEvidenceTransitionsRemainOneWay(t *testing.T) {
	for _, from := range []Status{StatusSubmittedPendingVerification, StatusResultUnknown} {
		for _, to := range []Status{StatusPublishedVerified, StatusScheduledVerified} {
			if !CanTransition(from, to) {
				t.Fatalf("expected creator-backend transition %s → %s", from, to)
			}
		}
	}
	if CanTransition(StatusResultUnknown, StatusQueued) {
		t.Fatal("result_unknown must not be auto-requeued")
	}
	if CanTransition(StatusLoginRequired, StatusSubmitting) {
		t.Fatal("login_required must not bypass a fresh preflight")
	}
}
