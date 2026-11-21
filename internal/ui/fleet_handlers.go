package ui

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/db"
)

type fleetRunResponse struct {
	ID          int64  `json:"id"`
	Operation   string `json:"operation"`
	Status      string `json:"status"`
	Total       int    `json:"total"`
	Pending     int    `json:"pending"`
	Running     int    `json:"running"`
	Success     int    `json:"success"`
	Failed      int    `json:"failed"`
	Skipped     int    `json:"skipped"`
	Concurrency int    `json:"concurrency"`
}

type fleetItemResponse struct {
	ID          int64  `json:"id"`
	AccountID   int64  `json:"account_id"`
	Status      string `json:"status"`
	CurrentStep string `json:"current_step"`
	ErrorCode   string `json:"error_code,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	FinishedAt  string `json:"finished_at,omitempty"`
}

// fleetInstanceResponse is deliberately a display-only runtime snapshot. CDP
// URLs, profile identifiers, process IDs, and lease owners stay server-side.
type fleetInstanceResponse struct {
	AccountID    int64  `json:"accountId"`
	AccountName  string `json:"accountName"`
	PlatformCode string `json:"platformCode"`
	State        string `json:"state"`
	CDPPort      int    `json:"cdpPort"`
	StartedAt    string `json:"startedAt,omitempty"`
	LastSeenAt   string `json:"lastSeenAt,omitempty"`
}

func fleetRunDTO(run db.BatchRun) fleetRunResponse {
	return fleetRunResponse{
		ID: run.ID, Operation: strings.TrimPrefix(run.OperationType, "fleet_"), Status: run.Status,
		Total: run.TotalItems, Pending: run.PendingCount, Running: run.RunningCount,
		Success: run.SuccessCount, Failed: run.FailedCount, Skipped: run.SkippedCount, Concurrency: run.Concurrency,
	}
}

// handleAPIFleetRuns exposes only durable Fleet state. It never returns CDP
// URLs, profile directories, cookies, or browser command-line details.
func (s *Server) handleAPIFleetRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.db.ListBatchRuns(100)
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "FLEET_LIST_FAILED")
		return
	}
	response := make([]fleetRunResponse, 0, len(runs))
	for _, run := range runs {
		if strings.HasPrefix(run.OperationType, "fleet_") {
			response = append(response, fleetRunDTO(run))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) handleAPIFleetRun(w http.ResponseWriter, r *http.Request) {
	idText := strings.TrimPrefix(r.URL.Path, "/api/fleet/runs/")
	runID, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || runID <= 0 || s.toolRuntime == nil || s.toolRuntime.Fleet == nil {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	detail, err := s.toolRuntime.Fleet.Detail(runID)
	if err != nil {
		writeToolsError(w, http.StatusNotFound, "FLEET_RUN_NOT_FOUND")
		return
	}
	items := make([]fleetItemResponse, 0, len(detail.Items))
	for _, item := range detail.Items {
		items = append(items, fleetItemResponse{ID: item.ID, AccountID: item.AccountID, Status: item.Status, CurrentStep: item.CurrentStep, ErrorCode: item.ErrorCode, StartedAt: item.StartedAt, FinishedAt: item.FinishedAt})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"run": fleetRunDTO(*detail.Run), "items": items})
}

// handleAPIFleetInstances is the safe CDP observatory data source. It is
// read-only and exposes only the account identity plus non-sensitive runtime
// state required to manage local browser windows.
func (s *Server) handleAPIFleetInstances(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.db.ListAccounts()
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "FLEET_INSTANCE_LIST_FAILED")
		return
	}
	instances := make([]fleetInstanceResponse, 0)
	if s.facade != nil {
		for _, account := range accounts {
			status, statusErr := s.facade.Status(account.ID)
			if statusErr != nil || status == nil || !status.Running {
				continue
			}
			instance := fleetInstanceResponse{
				AccountID: account.ID, AccountName: account.Name, PlatformCode: account.PlatformCode,
				State: string(status.State), CDPPort: status.CDPPort,
			}
			_ = s.db.QueryRow(`SELECT started_at, last_seen_at FROM browser_runtime_instances WHERE account_id=?`, account.ID).Scan(&instance.StartedAt, &instance.LastSeenAt)
			instances = append(instances, instance)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"instances": instances, "count": len(instances)})
}
