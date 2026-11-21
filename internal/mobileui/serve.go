// Package mobileui 托管手机端的单页界面。
//
// 为什么单独一个包而不是塞进 internal/ui：
//
//	internal/ui 的 Serve 强制要求 listener 是回环地址（那是全应用最重要的一条
//	安全断言，不能为手机端放宽）。手机要能在局域网里打开页面，就必然监听
//	0.0.0.0 —— 所以页面必须由一个明确知道自己在做什么的独立入口提供。
//
// 这个包只做三件事：托管一个内嵌的静态页；把 /api/* 原样反代到本机 UI；
// 打印出可访问的局域网地址。它不读数据库、不碰 EgoLite、不写任何东西。
package mobileui

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

//go:embed page.html
var pageHTML []byte

// Options 是手机端服务的启动参数。
type Options struct {
	// Listen 是监听地址，例如 "0.0.0.0:17422"。
	Listen string
	// Upstream 是本机 UI 的地址，例如 "http://127.0.0.1:17421"。
	Upstream string
	// AllowedOrigin 非空时用于 CORS 白名单（一般留空：页面与 API 同源）。
	AllowedOrigin string
	// AllowPrivateOnly 为 true 时拒绝非私网来源（默认 true）。
	AllowPrivateOnly bool
}

// Server 是手机端页面的 HTTP 服务。
type Server struct {
	opts   Options
	proxy  *httputil.ReverseProxy
	server *http.Server
}

// New 构造服务。upstream 必须是 http(s) URL。
func New(opts Options) (*Server, error) {
	target, err := url.Parse(strings.TrimSpace(opts.Upstream))
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("mobileui: upstream must be an absolute URL")
	}
	if strings.TrimSpace(opts.Listen) == "" {
		opts.Listen = "0.0.0.0:17422"
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	// 上游是本机 UI，它自己会做鉴权与 CORS；这里只透传，不额外加头。
	proxy.Transport = &http.Transport{
		Proxy:                 nil,
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, "本机服务不可达："+err.Error(), http.StatusBadGateway)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opts.AllowPrivateOnly && !isPrivateRemote(r) {
			http.Error(w, "仅限局域网访问", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			proxy.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			// 移动端页面要允许内联脚本：这里刻意不加 CSP，因为页面是本程序
			// 自己内嵌的静态资源，没有任何第三方脚本。
			_, _ = w.Write(pageHTML)
			return
		}
		http.NotFound(w, r)
	})

	return &Server{
		opts:  opts,
		proxy: proxy,
		server: &http.Server{
			Addr:              opts.Listen,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       2 * time.Minute,
		},
	}, nil
}

// isPrivateRemote 只允许私网来源：局域网、本机、以及 IPv6 链路本地。
//
// 手机端页面本身不带写操作，但它会把 /api/* 反代给本机 UI；如果暴露到公网，
// 任何拿到地址的人都能试探本机接口。这一层是「别不小心把家底挂到公网」的
// 兜底，不是主防线（主防线是 /api/* 的 Bearer 鉴权）。
func isPrivateRemote(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// ListenAndServe 启动服务。listener 由调用方持有，便于测试。
func (s *Server) Serve(listener net.Listener) error {
	return s.server.Serve(listener)
}

// Shutdown 优雅关闭。
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Addr 返回实际监听地址（端口为 0 时由系统分配）。
func (s *Server) Addr() string { return s.server.Addr }

// LANAddresses 列出本机所有可用于手机访问的 IPv4 地址。
//
// 只返回私网地址：把公网地址打印给用户，等于鼓励把服务挂出去。
func LANAddresses() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, item := range interfaces {
		if item.Flags&net.FlagUp == 0 || item.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := item.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch typed := addr.(type) {
			case *net.IPNet:
				ip = typed.IP
			case *net.IPAddr:
				ip = typed.IP
			}
			if ip == nil || ip.To4() == nil || !ip.IsPrivate() {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
}
