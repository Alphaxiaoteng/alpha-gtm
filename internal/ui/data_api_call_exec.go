package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tikhub"
	"alpha-nexus-egolite/internal/trace"
)

// dataAPICallOutcome carries everything one paid call produced, so the HTTP
// handler and the Trend Radar runner can share a single execution path.
// ErrorCode is empty on success; Call is set whenever a local call row exists.
type dataAPICallOutcome struct {
	Call            *db.TikHubAPICall
	HTTPStatus      int
	ErrorCode       string
	ErrorMessage    string
	Headers         map[string]string
	ItemCount       int64
	DurationMS      int64
	ProviderCode    string
	ProviderMessage string
	// Reused 表示这次没有产生付费调用，回放的是窗口内同参数的既有快照。
	Reused   bool
	Response map[string]any
	RawPath  string
	// Cards 是 keyword_search 响应里平台自己标出来的卡（广告卡/引导卡）与
	// 读不到条目的逐条账；Found=false 表示响应形态不认识，数字不作数。
	Cards radarResultTally
}

func (o *dataAPICallOutcome) failed(status int, code, message string) *dataAPICallOutcome {
	o.HTTPStatus = status
	o.ErrorCode = code
	o.ErrorMessage = message
	return o
}

// executeDataAPICall creates the local call record, executes a paid TikHub
// request, persists the raw snapshot, and records the outcome. The paid call is
// detached from the request context on purpose: once money is spent the result
// must be persisted even if the renderer navigates away.
func (s *Server) executeDataAPICall(ctx context.Context, dataDir string, endpoint tikhub.EndpointDefinition, prepared map[string]any) *dataAPICallOutcome {
	outcome := &dataAPICallOutcome{Headers: map[string]string{}}
	providerConfig, err := tikhub.LoadProviderConfig(dataDir)
	if err != nil || !providerConfig.Active || providerConfig.TestStatus != tikhub.TestStatusPassed {
		return outcome.failed(http.StatusConflict, "provider_not_active", "请先完成 TikHub 真实连接测试并激活")
	}
	// 先看窗口内有没有同样参数的响应：有就回放，不花钱。
	// 磁盘索引是跨进程的那一份（radar worker 与博主归档也写它），进程内 map
	// 只负责吸收同一秒内的并发重复，两者都未命中才真的发起付费调用。
	if entry, hit := tikhub.LookupReusableCall(dataDir, endpoint.ID, prepared, endpoint.Capability, time.Now()); hit {
		if response, readErr := readSnapshotResponse(dataDir, entry.RawPath); readErr == nil {
			outcome.Response = response
			outcome.RawPath = entry.RawPath
			outcome.DurationMS = 0
			outcome.Reused = true
			if call, callErr := s.db.GetTikHubAPICall(entry.CallID); callErr == nil {
				outcome.Call = call
			}
			return outcome
		}
	}
	if cached, hit := s.dataAPICache.lookup(endpoint.ID, prepared, time.Now()); hit {
		if response, readErr := readSnapshotResponse(dataDir, cached.RawPath); readErr == nil {
			outcome.Response = response
			outcome.RawPath = cached.RawPath
			outcome.DurationMS = 0
			// 复用不产生新的调用记录；标记来源，便于界面区分。
			outcome.Reused = true
			if call, callErr := s.db.GetTikHubAPICall(cached.CallID); callErr == nil {
				outcome.Call = call
			}
			return outcome
		}
	}

	parametersJSON, err := json.Marshal(prepared)
	if err != nil {
		return outcome.failed(http.StatusBadRequest, "invalid_parameters", "调用参数无法保存")
	}
	traceID := string(trace.New())
	call, err := s.db.CreateTikHubAPICall(db.TikHubAPICallInput{
		TraceID:        traceID,
		PlatformCode:   endpoint.Platform,
		EndpointID:     endpoint.ID,
		Capability:     endpoint.Capability,
		Method:         endpoint.Method,
		Path:           endpoint.Path,
		ParametersJSON: string(parametersJSON),
	})
	if err != nil {
		return outcome.failed(http.StatusInternalServerError, "history_write_failed", "无法创建本地调用记录")
	}
	outcome.Call = call

	started := time.Now()
	callContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 75*time.Second)
	defer cancel()
	execute := s.tikhubCallEndpoint
	if execute == nil {
		client, _, clientErr := tikhub.NewActiveClient(dataDir)
		if clientErr != nil {
			_, _ = s.db.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
				Status: "failed", ErrorCode: "provider_not_active", DurationMS: time.Since(started).Milliseconds(),
			})
			return outcome.failed(http.StatusConflict, "provider_not_active", "TikHub 配置未激活")
		}
		execute = client.CallPreparedEndpoint
	}
	response, callErr := execute(callContext, endpoint, prepared)
	duration := time.Since(started).Milliseconds()
	outcome.DurationMS = duration
	if callErr != nil {
		providerCode, providerMessage := tikhub.SafeRequestFailure(callErr)
		outcome.ProviderCode, outcome.ProviderMessage = providerCode, providerMessage
		finished, _ := s.db.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
			Status: "failed", ErrorCode: "provider_request_failed", ProviderCode: providerCode,
			ProviderMessage: providerMessage, DurationMS: duration,
		})
		if finished != nil {
			outcome.Call = finished
		}
		_, _ = s.db.CreateOperationEvent(db.OperationEventInput{
			TraceID: traceID, PlatformCode: endpoint.Platform, Component: "tikhub",
			EventType: "api_call", Step: endpoint.ID, Status: "failed", Level: "error",
			ErrorCode: "provider_request_failed", DurationMS: duration,
		})
		return outcome.failed(http.StatusBadGateway, "provider_request_failed", providerMessage+"；详细状态已保存")
	}

	snapshot := tikhub.CallSnapshot{
		CallID: call.ID, TraceID: traceID, Platform: endpoint.Platform, EndpointID: endpoint.ID,
		Capability: endpoint.Capability, Method: endpoint.Method, Path: endpoint.Path,
		Parameters: prepared, Response: response,
	}
	rawPath, responseBytes, saveErr := tikhub.SaveCallSnapshot(dataDir, snapshot)
	providerCode, providerMessage := safeProviderMetadata(response)
	outcome.ProviderCode, outcome.ProviderMessage = providerCode, providerMessage
	itemCount := tikhub.CountPrimaryItems(response)
	outcome.ItemCount = itemCount
	// 成功拿到响应就入缓存，供窗口内的同参数请求回放。
	if saveErr == nil {
		now := time.Now()
		s.dataAPICache.store(endpoint.ID, prepared, dataAPICacheEntry{
			Response: response, RawPath: rawPath, CallID: call.ID,
		}, now)
		tikhub.RecordReusableCall(dataDir, endpoint.ID, prepared,
			tikhub.ReuseEntry{CallID: call.ID, RawPath: rawPath}, now)
	}
	if endpoint.Capability == "keyword_search" {
		outcome.Cards = radarClassifyResultCards(endpoint.Platform, response)
	}
	if saveErr != nil {
		_, _ = s.db.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
			Status: "storage_failed", ErrorCode: "snapshot_write_failed", ProviderCode: providerCode,
			ProviderMessage: providerMessage, ItemCount: itemCount, DurationMS: duration,
		})
		return outcome.failed(http.StatusInternalServerError, "snapshot_write_failed", "接口已返回，但主要数据未能完整保存")
	}
	if endpoint.Capability == "creator_profile" || endpoint.Capability == "creator_posts" {
		if _, archiveErr := tikhub.ArchiveCreatorSnapshot(dataDir, rawPath, snapshot); archiveErr != nil {
			_, _ = s.db.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
				Status: "storage_failed", ErrorCode: "creator_archive_failed", ProviderCode: providerCode,
				ProviderMessage: providerMessage, ItemCount: itemCount, ResponseBytes: responseBytes,
				RawPath: rawPath, DurationMS: duration,
			})
			return outcome.failed(http.StatusInternalServerError, "creator_archive_failed", "完整原始数据已保存，但博主文件夹归档失败")
		}
	}
	if endpoint.Platform == "douyin" && endpoint.Capability == "content_detail" {
		if _, archiveErr := tikhub.ArchiveVideoLookupSnapshot(dataDir, rawPath, snapshot); archiveErr != nil {
			_, _ = s.db.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
				Status: "storage_failed", ErrorCode: "video_search_archive_failed", ProviderCode: providerCode,
				ProviderMessage: providerMessage, ItemCount: itemCount, ResponseBytes: responseBytes,
				RawPath: rawPath, DurationMS: duration,
			})
			return outcome.failed(http.StatusInternalServerError, "video_search_archive_failed", "完整原始数据已保存，但视频搜索文件夹归档失败")
		}
	}
	transcriptPath := ""
	if endpoint.Capability == "content_transcript" {
		content, language := tikhub.ExtractTranscriptContent(response)
		if content != "" {
			transcriptPath, err = tikhub.SaveTranscriptMarkdown(rawPath, fmt.Sprint(prepared["video_id"]), language, content)
			if err != nil {
				_, _ = s.db.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
					Status: "storage_failed", ErrorCode: "transcript_write_failed", ProviderCode: providerCode,
					ProviderMessage: providerMessage, ItemCount: itemCount, ResponseBytes: responseBytes,
					RawPath: rawPath, DurationMS: duration,
				})
				return outcome.failed(http.StatusInternalServerError, "transcript_write_failed", "完整字幕 JSON 已保存，但 Markdown 写入失败")
			}
		}
	}
	coverContext, cancelCovers := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	coverResult := tikhub.CacheContentCovers(coverContext, dataDir, response)
	cancelCovers()
	duration = time.Since(started).Milliseconds()
	outcome.DurationMS = duration
	finished, err := s.db.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
		Status: "succeeded", ProviderCode: providerCode, ProviderMessage: providerMessage,
		ItemCount: itemCount, ResponseBytes: responseBytes, RawPath: rawPath, DurationMS: duration,
	})
	if err != nil {
		return outcome.failed(http.StatusInternalServerError, "history_write_failed", "主要数据已保存，但调用索引更新失败")
	}
	outcome.Call = finished
	_, _ = s.db.CreateOperationEvent(db.OperationEventInput{
		TraceID: traceID, PlatformCode: endpoint.Platform, Component: "tikhub",
		EventType: "api_call", Step: endpoint.ID, Status: "succeeded", Level: "info", DurationMS: duration,
	})
	outcome.Headers["X-Nexus-Covers-Discovered"] = strconv.Itoa(coverResult.Discovered)
	outcome.Headers["X-Nexus-Covers-Cached"] = strconv.Itoa(coverResult.Cached + coverResult.Existing)
	if transcriptPath != "" {
		outcome.Headers["X-Nexus-Transcript-Path"] = transcriptPath
	}
	return outcome
}
