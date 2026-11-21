package publish

import (
	"encoding/json"
	"testing"
)

func TestCompactPublishEvidenceKeepsOnlySafeCoverTransportOutcome(t *testing.T) {
	raw := compactPublishEvidence(map[string]interface{}{
		"articleCoverNativeUploadObserved": true,
		"articleCoverNativeUploadStatuses": []int{200, 200, 999, 403},
		"reviewedComposerLandmarks": map[string]bool{
			"body": true, "submit": false,
		},
		"videoUploadedSelectionVerified": true,
		"videoUploadedPreviewObserved":   false,
		"videoUploadedPreviewMediaCount": 0,
		"videoUploadedExpectedItemCount": 1,
		"pageInspection":                 map[string]interface{}{"title": "must not persist"},
	})
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal compact evidence: %v", err)
	}
	if got["articleCoverNativeUploadObserved"] != true {
		t.Fatalf("missing native upload observation: %#v", got)
	}
	if _, leaked := got["pageInspection"]; leaked {
		t.Fatalf("page inspection leaked: %#v", got)
	}
	if got["videoUploadedSelectionVerified"] != true ||
		got["videoUploadedPreviewObserved"] != false ||
		got["videoUploadedExpectedItemCount"] != float64(1) {
		t.Fatalf("missing compact video upload evidence: %#v", got)
	}
	landmarks, ok := got["reviewedComposerLandmarks"].(map[string]interface{})
	if !ok || landmarks["body"] != true || landmarks["submit"] != false {
		t.Fatalf("unexpected composer landmarks: %#v", got["reviewedComposerLandmarks"])
	}
	statuses, ok := got["articleCoverNativeUploadStatuses"].([]interface{})
	if !ok || len(statuses) != 2 || statuses[0] != float64(200) || statuses[1] != float64(403) {
		t.Fatalf("unexpected compact statuses: %#v", got["articleCoverNativeUploadStatuses"])
	}
}
