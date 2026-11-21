//go:build !legacy_chromium

package cli_test

import (
	"testing"

	"alpha-nexus-egolite/internal/cli"
)

func TestStudioScheduleHandoffCLIAndFreezeHandoffAreThinCommands(t *testing.T) {
	root := cli.NewRootCmd()
	for _, path := range [][]string{
		{"studio", "handoff", "create"}, {"studio", "handoff", "get"}, {"studio", "handoff", "list"}, {"studio", "handoff", "cancel"},
		{"publish", "plan", "freeze-handoff"},
	} {
		if command, _, err := root.Find(path); err != nil || command == nil {
			t.Fatalf("path=%v command=%v err=%v", path, command, err)
		}
	}
	create, _, err := root.Find([]string{"studio", "handoff", "create"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"workflow-id", "content-id", "original-local-time", "timezone"} {
		if create.Flags().Lookup(flag) == nil {
			t.Fatalf("create is missing --%s", flag)
		}
	}
	freeze, _, err := root.Find([]string{"publish", "plan", "freeze-handoff"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"handoff-id", "targets", "concurrency", "compliance-policy"} {
		if freeze.Flags().Lookup(flag) == nil {
			t.Fatalf("freeze-handoff is missing --%s", flag)
		}
	}
	if flag := freeze.Flags().Lookup("concurrency"); flag == nil || flag.DefValue != "1" {
		t.Fatalf("freeze-handoff concurrency flag=%+v", flag)
	}
	for _, forbidden := range []string{"run-at", "execute-at", "cron", "scheduler"} {
		if freeze.Flags().Lookup(forbidden) != nil {
			t.Fatalf("freeze-handoff must not expose local execution flag --%s", forbidden)
		}
	}
}
