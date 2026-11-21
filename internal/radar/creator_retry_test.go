package radar

import "testing"

// 只有可能在下一次成功的失败才值得重试。每次重试都是一次付费调用，
// 对终态错误重试等于白花钱。
func TestIsRetryableHTTPStatus(t *testing.T) {
	cases := []struct {
		code string
		want bool
		why  string
	}{
		{"http_400", true, "TikHub 对部分 cursor 会短暂返回 400"},
		{"http_429", true, "被限流，退避后可重试"},
		{"http_500", true, "上游故障"},
		{"http_502", true, "上游故障"},
		{"http_503", true, "上游故障"},
		{"http_401", false, "凭证错误，重试不会变"},
		{"http_403", false, "被禁，重试不会变"},
		{"http_404", false, "资源不存在，重试不会变"},
		{"network_error", false, "非 http_ 前缀由上游分支处理"},
		{"http_", false, "空状态码不可解析"},
		{"http_abc", false, "非法状态码不可解析"},
	}
	for _, tc := range cases {
		if got := isRetryableHTTPStatus(tc.code); got != tc.want {
			t.Errorf("isRetryableHTTPStatus(%q) = %v, want %v (%s)", tc.code, got, tc.want, tc.why)
		}
	}
}
