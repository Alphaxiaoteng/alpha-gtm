# Meta Automation

## 结论

Instagram / Facebook 自动发布只通过 fingerprint-chromium + CDP + chromedp 模拟点击实现。本文档不包含 Lite 平台发布脚本。

## 支持范围

支持：

- Facebook Page 文本/图片发布。
- Instagram professional account 图片/Reel 发布。
- Meta Business Suite 目标发现。
- 单条发布。
- 批量发布。

不支持：

- Facebook 个人主页稳定发布。
- Instagram 普通个人号稳定发布。
- Stories。
- Graph API 发布。
- 自动绕验证码、2FA、checkpoint。

## 账号模型

```text
accounts.platform_code = facebook
  -> account_assets.platform_code = facebook
  -> account_assets.asset_type = facebook_page

accounts.platform_code = instagram
  -> account_assets.platform_code = instagram
  -> account_assets.asset_type = instagram_account
```

账号创建时仍会有默认 `browser_account`，它只代表本地浏览器登录容器，不能通过 Meta 发布校验。桌面端在 Facebook / Instagram 账号卡片提供“发布目标”表单，用户明确填写目标名称和公开 Page/Instagram ID 后，服务端按账号平台固定资产类型并写入审计事件；该表单不接收密码、Cookie、令牌或其他凭证。自动发现只写入当前 Meta 页面观察到的名称；用户可用同名“发布目标”表单补齐公开 ID，原发现资产会被原地确认而非新增重复项。真实自动发布继续以测试账号验收为准。

## 登录流程

```text
启动 fingerprint-chromium
打开 business.facebook.com
用户手动登录
保存 profile
检查登录态
发现 Facebook Page / Instagram account
写入 account_assets
```

## 发布流程

```text
创建 publish_job
锁定 browser profile
连接 CDP
打开 Meta Business Suite
以目标名称 + 公开 ID 确认已选中目标资产
打开创建器
上传素材
填写 caption
点击发布
等待发布结果
保存 publish_record
释放 browser profile lock
```

自动提交必须同时满足：资产有公开 ID、Business Suite 中找到名称和 ID 均匹配的目标、页面暴露该目标为已选中。任何一项不能确认时，任务停在 `awaiting_review/TARGET_SELECTION_UNCONFIRMED`，不打开创建器、更不点击发布。

## 批量发布

批量发布必须由 Batch Runner 调度：

- 一个 `batch_runs` 记录对应一次批量发布。
- 每个目标发布创建一个 `batch_items`。
- 每个 `batch_items` 成功后关联一个 `publish_jobs`。
- 默认 `concurrency=1`。
- 同一 profile 不允许并发。
- 失败 item 可以单独重试。
- `--resume` 只处理 `pending`、`failed`、`interrupted` item。

## 失败处理

每一步失败必须保存：

```text
current_step
error_code
error_message
screenshot_path
dom_snapshot_path
```

常见错误码：

```text
LOGIN_REQUIRED
CHECKPOINT_REQUIRED
TARGET_NOT_FOUND
UPLOAD_TIMEOUT
PUBLISH_BUTTON_NOT_FOUND
PUBLISH_RESULT_UNKNOWN
PROFILE_LOCKED
CDP_CONNECT_FAILED
```

## Selector 策略

优先级：

1. 可访问性文本和 role。
2. 稳定输入框结构。
3. URL 和页面状态。
4. 局部 DOM 快照辅助判断。

不要依赖纯 class hash。Meta 页面 class 变化频繁。
