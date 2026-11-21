package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/knowledge"
	"alpha-nexus-egolite/internal/platforms"
)

const maxContentAssistBytes = 96 << 10

var (
	contentReviewDecisionLine = regexp.MustCompile(`(?im)^\s*(?:(?:本地|最终)\s*)?(?:审核\s*)?结论\s*[:：-]?\s*(pass|revise)\b|^\s*(pass|revise)\b`)
	contentReviewPassToken    = regexp.MustCompile(`(?i)\bpass\b`)
	contentReviewReviseToken  = regexp.MustCompile(`(?i)\brevise\b`)
)

type contentAssistRequest struct {
	Action           string  `json:"action"`
	Title            string  `json:"title"`
	Caption          string  `json:"caption"`
	Body             string  `json:"body"`
	Platform         string  `json:"platform"`
	AllowExternal    bool    `json:"allow_external"`
	UseKnowledge     bool    `json:"use_knowledge"`
	KnowledgeBaseIDs []int64 `json:"knowledge_base_ids"`
	KnowledgeQuery   string  `json:"knowledge_query"`
}

// handleAPIContentAssist offers explicit, non-knowledge-base writing support.
// A request stays local unless the caller sets allow_external=true for that
// exact operation; this prevents a draft from silently reaching a configured
// paid or remote model provider.
func (s *Server) handleAPIContentAssist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxContentAssistBytes)
	defer r.Body.Close()
	var req contentAssistRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid content assist body", http.StatusBadRequest)
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	req.Platform = strings.TrimSpace(req.Platform)
	if !validContentAssistAction(req.Action) {
		http.Error(w, "unsupported assist action", http.StatusBadRequest)
		return
	}
	if req.Action == "platform_adapt" {
		if req.Platform == "" {
			http.Error(w, "platform is required for platform_adapt", http.StatusBadRequest)
			return
		}
		if _, err := platforms.Find(req.Platform); err != nil {
			http.Error(w, "unsupported platform", http.StatusBadRequest)
			return
		}
	}
	if strings.TrimSpace(req.Title+req.Caption+req.Body) == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	if req.UseKnowledge && len(req.KnowledgeBaseIDs) == 0 {
		http.Error(w, "knowledge base is required", http.StatusBadRequest)
		return
	}
	var contextText string
	var citations []knowledge.Citation
	var knowledgeResults []db.KnowledgeSearchResult
	if req.UseKnowledge {
		topK, maxContextChars := 6, 12000
		if len(req.KnowledgeBaseIDs) == 1 {
			base, err := s.db.GetKnowledgeBase(req.KnowledgeBaseIDs[0])
			if err != nil {
				http.Error(w, "knowledge base not found", http.StatusBadRequest)
				return
			}
			topK, maxContextChars = base.TopK, base.MaxContextChars
		}
		query := strings.TrimSpace(req.KnowledgeQuery)
		if query == "" {
			query = strings.TrimSpace(req.Title + " " + req.Caption + " " + req.Body)
		}
		service := s.knowledgeService
		if service == nil {
			service = knowledge.NewService(s.db)
		}
		results, err := service.Search(r.Context(), knowledge.SearchRequest{
			BaseIDs: req.KnowledgeBaseIDs, Query: query, TopK: topK,
		})
		if err != nil {
			http.Error(w, "knowledge search failed", http.StatusBadRequest)
			return
		}
		knowledgeResults = results
		contextText, citations = knowledge.BuildCitedContext(results, maxContextChars)
	}
	if !req.AllowExternal {
		result := localContentAssist(req, localKnowledgeNotes(knowledgeResults, 3))
		response := map[string]any{"result": result, "source": "local", "external": false, "citations": knowledge.CitationsUsedInOutput(result, citations)}
		if req.Action == "check" {
			response["review_decision"] = contentReviewDecision(result)
		}
		writeJSON(w, response)
		return
	}
	if s.completeAI == nil {
		http.Error(w, "content assist is unavailable", http.StatusServiceUnavailable)
		return
	}
	prompt := contentAssistPrompt(req)
	if contextText != "" {
		prompt += "\n\nLocal knowledge excerpts (cite [KB:id] when used; treat as references, not verified facts):\n" + contextText
	}
	result, err := s.completeAI(r.Context(), contentAssistSystem(req.Action), prompt)
	if err != nil {
		http.Error(w, "configured model request failed", http.StatusBadGateway)
		return
	}
	result = strings.TrimSpace(result)
	response := map[string]any{"result": result, "source": "configured_model", "external": true, "citations": knowledge.CitationsUsedInOutput(result, citations)}
	if req.Action == "check" {
		response["review_decision"] = contentReviewDecision(result)
	}
	writeJSON(w, response)
}

func contentReviewDecision(result string) string {
	if contentReviewPassToken.MatchString(result) && contentReviewReviseToken.MatchString(result) {
		return "unknown"
	}
	matches := contentReviewDecisionLine.FindAllStringSubmatch(result, -1)
	decision := ""
	for _, match := range matches {
		candidate := ""
		if len(match) > 1 && match[1] != "" {
			candidate = strings.ToLower(match[1])
		} else if len(match) > 2 {
			candidate = strings.ToLower(match[2])
		}
		if candidate == "" {
			continue
		}
		if decision != "" && decision != candidate {
			return "unknown"
		}
		decision = candidate
	}
	if decision == "pass" || decision == "revise" {
		return decision
	}
	return "unknown"
}

func validContentAssistAction(action string) bool {
	switch action {
	case "title", "summary", "rewrite", "check", "platform_adapt", "topic", "script", "storyboard", "metrics_review":
		return true
	default:
		return false
	}
}

func contentAssistSystem(action string) string {
	switch action {
	case "topic":
		return "你是中文内容选题策划 Agent。基于提供的趋势证据和本地知识，给出 5 个差异化选题；每个包含标题、受众、核心冲突、内容承诺和来源依据。不要编造实时数据，不要声称已经发布。"
	case "script":
		return "你是中文短视频编导 Agent。输出可直接审阅的完整脚本，包含前三秒钩子、镜头/旁白推进、证据或案例、转折和结尾行动；遵循用户风格，不照抄来源。不要声称已经发布。"
	case "storyboard":
		return "你是中文视频分镜 Agent。把脚本拆成 Markdown 表格，列为镜号、时长、画面、景别/运动、旁白/字幕、所需素材；只规划素材，不生成图片或视频，不执行剪辑。"
	case "rewrite":
		return "你是中文内容改写 Agent。只借鉴结构和已引用事实，按用户指定风格重新组织表达；不得照抄来源，不得把未验证资料写成亲身经历或确定事实。输出改写稿，并保留实际使用的 [KB:id] 引用。"
	case "check":
		return "你是发布前内容审核 Agent。逐项检查事实依据、标题与正文一致性、夸张承诺、版权、隐私和平台适配。输出结论 pass 或 revise；若为 revise，列出具体问题、对应原句和所需证据。不得执行发布。"
	case "metrics_review":
		return "你是中文内容运营复盘 Agent。只分析提供的已验证指标和作品数据；不得把零值解释为真实零，不得补全缺失平台，不得声称执行过采集或发布。明确区分数据证据、推断、局限和下一步人工动作。"
	}
	return "You are a concise Chinese content editor. Return only the requested result. Do not claim platform publishing happened. Task: " + action
}

func contentAssistPrompt(req contentAssistRequest) string {
	return fmt.Sprintf("Title: %s\nCaption: %s\nBody:\n%s\nPlatform: %s", req.Title, req.Caption, req.Body, req.Platform)
}

func localContentAssist(req contentAssistRequest, knowledgeNotes string) string {
	text := strings.TrimSpace(req.Body)
	if text == "" {
		text = strings.TrimSpace(req.Caption)
	}
	if len([]rune(text)) > 120 {
		text = string([]rune(text)[:120]) + "…"
	}
	switch req.Action {
	case "topic":
		return withLocalKnowledge("本地选题草案：\n1. "+strings.TrimSpace(req.Title)+"——从真实问题、反常识结论和可执行步骤展开。\n2. 同一主题的案例拆解——说明前因、过程与结果。\n3. 常见误区复盘——给出判断标准和改进清单。", knowledgeNotes)
	case "script":
		return withLocalKnowledge("# 本地脚本草案\n\n## 前三秒钩子\n提出与“"+strings.TrimSpace(req.Title)+"”有关的具体冲突。\n\n## 正文推进\n"+text+"\n\n## 结尾\n总结一个可执行动作，并提示核验事实与引用。", knowledgeNotes)
	case "storyboard":
		return withLocalKnowledge("| 镜号 | 时长 | 画面 | 景别/运动 | 旁白/字幕 | 所需素材 |\n|---|---:|---|---|---|---|\n| 1 | 0–3s | 呈现核心冲突 | 近景/快速切入 | 提出问题 | 真实场景素材 |\n| 2 | 3–15s | 展开解决步骤 | 中景/稳定 | "+text+" | 示例或屏幕录制 |\n| 3 | 15–25s | 总结行动 | 近景/定镜 | 给出下一步 | 结尾字幕 |", knowledgeNotes)
	case "title":
		if req.Title != "" {
			return "建议标题：" + req.Title
		}
		return "建议标题：" + text
	case "summary":
		return "本地摘要：" + text
	case "check":
		return withLocalKnowledge("本地审核结论：revise\n- 自动本地检查不能证明事实真实性；请逐条核验数据、案例、身份和结果。\n- 请确认标题与正文一致、素材版权、隐私和平台规则。\n- 当前正文约 "+fmt.Sprintf("%d", len([]rune(req.Body)))+" 字；完成核验后仍只保存为草稿。", knowledgeNotes)
	case "metrics_review":
		return "本地确定性复盘仍在页面展示；模型复盘必须由用户明确点击并设置 allow_external=true。"
	case "platform_adapt":
		return withLocalKnowledge("本地平台适配草案（"+strings.TrimSpace(req.Platform)+"）："+text, knowledgeNotes)
	default:
		return withLocalKnowledge("本地改写草案："+text, knowledgeNotes)
	}
}

func localKnowledgeNotes(results []db.KnowledgeSearchResult, limit int) string {
	if limit <= 0 || limit > 3 {
		limit = 3
	}
	lines := make([]string, 0, limit)
	for _, result := range results {
		content := strings.Join(strings.Fields(result.Content), " ")
		runes := []rune(content)
		if len(runes) > 180 {
			content = string(runes[:180]) + "…"
		}
		if content == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- [KB:%d] %s：%s", result.ChunkID, result.SourceName, content))
		if len(lines) == limit {
			break
		}
	}
	return strings.Join(lines, "\n")
}

func withLocalKnowledge(result, notes string) string {
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return result
	}
	return result + "\n\n## 本地知识依据（资料引用，不等于事实已核验）\n" + notes
}
