package ui

import (
	"encoding/json"
	"io"
	"net/http"

	"alpha-nexus-egolite/internal/tools"
)

const maxAccountTagBodyBytes = 64 << 10

func decodeAccountTagJSON(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAccountTagBodyBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return false
	}
	return true
}

func (s *Server) callAccountTagTool(w http.ResponseWriter, r *http.Request, name string, args map[string]interface{}) *tools.Result {
	result, _ := tools.Default.Call(r.Context(), s.toolRuntime, name, args)
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return nil
	}
	if !result.OK {
		code := result.Error
		if code == "" {
			code = "VALIDATION_FAILED"
		}
		writeToolsError(w, http.StatusBadRequest, code)
		return nil
	}
	return result
}

func (s *Server) handleAPIAccountTags(w http.ResponseWriter, r *http.Request) {
	result := s.callAccountTagTool(w, r, "account.tag.list", nil)
	if result == nil {
		return
	}
	writeJSON(w, result.Data["tags"])
}

func (s *Server) handleAPIAccountTagCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !decodeAccountTagJSON(w, r, &body) {
		return
	}
	result := s.callAccountTagTool(w, r, "account.tag.create", map[string]interface{}{"name": body.Name, "color": body.Color})
	if result == nil {
		return
	}
	writeJSON(w, result.Data["tag"])
}

func (s *Server) handleAPIAccountTagBulkUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountIDs []int64 `json:"accountIds"`
		TagIDs     []int64 `json:"tagIds"`
		Mode       string  `json:"mode"`
	}
	if !decodeAccountTagJSON(w, r, &body) {
		return
	}
	result := s.callAccountTagTool(w, r, "account.tag.update", map[string]interface{}{
		"account_ids": body.AccountIDs, "tag_ids": body.TagIDs, "mode": body.Mode,
	})
	if result == nil {
		return
	}
	writeJSON(w, result.Data)
}

func (s *Server) handleAPIAccountTagReorder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TagIDs []int64 `json:"tagIds"`
	}
	if !decodeAccountTagJSON(w, r, &body) {
		return
	}
	result := s.callAccountTagTool(w, r, "account.tag.reorder", map[string]interface{}{"tag_ids": body.TagIDs})
	if result == nil {
		return
	}
	writeJSON(w, result.Data)
}

func (s *Server) handleAPIAccountTagResolve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TagIDs   []int64 `json:"tagIds"`
		Platform string  `json:"platform"`
	}
	if !decodeAccountTagJSON(w, r, &body) {
		return
	}
	result := s.callAccountTagTool(w, r, "account.tag.resolve", map[string]interface{}{
		"tag_ids": body.TagIDs, "platform": body.Platform,
	})
	if result == nil {
		return
	}
	writeJSON(w, result.Data)
}
