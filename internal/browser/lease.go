package browser

import (
	"fmt"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/errors"
)

// LeaseHolderType 标识 lease 持有者类型。
type LeaseHolderType string

const (
	HolderUI    LeaseHolderType = "ui"
	HolderCLI   LeaseHolderType = "cli"
	HolderJob   LeaseHolderType = "job"
	HolderBatch LeaseHolderType = "batch"
)

// Lease 代表一个 profile 的互斥租约。
type Lease struct {
	ProfileID  string
	HolderType LeaseHolderType
	HolderID   string
	AcquiredAt time.Time
	ExpiresAt  time.Time
}

// LeaseManager 基于 SQLite profile_leases 表实现跨进程 profile 互斥。
// ponytail: concrete struct, 不建 interface。与 lock.go 的 flock 互补：
// flock 保护文件系统级互斥，lease 保护逻辑级互斥（CLI job vs UI session）。
type LeaseManager struct {
	db *db.DB
}

func NewLeaseManager(database *db.DB) *LeaseManager {
	return &LeaseManager{db: database}
}

// Acquire 尝试获取 profile 租约。先清理过期，再原子 INSERT。
func (l *LeaseManager) Acquire(profileID string, holderType LeaseHolderType, holderID string, ttl time.Duration) error {
	for attempt := 0; attempt < 2; attempt++ {
		if err := l.CleanExpired(); err != nil {
			return err
		}
		nowTime := time.Now().UTC()
		now := nowTime.Format(time.RFC3339)
		expiresAt := nowTime.Add(ttl).Format(time.RFC3339)
		res, err := l.db.Exec(
			`INSERT OR IGNORE INTO profile_leases (profile_id, holder_type, holder_id, acquired_at, expires_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			profileID, string(holderType), holderID, now, expiresAt, now,
		)
		if err != nil {
			return fmt.Errorf("acquire lease: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("acquire lease rows affected: %w", err)
		}
		if rows == 1 {
			return nil
		}

		// The holder may be us, another live owner, or an expired row that
		// raced the first cleanup. Retry only the last case, and only once.
		existing, ok := l.Get(profileID)
		if ok && existing.HolderID == holderID {
			return l.Renew(profileID, holderID, ttl)
		}
		if ok {
			return fmt.Errorf("%s: held by %s (%s)", errors.PROFILE_LOCKED, existing.HolderType, existing.HolderID)
		}
	}
	return fmt.Errorf("%s: lease changed while acquiring profile %s", errors.PROFILE_LOCKED, profileID)
}

// Renew 续租。
func (l *LeaseManager) Renew(profileID, holderID string, ttl time.Duration) error {
	now := time.Now().UTC().Format(time.RFC3339)
	expiresAt := time.Now().UTC().Add(ttl).Format(time.RFC3339)

	res, err := l.db.Exec(
		`UPDATE profile_leases SET expires_at = ?, updated_at = ?
		 WHERE profile_id = ? AND holder_id = ?`,
		expiresAt, now, profileID, holderID,
	)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%s: lease for profile %s is no longer owned by %s", errors.PROFILE_LOCKED, profileID, holderID)
	}
	return nil
}

// Release 释放租约。
func (l *LeaseManager) Release(profileID, holderID string) error {
	_, err := l.db.Exec(
		`DELETE FROM profile_leases WHERE profile_id = ? AND holder_id = ?`,
		profileID, holderID,
	)
	return err
}

// Get 查询当前租约持有者。返回 nil, false 表示空闲。
func (l *LeaseManager) Get(profileID string) (*Lease, bool) {
	var lease Lease
	var holderType, acquiredAt, expiresAt string

	err := l.db.QueryRow(
		`SELECT profile_id, holder_type, holder_id, acquired_at, expires_at
		 FROM profile_leases WHERE profile_id = ?`, profileID,
	).Scan(&lease.ProfileID, &holderType, &lease.HolderID, &acquiredAt, &expiresAt)

	if err != nil {
		return nil, false
	}

	lease.HolderType = LeaseHolderType(holderType)
	lease.AcquiredAt, _ = time.Parse(time.RFC3339, acquiredAt)
	lease.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)

	// 如果已过期，当作不存在
	if lease.ExpiresAt.Before(time.Now().UTC()) {
		return nil, false
	}

	return &lease, true
}

// CleanExpired 清理所有过期租约。
func (l *LeaseManager) CleanExpired() error {
	_, err := l.db.Exec(`DELETE FROM profile_leases WHERE expires_at < ?`, time.Now().UTC().Format(time.RFC3339))
	return err
}
