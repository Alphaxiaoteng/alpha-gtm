package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrEgoPlatformConfigurationConflict = errors.New("EGO_PLATFORM_CONFIGURATION_CONFLICT")

func (db *DB) ConfigureIdentityPlatforms(identityID int64, platformCodes []string) ([]Account, error) {
	return db.configureIdentityPlatforms(identityID, platformCodes, nil)
}

func (db *DB) ConfigureIdentityPlatformsIfExpected(identityID int64, platformCodes, expectedPlatformCodes []string) ([]Account, error) {
	return db.configureIdentityPlatforms(identityID, platformCodes, &expectedPlatformCodes)
}

func (db *DB) configureIdentityPlatforms(identityID int64, platformCodes []string, expectedPlatformCodes *[]string) ([]Account, error) {
	if identityID <= 0 {
		return nil, fmt.Errorf("identity_id must be positive")
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM ego_identities WHERE id=?`, identityID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists != 1 {
		return nil, fmt.Errorf("identity not found")
	}
	codes, err := configuredPlatformCodes(tx, platformCodes, true)
	if err != nil {
		return nil, err
	}
	if expectedPlatformCodes != nil {
		expected, err := configuredPlatformCodes(tx, *expectedPlatformCodes, false)
		if err != nil {
			return nil, err
		}
		if current, err := enabledPlatformCodes(tx, identityID); err != nil {
			return nil, err
		} else if !samePlatformCodes(current, expected) {
			return nil, ErrEgoPlatformConfigurationConflict
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE accounts SET enabled=0,updated_at=? WHERE identity_id=?`, now, identityID); err != nil {
		return nil, err
	}
	for position, code := range codes {
		var accountID int64
		err := tx.QueryRow(`SELECT id FROM accounts WHERE identity_id=? AND platform_code=?`, identityID, code).Scan(&accountID)
		if err == nil {
			if _, err := tx.Exec(`UPDATE accounts SET enabled=1,sort=?,updated_at=? WHERE id=?`, position, now, accountID); err != nil {
				return nil, err
			}
			continue
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
		var platformName string
		if err := tx.QueryRow(`SELECT name FROM platforms WHERE code=?`, code).Scan(&platformName); err != nil {
			return nil, err
		}
		result, err := tx.Exec(`INSERT INTO accounts (
				identity_id,platform_code,kind,name,login_state,sort,remark,source,created_at,updated_at
			) VALUES (?,?,'browser_login',?,'unknown',?,'','manual',?,?)`,
			identityID, code, platformName, position, now, now)
		if err != nil {
			return nil, err
		}
		accountID, err = result.LastInsertId()
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`INSERT INTO account_assets (
				account_id,platform_code,asset_type,name,external_id,avatar,enabled,last_seen_at,created_at,updated_at
			) VALUES (?,?,'browser_account',?,'','',1,'',?,?)`,
			accountID, code, platformName, now, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	all, err := db.ListAccountsByIdentity(identityID)
	if err != nil {
		return nil, err
	}
	enabled := make([]Account, 0, len(codes))
	for _, account := range all {
		if account.Enabled {
			enabled = append(enabled, account)
		}
	}
	return enabled, nil
}

func configuredPlatformCodes(tx *sql.Tx, platformCodes []string, requireEnabled bool) ([]string, error) {
	codes := make([]string, 0, len(platformCodes))
	seen := make(map[string]struct{}, len(platformCodes))
	for _, rawCode := range platformCodes {
		code := strings.TrimSpace(rawCode)
		if code == "" {
			return nil, fmt.Errorf("platform_code is required")
		}
		if _, duplicate := seen[code]; duplicate {
			return nil, fmt.Errorf("duplicate platform_code")
		}
		query := `SELECT COUNT(*) FROM platforms WHERE code=?`
		if requireEnabled {
			query += ` AND enabled=1`
		}
		var exists int
		if err := tx.QueryRow(query, code).Scan(&exists); err != nil {
			return nil, err
		}
		if exists != 1 {
			return nil, fmt.Errorf("platform %q not found", code)
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	return codes, nil
}

func enabledPlatformCodes(tx *sql.Tx, identityID int64) ([]string, error) {
	rows, err := tx.Query(`SELECT platform_code FROM accounts WHERE identity_id=? AND enabled=1`, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	codes := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, rows.Err()
}

func samePlatformCodes(current, expected []string) bool {
	if len(current) != len(expected) {
		return false
	}
	expectedSet := make(map[string]struct{}, len(expected))
	for _, code := range expected {
		expectedSet[code] = struct{}{}
	}
	for _, code := range current {
		if _, ok := expectedSet[code]; !ok {
			return false
		}
	}
	return true
}
