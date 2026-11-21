# EgoLite 一键发布与本地知识库设计

## 目标

在不恢复 Chromium/CDP 产品执行链、不绕过验证码/风控、不伪造发布成功的前提下：

1. 将现有冻结计划、幂等、审批和证据机制接到 EgoLite Space。
2. 为当前 10 个平台提供统一的 prepare/submit/verify 契约和清晰的人工接管降级。
3. 新增完全本地的 OpenWiki/Markdown/PDF/DOCX 知识库，以低资源 RAG 辅助内容和脚本创作。
4. 单个文档、平台或浏览器步骤失败时记录错误并继续处理其他项目。

## 方案选择

### 知识检索

采用 SQLite FTS5/BM25，而不是首版安装本地向量模型或启动外部知识库服务。

- 优点：复用现有 SQLite，零常驻模型、零额外服务、启动快、可解释、中文标题和关键字检索稳定。
- 限制：语义近义词召回弱于嵌入模型。
- 升级边界：`knowledge.Searcher` 返回稳定的 `SearchResult`，以后可在该接口后增加可选本地 embedding，不改 UI、Agent 或内容创作接口。

未采用方案：

- 本地 embedding + 向量索引：召回更强，但增加模型下载、CPU/内存和索引迁移。
- 外接 OpenWiki/RAG 服务：接入快，但引入进程、权限、版本和隐私边界，不符合本地轻量目标。

### 文档解析

导入器统一产出规范化 Markdown：

- OpenWiki OKF：识别带 `okf_version: "0.1"` 的根 `index.md` 和 Markdown 链接，保留相对来源路径。
- Markdown/Text：原生读取。
- ZIP：仅解包支持格式，拒绝绝对路径、`..` 路径穿越和超额展开。
- DOCX/PDF：通过隔离的纯 Go 转换器适配层解析；扫描 PDF 没有文字时标记 `no_extractable_text`，首版不做 OCR。

安全上限：单输入 64 MiB、单展开文件 16 MiB、总展开 128 MiB、最多 1024 个文件。单文件失败不会回滚同一导入批次的其他成功文件。

原文件保存到 `~/.alpha-nexus-egolite/knowledge/originals/<sha256>`，规范化文本保存到 `knowledge/normalized/<sha256>.md`；数据库只保存元数据、分块和检索索引。SHA-256 用于幂等去重。

### 分块与检索

- 优先按 Markdown 标题切段，再按空行合并。
- 每块目标约 900 个 Unicode 字符，上限 1400，重叠 120；不在标题边界制造无意义重叠。
- 默认返回 6 块，最大 12 块、上下文最多 12,000 字符。
- 结果必须包含知识库、文档、标题路径、来源路径和片段 ID；Agent 输出通过这些 ID 展示引用，不把相似度描述成事实可靠度。

## 数据模型

- `knowledge_bases`：名称、描述、默认库、检索设置。
- `knowledge_import_runs`：批次状态和统计。
- `knowledge_documents`：来源、SHA-256、解析状态、错误码、原文件与规范化文件路径。
- `knowledge_chunks`：文档、顺序、标题路径、正文、字符数。
- `knowledge_chunks_fts`：`content` 与 `heading_path` 的 FTS5 外部内容索引。

删除知识库时用事务删除文档、块和索引；磁盘原件先移到可恢复的本地回收目录，避免数据库成功但文件半删。

## HTTP、Tool Registry 与 Agent

HTTP：

- `GET/POST /api/knowledge/bases`
- `PATCH/DELETE /api/knowledge/bases/{id}`
- `POST /api/knowledge/imports`：multipart，多文件；响应逐文件成功/跳过/失败。
- `GET /api/knowledge/imports/{id}`
- `GET /api/knowledge/documents`
- `POST /api/knowledge/search`
- `POST /api/content-assist` 增加 `knowledge_base_ids` 与 `use_knowledge`。

Registry：

- `knowledge.list`
- `knowledge.import`
- `knowledge.search`
- `knowledge.content-draft`
- `knowledge.script-draft`

Agent 可以检索、生成草稿、创建冻结发布计划，但不能读取任意文件、执行任意 Shell、读取 Cookie 或直接触发最终提交。外部模型只在用户明确允许本次调用时收到检索到的有限片段，不发送完整知识库。

## EgoLite 发布执行链

保留现有 `publish_plans` 冻结快照、action-bound approval、幂等键、单次提交和证据表。新增 `EgoPublishRuntime`，产品执行不再依赖 BrowserFacade、CDP 端口或 Playwright worker。

统一阶段：

1. `observe`：确认矩阵 Space、平台账号和创作者后台登录态。
2. `prepare`：打开或复用平台标签页，上传、填表并回读；禁止点击最终按钮。
3. `approve`：Electron 展示冻结的账号、平台、媒体哈希和参数，用户一次确认整批。
4. `submit`：同一冻结目标最多调用一次平台最终动作。
5. `verify`：以平台后台的账号、标题、状态和证据交叉核验。

状态规则：

- 缺少可靠选择器、出现扫码/验证码/风控：`manual_takeover`。
- 最终动作尚未发生的技术错误：可由用户重新准备。
- 最终动作可能已经发生但证据不完整：`result_unknown`，禁止自动重试。
- 只有可靠证据匹配时为 `published`。

并发规则：

- 同一 Space 严格互斥。
- 不同 Space 并行，受实测并发上限控制，默认最多 3。
- 一个目标人工接管或失败不阻塞冻结批次其他 Space。

平台 manifest 覆盖：抖音、视频号、快手、小红书、TikTok、YouTube、Instagram、Facebook、微信公众号、CSDN。Manifest 只声明经过契约测试的页面和能力；没有真实创作者后台证据的平台显示“需验证/人工接管”，不得用通用选择器猜测点击。

## Electron 交互

新增“知识库”工作区：

- 拖入/选择 OpenWiki 文件夹或 ZIP、Markdown、PDF、DOCX。
- 实时显示每文件的解析、跳过、错误和重试状态；其他文件继续。
- 支持搜索预览、来源筛选、引用预览、默认 top-k 和上下文上限设置。

内容和脚本工作区增加“引用知识库”开关、知识库选择和引用条目；草稿生成后保留来源引用。发布工作区显示每平台的准备、登录、人工接管、已提交待核验和可靠成功状态。

## 验收

- 解析：OKF/Markdown/DOCX/文本 PDF、损坏文件、扫描 PDF、ZIP 穿越、超限、重复文件。
- RAG：中文关键字、标题权重、库过滤、top-k、字符预算、来源引用和空结果。
- Agent：只传选中片段、外部调用需明确允许、发布最终动作需独立审批。
- 发布：10 平台 manifest 契约、prepare 不提交、每目标一次提交、`result_unknown` 不重试、同 Space 互斥、跨 Space 并行、人工接管隔离。
- UI：导入过程不中断、错误可见、创作引用可追溯、发布状态不夸大。
- 真实平台验收必须由已登录创作者后台产生证据；未取得证据的能力明确标记“未验证”。
