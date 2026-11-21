package ui

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"alpha-nexus-egolite/internal/agenthub"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/knowledge"
)

type creationWorkflowBody struct {
	Title           string            `json:"title"`
	Platform        string            `json:"platform"`
	Style           string            `json:"style"`
	Stage           string            `json:"stage"`
	KnowledgeScopes []knowledge.Scope `json:"knowledge_scopes"`
}

type creationWorkflowPatchBody struct {
	Title             *string           `json:"title"`
	Platform          *string           `json:"platform"`
	Style             *string           `json:"style"`
	Stage             *string           `json:"stage"`
	KnowledgeScopes   []knowledge.Scope `json:"knowledge_scopes"`
	AcceptedArtifacts json.RawMessage   `json:"accepted_artifacts"`
}

type creationWorkflowAdvanceBody struct {
	FromStage string `json:"from_stage"`
	Artifact  string `json:"artifact"`
}

type creationWorkflowRunBody struct {
	Message         string            `json:"message"`
	Stage           string            `json:"stage"`
	KnowledgeScopes []knowledge.Scope `json:"knowledge_scopes"`
	TrendContext    string            `json:"trend_context"`
}

type creationWorkflowView struct {
	Workflow     *db.CreationWorkflow   `json:"workflow"`
	Conversation *agenthub.Conversation `json:"conversation"`
}

func (s *Server) handleAPICreationWorkflows(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.db.ListCreationWorkflows(r.Context())
		if err != nil {
			writeCreationWorkflowError(w, err)
			return
		}
		views := make([]creationWorkflowView, 0, len(items))
		for index := range items {
			conversation, err := s.agentHub.GetConversation(r.Context(), items[index].ConversationID)
			if errors.Is(err, agenthub.ErrConversationNotFound) {
				continue
			}
			if err != nil {
				writeCreationWorkflowError(w, err)
				return
			}
			views = append(views, creationWorkflowView{Workflow: &items[index], Conversation: conversation})
		}
		writeJSON(w, map[string]any{"items": views})
	case http.MethodPost:
		var body creationWorkflowBody
		if !decodeCreationJSON(w, r, &body) {
			return
		}
		if s.agentHub == nil {
			writeCreationWorkflowError(w, fmt.Errorf("agent hub unavailable"))
			return
		}
		selection, err := s.agentHub.TextTaskSelection(r.Context())
		if err != nil {
			writeCreationWorkflowError(w, err)
			return
		}
		stage := strings.TrimSpace(body.Stage)
		if stage == "" {
			stage = "topic"
		}
		if !validCreationWorkflowStageUI(stage) {
			writeCreationWorkflowError(w, fmt.Errorf("invalid creation workflow stage"))
			return
		}
		scopes, err := s.validateCreationWorkflowScopes(r, body.KnowledgeScopes)
		if err != nil {
			writeCreationWorkflowError(w, err)
			return
		}
		title := strings.TrimSpace(body.Title)
		if title == "" {
			title = "创作访谈"
		}
		conversation, err := s.agentHub.CreateConversation(r.Context(), selection.ProviderID, title, selection.ModelID, agenthub.ContextSnapshot{
			Workspace: "Alpha-Nexus", Page: "创作工作台",
		})
		if err != nil {
			writeCreationWorkflowError(w, err)
			return
		}
		scopeJSON, _ := json.Marshal(scopes)
		workflow, err := s.db.CreateCreationWorkflow(r.Context(), db.CreationWorkflowInput{
			ID: newCreationWorkflowID(), ConversationID: conversation.ID, Title: title,
			Platform: strings.TrimSpace(body.Platform), Style: strings.TrimSpace(body.Style), Stage: stage,
			KnowledgeScopes: scopeJSON,
		})
		if err != nil {
			writeCreationWorkflowError(w, err)
			return
		}
		selection.ConversationID = conversation.ID
		_, _ = s.agentHub.SaveProviderSelection(r.Context(), *selection)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, creationWorkflowView{Workflow: workflow, Conversation: conversation})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPICreationWorkflow(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	workflow, err := s.db.GetCreationWorkflow(r.Context(), r.PathValue("id"))
	if err != nil {
		writeCreationWorkflowError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		conversation, err := s.agentHub.GetConversation(r.Context(), workflow.ConversationID)
		if err != nil {
			writeCreationWorkflowError(w, err)
			return
		}
		writeJSON(w, creationWorkflowView{Workflow: workflow, Conversation: conversation})
	case http.MethodPatch:
		var body creationWorkflowPatchBody
		if !decodeCreationJSON(w, r, &body) {
			return
		}
		patch := db.CreationWorkflowPatch{Title: body.Title, Platform: body.Platform, Style: body.Style, Stage: body.Stage, AcceptedArtifacts: body.AcceptedArtifacts}
		if body.KnowledgeScopes != nil {
			scopes, scopeErr := s.validateCreationWorkflowScopes(r, body.KnowledgeScopes)
			if scopeErr != nil {
				writeCreationWorkflowError(w, scopeErr)
				return
			}
			patch.KnowledgeScopes, _ = json.Marshal(scopes)
		}
		updated, err := s.db.UpdateCreationWorkflow(r.Context(), workflow.ID, patch)
		if err != nil {
			writeCreationWorkflowError(w, err)
			return
		}
		conversation, err := s.agentHub.GetConversation(r.Context(), updated.ConversationID)
		if err != nil {
			writeCreationWorkflowError(w, err)
			return
		}
		if body.Title != nil {
			conversation, err = s.agentHub.UpdateConversation(r.Context(), conversation.ID, body.Title, nil)
			if err != nil {
				writeCreationWorkflowError(w, err)
				return
			}
		}
		writeJSON(w, creationWorkflowView{Workflow: updated, Conversation: conversation})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPICreationWorkflowAdvance(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body creationWorkflowAdvanceBody
	if !decodeCreationJSON(w, r, &body) {
		return
	}
	workflow, err := s.db.GetCreationWorkflow(r.Context(), r.PathValue("id"))
	if err != nil {
		writeCreationWorkflowError(w, err)
		return
	}
	fromStage := strings.TrimSpace(body.FromStage)
	if fromStage == "" {
		fromStage = workflow.Stage
	}
	if fromStage != workflow.Stage || strings.TrimSpace(body.Artifact) == "" || len([]rune(body.Artifact)) > 64000 {
		writeCreationWorkflowError(w, fmt.Errorf("workflow stage cannot advance"))
		return
	}
	nextStage, ok := nextCreationWorkflowStage(fromStage)
	if !ok {
		writeCreationWorkflowError(w, fmt.Errorf("workflow is already at the final stage"))
		return
	}
	artifacts := map[string]string{}
	if len(workflow.AcceptedArtifacts) > 0 && json.Unmarshal(workflow.AcceptedArtifacts, &artifacts) != nil {
		writeCreationWorkflowError(w, fmt.Errorf("invalid accepted artifacts"))
		return
	}
	artifacts[fromStage] = strings.TrimSpace(body.Artifact)
	encoded, _ := json.Marshal(artifacts)
	updated, err := s.db.UpdateCreationWorkflow(r.Context(), workflow.ID, db.CreationWorkflowPatch{Stage: &nextStage, AcceptedArtifacts: encoded})
	if err != nil {
		writeCreationWorkflowError(w, err)
		return
	}
	conversation, err := s.agentHub.GetConversation(r.Context(), updated.ConversationID)
	if err != nil {
		writeCreationWorkflowError(w, err)
		return
	}
	writeJSON(w, creationWorkflowView{Workflow: updated, Conversation: conversation})
}

func (s *Server) handleAPICreationWorkflowRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body creationWorkflowRunBody
	if !decodeCreationJSON(w, r, &body) {
		return
	}
	body.Message = strings.TrimSpace(body.Message)
	if body.Message == "" || len([]rune(body.Message)) > 12000 {
		writeCreationWorkflowError(w, fmt.Errorf("invalid creation message"))
		return
	}
	workflow, err := s.db.GetCreationWorkflow(r.Context(), r.PathValue("id"))
	if err != nil {
		writeCreationWorkflowError(w, err)
		return
	}
	if body.Stage != "" && body.Stage != workflow.Stage {
		writeCreationWorkflowError(w, fmt.Errorf("workflow stage changed"))
		return
	}
	scopes := body.KnowledgeScopes
	if scopes == nil {
		_ = json.Unmarshal(workflow.KnowledgeScopes, &scopes)
	}
	normalizedScopes, err := s.validateCreationWorkflowScopes(r, scopes)
	if err != nil {
		writeCreationWorkflowError(w, err)
		return
	}
	if body.KnowledgeScopes != nil {
		scopeJSON, _ := json.Marshal(normalizedScopes)
		workflow, err = s.db.UpdateCreationWorkflow(r.Context(), workflow.ID, db.CreationWorkflowPatch{KnowledgeScopes: scopeJSON})
		if err != nil {
			writeCreationWorkflowError(w, err)
			return
		}
	}
	conversation, err := s.agentHub.GetConversation(r.Context(), workflow.ConversationID)
	if err != nil {
		writeCreationWorkflowError(w, err)
		return
	}
	promptContext := creationWorkflowPrompt(workflow, normalizedScopes, body.TrendContext)
	if workflow.Stage == "script" {
		if s.skillLibrary == nil {
			writeCreationWorkflowError(w, fmt.Errorf("script workflow skill unavailable"))
			return
		}
		skill, skillErr := s.skillLibrary.Get(r.Context(), "social-content-workflow")
		if skillErr != nil || !skill.Enabled {
			writeCreationWorkflowError(w, fmt.Errorf("script workflow skill unavailable"))
			return
		}
		promptContext += "\n\n<internal-skill id=\"social-content-workflow\">\n" + skill.Instructions + "\n</internal-skill>"
	}
	var citations []knowledge.Citation
	if len(normalizedScopes) > 0 {
		service := s.knowledgeService
		if service == nil {
			service = knowledge.NewService(s.db)
		}
		results, searchErr := service.Search(r.Context(), knowledge.SearchRequest{Scopes: normalizedScopes, Query: body.Message, TopK: 6})
		if searchErr != nil {
			writeCreationWorkflowError(w, fmt.Errorf("knowledge search failed"))
			return
		}
		evidence, evidenceCitations := knowledge.BuildCitedContext(results, 12000)
		citations = evidenceCitations
		if evidence != "" {
			promptContext += "\n\n以下是用户本轮明确附加资料中的证据。只把带 [KB:id] 的内容当作资料事实；没有证据的内容标为创作建议。\n\n" + evidence
		}
	}
	run, err := s.agentHub.StartRunWithPromptContext(r.Context(), conversation.ID, conversation.Provider, conversation.ModelID, body.Message, conversation.Context, promptContext)
	if err != nil {
		writeAgentHubError(w, err)
		return
	}
	if len(normalizedScopes) > 0 {
		scopeJSON, _ := json.Marshal(normalizedScopes)
		citationJSON, _ := json.Marshal(citations)
		_ = s.db.CreateAgentKnowledgeTurn(r.Context(), run.ConversationID, run.ID, body.Message, scopeJSON, citationJSON)
	}
	writeJSON(w, map[string]any{"workflow": workflow, "run": run, "citations": citations})
}

func (s *Server) validateCreationWorkflowScopes(r *http.Request, scopes []knowledge.Scope) ([]knowledge.Scope, error) {
	if len(scopes) == 0 {
		return []knowledge.Scope{}, nil
	}
	service := s.knowledgeService
	if service == nil {
		service = knowledge.NewService(s.db)
	}
	return service.ValidateScopes(r.Context(), scopes)
}

func creationWorkflowPrompt(workflow *db.CreationWorkflow, scopes []knowledge.Scope, trend string) string {
	accepted := "{}"
	if len(workflow.AcceptedArtifacts) > 0 {
		accepted = string(workflow.AcceptedArtifacts)
	}
	parts := []string{
		"你是 Alpha Agent 的创作访谈助手。当前工作台只处理一个连续创作会话。",
		"当前阶段：" + creationWorkflowStageLabel(workflow.Stage) + "。每轮最多追问一个最关键的问题；信息足够时给出可编辑草稿，并等待用户确认，不要自动推进阶段。",
		"阶段完成后，用户会点击采用并继续；不要代替用户确认，也不要触发采集、浏览器操作或发布。",
		"已采用的阶段产物（仅作为本会话上下文）：" + accepted,
	}
	if len(scopes) == 0 {
		parts = append(parts, "本轮没有附加资料。没有证据的表达必须标为创作建议。")
	}
	if strings.TrimSpace(trend) != "" {
		parts = append(parts, "用户明确附加的趋势参考：\n"+strings.TrimSpace(trend))
	}
	return strings.Join(parts, "\n\n")
}

func creationWorkflowStageLabel(stage string) string {
	switch stage {
	case "topic":
		return "选题"
	case "copy":
		return "文案"
	case "script":
		return "脚本"
	default:
		return "审校与入库"
	}
}

func validCreationWorkflowStageUI(stage string) bool {
	return stage == "topic" || stage == "copy" || stage == "script" || stage == "review"
}

func nextCreationWorkflowStage(stage string) (string, bool) {
	switch stage {
	case "topic":
		return "copy", true
	case "copy":
		return "script", true
	case "script":
		return "review", true
	default:
		return "", false
	}
}

func newCreationWorkflowID() string {
	var buffer [18]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return "creation-workflow-fallback"
	}
	return "cw_" + base64.RawURLEncoding.EncodeToString(buffer[:])
}

func writeCreationWorkflowError(w http.ResponseWriter, err error) {
	if errors.Is(err, agenthub.ErrModelNotVerified) || strings.Contains(err.Error(), "model unavailable") || strings.Contains(err.Error(), "no verified") {
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]string{"error": "CREATION_MODEL_UNAVAILABLE"})
		return
	}
	writeCreationError(w, err)
}
