//go:build legacy_chromium

package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/logincheck"
	"alpha-nexus-egolite/internal/notify"
)

func TestAccountObserverBatchIsSerialAndContinuesAfterPlatformFailure(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	codes := []string{"xhs", "douyin", "wechat_mp", "wechat_video", "kuaishou", "csdn"}
	ids := make(map[int64]string)
	for _, code := range codes {
		account, err := database.CreateAccount(code, code+"-account", "")
		if err != nil {
			t.Fatal(err)
		}
		ids[account.ID] = code
	}
	var active, maxActive int32
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15100 + int(accountID)}, nil
	})
	observer.observe = func(_ context.Context, platform string, _ int) (*logincheck.Observation, error) {
		current := atomic.AddInt32(&active, 1)
		for {
			previous := atomic.LoadInt32(&maxActive)
			if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
				break
			}
		}
		defer atomic.AddInt32(&active, -1)
		time.Sleep(5 * time.Millisecond)
		if platform == "wechat_video" {
			return nil, context.DeadlineExceeded
		}
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedCookie, EvidenceCode: logincheck.EvidenceReviewedCookie}, nil
	}

	results, err := observer.ObserveRunning(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(codes) || maxActive != 1 {
		t.Fatalf("results=%d maxActive=%d", len(results), maxActive)
	}
	var leaseCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_operation_leases`).Scan(&leaseCount); err != nil {
		t.Fatal(err)
	}
	if leaseCount != 0 {
		t.Fatalf("successful observation batch leaked %d browser operation leases", leaseCount)
	}
	for accountID, platform := range ids {
		events, err := database.ListAccountLoginEvents(accountID, 10)
		if err != nil || len(events) != 1 {
			t.Fatalf("platform=%s events=%+v err=%v", platform, events, err)
		}
		if platform == "wechat_video" {
			if events[0].AuthState != db.AccountAuthUnknown || events[0].ReasonCode != db.LoginReasonCDPUnreachable {
				t.Fatalf("video event=%+v", events[0])
			}
		} else if events[0].AuthState != db.AccountAuthLoggedIn || events[0].LoginMode != db.LoginModeBrowserProfileSession || events[0].AuthMethod != db.AuthMethodUnknown {
			t.Fatalf("platform=%s event=%+v", platform, events[0])
		}
	}
}

func TestAccountObservationLeaseTTLHasSafetyMargin(t *testing.T) {
	// The observer may spend up to 10s enumerating targets plus 6x7s on
	// bounded target inspection. Leave enough room so the lease cannot expire
	// while a normal observation is still using the browser.
	if accountObservationLeaseTTL < 75*time.Second {
		t.Fatalf("observation lease TTL %s has no safety margin", accountObservationLeaseTTL)
	}
}

func TestAccountObserverFastCycleDetectsTransitionsWithoutRepeatedProfileCapture(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("wechat_video", "video fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15180}, nil
	})
	state := logincheck.AuthLoggedIn
	profileCalls := 0
	probeCalls := 0
	observation := func() *logincheck.Observation {
		if state == logincheck.AuthLoggedOut {
			return &logincheck.Observation{AuthState: state, ReasonCode: logincheck.ReasonLoginRequiredPage, EvidenceCode: logincheck.EvidenceExplicitLoginDOM}
		}
		return &logincheck.Observation{
			AuthState: state, ReasonCode: logincheck.ReasonVerifiedProfileDOM,
			EvidenceCode: logincheck.EvidenceReviewedProfileDOM, Name: "real-video-creator",
		}
	}
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		profileCalls++
		return observation(), nil
	}
	observer.probe = func(context.Context, string, int) (*logincheck.Observation, error) {
		probeCalls++
		return observation(), nil
	}

	first, err := observer.ObserveAccount(context.Background(), account.ID)
	if err != nil || first.AuthState != db.AccountAuthLoggedIn || profileCalls != 1 {
		t.Fatalf("first=%+v profileCalls=%d err=%v", first, profileCalls, err)
	}
	if _, err := observer.observeFastCycle(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if probeCalls != 1 || profileCalls != 1 {
		t.Fatalf("unchanged state triggered full profile capture: probes=%d profiles=%d", probeCalls, profileCalls)
	}

	state = logincheck.AuthLoggedOut
	if _, err := observer.observeFastCycle(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	loggedOut, err := database.GetAccount(account.ID)
	if err != nil || loggedOut.LoginState != db.AccountAuthLoggedOut || profileCalls != 2 {
		t.Fatalf("logout account=%+v probes=%d profiles=%d err=%v", loggedOut, probeCalls, profileCalls, err)
	}

	state = logincheck.AuthLoggedIn
	if _, err := observer.observeFastCycle(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	loggedIn, err := database.GetAccount(account.ID)
	if err != nil || loggedIn.LoginState != db.AccountAuthLoggedIn || loggedIn.Name != "real-video-creator" || profileCalls != 3 {
		t.Fatalf("login account=%+v probes=%d profiles=%d err=%v", loggedIn, probeCalls, profileCalls, err)
	}
}

func TestAccountObserverFastLogoutRequiresFullConfirmation(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("meta", "Meta fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15188}, nil
	})
	fullCalls := 0
	profileID := browser.ProfileIDForAccount(account.ID)
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		fullCalls++
		if fullCalls == 2 {
			acquired, leaseErr := database.AcquireBrowserOperationLease(profileID, "publish", "competing-operation", time.Now(), time.Minute)
			if leaseErr != nil {
				t.Fatal(leaseErr)
			}
			if acquired {
				t.Fatal("fast transition confirmation released the profile lease before persistence")
			}
		}
		return &logincheck.Observation{
			AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedCookie,
			EvidenceCode: logincheck.EvidenceReviewedCookie,
		}, nil
	}
	observer.probe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{
			AuthState: logincheck.AuthLoggedOut, ReasonCode: logincheck.ReasonLoginRequiredPage,
			EvidenceCode: logincheck.EvidenceExplicitLoginDOM,
		}, nil
	}
	if _, err := observer.ObserveAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.observeFastCycle(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil || updated.LoginState != db.AccountAuthLoggedIn || fullCalls != 2 {
		t.Fatalf("transient Meta logout was persisted: account=%+v fullCalls=%d err=%v", updated, fullCalls, err)
	}
	acquired, err := database.AcquireBrowserOperationLease(profileID, "publish", "after-confirmation", time.Now(), time.Minute)
	if err != nil || !acquired {
		t.Fatalf("fast transition lease was not released: acquired=%v err=%v", acquired, err)
	}
	_ = database.ReleaseBrowserOperationLease(profileID, "after-confirmation")
}

func TestAccountObserverSyncsAvatarAfterFastLoginTransition(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("wechat_mp", "existing account", "")
	if err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15181}, nil
	})
	avatar := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			avatar.Set(x, y, color.RGBA{R: 40, G: 120, B: 220, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, avatar); err != nil {
		t.Fatal(err)
	}
	observer.probe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM, EvidenceCode: logincheck.EvidenceReviewedProfileDOM, Name: "creator"}, nil
	}
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM, EvidenceCode: logincheck.EvidenceReviewedProfileDOM, Name: "creator", AvatarPNG: encoded.Bytes()}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	var waitGroup sync.WaitGroup
	observer.Start(ctx, &waitGroup)
	t.Cleanup(func() {
		cancel()
		waitGroup.Wait()
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		updated, getErr := database.GetAccount(account.ID)
		if getErr == nil && updated.AvatarSource == db.AvatarSourceObserved && updated.AvatarSHA256 != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	updated, getErr := database.GetAccount(account.ID)
	t.Fatalf("fast login did not sync avatar: account=%+v err=%v", updated, getErr)
}

func TestAccountObserverRefreshesExistingIdentityOnNewBrowserSession(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("wechat_mp", "旧公众号", "")
	if err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, PID: 7123, CDPPort: 15182}, nil
	})
	encodeAvatar := func(red uint8) []byte {
		t.Helper()
		avatar := image.NewRGBA(image.Rect(0, 0, 32, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				avatar.Set(x, y, color.RGBA{R: red, G: 60, B: 180, A: 255})
			}
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, avatar); err != nil {
			t.Fatal(err)
		}
		return encoded.Bytes()
	}
	oldAvatar := encodeAvatar(30)
	newAvatar := encodeAvatar(220)
	profileCalls := 0
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		profileCalls++
		if profileCalls == 1 {
			return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM, EvidenceCode: logincheck.EvidenceReviewedProfileDOM, Name: "旧公众号", AccountIdentifier: "old-id", AvatarPNG: oldAvatar}, nil
		}
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM, EvidenceCode: logincheck.EvidenceReviewedProfileDOM, Name: "新公众号", AccountIdentifier: "new-id", AvatarPNG: newAvatar}, nil
	}
	observer.probe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedSessionDOM, EvidenceCode: logincheck.EvidenceReviewedSessionDOM}, nil
	}
	if _, err := observer.ObserveAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	before, err := database.GetAccount(account.ID)
	if err != nil || before.AvatarSHA256 == "" {
		t.Fatalf("initial account=%+v err=%v", before, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var waitGroup sync.WaitGroup
	observer.Start(ctx, &waitGroup)
	t.Cleanup(func() {
		cancel()
		waitGroup.Wait()
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		updated, getErr := database.GetAccount(account.ID)
		if getErr == nil && updated.Name == "新公众号" && updated.AccountIdentifier == "new-id" && updated.AvatarSHA256 != before.AvatarSHA256 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	updated, getErr := database.GetAccount(account.ID)
	t.Fatalf("fresh browser session did not refresh identity: account=%+v profileCalls=%d err=%v", updated, profileCalls, getErr)
}

func TestAccountObserverFastCycleNeverWaitsForFullProfileCapture(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	for _, platform := range []string{"csdn", "kuaishou"} {
		if _, err := database.CreateAccount(platform, platform+" fixture", ""); err != nil {
			t.Fatal(err)
		}
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15180 + int(accountID)}, nil
	})
	profileCalls := 0
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		profileCalls++
		time.Sleep(2 * time.Second)
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM, EvidenceCode: logincheck.EvidenceReviewedProfileDOM}, nil
	}
	observer.probe = func(_ context.Context, platform string, _ int) (*logincheck.Observation, error) {
		if platform == "kuaishou" {
			return &logincheck.Observation{AuthState: logincheck.AuthLoggedOut, ReasonCode: logincheck.ReasonLoginRequiredPage, EvidenceCode: logincheck.EvidenceExplicitLoginDOM}, nil
		}
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedSessionDOM, EvidenceCode: logincheck.EvidenceReviewedSessionDOM}, nil
	}

	started := time.Now()
	results, err := observer.observeFastCycle(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("fast cycle waited for profile capture: %s", elapsed)
	}
	if profileCalls != 0 || len(results) != 2 {
		t.Fatalf("profileCalls=%d results=%+v", profileCalls, results)
	}
}

func TestAccountObserverFastCyclePersistsCheckpointAndProbeFailure(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15186}, nil
	})
	profileCalls := 0
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		profileCalls++
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM, EvidenceCode: logincheck.EvidenceReviewedProfileDOM}, nil
	}
	if _, err := observer.ObserveAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if profileCalls != 1 {
		t.Fatalf("initial profileCalls=%d", profileCalls)
	}
	observer.probe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{AuthState: logincheck.AuthUnknown, ReasonCode: logincheck.ReasonCheckpointRequired, EvidenceCode: logincheck.EvidenceCheckpointDOM}, nil
	}

	checkpoint, err := observer.observeFastCycle(context.Background(), true)
	if err != nil || len(checkpoint) != 1 || checkpoint[0].AuthState != db.AccountAuthUnknown || checkpoint[0].ReasonCode != db.LoginReasonCheckpointRequired {
		t.Fatalf("checkpoint=%+v err=%v", checkpoint, err)
	}
	latest, err := database.LatestAccountLoginEvent(account.ID)
	if err != nil || latest == nil || latest.AuthState != db.AccountAuthUnknown || latest.ReasonCode != db.LoginReasonCheckpointRequired {
		t.Fatalf("latest checkpoint=%+v err=%v", latest, err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil || updated.LoginState != db.AccountAuthLoggedIn {
		t.Fatalf("checkpoint must not invent a logout: account=%+v err=%v", updated, err)
	}

	observer.probe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return nil, context.DeadlineExceeded
	}
	unreachable, err := observer.observeFastCycle(context.Background(), true)
	if err != nil || len(unreachable) != 1 || unreachable[0].AuthState != db.AccountAuthUnknown || unreachable[0].ReasonCode != db.LoginReasonCDPUnreachable {
		t.Fatalf("unreachable=%+v err=%v", unreachable, err)
	}
	latest, err = database.LatestAccountLoginEvent(account.ID)
	if err != nil || latest == nil || latest.AuthState != db.AccountAuthUnknown || latest.ReasonCode != db.LoginReasonCDPUnreachable {
		t.Fatalf("latest unreachable=%+v err=%v", latest, err)
	}
	if profileCalls != 1 {
		t.Fatalf("fast checkpoint/error unexpectedly triggered profile capture: %d", profileCalls)
	}
}

func TestAccountObserverFastIntervalIsNearRealTime(t *testing.T) {
	if accountObservationInterval > time.Second {
		t.Fatalf("account observation interval=%s want <=1s", accountObservationInterval)
	}
}

func TestFastObservationRepairsMissingSafePageOriginWithoutRouteSpam(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("wechat_mp", "公众号", "")
	if err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(int64) (*browser.AccountStatus, error) { return nil, nil })
	base := db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthUnknown, BrowserState: string(browser.StateReady),
		LoginMode: db.LoginModeBrowserProfileSession, AuthMethod: db.AuthMethodUnknown,
		ReasonCode: db.LoginReasonProfileSelectorMissing, EvidenceCode: db.LoginEvidenceMissing,
		AvatarSource: db.AvatarSourcePlatformFallback, ObservedAt: time.Now().UTC().Format(time.RFC3339), TraceID: "origin-before-fix",
	}
	if _, err := database.RecordAccountLoginObservation(base); err != nil {
		t.Fatal(err)
	}
	withOrigin := base
	withOrigin.PageOrigin = "https://mp.weixin.qq.com/cgi-bin/home"
	withOrigin.TraceID = "origin-repair"
	if !observer.fastObservationChanged(account, withOrigin, false) {
		t.Fatal("missing safe page origin was not repaired")
	}
	if _, err := database.RecordAccountLoginObservation(withOrigin); err != nil {
		t.Fatal(err)
	}
	differentRoute := withOrigin
	differentRoute.PageOrigin = "https://mp.weixin.qq.com/cgi-bin/appmsg"
	if observer.fastObservationChanged(account, differentRoute, false) {
		t.Fatal("unchanged unknown evidence would create route-level log spam")
	}
}

func TestAccountObserverManualCheckDoesNotWaitForBatchCycle(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15106}, nil
	})
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedSessionDOM, EvidenceCode: logincheck.EvidenceReviewedSessionDOM}, nil
	}

	observer.cycleMu.Lock()
	type checkResult struct {
		result *AccountObservationResponse
		err    error
	}
	done := make(chan checkResult, 1)
	go func() {
		result, observeErr := observer.ObserveAccount(context.Background(), account.ID)
		done <- checkResult{result: result, err: observeErr}
	}()

	select {
	case got := <-done:
		observer.cycleMu.Unlock()
		if got.err != nil || got.result == nil || got.result.AuthState != db.AccountAuthLoggedIn {
			t.Fatalf("result=%+v err=%v", got.result, got.err)
		}
	case <-time.After(250 * time.Millisecond):
		observer.cycleMu.Unlock()
		t.Fatal("manual account check waited for the unrelated batch cycle")
	}
}

func TestAccountObserverManualCheckRetriesFreshPlatformTab(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("douyin", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15107}, nil
	})
	var calls int
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		calls++
		if calls == 1 {
			return &logincheck.Observation{AuthState: logincheck.AuthUnknown, ReasonCode: logincheck.ReasonPlatformTabMissing, EvidenceCode: logincheck.EvidenceMissing}, nil
		}
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM, EvidenceCode: logincheck.EvidenceReviewedProfileDOM}, nil
	}

	result, err := observer.ObserveAccount(context.Background(), account.ID)
	if err != nil || result == nil || result.AuthState != db.AccountAuthLoggedIn || calls != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
}

func TestAccountObserverEmitsStateChangeOnlyAfterDurableUpdate(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("kuaishou", "快手通知测试", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE accounts SET login_state='logged_in' WHERE id=?`, account.ID); err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15185}, nil
	})
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{
			AuthState: logincheck.AuthLoggedOut, ReasonCode: logincheck.ReasonSessionExpiredPage,
			EvidenceCode: logincheck.EvidenceExplicitExpiredDOM,
		}, nil
	}
	var received bool
	observer.onStateChange = func(change notify.AccountStateChange) error {
		persisted, err := database.GetAccount(change.AccountID)
		if err != nil {
			t.Fatal(err)
		}
		if persisted.LoginState != db.AccountAuthLoggedOut || change.Previous != db.AccountAuthLoggedIn || change.Current != db.AccountAuthLoggedOut {
			t.Fatalf("change=%+v persisted=%+v", change, persisted)
		}
		received = true
		return nil
	}
	if _, err := observer.ObserveAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if !received {
		t.Fatal("state change was not emitted")
	}
}

func TestAccountObserverFastProbesRunConcurrently(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	for _, code := range []string{"xhs", "douyin", "wechat_mp", "wechat_video", "kuaishou", "csdn"} {
		if _, err := database.CreateAccount(code, code+" fixture", ""); err != nil {
			t.Fatal(err)
		}
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15100 + int(accountID)}, nil
	})
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedCookie, EvidenceCode: logincheck.EvidenceReviewedCookie}, nil
	}
	if _, err := observer.ObserveRunning(context.Background()); err != nil {
		t.Fatal(err)
	}

	var active, maxActive int32
	observer.probe = func(context.Context, string, int) (*logincheck.Observation, error) {
		current := atomic.AddInt32(&active, 1)
		for {
			previous := atomic.LoadInt32(&maxActive)
			if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedSessionDOM, EvidenceCode: logincheck.EvidenceReviewedSessionDOM}, nil
	}
	if _, err := observer.observeFastCycle(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if maxActive < 2 {
		t.Fatalf("fast probes were serialized: max active=%d", maxActive)
	}
}

func TestAccountObserverRespectsCrossProcessOperationLease(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	profileID := browser.ProfileIDForAccount(account.ID)
	acquired, err := database.AcquireBrowserOperationLease(profileID, "publish", "other-process", time.Now(), time.Minute)
	if err != nil || !acquired {
		t.Fatalf("acquired=%v err=%v", acquired, err)
	}
	defer database.ReleaseBrowserOperationLease(profileID, "other-process")
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateBusy, CDPPort: 15101}, nil
	})
	var called bool
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		called = true
		return nil, nil
	}
	result, err := observer.ObserveAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if called || result.AuthState != db.AccountAuthUnknown || result.ReasonCode != db.LoginReasonBrowserOperationBusy || result.EvidenceCode != db.LoginEvidenceOperationLeaseBusy {
		t.Fatalf("called=%v result=%+v", called, result)
	}
}

func TestManualAccountObservationWaitsForBackgroundProbe(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15101}, nil
	})
	called := false
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		called = true
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedSessionDOM, EvidenceCode: logincheck.EvidenceReviewedSessionDOM}, nil
	}
	profileID := browser.ProfileIDForAccount(account.ID)
	probeOwner := "observe-probe:test-manual-wait"
	if acquired, err := database.AcquireBrowserOperationLease(profileID, "observe", probeOwner, time.Now(), time.Minute); err != nil || !acquired {
		t.Fatalf("probe acquire=%v err=%v", acquired, err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(75 * time.Millisecond)
		_ = database.ReleaseBrowserOperationLease(profileID, probeOwner)
		close(released)
	}()

	result, err := observer.ObserveAccount(context.Background(), account.ID)
	if err != nil || result == nil || result.AuthState != db.AccountAuthLoggedIn || !called {
		t.Fatalf("manual observation lost to background probe: result=%+v called=%v err=%v", result, called, err)
	}
	<-released
}

func TestAccountObserverFastProbeDoesNotOverwriteVerifiedStateWhenLeaseBusy(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("csdn", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15106}, nil
	})
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedSessionDOM, EvidenceCode: logincheck.EvidenceReviewedSessionDOM}, nil
	}
	if _, err := observer.ObserveAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	profileID := browser.ProfileIDForAccount(account.ID)
	acquired, err := database.AcquireBrowserOperationLease(profileID, "publish", "other-process", time.Now(), time.Minute)
	if err != nil || !acquired {
		t.Fatalf("acquired=%v err=%v", acquired, err)
	}
	defer database.ReleaseBrowserOperationLease(profileID, "other-process")
	if _, err := observer.observeFastCycle(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	latest, latestErr := database.LatestAccountLoginEvent(account.ID)
	if err != nil || latestErr != nil || latest == nil || latest.ReasonCode != db.LoginReasonVerifiedSessionDOM || updated.LoginState != db.AccountAuthLoggedIn {
		t.Fatalf("account=%+v latest=%+v err=%v latestErr=%v", updated, latest, err, latestErr)
	}
}

func TestActiveManualLoginSessionKeepsManualMode(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("douyin", "douyin", "")
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
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15102}, nil
	})
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedCookie, EvidenceCode: logincheck.EvidenceReviewedCookie}, nil
	}
	result, err := observer.ObserveAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.LoginMode != db.LoginModeManualBrowser || result.AuthMethod != db.AuthMethodUnknown {
		t.Fatalf("result=%+v", result)
	}
	sessions, err := database.ListLoginSessions(10)
	if err != nil || len(sessions) != 1 || sessions[0].Status != "verified" {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	second, err := observer.ObserveAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.LoginMode != db.LoginModeManualBrowser {
		t.Fatalf("periodic observation overwrote manual login provenance: %+v", second)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil || updated.LoginMode != db.LoginModeManualBrowser {
		t.Fatalf("account=%+v err=%v", updated, err)
	}
}

func TestAvatarFailuresAreNonFatalAndDurable(t *testing.T) {
	tests := []struct {
		name        string
		observation logincheck.Observation
		wantCode    string
	}{
		{
			name: "capture failure",
			observation: logincheck.Observation{
				AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM,
				EvidenceCode: logincheck.EvidenceReviewedProfileDOM, Name: "creator",
				AvatarErrorCode: "AVATAR_CAPTURE_FAILED",
			},
			wantCode: "AVATAR_CAPTURE_FAILED",
		},
		{
			name: "persist failure",
			observation: logincheck.Observation{
				AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM,
				EvidenceCode: logincheck.EvidenceReviewedProfileDOM, Name: "creator",
				AvatarPNG: []byte("not a png"),
			},
			wantCode: "AVATAR_PERSIST_FAILED",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, dataDir := newObserverTestDB(t)
			account, err := database.CreateAccount("xhs", "creator alias", "")
			if err != nil {
				t.Fatal(err)
			}
			observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
				return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15180}, nil
			})
			observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
				copy := test.observation
				return &copy, nil
			}
			result, err := observer.ObserveAccount(context.Background(), account.ID)
			if err != nil {
				t.Fatal(err)
			}
			if result.AuthState != db.AccountAuthLoggedIn || result.ReasonCode != db.LoginReasonVerifiedProfileDOM || result.AvatarErrorCode != test.wantCode {
				t.Fatalf("result=%+v", result)
			}
			updated, err := database.GetAccount(account.ID)
			if err != nil || updated.LoginState != db.AccountAuthLoggedIn {
				t.Fatalf("account=%+v err=%v", updated, err)
			}
			event, err := database.LatestAccountLoginEvent(account.ID)
			if err != nil || event == nil || event.AvatarErrorCode != test.wantCode || event.AuthState != db.AccountAuthLoggedIn {
				t.Fatalf("event=%+v err=%v", event, err)
			}
		})
	}
}

func TestAvatarPlaceholderRecoversLastValidSnapshotForSameIdentity(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	valid := image.NewRGBA(image.Rect(0, 0, 48, 48))
	draw.Draw(valid, valid.Bounds(), &image.Uniform{C: color.RGBA{R: 24, G: 96, B: 210, A: 255}}, image.Point{}, draw.Src)
	var validPNG bytes.Buffer
	if err := png.Encode(&validPNG, valid); err != nil {
		t.Fatal(err)
	}
	validRef, validSHA, err := persistAccountAvatarSnapshot(dataDir, account.ID, validPNG.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		LoginMode: db.LoginModeBrowserProfileSession, AuthMethod: db.AuthMethodUnknown,
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		Name: "creator", AccountIdentifier: "AgentCreator",
		AvatarSource: db.AvatarSourceObserved, AvatarRef: validRef, AvatarSHA256: validSHA,
		TraceID: "valid-avatar",
	}); err != nil {
		t.Fatal(err)
	}

	placeholder := image.NewRGBA(image.Rect(0, 0, 48, 48))
	draw.Draw(placeholder, placeholder.Bounds(), &image.Uniform{C: color.RGBA{R: 244, G: 244, B: 244, A: 255}}, image.Point{}, draw.Src)
	var placeholderPNG bytes.Buffer
	if err := png.Encode(&placeholderPNG, placeholder); err != nil {
		t.Fatal(err)
	}
	placeholderDigest := sha256.Sum256(placeholderPNG.Bytes())
	placeholderSHA := fmt.Sprintf("%x", placeholderDigest[:])
	placeholderRef := filepath.ToSlash(filepath.Join("account-avatars", fmt.Sprintf("account-%d-%s.png", account.ID, placeholderSHA[:16])))
	if err := os.WriteFile(filepath.Join(dataDir, filepath.FromSlash(placeholderRef)), placeholderPNG.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE accounts SET avatar=?, avatar_source=?, avatar_sha256=? WHERE id=?`,
		placeholderRef, db.AvatarSourceObserved, placeholderSHA, account.ID); err != nil {
		t.Fatal(err)
	}

	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15180}, nil
	})
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{
			AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM,
			EvidenceCode: logincheck.EvidenceReviewedProfileDOM, Name: "creator",
			AccountIdentifier: "AgentCreator", AvatarPNG: placeholderPNG.Bytes(),
		}, nil
	}
	result, err := observer.ObserveAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.AvatarSHA256 != validSHA || result.AvatarErrorCode != "" {
		t.Fatalf("result=%+v, want recovered avatar sha %q", result, validSHA)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Avatar != validRef || updated.AvatarSHA256 != validSHA {
		t.Fatalf("account=%+v, want recovered avatar %q", updated, validRef)
	}
}

func TestAccountsAPINeverReturnsLegacyRemoteAvatar(t *testing.T) {
	database, _ := newObserverTestDB(t)
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE accounts SET avatar='https://cdn.example/avatar.png?signature=secret', avatar_source='legacy_remote' WHERE id=?`, account.ID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database}
	recorder := httptest.NewRecorder()
	server.handleAPIAccounts(recorder, httptest.NewRequest(http.MethodGet, "/api/accounts", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response []struct {
		Avatar       string `json:"avatar"`
		AvatarSource string `json:"avatarSource"`
		Enabled      bool   `json:"enabled"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 1 || !response[0].Enabled || response[0].Avatar != "" || response[0].AvatarSource != db.AvatarSourcePlatformFallback {
		t.Fatalf("response=%+v", response)
	}
}

func TestAccountsAPIReportsLatestEvidenceWithoutDemotingPersistedLoginState(t *testing.T) {
	database, _ := newObserverTestDB(t)
	account, err := database.CreateAccount("douyin", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		ObservedAt: "2026-07-25T01:00:00Z", TraceID: "verified",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthUnknown, BrowserState: "ready",
		ReasonCode: db.LoginReasonProfileSelectorMissing, EvidenceCode: db.LoginEvidenceMissing,
		ObservedAt: "2026-07-25T01:00:01Z", TraceID: "fast-unknown",
	}); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: database}
	recorder := httptest.NewRecorder()
	server.handleAPIAccounts(recorder, httptest.NewRequest(http.MethodGet, "/api/accounts", nil))
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("accounts cache policy=%q, want no-store", recorder.Header().Get("Cache-Control"))
	}
	var response []struct {
		LoginState        string `json:"loginState"`
		LoginReasonCode   string `json:"loginReasonCode"`
		LoginEvidenceCode string `json:"loginEvidenceCode"`
		LastObservedAt    string `json:"lastObservedAt"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 1 || response[0].LoginState != db.AccountAuthLoggedIn ||
		response[0].LoginReasonCode != db.LoginReasonProfileSelectorMissing ||
		response[0].LoginEvidenceCode != db.LoginEvidenceMissing ||
		response[0].LastObservedAt != "2026-07-25T01:00:01Z" {
		t.Fatalf("response=%+v", response)
	}
}

func TestAccountsAPIDefaultHidesArchivedAccounts(t *testing.T) {
	database, _ := newObserverTestDB(t)
	visible, err := database.CreateAccount("xhs", "visible", "")
	if err != nil {
		t.Fatal(err)
	}
	archived, err := database.CreateAccount("douyin", "archived", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SetAccountEnabled(archived.ID, false); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database}
	recorder := httptest.NewRecorder()
	server.handleAPIAccounts(recorder, httptest.NewRequest(http.MethodGet, "/api/accounts", nil))
	var active []struct {
		ID int64 `json:"id"`
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := json.NewDecoder(recorder.Body).Decode(&active); err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != visible.ID {
		t.Fatalf("active=%+v", active)
	}

	recorder = httptest.NewRecorder()
	server.handleAPIAccounts(recorder, httptest.NewRequest(http.MethodGet, "/api/accounts?includeDisabled=true", nil))
	var all []struct {
		ID      int64 `json:"id"`
		Enabled bool  `json:"enabled"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("all=%+v", all)
	}
}

func TestAccountEnabledAPIArchivesWithoutDeletingAccount(t *testing.T) {
	database, _ := newObserverTestDB(t)
	account, err := database.CreateAccount("wechat_video", "archive-me", "")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database}
	request := httptest.NewRequest(http.MethodPost, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/enabled", strings.NewReader(`{"enabled":false}`))
	request.SetPathValue("id", strconv.FormatInt(account.ID, 10))
	recorder := httptest.NewRecorder()
	server.handleAPIAccountEnabled(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil || updated.Enabled {
		t.Fatalf("account=%+v err=%v", updated, err)
	}
}

func TestAccountsAPIRemovesObservedAvatarWhenIdentityChangesWithoutFreshCapture(t *testing.T) {
	database, _ := newObserverTestDB(t)
	account, err := database.CreateAccount("kuaishou", "旧账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		Name: "旧账号", AccountIdentifier: "old-id", AvatarSource: db.AvatarSourceObserved,
		AvatarRef: "account-avatars/account-1-aaaaaaaaaaaaaaaa.png", AvatarSHA256: strings.Repeat("a", 64),
		ObservedAt: "2026-07-20T10:00:00Z", TraceID: "old-avatar",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		Name: "新账号", AccountIdentifier: "new-id", AvatarSource: db.AvatarSourcePlatformFallback,
		ObservedAt: "2026-07-20T10:01:00Z", TraceID: "new-no-avatar",
	}); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: database}
	recorder := httptest.NewRecorder()
	server.handleAPIAccounts(recorder, httptest.NewRequest(http.MethodGet, "/api/accounts", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response []struct {
		Name              string `json:"name"`
		AccountIdentifier string `json:"accountIdentifier"`
		Avatar            string `json:"avatar"`
		AvatarSource      string `json:"avatarSource"`
		AvatarSHA256      string `json:"avatarSha256"`
		AvatarRefreshedAt string `json:"avatarRefreshedAt"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 1 || response[0].Name != "新账号" || response[0].AccountIdentifier != "new-id" ||
		response[0].Avatar != "" || response[0].AvatarSHA256 != "" || response[0].AvatarRefreshedAt != "" || response[0].AvatarSource != db.AvatarSourcePlatformFallback {
		t.Fatalf("API retained an avatar from the previous identity: %+v", response)
	}
}

func TestAccountsAPINeverReturnsBrowserProfileDirectory(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	privateProfileDir := filepath.Join(dataDir, "profiles", "private-synthetic-profile")
	if _, err := database.CreateBrowserProfile(account.ID, privateProfileDir, 123456, "zh-CN", "zh-CN,zh", "Asia/Shanghai"); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database}
	recorder := httptest.NewRecorder()
	server.handleAPIAccounts(recorder, httptest.NewRequest(http.MethodGet, "/api/accounts", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), privateProfileDir) || strings.Contains(recorder.Body.String(), `"profileDir"`) {
		t.Fatalf("accounts API exposed a browser profile directory: %s", recorder.Body.String())
	}
}

func TestAccountBrowserEventsAPIReturnsSafeLifecycleMetadata(t *testing.T) {
	database, _ := newObserverTestDB(t)
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	privateDetail := "private browser stderr must not leave sqlite"
	if _, err := database.Exec(`INSERT INTO browser_events
		(profile_id, event_type, message, reason_code, exit_code, pid, cdp_port, created_at)
		VALUES (?, 'crashed', ?, 'PROCESS_EXIT_NONZERO', 23, 321, 15121, '2026-07-21T01:02:03Z')`, browser.ProfileIDForAccount(account.ID), privateDetail); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/accounts/1/browser-events?limit=20", nil)
	request.SetPathValue("id", strconv.FormatInt(account.ID, 10))
	server.handleAPIAccountBrowserEvents(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), privateDetail) || strings.Contains(recorder.Body.String(), `"message"`) || strings.Contains(recorder.Body.String(), `"profileId"`) {
		t.Fatalf("browser diagnostics exposed private fields: %s", recorder.Body.String())
	}
	var events []db.BrowserLifecycleEvent
	if err := json.NewDecoder(recorder.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "crashed" || events[0].ReasonCode != "PROCESS_EXIT_NONZERO" || events[0].ExitCode == nil || *events[0].ExitCode != 23 || events[0].PID != 321 || events[0].CDPPort != 15121 {
		t.Fatalf("events=%+v", events)
	}
}

func TestAccountAddAPINeverReturnsBrowserProfileDirectory(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	server := &Server{db: database, cfg: config.Config{DataDir: dataDir}}
	body := bytes.NewBufferString(`{"platform":"xhs","name":"creator","startNow":false}`)
	recorder := httptest.NewRecorder()
	server.handleAPIAccountAdd(recorder, httptest.NewRequest(http.MethodPost, "/api/accounts/add", body))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), dataDir) || strings.Contains(recorder.Body.String(), `"profileDir"`) {
		t.Fatalf("account add API exposed a browser profile directory: %s", recorder.Body.String())
	}
}

func TestPersistAccountAvatarSnapshotIsValidatedAndAtomic(t *testing.T) {
	dataDir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			img.Set(x, y, color.RGBA{R: 30, G: 120, B: 210, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	reference, sha, err := persistAccountAvatarSnapshot(dataDir, 7, encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(reference) || len(sha) != 64 {
		t.Fatalf("reference=%q sha=%q", reference, sha)
	}
	info, err := os.Stat(filepath.Join(dataDir, filepath.FromSlash(reference)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	if _, _, err := persistAccountAvatarSnapshot(dataDir, 7, []byte("not an image")); err == nil {
		t.Fatal("invalid avatar bytes were accepted")
	}
}

func TestPersistAccountAvatarSnapshotAcceptsReviewedQLogoDimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1080, 1080))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 0, G: 135, B: 168, A: 255}}, image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	if _, _, err := persistAccountAvatarSnapshot(t.TempDir(), 5, encoded.Bytes()); err != nil {
		t.Fatalf("reviewed 1080px qlogo avatar was rejected: %v", err)
	}
}

func TestNormalizeAvatarPNGRecentersSmallArtworkOnSolidCanvas(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 120, 120))
	draw.Draw(source, source.Bounds(), &image.Uniform{C: color.RGBA{A: 255}}, image.Point{}, draw.Src)
	draw.Draw(source, image.Rect(32, 53, 88, 67), &image.Uniform{C: color.RGBA{R: 240, G: 240, B: 240, A: 255}}, image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}

	normalized, err := normalizeAvatarPNG(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(normalized))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() >= source.Bounds().Dx() || got.Bounds().Dx() != got.Bounds().Dy() {
		t.Fatalf("small centered artwork was not reframed: before=%v after=%v", source.Bounds(), got.Bounds())
	}
	content, found := avatarContentBounds(got, got.Bounds(), color.NRGBA{A: 255})
	if !found {
		t.Fatal("normalized artwork disappeared")
	}
	left := content.Min.X
	right := got.Bounds().Max.X - content.Max.X
	top := content.Min.Y
	bottom := got.Bounds().Max.Y - content.Max.Y
	if left-right > 1 || right-left > 1 || top-bottom > 1 || bottom-top > 1 {
		t.Fatalf("normalized artwork is not centered: content=%v bounds=%v", content, got.Bounds())
	}
}

func TestNormalizeAvatarPNGLeavesFullCanvasArtworkUntouched(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 120, 120))
	draw.Draw(source, source.Bounds(), &image.Uniform{C: color.RGBA{A: 255}}, image.Point{}, draw.Src)
	draw.Draw(source, image.Rect(10, 10, 110, 110), &image.Uniform{C: color.RGBA{R: 32, G: 128, B: 220, A: 255}}, image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	normalized, err := normalizeAvatarPNG(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(normalized, encoded.Bytes()) {
		t.Fatal("regular full-canvas artwork was unexpectedly reframed")
	}
}

func TestPersistAccountAvatarSnapshotRejectsUniformNeutralPlaceholder(t *testing.T) {
	dataDir := t.TempDir()
	placeholder := image.NewRGBA(image.Rect(0, 0, 144, 146))
	for y := 0; y < 146; y++ {
		for x := 0; x < 144; x++ {
			placeholder.Set(x, y, color.RGBA{R: 242, G: 244, B: 247, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, placeholder); err != nil {
		t.Fatal(err)
	}
	if _, _, err := persistAccountAvatarSnapshot(dataDir, 1, encoded.Bytes()); err == nil {
		t.Fatal("uniform neutral placeholder was accepted as an account avatar")
	}
}

func TestRecoverLatestValidAccountAvatarSnapshotSkipsNeutralPlaceholder(t *testing.T) {
	dataDir := t.TempDir()
	valid := image.NewRGBA(image.Rect(0, 0, 48, 48))
	draw.Draw(valid, valid.Bounds(), &image.Uniform{C: color.RGBA{R: 24, G: 96, B: 210, A: 255}}, image.Point{}, draw.Src)
	var validPNG bytes.Buffer
	if err := png.Encode(&validPNG, valid); err != nil {
		t.Fatal(err)
	}
	wantRef, wantSHA, err := persistAccountAvatarSnapshot(dataDir, 7, validPNG.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	placeholder := image.NewRGBA(image.Rect(0, 0, 48, 48))
	draw.Draw(placeholder, placeholder.Bounds(), &image.Uniform{C: color.RGBA{R: 244, G: 244, B: 244, A: 255}}, image.Point{}, draw.Src)
	var placeholderPNG bytes.Buffer
	if err := png.Encode(&placeholderPNG, placeholder); err != nil {
		t.Fatal(err)
	}
	placeholderDigest := sha256.Sum256(placeholderPNG.Bytes())
	placeholderName := fmt.Sprintf("account-7-%x.png", placeholderDigest[:8])
	placeholderPath := filepath.Join(dataDir, "account-avatars", placeholderName)
	if err := os.WriteFile(placeholderPath, placeholderPNG.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	newer := time.Now().Add(time.Minute)
	if err := os.Chtimes(placeholderPath, newer, newer); err != nil {
		t.Fatal(err)
	}

	gotRef, gotSHA, err := recoverLatestValidAccountAvatarSnapshot(dataDir, 7)
	if err != nil {
		t.Fatal(err)
	}
	if gotRef != wantRef || gotSHA != wantSHA {
		t.Fatalf("recovered ref=%q sha=%q, want ref=%q sha=%q", gotRef, gotSHA, wantRef, wantSHA)
	}
}

func TestPersistAccountAvatarSnapshotRemovesLargeOneSidedPadding(t *testing.T) {
	dataDir := t.TempDir()
	avatar := image.NewRGBA(image.Rect(0, 0, 96, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			avatar.Set(x, y, color.White)
		}
	}
	for y := 0; y < 96; y++ {
		for x := 15; x < 96; x++ {
			dx := (float64(x) - 55) / 40
			dy := (float64(y) - 48) / 48
			if dx*dx+dy*dy <= 1 {
				avatar.Set(x, y, color.Black)
			}
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, avatar); err != nil {
		t.Fatal(err)
	}
	reference, _, err := persistAccountAvatarSnapshot(dataDir, 7, encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(dataDir, filepath.FromSlash(reference)))
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 96 || config.Height != 96 {
		t.Fatalf("normalized avatar size=%dx%d, want canonical 96x96", config.Width, config.Height)
	}
}

func TestAccountAvatarStorageRejectsDirectoryAndTargetSymlinks(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 24, G: 96, B: 210, A: 255}}, image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}

	directoryData := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(directoryData, "account-avatars")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, err := persistAccountAvatarSnapshot(directoryData, 1, encoded.Bytes()); err == nil {
		t.Fatal("symlinked avatar directory was accepted")
	}

	targetData := t.TempDir()
	reference, sha, err := persistAccountAvatarSnapshot(targetData, 2, encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetData, filepath.FromSlash(reference))
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "outside.png")
	if err := os.WriteFile(outsideFile, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, target); err != nil {
		t.Skipf("target symlink unavailable: %v", err)
	}
	if _, err := managedAvatarPath(targetData, reference); err == nil {
		t.Fatal("symlinked avatar target was accepted")
	}
	if _, _, err := persistAccountAvatarSnapshot(targetData, 2, encoded.Bytes()); err == nil {
		t.Fatal("symlinked avatar destination was accepted")
	}
	_ = sha
}

func TestWechatVideoLoginRequiredFixtureRecordsExplicitLogout(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("wechat_video", "动态社交C", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15104}, nil
	})
	// Deterministic fixture only: it does not attach to or log out the user's
	// real WeChat Channels browser.
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{
			AuthState: logincheck.AuthLoggedOut, ReasonCode: logincheck.ReasonLoginRequiredPage,
			EvidenceCode: logincheck.EvidenceExplicitLoginDOM, PageOrigin: "https://channels.weixin.qq.com/login.html",
		}, nil
	}
	result, err := observer.ObserveAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthState != db.AccountAuthLoggedOut || updated.LoginState != db.AccountAuthLoggedOut || result.ReasonCode != db.LoginReasonLoginRequiredPage {
		t.Fatalf("result=%+v account=%+v", result, updated)
	}
}

func TestAccountObserverEmitsOneSafeDiagnosticPerEvidenceChange(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	emitter := &captureObserverEmitter{}
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15105}, nil
	})
	observer.events = emitter
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{
			AuthState: logincheck.AuthLoggedOut, ReasonCode: logincheck.ReasonLoginRequiredPage,
			EvidenceCode: logincheck.EvidenceExplicitLoginDOM,
		}, nil
	}
	if _, err := observer.ObserveAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.ObserveAccount(context.Background(), account.ID); err != nil {
		t.Fatal(err)
	}
	if len(emitter.events) != 1 {
		t.Fatalf("diagnostic events=%+v", emitter.events)
	}
	event := emitter.events[0]
	if event.AccountID != account.ID || event.Platform != "xhs" || event.Status != db.AccountAuthLoggedOut || event.Step != db.LoginReasonLoginRequiredPage || event.ErrorCode != "LOGIN_REQUIRED" {
		t.Fatalf("event=%+v", event)
	}
}

func TestWechatVideoBrowserExitAfterVerifiedLoginKeepsPersistedState(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("wechat_video", "video fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	running := true
	observer := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		if !running {
			return &browser.AccountStatus{AccountID: accountID, Running: false, State: browser.StateStopped}, nil
		}
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15181}, nil
	})
	observeCalls := 0
	observer.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		observeCalls++
		return &logincheck.Observation{
			AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM,
			EvidenceCode: logincheck.EvidenceReviewedProfileDOM, Name: "isolated video creator",
		}, nil
	}
	first, err := observer.ObserveAccount(context.Background(), account.ID)
	if err != nil || first.AuthState != db.AccountAuthLoggedIn {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if _, err := database.Exec(`INSERT INTO browser_events (profile_id, event_type, message, pid, cdp_port, created_at) VALUES (?, 'process_exited', '', 0, 0, ?)`, browser.ProfileIDForAccount(account.ID), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	running = false
	results, err := observer.observeCycle(context.Background(), true)
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	exit := results[0]
	if exit.AuthState != db.AccountAuthUnknown || exit.PersistedLoginState != db.AccountAuthLoggedIn || exit.ReasonCode != db.LoginReasonBrowserProcessExited || exit.EvidenceCode != db.LoginEvidenceBrowserProcessExited {
		t.Fatalf("exit=%+v", exit)
	}
	if observeCalls != 1 {
		t.Fatalf("offline cycle unexpectedly attached to CDP; calls=%d", observeCalls)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil || updated.LoginState != db.AccountAuthLoggedIn {
		t.Fatalf("account=%+v err=%v", updated, err)
	}
	events, err := database.ListAccountLoginEvents(account.ID, 10)
	if err != nil || len(events) != 2 || events[0].ReasonCode != db.LoginReasonBrowserProcessExited || events[0].PersistedLoginState != db.AccountAuthLoggedIn {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestAccountObserverRestartRecoversPersistedTrackingForOfflineAccount(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("kuaishou", "快手 fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	firstObserver := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: true, State: browser.StateReady, CDPPort: 15182}, nil
	})
	firstObserver.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		return &logincheck.Observation{
			AuthState: logincheck.AuthLoggedIn, ReasonCode: logincheck.ReasonVerifiedProfileDOM,
			EvidenceCode: logincheck.EvidenceReviewedProfileDOM, Name: "快手 creator",
		}, nil
	}
	first, err := firstObserver.ObserveAccount(context.Background(), account.ID)
	if err != nil || first.AuthState != db.AccountAuthLoggedIn {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if _, err := database.Exec(`INSERT INTO browser_events (profile_id, event_type, message, pid, cdp_port, created_at) VALUES (?, 'crashed', 'fixture detail must not leave the database', 0, 0, ?)`, browser.ProfileIDForAccount(account.ID), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// A fresh observer simulates a service restart: its in-memory tracked map is empty.
	restarted := newAccountObserver(database, dataDir, func(accountID int64) (*browser.AccountStatus, error) {
		return &browser.AccountStatus{AccountID: accountID, Running: false, State: browser.StateStopped}, nil
	})
	restarted.observe = func(context.Context, string, int) (*logincheck.Observation, error) {
		t.Fatal("offline recovery must not attach to CDP")
		return nil, nil
	}
	results, err := restarted.observeCycle(context.Background(), true)
	if err != nil || len(results) != 1 {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	result := results[0]
	if result.AuthState != db.AccountAuthUnknown || result.PersistedLoginState != db.AccountAuthLoggedIn || result.ReasonCode != db.LoginReasonBrowserProcessExited || result.EvidenceCode != db.LoginEvidenceBrowserCrashed {
		t.Fatalf("result=%+v", result)
	}
	if strings.Contains(fmt.Sprintf("%+v", result), "fixture detail") {
		t.Fatalf("browser event message escaped into response: %+v", result)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil || updated.LoginState != db.AccountAuthLoggedIn {
		t.Fatalf("account=%+v err=%v", updated, err)
	}
	if _, err := database.Exec(`UPDATE accounts SET last_check_at=? WHERE id=?`, time.Now().UTC().Add(2*time.Second).Format(time.RFC3339), account.ID); err != nil {
		t.Fatal(err)
	}
	second, err := restarted.observeCycle(context.Background(), true)
	if err != nil || len(second) != 1 || second[0].EvidenceCode != db.LoginEvidenceBrowserCrashed {
		t.Fatalf("repeated offline observation lost the durable exit cause: results=%+v err=%v", second, err)
	}
	events, err := database.ListAccountLoginEvents(account.ID, 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("unchanged offline evidence was not deduplicated: events=%+v err=%v", events, err)
	}
}

func TestAccountAvatarHandlerServesOnlyObservedManagedSnapshot(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: 24, G: 96, B: 210, A: 255}}, image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	reference, sha, err := persistAccountAvatarSnapshot(dataDir, account.ID, encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		LoginMode: db.LoginModeBrowserProfileSession, AuthMethod: db.AuthMethodUnknown,
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		AvatarSource: db.AvatarSourceObserved, AvatarRef: reference, AvatarSHA256: sha,
		TraceID: "avatar-handler",
	})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: config.Config{DataDir: dataDir}, db: database, mux: http.NewServeMux()}
	server.mux.HandleFunc("GET /api/accounts/{id}/avatar", server.handleAPIAccountAvatar)
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/avatar", nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/png" || !bytes.Equal(recorder.Body.Bytes(), encoded.Bytes()) {
		t.Fatalf("status=%d contentType=%q bytes=%d", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.Len())
	}
}

func TestAccountAvatarHandlerFallsBackFromNeutralPlaceholder(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	valid := image.NewRGBA(image.Rect(0, 0, 48, 48))
	draw.Draw(valid, valid.Bounds(), &image.Uniform{C: color.RGBA{R: 24, G: 96, B: 210, A: 255}}, image.Point{}, draw.Src)
	var validPNG bytes.Buffer
	if err := png.Encode(&validPNG, valid); err != nil {
		t.Fatal(err)
	}
	if _, _, err := persistAccountAvatarSnapshot(dataDir, account.ID, validPNG.Bytes()); err != nil {
		t.Fatal(err)
	}

	placeholder := image.NewRGBA(image.Rect(0, 0, 48, 48))
	draw.Draw(placeholder, placeholder.Bounds(), &image.Uniform{C: color.RGBA{R: 244, G: 244, B: 244, A: 255}}, image.Point{}, draw.Src)
	var placeholderPNG bytes.Buffer
	if err := png.Encode(&placeholderPNG, placeholder); err != nil {
		t.Fatal(err)
	}
	placeholderDigest := sha256.Sum256(placeholderPNG.Bytes())
	placeholderSHA := fmt.Sprintf("%x", placeholderDigest[:])
	placeholderRef := filepath.ToSlash(filepath.Join("account-avatars", fmt.Sprintf("account-%d-%s.png", account.ID, placeholderSHA[:16])))
	if err := os.WriteFile(filepath.Join(dataDir, filepath.FromSlash(placeholderRef)), placeholderPNG.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE accounts SET avatar=?, avatar_source=?, avatar_sha256=? WHERE id=?`,
		placeholderRef, db.AvatarSourceObserved, placeholderSHA, account.ID); err != nil {
		t.Fatal(err)
	}

	server := &Server{cfg: config.Config{DataDir: dataDir}, db: database, mux: http.NewServeMux()}
	server.mux.HandleFunc("GET /api/accounts/{id}/avatar", server.handleAPIAccountAvatar)
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/avatar", nil))
	if recorder.Code != http.StatusOK || !bytes.Equal(recorder.Body.Bytes(), validPNG.Bytes()) {
		t.Fatalf("status=%d bytes=%d, want valid historical avatar", recorder.Code, recorder.Body.Len())
	}
}

func TestAccountAvatarHandlerNormalizesLegacyOneSidedPadding(t *testing.T) {
	database, dataDir := newObserverTestDB(t)
	account, err := database.CreateAccount("wechat_mp", "公众号", "")
	if err != nil {
		t.Fatal(err)
	}
	avatar := image.NewRGBA(image.Rect(0, 0, 96, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			avatar.Set(x, y, color.White)
		}
	}
	for y := 0; y < 96; y++ {
		for x := 15; x < 96; x++ {
			dx := (float64(x) - 55) / 40
			dy := (float64(y) - 48) / 48
			if dx*dx+dy*dy <= 1 {
				avatar.Set(x, y, color.Black)
			}
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, avatar); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw.Bytes())
	sha := fmt.Sprintf("%x", digest[:])
	if err := os.Mkdir(filepath.Join(dataDir, "account-avatars"), 0700); err != nil {
		t.Fatal(err)
	}
	reference := "account-avatars/legacy-padded.png"
	if err := os.WriteFile(filepath.Join(dataDir, filepath.FromSlash(reference)), raw.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		LoginMode: db.LoginModeBrowserProfileSession, AuthMethod: db.AuthMethodUnknown,
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		AvatarSource: db.AvatarSourceObserved, AvatarRef: reference, AvatarSHA256: sha,
		TraceID: "legacy-avatar-handler",
	}); err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: config.Config{DataDir: dataDir}, db: database, mux: http.NewServeMux()}
	server.mux.HandleFunc("GET /api/accounts/{id}/avatar", server.handleAPIAccountAvatar)
	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/avatar", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	if recorder.Header().Get("Cache-Control") != "private, max-age=0, must-revalidate" || recorder.Header().Get("ETag") == `"`+sha+`"` {
		t.Fatalf("cache=%q etag=%q", recorder.Header().Get("Cache-Control"), recorder.Header().Get("ETag"))
	}
	config, err := png.DecodeConfig(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 96 || config.Height != 96 {
		t.Fatalf("legacy avatar response size=%dx%d, want canonical 96x96", config.Width, config.Height)
	}
}
