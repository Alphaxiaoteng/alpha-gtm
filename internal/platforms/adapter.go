package platforms

import (
	"context"
	"fmt"
)

// PublishStrategy matches AgentPush adapter strategy (V1 mainly browser).
type PublishStrategy string

const (
	StrategyBrowser PublishStrategy = "browser"
	StrategyAPI     PublishStrategy = "api"
	StrategyHybrid  PublishStrategy = "hybrid"
)

// PublishRequest is the adapter input (content + target asset).
type PublishRequest struct {
	JobID        int64
	PlatformCode string
	ContentID    int64
	ContentType  string
	Title        string
	Caption      string
	Body         string
	TagsJSON     string
	// MediaPath is the primary local media file for upload (optional).
	MediaPath string
	Media     []PublishMedia
	AccountID int64
	AssetID   int64
	AssetName string
	AssetType string
	// AssetExternalID is the public Page / professional-account identifier.
	// Browser adapters use it only to confirm the intended target, never as a credential.
	AssetExternalID          string
	ConnectedAssetName       string
	ConnectedAssetExternalID string
	CDPURL                   string
	DryRun                   bool
	// AutoSubmit requests a publish click when true. Adapters must still
	// return manual_takeover (or similar) if success is uncertain — never
	// report published without a clear success signal.
	AutoSubmit bool
	// SubmitPreparedOnly is limited to a reviewed, still-open creator form.
	// It never refills or uploads; the worker may only revalidate and click
	// the exact final control. It is set by the frozen-plan executor.
	SubmitPreparedOnly bool
	// TemplateID and SettingsJSON are an immutable, safe snapshot of the
	// platform template selected for this job. They never hold credentials.
	TemplateID   int64
	SettingsJSON string
	// ComplianceJSON is the renderer-neutral, immutable compliance_fields
	// array copied from the linked frozen plan target. Legacy jobs leave it
	// empty; adapters must not infer it from mutable live content.
	ComplianceJSON string
	// PublishAt is copied from the reviewed plan as UTC RFC3339. It is only a
	// platform form value and never creates an Alpha-Nexus background timer.
	PublishAt string
	// CredentialRef is reserved for future native API connectors (token not stored).
	// Until those connectors exist, API live runs fail with API_CONNECTOR_UNAVAILABLE.
	CredentialRef string
}

type PublishMedia struct {
	Path     string
	Kind     string
	Role     string
	Position int
	SHA256   string
}

// PublishResult is returned by adapters.
type PublishResult struct {
	Status       string // publish.Status string
	Step         string
	PostURL      string
	ExternalID   string
	ErrorCode    string
	ErrorMessage string
	Screenshot   string
	Message      string
	Evidence     map[string]interface{}
}

// Publisher is the AgentPush PlatformAdapter.publish* surface (Go V1).
type Publisher interface {
	Platform() string
	Strategy() PublishStrategy
	Validate(req PublishRequest) error
	// Publish runs dry-run or browser path. DryRun must not submit.
	Publish(ctx context.Context, req PublishRequest) (*PublishResult, error)
}

var publishers = map[string]Publisher{}

// RegisterPublisher registers a platform publisher (call from init).
func RegisterPublisher(p Publisher) {
	if p == nil {
		return
	}
	publishers[p.Platform()] = p
}

// GetPublisher returns a registered publisher or error.
func GetPublisher(platformCode string) (Publisher, error) {
	p, ok := publishers[platformCode]
	if !ok {
		return nil, fmt.Errorf("no publisher for platform %q", platformCode)
	}
	return p, nil
}
