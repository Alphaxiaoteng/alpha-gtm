package guanying

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/knowledge"
)

type Lead struct {
	ID             string `json:"id"`
	Company        string `json:"company"`
	Country        string `json:"country"`
	Channel        string `json:"channel"` // SEO独立站 | WhatsApp | LinkedIn | TikTok | Facebook
	Timezone       string `json:"timezone"`
	BuyerType      string `json:"buyer_type"` // 终端车队 | 汽配连锁 | 车桥/大修厂 | 贸易商(已过滤)
	Confidence     int    `json:"confidence"` // 0-100
	IsTerminal     bool   `json:"is_terminal"`
	InquiryText    string `json:"inquiry_text"`
	MatchedCode    string `json:"matched_code"`
	MatchedProduct string `json:"matched_product"`
	Status         string `json:"status"` // 待报价 | 已出报价单 | 已过滤噪音
	FilterReason   string `json:"filter_reason"`
	CreatedAt      string `json:"created_at"`
}

type Quote struct {
	ID            string `json:"id"`
	LeadID        string `json:"lead_id,omitempty"`
	CustomerQuery string `json:"customer_query"`
	ProductID     string `json:"product_id"`
	ProductCode   string `json:"product_code"`
	ProductName   string `json:"product_name"`
	Dimensions    string `json:"dimensions"`
	FormulaCode   string `json:"formula_code"`
	UnitPriceUSD  string `json:"unit_price_usd"`
	UnitPriceCNY  string `json:"unit_price_cny"`
	MOQ           string `json:"moq"`
	SwapAdvice    string `json:"swap_advice"`
	BilingualText string `json:"bilingual_text"`
	CreatedAt     string `json:"created_at"`
}

type SEOPage struct {
	ID              string `json:"id"`
	Slug            string `json:"slug"`
	Lang            string `json:"lang"`
	Title           string `json:"title"`
	MetaDescription string `json:"meta_description"`
	TargetKeywords  string `json:"target_keywords"`
	TargetMarket    string `json:"target_market"`
	SeriesFocus     string `json:"series_focus"`
	HeroHeading     string `json:"hero_heading"`
	HeroSubheading  string `json:"hero_subheading"`
	HighlightsJSON  string `json:"highlights_json"`
	ProductCodes    string `json:"product_codes"`
	SchemaJSONLD    string `json:"schema_jsonld"`
	InquiryCount    int    `json:"inquiry_count"`
	CreatedAt       string `json:"created_at"`
}

type Overview struct {
	Products []OfficialProduct `json:"products"`
	Formulas []FormulaSpec     `json:"formulas"`
	Leads    []Lead            `json:"leads"`
	Quotes   []Quote           `json:"quotes"`
	SEOPages []SEOPage         `json:"seo_pages"`
	Stats    struct {
		ProductCount      int `json:"product_count"`
		TerminalLeadCount int `json:"terminal_lead_count"`
		FilteredTraderCnt int `json:"filtered_trader_count"`
		QuoteCount        int `json:"quote_count"`
		SEOPageCount      int `json:"seo_page_count"`
		ContentCount      int `json:"content_count"`
		KnowledgeDocCount int `json:"knowledge_doc_count"`
	} `json:"stats"`
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func newID(prefix string) string {
	var raw [6]byte
	_, _ = rand.Read(raw[:])
	return prefix + "_" + hex.EncodeToString(raw[:])
}

// EnsureSchemaAndSeed initializes Guanying SQLite tables, seeds 1:1 official
// PDF catalog items, leads, SEO landing pages, content drafts, knowledge base
// documents, and copies feishu.json when missing in dataDir.
func EnsureSchemaAndSeed(ctx context.Context, database *db.DB, dataDir string) error {
	if database == nil || database.DB == nil {
		return fmt.Errorf("guanying: nil database")
	}
	sqlDB := database.DB

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS guanying_products (
			id TEXT PRIMARY KEY,
			code TEXT NOT NULL,
			oe_or_wva TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			brand TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT '',
			vehicle_series TEXT NOT NULL DEFAULT '',
			outer_arc_mm REAL NOT NULL DEFAULT 0,
			inner_arc_mm REAL NOT NULL DEFAULT 0,
			width_mm REAL NOT NULL DEFAULT 0,
			thickness_str TEXT NOT NULL DEFAULT '',
			thickness_mm REAL NOT NULL DEFAULT 0,
			drum_or_taper TEXT NOT NULL DEFAULT '',
			holes INTEGER NOT NULL DEFAULT 0,
			rivet_spec TEXT NOT NULL DEFAULT '',
			applicable_models TEXT NOT NULL DEFAULT '',
			swap_warning TEXT NOT NULL DEFAULT '',
			formula_code TEXT NOT NULL DEFAULT '',
			price_fob_usd TEXT NOT NULL DEFAULT '',
			price_cny TEXT NOT NULL DEFAULT '',
			moq TEXT NOT NULL DEFAULT '',
			catalog_page TEXT NOT NULL DEFAULT '',
			image_url TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS guanying_leads (
			id TEXT PRIMARY KEY,
			company TEXT NOT NULL,
			country TEXT NOT NULL,
			channel TEXT NOT NULL,
			timezone TEXT NOT NULL DEFAULT '',
			buyer_type TEXT NOT NULL,
			confidence INTEGER NOT NULL DEFAULT 0,
			is_terminal INTEGER NOT NULL DEFAULT 0,
			inquiry_text TEXT NOT NULL,
			matched_code TEXT NOT NULL DEFAULT '',
			matched_product TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '待报价',
			filter_reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS guanying_quotes (
			id TEXT PRIMARY KEY,
			lead_id TEXT NOT NULL DEFAULT '',
			customer_query TEXT NOT NULL,
			product_id TEXT NOT NULL,
			product_code TEXT NOT NULL,
			product_name TEXT NOT NULL,
			dimensions TEXT NOT NULL,
			formula_code TEXT NOT NULL,
			unit_price_usd TEXT NOT NULL,
			unit_price_cny TEXT NOT NULL,
			moq TEXT NOT NULL,
			swap_advice TEXT NOT NULL,
			bilingual_text TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS guanying_seo_pages (
			id TEXT PRIMARY KEY,
			slug TEXT NOT NULL UNIQUE,
			lang TEXT NOT NULL,
			title TEXT NOT NULL,
			meta_description TEXT NOT NULL,
			target_keywords TEXT NOT NULL,
			target_market TEXT NOT NULL,
			series_focus TEXT NOT NULL,
			hero_heading TEXT NOT NULL,
			hero_subheading TEXT NOT NULL,
			highlights_json TEXT NOT NULL DEFAULT '[]',
			product_codes TEXT NOT NULL DEFAULT '',
			schema_jsonld TEXT NOT NULL DEFAULT '',
			inquiry_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,
	}
	for _, stmt := range stmts {
		if _, err := sqlDB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create guanying schema: %w", err)
		}
	}

	if err := seedProducts(ctx, sqlDB); err != nil {
		return err
	}
	if err := seedLeads(ctx, sqlDB); err != nil {
		return err
	}
	if err := seedQuotes(ctx, sqlDB); err != nil {
		return err
	}
	if err := seedSEOPages(ctx, sqlDB); err != nil {
		return err
	}
	if err := seedContents(ctx, sqlDB); err != nil {
		return err
	}
	if err := seedKnowledgeBase(ctx, database, dataDir); err != nil {
		return err
	}
	ensureFeishuConfig(dataDir)
	return nil
}

func seedProducts(ctx context.Context, sqlDB *sql.DB) error {
	for _, p := range OfficialCatalogProducts {
		_, err := sqlDB.ExecContext(ctx, `INSERT OR REPLACE INTO guanying_products(
			id,code,oe_or_wva,name,brand,category,vehicle_series,
			outer_arc_mm,inner_arc_mm,width_mm,thickness_str,thickness_mm,
			drum_or_taper,holes,rivet_spec,applicable_models,swap_warning,
			formula_code,price_fob_usd,price_cny,moq,catalog_page,image_url
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.ID, p.Code, p.OEOrWVA, p.Name, p.Brand, p.Category, p.VehicleSeries,
			p.OuterArcMM, p.InnerArcMM, p.WidthMM, p.ThicknessStr, p.ThicknessMM,
			p.DrumOrTaper, p.Holes, p.RivetSpec, p.ApplicableModels, p.SwapWarning,
			p.FormulaCode, p.PriceFOBUSD, p.PriceCNY, p.MOQ, p.CatalogPage, p.ImageURL,
		)
		if err != nil {
			return fmt.Errorf("seed guanying product %s: %w", p.ID, err)
		}
	}
	return nil
}

func seedLeads(ctx context.Context, sqlDB *sql.DB) error {
	var count int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM guanying_leads`).Scan(&count); err == nil && count > 0 {
		return nil
	}
	now := time.Now().UTC()
	leads := []Lead{
		{
			ID: "lead_sa_fleet_01", Company: "Al-Riyadh Heavy Transport Fleet LLC", Country: "沙特阿拉伯 (Riyadh)",
			Channel: "WhatsApp 跨时差留言", Timezone: "UTC+3 (凌晨 02:18 拦截)", BuyerType: "终端车队",
			Confidence: 96, IsTerminal: true,
			InquiryText: "We operate 85 SINOTRUK HOWO tippers in desert quarry. Need 14-hole rear drum brake linings 220mm width, plus WVA 29087 front disc pads. Require high-temp formula (400C no fade).",
			MatchedCode: "GYS-126 / WVA-29087", MatchedProduct: "中国重汽 HOWO2 (204×220×15, 14孔) + WVA 29087",
			Status: "待报价", FilterReason: "明确提及自有 85 台豪沃矿区自卸车队及 14 孔 220mm 参数，终端需求极强，自动匹配 GY-CF-400 高碳纤维抗热衰退配方。",
			CreatedAt: now.Add(-45 * time.Minute).Format(time.RFC3339),
		},
		{
			ID: "lead_cl_parts_02", Company: "Andes Truck & Trailer Spare Parts S.A.", Country: "智利 (Santiago)",
			Channel: "SEO独立站询盘", Timezone: "UTC-4", BuyerType: "汽配连锁",
			Confidence: 93, IsTerminal: true,
			InquiryText: "Looking for direct manufacturer of BPW trailer axle brake lining 19032 (180mm) and 19094 (200mm), and 307AF-1 / 307AF-2. Send E-Mark / IATF16949 certificate and FOB Ningbo price for 2x20GP.",
			MatchedCode: "GYS-007 / GYS-018 / GYS-143", MatchedProduct: "BPW 19032 / 19094 + 307AF-1/2",
			Status: "待报价", FilterReason: "智利专业挂车汽配连锁，精确区分 BPW 180mm/200mm 及 307AF-1/-2，整柜采购意向。",
			CreatedAt: now.Add(-2 * time.Hour).Format(time.RFC3339),
		},
		{
			ID: "lead_my_bus_03", Company: "Syarikat Bas Ekspres Nusantara Sdn Bhd", Country: "马来西亚 (Kuala Lumpur)",
			Channel: "TikTok 海外评论私信", Timezone: "UTC+8", BuyerType: "终端车队",
			Confidence: 91, IsTerminal: true,
			InquiryText: "Saw your Krauss test video. Our express buses use HINO ZY front/rear and YUTONG 6770 / NEW EQ153. Can Kooying supply low-noise NAO brake linings?",
			MatchedCode: "GYS-119 / GYS-120 / GYS-129", MatchedProduct: "日野 HINO ZY 前后片 + 宇通新 EQ153",
			Status: "待报价", FilterReason: "东南亚高速客运公司，关注低噪音(<1.6Hz)与日野/宇通车型匹配。",
			CreatedAt: now.Add(-4 * time.Hour).Format(time.RFC3339),
		},
		{
			ID: "lead_ru_mining_04", Company: "Siberia Ural Quarry Maintenance Co.", Country: "俄罗斯 (Yekaterinburg)",
			Channel: "SEO独立站询盘", Timezone: "UTC+5", BuyerType: "车桥/大修厂",
			Confidence: 95, IsTerminal: true,
			InquiryText: "Our repair depot services Shacman & Weichai mining dump trucks with Hande 35T axles (outer arc 240mm, width 220/240mm, 20 holes). Need wear test report at 400C.",
			MatchedCode: "GL-HD35T-F / GL-HD35T-R", MatchedProduct: "汉德桥 35T 前/后片 (240×220/240×18.5, 20孔)",
			Status: "待报价", FilterReason: "乌拉尔矿区大修厂，精准提供汉德35T外弧/孔数参数并索要400℃台架报告。",
			CreatedAt: now.Add(-6 * time.Hour).Format(time.RFC3339),
		},
		{
			ID: "lead_cn_store_05", Company: "杭州萧山顺达商用车汽配店（一线现场）", Country: "中国 · 浙江杭州",
			Channel: "门店销售现场录入", Timezone: "UTC+8", BuyerType: "汽配连锁",
			Confidence: 98, IsTerminal: true,
			InquiryText: "司机开老解放奥威过来换后轮刹车片，说以前大修厂好像换过桥。现场卡尺量旧片外弧约208mm、宽200mm、数了16个孔带ABS感应孔。",
			MatchedCode: "JF-XAW-RA", MatchedProduct: "解放新奥威后 A(ABS) (208×200×16/20, 16孔)",
			Status: "待报价", FilterReason: "典型后市场换桥场景：虽口述老奥威(原165×220 12孔)，但实测外弧208×宽200 16孔已精确锁定「新奥威后A(ABS)」。",
			CreatedAt: now.Add(-1 * time.Hour).Format(time.RFC3339),
		},
		{
			ID: "lead_tr_noise_06", Company: "Global Horizon Imp & Exp Trading Ltd.", Country: "中国/香港（海关名录抓取）",
			Channel: "海关数据筛查", Timezone: "UTC+8", BuyerType: "贸易商(已过滤)",
			Confidence: 22, IsTerminal: false,
			InquiryText: "Please send full catalog and lowest FOB price list for all truck spare parts.",
			MatchedCode: "-", MatchedProduct: "无明确车型/规格",
			Status: "已过滤噪音", FilterReason: "典型外贸转口贸易公司特征（泛泛索要全品最低价、无车队/汽配门店/具体 WVA 或尺寸参数），AI 自动降权过滤，释放销售精力。",
			CreatedAt: now.Add(-9 * time.Hour).Format(time.RFC3339),
		},
		{
			ID: "lead_tr_noise_07", Company: "Apex General Merchandise Sourcing FZE", Country: "阿联酋 (Dubai Free Zone)",
			Channel: "社媒群发私信", Timezone: "UTC+4", BuyerType: "贸易商(已过滤)",
			Confidence: 28, IsTerminal: false,
			InquiryText: "Hi friend, we trade general hardware and auto items, send best price.",
			MatchedCode: "-", MatchedProduct: "无明确车型/规格",
			Status: "已过滤噪音", FilterReason: "迪拜自由区综合贸易中介，非终端车队或专业商用车汽配商。",
			CreatedAt: now.Add(-11 * time.Hour).Format(time.RFC3339),
		},
	}
	for _, l := range leads {
		isTerm := 0
		if l.IsTerminal {
			isTerm = 1
		}
		_, err := sqlDB.ExecContext(ctx, `INSERT INTO guanying_leads(
			id,company,country,channel,timezone,buyer_type,confidence,is_terminal,
			inquiry_text,matched_code,matched_product,status,filter_reason,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			l.ID, l.Company, l.Country, l.Channel, l.Timezone, l.BuyerType, l.Confidence, isTerm,
			l.InquiryText, l.MatchedCode, l.MatchedProduct, l.Status, l.FilterReason, l.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("seed guanying lead %s: %w", l.ID, err)
		}
	}
	return nil
}

func seedQuotes(ctx context.Context, sqlDB *sql.DB) error {
	var count int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM guanying_quotes`).Scan(&count); err == nil && count > 0 {
		return nil
	}
	now := time.Now().UTC()
	quotes := []Quote{
		{
			ID: "qt_demo_01", LeadID: "lead_sa_fleet_01", CustomerQuery: "HOWO 14-hole 220mm desert quarry",
			ProductID: "gy_p_015", ProductCode: "GYS-126 (HOWO2)", ProductName: "中国重汽 HOWO2 刹车片 (GYS-126)",
			Dimensions: "204.0 × 220.0 × 15 mm (14孔, 铆钉 10*18)", FormulaCode: "GY-CF-400",
			UnitPriceUSD: "$5.10 - $5.85 / 片", UnitPriceCNY: "¥37 - ¥42 / 片", MOQ: "300 片",
			SwapAdvice: "HOWO1 为 8 孔，HOWO2 为 14 孔，宽度同为 220mm，已锁定 14 孔重载规格。",
			BilingualText: "【浙江冠英 Kooying 闪电报价单】GYS-126 (HOWO2) 204×220×15mm 14孔 · 配方 GY-CF-400 (400℃ 磨损 0.040mm) · FOB $5.10-$5.85/片",
			CreatedAt: now.Add(-40 * time.Minute).Format(time.RFC3339),
		},
		{
			ID: "qt_demo_02", LeadID: "lead_cl_parts_02", CustomerQuery: "BPW 19032 180mm 10 holes",
			ProductID: "gy_p_002", ProductCode: "GYS-007 (WVA 19032 / BFMC BC/36/1)", ProductName: "BPW 车桥鼓式刹车片 19032",
			Dimensions: "220.0 × 180.0 × 17.5/10.5 mm (10孔, 铆钉 8*15)", FormulaCode: "GY-NAO-38",
			UnitPriceUSD: "$4.60 - $5.30 / 片", UnitPriceCNY: "¥33 - ¥38 / 片", MOQ: "200 片",
			SwapAdvice: "BPW 常见 180mm (19032) 与 200mm (19094) 两种宽度，已按 180mm 核对确认。",
			BilingualText: "【浙江冠英 Kooying 闪电报价单】GYS-007 (WVA 19032) 220×180×17.5/10.5mm 10孔 · E-Mark GY-NAO-38 · FOB $4.60-$5.30/片",
			CreatedAt: now.Add(-90 * time.Minute).Format(time.RFC3339),
		},
		{
			ID: "qt_demo_03", LeadID: "lead_ru_mining_04", CustomerQuery: "Hande 35T 240x220 20 holes",
			ProductID: "gy_p_034", ProductCode: "GL-HD35T-F (汉德桥35T前)", ProductName: "汉德桥 35T 前轮刹车片",
			Dimensions: "240.0 × 220.0 × 18.5 mm (20孔, 铆钉 8)", FormulaCode: "GY-CF-400",
			UnitPriceUSD: "$6.80 - $7.80 / 片", UnitPriceCNY: "¥48 - ¥56 / 片", MOQ: "100 片",
			SwapAdvice: "20 孔超宽矿用片！前桥宽 220mm，后桥宽 240mm，两者外弧均为 240mm。",
			BilingualText: "【浙江冠英 GOOLE 闪电报价单】汉德35T前 (240×220×18.5mm 20孔) · 配方 GY-CF-400 · FOB $6.80-$7.80/片",
			CreatedAt: now.Add(-3 * time.Hour).Format(time.RFC3339),
		},
	}
	for _, q := range quotes {
		if _, err := sqlDB.ExecContext(ctx, `INSERT INTO guanying_quotes(
			id,lead_id,customer_query,product_id,product_code,product_name,dimensions,
			formula_code,unit_price_usd,unit_price_cny,moq,swap_advice,bilingual_text,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			q.ID, q.LeadID, q.CustomerQuery, q.ProductID, q.ProductCode, q.ProductName,
			q.Dimensions, q.FormulaCode, q.UnitPriceUSD, q.UnitPriceCNY, q.MOQ, q.SwapAdvice, q.BilingualText, q.CreatedAt,
		); err != nil {
			return fmt.Errorf("seed guanying quote %s: %w", q.ID, err)
		}
	}
	return nil
}

func seedSEOPages(ctx context.Context, sqlDB *sql.DB) error {
	var count int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM guanying_seo_pages`).Scan(&count); err == nil && count > 0 {
		return nil
	}
	now := nowISO()
	pages := []SEOPage{
		{
			ID: "seo_wva_disc", Slug: "heavy-duty-disc-brake-pads-wva-29087-29030", Lang: "English (Global)",
			Title:           "Heavy-Duty Commercial Disc Brake Pads WVA 29087 & 29030 | Zhejiang Kooying Factory Direct",
			MetaDescription: "IATF16949 & ECE R90 certified manufacturer in Huzhou, China. 15M sets/year capacity. Asbestos-free NAO & Carbon-Fiber disc brake pads for ZF, MAN, Scania, HOWO & Yutong.",
			TargetKeywords:  "WVA 29087 brake pads supplier, WVA 29030 commercial brake pad factory, IATF16949 heavy duty brake pads China, Kooying",
			TargetMarket:    "欧洲 / 中东 / 南美重卡与客车车队",
			SeriesFocus:     "盘式刹车片 (WVA 29087 / 29030 / 29254 / 29125 / 29143)",
			HeroHeading:     "Asbestos-Free Commercial Disc Brake Pads — WVA 29087 / 29030 Factory Direct",
			HeroSubheading:  "Engineered in Huzhou (43,000㎡, 15M sets/yr). Krauss 400℃ constant-torque wear only 0.040mm (52.9% less wear than industry NC benchmark). OEM supplier for Yutong, Weichai & Shanghai Public Bus.",
			HighlightsJSON:  `["Friction Coefficient 0.35–0.38 stable from 200℃ to 400℃+","400℃ Thickness Wear: 0.040mm vs Industry 0.085mm","Zero Asbestos (GB5763-2018 & E-Mark compliant)","Noise <= 1.6Hz · 100,000km+ Service Life"]`,
			ProductCodes:    "WVA-29087,WVA-29030,WVA-29254,WVA-29125,WVA-29143,GL-DA06",
			SchemaJSONLD: `{
  "@context": "https://schema.org/",
  "@type": "Product",
  "name": "Kooying WVA 29087 Heavy-Duty Commercial Disc Brake Pads",
  "sku": "WVA-29087",
  "brand": { "@type": "Brand", "name": "Kooying / GOOLE (Zhejiang Kooying Auto Parts)" },
  "description": "247.6 x 109.7 x 30mm IATF16949 certified asbestos-free disc brake pads for ZF axles, Scania, Neoplan, Shacman & Yutong.",
  "offers": { "@type": "AggregateOffer", "priceCurrency": "USD", "lowPrice": "16.50", "highPrice": "19.50", "offerCount": "100" }
}`,
			InquiryCount: 14, CreatedAt: now,
		},
		{
			ID: "seo_axle_linings", Slug: "axle-matched-drum-brake-linings-bpw-meritor-hande", Lang: "English / Español",
			Title:           "Axle-Matched Drum Brake Linings for BPW, Meritor, SAF & Hande 35T Axles | Kooying",
			MetaDescription: "Stop aftermarket axle mismatches. Match your brake lining by Outer Arc x Width x Thickness & Hole Count (19032, 19094, 307AF, Hande 35T). Krauss bench-tested factory in China.",
			TargetKeywords:  "BPW 19032 19094 brake lining, Hande 35T brake shoe lining, 307AF-1 307AF-2 supplier, aftermarket axle brake lining",
			TargetMarket:    "南美矿山 / 非洲 / 中亚挂车与重卡售后市场",
			SeriesFocus:     "车桥专配鼓式刹车片 (BPW 19032/19094, SAF 19036, 307AF, 汉德35T, 美驰桥)",
			HeroHeading:     "Exact Axle-Matched Drum Brake Linings — BPW, Meritor, SAF & Hande 35T",
			HeroSubheading:  "No OEM number? Aftermarket axle swapped? Identify exact Kooying / GOOLE brake linings by Arc, Width, Thickness & Rivet Holes in seconds.",
			HighlightsJSON:  `["Covers BPW 180mm (19032) & 200mm (19094) 10-hole series","Hande 35T 20-hole mining series with Carbon-Fiber GY-CF-400","1000T PLC presses + 10-hole automatic drilling lines","1000MPa Q355/Q235 shoe assembly & high-carbon steel rivets"]`,
			ProductCodes:    "GYS-007,GYS-018,GYS-009,GYS-143,GYS-144,GL-HD35T-F,GL-HD35T-R,GL-MERITOR-AB",
			SchemaJSONLD: `{
  "@context": "https://schema.org/",
  "@type": "Product",
  "name": "Kooying Axle-Matched Drum Brake Linings (BPW / Hande 35T / 307AF)",
  "sku": "GYS-007-19032",
  "brand": { "@type": "Brand", "name": "Kooying / GOOLE" },
  "description": "Precision drilled drum brake linings matched by outer arc, width, thickness and rivet pattern for commercial axles.",
  "offers": { "@type": "AggregateOffer", "priceCurrency": "USD", "lowPrice": "3.40", "highPrice": "8.50" }
}`,
			InquiryCount: 9, CreatedAt: now,
		},
		{
			ID: "seo_howo_steyr_eq153", Slug: "chinese-heavy-truck-brake-linings-howo-steyr-jiefang-eq153", Lang: "English / Русский / العربية",
			Title:           "HOWO, STEYR, JIEFANG & DONGFENG EQ153 Brake Linings OEM/Aftermarket | Kooying",
			MetaDescription: "Direct factory supply for SINOTRUK HOWO1/HOWO2 (GYS-125/126), STEYR (GYS-122/123/124), DONGFENG EQ153 & FAW Jiefang Aowei. 30+ formulas for Middle East, Africa & CIS fleets.",
			TargetKeywords:  "HOWO brake lining 14 holes GYS-126, STEYR F2 brake shoe, Dongfeng EQ153 brake lining, Sinotruk brake parts factory",
			TargetMarket:    "中东 / 俄罗斯 / 非洲中资重卡出口保有量市场",
			SeriesFocus:     "中系重卡主力型号 (HOWO1/2, STEYR F1/F2/R, EQ153 F/R, 解放悍威/奥威/天威)",
			HeroHeading:     "HOWO, STEYR, FAW & DONGFENG Brake Linings — Built for Extreme Loads & Heat",
			HeroSubheading:  "8-hole HOWO1 vs 14-hole HOWO2, old EQ153 vs new EQ153 — exact dimension verification with direct Huzhou manufacturing.",
			HighlightsJSON:  `["HOWO 19247 / GYS-125 (8 holes) & GYS-126 (14 holes) ready in stock","Weichai Power OEM supplier since 2022","Tested on North America Chase & European Krauss dynamometers","Fast container mixing for 30+ countries"]`,
			ProductCodes:    "GYS-125,GYS-126,GYS-025,GYS-122,GYS-123,GYS-124,GYS-127,GYS-128,JF-AW-RA,JF-XAW-RA",
			SchemaJSONLD: `{
  "@context": "https://schema.org/",
  "@type": "Product",
  "name": "Kooying Sinotruk HOWO & STEYR Heavy-Duty Brake Linings",
  "sku": "GYS-126",
  "brand": { "@type": "Brand", "name": "Kooying" },
  "offers": { "@type": "AggregateOffer", "priceCurrency": "USD", "lowPrice": "3.10", "highPrice": "6.20" }
}`,
			InquiryCount: 11, CreatedAt: now,
		},
		{
			ID: "seo_asia_bus_truck", Slug: "hino-hyundai-daewoo-yutong-bus-brake-linings", Lang: "English / Bahasa",
			Title:           "HINO, HYUNDAI, DAWOO & YUTONG Bus Brake Linings (Low-Noise NAO) | Kooying",
			MetaDescription: "Low-noise (<=1.6Hz), zero-asbestos bus and truck brake linings for HINO ZY (GYS-119/120), HYUNDAI BUS (GYS-133/135), DAWOO & YUTONG/KINGLONG.",
			TargetKeywords:  "HINO ZY brake lining GYS-119, Hyundai Bus brake lining GYS-133, Yutong Kinglong brake lining supplier",
			TargetMarket:    "东南亚 / 韩国 / 城市公交与客运车队",
			SeriesFocus:     "日韩车系与大型客车 (HINO ZY, HYUNDAI BUS, DAWOO, 宇通/金龙/海格)",
			HeroHeading:     "Quiet, Long-Life Brake Linings for HINO, HYUNDAI, DAWOO & Express Buses",
			HeroSubheading:  "Trusted by Shanghai, Jinan & Yangzhou Public Transit. Stable 0.35–0.38 friction without squeal.",
			HighlightsJSON:  `["GYS-119/120 HINO ZY 8-hole & 12-hole precision fit","GYS-133/135 Hyundai Bus 10-hole & 13-hole series","GYS-129/130 Yutong / Kinglong / Higer OE grade","Eco-friendly dust-free formulation"]`,
			ProductCodes:    "GYS-119,GYS-120,GYS-133,GYS-135,GYS-137,GYS-129,GYS-130,GL-HQ80Q-R",
			SchemaJSONLD: `{
  "@context": "https://schema.org/",
  "@type": "Product",
  "name": "Kooying HINO / HYUNDAI / YUTONG Commercial Bus Brake Linings",
  "sku": "GYS-119",
  "brand": { "@type": "Brand", "name": "Kooying" },
  "offers": { "@type": "AggregateOffer", "priceCurrency": "USD", "lowPrice": "3.65", "highPrice": "6.10" }
}`,
			InquiryCount: 7, CreatedAt: now,
		},
	}
	for _, p := range pages {
		_, err := sqlDB.ExecContext(ctx, `INSERT INTO guanying_seo_pages(
			id,slug,lang,title,meta_description,target_keywords,target_market,series_focus,
			hero_heading,hero_subheading,highlights_json,product_codes,schema_jsonld,inquiry_count,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.ID, p.Slug, p.Lang, p.Title, p.MetaDescription, p.TargetKeywords, p.TargetMarket, p.SeriesFocus,
			p.HeroHeading, p.HeroSubheading, p.HighlightsJSON, p.ProductCodes, p.SchemaJSONLD, p.InquiryCount, p.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("seed seo page %s: %w", p.ID, err)
		}
	}
	return nil
}

func seedContents(ctx context.Context, sqlDB *sql.DB) error {
	var count int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM contents WHERE source LIKE 'guanying-%'`).Scan(&count); err == nil && count > 0 {
		return nil
	}
	now := nowISO()
	drafts := []struct {
		typeName string
		title    string
		caption  string
		body     string
		tags     string
		status   string
		source   string
	}{
		{
			typeName: "article",
			title:    "【LinkedIn 终端车队开发贴】Why Fleet Managers Switch to Kooying WVA 29087 & 400℃ Carbon-Fiber Pads",
			caption:  "面向中东/欧洲重卡车队与汽配连锁的工厂技术背书贴（直击热衰退与安全里程痛点）",
			body:     "Downhill brake fade on 40-ton haulers isn't a driver problem — it's a friction stability problem.\n\nAt Zhejiang Kooying (Huzhou, 43,000㎡ IATF16949 plant, 15M sets/yr), our Krauss 400℃ constant-torque dynamometer test proves:\n• Thickness wear at 400℃: 0.040mm vs Industry NC 0.085mm (-52.9% wear)\n• Weight loss at 400℃: 2.300g vs Industry NC 2.610g\n• Friction coefficient locked at 0.35–0.38 with <=1.6Hz low noise\n\nDirect factory supply for WVA 29087, 29030, BPW 19032/19094, HOWO 14-hole & Hande 35T. Reply with your WVA or Arc x Width x Holes for a 30-second factory quotation.",
			tags:     `["LinkedIn","WVA29087","终端车队开发","海外获客"]`,
			status:   "ready",
			source:   "guanying-linkedin",
		},
		{
			typeName: "video_script",
			title:    "【TikTok / YouTube 短视频脚本】HOWO1 (8孔) vs HOWO2 (14孔)：15秒教海外汽配店老板永不买错刹车片",
			caption:  "解决后市场换桥、同车不同码痛点，评论区自动引导发尺寸/拍照询盘",
			body:     "【0-3秒 强钩子】Same SINOTRUK HOWO truck, same 220mm width — why do half of aftermarket orders fail? Look at the rivet holes!\n【4-10秒 实拍对比】Left is GYS-125 (HOWO1): 201×220×15mm, 8 holes. Right is GYS-126 (HOWO2): 204×220×15mm, 14 holes, 10*18 rivets.\n【11-15秒 行动引导】Don't guess by truck model after axle swaps! Comment your Outer Arc × Width × Holes or click our bio link for instant Kooying catalog matching.",
			tags:     `["TikTok","YouTube","HOWO","后市场防错","评论区截流"]`,
			status:   "ready",
			source:   "guanying-video",
		},
		{
			typeName: "article",
			title:    "【SEO 专题长文】How to Identify Commercial Brake Linings Without an OEM Number (Arc, Width, Thickness & Hole Guide)",
			caption:  "英文独立站自然流量核心长尾词支柱页（承接无 OE 号搜尺寸/车桥的精准买家）",
			body:     "1. Measure Outer Arc (A) along the outside curve in mm.\n2. Measure Width (C) and Thickness (D) — note tapered linings like 17.5/10.5mm (BPW 19032) or 16/20mm (Jiefang New Aowei).\n3. Count Rivet Holes (#) & check ABS sensor holes: e.g. EQ153 Old (6 holes, 130mm) vs New EQ153 (10 holes, 150mm); 307AF-1 (10 holes) vs 307AF-2 (12 holes).\n4. Submit measurements on Kooying's instant matcher to lock GYS/GY SKU and E-Mark / GB5763-2018 certified pricing.",
			tags:     `["SEO长文","独立站引流","选型SOP","307AF","BPW"]`,
			status:   "ready",
			source:   "guanying-seo",
		},
		{
			typeName: "article",
			title:    "【WhatsApp 跨时差防漏单首响模板】中东/拉美凌晨询盘 30 秒自动确认话术（英文+西语+阿语摘要）",
			caption:  "解决 10 条/天询盘因时差漏单与 2 小时核对报价痛点",
			body:     "Hi [Buyer Name], thank you for reaching Zhejiang Kooying (15M sets/yr IATF16949 factory).\nBased on your inquiry ([Vehicle/Axle/WVA]), we preliminary matched:\n• SKU: [GYS/WVA Code] ([OuterArc × Width × Thickness, Holes])\n• Recommended Formula: [GY-CF-400 / GY-NAO-38] (400℃ wear 0.040mm)\n• Quick Check: If your truck had an aftermarket axle swap, please reply with the rivet hole count & width (mm) so we lock 100% fitment and send FOB Ningbo quotation right away.",
			tags:     `["WhatsApp","跨时差首响","闪电报价","防漏单"]`,
			status:   "ready",
			source:   "guanying-whatsapp",
		},
		{
			typeName: "article",
			title:    "【国内门店推广图文】解放奥威/新奥威后桥换了怎么买刹车片？冠隆 165 vs 208 实测一图看懂",
			caption:  "赋能一线国内销售在汽配店当场答复，降低对样件的依赖",
			body:     "很多老解放奥威在修理厂换过新奥威车桥后，只报车型肯定买错片！记住冠隆一线销售三句话口诀：\n1) 老奥威后A(ABS)：外弧 165 × 宽 220，12孔；\n2) 新奥威后A(ABS)：外弧 208 × 宽 200，16孔（另有220加宽版）；\n3) 拿不准用卷尺量外弧和宽度 + 数孔数，10秒在冠英选型库出结果并按 GY-CF-400 重载配方当场报价。",
			tags:     `["国内售后","解放奥威","换桥防错","一线销售"]`,
			status:   "ready",
			source:   "guanying-domestic",
		},
		{
			typeName: "article",
			title:    "【主机与大客户背书简报】浙江冠英湖州南太湖智造基地：克劳斯/Chase 台架与公交矿卡配套实力",
			caption:  "用于大客户投标、独立站 About Us 及行业展会分发",
			body:     "浙江冠英汽车零部件有限公司始创于2006年，2019年湖州南太湖基地投产（总投资1.8亿元，43,000㎡）。配备1000T四柱三梁全自动压机、自动配拌料称料线、2台欧洲克劳斯试验机、4台定速试验机及北美Chase试验机。已配套潍柴动力、丰奥车桥及上海/济南/扬州公交系统，是您稳定可靠的商用车制动摩擦材料长期伙伴。",
			tags:     `["品牌背书","克劳斯试验机","潍柴配套","湖州智造"]`,
			status:   "ready",
			source:   "guanying-brand",
		},
	}
	for _, d := range drafts {
		_, err := sqlDB.ExecContext(ctx, `INSERT INTO contents(
			type,title,caption,body,tags_json,status,source,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
			d.typeName, d.title, d.caption, d.body, d.tags, d.status, d.source, now, now,
		)
		if err != nil {
			return fmt.Errorf("seed guanying content: %w", err)
		}
	}
	return nil
}

func seedKnowledgeBase(ctx context.Context, database *db.DB, dataDir string) error {
	bases, err := database.ListKnowledgeBases()
	if err != nil {
		return err
	}
	var baseID int64
	for _, b := range bases {
		if b.Name == "浙江冠英·品牌与制动技术知识库" {
			baseID = b.ID
			break
		}
	}
	if baseID == 0 {
		created, err := database.CreateKnowledgeBase("浙江冠英·品牌与制动技术知识库", "1:1 来自浙江冠英 2026 企业介绍、Kooying 英文目录及冠隆产品册的官方资料与台架参数")
		if err != nil {
			return fmt.Errorf("create guanying knowledge base: %w", err)
		}
		baseID = created.ID
	}

	docs, err := database.ListKnowledgeDocuments(baseID)
	if err == nil && len(docs) >= 4 {
		return nil
	}

	kbDir := filepath.Join(dataDir, "guanying-kb-docs")
	if err := os.MkdirAll(kbDir, 0o755); err != nil {
		return err
	}

	files := []struct {
		name    string
		content string
	}{
		{
			name: "01_2026年浙江冠英企业介绍与5大品牌战略.md",
			content: `# 浙江冠英汽车零部件有限公司（Zhejiang Kooying Auto Parts Co., Ltd.）

## 基本概况
- **创立时间与基地**：始创于 2006 年，2018-2019 年在浙江省湖州市南太湖新区建成总投资 1.8 亿元、建筑面积 43,000 平方米的现代化生产基地，员工 100 余人（研发团队 18 人，平均在职 10 年）。
- **产能规模**：年产商用车刹车片与摩擦材料 **1500 万套**（刹车片及风电制动块 10,000 余吨、制动蹄总成 60 万套），零件号超过 1000+，服务国内 18 个办事处及全球 30+ 国家和地区（东南亚、中东、南美、北美、欧洲、非洲）。
- **旗下 5+ 核心品牌**：**冠英 (Kooying)**、**冠隆 (GOOLE)**、**新莱 (NEWLINE)**、**圣雕 (SENDIA)**、**卡能 (KANON)**、**三安 (SANAN)**。
- **权威认证与配套客户**：IATF 16949:2016 质量管理体系认证、国家强制性 3C 认证、欧洲 E-Mark / ECE 法规适配、国家高新技术企业。2020 年与丰奥等车桥厂配套，2022 年与潍柴动力配套并新增盘式及风电刹车片产线，2023 年与上海公交、济南公交、扬州公交配套。
- **产学研合作**：浙江大学战略合作伙伴、国防科技大学合作、中国地质大学（武汉）纳米矿物纤维联合研究。
`,
		},
		{
			name: "02_冠英3大无石棉配方与克劳斯400度台架检测报告.md",
			content: `# 冠英摩擦材料配方体系与克劳斯/Chase台架检测数据

## 法规红线与配方优势
- 严格执行 **GB 5763-2018** 国家标准，**100% 无石棉**（半金属、NAO、陶瓷、碳纤维复合材料），对标 SAE & ECE 国际标准。
- **3 大系列 30+ 种成熟配方**：
  1. ` + "`GY-NAO-38`" + `（出口低噪长寿无石棉配方）：摩擦系数稳定 0.35~0.38，噪音 <=1.6Hz，寿命 6M/100,000km+，适合中东/东南亚/欧洲重卡与城市公交。
  2. ` + "`GY-CF-400`" + `（重载矿山高碳纤维抗热衰退配方）：400℃+ 连续制动热衰退控制极佳，适合矿卡、自卸车、汉德 35T / 潍柴矿车。
  3. ` + "`GY-STD-35`" + `（国标高性价比耐用配方）：符合 QC/T25-2004，适合国内售后及标载干线物流。

## 冠英 vs 行业参照 (NC) 真实台架对比（企业介绍 P25）
- **定速重量磨损**：冠英 ` + "`0.580 g`" + ` vs NC ` + "`0.650 g`" + `（更耐磨 -0.070 g）
- **硬度 HRM/10**：冠英 ` + "`8.442`" + ` vs NC ` + "`6.510`" + `
- **摩擦系数 uMax / uMin**：冠英 ` + "`0.390 / 0.200`" + ` vs NC ` + "`0.290 / 0.170`" + `（制动力更强劲稳定）
- **400℃ 厚度磨损**：冠英 ` + "`0.040 mm`" + ` vs NC ` + "`0.085 mm`" + `（高温磨损降低 52.9%）
- **400℃ 磨损失重**：冠英 ` + "`2.300 g`" + ` vs NC ` + "`2.610 g`" + `（高温失重降低 0.310 g）

## 制动蹄总成 BOM 工艺
- 蹄铁材质 Q235/Q355/Q288 激光切割并电泳，交口最大变形应力 >= 1000 MPa；BPW 车轴采用抗拉强度 >= 1000 MPa 的高碳钢铆钉（铆接气压 3.5-7.0 MPa）。
`,
		},
		{
			name: "03_Kooying外贸商用车刹车片选型总表.md",
			content: buildCatalogMarkdown("外贸 Kooying Catalog 主力选型对照", "kooying"),
		},
		{
			name: "04_冠隆内贸车桥与重型盘式刹车片选型手册.md",
			content: buildCatalogMarkdown("冠隆内贸车桥与 WVA 29xxx 盘式刹车片手册", "guanlong"),
		},
	}

	inputFiles := make([]knowledge.InputFile, 0, len(files))
	for _, f := range files {
		p := filepath.Join(kbDir, f.name)
		if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
			return err
		}
		inputFiles = append(inputFiles, knowledge.InputFile{Name: f.name, Path: p})
	}
	importer := knowledge.NewImporter(database)
	if _, err := importer.Import(ctx, knowledge.ImportRequest{
		BaseID: baseID,
		Files:  inputFiles,
	}); err != nil {
		return fmt.Errorf("import guanying knowledge docs: %w", err)
	}
	return nil
}

func buildCatalogMarkdown(title, scope string) string {
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	b.WriteString("| 冠英编码 | WVA/车型代号 | 类别 | 外弧×宽度×厚度(mm) | 孔数/铆钉 | 适配车型与车桥 | 换桥防错提醒 | 来源页 |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for _, p := range OfficialCatalogProducts {
		isKooying := strings.HasPrefix(p.CatalogPage, "kooying")
		if scope == "kooying" && !isKooying {
			continue
		}
		if scope == "guanlong" && isKooying {
			continue
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %.1f×%.1f×%s | %d孔 / %s | %s | %s | %s |\n",
			p.Code, p.OEOrWVA, p.Category, p.OuterArcMM, p.WidthMM, p.ThicknessStr, p.Holes, p.RivetSpec,
			p.ApplicableModels, p.SwapWarning, p.CatalogPage))
	}
	return b.String()
}

func ensureFeishuConfig(dataDir string) {
	dst := filepath.Join(dataDir, "feishu.json")
	if _, err := os.Stat(dst); err == nil {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	src := filepath.Join(home, ".alpha-nexus-egolite", "feishu.json")
	raw, err := os.ReadFile(src)
	if err != nil || len(raw) == 0 {
		return
	}
	_ = os.WriteFile(dst, raw, 0o600)
}

func GetOverview(ctx context.Context, sqlDB *sql.DB) (*Overview, error) {
	out := &Overview{
		Products: make([]OfficialProduct, 0),
		Formulas: append([]FormulaSpec(nil), OfficialFormulas...),
		Leads:    make([]Lead, 0),
		Quotes:   make([]Quote, 0),
		SEOPages: make([]SEOPage, 0),
	}

	pRows, err := sqlDB.QueryContext(ctx, `SELECT id,code,oe_or_wva,name,brand,category,vehicle_series,
		outer_arc_mm,inner_arc_mm,width_mm,thickness_str,thickness_mm,drum_or_taper,holes,rivet_spec,
		applicable_models,swap_warning,formula_code,price_fob_usd,price_cny,moq,catalog_page,image_url
		FROM guanying_products ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for pRows.Next() {
		var p OfficialProduct
		if err := pRows.Scan(&p.ID, &p.Code, &p.OEOrWVA, &p.Name, &p.Brand, &p.Category, &p.VehicleSeries,
			&p.OuterArcMM, &p.InnerArcMM, &p.WidthMM, &p.ThicknessStr, &p.ThicknessMM, &p.DrumOrTaper,
			&p.Holes, &p.RivetSpec, &p.ApplicableModels, &p.SwapWarning, &p.FormulaCode, &p.PriceFOBUSD,
			&p.PriceCNY, &p.MOQ, &p.CatalogPage, &p.ImageURL); err != nil {
			_ = pRows.Close()
			return nil, err
		}
		out.Products = append(out.Products, p)
	}
	_ = pRows.Close()

	lRows, err := sqlDB.QueryContext(ctx, `SELECT id,company,country,channel,timezone,buyer_type,confidence,is_terminal,
		inquiry_text,matched_code,matched_product,status,filter_reason,created_at
		FROM guanying_leads ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	for lRows.Next() {
		var l Lead
		var isTerm int
		if err := lRows.Scan(&l.ID, &l.Company, &l.Country, &l.Channel, &l.Timezone, &l.BuyerType, &l.Confidence,
			&isTerm, &l.InquiryText, &l.MatchedCode, &l.MatchedProduct, &l.Status, &l.FilterReason, &l.CreatedAt); err != nil {
			_ = lRows.Close()
			return nil, err
		}
		l.IsTerminal = isTerm != 0
		if l.IsTerminal {
			out.Stats.TerminalLeadCount++
		} else {
			out.Stats.FilteredTraderCnt++
		}
		out.Leads = append(out.Leads, l)
	}
	_ = lRows.Close()

	qRows, err := sqlDB.QueryContext(ctx, `SELECT id,lead_id,customer_query,product_id,product_code,product_name,
		dimensions,formula_code,unit_price_usd,unit_price_cny,moq,swap_advice,bilingual_text,created_at
		FROM guanying_quotes ORDER BY created_at DESC LIMIT 30`)
	if err == nil {
		for qRows.Next() {
			var q Quote
			if scanErr := qRows.Scan(&q.ID, &q.LeadID, &q.CustomerQuery, &q.ProductID, &q.ProductCode, &q.ProductName,
				&q.Dimensions, &q.FormulaCode, &q.UnitPriceUSD, &q.UnitPriceCNY, &q.MOQ, &q.SwapAdvice, &q.BilingualText, &q.CreatedAt); scanErr == nil {
				out.Quotes = append(out.Quotes, q)
			}
		}
		_ = qRows.Close()
	}

	sRows, err := sqlDB.QueryContext(ctx, `SELECT id,slug,lang,title,meta_description,target_keywords,target_market,
		series_focus,hero_heading,hero_subheading,highlights_json,product_codes,schema_jsonld,inquiry_count,created_at
		FROM guanying_seo_pages ORDER BY created_at DESC`)
	if err == nil {
		for sRows.Next() {
			var sp SEOPage
			if scanErr := sRows.Scan(&sp.ID, &sp.Slug, &sp.Lang, &sp.Title, &sp.MetaDescription, &sp.TargetKeywords,
				&sp.TargetMarket, &sp.SeriesFocus, &sp.HeroHeading, &sp.HeroSubheading, &sp.HighlightsJSON, &sp.ProductCodes,
				&sp.SchemaJSONLD, &sp.InquiryCount, &sp.CreatedAt); scanErr == nil {
				out.SEOPages = append(out.SEOPages, sp)
			}
		}
		_ = sRows.Close()
	}

	out.Stats.ProductCount = len(out.Products)
	out.Stats.QuoteCount = len(out.Quotes)
	out.Stats.SEOPageCount = len(out.SEOPages)
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM contents`).Scan(&out.Stats.ContentCount)
	_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM knowledge_documents WHERE status='ready'`).Scan(&out.Stats.KnowledgeDocCount)
	return out, nil
}

type MatchRequest struct {
	Query      string  `json:"query"`
	OuterArcMM float64 `json:"outer_arc_mm"`
	WidthMM    float64 `json:"width_mm"`
	Thickness  float64 `json:"thickness_mm"`
	Holes      int     `json:"holes"`
	ProductID  string  `json:"product_id"`
	LeadID     string  `json:"lead_id"`
	SaveQuote  bool    `json:"save_quote"`
}

type MatchCandidate struct {
	Product OfficialProduct `json:"product"`
	Score   float64         `json:"score"`
	Reason  string          `json:"reason"`
}

type MatchResponse struct {
	Matches []MatchCandidate `json:"matches"`
	Quote   *Quote           `json:"quote,omitempty"`
	Formula *FormulaSpec     `json:"formula,omitempty"`
}

func MatchAndQuote(ctx context.Context, sqlDB *sql.DB, req MatchRequest) (*MatchResponse, error) {
	overview, err := GetOverview(ctx, sqlDB)
	if err != nil {
		return nil, err
	}
	queryLower := strings.ToLower(strings.TrimSpace(req.Query))
	tokens := strings.Fields(strings.NewReplacer(",", " ", "，", " ", "/", " ", "×", " ", "*", " ", "x", " ").Replace(queryLower))

	candidates := make([]MatchCandidate, 0, len(overview.Products))
	for _, p := range overview.Products {
		if req.ProductID != "" && p.ID == req.ProductID {
			candidates = append(candidates, MatchCandidate{Product: p, Score: 1000, Reason: "精确指定官方型号"})
			continue
		}
		score := 0.0
		reasons := make([]string, 0)
		haystack := strings.ToLower(strings.Join([]string{
			p.Code, p.OEOrWVA, p.Name, p.Brand, p.Category, p.VehicleSeries, p.ApplicableModels, p.SwapWarning,
		}, " "))

		if queryLower != "" {
			if strings.Contains(strings.ToLower(p.Code), queryLower) || strings.Contains(strings.ToLower(p.OEOrWVA), queryLower) {
				score += 120
				reasons = append(reasons, "精确命中型号/WVA编码")
			}
			for _, tok := range tokens {
				if len(tok) < 2 {
					continue
				}
				if strings.Contains(haystack, tok) {
					score += 35
					reasons = append(reasons, "关键词命中:"+tok)
				}
			}
		}

		// Geometric dimension tolerance matching (±5mm)
		dimMatched := false
		if req.OuterArcMM > 0 && p.OuterArcMM > 0 {
			diff := math.Abs(p.OuterArcMM - req.OuterArcMM)
			if diff <= 5 {
				score += 65 - diff*8
				reasons = append(reasons, fmt.Sprintf("外弧 %.1fmm 吻合(差%.1f)", p.OuterArcMM, diff))
				dimMatched = true
			}
		}
		if req.WidthMM > 0 && p.WidthMM > 0 {
			diff := math.Abs(p.WidthMM - req.WidthMM)
			if diff <= 5 {
				score += 65 - diff*8
				reasons = append(reasons, fmt.Sprintf("宽度 %.1fmm 吻合(差%.1f)", p.WidthMM, diff))
				dimMatched = true
			}
		}
		if req.Thickness > 0 && p.ThicknessMM > 0 {
			diff := math.Abs(p.ThicknessMM - req.Thickness)
			if diff <= 3 {
				score += 35 - diff*5
				reasons = append(reasons, fmt.Sprintf("厚度 %smm 匹配", p.ThicknessStr))
			}
		}
		if req.Holes > 0 && p.Holes == req.Holes {
			score += 70
			reasons = append(reasons, fmt.Sprintf("孔数 %d 孔一致", p.Holes))
			dimMatched = true
		}

		if queryLower == "" && !dimMatched && req.OuterArcMM == 0 && req.WidthMM == 0 && req.Holes == 0 {
			score = 10
		}
		if score > 0 {
			if len(reasons) == 0 {
				reasons = append(reasons, "同车系候选")
			}
			candidates = append(candidates, MatchCandidate{
				Product: p,
				Score:   score,
				Reason:  strings.Join(reasons, " · "),
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Product.ID < candidates[j].Product.ID
	})
	if len(candidates) > 8 {
		candidates = candidates[:8]
	}

	resp := &MatchResponse{Matches: candidates}
	if len(candidates) > 0 {
		top := candidates[0].Product
		var matchedFormula *FormulaSpec
		for i := range OfficialFormulas {
			if OfficialFormulas[i].Code == top.FormulaCode {
				matchedFormula = &OfficialFormulas[i]
				break
			}
		}
		if matchedFormula == nil {
			matchedFormula = &OfficialFormulas[0]
		}
		resp.Formula = matchedFormula

		dims := fmt.Sprintf("%.1f × %.1f × %s mm (%d孔, 铆钉 %s)", top.OuterArcMM, top.WidthMM, top.ThicknessStr, top.Holes, top.RivetSpec)
		if top.Category == "盘式刹车片" {
			dims = fmt.Sprintf("%.1f × %.1f × %s mm (盘刹总成)", top.OuterArcMM, top.WidthMM, top.ThicknessStr)
		}
		bilingual := fmt.Sprintf(`【浙江冠英 Kooying / GOOLE 闪电选型报价单】
1. 命中产品：%s（%s / %s）
2. 几何参数：%s | 适配：%s
3. 换桥防错核对：%s
4. 推荐摩擦配方：%s（%s）
   - 认证与台架依据：%s；摩擦系数 %s；400℃ 厚度磨损 %s
5. 阶梯参考价：FOB %s ｜ 国内 %s ｜ MOQ：%s ｜ 来源：%s

--- English Reply for Buyer (WhatsApp / Email) ---
Dear Partner,
For your inquiry (%s), Zhejiang Kooying Auto Parts (IATF16949 & E-Mark certified, 15M sets/yr) recommends:
• Model: %s (%s) — %s
• Dimensions: %s
• Friction Formula: %s (Krauss 400℃ wear only 0.040mm vs 0.085mm benchmark, friction 0.35–0.38, zero asbestos)
• Indicative FOB Ningbo/Shanghai Price: %s (MOQ: %s, Lead time: 15–20 days)
Fitment Check: %s`,
			top.Name, top.Code, top.OEOrWVA, dims, top.ApplicableModels, top.SwapWarning,
			matchedFormula.Code, matchedFormula.Name, matchedFormula.Certification, matchedFormula.FrictionRange, matchedFormula.Wear400C,
			top.PriceFOBUSD, top.PriceCNY, top.MOQ, top.CatalogPage,
			strings.TrimSpace(req.Query), top.Code, top.OEOrWVA, top.ApplicableModels, dims,
			matchedFormula.Code, top.PriceFOBUSD, top.MOQ, top.SwapWarning,
		)

		quote := &Quote{
			ID:            newID("qt"),
			LeadID:        req.LeadID,
			CustomerQuery: req.Query,
			ProductID:     top.ID,
			ProductCode:   top.Code + " (" + top.OEOrWVA + ")",
			ProductName:   top.Name,
			Dimensions:    dims,
			FormulaCode:   matchedFormula.Code,
			UnitPriceUSD:  top.PriceFOBUSD,
			UnitPriceCNY:  top.PriceCNY,
			MOQ:           top.MOQ,
			SwapAdvice:    top.SwapWarning,
			BilingualText: bilingual,
			CreatedAt:     nowISO(),
		}
		resp.Quote = quote

		if req.SaveQuote {
			_, _ = sqlDB.ExecContext(ctx, `INSERT INTO guanying_quotes(
				id,lead_id,customer_query,product_id,product_code,product_name,dimensions,
				formula_code,unit_price_usd,unit_price_cny,moq,swap_advice,bilingual_text,created_at
			) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				quote.ID, quote.LeadID, quote.CustomerQuery, quote.ProductID, quote.ProductCode, quote.ProductName,
				quote.Dimensions, quote.FormulaCode, quote.UnitPriceUSD, quote.UnitPriceCNY, quote.MOQ,
				quote.SwapAdvice, quote.BilingualText, quote.CreatedAt,
			)
			if req.LeadID != "" {
				_, _ = sqlDB.ExecContext(ctx, `UPDATE guanying_leads SET status='已出报价单' WHERE id=?`, req.LeadID)
			}
		}
	}
	return resp, nil
}

type SubmitLeadRequest struct {
	Company     string `json:"company"`
	Country     string `json:"country"`
	Channel     string `json:"channel"`
	InquiryText string `json:"inquiry_text"`
	SEOPageID   string `json:"seo_page_id"`
}

func SubmitLead(ctx context.Context, sqlDB *sql.DB, req SubmitLeadRequest) (*Lead, error) {
	company := strings.TrimSpace(req.Company)
	inquiry := strings.TrimSpace(req.InquiryText)
	if company == "" || inquiry == "" {
		return nil, fmt.Errorf("company and inquiry_text are required")
	}
	country := strings.TrimSpace(req.Country)
	if country == "" {
		country = "Global"
	}
	channel := strings.TrimSpace(req.Channel)
	if channel == "" {
		channel = "SEO独立站询盘"
	}

	lower := strings.ToLower(company + " " + inquiry)
	// Evaluate terminal buyer signals vs trading noise
	isTrader := (strings.Contains(lower, "trading") || strings.Contains(lower, "imp & exp") || strings.Contains(lower, "general merchandise")) &&
		!strings.Contains(lower, "fleet") && !strings.Contains(lower, "axle") && !strings.Contains(lower, "wva") && !strings.Contains(lower, "howo") && !strings.Contains(lower, "hino")

	buyerType := "终端车队/汽配商"
	confidence := 89
	status := "待报价"
	filterReason := "独立站精确型号/工况询盘，具备终端买家采购特征。"
	if isTrader {
		buyerType = "贸易商(已过滤)"
		confidence = 26
		status = "已过滤噪音"
		filterReason = "检测到综合贸易中介话术且缺少具体车桥/WVA/孔数参数，AI 自动标记过滤。"
	} else {
		if strings.Contains(lower, "fleet") || strings.Contains(lower, "bus") || strings.Contains(lower, "transport") || strings.Contains(lower, "车队") {
			buyerType = "终端车队"
			confidence = 95
		} else if strings.Contains(lower, "parts") || strings.Contains(lower, "auto") || strings.Contains(lower, "汽配") {
			buyerType = "汽配连锁"
			confidence = 92
		} else if strings.Contains(lower, "depot") || strings.Contains(lower, "repair") || strings.Contains(lower, "axle") || strings.Contains(lower, "矿") {
			buyerType = "车桥/大修厂"
			confidence = 94
		}
	}

	matchedCode := "-"
	matchedProduct := "待补充具体尺寸或车桥"
	if matchResp, err := MatchAndQuote(ctx, sqlDB, MatchRequest{Query: inquiry}); err == nil && len(matchResp.Matches) > 0 {
		top := matchResp.Matches[0].Product
		matchedCode = top.Code
		matchedProduct = top.Name + " (" + top.OEOrWVA + ")"
	}

	lead := &Lead{
		ID:             newID("lead"),
		Company:        company,
		Country:        country,
		Channel:        channel,
		Timezone:       "实时接收",
		BuyerType:      buyerType,
		Confidence:     confidence,
		IsTerminal:     !isTrader,
		InquiryText:    inquiry,
		MatchedCode:    matchedCode,
		MatchedProduct: matchedProduct,
		Status:         status,
		FilterReason:   filterReason,
		CreatedAt:      nowISO(),
	}
	isTerm := 0
	if lead.IsTerminal {
		isTerm = 1
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO guanying_leads(
		id,company,country,channel,timezone,buyer_type,confidence,is_terminal,
		inquiry_text,matched_code,matched_product,status,filter_reason,created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		lead.ID, lead.Company, lead.Country, lead.Channel, lead.Timezone, lead.BuyerType,
		lead.Confidence, isTerm, lead.InquiryText, lead.MatchedCode, lead.MatchedProduct,
		lead.Status, lead.FilterReason, lead.CreatedAt,
	); err != nil {
		return nil, err
	}
	if req.SEOPageID != "" {
		_, _ = sqlDB.ExecContext(ctx, `UPDATE guanying_seo_pages SET inquiry_count = inquiry_count + 1 WHERE id=?`, req.SEOPageID)
	}
	return lead, nil
}

type CreateSEOPageRequest struct {
	SeriesFocus    string `json:"series_focus"`
	TargetMarket   string `json:"target_market"`
	Lang           string `json:"lang"`
	TargetKeywords string `json:"target_keywords"`
}

func CreateSEOPage(ctx context.Context, sqlDB *sql.DB, req CreateSEOPageRequest) (*SEOPage, error) {
	series := strings.TrimSpace(req.SeriesFocus)
	if series == "" {
		series = "HOWO & Hande 35T Heavy-Duty Brake Linings"
	}
	market := strings.TrimSpace(req.TargetMarket)
	if market == "" {
		market = "中东 / 南美 / 东南亚"
	}
	lang := strings.TrimSpace(req.Lang)
	if lang == "" {
		lang = "English"
	}
	kw := strings.TrimSpace(req.TargetKeywords)
	if kw == "" {
		kw = series + ", IATF16949 brake lining supplier China, Kooying factory"
	}

	id := newID("seo")
	slug := "kooying-" + strings.ToLower(strings.ReplaceAll(id, "_", "-"))
	title := fmt.Sprintf("%s | Zhejiang Kooying Factory Direct (%s)", series, market)
	hero := fmt.Sprintf("%s — Factory Direct for %s", series, market)
	sub := "Manufactured by Zhejiang Kooying Auto Parts (Huzhou, 43,000㎡, 15M sets/yr). Krauss 400℃ wear only 0.040mm. Instant fitment check by Vehicle, Axle or Dimensions."

	schemaObj := map[string]any{
		"@context": "https://schema.org/",
		"@type":    "Product",
		"name":     series,
		"brand":    map[string]string{"@type": "Brand", "name": "Kooying / GOOLE"},
		"description": sub,
		"offers": map[string]string{
			"@type":         "AggregateOffer",
			"priceCurrency": "USD",
			"lowPrice":      "3.40",
			"highPrice":     "19.50",
		},
	}
	schemaBytes, _ := json.MarshalIndent(schemaObj, "", "  ")

	page := &SEOPage{
		ID:              id,
		Slug:            slug,
		Lang:            lang,
		Title:           title,
		MetaDescription: sub,
		TargetKeywords:  kw,
		TargetMarket:    market,
		SeriesFocus:     series,
		HeroHeading:     hero,
		HeroSubheading:  sub,
		HighlightsJSON:  `["IATF16949 & ECE R90 / GB5763-2018 Asbestos-Free","400℃ Thickness Wear 0.040mm vs Industry 0.085mm","Weichai Power & Public Bus Verified Supplier","30-Second Quote by Axle / WVA / Arc×Width×Holes"]`,
		ProductCodes:    "GYS-126,WVA-29087,GYS-007,GL-HD35T-F",
		SchemaJSONLD:    string(schemaBytes),
		InquiryCount:    0,
		CreatedAt:       nowISO(),
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO guanying_seo_pages(
		id,slug,lang,title,meta_description,target_keywords,target_market,series_focus,
		hero_heading,hero_subheading,highlights_json,product_codes,schema_jsonld,inquiry_count,created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		page.ID, page.Slug, page.Lang, page.Title, page.MetaDescription, page.TargetKeywords,
		page.TargetMarket, page.SeriesFocus, page.HeroHeading, page.HeroSubheading, page.HighlightsJSON,
		page.ProductCodes, page.SchemaJSONLD, page.InquiryCount, page.CreatedAt,
	); err != nil {
		return nil, err
	}
	return page, nil
}
