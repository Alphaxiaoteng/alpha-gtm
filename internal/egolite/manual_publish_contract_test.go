package egolite_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
	"alpha-nexus-egolite/internal/egopublish"
)

type manualPublishContractRunner struct {
	db       *db.DB
	programs []string
	events   [][]string
	opened   []string
}

func (runner *manualPublishContractRunner) Run(ctx context.Context, program string, result any) error {
	runner.programs = append(runner.programs, program)
	if strings.Contains(program, "const profileResult = await listProfiles()") {
		payload := map[string]any{
			"id":        int64(601),
			"taskId":    "task-manual",
			"name":      "manual-publish-space",
			"ownership": "agent",
		}
		raw, _ := json.Marshal(payload)
		runner.events = append(runner.events, []string{"result:ALPHA_EGO_RESULT:" + string(raw)})
		return json.Unmarshal(raw, result)
	}
	bootstrap := `const events = []
globalThis.listTaskSpaces = async () => [{ id: ` + jsonNumber(601) + `, taskId: "task-manual", name: "manual-publish-space", ownership: "agent" }]
globalThis.useOrCreateTaskSpace = async (id) => { events.push("useOrCreateTaskSpace:" + String(id)); return { id, taskId: "task-manual", ownership: "agent" } }
globalThis.listTabs = async () => []
globalThis.closeTab = async (id) => { events.push("closeTab:" + String(id)) }
globalThis.openOrReuseTab = async (url) => { events.push("openOrReuseTab:" + String(url)); return { id: "tab-manual" } }
globalThis.handOffTaskSpace = async (id) => { events.push("handOffTaskSpace:" + String(id)); return { done: true, ownership: "agentDelegatedToUser" } }
globalThis.claimTaskSpace = async (id) => { events.push("claimTaskSpace:" + String(id)); return { done: true } }
globalThis.completeTaskSpace = async (id, options) => { events.push("completeTaskSpace:" + String(id) + ":" + String(options?.keep)); return { done: true } }
globalThis.setTimeout = (resolve) => resolve()
globalThis.cliLog = (line) => events.push("result:" + String(line))
try {
` + program + `
} catch (error) {
  events.push("error:" + String(error?.message ?? error))
}
process.stdout.write(JSON.stringify(events))`
	output, err := exec.CommandContext(ctx, "node", "--input-type=module", "--eval", bootstrap).CombinedOutput()
	if err != nil {
		return fmt.Errorf("manual handoff fake runner failed: %v\n%s", err, output)
	}
	var events []string
	if err := json.Unmarshal(output, &events); err != nil {
		return fmt.Errorf("decode manual handoff events: %v output=%q", err, output)
	}
	runner.events = append(runner.events, events)
	var payload string
	for _, event := range events {
		if strings.HasPrefix(event, "openOrReuseTab:") {
			runner.opened = append(runner.opened, strings.TrimPrefix(event, "openOrReuseTab:"))
		}
		if strings.HasPrefix(event, "result:ALPHA_EGO_RESULT:") {
			payload = strings.TrimPrefix(event, "result:ALPHA_EGO_RESULT:")
		}
	}
	if payload == "" {
		return fmt.Errorf("manual handoff fake runner emitted no result: %v", events)
	}
	return json.Unmarshal([]byte(payload), result)
}

func TestUnsupportedPublishContractsFailWithoutOpeningComposer(t *testing.T) {
	manualPlatformCount := 0
	expectedContractCount := 0
	contractCount := 0
	for _, manifest := range egopublish.EgoLiteManifests() {
		manifest := manifest
		t.Run(manifest.PlatformCode, func(t *testing.T) {
			manualForPlatform := false
			for _, capability := range manifest.Capabilities {
				if capability.PrepareMode != egopublish.PrepareManualHandoff {
					continue
				}
				manualForPlatform = true
				expectedContractCount++
				contentType := capability.ContentType
				composerURL := capability.ComposerURL
				contractCount++
				t.Run(contentType, func(t *testing.T) {
					resolvedComposerURL, ok := egopublish.ComposerURLFor(manifest.PlatformCode, contentType, "")
					if !ok || resolvedComposerURL != composerURL {
						t.Fatalf("composer URL=(%q,%v), manifest=%q", resolvedComposerURL, ok, composerURL)
					}

					database := openServiceDB(t)
					identity, err := database.CreateEgoIdentity("manual publish " + manifest.PlatformCode + " " + contentType)
					if err != nil {
						t.Fatal(err)
					}
					account, err := database.CreateAccountForIdentity(identity.ID, manifest.PlatformCode, manifest.Name, "contract test")
					if err != nil {
						t.Fatal(err)
					}
					if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
						t.Fatal(err)
					}
					space, err := database.CreateEgoSpaceForIdentity(identity.ID, 601, "task-manual", "manual-publish-space")
					if err != nil {
						t.Fatal(err)
					}

					page := manualPublishContractPage(t, manifest.PlatformCode, contentType)
					runner := &manualPublishContractRunner{}
					batch, err := egolite.NewSpaceService(database, runner).OpenPublishBatchAndHandoff(
						context.Background(), identity.ID, []egolite.PublishPage{page},
					)
					if err != nil {
						t.Fatalf("OpenPublishBatchAndHandoff: %v", err)
					}
					if batch.State != "failed" || batch.SpaceID != space.SpaceID ||
						len(batch.PlatformCodes) != 1 || batch.PlatformCodes[0] != manifest.PlatformCode ||
						len(batch.PreparedPlatformCodes) != 0 || batch.PlatformErrors[manifest.PlatformCode] != "EGO_PUBLISH_AUTOMATION_UNSUPPORTED" {
						t.Fatalf("batch=%+v, want unsupported failure without prepared platforms", batch)
					}
					if len(runner.programs) != 3 || len(runner.events) != 3 {
						t.Fatalf("runner programs=%d events=%d, want binding check, prepare result, and completion", len(runner.programs), len(runner.events))
					}
					program := runner.programs[len(runner.programs)-2]
					for _, required := range []string{
						"const preparationResults = []",
						"await useOrCreateTaskSpace(",
						"EGO_PUBLISH_AUTOMATION_UNSUPPORTED",
					} {
						if !strings.Contains(program, required) {
							t.Errorf("manual program missing %q:\n%s", required, program)
						}
					}
					for _, forbidden := range []string{"const submitResults = []", "submit approved publish"} {
						if strings.Contains(program, forbidden) {
							t.Errorf("manual program contains forbidden operation %q:\n%s", forbidden, program)
						}
					}
					if !strings.Contains(program, `if (existingSpace.ownership === "user")`) ||
						!strings.Contains(program, `throw new Error("EGO_SPACE_USER_CONTROLLED")`) {
						t.Errorf("manual program lost its user-controlled fail-closed guard:\n%s", program)
					}
					if len(runner.opened) != 0 {
						t.Fatalf("opened URLs=%v, want none for unsupported publish", runner.opened)
					}
					if got := strings.Join(runner.events[len(runner.events)-2], ","); !strings.Contains(got, fmt.Sprintf("useOrCreateTaskSpace:%d", space.SpaceID)) || strings.Contains(got, "handOffTaskSpace:%d") || !strings.Contains(got, "EGO_PUBLISH_AUTOMATION_UNSUPPORTED") {
						t.Fatalf("unsupported events=%s", got)
					}
					if cleanup := strings.Join(runner.events[len(runner.events)-1], ","); !strings.Contains(cleanup, fmt.Sprintf("completeTaskSpace:%d:true", space.SpaceID)) || strings.Contains(cleanup, "handOffTaskSpace") {
						t.Fatalf("unsupported publish cleanup=%s", cleanup)
					}

					persisted, err := database.GetEgoSpaceByIdentity(identity.ID)
					if err != nil {
						t.Fatal(err)
					}
					if persisted.SpaceID != space.SpaceID || persisted.Ownership != "agent" || persisted.State != "ready" {
						t.Fatalf("persisted Space=%+v, want agent/ready retained Space", persisted)
					}
					var jobs, records int
					if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs`).Scan(&jobs); err != nil {
						t.Fatal(err)
					}
					if err := database.QueryRow(`SELECT COUNT(*) FROM publish_records`).Scan(&records); err != nil {
						t.Fatal(err)
					}
					if jobs != 0 || records != 0 {
						t.Fatalf("manual handoff created persistent publish state: jobs=%d records=%d", jobs, records)
					}
				})
			}
			if manualForPlatform {
				manualPlatformCount++
			}
		})
	}
	if manualPlatformCount != 13 {
		t.Fatalf("manual manifests=%d, want 13", manualPlatformCount)
	}
	if expectedContractCount != 26 {
		t.Fatalf("manual manifest content contracts=%d, want 26", expectedContractCount)
	}
	if contractCount != expectedContractCount {
		t.Fatalf("executed manual content contracts=%d, want dynamically derived %d", contractCount, expectedContractCount)
	}
}

func manualPublishContractPage(t *testing.T, platformCode, contentType string) egolite.PublishPage {
	t.Helper()
	page := egolite.PublishPage{
		PlatformCode: platformCode,
		ContentType:  contentType,
		SettingsJSON: "{}",
		Title:        "合同测试标题",
		Body:         "合同测试正文",
		TagsJSON:     "[]",
	}
	var kind, role, filename string
	switch contentType {
	case "graph_text":
		kind, role, filename = "image", "image", "manual-graph-text.bin"
	case "video":
		kind, role, filename = "video", "video", "manual-video.bin"
	default:
		return page
	}
	path := filepath.Join(t.TempDir(), filename)
	contents := []byte("synthetic " + contentType + " media")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	page.Media = []egolite.PublishMedia{{
		FilePath: path,
		SHA256:   hex.EncodeToString(digest[:]),
		Kind:     kind,
		Role:     role,
	}}
	return page
}
