package radar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tikhub"
	"alpha-nexus-egolite/internal/trace"
)

// CreatorSyncMaxPages bounds one unattended full pull. Douyin returns 20 posts
// per page, so this allows ~4000 posts before the caller must opt in again;
// without a cap a runaway cursor could spend unbounded API credit.
const CreatorSyncMaxPages = 200

// CreatorSyncOptions controls one full creator archival pass.
type CreatorSyncOptions struct {
	// MaxPages caps this pass. Zero means "until the platform says has_more=0",
	// still bounded by CreatorSyncMaxPages.
	MaxPages int
	PageSize int
	SortType int
	// Media downloads covers and audio into the creator folder.
	Media   bool
	Workers int
	// MediaOnly skips the list fetch and only re-downloads the covers/audio that
	// are still missing from the existing index. Retrying one failed cover this
	// way costs zero TikHub credit instead of a full six-page refetch.
	MediaOnly bool
	// OnProgress reports coarse progress for NDJSON / UI streaming.
	OnProgress func(stage string, done, total int)
}

// CreatorSyncResult summarizes one archival pass.
type CreatorSyncResult struct {
	Folder    string `json:"folder"`
	IndexPath string `json:"index_path"`
	Pages     int    `json:"pages"`
	// Posts is the number of posts the archive now holds. It can exceed Fetched
	// when a limited run (page cap / stalled cursor) is merged into history.
	Posts int `json:"posts"`
	// Fetched is how many posts this pass actually saw. A value well below Posts
	// means the run only reached one end of the list, which is why the archive
	// keeps the older records instead of dropping them.
	Fetched  int                       `json:"fetched"`
	NewPosts int                       `json:"new_posts"`
	Media    tikhub.CreatorMediaResult `json:"media"`
	CallIDs  []int64                   `json:"call_ids"`
	Stopped  string                    `json:"stopped,omitempty"`
}

// CreatorIndex is the on-disk manifest that makes a creator folder
// self-describing: it lists every archived post and its local media paths.
type CreatorIndex struct {
	SchemaVersion int                       `json:"schema_version"`
	Platform      string                    `json:"platform"`
	UserID        string                    `json:"user_id"`
	Name          string                    `json:"name,omitempty"`
	SyncedAt      string                    `json:"synced_at"`
	Posts         []tikhub.CreatorPostMedia `json:"posts"`
}

func creatorIndexPath(dataDir, platform, userID string) string {
	return filepath.Join(tikhub.CreatorFolder(dataDir, platform, userID), "index.json")
}

// LoadCreatorIndex reads the manifest, returning an empty index when the creator
// has never been synced so callers can treat "first run" as a normal case.
func LoadCreatorIndex(dataDir, platform, userID string) (CreatorIndex, error) {
	empty := CreatorIndex{SchemaVersion: 1, Platform: platform, UserID: userID, Posts: []tikhub.CreatorPostMedia{}}
	raw, err := os.ReadFile(creatorIndexPath(dataDir, platform, userID))
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var index CreatorIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		// A corrupt manifest must not block re-syncing; the snapshots remain.
		return empty, nil
	}
	if index.Posts == nil {
		index.Posts = []tikhub.CreatorPostMedia{}
	}
	return index, nil
}

func saveCreatorIndex(dataDir, platform, userID, name string, posts []tikhub.CreatorPostMedia) (string, error) {
	root := tikhub.CreatorFolder(dataDir, platform, userID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	index := CreatorIndex{
		SchemaVersion: 1, Platform: platform, UserID: userID, Name: name,
		SyncedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Posts:    posts,
	}
	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, "index.json")
	temp, err := os.CreateTemp(root, ".index-*.tmp")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return "", err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, target); err != nil {
		return "", err
	}
	return target, nil
}

// creatorSyncClientFactory builds the provider client for one archive pass.
// It is a package-private seam: tests replace it to point the client at a local
// httptest server, so no test ever spends real TikHub credit.
var creatorSyncClientFactory = func(dataDir string) (*tikhub.Client, error) {
	client, _, err := tikhub.NewActiveClient(dataDir)
	return client, err
}

// creatorEndpointKey builds the curated endpoint id for one creator sync step.
// The platform comes from the creator itself, so an xhs creator can never be
// served by a douyin endpoint, and a missing definition is an error rather than
// a silent fallback to another platform.
func creatorEndpointKey(platform, capability string) (string, error) {
	if !supportsCreatorAutoArchive(platform) {
		return "", fmt.Errorf("unsupported creator sync platform %q", platform)
	}
	return platform + "." + capability, nil
}

// creatorEndpoint resolves one curated endpoint for a creator sync.
func creatorEndpoint(platform, capability string) (tikhub.EndpointDefinition, error) {
	key, err := creatorEndpointKey(platform, capability)
	if err != nil {
		return tikhub.EndpointDefinition{}, err
	}
	endpoint, ok := tikhub.FindEndpoint(key)
	if !ok {
		return tikhub.EndpointDefinition{}, fmt.Errorf("endpoint %s is unavailable", key)
	}
	return endpoint, nil
}

// creatorUserIDParamName is the single place that maps a platform to the request
// parameter carrying its creator identifier: douyin endpoints take sec_user_id,
// xhs endpoints take user_id. Both read the same WatchUser.UserID, so the
// identifier source stays one field while the wire name stays platform-correct.
func creatorUserIDParamName(platform string) (string, error) {
	switch platform {
	case "douyin":
		return "sec_user_id", nil
	case "xhs":
		return "user_id", nil
	default:
		return "", fmt.Errorf("unsupported creator sync platform %q", platform)
	}
}

// creatorEndpointParams starts from the endpoint's own fixed parameters and adds
// the creator identifier under the name the definition declares. Copying
// FixedParams keeps the request equal to what the definition describes
// (xhs.creator_posts needs its empty cursor) without inventing a parameter the
// definition does not list.
func creatorEndpointParams(endpoint tikhub.EndpointDefinition, extra map[string]any) map[string]any {
	params := make(map[string]any, len(endpoint.FixedParams)+len(extra))
	for name, value := range endpoint.FixedParams {
		params[name] = value
	}
	for name, value := range extra {
		params[name] = value
	}
	return params
}

// saveCreatorFollowerSnapshot stores one verified follower total for the
// creator's own account. The account is resolved by platform identifier; a
// creator without one has no metric identity, so this returns an error and
// writes nothing rather than inventing an account row.
func saveCreatorFollowerSnapshot(database *db.DB, user tikhub.WatchUser, followers int64, collectedAt time.Time) error {
	accountID, err := database.CreatorMetricAccountID(user.Platform, user.UserID)
	if err != nil {
		return err
	}
	collectedAt = collectedAt.UTC()
	// window_days 7 is the schema default and the only value the snapshot
	// validator accepts, so a same-day row UPSERTs onto the existing key
	// (including an earlier REVIEWED_ANALYTICS_DOM placeholder) instead of
	// creating a second identity for the same creator and day.
	_, err = database.UpsertCreatorMetricSnapshot(db.CreatorMetricSnapshotInput{
		AccountID:         accountID,
		PlatformCode:      user.Platform,
		SnapshotDate:      collectedAt.Format("2006-01-02"),
		WindowDays:        7,
		RangeVerified:     false,
		Followers:         followers,
		FollowersVerified: true,
		EvidenceCode:      db.CreatorProfileEvidenceCode,
		CollectedAt:       collectedAt.Format(time.RFC3339),
	}, nil)
	return err
}

// SyncCreatorAll pulls every page of a creator's posts, archives the raw
// snapshots, downloads covers/audio, and writes the folder manifest. It is the
// one-shot "grab everything" path behind both the UI button and the CLI.
func SyncCreatorAll(
	ctx context.Context,
	database *db.DB,
	dataDir string,
	user tikhub.WatchUser,
	options CreatorSyncOptions,
) (CreatorSyncResult, error) {
	tikhub.ApplyWatchDefaultsForSync(&user)
	result := CreatorSyncResult{CallIDs: []int64{}}
	progress := CreatorArchiveProgress{Phase: "posts", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if options.OnProgress != nil {
		_ = SaveCreatorArchiveProgress(dataDir, user.Platform, user.UserID, progress)
		defer ClearCreatorArchiveProgress(dataDir, user.Platform, user.UserID)
	}
	archiveErr := func(err error) error {
		if options.OnProgress != nil {
			progress.Phase = "error"
			progress.Error = err.Error()
			_ = SaveCreatorArchiveProgress(dataDir, user.Platform, user.UserID, progress)
		}
		return err
	}
	if !supportsCreatorAutoArchive(user.Platform) {
		return result, archiveErr(fmt.Errorf("full archive currently supports douyin and xhs creators only"))
	}
	root, err := tikhub.EnsureCreatorFolder(dataDir, user)
	if err != nil {
		return result, archiveErr(err)
	}
	result.Folder = root

	// 只补媒体：完全不碰 provider，只按已落盘的索引把缺失的封面/音频补齐。
	// 这是「重试失败项」的路径——补 1 张封面不该再花 6 次列表调用。
	if options.MediaOnly {
		return syncCreatorMediaOnly(ctx, dataDir, user, options, result, progress, archiveErr)
	}

	postsEndpoint, err := creatorEndpoint(user.Platform, "creator_posts")
	if err != nil {
		return result, archiveErr(err)
	}
	profileEndpoint, err := creatorEndpoint(user.Platform, "creator_profile")
	if err != nil {
		return result, archiveErr(err)
	}
	userIDParam, err := creatorUserIDParamName(user.Platform)
	if err != nil {
		return result, archiveErr(err)
	}
	provider, err := tikhub.LoadProviderConfig(dataDir)
	if err != nil || !provider.Active || provider.TestStatus != tikhub.TestStatusPassed {
		return result, archiveErr(fmt.Errorf("provider inactive"))
	}
	client, err := creatorSyncClientFactory(dataDir)
	if err != nil {
		return result, archiveErr(err)
	}
	result.Folder = root

	// 先取资料：粉丝数是本轮最缺的数据。资料失败会作为错误返回，但不阻断下面
	// 的作品同步；作品失败也不会抹掉这里已经落库的粉丝快照。
	profileFailure := func() error {
		profileResponse, callID, callErr := callCreatorEndpointWithRetry(ctx, database, dataDir, client, profileEndpoint,
			creatorEndpointParams(profileEndpoint, map[string]any{userIDParam: user.UserID}), nil)
		result.CallIDs = append(result.CallIDs, callID)
		if callErr != nil {
			return fmt.Errorf("creator profile request failed: %w", callErr)
		}
		followers, ok := tikhub.CreatorFollowerCount(profileResponse, user.Platform)
		if !ok {
			return fmt.Errorf("creator profile response carries no verified follower count")
		}
		if err := saveCreatorFollowerSnapshot(database, user, followers, time.Now()); err != nil {
			return err
		}
		return nil
	}()

	pageSize := options.PageSize
	if pageSize <= 0 || pageSize > 20 {
		pageSize = 20
	}
	pageLimit := options.MaxPages
	if pageLimit <= 0 || pageLimit > CreatorSyncMaxPages {
		pageLimit = CreatorSyncMaxPages
	}

	seen := map[string]bool{}
	collected := make([]tikhub.CreatorPostMedia, 0)
	cursor := "0"
	var postsFailure error
	for page := 0; page < pageLimit; page++ {
		if err := ctx.Err(); err != nil {
			result.Stopped = "cancelled"
			break
		}
		params := creatorEndpointParams(postsEndpoint, map[string]any{userIDParam: user.UserID})
		if user.Platform == "douyin" {
			// douyin.creator_posts 的四个参数名以 internal/tikhub/endpoints.go 为准。
			params["max_cursor"] = cursor
			params["count"] = fmt.Sprint(pageSize)
			params["sort_type"] = fmt.Sprint(options.SortType)
		}
		response, callID, callErr := callCreatorEndpointWithRetry(ctx, database, dataDir, client, postsEndpoint, params,
			func(attempt int, reason string) {
				if options.OnProgress != nil {
					options.OnProgress("retry", attempt, 0)
				}
			})
		if callErr != nil {
			if page == 0 {
				// 不再提前返回：资料已经落库，作品失败只作为错误上报。
				postsFailure = callErr
				break
			}
			// Keep what we already archived instead of discarding a long pull.
			result.Stopped = "request_failed"
			break
		}
		result.CallIDs = append(result.CallIDs, callID)
		result.Pages++

		batch := tikhub.ExtractCreatorPostMedia(response)
		for _, item := range batch {
			if seen[item.AwemeID] {
				continue
			}
			seen[item.AwemeID] = true
			collected = append(collected, item)
		}
		if options.OnProgress != nil {
			options.OnProgress("posts", len(collected), 0)
			progress.PostsFetched = len(collected)
			progress.PostsTotalEstimate = len(collected)
			progress.Phase = "posts"
			_ = SaveCreatorArchiveProgress(dataDir, user.Platform, user.UserID, progress)
		}

		if user.Platform != "douyin" {
			// 非抖音平台的分页游标没有页级字段（真实 xhs 响应只在每条笔记上带
			// cursor），本周期不猜参数：每轮只取第一页，并如实标注是否还有更多。
			if tikhub.CreatorPostsHasMore(response, user.Platform) {
				result.Stopped = "paging_unavailable"
			} else {
				result.Stopped = "end_of_list"
			}
			break
		}
		next, more := tikhub.CreatorPostsPaging(response)
		if !more {
			result.Stopped = "end_of_list"
			break
		}
		if next == "" || next == cursor {
			result.Stopped = "cursor_stalled"
			break
		}
		cursor = next
	}
	if result.Stopped == "" {
		result.Stopped = "page_limit"
	}
	if postsFailure != nil {
		result.Stopped = "request_failed"
	}

	tikhub.SortCreatorMediaNewestFirst(collected)
	result.Fetched = len(collected)

	previous, err := LoadCreatorIndex(dataDir, user.Platform, user.UserID)
	if err != nil {
		return result, archiveErr(err)
	}
	known := make(map[string]bool, len(previous.Posts))
	for _, post := range previous.Posts {
		known[post.AwemeID] = true
	}
	for _, post := range collected {
		if !known[post.AwemeID] {
			result.NewPosts++
		}
	}

	if options.Media {
		mediaResult, mediaErr := tikhub.DownloadCreatorMedia(ctx, dataDir, user.Platform, user.UserID, collected, tikhub.CreatorMediaOptions{
			Workers: options.Workers, Covers: true, Audio: true,
			OnProgress: func(done, total int) {
				if options.OnProgress != nil {
					options.OnProgress("media", done, total)
					progress.Phase = "covers"
					progress.CoversDone = done
					progress.CoversTotal = total
					progress.AudioDone = done
					progress.AudioTotal = total
					_ = SaveCreatorArchiveProgress(dataDir, user.Platform, user.UserID, progress)
				}
			},
		})
		if mediaErr != nil {
			return result, archiveErr(mediaErr)
		}
		result.Media = mediaResult
	}

	// The failure ledger mirrors this run: it is replaced only when the media
	// stage ran, so a skip_media archive cannot erase earlier failures. The
	// collected posts carry title and cover path, which the ledger copies onto
	// each failure so the UI can name the post instead of only its id.
	if err := persistCreatorArchiveFailures(dataDir, user.Platform, user.UserID, options.Media, result.Media, collected); err != nil {
		return result, archiveErr(err)
	}

	// 索引是本地归档的唯一账本，不能因为这次只看到列表的一头就删掉旧记录：
	// --max-pages、分页提前 cursor_stalled、某页重试耗尽都会让 collected 变短，
	// 而那些作品的文件其实还在 media/ 里。本次拿到的用新数据，没见到的保留。
	merged := mergeCreatorIndexPosts(collected, previous.Posts)
	result.Posts = len(merged)
	indexPath, err := saveCreatorIndex(dataDir, user.Platform, user.UserID, user.Name, merged)
	if err != nil {
		return result, err
	}
	result.IndexPath = indexPath

	// 资料与作品各自失败都不阻断另一项：两项都已经尽力归档之后，才把错误交回
	// 调用方（自动归档据此记录失败并沿用 24 小时节流）。
	if profileFailure != nil {
		if postsFailure != nil {
			return result, archiveErr(fmt.Errorf("%w; creator posts also failed: %v", profileFailure, postsFailure))
		}
		return result, archiveErr(profileFailure)
	}
	if postsFailure != nil {
		return result, archiveErr(postsFailure)
	}
	return result, nil
}

// syncCreatorMediaOnly re-downloads the media that is still missing from an
// existing archive. It never calls the provider: the index already holds every
// post and its source URL, so retrying a failed cover costs no TikHub credit.
func syncCreatorMediaOnly(
	ctx context.Context,
	dataDir string,
	user tikhub.WatchUser,
	options CreatorSyncOptions,
	result CreatorSyncResult,
	progress CreatorArchiveProgress,
	archiveErr func(error) error,
) (CreatorSyncResult, error) {
	index, err := LoadCreatorIndex(dataDir, user.Platform, user.UserID)
	if err != nil {
		return result, archiveErr(err)
	}
	if len(index.Posts) == 0 {
		return result, archiveErr(fmt.Errorf("本地还没有作品索引，请先完整归档一次"))
	}
	result.Posts = len(index.Posts)
	result.Stopped = "media_only"

	progress.Phase = "media"
	progress.CoversTotal = len(index.Posts)
	progress.AudioTotal = len(index.Posts)
	if options.OnProgress != nil {
		_ = SaveCreatorArchiveProgress(dataDir, user.Platform, user.UserID, progress)
	}

	mediaResult, err := tikhub.DownloadCreatorMedia(ctx, dataDir, user.Platform, user.UserID, index.Posts, tikhub.CreatorMediaOptions{
		Workers: options.Workers, Covers: true, Audio: true,
		OnProgress: func(done, total int) {
			if options.OnProgress == nil {
				return
			}
			options.OnProgress("media", done, total)
			progress.Phase = "media"
			progress.CoversDone = done
			progress.CoversTotal = total
			progress.AudioDone = done
			progress.AudioTotal = total
			_ = SaveCreatorArchiveProgress(dataDir, user.Platform, user.UserID, progress)
		},
	})
	if err != nil {
		return result, archiveErr(err)
	}
	result.Media = mediaResult

	if err := persistCreatorArchiveFailures(dataDir, user.Platform, user.UserID, true, mediaResult, index.Posts); err != nil {
		return result, archiveErr(err)
	}
	// DownloadCreatorMedia 原地回填了 CoverPath/AudioPath，直接落回索引即可。
	indexPath, err := saveCreatorIndex(dataDir, user.Platform, user.UserID, user.Name, index.Posts)
	if err != nil {
		return result, archiveErr(err)
	}
	result.IndexPath = indexPath
	return result, nil
}

// mergeCreatorIndexPosts keeps archived history when a run only sees one end of
// the list. Posts fetched this pass win (fresh metrics and media paths); posts the
// pass did not reach keep their previous record instead of disappearing from the
// local archive. Pruning abandoned posts is a separate, explicit action.
func mergeCreatorIndexPosts(fresh, previous []tikhub.CreatorPostMedia) []tikhub.CreatorPostMedia {
	if len(previous) == 0 {
		return fresh
	}
	seen := make(map[string]bool, len(fresh))
	merged := make([]tikhub.CreatorPostMedia, 0, len(fresh)+len(previous))
	for _, post := range fresh {
		if post.AwemeID != "" {
			seen[post.AwemeID] = true
		}
		merged = append(merged, post)
	}
	for _, post := range previous {
		if post.AwemeID == "" || seen[post.AwemeID] {
			continue
		}
		merged = append(merged, post)
	}
	tikhub.SortCreatorMediaNewestFirst(merged)
	return merged
}

// creatorPageRetries bounds how many extra attempts one page gets. TikHub
// intermittently answers 400 "Request failed. Please retry." for a cursor that
// succeeds moments later, and abandoning the whole archive on that would leave
// the folder silently incomplete.
const creatorPageRetries = 3

// creatorPageBackoff is the base delay between retries; it doubles per attempt.
const creatorPageBackoff = 700 * time.Millisecond

// callCreatorEndpointWithRetry retries only the provider request. Snapshot and
// database writes are not retried because those failures are deterministic.
func callCreatorEndpointWithRetry(
	ctx context.Context,
	database *db.DB,
	dataDir string,
	client *tikhub.Client,
	endpoint tikhub.EndpointDefinition,
	params map[string]any,
	onRetry func(attempt int, reason string),
) (map[string]interface{}, int64, error) {
	var lastErr error
	var lastCallID int64
	for attempt := 0; attempt <= creatorPageRetries; attempt++ {
		if attempt > 0 {
			delay := creatorPageBackoff << (attempt - 1)
			select {
			case <-ctx.Done():
				return nil, lastCallID, ctx.Err()
			case <-time.After(delay):
			}
			if onRetry != nil {
				onRetry(attempt, lastErr.Error())
			}
		}
		response, callID, err := callCreatorEndpoint(ctx, database, dataDir, client, endpoint, params)
		lastCallID = callID
		if err == nil {
			return response, callID, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return nil, callID, err
		}
		if !isRetryableCreatorError(err) {
			return nil, callID, err
		}
	}
	return nil, lastCallID, lastErr
}

// isRetryableCreatorError reports whether the provider failure is worth another
// attempt. Local storage errors and context cancellation are not.
func isRetryableCreatorError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	code, fromProvider := tikhub.ProviderFailureCode(err)
	if !fromProvider {
		// Local write failures are deterministic; retrying only wastes time.
		return false
	}
	switch code {
	case "network_error", "transport_timeout":
		return true
	}
	// Provider responses arrive as http_<status>. Only the statuses that can
	// succeed on a second try are retried:
	//
	//   400 — TikHub answers this transiently for some cursors
	//   429 — rate limited
	//   5xx — upstream failure
	//
	// 401/403/404 are terminal: the credential is wrong, the endpoint is
	// forbidden, or the resource does not exist. Retrying those costs a full
	// paid call per attempt and can never change the outcome.
	return isRetryableHTTPStatus(code)
}

// callCreatorEndpoint performs one archived provider call: it records the call
// row, saves the raw snapshot, and hard-links the snapshot into the creator
// folder for both creator capabilities (profile and posts).
func callCreatorEndpoint(
	ctx context.Context,
	database *db.DB,
	dataDir string,
	client *tikhub.Client,
	endpoint tikhub.EndpointDefinition,
	params map[string]any,
) (map[string]interface{}, int64, error) {
	// 窗口内同端点同参数已经付过费，就直接回放那份快照的内容。
	// 「抓取全部作品」和「更新」是用户会反复点的按钮，也是最贵的调用。
	//
	// creator_profile 不在此列：它是「此刻的关注数」，用户在同一天再点一次
	// 更新，要的正是最新那个数。回放旧快照会让「同一天重复同步保留最新值」
	// 这条约定直接失效（有测试固定）。
	//
	// creator_posts 也不复用：同一次归档里每一页都用同样的参数反复取，
	// 回放上一页会让分页永远停在原地；用户重跑归档时也期待拿到新增的作品。
	if endpoint.Capability != "creator_profile" && endpoint.Capability != "creator_posts" {
		if entry, hit := tikhub.LookupReusableCall(dataDir, endpoint.ID, params, endpoint.Capability, time.Now()); hit {
			if response, readErr := tikhub.ReadSnapshotResponse(dataDir, entry.RawPath); readErr == nil {
				return response, entry.CallID, nil
			}
		}
	}
	parametersJSON, err := json.Marshal(params)
	if err != nil {
		return nil, 0, err
	}
	traceID := string(trace.New())
	call, err := database.CreateTikHubAPICall(db.TikHubAPICallInput{
		TraceID: traceID, PlatformCode: endpoint.Platform, EndpointID: endpoint.ID,
		Capability: endpoint.Capability, Method: endpoint.Method, Path: endpoint.Path,
		ParametersJSON: string(parametersJSON),
	})
	if err != nil {
		return nil, 0, err
	}
	started := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	response, callErr := client.CallPreparedEndpoint(callCtx, endpoint, params)
	duration := time.Since(started).Milliseconds()
	if callErr != nil {
		providerCode, providerMessage := tikhub.SafeRequestFailure(callErr)
		_, _ = database.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
			Status: "failed", ErrorCode: "provider_request_failed", ProviderCode: providerCode,
			ProviderMessage: providerMessage, DurationMS: duration,
		})
		return nil, call.ID, callErr
	}
	rawPath, responseBytes, saveErr := tikhub.SaveCallSnapshot(dataDir, tikhub.CallSnapshot{
		CallID: call.ID, TraceID: traceID, Platform: endpoint.Platform, EndpointID: endpoint.ID,
		Capability: endpoint.Capability, Method: endpoint.Method, Path: endpoint.Path,
		Parameters: params, Response: response,
	})
	if saveErr != nil {
		_, _ = database.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
			Status: "storage_failed", ErrorCode: "snapshot_write_failed", DurationMS: duration,
		})
		return nil, call.ID, saveErr
	}
	if _, err := database.FinishTikHubAPICall(call.ID, db.TikHubAPICallFinish{
		Status: "succeeded", ItemCount: tikhub.CountPrimaryItems(response), ResponseBytes: responseBytes,
		RawPath: rawPath, DurationMS: duration,
	}); err != nil {
		return nil, call.ID, err
	}
	tikhub.RecordReusableCall(dataDir, endpoint.ID, params,
		tikhub.ReuseEntry{CallID: call.ID, RawPath: rawPath}, time.Now())
	_, _ = tikhub.ArchiveCreatorSnapshot(dataDir, rawPath, tikhub.CallSnapshot{
		CallID: call.ID, TraceID: traceID, Platform: endpoint.Platform, EndpointID: endpoint.ID,
		Capability: endpoint.Capability, Method: endpoint.Method, Path: endpoint.Path,
		Parameters: params, Response: response,
	})
	return response, call.ID, nil
}

// isRetryableHTTPStatus 判定一次 provider 的 http_<status> 失败是否值得重试。
// 独立出来是为了能直接测——构造真实的失败需要完整调用链。
//
//	400 — TikHub 对部分 cursor 会短暂返回，下一次可能成功
//	429 — 被限流
//	5xx — 上游故障
//
// 其余（401/403/404）是终态：凭证错、被禁、资源不存在，重试只会再付一次钱。
func isRetryableHTTPStatus(code string) bool {
	status := strings.TrimPrefix(code, "http_")
	if status == code {
		return false
	}
	value, err := strconv.Atoi(status)
	if err != nil {
		return false
	}
	switch {
	case value == http.StatusBadRequest:
		return true
	case value == http.StatusTooManyRequests:
		return true
	case value >= 500:
		return true
	default:
		return false
	}
}
