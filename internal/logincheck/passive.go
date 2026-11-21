// Package logincheck verifies a running account browser without navigation.
package logincheck

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/selectors"
	"github.com/chromedp/cdproto/cdp"
	cdpio "github.com/chromedp/cdproto/io"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdptarget "github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	_ "image/jpeg"
)

type PassiveResult struct {
	LoggedIn          bool
	VerificationState string
	ReasonCode        string
	EvidenceCode      string
}

// VerifyPassive preserves the existing account-check contract while delegating
// to the richer read-only observation. Missing evidence is never logout proof.
func VerifyPassive(ctx context.Context, platformCode string, cdpPort int) (*PassiveResult, error) {
	observation, err := ObservePassive(ctx, platformCode, cdpPort)
	if err != nil {
		return nil, err
	}
	state := "not_verified"
	if observation.AuthState == AuthLoggedIn {
		state = "verified"
	} else if observation.AuthState == AuthLoggedOut {
		state = "login_required"
	} else if catalog, catalogErr := selectors.Default(); catalogErr == nil && !catalog.HasLoginCookieEvidence(platformCode) {
		state = "manual_verify"
	}
	return &PassiveResult{
		LoggedIn:          observation.AuthState == AuthLoggedIn,
		VerificationState: state,
		ReasonCode:        observation.ReasonCode,
		EvidenceCode:      observation.EvidenceCode,
	}, nil
}

// ObservePassive attaches to already-existing platform tabs, reads only
// reviewed cookie names and platform-specific DOM, and detaches without
// creating, navigating, focusing, or closing a tab.
func ObservePassive(ctx context.Context, platformCode string, cdpPort int) (*Observation, error) {
	return observePassive(ctx, platformCode, cdpPort, true, true)
}

// ObserveDOMPassive is the low-cost transition probe used by the local account
// monitor. It reads only reviewed DOM evidence and never captures an avatar or
// requests Cookie values. A state change is confirmed by ObservePassive before
// it is persisted.
func ObserveDOMPassive(ctx context.Context, platformCode string, cdpPort int) (*Observation, error) {
	return observePassive(ctx, platformCode, cdpPort, false, false)
}

func observePassive(ctx context.Context, platformCode string, cdpPort int, readCookies, captureAvatar bool) (*Observation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if cdpPort <= 0 {
		return nil, fmt.Errorf("CDP port is required")
	}
	if _, err := platforms.Find(platformCode); err != nil {
		return nil, fmt.Errorf("platform not found: %w", err)
	}
	catalog, err := selectors.Default()
	if err != nil {
		return nil, fmt.Errorf("load selector catalog: %w", err)
	}

	rule, hasProfileRule := profileRules[platformCode]
	if !hasProfileRule {
		rule.Hosts = hostsFromURLs(catalog.URLs(platformCode))
	}

	// Target ownership must not inherit an HTTP/batch cancellation: chromedp
	// closes an attached target when its owning context is cancelled. Caller
	// cancellation is applied only to action contexts below; cleanup first
	// clears target ownership, then actively cancels the tab context.
	ownershipCtx, ownershipCancel := context.WithCancel(context.Background())
	defer ownershipCancel()
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ownershipCtx, fmt.Sprintf("http://127.0.0.1:%d", cdpPort))
	defer allocCancel()
	// The first allocator call owns the remote websocket for its context
	// lifetime. Give it an independently cancellable context, and stop linking
	// caller/timeout cancellation immediately after target enumeration succeeds.
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()
	stopCaller := context.AfterFunc(ctx, browserCancel)
	allocationTimer := time.AfterFunc(10*time.Second, browserCancel)
	targets, err := chromedp.Targets(browserCtx)
	allocationTimer.Stop()
	stopCaller()
	if err != nil {
		return nil, fmt.Errorf("read CDP targets: %w", err)
	}
	candidates := selectPlatformTargets(targets, rule.Hosts, rule.LoginPaths)
	if len(candidates) == 0 {
		return &Observation{AuthState: AuthUnknown, ReasonCode: ReasonPlatformTabMissing, EvidenceCode: EvidenceMissing}, nil
	}
	maxTargets := platformTargetHardLimit
	if !readCookies && !captureAvatar {
		// The one-second monitor checks at most the first two reviewed pages so a
		// creator home tab cannot permanently hide an already-open editor session.
		// Explicit checks still inspect the complete bounded set.
		maxTargets = domProbeTargetLimit
	}
	items, incomplete, lastErr := inspectPlatformTargetsWithLimit(candidates, maxTargets, func(targetID cdptarget.ID) (*Observation, error) {
		return inspectExistingTarget(browserCtx, ctx, catalog, platformCode, rule, targetID, readCookies, captureAvatar)
	})
	if len(items) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return &Observation{AuthState: AuthUnknown, ReasonCode: ReasonManualVerifyRequired, EvidenceCode: EvidenceMissing}, nil
	}
	merged := mergeInspectedObservations(items, incomplete)
	if !hasProfileRule && merged.AuthState == AuthUnknown {
		merged.ReasonCode = ReasonManualVerifyRequired
	}
	return &merged, nil
}

const (
	platformTargetHardLimit = 6
	domProbeTargetLimit     = 2
)

func boundedPlatformTargets(candidates []*cdptarget.Info) []*cdptarget.Info {
	return boundedTargets(candidates, platformTargetHardLimit)
}

func boundedDOMProbeTargets(candidates []*cdptarget.Info) []*cdptarget.Info {
	return boundedTargets(candidates, domProbeTargetLimit)
}

func boundedTargets(candidates []*cdptarget.Info, limit int) []*cdptarget.Info {
	if limit > 0 && len(candidates) > limit {
		return candidates[:limit]
	}
	return candidates
}

func inspectPlatformTargets(candidates []*cdptarget.Info, inspect func(cdptarget.ID) (*Observation, error)) ([]Observation, bool, error) {
	return inspectPlatformTargetsWithLimit(candidates, platformTargetHardLimit, inspect)
}

func inspectPlatformTargetsWithLimit(candidates []*cdptarget.Info, limit int, inspect func(cdptarget.ID) (*Observation, error)) ([]Observation, bool, error) {
	selected := boundedTargets(candidates, limit)
	incomplete := len(candidates) > len(selected)
	items := make([]Observation, 0, len(selected))
	var lastErr error
	for _, candidate := range selected {
		item, err := inspect(candidate.TargetID)
		if err != nil {
			lastErr = err
			incomplete = true
			continue
		}
		if item == nil {
			lastErr = fmt.Errorf("target inspection returned no observation")
			incomplete = true
			continue
		}
		items = append(items, *item)
	}
	return items, incomplete, lastErr
}

func inspectExistingTarget(allocCtx, callerCtx context.Context, catalog *selectors.Catalog, platformCode string, rule platformProfileRule, targetID cdptarget.ID, readCookies, captureAvatar bool) (*Observation, error) {
	tabCtx, tabCancel := chromedp.NewContext(allocCtx, chromedp.WithTargetID(targetID))
	runCtx, runCancel := actionContext(tabCtx, callerCtx, 7*time.Second)
	defer func() {
		runCancel()
		detachExistingTarget(tabCtx, tabCancel)
	}()

	script, err := profileObservationScript(rule)
	if err != nil {
		return nil, err
	}
	var snapshot domSnapshot
	var cookies []*network.Cookie
	err = chromedp.Run(runCtx, chromedp.Evaluate(script, &snapshot))
	if err != nil {
		return nil, fmt.Errorf("inspect existing platform tab: %w", err)
	}
	// The target may have navigated after enumeration. Never accept identity,
	// login DOM, or an avatar unless the URL returned by the same Evaluate is
	// still one of this platform's reviewed creator hosts.
	if !pageURLMatchesReviewedHosts(snapshot.PageURL, rule.Hosts) {
		result := unreviewedSnapshotObservation(snapshot.PageURL)
		return &result, nil
	}
	if readCookies {
		err = chromedp.Run(runCtx, chromedp.ActionFunc(func(actionCtx context.Context) error {
			var actionErr error
			cookies, actionErr = network.GetCookies().WithURLs(catalog.URLs(platformCode)).Do(actionCtx)
			return actionErr
		}))
		if err != nil {
			return nil, fmt.Errorf("inspect reviewed cookie names: %w", err)
		}
	}

	// The protocol response includes complete Cookie objects. Only names are
	// copied into application memory; values are never inspected, returned, or
	// persisted.
	names := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie != nil {
			names = append(names, cookie.Name)
		}
	}
	result := classifyEvidence(platformCode, names, snapshot)
	if captureAvatar && (snapshot.IdentityVerified || snapshot.SessionVerified) && snapshot.AvatarSelector != "" {
		// Avatar capture is useful identity metadata, but it is not authentication
		// evidence. Record a stable warning while preserving the verified login.
		result.AvatarErrorCode = AvatarErrorCaptureFailed
		if snapshot.AvatarSelector != "" && snapshot.AvatarIndex >= 0 {
			jsPath := deepSelectorJSPath(snapshot.AvatarSelector, snapshot.AvatarIndex)
			var avatar []byte
			var currentURL string
			if snapshot.AvatarResourceURL != "" {
				if resourceAvatar, resourceErr := loadReviewedAvatarResourcePNG(tabCtx, callerCtx, snapshot.AvatarResourceURL, rule.AvatarResourceHostSuffixes); resourceErr == nil {
					result.AvatarPNG = resourceAvatar
					result.AvatarErrorCode = ""
				}
			}
			if screenshotErr := chromedp.Run(runCtx, chromedp.Location(&currentURL)); screenshotErr == nil &&
				len(result.AvatarPNG) == 0 && snapshot.AvatarScreenshotReady &&
				pageURLMatchesReviewedHosts(currentURL, rule.Hosts) {
				screenshotErr = chromedp.Run(runCtx, chromedp.Screenshot(jsPath, &avatar, chromedp.ByJSPath))
				if screenshotErr == nil && len(avatar) > 0 {
					result.AvatarPNG = avatar
					result.AvatarErrorCode = ""
				}
			}
		}
	}
	return &result, nil
}

func deepSelectorJSPath(selector string, index int) string {
	selectorJSON, _ := json.Marshal(selector)
	return fmt.Sprintf(`(() => {
		const selector = %s;
		const nodes = [];
		const roots = [document];
		for (let rootIndex = 0; rootIndex < roots.length; rootIndex += 1) {
			const root = roots[rootIndex];
			for (const element of root.querySelectorAll(selector)) {
				if (!nodes.includes(element)) nodes.push(element);
			}
			for (const element of root.querySelectorAll('*')) {
				if (element.shadowRoot) roots.push(element.shadowRoot);
			}
		}
		return nodes[%d] || null;
	})()`, selectorJSON, index)
}

func loadReviewedAvatarResourcePNG(tabCtx, callerCtx context.Context, rawURL string, allowedHostSuffixes []string) ([]byte, error) {
	if !avatarResourceURLAllowed(rawURL, allowedHostSuffixes) {
		return nil, fmt.Errorf("avatar resource URL is not reviewed")
	}
	resourceCtx, cancel := actionContext(tabCtx, callerCtx, 5*time.Second)
	defer cancel()
	var payload []byte
	err := chromedp.Run(resourceCtx, chromedp.ActionFunc(func(actionCtx context.Context) error {
		frameTree, err := page.GetFrameTree().Do(actionCtx)
		if err != nil {
			return fmt.Errorf("read avatar resource frame: %w", err)
		}
		if frameTree == nil || frameTree.Frame == nil {
			return fmt.Errorf("read avatar resource frame: missing frame")
		}
		resource, err := network.LoadNetworkResource(rawURL, &network.LoadNetworkResourceOptions{
			DisableCache: false, IncludeCredentials: false,
		}).WithFrameID(frameTree.Frame.ID).Do(actionCtx)
		if err != nil || resource == nil || !resource.Success || resource.HTTPStatusCode != 200 || resource.Stream == "" {
			return fmt.Errorf("load avatar resource")
		}
		defer func() { _ = cdpio.Close(resource.Stream).Do(actionCtx) }()
		payload = make([]byte, 0, 64<<10)
		for {
			params := cdpio.Read(resource.Stream).WithSize(256 << 10)
			var response struct {
				Base64encoded bool   `json:"base64Encoded"`
				Data          string `json:"data"`
				EOF           bool   `json:"eof"`
			}
			if err := cdp.Execute(actionCtx, cdpio.CommandRead, params, &response); err != nil {
				return fmt.Errorf("read avatar resource: %w", err)
			}
			chunk := []byte(response.Data)
			if response.Base64encoded {
				chunk, err = base64.StdEncoding.DecodeString(response.Data)
				if err != nil {
					return fmt.Errorf("decode avatar resource stream: %w", err)
				}
			}
			if len(payload)+len(chunk) > 2<<20 {
				return fmt.Errorf("avatar resource size is invalid")
			}
			payload = append(payload, chunk...)
			if response.EOF {
				return nil
			}
		}
	}))
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("avatar resource size is invalid")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(payload))
	if err != nil || config.Width < 16 || config.Height < 16 || config.Width > 2048 || config.Height > 2048 {
		return nil, fmt.Errorf("avatar resource dimensions are invalid")
	}
	decoded, _, err := image.Decode(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("decode avatar resource: %w", err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, decoded); err != nil {
		return nil, fmt.Errorf("encode avatar resource: %w", err)
	}
	return encoded.Bytes(), nil
}

func avatarResourceURLAllowed(rawURL string, allowedHostSuffixes []string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	for _, suffix := range allowedHostSuffixes {
		suffix = strings.ToLower(strings.TrimSpace(suffix))
		if suffix == "" {
			continue
		}
		if strings.HasPrefix(suffix, ".") {
			if host == strings.TrimPrefix(suffix, ".") || strings.HasSuffix(host, suffix) {
				return true
			}
			continue
		}
		if host == suffix {
			return true
		}
	}
	return false
}

func actionContext(ownerCtx, callerCtx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	actionCtx, cancel := context.WithTimeout(ownerCtx, timeout)
	stopCaller := context.AfterFunc(callerCtx, cancel)
	return actionCtx, func() {
		stopCaller()
		cancel()
	}
}

// chromedp normally closes a target when a child context is cancelled. This
// observer attached to a user-owned existing target, so clear ownership before
// cancellation; chromedp still detaches the CDP session but leaves the page.
func detachExistingTarget(tabCtx context.Context, cancel context.CancelFunc) {
	if chromedpContext := chromedp.FromContext(tabCtx); chromedpContext != nil && chromedpContext.Target != nil {
		chromedpContext.Target.TargetID = ""
	}
	cancel()
}

func profileObservationScript(rule platformProfileRule) (string, error) {
	rule.ExpiredTexts = append(append([]string(nil), rule.ExpiredTexts...), commonExpiredTexts...)
	rule.CheckpointTexts = append(append([]string(nil), rule.CheckpointTexts...), commonCheckpointTexts...)
	payload, err := json.Marshal(rule)
	if err != nil {
		return "", fmt.Errorf("encode profile observation rule: %w", err)
	}
	return `(() => {
  const rule = ` + string(payload) + `;
  const visible = (el) => {
    if (!el) return false;
    const style = window.getComputedStyle(el);
    const rect = el.getBoundingClientRect();
    return style.display !== 'none' && style.visibility !== 'hidden' &&
      rect.width > 0 && rect.height > 0 &&
      rect.right > 0 && rect.bottom > 0 &&
      rect.left < window.innerWidth && rect.top < window.innerHeight;
  };
  const nodesWithin = (root, selector) => {
    const nodes = [];
    if (root instanceof Element && root.matches(selector)) nodes.push(root);
    const queryRoots = [root];
    for (let index = 0; index < queryRoots.length; index += 1) {
      const queryRoot = queryRoots[index];
      for (const el of queryRoot.querySelectorAll(selector)) {
        if (!nodes.includes(el)) nodes.push(el);
      }
      for (const el of queryRoot.querySelectorAll('*')) {
        if (el.shadowRoot) queryRoots.push(el.shadowRoot);
      }
    }
    return nodes;
  };
  const stripNamePrefix = (value) => {
    const text = String(value || '').replace(/\s+/g, ' ').trim();
    for (const prefix of rule.NamePrefixes || []) {
      const normalized = String(prefix || '').replace(/\s+/g, ' ').trim();
      if (normalized && text.toLowerCase().startsWith(normalized.toLowerCase())) {
        return { matched: true, text: text.slice(normalized.length).trim() };
      }
    }
    return { matched: false, text };
  };
  const firstTextElement = (root, selectors) => {
    for (const selector of selectors || []) {
      for (const el of nodesWithin(root, selector)) {
        const text = String(el.textContent || '').replace(/\s+/g, ' ').trim();
        if (visible(el) && text && text.length <= 120) return { el, text };
      }
    }
    return null;
  };
  const firstPrefixedTextElement = (root, selectors) => {
    for (const selector of selectors || []) {
      for (const el of nodesWithin(root, selector)) {
        const normalized = stripNamePrefix(el.textContent || el.getAttribute('aria-label'));
        if (visible(el) && normalized.matched && normalized.text && normalized.text.length <= 120) {
          return { el, text: normalized.text };
        }
      }
    }
    return null;
  };
  const profileHandleFromHref = (rawHref) => {
    const href = String(rawHref || '').trim();
    if (!href) return '';
    let path = '';
    try { path = new URL(href, document.baseURI).pathname; } catch (_) {
      path = href.split(/[?#]/, 1)[0];
    }
    return path.match(/^\/@([A-Za-z0-9._-]{1,80})(?:\/|$)/)?.[1] || '';
  };
  const firstHiddenTextElement = (root, selectors) => {
    for (const selector of selectors || []) {
      for (const el of nodesWithin(root, selector)) {
        let text = String(el.textContent || '').replace(/\s+/g, ' ').trim();
        if (String(el.tagName || '').toLowerCase() === 'a') {
          const handle = profileHandleFromHref(el.getAttribute('href'));
          if (handle) text = handle;
        }
        if (text && text.length <= 120) return { el, text, unpositioned: !visible(el) };
      }
    }
    return null;
  };
  const firstIdentifier = (root) => {
    const textIdentifier = firstTextElement(root, rule.IdentifierSelectors);
    if (textIdentifier) return textIdentifier;
    const suffix = String(rule.IdentifierLinkHostSuffix || '').toLowerCase();
	const pathHost = String(rule.IdentifierLinkPathHost || '').toLowerCase();
    if (!suffix && !pathHost) return null;
    for (const selector of rule.IdentifierLinkSelectors || []) {
      for (const el of nodesWithin(root, selector)) {
        if (String(el.tagName).toLowerCase() !== 'a' || (!visible(el) && !pathHost)) continue;
        try {
		  const rawHref = el.getAttribute('href') || '';
		  const parsed = new URL(el.href || rawHref, document.baseURI);
		  const host = parsed.hostname.toLowerCase();
		  if (suffix && host.endsWith(suffix)) {
			const handle = host.slice(0, -suffix.length);
			if (handle && !handle.includes('.') && /^[a-z0-9_-]{1,80}$/.test(handle)) {
			  return { el, text: handle };
			}
		  }
		  if (pathHost && host === pathHost) {
				const handle = profileHandleFromHref(rawHref || parsed.href);
				if (handle) return { el, text: handle };
			  }
        } catch (_) {}
      }
    }
    return null;
  };
  const avatarCandidates = (root, imageSelectors, elementSelectors) => {
    const result = [];
    const groups = [
      { selectors: imageSelectors || [], imageOnly: true },
      { selectors: elementSelectors || [], imageOnly: false }
    ];
    for (const group of groups) {
      for (const selector of group.selectors) {
        for (const element of nodesWithin(root, selector)) {
        if (!visible(element) || (group.imageOnly && String(element.tagName).toLowerCase() !== 'img')) continue;
        if (!group.imageOnly && String(element.tagName).toLowerCase() !== 'img') {
          const role = String(element.getAttribute('role') || '').toLowerCase();
          const backgroundImage = String(window.getComputedStyle(element).backgroundImage || '');
          if (role !== 'img' && (!backgroundImage || backgroundImage === 'none')) continue;
        }
        const rect = element.getBoundingClientRect();
        const fingerprint = [
          element.getAttribute('src'), element.getAttribute('alt'), element.getAttribute('aria-label'),
          element.getAttribute('style'), element.className, element.id
        ]
          .filter(Boolean).join(' ').toLowerCase();
        if (/(^|[^a-z])(qr|qrcode|qr-code|barcode|logo|brand|login|passport|scan)([^a-z]|$)/.test(fingerprint)) continue;
        if (rect.width < 20 || rect.height < 20 || rect.width > 512 || rect.height > 512) continue;
        const ratio = rect.width / rect.height;
        if (ratio < 0.65 || ratio > 1.55) continue;
        const index = nodesWithin(document, selector).indexOf(element);
        if (index >= 0) result.push({ el: element, selector, index });
        }
      }
    }
    return result;
  };
  const identityWithin = (root) => {
    if (!root || !visible(root)) return null;
    const rootRect = root.getBoundingClientRect();
    if (rule.AccountRootMaxHeight > 0 && rootRect.height > rule.AccountRootMaxHeight) return null;
    const name = firstTextElement(root, rule.NameSelectors) ||
      firstPrefixedTextElement(root, rule.PrefixedNameSelectors) ||
      firstHiddenTextElement(root, rule.HiddenNameSelectors);
    const avatars = avatarCandidates(root, rule.AvatarSelectors, rule.AvatarElementSelectors);
    if (!name || avatars.length === 0) return null;
    const nameRect = name.el.getBoundingClientRect();
    let avatar = avatars[0];
    let distance = Number.POSITIVE_INFINITY;
    if (!name.unpositioned) {
      for (const candidate of avatars) {
        const avatarRect = candidate.el.getBoundingClientRect();
        const candidateDistance = Math.hypot(
          nameRect.left + nameRect.width / 2 - avatarRect.left - avatarRect.width / 2,
          nameRect.top + nameRect.height / 2 - avatarRect.top - avatarRect.height / 2
        );
        if (candidateDistance < distance) {
          distance = candidateDistance;
          avatar = candidate;
        }
      }
      if (rule.IdentityMaxCenterDistance > 0 && distance > rule.IdentityMaxCenterDistance) return null;
    }
    const identifier = firstIdentifier(root);
    if (rule.RequireIdentifier && !identifier) return null;
    return {
      name: name.text,
      accountIdentifier: identifier ? identifier.text : '',
      avatarSelector: avatar.selector,
      avatarIndex: avatar.index
    };
  };
  let identity = null;
  for (const selector of rule.AccountRootSelectors || []) {
    for (const root of nodesWithin(document, selector)) {
      identity = identityWithin(root);
      if (identity) break;
    }
    if (identity) break;
  };
  const visibleContainerIncludes = (selectors, values) => {
    for (const selector of selectors || []) {
      for (const el of nodesWithin(document, selector)) {
        if (!visible(el)) continue;
        const text = (el.innerText || '').replace(/\s+/g, ' ');
        if ((values || []).some((value) => value && text.includes(value))) return true;
      }
    }
    return false;
  };
	const reviewedSessionIncludes = (selectors, values) => {
	  for (const selector of selectors || []) {
	    for (const el of nodesWithin(document, selector)) {
	      if (!visible(el)) continue;
	      const text = (el.textContent || '').replace(/\s+/g, ' ');
	      if ((values || []).some((value) => value && text.includes(value))) return true;
	    }
	  }
	  return false;
	};
  const path = window.location.pathname.toLowerCase();
  const loginPath = (rule.LoginPaths || []).some((prefix) => prefix && path.startsWith(prefix.toLowerCase()));
  const sessionVerified = reviewedSessionIncludes(rule.SessionContainers, rule.SessionTexts);
  const sessionProofCount = (rule.SessionProofSelectors || []).reduce((count, selector) => {
    return count + (nodesWithin(document, selector).some((el) => visible(el)) ? 1 : 0);
  }, 0);
  const sessionAvatars = sessionVerified
    ? avatarCandidates(document, rule.SessionAvatarSelectors, rule.SessionAvatarElements)
    : [];
  const selectedAvatar = sessionAvatars[0] || (identity ? {
    selector: identity.avatarSelector,
    index: identity.avatarIndex,
    el: nodesWithin(document, identity.avatarSelector)[identity.avatarIndex] || null
  } : null);
  const avatarResourceURL = (() => {
    const element = selectedAvatar ? selectedAvatar.el : null;
    if (!element) return '';
    if (String(element.tagName).toLowerCase() === 'img') {
      return String(element.currentSrc || element.getAttribute('src') || '');
    }
    const background = String(window.getComputedStyle(element).backgroundImage || '');
    const match = background.match(/^url\(["']?(.*?)["']?\)$/);
    return match ? match[1] : '';
  })();
  const avatarScreenshotReady = (() => {
    const element = selectedAvatar ? selectedAvatar.el : null;
    if (!element) return false;
    if (String(element.tagName).toLowerCase() === 'img' &&
        (!element.complete || Number(element.naturalWidth || 0) <= 0)) {
      return false;
    }
    for (let current = element; current instanceof Element; current = current.parentElement) {
      const opacity = Number.parseFloat(window.getComputedStyle(current).opacity || '1');
      if (!Number.isFinite(opacity) || opacity < 0.999) return false;
    }
    return true;
  })();
  return {
    name: identity ? identity.name : '',
    accountIdentifier: identity ? identity.accountIdentifier : '',
    avatarSelector: selectedAvatar ? selectedAvatar.selector : '',
    avatarIndex: selectedAvatar ? selectedAvatar.index : -1,
    avatarResourceUrl: avatarResourceURL,
    avatarScreenshotReady,
    identityVerified: Boolean(identity),
    sessionVerified,
    sessionProofCount,
    loginRequired: loginPath || visibleContainerIncludes(rule.LoginContainers, rule.LoginTexts),
    sessionExpired: visibleContainerIncludes(rule.DialogSelectors, rule.ExpiredTexts),
    checkpointRequired: visibleContainerIncludes(rule.DialogSelectors, rule.CheckpointTexts),
    pageURL: window.location.href
  };
})()`, nil
}

func hostsFromURLs(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		parsed, err := url.Parse(value)
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		result = append(result, host)
	}
	return result
}
