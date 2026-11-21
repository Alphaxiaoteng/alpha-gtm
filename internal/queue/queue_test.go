package queue

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

type fakeStore struct {
	item      *Item
	claimErr  error
	updateErr error
	updates   []Item
}

func (s *fakeStore) Enqueue(kind, payload, runAfter, traceID string, priority int) (*Item, error) {
	return nil, nil
}

func (s *fakeStore) ClaimNext(now string) (*Item, error) {
	return s.item, s.claimErr
}

func (s *fakeStore) UpdateItem(id int64, status, lastError string, attempts int) error {
	s.updates = append(s.updates, Item{ID: id, Status: status, LastError: lastError, Attempts: attempts})
	return s.updateErr
}

func (s *fakeStore) ListItems(limit int) ([]Item, error) { return nil, nil }

func TestRunOnceDoesNotDoubleCountClaimedAttempt(t *testing.T) {
	store := &fakeStore{item: &Item{ID: 7, Status: "running", Attempts: 1}}
	r := &Runner{Store: store, Handler: func(context.Context, *Item) error { return nil }}
	if err := r.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.updates) != 1 || store.updates[0].Status != "done" || store.updates[0].Attempts != 1 {
		t.Fatalf("updates=%+v, want one done attempt=1", store.updates)
	}
}

func TestRunOnceSurfacesFinalUpdateFailure(t *testing.T) {
	store := &fakeStore{item: &Item{ID: 8, Status: "running", Attempts: 1}, updateErr: errors.New("write failed")}
	r := &Runner{Store: store, Handler: func(context.Context, *Item) error { return nil }}
	if err := r.RunOnce(context.Background()); err == nil {
		t.Fatal("expected update error")
	}
}

func TestRunOncePreservesHandlerAndPersistenceErrors(t *testing.T) {
	store := &fakeStore{item: &Item{ID: 9, Status: "running", Attempts: 2}, updateErr: errors.New("write failed")}
	r := &Runner{Store: store, Handler: func(context.Context, *Item) error { return errors.New("handler failed") }}
	err := r.RunOnce(context.Background())
	if err == nil || err.Error() != "handler failed; persist failed status: write failed" {
		t.Fatalf("err=%v", err)
	}
	if len(store.updates) != 1 || store.updates[0].Attempts != 2 {
		t.Fatalf("updates=%+v", store.updates)
	}
}

func TestLoopReportsWorkerErrors(t *testing.T) {
	store := &fakeStore{item: &Item{ID: 10, Status: "running", Attempts: 1}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reported := make(chan error, 1)
	var once sync.Once
	r := &Runner{
		Store:     store,
		PollEvery: time.Millisecond,
		Handler: func(context.Context, *Item) error {
			return errors.New("worker failed")
		},
		OnError: func(err error) {
			once.Do(func() {
				reported <- err
				cancel()
			})
		},
	}
	err := r.Loop(ctx)
	if err == nil || !strings.Contains(err.Error(), "worker failed") {
		t.Fatalf("loop error=%v", err)
	}
	select {
	case got := <-reported:
		if got == nil || got.Error() != "worker failed" {
			t.Fatalf("reported=%v", got)
		}
	default:
		t.Fatal("worker error was not reported")
	}
}

func TestDBWorkerCompletesLeaseAndHonorsCancellation(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	first, err := database.EnqueueTask("test", `{}`, 0, "", "trace-worker")
	if err != nil {
		t.Fatal(err)
	}
	worker := &DBWorker{
		Store: database, Owner: "worker-test", LeaseTTL: 30 * time.Millisecond,
		Handler: func(context.Context, *db.TaskQueueItem) error { return nil },
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, err := database.ListTaskQueue(10)
	if err != nil || len(items) != 1 || items[0].ID != first.ID || items[0].Status != "done" || items[0].LeaseOwner != "" {
		t.Fatalf("items=%+v err=%v", items, err)
	}

	second, err := database.EnqueueTask("test", `{}`, 0, "", "trace-cancel")
	if err != nil {
		t.Fatal(err)
	}
	worker.Handler = func(_ context.Context, item *db.TaskQueueItem) error {
		_, err := database.RequestTaskCancel(item.ID)
		return err
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, err = database.ListTaskQueue(10)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].ID != second.ID || items[0].Status != "cancelled" {
		t.Fatalf("cancelled task=%+v", items[0])
	}
}

func TestDBWorkerPersistsOnlyStableHandlerError(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueTask("test", `{}`, 0, "", "trace-error"); err != nil {
		t.Fatal(err)
	}
	worker := &DBWorker{
		Store:    database,
		Owner:    "worker-test",
		LeaseTTL: time.Second,
		Handler: func(context.Context, *db.TaskQueueItem) error {
			return errors.New("RETRYABLE_FAILED: Bearer secret-value")
		},
	}
	if err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("expected handler error")
	}
	items, err := database.ListTaskQueue(10)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if got := items[0].LastError; got != "task failed (RETRYABLE_FAILED)" {
		t.Fatalf("last_error=%q", got)
	}
}

func TestDBWorkerRunTaskExecutesOnlyRequestedDurableTask(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	first, err := database.EnqueueTask("publish", `{"job":1}`, 100, "", "trace-first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.EnqueueTask("publish", `{"job":2}`, 1, "", "trace-second")
	if err != nil {
		t.Fatal(err)
	}
	var handledID int64
	worker := &DBWorker{
		Store: database, Owner: "worker-specific", LeaseTTL: time.Second,
		Handler: func(_ context.Context, item *db.TaskQueueItem) error {
			handledID = item.ID
			return nil
		},
	}
	if err := worker.RunTask(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
	if handledID != second.ID {
		t.Fatalf("handled=%d; want %d", handledID, second.ID)
	}
	items, err := database.ListTaskQueue(10)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	statuses := map[int64]string{items[0].ID: items[0].Status, items[1].ID: items[1].Status}
	if statuses[first.ID] != "pending" || statuses[second.ID] != "done" {
		t.Fatalf("statuses=%+v", statuses)
	}
}
