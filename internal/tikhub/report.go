package tikhub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WriteRankMarkdown writes a human-readable daily board report (alphaagent-style).
func WriteRankMarkdown(dataDir, date, board string, items []map[string]interface{}, fetchedAt string) (string, error) {
	if date == "" {
		date = Today()
	}
	if fetchedAt == "" {
		fetchedAt = time.Now().Format(time.RFC3339)
	}
	dir := filepath.Join(dataDir, "tikhub", "ranks", date)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fp := filepath.Join(dir, board+".md")

	var b strings.Builder
	fmt.Fprintf(&b, "# 抖音 · %s（%s）\n\n", boardTitle(board), date)
	fmt.Fprintf(&b, "> 抓取：%s\n", fetchedAt)
	fmt.Fprintf(&b, "> 共 %d 条 · 来源 TikHub billboard\n\n---\n\n", len(items))

	limit := len(items)
	if limit > 30 {
		limit = 30
	}
	for i := 0; i < limit; i++ {
		it := items[i]
		title := firstStr(it, "item_title", "title", "word", "sentence")
		if title == "" {
			title = "(无标题)"
		}
		nick := firstStr(it, "nick_name", "author", "nickname")
		fmt.Fprintf(&b, "## %d. %s\n\n", i+1, title)
		if nick != "" {
			fmt.Fprintf(&b, "- **作者**：%s\n", nick)
		}
		if id := firstStr(it, "item_id", "aweme_id", "id"); id != "" {
			fmt.Fprintf(&b, "- **链接**：https://www.douyin.com/video/%s\n", id)
		}
		play := firstNum(it, "play_cnt", "play_count")
		like := firstNum(it, "like_cnt", "digg_count", "like_count")
		fans := firstNum(it, "fans_cnt", "follower_count")
		if play > 0 || like > 0 || fans > 0 {
			fmt.Fprintf(&b, "- **数据**：▶️ %s 播放 / 👍 %s 赞 / 👥 %s 粉\n", fmtInt(play), fmtInt(like), fmtInt(fans))
		}
		b.WriteString("\n---\n\n")
	}
	if err := os.WriteFile(fp, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return fp, nil
}

func boardTitle(board string) string {
	switch board {
	case "video":
		return "视频热榜"
	case "low_fan":
		return "低粉爆款榜"
	case "high_like":
		return "高点赞率榜"
	case "high_fan":
		return "高涨粉率榜"
	case "high_play":
		return "高播放榜"
	case "hot_search":
		return "热搜榜"
	default:
		return board
	}
}

func firstStr(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func firstNum(m map[string]interface{}, keys ...string) int64 {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			switch n := v.(type) {
			case float64:
				return int64(n)
			case int:
				return int64(n)
			case int64:
				return n
			default:
				var x int64
				if _, err := fmt.Sscan(fmt.Sprint(v), &x); err == nil {
					return x
				}
			}
		}
	}
	return 0
}

func fmtInt(n int64) string {
	if n < 0 {
		return "0"
	}
	s := fmt.Sprintf("%d", n)
	// simple thousands
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}

// TopItems returns first n items for display.
func TopItems(items []map[string]interface{}, n int) []map[string]interface{} {
	if n <= 0 || n >= len(items) {
		return items
	}
	return items[:n]
}
