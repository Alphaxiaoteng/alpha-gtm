package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/trace"
)

type creationDraftRequest struct {
	Type                  string `json:"type"`
	Title                 string `json:"title"`
	Caption               string `json:"caption"`
	Body                  string `json:"body"`
	TagsJSON              string `json:"tags_json"`
	ReviewResult          string `json:"review_result"`
	ManualReviewConfirmed bool   `json:"manual_review_confirmed"`
}

// handleAPICreationDraft commits only a local draft. It does not approve or
// schedule publication. The review is parsed again on the server so the UI
// cannot silently turn a revise result into a pass.
func (s *Server) handleAPICreationDraft(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxContentBodyBytes)
	defer r.Body.Close()
	var input creationDraftRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&input); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid creation draft body", http.StatusBadRequest)
		return
	}
	input.Type = strings.TrimSpace(input.Type)
	input.Title = strings.TrimSpace(input.Title)
	input.Caption = strings.TrimSpace(input.Caption)
	input.Body = strings.TrimSpace(input.Body)
	input.ReviewResult = strings.TrimSpace(input.ReviewResult)
	if input.TagsJSON == "" {
		input.TagsJSON = "[]"
	}
	if !validContentBody(contentBody{Type: input.Type, Status: "draft"}) || input.Title == "" || len([]rune(input.Title)) > 120 || input.Body == "" {
		http.Error(w, "invalid creation draft", http.StatusBadRequest)
		return
	}
	var tags []any
	if err := json.Unmarshal([]byte(input.TagsJSON), &tags); err != nil {
		http.Error(w, "tags_json must be a JSON array", http.StatusBadRequest)
		return
	}
	decision := contentReviewDecision(input.ReviewResult)
	if decision == "unknown" {
		http.Error(w, "review decision is ambiguous", http.StatusBadRequest)
		return
	}
	if decision == "revise" && !input.ManualReviewConfirmed {
		http.Error(w, "manual review confirmation required", http.StatusConflict)
		return
	}
	canonical, err := json.Marshal(struct {
		Type         string `json:"type"`
		Title        string `json:"title"`
		Caption      string `json:"caption"`
		Body         string `json:"body"`
		TagsJSON     string `json:"tags_json"`
		ReviewResult string `json:"review_result"`
	}{input.Type, input.Title, input.Caption, input.Body, input.TagsJSON, input.ReviewResult})
	if err != nil {
		http.Error(w, "hash creation draft failed", http.StatusInternalServerError)
		return
	}
	digest := sha256.Sum256(canonical)
	snapshotHash := hex.EncodeToString(digest[:])
	manualLabel := "否"
	if input.ManualReviewConfirmed {
		manualLabel = "是"
	}
	auditBlock := fmt.Sprintf("## 审核记录\n\n%s\n\n- 服务端审核结论：%s\n- 人工确认：%s\n- 审核快照 SHA-256：%s\n- 入库时间：%s",
		input.ReviewResult, decision, manualLabel, snapshotHash, time.Now().UTC().Format(time.RFC3339))
	content, err := s.db.CreateContent(input.Type, input.Title, input.Caption, input.Body+"\n\n"+auditBlock, input.TagsJSON, "draft")
	if err != nil {
		http.Error(w, "create reviewed draft failed", http.StatusInternalServerError)
		return
	}
	auditJSON, _ := json.Marshal(map[string]any{
		"review_decision": decision, "manual_review_confirmed": input.ManualReviewConfirmed,
		"review_snapshot_hash": snapshotHash,
	})
	_, _ = s.db.CreateAuditEvent(string(trace.New()), "info", "creation", "reviewed draft committed", "contents", content.ID, string(auditJSON))
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{
		"content": content, "review_decision": decision, "review_snapshot_hash": snapshotHash,
	})
}
