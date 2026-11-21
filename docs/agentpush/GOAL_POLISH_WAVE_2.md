# 深度补强 · Wave 2

分支：`feat/polish-wave-1`（承接 Wave 1）

## 0. 实测基线（本轮盘点）

| 项 | 当前值 |
| --- | --- |
| 账号 | 24 个：8 已登录 / 5 已登出 / 11 待确认 |
| 平台发布能力 | manual_handoff 14 / unavailable 12 / browser_automation 9 / api 1 |
| 雷达信号 | **571 条**：xhs 173 / douyin 188 / x 101 / **wechat_video 60** / youtube 40 / **kuaishou 9** |
| 雷达博主 | 458 个 |
| 指标快照 | 3 个账号有数（CSDN 420 / YouTube 3 / xhs 0） |
| 发布记录 | 0 行 |
| 内容 | 3 条，全是草稿 |
| 知识库 | 4 个，其中 2 个品牌库为空 |
| 前端测试 | 374 项，全通过 |
| Go 测试 | 全通过；`go vet` 全仓干净 |

## 1. 本轮已修（有实测证据）

### 1.1 雷达平台覆盖：快手与视频号一条都进不来

两个平台抓取都显示 `succeeded`、快照文件也落盘了，但雷达信号里零条。根因是字段名不同：

| 平台 | 问题 | 修法 | 实测 |
| --- | --- | --- | --- |
| 快手 | 作品文案在 `caption`，没有 `title`/`desc`，被当成「没有标题的杂项」丢弃 | 标题与正文候选都加 `caption` | 0 → **9 条**，赞/藏正常 |
| 视频号 | 作品标识叫 `docID`，不在 identity 候选里；标题还带 `<em class="highlight">` | 补 `docID`/`hashDocID`，并在 `radarString` 统一出口剥离高亮标记 | 0 → **60 条**，标题干净 |

### 1.2 截图清理是空操作

`pruneDirectoryContents` 只删顶层文件、跳过子目录，而截图实际写在 `publish-inspections/`、`desktop/`、`publish/` 里。于是「清理失败截图」永远回收 0 字节，而用量统计是递归算的——用户看到占用却清不掉。改为递归 + 清空目录，符号链接仍不跟随。

### 1.3 删除账号会清错 profile 目录

`internal/ui` 与 `internal/cli` 各有一份算法不同的 `hashProfileID`（Knuth 乘法 vs 2654435761 混合）。同一账号算出两个目录名（id=1 → `pc3383c52` vs `p5afad963`），CLI 删除时清不到真实目录，登录态残留。改为 CLI 复用 `ui.ProfileDirName`，并加样本值测试固定算法。

### 1.4 工程质量

- **补最小 CI**：仓库此前零 workflow，编译/vet/测试全靠手动。
- **`go vet` 全仓干净**：修了 agenthub 测试返回含 `sync.Once` 结构体副本的问题。
- **裸跑测试**：`agent-offline.test.mjs` 的 165 条断言全在模块顶层，任何失败只显示「1 个测试失败」且无法定位。已包进 `test()`。

## 1.5 本轮第二批修复（审计驱动，全部有实测）

### 后台 worker 永远停在首次安装版本

`InstallWorker` 只要 plist 存在且 launchctl 已加载就 `return nil`，永不替换可执行文件。实测线上 worker 是 **11-04 的构建**，`strings` 里连 `creators_due`（自动归档标识）都没有——界面上那个「每 24 小时自动更新」开关是**假的**。

改为比对大小与修改时间，不一致就 bootout → 覆盖 → 重新加载；并在服务启动时也对齐一次（原先只在启用定时计划时才安装）。**实测：重启后 worker 从 11-04 变成 11-21，且已包含 `creators_due`。**

### 付费调用 93% 是重复

实测：`douyin.creator_posts` **124 次调用只有 8 组唯一参数**（重复 116 次，93.5%）；`douyin.video_search` 47 次只有 12 组唯一。合计 198 次付费调用里只有 47 组唯一参数。

在 `executeDataAPICall` 入口按「端点 + 规范化参数」查进程内缓存，命中直接回放上次快照，不创建调用记录、不花钱。TTL 按能力分级：搜索 6h / 作品 1h / 资料与字幕 24h / 详情 12h；未知能力不复用。

### 终态失败也在重试

此前所有 `http_*` 失败都重试，单页最多 3 次重试 = **4 次付费调用**。401（凭证错）、403（被禁）、404（资源不存在）都是终态，重试只会再付一次钱。收窄到 400 / 429 / 5xx。

### 刷新博主必失败

抖音只认 `sec_user_id`、小红书只认 `user_id`，代码同时传两个 → 后端判定「不支持的参数」直接拒绝。抓取采集页的「刷新博主」每次必失败。

### 其他

- **日志分节永远为空**：`useSWR` 的 key 绑在 `runtime` 上但列表渲染在 `logs` 里，进 logs 时 key 为 null 请求根本不发（接口本来有 80 条事件）。顺带修了 `level === 'ERROR'` 大小写不匹配导致错误事件不标红。
- **并发校准不落盘**：只改内存 `s.cfg`，不写 `config.json`；`toolRuntime` 还持有启动时的值拷贝。实测校准出 measured=12/suggested=3，现在能真正写盘。
- **内置主 Agent 可被删除**：删掉后顶部大卡失去落点，下次启动又被插回、期间会话被解绑。现在返回 403。
- **推荐卡创建的 Bot 建不出会话**：创建时不带 model，服务端只有 Bot 有 model 时才带进会话。实测对照：带 model 返回 201，不带返回 400 `AGENT_MODEL_NOT_VERIFIED`。
- **「置顶」伪分区可被重命名**：会把置顶 Bot 的 `group_name` 改写成「置顶」，凭空造出一个真实部门。
- **内容库接入 AI 辅助**：后端 `/api/content-assist` 一直活着但只有数据复盘页在用，编辑器写文案时没入口。现在有「生成标题/润色正文/检查问题」三个动作。
- **删除零引用组件约 470 行**：TemplatesView、CodingAgentView、FleetView，连同只测它们的断言。

## 1.6 Bot 管理能力补强

对照参考实现的功能清单，补齐了侧栏缺失的常规操作：

| 操作 | 之前 | 现在 |
| --- | --- | --- |
| 新建 Bot | 只有列表为空时才有推荐卡，非空后无入口 | 收件箱下方常驻「新建 Bot」，直达设置 |
| 编辑资料 | 只能自己找到设置页 | 右键「编辑资料」直达 Bot 团队分节 |
| 复制人格 | 无 | 右键「复制人格」，继承 role/instructions/model/avatar/kickoff/group |
| 移到分组 | 无 | 右键列出其他分组，点一下移动 |
| 删除 Bot | 只能进设置页 | 右键「删除 Bot」，二次确认；内置主 Agent 前端先挡一次 |

复制刻意不继承 `is_primary`（主 Agent 只能有一个）与 `pinned`（副本不该插到顶部）。删除只删人格，会话保留。

## 2. 审计发现但本轮不动（附理由）

### 2.1 前端测试 81% 只断言源码字符串

366 个 test 块里 299 个是 `assert.match(源码, /正则/)`，真正 import 并执行的只有 67 个；`src/api.ts`（4344 行、288 个导出函数）零测试 import。最脆的例子是 15 处 `[\s\S]{n,m}` 字符距离断言，函数体多一行注释就挂。

**不动理由**：这是全仓的测试范式，不是局部问题。改造等于重写测试套件，风险远大于收益。建议逐步替换：新功能写真测试，老测试保留。

### 2.2 测试覆盖死代码（3821 行组件）

`AccountsView`(2558)、`TerminalView`(342)、`BatchView`(332)、`ContentView`(322)、`TemplatesView`(140)、`FleetView`(122) 都是 0 引用，但有 9 个测试文件读它们。

**不动理由**：这些测试是混合断言——同一文件里既有对死组件的断言，也有对 `api.ts` 等活代码的有效覆盖。整文件删除会丢真实覆盖；逐条拆需要人工判断归属，收益有限。留着无害。

### 2.3 `SettingsView.tsx` 3265 行只剩一段活的

只有 `LLMConfigPanel` 仍被 `AlphaAgentSettingsSection` 使用，其余约 3000 行是死的。

**不动理由**：拆出来需要把 `LLMConfigPanel` 挪到独立文件，涉及 14 个测试文件的引用。可以单独做，但不是本轮优先项。

### 2.4 `internal/ui/dist` 5.9MB 构建产物入库

**不改理由**：`//go:embed all:dist` 是编译前提，不入库新克隆就编不过。这是有意设计。唯一代价是每次前端重建产生 diff 噪声。

### 2.5 legacy_chromium 构建下 306 个测试永不执行

`publishverify` 整包在默认构建里不存在。这意味着真实发布点击链路无自动化验收。

**不改理由**：这是项目的有意隔离（默认构建 fail-closed）。要补需要真实浏览器环境，属长期项。

## 2.6 发布链路为什么 0 成功（实测机制）

审计确认：`egopublish.Capability.SubmitVerified` 在全仓**没有任何一处被设为 true**，`manifest_test.go` 甚至断言它必须为 false。这是 fail-closed 的安全设计——没有经过真实创作者后台验收的平台，不允许自动点击最终提交按钮。

后果链：
1. UI 上「确认并提交一次」按钮可以点；
2. 后端 `publish_plan_handlers.go` 读到 `!SubmitVerifiedFor(...)`，直接落 `EGO_PUBLISH_SUBMIT_UNVERIFIED`；
3. 因此 `publish_records` 结构上不可能非空——59 个计划 0 成功不是"写错表"，是这条门禁在起作用。

**问题在体验，不在逻辑**：按钮可点、必然失败、失败原因对用户不可见。建议（未做）：
- 发布页对未验收平台直接禁用提交按钮，并说明「该平台尚未通过真实后台验收，本产品不会代为点击发布」；
- 或保留按钮但预检时明确提示，而不是让用户走完整个流程才在最后一步失败。

## 3. 剩余可做（按投入产出排序）

| 优先级 | 项 | 说明 |
| --- | --- | --- |
| P1 | 品牌知识库灌文档 | AlphaFDE / Alpha Game 两个库是空的，有素材就能导 |
| P1 | 内容库缺 AI 辅助入口 | 后端 `POST /api/content-assist` 与 `api.assistContent` 都是活的，但只有 `XHSDataDashboardView` 在用（数据复盘）。内容库编辑器（`LocalContentView`）写文案时**没有调用入口**——这个能力被留在零引用的 `ContentView.tsx` 里。 |
| P1 | 内容阶段：先建设计再动代码 | `internal/contentstage` 的纯函数已写好并有测试，但接进产品需要**阶段事件表**——一个内容在每个阶段各有自己的排期与完成时间，塞进 `contents` 单表的 `planned_at` 表达不了。本轮试做过简化版，因为存不下日期导致「被拒绝的推进仍然改动了阶段」，已整块回滚，不留半成品。 |
| P2 | DashboardView 拆分 | 3733 行，改一处容易崩 |
| P2 | 账号登录规则补全 | 11 个待确认平台需逐个写 `platformReviewedLoginRule` |
| P3 | 发布链路真实验收 | **实测确认：59 个发布计划里 0 个成功**（45 manual_takeover / 9 failed / 3 frozen / 1 login_required / 1 result_unknown），`publish_content_records` 0 行。所以内容库显示「未发布」是**正确的**，不是 bug。要跑通需要真实账号逐个验收。 |
| P3 | 测试范式改造 | 逐步把源码正则换成真实执行 |

## 4. 验收口径

- `go build ./...`、`go vet ./internal/...`、`go test -p 1 ./internal/...` 全绿
- `node --test ui-web/test/*.test.mjs` 全通过
- 每项修复附实测数据（改动前后的真实数字）
- 不新增 Cookie 读写路径；不新增定时发布
