# Agent 接入方案（Multi-Scheme Hub）

Alpha-Nexus 把「Agent 框架 + API」拆成 **5 条可并行测试的路径**，UI「Agent」页每个方案一个子按钮；CLI / HTTP 同一套 `SchemeRunRequest`。

## 为什么不是单一框架？

| 诉求 | 选择 |
|------|------|
| 查 SQLite、跑工具、可预测 | **Native ToolAgent**（已有 `tools.Registry`） |
| 自然语言编排「做很多事」 | **LLM + Tools Loop**（OpenAI 兼容网关） |
| 轻量接入外部强 Agent，不膨胀 Go 进程 | **OpenCode CLI Bridge**（`opencode run`） |
| 让 Claude / Cursor / OpenCode 挂本应用能力 | **MCP Tool Export** + `agent mcp-serve` |
| 操作指纹浏览器 | **BrowserAgent CDP**（chromedp attach） |

研究结论（2026）：

- **OpenCode**（~160k★）：`run` / `serve` / `acp` / `mcp` 齐全，适合作为 **外挂推理引擎**，通过 MCP 或 CLI 回控本应用，而不是 fork 进 monorepo。
- **MCP** 是跨 Agent 的标准工具面；本项目以 `tools.Registry` 为单一能力源，MCP 只做投影。
- 内嵌 LangGraph / AutoGen 等过重，违背 Ponytail；V1 不引入。

## 五方案一览

### 1. `native` — Native ToolAgent（推荐默认）

- 实现：`agents.ToolAgent` + `tools.Default.Call`
- 无 LLM；多步 plan + 重试
- 测试：账号列表、浏览器状态

```bash
alpha-nexus agent scheme --id native --dry-run
```

### 2. `llm_tools` — LLM + Tools Loop

- 实现：`llm.Gateway.Complete` 产出 `{"steps":[...]}` → ToolAgent 执行
- 环境变量：`AGENTPUSH_LLM_BASE` / `OPENAI_BASE_URL`（默认 Ollama `11434`）
- dry-run 只出 plan 不执行
- `local/<模型>` 固定直连 `127.0.0.1:11434/v1`，不会读取代理、远端地址或 Key
- 通用 Planner 只执行安全本地步骤；发布、任务入队、工作流、TikHub 网络调用与剪辑输出都必须在各自界面单独确认，不能由模型生成的 `confirm:true` 放行

```bash
alpha-nexus agent scheme --id llm_tools --goal "list accounts" --dry-run
alpha-nexus agent scheme --id llm_tools --goal "list accounts" --live
```

### 3. `opencode` — OpenCode CLI Bridge

- 实现：`exec.Command("opencode", "run", "--format", "json", goal)`
- dry-run：只 `--version` 探测
- 配置：`~/.config/opencode/opencode.json`（你已有 proxy/18686）

```bash
alpha-nexus agent scheme --id opencode --dry-run
alpha-nexus agent scheme --id opencode --live --goal "explain alpha-nexus account list"
```

若本机 opencode SQLite 迁移报错，先 `opencode upgrade`，或修复 `~/.local/share/opencode`；桥接层会返回结构化错误，不拖垮 UI。

### 4. `mcp_export` / `mcp-serve` — MCP（对齐 turbopush-mcp）

参考实现：[xueyc1f/turbopush-mcp](https://github.com/xueyc1f/turbopush-mcp)

| TurboPush MCP | Alpha-Nexus MCP |
|---------------|-----------------|
| `mark3labs/mcp-go` stdio | 自研 stdio（Content-Length + 换行双兼容，零额外依赖） |
| HTTP → TurboPush REST | **直连 SQLite + tools.Registry** |
| `list_accounts` / `create_video` / `publish_*` | **同名友好工具** + `browser_*` |
| `~/.TurboPush/mcp.json` | `ALPHA_NEXUS_DATA_DIR` / `~/.alpha-nexus` |

Friendly tools（Agent 肌肉记忆）：

```text
list_platforms, list_accounts, list_logged_accounts
list_articles, get_article, create_article, create_graph_text, create_video, update_article
publish_article, publish_graph_text, publish_video   # 22 个声明路径，dry_run 默认；readiness 决定 live 边界
list_records, get_record_info
browser_status, browser_start, browser_stop
tools_list, tools_call, agent_schemes, agent_scheme_run
registry_*   # 完整 Registry 投影
```

```bash
# 启动（stdio）
alpha-nexus agent mcp-serve

# 一键注册 Claude / OpenCode
./scripts/install-mcp.sh

# 或手动
claude mcp add alpha-nexus -- alpha-nexus agent mcp-serve
opencode mcp add alpha-nexus -- alpha-nexus agent mcp-serve
```

`mcp_export` scheme 仍会写出 schema JSON + client 配置片段，便于 UI 一键测试。

### 5. `browser` — BrowserAgent CDP

- 附着已运行浏览器；不替代 BrowserFacade 启停
- dry-run：无 CDP 时返回 ready；有 CDP 时只 snapshot
- live：LLM 规划 navigate/click/type，验证码 → takeover

```bash
alpha-nexus browser start --account-id 1
alpha-nexus agent scheme --id browser --account-id 1 --goal "https://example.com" --live
```

## API

```http
GET  /api/agent/schemes
POST /api/agent/scheme/run
Content-Type: application/json

{
  "scheme": "native",
  "goal": "probe",
  "dry_run": true,
  "account_id": 0,
  "max_steps": 4
}
```

## UI：统一 Agent 中心（合并 Coding）

侧栏只保留 **Agent**（不再单独「Coding」页）。三个子页：

| 子页 | 用途 |
|------|------|
| **对话** | Workspace 智能体（Claude/OpenCode chat）；只动沙箱 |
| **方案** | 五方案 smoke（native / llm_tools / opencode / mcp / browser） |
| **AI 浏览器** | 在账号指纹浏览器里打开 ChatGPT/Claude/Gemini，**发消息**（不是只控画面） |

### 沙箱规则（硬约束）

```text
~/.alpha-nexus/agent-workspace/          # 唯一可写工作区
  automations/                           # 允许微调的自动化脚本
  notes/  exports/
```

- Agent **可以** 调 MCP / tools、读写 `agent-workspace`（尤其 `automations/`）。
- Agent **禁止** 改 Alpha-Nexus 应用源码（`internal/`、`ui-web/`、`electron/`、`cmd/`…）。
- API `/api/coding/chat` 会 **强制** `work_dir` 为沙箱，忽略客户端指到 app 源码的路径。

实现：`internal/agentws` + `terminal.Chat` policy 注入。

```bash
alpha-nexus agent workspace path
alpha-nexus agent chat --engine claude --message "list MCP tools"
```

### CLI 控制浏览器内 AI 窗口

不只是 CDP 截图/点击页面任意位置，而是打开 **AI 聊天页 → 聚焦输入框 → 输入 → 发送**：

```bash
alpha-nexus browser start --account-id 1
alpha-nexus browser ai-chat --account-id 1 --provider chatgpt --message "hello"
alpha-nexus browser ai-chat --cdp-url http://127.0.0.1:15101 --provider claude --message "hi"
```

HTTP：`POST /api/browser/ai-chat` `{ account_id|cdp_url, provider, message, url? }`

## API（补充）

```http
GET  /api/coding/status          # engines + sandbox paths
POST /api/coding/chat            # { engine, message, continue } → 强制沙箱
POST /api/browser/ai-chat        # 浏览器内 AI 发消息
GET  /api/agent/schemes
POST /api/agent/scheme/run
```

## 推荐组合（生产）

```text
当次明确执行   → native 工具
对话式运营/工具  → Agent 对话 + MCP（沙箱）
重推理           → opencode bridge + MCP
浏览器 AI 对话   → browser ai-chat（CDP 打开 AI 窗发消息）
浏览器动作       → browser scheme（CDP）
外部 IDE Agent   → mcp-serve
```

## 非目标

- 不做云端多租户 Agent 平台
- 不把 OpenCode 源码 vendoring 进仓库
- 不绕过验证码 / 不存平台密码
- 工作区 Agent 不改写 App 源码（仅 automations 微调）
