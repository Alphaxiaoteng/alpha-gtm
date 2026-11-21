// Package openbot discovers OpenBot installations on this machine so the Bot
// page can offer them without asking anyone for a port, a config path, or a
// second setup flow.
package openbot

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Kind names the OpenBot product. They share a name but not an API, so the
// discovery result always says which one was found.
type Kind string

const (
	// KindCopilotKit is the AI-coworker workspace (React app + Hono server).
	KindCopilotKit Kind = "copilotkit"
	// KindMeetOpenBot is the small local agent server (SSE on port 4132).
	KindMeetOpenBot Kind = "meetopenbot"
)

// Status is one discovery result. Installed and Running are independent: the
// desktop build can be installed but stopped, or a server can run from source
// without an app bundle.
type Status struct {
	Kind      Kind     `json:"kind"`
	Label     string   `json:"label"`
	Installed bool     `json:"installed"`
	Running   bool     `json:"running"`
	Endpoint  string   `json:"endpoint,omitempty"`
	Version   string   `json:"version,omitempty"`
	Evidence  []string `json:"evidence,omitempty"`
}

// Options exist for tests and for future user overrides. The zero value uses
// the documented default ports and the current user's home.
type Options struct {
	Home            string
	CopilotKitPort  int
	MeetOpenBotPort int
	Timeout         time.Duration
}

const (
	defaultCopilotKitPort  = 3001
	defaultMeetOpenBotPort = 4132
	defaultTimeout         = 800 * time.Millisecond
)

// Discover returns one status per known OpenBot product, always in a stable
// order. It never installs, starts or writes anything.
func Discover(ctx context.Context) []Status {
	return discover(ctx, Options{})
}

func discover(ctx context.Context, options Options) []Status {
	home := options.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	copilotKitPort := options.CopilotKitPort
	if copilotKitPort == 0 {
		copilotKitPort = defaultCopilotKitPort
	}
	meetPort := options.MeetOpenBotPort
	if meetPort == 0 {
		meetPort = defaultMeetOpenBotPort
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return []Status{
		discoverCopilotKit(ctx, home, copilotKitPort, timeout),
		discoverMeetOpenBot(ctx, home, meetPort, timeout),
	}
}

func discoverCopilotKit(ctx context.Context, home string, port int, timeout time.Duration) Status {
	status := Status{Kind: KindCopilotKit, Label: "OpenBot（CopilotKit）"}
	for _, candidate := range []string{
		filepath.Join(home, "Applications", "OpenBot.app"),
		"/Applications/OpenBot.app",
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			status.Installed = true
			status.Evidence = append(status.Evidence, candidate)
			break
		}
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !portOpen(ctx, port, timeout) {
		return status
	}
	status.Running = true
	status.Endpoint = base
	// /api/capabilities is the server's public capability document; unlike a
	// chat endpoint it has no side effects, so discovery stays read-only.
	version, ok := capabilitiesVersion(ctx, base+"/api/capabilities", timeout)
	if ok {
		status.Version = version
	}
	return status
}

func discoverMeetOpenBot(ctx context.Context, home string, port int, timeout time.Duration) Status {
	status := Status{Kind: KindMeetOpenBot, Label: "OpenBot（本地 agent 服务）"}
	// ~/.openbot holds channels, threads, agents and config for this product.
	configDir := filepath.Join(home, ".openbot")
	if info, err := os.Stat(configDir); err == nil && info.IsDir() {
		status.Installed = true
		status.Evidence = append(status.Evidence, configDir)
	}
	if portOpen(ctx, port, timeout) {
		status.Running = true
		status.Endpoint = fmt.Sprintf("http://127.0.0.1:%d", port)
	}
	return status
}

func portOpen(ctx context.Context, port int, timeout time.Duration) bool {
	if port <= 0 {
		return false
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func capabilitiesVersion(ctx context.Context, endpoint string, timeout time.Duration) (string, bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", false
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return "", false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", false
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(&limitedReader{reader: response.Body, remaining: 64 << 10}).Decode(&payload); err != nil {
		return "", false
	}
	return payload.Version, payload.Version != ""
}

// limitedReader keeps a misbehaving local server from making discovery read an
// unbounded body.
type limitedReader struct {
	reader    interface{ Read([]byte) (int, error) }
	remaining int64
}

func (r *limitedReader) Read(buffer []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, fmt.Errorf("openbot discovery response too large")
	}
	if int64(len(buffer)) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	read, err := r.reader.Read(buffer)
	r.remaining -= int64(read)
	return read, err
}
