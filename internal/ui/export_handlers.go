package ui

import (
	"net/http"
	"time"

	"alpha-nexus-egolite/internal/db"
)

// SafeSnapshot is deliberately not a database backup. It contains only local
// business metadata that can be reviewed or moved without carrying a login
// session, browser profile, credential reference, or other secret material.
type SafeSnapshot struct {
	SchemaVersion  int                   `json:"schema_version"`
	ExportedAt     string                `json:"exported_at"`
	Accounts       []safeSnapshotAccount `json:"accounts"`
	Contents       []db.Content          `json:"contents"`
	Templates      []db.PlatformSetting  `json:"templates"`
	Records        []db.PublishRecord    `json:"publish_records"`
	ProxyEndpoints []db.ProxyEndpoint    `json:"proxy_endpoints"`
	Metrics        *db.MetricSummary     `json:"metric_summary"`
}

type safeSnapshotAccount struct {
	PlatformCode string `json:"platform_code"`
	Name         string `json:"name"`
	AccountGroup string `json:"account_group"`
	Remark       string `json:"remark"`
}

func (s *Server) handleAPIExportSnapshot(w http.ResponseWriter, r *http.Request) {
	accounts, err := s.db.ListAccounts()
	if err != nil {
		http.Error(w, "list export accounts failed", http.StatusInternalServerError)
		return
	}
	contents, err := s.db.ListContents()
	if err != nil {
		http.Error(w, "list export contents failed", http.StatusInternalServerError)
		return
	}
	templates, err := s.db.ListPlatformSettings()
	if err != nil {
		http.Error(w, "list export templates failed", http.StatusInternalServerError)
		return
	}
	records, err := s.db.ListPublishRecords(10000)
	if err != nil {
		http.Error(w, "list export records failed", http.StatusInternalServerError)
		return
	}
	endpoints, err := s.db.ListProxyEndpoints()
	if err != nil {
		http.Error(w, "list export proxy endpoints failed", http.StatusInternalServerError)
		return
	}
	metrics, err := s.db.GetMetricSummary()
	if err != nil {
		http.Error(w, "build export metrics failed", http.StatusInternalServerError)
		return
	}
	safeAccounts := make([]safeSnapshotAccount, 0, len(accounts))
	for _, account := range accounts {
		safeAccounts = append(safeAccounts, safeSnapshotAccount{
			PlatformCode: account.PlatformCode,
			Name:         account.Name,
			AccountGroup: account.AccountGroup,
			Remark:       account.Remark,
		})
	}
	writeJSON(w, SafeSnapshot{
		SchemaVersion:  1,
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		Accounts:       safeAccounts,
		Contents:       contents,
		Templates:      templates,
		Records:        records,
		ProxyEndpoints: endpoints,
		Metrics:        metrics,
	})
}
