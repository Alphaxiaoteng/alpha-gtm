// Package publish implements the AgentPush-aligned publish task state machine
// and runner on the Alpha-Nexus Go stack.
package publish

import "fmt"

// Status mirrors the publish task states used by Alpha-Nexus.
type Status string

const (
	StatusQueued                       Status = "queued"
	StatusPreflight                    Status = "preflight"
	StatusBrowserAttached              Status = "browser_attached"
	StatusAPIAuthenticated             Status = "api_authenticated"
	StatusAssetProcessing              Status = "asset_processing"
	StatusAssetUploaded                Status = "asset_uploaded"
	StatusMetadataFilled               Status = "metadata_filled"
	StatusAwaitingReview               Status = "awaiting_review"
	StatusSubmitting                   Status = "submitting"
	StatusLoginRequired                Status = "login_required"
	StatusSubmittedPendingVerification Status = "submitted_pending_verification"
	StatusPublishedVerified            Status = "published_verified"
	StatusScheduledVerified            Status = "scheduled_verified"
	// StatusPublished is retained for legacy adapter and record compatibility.
	// New creator-backend evidence transitions use StatusPublishedVerified.
	StatusPublished       Status = "published"
	StatusRetryableFailed Status = "retryable_failed"
	StatusPermanentFailed Status = "permanent_failed"
	StatusResultUnknown   Status = "result_unknown"
	StatusManualTakeover  Status = "manual_takeover"
	StatusCancelled       Status = "cancelled"
	StatusDryRunOK        Status = "dry_run_ok"
)

// transitions: from -> allowed next statuses.
var transitions = map[Status][]Status{
	StatusQueued: {
		StatusPreflight, StatusLoginRequired, StatusCancelled, StatusPermanentFailed,
	},
	StatusPreflight: {
		StatusBrowserAttached, StatusAPIAuthenticated, StatusDryRunOK,
		StatusLoginRequired, StatusRetryableFailed, StatusPermanentFailed, StatusCancelled, StatusManualTakeover,
		// Adapter may report a later handoff without the runner walking every mid-step.
		StatusAwaitingReview, StatusMetadataFilled,
	},
	StatusBrowserAttached: {
		StatusAssetProcessing, StatusMetadataFilled, StatusLoginRequired, StatusManualTakeover,
		StatusRetryableFailed, StatusPermanentFailed, StatusCancelled,
	},
	StatusAPIAuthenticated: {
		StatusAssetProcessing, StatusMetadataFilled, StatusLoginRequired, StatusManualTakeover,
		StatusRetryableFailed, StatusPermanentFailed, StatusCancelled,
	},
	StatusAssetProcessing: {
		StatusAssetUploaded, StatusMetadataFilled, StatusLoginRequired, StatusManualTakeover,
		StatusRetryableFailed, StatusPermanentFailed, StatusCancelled,
	},
	StatusAssetUploaded: {
		StatusMetadataFilled, StatusLoginRequired, StatusManualTakeover,
		StatusRetryableFailed, StatusPermanentFailed, StatusCancelled,
	},
	StatusMetadataFilled: {
		StatusAwaitingReview, StatusSubmitting, StatusLoginRequired, StatusManualTakeover,
		StatusRetryableFailed, StatusPermanentFailed, StatusCancelled,
	},
	StatusAwaitingReview: {
		StatusSubmitting, StatusLoginRequired, StatusCancelled, StatusManualTakeover,
		StatusRetryableFailed, StatusPermanentFailed,
	},
	StatusSubmitting: {
		StatusSubmittedPendingVerification, StatusResultUnknown, StatusLoginRequired,
		StatusPublished, StatusRetryableFailed, StatusPermanentFailed, StatusManualTakeover,
	},
	StatusSubmittedPendingVerification: {
		StatusPublishedVerified, StatusScheduledVerified, StatusResultUnknown,
	},
	StatusResultUnknown: {
		// A creator-backend inspection may reconcile an uncertain final click,
		// but the job is never eligible for an automatic retry or requeue.
		StatusPublishedVerified, StatusScheduledVerified,
	},
	StatusLoginRequired: {
		// Login completion is external to this state machine. A new preflight
		// is required before any later submission attempt.
		StatusQueued, StatusCancelled,
	},
	StatusRetryableFailed: {
		StatusQueued, StatusCancelled, StatusPermanentFailed,
	},
	// terminals / resume edges
	StatusPublished:         {},
	StatusPublishedVerified: {},
	StatusScheduledVerified: {},
	StatusPermanentFailed:   {},
	StatusManualTakeover:    {StatusQueued, StatusBrowserAttached, StatusCancelled},
	StatusCancelled:         {},
	StatusDryRunOK:          {},
}

// CanTransition reports whether from → to is allowed.
func CanTransition(from, to Status) bool {
	if from == to {
		return true
	}
	for _, n := range transitions[from] {
		if n == to {
			return true
		}
	}
	return false
}

// Transition validates and returns to, or error.
func Transition(from, to Status) (Status, error) {
	if !CanTransition(from, to) {
		return from, fmt.Errorf("invalid status transition %s → %s", from, to)
	}
	return to, nil
}

// IsTerminal is true when no further automatic work should run.
func IsTerminal(s Status) bool {
	switch s {
	case StatusPublished, StatusPublishedVerified, StatusScheduledVerified,
		StatusPermanentFailed, StatusResultUnknown, StatusLoginRequired,
		StatusCancelled, StatusDryRunOK, StatusManualTakeover:
		return true
	default:
		// awaiting_review is not terminal: user/agent may still approve and submit.
		return false
	}
}
