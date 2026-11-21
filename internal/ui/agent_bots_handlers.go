package ui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"alpha-nexus-egolite/internal/agenthub"
)

// agentBotBody is the accepted create payload. It intentionally has no field
// for credentials, executable paths, DOM or tool grants.
type agentBotBody struct {
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Instructions string   `json:"instructions"`
	Model        string   `json:"model"`
	Avatar       string   `json:"avatar"`
	Color        string   `json:"color"`
	Effort       string   `json:"effort"`
	Kickoff      string   `json:"kickoff"`
	Capabilities []string `json:"capabilities"`
	Pinned       bool     `json:"pinned"`
	IsPrimary    bool     `json:"is_primary"`
	GroupName    string   `json:"group_name"`
	Muted        bool     `json:"muted"`
}

// agentBotPatchBody mirrors agenthub.BotPatch; nil means "keep the stored
// value", so an explicit "" clears one column.
type agentBotPatchBody struct {
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

// requireLocalAgentBotRequest enforces loopback + same-origin before any bot
// state is read or written. Cross-site pages get 403, not a 400 hint.
func (s *Server) requireLocalAgentBotRequest(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequest(r) || !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "AGENT_BOT_LOCAL_ONLY", "agent bot API is localhost same-origin only")
		return false
	}
	return true
}

func writeAgentBotError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	code := "AGENT_BOT_INVALID"
	switch {
	case errors.Is(err, agenthub.ErrBotNotFound):
		status, code = http.StatusNotFound, "AGENT_BOT_NOT_FOUND"
	case errors.Is(err, agenthub.ErrBotName):
		status, code = http.StatusBadRequest, "AGENT_BOT_NAME_REQUIRED"
	case errors.Is(err, agenthub.ErrBuiltinBotImmutable):
		// 403 而不是 400：请求本身合法，只是这个目标不允许改动。
		status, code = http.StatusForbidden, "AGENT_BOT_BUILTIN_IMMUTABLE"
	case errors.Is(err, agenthub.ErrInvalidModel):
		status, code = http.StatusBadRequest, "AGENT_MODEL_INVALID"
	case errors.Is(err, agenthub.ErrBotInvalid):
		status, code = http.StatusBadRequest, "AGENT_BOT_INVALID"
	case errors.Is(err, agenthub.ErrConversationNotFound):
		status, code = http.StatusNotFound, "AGENT_CONVERSATION_NOT_FOUND"
	}
	writeDataAPIError(w, status, code, err.Error())
}

func (s *Server) handleAPIAgentBots(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalAgentBotRequest(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		bots, err := s.agentHub.ListBots(r.Context())
		if err != nil {
			writeAgentBotError(w, err)
			return
		}
		writeJSON(w, map[string]any{"bots": bots})
	case http.MethodPost:
		var body agentBotBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeDataAPIError(w, http.StatusBadRequest, "AGENT_BOT_INVALID", "invalid agent bot json")
			return
		}
		bot, err := s.agentHub.CreateBot(r.Context(), agenthub.Bot{
			Name:         strings.TrimSpace(body.Name),
			Role:         body.Role,
			Instructions: body.Instructions,
			Model:        body.Model,
			Avatar:       body.Avatar,
			Color:        body.Color,
			Effort:       body.Effort,
			Kickoff:      body.Kickoff,
			Capabilities: body.Capabilities,
			Pinned:       body.Pinned,
			IsPrimary:    body.IsPrimary,
			GroupName:    body.GroupName,
			Muted:        body.Muted,
		})
		if err != nil {
			writeAgentBotError(w, err)
			return
		}
		writeJSONStatus(w, http.StatusCreated, map[string]any{"bot": bot})
	default:
		writeDataAPIError(w, http.StatusMethodNotAllowed, "AGENT_BOT_METHOD_NOT_ALLOWED", "method not allowed")
	}
}

// handleAPIAgentBotsRail is the message-app rail view: the same roster plus
// each persona's group, mute flag, unread count and last message line, so the
// rail needs one request instead of a bot list and a conversation list.
func (s *Server) handleAPIAgentBotsRail(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalAgentBotRequest(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeDataAPIError(w, http.StatusMethodNotAllowed, "AGENT_BOT_METHOD_NOT_ALLOWED", "method not allowed")
		return
	}
	items, err := s.agentHub.RailBots(r.Context())
	if err != nil {
		writeAgentBotError(w, err)
		return
	}
	// The main Alpha Agent is not a row in agent_bots, so its unread count is
	// aggregated separately and folded into the inbox total.
	agent, err := s.agentHub.RailMainAgent(r.Context())
	if err != nil {
		writeAgentBotError(w, err)
		return
	}
	unreadTotal := agent.Unread
	for _, item := range items {
		unreadTotal += item.Unread
	}
	writeJSON(w, map[string]any{"bots": items, "agent": agent, "unread_total": unreadTotal})
}

func (s *Server) handleAPIAgentBot(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalAgentBotRequest(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeDataAPIError(w, http.StatusBadRequest, "AGENT_BOT_INVALID", "agent bot id is required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		bot, err := s.agentHub.GetBot(r.Context(), id)
		if err != nil {
			writeAgentBotError(w, err)
			return
		}
		writeJSON(w, map[string]any{"bot": bot})
	case http.MethodPut:
		var body agentBotPatchBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeDataAPIError(w, http.StatusBadRequest, "AGENT_BOT_INVALID", "invalid agent bot patch json")
			return
		}
		bot, err := s.agentHub.UpdateBot(r.Context(), id, agenthub.BotPatch{
			Name:         body.Name,
			Role:         body.Role,
			Instructions: body.Instructions,
			Model:        body.Model,
			Avatar:       body.Avatar,
			Color:        body.Color,
			Effort:       body.Effort,
			Kickoff:      body.Kickoff,
			Capabilities: body.Capabilities,
			Pinned:       body.Pinned,
			IsPrimary:    body.IsPrimary,
			GroupName:    body.GroupName,
			Muted:        body.Muted,
		})
		if err != nil {
			writeAgentBotError(w, err)
			return
		}
		writeJSON(w, map[string]any{"bot": bot})
	case http.MethodDelete:
		if err := s.agentHub.DeleteBot(r.Context(), id); err != nil {
			writeAgentBotError(w, err)
			return
		}
		// Conversations that referenced this persona are detached, never deleted.
		writeJSON(w, map[string]any{"deleted": true, "id": id, "conversations_detached": true})
	default:
		writeDataAPIError(w, http.StatusMethodNotAllowed, "AGENT_BOT_METHOD_NOT_ALLOWED", "method not allowed")
	}
}

// handleAPIAgentBotPin toggles one persona's pin. The body must say which way,
// so a malformed request cannot silently unpin a bot.
func (s *Server) handleAPIAgentBotPin(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalAgentBotRequest(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeDataAPIError(w, http.StatusBadRequest, "AGENT_BOT_INVALID", "agent bot id is required")
		return
	}
	var body struct {
		Pinned *bool `json:"pinned"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Pinned == nil {
		writeDataAPIError(w, http.StatusBadRequest, "AGENT_BOT_INVALID", `pin body must be {"pinned":true|false}`)
		return
	}
	bot, err := s.agentHub.UpdateBot(r.Context(), id, agenthub.BotPatch{Pinned: body.Pinned})
	if err != nil {
		writeAgentBotError(w, err)
		return
	}
	writeJSON(w, map[string]any{"bot": bot})
}

// handleAPIAgentBotPrimary promotes one persona and demotes the rest. It takes
// no body: the target id is the whole request.
func (s *Server) handleAPIAgentBotPrimary(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalAgentBotRequest(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeDataAPIError(w, http.StatusBadRequest, "AGENT_BOT_INVALID", "agent bot id is required")
		return
	}
	// Optional body: {"primary": false} hands the slot back to Alpha Agent.
	var body struct {
		Primary *bool `json:"primary"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Primary != nil && !*body.Primary {
		if err := s.agentHub.ClearPrimaryBots(r.Context()); err != nil {
			writeAgentBotError(w, err)
			return
		}
		writeJSON(w, map[string]any{"cleared": true})
		return
	}
	bot, err := s.agentHub.SetPrimaryBot(r.Context(), id)
	if err != nil {
		writeAgentBotError(w, err)
		return
	}
	writeJSON(w, map[string]any{"bot": bot})
}

// agentBotMemoryBody is the editable memory textarea. The field is named
// "memory" so a GET response can be PUT back unchanged.
type agentBotMemoryBody struct {
	Memory string `json:"memory"`
}

// handleAPIAgentBotMemory reads, replaces or clears one persona's Markdown
// memory file. The bot row must exist first: memory is never an orphan file
// created by a typo in the URL.
// handleAPIAgentConversationsReadAll clears the unread badge of one rail row.
// The body's bot_id is optional: empty means the main-agent rows, which are the
// legacy conversations without a bot_id.
func (s *Server) handleAPIAgentConversationsReadAll(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeAgentHubJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "AGENT_METHOD_NOT_ALLOWED"})
		return
	}
	var body struct {
		BotID string `json:"bot_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeAgentHubJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid read-all json"})
		return
	}
	changed, err := s.agentHub.MarkAllConversationsRead(r.Context(), strings.TrimSpace(body.BotID))
	if err != nil {
		writeAgentHubError(w, err)
		return
	}
	writeAgentHubJSON(w, http.StatusOK, map[string]any{"updated": changed})
}

func (s *Server) handleAPIAgentBotMemory(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalAgentBotRequest(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeDataAPIError(w, http.StatusBadRequest, "AGENT_BOT_INVALID", "agent bot id is required")
		return
	}
	bot, err := s.agentHub.GetBot(r.Context(), id)
	if err != nil {
		writeAgentBotError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		memory, err := s.agentHub.ReadBotMemory(r.Context(), bot.ID)
		if err != nil {
			writeAgentBotError(w, err)
			return
		}
		writeJSON(w, map[string]any{"memory": memory, "bot_id": bot.ID})
	case http.MethodPut:
		var body agentBotMemoryBody
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&body); err != nil {
			writeDataAPIError(w, http.StatusBadRequest, "AGENT_BOT_INVALID", "invalid agent bot memory json")
			return
		}
		if err := s.agentHub.WriteBotMemory(r.Context(), bot.ID, body.Memory); err != nil {
			writeAgentBotError(w, err)
			return
		}
		memory, err := s.agentHub.ReadBotMemory(r.Context(), bot.ID)
		if err != nil {
			writeAgentBotError(w, err)
			return
		}
		writeJSON(w, map[string]any{"memory": memory, "bot_id": bot.ID})
	case http.MethodDelete:
		if err := s.agentHub.ClearBotMemory(r.Context(), bot.ID); err != nil {
			writeAgentBotError(w, err)
			return
		}
		writeJSON(w, map[string]any{"cleared": true, "memory": "", "bot_id": bot.ID})
	default:
		writeDataAPIError(w, http.StatusMethodNotAllowed, "AGENT_BOT_METHOD_NOT_ALLOWED", "method not allowed")
	}
}
