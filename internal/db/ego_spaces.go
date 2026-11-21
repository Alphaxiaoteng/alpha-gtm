package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type EgoIdentity struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Sort        int    `json:"sort"`
	ProfileID   string `json:"profile_id"`
	ProfileName string `json:"profile_name"`
	Avatar      string `json:"avatar"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type EgoSpace struct {
	ID           int64  `json:"id"`
	IdentityID   int64  `json:"identity_id"`
	AccountID    int64  `json:"-"`
	SpaceID      int64  `json:"space_id"`
	TaskID       string `json:"task_id"`
	Name         string `json:"name"`
	Ownership    string `json:"ownership"`
	State        string `json:"state"`
	ControlEpoch int    `json:"control_epoch"`
	ActiveJobID  int64  `json:"active_job_id"`
	LastSeenAt   string `json:"last_seen_at"`
	ErrorCode    string `json:"error_code"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func (db *DB) CreateEgoIdentity(name string) (*EgoIdentity, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		var next int
		if err := db.QueryRow(`SELECT COUNT(*)+1 FROM ego_identities`).Scan(&next); err != nil {
			return nil, err
		}
		name = browserName(next)
	}
	if len([]rune(name)) > 80 {
		return nil, fmt.Errorf("invalid ego identity name")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var nextSort int
	if err := db.QueryRow(`SELECT COALESCE(MAX(sort),-1)+1 FROM ego_identities`).Scan(&nextSort); err != nil {
		return nil, err
	}
	result, err := db.Exec(`INSERT INTO ego_identities (name,enabled,sort,created_at,updated_at) VALUES (?,1,?,?,?)`, name, nextSort, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return db.GetEgoIdentity(id)
}

func (db *DB) GetEgoIdentity(id int64) (*EgoIdentity, error) {
	var identity EgoIdentity
	var enabled int
	var avatar sql.NullString
	err := db.QueryRow(`SELECT id,name,enabled,sort,profile_id,profile_name,avatar,created_at,updated_at FROM ego_identities WHERE id=?`, id).
		Scan(&identity.ID, &identity.Name, &enabled, &identity.Sort, &identity.ProfileID, &identity.ProfileName, &avatar, &identity.CreatedAt, &identity.UpdatedAt)
	if avatar.Valid {
		identity.Avatar = avatar.String
	}
	if err != nil {
		return nil, err
	}
	identity.Enabled = enabled == 1
	return &identity, nil
}

func (db *DB) ListEgoIdentities() ([]EgoIdentity, error) {
	rows, err := db.Query(`SELECT id,name,enabled,sort,profile_id,profile_name,avatar,created_at,updated_at FROM ego_identities ORDER BY sort,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var identities []EgoIdentity
	for rows.Next() {
		var identity EgoIdentity
		var enabled int
		var avatar sql.NullString
		if err := rows.Scan(&identity.ID, &identity.Name, &enabled, &identity.Sort, &identity.ProfileID, &identity.ProfileName, &avatar, &identity.CreatedAt, &identity.UpdatedAt); err != nil {
			return nil, err
		}
		identity.Enabled = enabled == 1
		if avatar.Valid {
			identity.Avatar = avatar.String
		}
		identities = append(identities, identity)
	}
	return identities, rows.Err()
}

func (db *DB) RenameEgoIdentity(id int64, name string) (*EgoIdentity, error) {
	identity, err := db.GetEgoIdentity(id)
	if err != nil {
		return nil, err
	}
	return db.UpdateEgoIdentity(id, name, identity.Sort)
}

func (db *DB) UpdateEgoIdentity(id int64, name string, sortOrder int) (*EgoIdentity, error) {
	name = strings.TrimSpace(name)
	if id <= 0 || name == "" || len([]rune(name)) > 80 {
		return nil, fmt.Errorf("invalid ego identity name")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE ego_identities SET name=?,sort=?,updated_at=? WHERE id=?`, name, sortOrder, now, id)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, sql.ErrNoRows
	}
	return db.GetEgoIdentity(id)
}

func browserName(index int) string {
	return fmt.Sprintf("浏览器%d", index)
}

func isLegacyAutomaticIdentityName(name string) bool {
	compact := strings.ReplaceAll(strings.TrimSpace(name), " ", "")
	for _, prefix := range []string{"用户", "矩阵"} {
		suffix, found := strings.CutPrefix(compact, prefix)
		if !found || suffix == "" {
			continue
		}
		number, err := strconv.Atoi(suffix)
		if err == nil && number > 0 && strconv.Itoa(number) == suffix {
			return true
		}
	}
	for _, numeral := range []string{"一", "二", "三", "四", "五", "六", "七", "八", "九", "十"} {
		if compact == "矩阵"+numeral {
			return true
		}
	}
	return false
}

func (db *DB) CreateEgoSpaceForIdentity(identityID, spaceID int64, taskID, name string) (*EgoSpace, error) {
	taskID = strings.TrimSpace(taskID)
	name = strings.TrimSpace(name)
	if identityID <= 0 || spaceID < 0 || taskID == "" || name == "" {
		return nil, fmt.Errorf("invalid ego space")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`INSERT INTO ego_spaces (
		identity_id,space_id,task_id,name,ownership,state,control_epoch,last_seen_at,error_code,created_at,updated_at
	) VALUES (?,?,?,?,'agent','ready',1,?,'',?,?)`, identityID, spaceID, taskID, name, now, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return db.getEgoSpace(`WHERE id=?`, id)
}

func (db *DB) CreateEgoSpace(accountID, spaceID int64, taskID, name string) (*EgoSpace, error) {
	account, err := db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	return db.CreateEgoSpaceForIdentity(account.IdentityID, spaceID, taskID, name)
}

func (db *DB) GetEgoSpaceByIdentity(identityID int64) (*EgoSpace, error) {
	return db.getEgoSpace(`WHERE identity_id=?`, identityID)
}

func (db *DB) GetEgoSpaceByAccount(accountID int64) (*EgoSpace, error) {
	account, err := db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	space, err := db.GetEgoSpaceByIdentity(account.IdentityID)
	if err == nil {
		space.AccountID = accountID
	}
	return space, err
}

func (db *DB) ListEgoSpaces() ([]EgoSpace, error) {
	rows, err := db.Query(`SELECT id,identity_id,space_id,task_id,name,ownership,state,control_epoch,
		COALESCE(active_job_id,0),last_seen_at,error_code,created_at,updated_at
		FROM ego_spaces ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	spaces := make([]EgoSpace, 0)
	for rows.Next() {
		space, err := scanEgoSpace(rows)
		if err != nil {
			return nil, err
		}
		spaces = append(spaces, *space)
	}
	return spaces, rows.Err()
}

func (db *DB) UpdateEgoSpaceState(accountID int64, ownership, state, errorCode string) (*EgoSpace, error) {
	account, err := db.GetAccount(accountID)
	if err != nil {
		return nil, err
	}
	return db.UpdateEgoSpaceStateForIdentity(account.IdentityID, ownership, state, errorCode)
}

func (db *DB) UpdateEgoSpaceStateForIdentity(identityID int64, ownership, state, errorCode string) (*EgoSpace, error) {
	ownership = strings.TrimSpace(ownership)
	state = strings.TrimSpace(state)
	if ownership == "" || state == "" {
		return nil, fmt.Errorf("invalid ego space state")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE ego_spaces
		SET ownership=?,state=?,error_code=?,control_epoch=control_epoch+1,last_seen_at=?,updated_at=?
		WHERE identity_id=?`, ownership, state, strings.TrimSpace(errorCode), now, now, identityID)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, sql.ErrNoRows
	}
	return db.GetEgoSpaceByIdentity(identityID)
}

func (db *DB) RebindEgoSpaceForIdentity(identityID, spaceID int64, taskID, name string) (*EgoSpace, error) {
	taskID = strings.TrimSpace(taskID)
	name = strings.TrimSpace(name)
	if identityID <= 0 || spaceID < 0 || taskID == "" || name == "" {
		return nil, fmt.Errorf("invalid ego space")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(`UPDATE ego_spaces SET
		space_id=?,task_id=?,name=?,ownership='agent',state='ready',active_job_id=NULL,
		error_code='',control_epoch=control_epoch+1,last_seen_at=?,updated_at=?
		WHERE identity_id=?`, spaceID, taskID, name, now, now, identityID)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, sql.ErrNoRows
	}
	return db.GetEgoSpaceByIdentity(identityID)
}

func (db *DB) getEgoSpace(where string, arg any) (*EgoSpace, error) {
	return scanEgoSpace(db.QueryRow(`SELECT id,identity_id,space_id,task_id,name,ownership,state,control_epoch,
		COALESCE(active_job_id,0),last_seen_at,error_code,created_at,updated_at
		FROM ego_spaces `+where, arg))
}

func scanEgoSpace(scanner interface{ Scan(...any) error }) (*EgoSpace, error) {
	var space EgoSpace
	err := scanner.Scan(&space.ID, &space.IdentityID, &space.SpaceID, &space.TaskID, &space.Name,
		&space.Ownership, &space.State, &space.ControlEpoch, &space.ActiveJobID, &space.LastSeenAt,
		&space.ErrorCode, &space.CreatedAt, &space.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &space, nil
}

// ErrEgoProfileTaken reports that a profile is already bound to another identity.
var ErrEgoProfileTaken = errors.New("ego profile already bound")

// SetEgoIdentityProfile binds an identity to one concrete ego-lite profile.
// A profile may only belong to a single identity.
func (db *DB) SetEgoIdentityProfile(id int64, profileID, profileName string) error {
	profileID = strings.TrimSpace(profileID)
	if id <= 0 || profileID == "" {
		return fmt.Errorf("invalid ego identity profile binding")
	}
	identity, err := db.GetEgoIdentity(id)
	if err != nil {
		return err
	}
	if identity.ProfileID == profileID {
		return nil
	}
	var owner int64
	err = db.QueryRow(`SELECT id FROM ego_identities WHERE profile_id=? AND id<>? LIMIT 1`, profileID, id).Scan(&owner)
	if err == nil {
		return fmt.Errorf("%w: identity %d", ErrEgoProfileTaken, owner)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = db.Exec(`UPDATE ego_identities SET profile_id=?, profile_name=?, updated_at=? WHERE id=?`,
		profileID, strings.TrimSpace(profileName), time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// ClearEgoIdentityProfile removes the profile binding from an identity.
func (db *DB) ClearEgoIdentityProfile(id int64) error {
	if id <= 0 {
		return fmt.Errorf("invalid ego identity")
	}
	_, err := db.Exec(`UPDATE ego_identities SET profile_id='', profile_name='', updated_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// EgoSpacesByIdentity returns every bound Space keyed by identity, so callers
// that render a whole account list do not repeat one query per account. The
// per-account lookup it replaces (`GetEgoSpaceByAccount`) resolves the account
// row first only to read its identity, which the caller already has.
func (db *DB) EgoSpacesByIdentity() (map[int64]EgoSpace, error) {
	rows, err := db.Query(`SELECT id,identity_id,space_id,task_id,name,ownership,state,control_epoch,
		COALESCE(active_job_id,0),last_seen_at,error_code,created_at,updated_at
		FROM ego_spaces ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byIdentity := make(map[int64]EgoSpace, 16)
	for rows.Next() {
		var space EgoSpace
		if err := rows.Scan(&space.ID, &space.IdentityID, &space.SpaceID, &space.TaskID, &space.Name,
			&space.Ownership, &space.State, &space.ControlEpoch, &space.ActiveJobID, &space.LastSeenAt,
			&space.ErrorCode, &space.CreatedAt, &space.UpdatedAt); err != nil {
			return nil, err
		}
		byIdentity[space.IdentityID] = space
	}
	return byIdentity, rows.Err()
}
