package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

var creationModelRoles = map[string]struct{}{
	"conversation": {},
	"copywriter":   {},
	"reviewer":     {},
}

type CreationModelRole struct {
	Role       string `json:"role"`
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
	UpdatedAt  string `json:"updated_at"`
}

type CreationSessionInput struct {
	Title            string
	TopicBrief       string
	Style            string
	Platform         string
	SOPStage         string
	KnowledgeBaseIDs []int64
}

type CreationSession struct {
	ID               string        `json:"id"`
	Title            string        `json:"title"`
	TopicBrief       string        `json:"topic_brief"`
	Style            string        `json:"style"`
	Platform         string        `json:"platform"`
	SOPStage         string        `json:"sop_stage"`
	KnowledgeBaseIDs []int64       `json:"knowledge_base_ids"`
	CreatedAt        string        `json:"created_at"`
	UpdatedAt        string        `json:"updated_at"`
	Runs             []CreationRun `json:"runs"`
}

type CreationRunInput struct {
	SessionID     string
	Action        string
	ProviderID    string
	ModelID       string
	UsedFallback  bool
	PromptSummary string
	Result        string
	CitationsJSON string
	QualityJSON   string
}

type CreationRun struct {
	ID            string `json:"id"`
	SessionID     string `json:"session_id"`
	Action        string `json:"action"`
	ProviderID    string `json:"provider_id"`
	ModelID       string `json:"model_id"`
	UsedFallback  bool   `json:"used_fallback"`
	PromptSummary string `json:"prompt_summary"`
	Result        string `json:"result"`
	CitationsJSON string `json:"citations_json"`
	QualityJSON   string `json:"quality_json"`
	CreatedAt     string `json:"created_at"`
}

func (db *DB) PutCreationModelRole(role CreationModelRole) (*CreationModelRole, error) {
	role.Role = strings.TrimSpace(role.Role)
	role.ProviderID = strings.TrimSpace(role.ProviderID)
	role.ModelID = strings.TrimSpace(role.ModelID)
	if _, ok := creationModelRoles[role.Role]; !ok || role.ProviderID == "" || role.ModelID == "" || len(role.ProviderID) > 80 || len(role.ModelID) > 240 {
		return nil, fmt.Errorf("invalid creation model role")
	}
	role.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO creation_model_roles(role,provider_id,model_id,updated_at) VALUES(?,?,?,?)
		ON CONFLICT(role) DO UPDATE SET provider_id=excluded.provider_id,model_id=excluded.model_id,updated_at=excluded.updated_at`,
		role.Role, role.ProviderID, role.ModelID, role.UpdatedAt); err != nil {
		return nil, err
	}
	return db.GetCreationModelRole(role.Role)
}

func (db *DB) GetCreationModelRole(role string) (*CreationModelRole, error) {
	role = strings.TrimSpace(role)
	if _, ok := creationModelRoles[role]; !ok {
		return nil, fmt.Errorf("invalid creation model role")
	}
	var item CreationModelRole
	if err := db.QueryRow(`SELECT role,provider_id,model_id,updated_at FROM creation_model_roles WHERE role=?`, role).Scan(
		&item.Role, &item.ProviderID, &item.ModelID, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func (db *DB) ListCreationModelRoles() ([]CreationModelRole, error) {
	rows, err := db.Query(`SELECT role,provider_id,model_id,updated_at FROM creation_model_roles ORDER BY role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CreationModelRole, 0, len(creationModelRoles))
	for rows.Next() {
		var item CreationModelRole
		if err := rows.Scan(&item.Role, &item.ProviderID, &item.ModelID, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) CreateCreationSession(input CreationSessionInput) (*CreationSession, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.TopicBrief = strings.TrimSpace(input.TopicBrief)
	input.Style = strings.TrimSpace(input.Style)
	input.Platform = strings.TrimSpace(input.Platform)
	input.SOPStage = strings.TrimSpace(input.SOPStage)
	if input.SOPStage == "" {
		input.SOPStage = "topic"
	}
	if input.Title == "" || len([]rune(input.Title)) > 120 || len([]rune(input.TopicBrief)) > 16000 || len([]rune(input.Style)) > 8000 || len(input.Platform) > 80 || !validCreationStage(input.SOPStage) {
		return nil, fmt.Errorf("invalid creation session")
	}
	for _, baseID := range input.KnowledgeBaseIDs {
		if baseID <= 0 {
			return nil, fmt.Errorf("invalid knowledge base attachment")
		}
	}
	attachments, err := json.Marshal(uniqueCreationBaseIDs(input.KnowledgeBaseIDs))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	item := CreationSession{ID: newCreationID(), Title: input.Title, TopicBrief: input.TopicBrief, Style: input.Style, Platform: input.Platform, SOPStage: input.SOPStage, KnowledgeBaseIDs: uniqueCreationBaseIDs(input.KnowledgeBaseIDs), CreatedAt: now, UpdatedAt: now, Runs: []CreationRun{}}
	if _, err := db.Exec(`INSERT INTO creation_sessions(id,title,topic_brief,style,platform,sop_stage,knowledge_base_ids_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, item.ID, item.Title, item.TopicBrief, item.Style, item.Platform, item.SOPStage, string(attachments), now, now); err != nil {
		return nil, err
	}
	return &item, nil
}

func (db *DB) GetCreationSession(id string) (*CreationSession, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, sql.ErrNoRows
	}
	item := CreationSession{Runs: []CreationRun{}}
	var attachments string
	if err := db.QueryRow(`SELECT id,title,topic_brief,style,platform,sop_stage,knowledge_base_ids_json,created_at,updated_at FROM creation_sessions WHERE id=?`, id).Scan(
		&item.ID, &item.Title, &item.TopicBrief, &item.Style, &item.Platform, &item.SOPStage, &attachments, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(attachments), &item.KnowledgeBaseIDs); err != nil {
		return nil, fmt.Errorf("decode creation session attachments: %w", err)
	}
	runs, err := db.listCreationRuns(item.ID)
	if err != nil {
		return nil, err
	}
	item.Runs = runs
	return &item, nil
}

func (db *DB) AppendCreationRun(input CreationRunInput) (*CreationRun, error) {
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Action = strings.TrimSpace(input.Action)
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	input.ModelID = strings.TrimSpace(input.ModelID)
	input.PromptSummary = strings.TrimSpace(input.PromptSummary)
	input.Result = strings.TrimSpace(input.Result)
	if input.SessionID == "" || !validCreationAction(input.Action) || len(input.ProviderID) > 80 || len(input.ModelID) > 240 || input.Result == "" || len([]rune(input.PromptSummary)) > 4000 || len([]rune(input.Result)) > 64000 || !validCreationJSON(input.CitationsJSON) || !validCreationJSON(input.QualityJSON) {
		return nil, fmt.Errorf("invalid creation run")
	}
	if _, err := db.GetCreationSession(input.SessionID); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	item := CreationRun{ID: newCreationID(), SessionID: input.SessionID, Action: input.Action, ProviderID: input.ProviderID, ModelID: input.ModelID, UsedFallback: input.UsedFallback, PromptSummary: input.PromptSummary, Result: input.Result, CitationsJSON: creationJSONOrEmpty(input.CitationsJSON), QualityJSON: creationJSONOrEmpty(input.QualityJSON), CreatedAt: now}
	if _, err := db.Exec(`INSERT INTO creation_runs(id,session_id,action,provider_id,model_id,used_fallback,prompt_summary,result,citations_json,quality_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.SessionID, item.Action, item.ProviderID, item.ModelID, boolToInt(item.UsedFallback), item.PromptSummary, item.Result, item.CitationsJSON, item.QualityJSON, item.CreatedAt); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`UPDATE creation_sessions SET updated_at=? WHERE id=?`, now, item.SessionID); err != nil {
		return nil, err
	}
	return &item, nil
}

func (db *DB) listCreationRuns(sessionID string) ([]CreationRun, error) {
	rows, err := db.Query(`SELECT id,session_id,action,provider_id,model_id,used_fallback,prompt_summary,result,citations_json,quality_json,created_at
		FROM creation_runs WHERE session_id=? ORDER BY created_at,id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]CreationRun, 0)
	for rows.Next() {
		var item CreationRun
		var usedFallback int
		if err := rows.Scan(&item.ID, &item.SessionID, &item.Action, &item.ProviderID, &item.ModelID, &usedFallback, &item.PromptSummary, &item.Result, &item.CitationsJSON, &item.QualityJSON, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.UsedFallback = usedFallback == 1
		runs = append(runs, item)
	}
	return runs, rows.Err()
}

func validCreationStage(stage string) bool {
	switch stage {
	case "topic", "evidence", "copy", "script", "review", "save":
		return true
	default:
		return false
	}
}

func validCreationAction(action string) bool {
	switch action {
	case "topic", "draft", "review", "revise":
		return true
	default:
		return false
	}
}

func validCreationJSON(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	var decoded any
	return json.Unmarshal([]byte(value), &decoded) == nil
}

func creationJSONOrEmpty(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return "{}"
}

func uniqueCreationBaseIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func newCreationID() string {
	var bytes [18]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic("creation id random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:])
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
