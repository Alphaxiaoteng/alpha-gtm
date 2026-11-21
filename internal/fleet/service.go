// Package fleet coordinates explicit local browser actions across accounts.
// It has no scheduler: callers create and execute one durable run themselves.
package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/db"
)

const (
	maxConcurrency = 5
	maxTargets     = 10
)

type Operation string

const (
	OperationStart   Operation = "start"
	OperationStop    Operation = "stop"
	OperationObserve Operation = "observe"
)

type BrowserController interface {
	StartAccount(accountID int64, opts browser.StartOptions) (*browser.Instance, error)
	StopAccount(accountID int64) error
	Status(accountID int64) (*browser.AccountStatus, error)
}

type RunRequest struct {
	Operation   Operation
	AccountIDs  []int64
	Concurrency int
}

type Service struct {
	db      *db.DB
	browser BrowserController
}

type RunDetail struct {
	Run   *db.BatchRun
	Items []db.BatchItem
}

func NewService(database *db.DB, controller BrowserController) *Service {
	return &Service{db: database, browser: controller}
}

// Run first commits every target, then executes the recorded items. No browser
// side effect starts before the dashboard-visible batch row exists.
func (s *Service) Run(ctx context.Context, req RunRequest) (*db.BatchRun, error) {
	run, err := s.Create(req)
	if err != nil {
		return nil, err
	}
	if err := s.Execute(ctx, run.ID); err != nil {
		return run, err
	}
	return s.db.GetBatchRun(run.ID)
}

func (s *Service) Detail(runID int64) (*RunDetail, error) {
	if s == nil || s.db == nil || runID <= 0 {
		return nil, fmt.Errorf("invalid fleet run id")
	}
	run, err := s.db.GetBatchRun(runID)
	if err != nil {
		return nil, err
	}
	if _, err := operationForRun(run.OperationType); err != nil {
		return nil, err
	}
	items, err := s.db.ListBatchItems(runID)
	if err != nil {
		return nil, err
	}
	return &RunDetail{Run: run, Items: items}, nil
}

func (s *Service) Create(req RunRequest) (*db.BatchRun, error) {
	if s == nil || s.db == nil || s.browser == nil {
		return nil, fmt.Errorf("fleet service is not configured")
	}
	if req.Operation != OperationStart && req.Operation != OperationStop && req.Operation != OperationObserve {
		return nil, fmt.Errorf("unsupported fleet operation %q", req.Operation)
	}
	if len(req.AccountIDs) == 0 {
		return nil, fmt.Errorf("at least one account is required")
	}
	if len(req.AccountIDs) > maxTargets {
		return nil, fmt.Errorf("fleet accepts at most %d accounts per run", maxTargets)
	}
	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > maxConcurrency {
		return nil, fmt.Errorf("fleet concurrency must be between 1 and %d", maxConcurrency)
	}
	items := make([]db.BatchItemInput, 0, len(req.AccountIDs))
	seen := make(map[int64]struct{}, len(req.AccountIDs))
	for _, accountID := range req.AccountIDs {
		if accountID <= 0 {
			return nil, fmt.Errorf("account id must be positive")
		}
		if _, exists := seen[accountID]; exists {
			return nil, fmt.Errorf("duplicate account id %d", accountID)
		}
		seen[accountID] = struct{}{}
		account, err := s.db.GetAccount(accountID)
		if err != nil {
			return nil, err
		}
		if !account.Enabled {
			return nil, fmt.Errorf("account %d is disabled", accountID)
		}
		items = append(items, db.BatchItemInput{ItemType: "fleet_" + string(req.Operation), AccountID: accountID})
	}
	settings, err := json.Marshal(map[string]string{"operation": string(req.Operation), "source": "explicit_local"})
	if err != nil {
		return nil, fmt.Errorf("encode fleet settings: %w", err)
	}
	return s.db.CreateBatchRunWithItems("fleet_"+string(req.Operation), "", "pending", concurrency, string(settings), items)
}

func (s *Service) Execute(ctx context.Context, runID int64) error {
	if s == nil || s.db == nil || s.browser == nil {
		return fmt.Errorf("fleet service is not configured")
	}
	run, err := s.db.GetBatchRun(runID)
	if err != nil {
		return err
	}
	operation, err := operationForRun(run.OperationType)
	if err != nil {
		return err
	}
	items, err := s.db.ListBatchItems(run.ID)
	if err != nil {
		return err
	}
	workers := run.Concurrency
	if workers < 1 {
		workers = 1
	}
	if workers > maxConcurrency {
		workers = maxConcurrency
	}
	queue := make(chan db.BatchItem, len(items))
	for _, item := range items {
		if item.Status == "pending" {
			queue <- item
		}
	}
	close(queue)

	var workersWG sync.WaitGroup
	for index := 0; index < workers; index++ {
		workersWG.Add(1)
		go func() {
			defer workersWG.Done()
			for item := range queue {
				s.executeItem(ctx, operation, &item)
			}
		}()
	}
	workersWG.Wait()
	return s.updateRunState(run.ID)
}

func operationForRun(operationType string) (Operation, error) {
	switch operationType {
	case "fleet_start":
		return OperationStart, nil
	case "fleet_stop":
		return OperationStop, nil
	case "fleet_observe":
		return OperationObserve, nil
	default:
		return "", fmt.Errorf("batch run is not a fleet operation")
	}
}

func (s *Service) executeItem(ctx context.Context, operation Operation, item *db.BatchItem) {
	// Claim before taking the profile lease. A second Execute can observe the
	// same run snapshot; losing this CAS must be a no-op, never a terminal
	// update to the first executor's item.
	claimed, err := s.db.ClaimPendingBatchItem(item.ID, string(operation))
	if err != nil || !claimed {
		return
	}
	_ = s.updateRunState(item.BatchID)
	defer func() { _ = s.updateRunState(item.BatchID) }()
	owner := fmt.Sprintf("fleet:%d:item:%d", item.BatchID, item.ID)
	profileID := browser.ProfileIDForAccount(item.AccountID)
	acquired, err := s.db.AcquireBrowserOperationLease(profileID, string(operation), owner, time.Now(), 2*time.Minute)
	if err != nil {
		_ = s.db.UpdateBatchItemStatus(item.ID, "failed", "operation_lease", "OPERATION_LEASE_FAILED", "could not acquire browser operation lease")
		return
	}
	if !acquired {
		_ = s.db.UpdateBatchItemStatus(item.ID, "failed", "operation_lease", "BUSY_BY_OPERATION", "another browser operation owns this profile")
		return
	}
	defer func() { _ = s.db.ReleaseBrowserOperationLease(profileID, owner) }()
	if err := ctx.Err(); err != nil {
		_ = s.db.UpdateBatchItemStatus(item.ID, "failed", string(operation), "CANCELLED", "fleet run cancelled")
		return
	}
	switch operation {
	case OperationStart:
		if _, err := s.browser.StartAccount(item.AccountID, browser.StartOptions{HolderType: browser.HolderBatch, HolderID: owner, SkipFocus: true}); err != nil {
			_ = s.db.UpdateBatchItemStatus(item.ID, "failed", "start", "BROWSER_START_FAILED", "browser start failed")
			return
		}
		status, err := s.browser.Status(item.AccountID)
		if err != nil || status == nil || !status.Running {
			_ = s.db.UpdateBatchItemStatus(item.ID, "failed", "start", "BROWSER_NOT_RUNNING", "browser did not report ready after start")
			return
		}
		_ = s.db.UpdateBatchItemStatus(item.ID, "success", "ready", "", "")
	case OperationStop:
		if err := s.browser.StopAccount(item.AccountID); err != nil {
			_ = s.db.UpdateBatchItemStatus(item.ID, "failed", "stop", "BROWSER_STOP_FAILED", "browser stop failed")
			return
		}
		_ = s.db.UpdateBatchItemStatus(item.ID, "success", "stopped", "", "")
	case OperationObserve:
		status, err := s.browser.Status(item.AccountID)
		if err != nil {
			_ = s.db.UpdateBatchItemStatus(item.ID, "failed", "observe", "BROWSER_STATUS_FAILED", "browser status unavailable")
			return
		}
		if status == nil || !status.Running {
			_ = s.db.UpdateBatchItemStatus(item.ID, "waiting_human", "manual_takeover", "ACCOUNT_BROWSER_NOT_RUNNING", "start the local account browser to continue")
			return
		}
		_ = s.db.UpdateBatchItemStatus(item.ID, "success", "observed", "", "")
	}
}

func (s *Service) updateRunState(runID int64) error {
	items, err := s.db.ListBatchItems(runID)
	if err != nil {
		return err
	}
	pending, running, success, failed, skipped := 0, 0, 0, 0, 0
	waitingHuman := false
	for _, item := range items {
		switch item.Status {
		case "success":
			success++
		case "failed":
			failed++
		case "cancelled", "skipped":
			skipped++
		case "running":
			running++
		case "waiting_human":
			pending++
			waitingHuman = true
		default:
			pending++
		}
	}
	if waitingHuman {
		return s.db.UpdateBatchRunWaitingHuman(runID, pending, running, success, failed, skipped)
	}
	return s.db.UpdateBatchRunCounters(runID, pending, running, success, failed, skipped)
}
