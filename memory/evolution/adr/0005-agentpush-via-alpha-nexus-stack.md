# 0005. AgentPush 愿景落在 Alpha-Nexus Go 主线

| 字段 | 值 |
|------|-----|
| Status | accepted |
| Date | 2026-07-12 |
| Area | publish |

## Context

仓库收到完整 AgentPush PRD（Electron monorepo、Playwright、10 平台、LLM Agent、32 周计划）。与现有 Alpha-Nexus（Go、chromedp、fingerprint-chromium、CLI-first）冲突。

## Decision

- AgentPush 产品愿景落在本仓 Go 主线；原文 PRD/TECH 已归档  
- **不**整仓迁到 Node/Playwright monorepo  
- 用 Go 实现 AgentPush 的状态机 / Adapter 契约 / 发布管道  
- 现行 Goal：`docs/agentpush/GOAL_ALPHA_GAMER_LAUNCH.md`  
- 扩大平台/AI 前必须改 `AGENTS.md`  

## Consequences

### 正

- 保留已验证 Facade/SQLite/CLI  
- 可渐进交付，避免 32 周空转  

### 负 / 代价

- 与 AgentPush 原文技术栈表不一致（原文已归档）  
- Desktop 体验与「纯 Electron 产品」有差距  

## Related

- Docs（现行）：当前 Goal、`AGENTS.md`  
- Docs（归档）：`docs/archive/agentpush/`
