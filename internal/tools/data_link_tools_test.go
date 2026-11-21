package tools

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"alpha-nexus-egolite/internal/datalink"
	"alpha-nexus-egolite/internal/db"
	_ "modernc.org/sqlite"
)

func TestDataLinkRegistryFlow(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(t.TempDir(), "registry.sqlite")
	fixture, err := sql.Open("sqlite", fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(`CREATE TABLE sales(day TEXT, sku TEXT, gmv REAL); INSERT INTO sales VALUES ('2026-08-01','a',10.5)`); err != nil {
		t.Fatal(err)
	}
	_ = fixture.Close()
	file, err := os.Open(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	source, err := datalink.NewService(database).RegisterSQLite(context.Background(), "registry.sqlite", file)
	_ = file.Close()
	_ = database.Close()
	if err != nil {
		t.Fatal(err)
	}

	runtime := &Runtime{DataDir: dataDir}
	listed, err := Default.Call(context.Background(), runtime, "data.sql.list", map[string]interface{}{})
	if err != nil || !listed.OK {
		t.Fatalf("list=%+v err=%v", listed, err)
	}
	inspected, err := Default.Call(context.Background(), runtime, "data.sql.inspect", map[string]interface{}{"source_id": source.ID, "object_name": "sales"})
	if err != nil || !inspected.OK {
		t.Fatalf("inspect=%+v err=%v", inspected, err)
	}
	collected, err := Default.Call(context.Background(), runtime, "data.sql.collect", map[string]interface{}{
		"source_id": source.ID, "object_name": "sales", "mapping": map[string]interface{}{"date": "day", "sku": "sku", "gmv": "gmv"},
	})
	if err != nil || !collected.OK {
		t.Fatalf("collect=%+v err=%v", collected, err)
	}
	if _, ok := collected.Data["snapshot"]; !ok {
		t.Fatalf("snapshot missing: %+v", collected)
	}
	unsafe, _ := Default.Call(context.Background(), runtime, "data.sql.inspect", map[string]interface{}{"source_id": source.ID, "query": "DROP TABLE sales"})
	if unsafe.OK || unsafe.Error != "DATA_SOURCE_SQL_UNSAFE" {
		t.Fatalf("unsafe=%+v", unsafe)
	}
}

func TestDataLinkToolsAreNotAgentVisible(t *testing.T) {
	for _, name := range []string{"data.sql.list", "data.sql.inspect", "data.sql.preflight", "data.sql.collect", "data.sql.review.confirm"} {
		definition, ok := Default.Get(name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if definition.AgentVisible {
			t.Fatalf("%s exposed to agent", name)
		}
	}
}
