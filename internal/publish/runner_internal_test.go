package publish

import "testing"

func TestSafePublishMessageExplainsPlatformVerificationFailure(t *testing.T) {
	got := safePublishMessage(StatusManualTakeover, "PLATFORM_VERIFICATION_REQUIRED")
	want := "publish stopped because the platform requires human verification"
	if got != want {
		t.Fatalf("safePublishMessage() = %q, want %q", got, want)
	}
}
