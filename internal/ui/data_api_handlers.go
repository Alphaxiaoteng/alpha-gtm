package ui

import (
	"context"
	"encoding/json"
	"net/http"

	"alpha-nexus-egolite/internal/tikhub"
)

type dataAPIEndpoint struct {
	ID          string                     `json:"id,omitempty"`
	Name        string                     `json:"name,omitempty"`
	Method      string                     `json:"method,omitempty"`
	Path        string                     `json:"path,omitempty"`
	DocsURL     string                     `json:"docs_url,omitempty"`
	CostHint    string                     `json:"cost_hint,omitempty"`
	FixedParams map[string]any             `json:"fixed_params,omitempty"`
	Params      []tikhub.EndpointParameter `json:"params,omitempty"`
}

type dataAPICapability struct {
	Capability string            `json:"capability"`
	Label      string            `json:"label"`
	Binding    string            `json:"binding"`
	Message    string            `json:"message"`
	Endpoints  []dataAPIEndpoint `json:"endpoints,omitempty"`
}

type dataAPIPlatform struct {
	Platform     string              `json:"platform"`
	Name         string              `json:"name"`
	Capabilities []dataAPICapability `json:"capabilities"`
}

type dataAPIConfigResponse struct {
	Provider        string              `json:"provider"`
	BaseURL         string              `json:"base_url"`
	BaseOptions     []map[string]string `json:"base_options"`
	Configured      bool                `json:"configured"`
	Active          bool                `json:"active"`
	State           string              `json:"state"`
	TestStatus      string              `json:"test_status"`
	TestedAt        string              `json:"tested_at,omitempty"`
	ErrorCode       string              `json:"error_code,omitempty"`
	TestEndpoint    dataAPIEndpoint     `json:"test_endpoint"`
	EndpointCatalog []dataAPIPlatform   `json:"endpoint_catalog"`
}

func (s *Server) handleAPIDataAPIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	s.writeDataAPIConfig(w, http.StatusOK)
}

func (s *Server) handleAPIDataAPICredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "PUT only", http.StatusMethodNotAllowed)
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "数据 API 配置只允许从当前本地应用修改")
		return
	}
	var body struct {
		APIKey string `json:"api_key"`
		Region string `json:"region"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_request", "配置格式不正确")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	if _, err := tikhub.SaveProviderCredential(dataDir, body.APIKey, body.Region); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_credential", "API Key 或区域无效")
		return
	}
	s.writeDataAPIConfig(w, http.StatusOK)
}

func (s *Server) handleAPIDataAPITest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "数据 API 测试只允许从当前本地应用发起")
		return
	}
	var body struct {
		Confirm bool `json:"confirm"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_request", "测试请求格式不正确")
		return
	}
	if !body.Confirm {
		writeDataAPIError(w, http.StatusBadRequest, "confirmation_required", "请明确确认后再发起真实连接测试")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	cfg, err := tikhub.LoadProviderConfig(dataDir)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "provider_config_invalid", "数据 API 配置损坏")
		return
	}
	key, err := tikhub.LoadAPIKey(dataDir)
	if err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "credential_missing", "请先保存 API Key")
		return
	}
	testConnection := s.tikhubTestConnection
	if testConnection == nil {
		testConnection = testTikHubConnection
	}
	if err := testConnection(r.Context(), key, cfg.BaseURL); err != nil {
		_ = tikhub.MarkProviderTest(dataDir, false, "provider_rejected")
		s.writeDataAPIConfig(w, http.StatusBadGateway)
		return
	}
	if err := tikhub.MarkProviderTest(dataDir, true, ""); err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "activation_write_failed", "连接成功，但本地激活状态保存失败")
		return
	}
	s.writeDataAPIConfig(w, http.StatusOK)
}

func testTikHubConnection(ctx context.Context, key, baseURL string) error {
	client, err := tikhub.NewWithBaseURL(key, baseURL)
	if err != nil {
		return err
	}
	_, err = client.GetTikHubUserInfo(ctx)
	return err
}

func (s *Server) writeDataAPIConfig(w http.ResponseWriter, status int) {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	cfg, err := tikhub.LoadProviderConfig(dataDir)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "provider_config_invalid", "数据 API 配置损坏")
		return
	}
	configured := tikhub.ProviderConfigured(dataDir)
	state := "credential_missing"
	if configured {
		state = "awaiting_test"
	}
	if cfg.TestStatus == tikhub.TestStatusFailed {
		state = "test_failed"
	}
	if configured && cfg.Active && cfg.TestStatus == tikhub.TestStatusPassed {
		state = "active"
	}
	writeJSONStatus(w, status, dataAPIConfigResponse{
		Provider: "tikhub",
		BaseURL:  cfg.BaseURL,
		BaseOptions: []map[string]string{
			{"region": tikhub.RegionMainland, "label": "中国大陆加速", "base_url": tikhub.MainlandBase},
			{"region": tikhub.RegionGlobal, "label": "海外 / 全球", "base_url": tikhub.DefaultBase},
		},
		Configured: configured,
		Active:     state == "active",
		State:      state,
		TestStatus: cfg.TestStatus,
		TestedAt:   cfg.TestedAt,
		ErrorCode:  cfg.ErrorCode,
		TestEndpoint: dataAPIEndpoint{
			Method: http.MethodGet,
			Path:   "/api/v1/tikhub/user/get_user_info",
		},
		EndpointCatalog: fixedDataAPIEndpointCatalog(),
	})
}

func writeDataAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSONStatus(w, status, map[string]any{"ok": false, "error_code": code, "message": message})
}

func fixedDataAPIEndpointCatalog() []dataAPIPlatform {
	type platformMeta struct {
		code string
		name string
	}
	platforms := []platformMeta{
		{code: "douyin", name: "抖音"},
		{code: "xhs", name: "小红书"},
		{code: "wechat_video", name: "微信视频号"},
		{code: "kuaishou", name: "快手"},
		{code: "wechat_mp", name: "微信公众号"},
		{code: "youtube", name: "YouTube"},
		{code: "x", name: "X"},
		// TikTok / Instagram 目前只在精选目录里做关键词搜索（趋势雷达的赛道配置
		// 白名单还没放这两个平台），所以详情/博主能力会显示「本地生成」。
		{code: "tiktok", name: "TikTok"},
		{code: "instagram", name: "Instagram"},
	}
	capabilityLabels := []struct {
		code  string
		label string
	}{
		{code: "keyword_search", label: "关键词搜索"},
		{code: "content_detail", label: "内容详情"},
		{code: "content_transcript", label: "视频字幕"},
		{code: "creator_profile", label: "博主资料"},
		{code: "creator_posts", label: "博主作品"},
		{code: "topic_selection", label: "选题"},
		{code: "benchmark", label: "对标"},
		{code: "circle_analysis", label: "圈层"},
	}
	definitions := tikhub.CuratedEndpoints()
	result := make([]dataAPIPlatform, 0, len(platforms))
	for _, platform := range platforms {
		item := dataAPIPlatform{Platform: platform.code, Name: platform.name}
		for _, capability := range capabilityLabels {
			endpoints := make([]dataAPIEndpoint, 0)
			for _, definition := range definitions {
				if definition.Platform != platform.code || definition.Capability != capability.code {
					continue
				}
				endpoints = append(endpoints, dataAPIEndpoint{
					ID: definition.ID, Name: definition.Name, Method: definition.Method,
					Path: definition.Path, DocsURL: definition.DocsURL, CostHint: definition.CostHint,
					FixedParams: definition.FixedParams, Params: definition.Params,
				})
			}
			binding := "bound"
			message := "已绑定 Nexus 精选官方接口；调用结果完整保存到本地。"
			if len(endpoints) == 0 {
				binding = "local_derived"
				switch capability.code {
				case "topic_selection":
					message = "由赛道搜索与博主快照在本地生成选题。"
				case "benchmark":
					message = "由博主资料与作品快照在本地生成对标分析。"
				case "circle_analysis":
					message = "由关键词、博主与互动数据在本地生成圈层分析。"
				}
			}
			item.Capabilities = append(item.Capabilities, dataAPICapability{
				Capability: capability.code, Label: capability.label, Binding: binding,
				Message: message, Endpoints: endpoints,
			})
		}
		result = append(result, item)
	}
	return result
}
