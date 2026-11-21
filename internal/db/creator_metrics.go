package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	creatorMetricWindowDays = 7
	maxCreatorMetricRawJSON = 64 << 10
)

// CreatorProfileEvidenceCode marks a follower total that came from a real
// TikHub creator_profile response. Unlike the REVIEWED_* codes it is written by
// the collector itself, and it is the only evidence code that may carry a
// verified follower count with range_verified=0: the call proves a current
// follower number, not a verified seven-day window.
const CreatorProfileEvidenceCode = "TIKHUB_CREATOR_PROFILE"

var creatorMetricPlatforms = map[string]struct{}{
	"douyin": {}, "xhs": {}, "wechat_video": {}, "kuaishou": {},
	"tiktok": {}, "youtube": {}, "meta": {}, "wechat_mp": {}, "csdn": {},
}

// creatorMetricCollectionPlatforms is intentionally broader than the
// extractor catalog. A configured account must be able to retain the honest
// outcome of its data workflow (including a manual handoff) even before a
// reviewed seven-day extractor exists for that platform.
var creatorMetricCollectionPlatforms = map[string]struct{}{
	"acfun": {}, "amazon_seller": {}, "baijiahao": {}, "bilibili": {}, "csdn": {},
	"douyin": {}, "douyin_shop": {}, "facebook": {}, "instagram": {}, "jianshu": {},
	"juejin": {}, "kuaishou": {}, "meta": {}, "omtencent": {}, "pdd": {},
	"shopee": {}, "shopify": {}, "sina": {}, "taobao": {}, "tiktok": {},
	"tiktok_shop": {}, "toutiao": {}, "wechat_mp": {}, "wechat_video": {}, "weishi": {},
	"x": {}, "xhs": {}, "youtube": {}, "zhihu": {},
}

var creatorMetricPlatformSuffixes = map[string][]string{
	"douyin":       {"douyin.com"},
	"xhs":          {"xiaohongshu.com"},
	"wechat_video": {"channels.weixin.qq.com", "weixin.qq.com"},
	"kuaishou":     {"kuaishou.com"},
	"tiktok":       {"tiktok.com"},
	"youtube":      {"youtube.com"},
	"meta":         {"facebook.com", "instagram.com", "meta.com"},
	"wechat_mp":    {"weixin.qq.com"},
	"csdn":         {"csdn.net"},
}

type CreatorMetricSnapshotInput struct {
	AccountID         int64
	PlatformCode      string
	SnapshotDate      string
	WindowDays        int
	RangeVerified     bool
	Followers         int64
	FollowersVerified bool
	Views             int64
	Likes             int64
	Comments          int64
	Shares            int64
	Favorites         int64
	Posts             int64
	EvidenceCode      string
	RawJSON           string
	CollectedAt       string
}

type CreatorMetricCollectionStateInput struct {
	AccountID     int64
	PlatformCode  string
	State         string
	ErrorCode     string
	EvidenceCode  string
	RangeVerified bool
	PostCount     int
	CollectedAt   string
}

type CreatorPostMetricInput struct {
	ExternalPostID string
	Title          string
	PublishedAt    string
	PostURL        string
	Views          int64
	Likes          int64
	Comments       int64
	Shares         int64
	Favorites      int64
	RawJSON        string
}

type CreatorMetricSnapshot struct {
	ID                int64  `json:"id"`
	AccountID         int64  `json:"account_id"`
	AccountName       string `json:"account_name"`
	PlatformCode      string `json:"platform_code"`
	SnapshotDate      string `json:"snapshot_date"`
	WindowDays        int    `json:"window_days"`
	RangeVerified     bool   `json:"range_verified"`
	Followers         int64  `json:"followers"`
	FollowersVerified bool   `json:"followers_verified"`
	Views             int64  `json:"views"`
	Likes             int64  `json:"likes"`
	Comments          int64  `json:"comments"`
	Shares            int64  `json:"shares"`
	Favorites         int64  `json:"favorites"`
	Posts             int64  `json:"posts"`
	EvidenceCode      string `json:"evidence_code"`
	CollectedAt       string `json:"collected_at"`
}

type CreatorMetricCollectionState struct {
	AccountID     int64  `json:"account_id"`
	AccountName   string `json:"account_name"`
	PlatformCode  string `json:"platform_code"`
	State         string `json:"state"`
	ErrorCode     string `json:"error_code,omitempty"`
	EvidenceCode  string `json:"evidence_code,omitempty"`
	RangeVerified bool   `json:"range_verified"`
	PostCount     int    `json:"post_count"`
	CollectedAt   string `json:"collected_at"`
	UpdatedAt     string `json:"updated_at"`
}

type CreatorPostMetric struct {
	ID             int64  `json:"id"`
	SnapshotID     int64  `json:"snapshot_id"`
	AccountID      int64  `json:"account_id"`
	PlatformCode   string `json:"platform_code"`
	ExternalPostID string `json:"external_post_id"`
	Title          string `json:"title"`
	PublishedAt    string `json:"published_at"`
	PostURL        string `json:"post_url"`
	Views          int64  `json:"views"`
	Likes          int64  `json:"likes"`
	Comments       int64  `json:"comments"`
	Shares         int64  `json:"shares"`
	Favorites      int64  `json:"favorites"`
}

type CreatorMetricDay struct {
	Date      string `json:"date"`
	Accounts  int    `json:"accounts"`
	Views     int64  `json:"views"`
	Likes     int64  `json:"likes"`
	Comments  int64  `json:"comments"`
	Shares    int64  `json:"shares"`
	Favorites int64  `json:"favorites"`
	Posts     int64  `json:"posts"`
}

// CreatorMetricAccountDay preserves account ownership for browser-scoped UI aggregation.
// Daily remains the backward-compatible all-account summary.
type CreatorMetricAccountDay struct {
	AccountID    int64  `json:"account_id"`
	PlatformCode string `json:"platform_code"`
	Date         string `json:"date"`
	Views        int64  `json:"views"`
	Likes        int64  `json:"likes"`
	Comments     int64  `json:"comments"`
	Shares       int64  `json:"shares"`
	Favorites    int64  `json:"favorites"`
	Posts        int64  `json:"posts"`
}

type CreatorMetricPlatformSummary struct {
	PlatformCode string  `json:"platform_code"`
	Accounts     int     `json:"accounts"`
	Views        int64   `json:"views"`
	Interactions int64   `json:"interactions"`
	Posts        int64   `json:"posts"`
	Engagement   float64 `json:"engagement_rate"`
}

type CreatorMetricOverview struct {
	WindowDays   int                            `json:"window_days"`
	FromDate     string                         `json:"from_date"`
	ToDate       string                         `json:"to_date"`
	Latest       []CreatorMetricSnapshot        `json:"latest"`
	Daily        []CreatorMetricDay             `json:"daily"`
	AccountDaily []CreatorMetricAccountDay      `json:"account_daily"`
	Platforms    []CreatorMetricPlatformSummary `json:"platforms"`
	TopPosts     []CreatorPostMetric            `json:"top_posts"`
	Collections  []CreatorMetricCollectionState `json:"collections"`
	Insights     []string                       `json:"insights"`
}

type CreatorMetricPreference struct {
	HistoryDays   int    `json:"history_days"`
	RetentionDays int    `json:"retention_days"`
	UpdatedAt     string `json:"updated_at"`
}

func (db *DB) CreatorMetricPreferences() (*CreatorMetricPreference, error) {
	preference := &CreatorMetricPreference{}
	if err := db.QueryRow(`SELECT history_days,retention_days,updated_at FROM creator_metric_preferences WHERE id=1`).Scan(
		&preference.HistoryDays, &preference.RetentionDays, &preference.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return preference, nil
}

func (db *DB) UpdateCreatorMetricPreferences(historyDays, retentionDays int) (*CreatorMetricPreference, error) {
	if !validCreatorMetricHistoryDays(historyDays) || !validCreatorMetricRetentionDays(retentionDays) || retentionDays < historyDays {
		return nil, fmt.Errorf("invalid creator metric preferences")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE creator_metric_preferences SET history_days=?,retention_days=?,updated_at=? WHERE id=1`, historyDays, retentionDays, now); err != nil {
		return nil, err
	}
	var latestDate string
	if err := tx.QueryRow(`SELECT COALESCE(MAX(snapshot_date),'') FROM creator_metric_snapshots`).Scan(&latestDate); err != nil {
		return nil, err
	}
	if latestDate != "" {
		parsedLatest, _ := time.Parse("2006-01-02", latestDate)
		cutoffDate := parsedLatest.AddDate(0, 0, -(retentionDays - 1)).Format("2006-01-02")
		if _, err := tx.Exec(`DELETE FROM creator_metric_snapshots WHERE snapshot_date<?`, cutoffDate); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &CreatorMetricPreference{HistoryDays: historyDays, RetentionDays: retentionDays, UpdatedAt: now}, nil
}

func validCreatorMetricHistoryDays(days int) bool {
	return days == 7 || days == 30 || days == 90
}

func validCreatorMetricRetentionDays(days int) bool {
	return days == 30 || days == 90 || days == 180 || days == 365
}

// CreatorMetricAccountID resolves the enabled account that represents a
// creator on one platform. Trend-radar watch entries and metric accounts are
// linked by the platform identifier (a douyin sec_user_id or an xhs user_id),
// so a creator with no such account has no metric identity: callers get an
// error and must report it instead of inventing an account row or writing the
// snapshot under someone else's account.
func (db *DB) CreatorMetricAccountID(platformCode, identifier string) (int64, error) {
	if _, ok := creatorMetricPlatforms[platformCode]; !ok {
		return 0, fmt.Errorf("unsupported creator metric platform")
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return 0, fmt.Errorf("creator metric account identifier is empty")
	}
	var accountID int64
	err := db.QueryRow(`SELECT id FROM accounts
		WHERE enabled=1 AND platform_code=? AND LOWER(TRIM(account_identifier))=LOWER(?)
		ORDER BY id LIMIT 1`, platformCode, identifier).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("creator metric account not linked for %s", platformCode)
	}
	if err != nil {
		return 0, err
	}
	return accountID, nil
}

func (db *DB) UpsertCreatorMetricCollectionState(input CreatorMetricCollectionStateInput) (*CreatorMetricCollectionState, error) {
	if input.AccountID <= 0 || input.PostCount < 0 {
		return nil, fmt.Errorf("invalid creator metric collection state")
	}
	if _, ok := creatorMetricCollectionPlatforms[input.PlatformCode]; !ok {
		return nil, fmt.Errorf("unsupported creator metric platform")
	}
	if input.State != "observed" && input.State != "skipped" && input.State != "unknown" && input.State != "manual_takeover" {
		return nil, fmt.Errorf("invalid creator metric collection status")
	}
	if input.State == "observed" && !input.RangeVerified {
		return nil, fmt.Errorf("observed creator metric collection requires verified range")
	}
	if !validCreatorMetricCode(input.ErrorCode) || !validCreatorMetricCode(input.EvidenceCode) {
		return nil, fmt.Errorf("invalid creator metric collection code")
	}
	var accountPlatform, accountName string
	if err := db.QueryRow(`SELECT platform_code,name FROM accounts WHERE id=? AND enabled=1`, input.AccountID).Scan(&accountPlatform, &accountName); err != nil {
		return nil, fmt.Errorf("creator metric account unavailable")
	}
	if accountPlatform != input.PlatformCode {
		return nil, fmt.Errorf("creator metric account platform mismatch")
	}
	if input.CollectedAt == "" {
		input.CollectedAt = time.Now().UTC().Format(time.RFC3339)
	} else if _, err := time.Parse(time.RFC3339, input.CollectedAt); err != nil {
		return nil, fmt.Errorf("invalid creator metric collected_at")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO creator_metric_collection_states
		(account_id,platform_code,state,error_code,evidence_code,range_verified,post_count,collected_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(account_id) DO UPDATE SET platform_code=excluded.platform_code,state=excluded.state,
		error_code=excluded.error_code,evidence_code=excluded.evidence_code,range_verified=excluded.range_verified,
		post_count=excluded.post_count,collected_at=excluded.collected_at,updated_at=excluded.updated_at`,
		input.AccountID, input.PlatformCode, input.State, input.ErrorCode, input.EvidenceCode,
		input.RangeVerified, input.PostCount, input.CollectedAt, now); err != nil {
		return nil, err
	}
	return &CreatorMetricCollectionState{
		AccountID: input.AccountID, AccountName: accountName, PlatformCode: input.PlatformCode,
		State: input.State, ErrorCode: input.ErrorCode, EvidenceCode: input.EvidenceCode,
		RangeVerified: input.RangeVerified, PostCount: input.PostCount,
		CollectedAt: input.CollectedAt, UpdatedAt: now,
	}, nil
}

func (db *DB) UpsertCreatorMetricSnapshot(input CreatorMetricSnapshotInput, posts []CreatorPostMetricInput) (*CreatorMetricSnapshot, error) {
	if err := validateCreatorMetricInput(input, posts); err != nil {
		return nil, err
	}
	if input.RawJSON == "" {
		input.RawJSON = "{}"
	}
	for index := range posts {
		if posts[index].RawJSON == "" {
			posts[index].RawJSON = "{}"
		}
	}
	var accountPlatform, accountName string
	if err := db.QueryRow(`SELECT platform_code,name FROM accounts WHERE id=? AND enabled=1`, input.AccountID).Scan(&accountPlatform, &accountName); err != nil {
		return nil, fmt.Errorf("creator metric account unavailable")
	}
	if accountPlatform != input.PlatformCode {
		return nil, fmt.Errorf("creator metric account platform mismatch")
	}
	if input.CollectedAt == "" {
		input.CollectedAt = time.Now().UTC().Format(time.RFC3339)
	} else if _, err := time.Parse(time.RFC3339, input.CollectedAt); err != nil {
		return nil, fmt.Errorf("invalid creator metric collected_at")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// 同一天可能先落一条已验证的七天区间行（REVIEWED_ANALYTICS_DOM），后落一条
	// profile 快照（TIKHUB_CREATOR_PROFILE，range_verified=0）。profile 只证明了
	// 「当前关注数」，对 views/likes/... 一无所知——直接覆盖会把当天真实浏览
	// 清零。因此指标列只在「入参已验证，或库里这行本来就没验证过」时才写。
	// 关注数与 raw_json 属于 profile 快照自己的产出，始终更新。
	_, err = tx.Exec(`INSERT INTO creator_metric_snapshots
		(account_id,platform_code,snapshot_date,window_days,range_verified,followers,followers_verified,views,likes,comments,shares,favorites,posts,evidence_code,raw_json,collected_at,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(account_id,snapshot_date,window_days) DO UPDATE SET
		platform_code=excluded.platform_code,
		range_verified=CASE WHEN excluded.range_verified=1 OR creator_metric_snapshots.range_verified=0
			THEN excluded.range_verified ELSE creator_metric_snapshots.range_verified END,
		views=CASE WHEN excluded.range_verified=1 OR creator_metric_snapshots.range_verified=0
			THEN excluded.views ELSE creator_metric_snapshots.views END,
		likes=CASE WHEN excluded.range_verified=1 OR creator_metric_snapshots.range_verified=0
			THEN excluded.likes ELSE creator_metric_snapshots.likes END,
		comments=CASE WHEN excluded.range_verified=1 OR creator_metric_snapshots.range_verified=0
			THEN excluded.comments ELSE creator_metric_snapshots.comments END,
		shares=CASE WHEN excluded.range_verified=1 OR creator_metric_snapshots.range_verified=0
			THEN excluded.shares ELSE creator_metric_snapshots.shares END,
		favorites=CASE WHEN excluded.range_verified=1 OR creator_metric_snapshots.range_verified=0
			THEN excluded.favorites ELSE creator_metric_snapshots.favorites END,
		posts=CASE WHEN excluded.range_verified=1 OR creator_metric_snapshots.range_verified=0
			THEN excluded.posts ELSE creator_metric_snapshots.posts END,
		evidence_code=CASE WHEN excluded.range_verified=1 OR creator_metric_snapshots.range_verified=0
			THEN excluded.evidence_code ELSE creator_metric_snapshots.evidence_code END,
		followers=excluded.followers,followers_verified=excluded.followers_verified,
		raw_json=excluded.raw_json,collected_at=excluded.collected_at,updated_at=excluded.updated_at`,
		input.AccountID, input.PlatformCode, input.SnapshotDate, input.WindowDays, input.RangeVerified,
		input.Followers, input.FollowersVerified, input.Views, input.Likes, input.Comments, input.Shares, input.Favorites,
		input.Posts, input.EvidenceCode, input.RawJSON, input.CollectedAt, now, now)
	if err != nil {
		return nil, err
	}
	var snapshotID int64
	if err := tx.QueryRow(`SELECT id FROM creator_metric_snapshots WHERE account_id=? AND snapshot_date=? AND window_days=?`, input.AccountID, input.SnapshotDate, input.WindowDays).Scan(&snapshotID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM creator_post_metric_snapshots WHERE snapshot_id=?`, snapshotID); err != nil {
		return nil, err
	}
	for _, post := range posts {
		postKey := creatorPostKey(post)
		if _, err := tx.Exec(`INSERT INTO creator_post_metric_snapshots
			(snapshot_id,account_id,platform_code,post_key,external_post_id,title,published_at,post_url,views,likes,comments,shares,favorites,raw_json,created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, snapshotID, input.AccountID, input.PlatformCode, postKey,
			post.ExternalPostID, post.Title, post.PublishedAt, post.PostURL, post.Views, post.Likes,
			post.Comments, post.Shares, post.Favorites, post.RawJSON, now); err != nil {
			return nil, err
		}
	}
	var retentionDays int
	if err := tx.QueryRow(`SELECT retention_days FROM creator_metric_preferences WHERE id=1`).Scan(&retentionDays); err != nil {
		return nil, err
	}
	snapshotDate, _ := time.Parse("2006-01-02", input.SnapshotDate)
	cutoffDate := snapshotDate.AddDate(0, 0, -(retentionDays - 1)).Format("2006-01-02")
	if _, err := tx.Exec(`DELETE FROM creator_metric_snapshots WHERE snapshot_date<?`, cutoffDate); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &CreatorMetricSnapshot{
		ID: snapshotID, AccountID: input.AccountID, AccountName: accountName, PlatformCode: input.PlatformCode,
		SnapshotDate: input.SnapshotDate, WindowDays: input.WindowDays, RangeVerified: input.RangeVerified,
		Followers: input.Followers, FollowersVerified: input.FollowersVerified,
		Views: input.Views, Likes: input.Likes, Comments: input.Comments,
		Shares: input.Shares, Favorites: input.Favorites, Posts: input.Posts,
		EvidenceCode: input.EvidenceCode, CollectedAt: input.CollectedAt,
	}, nil
}

func (db *DB) CreatorMetricOverview(windowDays int, now time.Time) (*CreatorMetricOverview, error) {
	if !validCreatorMetricHistoryDays(windowDays) {
		return nil, fmt.Errorf("creator metric history must be 7, 30, or 90 days")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	toDate := now.Format("2006-01-02")
	fromDate := now.AddDate(0, 0, -(windowDays - 1)).Format("2006-01-02")
	overview := &CreatorMetricOverview{WindowDays: windowDays, FromDate: fromDate, ToDate: toDate, Latest: []CreatorMetricSnapshot{}, Daily: []CreatorMetricDay{}, AccountDaily: []CreatorMetricAccountDay{}, Platforms: []CreatorMetricPlatformSummary{}, TopPosts: []CreatorPostMetric{}, Collections: []CreatorMetricCollectionState{}, Insights: []string{}}
	collectionRows, err := db.Query(`WITH account_bindings AS (
		SELECT a.*,
			EXISTS(SELECT 1 FROM accounts canonical
				WHERE canonical.enabled=1 AND canonical.login_state='logged_in'
				AND canonical.platform_code=a.platform_code
				AND LOWER(TRIM(canonical.account_identifier))=LOWER(TRIM(a.account_identifier))
				AND TRIM(a.account_identifier)<>'' AND canonical.identity_id<a.identity_id) AS duplicate_binding
		FROM accounts a
	)
		SELECT a.id,a.name,a.platform_code,
		CASE WHEN a.duplicate_binding=1 THEN 'skipped' ELSE COALESCE(c.state,'not_collected') END,
		CASE WHEN a.duplicate_binding=1 THEN 'DUPLICATE_ACCOUNT_BINDING' ELSE COALESCE(c.error_code,'') END,
		CASE WHEN a.duplicate_binding=1 THEN '' ELSE COALESCE(c.evidence_code,'') END,
		CASE WHEN a.duplicate_binding=1 THEN 0 ELSE COALESCE(c.range_verified,0) END,
		CASE WHEN a.duplicate_binding=1 THEN 0 ELSE COALESCE(c.post_count,0) END,
		CASE WHEN a.duplicate_binding=1 THEN '' ELSE COALESCE(c.collected_at,'') END,
		CASE WHEN a.duplicate_binding=1 THEN '' ELSE COALESCE(c.updated_at,'') END
		FROM account_bindings a LEFT JOIN creator_metric_collection_states c ON c.account_id=a.id
		WHERE a.enabled=1 AND (c.account_id IS NOT NULL OR a.login_state='logged_in')
		ORDER BY a.platform_code,a.id`)
	if err != nil {
		return nil, err
	}
	for collectionRows.Next() {
		var item CreatorMetricCollectionState
		if err := collectionRows.Scan(&item.AccountID, &item.AccountName, &item.PlatformCode, &item.State,
			&item.ErrorCode, &item.EvidenceCode, &item.RangeVerified, &item.PostCount,
			&item.CollectedAt, &item.UpdatedAt); err != nil {
			collectionRows.Close()
			return nil, err
		}
		overview.Collections = append(overview.Collections, item)
	}
	if err := collectionRows.Close(); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT s.id,s.account_id,a.name,s.platform_code,s.snapshot_date,s.window_days,s.range_verified,
		s.followers,s.followers_verified,s.views,s.likes,s.comments,s.shares,s.favorites,s.posts,s.evidence_code,s.collected_at
		FROM creator_metric_snapshots s JOIN accounts a ON a.id=s.account_id
		JOIN (SELECT account_id,MAX(id) id FROM creator_metric_snapshots WHERE snapshot_date BETWEEN ? AND ? AND window_days=? AND range_verified=1 GROUP BY account_id) latest
		ON latest.account_id=s.account_id AND latest.id=s.id
		ORDER BY s.platform_code,s.account_id`, fromDate, toDate, creatorMetricWindowDays)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item CreatorMetricSnapshot
		if err := rows.Scan(&item.ID, &item.AccountID, &item.AccountName, &item.PlatformCode, &item.SnapshotDate,
			&item.WindowDays, &item.RangeVerified, &item.Followers, &item.FollowersVerified, &item.Views, &item.Likes, &item.Comments,
			&item.Shares, &item.Favorites, &item.Posts, &item.EvidenceCode, &item.CollectedAt); err != nil {
			rows.Close()
			return nil, err
		}
		overview.Latest = append(overview.Latest, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	dailyRows, err := db.Query(`SELECT snapshot_date,COUNT(DISTINCT account_id),SUM(views),SUM(likes),SUM(comments),SUM(shares),SUM(favorites),SUM(posts)
		FROM creator_metric_snapshots WHERE snapshot_date BETWEEN ? AND ? AND window_days=? AND range_verified=1
		GROUP BY snapshot_date ORDER BY snapshot_date`, fromDate, toDate, creatorMetricWindowDays)
	if err != nil {
		return nil, err
	}
	for dailyRows.Next() {
		var day CreatorMetricDay
		if err := dailyRows.Scan(&day.Date, &day.Accounts, &day.Views, &day.Likes, &day.Comments, &day.Shares, &day.Favorites, &day.Posts); err != nil {
			dailyRows.Close()
			return nil, err
		}
		overview.Daily = append(overview.Daily, day)
	}
	if err := dailyRows.Close(); err != nil {
		return nil, err
	}

	accountDailyRows, err := db.Query(`SELECT account_id,platform_code,snapshot_date,views,likes,comments,shares,favorites,posts
		FROM creator_metric_snapshots
		WHERE snapshot_date BETWEEN ? AND ? AND window_days=? AND range_verified=1
		ORDER BY snapshot_date,account_id`, fromDate, toDate, creatorMetricWindowDays)
	if err != nil {
		return nil, err
	}
	for accountDailyRows.Next() {
		var day CreatorMetricAccountDay
		if err := accountDailyRows.Scan(&day.AccountID, &day.PlatformCode, &day.Date, &day.Views, &day.Likes,
			&day.Comments, &day.Shares, &day.Favorites, &day.Posts); err != nil {
			accountDailyRows.Close()
			return nil, err
		}
		overview.AccountDaily = append(overview.AccountDaily, day)
	}
	if err := accountDailyRows.Close(); err != nil {
		return nil, err
	}

	platforms := make(map[string]*CreatorMetricPlatformSummary)
	for _, item := range overview.Latest {
		summary := platforms[item.PlatformCode]
		if summary == nil {
			summary = &CreatorMetricPlatformSummary{PlatformCode: item.PlatformCode}
			platforms[item.PlatformCode] = summary
		}
		summary.Accounts++
		summary.Views += item.Views
		summary.Interactions += item.Likes + item.Comments + item.Shares + item.Favorites
		summary.Posts += item.Posts
	}
	for _, summary := range platforms {
		if summary.Views > 0 {
			summary.Engagement = float64(summary.Interactions) / float64(summary.Views)
		}
		overview.Platforms = append(overview.Platforms, *summary)
	}
	sort.Slice(overview.Platforms, func(i, j int) bool { return overview.Platforms[i].Views > overview.Platforms[j].Views })

	postRows, err := db.Query(`SELECT p.id,p.snapshot_id,p.account_id,p.platform_code,p.external_post_id,p.title,p.published_at,p.post_url,p.views,p.likes,p.comments,p.shares,p.favorites
		FROM creator_post_metric_snapshots p JOIN creator_metric_snapshots s ON s.id=p.snapshot_id
		WHERE s.snapshot_date BETWEEN ? AND ? AND s.window_days=? AND s.range_verified=1
		AND p.id IN (
			SELECT MAX(p2.id) FROM creator_post_metric_snapshots p2
			JOIN creator_metric_snapshots s2 ON s2.id=p2.snapshot_id
			WHERE s2.snapshot_date BETWEEN ? AND ? AND s2.window_days=? AND s2.range_verified=1
			GROUP BY p2.account_id,p2.platform_code,p2.post_key
		)
		ORDER BY p.views DESC,(p.likes+p.comments+p.shares+p.favorites) DESC,p.id DESC LIMIT 20`,
		fromDate, toDate, creatorMetricWindowDays, fromDate, toDate, creatorMetricWindowDays)
	if err != nil {
		return nil, err
	}
	for postRows.Next() {
		var post CreatorPostMetric
		if err := postRows.Scan(&post.ID, &post.SnapshotID, &post.AccountID, &post.PlatformCode, &post.ExternalPostID,
			&post.Title, &post.PublishedAt, &post.PostURL, &post.Views, &post.Likes, &post.Comments,
			&post.Shares, &post.Favorites); err != nil {
			postRows.Close()
			return nil, err
		}
		overview.TopPosts = append(overview.TopPosts, post)
	}
	if err := postRows.Close(); err != nil {
		return nil, err
	}
	overview.Insights = creatorMetricInsights(overview)
	return overview, nil
}

func validateCreatorMetricInput(input CreatorMetricSnapshotInput, posts []CreatorPostMetricInput) error {
	if input.AccountID <= 0 || input.WindowDays != creatorMetricWindowDays {
		return fmt.Errorf("invalid creator metric identity or window")
	}
	if _, ok := creatorMetricPlatforms[input.PlatformCode]; !ok {
		return fmt.Errorf("unsupported creator metric platform")
	}
	if _, err := time.Parse("2006-01-02", input.SnapshotDate); err != nil {
		return fmt.Errorf("invalid creator metric date")
	}
	if input.EvidenceCode != "REVIEWED_ANALYTICS_DOM" && input.EvidenceCode != "REVIEWED_ANALYTICS_RESOURCE" &&
		input.EvidenceCode != "REVIEWED_CONTENT_DOM" && input.EvidenceCode != "REVIEWED_CONTENT_RESOURCE" &&
		input.EvidenceCode != "REVIEWED_CONTENT_RESOURCE_EMPTY" && input.EvidenceCode != CreatorProfileEvidenceCode {
		return fmt.Errorf("invalid creator metric evidence")
	}
	// TIKHUB_CREATOR_PROFILE is a point-in-time profile read: it proves a current
	// follower total, not a verified seven-day window, so it is the one evidence
	// code allowed to leave range_verified at 0.
	if !input.RangeVerified && input.EvidenceCode != CreatorProfileEvidenceCode {
		return fmt.Errorf("creator metric snapshot requires verified seven-day range")
	}
	if input.EvidenceCode == CreatorProfileEvidenceCode && !input.FollowersVerified {
		return fmt.Errorf("creator profile evidence requires a verified follower value")
	}
	if (input.EvidenceCode == "REVIEWED_CONTENT_DOM" || input.EvidenceCode == "REVIEWED_CONTENT_RESOURCE") && len(posts) == 0 {
		return fmt.Errorf("creator content evidence requires measured posts")
	}
	if input.EvidenceCode == "REVIEWED_CONTENT_RESOURCE_EMPTY" &&
		((input.PlatformCode != "douyin" && input.PlatformCode != "kuaishou") || !input.RangeVerified || len(posts) != 0 ||
			input.Views+input.Likes+input.Comments+input.Shares+input.Favorites+input.Posts != 0) {
		return fmt.Errorf("empty creator content evidence requires a complete zero-value Douyin or Kuaishou window")
	}
	if hasNegativeMetric(input.Followers, input.Views, input.Likes, input.Comments, input.Shares, input.Favorites, input.Posts) {
		return fmt.Errorf("creator metrics must be non-negative")
	}
	if err := validateCreatorMetricJSON(input.RawJSON); err != nil {
		return err
	}
	if input.EvidenceCode == "REVIEWED_ANALYTICS_RESOURCE" {
		if err := validateCreatorMetricResourceProvenance(input.RawJSON, input.PlatformCode); err != nil {
			return err
		}
	}
	if len(posts) > 100 {
		return fmt.Errorf("too many creator post metrics")
	}
	for _, post := range posts {
		if strings.TrimSpace(post.ExternalPostID+post.Title) == "" || utf8.RuneCountInString(post.Title) > 240 || hasNegativeMetric(post.Views, post.Likes, post.Comments, post.Shares, post.Favorites) {
			return fmt.Errorf("invalid creator post metric")
		}
		if post.Views+post.Likes+post.Comments+post.Shares+post.Favorites == 0 {
			if strings.TrimSpace(post.ExternalPostID) == "" {
				return fmt.Errorf("zero-interaction creator post requires a stable external id")
			}
			if _, err := time.Parse(time.RFC3339, strings.TrimSpace(post.PublishedAt)); err != nil {
				return fmt.Errorf("zero-interaction creator post requires a verified publish time")
			}
		}
		if post.PostURL != "" && !safeCreatorMetricURL(post.PostURL, input.PlatformCode) {
			return fmt.Errorf("invalid creator post url")
		}
		if err := validateCreatorMetricJSON(post.RawJSON); err != nil {
			return err
		}
	}
	return nil
}

func validateCreatorMetricResourceProvenance(raw, platformCode string) error {
	var evidence struct {
		ResourceDiagnostics []struct {
			Resource    string   `json:"resource"`
			RangeHints  []string `json:"range_hints"`
			NumericKeys []string `json:"numeric_keys"`
		} `json:"resource_diagnostics"`
	}
	if err := json.Unmarshal([]byte(raw), &evidence); err != nil || len(evidence.ResourceDiagnostics) == 0 {
		return fmt.Errorf("creator metric resource provenance missing")
	}
	for _, diagnostic := range evidence.ResourceDiagnostics {
		if !safeCreatorMetricURL(diagnostic.Resource, platformCode) || len(diagnostic.RangeHints) == 0 || len(diagnostic.NumericKeys) == 0 {
			return fmt.Errorf("creator metric resource provenance invalid")
		}
		for _, hint := range diagnostic.RangeHints {
			if hint != "explicit_7_day_marker" && hint != "query_7_day_range" &&
				hint != "resource_complete_7_day_window" && hint != "resource_explicit_7_day_bucket" {
				return fmt.Errorf("creator metric resource range provenance invalid")
			}
		}
		for _, key := range diagnostic.NumericKeys {
			key = strings.TrimSpace(key)
			if key == "" || len(key) > 180 {
				return fmt.Errorf("creator metric resource numeric provenance invalid")
			}
			lower := strings.ToLower(key)
			for _, blocked := range []string{"cookie", "token", "password", "secret", "authorization", "session", "credential", "private_key", "access_key"} {
				if strings.Contains(lower, blocked) {
					return fmt.Errorf("creator metric resource numeric provenance invalid")
				}
			}
			for _, character := range key {
				if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune("._-[]", character) {
					continue
				}
				return fmt.Errorf("creator metric resource numeric provenance invalid")
			}
		}
	}
	return nil
}

func validCreatorMetricCode(value string) bool {
	if len(value) > 120 {
		return false
	}
	for _, r := range value {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func validateCreatorMetricJSON(raw string) error {
	if raw == "" {
		raw = "{}"
	}
	if len(raw) > maxCreatorMetricRawJSON {
		return fmt.Errorf("creator metric payload too large")
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("invalid creator metric payload")
	}
	if creatorMetricJSONHasSensitiveKey(value) {
		return fmt.Errorf("creator metric payload contains sensitive fields")
	}
	return nil
}

func creatorMetricJSONHasSensitiveKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			for _, blocked := range []string{"cookie", "token", "password", "secret", "authorization", "session", "credential", "private_key", "access_key"} {
				if strings.Contains(lower, blocked) {
					return true
				}
			}
			if creatorMetricJSONHasSensitiveKey(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if creatorMetricJSONHasSensitiveKey(child) {
				return true
			}
		}
	}
	return false
}

func hasNegativeMetric(values ...int64) bool {
	for _, value := range values {
		if value < 0 {
			return true
		}
	}
	return false
}

func safeCreatorMetricURL(raw, platformCode string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	for _, suffix := range creatorMetricPlatformSuffixes[platformCode] {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func creatorPostKey(post CreatorPostMetricInput) string {
	value := strings.TrimSpace(post.ExternalPostID)
	if value == "" {
		value = strings.TrimSpace(post.Title) + "\n" + strings.TrimSpace(post.PublishedAt)
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func creatorMetricInsights(overview *CreatorMetricOverview) []string {
	if overview == nil {
		return []string{"近 7 天暂无已验证的创作者后台快照。"}
	}
	insights := make([]string, 0, 4)
	if len(overview.Latest) == 0 {
		insights = append(insights, "近 7 天暂无已验证的创作者后台快照。")
	}
	if len(overview.Platforms) > 0 && overview.Platforms[0].Views > 0 {
		best := overview.Platforms[0]
		insights = append(insights, fmt.Sprintf("%s 当前浏览量最高：%d。", creatorMetricPlatformName(best.PlatformCode), best.Views))
	}
	verified := 0
	for _, item := range overview.Latest {
		if item.RangeVerified {
			verified++
		}
	}
	total := 0
	for _, item := range overview.Collections {
		if item.ErrorCode != "DUPLICATE_ACCOUNT_BINDING" {
			total++
		}
	}
	if total == 0 {
		total = len(overview.Latest)
	}
	if total > 0 {
		insights = append(insights, fmt.Sprintf("%d/%d 个账号已有近 7 天验证快照。", verified, total))
	}
	if len(overview.TopPosts) > 0 && overview.TopPosts[0].Views > 0 {
		insights = append(insights, fmt.Sprintf("当前最高浏览作品为“%s”，浏览 %d。", overview.TopPosts[0].Title, overview.TopPosts[0].Views))
	}
	if len(overview.Daily) >= 2 {
		previous, latest := overview.Daily[len(overview.Daily)-2], overview.Daily[len(overview.Daily)-1]
		if previous.Views > 0 || latest.Views > 0 {
			delta := latest.Views - previous.Views
			insights = append(insights, fmt.Sprintf("最近两次日快照浏览量变化：%+d。", delta))
		}
	}
	return insights
}

func creatorMetricPlatformName(platformCode string) string {
	switch platformCode {
	case "douyin":
		return "抖音"
	case "wechat_video":
		return "视频号"
	case "kuaishou":
		return "快手"
	case "xhs":
		return "小红书"
	case "tiktok":
		return "TikTok"
	case "youtube":
		return "YouTube"
	case "meta":
		return "Meta"
	case "wechat_mp":
		return "微信公众号"
	case "csdn":
		return "CSDN"
	default:
		return platformCode
	}
}
