package publish_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
	_ "alpha-nexus-egolite/internal/platforms/meta"
	"alpha-nexus-egolite/internal/publish"
	"alpha-nexus-egolite/internal/trace"
)

type memStore struct {
	mu               sync.Mutex
	claimed          bool
	claimErr         error
	updates          []string
	events           int
	records          int
	lastCode         string
	lastShot         string
	lastEventEv      string
	lastEvent        db.JobEventInput
	updateErr        error
	updateErrAt      int
	updateCalls      int
	eventErr         error
	eventErrAt       int
	eventCalls       int
	recordErr        error
	cancelled        bool
	cancelOnFinalize bool
	finalizeCalls    int
	mode             string
	settingsJSON     string
	runtimeBusy      bool
	runtimeJobID     int64
	runtimeReleases  int
	runtimeClaimErr  error
	connectedAsset   *db.AccountAsset
	submitAttempts   int
	submitOutcome    string
}

func (m *memStore) SetPublishJobSettings(id int64, settingsJSON string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settingsJSON = settingsJSON
	return nil
}

func (m *memStore) MarkPublishSubmitAttempt(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.submitAttempts > 0 {
		return db.ErrPublishJobConflict
	}
	m.submitAttempts++
	return nil
}

func (m *memStore) SetPublishSubmitOutcome(id int64, outcome string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.submitOutcome = outcome
	return nil
}

func (m *memStore) TouchPublishJob(id int64) (bool, error) { return true, nil }

func (m *memStore) ClaimBrowserRuntimeJob(accountID, jobID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runtimeClaimErr != nil {
		return false, m.runtimeClaimErr
	}
	if m.runtimeBusy && m.runtimeJobID != jobID {
		return false, nil
	}
	m.runtimeBusy = true
	m.runtimeJobID = jobID
	return true, nil
}

func (m *memStore) VerifyBrowserRuntimeJob(accountID, jobID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runtimeBusy && m.runtimeJobID == jobID, nil
}

func (m *memStore) ReleaseBrowserRuntimeJob(accountID, jobID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.runtimeBusy || m.runtimeJobID != jobID {
		return false, nil
	}
	m.runtimeBusy = false
	m.runtimeJobID = 0
	m.runtimeReleases++
	return true, nil
}

func (m *memStore) GetAccountAsset(id int64) (*db.AccountAsset, error) {
	if m.connectedAsset == nil || m.connectedAsset.ID != id {
		return nil, errors.New("account asset not found")
	}
	return m.connectedAsset, nil
}

func (m *memStore) ClaimPublishJob(id int64, traceID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.claimErr != nil {
		return false, m.claimErr
	}
	if m.claimed {
		return false, nil
	}
	m.claimed = true
	return true, nil
}

func (m *memStore) UpdatePublishJob(id int64, status, currentStep, errorCode, errorMessage string, retryCount int) error {
	m.updateCalls++
	m.updates = append(m.updates, status+":"+currentStep)
	m.lastCode = errorCode
	if m.updateErr != nil && (m.updateErrAt == 0 || m.updateErrAt == m.updateCalls) {
		return m.updateErr
	}
	return nil
}
func (m *memStore) CreateJobEventDetailed(event db.JobEventInput) (*db.JobEvent, error) {
	m.eventCalls++
	if m.eventErr != nil && (m.eventErrAt == 0 || m.eventErrAt == m.eventCalls) {
		return nil, m.eventErr
	}
	m.events++
	m.lastShot = event.ScreenshotPath
	m.lastEventEv = event.Type
	m.lastEvent = event
	return &db.JobEvent{ID: int64(m.events)}, nil
}
func (m *memStore) CreatePublishRecord(jobID, contentID, accountID, assetID int64, platformCode, contentType, status, postURL, externalPostID string) (*db.PublishRecord, error) {
	if m.recordErr != nil {
		return nil, m.recordErr
	}
	m.records++
	return &db.PublishRecord{ID: int64(m.records)}, nil
}

func (m *memStore) IsPublishJobCancelRequested(id int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cancelled, nil
}

func (m *memStore) ValidatePublishJobMode(id int64, mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mode != "" && m.mode != mode {
		return db.ErrPublishJobConflict
	}
	m.mode = mode
	return nil
}

func (m *memStore) FinalizePublishJob(id int64, status, currentStep, errorCode, errorMessage string, retryCount int) (bool, error) {
	m.finalizeCalls++
	if m.cancelOnFinalize {
		m.cancelled = true
		if err := m.UpdatePublishJob(id, "cancelled", "cancelled", "", "", retryCount); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, m.UpdatePublishJob(id, status, currentStep, errorCode, errorMessage, retryCount)
}

type blockingPublisher struct {
	platforms.Publisher
	code    string
	started chan struct{}
}

type confirmedPublisher struct{ code string }

type recordingAutoSubmitPublisher struct {
	code   string
	called bool
}

type requestCapturePublisher struct {
	code string
	req  platforms.PublishRequest
}

type evidencePublisher struct{ code string }

type imageEvidencePublisher struct{ code string }

type loginRequiredPublisher struct{ code string }

func (p *evidencePublisher) Platform() string                        { return p.code }
func (p *evidencePublisher) Strategy() platforms.PublishStrategy     { return platforms.StrategyBrowser }
func (p *evidencePublisher) Validate(platforms.PublishRequest) error { return nil }
func (p *evidencePublisher) Publish(context.Context, platforms.PublishRequest) (*platforms.PublishResult, error) {
	return &platforms.PublishResult{
		Status: string(publish.StatusManualTakeover), Step: "required_control_missing", ErrorCode: "FORM_REVIEW_REQUIRED",
		Evidence: map[string]interface{}{
			"missingRequiredControls": []string{"content.body", "media.article_cover"},
			"unverifiedFields":        []string{"settings.comment_enabled"},
			"pageInspection":          map[string]interface{}{"body": "must not persist", "controls": make([]string, 1000)},
		},
	}, nil
}

func (p *imageEvidencePublisher) Platform() string { return p.code }
func (p *imageEvidencePublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}
func (p *imageEvidencePublisher) Validate(platforms.PublishRequest) error { return nil }
func (p *imageEvidencePublisher) Publish(context.Context, platforms.PublishRequest) (*platforms.PublishResult, error) {
	return &platforms.PublishResult{Status: string(publish.StatusManualTakeover), Step: "required_control_missing", ErrorCode: "FORM_REVIEW_REQUIRED", Evidence: map[string]interface{}{
		"contentImagesUploadedSelectionVerified":   true,
		"contentImagesUploadedPreviewObserved":     false,
		"contentImagesUploadedProgressCleared":     true,
		"contentImagesUploadedErrorFree":           true,
		"contentImagesUploadedScopeVerified":       true,
		"contentImagesUploadedCompletionStable":    false,
		"contentImagesUploadedBaselineMediaCount":  0,
		"contentImagesUploadedPreviewMediaCount":   1,
		"contentImagesUploadedPreviewIncrement":    1,
		"contentImagesUploadedExpectedItemCount":   1,
		"contentImagesUploadedPreviewFingerprints": []string{"IMG:0:100x100:https://private.example/path?secret=no"},
		"criticalRevalidation":                     map[string]bool{"topics": false},
	}}, nil
}

func (p *loginRequiredPublisher) Platform() string { return p.code }
func (p *loginRequiredPublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}
func (p *loginRequiredPublisher) Validate(platforms.PublishRequest) error { return nil }
func (p *loginRequiredPublisher) Publish(context.Context, platforms.PublishRequest) (*platforms.PublishResult, error) {
	return &platforms.PublishResult{
		Status: string(publish.StatusManualTakeover), Step: "login_challenge", ErrorCode: "PUBLISH_ACCOUNT_NOT_LOGGED_IN",
	}, nil
}

func (p *requestCapturePublisher) Platform() string { return p.code }
func (p *requestCapturePublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}
func (p *requestCapturePublisher) Validate(platforms.PublishRequest) error { return nil }
func (p *requestCapturePublisher) Publish(_ context.Context, req platforms.PublishRequest) (*platforms.PublishResult, error) {
	p.req = req
	return &platforms.PublishResult{Status: "dry_run_ok", Step: "captured"}, nil
}

func (p *recordingAutoSubmitPublisher) Platform() string { return p.code }
func (p *recordingAutoSubmitPublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}
func (p *recordingAutoSubmitPublisher) Validate(platforms.PublishRequest) error { return nil }
func (p *recordingAutoSubmitPublisher) Publish(context.Context, platforms.PublishRequest) (*platforms.PublishResult, error) {
	p.called = true
	return &platforms.PublishResult{Status: "published", Step: "unsafe_fixture"}, nil
}

func (p *confirmedPublisher) Platform() string { return p.code }
func (p *confirmedPublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}
func (p *confirmedPublisher) Validate(platforms.PublishRequest) error { return nil }
func (p *confirmedPublisher) Publish(context.Context, platforms.PublishRequest) (*platforms.PublishResult, error) {
	return &platforms.PublishResult{
		Status: "published", Step: "confirmed_fixture",
		ExternalID: "confirmed-fixture-id", PostURL: "https://example.invalid/confirmed-fixture",
		Evidence: map[string]interface{}{
			"submitAttempted": true,
			"submitClicked":   true,
			"submitClickedAt": "2026-07-27T08:28:45.744733Z",
			"submitTargetId":  "A4D362607FC76673DA07AE900173A0F3",
		},
	}, nil
}

func useConfirmedPublisher(t *testing.T, code string) {
	t.Helper()
	original, err := platforms.GetPublisher(code)
	if err != nil {
		t.Fatal(err)
	}
	platforms.RegisterPublisher(&confirmedPublisher{code: code})
	t.Cleanup(func() { platforms.RegisterPublisher(original) })
}

func TestRunnerPersistsOnlyFieldLevelEvidenceSummary(t *testing.T) {
	const code = "instagram"
	original, err := platforms.GetPublisher(code)
	if err != nil {
		t.Fatal(err)
	}
	platforms.RegisterPublisher(&evidencePublisher{code: code})
	t.Cleanup(func() { platforms.RegisterPublisher(original) })
	store := &memStore{}
	result, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 48, PlatformCode: code, ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, PlatformCode: code, AssetType: "instagram_account"},
	})
	if err == nil || result == nil || result.Status != publish.StatusPermanentFailed || strings.Contains(strings.Join(store.updates, ","), "manual_takeover") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Contains(store.lastEvent.RawJSON, "pageInspection") || strings.Contains(store.lastEvent.RawJSON, "must not persist") {
		t.Fatalf("unsafe evidence persisted: %s", store.lastEvent.RawJSON)
	}
	if store.lastEvent.RawJSON != `{"missingRequiredControls":["content.body","media.article_cover"],"unverifiedFields":["settings.comment_enabled"]}` {
		t.Fatalf("summary=%s", store.lastEvent.RawJSON)
	}
}

func TestRunnerPersistsSafeImageUploadEvidenceSummary(t *testing.T) {
	const code = "instagram"
	original, err := platforms.GetPublisher(code)
	if err != nil {
		t.Fatal(err)
	}
	platforms.RegisterPublisher(&imageEvidencePublisher{code: code})
	t.Cleanup(func() { platforms.RegisterPublisher(original) })
	store := &memStore{}
	result, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 49, PlatformCode: code, ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, PlatformCode: code, AssetType: "instagram_account"},
	})
	if err == nil || result == nil || result.Status != publish.StatusPermanentFailed || strings.Contains(strings.Join(store.updates, ","), "manual_takeover") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	summary := store.lastEvent.RawJSON
	if strings.Contains(summary, "private.example") || strings.Contains(summary, "secret") {
		t.Fatalf("unsafe upload evidence persisted: %s", summary)
	}
	for _, token := range []string{"contentImagesUploadedSelectionVerified", "contentImagesUploadedPreviewIncrement", "criticalRevalidation", "topics"} {
		if !strings.Contains(summary, token) {
			t.Fatalf("missing safe upload evidence %s: %s", token, summary)
		}
	}
}

func TestRunnerMapsHistoricalManualLoginSignalToLoginRequired(t *testing.T) {
	const code = "instagram"
	original, err := platforms.GetPublisher(code)
	if err != nil {
		t.Fatal(err)
	}
	platforms.RegisterPublisher(&loginRequiredPublisher{code: code})
	t.Cleanup(func() { platforms.RegisterPublisher(original) })
	store := &memStore{}
	result, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 50, PlatformCode: code, ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, PlatformCode: code, AssetType: "instagram_account"},
	})
	if err != nil || result == nil || result.Status != publish.StatusLoginRequired {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Contains(strings.Join(store.updates, ","), "manual_takeover") || store.lastEventEv != "job.login_required" || result.Result.ErrorCode != "LOGIN_REQUIRED" {
		t.Fatalf("updates=%v event=%q result=%+v", store.updates, store.lastEventEv, result)
	}
}

func TestRunnerRejectsStoredModeMismatchBeforeClaimOrSettingsBackfill(t *testing.T) {
	store := &memStore{mode: publish.ModePrepare}
	_, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:        &db.PublishJob{ID: 90, PlatformCode: "xhs", ContentID: 1, Mode: publish.ModePrepare},
		Content:    &db.Content{ID: 1, Type: "graph_text", Caption: "mode guard"},
		Asset:      &db.AccountAsset{ID: 2, AccountID: 3, PlatformCode: "xhs"},
		AutoSubmit: true,
	})
	if err == nil || !strings.Contains(err.Error(), "JOB_MODE_MISMATCH") {
		t.Fatalf("mode mismatch error=%v", err)
	}
	if store.claimed || store.settingsJSON != "" || store.events != 0 || store.records != 0 {
		t.Fatalf("mode mismatch mutated execution state: %+v", store)
	}
}

func TestRunnerRejectsUnsupportedXHSAutoSubmitWithoutStateMutation(t *testing.T) {
	store := &memStore{}
	result, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:        &db.PublishJob{ID: 92, PlatformCode: "xhs", ContentID: 1, Mode: publish.ModeSubmit},
		Content:    &db.Content{ID: 1, Type: "graph_text", Caption: "manual only"},
		Asset:      &db.AccountAsset{ID: 2, AccountID: 3, PlatformCode: "xhs"},
		AutoSubmit: true,
	})
	if err == nil || !strings.Contains(err.Error(), "AUTO_SUBMIT_NOT_SUPPORTED") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result == nil || result.Status != publish.StatusPermanentFailed ||
		store.lastCode != "AUTO_SUBMIT_NOT_SUPPORTED" ||
		store.submitAttempts != 0 || store.runtimeBusy {
		t.Fatalf("result=%+v store=%+v", result, store)
	}
	if store.records != 0 {
		t.Fatalf("unfrozen XHS auto-submit created %d publish records", store.records)
	}
}

func TestRunnerExplicitPublisherCannotBypassWeChatMPFrozenSubmitPolicy(t *testing.T) {
	for _, code := range []string{"wechat_mp"} {
		t.Run(code, func(t *testing.T) {
			store := &memStore{}
			publisher := &recordingAutoSubmitPublisher{code: code}
			result, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
				Job:        &db.PublishJob{ID: 93, PlatformCode: code, ContentID: 1, Mode: publish.ModeSubmit},
				Content:    &db.Content{ID: 1, Type: "article", Title: "safe title", Body: "safe body"},
				Asset:      &db.AccountAsset{ID: 2, AccountID: 3, PlatformCode: code},
				AutoSubmit: true,
				Publisher:  publisher,
			})
			if err == nil || !strings.Contains(err.Error(), "PUBLISH_FROZEN_SNAPSHOT_REQUIRED") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if result == nil || result.Status != publish.StatusPermanentFailed || store.lastCode != "PUBLISH_FROZEN_SNAPSHOT_REQUIRED" || publisher.called {
				t.Fatalf("result=%+v store=%+v publisher.called=%v", result, store, publisher.called)
			}
		})
	}
}

func TestRunnerRejectsRealSubmitWithoutFrozenPlanTarget(t *testing.T) {
	for _, code := range []string{"instagram", "youtube", "tiktok"} {
		t.Run(code, func(t *testing.T) {
			store := &memStore{}
			publisher := &recordingAutoSubmitPublisher{code: code}
			assetType := "browser_account"
			contentType := "video"
			if code == "instagram" {
				assetType = "instagram_account"
				contentType = "graph_text"
			}
			result, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
				Job:        &db.PublishJob{ID: 94, PlatformCode: code, ContentID: 1, Mode: publish.ModeSubmit},
				Content:    &db.Content{ID: 1, Type: contentType, Caption: "unfrozen submit"},
				Asset:      &db.AccountAsset{ID: 2, AccountID: 3, PlatformCode: code, AssetType: assetType},
				AutoSubmit: true,
				Publisher:  publisher,
			})
			if err == nil || !strings.Contains(err.Error(), "PUBLISH_FROZEN_SNAPSHOT_REQUIRED") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if result == nil || result.Status != publish.StatusPermanentFailed ||
				store.submitAttempts != 0 || store.runtimeBusy || publisher.called {
				t.Fatalf("result=%+v store=%+v publisher.called=%v", result, store, publisher.called)
			}
		})
	}
}

type lateSuccessPublisher struct {
	code    string
	started chan struct{}
	release chan struct{}
}

func (p *lateSuccessPublisher) Platform() string { return p.code }
func (p *lateSuccessPublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}
func (p *lateSuccessPublisher) Validate(platforms.PublishRequest) error { return nil }
func (p *lateSuccessPublisher) Publish(context.Context, platforms.PublishRequest) (*platforms.PublishResult, error) {
	close(p.started)
	<-p.release
	return &platforms.PublishResult{Status: string(publish.StatusPublished), Step: "submitted"}, nil
}

func (p *blockingPublisher) Platform() string { return p.code }

func (p *blockingPublisher) Strategy() platforms.PublishStrategy {
	return platforms.StrategyBrowser
}

func (p *blockingPublisher) Validate(platforms.PublishRequest) error { return nil }

func (p *blockingPublisher) Publish(ctx context.Context, _ platforms.PublishRequest) (*platforms.PublishResult, error) {
	close(p.started)
	<-ctx.Done()
	return &platforms.PublishResult{
		Status:       string(publish.StatusRetryableFailed),
		Step:         "browser_attached",
		ErrorCode:    "CDP_CONNECT",
		ErrorMessage: ctx.Err().Error(),
	}, ctx.Err()
}

func TestRunnerCancelsActiveAdapterWhenJobCancelIsRequested(t *testing.T) {
	const platformCode = "pdd"
	original, err := platforms.GetPublisher(platformCode)
	if err != nil {
		t.Fatal(err)
	}
	blocking := &blockingPublisher{code: platformCode, started: make(chan struct{})}
	platforms.RegisterPublisher(blocking)
	t.Cleanup(func() { platforms.RegisterPublisher(original) })

	store := &memStore{}
	runner := &publish.Runner{Store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	var result *publish.RunResult
	var runErr error
	go func() {
		defer close(done)
		result, runErr = runner.Run(ctx, publish.RunInput{
			Job:     &db.PublishJob{ID: 91, PlatformCode: platformCode, ContentID: 1},
			Content: &db.Content{ID: 1, Type: "graph_text", Caption: "cancel me"},
			Asset:   &db.AccountAsset{ID: 2, AccountID: 3, PlatformCode: platformCode},
		})
	}()

	select {
	case <-blocking.started:
	case <-time.After(time.Second):
		t.Fatal("publisher did not start")
	}
	store.mu.Lock()
	store.cancelled = true
	store.mu.Unlock()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop cancelled adapter")
	}
	if runErr != nil {
		t.Fatalf("cancelled run returned error: %v", runErr)
	}
	if result == nil || result.Status != publish.StatusCancelled {
		t.Fatalf("result=%+v; want cancelled", result)
	}
	if store.lastEventEv != "job.cancelled" {
		t.Fatalf("event=%q; want job.cancelled", store.lastEventEv)
	}
}

func TestRunnerPersistedCancellationWinsOverLateAdapterSuccess(t *testing.T) {
	const platformCode = "pdd"
	original, err := platforms.GetPublisher(platformCode)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &lateSuccessPublisher{code: platformCode, started: make(chan struct{}), release: make(chan struct{})}
	platforms.RegisterPublisher(publisher)
	t.Cleanup(func() { platforms.RegisterPublisher(original) })

	store := &memStore{}
	done := make(chan struct{})
	var result *publish.RunResult
	var runErr error
	go func() {
		defer close(done)
		result, runErr = (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
			Job:     &db.PublishJob{ID: 92, PlatformCode: platformCode, ContentID: 1},
			Content: &db.Content{ID: 1, Type: "graph_text", Caption: "cancel wins"},
			Asset:   &db.AccountAsset{ID: 2, AccountID: 3, PlatformCode: platformCode},
		})
	}()
	<-publisher.started
	store.mu.Lock()
	store.cancelled = true
	store.mu.Unlock()
	close(publisher.release)
	<-done
	if runErr != nil || result == nil || result.Status != publish.StatusCancelled {
		t.Fatalf("result=%+v err=%v; want cancelled", result, runErr)
	}
	if store.records != 0 {
		t.Fatalf("late adapter success created %d publish records after cancellation", store.records)
	}
}

func TestRunnerAtomicFinalizerPreventsSuccessRecordAfterCancellationRace(t *testing.T) {
	const platformCode = "pdd"
	original, err := platforms.GetPublisher(platformCode)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &lateSuccessPublisher{code: platformCode, started: make(chan struct{}), release: make(chan struct{})}
	close(publisher.release)
	platforms.RegisterPublisher(publisher)
	t.Cleanup(func() { platforms.RegisterPublisher(original) })
	store := &memStore{cancelOnFinalize: true}
	result, runErr := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 93, PlatformCode: platformCode, ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "atomic cancel"},
		Asset:   &db.AccountAsset{ID: 2, AccountID: 3, PlatformCode: platformCode},
	})
	if runErr != nil || result == nil || result.Status != publish.StatusCancelled {
		t.Fatalf("result=%+v err=%v; want cancelled", result, runErr)
	}
	if store.finalizeCalls != 1 || store.records != 0 || store.lastEventEv != "job.cancelled" {
		t.Fatalf("finalizeCalls=%d records=%d event=%q", store.finalizeCalls, store.records, store.lastEventEv)
	}
}

func TestRunnerDryRun(t *testing.T) {
	store := &memStore{}
	r := &publish.Runner{Store: store}
	res, err := r.Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 1, PlatformCode: "instagram", ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "hi", Title: "t"},
		Asset:   &db.AccountAsset{ID: 9, AccountID: 3, Name: "ig", AssetType: "instagram_account", PlatformCode: "instagram"},
		DryRun:  true,
		TraceID: "trace-dry-run",
		TaskID:  "task-dry-run",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != publish.StatusDryRunOK {
		t.Fatalf("got %s", res.Status)
	}
	if store.records != 0 {
		t.Fatalf("dry-run must not create publish record, got %d", store.records)
	}
	if store.mode != "dry_run" {
		t.Fatalf("persisted mode=%q; want dry_run", store.mode)
	}
	if store.lastEvent.TraceID != "trace-dry-run" || store.lastEvent.TaskID != "task-dry-run" || store.lastEvent.DurationMS < 0 {
		t.Fatalf("typed terminal event missing correlation: %+v", store.lastEvent)
	}
}

func TestRunnerPassesVerifiedConnectedMetaTargetToPublisher(t *testing.T) {
	store := &memStore{connectedAsset: &db.AccountAsset{
		ID: 10, AccountID: 9, PlatformCode: "facebook", AssetType: "facebook_page",
		Name: "AlphaIntelligence", ExternalID: "1241410172381837",
	}}
	publisher := &requestCapturePublisher{code: "instagram"}
	_, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 21, PlatformCode: "instagram", ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "connected target"},
		Asset: &db.AccountAsset{
			ID: 11, AccountID: 9, PlatformCode: "instagram", AssetType: "instagram_account",
			Name: "chenpaiteng", ExternalID: "17841416733781085",
			ConnectedAssetID: 10, ConnectedAssetName: "AlphaIntelligence",
		},
		DryRun:    true,
		Publisher: publisher,
	})
	if err != nil {
		t.Fatal(err)
	}
	if publisher.req.ConnectedAssetName != "AlphaIntelligence" ||
		publisher.req.ConnectedAssetExternalID != "1241410172381837" {
		t.Fatalf("connected target request=%+v", publisher.req)
	}
}

func TestRunnerPassesImmutableComplianceJSONToMetaPublisher(t *testing.T) {
	const compliance = `[{"policy_key":"visibility","platform_key":"visibility","value":"public","status":"manual"}]`
	store := &memStore{}
	publisher := &requestCapturePublisher{code: "instagram"}
	_, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:            &db.PublishJob{ID: 22, PlatformCode: "instagram", ContentID: 1},
		Content:        &db.Content{ID: 1, Type: "graph_text", Caption: "frozen compliance"},
		Asset:          &db.AccountAsset{ID: 11, AccountID: 9, PlatformCode: "instagram", AssetType: "instagram_account"},
		DryRun:         true,
		Publisher:      publisher,
		ComplianceJSON: compliance,
	})
	if err != nil {
		t.Fatal(err)
	}
	if publisher.req.ComplianceJSON != compliance {
		t.Fatalf("publisher compliance=%q want=%q", publisher.req.ComplianceJSON, compliance)
	}
}

func TestRunnerLegacyJobKeepsComplianceJSONEmpty(t *testing.T) {
	store := &memStore{}
	publisher := &requestCapturePublisher{code: "instagram"}
	_, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:       &db.PublishJob{ID: 23, PlatformCode: "instagram", ContentID: 1},
		Content:   &db.Content{ID: 1, Type: "graph_text", Caption: "legacy job"},
		Asset:     &db.AccountAsset{ID: 11, AccountID: 9, PlatformCode: "instagram", AssetType: "instagram_account"},
		DryRun:    true,
		Publisher: publisher,
	})
	if err != nil {
		t.Fatal(err)
	}
	if publisher.req.ComplianceJSON != "" {
		t.Fatalf("legacy publisher compliance=%q; want empty", publisher.req.ComplianceJSON)
	}
}

func TestRunnerPersistsSubmitIntentAndRetainsBrowserRuntimeUntilVerification(t *testing.T) {
	useConfirmedPublisher(t, "instagram")
	store := &memStore{}
	res, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:        &db.PublishJob{ID: 11, PlanID: 1, PlanTargetID: 1, SnapshotHash: strings.Repeat("a", 64), PlatformCode: "instagram", ContentID: 1},
		Content:    &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:      &db.AccountAsset{ID: 1, AccountID: 1, PlatformCode: "instagram", AssetType: "instagram_account"},
		CDPURL:     "http://127.0.0.1:17001",
		AutoSubmit: true,
	})
	if err != nil || res == nil || res.Status != publish.StatusSubmittedPendingVerification {
		t.Fatalf("result=%+v err=%v", res, err)
	}
	if store.mode != publish.ModeSubmit || store.runtimeReleases != 0 || !store.runtimeBusy ||
		store.submitAttempts != 1 || store.submitOutcome != "verification_required" {
		t.Fatalf("mode=%q runtime releases=%d busy=%v attempts=%d outcome=%q",
			store.mode, store.runtimeReleases, store.runtimeBusy, store.submitAttempts, store.submitOutcome)
	}
}

func TestRunnerRequiresIndependentCreatorBackendEvidenceAfterSubmit(t *testing.T) {
	useConfirmedPublisher(t, "instagram")
	store := &memStore{}
	result, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:        &db.PublishJob{ID: 111, PlanID: 1, PlanTargetID: 1, SnapshotHash: strings.Repeat("a", 64), PlatformCode: "instagram", ContentID: 1, Mode: publish.ModeSubmit},
		Content:    &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:      &db.AccountAsset{ID: 1, AccountID: 1, PlatformCode: "instagram", AssetType: "instagram_account"},
		CDPURL:     "http://127.0.0.1:17001",
		AutoSubmit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Status != publish.StatusSubmittedPendingVerification || result.Result == nil || result.Result.ErrorCode != "" {
		t.Fatalf("result=%+v", result)
	}
	if store.submitAttempts != 1 || store.submitOutcome != "verification_required" {
		t.Fatalf("attempts=%d outcome=%q", store.submitAttempts, store.submitOutcome)
	}
	if store.records != 0 {
		t.Fatalf("adapter result created %d publish records before creator-backend verification", store.records)
	}
}

func TestRunnerDoesNotMarkSubmitAttemptWhenWorkerStopsBeforeClick(t *testing.T) {
	store := &memStore{}
	publisher := &imageEvidencePublisher{code: "instagram"}
	result, err := (&publish.Runner{Store: store}).Run(context.Background(), publish.RunInput{
		Job:        &db.PublishJob{ID: 112, PlanID: 1, PlanTargetID: 1, SnapshotHash: strings.Repeat("b", 64), PlatformCode: "instagram", ContentID: 1, Mode: publish.ModeSubmit},
		Content:    &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:      &db.AccountAsset{ID: 1, AccountID: 1, PlatformCode: "instagram", AssetType: "instagram_account"},
		CDPURL:     "http://127.0.0.1:17001",
		AutoSubmit: true,
		Publisher:  publisher,
	})
	if err == nil || result == nil || result.Status != publish.StatusPermanentFailed || strings.Contains(strings.Join(store.updates, ","), "manual_takeover") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if store.submitAttempts != 0 || store.submitOutcome != "" || store.runtimeBusy {
		t.Fatalf("pre-click stop burned submit gate: attempts=%d outcome=%q runtimeBusy=%v", store.submitAttempts, store.submitOutcome, store.runtimeBusy)
	}
}

func TestRunnerDouyinDryRun(t *testing.T) {
	store := &memStore{}
	r := &publish.Runner{Store: store}
	res, err := r.Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 2, PlatformCode: "douyin", ContentID: 1},
		Content: &db.Content{ID: 1, Caption: "x"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, Name: "x", PlatformCode: "douyin", AssetType: "douyin_account"},
		DryRun:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != publish.StatusDryRunOK {
		t.Fatalf("got %s", res.Status)
	}
	if store.lastEvent.TraceID == "" {
		t.Fatal("runner did not generate a trace for the terminal event")
	}
}

func TestRunnerUsesContextTraceWhenInputOmitsTrace(t *testing.T) {
	store := &memStore{}
	r := &publish.Runner{Store: store}
	ctx := trace.WithID(context.Background(), trace.ID("trace-context"))
	if _, err := r.Run(ctx, publish.RunInput{
		Job:     &db.PublishJob{ID: 20, PlatformCode: "douyin", ContentID: 1},
		Content: &db.Content{ID: 1, Caption: "x"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, Name: "x", PlatformCode: "douyin", AssetType: "douyin_account"},
		DryRun:  true,
	}); err != nil {
		t.Fatal(err)
	}
	if store.lastEvent.TraceID != "trace-context" {
		t.Fatalf("terminal trace=%q", store.lastEvent.TraceID)
	}
}

func TestRunnerMissingCDPSetsErrorCode(t *testing.T) {
	// Ensure CI simulate env does not short-circuit real CDP requirement.
	t.Setenv("AGENTPUSH_SIMULATE_PUBLISH", "")
	store := &memStore{}
	r := &publish.Runner{Store: store}
	res, err := r.Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 3, PlatformCode: "facebook", ContentID: 1},
		Content: &db.Content{ID: 1, Caption: "hi"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, Name: "page", AssetType: "facebook_page", ExternalID: "page-1"},
		// No CDP, not dry-run → adapter retryable_failed
	})
	if err == nil {
		t.Fatal("expected error without CDP")
	}
	if res == nil {
		t.Fatal("expected result")
	}
	if res.Status != publish.StatusRetryableFailed {
		t.Fatalf("status=%s", res.Status)
	}
	if store.lastCode == "" {
		t.Fatal("G2 AC3: runner must persist error_code on failure")
	}
	if res.Result != nil && res.Result.ErrorCode == "" {
		t.Fatal("result error_code empty")
	}
}

func TestRunnerFailsWhenJobEventPersistenceFails(t *testing.T) {
	store := &memStore{eventErr: errors.New("disk full")}
	r := &publish.Runner{Store: store}
	res, err := r.Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 4, PlatformCode: "xhs", ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, AssetType: "xhs_account"},
		DryRun:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "EVENT_PERSIST_FAILED") {
		t.Fatalf("err=%v, want EVENT_PERSIST_FAILED", err)
	}
	if res != nil && res.Status == publish.StatusPublished {
		t.Fatalf("must not report published when event persistence fails: %+v", res)
	}
	if store.records != 0 {
		t.Fatalf("record written after event failure: %d", store.records)
	}
	if got := store.updates[len(store.updates)-1]; !strings.HasPrefix(got, "failed:") {
		t.Fatalf("preflight event failure left non-terminal state: %q", got)
	}
}

func TestRunnerFailsWhenJobStatePersistenceFails(t *testing.T) {
	store := &memStore{updateErr: errors.New("read only")}
	r := &publish.Runner{Store: store}
	_, err := r.Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 5, PlatformCode: "xhs", ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, AssetType: "xhs_account"},
		DryRun:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "JOB_PERSIST_FAILED") {
		t.Fatalf("err=%v, want JOB_PERSIST_FAILED", err)
	}
	if store.events != 0 || store.records != 0 {
		t.Fatalf("writes continued after job state failure: events=%d records=%d", store.events, store.records)
	}
}

func TestRunnerFailsClosedWhenVerificationEventPersistenceFails(t *testing.T) {
	useConfirmedPublisher(t, "instagram")
	store := &memStore{eventErr: errors.New("event sink unavailable"), eventErrAt: 2}
	r := &publish.Runner{Store: store}
	res, err := r.Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 6, PlatformCode: "instagram", ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, AssetType: "instagram_account"},
	})
	if err == nil || !strings.Contains(err.Error(), "EVENT_PERSIST_FAILED") {
		t.Fatalf("verification event persistence err=%v", err)
	}
	if res == nil || res.Status != publish.StatusAwaitingReview {
		t.Fatalf("verification outcome=%+v", res)
	}
	if store.records != 0 {
		t.Fatalf("unverified publish record count=%d", store.records)
	}
	if got := store.updates[len(store.updates)-1]; !strings.HasPrefix(got, "awaiting_review:") {
		t.Fatalf("terminal state=%q, want awaiting verification", got)
	}
}

func TestRunnerDoesNotCreatePublishRecordFromAdapterResult(t *testing.T) {
	useConfirmedPublisher(t, "facebook")
	store := &memStore{recordErr: errors.New("database full")}
	r := &publish.Runner{Store: store}
	res, err := r.Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 7, PlatformCode: "facebook", ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, AssetType: "facebook_page"},
	})
	if err != nil {
		t.Fatalf("awaiting verification err=%v", err)
	}
	if res == nil || res.Status != publish.StatusAwaitingReview {
		t.Fatalf("verification outcome=%+v", res)
	}
	if len(res.WarningCodes) != 0 || store.records != 0 {
		t.Fatalf("warnings=%v", res.WarningCodes)
	}
	if store.lastEventEv != "job.awaiting_review" || store.lastEvent.ErrorCode != "PUBLISH_VERIFICATION_REQUIRED" {
		t.Fatalf("verification requirement was not persisted: %+v", store.lastEvent)
	}
}

func TestRunnerFailsClosedWhenAtomicTerminalStateFails(t *testing.T) {
	useConfirmedPublisher(t, "instagram")
	store := &memStore{updateErr: errors.New("database locked"), updateErrAt: 2}
	r := &publish.Runner{Store: store}
	res, err := r.Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 8, PlatformCode: "instagram", ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, AssetType: "instagram_account"},
	})
	if err == nil || !strings.Contains(err.Error(), "database locked") {
		t.Fatalf("atomic finalization error=%v", err)
	}
	if res == nil || res.Status != publish.StatusAwaitingReview || store.records != 0 {
		t.Fatalf("result=%+v records=%d; finalizer failure must not create a record", res, store.records)
	}
}

func TestRunnerRejectsConcurrentBrowserJobAsRetryable(t *testing.T) {
	store := &memStore{runtimeBusy: true, runtimeJobID: 999}
	r := &publish.Runner{Store: store}
	res, err := r.Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 10, PlatformCode: "douyin", ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, PlatformCode: "douyin", AssetType: "douyin_account"},
		CDPURL:  "http://127.0.0.1:17001",
	})
	if err == nil || !strings.Contains(err.Error(), "BUSY_BY_JOB") {
		t.Fatalf("concurrent browser job err=%v", err)
	}
	if res == nil || res.Status != publish.StatusRetryableFailed || res.Step != "browser_lease" {
		t.Fatalf("result=%+v", res)
	}
	if store.lastCode != "BUSY_BY_JOB" || !store.lastEvent.Retryable || store.lastEventEv != "job.retryable_failed" {
		t.Fatalf("durable failure code=%q event=%+v", store.lastCode, store.lastEvent)
	}
	if store.records != 0 || store.runtimeReleases != 0 {
		t.Fatalf("records=%d releases=%d", store.records, store.runtimeReleases)
	}
}

func TestRunnerRejectsAlreadyClaimedJobBeforePublishing(t *testing.T) {
	store := &memStore{claimed: true}
	r := &publish.Runner{Store: store}
	res, err := r.Run(context.Background(), publish.RunInput{
		Job:     &db.PublishJob{ID: 9, PlatformCode: "instagram", ContentID: 1},
		Content: &db.Content{ID: 1, Type: "graph_text", Caption: "safe test"},
		Asset:   &db.AccountAsset{ID: 1, AccountID: 1, AssetType: "instagram_account"},
	})
	if err == nil || !strings.Contains(err.Error(), "JOB_NOT_CLAIMABLE") {
		t.Fatalf("err=%v, want JOB_NOT_CLAIMABLE", err)
	}
	if res == nil || res.Step != "claim" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if store.updateCalls != 0 || store.events != 0 || store.records != 0 {
		t.Fatalf("writes continued after rejected claim: updates=%d events=%d records=%d", store.updateCalls, store.events, store.records)
	}
}
