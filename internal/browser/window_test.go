package browser

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

func TestMaximizeCDPWindowSetsMaximizedState(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var received []string
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/page/1"
		_ = json.NewEncoder(w).Encode([]map[string]string{{"type": "page", "webSocketDebuggerUrl": wsURL}})
	})
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		for len(received) < 2 {
			_, data, err := conn.ReadMessage()
			if err != nil {
				t.Error(err)
				return
			}
			var command struct {
				ID     int                    `json:"id"`
				Method string                 `json:"method"`
				Params map[string]interface{} `json:"params"`
			}
			if err := json.Unmarshal(data, &command); err != nil {
				t.Error(err)
				return
			}
			received = append(received, command.Method)
			if command.Method == "Browser.getWindowForTarget" {
				_ = conn.WriteJSON(map[string]interface{}{"id": command.ID, "result": map[string]interface{}{"windowId": 7}})
				continue
			}
			_ = conn.WriteJSON(map[string]interface{}{"id": command.ID, "result": map[string]interface{}{}})
		}
	})

	if err := maximizeCDPWindow(server.URL); err != nil {
		t.Fatal(err)
	}
	if strings.Join(received, ",") != "Browser.getWindowForTarget,Browser.setWindowBounds" {
		t.Fatalf("unexpected CDP commands: %v", received)
	}
}

func TestMaximizeCDPWindowRestoresMinimizedWindowBeforeRetry(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var states []string
	var observedStates []string
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/page/1"
		_ = json.NewEncoder(w).Encode([]map[string]string{{"type": "page", "webSocketDebuggerUrl": wsURL}})
	})
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		for commandIndex := 0; commandIndex < 5; commandIndex++ {
			var command struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
				Params struct {
					Bounds struct {
						WindowState string `json:"windowState"`
					} `json:"bounds"`
				} `json:"params"`
			}
			if err := conn.ReadJSON(&command); err != nil {
				t.Error(err)
				return
			}
			if command.Method == "Browser.getWindowForTarget" {
				_ = conn.WriteJSON(map[string]any{
					"id": command.ID,
					"result": map[string]any{
						"windowId": 7,
						"bounds":   map[string]string{"windowState": "minimized"},
					},
				})
				continue
			}
			if command.Method == "Browser.getWindowBounds" {
				state := "minimized"
				if len(observedStates) > 0 {
					state = "normal"
				}
				observedStates = append(observedStates, state)
				_ = conn.WriteJSON(map[string]any{
					"id":     command.ID,
					"result": map[string]any{"bounds": map[string]string{"windowState": state}},
				})
				continue
			}
			if command.Method != "Browser.setWindowBounds" {
				t.Errorf("unexpected method %q", command.Method)
				return
			}
			states = append(states, command.Params.Bounds.WindowState)
			_ = conn.WriteJSON(map[string]any{"id": command.ID, "result": map[string]any{}})
		}
	})

	if err := maximizeCDPWindow(server.URL); err != nil {
		t.Fatal(err)
	}
	if strings.Join(states, ",") != "normal,maximized" {
		t.Fatalf("window states=%v, want normal then maximized", states)
	}
	if strings.Join(observedStates, ",") != "minimized,normal" {
		t.Fatalf("observed states=%v, want bounded polling until normal", observedStates)
	}
}

func TestMaximizeCDPWindowSetsEveryPageWindowMaximized(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	maximized := make(chan int, 2)
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		base := "ws" + strings.TrimPrefix(server.URL, "http")
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"type": "page", "webSocketDebuggerUrl": base + "/devtools/page/1"},
			{"type": "service_worker", "webSocketDebuggerUrl": base + "/devtools/worker/1"},
			{"type": "page", "webSocketDebuggerUrl": base + "/devtools/page/2"},
		})
	})
	servePage := func(windowID int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			for commandIndex := 0; commandIndex < 2; commandIndex++ {
				var command struct {
					ID     int                    `json:"id"`
					Method string                 `json:"method"`
					Params map[string]interface{} `json:"params"`
				}
				if err := conn.ReadJSON(&command); err != nil {
					t.Error(err)
					return
				}
				if command.Method == "Browser.getWindowForTarget" {
					_ = conn.WriteJSON(map[string]interface{}{"id": command.ID, "result": map[string]interface{}{"windowId": windowID}})
					continue
				}
				if command.Method != "Browser.setWindowBounds" {
					t.Errorf("unexpected command %q", command.Method)
					return
				}
				maximized <- windowID
				_ = conn.WriteJSON(map[string]interface{}{"id": command.ID, "result": map[string]interface{}{}})
			}
		}
	}
	mux.HandleFunc("/devtools/page/1", servePage(7))
	mux.HandleFunc("/devtools/page/2", servePage(9))

	if err := maximizeCDPWindow(server.URL); err != nil {
		t.Fatal(err)
	}
	got := []int{<-maximized}
	select {
	case id := <-maximized:
		got = append(got, id)
	default:
	}
	if len(got) != 2 || got[0] != 7 || got[1] != 9 {
		t.Fatalf("maximized windows = %v, want [7 9]", got)
	}
}

func TestEnsureCDPWindowForFocusCreatesConfiguredPageOnlyWhenNoPageExists(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	var mu sync.Mutex
	hasPage := false
	createdURL := ""
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if !hasPage {
			_ = json.NewEncoder(w).Encode([]map[string]string{})
			return
		}
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/page/1"
		_ = json.NewEncoder(w).Encode([]map[string]string{{"type": "page", "webSocketDebuggerUrl": wsURL}})
	})
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, _ *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/browser"
		_ = json.NewEncoder(w).Encode(map[string]string{"webSocketDebuggerUrl": wsURL})
	})
	mux.HandleFunc("/devtools/browser", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		var command struct {
			ID     int               `json:"id"`
			Method string            `json:"method"`
			Params map[string]string `json:"params"`
		}
		if err := conn.ReadJSON(&command); err != nil {
			t.Error(err)
			return
		}
		if command.Method != "Target.createTarget" {
			t.Errorf("method=%q, want Target.createTarget", command.Method)
			return
		}
		mu.Lock()
		createdURL = command.Params["url"]
		hasPage = true
		mu.Unlock()
		_ = conn.WriteJSON(map[string]any{"id": command.ID, "result": map[string]string{"targetId": "page-1"}})
	})
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		for commandIndex := 0; commandIndex < 2; commandIndex++ {
			var command struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			if err := conn.ReadJSON(&command); err != nil {
				t.Error(err)
				return
			}
			if command.Method == "Browser.getWindowForTarget" {
				_ = conn.WriteJSON(map[string]any{"id": command.ID, "result": map[string]int{"windowId": 7}})
				continue
			}
			_ = conn.WriteJSON(map[string]any{"id": command.ID, "result": map[string]any{}})
		}
	})

	if err := ensureCDPWindowForFocus(server.URL, "https://mp.csdn.net"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if createdURL != "https://mp.csdn.net" {
		t.Fatalf("created URL=%q", createdURL)
	}
}

func TestEnsureCDPWindowForFocusNeverCreatesOrNavigatesExistingPage(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/page/1"
		_ = json.NewEncoder(w).Encode([]map[string]string{{"type": "page", "webSocketDebuggerUrl": wsURL}})
	})
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("existing page must not request Target.createTarget")
		http.Error(w, "unexpected", http.StatusInternalServerError)
	})
	mux.HandleFunc("/devtools/page/1", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		for commandIndex := 0; commandIndex < 2; commandIndex++ {
			var command struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			if err := conn.ReadJSON(&command); err != nil {
				t.Error(err)
				return
			}
			if command.Method == "Browser.getWindowForTarget" {
				_ = conn.WriteJSON(map[string]any{"id": command.ID, "result": map[string]int{"windowId": 7}})
				continue
			}
			_ = conn.WriteJSON(map[string]any{"id": command.ID, "result": map[string]any{}})
		}
	})

	if err := ensureCDPWindowForFocus(server.URL, "https://mp.csdn.net"); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureCDPWindowForFocusReportsEmptyTargetWithoutAnInitialURL(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]string{})
	})
	if err := ensureCDPWindowForFocus(server.URL, ""); !errors.Is(err, errNoCDPPageTarget) {
		t.Fatalf("err=%v, want no-page-target error", err)
	}
}

func TestNavigateFreshBlankPageReplacesOnlyStartupBlankTab(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	var gotURL string
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/page/blank"
		_ = json.NewEncoder(w).Encode([]map[string]string{{"type": "page", "url": "about:blank", "webSocketDebuggerUrl": wsURL}})
	})
	mux.HandleFunc("/devtools/page/blank", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		var command struct {
			ID     int               `json:"id"`
			Method string            `json:"method"`
			Params map[string]string `json:"params"`
		}
		if err := conn.ReadJSON(&command); err != nil {
			t.Error(err)
			return
		}
		if command.Method != "Page.navigate" {
			t.Errorf("method=%q", command.Method)
			return
		}
		gotURL = command.Params["url"]
		_ = conn.WriteJSON(map[string]any{"id": command.ID, "result": map[string]any{}})
	})

	navigated, err := navigateFreshBlankPage(server.URL, "https://mp.csdn.net")
	if err != nil || !navigated || gotURL != "https://mp.csdn.net" {
		t.Fatalf("navigated=%v url=%q err=%v", navigated, gotURL, err)
	}
}

func TestNavigateFreshBlankPageNeverTouchesExistingOrRestoredPages(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"type": "page", "url": "about:blank", "webSocketDebuggerUrl": "ws://127.0.0.1:1/devtools/page/blank"},
			{"type": "page", "url": "https://creator.xiaohongshu.com", "webSocketDebuggerUrl": "ws://127.0.0.1:1/devtools/page/creator"},
		})
	})
	navigated, err := navigateFreshBlankPage(server.URL, "https://creator.xiaohongshu.com")
	if err != nil || navigated {
		t.Fatalf("navigated=%v err=%v", navigated, err)
	}
}
