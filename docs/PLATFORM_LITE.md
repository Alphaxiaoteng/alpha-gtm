# Lite 四平台能力边界

## 结论

小红书、抖音、视频号、快手已接入统一账号、浏览器和发布任务管道。四个平台均支持离线 `dry-run`；live 只打开目标账号的创作后台并进入 `manual_takeover`，不会自动上传、填写或点击发布。

```text
xhs
douyin
wechat_video
kuaishou
```

## 已实现

- 每账号独立 browser profile，由 `browser_profiles.profile_dir` 持久化。
- 通过 `BrowserFacade` 启动、停止、聚焦并绑定本机 CDP。
- 人工登录接管；不保存密码、Cookie、OTP 或二维码内容。
- 登录成功只认可 Selector Catalog 中审核过的精确 Cookie 名；缺少可靠证据的平台返回 `manual_verify`，不会猜测已登录。
- 账号列表、分组、资产、模板、安全设置快照和批量账号检查。
- 发布内容、账号、资产和平台在服务端强绑定；live 请求要求稳定幂等键。
- dry-run 只做本地校验，不启动浏览器、不提交平台。

## 发布行为

| 平台 | live 行为 | 自动提交 |
|------|-----------|----------|
| 小红书 | 校验版本化设置快照，连接目标账号当前运行的 CDP，打开 Creator 后交给用户 | 不支持 |
| 抖音 | 打开目标账号的创作后台后交给用户 | 不支持 |
| 视频号 | 打开目标账号的创作后台后交给用户 | 不支持 |
| 快手 | 打开目标账号的创作后台后交给用户 | 不支持 |

人工完成后，只有用户点击“确认已发布”才写入 `user_confirmed` 发布记录。该记录表示用户确认，不是平台 API 回执。

## 数据采集

小红书保留用户明确触发的一次性本地采集。自动周期采集已删除；其余三平台没有 Native 指标采集器。

## 登录入口

```text
xhs:          https://creator.xiaohongshu.com
douyin:       https://creator.douyin.com
wechat_video: https://channels.weixin.qq.com
kuaishou:     https://cp.kuaishou.com
```

入口由 `internal/platforms/platforms.go` 的内置目录提供。`platform_settings` 保存的是发布模板设置，不覆盖登录或管理 URL。

## 非目标

- 定时发布、周期采集。
- 自动绕过验证码、2FA 或 checkpoint。
- 在没有真实页面证据时补写上传、填写、提交选择器。
- 复制 Turbo Push Cookie、Profile 或设备身份。
