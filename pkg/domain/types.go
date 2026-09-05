// Package domain defines the core domain models for AzLens:
// health states, capabilities, signals, findings, problems, causes,
// evidence, actions, and analysis results.
//
// Per Section 17, domain has no dependencies on CLI rendering, KQL, or Azure SDKs.
package domain

import (
	"time"
)

// HealthState represents the environment health state.
// AzLens has exactly three health states in v0.1 (Section 7).
type HealthState string

const (
	HealthStateHealthy  HealthState = "healthy"
	HealthStateDegraded HealthState = "degraded"
	HealthStateUnknown  HealthState = "unknown"
)

// CapabilityType identifies an observable telemetry capability.
type CapabilityType string

const (
	CapabilityRequests         CapabilityType = "requests"
	CapabilityDependencies     CapabilityType = "dependencies"
	CapabilityExceptions       CapabilityType = "exceptions"
	CapabilityAvailability     CapabilityType = "availability"
	CapabilityDatabaseSlowLogs CapabilityType = "database_slow_logs"
)

// CapabilityState represents the reachability or freshness state of a capability.
type CapabilityState string

const (
	CapabilityStateAvailable     CapabilityState = "available"
	CapabilityStateUnavailable   CapabilityState = "unavailable"
	CapabilityStateNotConfigured CapabilityState = "not_configured"
	CapabilityStateStale         CapabilityState = "stale"
)

// CapabilityStatus describes the status of an observable capability.
type CapabilityStatus struct {
	Capability  CapabilityType  `json:"capability"`
	State       CapabilityState `json:"state"`
	Reason      string          `json:"reason,omitempty"`
	Consequence string          `json:"consequence,omitempty"`
	LastSeen    *time.Time      `json:"last_seen,omitempty"`
}

// Scope defines the targeted operational boundaries (service, role, endpoint, pod, database, etc.).
type Scope struct {
	Service  string `json:"service,omitempty"`
	Role     string `json:"role,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Target   string `json:"target,omitempty"`
	Pod      string `json:"pod,omitempty"`
	Database string `json:"database,omitempty"`
}

// String returns a human-friendly representation of the scope.
func (s Scope) String() string {
	if s.Endpoint != "" {
		return s.Endpoint
	}
	if s.Service != "" {
		return s.Service
	}
	if s.Role != "" {
		return s.Role
	}
	if s.Target != "" {
		return s.Target
	}
	if s.Pod != "" {
		return s.Pod
	}
	if s.Database != "" {
		return s.Database
	}
	return ""
}

// Value represents a measurement value with optional unit and formatted text.
type Value struct {
	Val  float64 `json:"val"`
	Text string  `json:"text,omitempty"`
	Unit string  `json:"unit,omitempty"`
}

// Change represents the difference and percentage change between baseline and current.
type Change struct {
	Delta   float64 `json:"delta"`
	Pct     float64 `json:"pct"`
	Summary string  `json:"summary,omitempty"`
}

// Evidence represents factual support for a conclusion (Section 12.5).
type Evidence struct {
	Signal   string  `json:"signal"`
	Current  Value   `json:"current"`
	Baseline *Value  `json:"baseline,omitempty"`
	Change   *Change `json:"change,omitempty"`
	Scope    Scope   `json:"scope,omitempty"`
}

// EvidenceStrength indicates how strongly evidence supports a cause conclusion (Section 12.4).
type EvidenceStrength string

const (
	EvidenceStrengthStrong   EvidenceStrength = "strong"
	EvidenceStrengthModerate EvidenceStrength = "moderate"
	EvidenceStrengthWeak     EvidenceStrength = "weak"
)

// Cause represents the likely root cause of a problem (Section 12.4).
type Cause struct {
	Summary  string           `json:"summary"`
	Subject  string           `json:"subject,omitempty"`
	Strength EvidenceStrength `json:"strength"`
	Evidence []Evidence       `json:"evidence,omitempty"`
}

// Command represents a recommended CLI invocation.
type Command struct {
	Display string   `json:"display"`
	Args    []string `json:"args,omitempty"`
}

// Action represents the single preferred next action (Section 12.6).
type Action struct {
	Summary string   `json:"summary"`
	Command *Command `json:"command,omitempty"`
}

// Impact summarizes user/operational impact of a problem.
type Impact struct {
	Summary       string  `json:"summary,omitempty"`
	P95Current    string  `json:"p95_current,omitempty"`
	P95Baseline   string  `json:"p95_baseline,omitempty"`
	ErrorCurrent  string  `json:"error_current,omitempty"`
	ErrorBaseline string  `json:"error_baseline,omitempty"`
	TrafficPct    float64 `json:"traffic_pct,omitempty"`
	TotalCalls    int64   `json:"total_calls,omitempty"`
}

// FindingKind identifies the type of finding emitted by a detector.
type FindingKind string

const (
	FindingRequestLatencyRegression    FindingKind = "request_latency_regression"
	FindingRequestErrorRegression      FindingKind = "request_error_regression"
	FindingNewException                FindingKind = "new_exception"
	FindingExceptionRegression         FindingKind = "exception_regression"
	FindingDependencyLatencyRegression FindingKind = "dependency_latency_regression"
	FindingDependencyErrorRegression   FindingKind = "dependency_error_regression"
	FindingDependencyFanoutRegression  FindingKind = "dependency_fanout_regression"
	FindingSQLFanout                   FindingKind = "sql_fanout"
	FindingNPlusOneCandidate           FindingKind = "n_plus_one_candidate"
	FindingTelemetryStale              FindingKind = "telemetry_stale"
	FindingAvailabilityFailure         FindingKind = "availability_failure"
)

// Finding is an internal interpretation of signals emitted by a detector (Section 12.2).
type Finding struct {
	Kind        FindingKind `json:"kind"`
	Scope       Scope       `json:"scope"`
	Summary     string      `json:"summary"`
	StartedAt   *time.Time  `json:"started_at,omitempty"`
	Evidence    []Evidence  `json:"evidence,omitempty"`
	Severity    string      `json:"severity,omitempty"`
	RawMetric   string      `json:"raw_metric,omitempty"`
	SampleCount int64       `json:"sample_count,omitempty"`
}

// ProblemKind identifies the user-visible category of an operational problem.
type ProblemKind string

const (
	ProblemKindDegradation  ProblemKind = "degradation"
	ProblemKindAvailability ProblemKind = "availability"
	ProblemKindErrorSpike   ProblemKind = "error_spike"
	ProblemKindRuntimeRisk  ProblemKind = "runtime_risk"
	ProblemKindTelemetry    ProblemKind = "telemetry"
)

// Problem represents a correlated operational issue shown to the user (Section 12.3).
type Problem struct {
	ID   string      `json:"id,omitempty"`
	Kind ProblemKind `json:"kind"`
	// Priority: 1 = highest
	Priority  int        `json:"priority"`
	Scope     Scope      `json:"scope"`
	Summary   string     `json:"summary"`
	Impact    Impact     `json:"impact"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	Symptoms  []Finding  `json:"symptoms,omitempty"`
	Cause     *Cause     `json:"cause,omitempty"`
	Action    *Action    `json:"action,omitempty"`
	// Internal ranking score, never exposed in JSON/human
	RankScore float64 `json:"-"`
}

// WatchingItem represents an unusual or risky operational signal without enough impact (Section 8).
type WatchingItem struct {
	Summary   string     `json:"summary"`
	Detail    string     `json:"detail,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	Scope     Scope      `json:"scope,omitempty"`
}

// ProfileContext represents context headers for JSON output (Section 18.2).
type ProfileContext struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
}

type ScopeContext struct {
	Service string `json:"service,omitempty"`
	Role    string `json:"role,omitempty"`
	Pod     string `json:"pod,omitempty"`
}

type WindowContext struct {
	Label    string    `json:"label,omitempty"`
	Duration string    `json:"duration,omitempty"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
}

// AnalysisResult represents the top-level machine and human contract (Section 18.2).
type AnalysisResult struct {
	SchemaVersion string             `json:"schema_version"`
	Profile       ProfileContext     `json:"profile"`
	Scope         ScopeContext       `json:"scope"`
	Window        WindowContext      `json:"window"`
	State         HealthState        `json:"state"`
	Coverage      []CapabilityStatus `json:"coverage"`
	Problems      []Problem          `json:"problems"`
	Watching      []WatchingItem     `json:"watching"`
	StatusMessage string             `json:"status_message,omitempty"`
}
