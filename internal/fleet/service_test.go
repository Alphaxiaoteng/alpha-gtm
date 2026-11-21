//go:build legacy_chromium

package fleet

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/db"
)

type fakeBrowserController struct {
	mu      sync.Mutex
	started []int64
	stopped []int64
	status  map[int64]*browser.AccountStatus
}

type peakFleetController struct {
	mu      sync.Mutex
	current int
	peak    int
	entered chan struct{}
	release chan struct{}
}

func (f *peakFleetController) StartAccount(accountID int64, _ browser.StartOptions) (*browser.Instance, error) {
	f.mu.Lock()
	f.current++
	if f.current > f.peak {
		f.peak = f.current
	}
	f.mu.Unlock()
	f.entered <- struct{}{}
	<-f.release
	f.mu.Lock()
	f.current--
	f.mu.Unlock()
	return &browser.Instance{AccountID: accountID, ProfileID: browser.ProfileIDForAccount(accountID), State: browser.StateReady}, nil
}

func (f *peakFleetController) StopAccount(int64) error { return nil }

func (f *peakFleetController) Status(accountID int64) (*browser.AccountStatus, error) {
	return &browser.AccountStatus{AccountID: accountID, ProfileID: browser.ProfileIDForAccount(accountID), Running: true, State: browser.StateReady}, nil
}

func (f *peakFleetController) Peak() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.peak
}

func (f *fakeBrowserController) StartAccount(accountID int64, _ browser.StartOptions) (*browser.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, accountID)
	f.status[accountID] = &browser.AccountStatus{AccountID: accountID, ProfileID: browser.ProfileIDForAccount(accountID), Running: true, State: browser.StateReady}
	return &browser.Instance{AccountID: accountID, ProfileID: browser.ProfileIDForAccount(accountID), State: browser.StateReady}, nil
}

func (f *fakeBrowserController) StopAccount(accountID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, accountID)
	f.status[accountID] = &browser.AccountStatus{AccountID: accountID, ProfileID: browser.ProfileIDForAccount(accountID), State: browser.StateStopped}
	return nil
}

func (f *fakeBrowserController) Status(accountID int64) (*browser.AccountStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if current := f.status[accountID]; current != nil {
		copy := *current
		return &copy, nil
	}
	return &browser.AccountStatus{AccountID: accountID, ProfileID: browser.ProfileIDForAccount(accountID), State: browser.StateStopped}, nil
}

func setupFleetDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func createFleetAccounts(t *testing.T, database *db.DB, count int) []int64 {
	t.Helper()
	ids := make([]int64, 0, count)
	for i := 0; i < count; i++ {
		account, err := database.CreateAccount("xhs", "fleet-account", "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, account.ID)
	}
	return ids
}

func TestServiceRunStartCreatesDurableCompletedBatch(t *testing.T) {
	database := setupFleetDB(t)
	controller := &fakeBrowserController{status: map[int64]*browser.AccountStatus{}}
	service := NewService(database, controller)
	accounts := createFleetAccounts(t, database, 2)

	run, err := service.Create(RunRequest{Operation: OperationStart, AccountIDs: accounts, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetBatchRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "completed" || stored.SuccessCount != 2 {
		t.Fatalf("run=%+v", stored)
	}
	items, err := database.ListBatchItems(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Status != "success" || item.CurrentStep != "ready" {
			t.Fatalf("item=%+v", item)
		}
	}
}

func TestServiceObserveStopsAtManualTakeover(t *testing.T) {
	database := setupFleetDB(t)
	controller := &fakeBrowserController{status: map[int64]*browser.AccountStatus{}}
	service := NewService(database, controller)
	accounts := createFleetAccounts(t, database, 1)
	run, err := service.Create(RunRequest{Operation: OperationObserve, AccountIDs: accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetBatchRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "waiting_human" || stored.PendingCount != 1 {
		t.Fatalf("run=%+v", stored)
	}
	items, err := database.ListBatchItems(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != "waiting_human" || items[0].CurrentStep != "manual_takeover" {
		t.Fatalf("items=%+v", items)
	}
}

func TestServiceRejectsUnsafeConcurrency(t *testing.T) {
	database := setupFleetDB(t)
	service := NewService(database, &fakeBrowserController{status: map[int64]*browser.AccountStatus{}})
	accounts := createFleetAccounts(t, database, 1)
	if _, err := service.Create(RunRequest{Operation: OperationStart, AccountIDs: accounts, Concurrency: 6}); err == nil {
		t.Fatal("expected concurrency cap error")
	}
}

func TestServiceAllowsTenTargetsButRejectsEleven(t *testing.T) {
	database := setupFleetDB(t)
	service := NewService(database, &fakeBrowserController{status: map[int64]*browser.AccountStatus{}})
	accounts := createFleetAccounts(t, database, 11)
	if _, err := service.Create(RunRequest{Operation: OperationObserve, AccountIDs: accounts[:10], Concurrency: 5}); err != nil {
		t.Fatalf("ten targets should be allowed: %v", err)
	}
	if _, err := service.Create(RunRequest{Operation: OperationObserve, AccountIDs: accounts, Concurrency: 5}); err == nil {
		t.Fatal("expected target limit error")
	}
}

func TestServiceStartsTenTargetsWithAtMostFiveConcurrentActions(t *testing.T) {
	database := setupFleetDB(t)
	controller := &peakFleetController{entered: make(chan struct{}, 10), release: make(chan struct{})}
	service := NewService(database, controller)
	accounts := createFleetAccounts(t, database, 10)
	run, err := service.Create(RunRequest{Operation: OperationStart, AccountIDs: accounts, Concurrency: 5})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- service.Execute(context.Background(), run.ID) }()
	for i := 0; i < 5; i++ {
		select {
		case <-controller.entered:
		case <-time.After(time.Second):
			t.Fatalf("only %d start actions reached the controller", i)
		}
	}
	if peak := controller.Peak(); peak != 5 {
		t.Fatalf("peak concurrent start actions=%d want=5", peak)
	}
	close(controller.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stored, err := database.GetBatchRun(run.ID)
	if err != nil || stored.Status != "completed" || stored.SuccessCount != 10 {
		t.Fatalf("run=%+v err=%v", stored, err)
	}
}

func TestServiceStartsFiveLocalFakeCDPInstances(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	facade := browser.NewFacade(database, browser.FacadeConfig{
		BrowserPath:           writeFakeFleetBrowser(t, dataDir),
		DataDir:               dataDir,
		MaxConcurrentBrowsers: 5,
	})
	defer facade.Shutdown()
	accountIDs := make([]int64, 0, 5)
	for index := 0; index < 5; index++ {
		account, createErr := database.CreateAccount("xhs", fmt.Sprintf("fleet-cdp-%d", index), "")
		if createErr != nil {
			t.Fatal(createErr)
		}
		profileDir := filepath.Join(dataDir, "profiles", browser.ProfileIDForAccount(account.ID))
		if err := os.MkdirAll(profileDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := database.CreateBrowserProfile(account.ID, profileDir, index+1, "zh-CN", "zh-CN,zh", "Asia/Shanghai"); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`UPDATE browser_profiles SET last_cdp_port=0 WHERE account_id=?`, account.ID); err != nil {
			t.Fatal(err)
		}
		accountIDs = append(accountIDs, account.ID)
	}

	service := NewService(database, facade)
	run, err := service.Run(context.Background(), RunRequest{Operation: OperationStart, AccountIDs: accountIDs, Concurrency: 5})
	if err != nil || run.Status != "completed" || run.SuccessCount != 5 {
		t.Fatalf("start run=%+v err=%v", run, err)
	}
	instances := facade.ListRunning()
	if len(instances) != 5 {
		t.Fatalf("running instances=%d", len(instances))
	}
	ports := make(map[int]struct{}, len(instances))
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
	for _, instance := range instances {
		if instance.CDPPort < browser.PortMin || instance.CDPPort > browser.PortMax {
			t.Fatalf("invalid CDP port: %d", instance.CDPPort)
		}
		ports[instance.CDPPort] = struct{}{}
		response, getErr := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", instance.CDPPort))
		if getErr != nil || response.StatusCode != http.StatusOK {
			if response != nil {
				response.Body.Close()
			}
			t.Fatalf("local CDP port=%d response=%v err=%v", instance.CDPPort, response, getErr)
		}
		response.Body.Close()
	}
	if len(ports) != 5 {
		t.Fatalf("expected five distinct CDP ports, got %v", ports)
	}

	stopRun, err := service.Run(context.Background(), RunRequest{Operation: OperationStop, AccountIDs: accountIDs, Concurrency: 5})
	if err != nil || stopRun.Status != "completed" || stopRun.SuccessCount != 5 {
		t.Fatalf("stop run=%+v err=%v", stopRun, err)
	}
	if got := len(facade.ListRunning()); got != 0 {
		t.Fatalf("running instances after stop=%d", got)
	}
	var runtimeRows int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_runtime_instances`).Scan(&runtimeRows); err != nil || runtimeRows != 0 {
		t.Fatalf("runtime rows=%d err=%v", runtimeRows, err)
	}
}

func writeFakeFleetBrowser(t *testing.T, dataDir string) string {
	t.Helper()
	script := `#!/usr/bin/env bash
set -e
PORT=""
for arg in "$@"; do
  case "$arg" in --remote-debugging-port=*) PORT="${arg#--remote-debugging-port=}" ;; esac
done
test -n "$PORT"
python3 - "$PORT" <<'PY'
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        payload = b'[]' if self.path == "/json/list" else b'{"Browser":"fleet-fake","Protocol-Version":"1.3"}'
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
    def log_message(self, *args): pass
HTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()
PY
`
	path := filepath.Join(dataDir, "fleet-fake-browser.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServiceRunStopUsesOperationLeaseAndCompletes(t *testing.T) {
	database := setupFleetDB(t)
	controller := &fakeBrowserController{status: map[int64]*browser.AccountStatus{}}
	service := NewService(database, controller)
	accounts := createFleetAccounts(t, database, 1)
	run, err := service.Create(RunRequest{Operation: OperationStop, AccountIDs: accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if len(controller.stopped) != 1 || controller.stopped[0] != accounts[0] {
		t.Fatalf("stopped=%v", controller.stopped)
	}
	var leases int
	if err := database.QueryRow(`SELECT COUNT(*) FROM browser_operation_leases WHERE profile_id=?`, browser.ProfileIDForAccount(accounts[0])).Scan(&leases); err != nil {
		t.Fatal(err)
	}
	if leases != 0 {
		t.Fatalf("operation lease leaked: %d", leases)
	}
}

func TestServiceExecuteItemDoesNotOverwriteAnActiveClaim(t *testing.T) {
	database := setupFleetDB(t)
	service := NewService(database, &fakeBrowserController{status: map[int64]*browser.AccountStatus{}})
	accounts := createFleetAccounts(t, database, 1)
	run, err := service.Create(RunRequest{Operation: OperationStart, AccountIDs: accounts})
	if err != nil {
		t.Fatal(err)
	}
	items, err := database.ListBatchItems(run.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if claimed, err := database.ClaimPendingBatchItem(items[0].ID, "start"); err != nil || !claimed {
		t.Fatalf("claim=%t err=%v", claimed, err)
	}
	owner := "fleet:active:item:1"
	if acquired, err := database.AcquireBrowserOperationLease(browser.ProfileIDForAccount(accounts[0]), "start", owner, time.Now(), time.Minute); err != nil || !acquired {
		t.Fatalf("acquire=%t err=%v", acquired, err)
	}
	defer database.ReleaseBrowserOperationLease(browser.ProfileIDForAccount(accounts[0]), owner)

	service.executeItem(context.Background(), OperationStart, &items[0])
	items, err = database.ListBatchItems(run.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if items[0].Status != "running" {
		t.Fatalf("duplicate execute overwrote active claim: %+v", items[0])
	}
}
