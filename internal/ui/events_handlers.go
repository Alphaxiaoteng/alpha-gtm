package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
)

const (
	eventReplayLimit       = 100
	eventPollInterval      = 500 * time.Millisecond
	eventHeartbeatInterval = 15 * time.Second
)

// handleAPIJobEventsStream replays durable job events and then tails SQLite.
// SQLite stays the truth source: reconnecting with Last-Event-ID cannot lose
// an event just because the HTTP process or an SSE connection restarted.
func (s *Server) handleAPIJobEventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeToolsError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		return
	}
	lastID, err := parseLastEventID(r.Header.Get("Last-Event-ID"))
	if err != nil {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	poll := time.NewTicker(eventPollInterval)
	defer poll.Stop()
	heartbeat := time.NewTicker(eventHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		events, err := s.db.ListJobEventsAfter(lastID, eventReplayLimit)
		if err != nil {
			return
		}
		for _, event := range events {
			if err := writeJobEventSSE(w, event); err != nil {
				return
			}
			lastID = event.ID
		}
		if len(events) > 0 {
			flusher.Flush()
		}

		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-poll.C:
		}
	}
}

func parseLastEventID(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 0 {
		return 0, fmt.Errorf("invalid event cursor")
	}
	return id, nil
}

func writeJobEventSSE(w http.ResponseWriter, event db.JobEvent) error {
	payload, err := json.Marshal(newPublishJobEventResponse(event))
	if err != nil {
		return err
	}
	// JobEvent does not carry user-controlled newlines in id/event name. The
	// JSON payload is one line, so standard SSE framing remains unambiguous.
	_, err = fmt.Fprintf(w, "id: %d\nevent: job_event\ndata: %s\n\n", event.ID, payload)
	return err
}
