package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type StreamChunkHandler func(token string) error

type StreamChoiceDelta struct {
	Content string `json:"content"`
}

type StreamChoice struct {
	Delta StreamChoiceDelta `json:"delta"`
}

type StreamResponse struct {
	Choices []StreamChoice `json:"choices"`
}

type StreamOptions struct {
	EnableReasoning bool
}

// CompleteStream sends a streaming chat completions request and invokes callback per token.
func CompleteStream(ctx context.Context, cfg Config, messages []map[string]string, onToken StreamChunkHandler) (string, error) {
	return CompleteStreamWithOptions(ctx, cfg, messages, StreamOptions{EnableReasoning: true}, onToken)
}

// CompleteStreamWithOptions sends a streaming request with explicit reasoning and temperature controls.
func CompleteStreamWithOptions(ctx context.Context, cfg Config, messages []map[string]string, opts StreamOptions, onToken StreamChunkHandler) (string, error) {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if !strings.HasSuffix(baseURL, "/chat/completions") {
		baseURL += "/chat/completions"
	}

	payload := map[string]any{
		"model":       cfg.Model,
		"messages":    messages,
		"stream":      true,
		"temperature": 0.3,
	}

	if !opts.EnableReasoning {
		payload["reasoning_effort"] = "low"
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	client := NewDoubaoHTTPClient(120 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(body))
	}

	reader := bufio.NewReader(resp.Body)
	var fullContent strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fullContent.String(), err
		}

		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if line == "data: [DONE]" {
			break
		}

		if strings.HasPrefix(line, "data: ") {
			jsonStr := strings.TrimPrefix(line, "data: ")
			var streamResp StreamResponse
			if err := json.Unmarshal([]byte(jsonStr), &streamResp); err == nil {
				if len(streamResp.Choices) > 0 {
					delta := streamResp.Choices[0].Delta.Content
					if delta != "" {
						fullContent.WriteString(delta)
						if onToken != nil {
							if err := onToken(delta); err != nil {
								return fullContent.String(), err
							}
						}
					}
				}
			}
		}
	}

	return fullContent.String(), nil
}
