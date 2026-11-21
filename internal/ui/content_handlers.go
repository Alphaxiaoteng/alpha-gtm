package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
)

const maxContentBodyBytes = 1 << 20

type contentBody struct {
	Type                      string          `json:"type"`
	Title                     string          `json:"title"`
	Caption                   string          `json:"caption"`
	Body                      string          `json:"body"`
	TagsJSON                  string          `json:"tags_json"`
	Status                    string          `json:"status"`
	PlannedAt                 string          `json:"planned_at"`
	KnowledgeCitationChunkIDs json.RawMessage `json:"knowledge_citation_chunk_ids"`
}

func (s *Server) handleAPIContents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		contents, err := s.db.ListContents()
		if err != nil {
			http.Error(w, "list contents failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, contents)
	case http.MethodPost:
		body, ok := decodeContentBody(w, r)
		if !ok {
			return
		}
		chunkIDs, present, err := parseKnowledgeCitationChunkIDs(body.KnowledgeCitationChunkIDs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var content *db.Content
		if present {
			content, err = s.db.CreateContentWithPlannedAtAndKnowledgeCitations(body.Type, body.Title, body.Caption, body.Body, body.TagsJSON, body.Status, body.PlannedAt, chunkIDs)
		} else {
			content, err = s.db.CreateContentWithPlannedAt(body.Type, body.Title, body.Caption, body.Body, body.TagsJSON, body.Status, body.PlannedAt)
		}
		if err != nil {
			if strings.Contains(err.Error(), "knowledge citation") || strings.Contains(err.Error(), "too many knowledge") {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, "create content failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, content)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIContent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "content id required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		content, err := s.db.GetContent(id)
		if err != nil {
			http.Error(w, "content not found", http.StatusNotFound)
			return
		}
		writeJSON(w, content)
	case http.MethodPut:
		body, ok := decodeContentBody(w, r)
		if !ok {
			return
		}
		chunkIDs, present, parseErr := parseKnowledgeCitationChunkIDs(body.KnowledgeCitationChunkIDs)
		if parseErr != nil {
			http.Error(w, parseErr.Error(), http.StatusBadRequest)
			return
		}
		if present {
			err = s.db.ReplaceContentWithPlannedAtAndKnowledgeCitations(id, body.Type, body.Title, body.Caption, body.Body, body.TagsJSON, body.Status, body.PlannedAt, chunkIDs)
		} else {
			err = s.db.ReplaceContentWithPlannedAt(id, body.Type, body.Title, body.Caption, body.Body, body.TagsJSON, body.Status, body.PlannedAt)
		}
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, "content not found", http.StatusNotFound)
				return
			}
			http.Error(w, "update content failed", http.StatusBadRequest)
			return
		}
		content, _ := s.db.GetContent(id)
		writeJSON(w, content)
	case http.MethodDelete:
		if err := s.db.DeleteContent(id); err != nil {
			if strings.Contains(err.Error(), "publish jobs") {
				http.Error(w, "content has publish history", http.StatusConflict)
				return
			}
			if strings.Contains(err.Error(), "not found") {
				http.Error(w, "content not found", http.StatusNotFound)
				return
			}
			http.Error(w, "delete content failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"deleted": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func parseKnowledgeCitationChunkIDs(raw json.RawMessage) ([]int64, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, fmt.Errorf("knowledge_citation_chunk_ids must be an array")
	}
	var values []int64
	if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, true, fmt.Errorf("knowledge_citation_chunk_ids must be an integer array")
	}
	return values, true, nil
}

func (s *Server) handleAPIContentKnowledgeCitations(w http.ResponseWriter, r *http.Request, contentID int64) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.db.GetContent(contentID); err != nil {
		http.Error(w, "content not found", http.StatusNotFound)
		return
	}
	items, err := s.db.ListContentKnowledgeCitations(contentID)
	if err != nil {
		http.Error(w, "list content knowledge citations failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, items)
}

func (s *Server) handleAPIContentClone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "content id required", http.StatusBadRequest)
		return
	}
	content, err := s.db.CloneContent(id)
	if err != nil {
		http.Error(w, "content not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, content)
}

func decodeContentBody(w http.ResponseWriter, r *http.Request) (contentBody, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxContentBodyBytes)
	defer r.Body.Close()
	var body contentBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid content body", http.StatusBadRequest)
		return contentBody{}, false
	}
	body.Type = strings.TrimSpace(body.Type)
	body.Status = strings.TrimSpace(body.Status)
	plannedAt, err := normalizeContentPlannedAt(body.PlannedAt)
	if err != nil {
		http.Error(w, "planned_at must be an RFC3339 timestamp", http.StatusBadRequest)
		return contentBody{}, false
	}
	body.PlannedAt = plannedAt
	if !validContentBody(body) {
		http.Error(w, "type and status are invalid", http.StatusBadRequest)
		return contentBody{}, false
	}
	if body.TagsJSON == "" {
		body.TagsJSON = "[]"
	}
	var tags any
	if json.Unmarshal([]byte(body.TagsJSON), &tags) != nil {
		http.Error(w, "tags_json must be JSON", http.StatusBadRequest)
		return contentBody{}, false
	}
	return body, true
}

func normalizeContentPlannedAt(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	plannedAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "", err
	}
	return plannedAt.UTC().Format(time.RFC3339), nil
}

func validContentBody(body contentBody) bool {
	if body.Status != "draft" && body.Status != "published" && body.Status != "archived" {
		return false
	}
	return body.Type == "article" || body.Type == "graph_text" || body.Type == "video"
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
