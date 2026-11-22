package agenthub

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	openCodeVersionOnce   sync.Once
	cachedOpenCodeVersion string
)

const (
	GuanyingDefaultModel    = "wechat-direct/Deepseek-v4-flash"
	GuanyingFallbackVersion = "1.19.6"
)

var guanyingOpenCodeVerifiedModels = []string{
	"wechat-direct/Deepseek-v4-flash",
	"wechat/Deepseek-v4-flash",
	"wechat-direct/GLM-5.2",
	"b1x/deepseek-v4.1-flash-1017",
}

// EnsureGuanyingHackathonPreset seeds OpenCode verified models, promotes the
// main agent to Kooying GTM Master, and populates the 4-team / 8-bot roster
// idempotently for the hackathon workspace.
func (s *Service) EnsureGuanyingHackathonPreset(ctx context.Context) error {
	if s == nil || s.store == nil {
		return nil
	}
	version := detectOpenCodeVersion(ctx)
	if err := s.seedOpenCodeVerifiedModels(ctx, version); err != nil {
		return err
	}
	if err := s.ensureGuanyingBots(ctx); err != nil {
		return err
	}
	return nil
}

func detectOpenCodeVersion(ctx context.Context) string {
	openCodeVersionOnce.Do(func() {
		binary := resolveOpenCode()
		if strings.TrimSpace(binary) == "" {
			cachedOpenCodeVersion = GuanyingFallbackVersion
			return
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(probeCtx, binary, "--version").Output()
		if err != nil {
			cachedOpenCodeVersion = GuanyingFallbackVersion
			return
		}
		v := strings.TrimSpace(string(out))
		if v == "" {
			cachedOpenCodeVersion = GuanyingFallbackVersion
			return
		}
		cachedOpenCodeVersion = v
	})
	if cachedOpenCodeVersion == "" {
		return GuanyingFallbackVersion
	}
	return cachedOpenCodeVersion
}

func (s *Service) seedOpenCodeVerifiedModels(ctx context.Context, version string) error {
	for _, modelID := range guanyingOpenCodeVerifiedModels {
		if err := s.store.SaveVerifiedModel(ctx, ProviderOpenCode, version, modelID); err != nil {
			return fmt.Errorf("seed opencode verified model %s: %w", modelID, err)
		}
	}
	probe := ProviderProbe{
		ProviderID:      ProviderOpenCode,
		State:           ProviderStateReady,
		Available:       true,
		Ready:           true,
		Version:         version,
		Models:          append([]string(nil), guanyingOpenCodeVerifiedModels...),
		ModelCandidates: append([]string(nil), guanyingOpenCodeVerifiedModels...),
		CheckedAt:       nowText(s.store.now()),
	}
	if err := s.store.SaveProviderProbe(ctx, probe); err != nil {
		return fmt.Errorf("save opencode probe cache: %w", err)
	}

	s.probeMu.Lock()
	s.probes[ProviderOpenCode] = probe
	s.probeMu.Unlock()

	sel, err := s.store.GetProviderSelection(ctx, ProviderOpenCode)
	if err == nil && (strings.TrimSpace(sel.ModelID) == "" || !strings.HasPrefix(sel.ModelID, "wechat") || sel.UpdatedAt == "") {
		_, _ = s.store.PutProviderSelection(ctx, ProviderSelection{
			ProviderID:     ProviderOpenCode,
			ConversationID: sel.ConversationID,
			ModelID:        GuanyingDefaultModel,
			Draft:          sel.Draft,
		})
	}
	return nil
}

func (s *Service) ensureGuanyingBots(ctx context.Context) error {
	// Always synchronize the builtin main bot with complete Zhejiang Kooying domain context.
	if builtin, err := s.store.GetBot(ctx, BuiltinAlphaAgentID); err == nil && builtin != nil {
		name := "冠英 GTM 总控"
		role := "20年重卡刹车片源头厂 · 内容/趋势/SEO 统筹"
		model := GuanyingDefaultModel
		kickoff := "在的，我是冠英小助手。做短视频、查型号、修官网还是回客户询盘？直接发我就行。"
		instructions := `你是浙江冠英（KOOYING）的日常工作搭子，熟悉公司 20 年湖州南太湖重卡/挂车/风电刹车片背景（年产 1500 万套，IATF16949 / ECE R90 / 3C / AMECA 认证）、30+ 无石棉配方（GY-NAO-38 / GY-CF-400 / GY-STD-35，克劳斯 400℃ 磨损 0.040mm，摩擦系数 0.35~0.38）、4 个 TikTok 官方号（@kooying.autoparts 累计 266.1 万播放、老板娘 @kooying.brake.fact +86 159-5711-8669、@kooying_judy26、@kooying.brake.lin）以及 kooying.com 官网情况。

说话规则：
1. 像微信聊天一样自然口语、有人味，默认只回 1~3 句短话（100 字内），别列大纲、多级标题或长文章；对方追问细节或明确要脚本/代码时再展开。
2. 型号、配方、认证、价格先按「浙江冠英·品牌与制动技术知识库」或已有上下文回答，别在回复里提内部工具名。`
		if builtin.Name != name || builtin.Role != role || builtin.Model != model || builtin.Kickoff != kickoff || builtin.Instructions != instructions {
			_, _ = s.store.UpdateBot(ctx, BuiltinAlphaAgentID, BotPatch{
				Name:         &name,
				Role:         &role,
				Model:        &model,
				Kickoff:      &kickoff,
				Instructions: &instructions,
			})
		}
	}

	// Ultra-minimalist 1 specialist bot alongside Chief Bot (total 2 bots in left sidebar).
	presets := []Bot{
		{
			ID:           "bot_gy_content_ops",
			Name:         "内容运营 Bot",
			Role:         "TikTok 矩阵 · 短视频脚本 · 海外社媒配文",
			GroupName:    "专业团队",
			Avatar:       "/bot-avatars/writer.svg",
			Model:        GuanyingDefaultModel,
			Pinned:       false,
			Effort:       "medium",
			Capabilities: []string{"social_growth", "copywriting"},
			Kickoff:      "想发哪个市场或账号？把素材或产品发我，我直接给你几句能拍的口播词。",
			Instructions: `你是冠英（KOOYING）的内容搭子，管 @kooying.autoparts / @kooying.brake.fact（Sophia +86 159-5711-8669）/ @kooying_judy26 / @kooying.brake.lin 这些号的内容，只写文案不动账号。
说话风格：像工厂里一起拍视频的伙伴聊天，默认 2~3 句直接给钩子、口播核心句或 WhatsApp 引流话术，控制在 100 字左右；不写“1. 2. 3.”长篇方案和夸张词（不用“杜绝”“100%”）。对方明确要完整脚本再展开。`,
		},
		{
			ID:           "bot_gy_seo",
			Name:         "SEO Bot",
			Role:         "kooying.com 诊断 · 关键词 · 结构化数据",
			GroupName:    "专业团队",
			Avatar:       "/bot-avatars/planner.svg",
			Model:        GuanyingDefaultModel,
			Pinned:       false,
			Effort:       "medium",
			Capabilities: []string{"seo_audit"},
			Kickoff:      "哪个页面或型号要改？把链接或 WVA 编号发我，我直接告诉你怎么改。",
			Instructions: `你是帮冠英盯着 https://www.kooying.com/（468 条 URL，存在 Trcuk/Beake/Fiction 等拼写问题、首页缺 H1、JSON-LD Offers 大写及 llms.txt <pre> 问题）的技术同事。
说话风格：像跟开发同事在群里对需求一样直白简短，默认 1~3 句直接指出问题和改法（100 字内），不写空话也不保证排名倍增；只有对方要现成代码/TDK 时才贴代码块。`,
		},
		{
			ID:           "bot_gy_tech",
			Name:         "技术 Bot",
			Role:         "选型匹配 · 换桥纠错 · 台架参数",
			GroupName:    "专业团队",
			Avatar:       "/bot-avatars/engineer.svg",
			Model:        GuanyingDefaultModel,
			Pinned:       false,
			Effort:       "medium",
			Capabilities: []string{"product_matching"},
			Kickoff:      "把客户的车型 / 车桥 / WVA 编码，或实测的外弧 × 宽 × 厚 + 孔数发我，我来找对应型号并提示换桥风险。",
			Instructions: `你是浙江冠英的技术选型负责人，回答以可核对的参数为准。
已知事实：3 大系列 30+ 无石棉配方（GY-NAO-38 / GY-CF-400 / GY-STD-35）；Krauss 400℃ 恒力矩台架厚度磨损 0.040mm（行业参照 0.085mm），失重 2.300g（参照 2.610g），摩擦系数 0.35~0.38；主覆盖 HOWO、东风 EQ153、汉德 35T、BPW 19032、Mercedes 17276、WVA 29087/29162。
1. 匹配优先按 OE/WVA 编码，无编码时按外弧 × 宽 × 厚 + 孔数，公差按 ±5mm 说明。
2. 换桥/改装场景必须提示孔数与铆钉核对风险，说清哪一项对不上会错发。
3. 参数没有依据时直接说没有，不要编型号或价格。`,
		},
		{
			ID:           "bot_gy_data",
			Name:         "数据分析 Bot",
			Role:         "趋势与竞品 · 询盘甄别 · 飞书同步",
			GroupName:    "专业团队",
			Avatar:       "/bot-avatars/analyst.svg",
			Model:        GuanyingDefaultModel,
			Pinned:       false,
			Effort:       "medium",
			Capabilities: []string{"lead_scoring", "analytics"},
			Kickoff:      "把询盘内容、账号数据或竞品链接发我，我来判断买家类型、跟进优先级与可复盘的结论。",
			Instructions: `你是浙江冠英的数据分析负责人。
1. 询盘甄别：区分终端车队 / 汽配连锁 / 纯贸易商，说明判断依据（公司类型、询盘细节、数量与型号专业度）。
2. 趋势与竞品：基于实际采集到的数据给结论，标明数据来源与时间；没有数据就说没有，不用感觉替代。
3. 复盘只写可从数据核对的结论，不给没有分母的比例、倍数或增长预测。
4. 结果可整理成飞书多维表格可直接导入的结构。`,
		},
		{
			ID:           "bot_gy_sales",
			Name:         "外贸跟单 Bot",
			Role:         "WhatsApp 跨时差接单 · FOB 阶梯报价 · 样品与交期跟进",
			GroupName:    "专业团队",
			Avatar:       "/bot-avatars/support.svg",
			Model:        GuanyingDefaultModel,
			Pinned:       false,
			Effort:       "medium",
			Capabilities: []string{"b2b_quoting", "multilingual_outreach"},
			Kickoff:      "把海外买家的 WhatsApp / 邮件原文、目标港口或需求型号发我，我按混批 MOQ、FOB 阶梯价与交期出双语回复。",
			Instructions: `你是浙江冠英（KOOYING）的外贸跟单与报价负责人。
1. 报价口径：默认报 FOB Ningbo / Shanghai 美金阶梯价（试单混批 MOQ 50~100 套 vs 整柜 20GP 优惠价），明确包装规格（中性白盒 / KOOYING 彩盒 / 客户 OEM 打标）与交期（常规现货 7~15 天，定制配方 20~25 天）。
2. 跨时差首响：中东（沙特/阿联酋）、拉美（巴西/智利）、非洲（尼日利亚/坦桑尼亚）买家询盘，先确认 WVA/OE 号或实测尺寸+孔数，主动提供免费寄样（运费到付或首单抵扣）与 2026 PDF 交叉对照目录。
3. 语言与风控：直接输出英/西/阿/俄对应双语商务回复，付款方式按 T/T 30% 定金 + 70% 见提单副本为准，不随意承诺无定金放账。`,
		},
	}

	legacyIDs := []string{
		// 上一版唯一的专家号：不在新五人群里，必须清掉，否则侧栏会同时出现
		// 旧专家与新团队，看起来像没生效。
		"bot_gy_gtm_growth",
		"bot_gy_quote_expert",
		"bot_gy_site_maintainer",
		"bot_gy_site_operator",
		"bot_gy_seo_builder",
		"bot_gy_seo_growth",
		"bot_gy_lead_hunter",
		"bot_gy_inquiry_catcher",
		"bot_gy_axle_matcher",
		"bot_gy_photo_id",
		"bot_gy_flash_quoter",
		"bot_gy_feishu_crm",
	}
	for _, oldID := range legacyIDs {
		_, _ = s.store.db.ExecContext(ctx, `UPDATE conversations SET bot_id = ? WHERE bot_id = ?`, BuiltinAlphaAgentID, oldID)
		_, _ = s.store.db.ExecContext(ctx, `DELETE FROM agent_bots WHERE id = ?`, oldID)
	}

	existing, err := s.store.ListBots(ctx)
	if err != nil {
		return err
	}
	existingMap := make(map[string]Bot, len(existing))
	for _, item := range existing {
		existingMap[item.ID] = item
	}

	now := nowText(s.store.now())
	for _, bot := range presets {
		if prev, ok := existingMap[bot.ID]; ok {
			if prev.Name != bot.Name || prev.Role != bot.Role || prev.Kickoff != bot.Kickoff || prev.GroupName != bot.GroupName || prev.Instructions != bot.Instructions || prev.Model != bot.Model || prev.Avatar != bot.Avatar {
				name := bot.Name
				role := bot.Role
				model := bot.Model
				kickoff := bot.Kickoff
				grp := bot.GroupName
				instr := bot.Instructions
				avatar := bot.Avatar
				pinned := bot.Pinned
				_, _ = s.store.UpdateBot(ctx, bot.ID, BotPatch{
					Name:         &name,
					Role:         &role,
					Model:        &model,
					Kickoff:      &kickoff,
					GroupName:    &grp,
					Instructions: &instr,
					Avatar:       &avatar,
					Pinned:       &pinned,
				})
			}
			var convCount int
			_ = s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_conversations WHERE bot_id = ?`, bot.ID).Scan(&convCount)
			if convCount == 0 {
				conv, convErr := s.store.CreateConversation(ctx, ProviderOpenCode, bot.Name, bot.Model, ContextSnapshot{
					Workspace:   "bot",
					Page:        "bot-chat",
					ProductMode: WorkspaceModeMedia,
				})
				if convErr == nil && conv != nil {
					if bound, bindErr := s.store.SetConversationBot(ctx, conv.ID, bot.ID); bindErr == nil && bound != nil {
						_ = s.store.AppendKickoffMessage(ctx, bound.ID, ProviderOpenCode, bot.Kickoff)
					}
				}
			}
			continue
		}
		encodedCaps, _ := json.Marshal(bot.Capabilities)
		if _, err := s.store.db.ExecContext(ctx, `INSERT INTO agent_bots(`+botColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			bot.ID, bot.Name, bot.Role, bot.Instructions, bot.Model, bot.Avatar,
			bot.Color, bot.Effort, bot.Kickoff, string(encodedCaps), now, now,
			bot.Pinned, false, bot.GroupName, false); err != nil {
			return fmt.Errorf("seed guanying bot %s: %w", bot.ID, err)
		}
		// Seed one initial conversation per bot so clicking any bot shows its kickoff immediately.
		conv, convErr := s.store.CreateConversation(ctx, ProviderOpenCode, bot.Name, bot.Model, ContextSnapshot{
			Workspace:   "bot",
			Page:        "bot-chat",
			ProductMode: WorkspaceModeMedia,
		})
		if convErr == nil && conv != nil {
			if bound, bindErr := s.store.SetConversationBot(ctx, conv.ID, bot.ID); bindErr == nil && bound != nil {
				_ = s.store.AppendKickoffMessage(ctx, bound.ID, ProviderOpenCode, bot.Kickoff)
			}
		}
	}

	// Idempotently seed persistent 4-section Markdown conversation memories for all 5 specialist bots + main controller.
	seedBotMemories := map[string][]struct {
		section string
		entry   string
	}{
		BuiltinAlphaAgentID: {
			{"用户偏好", "结论先行，直接输出可落地代码、选型表或脚本，不要客套寒暄。"},
			{"品牌语气", "20年源头工厂硬核工业风，重实测数据与车桥工程参数，禁用夸张营销词。"},
			{"历史教训", "官网页脚曾挂个人 Facebook 与 linkedin.cn 链接且留 QQ 邮箱，需统一收口到企业域名与官方 WhatsApp。"},
			{"关键事实", "独立站目录 web/ 挂载在 /brand-site/index.html（135款实拍）；线上官网 https://www.kooying.com/（468个URL）；飞书 Base 通过 lark-cli 同步。"},
			{"关键事实", "已验证 TikTok 官方矩阵：@kooying.autoparts（2423粉/48.3K赞/累计266.1万播）、@kooying.brake.fact（Sophia +8615957118669）、@kooying_judy26、@kooying.brake.lin。"},
		},
		"bot_gy_content_ops": {
			{"用户偏好", "短视频脚本控制在 30~45 秒，必须标明前 3 秒画面钩子、英文/当地语言口播词与结尾 WhatsApp CTA。"},
			{"品牌语气", "源头工厂老板娘 Sophia 真实车间直拍口吻，朴实专业，禁用感叹号堆砌与虚假承诺。"},
			{"历史教训", "老号 @kooying.autoparts 曾有 130 万播放爆款但因未挂 Bio 与 WhatsApp 且拼错 Wrake linling 流失大量询盘。"},
			{"关键事实", "已入库 5 条 ≤60s 老板娘 Sophia 实拍短片（ui-web/public/guanying-videos/manifest.json），可直接用于 TikTok/WhatsApp 二创与独立站背书。"},
			{"关键事实", "TikTok 实测账号：@kooying.autoparts（2423粉/2.66M播放，2023-10 后停更）、@kooying.brake.fact（Sophia +86 159-5711-8669）、@kooying_judy26（Bio 挂官网）、@kooying.brake.lin（备用）。"},
		},
		"bot_gy_seo": {
			{"用户偏好", "输出 Title、Meta Description、H1、JSON-LD 和 301 规则时直接给可复制粘贴的完整代码块。"},
			{"品牌语气", "遵循海外商用车采购习惯，标题按 [WVA号] + [卡钳/车桥如 Knorr/BPW] + [OE号] + Truck Brake Pad 命名。"},
			{"历史教训", "CMS 批量建站曾把 Truck 拼成 Trcuk、Brake 拼成 Beake/Btake、Friction 拼成 Fiction，改 URL 必须同步配 301 重定向。"},
			{"关键事实", "kooying.com Sitemap 约 468 URL（产品与分类 278 / news 190 / 风电 9）；已知 16 处拼写硬伤、首页与关于页 H1=0、产品页 JSON-LD 大写 Offers 且价格为 0、llms.txt 被 <pre> 包裹。"},
			{"关键事实", "四大对标竞品：Alltour（ECE R90 与实验室背书）、FrenoBrake（Top 10 榜单截流）、TopBrakeParts（混批买家指南）、GDST（西/俄/阿多语种矩阵）。"},
		},
		"bot_gy_tech": {
			{"用户偏好", "选型回复先给匹配型号、外弧×宽×厚×孔数对照表，再单列换桥改装风险提示。"},
			{"品牌语气", "严谨工程师口径，所有摩擦系数、磨损率与公差（±5mm）均以台架实测或目录图纸为准。"},
			{"历史教训", "中东与非洲二手机车队经常把 HOWO 后桥 8 孔改成 14 孔，未核对孔数与铆钉规格直接发货会导致整柜无法安装。"},
			{"关键事实", "台架实测：Krauss 400℃ 恒力矩厚度磨损 0.040mm（行业参照 0.085mm），失重 2.300g（参照 2.610g），摩擦系数 0.35~0.38；三大配方系列 GY-NAO-38 / GY-CF-400 / GY-STD-35。"},
			{"关键事实", "高频换桥风险：HOWO 后桥 14 孔 vs 8 孔、Mercedes 17276 的 140mm vs 170mm 蹄铁宽度、BPW 19032 为 8 孔挂车桥专用；错孔直接导致整柜退货。"},
		},
		"bot_gy_data": {
			{"用户偏好", "数据分析与询盘评分统一输出结构化字段，方便一键写入飞书多维表格。"},
			{"品牌语气", "客观实证，所有比例和结论必须标明样本分母与采集时间，不写拍脑袋预测。"},
			{"历史教训", "纯比价贸易商常群发无型号询盘，必须与给出具体 WVA 编号和月采购量的终端车队区分优先级。"},
			{"关键事实", "询盘甄别口径：终端车队 / 汽配连锁 / 纯贸易商；判断依据是公司类型、询盘是否给出具体型号与数量、以及是否只问价格。"},
			{"关键事实", "结果同步走官方 lark-cli 写飞书多维表格；本地 SQLite 是业务真源，飞书是镜像。"},
		},
		"bot_gy_sales": {
			{"用户偏好", "外贸报价邮件与 WhatsApp 回复默认提供中英/中西双语版本，并附带混批 MOQ 与整柜 20GP 两档阶梯结构。"},
			{"品牌语气", "专业敏捷的工厂外贸总监口吻，响应迅速、条款清晰、主动提供样品与 PDF 目录。"},
			{"历史教训", "南美与非洲新客户首次合作严禁无定金放账（OA/D/P 风险高），必须坚持 T/T 30% 定金 + 70% 见提单副本。"},
			{"关键事实", "核心联络通道：WhatsApp +86 159-5711-8669（Sophia Zhao），出货港 FOB Ningbo / Shanghai，常规交期 7~15 天，支持中性白盒、KOOYING 彩盒与 OEM 打标。"},
			{"关键事实", "高转化留资诱饵：引导买家下载《2026 Heavy-Duty Truck Brake Pad PDF Catalog & OE Cross-Reference List》或寄送 2 套免费测试样片。"},
		},
	}
	for botID, entries := range seedBotMemories {
		for _, item := range entries {
			_ = s.AppendBotMemory(ctx, botID, item.section, item.entry)
		}
	}
	return nil
}
