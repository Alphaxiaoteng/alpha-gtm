package tools

import (
	"context"
	"testing"

	"alpha-nexus-egolite/internal/openbot"
)

func TestOpenBotDiscoveryToolReportsBothProducts(t *testing.T) {
	result, err := Default.Call(context.Background(), &Runtime{}, "agent.openbot.discover", nil)
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	instances, ok := result.Data["instances"].([]openbot.Status)
	if !ok || len(instances) != 2 {
		t.Fatalf("instances=%T %+v, want both products", result.Data["instances"], result.Data["instances"])
	}
	if instances[0].Kind != openbot.KindCopilotKit || instances[1].Kind != openbot.KindMeetOpenBot {
		t.Fatalf("unstable order: %+v", instances)
	}
}
