# 0004. Lite 四平台 V1 不做自动发布

| 字段 | 值 |
|------|-----|
| Status | **superseded (partial)** → [0006](./0006-agentpush-full-scope.md) |
| Date | 2026-05-01 |
| Area | publish |

> **AgentPush 路径：** 0006 已放宽「永久不做 Lite 自动发布」——适配器 in scope。  
> 未接线平台仍可返回 `PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1`；不得再用本 ADR 阻止立项。

## Context

六平台账号管理有价值，但小红书/抖音/视频号/快手自动发布成本与风控高，且会冲淡 Meta 双平台交付。

## Decision（历史 V1）

- xhs / douyin / wechat_video / kuaishou：**仅账号 + profile + 登录态**  
- 发布命令必须返回 `PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1`  
- Agent **不得**自行补这些平台的发布脚本  

## Consequences

### 正

- V1 范围可控  
- 可做采集等旁路能力（如 xhs collect）但不等于发布  

### 负 / 代价

- 用户可能误以为「六平台都能发」——需文档与错误码明确  

## Related

- Superseded for AgentPush publish path by: [0006](./0006-agentpush-full-scope.md)  
- Docs: `docs/PLATFORM_LITE.md`, `docs/PLAN.md`, `docs/agentpush/`  
- Code: `internal/platforms/platforms.go`, `internal/errors`
