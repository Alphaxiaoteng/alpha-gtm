# L002: CDP 端口 TIME_WAIT 假空闲

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | browser |
| Severity | medium |
| Date | 2026-06-01 |

## Symptoms

进程刚退出后立刻再分配同一 CDP 端口，绑定失败或行为诡异；用 `DialTimeout` 检测却显示「空闲」。

## Root cause

TCP 端口处于 `TIME_WAIT` 时，客户端 Dial 可能失败/成功不稳定，**不等于**本机可 Listen 绑定。

## Fix

`isTCPFree()`（或等价）用 `net.Listen` 尝试绑定；失败即视为不可用。PortAllocator 以 SQLite + 真实绑定为准。

## Prevention

- 禁止仅用 Dial 判断端口空闲
- 端口池 15101–15199；分配后写 DB

## Related

- Code: `internal/browser/ports.go`
