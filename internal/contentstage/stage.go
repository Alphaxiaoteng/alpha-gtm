// Package contentstage models the editorial pipeline a piece of content moves
// through, and the one rule that keeps a schedule honest: a later stage may not
// be planned before an earlier one.
//
// It is deliberately separate from the publish path. Publishing has its own
// frozen-plan and single-approval rules; this package only answers "is this
// schedule internally consistent", so nothing here can mark anything published.
package contentstage

import (
	"fmt"
	"strings"
	"time"
)

// Stage is one step of the pipeline, in order.
type Stage string

const (
	StageInbox      Stage = "inbox"
	StageTopic      Stage = "topic"
	StageScript     Stage = "script"
	StageRecording  Stage = "recording"
	StageEditing    Stage = "editing"
	StagePublishing Stage = "publishing"
	StageReview     Stage = "review"
	StageArchived   Stage = "archived"
)

// Pipeline is the canonical order. Index position is the stage rank, so the
// ordering rule is a comparison rather than a second table to keep in sync.
var Pipeline = []Stage{
	StageInbox, StageTopic, StageScript, StageRecording,
	StageEditing, StagePublishing, StageReview, StageArchived,
}

// Labels are the names a person sees.
var Labels = map[Stage]string{
	StageInbox: "素材箱", StageTopic: "选题", StageScript: "脚本",
	StageRecording: "录制", StageEditing: "剪辑", StagePublishing: "发布",
	StageReview: "复盘", StageArchived: "归档",
}

// Rank returns the position of a stage, or -1 when it is unknown.
func Rank(stage Stage) int {
	for index, candidate := range Pipeline {
		if candidate == stage {
			return index
		}
	}
	return -1
}

// Valid reports whether the value names a real stage.
func Valid(stage Stage) bool { return Rank(stage) >= 0 }

// Schedulable reports whether a stage can carry a planned date. Inbox and
// archived do not: one is a holding area, the other is the end state.
func Schedulable(stage Stage) bool {
	switch stage {
	case StageInbox, StageArchived:
		return false
	}
	return Valid(stage)
}

// Event is one planned or completed stage occurrence.
type Event struct {
	Stage       Stage
	PlannedDate string // YYYY-MM-DD; empty means unscheduled
	Completed   bool
}

// Violation names one broken ordering rule so the operator can fix it.
type Violation struct {
	Earlier Stage
	Later   Stage
	Reason  string
}

func (v Violation) Error() string {
	return fmt.Sprintf("%s 不能晚于 %s：%s", Labels[v.Earlier], Labels[v.Later], v.Reason)
}

const dateLayout = "2006-01-02"

// ParseDate reads a planned date written the way a date input produces it.
// A value carrying a clock is accepted and truncated to the day, because the
// interface offers a datetime picker.
func ParseDate(value string) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("date is empty")
	}
	if len(trimmed) > len(dateLayout) {
		trimmed = trimmed[:len(dateLayout)]
	}
	parsed, err := time.Parse(dateLayout, trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf("date %q is not YYYY-MM-DD", value)
	}
	return parsed, nil
}

// CheckOrder reports every place where an earlier stage is planned after a
// later one.
//
// The rule only compares stages whose earlier side is still open for planning:
// once a stage is completed its date is history, and forcing history to move
// would rewrite what already happened.
func CheckOrder(events []Event) ([]Violation, error) {
	type dated struct {
		stage Stage
		date  time.Time
	}
	var planned []dated
	for _, event := range events {
		if event.Completed || strings.TrimSpace(event.PlannedDate) == "" {
			continue
		}
		if !Valid(event.Stage) {
			return nil, fmt.Errorf("unknown stage %q", event.Stage)
		}
		date, err := ParseDate(event.PlannedDate)
		if err != nil {
			return nil, err
		}
		planned = append(planned, dated{stage: event.Stage, date: date})
	}

	var violations []Violation
	for i := 0; i < len(planned); i++ {
		for j := 0; j < len(planned); j++ {
			if i == j {
				continue
			}
			earlier, later := planned[i], planned[j]
			if Rank(earlier.stage) >= Rank(later.stage) {
				continue
			}
			if earlier.date.After(later.date) {
				violations = append(violations, Violation{
					Earlier: earlier.stage, Later: later.stage,
					Reason: fmt.Sprintf("前序阶段排在 %s，后序阶段却排在 %s",
						earlier.date.Format(dateLayout), later.date.Format(dateLayout)),
				})
			}
		}
	}
	return violations, nil
}

// CanSchedule reports whether planning one stage on a date is consistent with
// the events already recorded. It is the single-stage form of CheckOrder, used
// before a save so the interface can refuse the edit instead of repairing it.
func CanSchedule(stage Stage, plannedDate string, existing []Event) error {
	if !Schedulable(stage) {
		if !Valid(stage) {
			return fmt.Errorf("unknown stage %q", stage)
		}
		return fmt.Errorf("%s 不支持排期", Labels[stage])
	}
	candidate := Event{Stage: stage, PlannedDate: plannedDate}
	events := make([]Event, 0, len(existing)+1)
	events = append(events, existing...)
	events = append(events, candidate)

	violations, err := CheckOrder(events)
	if err != nil {
		return err
	}
	for _, violation := range violations {
		// Only report a violation this candidate takes part in; a pre-existing
		// inconsistency elsewhere is not this edit's fault.
		if violation.Earlier == stage || violation.Later == stage {
			return violation
		}
	}
	return nil
}

// AdvanceTo reports the stages crossed when content moves from one stage to
// another, so a caller can record completion for the steps skipped in between
// rather than leaving a gap in the history.
//
// Moving forward includes every stage strictly between the two; moving backward
// crosses nothing, because going back means the later work is being undone.
func AdvanceTo(from, to Stage) []Stage {
	fromRank, toRank := Rank(from), Rank(to)
	if fromRank < 0 || toRank < 0 || toRank <= fromRank {
		return nil
	}
	crossed := make([]Stage, 0, toRank-fromRank)
	for index := fromRank; index < toRank; index++ {
		crossed = append(crossed, Pipeline[index])
	}
	return crossed
}
