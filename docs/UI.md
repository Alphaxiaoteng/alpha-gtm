# Local UI

## 结论

UI 是本地完整操作台，不是云后台。核心接口仍然是 CLI；AI/Agent 通过 CLI 执行任务，UI 给人完成账号添加、账号显示、人工登录接管、状态查看、批量任务和异常处理。

启动命令：

```bash
alpha-nexus-egolite ui serve --port 17321
```

访问：

```text
http://127.0.0.1:17321
```

## 边界

UI 必须做到：

- 只绑定 `127.0.0.1`。
- 参考 OpenDesign 做完整操作台。
- 读取 SQLite 展示状态。
- 调用同一套 core/CLI 逻辑触发账号添加、登录接管、retry、resume、打开 profile。
- 不保存平台密码。

UI 不做：

- 云后台。
- 多用户登录。
- 权限系统。
- AI 创作。
- 知识库。
- 独立发布逻辑。
- 平台自动化脚本。

## 设计原则

参考 OpenDesign：

- 信息密度高，但不省略关键功能。
- 左侧导航，右侧工作区。
- 主操作始终可见。
- 每个后台动作显示实时状态。
- 页面文案短，适合 Agent 和人一起读。
- 所有动作都映射到 CLI/core 操作。

## 页面

### Dashboard

显示：

- 总账号数。
- logged_in / logged_out / manual_required 数量。
- 今日批量任务数。
- 失败任务数。
- 最近失败原因。

### Accounts

显示：

- 平台。
- 账号名。
- profile。
- 登录态。
- 最后检查时间。
- 绑定资产数。
- 最近错误。

动作：

- 添加账号。
- 打开 profile 接管登录。
- 触发单账号检查。
- 触发批量账号检查。
- 禁用账号。
- 编辑备注。

### Add Account

账号添加必须有 UI，不能只依赖 CLI。

流程：

```text
选择平台
  -> 设置 profile 名称
  -> 选择或确认 fingerprint-chromium 路径
  -> 启动独立 profile
  -> 打开平台登录页/创作者后台
  -> 用户人工登录
  -> UI 显示登录检查结果
  -> 保存账号和 browser_profile
```

平台：

- Instagram / Facebook：打开 Meta Business Suite。
- 小红书：打开小红书创作者后台。
- 抖音：打开抖音创作者后台。
- 视频号：打开视频号助手。
- 快手：打开快手创作者后台。

不保存：

- 平台密码。
- 2FA 密钥。
- 明文 cookie 到普通日志。

### Account Detail

显示：

- 基础信息。
- browser profile 路径。
- fingerprint seed。
- CDP 端口。
- 登录态。
- 最近检查历史。
- 关联资产：Facebook Page / Instagram account / Lite 平台账号。
- 最近任务和错误。

动作：

- 打开 profile。
- 重新检查登录态。
- 重新发现 Meta assets。
- 查看截图和 DOM 快照。

### Batches

显示：

- batch id。
- operation type。
- 平台。
- 总数、成功、失败、pending。
- 当前状态。

动作：

- retry failed。
- resume。
- 查看失败 item。

### Logs

显示：

- job_events。
- batch_items 错误。
- screenshot_path。
- dom_snapshot_path。

不做日志分析平台，只做排错入口。

### Settings

显示和编辑：

- data dir。
- browser path。
- 默认 CDP port。
- 默认并发。
- UI 端口。

设置写入本地配置，不写平台密码。

## 实现方式

第一版实现：

```text
internal/ui
  -> local web server
  -> UI views
  -> db readonly queries
  -> core actions: account add/login handoff/retry/resume/open profile
```

不得引入云后台、多用户权限、会员系统。是否使用轻量前端库由实现阶段按 Ponytail 判断。

## 验收

- `alpha-nexus-egolite ui serve --port 17321` 能启动。
- 浏览器打开 `127.0.0.1:17321` 能看到 Dashboard。
- Accounts 页面能列出六平台账号。
- Accounts 页面能添加账号并启动人工登录接管。
- Account Detail 页面能显示 profile、登录态、资产、错误。
- Batches 页面能查看 batch 状态并触发 retry/resume。
- Logs 页面能查看失败错误码和截图路径。
- UI 关闭不影响 CLI 和正在运行的任务记录。
