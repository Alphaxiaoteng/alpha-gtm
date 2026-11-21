package tikhub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type CallSnapshot struct {
	SchemaVersion int                    `json:"schema_version"`
	CallID        int64                  `json:"call_id"`
	TraceID       string                 `json:"trace_id"`
	Platform      string                 `json:"platform"`
	EndpointID    string                 `json:"endpoint_id"`
	Capability    string                 `json:"capability"`
	Method        string                 `json:"method"`
	Path          string                 `json:"path"`
	Parameters    map[string]any         `json:"parameters"`
	Response      map[string]interface{} `json:"response"`
	FetchedAt     string                 `json:"fetched_at"`
}

// SaveCallSnapshot writes the lossless successful provider response to an
// atomic, owner-only JSON file. No retention job deletes these records.
func SaveCallSnapshot(dataDir string, snapshot CallSnapshot) (string, int64, error) {
	if dataDir == "" || snapshot.CallID <= 0 || snapshot.EndpointID == "" {
		return "", 0, fmt.Errorf("invalid TikHub call snapshot")
	}
	if snapshot.SchemaVersion == 0 {
		snapshot.SchemaVersion = 1
	}
	if snapshot.FetchedAt == "" {
		snapshot.FetchedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", 0, err
	}
	date := time.Now().Format("2006-01-02")
	dir := filepath.Join(dataDir, "tikhub", "calls", date)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", 0, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", 0, err
	}
	target := filepath.Join(dir, snapshotFileName(snapshot, false))
	temp, err := os.CreateTemp(dir, ".call-*.tmp")
	if err != nil {
		return "", 0, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return "", 0, err
	}
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		return "", 0, err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", 0, err
	}
	if err := temp.Close(); err != nil {
		return "", 0, err
	}
	if err := publishSnapshot(tempPath, target); err != nil {
		// Call ids can repeat after a database restore, so a same-named snapshot
		// may already exist. The provider was already paid for: never drop the
		// response just because the file name collided.
		if os.IsExist(err) {
			alt := filepath.Join(dir, snapshotFileName(snapshot, true))
			if altErr := publishSnapshot(tempPath, alt); altErr == nil {
				return alt, int64(len(raw)), nil
			} else {
				err = altErr
			}
		}
		return "", 0, err
	}
	return target, int64(len(raw)), nil
}

func ExtractTranscriptContent(response map[string]interface{}) (content, language string) {
	data, ok := response["data"].(map[string]interface{})
	if !ok {
		return "", ""
	}
	content, _ = data["content"].(string)
	language, _ = data["language_code"].(string)
	return strings.TrimSpace(content), strings.TrimSpace(language)
}

// SaveTranscriptMarkdown keeps a readable sibling of the lossless JSON
// snapshot. It is only called after an explicit caption request.
func SaveTranscriptMarkdown(rawPath, videoID, language, content string) (string, error) {
	content = strings.TrimSpace(content)
	videoID = strings.TrimSpace(videoID)
	language = strings.TrimSpace(language)
	if !strings.HasSuffix(rawPath, ".json") || content == "" || videoID == "" || len(videoID) > 128 || len(language) > 64 {
		return "", fmt.Errorf("invalid transcript markdown input")
	}
	target := strings.TrimSuffix(rawPath, ".json") + ".md"
	body := fmt.Sprintf(
		"# YouTube 视频字幕\n\n- Video ID: `%s`\n- Language: `%s`\n- Source: TikHub YouTube official caption endpoint\n- Raw snapshot: `%s`\n\n## 字幕\n\n%s\n",
		strings.ReplaceAll(videoID, "`", ""),
		strings.ReplaceAll(language, "`", ""),
		filepath.Base(rawPath),
		content,
	)
	dir := filepath.Dir(target)
	temp, err := os.CreateTemp(dir, ".transcript-*.tmp")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return "", err
	}
	if _, err := temp.WriteString(body); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := publishSnapshot(tempPath, target); err != nil {
		return "", err
	}
	return target, nil
}

// publishSnapshot links the fully-synced temporary file into place without
// replacing an existing record. Call history is append-only.
func publishSnapshot(tempPath, target string) error {
	if err := os.Link(tempPath, target); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func safeSnapshotName(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "endpoint"
	}
	return builder.String()
}

// CountPrimaryItems reports the largest familiar result list in the response.
func CountPrimaryItems(response map[string]interface{}) int64 {
	var largest int64
	var walk func(any, int)
	walk = func(value any, depth int) {
		if depth > 8 {
			return
		}
		switch typed := value.(type) {
		case []interface{}:
			if int64(len(typed)) > largest {
				largest = int64(len(typed))
			}
			for _, item := range typed {
				walk(item, depth+1)
			}
		case map[string]interface{}:
			for _, child := range typed {
				walk(child, depth+1)
			}
		}
	}
	walk(response, 0)
	return largest
}

// snapshotFileName builds the lossless snapshot file name. withTrace adds a
// short trace suffix, used only to break a collision with an older file.
func snapshotFileName(snapshot CallSnapshot, withTrace bool) string {
	name := strconv.FormatInt(snapshot.CallID, 10)
	if withTrace {
		trace := safeSnapshotName(snapshot.TraceID)
		if len(trace) > 8 {
			trace = trace[:8]
		}
		if trace != "" {
			name += "_" + trace
		}
	}
	return name + "_" + safeSnapshotName(snapshot.EndpointID) + ".json"
}
