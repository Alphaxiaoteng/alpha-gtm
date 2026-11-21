package publishplan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
)

// ManagerEvidenceRecords contains only durable records backed by an
// independently inspected creator-manager row.
type ManagerEvidenceRecords struct {
	PublishRecord *db.PublishRecord        `json:"publish_record,omitempty"`
	ContentRecord *db.PublishContentRecord `json:"content_record,omitempty"`
}

// PersistManagerInspection copies ephemeral Worker screenshots into the
// private evidence directory before recording scheduled or published state.
// Composer success, upload completion, drafts, processing, and reviews are
// intentionally ignored.
func PersistManagerInspection(database *db.DB, planID int64, inspection *Inspection) (*ManagerEvidenceRecords, error) {
	if database == nil || inspection == nil || planID <= 0 || inspection.PlanID != planID {
		return nil, fmt.Errorf("complete creator manager evidence is required")
	}
	records := &ManagerEvidenceRecords{}
	published := inspection.Evidence["publishedState"] == true
	scheduled := inspection.Evidence["scheduledState"] == true
	if published && scheduled {
		return nil, fmt.Errorf("creator manager evidence has conflicting states")
	}
	if published {
		record, err := persistPublishedManagerInspection(database, inspection)
		if err != nil {
			return nil, err
		}
		records.PublishRecord = record
	}
	if scheduled {
		record, err := persistScheduledManagerInspection(database, planID, inspection)
		if err != nil {
			return nil, err
		}
		records.ContentRecord = record
	}
	return records, nil
}

func persistPublishedManagerInspection(database *db.DB, inspection *Inspection) (*db.PublishRecord, error) {
	if inspection.JobID <= 0 ||
		inspection.Evidence["inspectionStatus"] != "inspected" ||
		inspection.Evidence["matchingTitle"] != true ||
		inspection.Evidence["publishedState"] != true {
		return nil, fmt.Errorf("complete creator manager evidence is required")
	}
	observedStatus, _ := inspection.Evidence["observedStatus"].(string)
	postURL, _ := inspection.Evidence["postURL"].(string)
	externalPostID, _ := inspection.Evidence["externalPostID"].(string)
	privatePath, err := copyManagerEvidenceScreenshot(
		database,
		inspection,
		fmt.Sprintf("plan-%d-job-%d-published.png", inspection.PlanID, inspection.JobID),
	)
	if err != nil {
		return nil, fmt.Errorf("published evidence screenshot unavailable: %w", err)
	}
	return database.RecordCreatorBackendPublished(inspection.JobID, db.CreatorBackendEvidence{
		ObservedStatus: strings.TrimSpace(observedStatus),
		PostURL:        strings.TrimSpace(postURL),
		ExternalPostID: strings.TrimSpace(externalPostID),
		ScreenshotPath: privatePath,
	})
}

func persistScheduledManagerInspection(database *db.DB, planID int64, inspection *Inspection) (*db.PublishContentRecord, error) {
	if inspection.JobID <= 0 ||
		inspection.Evidence["inspectionStatus"] != "inspected" ||
		inspection.Evidence["matchingTitle"] != true ||
		inspection.Evidence["scheduledState"] != true {
		return nil, fmt.Errorf("complete scheduled creator manager evidence is required")
	}
	privatePath, err := copyManagerEvidenceScreenshot(
		database,
		inspection,
		fmt.Sprintf("plan-%d-job-%d-scheduled.png", planID, inspection.JobID),
	)
	if err != nil {
		return nil, fmt.Errorf("scheduled evidence screenshot unavailable: %w", err)
	}
	plan, err := (&Service{DB: database}).Get(planID)
	if err != nil {
		return nil, fmt.Errorf("frozen schedule unavailable")
	}
	var target *TargetSnapshot
	for index := range plan.Snapshot.Targets {
		candidate := &plan.Snapshot.Targets[index]
		if candidate.AccountID == inspection.AccountID && candidate.AssetID == inspection.AssetID && candidate.PlatformCode == inspection.PlatformCode {
			if target != nil {
				return nil, fmt.Errorf("frozen schedule unavailable")
			}
			target = candidate
		}
	}
	if target == nil || strings.TrimSpace(target.PublishAt) == "" {
		return nil, fmt.Errorf("frozen schedule unavailable")
	}
	scheduledAt, err := managerScheduledAt(target.PublishAt, inspection.Evidence)
	if err != nil {
		return nil, err
	}
	return database.RecordCreatorBackendScheduled(inspection.JobID, scheduledAt, privatePath)
}

func managerScheduledAt(requested string, evidence map[string]interface{}) (string, error) {
	requestedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(requested))
	if err != nil {
		return "", fmt.Errorf("frozen schedule unavailable")
	}
	effective, _ := evidence["effectiveScheduledAt"].(string)
	if strings.TrimSpace(effective) == "" {
		return requestedAt.UTC().Format(time.RFC3339), nil
	}
	effectiveAt, err := time.Parse(time.RFC3339, strings.TrimSpace(effective))
	if err != nil || effectiveAt.Before(requestedAt) || effectiveAt.Sub(requestedAt) > time.Hour {
		return "", fmt.Errorf("native scheduled time is inconsistent with the frozen request")
	}
	return effectiveAt.UTC().Format(time.RFC3339), nil
}

func copyManagerEvidenceScreenshot(database *db.DB, inspection *Inspection, filename string) (string, error) {
	sourcePath, _ := inspection.Evidence["screenshotPath"].(string)
	body, err := os.ReadFile(strings.TrimSpace(sourcePath))
	if err != nil || len(body) == 0 {
		return "", fmt.Errorf("screenshot unavailable")
	}
	evidenceDir := filepath.Join(database.DataDir, "screenshots", "publish")
	if err := os.MkdirAll(evidenceDir, 0o700); err != nil {
		return "", err
	}
	privatePath := filepath.Join(evidenceDir, filename)
	if err := os.WriteFile(privatePath, body, 0o600); err != nil {
		return "", err
	}
	return privatePath, nil
}
