//go:build legacy_chromium

package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/logincheck"
	"alpha-nexus-egolite/internal/notify"
	"alpha-nexus-egolite/internal/observability"
	"alpha-nexus-egolite/internal/trace"
)

const accountObservationInterval = time.Second

// A cold CDP attach can exceed one scheduler tick even while the browser is
// healthy. Keep probes concurrent, but allow a short bounded attach so a
// verified account is not downgraded by a startup-only false timeout.
const accountQuickProbeTimeout = 2 * time.Second
const accountQuickProbeLeaseTTL = 15 * time.Second
const accountFastConfirmationTimeout = 7 * time.Second
const accountObservationLeaseTTL = 90 * time.Second
const accountManualPlatformTabWait = 8 * time.Second
const accountManualPlatformTabPoll = 250 * time.Millisecond

type accountStatusLookup func(accountID int64) (*browser.AccountStatus, error)
type passiveAccountObserve func(context.Context, string, int) (*logincheck.Observation, error)

type fastProbeResult struct {
	observation *logincheck.Observation
	probeErr    error
	leaseBusy   bool
	storageErr  error
	release     func()
}

type AccountObservationResponse struct {
	AccountID           int64  `json:"accountId"`
	PlatformCode        string `json:"platformCode"`
	AuthState           string `json:"authState"`
	PersistedLoginState string `json:"persistedLoginState"`
	BrowserState        string `json:"browserState"`
	LoginMode           string `json:"loginMode"`
	AuthMethod          string `json:"authMethod"`
	ReasonCode          string `json:"reasonCode"`
	EvidenceCode        string `json:"evidenceCode"`
	Name                string `json:"name,omitempty"`
	AccountIdentifier   string `json:"accountIdentifier,omitempty"`
	Avatar              string `json:"avatar,omitempty"`
	AvatarSource        string `json:"avatarSource"`
	AvatarSHA256        string `json:"avatarSha256,omitempty"`
	AvatarRefreshedAt   string `json:"avatarRefreshedAt,omitempty"`
	AvatarErrorCode     string `json:"avatarErrorCode,omitempty"`
	ProfileChanged      bool   `json:"profileChanged"`
	PageOrigin          string `json:"pageOrigin,omitempty"`
	ObservedAt          string `json:"observedAt"`
	TraceID             string `json:"traceId"`
	SessionBackupState  string `json:"sessionBackupState,omitempty"`
}

type AccountObserverStatus struct {
	Running         bool   `json:"running"`
	Observing       bool   `json:"observing"`
	IntervalSeconds int    `json:"intervalSeconds"`
	LastStartedAt   string `json:"lastStartedAt,omitempty"`
	LastFinishedAt  string `json:"lastFinishedAt,omitempty"`
	LastErrorCode   string `json:"lastErrorCode,omitempty"`
	LastCount       int    `json:"lastCount"`
}

type accountObserver struct {
	db            *db.DB
	dataDir       string
	status        accountStatusLookup
	observe       passiveAccountObserve
	probe         passiveAccountObserve
	onStateChange func(notify.AccountStateChange) error
	events        observability.Emitter
	interval      time.Duration

	cycleMu              sync.Mutex
	stateMu              sync.Mutex
	trackedMu            sync.Mutex
	state                AccountObserverStatus
	tracked              map[int64]bool
	runningSessions      map[int64]string
	avatarMu             sync.Mutex
	avatarRefreshPending map[int64]bool
	avatarRefresh        chan int64
	startOnce            sync.Once
}

func newAccountObserver(database *db.DB, dataDir string, status accountStatusLookup) *accountObserver {
	return &accountObserver{
		db: database, dataDir: dataDir, status: status,
		observe: logincheck.ObservePassive, probe: logincheck.ObserveDOMPassive, interval: accountObservationInterval,
		tracked:              make(map[int64]bool),
		runningSessions:      make(map[int64]string),
		avatarRefreshPending: make(map[int64]bool),
		avatarRefresh:        make(chan int64, 32),
		state:                AccountObserverStatus{IntervalSeconds: int(accountObservationInterval / time.Second)},
	}
}

func (observer *accountObserver) Start(ctx context.Context, waitGroup *sync.WaitGroup) {
	if observer == nil || ctx == nil || waitGroup == nil {
		return
	}
	observer.startOnce.Do(func() {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case accountID := <-observer.avatarRefresh:
					observer.refreshObservedAvatar(ctx, accountID)
				}
			}
		}()
		observer.stateMu.Lock()
		observer.state.Running = true
		observer.stateMu.Unlock()
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			defer func() {
				observer.stateMu.Lock()
				observer.state.Running = false
				observer.state.Observing = false
				observer.stateMu.Unlock()
			}()
			observer.observeFastCycle(ctx, true)
			timer := time.NewTimer(observer.interval)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					// ponytail: the one-second monitor persists bounded DOM evidence only.
					// A confirmed login queues its full profile/avatar capture separately,
					// so one slow page cannot delay every account's status transition.
					observer.observeFastCycle(ctx, true)
					timer.Reset(observer.interval)
				}
			}
		}()
	})
}

func (observer *accountObserver) Status() AccountObserverStatus {
	if observer == nil {
		return AccountObserverStatus{IntervalSeconds: int(accountObservationInterval / time.Second)}
	}
	observer.stateMu.Lock()
	defer observer.stateMu.Unlock()
	return observer.state
}

// ObserveRunning observes only currently running instances, in account order.
// A failed account is converted to a durable observation and never aborts the
// rest of the batch.
func (observer *accountObserver) ObserveRunning(ctx context.Context) ([]AccountObservationResponse, error) {
	return observer.observeCycle(ctx, false)
}

func (observer *accountObserver) observeCycle(ctx context.Context, includeTrackedOffline bool) ([]AccountObservationResponse, error) {
	return observer.observeCycleMode(ctx, includeTrackedOffline, true, true)
}

func (observer *accountObserver) observeFastCycle(ctx context.Context, includeTrackedOffline bool) ([]AccountObservationResponse, error) {
	return observer.observeCycleMode(ctx, includeTrackedOffline, false, false)
}

func (observer *accountObserver) observeCycleMode(ctx context.Context, includeTrackedOffline, fullProfile, reportObserving bool) ([]AccountObservationResponse, error) {
	if observer == nil || observer.db == nil || observer.status == nil {
		return nil, fmt.Errorf("account observer is not initialized")
	}
	observer.cycleMu.Lock()
	defer observer.cycleMu.Unlock()

	startedAt := time.Now().UTC().Format(time.RFC3339)
	observer.stateMu.Lock()
	if reportObserving {
		observer.state.Observing = true
	}
	observer.state.LastStartedAt = startedAt
	observer.state.LastErrorCode = ""
	observer.stateMu.Unlock()
	defer func() {
		observer.stateMu.Lock()
		if reportObserving {
			observer.state.Observing = false
		}
		observer.state.LastFinishedAt = time.Now().UTC().Format(time.RFC3339)
		observer.stateMu.Unlock()
	}()

	accounts, err := observer.db.ListAccounts()
	if err != nil {
		observer.setCycleResult(0, "DATABASE_READ_FAILED")
		return nil, err
	}
	supported := make(map[string]bool)
	for _, code := range logincheck.SupportedObservedPlatforms() {
		supported[code] = true
	}
	type runtimeState struct {
		status    *browser.AccountStatus
		statusErr error
		running   bool
	}
	runtimes := make(map[int64]runtimeState, len(accounts))
	for index := range accounts {
		account := &accounts[index]
		if !account.Enabled || !supported[account.PlatformCode] {
			continue
		}
		status, statusErr := observer.status(account.ID)
		runtimes[account.ID] = runtimeState{
			status: status, statusErr: statusErr,
			running: statusErr == nil && status != nil && status.Running && status.CDPPort > 0,
		}
	}
	probeResults := make(map[int64]fastProbeResult, len(runtimes))
	if !fullProfile {
		var probeMu sync.Mutex
		var probeGroup sync.WaitGroup
		for index := range accounts {
			account := &accounts[index]
			runtime := runtimes[account.ID]
			if !runtime.running {
				continue
			}
			probeGroup.Add(1)
			go func(account *db.Account, status *browser.AccountStatus) {
				defer probeGroup.Done()
				probe := observer.fastObservation(ctx, account, status)
				probeMu.Lock()
				probeResults[account.ID] = probe
				probeMu.Unlock()
			}(account, runtime.status)
		}
		probeGroup.Wait()
		defer func() {
			for _, probe := range probeResults {
				if probe.release != nil {
					probe.release()
				}
			}
		}()
	}
	results := make([]AccountObservationResponse, 0, len(accounts))
	observedCount := 0
	for index := range accounts {
		if err := ctx.Err(); err != nil {
			observer.setCycleResult(len(results), "CONTEXT_CANCELLED")
			return results, err
		}
		account := &accounts[index]
		if !account.Enabled || !supported[account.PlatformCode] {
			continue
		}
		runtime := runtimes[account.ID]
		status, statusErr, running := runtime.status, runtime.statusErr, runtime.running
		observer.trackedMu.Lock()
		tracked := observer.tracked[account.ID]
		previousSession := observer.runningSessions[account.ID]
		currentSession := ""
		if running && status != nil {
			currentSession = strconv.Itoa(status.PID) + ":" + strconv.Itoa(status.CDPPort)
			observer.runningSessions[account.ID] = currentSession
		} else {
			delete(observer.runningSessions, account.ID)
		}
		if running {
			observer.tracked[account.ID] = true
		}
		observer.trackedMu.Unlock()
		freshBrowserSession := running && currentSession != "" && currentSession != previousSession
		persistedTracking := account.LastCheckAt != "" || account.LoginState == db.AccountAuthLoggedIn || account.LoginState == db.AccountAuthLoggedOut
		if !running && !(includeTrackedOffline && (tracked || persistedTracking)) {
			continue
		}
		observedCount++
		if running && !fullProfile {
			probe := probeResults[account.ID]
			if probe.storageErr != nil {
				observer.stateMu.Lock()
				observer.state.LastErrorCode = "OBSERVATION_PERSIST_FAILED"
				observer.stateMu.Unlock()
				continue
			}
			result, changed, observeErr := observer.recordFastObservation(ctx, account, status, statusErr, probe)
			if observeErr != nil {
				observer.stateMu.Lock()
				observer.state.LastErrorCode = "OBSERVATION_PERSIST_FAILED"
				observer.stateMu.Unlock()
				continue
			}
			if changed && result != nil {
				results = append(results, *result)
				observer.queueAvatarRefresh(*result)
			}
			// A browser restart retains the durable logged_in state, so its fast
			// evidence can be unchanged. Still run one bounded full observation
			// for this new local browser session to refresh account name/avatar.
			if freshBrowserSession && probe.observation != nil && probe.observation.AuthState == logincheck.AuthLoggedIn {
				observer.queueAvatarRefresh(AccountObservationResponse{AccountID: account.ID, AuthState: db.AccountAuthLoggedIn})
			}
			continue
		}
		result, observeErr := observer.observeAccount(ctx, account, status, statusErr)
		if observeErr != nil {
			observer.stateMu.Lock()
			observer.state.LastErrorCode = "OBSERVATION_PERSIST_FAILED"
			observer.stateMu.Unlock()
			continue
		}
		results = append(results, *result)
	}
	observer.setCycleResult(observedCount, "")
	return results, nil
}

// queueAvatarRefresh upgrades a confirmed DOM login to one bounded full
// observation. The fast monitor stays responsive while each new login/browser
// session refreshes the verified account identity and avatar together.
func (observer *accountObserver) queueAvatarRefresh(result AccountObservationResponse) {
	if observer == nil || result.AuthState != db.AccountAuthLoggedIn {
		return
	}
	observer.avatarMu.Lock()
	defer observer.avatarMu.Unlock()
	if observer.avatarRefreshPending[result.AccountID] {
		return
	}
	observer.avatarRefreshPending[result.AccountID] = true
	select {
	case observer.avatarRefresh <- result.AccountID:
	default:
		delete(observer.avatarRefreshPending, result.AccountID)
	}
}

func (observer *accountObserver) refreshObservedAvatar(ctx context.Context, accountID int64) {
	defer func() {
		observer.avatarMu.Lock()
		delete(observer.avatarRefreshPending, accountID)
		observer.avatarMu.Unlock()
	}()
	account, err := observer.db.GetAccount(accountID)
	if err != nil || !account.Enabled {
		return
	}
	status, statusErr := observer.status(accountID)
	if statusErr != nil || status == nil || !status.Running || status.CDPPort <= 0 {
		return
	}
	// observeAccount uses the existing per-profile lease and captures an avatar
	// only when the platform-specific identity rule proves it belongs to this account.
	_, _ = observer.observeAccount(ctx, account, status, nil)
}

func (observer *accountObserver) fastObservation(ctx context.Context, account *db.Account, status *browser.AccountStatus) fastProbeResult {
	if observer.probe == nil || account == nil || status == nil || !status.Running || status.CDPPort <= 0 {
		return fastProbeResult{}
	}
	profileID := browser.ProfileIDForAccount(account.ID)
	leaseOwner := "observe-probe:" + string(trace.New())
	acquired, err := observer.db.AcquireBrowserOperationLease(profileID, "observe", leaseOwner, time.Now(), accountQuickProbeLeaseTTL)
	if err != nil {
		return fastProbeResult{storageErr: err}
	}
	if !acquired {
		return fastProbeResult{leaseBusy: true}
	}
	release := func() { _ = observer.db.ReleaseBrowserOperationLease(profileID, leaseOwner) }

	probeCtx, cancel := context.WithTimeout(ctx, accountQuickProbeTimeout)
	observation, err := observer.probe(probeCtx, account.PlatformCode, status.CDPPort)
	cancel()
	result := fastProbeResult{observation: observation, probeErr: err, release: release}
	if err != nil {
		return result
	}
	if observation == nil {
		result.observation = &logincheck.Observation{
			AuthState: logincheck.AuthUnknown, ReasonCode: logincheck.ReasonManualVerifyRequired,
			EvidenceCode: logincheck.EvidenceMissing,
		}
		return result
	}
	if !requiresFastTransitionConfirmation(account.LoginState, observation.AuthState) {
		return result
	}
	// Keep the same per-profile lease from the quick probe through confirmation
	// and persistence. A transient Meta dialog cannot demote a verified session,
	// and an older probe cannot overwrite a manual check that owns the profile.
	confirmCtx, confirmCancel := context.WithTimeout(ctx, accountFastConfirmationTimeout)
	confirmed, confirmErr := observer.observe(confirmCtx, account.PlatformCode, status.CDPPort)
	confirmCancel()
	if confirmErr != nil {
		result.observation = nil
		result.probeErr = confirmErr
		return result
	}
	if confirmed == nil {
		confirmed = &logincheck.Observation{
			AuthState: logincheck.AuthUnknown, ReasonCode: logincheck.ReasonManualVerifyRequired,
			EvidenceCode: logincheck.EvidenceMissing,
		}
	}
	result.observation = confirmed
	return result
}

func requiresFastTransitionConfirmation(persisted, observed string) bool {
	if persisted != db.AccountAuthLoggedIn && persisted != db.AccountAuthLoggedOut {
		return false
	}
	if observed != logincheck.AuthLoggedIn && observed != logincheck.AuthLoggedOut {
		return false
	}
	return persisted != observed
}

// recordFastObservation persists only changed bounded DOM evidence. It never
// reads cookies or writes an avatar; those remain on the explicit full check.
func (observer *accountObserver) recordFastObservation(ctx context.Context, account *db.Account, status *browser.AccountStatus, statusErr error, probe fastProbeResult) (*AccountObservationResponse, bool, error) {
	// Another local operation owning the profile is not login evidence. Keep the
	// last verified state stable so a one-second probe cannot hide an account
	// while the user starts a controlled fill or an explicit login check.
	if account == nil || probe.leaseBusy || (probe.probeErr == nil && probe.observation == nil) {
		return nil, false, nil
	}
	previousLoginState := account.LoginState
	now := time.Now().UTC().Format(time.RFC3339)
	traceID := string(trace.New())
	browserState := "unknown"
	if status != nil && status.State != "" {
		browserState = string(status.State)
	}
	input := db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthUnknown, BrowserState: browserState,
		LoginMode: account.LoginMode, AuthMethod: account.AuthMethod,
		ReasonCode: db.LoginReasonManualVerifyRequired, EvidenceCode: db.LoginEvidenceMissing,
		AvatarSource: db.AvatarSourcePlatformFallback, ObservedAt: now, TraceID: traceID,
	}
	if input.LoginMode == "" {
		input.LoginMode = db.LoginModeUnknown
	}
	if input.AuthMethod == "" {
		input.AuthMethod = db.AuthMethodUnknown
	}
	activeLoginSession, _ := observer.db.ActiveLoginSession(account.ID)
	if activeLoginSession != nil {
		input.LoginMode = db.LoginModeManualBrowser
	}
	switch {
	case statusErr != nil:
		input.ReasonCode = db.LoginReasonCDPUnreachable
	case status == nil || !status.Running || status.CDPPort <= 0:
		input.ReasonCode = db.LoginReasonBrowserProcessExited
	case probe.leaseBusy:
		input.ReasonCode = db.LoginReasonBrowserOperationBusy
		input.EvidenceCode = db.LoginEvidenceOperationLeaseBusy
	case probe.probeErr != nil:
		input.ReasonCode = db.LoginReasonCDPUnreachable
	case probe.observation != nil:
		input.AuthState = probe.observation.AuthState
		input.ReasonCode = probe.observation.ReasonCode
		input.EvidenceCode = probe.observation.EvidenceCode
		input.PageOrigin = probe.observation.PageOrigin
		input.Name = probe.observation.Name
		input.AccountIdentifier = probe.observation.AccountIdentifier
	}
	if !observer.fastObservationChanged(account, input, activeLoginSession != nil) {
		return nil, false, nil
	}
	event, err := observer.db.RecordAccountLoginObservation(input)
	if err != nil {
		return nil, false, err
	}
	if activeLoginSession != nil && event.AuthState == db.AccountAuthLoggedIn {
		_ = observer.db.UpdateLoginSession(activeLoginSession.ID, "verified", "")
	}
	updated, err := observer.db.GetAccount(account.ID)
	if err != nil {
		return nil, false, err
	}
	if observer.onStateChange != nil && previousLoginState != updated.LoginState {
		_ = observer.onStateChange(notify.AccountStateChange{
			AccountID: updated.ID, PlatformCode: updated.PlatformCode, AccountName: updated.Name,
			Previous: previousLoginState, Current: updated.LoginState, ReasonCode: event.ReasonCode,
		})
	}
	observer.emitObservationDiagnostic(ctx, updated, event)
	return observationResponse(updated, event), true, nil
}

func (observer *accountObserver) fastObservationChanged(account *db.Account, input db.AccountLoginObservation, hasActiveLoginSession bool) bool {
	if account == nil {
		return false
	}
	latest, err := observer.db.LatestAccountLoginEvent(account.ID)
	if err != nil || latest == nil {
		return true
	}
	originRepair := latest.PageOrigin == "" && input.PageOrigin != ""
	switch input.AuthState {
	case db.AccountAuthLoggedIn:
		avatarChanged := (input.AvatarRef != "" && input.AvatarRef != account.Avatar) ||
			(input.AvatarSHA256 != "" && input.AvatarSHA256 != account.AvatarSHA256)
		return account.LoginState != db.AccountAuthLoggedIn || hasActiveLoginSession ||
			(input.Name != "" && input.Name != account.Name) ||
			(input.AccountIdentifier != "" && input.AccountIdentifier != account.AccountIdentifier) ||
			avatarChanged ||
			latest.AuthState != db.AccountAuthLoggedIn || originRepair
	case db.AccountAuthLoggedOut:
		return account.LoginState != db.AccountAuthLoggedOut || latest.AuthState != db.AccountAuthLoggedOut ||
			latest.ReasonCode != input.ReasonCode || latest.EvidenceCode != input.EvidenceCode || originRepair
	default:
		return latest.AuthState != db.AccountAuthUnknown || latest.BrowserState != input.BrowserState ||
			latest.ReasonCode != input.ReasonCode || latest.EvidenceCode != input.EvidenceCode || originRepair
	}
}

func (observer *accountObserver) ObserveAccount(ctx context.Context, accountID int64) (*AccountObservationResponse, error) {
	if observer == nil || observer.db == nil || observer.status == nil {
		return nil, fmt.Errorf("account observer is not initialized")
	}
	// A manual check must not queue behind a rich batch scan of unrelated
	// profiles. The per-profile SQLite lease in observeAccount still preserves
	// exclusive access to the requested browser.
	account, err := observer.db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	status, statusErr := observer.status(accountID)
	if statusErr == nil && status != nil && status.Running {
		observer.trackedMu.Lock()
		observer.tracked[accountID] = true
		observer.trackedMu.Unlock()
	}
	result, err := observer.observeAccount(ctx, account, status, statusErr)
	if err != nil || result == nil || result.ReasonCode != db.LoginReasonPlatformTabMissing ||
		statusErr != nil || status == nil || !status.Running || status.CDPPort <= 0 {
		return result, err
	}
	// A fresh Facade launch returns as soon as Chromium accepts navigation; the
	// first read can still see about:blank or a restore page. This is an explicit
	// user check, not a background monitor: retry the same existing tab briefly
	// so a normal page-load race is not reported as a dropped login.
	deadline := time.Now().Add(accountManualPlatformTabWait)
	for time.Now().Before(deadline) {
		timer := time.NewTimer(accountManualPlatformTabPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
		result, err = observer.observeAccount(ctx, account, status, statusErr)
		if err != nil || result == nil || result.ReasonCode != db.LoginReasonPlatformTabMissing {
			return result, err
		}
	}
	return result, nil
}

func (observer *accountObserver) observeAccount(ctx context.Context, account *db.Account, status *browser.AccountStatus, statusErr error) (*AccountObservationResponse, error) {
	previousLoginState := account.LoginState
	now := time.Now().UTC().Format(time.RFC3339)
	traceID := string(trace.New())
	browserState := "unknown"
	if status != nil && status.State != "" {
		browserState = string(status.State)
	}

	input := db.AccountLoginObservation{
		AccountID: account.ID, AuthState: db.AccountAuthUnknown, BrowserState: browserState,
		LoginMode: account.LoginMode, AuthMethod: account.AuthMethod,
		ReasonCode: db.LoginReasonBrowserProcessExited, EvidenceCode: db.LoginEvidenceMissing,
		AvatarSource: db.AvatarSourcePlatformFallback, ObservedAt: now, TraceID: traceID,
	}
	if input.LoginMode == "" {
		input.LoginMode = db.LoginModeUnknown
	}
	if input.AuthMethod == "" {
		input.AuthMethod = db.AuthMethodUnknown
	}
	activeLoginSession, _ := observer.db.ActiveLoginSession(account.ID)
	if activeLoginSession != nil {
		input.LoginMode = db.LoginModeManualBrowser
	}

	if statusErr != nil {
		input.ReasonCode = db.LoginReasonCDPUnreachable
	} else if status == nil || !status.Running || status.CDPPort <= 0 {
		input.ReasonCode = db.LoginReasonBrowserProcessExited
		if account.LastCheckAt != "" {
			if evidenceCode, err := observer.db.LatestBrowserExitEvidence(browser.ProfileIDForAccount(account.ID), account.LastCheckAt); err == nil {
				input.EvidenceCode = evidenceCode
				if evidenceCode == db.LoginEvidenceMissing {
					latest, latestErr := observer.db.LatestAccountLoginEvent(account.ID)
					if latestErr == nil && latest != nil && latest.AuthState == db.AccountAuthUnknown && latest.ReasonCode == db.LoginReasonBrowserProcessExited && isBrowserExitEvidence(latest.EvidenceCode) {
						input.EvidenceCode = latest.EvidenceCode
					}
				}
			}
		}
	} else {
		if activeLoginSession == nil && input.LoginMode == db.LoginModeUnknown {
			input.LoginMode = db.LoginModeBrowserProfileSession
		}
		profileID := browser.ProfileIDForAccount(account.ID)
		leaseOwner := "observe:" + traceID
		acquired, leaseErr := observer.acquireManualObservationLease(ctx, profileID, leaseOwner)
		if leaseErr != nil {
			return nil, leaseErr
		}
		if !acquired {
			input.ReasonCode = db.LoginReasonBrowserOperationBusy
			input.EvidenceCode = db.LoginEvidenceOperationLeaseBusy
		} else {
			func() {
				defer func() { _ = observer.db.ReleaseBrowserOperationLease(profileID, leaseOwner) }()
				observation, err := observer.observe(ctx, account.PlatformCode, status.CDPPort)
				if err != nil {
					input.ReasonCode = db.LoginReasonCDPUnreachable
					return
				}
				input.AuthState = observation.AuthState
				input.ReasonCode = observation.ReasonCode
				input.EvidenceCode = observation.EvidenceCode
				input.PageOrigin = observation.PageOrigin
				input.Name = observation.Name
				input.AccountIdentifier = observation.AccountIdentifier
				input.AvatarErrorCode = observation.AvatarErrorCode
				if len(observation.AvatarPNG) > 0 {
					avatarRef, avatarSHA, avatarErr := persistAccountAvatarSnapshot(observer.dataDir, account.ID, observation.AvatarPNG)
					if avatarErr == nil {
						input.AvatarRef = avatarRef
						input.AvatarSHA256 = avatarSHA
						input.AvatarSource = db.AvatarSourceObserved
						input.AvatarErrorCode = ""
					} else {
						input.AvatarErrorCode = db.AvatarErrorPersistFailed
						if observation.AccountIdentifier != "" && observation.AccountIdentifier == account.AccountIdentifier {
							recoveredRef, recoveredSHA, recoverErr := recoverLatestValidAccountAvatarSnapshot(observer.dataDir, account.ID)
							if recoverErr == nil {
								input.AvatarRef = recoveredRef
								input.AvatarSHA256 = recoveredSHA
								input.AvatarSource = db.AvatarSourceObserved
								input.AvatarErrorCode = ""
							}
						}
					}
				}
			}()
		}
	}

	event, err := observer.db.RecordAccountLoginObservation(input)
	if err != nil {
		return nil, err
	}
	if activeLoginSession != nil && event.AuthState == db.AccountAuthLoggedIn {
		_ = observer.db.UpdateLoginSession(activeLoginSession.ID, "verified", "")
	}
	updated, err := observer.db.GetAccount(account.ID)
	if err != nil {
		return nil, err
	}
	if observer.onStateChange != nil && previousLoginState != updated.LoginState {
		_ = observer.onStateChange(notify.AccountStateChange{
			AccountID: updated.ID, PlatformCode: updated.PlatformCode, AccountName: updated.Name,
			Previous: previousLoginState, Current: updated.LoginState, ReasonCode: event.ReasonCode,
		})
	}
	observer.emitObservationDiagnostic(ctx, updated, event)
	return observationResponse(updated, event), nil
}

func (observer *accountObserver) acquireManualObservationLease(ctx context.Context, profileID, owner string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(uiBackgroundProbeWait)
	for {
		now := time.Now()
		acquired, err := observer.db.AcquireBrowserOperationLease(profileID, "observe", owner, now, accountObservationLeaseTTL)
		if err != nil || acquired {
			return acquired, err
		}
		backgroundProbe, err := observer.db.HasBackgroundBrowserObservationLease(profileID, now)
		if err != nil {
			return false, err
		}
		if !backgroundProbe {
			// The probe can disappear between the failed insert and this read.
			// Retry once before treating the profile as owned by real work.
			acquired, err = observer.db.AcquireBrowserOperationLease(profileID, "observe", owner, time.Now(), accountObservationLeaseTTL)
			if err != nil || acquired {
				return acquired, err
			}
		}
		if !backgroundProbe || !now.Before(deadline) {
			return false, nil
		}
		timer := time.NewTimer(uiBackgroundProbePoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
}

func (observer *accountObserver) emitObservationDiagnostic(ctx context.Context, account *db.Account, event *db.AccountLoginEvent) {
	if observer == nil || observer.events == nil || account == nil || event == nil || !event.EvidenceChanged {
		return
	}
	level := slog.LevelInfo
	errorCode := ""
	if event.AuthState != db.AccountAuthLoggedIn {
		level = slog.LevelWarn
		switch event.ReasonCode {
		case db.LoginReasonLoginRequiredPage, db.LoginReasonSessionExpiredPage, db.LoginReasonCheckpointRequired:
			errorCode = "LOGIN_REQUIRED"
		case db.LoginReasonCDPUnreachable:
			errorCode = "CDP_CONNECT"
		case db.LoginReasonBrowserProcessExited:
			errorCode = "BROWSER_NOT_RUNNING"
		case db.LoginReasonBrowserOperationBusy:
			errorCode = "PROFILE_BUSY"
		default:
			errorCode = "LOGIN_CHECK_FAILED"
		}
	}
	_ = observer.events.Emit(ctx, observability.Event{
		Level: level, Component: "account.observer", Name: "account.login.changed",
		TraceID: event.TraceID, AccountID: account.ID, Platform: account.PlatformCode,
		Step: event.ReasonCode, Status: event.AuthState, ErrorCode: errorCode,
	})
}

func isBrowserExitEvidence(code string) bool {
	switch code {
	case db.LoginEvidenceBrowserStoppedByUser,
		db.LoginEvidenceBrowserProcessExited,
		db.LoginEvidenceBrowserCrashed,
		db.LoginEvidenceBrowserHeartbeatFailed,
		db.LoginEvidenceBrowserLeaseLost:
		return true
	default:
		return false
	}
}

func observationResponse(account *db.Account, event *db.AccountLoginEvent) *AccountObservationResponse {
	avatar := ""
	avatarSource := account.AvatarSource
	avatarSHA256 := account.AvatarSHA256
	avatarRefreshedAt := account.AvatarRefreshedAt
	if avatarSource != db.AvatarSourceObserved {
		avatarSource = db.AvatarSourcePlatformFallback
		avatarSHA256 = ""
		avatarRefreshedAt = ""
	}
	if avatarSource == db.AvatarSourceObserved && avatarSHA256 != "" {
		avatar = fmt.Sprintf("/api/accounts/%d/avatar?sha=%s", account.ID, account.AvatarSHA256)
	}
	return &AccountObservationResponse{
		AccountID: account.ID, PlatformCode: account.PlatformCode,
		AuthState: event.AuthState, PersistedLoginState: event.PersistedLoginState,
		BrowserState: event.BrowserState, LoginMode: event.LoginMode, AuthMethod: event.AuthMethod,
		ReasonCode: event.ReasonCode, EvidenceCode: event.EvidenceCode,
		Name: account.Name, AccountIdentifier: account.AccountIdentifier,
		Avatar: avatar, AvatarSource: avatarSource, AvatarSHA256: avatarSHA256,
		AvatarRefreshedAt: avatarRefreshedAt, AvatarErrorCode: event.AvatarErrorCode, ProfileChanged: event.ProfileChanged,
		PageOrigin: event.PageOrigin, ObservedAt: event.ObservedAt, TraceID: event.TraceID,
	}
}

func (observer *accountObserver) setCycleResult(count int, errorCode string) {
	observer.stateMu.Lock()
	observer.state.LastCount = count
	if errorCode != "" {
		observer.state.LastErrorCode = errorCode
	}
	observer.stateMu.Unlock()
}

func persistAccountAvatarSnapshot(dataDir string, accountID int64, payload []byte) (string, string, error) {
	if accountID <= 0 {
		return "", "", fmt.Errorf("account id must be positive")
	}
	if len(payload) == 0 || len(payload) > 2<<20 {
		return "", "", fmt.Errorf("avatar snapshot size is invalid")
	}
	config, err := png.DecodeConfig(bytes.NewReader(payload))
	if err != nil || config.Width < 16 || config.Height < 16 || config.Width > 2048 || config.Height > 2048 {
		return "", "", fmt.Errorf("avatar snapshot is not a supported PNG")
	}
	ratio := float64(config.Width) / float64(config.Height)
	if ratio < 0.65 || ratio > 1.55 {
		return "", "", fmt.Errorf("avatar snapshot aspect ratio is invalid")
	}
	normalized, err := normalizeAvatarPNG(payload)
	if err != nil {
		return "", "", fmt.Errorf("normalize avatar snapshot: %w", err)
	}
	payload = normalized
	if !avatarSnapshotHasVisualContent(payload) {
		return "", "", fmt.Errorf("avatar snapshot is a uniform neutral placeholder")
	}
	digest := sha256.Sum256(payload)
	sha := hex.EncodeToString(digest[:])
	directory, err := safeAvatarDirectory(dataDir, true)
	if err != nil {
		return "", "", err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return "", "", fmt.Errorf("restrict avatar directory: %w", err)
	}
	name := fmt.Sprintf("account-%d-%s.png", accountID, sha[:16])
	destination := filepath.Join(directory, name)
	if info, err := os.Lstat(destination); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("avatar target is not a regular managed file")
		}
		existing, readErr := os.ReadFile(destination)
		if readErr != nil {
			return "", "", readErr
		}
		existingDigest := sha256.Sum256(existing)
		if existingDigest != digest {
			return "", "", fmt.Errorf("avatar target content does not match its digest")
		}
		return filepath.ToSlash(filepath.Join("account-avatars", name)), sha, nil
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	temporary, err := os.CreateTemp(directory, ".avatar-*.tmp")
	if err != nil {
		return "", "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", "", err
	}
	if err := temporary.Close(); err != nil {
		return "", "", err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", "", err
	}
	return filepath.ToSlash(filepath.Join("account-avatars", name)), sha, nil
}

// recoverLatestValidAccountAvatarSnapshot keeps a verified identity usable
// when its creator page temporarily exposes only a neutral loading image.
// It never crosses account IDs and accepts only digest-named managed PNG files.
func recoverLatestValidAccountAvatarSnapshot(dataDir string, accountID int64) (string, string, error) {
	if accountID <= 0 {
		return "", "", fmt.Errorf("account id must be positive")
	}
	directory, err := safeAvatarDirectory(dataDir, false)
	if err != nil {
		return "", "", err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", "", err
	}
	prefix := fmt.Sprintf("account-%d-", accountID)
	var recoveredRef, recoveredSHA string
	var recoveredAt time.Time
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".png") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > 2<<20 {
			continue
		}
		payload, readErr := os.ReadFile(filepath.Join(directory, name))
		if readErr != nil {
			continue
		}
		normalized, normalizeErr := normalizeAvatarPNG(payload)
		if normalizeErr != nil || !avatarSnapshotHasVisualContent(normalized) {
			continue
		}
		digest := sha256.Sum256(payload)
		sha := hex.EncodeToString(digest[:])
		if name != prefix+sha[:16]+".png" || (!recoveredAt.IsZero() && !info.ModTime().After(recoveredAt)) {
			continue
		}
		recoveredAt = info.ModTime()
		recoveredRef = filepath.ToSlash(filepath.Join("account-avatars", name))
		recoveredSHA = sha
	}
	if recoveredRef == "" {
		return "", "", fmt.Errorf("no valid historical avatar snapshot found")
	}
	return recoveredRef, recoveredSHA, nil
}

// normalizeAvatarPNG removes a large one-sided margin, or excessive solid
// canvas around very small artwork. The result remains square so every UI size
// sees the same centered portrait and object-fit never crops a second time.
// ponytail: this is deliberately color-boundary based; it does not attempt
// face/logo detection.
func normalizeAvatarPNG(payload []byte) ([]byte, error) {
	source, err := png.Decode(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	bounds := source.Bounds()
	if bounds.Dx() != bounds.Dy() {
		return payload, nil
	}
	background := color.NRGBAModel.Convert(source.At(bounds.Min.X, bounds.Min.Y)).(color.NRGBA)
	if !sameAvatarBackground(source.At(bounds.Max.X-1, bounds.Min.Y), background) ||
		!sameAvatarBackground(source.At(bounds.Min.X, bounds.Max.Y-1), background) ||
		!sameAvatarBackground(source.At(bounds.Max.X-1, bounds.Max.Y-1), background) {
		return payload, nil
	}

	content, found := avatarContentBounds(source, bounds, background)
	if !found || content == bounds {
		return payload, nil
	}
	left, right := content.Min.X-bounds.Min.X, bounds.Max.X-content.Max.X
	top, bottom := content.Min.Y-bounds.Min.Y, bounds.Max.Y-content.Max.Y
	minimumMargin := bounds.Dx() / 12
	if minimumMargin < 4 {
		minimumMargin = 4
	}
	oneSided := (left >= minimumMargin && right == 0 && top == 0 && bottom == 0) ||
		(right >= minimumMargin && left == 0 && top == 0 && bottom == 0) ||
		(top >= minimumMargin && bottom == 0 && left == 0 && right == 0) ||
		(bottom >= minimumMargin && top == 0 && left == 0 && right == 0)
	ratio := float64(content.Dx()) / float64(content.Dy())
	oneSided = oneSided && content.Dx() >= 16 && content.Dy() >= 16 && ratio >= 0.8 && ratio <= 1.25
	canvasArea := float64(bounds.Dx() * bounds.Dy())
	contentArea := float64(content.Dx() * content.Dy())
	maxContentSide := content.Dx()
	if content.Dy() > maxContentSide {
		maxContentSide = content.Dy()
	}
	sparseSolidCanvas := maxContentSide >= 16 &&
		content.Dx() >= 4 && content.Dy() >= 4 &&
		float64(maxContentSide)/float64(bounds.Dx()) <= 0.75 &&
		contentArea/canvasArea <= 0.12
	if !oneSided && !sparseSolidCanvas {
		return payload, nil
	}

	side := maxContentSide
	if sparseSolidCanvas {
		side += maxContentSide / 4
	}
	result := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(result, result.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)
	offset := image.Pt((side-content.Dx())/2, (side-content.Dy())/2)
	destination := image.Rectangle{Min: offset, Max: offset.Add(content.Size())}
	draw.Draw(result, destination, source, content.Min, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, result); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// avatarSnapshotHasVisualContent rejects the neutral loading placeholder used
// by creator consoles before an account image finishes painting. A uniform
// saturated brand/avatar color is retained; real photos and logos naturally
// have channel variation.
func avatarSnapshotHasVisualContent(payload []byte) bool {
	source, err := png.Decode(bytes.NewReader(payload))
	if err != nil {
		return false
	}
	bounds := source.Bounds()
	if bounds.Empty() {
		return false
	}
	minimum := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	maximum := color.NRGBA{}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			current := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			if current.R < minimum.R {
				minimum.R = current.R
			}
			if current.G < minimum.G {
				minimum.G = current.G
			}
			if current.B < minimum.B {
				minimum.B = current.B
			}
			if current.A < minimum.A {
				minimum.A = current.A
			}
			if current.R > maximum.R {
				maximum.R = current.R
			}
			if current.G > maximum.G {
				maximum.G = current.G
			}
			if current.B > maximum.B {
				maximum.B = current.B
			}
			if current.A > maximum.A {
				maximum.A = current.A
			}
		}
	}
	if avatarChannelDistance(minimum.R, maximum.R) > 8 ||
		avatarChannelDistance(minimum.G, maximum.G) > 8 ||
		avatarChannelDistance(minimum.B, maximum.B) > 8 ||
		avatarChannelDistance(minimum.A, maximum.A) > 8 {
		return true
	}
	base := color.NRGBAModel.Convert(source.At(bounds.Min.X, bounds.Min.Y)).(color.NRGBA)
	channelMin, channelMax := base.R, base.R
	for _, value := range []uint8{base.G, base.B} {
		if value < channelMin {
			channelMin = value
		}
		if value > channelMax {
			channelMax = value
		}
	}
	return avatarChannelDistance(channelMin, channelMax) > 12
}

func avatarContentBounds(source image.Image, bounds image.Rectangle, background color.NRGBA) (image.Rectangle, bool) {
	content := image.Rectangle{Min: bounds.Max, Max: bounds.Min}
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if sameAvatarBackground(source.At(x, y), background) {
				continue
			}
			if !found {
				content = image.Rect(x, y, x+1, y+1)
				found = true
				continue
			}
			if x < content.Min.X {
				content.Min.X = x
			}
			if y < content.Min.Y {
				content.Min.Y = y
			}
			if x >= content.Max.X {
				content.Max.X = x + 1
			}
			if y >= content.Max.Y {
				content.Max.Y = y + 1
			}
		}
	}
	return content, found
}

func sameAvatarBackground(value color.Color, background color.NRGBA) bool {
	current := color.NRGBAModel.Convert(value).(color.NRGBA)
	return avatarChannelDistance(current.R, background.R) <= 4 &&
		avatarChannelDistance(current.G, background.G) <= 4 &&
		avatarChannelDistance(current.B, background.B) <= 4 &&
		avatarChannelDistance(current.A, background.A) <= 4
}

func avatarChannelDistance(left, right uint8) uint8 {
	if left > right {
		return left - right
	}
	return right - left
}

func managedAvatarPath(dataDir, reference string) (string, error) {
	reference = filepath.ToSlash(strings.TrimSpace(reference))
	if reference == "" || filepath.IsAbs(reference) || !strings.HasPrefix(reference, "account-avatars/") || strings.Contains(reference, "../") {
		return "", fmt.Errorf("avatar is not a managed reference")
	}
	root, err := safeAvatarDirectory(dataDir, false)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(filepath.FromSlash(reference))
	resolved := filepath.Clean(filepath.Join(dataDir, clean))
	info, err := os.Lstat(resolved)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("avatar target is not a regular managed file")
	}
	realTarget, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve avatar target: %w", err)
	}
	if realTarget == root || !strings.HasPrefix(realTarget, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("avatar reference escapes managed directory")
	}
	return realTarget, nil
}

func safeAvatarDirectory(dataDir string, create bool) (string, error) {
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}
	directory := filepath.Join(root, "account-avatars")
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) && create {
		if err := os.Mkdir(directory, 0700); err != nil {
			return "", fmt.Errorf("create avatar directory: %w", err)
		}
		info, err = os.Lstat(directory)
	}
	if err != nil {
		return "", fmt.Errorf("inspect avatar directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("avatar directory must be a real directory")
	}
	realDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", fmt.Errorf("resolve avatar directory: %w", err)
	}
	expected := filepath.Join(realRoot, "account-avatars")
	if realDirectory != expected {
		return "", fmt.Errorf("avatar directory escapes data directory")
	}
	return realDirectory, nil
}

func parsePositiveAccountID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid account id")
	}
	return id, nil
}
