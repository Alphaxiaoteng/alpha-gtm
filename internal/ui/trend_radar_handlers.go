package ui

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/radar"
	"alpha-nexus-egolite/internal/tikhub"
)

const (
	maxRadarSnapshotBytes = 64 << 20
	maxRadarCoverBytes    = 8 << 20
	archivedRadarNicheID  = "archived"
)

type trendRadarSummary struct {
	GeneratedAt     string `json:"generated_at"`
	LastSyncedAt    string `json:"last_synced_at,omitempty"`
	SnapshotState   string `json:"snapshot_state"`
	Storage         string `json:"storage"`
	ConfigUpdatedAt string `json:"config_updated_at,omitempty"`
	SearchRuns      int    `json:"search_runs"`
	// 平台返回的卡：广告卡 / 引导卡 / 读不到（既不是作品也没有明确卡标记）。
	AdCards      int                 `json:"ad_cards"`
	GuideCards   int                 `json:"guide_cards"`
	Unreadable   int                 `json:"unreadable"`
	Signals      []trendRadarSignal  `json:"signals"`
	VideoLookups []trendRadarSignal  `json:"video_lookups"`
	Niches       []trendRadarNiche   `json:"niches"`
	Creators     []trendRadarCreator `json:"creators"`
	Sources      []trendRadarSource  `json:"sources"`
}

type trendRadarSignal struct {
	NicheID        string               `json:"niche_id"`
	NicheName      string               `json:"niche_name"`
	SearchTerm     string               `json:"search_term"`
	Platform       string               `json:"platform"`
	Title          string               `json:"title"`
	ContentText    string               `json:"content_text,omitempty"`
	ContentSource  string               `json:"content_source,omitempty"`
	Creator        string               `json:"creator,omitempty"`
	CreatorID      string               `json:"creator_id,omitempty"`
	URL            string               `json:"url,omitempty"`
	CoverURL       string               `json:"cover_url,omitempty"`
	ObservedAt     string               `json:"observed_at,omitempty"`
	PublishedAt    string               `json:"published_at,omitempty"`
	FirstSeen      string               `json:"first_seen_at,omitempty"`
	Occurrences    int                  `json:"occurrences"`
	IsNew          bool                 `json:"is_new"`
	Play           int64                `json:"play"`
	Likes          int64                `json:"likes"`
	Collects       int64                `json:"collects"`
	Shares         int64                `json:"shares"`
	Comments       int64                `json:"comments,omitempty"`
	CallID         int64                `json:"call_id,omitempty"`
	VideoID        string               `json:"video_id,omitempty"`
	Tags           []string             `json:"tags,omitempty"`
	AnalysisStatus string               `json:"analysis_status,omitempty"`
	Analysis       *radar.VideoAnalysis `json:"analysis,omitempty"`
}

type trendRadarNiche struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Enabled        bool           `json:"enabled"`
	SearchTerms    []string       `json:"search_terms"`
	Platforms      []string       `json:"platforms"`
	PlatformLimits map[string]int `json:"platform_limits"`
	State          string         `json:"state"`
	ResultCount    int            `json:"result_count"`
	SearchRuns     int            `json:"search_runs"`
	LastSyncedAt   string         `json:"last_synced_at,omitempty"`
	LastRun        *radar.Run     `json:"last_run,omitempty"`
}

type trendRadarCreator struct {
	Platform        string                  `json:"platform"`
	UserID          string                  `json:"user_id"`
	Name            string                  `json:"name"`
	AvatarURL       string                  `json:"avatar_url,omitempty"`
	Signature       string                  `json:"signature,omitempty"`
	Note            string                  `json:"note,omitempty"`
	FolderPath      string                  `json:"folder_path"`
	LookbackDays    int                     `json:"lookback_days"`
	PageSize        int                     `json:"page_size"`
	MaxPages        int                     `json:"max_pages"`
	SortType        int                     `json:"sort_type"`
	SnapshotCount   int                     `json:"snapshot_count"`
	LastPages       int                     `json:"last_pages"`
	ProfileURL      string                  `json:"profile_url,omitempty"`
	ProfileChecked  string                  `json:"profile_checked_at,omitempty"`
	Followers       int64                   `json:"followers"`
	FollowersDelta  int64                   `json:"followers_delta"`
	TotalLikes      int64                   `json:"total_likes"`
	TotalLikesDelta int64                   `json:"total_likes_delta"`
	LastCheck       string                  `json:"last_check,omitempty"`
	LastCount       int                     `json:"last_count"`
	NewPosts        int                     `json:"new_posts"`
	TotalEngagement int64                   `json:"total_engagement"`
	EngagementDelta int64                   `json:"engagement_delta"`
	LatestPosts     []trendRadarCreatorPost `json:"latest_posts"`
	AutoArchive     bool                    `json:"auto_archive"`
	State           string                  `json:"state"`
	Message         string                  `json:"message"`
}

type trendRadarCreatorPost struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	URL        string `json:"url,omitempty"`
	CoverURL   string `json:"cover_url,omitempty"`
	CoverPath  string `json:"cover_path,omitempty"`
	AudioPath  string `json:"audio_path,omitempty"`
	Play       int64  `json:"play"`
	Likes      int64  `json:"likes"`
	Collects   int64  `json:"collects"`
	Shares     int64  `json:"shares"`
	Comments   int64  `json:"comments"`
	DurationMS int64  `json:"duration_ms"`
	ObservedAt string `json:"observed_at,omitempty"`
}

type trendRadarSource struct {
	Platform string `json:"platform"`
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	State    string `json:"state"`
	Message  string `json:"message"`
}

func (s *Server) handleAPITrendRadarSummary(w http.ResponseWriter, r *http.Request) {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	summary, err := s.cachedTrendRadarSummary(dataDir)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "trend_radar_unavailable", "无法读取本地监控记录")
		return
	}
	writeJSON(w, summary)
}

// buildTrendRadarSummary parses every stored call snapshot, which is hundreds of
// megabytes of JSON. Rebuilding it on each request costs seconds and blocks the
// interface on a screen that is otherwise instant, so the result is reused until
// the snapshot tree changes.
type radarSummaryCacheEntry struct {
	// signature changes when any snapshot file is added, removed, or rewritten.
	signature string
	summary   *trendRadarSummary
	builtAt   time.Time
}

const radarSummaryTTL = 60 * time.Second

// radarSnapshotSignature fingerprints the snapshot tree by count, newest
// modification time, and total size. It is far cheaper than reading the bodies
// and changes whenever a new call writes a file.
func radarSnapshotSignature(root string) string {
	var count int
	var newest int64
	var total int64
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		count++
		total += info.Size()
		if mod := info.ModTime().UnixNano(); mod > newest {
			newest = mod
		}
		return nil
	})
	return fmt.Sprintf("%d|%d|%d", count, total, newest)
}

func (s *Server) cachedTrendRadarSummary(dataDir string) (*trendRadarSummary, error) {
	signature := radarSnapshotSignature(filepath.Join(dataDir, "tikhub", "calls"))

	s.radarSummaryMu.Lock()
	if cached := s.radarSummary; cached != nil &&
		cached.signature == signature &&
		time.Since(cached.builtAt) < radarSummaryTTL {
		summary := cached.summary
		s.radarSummaryMu.Unlock()
		return summary, nil
	}
	s.radarSummaryMu.Unlock()

	// Build outside the lock: two concurrent first requests both doing the work
	// is cheaper than serialising every reader behind one slow parse.
	summary, err := buildTrendRadarSummary(dataDir, time.Now())
	if err != nil {
		return nil, err
	}

	s.radarSummaryMu.Lock()
	s.radarSummary = &radarSummaryCacheEntry{signature: signature, summary: summary, builtAt: time.Now()}
	s.radarSummaryMu.Unlock()
	return summary, nil
}

// invalidateRadarSummary drops the cache so the next request rebuilds it. Call
// it after anything that changes the snapshot tree directly.
func (s *Server) invalidateRadarSummary() {
	s.radarSummaryMu.Lock()
	s.radarSummary = nil
	s.radarSummaryMu.Unlock()
}

func (s *Server) handleAPITrendRadarConfig(w http.ResponseWriter, r *http.Request) {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	switch r.Method {
	case http.MethodGet:
		config, err := tikhub.LoadRadarConfig(dataDir)
		if err != nil {
			writeDataAPIError(w, http.StatusInternalServerError, "config_unavailable", "趋势雷达配置损坏")
			return
		}
		if len(config.Niches) == 0 {
			config.Niches = tikhub.BuiltinCuratedNiches()
		}
		writeJSON(w, config)
	case http.MethodPut:
		// 覆盖赛道和搜索词会改变后续付费抓取的范围，只接受本机同源调用。
		if !sameOriginLoopbackRequest(r) {
			writeDataAPIError(w, http.StatusForbidden, "origin_denied", "趋势雷达配置只允许从当前本地应用修改")
			return
		}
		var input tikhub.RadarConfig
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeDataAPIError(w, http.StatusBadRequest, "invalid_config", "赛道和搜索词配置格式不正确")
			return
		}
		if decoder.Decode(&struct{}{}) != io.EOF {
			writeDataAPIError(w, http.StatusBadRequest, "invalid_config", "配置包含多余 JSON")
			return
		}
		saved, err := tikhub.SaveRadarConfig(dataDir, input)
		if err != nil {
			writeDataAPIError(w, http.StatusBadRequest, "invalid_config", err.Error())
			return
		}
		writeJSON(w, saved)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

const maxRadarAssignTerms = 20

type trendRadarAssignKeywordRequest struct {
	Terms         []string `json:"terms"`
	TargetNicheID string   `json:"target_niche_id"`
	NewNiche      *struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Platforms   []string `json:"platforms"`
	} `json:"new_niche"`
}

// handleAPITrendRadarAssignKeyword moves unassigned keywords (currently shown
// under the derived "历史记录" niche) into an existing or brand-new niche. It
// only rewrites the local trend radar config; observation attribution is
// recomputed from the untouched call snapshots on the next summary build.
func (s *Server) handleAPITrendRadarAssignKeyword(w http.ResponseWriter, r *http.Request) {
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "搜索词归池只允许从当前本地应用发起")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	var input trendRadarAssignKeywordRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_terms", "归池参数格式不正确")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_terms", "归池参数包含多余 JSON")
		return
	}

	terms := make([]string, 0, len(input.Terms))
	seenTerms := map[string]bool{}
	for _, raw := range input.Terms {
		term := strings.TrimSpace(raw)
		if term == "" || len([]rune(term)) > 64 || seenTerms[term] {
			continue
		}
		seenTerms[term] = true
		terms = append(terms, term)
	}
	if len(terms) == 0 || len(terms) > maxRadarAssignTerms {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_terms", "请选择 1-20 个有效的未归属搜索词")
		return
	}

	targetID := strings.TrimSpace(input.TargetNicheID)
	createNew := input.NewNiche != nil
	if (targetID == "") == !createNew {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_target", "请选择一个现有赛道或新建赛道，二者只能选一")
		return
	}
	if createNew {
		input.NewNiche.Name = strings.TrimSpace(input.NewNiche.Name)
		input.NewNiche.Description = strings.TrimSpace(input.NewNiche.Description)
		if input.NewNiche.Name == "" || len([]rune(input.NewNiche.Name)) > 32 {
			writeDataAPIError(w, http.StatusBadRequest, "invalid_target", "新赛道名称必须为 1-32 个字符")
			return
		}
	}

	config, err := tikhub.LoadRadarConfig(dataDir)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "config_unavailable", "趋势雷达配置损坏")
		return
	}
	if len(config.Niches) == 0 {
		config.Niches = tikhub.BuiltinCuratedNiches()
	}

	// The unassigned list is always derived from the current summary, never
	// from client input, so a stale UI cannot move a word that is already
	// configured in another niche.
	current, err := buildTrendRadarSummary(dataDir, time.Now())
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "reassign_failed", "无法读取当前观测归属")
		return
	}
	archivedTerms := map[string]bool{}
	archivedPlatforms := []string{}
	for _, niche := range current.Niches {
		if niche.ID != archivedRadarNicheID {
			continue
		}
		for _, term := range niche.SearchTerms {
			archivedTerms[strings.TrimSpace(term)] = true
		}
		archivedPlatforms = append(archivedPlatforms, niche.Platforms...)
	}
	assigned := make([]string, 0, len(terms))
	for _, term := range terms {
		if archivedTerms[term] {
			assigned = append(assigned, term)
		}
	}
	if len(assigned) == 0 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_terms", "所选搜索词已归属其他赛道，无需归池")
		return
	}

	allowedPlatforms := map[string]bool{
		"douyin": true, "xhs": true, "kuaishou": true, "wechat_video": true, "wechat_mp": true,
		"youtube": true, "x": true,
	}
	platforms := make([]string, 0, len(archivedPlatforms))
	seenPlatforms := map[string]bool{}
	for _, platform := range archivedPlatforms {
		if !allowedPlatforms[platform] || seenPlatforms[platform] {
			continue
		}
		seenPlatforms[platform] = true
		platforms = append(platforms, platform)
	}
	if len(platforms) == 0 {
		platforms = []string{"douyin"}
	}

	targetIndex := -1
	if createNew {
		nicheID := "niche-" + strings.ToLower(strconv.FormatInt(time.Now().UnixNano(), 36))
		config.Niches = append(config.Niches, tikhub.RadarNiche{
			ID: nicheID, Name: input.NewNiche.Name, Description: input.NewNiche.Description,
			Enabled: true, SearchTerms: []string{}, Platforms: platforms,
		})
		targetIndex = len(config.Niches) - 1
	} else {
		for index := range config.Niches {
			if config.Niches[index].ID == targetID {
				if !config.Niches[index].Enabled {
					writeDataAPIError(w, http.StatusBadRequest, "invalid_target", "目标赛道已停用，请先启用")
					return
				}
				targetIndex = index
				break
			}
		}
		if targetIndex < 0 {
			writeDataAPIError(w, http.StatusBadRequest, "niche_not_found", "目标赛道不存在")
			return
		}
	}

	target := &config.Niches[targetIndex]
	for _, term := range assigned {
		if radarStringSliceContains(target.SearchTerms, term) {
			continue
		}
		if len(target.SearchTerms) >= maxRadarAssignTerms {
			writeDataAPIError(w, http.StatusBadRequest, "invalid_config", "目标赛道的搜索词已达上限")
			return
		}
		target.SearchTerms = append(target.SearchTerms, term)
	}

	saved, err := tikhub.SaveRadarConfig(dataDir, config)
	if err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}
	savedTargetID := target.ID
	savedTargetName := target.Name

	rebuilt, err := buildTrendRadarSummary(dataDir, time.Now())
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "reassign_failed", "归池后无法重算观测归属")
		return
	}
	for _, term := range assigned {
		found := false
		for _, niche := range rebuilt.Niches {
			if niche.ID == savedTargetID && radarStringSliceContains(niche.SearchTerms, term) {
				found = true
				break
			}
		}
		if !found {
			writeDataAPIError(w, http.StatusInternalServerError, "reassign_failed", "归池已保存，但观测归属未能重算到目标赛道")
			return
		}
	}

	writeJSON(w, map[string]any{
		"ok": true, "config": saved, "assigned_terms": assigned,
		"target_niche_id": savedTargetID, "target_niche_name": savedTargetName,
	})
}

func (s *Server) handleAPITrendRadarCreatorAdd(w http.ResponseWriter, r *http.Request) {
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "监测博主只允许从当前本地应用添加")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	var input struct {
		Platform string `json:"platform"`
		UserID   string `json:"user_id"`
		Name     string `json:"name"`
		Note     string `json:"note"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator", "博主信息格式不正确")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator", "博主信息包含多余 JSON")
		return
	}
	input.Platform = strings.ToLower(strings.TrimSpace(input.Platform))
	input.UserID = strings.TrimSpace(input.UserID)
	input.Name = strings.TrimSpace(input.Name)
	input.Note = strings.TrimSpace(input.Note)
	if (input.Platform != "xhs" && input.Platform != "douyin") ||
		!validRadarCreatorID(input.UserID) || len(input.Name) > 120 || len(input.Note) > 500 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator", "仅支持有效的抖音或小红书博主 ID")
		return
	}
	if err := tikhub.AddWatch(dataDir, input.Platform, input.UserID, input.Name, input.Note); err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "creator_add_failed", "无法更新本地监测名单")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleAPITrendRadarCreatorDelete(w http.ResponseWriter, r *http.Request) {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	platform := strings.TrimSpace(r.PathValue("platform"))
	userID := strings.TrimSpace(r.PathValue("user_id"))
	if (platform != "xhs" && platform != "douyin") || userID == "" || len(userID) > 256 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator", "博主平台或 ID 无效")
		return
	}
	if err := tikhub.RemoveWatch(dataDir, platform, userID); err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "creator_remove_failed", "无法更新本地监控名单")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleAPITrendRadarCreatorUpdate(w http.ResponseWriter, r *http.Request) {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	var input struct {
		Name         string `json:"name"`
		Note         string `json:"note"`
		LookbackDays int    `json:"lookback_days"`
		PageSize     int    `json:"page_size"`
		MaxPages     int    `json:"max_pages"`
		SortType     int    `json:"sort_type"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator_options", "博主抓取参数格式不正确")
		return
	}
	platform := strings.TrimSpace(r.PathValue("platform"))
	userID := strings.TrimSpace(r.PathValue("user_id"))
	user := tikhub.WatchUser{
		Platform: platform, UserID: userID, Name: strings.TrimSpace(input.Name), Note: strings.TrimSpace(input.Note),
		LookbackDays: input.LookbackDays, PageSize: input.PageSize, MaxPages: input.MaxPages, SortType: input.SortType,
	}
	if (platform != "xhs" && platform != "douyin") || !validRadarCreatorID(userID) ||
		len(user.Name) > 120 || len(user.Note) > 500 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator_options", "博主平台、ID 或参数无效")
		return
	}
	if err := tikhub.UpdateWatch(dataDir, user); err != nil {
		if os.IsNotExist(err) {
			writeDataAPIError(w, http.StatusNotFound, "creator_not_found", "监控博主不存在")
			return
		}
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator_options", "仅支持近 7/14/30 天、每页 20 条、最多 1/2/3/5 页和最新/最热排序")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleAPITrendRadarCreatorAutoArchive toggles the background worker's
// incremental archive for one monitored creator. It spends TikHub credit once
// per 24h, so it is off by default and only accepts explicit, same-origin,
// Douyin-on-watchlist requests.
func (s *Server) handleAPITrendRadarCreatorAutoArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeDataAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "只支持 PUT")
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "自动归档开关仅接受本机同源调用")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	platform := strings.TrimSpace(r.PathValue("platform"))
	userID := strings.TrimSpace(r.PathValue("user_id"))
	if platform != "douyin" || userID == "" || len(userID) > 256 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator", "自动增量归档当前仅支持抖音博主")
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Enabled == nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_auto_archive", "开关参数格式不正确")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_auto_archive", "开关参数包含多余 JSON")
		return
	}
	if _, err := tikhub.SetWatchAutoArchive(dataDir, platform, userID, *input.Enabled); err != nil {
		if os.IsNotExist(err) {
			writeDataAPIError(w, http.StatusNotFound, "creator_not_found", "博主不在本地监控名单中")
			return
		}
		writeDataAPIError(w, http.StatusInternalServerError, "creator_update_failed", "无法更新本地监控名单")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "enabled": *input.Enabled})
}

func validRadarCreatorID(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') &&
			(char < 'a' || char > 'z') &&
			(char < 'A' || char > 'Z') &&
			char != '_' && char != '-' && char != '.' {
			return false
		}
	}
	return true
}

func (s *Server) handleAPITrendRadarCoverBackfill(w http.ResponseWriter, r *http.Request) {
	// 封面补全会逐个下载并落盘，属于跨站可触发的写操作。
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "封面补全只允许从当前本地应用发起")
		return
	}
	var body struct {
		Confirm bool `json:"confirm"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || !body.Confirm {
		writeDataAPIError(w, http.StatusBadRequest, "confirmation_required", "请明确确认后再补全本地封面")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	snapshots, failedSnapshots, err := loadRadarCallSnapshots(filepath.Join(dataDir, "tikhub", "calls"), 0)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "snapshot_unavailable", "无法读取本地原始数据")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result := tikhub.CoverCacheResult{}
	for _, snapshot := range snapshots {
		cached := tikhub.CacheContentCovers(ctx, dataDir, snapshot.Response)
		result.Discovered += cached.Discovered
		result.Cached += cached.Cached
		result.Existing += cached.Existing
		result.Failed += cached.Failed
		if ctx.Err() != nil {
			break
		}
	}
	writeJSON(w, map[string]any{
		"ok":               result.Failed == 0 && failedSnapshots == 0,
		"snapshot_count":   len(snapshots),
		"snapshot_failed":  failedSnapshots,
		"cover_discovered": result.Discovered,
		"cover_cached":     result.Cached,
		"cover_existing":   result.Existing,
		"cover_failed":     result.Failed,
	})
}

func (s *Server) handleAPITrendRadarCover(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if len(name) != 68 || !strings.HasSuffix(name, ".img") {
		http.NotFound(w, r)
		return
	}
	if _, err := hex.DecodeString(strings.TrimSuffix(name, ".img")); err != nil {
		http.NotFound(w, r)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target := filepath.Join(dataDir, "tikhub", "covers", name)
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxRadarCoverBytes {
		http.NotFound(w, r)
		return
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := http.DetectContentType(raw)
	switch contentType {
	case "image/jpeg", "image/png", "image/webp", "image/gif", "image/avif":
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(raw)
}

// handleAPITrendRadarCreatorMedia serves locally cached covers and audio files for a creator.
// It enforces strict path traversal checks, validates platform/kind/filename, and supports HTTP Range
// requests via http.ServeContent so audio playback and scrubbing work smoothly.
func (s *Server) handleAPITrendRadarCreatorMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	platform := strings.TrimSpace(r.PathValue("platform"))
	userID := strings.TrimSpace(r.PathValue("user_id"))
	kind := strings.TrimSpace(r.PathValue("kind"))
	file := strings.TrimSpace(r.PathValue("file"))

	if (platform != "douyin" && platform != "xhs") || userID == "" || len(userID) > 256 {
		http.NotFound(w, r)
		return
	}
	if kind != "covers" && kind != "audio" {
		http.NotFound(w, r)
		return
	}
	if file == "" || len(file) > 128 || filepath.Base(file) != file || strings.Contains(file, "..") {
		http.NotFound(w, r)
		return
	}

	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	folder := tikhub.CreatorFolder(dataDir, platform, userID)
	mediaDir := filepath.Join(folder, "media", kind)
	target := filepath.Join(mediaDir, file)

	// Strict path traversal defense: ensure target is strictly inside mediaDir
	cleanMediaDir := filepath.Clean(mediaDir)
	cleanTarget := filepath.Clean(target)
	if !strings.HasPrefix(cleanTarget, cleanMediaDir+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(cleanTarget)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		http.NotFound(w, r)
		return
	}

	maxSize := int64(15 << 20)
	if kind == "audio" {
		maxSize = 64 << 20
	}
	if info.Size() > maxSize {
		http.NotFound(w, r)
		return
	}

	var contentType string
	ext := strings.ToLower(filepath.Ext(file))
	if kind == "covers" {
		switch ext {
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".png":
			contentType = "image/png"
		case ".webp":
			contentType = "image/webp"
		case ".gif":
			contentType = "image/gif"
		case ".avif":
			contentType = "image/avif"
		default:
			header := make([]byte, 512)
			n, _ := f.Read(header)
			_, _ = f.Seek(0, io.SeekStart)
			contentType = http.DetectContentType(header[:n])
			switch contentType {
			case "image/jpeg", "image/png", "image/webp", "image/gif", "image/avif":
			default:
				http.NotFound(w, r)
				return
			}
		}
	} else {
		switch ext {
		case ".mp3":
			contentType = "audio/mpeg"
		case ".m4a":
			contentType = "audio/x-m4a"
		case ".wav":
			contentType = "audio/wav"
		case ".aac":
			contentType = "audio/aac"
		default:
			contentType = "audio/mpeg"
		}
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, file, info.ModTime(), f)
}

func buildTrendRadarSummary(dataDir string, now time.Time) (*trendRadarSummary, error) {
	config, err := tikhub.LoadRadarConfig(dataDir)
	if err != nil {
		return nil, err
	}
	summary := &trendRadarSummary{
		GeneratedAt:     now.UTC().Format(time.RFC3339),
		SnapshotState:   "empty",
		Storage:         "本地配置 + SQLite 调用索引 + 完整 JSON 快照",
		ConfigUpdatedAt: config.UpdatedAt,
		Signals:         []trendRadarSignal{},
		Niches:          make([]trendRadarNiche, 0, len(config.Niches)),
		Creators:        []trendRadarCreator{},
		Sources:         radarSources(),
	}
	nicheIndex := map[string]int{}
	termNiches := map[string][]string{}
	for _, niche := range config.Niches {
		state := "disabled"
		if niche.Enabled {
			state = "not_synced"
		}
		summary.Niches = append(summary.Niches, trendRadarNiche{
			ID: niche.ID, Name: niche.Name, Description: niche.Description, Enabled: niche.Enabled,
			SearchTerms: append([]string(nil), niche.SearchTerms...), Platforms: append([]string(nil), niche.Platforms...),
			PlatformLimits: niche.PlatformLimits, State: state,
		})
		nicheIndex[niche.ID] = len(summary.Niches) - 1
		if niche.Enabled {
			for _, term := range niche.SearchTerms {
				key := strings.ToLower(strings.TrimSpace(term))
				termNiches[key] = append(termNiches[key], niche.ID)
			}
		}
	}

	snapshots, failedSnapshots, err := loadRadarCallSnapshots(filepath.Join(dataDir, "tikhub", "calls"), 0)
	if err != nil {
		return nil, err
	}
	// Per-run item caps are applied by call id, so history collected before a
	// cap existed is never retroactively truncated.
	runCaps := map[string]int{}
	lastRuns := map[string]radar.Run{}
	if runs, runErr := radar.ListRuns(dataDir, 0); runErr == nil {
		for _, run := range runs {
			if _, seen := lastRuns[run.NicheID]; !seen {
				lastRuns[run.NicheID] = run
			}
			for _, call := range run.Calls {
				if call.CallID > 0 {
					runCaps[fmt.Sprintf("%d|%s", call.CallID, run.NicheID)] = call.KeptCount
				}
			}
		}
	}
	type savedCaption struct {
		text      string
		fetchedAt string
	}
	youtubeCaptions := map[string]savedCaption{}
	for _, snapshot := range snapshots {
		if snapshot.Platform != "youtube" || snapshot.Capability != "content_transcript" {
			continue
		}
		videoID := strings.TrimSpace(fmt.Sprint(snapshot.Parameters["video_id"]))
		content := radarString(snapshot.Response, "data.content")
		if videoID == "" || videoID == "<nil>" || content == "" {
			continue
		}
		current, exists := youtubeCaptions[videoID]
		if !exists || snapshot.FetchedAt > current.fetchedAt {
			youtubeCaptions[videoID] = savedCaption{text: content, fetchedAt: snapshot.FetchedAt}
		}
	}
	signalIndex := map[string]int{}
	videoLookupIndex := map[string]int{}
	creatorPostSnapshots := map[string][]tikhub.CallSnapshot{}
	creatorProfileSnapshots := map[string][]tikhub.CallSnapshot{}
	for _, snapshot := range snapshots {
		if snapshot.FetchedAt > summary.LastSyncedAt {
			summary.LastSyncedAt = snapshot.FetchedAt
		}
		switch snapshot.Capability {
		case "keyword_search":
			// 广告卡/引导卡/读不到 按整份响应算一次，不受各池保留名额影响；
			// 这里只数平台自己标出来的卡，抓不到标记的仍算读不到。
			if tally := radarClassifyResultCards(snapshot.Platform, snapshot.Response); tally.Found {
				summary.AdCards += tally.Ads
				summary.GuideCards += tally.Guides
				summary.Unreadable += tally.Unreadable
			}
			keyword := strings.TrimSpace(fmt.Sprint(snapshot.Parameters["keyword"]))
			nicheIDs := termNiches[strings.ToLower(keyword)]
			if len(nicheIDs) == 0 {
				index, exists := nicheIndex[archivedRadarNicheID]
				if !exists {
					summary.Niches = append(summary.Niches, trendRadarNiche{
						ID: archivedRadarNicheID, Name: "历史记录", State: "archived",
						SearchTerms: []string{}, Platforms: []string{},
					})
					index = len(summary.Niches) - 1
					nicheIndex[archivedRadarNicheID] = index
				}
				archived := &summary.Niches[index]
				if keyword != "" && !radarStringSliceContains(archived.SearchTerms, keyword) {
					archived.SearchTerms = append(archived.SearchTerms, keyword)
				}
				if snapshot.Platform != "" && !radarStringSliceContains(archived.Platforms, snapshot.Platform) {
					archived.Platforms = append(archived.Platforms, snapshot.Platform)
				}
				nicheIDs = []string{archivedRadarNicheID}
			}
			summary.SearchRuns++
			for _, nicheID := range nicheIDs {
				index := nicheIndex[nicheID]
				summary.Niches[index].SearchRuns++
				if snapshot.FetchedAt > summary.Niches[index].LastSyncedAt {
					summary.Niches[index].LastSyncedAt = snapshot.FetchedAt
				}
				items := radarCandidateItems(snapshot.Platform, snapshot.Response)
				if kept, capped := runCaps[fmt.Sprintf("%d|%s", snapshot.CallID, nicheID)]; capped && kept >= 0 && len(items) > kept {
					items = items[:kept]
				}
				for _, item := range items {
					signal := normalizeRadarSignal(summary.Niches[index], keyword, snapshot.Platform, snapshot.FetchedAt, dataDir, item)
					if signal.Title == "" {
						continue
					}
					signal.CallID = snapshot.CallID
					if signal.Platform == "douyin" && signal.VideoID != "" {
						if analysis, loadErr := radar.LoadVideoAnalysis(dataDir, signal.VideoID); loadErr == nil {
							signal.AnalysisStatus = analysis.Status
							signal.Analysis = &analysis
						} else {
							signal.AnalysisStatus = "pending"
						}
					}
					if signal.Platform == "youtube" {
						videoID := radarString(item, "video_id", "videoId", "id.videoId")
						if caption, ok := youtubeCaptions[videoID]; ok {
							signal.ContentText = caption.text
							signal.ContentSource = "platform_caption"
						}
					}
					key := nicheID + "|" + radarWorkDedupKey(signal.Platform, signal.VideoID, signal.URL, signal.Title)
					if existingIndex, ok := signalIndex[key]; ok {
						existing := &summary.Signals[existingIndex]
						existing.Occurrences++
						if signal.ObservedAt > existing.ObservedAt {
							existing.ObservedAt = signal.ObservedAt
						}
						if signal.FirstSeen < existing.FirstSeen {
							existing.FirstSeen = signal.FirstSeen
						}
						if existing.PublishedAt == "" && signal.PublishedAt != "" {
							existing.PublishedAt = signal.PublishedAt
						}
						if existing.CallID == 0 {
							existing.CallID = signal.CallID
						}
						if existing.Analysis == nil && signal.Analysis != nil {
							existing.Analysis = signal.Analysis
							existing.AnalysisStatus = signal.AnalysisStatus
						}
						continue
					}
					signalIndex[key] = len(summary.Signals)
					summary.Signals = append(summary.Signals, signal)
				}
			}
		case "content_detail":
			if snapshot.Platform != "douyin" ||
				(snapshot.EndpointID != tikhub.DouyinVideoDetailEndpoint && snapshot.EndpointID != tikhub.DouyinVideoShareEndpoint) {
				continue
			}
			searchInput := strings.TrimSpace(fmt.Sprint(snapshot.Parameters["aweme_id"]))
			if searchInput == "" || searchInput == "<nil>" {
				searchInput = strings.TrimSpace(fmt.Sprint(snapshot.Parameters["share_url"]))
			}
			signal := bestRadarDetailSignal(snapshot, dataDir)
			if signal.Title == "" {
				continue
			}
			signal.NicheID = "video_lookup"
			signal.NicheName = "视频搜索"
			signal.SearchTerm = searchInput
			signal.CallID = snapshot.CallID
			signal.Occurrences = 1
			if signal.VideoID != "" {
				if analysis, loadErr := radar.LoadVideoAnalysis(dataDir, signal.VideoID); loadErr == nil {
					signal.AnalysisStatus = analysis.Status
					signal.Analysis = &analysis
				} else {
					signal.AnalysisStatus = "pending"
				}
			}
			key := radarWorkDedupKey(signal.Platform, signal.VideoID, signal.URL, signal.Title)
			if existingIndex, ok := videoLookupIndex[key]; ok {
				existing := &summary.VideoLookups[existingIndex]
				occurrences := existing.Occurrences + 1
				// Keep the first successful fetch as the canonical history row.
				// Raw snapshots and API logs remain untouched for auditability.
				if signal.CallID < existing.CallID {
					signal.Occurrences = occurrences
					*existing = signal
				} else {
					existing.Occurrences = occurrences
					if signal.ObservedAt > existing.ObservedAt {
						existing.ObservedAt = signal.ObservedAt
					}
				}
				continue
			}
			videoLookupIndex[key] = len(summary.VideoLookups)
			summary.VideoLookups = append(summary.VideoLookups, signal)
		case "creator_posts":
			creatorID := radarCreatorParameter(snapshot.Parameters)
			if creatorID == "" {
				continue
			}
			key := snapshot.Platform + "|" + creatorID
			creatorPostSnapshots[key] = append(creatorPostSnapshots[key], snapshot)
		case "creator_profile":
			creatorID := radarCreatorParameter(snapshot.Parameters)
			if creatorID == "" {
				continue
			}
			key := snapshot.Platform + "|" + creatorID
			creatorProfileSnapshots[key] = append(creatorProfileSnapshots[key], snapshot)
		}
	}

	sort.SliceStable(summary.Signals, func(i, j int) bool {
		if summary.Signals[i].ObservedAt == summary.Signals[j].ObservedAt {
			return summary.Signals[i].Likes > summary.Signals[j].Likes
		}
		return summary.Signals[i].ObservedAt > summary.Signals[j].ObservedAt
	})
	sort.SliceStable(summary.VideoLookups, func(i, j int) bool {
		return summary.VideoLookups[i].ObservedAt > summary.VideoLookups[j].ObservedAt
	})
	for index := range summary.Signals {
		summary.Signals[index].IsNew = radarSignalIsNew(summary.Signals[index].FirstSeen, now)
		niche := &summary.Niches[nicheIndex[summary.Signals[index].NicheID]]
		niche.ResultCount++
	}
	for index := range summary.Niches {
		if !summary.Niches[index].Enabled {
			continue
		}
		if summary.Niches[index].SearchRuns > 0 {
			summary.Niches[index].State = "ready"
		}
	}
	// Attach the most recent explicit run per niche so the console can show what
	// the last round cost and why it failed, without a second request.
	for nicheID, run := range lastRuns {
		index, ok := nicheIndex[nicheID]
		if !ok {
			continue
		}
		runCopy := run
		summary.Niches[index].LastRun = &runCopy
	}

	users, err := tikhub.LoadWatchlist(dataDir)
	if err != nil {
		return nil, fmt.Errorf("load creator watchlist: %w", err)
	}
	for _, user := range users {
		key := user.Platform + "|" + user.UserID
		sort.SliceStable(creatorPostSnapshots[key], func(i, j int) bool {
			return creatorPostSnapshots[key][i].FetchedAt > creatorPostSnapshots[key][j].FetchedAt
		})
		sort.SliceStable(creatorProfileSnapshots[key], func(i, j int) bool {
			return creatorProfileSnapshots[key][i].FetchedAt > creatorProfileSnapshots[key][j].FetchedAt
		})
		folderPath, folderErr := tikhub.EnsureCreatorFolder(dataDir, user)
		if folderErr != nil {
			return nil, fmt.Errorf("ensure creator folder: %w", folderErr)
		}
		creator := radarCreatorFromWatch(
			user,
			creatorPostSnapshots[key],
			creatorProfileSnapshots[key],
			now,
			dataDir,
		)
		creator.FolderPath = folderPath
		summary.Creators = append(summary.Creators, creator)
	}

	readyCreators := 0
	for _, creator := range summary.Creators {
		if creator.State == "ready" || creator.State == "stale" {
			readyCreators++
		}
	}
	switch {
	case failedSnapshots > 0:
		summary.SnapshotState = "partial"
	case summary.SearchRuns > 0 || readyCreators > 0 || len(summary.VideoLookups) > 0:
		summary.SnapshotState = "ready"
	}
	return summary, nil
}

func radarStringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func bestRadarDetailSignal(snapshot tikhub.CallSnapshot, dataDir string) trendRadarSignal {
	niche := trendRadarNiche{ID: "video_lookup", Name: "视频搜索"}
	best := trendRadarSignal{}
	bestScore := -1
	for _, item := range radarCandidateItems(snapshot.Platform, snapshot.Response) {
		signal := normalizeRadarSignal(niche, "", snapshot.Platform, snapshot.FetchedAt, dataDir, item)
		if signal.Title == "" {
			continue
		}
		score := 0
		if signal.URL != "" {
			score += 4
		}
		if signal.CoverURL != "" {
			score += 2
		}
		if signal.Creator != "" {
			score++
		}
		if signal.Play+signal.Likes+signal.Collects+signal.Shares > 0 {
			score++
		}
		if score > bestScore || (score == bestScore && signal.URL+"|"+signal.Title < best.URL+"|"+best.Title) {
			best = signal
			bestScore = score
		}
	}
	return best
}

func loadRadarCallSnapshots(root string, limit int) ([]tikhub.CallSnapshot, int, error) {
	dateEntries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	dateNames := make([]string, 0, len(dateEntries))
	for _, entry := range dateEntries {
		if entry.IsDir() {
			dateNames = append(dateNames, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dateNames)))
	capacity := 64
	if limit > 0 && limit < capacity {
		capacity = limit
	}
	result := make([]tikhub.CallSnapshot, 0, capacity)
	failed := 0
	for _, dateName := range dateNames {
		fileEntries, err := os.ReadDir(filepath.Join(root, dateName))
		if err != nil {
			failed++
			continue
		}
		sort.Slice(fileEntries, func(i, j int) bool { return fileEntries[i].Name() > fileEntries[j].Name() })
		for _, entry := range fileEntries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			var snapshot tikhub.CallSnapshot
			if err := readRadarJSON(filepath.Join(root, dateName, entry.Name()), &snapshot); err != nil {
				failed++
				continue
			}
			if snapshot.SchemaVersion != 1 || snapshot.CallID <= 0 || snapshot.EndpointID == "" {
				failed++
				continue
			}
			result = append(result, snapshot)
			if limit > 0 && len(result) >= limit {
				return result, failed, nil
			}
		}
	}
	return result, failed, nil
}

func readRadarJSON(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxRadarSnapshotBytes {
		return fmt.Errorf("snapshot exceeds %d bytes", maxRadarSnapshotBytes)
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxRadarSnapshotBytes+1))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("snapshot contains trailing JSON")
	}
	return nil
}

func radarCandidateItems(platform string, response map[string]interface{}) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, 32)
	seen := map[string]bool{}
	var walk func(any, int)
	walk = func(value any, depth int) {
		if depth > 12 {
			return
		}
		switch typed := value.(type) {
		case []interface{}:
			for _, child := range typed {
				walk(child, depth+1)
			}
		case map[string]interface{}:
			// 广告卡/引导卡整棵子树都不进候选：卡里的推广笔记、CTA 按钮、
			// 关联词都不是自然作品，不能让它们占 kept 名额或进榜单。
			if _, isCard := radarResultCardFromEntry(platform, typed); isCard {
				return
			}
			title := radarString(typed,
				"title", "desc", "description", "description_snippet", "full_text", "text", "legacy.full_text",
				"display_title", "item_title", "word", "sentence",
				// 快手把作品文案放在 caption，没有 title/desc；缺了它，
				// 快手搜索抓回来的作品会全部因为「没有标题」被判成杂项。
				"caption",
				"note_card.display_title", "note_card.title", "aweme_info.desc")
			// 视频号搜索只返回卡片：没有标题、没有正文，只有作者名与时长。
			// 用「作者 · 时长」合成一个可辨识的标题，否则这些作品会被整批丢弃，
			// 用户会以为视频号一条都没抓到。
			if strings.TrimSpace(title) == "" {
				if author := radarString(typed, "source.title", "source.nickname"); author != "" {
					duration := radarString(typed, "duration")
					if duration != "" {
						title = author + " · " + duration
					} else {
						title = author
					}
				}
			}
			// 卡片、日志一类杂项也有标题字段，但定位不到作品。它们不算候选，
			// 否则会占掉按 kept 截断的名额，把真作品整个切掉（2026-11-13 抖音搜索实测：
			// 21 个标题命中里只有 7 个是真作品，前 14 个杂项吃掉了 kept=14 的全部名额）。
			identity := radarString(typed,
				"share_url", "url", "note_card.url", "aweme_info.share_url",
				"aweme_id", "aweme_info.aweme_id", "note_id", "note_card.note_id",
				"photo_id", "object_id", "video_id", "videoId", "id.videoId",
				// 视频号的作品标识叫 docID；缺了它，视频号的作品全部因为
				// 「定位不到作品」被判成杂项，抓取成功但雷达里一条都没有。
				"docID", "doc_id", "hashDocID",
				"rest_id", "tweet_id", "id_str", "legacy.id_str")
			if identity == "" {
				// 与 normalizeRadarSignal 同一口径：小红书的作品 id 是 24 位十六进制，
				// 而搜索推荐词一类杂项也有 id，但形如 19 位数字，不能算作品。
				if candidate := radarString(typed, "id"); validXHSNoteID(candidate) {
					identity = candidate
				}
			}
			if strings.TrimSpace(title) != "" && identity != "" {
				key := title + "|" + identity
				if !seen[key] {
					seen[key] = true
					items = append(items, typed)
				}
				return
			}
			for _, child := range typed {
				walk(child, depth+1)
			}
		}
	}
	walk(response, 0)
	return items
}

// radarWorkIdentity is the single dedup identity value for one collected work,
// shared by the summary board, the store (radar_works.work_id) and the export.
// Resolution order:
//  1. the platform work id (douyin aweme_id, xhs note_id, ...) — the normal case;
//  2. a normalized url (scheme+host+path, query/fragment stripped), because
//     provider share urls carry per-share query params that must not split one
//     work into several rows;
//  3. the lowercased title — explicit last resort, only when a work has neither
//     an id nor a url. It is never used to merge works that do have an id.
func radarWorkIdentity(workID, rawURL, title string) string {
	if id := strings.TrimSpace(workID); id != "" {
		return id
	}
	if normalized := normalizeRadarWorkURL(rawURL); normalized != "" {
		return normalized
	}
	return strings.ToLower(strings.TrimSpace(title))
}

// radarWorkDedupKey scopes a work identity to its platform, so the same id
// collected from two platforms never collapses into one row.
func radarWorkDedupKey(platform, workID, rawURL, title string) string {
	return platform + "|" + radarWorkIdentity(workID, rawURL, title)
}

// normalizeRadarWorkURL keeps only scheme://host/path, lowercased, so two shares
// of the same work with different tracking params collapse to one key.
func normalizeRadarWorkURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return strings.ToLower(trimmed)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "" {
		scheme = "https"
	}
	host := strings.ToLower(parsed.Host)
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return scheme + "://" + host + path
}

func normalizeRadarSignal(niche trendRadarNiche, keyword, platform, observed, dataDir string, item map[string]interface{}) trendRadarSignal {
	url := radarString(item, "share_url", "url", "note_card.url", "aweme_info.share_url")
	id := ""
	switch platform {
	case "douyin":
		id = radarString(item, "aweme_id", "aweme_info.aweme_id")
	case "xhs":
		id = radarString(item, "note_id", "note_card.note_id")
		if id == "" {
			if candidate := radarString(item, "id"); validXHSNoteID(candidate) {
				id = candidate
			}
		}
	case "kuaishou":
		id = radarString(item, "photo_id")
	case "wechat_video":
		// 视频号搜索返回的是卡片：作品标识在 docID，没有 object_id。
		id = radarString(item, "object_id", "docID", "doc_id")
	case "wechat_mp":
		// 公众号同样用 docID，并自带 doc_url。
		id = radarString(item, "docID", "doc_id")
		if url == "" {
			url = radarString(item, "doc_url", "docUrl")
		}
	case "youtube":
		id = radarString(item, "video_id", "videoId", "id.videoId")
	case "x":
		id = radarString(item, "rest_id", "tweet_id", "id_str", "legacy.id_str")
	}
	if id == "" && url == "" {
		return trendRadarSignal{}
	}
	if url == "" && id != "" {
		switch platform {
		case "douyin":
			url = "https://www.douyin.com/video/" + id
		case "xhs":
			url = "https://www.xiaohongshu.com/explore/" + id
		case "youtube":
			url = "https://www.youtube.com/watch?v=" + id
		case "x":
			url = "https://x.com/i/web/status/" + id
		case "kuaishou":
			// 快手的作品标识是 photo_id，公开链接形如 short-video/<id>。
			url = "https://www.kuaishou.com/short-video/" + id
		case "wechat_video":
			// 视频号的 docID 是内部标识，拼不出稳定公开链接；
			// 留空比给一个打不开的地址更好。
		}
	}
	coverURL := ""
	coverSource := radarString(item,
		"images_list.0.url", "image_list.0.url_list.0",
		"cover.url", "cover.url_default", "cover.url_pre",
		"video.cover.url_list.0", "video.origin_cover.url_list.0",
		"aweme_info.video.cover.url_list.0", "aweme_info.video.origin_cover.url_list.0",
		"thumbnail.url", "thumbnails.0.url", "thumbnail.0.url",
		"media.video.0.media_url_https", "media.photo.0.media_url_https",
		"legacy.extended_entities.media.0.media_url_https",
		"legacy.entities.media.0.media_url_https",
	)
	if coverSource != "" {
		name := tikhub.CachedCoverName(coverSource)
		if info, err := os.Stat(filepath.Join(dataDir, "tikhub", "covers", name)); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			coverURL = "/api/trend-radar/covers/" + name
		}
	}
	contentText := radarString(item,
		"description", "description_snippet", "desc", "full_text", "text", "legacy.full_text",
		// 快手作品只有 caption，没有 desc/description。
		"caption",
		"note_card.desc", "note_card.description", "aweme_info.desc",
	)
	title := radarString(item,
		"title", "display_title", "item_title", "word", "sentence",
		"note_card.display_title", "note_card.title",
	)
	if title == "" {
		title = contentText
	}
	// 视频号搜索只给卡片：没有标题也没有正文，只有作者名与时长。
	// 与候选提取保持同一口径，否则这里会把它们又过滤掉。
	if title == "" {
		if author := radarString(item, "source.title", "source.nickname"); author != "" {
			if duration := radarString(item, "duration"); duration != "" {
				title = author + " · " + duration
			} else {
				title = author
			}
		}
	}
	contentSource := ""
	if contentText != "" {
		contentSource = "provider_text"
	}
	return trendRadarSignal{
		NicheID: niche.ID, NicheName: niche.Name, SearchTerm: keyword, Platform: platform,
		VideoID: id,
		Title:   title, ContentText: contentText, ContentSource: contentSource,
		Tags: radarPlatformTags(platform, item),
		Creator: radarString(item,
			// 快手的作者名在 user_name，不在任何 nickname 字段里；
			// 缺了它，快手信号有互动数但没有作者，无法追溯作品。
			"user_name", "userName",
			// 视频号搜索返回的是卡片，作者名在 source.title。
			"source.title",
			"nickname", "nick_name", "author", "author.nickname", "author.name", "channel.name", "channel_title",
			"user.nickname", "user.name", "user_info.name", "user_info.screen_name", "note_card.user.nickname", "aweme_info.author.nickname",
			"core.user_results.result.legacy.name", "core.user_results.result.legacy.screen_name",
		),
		CreatorID: radarString(item,
			// 快手同理：作者 id 是 user_id / kwaiId。
			"user_id", "userId", "kwaiId",
			"author.sec_uid", "author.uid", "aweme_info.author.sec_uid", "aweme_info.author.uid",
			"user.user_id", "user.userid", "note_card.user.user_id", "note_card.user.userid",
		),
		URL: url, CoverURL: coverURL, ObservedAt: observed, FirstSeen: observed, Occurrences: 1,
		PublishedAt: radarPublishedAtFor(platform, item, ""),
		Play:        radarInt(item, "play_count", "view_count", "views", "statistics.play_count", "aweme_info.statistics.play_count"),
		Likes:       radarInt(item, "digg_count", "liked_count", "like_count", "likes", "favorites", "favorite_count", "legacy.favorite_count", "statistics.digg_count", "aweme_info.statistics.digg_count"),
		Collects:    radarInt(item, "collect_count", "collected_count", "bookmarks", "statistics.collect_count", "aweme_info.statistics.collect_count", "aweme_info.statistics.collected_count"),
		Shares:      radarInt(item, "share_count", "retweets", "retweet_count", "legacy.retweet_count", "statistics.share_count", "aweme_info.statistics.share_count"),
		Comments:    radarInt(item, "comment_count", "comments", "reply_count", "statistics.comment_count", "aweme_info.statistics.comment_count"),
	}
}

// radarPlatformTags reads the platform's own hashtags/topics from the raw item
// (Douyin text_extra/cha_list, X entities.hashtags, Xiaohongshu tag_info). A
// platform that sent none stays empty: the export never invents a label.
func radarPlatformTags(platform string, item map[string]interface{}) []string {
	seen := map[string]bool{}
	tags := make([]string, 0, 4)
	appendTag := func(raw interface{}) {
		value := strings.TrimSpace(fmt.Sprint(raw))
		value = strings.TrimSpace(strings.TrimPrefix(value, "#"))
		if value == "" || value == "<nil>" || seen[value] {
			return
		}
		seen[value] = true
		tags = append(tags, value)
	}
	appendArray := func(path, key string) {
		value, ok := radarValue(item, path)
		if !ok {
			return
		}
		list, ok := value.([]interface{})
		if !ok {
			return
		}
		for _, entry := range list {
			object, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			raw, ok := object[key]
			if !ok {
				continue
			}
			appendTag(raw)
		}
	}
	switch platform {
	case "douyin":
		appendArray("aweme_info.text_extra", "hashtag_name")
		appendArray("text_extra", "hashtag_name")
		appendArray("aweme_info.cha_list", "cha_name")
		appendArray("cha_list", "cha_name")
	case "x":
		appendArray("entities.hashtags", "text")
		appendArray("legacy.entities.hashtags", "text")
	case "xhs":
		if title := radarString(item, "tag_info.title", "note_card.tag_info.title"); title != "" {
			appendTag(title)
		}
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

func validXHSNoteID(value string) bool {
	if len(value) != 24 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

func radarSignalIsNew(firstSeen string, now time.Time) bool {
	value, err := time.Parse(time.RFC3339Nano, firstSeen)
	if err != nil {
		value, err = time.Parse(time.RFC3339, firstSeen)
	}
	return err == nil && now.Sub(value) <= 48*time.Hour
}

func radarCreatorParameter(parameters map[string]any) string {
	for _, key := range []string{"user_id", "sec_user_id", "username"} {
		value := strings.TrimSpace(fmt.Sprint(parameters[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func radarCreatorFromWatch(
	user tikhub.WatchUser,
	postSnapshots []tikhub.CallSnapshot,
	profileSnapshots []tikhub.CallSnapshot,
	now time.Time,
	dataDir string,
) trendRadarCreator {
	creator := trendRadarCreator{
		Platform: user.Platform, UserID: user.UserID, Name: user.Name, Note: user.Note,
		LastCheck: user.LastCheck, LastCount: user.LastCount,
		LookbackDays: user.LookbackDays, PageSize: user.PageSize, MaxPages: user.MaxPages, SortType: user.SortType,
		SnapshotCount: len(postSnapshots) + len(profileSnapshots),
		// 后台 worker 的增量归档开关：默认 false，UI 用它渲染博主级开关状态。
		AutoArchive: user.AutoArchive,
		// 从未抓取的博主没有作品快照；预置空切片，避免 JSON 序列化成 null
		// 让前端卡片在 .length/.map 上直接崩溃。
		LatestPosts: []trendRadarCreatorPost{},
	}
	switch user.Platform {
	case "douyin":
		creator.ProfileURL = "https://www.douyin.com/user/" + user.UserID
	case "xhs":
		creator.ProfileURL = "https://www.xiaohongshu.com/user/profile/" + user.UserID
	}
	if creator.Name == "" {
		creator.Name = user.UserID
	}
	if len(profileSnapshots) > 0 {
		current := profileSnapshots[0]
		creator.ProfileChecked = current.FetchedAt
		if name := radarString(current.Response,
			"data.user.nickname", "data.nickname", "data.user_info.nickname", "data.user_info.name",
			"user.nickname", "nickname", "user_info.nickname", "user_info.name",
		); name != "" {
			creator.Name = name
		}
		if avatar := radarString(current.Response,
			"data.user.avatar_larger.url_list.0", "data.user.avatar_thumb.url_list.0",
			"data.user.avatar_300x300.url_list.0", "data.user.avatar_medium.url_list.0",
			"data.user_info.imageb", "data.user_info.images", "data.basic_info.images",
			"data.imageb", "data.avatar_url", "user.avatar_larger.url_list.0",
			"user.avatar_thumb.url_list.0", "avatar_url",
		); avatar != "" {
			creator.AvatarURL = avatar
		}
		if signature := radarString(current.Response,
			"data.user.signature", "data.signature", "data.user_info.desc", "data.desc",
		); signature != "" {
			creator.Signature = signature
		}
		creator.Followers = radarInt(current.Response,
			"data.user.follower_count", "data.follower_count", "data.user_info.fans", "data.user_info.fans_count",
			"user.follower_count", "follower_count", "user_info.fans", "user_info.fans_count",
		)
		creator.TotalLikes = radarInt(current.Response,
			"data.user.total_favorited", "data.total_favorited", "data.user_info.liked_count", "data.user_info.total_favorited",
			"user.total_favorited", "total_favorited", "user_info.liked_count", "user_info.total_favorited",
		)
		if len(profileSnapshots) > 1 {
			previous := profileSnapshots[1]
			creator.FollowersDelta = creator.Followers - radarInt(previous.Response,
				"data.user.follower_count", "data.follower_count", "data.user_info.fans", "data.user_info.fans_count",
				"user.follower_count", "follower_count", "user_info.fans", "user_info.fans_count",
			)
			creator.TotalLikesDelta = creator.TotalLikes - radarInt(previous.Response,
				"data.user.total_favorited", "data.total_favorited", "data.user_info.liked_count", "data.user_info.total_favorited",
				"user.total_favorited", "total_favorited", "user_info.liked_count", "user_info.total_favorited",
			)
		}
	}
	if len(postSnapshots) > 0 {
		postRuns := radarCreatorPostRuns(postSnapshots)
		currentPosts := radarCreatorPostsFromRun(postRuns[0], dataDir, now.AddDate(0, 0, -user.LookbackDays))
		creator.LastCheck = postSnapshots[0].FetchedAt
		creator.LastCount = len(currentPosts)
		creator.LastPages = len(postRuns[0])
		creator.TotalEngagement = radarCreatorEngagement(currentPosts)
		creator.LatestPosts = currentPosts
		if len(postRuns) > 1 {
			previousPosts := radarCreatorPostsFromRun(postRuns[1], dataDir, now.AddDate(0, 0, -user.LookbackDays))
			previousKeys := make(map[string]bool, len(previousPosts))
			for _, post := range previousPosts {
				previousKeys[radarCreatorPostKey(post)] = true
			}
			for _, post := range currentPosts {
				if !previousKeys[radarCreatorPostKey(post)] {
					creator.NewPosts++
				}
			}
			creator.EngagementDelta = creator.TotalEngagement - radarCreatorEngagement(previousPosts)
		}
	}

	// 融合本地归档 index.json：若已全量归档，自动注入本地封面/音频路径并补全作品集
	if idx, err := radar.LoadCreatorIndex(dataDir, user.Platform, user.UserID); err == nil && len(idx.Posts) > 0 {
		mediaMap := make(map[string]tikhub.CreatorPostMedia, len(idx.Posts))
		for _, post := range idx.Posts {
			mediaMap[post.AwemeID] = post
		}
		for i := range creator.LatestPosts {
			if m, ok := mediaMap[creator.LatestPosts[i].ID]; ok {
				creator.LatestPosts[i].CoverPath = m.CoverPath
				creator.LatestPosts[i].AudioPath = m.AudioPath
				if creator.LatestPosts[i].Play == 0 && m.Play > 0 {
					creator.LatestPosts[i].Play = m.Play
				}
				if creator.LatestPosts[i].Likes == 0 && m.Likes > 0 {
					creator.LatestPosts[i].Likes = m.Likes
				}
				if creator.LatestPosts[i].Comments == 0 && m.Comments > 0 {
					creator.LatestPosts[i].Comments = m.Comments
				}
				if creator.LatestPosts[i].Collects == 0 && m.Collects > 0 {
					creator.LatestPosts[i].Collects = m.Collects
				}
				if creator.LatestPosts[i].Shares == 0 && m.Shares > 0 {
					creator.LatestPosts[i].Shares = m.Shares
				}
				if creator.LatestPosts[i].DurationMS == 0 && m.DurationMS > 0 {
					creator.LatestPosts[i].DurationMS = m.DurationMS
				}
			}
		}
		if len(creator.LatestPosts) < len(idx.Posts) {
			seen := make(map[string]bool, len(creator.LatestPosts))
			for _, p := range creator.LatestPosts {
				seen[p.ID] = true
			}
			for _, post := range idx.Posts {
				if !seen[post.AwemeID] {
					seen[post.AwemeID] = true
					publishedAt := ""
					if post.CreateTime > 0 {
						publishedAt = time.Unix(post.CreateTime, 0).UTC().Format(time.RFC3339)
					}
					postURL := ""
					if user.Platform == "douyin" {
						postURL = "https://www.douyin.com/video/" + post.AwemeID
					}
					creator.LatestPosts = append(creator.LatestPosts, trendRadarCreatorPost{
						ID: post.AwemeID, Title: post.Title, URL: postURL, CoverURL: post.CoverURL,
						CoverPath: post.CoverPath, AudioPath: post.AudioPath,
						Play: post.Play, Likes: post.Likes, Comments: post.Comments,
						Collects: post.Collects, Shares: post.Shares, DurationMS: post.DurationMS,
						ObservedAt: publishedAt,
					})
				}
			}
		}
		sort.SliceStable(creator.LatestPosts, func(i, j int) bool {
			if creator.LatestPosts[i].ObservedAt != creator.LatestPosts[j].ObservedAt {
				return creator.LatestPosts[i].ObservedAt > creator.LatestPosts[j].ObservedAt
			}
			return creator.LatestPosts[i].ID > creator.LatestPosts[j].ID
		})
		if len(creator.LatestPosts) > creator.LastCount {
			creator.LastCount = len(creator.LatestPosts)
		}
		if creator.LastCheck == "" && idx.SyncedAt != "" {
			creator.LastCheck = idx.SyncedAt
		}
	}

	if creator.LastCheck == "" {
		creator.State = "never_synced"
		creator.Message = "已加入监测，尚未建立本地快照"
		return creator
	}
	checkedAt, err := time.Parse(time.RFC3339Nano, creator.LastCheck)
	if err != nil {
		checkedAt, err = time.Parse(time.RFC3339, creator.LastCheck)
	}
	if err != nil {
		creator.State = "failed"
		creator.Message = "最近监测时间无法解析"
		return creator
	}
	if now.Sub(checkedAt) > 72*time.Hour {
		creator.State = "stale"
		creator.Message = "本地快照已超过 72 小时"
		return creator
	}
	creator.State = "ready"
	if len(radarCreatorPostRuns(postSnapshots)) < 2 {
		creator.Message = "已建立基线；下次监测后显示变化"
	} else {
		creator.Message = "变化来自本地最近两次快照"
	}
	return creator
}

func radarCreatorPostRuns(snapshots []tikhub.CallSnapshot) [][]tikhub.CallSnapshot {
	runs := make([][]tikhub.CallSnapshot, 0)
	current := make([]tikhub.CallSnapshot, 0)
	for _, snapshot := range snapshots {
		current = append(current, snapshot)
		cursor := strings.TrimSpace(fmt.Sprint(snapshot.Parameters["max_cursor"]))
		if cursor == "" || cursor == "0" || cursor == "<nil>" {
			runs = append(runs, current)
			current = nil
		}
	}
	if len(current) > 0 {
		runs = append(runs, current)
	}
	return runs
}

func radarCreatorPostsFromRun(snapshots []tikhub.CallSnapshot, dataDir string, cutoff time.Time) []trendRadarCreatorPost {
	posts := make([]trendRadarCreatorPost, 0)
	seen := map[string]bool{}
	for index := len(snapshots) - 1; index >= 0; index-- {
		for _, post := range radarCreatorPosts(snapshots[index], dataDir, cutoff) {
			key := radarCreatorPostKey(post)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			posts = append(posts, post)
		}
	}
	sort.SliceStable(posts, func(i, j int) bool {
		return posts[i].ObservedAt > posts[j].ObservedAt
	})
	return posts
}

func radarCreatorPosts(snapshot tikhub.CallSnapshot, dataDir string, cutoff time.Time) []trendRadarCreatorPost {
	niche := trendRadarNiche{ID: "creator", Name: "博主监测"}
	items := radarCandidateItems(snapshot.Platform, snapshot.Response)
	posts := make([]trendRadarCreatorPost, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		signal := normalizeRadarSignal(niche, "", snapshot.Platform, snapshot.FetchedAt, dataDir, item)
		if signal.Title == "" {
			continue
		}
		publishedAt := radarPublishedAtFor(snapshot.Platform, item, snapshot.FetchedAt)
		if !cutoff.IsZero() {
			parsed, err := time.Parse(time.RFC3339Nano, publishedAt)
			if err != nil {
				parsed, err = time.Parse(time.RFC3339, publishedAt)
			}
			if err == nil && parsed.Before(cutoff) {
				continue
			}
		}
		id := ""
		switch snapshot.Platform {
		case "douyin":
			id = radarString(item, "aweme_id", "aweme_info.aweme_id")
		case "xhs":
			id = radarString(item, "note_id", "note_card.note_id", "id")
		}
		post := trendRadarCreatorPost{
			ID: id, Title: signal.Title, URL: signal.URL, CoverURL: signal.CoverURL,
			Play: signal.Play, Likes: signal.Likes, Collects: signal.Collects, Shares: signal.Shares,
			Comments:   signal.Comments,
			DurationMS: radarInt(item, "video.duration", "duration"),
			ObservedAt: publishedAt,
		}
		key := radarCreatorPostKey(post)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		posts = append(posts, post)
	}
	return posts
}

// radarPublishedAt returns when the work was published on its platform, or the
// fallback when the provider did not include it. It never guesses from the
// fetch time: "not published recently" and "not fetched recently" are
// different questions.
func radarPublishedAt(item map[string]interface{}, fallback string) string {
	return radarPublishedAtFor("", item, fallback)
}

func radarPublishedAtFor(platform string, item map[string]interface{}, fallback string) string {
	paths := []string{"create_time", "aweme_info.create_time"}
	switch platform {
	case "xhs":
		// "timestamp" is the note's publish time on the note object; update_time
		// is deliberately excluded because an edit is not a publish.
		paths = []string{"note.timestamp", "timestamp", "note_card.time", "note.time", "time"}
	case "youtube":
		paths = []string{"snippet.publishedAt", "publishedAt", "published_at"}
	case "x":
		paths = []string{"legacy.created_at", "created_at"}
	case "kuaishou":
		paths = []string{"timestamp", "photo.timestamp", "create_time"}
	case "wechat_video":
		paths = []string{"create_time", "object.create_time"}
	}
	var value interface{}
	for _, path := range paths {
		if candidate, ok := radarValue(item, path); ok && candidate != nil {
			value = candidate
			break
		}
	}
	switch typed := value.(type) {
	case json.Number:
		if number, err := typed.Int64(); err == nil && number > 0 {
			if number > 1_000_000_000_000 {
				number /= 1000
			}
			return time.Unix(number, 0).UTC().Format(time.RFC3339)
		}
	case float64:
		number := int64(typed)
		if number > 1_000_000_000_000 {
			number /= 1000
		}
		if number > 0 {
			return time.Unix(number, 0).UTC().Format(time.RFC3339)
		}
	case int64:
		if typed > 0 {
			return time.Unix(typed, 0).UTC().Format(time.RFC3339)
		}
	case string:
		if number, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil && number > 0 {
			if number > 1_000_000_000_000 {
				number /= 1000
			}
			return time.Unix(number, 0).UTC().Format(time.RFC3339)
		}
		for _, layout := range []string{time.RFC3339, time.RFC3339Nano, time.RubyDate, "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, strings.TrimSpace(typed)); err == nil {
				return parsed.UTC().Format(time.RFC3339)
			}
		}
	}
	return fallback
}

func radarCreatorPostKey(post trendRadarCreatorPost) string {
	if post.ID != "" {
		return post.ID
	}
	if post.URL != "" {
		return post.URL
	}
	return strings.ToLower(strings.TrimSpace(post.Title))
}

func radarCreatorEngagement(posts []trendRadarCreatorPost) int64 {
	var total int64
	for _, post := range posts {
		total += post.Likes + post.Collects + post.Shares
	}
	return total
}

func radarSources() []trendRadarSource {
	return []trendRadarSource{
		{Platform: "douyin", Name: "抖音", Mode: "关键词 + 博主", State: "ready", Message: "赛道搜索与博主作品均使用固定官方接口"},
		{Platform: "xhs", Name: "小红书", Mode: "关键词 + 博主", State: "ready", Message: "赛道搜索与博主作品均使用固定官方接口"},
		{Platform: "wechat_video", Name: "微信视频号", Mode: "关键词", State: "ready", Message: "用于赛道搜索；博主监控暂未开放"},
		{Platform: "kuaishou", Name: "快手", Mode: "关键词", State: "ready", Message: "用于赛道搜索；博主监控暂未开放"},
		{Platform: "wechat_mp", Name: "微信公众号", Mode: "关键词", State: "ready", Message: "用于文章赛道搜索与公众号观察"},
		{Platform: "youtube", Name: "YouTube", Mode: "关键词 + 字幕", State: "ready", Message: "结构化视频搜索；字幕按视频明确调用并长期保存"},
		{Platform: "x", Name: "X", Mode: "关键词", State: "ready", Message: "搜索正文与媒体封面；帖子详情按需调用"},
	}
}

// 搜索结果里的标题常带站点自己的高亮标记，例如视频号的
// `<em class="highlight">男人</em>不要欺负自己的妻子`。这些不是作品内容，
// 留着会让标题在界面上直接显示成 HTML 标签。
var radarMarkupTag = regexp.MustCompile(`</?em[^>]*>`)

func radarStripMarkup(text string) string {
	if !strings.Contains(text, "<") {
		return text
	}
	cleaned := radarMarkupTag.ReplaceAllString(text, "")
	return strings.TrimSpace(html.UnescapeString(cleaned))
}

func radarString(item map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, ok := radarValue(item, key)
		if !ok || value == nil {
			continue
		}
		switch value.(type) {
		case map[string]interface{}, []interface{}:
			continue
		}
		text := radarStripMarkup(strings.TrimSpace(fmt.Sprint(value)))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func radarInt(item map[string]interface{}, keys ...string) int64 {
	for _, key := range keys {
		value, ok := radarValue(item, key)
		if !ok || value == nil {
			continue
		}
		switch number := value.(type) {
		case float64:
			return int64(number)
		case int:
			return int64(number)
		case int64:
			return number
		case json.Number:
			if parsed, err := number.Int64(); err == nil {
				return parsed
			}
		default:
			if parsed, ok := parseRadarMetric(fmt.Sprint(value)); ok {
				return parsed
			}
		}
	}
	return 0
}

func parseRadarMetric(value string) (int64, bool) {
	text := strings.ToLower(strings.TrimSpace(value))
	if text == "" {
		return 0, false
	}
	text = strings.ReplaceAll(text, ",", "")
	if fields := strings.Fields(text); len(fields) > 0 {
		text = fields[0]
	}
	multiplier := float64(1)
	for suffix, scale := range map[string]float64{"k": 1e3, "m": 1e6, "b": 1e9, "万": 1e4, "亿": 1e8} {
		if strings.HasSuffix(text, suffix) {
			text = strings.TrimSuffix(text, suffix)
			multiplier = scale
			break
		}
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || number < 0 {
		return 0, false
	}
	return int64(number * multiplier), true
}

func radarValue(item map[string]interface{}, key string) (interface{}, bool) {
	parts := strings.Split(key, ".")
	var current interface{} = item
	for _, part := range parts {
		switch typed := current.(type) {
		case map[string]interface{}:
			value, ok := typed[part]
			if !ok {
				return nil, false
			}
			current = value
		case []interface{}:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return nil, false
			}
			current = typed[index]
		default:
			return nil, false
		}
	}
	return current, true
}

// creatorArchiveJobRetention keeps a finished job readable for a little while so
// the UI can still render the final state after a slow poll round trip.
const creatorArchiveJobRetention = 30 * time.Second

// creatorArchiveJob is shared between the archive goroutine and the polling HTTP
// handlers, so every field is guarded by mu. sync.Map only protects the pointer
// stored under the creator key; the struct fields need their own lock.
type creatorArchiveJob struct {
	mu       sync.Mutex
	status   string
	progress radar.CreatorArchiveProgress
	result   map[string]any
	errMsg   string
	doneAt   time.Time
}

// creatorArchiveJobSnapshot is a lock-free copy handed to HTTP responses. The
// embedded progress struct is a value type; result is copied so callers never
// alias the job's internal map.
type creatorArchiveJobSnapshot struct {
	status   string
	progress radar.CreatorArchiveProgress
	result   map[string]any
	errMsg   string
	doneAt   time.Time
}

func newCreatorArchiveJob() *creatorArchiveJob {
	return &creatorArchiveJob{status: "running"}
}

func (j *creatorArchiveJob) snapshot() creatorArchiveJobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	snapshot := creatorArchiveJobSnapshot{
		status:   j.status,
		progress: j.progress,
		errMsg:   j.errMsg,
		doneAt:   j.doneAt,
	}
	if j.result != nil {
		snapshot.result = make(map[string]any, len(j.result))
		for key, value := range j.result {
			snapshot.result[key] = value
		}
	}
	return snapshot
}

// updateProgress mutates the progress struct under the job lock; the callback
// mirrors the per-stage field writes the archive callback needs.
func (j *creatorArchiveJob) updateProgress(mutate func(*radar.CreatorArchiveProgress)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	mutate(&j.progress)
}

func (j *creatorArchiveJob) setDone(result map[string]any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = "done"
	j.result = result
	j.doneAt = time.Now()
}

func (j *creatorArchiveJob) setError(message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status = "error"
	j.errMsg = message
	j.doneAt = time.Now()
}

var creatorArchiveJobs sync.Map

// registerCreatorArchiveJob claims the per-creator archive slot. It returns
// (job, true) when this caller owns the run, or (nil, false) when a run for the
// same creator is still going.
//
// The claim has to be one atomic step: a plain LoadOrStore followed by Store lets
// two concurrent POSTs both observe a finished job and start two archives for one
// creator, which doubles the TikHub spend and races on the same files.
func registerCreatorArchiveJob(key string) (*creatorArchiveJob, bool) {
	job := newCreatorArchiveJob()
	for {
		actual, loaded := creatorArchiveJobs.LoadOrStore(key, job)
		if !loaded {
			return job, true
		}
		existing, ok := actual.(*creatorArchiveJob)
		if !ok || existing.snapshot().status == "running" {
			return nil, false
		}
		if creatorArchiveJobs.CompareAndSwap(key, actual, job) {
			return job, true
		}
		// CAS 失败说明另一个请求刚替换过，重新判定。
	}
}

// pruneFinishedCreatorArchiveJobs drops finished jobs the client never polled,
// so abandoned archives cannot accumulate forever. Only called when a new job is
// registered, so the sweep cost stays tied to archive starts.
func pruneFinishedCreatorArchiveJobs(now time.Time) {
	creatorArchiveJobs.Range(func(key, value any) bool {
		job, ok := value.(*creatorArchiveJob)
		if !ok {
			return true
		}
		snapshot := job.snapshot()
		if snapshot.status != "running" && now.Sub(snapshot.doneAt) > creatorArchiveJobRetention {
			creatorArchiveJobs.CompareAndDelete(key, value)
		}
		return true
	})
}

// handleAPITrendRadarCreatorArchive starts an asynchronous full-archive job and
// returns 202 immediately so the UI can poll live progress instead of blocking.
func (s *Server) handleAPITrendRadarCreatorArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDataAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "只支持 POST")
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "归档请求仅接受本机同源调用")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	platform := strings.TrimSpace(r.PathValue("platform"))
	userID := strings.TrimSpace(r.PathValue("user_id"))
	if platform != "douyin" || userID == "" || len(userID) > 256 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator", "全量归档当前仅支持抖音博主")
		return
	}

	var input struct {
		MaxPages  int  `json:"max_pages"`
		SkipMedia bool `json:"skip_media"`
		MediaOnly bool `json:"media_only"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&input)
	}

	watchlist, err := tikhub.LoadWatchlist(dataDir)
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "watchlist_unavailable", "无法读取本地监控名单")
		return
	}
	var watch tikhub.WatchUser
	found := false
	for _, candidate := range watchlist {
		if candidate.Platform == platform && candidate.UserID == userID {
			watch = candidate
			found = true
			break
		}
	}
	if !found {
		writeDataAPIError(w, http.StatusNotFound, "creator_not_found", "博主不在本地监控名单中")
		return
	}
	provider, err := tikhub.LoadProviderConfig(dataDir)
	if err != nil || !provider.Active || provider.TestStatus != tikhub.TestStatusPassed {
		writeDataAPIError(w, http.StatusConflict, "provider_inactive", "请先在设置中激活 TikHub API")
		return
	}

	key := platform + "-" + userID
	job, claimed := registerCreatorArchiveJob(key)
	if !claimed {
		writeDataAPIError(w, http.StatusConflict, "archive_already_running", "该博主正在归档中")
		return
	}
	pruneFinishedCreatorArchiveJobs(time.Now())

	go func() {
		database, openErr := db.Open(dataDir)
		if openErr != nil {
			job.setError("无法打开本地数据库")
			return
		}
		defer database.Close()
		if migrateErr := database.Migrate(); migrateErr != nil {
			job.setError("无法初始化本地数据库")
			return
		}

		summary, syncErr := radar.SyncCreatorAll(context.Background(), database, dataDir, watch, radar.CreatorSyncOptions{
			MaxPages:  input.MaxPages,
			Media:     !input.SkipMedia,
			MediaOnly: input.MediaOnly,
			OnProgress: func(stage string, done, total int) {
				job.updateProgress(func(progress *radar.CreatorArchiveProgress) {
					progress.Phase = stage
					if stage == "posts" {
						progress.PostsFetched = done
						progress.PostsTotalEstimate = done
					} else if stage == "media" {
						progress.CoversDone = done
						progress.CoversTotal = total
						progress.AudioDone = done
						progress.AudioTotal = total
					}
				})
			},
		})
		if syncErr != nil {
			job.setError(syncErr.Error())
			return
		}
		job.setDone(map[string]any{
			"ok":         true,
			"folder":     summary.Folder,
			"index_path": summary.IndexPath,
			"pages":      summary.Pages,
			"posts":      summary.Posts,
			"fetched":    summary.Fetched,
			"new_posts":  summary.NewPosts,
			"stopped":    summary.Stopped,
			"call_ids":   summary.CallIDs,
			"media": map[string]any{
				"total":   summary.Media.Total,
				"covers":  summary.Media.Covers,
				"audio":   summary.Media.Audio,
				"skipped": summary.Media.Skipped,
				"failed":  summary.Media.Failed,
			},
		})
	}()

	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"ok": true, "key": key, "accepted": true})
}

// handleAPITrendRadarCreatorArchiveStatus reports live archive progress so the UI
// can render a determinate bar and resume after a page reload.
func (s *Server) handleAPITrendRadarCreatorArchiveStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDataAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "只支持 GET")
		return
	}
	platform := strings.TrimSpace(r.PathValue("platform"))
	userID := strings.TrimSpace(r.PathValue("user_id"))
	if (platform != "douyin" && platform != "xhs") || userID == "" || len(userID) > 256 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator", "博主信息无效")
		return
	}
	key := platform + "-" + userID

	if value, ok := creatorArchiveJobs.Load(key); ok {
		job := value.(*creatorArchiveJob)
		snapshot := job.snapshot()
		if snapshot.status == "running" || time.Since(snapshot.doneAt) <= creatorArchiveJobRetention {
			writeJSON(w, map[string]any{
				"status":   snapshot.status,
				"progress": snapshot.progress,
				"result":   snapshot.result,
				"error":    snapshot.errMsg,
			})
			return
		}
		creatorArchiveJobs.CompareAndDelete(key, value)
	}

	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	if progress, ok := radar.LoadCreatorArchiveProgress(dataDir, platform, userID); ok {
		// 进度文件只在归档进行中存在（完成即删）。若它还在但很久没更新，
		// 说明上一次进程是崩溃/重启中断的，不能让 UI 永远停在"进行中"。
		if updated, parseErr := time.Parse(time.RFC3339Nano, progress.UpdatedAt); parseErr == nil &&
			time.Since(updated) <= 3*time.Minute {
			writeJSON(w, map[string]any{"status": "running", "progress": progress, "result": nil, "error": ""})
			return
		}
		radar.ClearCreatorArchiveProgress(dataDir, platform, userID)
	}
	writeJSON(w, map[string]any{"status": "idle", "progress": nil, "result": nil, "error": ""})
}

// handleAPITrendRadarCreatorArchiveFailures lists the assets that failed to
// download in the last media pass. The ledger lives on disk, so the UI can still
// show and retry them after the in-memory job has expired.
func (s *Server) handleAPITrendRadarCreatorArchiveFailures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDataAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "只支持 GET")
		return
	}
	platform := strings.TrimSpace(r.PathValue("platform"))
	userID := strings.TrimSpace(r.PathValue("user_id"))
	if (platform != "douyin" && platform != "xhs") || userID == "" || len(userID) > 256 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator", "博主信息无效")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	failures, ok := radar.LoadCreatorArchiveFailures(dataDir, platform, userID)
	if !ok {
		// 没有台账就是「没有失败项」，不是错误。
		failures = []radar.CreatorArchiveFailure{}
	}
	writeJSON(w, map[string]any{"ok": true, "failures": failures, "total": len(failures)})
}

// handleAPITrendRadarCreatorOpenFolder reveals the creator's local folder in the system file manager.
func (s *Server) handleAPITrendRadarCreatorOpenFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDataAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "只支持 POST")
		return
	}
	if !sameOriginLoopbackRequest(r) {
		writeDataAPIError(w, http.StatusForbidden, "origin_denied", "仅接受本机同源调用")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	platform := strings.TrimSpace(r.PathValue("platform"))
	userID := strings.TrimSpace(r.PathValue("user_id"))
	if (platform != "douyin" && platform != "xhs") || userID == "" || len(userID) > 256 {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_creator", "博主信息无效")
		return
	}
	folder := tikhub.CreatorFolder(dataDir, platform, userID)
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		writeDataAPIError(w, http.StatusNotFound, "folder_not_found", "博主本地文件夹不存在")
		return
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", folder)
	case "windows":
		cmd = exec.Command("explorer", folder)
	default:
		cmd = exec.Command("xdg-open", folder)
	}
	if err := cmd.Start(); err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "open_failed", "无法打开系统文件管理器")
		return
	}
	writeJSON(w, map[string]any{"ok": true, "folder": folder})
}
