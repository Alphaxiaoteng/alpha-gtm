package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// ArkResponsesEndpoint is the fixed Volcano Ark Responses API endpoint.
	// Model availability is verified separately; this constant is not evidence
	// that any particular model is enabled for the configured account.
	ArkResponsesEndpoint = DoubaoBaseURL + "/responses"

	arkResponsesMaxBody = 4 << 20
)

type ArkResponsesErrorCode string

const (
	ArkResponsesInvalidRequest  ArkResponsesErrorCode = "ARK_RESPONSES_INVALID_REQUEST"
	ArkResponsesNotConfigured   ArkResponsesErrorCode = "ARK_RESPONSES_NOT_CONFIGURED"
	ArkResponsesTransportFailed ArkResponsesErrorCode = "ARK_RESPONSES_TRANSPORT_FAILED"
	ArkResponsesHTTPFailed      ArkResponsesErrorCode = "ARK_RESPONSES_HTTP_FAILED"
	ArkResponsesInvalidResponse ArkResponsesErrorCode = "ARK_RESPONSES_INVALID_RESPONSE"
	ArkResponsesEmptyOutput     ArkResponsesErrorCode = "ARK_RESPONSES_EMPTY_OUTPUT"
)

// ArkResponsesError intentionally excludes response bodies, prompts and
// credentials. RequestID is provider-generated diagnostic metadata.
type ArkResponsesError struct {
	Code       ArkResponsesErrorCode
	StatusCode int
	RequestID  string
	cause      error
}

func (e *ArkResponsesError) Error() string {
	if e == nil {
		return "Ark Responses API error"
	}
	switch e.Code {
	case ArkResponsesInvalidRequest:
		return "Ark Responses API request is invalid"
	case ArkResponsesNotConfigured:
		return "Ark Responses API credential is not configured"
	case ArkResponsesTransportFailed:
		return "Ark Responses API request could not be completed"
	case ArkResponsesHTTPFailed:
		if e.StatusCode > 0 {
			return fmt.Sprintf("Ark Responses API returned HTTP %d", e.StatusCode)
		}
		return "Ark Responses API returned an HTTP error"
	case ArkResponsesInvalidResponse:
		return "Ark Responses API returned an invalid response"
	case ArkResponsesEmptyOutput:
		return "Ark Responses API returned no text"
	default:
		return "Ark Responses API error"
	}
}

func (e *ArkResponsesError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// ArkResponsesClient is the minimal non-streaming Volcano Ark Responses API
// client. Its credential remains private and is never included in errors.
type ArkResponsesClient struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
}

// NewArkResponsesClient creates a fixed-endpoint client using the existing
// macOS Keychain credential. It does not inspect env, config.json or model
// catalogs, and a constructor success does not mean the model has been tested.
func NewArkResponsesClient(model string, timeout time.Duration) (*ArkResponsesClient, error) {
	apiKey := strings.TrimSpace(loadDoubaoKeychainKey())
	if apiKey == "" {
		return nil, &ArkResponsesError{Code: ArkResponsesNotConfigured}
	}
	return newArkResponsesClient(
		ArkResponsesEndpoint,
		model,
		apiKey,
		NewDoubaoHTTPClient(timeout),
	)
}

func newArkResponsesClient(
	endpoint string,
	model string,
	apiKey string,
	client *http.Client,
) (*ArkResponsesClient, error) {
	endpoint = strings.TrimSpace(endpoint)
	model = strings.TrimSpace(model)
	apiKey = strings.TrimSpace(apiKey)
	if endpoint == "" || model == "" {
		return nil, &ArkResponsesError{Code: ArkResponsesInvalidRequest}
	}
	if apiKey == "" {
		return nil, &ArkResponsesError{Code: ArkResponsesNotConfigured}
	}
	if client == nil {
		return nil, &ArkResponsesError{Code: ArkResponsesInvalidRequest}
	}
	return &ArkResponsesClient{
		endpoint: endpoint,
		model:    model,
		apiKey:   apiKey,
		client:   client,
	}, nil
}

// ValidateArkResponsesModel performs one minimal paid request with the supplied
// in-memory key. Callers may persist the key/model only after this returns nil.
func ValidateArkResponsesModel(
	ctx context.Context,
	apiKey string,
	model string,
	timeout time.Duration,
) error {
	return validateArkResponsesModel(
		ctx,
		ArkResponsesEndpoint,
		apiKey,
		model,
		NewDoubaoHTTPClient(timeout),
	)
}

// ValidateOpenAIResponsesModel validates one user-selected OpenAI-compatible
// Responses endpoint. The credential remains in memory and is never persisted
// by this function.
func ValidateOpenAIResponsesModel(
	ctx context.Context,
	baseURL string,
	apiKey string,
	model string,
	timeout time.Duration,
) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/responses") {
		baseURL = strings.TrimSuffix(baseURL, "/responses")
	}
	return validateArkResponsesModel(
		ctx,
		baseURL+"/responses",
		apiKey,
		model,
		NewDoubaoHTTPClient(timeout),
	)
}

func validateArkResponsesModel(
	ctx context.Context,
	endpoint string,
	apiKey string,
	model string,
	client *http.Client,
) error {
	responsesClient, err := newArkResponsesClient(endpoint, model, apiKey, client)
	if err != nil {
		return err
	}
	_, err = responsesClient.Complete(ctx, "Reply with exactly: OK")
	return err
}

// Complete sends exactly model+input and returns concatenated
// output[].content[].text. No fallback or automatic retry is performed.
func (c *ArkResponsesClient) Complete(ctx context.Context, input string) (string, error) {
	if c == nil || c.client == nil || strings.TrimSpace(c.endpoint) == "" ||
		strings.TrimSpace(c.model) == "" {
		return "", &ArkResponsesError{Code: ArkResponsesInvalidRequest}
	}
	if strings.TrimSpace(input) == "" {
		return "", &ArkResponsesError{Code: ArkResponsesInvalidRequest}
	}

	payload, err := json.Marshal(struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}{
		Model: c.model,
		Input: input,
	})
	if err != nil {
		return "", &ArkResponsesError{Code: ArkResponsesInvalidRequest, cause: err}
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", &ArkResponsesError{Code: ArkResponsesInvalidRequest, cause: err}
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.client.Do(request)
	if err != nil {
		return "", &ArkResponsesError{Code: ArkResponsesTransportFailed, cause: err}
	}
	defer response.Body.Close()

	requestID := arkResponsesRequestID(response.Header)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, arkResponsesMaxBody))
		return "", &ArkResponsesError{
			Code:       ArkResponsesHTTPFailed,
			StatusCode: response.StatusCode,
			RequestID:  requestID,
		}
	}

	var decoded struct {
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, arkResponsesMaxBody))
	if err := decoder.Decode(&decoded); err != nil {
		return "", &ArkResponsesError{
			Code:      ArkResponsesInvalidResponse,
			RequestID: requestID,
			cause:     err,
		}
	}

	texts := make([]string, 0)
	for _, output := range decoded.Output {
		for _, content := range output.Content {
			if strings.TrimSpace(content.Text) != "" {
				texts = append(texts, content.Text)
			}
		}
	}
	result := strings.TrimSpace(strings.Join(texts, "\n"))
	if result == "" {
		return "", &ArkResponsesError{
			Code:      ArkResponsesEmptyOutput,
			RequestID: requestID,
		}
	}
	return result, nil
}

func arkResponsesRequestID(header http.Header) string {
	for _, name := range []string{"X-Tt-Logid", "X-Request-Id", "Request-Id"} {
		if value := strings.TrimSpace(header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}
