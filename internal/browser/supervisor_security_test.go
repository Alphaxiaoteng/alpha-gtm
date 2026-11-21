//go:build legacy_chromium

package browser

import (
	"os"
	"strings"
	"testing"
)

func TestBrowserLaunchDoesNotInjectLLMCredentials(t *testing.T) {
	source, err := os.ReadFile("supervisor.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"ResolveDoubaoConfig", "OPENAI_API_KEY=", "ANTHROPIC_API_KEY=", "GEMINI_API_KEY="} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("browser process must not receive LLM credentials: %s", forbidden)
		}
	}
}

func TestSanitizedBrowserEnvironmentRemovesInheritedCredentials(t *testing.T) {
	input := []string{
		"PATH=/usr/bin", "HOME=/tmp/profile", "LANG=zh_CN.UTF-8",
		"OPENAI_API_KEY=openai-secret", "ARK_API_KEY=ark-secret",
		"VOLCENGINE_ACCESS_KEY=access-secret", "VOLCENGINE_SECRET_KEY=secret-secret",
		"DOUBAO_KEY=doubao-secret", "GH_TOKEN=github-secret", "SERVICE_PASSWORD=password-secret",
	}
	got := sanitizedBrowserEnvironment(input)
	joined := strings.Join(got, "\n")
	for _, kept := range []string{"PATH=/usr/bin", "HOME=/tmp/profile", "LANG=zh_CN.UTF-8"} {
		if !strings.Contains(joined, kept) {
			t.Fatalf("safe browser environment lost %s: %v", kept, got)
		}
	}
	for _, secret := range []string{"OPENAI_API_KEY", "ARK_API_KEY", "VOLCENGINE_ACCESS_KEY", "VOLCENGINE_SECRET_KEY", "DOUBAO_KEY", "GH_TOKEN", "SERVICE_PASSWORD"} {
		if strings.Contains(joined, secret+"=") {
			t.Fatalf("browser inherited credential %s: %v", secret, got)
		}
	}
}
