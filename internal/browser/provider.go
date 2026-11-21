package browser

import (
	"strings"

	"alpha-nexus-egolite/internal/errors"
)

// ProfileProvider is the AgentPush BrowserProfileProvider surface (AP-301).
// Alpha-Nexus implements it via BrowserFacade + fingerprint-chromium.
type ProfileProvider interface {
	ProviderName() string
	Launch(accountID int64, opts StartOptions) (*AccountStatus, error)
	Stop(accountID int64) error
	Health(accountID int64) (*AccountStatus, error)
}

// FacadeProvider adapts BrowserFacade to ProfileProvider.
type FacadeProvider struct {
	Facade *BrowserFacade
	Name   string
}

func (p *FacadeProvider) ProviderName() string {
	if p.Name == "" {
		return "fingerprint-chromium"
	}
	return p.Name
}

func (p *FacadeProvider) Launch(accountID int64, opts StartOptions) (*AccountStatus, error) {
	_, err := p.Facade.StartAccount(accountID, opts)
	// ALREADY_RUNNING is a soft success for provider consumers.
	if err != nil && !errors.Is(err.Error(), errors.ALREADY_RUNNING) && !strings.Contains(err.Error(), string(errors.ALREADY_RUNNING)) {
		return nil, err
	}
	return p.Facade.Status(accountID)
}

func (p *FacadeProvider) Stop(accountID int64) error {
	return p.Facade.StopAccount(accountID)
}

func (p *FacadeProvider) Health(accountID int64) (*AccountStatus, error) {
	return p.Facade.Status(accountID)
}
