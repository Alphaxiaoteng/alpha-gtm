package mobileui

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 手机端页面必须能在局域网里打开：/, /index.html 返回内嵌页，其他路径 404。
func TestMobilePageServesEmbeddedHTML(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/mobile/summary" {
			_, _ = w.Write([]byte(`{"accounts":{"total":1}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	server, err := New(Options{Listen: "127.0.0.1:0", Upstream: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Shutdown(context.Background()) }()

	base := "http://" + listener.Addr().String()
	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Alpha-Nexus") {
		t.Fatalf("page body missing title")
	}

	// /api/* 必须原样反代给本机 UI，鉴权与 CORS 都由上游决定。
	apiResp, err := http.Get(base + "/api/mobile/summary")
	if err != nil {
		t.Fatal(err)
	}
	apiBody, _ := io.ReadAll(apiResp.Body)
	apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusOK || !strings.Contains(string(apiBody), "\"accounts\"") {
		t.Fatalf("proxy status=%d body=%s", apiResp.StatusCode, apiBody)
	}

	// 未定义路径不得回落到页面，否则手机点错地址会看到一个空壳。
	notFound, err := http.Get(base + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer notFound.Body.Close()
	if notFound.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path status=%d", notFound.StatusCode)
	}
}

// upstream 不是绝对 URL 时必须明确拒绝，而不是悄悄起一个什么都不转的服务。
func TestNewRejectsRelativeUpstream(t *testing.T) {
	for _, upstream := range []string{"", "127.0.0.1:17421", "/api"} {
		if _, err := New(Options{Upstream: upstream}); err == nil {
			t.Fatalf("upstream %q should be rejected", upstream)
		}
	}
}

// 非私网来源必须被拒：手机端只服务局域网，公网暴露需要用户另做隧道。
func TestPrivateRemoteGate(t *testing.T) {
	allowed := []string{"127.0.0.1:1234", "192.168.1.7:5000", "10.0.0.3:9000", "[::1]:1234"}
	for _, addr := range allowed {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = addr
		if !isPrivateRemote(r) {
			t.Errorf("remote %q should be allowed", addr)
		}
	}
	denied := []string{"8.8.8.8:443", "1.1.1.1:80"}
	for _, addr := range denied {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = addr
		if isPrivateRemote(r) {
			t.Errorf("remote %q should be denied", addr)
		}
	}
}

// LANAddresses 只返回私网地址，绝不把公网 IP 打印给用户。
func TestLANAddressesOnlyPrivate(t *testing.T) {
	for _, addr := range LANAddresses() {
		ip := net.ParseIP(addr)
		if ip == nil || !ip.IsPrivate() {
			t.Fatalf("non-private address leaked: %q", addr)
		}
	}
}
