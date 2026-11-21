package errors_test

import (
	"testing"

	"alpha-nexus-egolite/internal/errors"
)

func TestErrorCodes(t *testing.T) {
	tests := []struct {
		code errors.ErrorCode
		want string
	}{
		{errors.PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1, "PLATFORM_PUBLISH_NOT_SUPPORTED_IN_V1"},
		{errors.LOGIN_REQUIRED, "LOGIN_REQUIRED"},
		{errors.PROFILE_LOCKED, "PROFILE_LOCKED"},
		{errors.CDP_CONNECT, "CDP_CONNECT"},
		{errors.BROWSER_NOT_FOUND, "BROWSER_NOT_FOUND"},
		{errors.INVALID_DATA_DIR, "INVALID_DATA_DIR"},
		{errors.DB_MIGRATION_FAILED, "DB_MIGRATION_FAILED"},
		{errors.PLATFORM_NOT_FOUND, "PLATFORM_NOT_FOUND"},
		{errors.NO_PORT, "NO_PORT"},
		{errors.NO_PORT_AVAILABLE, "NO_PORT_AVAILABLE"},
		{errors.BROWSER_CRASH, "BROWSER_CRASH"},
		{errors.LEASE_EXPIRED, "LEASE_EXPIRED"},
		// alias aligns to BROWSER_CRASH string
		{errors.PROCESS_CRASHED, "BROWSER_CRASH"},
	}

	for _, tt := range tests {
		t.Run(tt.want+"_"+string(tt.code), func(t *testing.T) {
			if got := tt.code.String(); got != tt.want {
				t.Errorf("ErrorCode.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAllMultiBrowserCodes(t *testing.T) {
	want := map[string]bool{
		"PROFILE_LOCKED":    false,
		"NO_PORT":           false,
		"NO_PORT_AVAILABLE": false,
		"BROWSER_CRASH":     false,
		"CDP_CONNECT":       false,
		"LEASE_EXPIRED":     false,
		"BROWSER_NOT_FOUND": false,
	}
	for _, c := range errors.AllMultiBrowserCodes {
		s := c.String()
		if _, ok := want[s]; !ok {
			t.Errorf("unexpected multi-browser code %q", s)
			continue
		}
		want[s] = true
	}
	for code, seen := range want {
		if !seen {
			t.Errorf("missing multi-browser code %q in AllMultiBrowserCodes", code)
		}
	}
}

func TestIs(t *testing.T) {
	if !errors.Is("PROFILE_LOCKED", errors.PROFILE_LOCKED) {
		t.Fatal("exact match")
	}
	if !errors.Is("NO_PORT_AVAILABLE: all ports in use", errors.NO_PORT_AVAILABLE) {
		t.Fatal("prefix with colon")
	}
	if errors.Is("something PROFILE_LOCKED", errors.PROFILE_LOCKED) {
		t.Fatal("should not match mid-string")
	}
	if errors.Is("", errors.CDP_CONNECT) {
		t.Fatal("empty")
	}
}
