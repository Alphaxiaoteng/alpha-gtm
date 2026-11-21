package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/shoplisting"
	"alpha-nexus-egolite/internal/tools"
)

type shopListingTestRunner struct {
	programs  []string
	err       error
	spaceID   int64
	taskID    string
	name      string
	ownership string
}

func (runner *shopListingTestRunner) Run(_ context.Context, program string, result any) error {
	runner.programs = append(runner.programs, program)
	if runner.err != nil {
		return runner.err
	}
	spaceID := runner.spaceID
	if spaceID == 0 {
		spaceID = 91
	}
	taskID := runner.taskID
	if taskID == "" {
		taskID = "task-shop"
	}
	name := runner.name
	if name == "" {
		name = "shop-space"
	}
	ownership := runner.ownership
	if ownership == "" {
		ownership = "agentDelegatedToUser"
	}
	payload, err := json.Marshal(map[string]any{
		"id":        spaceID,
		"taskId":    taskID,
		"name":      name,
		"ownership": ownership,
		"done":      true,
	})
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, result)
}

func TestShopListingApprovalPrepareHandoffForAllPlatforms(t *testing.T) {
	for _, platformCode := range []string{
		"douyin_shop",
		"taobao",
		"tiktok_shop",
		"shopee",
		"shopify",
		"amazon_seller",
		"jd_shop",
		"pinduoduo_shop",
		"xhs_shop",
		"kuaishou_shop",
		"wechat_shop",
		"alibaba_1688",
		"aliexpress",
	} {
		t.Run(platformCode, func(t *testing.T) {
			runner := &shopListingTestRunner{}
			server, database, plan, _ := newShopListingTestServerForPlatform(t, runner, platformCode)
			defer database.Close()

			platform, err := platforms.Find(platformCode)
			if err != nil {
				t.Fatal(err)
			}
			space, err := database.GetEgoSpaceByIdentity(plan.IdentityID)
			if err != nil {
				t.Fatal(err)
			}
			if space.IdentityID != plan.IdentityID || space.SpaceID != runner.spaceID {
				t.Fatalf("space=%+v plan=%+v", space, plan)
			}

			approval := requestShopListing(t, server, http.MethodPost,
				"/api/shop/listing/plans/"+jsonNumber(plan.ID)+"/prepare-approval",
				`{"snapshotHash":"`+plan.SnapshotHash+`"}`)
			if approval.Code != http.StatusOK {
				t.Fatalf("approval status=%d body=%s", approval.Code, approval.Body.String())
			}
			var grant shopListingApprovalResponse
			if err := json.Unmarshal(approval.Body.Bytes(), &grant); err != nil {
				t.Fatal(err)
			}

			prepared := requestShopListing(t, server, http.MethodPost,
				"/api/shop/listing/plans/"+jsonNumber(plan.ID)+"/prepare",
				`{"snapshotHash":"`+plan.SnapshotHash+`","approvalId":"`+grant.ApprovalID+`","nonce":"`+grant.Nonce+`"}`)
			if prepared.Code != http.StatusOK {
				t.Fatalf("prepare status=%d body=%s", prepared.Code, prepared.Body.String())
			}
			var result struct {
				Plan struct {
					ID int64 `json:"id"`
				} `json:"plan"`
				Account struct {
					ID int64 `json:"id"`
				} `json:"account"`
				Identity struct {
					ID int64 `json:"id"`
				} `json:"identity"`
				Platform string `json:"platform"`
				Space    struct {
					SpaceID   int64  `json:"space_id"`
					State     string `json:"state"`
					Ownership string `json:"ownership"`
				} `json:"space"`
				Status    string `json:"status"`
				State     string `json:"state"`
				ErrorCode string `json:"error_code"`
			}
			if err := json.Unmarshal(prepared.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Plan.ID != plan.ID || result.Account.ID != plan.AccountID || result.Identity.ID != plan.IdentityID ||
				result.Platform != platformCode || result.Status != shoplisting.StatusManualTakeover ||
				result.State != shoplisting.StatusManualTakeover || result.ErrorCode != "SHOP_LISTING_FORM_REVIEW_REQUIRED" ||
				result.Space.SpaceID != space.SpaceID || result.Space.State != shoplisting.StatusManualTakeover ||
				result.Space.Ownership != "agentDelegatedToUser" {
				t.Fatalf("prepare result=%+v body=%s", result, prepared.Body.String())
			}

			stored, err := (&shoplisting.Service{DB: database}).Get(plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != shoplisting.StatusManualTakeover || stored.ErrorCode != "SHOP_LISTING_FORM_REVIEW_REQUIRED" || len(stored.Items) != 1 {
				t.Fatalf("stored plan=%+v", stored)
			}
			for _, item := range stored.Items {
				if item.Status != shoplisting.StatusManualTakeover || item.ErrorCode != "SHOP_LISTING_FORM_REVIEW_REQUIRED" {
					t.Fatalf("stored item=%+v", item)
				}
			}

			if len(runner.programs) != 1 {
				t.Fatalf("runtime programs=%d", len(runner.programs))
			}
			program := runner.programs[0]
			if !strings.Contains(program, `if (managerSpace.ownership === "user" || managerSpace.ownership === "agentDelegatedToUser")`) ||
				!strings.Contains(program, "await handOffTaskSpace(") {
				t.Fatalf("ownership guard/handoff missing: %s", program)
			}
			managerURLJSON, err := json.Marshal(platform.ManagerURL)
			if err != nil {
				t.Fatal(err)
			}
			openCall := "await openOrReuseTab(" + string(managerURLJSON) + ", { wait: true"
			if strings.Count(program, "openOrReuseTab(") != 1 || !strings.Contains(program, openCall) {
				t.Fatalf("manager URL open program=%s want=%s", program, openCall)
			}
			lowerProgram := strings.ToLower(program)
			for _, forbidden := range []string{
				"takeovertaskspace",
				"submit",
				"publish",
				"final-click",
				"final_click",
				"final click",
				"finalclick",
				"click(",
			} {
				if strings.Contains(lowerProgram, forbidden) {
					t.Fatalf("forbidden shop-listing handoff semantic %q in program=%s", forbidden, program)
				}
			}
		})
	}
}

func TestShopListingApprovalPrepareHandoffIsSingleUseAndRedacted(t *testing.T) {
	runner := &shopListingTestRunner{}
	server, database, plan, mediaPath := newShopListingTestServer(t, runner)
	defer database.Close()

	approval := requestShopListing(t, server, http.MethodPost,
		"/api/shop/listing/plans/"+jsonNumber(plan.ID)+"/prepare-approval",
		`{"snapshotHash":"`+plan.SnapshotHash+`"}`)
	if approval.Code != http.StatusOK {
		t.Fatalf("approval status=%d body=%s", approval.Code, approval.Body.String())
	}
	var grant shopListingApprovalResponse
	if err := json.Unmarshal(approval.Body.Bytes(), &grant); err != nil {
		t.Fatal(err)
	}
	if grant.ApprovalID == "" || grant.Nonce == "" || grant.ExpiresAt == "" ||
		strings.Contains(approval.Body.String(), mediaPath) || strings.Contains(approval.Body.String(), "never-leak-title") {
		t.Fatalf("approval=%+v body=%s", grant, approval.Body.String())
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, grant.ExpiresAt)
	if err != nil || time.Until(expiresAt) <= 4*time.Minute || time.Until(expiresAt) > 5*time.Minute {
		t.Fatalf("approval expiry=%q parsed=%v err=%v", grant.ExpiresAt, expiresAt, err)
	}
	var sessionID string
	if err := database.QueryRow(`SELECT session_id FROM approvals WHERE id=?`, grant.ApprovalID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if sessionID != "ui-session-stable" {
		t.Fatalf("approval session=%q", sessionID)
	}

	body := `{"snapshotHash":"` + plan.SnapshotHash + `","approvalId":"` + grant.ApprovalID + `","nonce":"` + grant.Nonce + `"}`
	prepared := requestShopListing(t, server, http.MethodPost,
		"/api/shop/listing/plans/"+jsonNumber(plan.ID)+"/prepare", body)
	if prepared.Code != http.StatusOK {
		t.Fatalf("prepare status=%d body=%s", prepared.Code, prepared.Body.String())
	}
	var result struct {
		Plan struct {
			ID int64 `json:"id"`
		} `json:"plan"`
		Account struct {
			ID int64 `json:"id"`
		} `json:"account"`
		Identity struct {
			ID int64 `json:"id"`
		} `json:"identity"`
		Platform string `json:"platform"`
		Space    struct {
			SpaceID   int64  `json:"space_id"`
			State     string `json:"state"`
			Ownership string `json:"ownership"`
		} `json:"space"`
		Status    string `json:"status"`
		State     string `json:"state"`
		ErrorCode string `json:"error_code"`
	}
	if err := json.Unmarshal(prepared.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Plan.ID != plan.ID || result.Account.ID != plan.AccountID || result.Identity.ID != plan.IdentityID ||
		result.Platform != plan.PlatformCode || result.Status != shoplisting.StatusManualTakeover ||
		result.State != shoplisting.StatusManualTakeover || result.ErrorCode != "SHOP_LISTING_FORM_REVIEW_REQUIRED" ||
		result.Space.SpaceID != 91 || result.Space.State != shoplisting.StatusManualTakeover {
		t.Fatalf("prepare result=%+v body=%s", result, prepared.Body.String())
	}
	for _, forbidden := range []string{"never-leak-title", mediaPath, "snapshot_json", "payload_json", "file_path"} {
		if strings.Contains(prepared.Body.String(), forbidden) {
			t.Fatalf("prepare leaked %q: %s", forbidden, prepared.Body.String())
		}
	}
	if len(runner.programs) != 1 || strings.Contains(runner.programs[0], "takeOverTaskSpace") || strings.Contains(runner.programs[0], "closeTab") {
		t.Fatalf("handoff programs=%v", runner.programs)
	}

	replayed := requestShopListing(t, server, http.MethodPost,
		"/api/shop/listing/plans/"+jsonNumber(plan.ID)+"/prepare", body)
	if replayed.Code != http.StatusConflict || !strings.Contains(replayed.Body.String(), "errorCode") {
		t.Fatalf("replayed prepare status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	stored, err := (&shoplisting.Service{DB: database}).Get(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != shoplisting.StatusManualTakeover || stored.ErrorCode != "SHOP_LISTING_FORM_REVIEW_REQUIRED" {
		t.Fatalf("stored plan=%+v", stored)
	}
}

func TestShopListingUserControlledSpaceStopsSafely(t *testing.T) {
	runner := &shopListingTestRunner{err: errors.New("EGO_SPACE_USER_CONTROLLED")}
	server, database, plan, _ := newShopListingTestServer(t, runner)
	defer database.Close()

	approval := requestShopListing(t, server, http.MethodPost,
		"/api/shop/listing/plans/"+jsonNumber(plan.ID)+"/prepare-approval",
		`{"snapshotHash":"`+plan.SnapshotHash+`"}`)
	var grant shopListingApprovalResponse
	if approval.Code != http.StatusOK || json.Unmarshal(approval.Body.Bytes(), &grant) != nil {
		t.Fatalf("approval status=%d body=%s", approval.Code, approval.Body.String())
	}
	prepared := requestShopListing(t, server, http.MethodPost,
		"/api/shop/listing/plans/"+jsonNumber(plan.ID)+"/prepare",
		`{"snapshotHash":"`+plan.SnapshotHash+`","approvalId":"`+grant.ApprovalID+`","nonce":"`+grant.Nonce+`"}`)
	if prepared.Code != http.StatusOK || !strings.Contains(prepared.Body.String(), `"status":"manual_takeover"`) ||
		!strings.Contains(prepared.Body.String(), `"error_code":"EGO_SPACE_USER_CONTROLLED"`) {
		t.Fatalf("user-controlled prepare status=%d body=%s", prepared.Code, prepared.Body.String())
	}
	space, err := database.GetEgoSpaceByIdentity(plan.IdentityID)
	if err != nil {
		t.Fatal(err)
	}
	if space.State != shoplisting.StatusManualTakeover || space.ErrorCode != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("space=%+v", space)
	}
	if len(runner.programs) != 1 {
		t.Fatalf("runner programs=%d", len(runner.programs))
	}
	guard := strings.Index(runner.programs[0], `throw new Error("EGO_SPACE_USER_CONTROLLED")`)
	navigation := strings.Index(runner.programs[0], "openOrReuseTab")
	if guard < 0 || navigation < 0 || guard > navigation || strings.Contains(runner.programs[0], "takeOverTaskSpace") {
		t.Fatalf("ownership guard ordering/program=%s", runner.programs[0])
	}
}

func TestShopListingHandoffFailurePersistsManualTakeoverAndConsumesApproval(t *testing.T) {
	runner := &shopListingTestRunner{err: errors.New("EGO_RUNTIME_EXECUTION_FAILED")}
	server, database, plan, _ := newShopListingTestServer(t, runner)
	defer database.Close()

	approval := requestShopListing(t, server, http.MethodPost,
		"/api/shop/listing/plans/"+jsonNumber(plan.ID)+"/prepare-approval",
		`{"snapshotHash":"`+plan.SnapshotHash+`"}`)
	var grant shopListingApprovalResponse
	if approval.Code != http.StatusOK || json.Unmarshal(approval.Body.Bytes(), &grant) != nil {
		t.Fatalf("approval status=%d body=%s", approval.Code, approval.Body.String())
	}
	body := `{"snapshotHash":"` + plan.SnapshotHash + `","approvalId":"` + grant.ApprovalID + `","nonce":"` + grant.Nonce + `"}`
	prepared := requestShopListing(t, server, http.MethodPost,
		"/api/shop/listing/plans/"+jsonNumber(plan.ID)+"/prepare", body)
	if prepared.Code != http.StatusConflict || !strings.Contains(prepared.Body.String(), `"errorCode":"EGO_RUNTIME_EXECUTION_FAILED"`) {
		t.Fatalf("prepare status=%d body=%s", prepared.Code, prepared.Body.String())
	}

	stored, err := (&shoplisting.Service{DB: database}).Get(plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != shoplisting.StatusManualTakeover || stored.ErrorCode != "EGO_RUNTIME_EXECUTION_FAILED" {
		t.Fatalf("stored plan status=%q error_code=%q", stored.Status, stored.ErrorCode)
	}
	for _, item := range stored.Items {
		if item.Status != shoplisting.StatusManualTakeover || item.ErrorCode != "EGO_RUNTIME_EXECUTION_FAILED" {
			t.Fatalf("stored item id=%d status=%q error_code=%q", item.ID, item.Status, item.ErrorCode)
		}
	}

	replayed := requestShopListing(t, server, http.MethodPost,
		"/api/shop/listing/plans/"+jsonNumber(plan.ID)+"/prepare", body)
	if replayed.Code != http.StatusConflict || len(runner.programs) != 1 {
		t.Fatalf("replayed prepare status=%d programs=%d body=%s", replayed.Code, len(runner.programs), replayed.Body.String())
	}
}

func TestShopListingPrepareRejectsInvalidInputAndUsesAuthMiddleware(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateAppUser("local", "not-a-password-hash", "admin"); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database, mux: http.NewServeMux(), toolRuntime: &tools.Runtime{DataDir: dataDir}}
	server.routes()

	unauthorized := httptest.NewRecorder()
	server.withAuth(server.mux).ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost,
		"/api/shop/listing/plans/1/prepare", bytes.NewBufferString(`{"snapshotHash":"hash","approvalId":"id","nonce":"nonce"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("auth status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	for _, test := range []struct {
		path string
		body string
	}{
		{"/api/shop/listing/plans/no/prepare-approval", `{"snapshotHash":"hash"}`},
		{"/api/shop/listing/plans/1/prepare-approval", `{"snapshotHash":""}`},
		{"/api/shop/listing/plans/1/prepare-approval", `{"snapshotHash":"hash","unknown":true}`},
		{"/api/shop/listing/plans/1/prepare", `{"snapshotHash":"hash","approvalId":"id"}`},
	} {
		response := httptest.NewRecorder()
		server.mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body)))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "VALIDATION_FAILED") {
			t.Fatalf("path=%s status=%d body=%s", test.path, response.Code, response.Body.String())
		}
	}
}

func newShopListingTestServer(t *testing.T, runner *shopListingTestRunner) (*Server, *db.DB, *shoplisting.Plan, string) {
	return newShopListingTestServerForPlatform(t, runner, "shopify")
}

func newShopListingTestServerForPlatform(t *testing.T, runner *shopListingTestRunner, platformCode string) (*Server, *db.DB, *shoplisting.Plan, string) {
	t.Helper()
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	identity, err := database.GetEgoIdentity(1)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	account, err := database.CreateAccountForIdentity(identity.ID, platformCode, platformCode+" 测试店铺", "")
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		database.Close()
		t.Fatal(err)
	}
	space, err := database.CreateEgoSpaceForIdentity(identity.ID, 91, "task-shop-"+platformCode, "shop-space-"+platformCode)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	runner.spaceID = space.SpaceID
	runner.taskID = space.TaskID
	runner.name = space.Name
	runner.ownership = "agentDelegatedToUser"
	mediaPath := filepath.Join(t.TempDir(), "private-product-image.bin")
	media := []byte("private shop listing media")
	if err := os.WriteFile(mediaPath, media, 0o600); err != nil {
		database.Close()
		t.Fatal(err)
	}
	digest := sha256.Sum256(media)
	plan, _, err := (&shoplisting.Service{DB: database}).Freeze(shoplisting.FreezeInput{
		IdentityID: identity.ID, AccountID: account.ID, PlatformCode: platformCode, IdempotencyKey: "shop-ui-test-" + platformCode,
		Items: []shoplisting.ItemInput{{
			Title: "never-leak-title", Description: "private product description", Category: "test",
			Currency: "CNY", PriceMinor: 100, Inventory: 1, WeightGrams: 10,
			Media: []shoplisting.MediaInput{{FilePath: mediaPath, SHA256: hex.EncodeToString(digest[:]), Role: "main"}},
		}},
	})
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	server := &Server{
		db: database, mux: http.NewServeMux(),
		health:      healthState{Nonce: "ui-session-stable"},
		egoSpaces:   egolite.NewSpaceService(database, runner),
		toolRuntime: &tools.Runtime{DataDir: dataDir},
	}
	server.toolRuntime.PrepareShopListing = server.PrepareShopListing
	server.routes()
	return server, database, plan, mediaPath
}

func requestShopListing(t *testing.T, server *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
	return response
}
