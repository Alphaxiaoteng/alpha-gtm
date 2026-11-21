package ui

import (
	"encoding/json"
	"errors"
	"net/http"

	"alpha-nexus-egolite/internal/skilllib"
)

func (s *Server) handleAPISkills(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		skills, err := s.skillLibrary.List(r.Context())
		if err != nil {
			writeSkillError(w, err)
			return
		}
		writeAgentHubJSON(w, http.StatusOK, map[string]any{"skills": skills})
	case http.MethodPost:
		var input skilllib.CreateInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeAgentHubJSON(w, http.StatusBadRequest, map[string]string{"error": "SKILL_INPUT_INVALID"})
			return
		}
		skill, err := s.skillLibrary.Create(r.Context(), input)
		if err != nil {
			writeSkillError(w, err)
			return
		}
		writeAgentHubJSON(w, http.StatusCreated, skill)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPISkill(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	skill, err := s.skillLibrary.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeSkillError(w, err)
		return
	}
	writeAgentHubJSON(w, http.StatusOK, skill)
}

func (s *Server) handleAPISkillImport(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAgentHubJSON(w, http.StatusBadRequest, map[string]string{"error": "SKILL_INPUT_INVALID"})
		return
	}
	skill, err := s.skillLibrary.Import(r.Context(), body.Content)
	if err != nil {
		writeSkillError(w, err)
		return
	}
	writeAgentHubJSON(w, http.StatusCreated, skill)
}

func (s *Server) handleAPISkillEnabled(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAgentHubJSON(w, http.StatusBadRequest, map[string]string{"error": "SKILL_INPUT_INVALID"})
		return
	}
	skill, err := s.skillLibrary.SetEnabled(r.Context(), r.PathValue("id"), body.Enabled)
	if err != nil {
		writeSkillError(w, err)
		return
	}
	writeAgentHubJSON(w, http.StatusOK, skill)
}

func writeSkillError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "SKILL_REQUEST_FAILED"
	if errors.Is(err, skilllib.ErrNotFound) {
		status = http.StatusNotFound
		code = "SKILL_NOT_FOUND"
	}
	if errors.Is(err, skilllib.ErrNotEnableable) {
		status = http.StatusConflict
		code = "SKILL_REVIEW_REQUIRED"
	}
	writeAgentHubJSON(w, status, map[string]string{"error": code})
}
