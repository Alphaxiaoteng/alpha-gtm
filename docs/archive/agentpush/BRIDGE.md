# AgentPush ↔ Alpha-Nexus 桥接

## 结论

| 维度 | AgentPush 文档 | Alpha-Nexus 现状 | V1 落地策略 |
|------|----------------|------------------|-------------|
| 产品形态 | Electron Desktop 为主 | CLI-first + 本地 UI/Electron 壳 | **保留 CLI 核心**；Desktop 继续作操作台 |
| 语言栈 | Node TS monorepo + Python AI | Go + chromedp | **不重写**；Go 实现 Adapter/状态机 |
| 浏览器 | BrowserOS / Cloak / Chrome + Playwright | fingerprint-chromium + CDP + chromedp | Provider 思想保留；**主 Provider = fingerprint-chromium** |
| 发布策略 | API-first，Browser fallback | 2 Meta best-effort + 17 manual + 3 API dry-run-only | readiness 必须先于任务创建；无证据不自动成功 |
| 平台 | 原 PRD 10 平台 | 23 项目录、22 个发布代码 | 逐平台 Native 化，`custom` unavailable |
| AI Agent | Content/Adapt/Publish/… | Registry/Agent/本地模板回落已接线 | 外发模型必须由用户在当次操作明确允许 |
| 数据目录 | `~/Library/.../AgentPush` | `~/.alpha-nexus` | 保持 AN；可加配置别名 |

**原则：** AgentPush PRD 定义「要去哪」；Alpha-Nexus 定义「现在怎么走」；禁止为对齐文档而拆掉已验证的 Facade/SQLite/CLI。

---

## 能力映射

| AgentPush 概念 | Alpha-Nexus 落点 |
|----------------|------------------|
| BrowserProfileProvider | `internal/browser.BrowserFacade` |
| Profile lock / lease | `LeaseManager` + flock |
| PublishTask 状态机 | `internal/publish` + `publish_jobs` |
| PlatformAdapter | `internal/platforms` + meta adapter |
| dryRun | `publish --dry-run` |
| manual_takeover | job status + UI login-handoff |
| Queue / batch | `batch_runs` / `batch_items` |
| Screenshot / DOM | `job_events` + screenshots 目录 |
| 内容中心 | `contents` + `media_assets`（渐进增强） |
| Agent Tool Contract | `internal/tools` + MCP/HTTP adapters；未实现项返回 `TOOL_NOT_IMPLEMENTED` |

---

## 分期（从文档 Epic 压缩）

| Phase | 对应 AgentPush Epic | Alpha-Nexus 交付 |
|-------|---------------------|------------------|
| G1 | EPIC-400 状态机 + EPIC-600 子集 | 已完成：publish 状态机、Adapter 接口、Meta dry-run/接管 |
| G2 | EPIC-300/600 Meta 浏览器真发 | 代码侧 best-effort 完成；真实测试账号重复验收未完成 |
| G3 | EPIC-200 内容增强 | 内容中心、素材索引、SHA256、预检和模板已完成 |
| G4 | EPIC-600 多平台 | 22 个 dry-run 契约完成；17 个人工接管，Native 逐平台取证 |
| G5 | EPIC-500/700 | Agent、tools、metrics、通知与本地控制面已接线，生产外发仍需授权 |

所有阶段均禁止本地后台定时器、到点唤醒、周期采集和任意 webhook 假成功；用户当次显式准备可填写平台原生发布时间。

---

## 明确不做（本仓短期）

- 整仓迁到 pnpm monorepo + Playwright 主线  
- 验证码破解 / MFA 绕过  
- 云托管 Cookie  
- 32 周并行 6 Squad 编制（单人/Agent 按 Goal 串行）

---

## 文档维护

- AgentPush 原文只增不改业务事实；策略变更写本 BRIDGE 或 evolution ADR  
- 新架构决策 → `memory/evolution/adr/`
- 踩坑 → `memory/evolution/mistakes/lessons/`
