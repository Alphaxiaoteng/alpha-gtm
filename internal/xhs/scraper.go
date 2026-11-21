//go:build legacy_chromium

package xhs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/chromedp/chromedp"
)

type Note struct {
	Title        string `json:"title"`
	PublishTime  string `json:"publish_time"`
	ViewCount    string `json:"view_count"`
	LikeCount    string `json:"like_count"`
	CommentCount string `json:"comment_count"`
	CollectCount string `json:"collect_count"`
	ShareCount   string `json:"share_count"`
	NoteID       string `json:"note_id"`
	URL          string `json:"url"`
}

type AccountData struct {
	Account    string `json:"account"`
	CrawledAt  string `json:"crawled_at"`
	NotesCount int    `json:"notes_count"`
	Notes      []Note `json:"notes"`
}

type DayData struct {
	Date     string        `json:"date"`
	Accounts []AccountData `json:"accounts"`
}

// noteManagerURLs ordered by preference (aligned with project/xhs CDP capture).
var noteManagerURLs = []string{
	"https://creator.xiaohongshu.com/new/note-manager",
	"https://creator.xiaohongshu.com/portal/note/list",
	"https://creator.xiaohongshu.com/creator/home",
}

// scrapeNotesJS prefers structured .note cards (project/xhs), then legacy selectors.
const scrapeNotesJS = `(() => {
  const normalize = (v) => (v || '').replace(/\s+/g, ' ').trim();
  const items = [];

  // Style A: classic creator note list (project/xhs scrape_xhs_with_running_chrome)
  const classic = Array.from(document.querySelectorAll('.note'));
  if (classic.length > 0) {
    for (const note of classic) {
      const title = normalize(note.querySelector('.title')?.innerText);
      let publishTime = normalize(note.querySelector('.time')?.innerText).replace(/^发布于\s*/, '');
      const metrics = Array.from(note.querySelectorAll('.icon_list .icon span:last-child, .icon_list span'))
        .map((n) => normalize(n.innerText)).filter(Boolean);
      let noteId = '';
      const a = note.querySelector('a[href*="explore"], a[href*="note"]');
      if (a) {
        const m = (a.getAttribute('href') || '').match(/([a-f0-9]{16,})/i);
        if (m) noteId = m[1];
      }
      if (!title) continue;
      items.push({
        title, publishTime,
        viewCount: metrics[0] || '',
        commentCount: metrics[1] || '',
        likeCount: metrics[2] || '',
        collectCount: metrics[3] || '',
        shareCount: metrics[4] || '',
        noteId
      });
    }
  }

  // Style B: generic cards
  if (items.length === 0) {
    const cards = document.querySelectorAll('.note-item, .notes-list > div, [class*="noteItem"], [class*="note-card"], [class*="NoteItem"]');
    for (const card of cards) {
      const text = card.textContent || '';
      if (!text.trim()) continue;
      const lines = text.split('\n').map(l => l.trim()).filter(Boolean);
      if (lines.length < 2) continue;
      const title = lines[0] || '';
      if (title.length > 120 || title.length < 1) continue;
      let publishTime = '';
      const timeLine = lines.find(l => /发布于|发布时间|编辑于|\d{4}[-/]\d{1,2}/.test(l));
      if (timeLine) {
        const m = timeLine.match(/\d{4}[-/]\d{1,2}[-/]\d{1,2}/);
        if (m) publishTime = m[0];
      }
      const metrics = lines.map(l => l.replace(/,/g, '')).filter(l => /^\d{1,8}$/.test(l));
      let noteId = '';
      const link = card.querySelector('a[href*="explore"], a[href*="note"]');
      if (link) {
        const idMatch = (link.getAttribute('href') || '').match(/([a-f0-9]{16,})/i);
        if (idMatch) noteId = idMatch[1];
      }
      items.push({
        title, publishTime,
        viewCount: metrics[0] || '',
        likeCount: metrics[1] || '',
        commentCount: metrics[2] || '',
        collectCount: metrics[3] || '',
        shareCount: metrics[4] || '',
        noteId
      });
    }
  }
  return items;
})()`

func ScrapeNotes(ctx context.Context, cdpURL string) (*AccountData, error) {
	allocCtx, acancel := chromedp.NewRemoteAllocator(ctx, cdpURL)
	defer acancel()

	timeout := 120 * time.Second
	browserCtx, cancel := context.WithTimeout(allocCtx, timeout)
	defer cancel()

	browserCtx, cancel = chromedp.NewContext(browserCtx)
	defer cancel()

	result := &AccountData{
		CrawledAt: time.Now().Format(time.RFC3339),
	}

	var pageTitle string
	var navErr error
	for _, u := range noteManagerURLs {
		pageTitle = ""
		navErr = chromedp.Run(browserCtx,
			chromedp.Navigate(u),
			chromedp.Sleep(5*time.Second),
			chromedp.Title(&pageTitle),
		)
		if navErr != nil {
			continue
		}
		if contains(pageTitle, "登录") || contains(pageTitle, "login") {
			return nil, fmt.Errorf("not logged in (page title suggests login)")
		}
		// try extract; if notes found, stop
		var probe []map[string]interface{}
		_ = chromedp.Run(browserCtx, chromedp.Evaluate(scrapeNotesJS, &probe))
		if len(probe) > 0 {
			break
		}
	}
	if navErr != nil {
		return nil, fmt.Errorf("navigate: %w", navErr)
	}

	_ = chromedp.Run(browserCtx,
		chromedp.Evaluate(`(() => {
			let name = '';
			const el = document.querySelector('.user-name, .account-name, [class*="userName"], [class*="nickname"], [class*="user-info"]');
			if (el) name = el.textContent.trim();
			if (!name) {
				const text = document.body.innerText || '';
				const idx = text.indexOf('发布笔记');
				if (idx > 0) {
					const before = text.substring(0, idx).trim();
					const lines = before.split('\n').filter(l => l.trim());
					name = lines[lines.length-1] || '';
				}
			}
			return name.slice(0, 64);
		})()`, &result.Account),
	)

	var rawNotes []map[string]interface{}
	err := chromedp.Run(browserCtx, chromedp.Evaluate(scrapeNotesJS, &rawNotes))
	if err != nil {
		return nil, fmt.Errorf("parse notes: %w", err)
	}

	for _, rn := range rawNotes {
		note := Note{
			Title:        getString(rn, "title"),
			PublishTime:  getString(rn, "publishTime"),
			ViewCount:    getString(rn, "viewCount"),
			LikeCount:    getString(rn, "likeCount"),
			CommentCount: getString(rn, "commentCount"),
			CollectCount: getString(rn, "collectCount"),
			ShareCount:   getString(rn, "shareCount"),
			NoteID:       getString(rn, "noteId"),
		}
		if note.NoteID != "" {
			note.URL = fmt.Sprintf("https://www.xiaohongshu.com/explore/%s", note.NoteID)
		}
		if note.Title == "" {
			continue
		}
		result.Notes = append(result.Notes, note)
	}
	result.NotesCount = len(result.Notes)
	if result.Account == "" {
		result.Account = "unknown"
	}
	return result, nil
}

func ScrapeDailyStats(ctx context.Context, cdpURL string) (map[string]interface{}, error) {
	allocCtx, cancel := chromedp.NewRemoteAllocator(ctx, cdpURL)
	defer cancel()

	browserCtx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	browserCtx, cancel = context.WithTimeout(browserCtx, 30*time.Second)
	defer cancel()

	apiData := map[string]interface{}{
		"source": "stats_page",
	}

	err := chromedp.Run(browserCtx,
		chromedp.Navigate("https://creator.xiaohongshu.com/statistics/account/v2"),
		chromedp.Sleep(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("navigate to stats: %w", err)
	}

	var accountName string
	err = chromedp.Run(browserCtx,
		chromedp.Evaluate(`(() => {
			const el = document.querySelector('.user-name, .account-name, [class*="userName"], [class*="nickname"]');
			if (el) return el.textContent.trim();
			const text = document.body.innerText;
			const idx = text.indexOf('发布笔记');
			if (idx > 0) {
				const before = text.substring(0, idx).trim();
				const lines = before.split('\n').filter(l => l.trim());
				return lines[lines.length-1] || '';
			}
			return '';
		})()`, &accountName),
	)
	if err == nil && accountName != "" {
		apiData["account"] = accountName
	}

	return apiData, nil
}

func SaveToFile(data *DayData, dataDir string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	filePath := filepath.Join(dataDir, data.Date+".json")

	existing := &DayData{Date: data.Date}
	if rawData, err := os.ReadFile(filePath); err == nil {
		json.Unmarshal(rawData, existing)
	}

	for _, newAcc := range data.Accounts {
		found := false
		for i, existingAcc := range existing.Accounts {
			if existingAcc.Account == newAcc.Account {
				existing.Accounts[i] = newAcc
				found = true
				break
			}
		}
		if !found {
			existing.Accounts = append(existing.Accounts, newAcc)
		}
	}

	jsonData, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	return os.WriteFile(filePath, jsonData, 0644)
}

func LoadFromFile(date, dataDir string) (*DayData, error) {
	filePath := filepath.Join(dataDir, date+".json")
	rawData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var data DayData
	if err := json.Unmarshal(rawData, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func ListDates(dataDir string) ([]string, error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var dates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if len(name) > 5 && name[len(name)-5:] == ".json" {
			dates = append(dates, name[:len(name)-5])
		}
	}
	return dates, nil
}

func FilterHighEngagement(notes []Note, commentThreshold int) []Note {
	var result []Note
	for _, n := range notes {
		if parseInt(n.CommentCount) > commentThreshold {
			result = append(result, n)
		}
	}
	return result
}

// ---- Aggregation ----

type AccountCollection struct {
	AccountName     string `json:"account_name"`
	PlatformCode    string `json:"platform_code"`
	TotalNotes      int    `json:"total_notes"`
	LastCollected   string `json:"last_collected"`
	LastNotesCount  int    `json:"last_notes_count"`
	CollectionCount int    `json:"collection_count"`
	Notes           []Note `json:"notes"`
}

type DashboardStats struct {
	TotalAccounts     int            `json:"total_accounts"`
	TotalNotes        int            `json:"total_notes"`
	TotalCollections  int            `json:"total_collections"`
	PlatformBreakdown []PlatformStat `json:"platform_breakdown"`
}

type PlatformStat struct {
	PlatformCode string `json:"platform_code"`
	Accounts     int    `json:"accounts"`
	TotalNotes   int    `json:"total_notes"`
	Collections  int    `json:"collections"`
}

func AggregateAccounts(dataDir string) ([]AccountCollection, error) {
	xhsDir := filepath.Join(dataDir, "xhs_data")
	dates, err := ListDates(xhsDir)
	if err != nil {
		return nil, err
	}

	accMap := make(map[string]*AccountCollection)

	for _, d := range dates {
		data, err := LoadFromFile(d, xhsDir)
		if err != nil {
			continue
		}
		for _, a := range data.Accounts {
			key := a.Account
			if key == "" {
				key = fmt.Sprintf("acc_%d", len(accMap))
			}
			existing, ok := accMap[key]
			if !ok {
				existing = &AccountCollection{
					AccountName:  a.Account,
					PlatformCode: "xhs",
				}
				accMap[key] = existing
			}
			existing.CollectionCount++
			existing.LastCollected = a.CrawledAt
			existing.LastNotesCount = a.NotesCount
			existing.Notes = append(existing.Notes, a.Notes...)
		}
	}

	for _, acc := range accMap {
		seen := make(map[string]bool)
		unique := make([]Note, 0)
		for _, n := range acc.Notes {
			idKey := n.NoteID
			if idKey == "" {
				idKey = n.Title + n.PublishTime
			}
			if !seen[idKey] {
				seen[idKey] = true
				unique = append(unique, n)
			}
		}
		acc.Notes = unique
		acc.TotalNotes = len(unique)
	}

	result := make([]AccountCollection, 0, len(accMap))
	for _, acc := range accMap {
		result = append(result, *acc)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CollectionCount > result[j].CollectionCount
	})
	return result, nil
}

func DashboardSummary(dataDir string) (*DashboardStats, error) {
	collections, err := AggregateAccounts(dataDir)
	if err != nil {
		return nil, err
	}

	stats := &DashboardStats{}
	platMap := make(map[string]*PlatformStat)

	for _, c := range collections {
		stats.TotalAccounts++
		stats.TotalNotes += c.TotalNotes
		stats.TotalCollections += c.CollectionCount

		code := c.PlatformCode
		if code == "" {
			code = "xhs"
		}
		ps, ok := platMap[code]
		if !ok {
			ps = &PlatformStat{PlatformCode: code}
			platMap[code] = ps
		}
		ps.Accounts++
		ps.TotalNotes += c.TotalNotes
		ps.Collections += c.CollectionCount
	}

	for _, ps := range platMap {
		stats.PlatformBreakdown = append(stats.PlatformBreakdown, *ps)
	}
	sort.Slice(stats.PlatformBreakdown, func(i, j int) bool {
		return stats.PlatformBreakdown[i].TotalNotes > stats.PlatformBreakdown[j].TotalNotes
	})

	return stats, nil
}

func GetAccountNotes(dataDir, accountName string) ([]Note, error) {
	xhsDir := filepath.Join(dataDir, "xhs_data")
	dates, err := ListDates(xhsDir)
	if err != nil {
		return nil, err
	}

	var allNotes []Note
	seen := make(map[string]bool)

	for _, d := range dates {
		data, err := LoadFromFile(d, xhsDir)
		if err != nil {
			continue
		}
		for _, a := range data.Accounts {
			if a.Account != accountName {
				continue
			}
			for _, n := range a.Notes {
				idKey := n.NoteID
				if idKey == "" {
					idKey = n.Title + n.PublishTime
				}
				if !seen[idKey] {
					seen[idKey] = true
					allNotes = append(allNotes, n)
				}
			}
		}
	}
	return allNotes, nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
