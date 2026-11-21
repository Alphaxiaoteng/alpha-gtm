package datalink

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appdb "alpha-nexus-egolite/internal/db"
	_ "modernc.org/sqlite"
)

func fixtureService(t *testing.T) (*Service, int64) {
	t.Helper()
	dataDir := t.TempDir()
	database, err := appdb.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}

	fixturePath := filepath.Join(t.TempDir(), "commerce.sqlite")
	fixture, err := sql.Open("sqlite", fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(`CREATE TABLE orders (
		day TEXT, shop TEXT, sku TEXT, impressions INTEGER, clicks INTEGER,
		orders INTEGER, gmv REAL, note TEXT
	);
	INSERT INTO orders VALUES
		('2026-08-01','A','sku-1',100,10,2,59.8,'first'),
		('2026-08-02','A','sku-1',120,12,3,89.7,'second'),
		(NULL,'A','sku-1',-1,12,3,89.7,'bad');
	CREATE VIEW order_summary AS SELECT day, SUM(gmv) AS gmv FROM orders GROUP BY day;`); err != nil {
		_ = fixture.Close()
		t.Fatal(err)
	}
	if err := fixture.Close(); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	service := NewService(database)
	source, err := service.RegisterSQLite(context.Background(), "commerce.sqlite", input)
	if err != nil {
		t.Fatal(err)
	}
	return service, source.ID
}

func TestValidateReadOnlySQL(t *testing.T) {
	for _, query := range []string{
		"SELECT * FROM orders",
		"WITH recent AS (SELECT * FROM orders) SELECT * FROM recent",
		"-- report\nSELECT day FROM orders",
	} {
		if err := ValidateReadOnlySQL(query); err != nil {
			t.Errorf("query rejected %q: %v", query, err)
		}
	}
	for _, query := range []string{
		"UPDATE orders SET gmv=0",
		"PRAGMA table_info(orders)",
		"SELECT 1; DELETE FROM orders",
		"WITH changed AS (DELETE FROM orders RETURNING *) SELECT * FROM changed",
		"ATTACH DATABASE 'other.db' AS other",
	} {
		if err := ValidateReadOnlySQL(query); err == nil {
			t.Errorf("unsafe query accepted %q", query)
		}
	}
}

func TestSQLiteInspectAndCollectRealSnapshot(t *testing.T) {
	service, sourceID := fixtureService(t)
	ctx := context.Background()
	inspection, err := service.Inspect(ctx, sourceID, InspectRequest{ObjectName: "orders", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Objects) != 2 || len(inspection.Preview) != 2 {
		t.Fatalf("objects=%d preview=%d", len(inspection.Objects), len(inspection.Preview))
	}
	if inspection.SchemaFingerprint == "" || inspection.Preview[0]["note"] != "first" {
		t.Fatalf("inspection=%+v", inspection)
	}

	preflight, err := service.Preflight(ctx, sourceID, CollectRequest{
		ObjectName: "orders",
		Mapping:    map[string]string{"date": "day", "shop": "shop", "sku": "sku", "impressions": "impressions", "orders": "orders", "gmv": "gmv"},
		MaxRows:    100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preflight.EstimatedRows != 3 || preflight.SchemaFingerprint == "" {
		t.Fatalf("preflight=%+v", preflight)
	}

	snapshot, err := service.Collect(ctx, sourceID, CollectRequest{
		ObjectName: "orders",
		Mapping:    map[string]string{"date": "day", "shop": "shop", "sku": "sku", "impressions": "impressions", "orders": "orders", "gmv": "gmv"},
		MaxRows:    100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "completed" || snapshot.RowCount != 3 || snapshot.Checksum == "" || snapshot.DataPath == "" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if len(snapshot.Quality.Issues) == 0 || snapshot.Quality.Missing["date"] != 1 || snapshot.Quality.Negative["impressions"] != 1 {
		t.Fatalf("quality=%+v", snapshot.Quality)
	}
	body, err := os.ReadFile(filepath.Join(service.DataDir(), filepath.FromSlash(snapshot.DataPath)))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"raw"`) || !strings.Contains(text, `"note":"first"`) || !strings.Contains(text, `"mapped"`) {
		t.Fatalf("snapshot data lost raw fields: %s", text)
	}
}

func TestCollectionLimitsAndKnowledgeConfirmation(t *testing.T) {
	service, sourceID := fixtureService(t)
	ctx := context.Background()
	if _, err := service.Collect(ctx, sourceID, CollectRequest{Query: "DELETE FROM orders", MaxRows: 10}); err == nil {
		t.Fatal("write query was accepted")
	}
	snapshot, err := service.Collect(ctx, sourceID, CollectRequest{ObjectName: "orders", MaxRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RowCount != 2 || !snapshot.Truncated {
		t.Fatalf("row limit not enforced: %+v", snapshot)
	}
	if _, err := service.Collect(ctx, sourceID, CollectRequest{ObjectName: "orders", MaxRows: 10, MaxBytes: 10}); err == nil || err.Error() != "DATA_SNAPSHOT_BYTE_LIMIT" {
		t.Fatalf("byte limit error=%v", err)
	}
	base, err := service.database.CreateKnowledgeBase("SQL 复盘", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmReview(ctx, snapshot.ID, base.ID, false); err == nil {
		t.Fatal("review was imported without confirmation")
	}
	confirmed, err := service.ConfirmReview(ctx, snapshot.ID, base.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.KnowledgeDocumentID <= 0 || confirmed.ConfirmedAt == "" {
		t.Fatalf("confirmed=%+v", confirmed)
	}
	var content string
	err = service.database.QueryRow(`SELECT group_concat(content,'\n') FROM knowledge_chunks WHERE document_id=?`, confirmed.KnowledgeDocumentID).Scan(&content)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if !strings.Contains(content, snapshot.Checksum) {
		t.Fatalf("knowledge review missing evidence: %s", content)
	}
	var sourceName string
	if err := service.database.QueryRow(`SELECT source_name FROM knowledge_documents WHERE id=?`, confirmed.KnowledgeDocumentID).Scan(&sourceName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sourceName, "sql-link/snapshot-") {
		t.Fatalf("source citation lost: %s", sourceName)
	}
}

func TestQualityChecksMissingDuplicateTypeDateAndMetricAnomaly(t *testing.T) {
	analyzer := newQualityAnalyzer(map[string]string{"date": "day", "gmv": "gmv"})
	for index := 0; index < 101; index++ {
		gmv := interface{}(1.0)
		if index == 100 {
			gmv = float64(1_000_000_000)
		}
		analyzer.Add(map[string]interface{}{"day": "2026-08-01", "gmv": gmv, "unknown": "kept"}, map[string]interface{}{"date": "2026-08-01", "gmv": gmv})
	}
	analyzer.Add(map[string]interface{}{"day": "bad-date", "gmv": "oops"}, map[string]interface{}{"date": "bad-date", "gmv": "oops"})
	analyzer.Add(map[string]interface{}{"day": nil, "gmv": -2}, map[string]interface{}{"date": nil, "gmv": -2})
	report := analyzer.Report()
	if report.Duplicates != 99 || report.Missing["date"] != 1 || report.TypeMismatch["date"] != 1 || report.TypeMismatch["gmv"] != 1 || report.Negative["gmv"] != 1 {
		t.Fatalf("quality=%+v", report)
	}
	if report.DateMin != "2026-08-01" || report.DateMax != "2026-08-01" || !strings.Contains(strings.Join(report.Issues, "|"), "gmv 存在极端异常值") {
		t.Fatalf("quality issues=%+v", report)
	}
}
