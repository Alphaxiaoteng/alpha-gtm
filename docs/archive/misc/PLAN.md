# Alpha-Nexus V1 Plan

## 结论

V1 是 AI-native 本地社媒工具。CLI 是核心接口，AI/Agent 通过 CLI 调度账号、内容、批量任务和发布；本地 UI 是完整操作台，用于账号添加、人工登录接管、任务监控和异常处理。

核心范围：

- 六平台账号管理：Instagram、Facebook、小红书、抖音、视频号、快手。
- 自动发布只做 Instagram / Facebook Page。
- 小红书、抖音、视频号、快手只做 Lite Adapter，不做自动发布。
- 所有高频操作必须支持批量化，TurboPush 欠缺批量化，本项目不能重复这个问题。
- 数据保存到 SQLite，账号 profile 保存在本地独立目录。
- 自动化使用 fingerprint-chromium + CDP `127.0.0.1:15101` + chromedp。
- UI 是本地可视化操作台，不承载独立发布逻辑。

## V1 功能

必须实现：

- 初始化本地数据目录 `~/.alpha-nexus`。
- 初始化 SQLite schema。
- 启动和接管 fingerprint-chromium。
- 每个账号一个独立 browser profile。
- 六个平台账号新增、检查、列表、备注、启用禁用。
- Instagram / Facebook 资产发现。
- Instagram / Facebook 自动发布。
- 小红书、抖音、视频号、快手人工登录接管和登录态保存。
- 批量账号检查、批量目标发现、批量内容导入、批量发布、批量重试、断点续跑。
- 本地 UI 操作台：Dashboard、Accounts、Add Account、Account Detail、Batches、Batch Detail、Content、Logs、Settings。
- TurboPush 数据迁移：账号、配置、内容、素材、发布记录。
- 每个任务保存 NDJSON 事件、截图、DOM 快照、错误码。

V1 不做：

- 完整 Web 管理后台。
- 云服务。
- AI 文案。
- 知识库。
- 官方 Graph API 发布。
- 自动绕验证码、2FA、checkpoint。
- 未经真实 fixture 验收的平台自动提交脚本。
- 大规模无限并发。

## 平台范围

| 能力档 | 平台 | 当前行为 |
| --- | --- | --- |
| 浏览器自动化尝试 | Instagram / Facebook | dry-run；live 可 best-effort，真实账号验收未完成 |
| 受控浏览器提交 | YouTube / TikTok | direct live 仅准备；最终点击只允许单目标冻结计划 + `submit-bound` 审批，真实账号验收未完成 |
| 安全人工接管 | 17 个创作后台平台（含 Lite 四平台） | dry-run；live 只打开目标账号后台，不点击发布 |
| API 占位 | X；YouTube/TikTok legacy API adapter | dry-run；live 返回 `API_CONNECTOR_UNAVAILABLE` |
| 不可发布 | custom | 不创建发布任务 |

所有 live 入口必须绑定内容、账号、资产、平台和稳定幂等键。系统不提供定时发布或周期采集。

## 架构

```text
AI/Agent
  -> CLI / NDJSON
  -> SQLite
  -> Batch Runner
  -> Browser Manager
  -> fingerprint-chromium CDP 127.0.0.1:15101
  -> chromedp automation
  -> Platform Adapters

Human
  -> Local UI 127.0.0.1:17321
  -> SQLite readonly/status + CLI-triggered actions
```

核心模块：

- `internal/browser`：启动、连接、检测 fingerprint-chromium。
- `internal/batch`：批量任务、并发限制、断点续跑。
- `internal/db`：SQLite schema、查询、迁移。
- `internal/platforms/meta`：Meta Business Suite 通用逻辑。
- `internal/platforms/facebook`：Facebook Page 发布。
- `internal/platforms/instagram`：Instagram 发布。
- `internal/platforms/xhs`、`douyin`、`wechat_video`、`kuaishou`：Lite 账号管理。
- `internal/ui`：本地 UI 操作台，负责账号添加、状态查看和人工接管。
- `internal/importers/turbopush`：TurboPush 数据迁移。

## 开发顺序

1. Go CLI 骨架。
2. SQLite schema。
3. 六个平台基础注册。
4. fingerprint-chromium BrowserManager。
5. 通用账号管理。
6. Batch Runner。
7. Lite 平台账号保存和登录态检查。
8. Instagram / Facebook asset discover。
9. 内容创建和批量导入。
10. Facebook Page 自动发布。
11. Instagram 自动发布。
12. 批量发布、重试、断点续跑。
13. 本地 UI 操作台。
14. TurboPush 迁移。
15. TUI 或桌面壳。

## 验收标准

- 六个平台都能保存账号记录。
- 六个平台都能绑定独立 browser profile。
- Instagram / Facebook 能创建任务并自动发布。
- 小红书、抖音、视频号、快手不能自动发布。
- 所有批量任务都有 `batch_runs` 和 `batch_items` 记录。
- 批量任务失败后能 `--resume`。
- UI 能添加账号、显示账号、人工登录接管、查看批量任务、日志和基础设置。
- 失败任务保存截图、DOM 快照、错误码。
- 能从 `/Users/albert/.TurboPush/data.db` 导入通用数据。
