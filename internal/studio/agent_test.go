package studio

import (
	"context"
	"errors"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestDoubaoAgentPlansStrictJSONWithoutNetwork(t *testing.T) {
	agent := NewDoubaoAgentWithCompletion(func(context.Context, string, string) (string, error) {
		return "```json\n{\"start_ms\": 1000, \"end_ms\": 59000, \"title\": \"开场\", \"reason\": \"脚本开场段落完整\"}\n```", nil
	})
	plan, err := agent.Plan(context.Background(), db.MediaLibraryItem{ID: 1, Mime: "video/mp4", DurationMS: 90_000}, db.StudioScript{ID: 1, Title: "脚本", Body: "开场内容"}, 60_000)
	if err != nil || plan.StartMS != 1000 || plan.EndMS != 59000 || plan.Title != "开场" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestDoubaoAgentRejectsInvalidOrUnavailablePlan(t *testing.T) {
	agent := NewDoubaoAgentWithCompletion(func(context.Context, string, string) (string, error) {
		return "{\"start_ms\":0,\"end_ms\":190000,\"title\":\"过长\",\"reason\":\"x\"}", nil
	})
	_, err := agent.Plan(context.Background(), db.MediaLibraryItem{ID: 1, Mime: "video/mp4"}, db.StudioScript{ID: 1, Title: "脚本", Body: "内容"}, 60_000)
	if err == nil || !strings.Contains(err.Error(), "DOUBAO_API_INVALID_PLAN") {
		t.Fatalf("invalid plan error=%v", err)
	}
	agent = NewDoubaoAgentWithCompletion(func(context.Context, string, string) (string, error) {
		return "", errors.New("connection refused")
	})
	_, err = agent.Plan(context.Background(), db.MediaLibraryItem{ID: 1, Mime: "video/mp4"}, db.StudioScript{ID: 1, Title: "脚本", Body: "内容"}, 60_000)
	if err == nil || !strings.Contains(err.Error(), "DOUBAO_API_UNAVAILABLE") {
		t.Fatalf("unavailable error=%v", err)
	}
}
