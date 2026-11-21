package feishu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// 稿件库 is the table that carries finished copy. It is separate from 作品库
// (what competitors published) so a reviewer never confuses our draft with a
// collected work.
const ContentTableName = "稿件库"

var contentTableFields = []map[string]any{
	{"name": "稿件ID", "type": "text"},
	{"name": "标题", "type": "text"},
	{"name": "平台", "type": "text"},
	{"name": "栏目", "type": "text"},
	{"name": "状态", "type": "text"},
	{"name": "字数", "type": "number"},
	{"name": "文件名", "type": "text"},
	{"name": "更新时间", "type": "text"},
	{"name": "正文", "type": "text"},
}

// ContentPushResult reports what a push wrote, so a dry run and a real run can
// be compared row for row.
type ContentPushResult struct {
	TableID  string           `json:"table_id,omitempty"`
	BaseURL  string           `json:"base_url,omitempty"`
	DryRun   bool             `json:"dry_run"`
	Scanned  int              `json:"scanned"`
	Created  int              `json:"created"`
	Updated  int              `json:"updated"`
	Failed   int              `json:"failed"`
	Rows     []map[string]any `json:"rows"`
	Errors   []string         `json:"errors,omitempty"`
	Skipped  []string         `json:"skipped,omitempty"`
	PushedAt string           `json:"pushed_at"`
}

type contentDraft struct {
	businessKey string
	fields      map[string]any
}

// PushContentDir scans finished drafts in dir and upserts each one into 稿件库.
// It only reads local files when dry-run is set.
func (exporter *Exporter) PushContentDir(ctx context.Context, dir string, dryRun bool) (*ContentPushResult, error) {
	if exporter == nil || exporter.DB == nil || exporter.Client == nil {
		return nil, fmt.Errorf("feishu exporter unavailable")
	}
	settings, err := exporter.DB.FeishuContentExportSettings()
	if err != nil {
		return nil, err
	}
	drafts, skipped, err := collectContentDrafts(dir)
	if err != nil {
		return nil, err
	}
	result := &ContentPushResult{
		BaseURL: settings.BaseURL, DryRun: dryRun, Scanned: len(drafts), Skipped: skipped,
		Rows: make([]map[string]any, 0, len(drafts)), PushedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if dryRun {
		for _, draft := range drafts {
			result.Rows = append(result.Rows, draft.fields)
		}
		return result, nil
	}
	if !settings.Configured() {
		return nil, fmt.Errorf("feishu 内容库还没建：先运行 feishu init")
	}
	tableID, err := exporter.Client.contentTableID(ctx, settings.BaseToken)
	if err != nil {
		return nil, err
	}
	result.TableID = tableID
	for _, draft := range drafts {
		recordID, linkErr := exporter.DB.FeishuRecordLink(tableID, draft.businessKey)
		if linkErr != nil {
			result.Failed++
			if len(result.Errors) < MaxErrorsPerRun {
				result.Errors = append(result.Errors, linkErr.Error())
			}
			continue
		}
		written, writeErr := exporter.Client.UpsertRecord(ctx, settings.BaseToken, tableID, recordID, draft.fields)
		if writeErr != nil {
			result.Failed++
			if len(result.Errors) < MaxErrorsPerRun {
				result.Errors = append(result.Errors, writeErr.Error())
			}
			continue
		}
		if recordID == "" {
			result.Created++
		} else {
			result.Updated++
		}
		result.Rows = append(result.Rows, draft.fields)
		if saveErr := exporter.DB.SaveFeishuRecordLink(tableID, draft.businessKey, written); saveErr != nil {
			result.Failed++
			if len(result.Errors) < MaxErrorsPerRun {
				result.Errors = append(result.Errors, saveErr.Error())
			}
		}
	}
	return result, nil
}

// contentTableID finds 稿件库 by name and creates it when absent. It never
// falls back to another table: writing drafts into 作品库 would corrupt it.
func (client *Client) contentTableID(ctx context.Context, baseToken string) (string, error) {
	payload, err := client.Call(ctx, "base", "+table-list", "--base-token", baseToken)
	if err != nil {
		return "", err
	}
	tree, err := decode(payload)
	if err != nil {
		return "", err
	}
	tables, _ := tree.(map[string]any)
	for _, key := range []string{"items", "tables"} {
		list, ok := tables[key].([]any)
		if !ok {
			continue
		}
		for _, item := range list {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := record["name"].(string)
			if name != ContentTableName {
				continue
			}
			id, _ := record["table_id"].(string)
			if id == "" {
				id, _ = record["id"].(string)
			}
			if id != "" {
				return id, nil
			}
		}
	}
	return client.CreateTable(ctx, baseToken, ContentTableName, contentTableFields)
}

// collectContentDrafts reads finished drafts. Only files that already passed
// review are here, so a name pattern is the filter; .bak copies are skipped.
func collectContentDrafts(dir string) ([]contentDraft, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	drafts := make([]contentDraft, 0, 16)
	skipped := make([]string, 0, 8)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "成稿方案-") || !strings.HasSuffix(name, ".md") {
			continue
		}
		if strings.Contains(name, ".bak") {
			skipped = append(skipped, name)
			continue
		}
		path := filepath.Join(dir, name)
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			skipped = append(skipped, name)
			continue
		}
		info, statErr := entry.Info()
		updated := ""
		if statErr == nil {
			updated = info.ModTime().Format("2006-01-02 15:04")
		}
		text := string(body)
		drafts = append(drafts, contentDraft{
			businessKey: "content:" + name,
			fields: map[string]any{
				"稿件ID": strings.TrimSuffix(name, ".md"),
				"标题":   draftTitle(text, name),
				"平台":   draftPlatform(name),
				"栏目":   draftSection(name),
				"状态":   "待发布",
				"字数":   draftWordCount(text),
				"文件名":  name,
				"更新时间": updated,
				"正文":   draftExcerpt(text, 600),
			},
		})
	}
	sort.Slice(drafts, func(i, j int) bool { return drafts[i].businessKey < drafts[j].businessKey })
	return drafts, skipped, nil
}

func draftTitle(text, name string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if title != "" {
			return title
		}
	}
	return strings.TrimSuffix(name, ".md")
}

func draftPlatform(name string) string {
	switch {
	case strings.Contains(name, "小红书"):
		return "小红书"
	case strings.Contains(name, "视频号"):
		return "视频号"
	case strings.Contains(name, "公众号"):
		return "公众号"
	case strings.Contains(name, "知乎"):
		return "知乎"
	case strings.Contains(name, "海外"):
		return "X"
	case strings.Contains(name, "M1M2"):
		return "抖音"
	}
	return "未标注"
}

func draftSection(name string) string {
	switch {
	case strings.Contains(name, "海外A"):
		return "海外 A"
	case strings.Contains(name, "海外B"):
		return "海外 B"
	case strings.Contains(name, "M1M2"):
		return "M1+M2"
	case strings.Contains(name, "M2"):
		return "M2"
	case strings.Contains(name, "M1"):
		return "M1"
	}
	return "未标注"
}

func draftWordCount(text string) int {
	count := 0
	for _, r := range text {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			continue
		}
		count++
	}
	return count
}

func draftExcerpt(text string, limit int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "…"
}
