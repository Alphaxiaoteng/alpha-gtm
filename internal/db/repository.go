package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	ErrPublishJobNotFound = errors.New("publish job not found")
	ErrPublishJobConflict = errors.New("publish job action conflicts with current state")
)

type Platform struct {
	ID                int64
	Code              string
	Name              string
	Icon              string
	LoginURL          string
	ManagerURL        string
	Enabled           bool
	SupportsArticle   bool
	SupportsGraphText bool
	SupportsVideo     bool
	Sort              int
}

type Account struct {
	ID                int64
	IdentityID        int64
	PlatformCode      string
	Kind              string
	Name              string
	Avatar            string
	AvatarSource      string
	AvatarSHA256      string
	AvatarRefreshedAt string
	LoginMode         string
	AuthMethod        string
	Host              string
	AccountIdentifier string
	ManagerAddr       string
	LoginState        string
	LoginTime         string
	LastCheckAt       string
	LastFocusedAt     string
	Sort              int
	Remark            string
	// AccountGroup is brand/region/product label (PRD 账号分组). Column: account_group.
	AccountGroup string
	// Enabled 控制账号浏览器与发布目标是否可执行。
	Enabled bool
}

type BrowserProfile struct {
	ID                int64
	AccountID         int64
	ProfileDir        string
	BrowserType       string
	FingerprintSeed   int
	FingerprintConfig string
	Lang              string
	AcceptLang        string
	Timezone          string
	LastCDPPort       int
	LastCDPURL        string
	ProxyServer       string
	ProxyID           sql.NullInt64
}

// ProxyEndpoint is a reusable credential-free network endpoint. It never
// stores usernames, passwords, tokens, or health-check response data.
type ProxyEndpoint struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Server    string `json:"server"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func (db *DB) ListPlatforms() ([]Platform, error) {
	rows, err := db.Query("SELECT id, code, name, icon, login_url, manager_url, enabled, supports_article, supports_graph_text, supports_video, sort FROM platforms ORDER BY sort")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var platforms []Platform
	for rows.Next() {
		var p Platform
		var enabled, article, graphText, video int
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Icon, &p.LoginURL, &p.ManagerURL, &enabled, &article, &graphText, &video, &p.Sort); err != nil {
			return nil, err
		}
		p.Enabled = enabled == 1
		p.SupportsArticle = article == 1
		p.SupportsGraphText = graphText == 1
		p.SupportsVideo = video == 1
		platforms = append(platforms, p)
	}
	return platforms, rows.Err()
}

func (db *DB) GetPlatform(code string) (*Platform, error) {
	var p Platform
	var enabled, article, graphText, video int
	err := db.QueryRow("SELECT id, code, name, icon, login_url, manager_url, enabled, supports_article, supports_graph_text, supports_video, sort FROM platforms WHERE code = ?", code).
		Scan(&p.ID, &p.Code, &p.Name, &p.Icon, &p.LoginURL, &p.ManagerURL, &enabled, &article, &graphText, &video, &p.Sort)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("platform %q not found", code)
	}
	if err != nil {
		return nil, err
	}
	p.Enabled = enabled == 1
	p.SupportsArticle = article == 1
	p.SupportsGraphText = graphText == 1
	p.SupportsVideo = video == 1
	return &p, nil
}

func (db *DB) CreateAccount(platformCode, name, remark string) (*Account, error) {
	var identityID int64
	err := db.QueryRow(`SELECT identity.id
		FROM ego_identities identity
		WHERE identity.enabled=1
			AND NOT EXISTS (
				SELECT 1 FROM accounts
				WHERE accounts.identity_id=identity.id AND accounts.platform_code=?
			)
		ORDER BY identity.sort,identity.id
		LIMIT 1`, strings.TrimSpace(platformCode)).Scan(&identityID)
	if err == sql.ErrNoRows {
		identity, createErr := db.CreateEgoIdentity("")
		if createErr != nil {
			return nil, fmt.Errorf("create ego identity: %w", createErr)
		}
		identityID = identity.ID
	} else if err != nil {
		return nil, fmt.Errorf("default ego identity: %w", err)
	}
	return db.CreateAccountForIdentity(identityID, platformCode, name, remark)
}

func (db *DB) CreateAccountForIdentity(identityID int64, platformCode, name, remark string) (*Account, error) {
	if identityID <= 0 {
		return nil, fmt.Errorf("identity_id must be positive")
	}
	platformCode = strings.TrimSpace(platformCode)
	name = strings.TrimSpace(name)
	if platformCode == "" {
		return nil, fmt.Errorf("platform_code is required")
	}
	if name == "" {
		if platform, err := db.GetPlatform(platformCode); err == nil {
			name = platform.Name
		} else {
			name = platformCode
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO accounts (identity_id, platform_code, kind, name, login_state, sort, remark, source, created_at, updated_at) VALUES (?, ?, 'browser_login', ?, 'unknown', 0, ?, 'manual', ?, ?)`,
		identityID, platformCode, name, remark, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO account_assets (account_id, platform_code, asset_type, name, external_id, avatar, enabled, last_seen_at, created_at, updated_at) VALUES (?, ?, 'browser_account', ?, '', '', 1, '', ?, ?)`, id, platformCode, name, now, now); err != nil {
		// Do not leave an account that cannot be selected as a publish target.
		// 回滚失败必须连同原错误一起返回，否则库里会留下一个半成品账号，
		// 而调用方只看到「创建资源失败」，无从知道还有个孤儿行要清。
		if _, rollbackErr := db.Exec(`DELETE FROM accounts WHERE id=?`, id); rollbackErr != nil {
			return nil, fmt.Errorf("create default account asset: %w (rollback of account %d also failed: %v)", err, id, rollbackErr)
		}
		return nil, fmt.Errorf("create default account asset: %w", err)
	}
	return &Account{
		ID:           id,
		IdentityID:   identityID,
		PlatformCode: platformCode,
		Kind:         "browser_login",
		Name:         name,
		AvatarSource: AvatarSourcePlatformFallback,
		LoginState:   "unknown",
		LoginMode:    LoginModeUnknown,
		AuthMethod:   AuthMethodUnknown,
		Remark:       remark,
		Enabled:      true,
	}, nil
}

func (db *DB) CreateBrowserProfile(accountID int64, profileDir string, fingerprintSeed int, lang, acceptLang, timezone string) (*BrowserProfile, error) {
	return db.CreateBrowserProfileEngine(accountID, profileDir, "fingerprint-chromium", fingerprintSeed, lang, acceptLang, timezone)
}

// CreateBrowserProfileEngine creates a profile with explicit engine (fingerprint-chromium|browseros|chromium).
func (db *DB) CreateBrowserProfileEngine(accountID int64, profileDir, browserType string, fingerprintSeed int, lang, acceptLang, timezone string) (*BrowserProfile, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if browserType == "" {
		browserType = "fingerprint-chromium"
	}
	res, err := db.Exec(`INSERT INTO browser_profiles (account_id, profile_dir, browser_type, fingerprint_seed, lang, accept_lang, timezone, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		accountID, profileDir, browserType, fingerprintSeed, lang, acceptLang, timezone, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &BrowserProfile{
		ID:              id,
		AccountID:       accountID,
		ProfileDir:      profileDir,
		BrowserType:     browserType,
		FingerprintSeed: fingerprintSeed,
		Lang:            lang,
		AcceptLang:      acceptLang,
		Timezone:        timezone,
	}, nil
}

const accountSelectCols = `id, COALESCE(identity_id,1), platform_code, kind, name, COALESCE(avatar,''), COALESCE(avatar_source,'platform_fallback'), COALESCE(avatar_sha256,''), COALESCE(avatar_refreshed_at,''), COALESCE(login_mode,'unknown'), COALESCE(auth_method,'unknown'), COALESCE(host,''), COALESCE(account_identifier,''), COALESCE(manager_addr,''), login_state, COALESCE(login_time,''), COALESCE(last_check_at,''), COALESCE(last_focused_at,''), sort, COALESCE(remark,''), COALESCE(account_group,''), enabled`

func scanAccount(scanner interface {
	Scan(dest ...any) error
}) (*Account, error) {
	var a Account
	var enabled int
	err := scanner.Scan(&a.ID, &a.IdentityID, &a.PlatformCode, &a.Kind, &a.Name, &a.Avatar, &a.AvatarSource, &a.AvatarSHA256, &a.AvatarRefreshedAt, &a.LoginMode, &a.AuthMethod, &a.Host, &a.AccountIdentifier, &a.ManagerAddr, &a.LoginState, &a.LoginTime, &a.LastCheckAt, &a.LastFocusedAt, &a.Sort, &a.Remark, &a.AccountGroup, &enabled)
	if err != nil {
		return nil, err
	}
	a.Enabled = enabled == 1
	return &a, nil
}

func (db *DB) ListAccounts() ([]Account, error) {
	rows, err := db.Query(`SELECT ` + accountSelectCols + ` FROM accounts ORDER BY platform_code, sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *a)
	}
	return accounts, rows.Err()
}

func (db *DB) ListAccountsByPlatform(platformCode string) ([]Account, error) {
	rows, err := db.Query(`SELECT `+accountSelectCols+` FROM accounts WHERE platform_code = ? ORDER BY sort, id`, platformCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *a)
	}
	return accounts, rows.Err()
}

func (db *DB) ListAccountsByIdentity(identityID int64) ([]Account, error) {
	rows, err := db.Query(`SELECT `+accountSelectCols+` FROM accounts WHERE identity_id = ? ORDER BY sort, id`, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []Account
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *account)
	}
	return accounts, rows.Err()
}

func (db *DB) ListAccountsByGroup(group string) ([]Account, error) {
	rows, err := db.Query(`SELECT `+accountSelectCols+` FROM accounts WHERE account_group = ? ORDER BY platform_code, sort, id`, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, *a)
	}
	return accounts, rows.Err()
}

func (db *DB) GetAccount(id int64) (*Account, error) {
	a, err := scanAccount(db.QueryRow(`SELECT `+accountSelectCols+` FROM accounts WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("account %d not found", id)
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (db *DB) UpdateAccountGroup(id int64, group string) error {
	if id <= 0 {
		return fmt.Errorf("account id must be positive")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE accounts SET account_group = ?, updated_at = ? WHERE id = ?`, group, now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("account %d not found", id)
	}
	return nil
}

func (db *DB) UpdateAccountRemark(id int64, remark string) error {
	if id <= 0 {
		return fmt.Errorf("account id must be positive")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE accounts SET remark = ?, updated_at = ? WHERE id = ?`, remark, now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("account %d not found", id)
	}
	return nil
}

// SetAccountEnabled 同步启停账号及其当前发布目标。
// 禁用账号不能启动受管浏览器，也不能创建可发布目标。
func (db *DB) SetAccountEnabled(id int64, enabled bool) error {
	if id <= 0 {
		return fmt.Errorf("account id must be positive")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.Exec(`UPDATE accounts SET enabled = ?, updated_at = ? WHERE id = ?`, boolInt(enabled), now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("account %d not found", id)
	}
	if _, err := tx.Exec(`UPDATE account_assets SET enabled = ?, updated_at = ? WHERE account_id = ?`, boolInt(enabled), now, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) UpdateAccountLoginState(id int64, state string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE accounts SET login_state = ?, login_time = CASE WHEN ? = 'logged_in' THEN ? ELSE login_time END, last_check_at = ?, updated_at = ? WHERE id = ?`,
		state, state, now, now, now, id)
	return err
}

// UpdateAccountManagerAddr stores only a caller-reviewed HTTPS manager route.
// Browser cookies, credentials, fragments and platform tokens never belong in
// this field; platform-specific allow-listing remains at the service boundary.
func (db *DB) UpdateAccountManagerAddr(id int64, managerAddr string) error {
	if id <= 0 || len(managerAddr) > 2048 {
		return fmt.Errorf("invalid account manager address")
	}
	parsed, err := url.Parse(managerAddr)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("invalid account manager address")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE accounts SET manager_addr=?, updated_at=? WHERE id=?`, managerAddr, now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("account %d not found", id)
	}
	return nil
}

// LoginSession is an auditable state machine for a local, human-completed
// browser login. It deliberately stores no cookie, password, QR payload or OTP.
type LoginSession struct {
	ID          int64  `json:"id"`
	AccountID   int64  `json:"account_id"`
	Status      string `json:"status"`
	ErrorCode   string `json:"error_code"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	ExpiresAt   string `json:"expires_at"`
}

const loginSessionTTL = time.Hour

func (db *DB) CreateLoginSession(accountID int64) (*LoginSession, error) {
	if accountID <= 0 {
		return nil, fmt.Errorf("account id must be positive")
	}
	if _, err := db.GetAccount(accountID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE login_sessions SET status='cancelled', completed_at=?, updated_at=? WHERE account_id=? AND status IN ('opening_browser','awaiting_login')`, nowText, nowText, accountID); err != nil {
		return nil, err
	}
	expiresAt := now.Add(loginSessionTTL).Format(time.RFC3339)
	res, err := tx.Exec(`INSERT INTO login_sessions (account_id, status, started_at, expires_at, created_at, updated_at) VALUES (?, 'opening_browser', ?, ?, ?, ?)`, accountID, nowText, expiresAt, nowText, nowText)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &LoginSession{ID: id, AccountID: accountID, Status: "opening_browser", StartedAt: nowText, ExpiresAt: expiresAt}, nil
}

func (db *DB) ListLoginSessions(limit int) ([]LoginSession, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.Query(`SELECT id, account_id, status, COALESCE(error_code,''), started_at, COALESCE(completed_at,''), COALESCE(expires_at,'') FROM login_sessions ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []LoginSession
	for rows.Next() {
		var item LoginSession
		if err := rows.Scan(&item.ID, &item.AccountID, &item.Status, &item.ErrorCode, &item.StartedAt, &item.CompletedAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, item)
	}
	return sessions, rows.Err()
}

func (db *DB) UpdateLoginSession(id int64, status, errorCode string) error {
	if id <= 0 {
		return fmt.Errorf("login session id must be positive")
	}
	if status != "awaiting_login" && status != "verified" && status != "failed" && status != "cancelled" {
		return fmt.Errorf("invalid login session status")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	completed := ""
	if status == "verified" || status == "failed" || status == "cancelled" {
		completed = now
	}
	res, err := db.Exec(`UPDATE login_sessions SET status=?, error_code=?, completed_at=CASE WHEN ?<>'' THEN ? ELSE completed_at END, updated_at=? WHERE id=? AND status IN ('opening_browser','awaiting_login')`, status, errorCode, completed, completed, now, id)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("login session %d not active", id)
	}
	return nil
}

func (db *DB) ActiveLoginSession(accountID int64) (*LoginSession, error) {
	var item LoginSession
	err := db.QueryRow(`SELECT id, account_id, status, COALESCE(error_code,''), started_at, COALESCE(completed_at,''), COALESCE(expires_at,'') FROM login_sessions WHERE account_id=? AND status IN ('opening_browser','awaiting_login') ORDER BY id DESC LIMIT 1`, accountID).Scan(&item.ID, &item.AccountID, &item.Status, &item.ErrorCode, &item.StartedAt, &item.CompletedAt, &item.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (db *DB) UpdateAccountProfile(id int64, name, avatar, accountID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE accounts SET name = COALESCE(NULLIF(?, ''), name), avatar = COALESCE(NULLIF(?, ''), avatar), account_identifier = COALESCE(NULLIF(?, ''), account_identifier), login_state = 'logged_in', login_time = ?, last_check_at = ?, updated_at = ? WHERE id = ?`,
		name, avatar, accountID, now, now, now, id)
	return err
}

// UpdateImportedAccountMetadata copies non-secret legacy labels without
// treating another browser engine's historical state as EgoLite login proof.
func (db *DB) UpdateImportedAccountMetadata(id int64, name, accountID, remark string) error {
	if id <= 0 {
		return fmt.Errorf("invalid account id")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE accounts SET
		name=COALESCE(NULLIF(?,''),name),
		account_identifier=COALESCE(NULLIF(?,''),account_identifier),
		remark=?,source='legacy_metadata',
		login_state=CASE WHEN login_state='logged_in' THEN login_state ELSE 'unknown' END,
		updated_at=? WHERE id=?`,
		strings.TrimSpace(name), strings.TrimSpace(accountID), strings.TrimSpace(remark), now, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("account not found")
	}
	if _, err := tx.Exec(`UPDATE account_assets SET
		name=COALESCE(NULLIF(?,''),name),
		external_id=COALESCE(NULLIF(?,''),external_id),
		updated_at=? WHERE account_id=? AND asset_type='browser_account'`,
		strings.TrimSpace(name), strings.TrimSpace(accountID), now, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) DeleteAccount(id int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM login_sessions WHERE account_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM account_assets WHERE account_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM browser_profiles WHERE account_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM accounts WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// --- AccountAsset ---

type AccountAsset struct {
	ID                 int64  `json:"id"`
	AccountID          int64  `json:"account_id"`
	PlatformCode       string `json:"platform_code"`
	AssetType          string `json:"asset_type"`
	Name               string `json:"name"`
	ExternalID         string `json:"external_id"`
	Avatar             string `json:"avatar"`
	ConnectedAssetID   int64  `json:"connected_asset_id"`
	ConnectedAssetName string `json:"connected_asset_name"`
	Enabled            bool   `json:"enabled"`
	LastSeenAt         string `json:"last_seen_at"`
	RawJSON            string `json:"-"`
}

func (db *DB) CreateAccountAsset(accountID int64, platformCode, assetType, name, externalID, avatar string, connectedAssetID int64, connectedAssetName string) (*AccountAsset, error) {
	account, err := db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO account_assets (account_id, platform_code, asset_type, name, external_id, avatar, connected_asset_id, connected_asset_name, enabled, last_seen_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		accountID, platformCode, assetType, name, externalID, avatar, connectedAssetID, connectedAssetName, boolInt(account.Enabled), now, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &AccountAsset{ID: id, AccountID: accountID, PlatformCode: platformCode, AssetType: assetType, Name: name, ExternalID: externalID, Avatar: avatar, ConnectedAssetID: connectedAssetID, ConnectedAssetName: connectedAssetName, Enabled: account.Enabled, LastSeenAt: now}, nil
}

// ConfirmAccountAssetIdentity upgrades a discovered name-only target with the
// public identifier explicitly supplied by the user. It does not change the
// target's platform, type, owner, enabled state, or any browser session data.
func (db *DB) ConfirmAccountAssetIdentity(id, accountID int64, name, externalID string) (*AccountAsset, error) {
	if id <= 0 || accountID <= 0 {
		return nil, fmt.Errorf("account asset id and account id must be positive")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE account_assets
		SET name = ?, external_id = ?, last_seen_at = ?, updated_at = ?
		WHERE id = ? AND account_id = ?`, name, externalID, now, now, id, accountID)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, fmt.Errorf("account asset %d not found for account %d", id, accountID)
	}
	return db.GetAccountAsset(id)
}

func (db *DB) UpdateAccountAssetAvatar(id int64, avatar string) error {
	if id <= 0 || !strings.HasPrefix(avatar, "account-asset-avatars/asset-") || strings.Contains(avatar, "..") {
		return fmt.Errorf("managed account asset avatar is invalid")
	}
	_, err := db.Exec(`UPDATE account_assets SET avatar = ?, last_seen_at = ?, updated_at = ? WHERE id = ?`,
		avatar, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (db *DB) ListAccountAssetsByAccount(accountID int64) ([]AccountAsset, error) {
	rows, err := db.Query(`SELECT id, account_id, platform_code, asset_type, name, COALESCE(external_id,''), COALESCE(avatar,''), COALESCE(connected_asset_id,0), COALESCE(connected_asset_name,''), enabled, COALESCE(last_seen_at,''), COALESCE(raw_json,'') FROM account_assets WHERE account_id = ? ORDER BY asset_type, name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assets []AccountAsset
	for rows.Next() {
		var a AccountAsset
		var enabled int
		if err := rows.Scan(&a.ID, &a.AccountID, &a.PlatformCode, &a.AssetType, &a.Name, &a.ExternalID, &a.Avatar, &a.ConnectedAssetID, &a.ConnectedAssetName, &enabled, &a.LastSeenAt, &a.RawJSON); err != nil {
			return nil, err
		}
		a.Enabled = enabled == 1
		assets = append(assets, a)
	}
	return assets, rows.Err()
}

func (db *DB) ListAllAccountAssets() ([]AccountAsset, error) {
	rows, err := db.Query(`SELECT id, account_id, platform_code, asset_type, name, COALESCE(external_id,''), COALESCE(avatar,''), COALESCE(connected_asset_id,0), COALESCE(connected_asset_name,''), enabled, COALESCE(last_seen_at,''), COALESCE(raw_json,'') FROM account_assets ORDER BY platform_code, asset_type, name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assets []AccountAsset
	for rows.Next() {
		var asset AccountAsset
		var enabled int
		if err := rows.Scan(&asset.ID, &asset.AccountID, &asset.PlatformCode, &asset.AssetType, &asset.Name, &asset.ExternalID, &asset.Avatar, &asset.ConnectedAssetID, &asset.ConnectedAssetName, &enabled, &asset.LastSeenAt, &asset.RawJSON); err != nil {
			return nil, err
		}
		asset.Enabled = enabled == 1
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (db *DB) ListAccountAssetsByPlatform(platformCode string) ([]AccountAsset, error) {
	rows, err := db.Query(`SELECT id, account_id, platform_code, asset_type, name, COALESCE(external_id,''), COALESCE(avatar,''), COALESCE(connected_asset_id,0), COALESCE(connected_asset_name,''), enabled, COALESCE(last_seen_at,''), COALESCE(raw_json,'') FROM account_assets WHERE platform_code = ? ORDER BY asset_type, name`, platformCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assets []AccountAsset
	for rows.Next() {
		var a AccountAsset
		var enabled int
		if err := rows.Scan(&a.ID, &a.AccountID, &a.PlatformCode, &a.AssetType, &a.Name, &a.ExternalID, &a.Avatar, &a.ConnectedAssetID, &a.ConnectedAssetName, &enabled, &a.LastSeenAt, &a.RawJSON); err != nil {
			return nil, err
		}
		a.Enabled = enabled == 1
		assets = append(assets, a)
	}
	return assets, rows.Err()
}

func (db *DB) GetAccountAsset(id int64) (*AccountAsset, error) {
	var a AccountAsset
	var enabled int
	err := db.QueryRow(`SELECT id, account_id, platform_code, asset_type, name, COALESCE(external_id,''), COALESCE(avatar,''), COALESCE(connected_asset_id,0), COALESCE(connected_asset_name,''), enabled, COALESCE(last_seen_at,''), COALESCE(raw_json,'') FROM account_assets WHERE id = ?`, id).
		Scan(&a.ID, &a.AccountID, &a.PlatformCode, &a.AssetType, &a.Name, &a.ExternalID, &a.Avatar, &a.ConnectedAssetID, &a.ConnectedAssetName, &enabled, &a.LastSeenAt, &a.RawJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("account asset %d not found", id)
	}
	a.Enabled = enabled == 1
	return &a, err
}

// UpdateAccountAssetConnection records one side of a Meta asset relationship.
// The connected asset is re-read inside the same transaction so callers cannot
// attach a target from another owner or with stale identity fields.
func (db *DB) UpdateAccountAssetConnection(sourceID, connectedID int64, platformCode, assetType, name, externalID string) error {
	if sourceID <= 0 || connectedID <= 0 || sourceID == connectedID {
		return fmt.Errorf("account asset connection is invalid")
	}
	platformCode = strings.TrimSpace(platformCode)
	assetType = strings.TrimSpace(assetType)
	name = strings.TrimSpace(name)
	externalID = strings.TrimSpace(externalID)
	if platformCode == "" || assetType == "" || name == "" || externalID == "" {
		return fmt.Errorf("account asset connection identity is incomplete")
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var sourceAccountID int64
	var sourcePlatformCode, sourceAssetType, sourceName, sourceExternalID string
	var sourceEnabled int
	if err := tx.QueryRow(`SELECT account_id, platform_code, asset_type, name, COALESCE(external_id,''), enabled FROM account_assets WHERE id = ?`, sourceID).
		Scan(&sourceAccountID, &sourcePlatformCode, &sourceAssetType, &sourceName, &sourceExternalID, &sourceEnabled); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("source account asset %d not found", sourceID)
		}
		return err
	}
	if sourceEnabled != 1 {
		return fmt.Errorf("source account asset %d is disabled", sourceID)
	}
	wantSourcePlatform, wantSourceType := "instagram", "instagram_account"
	if platformCode == "instagram" && assetType == "instagram_account" {
		wantSourcePlatform, wantSourceType = "facebook", "facebook_page"
	} else if platformCode != "facebook" || assetType != "facebook_page" {
		return fmt.Errorf("connected account asset is not a supported Meta target")
	}
	if sourcePlatformCode != wantSourcePlatform || sourceAssetType != wantSourceType ||
		strings.TrimSpace(sourceName) == "" || strings.TrimSpace(sourceExternalID) == "" {
		return fmt.Errorf("source account asset identity is inconsistent")
	}

	var connectedAccountID int64
	var connectedPlatformCode, connectedAssetType, connectedName, connectedExternalID string
	var connectedEnabled int
	if err := tx.QueryRow(`SELECT account_id, platform_code, asset_type, name, COALESCE(external_id,''), enabled FROM account_assets WHERE id = ?`, connectedID).
		Scan(&connectedAccountID, &connectedPlatformCode, &connectedAssetType, &connectedName, &connectedExternalID, &connectedEnabled); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("connected account asset %d not found", connectedID)
		}
		return err
	}
	if sourceAccountID != connectedAccountID || connectedEnabled != 1 ||
		connectedPlatformCode != platformCode || connectedAssetType != assetType ||
		connectedName != name || connectedExternalID != externalID {
		return fmt.Errorf("connected account asset identity is inconsistent")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := tx.Exec(`UPDATE account_assets SET connected_asset_id = ?, connected_asset_name = ?, updated_at = ? WHERE id = ?`, connectedID, connectedName, now, sourceID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("source account asset %d not found", sourceID)
	}
	return tx.Commit()
}

// --- Platform publish templates ---

// PlatformSetting is an account-independent publish preset. SettingsJSON is
// deliberately limited to non-secret JSON so it is safe to return to the UI.
type PlatformSetting struct {
	ID           int64  `json:"id"`
	PlatformCode string `json:"platform_code"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	SettingsJSON string `json:"settings_json"`
	Enabled      bool   `json:"enabled"`
	IsDefault    bool   `json:"is_default"`
	Source       string `json:"source"`
}

func (db *DB) ListPlatformSettings() ([]PlatformSetting, error) {
	rows, err := db.Query(`SELECT id, platform_code, name, COALESCE(description,''), settings_json, enabled, is_default, source FROM platform_settings ORDER BY platform_code, is_default DESC, name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var settings []PlatformSetting
	for rows.Next() {
		setting, err := scanPlatformSetting(rows)
		if err != nil {
			return nil, err
		}
		settings = append(settings, *setting)
	}
	return settings, rows.Err()
}

func (db *DB) GetPlatformSetting(id int64) (*PlatformSetting, error) {
	setting, err := scanPlatformSetting(db.QueryRow(`SELECT id, platform_code, name, COALESCE(description,''), settings_json, enabled, is_default, source FROM platform_settings WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("platform template %d not found", id)
	}
	return setting, err
}

func scanPlatformSetting(scanner interface{ Scan(dest ...any) error }) (*PlatformSetting, error) {
	var setting PlatformSetting
	var enabled, isDefault int
	err := scanner.Scan(&setting.ID, &setting.PlatformCode, &setting.Name, &setting.Description, &setting.SettingsJSON, &enabled, &isDefault, &setting.Source)
	if err != nil {
		return nil, err
	}
	setting.Enabled = enabled == 1
	setting.IsDefault = isDefault == 1
	return &setting, nil
}

func (db *DB) CreatePlatformSetting(platformCode, name, description, settingsJSON string, enabled, isDefault bool) (*PlatformSetting, error) {
	if err := db.validatePlatformSetting(platformCode, name, settingsJSON); err != nil {
		return nil, err
	}
	if _, err := db.GetPlatform(platformCode); err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if isDefault {
		if _, err := tx.Exec(`UPDATE platform_settings SET is_default=0, updated_at=? WHERE platform_code=?`, time.Now().UTC().Format(time.RFC3339), platformCode); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec(`INSERT INTO platform_settings (platform_code, name, description, settings_json, enabled, is_default, source, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 'manual', ?, ?)`, platformCode, name, description, settingsJSON, boolInt(enabled), boolInt(isDefault), now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PlatformSetting{ID: id, PlatformCode: platformCode, Name: name, Description: description, SettingsJSON: settingsJSON, Enabled: enabled, IsDefault: isDefault, Source: "manual"}, nil
}

func (db *DB) ReplacePlatformSetting(id int64, platformCode, name, description, settingsJSON string, enabled, isDefault bool) error {
	if id <= 0 {
		return fmt.Errorf("platform template id must be positive")
	}
	if err := db.validatePlatformSetting(platformCode, name, settingsJSON); err != nil {
		return err
	}
	if _, err := db.GetPlatform(platformCode); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if isDefault {
		if _, err := tx.Exec(`UPDATE platform_settings SET is_default=0, updated_at=? WHERE platform_code=? AND id<>?`, time.Now().UTC().Format(time.RFC3339), platformCode, id); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec(`UPDATE platform_settings SET platform_code=?, name=?, description=?, settings_json=?, enabled=?, is_default=?, updated_at=? WHERE id=?`, platformCode, name, description, settingsJSON, boolInt(enabled), boolInt(isDefault), now, id)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("platform template %d not found", id)
	}
	return tx.Commit()
}

func (db *DB) DeletePlatformSetting(id int64) error {
	if id <= 0 {
		return fmt.Errorf("platform template id must be positive")
	}
	res, err := db.Exec(`DELETE FROM platform_settings WHERE id=?`, id)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("platform template %d not found", id)
	}
	return nil
}

func (db *DB) validatePlatformSetting(platformCode, name, settingsJSON string) error {
	if strings.TrimSpace(platformCode) == "" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("platform and name are required")
	}
	if len(settingsJSON) > 64<<10 {
		return fmt.Errorf("template settings exceed 64 KiB")
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil || settings == nil {
		return fmt.Errorf("template settings must be a JSON object")
	}
	if hasSensitiveSettingKey(settings) {
		return fmt.Errorf("template settings must not contain credentials or session data")
	}
	return nil
}

func hasSensitiveSettingKey(value any) bool {
	switch typed := value.(type) {
	case map[string]json.RawMessage:
		for key, raw := range typed {
			key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
			for _, blocked := range []string{"cookie", "token", "password", "secret", "authorization", "session", "credential", "private_key", "access_key"} {
				if strings.Contains(key, blocked) {
					return true
				}
			}
			var nested any
			if json.Unmarshal(raw, &nested) == nil && hasSensitiveSettingKey(nested) {
				return true
			}
		}
	case map[string]any:
		for key, nested := range typed {
			if hasSensitiveSettingKey(map[string]json.RawMessage{key: mustMarshalRaw(nested)}) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if hasSensitiveSettingKey(nested) {
				return true
			}
		}
	}
	return false
}

func mustMarshalRaw(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// --- Content ---

type Content struct {
	ID             int64  `json:"id"`
	Type           string `json:"type"`
	Title          string `json:"title"`
	Caption        string `json:"caption"`
	Body           string `json:"body"`
	TagsJSON       string `json:"tags_json"`
	Status         string `json:"status"`
	PublishCount   int    `json:"publish_count"`
	PlannedAt      string `json:"planned_at,omitempty"`
	Revision       int64  `json:"revision"`
	CoverMediaID   int64  `json:"cover_media_id,omitempty"`
	CoverKind      string `json:"cover_kind,omitempty"`
	CoverMimeType  string `json:"cover_mime_type,omitempty"`
	RawPublishData string `json:"-"`
}

// LocalDashboardSummary is a read-only aggregate of Alpha's own business
// tables. It intentionally does not trigger platform collection.
type LocalDashboardSummary struct {
	Contents          int `json:"contents"`
	DraftContents     int `json:"draft_contents"`
	Accounts          int `json:"accounts"`
	LoggedInAccounts  int `json:"logged_in_accounts"`
	PublishRecords    int `json:"publish_records"`
	SuccessfulRecords int `json:"successful_records"`
	PendingJobs       int `json:"pending_jobs"`
	FailedJobs        int `json:"failed_jobs"`
}

func (db *DB) GetLocalDashboardSummary() (*LocalDashboardSummary, error) {
	var summary LocalDashboardSummary
	err := db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM contents),
		(SELECT COUNT(*) FROM contents WHERE status='draft'),
		(SELECT COUNT(*) FROM accounts),
		(SELECT COUNT(*) FROM accounts WHERE login_state='logged_in'),
		(SELECT COUNT(*) FROM publish_records),
		(SELECT COUNT(*) FROM publish_records WHERE status='published'),
		(SELECT COUNT(*) FROM publish_jobs WHERE status IN ('pending','queued','running','manual_takeover','awaiting_review')),
		(SELECT COUNT(*) FROM publish_jobs WHERE status IN ('failed','permanent_failed','retryable_failed'))`).Scan(
		&summary.Contents, &summary.DraftContents, &summary.Accounts, &summary.LoggedInAccounts,
		&summary.PublishRecords, &summary.SuccessfulRecords, &summary.PendingJobs, &summary.FailedJobs,
	)
	return &summary, err
}

func (db *DB) CreateContent(contentType, title, caption, body, tagsJSON, status string) (*Content, error) {
	return db.CreateContentWithPlannedAt(contentType, title, caption, body, tagsJSON, status, "")
}

// CreateContentWithPlannedAt stores editorial planning metadata only. It never
// creates a publish job or schedules a browser action.
func (db *DB) CreateContentWithPlannedAt(contentType, title, caption, body, tagsJSON, status, plannedAt string) (*Content, error) {
	if !validContentType(contentType) || !validContentStatus(status) {
		return nil, fmt.Errorf("invalid content type or status")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO contents (type, title, caption, body, tags_json, status, planned_at, source, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'manual', ?, ?)`,
		contentType, title, caption, body, tagsJSON, status, plannedAt, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Content{ID: id, Type: contentType, Title: title, Caption: caption, Body: body, TagsJSON: tagsJSON, Status: status, PlannedAt: plannedAt, Revision: 1}, nil
}

func (db *DB) GetContent(id int64) (*Content, error) {
	var c Content
	err := db.QueryRow(`SELECT c.id, c.type, COALESCE(c.title,''), COALESCE(c.caption,''), COALESCE(c.body,''), COALESCE(c.tags_json,''), c.status, COALESCE(c.publish_count,0), COALESCE(c.planned_at,''), COALESCE(c.revision,1), COALESCE(m.id,0), COALESCE(m.kind,''), COALESCE(m.mime_type,''), COALESCE(c.raw_publish_data,'')
		FROM contents c
		LEFT JOIN media_assets m ON m.id = (SELECT id FROM media_assets WHERE content_id=c.id ORDER BY CASE WHEN role='cover' THEN 0 ELSE 1 END, position, id LIMIT 1)
		WHERE c.id = ?`, id).
		Scan(&c.ID, &c.Type, &c.Title, &c.Caption, &c.Body, &c.TagsJSON, &c.Status, &c.PublishCount, &c.PlannedAt, &c.Revision, &c.CoverMediaID, &c.CoverKind, &c.CoverMimeType, &c.RawPublishData)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("content %d not found", id)
	}
	return &c, err
}

func (db *DB) ListContents() ([]Content, error) {
	rows, err := db.Query(`SELECT c.id, c.type, COALESCE(c.title,''), COALESCE(c.caption,''), COALESCE(c.body,''), COALESCE(c.tags_json,''), c.status, COALESCE(c.publish_count,0), COALESCE(c.planned_at,''), COALESCE(c.revision,1), COALESCE(m.id,0), COALESCE(m.kind,''), COALESCE(m.mime_type,''), COALESCE(c.raw_publish_data,'')
		FROM contents c
		LEFT JOIN media_assets m ON m.id = (SELECT id FROM media_assets WHERE content_id=c.id ORDER BY CASE WHEN role='cover' THEN 0 ELSE 1 END, position, id LIMIT 1)
		ORDER BY CASE WHEN c.planned_at='' THEN 1 ELSE 0 END, c.planned_at ASC, c.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var contents []Content
	for rows.Next() {
		var c Content
		if err := rows.Scan(&c.ID, &c.Type, &c.Title, &c.Caption, &c.Body, &c.TagsJSON, &c.Status, &c.PublishCount, &c.PlannedAt, &c.Revision, &c.CoverMediaID, &c.CoverKind, &c.CoverMimeType, &c.RawPublishData); err != nil {
			return nil, err
		}
		contents = append(contents, c)
	}
	return contents, rows.Err()
}

func (db *DB) UpdateContent(id int64, title, caption, body, tagsJSON, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE contents SET title = COALESCE(NULLIF(?, ''), title), caption = COALESCE(NULLIF(?, ''), caption), body = COALESCE(NULLIF(?, ''), body), tags_json = COALESCE(NULLIF(?, ''), tags_json), status = ?, revision=revision+1, updated_at = ? WHERE id = ?`,
		title, caption, body, tagsJSON, status, now, id)
	return err
}

// ReplaceContent updates every editable field, including deliberate empty
// values. The older UpdateContent method remains for partial CLI updates.
func (db *DB) ReplaceContent(id int64, contentType, title, caption, body, tagsJSON, status string) error {
	if id <= 0 {
		return fmt.Errorf("content id must be positive")
	}
	if !validContentType(contentType) || !validContentStatus(status) {
		return fmt.Errorf("invalid content type or status")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE contents SET type=?, title=?, caption=?, body=?, tags_json=?, status=?, revision=revision+1, updated_at=? WHERE id=?`, contentType, title, caption, body, tagsJSON, status, now, id)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("content %d not found", id)
	}
	return nil
}

// ReplaceContentWithPlannedAt replaces a content draft and its local calendar
// metadata; it deliberately does not enqueue or execute publication.
func (db *DB) ReplaceContentWithPlannedAt(id int64, contentType, title, caption, body, tagsJSON, status, plannedAt string) error {
	if id <= 0 {
		return fmt.Errorf("content id must be positive")
	}
	if !validContentType(contentType) || !validContentStatus(status) {
		return fmt.Errorf("invalid content type or status")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE contents SET type=?, title=?, caption=?, body=?, tags_json=?, status=?, planned_at=?, revision=revision+1, updated_at=? WHERE id=?`, contentType, title, caption, body, tagsJSON, status, plannedAt, now, id)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("content %d not found", id)
	}
	return nil
}

func (db *DB) CloneContent(id int64) (*Content, error) {
	original, err := db.GetContent(id)
	if err != nil {
		return nil, err
	}
	title := original.Title
	if title != "" {
		title += "（副本）"
	}
	return db.CreateContent(original.Type, title, original.Caption, original.Body, original.TagsJSON, "draft")
}

func (db *DB) DeleteContent(id int64) error {
	if id <= 0 {
		return fmt.Errorf("content id must be positive")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var referenced int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE content_id=?`, id).Scan(&referenced); err != nil {
		return err
	}
	if referenced > 0 {
		return fmt.Errorf("content %d has publish jobs and cannot be deleted", id)
	}
	if _, err := tx.Exec(`DELETE FROM media_assets WHERE content_id=?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM contents WHERE id=?`, id)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("content %d not found", id)
	}
	return tx.Commit()
}

func validContentType(value string) bool {
	switch value {
	case "article", "graph_text", "video":
		return true
	default:
		return false
	}
}

func validContentStatus(value string) bool {
	switch value {
	case "draft", "published", "archived":
		return true
	default:
		return false
	}
}

// --- MediaAsset ---

type MediaAsset struct {
	ID           int64  `json:"id"`
	ContentID    int64  `json:"content_id"`
	LibraryID    int64  `json:"library_id"`
	Role         string `json:"role"`
	Position     int    `json:"position"`
	Kind         string `json:"kind"`
	FilePath     string `json:"file_path"`
	OriginalPath string `json:"original_path"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	DurationMS   int    `json:"duration_ms"`
}

func (db *DB) GetMediaAsset(id int64) (*MediaAsset, error) {
	if id <= 0 {
		return nil, fmt.Errorf("media asset id must be positive")
	}
	var asset MediaAsset
	err := db.QueryRow(`SELECT id, COALESCE(content_id,0), COALESCE(library_id,0), COALESCE(role,'content'), COALESCE(position,0), kind, file_path, COALESCE(original_path,''), COALESCE(mime_type,''), COALESCE(size_bytes,0), COALESCE(sha256,''), COALESCE(width,0), COALESCE(height,0), COALESCE(duration_ms,0) FROM media_assets WHERE id=?`, id).
		Scan(&asset.ID, &asset.ContentID, &asset.LibraryID, &asset.Role, &asset.Position, &asset.Kind, &asset.FilePath, &asset.OriginalPath, &asset.MimeType, &asset.SizeBytes, &asset.SHA256, &asset.Width, &asset.Height, &asset.DurationMS)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("media asset %d not found", id)
	}
	return &asset, err
}

func (db *DB) CreateMediaAsset(contentID int64, kind, filePath, originalPath, mimeType string, sizeBytes int64, sha256 string, width, height, durationMS int) (*MediaAsset, error) {
	library, err := db.UpsertMediaLibrary(filePath, sha256, sizeBytes, mimeType, width, height, durationMS, "")
	if err != nil {
		return nil, fmt.Errorf("index media library: %w", err)
	}
	return db.createMediaAssetFromLibrarySnapshot(contentID, library, "content", 0, kind, filePath, originalPath, mimeType, sizeBytes, sha256, width, height, durationMS)
}

// CreateMediaAssetFromLibrary creates or adopts a content association from a
// durable media-library row. Legacy media_assets with the same content/hash
// are linked in place so upgrades do not duplicate existing content media.
func (db *DB) CreateMediaAssetFromLibrary(contentID, libraryID int64) (*MediaAsset, error) {
	return db.CreateMediaAssetFromLibraryWithRole(contentID, libraryID, "content", 0)
}

// CreateMediaAssetFromLibraryWithRole keeps the content/library association
// explicit: content media are ordered independently from the optional cover.
func (db *DB) CreateMediaAssetFromLibraryWithRole(contentID, libraryID int64, role string, position int) (*MediaAsset, error) {
	if contentID <= 0 || libraryID <= 0 {
		return nil, fmt.Errorf("content id and media library id must be positive")
	}
	role = strings.TrimSpace(role)
	if role == "" {
		role = "content"
	}
	if (role != "content" && role != "cover") || position < 0 || position > 10_000 {
		return nil, fmt.Errorf("media role or position is invalid")
	}
	library, err := db.GetMediaLibraryItem(libraryID)
	if err != nil {
		return nil, err
	}
	kind := "image"
	if strings.HasPrefix(strings.ToLower(library.Mime), "video/") {
		kind = "video"
	} else if strings.HasPrefix(strings.ToLower(library.Mime), "audio/") {
		kind = "audio"
	}
	if role == "cover" && kind != "image" {
		return nil, fmt.Errorf("content cover must be an image")
	}
	asset, err := db.createMediaAssetFromLibrarySnapshot(contentID, library, role, position, kind, library.FilePath, library.FilePath, library.Mime, library.SizeBytes, library.SHA256, library.Width, library.Height, library.DurationMS)
	if err != nil {
		return nil, err
	}
	if role == "cover" {
		if _, err := db.Exec(`UPDATE media_assets SET role='content' WHERE content_id=? AND role='cover' AND id<>?`, contentID, asset.ID); err != nil {
			return nil, err
		}
	}
	return asset, nil
}

func (db *DB) createMediaAssetFromLibrarySnapshot(contentID int64, library *MediaLibraryItem, role string, position int, kind, filePath, originalPath, mimeType string, sizeBytes int64, sha256 string, width, height, durationMS int) (*MediaAsset, error) {
	if library == nil || library.ID <= 0 {
		return nil, fmt.Errorf("media library item is required")
	}
	if sha256 != "" {
		existing, err := db.FindMediaAssetByContentSHA(contentID, sha256)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.LibraryID != 0 && existing.LibraryID != library.ID {
				return nil, fmt.Errorf("content media already links to another library item")
			}
			if existing.LibraryID == 0 {
				if _, err := db.Exec(`UPDATE media_assets SET library_id=?, role=?, position=?, source='library' WHERE id=? AND library_id IS NULL`, library.ID, role, position, existing.ID); err != nil {
					return nil, err
				}
				existing.LibraryID = library.ID
			} else if _, err := db.Exec(`UPDATE media_assets SET role=?, position=? WHERE id=?`, role, position, existing.ID); err != nil {
				return nil, err
			}
			existing.Role, existing.Position = role, position
			return existing, nil
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO media_assets (content_id, library_id, role, position, kind, file_path, original_path, mime_type, size_bytes, sha256, width, height, duration_ms, source, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'library', ?)`,
		contentID, library.ID, role, position, kind, filePath, originalPath, mimeType, sizeBytes, sha256, width, height, durationMS, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &MediaAsset{ID: id, ContentID: contentID, LibraryID: library.ID, Role: role, Position: position, Kind: kind, FilePath: filePath, OriginalPath: originalPath, MimeType: mimeType, SizeBytes: sizeBytes, SHA256: sha256, Width: width, Height: height, DurationMS: durationMS}, nil
}

func (db *DB) ListMediaByContent(contentID int64) ([]MediaAsset, error) {
	rows, err := db.Query(`SELECT id, COALESCE(content_id,0), COALESCE(library_id,0), COALESCE(role,'content'), COALESCE(position,0), kind, file_path, COALESCE(original_path,''), COALESCE(mime_type,''), COALESCE(size_bytes,0), COALESCE(sha256,''), COALESCE(width,0), COALESCE(height,0), COALESCE(duration_ms,0) FROM media_assets WHERE content_id = ? ORDER BY CASE WHEN role='cover' THEN 0 ELSE 1 END, position, id`, contentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// JSON consumers treat an empty media collection as an array, never null.
	// This keeps a text-only article selectable in the local publish workflow.
	assets := make([]MediaAsset, 0)
	for rows.Next() {
		var m MediaAsset
		if err := rows.Scan(&m.ID, &m.ContentID, &m.LibraryID, &m.Role, &m.Position, &m.Kind, &m.FilePath, &m.OriginalPath, &m.MimeType, &m.SizeBytes, &m.SHA256, &m.Width, &m.Height, &m.DurationMS); err != nil {
			return nil, err
		}
		assets = append(assets, m)
	}
	return assets, rows.Err()
}

func (db *DB) FindMediaAssetByContentSHA(contentID int64, sha256Hash string) (*MediaAsset, error) {
	var asset MediaAsset
	err := db.QueryRow(`SELECT id, COALESCE(content_id,0), COALESCE(library_id,0), COALESCE(role,'content'), COALESCE(position,0), kind, file_path, COALESCE(original_path,''), COALESCE(mime_type,''), COALESCE(size_bytes,0), COALESCE(sha256,''), COALESCE(width,0), COALESCE(height,0), COALESCE(duration_ms,0) FROM media_assets WHERE content_id=? AND sha256=? ORDER BY id LIMIT 1`, contentID, sha256Hash).
		Scan(&asset.ID, &asset.ContentID, &asset.LibraryID, &asset.Role, &asset.Position, &asset.Kind, &asset.FilePath, &asset.OriginalPath, &asset.MimeType, &asset.SizeBytes, &asset.SHA256, &asset.Width, &asset.Height, &asset.DurationMS)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &asset, err
}

// DeleteMediaAsset removes the content association only. It never deletes the
// user's original local file or its deduplicated media_library index entry.
func (db *DB) DeleteMediaAsset(id int64) error {
	if id <= 0 {
		return fmt.Errorf("media asset id must be positive")
	}
	res, err := db.Exec(`DELETE FROM media_assets WHERE id=?`, id)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("media asset %d not found", id)
	}
	return nil
}

// --- BatchRun ---

type BatchRun struct {
	ID            int64
	OperationType string
	PlatformCode  string
	Status        string
	TotalItems    int
	PendingCount  int
	RunningCount  int
	SuccessCount  int
	FailedCount   int
	SkippedCount  int
	Concurrency   int
	SettingsJSON  string
}

// BatchItemInput is the immutable target metadata used when a batch run is
// created. Publish jobs are linked later by the runner after preflight.
type BatchItemInput struct {
	ItemType  string
	ItemID    int64
	AccountID int64
	AssetID   int64
	ContentID int64
	JobID     int64
}

// CreateBatchRunWithItems creates a runnable batch and every queued item in
// one transaction, so a failed request cannot leave a partial dashboard run.
func (db *DB) CreateBatchRunWithItems(operationType, platformCode, status string, concurrency int, settingsJSON string, inputs []BatchItemInput) (*BatchRun, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("batch run requires at least one item")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO batch_runs (operation_type, platform_code, status, total_items, pending_count, running_count, success_count, failed_count, skipped_count, concurrency, settings_json, started_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 0, 0, 0, 0, ?, ?, ?, ?, ?)`,
		operationType, platformCode, status, len(inputs), len(inputs), concurrency, settingsJSON, now, now, now)
	if err != nil {
		return nil, err
	}
	runID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	for _, input := range inputs {
		if _, err := tx.Exec(`INSERT INTO batch_items (batch_id, item_type, item_id, account_id, asset_id, content_id, job_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
			runID, input.ItemType, nullableID(input.ItemID), nullableID(input.AccountID), nullableID(input.AssetID), nullableID(input.ContentID), nullableID(input.JobID), now, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &BatchRun{ID: runID, OperationType: operationType, PlatformCode: platformCode, Status: status, TotalItems: len(inputs), PendingCount: len(inputs), Concurrency: concurrency, SettingsJSON: settingsJSON}, nil
}

func (db *DB) CreateBatchRun(operationType, platformCode, status string, totalItems, concurrency int, settingsJSON string) (*BatchRun, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO batch_runs (operation_type, platform_code, status, total_items, pending_count, running_count, success_count, failed_count, skipped_count, concurrency, settings_json, started_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 0, 0, 0, 0, ?, ?, ?, ?, ?)`,
		operationType, platformCode, status, totalItems, totalItems, concurrency, settingsJSON, now, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &BatchRun{ID: id, OperationType: operationType, PlatformCode: platformCode, Status: status, TotalItems: totalItems, PendingCount: totalItems, Concurrency: concurrency}, nil
}

func (db *DB) GetBatchRun(id int64) (*BatchRun, error) {
	var b BatchRun
	err := db.QueryRow(`SELECT id, operation_type, COALESCE(platform_code,''), status, total_items, pending_count, running_count, success_count, failed_count, skipped_count, concurrency, COALESCE(settings_json,'') FROM batch_runs WHERE id = ?`, id).
		Scan(&b.ID, &b.OperationType, &b.PlatformCode, &b.Status, &b.TotalItems, &b.PendingCount, &b.RunningCount, &b.SuccessCount, &b.FailedCount, &b.SkippedCount, &b.Concurrency, &b.SettingsJSON)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("batch run %d not found", id)
	}
	return &b, err
}

func (db *DB) ListBatchRuns(limit int) ([]BatchRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`SELECT id, operation_type, COALESCE(platform_code,''), status, total_items, pending_count, running_count, success_count, failed_count, skipped_count, concurrency, COALESCE(settings_json,'') FROM batch_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []BatchRun
	for rows.Next() {
		var b BatchRun
		if err := rows.Scan(&b.ID, &b.OperationType, &b.PlatformCode, &b.Status, &b.TotalItems, &b.PendingCount, &b.RunningCount, &b.SuccessCount, &b.FailedCount, &b.SkippedCount, &b.Concurrency, &b.SettingsJSON); err != nil {
			return nil, err
		}
		runs = append(runs, b)
	}
	return runs, rows.Err()
}

func (db *DB) UpdateBatchRunCounters(id int64, pending, running, success, failed, skipped int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	status := "running"
	if pending == 0 && running == 0 {
		if failed > 0 {
			status = "completed_with_errors"
		} else {
			status = "completed"
		}
	}
	_, err := db.Exec(`UPDATE batch_runs SET pending_count = ?, running_count = ?, success_count = ?, failed_count = ?, skipped_count = ?, status = ?, finished_at = CASE WHEN ? IN ('completed','completed_with_errors') THEN ? ELSE finished_at END, updated_at = ? WHERE id = ?`,
		pending, running, success, failed, skipped, status, status, now, now, id)
	return err
}

// UpdateBatchRunWaitingHuman preserves a durable handoff instead of treating
// an unfinished browser operation as a completed or failed batch.
func (db *DB) UpdateBatchRunWaitingHuman(id int64, pending, running, success, failed, skipped int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE batch_runs SET pending_count = ?, running_count = ?, success_count = ?, failed_count = ?, skipped_count = ?, status = 'waiting_human', updated_at = ? WHERE id = ?`,
		pending, running, success, failed, skipped, now, id)
	return err
}

// UpdateFleetBatchRun writes Fleet-specific progress without applying the
// publish batch status policy. Fleet additionally has waiting_human/partial
// states and represents manual-takeover items in pendingCount.
func (db *DB) UpdateFleetBatchRun(id int64, status string, pending, running, success, failed, skipped int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE batch_runs SET pending_count=?, running_count=?, success_count=?, failed_count=?, skipped_count=?, status=?,
		finished_at=CASE WHEN ? IN ('completed','failed','partial','cancelled') THEN ? ELSE NULL END, updated_at=? WHERE id=?`,
		pending, running, success, failed, skipped, status, status, now, now, id)
	return err
}

// --- BatchItem ---

type BatchItem struct {
	ID              int64
	BatchID         int64
	ItemType        string
	ItemID          int64
	AccountID       int64
	AssetID         int64
	ContentID       int64
	JobID           int64
	Status          string
	CurrentStep     string
	ErrorCode       string
	ErrorMessage    string
	ScreenshotPath  string
	DOMSnapshotPath string
	StartedAt       string
	FinishedAt      string
}

func (db *DB) CreateBatchItem(batchID int64, itemType string, itemID, accountID, assetID, contentID, jobID int64) (*BatchItem, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO batch_items (batch_id, item_type, item_id, account_id, asset_id, content_id, job_id, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
		batchID, itemType, nullableID(itemID), nullableID(accountID), nullableID(assetID), nullableID(contentID), nullableID(jobID), now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &BatchItem{ID: id, BatchID: batchID, ItemType: itemType, Status: "pending"}, nil
}

func (db *DB) UpdateBatchItemStatus(id int64, status, currentStep, errorCode, errorMessage string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE batch_items SET status = ?, current_step = ?, error_code = ?, error_message = ?,
		started_at = CASE WHEN ? = 'running' THEN COALESCE(started_at, ?) ELSE started_at END,
		finished_at = CASE WHEN ? = 'running' THEN NULL WHEN ? IN ('success','failed','skipped','cancelled') THEN ? ELSE finished_at END,
		updated_at = ? WHERE id = ?`,
		status, currentStep, errorCode, errorMessage, status, now, status, status, now, now, id)
	return err
}

// ResetBatchItemForResume reopens only the exact terminal/interrupted state
// observed by the caller. It cannot move a concurrently claimed running item
// back to pending.
func (db *DB) ResetBatchItemForResume(id int64, fromStatus string) (bool, error) {
	switch fromStatus {
	case "failed", "interrupted", "retryable", "retryable_failed":
	default:
		return false, fmt.Errorf("batch item status %q cannot be reset for resume", fromStatus)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE batch_items SET status='pending', current_step='', error_code='', error_message='',
		finished_at=NULL, updated_at=? WHERE id=? AND status=?`, now, id, fromStatus)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	return changed == 1, err
}

// ClaimPendingBatchItem is the item-level execution fence. Two concurrent
// resume commands can list the same pending row, but only one may run it.
func (db *DB) ClaimPendingBatchItem(id int64, currentStep string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE batch_items SET status='running', current_step=?, error_code='', error_message='',
		started_at=COALESCE(started_at, ?), finished_at=NULL, updated_at=? WHERE id=? AND status='pending'`,
		currentStep, now, now, id)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	return changed == 1, err
}

// UpdateBatchItemJob links a publish_jobs row to the batch item.
func (db *DB) UpdateBatchItemJob(id, jobID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE batch_items SET job_id = ?, updated_at = ? WHERE id = ?`, jobID, now, id)
	return err
}

func (db *DB) ListBatchItems(batchID int64) ([]BatchItem, error) {
	rows, err := db.Query(`SELECT id, batch_id, item_type, COALESCE(item_id,0), COALESCE(account_id,0), COALESCE(asset_id,0), COALESCE(content_id,0), COALESCE(job_id,0), status, COALESCE(current_step,''), COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(screenshot_path,''), COALESCE(dom_snapshot_path,''), COALESCE(started_at,''), COALESCE(finished_at,'') FROM batch_items WHERE batch_id = ? ORDER BY id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []BatchItem
	for rows.Next() {
		var bi BatchItem
		if err := rows.Scan(&bi.ID, &bi.BatchID, &bi.ItemType, &bi.ItemID, &bi.AccountID, &bi.AssetID, &bi.ContentID, &bi.JobID, &bi.Status, &bi.CurrentStep, &bi.ErrorCode, &bi.ErrorMessage, &bi.ScreenshotPath, &bi.DOMSnapshotPath, &bi.StartedAt, &bi.FinishedAt); err != nil {
			return nil, err
		}
		items = append(items, bi)
	}
	return items, rows.Err()
}

// --- PublishJob ---

type PublishJob struct {
	ID                int64
	BatchID           int64
	PlanID            int64
	PlanTargetID      int64
	ContentID         int64
	AccountID         int64
	AssetID           int64
	PlatformCode      string
	ContentType       string
	Mode              string
	Status            string
	CurrentStep       string
	SettingsJSON      string
	RetryCount        int
	MaxRetries        int
	ErrorCode         string
	ErrorMessage      string
	TraceID           string
	IdempotencyKey    string
	CancelRequestedAt string
	SnapshotHash      string
	SubmitAttemptedAt string
	SubmitOutcome     string
	CreatedAt         string
}

func (db *DB) CreatePublishJob(batchID, contentID, accountID, assetID int64, platformCode, contentType string) (*PublishJob, error) {
	idempotencyKey := ""
	if batchID > 0 {
		idempotencyKey = fmt.Sprintf("batch:%d:content:%d:account:%d:asset:%d:platform:%s:type:%s", batchID, contentID, accountID, assetID, platformCode, contentType)
	}
	return db.CreatePublishJobWithIdempotencyKey(batchID, contentID, accountID, assetID, platformCode, contentType, idempotencyKey)
}

// CreatePublishJobWithIdempotencyKey creates one pending publish job. Callers
// that can receive a replay (HTTP, queue, MCP or workflow) must pass a stable
// caller-generated key; an empty key intentionally represents a new manual run.
func (db *DB) CreatePublishJobWithIdempotencyKey(batchID, contentID, accountID, assetID int64, platformCode, contentType, idempotencyKey string) (*PublishJob, error) {
	return db.CreatePublishJobWithIntent(batchID, contentID, accountID, assetID, platformCode, contentType, idempotencyKey, "browser", "")
}

// CreatePublishJobWithIntent atomically binds the caller's execution mode and
// immutable settings snapshot to the idempotency key. A replay can observe the
// existing job, but it cannot change what that job is allowed to do.
func (db *DB) CreatePublishJobWithIntent(batchID, contentID, accountID, assetID int64, platformCode, contentType, idempotencyKey, mode, settingsJSON string) (*PublishJob, error) {
	job, _, err := db.CreatePublishJobWithIntentResult(batchID, contentID, accountID, assetID, platformCode, contentType, idempotencyKey, mode, settingsJSON)
	return job, err
}

// FindPublishJobByIdempotencyKey returns an existing request without creating
// work. It is used to bypass live-browser preflight when a user retries the
// same already-created intent.
func (db *DB) FindPublishJobByIdempotencyKey(idempotencyKey string) (*PublishJob, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, false, nil
	}
	if !validIdempotencyKey(idempotencyKey) {
		return nil, false, fmt.Errorf("invalid publish idempotency key")
	}
	var id int64
	err := db.QueryRow(`SELECT id FROM publish_jobs WHERE idempotency_key=? AND idempotency_key IS NOT NULL AND idempotency_key<>''`, idempotencyKey).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	job, err := db.GetPublishJob(id)
	if err != nil {
		return nil, false, err
	}
	return job, true, nil
}

// CreatePublishJobWithIntentResult is CreatePublishJobWithIntent with an
// explicit creation marker. A false marker means the exact immutable request
// already exists and must be observed rather than executed again.
func (db *DB) CreatePublishJobWithIntentResult(batchID, contentID, accountID, assetID int64, platformCode, contentType, idempotencyKey, mode, settingsJSON string) (*PublishJob, bool, error) {
	return db.createPublishJobWithIntentResult(0, 0, "", batchID, contentID, accountID, assetID, platformCode, contentType, idempotencyKey, mode, settingsJSON)
}

// CreatePublishPlanJobWithIntentResult binds execution to one immutable
// publish-plan target. The link and the target's job pointer commit together,
// so retries can only reconstruct input from the reviewed frozen snapshot.
func (db *DB) CreatePublishPlanJobWithIntentResult(planID, planTargetID int64, snapshotHash string, batchID, contentID, accountID, assetID int64, platformCode, contentType, idempotencyKey, mode, settingsJSON string) (*PublishJob, bool, error) {
	if planID <= 0 || planTargetID <= 0 || !validSnapshotHash(snapshotHash) {
		return nil, false, fmt.Errorf("valid frozen publish plan link is required")
	}
	return db.createPublishJobWithIntentResult(planID, planTargetID, snapshotHash, batchID, contentID, accountID, assetID, platformCode, contentType, idempotencyKey, mode, settingsJSON)
}

func replaceablePreparedJobID(tx *sql.Tx, planID, planTargetID int64, snapshotHash string, contentID, accountID, assetID int64, platformCode, contentType, mode, settingsJSON string) (int64, error) {
	if planTargetID <= 0 {
		return 0, nil
	}
	var existingJobID int64
	var planStatus, approvedAction, targetStatus string
	err := tx.QueryRow(`SELECT COALESCE(target.publish_job_id,0), plan.status, COALESCE(plan.approved_action,''), target.status
		FROM publish_plans AS plan
		JOIN publish_plan_targets AS target ON target.plan_id=plan.id
		WHERE plan.id=? AND target.id=? AND plan.snapshot_hash=? AND plan.content_id=? AND plan.media_mode=?
		  AND target.account_id=? AND target.asset_id=? AND target.platform_code=? AND target.settings_json=?`,
		planID, planTargetID, snapshotHash, contentID, contentType,
		accountID, assetID, platformCode, settingsJSON,
	).Scan(&existingJobID, &planStatus, &approvedAction, &targetStatus)
	if err != nil {
		return 0, fmt.Errorf("%w: frozen publish target does not match the requested job", ErrPublishJobConflict)
	}
	approvedForMode := approvedAction == mode || (mode == "submit" && approvedAction == "acceptance_submit")
	if (planStatus != "approved" && planStatus != "running") || !approvedForMode {
		return 0, fmt.Errorf("%w: publish plan is not approved for %s", ErrPublishJobConflict, mode)
	}
	if existingJobID == 0 {
		if mode == "submit" {
			var ready int
			if err := tx.QueryRow(`SELECT
				COALESCE(SUM(CASE WHEN mode='prepare' AND status='awaiting_review' AND current_step IN ('ready_to_submit','review_or_submit') AND COALESCE(submit_attempted_at,'')='' THEN 1 ELSE 0 END),0)
				FROM publish_jobs WHERE plan_id=? AND plan_target_id=? AND snapshot_hash=? AND content_id=? AND account_id=? AND COALESCE(asset_id,0)=? AND platform_code=? AND content_type=? AND COALESCE(settings_json,'')=?`,
				planID, planTargetID, snapshotHash, contentID, accountID, assetID, platformCode, contentType, settingsJSON,
			).Scan(&ready); err == nil && ready > 0 && targetStatus == "failed" {
				return 0, nil
			}
			return 0, fmt.Errorf("%w: prepared publish target is required for submit", ErrPublishJobConflict)
		}
		return 0, nil
	}
	if mode == "prepare" {
		if targetStatus != "awaiting_review" && targetStatus != "manual_takeover" && targetStatus != "failed" && targetStatus != "cancelled" {
			return 0, fmt.Errorf("%w: frozen publish target is not eligible for another prepare attempt", ErrPublishJobConflict)
		}
		previous, previousErr := getPublishJobWith(tx, existingJobID)
		// A submit run that reached no trusted final click is recoverable by a
		// fresh prepare. It cannot be replayed as a submit; the new prepare must
		// rebuild the reviewed form from the frozen snapshot.
		unattemptedSubmit := previousErr == nil && previous.Mode == "submit" &&
			(previous.Status == "failed" || previous.Status == "manual_takeover") &&
			previous.SubmitAttemptedAt == ""
		if previousErr != nil ||
			previous.PlanID != planID || previous.PlanTargetID != planTargetID || previous.SnapshotHash != snapshotHash ||
			previous.ContentID != contentID || previous.AccountID != accountID || previous.AssetID != assetID ||
			previous.PlatformCode != platformCode || previous.ContentType != contentType || previous.SettingsJSON != settingsJSON ||
			(previous.Mode != "prepare" && !unattemptedSubmit) || previous.SubmitAttemptedAt != "" {
			return 0, fmt.Errorf("%w: previous prepare job does not match the frozen target", ErrPublishJobConflict)
		}
		if unattemptedSubmit {
			return existingJobID, nil
		}
		if previous.Status == "awaiting_review" &&
			(previous.CurrentStep == "ready_to_submit" || previous.CurrentStep == "review_or_submit") {
			return 0, fmt.Errorf("%w: prepared publish target is already ready for submit", ErrPublishJobConflict)
		}
		return existingJobID, nil
	}
	if mode == "submit" && (targetStatus == "failed" || targetStatus == "manual_takeover") {
		previous, previousErr := getPublishJobWith(tx, existingJobID)
		var ready int
		readyErr := tx.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE plan_id=? AND plan_target_id=? AND snapshot_hash=? AND content_id=? AND account_id=? AND COALESCE(asset_id,0)=? AND platform_code=? AND content_type=? AND COALESCE(settings_json,'')=? AND mode='prepare' AND status='awaiting_review' AND current_step IN ('ready_to_submit','review_or_submit') AND COALESCE(submit_attempted_at,'')=''`,
			planID, planTargetID, snapshotHash, contentID, accountID, assetID, platformCode, contentType, settingsJSON,
		).Scan(&ready)
		if previousErr == nil && previous.Mode == "submit" &&
			(previous.Status == "failed" || previous.Status == "manual_takeover") &&
			previous.SubmitAttemptedAt == "" && readyErr == nil && ready > 0 {
			return existingJobID, nil
		}
	}
	if mode != "submit" || targetStatus != "awaiting_review" {
		return 0, fmt.Errorf("%w: frozen publish target already belongs to another job", ErrPublishJobConflict)
	}
	prepareJob, err := getPublishJobWith(tx, existingJobID)
	if err != nil ||
		prepareJob.PlanID != planID || prepareJob.PlanTargetID != planTargetID || prepareJob.SnapshotHash != snapshotHash ||
		prepareJob.ContentID != contentID || prepareJob.AccountID != accountID || prepareJob.AssetID != assetID ||
		prepareJob.PlatformCode != platformCode || prepareJob.ContentType != contentType || prepareJob.SettingsJSON != settingsJSON ||
		prepareJob.Mode != "prepare" || prepareJob.Status != "awaiting_review" ||
		(prepareJob.CurrentStep != "ready_to_submit" && prepareJob.CurrentStep != "review_or_submit") ||
		prepareJob.SubmitAttemptedAt != "" {
		return 0, fmt.Errorf("%w: prepared publish target is not ready for submit", ErrPublishJobConflict)
	}
	return existingJobID, nil
}

func (db *DB) createPublishJobWithIntentResult(planID, planTargetID int64, snapshotHash string, batchID, contentID, accountID, assetID int64, platformCode, contentType, idempotencyKey, mode, settingsJSON string) (*PublishJob, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey != "" && !validIdempotencyKey(idempotencyKey) {
		return nil, false, fmt.Errorf("invalid publish idempotency key")
	}
	if !validPublishJobMode(mode) {
		return nil, false, fmt.Errorf("invalid publish job mode %q", mode)
	}
	settingsJSON = strings.TrimSpace(settingsJSON)
	if len(settingsJSON) > 64<<10 {
		return nil, false, fmt.Errorf("publish job settings exceed 64 KiB")
	}
	if settingsJSON != "" && !json.Valid([]byte(settingsJSON)) {
		return nil, false, fmt.Errorf("publish job settings must be JSON")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if idempotencyKey != "" {
		var existingID int64
		lookupErr := tx.QueryRow(`SELECT id FROM publish_jobs WHERE idempotency_key=? AND idempotency_key IS NOT NULL AND idempotency_key<>''`, idempotencyKey).Scan(&existingID)
		if lookupErr == nil {
			existing, getErr := getPublishJobWith(tx, existingID)
			if getErr != nil {
				return nil, false, getErr
			}
			if !samePublishJobIntent(existing, planID, planTargetID, snapshotHash, batchID, contentID, accountID, assetID, platformCode, contentType, mode, settingsJSON) {
				return nil, false, fmt.Errorf("%w: publish idempotency key conflicts with an existing request", ErrPublishJobConflict)
			}
			return existing, false, nil
		}
		if lookupErr != sql.ErrNoRows {
			return nil, false, lookupErr
		}
	}
	previousJobID, err := replaceablePreparedJobID(tx, planID, planTargetID, snapshotHash, contentID, accountID, assetID, platformCode, contentType, mode, settingsJSON)
	if err != nil {
		return nil, false, err
	}
	res, err := tx.Exec(`INSERT INTO publish_jobs
		(batch_id, plan_id, plan_target_id, snapshot_hash, content_id, account_id, asset_id, platform_code, content_type, mode, status, settings_json, max_retries, idempotency_key, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, 2, ?, ?, ?)`,
		nullableID(batchID), nullableID(planID), nullableID(planTargetID), snapshotHash,
		contentID, accountID, nullableID(assetID), platformCode, contentType, mode,
		nullableString(settingsJSON), nullableString(idempotencyKey), now, now)
	if err != nil {
		if idempotencyKey != "" {
			var existingID int64
			if lookupErr := tx.QueryRow(`SELECT id FROM publish_jobs WHERE idempotency_key=? AND idempotency_key IS NOT NULL AND idempotency_key<>''`, idempotencyKey).Scan(&existingID); lookupErr == nil {
				existing, getErr := getPublishJobWith(tx, existingID)
				if getErr != nil {
					return nil, false, getErr
				}
				if !samePublishJobIntent(existing, planID, planTargetID, snapshotHash, batchID, contentID, accountID, assetID, platformCode, contentType, mode, settingsJSON) {
					return nil, false, fmt.Errorf("%w: publish idempotency key conflicts with an existing request", ErrPublishJobConflict)
				}
				return existing, false, nil
			}
		}
		return nil, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, false, fmt.Errorf("publish job id: %w", err)
	}
	if planTargetID > 0 {
		var targetResult sql.Result
		var targetErr error
		if previousJobID > 0 {
			if mode == "prepare" {
				targetResult, targetErr = tx.Exec(`UPDATE publish_plan_targets
					SET publish_job_id=?, status='pending', error_code='', updated_at=?
					WHERE id=? AND plan_id=? AND publish_job_id=? AND status IN ('awaiting_review','manual_takeover','failed','cancelled')`,
					id, now, planTargetID, planID, previousJobID)
			} else {
				targetResult, targetErr = tx.Exec(`UPDATE publish_plan_targets
					SET publish_job_id=?, status='pending', error_code='', updated_at=?
					WHERE id=? AND plan_id=? AND publish_job_id=? AND status IN ('awaiting_review','failed','manual_takeover')`,
					id, now, planTargetID, planID, previousJobID)
			}
		} else {
			targetResult, targetErr = tx.Exec(`UPDATE publish_plan_targets
				SET publish_job_id=?, status='pending', error_code='', updated_at=?
				WHERE id=? AND plan_id=? AND (publish_job_id IS NULL OR publish_job_id=?)`,
				id, now, planTargetID, planID, id)
		}
		if targetErr != nil {
			return nil, false, targetErr
		}
		changed, targetErr := targetResult.RowsAffected()
		if targetErr != nil {
			return nil, false, targetErr
		}
		if changed != 1 {
			return nil, false, fmt.Errorf("%w: frozen publish target already belongs to another job", ErrPublishJobConflict)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return &PublishJob{
		ID: id, BatchID: batchID, PlanID: planID, PlanTargetID: planTargetID, SnapshotHash: snapshotHash,
		ContentID: contentID, AccountID: accountID, AssetID: assetID, PlatformCode: platformCode,
		ContentType: contentType, Mode: mode, Status: "pending", SettingsJSON: settingsJSON,
		MaxRetries: 2, IdempotencyKey: idempotencyKey,
	}, true, nil
}

func samePublishJobIntent(existing *PublishJob, planID, planTargetID int64, snapshotHash string, batchID, contentID, accountID, assetID int64, platformCode, contentType, mode, settingsJSON string) bool {
	return existing != nil &&
		existing.PlanID == planID && existing.PlanTargetID == planTargetID && existing.SnapshotHash == snapshotHash &&
		existing.BatchID == batchID && existing.ContentID == contentID && existing.AccountID == accountID &&
		existing.AssetID == assetID && existing.PlatformCode == platformCode && existing.ContentType == contentType &&
		existing.Mode == mode && existing.SettingsJSON == settingsJSON
}

func validSnapshotHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func validPublishJobMode(mode string) bool {
	switch mode {
	case "browser", "api", "hybrid", "dry_run", "prepare", "submit":
		return true
	default:
		return false
	}
}

func validIdempotencyKey(value string) bool {
	if len(value) < 8 || len(value) > 200 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '.' && r != '_' && r != '-' && r != ':' {
			return false
		}
	}
	return true
}

const publishJobSelectColumns = `id, COALESCE(batch_id,0), COALESCE(plan_id,0), COALESCE(plan_target_id,0),
	content_id, account_id, COALESCE(asset_id,0), platform_code, content_type, mode, status,
	COALESCE(current_step,''), COALESCE(settings_json,''), retry_count, max_retries,
	COALESCE(error_code,''), COALESCE(error_message,''), COALESCE(trace_id,''),
	COALESCE(idempotency_key,''), COALESCE(cancel_requested_at,''), COALESCE(snapshot_hash,''),
	COALESCE(submit_attempted_at,''), COALESCE(submit_outcome,''), COALESCE(created_at,'')`

type publishJobQueryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

func getPublishJobWith(queryer publishJobQueryRower, id int64) (*PublishJob, error) {
	var j PublishJob
	err := queryer.QueryRow(`SELECT `+publishJobSelectColumns+` FROM publish_jobs WHERE id = ?`, id).
		Scan(&j.ID, &j.BatchID, &j.PlanID, &j.PlanTargetID,
			&j.ContentID, &j.AccountID, &j.AssetID, &j.PlatformCode, &j.ContentType, &j.Mode, &j.Status,
			&j.CurrentStep, &j.SettingsJSON, &j.RetryCount, &j.MaxRetries, &j.ErrorCode, &j.ErrorMessage,
			&j.TraceID, &j.IdempotencyKey, &j.CancelRequestedAt, &j.SnapshotHash,
			&j.SubmitAttemptedAt, &j.SubmitOutcome, &j.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
	}
	return &j, err
}

func (db *DB) GetPublishJob(id int64) (*PublishJob, error) {
	return getPublishJobWith(db, id)
}

// IsPublishJobCancelRequested is the runner's durable cancellation probe.
func (db *DB) IsPublishJobCancelRequested(id int64) (bool, error) {
	if id <= 0 {
		return false, fmt.Errorf("publish job id must be positive")
	}
	var requested int
	err := db.QueryRow(`SELECT CASE WHEN cancel_requested_at IS NULL OR cancel_requested_at='' THEN 0 ELSE 1 END FROM publish_jobs WHERE id=?`, id).Scan(&requested)
	if err == sql.ErrNoRows {
		return false, fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
	}
	return requested == 1, err
}

// RequestPublishJobCancel persists the cancellation before returning. Pending
// and human-waiting work is stopped immediately; only an active runner keeps
// status=running while its per-run watcher cancels the adapter context.
func (db *DB) RequestPublishJobCancel(id int64) (string, *PublishJob, error) {
	if id <= 0 {
		return "", nil, fmt.Errorf("publish job id must be positive")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	var status, cancelRequestedAt, submitAttemptedAt string
	err = tx.QueryRow(`SELECT status, COALESCE(cancel_requested_at,''), COALESCE(submit_attempted_at,'')
		FROM publish_jobs WHERE id=?`, id).Scan(&status, &cancelRequestedAt, &submitAttemptedAt)
	if err == sql.ErrNoRows {
		return "", nil, fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
	}
	if err != nil {
		return "", nil, err
	}
	if cancelRequestedAt != "" {
		return "", nil, fmt.Errorf("%w: cancellation already requested", ErrPublishJobConflict)
	}
	if submitAttemptedAt != "" {
		return "", nil, fmt.Errorf("%w: submit was attempted; verify the creator backend before any other action", ErrPublishJobConflict)
	}
	action := "cancelled"
	var result sql.Result
	switch status {
	case "pending", "queued", "manual_takeover", "awaiting_review":
		result, err = tx.Exec(`UPDATE publish_jobs SET status='cancelled', current_step='cancelled', error_code='', error_message='', cancel_requested_at=?, finished_at=?, updated_at=? WHERE id=? AND status=? AND cancel_requested_at IS NULL`, now, now, now, id, status)
	case "running":
		action = "cancel_requested"
		result, err = tx.Exec(`UPDATE publish_jobs SET cancel_requested_at=?, updated_at=? WHERE id=? AND status='running' AND cancel_requested_at IS NULL`, now, now, id)
	default:
		return "", nil, fmt.Errorf("%w: cannot cancel status %q", ErrPublishJobConflict, status)
	}
	if err != nil {
		return "", nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return "", nil, err
	}
	if changed != 1 {
		return "", nil, fmt.Errorf("%w: cancellation race lost", ErrPublishJobConflict)
	}
	eventType := "job.cancelled"
	message := "publish cancelled"
	if action == "cancel_requested" {
		eventType = "job.cancel_requested"
		message = "publish cancellation requested"
	}
	if _, err := tx.Exec(`INSERT INTO job_events (job_id, level, step, event_type, message, retryable, schema_version, created_at) VALUES (?, 'info', 'cancelled', ?, ?, 0, 1, ?)`, id, eventType, message, now); err != nil {
		return "", nil, err
	}
	if err := tx.Commit(); err != nil {
		return "", nil, err
	}
	job, err := db.GetPublishJob(id)
	return action, job, err
}

// MarkPublishSubmitAttempt is the durable single-click gate. It is written
// immediately before the reviewed publisher is allowed to enter its submit
// path. Once present, neither retry nor resume can replay the job blindly.
func (db *DB) MarkPublishSubmitAttempt(id int64) error {
	if id <= 0 {
		return fmt.Errorf("publish job id must be positive")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`UPDATE publish_jobs
		SET submit_attempted_at=?, submit_outcome='attempted', updated_at=?
		WHERE id=? AND status='running' AND mode='submit'
		  AND COALESCE(submit_attempted_at,'')=''
		  AND COALESCE(cancel_requested_at,'')=''
		  AND COALESCE(plan_id,0)>0 AND COALESCE(plan_target_id,0)>0
		  AND length(COALESCE(snapshot_hash,''))=64
		  AND EXISTS (
			SELECT 1 FROM publish_plan_targets AS target
			JOIN publish_plans AS plan ON plan.id=target.plan_id
			WHERE plan.id=publish_jobs.plan_id
			  AND target.id=publish_jobs.plan_target_id
			  AND target.publish_job_id=publish_jobs.id
			  AND plan.snapshot_hash=publish_jobs.snapshot_hash
		  )`, now, now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		return nil
	}
	var exists int
	if lookupErr := db.QueryRow(`SELECT 1 FROM publish_jobs WHERE id=?`, id).Scan(&exists); lookupErr == sql.ErrNoRows {
		return fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
	}
	return fmt.Errorf("%w: submit requires one running frozen-plan job and cannot be replayed", ErrPublishJobConflict)
}

// ReleasePublishSubmitReservation clears the pre-submit crash gate only when
// the reviewed runner explicitly returned before clicking the final control.
// The job and its frozen target enter manual_takeover in the same transaction,
// so a crash after this call cannot leave a running/submitting half-state. A
// crash, unknown response, changed job state, or recorded outcome cannot use
// this path and therefore remains permanently blocked from blind replay.
func (db *DB) ReleasePublishSubmitReservation(id int64) error {
	if id <= 0 {
		return fmt.Errorf("publish job id must be positive")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE publish_jobs
		SET status='manual_takeover', current_step='submit_not_clicked',
			error_code='', error_message='', finished_at=NULL,
			submit_attempted_at='', submit_outcome='not_clicked', updated_at=?
		WHERE id=? AND status='running' AND current_step='submit_approved' AND mode='submit'
		  AND COALESCE(submit_attempted_at,'')<>'' AND submit_outcome='attempted'
		  AND COALESCE(cancel_requested_at,'')=''`, now, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: submit reservation cannot be released", ErrPublishJobConflict)
	}
	targetResult, err := tx.Exec(`UPDATE publish_plan_targets SET
		status='manual_takeover', error_code='', updated_at=?
		WHERE id=(SELECT plan_target_id FROM publish_jobs WHERE id=?)
		  AND publish_job_id=? AND status IN ('pending','submitting')`, now, id, id)
	if err != nil {
		return err
	}
	targetChanged, err := targetResult.RowsAffected()
	if err != nil {
		return err
	}
	if targetChanged != 1 {
		return fmt.Errorf("%w: frozen submit target cannot be released", ErrPublishJobConflict)
	}
	if _, err := tx.Exec(`INSERT INTO job_events
		(job_id,level,step,event_type,message,retryable,schema_version,created_at)
		VALUES (?,'info','submit_not_clicked','submit.not_clicked','reviewed runner confirmed final control was not clicked',0,1,?)`, id, now); err != nil {
		return err
	}
	return tx.Commit()
}

// SetPublishSubmitOutcome records only a bounded, non-secret outcome class.
// It never clears submit_attempted_at.
func (db *DB) SetPublishSubmitOutcome(id int64, outcome string) error {
	switch outcome {
	case "attempted", "verification_required", "unknown", "failed", "verified_not_published", "verified_published", "scheduled_verified":
	default:
		return fmt.Errorf("invalid publish submit outcome")
	}
	result, err := db.Exec(`UPDATE publish_jobs SET submit_outcome=?, updated_at=?
		WHERE id=? AND COALESCE(submit_attempted_at,'')<>''`,
		outcome, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: publish submit outcome has no matching attempt", ErrPublishJobConflict)
	}
	return nil
}

// ResolvePublishSubmitScheduled records creator-backend proof that a native
// scheduled test existed and was subsequently cancelled. It intentionally
// does not create a publish_record because the content never became public.
func (db *DB) ResolvePublishSubmitScheduled(
	id int64,
	evidencePath, evidenceSHA256,
	cancelledPath, cancelledSHA256 string,
) error {
	if id <= 0 {
		return fmt.Errorf("publish job id must be positive")
	}
	evidencePath = filepath.Clean(strings.TrimSpace(evidencePath))
	cancelledPath = filepath.Clean(strings.TrimSpace(cancelledPath))
	evidenceSHA256 = strings.ToLower(strings.TrimSpace(evidenceSHA256))
	cancelledSHA256 = strings.ToLower(strings.TrimSpace(cancelledSHA256))
	validEvidence := func(path, digest string) bool {
		decoded, err := hex.DecodeString(digest)
		if !filepath.IsAbs(path) || err != nil || len(decoded) != sha256.Size {
			return false
		}
		_, err = os.Stat(path)
		return err == nil
	}
	if !validEvidence(evidencePath, evidenceSHA256) || !validEvidence(cancelledPath, cancelledSHA256) {
		return fmt.Errorf("scheduled and cancellation evidence are required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	raw, _ := json.Marshal(map[string]string{
		"evidence_sha256":           evidenceSHA256,
		"cancelled_screenshot_path": cancelledPath,
		"cancelled_sha256":          cancelledSHA256,
	})
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var attemptedAt, outcome string
	var planID, targetID int64
	var published int
	if err := tx.QueryRow(`SELECT COALESCE(submit_attempted_at,''), COALESCE(submit_outcome,''),
		COALESCE(plan_id,0), COALESCE(plan_target_id,0),
		(SELECT COUNT(*) FROM publish_records WHERE job_id=publish_jobs.id AND status='published')
		FROM publish_jobs WHERE id=?`, id).
		Scan(&attemptedAt, &outcome, &planID, &targetID, &published); err == sql.ErrNoRows {
		return fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
	} else if err != nil {
		return err
	}
	if attemptedAt == "" || planID <= 0 || targetID <= 0 || published > 0 || outcome == "verified_published" {
		return fmt.Errorf("%w: submit outcome cannot be resolved as scheduled", ErrPublishJobConflict)
	}
	if _, err := tx.Exec(`UPDATE publish_jobs
		SET status='success', current_step='creator_backend_scheduled_verified',
		    error_code='', error_message='', submit_outcome='scheduled_verified',
		    finished_at=COALESCE(finished_at,?), updated_at=?
		WHERE id=?`, now, now, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE publish_plan_targets
		SET status='scheduled_verified', error_code='', updated_at=?
		WHERE id=? AND plan_id=? AND publish_job_id=?`, now, targetID, planID, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE publish_plans SET status='scheduled_verified', updated_at=?
		WHERE id=? AND NOT EXISTS (
			SELECT 1 FROM publish_plan_targets
			WHERE plan_id=? AND status NOT IN ('scheduled_verified','published')
		)`, now, planID, planID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO job_events
		(job_id, level, step, event_type, message, screenshot_path, raw_json,
		 error_code, retryable, schema_version, created_at)
		VALUES (?, 'info', 'creator_backend_scheduled_verified',
		 'job.creator_backend_scheduled_verified',
		 'creator backend confirmed native schedule; test schedule cancelled', ?, ?,
		 '', 0, 1, ?)`,
		id, evidencePath, string(raw), now); err != nil {
		return err
	}
	return tx.Commit()
}

// ResolvePublishSubmitNotPublished records a creator-backend negative check
// without erasing the immutable submit attempt. A later frozen plan may retry
// only because this explicit evidence resolves the earlier duplicate risk.
func (db *DB) ResolvePublishSubmitNotPublished(id int64, screenshotPath, evidenceSHA256 string) error {
	if id <= 0 {
		return fmt.Errorf("publish job id must be positive")
	}
	screenshotPath = filepath.Clean(strings.TrimSpace(screenshotPath))
	evidenceSHA256 = strings.ToLower(strings.TrimSpace(evidenceSHA256))
	if decoded, err := hex.DecodeString(evidenceSHA256); !filepath.IsAbs(screenshotPath) || err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("verified-not-published evidence is required")
	}
	if _, err := os.Stat(screenshotPath); err != nil {
		return fmt.Errorf("verified-not-published evidence is unavailable")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	raw, _ := json.Marshal(map[string]string{"evidence_sha256": evidenceSHA256})
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var attemptedAt, outcome string
	var published int
	if err := tx.QueryRow(`SELECT COALESCE(submit_attempted_at,''), COALESCE(submit_outcome,''),
		(SELECT COUNT(*) FROM publish_records WHERE job_id=publish_jobs.id AND status='published')
		FROM publish_jobs WHERE id=?`, id).Scan(&attemptedAt, &outcome, &published); err == sql.ErrNoRows {
		return fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
	} else if err != nil {
		return err
	}
	if attemptedAt == "" || published > 0 || outcome == "verified_published" {
		return fmt.Errorf("%w: submit outcome cannot be resolved as absent", ErrPublishJobConflict)
	}
	if _, err := tx.Exec(`UPDATE publish_jobs
		SET status='failed', current_step='creator_backend_verified_absent',
		    error_code='CREATOR_BACKEND_NOT_FOUND',
		    error_message='creator backend contains no matching item',
		    submit_outcome='verified_not_published', updated_at=?
		WHERE id=?`, now, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO job_events
		(job_id, level, step, event_type, message, screenshot_path, raw_json,
		 error_code, retryable, schema_version, created_at)
		VALUES (?, 'warn', 'creator_backend_verified_absent',
		 'job.creator_backend_not_published',
		 'creator backend contains no matching item', ?, ?,
		 'CREATOR_BACKEND_NOT_FOUND', 0, 1, ?)`,
		id, screenshotPath, string(raw), now); err != nil {
		return err
	}
	return tx.Commit()
}

// CanRetryPublishJob returns true only when the latest durable outcome says
// retryable and the job still has retry budget.
func (db *DB) CanRetryPublishJob(id int64) (bool, error) {
	if id <= 0 {
		return false, fmt.Errorf("publish job id must be positive")
	}
	var status, mode, snapshotHash string
	var retryCount, maxRetries int
	var submitAttemptedAt string
	var planID, planTargetID int64
	err := db.QueryRow(`SELECT status, mode, retry_count, max_retries, COALESCE(submit_attempted_at,''),
		COALESCE(plan_id,0), COALESCE(plan_target_id,0), COALESCE(snapshot_hash,'')
		FROM publish_jobs WHERE id=?`, id).
		Scan(&status, &mode, &retryCount, &maxRetries, &submitAttemptedAt, &planID, &planTargetID, &snapshotHash)
	if err == sql.ErrNoRows {
		return false, fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
	}
	if err != nil {
		return false, err
	}
	if status != "failed" || retryCount >= maxRetries || submitAttemptedAt != "" {
		return false, nil
	}
	if mode != "dry_run" {
		var snapshotJSON string
		if planID <= 0 || planTargetID <= 0 || !validSnapshotHash(snapshotHash) {
			return false, nil
		}
		err = db.QueryRow(`SELECT plan.snapshot_json FROM publish_plan_targets AS target
			JOIN publish_plans AS plan ON plan.id=target.plan_id
			WHERE plan.id=? AND target.id=? AND target.publish_job_id=?
			  AND plan.snapshot_hash=?`, planID, planTargetID, id, snapshotHash).Scan(&snapshotJSON)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		digest := sha256.Sum256([]byte(snapshotJSON))
		if fmt.Sprintf("%x", digest[:]) != snapshotHash {
			return false, nil
		}
	}
	var retryable int
	err = db.QueryRow(`SELECT retryable FROM job_events WHERE job_id=? ORDER BY id DESC LIMIT 1`, id).Scan(&retryable)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return retryable == 1, err
}

// RetryPublishJob atomically reopens a retryable failed job and enqueues the
// existing job for the real publish executor. Concurrent retries cannot create
// duplicate work because the state change and task insert share a transaction.
func (db *DB) RetryPublishJob(id int64, traceID string) (*PublishJob, *TaskQueueItem, error) {
	if id <= 0 {
		return nil, nil, fmt.Errorf("publish job id must be positive")
	}
	traceID = strings.TrimSpace(traceID)
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var status, mode, submitAttemptedAt, snapshotHash string
	var retryCount, maxRetries int
	var planID, planTargetID int64
	err = tx.QueryRow(`SELECT status, mode, retry_count, max_retries, COALESCE(submit_attempted_at,''),
		COALESCE(plan_id,0), COALESCE(plan_target_id,0), COALESCE(snapshot_hash,'')
		FROM publish_jobs WHERE id=?`, id).
		Scan(&status, &mode, &retryCount, &maxRetries, &submitAttemptedAt, &planID, &planTargetID, &snapshotHash)
	if err == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
	}
	if err != nil {
		return nil, nil, err
	}
	var retryable int
	eventErr := tx.QueryRow(`SELECT retryable FROM job_events WHERE job_id=? ORDER BY id DESC LIMIT 1`, id).Scan(&retryable)
	if status != "failed" || retryCount >= maxRetries || submitAttemptedAt != "" || eventErr == sql.ErrNoRows || retryable != 1 {
		return nil, nil, fmt.Errorf("%w: job is not retryable", ErrPublishJobConflict)
	}
	if eventErr != nil {
		return nil, nil, eventErr
	}
	if mode != "dry_run" {
		var snapshotJSON string
		if planID <= 0 || planTargetID <= 0 || !validSnapshotHash(snapshotHash) {
			return nil, nil, fmt.Errorf("%w: retry requires a frozen publish snapshot", ErrPublishJobConflict)
		}
		linkErr := tx.QueryRow(`SELECT plan.snapshot_json FROM publish_plan_targets AS target
			JOIN publish_plans AS plan ON plan.id=target.plan_id
			WHERE plan.id=? AND target.id=? AND target.publish_job_id=?
			  AND plan.snapshot_hash=?`, planID, planTargetID, id, snapshotHash).Scan(&snapshotJSON)
		if linkErr != nil {
			return nil, nil, fmt.Errorf("%w: retry frozen snapshot link is unavailable", ErrPublishJobConflict)
		}
		digest := sha256.Sum256([]byte(snapshotJSON))
		if fmt.Sprintf("%x", digest[:]) != snapshotHash {
			return nil, nil, fmt.Errorf("%w: retry frozen snapshot integrity check failed", ErrPublishJobConflict)
		}
	}
	res, err := tx.Exec(`UPDATE publish_jobs SET status='pending', current_step='queued', retry_count=retry_count+1, error_code='', error_message='', trace_id=CASE WHEN ?<>'' THEN ? ELSE trace_id END, cancel_requested_at=NULL, started_at=NULL, finished_at=NULL, updated_at=? WHERE id=? AND status='failed' AND retry_count=?`, traceID, traceID, now, id, retryCount)
	if err != nil {
		return nil, nil, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return nil, nil, err
	}
	if changed != 1 {
		return nil, nil, fmt.Errorf("%w: retry race lost", ErrPublishJobConflict)
	}
	payload, err := json.Marshal(map[string]interface{}{
		"existing_job_id": id,
		"dry_run":         mode == "dry_run",
		"auto_submit":     mode == "submit" || mode == "auto_submit",
	})
	if err != nil {
		return nil, nil, err
	}
	taskRes, err := tx.Exec(`INSERT INTO task_queue (kind, payload_json, status, priority, run_after, attempts, last_error, trace_id, created_at, updated_at) VALUES ('publish', ?, 'pending', 10, '', 0, '', ?, ?, ?)`, string(payload), traceID, now, now)
	if err != nil {
		return nil, nil, err
	}
	taskID, err := taskRes.LastInsertId()
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(`INSERT INTO job_events (job_id, level, step, event_type, message, trace_id, retryable, schema_version, created_at) VALUES (?, 'info', 'queued', 'job.retry_queued', 'publish retry queued', ?, 0, 1, ?)`, id, traceID, now); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	job, err := db.GetPublishJob(id)
	if err != nil {
		return nil, nil, err
	}
	task := &TaskQueueItem{ID: taskID, Kind: "publish", PayloadJSON: string(payload), Status: "pending", Priority: 10, TraceID: traceID, CreatedAt: now, UpdatedAt: now}
	return job, task, nil
}

// PreparePublishJobResume atomically reopens one manual-takeover or retryable
// failed job for immediate CLI execution. A concurrent resume can never move a
// running job back to pending.
func (db *DB) PreparePublishJobResume(id int64, traceID string) (*PublishJob, error) {
	if id <= 0 {
		return nil, fmt.Errorf("publish job id must be positive")
	}
	traceID = strings.TrimSpace(traceID)
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE publish_jobs SET
		status='pending', current_step='preflight', retry_count=retry_count+1,
		error_code='', error_message='', cancel_requested_at=NULL,
		trace_id=CASE WHEN ?<>'' THEN ? ELSE trace_id END,
		started_at=NULL, finished_at=NULL, updated_at=?
		WHERE id=? AND retry_count<max_retries AND (
			status='manual_takeover' OR (
				status='failed' AND COALESCE((
					SELECT retryable FROM job_events WHERE job_id=publish_jobs.id ORDER BY id DESC LIMIT 1
				), 0)=1
			)
		) AND COALESCE(submit_attempted_at,'')=''
		AND (
			mode='dry_run' OR (
				COALESCE(plan_id,0)>0 AND COALESCE(plan_target_id,0)>0
				AND length(COALESCE(snapshot_hash,''))=64
				AND EXISTS (
					SELECT 1 FROM publish_plan_targets AS target
					JOIN publish_plans AS plan ON plan.id=target.plan_id
					WHERE plan.id=publish_jobs.plan_id
					  AND target.id=publish_jobs.plan_target_id
					  AND target.publish_job_id=publish_jobs.id
					  AND plan.snapshot_hash=publish_jobs.snapshot_hash
				)
			)
		)`, traceID, traceID, now, id)
	if err != nil {
		return nil, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		var exists int
		if lookupErr := tx.QueryRow(`SELECT 1 FROM publish_jobs WHERE id=?`, id).Scan(&exists); lookupErr == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
		}
		return nil, fmt.Errorf("%w: publish job is not resumable", ErrPublishJobConflict)
	}
	if _, err := tx.Exec(`INSERT INTO job_events
		(job_id, level, step, event_type, message, trace_id, retryable, schema_version, created_at)
		VALUES (?, 'info', 'resume', 'job.resume_requested', 'publish resume requested', ?, 0, 1, ?)`,
		id, traceID, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db.GetPublishJob(id)
}

func (db *DB) ListPublishJobs(limit int) ([]PublishJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`SELECT `+publishJobSelectColumns+` FROM publish_jobs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []PublishJob
	for rows.Next() {
		var j PublishJob
		if err := rows.Scan(&j.ID, &j.BatchID, &j.PlanID, &j.PlanTargetID,
			&j.ContentID, &j.AccountID, &j.AssetID, &j.PlatformCode, &j.ContentType, &j.Mode, &j.Status,
			&j.CurrentStep, &j.SettingsJSON, &j.RetryCount, &j.MaxRetries, &j.ErrorCode, &j.ErrorMessage,
			&j.TraceID, &j.IdempotencyKey, &j.CancelRequestedAt, &j.SnapshotHash,
			&j.SubmitAttemptedAt, &j.SubmitOutcome, &j.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (db *DB) UpdatePublishJob(id int64, status, currentStep, errorCode, errorMessage string, retryCount int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	query := `UPDATE publish_jobs SET status = ?, current_step = ?, error_code = ?, error_message = ?`
	args := []interface{}{status, currentStep, errorCode, errorMessage}
	if retryCount > 0 {
		query += `, retry_count = ?`
		args = append(args, retryCount)
	}
	switch status {
	case "running":
		query += `, started_at = COALESCE(started_at, ?)`
		args = append(args, now)
	case "success", "failed", "manual_takeover", "awaiting_review", "published",
		"published_verified", "scheduled_verified", "result_unknown", "login_required", "cancelled":
		query += `, finished_at = COALESCE(finished_at, ?)`
		args = append(args, now)
	}
	query += `, updated_at = ? WHERE id = ?`
	args = append(args, now, id)
	res, err := db.Exec(query, args...)
	if err != nil {
		return err
	}
	updated, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("publish job rows affected: %w", err)
	}
	if updated != 1 {
		return fmt.Errorf("publish job %d not found", id)
	}
	return nil
}

// FinalizePublishJob writes one adapter outcome while atomically giving an
// already-persisted cancellation marker precedence over that outcome.
func (db *DB) FinalizePublishJob(id int64, status, currentStep, errorCode, errorMessage string, retryCount int) (bool, error) {
	if id <= 0 {
		return false, fmt.Errorf("publish job id must be positive")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	finish := 0
	switch status {
	case "success", "failed", "manual_takeover", "awaiting_review", "published",
		"published_verified", "scheduled_verified", "result_unknown", "login_required", "cancelled":
		finish = 1
	}
	res, err := db.Exec(`UPDATE publish_jobs SET
		status=CASE WHEN cancel_requested_at IS NOT NULL AND cancel_requested_at<>'' THEN 'cancelled' ELSE ? END,
		current_step=CASE WHEN cancel_requested_at IS NOT NULL AND cancel_requested_at<>'' THEN 'cancelled' ELSE ? END,
		error_code=CASE WHEN cancel_requested_at IS NOT NULL AND cancel_requested_at<>'' THEN '' ELSE ? END,
		error_message=CASE WHEN cancel_requested_at IS NOT NULL AND cancel_requested_at<>'' THEN '' ELSE ? END,
		retry_count=CASE WHEN ?>0 THEN ? ELSE retry_count END,
		finished_at=CASE WHEN (cancel_requested_at IS NOT NULL AND cancel_requested_at<>'') OR ?=1 THEN COALESCE(finished_at, ?) ELSE finished_at END,
		updated_at=?
		WHERE id=? AND status='running'`, status, currentStep, errorCode, errorMessage, retryCount, retryCount, finish, now, now, id)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed != 1 {
		var exists int
		if lookupErr := db.QueryRow(`SELECT 1 FROM publish_jobs WHERE id=?`, id).Scan(&exists); lookupErr == sql.ErrNoRows {
			return false, fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
		}
		return false, fmt.Errorf("%w: publish job is not running", ErrPublishJobConflict)
	}
	var finalStatus string
	if err := db.QueryRow(`SELECT status FROM publish_jobs WHERE id=?`, id).Scan(&finalStatus); err != nil {
		return false, err
	}
	return finalStatus == "cancelled", nil
}

// SetPublishJobSettings persists the safe template snapshot used for a job.
// It is intentionally separate from state transitions so a job's audit input
// cannot be lost when later status writes occur.
func (db *DB) SetPublishJobSettings(id int64, settingsJSON string) error {
	if id <= 0 {
		return fmt.Errorf("publish job id must be positive")
	}
	if len(settingsJSON) > 64<<10 {
		return fmt.Errorf("publish job settings exceed 64 KiB")
	}
	var value any
	if err := json.Unmarshal([]byte(settingsJSON), &value); err != nil {
		return fmt.Errorf("publish job settings must be JSON")
	}
	res, err := db.Exec(`UPDATE publish_jobs SET settings_json=?, updated_at=?
		WHERE id=? AND status='pending' AND (settings_json IS NULL OR settings_json='')`,
		settingsJSON, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		return nil
	}
	var status, existing string
	err = db.QueryRow(`SELECT status, COALESCE(settings_json,'') FROM publish_jobs WHERE id=?`, id).Scan(&status, &existing)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
	}
	if err != nil {
		return err
	}
	if existing == settingsJSON {
		return nil
	}
	return fmt.Errorf("%w: publish job settings snapshot is immutable (status=%s)", ErrPublishJobConflict, status)
}

// ValidatePublishJobMode checks the intent already bound at creation time.
// Runner never mutates this value, so retries and idempotent replays cannot
// upgrade a rehearsal or review-only job into automatic submit.
func (db *DB) ValidatePublishJobMode(id int64, mode string) error {
	if id <= 0 {
		return fmt.Errorf("publish job id must be positive")
	}
	if !validPublishJobMode(mode) {
		return fmt.Errorf("invalid publish job mode %q", mode)
	}
	var existing string
	err := db.QueryRow(`SELECT mode FROM publish_jobs WHERE id=?`, id).Scan(&existing)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: %d", ErrPublishJobNotFound, id)
	}
	if err != nil {
		return err
	}
	if existing == mode || (mode == "prepare" && (existing == "browser" || existing == "api" || existing == "hybrid")) {
		return nil
	}
	return fmt.Errorf("%w: publish job mode is immutable (stored=%s requested=%s)", ErrPublishJobConflict, existing, mode)
}

const publishExecutorStaleAfter = 2 * time.Minute

// ClaimBrowserRuntimeJob serializes browser work per account profile. A stale
// running owner is recovered only after its per-run heartbeat expires.
func (db *DB) ClaimBrowserRuntimeJob(accountID, jobID int64) (bool, error) {
	if accountID <= 0 || jobID <= 0 {
		return false, fmt.Errorf("account id and publish job id must be positive")
	}
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var profileID string
	var runtimeState string
	var activeJobID sql.NullInt64
	err = tx.QueryRow(`SELECT profile_id, state, active_job_id FROM browser_runtime_instances
		WHERE account_id=? ORDER BY updated_at DESC LIMIT 1`, accountID).Scan(&profileID, &runtimeState, &activeJobID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	switch runtimeState {
	case "stopping", "stopped", "crashed":
		return false, nil
	}
	if activeJobID.Valid && activeJobID.Int64 != jobID {
		var status, updatedAt, cancelRequestedAt, submitAttemptedAt, submitOutcome string
		ownerErr := tx.QueryRow(`SELECT status, COALESCE(updated_at,''), COALESCE(cancel_requested_at,''),
			COALESCE(submit_attempted_at,''), COALESCE(submit_outcome,'') FROM publish_jobs WHERE id=?`, activeJobID.Int64).
			Scan(&status, &updatedAt, &cancelRequestedAt, &submitAttemptedAt, &submitOutcome)
		reclaimable := ownerErr == sql.ErrNoRows || status != "running"
		if ownerErr != nil && ownerErr != sql.ErrNoRows {
			return false, ownerErr
		}
		if ownerErr == nil && status != "running" && submitAttemptedAt != "" && submitOutcome != "verified_published" {
			// A submit result that has not been independently resolved fences
			// the account even after the executor leaves running state.
			return false, nil
		}
		if status == "running" {
			lastSeen, parseErr := time.Parse(time.RFC3339, updatedAt)
			if parseErr != nil || !lastSeen.Before(nowTime.Add(-publishExecutorStaleAfter)) {
				return false, nil
			}
			finalStatus, step, eventType, code, message, retryable := "failed", "executor_recovery", "job.retryable_failed", "PUBLISH_EXECUTOR_LOST", "publish executor heartbeat expired", 1
			unresolvedSubmit := submitAttemptedAt != ""
			if unresolvedSubmit {
				finalStatus, step, eventType, code, message, retryable = "awaiting_review", "creator_backend_verification", "job.publish_outcome_unknown", "PUBLISH_RESULT_UNKNOWN", "submit executor was lost; creator backend verification is required", 0
			}
			if cancelRequestedAt != "" {
				finalStatus, step, eventType, code, message, retryable = "cancelled", "cancelled", "job.cancelled", "", "publish cancelled after executor exit", 0
				unresolvedSubmit = false
			}
			res, updateErr := tx.Exec(`UPDATE publish_jobs SET status=?, current_step=?, error_code=?, error_message=?,
				submit_outcome=CASE WHEN ?=1 THEN 'unknown' ELSE submit_outcome END,
				finished_at=COALESCE(finished_at, ?), updated_at=?
				WHERE id=? AND status='running' AND updated_at=?`,
				finalStatus, step, code, message, unresolvedSubmit, now, now, activeJobID.Int64, updatedAt)
			if updateErr != nil {
				return false, updateErr
			}
			changed, rowsErr := res.RowsAffected()
			if rowsErr != nil {
				return false, rowsErr
			}
			if changed != 1 {
				return false, nil
			}
			if _, eventErr := tx.Exec(`INSERT INTO job_events
				(job_id, level, step, event_type, message, error_code, error_class, retryable, schema_version, created_at)
				VALUES (?, 'error', ?, ?, ?, ?, 'runtime', ?, 1, ?)`,
				activeJobID.Int64, step, eventType, message, code, retryable, now); eventErr != nil {
				return false, eventErr
			}
			if unresolvedSubmit {
				if err := tx.Commit(); err != nil {
					return false, err
				}
				return false, nil
			}
			reclaimable = true
		}
		if !reclaimable {
			return false, nil
		}
	}
	res, err := tx.Exec(`UPDATE browser_runtime_instances SET active_job_id=?, updated_at=?
		WHERE profile_id=? AND state NOT IN ('stopping','stopped','crashed')
		AND (active_job_id IS NULL OR active_job_id=?)`, jobID, now, profileID, activeJobID)
	if err != nil {
		return false, fmt.Errorf("claim browser runtime job: %w", err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim browser runtime job rows affected: %w", err)
	}
	if changed != 1 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// VerifyBrowserRuntimeJob confirms the account runtime is still owned by this
// execution. Runner uses it with the existing heartbeat to fail closed if a
// runtime row is lost or replaced.
func (db *DB) VerifyBrowserRuntimeJob(accountID, jobID int64) (bool, error) {
	if accountID <= 0 || jobID <= 0 {
		return false, fmt.Errorf("account id and publish job id must be positive")
	}
	var owned int
	err := db.QueryRow(`SELECT 1 FROM browser_runtime_instances
		WHERE account_id=? AND active_job_id=?
		AND state NOT IN ('stopping','stopped','crashed')
		LIMIT 1`, accountID, jobID).Scan(&owned)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return owned == 1, err
}

// TouchPublishJob is the bounded Runner heartbeat used for crash recovery.
func (db *DB) TouchPublishJob(id int64) (bool, error) {
	if id <= 0 {
		return false, fmt.Errorf("publish job id must be positive")
	}
	res, err := db.Exec(`UPDATE publish_jobs SET updated_at=? WHERE id=? AND status='running'`,
		time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

// ReleaseBrowserRuntimeJob clears only the matching owner, so a late defer
// cannot release a newer job that reclaimed a terminal marker.
func (db *DB) ReleaseBrowserRuntimeJob(accountID, jobID int64) (bool, error) {
	if accountID <= 0 || jobID <= 0 {
		return false, fmt.Errorf("account id and publish job id must be positive")
	}
	res, err := db.Exec(`UPDATE browser_runtime_instances
		SET active_job_id=NULL, updated_at=?
		WHERE account_id=? AND active_job_id=?`,
		time.Now().UTC().Format(time.RFC3339), accountID, jobID)
	if err != nil {
		return false, fmt.Errorf("release browser runtime job: %w", err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("release browser runtime job rows affected: %w", err)
	}
	return changed == 1, nil
}

// ClaimPublishJob atomically acquires one pending job for a single executor.
// A failed job must be explicitly reset to pending by the retry workflow before
// it can be claimed again; terminal jobs are never reopened here.
func (db *DB) ClaimPublishJob(id int64, traceID string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE publish_jobs
		SET status='running', current_step='claimed',
			trace_id=CASE WHEN ? <> '' THEN ? ELSE trace_id END,
			started_at=COALESCE(started_at, ?), updated_at=?
		WHERE id=? AND status='pending'`,
		traceID, traceID, now, now, id)
	if err != nil {
		return false, fmt.Errorf("claim publish job: %w", err)
	}
	claimed, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim publish job rows affected: %w", err)
	}
	return claimed == 1, nil
}

// --- JobEvent ---

type JobEvent struct {
	ID              int64  `json:"id"`
	JobID           int64  `json:"job_id"`
	Level           string `json:"level"`
	Step            string `json:"step,omitempty"`
	EventType       string `json:"event_type"`
	Message         string `json:"message,omitempty"`
	ScreenshotPath  string `json:"screenshot_path,omitempty"`
	DOMSnapshotPath string `json:"dom_snapshot_path,omitempty"`
	RawJSON         string `json:"-"`
	TraceID         string `json:"trace_id,omitempty"`
	RequestID       string `json:"request_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorClass      string `json:"error_class,omitempty"`
	Retryable       bool   `json:"retryable"`
	SchemaVersion   int    `json:"schema_version"`
	CreatedAt       string `json:"created_at"`
}

type JobEventInput struct {
	JobID           int64
	Level           string
	Step            string
	Type            string
	Message         string
	ScreenshotPath  string
	DOMSnapshotPath string
	RawJSON         string
	TraceID         string
	RequestID       string
	TaskID          string
	DurationMS      int64
	ErrorCode       string
	ErrorClass      string
	Retryable       bool
}

func (db *DB) CreateJobEvent(jobID int64, level, step, eventType, message, screenshotPath, domSnapshotPath string) (*JobEvent, error) {
	return db.CreateJobEventDetailed(JobEventInput{
		JobID:           jobID,
		Level:           level,
		Step:            step,
		Type:            eventType,
		Message:         message,
		ScreenshotPath:  screenshotPath,
		DOMSnapshotPath: domSnapshotPath,
	})
}

func (db *DB) CreateJobEventDetailed(in JobEventInput) (*JobEvent, error) {
	if in.JobID <= 0 {
		return nil, fmt.Errorf("job event requires a positive job id")
	}
	if in.Type == "" {
		return nil, fmt.Errorf("job event type is required")
	}
	if in.Level == "" {
		in.Level = "info"
	}
	switch in.Level {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("invalid job event level %q", in.Level)
	}
	if !validEventToken(in.Type) {
		return nil, fmt.Errorf("invalid job event type")
	}
	if in.DurationMS < 0 {
		return nil, fmt.Errorf("job event duration must not be negative")
	}
	if in.Retryable && strings.TrimSpace(in.ErrorCode) == "" {
		return nil, fmt.Errorf("retryable job event requires an error code")
	}
	if !validErrorClass(in.ErrorClass) {
		return nil, fmt.Errorf("invalid job event error class %q", in.ErrorClass)
	}
	if len(in.RawJSON) > 64<<10 || (in.RawJSON != "" && !json.Valid([]byte(in.RawJSON))) {
		return nil, fmt.Errorf("job event raw_json must be valid JSON up to 64 KiB")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	retryable := 0
	if in.Retryable {
		retryable = 1
	}
	const schemaVersion = 1
	res, err := db.Exec(`INSERT INTO job_events (job_id, level, step, event_type, message, screenshot_path, dom_snapshot_path, raw_json, trace_id, request_id, task_id, duration_ms, error_code, error_class, retryable, schema_version, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.JobID, in.Level, in.Step, in.Type, in.Message, in.ScreenshotPath, in.DOMSnapshotPath, in.RawJSON, in.TraceID, in.RequestID, in.TaskID, in.DurationMS, in.ErrorCode, in.ErrorClass, retryable, schemaVersion, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("job event id: %w", err)
	}
	return &JobEvent{
		ID:              id,
		JobID:           in.JobID,
		Level:           in.Level,
		Step:            in.Step,
		EventType:       in.Type,
		Message:         in.Message,
		ScreenshotPath:  in.ScreenshotPath,
		DOMSnapshotPath: in.DOMSnapshotPath,
		RawJSON:         in.RawJSON,
		TraceID:         in.TraceID,
		RequestID:       in.RequestID,
		TaskID:          in.TaskID,
		DurationMS:      in.DurationMS,
		ErrorCode:       in.ErrorCode,
		ErrorClass:      in.ErrorClass,
		Retryable:       in.Retryable,
		SchemaVersion:   schemaVersion,
		CreatedAt:       now,
	}, nil
}

func validEventToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func validErrorClass(value string) bool {
	switch value {
	case "", "domain", "platform", "persistence", "timeout", "cancelled", "validation", "authorization", "tool", "internal":
		return true
	default:
		return false
	}
}

func (db *DB) ListAllJobEvents(limit int) ([]JobEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(`SELECT id, job_id, level, COALESCE(step,''), event_type, COALESCE(message,''), COALESCE(screenshot_path,''), COALESCE(dom_snapshot_path,''), COALESCE(raw_json,''), COALESCE(trace_id,''), COALESCE(request_id,''), COALESCE(task_id,''), duration_ms, COALESCE(error_code,''), COALESCE(error_class,''), retryable, schema_version, COALESCE(created_at,'') FROM job_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []JobEvent
	for rows.Next() {
		var e JobEvent
		var retryable int
		if err := rows.Scan(&e.ID, &e.JobID, &e.Level, &e.Step, &e.EventType, &e.Message, &e.ScreenshotPath, &e.DOMSnapshotPath, &e.RawJSON, &e.TraceID, &e.RequestID, &e.TaskID, &e.DurationMS, &e.ErrorCode, &e.ErrorClass, &retryable, &e.SchemaVersion, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Retryable = retryable == 1
		events = append(events, e)
	}
	return events, rows.Err()
}

func (db *DB) ListJobEventsByJob(jobID int64) ([]JobEvent, error) {
	rows, err := db.Query(`SELECT id, job_id, level, COALESCE(step,''), event_type, COALESCE(message,''), COALESCE(screenshot_path,''), COALESCE(dom_snapshot_path,''), COALESCE(raw_json,''), COALESCE(trace_id,''), COALESCE(request_id,''), COALESCE(task_id,''), duration_ms, COALESCE(error_code,''), COALESCE(error_class,''), retryable, schema_version, COALESCE(created_at,'') FROM job_events WHERE job_id = ? ORDER BY id`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []JobEvent
	for rows.Next() {
		var e JobEvent
		var retryable int
		if err := rows.Scan(&e.ID, &e.JobID, &e.Level, &e.Step, &e.EventType, &e.Message, &e.ScreenshotPath, &e.DOMSnapshotPath, &e.RawJSON, &e.TraceID, &e.RequestID, &e.TaskID, &e.DurationMS, &e.ErrorCode, &e.ErrorClass, &retryable, &e.SchemaVersion, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Retryable = retryable == 1
		events = append(events, e)
	}
	return events, rows.Err()
}

// ListJobEventsAfter returns the ordered business-event replay window used by
// SSE clients. RawJSON remains repository-only and is never serialized by the
// HTTP event stream.
func (db *DB) ListJobEventsAfter(afterID int64, limit int) ([]JobEvent, error) {
	if afterID < 0 {
		return nil, fmt.Errorf("job event cursor must not be negative")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.Query(`SELECT id, job_id, level, COALESCE(step,''), event_type, COALESCE(message,''), COALESCE(screenshot_path,''), COALESCE(dom_snapshot_path,''), COALESCE(raw_json,''), COALESCE(trace_id,''), COALESCE(request_id,''), COALESCE(task_id,''), duration_ms, COALESCE(error_code,''), COALESCE(error_class,''), retryable, schema_version, COALESCE(created_at,'') FROM job_events WHERE id > ? ORDER BY id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []JobEvent
	for rows.Next() {
		var event JobEvent
		var retryable int
		if err := rows.Scan(&event.ID, &event.JobID, &event.Level, &event.Step, &event.EventType, &event.Message, &event.ScreenshotPath, &event.DOMSnapshotPath, &event.RawJSON, &event.TraceID, &event.RequestID, &event.TaskID, &event.DurationMS, &event.ErrorCode, &event.ErrorClass, &retryable, &event.SchemaVersion, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.Retryable = retryable == 1
		events = append(events, event)
	}
	return events, rows.Err()
}

// OperationEvent is the safe, user-visible timeline produced by the Tool
// Registry. It intentionally omits tool arguments and browser/CDP payloads.
type OperationEvent struct {
	ID           int64  `json:"id"`
	TraceID      string `json:"trace_id"`
	RequestID    string `json:"request_id,omitempty"`
	TaskID       string `json:"task_id,omitempty"`
	AccountID    int64  `json:"account_id,omitempty"`
	PlatformCode string `json:"platform_code,omitempty"`
	Component    string `json:"component"`
	EventType    string `json:"event_type"`
	Step         string `json:"step,omitempty"`
	Status       string `json:"status,omitempty"`
	Level        string `json:"level"`
	ErrorCode    string `json:"error_code,omitempty"`
	DurationMS   int64  `json:"duration_ms"`
	CreatedAt    string `json:"created_at"`
}

type OperationEventInput struct {
	TraceID, RequestID, TaskID, PlatformCode, Component, EventType, Step, Status, Level, ErrorCode string
	AccountID, DurationMS                                                                          int64
}

func (db *DB) CreateOperationEvent(in OperationEventInput) (*OperationEvent, error) {
	if !validOperationEventToken(in.TraceID) || !validOperationEventToken(in.Component) || !validOperationEventToken(in.EventType) {
		return nil, fmt.Errorf("operation event requires safe trace, component, and type")
	}
	for _, value := range []string{in.RequestID, in.TaskID, in.PlatformCode, in.Step, in.Status, in.ErrorCode} {
		if value != "" && !validOperationEventToken(value) {
			return nil, fmt.Errorf("invalid operation event token")
		}
	}
	if in.AccountID < 0 || in.DurationMS < 0 {
		return nil, fmt.Errorf("operation event identifiers must be non-negative")
	}
	if in.Level == "" {
		in.Level = "info"
	}
	switch in.Level {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("invalid operation event level")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO operation_events (trace_id, request_id, task_id, account_id, platform_code, component, event_type, step, status, level, error_code, duration_ms, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.TraceID, in.RequestID, in.TaskID, in.AccountID, in.PlatformCode, in.Component, in.EventType, in.Step, in.Status, in.Level, in.ErrorCode, in.DurationMS, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("operation event id: %w", err)
	}
	return &OperationEvent{ID: id, TraceID: in.TraceID, RequestID: in.RequestID, TaskID: in.TaskID, AccountID: in.AccountID, PlatformCode: in.PlatformCode, Component: in.Component, EventType: in.EventType, Step: in.Step, Status: in.Status, Level: in.Level, ErrorCode: in.ErrorCode, DurationMS: in.DurationMS, CreatedAt: now}, nil
}

func (db *DB) ListOperationEvents(limit int) ([]OperationEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := db.Query(`SELECT id, trace_id, COALESCE(request_id,''), COALESCE(task_id,''), COALESCE(account_id,0), COALESCE(platform_code,''), component, event_type, COALESCE(step,''), COALESCE(status,''), level, COALESCE(error_code,''), duration_ms, created_at FROM operation_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]OperationEvent, 0)
	for rows.Next() {
		var event OperationEvent
		if err := rows.Scan(&event.ID, &event.TraceID, &event.RequestID, &event.TaskID, &event.AccountID, &event.PlatformCode, &event.Component, &event.EventType, &event.Step, &event.Status, &event.Level, &event.ErrorCode, &event.DurationMS, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func validOperationEventToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 160 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// --- PublishRecord ---

type PublishRecord struct {
	ID             int64  `json:"id"`
	JobID          int64  `json:"job_id"`
	ContentID      int64  `json:"content_id"`
	AccountID      int64  `json:"account_id"`
	AssetID        int64  `json:"asset_id"`
	PlatformCode   string `json:"platform_code"`
	ContentType    string `json:"content_type"`
	Status         string `json:"status"`
	Source         string `json:"source"`
	PostURL        string `json:"post_url"`
	ExternalPostID string `json:"external_post_id"`
	PublishData    string `json:"-"`
}

// ManualPublishJob is intentionally narrow: it is the information a user
// needs to attest that a handed-off or reviewed publish completed.
type ManualPublishJob struct {
	ID           int64  `json:"id"`
	ContentID    int64  `json:"content_id"`
	AccountID    int64  `json:"account_id"`
	AssetID      int64  `json:"asset_id"`
	PlatformCode string `json:"platform_code"`
	ContentType  string `json:"content_type"`
	Status       string `json:"status"`
	CurrentStep  string `json:"current_step"`
	CreatedAt    string `json:"created_at"`
}

func (db *DB) CreatePublishRecord(jobID, contentID, accountID, assetID int64, platformCode, contentType, status, postURL, externalPostID string) (*PublishRecord, error) {
	if jobID <= 0 {
		return nil, fmt.Errorf("publish record requires a positive job id")
	}
	if status != "published" {
		return nil, fmt.Errorf("%w: publish records only store independently verified published content", ErrPublishJobConflict)
	}
	var jobStatus, step, outcome string
	if err := db.QueryRow(`SELECT status, COALESCE(current_step,''), COALESCE(submit_outcome,'')
		FROM publish_jobs WHERE id=?`, jobID).Scan(&jobStatus, &step, &outcome); err != nil {
		return nil, err
	}
	if (jobStatus != "published" && jobStatus != "published_verified") ||
		step != "creator_backend_verified" || outcome != "verified_published" {
		return nil, fmt.Errorf("%w: publish record requires independent creator-backend evidence", ErrPublishJobConflict)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO publish_records (job_id, content_id, account_id, asset_id, platform_code, content_type, status, post_url, external_post_id, published_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		jobID, contentID, accountID, nullableID(assetID), platformCode, contentType, status, postURL, externalPostID, now, now, now)
	if err != nil {
		// The migration trigger serializes duplicate job writes, including across
		// processes. Return the already committed record to keep the API idempotent.
		if existing, lookupErr := db.getPublishRecordByJob(jobID); lookupErr == nil {
			return existing, nil
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("publish record id: %w", err)
	}
	return &PublishRecord{ID: id, JobID: jobID, ContentID: contentID, AccountID: accountID, AssetID: assetID, PlatformCode: platformCode, ContentType: contentType, Status: status, PostURL: postURL, ExternalPostID: externalPostID}, nil
}

type CreatorBackendEvidence struct {
	ObservedStatus string
	PostURL        string
	ExternalPostID string
	ScreenshotPath string
}

func normalizePublishedEvidenceStatus(value string) (string, error) {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "published":
		return "Published", nil
	case "public":
		return "Public", nil
	}
	switch value {
	case "已发布", "已发表":
		return value, nil
	default:
		return "", fmt.Errorf("creator backend status must explicitly be Published, Public, 已发布, or 已发表")
	}
}

func (db *DB) validateEvidenceScreenshot(path string) (string, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", fmt.Errorf("creator backend evidence screenshot is required")
	}
	root, err := filepath.Abs(filepath.Join(db.DataDir, "screenshots"))
	if err != nil {
		return "", "", err
	}
	actual, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	if rootErr == nil {
		root = resolvedRoot
	}
	resolvedActual, err := filepath.EvalSymlinks(actual)
	if err != nil {
		return "", "", fmt.Errorf("creator backend evidence screenshot is unavailable")
	}
	rel, err := filepath.Rel(root, resolvedActual)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("creator backend evidence screenshot must be stored in the private screenshots directory")
	}
	info, err := os.Stat(resolvedActual)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return "", "", fmt.Errorf("creator backend evidence screenshot must be a non-empty regular file")
	}
	file, err := os.Open(resolvedActual)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", "", err
	}
	return resolvedActual, fmt.Sprintf("%x", digest.Sum(nil)), nil
}

func hasDurableXHSSubmitClickEvidence(tx *sql.Tx, jobID int64, submitAttemptedAt string) (bool, error) {
	attemptedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(submitAttemptedAt))
	if err != nil {
		attemptedAt, err = time.Parse(time.RFC3339, strings.TrimSpace(submitAttemptedAt))
	}
	if err != nil {
		return false, fmt.Errorf("invalid XHS submit attempt time")
	}
	rows, err := tx.Query(`SELECT COALESCE(raw_json,'') FROM job_events WHERE job_id=? ORDER BY id DESC`, jobID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var rawJSON string
		if err := rows.Scan(&rawJSON); err != nil {
			return false, err
		}
		if strings.TrimSpace(rawJSON) == "" {
			continue
		}
		var raw map[string]json.RawMessage
		if len(rawJSON) > 64<<10 || json.Unmarshal([]byte(rawJSON), &raw) != nil {
			return false, fmt.Errorf("invalid XHS submit click evidence")
		}
		if _, present := raw["submitClicked"]; !present {
			continue
		}
		var evidence struct {
			SubmitAttempted bool   `json:"submitAttempted"`
			SubmitClicked   bool   `json:"submitClicked"`
			SubmitClickedAt string `json:"submitClickedAt"`
			SubmitTargetID  string `json:"submitTargetId"`
		}
		if json.Unmarshal([]byte(rawJSON), &evidence) != nil ||
			!evidence.SubmitAttempted || !evidence.SubmitClicked ||
			!validBrowserTargetID(evidence.SubmitTargetID) {
			return false, nil
		}
		clickedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(evidence.SubmitClickedAt))
		if err != nil {
			clickedAt, err = time.Parse(time.RFC3339, strings.TrimSpace(evidence.SubmitClickedAt))
		}
		if err != nil || clickedAt.Before(attemptedAt) || clickedAt.After(attemptedAt.Add(5*time.Minute)) {
			return false, nil
		}
		return true, nil
	}
	return false, rows.Err()
}

func validBrowserTargetID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 32 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

// updatePublishPlanAfterCreatorBackendVerification summarizes only verified
// targets. A result_unknown target remains unresolved and therefore cannot
// cause its plan to look complete or become eligible for a new final click.
func updatePublishPlanAfterCreatorBackendVerification(tx *sql.Tx, planID int64, now string) error {
	if planID <= 0 {
		return nil
	}
	var remaining, queued, active, published, publishedVerified, scheduledVerified int
	if err := tx.QueryRow(`SELECT
			COUNT(*) FILTER (WHERE status NOT IN ('published','published_verified','scheduled_verified')),
			COUNT(*) FILTER (WHERE status='queued'),
			COUNT(*) FILTER (WHERE status IN ('awaiting_review','manual_takeover','submitting','submitted_pending_verification','result_unknown')),
			COUNT(*) FILTER (WHERE status='published'),
			COUNT(*) FILTER (WHERE status='published_verified'),
			COUNT(*) FILTER (WHERE status='scheduled_verified')
		FROM publish_plan_targets WHERE plan_id=?`, planID).
		Scan(&remaining, &queued, &active, &published, &publishedVerified, &scheduledVerified); err != nil {
		return err
	}
	if remaining == 0 {
		status := "published"
		switch {
		case publishedVerified > 0:
			status = "published_verified"
		case scheduledVerified > 0 && published == 0:
			status = "scheduled_verified"
		}
		_, err := tx.Exec(`UPDATE publish_plans SET status=?, updated_at=? WHERE id=?`, status, now, planID)
		return err
	}
	if queued > 0 && active == 0 {
		_, err := tx.Exec(`UPDATE publish_plans SET status='partial_published', updated_at=? WHERE id=?`, now, planID)
		return err
	}
	return nil
}

// RecordCreatorBackendScheduled is the authoritative creator-manager
// transition for a platform-native schedule. It is deliberately separate
// from RecordScheduledPublishContent: a content card alone does not prove a
// post-click outcome. The frozen submit marker, private screenshot, and a
// post-click verification state must all exist in one transaction.
func (db *DB) RecordCreatorBackendScheduled(jobID int64, scheduledAt, evidencePath string) (*PublishContentRecord, error) {
	if jobID <= 0 {
		return nil, fmt.Errorf("publish job id is required")
	}
	scheduledAt = strings.TrimSpace(scheduledAt)
	if _, err := time.Parse(time.RFC3339, scheduledAt); err != nil {
		return nil, fmt.Errorf("scheduled_at must be RFC3339")
	}
	validatedEvidencePath, evidenceSHA256, err := db.validateEvidenceScreenshot(evidencePath)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	job, err := getPublishJobWith(tx, jobID)
	if err != nil {
		return nil, err
	}
	if job.SubmitAttemptedAt == "" || job.PlanID <= 0 || job.PlanTargetID <= 0 {
		return nil, fmt.Errorf("%w: scheduled verification requires one frozen submit attempt", ErrPublishJobConflict)
	}
	if job.Status == "scheduled_verified" {
		record, recordErr := scanPublishContentRecord(tx.QueryRow(publishContentRecordSelect+` WHERE job_id=?`, jobID))
		if recordErr == nil && record.RemoteState == "scheduled" && record.ScheduledAt == scheduledAt && record.EvidencePath == validatedEvidencePath {
			return record, nil
		}
		return nil, fmt.Errorf("%w: schedule verification is already resolved", ErrPublishJobConflict)
	}
	if job.Status != "submitted_pending_verification" && job.Status != "result_unknown" {
		return nil, fmt.Errorf("%w: publish job %d is %q, not awaiting creator-backend verification", ErrPublishJobConflict, jobID, job.Status)
	}
	var publishedRecords int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM publish_records WHERE job_id=? AND status='published'`, jobID).Scan(&publishedRecords); err != nil {
		return nil, err
	}
	if publishedRecords != 0 {
		return nil, fmt.Errorf("%w: published content cannot be resolved as scheduled", ErrPublishJobConflict)
	}
	result, err := tx.Exec(`UPDATE publish_jobs
		SET status='scheduled_verified', current_step='creator_backend_scheduled_verified',
			error_code='', error_message='', submit_outcome='scheduled_verified',
			finished_at=COALESCE(finished_at,?), updated_at=?
		WHERE id=? AND status IN ('submitted_pending_verification','result_unknown')
		  AND COALESCE(submit_attempted_at,'')<>''`, now, now, jobID)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, fmt.Errorf("%w: creator-backend schedule verification race lost", ErrPublishJobConflict)
	}
	record, err := recordScheduledPublishContentWithTx(tx, *job, scheduledAt, validatedEvidencePath, now)
	if err != nil {
		return nil, err
	}
	targetResult, err := tx.Exec(`UPDATE publish_plan_targets
		SET status='scheduled_verified', error_code='', updated_at=?
		WHERE id=? AND plan_id=? AND publish_job_id=?
		  AND status IN ('submitted_pending_verification','result_unknown')`,
		now, job.PlanTargetID, job.PlanID, job.ID)
	if err != nil {
		return nil, err
	}
	targetChanged, err := targetResult.RowsAffected()
	if err != nil {
		return nil, err
	}
	if targetChanged != 1 {
		return nil, fmt.Errorf("%w: creator-backend schedule target verification race lost", ErrPublishJobConflict)
	}
	metadata, err := json.Marshal(map[string]string{
		"source":            "creator_backend",
		"scheduled_at":      scheduledAt,
		"screenshot_sha256": evidenceSHA256,
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO job_events
		(job_id, level, step, event_type, message, screenshot_path, raw_json,
		 retryable, schema_version, created_at)
		VALUES (?, 'info', 'creator_backend_scheduled_verified',
		 'job.creator_backend_scheduled_verified',
		 'creator backend shows the native schedule', ?, ?, 0, 1, ?)`,
		jobID, validatedEvidencePath, string(metadata), now); err != nil {
		return nil, err
	}
	if err := updatePublishPlanAfterCreatorBackendVerification(tx, job.PlanID, now); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE browser_runtime_instances
		SET active_job_id=NULL, updated_at=? WHERE active_job_id=?`, now, jobID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return record, nil
}

// RecordCreatorBackendPublished is the only authoritative live-publish
// transition. It requires a backend-visible published state, stable content
// identity, public link, and a saved screenshot. Upload completion, a toast,
// processing, checks, or a draft can never satisfy this method.
func (db *DB) RecordCreatorBackendPublished(jobID int64, evidence CreatorBackendEvidence) (*PublishRecord, error) {
	if jobID <= 0 {
		return nil, fmt.Errorf("publish job id is required")
	}
	observedStatus, err := normalizePublishedEvidenceStatus(evidence.ObservedStatus)
	if err != nil {
		return nil, err
	}
	postURL, err := validatePublicPostURL(evidence.PostURL)
	if err != nil {
		return nil, err
	}
	externalPostID := strings.TrimSpace(evidence.ExternalPostID)
	if postURL == "" || externalPostID == "" {
		return nil, fmt.Errorf("creator backend evidence requires both public post URL and external post id")
	}
	if len([]rune(externalPostID)) > 256 || strings.ContainsAny(externalPostID, "\r\n") {
		return nil, fmt.Errorf("external post id must be at most 256 characters without line breaks")
	}
	screenshotPath, screenshotSHA256, err := db.validateEvidenceScreenshot(evidence.ScreenshotPath)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var job PublishJob
	err = tx.QueryRow(`SELECT id, COALESCE(plan_id,0), COALESCE(plan_target_id,0), content_id, account_id,
			COALESCE(asset_id,0), platform_code, content_type, status, COALESCE(submit_attempted_at,''), COALESCE(trace_id,'')
		FROM publish_jobs WHERE id=?`, jobID).
		Scan(&job.ID, &job.PlanID, &job.PlanTargetID, &job.ContentID, &job.AccountID,
			&job.AssetID, &job.PlatformCode, &job.ContentType, &job.Status, &job.SubmitAttemptedAt, &job.TraceID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: %d", ErrPublishJobNotFound, jobID)
	}
	if err != nil {
		return nil, err
	}
	if job.SubmitAttemptedAt == "" {
		return nil, fmt.Errorf("%w: no submit attempt is bound to this job", ErrPublishJobConflict)
	}
	if job.PlatformCode == "xhs" {
		clicked, clickErr := hasDurableXHSSubmitClickEvidence(tx, job.ID, job.SubmitAttemptedAt)
		if clickErr != nil {
			return nil, clickErr
		}
		if !clicked {
			return nil, fmt.Errorf("%w: XHS publish requires durable actual-click evidence", ErrPublishJobConflict)
		}
	}
	verifiedJobStatus := "published"
	verifiedTargetStatus := "published"
	if job.Status == "submitted_pending_verification" || job.Status == "result_unknown" {
		verifiedJobStatus = "published_verified"
		verifiedTargetStatus = "published_verified"
	}
	switch job.Status {
	case "awaiting_review", "manual_takeover", "failed", "submitted_pending_verification", "result_unknown":
	default:
		if job.Status == "published" || job.Status == "published_verified" {
			existing, lookupErr := getPublishRecordByJobWith(tx, jobID)
			if lookupErr == nil && existing.PostURL == postURL && existing.ExternalPostID == externalPostID {
				return existing, nil
			}
		}
		return nil, fmt.Errorf("%w: publish job %d is %q, not awaiting creator-backend verification", ErrPublishJobConflict, jobID, job.Status)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.Exec(`UPDATE publish_jobs SET status=?,
		current_step='creator_backend_verified', error_code='', error_message='',
		submit_outcome='verified_published', finished_at=COALESCE(finished_at,?), updated_at=?
		WHERE id=? AND status IN ('awaiting_review','manual_takeover','failed','submitted_pending_verification','result_unknown')
		  AND COALESCE(submit_attempted_at,'')<>''`, verifiedJobStatus, now, now, jobID)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, fmt.Errorf("%w: creator-backend verification race lost", ErrPublishJobConflict)
	}
	publishMetadata, err := json.Marshal(map[string]string{
		"source":            "creator_backend",
		"observed_status":   observedStatus,
		"screenshot_sha256": screenshotSHA256,
	})
	if err != nil {
		return nil, err
	}
	recordResult, err := tx.Exec(`INSERT INTO publish_records
		(job_id, content_id, account_id, asset_id, platform_code, content_type, status,
		 post_url, external_post_id, publish_data, published_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'published', ?, ?, ?, ?, ?, ?)`,
		job.ID, job.ContentID, job.AccountID, nullableID(job.AssetID), job.PlatformCode, job.ContentType,
		postURL, externalPostID, string(publishMetadata), now, now, now)
	if err != nil {
		return nil, err
	}
	recordID, err := recordResult.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := upsertPublishedPublishContentRecord(
		tx, job, postURL, externalPostID, screenshotPath, now,
	); err != nil {
		return nil, err
	}
	eventMetadata, err := json.Marshal(map[string]string{
		"source":            "creator_backend",
		"observed_status":   observedStatus,
		"screenshot_sha256": screenshotSHA256,
	})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO job_events
		(job_id, level, step, event_type, message, screenshot_path, raw_json, retryable, schema_version, created_at)
		VALUES (?, 'info', 'creator_backend_verified', 'job.creator_backend_verified',
		 'creator backend shows the content as published', ?, ?, 0, 1, ?)`,
		jobID, screenshotPath, string(eventMetadata), now); err != nil {
		return nil, err
	}
	// The global activity drawer needs one unambiguous success event. Registry
	// completion means only that a tool returned; this event exists solely after
	// the creator-backend transaction has stored its publish record.
	traceID := job.TraceID
	if !validOperationEventToken(traceID) {
		traceID = fmt.Sprintf("publish-%d", job.ID)
	}
	if _, err := tx.Exec(`INSERT INTO operation_events
		(trace_id, account_id, platform_code, component, event_type, step, status, level, duration_ms, created_at)
		VALUES (?, ?, ?, 'publish.verify', 'publish.creator_backend_verified', 'published_verified', 'published', 'info', 0, ?)`,
		traceID, job.AccountID, job.PlatformCode, now); err != nil {
		return nil, err
	}
	if job.PlanTargetID > 0 {
		targetResult, err := tx.Exec(`UPDATE publish_plan_targets
			SET status=?, error_code='', updated_at=?
			WHERE id=? AND plan_id=? AND publish_job_id=?`,
			verifiedTargetStatus, now, job.PlanTargetID, job.PlanID, job.ID)
		if err != nil {
			return nil, err
		}
		changed, err := targetResult.RowsAffected()
		if err != nil {
			return nil, err
		}
		if changed != 1 {
			return nil, fmt.Errorf("%w: creator-backend target verification race lost", ErrPublishJobConflict)
		}
		if err := updatePublishPlanAfterCreatorBackendVerification(tx, job.PlanID, now); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(`UPDATE browser_runtime_instances
		SET active_job_id=NULL, updated_at=?
		WHERE active_job_id=?`, now, jobID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &PublishRecord{
		ID: recordID, JobID: job.ID, ContentID: job.ContentID, AccountID: job.AccountID,
		AssetID: job.AssetID, PlatformCode: job.PlatformCode, ContentType: job.ContentType,
		Status: "published", Source: "creator_backend", PostURL: postURL,
		ExternalPostID: externalPostID, PublishData: string(publishMetadata),
	}, nil
}

func (db *DB) ListManualPublishJobs(limit int) ([]ManualPublishJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`SELECT id, content_id, account_id, COALESCE(asset_id,0), platform_code, content_type, status, COALESCE(current_step,''), COALESCE(created_at,'')
		FROM publish_jobs WHERE status IN ('manual_takeover','awaiting_review') ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]ManualPublishJob, 0)
	for rows.Next() {
		var job ManualPublishJob
		if err := rows.Scan(&job.ID, &job.ContentID, &job.AccountID, &job.AssetID, &job.PlatformCode, &job.ContentType, &job.Status, &job.CurrentStep, &job.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// ConfirmManualPublish is retained only as a compatibility guard. A user
// attestation is not independent platform evidence and therefore cannot
// create a publish_record or advance a job to published.
func (db *DB) ConfirmManualPublish(jobID int64, postURL, externalPostID string) (*PublishRecord, error) {
	if jobID <= 0 {
		return nil, fmt.Errorf("publish job id is required")
	}
	var status string
	err := db.QueryRow(`SELECT status FROM publish_jobs WHERE id=?`, jobID).Scan(&status)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: %d", ErrPublishJobNotFound, jobID)
	}
	if err != nil {
		return nil, err
	}
	if status != "manual_takeover" && status != "awaiting_review" {
		return nil, fmt.Errorf("%w: publish job %d is %q, not awaiting verification", ErrPublishJobConflict, jobID, status)
	}
	return nil, fmt.Errorf("%w: user confirmation is non-authoritative; creator-backend evidence is required", ErrPublishJobConflict)
}

func validatePublicPostURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(value) > 2048 {
		return "", fmt.Errorf("post URL must be an http(s) URL without credentials, query, or fragment")
	}
	return parsed.String(), nil
}

func getPublishRecordByJobWith(queryer publishJobQueryRower, jobID int64) (*PublishRecord, error) {
	var existing PublishRecord
	err := queryer.QueryRow(`SELECT id, job_id, content_id, account_id, COALESCE(asset_id,0), platform_code, content_type, status, COALESCE(post_url,''), COALESCE(external_post_id,''), COALESCE(publish_data,'') FROM publish_records WHERE job_id=? ORDER BY id LIMIT 1`, jobID).
		Scan(&existing.ID, &existing.JobID, &existing.ContentID, &existing.AccountID, &existing.AssetID, &existing.PlatformCode, &existing.ContentType, &existing.Status, &existing.PostURL, &existing.ExternalPostID, &existing.PublishData)
	if err != nil {
		return nil, fmt.Errorf("lookup publish record for job %d: %w", jobID, err)
	}
	existing.Source = publishRecordSource(existing.PublishData)
	return &existing, nil
}

func (db *DB) getPublishRecordByJob(jobID int64) (*PublishRecord, error) {
	return getPublishRecordByJobWith(db, jobID)
}

func publishRecordSource(publishData string) string {
	var metadata struct {
		Source string `json:"source"`
	}
	if json.Unmarshal([]byte(publishData), &metadata) != nil {
		return ""
	}
	return metadata.Source
}

func nullableID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (db *DB) ListPublishRecords(limit int) ([]PublishRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`SELECT id, COALESCE(job_id,0), content_id, account_id, COALESCE(asset_id,0), platform_code, content_type, status, COALESCE(post_url,''), COALESCE(external_post_id,''), COALESCE(publish_data,'') FROM publish_records ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []PublishRecord
	for rows.Next() {
		var r PublishRecord
		if err := rows.Scan(&r.ID, &r.JobID, &r.ContentID, &r.AccountID, &r.AssetID, &r.PlatformCode, &r.ContentType, &r.Status, &r.PostURL, &r.ExternalPostID, &r.PublishData); err != nil {
			return nil, err
		}
		r.Source = publishRecordSource(r.PublishData)
		records = append(records, r)
	}
	return records, rows.Err()
}

// --- RecordInfo ---

type RecordInfo struct {
	ID              int64
	PublishRecordID int64
	AccountID       int64
	AssetID         int64
	PlatformCode    string
	Link            string
	ReadNum         int
	CommentNum      int
	ShareNum        int
	CollectNum      int
	LikeNum         int
	Success         bool
	Setting         string
	Reason          string
}

func (db *DB) CreateRecordInfo(publishRecordID, accountID, assetID int64, platformCode, link string, success bool, setting, reason string) (*RecordInfo, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	s := 0
	if success {
		s = 1
	}
	res, err := db.Exec(`INSERT INTO record_infos (publish_record_id, account_id, asset_id, platform_code, link, success, setting, reason, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		publishRecordID, accountID, assetID, platformCode, link, s, setting, reason, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &RecordInfo{ID: id, PublishRecordID: publishRecordID, AccountID: accountID, AssetID: assetID, PlatformCode: platformCode, Link: link, Success: success, Setting: setting, Reason: reason}, nil
}

func (db *DB) ListRecordInfosByRecord(publishRecordID int64) ([]RecordInfo, error) {
	rows, err := db.Query(`SELECT id, publish_record_id, account_id, COALESCE(asset_id,0), platform_code, COALESCE(link,''), COALESCE(read_num,0), COALESCE(comment_num,0), COALESCE(share_num,0), COALESCE(collect_num,0), COALESCE(like_num,0), success, COALESCE(setting,''), COALESCE(reason,'') FROM record_infos WHERE publish_record_id = ? ORDER BY id`, publishRecordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var infos []RecordInfo
	for rows.Next() {
		var ri RecordInfo
		var success int
		if err := rows.Scan(&ri.ID, &ri.PublishRecordID, &ri.AccountID, &ri.AssetID, &ri.PlatformCode, &ri.Link, &ri.ReadNum, &ri.CommentNum, &ri.ShareNum, &ri.CollectNum, &ri.LikeNum, &success, &ri.Setting, &ri.Reason); err != nil {
			return nil, err
		}
		ri.Success = success == 1
		infos = append(infos, ri)
	}
	return infos, rows.Err()
}

// GetBrowserProfileByAccount gets browser profile for a given account
func (db *DB) GetBrowserProfileByAccount(accountID int64) (*BrowserProfile, error) {
	var p BrowserProfile
	err := db.QueryRow(`SELECT id, account_id, profile_dir, browser_type, fingerprint_seed, COALESCE(fingerprint_config,''), lang, accept_lang, timezone, COALESCE(last_cdp_port,0), COALESCE(last_cdp_url,''), COALESCE(proxy_server,''), proxy_id FROM browser_profiles WHERE account_id = ? LIMIT 1`, accountID).
		Scan(&p.ID, &p.AccountID, &p.ProfileDir, &p.BrowserType, &p.FingerprintSeed, &p.FingerprintConfig, &p.Lang, &p.AcceptLang, &p.Timezone, &p.LastCDPPort, &p.LastCDPURL, &p.ProxyServer, &p.ProxyID)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("browser profile for account %d not found", accountID)
	}
	return &p, err
}

// validateProxyServer accepts only a host/port endpoint. Credentials must be
// supplied through a future OS keychain reference, never in SQLite or logs.
func validateProxyServer(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "socks5") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("proxy must be http(s) or socks5 host:port without credentials")
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil || host == "" || port == "" {
		return "", fmt.Errorf("proxy must include host and port")
	}
	return parsed.Scheme + "://" + net.JoinHostPort(host, port), nil
}

func validateProxyEndpointName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return "", fmt.Errorf("proxy name must contain 1-120 characters")
	}
	return name, nil
}

func scanProxyEndpoint(scanner interface{ Scan(dest ...any) error }) (*ProxyEndpoint, error) {
	var endpoint ProxyEndpoint
	var enabled int
	if err := scanner.Scan(&endpoint.ID, &endpoint.Name, &endpoint.Server, &enabled, &endpoint.CreatedAt, &endpoint.UpdatedAt); err != nil {
		return nil, err
	}
	endpoint.Enabled = enabled != 0
	return &endpoint, nil
}

// ListProxyEndpoints returns metadata only. It deliberately performs no
// network health checks; those require explicit user authorization.
func (db *DB) ListProxyEndpoints() ([]ProxyEndpoint, error) {
	rows, err := db.Query(`SELECT id, name, server, enabled, created_at, updated_at FROM proxy_endpoints ORDER BY enabled DESC, name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	endpoints := make([]ProxyEndpoint, 0)
	for rows.Next() {
		endpoint, err := scanProxyEndpoint(rows)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, *endpoint)
	}
	return endpoints, rows.Err()
}

func (db *DB) CreateProxyEndpoint(name, server string, enabled bool) (*ProxyEndpoint, error) {
	name, err := validateProxyEndpointName(name)
	if err != nil {
		return nil, err
	}
	server, err = validateProxyServer(server)
	if err != nil || server == "" {
		if err == nil {
			err = fmt.Errorf("proxy server is required")
		}
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	result, err := db.Exec(`INSERT INTO proxy_endpoints (name, server, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, name, server, enabledInt, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return &ProxyEndpoint{ID: id, Name: name, Server: server, Enabled: enabled, CreatedAt: now, UpdatedAt: now}, nil
}

// UpdateProxyEndpoint synchronizes bound profiles in the same short
// transaction, so a restart cannot observe a stale endpoint.
func (db *DB) UpdateProxyEndpoint(id int64, name, server string, enabled bool) (*ProxyEndpoint, error) {
	if id <= 0 {
		return nil, fmt.Errorf("proxy endpoint id is required")
	}
	name, err := validateProxyEndpointName(name)
	if err != nil {
		return nil, err
	}
	server, err = validateProxyServer(server)
	if err != nil || server == "" {
		if err == nil {
			err = fmt.Errorf("proxy server is required")
		}
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	result, err := tx.Exec(`UPDATE proxy_endpoints SET name = ?, server = ?, enabled = ?, updated_at = ? WHERE id = ?`, name, server, enabledInt, now, id)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		if err == nil {
			err = fmt.Errorf("proxy endpoint %d not found", id)
		}
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE browser_profiles SET proxy_server = ?, is_proxy_custom = 0, updated_at = ? WHERE proxy_id = ?`, server, now, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ProxyEndpoint{ID: id, Name: name, Server: server, Enabled: enabled, UpdatedAt: now}, nil
}

// DeleteProxyEndpoint rejects referenced endpoints to prevent a bound account
// silently falling back to a direct connection.
func (db *DB) DeleteProxyEndpoint(id int64) error {
	result, err := db.Exec(`DELETE FROM proxy_endpoints WHERE id = ? AND NOT EXISTS (SELECT 1 FROM browser_profiles WHERE proxy_id = ?)`, id, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		var referenced bool
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM browser_profiles WHERE proxy_id = ?)`, id).Scan(&referenced); err != nil {
			return err
		}
		if referenced {
			return fmt.Errorf("proxy endpoint %d is still bound to an account", id)
		}
		return fmt.Errorf("proxy endpoint %d not found", id)
	}
	return nil
}

// BindProxyEndpoint makes an enabled pool endpoint authoritative for one
// account. It performs no connectivity check and never starts a browser.
func (db *DB) BindProxyEndpoint(accountID, endpointID int64) error {
	if accountID <= 0 || endpointID < 0 {
		return fmt.Errorf("account and proxy endpoint ids must be valid")
	}
	if endpointID == 0 {
		_, err := db.Exec(`UPDATE browser_profiles SET proxy_id = NULL, proxy_server = '', is_proxy_custom = 0, updated_at = ? WHERE account_id = ?`, time.Now().UTC().Format(time.RFC3339), accountID)
		return err
	}
	var server string
	var enabled int
	if err := db.QueryRow(`SELECT server, enabled FROM proxy_endpoints WHERE id = ?`, endpointID).Scan(&server, &enabled); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("proxy endpoint %d not found", endpointID)
		}
		return err
	}
	if enabled == 0 {
		return fmt.Errorf("proxy endpoint %d is disabled", endpointID)
	}
	result, err := db.Exec(`UPDATE browser_profiles SET proxy_id = ?, proxy_server = ?, is_proxy_custom = 0, updated_at = ? WHERE account_id = ?`, endpointID, server, time.Now().UTC().Format(time.RFC3339), accountID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return fmt.Errorf("browser profile for account %d not found", accountID)
	}
	return nil
}

// UpdateBrowserProfileFingerprint updates the fingerprint_seed for an account's browser profile
func (db *DB) UpdateBrowserProfileFingerprint(accountID int64, seed int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE browser_profiles SET fingerprint_seed = ?, updated_at = ? WHERE account_id = ?`, seed, now, accountID)
	return err
}

// UpdateBrowserProfileEngine sets browser_type (fingerprint-chromium|browseros|chromium).
func (db *DB) UpdateBrowserProfileEngine(accountID int64, browserType string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if browserType == "" {
		browserType = "fingerprint-chromium"
	}
	_, err := db.Exec(`UPDATE browser_profiles SET browser_type = ?, updated_at = ? WHERE account_id = ?`, browserType, now, accountID)
	return err
}

// ListBrowserProfiles returns all browser profiles
func (db *DB) ListBrowserProfiles() ([]BrowserProfile, error) {
	rows, err := db.Query(`SELECT id, account_id, profile_dir, browser_type, fingerprint_seed FROM browser_profiles ORDER BY account_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var profiles []BrowserProfile
	for rows.Next() {
		var p BrowserProfile
		if err := rows.Scan(&p.ID, &p.AccountID, &p.ProfileDir, &p.BrowserType, &p.FingerprintSeed); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// --- AgentPush foundation tables ---

type AuditEvent struct {
	ID          int64
	TraceID     string
	Level       string
	Category    string
	Message     string
	RelatedType string
	RelatedID   int64
	RawJSON     string
	CreatedAt   string
}

func (db *DB) CreateAuditEvent(traceID, level, category, message, relatedType string, relatedID int64, rawJSON string) (*AuditEvent, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if level == "" {
		level = "info"
	}
	res, err := db.Exec(`INSERT INTO audit_events (trace_id, level, category, message, related_type, related_id, raw_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		traceID, level, category, message, relatedType, relatedID, rawJSON, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &AuditEvent{ID: id, TraceID: traceID, Level: level, Category: category, Message: message, RelatedType: relatedType, RelatedID: relatedID, RawJSON: rawJSON, CreatedAt: now}, nil
}

func (db *DB) ListAuditEvents(limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(`SELECT id, COALESCE(trace_id,''), COALESCE(level,''), COALESCE(category,''), COALESCE(message,''), COALESCE(related_type,''), COALESCE(related_id,0), COALESCE(raw_json,''), COALESCE(created_at,'') FROM audit_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.TraceID, &e.Level, &e.Category, &e.Message, &e.RelatedType, &e.RelatedID, &e.RawJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type TaskQueueItem struct {
	ID                int64
	Kind              string
	PayloadJSON       string
	Status            string
	Priority          int
	RunAfter          string
	Attempts          int
	LastError         string
	TraceID           string
	LeaseOwner        string
	LeaseExpiresAt    string
	CancelRequestedAt string
	CreatedAt         string
	UpdatedAt         string
}

// ReserveCDPPort atomically reserves one local CDP port until the browser
// runtime row is written. Callers must release it on launch failure; once a
// runtime row exists, ReleaseCDPPort is safe because that row blocks reuse.
func (db *DB) ReserveCDPPort(profileID, owner string, port int, now time.Time, ttl time.Duration) (bool, error) {
	profileID = strings.TrimSpace(profileID)
	owner = strings.TrimSpace(owner)
	if profileID == "" || !validLeaseOwner(owner) || port <= 0 || ttl <= 0 || ttl > 5*time.Minute {
		return false, fmt.Errorf("invalid CDP port reservation")
	}
	now = now.UTC()
	nowText := now.Format(time.RFC3339)
	expiresAt := now.Add(ttl).Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin CDP port reservation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM cdp_port_leases WHERE expires_at <= ?`, nowText); err != nil {
		return false, fmt.Errorf("clean expired CDP port reservations: %w", err)
	}
	result, err := tx.Exec(`INSERT INTO cdp_port_leases (cdp_port, profile_id, owner, acquired_at, expires_at, updated_at)
		SELECT ?, ?, ?, ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM browser_runtime_instances WHERE cdp_port = ?)
		  AND NOT EXISTS (SELECT 1 FROM cdp_port_leases WHERE cdp_port = ? OR profile_id = ?)`,
		port, profileID, owner, nowText, expiresAt, nowText, port, port, profileID)
	if err != nil {
		return false, fmt.Errorf("reserve CDP port: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("reserve CDP port rows affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit CDP port reservation: %w", err)
	}
	return changed == 1, nil
}

// ReleaseCDPPort releases only the matching launch generation, preventing a
// delayed cleanup path from deleting a newer reservation for the same port.
func (db *DB) ReleaseCDPPort(port int, owner string) error {
	owner = strings.TrimSpace(owner)
	if port <= 0 || !validLeaseOwner(owner) {
		return fmt.Errorf("invalid CDP port release")
	}
	if _, err := db.Exec(`DELETE FROM cdp_port_leases WHERE cdp_port = ? AND owner = ?`, port, owner); err != nil {
		return fmt.Errorf("release CDP port: %w", err)
	}
	return nil
}

// AcquireBrowserOperationLease gives one Fleet/agent action exclusive access
// to a profile. It does not replace lifecycle profile_leases; callers acquire
// this first and then perform start/stop/observe work through BrowserFacade.
func (db *DB) AcquireBrowserOperationLease(profileID, operation, owner string, now time.Time, ttl time.Duration) (bool, error) {
	profileID = strings.TrimSpace(profileID)
	operation = strings.TrimSpace(operation)
	owner = strings.TrimSpace(owner)
	if profileID == "" || operation == "" || !validLeaseOwner(owner) || ttl <= 0 || ttl > 10*time.Minute {
		return false, fmt.Errorf("invalid browser operation lease")
	}
	now = now.UTC()
	nowText := now.Format(time.RFC3339)
	expiresAt := now.Add(ttl).Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin browser operation lease: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM browser_operation_leases WHERE expires_at <= ?`, nowText); err != nil {
		return false, fmt.Errorf("clean expired browser operation leases: %w", err)
	}
	result, err := tx.Exec(`INSERT OR IGNORE INTO browser_operation_leases
		(profile_id, operation, owner, acquired_at, expires_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, profileID, operation, owner, nowText, expiresAt, nowText)
	if err != nil {
		return false, fmt.Errorf("acquire browser operation lease: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("acquire browser operation lease rows affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit browser operation lease: %w", err)
	}
	return changed == 1, nil
}

// ReleaseBrowserOperationLease is owner-bound so delayed cleanup cannot
// unlock a newer action for the same profile.
func (db *DB) ReleaseBrowserOperationLease(profileID, owner string) error {
	profileID = strings.TrimSpace(profileID)
	owner = strings.TrimSpace(owner)
	if profileID == "" || !validLeaseOwner(owner) {
		return fmt.Errorf("invalid browser operation lease release")
	}
	if _, err := db.Exec(`DELETE FROM browser_operation_leases WHERE profile_id = ? AND owner = ?`, profileID, owner); err != nil {
		return fmt.Errorf("release browser operation lease: %w", err)
	}
	return nil
}

// ReleaseStoppedCancelledPublishLease repairs only a lease stranded by a
// cancelled, unsubmitted job after its BrowserFacade instance has stopped.
// It never releases a live run or a clicked submission awaiting verification.
func (db *DB) ReleaseStoppedCancelledPublishLease(accountID int64) (bool, error) {
	if accountID <= 0 {
		return false, fmt.Errorf("account id must be positive")
	}
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var profileID, state string
	err = tx.QueryRow(`SELECT profile_id, state FROM browser_runtime_instances
		WHERE account_id=? ORDER BY updated_at DESC LIMIT 1`, accountID).Scan(&profileID, &state)
	if err == sql.ErrNoRows || (err == nil && state != "stopped" && state != "crashed") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var running, unresolved, cancelled int
	if err := tx.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN status='running' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN COALESCE(submit_attempted_at,'')<>'' AND COALESCE(submit_outcome,'')<>'verified_published' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN status='cancelled' AND COALESCE(submit_attempted_at,'')='' THEN 1 ELSE 0 END),0)
		FROM publish_jobs WHERE account_id=?`, accountID).Scan(&running, &unresolved, &cancelled); err != nil {
		return false, err
	}
	if running > 0 || unresolved > 0 || cancelled == 0 {
		return false, nil
	}
	result, err := tx.Exec(`DELETE FROM browser_operation_leases WHERE profile_id=? AND operation='publish'`, profileID)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return changed > 0, nil
}

// HasBackgroundBrowserObservationLease identifies only the short, read-only
// account monitor lease. Callers may briefly wait for this owner, but must not
// wait for or preempt publish/start/stop/Agent operations. The owner value is
// deliberately not returned outside the repository.
func (db *DB) HasBackgroundBrowserObservationLease(profileID string, now time.Time) (bool, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return false, fmt.Errorf("profile id is required")
	}
	var operation, owner, expiresAt string
	err := db.QueryRow(`SELECT operation, owner, expires_at FROM browser_operation_leases WHERE profile_id=?`, profileID).Scan(&operation, &owner, &expiresAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read browser operation lease: %w", err)
	}
	expires, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return false, fmt.Errorf("parse browser operation lease expiry: %w", err)
	}
	return expires.After(now.UTC()) && operation == "observe" && strings.HasPrefix(owner, "observe-probe:"), nil
}

func (db *DB) EnqueueTask(kind, payloadJSON string, priority int, runAfter, traceID string) (*TaskQueueItem, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if runAfter == "" {
		runAfter = now
	}
	res, err := db.Exec(`INSERT INTO task_queue (kind, payload_json, status, priority, run_after, attempts, last_error, trace_id, created_at, updated_at) VALUES (?, ?, 'pending', ?, ?, 0, '', ?, ?, ?)`,
		kind, payloadJSON, priority, runAfter, traceID, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &TaskQueueItem{ID: id, Kind: kind, PayloadJSON: payloadJSON, Status: "pending", Priority: priority, RunAfter: runAfter, TraceID: traceID, CreatedAt: now, UpdatedAt: now}, nil
}

// ClaimNextTaskWithLease atomically reclaims expired work and leases one ready
// item to owner. The transaction only changes SQLite state; callers must do
// browser/network work after it commits.
func (db *DB) ClaimNextTaskWithLease(now time.Time, owner string, leaseTTL time.Duration) (*TaskQueueItem, error) {
	owner = strings.TrimSpace(owner)
	if !validLeaseOwner(owner) {
		return nil, fmt.Errorf("invalid task lease owner")
	}
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	if leaseTTL > 10*time.Minute {
		return nil, fmt.Errorf("task lease duration exceeds 10 minutes")
	}
	now = now.UTC()
	nowText := now.Format(time.RFC3339)
	expiresAt := now.Add(leaseTTL).Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin task claim: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE task_queue
		SET status=CASE WHEN cancel_requested_at IS NULL THEN 'pending' ELSE 'cancelled' END,
			lease_owner=NULL, lease_expires_at=NULL, updated_at=?
		WHERE status='running' AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?`, nowText, nowText); err != nil {
		return nil, fmt.Errorf("reclaim expired task leases: %w", err)
	}
	var task TaskQueueItem
	err = tx.QueryRow(`SELECT id, kind, COALESCE(payload_json,''), status, priority, COALESCE(run_after,''), attempts, COALESCE(last_error,''), COALESCE(trace_id,''), COALESCE(lease_owner,''), COALESCE(lease_expires_at,''), COALESCE(cancel_requested_at,''), COALESCE(created_at,''), COALESCE(updated_at,'')
		FROM task_queue
		WHERE status='pending' AND cancel_requested_at IS NULL AND (run_after IS NULL OR run_after='' OR run_after <= ?)
		ORDER BY priority DESC, id ASC LIMIT 1`, nowText).
		Scan(&task.ID, &task.Kind, &task.PayloadJSON, &task.Status, &task.Priority, &task.RunAfter, &task.Attempts, &task.LastError, &task.TraceID, &task.LeaseOwner, &task.LeaseExpiresAt, &task.CancelRequestedAt, &task.CreatedAt, &task.UpdatedAt)
	if err == sql.ErrNoRows {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit empty task claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select ready task: %w", err)
	}
	res, err := tx.Exec(`UPDATE task_queue SET status='running', attempts=attempts+1, lease_owner=?, lease_expires_at=?, updated_at=?
		WHERE id=? AND status='pending' AND cancel_requested_at IS NULL`, owner, expiresAt, nowText, task.ID)
	if err != nil {
		return nil, fmt.Errorf("lease task: %w", err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("lease task rows affected: %w", err)
	}
	if changed != 1 {
		return nil, fmt.Errorf("task %d claim lost", task.ID)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit task claim: %w", err)
	}
	task.Status = "running"
	task.Attempts++
	task.LeaseOwner = owner
	task.LeaseExpiresAt = expiresAt
	task.UpdatedAt = nowText
	return &task, nil
}

// ClaimTaskWithLease claims one known pending task. It is used by event-driven
// HTTP actions so they execute the task they just enqueued, not an unrelated
// higher-priority queue item.
func (db *DB) ClaimTaskWithLease(id int64, now time.Time, owner string, leaseTTL time.Duration) (*TaskQueueItem, error) {
	owner = strings.TrimSpace(owner)
	if id <= 0 || !validLeaseOwner(owner) {
		return nil, fmt.Errorf("invalid task lease request")
	}
	if leaseTTL <= 0 {
		leaseTTL = time.Minute
	}
	if leaseTTL > 10*time.Minute {
		return nil, fmt.Errorf("task lease duration exceeds 10 minutes")
	}
	now = now.UTC()
	nowText := now.Format(time.RFC3339)
	expiresAt := now.Add(leaseTTL).Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin task claim: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE task_queue
		SET status=CASE WHEN cancel_requested_at IS NULL THEN 'pending' ELSE 'cancelled' END,
			lease_owner=NULL, lease_expires_at=NULL, updated_at=?
		WHERE id=? AND status='running' AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?`, nowText, id, nowText); err != nil {
		return nil, fmt.Errorf("reclaim task lease: %w", err)
	}
	var task TaskQueueItem
	err = tx.QueryRow(`SELECT id, kind, COALESCE(payload_json,''), status, priority, COALESCE(run_after,''), attempts, COALESCE(last_error,''), COALESCE(trace_id,''), COALESCE(lease_owner,''), COALESCE(lease_expires_at,''), COALESCE(cancel_requested_at,''), COALESCE(created_at,''), COALESCE(updated_at,'')
		FROM task_queue WHERE id=? AND status='pending' AND cancel_requested_at IS NULL AND (run_after IS NULL OR run_after='' OR run_after <= ?)`, id, nowText).
		Scan(&task.ID, &task.Kind, &task.PayloadJSON, &task.Status, &task.Priority, &task.RunAfter, &task.Attempts, &task.LastError, &task.TraceID, &task.LeaseOwner, &task.LeaseExpiresAt, &task.CancelRequestedAt, &task.CreatedAt, &task.UpdatedAt)
	if err == sql.ErrNoRows {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit empty task claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select task: %w", err)
	}
	res, err := tx.Exec(`UPDATE task_queue SET status='running', attempts=attempts+1, lease_owner=?, lease_expires_at=?, updated_at=? WHERE id=? AND status='pending' AND cancel_requested_at IS NULL`, owner, expiresAt, nowText, id)
	if err != nil {
		return nil, fmt.Errorf("lease task: %w", err)
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("lease task rows affected: %w", err)
	}
	if changed != 1 {
		return nil, fmt.Errorf("task %d claim lost", id)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit task claim: %w", err)
	}
	task.Status = "running"
	task.Attempts++
	task.LeaseOwner = owner
	task.LeaseExpiresAt = expiresAt
	task.UpdatedAt = nowText
	return &task, nil
}

func validLeaseOwner(owner string) bool {
	if len(owner) < 3 || len(owner) > 128 {
		return false
	}
	for _, r := range owner {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' && r != ':' {
			return false
		}
	}
	return true
}

func (db *DB) RenewTaskLease(id int64, owner string, now time.Time, leaseTTL time.Duration) (bool, error) {
	if id <= 0 || !validLeaseOwner(strings.TrimSpace(owner)) || leaseTTL <= 0 || leaseTTL > 10*time.Minute {
		return false, fmt.Errorf("invalid task lease renewal")
	}
	now = now.UTC()
	res, err := db.Exec(`UPDATE task_queue SET lease_expires_at=?, updated_at=? WHERE id=? AND status='running' AND lease_owner=? AND cancel_requested_at IS NULL`, now.Add(leaseTTL).Format(time.RFC3339), now.Format(time.RFC3339), id, owner)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

func (db *DB) CompleteLeasedTask(id int64, owner, status, lastError string, attempts int) (bool, error) {
	if id <= 0 || !validLeaseOwner(strings.TrimSpace(owner)) {
		return false, fmt.Errorf("invalid leased task completion")
	}
	switch status {
	case "done", "failed", "cancelled":
	default:
		return false, fmt.Errorf("invalid leased task status %q", status)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE task_queue SET status=CASE WHEN cancel_requested_at IS NULL THEN ? ELSE 'cancelled' END, last_error=?, attempts=?, lease_owner=NULL, lease_expires_at=NULL, updated_at=? WHERE id=? AND status='running' AND lease_owner=?`, status, lastError, attempts, now, id, owner)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

func (db *DB) RequestTaskCancel(id int64) (bool, error) {
	if id <= 0 {
		return false, fmt.Errorf("task id must be positive")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE task_queue SET cancel_requested_at=?, status=CASE WHEN status='pending' THEN 'cancelled' ELSE status END, updated_at=? WHERE id=? AND status IN ('pending','running')`, now, now, id)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

// ClaimNextTask claims the highest-priority pending task with run_after <= nowRFC3339.
func (db *DB) ClaimNextTask(nowRFC3339 string) (*TaskQueueItem, error) {
	if nowRFC3339 == "" {
		nowRFC3339 = time.Now().UTC().Format(time.RFC3339)
	}
	var t TaskQueueItem
	err := db.QueryRow(`SELECT id, kind, COALESCE(payload_json,''), status, priority, COALESCE(run_after,''), attempts, COALESCE(last_error,''), COALESCE(trace_id,''), COALESCE(created_at,''), COALESCE(updated_at,'')
		FROM task_queue
		WHERE status = 'pending' AND (run_after IS NULL OR run_after = '' OR run_after <= ?)
		ORDER BY priority DESC, id ASC
		LIMIT 1`, nowRFC3339).
		Scan(&t.ID, &t.Kind, &t.PayloadJSON, &t.Status, &t.Priority, &t.RunAfter, &t.Attempts, &t.LastError, &t.TraceID, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`UPDATE task_queue SET status = 'running', attempts = attempts + 1, updated_at = ? WHERE id = ? AND status = 'pending'`, now, t.ID)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("legacy task claim rows affected: %w", err)
	}
	if n == 0 {
		return nil, nil
	}
	t.Status = "running"
	t.Attempts++
	t.UpdatedAt = now
	return &t, nil
}

func (db *DB) UpdateTaskQueueItem(id int64, status, lastError string, attempts int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`UPDATE task_queue SET status = ?, last_error = ?, attempts = ?, updated_at = ? WHERE id = ?`,
		status, lastError, attempts, now, id)
	return err
}

func (db *DB) ListTaskQueue(limit int) ([]TaskQueueItem, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`SELECT id, kind, COALESCE(payload_json,''), status, priority, COALESCE(run_after,''), attempts, COALESCE(last_error,''), COALESCE(trace_id,''), COALESCE(lease_owner,''), COALESCE(lease_expires_at,''), COALESCE(cancel_requested_at,''), COALESCE(created_at,''), COALESCE(updated_at,'') FROM task_queue ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskQueueItem
	for rows.Next() {
		var t TaskQueueItem
		if err := rows.Scan(&t.ID, &t.Kind, &t.PayloadJSON, &t.Status, &t.Priority, &t.RunAfter, &t.Attempts, &t.LastError, &t.TraceID, &t.LeaseOwner, &t.LeaseExpiresAt, &t.CancelRequestedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// --- Notifications ---

type Notification struct {
	ID          int64  `json:"id"`
	Type        string `json:"type"`
	Level       string `json:"level"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Read        bool   `json:"read"`
	RelatedType string `json:"related_type"`
	RelatedID   int64  `json:"related_id"`
	CreatedAt   string `json:"created_at"`
}

func (db *DB) CreateNotification(kind, level, title, body, relatedType string, relatedID int64) (*Notification, error) {
	if kind == "" || title == "" {
		return nil, fmt.Errorf("notification type and title are required")
	}
	switch level {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("invalid notification level")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO notifications (type, level, title, body, read, related_type, related_id, created_at) VALUES (?, ?, ?, ?, 0, ?, ?, ?)`, kind, level, title, body, relatedType, nullableID(relatedID), now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("notification id: %w", err)
	}
	return &Notification{ID: id, Type: kind, Level: level, Title: title, Body: body, RelatedType: relatedType, RelatedID: relatedID, CreatedAt: now}, nil
}

func (db *DB) ListNotifications(limit int, unreadOnly bool) ([]Notification, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT id, type, level, title, COALESCE(body,''), read, COALESCE(related_type,''), COALESCE(related_id,0), created_at FROM notifications`
	if unreadOnly {
		query += ` WHERE read=0`
	}
	query += ` ORDER BY id DESC LIMIT ?`
	rows, err := db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		var read int
		if err := rows.Scan(&n.ID, &n.Type, &n.Level, &n.Title, &n.Body, &read, &n.RelatedType, &n.RelatedID, &n.CreatedAt); err != nil {
			return nil, err
		}
		n.Read = read == 1
		out = append(out, n)
	}
	return out, rows.Err()
}

func (db *DB) GetNotification(id int64) (*Notification, error) {
	if id <= 0 {
		return nil, fmt.Errorf("notification id must be positive")
	}
	var n Notification
	var read int
	err := db.QueryRow(`SELECT id, type, level, title, COALESCE(body,''), read, COALESCE(related_type,''), COALESCE(related_id,0), created_at FROM notifications WHERE id=?`, id).
		Scan(&n.ID, &n.Type, &n.Level, &n.Title, &n.Body, &read, &n.RelatedType, &n.RelatedID, &n.CreatedAt)
	if err != nil {
		return nil, err
	}
	n.Read = read == 1
	return &n, nil
}

func (db *DB) MarkNotificationRead(id int64) (bool, error) {
	if id <= 0 {
		return false, fmt.Errorf("notification id must be positive")
	}
	res, err := db.Exec(`UPDATE notifications SET read=1 WHERE id=? AND read=0`, id)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

func (db *DB) MarkAllNotificationsRead() (int64, error) {
	res, err := db.Exec(`UPDATE notifications SET read=1 WHERE read=0`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type MediaLibraryItem struct {
	ID         int64  `json:"id"`
	FilePath   string `json:"file_path"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"size_bytes"`
	Mime       string `json:"mime"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMS int    `json:"duration_ms"`
	Purpose    string `json:"purpose"`
	ProbeJSON  string `json:"-"`
	CreatedAt  string `json:"created_at"`
}

func inferredMediaPurpose(mime string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mime)), "video/") {
		return "video"
	}
	return "shared"
}

func normalizeMediaPurpose(purpose string) (string, error) {
	purpose = strings.TrimSpace(purpose)
	if purpose != "shared" && purpose != "video" && purpose != "graph_text" && purpose != "article" {
		return "", fmt.Errorf("media purpose must be shared, video, graph_text, or article")
	}
	return purpose, nil
}

func (db *DB) SetMediaLibraryPurpose(id int64, purpose string) error {
	if id <= 0 {
		return fmt.Errorf("media library id must be positive")
	}
	purpose, err := normalizeMediaPurpose(purpose)
	if err != nil {
		return err
	}
	result, err := db.Exec(`UPDATE media_library SET purpose=? WHERE id=?`, purpose, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("media library item %d not found", id)
	}
	return nil
}

func (db *DB) UpsertMediaLibrary(filePath, sha256Hash string, sizeBytes int64, mime string, width, height, durationMS int, probeJSON string) (*MediaLibraryItem, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	purpose := inferredMediaPurpose(mime)
	if sha256Hash != "" {
		// ponytail: AI assets use <sha>.<ext> names. Preserve that managed copy
		// when a later ordinary import sees the same bytes; a future multi-location
		// media table can replace this filename convention if it becomes necessary.
		if !isContentAddressedMediaPath(filePath, sha256Hash) {
			var existing MediaLibraryItem
			err := db.QueryRow(`SELECT id, file_path, COALESCE(sha256,''), COALESCE(size_bytes,0), COALESCE(mime,''), COALESCE(width,0), COALESCE(height,0), COALESCE(duration_ms,0), COALESCE(probe_json,''), COALESCE(purpose,'shared'), COALESCE(created_at,'') FROM media_library WHERE sha256=?`, sha256Hash).
				Scan(&existing.ID, &existing.FilePath, &existing.SHA256, &existing.SizeBytes, &existing.Mime, &existing.Width, &existing.Height, &existing.DurationMS, &existing.ProbeJSON, &existing.Purpose, &existing.CreatedAt)
			if err == nil && isContentAddressedMediaPath(existing.FilePath, sha256Hash) {
				return &existing, nil
			}
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
		}
		_, err := db.Exec(`INSERT INTO media_library (file_path, sha256, size_bytes, mime, width, height, duration_ms, probe_json, purpose, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(sha256) DO UPDATE SET
				file_path = excluded.file_path,
				size_bytes = excluded.size_bytes,
				mime = excluded.mime,
				width = excluded.width,
				height = excluded.height,
				duration_ms = excluded.duration_ms,
				probe_json = excluded.probe_json`,
			filePath, sha256Hash, sizeBytes, mime, width, height, durationMS, probeJSON, purpose, now)
		if err != nil {
			return nil, err
		}
		var item MediaLibraryItem
		err = db.QueryRow(`SELECT id, file_path, COALESCE(sha256,''), COALESCE(size_bytes,0), COALESCE(mime,''), COALESCE(width,0), COALESCE(height,0), COALESCE(duration_ms,0), COALESCE(probe_json,''), COALESCE(purpose,'shared'), COALESCE(created_at,'') FROM media_library WHERE sha256 = ?`, sha256Hash).
			Scan(&item.ID, &item.FilePath, &item.SHA256, &item.SizeBytes, &item.Mime, &item.Width, &item.Height, &item.DurationMS, &item.ProbeJSON, &item.Purpose, &item.CreatedAt)
		if err != nil {
			return nil, err
		}
		return &item, nil
	}
	res, err := db.Exec(`INSERT INTO media_library (file_path, sha256, size_bytes, mime, width, height, duration_ms, probe_json, purpose, created_at) VALUES (?, NULL, ?, ?, ?, ?, ?, ?, ?, ?)`,
		filePath, sizeBytes, mime, width, height, durationMS, probeJSON, purpose, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &MediaLibraryItem{ID: id, FilePath: filePath, SizeBytes: sizeBytes, Mime: mime, Width: width, Height: height, DurationMS: durationMS, Purpose: purpose, ProbeJSON: probeJSON, CreatedAt: now}, nil
}

func isContentAddressedMediaPath(path, sha256Hash string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	hash := strings.ToLower(strings.TrimSpace(sha256Hash))
	if len(hash) != 64 || !strings.HasPrefix(base, hash+".") {
		return false
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".png", ".jpg", ".gif", ".mp4", ".mov", ".webm", ".mkv":
		return base == hash+filepath.Ext(base)
	default:
		return false
	}
}

func (db *DB) ListMediaLibrary(limit int) ([]MediaLibraryItem, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`SELECT id, file_path, COALESCE(sha256,''), COALESCE(size_bytes,0), COALESCE(mime,''), COALESCE(width,0), COALESCE(height,0), COALESCE(duration_ms,0), COALESCE(probe_json,''), COALESCE(purpose,'shared'), COALESCE(created_at,'') FROM media_library ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaLibraryItem
	for rows.Next() {
		var m MediaLibraryItem
		if err := rows.Scan(&m.ID, &m.FilePath, &m.SHA256, &m.SizeBytes, &m.Mime, &m.Width, &m.Height, &m.DurationMS, &m.ProbeJSON, &m.Purpose, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMediaLibrary 只删除 Alpha 的素材索引，绝不删除用户选择的原始文件。
func (db *DB) DeleteMediaLibrary(id int64) error {
	if id <= 0 {
		return fmt.Errorf("media library id must be positive")
	}
	var references int
	if err := db.QueryRow(`SELECT COUNT(*) FROM clip_jobs WHERE source_media_id=?`, id).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return fmt.Errorf("media library item %d has clip jobs and cannot be deleted", id)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM media_assets WHERE library_id=?`, id).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return fmt.Errorf("media library item %d has content media associations and cannot be deleted", id)
	}
	result, err := db.Exec(`DELETE FROM media_library WHERE id=?`, id)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("media library item %d not found", id)
	}
	return nil
}

type ContentVersion struct {
	ID        int64
	ContentID int64
	Lang      string
	Title     string
	Caption   string
	Body      string
	TagsJSON  string
	CreatedAt string
}

func (db *DB) CreateContentVersion(contentID int64, lang, title, caption, body, tagsJSON string) (*ContentVersion, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO content_versions (content_id, lang, title, caption, body, tags_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		contentID, lang, title, caption, body, tagsJSON, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &ContentVersion{ID: id, ContentID: contentID, Lang: lang, Title: title, Caption: caption, Body: body, TagsJSON: tagsJSON, CreatedAt: now}, nil
}

func (db *DB) ListContentVersions(contentID int64) ([]ContentVersion, error) {
	rows, err := db.Query(`SELECT id, content_id, COALESCE(lang,''), COALESCE(title,''), COALESCE(caption,''), COALESCE(body,''), COALESCE(tags_json,''), COALESCE(created_at,'') FROM content_versions WHERE content_id = ? ORDER BY id`, contentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContentVersion
	for rows.Next() {
		var v ContentVersion
		if err := rows.Scan(&v.ID, &v.ContentID, &v.Lang, &v.Title, &v.Caption, &v.Body, &v.TagsJSON, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type PostMetric struct {
	ID              int64  `json:"id"`
	PublishRecordID int64  `json:"publish_record_id"`
	AccountID       int64  `json:"account_id"`
	PlatformCode    string `json:"platform_code"`
	ContentID       int64  `json:"content_id"`
	Views           int    `json:"views"`
	Likes           int    `json:"likes"`
	Comments        int    `json:"comments"`
	Shares          int    `json:"shares"`
	Favorites       int    `json:"favorites"`
	RawJSON         string `json:"-"`
	CollectedAt     string `json:"collected_at"`
	CreatedAt       string `json:"created_at"`
}

type MetricSummary struct {
	Samples   int `json:"samples"`
	Views     int `json:"views"`
	Likes     int `json:"likes"`
	Comments  int `json:"comments"`
	Shares    int `json:"shares"`
	Favorites int `json:"favorites"`
	Followers int `json:"followers"`
	Posts     int `json:"posts"`
	// Interactions 与 EngagementRate 是快照口径的派生值，方便表格直接展示。
	Interactions   int              `json:"interactions"`
	EngagementRate float64          `json:"engagement_rate"`
	Platforms      []MetricPlatform `json:"platforms"`
}
type MetricPlatform struct {
	PlatformCode string `json:"platform_code"`
	Samples      int    `json:"samples"`
	Views        int    `json:"views"`
	Likes        int    `json:"likes"`
	Comments     int    `json:"comments"`
	Shares       int    `json:"shares"`
	Favorites    int    `json:"favorites"`
	// 关注数与发帖数只有快照才有，旧 post_metrics 没有这两列。
	Followers int `json:"followers"`
	Posts     int `json:"posts"`
	// 互动率 = 互动总数 / 浏览数，0 表示样本不足或浏览为 0。
	EngagementRate float64 `json:"engagement_rate"`
}

// GetMetricSummary reads the creator metrics that the collection scheduler
// actually writes. The older post_metrics table is only filled by the legacy
// publish path and stays empty for accounts that were never published through
// this app, so reading it alone made the dashboard report zero even when real
// snapshots existed. Snapshots are the source of truth here; one row is one
// account on one day, so the latest snapshot per account is what should be
// summed, otherwise a 7-day window would count the same account seven times.
func (db *DB) GetMetricSummary() (*MetricSummary, error) {
	var summary MetricSummary
	// 必须与 CreatorMetricOverview 同口径：同一 (account_id, snapshot_date) 上
	// 允许 window_days=1/7/30/90 并存，也允许未验证的 profile 行存在。
	// 不加这两个约束时，一条 range_verified=0 的 profile 行会把当天的真实
	// 浏览量覆盖成 0 后又被当作「最新一天」计入，汇总值凭空翻倍或归零。
	var latestPerAccount = `SELECT account_id, MAX(snapshot_date) AS d FROM creator_metric_snapshots
		WHERE window_days = ` + strconv.Itoa(creatorMetricWindowDays) + ` AND range_verified = 1 GROUP BY account_id`
	joined := `FROM creator_metric_snapshots s JOIN (` + latestPerAccount + `) latest
		ON s.account_id = latest.account_id AND s.snapshot_date = latest.d
		WHERE s.window_days = ` + strconv.Itoa(creatorMetricWindowDays) + ` AND s.range_verified = 1`
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(s.views),0), COALESCE(SUM(s.likes),0), COALESCE(SUM(s.comments),0), COALESCE(SUM(s.shares),0), COALESCE(SUM(s.favorites),0), COALESCE(SUM(s.followers),0), COALESCE(SUM(s.posts),0) `+joined).Scan(&summary.Samples, &summary.Views, &summary.Likes, &summary.Comments, &summary.Shares, &summary.Favorites, &summary.Followers, &summary.Posts); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT COALESCE(s.platform_code,''), COUNT(*), COALESCE(SUM(s.views),0), COALESCE(SUM(s.likes),0), COALESCE(SUM(s.comments),0), COALESCE(SUM(s.shares),0), COALESCE(SUM(s.favorites),0), COALESCE(SUM(s.followers),0), COALESCE(SUM(s.posts),0) ` + joined + ` GROUP BY s.platform_code ORDER BY SUM(s.views) DESC, s.platform_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item MetricPlatform
		if err := rows.Scan(&item.PlatformCode, &item.Samples, &item.Views, &item.Likes, &item.Comments, &item.Shares, &item.Favorites, &item.Followers, &item.Posts); err != nil {
			return nil, err
		}
		interactions := item.Likes + item.Comments + item.Shares + item.Favorites
		if item.Views > 0 {
			item.EngagementRate = float64(interactions) / float64(item.Views)
		}
		summary.Platforms = append(summary.Platforms, item)
	}
	summary.Interactions = summary.Likes + summary.Comments + summary.Shares + summary.Favorites
	if summary.Views > 0 {
		summary.EngagementRate = float64(summary.Interactions) / float64(summary.Views)
	}
	return &summary, rows.Err()
}

func (db *DB) CreatePostMetric(publishRecordID, accountID int64, platformCode string, contentID int64, views, likes, comments, shares, favorites int, rawJSON, collectedAt string) (*PostMetric, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if collectedAt == "" {
		collectedAt = now
	}
	res, err := db.Exec(`INSERT INTO post_metrics (publish_record_id, account_id, platform_code, content_id, views, likes, comments, shares, favorites, raw_json, collected_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		publishRecordID, accountID, platformCode, contentID, views, likes, comments, shares, favorites, rawJSON, collectedAt, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &PostMetric{ID: id, PublishRecordID: publishRecordID, AccountID: accountID, PlatformCode: platformCode, ContentID: contentID, Views: views, Likes: likes, Comments: comments, Shares: shares, Favorites: favorites, RawJSON: rawJSON, CollectedAt: collectedAt, CreatedAt: now}, nil
}

func (db *DB) ListPostMetrics(limit int) ([]PostMetric, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.Query(`SELECT id, COALESCE(publish_record_id,0), COALESCE(account_id,0), COALESCE(platform_code,''), COALESCE(content_id,0), COALESCE(views,0), COALESCE(likes,0), COALESCE(comments,0), COALESCE(shares,0), COALESCE(favorites,0), COALESCE(raw_json,''), COALESCE(collected_at,''), COALESCE(created_at,'') FROM post_metrics ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PostMetric
	for rows.Next() {
		var m PostMetric
		if err := rows.Scan(&m.ID, &m.PublishRecordID, &m.AccountID, &m.PlatformCode, &m.ContentID, &m.Views, &m.Likes, &m.Comments, &m.Shares, &m.Favorites, &m.RawJSON, &m.CollectedAt, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Credential stores only non-secret refs (ref_key / meta_json). Never store plaintext secrets.
type Credential struct {
	ID           int64
	AccountID    int64
	PlatformCode string
	Kind         string
	RefKey       string
	MetaJSON     string
	CreatedAt    string
	UpdatedAt    string
}

func (db *DB) CreateCredential(accountID int64, platformCode, kind, refKey, metaJSON string) (*Credential, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO credentials (account_id, platform_code, kind, ref_key, meta_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		accountID, platformCode, kind, refKey, metaJSON, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Credential{ID: id, AccountID: accountID, PlatformCode: platformCode, Kind: kind, RefKey: refKey, MetaJSON: metaJSON, CreatedAt: now, UpdatedAt: now}, nil
}

func (db *DB) GetCredentialRef(accountID int64, platformCode string) (string, error) {
	var ref string
	err := db.QueryRow(
		`SELECT ref_key FROM credentials WHERE account_id = ? AND platform_code = ? ORDER BY id DESC LIMIT 1`,
		accountID, platformCode,
	).Scan(&ref)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ref, nil
}

func (db *DB) ListCredentials() ([]Credential, error) {
	rows, err := db.Query(`SELECT id, COALESCE(account_id,0), COALESCE(platform_code,''), kind, ref_key, COALESCE(meta_json,''), COALESCE(created_at,''), COALESCE(updated_at,'') FROM credentials ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		var c Credential
		if err := rows.Scan(&c.ID, &c.AccountID, &c.PlatformCode, &c.Kind, &c.RefKey, &c.MetaJSON, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountRows returns SELECT COUNT(*) for an allowlisted table name (export helper).
func (db *DB) CountRows(table string) (int64, error) {
	allowed := map[string]bool{
		"accounts": true, "contents": true, "media_assets": true, "media_library": true,
		"publish_jobs": true, "publish_records": true, "task_queue": true,
		"post_metrics": true, "audit_events": true,
		"batch_runs": true, "content_versions": true,
	}
	if !allowed[table] {
		return 0, fmt.Errorf("unknown table %q", table)
	}
	var n int64
	err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n)
	return n, err
}

// --- App users (tool login, not social accounts) ---

type AppUser struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string
	Enabled      bool
	CreatedAt    string
}

func (db *DB) CreateAppUser(username, passwordHash, role string) (*AppUser, error) {
	if role == "" {
		role = "admin"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO app_users (username, password_hash, role, enabled, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)`,
		username, passwordHash, role, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &AppUser{ID: id, Username: username, PasswordHash: passwordHash, Role: role, Enabled: true, CreatedAt: now}, nil
}

func (db *DB) GetAppUserByUsername(username string) (*AppUser, error) {
	var u AppUser
	var enabled int
	err := db.QueryRow(`SELECT id, username, password_hash, role, enabled, COALESCE(created_at,'') FROM app_users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &enabled, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user %q not found", username)
	}
	if err != nil {
		return nil, err
	}
	u.Enabled = enabled == 1
	return &u, nil
}

func (db *DB) CountAppUsers() (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM app_users`).Scan(&n)
	return n, err
}

func (db *DB) ListAppUsers() ([]AppUser, error) {
	rows, err := db.Query(`SELECT id, username, role, enabled, COALESCE(created_at,'') FROM app_users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppUser
	for rows.Next() {
		var u AppUser
		var enabled int
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &enabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.Enabled = enabled == 1
		out = append(out, u)
	}
	return out, rows.Err()
}
