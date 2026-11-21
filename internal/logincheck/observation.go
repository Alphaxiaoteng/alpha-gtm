package logincheck

import (
	"net/url"
	"sort"
	"strings"

	"alpha-nexus-egolite/internal/selectors"
	cdptarget "github.com/chromedp/cdproto/target"
)

const (
	AuthLoggedIn  = "logged_in"
	AuthLoggedOut = "logged_out"
	AuthUnknown   = "unknown"

	ReasonVerifiedCookie         = "VERIFIED_COOKIE"
	ReasonVerifiedProfileDOM     = "VERIFIED_PROFILE_DOM"
	ReasonVerifiedSessionDOM     = "VERIFIED_SESSION_DOM"
	ReasonLoginRequiredPage      = "LOGIN_REQUIRED_PAGE"
	ReasonSessionExpiredPage     = "SESSION_EXPIRED_PAGE"
	ReasonCheckpointRequired     = "CHECKPOINT_REQUIRED"
	ReasonProfileSelectorMissing = "PROFILE_SELECTOR_MISSING"
	ReasonPlatformTabMissing     = "PLATFORM_TAB_MISSING"
	ReasonPlatformUnreachable    = "PLATFORM_UNREACHABLE"
	ReasonCDPUnreachable         = "CDP_UNREACHABLE"
	ReasonBrowserProcessExited   = "BROWSER_PROCESS_EXITED"
	ReasonManualVerifyRequired   = "MANUAL_VERIFY_REQUIRED"

	EvidenceReviewedCookie     = "REVIEWED_COOKIE_NAME"
	EvidenceReviewedProfileDOM = "REVIEWED_PROFILE_DOM"
	EvidenceReviewedSessionDOM = "REVIEWED_SESSION_DOM"
	EvidenceExplicitLoginDOM   = "EXPLICIT_LOGIN_DOM"
	EvidenceExplicitExpiredDOM = "EXPLICIT_EXPIRED_DOM"
	EvidenceCheckpointDOM      = "CHECKPOINT_DOM"
	EvidenceMissing            = "EVIDENCE_MISSING"

	AvatarErrorCaptureFailed = "AVATAR_CAPTURE_FAILED"
)

// Observation contains only bounded, non-secret evidence from an existing
// platform tab. Cookie values and full URLs are deliberately absent.
type Observation struct {
	AuthState         string `json:"authState"`
	ReasonCode        string `json:"reasonCode"`
	EvidenceCode      string `json:"evidenceCode"`
	Name              string `json:"name,omitempty"`
	AccountIdentifier string `json:"accountIdentifier,omitempty"`
	PageOrigin        string `json:"pageOrigin,omitempty"`
	AvatarPNG         []byte `json:"-"`
	AvatarErrorCode   string `json:"avatarErrorCode,omitempty"`
}

type domSnapshot struct {
	Name                  string `json:"name"`
	AccountIdentifier     string `json:"accountIdentifier"`
	AvatarSelector        string `json:"avatarSelector"`
	AvatarIndex           int    `json:"avatarIndex"`
	AvatarResourceURL     string `json:"avatarResourceUrl"`
	AvatarScreenshotReady bool   `json:"avatarScreenshotReady"`
	IdentityVerified      bool   `json:"identityVerified"`
	SessionVerified       bool   `json:"sessionVerified"`
	SessionProofCount     int    `json:"sessionProofCount"`
	LoginRequired         bool   `json:"loginRequired"`
	SessionExpired        bool   `json:"sessionExpired"`
	CheckpointRequired    bool   `json:"checkpointRequired"`
	PageURL               string `json:"pageURL"`
}

func classifyEvidence(platformCode string, cookieNames []string, snapshot domSnapshot) Observation {
	sessionVerified := snapshot.SessionVerified
	if rule, ok := profileRules[platformCode]; ok && rule.RequiredSessionProofs > 0 &&
		snapshot.SessionProofCount >= rule.RequiredSessionProofs {
		sessionVerified = true
	}
	result := Observation{
		AuthState:    AuthUnknown,
		ReasonCode:   ReasonProfileSelectorMissing,
		EvidenceCode: EvidenceMissing,
		PageOrigin:   sanitizePageOrigin(snapshot.PageURL),
	}
	if snapshot.IdentityVerified {
		result.Name = cleanProfileText(snapshot.Name, 80)
		result.AccountIdentifier = normalizeAccountIdentifier(platformCode, snapshot.AccountIdentifier)
	}
	if snapshot.CheckpointRequired {
		result.ReasonCode = ReasonCheckpointRequired
		result.EvidenceCode = EvidenceCheckpointDOM
		return result
	}
	// A verified account header and a simultaneous login/expiry component are
	// contradictory evidence. SPA overlays and account-switch dialogs can expose
	// both at once, so the observer must not persist a logout from that snapshot.
	if (snapshot.IdentityVerified || sessionVerified) && (snapshot.SessionExpired || snapshot.LoginRequired) {
		result.ReasonCode = ReasonManualVerifyRequired
		result.EvidenceCode = EvidenceMissing
		return result
	}
	if snapshot.SessionExpired {
		result.AuthState = AuthLoggedOut
		result.ReasonCode = ReasonSessionExpiredPage
		result.EvidenceCode = EvidenceExplicitExpiredDOM
		return result
	}
	if snapshot.LoginRequired {
		result.AuthState = AuthLoggedOut
		result.ReasonCode = ReasonLoginRequiredPage
		result.EvidenceCode = EvidenceExplicitLoginDOM
		return result
	}
	if snapshot.IdentityVerified {
		result.AuthState = AuthLoggedIn
		result.ReasonCode = ReasonVerifiedProfileDOM
		result.EvidenceCode = EvidenceReviewedProfileDOM
		return result
	}
	if sessionVerified {
		result.AuthState = AuthLoggedIn
		result.ReasonCode = ReasonVerifiedSessionDOM
		result.EvidenceCode = EvidenceReviewedSessionDOM
		return result
	}
	if catalog, err := selectors.Default(); err == nil && catalog.CookiesIndicateLogin(platformCode, cookieNames) {
		result.AuthState = AuthLoggedIn
		result.ReasonCode = ReasonVerifiedCookie
		result.EvidenceCode = EvidenceReviewedCookie
	}
	return result
}

// mergeObservations prefers positive evidence from any existing platform tab.
// A stale login tab must not override a simultaneously verified creator tab.
func mergeObservations(items []Observation) Observation {
	result := Observation{AuthState: AuthUnknown, ReasonCode: ReasonProfileSelectorMissing, EvidenceCode: EvidenceMissing}
	for index, item := range items {
		itemPriority := observationPriority(item)
		resultPriority := observationPriority(result)
		if index == 0 || itemPriority > resultPriority ||
			(itemPriority == resultPriority && observationIdentityStrength(item) > observationIdentityStrength(result)) {
			result = item
		}
	}
	return result
}

func mergeInspectedObservations(items []Observation, incomplete bool) Observation {
	merged := mergeObservations(items)
	if incomplete && merged.AuthState != AuthLoggedIn && merged.ReasonCode != ReasonCheckpointRequired {
		return Observation{
			AuthState: AuthUnknown, ReasonCode: ReasonManualVerifyRequired, EvidenceCode: EvidenceMissing,
			PageOrigin: merged.PageOrigin,
		}
	}
	return merged
}

func observationIdentityStrength(item Observation) int {
	strength := 0
	if item.ReasonCode == ReasonVerifiedProfileDOM {
		strength += 8
	}
	if item.Name != "" {
		strength += 4
	}
	if len(item.AvatarPNG) > 0 {
		strength += 2
	}
	if item.AccountIdentifier != "" {
		strength++
	}
	return strength
}

func observationPriority(item Observation) int {
	switch {
	case item.ReasonCode == ReasonCheckpointRequired:
		return 5
	case item.AuthState == AuthLoggedIn:
		return 4
	case item.ReasonCode == ReasonSessionExpiredPage:
		return 3
	case item.ReasonCode == ReasonLoginRequiredPage:
		return 2
	default:
		return 1
	}
}

func cleanProfileText(value string, max int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) > max {
		return ""
	}
	return value
}

func normalizeAccountIdentifier(platformCode, value string) string {
	value = cleanProfileText(value, 120)
	if platformCode != "xhs" {
		return value
	}
	for _, prefix := range []string{"小红书账号:", "小红书账号：", "小红书号:", "小红书号："} {
		if strings.HasPrefix(value, prefix) {
			return cleanProfileText(strings.TrimPrefix(value, prefix), 120)
		}
	}
	return value
}

func sanitizePageOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.RawPath = ""
	return parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
}

func selectPlatformTargets(targets []*cdptarget.Info, hosts, loginPaths []string) []*cdptarget.Info {
	priority := make(map[string]int, len(hosts))
	for index, host := range hosts {
		priority[strings.ToLower(host)] = index
	}
	type candidate struct {
		info      *cdptarget.Info
		hostScore int
		loginPath bool
	}
	candidates := make([]candidate, 0, len(targets))
	for _, info := range targets {
		if info == nil || info.Type != "page" {
			continue
		}
		parsed, err := url.Parse(info.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		matched := -1
		for reviewed, score := range priority {
			if host == reviewed || strings.HasSuffix(host, "."+reviewed) {
				if matched < 0 || score < matched {
					matched = score
				}
			}
		}
		if matched >= 0 {
			candidates = append(candidates, candidate{
				info: info, hostScore: matched, loginPath: pathMatchesPrefix(parsed.Path, loginPaths),
			})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].loginPath != candidates[j].loginPath {
			return !candidates[i].loginPath
		}
		return candidates[i].hostScore < candidates[j].hostScore
	})
	result := make([]*cdptarget.Info, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.info)
	}
	return result
}

func pathMatchesPrefix(path string, prefixes []string) bool {
	path = strings.ToLower(path)
	for _, prefix := range prefixes {
		prefix = strings.ToLower(strings.TrimSpace(prefix))
		if prefix != "" && strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func pageURLMatchesReviewedHosts(raw string, hosts []string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, reviewed := range hosts {
		reviewed = strings.ToLower(strings.TrimSpace(reviewed))
		if reviewed != "" && (host == reviewed || strings.HasSuffix(host, "."+reviewed)) {
			return true
		}
	}
	return false
}

func unreviewedSnapshotObservation(raw string) Observation {
	reason := ReasonPlatformTabMissing
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "chrome-error://") {
		reason = ReasonPlatformUnreachable
	}
	return Observation{AuthState: AuthUnknown, ReasonCode: reason, EvidenceCode: EvidenceMissing}
}
