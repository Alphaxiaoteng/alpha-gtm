package ui

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"alpha-nexus-egolite/internal/agenthub"
	"alpha-nexus-egolite/internal/creation"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/knowledge"
)

const maxCreationRequestBytes = 96 << 10

type creationRoleBody struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
}

type creationSessionBody struct {
	Title            string  `json:"title"`
	TopicBrief       string  `json:"topic_brief"`
	Style            string  `json:"style"`
	Platform         string  `json:"platform"`
	SOPStage         string  `json:"sop_stage"`
	KnowledgeBaseIDs []int64 `json:"knowledge_base_ids"`
}

type creationRunBody struct {
	Action        string `json:"action"`
	Input         string `json:"input"`
	AllowExternal bool   `json:"allow_external"`
}

type creationKnowledgeRetriever struct {
	service *knowledge.Service
}

func (retriever creationKnowledgeRetriever) Search(ctx context.Context, baseIDs []int64, query string) ([]db.KnowledgeSearchResult, error) {
	if len(baseIDs) == 0 {
		return []db.KnowledgeSearchResult{}, nil
	}
	return retriever.service.Search(ctx, knowledge.SearchRequest{BaseIDs: baseIDs, Query: query, TopK: 6})
}

type creationAgentRunner struct {
	hub *agenthub.Service
}

func (runner creationAgentRunner) Verify(ctx context.Context, selection creation.ModelSelection) error {
	if runner.hub == nil {
		return fmt.Errorf("agent hub unavailable")
	}
	return runner.hub.ValidateTextTaskSelection(ctx, agenthub.ProviderSelection{
		ProviderID: agenthub.ProviderID(selection.ProviderID), ModelID: selection.ModelID,
	})
}

func (runner creationAgentRunner) Complete(ctx context.Context, selection creation.ModelSelection, title, prompt string) (string, error) {
	if runner.hub == nil {
		return "", fmt.Errorf("agent hub unavailable")
	}
	result, err := runner.hub.CompleteTextTask(ctx, agenthub.ProviderSelection{
		ProviderID: agenthub.ProviderID(selection.ProviderID), ModelID: selection.ModelID,
	}, title, prompt, agenthub.ContextSnapshot{Workspace: "creation-workbench", Page: "knowledge-assisted-copywriting"})
	if err != nil {
		return "", err
	}
	return result.Answer, nil
}

func (s *Server) handleAPICreationModelRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.db.ListCreationModelRoles()
	if err != nil {
		writeCreationError(w, err)
		return
	}
	items := make([]map[string]string, 0)
	if s.agentHub != nil {
		_, probes := s.agentHub.Providers(r.Context())
		for _, probe := range probes {
			for _, modelID := range probe.Models {
				selection := agenthub.ProviderSelection{ProviderID: probe.ProviderID, ModelID: modelID}
				if s.agentHub.ValidateTextTaskSelection(r.Context(), selection) == nil {
					items = append(items, map[string]string{"provider_id": string(probe.ProviderID), "model_id": modelID})
				}
			}
		}
	}
	writeJSON(w, map[string]any{"roles": roles, "verified_models": items})
}

func (s *Server) handleAPICreationModelRole(w http.ResponseWriter, r *http.Request) {
	var body creationRoleBody
	if !decodeCreationJSON(w, r, &body) {
		return
	}
	selection := agenthub.ProviderSelection{ProviderID: agenthub.ProviderID(strings.TrimSpace(body.ProviderID)), ModelID: strings.TrimSpace(body.ModelID)}
	if s.agentHub == nil {
		writeCreationError(w, fmt.Errorf("agent hub unavailable"))
		return
	}
	if err := s.agentHub.ValidateTextTaskSelection(r.Context(), selection); err != nil {
		writeAgentHubError(w, err)
		return
	}
	role, err := s.db.PutCreationModelRole(db.CreationModelRole{Role: r.PathValue("role"), ProviderID: string(selection.ProviderID), ModelID: selection.ModelID})
	if err != nil {
		writeCreationError(w, err)
		return
	}
	writeJSON(w, role)
}

func (s *Server) handleAPICreationSessions(w http.ResponseWriter, r *http.Request) {
	var body creationSessionBody
	if !decodeCreationJSON(w, r, &body) {
		return
	}
	if err := s.ensureCreationKnowledgeBases(body.KnowledgeBaseIDs); err != nil {
		writeCreationError(w, err)
		return
	}
	session, err := s.db.CreateCreationSession(db.CreationSessionInput{
		Title: body.Title, TopicBrief: body.TopicBrief, Style: body.Style, Platform: body.Platform, SOPStage: body.SOPStage, KnowledgeBaseIDs: body.KnowledgeBaseIDs,
	})
	if err != nil {
		writeCreationError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, session)
}

func (s *Server) handleAPICreationSession(w http.ResponseWriter, r *http.Request) {
	session, err := s.db.GetCreationSession(r.PathValue("id"))
	if err != nil {
		writeCreationError(w, err)
		return
	}
	writeJSON(w, session)
}

func (s *Server) handleAPICreationSessionRun(w http.ResponseWriter, r *http.Request) {
	var body creationRunBody
	if !decodeCreationJSON(w, r, &body) {
		return
	}
	if s.creationService == nil {
		writeCreationError(w, fmt.Errorf("creation service unavailable"))
		return
	}
	result, err := s.creationService.Run(r.Context(), creation.RunRequest{
		SessionID: r.PathValue("id"), Action: body.Action, Input: body.Input, AllowExternal: body.AllowExternal,
	})
	if err != nil {
		writeCreationError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) ensureCreationKnowledgeBases(ids []int64) error {
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return fmt.Errorf("invalid knowledge base attachment")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		if _, err := s.db.GetKnowledgeBase(id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("knowledge base not found")
			}
			return err
		}
	}
	return nil
}

func decodeCreationJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxCreationRequestBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "CREATION_REQUEST_INVALID"})
		return false
	}
	return true
}

func writeCreationError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "CREATION_REQUEST_FAILED"
	if errors.Is(err, sql.ErrNoRows) {
		status = http.StatusNotFound
		code = "CREATION_NOT_FOUND"
	}
	if strings.Contains(err.Error(), "model unavailable") || strings.Contains(err.Error(), "no verified") {
		status = http.StatusUnprocessableEntity
		code = "CREATION_MODEL_UNAVAILABLE"
	}
	writeAgentHubJSON(w, status, map[string]string{"error": code})
}
