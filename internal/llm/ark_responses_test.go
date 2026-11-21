package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestArkResponsesCompleteSendsModelAndInputAndCollectsOutputText(t *testing.T) {
	const syntheticKey = "synthetic-key-that-must-not-leak"
	var gotPath, gotAuthorization string
	var gotPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "resp_fixture",
			"output": [
				{"type":"reasoning","content":[]},
				{"type":"message","content":[
					{"type":"output_text","text":"第一段"},
					{"type":"refusal","text":""},
					{"type":"output_text","text":"第二段"}
				]}
			]
		}`))
	}))
	defer server.Close()

	client, err := newArkResponsesClient(
		server.URL+"/api/v3/responses",
		"fixture-model",
		syntheticKey,
		server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	output, err := client.Complete(context.Background(), "fixture input")
	if err != nil {
		t.Fatal(err)
	}

	if output != "第一段\n第二段" {
		t.Fatalf("output=%q", output)
	}
	if gotPath != "/api/v3/responses" {
		t.Fatalf("path=%q", gotPath)
	}
	if gotAuthorization != "Bearer "+syntheticKey {
		t.Fatalf("authorization header was not set correctly")
	}
	if gotPayload["model"] != "fixture-model" || gotPayload["input"] != "fixture input" {
		t.Fatalf("payload=%v", gotPayload)
	}
	if len(gotPayload) != 2 {
		t.Fatalf("unexpected payload fields=%v", gotPayload)
	}
}

func TestNewArkResponsesClientUsesOnlyExistingKeychainLoader(t *testing.T) {
	previous := loadDoubaoKeychainKey
	loadDoubaoKeychainKey = func() string { return " synthetic-key " }
	t.Cleanup(func() { loadDoubaoKeychainKey = previous })
	t.Setenv("ARK_API_KEY", "environment-key-must-not-be-used")
	t.Setenv("AGENTPUSH_LLM_KEY", "environment-key-must-not-be-used")

	client, err := NewArkResponsesClient("fixture-model", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if client.apiKey != "synthetic-key" {
		t.Fatal("client did not use the trimmed Keychain value")
	}
	if client.endpoint != ArkResponsesEndpoint {
		t.Fatalf("endpoint=%q", client.endpoint)
	}
}

func TestNewArkResponsesClientFailsClosedWithoutKeychainCredential(t *testing.T) {
	previous := loadDoubaoKeychainKey
	loadDoubaoKeychainKey = func() string { return "" }
	t.Cleanup(func() { loadDoubaoKeychainKey = previous })
	t.Setenv("ARK_API_KEY", "environment-key-must-not-be-used")

	client, err := NewArkResponsesClient("fixture-model", time.Second)
	if client != nil {
		t.Fatal("client must be nil without a Keychain credential")
	}
	var responseErr *ArkResponsesError
	if !errors.As(err, &responseErr) || responseErr.Code != ArkResponsesNotConfigured {
		t.Fatalf("err=%T code=%v", err, responseErr)
	}
}

func TestArkResponsesHTTPErrorIsStructuredAndRedacted(t *testing.T) {
	const (
		syntheticKey = "synthetic-key-that-must-not-leak"
		secretBody   = "secret-provider-error-body"
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Tt-Logid", "logid-fixture")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(secretBody))
	}))
	defer server.Close()

	client, err := newArkResponsesClient(
		server.URL+"/responses",
		"fixture-model",
		syntheticKey,
		server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), "input-must-not-leak")
	if err == nil {
		t.Fatal("expected error")
	}
	var responseErr *ArkResponsesError
	if !errors.As(err, &responseErr) {
		t.Fatalf("err=%T", err)
	}
	if responseErr.Code != ArkResponsesHTTPFailed ||
		responseErr.StatusCode != http.StatusUnauthorized ||
		responseErr.RequestID != "logid-fixture" {
		t.Fatalf("structured error=%+v", responseErr)
	}
	for _, secret := range []string{syntheticKey, secretBody, "input-must-not-leak"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked protected data")
		}
	}
}

func TestArkResponsesRejectsBlankModelAndInput(t *testing.T) {
	if _, err := newArkResponsesClient(
		"https://example.invalid/responses",
		" \n ",
		"synthetic-key",
		http.DefaultClient,
	); !isArkResponsesErrorCode(err, ArkResponsesInvalidRequest) {
		t.Fatalf("blank model err=%v", err)
	}

	client, err := newArkResponsesClient(
		"https://example.invalid/responses",
		"fixture-model",
		"synthetic-key",
		http.DefaultClient,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), " \t "); !isArkResponsesErrorCode(err, ArkResponsesInvalidRequest) {
		t.Fatalf("blank input err=%v", err)
	}
}

func TestArkResponsesRejectsInvalidOrEmptyProviderOutput(t *testing.T) {
	tests := []struct {
		name string
		body string
		code ArkResponsesErrorCode
	}{
		{name: "invalid json", body: `not-json`, code: ArkResponsesInvalidResponse},
		{name: "empty output", body: `{"output":[{"content":[{"type":"output_text","text":"  "}]}]}`, code: ArkResponsesEmptyOutput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			client, err := newArkResponsesClient(
				server.URL+"/responses",
				"fixture-model",
				"synthetic-key",
				server.Client(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Complete(context.Background(), "fixture input"); !isArkResponsesErrorCode(err, test.code) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestValidateArkResponsesModelUsesProvidedKeyOnlyInMemory(t *testing.T) {
	const syntheticKey = "one-time-synthetic-key"
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{
			"output":[{"content":[{"type":"output_text","text":"OK"}]}]
		}`))
	}))
	defer server.Close()

	err := validateArkResponsesModel(
		context.Background(),
		server.URL+"/responses",
		syntheticKey,
		"fixture-model",
		server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuthorization != "Bearer "+syntheticKey {
		t.Fatal("one-time validation key was not used")
	}
}

func isArkResponsesErrorCode(err error, code ArkResponsesErrorCode) bool {
	var responseErr *ArkResponsesError
	return errors.As(err, &responseErr) && responseErr.Code == code
}
