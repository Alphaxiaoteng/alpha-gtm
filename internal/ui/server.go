package ui

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/agenthub"
	"alpha-nexus-egolite/internal/agnes"
	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/creation"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/egopublish"
	"alpha-nexus-egolite/internal/errors"
	"alpha-nexus-egolite/internal/feedback"
	"alpha-nexus-egolite/internal/feishu"
	"alpha-nexus-egolite/internal/guanying"
	"alpha-nexus-egolite/internal/knowledge"
	"alpha-nexus-egolite/internal/llm"
	"alpha-nexus-egolite/internal/metricsschedule"
	"alpha-nexus-egolite/internal/notify"
	"alpha-nexus-egolite/internal/observability"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/radar"
	"alpha-nexus-egolite/internal/skilllib"
	"alpha-nexus-egolite/internal/studio"
	"alpha-nexus-egolite/internal/tikhub"
	"alpha-nexus-egolite/internal/tools"
	"alpha-nexus-egolite/internal/trace"
	"alpha-nexus-egolite/internal/xhs"
)

type Server struct {
	legacyUIRuntime
	cfg        config.Config
	db         *db.DB
	mux        *http.ServeMux
	mu         sync.Mutex
	facade     *browser.BrowserFacade
	egoRuntime *egolite.Runtime
	egoSpaces  *egolite.SpaceService
	// egoScreen caches the last read-only Space frame per Space ID so a poll
	// loop does not spawn one node runtime per request.
	egoScreenMu              sync.Mutex
	egoScreen                map[int64]egoScreenCacheEntry
	egoBookmarks             *egolite.BookmarkService
	focusEgoLite             func(context.Context) error
	sessionMonitor           *egolite.SessionMonitor
	metricsScheduler         *metricsschedule.Scheduler
	metricsScheduleStartOnce sync.Once
	sessionMonitorStartOnce  sync.Once
	loginSync                *egolite.LoginSyncMonitor
	loginSyncStartOnce       sync.Once
	loginOperationMu         sync.Mutex
	calibrateEgo             func(context.Context) (*egolite.CalibrationResult, error)
	serverMu                 sync.Mutex
	httpServer               *http.Server
	health                   healthState
	toolRuntime              *tools.Runtime
	videoAnalysisMu          sync.Mutex
	radarWorkerInstallOnce   sync.Once
	// dataAPICache 吸收「同一参数在几分钟内被反复请求」的突发重复。
	// 付费接口按调用计费，重复一次就是一次真金白银。
	dataAPICache          *dataAPICache
	radarSummaryMu        sync.Mutex
	radarSummary          *radarSummaryCacheEntry
	batchCtx              context.Context
	batchCancel           context.CancelFunc
	batchWG               sync.WaitGroup
	completeAI            func(context.Context, string, string) (string, error)
	studioAgent           *studio.DoubaoAgent
	agnesAPIKeyConfigured func() (bool, error)
	agnesSaveAPIKey       func(string) error
	agnesClient           func(time.Duration) (agnesMediaClient, error)
	generationMu          sync.Mutex
	generationPoll        time.Duration
	notifier              *notify.Service
	agentHub              *agenthub.Service
	knowledgeService      *knowledge.Service
	creationService       *creation.Service
	agentPrewarmOnce      sync.Once
	skillLibrary          *skilllib.Library
	legacyAccountsPath    string
	tikhubTestConnection  func(context.Context, string, string) error
	tikhubCallEndpoint    func(context.Context, tikhub.EndpointDefinition, map[string]any) (map[string]any, error)
	installRadarWorker    func(context.Context, string) error
	feedbackClient        feedback.Client
}

const TransportTokenHeader = "X-Alpha-Transport-Token"

type transportAuthContextKey struct{}

func transportAuthenticatedRequest(r *http.Request) bool {
	authenticated, _ := r.Context().Value(transportAuthContextKey{}).(bool)
	return authenticated
}

type ServeOptions struct {
	TransportToken string
	InstanceNonce  string
	Version        string
}

type healthState struct {
	Ready   bool
	Nonce   string
	Version string
	PID     int
}

type FingerprintConfig struct {
	Platform            string `json:"platform"`
	PlatformVersion     string `json:"platformVersion"`
	Brand               string `json:"brand"`
	BrandVersion        string `json:"brandVersion"`
	HardwareConcurrency int    `json:"hardwareConcurrency"`
	GPUVendor           string `json:"gpuVendor"`
	GPURenderer         string `json:"gpuRenderer"`
	Lang                string `json:"lang"`
	AcceptLang          string `json:"acceptLang"`
	Timezone            string `json:"timezone"`
}

// hashProfileID 将 accountID 映射为一个 8 位十六进制字符串作为 profile 目录名。
// 使用 Knuth 乘法哈希，保证同账号每次相同、不同账号看起来无规律、不暴露数字序号。
// ponytail: 纯本地标识符，外部永远不可见，主要防御本地文件系统 pattern 扫描。
// ProfileDirName 是 profile 目录名的唯一实现。CLI 的 account delete 也用
// 它来定位目录——早先两处各写一份哈希，同一个账号算出的目录名不同，
// 删除时会清错目录、把登录态留在磁盘上。
func ProfileDirName(accountID int64) string { return hashProfileID(accountID) }

func hashProfileID(accountID int64) string {
	h := uint64(accountID)*14695981039346656037 ^ (uint64(accountID) >> 16)
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	return fmt.Sprintf("p%08x", h&0xffffffff)
}

// clashPort 将 accountID 映射为 Clash SOCKS5 端口，哈希分布不规律，防止顺序检测。
// 同一账号始终映射到相同端口（幂等），范围 [startPort, startPort+999]。
func clashPort(accountID int64, startPort int) int {
	h := uint64(accountID) * 2654435761
	h ^= h >> 16
	h *= 0x45d9f3b
	return startPort + int(h%1000)
}

func defaultFingerprintConfig() string {
	c := smartFingerprint("beijing", "", rand.Intn(900000)+100000)
	b, _ := json.Marshal(c)
	return string(b)
}

func smartFingerprint(region, platformTarget string, seed int) FingerprintConfig {
	type rc struct {
		lang, acceptLang, timezone string
	}
	regions := map[string]rc{
		"beijing":   {"zh-CN", "zh-CN,zh,en-US", "Asia/Shanghai"},
		"shanghai":  {"zh-CN", "zh-CN,zh,en-US", "Asia/Shanghai"},
		"guangzhou": {"zh-CN", "zh-CN,zh,yue,en-US", "Asia/Shanghai"},
		"shenzhen":  {"zh-CN", "zh-CN,zh,yue,en-US", "Asia/Shanghai"},
		"chengdu":   {"zh-CN", "zh-CN,zh,en-US", "Asia/Shanghai"},
		"hangzhou":  {"zh-CN", "zh-CN,zh,en-US", "Asia/Shanghai"},
		"wuhan":     {"zh-CN", "zh-CN,zh,en-US", "Asia/Shanghai"},
		"xian":      {"zh-CN", "zh-CN,zh,en-US", "Asia/Shanghai"},
		"sanya":     {"zh-CN", "zh-CN,zh,en-US", "Asia/Shanghai"},
		"nanjing":   {"zh-CN", "zh-CN,zh,en-US", "Asia/Shanghai"},
		"chongqing": {"zh-CN", "zh-CN,zh,en-US", "Asia/Shanghai"},
		"changsha":  {"zh-CN", "zh-CN,zh,en-US", "Asia/Shanghai"},
		"hong_kong": {"zh-HK", "zh-HK,zh-TW,zh,en-US", "Asia/Hong_Kong"},
		"macau":     {"zh-MO", "zh-MO,zh-HK,zh,en-US", "Asia/Macau"},
		"taiwan":    {"zh-TW", "zh-TW,zh,en-US", "Asia/Taipei"},
		"us_east":   {"en-US", "en-US,en", "America/New_York"},
		"us_west":   {"en-US", "en-US,en", "America/Los_Angeles"},
		"japan":     {"ja-JP", "ja-JP,ja,en-US", "Asia/Tokyo"},
		"korea":     {"ko-KR", "ko-KR,ko,en-US", "Asia/Seoul"},
		"uk":        {"en-GB", "en-GB,en", "Europe/London"},
		"singapore": {"en-SG", "en-SG,zh,en-US", "Asia/Singapore"},
	}
	r := regions[region]
	if r.lang == "" {
		r = regions["beijing"]
	}

	platformPool := []string{"macos", "macos", "macos", "macos", "windows"}
	cpuPool := []int{4, 8, 8, 10, 12}

	platform := platformPool[seed%len(platformPool)]
	if platformTarget != "" {
		platform = platformTarget
	}

	return FingerprintConfig{
		Platform:            platform,
		PlatformVersion:     "14",
		Brand:               "Chrome",
		BrandVersion:        "139",
		HardwareConcurrency: cpuPool[(seed+7)%len(cpuPool)],
		Lang:                r.lang,
		AcceptLang:          r.acceptLang,
		Timezone:            r.timezone,
	}
}

// bootMark reports one startup stage when ALPHA_BOOT_TRACE=1. It is the only
// way to tell a slow migration from a slow provider probe; keep it opt-in so a
// normal start stays silent.
func bootMark(start time.Time, stage string) {
	if os.Getenv("ALPHA_BOOT_TRACE") != "1" {
		return
	}
	fmt.Fprintf(os.Stderr, "[boot] %-22s %6dms\n", stage, time.Since(start).Milliseconds())
}

// eventLogRetention bounds how long a diagnostic event file survives once its
// owning process is gone. Business truth lives in SQLite, so these files are
// only useful for recent troubleshooting.
const eventLogRetention = 14 * 24 * time.Hour

// pruneStaleEventLogs removes event logs left behind by processes that are no
// longer running. It reports how much it reclaimed on the boot trace line and
// otherwise stays silent; the server must start even when the logs directory is
// unreadable.
func pruneStaleEventLogs(dataDir string) {
	logsDir := filepath.Join(dataDir, "logs")
	result, err := observability.PruneEventFiles(logsDir, observability.PruneEventFileOptions{
		KeepPIDs: map[int]struct{}{os.Getpid(): {}},
		MaxAge:   eventLogRetention,
	})
	if err != nil {
		if os.Getenv("ALPHA_BOOT_TRACE") == "1" {
			fmt.Fprintf(os.Stderr, "[boot] event-log-prune skipped: %v\n", err)
		}
		return
	}
	if result.Removed == 0 {
		return
	}
	if os.Getenv("ALPHA_BOOT_TRACE") == "1" {
		fmt.Fprintf(os.Stderr, "[boot] event-log-prune removed=%d bytes=%d\n", result.Removed, result.Bytes)
	}
}

func NewServer(cfg config.Config) (*Server, error) {
	bootStart := time.Now()
	dataDir, err := cfg.ResolvedDataDir()
	if err != nil {
		return nil, err
	}
	// Merge persisted config.json (browser path, proxy, max concurrent)
	cfg = mergePersistedConfig(cfg, dataDir)
	database, err := db.Open(dataDir)
	if err != nil {
		return nil, err
	}
	bootMark(bootStart, "before-migrate")
	if err := database.Migrate(); err != nil {
		database.Close()
		return nil, err
	}
	bootMark(bootStart, "migrate")
	if _, err := database.RecoverRunningTikHubAPICalls(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("recover interrupted TikHub calls: %w", err)
	}
	bootMark(bootStart, "recover-tikhub")
	skillLibrary, err := skilllib.New(database, dataDir)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("initialize skill library: %w", err)
	}
	bootMark(bootStart, "skill-library")
	agentStore := agenthub.NewStore(database)
	if err := agentStore.EnsureLegacyOptionalProviders(context.Background()); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("migrate optional agent providers: %w", err)
	}
	bootMark(bootStart, "ensure-providers")
	configuredProviders, err := agentStore.ListProviderConfigs(context.Background())
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("load optional agent providers: %w", err)
	}
	bootMark(bootStart, "list-provider-configs")
	registry, err := agenthub.DefaultRegistryWithConfigs(configuredProviders)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("initialize agent providers: %w", err)
	}
	bootMark(bootStart, "build-registry")
	var customProviderServer *Server
	if err := registry.Add(agenthub.NewCustomLLMProvider(func() agenthub.OpenCodeLLMConfig {
		if customProviderServer == nil {
			return agenthub.OpenCodeLLMConfig{}
		}
		return customProviderServer.openCodeLLMConfig(dataDir)
	})); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("initialize custom LLM provider: %w", err)
	}
	bootMark(bootStart, "custom-provider")
	hub, err := agenthub.NewService(agentStore, registry, dataDir)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("initialize agent hub: %w", err)
	}
	bootMark(bootStart, "agent-hub-service")
	hub.SetSkillPromptExpander(skillLibrary)
	if strings.TrimSpace(cfg.EgoBrowserCommand) == "" {
		cfg.EgoBrowserCommand = "ego-browser"
	}
	cfg.BrowserEngine = "ego-lite"
	bootMark(bootStart, "skill-expander")
	egoRuntime := egolite.NewRuntime(cfg.EgoBrowserCommand, 60*time.Second)
	egoSpaces := egolite.NewSpaceService(database, egoRuntime)
	egoBookmarks := egolite.NewBookmarkService(database, dataDir, egolite.NewSpaceBookmarkImporter(egoSpaces))
	bootMark(bootStart, "ego-services")
	toolRuntime := &tools.Runtime{
		DataDir: dataDir, Database: database, CDPPort: cfg.CDPPort, Browser: cfg.BrowserPath,
		BrowserOS: cfg.EgoLitePath, Engine: cfg.BrowserEngine, Skills: skillLibrary,
		EgoBrowserCommand:   cfg.EgoBrowserCommand,
		MaxConcurrentSpaces: cfg.MaxConcurrentSpaces,
	}
	// Event logs are written per process, so a machine that has restarted a
	// thousand times carries a thousand files that nothing ever collected. The
	// sweep runs before this process opens its own file and is best-effort: a
	// failure here must never stop the server from starting.
	pruneStaleEventLogs(dataDir)
	if err := toolRuntime.EnableFileEvents("alpha-sidecar"); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("initialize diagnostic events: %w", err)
	}
	toolRuntime.Events = observability.NewFanoutEmitter(toolRuntime.Events, observability.NewOperationEmitter(database))
	bootMark(bootStart, "tool-runtime")
	metricsScheduler := metricsschedule.New(database, func(ctx context.Context, accountID int64) (string, error) {
		runner := toolRuntime.EgoRunner
		if runner == nil {
			runner = egolite.NewRuntime(cfg.EgoBrowserCommand, 90*time.Second)
		}
		service := egolite.NewCreatorMetricsService(database, runner)
		collection, collectErr := service.Collect(ctx, accountID)
		if collectErr != nil {
			return "", collectErr
		}
		return collection.State, nil
	})
	// Every completed sweep mirrors the new content into the bound Feishu Base.
	// A disabled or unbound mirror is a no-op, so this stays harmless until the
	// person runs `feishu init`.
	feishuExporter := feishu.NewExporter(database, feishu.NewClient())
	metricsScheduler.SetAfterRun(func(ctx context.Context, _ db.MetricsCollectionResult) error {
		if _, err := feishuExporter.Run(ctx, feishu.DefaultExportDays); err != nil {
			return err
		}
		return nil
	})
	bootMark(bootStart, "metrics-scheduler")
	batchCtx, batchCancel := context.WithCancel(context.Background())
	s := &Server{
		cfg: cfg, db: database, mux: http.NewServeMux(), toolRuntime: toolRuntime,
		egoRuntime: egoRuntime, egoSpaces: egoSpaces, egoBookmarks: egoBookmarks,
		focusEgoLite:     egolite.FocusDesktopApp,
		metricsScheduler: metricsScheduler,
		batchCtx:         batchCtx, batchCancel: batchCancel, studioAgent: studio.NewDoubaoAgent(),
		agnesAPIKeyConfigured: agnes.APIKeyConfigured, agnesSaveAPIKey: agnes.SaveAPIKey,
		agnesClient: func(timeout time.Duration) (agnesMediaClient, error) {
			return agnes.NewClientFromKeychain(timeout)
		},
		generationPoll:     5 * time.Second,
		agentHub:           hub,
		skillLibrary:       skillLibrary,
		installRadarWorker: radar.InstallWorker,
		dataAPICache:       newDataAPICache(),
	}
	bootMark(bootStart, "server-struct")
	s.knowledgeService = knowledge.NewService(database)
	s.creationService = creation.NewService(database, creationKnowledgeRetriever{service: s.knowledgeService}, creationAgentRunner{hub: hub})
	bootMark(bootStart, "app-services")
	if err := initializeLegacyUIRuntime(s, dataDir); err != nil {
		_ = database.Close()
		return nil, err
	}
	bootMark(bootStart, "legacy-ui-runtime")
	if homeDir, homeErr := os.UserHomeDir(); homeErr == nil {
		s.legacyAccountsPath = filepath.Join(homeDir, ".alpha-nexus", "app.db")
	}
	s.calibrateEgo = func(ctx context.Context) (*egolite.CalibrationResult, error) {
		return egolite.NewCalibrator(egolite.NewRuntimeProbe(egoRuntime)).Run(ctx)
	}
	customProviderServer = s
	// 完整 quick_check 放到服务可用之后，用独立只读连接跑：启动不再等它。
	go func() {
		if err := database.VerifyIntegrity(context.Background()); err != nil {
			s.serverMu.Lock()
			s.health.Nonce = "sqlite-integrity-failed"
			s.serverMu.Unlock()
			slog.Error("sqlite integrity check failed", "error", err)
			return
		}
		slog.Info("sqlite integrity check completed")
	}()
	s.notifier = notify.NewService(database, notify.NewKeychainStore(), notify.NewDefaultSender(8*time.Second))
	// The recurring monitor must never compete with an active login handoff or
	// its bounded verifier for the same EgoLite Space. A busy observation is
	// deliberately recorded as unknown so it cannot demote a reliable state or
	// queue another browser operation behind the user.
	s.sessionMonitor = egolite.NewSessionMonitor(database, egolite.SessionAccountCheckerFunc(func(ctx context.Context, account db.Account) (egolite.SessionCheck, error) {
		if err := ctx.Err(); err != nil {
			return egolite.SessionCheck{}, err
		}
		if !s.loginOperationMu.TryLock() {
			return egolite.SessionCheck{
				AuthState:    db.AccountAuthUnknown,
				ReasonCode:   db.LoginReasonBrowserOperationBusy,
				EvidenceCode: db.LoginEvidenceOperationLeaseBusy,
			}, nil
		}
		defer s.loginOperationMu.Unlock()
		if s.egoSpaces == nil {
			return egolite.SessionCheck{}, fmt.Errorf("EGO_RUNTIME_UNAVAILABLE")
		}
		return s.egoSpaces.CheckAccountSession(ctx, account)
	}), s.notifier)
	toolRuntime.CheckEgoSessions = func(ctx context.Context) (*tools.Result, error) {
		result, runErr := s.sessionMonitor.Run(ctx)
		if runErr != nil {
			return &tools.Result{OK: false, Tool: "ego.session.check", Error: "SESSION_MONITOR_RUN_FAILED"}, nil
		}
		return &tools.Result{OK: true, Tool: "ego.session.check", Message: "session check completed", Data: map[string]interface{}{
			"checked": result.Checked, "logged_in": result.LoggedIn, "logged_out": result.LoggedOut,
			"unknown": result.Unknown, "manual_takeover": result.ManualTakeover, "failed": result.Failed,
		}}, nil
	}
	s.loginSync = egolite.NewLoginSyncMonitor(database, egolite.LoginBatchVerifierFunc(func(ctx context.Context, identityID int64, platformCodes []string) (*egolite.LoginVerification, error) {
		s.loginOperationMu.Lock()
		defer s.loginOperationMu.Unlock()
		if s.egoSpaces == nil {
			return nil, fmt.Errorf("EGO_RUNTIME_UNAVAILABLE")
		}
		return s.egoSpaces.VerifyLoginBatch(ctx, identityID, platformCodes)
	}))
	if err := database.RecoverEgoSessionMonitorRun(time.Now().UTC()); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("recover ego session monitor: %w", err)
	}
	s.feedbackClient = feedback.NewFeishuClient(feedback.NewKeychainStore(), 8*time.Second)
	toolRuntime.AuthorizeAgentTool = s.AuthorizeAgentTool
	toolRuntime.FreezePublishPlan = func(_ context.Context, input publishplan.FreezeInput) (*tools.Result, error) {
		plan, freezeErr := (&publishplan.Service{DB: database}).Freeze(input)
		if freezeErr != nil {
			return &tools.Result{OK: false, Error: "VALIDATION_FAILED"}, freezeErr
		}
		return &tools.Result{OK: true, Tool: "publish.plan.freeze", Message: "publish plan frozen", Data: map[string]interface{}{"plan": plan}}, nil
	}
	toolRuntime.GetPublishPlan = func(_ context.Context, planID int64) (*tools.Result, error) {
		plan, getErr := (&publishplan.Service{DB: database}).Get(planID)
		if getErr != nil {
			return &tools.Result{OK: false, Error: "PUBLISH_PLAN_NOT_FOUND"}, getErr
		}
		return &tools.Result{OK: true, Tool: "publish.plan.get", Message: "publish plan", Data: map[string]interface{}{"plan": plan}}, nil
	}
	toolRuntime.PreparePublishPlan = s.preparePublishPlan
	toolRuntime.PrepareShopListing = s.PrepareShopListing
	toolRuntime.SubmitPublishPlan = s.submitPublishPlan
	toolRuntime.InspectPublishPlan = s.inspectPublishPlan
	toolRuntime.InspectPublishPlanComposer = s.inspectPublishPlanComposer
	toolRuntime.InspectPublishPlanManager = s.inspectPublishPlanManager
	toolRuntime.DeletePublishContent = s.deletePublishContent
	toolRuntime.RevealPublishPlanSettings = s.revealPublishPlanSettings
	toolRuntime.ResumePublishPlanTopics = s.resumePublishPlanTopics
	toolRuntime.GenerateMedia = s.enqueueAIMediaGeneration
	broker, err := agenthub.NewToolBroker(agenthub.NewStore(database), tools.Default, toolRuntime)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("initialize agent tool broker: %w", err)
	}
	hub.SetToolBroker(broker)
	// Test runners may only disable paid provider calls. There is deliberately
	// no environment switch that can inject a credential or enable generation.
	if os.Getenv("ALPHA_NEXUS_DISABLE_PAID_MEDIA") == "1" {
		s.agnesAPIKeyConfigured = func() (bool, error) { return false, nil }
		s.agnesClient = nil
	}
	s.completeAI = func(ctx context.Context, system, user string) (string, error) {
		s.mu.Lock()
		verified := s.cfg.AgentAPIVerified
		baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.AgentAPIBaseURL), "/")
		apiKey := strings.TrimSpace(s.cfg.AgentAPIKey)
		model := strings.TrimSpace(s.cfg.AgentModel)
		s.mu.Unlock()
		if !verified || !validDirectAPIBaseURL(baseURL) || apiKey == "" || model == "" {
			return "", fmt.Errorf("SETTINGS_LLM_NOT_CONFIGURED")
		}
		gateway := &llm.Gateway{
			BaseURL: baseURL, APIKey: apiKey, Model: model,
			Client: llm.NewDoubaoHTTPClient(60 * time.Second), Strict: true,
		}
		return gateway.Complete(ctx, system, user)
	}
	s.deduplicateFingerprints()
	if err := hub.EnsureGuanyingHackathonPreset(context.Background()); err != nil {
		slog.Warn("seed guanying agent preset failed", "error", err)
	}
	if err := guanying.EnsureSchemaAndSeed(context.Background(), database, dataDir); err != nil {
		slog.Warn("seed guanying data failed", "error", err)
	}
	s.routes()
	s.notifier.Start(batchCtx, &s.batchWG)
	s.resumeAIMediaGenerationJobs()
	lifecycleContext := trace.WithID(context.Background(), trace.New())
	_ = toolRuntime.Events.Emit(lifecycleContext, observability.Event{
		Level: slog.LevelInfo, Component: "database", Name: "quick_check.completed",
		Step: "startup", Status: "succeeded",
	})
	_ = toolRuntime.Events.Emit(lifecycleContext, observability.Event{
		Level: slog.LevelInfo, Component: "app.lifecycle", Name: "startup.completed",
		Step: "initialize", Status: "succeeded",
	})
	return s, nil
}

// mergePersistedConfig overlays dataDir/config.json onto cfg (non-empty fields win from file when cfg empty / defaults).
func mergePersistedConfig(cfg config.Config, dataDir string) config.Config {
	configPath := filepath.Join(dataDir, "config.json")
	fileData, err := os.ReadFile(configPath)
	if err != nil {
		return cfg
	}
	var persisted struct {
		EgoBrowserCommand     string `json:"ego_browser_command"`
		MaxConcurrentSpaces   int    `json:"max_concurrent_spaces"`
		MeasuredSpaceCapacity int    `json:"measured_space_capacity"`
		AIImageEnabled        bool   `json:"ai_image_enabled"`
		BrowserPath           string `json:"browser_path"`
		EgoLitePath           string `json:"ego_lite_path"`
		BrowserEngine         string `json:"browser_engine"`
		AIImageProvider       string `json:"ai_image_provider"`
		AIImageModel          string `json:"ai_image_model"`
		AIVideoModel          string `json:"ai_video_model"`
		AIImageAssetDir       string `json:"ai_image_asset_dir"`
		ContentLibraryDir     string `json:"content_library_dir"`
		MaxConcurrentBrowsers int    `json:"max_concurrent_browsers"`

		AgentAPIProvider string `json:"agent_api_provider"`
		AgentAPIBaseURL  string `json:"agent_api_base_url"`
		AgentAPIKey      string `json:"agent_api_key"`
		AgentModel       string `json:"agent_model"`
		AgentAPIVerified bool   `json:"agent_api_verified"`
		CodingModel      string `json:"coding_model"`
		PlanModel        string `json:"plan_model"`

		ImageAPIProvider string `json:"image_api_provider"`
		ImageAPIBaseURL  string `json:"image_api_base_url"`
		ImageAPIKey      string `json:"image_api_key"`

		VideoAPIProvider string `json:"video_api_provider"`
		VideoAPIBaseURL  string `json:"video_api_base_url"`
		VideoAPIKey      string `json:"video_api_key"`
	}
	if err := json.Unmarshal(fileData, &persisted); err != nil {
		return cfg
	}
	if cfg.EgoBrowserCommand == "" && persisted.EgoBrowserCommand != "" {
		cfg.EgoBrowserCommand = persisted.EgoBrowserCommand
	}
	if persisted.MaxConcurrentSpaces > 0 {
		cfg.MaxConcurrentSpaces = persisted.MaxConcurrentSpaces
	}
	if persisted.MeasuredSpaceCapacity > 0 {
		cfg.MeasuredSpaceCapacity = persisted.MeasuredSpaceCapacity
	}
	cfg.AIImageEnabled = cfg.AIImageEnabled || persisted.AIImageEnabled
	if cfg.BrowserPath == "" && persisted.BrowserPath != "" && !isRetiredEgoLitePath(persisted.BrowserPath) {
		cfg.BrowserPath = persisted.BrowserPath
	}
	if persisted.BrowserEngine != "" && browser.NormalizeEngine(persisted.BrowserEngine) != browser.EngineEgoLite && (cfg.BrowserEngine == "" || cfg.BrowserEngine == browser.EngineFingerprintChromium) {
		cfg.BrowserEngine = persisted.BrowserEngine
	}
	if persisted.MaxConcurrentBrowsers > 0 {
		cfg.MaxConcurrentBrowsers = persisted.MaxConcurrentBrowsers
	}
	if cfg.AIImageProvider == "" {
		cfg.AIImageProvider = persisted.AIImageProvider
	}
	if cfg.AIImageModel == "" {
		cfg.AIImageModel = persisted.AIImageModel
	}
	if cfg.AIVideoModel == "" {
		cfg.AIVideoModel = persisted.AIVideoModel
	}
	if cfg.AIImageAssetDir == "" {
		cfg.AIImageAssetDir = persisted.AIImageAssetDir
	}
	if cfg.ContentLibraryDir == "" {
		cfg.ContentLibraryDir = persisted.ContentLibraryDir
	}

	if cfg.AgentAPIProvider == "" {
		cfg.AgentAPIProvider = persisted.AgentAPIProvider
	}
	if cfg.AgentAPIBaseURL == "" {
		cfg.AgentAPIBaseURL = persisted.AgentAPIBaseURL
	}
	if cfg.AgentAPIKey == "" {
		cfg.AgentAPIKey = persisted.AgentAPIKey
	}
	if cfg.AgentModel == "" {
		cfg.AgentModel = persisted.AgentModel
	}
	cfg.AgentAPIVerified = persisted.AgentAPIVerified
	if cfg.CodingModel == "" {
		cfg.CodingModel = persisted.CodingModel
	}
	if cfg.PlanModel == "" {
		cfg.PlanModel = persisted.PlanModel
	}

	if cfg.ImageAPIProvider == "" {
		cfg.ImageAPIProvider = persisted.ImageAPIProvider
	}
	if cfg.ImageAPIBaseURL == "" {
		cfg.ImageAPIBaseURL = persisted.ImageAPIBaseURL
	}
	if cfg.ImageAPIKey == "" {
		cfg.ImageAPIKey = persisted.ImageAPIKey
	}

	if cfg.VideoAPIProvider == "" {
		cfg.VideoAPIProvider = persisted.VideoAPIProvider
	}
	if cfg.VideoAPIBaseURL == "" {
		cfg.VideoAPIBaseURL = persisted.VideoAPIBaseURL
	}
	if cfg.VideoAPIKey == "" {
		cfg.VideoAPIKey = persisted.VideoAPIKey
	}

	return cfg
}

func DefaultBrowserPath() string {
	path, _ := browser.PreferredLocalBrowser()
	return path
}

func isRetiredEgoLitePath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	base := filepath.Base(path)
	return base == "ego-lite" || base == "ego lite" || base == "ego-lite.app" || base == "ego lite.app" ||
		strings.Contains(path, "/ego-lite.app/") || strings.Contains(path, "/ego lite.app/")
}

func (s *Server) Close() error {
	lifecycleContext := trace.WithID(context.Background(), trace.New())
	if s.toolRuntime != nil && s.toolRuntime.Events != nil {
		_ = s.toolRuntime.Events.Emit(lifecycleContext, observability.Event{
			Level: slog.LevelInfo, Component: "app.lifecycle", Name: "shutdown.started",
			Step: "close", Status: "running",
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	shutdownErr := s.Shutdown(ctx)
	cancel()
	if s.facade != nil {
		s.facade.Detach()
	}
	// Account browsers are durable local sessions. Closing the HTTP/UI process
	// detaches supervision; only an explicit account stop terminates Chromium.
	var dbErr, eventErr error
	if s.toolRuntime != nil && s.toolRuntime.Events != nil {
		event := observability.Event{
			Level: slog.LevelInfo, Component: "app.lifecycle", Name: "shutdown.completed",
			Step: "close", Status: "succeeded",
		}
		if shutdownErr != nil {
			event.Level = slog.LevelError
			event.Status = "failed"
			event.ErrorCode = "INTERNAL_ERROR"
		}
		_ = s.toolRuntime.Events.Emit(lifecycleContext, event)
	}
	if s.db != nil {
		dbErr = s.db.Close()
	}
	if s.toolRuntime != nil {
		eventErr = s.toolRuntime.CloseEvents()
	}
	return stderrors.Join(shutdownErr, dbErr, eventErr)
}

// errorCodeOf extracts stable errors package code from "CODE: detail" (aligned with CLI).
func errorCodeOf(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	known := []errors.ErrorCode{
		errors.PROFILE_LOCKED, errors.NO_PORT, errors.NO_PORT_AVAILABLE,
		errors.BROWSER_CRASH, errors.CDP_CONNECT, errors.LEASE_EXPIRED,
		errors.BROWSER_NOT_FOUND, errors.PLATFORM_NOT_FOUND, errors.PROFILE_NOT_FOUND,
		errors.PROFILE_BUSY, errors.BUSY_BY_JOB, errors.BUSY_BY_UI,
		errors.BROWSER_OPERATION_BUSY, errors.BROWSER_OPERATION_LEASE_FAILED,
		errors.CDP_NOT_READY, errors.ALREADY_RUNNING, errors.ACCOUNT_NOT_FOUND, errors.ACCOUNT_DISABLED,
		errors.INVALID_ACCOUNT_ID, errors.MAX_CONCURRENT_BROWSERS, errors.FOCUS_FAILED,
		errors.INVALID_DATA_DIR, errors.LOGIN_REQUIRED,
	}
	for _, c := range known {
		if errors.Is(msg, c) {
			return string(c)
		}
	}
	if idx := strings.Index(msg, ":"); idx > 0 {
		tok := msg[:idx]
		if tok == strings.ToUpper(tok) && strings.Contains(tok, "_") {
			return tok
		}
	}
	return "INTERNAL"
}

func httpStatusForCode(code string) int {
	switch code {
	case string(errors.INVALID_ACCOUNT_ID), string(errors.LOGIN_REQUIRED), string(errors.ACCOUNT_DISABLED):
		return http.StatusBadRequest
	case string(errors.ACCOUNT_NOT_FOUND), string(errors.PROFILE_NOT_FOUND), string(errors.PLATFORM_NOT_FOUND):
		return http.StatusNotFound
	case string(errors.ALREADY_RUNNING), string(errors.MAX_CONCURRENT_BROWSERS),
		string(errors.PROFILE_LOCKED), string(errors.PROFILE_BUSY),
		string(errors.NO_PORT), string(errors.NO_PORT_AVAILABLE),
		string(errors.BUSY_BY_JOB), string(errors.BUSY_BY_UI), string(errors.BROWSER_OPERATION_BUSY):
		return http.StatusConflict
	case string(errors.FOCUS_FAILED):
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

// writeFacadeError returns JSON {errorCode, message, error} with HTTP status matching CLI codes.
func writeFacadeError(w http.ResponseWriter, err error) {
	code := errorCodeOf(err)
	status := httpStatusForCode(code)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"errorCode": code,
		"message":   err.Error(),
		"error":     err.Error(),
	})
}

func parseAccountIDParam(r *http.Request) (int64, error) {
	if idStr := r.PathValue("id"); idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			return 0, fmt.Errorf("%s: invalid path id %q", errors.INVALID_ACCOUNT_ID, idStr)
		}
		return id, nil
	}
	return 0, fmt.Errorf("%s: account id required", errors.INVALID_ACCOUNT_ID)
}

func (s *Server) uiHolderID() string {
	return fmt.Sprintf("ui-pid-%d", os.Getpid())
}

const (
	uiBrowserStartStopLeaseTTL = 2 * time.Minute
	uiBrowserPublishLeaseTTL   = 10 * time.Minute
	uiBrowserCollectLeaseTTL   = 5 * time.Minute
	uiBrowserAgentLeaseTTL     = 10 * time.Minute
	uiBrowserAIChatLeaseTTL    = 5 * time.Minute
	uiBackgroundProbePoll      = 25 * time.Millisecond
)

func (s *Server) acquireBrowserOperationLease(accountID int64, operation string, ttl time.Duration) (func(), error) {
	if s.db == nil || accountID <= 0 {
		return nil, fmt.Errorf("%s: accountId required", errors.INVALID_ACCOUNT_ID)
	}
	profileID := browser.ProfileIDForAccount(accountID)
	owner := s.uiHolderID() + ":" + operation + ":" + string(trace.New())
	deadline := time.Now().Add(uiBackgroundProbeWait)
	for {
		now := time.Now()
		acquired, err := s.db.AcquireBrowserOperationLease(profileID, operation, owner, now, ttl)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", errors.BROWSER_OPERATION_LEASE_FAILED, err)
		}
		if acquired {
			return func() { _ = s.db.ReleaseBrowserOperationLease(profileID, owner) }, nil
		}
		// The one-second account monitor is advisory and must not make a user
		// action randomly fail. Wait only for its bounded read-only probe; a real
		// publish/start/stop/Agent owner remains fail-closed and is never preempted.
		backgroundProbe := s.backgroundAccountProbeOwnsLease(profileID, now)
		if !backgroundProbe {
			// The bounded read-only probe may have released after the failed
			// insert. Retry once so that release is not surfaced as BUSY.
			acquired, err = s.db.AcquireBrowserOperationLease(profileID, operation, owner, time.Now(), ttl)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", errors.BROWSER_OPERATION_LEASE_FAILED, err)
			}
			if acquired {
				return func() { _ = s.db.ReleaseBrowserOperationLease(profileID, owner) }, nil
			}
		}
		if !backgroundProbe || !now.Before(deadline) {
			return nil, fmt.Errorf("%s: another browser operation owns profile %s", errors.BROWSER_OPERATION_BUSY, profileID)
		}
		time.Sleep(uiBackgroundProbePoll)
	}
}

func (s *Server) backgroundAccountProbeOwnsLease(profileID string, now time.Time) bool {
	active, err := s.db.HasBackgroundBrowserObservationLease(profileID, now)
	return err == nil && active
}

func (s *Server) deduplicateFingerprints() {
	profiles, err := s.db.ListBrowserProfiles()
	if err != nil {
		return
	}
	used := make(map[int]bool)
	for _, p := range profiles {
		if p.FingerprintSeed >= 100000 && p.FingerprintSeed <= 999999 {
			used[p.FingerprintSeed] = true
		}
	}
	kept := make(map[int]bool)
	for _, p := range profiles {
		if p.FingerprintSeed >= 100000 && p.FingerprintSeed <= 999999 {
			if kept[p.FingerprintSeed] {
				seed := rand.Intn(900000) + 100000
				for used[seed] {
					seed = rand.Intn(900000) + 100000
				}
				used[seed] = true
				s.db.UpdateBrowserProfileFingerprint(p.AccountID, seed)
			} else {
				kept[p.FingerprintSeed] = true
			}
		} else {
			seed := rand.Intn(900000) + 100000
			for used[seed] {
				seed = rand.Intn(900000) + 100000
			}
			used[seed] = true
			s.db.UpdateBrowserProfileFingerprint(p.AccountID, seed)
		}
	}
}

func (s *Server) handleAPIBatchRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.db.ListBatchRuns(50)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	resp := make([]batchRunResponse, 0, len(runs))
	for index := range runs {
		resp = append(resp, newBatchRunResponse(&runs[index]))
	}
	writeJSON(w, resp)
}

func (s *Server) handleAPIBatchItems(w http.ResponseWriter, r *http.Request) {
	batchID, _ := strconv.ParseInt(r.URL.Query().Get("batchId"), 10, 64)
	if batchID == 0 {
		http.Error(w, "batchId required", 400)
		return
	}
	items, err := s.db.ListBatchItems(batchID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	resp := make([]batchItemResponse, 0, len(items))
	for index := range items {
		resp = append(resp, newBatchItemResponse(&items[index]))
	}
	writeJSON(w, resp)
}

func (s *Server) handleAPIJobsList(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.db.ListPublishJobs(100)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	response := make([]publishJobResponse, 0, len(jobs))
	for index := range jobs {
		response = append(response, newPublishJobResponse(&jobs[index]))
	}
	writeJSON(w, response)
}

func (s *Server) handleAPIJobsEvents(w http.ResponseWriter, r *http.Request) {
	jobID, _ := strconv.ParseInt(r.URL.Query().Get("jobId"), 10, 64)
	if jobID == 0 {
		http.Error(w, "jobId required", 400)
		return
	}
	events, err := s.db.ListJobEventsByJob(jobID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newPublishJobEventResponses(events))
}

func (s *Server) handleAPIJobsEventsAll(w http.ResponseWriter, r *http.Request) {
	events, err := s.db.ListAllJobEvents(100)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(newPublishJobEventResponses(events))
}

func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.Handle("/", AssetsHandler())
	s.mux.HandleFunc("/api/overview", s.handleAPIOverview)
	// 手机端只读端点。它们和其余 /api/* 一样走 Bearer 鉴权（有用户时），
	// 但只接受 JSON：任何带 text/html 的请求都被拒绝，页面由 CLI 单独托管。
	s.mux.HandleFunc("GET /api/mobile/summary", s.handleAPIMobileSummary)
	s.mux.HandleFunc("GET /api/mobile/accounts", s.handleAPIMobileAccounts)
	s.mux.HandleFunc("GET /api/mobile/notifications", s.handleAPIMobileNotifications)
	s.mux.HandleFunc("GET /api/feedback", s.handleAPIFeedback)
	s.mux.HandleFunc("POST /api/feedback", s.handleAPIFeedback)
	s.mux.HandleFunc("/api/dashboard/summary", s.handleAPILocalDashboardSummary)
	s.mux.HandleFunc("/api/platforms", s.handleAPIPlatforms)
	s.mux.HandleFunc("GET /api/ego/doctor", s.handleAPIEgoDoctor)
	s.mux.HandleFunc("GET /api/ego/legacy-accounts", s.handleAPIEgoLegacyAccounts)
	s.mux.HandleFunc("POST /api/ego/legacy-accounts/import", s.handleAPIEgoLegacyAccountsImport)
	s.mux.HandleFunc("GET /api/ego/profiles", s.handleAPIEgoProfiles)
	s.mux.HandleFunc("PUT /api/ego/identities/{id}/profile", s.handleAPIEgoIdentityProfile)
	s.mux.HandleFunc("GET /api/ego/identities", s.handleAPIEgoIdentities)
	s.mux.HandleFunc("POST /api/ego/identities", s.handleAPIEgoIdentityCreate)
	s.mux.HandleFunc("GET /api/ego/identities/{id}/avatar", s.handleAPIEgoIdentityAvatar)
	s.mux.HandleFunc("PATCH /api/ego/identities/{id}", s.handleAPIEgoIdentityRename)
	s.mux.HandleFunc("PUT /api/ego/identities/{id}/platforms", s.handleAPIEgoIdentityPlatforms)
	s.mux.HandleFunc("POST /api/ego/identities/{id}/open", s.handleAPIEgoIdentityOpen)
	s.mux.HandleFunc("POST /api/ego/identities/{id}/login-batches", s.handleAPIEgoLoginBatch)
	s.mux.HandleFunc("POST /api/ego/identities/{id}/login-verify", s.handleAPIEgoLoginVerify)
	s.mux.HandleFunc("POST /api/ego/identities/{id}/verify", s.handleAPIEgoIdentityVerify)
	s.mux.HandleFunc("GET /api/ego/spaces", s.handleAPIEgoSpaces)
	s.mux.HandleFunc("GET /api/ego/spaces/live", s.handleAPIEgoSpacesLive)
	s.mux.HandleFunc("GET /api/ego/spaces/{id}/screen", s.handleAPIEgoSpaceScreen)
	s.mux.HandleFunc("GET /api/ego/spaces/{id}/tabs", s.handleAPIEgoSpaceTabs)
	s.mux.HandleFunc("POST /api/ego/spaces/{id}/tabs", s.handleAPIEgoSpaceTabs)
	s.mux.HandleFunc("GET /api/desktop/screen", s.handleAPIDesktopScreen)
	s.mux.HandleFunc("GET /api/feishu/status", s.handleAPIFeishuStatus)
	s.mux.HandleFunc("POST /api/feishu/sync", s.handleAPIFeishuSync)
	s.mux.HandleFunc("GET /api/guanying/overview", s.handleAPIGuanyingOverview)
	s.mux.HandleFunc("POST /api/guanying/match-quote", s.handleAPIGuanyingMatchQuote)
	s.mux.HandleFunc("GET /api/guanying/leads", s.handleAPIGuanyingLeads)
	s.mux.HandleFunc("POST /api/guanying/leads", s.handleAPIGuanyingLeads)
	s.mux.HandleFunc("GET /api/guanying/seo-pages", s.handleAPIGuanyingSEOPages)
	s.mux.HandleFunc("POST /api/guanying/seo-pages", s.handleAPIGuanyingSEOPages)
	s.mux.HandleFunc("GET /api/desktop/screen/permission", s.handleAPIDesktopScreenPermission)
	s.mux.HandleFunc("POST /api/desktop/screen/permission", s.handleAPIDesktopScreenPermission)
	s.mux.HandleFunc("POST /api/desktop/screen/reveal", s.handleAPIDesktopScreenReveal)
	s.mux.HandleFunc("POST /api/ego/spaces/{id}/screen/takeover", s.handleAPIEgoSpaceScreenTakeover)
	s.mux.HandleFunc("POST /api/ego/focus", s.handleAPIEgoFocus)
	s.mux.HandleFunc("POST /api/ego/bookmarks/import", s.handleAPIEgoBookmarksImport)
	s.mux.HandleFunc("POST /api/ego/concurrency/calibrate", s.handleAPIEgoCalibrate)
	s.mux.HandleFunc("GET /api/creator-metrics/schedule", s.handleAPIMetricsScheduleGet)
	s.mux.HandleFunc("PUT /api/creator-metrics/schedule", s.handleAPIMetricsSchedulePut)
	s.mux.HandleFunc("POST /api/creator-metrics/schedule/run", s.handleAPIMetricsScheduleRun)
	s.mux.HandleFunc("GET /api/ego/session-monitor", s.handleAPIEgoSessionMonitorGet)
	s.mux.HandleFunc("PUT /api/ego/session-monitor", s.handleAPIEgoSessionMonitorPut)
	s.mux.HandleFunc("POST /api/ego/session-monitor/run", s.handleAPIEgoSessionMonitorRun)
	s.mux.HandleFunc("POST /api/accounts/{id}/space/ensure", s.handleAPIEgoSpaceEnsure)
	s.mux.HandleFunc("POST /api/accounts/{id}/space/login", s.handleAPIEgoSpaceLogin)
	s.mux.HandleFunc("POST /api/accounts/{id}/space/resume", s.handleAPIEgoSpaceResume)
	s.mux.HandleFunc("POST /api/accounts/{id}/login-qr", s.handleAPIEgoLoginQR)
	s.mux.HandleFunc("/api/accounts", s.handleAPIAccounts)
	s.mux.HandleFunc("GET /api/account-tags", s.handleAPIAccountTags)
	s.mux.HandleFunc("POST /api/account-tags", s.handleAPIAccountTagCreate)
	s.mux.HandleFunc("POST /api/account-tags/bulk", s.handleAPIAccountTagBulkUpdate)
	s.mux.HandleFunc("PUT /api/account-tags/order", s.handleAPIAccountTagReorder)
	s.mux.HandleFunc("POST /api/account-tags/resolve", s.handleAPIAccountTagResolve)
	s.mux.HandleFunc("/api/accounts/add", s.handleAPIAccountAdd)
	s.mux.HandleFunc("/api/accounts/delete", s.handleAPIAccountDelete)
	s.mux.HandleFunc("PATCH /api/accounts/{id}/remark", s.handleAPIAccountRemark)
	s.mux.HandleFunc("PATCH /api/accounts/{id}/group", s.handleAPIAccountGroup)
	s.mux.HandleFunc("POST /api/accounts/{id}/enabled", s.handleAPIAccountEnabled)
	s.mux.HandleFunc("POST /api/accounts/{id}/assets", s.handleAPIMetaAccountAsset)
	s.mux.HandleFunc("GET /api/account-assets/{id}/avatar", s.handleAPIAccountAssetAvatar)

	s.mux.HandleFunc("GET /api/accounts/{id}/avatar", s.handleAPIAccountAvatar)
	s.mux.HandleFunc("/api/data/dates", s.handleAPIDataDates)
	s.mux.HandleFunc("/api/data/notes", s.handleAPIDataNotes)
	s.mux.HandleFunc("/api/data/accounts", s.handleAPIDataAccounts)
	s.mux.HandleFunc("/api/data/dashboard", s.handleAPIDataDashboard)
	s.mux.HandleFunc("POST /api/data/sqlite-sources", s.handleAPIDataLinkSQLiteSource)
	s.mux.HandleFunc("GET /api/trend-radar/summary", s.handleAPITrendRadarSummary)
	s.mux.HandleFunc("GET /api/trend-radar/config", s.handleAPITrendRadarConfig)
	s.mux.HandleFunc("PUT /api/trend-radar/config", s.handleAPITrendRadarConfig)
	s.mux.HandleFunc("POST /api/trend-radar/assign-keyword", s.handleAPITrendRadarAssignKeyword)
	s.mux.HandleFunc("GET /api/trend-radar/creators/compare", s.handleAPITrendRadarCreatorsCompare)
	s.mux.HandleFunc("POST /api/trend-radar/creators", s.handleAPITrendRadarCreatorAdd)
	s.mux.HandleFunc("PUT /api/trend-radar/creators/{platform}/{user_id}", s.handleAPITrendRadarCreatorUpdate)
	s.mux.HandleFunc("DELETE /api/trend-radar/creators/{platform}/{user_id}", s.handleAPITrendRadarCreatorDelete)
	s.mux.HandleFunc("PUT /api/trend-radar/creators/{platform}/{user_id}/auto-archive", s.handleAPITrendRadarCreatorAutoArchive)
	s.mux.HandleFunc("POST /api/trend-radar/creators/{platform}/{user_id}/archive", s.handleAPITrendRadarCreatorArchive)
	s.mux.HandleFunc("GET /api/trend-radar/creators/{platform}/{user_id}/archive-status", s.handleAPITrendRadarCreatorArchiveStatus)
	s.mux.HandleFunc("GET /api/trend-radar/creators/{platform}/{user_id}/archive-failures", s.handleAPITrendRadarCreatorArchiveFailures)
	s.mux.HandleFunc("POST /api/trend-radar/creators/{platform}/{user_id}/open-folder", s.handleAPITrendRadarCreatorOpenFolder)
	s.mux.HandleFunc("GET /api/trend-radar/creators/{platform}/{user_id}/media/{kind}/{file}", s.handleAPITrendRadarCreatorMedia)
	s.mux.HandleFunc("POST /api/trend-radar/covers/backfill", s.handleAPITrendRadarCoverBackfill)
	s.mux.HandleFunc("GET /api/trend-radar/covers/{name}", s.handleAPITrendRadarCover)
	s.mux.HandleFunc("POST /api/trend-radar/niches/{id}/run", s.handleAPITrendRadarNicheRun)
	s.mux.HandleFunc("PUT /api/trend-radar/niches/{id}/limits", s.handleAPITrendRadarNicheLimits)
	s.mux.HandleFunc("GET /api/trend-radar/runs", s.handleAPITrendRadarRuns)
	s.mux.HandleFunc("GET /api/trend-radar/runs/{id}", s.handleAPITrendRadarRuns)
	s.mux.HandleFunc("POST /api/trend-radar/runs/{id}/retry", s.handleAPITrendRadarRunRetry)
	s.mux.HandleFunc("GET /api/trend-radar/store", s.handleAPITrendRadarStore)
	s.mux.HandleFunc("POST /api/trend-radar/store/backfill", s.handleAPITrendRadarStoreBackfill)
	s.mux.HandleFunc("GET /api/trend-radar/store/unattributed", s.handleAPITrendRadarUnattributed)
	s.mux.HandleFunc("GET /api/trend-radar/store/work", s.handleAPITrendRadarWorkAttribution)
	s.mux.HandleFunc("GET /api/trend-radar/export", s.handleAPITrendRadarExport)
	s.mux.HandleFunc("GET /api/trend-radar/schedules", s.handleAPITrendRadarSchedules)
	s.mux.HandleFunc("POST /api/trend-radar/schedules", s.handleAPITrendRadarSchedules)
	s.mux.HandleFunc("PUT /api/trend-radar/schedules/{id}", s.handleAPITrendRadarSchedules)
	s.mux.HandleFunc("DELETE /api/trend-radar/schedules/{id}", s.handleAPITrendRadarSchedules)
	s.mux.HandleFunc("POST /api/trend-radar/schedules/run-due", s.handleAPITrendRadarRunDue)
	s.mux.HandleFunc("GET /api/trend-radar/videos", s.handleAPITrendRadarVideos)
	s.mux.HandleFunc("POST /api/trend-radar/videos", s.handleAPITrendRadarVideos)
	s.mux.HandleFunc("GET /api/trend-radar/videos/{id}/cover", s.handleAPITrendRadarVideoCover)
	s.mux.HandleFunc("POST /api/trend-radar/video-search/parse", s.handleAPITrendRadarVideoSearchParse)
	s.mux.HandleFunc("POST /api/trend-radar/video-search/{call_id}/analyze", s.handleAPITrendRadarVideoAnalysis)
	s.mux.HandleFunc("GET /api/trend-radar/rss/sources", s.handleAPITrendRadarRSSSources)
	s.mux.HandleFunc("POST /api/trend-radar/rss/sources", s.handleAPITrendRadarRSSSources)
	s.mux.HandleFunc("PUT /api/trend-radar/rss/sources/{id}", s.handleAPITrendRadarRSSSources)
	s.mux.HandleFunc("DELETE /api/trend-radar/rss/sources/{id}", s.handleAPITrendRadarRSSSources)
	s.mux.HandleFunc("POST /api/trend-radar/rss/sources/{id}/collect", s.handleAPITrendRadarRSSCollect)
	s.mux.HandleFunc("GET /api/data-api/config", s.handleAPIDataAPIConfig)
	s.mux.HandleFunc("PUT /api/data-api/credential", s.handleAPIDataAPICredential)
	s.mux.HandleFunc("POST /api/data-api/test", s.handleAPIDataAPITest)
	s.mux.HandleFunc("GET /api/data-api/calls", s.handleAPIDataAPICallsList)
	s.mux.HandleFunc("POST /api/data-api/calls", s.handleAPIDataAPICall)
	s.mux.HandleFunc("GET /api/data-api/calls/{id}/response", s.handleAPIDataAPICallResponse)
	s.mux.HandleFunc("/api/batch/runs", s.handleAPIBatchRuns)
	s.mux.HandleFunc("/api/batch/items", s.handleAPIBatchItems)
	s.mux.HandleFunc("GET /api/studio/schedule-handoffs", s.handleAPIStudioScheduleHandoffs)
	s.mux.HandleFunc("POST /api/studio/schedule-handoffs", s.handleAPIStudioScheduleHandoffs)
	s.mux.HandleFunc("GET /api/studio/schedule-handoffs/{id}", s.handleAPIStudioScheduleHandoff)
	s.mux.HandleFunc("POST /api/studio/schedule-handoffs/{id}/cancel", s.handleAPIStudioScheduleHandoffCancel)
	s.mux.HandleFunc("POST /api/publish-plans/freeze-handoff", s.handleAPIPublishPlanFreezeHandoff)
	s.mux.HandleFunc("POST /api/publish-plans", s.handleAPIPublishPlanCreate)
	s.mux.HandleFunc("GET /api/publish-plans", s.handleAPIPublishPlanList)
	s.mux.HandleFunc("GET /api/publish-plans/{id}", s.handleAPIPublishPlanGet)
	s.mux.HandleFunc("POST /api/publish-plans/{id}/approve", s.handleAPIPublishPlanApprove)
	s.mux.HandleFunc("POST /api/publish-plans/{id}/prepare", s.handleAPIPublishPlanPrepare)
	s.mux.HandleFunc("POST /api/publish-plans/{id}/submit", s.handleAPIPublishPlanSubmit)
	s.mux.HandleFunc("POST /api/publish-plans/{id}/verify-manager", s.handleAPIPublishPlanVerifyManager)
	s.mux.HandleFunc("POST /api/shop/listing/plans/{id}/prepare-approval", s.handleAPIShopListingPrepareApproval)
	s.mux.HandleFunc("POST /api/shop/listing/plans/{id}/prepare", s.handleAPIShopListingPrepare)
	s.mux.HandleFunc("GET /api/ego/publish-manifests", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"items": egopublish.EgoLiteManifests()})
	})
	s.mux.HandleFunc("GET /api/jobs/list", s.handleAPIJobsList)
	s.mux.HandleFunc("GET /api/jobs/events", s.handleAPIJobsEvents)
	s.mux.HandleFunc("GET /api/jobs/events/all", s.handleAPIJobsEventsAll)
	s.mux.HandleFunc("GET /api/operations/events", s.handleAPIOperationEvents)
	s.mux.HandleFunc("POST /api/diagnostics/snapshot", s.handleAPIDiagnosticSnapshot)
	s.mux.HandleFunc("GET /api/diagnostics/log-directory", s.handleAPIDiagnosticLogDirectory)
	s.mux.HandleFunc("GET /api/storage/usage", s.handleAPIStorageUsage)
	s.mux.HandleFunc("POST /api/storage/cleanup", s.handleAPIStorageCleanup)
	s.mux.HandleFunc("POST /api/diagnostics/client-event", s.handleAPIClientDiagnostic)
	s.mux.HandleFunc("GET /api/jobs/{id}", s.handleAPIJobDetail)
	s.mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleAPIJobCancel)
	s.mux.HandleFunc("POST /api/jobs/{id}/retry", s.handleAPIJobRetry)
	s.mux.HandleFunc("/api/contents", s.handleAPIContents)
	s.mux.HandleFunc("/api/contents/{id}", s.handleAPIContent)
	s.mux.HandleFunc("GET /api/contents/{id}/knowledge-citations", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "content id required", http.StatusBadRequest)
			return
		}
		s.handleAPIContentKnowledgeCitations(w, r, id)
	})
	s.mux.HandleFunc("POST /api/contents/{id}/clone", s.handleAPIContentClone)
	s.mux.HandleFunc("/api/contents/{id}/media", s.handleAPIContentMedia)
	s.mux.HandleFunc("/api/shop/contents", s.handleAPIShopContents)
	s.mux.HandleFunc("/api/shop/contents/{id}", s.handleAPIShopContent)
	s.mux.HandleFunc("/api/shop/contents/{id}/media", s.handleAPIShopContentMedia)
	s.mux.HandleFunc("GET /api/shop/media/{id}/content", s.handleAPIShopMediaContent)
	s.mux.HandleFunc("POST /api/content-assist", s.handleAPIContentAssist)
	s.mux.HandleFunc("POST /api/creation/drafts", s.handleAPICreationDraft)
	s.mux.HandleFunc("GET /api/creation/model-roles", s.handleAPICreationModelRoles)
	s.mux.HandleFunc("PUT /api/creation/model-roles/{role}", s.handleAPICreationModelRole)
	s.mux.HandleFunc("POST /api/creation/sessions", s.handleAPICreationSessions)
	s.mux.HandleFunc("GET /api/creation/sessions/{id}", s.handleAPICreationSession)
	s.mux.HandleFunc("POST /api/creation/sessions/{id}/runs", s.handleAPICreationSessionRun)
	s.mux.HandleFunc("GET /api/creation/workflows", s.handleAPICreationWorkflows)
	s.mux.HandleFunc("POST /api/creation/workflows", s.handleAPICreationWorkflows)
	s.mux.HandleFunc("GET /api/creation/workflows/{id}", s.handleAPICreationWorkflow)
	s.mux.HandleFunc("PATCH /api/creation/workflows/{id}", s.handleAPICreationWorkflow)
	s.mux.HandleFunc("POST /api/creation/workflows/{id}/advance", s.handleAPICreationWorkflowAdvance)
	s.mux.HandleFunc("POST /api/creation/workflows/{id}/runs", s.handleAPICreationWorkflowRun)
	s.mux.HandleFunc("GET /api/knowledge/bases", s.handleAPIKnowledgeBases)
	s.mux.HandleFunc("POST /api/knowledge/bases", s.handleAPIKnowledgeBases)
	s.mux.HandleFunc("PATCH /api/knowledge/bases/{id}", s.handleAPIKnowledgeBase)
	s.mux.HandleFunc("DELETE /api/knowledge/bases/{id}", s.handleAPIKnowledgeBase)
	s.mux.HandleFunc("POST /api/knowledge/imports", s.handleAPIKnowledgeImports)
	s.mux.HandleFunc("POST /api/knowledge/presets/alpha-notes/import", s.handleAPIAlphaNotesKnowledgeImport)
	s.mux.HandleFunc("GET /api/knowledge/imports/{id}", s.handleAPIKnowledgeImportRun)
	s.mux.HandleFunc("GET /api/knowledge/documents", s.handleAPIKnowledgeDocuments)
	s.mux.HandleFunc("GET /api/knowledge/scopes", s.handleAPIKnowledgeScopes)
	s.mux.HandleFunc("POST /api/knowledge/search", s.handleAPIKnowledgeSearch)
	s.mux.HandleFunc("GET /api/shop/knowledge/bases", s.handleAPIShopKnowledgeBases)
	s.mux.HandleFunc("POST /api/shop/knowledge/bases", s.handleAPIShopKnowledgeBases)
	s.mux.HandleFunc("POST /api/shop/knowledge/imports", s.handleAPIShopKnowledgeImports)
	s.mux.HandleFunc("GET /api/shop/knowledge/documents", s.handleAPIShopKnowledgeDocuments)
	s.mux.HandleFunc("POST /api/shop/knowledge/search", s.handleAPIShopKnowledgeSearch)
	s.mux.HandleFunc("DELETE /api/media/{id}", s.handleAPIMedia)
	s.mux.HandleFunc("GET /api/media/{id}/content", s.handleAPIMediaContent)
	s.mux.HandleFunc("GET /api/content-library/config", s.handleAPIContentLibraryConfig)
	s.mux.HandleFunc("PUT /api/content-library/config", s.handleAPIContentLibraryConfig)
	s.mux.HandleFunc("POST /api/content-library/videos", s.handleAPIContentLibraryVideoUpload)
	s.mux.HandleFunc("POST /api/content-library/images", s.handleAPIContentLibraryImageUpload)
	s.mux.HandleFunc("GET /api/media/library", s.handleAPIMediaLibrary)
	s.mux.HandleFunc("POST /api/media/library", s.handleAPIMediaLibrary)
	s.mux.HandleFunc("PUT /api/media/library/{id}", s.handleAPIMediaLibraryItem)
	s.mux.HandleFunc("DELETE /api/media/library/{id}", s.handleAPIMediaLibraryItem)
	s.mux.HandleFunc("/api/ai/config", s.handleAPIAIConfig)
	s.mux.HandleFunc("POST /api/ai/config/agent", s.handleAPIAgentConfig)
	s.mux.HandleFunc("POST /api/ai/test", s.handleAPIAITest)
	s.mux.HandleFunc("GET /api/agent/dsh-catalog", s.handleAPIDshCatalog)
	// PersonAgent（Bot 头像 / 参考图）依赖这条最小生图面，它与完整的 AI 图片
	// 工作区是两个用途：工作区仍由 AIImageEnabled 门控，这里无条件注册。
	// 每个 handler 都自带 fail-closed 校验（未配置 provider/key 时返回 400
	// AI_IMAGE_NOT_CONFIGURED），因此不需要额外开关。
	s.mux.HandleFunc("GET /api/ai-image/config", s.handleAPIAIImageConfig)
	s.mux.HandleFunc("PUT /api/ai-image/config", s.handleAPIAIImageConfig)
	s.mux.HandleFunc("PUT /api/ai-image/credential", s.handleAPIAIImageCredential)
	s.mux.HandleFunc("POST /api/ai-image/generate", s.handleAPIAIImageGenerate)
	s.mux.HandleFunc("POST /api/ai-image/assets/upload", s.handleAPIAIImageAssetUpload)
	s.mux.HandleFunc("GET /api/ai-image/assets/{name}", s.handleAPIAIImageAssetFile)
	if s.cfg.AIImageEnabled {
		s.mux.HandleFunc("GET /api/ai-image/assets", s.handleAPIAIImageAssets)
		s.mux.HandleFunc("POST /api/ai-image/assets", s.handleAPIAIImageAssets)
		s.mux.HandleFunc("GET /api/ai-image/workspace", s.handleAPIAIImageWorkspace)
		s.mux.HandleFunc("PUT /api/ai-image/workspace", s.handleAPIAIImageWorkspace)
		s.mux.HandleFunc("GET /api/ai-image/projects", s.handleAPIAIImageProjects)
		s.mux.HandleFunc("POST /api/ai-image/projects", s.handleAPIAIImageProjects)
		s.mux.HandleFunc("GET /api/ai-image/projects/{id}", s.handleAPIAIImageProject)
		s.mux.HandleFunc("PUT /api/ai-image/projects/{id}", s.handleAPIAIImageProject)
		s.mux.HandleFunc("POST /api/ai-image/plan", s.handleAPIAIImagePlan)
		s.mux.HandleFunc("GET /api/ai-image/generations", s.handleAPIAIMediaGenerations)
		s.mux.HandleFunc("POST /api/ai-image/generations", s.handleAPIAIMediaGenerations)
		s.mux.HandleFunc("POST /api/ai-image/generations/batch", s.handleAPIAIMediaGenerationBatch)
		s.mux.HandleFunc("GET /api/ai-image/generations/{id}", s.handleAPIAIMediaGeneration)
	}
	s.mux.HandleFunc("/api/scripts", s.handleAPIScripts)
	s.mux.HandleFunc("/api/scripts/{id}", s.handleAPIScript)
	s.mux.HandleFunc("GET /api/clip-jobs", s.handleAPIClipJobs)
	s.mux.HandleFunc("POST /api/clip-jobs/plan", s.handleAPIClipPlan)
	s.mux.HandleFunc("POST /api/clip-jobs/{id}/execute", s.handleAPIClipExecute)
	s.mux.HandleFunc("/api/templates", s.handleAPITemplates)
	s.mux.HandleFunc("/api/templates/{id}", s.handleAPITemplate)
	s.mux.HandleFunc("/api/metrics/summary", s.handleAPIMetricsSummary)
	s.mux.HandleFunc("/api/metrics", s.handleAPIMetrics)
	s.mux.HandleFunc("/api/publish-records", s.handleAPIPublishRecords)
	s.mux.HandleFunc("/api/publish-content-records", s.handleAPIPublishContentRecords)
	s.mux.HandleFunc("GET /api/publish-content-records/{id}/cover", s.handleAPIPublishContentRecordCover)
	s.mux.HandleFunc("POST /api/publish-content-records/{id}/delete", s.handleAPIPublishContentRecordDelete)
	s.mux.HandleFunc("GET /api/publish-jobs/manual", s.handleAPIManualPublishJobs)
	s.mux.HandleFunc("POST /api/publish-jobs/{id}/confirm", s.handleAPIPublishJobConfirm)
	s.mux.HandleFunc("POST /api/publish-jobs/{id}/verify-published", s.handleAPIPublishJobVerifyPublished)
	s.mux.HandleFunc("/api/account-assets", s.handleAPIAccountAssets)
	s.mux.HandleFunc("/api/notifications", s.handleAPINotifications)
	s.mux.HandleFunc("GET /api/notifications/{id}", s.handleAPINotification)
	s.mux.HandleFunc("POST /api/notifications/read-all", s.handleAPIAllNotificationsRead)
	s.mux.HandleFunc("POST /api/notifications/{id}/read", s.handleAPINotificationRead)
	s.mux.HandleFunc("GET /api/notification-settings", s.handleAPINotificationSettings)
	s.mux.HandleFunc("PUT /api/notification-settings", s.handleAPINotificationSettings)
	s.mux.HandleFunc("PUT /api/notification-settings/{kind}", s.handleAPINotificationChannel)
	s.mux.HandleFunc("POST /api/notification-settings/{kind}/test", s.handleAPINotificationChannelTest)
	s.mux.HandleFunc("GET /api/export/snapshot", s.handleAPIExportSnapshot)

	// Versioned tool bridge: same Registry contract as CLI and MCP.
	s.mux.HandleFunc("/api/v1/tools/schema", s.handleAPIToolsSchema)
	s.mux.HandleFunc("/api/v1/tools/call", s.handleAPIToolsCall)
	s.mux.HandleFunc("/api/v1/events", s.handleAPIJobEventsStream)

	// Agent multi-scheme hub (UI sub-buttons)
	s.mux.HandleFunc("/api/agent/schemes", s.handleAPIAgentSchemes)
	s.mux.HandleFunc("/api/agent/scheme/run", s.handleAPIAgentSchemeRun)
	s.mux.HandleFunc("GET /api/agent/providers", s.handleAPIAgentProviders)
	s.mux.HandleFunc("POST /api/agent/providers", s.handleAPIAgentProviderAdd)
	s.mux.HandleFunc("GET /api/agent/provider-catalog", s.handleAPIAgentProviderCatalog)
	s.mux.HandleFunc("POST /api/agent/provider-catalog/{id}/probe", s.handleAPIAgentProviderCatalogProbe)
	s.mux.HandleFunc("POST /api/agent/provider-catalog/{id}/connect", s.handleAPIAgentProviderCatalogConnect)
	s.mux.HandleFunc("POST /api/agent/providers/probe", s.handleAPIAgentProviderProbe)
	s.mux.HandleFunc("POST /api/agent/providers/{id}/probe", s.handleAPIAgentProviderProbe)
	s.mux.HandleFunc("POST /api/agent/providers/{id}/connect", s.handleAPIAgentProviderConnect)
	s.mux.HandleFunc("POST /api/agent/providers/{id}/models/validate", s.handleAPIAgentProviderModelValidation)
	s.mux.HandleFunc("POST /api/agent/providers/{id}/models/validate-batch", s.handleAPIAgentProviderModelValidationBatch)
	s.mux.HandleFunc("PUT /api/agent/providers/{id}/selection", s.handleAPIAgentProviderSelection)
	s.mux.HandleFunc("DELETE /api/agent/providers/{id}", s.handleAPIAgentProviderRemove)
	s.mux.HandleFunc("GET /api/agent/bootstrap", s.handleAPIAgentBootstrap)
	s.mux.HandleFunc("GET /api/agent-bots", s.handleAPIAgentBots)
	s.mux.HandleFunc("POST /api/agent-bots", s.handleAPIAgentBots)
	s.mux.HandleFunc("GET /api/agent-bots/rail", s.handleAPIAgentBotsRail)
	s.mux.HandleFunc("GET /api/agent-bots/{id}", s.handleAPIAgentBot)
	s.mux.HandleFunc("PUT /api/agent-bots/{id}", s.handleAPIAgentBot)
	s.mux.HandleFunc("DELETE /api/agent-bots/{id}", s.handleAPIAgentBot)
	s.mux.HandleFunc("PUT /api/agent-bots/{id}/pin", s.handleAPIAgentBotPin)
	s.mux.HandleFunc("PUT /api/agent-bots/{id}/primary", s.handleAPIAgentBotPrimary)
	s.mux.HandleFunc("GET /api/agent-bots/{id}/memory", s.handleAPIAgentBotMemory)
	s.mux.HandleFunc("PUT /api/agent-bots/{id}/memory", s.handleAPIAgentBotMemory)
	s.mux.HandleFunc("DELETE /api/agent-bots/{id}/memory", s.handleAPIAgentBotMemory)
	s.mux.HandleFunc("GET /api/agent/conversations", s.handleAPIAgentConversations)
	s.mux.HandleFunc("POST /api/agent/conversations", s.handleAPIAgentConversations)
	s.mux.HandleFunc("GET /api/agent/conversations/{id}", s.handleAPIAgentConversation)
	s.mux.HandleFunc("PATCH /api/agent/conversations/{id}", s.handleAPIAgentConversation)
	s.mux.HandleFunc("DELETE /api/agent/conversations/{id}", s.handleAPIAgentConversation)
	s.mux.HandleFunc("POST /api/agent/conversations/{id}/read", s.handleAPIAgentConversationRead)
	s.mux.HandleFunc("POST /api/agent/conversations/read-all", s.handleAPIAgentConversationsReadAll)
	s.mux.HandleFunc("GET /api/agent/conversations/{id}/knowledge-turns", s.handleAPIAgentKnowledgeTurns)
	s.mux.HandleFunc("GET /api/agent/conversations/{id}/tool-calls", s.handleAPIAgentConversationToolCalls)
	s.mux.HandleFunc("POST /api/agent/conversations/{id}/runs", s.handleAPIAgentRuns)
	// In-app subagents: one Bot hands a bounded task to another Bot.
	s.mux.HandleFunc("GET /api/agent/delegations", s.handleAPIAgentDelegations)
	s.mux.HandleFunc("POST /api/agent/delegations", s.handleAPIAgentDelegations)
	s.mux.HandleFunc("GET /api/agent/delegations/{id}", s.handleAPIAgentDelegation)
	s.mux.HandleFunc("POST /api/agent/delegations/{id}/cancel", s.handleAPIAgentDelegationCancel)
	s.mux.HandleFunc("GET /api/agent/runs/{id}/events", s.handleAPIAgentRunEvents)
	s.mux.HandleFunc("GET /api/agent/runs/{id}/activities", s.handleAPIAgentRunActivities)
	s.mux.HandleFunc("GET /api/agent/activities/{id}", s.handleAPIAgentActivity)
	s.mux.HandleFunc("POST /api/agent/runs/{id}/cancel", s.handleAPIAgentRunCancel)
	s.mux.HandleFunc("GET /api/agent/tools", s.handleAPIAgentTools)
	s.mux.HandleFunc("POST /api/agent/runs/{id}/tools", s.handleAPIAgentToolPropose)
	s.mux.HandleFunc("POST /api/agent/approvals/{id}/resolve", s.handleAPIAgentApprovalResolve)
	s.mux.HandleFunc("POST /api/agent/approval-memory/clear", s.handleAPIAgentApprovalMemoryClear)
	s.mux.HandleFunc("GET /api/agent/broker/tools", s.handleAPIAgentBrokerTools)
	s.mux.HandleFunc("POST /api/agent/broker/call", s.handleAPIAgentBrokerCall)
	s.mux.HandleFunc("POST /api/agent/attachments", s.handleAPIAgentAttachments)
	s.mux.HandleFunc("GET /api/skills", s.handleAPISkills)
	s.mux.HandleFunc("POST /api/skills", s.handleAPISkills)
	s.mux.HandleFunc("POST /api/skills/import", s.handleAPISkillImport)
	s.mux.HandleFunc("GET /api/skills/{id}", s.handleAPISkill)
	s.mux.HandleFunc("PUT /api/skills/{id}/enabled", s.handleAPISkillEnabled)

	// In-app terminal (legacy PTY) + coding chat (primary UX)
	s.mux.HandleFunc("/api/terminal/probe", s.handleAPITerminalProbe)
	s.mux.HandleFunc("/api/terminal/sessions", s.handleAPITerminalSessions)
	s.mux.HandleFunc("/api/terminal/stop", s.handleAPITerminalStop)
	s.mux.HandleFunc("/api/terminal/ws", s.handleAPITerminalWS)
	s.mux.HandleFunc("/api/coding/status", s.handleAPICodingStatus)
	s.mux.HandleFunc("/api/coding/chat", s.handleAPICodingChat)
	s.mux.HandleFunc("/api/coding/chat/stream", s.handleAPICodingChatStream)
	s.mux.HandleFunc("/api/coding/sessions", s.handleAPICodingSessions)
	s.mux.HandleFunc("/api/coding/upload", s.handleAPICodingUpload)
	s.mux.HandleFunc("/api/coding/works", s.handleAPICodingWorks)
	s.mux.HandleFunc("/api/agent/models", s.handleAPIAgentModels)

	// Local tool login (demo A): status/login public; me requires token when auth on.
	s.mux.HandleFunc("/api/auth/status", s.handleAuthStatus)
	s.mux.HandleFunc("/api/auth/login", s.handleAuthLogin)
	s.mux.HandleFunc("/api/auth/me", s.handleAuthMe)
}

func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.cfg.UIPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(listener, ServeOptions{})
}

// Serve runs the network UI on an already-bound listener. A non-empty
// TransportToken enables the desktop sidecar trust boundary without changing
// standalone CLI or in-process Wails behavior.
func (s *Server) Serve(listener net.Listener, opts ServeOptions) error {
	if listener == nil {
		return fmt.Errorf("UI listener is required")
	}
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !tcpAddr.IP.IsLoopback() {
		_ = listener.Close()
		return fmt.Errorf("UI listener must use a loopback TCP address")
	}
	s.cfg.UIPort = tcpAddr.Port
	if opts.TransportToken != "" {
		if !validSidecarSecret(opts.TransportToken) || !validSidecarSecret(opts.InstanceNonce) {
			_ = listener.Close()
			return fmt.Errorf("sidecar transport token and nonce must be 256-bit hex values")
		}
	} else if opts.InstanceNonce != "" {
		_ = listener.Close()
		return fmt.Errorf("sidecar nonce requires transport authentication")
	}
	s.serverMu.Lock()
	if s.httpServer != nil {
		s.serverMu.Unlock()
		_ = listener.Close()
		return fmt.Errorf("UI server is already running")
	}
	handler := s.withAuth(s.mux)
	if opts.TransportToken != "" {
		handler = s.withTransportAuth(handler, opts.TransportToken)
	}
	handler = s.withHTTPRecovery(handler)
	handler = s.withCORS(handler)
	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	s.httpServer = httpServer
	s.health = healthState{Ready: true, Nonce: opts.InstanceNonce, Version: opts.Version, PID: os.Getpid()}
	s.serverMu.Unlock()
	executable, _ := os.Executable()
	s.agentHub.SetBrokerEndpoint("http://"+listener.Addr().String(), executable)
	s.agentPrewarmOnce.Do(func() { s.agentHub.PrewarmProviders(2) })
	s.sessionMonitorStartOnce.Do(func() {
		if s.sessionMonitor != nil {
			s.sessionMonitor.Start(s.batchCtx, &s.batchWG)
		}
	})
	s.metricsScheduleStartOnce.Do(func() {
		if s.metricsScheduler != nil {
			s.metricsScheduler.Start(s.batchCtx, &s.batchWG)
		}
	})
	s.loginSyncStartOnce.Do(func() {
		if s.loginSync != nil {
			s.loginSync.Start(s.batchCtx, &s.batchWG)
		}
	})
	// 启动时对齐一次 radar worker：已安装但版本落后的会在 InstallWorker 里
	// 被替换掉。只在「启用定时计划」时安装的话，重建后的新版永远不会
	// 覆盖旧 worker，界面上的自动更新就是个空开关。
	s.radarWorkerInstallOnce.Do(func() {
		if s.installRadarWorker == nil {
			return
		}
		go func() {
			installCtx, cancel := context.WithTimeout(s.batchCtx, 30*time.Second)
			defer cancel()
			resolved, dirErr := s.cfg.ResolvedDataDir()
			if dirErr != nil {
				return
			}
			if err := s.installRadarWorker(installCtx, resolved); err != nil && s.toolRuntime != nil && s.toolRuntime.Events != nil {
				_ = s.toolRuntime.Events.Emit(context.Background(), observability.Event{
					Level: slog.LevelWarn, Component: "radar.worker", Name: "install.failed",
					Status: "failed", ErrorCode: "WORKER_START_FAILED",
				})
			}
		}()
	})

	addr := listener.Addr().String()
	fmt.Printf("UI server running at http://%s\n", addr)
	fmt.Printf("Browser path: %s\n", s.cfg.BrowserPath)
	if s.authRequired() {
		fmt.Println("Auth: ON (Bearer token required for /api/* except /api/auth/login|status)")
	} else {
		fmt.Println("Auth: OFF (no app_users yet — run alpha-nexus auth bootstrap and enter credentials privately)")
	}
	fmt.Println("Press Ctrl+C to stop.")
	err := httpServer.Serve(listener)
	s.stopBatchWork()
	s.serverMu.Lock()
	if s.httpServer == httpServer {
		s.httpServer = nil
		s.health.Ready = false
	}
	s.serverMu.Unlock()
	if stderrors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.stopBatchWork()
	terminalErr := s.terminalManager().StopAll()
	var agentErr error
	if s.agentHub != nil {
		agentErr = s.agentHub.Shutdown(ctx)
	}
	s.serverMu.Lock()
	httpServer := s.httpServer
	s.serverMu.Unlock()
	if httpServer == nil {
		return stderrors.Join(terminalErr, agentErr)
	}
	return stderrors.Join(terminalErr, agentErr, httpServer.Shutdown(ctx))
}

// healthSnapshot 返回一份一致的 health 拷贝。health 由后台完整性检查在
// goroutine 里改写，直接裸读会与 s.health = healthState{...} 构成数据竞争。
func (s *Server) healthSnapshot() healthState {
	s.serverMu.Lock()
	defer s.serverMu.Unlock()
	return s.health
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	s.serverMu.Lock()
	health := s.health
	s.serverMu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"schema_version": 1,
		"ready":          health.Ready,
		"version":        health.Version,
		"nonce":          health.Nonce,
		"pid":            health.PID,
	})
}

func validSidecarSecret(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "http://127.0.0.1:3080" || origin == "http://localhost:3080" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withHTTPRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := string(trace.New())
		w.Header().Set("X-Request-ID", requestID)
		ctx := observability.WithCorrelation(r.Context(), observability.Correlation{RequestID: requestID})
		started := time.Now()
		response := &diagnosticResponseWriter{ResponseWriter: w, requestID: requestID}
		_, routePattern := s.mux.Handler(r)
		defer func() {
			// 统一错误契约的出口：必须在 recover 之前把暂存的错误体落地，
			// 否则 panic 分支会抢先写 header，JSON 改写被跳过。
			response.finishPendingWrap()
			recovered := recover()
			if recovered == nil {
				s.emitHTTPCompletion(ctx, r, routePattern, response.statusCode(), time.Since(started))
				return
			}
			stackFingerprint := sha256.Sum256(debug.Stack())
			if s.toolRuntime != nil && s.toolRuntime.Events != nil {
				_ = s.toolRuntime.Events.Emit(ctx, observability.Event{
					Level: slog.LevelError, Component: "ui.http", Name: "request.panic",
					RequestID: requestID, Status: "failed", ErrorCode: "INTERNAL_ERROR",
					ErrorClass: "panic", TaskID: "panic-" + hex.EncodeToString(stackFingerprint[:])[:12],
					Step: diagnosticRouteStep(r.Method, routePattern), HTTPStatus: http.StatusInternalServerError,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "internal server error", "errorCode": "INTERNAL_ERROR", "requestId": requestID,
			})
		}()
		next.ServeHTTP(response, r.WithContext(ctx))
	})
}

type diagnosticResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
	// 统一错误契约：暂存 4xx/5xx plain text 响应体，出口处改写成 JSON。
	// 见 json_error.go。requestID 用于回传给前端排障。
	requestID   string
	wrapPending bool
	buf         []byte
}

func (w *diagnosticResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	// 4xx/5xx 的 plain text（http.Error 的默认形态）暂存，等 Write 收集完
	// 再一次性改成 JSON 写出。必须在此判断——http.Error 会先设 Content-Type
	// 再调 WriteHeader，此时 content type 已经可知。
	if shouldWrapErrorAsJSON(w.Header().Get("Content-Type"), status) {
		w.wrapPending = true
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *diagnosticResponseWriter) Write(data []byte) (int, error) {
	if w.wrapPending {
		w.buf = append(w.buf, data...)
		// 对调用方谎报已写出，真实写出发生在 finishPendingWrap。
		return len(data), nil
	}
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	return n, err
}

// 把暂存的 plain text 错误体改写成统一 JSON 并真正写出。
// 必须在请求结束时调用且至多一次。
func (w *diagnosticResponseWriter) finishPendingWrap() {
	if !w.wrapPending {
		return
	}
	w.wrapPending = false
	header := w.Header()
	header.Del("Content-Length")
	header.Set("Content-Type", "application/json; charset=utf-8")
	body := wrapErrorAsJSON(w.status, string(w.buf), w.requestID)
	w.ResponseWriter.WriteHeader(w.status)
	n, _ := w.ResponseWriter.Write(body)
	w.bytes += int64(n)
}

func (w *diagnosticResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *diagnosticResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *diagnosticResponseWriter) Flush() {
	if w.wrapPending {
		// 错误路径上的 Flush（SSE 已被排除）：先落地 JSON 再 flush。
		w.finishPendingWrap()
	}
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *diagnosticResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *diagnosticResponseWriter) Push(target string, options *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, options)
}

func (s *Server) emitHTTPCompletion(ctx context.Context, r *http.Request, routePattern string, status int, duration time.Duration) {
	if s.toolRuntime == nil || s.toolRuntime.Events == nil || !strings.HasPrefix(r.URL.Path, "/api/") {
		return
	}
	mutation := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
	if !mutation && status < http.StatusBadRequest && duration < 2*time.Second {
		return
	}
	event := observability.Event{
		Level: slog.LevelInfo, Component: "ui.http", Name: "request.completed",
		Step: diagnosticRouteStep(r.Method, routePattern), Status: "succeeded",
		DurationMS: duration.Milliseconds(), HTTPStatus: status,
	}
	if status >= http.StatusBadRequest {
		event.Status = "failed"
		event.Level = slog.LevelWarn
		event.ErrorCode = "HTTP_CLIENT_ERROR"
		if status >= http.StatusInternalServerError {
			event.Level = slog.LevelError
			event.ErrorCode = "HTTP_SERVER_ERROR"
		}
	}
	_ = s.toolRuntime.Events.Emit(ctx, event)
}

func diagnosticRouteStep(method, pattern string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "UNKNOWN"
	}
	pattern = strings.TrimSpace(pattern)
	if separator := strings.IndexByte(pattern, ' '); separator >= 0 {
		pattern = strings.TrimSpace(pattern[separator+1:])
	}
	if pattern == "" {
		pattern = "unmatched"
	}
	var builder strings.Builder
	builder.WriteString(method)
	builder.WriteByte('.')
	lastSeparator := true
	for _, char := range pattern {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			builder.WriteRune(char)
			lastSeparator = false
			continue
		}
		if !lastSeparator {
			builder.WriteByte('.')
			lastSeparator = true
		}
	}
	step := strings.Trim(builder.String(), ".")
	if len(step) > 128 {
		return method + ".api.route"
	}
	return step
}

func (s *Server) withTransportAuth(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/agent/broker/") {
			next.ServeHTTP(w, r)
			return
		}
		query := r.URL.Query()
		if query.Has("transport_token") || query.Has("transportToken") || query.Has("x-alpha-transport-token") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":     "transport token is forbidden in URL query",
				"errorCode": "TRANSPORT_TOKEN_QUERY_FORBIDDEN",
			})
			return
		}
		provided := r.Header.Get(TransportTokenHeader)
		if len(provided) != len(token) || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":     "missing or invalid transport token",
				"errorCode": "TRANSPORT_AUTH_REQUIRED",
			})
			return
		}
		ctx := context.WithValue(r.Context(), transportAuthContextKey{}, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) handleAPIOverview(w http.ResponseWriter, r *http.Request) {
	plats, _ := s.db.ListPlatforms()
	accounts, _ := s.db.ListAccounts()
	dataDir, _ := s.cfg.ResolvedDataDir()

	runningCount := 0
	if s.facade != nil {
		// Durable runtime rows survive a backend upgrade even though the new
		// Supervisor did not launch those browser processes. Count canonical
		// account status so the overview agrees with /api/browsers after restart.
		for i := range accounts {
			status, err := s.facade.Status(accounts[i].ID)
			if err == nil && status != nil && status.Running {
				runningCount++
			}
		}
	}
	maxConcurrent := 0
	if s.facade != nil {
		maxConcurrent = s.facade.MaxConcurrentBrowsers()
	}

	loggedIn, _ := s.countLoginState("logged_in")
	loggedOut, _ := s.countLoginState("logged_out")

	browserPath := s.cfg.BrowserPath
	browserFound := browserPath != ""
	if browserPath != "" {
		if _, err := os.Stat(browserPath); err != nil {
			browserFound = false
		} else {
			browserFound = true
		}
	}
	cands := browser.DetectLocalBrowsers()
	// mark active candidate
	activeID := ""
	for i := range cands {
		if cands[i].Found && browserPath != "" && cands[i].Path == browserPath {
			activeID = cands[i].ID
			break
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"version":                 "0.4.0",
		"data_dir":                dataDir,
		"cdp_port":                s.cfg.CDPPort,
		"ui_port":                 s.cfg.UIPort,
		"platforms":               len(plats),
		"accounts":                len(accounts),
		"browser_path":            browserPath,
		"browser_found":           browserFound,
		"ego_lite_path":           s.cfg.EgoLitePath,
		"browser_engine":          s.cfg.BrowserEngine,
		"browsers":                cands,
		"active_browser_id":       activeID,
		"running":                 runningCount,
		"max_concurrent_browsers": maxConcurrent,
		"logged_in":               loggedIn,
		"logged_out":              loggedOut,
		"goos":                    runtime.GOOS,
		"goarch":                  runtime.GOARCH,
		"server_time":             time.Now().Format(time.RFC3339),
	})
}

func (s *Server) countLoginState(state string) (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE login_state = ?", state).Scan(&count)
	return count, err
}

func (s *Server) handleAPIPlatforms(w http.ResponseWriter, r *http.Request) {
	plats := platforms.EgoLiteAll()
	type PlatformJSON struct {
		Code                 string                    `json:"code"`
		Name                 string                    `json:"name"`
		Icon                 string                    `json:"icon"`
		LoginURL             string                    `json:"loginUrl"`
		ManagerURL           string                    `json:"managerUrl"`
		PublishMode          string                    `json:"publishMode"`
		CanCreatePublishJob  bool                      `json:"canCreatePublishJob"`
		CanAttemptAutoSubmit bool                      `json:"canAttemptAutoSubmit"`
		PublishSummary       string                    `json:"publishSummary"`
		Category             string                    `json:"category"`
		SettingsSchema       *platforms.SettingsSchema `json:"settingsSchema,omitempty"`
	}
	out := make([]PlatformJSON, len(plats))
	for i, p := range plats {
		readiness := platforms.Readiness(p.Code)
		var settingsSchema *platforms.SettingsSchema
		if schema, ok := platforms.SettingsSchemaFor(p.Code); ok {
			settingsSchema = &schema
		}
		out[i] = PlatformJSON{
			Code:                 p.Code,
			Name:                 p.Name,
			Icon:                 p.Icon,
			LoginURL:             p.LoginURL,
			ManagerURL:           p.ManagerURL,
			PublishMode:          readiness.Mode,
			CanCreatePublishJob:  readiness.CanCreateJob,
			CanAttemptAutoSubmit: readiness.CanAttemptAutoSubmit,
			Category:             p.Category,
			PublishSummary:       readiness.Summary,
			SettingsSchema:       settingsSchema,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *Server) handleAPIAccounts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	accounts, err := s.db.ListAccounts()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	groupFilter := strings.TrimSpace(r.URL.Query().Get("group"))
	platformFilter := strings.TrimSpace(r.URL.Query().Get("platform"))
	includeDisabled := r.URL.Query().Get("includeDisabled") == "true"
	type AccountWithStatus struct {
		ID                int64           `json:"id"`
		IdentityID        int64           `json:"identityId"`
		PlatformCode      string          `json:"platformCode"`
		Kind              string          `json:"kind"`
		Name              string          `json:"name"`
		Avatar            string          `json:"avatar"`
		AvatarSource      string          `json:"avatarSource"`
		AvatarSHA256      string          `json:"avatarSha256"`
		AvatarRefreshedAt string          `json:"avatarRefreshedAt"`
		AccountIdentifier string          `json:"accountIdentifier"`
		LoginState        string          `json:"loginState"`
		LoginMode         string          `json:"loginMode"`
		AuthMethod        string          `json:"authMethod"`
		LastCheckAt       string          `json:"lastCheckAt"`
		LastFocusedAt     string          `json:"lastFocusedAt"`
		LoginReasonCode   string          `json:"loginReasonCode"`
		LoginEvidenceCode string          `json:"loginEvidenceCode"`
		AvatarErrorCode   string          `json:"avatarErrorCode"`
		LastObservedAt    string          `json:"lastObservedAt"`
		Remark            string          `json:"remark"`
		AccountGroup      string          `json:"accountGroup"`
		Enabled           bool            `json:"enabled"`
		SpaceID           int64           `json:"spaceId"`
		SpaceOwnership    string          `json:"spaceOwnership"`
		SpaceState        string          `json:"spaceState"`
		ActiveJobID       int64           `json:"activeJobId"`
		Tags              []db.AccountTag `json:"tags"`
	}
	tagsByAccount, err := s.db.ListAccountTagsByAccount()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// 账号页每秒轮询一次，逐账号查 Space 与最新登录事件会让单个请求产生
	// 2×N 次查询（27 个账号 = 54 次）。这里一次取回整张表再按 id 索引。
	spacesByIdentity, err := s.db.EgoSpacesByIdentity()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	latestEvents, err := s.db.LatestAccountLoginEvents()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	result := make([]AccountWithStatus, 0, len(accounts))
	for _, a := range accounts {
		if !includeDisabled && !a.Enabled {
			continue
		}
		if groupFilter != "" && a.AccountGroup != groupFilter {
			continue
		}
		if platformFilter != "" && a.PlatformCode != platformFilter {
			continue
		}
		state := "unbound"
		leaseOwner := ""
		spaceID := int64(0)
		activeJobID := int64(0)
		if space, ok := spacesByIdentity[a.IdentityID]; ok {
			state = space.State
			leaseOwner = space.Ownership
			spaceID = space.SpaceID
			activeJobID = space.ActiveJobID
		}
		avatar := ""
		avatarSource := a.AvatarSource
		avatarSHA256 := a.AvatarSHA256
		avatarRefreshedAt := a.AvatarRefreshedAt
		if avatarSource == db.AvatarSourceObserved && avatarSHA256 != "" {
			avatar = fmt.Sprintf("/api/accounts/%d/avatar?sha=%s", a.ID, avatarSHA256)
		} else if a.Avatar != "" {
			// 支持外部或历史远端头像（如 YouTube、TikTok）
			avatar = a.Avatar
		} else {
			avatarSource = db.AvatarSourcePlatformFallback
			avatarSHA256 = ""
			avatarRefreshedAt = ""
		}
		loginReasonCode := ""
		loginEvidenceCode := ""
		avatarErrorCode := ""
		lastObservedAt := ""
		if latest, ok := latestEvents[a.ID]; ok && latest != nil {
			loginReasonCode = latest.ReasonCode
			loginEvidenceCode = latest.EvidenceCode
			avatarErrorCode = latest.AvatarErrorCode
			lastObservedAt = latest.ObservedAt
		}
		result = append(result, AccountWithStatus{
			ID: a.ID, IdentityID: a.IdentityID, PlatformCode: a.PlatformCode, Kind: a.Kind,
			Name: a.Name, Avatar: avatar, AvatarSource: avatarSource, AvatarSHA256: avatarSHA256,
			AvatarRefreshedAt: avatarRefreshedAt, AccountIdentifier: a.AccountIdentifier,
			LoginState: a.LoginState, LoginMode: a.LoginMode, AuthMethod: a.AuthMethod, LastCheckAt: a.LastCheckAt,
			LastFocusedAt:   a.LastFocusedAt,
			LoginReasonCode: loginReasonCode, LoginEvidenceCode: loginEvidenceCode, AvatarErrorCode: avatarErrorCode, LastObservedAt: lastObservedAt,
			Remark: a.Remark, AccountGroup: a.AccountGroup, Enabled: a.Enabled,
			SpaceID: spaceID, SpaceOwnership: leaseOwner, SpaceState: state, ActiveJobID: activeJobID,
			Tags: tagsByAccount[a.ID],
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleAPIAccountAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		IdentityID int64  `json:"identityId"`
		Platform   string `json:"platform"`
		Name       string `json:"name"`
		Remark     string `json:"remark"`
		Group      string `json:"group"`
		StartNow   bool   `json:"startNow"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.Platform == "" {
		http.Error(w, "platform required", 400)
		return
	}
	platformAllowed := false
	for _, candidate := range platforms.EgoLiteAll() {
		if candidate.Code == req.Platform {
			platformAllowed = true
			break
		}
	}
	if !platformAllowed {
		http.Error(w, "platform is not available in the EgoLite release", 400)
		return
	}
	if req.Name == "" {
		req.Name = req.Platform + "-" + strconv.FormatInt(time.Now().UnixNano()%100000, 10)
	}
	if req.IdentityID <= 0 {
		req.IdentityID = 1
	}
	account, err := s.db.CreateAccountForIdentity(req.IdentityID, req.Platform, req.Name, req.Remark)
	if err != nil {
		http.Error(w, "create account failed", 400)
		return
	}
	if strings.TrimSpace(req.Group) != "" {
		_ = s.db.UpdateAccountGroup(account.ID, strings.TrimSpace(req.Group))
		account.AccountGroup = strings.TrimSpace(req.Group)
	}
	resp := map[string]interface{}{
		"id": account.ID, "identityId": account.IdentityID,
		"platformCode": account.PlatformCode, "name": account.Name,
		"remark": account.Remark, "loginState": account.LoginState,
		"started": false,
	}

	if req.StartNow && s.egoSpaces != nil {
		space, startErr := s.egoSpaces.OpenLoginAndHandoff(r.Context(), account.ID)
		if startErr != nil {
			resp["startErrorCode"] = errorCodeOf(startErr)
		} else {
			resp["started"] = true
			resp["space"] = space
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAPIAccountDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var activeJobs int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE account_id=? AND status IN ('pending','queued','running','manual_takeover','awaiting_review')`, req.ID).Scan(&activeJobs); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "ACCOUNT_DELETE_CHECK_FAILED"})
		return
	}
	if activeJobs > 0 {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "ACCOUNT_HAS_ACTIVE_JOB"})
		return
	}
	if space, err := s.db.GetEgoSpaceByAccount(req.ID); err == nil &&
		(space.ActiveJobID > 0 || space.State == "running") {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "EGO_SPACE_ACTIVE"})
		return
	}
	if err := s.db.DeleteAccount(req.ID); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"deleted": true})
}

func (s *Server) handleAPIAccountEnabled(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.Enabled == nil {
		http.Error(w, "invalid account enabled request", http.StatusBadRequest)
		return
	}
	if !*request.Enabled && s.facade != nil {
		if status, statusErr := s.facade.Status(accountID); statusErr == nil && status != nil && status.Running {
			if err := s.stopAccountBrowser(accountID); err != nil {
				writeFacadeError(w, err)
				return
			}
		}
	}
	if err := s.db.SetAccountEnabled(accountID, *request.Enabled); err != nil {
		http.Error(w, "update account enabled state failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"accountId": accountID, "enabled": *request.Enabled})
}

func (s *Server) handleAPISetFingerprint(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		AccountID int64 `json:"accountId"`
		Seed      int   `json:"seed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := s.db.UpdateBrowserProfileFingerprint(req.AccountID, req.Seed); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Server) handleAPISetFingerprintConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		AccountID int64  `json:"accountId"`
		Config    string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE browser_profiles SET fingerprint_config = ?, updated_at = ? WHERE account_id = ?`, req.Config, now, req.AccountID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Server) handleAPISmartFingerprint(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		AccountID      int64  `json:"accountId"`
		Region         string `json:"region"`
		PlatformTarget string `json:"platformTarget"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.Region == "" {
		req.Region = "beijing"
	}
	bp, err := s.db.GetBrowserProfileByAccount(req.AccountID)
	if err != nil {
		http.Error(w, "profile not found: "+err.Error(), 404)
		return
	}
	cfg := smartFingerprint(req.Region, req.PlatformTarget, bp.FingerprintSeed)
	b, _ := json.Marshal(cfg)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(`UPDATE browser_profiles SET fingerprint_config = ?, fingerprint_seed = ?, updated_at = ? WHERE account_id = ?`, string(b), bp.FingerprintSeed, now, req.AccountID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"seed":                bp.FingerprintSeed,
		"platform":            cfg.Platform,
		"platformVersion":     cfg.PlatformVersion,
		"brand":               cfg.Brand,
		"brandVersion":        cfg.BrandVersion,
		"hardwareConcurrency": cfg.HardwareConcurrency,
		"gpuVendor":           cfg.GPUVendor,
		"gpuRenderer":         cfg.GPURenderer,
		"lang":                cfg.Lang,
		"acceptLang":          cfg.AcceptLang,
		"timezone":            cfg.Timezone,
	})
}

// startAccountBrowser is the shared Facade start path for path/body APIs.
func (s *Server) startAccountBrowser(accountID int64) (map[string]interface{}, error) {
	if s.facade == nil {
		return nil, fmt.Errorf("INTERNAL: facade not initialized")
	}
	account, err := s.db.GetAccount(accountID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", errors.ACCOUNT_NOT_FOUND, err)
	}
	release, err := s.acquireBrowserOperationLease(accountID, "start", uiBrowserStartStopLeaseTTL)
	if err != nil {
		return nil, err
	}
	defer release()
	inst, err := s.facade.StartAccount(accountID, browser.StartOptions{
		HolderType: browser.HolderUI,
		HolderID:   s.uiHolderID(),
	})
	// ALREADY_RUNNING still returns usable instance — soft success (CLI-aligned)
	if err != nil && !(inst != nil && errors.Is(err.Error(), errors.ALREADY_RUNNING)) {
		return nil, err
	}
	already := err != nil && errors.Is(err.Error(), errors.ALREADY_RUNNING)

	loginURL := ""
	if plat, ferr := platforms.Find(account.PlatformCode); ferr == nil {
		loginURL = plat.ManagerURL
		if loginURL == "" {
			loginURL = plat.LoginURL
		}
	}

	if !already && inst != nil {
		s.afterBrowserStart(accountID, inst)
	}

	resp := map[string]interface{}{
		"running":   true,
		"accountId": accountID,
		"profileId": browser.ProfileIDForAccount(accountID),
		"pid":       inst.PID,
		"port":      inst.CDPPort,
		"cdpPort":   inst.CDPPort,
		"cdpUrl":    inst.CDPURL,
		"state":     string(inst.State),
		"loginUrl":  loginURL,
		"platform":  account.PlatformCode,
	}
	if already {
		resp["errorCode"] = string(errors.ALREADY_RUNNING)
		resp["alreadyRunning"] = true
	}
	return resp, nil
}

func (s *Server) stopAccountBrowser(accountID int64) error {
	if s.facade == nil {
		return fmt.Errorf("INTERNAL: facade not initialized")
	}
	if released, err := s.db.ReleaseStoppedCancelledPublishLease(accountID); err != nil {
		return err
	} else if released {
		return nil
	}
	release, err := s.acquireBrowserOperationLease(accountID, "stop", uiBrowserStartStopLeaseTTL)
	if err != nil {
		return err
	}
	defer release()
	return s.facade.StopAccount(accountID)
}

// afterBrowserStart records that a human login is pending. Verification is an
// explicit user action; no browser, login, or tab watchdog is started here.
func (s *Server) afterBrowserStart(accountID int64, _ *browser.Instance) {
	s.markActiveLoginAwaiting(accountID)
}

func (s *Server) completeActiveLoginSession(accountID int64, status, code string) {
	if session, err := s.db.ActiveLoginSession(accountID); err == nil && session != nil {
		_ = s.db.UpdateLoginSession(session.ID, status, code)
	}
}

func (s *Server) markActiveLoginAwaiting(accountID int64) {
	s.completeActiveLoginSession(accountID, "awaiting_login", "")
}

func (s *Server) handleAPIAccountBrowserStart(w http.ResponseWriter, r *http.Request) {
	id, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	resp, err := s.startAccountBrowser(id)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAPIAccountBrowserStop(w http.ResponseWriter, r *http.Request) {
	id, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	if err := s.stopAccountBrowser(id); err != nil {
		writeFacadeError(w, err)
		return
	}
	s.completeActiveLoginSession(id, "cancelled", "BROWSER_STOPPED")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"stopped":   true,
		"accountId": id,
		"profileId": browser.ProfileIDForAccount(id),
	})
}

type browserBatchStopRequest struct {
	AccountIDs []int64 `json:"account_ids"`
}

type browserBatchStopResult struct {
	AccountID int64  `json:"account_id"`
	Stopped   bool   `json:"stopped"`
	ErrorCode string `json:"error_code,omitempty"`
}

type browserBatchStopResponse struct {
	Requested int                      `json:"requested"`
	Stopped   int                      `json:"stopped"`
	Failed    int                      `json:"failed"`
	Results   []browserBatchStopResult `json:"results"`
}

func (s *Server) handleAPIBrowserBatchStop(w http.ResponseWriter, r *http.Request) {
	var body browserBatchStopRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeFacadeError(w, fmt.Errorf("%s: account_ids required", errors.INVALID_ACCOUNT_ID))
		return
	}
	accountIDs := make([]int64, 0, len(body.AccountIDs))
	seen := make(map[int64]struct{}, len(body.AccountIDs))
	for _, accountID := range body.AccountIDs {
		if accountID <= 0 {
			writeFacadeError(w, fmt.Errorf("%s: account_ids must be positive", errors.INVALID_ACCOUNT_ID))
			return
		}
		if _, exists := seen[accountID]; exists {
			continue
		}
		seen[accountID] = struct{}{}
		accountIDs = append(accountIDs, accountID)
	}
	if len(accountIDs) == 0 || len(accountIDs) > 100 {
		writeFacadeError(w, fmt.Errorf("%s: account_ids must contain 1 to 100 accounts", errors.INVALID_ACCOUNT_ID))
		return
	}

	response := browserBatchStopResponse{
		Requested: len(accountIDs),
		Results:   make([]browserBatchStopResult, 0, len(accountIDs)),
	}
	// BrowserFacade remains the only lifecycle authority. Sequential stops
	// avoid racing accounts that resolve to the same profile.
	for _, accountID := range accountIDs {
		result := browserBatchStopResult{AccountID: accountID}
		if err := s.stopAccountBrowser(accountID); err != nil {
			result.ErrorCode = errorCodeOf(err)
			response.Failed++
		} else {
			result.Stopped = true
			response.Stopped++
			s.completeActiveLoginSession(accountID, "cancelled", "BROWSER_STOPPED")
		}
		response.Results = append(response.Results, result)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (s *Server) handleAPIAccountBrowserFocus(w http.ResponseWriter, r *http.Request) {
	id, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	if s.facade == nil {
		writeFacadeError(w, fmt.Errorf("INTERNAL: facade not initialized"))
		return
	}
	if err := s.facade.Focus(id); err != nil {
		writeFacadeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accountId": id})
}

func (s *Server) handleAPIBrowserStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		AccountID int64 `json:"accountId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.AccountID <= 0 {
		writeFacadeError(w, fmt.Errorf("%s: accountId required", errors.INVALID_ACCOUNT_ID))
		return
	}
	resp, err := s.startAccountBrowser(req.AccountID)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAPIBrowserStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		AccountID int64 `json:"accountId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := s.stopAccountBrowser(req.AccountID); err != nil {
		writeFacadeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"stopped": true, "accountId": req.AccountID})
}

func (s *Server) handleAPIBrowserStatus(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("accountId")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	if id <= 0 {
		writeFacadeError(w, fmt.Errorf("%s: accountId required", errors.INVALID_ACCOUNT_ID))
		return
	}
	if s.facade == nil {
		writeFacadeError(w, fmt.Errorf("INTERNAL: facade not initialized"))
		return
	}
	st, err := s.facade.Status(id)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"accountId":  st.AccountID,
		"profileId":  st.ProfileID,
		"running":    st.Running,
		"state":      string(st.State),
		"pid":        st.PID,
		"cdpPort":    st.CDPPort,
		"cdpUrl":     st.CDPURL,
		"leaseOwner": st.LeaseOwner,
	})
}

func (s *Server) handleAPIDataDates(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET only", 405)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	dates, err := xhs.ListDates(filepath.Join(dataDir, "xhs_data"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var result []map[string]interface{}
	for _, d := range dates {
		data, err := xhs.LoadFromFile(d, filepath.Join(dataDir, "xhs_data"))
		totalNotes := 0
		if err == nil {
			for _, a := range data.Accounts {
				totalNotes += a.NotesCount
			}
		}
		result = append(result, map[string]interface{}{
			"date":        d,
			"accounts":    len(data.Accounts),
			"total_notes": totalNotes,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleAPIDataNotes(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET only", 405)
		return
	}
	account := r.URL.Query().Get("account")
	if account == "" {
		http.Error(w, "missing account param", 400)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	notes, err := xhs.GetAccountNotes(dataDir, account)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if notes == nil {
		notes = []xhs.Note{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notes)
}

func (s *Server) handleAPIDataAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET only", 405)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	collections, err := xhs.AggregateAccounts(dataDir)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(collections)
}

func (s *Server) handleAPIDataDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "GET only", 405)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	stats, err := xhs.DashboardSummary(dataDir)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleAPIBrowsers lists running browsers via Facade (T-C01 GET /api/browsers).
func (s *Server) handleAPIBrowsers(w http.ResponseWriter, r *http.Request) {
	s.writeBrowsersList(w)
}

// handleAPIBrowserTabs keeps legacy path; same payload as /api/browsers.
func (s *Server) handleAPIBrowserTabs(w http.ResponseWriter, r *http.Request) {
	s.writeBrowsersList(w)
}

func (s *Server) writeBrowsersList(w http.ResponseWriter) {
	type tabResp struct {
		ProfileID  string `json:"profileId"`
		AccountID  int64  `json:"accountId"`
		State      string `json:"state"`
		CDPPort    int    `json:"cdpPort"`
		CDPURL     string `json:"cdpUrl"`
		PID        int    `json:"pid"`
		StartedAt  string `json:"startedAt,omitempty"`
		LastSeenAt string `json:"lastSeenAt,omitempty"`
		LeaseOwner string `json:"leaseOwner,omitempty"`
		Running    bool   `json:"running"`
	}

	tabs := make([]tabResp, 0)
	if s.facade == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"instances": tabs})
		return
	}

	seen := map[string]bool{}
	if accounts, err := s.db.ListAccounts(); err == nil {
		for _, a := range accounts {
			st, err := s.facade.Status(a.ID)
			if err != nil || st == nil || !st.Running {
				continue
			}
			seen[st.ProfileID] = true
			tabs = append(tabs, tabResp{
				ProfileID:  st.ProfileID,
				AccountID:  st.AccountID,
				State:      string(st.State),
				CDPPort:    st.CDPPort,
				CDPURL:     st.CDPURL,
				PID:        st.PID,
				LeaseOwner: st.LeaseOwner,
				Running:    true,
			})
		}
	}
	for _, inst := range s.facade.ListRunning() {
		if seen[inst.ProfileID] {
			continue
		}
		if inst.State == browser.StateStopped || inst.State == browser.StateCrashed {
			continue
		}
		tabs = append(tabs, tabResp{
			ProfileID:  inst.ProfileID,
			AccountID:  inst.AccountID,
			State:      string(inst.State),
			CDPPort:    inst.CDPPort,
			CDPURL:     inst.CDPURL,
			PID:        inst.PID,
			StartedAt:  inst.StartedAt.Format(time.RFC3339),
			LastSeenAt: inst.LastSeenAt.Format(time.RFC3339),
			LeaseOwner: inst.LeaseOwner,
			Running:    true,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"instances": tabs, "count": len(tabs)})
}

func openBrowser(url string) {
	time.Sleep(600 * time.Millisecond)
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = exec.Command("open", url).Start()
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("cmd", "/c", "start", url).Start()
	}
	if err != nil {
		fmt.Printf("Please open %s in your browser\n", url)
	}
}

func (s *Server) handleAPIBrowserFocus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		AccountID int64  `json:"accountId"`
		ProfileID string `json:"profileId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if s.facade == nil {
		writeFacadeError(w, fmt.Errorf("INTERNAL: facade not initialized"))
		return
	}
	accountID := req.AccountID
	if accountID <= 0 && req.ProfileID != "" {
		if strings.HasPrefix(req.ProfileID, "acct-") {
			if id, err := strconv.ParseInt(strings.TrimPrefix(req.ProfileID, "acct-"), 10, 64); err == nil {
				accountID = id
			}
		}
	}
	if accountID <= 0 {
		writeFacadeError(w, fmt.Errorf("%s: accountId or profileId required", errors.INVALID_ACCOUNT_ID))
		return
	}
	if err := s.facade.Focus(accountID); err != nil {
		writeFacadeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accountId": accountID})
}

func (s *Server) handleAPIConfigBrowsers(w http.ResponseWriter, r *http.Request) {
	// GET: scan local browsers; POST with {id} or {path} to apply quickly
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"browsers":          browser.DetectLocalBrowsers(),
			"browser_path":      s.cfg.BrowserPath,
			"ego_lite_path":     s.cfg.EgoLitePath,
			"browser_engine":    s.cfg.BrowserEngine,
			"active_browser_id": matchBrowserID(s.cfg.BrowserPath),
		})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "GET or POST only", 405)
		return
	}
	var req struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		Engine string `json:"engine"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	path := req.Path
	engine := req.Engine
	if req.ID != "" {
		for _, c := range browser.DetectLocalBrowsers() {
			if c.ID == req.ID && c.Found {
				path = c.Path
				engine = c.Engine
				break
			}
		}
	}
	if path == "" {
		// auto pick preferred
		path, engine = browser.PreferredLocalBrowser()
	}
	if path == "" {
		http.Error(w, "未在本机找到可用浏览器", 400)
		return
	}
	if browser.NormalizeEngine(engine) == browser.EngineEgoLite || isRetiredEgoLitePath(path) {
		http.Error(w, "ego lite 已取消，请选择 Chromium、BrowserOS 或 Google Chrome", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(path); err != nil {
		http.Error(w, fmt.Sprintf("浏览器路径不存在: %v", err), 400)
		return
	}
	if err := s.persistBrowserConfig(path, engine); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"browserPath": path,
		"engine":      engine,
		"id":          matchBrowserID(path),
	})
}

func matchBrowserID(path string) string {
	if path == "" {
		return ""
	}
	for _, c := range browser.DetectLocalBrowsers() {
		if c.Found && c.Path == path {
			return c.ID
		}
	}
	return ""
}

func (s *Server) persistBrowserConfig(browserPath, engine string) error {
	if browser.NormalizeEngine(engine) == browser.EngineEgoLite || isRetiredEgoLitePath(browserPath) {
		return fmt.Errorf("ego lite 已取消，请选择 Chromium、BrowserOS 或 Google Chrome")
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.updatePersistedConfigLocked(dataDir, func(existing map[string]any) {
		existing["browser_path"] = browserPath
		delete(existing, "ego_lite_path")
		if engine != "" {
			existing["browser_engine"] = engine
		}
		if engine == browser.EngineBrowserOS || strings.Contains(strings.ToLower(browserPath), "browseros") {
			existing["browseros_path"] = browserPath
		}
	}); err != nil {
		return err
	}
	s.cfg.BrowserPath = browserPath
	if engine != "" {
		s.cfg.BrowserEngine = engine
	}
	if engine == browser.EngineBrowserOS || strings.Contains(strings.ToLower(browserPath), "browseros") {
		s.cfg.BrowserOSPath = browserPath
	}
	if s.facade != nil {
		s.facade.SetBrowserPath(browserPath)
		s.facade.SetEgoLitePath(s.cfg.EgoLitePath)
		s.facade.SetBrowserOSPath(s.cfg.BrowserOSPath)
		s.facade.SetDefaultEngine(s.cfg.BrowserEngine)
	}
	return nil
}

// updatePersistedConfigLocked merges one local configuration update without
// dropping unrelated settings. Callers must hold s.mu.
func (s *Server) updatePersistedConfigLocked(dataDir string, update func(map[string]any)) (map[string]any, error) {
	values := map[string]any{}
	if data, err := os.ReadFile(filepath.Join(dataDir, "config.json")); err == nil {
		_ = json.Unmarshal(data, &values)
	}
	update(values)
	if err := writeAIImageJSONAtomic(filepath.Join(dataDir, "config.json"), values); err != nil {
		return nil, err
	}
	return values, nil
}

func (s *Server) handleAPIConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		BrowserPath           string `json:"browserPath"`
		EgoLitePath           string `json:"egoLitePath"`
		BrowserEngine         string `json:"browserEngine"`
		MaxConcurrentBrowsers *int   `json:"maxConcurrentBrowsers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	if req.BrowserPath != "" {
		if browser.NormalizeEngine(req.BrowserEngine) == browser.EngineEgoLite || isRetiredEgoLitePath(req.BrowserPath) {
			http.Error(w, "ego lite 已取消，请选择 Chromium、BrowserOS 或 Google Chrome", http.StatusBadRequest)
			return
		}
		if _, err := os.Stat(req.BrowserPath); err != nil {
			http.Error(w, fmt.Sprintf("浏览器路径不存在: %v", err), 400)
			return
		}
	}
	if req.EgoLitePath != "" || browser.NormalizeEngine(req.BrowserEngine) == browser.EngineEgoLite {
		http.Error(w, "ego lite 已取消，请选择 Chromium、BrowserOS 或 Google Chrome", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	maxConcurrent := s.cfg.MaxConcurrentBrowsers
	if req.MaxConcurrentBrowsers != nil {
		maxConcurrent = *req.MaxConcurrentBrowsers
		if maxConcurrent < 0 {
			maxConcurrent = 0
		}
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	configData, err := s.updatePersistedConfigLocked(dataDir, func(configData map[string]any) {
		configData["browser_path"] = req.BrowserPath
		configData["max_concurrent_browsers"] = maxConcurrent
		delete(configData, "ego_lite_path")
		delete(configData, "auto_collect")
		delete(configData, "collect_interval_seconds")
		if req.BrowserEngine != "" {
			configData["browser_engine"] = req.BrowserEngine
		}
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	s.cfg.BrowserPath = req.BrowserPath
	s.cfg.MaxConcurrentBrowsers = maxConcurrent
	if req.BrowserEngine != "" {
		s.cfg.BrowserEngine = req.BrowserEngine
	} else if v, ok := configData["browser_engine"].(string); ok {
		s.cfg.BrowserEngine = v
	}
	if s.facade != nil {
		s.facade.SetBrowserPath(req.BrowserPath)
		s.facade.SetEgoLitePath(s.cfg.EgoLitePath)
		s.facade.SetDefaultEngine(s.cfg.BrowserEngine)
		s.facade.SetMaxConcurrentBrowsers(maxConcurrent)
	}
	maxOut := maxConcurrent
	if s.facade != nil {
		maxOut = s.facade.MaxConcurrentBrowsers()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":                    true,
		"maxConcurrentBrowsers": maxOut,
		"browserPath":           req.BrowserPath,
		"browsers":              browser.DetectLocalBrowsers(),
	})
}

func (s *Server) handleAPIBrowserLayout(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	if s.facade == nil {
		http.Error(w, "facade not initialized", 500)
		return
	}
	if err := s.facade.Supervisor().ArrangeWindows("grid"); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *Server) handleAPISetEngine(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		AccountID int64  `json:"accountId"`
		Engine    string `json:"engine"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.AccountID <= 0 {
		http.Error(w, "accountId required", 400)
		return
	}
	engine := browser.NormalizeEngine(req.Engine)
	if engine == "" || engine == browser.EngineEgoLite {
		engine = browser.EngineChromium
	}
	if err := s.db.UpdateBrowserProfileEngine(req.AccountID, engine); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "accountId": req.AccountID, "engine": engine})
}

// ServeHTTP implements the http.Handler interface for Server.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "http://127.0.0.1:3080" || origin == "http://localhost:3080" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.withHTTPRecovery(s.mux).ServeHTTP(w, r)
}
