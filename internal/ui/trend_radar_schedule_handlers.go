package ui

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/radar"
)

func (s *Server) handleAPITrendRadarSchedules(w http.ResponseWriter, r *http.Request) {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, "trend radar data directory unavailable", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet {
		file, err := radar.LoadSchedules(dataDir)
		if err != nil {
			http.Error(w, "trend radar schedules unavailable", http.StatusInternalServerError)
			return
		}
		writeJSON(w, file)
		return
	}
	if !sameOriginLoopbackRequest(r) {
		http.Error(w, "trend radar schedule update denied", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodDelete {
		id := strings.TrimSpace(r.PathValue("id"))
		found := false
		err := radar.UpdateSchedules(dataDir, func(file *radar.ScheduleFile) error {
			next := file.Schedules[:0]
			for _, schedule := range file.Schedules {
				if schedule.ID == id {
					found = true
					continue
				}
				next = append(next, schedule)
			}
			file.Schedules = next
			return nil
		})
		if err != nil {
			http.Error(w, "trend radar schedule save failed", http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "trend radar schedule not found", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "history_preserved": true})
		return
	}

	var body struct {
		Schedule radar.Schedule `json:"schedule"`
		Confirm  bool           `json:"confirm"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		http.Error(w, "invalid trend radar schedule", http.StatusBadRequest)
		return
	}
	if body.Schedule.Enabled && !body.Confirm {
		http.Error(w, "explicit confirmation is required to enable paid collection", http.StatusBadRequest)
		return
	}
	validated, err := radar.ValidateSchedule(body.Schedule)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if validated.Enabled {
		if err := s.installRadarWorker(r.Context(), dataDir); err != nil {
			http.Error(w, "trend radar background worker installation failed", http.StatusInternalServerError)
			return
		}
	}
	now := time.Now().UTC()
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status := http.StatusOK
	err = radar.UpdateSchedules(dataDir, func(file *radar.ScheduleFile) error {
		index := -1
		for i := range file.Schedules {
			if file.Schedules[i].ID == validated.ID && validated.ID != "" {
				index = i
				break
			}
		}
		if r.Method == http.MethodPost {
			if validated.ID != "" && index >= 0 {
				status = http.StatusConflict
				return nil
			}
			validated.ID = radar.NewScheduleID()
			validated.CreatedAt = now.Format(time.RFC3339Nano)
			validated.UpdatedAt = validated.CreatedAt
			if validated.Enabled {
				validated.NextRunAt = now.Add(time.Duration(validated.IntervalMinutes) * time.Minute).Format(time.RFC3339Nano)
			}
			file.Schedules = append(file.Schedules, validated)
			return nil
		}
		if index < 0 || r.PathValue("id") != validated.ID {
			status = http.StatusNotFound
			return nil
		}
		previous := file.Schedules[index]
		validated.CreatedAt = previous.CreatedAt
		validated.UpdatedAt = now.Format(time.RFC3339Nano)
		validated.LastRunAt = previous.LastRunAt
		validated.LastCallID = previous.LastCallID
		validated.LastStatus = previous.LastStatus
		validated.LastError = previous.LastError
		validated.NextRunAt = previous.NextRunAt
		if validated.Enabled && !previous.Enabled {
			validated.NextRunAt = now.Add(time.Duration(validated.IntervalMinutes) * time.Minute).Format(time.RFC3339Nano)
		}
		if !validated.Enabled {
			validated.NextRunAt = ""
		}
		file.Schedules[index] = validated
		return nil
	})
	if err != nil {
		http.Error(w, "trend radar schedule save failed", http.StatusInternalServerError)
		return
	}
	if status == http.StatusConflict {
		http.Error(w, "trend radar schedule already exists", status)
		return
	}
	if status == http.StatusNotFound {
		http.Error(w, "trend radar schedule not found", status)
		return
	}
	writeJSON(w, validated)
}

// handleAPITrendRadarRunDue runs one due pass of Trend Radar schedules.
// It is the user-triggered counterpart to the background LaunchAgent worker,
// so the UI can refresh local signals without waiting for the next tick.
func (s *Server) handleAPITrendRadarRunDue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !sameOriginLoopbackRequest(r) {
		http.Error(w, "trend radar worker denied", http.StatusForbidden)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, "trend radar data directory unavailable", http.StatusInternalServerError)
		return
	}
	summary, err := radar.RunDue(r.Context(), dataDir, time.Now())
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "RADAR_WORKER_FAILED", "message": err.Error()})
		return
	}
	writeJSON(w, map[string]any{
		"due":       summary.Due,
		"succeeded": summary.Succeeded,
		"failed":    summary.Failed,
		"call_ids":  summary.CallIDs,
	})
}
