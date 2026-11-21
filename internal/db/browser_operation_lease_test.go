package db_test

import (
	"testing"
	"time"
)

func TestBrowserOperationLeaseIsExclusiveAndOwnerBound(t *testing.T) {
	d, cleanup := setupDB(t)
	defer cleanup()
	if err := d.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	acquired, err := d.AcquireBrowserOperationLease("acct-7", "observe", "fleet-run-1", now, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("first acquire = %v, %v", acquired, err)
	}
	acquired, err = d.AcquireBrowserOperationLease("acct-7", "start", "fleet-run-2", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("second owner acquired active operation lease")
	}
	if err := d.ReleaseBrowserOperationLease("acct-7", "fleet-run-2"); err != nil {
		t.Fatal(err)
	}
	acquired, err = d.AcquireBrowserOperationLease("acct-7", "start", "fleet-run-2", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("wrong owner release removed operation lease")
	}
	if err := d.ReleaseBrowserOperationLease("acct-7", "fleet-run-1"); err != nil {
		t.Fatal(err)
	}
	acquired, err = d.AcquireBrowserOperationLease("acct-7", "start", "fleet-run-2", now, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("acquire after release = %v, %v", acquired, err)
	}
}
