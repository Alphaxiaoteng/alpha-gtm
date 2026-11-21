package ui

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
)

var egoLiteAvatarFiles = map[string]string{
	"27": "avatar_origami_cat.png",
	"28": "avatar_origami_corgi.png",
	"29": "avatar_origami_dragon.png",
	"30": "avatar_origami_elephant.png",
	"31": "avatar_origami_fox.png",
	"32": "avatar_origami_monkey.png",
	"33": "avatar_origami_panda.png",
	"34": "avatar_origami_penguin.png",
	"35": "avatar_origami_pinkbutterfly.png",
	"36": "avatar_origami_rabbit.png",
	"37": "avatar_origami_unicorn.png",
}

func defaultEgoLiteProfileRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "Citro Labs", "ego lite")
}

func egoLiteProfileAvatarPath(root, identityName string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	identityName = strings.TrimSpace(identityName)
	if root == "." || identityName == "" {
		return "", os.ErrNotExist
	}
	payload, err := os.ReadFile(filepath.Join(root, "Local State"))
	if err != nil {
		return "", err
	}
	var state struct {
		Profile struct {
			InfoCache map[string]struct {
				Name       string `json:"name"`
				AvatarIcon string `json:"avatar_icon"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(payload, &state); err != nil {
		return "", err
	}
	const prefix = "chrome://theme/IDR_PROFILE_AVATAR_"
	for _, profile := range state.Profile.InfoCache {
		if strings.TrimSpace(profile.Name) != identityName || !strings.HasPrefix(profile.AvatarIcon, prefix) {
			continue
		}
		filename := egoLiteAvatarFiles[strings.TrimPrefix(profile.AvatarIcon, prefix)]
		if filename == "" {
			break
		}
		path := filepath.Join(root, "Avatars", filename)
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			break
		}
		return path, nil
	}
	return "", os.ErrNotExist
}

func (s *Server) handleAPIEgoDoctor(w http.ResponseWriter, r *http.Request) {
	if s.egoRuntime == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_RUNTIME_UNAVAILABLE"})
		return
	}
	result, err := s.egoRuntime.Doctor(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error(), "result": result})
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleAPIEgoIdentities(w http.ResponseWriter, _ *http.Request) {
	identities, err := s.db.ListEgoIdentities()
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "EGO_IDENTITIES_LIST_FAILED"})
		return
	}
	type identityResponse struct {
		db.EgoIdentity
		AvatarURL string `json:"avatar_url,omitempty"`
	}
	responses := make([]identityResponse, 0, len(identities))
	profileRoot := defaultEgoLiteProfileRoot()
	for _, identity := range identities {
		response := identityResponse{EgoIdentity: identity}
		if identity.Avatar != "" {
			if strings.HasPrefix(identity.Avatar, "http://") || strings.HasPrefix(identity.Avatar, "https://") {
				response.AvatarURL = identity.Avatar
			} else {
				response.AvatarURL = "/api/ego/identities/" + strconv.FormatInt(identity.ID, 10) + "/avatar"
			}
		} else if _, avatarErr := egoLiteProfileAvatarPath(profileRoot, identity.Name); avatarErr == nil {
			response.AvatarURL = "/api/ego/identities/" + strconv.FormatInt(identity.ID, 10) + "/avatar"
		}
		responses = append(responses, response)
	}
	writeJSON(w, responses)
}

func (s *Server) handleAPIEgoIdentityAvatar(w http.ResponseWriter, r *http.Request) {
	identityID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || identityID <= 0 {
		http.NotFound(w, r)
		return
	}
	identity, err := s.db.GetEgoIdentity(identityID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// 优先使用绑定在 identity 表中的本地头像资产
	if identity.Avatar != "" && !strings.HasPrefix(identity.Avatar, "http://") && !strings.HasPrefix(identity.Avatar, "https://") {
		home, _ := os.UserHomeDir()
		fullPath := filepath.Join(home, ".alpha-nexus-egolite", identity.Avatar)
		if info, statErr := os.Stat(fullPath); statErr == nil && info.Mode().IsRegular() {
			w.Header().Set("Cache-Control", "private, no-cache")
			http.ServeFile(w, r, fullPath)
			return
		}
	}
	path, err := egoLiteProfileAvatarPath(defaultEgoLiteProfileRoot(), identity.Name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeFile(w, r, path)
}

func (s *Server) handleAPIEgoIdentityCreate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_REQUEST"})
		return
	}
	identity, err := s.db.CreateEgoIdentity(input.Name)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "EGO_IDENTITY_INVALID"})
		return
	}
	writeJSONStatus(w, http.StatusCreated, identity)
}

func (s *Server) handleAPIEgoIdentityRename(w http.ResponseWriter, r *http.Request) {
	identityID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || identityID <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_IDENTITY_ID"})
		return
	}
	var input struct {
		Name *string `json:"name"`
		Sort *int    `json:"sort"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_REQUEST"})
		return
	}
	if input.Name == nil && input.Sort == nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_REQUEST"})
		return
	}
	current, err := s.db.GetEgoIdentity(identityID)
	if err != nil {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "EGO_IDENTITY_NOT_FOUND"})
		return
	}
	name, sortOrder := current.Name, current.Sort
	if input.Name != nil {
		name = *input.Name
	}
	if input.Sort != nil {
		sortOrder = *input.Sort
	}
	identity, err := s.db.UpdateEgoIdentity(identityID, name, sortOrder)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "EGO_IDENTITY_INVALID"})
		return
	}
	writeJSON(w, identity)
}

func (s *Server) handleAPIEgoIdentityPlatforms(w http.ResponseWriter, r *http.Request) {
	identityID, ok := egoIdentityID(w, r)
	if !ok {
		return
	}
	var input struct {
		PlatformCodes         json.RawMessage `json:"platform_codes"`
		ExpectedPlatformCodes json.RawMessage `json:"expected_platform_codes"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_REQUEST"})
		return
	}
	platformCodes, err := parseRequiredEgoPlatformCodes(input.PlatformCodes)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "EGO_PLATFORM_CONFIG_INVALID"})
		return
	}
	expected, expectedPresent, err := parseExpectedEgoPlatformCodes(input.ExpectedPlatformCodes)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "EGO_PLATFORM_CONFIG_INVALID"})
		return
	}
	var accounts []db.Account
	if expectedPresent {
		accounts, err = s.db.ConfigureIdentityPlatformsIfExpected(identityID, platformCodes, expected)
	} else {
		accounts, err = s.db.ConfigureIdentityPlatforms(identityID, platformCodes)
	}
	if err != nil {
		if stderrors.Is(err, db.ErrEgoPlatformConfigurationConflict) {
			writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "EGO_PLATFORM_CONFIGURATION_CONFLICT"})
			return
		}
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "EGO_PLATFORM_CONFIG_INVALID"})
		return
	}
	if s.egoBookmarks != nil {
		_, _ = s.egoBookmarks.ImportForIdentity(r.Context(), identityID)
	}
	writeJSON(w, accounts)
}

func parseRequiredEgoPlatformCodes(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("platform_codes must be an array")
	}
	var codes []string
	if err := json.Unmarshal(raw, &codes); err != nil || codes == nil {
		return nil, fmt.Errorf("platform_codes must be an array")
	}
	return codes, nil
}

func parseExpectedEgoPlatformCodes(raw json.RawMessage) ([]string, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, fmt.Errorf("expected_platform_codes must be an array")
	}
	var codes []string
	if err := json.Unmarshal(raw, &codes); err != nil || codes == nil {
		return nil, true, fmt.Errorf("expected_platform_codes must be an array")
	}
	return codes, true, nil
}

func (s *Server) handleAPIEgoLoginBatch(w http.ResponseWriter, r *http.Request) {
	identityID, ok := egoIdentityID(w, r)
	if !ok {
		return
	}
	var input struct {
		PlatformCodes []string `json:"platform_codes"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_REQUEST"})
			return
		}
	}
	if len(input.PlatformCodes) == 0 {
		accounts, err := s.db.ListAccountsByIdentity(identityID)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "EGO_ACCOUNTS_LIST_FAILED"})
			return
		}
		for _, account := range accounts {
			if account.Enabled && account.LoginState != "logged_in" {
				input.PlatformCodes = append(input.PlatformCodes, account.PlatformCode)
			}
		}
	}
	if s.egoSpaces == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_RUNTIME_UNAVAILABLE"})
		return
	}
	if !s.loginOperationMu.TryLock() {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "EGO_LOGIN_OPERATION_IN_PROGRESS"})
		return
	}
	batch, err := s.egoSpaces.OpenLoginBatch(r.Context(), identityID, input.PlatformCodes)
	var sessionErr error
	if err == nil {
		sessionErr = s.ensureLoginVerificationSessions(identityID, batch.PlatformCodes)
	}
	s.loginOperationMu.Unlock()
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	if sessionErr != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "EGO_LOGIN_SESSION_CREATE_FAILED"})
		return
	}
	if s.loginSync != nil {
		s.loginSync.Wake()
	}
	writeJSON(w, batch)
}

func (s *Server) handleAPIEgoIdentityOpen(w http.ResponseWriter, r *http.Request) {
	identityID, ok := egoIdentityID(w, r)
	if !ok {
		return
	}
	var input struct {
		Offset        int      `json:"offset"`
		PlatformCodes []string `json:"platform_codes,omitempty"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || input.Offset < 0 {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_REQUEST"})
			return
		}
	}
	if s.egoSpaces == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_RUNTIME_UNAVAILABLE"})
		return
	}
	s.loginOperationMu.Lock()
	var result *egolite.BrowserOpenResult
	var err error
	if len(input.PlatformCodes) > 0 {
		result, err = s.egoSpaces.OpenLoggedInPlatformsForCodes(r.Context(), identityID, input.Offset, input.PlatformCodes)
	} else {
		result, err = s.egoSpaces.OpenLoggedInPlatforms(r.Context(), identityID, input.Offset)
	}
	s.loginOperationMu.Unlock()
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleAPIEgoLoginVerify(w http.ResponseWriter, r *http.Request) {
	identityID, ok := egoIdentityID(w, r)
	if !ok {
		return
	}
	var input struct {
		PlatformCodes []string `json:"platform_codes"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || len(input.PlatformCodes) == 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_REQUEST"})
		return
	}
	if s.egoSpaces == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_RUNTIME_UNAVAILABLE"})
		return
	}
	var result *egolite.LoginVerification
	var err error
	if s.loginSync != nil {
		result, err = s.loginSync.VerifyLoginBatch(r.Context(), identityID, input.PlatformCodes)
	} else {
		result, err = s.egoSpaces.VerifyLoginBatch(r.Context(), identityID, input.PlatformCodes)
	}
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	accounts := make([]map[string]any, 0, len(result.Accounts))
	for _, account := range result.Accounts {
		if account.LoginState == db.AccountAuthLoggedIn {
			s.completeActiveLoginSession(account.ID, "verified", "")
		}
		accounts = append(accounts, map[string]any{
			"id": account.ID, "identityId": account.IdentityID,
			"platformCode": account.PlatformCode, "name": account.Name,
			"avatar": account.Avatar, "accountIdentifier": account.AccountIdentifier,
			"loginState": account.LoginState, "lastCheckAt": account.LastCheckAt,
		})
	}
	writeJSON(w, map[string]any{"identity_id": result.IdentityID, "state": result.State, "accounts": accounts})
}

func egoIdentityID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	identityID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || identityID <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_IDENTITY_ID"})
		return 0, false
	}
	return identityID, true
}

func (s *Server) handleAPIEgoSpaces(w http.ResponseWriter, _ *http.Request) {
	spaces, err := s.db.ListEgoSpaces()
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "EGO_SPACES_LIST_FAILED"})
		return
	}
	writeJSON(w, spaces)
}

// handleAPIDesktopScreen captures this Mac's screen for the 屏幕 tab. It is the
// one path that needs macOS Screen Recording (TCC); when that permission is
// missing the command fails and the reply carries the exact error plus where to
// grant it, instead of pretending there is no capture API.
func (s *Server) handleAPIDesktopScreen(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if runtime.GOOS != "darwin" {
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{
			"error": "SCREEN_CAPTURE_UNSUPPORTED",
			"hint":  "桌面截屏目前只在 macOS 上实现",
		})
		return
	}
	dir := filepath.Join(s.db.DataDir, "screenshots", "desktop")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "SCREEN_CAPTURE_DIR_FAILED"})
		return
	}
	target := filepath.Join(dir, fmt.Sprintf("desktop-%d.jpg", time.Now().UnixNano()))
	command := exec.CommandContext(r.Context(), "/usr/sbin/screencapture", "-x", "-t", "jpg", target)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		_ = os.Remove(target)
		// "could not create image from display" is how macOS reports a missing
		// Screen Recording grant (no TCC preflight exists for it).
		if strings.Contains(strings.ToLower(message), "could not create image") ||
			strings.Contains(strings.ToLower(message), "not authorized") ||
			strings.Contains(strings.ToLower(message), "denied") {
			executable, _ := os.Executable()
			writeJSONStatus(w, http.StatusForbidden, map[string]any{
				"error":        "SCREEN_PERMISSION_DENIED",
				"hint":         "系统设置 → 隐私与安全性 → 屏幕录制：点「+」选择本应用，或勾选运行它的终端；授权后画面会自动出现",
				"binary_path":  executable,
				"settings_url": "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
			})
			return
		}
		writeJSONStatus(w, http.StatusConflict, map[string]any{
			"error": "SCREEN_CAPTURE_FAILED",
			"hint":  message,
		})
		return
	}
	raw, err := os.ReadFile(target)
	_ = os.Remove(target)
	if err != nil || len(raw) == 0 {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "SCREEN_CAPTURE_EMPTY"})
		return
	}
	writeJSON(w, map[string]any{
		"png":         shrinkScreenFrameJPEG(base64.StdEncoding.EncodeToString(raw)),
		"mime":        "image/jpeg",
		"captured_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// handleAPIDesktopScreenReveal shows the running binary in Finder. macOS's
// Screen Recording pane is granted through its "+" button, so the user needs to
// pick this exact file; revealing it beats spelling out a hidden path.
func (s *Server) handleAPIDesktopScreenReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "EGO_METHOD_NOT_ALLOWED"})
		return
	}
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if runtime.GOOS != "darwin" {
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{"error": "SCREEN_CAPTURE_UNSUPPORTED"})
		return
	}
	executable, err := os.Executable()
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "SCREEN_BINARY_UNKNOWN"})
		return
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	if output, err := exec.CommandContext(r.Context(), "/usr/bin/open", "-R", executable).CombinedOutput(); err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "SCREEN_REVEAL_FAILED", "hint": strings.TrimSpace(string(output))})
		return
	}
	writeJSON(w, map[string]any{"revealed": true, "path": executable})
}

// handleAPIDesktopScreenPermission opens the macOS Screen Recording pane. It is
// the only way to move the user from "permission denied" to a working capture:
// browsers refuse the x-apple.systempreferences: scheme, so the shell does it.
func (s *Server) handleAPIDesktopScreenPermission(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if runtime.GOOS != "darwin" {
		writeJSONStatus(w, http.StatusNotImplemented, map[string]any{"error": "SCREEN_CAPTURE_UNSUPPORTED"})
		return
	}
	if r.Method == http.MethodGet {
		// Preflight only: never triggers a prompt.
		writeJSON(w, map[string]any{"granted": screenCaptureGranted()})
		return
	}
	if r.Method != http.MethodPost {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "EGO_METHOD_NOT_ALLOWED"})
		return
	}
	if screenCaptureGranted() {
		writeJSON(w, map[string]any{"granted": true, "prompted": false, "opened": false})
		return
	}
	if screenPreflightAvailable() {
		// Ask macOS itself: this is what registers the binary in the Screen
		// Recording list. The call blocks until the user answers, so the HTTP
		// reply goes first; if consent is still missing afterwards, the settings
		// pane opens so the user has a manual path.
		go func() {
			if requestScreenCaptureGrant() {
				return
			}
			_ = exec.Command("/usr/bin/open", "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture").Run()
		}()
		writeJSON(w, map[string]any{"granted": false, "prompted": true, "opened": false})
		return
	}
	command := exec.CommandContext(r.Context(), "/usr/bin/open", "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture")
	if output, err := command.CombinedOutput(); err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "SCREEN_SETTINGS_OPEN_FAILED", "hint": strings.TrimSpace(string(output))})
		return
	}
	writeJSON(w, map[string]any{"granted": false, "prompted": false, "opened": true})
}

// egoSpaceTab is one real tab inside an Ego Space, so the UI can mirror the
// browser instead of drawing its own list.
type egoSpaceTab struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type egoSpaceTabs struct {
	SpaceID        int64         `json:"space_id"`
	Ownership      string        `json:"ownership"`
	UserControlled bool          `json:"user_controlled"`
	Current        string        `json:"current"`
	Tabs           []egoSpaceTab `json:"tabs"`
}

func (s *Server) runSpaceTabsProgram(ctx context.Context, spaceID int64, activate string) (egoSpaceTabs, error) {
	activateJSON, _ := json.Marshal(strings.TrimSpace(activate))
	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const target = spaces.find((item) => Number(item.id) === %d)
if (!target) throw new Error("EGO_SPACE_NOT_FOUND")
const ownership = String(target.ownership ?? "")
if (ownership === "user" || ownership === "agentDelegatedToUser") {
  console.log(%q + JSON.stringify({ space_id: %d, ownership, user_controlled: true, current: "", tabs: [] }))
} else {
  await useOrCreateTaskSpace(%d)
  const activate = %s
  if (activate) await switchTab(activate)
  const tabs = await listTabs()
  const current = await currentTab()
  const cur = String(current?.targetId ?? current?.id ?? "")
  const rows = tabs.map((tab) => ({ id: String(tab.targetId ?? tab.id ?? ""), title: String(tab.title ?? ""), url: String(tab.url ?? "") }))
  console.log(%q + JSON.stringify({ space_id: %d, ownership, user_controlled: false, current: cur, tabs: rows }))
}
`, spaceID, egoScreenResultPrefix, spaceID, spaceID, string(activateJSON), egoScreenResultPrefix, spaceID)

	var out egoSpaceTabs
	if err := s.egoRuntime.Run(ctx, program, &out); err != nil {
		return egoSpaceTabs{}, err
	}
	if out.Tabs == nil {
		out.Tabs = []egoSpaceTab{}
	}
	return out, nil
}

// handleAPIEgoSpaceTabs lists (GET) or activates (POST) a real tab inside a
// Space. Read-only by default; activating still never takes ownership back.
func (s *Server) handleAPIEgoSpaceTabs(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	spaceID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || spaceID <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "EGO_SPACE_ID_INVALID"})
		return
	}
	if s.egoRuntime == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_RUNTIME_UNAVAILABLE"})
		return
	}
	activate := ""
	if r.Method == http.MethodPost {
		var body struct {
			TabID string `json:"tab_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		activate = strings.TrimSpace(body.TabID)
		if activate == "" {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "EGO_TAB_ID_REQUIRED"})
			return
		}
	} else if r.Method != http.MethodGet {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "EGO_METHOD_NOT_ALLOWED"})
		return
	}
	tabs, err := s.runSpaceTabsProgram(r.Context(), spaceID, activate)
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, tabs)
}

// egoLiveSpace is one Space as EgoLite actually has it right now. The DB copy
// can outlive a closed Space, so the UI reads this instead of stale bindings.
type egoLiveSpace struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Ownership string `json:"ownership"`
	State     string `json:"state"`
	Current   string `json:"current"`
}

func (s *Server) handleAPIEgoSpacesLive(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	if s.egoRuntime == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_RUNTIME_UNAVAILABLE"})
		return
	}
	program := `const spaces = await listTaskSpaces()
const rows = spaces.map((item) => ({ id: Number(item.id ?? item.spaceId ?? 0), name: String(item.name ?? ""), ownership: String(item.ownership ?? ""), state: String(item.state ?? "") }))
console.log(` + "`" + egoScreenResultPrefix + "`" + ` + JSON.stringify(rows))`
	var rows []egoLiveSpace
	if err := s.egoRuntime.Run(r.Context(), program, &rows); err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []egoLiveSpace{}
	}
	writeJSON(w, map[string]any{"spaces": rows})
}

// egoScreenResultPrefix mirrors egolite.resultPrefix (unexported there). The
// runtime prints one line with this prefix and the JSON payload after it.
const egoScreenResultPrefix = "ALPHA_EGO_RESULT:"

// egoScreenFrameTTL bounds how stale a preview frame may be. Polling is a UI
// concern; this cache only stops two tabs from spawning two node runtimes.
const egoScreenFrameTTL = 2500 * time.Millisecond

// egoScreenFrame is what GET /api/ego/spaces/{id}/screen returns: a read-only
// JPEG frame of the Space's current page, or a user-controlled status without
// any image. The frame never crosses the process boundary as a file.
type egoScreenFrame struct {
	SpaceID        int64  `json:"space_id"`
	Ownership      string `json:"ownership"`
	UserControlled bool   `json:"user_controlled"`
	PNG            string `json:"png"`
	URL            string `json:"url"`
	Mime           string `json:"mime"`
	CapturedAt     string `json:"captured_at"`
}

type egoScreenCacheEntry struct {
	frame egoScreenFrame
	at    time.Time
}

// handleAPIEgoSpaceScreen captures the current page of one Ego Space for the
// live preview. It is strictly read-only:
//   - it never calls takeOverTaskSpace, so a Space the user controls is never
//     stolen back; the reply says user_controlled instead of an image;
//   - it only screenshots Spaces the agent already owns;
//   - a short single-flight cache prevents one node runtime spawn per poll.
func (s *Server) handleAPIEgoSpaceScreen(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalTerminal(w, r) {
		return
	}
	spaceID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || spaceID <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "EGO_SPACE_ID_INVALID"})
		return
	}
	if s.egoRuntime == nil || s.egoSpaces == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_RUNTIME_UNAVAILABLE"})
		return
	}

	// Serve a recent frame without taking the capture lock again.
	s.egoScreenMu.Lock()
	if entry, ok := s.egoScreen[spaceID]; ok && time.Since(entry.at) < egoScreenFrameTTL {
		s.egoScreenMu.Unlock()
		writeJSON(w, entry.frame)
		return
	}
	s.egoScreenMu.Unlock()

	// One capture at a time: Ego is serial per Space, and a second concurrent
	// node spawn would only queue behind the first while burning memory.
	s.egoScreenMu.Lock()
	defer s.egoScreenMu.Unlock()
	if entry, ok := s.egoScreen[spaceID]; ok && time.Since(entry.at) < egoScreenFrameTTL {
		writeJSON(w, entry.frame)
		return
	}

	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const target = spaces.find((item) => Number(item.id) === %d)
if (!target) throw new Error("EGO_SPACE_NOT_FOUND")
const ownership = String(target.ownership ?? "")
if (ownership === "user" || ownership === "agentDelegatedToUser") {
  console.log(%q + JSON.stringify({ space_id: %d, ownership, user_controlled: true, png: "", url: "" }))
} else {
  await useOrCreateTaskSpace(%d)
  const tab = await currentTab()
  const shot = await cdp("Page.captureScreenshot", { format: "jpeg", quality: 55, fromSurface: true, captureBeyondViewport: false })
  const data = typeof shot?.data === "string" ? shot.data : ""
  console.log(%q + JSON.stringify({ space_id: %d, ownership, user_controlled: false, png: data, url: String(tab?.url ?? ""), mime: "image/jpeg" }))
}
`, spaceID, egoScreenResultPrefix, spaceID, spaceID, egoScreenResultPrefix, spaceID)

	var frame egoScreenFrame
	if err := s.egoRuntime.Run(r.Context(), program, &frame); err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	if frame.UserControlled {
		// Read-only path never even attempts a capture for a user-held Space; the
		// user must click "take back control" (the separate endpoint) first.
		frame.PNG = ""
	}
	frame.SpaceID = spaceID
	if frame.Mime == "" && frame.PNG != "" {
		frame.Mime = "image/jpeg"
	}
	frame.CapturedAt = time.Now().UTC().Format(time.RFC3339Nano)
	// Only JPEG goes through the Rust filter; anything else keeps its original.
	if frame.PNG != "" && (frame.Mime == "" || frame.Mime == "image/jpeg") {
		frame.PNG = shrinkScreenFrameJPEG(frame.PNG)
	}
	if s.egoScreen == nil {
		s.egoScreen = make(map[int64]egoScreenCacheEntry)
	}
	s.egoScreen[spaceID] = egoScreenCacheEntry{frame: frame, at: time.Now()}
	writeJSON(w, frame)
}

// handleAPIEgoSpaceScreenTakeover runs only when the user explicitly clicks
// "take back control". It is the single place allowed to call
// takeOverTaskSpace, and it exists because the read-only preview must never
// steal a Space on its own.
func (s *Server) handleAPIEgoSpaceScreenTakeover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "EGO_METHOD_NOT_ALLOWED"})
		return
	}
	if !s.requireLocalTerminal(w, r) {
		return
	}
	spaceID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || spaceID <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "EGO_SPACE_ID_INVALID"})
		return
	}
	if s.egoRuntime == nil || s.egoSpaces == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_RUNTIME_UNAVAILABLE"})
		return
	}
	// Explicit user action: take control back, then grab one frame.
	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const target = spaces.find((item) => Number(item.id) === %d)
if (!target) throw new Error("EGO_SPACE_NOT_FOUND")
let claimed = null
try {
  claimed = await takeOverTaskSpace(%d)
} catch (e) {
  claimed = { error_code: String(e) }
}
await useOrCreateTaskSpace(%d)
const tab = await currentTab()
const shot = await cdp("Page.captureScreenshot", { format: "jpeg", quality: 55, fromSurface: true, captureBeyondViewport: false })
const data = typeof shot?.data === "string" ? shot.data : ""
console.log(%q + JSON.stringify({ space_id: %d, ownership: "agent", user_controlled: false, png: data, url: String(tab?.url ?? ""), mime: "image/jpeg", claimed: Boolean(claimed && !claimed.error_code) }))
`, spaceID, spaceID, spaceID, egoScreenResultPrefix, spaceID)

	var frame egoScreenFrame
	if err := s.egoRuntime.Run(r.Context(), program, &frame); err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	frame.SpaceID = spaceID
	if frame.Mime == "" && frame.PNG != "" {
		frame.Mime = "image/jpeg"
	}
	frame.CapturedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.egoScreenMu.Lock()
	if s.egoScreen == nil {
		s.egoScreen = make(map[int64]egoScreenCacheEntry)
	}
	s.egoScreen[spaceID] = egoScreenCacheEntry{frame: frame, at: time.Now()}
	s.egoScreenMu.Unlock()
	writeJSON(w, frame)
}

func (s *Server) handleAPIEgoBookmarksImport(w http.ResponseWriter, r *http.Request) {
	if s.egoBookmarks == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_BOOKMARK_IMPORT_UNAVAILABLE"})
		return
	}
	result, err := s.egoBookmarks.Import(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleAPIEgoCalibrate(w http.ResponseWriter, r *http.Request) {
	if s.calibrateEgo == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_CALIBRATION_UNAVAILABLE"})
		return
	}
	result, err := s.calibrateEgo(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	s.mu.Lock()
	s.cfg.MeasuredSpaceCapacity = result.MeasuredCapacity
	s.cfg.MaxConcurrentSpaces = result.DefaultConcurrency
	// toolRuntime 持有的是启动时拷进去的值，只改 s.cfg 不影响真正吃并发的那个。
	if s.toolRuntime != nil {
		s.toolRuntime.MaxConcurrentSpaces = result.DefaultConcurrency
	}
	s.mu.Unlock()

	// 不写回 config.json 的话，刷新页面或重启就回到默认值——
	// 用户会看到「校准成功」但设置根本没保存。
	dataDir, dirErr := s.cfg.ResolvedDataDir()
	if dirErr == nil {
		_, _ = s.updatePersistedConfigLocked(dataDir, func(values map[string]any) {
			values["max_concurrent_spaces"] = result.DefaultConcurrency
			values["measured_space_capacity"] = result.MeasuredCapacity
		})
	}
	writeJSON(w, result)
}

func (s *Server) handleAPIEgoSessionMonitorGet(w http.ResponseWriter, _ *http.Request) {
	settings, err := s.db.EgoSessionMonitorSettings()
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "SESSION_MONITOR_SETTINGS_FAILED"})
		return
	}
	writeJSON(w, settings)
}

func (s *Server) handleAPIEgoSessionMonitorPut(w http.ResponseWriter, r *http.Request) {
	current, err := s.db.EgoSessionMonitorSettings()
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "SESSION_MONITOR_SETTINGS_FAILED"})
		return
	}
	var input struct {
		Enabled         *bool `json:"enabled"`
		IntervalMinutes *int  `json:"interval_minutes"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || (input.Enabled == nil && input.IntervalMinutes == nil) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_REQUEST"})
		return
	}
	enabled, interval := current.Enabled, current.IntervalMinutes
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if input.IntervalMinutes != nil {
		interval = *input.IntervalMinutes
	}
	updated, err := s.db.UpdateEgoSessionMonitor(enabled, interval)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "SESSION_MONITOR_SETTINGS_INVALID"})
		return
	}
	if s.sessionMonitor != nil {
		s.sessionMonitor.Wake()
	}
	writeJSON(w, updated)
}

func (s *Server) handleAPIEgoSessionMonitorRun(w http.ResponseWriter, r *http.Request) {
	if s.sessionMonitor == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "SESSION_MONITOR_UNAVAILABLE"})
		return
	}
	result, err := s.sessionMonitor.Run(r.Context())
	if err != nil {
		status := http.StatusInternalServerError
		if stderrors.Is(err, db.ErrEgoSessionMonitorRunning) {
			status = http.StatusConflict
		}
		writeJSONStatus(w, status, map[string]any{"error": "SESSION_MONITOR_RUN_FAILED"})
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleAPIEgoSpaceEnsure(w http.ResponseWriter, r *http.Request) {
	s.runEgoSpaceAction(w, r, "ensure")
	// A user-initiated re-bind is the explicit "retry now" the metrics
	// dashboard offers after EGO_SPACE_NOT_FOUND, so resume a waiting sweep.
	if s.metricsScheduler != nil {
		s.metricsScheduler.Wake()
	}
}

func (s *Server) handleAPIEgoSpaceLogin(w http.ResponseWriter, r *http.Request) {
	s.runEgoSpaceAction(w, r, "login")
}

func (s *Server) handleAPIEgoFocus(w http.ResponseWriter, r *http.Request) {
	if s.focusEgoLite == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_APP_FOCUS_UNAVAILABLE"})
		return
	}
	if err := s.focusEgoLite(r.Context()); err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleAPIEgoLoginQR(w http.ResponseWriter, r *http.Request) {
	accountID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || accountID <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_ACCOUNT_ID"})
		return
	}
	if s.egoSpaces == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_RUNTIME_UNAVAILABLE"})
		return
	}
	var input struct {
		Refresh  bool `json:"refresh"`
		Navigate bool `json:"navigate"`
		Release  bool `json:"release"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_REQUEST"})
			return
		}
	}
	var result *egolite.LoginQR
	if !s.loginOperationMu.TryLock() {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "EGO_LOGIN_OPERATION_IN_PROGRESS"})
		return
	}
	if input.Release {
		result, err = s.egoSpaces.ReleaseLoginQR(r.Context(), accountID)
	} else {
		if _, err := s.ensureActiveLoginSession(accountID); err != nil {
			s.loginOperationMu.Unlock()
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "EGO_LOGIN_SESSION_CREATE_FAILED"})
			return
		}
		result, err = s.egoSpaces.OpenLoginQR(r.Context(), accountID, !input.Refresh || input.Navigate)
	}
	s.loginOperationMu.Unlock()
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	if input.Release {
		// Closing the Electron QR dialog only returns the Space to the user.
		// Keep the bounded login session alive so verification continues while
		// the user finishes QR, password, or authorization steps inside EgoLite.
	}
	if !input.Release && s.loginSync != nil {
		s.loginSync.Wake()
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writeJSON(w, result)
}

func (s *Server) ensureActiveLoginSession(accountID int64) (*db.LoginSession, error) {
	session, err := s.db.ActiveLoginSession(accountID)
	if err != nil {
		return nil, err
	}
	if session != nil {
		expiresAt, parseErr := time.Parse(time.RFC3339, session.ExpiresAt)
		if parseErr == nil && expiresAt.After(time.Now().UTC()) {
			return session, nil
		}
		_ = s.db.UpdateLoginSession(session.ID, "cancelled", "LOGIN_SESSION_EXPIRED")
	}
	session, err = s.db.CreateLoginSession(accountID)
	if err != nil {
		return nil, err
	}
	if err := s.db.UpdateLoginSession(session.ID, "awaiting_login", ""); err != nil {
		return nil, err
	}
	session.Status = "awaiting_login"
	return session, nil
}

func (s *Server) ensureLoginVerificationSessions(identityID int64, platformCodes []string) error {
	accounts, err := s.db.ListAccountsByIdentity(identityID)
	if err != nil {
		return err
	}
	byPlatform := make(map[string]int64, len(accounts))
	for _, account := range accounts {
		byPlatform[account.PlatformCode] = account.ID
	}
	for _, platformCode := range platformCodes {
		accountID := byPlatform[platformCode]
		if accountID <= 0 {
			continue
		}
		if _, err := s.ensureActiveLoginSession(accountID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleAPIEgoSpaceResume(w http.ResponseWriter, r *http.Request) {
	s.runEgoSpaceAction(w, r, "resume")
}

func (s *Server) runEgoSpaceAction(w http.ResponseWriter, r *http.Request, action string) {
	accountID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || accountID <= 0 {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_ACCOUNT_ID"})
		return
	}
	if s.egoSpaces == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_RUNTIME_UNAVAILABLE"})
		return
	}
	var space any
	switch action {
	case "ensure":
		space, err = s.egoSpaces.EnsureAccountSpace(r.Context(), accountID)
	case "login":
		space, err = s.egoSpaces.OpenLoginAndHandoff(r.Context(), accountID)
	case "resume":
		space, err = s.egoSpaces.ResumeAfterConfirmation(r.Context(), accountID)
	default:
		err = strconv.ErrSyntax
	}
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, space)
}

// handleAPIEgoProfiles lists the read-only ego-lite profile library. It degrades
// to available:false instead of 500 so the UI can hide the panel when ego-lite
// is absent or unresponsive.
func (s *Server) handleAPIEgoProfiles(w http.ResponseWriter, _ *http.Request) {
	profiles, err := s.egoSpaces.ListProfiles(context.Background())
	if err != nil {
		writeJSON(w, map[string]any{
			"available": false,
			"profiles":  []any{},
			"error":     err.Error(),
		})
		return
	}
	writeJSON(w, map[string]any{
		"available": true,
		"profiles":  profiles,
	})
}

// handleAPIEgoIdentityProfile binds a Matrix identity to one concrete ego-lite
// profile by ID. Binding is metadata only: it never opens the profile or reads
// its contents.
func (s *Server) handleAPIEgoIdentityProfile(w http.ResponseWriter, r *http.Request) {
	identityID, ok := egoIdentityID(w, r)
	if !ok {
		return
	}
	var payload struct {
		ProfileID   string `json:"profileId"`
		ProfileName string `json:"profileName"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&payload); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "INVALID_BODY"})
		return
	}
	payload.ProfileID = strings.TrimSpace(payload.ProfileID)
	if payload.ProfileID == "" {
		if err := s.db.ClearEgoIdentityProfile(identityID); err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "EGO_PROFILE_CLEAR_FAILED"})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "profileId": ""})
		return
	}
	err := s.db.SetEgoIdentityProfile(identityID, payload.ProfileID, payload.ProfileName)
	if err != nil {
		if stderrors.Is(err, db.ErrEgoProfileTaken) {
			writeJSONStatus(w, http.StatusConflict, map[string]any{"error": "EGO_PROFILE_TAKEN", "message": err.Error()})
			return
		}
		if stderrors.Is(err, sql.ErrNoRows) {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "IDENTITY_NOT_FOUND"})
			return
		}
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "EGO_PROFILE_BIND_FAILED"})
		return
	}
	identity, err := s.db.GetEgoIdentity(identityID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "EGO_IDENTITY_READ_FAILED"})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "identity": identity})
}

// handleAPIEgoIdentityVerify refreshes every configured platform of an identity
// without requiring the caller to enumerate platform_codes. Results are written
// back to accounts.login_state by the shared verifier.
func (s *Server) handleAPIEgoIdentityVerify(w http.ResponseWriter, r *http.Request) {
	identityID, ok := egoIdentityID(w, r)
	if !ok {
		return
	}
	if s.egoSpaces == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "EGO_RUNTIME_UNAVAILABLE"})
		return
	}
	accounts, err := s.db.ListAccountsByIdentity(identityID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "EGO_ACCOUNTS_READ_FAILED"})
		return
	}
	seen := make(map[string]bool)
	codes := make([]string, 0, len(accounts))
	for _, account := range accounts {
		code := strings.TrimSpace(account.PlatformCode)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		writeJSON(w, map[string]any{
			"identity_id": identityID, "state": "empty", "accounts": []any{},
		})
		return
	}
	var result *egolite.LoginVerification
	if s.loginSync != nil {
		result, err = s.loginSync.VerifyLoginBatch(r.Context(), identityID, codes)
	} else {
		result, err = s.egoSpaces.VerifyLoginBatch(r.Context(), identityID, codes)
	}
	if err != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(result.Accounts))
	for _, account := range result.Accounts {
		if account.LoginState == db.AccountAuthLoggedIn {
			s.completeActiveLoginSession(account.ID, "verified", "")
		}
		out = append(out, map[string]any{
			"id": account.ID, "identityId": account.IdentityID,
			"platformCode": account.PlatformCode, "name": account.Name,
			"accountIdentifier": account.AccountIdentifier,
			"loginState":        account.LoginState, "lastCheckAt": account.LastCheckAt,
		})
	}
	writeJSON(w, map[string]any{"identity_id": result.IdentityID, "state": result.State, "accounts": out})
}
