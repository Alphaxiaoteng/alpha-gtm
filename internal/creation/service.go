// Package creation implements the bounded, local-first content-creation
// pipeline. It has no browser, publishing or filesystem capability.
package creation

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/knowledge"
)

var reviewDecisionPattern = regexp.MustCompile(`(?im)^\s*(?:最终\s*)?(?:审核\s*)?结论\s*[:：-]?\s*(pass|revise)\b|^\s*(pass|revise)\b`)

// AgentHub rejects text tasks beyond 16 KiB. Reserve a small safety margin
// for provider framing while retaining valid UTF-8 at the truncation edge.
const maxExternalPromptBytes = 15_000
const truncatedPromptNotice = "\n\n[上下文已按本次任务上限截断]"

type creationStore interface {
	GetCreationSession(string) (*db.CreationSession, error)
	GetCreationModelRole(string) (*db.CreationModelRole, error)
	AppendCreationRun(db.CreationRunInput) (*db.CreationRun, error)
}

type Retriever interface {
	Search(context.Context, []int64, string) ([]db.KnowledgeSearchResult, error)
}

type ModelSelection struct {
	ProviderID string
	ModelID    string
}

type TextRunner interface {
	Verify(context.Context, ModelSelection) error
	Complete(context.Context, ModelSelection, string, string) (string, error)
}

type Service struct {
	store     creationStore
	retriever Retriever
	runner    TextRunner
}

func NewService(store creationStore, retriever Retriever, runner TextRunner) *Service {
	return &Service{store: store, retriever: retriever, runner: runner}
}

type RunRequest struct {
	SessionID     string
	Action        string
	Input         string
	AllowExternal bool
}

type QualityReport struct {
	Decision string   `json:"decision"`
	Issues   []string `json:"issues"`
	Source   string   `json:"source"`
}

type RunResult struct {
	RunID        string               `json:"run_id"`
	Source       string               `json:"source"`
	ProviderID   string               `json:"provider_id,omitempty"`
	ModelID      string               `json:"model_id,omitempty"`
	UsedFallback bool                 `json:"used_fallback"`
	Result       string               `json:"result"`
	Citations    []knowledge.Citation `json:"citations"`
	Quality      QualityReport        `json:"quality"`
}

func (s *Service) Run(ctx context.Context, request RunRequest) (*RunResult, error) {
	if s == nil || s.store == nil || s.retriever == nil || s.runner == nil {
		return nil, fmt.Errorf("creation service unavailable")
	}
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.Action = strings.TrimSpace(request.Action)
	request.Input = strings.TrimSpace(request.Input)
	if request.SessionID == "" || !validAction(request.Action) {
		return nil, fmt.Errorf("invalid creation run")
	}
	session, err := s.store.GetCreationSession(request.SessionID)
	if err != nil {
		return nil, err
	}
	query := strings.Join([]string{session.Title, session.TopicBrief, session.Style, request.Input}, "\n")
	results, err := s.retriever.Search(ctx, append([]int64(nil), session.KnowledgeBaseIDs...), query)
	if err != nil {
		return nil, err
	}
	contextText, retrievedCitations := knowledge.BuildCitedContext(results, 12000)
	if !request.AllowExternal {
		return s.persistLocal(session, request, contextText, retrievedCitations)
	}
	role := "copywriter"
	if request.Action == "review" {
		role = "reviewer"
	}
	selection, usedFallback, err := s.resolveRole(role)
	if err != nil {
		return nil, err
	}
	if err := s.runner.Verify(ctx, selection); err != nil {
		return nil, fmt.Errorf("creation model unavailable: %w", err)
	}
	prompt := externalPrompt(session, request, contextText)
	result, err := s.runner.Complete(ctx, selection, "Alpha Nexus 创作", prompt)
	if err != nil {
		return nil, err
	}
	result = strings.TrimSpace(result)
	if result == "" {
		return nil, fmt.Errorf("creation model returned an empty result")
	}
	quality := deterministicQuality(session.Title, result, retrievedCitations)
	if request.Action == "review" {
		quality = reviewQuality(result, quality.Issues, "configured_model")
	} else if request.Action == "draft" {
		reviewSelection, reviewFallback, reviewErr := s.resolveRole("reviewer")
		if reviewErr != nil {
			return nil, reviewErr
		}
		if err := s.runner.Verify(ctx, reviewSelection); err != nil {
			return nil, fmt.Errorf("creation review model unavailable: %w", err)
		}
		reviewResult, err := s.runner.Complete(ctx, reviewSelection, "Alpha Nexus 文案审校", reviewPrompt(session, result, contextText))
		if err != nil {
			return nil, err
		}
		quality = reviewQuality(reviewResult, quality.Issues, "configured_model")
		if reviewFallback {
			quality.Issues = append(quality.Issues, "审校模型未单独配置，已使用文案模型审校。")
		}
	}
	usedCitations := knowledge.CitationsUsedInOutput(result, retrievedCitations)
	return s.persist(session, request.Action, selection, usedFallback, "configured_model", result, usedCitations, quality)
}

func (s *Service) persistLocal(session *db.CreationSession, request RunRequest, contextText string, retrieved []knowledge.Citation) (*RunResult, error) {
	result := localResult(session, request, contextText)
	quality := deterministicQuality(session.Title, result, retrieved)
	quality.Decision = "revise"
	quality.Source = "local"
	quality.Issues = append(quality.Issues, "本地结构草案未经过模型审校；请配置并明确授权模型后再审核。")
	return s.persist(session, request.Action, ModelSelection{}, false, "local", result, knowledge.CitationsUsedInOutput(result, retrieved), quality)
}

func (s *Service) persist(session *db.CreationSession, action string, selection ModelSelection, usedFallback bool, source, result string, citations []knowledge.Citation, quality QualityReport) (*RunResult, error) {
	citationsJSON, err := json.Marshal(citations)
	if err != nil {
		return nil, err
	}
	qualityJSON, err := json.Marshal(quality)
	if err != nil {
		return nil, err
	}
	run, err := s.store.AppendCreationRun(db.CreationRunInput{
		SessionID: session.ID, Action: action, ProviderID: selection.ProviderID, ModelID: selection.ModelID,
		UsedFallback: usedFallback, PromptSummary: creationPromptSummary(session, action), Result: result,
		CitationsJSON: string(citationsJSON), QualityJSON: string(qualityJSON),
	})
	if err != nil {
		return nil, err
	}
	return &RunResult{RunID: run.ID, Source: source, ProviderID: selection.ProviderID, ModelID: selection.ModelID, UsedFallback: usedFallback, Result: result, Citations: citations, Quality: quality}, nil
}

func (s *Service) resolveRole(role string) (ModelSelection, bool, error) {
	chain := []string{role}
	switch role {
	case "reviewer":
		chain = append(chain, "copywriter", "conversation")
	case "copywriter":
		chain = append(chain, "conversation")
	}
	for index, candidate := range chain {
		configured, err := s.store.GetCreationModelRole(candidate)
		if err != nil || configured == nil || strings.TrimSpace(configured.ProviderID) == "" || strings.TrimSpace(configured.ModelID) == "" {
			continue
		}
		return ModelSelection{ProviderID: configured.ProviderID, ModelID: configured.ModelID}, index > 0, nil
	}
	return ModelSelection{}, false, fmt.Errorf("no verified %s model is configured", role)
}

func externalPrompt(session *db.CreationSession, request RunRequest, contextText string) string {
	instruction := map[string]string{
		"topic":  "基于选题与证据提出三个差异化内容角度；每个写清受众、冲突和承诺。",
		"draft":  "生成中文社媒文案初稿。只使用资料中能支持的事实；使用资料时保留 [KB:id] 标识；不要声称发布已经发生。",
		"review": "审核下方稿件，第一行必须写“结论：pass”或“结论：revise”，然后列出具体问题、原句和修正方向。",
		"revise": "按给出的审核问题改写稿件；只保留有资料支持的事实，并保留实际使用的 [KB:id] 标识。",
	}[request.Action]
	return truncateUTF8Bytes(strings.TrimSpace(fmt.Sprintf("%s\n\n选题：%s\n风格：%s\n平台：%s\n\n用户输入：\n%s\n\n知识资料（引用资料不等于独立事实证明）：\n%s", instruction, session.Title, session.Style, session.Platform, request.Input, contextText)), maxExternalPromptBytes)
}

func reviewPrompt(session *db.CreationSession, draft, contextText string) string {
	return truncateUTF8Bytes(strings.TrimSpace(fmt.Sprintf("审核下方中文社媒文案。第一行必须写“结论：pass”或“结论：revise”；检查事实依据、标题正文一致性、夸张承诺、版权、隐私和平台适配。\n\n选题：%s\n平台：%s\n\n稿件：\n%s\n\n资料：\n%s", session.Title, session.Platform, draft, contextText)), maxExternalPromptBytes)
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	cut := limit - len(truncatedPromptNotice)
	if cut <= 0 {
		return ""
	}
	for cut > 0 && (value[cut]&0xC0) == 0x80 {
		cut--
	}
	return strings.TrimSpace(value[:cut]) + truncatedPromptNotice
}

func localResult(session *db.CreationSession, request RunRequest, contextText string) string {
	switch request.Action {
	case "topic":
		result := "本地选题草案：\n1. " + session.Title + "——从真实问题切入，给出可执行步骤。\n2. 常见误区复盘——用资料中的判断标准解释原因。\n3. 案例拆解——区分事实、推断与个人经验。"
		if contextText != "" {
			result += "\n\n## 参考资料\n" + contextText
		}
		return result
	case "review":
		return "结论：revise\n本地检查不能证明事实真实性；请逐条核验资料、案例、版权与平台规则。"
	default:
		result := "# 本地文案草案\n\n## 开场\n提出“" + session.Title + "”的具体冲突。\n\n## 正文\n给出步骤、边界和人工核验提示。\n\n## 结尾\n提供一个可执行的下一步。"
		if contextText != "" {
			result += "\n\n## 参考资料\n" + contextText
		}
		return result
	}
}

func deterministicQuality(title, output string, citations []knowledge.Citation) QualityReport {
	issues := make([]string, 0, 3)
	if strings.TrimSpace(title) == "" {
		issues = append(issues, "缺少选题标题。")
	}
	if len([]rune(strings.TrimSpace(output))) < 80 {
		issues = append(issues, "内容过短，无法完成有效审核。")
	}
	if len(citations) > 0 && len(knowledge.CitationsUsedInOutput(output, citations)) == 0 {
		issues = append(issues, "检索到了资料，但稿件没有标注实际使用的来源。")
	}
	decision := "pass"
	if len(issues) > 0 {
		decision = "revise"
	}
	return QualityReport{Decision: decision, Issues: issues, Source: "deterministic"}
}

func reviewQuality(result string, inherited []string, source string) QualityReport {
	quality := QualityReport{Decision: "unknown", Issues: append([]string(nil), inherited...), Source: source}
	matches := reviewDecisionPattern.FindAllStringSubmatch(result, -1)
	for _, match := range matches {
		decision := match[1]
		if decision == "" && len(match) > 2 {
			decision = match[2]
		}
		if decision == "pass" || decision == "revise" {
			quality.Decision = decision
			break
		}
	}
	if quality.Decision == "unknown" {
		quality.Issues = append(quality.Issues, "审校结果没有可识别的 pass/revise 结论。")
	}
	return quality
}

func creationPromptSummary(session *db.CreationSession, action string) string {
	return fmt.Sprintf("%s：%s", action, session.Title)
}

func validAction(action string) bool {
	switch action {
	case "topic", "draft", "review", "revise":
		return true
	default:
		return false
	}
}
