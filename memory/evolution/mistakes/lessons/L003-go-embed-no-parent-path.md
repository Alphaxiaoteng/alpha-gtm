# L003: go:embed 不能引用父目录 dist

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | build |
| Severity | medium |
| Date | 2026-06-01 |

## Symptoms

`go:embed ../ui-web/dist` 编译失败：`invalid pattern: contains '..'`。

## Root cause

Go embed 模式禁止 `..`，资源必须在包目录树内。

## Fix

`build.sh`：`ui-web` production build → 拷贝到 `internal/ui/dist` → `//go:embed all:dist`。

## Prevention

- 改前端打包路径时同步 `build.sh` 与 `internal/ui/assets.go`
- 不要在 Go 源码里写跨包相对 embed

## Related

- Code: `build.sh`, `internal/ui/assets.go`
