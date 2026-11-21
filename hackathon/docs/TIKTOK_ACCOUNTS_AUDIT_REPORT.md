# 浙江冠英（KOOYING）TikTok 账号矩阵全景核查与全量作品数据分析报告

> 核查时间：2026-11-21｜数据来源：`ego-browser` 实时主页采集 + `yt-dlp` 逐条元数据抓取（原始数据见 [`tiktok_accounts_data.json`](file:///Users/albert/Documents/project/bisai/intercity-hack-huzhou/hackathon/docs/tiktok_accounts_data.json)）

## 1. 结论先行

- **实锤确认 4 个浙江冠英 / 冠英外贸关联 TikTok 官方账号**，累计发布 **49 条作品**（46 条视频 + 3 条图文），合计 **2,664,029 次播放、44,279 次点赞、1,994 条评论、5,959 次转发**。

- **已排查并排除 2 个同名个人账号**：[`@kooying29`](https://www.tiktok.com/@kooying29)（泰国个人家庭日常号，15 条）与 [`@kooying`](https://www.tiktok.com/@kooying)（二次元个人空号，0 条），确认与浙江冠英刹车片业务无关。

- **最大增长漏水口**：老主力账号 [`@kooying.autoparts`](https://www.tiktok.com/@kooying.autoparts) 曾跑出 **266.1 万总播放**、单条最高 **130 万播放 / 1,936 条评论 / 5,600 次转发** 的现象级爆款，却自 **2023-10-12 断更超 3 年**，且主页 **无简介、无官网、无 WhatsApp 号**；后续团队又重开 3 个新号冷启动，流量反差达 **1,600 倍**。

## 2. 冠英 TikTok 账号矩阵总览与主页截图

| 序号 | 账号 Handle | 主页名称 | 归属角色 | 粉丝 / 获赞 | 作品数 | 实测总播放 | 活跃周期 | 主页截图 |
|---:|---|---|---|---|---:|---:|---|---|
| 1 | [`@kooying.brake.fact`](https://www.tiktok.com/@kooying.brake.fact) | Kooying brake factory boss | 老板娘 Sophia 主号（含 WhatsApp +86 159-5711-8669） | 73 粉 / 65 赞 | 13 | 1,618 | 2025-10-13 ~ 2026-02-08 | [tk-profile-1-kooying.brake.fact.png](file:///Users/albert/Documents/project/shots/tk-profile-1-kooying.brake.fact.png) |
| 2 | [`@kooying_judy26`](https://www.tiktok.com/@kooying_judy26) | kooying_judy26 | 外贸业务 / Judy 号（含官网 www.kooying.com） | 3 粉 / 17 赞 | 16 | 1,283 | 2026-01-30 ~ 2026-07-21 | [tk-profile-2-kooying_judy26.png](file:///Users/albert/Documents/project/shots/tk-profile-2-kooying_judy26.png) |
| 3 | [`@kooying.autoparts`](https://www.tiktok.com/@kooying.autoparts) | KOOYING | 冠英早期主力英文官方号（KOOYING 吉祥物 Logo） | 2423 粉 / 48.3K 赞 | 19 | 2,661,068 | 2023-05-18 ~ 2023-10-12 | [tk-profile-3-kooying.autoparts.png](file:///Users/albert/Documents/project/shots/tk-profile-3-kooying.autoparts.png) |
| 4 | [`@kooying.brake.lin`](https://www.tiktok.com/@kooying.brake.lin) | Kooying brake li ning | 老板娘红标备用号（含 +86 159-5711-8669） | 0 粉 / 1 赞 | 1 | 60 | 2024-08-06 ~ 2024-08-06 | [tk-profile-4-kooying.brake.lin.png](file:///Users/albert/Documents/project/shots/tk-profile-4-kooying.brake.lin.png) |

### 已核查排除的同名个人号（非冠英业务资产）

- [`@kooying29`](https://www.tiktok.com/@kooying29)（主页名 `kooying`）：头像及 15 条作品均为泰文个人亲子/寺庙生活照片合集，非刹车片外贸号。

- [`@kooying`](https://www.tiktok.com/@kooying)（主页名 `ying`）：二次元动漫头像、2 粉丝、0 作品，非企业号。


## 3. 各账号核心指标统计对比

| 账号 | 可统计视频数 | 总播放 | 中位数播放 | 最高单条播放 | 总点赞 | 总评论 | 总转发 | 中位时长(s) | 主页 Bio 转化链路 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| `@kooying.brake.fact` | 10 / 13 | 1,618 | 162 | 248 | 61 | 3 | 0 | 33.5s | 20 year experience of developing and producing brake pads and brake linings.wish to cooperate with you.Sophia +8615957118669 |
| `@kooying_judy26` | 16 / 16 | 1,283 | 96 | 154 | 17 | 0 | 0 | 25.5s | www.kooying.com / we are source factory of brake linings and pads.Please contact. |
| `@kooying.autoparts` | 19 / 19 | 2,661,068 | 1,797 | 1,300,000 | 44,200 | 1,991 | 5,959 | 26s | 尚无个人简介。 |
| `@kooying.brake.lin` | 1 / 1 | 60 | 60 | 60 | 1 | 0 | 0 | 225s | Brake lining / Brake pad / +86 159 5711 8669 |

## 4. 全量 49 条作品逐条抓取明细

### 4.1 [`@kooying.brake.fact`](https://www.tiktok.com/@kooying.brake.fact) — Kooying brake factory boss（共 13 条）

- **简介**：`20 year experience of developing and producing brake pads and brake linings.wish to cooperate with you.Sophia +8615957118669`｜**粉丝**：`73`｜**主页点赞**：`65`

| # | 发布时间 (UTC) | 类型 | 时长 | 播放 | 点赞 | 评论 | 转发 | 文案 / 主题摘要 | 作品链接 |
|---:|---|---|---:|---:|---:|---:|---:|---|---|
| 1 | 2026-02-08 07:16 UTC | 视频 | 26s | 116 | 5 | 0 | 0 | TikTok video #7604389793560317204 | [查看](https://www.tiktok.com/@kooying.brake.fact/video/7604389793560317204) |
| 2 | 2026-01-26 01:37 UTC | 视频 | 56s | 142 | 9 | 0 | 0 | #source factory #big factory to produce brake lining&pad#good performance high quality #brak... | [查看](https://www.tiktok.com/@kooying.brake.fact/video/7599478345201356052) |
| 3 | 2026-01-14 15:47 UTC | 视频 | 174s | 190 | 3 | 0 | 0 | #sourcefactory #storytime #ladyboss kooying#brake linings #source manufacture | [查看](https://www.tiktok.com/@kooying.brake.fact/video/7595244482162085140) |
| 4 | 2026-01-14 15:45 UTC | 视频 | 22s | 181 | 5 | 0 | 0 | #sourcefactory #lady #china #brake pad #brake lining | [查看](https://www.tiktok.com/@kooying.brake.fact/video/7595243813719854357) |
| 5 | 2025-12-26 14:44 UTC | 视频 | 26s | 143 | 5 | 0 | 0 | TikTok video #7588177429399702804 | [查看](https://www.tiktok.com/@kooying.brake.fact/video/7588177429399702804) |
| 6 | 2025-12-26 14:40 UTC | 视频 | 40s | 130 | 3 | 0 | 0 | TikTok video #7588176445181054228 | [查看](https://www.tiktok.com/@kooying.brake.fact/video/7588176445181054228) |
| 7 | 2025-12-20 17:32 UTC | 视频 | 32s | 248 | 12 | 0 | 0 | #brake lining manufacture#sourcefactory of brake pads #china factory #brake block for truck | [查看](https://www.tiktok.com/@kooying.brake.fact/video/7585994417375644949) |
| 8 | 2025-12-13 17:55 UTC | 视频 | 35s | 200 | 6 | 0 | 0 | how to find the source factory,I will tell you through this vedio.trust me. Will show you th... | [查看](https://www.tiktok.com/@kooying.brake.fact/video/7583402679364373781) |
| 9 | 2025-12-13 10:42 UTC | 视频 | 37s | 195 | 8 | 2 | 0 | I am the boss of kooying brake linings factory.today I will take some Vedio show you.let you... | [查看](https://www.tiktok.com/@kooying.brake.fact/video/7583291181329861908) |
| 10 | 图文/未标时间 | 图文 | — | — | — | — | — | Kooying brake factory boss 使用  的 原声 - Kooying brake factory boss 创作的 Automechanika Shanghai ... | [查看](https://www.tiktok.com/@kooying.brake.fact/photo/7574788730414746936) |
| 11 | 图文/未标时间 | 图文 | — | — | — | — | — | Kooying brake factory boss 使用 The Untouchables 的 original sound - the untouchables 创作的 www.k... | [查看](https://www.tiktok.com/@kooying.brake.fact/photo/7563226244964240660) |
| 12 | 2025-10-13 00:14 UTC | 视频 | 14s | 73 | 5 | 1 | 0 | beautiful shanghai china | [查看](https://www.tiktok.com/@kooying.brake.fact/video/7560492707240627472) |
| 13 | 图文/未标时间 | 图文 | — | — | — | — | — | Kooying brake factory boss 使用 Phortte Music 的 Working in Progress 创作的 brake lining factory.m... | [查看](https://www.tiktok.com/@kooying.brake.fact/photo/7553566354754915601) |

### 4.2 [`@kooying_judy26`](https://www.tiktok.com/@kooying_judy26) — kooying_judy26（共 16 条）

- **简介**：`www.kooying.com / we are source factory of brake linings and pads.Please contact.`｜**粉丝**：`3`｜**主页点赞**：`17`

| # | 发布时间 (UTC) | 类型 | 时长 | 播放 | 点赞 | 评论 | 转发 | 文案 / 主题摘要 | 作品链接 |
|---:|---|---|---:|---:|---:|---:|---:|---|---|
| 1 | 2026-07-21 03:35 UTC | 视频 | 44s | 100 | 8 | 0 | 0 | 54b96eefadf72bb523cdfbf7a975b79f | [查看](https://www.tiktok.com/@kooying_judy26/video/7664819613280029966) |
| 2 | 2026-07-07 00:14 UTC | 视频 | 23s | 32 | 0 | 0 | 0 | kooying brake lining If you want to check real brake linings, confirm matching models or lea... | [查看](https://www.tiktok.com/@kooying_judy26/video/7659572645914037517) |
| 3 | 2026-07-04 08:12 UTC | 视频 | 85s | 126 | 1 | 0 | 0 | Craftsmanship runs through every Kooying brake lining. kooying brake | [查看](https://www.tiktok.com/@kooying_judy26/video/7658582612235062541) |
| 4 | 2026-06-06 02:29 UTC | 视频 | 4s | 2 | 0 | 0 | 0 | brake pad #brakelining #kooying #factory #冠英刹车片 #worklife | [查看](https://www.tiktok.com/@kooying_judy26/video/7648103871914478862) |
| 5 | 2026-06-01 13:56 UTC | 视频 | 8s | 154 | 0 | 0 | 0 | daily life. Show you our big factory. welcome to Kooying. #factory #kooying  #brakelining | [查看](https://www.tiktok.com/@kooying_judy26/video/7646425339417103630) |
| 6 | 2026-05-26 11:52 UTC | 视频 | 13s | 42 | 0 | 0 | 0 | Weld marking on brake pads. Welcome to Kooying. Let us go.  #brakepad #worklife #冠英刹车片 #kooy... | [查看](https://www.tiktok.com/@kooying_judy26/video/7644166710207057165) |
| 7 | 2026-05-18 09:33 UTC | 视频 | 16s | 74 | 0 | 0 | 0 | #brakelining #dailylife #worklife ＃factory | [查看](https://www.tiktok.com/@kooying_judy26/video/7641162276354936078) |
| 8 | 2026-05-16 09:29 UTC | 视频 | 17s | 72 | 0 | 0 | 0 | welcome to our factory，let us go and see our work. #brake #dailylife #video #brakelining | [查看](https://www.tiktok.com/@kooying_judy26/video/7640418862340525326) |
| 9 | 2026-05-13 14:12 UTC | 视频 | 172s | 139 | 1 | 0 | 0 | 一件事，一辈子，冠英的001号员工，上海交大毕业后第一份工作，一干就是20年，成为冠英刹车片初闯市场的拓荒者，他踏遍渠道，深耕终端，用脚步丈量市场，用专业赢得信赖！#卡车刹车片#卡车配... | [查看](https://www.tiktok.com/@kooying_judy26/video/7639378906709003533) |
| 10 | 2026-05-05 00:36 UTC | 视频 | 21s | 109 | 2 | 0 | 0 | brake linings What you focus on，tell me.  I am very happy to service for you. #brakelining  ... | [查看](https://www.tiktok.com/@kooying_judy26/video/7636200128096881933) |
| 11 | 2026-05-04 09:30 UTC | 视频 | 53s | 127 | 0 | 0 | 0 | TikTok video #7635965948062747918 | [查看](https://www.tiktok.com/@kooying_judy26/video/7635965948062747918) |
| 12 | 2026-05-04 04:34 UTC | 视频 | 49s | 102 | 1 | 0 | 0 | Kooying #brakelining #brakepad  #auto #shopping  Stable braking starts with quality. Long‑la... | [查看](https://www.tiktok.com/@kooying_judy26/video/7635890150475697422) |
| 13 | 2026-04-30 08:24 UTC | 视频 | 28s | 92 | 1 | 0 | 0 | climb the mountain. enjoy the life. | [查看](https://www.tiktok.com/@kooying_judy26/video/7634464858213321997) |
| 14 | 2026-04-06 06:09 UTC | 视频 | 60s | 101 | 1 | 0 | 0 | Follow me,I'm the boss of a professional brake pad manufacturer.  welcome to kooying. #brake... | [查看](https://www.tiktok.com/@kooying_judy26/video/7625524239176682766) |
| 15 | 2026-01-31 02:55 UTC | 视频 | 22s | 2 | 1 | 0 | 0 | Professional Version One-Stop Shopping for Drum & Disc Brake Pads: High Performance, Maximum... | [查看](https://www.tiktok.com/@kooying_judy26/video/7601353690552864014) |
| 16 | 2026-01-30 00:20 UTC | 视频 | 55s | 9 | 1 | 0 | 0 | Welcome to China.Welcome to contact me for brake linings and pads.#brake linings and pads | [查看](https://www.tiktok.com/@kooying_judy26/video/7600942760476101943) |

### 4.3 [`@kooying.autoparts`](https://www.tiktok.com/@kooying.autoparts) — KOOYING（共 19 条）

- **简介**：`尚无个人简介。`｜**粉丝**：`2423`｜**主页点赞**：`48.3K`

| # | 发布时间 (UTC) | 类型 | 时长 | 播放 | 点赞 | 评论 | 转发 | 文案 / 主题摘要 | 作品链接 |
|---:|---|---|---:|---:|---:|---:|---:|---|---|
| 1 | 2023-06-26 07:14 UTC | 视频 | 70s | 611,300 | 8,132 | 28 | 247 | #foryou #brakepads #fyp #factory #truck #tiktok #fvp | [查看](https://www.tiktok.com/@kooying.autoparts/video/7248889590545747246) |
| 2 | 2023-05-25 08:33 UTC | 视频 | 9s | 559,200 | 1,432 | 16 | 46 | 🤔🤔#fyp #brakepads #factory #hforyou #foryou #tiktok #learnontuktok #processing | [查看](https://www.tiktok.com/@kooying.autoparts/video/7237035035637959979) |
| 3 | 2023-05-18 08:37 UTC | 视频 | 84s | 1,300,000 | 33,200 | 1,936 | 5,600 | For more business details , please send us a message #fyp #fyp #brakepads #factory #hforyou ... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7234438734060522794) |
| 4 | 2023-10-12 10:32 UTC | 视频 | 23s | 1,619 | 27 | 0 | 0 | Wrake linling manufacturers#truck #fyp #foryou #brakepads #brakelinings #production #brakeli... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7289017942673460511) |
| 5 | 2023-10-11 10:44 UTC | 视频 | 30s | 123,300 | 929 | 5 | 27 | Brake linling manufacturer#truck #fyp #foryou #brakepads #brakelinings #production #brakelin... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7288649849484872991) |
| 6 | 2023-10-11 01:26 UTC | 视频 | 54s | 1,148 | 4 | 0 | 0 | If you need please feel free to contact us#truck #fyp #foryou #brakepads #brakelinings #brak... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7288506138369264926) |
| 7 | 2023-10-09 10:10 UTC | 视频 | 42s | 1,797 | 17 | 0 | 0 | Contact us to production for you#truck #fyp #foryou #brakepads #brakelinings #production #br... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7287899002467601695) |
| 8 | 2023-10-08 09:39 UTC | 视频 | 10s | 2,662 | 15 | 1 | 0 | Truck brake linling manufacturers#truck #fyp #foryou #brakepads #brakelinings #brakelinings ... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7287519774668524831) |
| 9 | 2023-10-07 10:23 UTC | 视频 | 26s | 39,200 | 337 | 1 | 32 | Contact us for more details#truck #fyp #foryou #brakepads #brakelinings #production #brakeli... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7287160093714418974) |
| 10 | 2023-09-28 09:20 UTC | 视频 | 23s | 920 | 11 | 0 | 0 | Contact us for more details#truck #fyp #foryou #brakepads #factory #brakelinings #production... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7283804096090410271) |
| 11 | 2023-09-26 10:31 UTC | 视频 | 9s | 1,020 | 11 | 0 | 0 | Contact us for more information#truck #fyp #foryou #brakepads #factory #brakelinings #produc... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7283080350945905962) |
| 12 | 2023-09-26 10:31 UTC | 视频 | 30s | 799 | 3 | 0 | 0 | Contact us for more information#truck #fyp #foryou #brakepads #factory #brakelinings #produc... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7283080315990461738) |
| 13 | 2023-09-22 09:58 UTC | 视频 | 25s | 879 | 6 | 1 | 0 | Truck lining manufacturers#truck #fyp #foryou #brakepads #factory #brakelinings #factory #pr... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7281587275501325614) |
| 14 | 2023-09-21 10:12 UTC | 视频 | 29s | 885 | 5 | 0 | 1 | Truck brake lining processing plant#truck #fyp #foryou #brakepads #factory #brakelinings #pr... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7281220001896434986) |
| 15 | 2023-09-20 09:36 UTC | 视频 | 13s | 3,946 | 15 | 0 | 0 | Direct supply from source factory#truck #fyp #foryou #brakepads #factory #brakelinings #brak... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7280839535095844139) |
| 16 | 2023-09-19 09:40 UTC | 视频 | 34s | 695 | 3 | 0 | 0 | Please contact if necessary#truck #fyp #foryou #brakepads #factory #brakelinings #brakelinin... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7280469550737476895) |
| 17 | 2023-09-18 09:53 UTC | 视频 | 15s | 8,795 | 30 | 3 | 4 | Brake linling manufacturer#fyp #truck #foryou #brakepads #factory #brakelinings #brakelining... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7280101761719733534) |
| 18 | 2023-09-15 09:44 UTC | 视频 | 9s | 2,194 | 22 | 0 | 1 | Rake lining manufacturer#fyp #truck #foryou #brakepads #factory #brakelinings #brakelining #... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7278986240077106462) |
| 19 | 2023-09-13 09:58 UTC | 视频 | 32s | 709 | 1 | 0 | 1 | Please contact us for preferential prices#fyp #truck #foryou #brakepads #factory #brakelinin... | [查看](https://www.tiktok.com/@kooying.autoparts/video/7278247564539956522) |

### 4.4 [`@kooying.brake.lin`](https://www.tiktok.com/@kooying.brake.lin) — Kooying brake li ning（共 1 条）

- **简介**：`Brake lining / Brake pad / +86 159 5711 8669`｜**粉丝**：`0`｜**主页点赞**：`1`

| # | 发布时间 (UTC) | 类型 | 时长 | 播放 | 点赞 | 评论 | 转发 | 文案 / 主题摘要 | 作品链接 |
|---:|---|---|---:|---:|---:|---:|---:|---|---|
| 1 | 2024-08-06 09:32 UTC | 视频 | 225s | 60 | 1 | 0 | 0 | #brakepads | [查看](https://www.tiktok.com/@kooying.brake.lin/video/7399956985896078634) |

## 5. 深度运营诊断（4 大核心问题）

1. **千万级潜力的爆款老号被遗忘，新号从 0 重来**：`@kooying.autoparts`（2023-05~10）仅发 19 条即跑出 **266.1 万总播放**、单条最高 **130 万播放 / 1,936 条评论 / 5,600 次转发**（`7234438734060522794` 置顶外仅一句 `For more business details , please send us a message` 即引发大量买家评论求购），但该号至今 **0 Bio 简介、无 WhatsApp、无官网**，且 3 年未更新；随后重开的 `@kooying.brake.lin`（1 条/60 播）、`@kooying.brake.fact`（13 条/1,618 播）、`@kooying_judy26`（16 条/1,283 播）陷入多号互搏与重复冷启动。

2. **英文拼写与机械标签伤及专业信任**：`@kooying.autoparts` 多条英文标题出现 `Wrake linling`、`Brake linling`、`Rake lining`、`learnontuktok`、`hforyou` 等拼写错误；`@kooying.brake.fact` 视频标题出现 `Vedio`、`Automechanika Shangha`；与现有官网 `kooying.com` 存在 `Btake Lining`、`CartonFair` 一样，容易拉低欧美/中东专业车队买家信任度。

3. **抖音（302 作品）与 TikTok（49 作品）严重脱节**：老板娘抖音 `zyf0571` 已沉淀 302 条高质量工厂/广交会发车/客户签约实拍，但 TikTok 四账号合计仅 49 条且 2026-07 以后全部处于停更状态；大量海外热门题材（中国制造网外贸分享、广交会发货实录、新能源商用车摩擦配方升级）尚未同步到 TikTok。

4. **评论区与 Bio 转化承接断点**：`@kooying.autoparts` 单条爆款下累积 1,936 条评论与 5,600 次转发，却未置顶 WhatsApp `+86 159-5711-8669`、WVA/尺寸 30 秒报价入口，也未接通 7×24 跨时差 AI 评论/私信捕手；`@kooying_judy26` 仅留裸域名未写 WhatsApp，导致移动端买家流失。

## 6. 面向冠英 AI+GTM 赛道的 4 步可落地优化动作

1. **立即盘活 [`@kooying.autoparts`](https://www.tiktok.com/@kooying.autoparts)（优先级 P0）**：补全官方 Bio（`Zhejiang Kooying · 20Y Heavy-Duty Brake Lining Factory | IATF16949 & 400C 0.040mm Wear | WhatsApp +86 159-5711-8669 | www.kooying.com`），不要再另起炉灶开新号；可与老板娘 IP `@kooying.brake.fact` 双向 `@` 联动导流。

2. **用实测数据 + 车桥型号重构英文脚本（替代泛标签）**：从 `#fyp #foryou` 转向真实海外买家长尾词 `#brakepads #brakelinings #WVA29087 #BPW #HOWO #Shacman #Hande35T #truckmaintenance`，结合 `GY-NAO-38` 400℃ 台架磨损仅 `0.040mm`（行业参照 `0.085mm`）实测背书。

3. **建立“抖音日更 → AI 英西阿语切片 → TikTok 双号分发”流水线**：通过本工作台的 **短视频与内容运营 + 出海内容专家 Bot**，自动把老板娘抖音 1 分钟以内实拍转为无拼写错误的英/西双语配文，保持每周 3–4 更稳定节奏。

4. **评论区 + Bio 引流至 30 秒选型报价闭环**：所有置顶视频 CTA 统一引导买家发送 `WVA / Axle Photo / Outer Arc × Width × Holes` 到 WhatsApp `+86 159-5711-8669` 或独立站询盘表单，直接接入工作台 **跨时差询盘防漏单 + 飞书多维表格**。
