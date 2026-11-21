# L001: BrowserManager 与 Facade/Supervisor 双路径并存

| 字段 | 值 |
|------|-----|
| Status | mitigated |
| Area | browser |
| Severity | high |
| Date | 2026-06-01 |

## Symptoms

旧 `browser start` 阻塞单实例；新路径非阻塞多开。两套同时操作同一 profile 时端口冲突、状态不一致。

## Root cause

演进过程中保留 legacy `BrowserManager`，同时引入 Supervisor + Facade，命令面未完全收敛。

## Fix

- 生命周期真源定为 **`BrowserFacade`**（Supervisor + Lease + PortAllocator）
- CLI canonical：`browser start|stop|status|list|focus` 走 Facade
- legacy 仅兼容，新代码禁止旁路 `exec` 或直接 `BrowserManager`

## Prevention

- 改浏览器前读 `docs/CLI.md` 与 ADR 0003
- UI `/api/browser/*` 必须经 Supervisor，禁止自建 process map
- Code review：搜 `exec.Command` 起浏览器 = 红旗

## Related

- Code: `internal/browser/facade.go`, `supervisor.go`, `browser.go`
- ADR: [0003](../../adr/0003-browser-facade-not-manager.md)
