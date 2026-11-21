// Package openmausbot is a read-only client for a locally running OpenMausBot
// harness. It exists as its own package because internal/agenthub imports
// internal/tools (broker.go), so a tools.Registry handler cannot import
// agenthub without closing an import cycle. agenthub re-exports these types as
// OpenMausBotClient / ProbeResult / MausBot.
package openmausbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The harness serves a small local HTTP API. This client uses the read surface
// only: GET /api/health to prove the harness is alive and GET /api/bots to list
// the roster. Writes need a pairing code and answer 403 for anything that is
// not the desktop app or a paired device, so no write helper exists here — and
// nothing polls: a caller asks, gets an answer, done.
const (
	// DefaultBaseURL is the desktop harness's first-choice port. A fallback
	// port (18799, 28799) is the caller's explicit choice.
	DefaultBaseURL = "http://127.0.0.1:8799"
	DefaultTimeout = 8 * time.Second
	// bodyLimit keeps a local server answering with something other than the
	// documented JSON from being read into memory unbounded.
	bodyLimit = 8 << 20
	// detailLimit bounds the refusal text quoted back to a caller.
	detailLimit = 512
)

// Read-side error codes are stable so callers branch on them, never on prose.
const (
	CodeUnreachable  = "OPENMAUSBOT_UNREACHABLE"
	CodeTimeout      = "OPENMAUSBOT_TIMEOUT"
	CodeAuthRequired = "OPENMAUSBOT_AUTH_REQUIRED"
	CodeBadResponse  = "OPENMAUSBOT_UNEXPECTED_RESPONSE"
)

// Error carries the stable code plus the HTTP status it came from.
type Error struct {
	Code   string
	Status int
	Detail string
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("%s: %s", e.Code, e.Detail)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", e.Code, e.Status, e.Detail)
}

// Client is a read-only handle on a running harness. SessionToken is optional:
// the desktop build trusts loopback for reads, while a packaged build or a
// shared server asks for a paired bearer token. The token is never logged and
// never returned in a result.
type Client struct {
	BaseURL      string
	Timeout      time.Duration
	SessionToken string
}

// ProbeResult is the same small, visible shape agenthub.ProviderProbe uses: a
// reachability verdict and a short reason, never credentials or a payload.
type ProbeResult struct {
	BaseURL   string `json:"base_url"`
	Available bool   `json:"available"`
	Ready     bool   `json:"ready"`
	App       string `json:"app,omitempty"`
	Version   string `json:"version,omitempty"`
	Reason    string `json:"reason,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	CheckedAt string `json:"checked_at,omitempty"`
}

// ModelSelection is the engine/model pair a bot currently runs.
type ModelSelection struct {
	InstanceID string `json:"instanceId,omitempty"`
	Model      string `json:"model,omitempty"`
	Variant    string `json:"variant,omitempty"`
}

// Bot is one roster entry from GET /api/bots. The harness also returns each
// bot's `soul` (a whole standing prompt, kilobytes long), its task list and a
// message page; those stay out of this struct so a roster read stays small and
// does not copy another bot's instructions around. SoulHash is kept because it
// shows profile drift without exposing the text.
type Bot struct {
	ID             string         `json:"id"`
	ThreadID       string         `json:"threadId,omitempty"`
	MainThreadID   string         `json:"mainThreadId,omitempty"`
	Name           string         `json:"name"`
	Title          string         `json:"title,omitempty"`
	Description    string         `json:"description,omitempty"`
	Section        string         `json:"section,omitempty"`
	Cwd            string         `json:"cwd,omitempty"`
	Color          string         `json:"color,omitempty"`
	Activity       string         `json:"activity,omitempty"`
	ApprovalMode   string         `json:"approvalMode,omitempty"`
	ModelSelection ModelSelection `json:"modelSelection,omitempty"`
	Busy           bool           `json:"busy,omitempty"`
	ChiefOfStaff   bool           `json:"chiefOfStaff,omitempty"`
	Hidden         bool           `json:"hidden,omitempty"`
	Unread         bool           `json:"unread,omitempty"`
	SoulDrift      bool           `json:"soulDrift,omitempty"`
	SoulHash       string         `json:"soulHash,omitempty"`
	CreatedAt      int64          `json:"createdAt,omitempty"`
}

// Probe asks the documented health endpoint whether this is OpenMausBot.
// It returns both a description and an error: on failure the ProbeResult still
// says which code applied, so a caller can report OPENMAUSBOT_AUTH_REQUIRED
// instead of guessing.
func (c Client) Probe(ctx context.Context) (ProbeResult, error) {
	result := ProbeResult{BaseURL: c.Endpoint(), CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	var health struct {
		App     string `json:"app"`
		Version string `json:"version"`
	}
	if err := c.get(ctx, "/api/health", nil, &health); err != nil {
		result.Reason = err.Error()
		result.ErrorCode = CodeOf(err, CodeUnreachable)
		// A refusal here means even the health read is gated, which only
		// happens when loopback trust is off; /api/health itself needs no token.
		result.Available = result.ErrorCode == CodeAuthRequired
		return result, err
	}
	result.App = health.App
	result.Version = health.Version
	if strings.TrimSpace(health.App) != "openmausbot" {
		result.Reason = fmt.Sprintf("answered, but this is not OpenMausBot (%q)", health.App)
		result.ErrorCode = CodeBadResponse
		return result, &Error{Code: CodeBadResponse, Detail: result.Reason}
	}
	result.Available = true
	result.Ready = true
	return result, nil
}

// ListBots reads the roster. messages=0 is the documented way to ask for the
// bots without a message page attached.
func (c Client) ListBots(ctx context.Context) ([]Bot, error) {
	var payload struct {
		Bots []Bot `json:"bots"`
	}
	query := url.Values{"messages": []string{"0"}}
	if err := c.get(ctx, "/api/bots", query, &payload); err != nil {
		return nil, err
	}
	if payload.Bots == nil {
		return []Bot{}, nil
	}
	return payload.Bots, nil
}

// Endpoint is the origin this client actually talks to, with the default
// filled in, so a result can say where its answer came from.
func (c Client) Endpoint() string {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return DefaultBaseURL
	}
	return base
}

func (c Client) get(ctx context.Context, path string, query url.Values, target interface{}) error {
	endpoint := c.Endpoint() + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return &Error{Code: CodeUnreachable, Detail: err.Error()}
	}
	request.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(c.SessionToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: c.timeout()}
	response, err := client.Do(request)
	if err != nil {
		var timeout interface{ Timeout() bool }
		code := CodeUnreachable
		if errors.As(err, &timeout) && timeout.Timeout() {
			code = CodeTimeout
		}
		return &Error{Code: code, Detail: err.Error()}
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return &Error{
			Code:   CodeAuthRequired,
			Status: response.StatusCode,
			Detail: "the harness wants a paired session; pair it in OpenMausBot (Settings → Phone) and set OPENMAUSBOT_TOKEN — " + refusalText(response.Body),
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &Error{
			Code:   CodeBadResponse,
			Status: response.StatusCode,
			Detail: refusalText(response.Body),
		}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, bodyLimit)).Decode(target); err != nil {
		return &Error{
			Code:   CodeBadResponse,
			Status: response.StatusCode,
			Detail: "response was not the documented JSON: " + err.Error(),
		}
	}
	return nil
}

func (c Client) timeout() time.Duration {
	if c.Timeout <= 0 {
		return DefaultTimeout
	}
	return c.Timeout
}

// refusalText quotes a bounded, whitespace-collapsed slice of a refusal body.
func refusalText(body io.Reader) string {
	raw, _ := io.ReadAll(io.LimitReader(body, detailLimit))
	text := strings.Join(strings.Fields(string(raw)), " ")
	if text == "" {
		return "no explanation given"
	}
	return text
}

// CodeOf reads the stable code back out of a client error, falling back for
// errors that did not come from this client.
func CodeOf(err error, fallback string) string {
	var clientError *Error
	if errors.As(err, &clientError) && clientError.Code != "" {
		return clientError.Code
	}
	return fallback
}
