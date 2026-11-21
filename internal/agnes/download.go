package agnes

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
)

type DownloadResult struct {
	Bytes     int64
	SHA256    string
	MediaType string
}

// DownloadVideo downloads only an allowlisted HTTPS MP4 URL. It sends no API
// key to the artifact host and never follows redirects. Callers should pass a
// temporary destination and publish it atomically only after this succeeds.
func (c *Client) DownloadVideo(ctx context.Context, rawURL string, destination io.Writer) (DownloadResult, error) {
	if c == nil || c.httpClient == nil || destination == nil {
		return DownloadResult{}, newError(CodeInvalidInput, 0, false, nil)
	}
	parsed, err := c.validateVideoDownloadURL(rawURL)
	if err != nil {
		return DownloadResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return DownloadResult{}, newError(CodeDownloadRejected, 0, false, err)
	}
	req.Header.Set("Accept", "video/mp4")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return DownloadResult{}, newError(CodeDownloadFailed, 0, true, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		return DownloadResult{}, newError(CodeDownloadRedirectRejected, resp.StatusCode, false, nil)
	}
	if resp.StatusCode != http.StatusOK {
		return DownloadResult{}, newError(CodeDownloadFailed, resp.StatusCode, resp.StatusCode >= 500, nil)
	}
	if c.maxVideoDownloadBytes <= 0 || resp.ContentLength > c.maxVideoDownloadBytes {
		return DownloadResult{}, newError(CodeResponseTooLarge, resp.StatusCode, false, nil)
	}
	mediaType := ""
	if header := strings.TrimSpace(resp.Header.Get("Content-Type")); header != "" {
		mediaType, _, err = mime.ParseMediaType(header)
		if err != nil || (mediaType != "video/mp4" && mediaType != "application/octet-stream") {
			return DownloadResult{}, newError(CodeDownloadRejected, resp.StatusCode, false, err)
		}
	}

	limited := &io.LimitedReader{R: resp.Body, N: c.maxVideoDownloadBytes + 1}
	buffered := bufio.NewReaderSize(limited, 32<<10)
	header, err := buffered.Peek(12)
	if err != nil || len(header) < 12 || string(header[4:8]) != "ftyp" {
		return DownloadResult{}, newError(CodeDownloadRejected, resp.StatusCode, false, err)
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(destination, hash), buffered)
	if err != nil {
		return DownloadResult{}, newError(CodeDownloadFailed, resp.StatusCode, true, err)
	}
	if written <= 0 || written > c.maxVideoDownloadBytes {
		return DownloadResult{}, newError(CodeResponseTooLarge, resp.StatusCode, false, nil)
	}
	return DownloadResult{
		Bytes:     written,
		SHA256:    hex.EncodeToString(hash.Sum(nil)),
		MediaType: "video/mp4",
	}, nil
}

func (c *Client) validateVideoDownloadURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, newError(CodeDownloadRejected, 0, false, err)
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if _, allowed := c.videoDownloadHosts[hostname]; !allowed {
		return nil, newError(CodeDownloadRejected, 0, false, nil)
	}
	if !c.allowNonStandardDownloadPort {
		if port := parsed.Port(); port != "" && port != "443" {
			return nil, newError(CodeDownloadRejected, 0, false, nil)
		}
	}
	if strings.ToLower(path.Ext(parsed.Path)) != ".mp4" {
		return nil, newError(CodeDownloadRejected, 0, false, nil)
	}
	return parsed, nil
}
