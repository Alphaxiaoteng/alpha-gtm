package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// gracefulCloseCDP asks Chromium to flush browser state and close itself.
// Process signals remain the caller's bounded fallback when CDP is unavailable.
func gracefulCloseCDP(cdpURL string) error {
	base, err := localControlURL(cdpURL, "http", "https")
	if err != nil {
		return err
	}
	versionURL := *base
	versionURL.Path = "/json/version"
	versionURL.RawQuery = ""
	client := &http.Client{
		Timeout:       windowControlTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Get(versionURL.String())
	if err != nil {
		return fmt.Errorf("read browser version before close: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("read browser version before close: status %d", response.StatusCode)
	}
	var version struct {
		WebSocketURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&version); err != nil {
		return fmt.Errorf("decode browser version before close: %w", err)
	}
	if _, err := localControlURL(version.WebSocketURL, "ws", "wss"); err != nil {
		return fmt.Errorf("read browser control socket before close: %w", err)
	}
	dialer := websocket.Dialer{HandshakeTimeout: windowControlTimeout}
	conn, _, err := dialer.Dial(version.WebSocketURL, nil)
	if err != nil {
		return fmt.Errorf("connect browser control socket before close: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(windowControlTimeout)
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)
	if err := conn.WriteJSON(map[string]any{"id": 1, "method": "Browser.close"}); err != nil {
		return fmt.Errorf("request browser close: %w", err)
	}
	for {
		var reply struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := conn.ReadJSON(&reply); err != nil {
			return fmt.Errorf("confirm browser close: %w", err)
		}
		if reply.ID != 1 {
			continue
		}
		if reply.Error != nil {
			return fmt.Errorf("request browser close: %s", strings.TrimSpace(reply.Error.Message))
		}
		return nil
	}
}

// ensureCDPWindowForFocus only repairs an empty browser created for an explicit
// user open/login action. Existing pages are never navigated or replaced.
func ensureCDPWindowForFocus(cdpURL, initialURL string) error {
	err := maximizeCDPWindow(cdpURL)
	if !errors.Is(err, errNoCDPPageTarget) {
		return err
	}
	if strings.TrimSpace(initialURL) == "" {
		return err
	}
	if err := createCDPPage(cdpURL, initialURL); err != nil {
		return fmt.Errorf("create initial browser page: %w", err)
	}
	return maximizeCDPWindow(cdpURL)
}

// navigateFreshBlankPage replaces Chromium's single startup about:blank tab
// with the platform page selected for an explicit account launch. It is never
// used for an already-running profile: a restored/user page must not be
// navigated by Alpha-Nexus.
func navigateFreshBlankPage(cdpURL, initialURL string) (bool, error) {
	pageURL, err := url.Parse(strings.TrimSpace(initialURL))
	if err != nil || (pageURL.Scheme != "http" && pageURL.Scheme != "https") || pageURL.Hostname() == "" {
		return false, fmt.Errorf("initial browser URL must be an absolute HTTP(S) URL")
	}
	base, err := localControlURL(cdpURL, "http", "https")
	if err != nil {
		return false, err
	}
	listURL := *base
	listURL.Path = "/json/list"
	listURL.RawQuery = ""
	client := &http.Client{Timeout: windowControlTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(listURL.String())
	if err != nil {
		return false, fmt.Errorf("list browser targets: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("list browser targets: status %d", response.StatusCode)
	}
	var targets []cdpWindowTarget
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&targets); err != nil {
		return false, fmt.Errorf("decode browser targets: %w", err)
	}
	pages := make([]cdpWindowTarget, 0, 1)
	for _, target := range targets {
		if target.Type == "page" {
			pages = append(pages, target)
		}
	}
	if len(pages) != 1 || pages[0].URL != "about:blank" || pages[0].WebSocketURL == "" {
		return false, nil
	}
	if _, err := localControlURL(pages[0].WebSocketURL, "ws", "wss"); err != nil {
		return false, err
	}
	dialer := websocket.Dialer{HandshakeTimeout: windowControlTimeout}
	conn, _, err := dialer.Dial(pages[0].WebSocketURL, nil)
	if err != nil {
		return false, fmt.Errorf("connect blank browser page: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(windowControlTimeout)
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)
	if err := conn.WriteJSON(map[string]any{
		"id":     1,
		"method": "Page.navigate",
		"params": map[string]string{"url": pageURL.String()},
	}); err != nil {
		return false, fmt.Errorf("navigate blank browser page: %w", err)
	}
	for {
		var reply struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := conn.ReadJSON(&reply); err != nil {
			return false, fmt.Errorf("navigate blank browser page: %w", err)
		}
		if reply.ID != 1 {
			continue
		}
		if reply.Error != nil {
			return false, fmt.Errorf("navigate blank browser page: %s", strings.TrimSpace(reply.Error.Message))
		}
		return true, nil
	}
}

func createCDPPage(cdpURL, initialURL string) error {
	base, err := localControlURL(cdpURL, "http", "https")
	if err != nil {
		return err
	}
	pageURL, err := url.Parse(strings.TrimSpace(initialURL))
	if err != nil || (pageURL.Scheme != "http" && pageURL.Scheme != "https") || pageURL.Hostname() == "" {
		return fmt.Errorf("initial browser URL must be an absolute HTTP(S) URL")
	}
	versionURL := *base
	versionURL.Path = "/json/version"
	versionURL.RawQuery = ""
	client := &http.Client{Timeout: windowControlTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(versionURL.String())
	if err != nil {
		return fmt.Errorf("read browser version: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("read browser version: status %d", response.StatusCode)
	}
	var version struct {
		WebSocketURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&version); err != nil {
		return fmt.Errorf("decode browser version: %w", err)
	}
	if _, err := localControlURL(version.WebSocketURL, "ws", "wss"); err != nil {
		return fmt.Errorf("read browser control socket: %w", err)
	}
	dialer := websocket.Dialer{HandshakeTimeout: windowControlTimeout}
	conn, _, err := dialer.Dial(version.WebSocketURL, nil)
	if err != nil {
		return fmt.Errorf("connect browser control socket: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(windowControlTimeout)
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)
	if err := conn.WriteJSON(map[string]any{
		"id":     1,
		"method": "Target.createTarget",
		"params": map[string]string{"url": pageURL.String()},
	}); err != nil {
		return fmt.Errorf("create browser target: %w", err)
	}
	for {
		var reply struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := conn.ReadJSON(&reply); err != nil {
			return fmt.Errorf("create browser target: %w", err)
		}
		if reply.ID != 1 {
			continue
		}
		if reply.Error != nil {
			return fmt.Errorf("create browser target: %s", strings.TrimSpace(reply.Error.Message))
		}
		return nil
	}
}
