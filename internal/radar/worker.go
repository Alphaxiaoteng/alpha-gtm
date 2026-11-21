package radar

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tikhub"
	"alpha-nexus-egolite/internal/trace"
)

type RunSummary struct {
	Due       int     `json:"due"`
	Succeeded int     `json:"succeeded"`
	Failed    int     `json:"failed"`
	CallIDs   []int64 `json:"call_ids"`

	// Creator incremental archive pass. Every creator here is opt-in via the
	// watchlist switch, so an all-default watchlist reports zeros and makes no
	// TikHub call.
	CreatorsDue       int      `json:"creators_due"`
	CreatorsSucceeded int      `json:"creators_succeeded"`
	CreatorsFailed    int      `json:"creators_failed"`
	CreatorsSkipped   int      `json:"creators_skipped"`
	CreatorCallIDs    []int64  `json:"creator_call_ids"`
	CreatorErrors     []string `json:"creator_errors,omitempty"`
}

func RunDue(ctx context.Context, dataDir string, now time.Time) (RunSummary, error) {
	lockDir := filepath.Join(dataDir, "trend-radar", "worker.lock")
	if err := os.MkdirAll(filepath.Dir(lockDir), 0o700); err != nil {
		return RunSummary{}, err
	}
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		if os.IsExist(err) {
			info, statErr := os.Stat(lockDir)
			// ponytail: one pass can legitimately include several 120s calls; use
			// a conservative stale window instead of adding a heartbeat process.
			if statErr != nil || time.Since(info.ModTime()) <= 30*time.Minute {
				return RunSummary{}, fmt.Errorf("radar worker already running")
			}
			if removeErr := os.Remove(lockDir); removeErr != nil {
				return RunSummary{}, fmt.Errorf("remove stale radar worker lock: %w", removeErr)
			}
			if retryErr := os.Mkdir(lockDir, 0o700); retryErr != nil {
				return RunSummary{}, retryErr
			}
		} else {
			return RunSummary{}, err
		}
	}
	defer os.Remove(lockDir)

	schedules, err := LoadSchedules(dataDir)
	if err != nil {
		return RunSummary{}, err
	}
	database, err := db.Open(dataDir)
	if err != nil {
		return RunSummary{}, err
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		return RunSummary{}, err
	}

	summary := RunSummary{CallIDs: []int64{}, CreatorCallIDs: []int64{}}
	completed := make([]Schedule, 0)
	for index := range schedules.Schedules {
		schedule := &schedules.Schedules[index]
		if !schedule.Enabled || !scheduleDue(*schedule, now) {
			continue
		}
		summary.Due++
		call, runErr := runTikHubSchedule(ctx, database, dataDir, *schedule)
		schedule.LastRunAt = now.UTC().Format(time.RFC3339Nano)
		schedule.NextRunAt = now.Add(time.Duration(schedule.IntervalMinutes) * time.Minute).UTC().Format(time.RFC3339Nano)
		schedule.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
		if runErr != nil {
			summary.Failed++
			schedule.LastStatus = "failed"
			schedule.LastError = "抓取失败；请到日志查看"
			completed = append(completed, *schedule)
			continue
		}
		summary.Succeeded++
		summary.CallIDs = append(summary.CallIDs, call.ID)
		schedule.LastStatus = "succeeded"
		schedule.LastCallID = call.ID
		schedule.LastError = ""
		completed = append(completed, *schedule)
	}
	if len(completed) > 0 {
		if err := MergeScheduleRuntime(dataDir, completed); err != nil {
			return summary, err
		}
	}
	// Keyword schedules keep their existing behaviour; the creator pass runs
	// afterwards so a slow archive cannot delay due keyword searches.
	if err := runDueCreatorAutoArchives(ctx, database, dataDir, now, &summary); err != nil {
		return summary, err
	}
	return summary, nil
}

func scheduleDue(schedule Schedule, now time.Time) bool {
	if schedule.NextRunAt == "" {
		return true
	}
	next, err := time.Parse(time.RFC3339Nano, schedule.NextRunAt)
	if err != nil {
		return true
	}
	return !next.After(now)
}

func runTikHubSchedule(ctx context.Context, database *db.DB, dataDir string, schedule Schedule) (*db.TikHubAPICall, error) {
	endpoint, ok := tikhub.FindEndpoint(schedule.EndpointID)
	if !ok {
		return nil, fmt.Errorf("endpoint unavailable")
	}
	provider, err := tikhub.LoadProviderConfig(dataDir)
	if err != nil || !provider.Active || provider.TestStatus != tikhub.TestStatusPassed {
		return nil, fmt.Errorf("provider inactive")
	}
	client, _, err := tikhub.NewActiveClient(dataDir)
	if err != nil {
		return nil, err
	}
	// 窗口内同端点同参数已经付过费，就直接回放那份快照。
	// 定时搜索每 6 小时一轮、失败还会重试，是重复付费最密集的一条链路。
	if entry, hit := tikhub.LookupReusableCall(dataDir, endpoint.ID, schedule.Params, endpoint.Capability, time.Now()); hit {
		if call, callErr := database.GetTikHubAPICall(entry.CallID); callErr == nil {
			return call, nil
		}
	}
	parametersJSON, err := json.Marshal(schedule.Params)
	if err != nil {
		return nil, err
	}
	traceID := string(trace.New())
	call, err := database.CreateTikHubAPICall(db.TikHubAPICallInput{
		TraceID: traceID, PlatformCode: endpoint.Platform, EndpointID: endpoint.ID,
		Capability: endpoint.Capability, Method: endpoint.Method, Path: endpoint.Path,
		ParametersJSON: string(parametersJSON),
	})
	if err != nil {
		return nil, err
	}
	started := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	response, callErr := client.CallPreparedEndpoint(callCtx, endpoint, schedule.Params)
	duration := time.Since(started).Milliseconds()
	if callErr != nil {
		providerCode, providerMessage := tikhub.SafeRequestFailure(callErr)
		_, _ = database.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
			Status: "failed", ErrorCode: "provider_request_failed", ProviderCode: providerCode,
			ProviderMessage: providerMessage, DurationMS: duration,
		})
		return nil, callErr
	}
	rawPath, responseBytes, saveErr := tikhub.SaveCallSnapshot(dataDir, tikhub.CallSnapshot{
		CallID: call.ID, TraceID: traceID, Platform: endpoint.Platform, EndpointID: endpoint.ID,
		Capability: endpoint.Capability, Method: endpoint.Method, Path: endpoint.Path,
		Parameters: schedule.Params, Response: response,
	})
	itemCount := tikhub.CountPrimaryItems(response)
	if saveErr != nil {
		_, _ = database.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
			Status: "storage_failed", ErrorCode: "snapshot_write_failed", ItemCount: itemCount, DurationMS: duration,
		})
		return nil, saveErr
	}
	coverCtx, cancelCovers := context.WithTimeout(ctx, 45*time.Second)
	_ = tikhub.CacheContentCovers(coverCtx, dataDir, response)
	cancelCovers()
	tikhub.RecordReusableCall(dataDir, endpoint.ID, schedule.Params,
		tikhub.ReuseEntry{CallID: call.ID, RawPath: rawPath}, time.Now())
	return database.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
		Status: "succeeded", ItemCount: itemCount, ResponseBytes: responseBytes,
		RawPath: rawPath, DurationMS: time.Since(started).Milliseconds(),
	})
}
