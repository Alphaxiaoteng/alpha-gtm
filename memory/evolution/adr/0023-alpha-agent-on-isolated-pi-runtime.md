# 0023. Alpha Agent 默认使用隔离的 Pi 运行时

| 字段 | 值 |
|---|---|
| Status | accepted |
| Date | 2026-08-01 |
| Area | agent, ui, llm, electron |

## Context

Alpha-Nexus EgoLite 需要一个默认 Agent，但现有 OpenCode/Codex 名称和运行方式会让产品身份、权限边界与进程生命周期混在一起。Pi 提供 JSON 事件流和模型目录，但默认也提供文件、Shell、扩展与 Skill 能力，不能原样嵌入桌面应用。

## Decision

- 产品默认 provider 为 `alpha`，界面名称统一为 **Alpha Agent**；OpenCode、Codex 等保留为用户主动选择的可选 provider。
- 开发版固定使用 `@earendil-works/pi-coding-agent@0.84.2`（依赖要求 Node `>=22.19.0`）。Electron 只把项目内已解析的 Pi 可执行文件绝对路径传给 Go sidecar，不依赖用户 shell 的 PATH。
- 每次用户明确发送消息才启动一个 Pi JSON 子进程；回答结束、取消或超时后立即退出，不保留后台 Agent 进程。
- Pi 固定关闭内置工具、扩展、Skill、Prompt 模板、主题和上下文文件，并使用离线目录发现模式。Alpha-Nexus 不读取或保存 Pi 的凭证。
- Broker endpoint、一次性 token 与应用可执行路径只下发给显式声明支持私有 MCP 的 provider；Alpha Agent 没有该能力，因此不会接收这些字段。
- 只接收文本增量；如果 Pi 产生任何工具执行事件，运行立即 fail-closed。会话文件只写到对话专属的 `.alpha-agent-sessions` 目录，会话 ID 必须通过白名单校验。
- 候选模型来自 Pi 的真实模型目录，但只有真实验证成功的模型才可用于 Agent。系统不自动选择或调用可能收费的模型。
- 当前 Alpha Agent 是受限聊天运行时，尚不投影 `tools.Registry`。后续微调若需要业务工具，必须实现独立、审核过的显式工具桥，不能恢复 Pi 的任意文件或 Shell 权限。

## Consequences

Alpha Agent 获得统一产品身份和短生命周期默认运行时，同时不会因嵌入 Pi 而扩大本地权限。代价是首版不能直接调用浏览器、发布或知识库工具；这些能力必须后续通过现有 Registry 的参数校验、审批和证据链逐项接入。正式发布包中的 Pi 运行时布局尚未验收，本决策只覆盖 Electron 开发版。
