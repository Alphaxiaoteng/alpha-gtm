# 0014. 全局 Agent Hub 使用受控本地 CLI Provider

| 字段 | 值 |
|---|---|
| Status | accepted |
| Date | 2026-07-23 |
| Area | agent, ui, llm |

## Context

原 Agent 是左侧独立页面，且聊天路径固定走豆包直连 API。它无法表达本地 Codex/OpenCode/Antigravity CLI 的能力差异，也无法跨工作区保留可见上下文和 provider 原生会话。

## Decision

- Agent 改为 `Layout` 根级的全局右侧 Drawer；旧 `agent` / `terminal` UI tab 只打开 Drawer，不再占用左侧一级入口。
- Go + SQLite 保存会话、消息、Run、SSE 事件、各 provider 的 session 与审批记录。运行 workspace 固定在 `~/.alpha-nexus/agent-workspace/<conversation-id>`，不使用 Alpha-Nexus 源码目录。
- 用户可见 provider 仅为 Codex CLI、OpenCode、Antigravity CLI。豆包是 OpenCode 可选的模型配置，不是独立 provider，也不出现在 Provider 名称中；切换不自动回退或外发数据。
- Codex 使用 JSONL headless + read-only sandbox；OpenCode 使用 `run --format json`、隔离工作目录与禁止所有 CLI 工具的内联权限策略。Antigravity CLI 采用其当前非 TTY stdin 行为：不传 `--prompt` / `--print` 参数，完整聊天内容只写入 stdin，配合 `--sandbox --mode plan` 运行。该行为尚未写入 CLI 帮助，故仅作为实验性纯文本 chat；不声明原生续聊、结构化事件或业务工具能力。
- V1 chat-only 不注册业务 MCP 工具，因此不会浏览器操作、发布、付费请求或网络写入。后续只有通过参数哈希、过期和单次决议的审批桥后才可接 `tools.Registry`。
- Studio 仍使用既有固定豆包逻辑。本 ADR 只取代 0013 中“Agent 也固定豆包”的部分。

## Consequences

本地 CLI 编排不等于本地模型；模型仍可由 provider 云端服务。Codex 与 OpenCode 的真实多轮 chat 可用，但业务工具闭环、OpenCode 的用户私有 provider 配置、Antigravity 的安全宿主输入和可靠会话仍需各自独立验收，不能扩大能力声明。
