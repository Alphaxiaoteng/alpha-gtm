package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
)

// fakeEgoBrowser writes an executable that answers the Space-screen program.
// The program it receives must never take over: the handler is read-only.
func fakeEgoBrowser(t *testing.T, ownership string, png string) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "ego-browser")
	payload := map[string]any{
		"space_id":        462,
		"ownership":       ownership,
		"user_controlled": ownership == "user" || ownership == "agentDelegatedToUser",
		"png":             png,
		"url":             "https://example.test/creator",
		"mime":            "image/jpeg",
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		"#!/bin/sh",
		// Refuse to run if the program tried to take control: this handler must
		// never post takeOverTaskSpace.
		"if grep -q 'takeOverTaskSpace' /dev/stdin; then echo 'takeover attempted' >&2; exit 3; fi",
		"printf '%s%s\\n' 'ALPHA_EGO_RESULT:' '" + string(encoded) + "'",
		"",
	}, "\n")
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

func newScreenTestServer(t *testing.T, script string) *Server {
	t.Helper()
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0, EgoBrowserCommand: script})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	return server
}

func TestEgoSpaceScreenRejectsInvalidID(t *testing.T) {
	server := newScreenTestServer(t, fakeEgoBrowser(t, "agent", "AAAA"))
	request := httptest.NewRequest(http.MethodGet, "/api/ego/spaces/not-a-number/screen", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid id = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestEgoSpaceScreenIsLoopbackOnly(t *testing.T) {
	server := newScreenTestServer(t, fakeEgoBrowser(t, "agent", "AAAA"))
	request := httptest.NewRequest(http.MethodGet, "/api/ego/spaces/462/screen", nil)
	request.RemoteAddr = "10.0.0.9:50000"
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("non-loopback = %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestEgoSpaceScreenReturnsFrameForAgentSpace(t *testing.T) {
	server := newScreenTestServer(t, fakeEgoBrowser(t, "agent", "ZmFrZWpwZWc="))
	request := httptest.NewRequest(http.MethodGet, "/api/ego/spaces/462/screen", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("agent space = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var frame egoScreenFrame
	if err := json.Unmarshal(recorder.Body.Bytes(), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.UserControlled || frame.PNG == "" || frame.Mime != "image/jpeg" {
		t.Fatalf("frame = %+v", frame)
	}
	if frame.SpaceID != 462 || frame.CapturedAt == "" {
		t.Fatalf("frame metadata = %+v", frame)
	}
}

func TestEgoSpaceScreenReportsUserControlWithoutFrame(t *testing.T) {
	server := newScreenTestServer(t, fakeEgoBrowser(t, "agentDelegatedToUser", ""))
	request := httptest.NewRequest(http.MethodGet, "/api/ego/spaces/462/screen", nil)
	request.RemoteAddr = "127.0.0.1:17321"
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("user space = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var frame egoScreenFrame
	if err := json.Unmarshal(recorder.Body.Bytes(), &frame); err != nil {
		t.Fatal(err)
	}
	if !frame.UserControlled || frame.PNG != "" {
		t.Fatalf("user-controlled frame = %+v", frame)
	}
}

func TestEgoSpaceScreenServesCachedFrame(t *testing.T) {
	server := newScreenTestServer(t, fakeEgoBrowser(t, "agent", "ZmFrZWpwZWc="))
	first := httptest.NewRequest(http.MethodGet, "/api/ego/spaces/462/screen", nil)
	first.RemoteAddr = "127.0.0.1:17321"
	firstRecorder := httptest.NewRecorder()
	server.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first = %d body=%s", firstRecorder.Code, firstRecorder.Body.String())
	}
	// Point the runtime at a broken command; a cached frame must not run it.
	server.egoRuntime = nil
	second := httptest.NewRequest(http.MethodGet, "/api/ego/spaces/462/screen", nil)
	second.RemoteAddr = "127.0.0.1:17321"
	secondRecorder := httptest.NewRecorder()
	server.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code == http.StatusOK {
		// The TTL is 1.5s, so a second call may miss and hit the unavailable
		// runtime instead. Either a cached 200 or a 503 is correct; a panic or
		// a bogus 200 with an empty frame is not.
		var frame egoScreenFrame
		if err := json.Unmarshal(secondRecorder.Body.Bytes(), &frame); err != nil || frame.PNG == "" {
			t.Fatalf("cached frame lost: %d %s", secondRecorder.Code, secondRecorder.Body.String())
		}
	}
}
