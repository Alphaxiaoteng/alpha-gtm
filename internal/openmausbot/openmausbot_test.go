package openmausbot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeHarness answers the two documented read endpoints. The payloads are
// trimmed copies of what server/routes/threads.ts actually returns.
func fakeHarness() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"app":"openmausbot","version":"0.1.160","pid":1}`))
	})
	mux.HandleFunc("/api/bots", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("messages") != "0" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"messages must be a non-negative whole number"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bots":[{"id":"b1","threadId":"t1","name":"DEV","title":"开发","section":"开发团队",
			"cwd":"/tmp/work","activity":"idle","approvalMode":"ask","soulHash":"abc","soul":"a whole standing prompt",
			"modelSelection":{"instanceId":"omp","model":"raccoon/kimi-k3"},"tasks":[{"threadId":"x"}]}],
			"botQueuedMessages":{},"sections":[],"groups":[],"computerControl":{}}`))
	})
	return httptest.NewServer(mux)
}

func TestProbeAndListBotsReadTheDocumentedEndpoints(t *testing.T) {
	server := fakeHarness()
	defer server.Close()
	client := Client{BaseURL: server.URL}

	probe, err := client.Probe(context.Background())
	if err != nil {
		t.Fatalf("probe err=%v", err)
	}
	if !probe.Available || !probe.Ready || probe.App != "openmausbot" || probe.Version != "0.1.160" {
		t.Fatalf("probe=%+v, want a ready openmausbot", probe)
	}

	bots, err := client.ListBots(context.Background())
	if err != nil {
		t.Fatalf("list err=%v", err)
	}
	if len(bots) != 1 || bots[0].Name != "DEV" || bots[0].ModelSelection.InstanceID != "omp" {
		t.Fatalf("bots=%+v, want DEV on omp", bots)
	}
	// A roster read must not drag another bot's standing prompt along.
	encoded, err := json.Marshal(bots[0])
	if err != nil {
		t.Fatalf("marshal err=%v", err)
	}
	if strings.Contains(string(encoded), `"soul"`) || strings.Contains(string(encoded), "standing prompt") {
		t.Fatalf("roster entry leaked the soul prompt: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"soulHash":"abc"`) {
		t.Fatalf("roster entry lost soulHash: %s", encoded)
	}
}

func TestListBotsSendsThePairedBearerToken(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bots":[]}`))
	}))
	defer server.Close()

	if _, err := (Client{BaseURL: server.URL, SessionToken: "omb_sess_test"}).ListBots(context.Background()); err != nil {
		t.Fatalf("list err=%v", err)
	}
	if seen != "Bearer omb_sess_test" {
		t.Fatalf("Authorization=%q, want the paired bearer", seen)
	}
}

func TestRefusalWithoutAPairingReportsAuthRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("This request must come from the desktop app or a paired device."))
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL}

	probe, err := client.Probe(context.Background())
	if err == nil {
		t.Fatal("probe succeeded against a refusing harness")
	}
	if probe.Ready {
		t.Fatalf("probe=%+v, want not ready", probe)
	}
	if probe.ErrorCode != CodeAuthRequired {
		t.Fatalf("probe code=%q, want %q", probe.ErrorCode, CodeAuthRequired)
	}

	_, listErr := client.ListBots(context.Background())
	if code := CodeOf(listErr, ""); code != CodeAuthRequired {
		t.Fatalf("list code=%q err=%v, want %q", code, listErr, CodeAuthRequired)
	}
	// The refusal text is quoted back, but the client's own token never is.
	if strings.Contains(listErr.Error(), "omb_sess_") {
		t.Fatalf("refusal leaked a session token: %v", listErr)
	}
}

func TestSlowHarnessReportsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte(`{"bots":[]}`))
	}))
	defer server.Close()

	_, err := (Client{BaseURL: server.URL, Timeout: 20 * time.Millisecond}).ListBots(context.Background())
	if code := CodeOf(err, ""); code != CodeTimeout {
		t.Fatalf("code=%q err=%v, want %q", code, err, CodeTimeout)
	}
}

func TestNonOpenMausBotIsNotAccepted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"app":"something-else","version":"1"}`))
	}))
	defer server.Close()

	probe, err := (Client{BaseURL: server.URL}).Probe(context.Background())
	if err == nil || probe.ErrorCode != CodeBadResponse || probe.Available {
		t.Fatalf("probe=%+v err=%v, want a bad-response refusal", probe, err)
	}
}

func TestUnreachableHarnessReportsUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	address := server.URL
	server.Close()

	_, err := (Client{BaseURL: address, Timeout: 200 * time.Millisecond}).ListBots(context.Background())
	if code := CodeOf(err, ""); code != CodeUnreachable {
		t.Fatalf("code=%q err=%v, want %q", code, err, CodeUnreachable)
	}
}

// TestLiveHarnessContract probes the harness actually running on this machine.
// It is opt-in because a machine without OpenMausBot must not fail the suite.
func TestLiveHarnessContract(t *testing.T) {
	if os.Getenv("OPENMAUSBOT_LIVE") == "" {
		t.Skip("set OPENMAUSBOT_LIVE=1 to probe the harness running on 127.0.0.1:8799")
	}
	client := Client{}
	probe, err := client.Probe(context.Background())
	if err != nil {
		t.Fatalf("probe err=%v (code=%s)", err, probe.ErrorCode)
	}
	if !probe.Ready || probe.App != "openmausbot" || probe.Version == "" {
		t.Fatalf("probe=%+v, want a ready openmausbot with a version", probe)
	}
	t.Logf("live harness %s version %s", probe.BaseURL, probe.Version)

	bots, err := client.ListBots(context.Background())
	if err != nil {
		t.Fatalf("list err=%v", err)
	}
	if len(bots) == 0 {
		t.Fatal("the live harness reported no bots")
	}
	for _, bot := range bots {
		if bot.ID == "" || bot.Name == "" {
			t.Fatalf("a live roster entry has no id or name: %+v", bot)
		}
	}
	t.Logf("live roster: %d bot(s), first=%s", len(bots), bots[0].Name)
}
