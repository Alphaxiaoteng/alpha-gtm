package skilllib

import (
	"context"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestBuiltInEgoBrowserSkillIsReviewedAndRegistryBound(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	library, err := New(database, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	skill, err := library.Get(context.Background(), "ego-browser")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Source != SourceSystem || skill.Status != StatusReviewed || skill.Runtime != RuntimeInstructions || !skill.Enabled {
		t.Fatalf("skill=%+v", skill)
	}
	for _, forbidden := range []string{"cookie", "cdp", "playwright", "shell"} {
		if strings.Contains(strings.ToLower(skill.Instructions), forbidden) {
			t.Fatalf("built-in instructions expose forbidden capability %q", forbidden)
		}
	}
}
