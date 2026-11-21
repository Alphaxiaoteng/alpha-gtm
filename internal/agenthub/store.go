package agenthub

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/db"
)

type Store struct {
	db      *db.DB
	now     func() time.Time
	writeMu sync.Mutex
}

func NewStore(database *db.DB) *Store {
	return &Store{db: database, now: time.Now}
}

func (s *Store) ListConfiguredProviders(ctx context.Context) ([]ProviderID, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider_id FROM agent_provider_configs ORDER BY position,added_at,provider_id`)
	if err != nil {
		return nil, fmt.Errorf("list configured agent providers: %w", err)
	}
	defer rows.Close()
	providers := make([]ProviderID, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		providers = append(providers, ProviderID(id))
	}
	return providers, rows.Err()
}

func (s *Store) ListProviderConfigs(ctx context.Context) ([]ProviderConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider_id,executable_path,launch_mode FROM agent_provider_configs ORDER BY position,added_at,provider_id`)
	if err != nil {
		return nil, fmt.Errorf("list configured agent provider profiles: %w", err)
	}
	defer rows.Close()
	configs := make([]ProviderConfig, 0)
	for rows.Next() {
		var config ProviderConfig
		if err := rows.Scan(&config.ProviderID, &config.ExecutablePath, &config.LaunchMode); err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}
	return configs, rows.Err()
}

func (s *Store) AddConfiguredProvider(ctx context.Context, provider ProviderID) error {
	return s.SaveProviderConfig(ctx, ProviderConfig{ProviderID: provider})
}

func (s *Store) SaveProviderConfig(ctx context.Context, config ProviderConfig) error {
	now := nowText(s.now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_provider_configs(provider_id,executable_path,launch_mode,position,added_at,updated_at)
		VALUES(?,?,?,COALESCE((SELECT MAX(position)+1 FROM agent_provider_configs),0),?,?)
		ON CONFLICT(provider_id) DO UPDATE SET executable_path=excluded.executable_path,launch_mode=excluded.launch_mode,updated_at=excluded.updated_at`,
		config.ProviderID, strings.TrimSpace(config.ExecutablePath), strings.TrimSpace(string(config.LaunchMode)), now, now)
	if err != nil {
		return fmt.Errorf("add configured agent provider: %w", err)
	}
	return nil
}

func (s *Store) RemoveConfiguredProvider(ctx context.Context, provider ProviderID) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM agent_provider_configs WHERE provider_id=?`, provider); err != nil {
		return fmt.Errorf("remove configured agent provider: %w", err)
	}
	return nil
}

// EnsureLegacyOptionalProviders keeps an existing Antigravity setup visible
// after it moves from a hardcoded default to an optional Provider. Fresh
// installs have no matching state and therefore start with OpenCode + Codex.
func (s *Store) EnsureLegacyOptionalProviders(ctx context.Context) error {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM agent_provider_selections WHERE provider_id=?
		UNION ALL SELECT 1 FROM agent_provider_cache WHERE provider_id=?
		UNION ALL SELECT 1 FROM agent_provider_models WHERE provider_id=?
		UNION ALL SELECT 1 FROM agent_conversations WHERE provider_id=?
	)`, ProviderAntigravity, ProviderAntigravity, ProviderAntigravity, ProviderAntigravity).Scan(&exists)
	if err != nil {
		return fmt.Errorf("inspect legacy optional agent providers: %w", err)
	}
	if exists {
		return s.AddConfiguredProvider(ctx, ProviderAntigravity)
	}
	return nil
}

func (s *Store) HasActiveProviderRun(ctx context.Context, provider ProviderID) (bool, error) {
	var active bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runs WHERE provider_id=? AND status='running')`, provider).Scan(&active)
	return active, err
}

func (s *Store) CreateConversation(ctx context.Context, provider ProviderID, title, modelID string, snapshot ContextSnapshot) (*Conversation, error) {
	normalized, err := snapshot.Normalize()
	if err != nil {
		return nil, err
	}
	snapshot = normalized
	modelID = strings.TrimSpace(modelID)
	if err := ValidateModelID(modelID); err != nil {
		return nil, err
	}
	if provider == "" {
		provider = DefaultProvider
	}
	title = strings.TrimSpace(title)
	if len(title) > 160 {
		title = title[:160]
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode agent context: %w", err)
	}
	now := nowText(s.now())
	conversation := &Conversation{ID: newID("conv"), Title: title, Provider: provider, ModelID: modelID, WorkspaceMode: snapshot.WorkspaceMode, Context: snapshot, CreatedAt: now, UpdatedAt: now}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO agent_conversations(id,title,provider_id,model_id,workspace_mode,context_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, conversation.ID, conversation.Title, conversation.Provider, conversation.ModelID, conversation.WorkspaceMode, string(encoded), now, now); err != nil {
		return nil, fmt.Errorf("create agent conversation: %w", err)
	}
	return conversation, nil
}

// CreateConversationWithMode is the explicit mode-aware constructor. The
// legacy constructor remains available and derives the mode from snapshot.
func (s *Store) CreateConversationWithMode(ctx context.Context, provider ProviderID, title, modelID string, mode WorkspaceMode, snapshot ContextSnapshot) (*Conversation, error) {
	normalized, err := normalizeWorkspaceMode(mode)
	if err != nil {
		return nil, err
	}
	if snapshot.WorkspaceMode != "" && snapshot.WorkspaceMode != normalized {
		return nil, ErrWorkspaceModeMismatch
	}
	snapshot.WorkspaceMode = normalized
	return s.CreateConversation(ctx, provider, title, modelID, snapshot)
}

// CreateConversationInMode accepts the snapshot before the mode for callers
// that model the mode as an optional query-like argument.
func (s *Store) CreateConversationInMode(ctx context.Context, provider ProviderID, title, modelID string, snapshot ContextSnapshot, mode WorkspaceMode) (*Conversation, error) {
	return s.CreateConversationWithMode(ctx, provider, title, modelID, mode, snapshot)
}

func (s *Store) ListConversations(ctx context.Context, modes ...WorkspaceMode) ([]Conversation, error) {
	mode, err := oneWorkspaceMode(modes)
	if err != nil {
		return nil, err
	}
	return s.listConversations(ctx, "", mode)
}

func (s *Store) ListConversationsByMode(ctx context.Context, mode WorkspaceMode) ([]Conversation, error) {
	return s.listConversations(ctx, "", mode)
}

func (s *Store) ListConversationsByProvider(ctx context.Context, provider ProviderID, modes ...WorkspaceMode) ([]Conversation, error) {
	mode, err := oneWorkspaceMode(modes)
	if err != nil {
		return nil, err
	}
	return s.listConversations(ctx, provider, mode)
}

func (s *Store) ListConversationsByProviderAndMode(ctx context.Context, provider ProviderID, mode WorkspaceMode) ([]Conversation, error) {
	return s.listConversations(ctx, provider, mode)
}

func oneWorkspaceMode(modes []WorkspaceMode) (WorkspaceMode, error) {
	if len(modes) > 1 {
		return "", fmt.Errorf("agent conversation list accepts at most one workspace mode")
	}
	if len(modes) == 0 {
		return "", nil
	}
	mode, err := normalizeWorkspaceMode(modes[0])
	if err != nil {
		return "", err
	}
	return mode, nil
}

func (s *Store) listConversations(ctx context.Context, provider ProviderID, mode WorkspaceMode) ([]Conversation, error) {
	mode, err := normalizeOptionalWorkspaceMode(mode)
	if err != nil {
		return nil, err
	}
	query := `SELECT id,title,provider_id,model_id,bot_id,workspace_mode,context_json,created_at,updated_at FROM agent_conversations`
	args := []any{}
	where := make([]string, 0, 2)
	if provider != "" {
		where = append(where, `provider_id=?`)
		args = append(args, provider)
	}
	if mode != "" {
		where = append(where, `workspace_mode=?`)
		args = append(args, mode)
	}
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	// Cap the roster so a long-lived install cannot make every sidebar refresh
	// scan an unbounded history. Newest activity still wins.
	query += ` ORDER BY updated_at DESC,id DESC LIMIT 500`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list agent conversations: %w", err)
	}
	defer rows.Close()
	conversations := make([]Conversation, 0)
	for rows.Next() {
		conversation, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		if conversation.Context.Transient ||
			(conversation.Context.Workspace == "trend-radar" &&
				conversation.Context.Page == "video-analysis" &&
				strings.TrimSpace(conversation.Title) == "趋势雷达 · 视频拆解") {
			continue
		}
		conversations = append(conversations, conversation)
	}
	return conversations, rows.Err()
}

func normalizeOptionalWorkspaceMode(mode WorkspaceMode) (WorkspaceMode, error) {
	if strings.TrimSpace(string(mode)) == "" {
		return "", nil
	}
	return normalizeWorkspaceMode(mode)
}

// GetWorkspaceState returns the persisted shell state for mode. A missing row
// is a valid first-run state and is returned as an empty state with the mode
// applied, matching the provider-selection contract.
func (s *Store) GetWorkspaceState(ctx context.Context, mode WorkspaceMode) (*WorkspaceState, error) {
	normalized, err := normalizeWorkspaceMode(mode)
	if err != nil {
		return nil, err
	}
	state := &WorkspaceState{WorkspaceMode: normalized}
	var conversationID sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT current_conversation_id,draft,last_module,updated_at
		FROM agent_workspace_state WHERE workspace_mode=?`, normalized).Scan(
		&conversationID, &state.Draft, &state.LastModule, &state.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent workspace state: %w", err)
	}
	if conversationID.Valid {
		state.CurrentConversationID = conversationID.String
	}
	return state, nil
}

func (s *Store) LoadWorkspaceState(ctx context.Context, mode WorkspaceMode) (*WorkspaceState, error) {
	return s.GetWorkspaceState(ctx, mode)
}

func (s *Store) PutWorkspaceState(ctx context.Context, state WorkspaceState) (*WorkspaceState, error) {
	normalized, err := state.Normalize()
	if err != nil {
		return nil, err
	}
	if len(normalized.Draft) > 16000 {
		return nil, fmt.Errorf("agent workspace draft must be at most 16000 characters")
	}
	if !safeWorkspaceModule(normalized.LastModule) {
		return nil, fmt.Errorf("agent workspace last module is invalid")
	}
	if normalized.CurrentConversationID != "" {
		var conversationModeText string
		err := s.db.QueryRowContext(ctx, `SELECT workspace_mode FROM agent_conversations WHERE id=?`, normalized.CurrentConversationID).Scan(&conversationModeText)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConversationNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("check agent workspace conversation: %w", err)
		}
		conversationMode, err := normalizeWorkspaceMode(WorkspaceMode(conversationModeText))
		if err != nil {
			return nil, err
		}
		if conversationMode != normalized.WorkspaceMode {
			return nil, ErrWorkspaceConversationMode
		}
	}
	now := nowText(s.now())
	normalized.UpdatedAt = now
	var current any
	if normalized.CurrentConversationID != "" {
		current = normalized.CurrentConversationID
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_workspace_state(workspace_mode,current_conversation_id,draft,last_module,updated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(workspace_mode) DO UPDATE SET current_conversation_id=excluded.current_conversation_id,
		draft=excluded.draft,last_module=excluded.last_module,updated_at=excluded.updated_at`,
		normalized.WorkspaceMode, current, normalized.Draft, normalized.LastModule, normalized.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("save agent workspace state: %w", err)
	}
	return &normalized, nil
}

func (s *Store) SaveWorkspaceState(ctx context.Context, state WorkspaceState) (*WorkspaceState, error) {
	return s.PutWorkspaceState(ctx, state)
}

func (s *Store) GetAgentWorkspaceState(ctx context.Context, mode WorkspaceMode) (*WorkspaceState, error) {
	return s.GetWorkspaceState(ctx, mode)
}

func safeWorkspaceModule(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if len(value) > 120 || filepath.IsAbs(value) || strings.Contains(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	lower := strings.ToLower(value)
	for _, forbidden := range []string{"token", "password", "secret", "cookie", "credential"} {
		if strings.Contains(lower, forbidden) {
			return false
		}
	}
	return true
}

func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM agent_conversations WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete agent conversation: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete agent conversation: %w", err)
	}
	if deleted == 0 {
		return ErrConversationNotFound
	}
	return nil
}

func (s *Store) ListTransientConversationIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,context_json FROM agent_conversations ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list transient agent conversations: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id, encoded string
		if err := rows.Scan(&id, &encoded); err != nil {
			return nil, err
		}
		var snapshot ContextSnapshot
		if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
			return nil, fmt.Errorf("decode agent context: %w", err)
		}
		if snapshot.Transient {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

func (s *Store) UpdateConversation(ctx context.Context, id string, title, modelID *string) (*Conversation, error) {
	current, err := s.GetConversation(ctx, id)
	if err != nil {
		return nil, err
	}
	if title != nil {
		current.Title = strings.TrimSpace(*title)
		if len(current.Title) > 160 {
			current.Title = current.Title[:160]
		}
	}
	if modelID != nil {
		current.ModelID = strings.TrimSpace(*modelID)
		if err := ValidateModelID(current.ModelID); err != nil {
			return nil, err
		}
	}
	current.UpdatedAt = nowText(s.now())
	if _, err := s.db.ExecContext(ctx, `UPDATE agent_conversations SET title=?,model_id=?,updated_at=? WHERE id=?`, current.Title, current.ModelID, current.UpdatedAt, id); err != nil {
		return nil, fmt.Errorf("update agent conversation: %w", err)
	}
	return current, nil
}

func (s *Store) GetProviderSelection(ctx context.Context, provider ProviderID) (*ProviderSelection, error) {
	selection := &ProviderSelection{ProviderID: provider}
	err := s.db.QueryRowContext(ctx, `SELECT conversation_id,model_id,draft,updated_at FROM agent_provider_selections WHERE provider_id=?`, provider).Scan(&selection.ConversationID, &selection.ModelID, &selection.Draft, &selection.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return selection, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent provider selection: %w", err)
	}
	return selection, nil
}

func (s *Store) LastProviderID(ctx context.Context) (ProviderID, error) {
	var provider string
	err := s.db.QueryRowContext(ctx, `SELECT provider_id FROM agent_provider_selections ORDER BY updated_at DESC LIMIT 1`).Scan(&provider)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultProvider, nil
	}
	if err != nil {
		return "", fmt.Errorf("get last agent provider: %w", err)
	}
	return ProviderID(provider), nil
}

func (s *Store) PutProviderSelection(ctx context.Context, selection ProviderSelection) (*ProviderSelection, error) {
	if strings.TrimSpace(string(selection.ProviderID)) == "" {
		return nil, ErrProviderNotFound
	}
	selection.ModelID = strings.TrimSpace(selection.ModelID)
	if err := ValidateModelID(selection.ModelID); err != nil {
		return nil, err
	}
	if len(selection.Draft) > 16000 {
		return nil, fmt.Errorf("agent draft must be at most 16000 characters")
	}
	if selection.ConversationID != "" {
		var provider string
		err := s.db.QueryRowContext(ctx, `SELECT provider_id FROM agent_conversations WHERE id=?`, selection.ConversationID).Scan(&provider)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrConversationNotFound
		}
		if err != nil {
			return nil, err
		}
		if ProviderID(provider) != selection.ProviderID {
			return nil, ErrProviderMismatch
		}
	}
	selection.UpdatedAt = nowText(s.now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_provider_selections(provider_id,conversation_id,model_id,draft,updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(provider_id) DO UPDATE SET conversation_id=excluded.conversation_id,model_id=excluded.model_id,draft=excluded.draft,updated_at=excluded.updated_at`,
		selection.ProviderID, selection.ConversationID, selection.ModelID, selection.Draft, selection.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("save agent provider selection: %w", err)
	}
	return &selection, nil
}

func (s *Store) LoadProviderProbes(ctx context.Context) ([]ProviderProbe, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider_id,state,available,ready,version,models_json,reason,checked_at,model_expires_at FROM agent_provider_cache`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := s.now().UTC()
	probes := make([]ProviderProbe, 0)
	for rows.Next() {
		var probe ProviderProbe
		var provider, state, modelsJSON, modelExpiresAt string
		if err := rows.Scan(&provider, &state, &probe.Available, &probe.Ready, &probe.Version, &modelsJSON, &probe.Reason, &probe.CheckedAt, &modelExpiresAt); err != nil {
			return nil, err
		}
		probe.ProviderID = ProviderID(provider)
		probe.State = ProviderState(state)
		probe.Cached = true
		if err := json.Unmarshal([]byte(modelsJSON), &probe.ModelCandidates); err != nil {
			probe.ModelCandidates = nil
		}
		if expires, err := time.Parse(time.RFC3339Nano, modelExpiresAt); err != nil || !now.Before(expires) {
			probe.ModelCandidates = nil
		}
		if checked, err := time.Parse(time.RFC3339Nano, probe.CheckedAt); err != nil || now.Sub(checked) > 5*time.Minute {
			probe.State = ProviderStateUnknown
			probe.Ready = false
		}
		probes = append(probes, probe)
	}
	return probes, rows.Err()
}

func (s *Store) SaveProviderProbe(ctx context.Context, probe ProviderProbe) error {
	models, err := json.Marshal(probe.ModelCandidates)
	if err != nil {
		return err
	}
	checkedAt := probe.CheckedAt
	if checkedAt == "" {
		checkedAt = nowText(s.now())
	}
	modelExpiresAt := s.now().UTC().Add(24 * time.Hour).Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_provider_cache(provider_id,state,available,ready,version,models_json,reason,checked_at,model_expires_at) VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(provider_id) DO UPDATE SET state=excluded.state,available=excluded.available,ready=excluded.ready,version=excluded.version,models_json=excluded.models_json,reason=excluded.reason,checked_at=excluded.checked_at,model_expires_at=excluded.model_expires_at`,
		probe.ProviderID, probe.State, probe.Available, probe.Ready, probe.Version, string(models), probe.Reason, checkedAt, modelExpiresAt)
	return err
}

func (s *Store) LoadVerifiedModels(ctx context.Context, provider ProviderID, version string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT model_id FROM agent_provider_models
		WHERE provider_id=? AND provider_version=? ORDER BY model_id`,
		provider, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	models := make([]string, 0)
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return nil, err
		}
		if ValidateModelID(model) == nil && model != "" {
			models = append(models, model)
		}
	}
	return models, rows.Err()
}

func (s *Store) SaveVerifiedModel(ctx context.Context, provider ProviderID, version, model string) error {
	if provider == "" || strings.TrimSpace(model) == "" {
		return ErrInvalidModel
	}
	if err := ValidateModelID(model); err != nil {
		return err
	}
	now := s.now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_provider_models(provider_id,model_id,provider_version,verified_at,expires_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(provider_id,model_id) DO UPDATE SET provider_version=excluded.provider_version,verified_at=excluded.verified_at,expires_at=excluded.expires_at`,
		// The exact CLI version and the provider's current model catalog are the
		// validity boundary. Keep the legacy column populated for older readers;
		// current readers do not trigger paid nonce calls on a daily timer.
		provider, model, version, now.Format(time.RFC3339Nano), "9999-12-31T23:59:59Z")
	return err
}

func (s *Store) DeleteVerifiedModel(ctx context.Context, provider ProviderID, model string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_provider_models WHERE provider_id=? AND model_id=?`, provider, model)
	return err
}

type modelValidationBatch struct {
	ProviderID      ProviderID
	ProviderVersion string
	RemainingModels []string
	Total           int
	Completed       int
	Passed          int
	Failures        []ModelValidationFailure
}

func (s *Store) SaveModelValidationBatch(ctx context.Context, batch modelValidationBatch) error {
	if batch.ProviderID == "" || len(batch.RemainingModels) > 32 || batch.Total < 1 ||
		batch.Completed < 0 || batch.Passed < 0 || batch.Completed > batch.Total || batch.Passed > batch.Completed {
		return ErrInvalidModel
	}
	for _, model := range batch.RemainingModels {
		if strings.TrimSpace(model) == "" || ValidateModelID(model) != nil {
			return ErrInvalidModel
		}
	}
	remaining, err := json.Marshal(batch.RemainingModels)
	if err != nil {
		return err
	}
	failures, err := json.Marshal(batch.Failures)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_model_validation_batches(
			provider_id,provider_version,remaining_models_json,total,completed,passed,failures_json,updated_at
		) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(provider_id) DO UPDATE SET
			provider_version=excluded.provider_version,
			remaining_models_json=excluded.remaining_models_json,
			total=excluded.total,
			completed=excluded.completed,
			passed=excluded.passed,
			failures_json=excluded.failures_json,
			updated_at=excluded.updated_at`,
		batch.ProviderID, batch.ProviderVersion, string(remaining), batch.Total, batch.Completed,
		batch.Passed, string(failures), nowText(s.now()))
	return err
}

func (s *Store) LoadModelValidationBatch(ctx context.Context, provider ProviderID) (*modelValidationBatch, error) {
	var batch modelValidationBatch
	var remainingJSON, failuresJSON string
	err := s.db.QueryRowContext(ctx, `SELECT provider_id,provider_version,remaining_models_json,total,completed,passed,failures_json
		FROM agent_model_validation_batches WHERE provider_id=?`, provider).Scan(
		&batch.ProviderID, &batch.ProviderVersion, &remainingJSON, &batch.Total, &batch.Completed, &batch.Passed, &failuresJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal([]byte(remainingJSON), &batch.RemainingModels) != nil ||
		json.Unmarshal([]byte(failuresJSON), &batch.Failures) != nil {
		return nil, ErrModelValidation
	}
	if batch.ProviderID != provider || batch.Total < 1 || batch.Completed < 0 || batch.Completed > batch.Total ||
		batch.Passed < 0 || batch.Passed > batch.Completed || len(batch.RemainingModels) > 32 {
		return nil, ErrModelValidation
	}
	for _, model := range batch.RemainingModels {
		if strings.TrimSpace(model) == "" || ValidateModelID(model) != nil {
			return nil, ErrModelValidation
		}
	}
	return &batch, nil
}

func (s *Store) DeleteModelValidationBatch(ctx context.Context, provider ProviderID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_model_validation_batches WHERE provider_id=?`, provider)
	return err
}

func (s *Store) GetConversation(ctx context.Context, id string) (*Conversation, error) {
	conversation, err := scanConversation(s.db.QueryRowContext(ctx, `SELECT id,title,provider_id,model_id,bot_id,workspace_mode,context_json,created_at,updated_at FROM agent_conversations WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get agent conversation: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,provider_id,run_id,role,content,context_json,created_at FROM agent_messages WHERE conversation_id=? ORDER BY created_at,id`, id)
	if err != nil {
		return nil, fmt.Errorf("list agent messages: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		conversation.Messages = append(conversation.Messages, message)
	}
	return &conversation, rows.Err()
}

func (s *Store) CreateRun(ctx context.Context, conversationID string, provider ProviderID, modelID, message string, snapshot ContextSnapshot) (*Run, error) {
	if strings.TrimSpace(message) == "" || len(message) > 16000 {
		return nil, fmt.Errorf("agent message is required and must be at most 16000 characters")
	}
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	modelID = strings.TrimSpace(modelID)
	if err := ValidateModelID(modelID); err != nil {
		return nil, err
	}
	contextJSON, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode agent context: %w", err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var existingProvider, existingTitle, existingMode string
	if err := tx.QueryRowContext(ctx, `SELECT provider_id,title,workspace_mode FROM agent_conversations WHERE id=?`, conversationID).Scan(&existingProvider, &existingTitle, &existingMode); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConversationNotFound
	} else if err != nil {
		return nil, err
	}
	conversationMode, err := normalizeWorkspaceMode(WorkspaceMode(existingMode))
	if err != nil {
		return nil, err
	}
	snapshotMode, err := snapshot.resolveWorkspaceMode()
	if err != nil {
		return nil, err
	}
	if snapshotMode == DefaultWorkspaceMode && strings.TrimSpace(string(snapshot.WorkspaceMode)) == "" && strings.TrimSpace(string(snapshot.ProductMode)) == "" {
		snapshotMode = conversationMode
	}
	if snapshotMode != conversationMode {
		return nil, ErrWorkspaceModeMismatch
	}
	snapshot.WorkspaceMode = conversationMode
	snapshot.ProductMode = ProductMode(conversationMode)
	snapshot, err = snapshot.Normalize()
	if err != nil {
		return nil, err
	}
	contextJSON, err = json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode agent context: %w", err)
	}
	if provider == "" {
		provider = ProviderID(existingProvider)
	}
	if provider != ProviderID(existingProvider) {
		return nil, ErrProviderMismatch
	}
	// Runs are mutually exclusive per conversation (backed by
	// idx_agent_runs_active_conversation); the global cap only guards shared
	// provider resources so unrelated conversations can run in parallel.
	var conversationActive int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs WHERE conversation_id=? AND status='running'`, conversationID).Scan(&conversationActive); err != nil {
		return nil, err
	}
	if conversationActive > 0 {
		return nil, ErrRunActive
	}
	var activeRuns int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runs WHERE status='running'`).Scan(&activeRuns); err != nil {
		return nil, err
	}
	if activeRuns >= MaxConcurrentRuns {
		return nil, ErrRunCapacity
	}
	now := nowText(s.now())
	run := &Run{ID: newID("run"), ConversationID: conversationID, ProviderID: provider, ModelID: modelID, Status: "running", CreatedAt: now, UpdatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_runs(id,conversation_id,provider_id,model_id,status,error_code,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, run.ID, run.ConversationID, run.ProviderID, run.ModelID, run.Status, "", now, now); err != nil {
		if strings.Contains(err.Error(), "idx_agent_runs_active_conversation") || strings.Contains(err.Error(), "UNIQUE constraint failed: agent_runs.conversation_id") {
			return nil, ErrRunActive
		}
		return nil, fmt.Errorf("create agent run: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_messages(id,conversation_id,provider_id,run_id,role,content,context_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, newID("msg"), conversationID, provider, run.ID, "user", strings.TrimSpace(message), string(contextJSON), now); err != nil {
		return nil, fmt.Errorf("create agent message: %w", err)
	}
	title := existingTitle
	if needsConversationTitle(title) {
		title = conversationTitle(snapshot, message)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_conversations SET title=?,model_id=?,workspace_mode=?,context_json=?,updated_at=? WHERE id=?`, title, modelID, conversationMode, string(contextJSON), now, conversationID); err != nil {
		return nil, fmt.Errorf("update agent conversation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return run, nil
}

// AppendKickoffMessage writes a persona's opening line into a conversation that
// has no messages yet. run_id stays empty on purpose: the line is authored by
// the bot, not by a provider run. The INSERT ... WHERE NOT EXISTS makes it
// idempotent, so a retried creation cannot duplicate the greeting.
func (s *Store) AppendKickoffMessage(ctx context.Context, conversationID string, provider ProviderID, content string) error {
	conversationID = strings.TrimSpace(conversationID)
	content = strings.TrimSpace(content)
	if conversationID == "" || content == "" {
		return nil
	}
	now := nowText(s.now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_messages(id,conversation_id,provider_id,run_id,role,content,context_json,created_at)
		SELECT ?,?,?,'','assistant',?,'{}',? WHERE NOT EXISTS (SELECT 1 FROM agent_messages WHERE conversation_id=?)`,
		newID("msg"), conversationID, provider, content, now, conversationID)
	if err != nil {
		return fmt.Errorf("append agent kickoff message: %w", err)
	}
	return nil
}

// ListUnattributedMessages returns messages written outside a provider run:
// delegation tasks, delegation results and persona kickoffs. Provider sessions
// never see these, so the next run has to receive them explicitly.
func (s *Store) ListUnattributedMessages(ctx context.Context, conversationID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,provider_id,run_id,role,content,context_json,created_at
		FROM agent_messages WHERE conversation_id=? AND run_id=''
		ORDER BY created_at DESC,id DESC LIMIT ?`, conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("list unattributed agent messages: %w", err)
	}
	defer rows.Close()
	var messages []Message
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The query is newest-first for the LIMIT; prompts read chronologically.
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	return messages, nil
}

func (s *Store) AppendAssistantMessage(ctx context.Context, run *Run, content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if run == nil {
		return fmt.Errorf("agent assistant message requires a run")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	now := nowText(s.now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_messages(id,conversation_id,provider_id,run_id,role,content,context_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, newID("msg"), run.ConversationID, run.ProviderID, run.ID, "assistant", strings.TrimSpace(content), "{}", now)
	return err
}

func (s *Store) AppendEvent(ctx context.Context, run *Run, event ProviderEvent) (*Event, error) {
	if run == nil || strings.TrimSpace(event.Type) == "" {
		return nil, fmt.Errorf("agent event requires a run and type")
	}
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode agent event: %w", err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	now := nowText(s.now())
	var id, seq int64
	err = s.db.QueryRowContext(
		ctx,
		`INSERT INTO agent_events(run_id,conversation_id,provider_id,seq,timestamp,type,payload_json)
		 VALUES(?,?,?,COALESCE((SELECT MAX(seq) FROM agent_events WHERE run_id=?),0)+1,?,?,?)
		 RETURNING id, seq`,
		run.ID, run.ConversationID, run.ProviderID, run.ID, now, event.Type, string(encoded),
	).Scan(&id, &seq)
	if err != nil {
		return nil, err
	}
	return &Event{ID: id, RunID: run.ID, ConversationID: run.ConversationID, ProviderID: run.ProviderID, Seq: seq, Timestamp: now, Type: event.Type, Payload: payload}, nil
}

func (s *Store) ListEventsAfter(ctx context.Context, runID string, afterID int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,run_id,conversation_id,provider_id,seq,timestamp,type,payload_json FROM agent_events WHERE run_id=? AND id>? ORDER BY id`, runID, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		var provider string
		var payload string
		if err := rows.Scan(&event.ID, &event.RunID, &event.ConversationID, &provider, &event.Seq, &event.Timestamp, &event.Type, &payload); err != nil {
			return nil, err
		}
		event.ProviderID = ProviderID(provider)
		if err := json.Unmarshal([]byte(payload), &event.Payload); err != nil {
			return nil, fmt.Errorf("decode agent event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// ListAgentActivities projects durable event envelopes into the small,
// user-visible activity contract. It intentionally discards every payload
// field and therefore cannot expose raw arguments, paths or credentials.
func (s *Store) ListAgentActivities(ctx context.Context, runID string, afterID int64) ([]AgentActivity, error) {
	events, err := s.ListEventsAfter(ctx, runID, afterID)
	if err != nil {
		return nil, err
	}
	activities := make([]AgentActivity, 0, len(events))
	for _, event := range events {
		activities = append(activities, event.AgentActivity())
	}
	return activities, nil
}

func (s *Store) ListActivitiesAfter(ctx context.Context, runID string, afterID int64) ([]AgentActivity, error) {
	return s.ListAgentActivities(ctx, runID, afterID)
}

func (s *Store) AgentActivities(ctx context.Context, runID string, afterID int64) ([]AgentActivity, error) {
	return s.ListAgentActivities(ctx, runID, afterID)
}

func (s *Store) GetAgentActivity(ctx context.Context, id int64) (*AgentActivity, error) {
	if id <= 0 {
		return nil, ErrActivityNotFound
	}
	var event Event
	var provider, payload string
	err := s.db.QueryRowContext(ctx, `SELECT id,run_id,conversation_id,provider_id,seq,timestamp,type,payload_json FROM agent_events WHERE id=?`, id).
		Scan(&event.ID, &event.RunID, &event.ConversationID, &provider, &event.Seq, &event.Timestamp, &event.Type, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrActivityNotFound
	}
	if err != nil {
		return nil, err
	}
	event.ProviderID = ProviderID(provider)
	if err := json.Unmarshal([]byte(payload), &event.Payload); err != nil {
		return nil, fmt.Errorf("decode agent event: %w", err)
	}
	activity := event.AgentActivity()
	return &activity, nil
}

// ProjectAgentActivity converts one event to a safe activity value. Only
// reviewed event kinds and status values are emitted; unknown provider events
// remain generic system activity.
func ProjectAgentActivity(event Event) AgentActivity {
	typ := strings.ToLower(strings.TrimSpace(event.Type))
	kind := "system"
	if strings.HasPrefix(typ, "tool.") {
		kind = activityKindForTool(activityPayloadToken(event.Payload, "tool_name"))
	} else {
		switch {
		case strings.HasPrefix(typ, "approval."):
			kind = "approval"
		case strings.HasPrefix(typ, "evidence."):
			kind = "evidence"
		case strings.HasPrefix(typ, "subagent."):
			kind = "subagent"
		}
	}
	status := activityStatus(typ, event.Payload)
	title := activityTitle(kind)
	var detailRef *AgentActivityDetailRef
	if event.ID > 0 {
		detailRef = &AgentActivityDetailRef{Type: "agent_event", ID: strconv.FormatInt(event.ID, 10)}
	}
	return AgentActivity{
		ID:         event.ID,
		RunID:      event.RunID,
		Kind:       kind,
		Title:      title,
		Summary:    title + " " + status,
		Status:     status,
		DetailRef:  detailRef,
		OccurredAt: event.Timestamp,
	}
}

func (event Event) AgentActivity() AgentActivity {
	return ProjectAgentActivity(event)
}

func (event Event) ToAgentActivity() AgentActivity {
	return ProjectAgentActivity(event)
}

func (event Event) Activity() AgentActivity {
	return ProjectAgentActivity(event)
}

func activityPayloadToken(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	value = strings.TrimSpace(value)
	if !safeActivityToken(value) {
		return ""
	}
	return value
}

func safeActivityToken(value string) bool {
	if value == "" || len(value) > 160 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == ':' {
			continue
		}
		return false
	}
	return true
}

func activityKindForTool(toolName string) string {
	name := strings.ToLower(strings.TrimSpace(toolName))
	switch {
	case strings.HasPrefix(name, "skill."):
		return "skill"
	case strings.HasPrefix(name, "ego."), strings.HasPrefix(name, "social.account"):
		return "egolite"
	case strings.HasPrefix(name, "knowledge."):
		return "knowledge"
	case strings.HasPrefix(name, "content."), strings.HasPrefix(name, "media."), strings.HasPrefix(name, "studio."), strings.HasPrefix(name, "social.material"):
		return "content"
	case strings.HasPrefix(name, "publish."), strings.HasPrefix(name, "shop."), strings.HasPrefix(name, "social.publish"):
		return "publish_plan"
	case strings.HasPrefix(name, "evidence."), strings.HasPrefix(name, "social.metrics"):
		return "evidence"
	case strings.HasPrefix(name, "subagent."), strings.HasPrefix(name, "agent."):
		return "subagent"
	case strings.HasPrefix(name, "dashboard."), strings.HasPrefix(name, "settings."), strings.HasPrefix(name, "queue."):
		return "system"
	default:
		return "registry_tool"
	}
}

func activityStatus(eventType string, payload map[string]any) string {
	if eventType == "approval.resolved" {
		switch activityPayloadToken(payload, "status") {
		case "approved":
			return "completed"
		case "rejected", "cancelled":
			return "cancelled"
		}
	}
	if eventType == "run.started" || eventType == "message.delta" || strings.HasSuffix(eventType, ".started") || strings.HasSuffix(eventType, ".running") {
		return "running"
	}
	if eventType == "run.failed" || strings.HasSuffix(eventType, ".failed") {
		return "failed"
	}
	if eventType == "run.cancelled" || eventType == "run.interrupted" || strings.HasSuffix(eventType, ".cancelled") {
		return "cancelled"
	}
	if eventType == "run.completed" || eventType == "message.completed" || strings.HasSuffix(eventType, ".completed") || eventType == "provider.session.saved" {
		return "completed"
	}
	if status := activityPayloadToken(payload, "status"); status != "" {
		switch status {
		case "pending":
			return "pending"
		case "running":
			return "running"
		case "completed", "approved":
			return "completed"
		case "failed":
			return "failed"
		case "cancelled", "rejected", "expired":
			return "cancelled"
		}
	}
	if eventType == "approval.required" || strings.HasSuffix(eventType, ".proposed") {
		return "pending"
	}
	return "pending"
}

func activityTitle(kind string) string {
	switch kind {
	case "skill":
		return "Skill"
	case "registry_tool":
		return "Tool"
	case "egolite":
		return "EgoLite"
	case "content":
		return "Content"
	case "knowledge":
		return "Knowledge"
	case "publish_plan":
		return "Publish plan"
	case "approval":
		return "Approval"
	case "evidence":
		return "Evidence"
	case "subagent":
		return "Subagent"
	default:
		return "Agent"
	}
}

func (s *Store) GetRun(ctx context.Context, id string) (*Run, error) {
	var run Run
	var provider string
	err := s.db.QueryRowContext(ctx, `SELECT id,conversation_id,provider_id,model_id,provider_session_id,status,error_code,created_at,updated_at FROM agent_runs WHERE id=?`, id).Scan(&run.ID, &run.ConversationID, &provider, &run.ModelID, &run.ProviderSessionID, &run.Status, &run.ErrorCode, &run.CreatedAt, &run.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRunNotFound
	}
	if err != nil {
		return nil, err
	}
	run.ProviderID = ProviderID(provider)
	return &run, nil
}

func (s *Store) UserMessageForRun(ctx context.Context, runID string) (string, error) {
	var message string
	err := s.db.QueryRowContext(ctx, `SELECT content FROM agent_messages WHERE run_id=? AND role='user' ORDER BY created_at DESC,id DESC LIMIT 1`, runID).Scan(&message)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrRunNotFound
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(message), nil
}

func (s *Store) FinishRun(ctx context.Context, runID, status, code, providerSessionID string) error {
	if status != "completed" && status != "failed" && status != "cancelled" && status != "interrupted" {
		return fmt.Errorf("invalid agent run status")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	now := nowText(s.now())
	result, err := s.db.ExecContext(ctx, `UPDATE agent_runs SET status=?,error_code=?,provider_session_id=CASE WHEN ?<>'' THEN ? ELSE provider_session_id END,updated_at=? WHERE id=? AND status='running'`, status, code, providerSessionID, providerSessionID, now, runID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrRunNotActive
	}
	if providerSessionID != "" {
		_, _ = s.db.ExecContext(ctx, `INSERT INTO agent_provider_sessions(conversation_id,provider_id,session_id,updated_at) SELECT conversation_id,provider_id,?,? FROM agent_runs WHERE id=? ON CONFLICT(conversation_id,provider_id) DO UPDATE SET session_id=excluded.session_id,updated_at=excluded.updated_at`, providerSessionID, now, runID)
	}
	return nil
}

func (s *Store) CancelRun(ctx context.Context, runID string) (*Run, error) {
	run, err := s.GetRun(ctx, runID)
	if err != nil || run.Status != "running" {
		if err == nil {
			err = ErrRunNotActive
		}
		return nil, err
	}
	if err := s.FinishRun(ctx, runID, "cancelled", "AGENT_RUN_CANCELLED", ""); err != nil {
		return nil, err
	}
	run.Status = "cancelled"
	run.ErrorCode = "AGENT_RUN_CANCELLED"
	run.UpdatedAt = nowText(s.now())
	_, _ = s.db.ExecContext(ctx, `UPDATE agent_approvals SET status='cancelled',resolved_at=? WHERE run_id=? AND status='pending'`, run.UpdatedAt, runID)
	return run, nil
}

func (s *Store) MarkInterrupted(ctx context.Context) error {
	now := nowText(s.now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,conversation_id,provider_id FROM agent_runs WHERE status='running'`)
	if err != nil {
		return err
	}
	var runs []Run
	for rows.Next() {
		var run Run
		var provider string
		if err := rows.Scan(&run.ID, &run.ConversationID, &provider); err != nil {
			_ = rows.Close()
			return err
		}
		run.ProviderID = ProviderID(provider)
		runs = append(runs, run)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, run := range runs {
		var seq int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM agent_events WHERE run_id=?`, run.ID).Scan(&seq); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_events(run_id,conversation_id,provider_id,seq,timestamp,type,payload_json) VALUES(?,?,?,?,?,'run.interrupted',?)`, run.ID, run.ConversationID, run.ProviderID, seq, now, `{"code":"AGENT_RUN_INTERRUPTED"}`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status='interrupted',error_code='AGENT_RUN_INTERRUPTED',updated_at=? WHERE status='running'`, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_approvals SET status='cancelled',resolved_at=? WHERE status='pending'`, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ResolveApproval(ctx context.Context, id, status string) (*Approval, error) {
	if status != "approved" && status != "rejected" {
		return nil, fmt.Errorf("invalid approval resolution")
	}
	now := nowText(s.now())
	result, err := s.db.ExecContext(ctx, `UPDATE agent_approvals SET status=?,resolved_at=? WHERE id=? AND status='pending' AND expires_at>?`, status, now, id, now)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed == 0 {
		return nil, fmt.Errorf("agent approval is no longer pending")
	}
	approval := &Approval{}
	err = s.db.QueryRowContext(ctx, `SELECT id,run_id,tool_name,params_hash,impact_preview,status,expires_at,created_at,resolved_at FROM agent_approvals WHERE id=?`, id).Scan(&approval.ID, &approval.RunID, &approval.ToolName, &approval.ParamsHash, &approval.ImpactPreview, &approval.Status, &approval.ExpiresAt, &approval.CreatedAt, &approval.ResolvedAt)
	return approval, err
}

func (s *Store) CreateToolCall(ctx context.Context, run *Run, call *ToolCall, paramsJSON string, needsApproval bool) error {
	if run == nil || call == nil {
		return fmt.Errorf("agent tool call requires a run")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_tool_calls(
		id,run_id,conversation_id,provider_id,tool_name,workspace,risk_level,params_json,params_hash,impact_preview,status,expires_at,created_at,updated_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		call.ID, run.ID, run.ConversationID, run.ProviderID, call.ToolName, call.Workspace, call.RiskLevel,
		paramsJSON, call.ParamsHash, call.ImpactPreview, call.Status, call.ExpiresAt, call.CreatedAt, call.UpdatedAt); err != nil {
		return err
	}
	if needsApproval {
		call.ApprovalID = newID("approval")
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_approvals(
			id,run_id,tool_name,params_json,params_hash,impact_preview,status,expires_at,created_at
		) VALUES(?,?,?,?,?,?,?,?,?)`,
			call.ApprovalID, run.ID, call.ToolName, paramsJSON, call.ParamsHash, call.ImpactPreview,
			"pending", call.ExpiresAt, call.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ObserveExternalToolCall records a tool call that the provider runtime
// executed with its own tools (e.g. the dsh harness's bash), rather than one
// proposed through the ToolBroker. It mirrors the read-model shape brokered
// calls use, but carries no approval state: the call is inserted in its final
// status, so the conversation timeline and activity feed stay truthful about
// what the runtime actually did. A later observation of the same external call
// (typically the tool/result after the tool/call) upserts over the earlier
// row, so status and result always reflect the latest observation.
func (s *Store) ObserveExternalToolCall(ctx context.Context, run *Run, call *ToolCall, paramsJSON, resultJSON string) error {
	if run == nil || call == nil {
		return fmt.Errorf("agent tool call requires a run")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_tool_calls(
		id,run_id,conversation_id,provider_id,tool_name,workspace,risk_level,params_json,params_hash,impact_preview,status,result_json,error_code,created_at,updated_at,consumed_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET
		tool_name=CASE WHEN excluded.tool_name='' THEN agent_tool_calls.tool_name ELSE excluded.tool_name END,
		risk_level=CASE WHEN excluded.risk_level='' THEN agent_tool_calls.risk_level ELSE excluded.risk_level END,
		workspace=CASE WHEN excluded.workspace='' THEN agent_tool_calls.workspace ELSE excluded.workspace END,
		impact_preview=CASE WHEN excluded.impact_preview='' THEN agent_tool_calls.impact_preview ELSE excluded.impact_preview END,
		params_json=CASE WHEN excluded.params_json='' THEN agent_tool_calls.params_json ELSE excluded.params_json END,
		status=excluded.status,
		result_json=excluded.result_json,
		error_code=excluded.error_code,
		updated_at=excluded.updated_at,
		consumed_at=excluded.consumed_at`,
		call.ID, run.ID, run.ConversationID, run.ProviderID, call.ToolName, call.Workspace, call.RiskLevel,
		paramsJSON, call.ParamsHash, call.ImpactPreview, call.Status, resultJSON, call.ErrorCode,
		call.CreatedAt, call.UpdatedAt, call.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetToolCall(ctx context.Context, id string) (*ToolCall, string, error) {
	call := &ToolCall{}
	var provider, paramsJSON, resultJSON string
	err := s.db.QueryRowContext(ctx, `SELECT id,run_id,conversation_id,provider_id,tool_name,workspace,risk_level,
		params_json,params_hash,impact_preview,status,result_json,error_code,expires_at,created_at,updated_at
		FROM agent_tool_calls WHERE id=?`, id).Scan(
		&call.ID, &call.RunID, &call.ConversationID, &provider, &call.ToolName, &call.Workspace, &call.RiskLevel,
		&paramsJSON, &call.ParamsHash, &call.ImpactPreview, &call.Status, &resultJSON, &call.ErrorCode,
		&call.ExpiresAt, &call.CreatedAt, &call.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", fmt.Errorf("agent tool call not found")
	}
	if err != nil {
		return nil, "", err
	}
	call.ProviderID = ProviderID(provider)
	if resultJSON != "" {
		_ = json.Unmarshal([]byte(resultJSON), &call.Result)
	}
	return call, paramsJSON, nil
}

func (s *Store) GetToolCallByApproval(ctx context.Context, approvalID string) (*ToolCall, string, error) {
	var callID string
	err := s.db.QueryRowContext(ctx, `SELECT c.id FROM agent_tool_calls c
		JOIN agent_approvals a ON a.run_id=c.run_id AND a.tool_name=c.tool_name AND a.params_hash=c.params_hash
		WHERE a.id=?`, approvalID).Scan(&callID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", fmt.Errorf("agent approval has no frozen tool call")
	}
	if err != nil {
		return nil, "", err
	}
	call, paramsJSON, err := s.GetToolCall(ctx, callID)
	if err == nil {
		call.ApprovalID = approvalID
	}
	return call, paramsJSON, err
}

func (s *Store) ListPendingToolCalls(ctx context.Context, provider ProviderID) ([]ToolCall, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.run_id,c.conversation_id,c.provider_id,c.tool_name,c.workspace,c.risk_level,
		c.params_hash,c.impact_preview,c.status,c.expires_at,c.created_at,c.updated_at,a.id
		FROM agent_tool_calls c
		JOIN agent_approvals a ON a.run_id=c.run_id AND a.tool_name=c.tool_name AND a.params_hash=c.params_hash
		WHERE c.provider_id=? AND c.status='pending' AND a.status='pending' AND a.expires_at>?
		ORDER BY c.created_at`, provider, nowText(s.now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	calls := make([]ToolCall, 0)
	for rows.Next() {
		var call ToolCall
		var providerID string
		if err := rows.Scan(&call.ID, &call.RunID, &call.ConversationID, &providerID, &call.ToolName, &call.Workspace,
			&call.RiskLevel, &call.ParamsHash, &call.ImpactPreview, &call.Status, &call.ExpiresAt, &call.CreatedAt,
			&call.UpdatedAt, &call.ApprovalID); err != nil {
			return nil, err
		}
		call.ProviderID = ProviderID(providerID)
		calls = append(calls, call)
	}
	return calls, rows.Err()
}

func (s *Store) MarkToolCall(ctx context.Context, id, fromStatus, status, resultJSON, errorCode string) error {
	now := nowText(s.now())
	query := `UPDATE agent_tool_calls SET status=?,result_json=?,error_code=?,updated_at=?`
	args := []any{status, resultJSON, errorCode, now}
	if status == "completed" || status == "failed" {
		query += `,consumed_at=?`
		args = append(args, now)
	}
	query += ` WHERE id=?`
	args = append(args, id)
	if fromStatus != "" {
		query += ` AND status=?`
		args = append(args, fromStatus)
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("agent tool call is no longer executable")
	}
	return nil
}

func scanConversation(scanner interface{ Scan(...any) error }) (Conversation, error) {
	var conversation Conversation
	var provider, workspaceMode, raw string
	err := scanner.Scan(&conversation.ID, &conversation.Title, &provider, &conversation.ModelID, &conversation.BotID, &workspaceMode, &raw, &conversation.CreatedAt, &conversation.UpdatedAt)
	if err != nil {
		return conversation, err
	}
	conversation.Provider = ProviderID(provider)
	if err := json.Unmarshal([]byte(raw), &conversation.Context); err != nil {
		return conversation, fmt.Errorf("decode agent context: %w", err)
	}
	mode, err := normalizeWorkspaceMode(WorkspaceMode(workspaceMode))
	if err != nil {
		return conversation, err
	}
	conversation.WorkspaceMode = mode
	// The relational column is authoritative for isolation. This also repairs
	// old context JSON that predates the mode field without rewriting it on read.
	conversation.Context.WorkspaceMode = mode
	conversation.Context.ProductMode = ProductMode(mode)
	return conversation, nil
}

func conversationTitle(snapshot ContextSnapshot, message string) string {
	const maxRunes = 30
	page := strings.TrimSpace(snapshot.Page)
	message = strings.Join(strings.Fields(strings.TrimSpace(message)), " ")
	if page == "" {
		page = "助手"
	}
	if !hasConversationIntent(message) {
		return page + " · 助手任务"
	}
	runes := []rune(message)
	if len(runes) > maxRunes {
		message = string(runes[:maxRunes]) + "…"
	}
	return page + " · " + message
}

func needsConversationTitle(title string) bool {
	title = strings.TrimSpace(title)
	return title == "" || title == "新会话" || !hasConversationIntent(title)
}

func hasConversationIntent(value string) bool {
	for _, rune := range strings.TrimSpace(value) {
		if (rune >= 'a' && rune <= 'z') || (rune >= 'A' && rune <= 'Z') || (rune >= 0x4E00 && rune <= 0x9FFF) {
			return true
		}
	}
	return false
}

func scanMessage(scanner interface{ Scan(...any) error }) (Message, error) {
	var message Message
	var provider, raw string
	if err := scanner.Scan(&message.ID, &provider, &message.RunID, &message.Role, &message.Content, &raw, &message.CreatedAt); err != nil {
		return message, err
	}
	message.ProviderID = ProviderID(provider)
	if err := json.Unmarshal([]byte(raw), &message.Context); err != nil {
		return message, fmt.Errorf("decode agent message context: %w", err)
	}
	return message, nil
}

func newID(prefix string) string {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(bytes[:])
}

// HasActiveRun reports whether the conversation still owns a running turn.
func (s *Store) HasActiveRun(ctx context.Context, conversationID string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_runs WHERE conversation_id=? AND status='running'`, conversationID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
