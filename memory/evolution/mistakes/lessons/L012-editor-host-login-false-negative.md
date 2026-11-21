# L012: 创作编辑器域名导致登录假阴性

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | browser |
| Severity | high |
| Date | 2026-07-20 |
| Last verified | 2026-07-20 |

## Symptoms

账号已登录且创作编辑器正在使用，但被动监测返回 `PLATFORM_TAB_MISSING`，UI 只能沿用旧状态或显示无法确认。

## Wrong path

只把账号主页域名加入目标白名单，并要求每个页面同时暴露昵称、头像和账号 ID。创作编辑器通常不重复提供完整身份区，扩大通用选择器又会重新引入身份误采集。

## Root cause

同一平台的账号主页和创作编辑器可能使用不同受控域名。编辑器能提供可信的已登录会话 DOM，但不一定能提供足够强的身份来源。

## Verified correction

- 在 `internal/logincheck/profiles.go` 单独登记已审核的编辑器域名及会话容器/文本。
- 在 `internal/logincheck/observation.go` 区分 `SessionVerified` 与 `IdentityVerified`：前者只确认登录，不覆盖已持久化的昵称、头像和账号 ID。
- 用真实已打开的编辑器页面验证会话状态，并由单元测试锁定域名和证据边界。

## Prevention

- 接入平台时枚举账号主页、创作后台和编辑器的全部受控 origin。
- 登录证据与身份证据必须分开建模；弱页面不能改写身份。
- 每个平台至少保留一个“当前仅打开编辑器”的回归场景。

## Related

- Evidence: `internal/logincheck/observation_test.go`
- Code: `internal/logincheck/profiles.go`
- Code: `internal/logincheck/passive.go`
- Code: `internal/logincheck/observation.go`
