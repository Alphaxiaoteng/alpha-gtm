package terminal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/agentws"
)

// ChatSession is a local multi-turn conversation (OpenCode-like).
// Each session has its own sandbox subdir so CLI --continue stays isolated & fast.
type ChatSession struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Engine    string    `json:"engine"`
	Model     string    `json:"model,omitempty"`
	Effort    string    `json:"effort,omitempty"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at"`
	Messages  []ChatMsg `json:"messages"`
	Dir       string    `json:"dir,omitempty"` // absolute session folder
}

// ChatMsg is one UI message bubble.
type ChatMsg struct {
	ID        string   `json:"id"`
	Role      string   `json:"role"` // user | assistant | system
	Text      string   `json:"text"`
	Files     []string `json:"files,omitempty"` // relative attach paths
	Engine    string   `json:"engine,omitempty"`
	Model     string   `json:"model,omitempty"`
	Effort    string   `json:"effort,omitempty"`
	Duration  string   `json:"duration,omitempty"`
	Error     bool     `json:"error,omitempty"`
	CreatedAt string   `json:"created_at"`
}

func sessionsRoot(dataDir string) string {
	return filepath.Join(agentws.Path(dataDir), "sessions")
}

func sessionDir(dataDir, id string) string {
	return filepath.Join(sessionsRoot(dataDir), id)
}

func sessionMetaPath(dataDir, id string) string {
	return filepath.Join(sessionDir(dataDir, id), "session.json")
}

// EnsureSessionLayout creates sessions/ + works/ trees.
func EnsureSessionLayout(dataDir string) error {
	if _, err := agentws.Ensure(dataDir); err != nil {
		return err
	}
	for _, d := range []string{
		sessionsRoot(dataDir),
		agentws.WorksPath(dataDir),
		filepath.Join(agentws.WorksPath(dataDir), "_inbox"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	readme := filepath.Join(agentws.WorksPath(dataDir), "README.md")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		_ = os.WriteFile(readme, []byte(worksReadme), 0o644)
	}
	return nil
}

// ListChatSessions newest first.
func ListChatSessions(dataDir string) ([]ChatSession, error) {
	if err := EnsureSessionLayout(dataDir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(sessionsRoot(dataDir))
	if err != nil {
		return nil, err
	}
	out := make([]ChatSession, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := LoadChatSession(dataDir, e.Name())
		if err != nil {
			continue
		}
		// strip heavy messages for list view? keep last text only
		summary := *s
		if len(summary.Messages) > 0 {
			last := summary.Messages[len(summary.Messages)-1]
			summary.Messages = []ChatMsg{last}
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

// CreateChatSession makes a new session folder + meta.
func CreateChatSession(dataDir, title, engine, model, effort string) (*ChatSession, error) {
	if err := EnsureSessionLayout(dataDir); err != nil {
		return nil, err
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	dir := sessionDir(dataDir, id)
	if err := os.MkdirAll(filepath.Join(dir, "attaches"), 0o755); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(title) == "" {
		title = "新会话"
	}
	if engine == "" {
		engine = "opencode"
	}
	s := &ChatSession{
		ID:        id,
		Title:     title,
		Engine:    engine,
		Model:     model,
		Effort:    effort,
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  nil,
		Dir:       dir,
	}
	if err := saveSession(dataDir, s); err != nil {
		return nil, err
	}
	return s, nil
}

// LoadChatSession reads full session with messages.
func LoadChatSession(dataDir, id string) (*ChatSession, error) {
	b, err := os.ReadFile(sessionMetaPath(dataDir, id))
	if err != nil {
		return nil, err
	}
	var s ChatSession
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	s.Dir = sessionDir(dataDir, id)
	return &s, nil
}

// DeleteChatSession removes folder.
func DeleteChatSession(dataDir, id string) error {
	if id == "" || strings.Contains(id, "..") {
		return fmt.Errorf("bad session id")
	}
	return os.RemoveAll(sessionDir(dataDir, id))
}

func saveSession(dataDir string, s *ChatSession) error {
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sessionMetaPath(dataDir, s.ID), b, 0o644)
}

// AppendChatMessage persists a bubble.
func AppendChatMessage(dataDir, id string, msg ChatMsg) (*ChatSession, error) {
	s, err := LoadChatSession(dataDir, id)
	if err != nil {
		return nil, err
	}
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("m-%d", time.Now().UnixNano())
	}
	if msg.CreatedAt == "" {
		msg.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	s.Messages = append(s.Messages, msg)
	// auto title from first user message
	if s.Title == "新会话" || s.Title == "New session" {
		if msg.Role == "user" && strings.TrimSpace(msg.Text) != "" {
			t := strings.TrimSpace(msg.Text)
			if len([]rune(t)) > 28 {
				t = string([]rune(t)[:28]) + "…"
			}
			s.Title = t
		}
	}
	if err := saveSession(dataDir, s); err != nil {
		return nil, err
	}
	return s, nil
}

// UpdateSessionPrefs updates engine/model/effort.
func UpdateSessionPrefs(dataDir, id, engine, model, effort, title string) (*ChatSession, error) {
	s, err := LoadChatSession(dataDir, id)
	if err != nil {
		return nil, err
	}
	if engine != "" {
		s.Engine = engine
	}
	if model != "" {
		s.Model = model
	}
	if effort != "" {
		s.Effort = effort
	}
	if title != "" {
		s.Title = title
	}
	if err := saveSession(dataDir, s); err != nil {
		return nil, err
	}
	return s, nil
}

// SaveSessionAttach writes uploaded bytes into session attaches/.
func SaveSessionAttach(dataDir, sessionID, filename string, data []byte) (absPath string, rel string, err error) {
	if err := EnsureSessionLayout(dataDir); err != nil {
		return "", "", err
	}
	dir := filepath.Join(sessionDir(dataDir, sessionID), "attaches")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	base := filepath.Base(filename)
	base = strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, base)
	if base == "" || base == "." {
		base = "file.bin"
	}
	name := fmt.Sprintf("%d_%s", time.Now().UnixNano(), base)
	abs := filepath.Join(dir, name)
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return "", "", err
	}
	return abs, filepath.Join("attaches", name), nil
}

const worksReadme = `# Works — 批量作品素材

把要自动发布的内容放在这里。每个子文件夹是一条作品：

    works/2026-07-13_demo/
      video.mp4          # 或 image1.jpg …
      caption.txt        # 标题/正文
      tags.txt           # 可选话题
      meta.json          # 可选 { "platform": "instagram", "type": "reel" }

Agent 与批量发布会读取这些文件夹，不会改 App 源码。
拖入 _inbox/ 也可，再整理到正式子目录。
`
