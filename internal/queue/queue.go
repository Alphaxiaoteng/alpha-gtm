package queue

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Item is a SQLite-backed logical queue item (persisted via Store).
type Item struct {
	ID          int64
	Kind        string // publish | agent | metrics
	PayloadJSON string
	Status      string // pending|running|done|failed|cancelled
	Priority    int
	RunAfter    string // RFC3339 or empty
	Attempts    int
	LastError   string
	TraceID     string
	CreatedAt   string
	UpdatedAt   string
}

type Store interface {
	Enqueue(kind, payload, runAfter, traceID string, priority int) (*Item, error)
	ClaimNext(now string) (*Item, error)
	UpdateItem(id int64, status, lastError string, attempts int) error
	ListItems(limit int) ([]Item, error)
}

// Worker processes claimed items.
type Handler func(ctx context.Context, item *Item) error

// Runner polls store and runs handler with concurrency 1 by default.
type Runner struct {
	Store       Store
	Handler     Handler
	Concurrency int
	PollEvery   time.Duration
	OnError     func(error)
}

func (r *Runner) RunOnce(ctx context.Context) error {
	if r.Handler == nil {
		return fmt.Errorf("nil handler")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	item, err := r.Store.ClaimNext(now)
	if err != nil || item == nil {
		return err
	}
	runErr := r.Handler(ctx, item)
	if runErr != nil {
		if persistErr := r.Store.UpdateItem(item.ID, "failed", runErr.Error(), item.Attempts); persistErr != nil {
			return fmt.Errorf("%v; persist failed status: %w", runErr, persistErr)
		}
		return runErr
	}
	return r.Store.UpdateItem(item.ID, "done", "", item.Attempts)
}

func (r *Runner) Loop(ctx context.Context) error {
	every := r.PollEvery
	if every <= 0 {
		every = 2 * time.Second
	}
	n := r.Concurrency
	if n <= 0 {
		n = 1
	}
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var lastErr error
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if err := r.RunOnce(ctx); err != nil {
						if r.OnError != nil {
							r.OnError(err)
						}
						errMu.Lock()
						lastErr = err
						errMu.Unlock()
					}
				}
			}
		}()
	}
	<-ctx.Done()
	wg.Wait()
	errMu.Lock()
	defer errMu.Unlock()
	if lastErr != nil {
		return fmt.Errorf("%w; last queue error: %v", ctx.Err(), lastErr)
	}
	return ctx.Err()
}

// Backoff returns exponential delay seconds capped.
func Backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	sec := 1 << (attempts - 1)
	if sec > 300 {
		sec = 300
	}
	return time.Duration(sec) * time.Second
}
