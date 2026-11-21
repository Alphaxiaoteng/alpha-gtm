//go:build !darwin

package radar

import (
	"context"
	"fmt"
)

func InstallWorker(context.Context, string) error {
	return fmt.Errorf("background trend radar schedules require macOS")
}
