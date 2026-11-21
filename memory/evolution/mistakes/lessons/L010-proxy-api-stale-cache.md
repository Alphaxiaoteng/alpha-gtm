# L010: 动态代理 API 被库内旧 IP 挡住

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | proxy |
| Severity | medium |
| Date | 2026-06-25 |

## Symptoms

配置动态 IP API 后首次启动正常，第二次仍用过期 `proxy_server`，不走 API。

## Root cause

启动逻辑：`proxy_server != ""` 则跳过拉取，未区分「用户手写静态」与「上次 API 缓存」。

## Fix

`browser_profiles.is_proxy_custom`：`0` = 每次 API 取新；`1` = 沿用手动代理。

## Prevention

- 缓存字段与「用户意图」字段分离
- 设置页写清自动/手动模式

## Related

- Code: `internal/db/`, `internal/browser/`, `internal/config/proxy.go`
