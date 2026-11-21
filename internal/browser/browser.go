//go:build legacy_chromium

package browser

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"alpha-nexus-egolite/internal/errors"
)

type BrowserManager struct {
	browserPath string
	dataDir     string
	cdpPort     int
	cmd         *exec.Cmd
	lock        *ProfileLock
	profileName string
}

type Status struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid,omitempty"`
	Profile string `json:"profile"`
	CDPURL  string `json:"cdp_url,omitempty"`
	CDPPort int    `json:"cdp_port"`
}

func NewManager(browserPath, dataDir string, cdpPort int) *BrowserManager {
	return &BrowserManager{
		browserPath: browserPath,
		dataDir:     dataDir,
		cdpPort:     cdpPort,
	}
}

func (m *BrowserManager) Start(ctx context.Context, profileName string) (*Status, error) {
	if m.browserPath == "" {
		return nil, fmt.Errorf("%s: browser path not set", errors.BROWSER_NOT_FOUND)
	}
	if _, err := os.Stat(m.browserPath); err != nil {
		return nil, fmt.Errorf("%s: %w", errors.BROWSER_NOT_FOUND, err)
	}

	m.profileName = profileName
	m.lock = NewProfileLock(m.dataDir, profileName)
	if err := m.lock.Acquire(); err != nil {
		return nil, fmt.Errorf("%s: %w", errors.PROFILE_LOCKED, err)
	}

	profileDir := m.lock.ProfileDir()
	cdpHost := "127.0.0.1"
	cdpURL := fmt.Sprintf("http://%s:%d", cdpHost, m.cdpPort)

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", m.cdpPort),
		fmt.Sprintf("--user-data-dir=%s", profileDir),
		"--remote-debugging-address=127.0.0.1",
		"--remote-allow-origins=*",
		"--no-first-run",
		"--no-default-browser-check",
	}

	cmd := exec.CommandContext(ctx, m.browserPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		m.lock.Release()
		return nil, fmt.Errorf("start browser: %w", err)
	}
	m.cmd = cmd

	if err := m.waitForCDP(ctx, cdpURL, 30*time.Second); err != nil {
		m.Stop()
		return nil, fmt.Errorf("%s: %w", errors.CDP_CONNECT, err)
	}

	return &Status{
		Running: true,
		PID:     cmd.Process.Pid,
		Profile: profileName,
		CDPURL:  cdpURL,
		CDPPort: m.cdpPort,
	}, nil
}

func (m *BrowserManager) Stop() error {
	if m.cmd != nil && m.cmd.Process != nil {
		syscall.Kill(-m.cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			m.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			syscall.Kill(-m.cmd.Process.Pid, syscall.SIGKILL)
		}
		m.cmd = nil
	}
	if m.lock != nil {
		m.lock.Release()
		m.lock = nil
	}
	return nil
}

func (m *BrowserManager) Status(profileName string) (*Status, error) {
	port := m.cdpPort
	cdpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	status := &Status{
		Running: false,
		Profile: profileName,
		CDPPort: port,
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return status, nil
	}
	conn.Close()

	resp, err := http.Get(cdpURL + "/json/version")
	if err != nil {
		return status, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		status.Running = true
		status.CDPURL = cdpURL
		status.CDPPort = port
	}
	return status, nil
}

func (m *BrowserManager) waitForCDP(ctx context.Context, url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("CDP did not become ready within %s", timeout)
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(m.cdpPort), 500*time.Millisecond)
			if err == nil {
				conn.Close()
				resp, err := http.Get(url + "/json/version")
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode == 200 {
						return nil
					}
				}
			}
		}
	}
}

func ProfileExists(dataDir, profileName string) bool {
	profileDir := filepath.Join(dataDir, "profiles", profileName)
	_, err := os.Stat(profileDir)
	return err == nil
}
