package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
)

// 手机端只读接口。
//
// 设计边界（不是「暂时没做完」，是有意的）：
//   - 只读：没有 POST/PUT/DELETE，任何写操作都必须回到本机界面；
//   - 令牌：必须带 Authorization: Bearer <token>，token 来自 /api/auth/login；
//   - 回环：Accept 里带 text/html 的请求（即有人拿浏览器直连）一律拒绝，
//     页面本身由 `alpha-nexus-egolite mobile` 单独托管。
//
// 为什么不给手机开 UI 的写接口：AGENTS.md 的硬约束是「任务经 tools.Registry
// 调度、活跃 Space 最多 3、只执行用户当次明确动作」，这些都需要本机的审批
// 上下文。手机端是「看一眼」的窗口，不是第二个控制台。

// mobileTokenFingerprint 是写进 dataDir 的令牌指纹，供移动端页面在启动时
// 核对「这台手机连的是哪套数据」。不存明文令牌，存的是 sha256 前缀。
const mobileTokenFingerprintFile = "mobile-token.sha256"

func mobileAcceptIsBrowser(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

func (s *Server) rejectMobileBrowserRequest(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":"mobile API is token-only; open the mobile page instead","errorCode":"MOBILE_BROWSER_FORBIDDEN"}`))
}

// handleAPIMobileSummary 返回手机首屏需要的一切：账号概览、内容与发布进度、
// 未读通知、以及数据口径的「截至」时间。
func (s *Server) handleAPIMobileSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if mobileAcceptIsBrowser(r) {
		s.rejectMobileBrowserRequest(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	accounts, err := s.db.ListAccounts()
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"errorCode": "MOBILE_SUMMARY_FAILED"})
		return
	}
	contents, _ := s.db.ListContents()
	records, _ := s.db.ListPublishRecords(50)
	notifications, _ := s.db.ListNotifications(20, true)

	loggedIn, unknown, loggedOut := 0, 0, 0
	platformCounts := map[string]int{}
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		platformCounts[account.PlatformCode]++
		switch account.LoginState {
		case "logged_in":
			loggedIn++
		case "logged_out":
			loggedOut++
		default:
			unknown++
		}
	}

	// 发布进度：只统计状态，不带任何 payload。手机不需要看计划快照。
	publishByStatus := map[string]int{}
	for _, record := range records {
		publishByStatus[record.Status]++
	}

	type platformCount struct {
		Platform string `json:"platform"`
		Count    int    `json:"count"`
	}
	platformsSorted := make([]platformCount, 0, len(platformCounts))
	for code, count := range platformCounts {
		platformsSorted = append(platformsSorted, platformCount{Platform: code, Count: count})
	}
	sort.Slice(platformsSorted, func(a, b int) bool {
		if platformsSorted[a].Count != platformsSorted[b].Count {
			return platformsSorted[a].Count > platformsSorted[b].Count
		}
		return platformsSorted[a].Platform < platformsSorted[b].Platform
	})

	// 数据新鲜度：账号指标最后一次成功采集是什么时候。手机端必须能分辨
	// 「0 个粉丝」和「从没采到过」——这正是桌面端踩过的坑。
	freshness := s.mobileMetricsFreshness(ctx)

	writeJSON(w, map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"accounts": map[string]any{
			"total":      len(accounts),
			"logged_in":  loggedIn,
			"logged_out": loggedOut,
			"unknown":    unknown,
			"platforms":  platformsSorted,
		},
		"contents": map[string]any{
			"total": len(contents),
		},
		"publish": map[string]any{
			"total":     len(records),
			"by_status": publishByStatus,
		},
		"notifications": map[string]any{
			"unread": len(notifications),
		},
		"metrics": freshness,
	})
}

// mobileMetricsFreshness 报告账号指标的最新采集时间与行数。
//
// 任何查询失败都返回 available=false；手机端据此显示「尚未采集」，而不是
// 显示一个看着像真实数据的 0。
func (s *Server) mobileMetricsFreshness(ctx context.Context) map[string]any {
	var latest string
	var rows int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(snapshot_date),''), COUNT(*) FROM creator_metric_snapshots
		WHERE window_days=7 AND range_verified=1`).Scan(&latest, &rows)
	if err != nil {
		return map[string]any{"available": false}
	}
	return map[string]any{
		"available":     rows > 0,
		"latest_date":   latest,
		"verified_rows": rows,
	}
}

// handleAPIMobileAccounts 返回账号列表的精简投影：手机端只关心「哪个平台、
// 叫什么、登没登」，不返回 profile 路径、CDP 地址或任何凭据引用。
func (s *Server) handleAPIMobileAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if mobileAcceptIsBrowser(r) {
		s.rejectMobileBrowserRequest(w)
		return
	}
	accounts, err := s.db.ListAccounts()
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"errorCode": "MOBILE_ACCOUNTS_FAILED"})
		return
	}
	platformFilter := strings.TrimSpace(r.URL.Query().Get("platform"))
	type mobileAccount struct {
		ID           int64  `json:"id"`
		PlatformCode string `json:"platform_code"`
		Name         string `json:"name"`
		LoginState   string `json:"login_state"`
		Group        string `json:"group,omitempty"`
		LastCheckAt  string `json:"last_check_at,omitempty"`
	}
	out := make([]mobileAccount, 0, len(accounts))
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		if platformFilter != "" && account.PlatformCode != platformFilter {
			continue
		}
		out = append(out, mobileAccount{
			ID: account.ID, PlatformCode: account.PlatformCode, Name: account.Name,
			LoginState: account.LoginState, Group: account.AccountGroup, LastCheckAt: account.LastCheckAt,
		})
	}
	writeJSON(w, map[string]any{"accounts": out, "count": len(out)})
}

// handleAPIMobileNotifications 是手机端「有新动静」的来源。只读、限条数。
func (s *Server) handleAPIMobileNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if mobileAcceptIsBrowser(r) {
		s.rejectMobileBrowserRequest(w)
		return
	}
	items, err := s.db.ListNotifications(30, false)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"errorCode": "MOBILE_NOTIFICATIONS_FAILED"})
		return
	}
	if items == nil {
		items = []db.Notification{}
	}
	writeJSON(w, map[string]any{"notifications": items, "count": len(items)})
}

// mobileTokenFingerprint 计算令牌指纹并落到数据目录，供移动端页面核对身份。
// 只写 sha256 前 16 字符——这不是秘密，只是「连对了哪套数据」的标记。
func mobileTokenFingerprint(dataDir, token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

// writeMobileTokenFingerprint 在 CLI 启动移动端时调用；失败不影响服务。
func writeMobileTokenFingerprint(dataDir, token string) error {
	if strings.TrimSpace(dataDir) == "" || strings.TrimSpace(token) == "" {
		return nil
	}
	path := filepath.Join(dataDir, mobileTokenFingerprintFile)
	return os.WriteFile(path, []byte(mobileTokenFingerprint(dataDir, token)+"\n"), 0o600)
}
