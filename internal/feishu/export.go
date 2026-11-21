package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
)

// DefaultExportDays bounds the first mirror to recent content instead of the
// full history, so one click cannot spend minutes writing thousands of rows.
const DefaultExportDays = 7

// MaxErrorsPerRun caps the per-row failure list kept in the run result.
const MaxErrorsPerRun = 20

// MaxExportDays bounds a backfill window; it is deliberately wide enough for a
// first full mirror of an existing archive.
const MaxExportDays = 3650

// Exporter copies collected content into the bound Feishu Base.
type Exporter struct {
	DB     *db.DB
	Client *Client
	Now    func() time.Time
}

func NewExporter(database *db.DB, client *Client) *Exporter {
	return &Exporter{DB: database, Client: client, Now: time.Now}
}

// ExportResult summarises one mirror run. It is stored verbatim as the run
// result so the UI and CLI can show what the last sweep did.
type ExportResult struct {
	Enabled    bool     `json:"enabled"`
	Skipped    string   `json:"skipped,omitempty"`
	BaseURL    string   `json:"base_url,omitempty"`
	Posts      int      `json:"posts"`
	Metrics    int      `json:"metrics"`
	Created    int      `json:"created"`
	Updated    int      `json:"updated"`
	Failed     int      `json:"failed"`
	Errors     []string `json:"errors,omitempty"`
	StartedAt  string   `json:"started_at"`
	FinishedAt string   `json:"finished_at"`
}

// Configure creates a fresh Feishu Base for collected content and binds it.
// The mirror starts enabled: the person asked for the content library, and
// disabling it later is one flag away.
func (exporter *Exporter) Configure(ctx context.Context, name string) (*ContentBase, error) {
	if exporter == nil || exporter.DB == nil || exporter.Client == nil {
		return nil, fmt.Errorf("feishu exporter unavailable")
	}
	base, err := exporter.Client.CreateContentBase(ctx, name)
	if err != nil {
		return nil, err
	}
	settings := db.FeishuContentExport{
		Enabled: true, BaseToken: base.Token, BaseURL: base.URL,
		PostTableID: base.PostTableID, MetricsTableID: base.MetricsTableID,
		LastResultJSON: "{}",
	}
	if err := exporter.DB.SaveFeishuContentExport(settings); err != nil {
		return nil, err
	}
	return base, nil
}

// Run mirrors the accounts' recent content and metrics into the bound Base.
// A disabled or unconfigured mirror is a no-op, not an error: this runs after
// every scheduled collection.
func (exporter *Exporter) Run(ctx context.Context, sinceDays int) (*ExportResult, error) {
	if exporter == nil || exporter.DB == nil || exporter.Client == nil {
		return nil, fmt.Errorf("feishu exporter unavailable")
	}
	started := exporter.Now().UTC()
	result := &ExportResult{StartedAt: started.Format(time.RFC3339)}
	settings, err := exporter.DB.FeishuContentExportSettings()
	if err != nil {
		return nil, err
	}
	result.Enabled = settings.Enabled
	result.BaseURL = settings.BaseURL
	if !settings.Enabled {
		result.Skipped = "disabled"
		return result, nil
	}
	if !settings.Configured() {
		result.Skipped = "not_configured"
		return result, nil
	}
	if sinceDays <= 0 || sinceDays > MaxExportDays {
		sinceDays = DefaultExportDays
	}
	since := started.AddDate(0, 0, -sinceDays).Format("2006-01-02")

	posts, err := exporter.DB.ListFeishuPostRows(since, 0)
	if err != nil {
		return nil, err
	}
	result.Posts = len(posts)
	for _, post := range posts {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		fields := map[string]any{
			"作品键": post.BusinessKey, "平台": post.PlatformCode, "账号": post.AccountName,
			"标题": post.Title, "发布时间": post.PublishedAt, "链接": post.PostURL,
			"播放": post.Views, "点赞": post.Likes, "评论": post.Comments,
			"转发": post.Shares, "收藏": post.Favorites, "采集日期": post.SnapshotDate,
		}
		if err := exporter.writeRow(ctx, settings.BaseToken, settings.PostTableID, post.BusinessKey, fields, result); err != nil {
			result.Failed++
			appendError(result, err)
		}
	}

	metrics, err := exporter.DB.ListFeishuAccountMetricRows(since, 0)
	if err != nil {
		return nil, err
	}
	result.Metrics = len(metrics)
	for _, metric := range metrics {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		fields := map[string]any{
			"账号日期": metric.BusinessKey, "平台": metric.PlatformCode, "账号": metric.AccountName,
			"采集日期": metric.SnapshotDate, "统计天数": metric.WindowDays,
			"范围已验证": metric.RangeVerified, "粉丝": metric.Followers, "播放": metric.Views,
			"点赞": metric.Likes, "评论": metric.Comments, "转发": metric.Shares,
			"收藏": metric.Favorites, "作品数": metric.Posts,
		}
		if err := exporter.writeRow(ctx, settings.BaseToken, settings.MetricsTableID, metric.BusinessKey, fields, result); err != nil {
			result.Failed++
			appendError(result, err)
		}
	}

	result.FinishedAt = exporter.Now().UTC().Format(time.RFC3339)
	encoded, _ := json.Marshal(result)
	settings.LastExportAt = result.FinishedAt
	settings.LastResultJSON = string(encoded)
	if err := exporter.DB.SaveFeishuContentExport(settings); err != nil {
		return result, err
	}
	if result.Failed > 0 && result.Failed == result.Posts+result.Metrics {
		return result, fmt.Errorf("feishu export failed for every row (%d)", result.Failed)
	}
	return result, nil
}

// writeRow updates a known record or creates one, then remembers its id.
func (exporter *Exporter) writeRow(ctx context.Context, baseToken, tableID, businessKey string, fields map[string]any, result *ExportResult) error {
	recordID, err := exporter.DB.FeishuRecordLink(tableID, businessKey)
	if err != nil {
		return err
	}
	written, err := exporter.Client.UpsertRecord(ctx, baseToken, tableID, recordID, fields)
	if err != nil {
		return err
	}
	if strings.TrimSpace(recordID) == "" {
		result.Created++
	} else {
		result.Updated++
	}
	return exporter.DB.SaveFeishuRecordLink(tableID, businessKey, written)
}

func appendError(result *ExportResult, err error) {
	if len(result.Errors) >= MaxErrorsPerRun {
		return
	}
	result.Errors = append(result.Errors, err.Error())
}

// ExportPreview is what a sweep would write, without calling Feishu. It lets a
// reviewer check row counts, keys and field values before any Base is touched.
type ExportPreview struct {
	Enabled    bool             `json:"enabled"`
	Configured bool             `json:"configured"`
	BaseURL    string           `json:"base_url,omitempty"`
	Since      string           `json:"since"`
	Posts      []map[string]any `json:"posts"`
	Metrics    []map[string]any `json:"metrics"`
	PostRows   int              `json:"post_rows"`
	MetricRows int              `json:"metric_rows"`
	Truncated  bool             `json:"truncated"`
}

// Preview builds the exact field maps Run would send, bounded by limit rows per
// table. It never calls the CLI, so it works before a Base exists.
func (exporter *Exporter) Preview(sinceDays, limit int) (*ExportPreview, error) {
	if exporter == nil || exporter.DB == nil {
		return nil, fmt.Errorf("feishu exporter unavailable")
	}
	if sinceDays <= 0 || sinceDays > MaxExportDays {
		sinceDays = DefaultExportDays
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	now := exporter.Now
	if now == nil {
		now = time.Now
	}
	started := now().UTC()
	settings, err := exporter.DB.FeishuContentExportSettings()
	if err != nil {
		return nil, err
	}
	preview := &ExportPreview{
		Enabled: settings.Enabled, Configured: settings.Configured(), BaseURL: settings.BaseURL,
		Since: started.AddDate(0, 0, -sinceDays).Format("2006-01-02"),
		Posts: []map[string]any{}, Metrics: []map[string]any{},
	}
	posts, err := exporter.DB.ListFeishuPostRows(preview.Since, 0)
	if err != nil {
		return nil, err
	}
	preview.PostRows = len(posts)
	for index, post := range posts {
		if index >= limit {
			preview.Truncated = true
			break
		}
		preview.Posts = append(preview.Posts, map[string]any{
			"作品键": post.BusinessKey, "平台": post.PlatformCode, "账号": post.AccountName,
			"标题": post.Title, "发布时间": post.PublishedAt, "链接": post.PostURL,
			"播放": post.Views, "点赞": post.Likes, "评论": post.Comments,
			"转发": post.Shares, "收藏": post.Favorites, "采集日期": post.SnapshotDate,
		})
	}
	metrics, err := exporter.DB.ListFeishuAccountMetricRows(preview.Since, 0)
	if err != nil {
		return nil, err
	}
	preview.MetricRows = len(metrics)
	for index, metric := range metrics {
		if index >= limit {
			preview.Truncated = true
			break
		}
		preview.Metrics = append(preview.Metrics, map[string]any{
			"账号日期": metric.BusinessKey, "平台": metric.PlatformCode, "账号": metric.AccountName,
			"采集日期": metric.SnapshotDate, "统计天数": metric.WindowDays,
			"范围已验证": metric.RangeVerified, "粉丝": metric.Followers, "播放": metric.Views,
			"点赞": metric.Likes, "评论": metric.Comments, "转发": metric.Shares,
			"收藏": metric.Favorites, "作品数": metric.Posts,
		})
	}
	return preview, nil
}
