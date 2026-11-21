package db

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// RadarPoolTerm is one search term as configured inside a pool. The store keeps
// a snapshot so attribution is a table join and never depends on the current
// config file matching a historical string.
type RadarPoolTerm struct {
	PoolID   string
	PoolName string
	Term     string
}

// RadarRunCall is one paid provider call inside a run.
type RadarRunCall struct {
	CallID          int64  `json:"call_id,omitempty"`
	Term            string `json:"term"`
	Platform        string `json:"platform"`
	EndpointID      string `json:"endpoint_id"`
	Status          string `json:"status"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	ItemCount       int64  `json:"item_count"`
	KeptCount       int    `json:"kept_count"`
	AdCount         int    `json:"ad_count"`
	GuideCount      int    `json:"guide_count"`
	UnreadableCount int    `json:"unreadable_count"`
	DurationMS      int64  `json:"duration_ms"`
	RawPath         string `json:"raw_path,omitempty"`
}

// RadarRun mirrors the JSON run record the UI already renders.
type RadarRun struct {
	ID               string         `json:"id"`
	NicheID          string         `json:"niche_id"`
	NicheName        string         `json:"niche_name"`
	Platform         string         `json:"platform"`
	EndpointID       string         `json:"endpoint_id"`
	Terms            []string       `json:"terms"`
	State            string         `json:"state"`
	StartedAt        string         `json:"started_at"`
	FinishedAt       string         `json:"finished_at,omitempty"`
	LastRetryAt      string         `json:"last_retry_at,omitempty"`
	ItemCap          int            `json:"item_cap"`
	MaxCalls         int            `json:"max_calls"`
	EstimatedCostUSD float64        `json:"estimated_cost_usd"`
	CostBasis        string         `json:"cost_basis,omitempty"`
	Calls            []RadarRunCall `json:"calls"`
	TotalCalls       int            `json:"total_calls"`
	Succeeded        int            `json:"succeeded"`
	Failed           int            `json:"failed"`
	Skipped          int            `json:"skipped"`
	Items            int64          `json:"items"`
	Kept             int            `json:"kept"`
	// 卡计数由调用明细汇总，和 run 文件里的聚合值同一口径。
	AdCards    int `json:"ad_cards"`
	GuideCards int `json:"guide_cards"`
	Unreadable int `json:"unreadable"`
}

// RadarCapture is one work observed in one paid call.
type RadarCapture struct {
	Platform    string
	WorkID      string
	PoolID      string
	PoolName    string
	Term        string
	Author      string
	Title       string
	URL         string
	PublishedAt string
	CallID      int64
	RunID       string
	Play        int64
	Likes       int64
	Collects    int64
	Shares      int64
	Comments    int64
	ObservedAt  string
}

func normalizeRadarTerm(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// SyncRadarPoolTerms replaces the vocabulary snapshot in one transaction.
// Deleted terms drop out; existing terms keep pointing at the same pool.
func (db *DB) SyncRadarPoolTerms(terms []RadarPoolTerm) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM radar_pool_terms`); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	seen := map[string]bool{}
	for _, term := range terms {
		normalized := normalizeRadarTerm(term.Term)
		if normalized == "" || strings.TrimSpace(term.PoolID) == "" {
			continue
		}
		key := term.PoolID + "\x00" + normalized
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := tx.Exec(`INSERT INTO radar_pool_terms(pool_id,pool_name,term,normalized,updated_at)
			VALUES(?,?,?,?,?) ON CONFLICT(pool_id,normalized) DO UPDATE SET
			pool_name=excluded.pool_name, term=excluded.term, updated_at=excluded.updated_at`,
			term.PoolID, term.PoolName, term.Term, normalized, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RadarPoolForTerm resolves a search term to its configured pool. A term that
// is not in any pool returns found=false: that is how "历史记录" is now
// explained instead of guessed.
func (db *DB) RadarPoolForTerm(term string) (poolID, poolName string, found bool, err error) {
	normalized := normalizeRadarTerm(term)
	if normalized == "" {
		return "", "", false, nil
	}
	err = db.QueryRow(`SELECT pool_id,pool_name FROM radar_pool_terms WHERE normalized=? ORDER BY pool_id LIMIT 1`, normalized).
		Scan(&poolID, &poolName)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	return poolID, poolName, true, nil
}

// RecordRadarRun upserts one run and replaces its call rows, so a retried write
// never duplicates history.
func (db *DB) RecordRadarRun(run RadarRun) error {
	termsJSON, err := json.Marshal(run.Terms)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO radar_capture_runs(
		id,pool_id,pool_name,platform,endpoint_id,state,started_at,finished_at,last_retry_at,terms_json,
		item_cap,max_calls,estimated_cost_usd,cost_basis,total_calls,succeeded,failed,skipped,kept,items,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			pool_id=excluded.pool_id, pool_name=excluded.pool_name, platform=excluded.platform,
			endpoint_id=excluded.endpoint_id, state=excluded.state, started_at=excluded.started_at,
			finished_at=excluded.finished_at, last_retry_at=excluded.last_retry_at,
			terms_json=excluded.terms_json, item_cap=excluded.item_cap,
			max_calls=excluded.max_calls, estimated_cost_usd=excluded.estimated_cost_usd,
			cost_basis=excluded.cost_basis, total_calls=excluded.total_calls, succeeded=excluded.succeeded,
			failed=excluded.failed, skipped=excluded.skipped, kept=excluded.kept, items=excluded.items`,
		run.ID, run.NicheID, run.NicheName, run.Platform, run.EndpointID, run.State,
		run.StartedAt, run.FinishedAt, run.LastRetryAt, string(termsJSON), run.ItemCap, run.MaxCalls,
		run.EstimatedCostUSD, run.CostBasis, run.TotalCalls, run.Succeeded, run.Failed,
		run.Skipped, run.Kept, run.Items, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM radar_capture_calls WHERE run_id=?`, run.ID); err != nil {
		return err
	}
	for _, call := range run.Calls {
		if _, err := tx.Exec(`INSERT INTO radar_capture_calls(
			run_id,call_id,term,platform,endpoint_id,status,error_code,error_message,
			item_count,kept_count,ad_count,guide_count,unreadable_count,duration_ms,raw_path,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			run.ID, call.CallID, call.Term, call.Platform, call.EndpointID, call.Status,
			call.ErrorCode, call.ErrorMessage, call.ItemCount, call.KeptCount,
			call.AdCount, call.GuideCount, call.UnreadableCount,
			call.DurationMS, call.RawPath, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RecordRadarCapture upserts the work and appends one observation. It returns
// inserted=false when this exact work was already recorded for this call, so
// re-ingesting a snapshot is safe.
func (db *DB) RecordRadarCapture(c RadarCapture) (bool, error) {
	if strings.TrimSpace(c.Platform) == "" || strings.TrimSpace(c.WorkID) == "" {
		return false, nil
	}
	if strings.TrimSpace(c.ObservedAt) == "" {
		c.ObservedAt = time.Now().UTC().Format(time.RFC3339)
	}
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var workPK int64
	err = tx.QueryRow(`SELECT id FROM radar_works WHERE platform=? AND work_id=?`, c.Platform, c.WorkID).Scan(&workPK)
	switch {
	case err == sql.ErrNoRows:
		result, insertErr := tx.Exec(`INSERT INTO radar_works(
			platform,work_id,pool_id,pool_name,term,author,title,url,published_at,first_seen_at,last_seen_at,seen_count)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,1)`,
			c.Platform, c.WorkID, c.PoolID, c.PoolName, c.Term, c.Author, c.Title, c.URL,
			c.PublishedAt, c.ObservedAt, c.ObservedAt)
		if insertErr != nil {
			return false, insertErr
		}
		workPK, err = result.LastInsertId()
		if err != nil {
			return false, err
		}
	case err != nil:
		return false, err
	default:
		// Replaying an older capture (backfill) must not move last_seen_at
		// backwards, and empty identity fields are only filled, never cleared.
		if _, err := tx.Exec(`UPDATE radar_works SET
			last_seen_at=CASE WHEN last_seen_at<? THEN ? ELSE last_seen_at END,
			published_at=CASE WHEN published_at='' THEN ? ELSE published_at END,
			pool_id=CASE WHEN pool_id='' THEN ? ELSE pool_id END,
			pool_name=CASE WHEN pool_name='' THEN ? ELSE pool_name END,
			term=CASE WHEN term='' THEN ? ELSE term END,
			author=CASE WHEN author='' THEN ? ELSE author END,
			title=CASE WHEN title='' THEN ? ELSE title END,
			url=CASE WHEN url='' THEN ? ELSE url END
			WHERE id=?`,
			c.ObservedAt, c.ObservedAt, c.PublishedAt, c.PoolID, c.PoolName,
			c.Term, c.Author, c.Title, c.URL, workPK); err != nil {
			return false, err
		}
	}
	result, err := tx.Exec(`INSERT OR IGNORE INTO radar_observations(
		work_pk,platform,work_id,pool_id,pool_name,term,call_id,run_id,
		play,likes,collects,shares,comments,observed_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		workPK, c.Platform, c.WorkID, c.PoolID, c.PoolName, c.Term, c.CallID, c.RunID,
		c.Play, c.Likes, c.Collects, c.Shares, c.Comments, c.ObservedAt)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE radar_works SET seen_count=(SELECT COUNT(*) FROM radar_observations WHERE work_pk=?) WHERE id=?`, workPK, workPK); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return affected > 0, nil
}

// RadarStoreStats returns row counts for the radar store.
func (db *DB) RadarStoreStats() (map[string]int64, error) {
	out := map[string]int64{}
	for _, table := range []string{"radar_pool_terms", "radar_capture_runs", "radar_capture_calls", "radar_works", "radar_observations"} {
		var count int64
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			return nil, err
		}
		out[table] = count
	}
	return out, nil
}

// RadarRuns lists persisted runs newest first.
func (db *DB) RadarRuns(nicheID string, limit int) ([]RadarRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT id,pool_id,pool_name,platform,endpoint_id,state,started_at,finished_at,last_retry_at,terms_json,
		item_cap,max_calls,estimated_cost_usd,cost_basis,total_calls,succeeded,failed,skipped,kept,items
		FROM radar_capture_runs`
	args := []any{}
	if strings.TrimSpace(nicheID) != "" {
		query += ` WHERE pool_id=?`
		args = append(args, nicheID)
	}
	query += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]RadarRun, 0, limit)
	for rows.Next() {
		var run RadarRun
		var termsJSON string
		if err := rows.Scan(&run.ID, &run.NicheID, &run.NicheName, &run.Platform, &run.EndpointID,
			&run.State, &run.StartedAt, &run.FinishedAt, &run.LastRetryAt, &termsJSON, &run.ItemCap, &run.MaxCalls,
			&run.EstimatedCostUSD, &run.CostBasis, &run.TotalCalls, &run.Succeeded, &run.Failed,
			&run.Skipped, &run.Kept, &run.Items); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(termsJSON), &run.Terms)
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range runs {
		calls, err := db.radarRunCalls(runs[i].ID)
		if err != nil {
			return nil, err
		}
		runs[i].Calls = calls
		for _, call := range calls {
			runs[i].AdCards += call.AdCount
			runs[i].GuideCards += call.GuideCount
			runs[i].Unreadable += call.UnreadableCount
		}
	}
	return runs, nil
}

func (db *DB) radarRunCalls(runID string) ([]RadarRunCall, error) {
	rows, err := db.Query(`SELECT call_id,term,platform,endpoint_id,status,error_code,error_message,
		item_count,kept_count,ad_count,guide_count,unreadable_count,duration_ms,raw_path
		FROM radar_capture_calls WHERE run_id=? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	calls := make([]RadarRunCall, 0, 4)
	for rows.Next() {
		var call RadarRunCall
		if err := rows.Scan(&call.CallID, &call.Term, &call.Platform, &call.EndpointID, &call.Status,
			&call.ErrorCode, &call.ErrorMessage, &call.ItemCount, &call.KeptCount,
			&call.AdCount, &call.GuideCount, &call.UnreadableCount,
			&call.DurationMS, &call.RawPath); err != nil {
			return nil, err
		}
		calls = append(calls, call)
	}
	return calls, rows.Err()
}

// RadarRunIDsForCalls maps every persisted paid call to the run that made it,
// so the export can name the batch a work came from without re-reading the run
// files.
func (db *DB) RadarRunIDsForCalls() (map[int64]string, error) {
	rows, err := db.Query(`SELECT call_id,run_id FROM radar_capture_calls WHERE call_id>0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var callID int64
		var runID string
		if err := rows.Scan(&callID, &runID); err != nil {
			return nil, err
		}
		if strings.TrimSpace(runID) != "" {
			out[callID] = runID
		}
	}
	return out, rows.Err()
}

// RadarWorkAttribution answers, for one work, which pool and term it belongs
// to, which call collected it and how many times it was re-collected.
func (db *DB) RadarWorkAttribution(platform, workID string) (map[string]any, error) {
	var workPK int64
	var poolID, poolName, term, title, author, url, publishedAt, firstSeen, lastSeen string
	var seenCount int64
	err := db.QueryRow(`SELECT id,pool_id,pool_name,term,title,author,url,published_at,first_seen_at,last_seen_at,seen_count
		FROM radar_works WHERE platform=? AND work_id=?`, platform, workID).
		Scan(&workPK, &poolID, &poolName, &term, &title, &author, &url, &publishedAt, &firstSeen, &lastSeen, &seenCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT term,pool_id,pool_name,call_id,observed_at,play,likes,collects,shares,comments
		FROM radar_observations WHERE work_pk=? ORDER BY observed_at`, workPK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	observations := make([]map[string]any, 0, 8)
	for rows.Next() {
		var obsTerm, obsPoolID, obsPoolName, observedAt string
		var callID, play, likes, collects, shares, comments int64
		if err := rows.Scan(&obsTerm, &obsPoolID, &obsPoolName, &callID, &observedAt,
			&play, &likes, &collects, &shares, &comments); err != nil {
			return nil, err
		}
		observations = append(observations, map[string]any{
			"term": obsTerm, "pool_id": obsPoolID, "pool_name": obsPoolName,
			"call_id": callID, "observed_at": observedAt,
			"play": play, "likes": likes, "collects": collects, "shares": shares, "comments": comments,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{
		"platform": platform, "work_id": workID, "pool_id": poolID, "pool_name": poolName,
		"term": term, "title": title, "author": author, "url": url,
		"published_at":  publishedAt,
		"first_seen_at": firstSeen, "last_seen_at": lastSeen, "seen_count": seenCount,
		"observations": observations,
	}, nil
}

// RadarTermPoolMap returns term -> pool name for export enrichment.
func (db *DB) RadarTermPoolMap() (map[string]string, error) {
	rows, err := db.Query(`SELECT normalized,pool_name FROM radar_pool_terms`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var normalized, poolName string
		if err := rows.Scan(&normalized, &poolName); err != nil {
			return nil, err
		}
		if existing, ok := out[normalized]; ok && existing != poolName {
			out[normalized] = existing + "、" + poolName
			continue
		}
		out[normalized] = poolName
	}
	return out, rows.Err()
}

// RadarUnattributedTerms lists collected search terms that are in no pool.
// This is the honest replacement for the catch-all "历史记录" bucket.
func (db *DB) RadarUnattributedTerms() ([]map[string]any, error) {
	rows, err := db.Query(`SELECT term,platform,COUNT(*) AS observations,
		COUNT(DISTINCT work_id) AS works,MIN(observed_at),MAX(observed_at)
		FROM radar_observations WHERE pool_id='' GROUP BY term,platform
		ORDER BY observations DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]map[string]any, 0, 16)
	for rows.Next() {
		var term, platform, firstSeen, lastSeen string
		var observations, works int64
		if err := rows.Scan(&term, &platform, &observations, &works, &firstSeen, &lastSeen); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"term": term, "platform": platform, "observations": observations,
			"works": works, "first_seen_at": firstSeen, "last_seen_at": lastSeen,
		})
	}
	return out, rows.Err()
}

// RadarWorkSeenCounts returns "platform|work_id" ->真实抓取次数, so exports use
// the deduplicated count instead of the summary's title-based merge.
func (db *DB) RadarWorkSeenCounts() (map[string]int, error) {
	rows, err := db.Query(`SELECT platform,work_id,seen_count FROM radar_works`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var platform, workID string
		var seen int
		if err := rows.Scan(&platform, &workID, &seen); err != nil {
			return nil, err
		}
		out[platform+"|"+workID] = seen
	}
	return out, rows.Err()
}
