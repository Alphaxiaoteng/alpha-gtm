package egolite_test

import (
	"context"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/egolite"
)

type calibrationProbe struct {
	failAt int
	calls  []int
}

func (probe *calibrationProbe) Probe(_ context.Context, concurrency int) (egolite.ProbeResult, error) {
	probe.calls = append(probe.calls, concurrency)
	if concurrency >= probe.failAt {
		return egolite.ProbeResult{Failures: 1, Latencies: []time.Duration{time.Second}}, nil
	}
	return egolite.ProbeResult{Latencies: []time.Duration{500 * time.Millisecond, time.Second}}, nil
}

func TestCalibratorSelectsHighestStableLevelAndSafeDefault(t *testing.T) {
	probe := &calibrationProbe{failAt: 8}
	calibrator := egolite.NewCalibrator(probe)

	result, err := calibrator.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.MeasuredCapacity != 6 {
		t.Fatalf("MeasuredCapacity = %d, want 6", result.MeasuredCapacity)
	}
	if result.DefaultConcurrency != 3 {
		t.Fatalf("DefaultConcurrency = %d, want 3", result.DefaultConcurrency)
	}
	if len(probe.calls) != 15 {
		t.Fatalf("probe calls = %v, want three rounds at 1,2,4,6,8", probe.calls)
	}
}

func TestCalibratorKeepsConcurrencyOneWhenFirstLevelIsSlow(t *testing.T) {
	probe := egolite.ProbeFunc(func(_ context.Context, concurrency int) (egolite.ProbeResult, error) {
		return egolite.ProbeResult{Latencies: []time.Duration{6 * time.Second}}, nil
	})
	calibrator := egolite.NewCalibrator(probe)

	result, err := calibrator.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.MeasuredCapacity != 1 || result.DefaultConcurrency != 1 {
		t.Fatalf("result = %+v, want capacity/default 1", result)
	}
}
