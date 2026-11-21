package feedback

import (
	"errors"
	"strings"
	"testing"
)

func TestConfigBundleRoundTripAndStrictValidation(t *testing.T) {
	input := Config{
		AppID:     "  cli_test_app  ",
		AppSecret: "  synthetic-test-secret  ",
		WikiToken: "  wikcn_test  ",
		TableID:   "  tbl_test  ",
	}
	encoded, err := encodeConfig(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded, "synthetic-test-secret") || strings.Contains(encoded, "\n") {
		t.Fatalf("encoded bundle is not a single JSON object: %q", encoded)
	}
	decoded, err := decodeConfig(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.AppID != "cli_test_app" ||
		decoded.AppSecret != "synthetic-test-secret" ||
		decoded.WikiToken != "wikcn_test" ||
		decoded.BaseToken != "" ||
		decoded.TableID != "tbl_test" {
		t.Fatalf("decoded=%+v", decoded)
	}

	for _, raw := range []string{
		`{"app_id":"id","app_secret":"secret","wiki_token":"wiki","table_id":"table","unknown":true}`,
		`{"app_id":"id","app_secret":"secret","wiki_token":"wiki","table_id":"table"} trailing`,
	} {
		if _, err := decodeConfig(raw); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("raw=%q err=%v", raw, err)
		}
	}
}

func TestConfigBundleRejectsMissingAmbiguousOrUnsafeValues(t *testing.T) {
	valid := Config{
		AppID:     "cli_test_app",
		AppSecret: "synthetic-test-secret",
		WikiToken: "wikcn_test",
		TableID:   "tbl_test",
	}
	cases := []Config{
		{},
		{AppID: valid.AppID, AppSecret: valid.AppSecret, TableID: valid.TableID},
		{
			AppID: valid.AppID, AppSecret: valid.AppSecret,
			WikiToken: valid.WikiToken, BaseToken: "bascn_test", TableID: valid.TableID,
		},
		{AppID: valid.AppID, AppSecret: "unsafe\nsecret", WikiToken: valid.WikiToken, TableID: valid.TableID},
		{AppID: valid.AppID, AppSecret: valid.AppSecret, WikiToken: valid.WikiToken, TableID: "tbl\x00test"},
	}
	for _, candidate := range cases {
		if _, err := encodeConfig(candidate); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("candidate=%+v err=%v", candidate, err)
		}
	}
}
