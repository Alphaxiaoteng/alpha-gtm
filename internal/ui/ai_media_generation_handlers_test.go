package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/agnes"
	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
)

type fakeAgnesMediaClient struct {
	mu               sync.Mutex
	imageData        []byte
	videoData        []byte
	imageCalls       int
	imageURLCalls    int
	createCalls      int
	statusCalls      int
	imageRequests    []agnes.ImageRequest
	imageURLRequests []agnes.ImageRequest
	videoRequests    []agnes.VideoRequest
	imageURL         string
	imageError       error
	imageURLError    error
	createError      error
	statusErrors     []error
	statusTasks      []agnes.VideoTask
	downloadErrors   []error
	downloadCalls    int
}

func (f *fakeAgnesMediaClient) GenerateImageBase64(_ context.Context, request agnes.ImageRequest) (agnes.ImageResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imageCalls++
	f.imageRequests = append(f.imageRequests, request)
	if f.imageError != nil {
		return agnes.ImageResult{}, f.imageError
	}
	return agnes.ImageResult{Data: append([]byte(nil), f.imageData...), MediaType: "image/png", Width: 8, Height: 8}, nil
}

func (f *fakeAgnesMediaClient) GenerateImageURL(_ context.Context, request agnes.ImageRequest) (agnes.ImageURLResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imageURLCalls++
	f.imageURLRequests = append(f.imageURLRequests, request)
	if f.imageURLError != nil {
		return agnes.ImageURLResult{}, f.imageURLError
	}
	return agnes.ImageURLResult{URL: f.imageURL}, nil
}

func (f *fakeAgnesMediaClient) CreateVideo(_ context.Context, request agnes.VideoRequest) (agnes.VideoTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.videoRequests = append(f.videoRequests, request)
	if f.createError != nil {
		return agnes.VideoTask{}, f.createError
	}
	return agnes.VideoTask{TaskID: "task_synthetic", VideoID: "video_synthetic", Status: agnes.VideoQueued}, nil
}

func (f *fakeAgnesMediaClient) GetVideoStatus(_ context.Context, _ string) (agnes.VideoTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	index := f.statusCalls
	f.statusCalls++
	if index < len(f.statusErrors) && f.statusErrors[index] != nil {
		return agnes.VideoTask{}, f.statusErrors[index]
	}
	if index < len(f.statusTasks) {
		return f.statusTasks[index], nil
	}
	return agnes.VideoTask{TaskID: "task_synthetic", VideoID: "video_synthetic", Status: agnes.VideoCompleted, Progress: 100, URL: "https://platform-outputs.agnes-ai.space/videos/agnes-video-v2.0/video_synthetic.mp4"}, nil
}

func (f *fakeAgnesMediaClient) DownloadVideo(_ context.Context, _ string, destination io.Writer) (agnes.DownloadResult, error) {
	f.mu.Lock()
	index := f.downloadCalls
	f.downloadCalls++
	if index < len(f.downloadErrors) && f.downloadErrors[index] != nil {
		err := f.downloadErrors[index]
		f.mu.Unlock()
		return agnes.DownloadResult{}, err
	}
	data := append([]byte(nil), f.videoData...)
	f.mu.Unlock()
	written, err := destination.Write(data)
	return agnes.DownloadResult{Bytes: int64(written), MediaType: "video/mp4"}, err
}

func newAIMediaGenerationTestServer(t *testing.T) (*Server, *fakeAgnesMediaClient) {
	t.Helper()
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	server.agnesAPIKeyConfigured = func() (bool, error) { return true, nil }
	imagePath := filepath.Join(t.TempDir(), "generated.png")
	writeAIImageFixture(t, imagePath)
	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	videoData, err := os.ReadFile(filepath.Join("..", "..", "e2e", "fixtures", "local-video.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeAgnesMediaClient{
		imageData: imageData, videoData: videoData,
		imageURL: "https://platform-outputs.agnes-ai.space/images/derived-synthetic.png",
	}
	server.agnesClient = func(time.Duration) (agnesMediaClient, error) { return fake, nil }
	server.generationPoll = time.Millisecond
	return server, fake
}

func TestAgnesCredentialEndpointIsLocalSameOriginAndNeverEchoesKey(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0, AIImageEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	syntheticKey := "synthetic-test-key-never-persist"
	var stored string
	server.agnesSaveAPIKey = func(value string) error {
		stored = value
		return nil
	}
	server.agnesAPIKeyConfigured = func() (bool, error) { return stored != "", nil }

	requestBody, _ := json.Marshal(map[string]string{"api_key": syntheticKey})
	foreign := httptest.NewRequest(http.MethodPut, "/api/ai-image/credential", bytes.NewReader(requestBody))
	foreign.RemoteAddr = "127.0.0.1:41000"
	foreign.Host = "127.0.0.1:17321"
	foreign.Header.Set("Origin", "https://attacker.invalid")
	foreign.Header.Set("Content-Type", "application/json")
	foreignRecorder := httptest.NewRecorder()
	server.ServeHTTP(foreignRecorder, foreign)
	if foreignRecorder.Code != http.StatusForbidden || stored != "" {
		t.Fatalf("foreign credential request status=%d stored=%v", foreignRecorder.Code, stored != "")
	}

	recorder := callAIImageAPI(t, server, http.MethodPut, "/api/ai-image/credential", map[string]string{"api_key": syntheticKey})
	if recorder.Code != http.StatusOK || stored != syntheticKey || strings.Contains(recorder.Body.String(), syntheticKey) || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("credential response status=%d stored=%v cache=%q body=%s", recorder.Code, stored == syntheticKey, recorder.Header().Get("Cache-Control"), recorder.Body.String())
	}
	configRecorder := callAIImageAPI(t, server, http.MethodGet, "/api/ai-image/config", nil)
	if configRecorder.Code != http.StatusOK || !strings.Contains(configRecorder.Body.String(), `"credential_configured":true`) || strings.Contains(configRecorder.Body.String(), syntheticKey) {
		t.Fatalf("config credential status=%d body=%s", configRecorder.Code, configRecorder.Body.String())
	}
	dataDir, _ := server.cfg.ResolvedDataDir()
	configBytes, readErr := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if readErr == nil && bytes.Contains(configBytes, []byte(syntheticKey)) {
		t.Fatal("credential leaked into config.json")
	}
}

func TestAIMediaGenerationPersistsOnlyRealImageArtifact(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	prompt := "synthetic still life prompt"
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]string{"kind": "image", "prompt": prompt})
	if recorder.Code != http.StatusAccepted || strings.Contains(recorder.Body.String(), prompt) {
		t.Fatalf("queue image status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var queued aiMediaGenerationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	if queued.ID <= 0 || queued.Kind != "image" {
		t.Fatalf("queued=%+v", queued)
	}
	completed := waitForAIMediaGeneration(t, server, queued.ID)
	if completed.Status != "succeeded" || completed.Progress != 100 || completed.OutputAsset == nil || completed.OutputAsset.Kind != "image" {
		t.Fatalf("completed=%+v", completed)
	}
	if _, _, _, err := decodeAIImage(filepath.Join(mustAIImageAssetDir(t, server), completed.OutputAsset.Name)); err != nil {
		t.Fatalf("generated output is not a real image: %v", err)
	}
	fake.mu.Lock()
	imageCalls := fake.imageCalls
	fake.mu.Unlock()
	if imageCalls != 1 {
		t.Fatalf("image calls=%d", imageCalls)
	}
}

func TestAIMediaGenerationUsesExplicitValidatedModelAndOutputSize(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]string{
		"kind": "image", "mode": "text_to_image", "prompt": "portrait composition",
		"model": agnes.ImageModel20Flash, "output_size": "3:4",
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("queue image status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var queued aiMediaGenerationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	completed := waitForAIMediaGeneration(t, server, queued.ID)
	if completed.Status != db.GenerationJobSucceeded || completed.Model != agnes.ImageModel20Flash || completed.OutputSize != "3:4" {
		t.Fatalf("completed=%+v", completed)
	}
	fake.mu.Lock()
	requests := append([]agnes.ImageRequest(nil), fake.imageRequests...)
	fake.mu.Unlock()
	if len(requests) != 1 || requests[0].Model != agnes.ImageModel20Flash || requests[0].Size != "768x1024" || requests[0].Ratio != "" {
		t.Fatalf("image request=%+v", requests)
	}
}

func TestAIMediaVideoBatchUsesSelectedPortraitSize(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations/batch", map[string]any{
		"request_id":  "gen-video-portrait-batch-0001",
		"kind":        "video",
		"mode":        "text_to_video",
		"model":       agnes.VideoModel20,
		"output_size": "9:16",
		"prompt":      "portrait social video",
		"references":  []any{},
		"count":       2,
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("queue video batch status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var batch aiMediaGenerationBatchResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	for _, queued := range batch.Items {
		if completed := waitForAIMediaGeneration(t, server, queued.ID); completed.Status != db.GenerationJobSucceeded {
			t.Fatalf("completed=%+v", completed)
		}
	}
	fake.mu.Lock()
	requests := append([]agnes.VideoRequest(nil), fake.videoRequests...)
	fake.mu.Unlock()
	if len(requests) != 2 ||
		requests[0].Width != 576 || requests[0].Height != 1024 || requests[0].NumFrames != 121 || requests[0].FrameRate != 24 ||
		requests[1].Width != 576 || requests[1].Height != 1024 || requests[1].NumFrames != 121 || requests[1].FrameRate != 24 {
		t.Fatalf("video requests=%+v", requests)
	}
}

func TestAIMediaVideoDurationTenUses241FramesAndSurvivesResponse(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]any{
		"request_id":       "gen-video-duration-api-0001",
		"kind":             "video",
		"mode":             "text_to_video",
		"model":            agnes.VideoModel20,
		"output_size":      "9:16",
		"duration_seconds": 10,
		"prompt":           "portrait abstract motion",
		"references":       []any{},
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("queue video status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var queued aiMediaGenerationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	if queued.DurationSeconds != 10 {
		t.Fatalf("queued duration=%d", queued.DurationSeconds)
	}
	completed := waitForAIMediaGeneration(t, server, queued.ID)
	if completed.Status != db.GenerationJobSucceeded || completed.DurationSeconds != 10 {
		t.Fatalf("completed=%+v", completed)
	}
	fake.mu.Lock()
	requests := append([]agnes.VideoRequest(nil), fake.videoRequests...)
	fake.mu.Unlock()
	if len(requests) != 1 || requests[0].NumFrames != 241 || requests[0].FrameRate != 24 {
		t.Fatalf("video requests=%+v", requests)
	}
}

func TestAIMediaVideoDurationParticipatesInIdempotencyHash(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	requestID := "gen-video-duration-idempotent-0001"
	body := map[string]any{
		"request_id":       requestID,
		"kind":             "video",
		"mode":             "text_to_video",
		"model":            agnes.VideoModel20,
		"output_size":      "9:16",
		"duration_seconds": 10,
		"prompt":           "portrait abstract motion",
		"references":       []any{},
	}
	first := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", body)
	second := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", body)
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted {
		t.Fatalf("idempotent duration statuses first=%d second=%d", first.Code, second.Code)
	}
	var firstJob, secondJob aiMediaGenerationResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstJob); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondJob); err != nil {
		t.Fatal(err)
	}
	if firstJob.ID <= 0 || secondJob.ID != firstJob.ID {
		t.Fatalf("duration request replayed different jobs first=%+v second=%+v", firstJob, secondJob)
	}
	if completed := waitForAIMediaGeneration(t, server, firstJob.ID); completed.Status != db.GenerationJobSucceeded {
		t.Fatalf("completed=%+v", completed)
	}
	fake.mu.Lock()
	videoCalls := fake.createCalls
	fake.mu.Unlock()
	if videoCalls != 1 {
		t.Fatalf("idempotent duration called provider %d times", videoCalls)
	}

	changedDuration := map[string]any{}
	for key, value := range body {
		changedDuration[key] = value
	}
	changedDuration["duration_seconds"] = 5
	conflict := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", changedDuration)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "GENERATION_REQUEST_CONFLICT") {
		t.Fatalf("duration conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestAIMediaVideoDurationRejectsUnsupportedValuesBeforeProviderCall(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]any{
		"request_id":       "gen-video-duration-api-invalid-0001",
		"kind":             "video",
		"mode":             "text_to_video",
		"duration_seconds": 7,
		"prompt":           "portrait abstract motion",
		"references":       []any{},
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid duration status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	fake.mu.Lock()
	createCalls := fake.createCalls
	fake.mu.Unlock()
	if createCalls != 0 {
		t.Fatalf("provider called %d times for invalid duration", createCalls)
	}
}

func TestAIMediaGenerationBatchQueuesAndPersistsEveryRealArtifact(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations/batch", map[string]any{
		"request_id": "gen-ui-batch-request-0001",
		"kind":       "image",
		"mode":       "text_to_image",
		"prompt":     "three synthetic still life variants",
		"references": []any{},
		"count":      3,
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("queue batch status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var batch aiMediaGenerationBatchResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if batch.BatchID != "gen-ui-batch-request-0001" || batch.Count != 3 || len(batch.Items) != 3 {
		t.Fatalf("batch=%+v", batch)
	}
	seen := map[int64]bool{}
	for _, queued := range batch.Items {
		if queued.ID <= 0 || seen[queued.ID] {
			t.Fatalf("invalid or duplicate queued item=%+v", queued)
		}
		seen[queued.ID] = true
		completed := waitForAIMediaGeneration(t, server, queued.ID)
		if completed.Status != db.GenerationJobSucceeded || completed.OutputAsset == nil {
			t.Fatalf("completed=%+v", completed)
		}
	}
	fake.mu.Lock()
	imageCalls := fake.imageCalls
	fake.mu.Unlock()
	if imageCalls != 3 {
		t.Fatalf("image calls=%d", imageCalls)
	}
}

func TestAIMediaGenerationPersistsOnlyRealVideoArtifact(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]string{"kind": "video", "prompt": "synthetic motion prompt"})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("queue video status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var queued aiMediaGenerationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	completed := waitForAIMediaGeneration(t, server, queued.ID)
	if completed.Status != "succeeded" || completed.OutputAsset == nil || completed.OutputAsset.Kind != "video" || completed.OutputAsset.DurationMS <= 0 {
		t.Fatalf("completed=%+v", completed)
	}
	fake.mu.Lock()
	createCalls, statusCalls := fake.createCalls, fake.statusCalls
	fake.mu.Unlock()
	if createCalls != 1 || statusCalls != 1 {
		t.Fatalf("video create=%d status=%d", createCalls, statusCalls)
	}
}

func TestAIMediaGenerationWithoutCredentialCreatesNoJob(t *testing.T) {
	server, _ := newAIMediaGenerationTestServer(t)
	server.agnesAPIKeyConfigured = func() (bool, error) { return false, nil }
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]string{"kind": "image", "prompt": "must not run"})
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), string(agnes.CodeKeyMissing)) {
		t.Fatalf("missing credential status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	jobs, err := server.db.ListRecentGenerationJobs(10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
}

func TestAIMediaGenerationRequestIDReturnsSameJobAndRejectsChangedInput(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	requestID := "gen-ui-idempotent-request-0001"
	body := map[string]string{"request_id": requestID, "kind": "image", "prompt": "one explicit intent"}
	first := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", body)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	second := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", body)
	if second.Code != http.StatusAccepted {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	var firstJob, secondJob aiMediaGenerationResponse
	if err := json.Unmarshal(first.Body.Bytes(), &firstJob); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondJob); err != nil {
		t.Fatal(err)
	}
	if firstJob.ID <= 0 || secondJob.ID != firstJob.ID || firstJob.RequestID != requestID || secondJob.RequestID != requestID {
		t.Fatalf("first=%+v second=%+v", firstJob, secondJob)
	}
	completed := waitForAIMediaGeneration(t, server, firstJob.ID)
	if completed.Status != db.GenerationJobSucceeded {
		t.Fatalf("completed=%+v", completed)
	}
	fake.mu.Lock()
	imageCalls := fake.imageCalls
	fake.mu.Unlock()
	if imageCalls != 1 {
		t.Fatalf("idempotent request called provider %d times", imageCalls)
	}

	conflict := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]string{
		"request_id": requestID, "kind": "image", "prompt": "changed input",
	})
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "GENERATION_REQUEST_CONFLICT") {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestAIMediaProviderSubmissionUncertaintyIsTerminalAndNotAutomaticallyRepeated(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	fake.createError = &agnes.Error{Code: agnes.CodeProviderUnavailable, Retryable: true}
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]string{
		"request_id": "gen-provider-unknown-0001", "kind": "video", "prompt": "one uncertain submit",
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("queue status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var queued aiMediaGenerationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	failed := waitForAIMediaGeneration(t, server, queued.ID)
	if failed.Status != db.GenerationJobFailed || failed.ErrorCode != "GENERATION_PROVIDER_STATE_UNKNOWN" {
		t.Fatalf("uncertain submit=%+v", failed)
	}
	server.resumeAIMediaGenerationJobs()
	fake.mu.Lock()
	createCalls := fake.createCalls
	fake.mu.Unlock()
	if createCalls != 1 {
		t.Fatalf("uncertain provider submission was repeated: %d", createCalls)
	}
}

func TestAIMediaImageToImageUsesOnlyManagedSHAFixedReferences(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	referencePath := filepath.Join(t.TempDir(), "reference.png")
	writeAlternateAIImageFixture(t, referencePath)
	reference, err := server.importAIImageAsset(referencePath)
	if err != nil {
		t.Fatal(err)
	}
	prompt := "change the background while preserving the supplied object"
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]any{
		"kind": "image", "mode": "image_to_image", "prompt": prompt,
		"references": []map[string]string{{"asset_name": reference.Name, "role": "product"}},
	})
	if recorder.Code != http.StatusAccepted || strings.Contains(recorder.Body.String(), prompt) {
		t.Fatalf("queue image-to-image status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var queued aiMediaGenerationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	if queued.Mode != "image_to_image" || len(queued.References) != 1 || queued.References[0].AssetName != reference.Name || queued.References[0].SHA256 != reference.SHA256 {
		t.Fatalf("queued=%+v", queued)
	}
	completed := waitForAIMediaGeneration(t, server, queued.ID)
	if completed.Status != db.GenerationJobSucceeded || completed.OutputAsset == nil {
		t.Fatalf("completed=%+v", completed)
	}
	fake.mu.Lock()
	requests := append([]agnes.ImageRequest(nil), fake.imageRequests...)
	fake.mu.Unlock()
	if len(requests) != 1 || len(requests[0].Images) != 1 || !strings.HasPrefix(requests[0].Images[0], "data:image/png;base64,") {
		t.Fatalf("image requests=%+v", requests)
	}
}

func TestAIMediaImageToImageDoesNotClaimReferenceAsGeneratedOutput(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	referencePath := filepath.Join(t.TempDir(), "unchanged-reference.png")
	writeAIImageFixture(t, referencePath)
	reference, err := server.importAIImageAsset(referencePath)
	if err != nil {
		t.Fatal(err)
	}
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]any{
		"kind": "image", "mode": "image_to_image", "prompt": "preserve this exact image",
		"references": []map[string]string{{"asset_name": reference.Name, "role": "product"}},
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("queue status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var queued aiMediaGenerationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	completed := waitForAIMediaGeneration(t, server, queued.ID)
	if completed.Status != db.GenerationJobFailed || completed.ErrorCode != "GENERATION_ARTIFACT_UNCHANGED" || completed.OutputAsset != nil {
		t.Fatalf("completed=%+v", completed)
	}
	fake.mu.Lock()
	calls := fake.imageCalls
	fake.mu.Unlock()
	if calls != 1 {
		t.Fatalf("image calls=%d", calls)
	}
}

func TestAIMediaImageToVideoRejectsClientSuppliedProviderURLBeforeJobOrNetwork(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	referencePath := filepath.Join(t.TempDir(), "first-frame.png")
	writeAIImageFixture(t, referencePath)
	reference, err := server.importAIImageAsset(referencePath)
	if err != nil {
		t.Fatal(err)
	}
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]any{
		"kind": "video", "mode": "image_to_video", "prompt": "subtle camera motion",
		"references": []map[string]string{{
			"asset_name": reference.Name, "role": "first_frame", "public_url": "https://cdn.example.com/first-frame.png",
		}},
	})
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "VALIDATION_FAILED") {
		t.Fatalf("client URL status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	jobs, err := server.db.ListRecentGenerationJobs(10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("invalid image-to-video created jobs=%+v err=%v", jobs, err)
	}
	fake.mu.Lock()
	imageURLCalls, createCalls := fake.imageURLCalls, fake.createCalls
	fake.mu.Unlock()
	if imageURLCalls != 0 || createCalls != 0 {
		t.Fatalf("invalid image-to-video reached provider: image=%d video=%d", imageURLCalls, createCalls)
	}
}

func TestAIMediaImageToVideoDerivesProviderFrameInMemoryAndPersistsRealVideo(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	referencePath := filepath.Join(t.TempDir(), "first-frame.png")
	writeAIImageFixture(t, referencePath)
	reference, err := server.importAIImageAsset(referencePath)
	if err != nil {
		t.Fatal(err)
	}
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]any{
		"kind": "video", "mode": "image_to_video", "prompt": "subtle camera motion",
		"references": []map[string]string{{"asset_name": reference.Name, "role": "first_frame"}},
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("queue status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var queued aiMediaGenerationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	stored, err := server.db.GetGenerationJob(queued.ID)
	if err != nil || len(stored.References) != 1 {
		t.Fatalf("frozen image-to-video reference=%+v err=%v", stored, err)
	}
	storedJSON, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(storedJSON), fake.imageURL) || strings.Contains(recorder.Body.String(), fake.imageURL) {
		t.Fatal("provider handoff URL leaked into durable state or API response")
	}
	completed := waitForAIMediaGeneration(t, server, queued.ID)
	if completed.Status != db.GenerationJobSucceeded || completed.OutputAsset == nil || completed.OutputAsset.Kind != "video" || completed.OutputAsset.MimeType != "video/mp4" {
		t.Fatalf("completed image-to-video=%+v", completed)
	}
	finalStored, err := server.db.GetGenerationJob(queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	finalStoredJSON, _ := json.Marshal(finalStored)
	completedJSON, _ := json.Marshal(completed)
	if bytes.Contains(finalStoredJSON, []byte(fake.imageURL)) || bytes.Contains(completedJSON, []byte(fake.imageURL)) {
		t.Fatal("provider handoff URL leaked after generation completed")
	}
	fake.mu.Lock()
	imageRequests := append([]agnes.ImageRequest(nil), fake.imageURLRequests...)
	requests := append([]agnes.VideoRequest(nil), fake.videoRequests...)
	fake.mu.Unlock()
	if len(imageRequests) != 1 || imageRequests[0].Model != agnes.ImageModel21Flash || imageRequests[0].Prompt != derivedFirstFramePrompt || len(imageRequests[0].Images) != 1 || !strings.HasPrefix(imageRequests[0].Images[0], "data:image/png;base64,") {
		t.Fatalf("derived frame requests=%+v", imageRequests)
	}
	if len(requests) != 1 || requests[0].ImageURL != fake.imageURL {
		t.Fatalf("video requests=%+v", requests)
	}
}

func TestAIMediaImageToVideoRestartBeforeRemoteTaskFailsWithoutRepeatingProviderCall(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	referencePath := filepath.Join(t.TempDir(), "interrupted-first-frame.png")
	writeAIImageFixture(t, referencePath)
	reference, err := server.importAIImageAsset(referencePath)
	if err != nil {
		t.Fatal(err)
	}
	item, err := server.db.GetMediaLibraryItemBySHA256(reference.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	job, err := server.db.CreateGenerationJobWithReferences(
		"video", db.GenerationModeImageToVideo, "agnes", agnes.VideoModel20, "slow camera push",
		[]db.GenerationReference{{MediaID: item.ID, AssetName: reference.Name, Role: "first_frame", SHA256: reference.SHA256}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.db.MarkGenerationJobRunning(job.ID); err != nil {
		t.Fatal(err)
	}

	server.resumeAIMediaGenerationJobs()
	stored, err := server.db.GetGenerationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.GenerationJobFailed || stored.ErrorCode != "GENERATION_PROVIDER_STATE_UNKNOWN" {
		t.Fatalf("interrupted job=%+v", stored)
	}
	fake.mu.Lock()
	imageURLCalls, createCalls := fake.imageURLCalls, fake.createCalls
	fake.mu.Unlock()
	if imageURLCalls != 0 || createCalls != 0 {
		t.Fatalf("restart repeated provider calls: image=%d video=%d", imageURLCalls, createCalls)
	}
}

func TestAIMediaVideoStatusRetriesTransientErrorsAndRecovers(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	fake.statusErrors = []error{
		&agnes.Error{Code: agnes.CodeRateLimited, HTTPStatus: http.StatusTooManyRequests, Retryable: true},
		&agnes.Error{Code: agnes.CodeProviderUnavailable, HTTPStatus: http.StatusServiceUnavailable, Retryable: true},
	}
	job := createRunningRemoteVideoJob(t, server)
	server.executeAIMediaGeneration(context.Background(), job.ID)
	stored, err := server.db.GetGenerationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.GenerationJobSucceeded || stored.OutputMediaID == nil {
		t.Fatalf("transient status recovery=%+v", stored)
	}
	fake.mu.Lock()
	statusCalls := fake.statusCalls
	fake.mu.Unlock()
	if statusCalls != 3 {
		t.Fatalf("status calls=%d", statusCalls)
	}
}

func TestAIMediaVideoDownloadRetriesInSameWorkerWithoutCreatingAnotherVideo(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	fake.downloadErrors = []error{
		&agnes.Error{Code: agnes.CodeProviderUnavailable, Retryable: true},
		&agnes.Error{Code: agnes.CodeRequestFailed, Retryable: true},
	}
	job := createRunningRemoteVideoJob(t, server)
	server.executeAIMediaGeneration(context.Background(), job.ID)
	stored, err := server.db.GetGenerationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.GenerationJobSucceeded || stored.OutputMediaID == nil {
		t.Fatalf("download retry result=%+v", stored)
	}
	fake.mu.Lock()
	downloadCalls, createCalls := fake.downloadCalls, fake.createCalls
	fake.mu.Unlock()
	if downloadCalls != 3 || createCalls != 0 {
		t.Fatalf("download calls=%d create calls=%d", downloadCalls, createCalls)
	}
}

func TestAIMediaVideoLocalImportFailureDoesNotCreateAnotherProviderVideo(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	fake.videoData = []byte("not a decodable video")
	recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", map[string]string{
		"request_id": "gen-local-import-failure-0001", "kind": "video", "prompt": "single provider video",
	})
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("queue status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var queued aiMediaGenerationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	failed := waitForAIMediaGeneration(t, server, queued.ID)
	if failed.Status != db.GenerationJobFailed {
		t.Fatalf("local import failure=%+v", failed)
	}
	server.executeAIMediaGeneration(context.Background(), queued.ID)
	fake.mu.Lock()
	createCalls := fake.createCalls
	fake.mu.Unlock()
	if createCalls != 1 {
		t.Fatalf("local import failure recreated provider video: %d", createCalls)
	}
}

func TestAIMediaVideoStatusKeepsRetryingTransientErrorsUntilRecovery(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	for range maxAIMediaRetryableStatusErrors + 2 {
		fake.statusErrors = append(fake.statusErrors, &agnes.Error{Code: agnes.CodeRequestFailed, Retryable: true})
	}
	job := createRunningRemoteVideoJob(t, server)
	server.executeAIMediaGeneration(context.Background(), job.ID)
	stored, err := server.db.GetGenerationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.GenerationJobSucceeded || stored.OutputMediaID == nil {
		t.Fatalf("transient status recovery=%+v", stored)
	}
	fake.mu.Lock()
	statusCalls := fake.statusCalls
	fake.mu.Unlock()
	if statusCalls != maxAIMediaRetryableStatusErrors+3 {
		t.Fatalf("status calls=%d", statusCalls)
	}

	server, _ = newAIMediaGenerationTestServer(t)
	stopped := createRunningRemoteVideoJob(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	server.executeAIMediaGeneration(ctx, stopped.ID)
	stored, err = server.db.GetGenerationJob(stopped.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.GenerationJobRunning || stored.CompletedAt != "" {
		t.Fatalf("service stop must remain recoverable: %+v", stored)
	}
}

func TestAIMediaVideoKeepsPollingLongTaskButOfficialFailureIsTerminal(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	for range 60 {
		fake.statusTasks = append(fake.statusTasks, agnes.VideoTask{
			TaskID: "task_synthetic", VideoID: "video_synthetic", Status: agnes.VideoQueued,
		})
	}
	job := createRunningRemoteVideoJob(t, server)
	server.executeAIMediaGeneration(context.Background(), job.ID)
	stored, err := server.db.GetGenerationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.GenerationJobSucceeded || stored.OutputMediaID == nil {
		t.Fatalf("long-running provider task did not complete: %+v", stored)
	}

	server, fake = newAIMediaGenerationTestServer(t)
	fake.statusErrors = []error{&agnes.Error{Code: agnes.CodeVideoFailed, Retryable: false}}
	job = createRunningRemoteVideoJob(t, server)
	server.executeAIMediaGeneration(context.Background(), job.ID)
	stored, err = server.db.GetGenerationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != db.GenerationJobFailed || stored.ErrorCode != string(agnes.CodeVideoFailed) {
		t.Fatalf("official provider failure must be terminal: %+v", stored)
	}
}

func TestGenerationReferenceBytesMustMatchFrozenSnapshot(t *testing.T) {
	server, _ := newAIMediaGenerationTestServer(t)
	referencePath := filepath.Join(t.TempDir(), "reference.png")
	writeAIImageFixture(t, referencePath)
	reference, err := server.importAIImageAsset(referencePath)
	if err != nil {
		t.Fatal(err)
	}
	item, err := server.db.GetMediaLibraryItemBySHA256(reference.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(item.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	ref := db.GenerationReference{MediaID: item.ID, AssetName: reference.Name, Role: "product", SHA256: reference.SHA256}
	if err := validateGenerationReferenceBytes(data, ref, item.Mime, item.SizeBytes); err != nil {
		t.Fatalf("valid frozen bytes rejected: %v", err)
	}
	changed := append([]byte(nil), data...)
	changed[len(changed)-1] ^= 0xff
	if err := validateGenerationReferenceBytes(changed, ref, item.Mime, item.SizeBytes); err == nil {
		t.Fatal("changed bytes matched frozen reference")
	}
}

func TestAIMediaRegistryJSONReferencesAreStrictlyDecoded(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	referencePath := filepath.Join(t.TempDir(), "reference.png")
	writeAlternateAIImageFixture(t, referencePath)
	reference, err := server.importAIImageAsset(referencePath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := server.enqueueAIMediaGeneration(context.Background(), map[string]interface{}{
		"request_id": "gen-registry-reference-0001",
		"kind":       "image", "mode": "image_to_image", "prompt": "preserve the product",
		"references": []interface{}{map[string]interface{}{"asset_name": reference.Name, "role": "product"}},
	})
	if err != nil || result == nil || !result.OK {
		t.Fatalf("registry JSON references result=%+v err=%v", result, err)
	}
	jobID, _ := result.Data["job_id"].(int64)
	completed := waitForAIMediaGeneration(t, server, jobID)
	if completed.Status != db.GenerationJobSucceeded {
		t.Fatalf("registry JSON generation=%+v", completed)
	}

	result, err = server.enqueueAIMediaGeneration(context.Background(), map[string]interface{}{
		"request_id": "gen-registry-reference-0002",
		"kind":       "image", "mode": "image_to_image", "prompt": "must reject unknown fields",
		"references": []interface{}{map[string]interface{}{"asset_name": reference.Name, "role": "product", "path": "/tmp/forbidden"}},
	})
	if err == nil || result == nil || result.OK || result.Error != "GENERATION_REFERENCE_INVALID" {
		t.Fatalf("unknown reference field result=%+v err=%v", result, err)
	}

	result, err = server.enqueueAIMediaGeneration(context.Background(), map[string]interface{}{
		"request_id": "gen-registry-reference-0003",
		"kind":       "video", "mode": "image_to_video", "prompt": "slow camera push",
		"references": []interface{}{map[string]interface{}{
			"asset_name": reference.Name, "role": "first_frame",
		}},
	})
	if err != nil || result == nil || !result.OK {
		t.Fatalf("registry image-to-video result=%+v err=%v", result, err)
	}
	videoJobID, _ := result.Data["job_id"].(int64)
	if completed := waitForAIMediaGeneration(t, server, videoJobID); completed.Status != db.GenerationJobSucceeded {
		t.Fatalf("registry image-to-video=%+v", completed)
	}
	fake.mu.Lock()
	videoRequests := append([]agnes.VideoRequest(nil), fake.videoRequests...)
	fake.mu.Unlock()
	if len(videoRequests) != 1 || videoRequests[0].ImageURL != fake.imageURL {
		t.Fatalf("registry video requests=%+v", videoRequests)
	}
}

func createRunningRemoteVideoJob(t *testing.T, server *Server) *db.GenerationJob {
	t.Helper()
	job, err := server.db.CreateGenerationJob("video", "agnes", agnes.VideoModel20, "synthetic motion")
	if err != nil {
		t.Fatal(err)
	}
	if err := server.db.MarkGenerationJobRunning(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := server.db.MarkGenerationJobRemote(job.ID, "task_synthetic", "video_synthetic"); err != nil {
		t.Fatal(err)
	}
	job, err = server.db.GetGenerationJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestAIMediaGenerationRejectsUnmanagedOrConflictingReferencesWithoutJob(t *testing.T) {
	server, fake := newAIMediaGenerationTestServer(t)
	for _, body := range []map[string]any{
		{"kind": "image", "mode": "image_to_image", "prompt": "synthetic", "references": []map[string]string{{"asset_name": "../outside.png", "role": "product"}}},
		{"kind": "image", "mode": "text_to_image", "prompt": "synthetic", "references": []map[string]string{{"asset_name": strings.Repeat("a", 64) + ".png", "role": "product"}}},
		{"kind": "video", "mode": "image_to_video", "prompt": "synthetic", "references": []map[string]string{}},
	} {
		recorder := callAIImageAPI(t, server, http.MethodPost, "/api/ai-image/generations", body)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid body accepted: status=%d body=%s input=%+v", recorder.Code, recorder.Body.String(), body)
		}
	}
	jobs, err := server.db.ListRecentGenerationJobs(10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	fake.mu.Lock()
	imageCalls, videoCalls := fake.imageCalls, fake.createCalls
	fake.mu.Unlock()
	if imageCalls != 0 || videoCalls != 0 {
		t.Fatalf("invalid references reached provider: image=%d video=%d", imageCalls, videoCalls)
	}
}

func waitForAIMediaGeneration(t *testing.T, server *Server, id int64) aiMediaGenerationResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := server.db.GetGenerationJob(id)
		if err == nil && (job.Status == "succeeded" || job.Status == "failed") {
			return server.aiMediaGenerationResponse(job)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("generation %d did not reach a terminal state", id)
	return aiMediaGenerationResponse{}
}

func mustAIImageAssetDir(t *testing.T, server *Server) string {
	t.Helper()
	dir, err := server.ensureAIImageAssetDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeAlternateAIImageFixture(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 210, G: uint8(x * 5), B: uint8(y * 5), A: 255})
		}
	}
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
}
