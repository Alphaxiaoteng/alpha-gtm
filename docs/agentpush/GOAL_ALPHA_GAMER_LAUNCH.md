# Goal：Alpha-Nexus EgoLite 本地内容运营闭环

## 当前目标

在 Electron 开发版中完成一套本地、可恢复、可审计的内容运营闭环：

1. EgoLite 矩阵浏览器与平台账号登录、资料和头像同步。
2. 本地知识库导入、解析、检索和可追溯引用。
3. 趋势选题 → 文案 → 分镜脚本 → 内容库的创作 SOP。
4. 冻结批次、一次授权、EgoLite prepare/submit 和独立后台核验的一键发布。
5. 抖音、小红书、视频号、快手近 7 天只读数据采集、SQLite 持久化和本地复盘。

本轮只开发 Electron，不生成正式发布包或新图标。SQLite 真源为
`~/.alpha-nexus-egolite/app.db`。旧项目 `~/.alpha-nexus` 的登录态、截图和
发布记录不是 EgoLite 版本的验收证据。

## 唯一运行时边界

```text
Electron / React / CLI / Tool Agent
               ↓
        tools.Registry / local HTTP
               ↓
 SQLite + EgoRuntime / EgoSpaceService
               ↓
       ego-browser nodejs + EgoLite Space
```

- 产品运行时只使用 EgoLite；不启动 Chromium，不分配 CDP 端口，不管理 profile 目录。
- 一个逻辑浏览器（矩阵）绑定一个持久 Space；同一矩阵的多个平台共享该 Space。
- 不触碰 EgoLite Default，不读取或导出 Cookie、密码和 EgoLite 用户目录。
- 同一 Space 串行；不同 Space 才允许并行，默认最多 3 个。
- 验证码、扫码、2FA、checkpoint 和风控一律交给用户。
- Agent 只能调用审核过的 Registry 工具，不获得任意 Shell、Cookie 或最终提交权限。

## 登录与账号验收

- Electron 创建和管理“浏览器1、浏览器2……”；内部 Space ID 不暴露给用户。
- 每个平台行独立显示平台名、平台账号名、ID、头像和登录状态。
- 打开登录后建立 60 分钟有界核验会话；关闭二维码弹窗只归还控制权，不取消核验。Go 后台只在该会话存在时启动短时核验，按 2/4/8/15 秒退避，成功、过期或取消后即停止，不依赖 Electron 当前页面，也不常驻 AI Agent。
- 同一 Space 的一批最多 3 个平台在一次 EgoLite Runtime 调用中串行核验，避免每个平台重复冷启动；返回后逐账号落库并立即刷新 UI。
- 后台核验与手动“本批登录完成”共用串行入口；单平台登录弹窗提供“完成登录并同步”，同步期间按钮禁用并立即回写账号名称、ID、头像与登录状态。Electron 只读取落库状态与自动刷新过期二维码，不再重复接管同一 Space。
- 账号列表前后端均使用 `no-store`，可靠登录观察落库后不得继续显示缓存状态。
- `unknown` 不得覆盖已可靠确认的登录态；只有明确登录页证据才能标记 `logged_out`。
- 长期会话监控可配置，但不替代登录期间的快速核验。

当前证据（2026-08-01）：

- 浏览器1的 10 个首版平台按 Meta 合并模型对应 9 个启用登录账号，当前 9/9 均落库为 `logged_in`；Meta 使用一个登录容器管理 Facebook/Instagram 资产。
- 微信公众号只读核验当前实测 5.48 秒，账号名称和本地规范化头像均写入当前 EgoLite 数据库；同批最多 3 个平台现在只启动一次 Runtime，不再累计 3 次冷启动延迟。
- 公众号当前数据库状态为 `AlphaIntelligence / logged_in / observed avatar`。此前“已登录但 UI 迟迟不更新”的共因是快速核验由账号页临时轮询触发，离开页面、应用重启或会话边界后只剩 60 分钟监控；现已迁到有界 Go 后台同步器，账号 API 仍每秒 no-store 刷新。
- CSDN 创作中心实测同时存在账号昵称“墨衍 MoGrow”和公开博客标题“阿尔法智能”；当前解析已改为创作中心昵称优先，浏览器1账号资料为 `墨衍 MoGrow / weixin_49486457`。
- 简化账号页现在每秒同步主账号与 Meta 资产资料：所有行直接显示平台名称，Meta 登录容器下分别显示 Facebook Page 与 Instagram Account 的头像/名称。启动迁移及后续 logged-in 观察都会校准 `browser_account` 资产，避免 CSDN 等旧资产名称重新覆盖规范账号资料。
- `go test -p 1 ./... -count=1` 全量回归、前端 145/145、Electron 单元测试 34/34、账号页 E2E 12/12 和前端构建通过；后台登录同步集成测试覆盖成功资料落库、会话完成、未知保留和过期不打开 EgoLite。默认产品依赖门禁同时确认不含 `chromedp/cdproto`、旧 logincheck/sessionbackup/publishverify 运行链。Electron 开发版已重启加载当前构建（2026-08-01）。
- 2026-08-02 补充回归修复了登录会话在约 6 秒后停止核验的共同根因：待登录或暂时失败的会话会继续按 2/4/8/15 秒有界轮询，直到成功、取消或 60 分钟会话到期；单次检查结束即归还 Space，不维持 AI Agent 控制。定向 `internal/egolite` 测试已覆盖第五次核验才成功的延迟扫码场景。

## 知识库与创作 SOP 验收

知识库必须：

- 支持 Markdown、纯文本、PDF、DOCX、HTML/OpenWiki 等本地文件导入。
- 单文件失败不终止同批其他文件；解析结果分块保存在 SQLite。
- 检索结果返回知识库、来源、标题/章节、chunk ID 和可追溯引用。
- 内容辅助、脚本编辑器与创作工作台通过结构化 API 使用知识库，不直接读取原文件。

当前证据（2026-08-01）：

- 当前数据库：2 个知识库、2,337 个 `ready` 文档、19,040 个 chunks；两次真实初始导入分别完成 1,177/1,177 与 1,160/1,160。后续目录新增的 `opc-dashboard/index.html` 只是无正文的前端应用壳，幂等重跑明确记录 `no_extractable_text`，不把它计入“已导入”。
- OpenWiki HTML/HTM 已接入本地安全 HTML→Markdown 转换；`script/style` 和危险链接不进入索引，ZIP/Markdown/TXT/文本 PDF/DOCX 继续使用同一失败隔离流程。
- Registry `knowledge.search` 已对 Alpha-Notes 执行真实本地检索并返回带 `[KB:<chunk_id>]` 的编导、钩子和分镜内容。
- 知识库混合导入 E2E、引用检索和 Creation Workbench 相关测试通过。
- 知识库页默认展示 Alpha-Notes 编导库，并在顶部汇总 ready 文档、知识片段和本地库数量；资料目录按原相对路径展示类型与索引状态，并限制首屏 100 项、支持文件名/目录筛选。增量同步结果使用“新增 / 已存在 / 失败”和“索引已同步”，不再把 1,160 个幂等跳过项写成容易误解的“成功 0 / 跳过 1,160”。失败文档不再混入成功统计。CLI 收到中断信号会把导入运行记为 `interrupted`，不再留下永久“正在导入”。
- 增量同步保存来源文件大小与纳秒 mtime；两者未变时不再从 iCloud 重读正文。旧索引只有在来源时间不晚于索引时间、且大小等于本地原件副本时才回填缓存，否则继续 SHA256。真实 1,161 文件目录首次兼容回填耗时 10.22 秒，随后纯缓存重扫 3.30 秒；两次均为 1,160 跳过、1 个无正文 HTML 失败。
- 2,337 个文档当前只有 1,177 个 distinct SHA，跨知识库重复内容在检索层去重。
- 隔离的 Playwright UI E2E `creation-workbench.spec.ts` 已完成“趋势证据 → 选题 → 文案 → 脚本 → 分镜 → 审核 → 脚本库/内容库”，并由真实本地后端写入后回读 SQLite（1 条 video 脚本、1 条 draft 内容）；测试期间未启用外部模型、发布或数据采集。Electron 壳级 E2E `creation-workbench.electron.spec.ts` 也已通过真实 Electron、受监督 Go sidecar 和隔离 SQLite 完成同一闭环，并确认启动及创作过程的 EgoLite Space 数量为 0；启动竞态修复后连续 5 轮通过。测试使用专用绝对 `userData` 隔离单实例锁，不复用或覆盖开发版数据，结束后自动清理临时目录。

创作 SOP 必须保持五个阶段，但剪辑阶段本轮暂停：

1. 趋势证据与爆款参考生成候选选题。
2. 结合风格设置和知识库完成仿写、编导与文案。
3. 文案拆成脚本、分镜和素材需求。
4. 剪辑平台：暂停，不进入当前完成门槛。
5. 审核后的结果显式保存到内容库。

创作结果必须保留原始趋势证据、知识库引用和人工修改；Agent 不能静默覆盖草稿。

内容审核返回结构化 `review_decision=pass|revise|unknown`。脚本在任何结论下都可保存为本地草稿；内容只有明确 `pass`，或用户逐项勾选“我已人工核验”后，才能通过专用 `/api/creation/drafts` 端点保存为本地 `draft`。后端会重新解析审核结果、计算规范内容 SHA-256 并把审核结论、人工确认和哈希写入正文审计记录；该确认不是发布授权。修改任一选题、文案、脚本或分镜后立即使旧审核与人工确认失效。

## Skill 库

- 设置页包含“Skill 库”分区，可查看内置和本机 Skill、新建指令 Skill、导入标准 `SKILL.md`、启停已审核能力，并通过 Agent 中的 `/skill-id` 或 `@skill-id` 显式调用。
- 内置 Skill 在应用启动时按代码目录同步名称、说明、运行时绑定和 `SKILL.md`，同时保留用户的启停选择；本机自定义 Skill 独立存放且不会被启动同步覆盖。
- 涉及浏览器、固定脚本、账号或发布的本机自动化 Skill 默认进入 `review_required`，审核前不能启用。Skill 只能扩展 Agent 指令，不能授予 Shell、Cookie、浏览器或最终发布权限。
- 2026-08-02 定向回归已通过：`internal/skilllib`、Skill HTTP 路由、设置页入口、Agent 斜杠/艾特建议与本机导入契约。

## 一键发布契约

范围仍为 10 平台、16 形态：

- 图文 + 视频：Instagram、Facebook、小红书、抖音、快手、TikTok。
- 视频：视频号、YouTube。
- 文章：微信公众号、CSDN。

固定流程：

```text
选择内容和账号
  → 冻结账号 / Space / 形态 / 媒体哈希 / 平台参数 / 排期
  → prepare（禁止最终点击）
  → 展示冻结批次并取得一次 submit-bound 授权
  → 每个目标最多一次最终点击
  → 独立创作者后台核验
```

安全状态：

- `ready`：没有最终点击，可以修复后继续。
- `manual_takeover`：登录、验证码、页面或控件需要人工处理。
- `result_unknown`：可能已经点击但缺少成功证据；禁止自动重试。
- `submitted_pending_verification`：已点击，等待只读后台核验。
- `scheduled_verified`：后台确认匹配账号、内容和排期证据。
- `published_verified`：后台确认 Published/Public，并已写入公开记录。

当前真实边界（2026-08-23）：

| 平台范围 | 当前能力 | 当前验收 |
|---|---|---|
| 首版 10 平台 / 16 形态 | `egopublish` capability 是唯一真源；15 个形态为 `automated` prepare，TikTok `graph_text` 为 `manual_handoff` | 前端、冻结、prepare、submit、verify 均读取同一 capability；UI 只有在 `prepare_adapter_supported=true` 时才标为自动预填 |
| 全部首版 16 形态 | `prepare_adapter_supported` / `submit_adapter_supported` / `verify_adapter_supported` 只表示本地适配代码存在；不等于已验收 | 自动模式缺少 prepare adapter 时以 `EGO_PUBLISH_PREPARE_UNAVAILABLE` fail-closed；所有 `submit_verified=false`、`verify_verified=false`，不创建最终点击、不升级创作者后台证据为成功 |
| TikTok 图文 | 官方 Photo Post 入口仅供人工接管 | `manual_handoff`，提交和核验适配及 verified 均为 `false` |
| Bilibili | 目录中的显式人工路径 | 不属于首版 10 平台 / 16 形态，不能计入首发覆盖 |

发布账号卡和冻结清单直接标注 capability 的 `自动预填` 或 `人工接管` 边界；可选择的
平台或本地适配代码都不得被误读为已自动提交或已验证发布。

本地安全门禁已经覆盖：CSDN prepare 填写并回读必填标签，submit-bound
执行在唯一最终点击前再次从发布选项面板核对冻结标题、正文、标签和唯一可用按钮；
任一项不匹配均停在 `manual_takeover`。抖音图文多素材仍保持
`EGO_PREPARE_MULTI_MEDIA_UNVERIFIED`，因为当前 ego-browser 官方 `uploadFile`
契约只展示单文件参数；在没有真实 EgoLite 多文件上传证据前不伪装支持。
运行时返回的 `clicked=false` 会释放提交预占并保持可恢复；若它同时声称
`submitted_pending_verification`，则降为 `manual_takeover / EGO_SUBMIT_STATE_CONFLICT`。
缺少点击证据却声称已提交时统一降为 `result_unknown`，继续禁止自动重试。
冻结媒体在 prepare 后发生变化时，submit 现在会在创建提交 Job、占用单击资格和接管
Space 前以 `EGO_PUBLISH_MEDIA_HASH_CHANGED` 拒绝；真正运行 submit 前仍再次校验，避免
检查与执行之间被替换。相关 EgoLite、publishplan 和 UI 回归测试已通过。

2026-08-02 修复了 Electron 发布页把 prepare 与 submit 合并在同一次点击中的授权缺口：
“预填并打开平台”现在只消费 prepare-bound 授权并把 Space 交给用户检查；只有用户随后
再次点击“确认并提交一次”，才会申请独立 submit-bound 授权并进入单次最终点击。两个动作
由独立授权函数执行，定向测试已证明 prepare 路径不会调用 submit；真实 Electron E2E 尚未
在当前用户实例上重跑，因此本项仍不作为真实平台发布成功证据。

不得用旧 Chromium 项目的排期截图、旧 Job ID 或 `~/.alpha-nexus` 数据提升本表状态。

## 趋势雷达旧项目数据迁移

- EgoLite 版趋势雷达以旧项目 `~/.alpha-nexus` 的业务数据为一次性真源：22 条 TikHub 调用索引、18 个原始调用文件、170 个封面，以及雷达配置、观察名单、RSS、视频搜索、博主与视频拆解目录，共 194 个业务文件。
- 2026-08-02 已逐项核验：两边调用记录在重写数据目录前缀后哈希一致，全部业务文件逐文件一致。旧 `trend-radar/bin` 和 `trend-radar/logs` 中的 worker 二进制及 stdout/stderr 日志不属于业务数据，明确不迁入，也不在 EgoLite 版恢复旧后台工作器。
- `radar import-legacy --confirm` 使用只读 immutable SQLite、白名单路径、私有权限、事务和可恢复备份；重复执行不会产生重复调用记录。

## 全平台数据采集契约

当前覆盖抖音、小红书、视频号、快手、TikTok、YouTube、Meta、微信公众号和 CSDN；Facebook 与 Instagram 通过同一个 Meta 登录容器采集：

- 只处理已登录且已绑定 Space 的账号。
- 每次只读一个 Space；不点击发布，不上传，不读取 Cookie。
- 只保存最近 7 天日数据和作品数据；原始证据保留本地。
- 只有 `range_verified=true` 才写入快照；`unknown` 和范围不完整不得伪造成 0。
- UI 采集必须由用户显式触发；后台不周期采集。
- 本地复盘允许确定性规则；调用模型时必须标注模型与输入证据，不能把截图猜测当平台数据。

当前边界（2026-08-02）：

- Registry 已提供 `social.metrics.collect`、`collect-batch`、`overview`。
- 抖音具备受审查资源分页、游标/作品去重和最多 5 页/250 条保护。
- 9 个内容平台均已有 7 日成功 fixture、SQLite 落库、`unknown` 不落快照和缺失 Space 不调用 runtime 的契约测试；仪表盘 mock E2E 已证明按浏览器隔离、加载不自动采集、显式点击只提交当前浏览器账号。
- 当前浏览器1本地证据（2026-08-02 SQLite 真源）：抖音和快手均以 `REVIEWED_CONTENT_RESOURCE_EMPTY` 确认完整近 7 日作品窗口为空；小红书已以 `REVIEWED_ANALYTICS_RESOURCE` 写入 `range_verified=1` 的近 7 日零值快照，但没有可靠粉丝字段，因此 `followers_verified=0`。三者都不得解读为“没有账号”或虚构粉丝数。视频号账号当前明确为 `logged_out`，最近一次采集状态是 `skipped / LOGIN_REQUIRED`，没有快照；重新登录前继续跳过。快手历史误匹配的 124,000 粉丝和营销文案 1,000 播放仍保持 `INVALIDATED_KUAISHOU_DOM`，不得参与汇总或 AI 复盘。
- 指标快照日期已统一使用 UTC；这修复了上海午夜后采集写入次日、而仪表盘仍按 UTC 前一日查询导致“采集成功但看板不显示”的跨日问题。
- 小红书账号数据首先打开官方页 `https://creator.xiaohongshu.com/statistics/account/v2`；当前只允许读取该页的结构化页面证据。用户计划次日提供/启用 API，在实际接口与字段语义验证前不把请求候选升级为成功快照。
- 视频号失败根因已收敛到私有统计接口使用原始请求上下文，而旧采集器错误地把 Performance URL 统一重放为 GET。当前实现通过 EgoLite Runtime 内审核过的 Network helper 读取白名单、同平台的实际响应体：单条最多 1 MB、内存最多 24 条，原文不写日志/API/SQLite；无实际响应时才保留 GET 回退。Node 执行级契约已证明可从模拟的已认证 POST 响应解析 7 日指标且不会输出原始字段。真实浏览器1复测等待用户下一次明确允许接管，当前仍标记“未验证”。
- 当前复盘保留确定性本地规则，并已提供用户显式点击后才调用配置模型的复盘入口；只发送当前浏览器的已验证指标证据，不发送 Cookie 或页面 HTML。真实模型调用和 9 平台真实后台成功 dry-run 仍未验证。
- 仪表盘顶部除有效快照覆盖率外，新增互斥的当前状态分布：`已验证 / 证据不足 / 数据过期 / 未登录 / 未采集`；历史采集行在当前 7 天窗口不存在有效快照时明确标记“数据过期”，不再沿用“已验证”。平台卡继续显示最后采集时间和可操作原因，避免把“账号已登录”或旧证据误当成当前指标。
- 后端在保留原全局 `daily` 兼容汇总的同时新增带 `account_id` 的 `account_daily`；仪表盘趋势只能聚合当前选中浏览器的账号，并明确每根柱是“采集日对应的近 7 天窗口快照”，不是伪造的单日流量。浏览器2数据混入浏览器1趋势的回归测试已覆盖。
- 目标相关隔离 Web E2E 已真实执行 18/18：账号管理 12、浏览器安全 3、创作 SOP 1、全平台数据看板 1、知识库 RAG 1；Electron 壳级创作 E2E 1/1 另行通过。
- 2026-08-02 采集解析定向 Go 测试通过；仪表盘与状态选择相关前端测试 10/10 通过，覆盖显式采集、跨浏览器隔离、7 日趋势、掉线优先和只向显式 AI 复盘发送当前浏览器的已验证证据。

## 完成门槛

只有以下全部成立才能把本 Goal 标记为完成：

1. 登录：多个浏览器、多平台账号、弹窗关闭、过期会话、掉线与恢复测试通过；真实浏览器1登录同步在秒级完成。
2. 知识库：真实大批量导入、混合失败、检索引用和创作调用均有证据。
3. 创作：趋势 → 文案 → 分镜脚本 → 内容库在 Electron 中完成一条 E2E。
4. 发布：10 平台 × 16 形态均有契约测试；自动档位必须有当前 EgoLite 真实 prepare、一次提交和后台核验证据，未取得证据的平台保持人工接管/未验证。
5. 数据：9 个内容平台均有成功的 7 天契约 fixture；真实账号至少完成安全只读 dry-run，范围不完整时不得落库。
6. 资源：Electron 启动不创建 Space；登录批次最多 3 标签；空闲时不保留 Agent 控制；不启动旧 Chromium。
7. 门禁：`go test ./...`、前端测试、Electron 测试、前端构建和相关 E2E 真实执行通过。

真实平台被登录、风控或页面变化阻塞时，记录“未验证”并继续其他本地工作；不得编造成功，也不得为了验收重复最终点击。
