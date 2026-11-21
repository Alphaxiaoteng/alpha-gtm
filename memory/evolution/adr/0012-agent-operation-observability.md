# 0012. Agent 操作使用手动刷新的语义事件看板

| 字段 | 值 |
|------|-----|
| Status | accepted |
| Date | 2026-07-17 |
| Area | agent, observability, ui |

## 决定

- Tool Registry 在每次统一工具调用开始、完成或失败时，持久化 allowlist 操作事件。
- 事件只含 trace、任务、账号、平台、工具步骤、状态、耗时和稳定错误码；不保存参数、完整 URL、CDP 原始包、Cookie、凭证、profile 路径或截图。
- UI 只在页面加载、用户点击刷新或当前动作完成后读取本地只读 API；不使用定时轮询、SSE 或后台刷新。
- 截图/视频证据不自动采集。若后续实现，必须由用户显式请求、脱敏、限频且可审计。

## 后果

用户可以看到 Agent 实际经 Registry 发起的操作，而不是误把 CDP 端口或协议流当成业务步骤。直接绕过 Registry 的 CDP 操作不会出现在此看板，后续新增浏览器动作必须接入 Registry 和 profile operation lease。

## Related

- Code: `ui-web/src/components/OperationActivityPanel.tsx`
- Test: `ui-web/test/no-background-polling.test.mjs`
