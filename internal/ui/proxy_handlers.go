package ui

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// handleAPIProxies exposes metadata-only proxy pool management. It never
// probes an endpoint, fetches a rotating proxy, or accepts credentials.
func (s *Server) handleAPIProxies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		endpoints, err := s.db.ListProxyEndpoints()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeProxyJSON(w, http.StatusOK, endpoints)
	case http.MethodPost:
		var request struct {
			Name    string `json:"name"`
			Server  string `json:"server"`
			Enabled *bool  `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		enabled := true
		if request.Enabled != nil {
			enabled = *request.Enabled
		}
		endpoint, err := s.db.CreateProxyEndpoint(request.Name, request.Server, enabled)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeProxyJSON(w, http.StatusCreated, endpoint)
	default:
		http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIProxy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid proxy endpoint id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var request struct {
			Name    string `json:"name"`
			Server  string `json:"server"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		endpoint, err := s.db.UpdateProxyEndpoint(id, request.Name, request.Server, request.Enabled)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeProxyJSON(w, http.StatusOK, endpoint)
	case http.MethodDelete:
		if err := s.db.DeleteProxyEndpoint(id); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeProxyJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	default:
		http.Error(w, "PUT or DELETE only", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIProxyBind(w http.ResponseWriter, r *http.Request) {
	var request struct {
		AccountID       int64 `json:"accountId"`
		ProxyEndpointID int64 `json:"proxyEndpointId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.db.BindProxyEndpoint(request.AccountID, request.ProxyEndpointID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeProxyJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeProxyJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
