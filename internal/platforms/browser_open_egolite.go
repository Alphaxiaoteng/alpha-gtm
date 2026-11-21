//go:build !legacy_chromium

package platforms

import (
	"context"
	"fmt"
	"strings"

	"alpha-nexus-egolite/internal/errors"
)

// OpenURL preserves the legacy adapter contract while keeping the default
// EgoLite product build incapable of attaching to a CDP endpoint.
func OpenURL(_ context.Context, cdpURL, url string) error {
	if strings.TrimSpace(cdpURL) == "" {
		return fmt.Errorf("empty cdp url")
	}
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("empty url")
	}
	return fmt.Errorf("%s: legacy Chromium/CDP browser path is disabled in EgoLite build", errors.CDP_CONNECT)
}
