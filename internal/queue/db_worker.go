package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/observability"
)

// DBHandler processes one durable SQLite task after a short lease transaction
// has committed. Browser and network work must stay outside that transaction.
type DBHandler func(context.Context, *db.TaskQueueItem) error

// DBWorker is the production queue loop for Alpha-Nexus' SQLite task_queue.
// It uses owner-bound leases so a crashed worker's task becomes reclaimable.
type DBWorker struct {
	Store     *db.DB
	Owner     string
	LeaseTTL  time.Duration
	PollEvery time.Duration
	Handler   DBHandler
	OnError   func(error)
}

func (w *DBWorker) RunOnce(ctx context.Context) error {
	if w.Store == nil || w.Handler == nil {
		return fmt.Errorf("queue database and handler are required")
	}
	ttl := w.LeaseTTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	item, err := w.Store.ClaimNextTaskWithLease(time.Now(), w.Owner, ttl)
	if err != nil || item == nil {
		return err
	}
	return w.runClaimed(ctx, item, ttl)
}

// RunTask leases and executes exactly one durable task selected by id.
func (w *DBWorker) RunTask(ctx context.Context, id int64) error {
	if w.Store == nil || w.Handler == nil {
		return fmt.Errorf("queue database and handler are required")
	}
	ttl := w.LeaseTTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	item, err := w.Store.ClaimTaskWithLease(id, time.Now(), w.Owner, ttl)
	if err != nil {
		return err
	}
	if item == nil {
		return fmt.Errorf("task %d is not runnable", id)
	}
	return w.runClaimed(ctx, item, ttl)
}

func (w *DBWorker) runClaimed(ctx context.Context, item *db.TaskQueueItem, ttl time.Duration) error {
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopHeartbeat := make(chan struct{})
	var heartbeatWG sync.WaitGroup
	var leaseErr error
	var leaseMu sync.Mutex
	heartbeatWG.Add(1)
	go func() {
		defer heartbeatWG.Done()
		interval := ttl / 2
		if interval < 10*time.Millisecond {
			interval = 10 * time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-workCtx.Done():
				return
			case <-ticker.C:
				renewed, renewErr := w.Store.RenewTaskLease(item.ID, w.Owner, time.Now(), ttl)
				if renewErr != nil {
					leaseMu.Lock()
					leaseErr = fmt.Errorf("renew task lease: %w", renewErr)
					leaseMu.Unlock()
					cancel()
					return
				}
				if !renewed {
					leaseMu.Lock()
					leaseErr = fmt.Errorf("task %d lease lost", item.ID)
					leaseMu.Unlock()
					cancel()
					return
				}
			}
		}
	}()

	handlerErr := w.Handler(workCtx, item)
	close(stopHeartbeat)
	heartbeatWG.Wait()
	leaseMu.Lock()
	finalLeaseErr := leaseErr
	leaseMu.Unlock()
	if finalLeaseErr != nil {
		return finalLeaseErr
	}
	status := "done"
	lastError := ""
	if handlerErr != nil {
		status = "failed"
		// Queue rows outlive the process and may be rendered in the UI. Persist
		// only the stable error code; transport/browser errors can contain tokens.
		lastError = "task failed (" + observability.StableErrorCode(handlerErr.Error()) + ")"
	}
	completed, completeErr := w.Store.CompleteLeasedTask(item.ID, w.Owner, status, lastError, item.Attempts)
	if completeErr != nil {
		return fmt.Errorf("complete task lease: %w", completeErr)
	}
	if !completed {
		return fmt.Errorf("task %d completion lost its lease", item.ID)
	}
	return handlerErr
}

func (w *DBWorker) Loop(ctx context.Context) error {
	every := w.PollEvery
	if every <= 0 {
		every = 2 * time.Second
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil && ctx.Err() == nil && w.OnError != nil {
			w.OnError(err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
