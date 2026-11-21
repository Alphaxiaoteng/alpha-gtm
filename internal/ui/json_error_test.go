package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShouldWrapErrorAsJSON(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		status      int
		want        bool
	}{
		{"plain text 400 需要包装", "text/plain; charset=utf-8", http.StatusBadRequest, true},
		{"空 content type 400 需要包装", "", http.StatusInternalServerError, true},
		{"SSE 不包装", "text/event-stream", http.StatusInternalServerError, false},
		{"已是 JSON 不重复包装", "application/json", http.StatusBadRequest, false},
		{"200 成功不包装", "text/plain; charset=utf-8", http.StatusOK, false},
		{"301 重定向不包装", "text/plain", http.StatusMovedPermanently, false},
		{"图片不包装", "image/png", http.StatusInternalServerError, false},
		{"HTML 不包装", "text/html; charset=utf-8", http.StatusNotFound, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldWrapErrorAsJSON(c.contentType, c.status); got != c.want {
				t.Fatalf("shouldWrapErrorAsJSON(%q,%d)=%v want %v", c.contentType, c.status, got, c.want)
			}
		})
	}
}

func TestErrorCodeFromMessage(t *testing.T) {
	cases := []struct {
		message  string
		status   int
		fallback string
		want     string
	}{
		{"method not allowed", http.StatusMethodNotAllowed, "X", "METHOD_NOT_ALLOWED"},
		{"account not found", http.StatusNotFound, "X", "ACCOUNT_NOT_FOUND"},
		{"invalid json", http.StatusBadRequest, "X", "INVALID_JSON"},
		// 已是错误码字面量则原样保留，不再二次转换
		{"SESSION_MONITOR_RUN_FAILED", http.StatusInternalServerError, "X", "SESSION_MONITOR_RUN_FAILED"},
		// 空消息走 fallback
		{"", http.StatusNotFound, "NOT_FOUND", "NOT_FOUND"},
		// 中文句子无法提取 token，走 fallback（不产生乱码码值）
		{"内容不存在", http.StatusNotFound, "NOT_FOUND", "NOT_FOUND"},
		// 超长句子走 fallback，避免生成巨型错误码
		{strings.Repeat("a", 200), http.StatusBadRequest, "BAD_REQUEST", "BAD_REQUEST"},
	}
	for _, c := range cases {
		t.Run(c.message, func(t *testing.T) {
			if got := errorCodeFromMessage(c.message, c.status, c.fallback); got != c.want {
				t.Fatalf("errorCodeFromMessage(%q)=%q want %q", c.message, got, c.want)
			}
		})
	}
}

func TestFallbackErrorCode(t *testing.T) {
	cases := map[int]string{
		http.StatusBadRequest:          "BAD_REQUEST",
		http.StatusUnauthorized:        "UNAUTHORIZED",
		http.StatusForbidden:           "FORBIDDEN",
		http.StatusNotFound:            "NOT_FOUND",
		http.StatusMethodNotAllowed:    "METHOD_NOT_ALLOWED",
		http.StatusConflict:            "CONFLICT",
		http.StatusUnprocessableEntity: "UNPROCESSABLE_ENTITY",
		http.StatusTooManyRequests:     "TOO_MANY_REQUESTS",
		http.StatusServiceUnavailable:  "SERVICE_UNAVAILABLE",
		http.StatusInternalServerError: "INTERNAL_ERROR",
		http.StatusInsufficientStorage: "INTERNAL_ERROR",
		http.StatusNotExtended:         "INTERNAL_ERROR", // 5xx 一律 INTERNAL_ERROR
		http.StatusTeapot:              "REQUEST_FAILED", // 未枚举的 4xx
	}
	for status, want := range cases {
		if got := fallbackErrorCode(status); got != want {
			t.Fatalf("fallbackErrorCode(%d)=%q want %q", status, got, want)
		}
	}
}

func TestWrapErrorAsJSONShape(t *testing.T) {
	body := wrapErrorAsJSON(http.StatusBadRequest, "type and status are invalid", "req-123")
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("非法 JSON: %v (%s)", err, body)
	}
	// 契约字段必须齐全，且与 panic 路径既有格式（error/errorCode/requestId）一致
	for _, key := range []string{"ok", "errorCode", "error_code", "message", "error", "requestId"} {
		if _, exists := out[key]; !exists {
			t.Fatalf("缺少字段 %s: %s", key, body)
		}
	}
	if out["ok"] != false {
		t.Fatalf("ok 必须为 false: %s", body)
	}
	if out["errorCode"] != "TYPE_AND_STATUS_ARE_INVALID" {
		t.Fatalf("errorCode 不符: %v", out["errorCode"])
	}
	if out["requestId"] != "req-123" {
		t.Fatalf("requestId 未回传: %v", out["requestId"])
	}
	// message 保留原文——既有 586 处 strings.Contains 测试断言依赖这点
	if out["message"] != "type and status are invalid" {
		t.Fatalf("message 必须保留原文: %v", out["message"])
	}
}

// 端到端由 TestFinishPendingWrapFlushesJSON 覆盖；
// http.Error → JSON 的真实链路已通过线上 curl 验证（400/404 均返回结构化体）。

func TestFinishPendingWrapFlushesJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &diagnosticResponseWriter{ResponseWriter: rec, requestID: "rid-9"}
	w.WriteHeader(http.StatusBadRequest)
	// 模拟 http.Error 的写入序列
	if _, err := w.Write([]byte("invalid json")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	w.finishPendingWrap()

	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type=%q 应为 JSON", ct)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码应为 400，实际 %d", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应非 JSON: %v %s", err, rec.Body.String())
	}
	if out["errorCode"] != "INVALID_JSON" {
		t.Fatalf("errorCode=%v", out["errorCode"])
	}
	if out["requestId"] != "rid-9" {
		t.Fatalf("requestId=%v", out["requestId"])
	}
	if !strings.Contains(rec.Body.String(), "invalid json") {
		t.Fatalf("原文必须保留在 message 中: %s", rec.Body.String())
	}
}

// 幂等性：重复调用不得重复写出。
func TestFinishPendingWrapIdempotent(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &diagnosticResponseWriter{ResponseWriter: rec, requestID: "rid-1"}
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("content not found"))
	w.finishPendingWrap()
	w.finishPendingWrap()
	// errorCode 与 error_code 各出现一次属正常；这里断言"整体只写了一个 JSON 体"。
	if strings.Count(rec.Body.String(), `"requestId":"rid-1"`) != 1 {
		t.Fatalf("重复写出: %s", rec.Body.String())
	}
	if strings.Count(rec.Body.String(), "}{") != 0 {
		t.Fatalf("拼接出多个 JSON 体: %s", rec.Body.String())
	}
}

// SSE 必须原样透传，不能被包装破坏。
func TestSSEStreamNotWrapped(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &diagnosticResponseWriter{ResponseWriter: rec, requestID: "rid-2"}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("data: hello\n\n"))
	w.finishPendingWrap()
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("SSE Content-Type 被篡改: %q", ct)
	}
	if rec.Body.String() != "data: hello\n\n" {
		t.Fatalf("SSE 内容被改写: %q", rec.Body.String())
	}
}
