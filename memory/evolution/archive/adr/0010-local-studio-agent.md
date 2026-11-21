# 0010. 本地 Studio 使用显式 LLM 方案与 ffmpeg 执行

| 字段 | 值 |
|---|---|
| Status | superseded |
| Date | 2026-07-17 |
| Area | studio, local-ai, media |
| Superseded by | [0013](./0013-fixed-doubao-agent-gateway.md) |

> 本文保留 2026-07-17 的历史选择。Ollama、本地模型和 11434 已退出当前运行时契约；不要按本文恢复。

## Context

需要补齐独立素材管理、脚本存储和 AI 自动剪辑，但素材、脚本与模型请求不能默认离开本机；也不能把一次生成或剪辑扩展成后台队列、定时器或自动发布。

## Decision

1. 保持 Go 单体与 SQLite：素材只建立 `media_library` 索引，不复制原文件；脚本写入 `studio_scripts`；方案和产物状态写入 `clip_jobs`。
2. 剪辑 Agent 固定连接 `http://127.0.0.1:11434/v1` 的本机 Ollama，固定模型 `llama3.2`，禁用环境代理，不读取 Alpha 的远程模型、密钥或 Provider 配置，也没有远程回退。
3. Agent 只生成含起止毫秒、标题和理由的 JSON 方案。方案持久化后，必须由用户再次点击才调用本机 `ffmpeg`。
4. `ffmpeg` 使用非 shell 参数调用，以 stream copy 生成数据目录下的新 MP4；不覆盖或删除原始素材。运行时先探测 `ffmpeg`，再探测显式安装的 `ffmpeg-full`，两者都必须声明 MP4 muxer。
5. 页面仅在首次进入、用户刷新或动作完成后读取本地状态；不轮询、不自动执行。
6. 同一能力经受限 `tools.Registry` 投影给 Tool Agent 和 MCP：素材沿用既有 `media.import`，Agent 可列出本地素材、保存/列出脚本、生成方案；写 MP4 的工具仍要求独立 `confirm=true`。通用 LLM Tool Agent 即使在模型计划中看到执行步骤也会移除 `studio.clip.execute`；Tool Agent 只重试 read 工具，防止重复写入方案或产物。

## Consequences

### 正

- 本地素材、脚本、模型输入和剪辑产物有清晰边界，可审阅、可恢复。
- 用户能先检查 Agent 方案，再明确选择是否产生本地文件。
- 没有远程模型费用、隐式上传、任务队列或调度器。

### 负 / 限制

- 使用者需自行安装并运行匹配模型的本机 Ollama；不可用时返回 `LOCAL_LLM_UNAVAILABLE`，不伪造方案。
- 当前是基于脚本与媒体元数据的粗剪，不含转写、镜头识别或语义素材检索。
- `ffmpeg -c copy` 快且无重编码，但视频起点可能受关键帧限制。

## Alternatives

| 方案 | 为何未选 |
|---|---|
| 复用已配置远程 LLM | 可能产生费用并发送脚本，不符合本地默认 |
| 后台队列自动执行剪辑 | 失去用户对文件写入时机的明确控制 |
| 上传或复制全部素材到新库 | 增加磁盘占用与数据生命周期复杂度 |

## Verification Gates

- Studio DB CRUD、脚本删除引用保护和迁移一致性测试。
- Agent JSON 校验与不可用模型错误测试，不启动或调用真实模型。
- ffmpeg 参数仅经 `exec.CommandContext` 构造，输出路径限定在数据目录。
- UI 回归测试确认没有轮询，方案与执行保持两个按钮。
- `ALPHA_NEXUS_REAL_LOCAL_SMOKE=1` 显式启用的开发机烟测会使用临时 MP4、本机 Ollama `llama3.2` 和完整 ffmpeg 跑通“导入→方案→执行→探测输出”；默认测试跳过它，避免 CI 下载模型或发起本机模型调用。

## Related

- [0007. 栈决策：Go 控制面](./0007-stack-hybrid-go-control-plane.md)
- [0008. Turbo Push 能力采用混合绞杀集成](./0008-turbopush-capability-integration.md)
