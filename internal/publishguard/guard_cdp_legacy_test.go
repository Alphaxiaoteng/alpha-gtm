//go:build legacy_chromium

package publishguard

import (
	"errors"
	"testing"

	"alpha-nexus-egolite/internal/browser"
)

func TestLiveCDPURLRequiresRunningMatchingAccountStatus(t *testing.T) {
	tests := []struct {
		name    string
		account int64
		status  *browser.AccountStatus
		want    string
		wantErr error
	}{
		{name: "stopped", account: 7, status: &browser.AccountStatus{AccountID: 7}, wantErr: ErrBrowserNotRunning},
		{name: "degraded", account: 7, status: &browser.AccountStatus{AccountID: 7, Running: true, State: browser.StateDegraded, CDPURL: "http://127.0.0.1:17001"}, wantErr: ErrBrowserDegraded},
		{name: "wrong account", account: 7, status: &browser.AccountStatus{AccountID: 8, Running: true, CDPURL: "http://127.0.0.1:17001"}, wantErr: ErrBrowserAccountMismatch},
		{name: "missing endpoint", account: 7, status: &browser.AccountStatus{AccountID: 7, Running: true}, wantErr: ErrBrowserCDPUnavailable},
		{name: "explicit url", account: 7, status: &browser.AccountStatus{AccountID: 7, Running: true, CDPURL: "http://127.0.0.1:17001"}, want: "http://127.0.0.1:17001"},
		{name: "explicit port", account: 7, status: &browser.AccountStatus{AccountID: 7, Running: true, CDPPort: 17002}, want: "http://127.0.0.1:17002"},
		{name: "localhost", account: 7, status: &browser.AccountStatus{AccountID: 7, Running: true, CDPURL: "http://localhost:17003"}, want: "http://localhost:17003"},
		{name: "ipv6 loopback", account: 7, status: &browser.AccountStatus{AccountID: 7, Running: true, CDPURL: "http://[::1]:17004"}, want: "http://[::1]:17004"},
		{name: "external host", account: 7, status: &browser.AccountStatus{AccountID: 7, Running: true, CDPURL: "http://example.com:17001"}, wantErr: ErrBrowserCDPInvalid},
		{name: "wrong scheme", account: 7, status: &browser.AccountStatus{AccountID: 7, Running: true, CDPURL: "file:///tmp/socket"}, wantErr: ErrBrowserCDPInvalid},
		{name: "credentials", account: 7, status: &browser.AccountStatus{AccountID: 7, Running: true, CDPURL: "http://user:pass@127.0.0.1:17001"}, wantErr: ErrBrowserCDPInvalid},
		{name: "port mismatch", account: 7, status: &browser.AccountStatus{AccountID: 7, Running: true, CDPURL: "http://127.0.0.1:17005", CDPPort: 17006}, wantErr: ErrBrowserCDPInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := LiveCDPURL(test.account, test.status, nil)
			if got != test.want || !errors.Is(err, test.wantErr) {
				t.Fatalf("got=%q err=%v want=%q err=%v", got, err, test.want, test.wantErr)
			}
		})
	}
}
