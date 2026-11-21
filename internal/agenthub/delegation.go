package agenthub

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// A delegation is one Bot handing a bounded task to another Bot. The child side
// is a real conversation and a real run, so everything the UI already shows for
// a run keeps working; this record only adds the handoff and its returned
// result. The parent conversation receives the task as a user message and the
// answer as an assistant message, which is what makes a "group chat" readable
// without inventing a second transcript format.
const (
	DelegationRunning   = "running"
	DelegationCompleted = "completed"
	DelegationFailed    = "failed"
	DelegationCancelled = "cancelled"

	// maxDelegationTask bounds the handoff text. It matches the run message
	// limit closely enough that a delegatable task is always runnable.
	maxDelegationTask = 16000
	// MaxConcurrentDelegations caps in-flight handoffs per parent conversation.
	// Runs already cap at MaxConcurrentRuns globally; this stops one transcript
	// from opening every bot at once and starving the rest of the app.
	MaxConcurrentDelegations = 3
)

var (
	ErrDelegationNotFound = errors.New("agent delegation not found")
	ErrDelegationSelf     = errors.New("a bot cannot delegate to itself")
	ErrDelegationBusy     = errors.New("that bot is already working on a task from this conversation")
	ErrDelegationLimit    = errors.New("too many handoffs are already running")
)

type Delegation struct {
	ID                   string `json:"id"`
	ParentConversationID string `json:"parent_conversation_id"`
	FromBotID            string `json:"from_bot_id,omitempty"`
	ToBotID              string `json:"to_bot_id"`
	ChildConversationID  string `json:"child_conversation_id"`
	ChildRunID           string `json:"child_run_id"`
	Task                 string `json:"task"`
	Status               string `json:"status"`
	Result               string `json:"result,omitempty"`
	ErrorCode            string `json:"error_code,omitempty"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

// DelegateRequest is the caller-facing shape. FromBotID is optional: the rail
// can hand a task over from the main agent without naming it.
type DelegateRequest struct {
	ParentConversationID string `json:"parent_conversation_id"`
	FromBotID            string `json:"from_bot_id,omitempty"`
	ToBotID              string `json:"to_bot_id"`
	Task                 string `json:"task"`
}

// CreateDelegation persists the handoff row. It runs after the child run has
// been created, so a row can never point at a run that does not exist.
func (s *Store) CreateDelegation(ctx context.Context, delegation Delegation) (*Delegation, error) {
	now := nowText(s.now())
	delegation.ID = newID("dlg")
	delegation.CreatedAt = now
	delegation.UpdatedAt = now
	if strings.TrimSpace(delegation.Status) == "" {
		delegation.Status = DelegationRunning
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_delegations(id,parent_conversation_id,from_bot_id,to_bot_id,child_conversation_id,child_run_id,task,status,result,error_code,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		delegation.ID, delegation.ParentConversationID, delegation.FromBotID, delegation.ToBotID,
		delegation.ChildConversationID, delegation.ChildRunID, delegation.Task, delegation.Status,
		delegation.Result, delegation.ErrorCode, delegation.CreatedAt, delegation.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "idx_agent_delegations_running") ||
			strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrDelegationBusy
		}
		return nil, fmt.Errorf("create agent delegation: %w", err)
	}
	return &delegation, nil
}

// HasRunningDelegation answers the cheap question before a child run starts:
// is this exact handoff already in flight?
func (s *Store) HasRunningDelegation(ctx context.Context, parentConversationID, toBotID string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_delegations WHERE parent_conversation_id=? AND to_bot_id=? AND status='running')`,
		parentConversationID, toBotID).Scan(&exists)
	return exists != 0, err
}

// RunningDelegationCount is the per-transcript cap input.
func (s *Store) RunningDelegationCount(ctx context.Context, parentConversationID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_delegations WHERE parent_conversation_id=? AND status='running'`,
		parentConversationID).Scan(&count)
	return count, err
}

// RecoverInterruptedDelegations closes handoffs whose watcher died with the
// process. Without it a restart leaves rows "running" forever and the UI shows
// a spinner for work nobody is doing.
func (s *Store) RecoverInterruptedDelegations(ctx context.Context, errorCode string) ([]Delegation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+delegationColumns+` FROM agent_delegations WHERE status='running'`)
	if err != nil {
		return nil, fmt.Errorf("list interrupted delegations: %w", err)
	}
	var interrupted []Delegation
	for rows.Next() {
		delegation, scanErr := scanDelegation(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		interrupted = append(interrupted, delegation)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	now := nowText(s.now())
	for index := range interrupted {
		if _, err := s.db.ExecContext(ctx, `UPDATE agent_delegations SET status='failed',error_code=?,updated_at=? WHERE id=? AND status='running'`,
			errorCode, now, interrupted[index].ID); err != nil {
			return nil, fmt.Errorf("recover interrupted delegation: %w", err)
		}
	}
	return interrupted, nil
}

// UpdateDelegation writes a terminal state. Guarding on the previous status
// makes a cancel that races the watcher idempotent instead of overwriting a
// finished result with "cancelled".
func (s *Store) UpdateDelegation(ctx context.Context, id, status, result, errorCode string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE agent_delegations SET status=?,result=?,error_code=?,updated_at=? WHERE id=? AND status='running'`,
		status, result, errorCode, nowText(s.now()), id)
	if err != nil {
		return fmt.Errorf("update agent delegation: %w", err)
	}
	return nil
}

func (s *Store) GetDelegation(ctx context.Context, id string) (*Delegation, error) {
	delegation, err := scanDelegation(s.db.QueryRowContext(ctx, `SELECT `+delegationColumns+` FROM agent_delegations WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDelegationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get agent delegation: %w", err)
	}
	return &delegation, nil
}

// ListDelegations returns the newest handoffs of one conversation. The UI polls
// this to show "正在处理" without subscribing to a second event stream.
func (s *Store) ListDelegations(ctx context.Context, conversationID string, limit int) ([]Delegation, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+delegationColumns+` FROM agent_delegations WHERE parent_conversation_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, strings.TrimSpace(conversationID), limit)
	if err != nil {
		return nil, fmt.Errorf("list agent delegations: %w", err)
	}
	defer rows.Close()
	delegations := []Delegation{}
	for rows.Next() {
		delegation, err := scanDelegation(rows)
		if err != nil {
			return nil, err
		}
		delegations = append(delegations, delegation)
	}
	return delegations, rows.Err()
}

// AppendUserMessage records a message the user did not type through the normal
// run path — here, the "@Bot task" line a delegation writes into the parent
// conversation. run_id stays empty because no run owns this line.
func (s *Store) AppendUserMessage(ctx context.Context, conversationID string, provider ProviderID, content string) error {
	conversationID = strings.TrimSpace(conversationID)
	content = strings.TrimSpace(content)
	if conversationID == "" || content == "" {
		return nil
	}
	now := nowText(s.now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_messages(id,conversation_id,provider_id,run_id,role,content,context_json,created_at) VALUES(?,?,?,'','user',?,'{}',?)`,
		newID("msg"), conversationID, provider, content, now)
	if err != nil {
		return fmt.Errorf("append agent user message: %w", err)
	}
	// System-written handoff lines are already visible to the user in the
	// parent transcript; letting them raise a rail badge would leave a dot that
	// points at text the user just read. Real provider messages are different:
	// their unread watermark must stay untouched.
	if err := s.touchConversationForSystemWrite(ctx, conversationID, now); err != nil {
		return err
	}
	return nil
}

// touchConversationForSystemWrite advances updated_at and only advances the
// read watermark when the thread has no real (provider-run) unread messages.
// An API/tool-initiated handoff must never hide an answer the user has not
// opened yet.
func (s *Store) touchConversationForSystemWrite(ctx context.Context, conversationID, now string) error {
	var lastRead string
	if err := s.db.QueryRowContext(ctx, `SELECT last_read_at FROM agent_conversations WHERE id=?`, conversationID).Scan(&lastRead); err != nil {
		return fmt.Errorf("read agent conversation watermark: %w", err)
	}
	var realUnread int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_messages
		WHERE conversation_id=? AND run_id<>''
		  AND (?='' OR julianday(created_at)>julianday(?))`, conversationID, lastRead, lastRead).Scan(&realUnread); err != nil {
		return fmt.Errorf("count real unread messages: %w", err)
	}
	readAt := lastRead
	if realUnread == 0 {
		readAt = now
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE agent_conversations SET updated_at=?,last_read_at=? WHERE id=?`, now, readAt, conversationID); err != nil {
		return fmt.Errorf("touch agent conversation: %w", err)
	}
	return nil
}

// AppendDelegationResultMessage records the returned answer in the parent
// conversation. It is an assistant message with an empty run_id: the parent has
// no run for it, and pretending otherwise would break run-scoped replay.
func (s *Store) AppendDelegationResultMessage(ctx context.Context, conversationID string, provider ProviderID, content string) error {
	conversationID = strings.TrimSpace(conversationID)
	content = strings.TrimSpace(content)
	if conversationID == "" || content == "" {
		return nil
	}
	now := nowText(s.now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO agent_messages(id,conversation_id,provider_id,run_id,role,content,context_json,created_at) VALUES(?,?,?,'','assistant',?,'{}',?)`,
		newID("msg"), conversationID, provider, content, now)
	if err != nil {
		return fmt.Errorf("append delegation result message: %w", err)
	}
	if err := s.touchConversationForSystemWrite(ctx, conversationID, now); err != nil {
		return err
	}
	return nil
}

const delegationColumns = `id,parent_conversation_id,from_bot_id,to_bot_id,child_conversation_id,child_run_id,task,status,result,error_code,created_at,updated_at`

type delegationScanner interface {
	Scan(dest ...any) error
}

func scanDelegation(row delegationScanner) (Delegation, error) {
	var delegation Delegation
	err := row.Scan(&delegation.ID, &delegation.ParentConversationID, &delegation.FromBotID, &delegation.ToBotID,
		&delegation.ChildConversationID, &delegation.ChildRunID, &delegation.Task, &delegation.Status,
		&delegation.Result, &delegation.ErrorCode, &delegation.CreatedAt, &delegation.UpdatedAt)
	return delegation, err
}

// Delegate starts the child run and returns immediately. Completion is watched
// in the background because a provider run can take minutes; blocking an HTTP
// handler on it would time out and lose the result.
func (s *Service) Delegate(ctx context.Context, request DelegateRequest) (*Delegation, error) {
	parentID := strings.TrimSpace(request.ParentConversationID)
	toBotID := strings.TrimSpace(request.ToBotID)
	fromBotID := strings.TrimSpace(request.FromBotID)
	task := strings.TrimSpace(request.Task)
	if parentID == "" {
		return nil, fmt.Errorf("delegation requires a parent conversation")
	}
	if toBotID == "" {
		return nil, fmt.Errorf("delegation requires a target bot")
	}
	if task == "" || len(task) > maxDelegationTask {
		return nil, fmt.Errorf("delegation task is required and must be at most %d characters", maxDelegationTask)
	}
	if fromBotID != "" && fromBotID == toBotID {
		return nil, ErrDelegationSelf
	}
	parent, err := s.store.GetConversation(ctx, parentID)
	if err != nil {
		return nil, err
	}
	bot, err := s.store.GetBot(ctx, toBotID)
	if err != nil {
		return nil, err
	}

	// Adversarial input is cheap to send and expensive to run. Reject a second
	// handoff to the same bot, and cap how many a single transcript can open.
	if busy, busyErr := s.store.HasRunningDelegation(ctx, parentID, bot.ID); busyErr != nil {
		return nil, busyErr
	} else if busy {
		return nil, ErrDelegationBusy
	}
	running, err := s.store.RunningDelegationCount(ctx, parentID)
	if err != nil {
		return nil, err
	}
	if running >= MaxConcurrentDelegations {
		return nil, ErrDelegationLimit
	}

	providerID, modelID := s.botRoute(ctx, bot, parent)
	child, err := s.findBotConversation(ctx, bot.ID, parent.WorkspaceMode)
	if err != nil {
		return nil, err
	}
	if child == nil {
		created, createErr := s.store.CreateConversationInMode(ctx, providerID, bot.Name, modelID, parent.Context, parent.WorkspaceMode)
		if createErr != nil {
			return nil, createErr
		}
		bound, bindErr := s.store.SetConversationBot(ctx, created.ID, bot.ID)
		if bindErr != nil {
			return nil, bindErr
		}
		child = bound
		if seedErr := s.SeedKickoffMessage(ctx, child.ID); seedErr != nil {
			return nil, seedErr
		}
	}

	// The child conversation keeps its own provider/model. A bot whose pinned
	// model differs from its existing conversation must not silently drift, so
	// the stored conversation wins for later turns.
	providerID = child.Provider
	modelID = child.ModelID
	run, err := s.StartRunWithPromptContext(ctx, child.ID, providerID, modelID, task, parent.Context, delegationPromptContext(bot, task))
	if err != nil {
		return nil, err
	}

	// The parent transcript shows who the task went to before any result
	// arrives, so a slow bot never looks like a lost message.
	mention := "@" + bot.Name + " " + task
	if err := s.store.AppendUserMessage(ctx, parentID, parent.Provider, mention); err != nil {
		return nil, err
	}
	delegation, err := s.store.CreateDelegation(ctx, Delegation{
		ParentConversationID: parentID,
		FromBotID:            fromBotID,
		ToBotID:              bot.ID,
		ChildConversationID:  child.ID,
		ChildRunID:           run.ID,
		Task:                 task,
		Status:               DelegationRunning,
	})
	if err != nil {
		return nil, err
	}
	if !s.beginDelegationWatch() {
		_ = s.store.UpdateDelegation(context.Background(), delegation.ID, DelegationCancelled, "", "AGENT_SERVICE_SHUTDOWN")
		return delegation, nil
	}
	go s.watchDelegation(delegation, bot, parent.Provider)
	return delegation, nil
}

// botRoute mirrors the conversation-creation path: a persona's pinned model
// decides the route, and a persona without one inherits the parent's route.
func (s *Service) botRoute(ctx context.Context, bot *Bot, parent *Conversation) (ProviderID, string) {
	if bot != nil && strings.TrimSpace(bot.Model) != "" {
		return BotProvider(bot.Model), strings.TrimSpace(bot.Model)
	}
	if parent != nil {
		return parent.Provider, parent.ModelID
	}
	return DefaultProvider, ""
}

// findBotConversation reuses the most recent thread of that bot in the same
// workspace mode, with the same ordering the rail uses, so delegating twice in
// a row continues one context instead of spawning sibling conversations.
func (s *Service) findBotConversation(ctx context.Context, botID string, mode WorkspaceMode) (*Conversation, error) {
	conversations, err := s.store.ListConversationsByMode(ctx, mode)
	if err != nil {
		return nil, err
	}
	for index := range conversations {
		if conversations[index].BotID == botID {
			return &conversations[index], nil
		}
	}
	return nil, nil
}

// delegationPromptContext keeps the child prompt honest about what it is: a
// bounded subtask whose answer will be quoted back to another bot.
func delegationPromptContext(bot *Bot, task string) string {
	name := "另一个 Bot"
	if bot != nil && strings.TrimSpace(bot.Name) != "" {
		name = bot.Name
	}
	return "你正在接手一项由主 Agent 转交的子任务。请只处理下面的任务本身，不要复述这段说明。\n" +
		"接手方：" + name + "\n" +
		"要求：给出可以直接交回给主 Agent 的结论；需要证据时写明来源；不要编造未执行的操作。\n\n" +
		"子任务：\n" + task
}

func (s *Service) beginDelegationWatch() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.runWG.Add(1)
	return true
}

// watchDelegation polls the durable run row. It deliberately does not listen to
// the event stream: the child may outlive this process, and the run table is
// the only record that survives a restart.
func (s *Service) watchDelegation(delegation *Delegation, bot *Bot, parentProvider ProviderID) {
	defer s.runWG.Done()
	timeout := s.runTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()
	readFailures := 0
	for {
		select {
		case <-ctx.Done():
			_ = s.store.UpdateDelegation(context.Background(), delegation.ID, DelegationFailed, "", "AGENT_DELEGATION_TIMEOUT")
			return
		case <-ticker.C:
			run, err := s.store.GetRun(context.Background(), delegation.ChildRunID)
			if errors.Is(err, ErrRunNotFound) {
				_ = s.store.UpdateDelegation(context.Background(), delegation.ID, DelegationFailed, "", "AGENT_DELEGATION_RUN_LOST")
				s.appendDelegationResult(delegation, bot, parentProvider, "子任务记录已不存在，这次交接没有结果。", "AGENT_DELEGATION_RUN_LOST")
				return
			}
			if err != nil {
				// A transient lock is fine; a persistent read failure is not. Cap
				// it so a broken DB cannot keep a spinner alive forever.
				readFailures++
				if readFailures >= 20 {
					_ = s.store.UpdateDelegation(context.Background(), delegation.ID, DelegationFailed, "", "AGENT_DELEGATION_STATE_UNREADABLE")
					return
				}
				continue
			}
			readFailures = 0
			switch run.Status {
			case DelegationRunning, "":
				continue
			case DelegationCancelled, "interrupted":
				_ = s.store.UpdateDelegation(context.Background(), delegation.ID, DelegationCancelled, "", run.ErrorCode)
				s.appendDelegationResult(delegation, bot, parentProvider, "已取消", run.ErrorCode)
				return
			case DelegationFailed:
				_ = s.store.UpdateDelegation(context.Background(), delegation.ID, DelegationFailed, "", run.ErrorCode)
				s.appendDelegationResult(delegation, bot, parentProvider, "失败："+delegationErrorText(run.ErrorCode), run.ErrorCode)
				return
			case DelegationCompleted:
				result := s.delegationAnswer(delegation.ChildConversationID, delegation.ChildRunID)
				_ = s.store.UpdateDelegation(context.Background(), delegation.ID, DelegationCompleted, result, "")
				s.appendDelegationResult(delegation, bot, parentProvider, result, "")
				return
			}
		}
	}
}

// delegationAnswer reads the answer the child run actually produced. A
// completed run with empty output is reported as such instead of being padded
// with invented text.
func (s *Service) delegationAnswer(conversationID, runID string) string {
	conversation, err := s.store.GetConversation(context.Background(), conversationID)
	if err != nil || conversation == nil {
		return ""
	}
	for index := len(conversation.Messages) - 1; index >= 0; index-- {
		message := conversation.Messages[index]
		if message.RunID == runID && message.Role == "assistant" && strings.TrimSpace(message.Content) != "" {
			return strings.TrimSpace(message.Content)
		}
	}
	return ""
}

func (s *Service) appendDelegationResult(delegation *Delegation, bot *Bot, parentProvider ProviderID, result, errorCode string) {
	if delegation == nil {
		return
	}
	name := "Bot"
	if bot != nil && strings.TrimSpace(bot.Name) != "" {
		name = bot.Name
	}
	body := strings.TrimSpace(result)
	if body == "" {
		if errorCode != "" {
			body = "没有拿到可用结果（" + delegationErrorText(errorCode) + "）。"
		} else {
			body = "任务已结束，但这个 Bot 没有返回文本结果。"
		}
	}
	content := "@" + name + " 交回结果\n\n" + body
	_ = s.store.AppendDelegationResultMessage(context.Background(), delegation.ParentConversationID, parentProvider, content)
}

func delegationErrorText(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return "未知错误"
	}
	return code
}

// CancelDelegation cancels the child run and records the handoff as cancelled.
func (s *Service) CancelDelegation(ctx context.Context, id string) (*Delegation, error) {
	delegation, err := s.store.GetDelegation(ctx, id)
	if err != nil {
		return nil, err
	}
	if delegation.Status != DelegationRunning {
		return delegation, nil
	}
	if _, err := s.CancelRun(ctx, delegation.ChildRunID); err != nil && !errors.Is(err, ErrRunNotFound) {
		return nil, err
	}
	if err := s.store.UpdateDelegation(ctx, id, DelegationCancelled, "", "AGENT_DELEGATION_CANCELLED"); err != nil {
		return nil, err
	}
	return s.store.GetDelegation(ctx, id)
}

// recoverDelegations runs once at startup, after MarkInterrupted closed the
// runs. It records an honest note in each parent transcript so the handoff does
// not look like it is still in progress.
func (s *Service) recoverDelegations() error {
	ctx := context.Background()
	interrupted, err := s.store.RecoverInterruptedDelegations(ctx, "AGENT_DELEGATION_INTERRUPTED")
	if err != nil {
		return err
	}
	for index := range interrupted {
		delegation := interrupted[index]
		bot, _ := s.store.GetBot(ctx, delegation.ToBotID)
		provider := DefaultProvider
		if parent, parentErr := s.store.GetConversation(ctx, delegation.ParentConversationID); parentErr == nil {
			provider = parent.Provider
		}
		s.appendDelegationResult(&delegation, bot, provider, "上次交接在应用重启时中断，需要重新派一次。", "AGENT_DELEGATION_INTERRUPTED")
	}
	return nil
}

func (s *Service) GetDelegation(ctx context.Context, id string) (*Delegation, error) {
	return s.store.GetDelegation(ctx, id)
}

func (s *Service) ListDelegations(ctx context.Context, conversationID string) ([]Delegation, error) {
	if strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("delegation list requires a conversation")
	}
	return s.store.ListDelegations(ctx, conversationID, 20)
}
