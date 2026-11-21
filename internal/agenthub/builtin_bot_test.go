package agenthub

import (
	"context"
	"testing"
)

// countBots reports how many persona rows exist and how many of them hold the
// primary slot, which is the invariant the builtin seed must respect.
func countBots(t *testing.T, store *Store) (total, primary int) {
	t.Helper()
	if err := store.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN is_primary<>0 THEN 1 ELSE 0 END),0) FROM agent_bots`).
		Scan(&total, &primary); err != nil {
		t.Fatal(err)
	}
	return total, primary
}

func TestEnsureBuiltinAlphaAgentCreatesPrimaryRow(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.EnsureBuiltinAlphaAgent(ctx); err != nil {
		t.Fatal(err)
	}
	total, primary := countBots(t, store)
	if total != 1 || primary != 1 {
		t.Fatalf("total=%d primary=%d want 1/1", total, primary)
	}
	bot, err := store.GetBot(ctx, BuiltinAlphaAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if bot.Name != builtinAlphaAgentName || bot.GroupName != "" || bot.Avatar != "" ||
		bot.Model != builtinAlphaAgentModel || bot.Kickoff == "" || bot.Instructions == "" {
		t.Fatalf("builtin row does not match the contract: %+v", bot)
	}
	if !bot.IsPrimary || bot.Pinned || bot.Muted {
		t.Fatalf("builtin flags wrong: pinned=%v primary=%v muted=%v", bot.Pinned, bot.IsPrimary, bot.Muted)
	}
	if bot.Capabilities == nil || len(bot.Capabilities) != 0 {
		t.Fatalf("capabilities must be an empty array: %+v", bot.Capabilities)
	}
	if bot.CreatedAt == "" || bot.UpdatedAt != bot.CreatedAt {
		t.Fatalf("timestamps wrong: %+v", bot)
	}
	// The row must stay writable through the normal update path, so the values
	// inserted here have to satisfy normalizeBot's constraints too.
	if err := ValidateBotModel(bot.Model); err != nil {
		t.Fatalf("builtin model rejected by validation: %v", err)
	}
	if BotProvider(bot.Model) != ProviderCustom {
		t.Fatalf("builtin model must route to the custom provider: %s", BotProvider(bot.Model))
	}
}

func TestEnsureBuiltinAlphaAgentIsIdempotent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.EnsureBuiltinAlphaAgent(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := store.GetBot(ctx, BuiltinAlphaAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureBuiltinAlphaAgent(ctx); err != nil {
		t.Fatal(err)
	}
	total, primary := countBots(t, store)
	if total != 1 || primary != 1 {
		t.Fatalf("second ensure changed the roster: total=%d primary=%d", total, primary)
	}
	second, err := store.GetBot(ctx, BuiltinAlphaAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if second.CreatedAt != first.CreatedAt || second.UpdatedAt != first.UpdatedAt {
		t.Fatalf("idempotent ensure rewrote the row: first=%+v second=%+v", first, second)
	}
}

func TestEnsureBuiltinAlphaAgentKeepsUserEdits(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.EnsureBuiltinAlphaAgent(ctx); err != nil {
		t.Fatal(err)
	}
	name := "我的总控"
	model := "codex/gpt-5.6-sol"
	instructions := "只做本地检索。"
	if _, err := store.UpdateBot(ctx, BuiltinAlphaAgentID, BotPatch{Name: &name, Model: &model, Instructions: &instructions}); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureBuiltinAlphaAgent(ctx); err != nil {
		t.Fatal(err)
	}
	bot, err := store.GetBot(ctx, BuiltinAlphaAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if bot.Name != name || bot.Model != model || bot.Instructions != instructions {
		t.Fatalf("ensure overwrote user edits: %+v", bot)
	}
	if total, _ := countBots(t, store); total != 1 {
		t.Fatalf("ensure created a duplicate row: total=%d", total)
	}
}

func TestEnsureBuiltinAlphaAgentDoesNotStealPrimary(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	other, err := store.CreateBot(ctx, Bot{Name: "团队总控", IsPrimary: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureBuiltinAlphaAgent(ctx); err != nil {
		t.Fatal(err)
	}
	bot, err := store.GetBot(ctx, BuiltinAlphaAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if bot.IsPrimary {
		t.Fatalf("builtin bot must not take the primary slot from %s", other.ID)
	}
	total, primary := countBots(t, store)
	if total != 2 || primary != 1 {
		t.Fatalf("total=%d primary=%d want 2/1", total, primary)
	}
	stillPrimary, err := store.GetBot(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stillPrimary.IsPrimary {
		t.Fatalf("existing primary was demoted: %+v", stillPrimary)
	}
}

func TestNewServiceSeedsBuiltinAlphaAgent(t *testing.T) {
	store := testStore(t)
	registry, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, registry, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bots, err := service.ListBots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 1 || bots[0].ID != BuiltinAlphaAgentID || !bots[0].IsPrimary {
		t.Fatalf("service roster missing the builtin main bot: %+v", bots)
	}
}
