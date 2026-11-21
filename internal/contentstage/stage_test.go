package contentstage

import (
	"strings"
	"testing"
)

func TestRankFollowsPipelineOrder(t *testing.T) {
	for index, stage := range Pipeline {
		if got := Rank(stage); got != index {
			t.Errorf("Rank(%q) = %d, want %d", stage, got, index)
		}
	}
	if Rank("nope") != -1 {
		t.Error("unknown stage should rank -1")
	}
}

func TestSchedulableExcludesHoldAndEndStates(t *testing.T) {
	for _, stage := range []Stage{StageInbox, StageArchived} {
		if Schedulable(stage) {
			t.Errorf("%s must not be schedulable", stage)
		}
	}
	for _, stage := range []Stage{StageTopic, StageScript, StageRecording, StageEditing, StagePublishing, StageReview} {
		if !Schedulable(stage) {
			t.Errorf("%s should be schedulable", stage)
		}
	}
}

func TestCheckOrderAcceptsMonotonicSchedule(t *testing.T) {
	events := []Event{
		{Stage: StageTopic, PlannedDate: "2026-03-01"},
		{Stage: StageScript, PlannedDate: "2026-03-03"},
		{Stage: StageRecording, PlannedDate: "2026-03-05"},
	}
	violations, err := CheckOrder(events)
	if err != nil {
		t.Fatalf("CheckOrder: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations, got %+v", violations)
	}
}

func TestCheckOrderRejectsBackwardsSchedule(t *testing.T) {
	events := []Event{
		{Stage: StageTopic, PlannedDate: "2026-03-10"},
		{Stage: StageScript, PlannedDate: "2026-03-05"},
	}
	violations, err := CheckOrder(events)
	if err != nil {
		t.Fatalf("CheckOrder: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(violations), violations)
	}
	if violations[0].Earlier != StageTopic || violations[0].Later != StageScript {
		t.Errorf("wrong pair: %+v", violations[0])
	}
	if !strings.Contains(violations[0].Error(), "选题") {
		t.Errorf("message should name the stages: %q", violations[0].Error())
	}
}

func TestCheckOrderIgnoresCompletedAndUnscheduled(t *testing.T) {
	events := []Event{
		{Stage: StageTopic, PlannedDate: "2026-03-10", Completed: true},
		{Stage: StageScript, PlannedDate: "2026-03-05"},
		{Stage: StageRecording, PlannedDate: ""},
	}
	violations, err := CheckOrder(events)
	if err != nil {
		t.Fatalf("CheckOrder: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("completed history and unscheduled stages must not be judged: %+v", violations)
	}
}

func TestCheckOrderAllowsSameDay(t *testing.T) {
	events := []Event{
		{Stage: StageScript, PlannedDate: "2026-03-01"},
		{Stage: StageEditing, PlannedDate: "2026-03-01"},
	}
	violations, err := CheckOrder(events)
	if err != nil {
		t.Fatalf("CheckOrder: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("same-day stages are fine: %+v", violations)
	}
}

func TestCheckOrderRejectsBadDate(t *testing.T) {
	if _, err := CheckOrder([]Event{{Stage: StageTopic, PlannedDate: "明天"}}); err == nil {
		t.Fatal("non-date should be rejected")
	}
}

func TestParseDateAcceptsDatetimeInput(t *testing.T) {
	parsed, err := ParseDate("2026-03-01T14:30")
	if err != nil {
		t.Fatalf("ParseDate: %v", err)
	}
	if parsed.Format("2006-01-02") != "2026-03-01" {
		t.Errorf("got %v", parsed)
	}
}

func TestCanScheduleRejectsUnknownStage(t *testing.T) {
	if err := CanSchedule("nope", "2026-03-01", nil); err == nil {
		t.Fatal("unknown stage should be rejected")
	}
}

func TestCanScheduleRejectsInbox(t *testing.T) {
	if err := CanSchedule(StageInbox, "2026-03-01", nil); err == nil {
		t.Fatal("inbox must not be schedulable")
	}
}

func TestCanScheduleRejectsOrderConflict(t *testing.T) {
	existing := []Event{{Stage: StageTopic, PlannedDate: "2026-03-10"}}
	if err := CanSchedule(StageScript, "2026-03-05", existing); err == nil {
		t.Fatal("script before topic should be refused")
	}
}

func TestCanScheduleIgnoresUnrelatedViolation(t *testing.T) {
	// 已有数据本身不合法，但本次编辑没有参与其中，不应被拦。
	existing := []Event{
		{Stage: StageTopic, PlannedDate: "2026-03-10"},
		{Stage: StageScript, PlannedDate: "2026-03-01"},
	}
	if err := CanSchedule(StageReview, "2026-03-20", existing); err != nil {
		t.Errorf("unrelated pre-existing conflict should not block this edit: %v", err)
	}
}

func TestAdvanceToCrossesSkippedStages(t *testing.T) {
	crossed := AdvanceTo(StageTopic, StageEditing)
	want := []Stage{StageTopic, StageScript, StageRecording}
	if len(crossed) != len(want) {
		t.Fatalf("got %v, want %v", crossed, want)
	}
	for index := range want {
		if crossed[index] != want[index] {
			t.Errorf("crossed[%d] = %q, want %q", index, crossed[index], want[index])
		}
	}
}

func TestAdvanceToBackwardsCrossesNothing(t *testing.T) {
	if crossed := AdvanceTo(StageEditing, StageTopic); len(crossed) != 0 {
		t.Errorf("moving back should not auto-complete anything, got %v", crossed)
	}
	if crossed := AdvanceTo(StageTopic, StageTopic); len(crossed) != 0 {
		t.Errorf("same stage should cross nothing, got %v", crossed)
	}
}

func TestAdvanceToUnknownStage(t *testing.T) {
	if crossed := AdvanceTo("nope", StageEditing); len(crossed) != 0 {
		t.Errorf("unknown stage should cross nothing, got %v", crossed)
	}
}
