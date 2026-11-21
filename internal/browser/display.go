package browser

// DisplayBounds uses the global top-left window coordinate space accepted by
// Chromium's --window-position and macOS System Events.
type DisplayBounds struct {
	X      int
	Y      int
	Width  int
	Height int
}

func preferredSecondaryDisplay(displays []DisplayBounds) (DisplayBounds, bool) {
	// NSScreen returns the primary display first. Mirrored displays share the
	// primary origin, so only a valid non-primary global rectangle is useful.
	if len(displays) < 2 {
		return DisplayBounds{}, false
	}
	for _, display := range displays[1:] {
		if display.Width > 0 && display.Height > 0 && (display.X != 0 || display.Y != 0) {
			return display, true
		}
	}
	return DisplayBounds{}, false
}
