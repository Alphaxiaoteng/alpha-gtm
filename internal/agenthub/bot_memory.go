package agenthub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Bot memory is one plain Markdown file per persona under
// <dataDir>/bot-memory/<bot_id>.md. It is deliberately not a database table:
// the user must be able to read, diff, back up or delete the file by hand, and
// a bot's memory must survive the bot row being deleted.
const (
	botMemoryDirName    = "bot-memory"
	botMemoryFileMode   = 0600
	botMemoryDirMode    = 0700
	botMemoryMaxBytes   = 64 * 1024
	botMemoryPromptMax  = 4000
	botMemoryLineMax    = 200
	botMemoryEntryMax   = 3
	botMemoryIDMaxBytes = 80
)

// botMemorySections is the fixed section order of a memory file. Keeping it
// fixed means an append never reorders the user's document.
var botMemorySections = []string{"用户偏好", "品牌语气", "历史教训", "关键事实"}

const botMemoryEmptyEntry = "- （暂无）"

// ErrBotMemorySection rejects a section name that is not one of the four
// managed headings, so an append cannot silently create a stray heading.
var ErrBotMemorySection = errors.New("agent bot memory section is invalid")

// botMemoryPath validates the bot id before it is joined into a path. The id
// comes from the roster (newID("bot")) or from a URL, so a separator, a
// traversal segment or an absolute path must fail here, not in os.OpenFile.
func (s *Service) botMemoryPath(botID string) (string, error) {
	botID = strings.TrimSpace(botID)
	if !strings.HasPrefix(botID, "bot_") || len(botID) > botMemoryIDMaxBytes ||
		strings.ContainsAny(botID, `/\`) || strings.Contains(botID, "..") ||
		!safeActivityToken(botID) {
		return "", fmt.Errorf("%w: agent bot id is invalid", ErrBotInvalid)
	}
	return filepath.Join(s.dataDir, botMemoryDirName, botID+".md"), nil
}

// ReadBotMemory returns the stored Markdown, or an empty string when the bot
// has no memory yet. A missing file is a normal state, not an error.
func (s *Service) ReadBotMemory(_ context.Context, botID string) (string, error) {
	path, err := s.botMemoryPath(botID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read agent bot memory: %w", err)
	}
	return string(data), nil
}

// AppendBotMemory adds one bullet under section. The file is created (with the
// bot name in its title) when missing, and the write is idempotent: an entry
// that already appears anywhere in the file is skipped rather than duplicated.
func (s *Service) AppendBotMemory(ctx context.Context, botID, section, content string) error {
	path, err := s.botMemoryPath(botID)
	if err != nil {
		return err
	}
	section = normalizeBotMemorySection(section)
	if !knownBotMemorySection(section) {
		return ErrBotMemorySection
	}
	entry := botMemoryEntry(content)
	if entry == "" {
		return nil
	}
	existing, err := s.ReadBotMemory(ctx, botID)
	if err != nil {
		return err
	}
	if strings.Contains(existing, entry) {
		return nil
	}
	next := appendBotMemoryEntry(existing, section, entry, s.botMemoryTitle(ctx, botID))
	if len(next) > botMemoryMaxBytes {
		return fmt.Errorf("agent bot memory is full")
	}
	return s.writeBotMemoryFile(path, next)
}

// WriteBotMemory replaces the whole document. This backs the editable textarea
// in the Bot detail panel; an empty string clears the file.
func (s *Service) WriteBotMemory(_ context.Context, botID, content string) error {
	path, err := s.botMemoryPath(botID)
	if err != nil {
		return err
	}
	content = strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if content == "" {
		return s.ClearBotMemory(context.Background(), botID)
	}
	if len(content) > botMemoryMaxBytes {
		return fmt.Errorf("agent bot memory is too long")
	}
	for _, r := range content {
		if r == '\n' || r == '\t' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: agent bot memory contains control bytes", ErrBotInvalid)
		}
	}
	return s.writeBotMemoryFile(path, content+"\n")
}

// ClearBotMemory deletes the file. Bot deletion never calls this: memory is a
// recoverable record of what was learned, kept even when the persona is gone.
func (s *Service) ClearBotMemory(_ context.Context, botID string) error {
	path, err := s.botMemoryPath(botID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear agent bot memory: %w", err)
	}
	return nil
}

func (s *Service) writeBotMemoryFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, botMemoryDirMode); err != nil {
		return fmt.Errorf("prepare agent bot memory directory: %w", err)
	}
	// MkdirAll keeps an existing mode; pin it so an older directory created by
	// a different umask is still owner-only.
	if err := os.Chmod(dir, botMemoryDirMode); err != nil {
		return fmt.Errorf("secure agent bot memory directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), botMemoryFileMode); err != nil {
		return fmt.Errorf("write agent bot memory: %w", err)
	}
	// WriteFile only applies the mode when creating; chmod guarantees 0600 on
	// a file that already existed.
	if err := os.Chmod(path, botMemoryFileMode); err != nil {
		return fmt.Errorf("secure agent bot memory: %w", err)
	}
	return nil
}

// botMemoryTitle uses the stored persona name for the document heading and
// falls back to the id when the bot row is already gone.
func (s *Service) botMemoryTitle(ctx context.Context, botID string) string {
	bot, err := s.store.GetBot(ctx, botID)
	if err != nil || strings.TrimSpace(bot.Name) == "" {
		return botID
	}
	return bot.Name
}

func normalizeBotMemorySection(section string) string {
	section = strings.TrimSpace(section)
	section = strings.TrimSpace(strings.TrimPrefix(section, "##"))
	return section
}

func knownBotMemorySection(section string) bool {
	for _, known := range botMemorySections {
		if known == section {
			return true
		}
	}
	return false
}

// botMemoryEntry flattens one extracted sentence into a single bullet line:
// no newline can break the Markdown list, and the length is capped so a whole
// paragraph never lands in the file.
func botMemoryEntry(content string) string {
	content = strings.TrimSpace(content)
	content = strings.ReplaceAll(content, "\r", " ")
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.TrimSpace(strings.Join(strings.Fields(content), " "))
	content = strings.TrimPrefix(content, "-")
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if utf8.RuneCountInString(content) > botMemoryLineMax {
		runes := []rune(content)
		content = strings.TrimSpace(string(runes[:botMemoryLineMax])) + "…"
	}
	return content
}

func newBotMemoryDocument(title string) string {
	var builder strings.Builder
	builder.WriteString("# Bot 记忆：" + title + "\n")
	for _, section := range botMemorySections {
		builder.WriteString("\n## " + section + "\n")
		builder.WriteString(botMemoryEmptyEntry + "\n")
	}
	return builder.String()
}

func botMemorySectionRange(lines []string, section string) (int, int) {
	start := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == "## "+section {
			start = index
			break
		}
	}
	if start < 0 {
		return -1, -1
	}
	end := len(lines)
	for index := start + 1; index < len(lines); index++ {
		if strings.HasPrefix(strings.TrimSpace(lines[index]), "## ") {
			end = index
			break
		}
	}
	return start, end
}

// appendBotMemoryEntry rewrites the document in place: it guarantees the title
// and all four headings exist, drops the "（暂无）" placeholder of the target
// section and appends the new bullet as its last line.
func appendBotMemoryEntry(document, section, entry, title string) string {
	document = strings.TrimRight(strings.ReplaceAll(document, "\r\n", "\n"), "\n")
	lines := strings.Split(document, "\n")
	if document == "" {
		lines = strings.Split(strings.TrimRight(newBotMemoryDocument(title), "\n"), "\n")
	} else if !strings.HasPrefix(strings.TrimSpace(lines[0]), "# ") {
		// A hand-edited file without a title still gets one, and keeps its body.
		lines = append([]string{"# Bot 记忆：" + title, ""}, lines...)
	}
	for _, known := range botMemorySections {
		if start, _ := botMemorySectionRange(lines, known); start >= 0 {
			continue
		}
		lines = append(lines, "", "## "+known, botMemoryEmptyEntry)
	}
	start, end := botMemorySectionRange(lines, section)
	if start < 0 {
		return strings.Join(lines, "\n") + "\n"
	}
	body := make([]string, 0, end-start)
	for _, line := range lines[start+1 : end] {
		if strings.TrimSpace(line) == botMemoryEmptyEntry {
			continue
		}
		body = append(body, line)
	}
	for len(body) > 0 && strings.TrimSpace(body[0]) == "" {
		body = body[1:]
	}
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	rebuilt := append([]string{}, lines[:start+1]...)
	rebuilt = append(rebuilt, "")
	rebuilt = append(rebuilt, body...)
	rebuilt = append(rebuilt, "- "+entry)
	rest := lines[end:]
	if len(rest) > 0 {
		rebuilt = append(rebuilt, "")
	}
	rebuilt = append(rebuilt, rest...)
	return strings.Join(rebuilt, "\n") + "\n"
}

// botMemoryPromptBlock renders the stored memory for prompt injection. It is
// bounded so a huge file cannot push the provider prompt past its own cap.
func botMemoryPromptBlock(memory string) string {
	memory = strings.TrimSpace(memory)
	if memory == "" {
		return ""
	}
	if len(memory) > botMemoryPromptMax {
		cut := botMemoryPromptMax
		for cut > 0 && !utf8.RuneStart(memory[cut]) {
			cut--
		}
		memory = strings.TrimSpace(memory[:cut]) + "\n…（记忆已截断）"
	}
	return "【Bot 记忆】\n" + memory
}

// botMemoryPreferenceKeys route a matched sentence into 用户偏好; the recall
// keys route into 关键事实. Matching is plain substring search: no model call,
// no added latency and no new dependency.
var (
	botMemoryPreferenceKeys = []string{"偏好", "语气", "风格", "品牌", "禁用", "不要"}
	botMemoryRecallKeys     = []string{"记住", "下次", "以后", "教训"}
)

func botMemorySectionForSentence(sentence string) string {
	for _, key := range botMemoryPreferenceKeys {
		if strings.Contains(sentence, key) {
			return "用户偏好"
		}
	}
	for _, key := range botMemoryRecallKeys {
		if strings.Contains(sentence, key) {
			return "关键事实"
		}
	}
	return ""
}

// botMemorySentences splits a reply into sentences, keeping the terminator so
// the stored line reads like the original statement.
func botMemorySentences(text string) []string {
	sentences := make([]string, 0, 8)
	var builder strings.Builder
	flush := func() {
		if sentence := strings.TrimSpace(builder.String()); sentence != "" {
			sentences = append(sentences, sentence)
		}
		builder.Reset()
	}
	for _, r := range text {
		if r == '\n' || r == '\r' {
			flush()
			continue
		}
		builder.WriteRune(r)
		switch r {
		case '。', '！', '？', '；', '!', '?', ';':
			flush()
		}
	}
	flush()
	return sentences
}

// botMemoryCandidates picks the sentences of one assistant reply that are worth
// remembering. Markdown chrome (headings, tables, code fences) is skipped so a
// formatted answer does not store its own scaffolding.
func botMemoryCandidates(answer string) []botMemoryCandidate {
	candidates := make([]botMemoryCandidate, 0, botMemoryEntryMax)
	seen := make(map[string]struct{}, botMemoryEntryMax)
	for _, sentence := range botMemorySentences(answer) {
		if len(candidates) >= botMemoryEntryMax {
			break
		}
		trimmed := strings.TrimSpace(sentence)
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "|") ||
			strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, ">") {
			continue
		}
		section := botMemorySectionForSentence(trimmed)
		if section == "" {
			continue
		}
		entry := botMemoryEntry(trimmed)
		if utf8.RuneCountInString(entry) < 6 {
			continue
		}
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		candidates = append(candidates, botMemoryCandidate{Section: section, Entry: entry})
	}
	return candidates
}

type botMemoryCandidate struct {
	Section string
	Entry   string
}

// rememberBotConversation persists rule-matched sentences after a run. Every
// failure is ignored on purpose: memory is an enhancement, and losing it must
// never turn a completed answer into a failed run.
func (s *Service) rememberBotConversation(conversationID, answer string) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || strings.TrimSpace(answer) == "" {
		return
	}
	ctx := context.Background()
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil || conversation == nil || strings.TrimSpace(conversation.BotID) == "" {
		return
	}
	for _, candidate := range botMemoryCandidates(answer) {
		_ = s.AppendBotMemory(ctx, conversation.BotID, candidate.Section, candidate.Entry)
	}
}
