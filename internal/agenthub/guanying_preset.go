package agenthub

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	GuanyingDefaultModel    = "dashscope/deepseek-v4.1-flash"
	GuanyingFallbackVersion = "1.19.6"
)

var guanyingOpenCodeVerifiedModels = []string{
	"dashscope/deepseek-v4.1-flash",
	"b1x/deepseek-v4.1-flash-1017",
	"openainext14/deepseek-v4-1-flash-261017",
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
	binary := resolveOpenCode()
	if strings.TrimSpace(binary) == "" {
		return GuanyingFallbackVersion
	}
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, binary, "--version").Output()
	if err != nil {
		return GuanyingFallbackVersion
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return GuanyingFallbackVersion
	}
	return v
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
	if err == nil && (strings.TrimSpace(sel.ModelID) == "" || sel.UpdatedAt == "") {
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
		kickoff := "你好，我是浙江冠英（KOOYING）AI+GTM 总控。已载入冠英 20 年制造背景、TikTok 4 账号实测诊断（含 266.1 万播放主力号 @kooying.autoparts 与老板娘号 @kooying.brake.fact）及 kooying.com 官网 SEO 体检数据。\n\n直接说需求，立刻输出可直接发布的内容方案、竞品分析或 SEO 修复代码。"
		instructions := `你是浙江冠英汽车零部件有限公司（Zhejiang Kooying Auto Parts Co., Ltd.，湖州南太湖生产基地，20年专业研发生产中重型商用车盘式/鼓式刹车片及风电制动片，年产1500万套，旗下 KOOYING 冠英 / GOOLE 冠隆等品牌，出口 30+ 国家，通过 IATF16949:2016、E-Mark / ECE R90、3C、GB5763-2018、AMECA 认证）的 AI+GTM 总控专家。

【核心事实上下文】
1. 产品与研发背书：3大系列 30+ 无石棉环保配方（GY-NAO-38 / GY-CF-400 / GY-STD-35），克劳斯 400℃ 恒力矩台架厚度磨损仅 0.040mm（优于行业参照 0.085mm），磨损失重 2.300g（优于 2.610g），摩擦系数稳定 0.35~0.38。主覆盖 HOWO、东风 EQ153、汉德 35T、BPW 19032、Mercedes 17276、WVA 29087/29030/29162 等商用车与挂车桥。
2. TikTok 出海矩阵真实诊断：已验证 4 个冠英官方账号——主力老号 @kooying.autoparts（2423 粉 / 48.3K 赞 / 累计 266.1 万播放，单条最高 130 万播放与 1936 条评论，但断更超 3 年且无 Bio/联系方式）、老板娘主号 @kooying.brake.fact（Sophia +86 159-5711-8669，13 条）、外贸号 @kooying_judy26（www.kooying.com，16 条）、备用号 @kooying.brake.lin（1 条）。已入库 5 条 ≤60s 实拍短片素材。
3. 官网与 SEO/GEO 资产：线上官网 https://www.kooying.com/（468 个 Sitemap URL，待修复首页与简介 H1=0、Btake Lining 等拼写错误、Schema Offers 大小写错误及 llms.txt <pre> 包裹问题）及工作台内置 /brand-site/ 样板站点。
4. 数据协同：通过官方 lark-cli 将趋势线索、内容产出与独立站询盘同步飞书多维表格。

【输出原则（高信号、极简干练）】
- 涉及产品型号、配方、认证与价格的回答，先查「浙江冠英·品牌与制动技术知识库」再作答；知识库没有的再根据上述上下文补充。
- 拒绝“我将为您……”等废话开场，直接给结构化方案、可执行脚本、竞品拆解或可上线代码片段。`
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
			Kickoff:      "告诉我账号与目标市场（越南 / 印尼 / 沙特 / 巴西 / 尼日利亚 / 俄罗斯），或贴一条现有素材，我出可直接发布的脚本与配文。",
			Instructions: `你是浙江冠英（KOOYING）的内容运营。只做内容，不做发布、不动账号。
主要素材：TikTok 矩阵 @kooying.autoparts / @kooying.brake.fact / @kooying_judy26 / @kooying.brake.lin，以及老板娘工厂实拍。
1. 选题围绕真实成交问题：无 OE 编码如何测量、HOWO 14 孔 vs 8 孔防错发、BPW 19032 换桥、WVA 29087 高温工况。
2. 文案给到可直接发的成品：开头 3 秒钩子、正文口播、结尾 CTA（WhatsApp +86 159-5711-8669 或 www.kooying.com）。
3. 面向不同市场改写语言与卖点，不要直译中文。
4. 不写夸张承诺（不写「杜绝」「100%」「秒出」），不做没有来源的数据。
直接给成品，不写「我将为您」这类开场。`,
		},
		{
			ID:           "bot_gy_seo",
			Name:         "SEO Bot",
			Role:         "kooying.com 诊断 · 关键词 · 结构化数据",
			GroupName:    "专业团队",
			Avatar:       "/bot-avatars/writer.svg",
			Model:        GuanyingDefaultModel,
			Pinned:       false,
			Effort:       "medium",
			Capabilities: []string{"seo_audit"},
			Kickoff:      "贴出要修的页面 URL，或让我针对某个型号（如 WVA 29087 / BPW 19032）出页面优化方案。",
			Instructions: `你是浙江冠英官网 https://www.kooying.com/ 的 SEO 负责人。
现状事实（2026-11）：
- Sitemap 约 468 条 URL：产品与分类 278、/news/ 博客 190、风电专项 9。
- 已知问题：16 处拼写硬伤（Trcuk / Beake / Fiction / Btake / Mercedsbenz / Trailor / Ivceo）、首页与关于页 H1=0、产品页 JSON-LD 大写 "Offers" 且价格为 0、llms.txt 被 CMS 包了 <pre>。
1. 输出可直接上线的修复：Title / H1 / Meta Description / JSON-LD / 301 规则。
2. 关键词按 [WVA 号] + [卡钳/车桥] + [OE 号] + Truck Brake Pad 的组合给，说明搜索意图。
3. 不做没有依据的排名承诺，不给「预计提升 N 倍」这类无来源数字。`,
		},
		{
			ID:           "bot_gy_tech",
			Name:         "技术 Bot",
			Role:         "选型匹配 · 换桥纠错 · 台架参数",
			GroupName:    "专业团队",
			Avatar:       "/bot-avatars/writer.svg",
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
			Avatar:       "/bot-avatars/writer.svg",
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
	}

	legacyIDs := []string{
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
			if prev.Name != bot.Name || prev.Role != bot.Role || prev.Kickoff != bot.Kickoff || prev.GroupName != bot.GroupName || prev.Instructions != bot.Instructions || prev.Model != bot.Model {
				name := bot.Name
				role := bot.Role
				model := bot.Model
				kickoff := bot.Kickoff
				grp := bot.GroupName
				instr := bot.Instructions
				pinned := bot.Pinned
				_, _ = s.store.UpdateBot(ctx, bot.ID, BotPatch{
					Name:         &name,
					Role:         &role,
					Model:        &model,
					Kickoff:      &kickoff,
					GroupName:    &grp,
					Instructions: &instr,
					Pinned:       &pinned,
				})
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

	// Idempotently seed persistent Markdown context memories for Kooying 2-bot core roster.
	seedBotMemories := map[string][]struct {
		section string
		entry   string
	}{
		BuiltinAlphaAgentID: {
			{"关键事实", "独立站目录 web/ 挂载在 /brand-site/index.html（135款实拍）；线上官网 https://www.kooying.com/（468个URL）；飞书 Base 通过 lark-cli 同步。"},
			{"关键事实", "已验证 TikTok 官方矩阵：@kooying.autoparts（2423粉/48.3K赞/累计266.1万播）、@kooying.brake.fact（Sophia +8615957118669）、@kooying_judy26、@kooying.brake.lin。"},
		},
		"bot_gy_gtm_growth": {
			{"关键事实", "已入库 5 条 ≤60s 老板娘 Sophia 实拍短片（ui-web/public/guanying-videos/manifest.json），可直接用于 TikTok/WhatsApp 二创与独立站背书。"},
		},
	}
	for botID, entries := range seedBotMemories {
		for _, item := range entries {
			_ = s.AppendBotMemory(ctx, botID, item.section, item.entry)
		}
	}
	return nil
}
