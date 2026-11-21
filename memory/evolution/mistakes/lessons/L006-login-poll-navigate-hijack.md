# L006: 登录态轮询 Navigate 劫持用户页面

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | browser |
| Severity | high |
| Date | 2026-06-20 |

## Symptoms

扫码/填登录时页面每几秒刷新，输入丢失，感觉浏览器「卡住」。

## Root cause

后台 `verifyLogin` 在 URL 不匹配时强制 `chromedp.Navigate(managerURL)`，轮询间隔短，打断人工登录。

## Fix

- 后台用 **被动检测**（读关键 cookie，不 Navigate）
- 仅在确认已登录后做**一次**主动刮取 profile，然后停轮询

## Prevention

- 任何周期任务默认禁止 Navigate
- 人工接管窗口内不跑侵入式自动化

## Related

- Code: `internal/ui/verify.go`, `internal/ui/server.go`
