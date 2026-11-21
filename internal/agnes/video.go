package agnes

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const (
	VideoModeTI2VID    = "ti2vid"
	VideoModeKeyframes = "keyframes"
)

type VideoState string

const (
	VideoQueued     VideoState = "queued"
	VideoInProgress VideoState = "in_progress"
	VideoCompleted  VideoState = "completed"
	VideoFailed     VideoState = "failed"
)

type VideoRequest struct {
	Model             string
	Prompt            string
	ImageURL          string
	Mode              string
	Width             int
	Height            int
	NumFrames         int
	FrameRate         float64
	NumInferenceSteps int
	Seed              *int64
	NegativePrompt    string
	Keyframes         []string
}

type VideoTask struct {
	ID        string
	TaskID    string
	VideoID   string
	Object    string
	Model     string
	Status    VideoState
	Progress  int
	CreatedAt int64
	Seconds   string
	Size      string
	URL       string
}

type videoRequestPayload struct {
	Model             string          `json:"model"`
	Prompt            string          `json:"prompt"`
	ImageURL          string          `json:"image,omitempty"`
	Mode              string          `json:"mode,omitempty"`
	Width             int             `json:"width,omitempty"`
	Height            int             `json:"height,omitempty"`
	NumFrames         int             `json:"num_frames,omitempty"`
	FrameRate         float64         `json:"frame_rate,omitempty"`
	NumInferenceSteps int             `json:"num_inference_steps,omitempty"`
	Seed              *int64          `json:"seed,omitempty"`
	NegativePrompt    string          `json:"negative_prompt,omitempty"`
	ExtraBody         *videoExtraBody `json:"extra_body,omitempty"`
}

type videoExtraBody struct {
	Images []string `json:"image"`
	Mode   string   `json:"mode"`
}

type videoResponse struct {
	ID        string          `json:"id"`
	TaskID    string          `json:"task_id"`
	VideoID   string          `json:"video_id"`
	Object    string          `json:"object"`
	Model     string          `json:"model"`
	Status    string          `json:"status"`
	Progress  int             `json:"progress"`
	CreatedAt int64           `json:"created_at"`
	Seconds   string          `json:"seconds"`
	Size      string          `json:"size"`
	URL       string          `json:"url"`
	Error     json.RawMessage `json:"error"`
}

// CreateVideo creates one asynchronous Agnes video task.
func (c *Client) CreateVideo(ctx context.Context, request VideoRequest) (VideoTask, error) {
	request, err := validateVideoRequest(request)
	if err != nil {
		return VideoTask{}, err
	}
	payload := videoRequestPayload{
		Model:             request.Model,
		Prompt:            request.Prompt,
		ImageURL:          request.ImageURL,
		Mode:              request.Mode,
		Width:             request.Width,
		Height:            request.Height,
		NumFrames:         request.NumFrames,
		FrameRate:         request.FrameRate,
		NumInferenceSteps: request.NumInferenceSteps,
		Seed:              request.Seed,
		NegativePrompt:    request.NegativePrompt,
	}
	if request.Mode == VideoModeKeyframes {
		payload.Mode = ""
		payload.ExtraBody = &videoExtraBody{Images: request.Keyframes, Mode: VideoModeKeyframes}
	}
	var response videoResponse
	if err := c.doJSON(ctx, http.MethodPost, c.videoCreateEndpoint, payload, c.maxMetadataRequestBytes, c.maxMetadataResponseBytes, &response); err != nil {
		return VideoTask{}, err
	}
	return c.validateVideoResponse(response, "")
}

// GetVideoStatus polls the official video_id endpoint once. Scheduling and
// persistence remain the caller's responsibility.
func (c *Client) GetVideoStatus(ctx context.Context, videoID string) (VideoTask, error) {
	videoID = strings.TrimSpace(videoID)
	if !validProviderID(videoID) {
		return VideoTask{}, newError(CodeInvalidInput, 0, false, nil)
	}
	endpoint, err := url.Parse(c.videoStatusEndpoint)
	if err != nil {
		return VideoTask{}, newError(CodeInvalidInput, 0, false, err)
	}
	query := endpoint.Query()
	query.Set("video_id", videoID)
	endpoint.RawQuery = query.Encode()
	var response videoResponse
	if err := c.doJSON(ctx, http.MethodGet, endpoint.String(), nil, c.maxMetadataRequestBytes, c.maxMetadataResponseBytes, &response); err != nil {
		return VideoTask{}, err
	}
	return c.validateVideoResponse(response, videoID)
}

func (c *Client) validateVideoResponse(response videoResponse, expectedVideoID string) (VideoTask, error) {
	taskID := strings.TrimSpace(response.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(response.ID)
	}
	videoID := strings.TrimSpace(response.VideoID)
	state := VideoState(strings.TrimSpace(response.Status))
	if !validProviderID(taskID) || !validProviderID(videoID) || (expectedVideoID != "" && videoID != expectedVideoID) || response.Object != "video" || response.Model != VideoModel20 || response.Progress < 0 || response.Progress > 100 || strings.TrimSpace(response.Seconds) == "" || strings.TrimSpace(response.Size) == "" {
		return VideoTask{}, newError(CodeInvalidResponse, 0, false, nil)
	}
	switch state {
	case VideoQueued, VideoInProgress, VideoCompleted, VideoFailed:
	default:
		return VideoTask{}, newError(CodeInvalidResponse, 0, false, nil)
	}
	task := VideoTask{
		ID:        strings.TrimSpace(response.ID),
		TaskID:    taskID,
		VideoID:   videoID,
		Object:    response.Object,
		Model:     response.Model,
		Status:    state,
		Progress:  response.Progress,
		CreatedAt: response.CreatedAt,
		Seconds:   strings.TrimSpace(response.Seconds),
		Size:      strings.TrimSpace(response.Size),
		URL:       strings.TrimSpace(response.URL),
	}
	if state == VideoCompleted {
		if task.URL == "" {
			return VideoTask{}, newError(CodeInvalidResponse, 0, false, nil)
		}
		if _, err := c.validateVideoDownloadURL(task.URL); err != nil {
			return VideoTask{}, err
		}
	}
	if state == VideoFailed {
		return task, newError(CodeVideoFailed, 0, false, nil)
	}
	return task, nil
}

func validateVideoRequest(request VideoRequest) (VideoRequest, error) {
	request.Model = strings.TrimSpace(request.Model)
	if request.Model != VideoModel20 {
		return VideoRequest{}, newError(CodeUnsupportedModel, 0, false, nil)
	}
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Prompt == "" || len(request.Prompt) > maxPromptBytes {
		return VideoRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	request.NegativePrompt = strings.TrimSpace(request.NegativePrompt)
	if len(request.NegativePrompt) > maxPromptBytes {
		return VideoRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	if (request.Width == 0) != (request.Height == 0) {
		return VideoRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	if request.Width != 0 && (request.Width < 256 || request.Height < 256 || request.Width > 4096 || request.Height > 4096 || request.Width%64 != 0 || request.Height%64 != 0) {
		return VideoRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	if request.NumFrames != 0 && (request.NumFrames < 1 || request.NumFrames > 441 || (request.NumFrames-1)%8 != 0) {
		return VideoRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	if request.FrameRate != 0 && (request.FrameRate < 1 || request.FrameRate > 60) {
		return VideoRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	if request.NumInferenceSteps < 0 || request.NumInferenceSteps > 1000 {
		return VideoRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	request.ImageURL = strings.TrimSpace(request.ImageURL)
	if request.ImageURL != "" {
		if err := validatePublicHTTPSURL(request.ImageURL); err != nil {
			return VideoRequest{}, err
		}
	}
	request.Mode = strings.TrimSpace(request.Mode)
	if request.Mode != "" && request.Mode != VideoModeTI2VID && request.Mode != VideoModeKeyframes {
		return VideoRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	request.Keyframes = append([]string(nil), request.Keyframes...)
	if len(request.Keyframes) > MaxReferenceImages {
		return VideoRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	if request.Mode == VideoModeKeyframes {
		if request.ImageURL != "" || len(request.Keyframes) < 2 {
			return VideoRequest{}, newError(CodeInvalidInput, 0, false, nil)
		}
		for index, keyframe := range request.Keyframes {
			keyframe = strings.TrimSpace(keyframe)
			if err := validatePublicHTTPSURL(keyframe); err != nil {
				return VideoRequest{}, err
			}
			request.Keyframes[index] = keyframe
		}
	} else if len(request.Keyframes) != 0 {
		return VideoRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	return request, nil
}

func validProviderID(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func isLocalOrPrivateHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "0.0.0.0" || host == "::" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast())
}
