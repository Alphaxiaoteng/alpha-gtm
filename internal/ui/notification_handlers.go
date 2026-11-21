package ui

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// handleAPINotifications serves the durable notification inbox. Notification
// creation is internal-only so clients cannot forge publish outcomes.
func (s *Server) handleAPINotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	unreadOnly := r.URL.Query().Get("unread") == "1"
	items, err := s.db.ListNotifications(limit, unreadOnly)
	if err != nil {
		http.Error(w, "list notifications failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

func (s *Server) handleAPINotificationRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "notification id required", http.StatusBadRequest)
		return
	}
	changed, err := s.db.MarkNotificationRead(id)
	if err != nil {
		http.Error(w, "mark notification failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"changed": changed})
}

func (s *Server) handleAPINotification(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "notification id required", http.StatusBadRequest)
		return
	}
	item, err := s.db.GetNotification(id)
	if err != nil {
		http.Error(w, "notification not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(item)
}

func (s *Server) handleAPIAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	changed, err := s.db.MarkAllNotificationsRead()
	if err != nil {
		http.Error(w, "mark notifications failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int64{"changed": changed})
}
