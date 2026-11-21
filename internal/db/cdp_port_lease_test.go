package db_test

import (
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

func TestReserveCDPPortIsAtomicAcrossConnections(t *testing.T) {
	dir := t.TempDir()
	first, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.Migrate(); err != nil {
		t.Fatal(err)
	}
	second, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	now := time.Now().UTC()
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []struct {
		store   *db.DB
		profile string
		owner   string
	}{
		{first, "profile-a", "fleet-a"},
		{second, "profile-b", "fleet-b"},
	} {
		wg.Add(1)
		go func(candidate struct {
			store   *db.DB
			profile string
			owner   string
		}) {
			defer wg.Done()
			reserved, reserveErr := candidate.store.ReserveCDPPort(candidate.profile, candidate.owner, 15155, now, time.Minute)
			results <- reserved
			errs <- reserveErr
		}(candidate)
	}
	wg.Wait()
	close(results)
	close(errs)

	reservedCount := 0
	for reserveErr := range errs {
		if reserveErr != nil {
			t.Fatal(reserveErr)
		}
	}
	for reserved := range results {
		if reserved {
			reservedCount++
		}
	}
	if reservedCount != 1 {
		t.Fatalf("reserved count = %d, want 1", reservedCount)
	}
}

func TestReserveCDPPortRejectsRuntimeAndReclaimsExpiredLease(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := d.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, pid, cdp_port, cdp_url, state, started_at, last_seen_at, updated_at)
		VALUES ('running', 123, 15156, 'http://127.0.0.1:15156', 'ready', ?, ?, ?)`, now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	reserved, err := d.ReserveCDPPort("other", "fleet-other", 15156, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if reserved {
		t.Fatal("runtime-owned port was reserved")
	}

	if reserved, err = d.ReserveCDPPort("expired", "fleet-expired", 15157, now, time.Second); err != nil || !reserved {
		t.Fatalf("initial reservation = %v, %v", reserved, err)
	}
	reserved, err = d.ReserveCDPPort("replacement", "fleet-replacement", 15157, now.Add(2*time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !reserved {
		t.Fatal("expired reservation was not reclaimed")
	}
}
