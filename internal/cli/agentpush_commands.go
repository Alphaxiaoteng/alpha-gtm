package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"alpha-nexus-egolite/internal/agents"
	"alpha-nexus-egolite/internal/agentws"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/mcp"
	"alpha-nexus-egolite/internal/media"
	"alpha-nexus-egolite/internal/observability"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publish"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/queue"
	"alpha-nexus-egolite/internal/terminal"
	"alpha-nexus-egolite/internal/trace"
)

// addAgentPushCmds registers AgentPush content/media/queue/agent/metrics/audit/export commands.
func addAgentPushCmds(root *cobra.Command) {
	addMediaCmds(root)
	addContentAgentPushCmds(root)
	addCredentialCmds(root)
	addQueueCmds(root)
	addAgentCmds(root)
	addMetricsCmds(root)
	addAuditCmds(root)
	addExportCmds(root)
}

// ---- media ----

func addMediaCmds(root *cobra.Command) {
	mediaCmd := &cobra.Command{Use: "media", Short: "Media library import, index, preflight"}

	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Probe a media file, print sha256, upsert media_library",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Flags().GetString("path")
			if path == "" {
				return fmt.Errorf("--path is required")
			}
			meta, err := media.Probe(path)
			if err != nil {
				return err
			}
			cmd.Printf("path:    %s\n", meta.Path)
			cmd.Printf("sha256:  %s\n", meta.SHA256)
			cmd.Printf("size:    %d\n", meta.SizeBytes)
			cmd.Printf("mime:    %s\n", meta.MimeGuess)
			if meta.Width > 0 || meta.Height > 0 {
				cmd.Printf("dims:    %dx%d\n", meta.Width, meta.Height)
			}
			if meta.DurationMS > 0 {
				cmd.Printf("duration_ms: %d\n", meta.DurationMS)
			}

			d := openQuickDB()
			defer d.Close()
			entry, err := d.UpsertMediaLibrary(meta.Path, meta.SHA256, meta.SizeBytes, meta.MimeGuess, meta.Width, meta.Height, meta.DurationMS, meta.ProbeJSON)
			if err != nil {
				return err
			}
			cmd.Printf("media_library: #%d\n", entry.ID)
			_, _ = d.CreateAuditEvent(string(trace.New()), "info", "media", "import "+meta.Path, "media_library", entry.ID, "")
			return nil
		},
	}
	importCmd.Flags().String("path", "", "File path to import")
	_ = importCmd.MarkFlagRequired("path")

	indexCmd := &cobra.Command{
		Use:   "index",
		Short: "Walk a directory and index media files",
		RunE: func(cmd *cobra.Command, args []string) error {
			rootDir, _ := cmd.Flags().GetString("root")
			recursive, _ := cmd.Flags().GetBool("recursive")
			if rootDir == "" {
				return fmt.Errorf("--root is required")
			}
			return runMediaIndex(cmd, rootDir, recursive)
		},
	}
	indexCmd.Flags().String("root", "", "Root directory to index")
	indexCmd.Flags().Bool("recursive", true, "Walk subdirectories")
	_ = indexCmd.MarkFlagRequired("root")

	preflightCmd := &cobra.Command{
		Use:   "preflight",
		Short: "Validate media against platform-ish rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Flags().GetString("path")
			platform, _ := cmd.Flags().GetString("platform")
			if path == "" {
				return fmt.Errorf("--path is required")
			}
			if platform == "" {
				return fmt.Errorf("--platform is required")
			}
			r, err := media.Preflight(path, platform, "")
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(r, "", "  ")
			cmd.Println(string(b))
			if !r.OK {
				return fmt.Errorf("preflight failed")
			}
			return nil
		},
	}
	preflightCmd.Flags().String("path", "", "Media file path")
	preflightCmd.Flags().String("platform", "", "Platform code")
	_ = preflightCmd.MarkFlagRequired("path")
	_ = preflightCmd.MarkFlagRequired("platform")

	mediaCmd.AddCommand(importCmd, indexCmd, preflightCmd)
	root.AddCommand(mediaCmd)
}

func runMediaIndex(cmd *cobra.Command, rootDir string, recursive bool) error {
	items, err := media.WalkIndex(rootDir, recursive)
	if err != nil {
		return err
	}
	d := openQuickDB()
	defer d.Close()
	for _, m := range items {
		entry, err := d.UpsertMediaLibrary(m.Path, m.SHA256, m.SizeBytes, m.MimeGuess, m.Width, m.Height, m.DurationMS, m.ProbeJSON)
		if err != nil {
			cmd.Printf("skip %s: %v\n", m.Path, err)
			continue
		}
		cmd.Printf("#%d %s %s\n", entry.ID, m.SHA256[:12], m.Path)
	}
	cmd.Printf("Indexed %d file(s).\n", len(items))
	return nil
}

// ---- content version / adapt (extends existing content cmd) ----

func addContentAgentPushCmds(root *cobra.Command) {
	contentCmd := findCommand(root, "content")
	if contentCmd == nil {
		contentCmd = &cobra.Command{Use: "content", Short: "Manage content items"}
		root.AddCommand(contentCmd)
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Create a language/version snapshot of content",
		RunE: func(cmd *cobra.Command, args []string) error {
			contentID, _ := cmd.Flags().GetInt64("content")
			lang, _ := cmd.Flags().GetString("lang")
			title, _ := cmd.Flags().GetString("title")
			caption, _ := cmd.Flags().GetString("caption")
			if contentID == 0 {
				return fmt.Errorf("--content is required")
			}
			d := openQuickDB()
			defer d.Close()
			if _, err := d.GetContent(contentID); err != nil {
				return err
			}
			v, err := d.CreateContentVersion(contentID, lang, title, caption, "", "")
			if err != nil {
				return err
			}
			cmd.Printf("Content version created: #%d (content=%d lang=%s)\n", v.ID, v.ContentID, v.Lang)
			_, _ = d.CreateAuditEvent(string(trace.New()), "info", "content", "version", "content", contentID, "")
			return nil
		},
	}
	versionCmd.Flags().Int64("content", 0, "Content ID")
	versionCmd.Flags().String("lang", "en", "Language code")
	versionCmd.Flags().String("title", "", "Version title")
	versionCmd.Flags().String("caption", "", "Version caption")
	_ = versionCmd.MarkFlagRequired("content")

	adaptCmd := &cobra.Command{
		Use:   "adapt",
		Short: "Adapt text for a platform (AdaptAgent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			text, _ := cmd.Flags().GetString("text")
			if platform == "" {
				return fmt.Errorf("--platform is required")
			}
			if text == "" {
				return fmt.Errorf("--text is required")
			}
			ac := &agents.Context{
				TaskID:       "cli-adapt",
				Platform:     platform,
				MaxSteps:     10,
				AllowedTools: agents.DefaultTools(),
				TraceID:      trace.New(),
			}
			r, err := agents.AdaptAgent(context.Background(), ac, text, platform)
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(r, "", "  ")
			cmd.Println(string(b))
			return nil
		},
	}
	adaptCmd.Flags().String("platform", "", "Target platform")
	adaptCmd.Flags().String("text", "", "Source caption/text")
	_ = adaptCmd.MarkFlagRequired("platform")
	_ = adaptCmd.MarkFlagRequired("text")

	contentCmd.AddCommand(versionCmd, adaptCmd)
}

// ---- queue ----

func addQueueCmds(root *cobra.Command) {
	queueCmd := &cobra.Command{Use: "queue", Short: "SQLite-backed task queue"}

	enqueueCmd := &cobra.Command{
		Use:   "enqueue",
		Short: "Enqueue a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, _ := cmd.Flags().GetString("kind")
			payload, _ := cmd.Flags().GetString("payload")
			if kind == "" {
				return fmt.Errorf("--kind is required")
			}
			if strings.ToLower(strings.TrimSpace(kind)) != "publish" {
				return fmt.Errorf("unsupported queue kind %q: only publish is executable", kind)
			}
			if payload == "" {
				payload = "{}"
			}
			d := openQuickDB()
			defer d.Close()
			tid := string(trace.New())
			t, err := d.EnqueueTask(kind, payload, 0, "", tid)
			if err != nil {
				return err
			}
			cmd.Printf("Enqueued task #%d kind=%s trace=%s\n", t.ID, t.Kind, t.TraceID)
			_, _ = d.CreateAuditEvent(tid, "info", "queue", "enqueue "+kind, "task_queue", t.ID, payload)
			return nil
		},
	}
	enqueueCmd.Flags().String("kind", "", "Task kind (publish|agent|metrics)")
	enqueueCmd.Flags().String("payload", "{}", "JSON payload")
	_ = enqueueCmd.MarkFlagRequired("kind")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List queue tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := openQuickDB()
			defer d.Close()
			items, err := d.ListTaskQueue(50)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "ID\tKIND\tSTATUS\tATTEMPTS\tTRACE")
			for _, t := range items {
				fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%s\n", t.ID, t.Kind, t.Status, t.Attempts, t.TraceID)
			}
			w.Flush()
			if len(items) == 0 {
				cmd.Println("No queue tasks.")
			}
			return nil
		},
	}

	runOnceCmd := &cobra.Command{
		Use:   "run-once",
		Short: "Lease and complete one pending queue task",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := openQuickDB()
			defer d.Close()
			var handled *db.TaskQueueItem
			worker := &queue.DBWorker{
				Store: d, Owner: fmt.Sprintf("cli-run-once:%d", os.Getpid()), LeaseTTL: time.Minute,
				Handler: func(ctx context.Context, item *db.TaskQueueItem) error {
					handled = item
					return handleQueueTask(cmd, d, item)
				},
			}
			err := worker.RunOnce(cmd.Context())
			if handled == nil && err == nil {
				cmd.Println("No pending tasks.")
				return nil
			}
			status := "done"
			if err != nil {
				status = "failed"
			}
			cmd.Printf("Ran task #%d kind=%s status=%s\n", handled.ID, handled.Kind, status)
			// The queue row stores only a stable code; audit follows the same rule.
			code := ""
			if err != nil {
				code = observability.StableErrorCode(err.Error())
			}
			_, _ = d.CreateAuditEvent(handled.TraceID, "info", "queue", "run-once "+status, "task_queue", handled.ID, code)
			return err
		},
	}

	queueCmd.AddCommand(enqueueCmd, listCmd, runOnceCmd)
	workerCmd := &cobra.Command{
		Use:   "worker",
		Short: "Run the durable leased queue worker until interrupted",
		RunE: func(cmd *cobra.Command, args []string) error {
			owner, _ := cmd.Flags().GetString("owner")
			if owner == "" {
				owner = fmt.Sprintf("cli-worker:%d", os.Getpid())
			}
			poll, _ := cmd.Flags().GetDuration("poll")
			lease, _ := cmd.Flags().GetDuration("lease")
			d := openQuickDB()
			defer d.Close()
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			worker := &queue.DBWorker{
				Store: d, Owner: owner, PollEvery: poll, LeaseTTL: lease,
				Handler: func(ctx context.Context, item *db.TaskQueueItem) error {
					return handleQueueTask(cmd, d, item)
				},
				OnError: func(err error) {
					cmd.Printf("Queue worker error: %s\n", observability.StableErrorCode(err.Error()))
				},
			}
			cmd.Printf("Queue worker started owner=%s\n", owner)
			err := worker.Loop(ctx)
			if err == context.Canceled {
				return nil
			}
			return err
		},
	}
	workerCmd.Flags().String("owner", "", "Stable worker owner ID (default: cli-worker:<pid>)")
	workerCmd.Flags().Duration("poll", 2*time.Second, "Queue poll interval")
	workerCmd.Flags().Duration("lease", time.Minute, "Task lease duration")
	queueCmd.AddCommand(workerCmd)
	// Alias: run → run-once
	queueCmd.AddCommand(&cobra.Command{
		Use:   "run",
		Short: "Alias for run-once",
		RunE:  runOnceCmd.RunE,
	})
	root.AddCommand(queueCmd)
}

// ---- agent ----

func addAgentCmds(root *cobra.Command) {
	agentCmd := &cobra.Command{Use: "agent", Short: "Run AgentPush agents (content/adapt/publish/monitor/analytics)"}

	contentCmd := &cobra.Command{
		Use:   "content",
		Short: "ContentAgent: draft titles/captions",
		RunE: func(cmd *cobra.Command, args []string) error {
			draft, _ := cmd.Flags().GetString("draft")
			lang, _ := cmd.Flags().GetString("lang")
			if draft == "" {
				return fmt.Errorf("--draft is required")
			}
			if lang == "" {
				lang = "en"
			}
			ac := &agents.Context{
				TaskID: "cli-content", MaxSteps: 10, AllowedTools: agents.DefaultTools(), TraceID: trace.New(),
			}
			r, err := agents.ContentAgent(context.Background(), ac, draft, lang)
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(r, "", "  ")
			cmd.Println(string(b))
			return nil
		},
	}
	contentCmd.Flags().String("draft", "", "Draft text")
	contentCmd.Flags().String("lang", "en", "Language")
	_ = contentCmd.MarkFlagRequired("draft")

	adaptCmd := &cobra.Command{
		Use:   "adapt",
		Short: "AdaptAgent: platform-compressed caption",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			text, _ := cmd.Flags().GetString("text")
			if platform == "" || text == "" {
				return fmt.Errorf("--platform and --text are required")
			}
			ac := &agents.Context{
				TaskID: "cli-agent-adapt", Platform: platform, MaxSteps: 10,
				AllowedTools: agents.DefaultTools(), TraceID: trace.New(),
			}
			r, err := agents.AdaptAgent(context.Background(), ac, text, platform)
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(r, "", "  ")
			cmd.Println(string(b))
			return nil
		},
	}
	adaptCmd.Flags().String("platform", "", "Platform")
	adaptCmd.Flags().String("text", "", "Text")
	_ = adaptCmd.MarkFlagRequired("platform")
	_ = adaptCmd.MarkFlagRequired("text")

	publishPlanCmd := &cobra.Command{
		Use:   "publish-plan",
		Short: "Explain why an executable publish request is required",
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, _ := cmd.Flags().GetString("summary")
			alwaysReview, _ := cmd.Flags().GetBool("always-review")
			if summary == "" {
				return fmt.Errorf("--summary is required")
			}
			mode := agents.ApprovalTrustedAutoSubmit
			if alwaysReview {
				mode = agents.ApprovalAlwaysReview
			}
			ac := &agents.Context{
				TaskID: "cli-publish", ApprovalMode: mode, MaxSteps: 10,
				AllowedTools: agents.DefaultTools(), TraceID: trace.New(),
			}
			r, err := agents.PublishAgent(context.Background(), ac, summary)
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(r, "", "  ")
			cmd.Println(string(b))
			return fmt.Errorf("%s", r.Data["error_code"])
		},
	}
	publishPlanCmd.Flags().String("summary", "", "Requested publish summary (does not create a job)")
	publishPlanCmd.Flags().Bool("always-review", false, "Retained for compatibility; does not create a review job")
	_ = publishPlanCmd.MarkFlagRequired("summary")

	// Compatibility alias. It also exits non-zero instead of pretending to publish.
	publishAlias := &cobra.Command{
		Use:   "publish",
		Short: "Deprecated alias; use the top-level publish command",
		RunE:  publishPlanCmd.RunE,
	}
	publishAlias.Flags().String("summary", "", "Publish plan summary")
	publishAlias.Flags().Bool("always-review", false, "Require human review before submit")

	monitorCmd := &cobra.Command{
		Use:   "monitor",
		Short: "MonitorAgent: login/error health check",
		RunE: func(cmd *cobra.Command, args []string) error {
			loginState, _ := cmd.Flags().GetString("login-state")
			lastErr, _ := cmd.Flags().GetString("error")
			ac := &agents.Context{
				TaskID: "cli-monitor", MaxSteps: 10, AllowedTools: agents.DefaultTools(), TraceID: trace.New(),
			}
			r, err := agents.MonitorAgent(context.Background(), ac, loginState, lastErr)
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(r, "", "  ")
			cmd.Println(string(b))
			return nil
		},
	}
	monitorCmd.Flags().String("login-state", "unknown", "Account login state")
	monitorCmd.Flags().String("error", "", "Last error string")

	analyticsCmd := &cobra.Command{
		Use:   "analytics",
		Short: "AnalyticsAgent: insights from metrics JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			metricsJSON, _ := cmd.Flags().GetString("metrics-json")
			if metricsJSON == "" {
				metricsJSON = `{"views":0,"likes":0}`
			}
			ac := &agents.Context{
				TaskID: "cli-analytics", MaxSteps: 10, AllowedTools: agents.DefaultTools(), TraceID: trace.New(),
			}
			r, err := agents.AnalyticsAgent(context.Background(), ac, metricsJSON)
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(r, "", "  ")
			cmd.Println(string(b))
			return nil
		},
	}
	analyticsCmd.Flags().String("metrics-json", "", "Metrics JSON blob")

	// Tool Agent — execute full tools.Registry plans
	toolRunCmd := &cobra.Command{
		Use:   "tools-run",
		Short: "ToolAgent: run multi-step tools.Registry plan (JSON)",
		Long: `Execute a Tool Agent plan:

  alpha-nexus-egolite agent tools-run --json '{
    "goal":"daily ops",
    "stop_on_error":true,
    "max_retries":2,
    "steps":[
      {"tool":"account.list","args":{}},
      {"tool":"ego.space.list","args":{}},
      {"tool":"tikhub.config.status","args":{}}
    ]
  }'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := cmd.Flags().GetString("json")
			file, _ := cmd.Flags().GetString("file")
			if file != "" {
				b, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				raw = string(b)
			}
			if raw == "" {
				return fmt.Errorf("--json or --file required")
			}
			in, err := agents.ParseToolAgentJSON([]byte(raw))
			if err != nil {
				return err
			}
			// inject runtime from active config
			cfg := activeCfg
			if cfg.DataDir == "" {
				cfg = config.DefaultConfig()
			}
			in.Runtime = egoToolsRuntime(cfg)
			ac := &agents.Context{
				TaskID: "cli-tool-agent", MaxSteps: 100, TraceID: trace.New(),
			}
			rep, err := agents.ToolAgent(context.Background(), ac, in)
			if rep != nil {
				b, _ := json.MarshalIndent(rep, "", "  ")
				cmd.Println(string(b))
			}
			if err != nil {
				return err
			}
			if rep != nil && !rep.OK {
				return fmt.Errorf("%s", rep.Message)
			}
			return nil
		},
	}
	toolRunCmd.Flags().String("json", "", "ToolAgent plan JSON")
	toolRunCmd.Flags().String("file", "", "Path to plan JSON file")

	// ---- multi-scheme hub (UI sub-buttons) ----
	schemesCmd := &cobra.Command{
		Use:   "schemes",
		Short: "List agent integration schemes (native / llm_tools / opencode / mcp_export)",
		RunE: func(cmd *cobra.Command, args []string) error {
			list := agents.ListSchemes()
			b, _ := json.MarshalIndent(list, "", "  ")
			cmd.Println(string(b))
			return nil
		},
	}

	schemeRunCmd := &cobra.Command{
		Use:   "scheme",
		Short: "Run one agent scheme by id (smoke test entry for UI buttons)",
		Long: `Run an agent integration scheme:

  alpha-nexus-egolite agent schemes
  alpha-nexus-egolite agent scheme --id native --dry-run
  alpha-nexus-egolite agent scheme --id llm_tools --goal "list accounts"
  alpha-nexus-egolite agent scheme --id opencode --dry-run
  alpha-nexus-egolite agent scheme --id mcp_export
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetString("id")
			goal, _ := cmd.Flags().GetString("goal")
			dry, _ := cmd.Flags().GetBool("dry-run")
			live, _ := cmd.Flags().GetBool("live")
			maxSteps, _ := cmd.Flags().GetInt("max-steps")
			model, _ := cmd.Flags().GetString("model")
			if id == "" {
				return fmt.Errorf("--id required (native|llm_tools|opencode|mcp_export)")
			}
			if live {
				dry = false
			}
			cfg := activeCfg
			if cfg.DataDir == "" {
				cfg = config.DefaultConfig()
			}
			dir, _ := cfg.ResolvedDataDir()
			req := agents.SchemeRunRequest{
				Scheme:   agents.SchemeID(id),
				Goal:     goal,
				DryRun:   dry,
				MaxSteps: maxSteps,
				Model:    model,
				DataDir:  dir,
				Runtime:  egoToolsRuntime(cfg),
			}
			res, err := agents.RunScheme(context.Background(), req)
			if res != nil {
				b, _ := json.MarshalIndent(res, "", "  ")
				cmd.Println(string(b))
			}
			if err != nil && (res == nil || !res.OK) {
				return err
			}
			if res != nil && !res.OK {
				return fmt.Errorf("%s", firstNonEmptyStr(res.Error, res.Message))
			}
			return nil
		},
	}
	schemeRunCmd.Flags().String("id", "", "Scheme id")
	schemeRunCmd.Flags().String("goal", "", "Natural language / URL goal")
	schemeRunCmd.Flags().Bool("dry-run", true, "Safe mode (default true)")
	schemeRunCmd.Flags().Bool("live", false, "Disable dry-run and execute for real")
	schemeRunCmd.Flags().Int("max-steps", 6, "Max steps for agent loops")
	schemeRunCmd.Flags().String("model", "", "Model override (llm_tools / opencode)")
	_ = schemeRunCmd.MarkFlagRequired("id")

	mcpServeCmd := &cobra.Command{
		Use:   "mcp-serve",
		Short: "MCP stdio server (TurboPush-style tools + tools.Registry) for OpenCode / Claude",
		Long: `Alpha-Nexus MCP server — architecture aligned with xueyc1f/turbopush-mcp
(mark3labs/mcp-go + stdio), but local SQLite control plane instead of HTTP to TurboPush.

Friendly tools:
  list_platforms, list_accounts, list_logged_accounts
  list_articles, get_article, create_article, create_graph_text, create_video
  publish_article|graph_text|video (declared platform paths; dry_run default)
  list_records, get_record_info
  tools_list, tools_call, agent_scheme_run

Register:
  claude mcp add alpha-nexus -- alpha-nexus-egolite agent mcp-serve
  opencode mcp add alpha-nexus -- alpha-nexus-egolite agent mcp-serve

Env:
  ALPHA_NEXUS_DATA_DIR  override data dir (default ~/.alpha-nexus-egolite)
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := activeCfg
			if cfg.DataDir == "" {
				cfg = config.DefaultConfig()
			}
			// env override like TurboPush's TURBO_PUSH_*
			if v := os.Getenv("ALPHA_NEXUS_DATA_DIR"); v != "" {
				cfg.DataDir = v
			}
			rt := egoToolsRuntime(cfg)
			return agents.ServeMCPStdio(context.Background(), rt)
		},
	}
	mcpBrokerCmd := &cobra.Command{
		Use:    "mcp-broker",
		Short:  "Private per-run Agent Tool Broker MCP",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			endpoint, _ := cmd.Flags().GetString("endpoint")
			runID, _ := cmd.Flags().GetString("run-id")
			return mcp.ServeBrokerStdio(cmd.Context(), mcp.BrokerConfig{
				Endpoint: endpoint,
				RunID:    runID,
				Token:    os.Getenv("ALPHA_NEXUS_AGENT_RUN_TOKEN"),
			})
		},
	}
	mcpBrokerCmd.Flags().String("endpoint", "", "Loopback Agent broker endpoint")
	mcpBrokerCmd.Flags().String("run-id", "", "Current Agent run")
	_ = mcpBrokerCmd.MarkFlagRequired("endpoint")
	_ = mcpBrokerCmd.MarkFlagRequired("run-id")

	// workspace sandbox
	wsCmd := &cobra.Command{Use: "workspace", Short: "Agent sandbox (~/.alpha-nexus/agent-workspace)"}
	wsPathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print workspace path (create if missing)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := activeCfg
			if cfg.DataDir == "" {
				cfg = config.DefaultConfig()
			}
			dir, _ := cfg.ResolvedDataDir()
			p, err := agentws.Ensure(dir)
			if err != nil {
				return err
			}
			cmd.Println(p)
			cmd.Printf("automations: %s\n", agentws.AutomationsPath(dir))
			return nil
		},
	}
	wsCmd.AddCommand(wsPathCmd)

	// chat (sandbox-only)
	chatCmd := &cobra.Command{
		Use:   "chat",
		Short: "Workspace agent chat (sandbox only; MCP/tools; no app source)",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, _ := cmd.Flags().GetString("engine")
			msg, _ := cmd.Flags().GetString("message")
			cont, _ := cmd.Flags().GetBool("continue")
			model, _ := cmd.Flags().GetString("model")
			effort, _ := cmd.Flags().GetString("effort")
			thinking, _ := cmd.Flags().GetBool("thinking")
			if msg == "" && len(args) > 0 {
				msg = strings.Join(args, " ")
			}
			if msg == "" {
				return fmt.Errorf("--message required")
			}
			cfg := activeCfg
			if cfg.DataDir == "" {
				cfg = config.DefaultConfig()
			}
			dir, _ := cfg.ResolvedDataDir()
			res, err := terminal.Chat(context.Background(), terminal.ChatRequest{
				Engine: engine, Message: msg, DataDir: dir, Continue: cont,
				Model: model, Effort: effort, Thinking: thinking,
			})
			if res != nil {
				b, _ := json.MarshalIndent(res, "", "  ")
				cmd.Println(string(b))
			}
			if err != nil && (res == nil || !res.OK) {
				return err
			}
			return nil
		},
	}
	chatCmd.Flags().String("engine", "claude", "claude|opencode")
	chatCmd.Flags().String("message", "", "User message")
	chatCmd.Flags().Bool("continue", false, "Continue last session")
	chatCmd.Flags().String("model", "", "Model (claude: sonnet|opus|haiku; opencode: provider/model)")
	chatCmd.Flags().String("effort", "", "Thinking effort: low|medium|high|max")
	chatCmd.Flags().Bool("thinking", false, "Show thinking blocks (opencode)")

	agentCmd.AddCommand(contentCmd, adaptCmd, publishPlanCmd, publishAlias, monitorCmd, analyticsCmd, toolRunCmd, schemesCmd, schemeRunCmd, mcpServeCmd, mcpBrokerCmd, wsCmd, chatCmd)
	root.AddCommand(agentCmd)
}

func firstNonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- metrics ----

func addMetricsCmds(root *cobra.Command) {
	metricsCmd := &cobra.Command{Use: "metrics", Short: "Post metrics collect/list"}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List collected post metrics",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := openQuickDB()
			defer d.Close()
			items, err := d.ListPostMetrics(50)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "ID\tRECORD\tVIEWS\tLIKES\tCOLLECTED")
			for _, m := range items {
				fmt.Fprintf(w, "%d\t%d\t%d\t%d\t%s\n", m.ID, m.PublishRecordID, m.Views, m.Likes, m.CollectedAt)
			}
			w.Flush()
			if len(items) == 0 {
				cmd.Println("No metrics.")
			}
			return nil
		},
	}

	collectCmd := &cobra.Command{
		Use:   "collect",
		Short: "Record metrics for a publish record",
		RunE: func(cmd *cobra.Command, args []string) error {
			recordID, _ := cmd.Flags().GetInt64("record")
			views, _ := cmd.Flags().GetInt64("views")
			likes, _ := cmd.Flags().GetInt64("likes")
			if recordID == 0 {
				return fmt.Errorf("--record is required")
			}
			d := openQuickDB()
			defer d.Close()
			m, err := d.CreatePostMetric(recordID, 0, "", 0, int(views), int(likes), 0, 0, 0, "", "")
			if err != nil {
				return err
			}
			cmd.Printf("Metric #%d record=%d views=%d likes=%d\n", m.ID, m.PublishRecordID, m.Views, m.Likes)
			_, _ = d.CreateAuditEvent(string(trace.New()), "info", "metrics", "collect", "post_metrics", m.ID, "")
			return nil
		},
	}
	collectCmd.Flags().Int64("record", 0, "Publish record ID")
	collectCmd.Flags().Int64("views", 0, "View count")
	collectCmd.Flags().Int64("likes", 0, "Like count")
	_ = collectCmd.MarkFlagRequired("record")

	metricsCmd.AddCommand(listCmd, collectCmd)
	root.AddCommand(metricsCmd)
}

// ---- audit ----

func addAuditCmds(root *cobra.Command) {
	auditCmd := &cobra.Command{Use: "audit", Short: "Audit event log"}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent audit events",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := openQuickDB()
			defer d.Close()
			items, err := d.ListAuditEvents(50)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "ID\tLEVEL\tCATEGORY\tMESSAGE\tRELATED\tAT")
			for _, e := range items {
				msg := e.Message
				if len(msg) > 40 {
					msg = msg[:37] + "..."
				}
				rel := e.RelatedType
				if e.RelatedID > 0 {
					rel = fmt.Sprintf("%s:%d", e.RelatedType, e.RelatedID)
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", e.ID, e.Level, e.Category, msg, rel, e.CreatedAt)
			}
			w.Flush()
			if len(items) == 0 {
				cmd.Println("No audit events.")
			}
			return nil
		},
	}
	auditCmd.AddCommand(listCmd)
	root.AddCommand(auditCmd)
}

// ---- export ----

func addExportCmds(root *cobra.Command) {
	exportCmd := &cobra.Command{Use: "export", Short: "Export local data snapshots"}
	dataCmd := &cobra.Command{
		Use:   "data",
		Short: "Write export summary JSON (and note app.db path)",
		RunE: func(cmd *cobra.Command, args []string) error {
			outDir, _ := cmd.Flags().GetString("out")
			if outDir == "" {
				return fmt.Errorf("--out is required")
			}
			if err := os.MkdirAll(outDir, 0755); err != nil {
				return err
			}
			d := openQuickDB()
			defer d.Close()

			tables := []string{
				"accounts", "contents", "media_library", "publish_jobs",
				"publish_records", "task_queue",
				"post_metrics", "audit_events", "batch_runs", "content_versions",
			}
			counts := map[string]int64{}
			for _, t := range tables {
				n, err := d.CountRows(t)
				if err != nil {
					counts[t] = -1
					continue
				}
				counts[t] = n
			}
			summary := map[string]interface{}{
				"exported_at": time.Now().UTC().Format(time.RFC3339),
				"app_db":      filepath.Join(d.DataDir, "app.db"),
				"data_dir":    d.DataDir,
				"counts":      counts,
				"note":        "Copy app.db manually for full backup; this export is a counts summary.",
			}
			b, err := json.MarshalIndent(summary, "", "  ")
			if err != nil {
				return err
			}
			outPath := filepath.Join(outDir, "export_summary.json")
			if err := os.WriteFile(outPath, b, 0644); err != nil {
				return err
			}
			// Optional: also write a pointer note
			notePath := filepath.Join(outDir, "APP_DB_PATH.txt")
			_ = os.WriteFile(notePath, []byte(filepath.Join(d.DataDir, "app.db")+"\n"), 0644)

			cmd.Printf("Export written: %s\n", outPath)
			cmd.Printf("app.db path: %s\n", filepath.Join(d.DataDir, "app.db"))
			return nil
		},
	}
	dataCmd.Flags().String("out", "", "Output directory")
	_ = dataCmd.MarkFlagRequired("out")

	exportCmd.AddCommand(dataCmd)
	root.AddCommand(exportCmd)
}

// ---- credential (API platforms: youtube/tiktok/x) ----

func addCredentialCmds(root *cobra.Command) {
	credCmd := &cobra.Command{Use: "credential", Short: "Store API credential references (ref only, never tokens)"}

	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Store a credential reference for an account/platform",
		Long: `Stores only a reference key (vault/keychain pointer), never the secret itself.

Example:
  alpha-nexus credential set --platform youtube --account-id 1 --ref-key my-token-ref`,
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, _ := cmd.Flags().GetString("platform")
			accountID, _ := cmd.Flags().GetInt64("account-id")
			refKey, _ := cmd.Flags().GetString("ref-key")
			kind, _ := cmd.Flags().GetString("kind")
			if platform == "" {
				return fmt.Errorf("--platform is required")
			}
			if accountID == 0 {
				return fmt.Errorf("--account-id is required")
			}
			if strings.TrimSpace(refKey) == "" {
				return fmt.Errorf("--ref-key is required")
			}
			if kind == "" {
				kind = "oauth_token_ref"
			}
			if _, err := platforms.Find(platform); err != nil {
				return err
			}
			d := openQuickDB()
			defer d.Close()
			if _, err := d.GetAccount(accountID); err != nil {
				return err
			}
			c, err := d.CreateCredential(accountID, platform, kind, refKey, "")
			if err != nil {
				return err
			}
			cmd.Printf("Credential stored: #%d platform=%s account=%d ref_key=%s kind=%s\n",
				c.ID, c.PlatformCode, c.AccountID, c.RefKey, c.Kind)
			_, _ = d.CreateAuditEvent(string(trace.New()), "info", "credential", "set", "credentials", c.ID, platform)
			return nil
		},
	}
	setCmd.Flags().String("platform", "", "Platform code (youtube|tiktok|x|...)")
	setCmd.Flags().Int64("account-id", 0, "Account ID")
	setCmd.Flags().String("ref-key", "", "Credential reference key (not the secret)")
	setCmd.Flags().String("kind", "oauth_token_ref", "Credential kind")
	_ = setCmd.MarkFlagRequired("platform")
	_ = setCmd.MarkFlagRequired("account-id")
	_ = setCmd.MarkFlagRequired("ref-key")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List stored credential references",
		RunE: func(cmd *cobra.Command, args []string) error {
			d := openQuickDB()
			defer d.Close()
			items, err := d.ListCredentials()
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "ID\tACCOUNT\tPLATFORM\tKIND\tREF_KEY\tCREATED")
			for _, c := range items {
				fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\t%s\n",
					c.ID, c.AccountID, c.PlatformCode, c.Kind, c.RefKey, c.CreatedAt)
			}
			w.Flush()
			if len(items) == 0 {
				cmd.Println("No credentials.")
			}
			return nil
		},
	}

	credCmd.AddCommand(setCmd, listCmd)
	root.AddCommand(credCmd)
}

// publishQueuePayload is the JSON shape for kind=publish queue tasks.
type publishQueuePayload struct {
	ExistingJobID  int64  `json:"existing_job_id,omitempty"`
	Platform       string `json:"platform"`
	ContentID      int64  `json:"content_id"`
	TargetID       int64  `json:"target_id"`
	DryRun         bool   `json:"dry_run"`
	AutoSubmit     bool   `json:"auto_submit"`
	AccountID      int64  `json:"account_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	// CredentialRef optional override; otherwise looked up from credentials table.
	CredentialRef string `json:"credential_ref,omitempty"`
}

func handleQueueTask(cmd *cobra.Command, d *db.DB, t *db.TaskQueueItem) error {
	switch strings.ToLower(strings.TrimSpace(t.Kind)) {
	case "publish":
		return handlePublishQueueTask(cmd, d, t)
	default:
		return fmt.Errorf("unsupported queue kind %q", t.Kind)
	}
}

func handlePublishQueueTask(cmd *cobra.Command, d *db.DB, t *db.TaskQueueItem) error {
	var p publishQueuePayload
	raw := strings.TrimSpace(t.PayloadJSON)
	if raw == "" {
		raw = "{}"
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return fmt.Errorf("invalid publish payload: %w", err)
	}
	var job *db.PublishJob
	var content *db.Content
	var asset *db.AccountAsset
	var mediaPath, publishAt, complianceJSON string
	var publishMedia []platforms.PublishMedia
	var err error
	frozenInput := false
	newIdempotencyKey := ""
	if p.ExistingJobID > 0 {
		job, err = d.GetPublishJob(p.ExistingJobID)
		if err != nil {
			return err
		}
		if job.Status == "cancelled" {
			return nil
		}
		if job.Status != "pending" {
			return fmt.Errorf("publish job %d is not pending", job.ID)
		}
		p.Platform = job.PlatformCode
		p.ContentID = job.ContentID
		p.TargetID = job.AssetID
		p.DryRun, p.AutoSubmit = publish.IntentFromMode(job.Mode)
		if job.PlanID > 0 {
			frozen, frozenErr := (&publishplan.Service{DB: d}).FrozenInputForJob(job)
			if frozenErr != nil {
				return publishguard.FailPendingJob(d, job, "frozen_snapshot_preflight", t.TraceID, frozenErr, false)
			}
			content, asset = frozen.Content, frozen.Asset
			mediaPath, publishMedia, publishAt, complianceJSON = frozen.MediaPath, frozen.Media, frozen.PublishAt, frozen.ComplianceJSON
			frozenInput = true
		} else {
			content, err = d.GetContent(job.ContentID)
			if err != nil {
				return err
			}
			if job.AssetID > 0 {
				asset, err = d.GetAccountAsset(job.AssetID)
			} else {
				assets, listErr := d.ListAccountAssetsByAccount(job.AccountID)
				if listErr != nil {
					return listErr
				}
				for index := range assets {
					if assets[index].Enabled && assets[index].PlatformCode == job.PlatformCode {
						asset = &assets[index]
						break
					}
				}
				if asset == nil {
					return fmt.Errorf("publish job account asset not found")
				}
			}
			if err != nil {
				return err
			}
		}
	} else {
		if p.Platform == "" {
			return fmt.Errorf("publish payload: platform required")
		}
		if p.ContentID == 0 {
			return fmt.Errorf("publish payload: content_id required")
		}
		if p.TargetID == 0 {
			return fmt.Errorf("publish payload: target_id required")
		}
		content, err = d.GetContent(p.ContentID)
		if err != nil {
			return err
		}
		asset, err = d.GetAccountAsset(p.TargetID)
		if err != nil {
			return err
		}
		newIdempotencyKey = strings.TrimSpace(p.IdempotencyKey)
		if newIdempotencyKey == "" {
			newIdempotencyKey = fmt.Sprintf("queue:%012d", t.ID)
		}
	}
	failPreflight := func(step string, failure error, retryable bool) error {
		if job != nil {
			return publishguard.FailPendingJob(d, job, step, t.TraceID, failure, retryable)
		}
		return failure
	}
	if !p.DryRun {
		return failPreflight("ego_publish_plan_required", fmt.Errorf("PUBLISH_EGOLITE_PLAN_REQUIRED: live queue execution is disabled; use publish batch with a frozen EgoLite plan"), false)
	}
	if !platforms.CanPublish(p.Platform) {
		return failPreflight("platform_preflight", fmt.Errorf("platform %q does not support publishing", p.Platform), false)
	}
	if p.AutoSubmit && !platforms.Readiness(p.Platform).CanAttemptAutoSubmit {
		return failPreflight("platform_preflight", fmt.Errorf("AUTO_SUBMIT_NOT_SUPPORTED: platform %q requires its declared manual/API flow", p.Platform), false)
	}
	if p.AutoSubmit && (job == nil || job.PlanID <= 0 || job.PlanTargetID <= 0 || len(job.SnapshotHash) != 64) {
		return failPreflight("frozen_snapshot_preflight", fmt.Errorf("PUBLISH_FROZEN_SNAPSHOT_REQUIRED: real submit must use one approved single-target publish plan"), false)
	}
	if targetErr := publishguard.ValidateTargetOwnership(d, asset, p.Platform); targetErr != nil {
		return failPreflight("target_preflight", targetErr, false)
	}
	if p.AccountID > 0 && p.AccountID != asset.AccountID {
		return failPreflight("target_preflight", publishguard.ErrBrowserAccountMismatch, false)
	}

	if !frozenInput {
		mediaList, mediaErr := d.ListMediaByContent(content.ID)
		mediaPath, publishMedia, mediaErr = publishguard.PublishMediaPayload(mediaList, mediaErr, content.Type)
		if mediaErr != nil {
			return failPreflight("media_preflight", mediaErr, false)
		}
	}

	if job == nil {
		settingsJSON, settingsErr := publish.DefaultSettingsSnapshot(p.Platform)
		if settingsErr != nil {
			return settingsErr
		}
		job, err = d.CreatePublishJobWithIntent(
			0, content.ID, asset.AccountID, asset.ID, p.Platform, content.Type, newIdempotencyKey,
			publish.ExecutionMode(p.DryRun, p.AutoSubmit), settingsJSON,
		)
		if err != nil {
			return err
		}
	}

	cmd.Printf("  publish job #%d platform=%s content=%d target=%d dry_run=%v\n",
		job.ID, p.Platform, content.ID, asset.ID, p.DryRun)

	credRef := strings.TrimSpace(p.CredentialRef)
	if credRef == "" {
		if ref, refErr := d.GetCredentialRef(asset.AccountID, p.Platform); refErr == nil {
			credRef = ref
		}
	}

	runner := &publish.Runner{Store: d}
	result, runErr := runner.Run(context.Background(), publish.RunInput{
		Job:            job,
		Content:        content,
		Asset:          asset,
		CDPURL:         "",
		DryRun:         p.DryRun,
		AutoSubmit:     p.AutoSubmit,
		MediaPath:      mediaPath,
		Media:          publishMedia,
		EmitNDJSON:     true,
		CredentialRef:  credRef,
		PublishAt:      publishAt,
		ComplianceJSON: complianceJSON,
	})
	if result != nil {
		cmd.Printf("  result: %s step=%s\n", result.Status, result.Step)
		if result.Message != "" {
			cmd.Printf("  %s\n", result.Message)
		}
	}
	return runErr
}
