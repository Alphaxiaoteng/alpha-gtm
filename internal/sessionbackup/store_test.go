package sessionbackup

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func testSnapshot() Snapshot {
	return Snapshot{
		Version:   FormatVersion,
		CreatedAt: time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC),
		Account:   Account{PlatformCode: "xhs", Name: "测试账号", Remark: "迁移测试", Group: "内容"},
		Profile:   Profile{BrowserType: "chromium", FingerprintSeed: 123456, Lang: "zh-CN", AcceptLang: "zh-CN,zh", Timezone: "Asia/Shanghai"},
		Cookies:   []Cookie{{Name: "session", Value: "secret-session-value", Domain: ".xiaohongshu.com", Path: "/", Secure: true, HTTPOnly: true}},
	}
}

func TestPortableRoundTripEncryptsCookieValues(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot()
	archive, err := store.EncryptPortable(snapshot, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(archive, []byte(snapshot.Cookies[0].Value)) {
		t.Fatal("portable archive must not contain plaintext cookie values")
	}
	restored, err := store.DecryptPortable(archive, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Account.PlatformCode != snapshot.Account.PlatformCode || restored.Cookies[0].Value != snapshot.Cookies[0].Value {
		t.Fatal("portable archive did not restore the expected session")
	}
	if _, err := store.DecryptPortable(archive, "incorrect passphrase"); !errors.Is(err, ErrInvalidPassphrase) {
		t.Fatalf("wrong passphrase error = %v, want ErrInvalidPassphrase", err)
	}
}

func TestLocalBackupsRetainThirtyFiveDaysAndExposeOnlyMetadata(t *testing.T) {
	clock := time.Date(2026, time.January, 1, 9, 0, 0, 0, time.UTC)
	store, err := newStore(t.TempDir(), func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	for day := 0; day < 40; day++ {
		snapshot := testSnapshot()
		snapshot.CreatedAt = clock
		if _, err := store.SaveLocal(7, snapshot); err != nil {
			t.Fatalf("save day %d: %v", day, err)
		}
		clock = clock.AddDate(0, 0, 1)
	}
	backups, err := store.ListLocal(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) < RetentionDays || len(backups) > RetentionDays+1 {
		t.Fatalf("retained %d backups, want at least %d daily backups", len(backups), RetentionDays)
	}
	for _, backup := range backups {
		if backup.AccountID != 7 || backup.CreatedAt.IsZero() || backup.ExpiresAt.Before(backup.CreatedAt) {
			t.Fatalf("unexpected safe metadata: %#v", backup)
		}
	}
	restored, err := store.LoadLocal(7, backups[0].CreatedAt)
	if err != nil || restored.Cookies[0].Value != "secret-session-value" {
		t.Fatalf("local restore err=%v snapshot=%+v", err, restored.Account)
	}
}

func TestRejectsOversizedOrUnsafeSnapshots(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	invalid := testSnapshot()
	invalid.Account.PlatformCode = ""
	if _, err := store.EncryptPortable(invalid, "correct horse battery staple"); !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("invalid snapshot error = %v, want ErrInvalidSnapshot", err)
	}
	if _, err := store.DecryptPortable(bytes.Repeat([]byte("x"), MaxArchiveBytes+1), "correct horse battery staple"); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("large archive error = %v, want ErrArchiveTooLarge", err)
	}
}
