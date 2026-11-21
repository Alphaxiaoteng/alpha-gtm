package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func workspaceActivityStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	return NewStore(database)
}

func TestContextSnapshotWorkspaceModeDefaultsToMedia(t *testing.T) {
	store := workspaceActivityStore(t)
	ctx := context.Background()

	conversation, err := store.CreateConversation(ctx, ProviderCodex, "legacy", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if conversation.WorkspaceMode != WorkspaceModeMedia || conversation.Context.WorkspaceMode != WorkspaceModeMedia || conversation.Context.ProductMode != ProductModeMedia {
		t.Fatalf("conversation mode=%q context mode=%q product mode=%q, want media", conversation.WorkspaceMode, conversation.Context.WorkspaceMode, conversation.Context.ProductMode)
	}

	loaded, err := store.GetConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.WorkspaceMode != WorkspaceModeMedia || loaded.Context.WorkspaceMode != WorkspaceModeMedia || loaded.Context.ProductMode != ProductModeMedia {
		t.Fatalf("loaded mode=%q context mode=%q product mode=%q, want media", loaded.WorkspaceMode, loaded.Context.WorkspaceMode, loaded.Context.ProductMode)
	}
}

func TestConversationsListByWorkspaceModeIsolated(t *testing.T) {
	store := workspaceActivityStore(t)
	ctx := context.Background()
	media, err := store.CreateConversation(ctx, ProviderCodex, "media", "", ContextSnapshot{WorkspaceMode: WorkspaceModeMedia})
	if err != nil {
		t.Fatal(err)
	}
	shop, err := store.CreateConversation(ctx, ProviderCodex, "shop", "", ContextSnapshot{ProductMode: ProductModeShop})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		mode WorkspaceMode
		want string
	}{
		{WorkspaceModeMedia, media.ID},
		{WorkspaceModeShop, shop.ID},
	} {
		conversations, err := store.ListConversationsByMode(ctx, test.mode)
		if err != nil {
			t.Fatal(err)
		}
		if len(conversations) != 1 || conversations[0].ID != test.want {
			t.Fatalf("mode=%q conversations=%+v, want only %s", test.mode, conversations, test.want)
		}
	}

	conversations, err := store.ListConversationsByProvider(ctx, ProviderCodex, WorkspaceModeShop)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversations) != 1 || conversations[0].ID != shop.ID {
		t.Fatalf("provider + mode conversations=%+v, want shop only", conversations)
	}
}

func TestWorkspaceStateIsolatedAndRejectsForeignConversation(t *testing.T) {
	store := workspaceActivityStore(t)
	ctx := context.Background()
	media, err := store.CreateConversation(ctx, ProviderCodex, "media", "", ContextSnapshot{WorkspaceMode: WorkspaceModeMedia})
	if err != nil {
		t.Fatal(err)
	}
	shop, err := store.CreateConversation(ctx, ProviderCodex, "shop", "", ContextSnapshot{WorkspaceMode: WorkspaceModeShop})
	if err != nil {
		t.Fatal(err)
	}

	mediaState, err := store.PutWorkspaceState(ctx, WorkspaceState{
		WorkspaceMode:         WorkspaceModeMedia,
		CurrentConversationID: media.ID,
		Draft:                 "media draft",
		LastModule:            "content",
	})
	if err != nil {
		t.Fatal(err)
	}
	if mediaState.WorkspaceMode != WorkspaceModeMedia || mediaState.CurrentConversationID != media.ID {
		t.Fatalf("saved media state=%+v", mediaState)
	}

	shopState, err := store.PutWorkspaceState(ctx, WorkspaceState{
		WorkspaceMode:         WorkspaceModeShop,
		CurrentConversationID: shop.ID,
		Draft:                 "shop draft",
		LastModule:            "listing",
	})
	if err != nil {
		t.Fatal(err)
	}
	if shopState.WorkspaceMode != WorkspaceModeShop || shopState.CurrentConversationID != shop.ID {
		t.Fatalf("saved shop state=%+v", shopState)
	}

	gotMedia, err := store.GetWorkspaceState(ctx, WorkspaceModeMedia)
	if err != nil {
		t.Fatal(err)
	}
	gotShop, err := store.GetWorkspaceState(ctx, WorkspaceModeShop)
	if err != nil {
		t.Fatal(err)
	}
	if gotMedia.Draft != "media draft" || gotMedia.LastModule != "content" || gotShop.Draft != "shop draft" || gotShop.LastModule != "listing" {
		t.Fatalf("workspace state leaked across modes: media=%+v shop=%+v", gotMedia, gotShop)
	}

	_, err = store.PutWorkspaceState(ctx, WorkspaceState{WorkspaceMode: WorkspaceModeMedia, CurrentConversationID: shop.ID})
	if !errors.Is(err, ErrWorkspaceConversationMode) {
		t.Fatalf("cross-mode conversation error=%v, want ErrWorkspaceConversationMode", err)
	}
}

func TestAgentActivityProjectionOmitsRawSensitivePayload(t *testing.T) {
	store := workspaceActivityStore(t)
	ctx := context.Background()
	conversation, err := store.CreateConversation(ctx, ProviderCodex, "Media run", "", ContextSnapshot{WorkspaceMode: WorkspaceModeMedia})
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(ctx, conversation.ID, ProviderCodex, "", "run", conversation.Context)
	if err != nil {
		t.Fatal(err)
	}
	event, err := store.AppendEvent(ctx, run, ProviderEvent{
		Type: "tool.completed",
		Payload: map[string]any{
			"tool_name":    "media.import",
			"params":       map[string]any{"path": "/Users/albert/private.mp4", "token": "secret-token"},
			"raw_params":   "--path=/Users/albert/private.mp4",
			"access_token": "secret-token",
			"status":       "completed",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	activity := event.AgentActivity()
	if activity.ID != event.ID || activity.RunID != run.ID || activity.Kind != "content" || activity.Status != "completed" || activity.DetailRef == nil || activity.DetailRef.Type != "agent_event" || activity.DetailRef.ID != strconv.FormatInt(event.ID, 10) || activity.OccurredAt == "" {
		t.Fatalf("activity=%+v, want content completed with detail ref", activity)
	}
	encoded, err := json.Marshal(activity)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private.mp4", "secret-token", "raw_params", "access_token", "params"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("activity leaked %q: %s", forbidden, encoded)
		}
	}

	activities, err := store.ListAgentActivities(ctx, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(activities) != 1 || activities[0].DetailRef == nil || activities[0].DetailRef.ID != activity.DetailRef.ID {
		t.Fatalf("persisted activities=%+v, want projected event", activities)
	}
}
