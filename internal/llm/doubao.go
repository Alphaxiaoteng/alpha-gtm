package llm

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	// DoubaoBaseURL 与 DoubaoModelID 是 Alpha Agent 唯一允许的远程模型契约。
	DoubaoBaseURL = "https://ark.cn-beijing.volces.com/api/v3"
	DoubaoModelID = "doubao-seed-2-0-pro-260215"
)

// loadDoubaoKeychainKey 在测试中可替换；生产实现只从 macOS 钥匙串读取。
var loadDoubaoKeychainKey = loadDoubaoKeychainKeyImpl

// saveDoubaoKeychainKey is replaceable only for offline tests. Production
// implementations write directly to the macOS Keychain and never to app data.
var saveDoubaoKeychainKey = saveDoubaoKeychainKeyImpl

// SaveDoubaoAPIKey updates the sole Agent credential in secure storage.
// ponytail: no provider/model fields are accepted; the fixed runtime contract
// remains the single source of truth.
func SaveDoubaoAPIKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("豆包 API 密钥不能为空")
	}
	return saveDoubaoKeychainKey(key)
}

func doubaoKeychainInput(key string) string {
	// security prompts once for the new value and once for confirmation.
	return key + "\n" + key + "\n"
}

// ResolveDoubaoConfig 不读取 llm.json、环境变量、代理或 fallback 路由。
// ponytail: 仅支持当前唯一模型；需要多模型时新增明确的用户选择流程，而非恢复隐式路由配置。
func ResolveDoubaoConfig() Config {
	key := strings.TrimSpace(loadDoubaoKeychainKey())
	source := "keychain-missing"
	if key != "" {
		source = "keychain"
	}

	return Config{
		BaseURL: DoubaoBaseURL,
		APIKey:  key,
		Model:   DoubaoModelID,
		Source:  source,
	}
}

// NewDoubaoGateway 创建固定直连方舟的网关。它不会继承系统或本机模型代理。
func NewDoubaoGateway(timeout time.Duration) *Gateway {
	cfg := ResolveDoubaoConfig()
	if cfg.APIKey == "" {
		return nil
	}
	return &Gateway{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
		Client:  NewDoubaoHTTPClient(timeout),
		// 固定模型不可用时必须明确失败，不能生成本地兜底内容并伪装为 Agent 输出。
		Strict: true,
	}
}

// NewDoubaoHTTPClient 强制直连，忽略 HTTP_PROXY/HTTPS_PROXY 与旧代理配置。
func NewDoubaoHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Timeout: timeout, Transport: transport}
}
