# 飞书同步（Azimuth GTM Harness）

本地 SQLite 是**唯一真源**（`AGENTS.md` 硬约束）；飞书 Base 是每个板块的**共享工作台镜像**。
同步方向：**本地 → 飞书**，单向；飞书里的改动不会覆盖本地。

## 绑定

- Base：`Azimuth GTM Harness`
- 地址：https://my.feishu.cn/base/IAtCbesjhatm9MsajvYcaBbynyg
- 本地配置：`~/.alpha-nexus-egolite/feishu.json`（只存 app_token / table_id，不存密钥）
- 身份：`lark-cli` 的 bot 身份；token 由 CLI 自管，不进入前端、数据库或日志

## 表与模块（11 个同步表 + 1 个人工表）

| 模块 id | 表名 | table_id | 同步来源 | 上限 |
|---|---|---|---|---|
| accounts | 账号 | `tblVp3v17aSZLJIB` | `accounts` | 300 |
| contents | 内容 | `tbl1rNZft8Oxsrzg` | `contents` | 300 |
| knowledge | 知识库 | `tblc1TzsH6Idr80R` | `knowledge_documents` | 300 |
| radar | 趋势雷达 | `tblaTlBhxsnaC9c8` | `radar_works` | 300 |
| memory | 认知记忆 | `tbl0Zh0XRP25T7RO` | `bot-memory/<bot>.md` | 有记忆的 Bot |
| calendar | 内容日历 | `tbl8ZBEOQCQpLwfa` | `contents.planned_at` 非空 | 300 |
| metrics | 账号数据 | `tblW1MGaTd5mPnrj` | `creator_metric_snapshots` | 300 |
| captures | 抓取采集 | `tblhbRTm4xzigcv4` | `radar_capture_runs` | 200 |
| bots | Bot 团队 | `tbl6gyWNQ1rk6Thv` | `agent_bots` | 全部 |
| publish | 发布记录 | `tblmGiRnSywaUTli` | `publish_records` | 300 |
| logs | 运行日志 | `tblB6TyIZw6LQNoL` | `operation_events` | 200 |
| — | GTM 任务 | `tblXRJUDtfuEaqyu` | **人工维护**，不同步 | — |

`GTM 任务` 是给人做计划的表（阶段 / 截止日期 / 市场 / 渠道 / 负责人 / 目标值…），
没有对应的本地实体，因此**不做自动同步**，也不会被同步任务写入或清空。

## 去重与增量

`feishu_sync(module, local_key, table_id, record_id, content_hash, synced_at)` 记录每条本地行对应的飞书记录。

- 内容 hash 不变 → `skipped`，不发请求
- 内容变了 → `batch_update` 原地更新，计入 `updated`
- 本地新增 → `batch_create`，计入 `created`
- 飞书返回的记录数少于提交数 → 整批报错且**不写映射**，下次重试（宁重勿丢）

每批最多 20 条、单次调用 45s 超时。真实两轮验证：第一轮 `updated 854 / created 14`，第二轮 `updated 0 / skipped 658`。

## 接口

- `GET /api/feishu/status`
  - `configured` / `url` / `syncing` / `last_results` / `last_error`
  - 每模块：`table_id` + `synced` 已同步条数
- `POST /api/feishu/sync`
  - body：`{"module":"all"}` 或单个模块 id
  - 返回 202 并立即返回状态；同步在后台跑，同一时间只允许一个任务

## 设置页

设置 → 集成 → **飞书同步**（`/SM/settings/feishu`、`/bot/settings/feishu`）：

- 打开表 / 立即同步
- 每个模块一行：名称、table_id、已同步条数
- 失败原因原样显示（如 `field validation failed`、`lark-cli not found`）

## 真实核对（2026-11-20）

执行 `module=all` 后逐表核对 Feishu `total` 与本地条数，11 张表全部一致：
账号 27 / 内容 3 / 知识库 300 / 趋势雷达 300 / 认知记忆 3 / 内容日历 2 /
账号数据 10 / 抓取采集 5 / Bot 团队 8 / 发布记录 0 / 运行日志 200。

`趋势雷达.发布时间` 已接真实数据：`radar_works.published_at` 回填后 502/558 条有值，
飞书表里可直接看到 `2026-10-31T14:41:53Z` 这类发布时间；缺的 10% 保持空，不用抓取时间顶替。

## 已知缺口

- 飞书编辑不回写本地；需要改数据请走本地 UI/CLI。
- 同步状态（`last_results` / `syncing`）存在进程内存里，重启后丢失；只影响设置页显示，不影响数据。

## 边界

- 不改本地数据；飞书不可用时本地功能不受影响
- 不读取、导出或记录 Cookie / app secret / access token
- 同步失败不自动重试；由用户点击或下一次手动同步恢复
- 新增字段需同时改本地映射与飞书表结构
