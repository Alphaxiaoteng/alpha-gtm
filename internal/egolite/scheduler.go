package egolite

import (
	"context"
	"fmt"
	"sync"
)

type SpaceJob struct {
	ID      int64
	SpaceID int64
	Run     func(context.Context) error
}

type SpaceJobResult struct {
	ID      int64
	SpaceID int64
	Err     error
}

type SpaceScheduler struct {
	concurrency int
}

func NewSpaceScheduler(concurrency int) *SpaceScheduler {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 3 {
		concurrency = 3
	}
	return &SpaceScheduler{concurrency: concurrency}
}

func (scheduler *SpaceScheduler) Run(ctx context.Context, jobs []SpaceJob) []SpaceJobResult {
	results := make([]SpaceJobResult, len(jobs))
	type indexedJob struct {
		index int
		job   SpaceJob
	}
	groups := map[int64][]indexedJob{}
	for index, job := range jobs {
		results[index] = SpaceJobResult{ID: job.ID, SpaceID: job.SpaceID}
		if job.ID <= 0 || job.SpaceID < 0 || job.Run == nil {
			results[index].Err = fmt.Errorf("EGO_SPACE_JOB_INVALID")
			continue
		}
		groups[job.SpaceID] = append(groups[job.SpaceID], indexedJob{index: index, job: job})
	}

	limit := make(chan struct{}, scheduler.concurrency)
	var wait sync.WaitGroup
	for _, group := range groups {
		group := group
		wait.Add(1)
		go func() {
			defer wait.Done()
			for _, item := range group {
				select {
				case limit <- struct{}{}:
				case <-ctx.Done():
					results[item.index].Err = ctx.Err()
					continue
				}
				results[item.index].Err = item.job.Run(ctx)
				<-limit
			}
		}()
	}
	wait.Wait()
	return results
}
