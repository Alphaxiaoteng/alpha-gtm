package db_test

import (
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestUpdateAccountAvatarFallbackPersistsNonSecretReference(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("youtube", "频道", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountAvatarFallback(account.ID, "https://cdn.example.com/avatar.png", "2026-07-31T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Avatar != "https://cdn.example.com/avatar.png" ||
		updated.AvatarSource != db.AvatarSourcePlatformFallback ||
		updated.AvatarRefreshedAt != "2026-07-31T12:00:00Z" {
		t.Fatalf("updated=%+v", updated)
	}
}
