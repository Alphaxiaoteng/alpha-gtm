# 匿名想法与 Bug 反馈

## 当前开发模式

```text
Electron 想法抽屉
  -> 本机 Go /api/feedback
  -> 飞书开放平台 API
  -> 开发者飞书 Base
```

当前阶段不使用网关或云服务器。Go Sidecar 通过飞书自建应用的
`tenant_access_token` 在本机直接读写指定 Base；React 不接触飞书凭证。

飞书 Base 是处理状态和开发者回复的业务真源。本机 SQLite
`feedback_receipts` 只保存本机提交内容、飞书记录 ID、随机查询凭证和最近同步
结果，不保存 App ID、App Secret、tenant token 或 Base/Wiki token。

> ponytail: 这是开发者单机模式。若未来向不受信任用户分发应用，桌面客户端
> 无法永久隐藏开发者 App Secret；正式公发前必须改为每用户 OAuth 或受控网关。

## 飞书 API

- 凭证：`POST /open-apis/auth/v3/tenant_access_token/internal`
- Wiki 解析：`GET /open-apis/wiki/v2/spaces/get_node`
- 字段核对：`GET /open-apis/bitable/v1/apps/:app_token/tables/:table_id/fields`
- 创建反馈：`POST /open-apis/bitable/v1/apps/:app_token/tables/:table_id/records`
- 批量同步：`POST /open-apis/bitable/v1/apps/:app_token/tables/:table_id/records/batch_get`

创建请求使用 UUID `client_token` 防止网络重试产生重复记录。状态同步只读取这台
设备已经保存的飞书 `record_id`，不扫描整张表。

## 飞书字段

`需求意见` 是唯一强制字段。客户端先读取真实字段列表，只写表中已经存在的可选
字段，禁止猜测字段：

| 字段 | 建议类型 | 用途 |
|---|---|---|
| `需求意见` | 多行文本 | 用户输入，必填 |
| `反馈编号` | 单行文本 | 匿名可读编号 |
| `查询凭证摘要` | 单行文本 | 本机随机凭证的 SHA-256；不写明文 |
| `反馈类型` | 单选 | `未分类 / Bug / 建议 / 想法 / 其他` |
| `处理状态` | 单选 | `待处理 / 处理中 / 已完成 / 不采纳` |
| `是否处理完成` | 复选框 | 兼容旧表；勾选映射为 `已完成` |
| `开发者回复` | 多行文本 | 同步回本机展示 |
| `应用版本` | 单行文本 | 自动元数据 |
| `当前模块` | 单行文本 | 自动元数据 |
| `操作系统` | 单行文本 | 只记录系统类型 |
| `发布渠道` | 单行文本 | 当前为 `electron` |

## 本机安全配置

凭证作为一份严格 JSON bundle 保存于 macOS Keychain，service 为
`alpha-nexus.feedback.feishu`。Secret 只经标准输入写入，不进入 argv、源码、
`.env`、`config.json`、SQLite 或日志。

开发机一次性配置：

```bash
read -s FEISHU_APP_SECRET
printf '%s' "$FEISHU_APP_SECRET" | ./alpha-nexus feedback configure \
  --app-id '<app-id>' \
  --target-url 'https://example.feishu.cn/wiki/<wiki-token>?table=<table-id>' \
  --app-secret-stdin
unset FEISHU_APP_SECRET
```

飞书应用至少需要 Wiki 节点读取（Wiki URL 时）、Base 字段读取、记录创建和记录
读取权限，并且必须被添加为目标 Wiki/Base 的协作者。任何已经出现在聊天、截图
或日志中的 Secret 都应先轮换，再录入钥匙串。
