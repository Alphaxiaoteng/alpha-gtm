// Package sidecar manages the local desktop-to-Go runtime descriptor.
package sidecar

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const SchemaVersion = 1

var ErrDescriptorOwnership = errors.New("sidecar descriptor ownership mismatch")
var ErrInstanceLocked = errors.New("sidecar instance is already running")

type InstanceLock struct {
	file *os.File
}

// AcquireInstanceLock serializes desktop sidecars before they construct the
// application service. The kernel releases the lock if a sidecar crashes.
func AcquireInstanceLock(descriptorPath string) (*InstanceLock, error) {
	if descriptorPath == "" {
		return nil, errors.New("sidecar descriptor path is required")
	}
	dir := filepath.Dir(descriptorPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create sidecar runtime dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secure sidecar runtime dir: %w", err)
	}
	lockPath := descriptorPath + ".lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open sidecar instance lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure sidecar instance lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, ErrInstanceLocked
	}
	return &InstanceLock{file: file}, nil
}

func (l *InstanceLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	errUnlock := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	errClose := l.file.Close()
	l.file = nil
	return errors.Join(errUnlock, errClose)
}

// Descriptor is the private discovery contract between the desktop shell and
// the Go sidecar. Auth and Nonce are generated independently.
type Descriptor struct {
	SchemaVersion int       `json:"schema_version"`
	PID           int       `json:"pid"`
	Port          int       `json:"port"`
	StartedAt     time.Time `json:"started_at"`
	Version       string    `json:"version"`
	Auth          string    `json:"auth"`
	Nonce         string    `json:"nonce"`
}

// NewSecret returns a 256-bit random secret encoded as lowercase hex.
func NewSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate sidecar secret: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func (d Descriptor) Validate() error {
	if d.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported sidecar descriptor schema %d", d.SchemaVersion)
	}
	if d.PID <= 0 {
		return fmt.Errorf("invalid sidecar pid %d", d.PID)
	}
	if d.Port <= 0 || d.Port > 65535 {
		return fmt.Errorf("invalid sidecar port %d", d.Port)
	}
	if d.StartedAt.IsZero() {
		return errors.New("missing sidecar started_at")
	}
	if err := validateSecret("auth", d.Auth); err != nil {
		return err
	}
	if err := validateSecret("nonce", d.Nonce); err != nil {
		return err
	}
	return nil
}

func validateSecret(name, value string) error {
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != 32 {
		return fmt.Errorf("invalid sidecar %s", name)
	}
	return nil
}

// Stale checks descriptor ownership and age without trusting PID liveness.
// The supervising process additionally verifies child PID and /healthz nonce.
func (d Descriptor) Stale(expectedPID int, now time.Time, maxAge time.Duration) bool {
	if expectedPID > 0 && d.PID != expectedPID {
		return true
	}
	if d.StartedAt.IsZero() {
		return true
	}
	if d.StartedAt.After(now.Add(time.Minute)) {
		return true
	}
	return maxAge > 0 && now.Sub(d.StartedAt) > maxAge
}

// WriteDescriptor writes through a same-directory temporary file, fsyncs it,
// and atomically renames it into place.
func WriteDescriptor(path string, descriptor Descriptor) (err error) {
	if path == "" {
		return errors.New("sidecar descriptor path is required")
	}
	if err := descriptor.Validate(); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create sidecar runtime dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure sidecar runtime dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".backend-*.tmp")
	if err != nil {
		return fmt.Errorf("create sidecar descriptor temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("secure sidecar descriptor temp file: %w", err)
	}
	encoder := json.NewEncoder(tmp)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(descriptor); err != nil {
		return fmt.Errorf("encode sidecar descriptor: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync sidecar descriptor: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close sidecar descriptor: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install sidecar descriptor: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure sidecar descriptor: %w", err)
	}
	dirHandle, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open sidecar runtime dir for sync: %w", err)
	}
	if err := dirHandle.Sync(); err != nil {
		_ = dirHandle.Close()
		return fmt.Errorf("sync sidecar runtime dir: %w", err)
	}
	if err := dirHandle.Close(); err != nil {
		return fmt.Errorf("close sidecar runtime dir: %w", err)
	}
	return nil
}

func ReadDescriptor(path string) (Descriptor, error) {
	var descriptor Descriptor
	info, err := os.Lstat(path)
	if err != nil {
		return descriptor, err
	}
	if !info.Mode().IsRegular() {
		return descriptor, fmt.Errorf("sidecar descriptor is not a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return descriptor, fmt.Errorf("insecure sidecar descriptor permissions %o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return descriptor, err
	}
	if dirInfo.Mode().Perm()&0o077 != 0 {
		return descriptor, fmt.Errorf("insecure sidecar runtime directory permissions %o", dirInfo.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return descriptor, err
	}
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		return descriptor, fmt.Errorf("decode sidecar descriptor: %w", err)
	}
	if err := descriptor.Validate(); err != nil {
		return descriptor, err
	}
	return descriptor, nil
}

// RemoveDescriptor only removes the descriptor written by the matching nonce.
func RemoveDescriptor(path, expectedNonce string) error {
	descriptor, err := ReadDescriptor(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if expectedNonce == "" || !hmac.Equal([]byte(descriptor.Nonce), []byte(expectedNonce)) {
		return ErrDescriptorOwnership
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
