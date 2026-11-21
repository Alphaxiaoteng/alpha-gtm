package tikhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientHTTPFailureReturnsSafeDiagnostics(t *testing.T) {
	const secret = "synthetic-secret-that-must-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"Bearer ` + secret + `"}`))
	}))
	defer server.Close()

	client := New(secret)
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	_, err := client.postJSON(context.Background(), "/search", map[string]interface{}{"keyword": "test"})
	if err == nil {
		t.Fatal("expected provider failure")
	}
	code, message := SafeRequestFailure(err)
	if code != "http_429" || message != "TikHub HTTP 429 Too Many Requests" {
		t.Fatalf("diagnostics=(%q, %q)", code, message)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(message, secret) {
		t.Fatal("safe diagnostics leaked bearer token")
	}
}

func TestClientInvalidJSONReturnsSafeDiagnostics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer server.Close()

	client := New("synthetic-secret")
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	_, err := client.get(context.Background(), "/search", nil)
	code, message := SafeRequestFailure(err)
	if code != "invalid_json" || message != "TikHub HTTP 200 returned invalid JSON" {
		t.Fatalf("diagnostics=(%q, %q)", code, message)
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"` + strings.Repeat("x", maxResponseBytes) + `"}`))
	}))
	defer server.Close()

	client := New("synthetic-secret")
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	_, err := client.get(context.Background(), "/large", nil)
	code, message := SafeRequestFailure(err)
	if code != "response_too_large" || message != "TikHub response exceeded the local safety limit" {
		t.Fatalf("diagnostics=(%q, %q)", code, message)
	}
}

func TestClientDistinguishesLocalDeadlineFromTransportTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200}`))
	}))
	defer server.Close()

	requestContext, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	client := New("synthetic-token")
	client.BaseURL = server.URL
	_, err := client.get(requestContext, "/delayed", nil)
	code, message := SafeRequestFailure(err)
	if code != "request_timeout" || message != "TikHub request exceeded the local request deadline" {
		t.Fatalf("local deadline diagnostics = %q %q", code, message)
	}

	client = New("synthetic-token")
	client.BaseURL = server.URL
	client.HTTP.Timeout = time.Millisecond
	_, err = client.get(context.Background(), "/delayed", nil)
	code, message = SafeRequestFailure(err)
	if code != "transport_timeout" || message != "TikHub connection timed out before a provider response" {
		t.Fatalf("transport timeout diagnostics = %q %q", code, message)
	}
}

func TestClientFallsBackOnlyAfterTransportTimeout(t *testing.T) {
	primaryCalls := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryCalls++
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write([]byte(`{"code":200}`))
	}))
	defer primary.Close()
	fallbackCalls := 0
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fallbackCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"items":[]}}`))
	}))
	defer fallback.Close()

	client := New("synthetic-token")
	client.BaseURL = primary.URL
	client.FallbackBaseURL = fallback.URL
	client.HTTP.Timeout = 10 * time.Millisecond
	response, err := client.postJSON(context.Background(), "/search", map[string]interface{}{"keyword": "男性情感"})
	if err != nil || response["code"] == nil {
		t.Fatalf("fallback response=%#v err=%v", response, err)
	}
	if primaryCalls != 1 || fallbackCalls != 1 {
		t.Fatalf("calls primary=%d fallback=%d", primaryCalls, fallbackCalls)
	}
}

// TikHub 会用 HTTP 200 返回 {"code":400,...} 这类业务失败。只看状态码会把一次
// 已经付费的失败调用记成成功。
func TestClientReportsBusinessFailureInsideHTTPSuccess(t *testing.T) {
	const secret = "synthetic-secret-that-must-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":400,"message":"Request failed. Please retry.","detail":"Bearer ` + secret + `"}`))
	}))
	defer server.Close()

	client := New(secret)
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	if _, err := client.get(context.Background(), "/search", nil); err == nil {
		t.Fatal("a business failure inside HTTP 200 must not look like a success")
	} else {
		code, message := SafeRequestFailure(err)
		if code != "provider_400" {
			t.Fatalf("diagnostics=(%q, %q)", code, message)
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(message, secret) {
			t.Fatal("safe diagnostics leaked bearer token")
		}
	}
}

// 成功信封的 code 是 200（上游平台复用该字段时会写 0），其余形状不能误判。
func TestClientAcceptsSuccessfulEnvelopes(t *testing.T) {
	for _, body := range []string{
		`{"code":200,"data":{"items":[]}}`,
		`{"code":0,"data":{"items":[]}}`,
		`{"code":"200"}`,
		`{"code":200.0}`,
		`{"data":{"items":[]}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		client := New("synthetic-token")
		client.BaseURL = server.URL
		client.HTTP = server.Client()
		response, err := client.get(context.Background(), "/search", nil)
		server.Close()
		if err != nil {
			t.Fatalf("body %s reported as failure: %v", body, err)
		}
		if response == nil {
			t.Fatalf("body %s decoded to nil", body)
		}
	}
}

func TestClientDoesNotFallbackAfterProviderHTTPError(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"rate limited"}`))
	}))
	defer primary.Close()
	fallbackCalls := 0
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fallbackCalls++
		_, _ = w.Write([]byte(`{"code":200}`))
	}))
	defer fallback.Close()

	client := New("synthetic-token")
	client.BaseURL = primary.URL
	client.FallbackBaseURL = fallback.URL
	_, err := client.get(context.Background(), "/search", nil)
	code, _ := SafeRequestFailure(err)
	if code != "http_429" || fallbackCalls != 0 {
		t.Fatalf("provider error code=%q fallback_calls=%d", code, fallbackCalls)
	}
}
