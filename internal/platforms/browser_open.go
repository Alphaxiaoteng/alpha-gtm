//go:build legacy_chromium

package platforms

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// OpenURL attaches to an existing browser via CDP remote allocator and navigates
// to url. Used by browser/hybrid publishers; does not import publish (no cycles).
func OpenURL(ctx context.Context, cdpURL, url string) error {
	if strings.TrimSpace(cdpURL) == "" {
		return fmt.Errorf("empty cdp url")
	}
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("empty url")
	}

	// chromedp closes an attached target when its owning context is cancelled.
	// Keep ownership independent from the caller, apply caller cancellation only
	// to the bounded action, then clear target ownership before cleanup.
	ownershipCtx, ownershipCancel := context.WithCancel(context.Background())
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ownershipCtx, cdpURL)
	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	actionCtx, actionCancel := context.WithTimeout(taskCtx, 45*time.Second)
	stopCaller := context.AfterFunc(ctx, actionCancel)
	defer func() {
		stopCaller()
		actionCancel()
		if chromedpContext := chromedp.FromContext(taskCtx); chromedpContext != nil && chromedpContext.Target != nil {
			chromedpContext.Target.TargetID = ""
		}
		taskCancel()
		allocCancel()
		ownershipCancel()
	}()

	return chromedp.Run(actionCtx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body", chromedp.ByQuery),
	)
}
