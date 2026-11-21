package egolite

import (
	"context"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

func TestLoginSyncRetryIntervalBacksOffToBoundedPolling(t *testing.T) {
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 15 * time.Second}
	for attempt, expected := range want {
		if got := loginSyncRetryInterval(attempt); got != expected {
			t.Fatalf("attempt=%d interval=%s want=%s", attempt, got, expected)
		}
	}
}

type delayedLoginSyncVerifier struct {
	account   db.Account
	loginAt   int
	callCh    chan int
	mu        sync.Mutex
	callCount int
}

func (verifier *delayedLoginSyncVerifier) VerifyLoginBatch(context.Context, int64, []string) (*LoginVerification, error) {
	verifier.mu.Lock()
	verifier.callCount++
	call := verifier.callCount
	account := verifier.account
	if call >= verifier.loginAt {
		account.LoginState = db.AccountAuthLoggedIn
	}
	verifier.mu.Unlock()
	verifier.callCh <- call
	return &LoginVerification{Accounts: []db.Account{account}}, nil
}

func TestLoginSyncMonitorKeepsPollingAfterSixSecondsWithoutAnotherWake(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	identity, err := database.CreateEgoIdentity("延迟扫码浏览器")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	session, err := database.CreateLoginSession(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateLoginSession(session.ID, "awaiting_login", ""); err != nil {
		t.Fatal(err)
	}

	verifier := &delayedLoginSyncVerifier{account: *account, loginAt: 5, callCh: make(chan int, 8)}
	monitor := NewLoginSyncMonitor(database, verifier)
	ticks := make(chan time.Time, 8)
	durations := make(chan time.Duration, 8)
	monitor.after = func(delay time.Duration) <-chan time.Time {
		durations <- delay
		return ticks
	}
	ctx, cancel := context.WithCancel(context.Background())
	var waitGroup sync.WaitGroup
	monitor.Start(ctx, &waitGroup)

	wantDelays := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}
	for call, wantDelay := range wantDelays {
		select {
		case gotCall := <-verifier.callCh:
			if gotCall != call+1 {
				t.Fatalf("verification call=%d want=%d", gotCall, call+1)
			}
		case <-time.After(time.Second):
			t.Fatalf("verification call %d did not run", call+1)
		}
		select {
		case gotDelay := <-durations:
			if gotDelay != wantDelay {
				t.Fatalf("after call %d delay=%s want=%s", call+1, gotDelay, wantDelay)
			}
		case <-time.After(time.Second):
			t.Fatalf("delay after call %d was not scheduled", call+1)
		}
		ticks <- time.Now()
	}
	select {
	case gotCall := <-verifier.callCh:
		if gotCall != 5 {
			t.Fatalf("verification call=%d want=5", gotCall)
		}
	case <-time.After(time.Second):
		t.Fatal("login completed after the old six-second cutoff but was not detected")
	}
	cancel()
	waitGroup.Wait()
	if active, err := database.ActiveLoginSession(account.ID); err != nil || active != nil {
		t.Fatalf("verified login session remained active: active=%+v err=%v", active, err)
	}
}
