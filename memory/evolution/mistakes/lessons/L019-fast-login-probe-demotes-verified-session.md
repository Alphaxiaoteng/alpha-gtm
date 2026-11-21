# L019: 快速登录探针未经复核降级已验证会话

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | browser |
| Severity | high |
| Date | 2026-07-26 |
| Last verified | 2026-07-26 |

## Symptoms

已验证登录的账号在平台弹窗、页面切换或短暂 DOM 变化时突然变成 `logged_out`；Meta Business Suite 因表单和资产切换较多而更容易触发。

## Root cause

一秒快速探针只读取 DOM，但状态变化未经完整 `ObservePassive` 复核就直接写入 SQLite。探针完成后又先释放 profile 租约、稍后才持久化，旧的负面结果可能与人工完整检测竞态。

## Verified correction

- 已持久化的 `logged_in` / `logged_out` 与快速探针结果冲突时，先在同一 profile 租约内执行完整观察。
- 租约保持到确认结果完成持久化后再释放；租约忙、完整观察失败或证据未知时保留上次已验证状态。
- Meta 登录 DOM 从任意 `form` 收窄为包含明确 email/username 输入框的登录表单。

## Prevention

- 低成本轮询只能直接记录未知/诊断证据，不能凭单次易变 DOM 翻转已验证认证状态。
- 状态复核与写入必须共享同一个 profile 操作租约。
- 平台登录选择器禁止使用无身份语义的通用表单或页面文本。

## Related

- Code: `internal/ui/account_observer.go`
- Code: `internal/logincheck/profiles.go`
- Evidence: `internal/ui/account_observer_test.go`
- Evidence: `internal/logincheck/observation_test.go`
