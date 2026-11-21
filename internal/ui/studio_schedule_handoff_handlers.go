package ui

import (
	"net/http"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/publishplan"
	"alpha-nexus-egolite/internal/tools"
)

type studioScheduleHandoffCreateBody struct {
	WorkflowID        string `json:"workflowId"`
	ContentID         int64  `json:"contentId"`
	ScriptID          int64  `json:"scriptId"`
	OriginalLocalTime string `json:"originalLocalTime"`
	Timezone          string `json:"timezone"`
}

type publishPlanFreezeHandoffBody struct {
	HandoffID        int64                         `json:"handoffId"`
	Targets          []publishplan.TargetInput     `json:"targets"`
	CompliancePolicy *publishplan.CompliancePolicy `json:"compliancePolicy"`
	Concurrency      int                           `json:"concurrency"`
}

func studioScheduleHandoffID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) callStudioScheduleHandoffTool(w http.ResponseWriter, r *http.Request, name string, args map[string]interface{}) *tools.Result {
	result, _ := tools.Default.Call(r.Context(), s.toolRuntime, name, args)
	if result == nil {
		writeToolsError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
		return nil
	}
	if !result.OK {
		status := http.StatusBadRequest
		if strings.HasPrefix(result.Error, "EGO_SCHEDULE_") || result.Error == "EGO_PUBLISH_SCHEDULE_UNVERIFIED" {
			status = http.StatusConflict
		}
		writeToolsError(w, status, result.Error)
		return nil
	}
	return result
}

func (s *Server) handleAPIStudioScheduleHandoffs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 200 {
				writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
				return
			}
			limit = parsed
		}
		result := s.callStudioScheduleHandoffTool(w, r, "studio.schedule-handoff.list", map[string]interface{}{"limit": limit})
		if result != nil {
			writeJSON(w, result.Data["handoffs"])
		}
	case http.MethodPost:
		var body studioScheduleHandoffCreateBody
		if !decodePublishPlanJSON(w, r, &body) {
			return
		}
		result := s.callStudioScheduleHandoffTool(w, r, "studio.schedule-handoff.create", map[string]interface{}{
			"workflow_id": body.WorkflowID, "content_id": body.ContentID, "script_id": body.ScriptID,
			"original_local_time": body.OriginalLocalTime, "timezone": body.Timezone,
		})
		if result != nil {
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, result.Data["handoff"])
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIStudioScheduleHandoff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, ok := studioScheduleHandoffID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	result := s.callStudioScheduleHandoffTool(w, r, "studio.schedule-handoff.get", map[string]interface{}{"handoff_id": id})
	if result != nil {
		writeJSON(w, result.Data["handoff"])
	}
}

func (s *Server) handleAPIStudioScheduleHandoffCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, ok := studioScheduleHandoffID(r)
	if !ok {
		writeToolsError(w, http.StatusBadRequest, "VALIDATION_FAILED")
		return
	}
	result := s.callStudioScheduleHandoffTool(w, r, "studio.schedule-handoff.cancel", map[string]interface{}{"handoff_id": id})
	if result != nil {
		writeJSON(w, result.Data["handoff"])
	}
}

func (s *Server) handleAPIPublishPlanFreezeHandoff(w http.ResponseWriter, r *http.Request) {
	var body publishPlanFreezeHandoffBody
	if !decodePublishPlanJSON(w, r, &body) {
		return
	}
	result := s.callStudioScheduleHandoffTool(w, r, "publish.plan.freeze-handoff", map[string]interface{}{
		"handoff_id": body.HandoffID, "targets": body.Targets,
		"compliance_policy": body.CompliancePolicy, "concurrency": body.Concurrency,
	})
	if result != nil {
		writeJSON(w, result.Data["plan"])
	}
}
