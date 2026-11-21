# 浙江冠英（KOOYING）海外极简品牌独立站 + SEO/GEO 矩阵完整设计方案

> 所属工程：INTERCITY HACK 三城联动黑客松·湖州场（AlphaGTM · AI+GTM 赛道）
> 目标企业：浙江冠英汽车零部件有限公司（Zhejiang Kooying Autoparts Co., Ltd.）
> 品牌定位：AI-Era Direct Manufacturer Portal for Commercial Vehicle Friction Systems
> 关联底座：[`148a6ae4` GTM 业务台](http://127.0.0.1:17422)（直连线索雷达 `/api/guanying/leads` 与秒级选型报价 `/api/guanying/match-quote`）

---

## 一、项目背景与商业目标

根据官方赛事材料（[`problem-ai-gtm.md`](../hackathon/docs/problem-ai-gtm.md)）及企业内部 PDF 文档（[`2026年浙江冠英企业介绍.pdf`](../hackathon/materials/2026%E5%B9%B4%E6%B5%99%E6%B1%9F%E5%86%A0%E8%8B%B1%E4%BC%81%E4%B8%9A%E4%BB%8B%E7%BB%8D.pdf)、[`kooying catalog.pdf`](../hackathon/materials/kooying%20catalog.pdf)、[`冠隆产品册.pdf`](../hackathon/materials/%E5%86%A0%E9%9A%86%E4%BA%A7%E5%93%81%E5%86%8C.pdf)）：

1. **制造实力事实基线（Fact Sheet）**
   - 创立于 **2006 年**，位于浙江省湖州市南太湖新区；**2019 年**投产 **1.8 亿元**智能化生产基地。
   - 年产能 **1,500 万套** 商用车盘式刹车片、鼓式刹车片及风电制动片，服务全球 **30+ 个国家和地区**，核心覆盖 **东南亚、中东、南美、北美** 四大海外市场。
   - 旗下拥有 **5 大品牌矩阵**：**KOOYING（冠英国际）**、**冠隆（GUANLONG）**、**新莱（XINLAI）**、**圣雕（SHENGDIAO）**、**三安 / 卡能**。
   - 资质与研发：**IATF 16949:2016**、**ECE R90（E-Mark）**、**GB5763-2018**、**3C 认证**、国家高新技术企业；配备 **2 台欧洲 Krauss（克劳斯）试验机、北美 Chase 试验机、4 台定速试验机、剪切与压缩强度试验机**；与**国防科技大学、中国地质大学（武汉）**合作研究纳米矿物纤维与高碳纤维复合材料，沉淀 **3 大系列 30+ 种无石棉摩擦配方**。
   - 实测台架性能：工作温度下摩擦系数稳定在 **`0.35–0.38`**，**400°C+ 高温恒力矩厚度磨损仅 `0.040mm`**（对比行业参照 `0.085mm`），定速重量磨损 `0.580g`（对比 NC `0.650g`），密度 `2.040 g/cm³`，硬度 `HRM 8.442`，冲击强度 `0.340 J/cm²`。

2. **核心 GTM 痛点与独立站破局点**
   - **海关数据全是贸易商、社媒线索泛化**：通过高精准 **WVA 编码 / 挂车车桥（BPW / Meritor / Fuwa / Hande）/ 物理尺寸** 静态 SEO 与 GEO 着陆页，直接拦截海外终端车队维保总监、区域汽配批发商与矿山工程维修站的自然搜索及 AI 搜索需求。
   - **无主机厂 OE 编码 & 后市场频繁换桥**：传统靠单一车型报错率极高、一次人工核算报价需 2 小时。独立站内置 **BrakeFinder™ 四维交叉选型引擎**，支持按 **WVA/GYS 编码、车桥系统、重卡车系、或物理几何尺寸（外弧 × 宽度 × 厚度 + 孔数，支持 ±3mm 容差）** 毫秒级锁定实物三视图与配方，提交即触发 GTM 秒级报价与线索入库。

---

## 二、海外一线标杆对标（Global Industrial Benchmarking）

| 国际标杆 | 品牌地位 | 视觉与产品体验特征 | 融入 KOOYING 独立站的设计规则 |
|---|---|---|---|
| **TMD Friction / Textar** (`textar.com` & `brakebook.com`) | 德国百年商用车制动巨头 | 石墨炭黑 + 工业暖白 + 信号琥珀色；主站极致留白讲清实验室台架与配方实力；复杂型号全部收进 **Brakebook** 编码/尺寸交叉检索器 | 主页严格执行 **一屏一主题（One Section Per Screen）** 低密度排版；顶部保留极简导航与一键进入 **BrakeFinder™** 交叉选型器 |
| **Brembo Commercial Vehicles** (`brembo.com`) | 意大利高端制动系统标杆 | 微距金属与摩擦片颗粒质感、无堆砌边框、Grotesk 无衬线大标题 + Monospace 精密公差数据、实拍多视角与工程图对照 | 充分释放解压自实拍库的 **正视图（Front）/ 背板喷码图（Back）/ 侧面厚度图（Profile）** 三视图交互，与 PDF 孔位工程图同屏呼应 |
| **Beral (DRiV) & Fras-le** (`drivparts.com` / `fras-le.com`) | 欧美重卡挂车车桥制动领军品牌 | 按 **海外区域工况** 与 **挂车/驱动车桥体系（BPW / SAF / Meritor）** 独立建页，彻底解决后市场换桥选型难题 | 打造 **4 个高转化静态 SEO/GEO 专题落地页**，覆盖盘刹 WVA 族、挂车车桥族、日韩重卡鼓刹族与摩擦配方实验报告 |

---

## 三、视觉系统与低密度空间纪律（Industrial Minimalism）

1. **色彩系统（Swiss Industrial Precision Palette）**
   - **Warm Alabaster 工业暖白主底**：`#F5F4F0`（克制暖白，让深色无石棉摩擦块与激光喷码背板成为视觉主体）
   - **Carbon Friction 石墨碳黑对比屏**：`#111315`（用于 Hero 首屏与 400°C 台架科学屏，呈现深沉的欧洲精密工业质感）
   - **Machined Surface 机械浅灰面**：`#EAE8E1` / 深色面 `#191C20`；极细分割线 `1px solid rgba(17, 19, 21, 0.10)`
   - **Cast Iron Ink 铸铁主墨字**：`#111315`；次级工程灰文字 `#585C63`
   - **Braking Copper 制动铜橙信号色**：`#C84B21`（仅用于主按钮 CTA、400°C 热衰退指标、认证标识与选中态；严禁使用紫色或花哨霓虹渐变）

2. **字体体系**
   - **标题与品牌叙事**：`'Plus Jakarta Sans', 'Inter', system-ui, sans-serif`（紧字距 `-0.03em`，大呼吸感）
   - **编码与工程参数**：`'JetBrains Mono', ui-monospace, monospace`（所有 `WVA 29087`、`GYS-119`、`240×220×18.5mm`、`μ = 0.35–0.38`、`±0.5mm` 统一等宽呈现）

3. **低密度防拥挤规范（Anti-Crowding Rules）**
   - 最大内容容器宽度 `1320px`，左右边距严格对称。
   - 每个 `section` 独立占据 1 屏（`padding: 108px 0`），单屏正文不超过 80 英文词，信息层次克制。
   - 产品卡片采用大画幅 3 列矩阵，每张卡片内置 **Front / Back / Profile 三视图切换芯片**，图像底部不加冗长段落说明。

---

## 四、7 屏品牌主页信息架构（`web/index.html`）

1. **Screen 1 · Hero（Carbon Friction 深色首屏）**
   - 大标题：`Commercial Vehicle Friction Systems Engineered for Axle Precision.`
   - 硬核数据条：`15M Sets / Year` · `30+ Export Markets` · `μ 0.35–0.38 @ 400°C` · `IATF 16949 & ECE R90`
   - 交互主视觉：展示 **WVA 29162 / 29087** 高清实拍与正反侧三视角切换 + 顶部直达 **BrakeFinder™** 检索框。
2. **Screen 2 · Global Market Matrix（四大海外主力市场）**
   - **Middle East（中东高温重载）**：抗热衰退高碳纤维配方，主力型号 `WVA 29087`, `29228 BPW`, `BPW 16T BC39-1`。
   - **Southeast Asia（东南亚湿热高频）**：日韩与重卡全覆盖，`HINO 日野`, `ISUZU CVR146`, `HYUNDAI FA/FB/RA/RB`, `HOWO`。
   - **South America（南美矿山与欧洲重卡干线）**：高冲击重载耐摩，`SCANIA 19932`, `VOLVO`, `STEYR`, `三一/徐重矿卡`。
   - **North America（北美长途干线与挂车）**：美标托架与驱动桥，`19369`, `19370`, `19553`, `Meritor 美驰桥`, `美国托架 12T`。
3. **Screen 3 · Core Friction Architecture（三大产品阵线大画幅画廊）**
   - **Air Disc Brake Pads（盘式刹车片）**：展示 `WVA 29087`, `WVA 29162`, `BPW 8201`, `VIE 19.5/20K`, `25K 专用`，支持 **Front / Back / Profile** 三视角实时切换及 `5200.5300.mp4` 实拍演示入口。
   - **Heavy-Duty Drum Linings（鼓式刹车片）**：展示 `WVA 307AF / 311AF`, `BPW BC36-1~BC39-1`, `Meritor 美驰桥`, `Fuwa 富华桥`, `Hande 汉德桥 35T`。
   - **Wind Power & Mining Specialty（风电与特种矿卡）**：展示风电偏航制动与 `克拉斯 / 别拉斯` 重矿车刹车片。
4. **Screen 4 · BrakeFinder™ Four-Dimensional Cross-Reference（四维交叉选型器）**
   - 按 **WVA/GYS 编码**、**车桥品牌**、**卡车/客车品牌** 或 **外弧×宽度×厚度+孔数（±3mm 容差）** 即时筛选出匹配型号、实拍三视图与工程规格，彻底解决后市场换桥无 OE 码难题。
5. **Screen 5 · Friction Science & Krauss / Chase Dyno Lab（台架试验实证屏）**
   - 展示 `200°C / 300°C / 400°C+` 恒力矩对比柱图（冠英 `0.040mm` vs 行业平均 `0.085mm`）、定速测试详表、欧洲 Krauss 与北美 Chase 试验机实景、国防科大/中国地大校企研发与 IATF 16949 / ECE R90 认证。
6. **Screen 6 · Five-Brand Portfolio & Huzhou 1.8亿 Smart Factory（五品牌与南太湖基地）**
   - **KOOYING / 冠隆 / 新莱 / 圣雕 / 三安** 同款 `29162 加长 217mm` 多品牌实拍渲染切换器 + 湖州南太湖新区自动配拌料、自动称料、温控排模热压、激光切铆流水线实景画廊。
7. **Screen 7 · Direct Factory RFQ & 30-Second Spec Match（询盘直连 GTM 雷达）**
   - 终端买家填入公司名称、国家/市场、WVA 编码或物理尺寸（`Arc × Width × Thickness + Holes`）后，系统当场给出匹配型号、配方建议与阶梯参考报价，并同步推送至 `:17422` GTM 后台线索雷达。

---

## 五、双 ZIP 实拍资产迁移清洗规则 & 文件归口

1. **解码与去重**：
   - `鼓式刹车片照片.zip` 与 `盘式刹车片照片.zip` 内部目录采用中文 Windows `cp437 -> gbk` 编码；解压脚本 `web/scripts/unpack_and_build_assets.py` 自动完成无损还原。
   - `鼓式刹车片照片.zip` 存在 `鼓式刹车片照片/<型号>/` 与 `鼓式刹车片照片/鼓刹/<型号>/` 两份完全镜像的重复目录；脚本自动剔除冗余 `鼓刹/` 子目录与重复的 `日野前后/日野后`，归口 **94 个独立鼓刹型号目录（184 张高清实拍图）** 至 `web/assets/products/drum/`。
   - `盘式刹车片照片.zip` 完整归口 **41 个盘刹型号/系列目录（126 张三视图实拍 + 5 品牌渲染图 + 19 张喷码/样板图 + 1 条 `5200.5300.mp4` 实拍视频）** 至 `web/assets/products/disc/`（合计 **135 款已建索引型号**）。
2. **PDF 官方视觉资产提取**：
   - 从 `2026年浙江冠英企业介绍.pdf` 提取 **9 张无边框高清厂区/设备/认证原图**（`web/assets/factory/raw/`）与 **10 张页面切片**（`web/assets/factory/`）。
   - 从 `kooying catalog.pdf` 与 `冠隆产品册.pdf` 渲染 **8 张代表性欧美/亚太/车桥孔位工程尺寸图** 至 `web/assets/catalog-drawings/`。

---

## 六、SEO & GEO（搜索 + AI 答案引擎）技术矩阵

1. **4 大垂直静态落地页（`web/seo/*.html`）+ 全量目录页（`web/catalog.html`）**：
   - `seo/wva-29087-29162-air-disc-brake-pads.html`：重卡气动盘式刹车片专题（中东/欧洲高意图词）。
   - `seo/trailer-axle-brake-linings-bpw-meritor-fuwa.html`：挂车车桥换桥选型专题（BPW / Meritor / Fuwa / Hande）。
   - `seo/heavy-duty-drum-linings-hino-howo-scania.html`：东南亚/南美/北美主流重卡鼓刹专题（HINO / ISUZU / HYUNDAI / SCANIA / HOWO / STEYR）。
   - `seo/friction-material-formulas-krauss-testing.html`：30+ 无石棉配方与 400°C 台架测试白皮书（面向 AI 搜索引用）。
2. **可抓取性与 GEO 基建**：
   - **纯静态 HTML 直出核心内容**：不依赖客户端 JS 拉取正文，60–100 词 Direct Engineering Summary 置顶 + 标准 HTML 参数对照表。
   - **JSON-LD 结构化数据**：每页严格匹配可见文本，按页型嵌入 `Organization`、`Manufacturer`、`Product`、`ItemList`、`BreadcrumbList` 与 `TechArticle`。
   - **完整搜索入口**：`web/robots.txt`（放行 `Googlebot`, `Bingbot`, `OAI-SearchBot`, `GPTBot`, `PerplexityBot`, `ClaudeBot`）、`web/sitemap.xml`（含 `<image:image>` 扩展）、`web/llms.txt`。
