package tikhub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// WatchUser is a monitored creator (TikHub user_id).
type WatchUser struct {
	Platform     string `json:"platform"` // xhs | douyin
	UserID       string `json:"user_id"`
	Name         string `json:"name,omitempty"`
	Note         string `json:"note,omitempty"`
	AddedAt      string `json:"added_at"`
	LastCheck    string `json:"last_check,omitempty"`
	LastCount    int    `json:"last_count,omitempty"`
	LookbackDays int    `json:"lookback_days,omitempty"`
	PageSize     int    `json:"page_size,omitempty"`
	MaxPages     int    `json:"max_pages,omitempty"`
	SortType     int    `json:"sort_type,omitempty"`
	// AutoArchive opts this creator into the background worker's incremental
	// archive (at most once per 24h, latest pages only). It defaults to false:
	// watchlists written before this field existed decode to false and never
	// spend TikHub credit until the user turns it on explicitly.
	AutoArchive bool `json:"auto_archive,omitempty"`
}

type watchFile struct {
	Users []WatchUser `json:"users"`
}

var watchMu sync.Mutex

func watchPath(dataDir string) string {
	return filepath.Join(dataDir, "tikhub", "watchlist.json")
}

func LoadWatchlist(dataDir string) ([]WatchUser, error) {
	watchMu.Lock()
	defer watchMu.Unlock()
	return loadWatchlistLocked(dataDir)
}

func loadWatchlistLocked(dataDir string) ([]WatchUser, error) {
	b, err := os.ReadFile(watchPath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f watchFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	for i := range f.Users {
		applyWatchDefaults(&f.Users[i])
	}
	return f.Users, nil
}

func SaveWatchlist(dataDir string, users []WatchUser) error {
	watchMu.Lock()
	defer watchMu.Unlock()
	return saveWatchlistLocked(dataDir, users)
}

func saveWatchlistLocked(dataDir string, users []WatchUser) error {
	dir := filepath.Join(dataDir, "tikhub")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(watchFile{Users: users}, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".watchlist-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(b); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, watchPath(dataDir))
}

func AddWatch(dataDir, platform, userID, name, note string) error {
	users, err := LoadWatchlist(dataDir)
	if err != nil {
		return err
	}
	for _, u := range users {
		if u.Platform == platform && u.UserID == userID {
			_, err := EnsureCreatorFolder(dataDir, u)
			return err
		}
	}
	next := WatchUser{
		Platform: platform, UserID: userID, Name: name, Note: note,
		AddedAt:      time.Now().UTC().Format(time.RFC3339),
		LookbackDays: 30, PageSize: 20, MaxPages: 3, SortType: 0,
	}
	if _, err := EnsureCreatorFolder(dataDir, next); err != nil {
		return err
	}
	users = append(users, next)
	if err := SaveWatchlist(dataDir, users); err != nil {
		return err
	}
	return nil
}

func UpdateWatch(dataDir string, next WatchUser) error {
	applyWatchDefaults(&next)
	if !validWatchOptions(next) {
		return os.ErrInvalid
	}
	users, err := LoadWatchlist(dataDir)
	if err != nil {
		return err
	}
	found := false
	for i := range users {
		if users[i].Platform != next.Platform || users[i].UserID != next.UserID {
			continue
		}
		next.AddedAt = users[i].AddedAt
		next.LastCheck = users[i].LastCheck
		next.LastCount = users[i].LastCount
		// 自动归档开关不属于「抓取参数」表单，编辑参数时必须保留，否则用户
		// 每改一次页数就会把开关悄悄关掉。
		next.AutoArchive = users[i].AutoArchive
		users[i] = next
		found = true
		break
	}
	if !found {
		return os.ErrNotExist
	}
	if err := SaveWatchlist(dataDir, users); err != nil {
		return err
	}
	_, err = EnsureCreatorFolder(dataDir, next)
	return err
}

// SetWatchAutoArchive flips the unattended incremental-archive switch for one
// monitored creator. Load-modify-save runs under watchMu so a concurrent
// UpdateWatch cannot drop the toggle (or resurrect a removed creator) between
// our read and our write.
func SetWatchAutoArchive(dataDir, platform, userID string, enabled bool) (WatchUser, error) {
	watchMu.Lock()
	defer watchMu.Unlock()
	users, err := loadWatchlistLocked(dataDir)
	if err != nil {
		return WatchUser{}, err
	}
	for i := range users {
		if users[i].Platform != platform || users[i].UserID != userID {
			continue
		}
		users[i].AutoArchive = enabled
		if err := saveWatchlistLocked(dataDir, users); err != nil {
			return WatchUser{}, err
		}
		return users[i], nil
	}
	return WatchUser{}, os.ErrNotExist
}

func RemoveWatch(dataDir, platform, userID string) error {
	users, err := LoadWatchlist(dataDir)
	if err != nil {
		return err
	}
	out := users[:0]
	for _, u := range users {
		if u.Platform == platform && u.UserID == userID {
			continue
		}
		out = append(out, u)
	}
	return SaveWatchlist(dataDir, out)
}

func UpdateWatchStats(dataDir, platform, userID string, count int) error {
	users, err := LoadWatchlist(dataDir)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range users {
		if users[i].Platform == platform && users[i].UserID == userID {
			users[i].LastCheck = now
			users[i].LastCount = count
		}
	}
	return SaveWatchlist(dataDir, users)
}

func applyWatchDefaults(user *WatchUser) {
	if user.LookbackDays == 0 {
		user.LookbackDays = 30
	}
	if user.PageSize == 0 {
		user.PageSize = 20
	}
	if user.MaxPages == 0 {
		user.MaxPages = 3
	}
}

func validWatchOptions(user WatchUser) bool {
	validLookback := user.LookbackDays == 7 || user.LookbackDays == 14 || user.LookbackDays == 30
	validPages := user.MaxPages == 1 || user.MaxPages == 2 || user.MaxPages == 3 || user.MaxPages == 5
	return validLookback && user.PageSize == 20 && validPages && (user.SortType == 0 || user.SortType == 1)
}
