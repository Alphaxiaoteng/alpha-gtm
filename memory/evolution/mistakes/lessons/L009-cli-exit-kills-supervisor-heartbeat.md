# L009: CLI 退出后 Supervisor 心跳停止

| 字段 | 值 |
|------|-----|
| Status | mitigated |
| Area | browser |
| Severity | medium |
| Date | 2026-06-01 |

## Symptoms

`browser start` 非阻塞返回后，浏览器进程仍在（独立进程组），但租约续租/heartbeat 停，下次命令靠 cleanStale 收尸。

## Root cause

Supervisor 跑在 CLI 进程内；进程退出 → goroutine 全灭。浏览器用 Setpgid 存活。

## Fix（现状）

- 接受 V1 模型：长驻依赖 `ui serve` / Electron 托管 Supervisor
- 下次启动 cleanStaleInstances 重建 runtime

## Prevention

- 文档写清：需要稳定 heartbeat → 开 UI/桌面壳，不要只依赖短 CLI
- 未来可选 daemon（roadmap，未做）

## Related

- Code: `internal/browser/supervisor.go`
