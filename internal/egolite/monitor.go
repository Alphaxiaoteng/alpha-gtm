package egolite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/notify"
	"alpha-nexus-egolite/internal/trace"
)

const AccountAuthManualTakeover = "manual_takeover"

// sessionMonitorCatchUpDelay is the short grace period an overdue monitor
// waits after boot before catching up, so startup is not hammered.
const sessionMonitorCatchUpDelay = time.Minute

type SessionCheck struct {
	AuthState         string
	Name              string
	AccountIdentifier string
	AvatarRef         string
	AvatarURL         string
	AvatarSHA256      string
	AvatarErrorCode   string
	ReasonCode        string
	EvidenceCode      string
	PageOrigin        string
}

type SessionAccountChecker interface {
	CheckAccountSession(context.Context, db.Account) (SessionCheck, error)
}

// SessionAccountCheckerFunc adapts a checked function without introducing a
// second session-monitor implementation at the UI boundary.
type SessionAccountCheckerFunc func(context.Context, db.Account) (SessionCheck, error)

func (checker SessionAccountCheckerFunc) CheckAccountSession(ctx context.Context, account db.Account) (SessionCheck, error) {
	return checker(ctx, account)
}

type SessionStateNotifier interface {
	NotifyAccountChange(notify.AccountStateChange) error
}

type SessionMonitorResult struct {
	Checked        int `json:"checked"`
	LoggedIn       int `json:"logged_in"`
	LoggedOut      int `json:"logged_out"`
	Unknown        int `json:"unknown"`
	ManualTakeover int `json:"manual_takeover"`
	Failed         int `json:"failed"`
}

type SessionMonitor struct {
	db       *db.DB
	checker  SessionAccountChecker
	notifier SessionStateNotifier
	now      func() time.Time
	after    func(time.Duration) <-chan time.Time
	wake     chan struct{}
}

func NewSessionMonitor(database *db.DB, checker SessionAccountChecker, notifier SessionStateNotifier) *SessionMonitor {
	return &SessionMonitor{
		db: database, checker: checker, notifier: notifier,
		now: time.Now, after: time.After, wake: make(chan struct{}, 1),
	}
}

func (monitor *SessionMonitor) Wake() {
	if monitor == nil {
		return
	}
	select {
	case monitor.wake <- struct{}{}:
	default:
	}
}

func (monitor *SessionMonitor) Run(ctx context.Context) (result *SessionMonitorResult, returnErr error) {
	if monitor == nil || monitor.db == nil || monitor.checker == nil {
		return nil, fmt.Errorf("SESSION_MONITOR_UNAVAILABLE")
	}
	now := monitor.now().UTC()
	if _, err := monitor.db.BeginEgoSessionMonitorRun(now); err != nil {
		return nil, err
	}
	result = &SessionMonitorResult{}
	errorCode := ""
	defer func() {
		if returnErr != nil {
			errorCode = "SESSION_MONITOR_RUN_FAILED"
		}
		returnErr = errors.Join(returnErr, monitor.db.FinishEgoSessionMonitorRun(monitor.now().UTC(), errorCode, result))
	}()

	identities, err := monitor.db.ListEgoIdentities()
	if err != nil {
		return result, err
	}
	for _, identity := range identities {
		if !identity.Enabled {
			continue
		}
		space, err := monitor.db.GetEgoSpaceByIdentity(identity.ID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return result, err
		}
		accounts, err := monitor.db.ListAccountsByIdentity(identity.ID)
		if err != nil {
			return result, err
		}
		identityNeedsManualTakeover := false
		for _, account := range accounts {
			if !account.Enabled {
				continue
			}
			if err := ctx.Err(); err != nil {
				return result, err
			}
			previous := account.LoginState
			check, checkErr := monitor.checker.CheckAccountSession(ctx, account)
			result.Checked++
			if checkErr != nil {
				result.Failed++
				check = SessionCheck{
					AuthState:    db.AccountAuthUnknown,
					ReasonCode:   db.LoginReasonPlatformUnreachable,
					EvidenceCode: db.LoginEvidenceMissing,
				}
			}
			if check.AuthState == AccountAuthManualTakeover {
				identityNeedsManualTakeover = true
				result.ManualTakeover++
				check.AuthState = db.AccountAuthUnknown
				if check.ReasonCode == "" {
					check.ReasonCode = db.LoginReasonCheckpointRequired
				}
				if check.EvidenceCode == "" {
					check.EvidenceCode = db.LoginEvidenceCheckpointDOM
				}
				if space.State != "manual_takeover" {
					if _, err := monitor.db.CreateNotification(
						"session_monitor", "warn", "平台需要人工登录",
						fmt.Sprintf("%s 的 %s 需要扫码、验证码或人工确认。", identity.Name, account.PlatformCode),
						"account", account.ID,
					); err != nil {
						return result, err
					}
				}
				updatedSpace, err := monitor.db.UpdateEgoSpaceStateForIdentity(identity.ID, "agentDelegatedToUser", "manual_takeover", "CHECKPOINT_REQUIRED")
				if err != nil {
					return result, err
				}
				space = updatedSpace
			}
			switch check.AuthState {
			case db.AccountAuthLoggedIn:
				result.LoggedIn++
			case db.AccountAuthLoggedOut:
				result.LoggedOut++
			default:
				result.Unknown++
				check.AuthState = db.AccountAuthUnknown
			}
			if check.ReasonCode == "" {
				check.ReasonCode = db.LoginReasonProfileSelectorMissing
			}
			if check.EvidenceCode == "" {
				check.EvidenceCode = db.LoginEvidenceMissing
			}
			avatarSource := db.AvatarSourcePlatformFallback
			if check.AvatarRef != "" {
				avatarSource = db.AvatarSourceObserved
			}
			observedAt := monitor.now().UTC().Format(time.RFC3339)
			if _, err := monitor.db.RecordAccountLoginObservation(db.AccountLoginObservation{
				AccountID: account.ID, AuthState: check.AuthState, BrowserState: "ready",
				LoginMode: db.LoginModeBrowserProfileSession, AuthMethod: db.AuthMethodUnknown,
				ReasonCode: check.ReasonCode, EvidenceCode: check.EvidenceCode,
				PageOrigin: safePageOrigin(check.PageOrigin), Name: check.Name,
				AccountIdentifier: check.AccountIdentifier, AvatarRef: check.AvatarRef,
				AvatarSource: avatarSource, AvatarSHA256: check.AvatarSHA256,
				AvatarErrorCode: check.AvatarErrorCode, ObservedAt: observedAt,
				TraceID: string(trace.New()),
			}); err != nil {
				return result, err
			}
			if check.AuthState == db.AccountAuthLoggedIn {
				if avatarRef := SafeAvatarURL(check.AvatarURL); avatarRef != "" {
					if err := monitor.db.UpdateAccountAvatarFallback(account.ID, avatarRef, observedAt); err != nil {
						return result, err
					}
				}
			}
			updated, err := monitor.db.GetAccount(account.ID)
			if err != nil {
				return result, err
			}
			if monitor.notifier != nil && previous != updated.LoginState {
				_ = monitor.notifier.NotifyAccountChange(notify.AccountStateChange{
					AccountID: updated.ID, PlatformCode: updated.PlatformCode, AccountName: updated.Name,
					Previous: previous, Current: updated.LoginState, ReasonCode: check.ReasonCode,
				})
			}
		}
		if !identityNeedsManualTakeover && space.ErrorCode == "CHECKPOINT_REQUIRED" {
			updatedSpace, err := monitor.db.UpdateEgoSpaceStateForIdentity(
				identity.ID, space.Ownership, space.State, "",
			)
			if err != nil {
				return result, err
			}
			space = updatedSpace
		}
	}
	return result, nil
}

func (monitor *SessionMonitor) Start(ctx context.Context, waitGroup *sync.WaitGroup) {
	if monitor == nil || ctx == nil || waitGroup == nil {
		return
	}
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		for {
			settings, err := monitor.db.EgoSessionMonitorSettings()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case <-monitor.wake:
					continue
				case <-monitor.after(time.Minute):
					continue
				}
			}
			if !settings.Enabled {
				select {
				case <-ctx.Done():
					return
				case <-monitor.wake:
					continue
				}
			}
			delay := time.Duration(settings.IntervalMinutes) * time.Minute
			if last, parseErr := time.Parse(time.RFC3339, settings.LastFinishedAt); parseErr == nil && !last.IsZero() {
				elapsed := monitor.now().UTC().Sub(last)
				if elapsed < 0 {
					elapsed = 0
				}
				if elapsed >= delay {
					// Restarts more frequent than the interval must not push
					// the next check a full interval away again: an overdue
					// monitor catches up shortly after boot.
					delay = sessionMonitorCatchUpDelay
				} else {
					delay -= elapsed
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-monitor.wake:
				continue
			case <-monitor.after(delay):
				if _, runErr := monitor.Run(ctx); runErr != nil {
					fmt.Printf("ego session monitor run failed: %v\n", runErr)
				}
			}
		}
	}()
}
