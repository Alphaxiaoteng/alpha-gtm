package ui

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"alpha-nexus-egolite/internal/notify"
)

const maxNotificationSettingsBodyBytes = 16 << 10

func (s *Server) handleAPINotificationSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.notifier == nil {
		writeNotificationSettingsError(w, notify.CodeInternal)
		return
	}
	switch r.Method {
	case http.MethodGet:
		settings, err := s.notifier.Settings()
		if err != nil {
			writeNotificationSettingsError(w, notify.CodeOf(err))
			return
		}
		writeJSON(w, settings)
	case http.MethodPut:
		if !s.requireTrustedNotificationWrite(w, r) {
			return
		}
		var body struct {
			ReminderIntervalHours int `json:"reminder_interval_hours"`
		}
		if err := decodeNotificationSettingsJSON(w, r, &body); err != nil {
			writeNotificationSettingsError(w, notify.CodeInvalidConfig)
			return
		}
		if err := s.notifier.SetReminderInterval(body.ReminderIntervalHours); err != nil {
			writeNotificationSettingsError(w, notify.CodeOf(err))
			return
		}
		settings, err := s.notifier.Settings()
		if err != nil {
			writeNotificationSettingsError(w, notify.CodeOf(err))
			return
		}
		writeJSON(w, settings)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPINotificationChannel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.requireTrustedNotificationWrite(w, r) {
		return
	}
	if s.notifier == nil {
		writeNotificationSettingsError(w, notify.CodeInternal)
		return
	}
	kind := strings.TrimSpace(r.PathValue("kind"))
	var body notify.ChannelUpdate
	if err := decodeNotificationSettingsJSON(w, r, &body); err != nil {
		writeNotificationSettingsError(w, notify.CodeInvalidConfig)
		return
	}
	channel, err := s.notifier.UpdateChannel(r.Context(), kind, body)
	if err != nil {
		writeNotificationSettingsError(w, notify.CodeOf(err))
		return
	}
	writeJSON(w, channel)
}

func (s *Server) handleAPINotificationChannelTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.requireTrustedNotificationWrite(w, r) {
		return
	}
	if s.notifier == nil {
		writeNotificationSettingsError(w, notify.CodeInternal)
		return
	}
	if r.Body != nil && r.ContentLength != 0 {
		var body struct{}
		if err := decodeNotificationSettingsJSON(w, r, &body); err != nil {
			writeNotificationSettingsError(w, notify.CodeInvalidConfig)
			return
		}
	}
	deliveryID, err := s.notifier.TestChannel(r.Context(), strings.TrimSpace(r.PathValue("kind")))
	if err != nil {
		writeNotificationSettingsError(w, notify.CodeOf(err))
		return
	}
	writeJSON(w, map[string]any{"ok": true, "delivery_id": deliveryID})
}

func decodeNotificationSettingsJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxNotificationSettingsBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func (s *Server) requireTrustedNotificationWrite(w http.ResponseWriter, r *http.Request) bool {
	if !s.requireLocalTerminal(w, r) {
		return false
	}
	if !sameOriginLoopbackRequest(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error_code": "NOTIFY_ORIGIN_DENIED", "message": "通知设置只允许本地应用修改"})
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnsupportedMediaType)
		_ = json.NewEncoder(w).Encode(map[string]string{"error_code": "NOTIFY_JSON_REQUIRED", "message": "通知设置需要 JSON 请求"})
		return false
	}
	return true
}

func writeNotificationSettingsError(w http.ResponseWriter, code notify.ErrorCode) {
	status := http.StatusInternalServerError
	message := "通知设置暂时不可用"
	switch code {
	case notify.CodeInvalidConfig, notify.CodeEndpointRejected:
		status, message = http.StatusBadRequest, "请检查通知设置"
	case notify.CodeNotConfigured:
		status, message = http.StatusConflict, "请先保存通知地址或邮箱密码"
	case notify.CodeKeychainUnavailable, notify.CodeKeychainReadFailed, notify.CodeKeychainWriteFailed:
		status, message = http.StatusServiceUnavailable, "无法访问系统钥匙串"
	case notify.CodeRequestFailed, notify.CodeProviderRejected, notify.CodeSMTPFailed:
		status, message = http.StatusBadGateway, "通知发送失败，请检查连接和配置"
	case notify.CodeQueueFull:
		status, message = http.StatusServiceUnavailable, "通知队列繁忙，请稍后重试"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error_code": string(code), "message": message})
}
