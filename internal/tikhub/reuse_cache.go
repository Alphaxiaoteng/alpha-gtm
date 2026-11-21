package tikhub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 付费接口的重复调用是目前最大的一笔浪费：实测 douyin.creator_posts
// 124 次调用里只有 8 组唯一参数（重复率 93.5%）。
//
// 这些重复来自三条链路，而它们分属不同进程：
//   - UI 单次手点（internal/ui 的 executeDataAPICall）
//   - 定时抓取（radar worker 独立进程，直接 CallPreparedEndpoint）
//   - 博主归档（UI 进程内的 SyncCreatorAll）
//
// 原先的复用窗口只挂在第一条上，且只存在于进程内存里，所以 worker 与归档
// 完全绕过它。这里把索引落到数据目录，做成一个跨进程共享的小文件：谁
// 先调用谁写入，其余方在窗口内直接回放同一份快照。

// ReuseWindowTTL 是各能力的复用窗口。保守取值——搜索类结果变化快只留 6
// 小时，作品列表 1 小时，资料与字幕几乎不变留 24 小时，内容详情 12 小时。
// 未知能力返回 0，表示不复用：宁可多花一次也不要给出过期数据。
func ReuseWindowTTL(capability string) time.Duration {
	switch capability {
	case "keyword_search":
		return 6 * time.Hour
	case "creator_posts":
		return time.Hour
	case "creator_profile", "content_transcript":
		return 24 * time.Hour
	case "content_detail":
		return 12 * time.Hour
	default:
		return 0
	}
}

// ReuseKey 把端点与规范化参数压成一个稳定的键：参数按 key 排序后序列化，
// 避免 map 遍历顺序导致的键不稳定（否则同一请求会算出不同键，缓存永远不命中）。
func ReuseKey(endpointID string, params map[string]any) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteString(endpointID)
	for _, key := range keys {
		builder.WriteByte('\x1f')
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(reuseScalar(params[key]))
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:16])
}

func reuseScalar(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%v", typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}
		return string(encoded)
	}
}

// ReuseEntry 是一条可复用的记录：指向已落盘的快照与产生它的调用。
type ReuseEntry struct {
	Key        string    `json:"key"`
	EndpointID string    `json:"endpoint_id"`
	CallID     int64     `json:"call_id"`
	RawPath    string    `json:"raw_path"`
	StoredAt   time.Time `json:"stored_at"`
}

// reuseIndex 是落盘的索引结构。条目数有上限，避免文件无限增长。
type reuseIndex struct {
	Version int          `json:"version"`
	Entries []ReuseEntry `json:"entries"`
}

const reuseIndexMaxEntries = 400

var reuseIndexMu sync.Mutex

func reuseIndexPath(dataDir string) string {
	return filepath.Join(dataDir, "tikhub", "reuse-index.json")
}

// LookupReusableCall 在窗口内查找同一端点、同一参数的既有调用。
//
// 任何一步出错都当作未命中：缓存是省钱手段，不能因为它让正常调用失败。
func LookupReusableCall(dataDir, endpointID string, params map[string]any, capability string, now time.Time) (ReuseEntry, bool) {
	ttl := ReuseWindowTTL(capability)
	if ttl <= 0 || strings.TrimSpace(dataDir) == "" {
		return ReuseEntry{}, false
	}
	key := ReuseKey(endpointID, params)
	reuseIndexMu.Lock()
	defer reuseIndexMu.Unlock()
	index, err := readReuseIndex(dataDir)
	if err != nil {
		return ReuseEntry{}, false
	}
	for _, entry := range index.Entries {
		if entry.Key != key {
			continue
		}
		if now.Sub(entry.StoredAt) > ttl {
			return ReuseEntry{}, false
		}
		// 快照文件可能被清理掉了；文件不在了就当未命中，别回放一个空响应。
		if _, statErr := os.Stat(entry.RawPath); statErr != nil {
			return ReuseEntry{}, false
		}
		return entry, true
	}
	return ReuseEntry{}, false
}

// RecordReusableCall 记下这次调用的产物，供同参数的后继请求复用。
func RecordReusableCall(dataDir, endpointID string, params map[string]any, entry ReuseEntry, now time.Time) {
	if strings.TrimSpace(dataDir) == "" || entry.RawPath == "" {
		return
	}
	entry.Key = ReuseKey(endpointID, params)
	entry.EndpointID = endpointID
	entry.StoredAt = now
	reuseIndexMu.Lock()
	defer reuseIndexMu.Unlock()
	index, err := readReuseIndex(dataDir)
	if err != nil {
		index = reuseIndex{}
	}
	index.Version = 1
	// 同键覆盖，并清掉过期项。
	kept := make([]ReuseEntry, 0, len(index.Entries)+1)
	for _, existing := range index.Entries {
		if existing.Key == entry.Key {
			continue
		}
		ttl := ReuseWindowTTL(capabilityHint(existing.EndpointID))
		if ttl > 0 && now.Sub(existing.StoredAt) > ttl {
			continue
		}
		kept = append(kept, existing)
	}
	kept = append(kept, entry)
	if len(kept) > reuseIndexMaxEntries {
		kept = kept[len(kept)-reuseIndexMaxEntries:]
	}
	index.Entries = kept
	_ = writeReuseIndex(dataDir, index)
}

func readReuseIndex(dataDir string) (reuseIndex, error) {
	raw, err := os.ReadFile(reuseIndexPath(dataDir))
	if err != nil {
		return reuseIndex{}, err
	}
	var index reuseIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return reuseIndex{}, err
	}
	return index, nil
}

func writeReuseIndex(dataDir string, index reuseIndex) error {
	dir := filepath.Join(dataDir, "tikhub")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(index)
	if err != nil {
		return err
	}
	target := reuseIndexPath(dataDir)
	temp, err := os.CreateTemp(dir, ".reuse-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(body); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, target)
}

// capabilityHint 只用于清理过期项时估一个 TTL。索引里不存能力名，
// 从端点 ID 的前缀推断（`douyin.creator_posts` → `creator_posts`）。
func capabilityHint(endpointID string) string {
	if index := strings.LastIndex(endpointID, "."); index >= 0 && index+1 < len(endpointID) {
		return endpointID[index+1:]
	}
	return ""
}

// ReadSnapshotResponse 从已保存的原始快照里读回响应体，供缓存命中时回放。
//
// 放在 tikhub 包里是因为 UI 与 radar worker 都要用它，而两边各自实现一遍
// 就必然有一边在路径处理上跑偏。
func ReadSnapshotResponse(dataDir, rawPath string) (map[string]any, error) {
	if strings.TrimSpace(rawPath) == "" {
		return nil, os.ErrNotExist
	}
	path := rawPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(dataDir, filepath.FromSlash(strings.TrimPrefix(rawPath, "/")))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snapshot struct {
		Response map[string]any `json:"response"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	return snapshot.Response, nil
}
