package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const lockFileName = ".lock"

type ProfileLock struct {
	profileDir string
	lockFile   *os.File
	locked     bool
}

func NewProfileLock(dataDir, profileName string) *ProfileLock {
	profileDir := filepath.Join(dataDir, "profiles", profileName)
	return &ProfileLock{
		profileDir: profileDir,
	}
}

func (l *ProfileLock) Acquire() error {
	if l.locked {
		return nil
	}
	if err := os.MkdirAll(l.profileDir, 0755); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}
	lockPath := filepath.Join(l.profileDir, lockFileName)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return fmt.Errorf("profile is already in use (lock held by another process): %w", err)
	}
	pid := os.Getpid()
	if _, err := f.WriteString(fmt.Sprintf("%d\n", pid)); err != nil {
		f.Close()
		return err
	}
	f.Sync()
	l.lockFile = f
	l.locked = true
	return nil
}

func (l *ProfileLock) Release() error {
	if !l.locked || l.lockFile == nil {
		return nil
	}
	syscall.Flock(int(l.lockFile.Fd()), syscall.LOCK_UN)
	l.lockFile.Close()
	l.locked = false
	return nil
}

func (l *ProfileLock) IsLocked() bool {
	return l.locked
}

func (l *ProfileLock) ProfileDir() string {
	return l.profileDir
}
