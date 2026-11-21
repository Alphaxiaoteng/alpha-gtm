package egolite

import (
	"context"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/notify"
)

type fakeSessionChecker struct {
	mu      sync.Mutex
	results map[int64]SessionCheck
	calls   []int64
	active  int
	max     int
}

func (checker *fakeSessionChecker) CheckAccountSession(_ context.Context, account db.Account) (SessionCheck, error) {
	checker.mu.Lock()
	checker.active++
	if checker.active > checker.max {
		checker.max = checker.active
	}
	checker.calls = append(checker.calls, account.ID)
	result := checker.results[account.ID]
	checker.active--
	checker.mu.Unlock()
	return result, nil
}

type fakeStateNotifier struct {
	mu      sync.Mutex
	changes []notify.AccountStateChange
}

func (notifier *fakeStateNotifier) NotifyAccountChange(change notify.AccountStateChange) error {
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	notifier.changes = append(notifier.changes, change)
	return nil
}

func TestSessionMonitorIsSerialConservativeAndNotifiesOnlyTransitions(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	accounts, err := database.ConfigureIdentityPlatforms(1, []string{"douyin", "youtube", "xhs"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(1, 7001, "task-monitor", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpdateEgoSpaceStateForIdentity(1, "agentDelegatedToUser", "manual_takeover", "CHECKPOINT_REQUIRED"); err != nil {
		t.Fatal(err)
	}
	for _, account := range accounts {
		state := db.AccountAuthLoggedIn
		if account.PlatformCode == "youtube" {
			state = db.AccountAuthLoggedOut
		}
		if err := database.UpdateAccountLoginState(account.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	checker := &fakeSessionChecker{results: map[int64]SessionCheck{}}
	for _, account := range accounts {
		switch account.PlatformCode {
		case "douyin":
			checker.results[account.ID] = SessionCheck{AuthState: db.AccountAuthUnknown}
		case "youtube":
			checker.results[account.ID] = SessionCheck{
				AuthState: db.AccountAuthLoggedIn, Name: "Alpha Channel",
				AccountIdentifier: "UC-verified", ReasonCode: db.LoginReasonVerifiedProfileDOM,
				EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
				AvatarURL:    "https://cdn.example.com/youtube.png?token=secret",
			}
		case "xhs":
			checker.results[account.ID] = SessionCheck{
				AuthState: db.AccountAuthLoggedOut, ReasonCode: db.LoginReasonLoginRequiredPage,
				EvidenceCode: db.LoginEvidenceExplicitLoginDOM,
			}
		}
	}
	notifier := &fakeStateNotifier{}
	monitor := NewSessionMonitor(database, checker, notifier)
	monitor.now = func() time.Time { return time.Date(2026, 7, 31, 3, 0, 0, 0, time.UTC) }

	result, err := monitor.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Checked != 3 || result.Unknown != 1 || result.LoggedIn != 1 || result.LoggedOut != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if checker.max != 1 {
		t.Fatalf("monitor concurrency=%d, want 1", checker.max)
	}
	for _, account := range accounts {
		updated, err := database.GetAccount(account.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch account.PlatformCode {
		case "douyin":
			if updated.LoginState != db.AccountAuthLoggedIn {
				t.Fatalf("unknown demoted account to %s", updated.LoginState)
			}
		case "youtube":
			if updated.LoginState != db.AccountAuthLoggedIn || updated.Name != "Alpha Channel" ||
				updated.AccountIdentifier != "UC-verified" || updated.Avatar != "https://cdn.example.com/youtube.png" {
				t.Fatalf("recovered profile not persisted: %+v", updated)
			}
		case "xhs":
			if updated.LoginState != db.AccountAuthLoggedOut {
				t.Fatalf("explicit login page state=%s", updated.LoginState)
			}
		}
	}
	if len(notifier.changes) != 2 {
		t.Fatalf("notification transitions=%d, want 2", len(notifier.changes))
	}
	space, err := database.GetEgoSpaceByIdentity(1)
	if err != nil {
		t.Fatal(err)
	}
	if space.ErrorCode != "" || space.Ownership != "agentDelegatedToUser" || space.State != "manual_takeover" {
		t.Fatalf("resolved checkpoint must clear only its stale error: %+v", space)
	}
}

func TestSessionMonitorSchedulerWaitsForFirstInterval(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	checker := &fakeSessionChecker{results: map[int64]SessionCheck{}}
	monitor := NewSessionMonitor(database, checker, &fakeStateNotifier{})
	tick := make(chan time.Time, 1)
	monitor.after = func(time.Duration) <-chan time.Time { return tick }
	ctx, cancel := context.WithCancel(context.Background())
	var waitGroup sync.WaitGroup
	monitor.Start(ctx, &waitGroup)
	time.Sleep(20 * time.Millisecond)
	if len(checker.calls) != 0 {
		t.Fatal("scheduler ran immediately at startup")
	}
	tick <- time.Now()
	time.Sleep(20 * time.Millisecond)
	cancel()
	waitGroup.Wait()
	settings, err := database.EgoSessionMonitorSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.LastFinishedAt == "" {
		t.Fatal("scheduled run did not finish")
	}
}

func TestSessionMonitorSchedulerCatchesUpWhenIntervalOverdue(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	// Restarts more frequent than the interval must not push the next run
	// another full interval away: an overdue monitor catches up shortly
	// after boot instead of waiting a whole interval again.
	now := time.Date(2026, 9, 10, 17, 0, 0, 0, time.UTC)
	if _, err := database.UpdateEgoSessionMonitor(true, 1440); err != nil {
		t.Fatal(err)
	}
	if _, err := database.BeginEgoSessionMonitorRun(now.Add(-48 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := database.FinishEgoSessionMonitorRun(now.Add(-48*time.Hour), "", &SessionMonitorResult{}); err != nil {
		t.Fatal(err)
	}
	checker := &fakeSessionChecker{results: map[int64]SessionCheck{}}
	monitor := NewSessionMonitor(database, checker, &fakeStateNotifier{})
	monitor.now = func() time.Time { return now }
	durations := make(chan time.Duration, 4)
	tick := make(chan time.Time, 1)
	monitor.after = func(delay time.Duration) <-chan time.Time {
		durations <- delay
		return tick
	}
	ctx, cancel := context.WithCancel(context.Background())
	var waitGroup sync.WaitGroup
	monitor.Start(ctx, &waitGroup)
	select {
	case delay := <-durations:
		if delay <= 0 || delay >= 1440*time.Minute {
			t.Fatalf("overdue scheduler armed %v, want a short catch-up delay below the interval", delay)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler never armed a timer")
	}
	tick <- time.Now()
	time.Sleep(20 * time.Millisecond)
	cancel()
	waitGroup.Wait()
}
