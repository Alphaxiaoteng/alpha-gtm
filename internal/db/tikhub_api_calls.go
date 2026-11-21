package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type TikHubAPICall struct {
	ID              int64  `json:"id"`
	TraceID         string `json:"trace_id"`
	PlatformCode    string `json:"platform_code"`
	EndpointID      string `json:"endpoint_id"`
	Capability      string `json:"capability"`
	Method          string `json:"method"`
	Path            string `json:"path"`
	ParametersJSON  string `json:"parameters_json"`
	Status          string `json:"status"`
	ErrorCode       string `json:"error_code,omitempty"`
	ProviderCode    string `json:"provider_code,omitempty"`
	ProviderMessage string `json:"provider_message,omitempty"`
	ItemCount       int64  `json:"item_count"`
	ResponseBytes   int64  `json:"response_bytes"`
	RawPath         string `json:"raw_path,omitempty"`
	StartedAt       string `json:"started_at"`
	FinishedAt      string `json:"finished_at,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
	CreatedAt       string `json:"created_at"`
}

type TikHubAPICallInput struct {
	TraceID, PlatformCode, EndpointID, Capability, Method, Path, ParametersJSON string
}

type TikHubAPICallFinish struct {
	Status, ErrorCode, ProviderCode, ProviderMessage, RawPath string
	ItemCount, ResponseBytes, DurationMS                      int64
}

type TikHubAPICallFilter struct {
	PlatformCode string
	EndpointID   string
	Status       string
	Limit        int
}

func (db *DB) CreateTikHubAPICall(in TikHubAPICallInput) (*TikHubAPICall, error) {
	if !validOperationEventToken(in.TraceID) || !validOperationEventToken(in.PlatformCode) ||
		!validOperationEventToken(in.EndpointID) || !validOperationEventToken(in.Capability) {
		return nil, fmt.Errorf("invalid TikHub call identifier")
	}
	if in.Method != "GET" && in.Method != "POST" {
		return nil, fmt.Errorf("invalid TikHub call method")
	}
	if !strings.HasPrefix(in.Path, "/api/v1/") || len(in.Path) > 512 {
		return nil, fmt.Errorf("invalid TikHub call path")
	}
	if in.ParametersJSON == "" {
		in.ParametersJSON = "{}"
	}
	if len(in.ParametersJSON) > 64<<10 || !json.Valid([]byte(in.ParametersJSON)) {
		return nil, fmt.Errorf("invalid TikHub call parameters")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`INSERT INTO tikhub_api_calls
		(trace_id, platform_code, endpoint_id, capability, method, path, parameters_json, status, started_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'running', ?, ?)`,
		in.TraceID, in.PlatformCode, in.EndpointID, in.Capability, in.Method, in.Path, in.ParametersJSON, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return db.GetTikHubAPICall(id)
}

func (db *DB) FinishTikHubAPICall(id int64, finish TikHubAPICallFinish) (*TikHubAPICall, error) {
	switch finish.Status {
	case "succeeded", "failed", "storage_failed":
	default:
		return nil, fmt.Errorf("invalid TikHub call status")
	}
	if id <= 0 || finish.ItemCount < 0 || finish.ResponseBytes < 0 || finish.DurationMS < 0 {
		return nil, fmt.Errorf("invalid TikHub call result")
	}
	if len(finish.ProviderMessage) > 500 || len(finish.RawPath) > 4096 {
		return nil, fmt.Errorf("TikHub call result is too long")
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`UPDATE tikhub_api_calls SET
		status=?, error_code=?, provider_code=?, provider_message=?, item_count=?,
		response_bytes=?, raw_path=?, finished_at=?, duration_ms=? WHERE id=? AND status='running'`,
		finish.Status, finish.ErrorCode, finish.ProviderCode, finish.ProviderMessage,
		finish.ItemCount, finish.ResponseBytes, finish.RawPath, finishedAt, finish.DurationMS, id)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, fmt.Errorf("TikHub call not running")
	}
	return db.GetTikHubAPICall(id)
}

func (db *DB) RecoverRunningTikHubAPICalls() (int64, error) {
	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := db.Exec(`UPDATE tikhub_api_calls SET
		status='failed', error_code='local_process_interrupted',
		provider_message='本地进程在调用完成前退出', finished_at=?
		WHERE status='running'`, finishedAt)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (db *DB) GetTikHubAPICall(id int64) (*TikHubAPICall, error) {
	var call TikHubAPICall
	err := db.QueryRow(`SELECT id, trace_id, platform_code, endpoint_id, capability, method, path,
		parameters_json, status, error_code, provider_code, provider_message, item_count,
		response_bytes, raw_path, started_at, finished_at, duration_ms, created_at
		FROM tikhub_api_calls WHERE id=?`, id).Scan(
		&call.ID, &call.TraceID, &call.PlatformCode, &call.EndpointID, &call.Capability,
		&call.Method, &call.Path, &call.ParametersJSON, &call.Status, &call.ErrorCode,
		&call.ProviderCode, &call.ProviderMessage, &call.ItemCount, &call.ResponseBytes,
		&call.RawPath, &call.StartedAt, &call.FinishedAt, &call.DurationMS, &call.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &call, nil
}

func (db *DB) ListTikHubAPICalls(filter TikHubAPICallFilter) ([]TikHubAPICall, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	rows, err := db.Query(`SELECT id, trace_id, platform_code, endpoint_id, capability, method, path,
		parameters_json, status, error_code, provider_code, provider_message, item_count,
		response_bytes, raw_path, started_at, finished_at, duration_ms, created_at
		FROM tikhub_api_calls
		WHERE (?='' OR platform_code=?) AND (?='' OR endpoint_id=?) AND (?='' OR status=?)
		ORDER BY id DESC LIMIT ?`,
		filter.PlatformCode, filter.PlatformCode, filter.EndpointID, filter.EndpointID,
		filter.Status, filter.Status, filter.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	calls := make([]TikHubAPICall, 0)
	for rows.Next() {
		var call TikHubAPICall
		if err := rows.Scan(
			&call.ID, &call.TraceID, &call.PlatformCode, &call.EndpointID, &call.Capability,
			&call.Method, &call.Path, &call.ParametersJSON, &call.Status, &call.ErrorCode,
			&call.ProviderCode, &call.ProviderMessage, &call.ItemCount, &call.ResponseBytes,
			&call.RawPath, &call.StartedAt, &call.FinishedAt, &call.DurationMS, &call.CreatedAt,
		); err != nil {
			return nil, err
		}
		calls = append(calls, call)
	}
	return calls, rows.Err()
}
