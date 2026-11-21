package ui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/radar"
	"alpha-nexus-egolite/internal/tikhub"
)

// maxRadarRunCalls bounds one explicit collection round. TikHub bills per
// request, so this is the credit guard that sits in front of the per-platform
// item cap.
const maxRadarRunCalls = 8

// radarNicheRunLocks claims one paid collection round per niche+data dir. A
// single round can spend up to maxRadarRunCalls paid calls, so two concurrent
// POSTs for the same niche must not both run: the second is refused with 409
// instead of double-billing the configured search terms.
var radarNicheRunLocks sync.Map

// claimRadarNicheRun returns (key, true) when this caller owns the niche run, or
// (key, false) when a round for the same niche is still executing.
func claimRadarNicheRun(dataDir, nicheID string) (string, bool) {
	key := dataDir + "\x00" + nicheID
	_, loaded := radarNicheRunLocks.LoadOrStore(key, struct{}{})
	return key, !loaded
}

func releaseRadarNicheRun(key string) {
	radarNicheRunLocks.Delete(key)
}

func radarKeywordEndpoint(platform string) (string, bool) {
	switch platform {
	case "douyin":
		return "douyin.video_search", true
	case "xhs":
		return "xhs.note_search", true
	case "kuaishou":
		return "kuaishou.search", true
	case "wechat_video":
		return "wechat_video.keyword_search", true
	case "wechat_mp":
		return "wechat_mp.keyword_search", true
	case "youtube":
		return "youtube.video_search", true
	case "x":
		return "x.keyword_search", true
	default:
		return "", false
	}
}

func newRadarRunID() string {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "run-" + time.Now().UTC().Format("20060102t150405")
	}
	return "run-" + time.Now().UTC().Format("20060102t150405") + "-" + hex.EncodeToString(suffix)
}

// handleAPITrendRadarNicheRun executes one explicit, paid collection round for
// a niche on one platform and records what it cost and what failed.
func (s *Server) handleAPITrendRadarNicheRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "推荐池抓取只允许从当前本地应用发起")
		return
	}
	var body struct {
		Platform string   `json:"platform"`
		Terms    []string `json:"terms"`
		All      bool     `json:"all"`
		Confirm  bool     `json:"confirm"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_request", "抓取参数格式不正确")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_request", "抓取参数包含多余 JSON")
		return
	}
	if !body.Confirm {
		writeDataAPIError(w, http.StatusBadRequest, "confirmation_required", "请确认后再发起真实接口调用")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	config, err := tikhub.LoadRadarConfig(dataDir)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "config_unavailable", "推荐池配置损坏")
		return
	}
	nicheID := strings.TrimSpace(r.PathValue("id"))
	var niche *tikhub.RadarNiche
	for i := range config.Niches {
		if config.Niches[i].ID == nicheID {
			niche = &config.Niches[i]
			break
		}
	}
	if niche == nil {
		writeDataAPIError(w, http.StatusNotFound, "niche_not_found", "推荐池不存在")
		return
	}
	if !niche.Enabled {
		writeDataAPIError(w, http.StatusConflict, "niche_disabled", "该推荐池已停用")
		return
	}
	platform := strings.TrimSpace(body.Platform)
	if !radarStringSliceContains(niche.Platforms, platform) {
		writeDataAPIError(w, http.StatusBadRequest, "platform_not_allowed", "该推荐池未启用这个平台")
		return
	}
	endpointID, ok := radarKeywordEndpoint(platform)
	if !ok {
		writeDataAPIError(w, http.StatusBadRequest, "platform_not_supported", "该平台暂不支持推荐池抓取")
		return
	}
	endpoint, ok := tikhub.FindEndpoint(endpointID)
	if !ok {
		writeDataAPIError(w, http.StatusBadRequest, "endpoint_not_allowed", "该接口不在 Nexus 精选白名单中")
		return
	}
	terms, termErr := radarRunTerms(niche, body.Terms, body.All)
	if termErr != "" {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_terms", termErr)
		return
	}
	providerConfig, err := tikhub.LoadProviderConfig(dataDir)
	if err != nil || !providerConfig.Active || providerConfig.TestStatus != tikhub.TestStatusPassed {
		writeDataAPIError(w, http.StatusConflict, "provider_not_active", "请先完成 TikHub 真实连接测试并激活")
		return
	}

	itemCap := niche.ItemLimit(platform)
	run := radar.Run{
		ID: newRadarRunID(), NicheID: niche.ID, NicheName: niche.Name, Platform: platform,
		EndpointID: endpoint.ID, Terms: terms, ItemCap: itemCap, MaxCalls: maxRadarRunCalls,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if cost, priced := tikhub.EstimateCallCostUSD(endpoint); priced {
		run.EstimatedCostUSD = cost * float64(len(terms))
		run.CostBasis = "按本地登记的官方基础价 " + formatUSD(cost) + " / 次估算"
	} else {
		run.CostBasis = "本地未登记该接口单价，按实际账单为准"
	}
	runLockKey, claimed := claimRadarNicheRun(dataDir, niche.ID)
	if !claimed {
		writeDataAPIError(w, http.StatusConflict, "AGENT_RUN_ACTIVE", "该推荐池正在抓取中，请等待当前这一轮结束")
		return
	}
	defer releaseRadarNicheRun(runLockKey)

	run, err = radar.StartRun(dataDir, run)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "run_start_failed", "无法创建抓取记录")
		return
	}

	for _, term := range terms {
		prepared, prepErr := tikhub.PrepareEndpointParams(endpoint, map[string]any{"keyword": term})
		if prepErr != nil {
			run = s.recordRadarRunCall(dataDir, run, radar.RunCall{
				Term: term, Platform: platform, EndpointID: endpoint.ID, Status: "failed",
				ErrorCode: "invalid_parameters", ErrorMessage: prepErr.Error(), KeptCount: 0,
			})
			break
		}
		outcome := s.executeDataAPICall(r.Context(), dataDir, endpoint, prepared)
		call := radar.RunCall{
			Term: term, Platform: platform, EndpointID: endpoint.ID, Status: "succeeded",
			ItemCount: outcome.ItemCount, DurationMS: outcome.DurationMS,
		}
		if outcome.Cards.Found {
			call.AdCount = outcome.Cards.Ads
			call.GuideCount = outcome.Cards.Guides
			call.UnreadableCount = outcome.Cards.Unreadable
		}
		if outcome.Call != nil {
			call.CallID = outcome.Call.ID
			call.RawPath = outcome.Call.RawPath
		}
		kept := int(outcome.ItemCount)
		if kept > itemCap {
			kept = itemCap
		}
		call.KeptCount = kept
		if outcome.ErrorCode != "" {
			call.Status = "failed"
			call.ErrorCode = outcome.ErrorCode
			call.ErrorMessage = outcome.ErrorMessage
			call.KeptCount = 0
		}
		run = s.recordRadarRunCall(dataDir, run, call)
		if call.Status != "succeeded" {
			// Stop instead of burning credit on a provider that is clearly failing.
			break
		}
	}
	finished, err := radar.FinishRun(dataDir, run.ID)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "run_finish_failed", "抓取已完成，但记录写入失败")
		return
	}
	// 落库：这一轮的调用明细、花费与入库结果要能事后查，不靠重扫 JSON。
	s.persistRadarRun(dataDir, finished)
	writeJSON(w, finished)
}

func (s *Server) recordRadarRunCall(dataDir string, run radar.Run, call radar.RunCall) radar.Run {
	updated, err := radar.AppendRunCall(dataDir, run.ID, call)
	if err != nil {
		// Keep the in-memory record so the response still reports the attempt.
		run.Calls = append(run.Calls, call)
		return run
	}
	return updated
}

func radarRunTerms(niche *tikhub.RadarNiche, requested []string, all bool) ([]string, string) {
	var terms []string
	if all {
		terms = append(terms, niche.SearchTerms...)
	} else if len(requested) == 0 {
		if len(niche.SearchTerms) == 0 {
			return nil, "该推荐池没有搜索词"
		}
		terms = []string{niche.SearchTerms[0]}
	} else {
		allowed := make(map[string]bool, len(niche.SearchTerms))
		for _, term := range niche.SearchTerms {
			allowed[strings.TrimSpace(term)] = true
		}
		seen := map[string]bool{}
		for _, term := range requested {
			term = strings.TrimSpace(term)
			if term == "" || seen[term] {
				continue
			}
			if !allowed[term] {
				return nil, "搜索词必须来自该推荐池的配置：" + term
			}
			seen[term] = true
			terms = append(terms, term)
		}
	}
	if len(terms) == 0 {
		return nil, "请至少选择一个搜索词"
	}
	if len(terms) > maxRadarRunCalls {
		return nil, "单轮最多 " + strconv.Itoa(maxRadarRunCalls) + " 次调用，请减少搜索词"
	}
	return terms, ""
}

// handleAPITrendRadarRunRetry re-runs only the failed calls of one finished
// round. Terms that already succeeded are never called again, so a retry cannot
// double-charge the account, and the retry stops at the first failure exactly
// like the round itself did.
func (s *Server) handleAPITrendRadarRunRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "推荐池重试只允许从当前本地应用发起")
		return
	}
	var body struct {
		Confirm bool `json:"confirm"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_request", "重试参数格式不正确")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_request", "重试参数包含多余 JSON")
		return
	}
	if !body.Confirm {
		writeDataAPIError(w, http.StatusBadRequest, "confirmation_required", "请确认后再重试真实接口调用")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	run, err := radar.LoadRun(dataDir, strings.TrimSpace(r.PathValue("id")))
	if err != nil {
		writeDataAPIError(w, http.StatusNotFound, "run_not_found", "抓取记录不存在，无法重试")
		return
	}
	if run.State == radar.RunStateRunning {
		writeDataAPIError(w, http.StatusConflict, "run_running", "这一轮还没结束，结束后才能重试失败项")
		return
	}
	terms := run.PendingRetryTerms()
	if len(terms) == 0 {
		writeDataAPIError(w, http.StatusConflict, "nothing_to_retry", "这一轮没有失败调用需要重试")
		return
	}
	endpoint, ok := tikhub.FindEndpoint(run.EndpointID)
	if !ok {
		writeDataAPIError(w, http.StatusBadRequest, "endpoint_not_allowed", "该接口不在 Nexus 精选白名单中")
		return
	}
	providerConfig, err := tikhub.LoadProviderConfig(dataDir)
	if err != nil || !providerConfig.Active || providerConfig.TestStatus != tikhub.TestStatusPassed {
		writeDataAPIError(w, http.StatusConflict, "provider_not_active", "请先完成 TikHub 真实连接测试并激活")
		return
	}
	unitCost, priced := tikhub.EstimateCallCostUSD(endpoint)
	retried := make([]string, 0, len(terms))
	for _, term := range terms {
		prepared, prepErr := tikhub.PrepareEndpointParams(endpoint, map[string]any{"keyword": term})
		if prepErr != nil {
			run = s.recordRadarRunCall(dataDir, run, radar.RunCall{
				Term: term, Platform: run.Platform, EndpointID: endpoint.ID, Status: "failed",
				ErrorCode: "invalid_parameters", ErrorMessage: prepErr.Error(), KeptCount: 0,
			})
			retried = append(retried, term)
			break
		}
		outcome := s.executeDataAPICall(r.Context(), dataDir, endpoint, prepared)
		call := radar.RunCall{
			Term: term, Platform: run.Platform, EndpointID: endpoint.ID, Status: "succeeded",
			ItemCount: outcome.ItemCount, DurationMS: outcome.DurationMS,
		}
		if outcome.Call != nil {
			call.CallID = outcome.Call.ID
			call.RawPath = outcome.Call.RawPath
		}
		kept := int(outcome.ItemCount)
		if run.ItemCap > 0 && kept > run.ItemCap {
			kept = run.ItemCap
		}
		call.KeptCount = kept
		if outcome.ErrorCode != "" {
			call.Status = "failed"
			call.ErrorCode = outcome.ErrorCode
			call.ErrorMessage = outcome.ErrorMessage
			call.KeptCount = 0
		}
		run = s.recordRadarRunCall(dataDir, run, call)
		retried = append(retried, term)
		if call.Status != "succeeded" {
			// Stop instead of burning credit on a provider that is clearly failing.
			break
		}
	}
	retryCost := 0.0
	if priced {
		retryCost = unitCost * float64(len(retried))
	}
	finished, err := radar.FinishRetry(dataDir, run.ID, retryCost)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "run_retry_finish_failed", "重试已完成，但记录写入失败")
		return
	}
	s.persistRadarRun(dataDir, finished)
	pending := finished.PendingRetryTerms()
	pendingSet := make(map[string]bool, len(pending))
	for _, term := range pending {
		pendingSet[term] = true
	}
	recovered := make([]string, 0, len(retried))
	for _, term := range retried {
		if !pendingSet[term] {
			recovered = append(recovered, term)
		}
	}
	writeJSON(w, map[string]any{
		"ok": true, "run": finished, "retried_terms": retried,
		"recovered_terms": recovered, "pending_terms": pending,
	})
}

func (s *Server) handleAPITrendRadarRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	if id := strings.TrimSpace(r.PathValue("id")); id != "" {
		run, loadErr := radar.LoadRun(dataDir, id)
		if loadErr != nil {
			writeDataAPIError(w, http.StatusNotFound, "run_not_found", "抓取记录不存在")
			return
		}
		writeJSON(w, run)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	nicheID := strings.TrimSpace(r.URL.Query().Get("niche_id"))
	// 优先读数据库；库里还没有记录时退回文件，老数据不会因为换存储而消失。
	if s.db != nil {
		if stored, storeErr := s.db.RadarRuns(nicheID, limit); storeErr == nil && len(stored) > 0 {
			writeJSON(w, map[string]any{"runs": stored, "count": len(stored), "store": "sqlite"})
			return
		}
	}
	runs, err := radar.ListRuns(dataDir, limit)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "runs_unavailable", "无法读取抓取记录")
		return
	}
	if nicheID != "" {
		filtered := make([]radar.Run, 0, len(runs))
		for _, run := range runs {
			if run.NicheID == nicheID {
				filtered = append(filtered, run)
			}
		}
		runs = filtered
	}
	writeJSON(w, map[string]any{"runs": runs, "count": len(runs)})
}

func formatUSD(value float64) string {
	return "$" + strconv.FormatFloat(value, 'f', 2, 64)
}

// handleAPITrendRadarNicheLimits updates the per-platform item caps of one
// niche without rewriting the whole radar configuration from the client.
func (s *Server) handleAPITrendRadarNicheLimits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "PUT only", http.StatusMethodNotAllowed)
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "推荐池配置只允许从当前本地应用修改")
		return
	}
	var body struct {
		PlatformLimits map[string]int `json:"platform_limits"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_request", "条数上限格式不正确")
		return
	}
	if len(body.PlatformLimits) == 0 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_request", "请至少提供一个平台的条数上限")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	nicheID := strings.TrimSpace(r.PathValue("id"))
	saved := tikhub.RadarNiche{}
	found := false
	config, loadErr := tikhub.LoadRadarConfig(dataDir)
	if loadErr != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "config_unavailable", "推荐池配置损坏")
		return
	}
	for index := range config.Niches {
		if config.Niches[index].ID != nicheID {
			continue
		}
		found = true
		for platform, limit := range body.PlatformLimits {
			if !radarStringSliceContains(config.Niches[index].Platforms, platform) {
				writeDataAPIError(w, http.StatusBadRequest, "platform_not_allowed", "该推荐池未启用这个平台")
				return
			}
			if limit < 1 || limit > tikhub.RadarMaxItemCap {
				writeDataAPIError(w, http.StatusBadRequest, "invalid_limit", "条数上限必须在 1-"+strconv.Itoa(tikhub.RadarMaxItemCap)+" 之间")
				return
			}
			if config.Niches[index].PlatformLimits == nil {
				config.Niches[index].PlatformLimits = map[string]int{}
			}
			config.Niches[index].PlatformLimits[platform] = limit
		}
		saved = config.Niches[index]
		break
	}
	if !found {
		writeDataAPIError(w, http.StatusNotFound, "niche_not_found", "推荐池不存在")
		return
	}
	if _, saveErr := tikhub.SaveRadarConfig(dataDir, config); saveErr != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_config", saveErr.Error())
		return
	}
	writeJSON(w, saved)
}
