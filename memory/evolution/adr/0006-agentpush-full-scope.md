# 0006. AgentPush 全量能力面在 Go 主线落地

| 字段 | 值 |
|------|-----|
| Status | accepted |
| Date | 2026-07-12 |
| Area | product-scope / publish / agents |

## Context

- ADR 0005 已决定：AgentPush 愿景落在 Alpha-Nexus **Go** 栈，不迁 Node monorepo。  
- ADR 0004 将 Lite 四平台（xhs/douyin/wechat_video/kuaishou）定为 V1 **不做自动发布**，以控制 Meta 双平台交付。  
- 包层已出现 `publish` / `media` / `queue` / `scheduler` / `agents` / `llm` / `trace`，与 PRD 全量能力面对齐的脚手架。  
- 产品需要明确：**完整 AgentPush 能力面（10 平台适配器、内容中心、队列排期、Agent、指标）是否允许在本仓推进**。

## Decision

**Accepted：在 Go + fingerprint-chromium + CLI + SQLite 主线上实现 AgentPush 全量能力面。**

1. **范围**：PRD 所述内容中心、账号/浏览器、发布状态机与队列、5 Agent、10 平台 Publisher（API 和/或 Browser）、本地 metrics/export/audit —— 均 **in scope**。  
2. **栈不变**：不采用 Playwright/Node monorepo 为主线；Browser Provider 真源仍是 `BrowserFacade` + fingerprint-chromium + 本机 CDP。  
3. **Lite 历史限制**：ADR 0004 视为 **V1 历史约束**；对 **AgentPush 发布适配器路径** 予以 supersede —— 允许实现 douyin/xhs/wechat_video/kuaishou 等 Browser 发布适配器。未接线平台可暂时继续返回 `PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1`，但不得再以 ADR 0004 阻止立项。  
4. **交付节奏**：以 `docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md` 为现行 Goal；禁止假装「真提交已完成」。  
5. **文档真源**：规范入口 → `AGENTS.md`；找代码 → `docs/MAP.md`；历史 Epic 对照 → `docs/archive/agentpush/`。

## Consequences

### 正

- 产品北星与仓库范围一致，避免「文档说全量、AGENTS 说不做」的永久分裂  
- 复用已验证 Facade / SQLite / CLI / publish 状态机  
- 10 平台可按适配器增量交付，不必一次重写  

### 负 / 代价

- 范围显著大于原 V1「仅 IG+FB 自动发布」；需严格 Goal 切片，防范围爆炸  
- Lite 自动发布风控与 DOM 脆弱性上升；必须保留 manual_takeover、截图、错误码  
- ADR 0004 读者需读本 ADR，避免按旧禁令误拦  

## Supersedes / Related

- **Partially supersedes** [0004](../archive/adr/0004-lite-platforms-no-publish.md)（AgentPush 发布路径）  
- Builds on [0005](./0005-agentpush-via-alpha-nexus-stack.md)  
- Docs（现行）：当前 Goal、`AGENTS.md`；归档：`docs/archive/agentpush/`  
- Code: `internal/{publish,platforms,media,queue,agents,llm,trace}`
