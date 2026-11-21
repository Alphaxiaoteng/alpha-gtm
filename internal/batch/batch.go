package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"alpha-nexus-egolite/internal/db"
)

type Runner struct {
	db     *db.DB
	run    *db.BatchRun
	mu     sync.Mutex
	stopCh chan struct{}

	Concurrency int
	OnItem      func(ctx context.Context, item *db.BatchItem) error
}

func NewRunner(d *db.DB, run *db.BatchRun) *Runner {
	return &Runner{
		db:          d,
		run:         run,
		stopCh:      make(chan struct{}),
		Concurrency: 1,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	items, err := r.db.ListBatchItems(r.run.ID)
	if err != nil {
		return fmt.Errorf("list batch items: %w", err)
	}

	jobCh := make(chan *db.BatchItem, len(items))
	var wg sync.WaitGroup

	concurrency := r.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go r.worker(ctx, &wg, jobCh)
	}

	for i := range items {
		if items[i].Status == "success" || items[i].Status == "skipped" {
			continue
		}
		jobCh <- &items[i]
	}
	close(jobCh)

	wg.Wait()
	return nil
}

func (r *Runner) Stop() {
	close(r.stopCh)
}

func (r *Runner) worker(ctx context.Context, wg *sync.WaitGroup, jobCh <-chan *db.BatchItem) {
	defer wg.Done()
	for item := range jobCh {
		select {
		case <-r.stopCh:
			r.db.UpdateBatchItemStatus(item.ID, "cancelled", "", "CANCELLED", "batch cancelled")
			r.updateCountersAfter("cancelled", false)
			continue
		case <-ctx.Done():
			r.db.UpdateBatchItemStatus(item.ID, "cancelled", "", "CANCELLED", ctx.Err().Error())
			r.updateCountersAfter("cancelled", false)
			continue
		default:
		}

		r.db.UpdateBatchItemStatus(item.ID, "running", "", "", "")
		r.updateCountersStarted()
		err := r.OnItem(ctx, item)
		if err != nil {
			r.db.UpdateBatchItemStatus(item.ID, "failed", item.CurrentStep, "ERROR", err.Error())
			r.updateCountersAfter("failed", true)
		} else {
			r.db.UpdateBatchItemStatus(item.ID, "success", item.CurrentStep, "", "")
			r.updateCountersAfter("success", true)
		}
	}
}

func (r *Runner) updateCountersStarted() {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.db.GetBatchRun(r.run.ID)
	if err != nil {
		return
	}
	pending := run.PendingCount
	if pending > 0 {
		pending--
	}
	_ = r.db.UpdateBatchRunCounters(run.ID, pending, run.RunningCount+1, run.SuccessCount, run.FailedCount, run.SkippedCount)
}

func (r *Runner) updateCountersAfter(status string, wasRunning bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, err := r.db.GetBatchRun(r.run.ID)
	if err != nil {
		return
	}
	pending := run.PendingCount
	running := run.RunningCount
	success := run.SuccessCount
	failed := run.FailedCount
	skipped := run.SkippedCount

	consumeActive := func() {
		if wasRunning && running > 0 {
			running--
		} else if pending > 0 {
			pending--
		}
	}
	switch status {
	case "success":
		consumeActive()
		success++
	case "failed":
		consumeActive()
		failed++
	case "cancelled":
		consumeActive()
		skipped++
	}
	r.db.UpdateBatchRunCounters(r.run.ID, pending, running, success, failed, skipped)
}

func EmitNDJSON(event map[string]interface{}) {
	data, _ := json.Marshal(event)
	fmt.Println(string(data))
}
