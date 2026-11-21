# Current Project Memory

> 最后人工复核：2026-07-18。这里只记录当前可复用事实；源码和本轮真实测试证据始终优先。

| ID | Area | Active fact | Evidence | Last verified | Status |
|----|------|-------------|----------|---------------|--------|
| MEM-001 | docs | `AGENTS.md` 是唯一 Agent 入口；薄记忆在 `memory/evolution/`（CURRENT / mistakes / ADR），协议仪式已归档 | `AGENTS.md`、`memory/README.md` | 2026-07-27 | active |
| MEM-002 | product | Agent 是所有工作区可打开的右侧 Drawer，不再是左侧一级页面；一级入口保留浏览器管理、自媒体、仪表盘、一键发布和设置 | `ui-web/src/components/Layout.tsx`、`ui-web/test/workspace-navigation.test.mjs` | 2026-07-23 | active |
| MEM-003 | product | 旧能力通过浏览器与发布组合工作区嵌入复用，不恢复独立一级入口 | `ui-web/src/components/BrowserWorkspaceView.tsx`、`ui-web/src/components/PublishWorkspaceView.tsx` | 2026-07-18 | active |
| MEM-004 | browser | 浏览器生命周期只走现有 `BrowserFacade` | `internal/browser/facade.go`、`internal/browser/facade_test.go` | 2026-07-18 | active |
| MEM-005 | browser | 每账号 profile 继续复用现有 lease、Supervisor 和 CDP 端口机制 | `internal/browser/lease.go`、`internal/browser/ports.go`、`internal/browser/supervisor.go` | 2026-07-18 | active |
| MEM-006 | publish | 一键发布默认 dry-run；关闭演练后仍要求用户逐次明确触发 | `ui-web/src/components/PublishWorkspaceView.tsx`、`e2e/tests/shell.spec.ts` | 2026-07-18 | active |
| MEM-007 | publish | 同一发布意图使用稳定幂等键，改变意图不能复用旧请求 | `internal/publish/intent.go`、`internal/tools/handlers.go` | 2026-07-18 | active |
| MEM-008 | publish | 发布目标必须通过账号与资产归属校验 | `internal/publishguard/guard.go`、`internal/publishguard/guard_test.go` | 2026-07-18 | active |
| MEM-009 | publish | 没有独立平台成功证据时不得记录为真实发布成功 | `internal/publish/runner.go`、`internal/publish/runner_test.go` | 2026-07-18 | active |
| MEM-010 | agent | Studio 继续使用固定豆包网关；全局 Agent Hub 默认使用隔离 Pi 运行时的 Alpha Agent，开发版固定 `@earendil-works/pi-coding-agent@0.84.2`（Node `>=22.19.0`）；其他受控本地 CLI 只由用户显式选择，不自动 fallback | `internal/agenthub/`、`internal/studio/agent.go`、`electron/package.json`、`electron/pnpm-lock.yaml`、`memory/evolution/adr/0014-global-agent-hub-local-cli.md`、`memory/evolution/adr/0023-alpha-agent-on-isolated-pi-runtime.md` | 2026-08-23 | active |
| MEM-016 | agent | 默认 Alpha/Pi 运行时保持 chat/RAG-only，不接收 broker endpoint、token 或应用可执行路径；只有显式声明 private MCP 能力的可选 provider 才能取得单次 broker 访问，发布等副作用仍受领域审批约束 | `internal/agenthub/providers.go`、`internal/agenthub/service.go`、`internal/ui/agent_hub_handlers.go` | 2026-08-23 | active |
| MEM-017 | account-data | 账号登录态以独立本机 Chromium Profile 为运行时真源；人工完整检测或显式停止浏览器会创建按日加密会话备份，保留 35 天；跨设备只允许用户口令加密的 `.anxsession` 显式迁移，导入后必须平台确认 | `internal/sessionbackup/`、`internal/ui/account_session_handlers.go`、ADR 0015 | 2026-07-23 | active |
| MEM-018 | account-data | 数据库登录标签、浏览器运行、平台实时认证和账号窗口置前是不同状态；成功置前原子记录 `accounts.last_focused_at` 与 `browser_events.focus_succeeded`，失败单独记录且不改登录态 | `internal/browser/supervisor.go`、`internal/db/account_login_observation.go`、`migrations/018_account_focus.sql` | 2026-07-24 | active |
| MEM-019 | diagnostics | 应用诊断复用 SQLite 事件真源与本机轮转 JSONL：所有 API 写操作/失败/慢请求、应用与桌面壳生命周期、Renderer 指纹、数据库健康都可关联；设置页只导出脱敏快照，保留最近 10 份 | `internal/observability/`、`internal/db/diagnostics.go`、`internal/ui/diagnostic_handlers.go`、`electron/diagnostics.js`、`docs/DIAGNOSTICS.md` | 2026-07-24 | active |
| MEM-020 | skill | Skill 库以 SQLite 为目录/启停真源、以本机私有 `skills/<id>/SKILL.md` 保存内容；Agent 仅解析显式 `/skill-id` 或 `@skill-id`，自动化 Skill 审核前不能启用，执行仍走 Registry/Facade | `internal/skilllib/`、`internal/ui/skill_library_handlers.go`、`ui-web/src/components/SkillLibraryView.tsx`、ADR 0016 | 2026-07-24 | active |
| MEM-021 | publish | 内容库 `planned_at` 只在用户显式准备发布时冻结为各目标 `publish_at` 并尝试填写平台原生时间；冻结和执行前均校验至少提前 15 分钟，不建立本地定时器或到点唤醒，控件未验证即人工接管 | `internal/publishplan/`、`internal/publishworker/worker.cjs`、ADR 0017 | 2026-07-24 | active |
| MEM-022 | browser | 统一 `meta` 登录容器必须同时拥有同名观察规则和选择器 Cookie 契约；只接受 Business Suite/Facebook 的 `c_user`、`xs`，Instagram 单站 `sessionid` 不能证明统一容器可用 | `internal/logincheck/profiles.go`、`internal/selectors/catalog.v1.json`、L018 | 2026-07-26 | active |
| MEM-023 | browser | 一秒 DOM 探针与已持久化认证状态冲突时，必须在同一 profile 操作租约内用完整观察复核并持久化；租约忙、复核失败或证据未知均不得翻转已验证状态 | `internal/ui/account_observer.go`、L019 | 2026-07-26 | active |
| MEM-011 | agent | Agent 不能替用户确认浏览器、剪辑执行或真实发布副作用 | `internal/agents/llm_tools.go`、`internal/studio/agent.go` | 2026-07-18 | active |
| MEM-012 | proxy | 代理只使用现有 SQLite 端点、账号绑定与设置内入口，不建第二套评分、轮换或路由 | `internal/ui/proxy_handlers.go`、`ui-web/src/components/ProxyPoolView.tsx` | 2026-07-18 | active |
| MEM-013 | reference | VCat 会员能力只作候选参考；Alpha 不新增会员、云端、计费或子账号基础设施 | `docs/archive/` 与产品硬约束；旧摘录在 `archive/ceremony/VCAT_MEMBERSHIP_REFERENCE.md` | 2026-07-27 | active |
| MEM-014 | publish | 同一次跨平台发布为每个平台独立校验并提交 schema 参数快照；不混用模板或未知字段，仍逐账号使用独立幂等键 | `ui-web/src/components/PublishView.tsx`、`e2e/tests/shell.spec.ts` | 2026-07-19 | active |
| MEM-015 | browser | 控制面正常退出只 Detach 并保留账号 Chromium/runtime；只有显式账号停止才关闭浏览器，生命周期原因使用结构化白名单字段记录 | `internal/ui/server.go`、`internal/browser/supervisor.go`、`internal/browser/facade_test.go` | 2026-07-22 | active |

## 当前不能据此声称

- 历史某次测试通过，不代表当前工作树已通过；必须重新执行。
- 有平台字段/schema，不代表已自动填写第三方页面。
- 有发布任务完成态，不代表平台已成功，除非存在符合契约的独立成功证据。
- VCat 文档里的会员接口、表和测试编号，不代表 VCat 已实现对应代码。
