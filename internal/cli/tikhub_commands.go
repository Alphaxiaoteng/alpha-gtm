package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/batch"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/tikhub"
)

func addTikHubCmd(root *cobra.Command) {
	th := &cobra.Command{
		Use:   "tikhub",
		Short: "TikHub API: ranks, user monitor, search (no browser)",
		Long: `Uses TikHub HTTP API (api.tikhub.io). Set key:

  export TIKHUB_API_KEY=...
  # or
  alpha-nexus tikhub config set-key --key YOUR_KEY

Reference: xhs-comment-claw (CDP SOP) + alphaagent notes-scripts + xhs coze TikHub node.

Examples:
  alpha-nexus tikhub rank daily          # 每日榜单抓取
  alpha-nexus tikhub rank top --board video --n 10
  alpha-nexus tikhub user add --platform xhs --user-id xxx --name 竞品A
  alpha-nexus tikhub user check-all
  alpha-nexus tikhub update              # 每日一键：榜单 + 用户监控
  alpha-nexus tikhub search xhs --keyword 奢侈品奥莱
`,
	}

	// config
	cfgCmd := &cobra.Command{Use: "config", Short: "TikHub API key"}
	setKey := &cobra.Command{
		Use:   "set-key",
		Short: "Save API key to data-dir/tikhub_api_key",
		RunE: func(cmd *cobra.Command, args []string) error {
			key, _ := cmd.Flags().GetString("key")
			if key == "" {
				return fmt.Errorf("--key required")
			}
			d := dataDirForTikHub()
			if err := tikhub.SaveAPIKey(d, key); err != nil {
				return err
			}
			cmd.Printf("Saved key to %s/tikhub_api_key\n", d)
			return nil
		},
	}
	setKey.Flags().String("key", "", "TikHub API key")
	_ = setKey.MarkFlagRequired("key")
	cfgStatus := &cobra.Command{
		Use:   "status",
		Short: "Show whether API key is configured (masked)",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := dataDirForTikHub()
			key, err := tikhub.LoadAPIKey(d)
			if err != nil {
				cmd.Printf("key: missing (%v)\n", err)
				return nil
			}
			masked := key
			if len(key) > 8 {
				masked = key[:4] + "..." + key[len(key)-4:]
			}
			cmd.Printf("key: ok (%s)\ndata-dir: %s\n", masked, d)
			return nil
		},
	}
	cfgCmd.AddCommand(setKey, cfgStatus)

	// rank
	rankCmd := &cobra.Command{Use: "rank", Short: "Douyin top boards via TikHub"}
	rankDaily := &cobra.Command{
		Use:   "daily",
		Short: "Fetch and save daily top boards (video/low_fan/high_like/high_fan)",
		RunE: func(cmd *cobra.Command, args []string) error {
			boards, _ := cmd.Flags().GetString("boards")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			window, _ := cmd.Flags().GetInt("window")
			return runTikHubRankDaily(cmd, boards, pageSize, window)
		},
	}
	rankDaily.Flags().String("boards", "video,low_fan,high_like,high_fan", "Comma board names")
	rankDaily.Flags().Int("page-size", 30, "Page size")
	rankDaily.Flags().Int("window", 24, "date_window hours")

	rankList := &cobra.Command{
		Use:   "list",
		Short: "List saved daily rank files",
		RunE: func(cmd *cobra.Command, args []string) error {
			date, _ := cmd.Flags().GetString("date")
			return runTikHubRankList(cmd, date)
		},
	}
	rankList.Flags().String("date", "", "YYYY-MM-DD (default today)")

	rankTop := &cobra.Command{
		Use:   "top",
		Short: "Print top N from a saved daily board JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			board, _ := cmd.Flags().GetString("board")
			date, _ := cmd.Flags().GetString("date")
			n, _ := cmd.Flags().GetInt("n")
			if board == "" {
				board = "video"
			}
			if n <= 0 {
				n = 10
			}
			return runTikHubRankTop(cmd, board, date, n)
		},
	}
	rankTop.Flags().String("board", "video", "Board name")
	rankTop.Flags().String("date", "", "YYYY-MM-DD (default today)")
	rankTop.Flags().Int("n", 10, "Top N")

	rankCmd.AddCommand(rankDaily, rankList, rankTop)

	// user monitor
	userCmd := &cobra.Command{Use: "user", Short: "Monitor creators via TikHub"}
	userAdd := &cobra.Command{
		Use:   "add",
		Short: "Add user to watchlist",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			userID, _ := cmd.Flags().GetString("user-id")
			name, _ := cmd.Flags().GetString("name")
			note, _ := cmd.Flags().GetString("note")
			if platform == "" {
				platform = "xhs"
			}
			if userID == "" {
				return fmt.Errorf("--user-id required")
			}
			d := dataDirForTikHub()
			if err := tikhub.AddWatch(d, platform, userID, name, note); err != nil {
				return err
			}
			cmd.Printf("Watch added: %s %s\n", platform, userID)
			return nil
		},
	}
	userAdd.Flags().String("platform", "xhs", "xhs|douyin")
	userAdd.Flags().String("user-id", "", "Platform user id")
	userAdd.Flags().String("name", "", "Display name")
	userAdd.Flags().String("note", "", "Note")
	_ = userAdd.MarkFlagRequired("user-id")

	userRemove := &cobra.Command{
		Use:   "remove",
		Short: "Remove user from watchlist",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			userID, _ := cmd.Flags().GetString("user-id")
			if platform == "" {
				platform = "xhs"
			}
			if userID == "" {
				return fmt.Errorf("--user-id required")
			}
			if err := tikhub.RemoveWatch(dataDirForTikHub(), platform, userID); err != nil {
				return err
			}
			cmd.Printf("Watch removed: %s %s\n", platform, userID)
			return nil
		},
	}
	userRemove.Flags().String("platform", "xhs", "Platform")
	userRemove.Flags().String("user-id", "", "User id")
	_ = userRemove.MarkFlagRequired("user-id")

	userList := &cobra.Command{
		Use:   "list",
		Short: "List watchlist",
		RunE: func(cmd *cobra.Command, args []string) error {
			users, err := tikhub.LoadWatchlist(dataDirForTikHub())
			if err != nil {
				return err
			}
			if len(users) == 0 {
				cmd.Println("Empty watchlist. Use: tikhub user add --user-id ...")
				return nil
			}
			for _, u := range users {
				cmd.Printf("%s\t%s\t%s\tlast=%s count=%d\n", u.Platform, u.UserID, u.Name, u.LastCheck, u.LastCount)
			}
			return nil
		},
	}

	userCheck := &cobra.Command{
		Use:   "check",
		Short: "Fetch one watched user notes snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			userID, _ := cmd.Flags().GetString("user-id")
			platform, _ := cmd.Flags().GetString("platform")
			if platform == "" {
				platform = "xhs"
			}
			if userID == "" {
				return fmt.Errorf("--user-id required")
			}
			return runTikHubUserCheck(cmd, platform, userID)
		},
	}
	userCheck.Flags().String("user-id", "", "User id")
	userCheck.Flags().String("platform", "xhs", "Platform")
	_ = userCheck.MarkFlagRequired("user-id")

	userCheckAll := &cobra.Command{
		Use:   "check-all",
		Short: "Check all watchlist users and save daily snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTikHubUserCheckAll(cmd)
		},
	}

	userCmd.AddCommand(userAdd, userRemove, userList, userCheck, userCheckAll)

	// search
	searchCmd := &cobra.Command{Use: "search", Short: "Search notes / videos"}
	searchXHS := &cobra.Command{
		Use:   "xhs",
		Short: "Search Xiaohongshu notes by keyword",
		RunE: func(cmd *cobra.Command, args []string) error {
			kw, _ := cmd.Flags().GetString("keyword")
			pages, _ := cmd.Flags().GetInt("pages")
			if kw == "" {
				return fmt.Errorf("--keyword required")
			}
			if pages <= 0 {
				pages = 1
			}
			return runTikHubSearchXHS(cmd, kw, pages)
		},
	}
	searchXHS.Flags().String("keyword", "", "Keyword")
	searchXHS.Flags().Int("pages", 1, "Pages")
	_ = searchXHS.MarkFlagRequired("keyword")

	searchDY := &cobra.Command{
		Use:   "douyin",
		Short: "Search Douyin videos by keyword",
		RunE: func(cmd *cobra.Command, args []string) error {
			kw, _ := cmd.Flags().GetString("keyword")
			count, _ := cmd.Flags().GetInt("count")
			if kw == "" {
				return fmt.Errorf("--keyword required")
			}
			if count <= 0 {
				count = 10
			}
			return runTikHubSearchDouyin(cmd, kw, count)
		},
	}
	searchDY.Flags().String("keyword", "", "Keyword")
	searchDY.Flags().Int("count", 10, "Count")
	_ = searchDY.MarkFlagRequired("keyword")
	searchCmd.AddCommand(searchXHS, searchDY)

	priceCmd := &cobra.Command{
		Use:   "price",
		Short: "Query TikHub's official current price for one curated endpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			endpointID, _ := cmd.Flags().GetString("endpoint-id")
			confirm, _ := cmd.Flags().GetBool("confirm")
			if !confirm {
				return fmt.Errorf("--confirm is required for the official pricing request")
			}
			endpoint, ok := tikhub.FindEndpoint(endpointID)
			if !ok {
				return fmt.Errorf("unknown curated endpoint")
			}
			client, _, err := tikhub.NewActiveClient(dataDirForTikHub())
			if err != nil {
				return err
			}
			response, err := client.CalculatePrice(cmd.Context(), endpoint.Path, 1)
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(response)
		},
	}
	priceCmd.Flags().String("endpoint-id", "", "Curated endpoint ID")
	priceCmd.Flags().Bool("confirm", false, "Confirm one official pricing request")
	_ = priceCmd.MarkFlagRequired("endpoint-id")

	// daily all-in-one
	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Daily job: fetch ranks + check all watched users",
		Long:  "Equivalent to: tikhub rank daily && tikhub user check-all.",
		RunE: func(cmd *cobra.Command, args []string) error {
			boards, _ := cmd.Flags().GetString("boards")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			window, _ := cmd.Flags().GetInt("window")
			skipUsers, _ := cmd.Flags().GetBool("skip-users")
			batch.EmitNDJSON(map[string]interface{}{"event": "tikhub.update.started"})
			if err := runTikHubRankDaily(cmd, boards, pageSize, window); err != nil {
				return err
			}
			if !skipUsers {
				if err := runTikHubUserCheckAll(cmd); err != nil {
					return err
				}
			}
			batch.EmitNDJSON(map[string]interface{}{"event": "tikhub.update.finished", "date": tikhub.Today()})
			cmd.Printf("Daily update done for %s\n", tikhub.Today())
			return nil
		},
	}
	updateCmd.Flags().String("boards", "video,low_fan,high_like,high_fan", "Comma board names")
	updateCmd.Flags().Int("page-size", 30, "Page size")
	updateCmd.Flags().Int("window", 24, "date_window hours")
	updateCmd.Flags().Bool("skip-users", false, "Only ranks, skip user watchlist")

	th.AddCommand(cfgCmd, rankCmd, userCmd, searchCmd, priceCmd, updateCmd)
	root.AddCommand(th)
}

func dataDirForTikHub() string {
	cfg := activeCfg
	if cfg.DataDir == "" {
		cfg = config.DefaultConfig()
	}
	d, err := cfg.ResolvedDataDir()
	if err != nil {
		return ""
	}
	return d
}

func tikhubClient() (*tikhub.Client, string, error) {
	d := dataDirForTikHub()
	key, err := tikhub.LoadAPIKey(d)
	if err != nil {
		return nil, d, err
	}
	return tikhub.New(key), d, nil
}

func runTikHubRankDaily(cmd *cobra.Command, boardsCSV string, pageSize, window int) error {
	c, dataDir, err := tikhubClient()
	if err != nil {
		return err
	}
	boards := strings.Split(boardsCSV, ",")
	today := tikhub.Today()
	batch.EmitNDJSON(map[string]interface{}{"event": "tikhub.rank.daily.started", "date": today, "boards": boards})

	summary := map[string]interface{}{
		"date":       today,
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
		"boards":     map[string]interface{}{},
	}
	boardMap := summary["boards"].(map[string]interface{})

	ctx := context.Background()
	for _, b := range boards {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		cmd.Printf("Fetching board %s...\n", b)
		resp, err := c.FetchDouyinRank(ctx, b, 1, pageSize, window)
		if err != nil {
			cmd.Printf("  FAIL %s: %v\n", b, err)
			boardMap[b] = map[string]interface{}{"error": err.Error()}
			continue
		}
		items := tikhub.ExtractRankItems(resp)
		sub := filepath.Join("ranks", today, b+".json")
		fp, err := tikhub.SaveJSON(dataDir, sub, map[string]interface{}{
			"board": b, "fetched_at": summary["fetched_at"], "raw": resp, "items": items, "count": len(items),
		})
		if err != nil {
			return err
		}
		md, _ := tikhub.WriteRankMarkdown(dataDir, today, b, items, fmt.Sprint(summary["fetched_at"]))
		boardMap[b] = map[string]interface{}{"count": len(items), "file": fp, "md": md}
		cmd.Printf("  OK %s items=%d → %s\n", b, len(items), fp)
		// print top 5
		for i, it := range tikhub.TopItems(items, 5) {
			title := fmt.Sprint(it["item_title"])
			if title == "" || title == "<nil>" {
				title = fmt.Sprint(it["title"])
			}
			cmd.Printf("    %d. %s\n", i+1, truncateRunes(title, 50))
		}
	}

	// also hot search
	if hs, err := c.FetchDouyinHotSearch(ctx); err == nil {
		items := tikhub.ExtractRankItems(hs)
		sub := filepath.Join("ranks", today, "hot_search.json")
		fp, _ := tikhub.SaveJSON(dataDir, sub, map[string]interface{}{"fetched_at": summary["fetched_at"], "raw": hs, "items": items})
		md, _ := tikhub.WriteRankMarkdown(dataDir, today, "hot_search", items, fmt.Sprint(summary["fetched_at"]))
		boardMap["hot_search"] = map[string]interface{}{"count": len(items), "file": fp, "md": md}
		cmd.Printf("  OK hot_search items=%d → %s\n", len(items), fp)
	}

	indexPath, _ := tikhub.SaveJSON(dataDir, filepath.Join("ranks", today, "index.json"), summary)
	batch.EmitNDJSON(map[string]interface{}{"event": "tikhub.rank.daily.finished", "index": indexPath})
	cmd.Printf("Daily rank index: %s\n", indexPath)
	return nil
}

func runTikHubRankList(cmd *cobra.Command, date string) error {
	if date == "" {
		date = tikhub.Today()
	}
	d := dataDirForTikHub()
	dir := filepath.Join(d, "tikhub", "ranks", date)
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		cmd.Printf("No rank files for %s under %s\n", date, dir)
		return nil
	}
	for _, e := range entries {
		cmd.Println(e)
	}
	return nil
}

func runTikHubRankTop(cmd *cobra.Command, board, date string, n int) error {
	if date == "" {
		date = tikhub.Today()
	}
	d := dataDirForTikHub()
	fp := filepath.Join(d, "tikhub", "ranks", date, board+".json")
	raw, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Errorf("read %s: %w (run: tikhub rank daily first)", fp, err)
	}
	var payload struct {
		Items []map[string]interface{} `json:"items"`
		Count int                      `json:"count"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	items := tikhub.TopItems(payload.Items, n)
	cmd.Printf("Top %d · %s · %s (saved %d)\n", len(items), board, date, payload.Count)
	for i, it := range items {
		title := fmt.Sprint(it["item_title"])
		if title == "" || title == "<nil>" {
			title = fmt.Sprint(it["title"], it["word"], it["sentence"])
		}
		nick := fmt.Sprint(it["nick_name"])
		cmd.Printf("%2d. %s", i+1, truncateRunes(title, 56))
		if nick != "" && nick != "<nil>" {
			cmd.Printf("  @%s", nick)
		}
		cmd.Println()
	}
	return nil
}

func runTikHubUserCheck(cmd *cobra.Command, platform, userID string) error {
	c, dataDir, err := tikhubClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	if platform != "xhs" {
		return fmt.Errorf("user monitor currently implements xhs (got %s)", platform)
	}
	info, err := c.GetXHSUserInfo(ctx, userID)
	if err != nil {
		return fmt.Errorf("user info: %w", err)
	}
	notes, err := c.GetXHSUserNotes(ctx, userID, "")
	if err != nil {
		return fmt.Errorf("user notes: %w", err)
	}
	items := tikhub.ExtractRankItems(notes)
	// also try nested notes list
	if len(items) == 0 {
		if data, ok := notes["data"].(map[string]interface{}); ok {
			if inner, ok := data["data"].(map[string]interface{}); ok {
				items = tikhub.AsMapSlice(inner["notes"])
				if len(items) == 0 {
					items = tikhub.ExtractRankItems(map[string]interface{}{"data": inner})
				}
			}
		}
	}
	_ = tikhub.UpdateWatchStats(dataDir, platform, userID, len(items))
	sub := filepath.Join("users", tikhub.Today(), platform+"_"+userID+".json")
	fp, err := tikhub.SaveJSON(dataDir, sub, map[string]interface{}{
		"user_id": userID, "platform": platform, "info": info, "notes": notes, "note_count": len(items),
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	cmd.Printf("User %s notes≈%d → %s\n", userID, len(items), fp)
	return nil
}

func runTikHubUserCheckAll(cmd *cobra.Command) error {
	users, err := tikhub.LoadWatchlist(dataDirForTikHub())
	if err != nil {
		return err
	}
	if len(users) == 0 {
		cmd.Println("Watchlist empty.")
		return nil
	}
	batch.EmitNDJSON(map[string]interface{}{"event": "tikhub.user.check_all.started", "total": len(users)})
	ok, fail := 0, 0
	for _, u := range users {
		cmd.Printf("== %s %s %s ==\n", u.Platform, u.UserID, u.Name)
		if err := runTikHubUserCheck(cmd, u.Platform, u.UserID); err != nil {
			cmd.Printf("FAIL: %v\n", err)
			fail++
			continue
		}
		ok++
	}
	batch.EmitNDJSON(map[string]interface{}{"event": "tikhub.user.check_all.finished", "success": ok, "failed": fail})
	cmd.Printf("Done success=%d failed=%d\n", ok, fail)
	return nil
}

func runTikHubSearchXHS(cmd *cobra.Command, keyword string, pages int) error {
	c, dataDir, err := tikhubClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	var all []map[string]interface{}
	for p := 1; p <= pages; p++ {
		cmd.Printf("page %d...\n", p)
		resp, err := c.SearchXHSNotes(ctx, keyword, p)
		if err != nil {
			return err
		}
		items := tikhub.ExtractRankItems(resp)
		// xhs items often under data.data.items
		if len(items) == 0 {
			if data, ok := resp["data"].(map[string]interface{}); ok {
				if inner, ok := data["data"].(map[string]interface{}); ok {
					if arr, ok := inner["items"].([]interface{}); ok {
						for _, it := range arr {
							if m, ok := it.(map[string]interface{}); ok {
								if note, ok := m["note"].(map[string]interface{}); ok {
									items = append(items, note)
								} else {
									items = append(items, m)
								}
							}
						}
					}
				}
			}
		}
		all = append(all, items...)
		cmd.Printf("  +%d (total %d)\n", len(items), len(all))
		if len(items) == 0 {
			break
		}
	}
	sub := filepath.Join("search", tikhub.Today(), "xhs_"+sanitizeName(keyword)+".json")
	fp, err := tikhub.SaveJSON(dataDir, sub, map[string]interface{}{
		"keyword": keyword, "count": len(all), "items": all,
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	cmd.Printf("Saved %d notes → %s\n", len(all), fp)
	for i, it := range tikhub.TopItems(all, 5) {
		title := fmt.Sprint(it["title"], it["display_title"], it["desc"])
		cmd.Printf("  %d. %s\n", i+1, truncateRunes(title, 48))
	}
	return nil
}

func runTikHubSearchDouyin(cmd *cobra.Command, keyword string, count int) error {
	c, dataDir, err := tikhubClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	resp, err := c.SearchDouyinVideos(ctx, keyword, 0, count)
	if err != nil {
		return err
	}
	items := tikhub.ExtractRankItems(resp)
	if len(items) == 0 {
		if data, ok := resp["data"].(map[string]interface{}); ok {
			items = tikhub.AsMapSlice(data["aweme_list"])
			if len(items) == 0 {
				if inner, ok := data["data"].(map[string]interface{}); ok {
					items = tikhub.AsMapSlice(inner["aweme_list"])
					if len(items) == 0 {
						items = tikhub.ExtractRankItems(map[string]interface{}{"data": inner})
					}
				}
			}
		}
	}
	sub := filepath.Join("search", tikhub.Today(), "douyin_"+sanitizeName(keyword)+".json")
	fp, err := tikhub.SaveJSON(dataDir, sub, map[string]interface{}{
		"keyword": keyword, "count": len(items), "items": items, "raw": resp,
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	cmd.Printf("Saved %d videos → %s\n", len(items), fp)
	for i, it := range tikhub.TopItems(items, 5) {
		title := fmt.Sprint(it["desc"], it["title"], it["item_title"])
		cmd.Printf("  %d. %s\n", i+1, truncateRunes(title, 48))
	}
	return nil
}

func sanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', ' ':
			return '_'
		}
		return r
	}, s)
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
