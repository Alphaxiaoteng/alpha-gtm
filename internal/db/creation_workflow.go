package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type CreationWorkflow struct {
	ID                string          `json:"id"`
	ConversationID    string          `json:"conversation_id"`
	Title             string          `json:"title"`
	Platform          string          `json:"platform,omitempty"`
	Style             string          `json:"style,omitempty"`
	Stage             string          `json:"stage"`
	KnowledgeScopes   json.RawMessage `json:"knowledge_scopes"`
	AcceptedArtifacts json.RawMessage `json:"accepted_artifacts"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
}

type CreationWorkflowInput struct {
	ID                string
	ConversationID    string
	Title             string
	Platform          string
	Style             string
	Stage             string
	KnowledgeScopes   json.RawMessage
	AcceptedArtifacts json.RawMessage
}

type CreationWorkflowPatch struct {
	Title             *string
	Platform          *string
	Style             *string
	Stage             *string
	KnowledgeScopes   json.RawMessage
	AcceptedArtifacts json.RawMessage
}

func (db *DB) CreateCreationWorkflow(ctx context.Context, input CreationWorkflowInput) (*CreationWorkflow, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.ConversationID = strings.TrimSpace(input.ConversationID)
	input.Title = strings.TrimSpace(input.Title)
	input.Platform = strings.TrimSpace(input.Platform)
	input.Style = strings.TrimSpace(input.Style)
	input.Stage = strings.TrimSpace(input.Stage)
	if input.ID == "" || input.ConversationID == "" || input.Title == "" || !validCreationWorkflowStage(input.Stage) || len([]rune(input.Title)) > 160 || len([]rune(input.Style)) > 4000 || !validCreationWorkflowJSON(input.KnowledgeScopes) || !validCreationWorkflowJSON(input.AcceptedArtifacts) {
		return nil, fmt.Errorf("invalid creation workflow")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	item := &CreationWorkflow{
		ID: input.ID, ConversationID: input.ConversationID, Title: input.Title,
		Platform: input.Platform, Style: input.Style, Stage: input.Stage,
		KnowledgeScopes:   creationWorkflowJSONOrEmpty(input.KnowledgeScopes, "[]"),
		AcceptedArtifacts: creationWorkflowJSONOrEmpty(input.AcceptedArtifacts, "{}"),
		CreatedAt:         now, UpdatedAt: now,
	}
	_, err := db.ExecContext(ctx, `INSERT INTO creation_workflows
		(id,conversation_id,title,platform,style,stage,knowledge_scopes_json,accepted_artifacts_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, item.ID, item.ConversationID, item.Title, item.Platform, item.Style, item.Stage, string(item.KnowledgeScopes), string(item.AcceptedArtifacts), now, now)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (db *DB) GetCreationWorkflow(ctx context.Context, id string) (*CreationWorkflow, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, sql.ErrNoRows
	}
	item := &CreationWorkflow{}
	var scopes, artifacts string
	err := db.QueryRowContext(ctx, `SELECT id,conversation_id,title,platform,style,stage,knowledge_scopes_json,accepted_artifacts_json,created_at,updated_at
		FROM creation_workflows WHERE id=?`, id).Scan(&item.ID, &item.ConversationID, &item.Title, &item.Platform, &item.Style, &item.Stage, &scopes, &artifacts, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item.KnowledgeScopes = json.RawMessage(scopes)
	item.AcceptedArtifacts = json.RawMessage(artifacts)
	return item, nil
}

func (db *DB) ListCreationWorkflows(ctx context.Context) ([]CreationWorkflow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,conversation_id,title,platform,style,stage,knowledge_scopes_json,accepted_artifacts_json,created_at,updated_at
		FROM creation_workflows ORDER BY updated_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CreationWorkflow, 0)
	for rows.Next() {
		var item CreationWorkflow
		var scopes, artifacts string
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.Title, &item.Platform, &item.Style, &item.Stage, &scopes, &artifacts, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.KnowledgeScopes = json.RawMessage(scopes)
		item.AcceptedArtifacts = json.RawMessage(artifacts)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) UpdateCreationWorkflow(ctx context.Context, id string, patch CreationWorkflowPatch) (*CreationWorkflow, error) {
	current, err := db.GetCreationWorkflow(ctx, id)
	if err != nil {
		return nil, err
	}
	if patch.Title != nil {
		current.Title = strings.TrimSpace(*patch.Title)
	}
	if patch.Platform != nil {
		current.Platform = strings.TrimSpace(*patch.Platform)
	}
	if patch.Style != nil {
		current.Style = strings.TrimSpace(*patch.Style)
	}
	if patch.Stage != nil {
		current.Stage = strings.TrimSpace(*patch.Stage)
	}
	if len(patch.KnowledgeScopes) > 0 {
		current.KnowledgeScopes = patch.KnowledgeScopes
	}
	if len(patch.AcceptedArtifacts) > 0 {
		current.AcceptedArtifacts = patch.AcceptedArtifacts
	}
	if current.Title == "" || len([]rune(current.Title)) > 160 || len([]rune(current.Style)) > 4000 || !validCreationWorkflowStage(current.Stage) || !validCreationWorkflowJSON(current.KnowledgeScopes) || !validCreationWorkflowJSON(current.AcceptedArtifacts) {
		return nil, fmt.Errorf("invalid creation workflow update")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.ExecContext(ctx, `UPDATE creation_workflows SET title=?,platform=?,style=?,stage=?,knowledge_scopes_json=?,accepted_artifacts_json=?,updated_at=? WHERE id=?`, current.Title, current.Platform, current.Style, current.Stage, string(current.KnowledgeScopes), string(current.AcceptedArtifacts), now, current.ID)
	if err != nil {
		return nil, err
	}
	current.UpdatedAt = now
	return current, nil
}

func validCreationWorkflowStage(stage string) bool {
	switch stage {
	case "topic", "copy", "script", "review":
		return true
	default:
		return false
	}
}

func validCreationWorkflowJSON(value json.RawMessage) bool {
	return len(value) == 0 || json.Valid(value)
}

func creationWorkflowJSONOrEmpty(value json.RawMessage, fallback string) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(fallback)
	}
	return value
}
