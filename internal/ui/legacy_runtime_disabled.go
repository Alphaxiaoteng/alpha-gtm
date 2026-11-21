//go:build !legacy_chromium

package ui

import "time"

type legacyUIRuntime struct{}

const uiBackgroundProbeWait time.Duration = 0

func initializeLegacyUIRuntime(*Server, string) error { return nil }
