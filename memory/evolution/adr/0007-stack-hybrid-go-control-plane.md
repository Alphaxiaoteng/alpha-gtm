# 0007. 技术栈：Go 控制面为主，不整仓 Node 重写

| 字段 | 值 |
|------|-----|
| Status | accepted |
| Date | 2026-07-12 |
| Area | architecture |

## Context

AgentPush 文档倾向 Node monorepo + Playwright；仓库已有 Go CLI + chromedp + fingerprint-chromium + SQLite + React/Electron。  
两 Agent 三轮对抗：Go 营强调控制面/隔离/沉没成本；Node 营强调选择器 DX 与 API SDK。

## Decision

**采用混合但以 Go 为唯一执行真源（短期可交付最优）：**

| 层 | 选型 |
|----|------|
| 控制面 | **Go CLI + SQLite + publish 状态机 + batch** |
| 浏览器隔离 | **fingerprint-chromium + 本机 CDP + BrowserFacade** |
| 页面自动化 | **chromedp**（Meta/国内浏览器路径） |
| API 平台 | 凭证引用与 dry-run 边界；YT/TikTok/X 原生 Connector 尚未实现 |
| UI / E2E | 现有 React + Electron；**Playwright 仅 e2e/** |
| AI | `internal/llm` + agents；可选外置 worker |
| 未来扩展 | Playwright **可作为** Facade 后的可选 Provider，**不得**分叉第二套 job 真源 |

**明确否决：** 全量迁 pnpm monorepo / 以 Playwright 替换 Facade 为唯一主线（在现有人力与进度下）。

## Consequences

### 正

- 复用 G1/G2、lease/port、已测 smoke  
- 单二进制、CLI-first 不变  
- 对抗结论与 ADR 0005/0006 兼容  

### 负

- 复杂 DOM 自动化 chromedp 成本高于 Playwright  
- 官方 Node SDK 需 Go 封装或 webhook 桥  

### 升级闸门

若连续 2 个平台真发因 chromedp 阻塞 >2 周，可开 **Playwright worker 试点**（仍由 Go 发 job / 收 NDJSON），再立 ADR 修订本条。

> 2026-07-26：YouTube/TikTok 的受控浏览器提交路径由 [ADR 0022](./0022-controlled-browser-submit-youtube-tiktok.md) 接续；原生 API Connector 仍未实现，X 仍只有 API dry-run。Go/SQLite/BrowserFacade 继续是唯一控制面与生命周期真源。

## Related

- 对抗记录：会话 2026-07-12 Debater-A/B  
- ADR 0005、0006  
