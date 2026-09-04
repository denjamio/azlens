package domain

import (
	"time"

	"github.com/denjamio/azlens/pkg/model"
)

// WorkloadStatus describes the health and replica status of a Kubernetes workload.
type WorkloadStatus struct {
	Name            string `json:"name"`
	Namespace       string `json:"namespace"`
	Kind            string `json:"kind"` // Deployment, StatefulSet, DaemonSet, etc.
	DesiredReplicas int32  `json:"desired_replicas"`
	ReadyReplicas   int32  `json:"ready_replicas"`
	PendingReplicas int32  `json:"pending_replicas"`
	Restarts        int32  `json:"restarts"`
	OOMKills        int32  `json:"oom_kills"`
	CrashLooping    bool   `json:"crash_looping"`
}

// PodRuntimeStatus describes the runtime status of an individual pod.
type PodRuntimeStatus struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Workload  string    `json:"workload"`
	Status    string    `json:"status"` // Running, Pending, CrashLoopBackOff, etc.
	Restarts  int32     `json:"restarts"`
	OOMKilled bool      `json:"oom_killed"`
	LastSeen  time.Time `json:"last_seen"`
}

// ResourceSaturation describes CPU and memory utilization.
type ResourceSaturation struct {
	Scope       Scope   `json:"scope"`
	CPUPct      float64 `json:"cpu_pct"`      // 0 - 100%
	MemoryPct   float64 `json:"memory_pct"`   // 0 - 100%
	HasData     bool    `json:"has_data"`
	IsSaturated bool    `json:"is_saturated"`
}

// AvailabilityMetric describes synthetic test or probe health.
type AvailabilityMetric struct {
	TestName    string  `json:"test_name"`
	TotalTests  int64   `json:"total_tests"`
	FailedTests int64   `json:"failed_tests"`
	SuccessRate float64 `json:"success_rate"` // 0.0 - 100.0%
	Message     string  `json:"message,omitempty"`
}

// FreshnessInfo tracks when telemetry signals were last observed.
type FreshnessInfo struct {
	RequestsLastSeen     *time.Time `json:"requests_last_seen,omitempty"`
	DependenciesLastSeen *time.Time `json:"dependencies_last_seen,omitempty"`
	ExceptionsLastSeen   *time.Time `json:"exceptions_last_seen,omitempty"`
	RuntimeLastSeen      *time.Time `json:"runtime_last_seen,omitempty"`
}

// Snapshot is a source-neutral snapshot of all observations required for an
// analysis window and scope (Section 16).
type Snapshot struct {
	Profile   ProfileContext `json:"profile"`
	Scope     Scope          `json:"scope"`
	Window    WindowContext  `json:"window"`
	Timestamp time.Time      `json:"timestamp"`

	// Baseline window telemetry (when comparing against baseline)
	BaselineOverall      *model.RequestMetric     `json:"baseline_overall,omitempty"`
	BaselineEndpoints    []model.RequestMetric    `json:"baseline_endpoints,omitempty"`
	BaselineDependencies []model.DependencyMetric `json:"baseline_dependencies,omitempty"`
	BaselineExceptions   []model.ErrorSummary     `json:"baseline_exceptions,omitempty"`
	BaselineFanout       []model.FanoutMetric     `json:"baseline_fanout,omitempty"`

	// Current window telemetry
	CurrentOverall      model.RequestMetric      `json:"current_overall"`
	CurrentEndpoints    []model.RequestMetric    `json:"current_endpoints"`
	CurrentDependencies []model.DependencyMetric `json:"current_dependencies"`
	CurrentExceptions   []model.ErrorSummary     `json:"current_exceptions"`
	CurrentFanout       []model.FanoutMetric     `json:"current_fanout"`

	// Availability & Runtime telemetry
	Availability []AvailabilityMetric `json:"availability,omitempty"`
	Workloads    []WorkloadStatus     `json:"workloads,omitempty"`
	Pods         []PodRuntimeStatus   `json:"pods,omitempty"`
	Saturation   []ResourceSaturation `json:"saturation,omitempty"`
	SlowLogs     []model.SlowLogGroup `json:"slow_logs,omitempty"`

	// Freshness and capability metadata
	Freshness              FreshnessInfo              `json:"freshness"`
	ConfiguredCapabilities map[CapabilityType]bool    `json:"configured_capabilities"`
	QueryErrors            map[CapabilityType]error   `json:"query_errors,omitempty"`
	CapabilityStates       map[CapabilityType]CapabilityState `json:"capability_states,omitempty"`
}

// NewSnapshot creates an empty Snapshot with initialized collections.
func NewSnapshot(profile ProfileContext, scope Scope, window WindowContext) *Snapshot {
	return &Snapshot{
		Profile:                profile,
		Scope:                  scope,
		Window:                 window,
		Timestamp:              time.Now(),
		CurrentEndpoints:       make([]model.RequestMetric, 0),
		CurrentDependencies:   make([]model.DependencyMetric, 0),
		CurrentExceptions:     make([]model.ErrorSummary, 0),
		CurrentFanout:         make([]model.FanoutMetric, 0),
		Availability:          make([]AvailabilityMetric, 0),
		Workloads:             make([]WorkloadStatus, 0),
		Pods:                  make([]PodRuntimeStatus, 0),
		Saturation:            make([]ResourceSaturation, 0),
		SlowLogs:              make([]model.SlowLogGroup, 0),
		ConfiguredCapabilities: make(map[CapabilityType]bool),
		QueryErrors:            make(map[CapabilityType]error),
		CapabilityStates:       make(map[CapabilityType]CapabilityState),
	}
}
