//go:build legacy_chromium

package publishverify

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const (
	xhsCreatorHomeURL              = "https://creator.xiaohongshu.com/new/home"
	xhsNoteManagerURL              = "https://creator.xiaohongshu.com/new/note-manager"
	xhsPublishedTabMountTimeout    = 10 * time.Second
	xhsPublishedTabMountPollPeriod = 200 * time.Millisecond
)

// ChromedpXHSBackendObserver navigates one existing XHS creator tab through
// the account browser already owned by BrowserFacade. It never starts a
// browser, reads cookies, or uses a model-supplied port.
type ChromedpXHSBackendObserver struct {
	ActionTimeout time.Duration
	Location      *time.Location
	Now           func() time.Time
}

type xhsDOMSnapshot struct {
	ManagerURL       string       `json:"managerUrl"`
	TargetName       string       `json:"targetName"`
	TargetExternalID string       `json:"targetExternalId"`
	PublishedActive  bool         `json:"publishedActive"`
	Items            []xhsDOMItem `json:"items"`
}

type xhsDOMIdentity struct {
	HomeURL          string `json:"homeUrl"`
	TargetName       string `json:"targetName"`
	TargetExternalID string `json:"targetExternalId"`
}

type xhsDOMItem struct {
	Title          string `json:"title"`
	PublishedAt    string `json:"publishedAt"`
	DataImpression string `json:"dataImpression"`
	ContentType    string `json:"contentType"`
	Marker         string `json:"marker"`
	HasThumbnail   bool   `json:"hasThumbnail"`
}

func waitForXHSPublishedTabMount(
	ctx context.Context,
	timeout time.Duration,
	probe func(context.Context) (int, error),
	sleep func(context.Context, time.Duration) error,
) (int, error) {
	if timeout <= 0 || probe == nil {
		return 0, fmt.Errorf("%w: invalid published-tab mount wait", ErrXHSBackendDOMUnverified)
	}
	if sleep == nil {
		sleep = func(ctx context.Context, duration time.Duration) error {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	deadline := time.Now().Add(timeout)
	for {
		count, err := probe(ctx)
		if err != nil {
			return 0, err
		}
		if count == 1 || count > 1 {
			return count, nil
		}
		if count < 0 {
			return 0, fmt.Errorf("%w: invalid published-tab count", ErrXHSBackendDOMUnverified)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, nil
		}
		wait := xhsPublishedTabMountPollPeriod
		if wait > remaining {
			wait = remaining
		}
		if err := sleep(ctx, wait); err != nil {
			return 0, err
		}
	}
}

func (o ChromedpXHSBackendObserver) Observe(ctx context.Context, cdpURL string, expected XHSExpectation) (XHSBackendSnapshot, error) {
	if strings.TrimSpace(cdpURL) == "" || expected.PlatformCode != "xhs" ||
		strings.TrimSpace(expected.TargetName) == "" || strings.TrimSpace(expected.TargetExternalID) == "" {
		return XHSBackendSnapshot{}, fmt.Errorf("%w: invalid observer input", ErrXHSBackendDOMUnverified)
	}
	timeout := o.ActionTimeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ownershipCtx, ownershipCancel := context.WithCancel(context.Background())
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ownershipCtx, cdpURL)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	stopEnumeration := context.AfterFunc(ctx, browserCancel)
	enumerationTimer := time.AfterFunc(10*time.Second, browserCancel)
	targets, err := chromedp.Targets(browserCtx)
	enumerationTimer.Stop()
	stopEnumeration()
	if err != nil {
		browserCancel()
		allocCancel()
		ownershipCancel()
		return XHSBackendSnapshot{}, fmt.Errorf("enumerate XHS creator targets: %w", err)
	}
	targetID, err := selectXHSCreatorTargetForJob(targets, expected.SubmitTargetID)
	if err != nil {
		browserCancel()
		allocCancel()
		ownershipCancel()
		return XHSBackendSnapshot{}, err
	}
	tabCtx, tabCancel := chromedp.NewContext(allocCtx, chromedp.WithTargetID(targetID))
	actionCtx, actionCancel := context.WithTimeout(tabCtx, timeout)
	stopCaller := context.AfterFunc(ctx, actionCancel)
	defer func() {
		stopCaller()
		actionCancel()
		if chromedpContext := chromedp.FromContext(tabCtx); chromedpContext != nil && chromedpContext.Target != nil {
			chromedpContext.Target.TargetID = ""
		}
		tabCancel()
		browserCancel()
		allocCancel()
		ownershipCancel()
	}()

	var identity xhsDOMIdentity
	if err := chromedp.Run(actionCtx,
		chromedp.Navigate(xhsCreatorHomeURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Poll(xhsIdentityReadyJS, nil,
			chromedp.WithPollingInterval(200*time.Millisecond),
			chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(xhsCollectIdentityJS, &identity),
	); err != nil {
		return XHSBackendSnapshot{}, fmt.Errorf("verify XHS creator identity: %w", err)
	}
	if !validXHSCreatorHomeURL(identity.HomeURL) ||
		!sameText(identity.TargetName, expected.TargetName) ||
		strings.TrimSpace(identity.TargetExternalID) != strings.TrimSpace(expected.TargetExternalID) {
		return XHSBackendSnapshot{}, fmt.Errorf("%w: creator identity does not match frozen target", ErrXHSBackendDOMUnverified)
	}

	if err := chromedp.Run(actionCtx,
		chromedp.Navigate(xhsNoteManagerURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return XHSBackendSnapshot{}, fmt.Errorf("open XHS note manager: %w", err)
	}
	tabCount, err := waitForXHSPublishedTabMount(
		actionCtx,
		xhsPublishedTabMountTimeout,
		func(probeCtx context.Context) (int, error) {
			var count int
			if err := chromedp.Run(probeCtx, chromedp.Evaluate(xhsVisiblePublishedTabCountJS, &count)); err != nil {
				return 0, err
			}
			return count, nil
		},
		nil,
	)
	if err != nil {
		return XHSBackendSnapshot{}, fmt.Errorf("wait for XHS published tab: %w", err)
	}
	if tabCount > 1 {
		return XHSBackendSnapshot{}, fmt.Errorf("%w: published tab is ambiguous", ErrXHSBackendAmbiguous)
	}
	if tabCount != 1 {
		return XHSBackendSnapshot{}, fmt.Errorf("%w: published tab is missing", ErrXHSBackendDOMUnverified)
	}
	var tabState string
	if err := chromedp.Run(actionCtx, chromedp.Evaluate(xhsEnsurePublishedTabJS, &tabState)); err != nil {
		return XHSBackendSnapshot{}, fmt.Errorf("select XHS published tab: %w", err)
	}
	switch tabState {
	case "active":
	case "clicked":
		if err := chromedp.Run(actionCtx,
			chromedp.Poll(xhsPublishedTabActiveJS, nil,
				chromedp.WithPollingInterval(200*time.Millisecond),
				chromedp.WithPollingTimeout(10*time.Second)),
		); err != nil {
			return XHSBackendSnapshot{}, fmt.Errorf("%w: published tab did not become active", ErrXHSBackendDOMUnverified)
		}
	case "ambiguous":
		return XHSBackendSnapshot{}, fmt.Errorf("%w: published tab is ambiguous", ErrXHSBackendAmbiguous)
	default:
		return XHSBackendSnapshot{}, fmt.Errorf("%w: published tab is missing", ErrXHSBackendDOMUnverified)
	}

	var observed xhsDOMSnapshot
	if err := chromedp.Run(actionCtx, chromedp.Evaluate(xhsCollectBackendJS, &observed)); err != nil {
		return XHSBackendSnapshot{}, fmt.Errorf("read XHS note manager: %w", err)
	}
	if !validXHSManagerURL(observed.ManagerURL) || !observed.PublishedActive {
		return XHSBackendSnapshot{}, fmt.Errorf("%w: manager URL or published tab is unverified", ErrXHSBackendDOMUnverified)
	}
	observed.TargetName = identity.TargetName
	observed.TargetExternalID = identity.TargetExternalID

	candidate, err := selectXHSBackendCard(observed.Items, expected.Title, expected.ContentType)
	if err != nil {
		return XHSBackendSnapshot{}, err
	}
	snapshot := XHSBackendSnapshot{
		PlatformCode: "xhs", ManagerURL: observed.ManagerURL,
		TargetName: observed.TargetName, TargetExternalID: observed.TargetExternalID,
	}
	if candidate.Marker == "" {
		var screenshot []byte
		if err := chromedp.Run(actionCtx, chromedp.FullScreenshot(&screenshot, 100)); err != nil {
			return XHSBackendSnapshot{}, fmt.Errorf("capture XHS backend screenshot: %w", err)
		}
		snapshot.ScreenshotPNG = screenshot
		return snapshot, nil
	}

	noteID, impressionContentType, err := parseXHSDataImpression(candidate.DataImpression)
	contentType := strings.TrimSpace(candidate.ContentType)
	if err != nil ||
		(impressionContentType != "" && impressionContentType != contentType) ||
		contentType != expected.ContentType || !candidate.HasThumbnail ||
		!validXHSCardMarker(candidate.Marker) {
		return XHSBackendSnapshot{}, fmt.Errorf("%w: note identity, type, or thumbnail is unverified", ErrXHSBackendDOMUnverified)
	}
	publishedAt, err := parseXHSPublishedTime(candidate.PublishedAt, o.location(), o.now())
	if err != nil {
		return XHSBackendSnapshot{}, fmt.Errorf("%w: publish time is unverified", ErrXHSBackendDOMUnverified)
	}
	var thumbnailPNG []byte
	selector := fmt.Sprintf(`[data-alpha-nexus-xhs-verify="%s"] img.content`, candidate.Marker)
	if err := chromedp.Run(actionCtx,
		chromedp.Screenshot(selector, &thumbnailPNG, chromedp.NodeVisible, chromedp.ByQuery),
	); err != nil {
		return XHSBackendSnapshot{}, fmt.Errorf("%w: capture rendered thumbnail: %v", ErrXHSThumbnailUnverified, err)
	}
	thumbnailHash, err := matchXHSThumbnail(thumbnailPNG, expected.FrozenMedia)
	if err != nil {
		return XHSBackendSnapshot{}, err
	}
	var screenshot []byte
	if err := chromedp.Run(actionCtx, chromedp.FullScreenshot(&screenshot, 100)); err != nil {
		return XHSBackendSnapshot{}, fmt.Errorf("capture XHS backend screenshot: %w", err)
	}
	snapshot.Items = []XHSBackendItem{{
		ContentType: contentType,
		Status:      "已发布",
		Title:       candidate.Title,
		// The XHS manager card exposes the frozen title and media identity. The
		// generic matcher may accept exact title-only content only because the
		// independent thumbnail, target, type, time, noteId and tab all match.
		ThumbnailSHA256: thumbnailHash,
		PublishedAt:     publishedAt.UTC().Format(time.RFC3339),
		PostURL:         "https://www.xiaohongshu.com/explore/" + noteID,
		ExternalPostID:  noteID,
	}}
	snapshot.ScreenshotPNG = screenshot
	return snapshot, nil
}

func selectXHSBackendCard(items []xhsDOMItem, title, contentType string) (xhsDOMItem, error) {
	candidates := make([]xhsDOMItem, 0, 1)
	for _, item := range items {
		if !sameText(item.Title, title) || strings.TrimSpace(item.ContentType) != strings.TrimSpace(contentType) {
			continue
		}
		channel, err := parseXHSDataImpressionChannel(item.DataImpression)
		if err != nil {
			return xhsDOMItem{}, fmt.Errorf("%w: note-manager channel is unverified", ErrXHSBackendDOMUnverified)
		}
		if channel == "published" {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) > 1 {
		return xhsDOMItem{}, ErrXHSBackendAmbiguous
	}
	if len(candidates) == 0 {
		return xhsDOMItem{}, nil
	}
	return candidates[0], nil
}

func selectXHSCreatorTarget(targets []*target.Info) (target.ID, error) {
	managerTargets := make([]target.ID, 0, 1)
	creatorTargets := make([]target.ID, 0, 1)
	for _, item := range targets {
		if item == nil || item.Type != "page" {
			continue
		}
		parsed, err := url.Parse(strings.TrimSpace(item.URL))
		if err != nil || parsed.Scheme != "https" ||
			!strings.EqualFold(parsed.Hostname(), "creator.xiaohongshu.com") {
			continue
		}
		creatorTargets = append(creatorTargets, item.TargetID)
		if parsed.EscapedPath() == "/new/note-manager" {
			managerTargets = append(managerTargets, item.TargetID)
		}
	}
	if len(managerTargets) == 1 {
		return managerTargets[0], nil
	}
	if len(managerTargets) > 1 || len(creatorTargets) > 1 {
		return "", ErrXHSBackendAmbiguous
	}
	if len(creatorTargets) == 1 {
		return creatorTargets[0], nil
	}
	return "", fmt.Errorf("%w: XHS creator tab is missing", ErrXHSBackendDOMUnverified)
}

func selectXHSCreatorTargetForJob(targets []*target.Info, submitTargetID string) (target.ID, error) {
	if submitTargetID == "" {
		return selectXHSCreatorTarget(targets)
	}
	if !validXHSTargetID(submitTargetID) {
		return "", fmt.Errorf("%w: invalid submit target id", ErrXHSBackendDOMUnverified)
	}
	for _, item := range targets {
		if item == nil || item.Type != "page" || string(item.TargetID) != submitTargetID {
			continue
		}
		parsed, err := url.Parse(strings.TrimSpace(item.URL))
		if err == nil && parsed.Scheme == "https" &&
			strings.EqualFold(parsed.Hostname(), "creator.xiaohongshu.com") {
			return item.TargetID, nil
		}
	}
	// XHS may close the submitted tab after accepting the click. Only the
	// independently selected unique manager tab is an acceptable fallback.
	return selectXHSCreatorTarget(targets)
}

func validXHSTargetID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func validXHSManagerURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed.Scheme == "https" && parsed.User == nil &&
		strings.EqualFold(parsed.Hostname(), "creator.xiaohongshu.com") &&
		parsed.Port() == "" && parsed.EscapedPath() == "/new/note-manager" &&
		parsed.RawQuery == "" && parsed.Fragment == ""
}

func validXHSCreatorHomeURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed.Scheme == "https" && parsed.User == nil &&
		strings.EqualFold(parsed.Hostname(), "creator.xiaohongshu.com") &&
		parsed.Port() == "" && parsed.EscapedPath() == "/new/home" &&
		parsed.RawQuery == "" && parsed.Fragment == ""
}

func validXHSCardMarker(value string) bool {
	if len(value) < 8 || len(value) > 80 {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-') {
			return false
		}
	}
	return true
}

func parseXHSPublishedTime(raw string, location *time.Location, now time.Time) (time.Time, error) {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "发布于"))
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	if location == nil {
		location = time.FixedZone("CST", 8*60*60)
	}
	for _, layout := range []string{
		"2006-01-02 15:04", "2006/01/02 15:04", "2006年01月02日 15:04",
	} {
		if parsed, err := time.ParseInLocation(layout, raw, location); err == nil {
			return parsed, nil
		}
	}
	if strings.HasPrefix(raw, "今天 ") {
		clock, err := time.ParseInLocation("15:04", strings.TrimSpace(strings.TrimPrefix(raw, "今天")), location)
		if err == nil {
			localNow := now.In(location)
			return time.Date(localNow.Year(), localNow.Month(), localNow.Day(),
				clock.Hour(), clock.Minute(), 0, 0, location), nil
		}
	}
	return time.Time{}, errors.New("unsupported XHS publish time")
}

func (o ChromedpXHSBackendObserver) location() *time.Location {
	if o.Location != nil {
		return o.Location
	}
	return time.FixedZone("CST", 8*60*60)
}

func (o ChromedpXHSBackendObserver) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

const xhsIdentityReadyJS = `(() => {
  const visible = (node) => {
    if (!(node instanceof Element)) return false;
    const style = getComputedStyle(node);
    const rect = node.getBoundingClientRect();
    return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
  };
  const clean = (value) => (value || '').replace(/\s+/g, ' ').trim();
  const cards = Array.from(document.querySelectorAll('.base')).filter(visible);
  const identities = cards.filter((card) => {
    const names = Array.from(card.querySelectorAll('.account-name, .name-box')).filter(visible)
      .map((node) => clean(node.textContent)).filter(Boolean);
    const identifiers = Array.from(card.querySelectorAll('.others.description-text > div:first-child'))
      .filter(visible).map((node) => clean(node.textContent)
        .replace(/^(小红书账号|小红书号)\s*[:：]\s*/, '')).filter(Boolean);
    return names.length === 1 && identifiers.length === 1;
  });
  return identities.length === 1;
})()`

const xhsCollectIdentityJS = `(() => {
  const visible = (node) => {
    if (!(node instanceof Element)) return false;
    const style = getComputedStyle(node);
    const rect = node.getBoundingClientRect();
    return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
  };
  const clean = (value) => (value || '').replace(/\s+/g, ' ').trim();
  const identities = [];
  const cards = Array.from(document.querySelectorAll('.base')).filter(visible);
  for (const card of cards) {
    const names = Array.from(card.querySelectorAll('.account-name, .name-box')).filter(visible)
      .map((node) => clean(node.textContent)).filter(Boolean);
    const identifiers = Array.from(card.querySelectorAll('.others.description-text > div:first-child'))
      .filter(visible).map((node) => clean(node.textContent)
        .replace(/^(小红书账号|小红书号)\s*[:：]\s*/, '')).filter(Boolean);
    if (names.length === 1 && identifiers.length === 1) {
      identities.push({name: names[0], externalId: identifiers[0]});
    }
  }
  return {
    homeUrl: location.href,
    targetName: identities.length === 1 ? identities[0].name : '',
    targetExternalId: identities.length === 1 ? identities[0].externalId : ''
  };
})()`

const xhsVisiblePublishedTabCountJS = `(() => {
  const visible = (node) => {
    if (!(node instanceof Element)) return false;
    const style = getComputedStyle(node);
    const rect = node.getBoundingClientRect();
    return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
  };
  const clean = (value) => (value || '').replace(/\s+/g, ' ').trim();
  return Array.from(document.querySelectorAll('[role="tab"], .tabs .tab, [class~="tab"], .tab-item'))
    .filter((node) => visible(node) && clean(node.textContent) === '已发布').length;
})()`

const xhsEnsurePublishedTabJS = `(() => {
  const visible = (node) => {
    if (!(node instanceof Element)) return false;
    const style = getComputedStyle(node);
    const rect = node.getBoundingClientRect();
    return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
  };
  const clean = (value) => (value || '').replace(/\s+/g, ' ').trim();
  const tabs = Array.from(document.querySelectorAll('[role="tab"], .tabs .tab, [class~="tab"], .tab-item'))
    .filter((node) => visible(node) && clean(node.textContent) === '已发布');
  if (tabs.length !== 1) return tabs.length > 1 ? 'ambiguous' : 'missing';
  const tab = tabs[0];
  const classes = Array.from(tab.classList);
  const active = tab.getAttribute('aria-selected') === 'true' ||
    tab.getAttribute('data-state') === 'active' ||
    classes.includes('active') || classes.includes('selected') || classes.includes('tab-item--active');
  if (active) return 'active';
  tab.click();
  return 'clicked';
})()`

const xhsPublishedTabActiveJS = `(() => {
  const visible = (node) => {
    if (!(node instanceof Element)) return false;
    const style = getComputedStyle(node);
    const rect = node.getBoundingClientRect();
    return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
  };
  const clean = (value) => (value || '').replace(/\s+/g, ' ').trim();
  const tabs = Array.from(document.querySelectorAll('[role="tab"], .tabs .tab, [class~="tab"], .tab-item'))
    .filter((node) => visible(node) && clean(node.textContent) === '已发布');
  if (tabs.length !== 1) return false;
  const tab = tabs[0];
  const classes = Array.from(tab.classList);
  return tab.getAttribute('aria-selected') === 'true' ||
    tab.getAttribute('data-state') === 'active' ||
    classes.includes('active') || classes.includes('selected') || classes.includes('tab-item--active');
})()`

const xhsCollectBackendJS = `(() => {
  const visible = (node) => {
    if (!(node instanceof Element)) return false;
    const style = getComputedStyle(node);
    const rect = node.getBoundingClientRect();
    return style.display !== 'none' && style.visibility !== 'hidden' && rect.width > 0 && rect.height > 0;
  };
  const clean = (value) => (value || '').replace(/\s+/g, ' ').trim();
  const roots = Array.from(document.querySelectorAll('.base, .user-info')).filter(visible);
  const identities = [];
  for (const root of roots) {
    const names = Array.from(root.querySelectorAll('.account-name, .name-box')).filter(visible)
      .map((node) => clean(node.textContent)).filter(Boolean);
    const identifiers = Array.from(root.querySelectorAll('.others.description-text > div:first-child'))
      .filter(visible).map((node) => clean(node.textContent)
        .replace(/^(小红书账号|小红书号)\s*[:：]\s*/, '')).filter(Boolean);
    if (names.length === 1 && identifiers.length === 1) {
      identities.push({name: names[0], externalId: identifiers[0]});
    }
  }
  const tabs = Array.from(document.querySelectorAll('[role="tab"], .tabs .tab, [class~="tab"], .tab-item'))
    .filter((node) => visible(node) && clean(node.textContent) === '已发布');
  const publishedActive = tabs.length === 1 && (
    tabs[0].getAttribute('aria-selected') === 'true' ||
    tabs[0].getAttribute('data-state') === 'active' ||
    tabs[0].classList.contains('active') || tabs[0].classList.contains('selected') ||
    tabs[0].classList.contains('tab-item--active')
  );
  const items = [];
  const cards = Array.from(document.querySelectorAll('.note-card[data-impression]')).filter(visible);
  cards.slice(0, 100).forEach((card, index) => {
    const titles = Array.from(card.querySelectorAll('.title, [class~="title"], .note-card__title')).filter(visible)
      .map((node) => clean(node.textContent)).filter(Boolean);
    const times = Array.from(card.querySelectorAll('time[datetime], .time, [class~="publish-time"], .note-card__time'))
      .filter(visible).map((node) => clean(node.getAttribute('datetime') || node.textContent)).filter(Boolean);
    const images = Array.from(card.querySelectorAll('img.content')).filter(visible);
    if (titles.length !== 1 || times.length !== 1) return;
    const contentType = card.querySelector('.play_time, img.play') ? 'video' : 'graph_text';
    const marker = 'verify-' + index + '-' + Date.now().toString(36);
    card.setAttribute('data-alpha-nexus-xhs-verify', marker);
    items.push({
      title: titles[0],
      publishedAt: times[0],
      dataImpression: card.getAttribute('data-impression') || '',
      contentType,
      marker,
      hasThumbnail: images.length === 1
    });
  });
  return {
    managerUrl: location.href,
    targetName: identities.length === 1 ? identities[0].name : '',
    targetExternalId: identities.length === 1 ? identities[0].externalId : '',
    publishedActive,
    items
  };
})()`
