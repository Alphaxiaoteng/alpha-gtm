# 0001. CLI 是唯一核心接口

| 字段 | 值 |
|------|-----|
| Status | accepted |
| Date | 2026-05-01 |
| Area | cli |

## Context

产品需要同时服务人类操作台与 AI/Agent 调度，若 UI 与 CLI 各写一套业务，状态分叉且无法批量化。

## Decision

- **CLI**（Cobra）是唯一核心业务接口  
- 本地 UI 只复用 core/API，**不实现独立发布逻辑**  
- Agent 必须通过 CLI（或等价调用同一 core）执行任务  

## Consequences

### 正

- 批量化、NDJSON、脚本化天然一致  
- UI 变薄，可替换壳（Electron/Wails）  

### 负 / 代价

- UI 不能「图省事」直接写 chromedp 发帖  
- 所有能力需先有 CLI 契约  

## Alternatives

| 方案 | 为何未选 |
|------|----------|
| UI 为中心 + 后台 API | 易做成 Web 管理后台，偏离 V1 |
| 仅 TUI | 账号接管与可视化不足 |

## Related

- Docs: `AGENTS.md`, `docs/CLI.md`；旧 V1 PLAN：`docs/archive/misc/PLAN.md`
