package agenthub

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// ToolCallRecord is the conversation timeline view of one tool call: what was
// proposed, what happened, how long it took. Params and Result are already
// redacted and size-capped here, so no caller can leak a token by rendering
// them.
type ToolCallRecord struct {
	ID            string `json:"id"`
	ToolName      string `json:"tool_name"`
	Workspace     string `json:"workspace,omitempty"`
	RiskLevel     string `json:"risk_level,omitempty"`
	Status        string `json:"status"`
	ImpactPreview string `json:"impact_preview,omitempty"`
	Params        any    `json:"params,omitempty"`
	Result        any    `json:"result,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	DurationMS    int64  `json:"duration_ms"`
}

type toolCallRow struct {
	ID            string
	ToolName      string
	Workspace     string
	RiskLevel     string
	ImpactPreview string
	Status        string
	ParamsJSON    string
	ResultJSON    string
	ErrorCode     string
	CreatedAt     string
	UpdatedAt     string
}

func (s *Store) ListConversationToolCalls(ctx context.Context, conversationID string, limit int) ([]toolCallRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,tool_name,COALESCE(workspace,''),COALESCE(risk_level,''),
		COALESCE(impact_preview,''),status,COALESCE(params_json,''),COALESCE(result_json,''),COALESCE(error_code,''),
		created_at,updated_at
		FROM agent_tool_calls WHERE conversation_id=?
		ORDER BY created_at DESC, id DESC LIMIT ?`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	calls := make([]toolCallRow, 0)
	for rows.Next() {
		var row toolCallRow
		if err := rows.Scan(&row.ID, &row.ToolName, &row.Workspace, &row.RiskLevel, &row.ImpactPreview,
			&row.Status, &row.ParamsJSON, &row.ResultJSON, &row.ErrorCode, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		calls = append(calls, row)
	}
	return calls, rows.Err()
}

// ConversationToolCalls returns the most recent tool calls for one
// conversation, newest first. An unknown conversation is not an error: the
// caller shows an empty timeline rather than an error card.
func (s *Service) ConversationToolCalls(ctx context.Context, conversationID string, limit int) ([]ToolCallRecord, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || len(conversationID) > 128 {
		return []ToolCallRecord{}, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.store.ListConversationToolCalls(ctx, conversationID, limit)
	if err != nil {
		return nil, err
	}
	records := make([]ToolCallRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, ToolCallRecord{
			ID:            row.ID,
			ToolName:      row.ToolName,
			Workspace:     row.Workspace,
			RiskLevel:     row.RiskLevel,
			Status:        row.Status,
			ImpactPreview: row.ImpactPreview,
			Params:        sanitizeToolPayload(row.ParamsJSON),
			Result:        sanitizeToolPayload(row.ResultJSON),
			ErrorCode:     row.ErrorCode,
			CreatedAt:     row.CreatedAt,
			UpdatedAt:     row.UpdatedAt,
			DurationMS:    toolCallDurationMS(row.CreatedAt, row.UpdatedAt),
		})
	}
	// RFC3339Nano trims trailing zeros in the fractional part, so the SQL text
	// order is not reliable. Sort by the parsed instant instead.
	sort.SliceStable(records, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339Nano, records[i].CreatedAt)
		right, rightErr := time.Parse(time.RFC3339Nano, records[j].CreatedAt)
		if leftErr != nil || rightErr != nil {
			return records[i].CreatedAt > records[j].CreatedAt
		}
		return left.After(right)
	})
	return records, nil
}

func toolCallDurationMS(createdAt, updatedAt string) int64 {
	start, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return 0
	}
	end, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return 0
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

// toolPayloadLimit is the longest single JSON field shown in the timeline.
// Arguments and results can carry a whole transcript; the panel only needs
// enough to recognize the call.
const toolPayloadLimit = 2000

var toolSecretKeyParts = []string{
	"token", "secret", "password", "passwd", "pwd", "cookie", "authorization",
	"api_key", "apikey", "credential", "session_key", "signature", "private_key",
}

// sanitizeToolPayload decodes stored JSON, redacts secret-looking values and
// caps the printable size. Undecodable JSON is kept as a truncated string so a
// malformed payload is visible instead of silently dropped.
func sanitizeToolPayload(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		// A truncated or corrupted payload still must not leak a token, so the
		// "key": "value" pairs are masked textually before display.
		return truncateToolText(maskLooseSecretPairs(raw))
	}
	decoded = redactToolValue(decoded, 0)
	encoded, err := json.Marshal(decoded)
	if err != nil {
		return truncateToolText(raw)
	}
	if len(encoded) <= toolPayloadLimit {
		return decoded
	}
	return truncateToolText(string(encoded))
}

func redactToolValue(value any, depth int) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSecretToolKey(key) {
				out[key] = "***"
				continue
			}
			// Past the depth limit keep the shape but stop recursing: a secret
			// nested in the seventh map must still be masked by its own key.
			if depth > 6 {
				out[key] = item
				continue
			}
			out[key] = redactToolValue(item, depth+1)
		}
		return out
	case []any:
		if depth > 6 {
			return typed
		}
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactToolValue(item, depth+1))
		}
		return out
	case string:
		// Providers often store arguments as a JSON string inside the result.
		trimmed := strings.TrimSpace(typed)
		if len(trimmed) > 1 && (trimmed[0] == '{' || trimmed[0] == '[') {
			var inner any
			if err := json.Unmarshal([]byte(trimmed), &inner); err == nil {
				if encoded, marshalErr := json.Marshal(redactToolValue(inner, depth+1)); marshalErr == nil {
					return string(encoded)
				}
			}
		}
		return typed
	default:
		return value
	}
}

func isSecretToolKey(key string) bool {
	lower := strings.ToLower(key)
	for _, part := range toolSecretKeyParts {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

func truncateToolText(text string) string {
	if len(text) <= toolPayloadLimit {
		return text
	}
	limit := toolPayloadLimit
	for limit > 0 && !utf8.ValidString(text[:limit]) {
		limit--
	}
	return text[:limit] + "…"
}

var looseSecretPair = regexp.MustCompile(`(?i)("(?:[a-z0-9_]*(?:token|secret|password|passwd|pwd|cookie|authorization|api_?key|credential)[a-z0-9_]*)"\s*:\s*)"[^"]*"`)

// maskLooseSecretPairs is the fallback for payloads that are not valid JSON.
func maskLooseSecretPairs(raw string) string {
	return looseSecretPair.ReplaceAllString(raw, `${1}"***"`)
}
