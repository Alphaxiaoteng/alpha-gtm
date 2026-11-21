package db_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

func TestAccountObservationUnknownNeverOverwritesVerifiedLogin(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("wechat_video", "视频号账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}

	event, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthUnknown, BrowserState: "degraded",
		ReasonCode: db.LoginReasonCDPUnreachable, EvidenceCode: db.LoginEvidenceMissing,
		ObservedAt: time.Now().UTC().Format(time.RFC3339), TraceID: "trace-unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LoginState != db.AccountAuthLoggedIn {
		t.Fatalf("unknown observation changed login state to %q", updated.LoginState)
	}
	if updated.LastCheckAt == "" || event.PersistedLoginState != db.AccountAuthLoggedIn {
		t.Fatalf("updated=%+v event=%+v", updated, event)
	}
}

func TestPlatformUnreachableObservationPreservesVerifiedLogin(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("wechat_mp", "公众号账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	event, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthUnknown, BrowserState: "ready",
		ReasonCode: db.LoginReasonPlatformUnreachable, EvidenceCode: db.LoginEvidenceMissing,
		ObservedAt: time.Now().UTC().Format(time.RFC3339), TraceID: "trace-platform-unreachable",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LoginState != db.AccountAuthLoggedIn || event.PersistedLoginState != db.AccountAuthLoggedIn {
		t.Fatalf("updated=%+v event=%+v", updated, event)
	}
}

func TestAccountObservationOnlyExplicitDOMCanPersistLogout(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("kuaishou", "快手账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}

	_, err = database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedOut, BrowserState: "ready",
		ReasonCode: db.LoginReasonProfileSelectorMissing, EvidenceCode: db.LoginEvidenceMissing,
		TraceID: "trace-invalid-logout",
	})
	if err == nil || !strings.Contains(err.Error(), "explicit login or expiry evidence") {
		t.Fatalf("err=%v", err)
	}
	stillLoggedIn, _ := database.GetAccount(account.ID)
	if stillLoggedIn.LoginState != db.AccountAuthLoggedIn {
		t.Fatalf("invalid logout changed state: %+v", stillLoggedIn)
	}

	_, err = database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedOut, BrowserState: "ready",
		ReasonCode: db.LoginReasonLoginRequiredPage, EvidenceCode: db.LoginEvidenceExplicitLoginDOM,
		TraceID: "trace-valid-logout",
	})
	if err != nil {
		t.Fatal(err)
	}
	loggedOut, _ := database.GetAccount(account.ID)
	if loggedOut.LoginState != db.AccountAuthLoggedOut {
		t.Fatalf("explicit logout was not persisted: %+v", loggedOut)
	}
}

func TestAccountObservationPersistsObservedIdentityAndAvatarProvenance(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("douyin", "douyin-7000", "")
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 64)
	event, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		Name: "真实抖音账号", AccountIdentifier: "DY-123", AvatarSource: db.AvatarSourceObserved,
		AvatarRef: "account-avatars/account-1-aaaaaaaaaaaaaaaa.png", AvatarSHA256: sha,
		TraceID: "trace-profile",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "真实抖音账号" || updated.AccountIdentifier != "DY-123" || updated.AvatarSource != db.AvatarSourceObserved || updated.AvatarSHA256 != sha || updated.AvatarRefreshedAt == "" {
		t.Fatalf("account=%+v", updated)
	}
	if !strings.HasPrefix(updated.Avatar, "account-avatars/") || !event.ProfileChanged || event.AvatarSource != db.AvatarSourceObserved {
		t.Fatalf("account=%+v event=%+v", updated, event)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) == 0 || assets[0].Name != "真实抖音账号" || assets[0].Avatar != updated.Avatar {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
}

func TestAccountObservationIdentityChangeWithoutFreshAvatarClearsPreviousAvatar(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("kuaishou", "旧账号", "")
	if err != nil {
		t.Fatal(err)
	}
	oldSHA := strings.Repeat("a", 64)
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		Name: "旧账号", AccountIdentifier: "old-id", AvatarSource: db.AvatarSourceObserved,
		AvatarRef: "account-avatars/account-1-aaaaaaaaaaaaaaaa.png", AvatarSHA256: oldSHA,
		ObservedAt: "2026-07-20T10:00:00Z", TraceID: "old-profile",
	}); err != nil {
		t.Fatal(err)
	}

	event, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		Name: "新账号", AccountIdentifier: "new-id", AvatarSource: db.AvatarSourcePlatformFallback,
		ObservedAt: "2026-07-20T10:01:00Z", TraceID: "new-profile-no-avatar",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "新账号" || updated.AccountIdentifier != "new-id" {
		t.Fatalf("identity was not updated: %+v", updated)
	}
	if updated.Avatar != "" || updated.AvatarSHA256 != "" || updated.AvatarRefreshedAt != "" || updated.AvatarSource != db.AvatarSourcePlatformFallback {
		t.Fatalf("stale avatar remained bound to the new identity: %+v", updated)
	}
	if event.AvatarSHA256 != "" || event.AvatarSource != db.AvatarSourcePlatformFallback || !event.ProfileChanged {
		t.Fatalf("event retained stale avatar provenance: %+v", event)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 || assets[0].Avatar != "" {
		t.Fatalf("browser account asset retained stale avatar: assets=%+v err=%v", assets, err)
	}
}

func TestAccountObservationRejectsPagePlaceholderAsProfileName(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("tiktok", "真实 TikTok 昵称", "")
	if err != nil {
		t.Fatal(err)
	}
	event, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		Name: " 暂无个人简介 ", AccountIdentifier: "creator-88",
		AvatarSource: db.AvatarSourcePlatformFallback, TraceID: "placeholder-profile",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "真实 TikTok 昵称" || updated.AccountIdentifier != "creator-88" {
		t.Fatalf("placeholder corrupted profile: %+v", updated)
	}
	if event.Name != "真实 TikTok 昵称" {
		t.Fatalf("event retained placeholder: %+v", event)
	}
}

func TestLoggedInObservationRepairsStaleBrowserAccountAsset(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "墨衍 MoGrow", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE accounts SET account_identifier='weixin_49486457' WHERE id=?`, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE account_assets SET name='阿尔法智能',external_id='',last_seen_at='' WHERE account_id=? AND asset_type='browser_account'`, account.ID); err != nil {
		t.Fatal(err)
	}

	observedAt := "2026-08-01T08:00:00Z"
	if _, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		Name: "墨衍 MoGrow", AccountIdentifier: "weixin_49486457",
		AvatarSource: db.AvatarSourcePlatformFallback, ObservedAt: observedAt, TraceID: "repair-stale-browser-asset",
	}); err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	if assets[0].Name != "墨衍 MoGrow" || assets[0].ExternalID != "weixin_49486457" || assets[0].LastSeenAt != observedAt {
		t.Fatalf("stale browser asset was not repaired: %+v", assets[0])
	}
}

func TestMigrateReconcilesStaleBrowserAccountAssetWithoutBrowserAccess(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("csdn", "墨衍 MoGrow", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE accounts SET account_identifier='weixin_49486457',last_check_at='2026-08-01T08:05:00Z' WHERE id=?`, account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE account_assets SET name='阿尔法智能',external_id='',last_seen_at='' WHERE account_id=? AND asset_type='browser_account'`, account.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Migrate(); err != nil {
		t.Fatal(err)
	}
	assets, err := reopened.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	if assets[0].Name != "墨衍 MoGrow" || assets[0].ExternalID != "weixin_49486457" || assets[0].LastSeenAt != "2026-08-01T08:05:00Z" {
		t.Fatalf("migration did not reconcile browser asset: %+v", assets[0])
	}
}

func TestAccountObservationDeduplicatesUnchangedEvents(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "xhs", "")
	if err != nil {
		t.Fatal(err)
	}
	input := db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthUnknown, BrowserState: "ready",
		ReasonCode: db.LoginReasonProfileSelectorMissing, EvidenceCode: db.LoginEvidenceMissing,
		TraceID: "trace-one", ObservedAt: "2026-07-20T01:00:00Z",
	}
	first, err := database.RecordAccountLoginObservation(input)
	if err != nil {
		t.Fatal(err)
	}
	input.TraceID = "trace-two"
	input.ObservedAt = "2026-07-20T01:01:00Z"
	second, err := database.RecordAccountLoginObservation(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("unchanged observation created duplicate ids %d and %d", first.ID, second.ID)
	}
	if !first.EvidenceChanged || second.EvidenceChanged {
		t.Fatalf("evidence change flags first=%v second=%v", first.EvidenceChanged, second.EvidenceChanged)
	}
	if second.TraceID != "trace-two" || second.ObservedAt != input.ObservedAt {
		t.Fatalf("deduplicated event did not advance observation metadata: %+v", second)
	}
	events, err := database.ListAccountLoginEvents(account.ID, 50)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if events[0].TraceID != "trace-two" || events[0].ObservedAt != input.ObservedAt {
		t.Fatalf("persisted latest event is stale: %+v", events[0])
	}
}

func TestAccountObservationPersistsNonFatalAvatarErrorCode(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("wechat_video", "video", "")
	if err != nil {
		t.Fatal(err)
	}
	event, err := database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		AvatarErrorCode: db.AvatarErrorCaptureFailed, TraceID: "avatar-warning",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetAccount(account.ID)
	if err != nil || updated.LoginState != db.AccountAuthLoggedIn || event.AvatarErrorCode != db.AvatarErrorCaptureFailed {
		t.Fatalf("account=%+v event=%+v err=%v", updated, event, err)
	}
	_, err = database.RecordAccountLoginObservation(db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthLoggedIn, BrowserState: "ready",
		ReasonCode: db.LoginReasonVerifiedProfileDOM, EvidenceCode: db.LoginEvidenceReviewedProfileDOM,
		AvatarErrorCode: "NOT_A_STABLE_CODE", TraceID: "invalid-avatar-warning",
	})
	if err == nil || !strings.Contains(err.Error(), "avatar error code") {
		t.Fatalf("err=%v", err)
	}
}

func TestLatestBrowserExitEvidenceReturnsOnlyStableCodes(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		eventType string
		message   string
		want      string
	}{
		{name: "user stop", eventType: "stopped_by_user", message: "ignored detail", want: db.LoginEvidenceBrowserStoppedByUser},
		{name: "process exit", eventType: "process_exited", message: "ignored detail", want: db.LoginEvidenceBrowserProcessExited},
		{name: "missing after restart", eventType: "process_missing_after_restart", message: "ignored detail", want: db.LoginEvidenceBrowserProcessExited},
		{name: "supervisor shutdown", eventType: "supervisor_shutdown", message: "ignored detail", want: db.LoginEvidenceBrowserProcessExited},
		{name: "crash", eventType: "crashed", message: "sensitive crash detail", want: db.LoginEvidenceBrowserCrashed},
		{name: "heartbeat", eventType: "heartbeat_failed", message: "sensitive heartbeat detail", want: db.LoginEvidenceBrowserHeartbeatFailed},
		{name: "lease", eventType: "lease_lost", message: "sensitive lease detail", want: db.LoginEvidenceBrowserLeaseLost},
		{name: "legacy user-looking message is not authority", eventType: "stopped", message: "Closed by supervisor", want: db.LoginEvidenceMissing},
		{name: "legacy process-looking message is not authority", eventType: "stopped", message: "Process exited", want: db.LoginEvidenceMissing},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profileID := fmt.Sprintf("account-browser-evidence-%d", index)
			if _, err := database.Exec(`INSERT INTO browser_events (profile_id, event_type, message, pid, cdp_port, created_at) VALUES (?, ?, ?, 0, 0, '2026-07-20T10:00:01Z')`, profileID, test.eventType, test.message); err != nil {
				t.Fatal(err)
			}
			got, err := database.LatestBrowserExitEvidence(profileID, "2026-07-20T10:00:00Z")
			if err != nil || got != test.want {
				t.Fatalf("got=%q want=%q err=%v", got, test.want, err)
			}
			if strings.Contains(got, test.message) {
				t.Fatalf("raw message escaped: %q", got)
			}
		})
	}

	if _, err := database.Exec(`INSERT INTO browser_events (profile_id, event_type, message, pid, cdp_port, created_at) VALUES ('account-stale', 'crashed', 'old detail', 0, 0, '2026-07-20T09:59:59Z')`); err != nil {
		t.Fatal(err)
	}
	got, err := database.LatestBrowserExitEvidence("account-stale", "2026-07-20T10:00:00Z")
	if err != nil || got != db.LoginEvidenceMissing {
		t.Fatalf("stale evidence got=%q err=%v", got, err)
	}
}

func TestListBrowserLifecycleEventsReturnsSafeTimelineWithoutMessages(t *testing.T) {
	database, cleanup := setupDB(t)
	defer cleanup()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	profileID := "acct-42"
	rows := []struct {
		eventType string
		message   string
		reason    string
		exitCode  any
		signal    any
		createdAt string
	}{
		{eventType: "started", message: "must not be returned", reason: "PROCESS_START_REQUESTED", createdAt: "2026-07-20T10:00:00Z"},
		{eventType: "focus_succeeded", message: "must not be returned", createdAt: "2026-07-20T10:00:30Z"},
		{eventType: "crashed", message: "private process detail", reason: "PROCESS_EXIT_NONZERO", exitCode: 23, createdAt: "2026-07-20T10:01:00Z"},
	}
	for _, row := range rows {
		if _, err := database.Exec(`INSERT INTO browser_events
			(profile_id, event_type, message, reason_code, exit_code, term_signal, pid, cdp_port, created_at)
			VALUES (?, ?, ?, ?, ?, ?, 123, 15142, ?)`, profileID, row.eventType, row.message, row.reason, row.exitCode, row.signal, row.createdAt); err != nil {
			t.Fatal(err)
		}
	}

	events, err := database.ListBrowserLifecycleEvents(profileID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].EventType != "crashed" || events[1].EventType != "focus_succeeded" || events[2].EventType != "started" {
		t.Fatalf("events=%+v", events)
	}
	encoded := fmt.Sprintf("%+v", events)
	if strings.Contains(encoded, rows[0].message) || strings.Contains(encoded, rows[1].message) {
		t.Fatalf("browser event messages escaped into safe timeline: %s", encoded)
	}
	if events[0].PID != 123 || events[0].CDPPort != 15142 || events[0].CreatedAt != rows[2].createdAt || events[0].ReasonCode != "PROCESS_EXIT_NONZERO" || events[0].ExitCode == nil || *events[0].ExitCode != 23 || events[0].TermSignal != nil {
		t.Fatalf("event=%+v", events[0])
	}

	if _, err := database.ListBrowserLifecycleEvents("../profile", 20); err == nil {
		t.Fatal("unsafe profile id was accepted")
	}
}
