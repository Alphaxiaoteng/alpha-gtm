package egolite_test

import (
	"context"
	"reflect"
	"testing"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
)

type loginSyncVerifier struct {
	calls    [][]string
	accounts []db.Account
}

func (verifier *loginSyncVerifier) VerifyLoginBatch(_ context.Context, _ int64, platformCodes []string) (*egolite.LoginVerification, error) {
	verifier.calls = append(verifier.calls, append([]string(nil), platformCodes...))
	return &egolite.LoginVerification{Accounts: verifier.accounts}, nil
}

func TestLoginSyncMonitorCompletesSuccessfulSessionsWithoutElectronPolling(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("后台登录同步")
	if err != nil {
		t.Fatal(err)
	}
	douyin, err := database.CreateAccountForIdentity(identity.ID, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	wechat, err := database.CreateAccountForIdentity(identity.ID, "wechat_mp", "微信公众号", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []*db.Account{douyin, wechat} {
		session, createErr := database.CreateLoginSession(account.ID)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if updateErr := database.UpdateLoginSession(session.ID, "awaiting_login", ""); updateErr != nil {
			t.Fatal(updateErr)
		}
	}
	verifiedDouyin := *douyin
	verifiedDouyin.LoginState = db.AccountAuthLoggedIn
	unknownWechat := *wechat
	unknownWechat.LoginState = db.AccountAuthUnknown
	verifier := &loginSyncVerifier{accounts: []db.Account{verifiedDouyin, unknownWechat}}
	monitor := egolite.NewLoginSyncMonitor(database, verifier)

	result, err := monitor.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Checked != 2 || result.Verified != 1 || result.Pending != 1 {
		t.Fatalf("result=%+v", result)
	}
	if len(verifier.calls) != 1 || !reflect.DeepEqual(verifier.calls[0], []string{"douyin", "wechat_mp"}) {
		t.Fatalf("calls=%v", verifier.calls)
	}
	if session, sessionErr := database.ActiveLoginSession(douyin.ID); sessionErr != nil || session != nil {
		t.Fatalf("verified session remained active: session=%+v err=%v", session, sessionErr)
	}
	if session, sessionErr := database.ActiveLoginSession(wechat.ID); sessionErr != nil || session == nil {
		t.Fatalf("unknown session did not remain pending: session=%+v err=%v", session, sessionErr)
	}
}

func TestLoginSyncMonitorCancelsExpiredSessionWithoutOpeningEgoLite(t *testing.T) {
	database := openServiceDB(t)
	identity, err := database.CreateEgoIdentity("过期登录同步")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, "wechat_mp", "微信公众号", "")
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
	if _, err := database.Exec(`UPDATE login_sessions SET expires_at='2020-01-01T00:00:00Z' WHERE id=?`, session.ID); err != nil {
		t.Fatal(err)
	}
	verifier := &loginSyncVerifier{}
	monitor := egolite.NewLoginSyncMonitor(database, verifier)

	result, err := monitor.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Expired != 1 || len(verifier.calls) != 0 {
		t.Fatalf("result=%+v calls=%v", result, verifier.calls)
	}
	if active, activeErr := database.ActiveLoginSession(account.ID); activeErr != nil || active != nil {
		t.Fatalf("expired session remained active: active=%+v err=%v", active, activeErr)
	}
}
