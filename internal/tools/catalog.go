package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/radar"
	"alpha-nexus-egolite/internal/tikhub"
)

// CreatorArchiveOptions controls one creator archival run. Both the CLI
// (`radar creator archive`) and the radar.creator.archive tool go through
// RunCreatorArchive so the two adapters share one progress and summary path.
type CreatorArchiveOptions struct {
	UserID string
	Name   string
	// Platform defaults to douyin; any other value is rejected before the
	// local database is opened.
	Platform  string
	MaxPages  int
	Workers   int
	WithMedia bool
	// MediaOnly skips the list fetch and only re-downloads missing covers/audio.
	MediaOnly  bool
	OnProgress func(stage string, done, total int)
}

// RunCreatorArchive archives one creator into dataDir, opening the local
// database so every page request is recorded like any other TikHub call.
func RunCreatorArchive(
	ctx context.Context,
	rt *Runtime,
	dataDir string,
	options CreatorArchiveOptions,
) (radar.CreatorSyncResult, error) {
	_ = rt
	platform := strings.TrimSpace(options.Platform)
	if platform == "" {
		platform = "douyin"
	}
	if platform != "douyin" {
		return radar.CreatorSyncResult{}, fmt.Errorf("creator archive currently supports douyin only, got %q", platform)
	}
	userID := strings.TrimSpace(options.UserID)
	if userID == "" {
		return radar.CreatorSyncResult{}, fmt.Errorf("user_id is required")
	}
	database, err := db.Open(dataDir)
	if err != nil {
		return radar.CreatorSyncResult{}, err
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		return radar.CreatorSyncResult{}, err
	}
	user := tikhub.WatchUser{Platform: platform, UserID: userID, Name: strings.TrimSpace(options.Name)}
	return radar.SyncCreatorAll(ctx, database, dataDir, user, radar.CreatorSyncOptions{
		MaxPages: options.MaxPages, Media: options.WithMedia, Workers: options.Workers,
		MediaOnly: options.MediaOnly, OnProgress: options.OnProgress,
	})
}

// CreatorProgressLine is one NDJSON progress record for a long-running creator
// archive (project convention: long tasks stream NDJSON).
type CreatorProgressLine struct {
	Type  string `json:"type"`
	Phase string `json:"phase"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

// CreatorProgressWriter serialises creator archive progress as NDJSON and drops
// repeated (phase, done) pairs so a long pull does not flood the stream. It is
// safe for concurrent use because media downloads may report from workers.
type CreatorProgressWriter struct {
	mu   sync.Mutex
	enc  *json.Encoder
	seen map[string]bool
}

// NewCreatorProgressWriter streams progress lines to w.
func NewCreatorProgressWriter(w io.Writer) *CreatorProgressWriter {
	return &CreatorProgressWriter{enc: json.NewEncoder(w), seen: map[string]bool{}}
}

// OnProgress matches radar.CreatorSyncOptions.OnProgress.
func (p *CreatorProgressWriter) OnProgress(stage string, done, total int) {
	if p == nil || p.enc == nil {
		return
	}
	key := stage + ":" + strconv.Itoa(done)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.seen[key] {
		return
	}
	p.seen[key] = true
	_ = p.enc.Encode(CreatorProgressLine{Type: "progress", Phase: stage, Done: done, Total: total})
}

// archiveCreator keeps the tool-facing signature and output contract stable.
// A nil onProgress reproduces the previous one-shot JSON result exactly.
func archiveCreator(
	ctx context.Context,
	rt *Runtime,
	dataDir, userID, name string,
	maxPages, workers int,
	withMedia, mediaOnly bool,
	onProgress func(stage string, done, total int),
) (map[string]interface{}, error) {
	summary, err := RunCreatorArchive(ctx, rt, dataDir, CreatorArchiveOptions{
		UserID: userID, Name: name, Platform: "douyin",
		MaxPages: maxPages, Workers: workers, WithMedia: withMedia, MediaOnly: mediaOnly,
		OnProgress: onProgress,
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"folder":     summary.Folder,
		"index_path": summary.IndexPath,
		"pages":      summary.Pages,
		"posts":      summary.Posts,
		"fetched":    summary.Fetched,
		"new_posts":  summary.NewPosts,
		"stopped":    summary.Stopped,
		"media": map[string]interface{}{
			"covers":  summary.Media.Covers,
			"audio":   summary.Media.Audio,
			"skipped": summary.Media.Skipped,
			"failed":  summary.Media.Failed,
			"total":   summary.Media.Total,
		},
		"call_ids": summary.CallIDs,
	}, nil
}

// radarSearchEndpoint maps a platform code to its registered keyword-search
// endpoint so scheduled jobs always reference a real catalog entry. The set must
// stay aligned with radar.allowedScheduleEndpoints, otherwise callers get a
// confusing "not enabled for automated schedules" error after a successful map.
func radarSearchEndpoint(platform string) (string, bool) {
	switch platform {
	case "douyin":
		return "douyin.video_search", true
	case "xhs":
		return "xhs.note_search", true
	case "youtube":
		return "youtube.video_search", true
	case "kuaishou":
		return "kuaishou.search", true
	case "wechat_mp":
		return "wechat_mp.keyword_search", true
	case "x":
		return "x.keyword_search", true
	default:
		return "", false
	}
}

// RegisterCatalog fills Default with the full application tool surface.
// Tool Agent / workflows call these names; CLI is only one adapter.
func RegisterCatalog() {
	r := Default
	registerSocialSkills(r)
	// ---- meta ----
	r.Register(Def{
		Name: "tools.list", Category: "meta", SideEffect: "read",
		Description: "List all registered tools (for Tool Agent discovery)",
		Params:      []Param{{Name: "category", Type: "string", Description: "Filter by category"}},
		Handler: func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
			cat := argString(args, "category", "")
			list := r.ListByCategory(cat)
			names := make([]string, 0, len(list))
			for _, d := range list {
				names = append(names, d.Name)
			}
			return okResult("ok", map[string]interface{}{"count": len(list), "tools": names, "category": cat}), nil
		},
	})

	registerEgoTools(r)
	registerKnowledgeTools(r)
	registerDataLinkTools(r)
	registerShopListingTools(r)
	registerStudioScheduleHandoffTools(r)
	registerTranscriptTools(r)
	registerCopyTools(r)
	registerContentStageTools(r)

	// ---- account ----
	r.Register(Def{
		Name: "account.add", Category: "account", SideEffect: "write",
		Description: "Add one platform account under a logical Ego identity",
		Params: []Param{
			{Name: "identity_id", Type: "int", Description: "Logical identity id; defaults to the seeded identity"},
			{Name: "platform", Type: "string", Required: true},
			{Name: "name", Type: "string", Required: true},
			{Name: "group", Type: "string"},
		},
		Handler: handleAccountAdd,
	})
	r.Register(Def{
		Name: "account.list", Category: "account", SideEffect: "read",
		Description: "List accounts (optional platform/group/tag union filter)",
		Params: []Param{
			{Name: "platform", Type: "string"},
			{Name: "group", Type: "string"},
			{Name: "tag_ids", Type: "array"},
		},
		Handler: handleAccountList,
	})
	r.Register(Def{
		Name: "account.tag.list", Category: "account", SideEffect: "read",
		Description: "List normalized account tags and account counts",
		Handler:     handleAccountTagList,
	})
	r.Register(Def{
		Name: "account.tag.create", Category: "account", SideEffect: "write",
		Description: "Create or recolor one local account tag",
		Params: []Param{
			{Name: "name", Type: "string", Required: true},
			{Name: "color", Type: "string", Required: true, Description: "#RRGGBB"},
		},
		Handler: handleAccountTagCreate,
	})
	r.Register(Def{
		Name: "account.tag.update", Category: "account", SideEffect: "write",
		Description: "Transactionally append, remove, or replace tags for 1-100 accounts",
		Params: []Param{
			{Name: "account_ids", Type: "array", Required: true},
			{Name: "tag_ids", Type: "array", Required: true},
			{Name: "mode", Type: "string", Required: true, Description: "append|remove|replace"},
		},
		Handler: handleAccountTagUpdate,
	})
	r.Register(Def{
		Name: "account.tag.reorder", Category: "account", SideEffect: "write",
		Description: "Persist the complete preferred display order for account tags",
		Params: []Param{
			{Name: "tag_ids", Type: "array", Required: true, Description: "Every current tag id in preferred order"},
		},
		Handler: handleAccountTagReorder,
	})
	r.Register(Def{
		Name: "account.tag.resolve", Category: "account", SideEffect: "read",
		Description: "Resolve the deterministic union of accounts matching selected tags",
		Params: []Param{
			{Name: "tag_ids", Type: "array", Required: true},
			{Name: "platform", Type: "string"},
		},
		Handler: handleAccountTagResolve,
	})

	// ---- publish / workflow ----
	r.Register(Def{
		Name: "workflow.run", Category: "workflow", SideEffect: "write",
		Description: "Run ordered tool steps (Tool Agent primitive). steps=[{tool,args}]",
		Params: []Param{
			{Name: "steps", Type: "array", Required: true, Description: "[{tool,args}]"},
			{Name: "stop_on_error", Type: "bool", Description: "default true"},
		},
		Handler: handleWorkflowRun,
	})
	r.Register(Def{
		Name: "publish.run", Category: "publish", SideEffect: "external_publish",
		Description: "Run the legacy local publish contract in dry-run mode only; live publishing requires an EgoLite frozen plan",
		Params: []Param{
			{Name: "platform", Type: "string", Required: true},
			{Name: "content_id", Type: "int", Required: true},
			{Name: "target_id", Type: "int", Required: true},
			{Name: "template_id", Type: "int", Description: "Optional enabled template for the same platform"},
			{Name: "settings_json", Type: "string", Description: "Optional one-request settings for a platform with a declared schema"},
			{Name: "idempotency_key", Type: "string", Description: "Optional stable key for dry-run replay"},
			{Name: "dry_run", Type: "bool", Description: "default true"},
			{Name: "auto_submit", Type: "bool", Description: "Legacy flag; always rejected"},
			{Name: "confirm", Type: "bool", Description: "Legacy live-mode confirmation; live mode remains rejected"},
		},
		Handler: handlePublishRun,
	})
	r.Register(Def{
		Name: "content.variant.upsert", Category: "content", SideEffect: "write",
		Description: "Create or update one editable platform/account content variant",
		Params: []Param{
			{Name: "content_id", Type: "int", Required: true},
			{Name: "platform", Type: "string", Required: true},
			{Name: "account_id", Type: "int"},
			{Name: "title", Type: "string"}, {Name: "caption", Type: "string"},
			{Name: "body", Type: "string"}, {Name: "tags_json", Type: "string"},
		},
		Handler: handleContentVariantUpsert,
	})
	r.Register(Def{
		Name: "publish.plan.freeze", Category: "publish", SideEffect: "write",
		Description: "Validate and freeze an immutable reviewed-platform publish plan; never operates a browser",
		Params: []Param{
			{Name: "content_id", Type: "int", Required: true},
			{Name: "media_mode", Type: "string", Required: true, Description: "graph_text|video|article"},
			{Name: "media_ids", Type: "array", Required: true},
			{Name: "cover_media_id", Type: "int"},
			{Name: "native_video_cover", Type: "bool", Description: "Use the platform-native first video frame when no custom cover is selected"},
			{Name: "publish_at", Type: "string", Description: "Optional timezone-aware RFC3339 native platform publish time"},
			{Name: "compliance_policy", Type: "object", Description: "Optional immutable CompliancePolicy included in the snapshot hash"},
			{Name: "concurrency", Type: "int", Description: "Distinct account Spaces, bounded by local EgoLite calibration"},
			{Name: "targets", Type: "array", Required: true, Description: "[{account_id,variant_id,settings_json}]"},
		},
		Handler: handlePublishPlanFreeze,
	})
	r.Register(Def{
		Name: "publish.plan.get", Category: "publish", SideEffect: "read",
		Description: "Read one frozen publish plan and its immutable snapshot",
		Params:      []Param{{Name: "plan_id", Type: "int", Required: true}},
		Handler:     handlePublishPlanGet,
	})
	r.Register(Def{
		Name: "publish.plan.list", Category: "publish", SideEffect: "read",
		Description: "List redacted publish-plan summaries and current target states",
		Params:      []Param{{Name: "limit", Type: "int", Description: "1-100; default 10"}},
		Handler:     handlePublishPlanList,
	})
	r.Register(Def{
		Name: "publish.content.delete", Category: "publish", SideEffect: "external_delete",
		Description: "Permanently delete one independently recorded remote content item after explicit local confirmation",
		Params: []Param{
			{Name: "record_id", Type: "int", Required: true},
			{Name: "confirm_permanent_delete", Type: "bool", Required: true},
		},
		Handler: handlePublishContentDelete,
	})
	r.Register(Def{
		Name: "publish.plan.prepare", Category: "publish", SideEffect: "external_publish",
		Description: "Consume one local UI approval and open frozen targets in isolated ego Spaces; never clicks the final publish control",
		Params: []Param{
			{Name: "plan_id", Type: "int", Required: true},
			{Name: "snapshot_hash", Type: "string", Required: true},
			{Name: "approval_id", Type: "string", Required: true},
			{Name: "nonce", Type: "string", Required: true},
		},
		Handler: handlePublishPlanPrepare,
	})
	r.Register(Def{
		Name: "publish.plan.submit", Category: "publish", SideEffect: "external_publish",
		Description: "Consume one submit-bound local UI approval; unverified ego adapters fail closed before the final click",
		Params: []Param{
			{Name: "plan_id", Type: "int", Required: true},
			{Name: "snapshot_hash", Type: "string", Required: true},
			{Name: "approval_id", Type: "string", Required: true},
			{Name: "nonce", Type: "string", Required: true},
		},
		Handler: handlePublishPlanSubmit,
	})
	r.Register(Def{
		Name: "queue.enqueue", Category: "workflow", SideEffect: "write",
		Description: "Enqueue an immediately executable publish job",
		Params: []Param{
			{Name: "type", Type: "string", Required: true},
			{Name: "payload", Type: "object"},
			{Name: "priority", Type: "int", Description: "-100..100; default 0"},
		},
		Handler: handleQueueEnqueue,
	})
	// ---- media / content ----
	r.Register(Def{
		Name: "media.import", Category: "media", SideEffect: "write",
		Description: "Index one user-selected local media file or folder in the content library",
		Params: []Param{
			{Name: "path", Type: "string", Required: true, Description: "Exact local file or folder path supplied by the user in the current request"},
			{Name: "purpose", Type: "string", Description: "shared|video|graph_text|article"},
			{Name: "recursive", Type: "bool", Description: "Scan subfolders; default true"},
			{Name: "max_files", Type: "int", Description: "1-500; default 200"},
		},
		Handler: handleMediaImport,
	})
	r.Register(Def{
		Name: "media.generate", Category: "media", SideEffect: "network",
		Description: "Create one paid Agnes image or video generation job from an explicit trusted local UI action",
		Params: []Param{
			{Name: "request_id", Type: "string", Required: true, Description: "Unique caller nonce used for idempotent retries"},
			{Name: "kind", Type: "string", Required: true, Description: "image|video"},
			{Name: "mode", Type: "string", Description: "text_to_image|image_to_image|text_to_video|image_to_video; defaults by kind"},
			{Name: "model", Type: "string", Description: "Explicit supported image or video model"},
			{Name: "output_size", Type: "string", Description: "1:1|3:4|4:3|16:9|9:16|2:3|3:2|21:9"},
			{Name: "duration_seconds", Type: "int", Description: "Video only: 5|10; default 5"},
			{Name: "prompt", Type: "string", Required: true},
			{Name: "references", Type: "array", Description: "Managed asset_name and local role snapshots; empty for text modes"},
			{Name: "confirm", Type: "bool", Required: true, Description: "Must be true for the explicit UI click"},
		},
		Handler: handleMediaGenerate,
	})
	r.Register(Def{
		Name: "media.generate.batch", Category: "media", SideEffect: "network",
		Description: "Create 1-8 paid Agnes image or video generation jobs from one explicit trusted local UI action",
		Params: []Param{
			{Name: "request_id", Type: "string", Required: true, Description: "Unique batch nonce; each item receives a stable derived request ID"},
			{Name: "kind", Type: "string", Required: true, Description: "image|video"},
			{Name: "mode", Type: "string", Description: "text_to_image|image_to_image|text_to_video|image_to_video; defaults by kind"},
			{Name: "model", Type: "string", Description: "Explicit supported image or video model"},
			{Name: "output_size", Type: "string", Description: "1:1|3:4|4:3|16:9|9:16|2:3|3:2|21:9"},
			{Name: "duration_seconds", Type: "int", Description: "Video only: 5|10; default 5"},
			{Name: "prompt", Type: "string", Required: true},
			{Name: "references", Type: "array", Description: "Managed asset_name and local role snapshots; empty for text modes"},
			{Name: "count", Type: "int", Required: true, Description: "1-8 real provider jobs"},
			{Name: "confirm", Type: "bool", Required: true, Description: "Must be true for the explicit UI click"},
		},
		Handler: handleMediaGenerateBatch,
	})
	r.Register(Def{
		Name: "studio.media.list", Category: "studio", SideEffect: "read",
		Description: "List local Studio media indexes; no source files are copied or uploaded",
		Params:      []Param{{Name: "limit", Type: "int", Description: "1-200; default 200"}},
		Handler:     handleStudioMediaList,
	})
	r.Register(Def{
		Name: "studio.script.create", Category: "studio", SideEffect: "write",
		Description: "Save a local Studio script for a later clip plan",
		Params: []Param{
			{Name: "title", Type: "string", Required: true},
			{Name: "body", Type: "string", Required: true},
			{Name: "content_type", Type: "string", Description: "video|graph_text|article; default video"},
		},
		Handler: handleStudioScriptCreate,
	})
	r.Register(Def{
		Name: "studio.script.list", Category: "studio", SideEffect: "read",
		Description: "List local Studio scripts",
		Params:      []Param{{Name: "limit", Type: "int", Description: "1-200; default 200"}},
		Handler:     handleStudioScriptList,
	})
	r.Register(Def{
		Name: "studio.clip.plan", Category: "studio", SideEffect: "write",
		Description: "Ask the fixed Doubao Agent to create a reviewable clip plan; it does not create a video file",
		Params: []Param{
			{Name: "source_media_id", Type: "int", Required: true},
			{Name: "script_id", Type: "int", Required: true},
			{Name: "target_duration_ms", Type: "int", Description: "5000-180000; default 60000"},
		},
		Handler: handleStudioClipPlan,
	})
	r.Register(Def{
		Name: "studio.clip.execute", Category: "studio", SideEffect: "write",
		Description: "Execute a saved local clip plan with ffmpeg; requires confirm=true and never changes the source file",
		Params: []Param{
			{Name: "clip_job_id", Type: "int", Required: true},
			{Name: "confirm", Type: "bool", Required: true, Description: "Must be true before writing a local MP4 output"},
		},
		Handler: handleStudioClipExecute,
	})
	r.Register(Def{
		Name: "content.create", Category: "content", SideEffect: "write",
		Description: "Create a local article, graph-text, or video draft",
		Params: []Param{
			{Name: "type", Type: "string", Required: true},
			{Name: "title", Type: "string"},
			{Name: "caption", Type: "string"},
			{Name: "body", Type: "string"},
			{Name: "tags_json", Type: "string", Description: "JSON array; default []"},
			{Name: "status", Type: "string", Description: "draft|published|archived; default draft"},
		},
		Handler: handleContentCreate,
	})
	r.Register(Def{
		Name: "content.media.attach", Category: "content", SideEffect: "write",
		Description: "Attach one validated media-library item to a content draft with an explicit role and order",
		Params: []Param{
			{Name: "content_id", Type: "int", Required: true},
			{Name: "library_id", Type: "int", Required: true},
			{Name: "role", Type: "string", Required: true, Description: "content|cover"},
			{Name: "position", Type: "int", Required: true, Description: "Non-negative order within the role"},
		},
		Handler: handleContentMediaAttach,
	})

	// ---- tikhub (public ranks / watch / search) ----
	r.Register(Def{
		Name: "tikhub.config.status", Category: "tikhub", SideEffect: "read",
		Description: "Check whether TikHub API key is configured (no network)",
		Handler: func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
			dir := dataDir(rt)
			cfg, err := tikhub.LoadProviderConfig(dir)
			if err != nil {
				return &Result{OK: false, Error: "TIKHUB_PROVIDER_CONFIG_INVALID"}, err
			}
			configured := tikhub.ProviderConfigured(dir)
			return okResult("TikHub provider status", map[string]interface{}{
				"configured":  configured,
				"active":      configured && cfg.Active,
				"base_url":    cfg.BaseURL,
				"test_status": cfg.TestStatus,
				"tested_at":   cfg.TestedAt,
			}), nil
		},
	})
	r.Register(Def{
		Name: "tikhub.rank.daily", Category: "tikhub", SideEffect: "network",
		Description:    "Fetch Douyin daily top boards and save JSON+MD (uses free_credit when allowed)",
		FreeCreditHint: "≈0.001 USD free_credit per board; default 4 boards + hot_search",
		Params: []Param{
			{Name: "boards", Type: "string", Description: "Comma list: video,low_fan,high_like,high_fan"},
			{Name: "page_size", Type: "int"},
			{Name: "window", Type: "int", Description: "date_window hours"},
			{Name: "confirm", Type: "bool", Description: "Must be true to hit the paid network API"},
		},
		Handler: func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
			return runTikHubRankDaily(ctx, rt, args)
		},
	})
	r.Register(Def{
		Name: "tikhub.user.add", Category: "tikhub", SideEffect: "write",
		Description: "Add creator to watchlist (local only)",
		Params: []Param{
			{Name: "user_id", Type: "string", Required: true},
			{Name: "platform", Type: "string"},
			{Name: "name", Type: "string"},
			{Name: "note", Type: "string"},
		},
		Handler: func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
			dir := dataDir(rt)
			platform := strings.ToLower(strings.TrimSpace(argString(args, "platform", "xhs")))
			uid := strings.TrimSpace(argString(args, "user_id", ""))
			if uid == "" || (platform != "xhs" && platform != "douyin") {
				err := fmt.Errorf("platform must be xhs or douyin and user_id is required")
				return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
			}
			if err := tikhub.AddWatch(dir, platform, uid, strings.TrimSpace(argString(args, "name", "")), strings.TrimSpace(argString(args, "note", ""))); err != nil {
				return nil, err
			}
			return okResult("watch added", map[string]interface{}{"platform": platform, "user_id": uid}), nil
		},
	})
	r.Register(Def{
		Name: "tikhub.user.list", Category: "tikhub", SideEffect: "read",
		Description: "List local creator watchlist",
		Handler:     handleTikHubUserList,
	})
	r.Register(Def{
		Name: "tikhub.user.check_all", Category: "tikhub", SideEffect: "network",
		Description:    "Snapshot all watched creators via TikHub",
		FreeCreditHint: "charges free_credit per user info/notes call; require confirm=true",
		Params:         []Param{{Name: "confirm", Type: "bool", Description: "Must be true to hit network"}},
		Handler:        handleTikHubUserCheckAll,
	})
	r.Register(Def{
		Name: "tikhub.update", Category: "tikhub", SideEffect: "network",
		Description:    "Daily workflow: ranks + user watchlist check",
		FreeCreditHint: "uses free_credit; require confirm=true",
		Params: []Param{
			{Name: "skip_users", Type: "bool"},
			{Name: "boards", Type: "string"},
			{Name: "confirm", Type: "bool"},
		},
		Handler: func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
			if !argBool(args, "confirm", false) {
				return okResult("dry plan: tikhub.rank.daily + optional user.check_all", map[string]interface{}{
					"boards":     argString(args, "boards", "video,low_fan,high_like,high_fan"),
					"skip_users": argBool(args, "skip_users", false),
					"hint":       "pass confirm=true to execute network calls",
				}), nil
			}
			// Compose via workflow primitive so one audit trail
			steps := []interface{}{
				map[string]interface{}{
					"tool": "tikhub.rank.daily",
					"args": map[string]interface{}{
						"confirm": true,
						"boards":  argString(args, "boards", "video,low_fan,high_like,high_fan"),
					},
				},
			}
			if !argBool(args, "skip_users", false) {
				steps = append(steps, map[string]interface{}{
					"tool": "tikhub.user.check_all",
					"args": map[string]interface{}{"confirm": true},
				})
			}
			return handleWorkflowRun(ctx, rt, map[string]interface{}{
				"steps": steps, "stop_on_error": false,
			})
		},
	})
	r.Register(Def{
		Name: "tikhub.search.xhs", Category: "tikhub", SideEffect: "network",
		Description: "Search Xiaohongshu notes by keyword via TikHub",
		Params: []Param{
			{Name: "keyword", Type: "string", Required: true},
			{Name: "pages", Type: "int"},
		},
		Handler: planHandler("tikhub.search.xhs", "CLI: tikhub search xhs --keyword"),
	})

	r.Register(Def{
		Name: "radar.schedule.list", Category: "radar", SideEffect: "read",
		Description: "List Trend Radar scheduled fetch jobs with due state (no network)",
		Handler: func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
			file, err := radar.LoadSchedules(dataDir(rt))
			if err != nil {
				return &Result{OK: false, Error: "RADAR_SCHEDULE_READ_FAILED"}, err
			}
			return okResult("radar schedules", map[string]interface{}{
				"count":      len(file.Schedules),
				"schedules":  file.Schedules,
				"updated_at": file.UpdatedAt,
			}), nil
		},
	})
	r.Register(Def{
		Name: "radar.schedule.create", Category: "radar", SideEffect: "write",
		Description: "Create a Trend Radar scheduled fetch job (disabled by default; no network until enabled)",
		Params: []Param{
			{Name: "name", Type: "string", Required: true, Description: "Human readable schedule name"},
			{Name: "niche_id", Type: "string", Required: true, Description: "Target niche id from radar config"},
			{Name: "keyword", Type: "string", Required: true, Description: "Single search term"},
			{Name: "platform", Type: "string", Description: "douyin (default) or xhs"},
			{Name: "interval_minutes", Type: "int", Description: "Fetch cadence; minimum 360"},
			{Name: "enabled", Type: "bool", Description: "Enable immediately (still requires due time to elapse)"},
		},
		Handler: func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
			dir := dataDir(rt)
			platform := strings.ToLower(strings.TrimSpace(argString(args, "platform", "douyin")))
			endpoint, ok := radarSearchEndpoint(platform)
			if !ok {
				return &Result{OK: false, Error: "RADAR_PLATFORM_UNSUPPORTED"}, fmt.Errorf("platform %q has no radar search endpoint", platform)
			}
			interval := argInt(args, "interval_minutes", 360)
			now := time.Now().UTC().Format(time.RFC3339)
			// 只有抖音搜索端点接受 cursor/sort_type/publish_time；
			// 其他平台只传 keyword，其余参数交给 PrepareEndpointParams 填默认值。
			params := map[string]any{"keyword": strings.TrimSpace(argString(args, "keyword", ""))}
			if platform == "douyin" {
				params["cursor"] = "0"
				params["sort_type"] = "1"
				params["publish_time"] = "7"
			}
			schedule := radar.Schedule{
				ID:              radar.NewScheduleID(),
				Name:            strings.TrimSpace(argString(args, "name", "")),
				NicheID:         strings.TrimSpace(argString(args, "niche_id", "")),
				EndpointID:      endpoint,
				Params:          params,
				IntervalMinutes: interval,
				Enabled:         argBool(args, "enabled", false),
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			validated, err := radar.ValidateSchedule(schedule)
			if err != nil {
				return &Result{OK: false, Error: "RADAR_SCHEDULE_INVALID"}, err
			}
			if err := radar.UpdateSchedules(dir, func(file *radar.ScheduleFile) error {
				file.Schedules = append(file.Schedules, validated)
				return nil
			}); err != nil {
				return &Result{OK: false, Error: "RADAR_SCHEDULE_WRITE_FAILED"}, err
			}
			return okResult("radar schedule created", map[string]interface{}{
				"id": validated.ID, "enabled": validated.Enabled, "interval_minutes": validated.IntervalMinutes,
			}), nil
		},
	})
	r.Register(Def{
		Name: "radar.schedule.run_due", Category: "radar", SideEffect: "network",
		Description:    "Run one due pass of Trend Radar schedules (hits TikHub for every due job)",
		FreeCreditHint: "each due keyword costs one TikHub search call",
		Params:         []Param{{Name: "confirm", Type: "bool", Description: "Must be true to hit the paid network API"}},
		Handler: func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
			dir := dataDir(rt)
			if !argBool(args, "confirm", false) {
				file, err := radar.LoadSchedules(dir)
				if err != nil {
					return &Result{OK: false, Error: "RADAR_SCHEDULE_READ_FAILED"}, err
				}
				enabled := 0
				for _, item := range file.Schedules {
					if item.Enabled {
						enabled++
					}
				}
				return okResult("dry plan: radar.schedule.run_due", map[string]interface{}{
					"enabled_schedules": enabled,
					"hint":              "pass confirm=true to execute due fetches",
				}), nil
			}
			summary, err := radar.RunDue(ctx, dir, time.Now())
			if err != nil {
				return &Result{OK: false, Error: "RADAR_WORKER_FAILED"}, err
			}
			return okResult("radar due pass complete", map[string]interface{}{
				"due":       summary.Due,
				"succeeded": summary.Succeeded,
				"failed":    summary.Failed,
				"call_ids":  summary.CallIDs,
			}), nil
		},
	})

	r.Register(Def{
		Name: "radar.creator.archive", Category: "radar", SideEffect: "network",
		Description:    "Archive every post of a Douyin creator into its local folder, downloading covers and audio",
		FreeCreditHint: "one TikHub page call per 20 posts, plus free CDN downloads for covers and audio",
		Params: []Param{
			{Name: "user_id", Type: "string", Required: true, Description: "Douyin sec_user_id"},
			{Name: "name", Type: "string", Description: "Display name stored with the archive"},
			{Name: "max_pages", Type: "int", Description: "Page cap; 0 = until the platform ends the list"},
			{Name: "workers", Type: "int", Description: "Concurrent media downloads; default 8"},
			{Name: "skip_media", Type: "bool", Description: "Only write the manifest, download nothing"},
			{Name: "media_only", Type: "bool", Description: "Skip the list fetch; only re-download covers/audio missing from the existing archive"},
			{Name: "confirm", Type: "bool", Description: "Must be true to hit the paid network API"},
		},
		Handler: func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
			dir := dataDir(rt)
			userID := strings.TrimSpace(argString(args, "user_id", ""))
			if userID == "" {
				return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("user_id is required")
			}
			if !argBool(args, "confirm", false) {
				return okResult("dry plan: radar.creator.archive", map[string]interface{}{
					"user_id":    userID,
					"max_pages":  argInt(args, "max_pages", 0),
					"skip_media": argBool(args, "skip_media", false),
					"media_only": argBool(args, "media_only", false),
					"hint":       "pass confirm=true to fetch all pages and download covers + audio",
				}), nil
			}
			// 归档要跑几十秒。调用方（CLI）可以通过 context 装一个进度槽；
			// 没装就退回「只在结束时给一个结果」，行为与之前完全一致。
			onProgress, _ := ProgressFrom(ctx)
			summary, err := archiveCreator(ctx, rt, dir, userID, strings.TrimSpace(argString(args, "name", "")),
				argInt(args, "max_pages", 0), argInt(args, "workers", 0), !argBool(args, "skip_media", false),
				argBool(args, "media_only", false), onProgress)
			if err != nil {
				return &Result{OK: false, Error: "RADAR_CREATOR_ARCHIVE_FAILED"}, err
			}
			return okResult("creator archive complete", summary), nil
		},
	})

	// ---- agent role runners (LLM-ish) ----
	r.Register(Def{
		Name: "agent.content", Category: "agent", SideEffect: "network",
		Description: "ContentAgent: draft title/caption/tags",
		Params:      []Param{{Name: "draft", Type: "string", Required: true}, {Name: "lang", Type: "string"}},
		Handler:     planHandler("agent.content", "CLI: agent content --draft"),
	})
	registerAgentSurface(r)
	registerBotTools(r)
	registerOpenBotTools(r)
	registerOpenMausBotTools(r)
}

func planHandler(name, note string) Handler {
	return func(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
		return &Result{
			OK:      false,
			Tool:    name,
			Message: "tool is not bound to a runtime implementation",
			Error:   "TOOL_NOT_IMPLEMENTED",
			Data: map[string]interface{}{
				"implemented": false,
				"replacement": note,
			},
		}, fmt.Errorf("TOOL_NOT_IMPLEMENTED: %s", name)
	}
}

func dataDir(rt *Runtime) string {
	if rt != nil && rt.DataDir != "" {
		return rt.DataDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".alpha-nexus")
}

func runTikHubRankDaily(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	dir := dataDir(rt)
	cfg, configErr := tikhub.LoadProviderConfig(dir)
	configured := tikhub.ProviderConfigured(dir)
	// Safety: require explicit confirm for network scrape from Tool Agent
	if !argBool(args, "confirm", false) {
		return okResult("dry plan only — pass confirm=true to hit TikHub network", map[string]interface{}{
			"boards":     argString(args, "boards", "video,low_fan,high_like,high_fan"),
			"hint":       "each board ~0.001 free_credit when allow_free_credit",
			"configured": configured,
			"active":     configErr == nil && configured && cfg.Active,
		}), nil
	}
	c, _, err := tikhub.NewActiveClient(dir)
	if err != nil {
		code := tikhubProviderErrorCode(err)
		return &Result{OK: false, Error: code, Message: "请先在设置中保存、测试并激活 TikHub 数据 API"}, err
	}
	boardsCSV := argString(args, "boards", "video,low_fan,high_like,high_fan")
	pageSize := argInt(args, "page_size", 30)
	window := argInt(args, "window", 24)
	today := tikhub.Today()
	fetchedAt := time.Now().UTC().Format(time.RFC3339)
	boardMap := map[string]interface{}{}
	successCount := 0
	failedCount := 0
	for _, b := range strings.Split(boardsCSV, ",") {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		resp, err := c.FetchDouyinRank(ctx, b, 1, pageSize, window)
		if err != nil {
			boardMap[b] = map[string]interface{}{"error": err.Error()}
			failedCount++
			continue
		}
		items := tikhub.ExtractRankItems(resp)
		sub := filepath.Join("ranks", today, b+".json")
		fp, err := tikhub.SaveJSON(dir, sub, map[string]interface{}{
			"board": b, "fetched_at": fetchedAt, "items": items, "count": len(items),
		})
		if err != nil {
			return &Result{OK: false, Error: "TIKHUB_SNAPSHOT_WRITE_FAILED"}, err
		}
		md, err := tikhub.WriteRankMarkdown(dir, today, b, items, fetchedAt)
		if err != nil {
			return &Result{OK: false, Error: "TIKHUB_REPORT_WRITE_FAILED"}, err
		}
		boardMap[b] = map[string]interface{}{"count": len(items), "file": fp, "md": md}
		successCount++
	}

	if response, err := c.FetchDouyinHotSearch(ctx); err != nil {
		boardMap["hot_search"] = map[string]interface{}{"error": err.Error()}
		failedCount++
	} else {
		items := tikhub.ExtractRankItems(response)
		fp, saveErr := tikhub.SaveJSON(dir, filepath.Join("ranks", today, "hot_search.json"), map[string]interface{}{
			"board": "hot_search", "fetched_at": fetchedAt, "items": items, "count": len(items),
		})
		if saveErr != nil {
			return &Result{OK: false, Error: "TIKHUB_SNAPSHOT_WRITE_FAILED"}, saveErr
		}
		boardMap["hot_search"] = map[string]interface{}{"count": len(items), "file": fp}
		successCount++
	}

	index, err := tikhub.SaveJSON(dir, filepath.Join("ranks", today, "index.json"), map[string]interface{}{
		"date": today, "fetched_at": fetchedAt, "boards": boardMap,
	})
	if err != nil {
		return &Result{OK: false, Error: "TIKHUB_INDEX_WRITE_FAILED"}, err
	}
	if successCount == 0 {
		err := fmt.Errorf("all TikHub rank requests failed")
		return &Result{OK: false, Error: "TIKHUB_ALL_RANKS_FAILED", Data: map[string]interface{}{
			"date": today, "boards": boardMap, "index": index,
		}}, err
	}
	return okResult(fmt.Sprintf("fetched ranks for %s", today), map[string]interface{}{
		"date": today, "fetched_at": fetchedAt, "boards": boardMap, "index": index,
		"success": successCount, "failed": failedCount,
	}), nil
}

func tikhubProviderErrorCode(err error) string {
	if err != nil && strings.HasPrefix(err.Error(), "TIKHUB_") {
		return err.Error()
	}
	return "TIKHUB_PROVIDER_UNAVAILABLE"
}

func init() {
	RegisterCatalog()
}
