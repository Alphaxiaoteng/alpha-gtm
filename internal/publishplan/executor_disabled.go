//go:build !legacy_chromium

package publishplan

import (
	"context"
	"fmt"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/platforms"
	"alpha-nexus-egolite/internal/publishworker"
)

// Executor is retained only as a fail-closed compatibility surface. EgoLite
// publishing is implemented by the reviewed EgoRuntime flow.
type Executor struct {
	DB                   *db.DB
	Browsers             any
	Worker               platforms.PlaywrightWorker
	PublisherFor         func(string) (platforms.Publisher, error)
	LoginProbe           any
	VerifyCreatorBackend CreatorBackendVerifyFunc
	VerifyInstagram      CreatorBackendVerifyFunc
	VerifyXHS            CreatorBackendVerifyFunc
	Now                  func() time.Time
}

func legacyExecutorDisabled() error {
	return fmt.Errorf("PUBLISH_LEGACY_CHROMIUM_DISABLED")
}

func (e *Executor) Prepare(context.Context, int64) (*Execution, error) {
	return nil, legacyExecutorDisabled()
}

func (e *Executor) Submit(context.Context, int64) (*Execution, error) {
	return nil, legacyExecutorDisabled()
}

func (e *Executor) Inspect(context.Context, int64) (*Inspection, error) {
	return nil, legacyExecutorDisabled()
}

func (e *Executor) InspectComposer(context.Context, int64) (*Inspection, error) {
	return nil, legacyExecutorDisabled()
}

func (e *Executor) InspectManager(context.Context, int64) (*Inspection, error) {
	return nil, legacyExecutorDisabled()
}

func (e *Executor) ResumeTopics(context.Context, int64) (*Inspection, error) {
	return nil, legacyExecutorDisabled()
}

func (e *Executor) RevealSettings(context.Context, int64) (*Inspection, error) {
	return nil, legacyExecutorDisabled()
}

func (e *Executor) DeleteContentRecord(context.Context, int64) (*publishworker.Output, error) {
	return nil, legacyExecutorDisabled()
}
