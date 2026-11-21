# L016: 未压缩 Electron 包被误判为开发壳

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | electron |
| Severity | high |
| Date | 2026-07-24 |
| Last verified | 2026-07-24 |

## Symptoms

用户拿到的是 `http://127.0.0.1:17321` 本机控制地址，而不是可直接启动的桌面客户端。正式 `.app` 与开发壳同时存在时还会争抢单实例锁；正式包可能立即退出，或者显示依赖外部端口的开发页面。

## Wrong path

把本机 Go 控制面地址当成交付入口，并假设复制到 `Resources/app` 的未压缩 Electron 应用一定会令 `app.isPackaged` 为真。

## Root cause

当前本地打包脚本保留未压缩的 `Resources/app`，运行时 `app.isPackaged` 仍可能为假，导致正式 `.app` 走开发分支。开发壳与正式包又曾共用 Electron `userData`，因此共享同一个单实例锁。

## Verified correction

`electron/main.js` 使用应用包内嵌的 `Resources/alpha-nexus` 作为自包含桌面运行态的权威标记；仅开发壳使用独立的 `Alpha-Nexus-Dev` userData。重新打包后，`release/Alpha-Nexus.app` 可同时启动内嵌 Go sidecar，并在桌面窗口显示后端正常。

## Prevention

- 交付说明只指向 `.app`，不得把 loopback 控制地址描述为产品部署地址。
- Electron 打包验收必须同时检查桌面主进程、`Resources/alpha-nexus-egolite ui serve --port 0` 子进程和真实窗口。
- 开发壳与正式包必须使用不同的 Electron userData / 单实例锁。
- 自定义未压缩打包布局不得只依赖 `app.isPackaged` 判断运行模式。

## Related

- Evidence: `electron/backend-api.test.js`
- Evidence: `electron/package-macos.test.mjs`
- Code: `electron/main.js`
- Code: `electron/package-macos.mjs`
