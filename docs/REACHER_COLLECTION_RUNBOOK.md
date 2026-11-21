# 采集与内容镜像 · 调研 reacher 使用手册

这份手册给「调研 reacher」用：怎么跑采集、怎么核对结果、怎么确认内容能进飞书多维表格。
不需要读源码，照着命令跑即可。

---

## 0. 环境

| 项 | 值 |
| --- | --- |
| 程序 | `/Users/albert/Documents/project/App/Alpha-Nexus-egolite/bin/alpha-nexus-egolite` |
| 数据目录 | `~/.alpha-nexus-egolite`（默认，无需指定） |
| 当前版本 | 0.2.0 |

建议先设一个别名，后面命令更短：

```bash
ANX=/Users/albert/Documents/project/App/Alpha-Nexus-egolite/bin/alpha-nexus-egolite
```

**先读，再写。** 第 1 节全部是只读的，先跑通再碰采集。

---

## 1. 只读：先确认环境和数据现状

```bash
$ANX tools call account.list --args '{}'
```
→ `ok:true`，`data.accounts` 是账号列表，看 `id` / `platform` / `login_state`。
只有 `login_state=logged_in` 的账号才能采到数据。

```bash
$ANX tools call social.metrics.overview --args '{"history_days":7}'
```
→ 本地已存的指标概览。`collections` 里每条账号带 `state` 和 `error_code`，
这是判断「上次为什么没采到」的第一手依据。

```bash
$ANX feishu status
```
→ 看 `cli`（飞书 CLI 身份）和 `export`（多维表格绑定状态）。
现在 `configured` 应该是 `false` —— 飞书授权还没走完，这是已知状态，不是故障。

---

## 2. 采集（会打开 EgoLite 浏览器）

### 单账号

```bash
$ANX tools call social.metrics.collect --args '{"account_id":10}'
```

### 批量（最多 100 个）

```bash
$ANX tools call social.metrics.collect-batch --args '{"account_ids":[10,6,3]}'
```

**行为说明（都是设计如此，不是 bug）：**

- 同一身份的空间**严格串行**，并发固定 1；批量会排队跑，不会同时开多个浏览器。
- 单账号有 3 分钟上限，超时会记 `EGO_METRICS_TIMEOUT` 并跳过，不影响后面的账号。
- 只读页面、只记录数据，**不点赞、不评论、不关注、不转发、不发布**。
- 平台没适配的会记 `manual_takeover` / `EGO_METRICS_MANUAL_HANDOFF`，这是「需要人工看」，不是你操作错了。
- 登录失效的账号会记 `ACCOUNT_NOT_LOGGED_IN` 并跳过；不要尝试重新登录，交回开发处理。

### 采集完怎么确认真的落库了

最直接的办法是用镜像预览反查（不会写飞书）：

```bash
$ANX feishu export --dry-run --days 1
```

刚采到的账号会出现在 `metrics` 里，`采集日期` 是今天。没出现就是没落库。

---

## 3. 内容进飞书多维表格

镜像逻辑：**采集任务跑完会自动推一次**，不需要你手动触发。

手动补推：

```bash
# 先看会写什么，不发送任何东西（安全）
$ANX feishu export --dry-run --days 7

# 真正写（需要飞书授权完成，且已绑定表格）
$ANX feishu export --days 7
```

两张表：

- **作品库**：一条作品一行。作品键、平台、账号、标题、发布时间、链接、播放、点赞、评论、转发、收藏、采集日期。
- **账号日报**：一个账号一天一行。账号日期、平台、账号、采集日期、统计天数、范围已验证、粉丝、播放、点赞、评论、转发、收藏、作品数。

同一个作品 / 同一天同一账号重复推，**只更新不重复建行**。

没授权、没绑定时，`feishu export` 会直接返回 `skipped: "disabled"` 或 `"not_configured"`，**不会报错、不会写任何东西**。

---

## 4. 已知现状（2026-11-12 实测，别当成 bug）

| 事实 | 数值 |
| --- | --- |
| 本地账号 | 27 个 |
| 账号日指标 | 5 条（2026-11-12 csdn / xhs 各一条，另有 3 条历史行） |
| 作品快照 | 0 条 |
| 绑定的空间 | 171（2026-11-12 重建，旧空间 50 已不存在） |
| 飞书表格绑定 | 未完成（等授权） |

**2026-11-12 修正**：10-25、11-04 两条 csdn 历史行是旧逻辑写的累计数，已把它们的「范围已验证」改成 false（数据本身没删，备份在 `app.db.bak-before-scope-fix-20261112`）。

**这两个条数是会变的**：日指标按保留期自动清理旧行，作品快照也会随采集增减。所以第 5 条不要拿固定数字当标准，也不要用 `social.metrics.overview` 的 `collections` 去对——它按账号只留最新一条，不是全量行数。要对就查本地表的行数：

```bash
# 应用开着时，sqlite3 直接打开原库可能报 "unable to open database file"（库正被应用占用，
# WAL 还没合并）。把库和 WAL 一起复制到临时目录再查，不要动原库：
tmp=$(mktemp -d)
cp "$HOME/.alpha-nexus-egolite/app.db" "$HOME/.alpha-nexus-egolite/app.db-wal" \
   "$HOME/.alpha-nexus-egolite/app.db-shm" "$tmp"/ 2>/dev/null
sqlite3 -readonly "$tmp/app.db" \
  "select (select count(*) from creator_metric_snapshots) as metric_rows,
          (select count(*) from creator_post_metric_snapshots) as post_rows;"
```

如果只是想看当前状态，也可以直接用 `$ANX tools call social.metrics.overview`（走应用自己的只读查询，不受占用影响）。

两个数分别和第 5 条返回的 `metric_rows` / `post_rows` 对上，就是全量一致。

另外：**2026-11-08 那次定时采集 10 个账号全部跳过、0 条入库**，原因是那批账号当时都不可采（登录态/平台适配），已单独排查，不是这次的改动引入的。

**如果采集返回 `EGO_SPACE_USER_CONTROLLED`**：说明那个身份的空间正被人手动接管，程序会安全停下，这是对的。原样记录回报，不要自己重试、也不要改任何状态。

---

## 5. 测试清单（请照这个跑，逐条回报）

只读部分随时可跑；采集部分**先确认没有别人在用这个应用**（问一句开发即可）。

| # | 命令 | 期望 |
| --- | --- | --- |
| 1 | `$ANX version` | 打印版本与构建信息，退出码 0 |
| 2 | `$ANX tools call account.list --args '{}'` | `ok:true`，能列出 27 个账号 |
| 3 | `$ANX tools call social.metrics.overview --args '{"history_days":7}'` | `ok:true`，`window_days` 为 7，有 `collections` 明细 |
| 4 | `$ANX feishu status` | `configured:false`，`cli.brand` 为 `feishu` |
| 5 | `$ANX feishu export --dry-run --days 120` | `post_rows` / `metric_rows` 与本地表的全量行数一致（不是固定数字，也不要用 `overview.collections` 对），`posts`/`metrics` 字段名是中文表头 |
| 6 | `$ANX feishu export --days 120` | 返回 `skipped:"disabled"`；飞书侧**不应有任何新表格** |
| 7 | 选一个 `login_state=logged_in` 的账号，`$ANX tools call social.metrics.collect --args '{"account_id":<id>}'` | `ok:true`，`data.snapshot.snapshot_date` 是今天；`metric_scope` 是 `window` 才代表数字对应「近 7 日」，并核对 `metric_evidence.*.value_text` 与页面原文一致；若是 `EGO_SPACE_USER_CONTROLLED` 等明确错误码，也算这项合格（要记录原文） |
| 8 | 紧接着 `$ANX feishu export --dry-run --days 1` | 第 7 步采到的账号出现在 `metrics` 里，`采集日期` 是今天，数字与第 7 步返回的 `snapshot` 一致 |
| 9 | `$ANX tools call social.metrics.collect-batch --args '{"account_ids":[<id1>,<id2>]}'` | 两个账号串行执行，各自有独立结果，不互相打断；每个账号的返回里都有和单账号相同的 `snapshot` 字段 |

**重点看第 8 条**：它是「采集 → 落库 → 内容可镜像」的闭环验证。第 8 条过了，说明整条链路是通的。

**怎么判断数字是不是近 7 日**（2026-11-12 修正）：`range_verified` 只看页面上有没有「近 7 日」这个字样是不够的——CSDN 首页就同时挂着「近 7 日创作表现」和账号累计卡（收藏数 6304）。所以采集结果里有 `metric_scope` 和 `scope_source`：

- `window`：数字已经绑定到 7 日证据，可以当周增量用。`scope_source` 说明绑定方式：
  - `seven_day_card`：CSDN 的「近7日创作表现」卡（同时带「新增…」）；
  - `page_period_panel`：同一个容器里既有「近7日」又有至少两个指标；
  - `selected_seven_day_control`：页面上可见且被选中的「近7日」tab（小红书就是这种）。
- `page`：整页扫出来的，没有绑定到 7 日容器。CSDN 遇到 `page` 直接判未验证、不入库（宁可不采，也不把累计数写成 7 日）；其他平台只有在专用分析页上才可能接受，回报时按 `page` 标注，不要当周增量。

每个数字都带原文证据，在返回的 `metric_evidence` 里：

```json
"views": {"label": "新增阅读", "value_text": "503", "context": "新增阅读 503"}
```

`snapshot.raw_json` 里也能回查同一份证据（`metric_evidence` / `scoped_evidence` / `scope_source`）。如果 `value_text` 和页面原文对不上，就算这项不合格。

CSDN 现在只采「新增阅读」（7 日阅读），`新增互动` 是点赞+评论+收藏合计数，单独放在 `scoped_engagement`，不会再拆成三个假数字。**页面没有这个数时不出现 `scoped_engagement` 字段；出现且为 `0` 表示页面写的就是 0**。单账号和批量现在用同一套字段，不会一边缺字段、一边给 `null`。

采集失败时（例如 `EGO_METRICS_COLLECTION_FAILED`），返回里会带 `account_id`、`stage` 和 `run_error`（`stage` 为 `RUNTIME_EXECUTION` 表示页面程序没跑完，`SPACE_BINDING` 表示空间绑定/重建失败）：`run_error` 是稳定错误码（如 `EGO_RUNTIME_EXECUTION_FAILED`）或数据库竞争标记（`SQLITE_BUSY` / `SQLITE_FULL`），不会带页面原文或本地路径。看到 `SQLITE_BUSY` 说明是应用同时写库，隔一会儿重跑即可，不要改数据。

---

## 6. 边界（不要做）

- 不要发布、不要点赞评论关注转发、不要重新登录、不要退出登录。
- 不要跑 `radar.creator.archive`、`tikhub.*` 这类**会产生费用**的命令；要用先问编导。
- 不要改 `~/.alpha-nexus-egolite` 下的文件，也不要删数据。
- 不要碰飞书授权；授权由阿腾本人完成。
- 遇到 `EGO_SPACE_*` 开头的错误码，原样记录下来回报，不要自己重试或重建空间。

---

## 7. 回报格式

每条按这个写，别写「测试通过」这种没有依据的结论：

```
#N 命令：<原样>
    结果：<关键字段原文，或失败时的错误码>
    判断：符合预期 / 不符合，差在哪
```

第 7、8 条附上采集返回的 `state` / `error_code` 原文。
