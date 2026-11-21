package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	apperrors "alpha-nexus-egolite/internal/errors"
	"alpha-nexus-egolite/internal/media"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publish"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/selectors"
	"alpha-nexus-egolite/internal/tikhub"
	"alpha-nexus-egolite/internal/trace"
)

// openDB opens and migrates a fresh connection for one tool call.
//
// Callers own the returned handle and `defer database.Close()` it, so this must
// never hand back a shared connection: a tool that closed a server-owned handle
// would take the whole UI's database down with it.
//
// Migrate is cheap on an already-migrated database (it skips the DDL block and
// only replays the idempotent backfill statements), so the per-call cost here is
// milliseconds rather than the several hundred it used to be.
func openDB(rt *Runtime) (*db.DB, error) {
	dir := dataDir(rt)
	database, err := db.Open(dir)
	if err != nil {
		return nil, err
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

func handleMediaGenerate(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	if rt == nil || !rt.TrustedLocalUI || rt.GenerateMedia == nil {
		return &Result{OK: false, Error: "TRUSTED_LOCAL_UI_REQUIRED"}, fmt.Errorf("TRUSTED_LOCAL_UI_REQUIRED")
	}
	if !argBool(args, "confirm", false) {
		return &Result{OK: false, Error: "CONFIRMATION_REQUIRED"}, fmt.Errorf("CONFIRMATION_REQUIRED")
	}
	requestID := strings.TrimSpace(argString(args, "request_id", ""))
	kind := strings.TrimSpace(argString(args, "kind", ""))
	prompt := strings.TrimSpace(argString(args, "prompt", ""))
	durationSeconds, durationOK := generationDurationSeconds(args["duration_seconds"])
	if len(requestID) < 16 || len(requestID) > 128 || (kind != "image" && kind != "video") || prompt == "" || !durationOK ||
		(kind == "image" && durationSeconds != 0) || (kind == "video" && durationSeconds != 0 && durationSeconds != 5 && durationSeconds != 10) {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("VALIDATION_FAILED")
	}
	return rt.GenerateMedia(ctx, args)
}

func generationDurationSeconds(value interface{}) (int, bool) {
	if value == nil {
		return 0, true
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), int64(int(typed)) == typed
	case float64:
		value := int(typed)
		return value, typed == float64(value)
	case json.Number:
		value, err := typed.Int64()
		return int(value), err == nil && int64(int(value)) == value
	default:
		return 0, false
	}
}

func handleMediaGenerateBatch(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	if rt == nil || !rt.TrustedLocalUI || rt.GenerateMedia == nil {
		return &Result{OK: false, Error: "TRUSTED_LOCAL_UI_REQUIRED"}, fmt.Errorf("TRUSTED_LOCAL_UI_REQUIRED")
	}
	if !argBool(args, "confirm", false) {
		return &Result{OK: false, Error: "CONFIRMATION_REQUIRED"}, fmt.Errorf("CONFIRMATION_REQUIRED")
	}
	requestID := strings.TrimSpace(argString(args, "request_id", ""))
	kind := strings.TrimSpace(argString(args, "kind", ""))
	prompt := strings.TrimSpace(argString(args, "prompt", ""))
	count := argInt(args, "count", 1)
	if len(requestID) < 16 || len(requestID) > 124 || (kind != "image" && kind != "video") || prompt == "" || count < 1 || count > 8 {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("VALIDATION_FAILED")
	}
	jobIDs := make([]int64, 0, count)
	for index := 1; index <= count; index++ {
		itemArgs := make(map[string]interface{}, len(args))
		for key, value := range args {
			itemArgs[key] = value
		}
		itemArgs["request_id"] = fmt.Sprintf("%s-%02d", requestID, index)
		result, err := handleMediaGenerate(ctx, rt, itemArgs)
		if err != nil || result == nil || !result.OK {
			code := "GENERATION_QUEUE_FAILED"
			if result != nil && result.Error != "" {
				code = result.Error
			}
			return &Result{
				OK: false, Error: code,
				Data: map[string]interface{}{"job_ids": jobIDs, "failed_index": index},
			}, err
		}
		jobID, ok := result.Data["job_id"].(int64)
		if !ok || jobID <= 0 {
			return &Result{
				OK: false, Error: "GENERATION_QUEUE_FAILED",
				Data: map[string]interface{}{"job_ids": jobIDs, "failed_index": index},
			}, fmt.Errorf("GENERATION_QUEUE_FAILED")
		}
		jobIDs = append(jobIDs, jobID)
	}
	return okResult(fmt.Sprintf("%d generation jobs queued", len(jobIDs)), map[string]interface{}{
		"job_ids": jobIDs, "count": len(jobIDs),
	}), nil
}

func handleAccountList(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()

	platform := argString(args, "platform", "")
	group := argString(args, "group", "")
	tagIDs, parseErr := argInt64List(args["tag_ids"])
	if parseErr != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, parseErr
	}
	var accounts []db.Account
	switch {
	case len(tagIDs) > 0:
		accounts, err = database.ListAccountsByTagIDs(tagIDs)
	case platform != "":
		accounts, err = database.ListAccountsByPlatform(platform)
	case group != "":
		accounts, err = database.ListAccountsByGroup(group)
	default:
		accounts, err = database.ListAccounts()
	}
	if err != nil {
		return nil, err
	}
	tagsByAccount, err := database.ListAccountTagsByAccount()
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]interface{}, 0, len(accounts))
	for _, a := range accounts {
		if platform != "" && a.PlatformCode != platform {
			continue
		}
		rows = append(rows, map[string]interface{}{
			"id": a.ID, "platform": a.PlatformCode, "name": a.Name,
			"login_state": a.LoginState, "group": a.AccountGroup, "remark": a.Remark,
			"tags": tagsByAccount[a.ID],
		})
	}
	return okResult(fmt.Sprintf("%d accounts", len(rows)), map[string]interface{}{
		"count": len(rows), "accounts": rows,
	}), nil
}

func argInt64List(value interface{}) ([]int64, error) {
	if value == nil {
		return nil, nil
	}
	var raw []interface{}
	switch typed := value.(type) {
	case []interface{}:
		raw = typed
	case []int64:
		result := append([]int64(nil), typed...)
		return result, nil
	case []int:
		result := make([]int64, len(typed))
		for index, item := range typed {
			result[index] = int64(item)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("ids must be an array")
	}
	result := make([]int64, 0, len(raw))
	for _, item := range raw {
		switch number := item.(type) {
		case float64:
			converted := int64(number)
			if number != float64(converted) {
				return nil, fmt.Errorf("ids must contain integers")
			}
			result = append(result, converted)
		case int:
			result = append(result, int64(number))
		case int64:
			result = append(result, number)
		default:
			return nil, fmt.Errorf("ids must contain integers")
		}
	}
	return result, nil
}

func accountTagData(tag *db.AccountTag) map[string]interface{} {
	if tag == nil {
		return nil
	}
	return map[string]interface{}{
		"id": tag.ID, "name": tag.Name, "color": tag.Color, "position": tag.Position, "accountCount": tag.AccountCount,
	}
}

func handleAccountTagList(_ context.Context, rt *Runtime, _ map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	tags, err := database.ListAccountTags()
	if err != nil {
		return nil, err
	}
	return okResult(fmt.Sprintf("%d account tags", len(tags)), map[string]interface{}{"count": len(tags), "tags": tags}), nil
}

func handleAccountTagCreate(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	tag, err := database.UpsertAccountTag(argString(args, "name", ""), argString(args, "color", ""))
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	return okResult("account tag saved", map[string]interface{}{"tag": accountTagData(tag)}), nil
}

func handleAccountTagUpdate(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	accountIDs, err := argInt64List(args["account_ids"])
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	tagIDs, err := argInt64List(args["tag_ids"])
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	mode := strings.TrimSpace(argString(args, "mode", ""))
	if err := database.BulkUpdateAccountTags(accountIDs, tagIDs, mode); err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	return okResult("account tags updated", map[string]interface{}{
		"account_count": len(accountIDs), "tag_count": len(tagIDs), "mode": mode,
	}), nil
}

func handleAccountTagReorder(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	tagIDs, err := argInt64List(args["tag_ids"])
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	if err := database.ReorderAccountTags(tagIDs); err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	return okResult("account tags reordered", map[string]interface{}{"tag_count": len(tagIDs)}), nil
}

func handleAccountTagResolve(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	tagIDs, err := argInt64List(args["tag_ids"])
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	accounts, err := database.ListAccountsByTagIDs(tagIDs)
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	platform := strings.TrimSpace(argString(args, "platform", ""))
	rows := make([]map[string]interface{}, 0, len(accounts))
	for _, account := range accounts {
		if platform != "" && account.PlatformCode != platform {
			continue
		}
		rows = append(rows, map[string]interface{}{
			"id": account.ID, "platform": account.PlatformCode, "name": account.Name,
		})
	}
	return okResult(fmt.Sprintf("%d matching accounts", len(rows)), map[string]interface{}{"count": len(rows), "accounts": rows}), nil
}

func handleContentVariantUpsert(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	variant, err := database.UpsertContentVariant(db.ContentVariantInput{
		ContentID: int64(argInt(args, "content_id", 0)), PlatformCode: argString(args, "platform", ""),
		AccountID: int64(argInt(args, "account_id", 0)), Title: argString(args, "title", ""),
		Caption: argString(args, "caption", ""), Body: argString(args, "body", ""),
		TagsJSON: argString(args, "tags_json", "[]"), Source: "agent",
	})
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	return okResult("content variant saved", map[string]interface{}{"variant": variant}), nil
}

func decodePublishPlanInput(args map[string]interface{}) (publishplan.FreezeInput, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return publishplan.FreezeInput{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var input publishplan.FreezeInput
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	return input, nil
}

func handlePublishPlanFreeze(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	input, err := decodePublishPlanInput(args)
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	if rt != nil && rt.FreezePublishPlan != nil {
		return rt.FreezePublishPlan(ctx, input)
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	plan, err := (&publishplan.Service{DB: database}).Freeze(input)
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	return okResult("publish plan frozen", map[string]interface{}{"plan": plan}), nil
}

func handlePublishPlanGet(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	planID := int64(argInt(args, "plan_id", 0))
	if planID <= 0 {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("plan_id required")
	}
	if rt != nil && rt.GetPublishPlan != nil {
		return rt.GetPublishPlan(ctx, planID)
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	plan, err := (&publishplan.Service{DB: database}).Get(planID)
	if err != nil {
		return &Result{OK: false, Error: "PUBLISH_PLAN_NOT_FOUND"}, err
	}
	return okResult("publish plan", map[string]interface{}{"plan": plan}), nil
}

func handlePublishPlanList(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	plans, err := (&publishplan.Service{DB: database}).ListSummaries(argInt(args, "limit", 10))
	if err != nil {
		return &Result{OK: false, Error: "PUBLISH_PLAN_LIST_FAILED"}, err
	}
	return okResult("publish plans", map[string]interface{}{"plans": plans}), nil
}

func handlePublishPlanInspect(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	if rt == nil || !rt.TrustedLocalUI || rt.InspectPublishPlan == nil {
		return &Result{OK: false, Error: "TRUSTED_LOCAL_UI_REQUIRED"}, fmt.Errorf("TRUSTED_LOCAL_UI_REQUIRED")
	}
	planID := int64(argInt(args, "plan_id", 0))
	if planID <= 0 {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("plan_id required")
	}
	return rt.InspectPublishPlan(ctx, planID)
}

func handlePublishPlanInspectComposer(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	if rt == nil || !rt.TrustedLocalUI || rt.InspectPublishPlanComposer == nil {
		return &Result{OK: false, Error: "TRUSTED_LOCAL_UI_REQUIRED"}, fmt.Errorf("TRUSTED_LOCAL_UI_REQUIRED")
	}
	planID := int64(argInt(args, "plan_id", 0))
	if planID <= 0 {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("plan_id required")
	}
	return rt.InspectPublishPlanComposer(ctx, planID)
}

func publishPlanInspectionErrorCode(err error) string {
	if err == nil {
		return ""
	}
	code := strings.TrimSpace(err.Error())
	if len(code) <= 80 && strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == "" {
		return code
	}
	return "PUBLISH_PLAN_INSPECTION_FAILED"
}

func handlePublishContentDelete(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	if rt == nil || !rt.TrustedLocalUI || rt.DeletePublishContent == nil {
		return &Result{OK: false, Error: "TRUSTED_LOCAL_UI_REQUIRED"}, fmt.Errorf("TRUSTED_LOCAL_UI_REQUIRED")
	}
	recordID := int64(argInt(args, "record_id", 0))
	if recordID <= 0 || !argBool(args, "confirm_permanent_delete", false) {
		return &Result{OK: false, Error: "DELETE_CONFIRMATION_REQUIRED"}, fmt.Errorf("DELETE_CONFIRMATION_REQUIRED")
	}
	return rt.DeletePublishContent(ctx, recordID)
}

func handlePublishPlanRevealSettings(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	if rt == nil || !rt.TrustedLocalUI || rt.RevealPublishPlanSettings == nil {
		return &Result{OK: false, Error: "TRUSTED_LOCAL_UI_REQUIRED"}, fmt.Errorf("TRUSTED_LOCAL_UI_REQUIRED")
	}
	planID := int64(argInt(args, "plan_id", 0))
	if planID <= 0 {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("plan_id required")
	}
	return rt.RevealPublishPlanSettings(ctx, planID)
}

func handlePublishPlanResumeTopics(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	if rt == nil || !rt.TrustedLocalUI || rt.ResumePublishPlanTopics == nil {
		return &Result{OK: false, Error: "TRUSTED_LOCAL_UI_REQUIRED"}, fmt.Errorf("TRUSTED_LOCAL_UI_REQUIRED")
	}
	planID := int64(argInt(args, "plan_id", 0))
	if planID <= 0 {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("plan_id required")
	}
	return rt.ResumePublishPlanTopics(ctx, planID)
}

func handlePublishPlanPrepare(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	if rt == nil || !rt.TrustedLocalUI || rt.PreparePublishPlan == nil {
		return &Result{OK: false, Error: "TRUSTED_LOCAL_UI_REQUIRED"}, fmt.Errorf("TRUSTED_LOCAL_UI_REQUIRED")
	}
	if argInt(args, "plan_id", 0) <= 0 || strings.TrimSpace(argString(args, "snapshot_hash", "")) == "" ||
		strings.TrimSpace(argString(args, "approval_id", "")) == "" || strings.TrimSpace(argString(args, "nonce", "")) == "" {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("VALIDATION_FAILED")
	}
	return rt.PreparePublishPlan(ctx, int64(argInt(args, "plan_id", 0)), argString(args, "snapshot_hash", ""), argString(args, "approval_id", ""), argString(args, "nonce", ""))
}

func handlePublishPlanSubmit(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	if rt == nil || !rt.TrustedLocalUI || rt.SubmitPublishPlan == nil {
		return &Result{OK: false, Error: "TRUSTED_LOCAL_UI_REQUIRED"}, fmt.Errorf("TRUSTED_LOCAL_UI_REQUIRED")
	}
	if argInt(args, "plan_id", 0) <= 0 || strings.TrimSpace(argString(args, "snapshot_hash", "")) == "" ||
		strings.TrimSpace(argString(args, "approval_id", "")) == "" || strings.TrimSpace(argString(args, "nonce", "")) == "" {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("VALIDATION_FAILED")
	}
	return rt.SubmitPublishPlan(ctx, int64(argInt(args, "plan_id", 0)), argString(args, "snapshot_hash", ""), argString(args, "approval_id", ""), argString(args, "nonce", ""))
}

// handleAccountCheck is retained only for source compatibility. It is not
// registered in the EgoLite product; login verification goes through the
// reviewed ego.space/account tools so a caller can never fall back to CDP.
func handleAccountCheck(_ context.Context, _ *Runtime, args map[string]interface{}) (*Result, error) {
	accountID := int64(argInt(args, "account_id", 0))
	if accountID <= 0 {
		return &Result{OK: false, Error: "MISSING_PARAM:account_id"}, fmt.Errorf("account_id required")
	}
	return &Result{
		OK: false, Error: "EGO_RUNTIME_REQUIRED",
		Data: map[string]interface{}{"account_id": accountID},
	}, fmt.Errorf("EGO_RUNTIME_REQUIRED: use social.account-check")
}

const metaBusinessSuiteURL = "https://business.facebook.com"

func isMetaPlatform(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "meta", "facebook", "instagram":
		return true
	default:
		return false
	}
}

// resolveMetaAccountBrowser is a fail-closed compatibility shim. Meta now uses
// the account's Ego Space and never resolves a CDP endpoint.
func resolveMetaAccountBrowser(_ *Runtime, accountID int64, _ string) (*db.DB, *db.Account, string, func(), *Result, error) {
	if accountID <= 0 {
		return nil, nil, "", nil, &Result{OK: false, Error: "MISSING_PARAM:account_id"}, fmt.Errorf("account_id required")
	}
	fail := &Result{OK: false, Error: "EGO_RUNTIME_REQUIRED", Data: map[string]interface{}{"account_id": accountID}}
	return nil, nil, "", nil, fail, fmt.Errorf("EGO_RUNTIME_REQUIRED: use EgoLite Meta account tools")
}

// handleMetaLogin opens Business Suite on a running account browser for manual login.
// It does not poll login state, store credentials, or claim the account is logged in.
func handleMetaLogin(_ context.Context, _ *Runtime, args map[string]interface{}) (*Result, error) {
	accountID := int64(argInt(args, "account_id", 0))
	if accountID <= 0 {
		return &Result{OK: false, Error: "MISSING_PARAM:account_id"}, fmt.Errorf("account_id required")
	}
	return &Result{OK: false, Error: "EGO_RUNTIME_REQUIRED", Data: map[string]interface{}{"account_id": accountID}}, fmt.Errorf("EGO_RUNTIME_REQUIRED: use EgoLite Meta login")
}

// handleMetaDiscover scans a running Meta browser session for Page/IG targets.
// Discovery may write local account_assets names only; it never stores credentials
// and never marks targets ready for auto-submit without a public external id.
func handleMetaDiscover(_ context.Context, _ *Runtime, args map[string]interface{}) (*Result, error) {
	accountID := int64(argInt(args, "account_id", 0))
	if accountID <= 0 {
		return &Result{OK: false, Error: "MISSING_PARAM:account_id"}, fmt.Errorf("account_id required")
	}
	return &Result{OK: false, Error: "EGO_RUNTIME_REQUIRED", Data: map[string]interface{}{"account_id": accountID}}, fmt.Errorf("EGO_RUNTIME_REQUIRED: use EgoLite Meta discovery")
}

func handleAccountAdd(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	platform := argString(args, "platform", "")
	name := argString(args, "name", "")
	if name == "" {
		name = argString(args, "profile", "")
	}
	if platform == "" || name == "" {
		return &Result{OK: false, Error: "MISSING_PARAM:platform|name"}, fmt.Errorf("platform and name required")
	}
	platformAllowed := false
	for _, candidate := range platforms.EgoLiteAll() {
		if candidate.Code == platform {
			platformAllowed = true
			break
		}
	}
	if !platformAllowed {
		return &Result{OK: false, Error: "PLATFORM_NOT_AVAILABLE"}, fmt.Errorf("platform is not available in the EgoLite release")
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()

	if _, err := database.GetPlatform(platform); err != nil {
		return &Result{OK: false, Error: "PLATFORM_NOT_FOUND"}, err
	}
	identityID := int64(argInt(args, "identity_id", 1))
	acc, err := database.CreateAccountForIdentity(identityID, platform, name, argString(args, "note", ""))
	if err != nil {
		return nil, err
	}
	// optional group
	if g := argString(args, "group", ""); g != "" {
		_, _ = database.Exec(`UPDATE accounts SET account_group = ?, updated_at = ? WHERE id = ?`,
			g, time.Now().UTC().Format(time.RFC3339), acc.ID)
		acc.AccountGroup = g
	}
	return okResult("account created", map[string]interface{}{
		"account_id": acc.ID, "platform": platform, "name": name,
		"identity_id": identityID, "group": acc.AccountGroup,
	}), nil
}

func handleContentCreate(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	contentType := strings.TrimSpace(argString(args, "type", ""))
	title := argString(args, "title", "")
	caption := argString(args, "caption", "")
	body := argString(args, "body", "")
	tagsJSON := strings.TrimSpace(argString(args, "tags_json", "[]"))
	status := strings.TrimSpace(argString(args, "status", "draft"))
	if tagsJSON == "" {
		tagsJSON = "[]"
	}
	if !json.Valid([]byte(tagsJSON)) {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("tags_json must be valid JSON")
	}
	if len(title)+len(caption)+len(body)+len(tagsJSON) > 1<<20 {
		return &Result{OK: false, Error: "PAYLOAD_TOO_LARGE"}, fmt.Errorf("content payload exceeds 1 MiB")
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	content, err := database.CreateContent(contentType, title, caption, body, tagsJSON, status)
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	return okResult("content created", map[string]interface{}{
		"content_id": content.ID,
		"type":       content.Type,
		"status":     content.Status,
	}), nil
}

func handleContentMediaAttach(_ context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	contentID := int64(argInt(args, "content_id", 0))
	libraryID := int64(argInt(args, "library_id", 0))
	role := strings.TrimSpace(argString(args, "role", ""))
	position := argInt(args, "position", -1)
	if contentID <= 0 || libraryID <= 0 || (role != "content" && role != "cover") || position < 0 || position > 10_000 {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("content media attachment is invalid")
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	content, err := database.GetContent(contentID)
	if err != nil {
		return &Result{OK: false, Error: "CONTENT_NOT_FOUND"}, err
	}
	item, err := database.GetMediaLibraryItem(libraryID)
	if err != nil {
		return &Result{OK: false, Error: "MEDIA_LIBRARY_NOT_FOUND"}, err
	}
	if _, err := media.ValidateContentAttachment(content.Type, item.Purpose, item.FilePath, item.SHA256); err != nil {
		code := "VALIDATION_FAILED"
		if strings.Contains(err.Error(), "changed since import") {
			code = "MEDIA_CHANGED"
		}
		return &Result{OK: false, Error: code}, err
	}
	asset, err := database.CreateMediaAssetFromLibraryWithRole(contentID, libraryID, role, position)
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, err
	}
	return okResult("content media attached", map[string]interface{}{
		"media_id": asset.ID, "content_id": asset.ContentID, "library_id": asset.LibraryID,
		"role": asset.Role, "position": asset.Position, "kind": asset.Kind, "sha256": asset.SHA256,
	}), nil
}

func handleMediaImport(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	purpose := strings.TrimSpace(argString(args, "purpose", ""))
	if purpose != "" && purpose != "shared" && purpose != "video" && purpose != "graph_text" && purpose != "article" {
		return &Result{OK: false, Error: "MEDIA_PURPOSE_INVALID"}, fmt.Errorf("invalid media purpose")
	}
	maxFiles := argInt(args, "max_files", 200)
	if maxFiles < 1 || maxFiles > 500 {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("max_files must be between 1 and 500")
	}
	paths, err := media.DiscoverLocalMedia(argString(args, "path", ""), argBool(args, "recursive", true), maxFiles)
	if err != nil {
		if errors.Is(err, media.ErrImportLimitExceeded) {
			return &Result{OK: false, Error: "MEDIA_IMPORT_LIMIT_EXCEEDED"}, err
		}
		if errors.Is(err, media.ErrNoSupportedMedia) {
			return &Result{OK: false, Error: "NO_SUPPORTED_MEDIA"}, err
		}
		return &Result{OK: false, Error: "INVALID_MEDIA_PATH"}, err
	}
	metadata := make([]*media.FileMeta, 0, len(paths))
	for _, path := range paths {
		meta, probeErr := media.Probe(path)
		if probeErr != nil {
			return &Result{OK: false, Error: "MEDIA_PROBE_FAILED"}, probeErr
		}
		metadata = append(metadata, meta)
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	ids := make([]int64, 0, len(metadata))
	items := make([]map[string]interface{}, 0, len(metadata))
	seen := make(map[int64]struct{}, len(metadata))
	for _, meta := range metadata {
		item, indexErr := database.UpsertMediaLibrary(meta.Path, meta.SHA256, meta.SizeBytes, meta.MimeGuess, meta.Width, meta.Height, meta.DurationMS, meta.ProbeJSON)
		if indexErr != nil {
			return &Result{OK: false, Error: "MEDIA_INDEX_FAILED"}, indexErr
		}
		if purpose != "" {
			if purposeErr := database.SetMediaLibraryPurpose(item.ID, purpose); purposeErr != nil {
				return &Result{OK: false, Error: "MEDIA_PURPOSE_INVALID"}, purposeErr
			}
			item.Purpose = purpose
		}
		if _, exists := seen[item.ID]; exists {
			continue
		}
		seen[item.ID] = struct{}{}
		ids = append(ids, item.ID)
		items = append(items, map[string]interface{}{
			"media_id": item.ID, "sha256": item.SHA256, "size_bytes": item.SizeBytes,
			"mime": item.Mime, "purpose": item.Purpose,
		})
	}
	data := map[string]interface{}{"imported_count": len(ids), "media_ids": ids, "items": items}
	if len(items) == 1 {
		for key, value := range items[0] {
			data[key] = value
		}
	}
	return okResult(fmt.Sprintf("indexed %d media item(s)", len(ids)), data), nil
}

func handleQueueEnqueue(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	kind := strings.ToLower(strings.TrimSpace(argString(args, "type", "")))
	if kind != "publish" {
		return &Result{OK: false, Error: "UNSUPPORTED_TASK_KIND"}, fmt.Errorf("only publish queue tasks have an executable handler")
	}
	payload := map[string]interface{}{}
	if raw, exists := args["payload"]; exists && raw != nil {
		value, ok := raw.(map[string]interface{})
		if !ok {
			return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("payload must be an object")
		}
		for key, value := range value {
			payload[key] = value
		}
	}
	if strings.TrimSpace(argString(payload, "platform", "")) == "" || argInt(payload, "content_id", 0) <= 0 || argInt(payload, "target_id", 0) <= 0 {
		return &Result{OK: false, Error: "MISSING_PARAM"}, fmt.Errorf("publish payload requires platform, content_id, and target_id")
	}
	if _, exists := payload["dry_run"]; !exists {
		payload["dry_run"] = true
	}
	if !argBool(payload, "dry_run", true) {
		return &Result{OK: false, Error: "PUBLISH_EGOLITE_PLAN_REQUIRED"}, fmt.Errorf("PUBLISH_EGOLITE_PLAN_REQUIRED: live publishing cannot be queued; use a frozen EgoLite publish plan")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("encode queue payload: %w", err)
	}
	priority := argInt(args, "priority", 0)
	if priority < -100 || priority > 100 {
		return &Result{OK: false, Error: "VALIDATION_FAILED"}, fmt.Errorf("priority must be between -100 and 100")
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	traceID := string(trace.FromContext(ctx))
	item, err := database.EnqueueTask(kind, string(encoded), priority, "", traceID)
	if err != nil {
		return &Result{OK: false, Error: "QUEUE_ENQUEUE_FAILED"}, err
	}
	return okResult("task enqueued", map[string]interface{}{
		"queue_id": item.ID,
		"type":     item.Kind,
		"status":   item.Status,
		"trace_id": item.TraceID,
	}), nil
}

func facadeFrom(rt *Runtime, database *db.DB) *browser.BrowserFacade {
	cfg := config.DefaultConfig()
	if rt != nil {
		if rt.Browser != "" {
			cfg.BrowserPath = rt.Browser
		}
		if rt.BrowserOS != "" {
			cfg.EgoLitePath = rt.BrowserOS
		}
		if rt.Engine != "" {
			cfg.BrowserEngine = rt.Engine
		}
	}
	return browser.NewFacadeFromConfig(database, cfg, dataDir(rt))
}

const (
	browserStartStopLeaseTTL    = 2 * time.Minute
	browserAccountCheckLeaseTTL = 2 * time.Minute
	browserMetaLeaseTTL         = 5 * time.Minute
	toolBackgroundProbeWait     = 2250 * time.Millisecond
	toolBackgroundProbePoll     = 25 * time.Millisecond
)

func acquireToolBrowserOperationLease(database *db.DB, accountID int64, operation string, ttl time.Duration) (func(), *Result, error) {
	profileID := browser.ProfileIDForAccount(accountID)
	owner := "tool:" + operation + ":" + string(trace.New())
	deadline := time.Now().Add(toolBackgroundProbeWait)
	for {
		now := time.Now()
		acquired, err := database.AcquireBrowserOperationLease(profileID, operation, owner, now, ttl)
		if err != nil {
			code := string(apperrors.BROWSER_OPERATION_LEASE_FAILED)
			return nil, &Result{OK: false, Error: code}, fmt.Errorf("%s: %w", code, err)
		}
		if acquired {
			return func() { _ = database.ReleaseBrowserOperationLease(profileID, owner) }, nil, nil
		}
		backgroundProbe, probeErr := database.HasBackgroundBrowserObservationLease(profileID, now)
		if probeErr != nil {
			code := string(apperrors.BROWSER_OPERATION_LEASE_FAILED)
			return nil, &Result{OK: false, Error: code}, fmt.Errorf("%s: %w", code, probeErr)
		}
		if !backgroundProbe {
			// The short probe can release between the failed insert and the
			// classification query. Retry once before reporting a busy profile.
			acquired, err = database.AcquireBrowserOperationLease(profileID, operation, owner, time.Now(), ttl)
			if err != nil {
				code := string(apperrors.BROWSER_OPERATION_LEASE_FAILED)
				return nil, &Result{OK: false, Error: code}, fmt.Errorf("%s: %w", code, err)
			}
			if acquired {
				return func() { _ = database.ReleaseBrowserOperationLease(profileID, owner) }, nil, nil
			}
		}
		if !backgroundProbe || !now.Before(deadline) {
			err := fmt.Errorf("%s: another browser operation owns profile %s", apperrors.BROWSER_OPERATION_BUSY, profileID)
			return nil, &Result{OK: false, Error: string(apperrors.BROWSER_OPERATION_BUSY)}, err
		}
		time.Sleep(toolBackgroundProbePoll)
	}
}

func handleBrowserStart(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	aid := int64(argInt(args, "account_id", 0))
	if aid <= 0 {
		return &Result{OK: false, Error: "MISSING_PARAM:account_id"}, fmt.Errorf("account_id required")
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	release, fail, err := acquireToolBrowserOperationLease(database, aid, "start", browserStartStopLeaseTTL)
	if err != nil {
		return fail, err
	}
	defer release()
	// optional engine override before start
	if eng := argString(args, "engine", ""); eng != "" {
		_ = database.UpdateBrowserProfileEngine(aid, browser.NormalizeEngine(eng))
	}
	// Do NOT facade.Shutdown after start — that kills the browser process.
	// Process is tracked in SQLite runtime; heartbeat stops when Facade is GC'd
	// but child keeps running (same as CLI browser start).
	facade := facadeFrom(rt, database)
	inst, err := facade.StartAccount(aid, browser.StartOptions{SkipFocus: argBool(args, "skip_focus", true)})
	soft := err != nil && strings.Contains(err.Error(), "ALREADY_RUNNING")
	if err != nil && !soft {
		return &Result{OK: false, Error: err.Error()}, err
	}
	st, _ := facade.Status(aid)
	data := map[string]interface{}{"account_id": aid, "engine": argString(args, "engine", "browseros|fingerprint")}
	if inst != nil {
		data["pid"] = inst.PID
		data["cdp_port"] = inst.CDPPort
		data["cdp_url"] = inst.CDPURL
		data["state"] = string(inst.State)
	}
	if st != nil {
		data["running"] = st.Running
		if st.CDPURL != "" {
			data["cdp_url"] = st.CDPURL
		}
		if st.CDPPort > 0 {
			data["cdp_port"] = st.CDPPort
		}
	}
	if soft {
		data["note"] = "already_running"
	}
	return okResult("browser started", data), nil
}

func handleBrowserStop(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	aid := int64(argInt(args, "account_id", 0))
	if aid <= 0 {
		return &Result{OK: false, Error: "MISSING_PARAM:account_id"}, fmt.Errorf("account_id required")
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()
	release, fail, err := acquireToolBrowserOperationLease(database, aid, "stop", browserStartStopLeaseTTL)
	if err != nil {
		return fail, err
	}
	defer release()
	facade := facadeFrom(rt, database)
	// StopAccount is intentional; Shutdown after stop is OK to clean heartbeat
	if err := facade.StopAccount(aid); err != nil {
		// still try shutdown
		facade.Shutdown()
		return &Result{OK: false, Error: err.Error()}, err
	}
	facade.Shutdown()
	return okResult("browser stopped", map[string]interface{}{"account_id": aid}), nil
}

func handlePublishRun(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	platform := argString(args, "platform", "")
	contentID := int64(argInt(args, "content_id", 0))
	targetID := int64(argInt(args, "target_id", 0))
	templateID := int64(argInt(args, "template_id", 0))
	idempotencyKey := argString(args, "idempotency_key", "")
	settingsOverride, hasSettingsOverride := args["settings_json"]
	dryRun := argBool(args, "dry_run", true) // default safe
	autoSubmit := argBool(args, "auto_submit", false)
	confirmed := argBool(args, "confirm", false)
	if platform == "" || contentID <= 0 || targetID <= 0 {
		return &Result{OK: false, Error: "MISSING_PARAM"}, fmt.Errorf("platform, content_id, target_id required")
	}
	if !dryRun && idempotencyKey == "" {
		return &Result{OK: false, Error: "MISSING_PARAM"}, fmt.Errorf("idempotency_key required for live publish")
	}
	readiness := platforms.Readiness(platform)
	if !readiness.CanCreateJob {
		return &Result{OK: false, Error: "PLATFORM_PUBLISH_UNAVAILABLE"}, fmt.Errorf("platform %q has no publish path", platform)
	}
	if autoSubmit && !readiness.CanAttemptAutoSubmit {
		return &Result{OK: false, Error: "AUTO_SUBMIT_NOT_SUPPORTED"}, fmt.Errorf("AUTO_SUBMIT_NOT_SUPPORTED: platform %q requires its declared manual/API flow", platform)
	}
	if autoSubmit {
		return &Result{OK: false, Error: "PUBLISH_FROZEN_SNAPSHOT_REQUIRED"}, fmt.Errorf("PUBLISH_FROZEN_SNAPSHOT_REQUIRED: use publish.plan.submit with one approved single-target plan")
	}
	if !dryRun && !confirmed {
		return &Result{OK: false, Error: "PUBLISH_CONFIRMATION_REQUIRED"}, fmt.Errorf("live publish requires confirm=true")
	}
	if !dryRun {
		return &Result{OK: false, Error: "PUBLISH_EGOLITE_PLAN_REQUIRED"}, fmt.Errorf("PUBLISH_EGOLITE_PLAN_REQUIRED: freeze a plan and use publish.plan.prepare or publish.plan.submit")
	}
	overrideJSON := ""
	if hasSettingsOverride {
		var ok bool
		overrideJSON, ok = settingsOverride.(string)
		if !ok {
			return &Result{OK: false, Error: "INVALID_PUBLISH_SETTINGS"}, fmt.Errorf("settings_json must be a JSON string")
		}
		if _, schemaExists := platforms.SettingsSchemaFor(platform); !schemaExists {
			return &Result{OK: false, Error: "INVALID_PUBLISH_SETTINGS"}, fmt.Errorf("platform %q does not support request settings", platform)
		}
		if _, err := platforms.NormalizeSettings(platform, overrideJSON); err != nil {
			return &Result{OK: false, Error: "INVALID_PUBLISH_SETTINGS"}, err
		}
	}
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()

	content, err := database.GetContent(contentID)
	if err != nil {
		return nil, err
	}
	asset, err := database.GetAccountAsset(targetID)
	if err != nil {
		return nil, err
	}
	if targetErr := publishguard.ValidateTargetOwnership(database, asset, platform); targetErr != nil {
		return &Result{OK: false, Error: targetErr.Error()}, targetErr
	}
	settingsJSON := ""
	templateSettingsJSON := "{}"
	if templateID > 0 {
		template, err := database.GetPlatformSetting(templateID)
		if err != nil {
			return &Result{OK: false, Error: "TEMPLATE_NOT_FOUND"}, err
		}
		if !template.Enabled || template.PlatformCode != platform {
			return &Result{OK: false, Error: "INVALID_TEMPLATE_PLATFORM"}, fmt.Errorf("template must be enabled and match publish platform")
		}
		templateSettingsJSON = template.SettingsJSON
	}
	if hasSettingsOverride {
		templateSettingsJSON = overrideJSON
	}
	if _, schemaExists := platforms.SettingsSchemaFor(platform); schemaExists {
		catalog, err := selectors.Default()
		if err != nil {
			return &Result{OK: false, Error: "SELECTOR_CATALOG_INVALID"}, err
		}
		adapterVersion, versionOK := platforms.PublishSettingsAdapterVersion(platform)
		if !versionOK {
			return &Result{OK: false, Error: "INVALID_PUBLISH_SETTINGS"}, fmt.Errorf("platform %q is missing a settings adapter version", platform)
		}
		settingsJSON, err = platforms.BuildPublishSettingsSnapshot(
			platform, templateID, templateSettingsJSON, catalog.CatalogVersion, adapterVersion,
		)
		if err != nil {
			return &Result{OK: false, Error: "INVALID_PUBLISH_SETTINGS"}, err
		}
	} else if templateID > 0 {
		encoded, err := json.Marshal(struct {
			TemplateID int64           `json:"template_id"`
			Settings   json.RawMessage `json:"settings"`
		}{TemplateID: templateID, Settings: json.RawMessage(templateSettingsJSON)})
		if err != nil {
			return nil, fmt.Errorf("encode publish template: %w", err)
		}
		settingsJSON = string(encoded)
	}
	mode := publish.ExecutionMode(dryRun, autoSubmit)
	if existing, found, findErr := database.FindPublishJobByIdempotencyKey(idempotencyKey); findErr != nil {
		return nil, findErr
	} else if found {
		if existing.BatchID != 0 || existing.ContentID != content.ID || existing.AccountID != asset.AccountID || existing.AssetID != asset.ID || existing.PlatformCode != platform || existing.ContentType != content.Type || existing.Mode != mode || existing.SettingsJSON != settingsJSON {
			return nil, fmt.Errorf("%w: publish idempotency key conflicts with an existing request", db.ErrPublishJobConflict)
		}
		return publishReplayResult(existing, platform, dryRun, templateID), nil
	}

	mediaList, mediaErr := database.ListMediaByContent(content.ID)
	mediaPath, publishMedia, mediaErr := publishguard.PublishMediaPayload(mediaList, mediaErr, content.Type)
	if mediaErr != nil {
		return &Result{OK: false, Error: mediaErr.Error()}, mediaErr
	}
	job, created, err := database.CreatePublishJobWithIntentResult(0, content.ID, asset.AccountID, asset.ID, platform, content.Type, idempotencyKey, mode, settingsJSON)
	if err != nil {
		return nil, err
	}
	if !created {
		return publishReplayResult(job, platform, dryRun, templateID), nil
	}
	runner := &publish.Runner{Store: database}
	result, runErr := runner.Run(ctx, publish.RunInput{
		Job: job, Content: content, Asset: asset,
		DryRun: dryRun, AutoSubmit: autoSubmit,
		MediaPath: mediaPath, Media: publishMedia, EmitNDJSON: true,
	})
	data := map[string]interface{}{
		"job_id": job.ID, "platform": platform, "dry_run": dryRun,
	}
	if templateID > 0 {
		data["template_id"] = templateID
	}
	if result != nil {
		data["status"] = string(result.Status)
		data["step"] = result.Step
		data["message"] = result.Message
	}
	if runErr != nil {
		return &Result{OK: false, Error: runErr.Error(), Data: data, Message: "publish failed"}, runErr
	}
	return okResult("publish finished", data), nil
}

// publishReplayResult reports the durable state of an existing intent without
// invoking Runner. Replays must not claim a terminal/running job a second time.
func publishReplayResult(job *db.PublishJob, platform string, dryRun bool, templateID int64) *Result {
	data := map[string]interface{}{
		"job_id": job.ID, "platform": platform, "dry_run": dryRun,
		"status": job.Status, "step": job.CurrentStep,
	}
	if templateID > 0 {
		data["template_id"] = templateID
	}
	message := "已恢复同一发布请求的已有任务"
	if job.ErrorMessage != "" {
		message = job.ErrorMessage
	}
	data["message"] = message
	switch job.Status {
	case "pending", "running":
		return okResult("same publish request is still in progress", data)
	case "failed", "cancelled":
		errorCode := job.ErrorCode
		if errorCode == "" {
			errorCode = "PUBLISH_REPLAY_FAILED"
		}
		return &Result{OK: false, Error: errorCode, Message: message, Data: data}
	default:
		return okResult("same publish request restored", data)
	}
}

func handleBrowserStatus(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	database, err := openDB(rt)
	if err != nil {
		return nil, err
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	if rt != nil && rt.Browser != "" {
		cfg.BrowserPath = rt.Browser
	}
	facade := browser.NewFacadeFromConfig(database, cfg, dataDir(rt))
	defer facade.Shutdown()

	// single account
	if id := argInt(args, "account_id", 0); id > 0 {
		st, err := facade.Status(int64(id))
		if err != nil {
			return &Result{OK: false, Error: err.Error()}, err
		}
		return okResult("status", map[string]interface{}{
			"account_id": st.AccountID, "running": st.Running, "state": string(st.State),
		}), nil
	}

	accounts, err := database.ListAccounts()
	if err != nil {
		return nil, err
	}
	statuses := make([]map[string]interface{}, 0, len(accounts))
	running := make([]map[string]interface{}, 0, len(accounts))
	for _, a := range accounts {
		isRunning := false
		state := string(browser.StateStopped)
		if st, statusErr := facade.Status(a.ID); statusErr == nil && st != nil {
			isRunning = st.Running
			state = string(st.State)
		}
		row := map[string]interface{}{
			"account_id": a.ID, "running": isRunning, "state": state,
			"platform": a.PlatformCode, "name": a.Name, "login_state": a.LoginState,
		}
		statuses = append(statuses, row)
		if isRunning {
			running = append(running, row)
		}
	}
	return okResult(fmt.Sprintf("%d running accounts", len(running)), map[string]interface{}{
		"running": running, "accounts": statuses,
	}), nil
}

func handleTikHubUserList(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	users, err := tikhub.LoadWatchlist(dataDir(rt))
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]interface{}, 0, len(users))
	for _, u := range users {
		rows = append(rows, map[string]interface{}{
			"platform": u.Platform, "user_id": u.UserID, "name": u.Name,
			"last_check": u.LastCheck, "last_count": u.LastCount, "note": u.Note,
		})
	}
	return okResult(fmt.Sprintf("%d watched", len(rows)), map[string]interface{}{"users": rows, "count": len(rows)}), nil
}

func handleTikHubUserCheckAll(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	dir := dataDir(rt)
	users, err := tikhub.LoadWatchlist(dir)
	if err != nil {
		return nil, err
	}
	if !argBool(args, "confirm", false) {
		return okResult("dry plan only — pass confirm=true to hit TikHub network", map[string]interface{}{
			"watch_count": len(users), "configured": true,
		}), nil
	}
	c, _, err := tikhub.NewActiveClient(dir)
	if err != nil {
		return &Result{
			OK:      false,
			Error:   tikhubProviderErrorCode(err),
			Message: "请先在设置中保存、测试并激活 TikHub 数据 API",
		}, err
	}
	okN, failN := 0, 0
	details := make([]map[string]interface{}, 0, len(users))
	for _, u := range users {
		if u.Platform != "xhs" {
			details = append(details, map[string]interface{}{"user_id": u.UserID, "error": "only xhs implemented"})
			failN++
			continue
		}
		info, err1 := c.GetXHSUserInfo(ctx, u.UserID)
		notes, err2 := c.GetXHSUserNotes(ctx, u.UserID, "")
		if err1 != nil || err2 != nil {
			msg := ""
			if err1 != nil {
				msg = err1.Error()
			}
			if err2 != nil {
				msg += "; " + err2.Error()
			}
			details = append(details, map[string]interface{}{"user_id": u.UserID, "error": msg})
			failN++
			continue
		}
		items := tikhub.ExtractRankItems(notes)
		sub := filepath.Join("users", tikhub.Today(), u.Platform+"_"+u.UserID+".json")
		fp, err := tikhub.SaveJSON(dir, sub, map[string]interface{}{
			"user_id": u.UserID, "info": info, "notes": notes, "note_count": len(items),
			"fetched_at": time.Now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			details = append(details, map[string]interface{}{"user_id": u.UserID, "error": "save user snapshot failed"})
			failN++
			continue
		}
		if err := tikhub.UpdateWatchStats(dir, u.Platform, u.UserID, len(items)); err != nil {
			details = append(details, map[string]interface{}{"user_id": u.UserID, "file": fp, "error": "save watch stats failed"})
			failN++
			continue
		}
		details = append(details, map[string]interface{}{"user_id": u.UserID, "note_count": len(items), "file": fp})
		okN++
	}
	return okResult(fmt.Sprintf("success=%d failed=%d", okN, failN), map[string]interface{}{
		"success": okN, "failed": failN, "details": details,
	}), nil
}

// handleWorkflowRun executes ordered tool steps (Tool Agent primitive).
// args.steps = [{ "tool": "name", "args": {...} }, ...]
func handleWorkflowRun(ctx context.Context, rt *Runtime, args map[string]interface{}) (*Result, error) {
	rawSteps, ok := args["steps"]
	if !ok {
		return &Result{OK: false, Error: "MISSING_PARAM:steps"}, fmt.Errorf("steps required")
	}
	arr, ok := rawSteps.([]interface{})
	if !ok || len(arr) == 0 {
		return &Result{OK: false, Error: "INVALID_STEPS"}, fmt.Errorf("steps must be non-empty array")
	}
	stopOnErr := argBool(args, "stop_on_error", true)
	results := make([]map[string]interface{}, 0, len(arr))
	allOK := true
	for i, raw := range arr {
		step, ok := raw.(map[string]interface{})
		if !ok {
			allOK = false
			results = append(results, map[string]interface{}{"index": i, "error": "step not object"})
			if stopOnErr {
				break
			}
			continue
		}
		name := argString(step, "tool", "")
		if name == "" {
			name = argString(step, "name", "")
		}
		if name == "" {
			allOK = false
			results = append(results, map[string]interface{}{"index": i, "error": "missing tool"})
			if stopOnErr {
				break
			}
			continue
		}
		if name == "workflow.run" {
			allOK = false
			results = append(results, map[string]interface{}{"index": i, "tool": name, "error": "nested workflow.run not allowed"})
			if stopOnErr {
				break
			}
			continue
		}
		stepArgs, _ := step["args"].(map[string]interface{})
		res, err := Default.Call(ctx, rt, name, stepArgs)
		entry := map[string]interface{}{"index": i, "tool": name}
		if res != nil {
			entry["ok"] = res.OK
			entry["message"] = res.Message
			entry["error"] = res.Error
			entry["data"] = res.Data
			entry["duration"] = res.Duration
			if !res.OK {
				allOK = false
			}
		}
		if err != nil {
			allOK = false
			entry["error"] = err.Error()
			if stopOnErr {
				results = append(results, entry)
				break
			}
		}
		results = append(results, entry)
	}
	msg := "workflow finished"
	if !allOK {
		msg = "workflow finished with errors"
	}
	return &Result{
		OK:      allOK,
		Message: msg,
		Data:    map[string]interface{}{"steps": results, "count": len(results)},
	}, nil
}
