package ui

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/radar"
)

// trendRadarExportRow is the structured hand-off format for downstream teams.
// One row is one collected work; the raw snapshot stays the audit source.
type trendRadarExportRow struct {
	Platform    string `json:"platform"`
	NicheID     string `json:"niche_id"`
	NicheName   string `json:"niche_name"`
	SearchTerm  string `json:"search_term"`
	Author      string `json:"author"`
	URL         string `json:"url"`
	Text        string `json:"text"`
	Play        int64  `json:"play"`
	Likes       int64  `json:"likes"`
	Collects    int64  `json:"collects"`
	Shares      int64  `json:"shares"`
	Comments    int64  `json:"comments"`
	FetchedAt   string `json:"fetched_at"`
	FirstSeenAt string `json:"first_seen_at"`
	Occurrences int    `json:"occurrences"`
	CallID      int64  `json:"call_id"`
	SearchPool  string `json:"search_pool"`
	WorkID      string `json:"work_id"`
	Batch       string `json:"batch"`
	Tags        string `json:"tags"`
}

var trendRadarExportHeader = []string{
	"平台", "来源池", "搜索词", "作者", "作品链接", "文案",
	"播放", "点赞", "收藏", "转发", "评论", "抓取时间", "首次发现", "出现次数", "调用ID",
	"搜索词池", "作品ID", "批次", "标签",
}

func (s *Server) handleAPITrendRadarExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "data_dir_unavailable", "无法读取本地数据目录")
		return
	}
	summary, err := buildTrendRadarSummary(dataDir, time.Now())
	if err != nil {
		writeDataAPIError(w, http.StatusInternalServerError, "trend_radar_unavailable", "无法读取本地监控记录")
		return
	}
	// 搜索词池：这条作品的搜索词是否真的挂在某个池子的词表里。
	// 与「来源池」不同——来源池匹配不上时会被归进「历史记录」，这里如实写「未归属」。
	// 优先用数据库里的池子词表（join），读不到再退回当前配置。
	termPools := map[string]string{}
	if s.db != nil {
		if stored, storeErr := s.db.RadarTermPoolMap(); storeErr == nil {
			termPools = stored
		}
	}
	if len(termPools) == 0 {
		for _, niche := range summary.Niches {
			if niche.ID == archivedRadarNicheID {
				continue
			}
			for _, term := range niche.SearchTerms {
				key := strings.ToLower(strings.TrimSpace(term))
				if key == "" {
					continue
				}
				if existing, ok := termPools[key]; ok && existing != niche.Name {
					termPools[key] = existing + "、" + niche.Name
					continue
				}
				termPools[key] = niche.Name
			}
		}
	}
	// 「出现次数」用去重后的真实抓取次数，不用标题合并出来的估算值。
	seenCounts := map[string]int{}
	if s.db != nil {
		if stored, storeErr := s.db.RadarWorkSeenCounts(); storeErr == nil {
			seenCounts = stored
		}
	}
	// 批次：这条作品的付费调用属于哪一轮抓取。库里能查到就用库，查不到再退回 run 文件，
	// 都没有就如实写「未归属」——老调用本来就不属于任何一轮。
	batchByCall := map[int64]string{}
	if s.db != nil {
		if stored, storeErr := s.db.RadarRunIDsForCalls(); storeErr == nil {
			batchByCall = stored
		}
	}
	if len(batchByCall) == 0 {
		if runs, runErr := radar.ListRuns(dataDir, 0); runErr == nil {
			for _, run := range runs {
				for _, call := range run.Calls {
					if call.CallID > 0 {
						batchByCall[call.CallID] = run.ID
					}
				}
			}
		}
	}
	nicheID := strings.TrimSpace(r.URL.Query().Get("niche_id"))
	platform := strings.TrimSpace(r.URL.Query().Get("platform"))
	rows := make([]trendRadarExportRow, 0, len(summary.Signals))
	for _, signal := range summary.Signals {
		if nicheID != "" && signal.NicheID != nicheID {
			continue
		}
		if platform != "" && signal.Platform != platform {
			continue
		}
		row := trendRadarExportRow{
			Platform: signal.Platform, NicheID: signal.NicheID, NicheName: signal.NicheName,
			SearchTerm: signal.SearchTerm, Author: signal.Creator, URL: signal.URL, Text: signal.Title,
			Play: signal.Play, Likes: signal.Likes, Collects: signal.Collects, Shares: signal.Shares,
			Comments: signal.Comments, FetchedAt: signal.ObservedAt, FirstSeenAt: signal.FirstSeen,
			Occurrences: signal.Occurrences, CallID: signal.CallID,
			SearchPool: termPools[strings.ToLower(strings.TrimSpace(signal.SearchTerm))],
			WorkID:     signal.VideoID,
			Batch:      batchByCall[signal.CallID],
			Tags:       strings.Join(signal.Tags, "、"),
		}
		if row.SearchPool == "" {
			row.SearchPool = "未归属"
		}
		if row.Batch == "" {
			row.Batch = "未归属"
		}
		if seen := seenCounts[radarWorkDedupKey(signal.Platform, signal.VideoID, signal.URL, signal.Title)]; seen > 0 {
			row.Occurrences = seen
		}
		rows = append(rows, row)
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "csv"
	}
	slug := nicheID
	if slug == "" {
		slug = "all"
	}
	if !validRadarExportSlug(slug) {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_niche_id", "推荐池 ID 无效")
		return
	}
	stamp := time.Now().Format("20060102-1504")
	filename := fmt.Sprintf("trend-radar-%s-%s.%s", slug, stamp, format)
	switch format {
	case "csv":
		var builder strings.Builder
		builder.WriteString("\uFEFF") // Excel on Windows needs the BOM to read UTF-8.
		writer := csv.NewWriter(&builder)
		if err := writer.Write(trendRadarExportHeader); err != nil {
			writeDataAPIError(w, http.StatusInternalServerError, "export_failed", "导出失败")
			return
		}
		for _, row := range rows {
			if err := writer.Write([]string{
				row.Platform, row.NicheName, row.SearchTerm, row.Author, row.URL, row.Text,
				strconv.FormatInt(row.Play, 10), strconv.FormatInt(row.Likes, 10),
				strconv.FormatInt(row.Collects, 10), strconv.FormatInt(row.Shares, 10),
				strconv.FormatInt(row.Comments, 10), row.FetchedAt, row.FirstSeenAt,
				strconv.Itoa(row.Occurrences), strconv.FormatInt(row.CallID, 10),
				row.SearchPool, row.WorkID, row.Batch, row.Tags,
			}); err != nil {
				writeDataAPIError(w, http.StatusInternalServerError, "export_failed", "导出失败")
				return
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			writeDataAPIError(w, http.StatusInternalServerError, "export_failed", "导出失败")
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Nexus-Export-Rows", strconv.Itoa(len(rows)))
		_, _ = w.Write([]byte(builder.String()))
	case "json":
		payload := map[string]any{
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"niche_id":     nicheID,
			"platform":     platform,
			"count":        len(rows),
			"rows":         rows,
		}
		raw, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			writeDataAPIError(w, http.StatusInternalServerError, "export_failed", "导出失败")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Nexus-Export-Rows", strconv.Itoa(len(rows)))
		_, _ = w.Write(raw)
	default:
		writeDataAPIError(w, http.StatusBadRequest, "invalid_format", "只支持 csv 或 json 导出")
	}
}

func validRadarExportSlug(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}
