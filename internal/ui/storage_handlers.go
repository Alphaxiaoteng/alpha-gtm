package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/observability"
)

// The data directory accumulates several independent kinds of bytes: the asset
// database, its historical snapshots, per-process diagnostic event files, the
// trend-radar media archive, and superseded binaries. None of them was ever
// reclaimed, so a long-lived install can carry hundreds of megabytes of dead
// weight that the operator cannot see from inside the app.
//
// These handlers expose that usage and let the operator reclaim the parts that
// are safe to delete. Every cleanup is scoped to one well-defined category and
// never touches the live database.

const (
	// keepDatabaseSnapshots is how many dated DB copies survive. One is enough
	// to recover from a bad migration; more than that is clutter.
	keepDatabaseSnapshots = 1
)

// storageCategory is one reclaimable bucket in the settings storage view.
type storageCategory struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Files       int    `json:"files"`
	Bytes       int64  `json:"bytes"`
	// Reclaimable reports whether this app knows how to remove these bytes.
	Reclaimable bool `json:"reclaimable"`
	// RequiresConfirm marks categories where deletion is irreversible and the
	// UI must ask before calling the cleanup endpoint.
	RequiresConfirm bool `json:"requires_confirm"`
}

type storageUsageResponse struct {
	DataDir    string            `json:"data_dir"`
	TotalBytes int64             `json:"total_bytes"`
	Categories []storageCategory `json:"categories"`
}

func (s *Server) handleAPIStorageUsage(w http.ResponseWriter, _ *http.Request) {
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "STORAGE_USAGE_FAILED")
		return
	}
	categories := []storageCategory{
		categoryFor("logs", "诊断日志", "每个进程一个事件文件；退出后可回收。", filepath.Join(dataDir, "logs"), true, false),
		categoryFor("database_snapshots", "数据库快照", "历史 app.db 副本；只保留最近一份。", "", true, true),
		categoryFor("media", "素材库", "已导入的图片与视频素材。", filepath.Join(dataDir, "media"), false, true),
		categoryFor("trend_radar", "趋势雷达归档", "博主作品与媒体归档，最大的一块。", filepath.Join(dataDir, "trend-radar"), false, true),
		categoryFor("screenshots", "失败截图", "失败证据截图，可安全清理。", filepath.Join(dataDir, "screenshots"), true, false),
		// 二进制目录里同时有正在使用的可执行文件，因此只统计、不提供清理。
		categoryFor("binaries", "程序文件", "应用与辅助二进制；运行中的文件不可删除。", filepath.Join(dataDir, "bin"), false, false),
	}
	// The snapshots category spans several files directly in the data root
	// rather than a directory, so it is measured separately.
	categories[1] = databaseSnapshotCategory(dataDir)
	var total int64
	for _, category := range categories {
		total += category.Bytes
	}
	writeJSON(w, storageUsageResponse{DataDir: dataDir, TotalBytes: total, Categories: categories})
}

// categoryFor measures a directory. A missing directory is reported as empty
// rather than as an error: a fresh install simply has nothing there yet.
func categoryFor(key, label, description, dir string, reclaimable, requiresConfirm bool) storageCategory {
	category := storageCategory{
		Key: key, Label: label, Description: description,
		Reclaimable: reclaimable, RequiresConfirm: requiresConfirm,
	}
	if dir == "" {
		return category
	}
	files, bytes := directoryUsage(dir)
	category.Files, category.Bytes = files, bytes
	return category
}

func databaseSnapshotCategory(dataDir string) storageCategory {
	category := storageCategory{
		Key: "database_snapshots", Label: "数据库快照",
		Description: "历史 app.db 副本；只保留最近一份。",
		Reclaimable: true, RequiresConfirm: true,
	}
	for _, path := range databaseSnapshotPaths(dataDir) {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		category.Files++
		category.Bytes += info.Size()
	}
	return category
}

// databaseSnapshotPaths lists dated copies of the asset database sitting next
// to app.db. Two naming shapes exist in practice, both produced by migrations
// and manual repairs:
//
//	app.db.bak-<stamp>        suffix after the live name
//	app.<reason>[-<stamp>].db different stem, still ends in .db
//
// The live app.db, its -wal/-shm companions, and unrelated databases are
// deliberately excluded.
func databaseSnapshotPaths(dataDir string) []string {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isDatabaseSnapshotName(entry.Name()) {
			continue
		}
		paths = append(paths, filepath.Join(dataDir, entry.Name()))
	}
	sort.Strings(paths)
	return paths
}

func isDatabaseSnapshotName(name string) bool {
	// SQLite companions belong to the database beside them and are removed
	// with it, never counted as snapshots in their own right.
	if strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") {
		return false
	}
	if strings.HasPrefix(name, "app.db.bak-") {
		return true
	}
	if !strings.HasPrefix(name, "app.") || !strings.HasSuffix(name, ".db") {
		return false
	}
	return name != "app.db"
}

// directoryUsage walks dir summing regular files. It tolerates unreadable
// subtrees so one bad directory cannot hide the rest of the usage.
func directoryUsage(dir string) (int, int64) {
	var files int
	var bytes int64
	_ = filepath.WalkDir(dir, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		files++
		bytes += info.Size()
		return nil
	})
	return files, bytes
}

type storageCleanupResponse struct {
	Key     string `json:"key"`
	Removed int    `json:"removed"`
	Bytes   int64  `json:"bytes"`
	Detail  string `json:"detail"`
}

func (s *Server) handleAPIStorageCleanup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key string `json:"key"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		writeToolsError(w, http.StatusInternalServerError, "STORAGE_CLEANUP_FAILED")
		return
	}
	switch strings.TrimSpace(body.Key) {
	case "logs":
		result, pruneErr := observability.PruneEventFiles(filepath.Join(dataDir, "logs"), observability.PruneEventFileOptions{
			KeepPIDs: map[int]struct{}{os.Getpid(): {}},
		})
		if pruneErr != nil {
			writeToolsError(w, http.StatusInternalServerError, "STORAGE_CLEANUP_FAILED")
			return
		}
		writeJSON(w, storageCleanupResponse{Key: "logs", Removed: result.Removed, Bytes: result.Bytes,
			Detail: "已清理不属于运行中进程的事件日志。"})
	case "database_snapshots":
		removed, bytes, cleanupErr := pruneDatabaseSnapshots(dataDir, keepDatabaseSnapshots)
		if cleanupErr != nil {
			writeToolsError(w, http.StatusInternalServerError, "STORAGE_CLEANUP_FAILED")
			return
		}
		writeJSON(w, storageCleanupResponse{Key: "database_snapshots", Removed: removed, Bytes: bytes,
			Detail: "已删除较旧的数据库快照，保留最近一份。"})
	case "screenshots":
		screenshotsDir := filepath.Join(dataDir, "screenshots")
		removed, bytes := pruneDirectoryContents(screenshotsDir)
		removeEmptySubdirs(screenshotsDir)
		writeJSON(w, storageCleanupResponse{Key: "screenshots", Removed: removed, Bytes: bytes,
			Detail: "已清空失败截图。"})
	default:
		writeToolsError(w, http.StatusBadRequest, "STORAGE_CATEGORY_UNSUPPORTED")
	}
}

// pruneDatabaseSnapshots keeps the newest keep snapshots and removes the rest,
// including their -wal/-shm companions, which are useless without the database
// they belong to.
func pruneDatabaseSnapshots(dataDir string, keep int) (int, int64, error) {
	paths := databaseSnapshotPaths(dataDir)
	if len(paths) <= keep {
		return 0, 0, nil
	}
	type snapshot struct {
		path string
		mod  time.Time
	}
	snapshots := make([]snapshot, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		snapshots = append(snapshots, snapshot{path: path, mod: info.ModTime()})
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].mod.After(snapshots[j].mod) })
	var removed int
	var bytes int64
	for _, snap := range snapshots[keep:] {
		for _, path := range []string{snap.path, snap.path + "-wal", snap.path + "-shm"} {
			info, statErr := os.Stat(path)
			if statErr != nil {
				continue
			}
			// Size is tallied before removal so a failed unlink still reports
			// what it was aiming at. The count tracks removed entries only.
			if err := os.Remove(path); err != nil {
				return removed, bytes, err
			}
			removed++
			bytes += info.Size()
		}
	}
	return removed, bytes, nil
}

// 截图实际写在子目录里（publish-inspections/、desktop/、publish/），
// 只扫顶层会让「清理失败截图」永远回收不到任何字节，而用量统计又是
// 递归算的——用户看到占用却清不掉。所以这里必须递归。
func pruneDirectoryContents(dir string) (int, int64) {
	var removed int
	var bytes int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, entry := range entries {
		if entry.IsDir() {
			// 子目录递归处理；符号链接目录在下一层被 IsDir 判断为 false，
			// 不会顺着链接遍历到目录之外。
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			subRemoved, subBytes := pruneDirectoryContents(filepath.Join(dir, entry.Name()))
			removed += subRemoved
			bytes += subBytes
			continue
		}
		// A symlink is not the file it points at. Removing the link is safe,
		// but reporting the target's size would overstate what was reclaimed,
		// and a link to something important should not be touched at all.
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		if removeErr := os.Remove(path); removeErr != nil {
			continue
		}
		removed++
		bytes += info.Size()
	}
	return removed, bytes
}

// removeEmptySubdirs 清掉递归之后留下的空目录，让「清理」之后的目录树
// 与用量统计一致（否则 usage 仍会列出这些空壳，看起来没清干净）。
func removeEmptySubdirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		child := filepath.Join(dir, entry.Name())
		removeEmptySubdirs(child)
		if remaining, readErr := os.ReadDir(child); readErr == nil && len(remaining) == 0 {
			_ = os.Remove(child)
		}
	}
}
