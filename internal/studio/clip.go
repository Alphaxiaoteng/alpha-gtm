package studio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/media"
)

// OutputPath 返回 Alpha 数据目录内的剪辑输出路径，不改写用户原始素材。
func OutputPath(dataDir string, clipJobID int64) (string, error) {
	if clipJobID <= 0 {
		return "", fmt.Errorf("clip job id must be positive")
	}
	root := filepath.Join(dataDir, "edits")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create edits directory: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return "", fmt.Errorf("restrict edits directory: %w", err)
	}
	return filepath.Join(root, fmt.Sprintf("clip-%d.mp4", clipJobID)), nil
}

// ExecuteClip 用本机 ffmpeg 生成一个可审阅粗剪。它不上传、删除或覆盖原始素材。
func ExecuteClip(ctx context.Context, sourcePath, outputPath string, startMS, endMS int) error {
	if startMS < 0 || endMS <= startMS || endMS-startMS > maxClipMS {
		return fmt.Errorf("invalid clip range")
	}
	sourcePath, err := media.ValidateLocalPath(sourcePath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(outputPath) == "" || strings.ToLower(filepath.Ext(outputPath)) != ".mp4" {
		return fmt.Errorf("clip output must be an mp4 path")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return fmt.Errorf("create clip output directory: %w", err)
	}
	ffmpegPath, err := findUsableFFMPEG(ctx)
	if err != nil {
		return err
	}
	args := clipArgs(sourcePath, outputPath, startMS, endMS)
	command := exec.CommandContext(ctx, ffmpegPath, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("FFMPEG_CLIP_FAILED: %w: %s", err, truncateClipError(string(output)))
	}
	return nil
}

// findUsableFFMPEG 保留系统 ffmpeg 优先级；若它是精简版，则使用显式安装的完整二进制。
func findUsableFFMPEG(ctx context.Context) (string, error) {
	var capabilityErr error
	for _, name := range []string{"ffmpeg", "ffmpeg-full"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := requireMP4Muxer(ctx, path); err == nil {
			return path, nil
		} else {
			capabilityErr = err
		}
	}
	if capabilityErr != nil {
		return "", capabilityErr
	}
	return "", fmt.Errorf("FFMPEG_NOT_FOUND: ffmpeg or ffmpeg-full is not on PATH")
}

// requireMP4Muxer 先给出稳定错误，避免精简版 ffmpeg 只在执行后返回难以判断的失败文本。
func requireMP4Muxer(ctx context.Context, ffmpegPath string) error {
	output, err := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-formats").Output()
	if err != nil {
		return fmt.Errorf("FFMPEG_CAPABILITY_UNAVAILABLE: inspect formats: %w", err)
	}
	if !hasMP4Muxer(string(output)) {
		return fmt.Errorf("FFMPEG_MP4_MUXER_UNAVAILABLE: install a full ffmpeg build with MP4 muxing")
	}
	return nil
}

func hasMP4Muxer(formats string) bool {
	for _, line := range strings.Split(formats, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(fields[0], "E") {
			continue
		}
		for _, name := range strings.Split(fields[1], ",") {
			if name == "mp4" {
				return true
			}
		}
	}
	return false
}

func clipArgs(sourcePath, outputPath string, startMS, endMS int) []string {
	startSeconds := strconv.FormatFloat(float64(startMS)/1000, 'f', 3, 64)
	durationSeconds := strconv.FormatFloat(float64(endMS-startMS)/1000, 'f', 3, 64)
	return []string{
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-ss", startSeconds, "-i", sourcePath, "-t", durationSeconds,
		"-c", "copy", "-movflags", "+faststart", outputPath,
	}
}

func truncateClipError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 400 {
		return value
	}
	return value[:400] + "…"
}
