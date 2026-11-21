# 0009. Electron 监管动态 Go Sidecar 与统一可观测性

| 字段 | 值 |
|------|-----|
| Status | proposed |
| Date | 2026-07-16 |
| Area | runtime, security, observability |

## Context

Turbo Push 的实时架构已验证为桌面壳监管两个本地 Go Sidecar，以权限受限的发现文件共享随机端口和认证信息；主服务可独立更新。其优点是桌面职责与业务服务解耦，但日志分散在文本、panic、JSON 通知和 macOS Unified Log，缺少稳定任务关联、轮转与业务审计真值。

实施前，Alpha-Nexus 已有 Go `ui serve`、Electron、`tools.Registry`、BrowserFacade、publish_jobs/job_events、audit_events、browser_events 和 task_queue，但存在：

- Electron 生产态固定端口 17321，健康检查和 API 没有独立进程认证；
- app_user 为空时 API 完全关闭认证，并兼容 query token；
- Electron 丢弃 Go stdout/stderr，Go 没有统一 `slog`、脱敏和轮转；
- trace 只在进程内记录，job/browser/audit 事件字段和错误处理不完整；
- Publish Runner、Queue 和 Browser Supervisor 无法组成可恢复、可追踪的统一生命周期。

2026-07-16 的基础实现已消除前四项：Electron Sidecar 使用动态 loopback descriptor 和 transport token；Registry 有生产 JSONL emitter；Runner 用 SQLite typed job event 保存业务真值，并使用原子 Claim 防止同一 job 并发执行。Queue lease/crash reclaim、HTTP middleware 与 Browser Supervisor 的完整接线仍是后续工作，不能据此宣称完整异步控制面已经完成。

知识库不在本次集成范围。Turbo Push 的密文、Cookie、设备私钥、浏览器 Profile 和闭源选择器配置不作为 Alpha 运行依赖。

## Decision

### Runtime

1. 生产桌面版运行一个 Alpha Go 业务 Sidecar；不新增知识库 Sidecar。
2. Electron Main 是进程监管者：启动、握手、健康检查、优雅停止、限时 KILL、受限重启和版本回滚。
3. Go 监听 `127.0.0.1:0`。启动后原子写 `run/backend.json`，目录 `0700`、文件 `0600`；descriptor 含 schema_version、pid、port、started_at、version 和随机 transport auth。
4. transport auth 不进入 argv、query、renderer localStorage 或日志。Electron Main 只在内存持有，通过受限 IPC 代理 renderer 请求。
5. `GET /healthz` 仅暴露最小 ready/version；所有 `/api/v1/*` 强制 transport auth。用户 JWT/RBAC 是第二层身份，不能替代 Sidecar 认证。
6. Go 使用 `http.Server.Shutdown`，停止接单后释放 lease、持久化终态并 flush 事件。Electron 等待最多 5 秒再强制终止。
7. 意外退出采用有上限指数退避；超过预算进入 degraded，不无限拉起。

### API and jobs

1. HTTP、MCP、CLI 和 UI 共用 `tools.Registry`、BrowserFacade 和同一领域服务。
2. 发布与登录操作可建模为持久异步任务。创建发布返回 `202 + job_id`，必须提供 Idempotency-Key；不提供定时发布。
3. Queue Claim 使用短事务与 lease/heartbeat/crash reclaim；网络和浏览器操作不在事务内。
4. 状态与事件先写 SQLite，再通过 SSE 广播。SSE 支持 `Last-Event-ID`，断线不改变任务真值。
5. `published` 只在取得明确平台提交证据后成立；未知结果进入 manual_takeover/awaiting_review。

### Observability

1. 新建 `internal/observability`，使用标准库 `log/slog` JSON Handler、context correlation、字段白名单 sanitizer 和统一 Emitter。
2. `job_events` 是任务生命周期真值，`audit_events` 是安全/配置/工具操作审计，notifications 是用户动作面，JSONL 只用于诊断。
3. Emitter 首批接入 `tools.Registry.Call`、HTTP middleware、Publish Runner、Queue Worker 和 Browser Supervisor。
4. JSONL 文件 `0600`，10 MB × 5，保留 14 天；crash 文件保留 30 天。审计默认保留 90 天。
5. 日志允许字段：时间、级别、服务/组件/事件、Schema 版本、trace/request/task/job/record/batch ID、平台、不可逆 account_ref、尝试、步骤、状态、稳定错误分类、耗时、HTTP 状态和版本/pid。
6. 禁止日志记录 Cookie、Authorization、密码、API Key、代理凭证、URL query/fragment、内容标题/正文、通知 body、DOM 原文、完整指纹和绝对媒体路径。
7. 旧 NDJSON 变为相同 typed Event 的 stdout sink；Electron 停止输出代理/指纹配置并接管后端输出。

### Implementation status

已验证的运行时基础：descriptor 目录/文件权限、原子写入、PID/nonce health、header-only transport auth、query token 拒绝、有限 restart、TERM→KILL、Go Shutdown、Registry 一次 terminal event、0600 有界 JSONL、trace/correlation 合并、job event 结构校验、旧库关系触发器与发布 Claim。

仍未完成的决策条款：HTTP 发布 API 的强制 Idempotency-Key、Worker lease heartbeat/crash reclaim、HTTP/Queue/Browser 的全链路 Emitter、前端任务时间线接线、日志保留清理任务及 Sidecar 签名更新/回滚。CLI、tools 与队列入口已支持或自动生成稳定 key；`/api/v1/events` 已提供 SQLite durable replay + `Last-Event-ID`。其余能力保持在 Phase 2/5，不在本次基础验收范围内。

## Consequences

### 正

- 端口抢占和未认证本地调用风险显著降低。
- 桌面、Web、MCP、CLI 不再形成多套业务实现。
- Sidecar、任务、浏览器和工具调用可用同一 trace 复盘。
- 进程或 SSE 重启不丢任务，诊断日志删除不破坏审计真值。
- 后续签名 Sidecar/Selector 更新有稳定生命周期边界。

### 负 / 代价

- Electron IPC、运行发现文件和两层认证增加实现与测试成本。
- 现有 HTTP 路由、renderer API、Schema 和测试需要分阶段迁移。
- 事件字段与 retention 成为兼容契约，需要迁移和版本管理。
- 文件轮转、crash reclaim、SSE replay 和 updater rollback 都需要故障注入测试。

## Alternatives

| 方案 | 为何未选 |
|------|----------|
| 保留固定 17321 且只依赖 loopback | 同用户进程可抢占/调用，启用用户认证后 Electron 又缺 Bearer，无法形成可靠边界 |
| 把 Go 逻辑移入 Electron/Node | 与既定 Go 控制面冲突，复制 Registry、DB 和浏览器生命周期 |
| 每个能力拆独立 Sidecar | 当前规模增加进程、认证、更新和日志复杂度；知识库也已明确排除 |
| 只写文本日志，不持久化事件 | 无法恢复任务、断线续传或证明发布状态 |
| 直接复刻 Turbo REST/query auth | 非版本化、Token 易进入历史/日志，且缺幂等/lease 契约 |

## Verification Gates

- 动态端口、陈旧 descriptor、端口抢占、无/错 Token、query token 的集成测试。
- SIGTERM 优雅关闭、5 秒强停、崩溃预算与上一版本回滚测试。
- sanitizer 敏感字段负面测试；日志/descriptor 权限与轮转测试。
- trace 跨 HTTP → Registry → Queue → Runner → Browser 的传播测试。
- Idempotency、lease 超时回收、状态顺序、事件先持久化和 SSE replay 测试。
- 运行时 Schema 与 migration 一致性、FK 和索引测试。

## Review Date

Phase 1 Go Sidecar 与统一可观测性验收后，将本 ADR 置为 accepted 或记录替代决策。

## Related

- [0003. BrowserFacade 为生命周期真源](./0003-browser-facade-not-manager.md)
- [0007. 栈决策：Go 控制面](./0007-stack-hybrid-go-control-plane.md)
- [0008. Turbo Push 能力采用混合绞杀集成](./0008-turbopush-capability-integration.md)
- [Turbo Push 全功能解剖](../../../../docs/archive/agentpush/TURBOPUSH_DEEP_DIVE.md)
