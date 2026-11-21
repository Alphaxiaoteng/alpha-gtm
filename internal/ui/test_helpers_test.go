package ui

import (
	"context"
	"testing"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/observability"
)

type captureObserverEmitter struct {
	events []observability.Event
}

func (e *captureObserverEmitter) Emit(_ context.Context, event observability.Event) error {
	e.events = append(e.events, event)
	return nil
}

func newObserverTestDB(t *testing.T) (*db.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	return database, dataDir
}
