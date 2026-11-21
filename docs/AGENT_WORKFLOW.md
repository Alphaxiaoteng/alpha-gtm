# Agent · Workflow · Tools

> EgoLite 分支说明：本仓库的产品浏览器运行时以 `EgoRuntime` 和矩阵 Space 为准。下文仍出现的 `BrowserFacade`、CDP 与 fingerprint-chromium 内容仅是旧版背景，不是 EgoLite 产品执行链。

## EgoLite 本地知识库与创作

```text
Electron 导入 MD/TXT/PDF/DOCX/OpenWiki ZIP
  → 本地解析、Markdown 归一化、确定性分块
  → SQLite FTS5 检索
  → knowledge.content-draft / knowledge.script-draft
  → 带 [KB:<chunk_id>] 引用的创作简报
  → 冻结发布计划与独立审批
```

- Agent 仅使用 `knowledge.list`、`knowledge.base.create`、`knowledge.import`、`knowledge.search`、`knowledge.content-draft`、`knowledge.script-draft`。
- 单文件失败不终止整批；损坏、加密、图片型或不支持的文件必须明确失败，禁止伪造解析成功。
- 检索限制为最多 12 个片段、12,000 字符。默认完全本地；只有用户当次开启外部辅助时，才允许发送被选中的引用片段。
- 创作简报不包含发布授权。发布仍经过冻结计划、批次清单和一次性审批；未验证平台进入 `manual_takeover`。

## 产品模型（比 CLI 更重要）

Alpha-Nexus 的主消费方不是「人手敲命令」，而是 **自动化工作流 + Tool Agent**。

```text
用户 / 外部 Agent
        │
        ▼
 ┌──────────────────┐
 │  Tool Agent      │  规划步骤、选工具、要审批、重试
 └────────┬─────────┘
          │ tools.Call(name, args)
          ▼
 ┌──────────────────┐
 │  tools.Registry  │  全应用能力目录（单一真源）
 └────────┬─────────┘
          │
    ┌─────┴──────────────────────────────┐
    ▼            ▼            ▼          ▼
 browser      account      publish    tikhub
 media        queue        ops/CDP
```

**CLI / 本地 UI** 只是同一 Registry 的适配层，不另写业务。

| 入口 | 角色 |
|------|------|
| `alpha-nexus tools *` | 人机调试、Agent 绑定 schema |
| `alpha-nexus <domain> *` | 人类友好子命令（可逐步改为薄封装 tools.Call） |
| 本地 UI :17321 | 账号/接管/监控操作台 |
| **未来 Tool Agent** | 调用全部 tools（浏览器、账号、发布、TikHub…） |

## 工作流示例

### 1. 手动更新榜单（TikHub）

```text
tools.call tikhub.update { confirm: true }
  → 或拆分: tikhub.rank.daily + tikhub.user.check_all
  → 结果落盘 ~/.alpha-nexus/tikhub/ranks/YYYY-MM-DD/
```

优先 **free_credit**；单榜约 0.001。Agent 默认 `confirm=false` 只出计划，避免误扣费。

### 2. 账号上线 + 登录接管

```text
account.add → browser.start(account_id)
  → meta.login(account_id) / account.check(account_id)
  → 人机 2FA → meta.discover(account_id) → account.list
```

`meta.login` / `meta.discover` 走 tools.Registry 真实现：只附着目标账号已运行的 loopback CDP，不启动浏览器、不接受外部 CDP、不写凭证；发现后仍需用户在发布目标里确认公开 ID 才能自动提交。

### 3. 发布流水线

```text
content.create → media.import → publish.run (dry_run)
  → 审批 → publish.run (auto_submit) / batch.publish
  → jobs/records
```

一键发布使用冻结计划：内容库 `planned_at` 仅在用户当次明确点击准备时复制到目标 `publish_at`，并尝试填写平台原生定时控件。冻结和操作平台前都必须校验时间；控件无法精确匹配或回读时返回人工接管。Alpha-Nexus 不建立本地定时器、不到点唤醒浏览器，最终发布仍需独立授权。

### 4. 自有账号数据 vs 公开榜单

| 工具 | 数据来源 |
|------|----------|
| `ops.collect` / CDP | **自己账号** 创作者后台 |
| `tikhub.*` | **公开** 榜单 / 竞品 / 搜索（第三方 API） |

## Tool 契约

- **名称**：`domain.action`（如 `browser.start`、`tikhub.rank.daily`）
- **入参**：JSON object（见 `tools schema`）
- **出参**：`{ ok, tool, message, data, error, duration }`
- **side_effect**：`read | write | browser | network`
- **未接线**：handler 返回 `ok=false` 与稳定错误码 `TOOL_NOT_IMPLEMENTED`；禁止把未执行操作包装为 `planned`、`queued` 或 `awaiting_review`

```bash
# 发现
alpha-nexus tools list
alpha-nexus tools list --category tikhub
alpha-nexus tools schema

# 单工具（无网络计划）
alpha-nexus tools call tikhub.config.status
alpha-nexus tools call account.list
alpha-nexus tools call browser.status
alpha-nexus tools call tikhub.rank.daily --args '{}'

# 多步工作流（Tool Agent 原语）
alpha-nexus tools workflow --args '{
  "steps":[
    {"tool":"tikhub.config.status","args":{}},
    {"tool":"account.list","args":{}},
    {"tool":"browser.status","args":{}}
  ]
}'

# 真抓榜（扣 free_credit，需显式 confirm）
alpha-nexus tools call tikhub.rank.daily --args '{"confirm":true,"boards":"video"}'
```

### 已绑定真实现 vs 未接线

| 工具 | 状态 |
|------|------|
| `account.list` / `account.add` | ✅ SQLite |
| `browser.status` | ✅ Facade |
| `tikhub.config.status` / `user.add` / `user.list` | ✅ 本地 |
| `tikhub.rank.daily` / `user.check_all` / `update` | ✅ 需 `confirm=true` 才联网 |
| `workflow.run` | ✅ 多步编排 |
| `browser.start/stop` · `publish.run` | ✅ Facade / Runner |
| `meta.*` | 未接线时显式失败；不制造计划成功 |
| **engines** | fingerprint-chromium · **browseros** · chromium → `docs/BROWSER_ENGINES.md` |

### Tool Agent

```bash
alpha-nexus-egolite agent tools-run --json '{
  "goal":"daily health",
  "max_retries":2,
  "steps":[
    {"tool":"account.list","args":{}},
    {"tool":"browser.status","args":{}},
    {"tool":"tikhub.update","args":{"confirm":false}}
  ]
}'
```

## 与「五 Agent」关系

| 角色 Agent | 用到的 tools（示意） |
|------------|----------------------|
| ContentAgent | `agent.content`, `content.create` |
| AdaptAgent | `content` 改写 |
| PublishAgent | 只做安全边界提示；实际执行必须经 ToolAgent 调用 `publish.run` / `browser.*` |
| MonitorAgent | `account.check`, `browser.status` |
| AnalyticsAgent | `ops.collect`, `tikhub.*`, metrics |
| **Tool Agent（总控）** | **全部** Registry |

五 Agent 是领域策略；**Tool Agent** 是通用执行器，绑 `tools schema` 即可操作整应用。

## 演进原则（ponytail）

1. 新能力先加 **domain 包**，再 **Register** 到 `internal/tools`，最后才挂 CLI 子命令。  
2. 禁止 UI / 外部脚本直接复制业务；一律 `tools.Call` 或 domain API。  
3. 付费 / 浏览器副作用：默认 plan 或 dry-run，显式 `confirm` / 审批后再执行。  
4. 长任务继续 NDJSON + SQLite 状态，方便 Agent 续跑。

## 代码落点

| 路径 | 职责 |
|------|------|
| `internal/tools` | Registry + catalog + Call |
| `internal/cli/tools_commands.go` | `tools list/schema/call` |
| `internal/tikhub` | 榜单/用户监控实现 |
| `internal/browser` | CDP / Facade |
| `internal/agents` | 角色 Agent 骨架 |
| `docs/TIKHUB.md` | TikHub 细节 |
