package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/cli"
)

func TestRootExposesOnlyEgoBrowserLifecycle(t *testing.T) {
	rootOutput, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootOutput, "ego") {
		t.Fatalf("help does not expose ego command: %s", rootOutput)
	}
	for _, legacyFlag := range []string{"--browser ", "--browser-engine", "--cdp-port", "--egolite"} {
		if strings.Contains(rootOutput, legacyFlag) {
			t.Fatalf("help exposes legacy browser flag %q: %s", legacyFlag, rootOutput)
		}
	}

	root := cli.NewRootCmd()
	for _, command := range root.Commands() {
		switch command.Name() {
		case "browser", "meta", "ops", "xhs":
			t.Fatalf("legacy browser-backed command %q must not be registered", command.Name())
		}
	}
	agent, _, err := root.Find([]string{"agent"})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range agent.Commands() {
		if command.Name() == "browser" {
			t.Fatal("legacy CDP BrowserAgent command must not be registered")
		}
	}
	scheme, _, err := root.Find([]string{"agent", "scheme"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"account-id", "cdp-url"} {
		if scheme.Flags().Lookup(flag) != nil {
			t.Fatalf("agent scheme exposes legacy browser flag %q", flag)
		}
	}
	if strings.Contains(strings.ToLower(scheme.Long+" "+scheme.Short), "browser") {
		t.Fatalf("agent scheme still advertises legacy browser runtime: %s", scheme.Long)
	}
}

func TestEgoDoctorUsesConfiguredCLI(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "ego-browser-test")
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' 'ALPHA_EGO_RESULT:{\"available\":true,\"missing\":[]}'\n"
	if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	output, err := executeCommand(t, "--data-dir", filepath.Join(dir, "data"), "--ego-browser-command", command, "ego", "doctor")
	if err != nil {
		t.Fatalf("doctor error=%v output=%s", err, output)
	}
	if !strings.Contains(output, `"available":true`) {
		t.Fatalf("doctor output=%s", output)
	}
}

func TestEgoVerifyRequiresIdentityAndPlatformSelection(t *testing.T) {
	root := cli.NewRootCmd()
	verify, _, err := root.Find([]string{"ego", "verify"})
	if err != nil {
		t.Fatal(err)
	}
	if verify.Flags().Lookup("identity-id") == nil || verify.Flags().Lookup("platform") == nil {
		t.Fatalf("ego verify flags=%v", verify.Flags())
	}
}

func TestAccountLoginHandoffAndCheckUseEgoSpace(t *testing.T) {
	dir := t.TempDir()
	command := filepath.Join(dir, "ego-browser-test")
	script := `#!/bin/sh
program=$(cat)
case "$program" in
  *handOffTaskSpace*) printf '%s\n' 'ALPHA_EGO_RESULT:{"done":true,"ownership":"agentDelegatedToUser"}' >&2 ;;
  *snapshotText*) printf '%s\n' 'ALPHA_EGO_RESULT:{"available":true,"length":42}' >&2 ;;
  *) printf '%s\n' 'ALPHA_EGO_RESULT:{"id":701,"taskId":"task-701","name":"identity-1/account-1/xhs","ownership":"agent"}' >&2 ;;
esac
`
	if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, "data")
	if _, err := executeCommand(t, "init", "--data-dir", dataDir); err != nil {
		t.Fatal(err)
	}
	if _, err := executeCommand(t, "account", "add", "--platform", "xhs", "--name", "主号", "--data-dir", dataDir); err != nil {
		t.Fatal(err)
	}

	handoff, err := executeCommand(t, "--data-dir", dataDir, "--ego-browser-command", command, "account", "login-handoff", "--id", "1")
	if err != nil || !strings.Contains(handoff, "Space:     701") || strings.Contains(strings.ToLower(handoff), "cdp") {
		t.Fatalf("handoff output=%s err=%v", handoff, err)
	}
	check, err := executeCommand(t, "--data-dir", dataDir, "--ego-browser-command", command, "account", "check", "--id", "1")
	if err != nil || !strings.Contains(check, "EGO_SNAPSHOT_UNVERIFIED") || !strings.Contains(check, "Verified: false") {
		t.Fatalf("check output=%s err=%v", check, err)
	}
}

func TestPublishCLIExposesOnlyApprovedEgoWorkflows(t *testing.T) {
	root := cli.NewRootCmd()
	publish, _, err := root.Find([]string{"publish"})
	if err != nil {
		t.Fatal(err)
	}
	children := publish.Commands()
	if len(children) != 3 || children[0].Name() != "batch" || children[1].Name() != "listing" || children[2].Name() != "plan" {
		t.Fatalf("publish children=%v", children)
	}
	plan, _, err := root.Find([]string{"publish", "plan"})
	if err != nil || len(plan.Commands()) != 1 || plan.Commands()[0].Name() != "freeze-handoff" {
		t.Fatalf("publish plan children=%v err=%v", plan.Commands(), err)
	}
	listing, _, err := root.Find([]string{"publish", "listing"})
	if err != nil {
		t.Fatal(err)
	}
	listingChildren := listing.Commands()
	if len(listingChildren) != 1 || listingChildren[0].Name() != "prepare" {
		t.Fatalf("publish listing children=%v", listingChildren)
	}
	for _, flag := range []string{"platform", "target", "auto-submit", "cdp-url"} {
		if publish.Flags().Lookup(flag) != nil {
			t.Fatalf("legacy direct publish flag %q remains", flag)
		}
	}
}

func TestLegacyBatchBrowserActionsAreUnavailable(t *testing.T) {
	root := cli.NewRootCmd()
	batch, _, err := root.Find([]string{"batch"})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range batch.Commands() {
		switch command.Name() {
		case "account-check", "discover", "publish", "resume", "retry":
			t.Fatalf("legacy batch action %q remains available", command.Name())
		}
	}
}
