package analysis

import (
	"github.com/denjamio/azlens/pkg/analysis/detectors"
	"github.com/denjamio/azlens/pkg/domain"
)

// Engine coordinates telemetry analysis through the required pipeline (Section 12):
// Telemetry -> Signals -> Detectors -> Findings -> Correlation -> Problems -> Ranking -> Presentation
type Engine struct {
	detectors  *detectors.Registry
	correlator *Correlator
	ranker     *Ranker
	evaluator  *CapabilityEvaluator
}

// NewEngine creates a new analytical Engine.
func NewEngine(cfg detectors.Config) *Engine {
	return &Engine{
		detectors:  detectors.NewDefaultRegistry(cfg),
		correlator: NewCorrelator(),
		ranker:     NewRanker(),
		evaluator:  NewCapabilityEvaluator(),
	}
}

// Analyze processes a snapshot and yields a full AnalysisResult.
func (e *Engine) Analyze(snapshot *domain.Snapshot) *domain.AnalysisResult {
	// 1. Run all detectors -> Findings
	findings := e.detectors.Run(snapshot)

	// 2. Correlate findings -> Problems and Watching items
	problems, watching := e.correlator.Correlate(snapshot, findings)

	// 3. Rank problems and watching items -> Needs Attention vs Worth Watching
	rankedProblems, rankedWatching := e.ranker.Rank(problems, watching)

	// 4. Evaluate capabilities coverage
	coverage := e.evaluator.EvaluateCoverage(snapshot)

	// 5. Compute overall environment health state
	healthState, statusMsg := e.evaluator.DetermineHealthState(coverage, rankedProblems, findings)

	// Build scope context
	var roles, pods []string
	if snapshot.Scope.Role != "" {
		roles = []string{snapshot.Scope.Role}
	}
	if snapshot.Scope.Pod != "" {
		pods = []string{snapshot.Scope.Pod}
	}

	return &domain.AnalysisResult{
		SchemaVersion: "1",
		Profile:       snapshot.Profile,
		Scope: domain.ScopeContext{
			Roles: roles,
			Pods:  pods,
		},
		Window:        snapshot.Window,
		State:         healthState,
		Coverage:      coverage,
		Problems:      rankedProblems,
		Watching:      rankedWatching,
		StatusMessage: statusMsg,
	}
}
