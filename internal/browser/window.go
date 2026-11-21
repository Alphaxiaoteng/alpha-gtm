package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	windowControlTimeout      = 3 * time.Second
	windowRestoreTimeout      = 750 * time.Millisecond
	windowRestorePollInterval = 25 * time.Millisecond
)

var errNoCDPPageTarget = errors.New("browser has no page target")

type cdpWindowTarget struct {
	Type         string `json:"type"`
	URL          string `json:"url"`
	WebSocketURL string `json:"webSocketDebuggerUrl"`
}

type cdpWindowReply struct {
	ID     int `json:"id"`
	Result struct {
		WindowID int `json:"windowId"`
		Bounds   struct {
			WindowState string `json:"windowState"`
		} `json:"bounds"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func localControlURL(raw string, schemes ...string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	allowedScheme := false
	for _, scheme := range schemes {
		allowedScheme = allowedScheme || u.Scheme == scheme
	}
	ip := net.ParseIP(u.Hostname())
	if !allowedScheme || ip == nil || !ip.IsLoopback() || u.Port() == "" {
		return nil, fmt.Errorf("browser window control requires a loopback endpoint")
	}
	return u, nil
}

// maximizeCDPWindow changes only native window state. It applies the bound to
// every page target because one Chromium profile can restore more than one
// native window. It never navigates, creates, closes, or submits a target.
func maximizeCDPWindow(cdpURL string) error {
	base, err := localControlURL(cdpURL, "http", "https")
	if err != nil {
		return err
	}
	listURL := *base
	listURL.Path = "/json/list"
	listURL.RawQuery = ""
	client := &http.Client{
		Timeout:       windowControlTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(listURL.String())
	if err != nil {
		return fmt.Errorf("list browser targets: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list browser targets: status %d", resp.StatusCode)
	}
	var targets []cdpWindowTarget
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&targets); err != nil {
		return fmt.Errorf("decode browser targets: %w", err)
	}
	var pageTargets []string
	for _, target := range targets {
		if target.Type == "page" && target.WebSocketURL != "" {
			pageTargets = append(pageTargets, target.WebSocketURL)
		}
	}
	if len(pageTargets) == 0 {
		return errNoCDPPageTarget
	}
	for _, wsURL := range pageTargets {
		if err := maximizeCDPTarget(wsURL); err != nil {
			return err
		}
	}
	return nil
}

func maximizeCDPTarget(wsURL string) error {
	if _, err := localControlURL(wsURL, "ws", "wss"); err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: windowControlTimeout}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect browser window target: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(windowControlTimeout)
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)

	windowID, windowState, err := cdpWindowInfo(conn)
	if err != nil {
		return err
	}
	commandID := 2
	if windowState == "minimized" || windowState == "fullscreen" {
		return restoreAndMaximizeCDPWindow(conn, &commandID, windowID)
	}
	if err := setCDPWindowState(conn, commandID, windowID, "maximized"); err == nil {
		return nil
	} else if !strings.Contains(strings.ToLower(err.Error()), "restore it to normal state first") {
		return fmt.Errorf("maximize browser window: %w", err)
	}
	commandID++
	return restoreAndMaximizeCDPWindow(conn, &commandID, windowID)
}

func restoreAndMaximizeCDPWindow(conn *websocket.Conn, commandID *int, windowID int) error {
	if err := setCDPWindowState(conn, *commandID, windowID, "normal"); err != nil {
		return fmt.Errorf("restore browser window to normal state: %w", err)
	}
	(*commandID)++
	// Native fullscreen/minimize transitions finish asynchronously.
	if err := waitForCDPWindowState(conn, commandID, windowID, "normal"); err != nil {
		return fmt.Errorf("restore browser window to normal state: %w", err)
	}
	if err := setCDPWindowState(conn, *commandID, windowID, "maximized"); err != nil {
		return fmt.Errorf("maximize browser window: %w", err)
	}
	return nil
}

func waitForCDPWindowState(conn *websocket.Conn, commandID *int, windowID int, want string) error {
	deadline := time.Now().Add(windowRestoreTimeout)
	for {
		state, err := getCDPWindowState(conn, *commandID, windowID)
		(*commandID)++
		if err != nil {
			return err
		}
		if state == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("window remained %q while waiting for %q", state, want)
		}
		time.Sleep(windowRestorePollInterval)
	}
}

func getCDPWindowState(conn *websocket.Conn, commandID, windowID int) (string, error) {
	if err := conn.WriteJSON(map[string]interface{}{
		"id":     commandID,
		"method": "Browser.getWindowBounds",
		"params": map[string]int{"windowId": windowID},
	}); err != nil {
		return "", err
	}
	for {
		var reply cdpWindowReply
		if err := conn.ReadJSON(&reply); err != nil {
			return "", err
		}
		if reply.ID != commandID {
			continue
		}
		if reply.Error != nil {
			return "", errors.New(strings.TrimSpace(reply.Error.Message))
		}
		return reply.Result.Bounds.WindowState, nil
	}
}

func setCDPWindowState(conn *websocket.Conn, commandID, windowID int, state string) error {
	command := map[string]interface{}{
		"id":     commandID,
		"method": "Browser.setWindowBounds",
		"params": map[string]interface{}{
			"windowId": windowID,
			"bounds":   map[string]string{"windowState": state},
		},
	}
	if err := conn.WriteJSON(command); err != nil {
		return err
	}
	for {
		var reply cdpWindowReply
		if err := conn.ReadJSON(&reply); err != nil {
			return err
		}
		if reply.ID != commandID {
			continue
		}
		if reply.Error != nil {
			return errors.New(strings.TrimSpace(reply.Error.Message))
		}
		return nil
	}
}

func cdpWindowInfo(conn *websocket.Conn) (int, string, error) {
	if err := conn.WriteJSON(map[string]interface{}{"id": 1, "method": "Browser.getWindowForTarget"}); err != nil {
		return 0, "", fmt.Errorf("read browser window: %w", err)
	}
	for {
		var reply cdpWindowReply
		if err := conn.ReadJSON(&reply); err != nil {
			return 0, "", fmt.Errorf("read browser window: %w", err)
		}
		if reply.ID != 1 {
			continue
		}
		if reply.Error != nil {
			return 0, "", fmt.Errorf("read browser window: %s", strings.TrimSpace(reply.Error.Message))
		}
		if reply.Result.WindowID == 0 {
			return 0, "", fmt.Errorf("browser returned an empty window id")
		}
		return reply.Result.WindowID, reply.Result.Bounds.WindowState, nil
	}
}
