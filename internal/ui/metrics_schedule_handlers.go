package ui

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleAPIMetricsScheduleGet(w http.ResponseWriter, _ *http.Request) {
	settings, err := s.db.MetricsCollectionScheduleSettings()
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "METRICS_SCHEDULE_UNAVAILABLE"})
		return
	}
	writeJSON(w, settings)
}

func (s *Server) handleAPIMetricsSchedulePut(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled       *bool `json:"enabled"`
		IntervalHours *int  `json:"interval_hours"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_REQUEST"})
		return
	}
	settings, err := s.db.MetricsCollectionScheduleSettings()
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "METRICS_SCHEDULE_UNAVAILABLE"})
		return
	}
	enabled := settings.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	interval := settings.IntervalHours
	if input.IntervalHours != nil {
		interval = *input.IntervalHours
	}
	updated, err := s.db.UpdateMetricsCollectionSchedule(enabled, interval)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if enabled {
		s.metricsScheduler.Wake()
	}
	writeJSON(w, updated)
}

// handleAPIMetricsScheduleRun triggers one sweep immediately, ignoring the
// interval cooldown (see Scheduler.RunNow). It still refuses when the schedule
// is disabled — that switch is the user's own decision.
func (s *Server) handleAPIMetricsScheduleRun(w http.ResponseWriter, r *http.Request) {
	result, err := s.metricsScheduler.RunNow(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, result)
}
