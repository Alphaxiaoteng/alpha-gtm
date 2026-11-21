package tools

import (
	"context"
	"strings"
	"testing"
)

func TestTranscriptToolsAreRegistered(t *testing.T) {
	RegisterCatalog()

	for _, name := range []string{"media.transcript", "media.transcript.status"} {
		var def Def
		var found bool
		for _, candidate := range Default.List() {
			if candidate.Name == name {
				def, found = candidate, true
				break
			}
		}
		if !found {
			t.Fatalf("%s is not registered", name)
		}
		if def.SideEffect != "read" {
			t.Errorf("%s side effect = %q, want read", name, def.SideEffect)
		}
	}
}

func TestTranscriptStatusReportsInstallation(t *testing.T) {
	RegisterCatalog()
	result, err := Default.Call(context.Background(), nil, "media.transcript.status", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if _, ok := result.Data["available"]; !ok {
		t.Errorf("status should report availability, got %+v", result.Data)
	}
}

func TestTranscriptRequiresURL(t *testing.T) {
	RegisterCatalog()
	if _, err := Default.Call(context.Background(), nil, "media.transcript", map[string]interface{}{}); err == nil {
		t.Fatal("missing url should fail")
	}
}

func TestTranscriptRejectsNonPublicURL(t *testing.T) {
	RegisterCatalog()
	_, err := Default.Call(context.Background(), nil, "media.transcript", map[string]interface{}{
		"url": "file:///etc/passwd",
	})
	if err == nil {
		t.Fatal("non-public url should be rejected")
	}
	if strings.Contains(err.Error(), "passwd") {
		t.Errorf("error must not echo the rejected path: %v", err)
	}
}

func TestCopyToolsRegistered(t *testing.T) {
	RegisterCatalog()
	for _, name := range []string{"copy.humanize", "copy.rules.list"} {
		found := false
		for _, def := range Default.List() {
			if def.Name == name {
				found = true
				if def.SideEffect != "read" {
					t.Errorf("%s side effect = %q, want read", name, def.SideEffect)
				}
			}
		}
		if !found {
			t.Errorf("%s is not registered", name)
		}
	}
}

func TestCopyHumanizeToolRewrites(t *testing.T) {
	RegisterCatalog()
	result, err := Default.Call(context.Background(), nil, "copy.humanize", map[string]interface{}{
		"text": "众所周知，我们要赋能团队。",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	text, _ := result.Data["text"].(string)
	if strings.Contains(text, "众所周知") || strings.Contains(text, "赋能") {
		t.Errorf("rules should have fired, got %q", text)
	}
	if changed, _ := result.Data["changed"].(bool); !changed {
		t.Error("changed should be true")
	}
}

func TestCopyHumanizeRequiresText(t *testing.T) {
	RegisterCatalog()
	if _, err := Default.Call(context.Background(), nil, "copy.humanize", map[string]interface{}{}); err == nil {
		t.Fatal("missing text should fail")
	}
}
