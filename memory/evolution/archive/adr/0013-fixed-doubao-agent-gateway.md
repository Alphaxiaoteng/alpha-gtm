# 0013. Agent 与 Studio 规划统一固定豆包网关

| 字段 | 值 |
|------|-----|
| Status | superseded (Agent portion; Studio remains accepted) |
| Date | 2026-07-18 |
| Area | agent, llm, studio |

## Context

旧 Studio 和工具协同曾分别依赖 Ollama、本地模型或可配置路由，形成多套模型入口，也让不可达错误难以判断。当前产品只需要用户显式发起的固定豆包 API 能力。

> 2026-07-23：受控本地 CLI 的全局 Agent Hub 已由 [0014](./0014-global-agent-hub-local-cli.md) 接替本 ADR 的 Agent 部分；Studio 保持本 ADR 的固定豆包决策。

## Decision

1. 普通 Agent、工具计划和 Studio 剪辑方案统一复用固定豆包网关与固定模型；不提供模型列表、Base URL、路由、代理或本地模型回退。
2. 密钥只从现有系统安全存储读取；UI、日志、浏览器子进程和响应均不得显示或继承密钥。
3. 未配置、鉴权失败、网络失败或模型失败时 fail closed，返回稳定错误；不得合成本地回答冒充模型结果。
4. 模型只生成可审阅计划。浏览器动作、ffmpeg 执行和真实发布仍由用户在对应界面独立明确触发，Agent 不替代确认。
5. 不增加后台 Agent、定时器、模型探测、自动重试或新的 LLM 配置基础设施。

## Consequences

- 模型路径更少，错误与安全边界更清晰；Ollama 和 11434 不再是运行时前置条件。
- 固定 API 不可用时没有自动降级，用户必须先修复安全存储中的有效配置。
- 本地素材和 ffmpeg 产物仍留在本机，但生成方案会在用户明确触发后调用固定 API。

## Related

- Supersedes: [0010](./0010-local-studio-agent.md)
- Code: `internal/llm/doubao.go`, `internal/agents/llm_tools.go`, `internal/studio/agent.go`
- Tests: `internal/llm/doubao_test.go`, `internal/studio/agent_test.go`, `ui-web/test/settings-doubao-key.test.mjs`
