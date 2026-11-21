package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/tikhub"
)

func (s *Server) handleAPITrendRadarVideoSearchParse(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Input string `json:"input"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_video_input", "视频输入格式不正确")
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_video_input", "视频输入包含多余 JSON")
		return
	}
	parsed, err := tikhub.ParseDouyinVideoInput(input.Input)
	if err != nil {
		writeDataAPIError(w, http.StatusBadRequest, "invalid_video_input", err.Error())
		return
	}
	response := struct {
		tikhub.ParsedDouyinVideoInput
		ExistingCallID         int64  `json:"existing_call_id,omitempty"`
		ExistingVideoID        string `json:"existing_video_id,omitempty"`
		ExistingAnalysisStatus string `json:"existing_analysis_status,omitempty"`
	}{ParsedDouyinVideoInput: parsed}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err == nil {
		if summary, summaryErr := buildTrendRadarSummary(dataDir, time.Now()); summaryErr == nil {
			for _, signal := range summary.VideoLookups {
				idMatch := parsed.VideoID != "" && parsed.VideoID == signal.VideoID
				urlMatch := parsed.NormalizedURL != "" && strings.EqualFold(parsed.NormalizedURL, signal.SearchTerm)
				if idMatch || urlMatch {
					response.ExistingCallID = signal.CallID
					response.ExistingVideoID = signal.VideoID
					response.ExistingAnalysisStatus = signal.AnalysisStatus
					break
				}
			}
		}
	}
	writeJSON(w, response)
}
