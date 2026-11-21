package ui

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// 统一错误响应契约。
//
// 历史状况：internal/ui 里有 620 处 http.Error(w, "xxx", status)，返回的是
// plain text 短句；同时又有 9 个各写各的 writeXxxError helper，输出
// {"error":"CODE"} / {"error_code":code} / {"ok":false,...} 三种形态。
// 前端 api.ts 的 request<T>() 期望的是 {errorCode|error_code, message}，
// 因此绝大多数错误都走不到结构化分支，只能把英文原文直接抛给用户。
//
// 收敛策略：不在 620 处调用点上逐个改（风险高、易漏），改为在
// diagnosticResponseWriter 这一唯一出口上把 4xx/5xx 的 plain text 响应
// 统一包装成 JSON。message 保留原文，既有测试的 strings.Contains 断言
// 不受影响；同时补上 errorCode 与 requestId，让前端能结构化处理。

// 4xx/5xx 且是 plain text 才包装。2xx/3xx、SSE、图片、文件下载必须原样放行。
func shouldWrapErrorAsJSON(contentType string, status int) bool {
	if status < 400 {
		return false
	}
	ct := strings.ToLower(contentType)
	if ct == "" {
		return true
	}
	// 已经是 JSON 的错误响应（各 writeXxxError helper 产出）不重复包装。
	if strings.Contains(ct, "application/json") {
		return false
	}
	// SSE / 事件流不碰，否则会破坏流式推送。
	if strings.Contains(ct, "text/event-stream") {
		return false
	}
	// 只有 text/plain（http.Error 的默认输出）需要包装。
	return strings.HasPrefix(ct, "text/plain")
}

var errorCodeTokenPattern = regexp.MustCompile(`[^A-Z0-9]+`)

// 把英文短句转成稳定的大写错误码，前端可据此分支，不必比对文案。
// "method not allowed" -> "METHOD_NOT_ALLOWED"
// "account not found"  -> "ACCOUNT_NOT_FOUND"
// 无法提取有效 token 时返回 fallback。
func errorCodeFromMessage(message string, status int, fallback string) string {
	const maxLen = 48
	trimmed := strings.TrimSpace(message)
	// 已经是 CODE 形态（例如某些 handler 直接把 error 常量写进 http.Error）则原样保留。
	if trimmed != "" && isErrorCodeLiteral(trimmed) {
		return trimmed
	}
	upper := strings.ToUpper(trimmed)
	token := errorCodeTokenPattern.ReplaceAllString(upper, "_")
	token = strings.Trim(token, "_")
	runeCount := len([]rune(token))
	if runeCount == 0 || runeCount > maxLen {
		return fallback
	}
	return token
}

var errorCodeLiteralPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,47}$`)

func isErrorCodeLiteral(value string) bool {
	// 纯中文/含空格的普通句子不算；全大写下划线才算错误码字面量。
	return errorCodeLiteralPattern.MatchString(value) && !strings.Contains(value, " ")
}

// 从状态码兜底一个语义码，供无法从文案提取时使用。
func fallbackErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "BAD_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusMethodNotAllowed:
		return "METHOD_NOT_ALLOWED"
	case http.StatusConflict:
		return "CONFLICT"
	case http.StatusUnprocessableEntity:
		return "UNPROCESSABLE_ENTITY"
	case http.StatusTooManyRequests:
		return "TOO_MANY_REQUESTS"
	case http.StatusServiceUnavailable:
		return "SERVICE_UNAVAILABLE"
	default:
		if status >= 500 {
			return "INTERNAL_ERROR"
		}
		return "REQUEST_FAILED"
	}
}

// 包装成统一 JSON 错误体。ok 恒为 false，方便前端一眼区分成功/失败。
func wrapErrorAsJSON(status int, message, requestID string) []byte {
	message = strings.TrimSpace(message)
	if message == "" {
		message = http.StatusText(status)
	}
	code := errorCodeFromMessage(message, status, fallbackErrorCode(status))
	body, err := json.Marshal(map[string]any{
		"ok":         false,
		"errorCode":  code,
		"error_code": code, // 兼容既有前端与 CLI 的 error_code 读取
		"message":    message,
		"error":      message, // 兼容既有前端的 error 读取
		"requestId":  requestID,
	})
	if err != nil {
		// 极小概率（message 含非法 UTF-8 已被 json 替换，不会到这）；退回最小可用体。
		return []byte(`{"ok":false,"errorCode":"INTERNAL_ERROR","message":"internal server error"}`)
	}
	return body
}
