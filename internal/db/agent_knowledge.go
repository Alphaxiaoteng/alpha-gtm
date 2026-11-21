package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type AgentKnowledgeTurn struct {
	ID             int64           `json:"id"`
	ConversationID string          `json:"conversation_id"`
	RunID          string          `json:"run_id"`
	Query          string          `json:"query"`
	Scopes         json.RawMessage `json:"scopes"`
	Citations      json.RawMessage `json:"citations"`
	Output         string          `json:"output,omitempty"`
	CreatedAt      string          `json:"created_at"`
}

func (db *DB) CreateAgentKnowledgeTurn(ctx context.Context, conversationID, runID, query string, scopes, citations json.RawMessage) error {
	conversationID = strings.TrimSpace(conversationID)
	runID = strings.TrimSpace(runID)
	query = strings.TrimSpace(query)
	if conversationID == "" || runID == "" || !json.Valid(scopes) || !json.Valid(citations) {
		return fmt.Errorf("invalid agent knowledge turn")
	}
	_, err := db.ExecContext(ctx, `INSERT INTO agent_knowledge_turns
		(conversation_id,run_id,query,scopes_json,citations_json,created_at) VALUES (?,?,?,?,?,?)`,
		conversationID, runID, query, string(scopes), string(citations), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (db *DB) ListAgentKnowledgeTurns(ctx context.Context, conversationID string) ([]AgentKnowledgeTurn, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil, fmt.Errorf("invalid agent conversation")
	}
	rows, err := db.QueryContext(ctx, `SELECT t.id,t.conversation_id,t.run_id,t.query,t.scopes_json,t.citations_json,
		COALESCE((SELECT m.content FROM agent_messages m WHERE m.run_id=t.run_id AND m.role='assistant' ORDER BY m.created_at DESC LIMIT 1),''),t.created_at
		FROM agent_knowledge_turns t WHERE t.conversation_id=? ORDER BY t.id ASC`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AgentKnowledgeTurn, 0)
	for rows.Next() {
		var item AgentKnowledgeTurn
		var scopes, citations string
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.RunID, &item.Query, &scopes, &citations, &item.Output, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Scopes = json.RawMessage(scopes)
		item.Citations = json.RawMessage(citations)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (db *DB) GetAgentKnowledgeTurn(ctx context.Context, runID string) (*AgentKnowledgeTurn, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("invalid agent run")
	}
	var item AgentKnowledgeTurn
	var scopes, citations string
	err := db.QueryRowContext(ctx, `SELECT t.id,t.conversation_id,t.run_id,t.query,t.scopes_json,t.citations_json,
		COALESCE((SELECT m.content FROM agent_messages m WHERE m.run_id=t.run_id AND m.role='assistant' ORDER BY m.created_at DESC LIMIT 1),''),t.created_at
		FROM agent_knowledge_turns t WHERE t.run_id=?`, runID).Scan(&item.ID, &item.ConversationID, &item.RunID, &item.Query, &scopes, &citations, &item.Output, &item.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	item.Scopes = json.RawMessage(scopes)
	item.Citations = json.RawMessage(citations)
	return &item, nil
}
