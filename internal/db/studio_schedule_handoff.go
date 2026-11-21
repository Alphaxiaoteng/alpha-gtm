package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/egopublish"
)

const (
	StudioScheduleHandoffReady         = "ready"
	StudioScheduleHandoffFrozen        = "frozen"
	StudioScheduleHandoffSuperseded    = "superseded"
	StudioScheduleHandoffCancelled     = "cancelled"
	StudioScheduleHandoffExpired       = "expired"
	studioScheduleHandoffWriteAttempts = 3
)

type StudioScheduleHandoffInput struct {
	WorkflowID        string `json:"workflow_id"`
	ContentID         int64  `json:"content_id"`
	ScriptID          int64  `json:"script_id,omitempty"`
	OriginalLocalTime string `json:"original_local_time"`
	Timezone          string `json:"timezone"`
}

// StudioScheduleHandoff is an editorial handoff only. It never contains an
// approval, nonce, browser state, or instruction to run at a future instant.
type StudioScheduleHandoff struct {
	ID                int64   `json:"id"`
	WorkflowID        string  `json:"workflow_id"`
	ContentID         int64   `json:"content_id"`
	ScriptID          int64   `json:"script_id,omitempty"`
	Revision          int64   `json:"revision"`
	MediaMode         string  `json:"media_mode"`
	MediaIDs          []int64 `json:"media_ids"`
	CoverMediaID      int64   `json:"cover_media_id,omitempty"`
	NativeVideoCover  bool    `json:"native_video_cover,omitempty"`
	CanonicalHash     string  `json:"canonical_hash"`
	PlannedAtUTC      string  `json:"planned_at_utc"`
	Timezone          string  `json:"timezone"`
	OriginalLocalTime string  `json:"original_local_time"`
	IntentHash        string  `json:"intent_hash"`
	Status            string  `json:"status"`
	FrozenPlanID      int64   `json:"frozen_plan_id,omitempty"`
	SnapshotHash      string  `json:"snapshot_hash,omitempty"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

type studioScheduleQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func studioScheduleHandoffEffectiveStatus(ctx context.Context, query studioScheduleQuery, handoff *StudioScheduleHandoff, now time.Time) (string, error) {
	if handoff == nil || handoff.Status != StudioScheduleHandoffReady {
		return "", nil
	}
	plannedAt, err := time.Parse(time.RFC3339, handoff.PlannedAtUTC)
	if err != nil || !plannedAt.After(now.UTC()) {
		return StudioScheduleHandoffExpired, nil
	}
	content, contentErr := studioScheduleContentFor(query, ctx, handoff.ContentID)
	media, mediaErr := studioScheduleMediaFor(query, ctx, handoff.ContentID, handoff.MediaIDs, handoff.CoverMediaID)
	if contentErr != nil || mediaErr != nil {
		return StudioScheduleHandoffSuperseded, nil
	}
	canonical, hashErr := studioScheduleHash(content, handoff.MediaIDs, handoff.CoverMediaID, handoff.NativeVideoCover, media)
	if hashErr != nil || content.Revision != handoff.Revision || canonical != handoff.CanonicalHash || content.Type != handoff.MediaMode {
		return StudioScheduleHandoffSuperseded, nil
	}
	return "", nil
}

func isStudioScheduleHandoffBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked") || strings.Contains(message, "sqlite_busy")
}

type studioScheduleContent struct {
	ID       int64
	Type     string
	Title    string
	Caption  string
	Body     string
	TagsJSON string
	Status   string
	Revision int64
}

type studioScheduleMedia struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	SHA256   string `json:"sha256"`
	Role     string `json:"role"`
	Position int    `json:"position"`
}

type studioScheduleCanonical struct {
	Content  studioScheduleContent `json:"content"`
	MediaIDs []int64               `json:"media_ids"`
	CoverID  int64                 `json:"cover_media_id,omitempty"`
	Native   bool                  `json:"native_video_cover,omitempty"`
	Media    []studioScheduleMedia `json:"media"`
}

func parseStudioScheduleTime(localTime, timezone string) (time.Time, string, error) {
	localTime = strings.TrimSpace(localTime)
	timezone = strings.TrimSpace(timezone)
	location, err := time.LoadLocation(timezone)
	if timezone == "" || err != nil {
		return time.Time{}, "", fmt.Errorf("%s", egopublish.ScheduleTimezoneInvalid)
	}
	wall, err := time.Parse("2006-01-02T15:04", localTime)
	if err != nil || wall.Format("2006-01-02T15:04") != localTime {
		return time.Time{}, "", fmt.Errorf("%s", egopublish.ScheduleTimezoneInvalid)
	}
	naive := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), 0, 0, time.UTC)
	offsets := map[int]struct{}{}
	for hour := -48; hour <= 48; hour++ {
		_, offset := naive.Add(time.Duration(hour) * time.Hour).In(location).Zone()
		offsets[offset] = struct{}{}
	}
	candidates := make([]time.Time, 0, 2)
	for offset := range offsets {
		candidate := naive.Add(-time.Duration(offset) * time.Second)
		actual := candidate.In(location)
		if actual.Year() == wall.Year() && actual.Month() == wall.Month() && actual.Day() == wall.Day() && actual.Hour() == wall.Hour() && actual.Minute() == wall.Minute() {
			candidates = append(candidates, candidate)
		}
	}
	switch len(candidates) {
	case 1:
		return candidates[0].UTC(), timezone, nil
	case 0:
		return time.Time{}, "", fmt.Errorf("%s", egopublish.ScheduleTimezoneInvalid)
	default:
		return time.Time{}, "", fmt.Errorf("%s", egopublish.ScheduleDSTAmbiguous)
	}
}

func studioScheduleContentFor(query studioScheduleQuery, ctx context.Context, contentID int64) (studioScheduleContent, error) {
	var content studioScheduleContent
	err := query.QueryRowContext(ctx, `SELECT id,type,COALESCE(title,''),COALESCE(caption,''),COALESCE(body,''),COALESCE(tags_json,'[]'),status,COALESCE(revision,1)
		FROM contents WHERE id=?`, contentID).Scan(
		&content.ID, &content.Type, &content.Title, &content.Caption, &content.Body, &content.TagsJSON, &content.Status, &content.Revision,
	)
	if err == sql.ErrNoRows {
		return content, fmt.Errorf("content %d not found", contentID)
	}
	if err != nil {
		return content, err
	}
	if !validContentType(content.Type) || content.Status == "archived" {
		return content, fmt.Errorf("reviewed content is unavailable")
	}
	var tags []any
	if err := json.Unmarshal([]byte(content.TagsJSON), &tags); err != nil {
		return content, fmt.Errorf("content tags must be a JSON array")
	}
	canonical, _ := json.Marshal(tags)
	content.TagsJSON = string(canonical)
	return content, nil
}

func studioScheduleMediaFor(query studioScheduleQuery, ctx context.Context, contentID int64, ids []int64, coverID int64) ([]studioScheduleMedia, error) {
	seen := make(map[int64]bool, len(ids)+1)
	ordered := append([]int64(nil), ids...)
	if coverID > 0 {
		ordered = append(ordered, coverID)
	}
	media := make([]studioScheduleMedia, 0, len(ordered))
	for _, id := range ordered {
		if id <= 0 || seen[id] {
			return nil, fmt.Errorf("reviewed media selection is invalid")
		}
		seen[id] = true
		var item studioScheduleMedia
		err := query.QueryRowContext(ctx, `SELECT id,kind,COALESCE(sha256,''),COALESCE(role,'content'),COALESCE(position,0)
			FROM media_assets WHERE id=? AND content_id=?`, id, contentID).Scan(&item.ID, &item.Kind, &item.SHA256, &item.Role, &item.Position)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("reviewed media is unavailable")
		}
		if err != nil || len(strings.TrimSpace(item.SHA256)) != 64 {
			return nil, fmt.Errorf("reviewed media is unavailable")
		}
		item.SHA256 = strings.ToLower(item.SHA256)
		media = append(media, item)
	}
	return media, nil
}

func studioScheduleSelection(query studioScheduleQuery, ctx context.Context, content studioScheduleContent) ([]int64, int64, bool, error) {
	rows, err := query.QueryContext(ctx, `SELECT id,kind,COALESCE(role,'content'),COALESCE(position,0)
		FROM media_assets WHERE content_id=? ORDER BY CASE WHEN role='cover' THEN 0 ELSE 1 END,position,id`, content.ID)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()
	type candidate struct {
		id         int64
		kind, role string
	}
	items := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		var position int
		if err := rows.Scan(&item.id, &item.kind, &item.role, &position); err != nil {
			return nil, 0, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, err
	}
	ids := make([]int64, 0, len(items))
	var coverID int64
	switch content.Type {
	case "article", "graph_text":
		for _, item := range items {
			if item.kind == "image" {
				ids = append(ids, item.id)
			}
		}
	case "video":
		for _, item := range items {
			if item.kind == "video" {
				ids = append(ids, item.id)
			}
			if coverID == 0 && item.kind == "image" && item.role == "cover" {
				coverID = item.id
			}
		}
		if len(ids) != 1 {
			return nil, 0, false, fmt.Errorf("video handoff requires exactly one reviewed video")
		}
	default:
		return nil, 0, false, fmt.Errorf("reviewed content mode is invalid")
	}
	if content.Type == "graph_text" && (len(ids) < 1 || len(ids) > 9) {
		return nil, 0, false, fmt.Errorf("graph_text handoff requires 1-9 reviewed images")
	}
	if content.Type == "article" && len(ids) > 9 {
		return nil, 0, false, fmt.Errorf("article handoff accepts at most 9 reviewed images")
	}
	return ids, coverID, content.Type == "video" && coverID == 0, nil
}

func studioScheduleHash(content studioScheduleContent, ids []int64, coverID int64, native bool, media []studioScheduleMedia) (string, error) {
	encoded, err := json.Marshal(studioScheduleCanonical{Content: content, MediaIDs: ids, CoverID: coverID, Native: native, Media: media})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func studioScheduleIntentHash(handoff *StudioScheduleHandoff) string {
	payload := struct {
		WorkflowID, CanonicalHash, PlannedAtUTC, Timezone, OriginalLocalTime string
		ContentID, ScriptID, Revision                                        int64
		MediaMode                                                            string
		MediaIDs                                                             []int64
		CoverMediaID                                                         int64
		NativeVideoCover                                                     bool
	}{
		WorkflowID: handoff.WorkflowID, ContentID: handoff.ContentID, ScriptID: handoff.ScriptID, Revision: handoff.Revision,
		MediaMode: handoff.MediaMode, MediaIDs: handoff.MediaIDs, CoverMediaID: handoff.CoverMediaID, NativeVideoCover: handoff.NativeVideoCover,
		CanonicalHash: handoff.CanonicalHash, PlannedAtUTC: handoff.PlannedAtUTC, Timezone: handoff.Timezone, OriginalLocalTime: handoff.OriginalLocalTime,
	}
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func scanStudioScheduleHandoff(scanner interface{ Scan(...any) error }) (*StudioScheduleHandoff, error) {
	var handoff StudioScheduleHandoff
	var scriptID, coverID, frozenPlanID sql.NullInt64
	var nativeCover int
	var mediaIDs string
	if err := scanner.Scan(
		&handoff.ID, &handoff.WorkflowID, &handoff.ContentID, &scriptID, &handoff.Revision, &handoff.MediaMode, &mediaIDs, &coverID, &nativeCover,
		&handoff.CanonicalHash, &handoff.PlannedAtUTC, &handoff.Timezone, &handoff.OriginalLocalTime, &handoff.IntentHash, &handoff.Status,
		&frozenPlanID, &handoff.SnapshotHash, &handoff.CreatedAt, &handoff.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(mediaIDs), &handoff.MediaIDs); err != nil {
		return nil, fmt.Errorf("decode studio schedule media: %w", err)
	}
	handoff.NativeVideoCover = nativeCover == 1
	if scriptID.Valid {
		handoff.ScriptID = scriptID.Int64
	}
	if coverID.Valid {
		handoff.CoverMediaID = coverID.Int64
	}
	if frozenPlanID.Valid {
		handoff.FrozenPlanID = frozenPlanID.Int64
	}
	return &handoff, nil
}

const studioScheduleHandoffColumns = `id,workflow_id,content_id,script_id,revision,media_mode,media_ids_json,cover_media_id,native_video_cover,
	canonical_hash,planned_at_utc,timezone,original_local_time,intent_hash,status,frozen_plan_id,COALESCE(snapshot_hash,''),created_at,updated_at`

func (db *DB) createStudioScheduleHandoffState(ctx context.Context, input StudioScheduleHandoffInput) (*StudioScheduleHandoff, error) {
	content, err := studioScheduleContentFor(db, ctx, input.ContentID)
	if err != nil {
		return nil, err
	}
	ids, coverID, nativeCover, err := studioScheduleSelection(db, ctx, content)
	if err != nil {
		return nil, err
	}
	media, err := studioScheduleMediaFor(db, ctx, content.ID, ids, coverID)
	if err != nil {
		return nil, err
	}
	canonicalHash, err := studioScheduleHash(content, ids, coverID, nativeCover, media)
	if err != nil {
		return nil, err
	}
	plannedAt, timezone, err := parseStudioScheduleTime(input.OriginalLocalTime, input.Timezone)
	if err != nil {
		return nil, err
	}
	handoff := &StudioScheduleHandoff{
		WorkflowID: strings.TrimSpace(input.WorkflowID), ContentID: content.ID, ScriptID: input.ScriptID, Revision: content.Revision,
		MediaMode: content.Type, MediaIDs: ids, CoverMediaID: coverID, NativeVideoCover: nativeCover, CanonicalHash: canonicalHash,
		PlannedAtUTC: plannedAt.Format(time.RFC3339), Timezone: timezone, OriginalLocalTime: strings.TrimSpace(input.OriginalLocalTime), Status: StudioScheduleHandoffReady,
	}
	handoff.IntentHash = studioScheduleIntentHash(handoff)
	return handoff, nil
}

func (db *DB) CreateStudioScheduleHandoff(ctx context.Context, input StudioScheduleHandoffInput) (*StudioScheduleHandoff, bool, error) {
	input.WorkflowID = strings.TrimSpace(input.WorkflowID)
	if input.WorkflowID == "" || input.ContentID <= 0 || input.ScriptID < 0 {
		return nil, false, fmt.Errorf("workflow and content are required")
	}
	workflow, err := db.GetCreationWorkflow(ctx, input.WorkflowID)
	if err != nil {
		return nil, false, err
	}
	if workflow.Stage != "review" {
		return nil, false, fmt.Errorf("%s", egopublish.ScheduleWorkflowNotReview)
	}
	handoff, err := db.createStudioScheduleHandoffState(ctx, input)
	if err != nil {
		return nil, false, err
	}
	plannedAt, err := time.Parse(time.RFC3339, handoff.PlannedAtUTC)
	if err != nil || !plannedAt.After(time.Now().UTC()) {
		return nil, false, fmt.Errorf("%s", egopublish.ScheduleTimeInvalid)
	}
	if input.ScriptID > 0 {
		script, err := db.GetStudioScript(input.ScriptID)
		if err != nil || script.ContentType != handoff.MediaMode {
			return nil, false, fmt.Errorf("reviewed script does not match content")
		}
	}
	var lastErr error
	for attempt := 0; attempt < studioScheduleHandoffWriteAttempts; attempt++ {
		created, wasCreated, createErr := db.createStudioScheduleHandoffAttempt(ctx, handoff)
		if createErr == nil {
			return created, wasCreated, nil
		}
		if !isStudioScheduleHandoffBusy(createErr) {
			return nil, false, createErr
		}
		lastErr = createErr
		if attempt+1 < studioScheduleHandoffWriteAttempts {
			timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, false, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil, false, lastErr
}

// createStudioScheduleHandoffAttempt serializes ready-row replacement with a
// SQLite write transaction so an exact replay cannot supersede itself through
// a second connection. The bounded retry belongs to its caller.
func (db *DB) createStudioScheduleHandoffAttempt(ctx context.Context, handoff *StudioScheduleHandoff) (*StudioScheduleHandoff, bool, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	var existingID int64
	err = conn.QueryRowContext(ctx, `SELECT id FROM studio_schedule_handoffs WHERE workflow_id=? AND intent_hash=? AND status='ready'`, handoff.WorkflowID, handoff.IntentHash).Scan(&existingID)
	if err == nil {
		existing, loadErr := loadStudioScheduleHandoff(ctx, conn, existingID)
		if loadErr != nil {
			return nil, false, loadErr
		}
		effectiveStatus, effectiveErr := studioScheduleHandoffEffectiveStatus(ctx, conn, existing, time.Now().UTC())
		if effectiveErr != nil {
			return nil, false, effectiveErr
		}
		if effectiveStatus == "" {
			if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
				return nil, false, err
			}
			committed = true
			return existing, false, nil
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := conn.ExecContext(ctx, `UPDATE studio_schedule_handoffs SET status=?,updated_at=? WHERE id=? AND status='ready'`, effectiveStatus, now, existing.ID); err != nil {
			return nil, false, err
		}
	} else if err != sql.ErrNoRows {
		return nil, false, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := conn.ExecContext(ctx, `UPDATE studio_schedule_handoffs SET status='superseded',updated_at=? WHERE workflow_id=? AND status='ready'`, now, handoff.WorkflowID); err != nil {
		return nil, false, err
	}
	mediaIDs, _ := json.Marshal(handoff.MediaIDs)
	result, err := conn.ExecContext(ctx, `INSERT INTO studio_schedule_handoffs
		(workflow_id,content_id,script_id,revision,media_mode,media_ids_json,cover_media_id,native_video_cover,canonical_hash,planned_at_utc,timezone,original_local_time,intent_hash,status,frozen_plan_id,snapshot_hash,created_at,updated_at)
		VALUES(?,?,NULLIF(?,0),?,?,?,NULLIF(?,0),?,?,?,?,?,?, 'ready',NULL,'',?,?)`,
		handoff.WorkflowID, handoff.ContentID, handoff.ScriptID, handoff.Revision, handoff.MediaMode, string(mediaIDs), handoff.CoverMediaID, boolInt(handoff.NativeVideoCover),
		handoff.CanonicalHash, handoff.PlannedAtUTC, handoff.Timezone, handoff.OriginalLocalTime, handoff.IntentHash, now, now,
	)
	if err != nil {
		return nil, false, err
	}
	handoff.ID, err = result.LastInsertId()
	if err != nil {
		return nil, false, err
	}
	handoff.CreatedAt, handoff.UpdatedAt = now, now
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, false, err
	}
	committed = true
	return handoff, true, nil
}

func (db *DB) loadStudioScheduleHandoff(ctx context.Context, id int64) (*StudioScheduleHandoff, error) {
	return loadStudioScheduleHandoff(ctx, db, id)
}

func loadStudioScheduleHandoff(ctx context.Context, query studioScheduleQuery, id int64) (*StudioScheduleHandoff, error) {
	handoff, err := scanStudioScheduleHandoff(query.QueryRowContext(ctx, `SELECT `+studioScheduleHandoffColumns+` FROM studio_schedule_handoffs WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("studio schedule handoff %d not found", id)
	}
	return handoff, err
}

// GetStudioScheduleHandoff returns an effective expired or superseded state
// without changing the persisted audit row. Explicit write paths materialize
// that state only when they must transition the handoff.
func (db *DB) GetStudioScheduleHandoff(ctx context.Context, id int64) (*StudioScheduleHandoff, error) {
	if id <= 0 {
		return nil, fmt.Errorf("studio schedule handoff id is required")
	}
	handoff, err := db.loadStudioScheduleHandoff(ctx, id)
	if err != nil {
		return nil, err
	}
	effectiveStatus, err := studioScheduleHandoffEffectiveStatus(ctx, db, handoff, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if effectiveStatus != "" {
		handoff.Status = effectiveStatus
	}
	return handoff, nil
}

func (db *DB) ListStudioScheduleHandoffs(ctx context.Context, limit int) ([]StudioScheduleHandoff, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `SELECT id FROM studio_schedule_handoffs ORDER BY updated_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]StudioScheduleHandoff, 0, len(ids))
	for _, id := range ids {
		handoff, err := db.GetStudioScheduleHandoff(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, *handoff)
	}
	return items, nil
}

func (db *DB) CancelStudioScheduleHandoff(ctx context.Context, id int64) (*StudioScheduleHandoff, error) {
	handoff, err := db.loadStudioScheduleHandoff(ctx, id)
	if err != nil {
		return nil, err
	}
	effectiveStatus, err := studioScheduleHandoffEffectiveStatus(ctx, db, handoff, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if effectiveStatus != "" {
		if _, err := db.ExecContext(ctx, `UPDATE studio_schedule_handoffs SET status=?,updated_at=? WHERE id=? AND status='ready'`, effectiveStatus, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("studio schedule handoff is not ready")
	}
	if handoff.Status != StudioScheduleHandoffReady {
		return nil, fmt.Errorf("studio schedule handoff is not ready")
	}
	result, err := db.ExecContext(ctx, `UPDATE studio_schedule_handoffs SET status='cancelled',updated_at=? WHERE id=? AND status='ready'`, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return nil, fmt.Errorf("studio schedule handoff is not ready")
	}
	return db.loadStudioScheduleHandoff(ctx, id)
}

// ValidateStudioScheduleHandoffTx is called immediately before the frozen
// plan insert. It closes the content-edit race without creating a local timer.
func (db *DB) ValidateStudioScheduleHandoffTx(ctx context.Context, tx *sql.Tx, id int64, now time.Time) (*StudioScheduleHandoff, error) {
	handoff, err := scanStudioScheduleHandoff(tx.QueryRowContext(ctx, `SELECT `+studioScheduleHandoffColumns+` FROM studio_schedule_handoffs WHERE id=?`, id))
	if err == sql.ErrNoRows || handoff.Status != StudioScheduleHandoffReady {
		return nil, fmt.Errorf("%s", egopublish.ScheduleHandoffSuperseded)
	}
	if err != nil {
		return nil, err
	}
	plannedAt, parseErr := time.Parse(time.RFC3339, handoff.PlannedAtUTC)
	content, contentErr := studioScheduleContentFor(tx, ctx, handoff.ContentID)
	media, mediaErr := studioScheduleMediaFor(tx, ctx, handoff.ContentID, handoff.MediaIDs, handoff.CoverMediaID)
	canonical := ""
	if contentErr == nil && mediaErr == nil {
		canonical, _ = studioScheduleHash(content, handoff.MediaIDs, handoff.CoverMediaID, handoff.NativeVideoCover, media)
	}
	if parseErr != nil || !plannedAt.After(now.UTC()) {
		_, _ = tx.ExecContext(ctx, `UPDATE studio_schedule_handoffs SET status='expired',updated_at=? WHERE id=? AND status='ready'`, now.UTC().Format(time.RFC3339Nano), handoff.ID)
		return nil, fmt.Errorf("%s", egopublish.ScheduleHandoffSuperseded)
	}
	if contentErr != nil || mediaErr != nil || content.Revision != handoff.Revision || content.Type != handoff.MediaMode || canonical != handoff.CanonicalHash {
		_, _ = tx.ExecContext(ctx, `UPDATE studio_schedule_handoffs SET status='superseded',updated_at=? WHERE id=? AND status='ready'`, now.UTC().Format(time.RFC3339Nano), handoff.ID)
		return nil, fmt.Errorf("%s", egopublish.ScheduleHandoffSuperseded)
	}
	return handoff, nil
}

func (db *DB) FreezeStudioScheduleHandoffTx(ctx context.Context, tx *sql.Tx, handoffID, planID int64, snapshotHash string, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE studio_schedule_handoffs SET status='frozen',frozen_plan_id=?,snapshot_hash=?,updated_at=?
		WHERE id=? AND status='ready'`, planID, snapshotHash, now.UTC().Format(time.RFC3339Nano), handoffID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return fmt.Errorf("%s", egopublish.ScheduleHandoffSuperseded)
	}
	return nil
}
