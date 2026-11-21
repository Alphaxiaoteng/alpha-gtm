package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type staticResolver struct {
	addresses []net.IPAddr
	err       error
}

func (resolver staticResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return resolver.addresses, resolver.err
}

func TestWebhookValidationPinsOfficialHostsAndRejectsPrivateGenericTargets(t *testing.T) {
	ctx := context.Background()
	public := staticResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}
	private := staticResolver{addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}}
	tests := []struct {
		kind     string
		endpoint string
		resolver ipResolver
		wantCode ErrorCode
	}{
		{kind: "feishu", endpoint: "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-hook"},
		{kind: "dingtalk", endpoint: "https://oapi.dingtalk.com/robot/send?access_token=synthetic-token"},
		{kind: "feishu", endpoint: "https://example.com/open-apis/bot/v2/hook/synthetic-hook", wantCode: CodeEndpointRejected},
		{kind: "dingtalk", endpoint: "https://oapi.dingtalk.com/robot/send", wantCode: CodeEndpointRejected},
		{kind: "webhook", endpoint: "https://127.0.0.1/hook", resolver: public, wantCode: CodeEndpointRejected},
		{kind: "webhook", endpoint: "https://100.64.0.1/hook", resolver: public, wantCode: CodeEndpointRejected},
		{kind: "webhook", endpoint: "https://198.18.0.1/hook", resolver: public, wantCode: CodeEndpointRejected},
		{kind: "webhook", endpoint: "https://203.0.113.10/hook", resolver: public, wantCode: CodeEndpointRejected},
		{kind: "webhook", endpoint: "https://224.0.0.1/hook", resolver: public, wantCode: CodeEndpointRejected},
		{kind: "webhook", endpoint: "https://240.0.0.1/hook", resolver: public, wantCode: CodeEndpointRejected},
		{kind: "webhook", endpoint: "https://hooks.example.test/notify", resolver: private, wantCode: CodeEndpointRejected},
		{kind: "webhook", endpoint: "https://hooks.example.test/notify", resolver: public},
		{kind: "webhook", endpoint: "http://hooks.example.test/notify", resolver: public, wantCode: CodeEndpointRejected},
	}
	for _, test := range tests {
		err := validateWebhookURL(ctx, test.kind, test.endpoint, test.resolver)
		if got := CodeOf(err); got != test.wantCode {
			t.Errorf("kind=%s endpoint host validation code=%s want=%s", test.kind, got, test.wantCode)
		}
	}
}

func TestWebhookPayloadsAndBusinessCodesFailClosed(t *testing.T) {
	tests := []struct {
		name         string
		kind         string
		response     string
		wantCode     ErrorCode
		payloadField string
	}{
		{name: "feishu code success", kind: "feishu", response: `{"code":0,"msg":"ok"}`, payloadField: "msg_type"},
		{name: "feishu legacy success", kind: "feishu", response: `{"StatusCode":0,"StatusMessage":"ok"}`, payloadField: "msg_type"},
		{name: "feishu business failure", kind: "feishu", response: `{"code":19021,"msg":"rejected"}`, wantCode: CodeProviderRejected, payloadField: "msg_type"},
		{name: "feishu missing status", kind: "feishu", response: `{"msg":"unknown"}`, wantCode: CodeProviderRejected, payloadField: "msg_type"},
		{name: "dingtalk success", kind: "dingtalk", response: `{"errcode":0,"errmsg":"ok"}`, payloadField: "msgtype"},
		{name: "dingtalk business failure", kind: "dingtalk", response: `{"errcode":310000,"errmsg":"rejected"}`, wantCode: CodeProviderRejected, payloadField: "msgtype"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var observed map[string]any
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected request method=%s content-type=%s", r.Method, r.Header.Get("Content-Type"))
				}
				if err := json.NewDecoder(r.Body).Decode(&observed); err != nil {
					t.Errorf("decode payload: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()
			sender := &DefaultSender{httpClient: server.Client(), timeout: time.Second}
			err := sender.sendWebhook(context.Background(), test.kind, server.URL, Message{EventType: "test", Title: "测试", Body: "安全内容"})
			if got := CodeOf(err); got != test.wantCode {
				t.Fatalf("code=%s want=%s", got, test.wantCode)
			}
			if _, ok := observed[test.payloadField]; !ok {
				t.Fatalf("payload missing %q: %#v", test.payloadField, observed)
			}
		})
	}
}

func TestWebhookRedirectIsNeverFollowed(t *testing.T) {
	var targetCalls int
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls++ }))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusFound)
	}))
	defer source.Close()
	client := source.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	sender := &DefaultSender{httpClient: client, timeout: time.Second}
	err := sender.sendWebhook(context.Background(), "webhook", source.URL, Message{EventType: "test", Title: "测试", Body: "安全内容"})
	if CodeOf(err) != CodeProviderRejected || targetCalls != 0 {
		t.Fatalf("code=%s targetCalls=%d", CodeOf(err), targetCalls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestFixedProviderUsesProxyClientWithoutOpeningGenericWebhookProxy(t *testing.T) {
	var directCalls, providerCalls int
	response := func(body string) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}
	}
	sender := &DefaultSender{
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			directCalls++
			return response(`{"code":0}`), nil
		})},
		providerHTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			providerCalls++
			return response(`{"code":0}`), nil
		})},
		providerProxy: func(*http.Request) (*url.URL, error) {
			return &url.URL{Scheme: "http", Host: "operator-proxy.test:8080"}, nil
		},
	}
	message := Message{EventType: "test", Title: "测试", Body: "安全内容"}
	if err := sender.sendWebhook(context.Background(), "feishu", "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-hook", message); err != nil {
		t.Fatal(err)
	}
	if providerCalls != 1 || directCalls != 0 {
		t.Fatalf("fixed provider calls provider=%d direct=%d", providerCalls, directCalls)
	}
	if err := sender.sendWebhook(context.Background(), "webhook", "https://hooks.example.test/notify", message); err != nil {
		t.Fatal(err)
	}
	if providerCalls != 1 || directCalls != 1 {
		t.Fatalf("generic webhook calls provider=%d direct=%d", providerCalls, directCalls)
	}
	sender.providerProxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	if err := sender.sendWebhook(context.Background(), "feishu", "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-hook", message); err != nil {
		t.Fatal(err)
	}
	if providerCalls != 2 || directCalls != 1 {
		t.Fatalf("provider without proxy calls provider=%d direct=%d", providerCalls, directCalls)
	}
}

func TestDefaultSenderKeepsGenericWebhookTransportDirect(t *testing.T) {
	sender := NewDefaultSender(time.Second)
	direct, ok := sender.httpClient.Transport.(*http.Transport)
	if !ok || direct.Proxy != nil || direct.DialContext == nil {
		t.Fatal("generic webhook transport must keep proxy disabled and safe dialing enabled")
	}
	provider, ok := sender.providerHTTPClient.Transport.(*http.Transport)
	if !ok || provider.Proxy == nil {
		t.Fatal("fixed provider transport must be able to use the operator proxy")
	}
}

func TestFixedProviderRejectsBrokenProxyWithoutDirectFallback(t *testing.T) {
	directCalls := 0
	sender := &DefaultSender{
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			directCalls++
			return nil, errors.New("must not use direct client")
		})},
		providerHTTPClient: &http.Client{},
		providerProxy: func(*http.Request) (*url.URL, error) {
			return &url.URL{Scheme: "socks5", Host: "proxy.test:1080"}, nil
		},
	}
	err := sender.sendWebhook(context.Background(), "feishu", "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-hook", Message{EventType: "test", Title: "测试", Body: "安全内容"})
	if CodeOf(err) != CodeRequestFailed || directCalls != 0 {
		t.Fatalf("code=%s directCalls=%d", CodeOf(err), directCalls)
	}
}

func TestEmailValidationRequiresEncryptedTransportAndPlainAddresses(t *testing.T) {
	valid := testEmailChannel()
	if err := validateEmailChannel(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.SMTPSecurity = "plain"
	if CodeOf(validateEmailChannel(invalid)) != CodeInvalidConfig {
		t.Fatal("plain SMTP was accepted")
	}
	invalid = valid
	invalid.EmailFrom = "Alpha <sender@example.test>"
	if CodeOf(validateEmailChannel(invalid)) != CodeInvalidConfig {
		t.Fatal("display-name email was accepted")
	}
	if strings.Contains(sanitizeHeader("hello\r\nBcc: x@example.test"), "\n") {
		t.Fatal("header injection survived sanitization")
	}
	message := buildEmailMessage(valid, Message{Title: "账号需要重新登录\r\nBcc: x@example.test", Body: "安全内容"})
	lines := strings.Split(message, "\r\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[2], "Subject: ") || strings.HasPrefix(lines[3], "Bcc:") {
		t.Fatalf("unsafe email headers: %q", message)
	}
	decoded, err := (&mime.WordDecoder{}).DecodeHeader(strings.TrimPrefix(lines[2], "Subject: "))
	if err != nil || !strings.Contains(decoded, "账号需要重新登录") || strings.Contains(decoded, "\n") {
		t.Fatalf("decoded subject=%q err=%v", decoded, err)
	}
}
