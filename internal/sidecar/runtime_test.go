package sidecar

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewSecretIs256Bit(t *testing.T) {
	first, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(first)
	if err != nil {
		t.Fatalf("secret is not hex: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("secret length = %d bytes, want 32", len(decoded))
	}
	if first == second {
		t.Fatal("two generated secrets must differ")
	}
}

func TestInstanceLockRejectsConcurrentOwnerAndRecoversAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "backend.json")
	first, err := AcquireInstanceLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireInstanceLock(path); !errors.Is(err, ErrInstanceLocked) {
		t.Fatalf("second lock error = %v, want %v", err, ErrInstanceLocked)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireInstanceLock(path)
	if err != nil {
		t.Fatalf("lock after close: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDescriptorAtomicWriteReadAndOwnedRemove(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "run", "backend.json")
	d := Descriptor{
		SchemaVersion: SchemaVersion,
		PID:           os.Getpid(),
		Port:          43210,
		StartedAt:     time.Now().UTC(),
		Version:       "test",
		Auth:          mustSecret(t),
		Nonce:         mustSecret(t),
	}
	if err := WriteDescriptor(path, d); err != nil {
		t.Fatal(err)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("runtime dir mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("descriptor mode = %o, want 600", got)
	}

	got, err := ReadDescriptor(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != d.PID || got.Port != d.Port || got.Auth != d.Auth || got.Nonce != d.Nonce {
		t.Fatalf("descriptor round trip mismatch: %#v", got)
	}

	if err := RemoveDescriptor(path, mustSecret(t)); !errors.Is(err, ErrDescriptorOwnership) {
		t.Fatalf("wrong nonce error = %v, want ErrDescriptorOwnership", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("wrong owner removed descriptor: %v", err)
	}
	if err := RemoveDescriptor(path, d.Nonce); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("descriptor still exists: %v", err)
	}
}

func TestDescriptorStale(t *testing.T) {
	now := time.Now().UTC()
	d := Descriptor{PID: 42, StartedAt: now.Add(-time.Minute)}
	if d.Stale(42, now, 2*time.Minute) {
		t.Fatal("matching recent descriptor should be live")
	}
	if !d.Stale(43, now, 2*time.Minute) {
		t.Fatal("PID mismatch should be stale")
	}
	if !d.Stale(42, now, 30*time.Second) {
		t.Fatal("old descriptor should be stale")
	}
	d.StartedAt = now.Add(2 * time.Minute)
	if !d.Stale(42, now, 30*time.Second) {
		t.Fatal("future descriptor should be stale")
	}
}

func TestReadDescriptorRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.json")
	link := filepath.Join(root, "backend.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ReadDescriptor(link); err == nil {
		t.Fatal("symlink descriptor was accepted")
	}
}

func mustSecret(t *testing.T) string {
	t.Helper()
	secret, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	return secret
}
