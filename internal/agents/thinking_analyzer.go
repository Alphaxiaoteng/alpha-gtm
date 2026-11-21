package agents

import (
	"strings"
)

// ShouldEnableReasoning 根据官方 API 规范的 reasoning_effort 参数判断是否需要深度思考。
// 官方参数选项: auto | none | low | medium | high
func ShouldEnableReasoning(msg string, mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "none", "off", "false", "0":
		return false
	case "low", "medium", "high", "deep", "on", "1024", "4096", "16384":
		return true
	}

	// 默认 auto 智能模式：简单短消息（如 "1", "hi", "ok"）关闭思考；复杂代码/分析开启思考
	trimmed := strings.TrimSpace(msg)
	if len([]rune(trimmed)) <= 15 {
		return false
	}

	keywords := []string{
		"code", "function", "error", "bug", "refactor", "how to", "why",
		"代码", "重构", "修复", "报错", "分析", "架构", "设计", "算法", "实现",
	}
	lower := strings.ToLower(trimmed)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	return len([]rune(trimmed)) > 80
}
