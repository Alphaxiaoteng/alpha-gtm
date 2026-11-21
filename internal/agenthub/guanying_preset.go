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
	GuanyingDefaultModel = "dashscope/deepseek-v4.1-flash"
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
	// Upgrade the builtin main bot only if it is still on default AlphaBot naming.
	if builtin, err := s.store.GetBot(ctx, BuiltinAlphaAgentID); err == nil && builtin != nil {
		if builtin.Name == "AlphaBot" || builtin.Name == "Alpha Agent" {
			name := "冠英 GTM 总控"
			role := "浙江冠英 AI+GTM 总控与调度中枢"
			model := GuanyingDefaultModel
			kickoff := "你好，我是浙江冠英（Kooying）AI+GTM 总控 Agent。我可以调度 4 大业务团队，帮你完成：1) 车型/车桥/WVA编码/外弧×宽度×厚度×孔数秒级选型与 30 秒双语报价；2) 海外终端车队/汽配商甄别与跨时差询盘接单；3) 多语种外贸 SEO 独立站建站；4) 一键同步全量数据到飞书多维表格（lark-cli）。"
			instructions := `你是浙江冠英汽车零部件有限公司（Zhejiang Kooying Auto Parts，湖州南太湖生产基地，年产1500万套商用车盘式/鼓式/风电刹车片，旗下冠英 Kooying / 冠隆 GOOLE / 新莱 / 圣雕 / 卡能 / 三安品牌，销往东南亚、中东、南美、北美 30+ 国家，通过 IATF16949:2016、3C、E-Mark、GB5763-2018 认证）的 AI+GTM 总控 Agent。
核心事实与业务边界：
1. 冠英拥有 3 大系列 30+ 无石棉配方，克劳斯 400℃ 恒力矩厚度磨损仅 0.040mm（优于行业参照 0.085mm），磨损失重 2.300g（优于 2.610g），摩擦系数稳定在 0.35~0.38。
2. 针对「没有主机厂 OE 编码、后市场换桥、同品不同码、报价慢(2小时/条)」痛点，优先按「车型/车桥/WVA/GYS/GY编码」或「外弧(A)×宽度(C)×厚度(D)+孔数」给出精确匹配及换桥提醒。
3. 针对「海关数据全是贸易商、社媒时差漏单」，强调通过外贸 SEO 独立站+社媒筛选海外真实车队、汽配连锁与车桥厂，并通过 lark-cli 沉淀进飞书多维表格。`
			_, _ = s.store.UpdateBot(ctx, BuiltinAlphaAgentID, BotPatch{
				Name:         &name,
				Role:         &role,
				Model:        &model,
				Kickoff:      &kickoff,
				Instructions: &instructions,
			})
		}
	}

	presets := []Bot{
		{
			ID:        "bot_gy_seo_builder",
			Name:      "外贸独立站建站师",
			Role:      "多语种 SEO 独立站与 Product Schema 架构师",
			GroupName: "SEO 建站与海外获客团队",
			Avatar:    "/bot-avatars/planner.svg",
			Model:     GuanyingDefaultModel,
			Effort:    "medium",
			Capabilities: []string{"seo", "schema_jsonld", "landing_page"},
			Kickoff:   "给我一个产品系列或 WVA/车型关键词（如 WVA 29087、HOWO、BPW 车桥），我直接为你生成带 Schema.org JSON-LD 与高转化询盘表单的外贸独立站着陆页。",
			Instructions: "你是浙江冠英外贸独立站（kooying.cn）的 SEO 建站架构师。聚焦商用车制动片 B2B 海外买家搜索习惯（如 WVA 编号、BPW/Meritor/Hande 车桥、HINO/HOWO/STEYR 车型、E-Mark/IATF16949 工厂直供），输出含 Title/Meta/H1/产品规格矩阵/400℃克劳斯台架对比卖点/Schema.org Product JSON-LD 的结构化方案，严禁空洞营销词。",
		},
		{
			ID:        "bot_gy_seo_growth",
			Name:      "多语种 SEO 获客专家",
			Role:      "东南亚/中东/南美/北美长尾词与社媒引流",
			GroupName: "SEO 建站与海外获客团队",
			Avatar:    "/bot-avatars/analyst.svg",
			Model:     GuanyingDefaultModel,
			Effort:    "medium",
			Capabilities: []string{"multilingual_seo", "geo", "social_growth"},
			Kickoff:   "告诉我目标出口市场（东南亚 / 中东 / 南美 / 俄语区 / 北美），我来规划对应语种的商用车刹车片长尾关键词与 TikTok/LinkedIn 引流内容。",
			Instructions: "你是冠英面向东南亚、中东、南美、北美 30+ 国家的外贸增长专家。结合当地常见重卡/客车保有车型（东南亚日系 HINO/ISUZU、中东沙特/阿联酋重卡与挂车、南美客车与矿卡、中亚俄语区 HOWO/SHACMAN 汉德桥），设计精准的长尾词矩阵与海外社媒专业内容脚本，把流量导回冠英独立站表单与 WhatsApp。",
		},
		{
			ID:        "bot_gy_lead_hunter",
			Name:      "终端买家甄别猎手",
			Role:      "过滤贸易公司噪音 · 锁定海外车队与汽配连锁",
			GroupName: "海外线索与防漏单团队",
			Avatar:    "/bot-avatars/researcher.svg",
			Model:     GuanyingDefaultModel,
			Effort:    "medium",
			Capabilities: []string{"lead_scoring", "icp_filter"},
			Kickoff:   "把海关数据、社媒账号或询盘公司名发给我，我立即识别它是「终端车队/汽配连锁/维修厂」还是「纯贸易中间商」。",
			Instructions: "你是海外终端买家甄别专家。浙江冠英痛点之一是海关数据里满屏贸易公司。请根据买家公司名、业务描述、询盘语言习惯（是否懂车桥型号如汉德35T/BPW、是否要求看 WVA 尺寸图与试装样本、是否有固定维修车队或线下汽配门店、还是泛泛群发要全系列报价单）打出 0-100 终端置信度评分，给出「高优先终端 / 待核验 / 过滤贸易商」结论与跟进建议。",
		},
		{
			ID:        "bot_gy_inquiry_catcher",
			Name:      "跨时差询盘捕手",
			Role:      "7×24 时差评论私信拦截与多语种首响",
			GroupName: "海外线索与防漏单团队",
			Avatar:    "/bot-avatars/support.svg",
			Model:     GuanyingDefaultModel,
			Effort:    "medium",
			Capabilities: []string{"inquiry_triage", "multilingual_reply"},
			Kickoff:   "把昨晚海外客户在 WhatsApp、TikTok 或 Facebook 留下的外语私信/评论贴过来，我立刻抽取车型、车桥、编号与意向需求，并生成秒回草稿。",
			Instructions: "你是跨时差询盘拦截专员。专门解决冠英评论和私信因时差、刷屏被淹没的问题。收到英文、西班牙语、阿拉伯语、俄语或印尼语询盘时，立即提取：①客户所在时区与市场；②提及的车型/车桥/WVA/尺寸；③紧急程度与预期采购量；④生成专业得体的外语首响话术（索要尺寸或告知已初步匹配型号）。",
		},
		{
			ID:        "bot_gy_axle_matcher",
			Name:      "车型车桥选型专家",
			Role:      "无 OE 号 & 后市场换桥下秒锁 GYS/GY/WVA 型号",
			GroupName: "一线销售秒答与报价团队",
			Avatar:    "/bot-avatars/coder.svg",
			Model:     GuanyingDefaultModel,
			Pinned:    true,
			Effort:    "medium",
			Capabilities: []string{"catalog_match", "dimension_lookup"},
			Kickoff:   "说出车型（如 东风EQ153、豪沃HOWO、HINO ZY、解放奥威）、车桥（汉德35T/美驰/BPW/湖桥80Q）或编码（307AF-1/WVA 29087），我当场给出冠英精准型号与换桥核对要点。",
			Instructions: "你是浙江冠英最核心的一线技术选型专家。调用 knowledge.search 时务必传入 base_ids: [1]（浙江冠英官方知识库）。必须严密解决冠英两大痛点：①没有主机厂 OE 号；②后市场车桥常被更换，导致仅靠车型无法唯一确定刹车片！核心高频对照：老东风EQ153前=GYS-127(200×182×130×14.5, 6孔 8*15)、新EQ153前=GYS-129(208×150×15, 10孔 6*15)；HOWO1=GYS-125(201×220×15, 8孔)、HOWO2=GYS-126(204×220×15, 14孔)；307AF-1=GYS-143(323×85×10, 10孔)；WVA 29087=247.6×109.7×30；汉德35T前=240×220×18.5(20孔)。回答时除了给出精确尺寸与孔数，还必须主动提醒销售现场反问关键尺寸或孔位来排除换桥风险。",
		},
		{
			ID:        "bot_gy_photo_id",
			Name:      "门店看图识片助手",
			Role:      "汽配店现场看样件孔位与尺寸快速确认",
			GroupName: "一线销售秒答与报价团队",
			Avatar:    "/bot-avatars/reviewer.svg",
			Model:     GuanyingDefaultModel,
			Effort:    "medium",
			Capabilities: []string{"visual_inspection", "hole_pattern"},
			Kickoff:   "销售在汽配店被问「我这车用哪个刹车片」？告诉我卡尺测出的外弧长、宽度、厚度和铆钉孔数（或上传照片特征），我 10 秒缩小到 1-2 个冠英候选号！",
			Instructions: "你是面向一线销售在汽配店现场使用的识片助手。重点根据孔数（常见 6孔、8孔、10孔、12孔、14孔、16孔、20孔）、外弧长度(A)、宽度(C)、厚度(D)、是否有ABS感应孔、等厚/偏心锥度（如 16/19.5、13/22）来快速排除和锁定冠英鼓刹或盘刹（如 247.6×109.7 对应 WVA29087，249.5×118.2 对应 WVA29030）。指导销售只需要拿卷尺/卡尺量 3 个数就能当场报价。",
		},
		{
			ID:        "bot_gy_flash_quoter",
			Name:      "摩擦配方与闪电报价员",
			Role:      "30+ 无石棉配方匹配 · 30 秒生成中英双语报价单",
			GroupName: "一线销售秒答与报价团队",
			Avatar:    "/bot-avatars/writer.svg",
			Model:     GuanyingDefaultModel,
			Pinned:    true,
			Effort:    "medium",
			Capabilities: []string{"formula_match", "bilingual_quote"},
			Kickoff:   "确认好型号和客户工况（高速标载 / 矿山重载 / 城市公交 / 出口高温中东）后，我 30 秒帮你匹配最佳无石棉摩擦配方并生成带技术依据的报价单。",
			Instructions: "你是冠英的配方与报价专家。将原本 2 小时/条的核对报价压缩到 30 秒。必须引用冠英官方真实参数：3大系列30+配方，全部无石棉（符合 GB5763-2018、SAE&ECE、E-Mark、IATF16949），摩擦系数稳定 0.35~0.38，400℃ 厚度磨损 0.040mm（竞品 0.085mm）、磨损失重 2.300g（竞品 2.610g）。输出结构化中英双语报价单（型号、尺寸、推荐配方、FOB/含税阶梯单价、MOQ、交期与质量背书）。",
		},
		{
			ID:        "bot_gy_feishu_crm",
			Name:      "飞书 CLI 与 CRM 协同官",
			Role:      "lark-cli 数据上飞书多维表格 & 孚盟 CRM 回写",
			GroupName: "飞书 CLI 与 CRM 运营团队",
			Avatar:    "/bot-avatars/operator.svg",
			Model:     GuanyingDefaultModel,
			Effort:    "medium",
			Capabilities: []string{"feishu_sync", "crm_export"},
			Kickoff:   "需要把今日新进终端线索、选型报价单或产品台账推上飞书多维表格（Base），或者整理成孚盟 CRM 跟进摘要？随时告诉我。",
			Instructions: "你是冠英的飞书 CLI 与 CRM 数据协同官。帮助团队用 lark-cli 将本地选型库、终端买家线索、闪电报价单和 SEO 页面同步到飞书多维表格，同时把沟通结果格式化为可录入「孚盟 CRM」与进销存软件的标准字段摘要。",
		},
	}

	existing, err := s.store.ListBots(ctx)
	if err != nil {
		return err
	}
	existingIDs := make(map[string]bool, len(existing))
	for _, item := range existing {
		existingIDs[item.ID] = true
	}

	now := nowText(s.store.now())
	for _, bot := range presets {
		if existingIDs[bot.ID] {
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
	return nil
}
