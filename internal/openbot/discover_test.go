package openbot

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestDiscoverFindsNothingOnACleanMachine(t *testing.T) {
	results := discover(context.Background(), Options{
		Home:            t.TempDir(),
		CopilotKitPort:  closedPort(t),
		MeetOpenBotPort: closedPort(t),
		Timeout:         200 * time.Millisecond,
	})
	if len(results) != 2 {
		t.Fatalf("got %d results, want one per product", len(results))
	}
	for _, result := range results {
		if result.Installed || result.Running || result.Endpoint != "" {
			t.Fatalf("%s: expected a quiet result, got %+v", result.Kind, result)
		}
	}
}

func TestDiscoverReadsTheMeetOpenBotConfigDirectory(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".openbot", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	results := discover(context.Background(), Options{
		Home:            home,
		CopilotKitPort:  closedPort(t),
		MeetOpenBotPort: closedPort(t),
		Timeout:         200 * time.Millisecond,
	})
	meet := findKind(t, results, KindMeetOpenBot)
	if !meet.Installed || meet.Running {
		t.Fatalf("meetopenbot=%+v, want installed but stopped", meet)
	}
	if len(meet.Evidence) == 0 || meet.Evidence[0] != filepath.Join(home, ".openbot") {
		t.Fatalf("evidence=%v, want the config directory", meet.Evidence)
	}
	if copilot := findKind(t, results, KindCopilotKit); copilot.Installed {
		t.Fatalf("copilotkit=%+v, want not installed", copilot)
	}
}

func TestDiscoverReportsARunningCopilotKitWithItsVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capabilities" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"version":"1.4.2","features":["channels","computers"]}`)
	}))
	defer server.Close()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "Applications", "OpenBot.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	results := discover(context.Background(), Options{
		Home:            home,
		CopilotKitPort:  portOf(t, server.URL),
		MeetOpenBotPort: closedPort(t),
		Timeout:         time.Second,
	})
	copilot := findKind(t, results, KindCopilotKit)
	if !copilot.Installed || !copilot.Running {
		t.Fatalf("copilotkit=%+v, want installed and running", copilot)
	}
	if copilot.Version != "1.4.2" {
		t.Fatalf("version=%q, want 1.4.2", copilot.Version)
	}
	if copilot.Endpoint != server.URL {
		t.Fatalf("endpoint=%q, want %q", copilot.Endpoint, server.URL)
	}
}

func TestDiscoverKeepsRunningWhenCapabilitiesAreUnreadable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer server.Close()

	results := discover(context.Background(), Options{
		Home:            t.TempDir(),
		CopilotKitPort:  portOf(t, server.URL),
		MeetOpenBotPort: closedPort(t),
		Timeout:         time.Second,
	})
	copilot := findKind(t, results, KindCopilotKit)
	if !copilot.Running || copilot.Version != "" {
		t.Fatalf("copilotkit=%+v, want running without a version", copilot)
	}
}

func findKind(t *testing.T, results []Status, kind Kind) Status {
	t.Helper()
	for _, result := range results {
		if result.Kind == kind {
			return result
		}
	}
	t.Fatalf("missing %s in %+v", kind, results)
	return Status{}
}

func portOf(t *testing.T, rawURL string) int {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

// closedPort reserves a port and closes it, so a dial to it is refused.
func closedPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
