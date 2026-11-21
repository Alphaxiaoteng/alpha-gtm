//go:build legacy_chromium

package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/sessionbackup"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	cdptarget "github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const sessionBackupLeaseTTL = 45 * time.Second

// Session transfer is intentionally separate from the normal safe snapshot.
// It is an explicit user action because it reads session cookies into memory,
// immediately encrypts them, and never serializes them into an API response.
func (s *Server) handleAPIAccountSessionBackups(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	if s.sessionBackups == nil {
		http.Error(w, "session backup service is unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, err := s.db.GetAccount(accountID); err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}
	backups, err := s.sessionBackups.ListLocal(accountID)
	if err != nil {
		http.Error(w, "list session backups failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"retentionDays": sessionbackup.RetentionDays,
		"backups":       backups,
	})
}

func (s *Server) handleAPIAccountSessionBackup(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	backup, err := s.backupAccountSession(r.Context(), accountID)
	if err != nil {
		writeSessionBackupError(w, err)
		return
	}
	writeJSON(w, map[string]any{"backup": backup, "retentionDays": sessionbackup.RetentionDays})
}

func (s *Server) handleAPIAccountSessionBackupRestore(w http.ResponseWriter, r *http.Request) {
	if s.sessionBackups == nil || s.facade == nil {
		writeSessionBackupError(w, fmt.Errorf("SESSION_BACKUP_UNAVAILABLE"))
		return
	}
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	createdAt, err := time.ParseInLocation("2006-01-02", r.PathValue("date"), time.UTC)
	if err != nil {
		http.Error(w, "invalid local backup date", http.StatusBadRequest)
		return
	}
	snapshot, err := s.sessionBackups.LoadLocal(accountID, createdAt)
	if err != nil {
		writeSessionBackupError(w, err)
		return
	}
	account, err := s.db.GetAccount(accountID)
	if err != nil || account.PlatformCode != snapshot.Account.PlatformCode {
		writeSessionBackupError(w, fmt.Errorf("SESSION_RESTORE_ACCOUNT_MISMATCH"))
		return
	}
	status, statusErr := s.facade.Status(accountID)
	if statusErr != nil || status == nil || !status.Running {
		if _, err := s.startAccountBrowser(accountID); err != nil {
			writeSessionBackupError(w, fmt.Errorf("SESSION_RESTORE_BROWSER_START_FAILED: %w", err))
			return
		}
	}
	if err := s.restoreAccountSession(r.Context(), accountID, snapshot); err != nil {
		writeSessionBackupError(w, fmt.Errorf("SESSION_RESTORE_FAILED: %w", err))
		return
	}
	writeJSON(w, map[string]any{
		"accountId":            accountID,
		"restored":             true,
		"verificationRequired": true,
		"message":              "已恢复本机加密快照，请在平台页面确认登录状态。",
	})
}

// handleAPIAccountsSessionBackup explicitly snapshots every currently
// verified, running account. It is not a background task and never opens a
// browser; unavailable accounts are reported without exposing session data.
func (s *Server) handleAPIAccountsSessionBackup(w http.ResponseWriter, r *http.Request) {
	if s.sessionBackups == nil {
		http.Error(w, "session backup service is unavailable", http.StatusServiceUnavailable)
		return
	}
	accounts, err := s.db.ListAccounts()
	if err != nil {
		http.Error(w, "list accounts failed", http.StatusInternalServerError)
		return
	}
	type result struct {
		AccountID int64  `json:"accountId"`
		Status    string `json:"status"`
		ErrorCode string `json:"errorCode,omitempty"`
	}
	results := make([]result, 0, len(accounts))
	for _, account := range accounts {
		if !account.Enabled || account.LoginState != db.AccountAuthLoggedIn {
			continue
		}
		if _, err := s.backupAccountSession(r.Context(), account.ID); err != nil {
			results = append(results, result{AccountID: account.ID, Status: "unavailable", ErrorCode: sessionBackupErrorCode(err)})
			continue
		}
		results = append(results, result{AccountID: account.ID, Status: "backed_up"})
	}
	writeJSON(w, map[string]any{"retentionDays": sessionbackup.RetentionDays, "results": results})
}

func (s *Server) handleAPIAccountSessionExport(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	var body struct {
		Passphrase string `json:"passphrase"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		http.Error(w, "invalid session export request", http.StatusBadRequest)
		return
	}
	release, err := s.acquireBrowserOperationLease(accountID, "session_export", sessionBackupLeaseTTL)
	if err != nil {
		writeSessionBackupError(w, err)
		return
	}
	defer release()
	snapshot, err := s.captureAccountSession(r.Context(), accountID)
	if err != nil {
		writeSessionBackupError(w, err)
		return
	}
	archive, err := s.sessionBackups.EncryptPortable(snapshot, body.Passphrase)
	if err != nil {
		writeSessionBackupError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.alpha-nexus.account-session")
	w.Header().Set("Content-Disposition", `attachment; filename="alpha-nexus-account.anxsession"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(archive)
}

func (s *Server) handleAPIAccountSessionImport(w http.ResponseWriter, r *http.Request) {
	snapshot, ok := s.decodePortableSessionRequest(w, r)
	if !ok {
		return
	}

	account, err := s.createSessionImportedAccount(snapshot)
	if err != nil {
		http.Error(w, "create imported account failed", http.StatusInternalServerError)
		return
	}
	if _, err := s.startAccountBrowser(account.ID); err != nil {
		writeSessionBackupError(w, fmt.Errorf("SESSION_IMPORT_BROWSER_START_FAILED: %w", err))
		return
	}
	if err := s.restoreAccountSession(r.Context(), account.ID, snapshot); err != nil {
		// The empty isolated profile remains visible so the user can manually
		// complete login rather than losing their newly imported account record.
		writeSessionBackupError(w, fmt.Errorf("SESSION_IMPORT_RESTORE_FAILED: %w", err))
		return
	}
	writeJSON(w, map[string]any{
		"accountId":            account.ID,
		"platformCode":         account.PlatformCode,
		"sessionImported":      true,
		"verificationRequired": true,
		"message":              "会话已写入新的独立浏览器，请在平台页面确认；平台可能要求再次验证。",
	})
}

// handleAPIAccountSessionImportExisting restores a portable session into an
// already-known account. It avoids creating duplicates when moving a local
// data root while preserving the target account's stable profile identity.
func (s *Server) handleAPIAccountSessionImportExisting(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	snapshot, ok := s.decodePortableSessionRequest(w, r)
	if !ok {
		return
	}
	account, err := s.db.GetAccount(accountID)
	if err != nil {
		writeSessionBackupError(w, fmt.Errorf("ACCOUNT_NOT_FOUND"))
		return
	}
	if account.PlatformCode != snapshot.Account.PlatformCode {
		writeSessionBackupError(w, fmt.Errorf("SESSION_RESTORE_ACCOUNT_MISMATCH"))
		return
	}
	status, statusErr := s.facade.Status(accountID)
	if statusErr == nil && status != nil && status.Running {
		if err := s.stopAccountBrowser(accountID); err != nil {
			writeSessionBackupError(w, fmt.Errorf("SESSION_RESTORE_BROWSER_START_FAILED: %w", err))
			return
		}
	}
	if err := s.applyImportedSessionProfile(accountID, snapshot.Profile); err != nil {
		writeSessionBackupError(w, fmt.Errorf("SESSION_RESTORE_ACCOUNT_MISMATCH: %w", err))
		return
	}
	if _, err := s.startAccountBrowser(accountID); err != nil {
		writeSessionBackupError(w, fmt.Errorf("SESSION_RESTORE_BROWSER_START_FAILED: %w", err))
		return
	}
	if err := s.restoreAccountSession(r.Context(), accountID, snapshot); err != nil {
		writeSessionBackupError(w, fmt.Errorf("SESSION_RESTORE_FAILED: %w", err))
		return
	}
	// Restart through BrowserFacade so the reviewed manager page reloads with
	// the restored cookie jar. The stop path also creates a local encrypted
	// snapshot before Chromium exits.
	if err := s.stopAccountBrowser(accountID); err != nil {
		writeSessionBackupError(w, fmt.Errorf("SESSION_RESTORE_FAILED: %w", err))
		return
	}
	if _, err := s.startAccountBrowser(accountID); err != nil {
		writeSessionBackupError(w, fmt.Errorf("SESSION_RESTORE_BROWSER_START_FAILED: %w", err))
		return
	}
	writeJSON(w, map[string]any{
		"accountId":            accountID,
		"platformCode":         account.PlatformCode,
		"sessionImported":      true,
		"verificationRequired": true,
		"message":              "会话已写入现有独立浏览器，请在平台页面确认；平台可能要求再次验证。",
	})
}

func (s *Server) decodePortableSessionRequest(w http.ResponseWriter, r *http.Request) (sessionbackup.Snapshot, bool) {
	if s.sessionBackups == nil {
		http.Error(w, "session backup service is unavailable", http.StatusServiceUnavailable)
		return sessionbackup.Snapshot{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, sessionbackup.MaxArchiveBytes+64<<10)
	if err := r.ParseMultipartForm(64 << 10); err != nil {
		http.Error(w, "invalid session import", http.StatusBadRequest)
		return sessionbackup.Snapshot{}, false
	}
	file, _, err := r.FormFile("archive")
	if err != nil {
		http.Error(w, "session archive is required", http.StatusBadRequest)
		return sessionbackup.Snapshot{}, false
	}
	defer file.Close()
	archive, err := io.ReadAll(io.LimitReader(file, sessionbackup.MaxArchiveBytes+1))
	if err != nil || len(archive) > sessionbackup.MaxArchiveBytes {
		http.Error(w, "invalid session archive", http.StatusBadRequest)
		return sessionbackup.Snapshot{}, false
	}
	snapshot, err := s.sessionBackups.DecryptPortable(archive, r.FormValue("passphrase"))
	if err != nil {
		writeSessionBackupError(w, err)
		return sessionbackup.Snapshot{}, false
	}
	return snapshot, true
}

func (s *Server) backupAccountSession(ctx context.Context, accountID int64) (sessionbackup.BackupInfo, error) {
	if s.sessionBackups == nil {
		return sessionbackup.BackupInfo{}, fmt.Errorf("SESSION_BACKUP_UNAVAILABLE")
	}
	release, err := s.acquireBrowserOperationLease(accountID, "session_backup", sessionBackupLeaseTTL)
	if err != nil {
		return sessionbackup.BackupInfo{}, err
	}
	defer release()
	return s.backupAccountSessionWithHeldLease(ctx, accountID)
}

// backupAccountSessionWithHeldLease is used by an explicit backup and before a
// user-requested browser stop. The caller must hold this account's browser
// operation lease; failure never reveals or persists plaintext session data.
func (s *Server) backupAccountSessionWithHeldLease(ctx context.Context, accountID int64) (sessionbackup.BackupInfo, error) {
	if s.sessionBackups == nil {
		return sessionbackup.BackupInfo{}, fmt.Errorf("SESSION_BACKUP_UNAVAILABLE")
	}
	snapshot, err := s.captureAccountSession(ctx, accountID)
	if err != nil {
		return sessionbackup.BackupInfo{}, err
	}
	return s.sessionBackups.SaveLocal(accountID, snapshot)
}

func (s *Server) captureAccountSession(ctx context.Context, accountID int64) (sessionbackup.Snapshot, error) {
	if s.facade == nil || s.sessionBackups == nil {
		return sessionbackup.Snapshot{}, fmt.Errorf("SESSION_BACKUP_UNAVAILABLE")
	}
	account, err := s.db.GetAccount(accountID)
	if err != nil {
		return sessionbackup.Snapshot{}, fmt.Errorf("ACCOUNT_NOT_FOUND")
	}
	profile, err := s.db.GetBrowserProfileByAccount(accountID)
	if err != nil {
		return sessionbackup.Snapshot{}, fmt.Errorf("SESSION_PROFILE_MISSING")
	}
	status, err := s.facade.Status(accountID)
	if err != nil || status == nil || !status.Running || status.CDPPort <= 0 {
		return sessionbackup.Snapshot{}, fmt.Errorf("SESSION_BROWSER_NOT_RUNNING")
	}
	urls, err := sessionPlatformURLs(account.PlatformCode)
	if err != nil {
		return sessionbackup.Snapshot{}, fmt.Errorf("SESSION_PLATFORM_UNSUPPORTED")
	}
	cookies, err := captureSessionCookies(ctx, status.CDPPort, urls)
	if err != nil {
		return sessionbackup.Snapshot{}, fmt.Errorf("SESSION_CAPTURE_FAILED")
	}
	if len(cookies) == 0 {
		return sessionbackup.Snapshot{}, fmt.Errorf("SESSION_CAPTURE_EMPTY")
	}
	return sessionbackup.Snapshot{
		Version:   sessionbackup.FormatVersion,
		CreatedAt: time.Now().UTC(),
		Account: sessionbackup.Account{
			PlatformCode: account.PlatformCode,
			Name:         account.Name,
			Remark:       account.Remark,
			Group:        account.AccountGroup,
		},
		Profile: sessionbackup.Profile{
			BrowserType:       browser.NormalizeEngine(profile.BrowserType),
			FingerprintSeed:   profile.FingerprintSeed,
			FingerprintConfig: profile.FingerprintConfig,
			Lang:              profile.Lang,
			AcceptLang:        profile.AcceptLang,
			Timezone:          profile.Timezone,
		},
		Cookies: cookies,
	}, nil
}

func (s *Server) restoreAccountSession(ctx context.Context, accountID int64, snapshot sessionbackup.Snapshot) error {
	if s.facade == nil {
		return fmt.Errorf("SESSION_BACKUP_UNAVAILABLE")
	}
	release, err := s.acquireBrowserOperationLease(accountID, "session_import", sessionBackupLeaseTTL)
	if err != nil {
		return err
	}
	defer release()
	status, err := s.facade.Status(accountID)
	if err != nil || status == nil || !status.Running || status.CDPPort <= 0 {
		return fmt.Errorf("SESSION_BROWSER_NOT_RUNNING")
	}
	return restoreSessionCookies(ctx, status.CDPPort, snapshot.Cookies)
}

func (s *Server) createSessionImportedAccount(snapshot sessionbackup.Snapshot) (*db.Account, error) {
	if _, err := platforms.Find(snapshot.Account.PlatformCode); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(snapshot.Account.Name)
	if name == "" {
		name = snapshot.Account.PlatformCode + "-imported"
	}
	account, err := s.db.CreateAccount(snapshot.Account.PlatformCode, name, strings.TrimSpace(snapshot.Account.Remark))
	if err != nil {
		return nil, err
	}
	if group := strings.TrimSpace(snapshot.Account.Group); group != "" {
		if err := s.db.UpdateAccountGroup(account.ID, group); err != nil {
			return nil, err
		}
		account.AccountGroup = group
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		return nil, err
	}
	profileDir, err := createFreshProfileDir(dataDir, account.ID)
	if err != nil {
		return nil, err
	}
	engine := browser.NormalizeEngine(snapshot.Profile.BrowserType)
	if engine == "" {
		engine = browser.NormalizeEngine(s.cfg.BrowserEngine)
	}
	profile, err := s.db.CreateBrowserProfileEngine(account.ID, profileDir, engine, snapshot.Profile.FingerprintSeed, snapshot.Profile.Lang, snapshot.Profile.AcceptLang, snapshot.Profile.Timezone)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(snapshot.Profile.FingerprintConfig) != "" {
		_, err = s.db.Exec(`UPDATE browser_profiles SET fingerprint_config = ?, updated_at = ? WHERE id = ?`, snapshot.Profile.FingerprintConfig, time.Now().UTC().Format(time.RFC3339), profile.ID)
		if err != nil {
			return nil, err
		}
	}
	return account, nil
}

func (s *Server) applyImportedSessionProfile(accountID int64, imported sessionbackup.Profile) error {
	profile, err := s.db.GetBrowserProfileByAccount(accountID)
	if err != nil {
		return err
	}
	engine := browser.NormalizeEngine(imported.BrowserType)
	if engine == "" {
		engine = browser.NormalizeEngine(profile.BrowserType)
	}
	seed := imported.FingerprintSeed
	if seed <= 0 {
		seed = profile.FingerprintSeed
	}
	lang := strings.TrimSpace(imported.Lang)
	if lang == "" {
		lang = profile.Lang
	}
	acceptLang := strings.TrimSpace(imported.AcceptLang)
	if acceptLang == "" {
		acceptLang = profile.AcceptLang
	}
	timezone := strings.TrimSpace(imported.Timezone)
	if timezone == "" {
		timezone = profile.Timezone
	}
	fingerprintConfig := strings.TrimSpace(imported.FingerprintConfig)
	if fingerprintConfig == "" {
		fingerprintConfig = profile.FingerprintConfig
	}
	_, err = s.db.Exec(`UPDATE browser_profiles
		SET browser_type=?, fingerprint_seed=?, fingerprint_config=?, lang=?, accept_lang=?, timezone=?, updated_at=?
		WHERE id=?`,
		engine, seed, fingerprintConfig, lang, acceptLang, timezone, time.Now().UTC().Format(time.RFC3339), profile.ID)
	return err
}

// sessionPlatformURLs constrains capture to the platform's declared login and
// creator URLs. It never looks at every browser cookie.
func sessionPlatformURLs(code string) ([]string, error) {
	platform, err := platforms.Find(code)
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, 2)
	for _, value := range []string{platform.LoginURL, platform.ManagerURL} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		duplicate := false
		for _, existing := range urls {
			duplicate = duplicate || existing == value
		}
		if !duplicate {
			urls = append(urls, value)
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("platform has no reviewed session URL")
	}
	return urls, nil
}

func captureSessionCookies(ctx context.Context, cdpPort int, urls []string) ([]sessionbackup.Cookie, error) {
	var captured []*network.Cookie
	err := withExistingCDPPage(ctx, cdpPort, func(tabCtx context.Context) error {
		return chromedp.Run(tabCtx, chromedp.ActionFunc(func(actionCtx context.Context) error {
			var err error
			captured, err = network.GetCookies().WithURLs(urls).Do(actionCtx)
			return err
		}))
	})
	if err != nil {
		return nil, err
	}
	cookies := make([]sessionbackup.Cookie, 0, len(captured))
	for _, item := range captured {
		if item == nil || item.Name == "" || item.Value == "" || item.Domain == "" {
			continue
		}
		cookie := sessionbackup.Cookie{
			Name: item.Name, Value: item.Value, Domain: item.Domain, Path: item.Path, Expires: item.Expires,
			HTTPOnly: item.HTTPOnly, Secure: item.Secure, SameSite: string(item.SameSite), Priority: string(item.Priority),
			SourceScheme: string(item.SourceScheme), SourcePort: item.SourcePort,
		}
		if item.PartitionKey != nil {
			cookie.PartitionKeyTopURL = item.PartitionKey.TopLevelSite
		}
		cookies = append(cookies, cookie)
	}
	return cookies, nil
}

func restoreSessionCookies(ctx context.Context, cdpPort int, cookies []sessionbackup.Cookie) error {
	params := make([]*network.CookieParam, 0, len(cookies))
	for _, item := range cookies {
		if item.Name == "" || item.Value == "" || item.Domain == "" {
			return fmt.Errorf("invalid session cookie")
		}
		param := &network.CookieParam{
			Name: item.Name, Value: item.Value, Domain: item.Domain, Path: item.Path, Secure: item.Secure, HTTPOnly: item.HTTPOnly,
			SameSite: network.CookieSameSite(item.SameSite), Priority: network.CookiePriority(item.Priority),
			SourceScheme: network.CookieSourceScheme(item.SourceScheme), SourcePort: item.SourcePort,
		}
		if item.Expires > 0 {
			expires := cdp.TimeSinceEpoch(time.Unix(0, int64(item.Expires*float64(time.Second))))
			param.Expires = &expires
		}
		if item.PartitionKeyTopURL != "" {
			param.PartitionKey = &network.CookiePartitionKey{TopLevelSite: item.PartitionKeyTopURL}
		}
		params = append(params, param)
	}
	return withExistingCDPPage(ctx, cdpPort, func(tabCtx context.Context) error {
		return chromedp.Run(tabCtx, chromedp.ActionFunc(func(actionCtx context.Context) error {
			return network.SetCookies(params).Do(actionCtx)
		}))
	})
}

// withExistingCDPPage attaches to an already-open page and deliberately
// detaches without closing it. Import/export must not navigate, focus, or take
// over a user's browser tab.
func withExistingCDPPage(ctx context.Context, cdpPort int, action func(context.Context) error) error {
	if cdpPort <= 0 {
		return fmt.Errorf("CDP port is required")
	}
	ownerCtx, ownerCancel := context.WithCancel(context.Background())
	defer ownerCancel()
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ownerCtx, fmt.Sprintf("http://127.0.0.1:%d", cdpPort))
	defer allocCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()
	// chromedp owns the remote allocator lifetime. Link the caller only while
	// attaching, then link it again to the bounded CDP action below. This keeps
	// a canceled HTTP request from leaking an attached browser target.
	stopCallerAttach := context.AfterFunc(ctx, browserCancel)
	attachTimeout := time.AfterFunc(10*time.Second, browserCancel)
	targets, err := chromedp.Targets(browserCtx)
	attachTimeout.Stop()
	stopCallerAttach()
	if err != nil {
		return fmt.Errorf("list browser pages: %w", err)
	}
	var targetID cdptarget.ID
	for _, target := range targets {
		if target != nil && target.Type == "page" {
			targetID = target.TargetID
			break
		}
	}
	if targetID == "" {
		return fmt.Errorf("no browser page available")
	}
	tabCtx, tabCancel := chromedp.NewContext(browserCtx, chromedp.WithTargetID(targetID))
	actionCtx, actionCancel := context.WithTimeout(tabCtx, 12*time.Second)
	stopCallerAction := context.AfterFunc(ctx, actionCancel)
	defer func() {
		stopCallerAction()
		actionCancel()
		if chromedpContext := chromedp.FromContext(tabCtx); chromedpContext != nil && chromedpContext.Target != nil {
			chromedpContext.Target.TargetID = ""
		}
		tabCancel()
	}()
	return action(actionCtx)
}

func writeSessionBackupError(w http.ResponseWriter, err error) {
	code := sessionBackupErrorCode(err)
	status := http.StatusBadRequest
	if code == "SESSION_BROWSER_NOT_RUNNING" || code == "SESSION_CAPTURE_EMPTY" {
		status = http.StatusConflict
	}
	if code == "SESSION_BACKUP_UNAVAILABLE" {
		status = http.StatusServiceUnavailable
	}
	writeJSONStatus(w, status, map[string]string{"errorCode": code, "message": "账号会话备份或迁移未完成"})
}

func sessionBackupErrorCode(err error) string {
	switch {
	case errors.Is(err, sessionbackup.ErrInvalidPassphrase):
		return "SESSION_TRANSFER_PASSPHRASE_INVALID"
	case errors.Is(err, sessionbackup.ErrArchiveTooLarge):
		return "SESSION_TRANSFER_ARCHIVE_TOO_LARGE"
	case errors.Is(err, sessionbackup.ErrInvalidArchive), errors.Is(err, sessionbackup.ErrInvalidSnapshot):
		return "SESSION_TRANSFER_ARCHIVE_INVALID"
	}
	for _, code := range []string{
		"ACCOUNT_NOT_FOUND", "SESSION_PROFILE_MISSING", "SESSION_BROWSER_NOT_RUNNING", "SESSION_PLATFORM_UNSUPPORTED",
		"SESSION_CAPTURE_FAILED", "SESSION_CAPTURE_EMPTY", "SESSION_BACKUP_UNAVAILABLE", "SESSION_IMPORT_BROWSER_START_FAILED",
		"SESSION_IMPORT_RESTORE_FAILED",
		"SESSION_RESTORE_ACCOUNT_MISMATCH", "SESSION_RESTORE_BROWSER_START_FAILED", "SESSION_RESTORE_FAILED",
	} {
		if strings.Contains(err.Error(), code) {
			return code
		}
	}
	return "SESSION_BACKUP_FAILED"
}
