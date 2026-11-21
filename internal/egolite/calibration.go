package egolite

import (
	"context"
	"sort"
	"time"
)

var calibrationLevels = []int{1, 2, 4, 6, 8, 10, 12}

type ProbeResult struct {
	Failures  int
	Latencies []time.Duration
}

type CapacityProbe interface {
	Probe(context.Context, int) (ProbeResult, error)
}

type ProbeFunc func(context.Context, int) (ProbeResult, error)

func (function ProbeFunc) Probe(ctx context.Context, concurrency int) (ProbeResult, error) {
	return function(ctx, concurrency)
}

type CalibrationLevel struct {
	Concurrency int           `json:"concurrency"`
	Failures    int           `json:"failures"`
	P95         time.Duration `json:"p95"`
	Stable      bool          `json:"stable"`
}

type CalibrationResult struct {
	MeasuredCapacity   int                `json:"measured_capacity"`
	DefaultConcurrency int                `json:"default_concurrency"`
	Levels             []CalibrationLevel `json:"levels"`
}

type Calibrator struct {
	probe CapacityProbe
}

func NewCalibrator(probe CapacityProbe) *Calibrator {
	return &Calibrator{probe: probe}
}

func (calibrator *Calibrator) Run(ctx context.Context) (*CalibrationResult, error) {
	result := &CalibrationResult{MeasuredCapacity: 1, DefaultConcurrency: 1}
	for _, concurrency := range calibrationLevels {
		level := CalibrationLevel{Concurrency: concurrency}
		var latencies []time.Duration
		for range 3 {
			probeResult, err := calibrator.probe.Probe(ctx, concurrency)
			if err != nil {
				return nil, err
			}
			level.Failures += probeResult.Failures
			latencies = append(latencies, probeResult.Latencies...)
		}
		level.P95 = percentile95(latencies)
		level.Stable = level.Failures == 0 && level.P95 <= 5*time.Second
		result.Levels = append(result.Levels, level)
		if !level.Stable {
			break
		}
		result.MeasuredCapacity = concurrency
	}
	if result.MeasuredCapacity >= 3 {
		result.DefaultConcurrency = 3
	} else {
		result.DefaultConcurrency = result.MeasuredCapacity
	}
	return result, nil
}

func percentile95(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := (95*len(sorted) + 99) / 100
	if index < 1 {
		index = 1
	}
	return sorted[index-1]
}
