//go:build legacy_chromium

package ui

import (
	"context"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/logincheck"
	"alpha-nexus-egolite/internal/selectors"
	"github.com/chromedp/cdproto/network"
)

type VerifyResult struct {
	LoggedIn          bool   `json:"loggedIn"`
	VerificationState string `json:"verificationState"`
	AccountID         string `json:"accountId,omitempty"`
	AccountURL        string `json:"accountUrl,omitempty"`
	Name              string `json:"name,omitempty"`
	Avatar            string `json:"avatar,omitempty"`
	PageTitle         string `json:"pageTitle"`
	PageURL           string `json:"pageUrl"`
}

// verifyLogin is retained for CLI/UI compatibility. It is now strictly
// passive: logincheck attaches to an existing platform target and never
// navigates, creates, focuses, or closes a user tab.
func verifyLogin(ctx context.Context, account *db.Account, cdpPort int) (*VerifyResult, error) {
	observation, err := logincheck.ObservePassive(ctx, account.PlatformCode, cdpPort)
	if err != nil {
		return nil, err
	}
	return &VerifyResult{
		LoggedIn:          observation.AuthState == logincheck.AuthLoggedIn,
		VerificationState: verificationStateForObservation(observation.AuthState),
		AccountID:         observation.AccountIdentifier,
		Name:              observation.Name,
		PageURL:           observation.PageOrigin,
	}, nil
}

func platformURLs(platformCode string) []string {
	catalog, err := selectors.Default()
	if err != nil {
		return nil
	}
	return catalog.URLs(platformCode)
}

func isPassiveLoggedIn(cookies []*network.Cookie, platform string) bool {
	if len(cookies) == 0 {
		return false
	}
	names := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie != nil {
			names = append(names, cookie.Name)
		}
	}
	catalog, err := selectors.Default()
	return err == nil && catalog.CookiesIndicateLogin(platform, names)
}

func verifyLoginPassive(ctx context.Context, account *db.Account, cdpPort int) (*VerifyResult, error) {
	return verifyLogin(ctx, account, cdpPort)
}

func isLoggedIn(catalog *selectors.Catalog, platformCode string, cookies []*network.Cookie, _, _ string) bool {
	names := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie != nil {
			names = append(names, cookie.Name)
		}
	}
	return catalog != nil && catalog.CookiesIndicateLogin(platformCode, names)
}
