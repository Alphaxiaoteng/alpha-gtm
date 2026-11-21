package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/radar"
)

// TestCreatorArchiveJobSnapshotIsolatesInternalState makes sure the polling
// handler never receives the job's own result map: a caller mutating the
// snapshot must not touch the live job.
func TestCreatorArchiveJobSnapshotIsolatesInternalState(t *testing.T) {
	job := newCreatorArchiveJob()
	job.setDone(map[string]any{"ok": true, "folder": "/tmp/creator"})

	snapshot := job.snapshot()
	if snapshot.status != "done" || snapshot.result["folder"] != "/tmp/creator" {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	snapshot.result["folder"] = "mutated"

	if again := job.snapshot(); again.result["folder"] != "/tmp/creator" {
		t.Fatalf("snapshot aliased the internal result map: %#v", again.result)
	}
}

// TestPruneFinishedCreatorArchiveJobsDropsAbandonedJobs covers the leak: a job
// whose client never polled must be swept when the next archive starts, while a
// running job and a freshly finished job stay readable.
func TestPruneFinishedCreatorArchiveJobsDropsAbandonedJobs(t *testing.T) {
	staleKey, staleJob := "douyin-stale-job", &creatorArchiveJob{status: "done", doneAt: time.Now().Add(-time.Minute)}
	freshKey, freshJob := "douyin-fresh-job", &creatorArchiveJob{status: "error", doneAt: time.Now()}
	runningKey, runningJob := "douyin-running-job", newCreatorArchiveJob()
	for key, job := range map[string]*creatorArchiveJob{
		staleKey: staleJob, freshKey: freshJob, runningKey: runningJob,
	} {
		creatorArchiveJobs.Store(key, job)
		t.Cleanup(func() { creatorArchiveJobs.CompareAndDelete(key, job) })
	}

	pruneFinishedCreatorArchiveJobs(time.Now())

	if _, ok := creatorArchiveJobs.Load(staleKey); ok {
		t.Fatal("abandoned finished job was not pruned")
	}
	if _, ok := creatorArchiveJobs.Load(freshKey); !ok {
		t.Fatal("freshly finished job was pruned before its polling window")
	}
	if _, ok := creatorArchiveJobs.Load(runningKey); !ok {
		t.Fatal("running job was pruned")
	}
}

// TestRegisterCreatorArchiveJobClaimIsAtomic proves only one concurrent POST can
// own a creator's archive slot. The pre-fix LoadOrStore-then-Store sequence let two
// callers both observe a finished job and start two archives for one creator.
func TestRegisterCreatorArchiveJobClaimIsAtomic(t *testing.T) {
	const key = "douyin-claim-race"
	t.Cleanup(func() { creatorArchiveJobs.Delete(key) })

	const callers = 8
	start := make(chan struct{})
	claims := make(chan *creatorArchiveJob, callers)
	var wg sync.WaitGroup
	for index := 0; index < callers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if job, ok := registerCreatorArchiveJob(key); ok {
				claims <- job
			}
		}()
	}
	close(start)
	wg.Wait()
	close(claims)

	var owned []*creatorArchiveJob
	for job := range claims {
		owned = append(owned, job)
	}
	if len(owned) != 1 {
		t.Fatalf("concurrent claims=%d want exactly 1", len(owned))
	}

	// 还在跑：第二个请求必须被拒，否则会双倍消耗 TikHub 额度。
	if _, ok := registerCreatorArchiveJob(key); ok {
		t.Fatal("a running job must reject a second claim")
	}

	// 跑完了：必须能重新接手（「更新」按钮要可反复点）。
	owned[0].setDone(map[string]any{"ok": true})
	second, ok := registerCreatorArchiveJob(key)
	if !ok {
		t.Fatal("a finished job must not block the next archive")
	}
	if second == owned[0] {
		t.Fatal("the next archive reused the finished job object")
	}
	if snapshot := second.snapshot(); snapshot.status != "running" {
		t.Fatalf("newly claimed job status=%q want running", snapshot.status)
	}
}

// TestCreatorArchiveJobStatusReadsRaceWithJobWrites drives the real status
// handler against a job that keeps mutating status/progress/result from another
// goroutine. Run with -race to prove the handler no longer touches unguarded
// fields.
func TestCreatorArchiveJobStatusReadsRaceWithJobWrites(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	const (
		platform = "douyin"
		userID   = "race-writer"
		writes   = 200
		readers  = 4
	)
	key := platform + "-" + userID
	job := newCreatorArchiveJob()
	creatorArchiveJobs.Store(key, job)
	t.Cleanup(func() { creatorArchiveJobs.CompareAndDelete(key, job) })

	start := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for index := 0; index < writes; index++ {
			job.updateProgress(func(progress *radar.CreatorArchiveProgress) {
				progress.Phase = "media"
				progress.CoversDone = index
				progress.CoversTotal = writes
				progress.AudioDone = index
				progress.AudioTotal = writes
			})
			switch index % 4 {
			case 1:
				job.setDone(map[string]any{"ok": true, "pages": index})
			case 3:
				job.setError("归档中断")
			}
			// 模拟下一次归档启动时的清扫，与轮询并发执行。
			if index%50 == 49 {
				pruneFinishedCreatorArchiveJobs(time.Now())
			}
		}
	}()

	for reader := 0; reader < readers; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for index := 0; index < writes; index++ {
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodGet,
					"/api/trend-radar/creators/"+platform+"/"+userID+"/archive-status", nil)
				server.ServeHTTP(recorder, request)
				if recorder.Code != http.StatusOK {
					t.Errorf("status=%d body=%s", recorder.Code, recorder.Body.String())
					return
				}
				var payload struct {
					Status   string                       `json:"status"`
					Progress radar.CreatorArchiveProgress `json:"progress"`
					Result   map[string]any               `json:"result"`
					Error    string                       `json:"error"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
					t.Errorf("decode status payload: %v body=%s", err, recorder.Body.String())
					return
				}
				if payload.Status != "running" && payload.Status != "done" && payload.Status != "error" {
					t.Errorf("unexpected status %q", payload.Status)
					return
				}
			}
		}()
	}

	close(start)
	wg.Wait()
}
