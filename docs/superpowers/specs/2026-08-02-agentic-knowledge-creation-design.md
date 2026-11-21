# Agent 化知识创作与文案模型路由设计

## 目标

将“创作工作台”从分散表单升级为可恢复的本地 Agent 创作会话：用户从选题出发，显式挂载知识库，在 SOP 约束下生成、审校、修改并保存本地草稿。文案生成不再复用不透明的全局模型，而由设置中已验证的模型角色绑定决定。

本设计提升可检查性，不把模型评分表述为事实或质量保证。任何内容仍须人工审核，保存只产生本地草稿，绝不触发采集、素材生成或发布。

## 约束

- SQLite 是所有会话、模型选择、证据和质量报告的真源；不新增云服务或常驻向量服务。
- 原始知识文档不可由模型覆盖；生成的 Wiki/摘要是可重建、可审核的派生层。
- 只使用已验证的 Provider + Model 组合；密钥继续只保留在既有 Keychain 路径，角色表不存密钥。
- Kimi API、Gemini OpenAI 兼容 API 都通过既有 `custom` Provider 接入。不能把尚不支持孤立文本任务的 Kimi CLI 伪装为文案运行时。
- 外部模型调用必须由用户在本会话明确开启；关闭时返回确定性的本地结构草案。
- 选题、知识库和 SOP 只发送当前执行必需的有限上下文；不得读取 Cookie、Profile、密码或任意本地文件。

## 方案

### 模型角色

增加三个持久化角色：

| 角色 | 用途 | 回退 |
| --- | --- | --- |
| `conversation` | 创作会话的显式 Agent 请求 | 当前 AgentHub 文本任务选择 |
| `copywriter` | 大纲、文案、脚本与分镜生成 | `conversation` |
| `reviewer` | 事实边界、平台适配、标题一致性和改写建议 | `copywriter` |

角色保存为 `{role, provider_id, model_id}`。读取时必须再次检查 Provider 存在、运行时支持孤立文本任务且模型仍处于验证期；失效时不静默换成任意模型，而是按上述回退链解析并在响应中返回实际使用模型和回退原因。

### 创作会话与质量流水线

`creation_sessions` 保存一个会话的选题、风格、平台、SOP 版本、显式知识库附件、阶段和最近草稿。`creation_runs` 记录每次阶段调用的模型、输入摘要、输出、证据和质量报告，不存 API key。

一次“生成文案”执行如下有限流程：

1. 规范化选题 Brief（标题、受众、冲突、内容承诺、平台和风格）。
2. 只检索会话挂载知识库，构建带 `[KB:<chunk>]` 的有限证据包。
3. 由 `copywriter` 输出结构化草稿；无外部授权时输出本地结构草案。
4. 确定性检查：空稿、标题缺失、长度、未使用的引用、无任何证据的事实风险提示。
5. 由 `reviewer` 输出明确的 `pass` 或 `revise` 与逐项问题；如果未配置独立审校模型，使用文案模型并标记“同模型审校”。
6. `revise` 时只允许一次自动修订；最终用户仍可在对话中继续要求改写。

第一版不做多模型并行投票、自动发布、自动抓取新资料、自动写入知识真源或向量索引。生成的 Wiki 作为下一阶段：基于当前文档/块产生可审阅的主题页与来源链接，审核后才进入可检索派生层。

### API

新增本地 API：

- `GET /api/creation/model-roles`：返回角色配置、可用的已验证模型和回退状态。
- `PUT /api/creation/model-roles/{role}`：保存一个角色的 Provider/Model，拒绝未验证或不支持的模型。
- `POST /api/creation/sessions`：创建会话，要求非空选题；知识库附件是显式 ID 列表。
- `GET /api/creation/sessions/{id}`：读取会话和运行记录。
- `POST /api/creation/sessions/{id}/runs`：执行 `topic`、`draft`、`review` 或 `revise`。`allow_external` 仅对此次请求有效。

旧 `POST /api/content-assist` 保持兼容；新创作工作台只调用会话 API。

### 工作台界面

左栏固定呈现 SOP：选题、证据、文案、脚本/分镜、审核/入库；也提供知识库多选附件与外部模型本次授权开关。主区域为消息式会话，用户可以提出“生成三个角度”“按这个证据重写”“进入审校”等明确请求。每次 Agent 结果显示模型、引用、质量结论和是否发生回退。

入库要求有审核结果；`revise` 或无法识别结论时，仍必须勾选既有人工核验确认。该确认不构成平台发布授权。

## 数据模型

```text
creation_model_roles(role PK, provider_id, model_id, updated_at)
creation_sessions(id PK, title, topic_brief, style, platform, sop_stage,
                  knowledge_base_ids_json, created_at, updated_at)
creation_runs(id PK, session_id FK, action, provider_id, model_id,
              used_fallback, prompt_summary, result, citations_json,
              quality_json, created_at)
```

为保持 SQLite 迁移兼容，表定义进入 `internal/db/db.go` 的内嵌 schema 和单独 `migrations` 文件。删除会话会级联删除运行记录；知识库删除前仍按现有规则处理附件引用。

## 验收

- 未验证/不支持的模型不可绑定角色；失效模型仅能按明确回退链运行或返回可见错误。
- Kimi、Gemini 只作为已验证 `custom` Provider 模型出现，不新增伪 CLI 支持。
- 外部授权关闭时没有 Provider 调用；开启时只发送挂载库的受限引用上下文。
- 每个运行持久化模型、引用、审核结论和回退信息；页面刷新后可恢复。
- 工作台可以从选题创建会话、挂载多个知识库、生成草稿、显示质量结论，并在审校通过或人工确认后保存本地草稿。
- 不新增发布、浏览器、采集或后台定时动作。
