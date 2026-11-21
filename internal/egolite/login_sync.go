package egolite

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/db"
)

const (
	loginSyncActiveInterval = 2 * time.Second
	loginSyncIdleInterval   = 30 * time.Second
	loginSyncBatchLimit     = 3
	loginSyncMaxInterval    = 15 * time.Second
)

func loginSyncRetryInterval(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= 3 {
		return loginSyncMaxInterval
	}
	delay := loginSyncActiveInterval << attempt
	if delay > loginSyncMaxInterval {
		return loginSyncMaxInterval
	}
	return delay
}

type LoginBatchVerifier interface {
	VerifyLoginBatch(context.Context, int64, []string) (*LoginVerification, error)
}

type LoginBatchVerifierFunc func(context.Context, int64, []string) (*LoginVerification, error)

func (verify LoginBatchVerifierFunc) VerifyLoginBatch(ctx context.Context, identityID int64, platformCodes []string) (*LoginVerification, error) {
	return verify(ctx, identityID, platformCodes)
}

type LoginSyncResult struct {
	Checked  int `json:"checked"`
	Verified int `json:"verified"`
	Pending  int `json:"pending"`
	Expired  int `json:"expired"`
	Failed   int `json:"failed"`
}

type LoginSyncMonitor struct {
	db       *db.DB
	verifier LoginBatchVerifier
	now      func() time.Time
	after    func(time.Duration) <-chan time.Time
	wake     chan struct{}
	verifyMu sync.Mutex
}

func NewLoginSyncMonitor(database *db.DB, verifier LoginBatchVerifier) *LoginSyncMonitor {
	return &LoginSyncMonitor{
		db: database, verifier: verifier, now: time.Now, after: time.After,
		wake: make(chan struct{}, 1),
	}
}

func (monitor *LoginSyncMonitor) Wake() {
	if monitor == nil {
		return
	}
	select {
	case monitor.wake <- struct{}{}:
	default:
	}
}

// VerifyLoginBatch serializes the short-lived login observer used by both the
// HTTP endpoint and the background sync loop. It never keeps an Agent process
// alive between checks.
func (monitor *LoginSyncMonitor) VerifyLoginBatch(ctx context.Context, identityID int64, platformCodes []string) (*LoginVerification, error) {
	if monitor == nil || monitor.verifier == nil {
		return nil, errors.New("EGO_LOGIN_SYNC_UNAVAILABLE")
	}
	monitor.verifyMu.Lock()
	defer monitor.verifyMu.Unlock()
	return monitor.verifier.VerifyLoginBatch(ctx, identityID, platformCodes)
}

type pendingLoginSync struct {
	session  db.LoginSession
	account  db.Account
	identity int64
}

func (monitor *LoginSyncMonitor) RunOnce(ctx context.Context) (*LoginSyncResult, error) {
	result := &LoginSyncResult{}
	if monitor == nil || monitor.db == nil || monitor.verifier == nil {
		return result, errors.New("EGO_LOGIN_SYNC_UNAVAILABLE")
	}
	sessions, err := monitor.db.ListLoginSessions(200)
	if err != nil {
		return result, err
	}
	now := monitor.now().UTC()
	groups := make(map[int64][]pendingLoginSync)
	var runErr error
	for _, session := range sessions {
		if session.Status != "awaiting_login" && session.Status != "opening_browser" {
			continue
		}
		expiresAt, parseErr := time.Parse(time.RFC3339, session.ExpiresAt)
		if parseErr != nil || !expiresAt.After(now) {
			if updateErr := monitor.db.UpdateLoginSession(session.ID, "cancelled", "LOGIN_SESSION_EXPIRED"); updateErr != nil {
				runErr = errors.Join(runErr, updateErr)
				result.Failed++
			} else {
				result.Expired++
			}
			continue
		}
		account, accountErr := monitor.db.GetAccount(session.AccountID)
		if accountErr != nil {
			result.Failed++
			runErr = errors.Join(runErr, accountErr)
			continue
		}
		result.Checked++
		if account.LoginState == db.AccountAuthLoggedIn {
			if updateErr := monitor.db.UpdateLoginSession(session.ID, "verified", ""); updateErr != nil {
				result.Failed++
				runErr = errors.Join(runErr, updateErr)
			} else {
				result.Verified++
			}
			continue
		}
		groups[account.IdentityID] = append(groups[account.IdentityID], pendingLoginSync{
			session: session, account: *account, identity: account.IdentityID,
		})
	}

	identityIDs := make([]int64, 0, len(groups))
	for identityID := range groups {
		identityIDs = append(identityIDs, identityID)
	}
	sort.Slice(identityIDs, func(left, right int) bool { return identityIDs[left] < identityIDs[right] })
	for _, identityID := range identityIDs {
		items := groups[identityID]
		sort.Slice(items, func(left, right int) bool { return items[left].account.ID < items[right].account.ID })
		if len(items) > loginSyncBatchLimit {
			result.Pending += len(items) - loginSyncBatchLimit
			items = items[:loginSyncBatchLimit]
		}
		codes := make([]string, 0, len(items))
		for _, item := range items {
			codes = append(codes, item.account.PlatformCode)
		}
		verification, verifyErr := monitor.VerifyLoginBatch(ctx, identityID, codes)
		if verifyErr != nil {
			result.Failed += len(items)
			result.Pending += len(items)
			runErr = errors.Join(runErr, verifyErr)
			continue
		}
		verifiedByID := make(map[int64]bool, len(verification.Accounts))
		for _, account := range verification.Accounts {
			verifiedByID[account.ID] = account.LoginState == db.AccountAuthLoggedIn
		}
		for _, item := range items {
			if !verifiedByID[item.account.ID] {
				result.Pending++
				continue
			}
			if updateErr := monitor.db.UpdateLoginSession(item.session.ID, "verified", ""); updateErr != nil {
				result.Failed++
				runErr = errors.Join(runErr, updateErr)
				continue
			}
			result.Verified++
		}
	}
	return result, runErr
}

func (monitor *LoginSyncMonitor) Start(ctx context.Context, waitGroup *sync.WaitGroup) {
	if monitor == nil || ctx == nil || waitGroup == nil {
		return
	}
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		interval := time.Duration(0)
		pendingAttempts := 0
		for {
			woke := false
			if interval > 0 {
				select {
				case <-ctx.Done():
					return
				case <-monitor.wake:
					woke = true
				case <-monitor.after(interval):
				}
			}
			if woke {
				pendingAttempts = 0
			}
			result, _ := monitor.RunOnce(ctx)
			if ctx.Err() != nil {
				return
			}
			if result.Pending > 0 || result.Failed > 0 {
				if result.Verified > 0 {
					pendingAttempts = 0
				}
				interval = loginSyncRetryInterval(pendingAttempts)
				pendingAttempts++
			} else {
				interval = loginSyncIdleInterval
				pendingAttempts = 0
			}
		}
	}()
}
