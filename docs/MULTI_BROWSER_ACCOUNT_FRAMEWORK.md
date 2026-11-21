# 多浏览器 · 多账号管理框架

**状态**: 规划冻结（superpowers 三阶段完成）；**W1 已落地** — `BrowserFacade` 为 CLI/UI/batch **唯一**浏览器生命周期入口  
**详细制品**: `docs/superpowers/multi-browser-account/`  
**集成记录**: `docs/superpowers/multi-browser-account/INTEGRATION_NOTES.md`

## 一句话

Electron 是**本地操作台桌面壳**；Go `BrowserFacade`（内聚 `Supervisor + Lease + PortAllocator`）是**多浏览器实例**真源；每个账号一个 **fingerprint-chromium profile**；CLI/UI/batch **必须**走同一 Facade，禁止第二套启动路径。

## 角色分工

| 层 | 路径 | 职责 |
|----|------|------|
| 桌面壳 | `electron/` | 单实例、加载 UI、选浏览器二进制、Finder；可选 spawn `ui serve` |
| 操作台 UI | `ui-web/` + `internal/ui` | 账号/批量/设置；触发 start/stop/focus；展示 runtime |
| 核心入口 | `internal/browser/facade.go` | **对外 API 唯一入口**（见下表） |
| 核心实现 | `internal/browser` Supervisor/Lease/Ports | 多实例、端口 15101–15600（500 槽位）、lease 互斥、探活回收 |
| 入口 | CLI `alpha-nexus` | Agent/脚本唯一自动化入口（应对齐 Facade） |
| 数据 | SQLite + profile 目录 | accounts、browser_profiles、runtime、leases |

## 数据模型（逻辑）

```text
Account 1 ──1:1──► BrowserProfile
                      │ user-data-dir
                      │ fingerprint_seed / proxy / lang / tz
                      │ last_cdp_port (提示，非锁)
                      │
                      ▼ (运行时 0..1)
                 RuntimeInstance
                      │ pid, cdp_port, state
                      │
                 ProfileLease (0..1 有效)
                      │ holder: ui | cli | job | batch
```

表名：`browser_profiles`、`browser_runtime_instances`、`profile_leases`（及 `browser_events`）。  
Schema：`migrations/002_browser_runtime.sql` + `internal/db` ensure-schema；见 `docs/SCHEMA.md`。

## 状态机（实例）

`starting → ready ⇄ busy → degraded|crashed → stopped`

- `busy`: 持有 job/batch lease 的自动化
- `ready`: 可人工接管或接任务
- 探活失败 → `crashed` → **`BrowserFacade.Reconcile()`**（内部 `Supervisor.Reconcile` / heartbeat）释放端口与 lease

## Electron 与自动化边界

```text
✅ Electron: 窗口壳、路径选择、打开目录、加载 http://127.0.0.1:17321
❌ Electron: 发布点击流、chromedp、作为账号 profile 真源
⚠️  webview partition IPC: legacy / 非 V1 账号真源
```

## 端口与安全

- CDP: `127.0.0.1:15101–15600`，共 500 个槽位（`PortAllocator`）
- UI: `127.0.0.1:17321`（dev Vite `:5173`）
- 禁止 CDP/`ui serve` 绑定公网

## Facade API（对外入口 · `internal/browser/facade.go`）

构造：`NewFacade` / `NewFacadeFromConfig`。

| 方法 | 职责 |
|------|------|
| `StartAccount(accountID, StartOptions)` | LaunchConfig → 并发校验 → 启实例 → 可选 Focus |
| `StopAccount(accountID)` | 停实例；未运行/重复 stop **幂等** |
| `Status(accountID)` | 单账号运行态快照 |
| `ListRunning()` | 本进程运行中实例列表 |
| `Focus(accountID)` | 窗口置前 |
| `AcquireLease` / `ReleaseLease` | profile 租约 |
| `LaunchConfigFromProfile(accountID)` | 从 DB 复原指纹/代理/login_url 等 |
| `Reconcile()` | 死 PID / 僵尸端口 / 过期 lease |
| `SetMaxConcurrentBrowsers` / `MaxConcurrentBrowsers` | 并发上限（超限拒绝，不排队） |

**废弃路径**（勿新增调用）：直接 `Supervisor.Open` / `BrowserManager.Start`。高级能力（如 `ArrangeWindows`）可经 `Facade.Supervisor()`，业务启停仍须走 Facade。

## 实现入口（代码现状）

| 组件 | 文件 | 备注 |
|------|------|------|
| **Facade（对外）** | `internal/browser/facade.go` | **API 入口以本文件为准** |
| 多实例调度 | `internal/browser/supervisor.go` | Facade 内部；`Open` 标 deprecated for external |
| 端口分配 | `internal/browser/ports.go` | `PortMin=15101` `PortMax=15600` |
| 租约 | `internal/browser/lease.go` | SQLite `profile_leases` |
| 进程锁 | `internal/browser/lock.go` | flock 辅助 |
| Focus | `internal/browser/focus_darwin.go` 等 | 平台分支 |
| 单实例旧路径 | `internal/browser/browser.go` | `BrowserManager`；废弃，应收拢调用方到 Facade |
| 错误码 | `internal/errors` | `PROFILE_LOCKED` / `NO_PORT*` / `BROWSER_CRASH` / `CDP_CONNECT` / `LEASE_EXPIRED` 等 |
| Electron 壳 | `electron/main.js` | 桌面能力 + legacy partition/webview 标注 |
| 开发一键 | `run_electron.sh` | |
| CLI browser | `internal/cli/browser_commands.go` | 泳道 B：应对齐 Facade 子命令/NDJSON |
| Reconcile smoke | `scripts/browser-reconcile-smoke.sh` | 泳道 E |

## 执行计划

见：

1. `docs/superpowers/multi-browser-account/brainstorm.md`
2. `docs/superpowers/multi-browser-account/tasks.md`
3. `docs/superpowers/multi-browser-account/audit.md`
4. `docs/superpowers/multi-browser-account/MULTI_AGENT_SPECS.md` ← **多 Agent 开干用这个**
5. `docs/superpowers/multi-browser-account/INTEGRATION_NOTES.md` ← 集成验收记录

## 波次摘要

1. **W0**: schema + Electron 生命周期 + 文档指针  
2. **W1**: BrowserFacade ✅（`facade.go`）  
3. **W2**: Reconcile / LaunchConfig / CLI / HTTP（CLI·HTTP 待 B/C）  
4. **W3**: 并发、Focus、Accounts UI、添加向导  
5. **W4**: 批量检查、安全、打包、集成验收  
