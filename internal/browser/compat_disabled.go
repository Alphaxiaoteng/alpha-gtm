//go:build !legacy_chromium

package browser

import (
	"context"
	"errors"
	"fmt"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
)

const LegacyChromiumDisabledCode = "LEGACY_CHROMIUM_DISABLED"

var ErrLegacyChromiumDisabled = errors.New(LegacyChromiumDisabledCode)

type State string

const (
	StateStarting State = "starting"
	StateReady    State = "ready"
	StateBusy     State = "busy"
	StateDegraded State = "degraded"
	StateStopping State = "stopping"
	StateCrashed  State = "crashed"
	StateStopped  State = "stopped"
)

type Instance struct {
	ProfileID   string
	AccountID   int64
	PID         int
	CDPPort     int
	CDPURL      string
	State       State
	StartedAt   time.Time
	LastSeenAt  time.Time
	ActiveJobID *int64
	LeaseOwner  string
}

type LaunchConfig struct {
	ProfileID       string
	AccountID       int64
	UserDataDir     string
	FingerprintSeed int
	Platform        string
	PlatformVersion string
	Brand           string
	BrandVersion    string
	HWConcurrency   int
	GPUVendor       string
	GPURenderer     string
	Lang            string
	AcceptLang      string
	Timezone        string
	BinPath         string
	Engine          string
	EgoLitePath     string
	BrowserOSPath   string
	LoginURL        string
	PreferredPort   int
	ProxyServer     string
	WindowBounds    *DisplayBounds
	Background      bool
	HolderType      LeaseHolderType
	HolderID        string
}

type RuntimeSnapshot struct {
	UpdatedAt string            `json:"updated_at"`
	Instances []InstanceSummary `json:"instances"`
}

type InstanceSummary struct {
	ProfileID   string `json:"profile_id"`
	AccountID   int64  `json:"account_id"`
	Platform    string `json:"platform,omitempty"`
	State       string `json:"state"`
	CDPPort     int    `json:"cdp_port"`
	CDPURL      string `json:"cdp_url"`
	PID         int    `json:"pid"`
	StartedAt   string `json:"started_at"`
	LastSeenAt  string `json:"last_seen_at"`
	LeaseOwner  string `json:"lease_owner,omitempty"`
	ActiveJobID *int64 `json:"active_job_id,omitempty"`
}

type BrowserManager struct{}

type Status struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid,omitempty"`
	Profile string `json:"profile"`
	CDPURL  string `json:"cdp_url,omitempty"`
	CDPPort int    `json:"cdp_port"`
}

func NewManager(_, _ string, _ int) *BrowserManager { return &BrowserManager{} }

func (*BrowserManager) Start(context.Context, string) (*Status, error) {
	return nil, ErrLegacyChromiumDisabled
}

func (*BrowserManager) Stop() error { return ErrLegacyChromiumDisabled }

func (*BrowserManager) Status(profileName string) (*Status, error) {
	return &Status{Profile: profileName}, ErrLegacyChromiumDisabled
}

func ProfileExists(_, _ string) bool { return false }

type Space struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ProfileID    string `json:"profile_id"`
	Kind         string `json:"kind"`
	Ownership    string `json:"ownership"`
	State        string `json:"state"`
	ControlEpoch int    `json:"control_epoch"`
	CreatedAt    int64  `json:"created_at"`
}

type SpaceManager struct {
	facade *BrowserFacade
	db     *db.DB
}

const DefaultMaxConcurrentBrowsers = PortMax - PortMin + 1

type FacadeConfig struct {
	BrowserPath           string
	EgoLitePath           string
	BrowserOSPath         string
	DefaultEngine         string
	DataDir               string
	MaxConcurrentBrowsers int
	displayProvider       func() ([]DisplayBounds, error)
}

type StartOptions struct {
	HolderType LeaseHolderType
	HolderID   string
	SkipFocus  bool
}

type AccountStatus struct {
	AccountID  int64  `json:"account_id"`
	ProfileID  string `json:"profile_id"`
	Running    bool   `json:"running"`
	State      State  `json:"state"`
	PID        int    `json:"pid,omitempty"`
	CDPPort    int    `json:"cdp_port,omitempty"`
	CDPURL     string `json:"cdp_url,omitempty"`
	LeaseOwner string `json:"lease_owner,omitempty"`
}

type BrowserFacade struct {
	sup *Supervisor
	cfg FacadeConfig
}

func NewFacade(_ *db.DB, cfg FacadeConfig) *BrowserFacade {
	if cfg.MaxConcurrentBrowsers <= 0 {
		cfg.MaxConcurrentBrowsers = DefaultMaxConcurrentBrowsers
	}
	return &BrowserFacade{sup: &Supervisor{}, cfg: cfg}
}

func NewFacadeFromConfig(database *db.DB, cfg config.Config, dataDir string) *BrowserFacade {
	return NewFacade(database, FacadeConfig{
		BrowserPath:   cfg.BrowserPath,
		EgoLitePath:   cfg.EgoLitePath,
		DefaultEngine: cfg.BrowserEngine,
		DataDir:       dataDir,
	})
}

func (f *BrowserFacade) Supervisor() *Supervisor { return f.sup }
func (*BrowserFacade) Shutdown()                 {}
func (*BrowserFacade) Detach()                   {}

func (*BrowserFacade) CreateSpace(_, _, _, _ string) (*Space, error) {
	return nil, ErrLegacyChromiumDisabled
}

type SpaceControl struct{}

func (*BrowserFacade) NewSpaceControl(string) *SpaceControl { return &SpaceControl{} }
func (*SpaceControl) Handoff(string) error                  { return ErrLegacyChromiumDisabled }
func (*SpaceControl) Resume(string) error                   { return ErrLegacyChromiumDisabled }
func (*BrowserFacade) StartSocketServer() error             { return ErrLegacyChromiumDisabled }

func (f *BrowserFacade) SetBrowserPath(path string)   { f.cfg.BrowserPath = path }
func (f *BrowserFacade) SetEgoLitePath(path string)   { f.cfg.EgoLitePath = path }
func (f *BrowserFacade) SetBrowserOSPath(path string) { f.cfg.BrowserOSPath = path }
func (f *BrowserFacade) SetDefaultEngine(engine string) {
	f.cfg.DefaultEngine = NormalizeEngine(engine)
}
func (f *BrowserFacade) SetMaxConcurrentBrowsers(n int) {
	if n <= 0 {
		n = DefaultMaxConcurrentBrowsers
	}
	f.cfg.MaxConcurrentBrowsers = n
}
func (f *BrowserFacade) MaxConcurrentBrowsers() int { return f.cfg.MaxConcurrentBrowsers }

func ProfileIDForAccount(accountID int64) string { return fmt.Sprintf("acct-%d", accountID) }

func (*BrowserFacade) LaunchConfigFromProfile(int64) (LaunchConfig, error) {
	return LaunchConfig{}, ErrLegacyChromiumDisabled
}
func (*BrowserFacade) StartAccount(int64, StartOptions) (*Instance, error) {
	return nil, ErrLegacyChromiumDisabled
}
func (*BrowserFacade) StopAccount(int64) error { return ErrLegacyChromiumDisabled }
func (*BrowserFacade) Status(accountID int64) (*AccountStatus, error) {
	return &AccountStatus{AccountID: accountID, ProfileID: ProfileIDForAccount(accountID), State: StateStopped}, ErrLegacyChromiumDisabled
}
func (*BrowserFacade) ListRunning() []*Instance { return nil }
func (*BrowserFacade) Focus(int64) error        { return ErrLegacyChromiumDisabled }
func (*BrowserFacade) AcquireLease(int64, LeaseHolderType, string, time.Duration) error {
	return ErrLegacyChromiumDisabled
}
func (*BrowserFacade) ReleaseLease(int64, string) error { return ErrLegacyChromiumDisabled }
func (*BrowserFacade) Reconcile()                       {}

type Supervisor struct{}

func NewSupervisor(*db.DB, string, string) *Supervisor { return &Supervisor{} }
func (*Supervisor) Open(LaunchConfig) (*Instance, error) {
	return nil, ErrLegacyChromiumDisabled
}
func (*Supervisor) Close(string) error                        { return ErrLegacyChromiumDisabled }
func (*Supervisor) Focus(string) error                        { return ErrLegacyChromiumDisabled }
func (*Supervisor) FocusWithInitialPage(string, string) error { return ErrLegacyChromiumDisabled }
func (*Supervisor) Reconcile()                                {}
func (*Supervisor) CountRunning() int                         { return 0 }
func (*Supervisor) MarkDegraded(string, string)               {}
func (*Supervisor) List() []*Instance                         { return nil }
func (*Supervisor) Get(string) (*Instance, bool)              { return nil, false }
func (*Supervisor) Health(string) (State, error)              { return StateStopped, ErrLegacyChromiumDisabled }
func (*Supervisor) Detach()                                   {}
func (*Supervisor) Shutdown()                                 {}
func (*Supervisor) Leases() *LeaseManager                     { return nil }
func (*Supervisor) SetBrowserPath(string)                     {}
func (*Supervisor) ArrangeWindows(string) error               { return ErrLegacyChromiumDisabled }

func FocusByPID(int) error                             { return ErrLegacyChromiumDisabled }
func ResizeAndMoveByPID(int, int, int, int, int) error { return ErrLegacyChromiumDisabled }
func GetAvailableDisplays() ([]DisplayBounds, error)   { return nil, ErrLegacyChromiumDisabled }
func GetScreenResolution() (int, int, error)           { return 0, 0, ErrLegacyChromiumDisabled }
