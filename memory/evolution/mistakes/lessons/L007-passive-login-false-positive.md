# L007: 游客 Cookie 导致登录假阳性

| 字段 | 值 |
|------|-----|
| Status | resolved |
| Area | browser |
| Severity | high |
| Date | 2026-06-20 |

## Symptoms

未登录显示「已登录」；甚至把头像存成登录页二维码图。

## Root cause

`len(cookies) > 5` 等弱启发：平台游客态也有大量 tracking cookie。刮取未识别登录页。

## Fix

- 按平台检查**核心会话 cookie**（如 xhs `web_session`）
- 刮取检测登录页特征；过滤 qrcode / 短 data-URI 头像
- 清理库内脏头像数据

## Prevention

- 禁止 cookie 数量阈值判断登录
- 新平台接入时先文档化「登录 cookie 名」

## Related

- Code: `internal/ui/verify.go`
