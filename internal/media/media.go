package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// FileMeta is local media index entry (AgentPush content center).
type FileMeta struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	SizeBytes  int64  `json:"size_bytes"`
	MimeGuess  string `json:"mime_guess"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`
	IndexedAt  string `json:"indexed_at"`
	ProbeJSON  string `json:"probe_json,omitempty"`
}

const maxLocalMediaBytes int64 = 4 << 30

var (
	ErrImportLimitExceeded = errors.New("local media import limit exceeded")
	ErrNoSupportedMedia    = errors.New("no supported local media files")
)

// ValidateContentAttachment rechecks a durable library snapshot before it is
// associated with a content draft. The local file remains the source of truth.
func ValidateContentAttachment(contentType, purpose, path, expectedSHA256 string) (*FileMeta, error) {
	contentType = strings.TrimSpace(contentType)
	purpose = strings.TrimSpace(purpose)
	path, err := ValidateLocalPath(path)
	if err != nil {
		return nil, err
	}
	meta, err := Probe(path)
	if err != nil {
		return nil, fmt.Errorf("probe media: %w", err)
	}
	compatibleImagePurpose := strings.HasPrefix(meta.MimeGuess, "image/") &&
		(contentType == "graph_text" || contentType == "article") &&
		(purpose == "graph_text" || purpose == "article")
	if purpose != "shared" && purpose != contentType && !compatibleImagePurpose {
		return nil, fmt.Errorf("media library purpose does not match content type")
	}
	if expectedSHA256 != "" && !strings.EqualFold(strings.TrimSpace(expectedSHA256), meta.SHA256) {
		return nil, fmt.Errorf("media library file changed since import")
	}
	return meta, nil
}

const (
	strictVideoProbeTimeout     = 30 * time.Second
	strictVideoProbeOutputBytes = 1 << 20
)

// ValidateLocalPath resolves a user-selected media file without allowing
// credential/runtime directories to be indexed as content.
func ValidateLocalPath(value string) (string, error) {
	path := strings.TrimSpace(value)
	if path == "" {
		return "", fmt.Errorf("media path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve media path: %w", err)
	}
	if protectedLocalPath(abs) {
		return "", fmt.Errorf("media path is in a protected location")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("read media metadata: %w", err)
	}
	if protectedLocalPath(resolved) {
		return "", fmt.Errorf("media path is in a protected location")
	}
	if !supportedLocalMediaExt(filepath.Ext(resolved)) {
		return "", fmt.Errorf("unsupported media file type")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("read media metadata: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxLocalMediaBytes {
		return "", fmt.Errorf("media file must be a non-empty regular file up to 4 GiB")
	}
	return resolved, nil
}

// DiscoverLocalMedia resolves one user-selected file or scans one explicitly
// selected folder. Directory walks never follow symlinks or hidden/protected
// folders and are bounded before any file is indexed.
func DiscoverLocalMedia(value string, recursive bool, limit int) ([]string, error) {
	if limit < 1 {
		return nil, fmt.Errorf("media import limit must be positive")
	}
	path := strings.TrimSpace(value)
	if path == "" {
		return nil, fmt.Errorf("media path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve media path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("read media metadata: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("read media metadata: %w", err)
	}
	if info.Mode().IsRegular() {
		file, err := ValidateLocalPath(resolved)
		if err != nil {
			return nil, err
		}
		return []string{file}, nil
	}
	if !info.IsDir() || broadLocalImportRoot(resolved) || protectedImportDirectory(resolved) {
		return nil, fmt.Errorf("media import folder is not allowed")
	}

	const maxVisitedEntries = 10_000
	paths := make([]string, 0)
	visited := 0
	err = filepath.WalkDir(resolved, func(candidate string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if candidate == resolved {
				return walkErr
			}
			return nil
		}
		if candidate == resolved {
			return nil
		}
		if entry.IsDir() {
			if !recursive || strings.HasPrefix(entry.Name(), ".") || protectedImportDirectory(candidate) {
				return fs.SkipDir
			}
			return nil
		}
		visited++
		if visited > maxVisitedEntries {
			return ErrImportLimitExceeded
		}
		if entry.Type()&fs.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") || !supportedLocalMediaExt(filepath.Ext(entry.Name())) {
			return nil
		}
		validated, validateErr := ValidateLocalPath(candidate)
		if validateErr != nil {
			return nil
		}
		paths = append(paths, validated)
		if len(paths) > limit {
			return ErrImportLimitExceeded
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, ErrNoSupportedMedia
	}
	sort.Strings(paths)
	return paths, nil
}

func broadLocalImportRoot(path string) bool {
	clean := filepath.Clean(path)
	if filepath.Dir(clean) == clean {
		return true
	}
	home, err := os.UserHomeDir()
	return err == nil && clean == filepath.Clean(home)
}

func protectedImportDirectory(path string) bool {
	segments := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for _, segment := range segments {
		switch strings.ToLower(segment) {
		case ".git", ".ssh", ".gnupg", ".aws", ".azure", "1key", "keychains", "profiles", "session-backups":
			return true
		}
	}
	return protectedLocalPath(path)
}

func protectedLocalPath(path string) bool {
	for _, segment := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		lower := strings.ToLower(segment)
		if lower == ".turbopush" || lower == "1key" || lower == ".env" || strings.HasPrefix(lower, ".env.") {
			return true
		}
	}
	return false
}

func supportedLocalMediaExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp4", ".mov", ".webm", ".mkv", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".mp3", ".wav", ".m4a":
		return true
	default:
		return false
	}
}

func HashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), st.Size(), nil
}

func GuessMime(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".mov", ".webm", ".mkv":
		return "video/" + strings.TrimPrefix(ext, ".")
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif", ".webp":
		return "image/" + strings.TrimPrefix(ext, ".")
	case ".mp3", ".wav", ".m4a":
		return "audio/" + strings.TrimPrefix(ext, ".")
	case ".md", ".markdown":
		return "text/markdown"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "application/octet-stream"
	}
}

// Probe runs ffprobe if available; otherwise returns size/hash only.
func Probe(path string) (*FileMeta, error) {
	sum, size, err := HashFile(path)
	if err != nil {
		return nil, err
	}
	meta := &FileMeta{
		Path:      path,
		SHA256:    sum,
		SizeBytes: size,
		MimeGuess: GuessMime(path),
		IndexedAt: time.Now().UTC().Format(time.RFC3339),
	}
	ffprobePath := localFFprobePath()
	if ffprobePath == "" {
		return meta, nil
	}
	cmd := exec.Command(ffprobePath, "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", path)
	out, err := cmd.Output()
	if err != nil {
		return meta, nil
	}
	meta.ProbeJSON = string(out)
	var raw map[string]interface{}
	if json.Unmarshal(out, &raw) == nil {
		if streams, ok := raw["streams"].([]interface{}); ok {
			for _, s := range streams {
				m, _ := s.(map[string]interface{})
				if m == nil {
					continue
				}
				if m["codec_type"] == "video" {
					if w, ok := m["width"].(float64); ok {
						meta.Width = int(w)
					}
					if h, ok := m["height"].(float64); ok {
						meta.Height = int(h)
					}
					if d, ok := m["duration"].(string); ok {
						var sec float64
						fmt.Sscanf(d, "%f", &sec)
						meta.DurationMS = int(sec * 1000)
					}
				}
			}
		}
		if format, ok := raw["format"].(map[string]interface{}); ok && meta.DurationMS == 0 {
			if d, ok := format["duration"].(string); ok {
				var sec float64
				fmt.Sscanf(d, "%f", &sec)
				meta.DurationMS = int(sec * 1000)
			}
		}
	}
	return meta, nil
}

// localFFprobe 允许应用并存系统精简版和用户显式安装的完整探测工具。
func localFFprobePath() string {
	for _, name := range []string{"ffprobe", "ffprobe-full"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if os.Getenv("ALPHA_NEXUS_DISABLE_APP_FFPROBE_FALLBACK") != "1" {
		// Trae bundles ffprobe on macOS. Treat it as an optional local helper only
		// when the user has already installed the app; no download or execution
		// outside the explicit media validation path is performed.
		for _, path := range []string{
			"/Applications/Trae CN.app/Contents/Resources/app/bin/ffprobe",
			"/Applications/Trae.app/Contents/Resources/app/bin/ffprobe",
		} {
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				return path
			}
		}
	}
	return ""
}

type boundedProbeBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (w *boundedProbeBuffer) Write(p []byte) (int, error) {
	originalLength := len(p)
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = w.buffer.Write(p)
	}
	if originalLength > remaining {
		w.exceeded = true
	}
	return originalLength, nil
}

// ProbeDecodableVideo validates one explicit local video and fails closed when
// ffprobe cannot prove that it contains a real, non-empty video stream. Errors
// intentionally omit the local path and ffprobe output.
func ProbeDecodableVideo(path string) (*FileMeta, error) {
	resolved, err := ValidateLocalPath(path)
	if err != nil {
		return nil, fmt.Errorf("invalid local video")
	}
	mime := GuessMime(resolved)
	if !strings.HasPrefix(mime, "video/") {
		return nil, fmt.Errorf("local media is not a video")
	}
	ffprobePath := localFFprobePath()
	if ffprobePath == "" {
		return nil, fmt.Errorf("video probe unavailable")
	}

	ctx, cancel := context.WithTimeout(context.Background(), strictVideoProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		ffprobePath,
		"-v", "error",
		"-print_format", "json",
		"-show_entries", "stream=codec_type,width,height,duration",
		resolved,
	)
	output := &boundedProbeBuffer{limit: strictVideoProbeOutputBytes}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("video probe timed out")
		}
		return nil, fmt.Errorf("video is not decodable")
	}
	if output.exceeded {
		return nil, fmt.Errorf("video probe output is too large")
	}

	var probe struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Duration  string `json:"duration"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output.buffer.Bytes(), &probe); err != nil {
		return nil, fmt.Errorf("video probe returned invalid metadata")
	}
	width, height, durationMS := 0, 0, 0
	for _, stream := range probe.Streams {
		if stream.CodecType != "video" || stream.Width <= 0 || stream.Height <= 0 {
			continue
		}
		durationSeconds, err := strconv.ParseFloat(stream.Duration, 64)
		if err != nil || math.IsNaN(durationSeconds) || math.IsInf(durationSeconds, 0) || durationSeconds <= 0 {
			continue
		}
		width, height = stream.Width, stream.Height
		durationMS = int(durationSeconds * 1000)
		if durationMS <= 0 {
			durationMS = 1
		}
		break
	}
	if width <= 0 || height <= 0 || durationMS <= 0 {
		return nil, fmt.Errorf("video stream metadata is invalid")
	}

	hash, size, err := HashFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("hash local video failed")
	}
	return &FileMeta{
		Path:       resolved,
		SHA256:     hash,
		SizeBytes:  size,
		MimeGuess:  mime,
		Width:      width,
		Height:     height,
		DurationMS: durationMS,
		IndexedAt:  time.Now().UTC().Format(time.RFC3339),
		ProbeJSON:  output.buffer.String(),
	}, nil
}

// PreflightResult validates media against platform-ish rules.
type PreflightResult struct {
	OK       bool     `json:"ok"`
	Warnings []string `json:"warnings"`
	Errors   []string `json:"errors"`
}

func Preflight(path, platform, contentType string) (*PreflightResult, error) {
	meta, err := Probe(path)
	if err != nil {
		return nil, err
	}
	r := &PreflightResult{OK: true}
	if meta.SizeBytes == 0 {
		r.OK = false
		r.Errors = append(r.Errors, "empty file")
	}
	if meta.SizeBytes > 4*1024*1024*1024 {
		r.OK = false
		r.Errors = append(r.Errors, "file larger than 4GB")
	}
	switch platform {
	case "xhs", "instagram":
		if strings.HasPrefix(meta.MimeGuess, "video/") && meta.DurationMS > 15*60*1000 {
			r.Warnings = append(r.Warnings, "video longer than 15m may be rejected")
		}
	case "youtube":
		if !strings.HasPrefix(meta.MimeGuess, "video/") && contentType == "video" {
			r.Warnings = append(r.Warnings, "youtube expects video mime")
		}
	}
	if strings.HasPrefix(meta.MimeGuess, "video/") && meta.Width > 0 && meta.Width < 480 {
		r.Warnings = append(r.Warnings, "low resolution video")
	}
	return r, nil
}

// Transcode 使用本机 ffmpeg 做无损 MP4 封装，不做剪辑或重编码。
func Transcode(src, dst string) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}
	cmd := exec.Command("ffmpeg", "-y", "-i", src, "-c", "copy", dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, string(out))
	}
	return nil
}

// ExtractFirstFrame creates a local JPEG cover from a video. It never changes
// the source file and fails closed when no local ffmpeg is available.
func ExtractFirstFrame(src, dst string) (*FileMeta, error) {
	source, err := ValidateLocalPath(src)
	if err != nil {
		return nil, err
	}
	ffmpegPath := localFFmpegPath()
	if ffmpegPath == "" {
		return nil, fmt.Errorf("ffmpeg not found")
	}
	output, err := filepath.Abs(strings.TrimSpace(dst))
	if err != nil || output == "" {
		return nil, fmt.Errorf("cover output path is invalid")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return nil, fmt.Errorf("create cover directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-ss", "0", "-i", source,
		"-frames:v", "1", "-vf", "scale='min(1080,iw)':-2", "-q:v", "2", "-y", output,
	)
	if commandOutput, runErr := cmd.CombinedOutput(); runErr != nil {
		_ = os.Remove(output)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("extract video cover timed out")
		}
		return nil, fmt.Errorf("extract video cover: %w: %s", runErr, strings.TrimSpace(string(commandOutput)))
	}
	if err := os.Chmod(output, 0o600); err != nil {
		_ = os.Remove(output)
		return nil, fmt.Errorf("restrict video cover: %w", err)
	}
	meta, err := Probe(output)
	if err != nil || meta.SizeBytes <= 0 || !strings.HasPrefix(meta.MimeGuess, "image/") {
		_ = os.Remove(output)
		return nil, fmt.Errorf("generated video cover is invalid")
	}
	return meta, nil
}

// ExtractWhisperAudio creates the 16 kHz mono PCM WAV expected by local
// Whisper. It never modifies or removes the source video.
func ExtractWhisperAudio(src, dst string) (*FileMeta, error) {
	source, err := ValidateLocalPath(src)
	if err != nil {
		return nil, err
	}
	ffmpegPath := localFFmpegPath()
	if ffmpegPath == "" {
		return nil, fmt.Errorf("ffmpeg not found")
	}
	output, err := filepath.Abs(strings.TrimSpace(dst))
	if err != nil || output == "" {
		return nil, fmt.Errorf("audio output path is invalid")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return nil, fmt.Errorf("create audio directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-i", source,
		"-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-y", output,
	)
	if commandOutput, runErr := cmd.CombinedOutput(); runErr != nil {
		_ = os.Remove(output)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("extract video audio timed out")
		}
		return nil, fmt.Errorf("extract video audio: %w: %s", runErr, strings.TrimSpace(string(commandOutput)))
	}
	if err := os.Chmod(output, 0o600); err != nil {
		_ = os.Remove(output)
		return nil, fmt.Errorf("restrict video audio: %w", err)
	}
	meta, err := Probe(output)
	if err != nil || meta.SizeBytes <= 0 || !strings.HasPrefix(meta.MimeGuess, "audio/") {
		_ = os.Remove(output)
		return nil, fmt.Errorf("generated video audio is invalid")
	}
	return meta, nil
}

func localFFmpegPath() string {
	candidates := make([]string, 0, 8)
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		candidates = append(candidates, path)
	}
	candidates = append(candidates,
		"/Applications/Trae CN.app/Contents/Resources/app/bin/ffmpeg",
		"/Applications/Trae.app/Contents/Resources/app/bin/ffmpeg",
		"/opt/homebrew/bin/ffmpeg",
		"/usr/local/bin/ffmpeg",
	)
	home, err := os.UserHomeDir()
	if err == nil {
		matches, _ := filepath.Glob(filepath.Join(home, "Library", "Python", "*", "lib", "python", "site-packages", "static_ffmpeg", "bin", "darwin_*", "ffmpeg"))
		candidates = append(candidates, matches...)
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 && ffmpegSupportsVideoCover(path) {
			return path
		}
	}
	return ""
}

func ffmpegSupportsVideoCover(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	formats, err := exec.CommandContext(ctx, path, "-hide_banner", "-formats").CombinedOutput()
	if err != nil || ctx.Err() != nil || !strings.Contains(string(formats), "mov,mp4,m4a,3gp,3g2,mj2") {
		return false
	}
	encoders, err := exec.CommandContext(ctx, path, "-hide_banner", "-encoders").CombinedOutput()
	return err == nil && ctx.Err() == nil && strings.Contains(string(encoders), " mjpeg ")
}

// WalkIndex indexes media under root (non-recursive option via maxDepth).
func WalkIndex(root string, recursive bool) ([]*FileMeta, error) {
	var out []*FileMeta
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if !recursive && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".mp4", ".mov", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".md", ".mp3", ".wav":
		default:
			return nil
		}
		m, err := Probe(path)
		if err != nil {
			return nil
		}
		out = append(out, m)
		return nil
	})
	return out, err
}
