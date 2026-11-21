package radar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/tikhub"
)

const videoAnalysisSchemaVersion = 1

type VideoEvidence struct {
	Platform          string           `json:"platform"`
	VideoID           string           `json:"video_id"`
	Description       string           `json:"description"`
	Creator           string           `json:"creator,omitempty"`
	DurationSeconds   int64            `json:"duration_seconds,omitempty"`
	Metrics           map[string]int64 `json:"metrics"`
	Hashtags          []string         `json:"hashtags,omitempty"`
	Transcript        string           `json:"transcript,omitempty"`
	TranscriptMissing bool             `json:"transcript_missing"`
}

type VideoAnalysisPrompt struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

type VideoAnalysisResult struct {
	Summary          string                `json:"summary"`
	ViralPotential   string                `json:"viral_potential"`
	Hook             string                `json:"hook"`
	Evidence         []string              `json:"evidence"`
	Structure        []string              `json:"structure"`
	Audience         string                `json:"audience"`
	EmotionalDrivers []string              `json:"emotional_drivers"`
	TopicSuggestions []string              `json:"topic_suggestions"`
	AnalysisPrompts  []VideoAnalysisPrompt `json:"analysis_prompts"`
	Limitations      []string              `json:"limitations"`
}

type VideoAnalysisAttempt struct {
	Route      string `json:"route"`
	Provider   string `json:"provider"`
	Model      string `json:"model,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
	Status     string `json:"status"`
	ErrorCode  string `json:"error_code,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type VideoAnalysis struct {
	SchemaVersion int                    `json:"schema_version"`
	VideoID       string                 `json:"video_id"`
	SourceCallID  int64                  `json:"source_call_id"`
	Status        string                 `json:"status"`
	Route         string                 `json:"route,omitempty"`
	Provider      string                 `json:"provider"`
	Model         string                 `json:"model"`
	Attempts      []VideoAnalysisAttempt `json:"attempts,omitempty"`
	SourceSHA256  string                 `json:"source_sha256"`
	StartedAt     string                 `json:"started_at"`
	FinishedAt    string                 `json:"finished_at,omitempty"`
	DurationMS    int64                  `json:"duration_ms"`
	ErrorCode     string                 `json:"error_code,omitempty"`
	ErrorMessage  string                 `json:"error_message,omitempty"`
	Result        VideoAnalysisResult    `json:"result"`
}

type CompleteVideoAnalysis func(context.Context, string, string) (string, error)

type VideoAnalysisExecutor struct {
	Route          string
	Provider       string
	Model          string
	Complete       CompleteVideoAnalysis
	TaskID         func() string
	ShouldFallback func(error) bool
}

func ExtractVideoEvidence(snapshot tikhub.CallSnapshot) (VideoEvidence, error) {
	return ExtractVideoEvidenceForTarget(snapshot, "")
}

func ExtractVideoEvidenceForTarget(snapshot tikhub.CallSnapshot, targetVideoID string) (VideoEvidence, error) {
	if snapshot.Platform != "douyin" {
		return VideoEvidence{}, fmt.Errorf("unsupported video snapshot")
	}
	targetVideoID = strings.TrimSpace(targetVideoID)
	var aweme map[string]interface{}
	var videoID string

	if snapshot.Capability == "content_detail" {
		data := objectValue(snapshot.Response["data"])
		detail := objectValue(data["aweme_detail"])
		if len(detail) > 0 {
			vID := stringValue(detail["aweme_id"])
			if vID == "" {
				vID = stringValue(snapshot.Parameters["aweme_id"])
			}
			if targetVideoID == "" || targetVideoID == vID {
				aweme = detail
				videoID = vID
			}
		}
	}

	if len(aweme) == 0 {
		aweme, videoID = findDouyinAweme(snapshot.Response, targetVideoID)
	}
	if len(aweme) == 0 {
		return VideoEvidence{}, fmt.Errorf("video detail is missing")
	}
	if videoID == "" {
		videoID = stringValue(aweme["aweme_id"])
	}
	if !validDouyinVideoID(videoID) {
		return VideoEvidence{}, fmt.Errorf("invalid Douyin video ID")
	}
	author := objectValue(aweme["author"])
	stats := objectValue(aweme["statistics"])
	evidence := VideoEvidence{
		Platform: "douyin", VideoID: videoID,
		Description: strings.TrimSpace(stringValue(aweme["desc"])),
		Creator:     strings.TrimSpace(stringValue(author["nickname"])),
		Metrics: map[string]int64{
			"play":     intValue(stats["play_count"]),
			"likes":    intValue(stats["digg_count"]),
			"collects": intValue(stats["collect_count"]),
			"comments": intValue(stats["comment_count"]),
			"shares":   intValue(stats["share_count"]),
		},
		TranscriptMissing: true,
	}
	duration := intValue(aweme["duration"])
	if duration <= 0 {
		duration = intValue(objectValue(aweme["video"])["duration"])
	}
	if duration > 0 {
		evidence.DurationSeconds = duration / 1000
	}
	seenTags := map[string]bool{}
	for _, item := range arrayValue(aweme["text_extra"]) {
		tag := strings.TrimSpace(stringValue(objectValue(item)["hashtag_name"]))
		if tag != "" && !seenTags[tag] {
			seenTags[tag] = true
			evidence.Hashtags = append(evidence.Hashtags, tag)
		}
	}
	for _, item := range arrayValue(aweme["cha_list"]) {
		tag := strings.TrimSpace(stringValue(objectValue(item)["cha_name"]))
		if tag != "" && !seenTags[tag] {
			seenTags[tag] = true
			evidence.Hashtags = append(evidence.Hashtags, tag)
		}
	}
	return evidence, nil
}

func findDouyinAweme(response map[string]interface{}, targetVideoID string) (map[string]interface{}, string) {
	var fallbackMap map[string]interface{}
	var fallbackID string

	checkAweme := func(m map[string]interface{}) bool {
		if m == nil {
			return false
		}
		id := stringValue(m["aweme_id"])
		if validDouyinVideoID(id) {
			if targetVideoID != "" && id == targetVideoID {
				fallbackMap = m
				fallbackID = id
				return true
			}
			if targetVideoID == "" && fallbackMap == nil {
				fallbackMap = m
				fallbackID = id
			}
		}
		if info := objectValue(m["aweme_info"]); len(info) > 0 {
			infoID := stringValue(info["aweme_id"])
			if validDouyinVideoID(infoID) {
				if targetVideoID != "" && infoID == targetVideoID {
					fallbackMap = info
					fallbackID = infoID
					return true
				}
				if targetVideoID == "" && fallbackMap == nil {
					fallbackMap = info
					fallbackID = infoID
				}
			}
		}
		return false
	}

	var walk func(any, int) bool
	walk = func(v any, depth int) bool {
		if depth > 12 {
			return false
		}
		switch typed := v.(type) {
		case map[string]interface{}:
			if checkAweme(typed) && targetVideoID != "" {
				return true
			}
			for _, child := range typed {
				if walk(child, depth+1) {
					return true
				}
			}
		case []interface{}:
			for _, child := range typed {
				if walk(child, depth+1) {
					return true
				}
			}
		}
		return false
	}

	walk(response, 0)
	return fallbackMap, fallbackID
}

func VideoAnalysisPath(dataDir, videoID string) (string, error) {
	if !validDouyinVideoID(videoID) {
		return "", fmt.Errorf("invalid Douyin video ID")
	}
	return filepath.Join(dataDir, "trend-radar", "video-search", "analyses", videoID+".json"), nil
}

func LoadVideoAnalysis(dataDir, videoID string) (VideoAnalysis, error) {
	path, err := VideoAnalysisPath(dataDir, videoID)
	if err != nil {
		return VideoAnalysis{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return VideoAnalysis{}, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 256<<10 {
		return VideoAnalysis{}, fmt.Errorf("invalid video analysis file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return VideoAnalysis{}, err
	}
	var analysis VideoAnalysis
	if err := json.Unmarshal(raw, &analysis); err != nil {
		return VideoAnalysis{}, err
	}
	if analysis.SchemaVersion != videoAnalysisSchemaVersion || analysis.VideoID != videoID {
		return VideoAnalysis{}, fmt.Errorf("invalid video analysis record")
	}
	return analysis, nil
}

func AnalyzeVideo(
	ctx context.Context,
	dataDir string,
	snapshot tikhub.CallSnapshot,
	provider string,
	model string,
	complete CompleteVideoAnalysis,
	force bool,
) (VideoAnalysis, bool, error) {
	return AnalyzeVideoWithFallback(ctx, dataDir, snapshot, VideoAnalysisExecutor{
		Route: "model", Provider: provider, Model: model, Complete: complete,
	}, nil, force)
}

func AnalyzeVideoWithFallback(
	ctx context.Context,
	dataDir string,
	snapshot tikhub.CallSnapshot,
	primary VideoAnalysisExecutor,
	fallback *VideoAnalysisExecutor,
	force bool,
) (VideoAnalysis, bool, error) {
	return AnalyzeVideoWithFallbackForTarget(ctx, dataDir, snapshot, "", primary, fallback, force)
}

func AnalyzeVideoWithFallbackForTarget(
	ctx context.Context,
	dataDir string,
	snapshot tikhub.CallSnapshot,
	targetVideoID string,
	primary VideoAnalysisExecutor,
	fallback *VideoAnalysisExecutor,
	force bool,
) (VideoAnalysis, bool, error) {
	evidence, err := ExtractVideoEvidenceForTarget(snapshot, targetVideoID)
	if err != nil {
		return VideoAnalysis{}, false, err
	}
	if existing, loadErr := LoadVideoAnalysis(dataDir, evidence.VideoID); loadErr == nil && existing.Status == "succeeded" && !force {
		return existing, true, nil
	}
	if primary.Complete == nil {
		return VideoAnalysis{}, false, fmt.Errorf("configured model is unavailable")
	}
	source, err := json.Marshal(evidence)
	if err != nil {
		return VideoAnalysis{}, false, err
	}
	sum := sha256.Sum256(source)
	started := time.Now()
	analysis := VideoAnalysis{
		SchemaVersion: videoAnalysisSchemaVersion,
		VideoID:       evidence.VideoID, SourceCallID: snapshot.CallID, Status: "running",
		Route:    strings.TrimSpace(primary.Route),
		Provider: strings.TrimSpace(primary.Provider), Model: strings.TrimSpace(primary.Model),
		SourceSHA256: hex.EncodeToString(sum[:]),
		StartedAt:    started.UTC().Format(time.RFC3339Nano),
	}
	if err := saveVideoAnalysis(dataDir, analysis); err != nil {
		return VideoAnalysis{}, false, err
	}
	executors := []VideoAnalysisExecutor{primary}
	if fallback != nil && fallback.Complete != nil {
		executors = append(executors, *fallback)
	}
	var lastErr error
	for index, executor := range executors {
		attemptStarted := time.Now()
		output, completeErr := executor.Complete(ctx, videoAnalysisSystemPrompt(), string(source))
		mayFallback := true
		attempt := VideoAnalysisAttempt{
			Route: strings.TrimSpace(executor.Route), Provider: strings.TrimSpace(executor.Provider),
			Model: strings.TrimSpace(executor.Model), Status: "failed",
			DurationMS: time.Since(attemptStarted).Milliseconds(),
		}
		if executor.TaskID != nil {
			attempt.TaskID = strings.TrimSpace(executor.TaskID())
		}
		if completeErr != nil {
			attempt.ErrorCode = "MODEL_REQUEST_FAILED"
			lastErr = completeErr
			if executor.ShouldFallback != nil {
				mayFallback = executor.ShouldFallback(completeErr)
			}
		} else {
			result, parseErr := parseVideoAnalysisResult(output)
			if parseErr != nil {
				attempt.ErrorCode = "MODEL_OUTPUT_INVALID"
				lastErr = parseErr
			} else {
				attempt.Status = "succeeded"
				analysis.Attempts = append(analysis.Attempts, attempt)
				analysis.Status = "succeeded"
				analysis.Route = attempt.Route
				analysis.Provider = attempt.Provider
				analysis.Model = attempt.Model
				analysis.Result = result
				analysis.DurationMS = time.Since(started).Milliseconds()
				analysis.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
				if err := saveVideoAnalysis(dataDir, analysis); err != nil {
					return VideoAnalysis{}, false, err
				}
				return analysis, false, nil
			}
		}
		analysis.Attempts = append(analysis.Attempts, attempt)
		analysis.DurationMS = time.Since(started).Milliseconds()
		if err := saveVideoAnalysis(dataDir, analysis); err != nil {
			return VideoAnalysis{}, false, err
		}
		if ctx.Err() != nil || !mayFallback || index == len(executors)-1 {
			break
		}
	}
	analysis.Status = "failed"
	analysis.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if len(analysis.Attempts) > 0 {
		last := analysis.Attempts[len(analysis.Attempts)-1]
		analysis.Route = last.Route
		analysis.Provider = last.Provider
		analysis.Model = last.Model
		analysis.ErrorCode = last.ErrorCode
	}
	if analysis.ErrorCode == "MODEL_OUTPUT_INVALID" {
		analysis.ErrorMessage = "模型返回内容不符合视频拆解结构"
	} else {
		analysis.ErrorMessage = "已配置模型调用失败"
	}
	if err := saveVideoAnalysis(dataDir, analysis); err != nil {
		return VideoAnalysis{}, false, err
	}
	return analysis, false, lastErr
}

func videoAnalysisSystemPrompt() string {
	return `Role
你是中文短视频内容策略分析师。你只根据提供的视频证据工作。

Goal
产出可用于复盘、选题和脚本改写的短视频拆解。

Success criteria
传播判断必须引用输入中的文案、标签或公开指标。区分已观察事实与推断。给出可执行的选题建议和后续分析提示词。证据不足时明确写“未验证”。

Constraints
不得编造口播、画面、受众反馈或平台数据。不得引用输入中未提供的行业基准，不得声称高于或低于平台常见水平。transcript_missing=true 时，不得声称理解了完整视频内容；必须在 limitations 中说明只能分析标题、标签和公开指标。不要把高互动直接等同于因果。

Output
只返回一个 JSON 对象，不要 Markdown 或代码围栏。字段必须完整：
{"summary":"string","viral_potential":"string","hook":"string","evidence":["string"],"structure":["string"],"audience":"string","emotional_drivers":["string"],"topic_suggestions":["string"],"analysis_prompts":[{"title":"string","text":"string"}],"limitations":["string"]}
evidence、structure、emotional_drivers、topic_suggestions、limitations 必须始终是 JSON 字符串数组，即使无法验证也要写成 ["未验证"]，禁止把这些字段写成单个字符串。analysis_prompts 必须始终是对象数组。
所有说明都必须写在 JSON 字符串中；数组或对象结束后不得追加解释文字。返回前检查整个答案可以被标准 JSON.parse 直接解析，JSON 语法位置只使用英文半角标点。

Stop rules
完成全部字段后停止。无法验证的字段填写“未验证”，不要补写不存在的内容。`
}

func parseVideoAnalysisResult(output string) (VideoAnalysisResult, error) {
	output = strings.TrimSpace(output)
	if strings.HasPrefix(output, "```") {
		if newline := strings.IndexByte(output, '\n'); newline >= 0 {
			output = output[newline+1:]
		}
		output = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(output), "```"))
	}
	if len(output) == 0 || len(output) > 128<<10 {
		return VideoAnalysisResult{}, fmt.Errorf("invalid model output size")
	}
	var result VideoAnalysisResult
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return VideoAnalysisResult{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return VideoAnalysisResult{}, fmt.Errorf("model output contains trailing JSON")
	}
	result.Summary = trimRunes(result.Summary, 1200)
	result.ViralPotential = trimRunes(result.ViralPotential, 500)
	result.Hook = trimRunes(result.Hook, 800)
	result.Audience = trimRunes(result.Audience, 500)
	result.Evidence = trimStringList(result.Evidence, 12, 600)
	result.Structure = trimStringList(result.Structure, 10, 600)
	result.EmotionalDrivers = trimStringList(result.EmotionalDrivers, 10, 400)
	result.TopicSuggestions = trimStringList(result.TopicSuggestions, 12, 600)
	result.Limitations = trimStringList(result.Limitations, 10, 600)
	if len(result.AnalysisPrompts) > 6 {
		result.AnalysisPrompts = result.AnalysisPrompts[:6]
	}
	for index := range result.AnalysisPrompts {
		result.AnalysisPrompts[index].Title = trimRunes(result.AnalysisPrompts[index].Title, 80)
		result.AnalysisPrompts[index].Text = trimRunes(result.AnalysisPrompts[index].Text, 2000)
	}
	if result.Summary == "" || len(result.Evidence) == 0 || len(result.TopicSuggestions) == 0 {
		return VideoAnalysisResult{}, fmt.Errorf("model output is incomplete")
	}
	return result, nil
}

func saveVideoAnalysis(dataDir string, analysis VideoAnalysis) error {
	path, err := VideoAnalysisPath(dataDir, analysis.VideoID)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return err
	}
	return replacePrivateFile(path, raw)
}

func validDouyinVideoID(value string) bool {
	if len(value) < 10 || len(value) > 32 {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func objectValue(value any) map[string]interface{} {
	result, _ := value.(map[string]interface{})
	return result
}

func arrayValue(value any) []interface{} {
	result, _ := value.([]interface{})
	return result
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func intValue(value any) int64 {
	switch typed := value.(type) {
	case json.Number:
		result, _ := typed.Int64()
		return result
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		result, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
		return result
	}
}

func trimRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func trimStringList(values []string, limit int, runeLimit int) []string {
	if len(values) > limit {
		values = values[:limit]
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := trimRunes(value, runeLimit); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
