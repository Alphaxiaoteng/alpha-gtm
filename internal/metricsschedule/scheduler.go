package metricsschedule

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
)

// CollectFunc runs the reviewed browser-evidence collection for one account.
// It is egolite.CreatorMetricsService.Collect behind a function seam so tests
// can fake the whole browser workflow.
type CollectFunc func(ctx context.Context, accountID int64) (state string, err error)

// AfterRunFunc runs once per completed sweep, after every account was
// attempted and the counters are final. A failure is reported to the caller
// but does not fail the collection itself: mirroring content elsewhere must
// never lose a collection result.
type AfterRunFunc func(ctx context.Context, result db.MetricsCollectionResult) error

// staleRunIntervalFactor declares a 'running' row stale after this many
// intervals without any update. One sweep is bounded by the per-account timeout
// times the account count, far below three full intervals, so a row that old
// belongs to a process that died mid-sweep.
const staleRunIntervalFactor = 3

// staleRunErrorCode marks a 'running' row reset by recovery so the settings UI
// can show why the schedule resumed instead of pretending the run finished.
const staleRunErrorCode = "METRICS_SCHEDULE_STALE_RUN"

// minRunGap is the floor between two sweep attempts. A sweep can fail
// immediately (already running, settings unreadable) while the schedule stays
// overdue, so retrying without a floor spins the loop.
const minRunGap = 5 * time.Second

type Scheduler struct {
	db       *db.DB
	collect  CollectFunc
	afterRun AfterRunFunc
	now      func() time.Time
	after    func(time.Duration) <-chan time.Time
	wake     chan struct{}
	// onRunError is the loop's failure sink. Production leaves it nil and the
	// error goes to the process log; tests install it to assert the loop does
	// not drop a failed sweep.
	onRunError func(error)

	mu      sync.Mutex
	running bool
}

func New(database *db.DB, collect CollectFunc) *Scheduler {
	return &Scheduler{
		db:      database,
		collect: collect,
		now:     time.Now,
		after:   time.After,
		wake:    make(chan struct{}, 1),
	}
}

// SetAfterRun installs the post-sweep hook, for example the Feishu mirror.
func (s *Scheduler) SetAfterRun(fn AfterRunFunc) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.afterRun = fn
}

func (s *Scheduler) Wake() {
	if s == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Scheduler) due(settings *db.MetricsCollectionSchedule, now time.Time) bool {
	if !settings.Enabled {
		return false
	}
	if settings.LastFinishedAt == "" {
		return true
	}
	lastFinished, err := time.Parse(time.RFC3339, settings.LastFinishedAt)
	if err != nil {
		return true
	}
	return !now.Before(lastFinished.Add(time.Duration(settings.IntervalHours) * time.Hour))
}

// recoverStaleRun resets a 'running' row left behind by a crashed or killed
// process. Without this path the row stayed 'running' forever: Begin only flips
// an idle row, so every later sweep failed with ErrMetricsCollectionRunning and
// the schedule silently never collected again.
//
// last_finished_at is intentionally left alone so the missed sweep is still due
// and runs immediately, and updated_at (not last_started_at) is the anchor
// because it moves on any settings change, which keeps a live sweep safe.
func (s *Scheduler) recoverStaleRun(settings *db.MetricsCollectionSchedule, now time.Time) error {
	if settings == nil || settings.State != "running" {
		return nil
	}
	// A sweep this process is still running is never stale, whatever the row
	// says: the row only refreshes at Begin and Finish, so a long sweep must not
	// be reset out from under itself and restarted concurrently.
	s.mu.Lock()
	inFlight := s.running
	s.mu.Unlock()
	if inFlight {
		return nil
	}
	anchor, ok := scheduleAnchor(settings)
	if !ok {
		// An unparsable timestamp cannot prove the run is live; treat it as stale
		// instead of leaving the schedule wedged forever.
		anchor = time.Time{}
	}
	if !anchor.IsZero() && now.Sub(anchor) < time.Duration(settings.IntervalHours)*staleRunIntervalFactor*time.Hour {
		return nil
	}
	_, err := s.db.Exec(`UPDATE metrics_collection_schedule
		SET state='idle', last_error_code=?, updated_at=? WHERE id=1 AND state='running'`,
		staleRunErrorCode, now.UTC().Format(time.RFC3339))
	return err
}

// markSweepInFlight records whether this process owns the current sweep, so
// stale-run recovery can tell a crashed row from a live one.
func (s *Scheduler) markSweepInFlight(running bool) {
	s.mu.Lock()
	s.running = running
	s.mu.Unlock()
}

// scheduleAnchor returns the newest credible timestamp for a 'running' row.
func scheduleAnchor(settings *db.MetricsCollectionSchedule) (time.Time, bool) {
	for _, text := range []string{settings.UpdatedAt, settings.LastStartedAt} {
		if parsed, err := time.Parse(time.RFC3339, text); err == nil && !parsed.IsZero() {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

// Run performs at most one sweep: disabled or not-yet-due schedules are a
// no-op. Eligible accounts run through the reviewed browser-evidence
// collection serially — the same ego space is serialized inside the service.
func (s *Scheduler) Run(ctx context.Context) (*db.MetricsCollectionResult, error) {
	return s.run(ctx, false)
}

// RunNow 是用户点「立即采集」走的路径：无视间隔直接采一次。
//
// 此前这个按钮直接调用 Run，而 Run 第一件事就是判断 due——24 小时间隔内点
// 它只会拿到一个 eligible/collected/skipped/failed 全 0 的空结果，界面却把它
// 渲染成一次成功的采集。注意 enabled 仍然生效：用户明确关掉定时采集时，
// 按钮也不该绕过这个开关去起浏览器。
func (s *Scheduler) RunNow(ctx context.Context) (*db.MetricsCollectionResult, error) {
	return s.run(ctx, true)
}

func (s *Scheduler) run(ctx context.Context, force bool) (*db.MetricsCollectionResult, error) {
	if s == nil || s.db == nil || s.collect == nil {
		return nil, errors.New("metrics scheduler unavailable")
	}
	now := s.now().UTC()
	settings, err := s.db.MetricsCollectionScheduleSettings()
	if err != nil {
		return nil, err
	}
	result := &db.MetricsCollectionResult{}
	if err := s.recoverStaleRun(settings, now); err != nil {
		return result, err
	}
	if !force && !s.due(settings, now) {
		return result, nil
	}
	// 强制采一次也必须尊重「定时采集已关闭」这个开关——那是用户明确的选择。
	if force && !settings.Enabled {
		return result, nil
	}
	if _, err := s.db.BeginMetricsCollectionRun(now); err != nil {
		return result, err
	}
	s.markSweepInFlight(true)
	defer s.markSweepInFlight(false)
	errorCode := ""
	defer func() {
		if ctx.Err() != nil {
			errorCode = "METRICS_SCHEDULE_CANCELLED"
		}
		_ = s.db.FinishMetricsCollectionRun(s.now().UTC(), errorCode, *result)
	}()

	candidates, err := s.db.StaleLoggedInAccounts(s.now().UTC(), settings.IntervalHours)
	if err != nil {
		return result, err
	}
	result.Eligible = len(candidates)
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if !egolite.SupportsCreatorMetrics(candidate.PlatformCode) {
			result.Skipped++
			continue
		}
		accountCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		state, collectErr := s.collect(accountCtx, candidate.ID)
		cancel()
		switch {
		case collectErr != nil:
			result.Failed++
		case state == "observed":
			result.Collected++
		default:
			result.Skipped++
		}
	}
	s.mu.Lock()
	hook := s.afterRun
	s.mu.Unlock()
	if hook != nil {
		if hookErr := hook(ctx, *result); hookErr != nil {
			// The sweep itself succeeded; the hook failure is surfaced without
			// discarding the per-account counters above.
			return result, fmt.Errorf("after-collection hook: %w", hookErr)
		}
	}
	return result, nil
}

// Start loops like the session monitor: re-read settings each cycle so the
// settings UI can flip the switch without a restart.
func (s *Scheduler) Start(ctx context.Context, waitGroup *sync.WaitGroup) {
	if s == nil || ctx == nil || waitGroup == nil {
		return
	}
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		for {
			settings, err := s.db.MetricsCollectionScheduleSettings()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case <-s.wake:
					continue
				case <-s.after(time.Minute):
					continue
				}
			}
			if !settings.Enabled {
				select {
				case <-ctx.Done():
					return
				case <-s.wake:
					continue
				}
			}
			delay := time.Until(s.nextDue(settings))
			if delay < 0 {
				delay = 0
			}
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
				continue
			case <-s.after(delay):
				attempted := time.Now()
				_, runErr := s.Run(ctx)
				if runErr == nil {
					continue
				}
				// The failure is reported instead of dropped: a sweep that fails
				// immediately leaves nextDue in the past, so the loop would
				// otherwise retry at 0s and discard the same error every tick.
				s.reportRunError(runErr)
				if wait := minRunGap - time.Since(attempted); wait > 0 {
					select {
					case <-ctx.Done():
						return
					case <-s.wake:
					case <-s.after(wait):
					}
				}
			}
		}
	}()
}

// reportRunError surfaces a failed sweep. Start has no return value, so a
// failure goes to the process log next to the other monitors' loop errors.
func (s *Scheduler) reportRunError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	sink := s.onRunError
	s.mu.Unlock()
	if sink != nil {
		sink(err)
		return
	}
	fmt.Printf("creator metrics schedule run failed: %v\n", err)
}

func (s *Scheduler) nextDue(settings *db.MetricsCollectionSchedule) time.Time {
	if settings.LastFinishedAt == "" {
		// First scheduled sweep waits one interval after enable so a fresh
		// toggle does not immediately grab every browser.
		return s.now().Add(time.Duration(settings.IntervalHours) * time.Hour)
	}
	lastFinished, err := time.Parse(time.RFC3339, settings.LastFinishedAt)
	if err != nil {
		return s.now()
	}
	return lastFinished.Add(time.Duration(settings.IntervalHours) * time.Hour)
}
