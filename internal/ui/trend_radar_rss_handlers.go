package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/radar"
)

func (s *Server) handleAPITrendRadarRSSSources(w http.ResponseWriter, r *http.Request) {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, "RSS data directory unavailable", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet {
		file, err := radar.LoadRSSSources(dataDir)
		if err != nil {
			http.Error(w, "RSS sources unavailable", http.StatusInternalServerError)
			return
		}
		runs, _ := radar.ListRSSRuns(dataDir)
		writeJSON(w, map[string]any{"sources": file.Sources, "runs": runs})
		return
	}
	if !sameOriginLoopbackRequest(r) {
		http.Error(w, "RSS source update denied", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodDelete {
		id := strings.TrimSpace(r.PathValue("id"))
		file, err := radar.LoadRSSSources(dataDir)
		if err != nil {
			http.Error(w, "RSS sources unavailable", http.StatusInternalServerError)
			return
		}
		next := file.Sources[:0]
		found := false
		for _, source := range file.Sources {
			if source.ID == id {
				found = true
				continue
			}
			next = append(next, source)
		}
		if !found {
			http.Error(w, "RSS source not found", http.StatusNotFound)
			return
		}
		file.Sources = next
		if err := radar.SaveRSSSources(dataDir, file); err != nil {
			http.Error(w, "RSS source save failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "history_preserved": true})
		return
	}
	var source radar.RSSSource
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil {
		http.Error(w, "invalid RSS source", http.StatusBadRequest)
		return
	}
	source, err = radar.ValidateRSSSource(source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, err := radar.LoadRSSSources(dataDir)
	if err != nil {
		http.Error(w, "RSS sources unavailable", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if r.Method == http.MethodPost {
		source.ID = radar.NewScheduleID()
		source.CreatedAt = now
		source.UpdatedAt = now
		file.Sources = append(file.Sources, source)
	} else if r.Method == http.MethodPut {
		found := false
		for index := range file.Sources {
			if file.Sources[index].ID == source.ID && r.PathValue("id") == source.ID {
				source.CreatedAt = file.Sources[index].CreatedAt
				source.UpdatedAt = now
				file.Sources[index] = source
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "RSS source not found", http.StatusNotFound)
			return
		}
	} else {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := radar.SaveRSSSources(dataDir, file); err != nil {
		http.Error(w, "RSS source save failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, source)
}

func (s *Server) handleAPITrendRadarRSSCollect(w http.ResponseWriter, r *http.Request) {
	if !sameOriginLoopbackRequest(r) {
		http.Error(w, "RSS collection denied", http.StatusForbidden)
		return
	}
	var body struct {
		Confirm bool `json:"confirm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || !body.Confirm {
		http.Error(w, "explicit RSS collection confirmation required", http.StatusBadRequest)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, "RSS data directory unavailable", http.StatusInternalServerError)
		return
	}
	file, err := radar.LoadRSSSources(dataDir)
	if err != nil {
		http.Error(w, "RSS sources unavailable", http.StatusInternalServerError)
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var source *radar.RSSSource
	for index := range file.Sources {
		if file.Sources[index].ID == id {
			source = &file.Sources[index]
			break
		}
	}
	if source == nil || !source.Enabled {
		http.Error(w, "RSS source not found or disabled", http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	run, err := radar.FetchRSS(ctx, dataDir, *source)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, run)
		return
	}
	writeJSON(w, run)
}
