# Alpha-Nexus Project Agent

这是项目 Agent 的工作记忆入口，不替代 [`AGENTS.md`](../AGENTS.md) 的硬约束。

## 最小读序

1. 先读用户当前 Goal（[`docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md`](../docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md)）与 `AGENTS.md`。
2. 要改代码时读 `docs/MAP.md`；需要范围或流程判断时读 [`EVOLUTION.md`](./EVOLUTION.md)。
3. 按 area 查 [`evolution/CURRENT.md`](./evolution/CURRENT.md) 和 [`evolution/mistakes/INDEX.md`](./evolution/mistakes/INDEX.md)，只打开命中的 lesson 或 accepted ADR。
4. 以源码和本轮真实测试为准；记忆只提供已验证的提示，不能替代验证。

## 记录边界

- 有可复用的验证事实：更新 `evolution/CURRENT.md` 的原条目。
- 有非显然且已修复的错误：写 `evolution/mistakes/lessons/`，并更新唯一索引。
- 有架构取舍：写 `evolution/adr/`。
- 不创建运行时 Agent、后台任务、定时器、hooks、模型自动改写或跨会话自动同步。
- 不记录任何凭证、个人数据、原始日志或生产请求载荷。

历史长文仅在追溯时读取：[`evolution/archive/project_agent-legacy-2026-07-18.md`](./evolution/archive/project_agent-legacy-2026-07-18.md)。
