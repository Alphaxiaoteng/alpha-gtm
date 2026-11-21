package batch

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/db"
)

// PublishStatusDisposition is the batch-facing meaning of a publish runner
// or existing publish job status. A human handoff is still unfinished work:
// it must remain visible and resumable, never be counted as success.
type PublishStatusDisposition struct {
	ItemStatus             string
	Event                  string
	Success                bool
	WaitingHuman           bool
	PendingVerification    bool
	RequiresReconciliation bool
}

// ClassifyPublishStatus maps publish statuses onto the durable batch item
// state and event vocabulary. Only a trusted dry-run or publish success is a
// batch success; manual states are explicit handoffs.
func ClassifyPublishStatus(status string) PublishStatusDisposition {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "dry_run_ok", "published", "published_verified", "scheduled_verified", "success":
		return PublishStatusDisposition{ItemStatus: "success", Event: "batch.item.success", Success: true}
	case "manual_takeover":
		return PublishStatusDisposition{ItemStatus: "manual_takeover", Event: "batch.item.handoff", WaitingHuman: true}
	case "awaiting_review":
		return PublishStatusDisposition{ItemStatus: "awaiting_review", Event: "batch.item.awaiting_review", WaitingHuman: true}
	case "login_required":
		return PublishStatusDisposition{ItemStatus: "login_required", Event: "batch.item.login_required", WaitingHuman: true}
	case "submitted_pending_verification":
		return PublishStatusDisposition{ItemStatus: "submitted_pending_verification", Event: "batch.item.verification_pending", PendingVerification: true}
	case "result_unknown":
		return PublishStatusDisposition{ItemStatus: "result_unknown", Event: "batch.item.result_unknown", RequiresReconciliation: true}
	default:
		return PublishStatusDisposition{ItemStatus: "failed", Event: "batch.item.failed"}
	}
}

// PublishBatchCounts is the authoritative recount used to close or suspend a
// publish batch after its item states have been persisted.
type PublishBatchCounts struct {
	Pending             int
	Success             int
	Failed              int
	WaitingCount        int
	WaitingHuman        bool
	LoginRequired       int
	VerificationPending int
	ResultUnknown       int
}

// RecountPublishBatchItems keeps human handoffs in the pending/waiting
// bucket. This prevents UpdateBatchRunCounters from marking an unfinished
// batch completed.
func RecountPublishBatchItems(items []db.BatchItem) PublishBatchCounts {
	var counts PublishBatchCounts
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Status)) {
		case "success", "dry_run_ok", "published", "published_verified", "scheduled_verified":
			counts.Success++
		case "failed":
			counts.Failed++
		case "pending", "running":
			counts.Pending++
		case "manual_takeover", "awaiting_review", "login_required":
			counts.Pending++
			counts.WaitingCount++
			counts.WaitingHuman = true
			if item.Status == "login_required" {
				counts.LoginRequired++
			}
		case "submitted_pending_verification":
			counts.Pending++
			counts.VerificationPending++
		case "result_unknown":
			counts.Pending++
			counts.ResultUnknown++
		}
	}
	return counts
}

// PublishBatchSummaryEvent keeps an unfinished human handoff out of the
// completed-batch event stream.
func PublishBatchSummaryEvent(counts PublishBatchCounts) string {
	if counts.WaitingHuman {
		return "batch.waiting_human"
	}
	if counts.VerificationPending > 0 || counts.ResultUnknown > 0 {
		return "batch.verification_pending"
	}
	return "batch.finished"
}

// PublishCSVRow is one line from batch publish content-file.
// CSV header: content_id,platform,target_id,account_id
type PublishCSVRow struct {
	ContentID int64
	Platform  string
	TargetID  int64 // asset_id
	AccountID int64
	LineNum   int
}

// ItemIsResumable reports whether a batch item status should be re-run on --resume.
// A running item may still belong to a live worker and is never auto-reset.
func ItemIsResumable(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending", "failed", "interrupted", "retryable", "retryable_failed", "manual_takeover", "awaiting_review":
		return true
	default:
		return false
	}
}

// FilterResumableItems returns only items that --resume should re-run.
func FilterResumableItems(items []db.BatchItem) []db.BatchItem {
	out := make([]db.BatchItem, 0, len(items))
	for _, it := range items {
		if ItemIsResumable(it.Status) {
			out = append(out, it)
		}
	}
	return out
}

// FindLatestPublishBatch picks the newest batch_run with operation publish for platform.
// contentFile, when non-empty, prefers a run whose settings_json contains that path.
func FindLatestPublishBatch(runs []db.BatchRun, platformCode, contentFile string) *db.BatchRun {
	var fallback *db.BatchRun
	for i := range runs {
		r := &runs[i]
		if r.OperationType != "publish" {
			continue
		}
		if platformCode != "" && r.PlatformCode != platformCode {
			continue
		}
		if contentFile != "" && strings.Contains(r.SettingsJSON, contentFile) {
			return r
		}
		if fallback == nil {
			// ListBatchRuns is id DESC; first match is latest.
			fallback = r
		}
	}
	return fallback
}

// ParsePublishCSV reads content_id,platform,target_id,account_id rows.
// First line is treated as header if it does not start with a digit.
func ParsePublishCSV(r io.Reader) ([]PublishCSVRow, error) {
	sc := bufio.NewScanner(r)
	lineNum := 0
	var rows []PublishCSVRow
	for sc.Scan() {
		lineNum++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := splitCSVLine(line)
		if len(fields) == 0 {
			continue
		}
		// Header row
		if lineNum == 1 && !isDigits(fields[0]) {
			continue
		}
		if len(fields) < 3 {
			return nil, fmt.Errorf("line %d: need content_id,platform,target_id at least", lineNum)
		}
		contentID, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64)
		if err != nil || contentID <= 0 {
			return nil, fmt.Errorf("line %d: invalid content_id %q", lineNum, fields[0])
		}
		platform := strings.TrimSpace(fields[1])
		targetID, err := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
		if err != nil || targetID <= 0 {
			return nil, fmt.Errorf("line %d: invalid target_id %q", lineNum, fields[2])
		}
		var accountID int64
		if len(fields) > 3 && strings.TrimSpace(fields[3]) != "" {
			accountID, err = strconv.ParseInt(strings.TrimSpace(fields[3]), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid account_id %q", lineNum, fields[3])
			}
		}
		rows = append(rows, PublishCSVRow{
			ContentID: contentID,
			Platform:  platform,
			TargetID:  targetID,
			AccountID: accountID,
			LineNum:   lineNum,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func splitCSVLine(line string) []string {
	// Minimal CSV split (no quoted commas in V1 contract).
	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isDigits(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// JobIsResumable reports whether a publish job DB status can be re-run via publish resume.
func JobIsResumable(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "manual_takeover", "failed", "retryable_failed", "permanent_failed",
		"awaiting_review", "cancelled", "pending":
		return true
	default:
		// success / published / dry_run_ok are terminal successes
		return false
	}
}
