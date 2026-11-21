# L018: 统一 Meta 账号缺少登录契约导致状态反复降级

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | browser |
| Severity | high |
| Date | 2026-07-26 |
| Last verified | 2026-07-26 |

## Symptoms

通过界面新增的统一 `meta` 账号已在 Business Suite 登录，但完整登录检测只能依赖易变 DOM；页面布局变化时，账号会反复显示未验证或掉线。

## Root cause

界面和平台目录已把 Facebook Page 与 Instagram 资产合并为 `platform_code=meta` 的登录容器，但 `internal/selectors/catalog.v1.json` 只有 `facebook` 与 `instagram`，没有同名 `meta` 会话契约。观察器因此无法为统一账号读取受审 URL 和 Cookie 名。

## Verified correction

- 为 `meta` 增加 Business Suite / Facebook URL 与 `c_user`、`xs` Cookie 名契约。
- 不把 Instagram 单独的 `sessionid` 当成完整 Business Suite 登录证据。
- 登录观察测试确认 `meta` 能通过受审 Cookie 名保持 `logged_in`，并保留现有 Meta DOM 证据优先级。

## Prevention

- 新增或合并平台代码时，同时检查平台目录、登录观察规则和选择器目录是否都有同名契约。
- 平台别名必须有单独回归测试，不能假设底层平台契约会自动继承。
- 统一登录容器只接受能证明容器本身可用的会话证据，不能用单个资产站点的 Cookie 放宽认证边界。

## Related

- Code: `internal/selectors/catalog.v1.json`
- Evidence: `internal/selectors/catalog_test.go`
- Evidence: `internal/logincheck/observation_test.go`
