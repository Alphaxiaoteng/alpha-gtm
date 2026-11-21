package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"

	"alpha-nexus-egolite/internal/batch"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/publish"
	"alpha-nexus-egolite/internal/publishguard"
)

type batchPublishRequest struct {
	ContentID   int64   `json:"contentId"`
	AssetIDs    []int64 `json:"assetIds"`
	Concurrency int     `json:"concurrency"`
	DryRun      *bool   `json:"dryRun"`
}

// batchRunResponse is the single HTTP contract used by both batch creation
// and batch listing. Database structs intentionally stay transport-agnostic.
type batchRunResponse struct {
	ID            int64  `json:"id"`
	OperationType string `json:"operationType"`
	PlatformCode  string `json:"platformCode"`
	Status        string `json:"status"`
	TotalItems    int    `json:"totalItems"`
	PendingCount  int    `json:"pendingCount"`
	RunningCount  int    `json:"runningCount"`
	SuccessCount  int    `json:"successCount"`
	FailedCount   int    `json:"failedCount"`
	SkippedCount  int    `json:"skippedCount"`
	Concurrency   int    `json:"concurrency"`
	DryRun        bool   `json:"dryRun"`
}

type batchItemResponse struct {
	ID           int64  `json:"id"`
	BatchID      int64  `json:"batchId"`
	AccountID    int64  `json:"accountId"`
	ContentID    int64  `json:"contentId"`
	Status       string `json:"status"`
	ErrorMessage string `json:"errorMessage"`
	StartedAt    string `json:"startedAt"`
	FinishedAt   string `json:"finishedAt"`
}

func newBatchRunResponse(run *db.BatchRun) batchRunResponse {
	dryRun := false
	if run.SettingsJSON != "" {
		var settings struct {
			DryRun bool `json:"dry_run"`
		}
		if json.Unmarshal([]byte(run.SettingsJSON), &settings) == nil {
			dryRun = settings.DryRun
		}
	}
	return batchRunResponse{
		ID:            run.ID,
		OperationType: run.OperationType,
		PlatformCode:  run.PlatformCode,
		Status:        run.Status,
		TotalItems:    run.TotalItems,
		PendingCount:  run.PendingCount,
		RunningCount:  run.RunningCount,
		SuccessCount:  run.SuccessCount,
		FailedCount:   run.FailedCount,
		SkippedCount:  run.SkippedCount,
		Concurrency:   run.Concurrency,
		DryRun:        dryRun,
	}
}

func newBatchItemResponse(item *db.BatchItem) batchItemResponse {
	return batchItemResponse{
		ID:           item.ID,
		BatchID:      item.BatchID,
		AccountID:    item.AccountID,
		ContentID:    item.ContentID,
		Status:       item.Status,
		ErrorMessage: item.ErrorMessage,
		StartedAt:    item.StartedAt,
		FinishedAt:   item.FinishedAt,
	}
}

// handleAPIBatchPublish creates a real, durable batch but deliberately only
// accepts dry-run work. Live multi-account clicks require a separate explicit
// approval flow; they are never hidden behind a historical dashboard button.
func (s *Server) handleAPIBatchPublish(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	defer r.Body.Close()
	var req batchPublishRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid batch publish body", http.StatusBadRequest)
		return
	}
	if req.DryRun != nil && !*req.DryRun {
		http.Error(w, "live batch publish is not available from the desktop UI", http.StatusBadRequest)
		return
	}
	if req.ContentID <= 0 || len(req.AssetIDs) == 0 || len(req.AssetIDs) > 100 {
		http.Error(w, "contentId and 1-100 assetIds are required", http.StatusBadRequest)
		return
	}
	content, err := s.db.GetContent(req.ContentID)
	if err != nil {
		http.Error(w, "content not found", http.StatusNotFound)
		return
	}
	assetIDs := append([]int64(nil), req.AssetIDs...)
	sort.Slice(assetIDs, func(i, j int) bool { return assetIDs[i] < assetIDs[j] })
	assets := make([]*db.AccountAsset, 0, len(assetIDs))
	platform := ""
	for index, assetID := range assetIDs {
		if assetID <= 0 || (index > 0 && assetID == assetIDs[index-1]) {
			http.Error(w, "assetIds must be unique positive ids", http.StatusBadRequest)
			return
		}
		asset, err := s.db.GetAccountAsset(assetID)
		if err != nil || !asset.Enabled {
			http.Error(w, "enabled account asset not found", http.StatusBadRequest)
			return
		}
		if targetErr := publishguard.ValidateTargetOwnership(s.db, asset, asset.PlatformCode); targetErr != nil {
			http.Error(w, targetErr.Error(), http.StatusBadRequest)
			return
		}
		if platform == "" {
			platform = asset.PlatformCode
		}
		if asset.PlatformCode != platform {
			http.Error(w, "all batch assets must use the same platform", http.StatusBadRequest)
			return
		}
		assets = append(assets, asset)
	}
	concurrency := req.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 10 {
		concurrency = 10
	}
	settings, _ := json.Marshal(map[string]any{"dry_run": true, "source": "ui"})
	items := make([]db.BatchItemInput, 0, len(assets))
	for _, asset := range assets {
		items = append(items, db.BatchItemInput{ItemType: "publish", AccountID: asset.AccountID, AssetID: asset.ID, ContentID: content.ID})
	}
	run, err := s.db.CreateBatchRunWithItems("publish", platform, "running", concurrency, string(settings), items)
	if err != nil {
		http.Error(w, "create batch run failed", http.StatusInternalServerError)
		return
	}
	s.startBatchPublish(run, content)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, newBatchRunResponse(run))
}

func (s *Server) startBatchPublish(run *db.BatchRun, content *db.Content) {
	if s.batchCtx == nil {
		return
	}
	s.batchWG.Add(1)
	go func() {
		defer s.batchWG.Done()
		runner := batch.NewRunner(s.db, run)
		runner.Concurrency = run.Concurrency
		runner.OnItem = func(ctx context.Context, item *db.BatchItem) error {
			asset, err := s.db.GetAccountAsset(item.AssetID)
			if err != nil {
				return fmt.Errorf("batch asset unavailable")
			}
			_ = s.db.UpdateBatchItemStatus(item.ID, "running", "preflight", "", "")
			key := fmt.Sprintf("batch:%d:item:%d", run.ID, item.ID)
			settingsJSON, err := publish.DefaultSettingsSnapshot(asset.PlatformCode)
			if err != nil {
				return fmt.Errorf("build batch publish settings")
			}
			job, err := s.db.CreatePublishJobWithIntent(
				run.ID, content.ID, asset.AccountID, asset.ID, asset.PlatformCode, content.Type, key,
				publish.ModeDryRun, settingsJSON,
			)
			if err != nil {
				return fmt.Errorf("create batch publish job")
			}
			if err := s.db.UpdateBatchItemJob(item.ID, job.ID); err != nil {
				return fmt.Errorf("link batch publish job")
			}
			result, err := (&publish.Runner{Store: s.db}).Run(ctx, publish.RunInput{
				Job: job, Content: content, Asset: asset, DryRun: true,
				TraceID: fmt.Sprintf("batch-%d-item-%d", run.ID, item.ID),
			})
			if result != nil {
				_ = s.db.UpdateBatchItemStatus(item.ID, "running", result.Step, "", "")
			}
			return err
		}
		_ = runner.Run(s.batchCtx)
	}()
}

func (s *Server) stopBatchWork() {
	if s.batchCancel == nil {
		return
	}
	s.batchCancel()
	done := make(chan struct{})
	go func() {
		s.batchWG.Wait()
		close(done)
	}()
	<-done
}
