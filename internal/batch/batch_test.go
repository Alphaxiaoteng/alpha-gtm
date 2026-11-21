package batch

import (
	"context"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

func TestRunnerTracksActiveItemAndLifecycleTimes(t *testing.T) {
	store, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateBatchRunWithItems("test", "xhs", "running", 1, `{}`, []db.BatchItemInput{
		{ItemType: "test"},
		{ItemType: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	runner := NewRunner(store, run)
	runner.OnItem = func(ctx context.Context, _ *db.BatchItem) error {
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan error, 1)
	go func() { done <- runner.Run(context.Background()) }()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first item did not start")
	}
	active, err := store.GetBatchRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active.PendingCount != 1 || active.RunningCount != 1 {
		t.Fatalf("active counters pending=%d running=%d; want 1/1", active.PendingCount, active.RunningCount)
	}
	items, err := store.ListBatchItems(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].StartedAt == "" || items[0].FinishedAt != "" || items[1].StartedAt != "" {
		t.Fatalf("unexpected active item timestamps: %+v", items)
	}

	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("batch runner did not finish")
	}
	completed, err := store.GetBatchRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || completed.PendingCount != 0 || completed.RunningCount != 0 || completed.SuccessCount != 2 {
		t.Fatalf("unexpected completed counters: %+v", completed)
	}
	items, err = store.ListBatchItems(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.StartedAt == "" || item.FinishedAt == "" {
			t.Fatalf("completed item missing lifecycle timestamp: %+v", item)
		}
	}
}
