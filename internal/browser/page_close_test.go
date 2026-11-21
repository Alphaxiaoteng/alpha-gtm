package browser

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestGracefulCloseCDPSendsBrowserCloseToLocalControlSocket(t *testing.T) {
	t.Helper()
	requests := make(chan string, 1)
	upgrader := websocket.Upgrader{}
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/browser/test",
		})
	})
	mux.HandleFunc("/devtools/browser/test", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		var command struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := conn.ReadJSON(&command); err != nil {
			t.Errorf("read command: %v", err)
			return
		}
		requests <- command.Method
		_ = conn.WriteJSON(map[string]any{"id": command.ID, "result": map[string]any{}})
	})
	server = httptest.NewServer(mux)
	defer server.Close()

	if err := gracefulCloseCDP(server.URL); err != nil {
		t.Fatal(err)
	}
	select {
	case method := <-requests:
		if method != "Browser.close" {
			t.Fatalf("method=%q", method)
		}
	default:
		t.Fatal(fmt.Errorf("browser close command was not observed"))
	}
}
