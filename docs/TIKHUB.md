# TikHub：赛道监控 / 博主监控 / 搜索

在 Alpha-Nexus 用 **Go HTTP 客户端** 调 `https://api.tikhub.io`（不绑 Python SDK）。

## 配置 Key

推荐在桌面端进入“设置 → TikHub 数据 API”：

1. 选择“中国大陆加速”或“海外 / 全球”。
2. 粘贴 TikHub API Key 并保存。Key 只写入本机，服务端响应不会回显。
3. 点击“测试并激活”，确认后调用 `GET /api/v1/tikhub/user/get_user_info`。
4. 只有测试成功的配置才会进入 `active`，真实抓取工具会拒绝使用未激活配置。

命令行仍可用于诊断或兼容旧流程：

```bash
export TIKHUB_API_KEY=你的key
# 或写入 data-dir
alpha-nexus tikhub config set-key --key 你的key
# → ~/.alpha-nexus/tikhub_api_key

alpha-nexus tikhub config status   # 检查是否已配置（脱敏）
```

## 产品边界

- 趋势雷达只服务三件事：博主监控、赛道监控、常用搜索词。
- 不内置行业赛道。每位用户在 UI 中自行创建赛道，并配置名称、说明、搜索词、监测平台与启停状态；删除赛道不会删除已经保存的历史抓取记录。
- 日榜和评论不进入趋势雷达与精选 API 目录。
- 爆款和选题是监控信号的下游分析动作，不再作为独立榜单页。
- 浏览、编辑配置和读取历史均不联网；每次真实 TikHub 调用仍需用户明确确认。

## 本地趋势雷达

前端“趋势雷达”加载时只读取：

```text
GET /api/trend-radar/summary
```

该接口聚合已保存的关键词搜索快照、博主作品快照和本地监控配置，不调用 TikHub。

- 抖音、小红书：支持赛道搜索和博主资料/作品监控。
- YouTube、X：支持赛道搜索与内容详情；YouTube 另提供官方字幕读取。
- 视频号、快手、公众号：支持赛道搜索；固定目录也保留资料/作品接口供手动调用。
- 数据 API 固定平台为抖音、小红书、视频号、快手、公众号、YouTube、X；文章平台只保留公众号。
- 搜索结果按赛道和搜索词归档；重复出现次数、首次发现、最近发现和可用互动指标由本地分析层计算。

配置 API：

```text
GET    /api/trend-radar/config
PUT    /api/trend-radar/config
DELETE /api/trend-radar/creators/{platform}/{user_id}
```

## 固定接口目录

设置页的接口目录来自 `internal/tikhub/endpoints.go`，只保留趋势雷达的常用主链，不镜像 TikHub 的完整接口数量：

| 平台 | 精选主链 |
|------|----------|
| 抖音 | 视频关键词搜索 V2、作品详情、博主资料、博主作品 |
| 小红书 | 笔记搜索、图文/视频详情、博主资料、博主笔记 |
| 视频号 | 微信搜一搜视频垂类、视频详情、视频号资料、博主作品 |
| 快手 | 综合搜索、作品详情、博主资料、博主作品 |
| 公众号 | 微信搜一搜文章垂类、文章详情、公众号资料、文章列表 |
| YouTube | 结构化综合搜索 V2、视频详情、视频字幕 |
| X | Top 关键词搜索、帖子详情 |

共 26 个固定接口。抖音关键词搜索使用官方当前的 `POST /api/v1/douyin/search/fetch_video_search_v2`，YouTube 使用结构化 `GET /api/v1/youtube/web_v2/get_general_search_v2`，X 使用 `GET /api/v1/twitter/web/fetch_search_timeline`。榜单、评论、回复分页、冷门垂类和重复版本不进入 Nexus 精选目录。

## 接口调用工作台

“设置 → TikHub 数据 API → 接口调用工作台”支持：

1. 从精选白名单选择接口，按固定参数 Schema 填写；
2. 每次真实请求前显式确认，未知接口、未知参数和覆盖固定参数都会拒绝；
3. 调用前在 SQLite 创建 `running` 记录；
4. 成功响应完整写入权限 `0600` 的原始 JSON；
5. SQLite 更新状态、供应商代码、结果规模、响应字节、耗时和原始文件路径；
6. 在“长期记录”中回读原始响应。

YouTube 字幕先读取语言列表，再按 `video_id + language_code` 获取 `txt`。完整 JSON 与同名 Markdown 都以 `0600` 保存在当天调用目录；趋势雷达优先显示已保存的官方字幕，否则显示搜索接口返回的平台描述。抖音、X 当前显示平台原文；项目尚无已验证 ASR/Whisper 管线，因此不会把标题冒充视频转写。

本地 API：

```text
GET  /api/data-api/config
POST /api/data-api/calls
GET  /api/data-api/calls
GET  /api/data-api/calls/{id}/response
```

微信相关接口的调用超时为 60 秒。Go 客户端使用 `json.Decoder.UseNumber()`，避免 64 位 ID 在保存前经过浮点数而丢失精度；前端涉及作品或账号 ID 时一律使用字符串字段。

## 博主监控

趋势雷达 UI 支持抖音 `sec_user_id` 与小红书 `user_id`。加入名单只写本地；点击“刷新快照”并确认后，分别调用博主资料和博主作品两个固定接口，完整响应进入统一调用记录。

```bash
alpha-nexus tikhub user add --platform xhs --user-id <uid> --name 竞品A
alpha-nexus tikhub user list
alpha-nexus tikhub user remove --user-id <uid>
alpha-nexus tikhub user check --user-id <uid>
alpha-nexus tikhub user check-all
# 名单：tikhub/watchlist.json
```

旧 CLI 的 `user check` 路径仍优先小红书；桌面趋势雷达已改用统一 Data API 调用链支持抖音和小红书。

## 搜索抓取

```bash
alpha-nexus tikhub search xhs --keyword 奢侈品奥莱 --pages 2
alpha-nexus tikhub search douyin --keyword AI智能体 --count 10
# 结果：tikhub/search/YYYY-MM-DD/*.json
```

## 与 CDP 数据回收关系

| 路径 | 用途 |
|------|------|
| `ops collect` / `xhs collect` | **自己账号** 创作者后台（需登录 CDP；见 xhs-comment-claw SOP） |
| `tikhub *` | **公开/第三方 API** 竞品用户、关键词搜索（需 TikHub key） |

两者互补：自家数据走 CDP，公开赛道与竞品监控走 TikHub。

## 数据目录

```text
~/.alpha-nexus/
  tikhub_api_key
  tikhub/
    trend_radar.json
    calls/YYYY-MM-DD/{call_id}_{endpoint_id}.json
    watchlist.json
```
