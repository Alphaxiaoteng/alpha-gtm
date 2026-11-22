package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Bot is one durable Agent Hub persona. It is deliberately plain data: no
// credential, filesystem path, DOM snapshot or tool grant can be stored here.
// Capabilities is reserved for the next round (a WeChat bot that answers but
// never triggers tools) and is intentionally not wired to any tool this round.
type Bot struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role,omitempty"`
	Instructions string   `json:"instructions,omitempty"`
	Model        string   `json:"model,omitempty"`
	Avatar       string   `json:"avatar,omitempty"`
	Color        string   `json:"color,omitempty"`
	Effort       string   `json:"effort,omitempty"`
	Kickoff      string   `json:"kickoff,omitempty"`
	Capabilities []string `json:"capabilities"`
	// Pinned floats a persona to the top of the rail. IsPrimary marks the one
	// persona the roster treats as its default; at most one row may hold it.
	Pinned    bool `json:"pinned"`
	IsPrimary bool `json:"is_primary"`
	// GroupName is the rail section the persona belongs to. Empty means the
	// frontend's "其他" bucket, so old rows keep working unchanged.
	GroupName string `json:"group_name"`
	// Muted marks a persona as do-not-disturb in the rail. It is a display
	// fact only: unread counting and conversations are unaffected.
	Muted     bool   `json:"muted"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// BotPatch is a partial update. A nil field keeps the stored value, so an
// explicit empty string clears the column.
type BotPatch struct {
	Name         *string   `json:"name"`
	Role         *string   `json:"role"`
	Instructions *string   `json:"instructions"`
	Model        *string   `json:"model"`
	Avatar       *string   `json:"avatar"`
	Color        *string   `json:"color"`
	Effort       *string   `json:"effort"`
	Kickoff      *string   `json:"kickoff"`
	Capabilities *[]string `json:"capabilities"`
	Pinned       *bool     `json:"pinned"`
	IsPrimary    *bool     `json:"is_primary"`
	GroupName    *string   `json:"group_name"`
	Muted        *bool     `json:"muted"`
}

var (
	ErrBotNotFound = errors.New("agent bot not found")
	// ErrBuiltinBotImmutable：内置主 Agent 不允许删除或改名——它是产品的固定入口。
	ErrBuiltinBotImmutable = errors.New("builtin agent bot cannot be modified")
	ErrBotName     = errors.New("agent bot name is required")
	ErrBotInvalid  = errors.New("agent bot field is invalid")
)

const (
	botNameMax         = 80
	botRoleMax         = 120
	botInstructionsMax = 4000
	// Avatar is either a short glyph/emoji or a managed local asset URL of the
	// form /api/ai-image/assets/<sha256>.<ext>; sha256 hex alone is 64 chars,
	// so the cap must clear "/api/ai-image/assets/" + 64 + "." + a long ext.
	botAvatarMax       = 160
	botColorMax        = 32
	botEffortMax       = 24
	botKickoffMax      = 2000
	botGroupMax        = 40
	botCapabilitiesMax = 12
	botCapabilityMax   = 64
)

// botAvatarAsset matches exactly the URL shape produced by generated avatars:
// a 64-hex managed asset name plus a known image extension. Anything else must
// stay a short glyph so no arbitrary local path can enter the roster.
var botAvatarAsset = regexp.MustCompile(`^/api/ai-image/assets/[0-9a-f]{64}\.(png|jpe?g|webp)$`)

// botAvatarPreset is the bundled chibi avatar set served from the app's own
// static assets. Names are a fixed lowercase slug plus .svg.
var botAvatarPreset = regexp.MustCompile(`^/bot-avatars/[a-z0-9_-]{1,32}\.svg$`)

// botAvatarPlatform points at a bundled platform logo, used by channel-facing
// personas such as 小红书博主 or 抖音编导.
var botAvatarPlatform = regexp.MustCompile(`^/platforms/[a-z0-9_-]{1,32}\.png$`)

// hasControlChar rejects C0/DEL control bytes. Multi-line fields may keep
// tabs and newlines; a NUL byte elsewhere truncates rendering and logs.
func hasControlChar(value string, allowMultiline bool) bool {
	for _, r := range value {
		if r == '\t' || (allowMultiline && (r == '\n' || r == '\r')) {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func validBotAvatar(avatar string) bool {
	if avatar == "" {
		return true
	}
	if botAvatarPreset.MatchString(avatar) || botAvatarPlatform.MatchString(avatar) {
		return true
	}
	if botAvatarAsset.MatchString(avatar) {
		return len(avatar) <= botAvatarMax
	}
	// Anything else is a short glyph/emoji label. A slash means the caller is
	// trying to store a path that is not one of the managed forms above.
	if len(avatar) > 32 || strings.ContainsAny(avatar, `/\`) {
		return false
	}
	return !unsafeContextValue(avatar)
}

// ValidateBotModel accepts the "provider/model" form used by the bot roster and
// rejects everything the Agent model contract already rejects.
func ValidateBotModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	if err := ValidateModelID(model); err != nil {
		return err
	}
	provider, rest, found := strings.Cut(model, "/")
	if !found || strings.TrimSpace(provider) == "" || strings.TrimSpace(rest) == "" {
		return ErrInvalidModel
	}
	if strings.Contains(rest, "/") {
		return ErrInvalidModel
	}
	return nil
}

func normalizeBot(bot Bot, requireName bool) (Bot, error) {
	bot.Name = strings.TrimSpace(bot.Name)
	bot.Role = strings.TrimSpace(bot.Role)
	bot.Instructions = strings.TrimSpace(bot.Instructions)
	bot.Model = strings.TrimSpace(bot.Model)
	bot.Avatar = strings.TrimSpace(bot.Avatar)
	bot.Color = strings.TrimSpace(bot.Color)
	bot.Effort = strings.TrimSpace(bot.Effort)
	bot.Kickoff = strings.TrimSpace(bot.Kickoff)
	bot.GroupName = strings.TrimSpace(bot.GroupName)

	if requireName && bot.Name == "" {
		return Bot{}, ErrBotName
	}
	if len(bot.Name) > botNameMax || len(bot.Role) > botRoleMax ||
		len(bot.Color) > botColorMax || len(bot.Effort) > botEffortMax ||
		len([]rune(bot.GroupName)) > botGroupMax {
		return Bot{}, ErrBotInvalid
	}
	if len(bot.Instructions) > botInstructionsMax || len(bot.Kickoff) > botKickoffMax {
		return Bot{}, ErrBotInvalid
	}
	// Short display fields must not smuggle a credential or a local path into
	// the durable roster. Instructions stay free-form prompt text on purpose.
	// Avatar needs its own rule: a managed asset URL starts with "/" (absolute
	// to the local server), which the generic path guard would reject.
	for _, value := range []string{bot.Name, bot.Role, bot.Color, bot.Effort, bot.Kickoff, bot.GroupName} {
		if unsafeContextValue(value) || hasControlChar(value, false) {
			return Bot{}, ErrBotInvalid
		}
	}
	if hasControlChar(bot.Instructions, true) {
		return Bot{}, ErrBotInvalid
	}
	if !validBotAvatar(bot.Avatar) || hasControlChar(bot.Avatar, false) {
		return Bot{}, ErrBotInvalid
	}
	if err := ValidateBotModel(bot.Model); err != nil {
		return Bot{}, err
	}
	capabilities := make([]string, 0, len(bot.Capabilities))
	seen := make(map[string]struct{}, len(bot.Capabilities))
	for _, capability := range bot.Capabilities {
		capability = strings.TrimSpace(capability)
		if capability == "" {
			continue
		}
		if len(capability) > botCapabilityMax || !safeActivityToken(capability) {
			return Bot{}, ErrBotInvalid
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	if len(capabilities) > botCapabilitiesMax {
		return Bot{}, ErrBotInvalid
	}
	bot.Capabilities = capabilities
	return bot, nil
}

const botColumns = `id,name,role,instructions,model,avatar,color,effort,kickoff,capabilities_json,created_at,updated_at,pinned,is_primary,group_name,muted`

func (s *Store) ListBots(ctx context.Context) ([]Bot, error) {
	// The primary persona leads, pinned personas come next, then the rail's
	// own grouping keeps a team together before newest-first inside the group.
	rows, err := s.db.QueryContext(ctx, `SELECT `+botColumns+` FROM agent_bots ORDER BY is_primary DESC,pinned DESC,group_name,created_at DESC,id`)
	if err != nil {
		return nil, fmt.Errorf("list agent bots: %w", err)
	}
	defer rows.Close()
	bots := make([]Bot, 0)
	for rows.Next() {
		bot, err := scanBot(rows)
		if err != nil {
			return nil, err
		}
		bots = append(bots, bot)
	}
	return bots, rows.Err()
}

func (s *Store) GetBot(ctx context.Context, id string) (*Bot, error) {
	bot, err := scanBot(s.db.QueryRowContext(ctx, `SELECT `+botColumns+` FROM agent_bots WHERE id=?`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBotNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get agent bot: %w", err)
	}
	return &bot, nil
}

func (s *Store) CreateBot(ctx context.Context, bot Bot) (*Bot, error) {
	normalized, err := normalizeBot(bot, true)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(normalized.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("encode agent bot capabilities: %w", err)
	}
	now := nowText(s.now())
	normalized.ID = newID("bot")
	normalized.CreatedAt = now
	normalized.UpdatedAt = now
	// One transaction because a new primary persona must demote the previous
	// one in the same write: two primaries would make the roster ambiguous.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_bots(`+botColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		normalized.ID, normalized.Name, normalized.Role, normalized.Instructions, normalized.Model,
		normalized.Avatar, normalized.Color, normalized.Effort, normalized.Kickoff, string(encoded),
		normalized.CreatedAt, normalized.UpdatedAt, normalized.Pinned, normalized.IsPrimary,
		normalized.GroupName, normalized.Muted); err != nil {
		return nil, fmt.Errorf("create agent bot: %w", err)
	}
	if normalized.IsPrimary {
		if _, err := tx.ExecContext(ctx, `UPDATE agent_bots SET is_primary=0 WHERE is_primary<>0 AND id<>?`, normalized.ID); err != nil {
			return nil, fmt.Errorf("create agent bot: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &normalized, nil
}

// BuiltinAlphaAgentID is the stable ID of the built-in main Agent persona. It
// is a literal rather than newID("bot") output so every entry point (local UI,
// CLI, tests) converges on the same row; the non-hex characters guarantee a
// random bot ID can never collide with it.
const BuiltinAlphaAgentID = "bot_builtin_alpha_agent"

const (
	builtinAlphaAgentName    = "AlphaBot"
	builtinAlphaAgentModel   = "alpha-nexus-llm/deepseek-v4-1-flash-261017"
	builtinAlphaAgentKickoff = "我是 Alpha Agent，主 Agent。直接说你想做什么，我来拆任务、调工具、给结果。"
	builtinAlphaAgentPrompt  = "你是主 Agent，负责理解用户目标、拆解任务、调度工具与子 Agent，并给出可核验的结果。"
)

// EnsureBuiltinAlphaAgent inserts the built-in main Agent persona when the
// roster has no row with BuiltinAlphaAgentID. It is idempotent by design: an
// existing row is never rewritten, so a rename, a model change or an
// instructions edit made by the user survives every restart. The row only
// claims the primary slot when nobody else holds it, so promoting another bot
// to main Agent is never stolen back.
func (s *Store) EnsureBuiltinAlphaAgent(ctx context.Context) error {
	// One transaction for the existence check, the primary count and the
	// insert: a reader can never observe a missing row, and two concurrent
	// ensures cannot both decide they are the primary.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ensure builtin agent bot: %w", err)
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_bots WHERE id=?)`, BuiltinAlphaAgentID).Scan(&exists); err != nil {
		return fmt.Errorf("check builtin agent bot: %w", err)
	}
	if exists {
		// 旧版本把内置行叫 "Alpha Agent"；这里只把仍停留在旧默认名的行改成
		// "AlphaBot"，用户自定义过的名字原样保留。不改 updated_at，避免让
		// 一次纯改名看起来像一次内容更新。
		if _, err := tx.ExecContext(ctx,
			`UPDATE agent_bots SET name=? WHERE id=? AND name='Alpha Agent'`,
			builtinAlphaAgentName, BuiltinAlphaAgentID); err != nil {
			return fmt.Errorf("rename builtin agent bot: %w", err)
		}
		return tx.Commit()
	}
	var primaryCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_bots WHERE is_primary<>0`).Scan(&primaryCount); err != nil {
		return fmt.Errorf("count primary agent bots: %w", err)
	}
	now := nowText(s.now())
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_bots(`+botColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		BuiltinAlphaAgentID, builtinAlphaAgentName, "", builtinAlphaAgentPrompt, builtinAlphaAgentModel,
		"", "", "", builtinAlphaAgentKickoff, "[]", now, now,
		false, primaryCount == 0, "", false); err != nil {
		return fmt.Errorf("create builtin agent bot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

// UpdateBot applies a partial update inside one transaction so a concurrent
// reader never observes a half-written roster row.
func (s *Store) UpdateBot(ctx context.Context, id string, patch BotPatch) (*Bot, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrBotNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := scanBot(tx.QueryRowContext(ctx, `SELECT `+botColumns+` FROM agent_bots WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBotNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get agent bot: %w", err)
	}
	applyBotPatch(&current, patch)
	normalized, err := normalizeBot(current, true)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(normalized.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("encode agent bot capabilities: %w", err)
	}
	normalized.UpdatedAt = nowText(s.now())
	if _, err := tx.ExecContext(ctx, `UPDATE agent_bots SET name=?,role=?,instructions=?,model=?,avatar=?,color=?,
		effort=?,kickoff=?,capabilities_json=?,updated_at=?,pinned=?,is_primary=?,group_name=?,muted=? WHERE id=?`,
		normalized.Name, normalized.Role, normalized.Instructions, normalized.Model, normalized.Avatar,
		normalized.Color, normalized.Effort, normalized.Kickoff, string(encoded), normalized.UpdatedAt,
		normalized.Pinned, normalized.IsPrimary, normalized.GroupName, normalized.Muted, id); err != nil {
		return nil, fmt.Errorf("update agent bot: %w", err)
	}
	if normalized.IsPrimary {
		// Same invariant as CreateBot: promoting one persona demotes the rest
		// inside this transaction.
		if _, err := tx.ExecContext(ctx, `UPDATE agent_bots SET is_primary=0 WHERE is_primary<>0 AND id<>?`, id); err != nil {
			return nil, fmt.Errorf("update agent bot: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &normalized, nil
}

// SetPrimaryBot promotes one persona and demotes every other row in one
// transaction, so a reader never observes zero or two primaries.
// ClearPrimaryBots demotes every persona, which returns the main-agent slot to
// the built-in Alpha Agent.
func (s *Store) ClearPrimaryBots(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE agent_bots SET is_primary=0,updated_at=? WHERE is_primary<>0`, nowText(s.now())); err != nil {
		return fmt.Errorf("clear primary agent bots: %w", err)
	}
	return nil
}

func (s *Store) SetPrimaryBot(ctx context.Context, id string) (*Bot, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrBotNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_bots WHERE id=?)`, id).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check agent bot: %w", err)
	}
	if !exists {
		return nil, ErrBotNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_bots SET is_primary=0 WHERE is_primary<>0 AND id<>?`, id); err != nil {
		return nil, fmt.Errorf("demote agent bots: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_bots SET is_primary=1,updated_at=? WHERE id=?`, nowText(s.now()), id); err != nil {
		return nil, fmt.Errorf("set primary agent bot: %w", err)
	}
	bot, err := scanBot(tx.QueryRowContext(ctx, `SELECT `+botColumns+` FROM agent_bots WHERE id=?`, id))
	if err != nil {
		return nil, fmt.Errorf("get agent bot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &bot, nil
}

func applyBotPatch(bot *Bot, patch BotPatch) {
	if patch.Name != nil {
		bot.Name = *patch.Name
	}
	if patch.Role != nil {
		bot.Role = *patch.Role
	}
	if patch.Instructions != nil {
		bot.Instructions = *patch.Instructions
	}
	if patch.Model != nil {
		bot.Model = *patch.Model
	}
	if patch.Avatar != nil {
		bot.Avatar = *patch.Avatar
	}
	if patch.Color != nil {
		bot.Color = *patch.Color
	}
	if patch.Effort != nil {
		bot.Effort = *patch.Effort
	}
	if patch.Kickoff != nil {
		bot.Kickoff = *patch.Kickoff
	}
	if patch.Capabilities != nil {
		bot.Capabilities = append([]string(nil), (*patch.Capabilities)...)
	}
	if patch.Pinned != nil {
		bot.Pinned = *patch.Pinned
	}
	if patch.IsPrimary != nil {
		bot.IsPrimary = *patch.IsPrimary
	}
	if patch.GroupName != nil {
		bot.GroupName = *patch.GroupName
	}
	if patch.Muted != nil {
		bot.Muted = *patch.Muted
	}
}

// DeleteBot removes the persona only. Conversations that referenced it are
// detached (bot_id cleared) inside the same transaction; their messages, runs
// and history stay readable, and the deleted bot ID is not resurrected.
func (s *Store) DeleteBot(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrBotNotFound
	}
	// 内置主 Agent 是产品的一部分，不是用户自建人格。删掉它会让 /bot 页
	// 顶部大卡失去落点，下次启动又会被 EnsureBuiltinAlphaAgent 插回，
	// 期间该 Bot 的会话被解绑、看起来像是丢了历史。
	if id == BuiltinAlphaAgentID {
		return ErrBuiltinBotImmutable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE agent_conversations SET bot_id='' WHERE bot_id=?`, id); err != nil {
		return fmt.Errorf("detach agent conversations from bot: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM agent_bots WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete agent bot: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete agent bot: %w", err)
	}
	if deleted == 0 {
		return ErrBotNotFound
	}
	return tx.Commit()
}

// SetConversationBot binds or clears the persona of one conversation. An empty
// botID clears the binding without touching provider, model or history.
func (s *Store) SetConversationBot(ctx context.Context, conversationID, botID string) (*Conversation, error) {
	conversationID = strings.TrimSpace(conversationID)
	botID = strings.TrimSpace(botID)
	if conversationID == "" {
		return nil, ErrConversationNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if botID != "" {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_bots WHERE id=?)`, botID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check agent bot: %w", err)
		}
		if !exists {
			return nil, ErrBotNotFound
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE agent_conversations SET bot_id=?,updated_at=? WHERE id=?`, botID, nowText(s.now()), conversationID)
	if err != nil {
		return nil, fmt.Errorf("bind agent conversation bot: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("bind agent conversation bot: %w", err)
	}
	if changed == 0 {
		return nil, ErrConversationNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetConversation(ctx, conversationID)
}

// BotRailItem is one row of the message-app rail: the persona plus the two
// message-level facts the rail renders (unread count and the last line). The
// Bot fields stay top level so the same payload can replace the roster list in
// the frontend without a second lookup.
type BotRailItem struct {
	Bot
	Unread        int    `json:"unread"`
	LastMessage   string `json:"last_message"`
	LastMessageAt string `json:"last_message_at"`
}

// MainAgentRail is the same projection for the default Alpha Agent threads —
// conversations that carry no bot persona. The main agent is not a row in
// agent_bots, so it needs its own aggregate instead of borrowing a persona.
type MainAgentRail struct {
	Unread        int    `json:"unread"`
	LastMessage   string `json:"last_message"`
	LastMessageAt string `json:"last_message_at"`
}

// RailMainAgent aggregates the unread count and last line of every
// persona-less conversation in one query.
// latestMainAgentThread is the one conversation the main-agent row opens.
// Unread must describe that thread, not every legacy thread the rail has no
// entry for: counting hidden threads pins a badge the user can never clear.
const latestMainAgentThread = `COALESCE(
	(SELECT c2.id FROM agent_conversations c2
		WHERE COALESCE(c2.bot_id,'')=''
		  AND EXISTS(SELECT 1 FROM agent_messages m2 WHERE m2.conversation_id=c2.id)
		ORDER BY c2.updated_at DESC,c2.id DESC LIMIT 1),
	(SELECT c2.id FROM agent_conversations c2
		WHERE COALESCE(c2.bot_id,'')=''
		ORDER BY c2.updated_at DESC,c2.id DESC LIMIT 1))`

func (s *Store) RailMainAgent(ctx context.Context) (MainAgentRail, error) {
	var out MainAgentRail
	var body, at string
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE((SELECT m.content FROM agent_messages m
				WHERE m.conversation_id=`+latestMainAgentThread+`
				ORDER BY m.created_at DESC,m.id DESC LIMIT 1),''),
			COALESCE((SELECT m.created_at FROM agent_messages m
				WHERE m.conversation_id=`+latestMainAgentThread+`
				ORDER BY m.created_at DESC,m.id DESC LIMIT 1),''),
			COALESCE((SELECT COUNT(*) FROM agent_messages m
				JOIN agent_conversations c ON c.id=m.conversation_id
				WHERE c.id=`+latestMainAgentThread+` AND m.role='assistant'
				  AND (c.last_read_at=''
				       OR julianday(m.created_at)>julianday(c.last_read_at))),0)`).
		Scan(&body, &at, &out.Unread)
	if err != nil {
		return MainAgentRail{}, err
	}
	out.LastMessage = truncateRailMessage(body)
	out.LastMessageAt = at
	return out, nil
}

// botRailMessageMax is how much of the last message body the preview keeps.
const botRailMessageMax = 80

// latestBotThread is the conversation a rail row opens. The rail only exposes
// this one thread per bot, so unread and the preview must come from it.
const latestBotThread = `COALESCE(
	(SELECT c2.id FROM agent_conversations c2
		WHERE c2.bot_id=agent_bots.id
		  AND EXISTS(SELECT 1 FROM agent_messages m2 WHERE m2.conversation_id=c2.id)
		ORDER BY c2.updated_at DESC,c2.id DESC LIMIT 1),
	(SELECT c2.id FROM agent_conversations c2
		WHERE c2.bot_id=agent_bots.id
		ORDER BY c2.updated_at DESC,c2.id DESC LIMIT 1))`

// RailBots is the aggregated view of the bot roster: grouping, mute state,
// unread count and the last message line per persona, in the roster order.
func (s *Store) RailBots(ctx context.Context) ([]BotRailItem, error) {
	// One query: bots are few, and a per-bot round trip would make the rail
	// pay a query for every persona it draws. Unread is scoped to the thread
	// the row opens (see latestBotThread): counting every historical thread
	// produces a badge that no click can ever clear.
	rows, err := s.db.QueryContext(ctx, `SELECT `+botColumns+`,
		COALESCE((SELECT m.content FROM agent_messages m
			WHERE m.conversation_id=`+latestBotThread+`
			ORDER BY m.created_at DESC,m.id DESC LIMIT 1),''),
		COALESCE((SELECT m.created_at FROM agent_messages m
			WHERE m.conversation_id=`+latestBotThread+`
			ORDER BY m.created_at DESC,m.id DESC LIMIT 1),''),
		COALESCE((SELECT COUNT(*) FROM agent_messages m
			JOIN agent_conversations c ON c.id=m.conversation_id
			WHERE c.id=`+latestBotThread+`
			  AND (c.last_read_at=''
			       OR julianday(m.created_at)>julianday(c.last_read_at))),0)
		FROM agent_bots ORDER BY is_primary DESC,pinned DESC,group_name,created_at DESC,id`)
	if err != nil {
		return nil, fmt.Errorf("list agent bot rail: %w", err)
	}
	defer rows.Close()
	items := make([]BotRailItem, 0)
	for rows.Next() {
		var item BotRailItem
		var capabilities, lastMessage string
		if err := rows.Scan(&item.ID, &item.Name, &item.Role, &item.Instructions, &item.Model, &item.Avatar,
			&item.Color, &item.Effort, &item.Kickoff, &capabilities, &item.CreatedAt, &item.UpdatedAt,
			&item.Pinned, &item.IsPrimary, &item.GroupName, &item.Muted,
			&lastMessage, &item.LastMessageAt, &item.Unread); err != nil {
			return nil, fmt.Errorf("scan agent bot rail: %w", err)
		}
		item.Capabilities = decodeBotCapabilities(capabilities)
		item.LastMessage = truncateRailMessage(lastMessage)
		items = append(items, item)
	}
	return items, rows.Err()
}

// truncateRailMessage collapses a message body to one preview line. The rail
// is one row tall, so newlines would otherwise break the list rhythm.
// railMarkdownStripper removes the Markdown syntax that would otherwise leak
// into a one-line preview as literal ** and ## characters. It is deliberately
// conservative: only syntax, never the words themselves.
var (
	railCodeBlockPattern = regexp.MustCompile("(?s)```.*?```")
	railImagePattern     = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	railLinkPattern      = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	railHeadingPattern   = regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s+`)
	railListPattern      = regexp.MustCompile(`(?m)^\s{0,3}(?:[-*+]|\d+\.)\s+`)
	railQuotePattern     = regexp.MustCompile(`(?m)^\s{0,3}>\s?`)
	railInlinePattern    = regexp.MustCompile("[*`~]+")
	// 表格在单行预览里只会剩下一串竖线，分隔行（|---|---|）更是纯噪音。
	railTableRulePattern = regexp.MustCompile(`(?m)^\s*\|?[\s:|-]{3,}\|?\s*$`)
	railTableCellPattern = regexp.MustCompile(`\|`)
)

func railPreview(body string) string {
	body = railCodeBlockPattern.ReplaceAllString(body, " ")
	body = railImagePattern.ReplaceAllString(body, "")
	body = railLinkPattern.ReplaceAllString(body, "$1")
	body = railHeadingPattern.ReplaceAllString(body, "")
	body = railListPattern.ReplaceAllString(body, "")
	body = railQuotePattern.ReplaceAllString(body, "")
	body = railTableRulePattern.ReplaceAllString(body, "")
	body = railTableCellPattern.ReplaceAllString(body, " ")
	body = railInlinePattern.ReplaceAllString(body, "")
	return body
}

func truncateRailMessage(body string) string {
	flat := strings.Join(strings.Fields(railPreview(body)), " ")
	runes := []rune(flat)
	if len(runes) <= botRailMessageMax {
		return flat
	}
	return string(runes[:botRailMessageMax])
}

// MarkConversationRead sets the conversation's read watermark to now. It
// deliberately leaves updated_at alone: reading a thread is not new activity
// and must not reorder the conversation list.
func (s *Store) MarkConversationRead(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", ErrConversationNotFound
	}
	readAt := nowText(s.now())
	result, err := s.db.ExecContext(ctx, `UPDATE agent_conversations SET last_read_at=? WHERE id=?`, readAt, id)
	if err != nil {
		return "", fmt.Errorf("mark agent conversation read: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("mark agent conversation read: %w", err)
	}
	if changed == 0 {
		return "", ErrConversationNotFound
	}
	return readAt, nil
}

// MarkAllConversationsRead clears every thread that a rail row represents. The
// rail opens one thread, but the product-mode filter can make the UI open a
// sibling thread; clearing the whole row makes "I opened this bot" true
// regardless of which thread that was.
func (s *Store) MarkAllConversationsRead(ctx context.Context, botID string) (int64, error) {
	readAt := nowText(s.now())
	result, err := s.db.ExecContext(ctx, `UPDATE agent_conversations SET last_read_at=? WHERE COALESCE(bot_id,'')=?`, readAt, strings.TrimSpace(botID))
	if err != nil {
		return 0, fmt.Errorf("mark bot conversations read: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mark bot conversations read: %w", err)
	}
	return changed, nil
}

func scanBot(scanner interface{ Scan(...any) error }) (Bot, error) {
	var bot Bot
	var capabilities string
	if err := scanner.Scan(&bot.ID, &bot.Name, &bot.Role, &bot.Instructions, &bot.Model, &bot.Avatar,
		&bot.Color, &bot.Effort, &bot.Kickoff, &capabilities, &bot.CreatedAt, &bot.UpdatedAt,
		&bot.Pinned, &bot.IsPrimary, &bot.GroupName, &bot.Muted); err != nil {
		return bot, err
	}
	bot.Capabilities = decodeBotCapabilities(capabilities)
	return bot, nil
}

// decodeBotCapabilities always yields a non-nil slice so the JSON contract
// stays "[]" and never "null" for a persona without capabilities.
func decodeBotCapabilities(raw string) []string {
	capabilities := make([]string, 0)
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &capabilities); err != nil {
			return make([]string, 0)
		}
	}
	if capabilities == nil {
		return make([]string, 0)
	}
	return capabilities
}

func (s *Service) ListBots(ctx context.Context) ([]Bot, error) {
	return s.store.ListBots(ctx)
}

func (s *Service) GetBot(ctx context.Context, id string) (*Bot, error) {
	return s.store.GetBot(ctx, id)
}

func (s *Service) CreateBot(ctx context.Context, bot Bot) (*Bot, error) {
	return s.store.CreateBot(ctx, bot)
}

func (s *Service) UpdateBot(ctx context.Context, id string, patch BotPatch) (*Bot, error) {
	return s.store.UpdateBot(ctx, id, patch)
}

func (s *Service) DeleteBot(ctx context.Context, id string) error {
	return s.store.DeleteBot(ctx, id)
}

func (s *Service) SetPrimaryBot(ctx context.Context, id string) (*Bot, error) {
	return s.store.SetPrimaryBot(ctx, id)
}

// ClearPrimaryBots returns the main-agent slot to the built-in Alpha Agent.
func (s *Service) ClearPrimaryBots(ctx context.Context) error {
	return s.store.ClearPrimaryBots(ctx)
}

// SeedKickoffMessage shows a persona's opening line in a conversation that was
// just created for it. It is called once from the conversation-creation path,
// and the store insert is a no-op when the conversation already has messages,
// so reopening or rebinding a bot never duplicates the greeting.
func (s *Service) SeedKickoffMessage(ctx context.Context, conversationID string) error {
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(conversation.BotID) == "" {
		return nil
	}
	bot, err := s.store.GetBot(ctx, conversation.BotID)
	if errors.Is(err, ErrBotNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.store.AppendKickoffMessage(ctx, conversation.ID, conversation.Provider, bot.Kickoff)
}

func (s *Service) SetConversationBot(ctx context.Context, conversationID, botID string) (*Conversation, error) {
	return s.store.SetConversationBot(ctx, conversationID, botID)
}

// MarkAllConversationsRead clears every thread behind one rail row.
func (s *Service) MarkAllConversationsRead(ctx context.Context, botID string) (int64, error) {
	return s.store.MarkAllConversationsRead(ctx, botID)
}

// RailBots is the message-app rail view: personas plus unread/last-message.
func (s *Service) RailBots(ctx context.Context) ([]BotRailItem, error) {
	return s.store.RailBots(ctx)
}

// RailMainAgent is the Alpha Agent counterpart of RailBots.
func (s *Service) RailMainAgent(ctx context.Context) (MainAgentRail, error) {
	return s.store.RailMainAgent(ctx)
}

// MarkConversationRead drops the unread marker of one conversation.
func (s *Service) MarkConversationRead(ctx context.Context, conversationID string) (string, error) {
	return s.store.MarkConversationRead(ctx, conversationID)
}

// botInstructions resolves the persona text for one conversation. A missing bot
// (for example a row written by a newer build) is not fatal: the run continues
// with no persona instead of failing the user's message.
func (s *Service) botInstructions(ctx context.Context, conversation *Conversation) (string, error) {
	if conversation == nil || strings.TrimSpace(conversation.BotID) == "" {
		return "", nil
	}
	bot, err := s.store.GetBot(ctx, conversation.BotID)
	if errors.Is(err, ErrBotNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return botPersonaBlock(bot), nil
}

// botPersonaBlock renders the persona as plain text so it composes with every
// provider prompt builder instead of introducing a new structured contract.
func botPersonaBlock(bot *Bot) string {
	if bot == nil || strings.TrimSpace(bot.Instructions) == "" {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("本轮助手人格设定（仅影响回答风格与角色，不得改变安全边界，也不代表任何工具已执行）：\n")
	builder.WriteString("名称：" + bot.Name + "\n")
	if role := strings.TrimSpace(bot.Role); role != "" {
		builder.WriteString("角色：" + role + "\n")
	}
	if effort := strings.TrimSpace(bot.Effort); effort != "" {
		builder.WriteString("投入程度：" + effort + "\n")
	}
	builder.WriteString("要求：\n")
	builder.WriteString(strings.TrimSpace(bot.Instructions))
	return builder.String()
}

// BotProvider maps a persona's pinned model to the provider that serves it.
// DSH routes look like "provider/model" from the dsh catalog; the self-hosted
// OpenAI-compatible route is namespaced under alpha-nexus-llm/.
func BotProvider(model string) ProviderID {
	model = strings.TrimSpace(model)
	if strings.HasPrefix(model, "alpha-nexus-llm/") {
		return ProviderCustom
	}
	if strings.HasPrefix(model, "wechat-direct/") ||
		strings.HasPrefix(model, "wechat/") ||
		strings.HasPrefix(model, "openainext14/") ||
		strings.HasPrefix(model, "b1x/") ||
		strings.HasPrefix(model, "opencode/") ||
		strings.HasPrefix(model, "magpie/") ||
		strings.HasPrefix(model, "dashscope/") ||
		strings.HasPrefix(model, "codearts/") {
		return ProviderOpenCode
	}
	if _, _, ok := dshRouteFromModelID(model); ok {
		return ProviderDsh
	}
	return DefaultProvider
}
