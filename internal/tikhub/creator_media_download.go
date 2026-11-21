package tikhub

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxCoverDownloadBytes = 12 << 20
	maxAudioDownloadBytes = 64 << 20
)

// newMediaClient builds the shared HTTP client for creator media. Redirects are
// re-validated because CDNs routinely bounce to a sibling host.
func newMediaClient() *http.Client {
	return &http.Client{
		Timeout: 45 * time.Second,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if !allowedMediaURL(request.URL) {
				return fmt.Errorf("media redirect host is not allowed")
			}
			return nil
		},
	}
}

func allowedMediaType(kind, contentType string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if kind == "cover" {
		return allowedImageType(base)
	}
	switch base {
	case "audio/mpeg", "audio/mp3", "audio/mp4", "audio/aac", "audio/x-m4a", "audio/wav", "audio/x-wav",
		"application/octet-stream", "binary/octet-stream":
		return true
	default:
		return false
	}
}

func maxBytesForKind(kind string) int64 {
	if kind == "audio" {
		return maxAudioDownloadBytes
	}
	return maxCoverDownloadBytes
}

// downloadMediaFile streams one asset to a 0600 file and renames it into place,
// so a partial download never looks like a finished archive entry.
func downloadMediaFile(ctx context.Context, client *http.Client, allow func(*url.URL) bool, sourceURL, dir, target, kind string) error {
	parsed, err := url.Parse(sourceURL)
	if err != nil || !allow(parsed) {
		return fmt.Errorf("media URL is not allowed")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	if kind == "cover" {
		request.Header.Set("Accept", "image/avif,image/webp,image/png,image/jpeg")
	} else {
		request.Header.Set("Accept", "audio/mpeg,audio/mp4,audio/*;q=0.9,*/*;q=0.8")
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("media returned HTTP %d", response.StatusCode)
	}

	limit := maxBytesForKind(kind)
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return err
	}
	if len(raw) == 0 || int64(len(raw)) > limit {
		return fmt.Errorf("media size is invalid")
	}
	// Trust the sniffed type over the header: some CDNs serve audio as
	// octet-stream, while an HTML error page must never be archived as media.
	contentType := http.DetectContentType(raw)
	if !allowedMediaType(kind, contentType) {
		return fmt.Errorf("media content type %q is not allowed", contentType)
	}

	temp, err := os.CreateTemp(dir, ".media-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, target); err != nil {
		return err
	}
	// Keep the folder cheap to sync even on large archives.
	_ = os.Chmod(filepath.Dir(target), 0o700)
	return nil
}
