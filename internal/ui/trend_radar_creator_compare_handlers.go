package ui

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/tikhub"
)

// creatorCompareDays are the only windows the comparison view offers. A smaller
// number would cut the real observation history (the store holds multi-day
// series per creator), a bigger one would mostly show empty days.
var creatorCompareDays = map[int]struct{}{7: {}, 30: {}, 90: {}}

// creatorCompareDay is one day in one creator's series. Followers is a pointer
// because "not collected" and a real zero are different facts: nil renders as
// null and the UI must not draw it as a value.
type creatorCompareDay struct {
	Date         string `json:"date"`
	Followers    *int64 `json:"followers"`
	Likes        int64  `json:"likes"`
	Collects     int64  `json:"collects"`
	Shares       int64  `json:"shares"`
	Comments     int64  `json:"comments"`
	Observations int    `json:"observations"`
}

type creatorCompareEntry struct {
	Key                   string              `json:"key"`
	Platform              string              `json:"platform"`
	Author                string              `json:"author"`
	Works                 int                 `json:"works"`
	Observations          int                 `json:"observations"`
	FollowerPoints        int                 `json:"follower_points"`
	InsufficientFollowers bool                `json:"insufficient_followers"`
	InsufficientSeries    bool                `json:"insufficient_series"`
	Days                  []creatorCompareDay `json:"days"`
}

type creatorCompareResponse struct {
	Window   creatorCompareWindow  `json:"window"`
	Creators []creatorCompareEntry `json:"creators"`
	Empty    bool                  `json:"empty"`
}

type creatorCompareWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
	Days int    `json:"days"`
}

// handleAPITrendRadarCreatorsCompare serves the creator comparison view: real
// radar observations per creator per day plus the follower points that exist as
// archived creator_profile snapshots. Everything it returns was actually
// collected; days without data are absent and flagged, never interpolated.
func (s *Server) handleAPITrendRadarCreatorsCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if s.db == nil {
		writeDataAPIError(w, http.StatusServiceUnavailable, "db_unavailable", "本地数据库未就绪")
		return
	}
	days := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeDataAPIError(w, http.StatusBadRequest, "invalid_days", "days 必须是 7、30 或 90")
			return
		}
		days = parsed
	}
	if _, ok := creatorCompareDays[days]; !ok {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_days", "days 必须是 7、30 或 90")
		return
	}

	// The store compares observed_at against its local calendar date, so the
	// window is built from local dates too; a UTC window would clip a day for
	// readers east of UTC.
	now := time.Now()
	toDate := now.Format("2006-01-02")
	fromDate := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")

	series, err := s.db.RadarCreatorComparison(fromDate, toDate, "")
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "store_unavailable", "无法读取雷达观测数据")
		return
	}

	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	followerPoints := loadRadarCreatorFollowerPoints(dataDir, fromDate, toDate)

	entries := []creatorCompareEntry{}
	index := map[string]int{}
	for _, one := range series {
		key := creatorCompareKey(one.Platform, one.Author)
		entry := creatorCompareEntry{
			Key:          one.Platform + ":" + one.Author,
			Platform:     one.Platform,
			Author:       one.Author,
			Works:        one.Works,
			Observations: one.Observations,
			Days:         []creatorCompareDay{},
		}
		for _, day := range one.Days {
			entry.Days = append(entry.Days, creatorCompareDay{
				Date: day.Date, Likes: day.Likes, Collects: day.Collects,
				Shares: day.Shares, Comments: day.Comments, Observations: day.Observations,
			})
		}
		entries = append(entries, entry)
		index[key] = len(entries) - 1
	}
	// A creator that only has archived profile snapshots (no works in the store
	// yet) still belongs in the comparison: their follower line is real data.
	for _, point := range followerPoints {
		key := creatorCompareKey(point.Platform, point.Author)
		if _, found := index[key]; found {
			continue
		}
		entries = append(entries, creatorCompareEntry{
			Key:      point.Platform + ":" + point.Author,
			Platform: point.Platform,
			Author:   point.Author,
			Days:     []creatorCompareDay{},
		})
		index[key] = len(entries) - 1
	}

	for position := range entries {
		entry := &entries[position]
		key := creatorCompareKey(entry.Platform, entry.Author)
		// Work on heap copies so appending follower-only days cannot invalidate
		// pointers into the slice's backing array.
		byDate := map[string]*creatorCompareDay{}
		for _, day := range entry.Days {
			copied := day
			byDate[day.Date] = &copied
		}
		for _, point := range followerPoints {
			if creatorCompareKey(point.Platform, point.Author) != key {
				continue
			}
			day, found := byDate[point.Date]
			if !found {
				day = &creatorCompareDay{Date: point.Date}
				byDate[point.Date] = day
			}
			followers := point.Followers
			day.Followers = &followers
			entry.FollowerPoints++
		}
		dates := make([]string, 0, len(byDate))
		for date := range byDate {
			dates = append(dates, date)
		}
		sort.Strings(dates)
		entry.Days = make([]creatorCompareDay, 0, len(dates))
		for _, date := range dates {
			entry.Days = append(entry.Days, *byDate[date])
		}
		observedDays := 0
		for _, day := range entry.Days {
			if day.Observations > 0 {
				observedDays++
			}
		}
		entry.InsufficientFollowers = entry.FollowerPoints < 2
		entry.InsufficientSeries = observedDays < 2
	}

	// Deterministic order: the creators with the most real data first, so the
	// view opens on comparable lines instead of an arbitrary order.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Observations != entries[j].Observations {
			return entries[i].Observations > entries[j].Observations
		}
		if entries[i].FollowerPoints != entries[j].FollowerPoints {
			return entries[i].FollowerPoints > entries[j].FollowerPoints
		}
		if entries[i].Platform != entries[j].Platform {
			return entries[i].Platform < entries[j].Platform
		}
		return entries[i].Author < entries[j].Author
	})

	writeJSON(w, creatorCompareResponse{
		Window:   creatorCompareWindow{From: fromDate, To: toDate, Days: days},
		Creators: entries,
		Empty:    len(entries) == 0,
	})
}

func creatorCompareKey(platform, author string) string {
	return platform + "\x00" + author
}

// radarCreatorFollowerPoint is one archived creator_profile observation: the
// follower total the provider really answered for that creator on that day.
type radarCreatorFollowerPoint struct {
	Platform  string
	Author    string
	Date      string
	Followers int64
}

// loadRadarCreatorFollowerPoints walks the archived creator snapshots and keeps
// one point per creator per day (the latest fetch that day). It never invents a
// point: a snapshot whose follower number cannot be read contributes nothing,
// and a creator with no snapshot simply has no follower line.
func loadRadarCreatorFollowerPoints(dataDir, fromDate, toDate string) []radarCreatorFollowerPoint {
	root := filepath.Join(dataDir, "trend-radar", "creators")
	platforms, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	latest := map[string]radarCreatorFollowerPoint{}
	fetchedAt := map[string]time.Time{}
	for _, platformEntry := range platforms {
		if !platformEntry.IsDir() {
			continue
		}
		platform := platformEntry.Name()
		creatorDirs, err := os.ReadDir(filepath.Join(root, platform))
		if err != nil {
			continue
		}
		for _, creatorDir := range creatorDirs {
			if !creatorDir.IsDir() {
				continue
			}
			snapshotDir := filepath.Join(root, platform, creatorDir.Name(), "snapshots")
			files, err := os.ReadDir(snapshotDir)
			if err != nil {
				continue
			}
			for _, file := range files {
				name := file.Name()
				if file.IsDir() || !strings.Contains(name, "creator_profile") || !strings.HasSuffix(name, ".json") {
					continue
				}
				point, fetched, ok := readRadarCreatorFollowerPoint(filepath.Join(snapshotDir, name), platform, fromDate, toDate)
				if !ok {
					continue
				}
				key := creatorCompareKey(point.Platform, point.Author) + "\x00" + point.Date
				if current, found := fetchedAt[key]; found && !fetched.After(current) {
					continue
				}
				latest[key] = point
				fetchedAt[key] = fetched
			}
		}
	}
	points := make([]radarCreatorFollowerPoint, 0, len(latest))
	for _, entry := range latest {
		points = append(points, entry)
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Date != points[j].Date {
			return points[i].Date < points[j].Date
		}
		if points[i].Platform != points[j].Platform {
			return points[i].Platform < points[j].Platform
		}
		return points[i].Author < points[j].Author
	})
	return points
}

func readRadarCreatorFollowerPoint(path, platform, fromDate, toDate string) (radarCreatorFollowerPoint, time.Time, bool) {
	var snapshot struct {
		Platform  string                 `json:"platform"`
		FetchedAt string                 `json:"fetched_at"`
		Response  map[string]interface{} `json:"response"`
	}
	if err := readRadarJSON(path, &snapshot); err != nil {
		return radarCreatorFollowerPoint{}, time.Time{}, false
	}
	if snapshot.Platform != "" {
		platform = snapshot.Platform
	}
	if snapshot.Response == nil {
		return radarCreatorFollowerPoint{}, time.Time{}, false
	}
	followers, ok := tikhub.CreatorFollowerCount(snapshot.Response, platform)
	if !ok {
		return radarCreatorFollowerPoint{}, time.Time{}, false
	}
	author := radarCreatorProfileNickname(snapshot.Response, platform)
	if author == "" {
		return radarCreatorFollowerPoint{}, time.Time{}, false
	}
	fetchedAt, err := time.Parse(time.RFC3339Nano, snapshot.FetchedAt)
	if err != nil {
		fetchedAt, err = time.Parse(time.RFC3339, snapshot.FetchedAt)
		if err != nil {
			return radarCreatorFollowerPoint{}, time.Time{}, false
		}
	}
	date := fetchedAt.Local().Format("2006-01-02")
	if date < fromDate || date > toDate {
		return radarCreatorFollowerPoint{}, time.Time{}, false
	}
	return radarCreatorFollowerPoint{Platform: platform, Author: author, Date: date, Followers: followers}, fetchedAt, true
}

// radarCreatorProfileNickname reads the creator's display name out of an
// archived profile response. The paths mirror the ones CreatorFollowerCount
// documents for real responses: douyin answers data.user.nickname, xhs answers
// data.data.nickname.
func radarCreatorProfileNickname(response map[string]interface{}, platform string) string {
	var path []string
	switch platform {
	case "douyin":
		path = []string{"data", "user", "nickname"}
	case "xhs":
		path = []string{"data", "data", "nickname"}
	default:
		return ""
	}
	var current interface{} = response
	for _, key := range path {
		container, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		current, ok = container[key]
		if !ok {
			return ""
		}
	}
	value, ok := current.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}
