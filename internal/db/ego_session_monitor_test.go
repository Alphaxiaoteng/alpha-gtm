package db

import (
	"errors"
	"testing"
	"time"
)

func TestEgoSessionMonitorDefaultsUpdateAndCrashRecovery(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	settings, err := database.EgoSessionMonitorSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Enabled || settings.IntervalMinutes != 60 || settings.Concurrency != 1 || settings.State != "idle" {
		t.Fatalf("unexpected defaults: %+v", settings)
	}

	settings, err = database.UpdateEgoSessionMonitor(false, 90)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Enabled || settings.IntervalMinutes != 90 {
		t.Fatalf("unexpected update: %+v", settings)
	}
	if _, err := database.UpdateEgoSessionMonitor(true, 0); err == nil {
		t.Fatal("zero interval was accepted")
	}

	started := time.Date(2026, 7, 31, 2, 0, 0, 0, time.UTC)
	if _, err := database.BeginEgoSessionMonitorRun(started); err != nil {
		t.Fatal(err)
	}
	if _, err := database.BeginEgoSessionMonitorRun(started.Add(time.Minute)); !errors.Is(err, ErrEgoSessionMonitorRunning) {
		t.Fatalf("concurrent run err=%v", err)
	}
	if err := database.RecoverEgoSessionMonitorRun(started.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	recovered, err := database.EgoSessionMonitorSettings()
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != "idle" || recovered.LastErrorCode != "SESSION_MONITOR_INTERRUPTED" {
		t.Fatalf("unexpected recovered state: %+v", recovered)
	}
}
