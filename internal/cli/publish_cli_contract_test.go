//go:build !legacy_chromium

package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/cli"
	"alpha-nexus-egolite/internal/db"
)

type publishCLIToolResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Data  struct {
		Plan struct {
			ID          int64 `json:"id"`
			Concurrency int   `json:"concurrency"`
			Snapshot    struct {
				Concurrency int `json:"concurrency"`
				Targets     []struct {
					PlatformCode string `json:"platform_code"`
				} `json:"targets"`
			} `json:"snapshot"`
		} `json:"plan"`
	} `json:"data"`
}

type shopCLIResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Data  struct {
		Count int `json:"count"`
	} `json:"data"`
}

func TestShopListingPrepareCLIContract(t *testing.T) {
	root := cli.NewRootCmd()
	prepare, _, err := root.Find([]string{"publish", "listing", "prepare"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"plan-id", "snapshot-hash", "approval-id", "nonce"} {
		if prepare.Flags().Lookup(flag) == nil {
			t.Fatalf("shop listing prepare is missing --%s", flag)
		}
	}
	if prepare.Flags().Lookup("action") != nil {
		t.Fatal("shop listing prepare must not expose a generic action flag")
	}

	for _, helpArgs := range [][]string{
		{"publish", "listing", "--help"},
		{"publish", "listing", "prepare", "--help"},
	} {
		help, err := executeCommand(t, helpArgs...)
		if err != nil {
			t.Fatalf("help args=%v err=%v", helpArgs, err)
		}
		for _, forbidden := range []string{"submit", "verify"} {
			if strings.Contains(strings.ToLower(help), forbidden) {
				t.Fatalf("shop help args=%v exposes %q: %s", helpArgs, forbidden, help)
			}
		}
	}
}

func TestShopListingPrepareRejectsMissingApprovalArgumentsBeforeServer(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data-file")
	if err := os.WriteFile(dataDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, missing := range []string{"plan-id", "snapshot-hash", "approval-id", "nonce"} {
		t.Run(missing, func(t *testing.T) {
			args := []string{
				"--data-dir", dataDir,
				"publish", "listing", "prepare",
			}
			if missing != "plan-id" {
				args = append(args, "--plan-id", "1")
			}
			if missing != "snapshot-hash" {
				args = append(args, "--snapshot-hash", strings.Repeat("a", 64))
			}
			if missing != "approval-id" {
				args = append(args, "--approval-id", "approval")
			}
			if missing != "nonce" {
				args = append(args, "--nonce", "nonce")
			}

			output, err := executeCommand(t, args...)
			if err == nil || err.Error() != "plan-id, snapshot-hash, approval-id and nonce are required" {
				t.Fatalf("missing=%s err=%v output=%s; expected validation before NewServer", missing, err, output)
			}
		})
	}
}

func TestContentCreateTagsCanonicalizesForPublishCLI(t *testing.T) {
	dir := initTestDir(t)

	if _, err := executeCommand(t,
		"--data-dir", dir,
		"content", "create",
		"--type", "article",
		"--title", "CLI tags contract",
		"--body", "An article body for the publish contract.",
		"--tags", " first, second, , third ",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := executeCommand(t,
		"--data-dir", dir,
		"content", "create",
		"--type", "article",
		"--title", "CLI empty tags contract",
		"--body", "An article body without tags.",
	); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	content, err := database.GetContent(1)
	if err != nil {
		t.Fatal(err)
	}
	if content.TagsJSON != `["first","second","third"]` {
		t.Fatalf("content tags_json=%q, want canonical JSON string array", content.TagsJSON)
	}
	emptyTagsContent, err := database.GetContent(2)
	if err != nil {
		t.Fatal(err)
	}
	if emptyTagsContent.TagsJSON != `[]` {
		t.Fatalf("empty content tags_json=%q, want []", emptyTagsContent.TagsJSON)
	}
}

func TestPublishAndShopCLIContract(t *testing.T) {
	dir := initTestDir(t)

	if _, err := executeCommand(t,
		"--data-dir", dir,
		"account", "add",
		"--platform", "zhihu",
		"--name", "知乎人工接管账号",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := executeCommand(t,
		"--data-dir", dir,
		"content", "create",
		"--type", "article",
		"--title", "CLI publish contract",
		"--body", "An article body for the publish contract.",
		"--tags", " alpha, beta, , gamma ",
	); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	content, err := database.GetContent(1)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(1, db.AccountAuthLoggedIn); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	freezeArgs := `{"content_id":1,"media_mode":"article","media_ids":[],"targets":[{"account_id":1}]}`
	freezeOutput, err := executeCommand(t,
		"--data-dir", dir,
		"tools", "call", "publish.plan.freeze",
		"--args", freezeArgs,
	)
	if err != nil {
		t.Fatalf("freeze err=%v output=%s", err, freezeOutput)
	}
	var freeze publishCLIToolResult
	if err := json.Unmarshal([]byte(freezeOutput), &freeze); err != nil {
		t.Fatalf("decode freeze output=%v output=%s", err, freezeOutput)
	}
	if !freeze.OK || freeze.Error != "" {
		t.Fatalf("freeze result=%+v output=%s", freeze, freezeOutput)
	}
	if freeze.Data.Plan.ID <= 0 || freeze.Data.Plan.Concurrency != 1 || freeze.Data.Plan.Snapshot.Concurrency != 1 {
		t.Fatalf("freeze plan=%+v, want persisted concurrency=1", freeze.Data.Plan)
	}
	if len(freeze.Data.Plan.Snapshot.Targets) != 1 || freeze.Data.Plan.Snapshot.Targets[0].PlatformCode != "zhihu" {
		t.Fatalf("freeze target=%+v, want zhihu", freeze.Data.Plan.Snapshot.Targets)
	}

	getArgs := `{"plan_id":1}`
	getOutput, err := executeCommand(t,
		"--data-dir", dir,
		"tools", "call", "publish.plan.get",
		"--args", getArgs,
	)
	if err != nil {
		t.Fatalf("get err=%v output=%s", err, getOutput)
	}
	var recovered publishCLIToolResult
	if err := json.Unmarshal([]byte(getOutput), &recovered); err != nil {
		t.Fatalf("decode get output=%v output=%s", err, getOutput)
	}
	if !recovered.OK || recovered.Data.Plan.ID != freeze.Data.Plan.ID || recovered.Data.Plan.Concurrency != 1 {
		t.Fatalf("recovered plan=%+v, freeze plan=%+v output=%s", recovered.Data.Plan, freeze.Data.Plan, getOutput)
	}

	shopOutput, err := executeCommand(t,
		"--data-dir", dir,
		"tools", "call", "shop.listing.list",
		"--args", `{"input":{}}`,
	)
	if err != nil {
		t.Fatalf("shop list err=%v output=%s", err, shopOutput)
	}
	var shop shopCLIResult
	if err := json.Unmarshal([]byte(shopOutput), &shop); err != nil {
		t.Fatalf("decode shop output=%v output=%s", err, shopOutput)
	}
	if !shop.OK || shop.Error != "" || shop.Data.Count != 0 {
		t.Fatalf("shop empty result=%+v output=%s", shop, shopOutput)
	}

	if content.TagsJSON != `["alpha","beta","gamma"]` {
		t.Fatalf("content tags_json=%q, want canonical JSON string array", content.TagsJSON)
	}
	if strings.Contains(freezeOutput, "raw contract") {
		t.Fatalf("freeze exposed an internal raw-tag error: %s", freezeOutput)
	}
}
