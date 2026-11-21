package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tikhub"
)

func (s *Server) handleAPIDataAPICallsList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	calls, err := s.db.ListTikHubAPICalls(db.TikHubAPICallFilter{
		PlatformCode: strings.TrimSpace(r.URL.Query().Get("platform")),
		EndpointID:   strings.TrimSpace(r.URL.Query().Get("endpoint_id")),
		Status:       strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:        limit,
	})
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "history_unavailable", "无法读取本地调用记录")
		return
	}
	writeJSON(w, map[string]any{"calls": calls, "count": len(calls)})
}

func (s *Server) handleAPIDataAPICall(w http.ResponseWriter, r *http.Request) {
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "数据 API 调用只允许从当前本地应用发起")
		return
	}
	var body struct {
		EndpointID string         `json:"endpoint_id"`
		Params     map[string]any `json:"params"`
		Confirm    bool           `json:"confirm"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_request", "调用参数格式不正确")
		return
	}
	if !body.Confirm {
		writeDataAPIError(w, http.StatusBadRequest, "confirmation_required", "请明确确认后再发起真实接口调用")
		return
	}
	endpoint, ok := tikhub.FindEndpoint(body.EndpointID)
	if !ok {
		writeDataAPIError(w, http.StatusBadRequest, "endpoint_not_allowed", "该接口不在 Nexus 精选白名单中")
		return
	}
	prepared, err := tikhub.PrepareEndpointParams(endpoint, body.Params)
	if err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_parameters", err.Error())
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	outcome := s.executeDataAPICall(r.Context(), dataDir, endpoint, prepared)
	for name, value := range outcome.Headers {
		w.Header().Set(name, value)
	}
	if outcome.ErrorCode != "" {
		writeDataAPIError(w, outcome.HTTPStatus, outcome.ErrorCode, outcome.ErrorMessage)
		return
	}
	writeJSON(w, outcome.Call)
}

func (s *Server) handleAPIDataAPICallResponse(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_call_id", "调用记录 ID 无效")
		return
	}
	call, err := s.db.GetTikHubAPICall(id)
	if err != nil || call.RawPath == "" {
		writeDataAPIError(w, http.StatusNotFound, "snapshot_not_found", "该调用没有可读取的本地原始数据")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	root, err := filepath.Abs(filepath.Join(dataDir, "tikhub", "calls"))
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "snapshot_unavailable", "无法定位本地原始数据")
		return
	}
	target, err := filepath.Abs(call.RawPath)
	if err != nil || (target != root && !strings.HasPrefix(target, root+string(os.PathSeparator))) {
		writeDataAPIError(w, http.StatusForbidden, "snapshot_path_invalid", "原始数据路径不安全")
		return
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		writeDataAPIError(w, http.StatusNotFound, "snapshot_not_found", "本地原始数据文件不存在")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="tikhub-call-%d.json"`, call.ID))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(raw)
}

func safeProviderMetadata(response map[string]interface{}) (string, string) {
	code := ""
	if value, ok := response["code"]; ok {
		code = fmt.Sprint(value)
	}
	message := ""
	for _, key := range []string{"message", "msg"} {
		if value, ok := response[key]; ok {
			message = strings.TrimSpace(fmt.Sprint(value))
			break
		}
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "bearer") || strings.Contains(lower, "token") || strings.Contains(lower, "api key") {
		message = "[redacted]"
	}
	if len(message) > 500 {
		message = message[:500]
	}
	if len(code) > 64 {
		code = code[:64]
	}
	return code, message
}
