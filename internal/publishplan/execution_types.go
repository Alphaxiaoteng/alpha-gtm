package publishplan

import "context"

type CreatorBackendVerification struct {
	Status    string
	Step      string
	ErrorCode string
}

// CreatorBackendVerifyFunc persists independent creator-backend evidence.
type CreatorBackendVerifyFunc func(context.Context, int64) (CreatorBackendVerification, error)

type TargetRun struct {
	AccountID         int64  `json:"accountId"`
	AssetID           int64  `json:"assetId"`
	PlatformCode      string `json:"platformCode"`
	JobID             int64  `json:"jobId"`
	Status            string `json:"status"`
	Step              string `json:"step"`
	ErrorCode         string `json:"errorCode,omitempty"`
	BrowserClosed     bool   `json:"browserClosed"`
	BrowserCloseError string `json:"browserCloseError,omitempty"`
}

type Execution struct {
	PlanID  int64       `json:"planId"`
	Status  string      `json:"status"`
	Trace   string      `json:"trace"`
	Targets []TargetRun `json:"targets"`
}

// Inspection is a read-only creator-console snapshot.
type Inspection struct {
	PlanID       int64                  `json:"planId"`
	JobID        int64                  `json:"jobId,omitempty"`
	AccountID    int64                  `json:"accountId"`
	AssetID      int64                  `json:"assetId"`
	PlatformCode string                 `json:"platformCode"`
	Location     string                 `json:"location,omitempty"`
	Evidence     map[string]interface{} `json:"evidence,omitempty"`
}
