package guanying

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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
		`CREATE TABLE IF NOT EXISTS guanying_site_logs (
			id TEXT PRIMARY KEY,
			category TEXT NOT NULL,
			title TEXT NOT NULL,
			target_path TEXT NOT NULL,
			page_url TEXT NOT NULL,
			summary TEXT NOT NULL,
			metrics_json TEXT NOT NULL DEFAULT '{}',
			operator_bot TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'verified',
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
	if err := seedSiteMaintenanceLogs(ctx, sqlDB); err != nil {
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
	hasSiteDoc := false
	if err == nil {
		for _, doc := range docs {
			if strings.Contains(doc.SourceName, "05_KOOYING品牌独立站") {
				hasSiteDoc = true
				break
			}
		}
		if len(docs) >= 5 && hasSiteDoc {
			return nil
		}
	}

	kbDir := filepath.Join(dataDir, "guanying-kb-docs")
	if err := os.MkdirAll(kbDir, 0o755); err != nil {
		return err
	}

	allFiles := []struct {
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
		{
			name: "05_KOOYING品牌独立站架构与135款实拍资产维护手册.md",
			content: `# KOOYING 海外极简品牌独立站与 SEO/GEO 运维手册

## 1. 站点定位与本地挂载
- **物理路径**：` + "`web/`" + `（设计总纲 [` + "`web/PLAN.md`" + `]）
- **工作台挂载地址**：` + "`http://127.0.0.1:17422/brand-site/index.html`" + `
- **对标标准**：德国 Textar / Brakebook、意大利 Brembo Commercial、Fras-le，采用瑞士工业极简低密度排版（Warm Alabaster ` + "`#F5F4F0`" + ` + Carbon Friction ` + "`#111315`" + ` + Thermal Copper ` + "`#C84B21`" + `）。

## 2. 页面矩阵（6 大纯静态可抓取 HTML + 搜索基建）
1. ` + "`/brand-site/index.html`" + `：7 屏极简品牌主页（Hero 三视图切面 · 四大海外市场工况 · 三大摩擦产线 · BrakeFinder™ 选型器 · 400℃ Krauss 台架实证 · 五品牌与湖州基地 · Direct Factory RFQ 询盘直连）。
2. ` + "`/brand-site/catalog.html`" + `：全量 135 款实拍型号交互目录（支持 WVA/车桥/市场/外弧×宽×厚+孔数 ` + "`±4.5mm`" + ` 容差筛选）。
3. ` + "`/brand-site/seo/wva-29087-29162-air-disc-brake-pads.html`" + `：中东/欧洲重卡空气盘式刹车片专题（WVA 29087 / 29162 / 29030 / BPW 8201）。
4. ` + "`/brand-site/seo/trailer-axle-brake-linings-bpw-meritor-fuwa.html`" + `：全球挂车与换桥选型专题（BPW 11.5T–16T BC36-1~BC39-1 / Meritor 美驰 / Fuwa 富华 / Hande 汉德 35T）。
5. ` + "`/brand-site/seo/heavy-duty-drum-linings-hino-howo-scania.html`" + `：东南亚/南美重卡鼓刹专题（HINO / ISUZU / HYUNDAI / SCANIA 19932 / HOWO / STEYR）。
6. ` + "`/brand-site/seo/friction-material-formulas-krauss-testing.html`" + `：30+ 无石棉配方与 400℃ 克劳斯台架对比白皮书（GEO 核心引用页）。
7. **搜索与 AI 引用基建**：` + "`sitemap.xml`" + `（含 ` + "`<image:image>`" + `）、` + "`robots.txt`" + `（放行 ` + "`Googlebot / Bingbot / OAI-SearchBot / GPTBot / PerplexityBot / ClaudeBot`" + `）、` + "`llms.txt`" + `。

## 3. 135 款实拍图库规范与常见排障规则
- **数据真源**：` + "`web/assets/data/products-catalog.json`" + `（由 ` + "`web/scripts/unpack_and_build_assets.py`" + ` 生成）。
- **资产统计**：41 款盘刹（126 张三视图）+ 94 款去重鼓刹（184 张实拍）+ 5 大品牌 ` + "`29162`" + ` 渲染图 + ` + "`5200.5300.mp4`" + `（已压制为 3.9MB Fast-Start）+ 9 张无边框厂区/认证原图 + 8 张工程尺寸图。
- **实拍视角约定**：盘刹摄影命名中 ` + "`*-反.JPG`" + ` 为摩擦材料正面（Front Friction Face），` + "`*-正.JPG`" + ` 为钢背激光喷码面（Backing Plate），` + "`*-侧.JPG`" + ` 为侧面厚度（Profile）。
- **询盘回流链路**：独立站 RFQ 表单（` + "`web/assets/js/site.js`" + `）直接 POST ` + "`/api/guanying/leads`" + `，触发终端买家 AI 评分与型号匹配并写入 ` + "`guanying_leads`" + `，可在「产品与选型数据库 -> 海外终端询盘库」及「飞书 CLI 中枢」实时查看。
`,
		},
	}

	existingNames := map[string]bool{}
	for _, d := range docs {
		existingNames[d.SourceName] = true
	}
	files := make([]struct {
		name    string
		content string
	}, 0, len(allFiles))
	for _, f := range allFiles {
		if !existingNames[f.name] {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nil
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

type SiteMaintenanceLog struct {
	ID          string `json:"id"`
	Category    string `json:"category"` // architecture | asset_sync | seo_geo | qa_audit | content_ops
	Title       string `json:"title"`
	TargetPath  string `json:"target_path"`
	PageURL     string `json:"page_url"`
	Summary     string `json:"summary"`
	MetricsJSON string `json:"metrics_json"`
	OperatorBot string `json:"operator_bot"`
	Status      string `json:"status"` // verified | active | pending
	CreatedAt   string `json:"created_at"`
}

type SitePageHealth struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // page | seo | infra | doc
	Label       string `json:"label"`
	RelPath     string `json:"rel_path"`
	PreviewURL  string `json:"preview_url"`
	Title       string `json:"title"`
	H1Count     int    `json:"h1_count"`
	HasCanon    bool   `json:"has_canonical"`
	HasJSONLD   bool   `json:"has_jsonld"`
	SizeBytes   int64  `json:"size_bytes"`
	UpdatedAt   string `json:"updated_at"`
	Status      string `json:"status"`
}

type SiteMaintenanceOverview struct {
	SiteDir       string               `json:"site_dir"`
	MountURL      string               `json:"mount_url"`
	Pages         []SitePageHealth     `json:"pages"`
	Logs          []SiteMaintenanceLog `json:"logs"`
	AssetStats    map[string]any       `json:"asset_stats"`
	SiteLeadCount int                  `json:"site_lead_count"`
	CheckedAt     string               `json:"checked_at"`
}

// ResolveBrandSiteDir locates the static KOOYING brand website folder (`web/`).
func ResolveBrandSiteDir() string {
	candidates := []string{
		filepath.Join(".", "web"),
		filepath.Join("..", "web"),
		"/Users/albert/Documents/project/apps/bisai-hackathons/intercity-hack-huzhou/web",
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append([]string{filepath.Join(filepath.Dir(exe), "web")}, candidates...)
	}
	for _, cand := range candidates {
		if info, err := os.Stat(filepath.Join(cand, "index.html")); err == nil && !info.IsDir() {
			if abs, err := filepath.Abs(cand); err == nil {
				return abs
			}
			return cand
		}
	}
	return "/Users/albert/Documents/project/apps/bisai-hackathons/intercity-hack-huzhou/web"
}

func seedSiteMaintenanceLogs(ctx context.Context, sqlDB *sql.DB) error {
	var count int
	if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM guanying_site_logs`).Scan(&count); err == nil && count > 0 {
		return nil
	}
	now := time.Now().UTC()
	logs := []SiteMaintenanceLog{
		{
			ID:          "slog_01_plan",
			Category:    "architecture",
			Title:       "确立海外极简工业独立站与 SEO/GEO 规划总纲",
			TargetPath:  "web/PLAN.md",
			PageURL:     "/brand-site/index.html",
			Summary:     "对标 Textar / Brakebook、Brembo Commercial 与 Fras-le，确立 Warm Alabaster (#F5F4F0) + Carbon Friction (#111315) + Thermal Copper (#C84B21) 瑞士工业极简视觉系统，规划 7 屏低密度首页与四大海外市场矩阵。",
			MetricsJSON: `{"sections":7,"benchmarks":["TMD Textar","Brembo","Fras-le"],"max_width":"1320px"}`,
			OperatorBot: "bot_gy_site_maintainer",
			Status:      "verified",
			CreatedAt:   now.Add(-90 * time.Minute).Format(time.RFC3339),
		},
		{
			ID:          "slog_02_assets",
			Category:    "asset_sync",
			Title:       "双 ZIP 刹车片实拍图库 GBK 解码与去重归口 (135 款 SKU)",
			TargetPath:  "web/assets/data/products-catalog.json",
			PageURL:     "/brand-site/catalog.html",
			Summary:     "自动解码 Windows GBK 乱码目录并剔除鼓刹镜像重复子目录，归口 41 款盘刹 (126 张正面摩擦/钢背喷码/侧面轮廓三视图) + 94 款鼓刹 (184 张实拍) + 5 大子品牌 29162 渲染图 + 3.9MB fast-start 视频 5200.5300.mp4。",
			MetricsJSON: `{"total_skus":135,"disc_models":41,"drum_models":94,"disc_photos":126,"drum_photos":184}`,
			OperatorBot: "bot_gy_site_maintainer",
			Status:      "verified",
			CreatedAt:   now.Add(-75 * time.Minute).Format(time.RFC3339),
		},
		{
			ID:          "slog_03_factory_pdf",
			Category:    "asset_sync",
			Title:       "官方 PDF 无边框高清基地、试验机与孔位图纸抽取",
			TargetPath:  "web/assets/factory/raw/",
			PageURL:     "/brand-site/index.html#factory",
			Summary:     "从 2026 冠英企业介绍、Kooying 外贸册与冠隆产品册提取 9 张无边框原图（湖州南太湖 43,000㎡ 基地航拍、温控热压产线、2 台欧洲 Krauss 试验机、IATF16949 证书）与 8 张孔位工程尺寸图。",
			MetricsJSON: `{"raw_factory_images":9,"drawing_plates":8,"dyno_wear_400c":"0.040mm"}`,
			OperatorBot: "bot_gy_site_operator",
			Status:      "verified",
			CreatedAt:   now.Add(-60 * time.Minute).Format(time.RFC3339),
		},
		{
			ID:          "slog_04_brakefinder",
			Category:    "content_ops",
			Title:       "上线 BrakeFinder™ 四维交叉选型器与 :17422 GTM 询盘直连",
			TargetPath:  "web/assets/js/site.js",
			PageURL:     "/brand-site/index.html#brakefinder",
			Summary:     "解决无主机厂 OE 码及后市场换桥痛点：支持 WVA/GYS 编码、车桥系统、车型与外弧×宽度×厚度+孔数 (±4.5mm 容差) 实时匹配，RFQ 表单提交直接同步写入 :17422 线索雷达。",
			MetricsJSON: `{"tolerance_mm":4.5,"api_endpoint":"/api/guanying/leads"}`,
			OperatorBot: "bot_gy_site_operator",
			Status:      "verified",
			CreatedAt:   now.Add(-45 * time.Minute).Format(time.RFC3339),
		},
		{
			ID:          "slog_05_seo_geo",
			Category:    "seo_geo",
			Title:       "部署 4 大垂直 SEO/GEO 静态落地页与 AI 爬虫基建",
			TargetPath:  "web/seo/",
			PageURL:     "/brand-site/seo/wva-29087-29162-air-disc-brake-pads.html",
			Summary:     "交付 WVA 29087/29162 盘刹、BPW/Meritor/Fuwa/Hande 挂车车桥、HINO/HOWO/SCANIA 重卡鼓刹、30+ 配方与 400℃ 台架白皮书 4 个纯静态着陆页，配备 sitemap.xml (含 image 扩展)、robots.txt 与 llms.txt。",
			MetricsJSON: `{"seo_pages":4,"bots_allowed":["Googlebot","OAI-SearchBot","PerplexityBot","ClaudeBot"]}`,
			OperatorBot: "bot_gy_seo_builder",
			Status:      "verified",
			CreatedAt:   now.Add(-30 * time.Minute).Format(time.RFC3339),
		},
		{
			ID:          "slog_06_qa_audit",
			Category:    "qa_audit",
			Title:       "全站多视口真机渲染验收与对比度修复完成",
			TargetPath:  "web/assets/css/site.css",
			PageURL:     "/brand-site/catalog.html",
			Summary:     "Playwright 无头 Chrome 实测 1440×900 与 390×844 均零横向溢出、零 JS 报错、138 张目录图片 100% 加载成功；修复移动端长文本按钮换行、首屏 4 项指标不折行及 .seo-hero .lead 浅色对比度。",
			MetricsJSON: `{"desktop_overflow":false,"mobile_390_overflow":false,"broken_images":0}`,
			OperatorBot: "bot_gy_site_maintainer",
			Status:      "verified",
			CreatedAt:   now.Add(-15 * time.Minute).Format(time.RFC3339),
		},
	}

	for _, item := range logs {
		if _, err := sqlDB.ExecContext(ctx, `INSERT INTO guanying_site_logs(
			id,category,title,target_path,page_url,summary,metrics_json,operator_bot,status,created_at
		) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			item.ID, item.Category, item.Title, item.TargetPath, item.PageURL, item.Summary,
			item.MetricsJSON, item.OperatorBot, item.Status, item.CreatedAt,
		); err != nil {
			return fmt.Errorf("seed site maintenance log %s: %w", item.ID, err)
		}
	}
	return nil
}

var (
	reHTMLTitle = regexp.MustCompile(`(?is)<title>(.*?)</title>`)
	reHTMLH1    = regexp.MustCompile(`(?is)<h1\b`)
	reHTMLCanon = regexp.MustCompile(`(?is)<link\s+rel=["']canonical["']`)
	reHTMLJSON  = regexp.MustCompile(`(?is)<script\s+type=["']application/ld\+json["']`)
)

// InspectBrandSite audits the static web/ files and returns live health, asset stats, and logs.
func InspectBrandSite(ctx context.Context, sqlDB *sql.DB) (*SiteMaintenanceOverview, error) {
	siteDir := ResolveBrandSiteDir()
	targets := []struct {
		id         string
		kind       string
		label      string
		relPath    string
		previewURL string
	}{
		{"home", "page", "品牌独立站主页 (7屏极简)", "index.html", "/brand-site/index.html"},
		{"catalog", "page", "135 款型号全量选型目录", "catalog.html", "/brand-site/catalog.html"},
		{"seo_disc", "seo", "SEO 专题 · WVA 29087/29162 盘刹", "seo/wva-29087-29162-air-disc-brake-pads.html", "/brand-site/seo/wva-29087-29162-air-disc-brake-pads.html"},
		{"seo_axle", "seo", "SEO 专题 · BPW/Meritor/Fuwa 换桥选型", "seo/trailer-axle-brake-linings-bpw-meritor-fuwa.html", "/brand-site/seo/trailer-axle-brake-linings-bpw-meritor-fuwa.html"},
		{"seo_truck", "seo", "SEO 专题 · HINO/HOWO/SCANIA 鼓刹", "seo/heavy-duty-drum-linings-hino-howo-scania.html", "/brand-site/seo/heavy-duty-drum-linings-hino-howo-scania.html"},
		{"seo_dyno", "seo", "GEO 白皮书 · 30+ 配方与 400℃ 台架", "seo/friction-material-formulas-krauss-testing.html", "/brand-site/seo/friction-material-formulas-krauss-testing.html"},
		{"plan", "doc", "架构与对标规划文档 (PLAN.md)", "PLAN.md", "/brand-site/PLAN.md"},
		{"sitemap", "infra", "XML Sitemap (含 Image 扩展)", "sitemap.xml", "/brand-site/sitemap.xml"},
		{"robots", "infra", "爬虫放行规则 (robots.txt)", "robots.txt", "/brand-site/robots.txt"},
		{"llms", "infra", "GEO AI 引用摘要 (llms.txt)", "llms.txt", "/brand-site/llms.txt"},
	}

	pages := make([]SitePageHealth, 0, len(targets))
	for _, t := range targets {
		fullPath := filepath.Join(siteDir, filepath.FromSlash(t.relPath))
		item := SitePageHealth{
			ID:         t.id,
			Kind:       t.kind,
			Label:      t.label,
			RelPath:    "web/" + t.relPath,
			PreviewURL: t.previewURL,
			Status:     "missing",
		}
		if info, err := os.Stat(fullPath); err == nil {
			item.SizeBytes = info.Size()
			item.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339)
			item.Status = "healthy"
			if strings.HasSuffix(t.relPath, ".html") {
				if raw, readErr := os.ReadFile(fullPath); readErr == nil {
					txt := string(raw)
					if m := reHTMLTitle.FindStringSubmatch(txt); len(m) > 1 {
						item.Title = strings.TrimSpace(m[1])
					}
					item.H1Count = len(reHTMLH1.FindAllStringIndex(txt, -1))
					item.HasCanon = reHTMLCanon.MatchString(txt)
					item.HasJSONLD = reHTMLJSON.MatchString(txt)
					if item.H1Count != 1 || !item.HasCanon || item.Title == "" {
						item.Status = "warn"
					}
				}
			}
		}
		pages = append(pages, item)
	}

	assetStats := map[string]any{
		"total_models":        135,
		"disc_models":         41,
		"drum_models":         94,
		"total_disc_photos":   126,
		"total_drum_photos":   184,
		"brand_renders":       5,
		"factory_clean_count": 9,
		"drawing_count":       8,
		"video_count":         1,
	}
	if rawCatalog, err := os.ReadFile(filepath.Join(siteDir, "assets", "data", "products-catalog.json")); err == nil {
		var parsed struct {
			Stats         map[string]any `json:"stats"`
			BrandRenders  []any          `json:"brand_renders"`
			FactoryClean  []any          `json:"factory_clean"`
			DrawingAssets []any          `json:"drawing_assets"`
			Videos        []any          `json:"videos"`
		}
		if json.Unmarshal(rawCatalog, &parsed) == nil && parsed.Stats != nil {
			for k, v := range parsed.Stats {
				assetStats[k] = v
			}
			assetStats["brand_renders"] = len(parsed.BrandRenders)
			assetStats["factory_clean_count"] = len(parsed.FactoryClean)
			assetStats["drawing_count"] = len(parsed.DrawingAssets)
			assetStats["video_count"] = len(parsed.Videos)
		}
	}

	logs := make([]SiteMaintenanceLog, 0)
	if sqlDB != nil {
		rows, err := sqlDB.QueryContext(ctx, `SELECT id,category,title,target_path,page_url,summary,metrics_json,operator_bot,status,created_at FROM guanying_site_logs ORDER BY created_at DESC LIMIT 50`)
		if err == nil {
			for rows.Next() {
				var l SiteMaintenanceLog
				if scanErr := rows.Scan(&l.ID, &l.Category, &l.Title, &l.TargetPath, &l.PageURL, &l.Summary, &l.MetricsJSON, &l.OperatorBot, &l.Status, &l.CreatedAt); scanErr == nil {
					logs = append(logs, l)
				}
			}
			_ = rows.Close()
		}
	}

	var siteLeadCount int
	if sqlDB != nil {
		_ = sqlDB.QueryRowContext(ctx, `SELECT COUNT(1) FROM guanying_leads WHERE channel LIKE '%独立站%'`).Scan(&siteLeadCount)
	}

	return &SiteMaintenanceOverview{
		SiteDir:       siteDir,
		MountURL:      "/brand-site/index.html",
		Pages:         pages,
		Logs:          logs,
		AssetStats:    assetStats,
		SiteLeadCount: siteLeadCount,
		CheckedAt:     nowISO(),
	}, nil
}

type CreateSiteLogRequest struct {
	Category    string `json:"category"`
	Title       string `json:"title"`
	TargetPath  string `json:"target_path"`
	PageURL     string `json:"page_url"`
	Summary     string `json:"summary"`
	OperatorBot string `json:"operator_bot"`
	Status      string `json:"status"`
}

func AddSiteMaintenanceLog(ctx context.Context, sqlDB *sql.DB, req CreateSiteLogRequest) (*SiteMaintenanceLog, error) {
	title := strings.TrimSpace(req.Title)
	summary := strings.TrimSpace(req.Summary)
	if title == "" {
		title = "执行独立站全量 SEO/GEO 与资产健康巡检"
	}
	if summary == "" {
		overview, _ := InspectBrandSite(ctx, sqlDB)
		healthy := 0
		for _, p := range overview.Pages {
			if p.Status == "healthy" {
				healthy++
			}
		}
		summary = fmt.Sprintf("全站巡检完成：%d/%d 页面与搜索基建文件健康，135 款实拍型号索引完整，已记录至站点上下文。", healthy, len(overview.Pages))
	}
	cat := strings.TrimSpace(req.Category)
	if cat == "" {
		cat = "qa_audit"
	}
	target := strings.TrimSpace(req.TargetPath)
	if target == "" {
		target = "web/"
	}
	pageURL := strings.TrimSpace(req.PageURL)
	if pageURL == "" {
		pageURL = "/brand-site/index.html"
	}
	bot := strings.TrimSpace(req.OperatorBot)
	if bot == "" {
		bot = "bot_gy_site_maintainer"
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "verified"
	}
	logItem := &SiteMaintenanceLog{
		ID:          newID("slog"),
		Category:    cat,
		Title:       title,
		TargetPath:  target,
		PageURL:     pageURL,
		Summary:     summary,
		MetricsJSON: `{"checked_by":"site_maintenance_panel"}`,
		OperatorBot: bot,
		Status:      status,
		CreatedAt:   nowISO(),
	}
	if _, err := sqlDB.ExecContext(ctx, `INSERT INTO guanying_site_logs(
		id,category,title,target_path,page_url,summary,metrics_json,operator_bot,status,created_at
	) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		logItem.ID, logItem.Category, logItem.Title, logItem.TargetPath, logItem.PageURL,
		logItem.Summary, logItem.MetricsJSON, logItem.OperatorBot, logItem.Status, logItem.CreatedAt,
	); err != nil {
		return nil, err
	}
	return logItem, nil
}

type SEOIssueItem struct {
	Code             string `json:"code"`
	Severity         string `json:"severity"` // critical | warning | pass
	Category         string `json:"category"` // meta | heading | slug | schema | geo | image
	Title            string `json:"title"`
	CurrentValue     string `json:"current_value"`
	RecommendedValue string `json:"recommended_value"`
	Impact           string `json:"impact"`
}

type KooyingPageAudit struct {
	ID                string         `json:"id"`
	URL               string         `json:"url"`
	Path              string         `json:"path"`
	PageType          string         `json:"page_type"`
	HTTPStatus        int            `json:"http_status"`
	SizeBytes         int            `json:"size_bytes"`
	Title             string         `json:"title"`
	TitleLen          int            `json:"title_len"`
	MetaDescription   string         `json:"meta_description"`
	MetaDescLen       int            `json:"meta_desc_len"`
	MetaKeywords      string         `json:"meta_keywords"`
	H1Count           int            `json:"h1_count"`
	H1Texts           []string       `json:"h1_texts"`
	H2Count           int            `json:"h2_count"`
	H2Sample          []string       `json:"h2_sample"`
	ImageTotal        int            `json:"image_total"`
	ImageMissingAlt   int            `json:"image_missing_alt"`
	HasCanonical      bool           `json:"has_canonical"`
	CanonicalURL      string         `json:"canonical_url"`
	HasJSONLD         bool           `json:"has_jsonld"`
	RawJSONLD         string         `json:"raw_jsonld"`
	Score             int            `json:"score"`
	Issues            []SEOIssueItem `json:"issues"`
	OptimizedTitle    string         `json:"optimized_title"`
	OptimizedMetaDesc string         `json:"optimized_meta_desc"`
	OptimizedH1       string         `json:"optimized_h1"`
	OptimizedSlug     string         `json:"optimized_slug"`
	OptimizedJSONLD   string         `json:"optimized_jsonld"`
	CheckedAt         string         `json:"checked_at"`
	IsLiveFetched     bool           `json:"is_live_fetched"`
}

type KooyingSitemapTypo struct {
	URL          string `json:"url"`
	WrongToken   string `json:"wrong_token"`
	CorrectToken string `json:"correct_token"`
	ModelCode    string `json:"model_code"`
	SuggestedURL string `json:"suggested_url"`
	Reason       string `json:"reason"`
}

type KooyingSiteAuditOverview struct {
	TargetDomain        string               `json:"target_domain"`
	OverallScore        int                  `json:"overall_score"`
	ProjectedScore      int                  `json:"projected_score"`
	SitemapURLCount     int                  `json:"sitemap_url_count"`
	CriticalCount       int                  `json:"critical_count"`
	WarningCount        int                  `json:"warning_count"`
	PassedCount         int                  `json:"passed_count"`
	LiveProbeError      string               `json:"live_probe_error,omitempty"`
	Pages               []KooyingPageAudit   `json:"pages"`
	SitemapTypos        []KooyingSitemapTypo `json:"sitemap_typos"`
	RobotsTxtCurrent    string               `json:"robots_txt_current"`
	RobotsTxtOptimized  string               `json:"robots_txt_optimized"`
	LlmsTxtCurrentSample string              `json:"llms_txt_current_sample"`
	LlmsTxtOptimized    string               `json:"llms_txt_optimized"`
	CheckedAt           string               `json:"checked_at"`
}

var (
	reAuditMetaDesc1  = regexp.MustCompile(`(?is)<meta\b[^>]*name=["']description["'][^>]*content=["'](.*?)["'][^>]*>`)
	reAuditMetaDesc2  = regexp.MustCompile(`(?is)<meta\b[^>]*content=["'](.*?)["'][^>]*name=["']description["'][^>]*>`)
	reAuditMetaKW1    = regexp.MustCompile(`(?is)<meta\b[^>]*name=["']keywords["'][^>]*content=["'](.*?)["'][^>]*>`)
	reAuditMetaKW2    = regexp.MustCompile(`(?is)<meta\b[^>]*content=["'](.*?)["'][^>]*name=["']keywords["'][^>]*>`)
	reAuditH1Block    = regexp.MustCompile(`(?is)<h1\b[^>]*>(.*?)</h1>`)
	reAuditH2Block    = regexp.MustCompile(`(?is)<h2\b[^>]*>(.*?)</h2>`)
	reAuditCanonHref  = regexp.MustCompile(`(?is)<link\b[^>]*rel=["']canonical["'][^>]*href=["']([^"']+)["']`)
	reAuditImgTag     = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	reAuditImgAlt     = regexp.MustCompile(`(?is)\balt\s*=\s*["']([^"']*)["']`)
	reAuditJSONLDTag  = regexp.MustCompile(`(?is)<script\b[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	reAuditStripTags  = regexp.MustCompile(`(?is)<[^>]+>`)
)

func cleanHTMLText(s string) string {
	noTags := reAuditStripTags.ReplaceAllString(s, " ")
	unescaped := html.UnescapeString(noTags)
	return strings.TrimSpace(strings.Join(strings.Fields(unescaped), " "))
}

func extractMetaContent(body string, re1, re2 *regexp.Regexp) string {
	if m := re1.FindStringSubmatch(body); len(m) > 1 {
		return cleanHTMLText(m[1])
	}
	if m := re2.FindStringSubmatch(body); len(m) > 1 {
		return cleanHTMLText(m[1])
	}
	return ""
}

func baselineKooyingPages(checkedAt string) []KooyingPageAudit {
	homeRawJSON := `[
  {
    "@context": "https://schema.org/",
    "@type": "Organization",
    "logo": "https://www.kooying.com/template/en/images/logo.png",
    "name": "ZHEJIANG KOOYING AUTOPARTS CO.,LTD.",
    "url": "https://www.kooying.com/",
    "address": "No.398, Shilin Road, Huzhou Economic and Technological Development Zone, Huzhou City, Zhejiang Province,China",
    "contactPoint": [
      {
        "@type": "ContactPoint",
        "telephone": "+8615957118669",
        "email": "Jenny Ji: 2217726858@qq.com",
        "contactType": "customer service"
      }
    ]
  },
  {
    "@context": "https://schema.org/",
    "@type": "WebSite",
    "name": "ZHEJIANG KOOYING AUTOPARTS CO.,LTD.",
    "url": "https://www.kooying.com/",
    "potentialAction": {
      "query-Input": {
        "valueName": "search_string",
        "valueRequired": true,
        "@type": "PropertyValueSpecification"
      },
      "target": {
        "urlTemplate": "https://www.kooying.com/product/?s={search_string}",
        "@type": "EntryPoint"
      },
      "@type": "SearchAction"
    }
  }
]`
	homeOptJSON := `[
  {
    "@context": "https://schema.org",
    "@type": "Manufacturer",
    "name": "Zhejiang Kooying Auto Parts Co., Ltd.",
    "alternateName": ["KOOYING", "GOOLE 冠隆", "冠英"],
    "url": "https://www.kooying.com/",
    "logo": "https://www.kooying.com/template/en/images/logo.png",
    "foundingDate": "2003",
    "numberOfEmployees": { "@type": "QuantitativeValue", "minValue": 120 },
    "address": {
      "@type": "PostalAddress",
      "streetAddress": "No.398, Shilin Road, Huzhou Economic and Technological Development Zone",
      "addressLocality": "Huzhou",
      "addressRegion": "Zhejiang",
      "addressCountry": "CN"
    },
    "hasCertification": [
      { "@type": "Certification", "name": "IATF 16949:2016" },
      { "@type": "Certification", "name": "ECE R90" },
      { "@type": "Certification", "name": "GB5763-2018 Asbestos-Free" }
    ],
    "contactPoint": [
      {
        "@type": "ContactPoint",
        "contactType": "sales",
        "name": "Jenny Ji (Export Director)",
        "telephone": "+86-159-5711-8669",
        "email": "sales@kooying.com"
      }
    ]
  },
  {
    "@context": "https://schema.org",
    "@type": "WebSite",
    "name": "Zhejiang Kooying Auto Parts",
    "url": "https://www.kooying.com/",
    "potentialAction": {
      "@type": "SearchAction",
      "target": {
        "@type": "EntryPoint",
        "urlTemplate": "https://www.kooying.com/product/?s={search_string}"
      },
      "query-input": "required name=search_string"
    }
  }
]`

	bpwRawJSON := `[
  {
    "@context": "http://schema.org",
    "@type": "Product",
    "@id": "https://www.kooying.com/product/european-vehicle-brake-lining/19032-bc-36-1-european-truck-btake-lining-for-bpw.html",
    "name": "19032  BC/36/1 European Truck Btake Lining for BPW",
    "image": [
      "https://www.kooying.com/kooying/2026/02/27/WVA19032.jpg",
      "https://www.kooying.com/kooying/2026/02/27/WVA19032.jpg"
    ],
    "description": "ZHEJIANG KOOYING AUTOPARTS CO.,LTD. is China Wholesale 19032  BC/36/1 European Truck Btake Lining for BPW Suppliers and Factory, WVA19032  BFMC  BC/36/1 is suitable for BPW. Truck brake linings feature outstanding high-temperature stability and strong wear resistance, construct...",
    "review": {
      "@type": "Review",
      "reviewRating": { "@type": "Rating", "ratingValue": 5, "bestRating": 5 },
      "author": { "@type": "Organization", "name": "ZHEJIANG KOOYING AUTOPARTS CO.,LTD." }
    },
    "Offers": {
      "@type": "AggregateOffer",
      "offerCount": 1,
      "priceCurrency": "USD",
      "lowPrice": 0.0,
      "highPrice": 0.0,
      "price": 0.0
    }
  }
]`
	bpwOptJSON := `{
  "@context": "https://schema.org",
  "@type": "Product",
  "name": "WVA 19032 / BFMC BC/36/1 Heavy-Duty Truck Brake Lining for BPW Axle",
  "sku": "GYS-19032",
  "mpn": "WVA 19032",
  "brand": { "@type": "Brand", "name": "KOOYING" },
  "manufacturer": { "@type": "Organization", "name": "Zhejiang Kooying Auto Parts Co., Ltd." },
  "image": ["https://www.kooying.com/kooying/2026/02/27/WVA19032.jpg"],
  "description": "IATF16949 & ECE R90 asbestos-free brake lining WVA 19032 (BFMC BC/36/1) for BPW heavy-duty axles. Krauss 400C thickness wear 0.040mm, friction 0.35-0.38.",
  "additionalProperty": [
    { "@type": "PropertyValue", "name": "WVA Reference", "value": "19032" },
    { "@type": "PropertyValue", "name": "BFMC Code", "value": "BC/36/1" },
    { "@type": "PropertyValue", "name": "Krauss 400C Thickness Wear", "value": "0.040 mm" },
    { "@type": "PropertyValue", "name": "Friction Coefficient", "value": "0.35 - 0.38 (Asbestos-Free)" }
  ],
  "offers": {
    "@type": "AggregateOffer",
    "priceCurrency": "USD",
    "lowPrice": "4.20",
    "highPrice": "7.80",
    "offerCount": 4,
    "availability": "https://schema.org/InStock"
  }
}`

	prodOptJSON := `{
  "@context": "https://schema.org",
  "@type": "CollectionPage",
  "name": "Commercial Truck & Trailer Brake Lining, Air Disc Pad & Brake Shoe Catalog",
  "url": "https://www.kooying.com/product/",
  "description": "Search 135+ Kooying commercial brake models by WVA number, BPW/Meritor/Fuwa/Hande axle, or outer arc x width x holes."
}`

	drumOptJSON := `{
  "@context": "https://schema.org",
  "@type": "CollectionPage",
  "name": "Heavy-Duty Truck Brake Linings (BPW, Meritor, HOWO, HINO, SCANIA)",
  "url": "https://www.kooying.com/product/brake-lining/",
  "description": "Factory-direct asbestos-free drum brake linings matched by WVA/FMSI, axle model, and rivet hole specification."
}`

	aboutOptJSON := `{
  "@context": "https://schema.org",
  "@type": "AboutPage",
  "name": "About Zhejiang Kooying Auto Parts — 43,000m2 Plant & 15M Sets/Year",
  "url": "https://www.kooying.com/about/company-profile.html",
  "mainEntity": {
    "@type": "Organization",
    "name": "Zhejiang Kooying Auto Parts Co., Ltd.",
    "location": "Huzhou, Zhejiang, China"
  }
}`

	return []KooyingPageAudit{
		{
			ID:              "home",
			URL:             "https://www.kooying.com/",
			Path:            "/",
			PageType:        "官网首页",
			HTTPStatus:      200,
			SizeBytes:       58220,
			Title:           "Truck & Automobile Brake Lining Manufacturers, Suppliers",
			TitleLen:        56,
			MetaDescription: "ZHEJIANG KOOYING AUTOPARTS CO.,LTD. is Truck/Automobile Brake Lining Manufacturers and Wholesale Brake Drum, Brake Pads Factory, Brake Shoes Suppliers in China",
			MetaDescLen:     159,
			MetaKeywords:    "brake lining,brake shoes,brake drum,brake pads",
			H1Count:         0,
			H1Texts:         []string{},
			H2Count:         11,
			H2Sample:        []string{"kooying", "kooying", "kooying", "kooying", "about kooying", "product category", "ABOUT KOOYING", "Products"},
			ImageTotal:      36,
			ImageMissingAlt: 8,
			HasCanonical:    true,
			CanonicalURL:    "https://www.kooying.com/",
			HasJSONLD:       true,
			RawJSONLD:       homeRawJSON,
			Score:           58,
			Issues: []SEOIssueItem{
				{
					Code:             "HOME_NO_H1",
					Severity:         "critical",
					Category:         "heading",
					Title:            "首页完全缺失 <h1> 主标题，且重复出现 4 个 <h2>kooying</h2>",
					CurrentValue:     "H1 Count = 0；H2 含 4 个无语义 'kooying'",
					RecommendedValue: "设置唯一 <h1>Heavy-Duty Truck Brake Linings & Air Disc Pads — IATF16949 Factory</h1>，将装饰性 kooying 改为 <span>",
					Impact:           "Google 无法识别首页最高权重核心主题词，直接拉低首页商业车摩擦片主词自然排名。",
				},
				{
					Code:             "HOME_SCHEMA_SYNTAX",
					Severity:         "critical",
					Category:         "schema",
					Title:            "Organization 邮箱格式非法 & SearchAction 键名大小写错误",
					CurrentValue:     `"email":"Jenny Ji: 2217726858@qq.com" 与 "query-Input"`,
					RecommendedValue: `"email":"sales@kooying.com" 与 "query-input":"required name=search_string"（补全 Manufacturer 认证字段）`,
					Impact:           "Google Rich Results 检测判定 contactPoint email 格式报错且站内搜索微件无法激活。",
				},
				{
					Code:             "HOME_IMG_ALT",
					Severity:         "warning",
					Category:         "image",
					Title:            "36 张首页图片中有 8 张缺少或为空 alt 属性",
					CurrentValue:     "36 total / 8 missing or empty alt",
					RecommendedValue: "为厂房/产品实拍补齐含型号、车桥与工况的英文 Alt（如 Kooying WVA 29087 Air Disc Brake Pad for BPW）",
					Impact:           "损失 Google Images 图片搜索流量，降低车间/产线实体可信度。",
				},
				{
					Code:             "HOME_META_UPGRADE",
					Severity:         "warning",
					Category:         "meta",
					Title:            "Title 与 Description 缺少品牌词 KOOYING 与量化台架信任背书",
					CurrentValue:     "Truck & Automobile Brake Lining Manufacturers, Suppliers",
					RecommendedValue: "KOOYING® Heavy-Duty Brake Linings & Air Disc Pads | IATF16949 Factory",
					Impact:           "缺少差异化产能（43,000㎡ / 15M sets/yr）与台架实测数据（400°C 0.040mm 磨损），海外车队买家 CTR 偏低。",
				},
			},
			OptimizedTitle:    "KOOYING® Heavy-Duty Brake Linings & Air Disc Pads | IATF16949 Factory",
			OptimizedMetaDesc: "Zhejiang Kooying Auto Parts (43,000㎡, 15M sets/yr): IATF16949 & ECE R90 truck brake linings & WVA disc pads. Krauss 400C wear 0.040mm. Cross-match by Axle or WVA.",
			OptimizedH1:       "IATF16949 Truck Brake Linings & WVA Air Disc Brake Pads — 15M Sets/Year Factory Direct",
			OptimizedSlug:     "https://www.kooying.com/",
			OptimizedJSONLD:   homeOptJSON,
			CheckedAt:         checkedAt,
		},
		{
			ID:              "bpw_19032",
			URL:             "https://www.kooying.com/product/european-vehicle-brake-lining/19032-bc-36-1-european-truck-btake-lining-for-bpw.html",
			Path:            "/product/european-vehicle-brake-lining/19032-bc-36-1-european-truck-btake-lining-for-bpw.html",
			PageType:        "欧系产品详情页 (WVA 19032)",
			HTTPStatus:      200,
			SizeBytes:       62410,
			Title:           "Wholesale 19032  BC/36/1 European Truck Btake Lining for BPW Suppliers, Factory",
			TitleLen:        78,
			MetaDescription: "ZHEJIANG KOOYING AUTOPARTS CO.,LTD. is China Wholesale 19032  BC/36/1 European Truck Btake Lining for BPW Suppliers and Factory, WVA19032  BFMC  BC/36/1 is suitable for BPW. Truck brake linings feature outstanding high-temperature stability and strong wear resistance, construct...",
			MetaDescLen:     286,
			MetaKeywords:    "19032  BC/36/1 European Truck Btake Lining for BPW",
			H1Count:         1,
			H1Texts:         []string{"19032  BC/36/1 European Truck Btake Lining for BPW"},
			H2Count:         4,
			H2Sample:        []string{"Product Description", "Specifications", "Related Products"},
			ImageTotal:      18,
			ImageMissingAlt: 3,
			HasCanonical:    true,
			CanonicalURL:    "https://www.kooying.com/product/european-vehicle-brake-lining/19032-bc-36-1-european-truck-btake-lining-for-bpw.html",
			HasJSONLD:       true,
			RawJSONLD:       bpwRawJSON,
			Score:           49,
			Issues: []SEOIssueItem{
				{
					Code:             "BPW_TYPO_BTAKE",
					Severity:         "critical",
					Category:         "slug",
					Title:            "核心词 Brake 拼写错误为 Btake（URL、Title、H1、Description 四处同错）",
					CurrentValue:     "...-btake-lining-for-bpw.html / Title & H1: 'Btake Lining'",
					RecommendedValue: "Slug 改用 19032-bc-36-1-european-truck-brake-lining-for-bpw.html 并配置 301 重定向，同步修正 Title 与 H1",
					Impact:           "Google 与采购商搜索 'WVA 19032 BPW brake lining' 时精准关键词彻底失配，直接损失高意向欧系车桥询盘。",
				},
				{
					Code:             "BPW_SCHEMA_OFFERS",
					Severity:         "critical",
					Category:         "schema",
					Title:            "Product JSON-LD 'Offers' 首字母大写失效 + 价格 0.0 + 自评五星",
					CurrentValue:     `"Offers": { "lowPrice": 0.0, "highPrice": 0.0, "price": 0.0 } + 自评 Review`,
					RecommendedValue: `小写 "offers"、标明真实 FOB 区间 $4.20-$7.80、去除自评 Review 并补充 sku/mpn/400C 磨损 additionalProperty`,
					Impact:           "Schema.org 属性严格区分大小写，'Offers' 与 0 价格会被 Google Search Console 拒绝出图和富媒体摘要。",
				},
				{
					Code:             "BPW_TITLE_DESC_LEN",
					Severity:         "warning",
					Category:         "meta",
					Title:            "Title (78字符) 与 Meta Description (286字符) 严重超长并带多空格",
					CurrentValue:     "78 chars Title / 286 chars truncated Description with 'construct...'",
					RecommendedValue: "Title 控制在 55-62 字符，Description 控制在 145-158 字符（含 WVA 19032、BFMC BC/36/1、BPW 适配）",
					Impact:           "Google SERP 搜索结果摘要在 '...construct...' 处生硬截断，影响欧洲配件商点击意愿。",
				},
			},
			OptimizedTitle:    "WVA 19032 (BC/36/1) Heavy Truck Brake Lining for BPW | Kooying",
			OptimizedMetaDesc: "Factory-direct WVA 19032 / BFMC BC/36/1 brake lining for BPW commercial axles. ECE R90 & IATF16949 certified, Krauss 400C wear 0.040mm. Check arc/width/holes.",
			OptimizedH1:       "WVA 19032 / BFMC BC/36/1 European Truck Brake Lining for BPW Axles",
			OptimizedSlug:     "https://www.kooying.com/product/european-vehicle-brake-lining/19032-bc-36-1-european-truck-brake-lining-for-bpw.html",
			OptimizedJSONLD:   bpwOptJSON,
			CheckedAt:         checkedAt,
		},
		{
			ID:              "product_root",
			URL:             "https://www.kooying.com/product/",
			Path:            "/product/",
			PageType:        "产品总目录页",
			HTTPStatus:      200,
			SizeBytes:       77698,
			Title:           "Brake Lining/Brake Shoes with Aramid/Ceramic Raw Material",
			TitleLen:        57,
			MetaDescription: "KOOYING AUTOPARTS offer Custom Brake Lining/Brake Shoes/Brake Drum with Aramid or Ceramic Raw Material, Custom Clutch Friction Plates/Clutch Disc Factory",
			MetaDescLen:     154,
			MetaKeywords:    "Brake Lining,Brake Shoes,Brake Drum",
			H1Count:         1,
			H1Texts:         []string{"Custom Manufacturers of Brake Lining/Brake Shoes/Brake Drum with Aramid or Ceramic Raw Material"},
			H2Count:         8,
			H2Sample:        []string{"Product Categories", "Brake Lining", "Brake Shoes", "Brake Pads"},
			ImageTotal:      42,
			ImageMissingAlt: 6,
			HasCanonical:    true,
			CanonicalURL:    "https://www.kooying.com/product/",
			HasJSONLD:       false,
			RawJSONLD:       "(本页缺少 CollectionPage / ItemList JSON-LD)",
			Score:           68,
			Issues: []SEOIssueItem{
				{
					Code:             "PROD_BUYER_INTENT",
					Severity:         "warning",
					Category:         "meta",
					Title:            "标题与 H1 偏重原料名词（Aramid/Ceramic Raw Material），缺少 WVA 与车桥检索词",
					CurrentValue:     "Brake Lining/Brake Shoes with Aramid/Ceramic Raw Material",
					RecommendedValue: "补充 B2B 高转化检索词：Commercial Brake Linings & Air Disc Pads | WVA / Axle Catalog",
					Impact:           "海外终端维修厂与车队直接搜 WVA 号或 BPW/Meritor/HOWO/HINO，很少搜原材料词。",
				},
				{
					Code:             "PROD_NO_COLLECTION_SCHEMA",
					Severity:         "warning",
					Category:         "schema",
					Title:            "缺少 CollectionPage 结构化标记与内链交叉选型引导",
					CurrentValue:     "JSON-LD Count = 0",
					RecommendedValue: "补充 CollectionPage + 引导按 外弧×宽度×孔数 匹配的交互入口摘要",
					Impact:           "增强目录页在 Google 与 Perplexity/ChatGPT 中的产品目录可引用性。",
				},
			},
			OptimizedTitle:    "Commercial Brake Linings, Air Disc Pads & Shoes | WVA & Axle Catalog — KOOYING",
			OptimizedMetaDesc: "Browse Kooying 135+ heavy-duty brake linings, WVA air disc pads & drum shoes by WVA/FMSI code, BPW/Meritor/Fuwa/Hande axle, or arc x width x holes.",
			OptimizedH1:       "Commercial Truck & Trailer Brake Catalog — Match by WVA, Axle or Dimensions",
			OptimizedSlug:     "https://www.kooying.com/product/",
			OptimizedJSONLD:   prodOptJSON,
			CheckedAt:         checkedAt,
		},
		{
			ID:              "brake_lining",
			URL:             "https://www.kooying.com/product/brake-lining/",
			Path:            "/product/brake-lining/",
			PageType:        "鼓刹分类页",
			HTTPStatus:      200,
			SizeBytes:       78814,
			Title:           "Custom Brake Shoes/Brake Drums/Brake Linings For Brake Lining Models",
			TitleLen:        70,
			MetaDescription: "ZHEJIANG KOOYING AUTOPARTS is China Wholesale Manufacturers of Brake Lining Models Brake Shoes/Brake Drum/Brake Lining with Aramid or Ceramic Raw Material.",
			MetaDescLen:     156,
			MetaKeywords:    "Brake Lining Models,Brake Shoes,Brake Drum",
			H1Count:         1,
			H1Texts:         []string{"Custom Manufacturers of Brake Shoes/Brake Drums/Brake Linings For Brake Lining Models"},
			H2Count:         6,
			H2Sample:        []string{"European Vehicle Brake Lining", "American Vehicle Brake Lining", "Japanese Vehicle Brake Lining"},
			ImageTotal:      38,
			ImageMissingAlt: 5,
			HasCanonical:    true,
			CanonicalURL:    "https://www.kooying.com/product/brake-lining/",
			HasJSONLD:       false,
			RawJSONLD:       "(本页缺少 CollectionPage JSON-LD)",
			Score:           66,
			Issues: []SEOIssueItem{
				{
					Code:             "DRUM_TAUTOLOGY",
					Severity:         "warning",
					Category:         "heading",
					Title:            "CMS 模板套话重复：'...Brake Linings For Brake Lining Models'",
					CurrentValue:     "Custom Brake Shoes/Brake Drums/Brake Linings For Brake Lining Models (70 chars)",
					RecommendedValue: "替换为具体重卡车系与车桥对照：Heavy Truck Brake Linings — European, US, Japanese & HOWO/BPW",
					Impact:           "语义同义反复降低相关性评分，未覆盖 HOWO/HINO/ISUZU/SCANIA/VOLVO/BPW 等高价值搜索词。",
				},
			},
			OptimizedTitle:    "Heavy-Duty Truck Brake Linings — BPW, Meritor, HINO, HOWO, SCANIA | KOOYING",
			OptimizedMetaDesc: "Asbestos-free drum brake linings for European (BPW/SAF/MAN), US (4515/4707), Japanese (HINO/ISUZU) & HOWO axles. Matching 6 to 20 rivet hole patterns.",
			OptimizedH1:       "Heavy-Duty Drum Brake Linings — European, American, Japanese & Chinese Axle Models",
			OptimizedSlug:     "https://www.kooying.com/product/brake-lining/",
			OptimizedJSONLD:   drumOptJSON,
			CheckedAt:         checkedAt,
		},
		{
			ID:              "about",
			URL:             "https://www.kooying.com/about/company-profile.html",
			Path:            "/about/company-profile.html",
			PageType:        "公司简介页",
			HTTPStatus:      200,
			SizeBytes:       40936,
			Title:           "Company Profile- ZHEJIANG KOOYING AUTOPARTS CO.,LTD.",
			TitleLen:        54,
			MetaDescription: "Zhejiang Kooying Auto Parts Co., Ltd. is a company specializing in the R&amp;D and production of truck friction material series products.\nAn enterprise integrat...",
			MetaDescLen:     161,
			MetaKeywords:    "Zhejiang Kooying Auto Parts,Brake Lining Factory",
			H1Count:         0,
			H1Texts:         []string{},
			H2Count:         5,
			H2Sample:        []string{"Company Profile", "Production Equipment", "Quality Control"},
			ImageTotal:      14,
			ImageMissingAlt: 4,
			HasCanonical:    true,
			CanonicalURL:    "https://www.kooying.com/about/company-profile.html",
			HasJSONLD:       false,
			RawJSONLD:       "(本页缺少 AboutPage JSON-LD)",
			Score:           61,
			Issues: []SEOIssueItem{
				{
					Code:             "ABOUT_NO_H1",
					Severity:         "critical",
					Category:         "heading",
					Title:            "公司简介页缺失 <h1> 标签",
					CurrentValue:     "H1 Count = 0",
					RecommendedValue: "增加唯一 <h1>Zhejiang Kooying Auto Parts — 43,000㎡ Friction Material Plant in Huzhou</h1>",
					Impact:           "品牌实力页（E-E-A-T 权威页）缺乏明确一级标题，不利于 AI 引擎提取企业实体档案。",
				},
				{
					Code:             "ABOUT_META_ENTITY",
					Severity:         "warning",
					Category:         "meta",
					Title:            "Description 含未解码实体 R&amp;D、原始换行符与尾部截断",
					CurrentValue:     "...specializing in the R&amp;D and production...\\nAn enterprise integrat...",
					RecommendedValue: "输出单行干净英文摘要，写明 2003 建厂、43,000㎡、1500万套产能、2台 Krauss 试验机与 IATF16949 认证",
					Impact:           "破坏 SERP 展示美观度，错失终端车队审厂阶段的信任转化。",
				},
			},
			OptimizedTitle:    "About Zhejiang Kooying | 43,000㎡ Truck Brake Factory (15M Sets/Yr)",
			OptimizedMetaDesc: "Founded in Huzhou, Zhejiang Kooying operates a 43,000㎡ IATF16949 plant producing 15M brake pad & lining sets yearly, verified by European Krauss dyno.",
			OptimizedH1:       "About Zhejiang Kooying Auto Parts — 43,000㎡ Commercial Friction Plant",
			OptimizedSlug:     "https://www.kooying.com/about/company-profile.html",
			OptimizedJSONLD:   aboutOptJSON,
			CheckedAt:         checkedAt,
		},
		{
			ID:              "wva_29137_trcuk",
			URL:             "https://www.kooying.com/product/brake-pad-for-truck/trcuk-brake-pad-29137.html",
			Path:            "/product/brake-pad-for-truck/trcuk-brake-pad-29137.html",
			PageType:        "盘刹单品页 (WVA 29137 错词)",
			HTTPStatus:      200,
			SizeBytes:       61240,
			Title:           "Wholesale Trcuk Brake Pad 29137 Manufacturers, Suppliers",
			TitleLen:        56,
			MetaDescription: "ZHEJIANG KOOYING AUTOPARTS is China Wholesale Trcuk Brake Pad 29137 Manufacturers and Factory, WVA 29137 heavy duty truck disc brake pad...",
			MetaDescLen:     140,
			MetaKeywords:    "Trcuk Brake Pad 29137",
			H1Count:         1,
			H1Texts:         []string{"Trcuk Brake Pad 29137"},
			H2Count:         4,
			H2Sample:        []string{"Product Description", "Specifications", "Related Products"},
			ImageTotal:      12,
			ImageMissingAlt: 3,
			HasCanonical:    true,
			CanonicalURL:    "https://www.kooying.com/product/brake-pad-for-truck/trcuk-brake-pad-29137.html",
			HasJSONLD:       true,
			RawJSONLD:       `{ "@type": "Product", "name": "Trcuk Brake Pad 29137", "Offers": { "lowPrice": 0.0, "price": 0.0 } }`,
			Score:           46,
			Issues: []SEOIssueItem{
				{
					Code:             "TRCUK_SLUG_TYPO",
					Severity:         "critical",
					Category:         "slug",
					Title:            "核心大词 Truck 拼写错误为 'Trcuk'（波及 29137/29162/29093/ad-05/yf3501 共 5 页）",
					CurrentValue:     "trcuk-brake-pad-29137.html / Trcuk Brake Pad 29137",
					RecommendedValue: "Slug 改用 wva-29137-truck-brake-pad-knorr-sb7000.html，Title 按 [WVA 29137] + [卡钳] + [OE] 重写并设置 301",
					Impact:           "Google 与海外买家搜索 'Truck Brake Pad 29137' 时完全失配；波及同批次 5 个高转化盘刹产品页。",
				},
				{
					Code:             "TRCUK_NO_OE_TABLE",
					Severity:         "warning",
					Category:         "geo",
					Title:            "缺少卡钳型号 / 长×宽×厚尺寸表（HTML Table）与顶部 Catalog PDF 留资钩子",
					CurrentValue:     "仅展示通用文描与静态 Contact Us 表单",
					RecommendedValue: "增加 HTML 结构化参数表（247.5 × 109.5 × 30 mm / WVA 29137）争夺 Google Featured Snippets + 顶部 PDF 目录下载诱饵",
					Impact:           "无法进入 Google 精选摘要，商用车维修厂线索留资率落后 FrenoBrake / Alltour。",
				},
			},
			OptimizedTitle:    "WVA 29137 Truck Air Disc Brake Pad (247.5×109.5mm) — Knorr Caliper | KOOYING",
			OptimizedMetaDesc: "WVA 29137 heavy-duty truck brake pad with ECE R90 & IATF16949. 247.5x109.5x30mm OE replacement, Krauss 400C wear 0.040mm. Download 2026 PDF catalog.",
			OptimizedH1:       "WVA 29137 Heavy-Duty Truck Air Disc Brake Pad — Dimensions, Caliper & OE Cross Reference",
			OptimizedSlug:     "https://www.kooying.com/product/brake-pad-for-truck/wva-29137-truck-brake-pad.html",
			OptimizedJSONLD: `{
  "@context": "https://schema.org",
  "@type": "Product",
  "name": "WVA 29137 Truck Air Disc Brake Pad",
  "sku": "WVA-29137",
  "mpn": "29137",
  "brand": { "@type": "Brand", "name": "KOOYING" },
  "offers": { "@type": "AggregateOffer", "priceCurrency": "USD", "lowPrice": "14.50", "highPrice": "19.50", "offerCount": 3 }
}`,
			CheckedAt: checkedAt,
		},
		{
			ID:              "wva_29124_fiction",
			URL:             "https://www.kooying.com/product/brake-pad-for-truck/29124-brake-pad-truck-fiction-material.html",
			Path:            "/product/brake-pad-for-truck/29124-brake-pad-truck-fiction-material.html",
			PageType:        "盘刹单品页 (WVA 29124 Fiction 错词)",
			HTTPStatus:      200,
			SizeBytes:       60890,
			Title:           "29124 Brake Pad Truck Fiction Material Suppliers, Factory",
			TitleLen:        59,
			MetaDescription: "Wholesale 29124 Brake Pad Truck Fiction Material from Zhejiang Kooying...",
			MetaDescLen:     73,
			MetaKeywords:    "29124 Brake Pad Truck Fiction Material",
			H1Count:         1,
			H1Texts:         []string{"29124 Brake Pad Truck Fiction Material"},
			H2Count:         4,
			H2Sample:        []string{"Product Description", "Specifications", "Related Products"},
			ImageTotal:      10,
			ImageMissingAlt: 2,
			HasCanonical:    true,
			CanonicalURL:    "https://www.kooying.com/product/brake-pad-for-truck/29124-brake-pad-truck-fiction-material.html",
			HasJSONLD:       true,
			RawJSONLD:       `{ "@type": "Product", "name": "29124 Brake Pad Truck Fiction Material" }`,
			Score:           50,
			Issues: []SEOIssueItem{
				{
					Code:             "FICTION_TYPO",
					Severity:         "critical",
					Category:         "slug",
					Title:            "行业核心术语 Friction 被拼成 'Fiction'（小说材质），波及 29124 与 29136",
					CurrentValue:     "29124-brake-pad-truck-fiction-material.html",
					RecommendedValue: "修正为 29124-brake-pad-truck-friction-material.html，同步标注 ECE R90 / Copper-Free / Semi-Metallic 配方级数",
					Impact:           "严重损害海外技术买家的专业信任度，丢失 'truck friction material / WVA 29124' 搜索匹配。",
				},
			},
			OptimizedTitle:    "WVA 29124 Commercial Truck Brake Pad — Copper-Free Friction Formula | KOOYING",
			OptimizedMetaDesc: "WVA 29124 heavy truck air disc pad using asbestos-free ceramic & low-metallic friction formula (FF grade, μ 0.35-0.38). View OE cross-reference table.",
			OptimizedH1:       "WVA 29124 Truck Air Disc Brake Pad — Friction Formulation & OE Cross Reference",
			OptimizedSlug:     "https://www.kooying.com/product/brake-pad-for-truck/wva-29124-truck-brake-pad-friction-material.html",
			OptimizedJSONLD: `{
  "@context": "https://schema.org",
  "@type": "Product",
  "name": "WVA 29124 Truck Air Disc Brake Pad",
  "sku": "WVA-29124",
  "brand": { "@type": "Brand", "name": "KOOYING" }
}`,
			CheckedAt: checkedAt,
		},
		{
			ID:              "beake_daewoo",
			URL:             "https://www.kooying.com/product/korea-vehicle-brake-lining/beake-lining-dawoo-r-16-holes-brake-pad.html",
			Path:            "/product/korea-vehicle-brake-lining/beake-lining-dawoo-r-16-holes-brake-pad.html",
			PageType:        "韩日系鼓刹页 (Beake 错词)",
			HTTPStatus:      200,
			SizeBytes:       61180,
			Title:           "Beake Lining Dawoo R 16 Holes Brake Pad Suppliers",
			TitleLen:        51,
			MetaDescription: "Beake Lining Dawoo R 16 Holes Brake Pad manufactured by Zhejiang Kooying...",
			MetaDescLen:     75,
			MetaKeywords:    "Beake Lining,Dawoo Brake Pad",
			H1Count:         1,
			H1Texts:         []string{"Beake Lining Dawoo R 16 Holes Brake Pad"},
			H2Count:         4,
			H2Sample:        []string{"Specifications", "Related Products"},
			ImageTotal:      8,
			ImageMissingAlt: 2,
			HasCanonical:    true,
			CanonicalURL:    "https://www.kooying.com/product/korea-vehicle-brake-lining/beake-lining-dawoo-r-16-holes-brake-pad.html",
			HasJSONLD:       true,
			RawJSONLD:       `{ "@type": "Product", "name": "Beake Lining Dawoo R 16 Holes" }`,
			Score:           48,
			Issues: []SEOIssueItem{
				{
					Code:             "BEAKE_DAWOO_TYPO",
					Severity:         "critical",
					Category:         "slug",
					Title:            "双重拼写硬伤：'Beake Lining' (Brake) 与 'Dawoo' (Daewoo)，波及 HINO ZY / 三菱共 3 页",
					CurrentValue:     "beake-lining-dawoo-r-16-holes-brake-pad.html",
					RecommendedValue: "修正为 brake-lining-daewoo-rear-16-holes.html 并补充外弧×宽度×厚度×16孔结构化表",
					Impact:           "日韩重卡高频出口市场（东南亚/拉美/非洲）检索 'Daewoo 16 holes brake lining' 时完全失效。",
				},
			},
			OptimizedTitle:    "Daewoo Rear 16-Hole Truck Brake Lining (OE Cross-Match) | KOOYING",
			OptimizedMetaDesc: "16-hole rear drum brake lining for Daewoo heavy trucks. Verified arc x width x thickness dimensions, asbestos-free ECE R90 friction formula.",
			OptimizedH1:       "Brake Lining for Daewoo Rear Axle (16 Holes) — Dimensions & Cross Reference",
			OptimizedSlug:     "https://www.kooying.com/product/korea-vehicle-brake-lining/brake-lining-daewoo-rear-16-holes.html",
			OptimizedJSONLD: `{
  "@context": "https://schema.org",
  "@type": "Product",
  "name": "Brake Lining for Daewoo Rear Axle (16 Holes)",
  "brand": { "@type": "Brand", "name": "KOOYING" }
}`,
			CheckedAt: checkedAt,
		},
		{
			ID:              "wva_29087_matrix",
			URL:             "https://www.kooying.com/product/brake-pad-for-truck/truck-brake-pad-29087-29105-29106-29108-29109-29163-29179.html",
			Path:            "/product/brake-pad-for-truck/truck-brake-pad-29087-29105-29106-29108-29109-29163-29179.html",
			PageType:        "WVA 长尾矩阵核心页 (29087/29108)",
			HTTPStatus:      200,
			SizeBytes:       64820,
			Title:           "Truck Brake Pad 29087 29105 29106 29108 29109 29163 29179 Manufacturers",
			TitleLen:        73,
			MetaDescription: "ZHEJIANG KOOYING AUTOPARTS offers Truck Brake Pad 29087 29105 29106 29108...",
			MetaDescLen:     78,
			MetaKeywords:    "Truck Brake Pad 29087,WVA 29108,WVA 29171",
			H1Count:         1,
			H1Texts:         []string{"Truck Brake Pad 29087 29105 29106 29108 29109 29163 29179"},
			H2Count:         5,
			H2Sample:        []string{"Product Description", "Parameters", "Related Products"},
			ImageTotal:      14,
			ImageMissingAlt: 3,
			HasCanonical:    true,
			CanonicalURL:    "https://www.kooying.com/product/brake-pad-for-truck/truck-brake-pad-29087-29105-29106-29108-29109-29163-29179.html",
			HasJSONLD:       true,
			RawJSONLD:       `{ "@type": "Product", "name": "Truck Brake Pad 29087 29105 29106 29108 29109 29163 29179" }`,
			Score:           74,
			Issues: []SEOIssueItem{
				{
					Code:             "WVA_MATRIX_STRENGTH",
					Severity:         "pass",
					Category:         "meta",
					Title:            "核心长尾策略有效：Title 与 URL 集中覆盖 WVA 29087 / 29108 等 7 个高频互换号",
					CurrentValue:     "WVA 29087 / 29105 / 29106 / 29108 / 29109 / 29163 / 29179",
					RecommendedValue: "保持多 WVA 互换覆盖，并拆分出独立对比页 WVA 29087 vs 29108 截流精准替换需求",
					Impact:           "这是 kooying.com 现有最强的自然精准询盘流量抓手。",
				},
				{
					Code:             "WVA_MISSING_CALIPER_CATALOG",
					Severity:         "warning",
					Category:         "geo",
					Title:            "缺少卡钳型号（Knorr SB7000 / Wabco PAN22）+ OE 交叉 HTML 参数表与 PDF Catalog 下载钩子",
					CurrentValue:     "Title 超过 73 字符且无 OE 0044206020、尺寸图及 Catalog Lead Magnet",
					RecommendedValue: "嵌入 247.6×109.7×30mm HTML Cross-Reference Table 抢占 Google Featured Snippets，并加顶部 PDF Catalog 留资诱饵",
					Impact:           "补齐后可兼具精准型号搜索权重与高转化线索留资率（预计提升 3-5 倍）。",
				},
			},
			OptimizedTitle:    "WVA 29087 / 29108 Knorr Caliper Truck Brake Pad (OE 0044206020) | KOOYING",
			OptimizedMetaDesc: "WVA 29087/29108 heavy truck disc pad (247.6x109.7x30mm) for Knorr SB7000 & Mercedes Actros (OE 0044206020). 400C wear 0.040mm. Download OE PDF list.",
			OptimizedH1:       "WVA 29087 / 29108 Truck Air Disc Brake Pad — Knorr Caliper, Dimensions & OE 0044206020 Cross Reference",
			OptimizedSlug:     "https://www.kooying.com/product/brake-pad-for-truck/truck-brake-pad-29087-29108-knorr-actros.html",
			OptimizedJSONLD: `{
  "@context": "https://schema.org",
  "@type": "Product",
  "name": "WVA 29087 / 29108 Heavy Truck Air Disc Brake Pad",
  "sku": "WVA-29087",
  "mpn": "0044206020",
  "brand": { "@type": "Brand", "name": "KOOYING" },
  "offers": { "@type": "AggregateOffer", "priceCurrency": "USD", "lowPrice": "13.80", "highPrice": "19.50", "offerCount": 5 }
}`,
			CheckedAt: checkedAt,
		},
		{
			ID:              "wind_turbine",
			URL:             "https://www.kooying.com/product/brake-pad-for-wind-turbine/9710081817-highfriction-brake-pad-for-wind-turbine.html",
			Path:            "/product/brake-pad-for-wind-turbine/9710081817-highfriction-brake-pad-for-wind-turbine.html",
			PageType:        "特种高毛利风电刹车片 (9页集群)",
			HTTPStatus:      200,
			SizeBytes:       59100,
			Title:           "9710081817 High-Friction Brake Pad for Wind Turbine",
			TitleLen:        53,
			MetaDescription: "High-friction sintered & organic brake pad 9710081817 for wind turbine yaw and rotor brakes...",
			MetaDescLen:     92,
			MetaKeywords:    "brake pad for wind turbine,9710081817",
			H1Count:         1,
			H1Texts:         []string{"9710081817 High-Friction Brake Pad for Wind Turbine"},
			H2Count:         5,
			H2Sample:        []string{"Selection Guide", "Materials", "Maintenance"},
			ImageTotal:      8,
			ImageMissingAlt: 1,
			HasCanonical:    true,
			CanonicalURL:    "https://www.kooying.com/product/brake-pad-for-wind-turbine/9710081817-highfriction-brake-pad-for-wind-turbine.html",
			HasJSONLD:       true,
			RawJSONLD:       `{ "@type": "Product", "name": "9710081817 Brake Pad for Wind Turbine" }`,
			Score:           79,
			Issues: []SEOIssueItem{
				{
					Code:             "WIND_BLUEOCEAN_STRENGTH",
					Severity:         "pass",
					Category:         "geo",
					Title:            "优势差异化蓝海：切入风力发电机偏航/转子刹车片（9 个型号 + 指南长文）",
					CurrentValue:     "/product/brake-pad-for-wind-turbine/ (9710081817, 9710071701, 9710030717, dadh90jfa06)",
					RecommendedValue: "保持 190 篇博客 + 风电高毛利指南优势，补充与 /news/ 风电选型指南的内链互通及下载规格书钩子",
					Impact:           "有效避开常规卡车红海竞争，拿取风电运维与风机厂高净值 B2B 询盘。",
				},
			},
			OptimizedTitle:    "9710081817 Wind Turbine Rotor & Yaw Brake Pad — High-Friction Spec | KOOYING",
			OptimizedMetaDesc: "OEM replacement wind turbine brake pad 9710081817 with stable high-temp friction coefficient & low rotor wear. Download wind brake selection & maintenance PDF.",
			OptimizedH1:       "9710081817 Brake Pad for Wind Turbine Rotor & Yaw Systems — Selection & Maintenance Spec",
			OptimizedSlug:     "https://www.kooying.com/product/brake-pad-for-wind-turbine/9710081817-wind-turbine-brake-pad.html",
			OptimizedJSONLD: `{
  "@context": "https://schema.org",
  "@type": "Product",
  "name": "9710081817 Wind Turbine Brake Pad",
  "sku": "9710081817",
  "brand": { "@type": "Brand", "name": "KOOYING" }
}`,
			CheckedAt: checkedAt,
		},
	}
}

func baselineSitemapTypos() []KooyingSitemapTypo {
	return []KooyingSitemapTypo{
		{
			URL:          "https://www.kooying.com/product/brake-pad-for-truck/trcuk-brake-pad-29137.html",
			WrongToken:   "trcuk (波及 29137 / 29162 / 29093 / ad-05 / yf3501 共 5 页)",
			CorrectToken: "truck",
			ModelCode:    "WVA 29137 / 29162 / 29093 (空气盘刹)",
			SuggestedURL: "https://www.kooying.com/product/brake-pad-for-truck/truck-brake-pad-29137.html",
			Reason:       "核心高频主词 Truck 在 URL、Title、H1 同错拼成 Trcuk，波及 5 个高价值 WVA 盘刹页。",
		},
		{
			URL:          "https://www.kooying.com/product/brake-pad-for-truck/29124-brake-pad-truck-fiction-material.html",
			WrongToken:   "fiction-material (波及 29124 / 29136 共 2 页)",
			CorrectToken: "friction-material",
			ModelCode:    "WVA 29124 / 29136 (重卡盘刹)",
			SuggestedURL: "https://www.kooying.com/product/brake-pad-for-truck/wva-29124-truck-brake-pad-friction-material.html",
			Reason:       "核心行业术语 Friction（摩擦材料）错写成 Fiction（小说），严重影响工程师买家信任度。",
		},
		{
			URL:          "https://www.kooying.com/product/korea-vehicle-brake-lining/beake-lining-dawoo-r-16-holes-brake-pad.html",
			WrongToken:   "beake-lining + dawoo (波及 HINO ZY / 三菱 / 大宇 共 3 页)",
			CorrectToken: "brake-lining + daewoo",
			ModelCode:    "Daewoo 16-Hole / HINO ZY F1 / Mitsubishi Jumpo",
			SuggestedURL: "https://www.kooying.com/product/korea-vehicle-brake-lining/brake-lining-daewoo-r-16-holes.html",
			Reason:       "Brake 错拼成 Beake、Daewoo 错拼成 Dawoo、Mitsubishi Jumbo 错拼成 Jumpo。",
		},
		{
			URL:          "https://www.kooying.com/product/european-vehicle-brake-lining/19032-bc-36-1-european-truck-btake-lining-for-bpw.html",
			WrongToken:   "btake-lining",
			CorrectToken: "brake-lining",
			ModelCode:    "WVA 19032 / BFMC BC/36/1 (BPW)",
			SuggestedURL: "https://www.kooying.com/product/european-vehicle-brake-lining/19032-bc-36-1-european-truck-brake-lining-for-bpw.html",
			Reason:       "核心产品词 Brake 误拼为 Btake，导致 URL Slug、Title、H1 同步报错，直接损失 WVA 19032 精准流量。",
		},
		{
			URL:          "https://www.kooying.com/product/european-vehicle-brake-lining/17276-mb-60-1-highend-quality-brake-lining-for-mercedsbenz.html",
			WrongToken:   "mercedsbenz",
			CorrectToken: "mercedes-benz",
			ModelCode:    "WVA 17276 / BFMC MB/60/1 (Mercedes-Benz)",
			SuggestedURL: "https://www.kooying.com/product/european-vehicle-brake-lining/17276-mb-60-1-mercedes-benz-truck-brake-lining.html",
			Reason:       "主机厂品牌 Mercedes-Benz 误拼为 mercedsbenz（漏字母 e 且连写），同时建议去除虚词 highend-quality。",
		},
		{
			URL:          "https://www.kooying.com/product/european-vehicle-brake-lining/19037-df-24-1-brake-lining-for-saf-trailor-york.html",
			WrongToken:   "trailor",
			CorrectToken: "trailer",
			ModelCode:    "WVA 19037 / BFMC DF/24/1 (SAF / York)",
			SuggestedURL: "https://www.kooying.com/product/european-vehicle-brake-lining/19037-df-24-1-brake-lining-for-saf-trailer-york.html",
			Reason:       "挂车英文 Trailer 误拼为 trailor，影响中东/欧洲挂车车桥配件买家检索匹配。",
		},
		{
			URL:          "https://www.kooying.com/product/american-vehicle-brake-lining/american-brake-lining-4702a-for-ford-ivceo.html",
			WrongToken:   "ivceo (波及 4702A / 4702C)",
			CorrectToken: "iveco",
			ModelCode:    "FMSI 4702A / 4702C (Ford / Iveco)",
			SuggestedURL: "https://www.kooying.com/product/american-vehicle-brake-lining/american-brake-lining-4702a-for-ford-iveco.html",
			Reason:       "商用车品牌 Iveco 字母颠倒误拼为 ivceo。",
		},
		{
			URL:          "https://www.kooying.com/product/european-vehicle-brake-lining/19017-rw-1-5-heavy-duty-truck-brake-lining-for.html",
			WrongToken:   "-for.html (截断URL)",
			CorrectToken: "-for-ror-meritor-axle.html",
			ModelCode:    "WVA 19017 / BFMC RW/1/5",
			SuggestedURL: "https://www.kooying.com/product/european-vehicle-brake-lining/19017-rw-1-5-heavy-duty-truck-brake-lining-ror-axle.html",
			Reason:       "CMS 生成 URL 在介词 'for' 处被截断，缺少目标车桥名（ROR / Meritor），语义不完整。",
		},
	}
}

func fetchAndAuditLiveKooyingURL(ctx context.Context, targetURL string, checkedAt string) (*KooyingPageAudit, error) {
	parsed, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("请输入完整的 http/https URL，例如 https://www.kooying.com/")
	}
	client := &http.Client{Timeout: 7 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AlphaGTM-KooyingSEOAuditor/1.0)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rawBytes, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}
	body := string(rawBytes)

	title := ""
	if m := reHTMLTitle.FindStringSubmatch(body); len(m) > 1 {
		title = cleanHTMLText(m[1])
	}
	desc := extractMetaContent(body, reAuditMetaDesc1, reAuditMetaDesc2)
	kw := extractMetaContent(body, reAuditMetaKW1, reAuditMetaKW2)

	h1Matches := reAuditH1Block.FindAllStringSubmatch(body, -1)
	h1Texts := make([]string, 0, len(h1Matches))
	for _, m := range h1Matches {
		if t := cleanHTMLText(m[1]); t != "" {
			h1Texts = append(h1Texts, t)
		}
	}
	h2Matches := reAuditH2Block.FindAllStringSubmatch(body, -1)
	h2Sample := make([]string, 0, 8)
	for i, m := range h2Matches {
		if i >= 8 {
			break
		}
		if t := cleanHTMLText(m[1]); t != "" {
			h2Sample = append(h2Sample, t)
		}
	}

	canon := ""
	if m := reAuditCanonHref.FindStringSubmatch(body); len(m) > 1 {
		canon = strings.TrimSpace(m[1])
	}

	imgs := reAuditImgTag.FindAllString(body, -1)
	missingAlt := 0
	for _, imgTag := range imgs {
		altMatch := reAuditImgAlt.FindStringSubmatch(imgTag)
		if len(altMatch) < 2 || strings.TrimSpace(altMatch[1]) == "" {
			missingAlt++
		}
	}

	ldMatches := reAuditJSONLDTag.FindAllStringSubmatch(body, -1)
	ldJoined := ""
	if len(ldMatches) > 0 {
		parts := make([]string, 0, len(ldMatches))
		for _, m := range ldMatches {
			parts = append(parts, strings.TrimSpace(m[1]))
		}
		ldJoined = strings.Join(parts, "\n\n")
	}

	issues := make([]SEOIssueItem, 0)
	score := 100

	if len(h1Matches) == 0 {
		score -= 18
		issues = append(issues, SEOIssueItem{
			Code:             "LIVE_NO_H1",
			Severity:         "critical",
			Category:         "heading",
			Title:            "线上页面缺失 <h1> 一级标题",
			CurrentValue:     "H1 Count = 0",
			RecommendedValue: "补充唯一且含产品型号/车桥与工厂背书的 <h1> 标题",
			Impact:           "搜索引擎无法准确提取当前页面主意图主题。",
		})
	} else if len(h1Matches) > 1 {
		score -= 8
		issues = append(issues, SEOIssueItem{
			Code:             "LIVE_MULTI_H1",
			Severity:         "warning",
			Category:         "heading",
			Title:            fmt.Sprintf("页面存在 %d 个 <h1>（建议仅保留 1 个）", len(h1Matches)),
			CurrentValue:     strings.Join(h1Texts, " | "),
			RecommendedValue: "仅在主 Hero 或产品主标题保留唯一 <h1>",
			Impact:           "分散主标题权重信号。",
		})
	} else {
		issues = append(issues, SEOIssueItem{
			Code:             "LIVE_H1_OK",
			Severity:         "pass",
			Category:         "heading",
			Title:            "已包含唯一 <h1> 主标题",
			CurrentValue:     strings.Join(h1Texts, ""),
			RecommendedValue: "检查是否含核心拼写错误及 WVA/车桥编号",
			Impact:           "标题层级符合基础规范。",
		})
	}

	if title == "" {
		score -= 20
		issues = append(issues, SEOIssueItem{
			Code:             "LIVE_NO_TITLE",
			Severity:         "critical",
			Category:         "meta",
			Title:            "缺少 <title> 标签",
			CurrentValue:     "(empty)",
			RecommendedValue: "设置 50-65 字符英文工业品标题并附带 | KOOYING",
			Impact:           "直接丧失搜索结果标题展示。",
		})
	} else if len([]rune(title)) > 65 || len([]rune(title)) < 28 {
		score -= 8
		issues = append(issues, SEOIssueItem{
			Code:             "LIVE_TITLE_LEN",
			Severity:         "warning",
			Category:         "meta",
			Title:            fmt.Sprintf("<title> 长度不理想（当前 %d 字符，建议 35–62）", len([]rune(title))),
			CurrentValue:     title,
			RecommendedValue: "精简去掉重复 Wholesale/Suppliers，保留型号 + 适配车桥 + KOOYING",
			Impact:           "Google SERP 中易被折叠截断，降低英文买家点击率。",
		})
	}

	if desc == "" {
		score -= 12
		issues = append(issues, SEOIssueItem{
			Code:             "LIVE_NO_DESC",
			Severity:         "critical",
			Category:         "meta",
			Title:            "缺少 Meta Description",
			CurrentValue:     "(empty)",
			RecommendedValue: "补充 135–158 字符精准摘要（含 IATF16949、400°C 磨损 0.040mm、尺寸孔数）",
			Impact:           "搜索结果摘要由爬虫随机抓取正文碎片。",
		})
	} else if len([]rune(desc)) > 165 || strings.Contains(desc, "...") {
		score -= 7
		issues = append(issues, SEOIssueItem{
			Code:             "LIVE_DESC_TRUNCATED",
			Severity:         "warning",
			Category:         "meta",
			Title:            fmt.Sprintf("Meta Description 过长或存在截取省略号（%d 字符）", len([]rune(desc))),
			CurrentValue:     desc,
			RecommendedValue: "重写为 145–158 字符完整句子，去除 'construct...' 等截断尾巴",
			Impact:           "摘要信息不全，影响海外汽配商点击转换。",
		})
	}

	lowerAll := strings.ToLower(parsed.Path + " " + title + " " + strings.Join(h1Texts, " "))
	typoPairs := [][2]string{
		{"btake", "brake"},
		{"beake", "brake"},
		{"trcuk", "truck"},
		{"fiction", "friction"},
		{"dawoo", "daewoo"},
		{"mercedsbenz", "mercedes-benz"},
		{"trailor", "trailer"},
		{"ivceo", "iveco"},
	}
	for _, pair := range typoPairs {
		if strings.Contains(lowerAll, pair[0]) {
			score -= 18
			issues = append(issues, SEOIssueItem{
				Code:             "LIVE_TYPO_" + strings.ToUpper(pair[0]),
				Severity:         "critical",
				Category:         "slug",
				Title:            fmt.Sprintf("检测到线上英文拼写错误：'%s' → 应为 '%s'", pair[0], pair[1]),
				CurrentValue:     parsed.Path + " / " + title,
				RecommendedValue: "修改 URL Slug（旧 URL 做 301 重定向）并同步修正 Title / H1 / Alt",
				Impact:           "关键词拼写错误导致目标检索词 100% 匹配失败。",
			})
		}
	}

	if missingAlt > 0 {
		penalty := 6
		if missingAlt > 5 {
			penalty = 10
		}
		score -= penalty
		issues = append(issues, SEOIssueItem{
			Code:             "LIVE_IMG_ALT",
			Severity:         "warning",
			Category:         "image",
			Title:            fmt.Sprintf("共 %d 张图片，其中 %d 张缺少或为空 alt 属性", len(imgs), missingAlt),
			CurrentValue:     fmt.Sprintf("%d / %d missing alt", missingAlt, len(imgs)),
			RecommendedValue: "为每张实拍图填写真实英文车型/WVA/工厂描述",
			Impact:           "影响 Google 图片检索收录与无障碍可访问性。",
		})
	}

	if ldJoined == "" {
		score -= 10
		issues = append(issues, SEOIssueItem{
			Code:             "LIVE_NO_JSONLD",
			Severity:         "warning",
			Category:         "schema",
			Title:            "页面未检测到 Schema.org JSON-LD 结构化数据",
			CurrentValue:     "JSON-LD = 0",
			RecommendedValue: "注入规范 Product / Organization / CollectionPage JSON-LD",
			Impact:           "无法被 Google Rich Results 与 AI 引擎结构化提取。",
		})
	} else if strings.Contains(ldJoined, `"Offers"`) || strings.Contains(ldJoined, `"query-Input"`) || strings.Contains(ldJoined, `"price": 0.0`) || strings.Contains(ldJoined, `Jenny Ji:`) {
		score -= 14
		issues = append(issues, SEOIssueItem{
			Code:             "LIVE_JSONLD_DEFECT",
			Severity:         "critical",
			Category:         "schema",
			Title:            "检测到 Schema JSON-LD 语法或数值缺陷",
			CurrentValue:     "存在大写键名 'Offers'/'query-Input'、零价格 0.0 或非规范 Email 字段",
			RecommendedValue: "全部按 Schema.org 标准小写键名重写，并填入真实 FOB 价格区间与企业邮箱",
			Impact:           "Rich Results 结构化数据验证报错，富媒体卡片不生效。",
		})
	}

	if canon == "" {
		score -= 6
		issues = append(issues, SEOIssueItem{
			Code:             "LIVE_NO_CANONICAL",
			Severity:         "warning",
			Category:         "meta",
			Title:            "缺少 rel=canonical 规范链接声明",
			CurrentValue:     "(missing)",
			RecommendedValue: "添加 <link rel=\"canonical\" href=\"" + parsed.String() + "\" />",
			Impact:           "多路径或带参数访问时易分散页面权重。",
		})
	}

	if score < 25 {
		score = 25
	}

	optTitle := title
	if optTitle == "" {
		optTitle = "KOOYING Commercial Brake Linings & Disc Pads | IATF16949"
	} else {
		optTitle = strings.ReplaceAll(optTitle, "Btake", "Brake")
		optTitle = strings.ReplaceAll(optTitle, "btake", "brake")
		optTitle = strings.ReplaceAll(optTitle, "  ", " ")
		if !strings.Contains(strings.ToUpper(optTitle), "KOOYING") {
			optTitle = strings.TrimSpace(optTitle) + " | KOOYING"
		}
		if len([]rune(optTitle)) > 64 {
			optTitle = string([]rune(optTitle)[:61]) + "..."
		}
	}

	optH1 := ""
	if len(h1Texts) > 0 {
		optH1 = strings.ReplaceAll(h1Texts[0], "Btake", "Brake")
		optH1 = strings.ReplaceAll(optH1, "  ", " ")
	} else {
		optH1 = "IATF16949 Commercial Truck Brake Linings & WVA Air Disc Pads — KOOYING Factory Direct"
	}

	optDesc := fmt.Sprintf("Zhejiang Kooying Auto Parts (43,000㎡, 15M sets/yr): %s. Certified IATF16949 & ECE R90, Krauss 400C wear 0.040mm.", strings.TrimSuffix(optH1, " | KOOYING"))
	if len([]rune(optDesc)) > 160 {
		optDesc = string([]rune(optDesc)[:157]) + "..."
	}

	pathStr := parsed.Path
	if pathStr == "" {
		pathStr = "/"
	}
	return &KooyingPageAudit{
		ID:                "live_" + strings.Trim(strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(pathStr), "_"),
		URL:               parsed.String(),
		Path:              pathStr,
		PageType:          "实时抓取分析页",
		HTTPStatus:        resp.StatusCode,
		SizeBytes:         len(rawBytes),
		Title:             title,
		TitleLen:          len([]rune(title)),
		MetaDescription:   desc,
		MetaDescLen:       len([]rune(desc)),
		MetaKeywords:      kw,
		H1Count:           len(h1Matches),
		H1Texts:           h1Texts,
		H2Count:           len(h2Matches),
		H2Sample:          h2Sample,
		ImageTotal:        len(imgs),
		ImageMissingAlt:   missingAlt,
		HasCanonical:      canon != "",
		CanonicalURL:      canon,
		HasJSONLD:         ldJoined != "",
		RawJSONLD:         ldJoined,
		Score:             score,
		Issues:            issues,
		OptimizedTitle:    optTitle,
		OptimizedMetaDesc: optDesc,
		OptimizedH1:       optH1,
		OptimizedSlug:     strings.ReplaceAll(parsed.String(), "btake-lining", "brake-lining"),
		OptimizedJSONLD:   fmt.Sprintf("{\n  \"@context\": \"https://schema.org\",\n  \"@type\": \"WebPage\",\n  \"name\": %q,\n  \"url\": %q,\n  \"publisher\": { \"@type\": \"Organization\", \"name\": \"Zhejiang Kooying Auto Parts Co., Ltd.\" }\n}", optH1, parsed.String()),
		CheckedAt:         checkedAt,
		IsLiveFetched:     true,
	}, nil
}

// AuditKooyingOfficialSite inspects Zhejiang Kooying's live official site https://www.kooying.com/
// Combining verified baseline findings (robots/sitemap/llms/JSON-LD/H1/slug typos) with optional live HTTP fetch.
func AuditKooyingOfficialSite(ctx context.Context, customURL string, liveFetch bool) (*KooyingSiteAuditOverview, error) {
	checkedAt := nowISO()
	pages := baselineKooyingPages(checkedAt)
	probeErr := ""

	targetURL := strings.TrimSpace(customURL)
	if liveFetch || targetURL != "" {
		if targetURL == "" {
			targetURL = "https://www.kooying.com/"
		}
		livePage, err := fetchAndAuditLiveKooyingURL(ctx, targetURL, checkedAt)
		if err != nil {
			probeErr = err.Error()
		} else if livePage != nil {
			matched := false
			normTarget := strings.TrimRight(livePage.URL, "/")
			for i := range pages {
				if strings.TrimRight(pages[i].URL, "/") == normTarget {
					// Preserve curated recommendations when matching known baseline pages, while updating live telemetry
					if pages[i].OptimizedTitle != "" {
						livePage.ID = pages[i].ID
						livePage.PageType = pages[i].PageType + " (实时抓检)"
						livePage.OptimizedTitle = pages[i].OptimizedTitle
						livePage.OptimizedMetaDesc = pages[i].OptimizedMetaDesc
						livePage.OptimizedH1 = pages[i].OptimizedH1
						livePage.OptimizedSlug = pages[i].OptimizedSlug
						livePage.OptimizedJSONLD = pages[i].OptimizedJSONLD
					}
					pages[i] = *livePage
					matched = true
					break
				}
			}
			if !matched {
				pages = append([]KooyingPageAudit{*livePage}, pages...)
			}
		}
	}

	totalScore := 0
	criticalCount := 0
	warningCount := 0
	passedCount := 0
	for _, p := range pages {
		totalScore += p.Score
		for _, iss := range p.Issues {
			switch iss.Severity {
			case "critical":
				criticalCount++
			case "warning":
				warningCount++
			case "pass":
				passedCount++
			}
		}
	}
	overallScore := 62
	if len(pages) > 0 {
		overallScore = int(math.Round(float64(totalScore) / float64(len(pages))))
	}

	robotsCurrent := `User-agent: AhrefsBot 
Disallow: / 
User-agent: SemrushBot
Disallow: / 
User-Agent: Googlebot 
Allow: /
User-agent: GPTBot
Allow: /`

	robotsOptimized := `User-agent: *
Allow: /
Disallow: /api/min/

# Explicit AI & Search Crawler Allowances (GEO + SEO)
User-agent: Googlebot
Allow: /
User-agent: Bingbot
Allow: /
User-agent: OAI-SearchBot
Allow: /
User-agent: GPTBot
Allow: /
User-agent: PerplexityBot
Allow: /
User-agent: ClaudeBot
Allow: /

# Competitor scrapers kept blocked
User-agent: AhrefsBot
Disallow: /
User-agent: SemrushBot
Disallow: /

Sitemap: https://www.kooying.com/sitemap.xml`

	llmsCurrentSample := `<pre style="word-wrap:break-word; white-space:pre-wrap;">

# Truck & Automobile Brake Lining Manufacturers, Suppliers

> ZHEJIANG KOOYING AUTOPARTS CO.,LTD. is Truck/Automobile Brake Lining Manufacturers and Wholesale Brake Drum, Brake Pads Factory, Brake Shoes Suppliers in China

### Company
ZHEJIANG KOOYING AUTOPARTS CO.,LTD.
(缺陷：响应体最外层被 CMS 包裹了 HTML <pre> 标签，且缺少 WVA/尺寸/400C 耐磨实测与 30s 选型直达事实)
</pre>`

	llmsOptimized := `# Zhejiang Kooying Auto Parts Co., Ltd. (KOOYING / GOOLE)

> IATF16949 & ECE R90 Commercial Brake Linings, Air Disc Brake Pads & Drum Shoes Manufacturer (Huzhou, China · 43,000㎡ Plant · 15M Sets/Year).

## Engineering & Friction Proof
- Asbestos-free formulations (GB5763-2018, SAE J661, ECE R90) across 30+ heavy-duty formulas
- Krauss 400C constant-torque dynamometer test: 0.040 mm thickness wear (vs 0.085 mm benchmark), 2.300 g weight loss, stable μ = 0.35–0.38
- Full dimensional cross-match by WVA/FMSI code, Axle (BPW, Meritor, SAF, Fuwa, Hande 35T), or Outer Arc × Width × Thickness + Rivet Holes

## Priority Product Series
- [Air Disc Brake Pads (WVA 29087, 29030, 29162, 29244)](https://www.kooying.com/product/brake-pads/): CV disc assemblies for European & global trucks
- [European Heavy-Duty Brake Linings (BPW, Mercedes-Benz, Volvo, Scania, SAF)](https://www.kooying.com/product/european-vehicle-brake-lining/): Precision 6–26 hole linings
- [American & Japanese Heavy-Duty Linings (FMSI 4515/4707, HINO, ISUZU)](https://www.kooying.com/product/american-vehicle-brake-lining/): High-load fleet & mining aftermarket
- [Company & 43,000m2 Production Base](https://www.kooying.com/about/company-profile.html): In-house automated thermal pressing & testing

## Direct Export Contact
- Phone / WhatsApp: +86-159-5711-8669 | Email: sales@kooying.com`

	return &KooyingSiteAuditOverview{
		TargetDomain:         "https://www.kooying.com",
		OverallScore:         overallScore,
		ProjectedScore:       96,
		SitemapURLCount:      468,
		CriticalCount:        criticalCount,
		WarningCount:         warningCount,
		PassedCount:          passedCount,
		LiveProbeError:       probeErr,
		Pages:                pages,
		SitemapTypos:         baselineSitemapTypos(),
		RobotsTxtCurrent:     robotsCurrent,
		RobotsTxtOptimized:   robotsOptimized,
		LlmsTxtCurrentSample: llmsCurrentSample,
		LlmsTxtOptimized:     llmsOptimized,
		CheckedAt:            checkedAt,
	}, nil
}
