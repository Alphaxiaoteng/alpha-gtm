package radar

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// A Trend Radar run is one explicit, paid collection round for one niche and
// one platform. Each run is persisted as its own JSON file so the UI can show
// what the round cost and which calls failed, without adding a schema
// migration to the shared app database.
const (
	RunStateRunning   = "running"
	RunStateSucceeded = "succeeded"
	RunStatePartial   = "partial"
	RunStateFailed    = "failed"
)

var (
	runIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{7,63}$`)
	runStoreMu   sync.Mutex
)

type RunCall struct {
	CallID       int64  `json:"call_id,omitempty"`
	Term         string `json:"term"`
	Platform     string `json:"platform"`
	EndpointID   string `json:"endpoint_id"`
	Status       string `json:"status"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	ItemCount    int64  `json:"item_count"`
	KeptCount    int    `json:"kept_count"`
	// 平台明确标出来的卡：广告卡 / 引导卡；以及既不是作品也不是卡的「读不到」。
	// 三个数之和加作品数等于平台返回的结果列表条数（见 radarClassifyResultCards）。
	AdCount         int    `json:"ad_count,omitempty"`
	GuideCount      int    `json:"guide_count,omitempty"`
	UnreadableCount int    `json:"unreadable_count,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
	RawPath         string `json:"raw_path,omitempty"`
}

type Run struct {
	ID         string   `json:"id"`
	NicheID    string   `json:"niche_id"`
	NicheName  string   `json:"niche_name"`
	Platform   string   `json:"platform"`
	EndpointID string   `json:"endpoint_id"`
	Terms      []string `json:"terms"`
	State      string   `json:"state"`
	StartedAt  string   `json:"started_at"`
	FinishedAt string   `json:"finished_at,omitempty"`
	// LastRetryAt stamps the newest retry of failed calls. FinishedAt keeps the
	// round's original finish time so the first attempt's timeline stays intact.
	LastRetryAt      string    `json:"last_retry_at,omitempty"`
	ItemCap          int       `json:"item_cap"`
	MaxCalls         int       `json:"max_calls"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd"`
	CostBasis        string    `json:"cost_basis,omitempty"`
	Calls            []RunCall `json:"calls"`

	// Derived, persisted for cheap reading in the summary endpoint.
	TotalCalls int   `json:"total_calls"`
	Succeeded  int   `json:"succeeded"`
	Failed     int   `json:"failed"`
	Skipped    int   `json:"skipped"`
	Items      int64 `json:"items"`
	Kept       int   `json:"kept"`
	AdCards    int   `json:"ad_cards"`
	GuideCards int   `json:"guide_cards"`
	Unreadable int   `json:"unreadable"`
}

func RunDir(dataDir string) string {
	return filepath.Join(dataDir, "trend-radar", "runs")
}

func ValidRunID(value string) bool {
	return runIDPattern.MatchString(value)
}

func runPath(dataDir, runID string) (string, error) {
	if !ValidRunID(runID) {
		return "", fmt.Errorf("invalid trend radar run id")
	}
	return filepath.Join(RunDir(dataDir), runID+".json"), nil
}

// StartRun writes a new running record. It refuses to overwrite an existing run
// so a retried request cannot silently rewrite history.
func StartRun(dataDir string, run Run) (Run, error) {
	path, err := runPath(dataDir, run.ID)
	if err != nil {
		return Run{}, err
	}
	if run.StartedAt == "" {
		run.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	run.State = RunStateRunning
	if run.Calls == nil {
		run.Calls = []RunCall{}
	}
	run.recount()
	runStoreMu.Lock()
	defer runStoreMu.Unlock()
	if _, statErr := os.Stat(path); statErr == nil {
		return Run{}, fmt.Errorf("trend radar run already exists")
	}
	if err := os.MkdirAll(RunDir(dataDir), 0o700); err != nil {
		return Run{}, err
	}
	if err := os.Chmod(RunDir(dataDir), 0o700); err != nil {
		return Run{}, err
	}
	if err := writeRunFile(path, run); err != nil {
		return Run{}, err
	}
	return run, nil
}

// AppendRunCall records one finished paid call on the run.
func AppendRunCall(dataDir, runID string, call RunCall) (Run, error) {
	path, err := runPath(dataDir, runID)
	if err != nil {
		return Run{}, err
	}
	runStoreMu.Lock()
	defer runStoreMu.Unlock()
	run, err := readRunFile(path)
	if err != nil {
		return Run{}, err
	}
	run.Calls = append(run.Calls, call)
	run.recount()
	if err := writeRunFile(path, run); err != nil {
		return Run{}, err
	}
	return run, nil
}

// FinishRun seals a run and derives its final state.
func FinishRun(dataDir, runID string) (Run, error) {
	path, err := runPath(dataDir, runID)
	if err != nil {
		return Run{}, err
	}
	runStoreMu.Lock()
	defer runStoreMu.Unlock()
	run, err := readRunFile(path)
	if err != nil {
		return Run{}, err
	}
	run.recount()
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	run.State = run.deriveState()
	if err := writeRunFile(path, run); err != nil {
		return Run{}, err
	}
	return run, nil
}

// FinishRetry seals a round after a retry of its failed calls. The original
// FinishedAt is preserved; only the retry stamp, the derived state and the
// retry's own estimated cost are added.
func FinishRetry(dataDir, runID string, retryCostUSD float64) (Run, error) {
	path, err := runPath(dataDir, runID)
	if err != nil {
		return Run{}, err
	}
	runStoreMu.Lock()
	defer runStoreMu.Unlock()
	run, err := readRunFile(path)
	if err != nil {
		return Run{}, err
	}
	run.recount()
	run.LastRetryAt = time.Now().UTC().Format(time.RFC3339Nano)
	run.State = run.deriveState()
	run.EstimatedCostUSD += retryCostUSD
	if err := writeRunFile(path, run); err != nil {
		return Run{}, err
	}
	return run, nil
}

func LoadRun(dataDir, runID string) (Run, error) {
	path, err := runPath(dataDir, runID)
	if err != nil {
		return Run{}, err
	}
	runStoreMu.Lock()
	defer runStoreMu.Unlock()
	return readRunFile(path)
}

// LatestRunForNiche returns the most recently started run for a niche. A
// missing run is not an error.
func LatestRunForNiche(dataDir, nicheID string) (Run, bool, error) {
	runs, err := ListRuns(dataDir, 0)
	if err != nil {
		return Run{}, false, err
	}
	for _, run := range runs {
		if run.NicheID == nicheID {
			return run, true, nil
		}
	}
	return Run{}, false, nil
}

// ListRuns returns runs newest first. limit <= 0 means no limit.
func ListRuns(dataDir string, limit int) ([]Run, error) {
	entries, err := os.ReadDir(RunDir(dataDir))
	if os.IsNotExist(err) {
		return []Run{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	// File names are not chronological; read and sort by StartedAt.
	runs := make([]Run, 0, len(names))
	for _, name := range names {
		run, readErr := readRunFile(filepath.Join(RunDir(dataDir), name))
		if readErr != nil {
			continue
		}
		runs = append(runs, run)
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].StartedAt == runs[j].StartedAt {
			return runs[i].ID > runs[j].ID
		}
		return runs[i].StartedAt > runs[j].StartedAt
	})
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

func readRunFile(path string) (Run, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Run{}, err
	}
	var run Run
	if err := json.Unmarshal(raw, &run); err != nil {
		return Run{}, fmt.Errorf("decode trend radar run: %w", err)
	}
	run.recount()
	return run, nil
}

func writeRunFile(path string, run Run) error {
	raw, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".radar-run-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

// recount derives the round's totals. Succeeded/Failed/Skipped count terms, not
// attempts: the latest attempt of a term decides its outcome, so a failed call
// that a retry fixed stops counting as a failure while TotalCalls still reports
// every paid attempt the round made.
func (r *Run) recount() {
	r.TotalCalls = len(r.Calls)
	r.Items, r.Kept = 0, 0
	r.AdCards, r.GuideCards, r.Unreadable = 0, 0, 0
	lastStatus := map[string]string{}
	attempted := make([]string, 0, len(r.Calls))
	for _, call := range r.Calls {
		if _, seen := lastStatus[call.Term]; !seen {
			attempted = append(attempted, call.Term)
		}
		lastStatus[call.Term] = call.Status
		r.Items += call.ItemCount
		r.Kept += call.KeptCount
		r.AdCards += call.AdCount
		r.GuideCards += call.GuideCount
		r.Unreadable += call.UnreadableCount
	}
	r.Succeeded, r.Failed = 0, 0
	for _, term := range attempted {
		if lastStatus[term] == "succeeded" {
			r.Succeeded++
			continue
		}
		r.Failed++
	}
	r.Skipped = len(r.Terms) - len(attempted)
	if r.Skipped < 0 {
		r.Skipped = 0
	}
}

// PendingRetryTerms returns the terms whose latest attempt failed, in the
// round's own order. A term that already succeeded is never included, so a
// retry cannot repeat a paid call that worked.
func (r Run) PendingRetryTerms() []string {
	lastStatus := map[string]string{}
	for _, call := range r.Calls {
		lastStatus[call.Term] = call.Status
	}
	terms := make([]string, 0, len(r.Terms))
	for _, term := range r.Terms {
		if status, attempted := lastStatus[term]; attempted && status != "succeeded" {
			terms = append(terms, term)
		}
	}
	return terms
}

func (r Run) deriveState() string {
	switch {
	case r.TotalCalls == 0:
		return RunStateFailed
	case r.Failed == 0 && r.Skipped == 0:
		return RunStateSucceeded
	case r.Succeeded == 0:
		return RunStateFailed
	default:
		return RunStatePartial
	}
}
