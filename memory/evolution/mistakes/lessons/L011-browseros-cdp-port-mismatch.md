# L011: BrowserOS 代理端与 CDP 端口不匹配导致侧边栏崩溃

| 字段 | 值 |
|------|-----|
| Status | `mitigated` |
| Area | `browser` |
| Severity | `high` |
| Date | 2026-07-15 |
| Last verified | 2026-07-18 |

## Symptoms

- 用户点击 BrowserOS 的 "Assistant"（小狗侧边栏）无反应，控制台/网页提示 `"BrowserOS Agent is installing/updating. Please try again shortly."`
- `browseros-server.log` 日志中出现 CDP 尝试连接 `9100` 端口 3 次失败后崩溃退出的错误记录。
- 启动浏览器极其缓慢（耗时约 15 秒），原因为后台逻辑卡在等待 CDP 连接就绪直到超时。

## Root cause

BrowserOS 启动时会通过其主进程拉起本地 AI 代理服务（`browseros_server`）。该服务器默认会连接 `9100` 端口的 Chrome DevTools Protocol (CDP)。
然而，Alpha-Nexus 后台服务为了支持多开和隔离，会为每个浏览器实例动态分配 CDP 端口（如 `15101`）。主程序虽然以 `15101` 启动了 Chromium 内核，但没有将这个自定义端口告知 `browseros_server` 子进程。连接断开导致 AI 代理服务退出崩溃，使得侧边栏小狗助手瘫痪。

## Fix

- `internal/browser/supervisor.go` 的 `args()` 在 `EngineBrowserOS` 下传递当前实现使用的端口参数：
  - `--browseros-cdp-port=<port>`
  - `--browseros-server-port=<port+1000>`
  - `--browseros-extension-port=<port+2000>`
  - `--browseros-proxy-port=<port-100>`
- `Open()` 将这些端口写入当前 profile 内的 `.browseros/server_config.json`。
- 浏览器进程环境会删除名称含 `KEY`、`TOKEN`、`SECRET`、`PASSWORD`、`CREDENTIAL` 的变量；**不得**把 Alpha 的模型密钥传给 BrowserOS 或任何浏览器子进程。

当前本机 BrowserOS bundle 尚未完成真实 CDP 就绪验收，因此本条状态是 `mitigated`，不能据此声称 BrowserOS Assistant 已可用。

## Prevention

- 定制浏览器的控制流端口必须在命令行、profile 配置和非敏感环境变量之间保持一致，并用隔离 profile 做 CDP readiness 测试。
- 未经官方契约或真实 fixture 验证的参数只能标为候选；禁止把猜测或侧边栏 UI 出现当成功证据。
- 浏览器进程的凭证过滤必须有回归测试；任何新增环境变量先证明不含密钥。

## Related

- Code: `internal/browser/supervisor.go`
- Test: `internal/browser/supervisor_security_test.go`
