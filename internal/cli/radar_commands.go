package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/agenthub"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/radar"
	"alpha-nexus-egolite/internal/tikhub"
	"alpha-nexus-egolite/internal/tools"
	"github.com/spf13/cobra"
)

func addRadarCmd(root *cobra.Command) {
	radarCmd := &cobra.Command{Use: "radar", Short: "Trend Radar local worker"}
	defaultLegacyDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		defaultLegacyDir = filepath.Join(home, ".alpha-nexus")
	}
	var legacyDataDir string
	var confirmLegacyImport bool
	importLegacy := &cobra.Command{
		Use:   "import-legacy",
		Short: "Replace Trend Radar data with an earlier Alpha-Nexus local archive",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirmLegacyImport {
				return fmt.Errorf("confirmation required: pass --confirm to replace current Trend Radar data")
			}
			targetDataDir, err := activeCfg.ResolvedDataDir()
			if err != nil {
				return err
			}
			database, err := db.Open(targetDataDir)
			if err != nil {
				return err
			}
			defer database.Close()
			if err := database.Migrate(); err != nil {
				return err
			}
			result, err := radar.ImportLegacyTrendRadar(cmd.Context(), legacyDataDir, targetDataDir, database.DB)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		},
	}
	importLegacy.Flags().StringVar(&legacyDataDir, "source-data-dir", defaultLegacyDir, "Earlier Alpha-Nexus data directory")
	importLegacy.Flags().BoolVar(&confirmLegacyImport, "confirm", false, "Confirm replacement after a recoverable backup")
	workerCmd := &cobra.Command{Use: "worker", Short: "Run or install the independent Radar worker"}
	runOnce := &cobra.Command{
		Use:   "run-once",
		Short: "Run at most one due pass and exit",
		RunE: func(cmd *cobra.Command, args []string) error {
			dataDir, err := activeCfg.ResolvedDataDir()
			if err != nil {
				return err
			}
			summary, err := radar.RunDue(cmd.Context(), dataDir, time.Now())
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(summary)
		},
	}
	install := &cobra.Command{
		Use:   "install",
		Short: "Install a per-user macOS LaunchAgent for Radar schedules",
		RunE: func(cmd *cobra.Command, args []string) error {
			dataDir, err := activeCfg.ResolvedDataDir()
			if err != nil {
				return err
			}
			return radar.InstallWorker(cmd.Context(), dataDir)
		},
	}
	workerCmd.AddCommand(runOnce, install)
	var analyzeCallID int64
	var forceAnalysis bool
	analyzeVideo := &cobra.Command{
		Use:   "analyze-video",
		Short: "Analyze one saved Douyin video snapshot with the selected Agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			dataDir, err := activeCfg.ResolvedDataDir()
			if err != nil {
				return err
			}
			database, err := db.Open(dataDir)
			if err != nil {
				return err
			}
			defer database.Close()
			call, err := database.GetTikHubAPICall(analyzeCallID)
			if err != nil {
				return err
			}
			if call.Status != "succeeded" || call.PlatformCode != "douyin" || call.Capability != "content_detail" {
				return fmt.Errorf("call %d is not a successful Douyin video detail record", analyzeCallID)
			}
			root, err := filepath.Abs(filepath.Join(dataDir, "tikhub", "calls"))
			if err != nil {
				return err
			}
			target, err := filepath.Abs(call.RawPath)
			if err != nil || (target != root && !strings.HasPrefix(target, root+string(os.PathSeparator))) {
				return fmt.Errorf("saved snapshot path is outside the TikHub call archive")
			}
			file, err := os.Open(target)
			if err != nil {
				return err
			}
			defer file.Close()
			var snapshot tikhub.CallSnapshot
			decoder := json.NewDecoder(io.LimitReader(file, 64<<20))
			decoder.UseNumber()
			if err := decoder.Decode(&snapshot); err != nil {
				return err
			}
			if snapshot.CallID != call.ID || snapshot.Platform != "douyin" {
				return fmt.Errorf("saved snapshot does not match call %d", call.ID)
			}
			agentStore := agenthub.NewStore(database)
			if err := agentStore.EnsureLegacyOptionalProviders(cmd.Context()); err != nil {
				return err
			}
			providerConfigs, err := agentStore.ListProviderConfigs(cmd.Context())
			if err != nil {
				return err
			}
			registry, err := agenthub.DefaultRegistryWithConfigs(providerConfigs)
			if err != nil {
				return err
			}
			hub, err := agenthub.NewService(agentStore, registry, dataDir)
			if err != nil {
				return err
			}
			defer hub.Shutdown(context.Background())
			selection, err := hub.TextTaskSelection(cmd.Context())
			if err != nil {
				return fmt.Errorf("selected Agent is not ready: %w", err)
			}
			evidence, err := radar.ExtractVideoEvidence(snapshot)
			if err != nil {
				return err
			}
			var agentResult agenthub.TextTaskResult
			analysis, reused, err := radar.AnalyzeVideo(
				cmd.Context(), dataDir, snapshot, string(selection.ProviderID), selection.ModelID,
				func(ctx context.Context, system, user string) (string, error) {
					agentResult, err = hub.CompleteTextTask(
						ctx,
						*selection,
						"趋势雷达 · 视频拆解",
						system+"\n\nInput evidence JSON\n"+user,
						agenthub.ContextSnapshot{
							Workspace: "trend-radar",
							Page:      "video-analysis",
							Selections: []agenthub.ContextSelection{{
								ID: evidence.VideoID, Name: "抖音视频 " + evidence.VideoID,
							}},
						},
					)
					return agentResult.Answer, err
				},
				forceAnalysis,
			)
			if err != nil {
				return err
			}
			path, _ := radar.VideoAnalysisPath(dataDir, analysis.VideoID)
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
				"video_id": analysis.VideoID, "status": analysis.Status,
				"source_call_id": analysis.SourceCallID, "reused": reused,
				"provider": analysis.Provider, "model": analysis.Model,
				"agent_run_id": agentResult.RunID,
				"duration_ms":  analysis.DurationMS, "path": path,
			})
		},
	}
	analyzeVideo.Flags().Int64Var(&analyzeCallID, "call-id", 0, "saved TikHub call ID")
	_ = analyzeVideo.MarkFlagRequired("call-id")
	analyzeVideo.Flags().BoolVar(&forceAnalysis, "force", false, "replace an existing successful analysis")
	var creatorUserID, creatorName, creatorPlatform string
	var creatorMaxPages int
	var creatorSkipMedia, creatorMediaOnly bool
	creatorCmd := &cobra.Command{Use: "creator", Short: "Archive one creator's posts into a local folder"}
	creatorArchive := &cobra.Command{
		Use:   "archive",
		Short: "Archive every post of a creator, streaming NDJSON progress",
		Long: `Archive every post of one creator into its local folder. Progress and the
final summary are newline-delimited JSON, one object per line:

  {"type":"progress","phase":"posts","done":42,"total":0}
  {"type":"result","platform":"douyin","user_id":"...","posts":42,...}

total is 0 while the platform has not reported a page count; it is never faked.

With --media-only no page is fetched at all: the existing index already holds every
post and its source URL, so retrying a failed cover costs no TikHub credit.

The creator must already be on the local watchlist: the id is checked before the
first provider call, so a typo cannot burn credit or leave an empty folder.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			userID := strings.TrimSpace(creatorUserID)
			if userID == "" {
				return fmt.Errorf("--user-id is required")
			}
			platform := strings.ToLower(strings.TrimSpace(creatorPlatform))
			if platform == "" {
				platform = "douyin"
			}
			if platform != "douyin" {
				return fmt.Errorf("creator archive currently supports douyin only, got %q", platform)
			}
			dataDir, err := activeCfg.ResolvedDataDir()
			if err != nil {
				return err
			}
			// 先在监控名单里核对身份再动手：打错一个字符就会白烧一次真实调用，
			// 并在数据目录里留下一个空文件夹。名单是博主身份的唯一真源。
			watchlist, err := tikhub.LoadWatchlist(dataDir)
			if err != nil {
				return fmt.Errorf("read watchlist: %w", err)
			}
			watch, found := tikhub.WatchUser{}, false
			for _, candidate := range watchlist {
				if candidate.Platform == platform && candidate.UserID == userID {
					watch, found = candidate, true
					break
				}
			}
			if !found {
				return fmt.Errorf(
					"creator %s/%s is not in the local watchlist; add it in the trend radar first",
					platform, userID,
				)
			}
			if name := strings.TrimSpace(creatorName); name != "" {
				watch.Name = name
			}
			sink := tools.NewCreatorProgressWriter(cmd.OutOrStdout())
			summary, err := tools.RunCreatorArchive(cmd.Context(), nil, dataDir, tools.CreatorArchiveOptions{
				UserID:     userID,
				Name:       watch.Name,
				Platform:   platform,
				MaxPages:   creatorMaxPages,
				WithMedia:  !creatorSkipMedia,
				MediaOnly:  creatorMediaOnly,
				OnProgress: sink.OnProgress,
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
				"type": "result", "platform": platform, "user_id": userID,
				"folder": summary.Folder, "index_path": summary.IndexPath,
				"pages": summary.Pages, "posts": summary.Posts, "fetched": summary.Fetched,
				"new_posts": summary.NewPosts, "stopped": summary.Stopped,
				"media": map[string]any{
					"covers":  summary.Media.Covers,
					"audio":   summary.Media.Audio,
					"skipped": summary.Media.Skipped,
					"failed":  summary.Media.Failed,
					"total":   summary.Media.Total,
				},
				"call_ids": summary.CallIDs,
			})
		},
	}
	creatorArchive.Flags().StringVar(&creatorUserID, "user-id", "", "Douyin sec_user_id (required)")
	_ = creatorArchive.MarkFlagRequired("user-id")
	creatorArchive.Flags().StringVar(&creatorName, "name", "", "Override the display name stored with the archive")
	creatorArchive.Flags().StringVar(&creatorPlatform, "platform", "douyin", "Creator platform; only douyin is supported")
	creatorArchive.Flags().IntVar(&creatorMaxPages, "max-pages", 0, "Page cap; 0 = until the platform ends the list")
	creatorArchive.Flags().BoolVar(&creatorSkipMedia, "skip-media", false, "Only write the manifest, download nothing")
	creatorArchive.Flags().BoolVar(&creatorMediaOnly, "media-only", false, "Skip the list fetch; only re-download covers/audio missing from the existing archive")
	creatorCmd.AddCommand(creatorArchive)
	radarCmd.AddCommand(importLegacy, workerCmd, analyzeVideo, creatorCmd)
	root.AddCommand(radarCmd)
}
