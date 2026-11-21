//go:build legacy_chromium

package publishguard

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/browser"
)

// LiveCDPURL accepts only the target account's running BrowserFacade status.
func LiveCDPURL(accountID int64, status *browser.AccountStatus, statusErr error) (string, error) {
	if accountID <= 0 || statusErr != nil || status == nil || !status.Running {
		return "", ErrBrowserNotRunning
	}
	if status.State == browser.StateDegraded {
		return "", ErrBrowserDegraded
	}
	if status.AccountID != accountID {
		return "", ErrBrowserAccountMismatch
	}
	cdpURL := strings.TrimSpace(status.CDPURL)
	if status.CDPPort > 0 && cdpURL == "" {
		cdpURL = fmt.Sprintf("http://127.0.0.1:%d", status.CDPPort)
	}
	if cdpURL == "" {
		return "", ErrBrowserCDPUnavailable
	}
	parsed, err := url.Parse(cdpURL)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Hostname() == "" || parsed.Port() == "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrBrowserCDPInvalid
	}
	host := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", ErrBrowserCDPInvalid
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port <= 0 || port > 65535 || (status.CDPPort > 0 && port != status.CDPPort) {
		return "", ErrBrowserCDPInvalid
	}
	return cdpURL, nil
}
