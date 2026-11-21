package agnes

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	maxPromptBytes               = 32 << 10
	MaxReferenceImageBytes int64 = 16 << 20
	MaxReferenceImages           = 8
	maxImageDimension            = 8192
	maxReferenceURLBytes         = 8 << 10
)

type ImageRequest struct {
	Model  string
	Prompt string
	Size   string
	Ratio  string
	Images []string
}

type ImageResult struct {
	Created       int64
	Data          []byte
	MediaType     string
	Width         int
	Height        int
	RevisedPrompt string
}

// ImageURLResult is an in-memory handoff value for provider APIs that require
// an HTTPS image. Callers must not persist the URL because its lifetime and
// access policy are controlled by the provider.
type ImageURLResult struct {
	Created       int64
	URL           string
	RevisedPrompt string
}

type imageRequestPayload struct {
	Model        string         `json:"model"`
	Prompt       string         `json:"prompt"`
	Size         string         `json:"size"`
	Ratio        string         `json:"ratio,omitempty"`
	ReturnBase64 bool           `json:"return_base64,omitempty"`
	ExtraBody    imageExtraBody `json:"extra_body"`
}

type imageExtraBody struct {
	Images         []string `json:"image,omitempty"`
	ResponseFormat string   `json:"response_format"`
}

type imageResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		URL           *string `json:"url"`
		Base64JSON    *string `json:"b64_json"`
		RevisedPrompt *string `json:"revised_prompt"`
	} `json:"data"`
}

// GenerateImageBase64 forces Base64 output, decodes it, and validates that the
// result is a real image before returning bytes to the caller.
func (c *Client) GenerateImageBase64(ctx context.Context, request ImageRequest) (ImageResult, error) {
	request, err := validateImageRequest(request)
	if err != nil {
		return ImageResult{}, err
	}
	payload := imageRequestPayload{
		Model:        request.Model,
		Prompt:       request.Prompt,
		Size:         request.Size,
		Ratio:        request.Ratio,
		ReturnBase64: true,
		ExtraBody: imageExtraBody{
			Images:         request.Images,
			ResponseFormat: "b64_json",
		},
	}
	var response imageResponse
	if err := c.doJSON(ctx, http.MethodPost, c.imageEndpoint, payload, c.maxImageRequestBytes, c.maxImageResponseBytes, &response); err != nil {
		return ImageResult{}, err
	}
	if len(response.Data) != 1 || response.Data[0].Base64JSON == nil {
		return ImageResult{}, newError(CodeInvalidImageArtifact, 0, false, nil)
	}
	raw := strings.TrimSpace(*response.Data[0].Base64JSON)
	if raw == "" || base64.StdEncoding.DecodedLen(len(raw)) > int(c.maxImageArtifactBytes) {
		return ImageResult{}, newError(CodeInvalidImageArtifact, 0, false, nil)
	}
	artifact, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil || len(artifact) == 0 || int64(len(artifact)) > c.maxImageArtifactBytes {
		return ImageResult{}, newError(CodeInvalidImageArtifact, 0, false, err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(artifact))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > maxImageDimension || config.Height > maxImageDimension {
		return ImageResult{}, newError(CodeInvalidImageArtifact, 0, false, err)
	}
	mediaType := http.DetectContentType(artifact)
	if format != "png" && format != "jpeg" && format != "gif" {
		return ImageResult{}, newError(CodeInvalidImageArtifact, 0, false, nil)
	}
	if mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/gif" {
		return ImageResult{}, newError(CodeInvalidImageArtifact, 0, false, nil)
	}
	revisedPrompt := ""
	if response.Data[0].RevisedPrompt != nil {
		revisedPrompt = *response.Data[0].RevisedPrompt
	}
	return ImageResult{
		Created:       response.Created,
		Data:          artifact,
		MediaType:     mediaType,
		Width:         config.Width,
		Height:        config.Height,
		RevisedPrompt: revisedPrompt,
	}, nil
}

// GenerateImageURL requests exactly one provider-hosted image URL. It is used
// only as an immediate in-memory handoff to another provider endpoint.
func (c *Client) GenerateImageURL(ctx context.Context, request ImageRequest) (ImageURLResult, error) {
	request, err := validateImageRequest(request)
	if err != nil {
		return ImageURLResult{}, err
	}
	payload := imageRequestPayload{
		Model: request.Model, Prompt: request.Prompt, Size: request.Size, Ratio: request.Ratio,
		ReturnBase64: false,
		ExtraBody:    imageExtraBody{Images: request.Images, ResponseFormat: "url"},
	}
	var response imageResponse
	if err := c.doJSON(ctx, http.MethodPost, c.imageEndpoint, payload, c.maxImageRequestBytes, c.maxImageResponseBytes, &response); err != nil {
		return ImageURLResult{}, err
	}
	if len(response.Data) != 1 || response.Data[0].URL == nil {
		return ImageURLResult{}, newError(CodeInvalidResponse, 0, false, nil)
	}
	providerURL := strings.TrimSpace(*response.Data[0].URL)
	if err := validatePublicHTTPSURL(providerURL); err != nil {
		return ImageURLResult{}, newError(CodeInvalidResponse, 0, false, nil)
	}
	revisedPrompt := ""
	if response.Data[0].RevisedPrompt != nil {
		revisedPrompt = *response.Data[0].RevisedPrompt
	}
	return ImageURLResult{Created: response.Created, URL: providerURL, RevisedPrompt: revisedPrompt}, nil
}

func validateImageRequest(request ImageRequest) (ImageRequest, error) {
	request.Model = strings.TrimSpace(request.Model)
	if request.Model != ImageModel20Flash && request.Model != ImageModel21Flash {
		return ImageRequest{}, newError(CodeUnsupportedModel, 0, false, nil)
	}
	request.Prompt = strings.TrimSpace(request.Prompt)
	if request.Prompt == "" || len(request.Prompt) > maxPromptBytes {
		return ImageRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	request.Size = strings.TrimSpace(request.Size)
	if !validImageSize(request.Model, request.Size) {
		return ImageRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	request.Ratio = strings.TrimSpace(request.Ratio)
	if request.Model == ImageModel20Flash && request.Ratio != "" {
		return ImageRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	if request.Ratio != "" && !validImageRatio(request.Ratio) {
		return ImageRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	if len(request.Images) > MaxReferenceImages {
		return ImageRequest{}, newError(CodeInvalidInput, 0, false, nil)
	}
	request.Images = append([]string(nil), request.Images...)
	totalRequestBytes := int64(len(request.Model) + len(request.Prompt) + len(request.Size) + len(request.Ratio) + 1024)
	for index, reference := range request.Images {
		reference = strings.TrimSpace(reference)
		totalRequestBytes += int64(len(reference))
		if totalRequestBytes > MaxImageRequestBytes {
			return ImageRequest{}, newError(CodeInvalidInput, 0, false, nil)
		}
		if err := validateImageReference(reference); err != nil {
			return ImageRequest{}, err
		}
		request.Images[index] = reference
	}
	return request, nil
}

func validImageSize(model, size string) bool {
	switch size {
	case "1K", "2K", "3K", "4K":
		return model == ImageModel21Flash
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return false
	}
	width, errWidth := strconv.Atoi(parts[0])
	height, errHeight := strconv.Atoi(parts[1])
	return errWidth == nil && errHeight == nil && width >= 256 && height >= 256 && width <= maxImageDimension && height <= maxImageDimension && width%16 == 0 && height%16 == 0
}

func validImageRatio(ratio string) bool {
	switch ratio {
	case "1:1", "3:4", "4:3", "16:9", "9:16", "2:3", "3:2", "21:9":
		return true
	default:
		return false
	}
}

func validateImageReference(reference string) error {
	if strings.HasPrefix(reference, "data:") {
		return validateImageDataURI(reference)
	}
	return validatePublicHTTPSURL(reference)
}

func validateImageDataURI(reference string) error {
	comma := strings.IndexByte(reference, ',')
	if comma < 0 {
		return newError(CodeInvalidInput, 0, false, nil)
	}
	media := strings.ToLower(reference[:comma])
	if media != "data:image/png;base64" && media != "data:image/jpeg;base64" && media != "data:image/jpg;base64" && media != "data:image/gif;base64" {
		return newError(CodeInvalidInput, 0, false, nil)
	}
	raw := reference[comma+1:]
	if raw == "" || int64(base64.StdEncoding.DecodedLen(len(raw))) > MaxReferenceImageBytes {
		return newError(CodeInvalidInput, 0, false, nil)
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) == 0 || int64(len(decoded)) > MaxReferenceImageBytes {
		return newError(CodeInvalidInput, 0, false, err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > maxImageDimension || config.Height > maxImageDimension {
		return newError(CodeInvalidInput, 0, false, err)
	}
	return nil
}

func validatePublicHTTPSURL(raw string) error {
	if len(raw) == 0 || len(raw) > maxReferenceURLBytes {
		return newError(CodeInvalidInput, 0, false, nil)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return newError(CodeInvalidInput, 0, false, err)
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return newError(CodeInvalidInput, 0, false, nil)
	}
	if isLocalOrPrivateHost(parsed.Hostname()) {
		return newError(CodeInvalidInput, 0, false, nil)
	}
	return nil
}

// ValidatePublicHTTPSImageURL validates a provider-fetchable image URL without
// performing a network request. Callers should persist the trimmed value they
// validated so retries use the same immutable input.
func ValidatePublicHTTPSImageURL(raw string) error {
	return validatePublicHTTPSURL(strings.TrimSpace(raw))
}
