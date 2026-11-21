package ui

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"alpha-nexus-egolite/internal/terminal"
)

var (
	termOnce sync.Once
	termMgr  *terminal.Manager
)

func (s *Server) terminalManager() *terminal.Manager {
	termOnce.Do(func() {
		dir, _ := s.cfg.ResolvedDataDir()
		// prefer project cwd when running from repo; else data dir
		wd, _ := os.Getwd()
		if wd == "" {
			wd = dir
		}
		// if cwd is not the alpha-nexus project, still fine
		termMgr = terminal.NewManager(dir, wd)
	})
	return termMgr
}

func (s *Server) requireLocalTerminal(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequest(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "terminal API is localhost-only", "errorCode": "TERMINAL_LOCALHOST_ONLY",
		})
		return false
	}
	return true
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func terminalProbeForUI(raw map[string]any) map[string]any {
	probe := make(map[string]any, len(raw))
	for key, value := range raw {
		probe[key] = value
	}
	openCode := map[string]any{}
	if current, ok := raw["opencode"].(map[string]any); ok {
		for key, value := range current {
			openCode[key] = value
		}
	}
	binary, _ := openCode["binary"].(string)
	openCode["available"] = false
	openCode["ready"] = false
	if strings.TrimSpace(binary) == "" {
		openCode["availability"] = openCodeNotFound
		openCode["note"] = "OpenCode not found"
	} else {
		openCode["availability"] = openCodeAvailableUnverified
		openCode["note"] = "Binary found; brokered read-only smoke unverified"
	}
	probe["opencode"] = openCode
	return probe
}

func rejectUnbrokeredTerminalKind(kind string) (string, bool) {
	if strings.EqualFold(strings.TrimSpace(kind), string(terminal.KindOpenCode)) {
		return "OPENCODE_TUI_DISABLED", true
	}
	return "", false
}

func (s *Server) handleAPITerminalProbe(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"probe":       terminalProbeForUI(terminal.Probe()),
		"server_time": time.Now().UTC().Format(time.RFC3339),
		"work_dir":    s.terminalManager().WorkDir,
		"data_dir":    s.terminalManager().DataDir,
	})
}

func (s *Server) handleAPITerminalSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		m := s.terminalManager()
		list := m.List()
		type row struct {
			ID        string    `json:"id"`
			Kind      string    `json:"kind"`
			CmdLine   string    `json:"cmd_line"`
			Binary    string    `json:"binary"`
			WorkDir   string    `json:"work_dir"`
			StartedAt time.Time `json:"started_at"`
			Alive     bool      `json:"alive"`
		}
		out := make([]row, 0, len(list))
		for _, sess := range list {
			out = append(out, row{
				ID: sess.ID, Kind: string(sess.Kind), CmdLine: sess.CmdLine,
				Binary: sess.Binary, WorkDir: sess.WorkDir, StartedAt: sess.StartedAt, Alive: sess.Alive,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"sessions": out})
	case http.MethodPost:
		var body struct {
			Kind      string   `json:"kind"` // shell | claude | opencode
			WorkDir   string   `json:"work_dir"`
			ExtraArgs []string `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Kind == "" {
			body.Kind = "shell"
		}
		body.Kind = strings.ToLower(strings.TrimSpace(body.Kind))
		if errorCode, rejected := rejectUnbrokeredTerminalKind(body.Kind); rejected {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":        false,
				"error":     "OpenCode TUI is disabled until the brokered read-only integration is verified",
				"errorCode": errorCode,
			})
			return
		}
		// Coding agents only for UI (shell disabled for end users)
		switch body.Kind {
		case "claude":
		case "shell":
			http.Error(w, "shell disabled — use opencode or claude", http.StatusBadRequest)
			return
		default:
			http.Error(w, "kind must be opencode|claude", http.StatusBadRequest)
			return
		}
		body.ExtraArgs = nil
		m := s.terminalManager()
		// default workdir: project root if present under cwd
		wd := body.WorkDir
		if wd == "" {
			wd = m.WorkDir
			if _, err := os.Stat(filepath.Join(wd, "AGENTS.md")); err != nil {
				if dir, err := s.cfg.ResolvedDataDir(); err == nil && dir != "" {
					wd = dir
				}
			}
		}
		sess, err := m.Start(terminal.Kind(body.Kind), wd, body.ExtraArgs)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": false, "error": err.Error(),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"session": map[string]interface{}{
				"id": sess.ID, "kind": sess.Kind, "cmd_line": sess.CmdLine,
				"binary": sess.Binary, "work_dir": sess.WorkDir, "alive": sess.Alive,
			},
			"ws_path": "/api/terminal/ws?id=" + sess.ID,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPITerminalStop(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	err := s.terminalManager().Stop(body.ID)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

var termUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		o := r.Header.Get("Origin")
		if o == "" {
			// non-browser or same-origin tooling
			return isLoopbackRequest(r)
		}
		// allow only localhost UI origins
		return strings.Contains(o, "://127.0.0.1") ||
			strings.Contains(o, "://localhost") ||
			strings.Contains(o, "://[::1]")
	},
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// handleAPITerminalWS bridges PTY bytes <-> browser xterm (binary frames).
// Auth: Bearer header OR ?token= when app_users exist; always localhost-only.
func (s *Server) handleAPITerminalWS(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	sess := s.terminalManager().Get(id)
	if sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	conn, err := termUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// client -> process
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				_ = sess.Kill()
				return
			}
			if mt == websocket.TextMessage || mt == websocket.BinaryMessage {
				// text protocol control: {"type":"resize","cols":n,"rows":n} ignored for script PTY
				if len(data) > 0 && data[0] == '{' {
					var ctrl struct {
						Type string `json:"type"`
						Data string `json:"data"`
					}
					if json.Unmarshal(data, &ctrl) == nil {
						if ctrl.Type == "input" && ctrl.Data != "" {
							_, _ = sess.Write([]byte(ctrl.Data))
						}
						// resize: no-op for script-based PTY
						continue
					}
				}
				_, _ = sess.Write(data)
			}
		}
	}()

	// process -> client
	buf := make([]byte, 8192)
	for {
		select {
		case <-done:
			return
		default:
		}
		n, err := sess.ReadStdout(buf)
		if n > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
				_ = sess.Kill()
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n[session ended: "+err.Error()+"]\r\n"))
			} else {
				_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n[session ended]\r\n"))
			}
			return
		}
	}
}
