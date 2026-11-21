//go:build !legacy_chromium

// Package xhs retains read-only access to historical local exports in the
// EgoLite build. Live collection is handled by egolite.CreatorMetricsService.
package xhs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

func LoadFromFile(date, dataDir string) (*DayData, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, date+".json"))
	if err != nil {
		return nil, err
	}
	var data DayData
	if err := json.Unmarshal(raw, &data); err != nil {
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
	dates := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && filepath.Ext(name) == ".json" {
			dates = append(dates, name[:len(name)-len(filepath.Ext(name))])
		}
	}
	sort.Strings(dates)
	return dates, nil
}

func AggregateAccounts(dataDir string) ([]AccountCollection, error) {
	dates, err := ListDates(filepath.Join(dataDir, "xhs_data"))
	if err != nil {
		return nil, err
	}
	accounts := make(map[string]*AccountCollection)
	for _, date := range dates {
		data, readErr := LoadFromFile(date, filepath.Join(dataDir, "xhs_data"))
		if readErr != nil {
			continue
		}
		for _, account := range data.Accounts {
			key := account.Account
			if key == "" {
				key = fmt.Sprintf("account_%d", len(accounts)+1)
			}
			collection := accounts[key]
			if collection == nil {
				collection = &AccountCollection{AccountName: account.Account, PlatformCode: "xhs"}
				accounts[key] = collection
			}
			collection.CollectionCount++
			collection.LastCollected = account.CrawledAt
			collection.LastNotesCount = account.NotesCount
			collection.Notes = append(collection.Notes, account.Notes...)
		}
	}
	result := make([]AccountCollection, 0, len(accounts))
	for _, collection := range accounts {
		collection.Notes = uniqueNotes(collection.Notes)
		collection.TotalNotes = len(collection.Notes)
		result = append(result, *collection)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].CollectionCount > result[right].CollectionCount })
	return result, nil
}

func DashboardSummary(dataDir string) (*DashboardStats, error) {
	collections, err := AggregateAccounts(dataDir)
	if err != nil {
		return nil, err
	}
	stats := &DashboardStats{}
	platforms := map[string]*PlatformStat{}
	for _, collection := range collections {
		stats.TotalAccounts++
		stats.TotalNotes += collection.TotalNotes
		stats.TotalCollections += collection.CollectionCount
		code := collection.PlatformCode
		if code == "" {
			code = "xhs"
		}
		platform := platforms[code]
		if platform == nil {
			platform = &PlatformStat{PlatformCode: code}
			platforms[code] = platform
		}
		platform.Accounts++
		platform.TotalNotes += collection.TotalNotes
		platform.Collections += collection.CollectionCount
	}
	for _, platform := range platforms {
		stats.PlatformBreakdown = append(stats.PlatformBreakdown, *platform)
	}
	sort.Slice(stats.PlatformBreakdown, func(left, right int) bool {
		return stats.PlatformBreakdown[left].TotalNotes > stats.PlatformBreakdown[right].TotalNotes
	})
	return stats, nil
}

func GetAccountNotes(dataDir, accountName string) ([]Note, error) {
	collections, err := AggregateAccounts(dataDir)
	if err != nil {
		return nil, err
	}
	for _, collection := range collections {
		if collection.AccountName == accountName {
			return collection.Notes, nil
		}
	}
	return nil, nil
}

func uniqueNotes(notes []Note) []Note {
	seen := make(map[string]struct{}, len(notes))
	result := make([]Note, 0, len(notes))
	for _, note := range notes {
		key := note.NoteID
		if key == "" {
			key = note.Title + "\n" + note.PublishTime
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, note)
	}
	return result
}
