//go:build !legacy_chromium

package publishguard

// LiveCDPURL is a fail-closed compatibility surface. The EgoLite product does
// not expose or consume CDP endpoints.
func LiveCDPURL(_ int64, _ any, _ error) (string, error) {
	return "", ErrBrowserCDPUnavailable
}
