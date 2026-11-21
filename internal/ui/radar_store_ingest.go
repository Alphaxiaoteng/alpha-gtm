package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/radar"
	"alpha-nexus-egolite/internal/tikhub"
)

// The Trend Radar store is written from the same raw provider snapshots the
// summary reads, so there is one source of truth and no second collection path.
// Ingestion is idempotent: a work is keyed by platform + provider work id, and
// an observation by work + call + time.

type radarIngestResult struct {
	Snapshots  int `json:"snapshots"`
	Captures   int `json:"captures"`
	Inserted   int `json:"inserted"`
	Duplicates int `json:"duplicates"`
	Skipped    int `json:"skipped"`
	// 平台自己标出来的卡（广告卡/引导卡）与读不到条目，都不进 radar_works。
	AdCards    int              `json:"ad_cards"`
	GuideCards int              `json:"guide_cards"`
	Unreadable int              `json:"unreadable"`
	Runs       int              `json:"runs"`
	Stats      map[string]int64 `json:"stats"`
}

// syncRadarPoolVocabulary snapshots the configured pools and their terms.
func syncRadarPoolVocabulary(database *db.DB, dataDir string) error {
	config, err := tikhub.LoadRadarConfig(dataDir)
	if err != nil {
		return err
	}
	terms := make([]db.RadarPoolTerm, 0, len(config.Niches)*8)
	for _, niche := range config.Niches {
		for _, term := range niche.SearchTerms {
			terms = append(terms, db.RadarPoolTerm{PoolID: niche.ID, PoolName: niche.Name, Term: term})
		}
	}
	return database.SyncRadarPoolTerms(terms)
}

// ingestRadarStore writes snapshots (all, or only the given call ids) plus the
// run records into the database.
func ingestRadarStore(database *db.DB, dataDir string, onlyCallIDs map[int64]bool) (radarIngestResult, error) {
	result := radarIngestResult{}
	if database == nil {
		return result, fmt.Errorf("database unavailable")
	}
	if err := syncRadarPoolVocabulary(database, dataDir); err != nil {
		return result, err
	}
	snapshots, _, err := loadRadarCallSnapshots(filepath.Join(dataDir, "tikhub", "calls"), 0)
	if err != nil {
		return result, err
	}
	sort.SliceStable(snapshots, func(i, j int) bool { return snapshots[i].FetchedAt < snapshots[j].FetchedAt })
	for _, snapshot := range snapshots {
		if snapshot.Capability != "keyword_search" {
			continue
		}
		if len(onlyCallIDs) > 0 && !onlyCallIDs[snapshot.CallID] {
			continue
		}
		keyword := strings.TrimSpace(fmt.Sprint(snapshot.Parameters["keyword"]))
		poolID, poolName, found, poolErr := database.RadarPoolForTerm(keyword)
		if poolErr != nil {
			return result, poolErr
		}
		if !found {
			poolID, poolName = "", "历史记录"
		}
		result.Snapshots++
		niche := trendRadarNiche{ID: poolID, Name: poolName}
		if poolID == "" {
			niche.ID = archivedRadarNicheID
		}
		// 卡计数按整份响应算，与 summary 同一口径；卡不进雷达库。
		if tally := radarClassifyResultCards(snapshot.Platform, snapshot.Response); tally.Found {
			result.AdCards += tally.Ads
			result.GuideCards += tally.Guides
			result.Unreadable += tally.Unreadable
		}
		for _, item := range radarCandidateItems(snapshot.Platform, snapshot.Response) {
			signal := normalizeRadarSignal(niche, keyword, snapshot.Platform, snapshot.FetchedAt, dataDir, item)
			if signal.Title == "" {
				result.Skipped++
				continue
			}
			workID := radarWorkIdentity(signal.VideoID, signal.URL, signal.Title)
			if workID == "" {
				result.Skipped++
				continue
			}
			result.Captures++
			inserted, captureErr := database.RecordRadarCapture(db.RadarCapture{
				Platform: snapshot.Platform, WorkID: workID, PoolID: poolID, PoolName: poolName,
				Term: keyword, Author: signal.Creator, Title: signal.Title, URL: signal.URL,
				PublishedAt: signal.PublishedAt,
				CallID:      snapshot.CallID, Play: signal.Play, Likes: signal.Likes,
				Collects: signal.Collects, Shares: signal.Shares, Comments: signal.Comments,
				ObservedAt: snapshot.FetchedAt,
			})
			if captureErr != nil {
				return result, captureErr
			}
			if inserted {
				result.Inserted++
			} else {
				result.Duplicates++
			}
		}
	}
	if runs, runErr := radar.ListRuns(dataDir, 0); runErr == nil {
		for _, run := range runs {
			record := db.RadarRun{
				ID: run.ID, NicheID: run.NicheID, NicheName: run.NicheName, Platform: run.Platform,
				EndpointID: run.EndpointID, Terms: run.Terms, State: run.State,
				StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, LastRetryAt: run.LastRetryAt,
				ItemCap:  run.ItemCap,
				MaxCalls: run.MaxCalls, EstimatedCostUSD: run.EstimatedCostUSD, CostBasis: run.CostBasis,
				TotalCalls: run.TotalCalls, Succeeded: run.Succeeded, Failed: run.Failed,
				Kept: run.Kept, Items: run.Items,
			}
			for _, call := range run.Calls {
				record.Calls = append(record.Calls, db.RadarRunCall{
					CallID: call.CallID, Term: call.Term, Platform: call.Platform,
					EndpointID: call.EndpointID, Status: call.Status, ErrorCode: call.ErrorCode,
					ErrorMessage: call.ErrorMessage, ItemCount: call.ItemCount,
					KeptCount: call.KeptCount, AdCount: call.AdCount, GuideCount: call.GuideCount,
					UnreadableCount: call.UnreadableCount,
					DurationMS:      call.DurationMS, RawPath: call.RawPath,
				})
			}
			if err := database.RecordRadarRun(record); err != nil {
				return result, err
			}
			result.Runs++
		}
	}
	stats, err := database.RadarStoreStats()
	if err != nil {
		return result, err
	}
	result.Stats = stats
	return result, nil
}

// ensureRadarStore populates an empty store on first read so the UI never shows
// a half-built backend.
func ensureRadarStore(database *db.DB, dataDir string) {
	if database == nil {
		return
	}
	stats, err := database.RadarStoreStats()
	if err != nil {
		return
	}
	if stats["radar_works"] > 0 || stats["radar_observations"] > 0 {
		return
	}
	_, _ = ingestRadarStore(database, dataDir, nil)
}

func (s *Server) handleAPITrendRadarStore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		writeDataAPIError(w, http.StatusServiceUnavailable, "db_unavailable", "本地数据库未就绪")
		return
	}
	stats, err := s.db.RadarStoreStats()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "store_unavailable", "无法读取雷达存储")
		return
	}
	writeJSON(w, map[string]any{"stats": stats, "store": "sqlite"})
}

func (s *Server) handleAPITrendRadarStoreBackfill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "雷达回填只允许从当前本地应用发起")
		return
	}
	if s.db == nil {
		writeDataAPIError(w, http.StatusServiceUnavailable, "db_unavailable", "本地数据库未就绪")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	result, err := ingestRadarStore(s.db, dataDir, nil)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "backfill_failed", "回填失败："+err.Error())
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleAPITrendRadarUnattributed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		writeDataAPIError(w, http.StatusServiceUnavailable, "db_unavailable", "本地数据库未就绪")
		return
	}
	terms, err := s.db.RadarUnattributedTerms()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "store_unavailable", "无法读取未归属搜索词")
		return
	}
	writeJSON(w, map[string]any{"terms": terms, "count": len(terms)})
}

func (s *Server) handleAPITrendRadarWorkAttribution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		writeDataAPIError(w, http.StatusServiceUnavailable, "db_unavailable", "本地数据库未就绪")
		return
	}
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	workID := strings.TrimSpace(r.URL.Query().Get("work_id"))
	if platform == "" || workID == "" {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_request", "需要 platform 与 work_id")
		return
	}
	work, err := s.db.RadarWorkAttribution(platform, workID)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "store_unavailable", "无法读取作品归属")
		return
	}
	if work == nil {
		writeDataAPIError(w, http.StatusNotFound, "work_not_found", "存储里没有这条作品")
		return
	}
	encoded, err := json.Marshal(work)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "encode_failed", "无法序列化")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(encoded)
}

// persistRadarRun records a finished run and only its own calls, so a routine
// collection does not rescan all history.
func (s *Server) persistRadarRun(dataDir string, run radar.Run) {
	if s.db == nil {
		return
	}
	record := db.RadarRun{
		ID: run.ID, NicheID: run.NicheID, NicheName: run.NicheName, Platform: run.Platform,
		EndpointID: run.EndpointID, Terms: run.Terms, State: run.State,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, ItemCap: run.ItemCap,
		MaxCalls: run.MaxCalls, EstimatedCostUSD: run.EstimatedCostUSD, CostBasis: run.CostBasis,
		TotalCalls: run.TotalCalls, Succeeded: run.Succeeded, Failed: run.Failed,
		Kept: run.Kept, Items: run.Items,
	}
	callIDs := map[int64]bool{}
	for _, call := range run.Calls {
		record.Calls = append(record.Calls, db.RadarRunCall{
			CallID: call.CallID, Term: call.Term, Platform: call.Platform,
			EndpointID: call.EndpointID, Status: call.Status, ErrorCode: call.ErrorCode,
			ErrorMessage: call.ErrorMessage, ItemCount: call.ItemCount,
			KeptCount: call.KeptCount, DurationMS: call.DurationMS, RawPath: call.RawPath,
		})
		if call.CallID > 0 {
			callIDs[call.CallID] = true
		}
	}
	if err := s.db.RecordRadarRun(record); err != nil {
		return
	}
	if len(callIDs) > 0 {
		_, _ = ingestRadarStore(s.db, dataDir, callIDs)
	}
}
