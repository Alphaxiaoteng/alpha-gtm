# L005: Electron/UI 状态枚举与 Go Supervisor 不一致

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | electron |
| Severity | high |
| Date | 2026-06-15 |

## Symptoms

工作台 Tab 空白、实例列表永远空、badge 为 0；前端过滤 `state === 'running'` 无匹配。

## Root cause

1. preload / 类型声明缺失导致静默失败  
2. Electron 路径跳过 `api.startBrowser()`，Supervisor 无注册  
3. Go 状态为 `ready|starting|degraded|crashed|stopped`，前端写死 `running`

## Fix

补类型与 preload 路径；Electron 也调 API 注册；UI 状态标签对齐 Go 枚举。

## Prevention

- 新增状态值先改 Go 再改 TS 联合类型
- 禁止魔法字符串散落；集中常量或 map 中文标签

## Related

- Code: `internal/browser/supervisor.go`, `ui-web/src/store.ts`, `electron/`
