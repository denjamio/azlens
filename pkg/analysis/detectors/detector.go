package detectors

import (
	"math"
	"time"

	"github.com/denjamio/azlens/pkg/domain"
)

// Detector defines the contract for an individual analytical detector (Section 13).
// A detector:
// - consumes source-neutral telemetry/snapshots;
// - emits zero or more findings;
// - never prints;
// - never parses CLI flags;
// - never queries Azure directly;
// - never decides final user-facing wording.
type Detector interface {
	Name() string
	Detect(snapshot *domain.Snapshot) []domain.Finding
}

// Config configures thresholds and policies for detectors.
type Config struct {
	LatencyWarnPct     float64
	LatencyCritPct     float64
	ErrorRateWarnDelta float64
	ErrorRateCritDelta float64
	MinSampleCalls     int64
	StaleDuration      time.Duration
}

// DefaultConfig returns sensible defaults adhering to convention over configuration.
func DefaultConfig() Config {
	return Config{
		LatencyWarnPct:     15.0,
		LatencyCritPct:     30.0,
		ErrorRateWarnDelta: 1.0,
		ErrorRateCritDelta: 3.0,
		MinSampleCalls:     5,
		StaleDuration:      15 * time.Minute,
	}
}

// Registry manages the active detector suite.
type Registry struct {
	detectors []Detector
}

// NewDefaultRegistry constructs a registry containing all 13 v0.1 detectors.
func NewDefaultRegistry(cfg Config) *Registry {
	return &Registry{
		detectors: []Detector{
			NewTelemetryStaleDetector(cfg),
			NewAvailabilityFailureDetector(cfg),
			NewWorkloadUnavailableDetector(cfg),
			NewOOMKilledDetector(cfg),
			NewRestartBurstDetector(cfg),
			NewResourceSaturationDetector(cfg),
			NewRequestLatencyRegressionDetector(cfg),
			NewRequestErrorRegressionDetector(cfg),
			NewNewExceptionDetector(cfg),
			NewExceptionRegressionDetector(cfg),
			NewDependencyLatencyRegressionDetector(cfg),
			NewDependencyErrorRegressionDetector(cfg),
			NewDependencyFanoutRegressionDetector(cfg),
		},
	}
}

// Run executes all registered detectors against the snapshot and returns aggregated findings.
func (r *Registry) Run(snapshot *domain.Snapshot) []domain.Finding {
	var allFindings []domain.Finding
	for _, d := range r.detectors {
		findings := d.Detect(snapshot)
		if len(findings) > 0 {
			allFindings = append(allFindings, findings...)
		}
	}
	return allFindings
}

// Helper: calcPctChange calculates percentage difference from baseline to current
func calcPctChange(baseline, current float64) float64 {
	if baseline <= 0 {
		if current <= 0 {
			return 0
		}
		return 100.0
	}
	return ((current - baseline) / baseline) * 100.0
}

// Helper: roundFloat rounds float to n decimal places
func roundFloat(val float64, precision int) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}
