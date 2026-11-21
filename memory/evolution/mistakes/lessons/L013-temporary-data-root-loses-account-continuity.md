# L013: 临时数据根目录导致账号与登录会话看似丢失

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | db |
| Severity | high |
| Date | 2026-07-21 |
| Last verified | 2026-07-21 |

## Symptoms

前一天存在的多个账号在开发页面中全部消失；重新补账号后，浏览器能启动，但平台页面要求重新登录，账号头像也没有恢复。

## Wrong path

开发启动脚本使用一次性目录或不稳定的数据库路径，再把新数据库中的空账号列表误判成平台集体掉线。仅恢复账号名称也看似能修复 UI，但新账号记录对应的是新的本地 profile，不能恢复原会话。

## Root cause

SQLite 账号记录和一账号一 profile 都以同一个 data root 为身份边界。切换 data root 等同于启动另一套 Alpha-Nexus；账号元数据、浏览器会话与本地头像不会自动跨根目录迁移。

## Verified correction

- 开发启动脚本默认使用稳定 data root，并允许通过同一组显式环境变量覆盖。
- 账号登录证据继续写入 `account_login_events`，浏览器起停/心跳/崩溃写入 `browser_events`。
- 账号“状态记录”同时展示两条安全时间线；诊断 JSONL 只记录稳定代码和不可逆标识，不记录 Cookie、页面内容或 profile 路径。
- 恢复元数据后不伪造 `logged_in`；只有平台 DOM 的明确证据才能恢复已登录状态。

## Prevention

- 开发、网页代理和桌面壳必须显式指向同一个稳定 data root；禁止日常启动使用临时目录。
- 排查“账号丢失”时先核对 data root 和账号创建时间，再区分浏览器进程状态与平台登录状态。
- 不复制 Cookie 或 profile；旧 profile 不可用时要求用户在受控账号浏览器中重新登录。
- 每次状态变化保留登录证据码和浏览器生命周期事件，重复相同证据只更新最后确认时间。

## Related

- Evidence: `internal/db/account_login_observation_test.go`
- Evidence: `internal/ui/account_observer_test.go`
- Code: `scripts/alpha-nexus-dev.sh`
- Code: `scripts/init-dev-db.sh`
- Code: `internal/db/account_login_observation.go`
- Code: `internal/ui/account_observer_handlers.go`

