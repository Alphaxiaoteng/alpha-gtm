package publishplan

import "testing"

func TestManagerScheduledAtUsesBoundedNativeRoundedTime(t *testing.T) {
	got, err := managerScheduledAt(
		"2026-07-31T05:44:00Z",
		map[string]interface{}{"effectiveScheduledAt": "2026-07-31T05:45:00.000Z"},
	)
	if err != nil || got != "2026-07-31T05:45:00Z" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	for _, invalid := range []string{
		"2026-07-31T05:43:00Z",
		"2026-07-31T07:00:00Z",
		"not-a-time",
	} {
		if _, err := managerScheduledAt(
			"2026-07-31T05:44:00Z",
			map[string]interface{}{"effectiveScheduledAt": invalid},
		); err == nil {
			t.Fatalf("accepted invalid effective time %q", invalid)
		}
	}
}
