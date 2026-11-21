package egolite

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func runPublishChallengeProgram(t *testing.T, program, ownership string) (publishRuntimeResult, error) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the generated EgoLite program")
	}
	ownershipJSON, err := json.Marshal(ownership)
	if err != nil {
		t.Fatal(err)
	}
	userOwnedJSON, err := json.Marshal(ownership == "user")
	if err != nil {
		t.Fatal(err)
	}
	requiresTakeoverJSON, err := json.Marshal(ownership == "agentDelegatedToUser")
	if err != nil {
		t.Fatal(err)
	}
	prelude := `
const listTaskSpaces = async () => [{ id: 71, ownership: ` + string(ownershipJSON) + ` }]
const takeOverTaskSpace = async () => {
  if (` + string(userOwnedJSON) + `) throw new Error("EGO_TEST_UNEXPECTED_TAKEOVER")
	globalThis.alphaPublishPrepareTookOverSpace = true
  return { done: true }
}
const claimTaskSpace = async () => ({ done: true })
const useOrCreateTaskSpace = async () => ({ done: true })
const completeTaskSpace = async () => ({ done: true })
const handOffTaskSpace = async () => ({ done: true })
const openOrReuseTab = async () => ({})
const pageInfo = async () => ({ url: "https://creator.douyin.com/creator-micro/content/publish" })
const listTabs = async () => [{ id: "tab-1", url: "https://creator.douyin.com/creator-micro/content/publish" }]
const switchTab = async () => ({})
const js = async (source) => String(source).includes("security verification")
const uploadFile = async () => ({})
const fillInput = async () => ({})
const pressKey = async () => ({})
const click = async () => ({})
const snapshotText = async () => ""
const cliLog = (value) => console.log(value)
`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(prelude + "\n" + program + `
if (` + string(requiresTakeoverJSON) + ` && !globalThis.alphaPublishPrepareTookOverSpace) {
  throw new Error("EGO_TEST_DELEGATED_TAKEOVER_MISSING")
}
`)
	output, err := command.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "EGO_TEST_UNEXPECTED_TAKEOVER") {
			return publishRuntimeResult{}, errors.New("EGO_TEST_UNEXPECTED_TAKEOVER")
		}
		if strings.Contains(string(output), "EGO_TEST_DELEGATED_TAKEOVER_MISSING") {
			return publishRuntimeResult{}, errors.New("EGO_TEST_DELEGATED_TAKEOVER_MISSING")
		}
		if strings.Contains(string(output), "EGO_SPACE_USER_CONTROLLED") {
			return publishRuntimeResult{}, errors.New("EGO_SPACE_USER_CONTROLLED")
		}
		t.Fatalf("generated EgoLite program failed: %v\n%s", err, output)
	}
	var result publishRuntimeResult
	found := false
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.HasPrefix(line, resultPrefix) {
			continue
		}
		found = true
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, resultPrefix)), &result); err != nil {
			t.Fatalf("decode runtime result: %v; output=%s", err, output)
		}
	}
	if !found {
		t.Fatalf("generated EgoLite program emitted no result: %s", output)
	}
	return result, nil
}

func runPublishPrepareCompletionProgram(t *testing.T, completionBody string) (publishRuntimeResult, []string, error) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the generated EgoLite program")
	}
	program := buildEgoPublishCompletionProgram(71)
	prelude := `
const events = []
const listTaskSpaces = async () => [{ id: 71, ownership: "agent" }]
const claimTaskSpace = async (id) => { events.push("claimTaskSpace:" + String(id)); return { done: true } }
const completeTaskSpace = async (id, options) => { events.push("completeTaskSpace:" + String(id) + ":" + String(options?.keep)); ` + completionBody + ` }
const cliLog = (value) => events.push("result:" + String(value))
` + program + `
process.stdout.write(JSON.stringify(events))
`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(prelude)
	output, err := command.CombinedOutput()
	if err != nil {
		return publishRuntimeResult{}, nil, err
	}
	var events []string
	if err := json.Unmarshal(output, &events); err != nil {
		return publishRuntimeResult{}, nil, err
	}
	var result publishRuntimeResult
	for _, event := range events {
		if strings.HasPrefix(event, "result:"+resultPrefix) {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(event, "result:"+resultPrefix)), &result); err != nil {
				return publishRuntimeResult{}, events, err
			}
		}
	}
	return result, events, nil
}

func TestPublishProgramsMapPlatformChallengeToLoginRequiredFailure(t *testing.T) {
	targets := []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/publish",
		Title:       "冻结标题", Body: "冻结正文",
	}}
	for _, test := range []struct {
		name                string
		program             string
		wantClickedReported bool
	}{
		{name: "prepare", program: buildEgoPublishPrepareProgram(71, targets)},
		{name: "submit", program: buildEgoPublishSubmitProgram(71, targets), wantClickedReported: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := runPublishChallengeProgram(t, test.program, "agent")
			if err != nil {
				t.Fatal(err)
			}
			clickedReported := result.Results[0].Clicked != nil
			if !result.Done || len(result.Results) != 1 || result.Results[0].Status != "failed" ||
				result.Results[0].ErrorCode != "LOGIN_REQUIRED" || clickedReported != test.wantClickedReported ||
				(result.Results[0].Clicked != nil && *result.Results[0].Clicked) {
				t.Fatalf("challenge result=%+v", result)
			}
		})
	}
}

func TestPublishChallengeDetectionRequiresVisibleChallengeSurface(t *testing.T) {
	targets := []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/publish",
		Title:       "冻结标题", Body: "冻结正文",
	}}
	for _, program := range []string{
		buildEgoPublishPrepareProgram(71, targets),
		buildEgoPublishSubmitProgram(71, targets),
	} {
		if strings.Contains(program, "(document.body?.innerText||'').slice(0,5000);return /captcha") {
			t.Fatal("background page text must not be treated as a platform challenge")
		}
		for _, required := range []string{
			"alphaPlatformChallengeVisible", "[role='dialog']", "[aria-modal='true']",
			"iframe[src*='captcha']", "iframe[src*='verify']",
		} {
			if !strings.Contains(program, required) {
				t.Fatalf("visible challenge contract missing %q", required)
			}
		}
	}
}

func TestPublishPrepareStopsForUserControlledSpace(t *testing.T) {
	targets := []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/publish",
		Title:       "冻结标题", Body: "冻结正文",
	}}
	program := buildEgoPublishPrepareProgram(71, targets)
	_, err := runPublishChallengeProgram(t, program, "user")
	if err == nil || err.Error() != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("error=%v, want EGO_SPACE_USER_CONTROLLED without a takeover", err)
	}
}

func TestPublishPrepareReclaimsExplicitlyDelegatedSpace(t *testing.T) {
	targets := []publishRuntimeTarget{{
		PlatformCode: "douyin", ContentType: "graph_text",
		ComposerURL: "https://creator.douyin.com/creator-micro/content/publish",
		Title:       "冻结标题", Body: "冻结正文",
	}}
	result, err := runPublishChallengeProgram(t, buildEgoPublishPrepareProgram(71, targets), "agentDelegatedToUser")
	if err != nil {
		t.Fatalf("delegated prepare did not reclaim the Space: %v", err)
	}
	if !result.Done {
		t.Fatalf("delegated prepare result=%+v, want completed runtime result", result)
	}
}

func TestPublishDedicatedCompletionFalseOrThrowFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name           string
		completionBody string
	}{
		{name: "false", completionBody: "return { done: false }"},
		{name: "throw", completionBody: `throw new Error("completion failure")`},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, events, err := runPublishPrepareCompletionProgram(t, test.completionBody)
			if test.name == "throw" {
				if err == nil {
					t.Fatal("completion exception must fail the dedicated runtime call")
				}
				return
			}
			if err != nil || result.Done {
				t.Fatalf("completion failure result=%+v events=%v", result, events)
			}
			if strings.Join(events, ",") != "claimTaskSpace:71,completeTaskSpace:71:true,result:ALPHA_EGO_RESULT:{\"done\":false,\"skipped\":\"\"}" {
				t.Fatalf("completion failure leaked or reordered events: %v", events)
			}
		})
	}
}

func TestPublishDedicatedCompletionSuccessRetainsSpace(t *testing.T) {
	result, events, err := runPublishPrepareCompletionProgram(t, "return { done: true }")
	if err != nil {
		t.Fatalf("dedicated completion success failed: %v events=%v", err, events)
	}
	if !result.Done {
		t.Fatalf("dedicated completion result=%+v, want done=true", result)
	}
	if strings.Join(events, ",") != "claimTaskSpace:71,completeTaskSpace:71:true,result:ALPHA_EGO_RESULT:{\"done\":true,\"skipped\":\"\"}" {
		t.Fatalf("completion success leaked or reordered events: %v", events)
	}
}

func TestPublishSubmitReportsBeforeDedicatedCompletion(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the generated EgoLite program")
	}
	program := buildEgoPublishSubmitProgram(71, []publishRuntimeTarget{{
		PlatformCode: "xhs", ContentType: "graph_text", PrepareMode: "manual_handoff",
		ComposerURL: "https://creator.xiaohongshu.com/publish/publish?from=menu&target=image",
	}})
	prelude := `
const events = []
const listTaskSpaces = async () => [{ id: 71, ownership: "agent" }]
const useOrCreateTaskSpace = async (id) => { events.push("useOrCreateTaskSpace:" + String(id)); return { done: true } }
const listTabs = async () => []
const cliLog = (value) => events.push("result:" + String(value))
` + program + `
process.stdout.write(JSON.stringify(events))
`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(prelude)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated submit completion-failure program failed: %v\n%s", err, output)
	}
	var events []string
	if err := json.Unmarshal(output, &events); err != nil {
		t.Fatalf("decode submit completion events: %v\n%s", err, output)
	}
	if len(events) != 2 || events[0] != "useOrCreateTaskSpace:71" {
		t.Fatalf("submit events=%v, want submit result before a separate completion run", events)
	}
	var result publishRuntimeResult
	if err := json.Unmarshal([]byte(strings.TrimPrefix(events[1], "result:"+resultPrefix)), &result); err != nil {
		t.Fatalf("decode submit completion result: %v", err)
	}
	if !result.Done || result.CompletionAttempted || result.CompletionDone || len(result.Results) != 1 ||
		result.Results[0].Status != "failed" || result.Results[0].Clicked == nil || *result.Results[0].Clicked {
		t.Fatalf("submit completion result=%+v", result)
	}
}

func TestKuaishouPrepareStopsForUserControlledSpace(t *testing.T) {
	targets := []publishRuntimeTarget{{
		PlatformCode: "kuaishou", ContentType: "video",
		ComposerURL: "https://cp.kuaishou.com/article/publish/video",
		PublishAt:   "2030-01-02T05:03:00Z", Title: "冻结快手标题", Body: "冻结快手正文",
	}}
	program := buildEgoPublishPrepareProgram(71, targets)
	_, err := runPublishChallengeProgram(t, program, "user")
	if err == nil || err.Error() != "EGO_SPACE_USER_CONTROLLED" {
		t.Fatalf("error=%v, want EGO_SPACE_USER_CONTROLLED", err)
	}
}

func TestPublishPrepareStopsBeforeOpeningFourthComposerTab(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the generated EgoLite program")
	}
	program := buildEgoPublishPrepareProgram(71, []publishRuntimeTarget{{
		PlatformCode: "csdn", ContentType: "article", ComposerURL: "https://editor.csdn.net/md/",
		Title: "冻结标题", Body: "冻结正文", Tags: []string{"AlphaNexus"},
	}})
	prelude := `
const events = []
const listTaskSpaces = async () => [{ id: 71, ownership: "agent" }]
const useOrCreateTaskSpace = async () => ({ done: true })
const listTabs = async () => [
  { id: "one", url: "https://creator.douyin.com/creator-micro/home" },
  { id: "two", url: "https://studio.youtube.com/channel/UC/content" },
  { id: "three", url: "https://mp.csdn.net/mp_blog/manage/article" },
]
const openOrReuseTab = async (url) => { events.push("open:" + url); return {} }
const switchTab = async (id) => { events.push("switch:" + id); return {} }
const completeTaskSpace = async () => ({ done: true })
const cliLog = (value) => { events.push(value); console.log(value) }
`
	command := exec.Command(node, "--input-type=module")
	command.Stdin = strings.NewReader(prelude + "\n" + program)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated cap program failed: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "open:") {
		t.Fatalf("prepare opened a fourth composer tab: %s", output)
	}
	var result publishRuntimeResult
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, resultPrefix) {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, resultPrefix)), &result); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !result.Done || len(result.Results) != 1 || result.Results[0].Status != "failed" || result.Results[0].ErrorCode != "EGO_PUBLISH_COMPOSER_TAB_LIMIT" {
		t.Fatalf("tab-cap result=%+v", result)
	}
}
