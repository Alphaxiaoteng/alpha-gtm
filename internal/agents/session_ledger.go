package agents

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type LedgerMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

type SessionLedger struct {
	mu       sync.RWMutex
	dir      string
	cache    map[string][]LedgerMessage
	maxCache int
}

var DefaultLedger = NewSessionLedger(200)

func NewSessionLedger(maxCache int) *SessionLedger {
	return &SessionLedger{
		cache:    make(map[string][]LedgerMessage),
		maxCache: maxCache,
	}
}

func (l *SessionLedger) Init(dataDir string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dir = filepath.Join(dataDir, "sessions")
	_ = os.MkdirAll(l.dir, 0755)
}

func (l *SessionLedger) Append(sessionID, role, content string) error {
	if sessionID == "" || content == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	msg := LedgerMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	}

	l.cache[sessionID] = append(l.cache[sessionID], msg)

	if l.dir == "" {
		return nil
	}

	filePath := filepath.Join(l.dir, sessionID+".jsonl")
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open ledger file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

func (l *SessionLedger) GetHistory(sessionID string) []LedgerMessage {
	if sessionID == "" {
		return nil
	}
	l.mu.RLock()
	if msgs, exists := l.cache[sessionID]; exists {
		result := make([]LedgerMessage, len(msgs))
		copy(result, msgs)
		l.mu.RUnlock()
		return result
	}
	l.mu.RUnlock()

	// Load from disk if not in cache
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.dir == "" {
		return nil
	}
	filePath := filepath.Join(l.dir, sessionID+".jsonl")
	f, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var history []LedgerMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var msg LedgerMessage
		if json.Unmarshal(scanner.Bytes(), &msg) == nil {
			history = append(history, msg)
		}
	}
	l.cache[sessionID] = history
	return history
}
