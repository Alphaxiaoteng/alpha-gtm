# AlphaGTM 参赛 PPT · 新增两页文案（第 9、10 页）

配 `show/deck/deck.html`（现 8 页，本文件补第 9、10 页，16:9）。
证据口径分三类：**文件已证实**（仓库内文件）／**本次实测**（2026-11-21 线上只读检查）／**无法验证**。
采集时间：2026-11-21 ~ 2026-11-22。所有数字均附来源；未附来源的数字一律不写。

---

## 第 9 页 · 海外媒体账号调研

### 上屏文案

**标题**
海外媒体账号：老号有量，门面是个人号

**结论**
海外有 4 个 TikTok 号、2 个 B2B 店铺、1 个抖音号，官网页脚是个人号。

**A｜账号矩阵（2026-11-21 审计）**
- @kooying.autoparts：2,423 粉、266 万播放，2023-10-12 停更
- @kooying.brake.fact：73 粉，至 2026-02-08
- @kooying_judy26 3 粉；@kooying.brake.lin 0 粉
- 抖音 zyf0571：5,052 粉、302 条实拍

**B｜平台状态**
- Instagram、YouTube、X：无品牌账号
- Made-in-China、Alibaba：店铺已收录
- 官网页脚（实测）：Facebook 个人号、linkedin.cn

**C｜三个卡点**
- 门面：企业主页缺位，点开是个人页
- 内容：302 条实拍留在国内
- 触点：官网留 QQ 邮箱与两个手机号

**优先行动**
盘活 @kooying.autoparts：补英文 Bio，挂官网与 WhatsApp，切片分发。

### 设计核对证据注（不上屏）

| 上屏内容 | 类别 | 来源 | 日期 |
|---|---|---|---|
| 4 个 TikTok 号及粉丝/播放/停更时间 | 文件已证实 | `hackathon/docs/KOOYING_OVERSEAS_MEDIA_AUDIT.md` 第 11–14 行 | 2026-11-21 |
| 抖音 zyf0571：5,052 粉 / 302 条 | 文件已证实 | 同上，第 23 行 | 2026-11-21 |
| Instagram / YouTube / X 未检出 | 文件已证实 | 同上，第 17–19 行 | 2026-11-21 |
| Made-in-China / Alibaba 店铺 | 文件已证实 | 同上，第 20–21 行 | 2026-11-21 |
| 官网页脚挂个人 FB 与 linkedin.cn | 本次实测 | `https://www.kooying.com/`（HTTP 200）页脚含 `facebook.com/jenny.ji.790256`、`linkedin.cn/in/晓玮-纪-750175197` | 2026-11-21 |
| 官网留 QQ 个人邮箱 | 文件已证实 + 本次实测 | 审计文件第 22、34 行；`www.kooying.com` 首页含 `qq.com` 邮箱 | 2026-11-21 |
| 「266 万播放」 | 文件已证实 | 审计文件第 11 行 `2,661,068`（单条最高 130 万） | 2026-11-21 |

- 未写：粉丝增长、播放转化、线索数、成交额、单平台月度趋势——**无法验证**，仓库无此类数据，本次也未采集。
- TikTok 粉丝/播放数字**未在本次实测中复核**（本次只读范围限定官网）。现场被追问时以审计文件为唯一口径。
- 审计文件另记官网含通用分享链接 `twitter.com/home?status=`；本次实测官网首页**未检出** X 账号链接，故上屏只写「无品牌账号」。
- 审计文件第 25 行已排除两个同名无关号（`@kooying29` 泰国生活号、`@kooying` 空号），上屏不列。

---

## 第 10 页 · 网站 SEO 深度分析

### 上屏文案

**标题**
网站 SEO：地基齐了，站还没上线

**结论**
6 页 title、canonical 与结构化数据到位，但 kooyingbrake.com 未解析。

**A｜技术地基（已证实）**
- robots 放行全站，含 GPTBot、ClaudeBot
- 6 页互链成环，每页唯一 title 与自引用 canonical
- 结构化数据：Organization、CollectionPage、ItemList、TechArticle

**B｜硬缺口**
- 域名未解析：公共 DNS 返回 NXDOMAIN（实测）
- 型号不可抓：135 条规格由 JS 渲染，无型号 URL
- 仅英文，无 hreflang，三语未落地

**C｜现官网对照**
- sitemap 468 条，272 个型号页
- 型号页标题是模板句，Mercedes-Benz 拼成 MERCEDS-BENZ

**优先行动**
P0 域名上线 + 型号页可抓取；P1 补 hreflang、BreadcrumbList 与标题。

**验收（目标）**
域名 200；6 个 URL 全部 200；收录 ≥ 6；可抓取型号页 0 → 135。

### 设计核对证据注（不上屏）

**技术地基（文件已证实）**

| 项 | 证据 |
|---|---|
| robots.txt 放行 | `web/robots.txt`：`Allow: /`，单列 Googlebot、Bingbot、OAI-SearchBot、GPTBot、PerplexityBot、ClaudeBot；含 `Sitemap: https://kooyingbrake.com/sitemap.xml` |
| sitemap 6 个 URL | `web/sitemap.xml`：`/`、`/catalog.html`、4 个 `/seo/*.html`；`lastmod` 2026-11-21 |
| 唯一 title / description / 自引用 canonical | `web/index.html`、`web/catalog.html`、`web/seo/*.html` 四页 head 逐页检查，canonical 均自引用 `https://kooyingbrake.com/...` |
| 结构化数据 4 类 | `index.html`：Organization；`catalog.html`：CollectionPage；`seo/wva-...`、`seo/heavy-duty-...`：ItemList + Product；`seo/trailer-...`、`seo/friction-...`：TechArticle |
| 6 页互链成环 | 每个 `/seo/` 页导航含首页、catalog 与其余 3 个 SEO 页（逐页 grep `href` 确认） |
| 缺 404 页 | `web/404.html` 不存在 |
| 缺 BreadcrumbList / FAQPage | 全站 HTML 无 `BreadcrumbList`、无 `FAQPage`；`web/PLAN.md` 第 104 行只把 BreadcrumbList 列为设计项 |

**硬缺口（文件已证实 + 本次实测）**

| 项 | 证据 |
|---|---|
| 域名未解析 | `kooyingbrake.com` 公共 DNS 查询返回 **NXDOMAIN**（Google DoH 与 Cloudflare DoH 均为 Status 3，2026-11-21 实测）；本机走代理命中 fake-ip `198.18.0.98`，TLS 握手失败、HTTP 经代理返回 502——域名未委派、站点未部署 |
| 型号页不可抓 | `web/catalog.html` 商品网格 `#brakefinder-grid` 为空容器，由 `assets/data/products-catalog.js` + `assets/js/site.js` 客户端渲染；无独立型号 URL。数据集声明 `total_models: 135`（41 盘式 / 94 鼓式） |
| 仅英文 | 6 个页面全部 `html lang="en"`；全仓 `hreflang` 零命中；无 es/ar 目录 |
| 与路演口径不符 | `show/copy/deck-script.md` 第 28 行讲「英、西、阿三种语言」，落地站只有英文 |

**现官网对照（本次实测 2026-11-21）**

| 项 | 证据 |
|---|---|
| 可访问 | `https://www.kooying.com/` HTTP 200，58,967 字节，响应头含 HSTS 与 CSP |
| sitemap | `https://www.kooying.com/sitemap.xml` HTTP 200，468 个 `<loc>`：272 个 `/product/`、190 个 `/news/`；声明 `xmlns:xhtml` 但 `xhtml:link` 计数为 0（无 hreflang 标注） |
| robots | `https://www.kooying.com/robots.txt` HTTP 200：`AhrefsBot`、`SemrushBot` 全站 `Disallow: /`；放行 Googlebot、GPTBot；**无 Sitemap 指令** |
| 型号页标题模板化 | 示例 `/product/european-vehicle-brake-lining/17276-mb-60-1-...html` 标题为 `Wholesale 17276 MB/60/1 High-end Quality Brake Lining for MERCEDS-BENZ Suppliers, Factory`（Mercedes-Benz 拼写错误），description 以公司全名开头、为套话模板 |
| 无 hreflang | 首页与 `cn.kooying.com` 首页均无 `hreflang`；语言切换靠 `cn.kooying.com` 独立域名，`cn` 站亦无 canonical |

**无法验证（不得上屏为事实）**
- Google/Bing 收录量、关键词排名、自然流量、DR、外链数：无 Search Console / Ahrefs 权限，仓库内也无该类数据。
- 仓库内唯一 SEO 策略文档 `SEO_STRATEGY.md`（2026-08-09）面向另一个域名 `pleiadops.com`，第 4 行自述「keyword volumes ... pending」，**不能**作为冠英站的关键词或流量来源。
- 新站上线后的真实抓取、索引、AI 引用：域名未解析，**本次无法验证**；上屏「验收（目标）」全部是下一步目标，不是已取得成果。

---

## 两页上屏字数（实测）

统计口径：仅「上屏文案」区块，去空白与换行后的字符数（含英文与数字），由 `python3` 统计。

| 页 | 非空白字符 | 其中汉字 | 区间 280–420 | 状态 |
|---|---:|---:|---|---|
| 第 9 页 · 海外媒体账号调研 | 420 | 142 | 280–420 | 符合 |
| 第 10 页 · 网站 SEO 深度分析 | 421 | 151 | 280–420 | 符合（+1） |

- 英文术语（canonical、hreflang、BreadcrumbList、型号号）占比高，故汉字数低于总字符数；若按「汉字数」单独计量，两页均在区间下沿以下，可按需再增补 2–3 行细节。
- 每页均按「标题 / 结论 / 3 个短模块 / 1 条优先行动」组织；第 10 页额外一行「验收（目标）」。
