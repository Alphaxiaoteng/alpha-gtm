package ui

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/auth"
)

// authRequired is true when at least one app_user exists (demo: bootstrap then lock API).
func (s *Server) authRequired() bool {
	n, err := s.db.CountAppUsers()
	return err == nil && n > 0
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	n, _ := s.db.CountAppUsers()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"auth_required": n > 0,
		"user_count":    n,
		"message":       "When auth_required, send Authorization: Bearer <token> for /api/*",
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || body.Password == "" {
		http.Error(w, `{"error":"username and password required"}`, http.StatusBadRequest)
		return
	}
	u, err := s.db.GetAppUserByUsername(body.Username)
	if err != nil || !u.Enabled || !auth.CheckPassword(body.Password, u.PasswordHash) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials", "errorCode": "AUTH_FAILED"})
		return
	}
	secret, err := auth.LoadOrCreateSecret(s.db.DataDir)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	tok, exp, err := auth.IssueToken(secret, u.Username, u.Role, auth.TokenTTL)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":      tok,
		"expires_at": exp.UTC().Format(time.RFC3339),
		"username":   u.Username,
		"role":       u.Role,
	})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user := r.Header.Get("X-Auth-User")
	role := r.Header.Get("X-Auth-Role")
	if user == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"username": user, "role": role})
}

// withAuth wraps the mux: public auth endpoints + static; all other /api/* need Bearer when users exist.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// static assets and SPA
		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		// public API
		if path == "/api/auth/login" || path == "/api/auth/status" {
			next.ServeHTTP(w, r)
			return
		}
		// Private CLI MCP calls use a per-run broker token validated by their
		// dedicated handlers, never the user's long-lived app session.
		if strings.HasPrefix(path, "/api/agent/broker/") {
			next.ServeHTTP(w, r)
			return
		}
		if !s.authRequired() {
			next.ServeHTTP(w, r)
			return
		}
		// WebSocket clients cannot set Authorization easily — allow ?token=
		h := r.Header.Get("Authorization")
		token := ""
		if strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		} else if q := r.URL.Query().Get("token"); q != "" {
			token = q
		}
		if token == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing bearer token", "errorCode": "AUTH_REQUIRED"})
			return
		}
		secret, err := auth.LoadOrCreateSecret(s.db.DataDir)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		user, role, err := auth.ParseToken(secret, token)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error(), "errorCode": "AUTH_INVALID"})
			return
		}
		r.Header.Set("X-Auth-User", user)
		r.Header.Set("X-Auth-Role", role)
		next.ServeHTTP(w, r)
	})
}
