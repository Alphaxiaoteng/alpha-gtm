package ui

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/tikhub"
)

// 付费接口的重复调用是目前最大的一笔浪费：实测 douyin.creator_posts
// 124 次调用里只有 8 组唯一参数（重复率 93.5%），douyin.video_search
// 47 次里只有 12 组唯一。
//
// 这些重复大多来自「同一批数据在几分钟内被反复拉」——用户手点几下、
// 定时任务与手动抓取撞在一起、归档重试。TikHub 按调用计费，每重复一次
// 都是真金白银，所以按能力给一个保守的复用窗口：窗口内同样参数的请求
// 直接回放上次的原始快照，不创建新的调用记录。

// dataAPICacheTTL 是各能力的复用窗口。取保守值：搜索类结果变化快，
// 只留 6 小时；作品列表 1 小时；资料与字幕这类几乎不变的数据留 24 小时。
func dataAPICacheTTL(capability string) time.Duration {
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
		return 0 // 未知能力不复用，宁可多花一次也不要给出过期数据
	}
}

// dataAPICacheKey 把端点与规范化参数压成一个稳定的键。
// 参数按 key 排序后序列化，避免 map 遍历顺序导致的键不稳定。
func dataAPICacheKey(endpointID string, prepared map[string]any) string {
	keys := make([]string, 0, len(prepared))
	for key := range prepared {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteString(endpointID)
	for _, key := range keys {
		builder.WriteByte('\x1f')
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(strings.TrimSpace(toCacheScalar(prepared[key])))
	}
	return builder.String()
}

func toCacheScalar(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		raw, _ := json.Marshal(typed)
		return string(raw)
	case int:
		raw, _ := json.Marshal(typed)
		return string(raw)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		raw, _ := json.Marshal(typed)
		return string(raw)
	}
}

// dataAPICacheEntry 是一次可复用的响应。
type dataAPICacheEntry struct {
	Response map[string]any `json:"response"`
	RawPath  string         `json:"raw_path"`
	CallID   int64          `json:"call_id"`
	StoredAt time.Time      `json:"stored_at"`
}

// dataAPICache 是进程内的短窗口缓存。不落盘：它的作用只是吸收
// 「同一分钟内反复点」这类突发重复，跨进程复用交给快照文件本身。
type dataAPICache struct {
	mu      sync.Mutex
	entries map[string]dataAPICacheEntry
}

func newDataAPICache() *dataAPICache {
	return &dataAPICache{entries: map[string]dataAPICacheEntry{}}
}

// lookup 返回窗口内的可复用响应。过期条目顺手清掉，避免无限增长。
func (c *dataAPICache) lookup(endpointID string, prepared map[string]any, now time.Time) (dataAPICacheEntry, bool) {
	if c == nil {
		return dataAPICacheEntry{}, false
	}
	key := dataAPICacheKey(endpointID, prepared)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return dataAPICacheEntry{}, false
	}
	ttl := dataAPICacheTTL(endpointCapabilityForCache(endpointID))
	if ttl <= 0 || now.Sub(entry.StoredAt) > ttl {
		delete(c.entries, key)
		return dataAPICacheEntry{}, false
	}
	return entry, true
}

func (c *dataAPICache) store(endpointID string, prepared map[string]any, entry dataAPICacheEntry, now time.Time) {
	if c == nil {
		return
	}
	key := dataAPICacheKey(endpointID, prepared)
	c.mu.Lock()
	defer c.mu.Unlock()
	// 顺手清理过期项，防止长时间运行时 map 只增不减。
	for candidate, stored := range c.entries {
		ttl := dataAPICacheTTL(endpointCapabilityForCache(strings.SplitN(candidate, "\x1f", 2)[0]))
		if ttl > 0 && now.Sub(stored.StoredAt) > ttl {
			delete(c.entries, candidate)
		}
	}
	entry.StoredAt = now
	c.entries[key] = entry
}

// endpointCapabilityForCache 由调用方在传入时解析好的端点定义决定；
// 这里只做兜底查表，避免缓存层反向依赖调用栈。
func endpointCapabilityForCache(endpointID string) string {
	if endpoint, ok := tikhub.FindEndpoint(endpointID); ok {
		return endpoint.Capability
	}
	return ""
}

// readSnapshotResponse 复用 tikhub 的同一份实现：UI 与 radar worker 都要
// 从快照回放响应，两边各写一遍必然有一边在路径处理上跑偏。
func readSnapshotResponse(dataDir, rawPath string) (map[string]any, error) {
	return tikhub.ReadSnapshotResponse(dataDir, rawPath)
}
