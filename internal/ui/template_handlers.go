package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/platforms"
)

const maxTemplateBodyBytes = 72 << 10

type templateBody struct {
	PlatformCode string `json:"platform_code"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	SettingsJSON string `json:"settings_json"`
	Enabled      bool   `json:"enabled"`
	IsDefault    bool   `json:"is_default"`
}

func (s *Server) handleAPITemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		templates, err := s.db.ListPlatformSettings()
		if err != nil {
			http.Error(w, "list templates failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, templates)
	case http.MethodPost:
		body, ok := decodeTemplateBody(w, r)
		if !ok {
			return
		}
		template, err := s.db.CreatePlatformSetting(body.PlatformCode, body.Name, body.Description, body.SettingsJSON, body.Enabled, body.IsDefault)
		if err != nil {
			writeTemplateError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, template)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPITemplate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "template id required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		template, err := s.db.GetPlatformSetting(id)
		if err != nil {
			http.Error(w, "template not found", http.StatusNotFound)
			return
		}
		writeJSON(w, template)
	case http.MethodPut:
		body, ok := decodeTemplateBody(w, r)
		if !ok {
			return
		}
		if err := s.db.ReplacePlatformSetting(id, body.PlatformCode, body.Name, body.Description, body.SettingsJSON, body.Enabled, body.IsDefault); err != nil {
			writeTemplateError(w, err)
			return
		}
		template, _ := s.db.GetPlatformSetting(id)
		writeJSON(w, template)
	case http.MethodDelete:
		if err := s.db.DeletePlatformSetting(id); err != nil {
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, "template not found", http.StatusNotFound)
				return
			}
			http.Error(w, "delete template failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"deleted": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func decodeTemplateBody(w http.ResponseWriter, r *http.Request) (templateBody, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTemplateBodyBytes)
	defer r.Body.Close()
	var body templateBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid template body", http.StatusBadRequest)
		return templateBody{}, false
	}
	body.PlatformCode = strings.TrimSpace(body.PlatformCode)
	body.Name = strings.TrimSpace(body.Name)
	body.Description = strings.TrimSpace(body.Description)
	body.SettingsJSON = strings.TrimSpace(body.SettingsJSON)
	if body.SettingsJSON == "" {
		body.SettingsJSON = "{}"
	}
	if _, ok := platforms.SettingsSchemaFor(body.PlatformCode); ok {
		normalized, err := platforms.NormalizeSettings(body.PlatformCode, body.SettingsJSON)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return templateBody{}, false
		}
		body.SettingsJSON = normalized
	}
	return body, true
}

func writeTemplateError(w http.ResponseWriter, err error) {
	message := err.Error()
	switch {
	case strings.Contains(message, "not found"):
		http.Error(w, message, http.StatusNotFound)
	case strings.Contains(message, "required"), strings.Contains(message, "JSON"), strings.Contains(message, "KiB"), strings.Contains(message, "credentials"), strings.Contains(message, "positive"):
		http.Error(w, message, http.StatusBadRequest)
	default:
		http.Error(w, "save template failed", http.StatusInternalServerError)
	}
}
