package radar

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/tikhub"
)

type Schedule struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	NicheID         string         `json:"niche_id"`
	EndpointID      string         `json:"endpoint_id"`
	Params          map[string]any `json:"params"`
	IntervalMinutes int            `json:"interval_minutes"`
	Enabled         bool           `json:"enabled"`
	LastRunAt       string         `json:"last_run_at,omitempty"`
	NextRunAt       string         `json:"next_run_at,omitempty"`
	LastStatus      string         `json:"last_status,omitempty"`
	LastCallID      int64          `json:"last_call_id,omitempty"`
	LastError       string         `json:"last_error,omitempty"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
}

type ScheduleFile struct {
	Version   int        `json:"version"`
	Schedules []Schedule `json:"schedules"`
	UpdatedAt string     `json:"updated_at"`
}

func schedulePath(dataDir string) string {
	return filepath.Join(dataDir, "trend-radar", "schedules.json")
}

func LoadSchedules(dataDir string) (ScheduleFile, error) {
	result := ScheduleFile{Version: 1, Schedules: []Schedule{}}
	raw, err := os.ReadFile(schedulePath(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ScheduleFile{}, err
	}
	if result.Version != 1 {
		return ScheduleFile{}, fmt.Errorf("unsupported radar schedule version")
	}
	if result.Schedules == nil {
		result.Schedules = []Schedule{}
	}
	return result, nil
}

// allowedScheduleEndpoints lists the keyword-search endpoints the background
// worker may call unattended. Every entry must exist in the TikHub catalog with
// a keyword_search capability; TestAllowedScheduleEndpointsExistInCatalog guards
// that, because an unknown ID here would only surface as a runtime
// "endpoint unavailable" failure inside the LaunchAgent.
var allowedScheduleEndpoints = map[string]bool{
	"douyin.video_search":      true,
	"xhs.note_search":          true,
	"youtube.video_search":     true,
	"kuaishou.search":          true,
	"wechat_mp.keyword_search": true,
	"x.keyword_search":         true,
}

func ValidateSchedule(schedule Schedule) (Schedule, error) {
	schedule.ID = strings.TrimSpace(schedule.ID)
	schedule.Name = strings.TrimSpace(schedule.Name)
	schedule.NicheID = strings.TrimSpace(schedule.NicheID)
	schedule.EndpointID = strings.TrimSpace(schedule.EndpointID)
	if !allowedScheduleEndpoints[schedule.EndpointID] {
		return Schedule{}, fmt.Errorf("endpoint %q is not enabled for automated schedules (supports douyin, xhs, youtube, kuaishou, wechat_mp, x keyword_search)", schedule.EndpointID)
	}
	endpoint, ok := tikhub.FindEndpoint(schedule.EndpointID)
	if !ok {
		return Schedule{}, fmt.Errorf("unknown endpoint")
	}
	input := make(map[string]any, len(schedule.Params))
	for name, value := range schedule.Params {
		if _, fixed := endpoint.FixedParams[name]; !fixed {
			input[name] = value
		}
	}
	prepared, err := tikhub.PrepareEndpointParams(endpoint, input)
	if err != nil {
		return Schedule{}, err
	}
	if schedule.IntervalMinutes != 60 && schedule.IntervalMinutes != 360 && schedule.IntervalMinutes != 1440 {
		return Schedule{}, fmt.Errorf("interval must be 60, 360, or 1440 minutes")
	}
	if schedule.Name == "" {
		schedule.Name = fmt.Sprint(prepared["keyword"])
	}
	if schedule.Name == "" || len([]rune(schedule.Name)) > 80 || len([]rune(schedule.NicheID)) > 80 {
		return Schedule{}, fmt.Errorf("invalid schedule name")
	}
	schedule.Params = prepared
	return schedule, nil
}

func SaveSchedules(dataDir string, schedules ScheduleFile) error {
	schedules.Version = 1
	schedules.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.MarshalIndent(schedules, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(schedulePath(dataDir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".schedules-*.tmp")
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
	return os.Rename(tempPath, schedulePath(dataDir))
}

func UpdateSchedules(dataDir string, update func(*ScheduleFile) error) error {
	lockDir := filepath.Join(dataDir, "trend-radar", "schedules.lock")
	if err := os.MkdirAll(filepath.Dir(lockDir), 0o700); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := os.Mkdir(lockDir, 0o700)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return err
		}
		info, statErr := os.Stat(lockDir)
		if statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			if removeErr := os.Remove(lockDir); removeErr == nil {
				continue
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("trend radar schedules are busy")
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer os.Remove(lockDir)

	file, err := LoadSchedules(dataDir)
	if err != nil {
		return err
	}
	if err := update(&file); err != nil {
		return err
	}
	return SaveSchedules(dataDir, file)
}

// MergeScheduleRuntime updates only worker-owned fields so UI edits made while a
// paid request is running are not overwritten by the worker's older snapshot.
func MergeScheduleRuntime(dataDir string, completed []Schedule) error {
	return UpdateSchedules(dataDir, func(file *ScheduleFile) error {
		byID := make(map[string]Schedule, len(completed))
		for _, schedule := range completed {
			byID[schedule.ID] = schedule
		}
		for index := range file.Schedules {
			runtimeState, ok := byID[file.Schedules[index].ID]
			if !ok {
				continue
			}
			file.Schedules[index].LastRunAt = runtimeState.LastRunAt
			file.Schedules[index].NextRunAt = runtimeState.NextRunAt
			file.Schedules[index].LastStatus = runtimeState.LastStatus
			file.Schedules[index].LastCallID = runtimeState.LastCallID
			file.Schedules[index].LastError = runtimeState.LastError
			file.Schedules[index].UpdatedAt = runtimeState.UpdatedAt
		}
		return nil
	})
}

func NewScheduleID() string {
	return fmt.Sprintf("radar-%d", time.Now().UTC().UnixNano())
}
