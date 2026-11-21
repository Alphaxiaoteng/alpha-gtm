package egolite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

// scriptedSpaceRunner answers the runtime programs used by the Space binding
// guard. membership is what listTaskSpaces returns; every program is recorded
// so a test can prove nothing was rebuilt when the Space was still live.
type scriptedSpaceRunner struct {
	mu          sync.Mutex
	membership  []int64
	newSpaceID  int64
	boundListed bool
	exists      bool
	programs    []string
}

func (runner *scriptedSpaceRunner) Run(_ context.Context, program string, result any) error {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.programs = append(runner.programs, program)
	if strings.Contains(program, "const boundSpace =") {
		// bindIdentitySpace: succeeds only when the script marks the new Space
		// as bound, so a re-bind failure is a real failure and not a stub.
		if !runner.boundListed {
			return fmt.Errorf("EGO_SPACE_BINDING_MISSING")
		}
		payload := map[string]any{"id": runner.newSpaceID, "taskId": "alpha-nexus/matrix-1", "name": "alpha-nexus/matrix-1", "ownership": "agent"}
		raw, _ := json.Marshal(payload)
		return json.Unmarshal(raw, result)
	}
	listing := make([]map[string]any, 0, len(runner.membership)+1)
	for _, id := range runner.membership {
		listing = append(listing, map[string]any{"id": id, "ownership": "agentDelegatedToUser"})
	}
	if runner.exists && runner.newSpaceID > 0 {
		listing = append(listing, map[string]any{"id": runner.newSpaceID, "ownership": "agent"})
	}
	raw, _ := json.Marshal(map[string]any{"spaces": listing})
	return json.Unmarshal(raw, result)
}

func (runner *scriptedSpaceRunner) listTaskSpaceCalls() int {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	count := 0
	for _, program := range runner.programs {
		if strings.Contains(program, "await listTaskSpaces()") {
			count++
		}
	}
	return count
}

func newBindingTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestRebindMissingRuntimeSpaceKeepsLiveBinding(t *testing.T) {
	database := newBindingTestDB(t)
	identity, err := database.CreateEgoIdentity("空间仍在")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := database.CreateEgoSpaceForIdentity(identity.ID, 50, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1")
	if err != nil {
		t.Fatal(err)
	}
	runner := &scriptedSpaceRunner{membership: []int64{50}, newSpaceID: 77, boundListed: true, exists: true}
	service := NewSpaceService(database, runner)
	got, err := service.rebindMissingRuntimeSpace(context.Background(), identity.ID, bound)
	if err != nil || got == nil || got.SpaceID != 50 {
		t.Fatalf("live binding changed: got=%+v err=%v", got, err)
	}
	if calls := runner.listTaskSpaceCalls(); calls != 1 {
		t.Fatalf("expected exactly one listTaskSpaces read, got %d", calls)
	}
	stored, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil || stored.SpaceID != 50 {
		t.Fatalf("stored binding changed while live: %+v err=%v", stored, err)
	}
}

func TestRebindMissingRuntimeSpaceReportsRebindFailedWhenRebindFails(t *testing.T) {
	database := newBindingTestDB(t)
	identity, err := database.CreateEgoIdentity("空间已失效")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := database.CreateEgoSpaceForIdentity(identity.ID, 50, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1")
	if err != nil {
		t.Fatal(err)
	}
	runner := &scriptedSpaceRunner{newSpaceID: 77, boundListed: false}
	service := NewSpaceService(database, runner)
	_, err = service.rebindMissingRuntimeSpace(context.Background(), identity.ID, bound)
	if err == nil || err.Error() != ErrSpaceRebindFailed {
		t.Fatalf("expected %s, got %v", ErrSpaceNotFound, err)
	}
	stored, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil || stored.SpaceID != 50 {
		t.Fatalf("a failed re-bind must not rewrite the binding: %+v err=%v", stored, err)
	}
}

func TestRebindMissingRuntimeSpaceRebindsVanishedSpace(t *testing.T) {
	database := newBindingTestDB(t)
	identity, err := database.CreateEgoIdentity("空间被重建")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := database.CreateEgoSpaceForIdentity(identity.ID, 50, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1")
	if err != nil {
		t.Fatal(err)
	}
	runner := &scriptedSpaceRunner{newSpaceID: 77, boundListed: true, exists: true}
	service := NewSpaceService(database, runner)
	got, err := service.rebindMissingRuntimeSpace(context.Background(), identity.ID, bound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SpaceID != 77 {
		t.Fatalf("expected the rebuilt Space 77, got %d", got.SpaceID)
	}
	stored, err := database.GetEgoSpaceByIdentity(identity.ID)
	if err != nil || stored.SpaceID != 77 {
		t.Fatalf("re-built Space was not persisted: %+v err=%v", stored, err)
	}
}

func TestRuntimeExecutionErrorSeparatesMissingSpaceFromMissingBinding(t *testing.T) {
	if got := runtimeExecutionError("Error: task space not found"); got != ErrSpaceNotFound {
		t.Fatalf("space not found mapped to %q", got)
	}
	if got := runtimeExecutionError("Error: EGO_SPACE_BINDING_MISSING"); got != ErrSpaceBindingMissing {
		t.Fatalf("binding missing mapped to %q", got)
	}
}

// A runtime answer without a Space listing is not evidence that the Space is
// gone. Rebuilding on it would create a second Space for the same identity, so
// the binding must be kept and the caller's own program reports the truth.
func TestRebindKeepsBindingWhenRuntimeDoesNotListSpaces(t *testing.T) {
	database := newBindingTestDB(t)
	identity, err := database.CreateEgoIdentity("列表不可读")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := database.CreateEgoSpaceForIdentity(identity.ID, 50, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1")
	if err != nil {
		t.Fatal(err)
	}
	runner := &unlistedSpaceRunner{}
	service := NewSpaceService(database, runner)
	got, err := service.rebindMissingRuntimeSpace(context.Background(), identity.ID, bound)
	if err != nil {
		t.Fatalf("unreadable listing must not fail the binding: %v", err)
	}
	if got == nil || got.SpaceID != 50 {
		t.Fatalf("binding rewritten without evidence: %+v", got)
	}
	if runner.bindCalls != 0 {
		t.Fatalf("a rebuild ran without evidence of disappearance: %d", runner.bindCalls)
	}
	if runner.probes != 1 {
		t.Fatalf("probes=%d, want exactly one", runner.probes)
	}
}

// unlistedSpaceRunner answers the membership probe with no `spaces` key at all,
// and would notice if a rebuild program were started.
type unlistedSpaceRunner struct {
	probes    int
	bindCalls int
}

func (runner *unlistedSpaceRunner) Run(_ context.Context, program string, result any) error {
	if strings.Contains(program, "const profileResult = await listProfiles()") {
		runner.bindCalls++
		return fmt.Errorf("unexpected rebuild")
	}
	runner.probes++
	raw, _ := json.Marshal(map[string]any{"done": true})
	return json.Unmarshal(raw, result)
}

// The membership probe is only readable when it emits the runtime result
// prefix. Without it the real runtime returns no payload and every Space reads
// as invalid output, which is how a broken probe once looked like a vanished
// Space in production while the test fakes still passed.
func TestSpaceProbeProgramEmitsRunnableResult(t *testing.T) {
	database := newBindingTestDB(t)
	identity, err := database.CreateEgoIdentity("探测程序")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(identity.ID, 50, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &programCapturingRunner{spaces: []int64{50}}
	service := NewSpaceService(database, runner)
	if _, err := service.EnsureRuntimeSpaceBinding(context.Background(), identity.ID); err != nil {
		t.Fatal(err)
	}
	program := runner.probeProgram()
	if program == "" {
		t.Fatal("no membership probe was run")
	}
	if !strings.Contains(program, resultPrefix) {
		t.Fatalf("probe does not emit %q, so the runtime returns no payload: %s", resultPrefix, program)
	}
	if !strings.Contains(program, "await listTaskSpaces()") {
		t.Fatalf("probe does not list Spaces: %s", program)
	}
}

type programCapturingRunner struct {
	spaces []int64
	seen   []string
}

func (runner *programCapturingRunner) Run(_ context.Context, program string, result any) error {
	runner.seen = append(runner.seen, program)
	if strings.Contains(program, "const profileResult = await listProfiles()") {
		return fmt.Errorf("unexpected rebuild")
	}
	listing := make([]map[string]any, 0, len(runner.spaces))
	for _, id := range runner.spaces {
		listing = append(listing, map[string]any{"id": id, "ownership": "agent"})
	}
	raw, _ := json.Marshal(map[string]any{"spaces": listing})
	return json.Unmarshal(raw, result)
}

func (runner *programCapturingRunner) probeProgram() string {
	for _, program := range runner.seen {
		if strings.Contains(program, "alpha-space-probe") {
			return program
		}
	}
	return ""
}
