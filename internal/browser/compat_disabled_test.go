//go:build !legacy_chromium

package browser

import (
	"context"
	"testing"
)

func TestLegacyChromiumCompatibilityMethodsFailClosed(t *testing.T) {
	want := "LEGACY_CHROMIUM_DISABLED"

	manager := NewManager("/bin/false", t.TempDir(), 15101)
	if _, err := manager.Start(context.Background(), "account-1"); err == nil || err.Error() != want {
		t.Fatalf("manager start error=%v want=%s", err, want)
	}

	facade := NewFacade(nil, FacadeConfig{BrowserPath: "/bin/false", DataDir: t.TempDir()})
	if _, err := facade.StartAccount(1, StartOptions{}); err == nil || err.Error() != want {
		t.Fatalf("facade start error=%v want=%s", err, want)
	}
	if err := facade.Focus(1); err == nil || err.Error() != want {
		t.Fatalf("facade focus error=%v want=%s", err, want)
	}
	if got := facade.ListRunning(); len(got) != 0 {
		t.Fatalf("disabled facade reported running browsers: %+v", got)
	}
	if err := FocusByPID(1); err == nil || err.Error() != want {
		t.Fatalf("process focus error=%v want=%s", err, want)
	}
}
