package egolite_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/egolite"
)

func TestSpaceSchedulerSerializesSameSpaceAndRunsDifferentSpacesConcurrently(t *testing.T) {
	scheduler := egolite.NewSpaceScheduler(3)
	var activeBySpace sync.Map
	var parallel atomic.Int32
	var peak atomic.Int32
	run := func(spaceID int64) func(context.Context) error {
		return func(context.Context) error {
			value, _ := activeBySpace.LoadOrStore(spaceID, new(atomic.Int32))
			active := value.(*atomic.Int32)
			if active.Add(1) != 1 {
				t.Errorf("space %d executed concurrently", spaceID)
			}
			current := parallel.Add(1)
			for current > peak.Load() && !peak.CompareAndSwap(peak.Load(), current) {
			}
			time.Sleep(25 * time.Millisecond)
			parallel.Add(-1)
			active.Add(-1)
			return nil
		}
	}

	results := scheduler.Run(context.Background(), []egolite.SpaceJob{
		{ID: 1, SpaceID: 10, Run: run(10)},
		{ID: 2, SpaceID: 10, Run: run(10)},
		{ID: 3, SpaceID: 20, Run: run(20)},
		{ID: 4, SpaceID: 30, Run: run(30)},
	})
	for _, result := range results {
		if result.Err != nil {
			t.Fatalf("result=%+v", result)
		}
	}
	if peak.Load() < 2 || peak.Load() > 3 {
		t.Fatalf("parallel peak=%d, want 2..3", peak.Load())
	}
}

func TestSpaceSchedulerAcceptsZeroBasedSpaceID(t *testing.T) {
	var called atomic.Bool
	results := egolite.NewSpaceScheduler(1).Run(context.Background(), []egolite.SpaceJob{{
		ID: 1, SpaceID: 0, Run: func(context.Context) error {
			called.Store(true)
			return nil
		},
	}})
	if len(results) != 1 || results[0].Err != nil || !called.Load() {
		t.Fatalf("results=%+v called=%v", results, called.Load())
	}
}

func TestSpaceSchedulerNeverExceedsThreeActiveSpaces(t *testing.T) {
	var active atomic.Int32
	var peak atomic.Int32
	jobs := make([]egolite.SpaceJob, 0, 8)
	for index := 0; index < 8; index++ {
		jobs = append(jobs, egolite.SpaceJob{
			ID: int64(index + 1), SpaceID: int64(index + 1),
			Run: func(context.Context) error {
				current := active.Add(1)
				for current > peak.Load() && !peak.CompareAndSwap(peak.Load(), current) {
				}
				time.Sleep(20 * time.Millisecond)
				active.Add(-1)
				return nil
			},
		})
	}
	egolite.NewSpaceScheduler(12).Run(context.Background(), jobs)
	if peak.Load() > 3 {
		t.Fatalf("parallel peak=%d, want <=3", peak.Load())
	}
}
