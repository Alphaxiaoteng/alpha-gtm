# UI Design

> **状态：Legacy，待提取契约。** 下列导航和平台范围已经过时；账号字段、登录接管、批量诊断、无障碍及 Agent 可操作性要求在迁入当前 UI 文档前保留。不得按本文恢复旧信息架构。

## 结论

UI 必须支持账号人工添加和登录接管，不能省略。整体风格参考 OpenDesign：专业、清晰、高信息密度、状态实时、适合大量账号管理。UI 是给人辅助 Agent 的本地完整操作台，不是云后台。

## 信息架构

```text
Dashboard
Accounts
  -> Add Account
  -> Account Detail
Batches
  -> Batch Detail
Content
Logs
Settings
```

左侧导航固定：

```text
Alpha-Nexus
Dashboard
Accounts
Batches
Content
Logs
Settings
```

顶部状态栏：

```text
DB: connected
CDP: 15101
Browser: ready / stopped
Running: 3
Failed: 2
```

## 页面设计

### Dashboard

目标：快速知道系统是否健康。

模块：

- Platform Status：六平台账号数和登录态。
- Active Batches：正在运行的批量任务。
- Failures：最近失败。
- Agent Hints：下一条建议 CLI 命令。

主操作：

- Add Account
- Run Account Check
- Open Failed Batch

### Accounts

目标：管理很多账号，减少人工找账号成本。

布局：

```text
Filters: platform / login_state / enabled / keyword
Table:
  platform
  name
  profile
  login_state
  assets
  last_check_at
  last_error
  actions
```

动作：

- Add Account
- Batch Check
- Open Profile
- Check
- Disable
- Detail

账号登录和添加必须从这里进入。

### Add Account

目标：让人完成平台登录，系统保存 profile 和账号记录。

步骤：

```text
1. Select Platform
2. Profile Setup
3. Launch Browser
4. Manual Login
5. Verify Login
6. Save Account
```

字段：

- platform
- display name，可登录后自动填，也允许手动填
- profile name
- fingerprint seed，默认自动生成
- browser path，默认读取 settings
- proxy，V1 可空
- remark

按钮：

- Launch Browser
- I Have Logged In
- Verify
- Save Account
- Cancel

状态提示：

- waiting_for_login
- checking
- logged_in
- manual_required
- checkpoint_required
- failed

### Account Detail

目标：排查单账号问题。

模块：

- Account Summary
- Browser Profile
- Login State
- Assets
- Recent Jobs
- Recent Errors

动作：

- Open Profile
- Check Login
- Discover Assets，仅 Meta 账号显示
- View Screenshot
- View DOM Snapshot

### Batches

目标：管理批量任务。

表格：

```text
batch_id
operation
platform
total
success
failed
pending
running
status
created_at
actions
```

动作：

- Resume
- Retry Failed
- Stop
- Detail

### Batch Detail

目标：看清每个 item 卡在哪里。

显示：

- batch summary
- item table
- NDJSON event stream
- failed item list

item 表格：

```text
item_id
account
target
content
status
current_step
error_code
screenshot
actions
```

### Content

目标：查看内容和批量导入结果。

V1 不做富文本编辑器。

动作：

- Import CSV
- View Content
- Assign Target

### Logs

目标：排错。

显示：

- job_events
- batch_items error
- screenshot_path
- dom_snapshot_path
- raw_json folded view

### Settings

字段：

- data dir
- browser path
- default CDP port
- UI port
- default concurrency
- screenshot dir
- logs dir

## 视觉规范

风格：

- 深色优先，允许浅色后续再做。
- 字体用系统 monospace / sans。
- 表格高密度。
- 卡片少用，只用于 summary。
- 主要颜色控制在 4 类：background、border、text、accent。
- 状态色固定：
  - success: green
  - warning: amber
  - failed: red
  - running: blue
  - disabled: gray

不要做：

- 大 hero。
- 营销式首页。
- 复杂动画。
- 渐变背景。
- 大面积装饰图。

## 技术实现

第一版技术方向：

```text
local web server
UI views
SQLite queries
core actions
```

不做：

```text
云后台
多用户权限
会员系统
AI 创作
知识库
```

是否使用轻量前端库由实现阶段按 Ponytail 判断；不能为了省略功能而牺牲账号添加、账号显示、登录接管和批量任务体验。

## Agent 友好要求

每个页面都要显示可复制 CLI 命令：

```bash
alpha-nexus account check --id 1
alpha-nexus batch resume --batch 8
```

每个后台动作都要显示：

- command
- status
- error_code
- created_at
- updated_at

## 验收

- 能从 Accounts 页面添加六个平台账号。
- 添加账号时能启动 browser profile 让用户人工登录。
- 登录后能 Verify 并保存账号。
- Accounts 页面能显示账号登录态。
- Account Detail 能显示 profile、资产、错误和截图路径。
- Batches 页面能 retry/resume。
- Logs 页面能查看失败详情。
