# L014: 旧工作副本关闭共享浏览器运行态

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | browser |
| Severity | high |
| Date | 2026-07-22 |
| Last verified | 2026-07-22 |

## Symptoms

SQLite 中六个账号仍存在，但所有 Chromium 窗口在同一轮监测中消失；网页随后把账号显示为浏览器已退出或平台已登出。重新启动页面后，同一批端口又可能被旧进程接管。

## Wrong path

只检查页面 URL 和数据库记录，没有核对实际监听 `17321/5176` 的进程工作目录。另一个旧工作副本和 Electron 壳仍在后台运行，并与当前开发副本共享同一个 `~/.alpha-nexus`，看起来像当前代码没有生效。

## Root cause

旧工作副本的服务关闭路径仍调用 `BrowserFacade.Shutdown()`，会终止它管理的 Chromium；当前副本虽已改成 `Detach()`，却从未真正占用服务端口。旧日志又把“控制面请求关闭”和“重启后发现 PID 已消失”都写成无原因的 `process_exited`，无法事后唯一归因。

## Verified correction

- 停止旧工作副本的 Electron、后端和重复 Vite，只让当前工作区占用 `17321/5176`。
- `Server.Close()` 只调用 `BrowserFacade.Detach()`；显式账号停止仍是唯一正常关闭浏览器的 UI 路径。
- `browser_events` 增加白名单 `reason_code`、`exit_code`、`term_signal`；区分安全分离、控制面关闭、正常退出、非零退出、信号退出和重启后发现进程缺失。API 永不返回自由格式 `message`。
- 真实启动六个 Chromium 后重启后端：六个 PID 与 `15101–15106` 全部保留，新后端恢复为六个 ready 实例，重复启动返回 `ALREADY_RUNNING`。

## Prevention

- 调试账号丢失时先核对 `17321/5176` 监听进程的 cwd，再看 data root、runtime 和平台登录证据。
- 同一 data root 同一时间只运行一个 Alpha-Nexus 控制面版本；停用旧 Electron/开发任务后再切换工作区。
- 服务退出必须产生 `SUPERVISOR_DETACHED`，且浏览器 PID/端口继续存在；出现 `SUPERVISOR_SHUTDOWN` 才表示控制面主动要求终止。
- 旧事件没有结构化原因时只报告“未验证”，不得从空 message 编造 crash 或平台掉线原因。

## Related

- Evidence: `internal/browser/facade_test.go`
- Evidence: `internal/browser/supervisor_test.go`
- Evidence: `internal/db/account_login_observation_test.go`
- Evidence: `internal/ui/account_observer_test.go`
- Code: `internal/ui/server.go`
- Code: `internal/browser/supervisor.go`
- Code: `internal/db/account_login_observation.go`
