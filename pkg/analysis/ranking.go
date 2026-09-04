package analysis

import (
	"sort"

	"github.com/denjamio/azlens/pkg/domain"
)

const (
	MaxDefaultProblems = 3
	MaxDefaultWatching = 3
)

// Ranker evaluates internal ranking and caps user-facing output according to Section 15.
type Ranker struct{}

func NewRanker() *Ranker {
	return &Ranker{}
}

// Rank orders problems by operational impact and truncates beyond default limits.
func (r *Ranker) Rank(problems []domain.Problem, watching []domain.WatchingItem) ([]domain.Problem, []domain.WatchingItem) {
	// Compute internal rank score for each problem
	for i := range problems {
		problems[i].RankScore = computeRankScore(&problems[i])
	}

	// Deterministic sorting: higher RankScore first, then alphabetically by Summary
	sort.SliceStable(problems, func(i, j int) bool {
		if problems[i].RankScore != problems[j].RankScore {
			return problems[i].RankScore > problems[j].RankScore
		}
		return problems[i].Summary < problems[j].Summary
	})

	// Assign 1-indexed priorities
	for i := range problems {
		problems[i].Priority = i + 1
	}

	// Cap problems at MaxDefaultProblems
	visibleProblems := problems
	if len(visibleProblems) > MaxDefaultProblems {
		visibleProblems = visibleProblems[:MaxDefaultProblems]
	}

	// Deterministic sort for watching items
	sort.SliceStable(watching, func(i, j int) bool {
		return watching[i].Summary < watching[j].Summary
	})

	visibleWatching := watching
	if len(visibleWatching) > MaxDefaultWatching {
		visibleWatching = visibleWatching[:MaxDefaultWatching]
	}

	return visibleProblems, visibleWatching
}

func computeRankScore(p *domain.Problem) float64 {
	// Conceptually: impact x affected traffic x regression magnitude x persistence x evidence quality
	score := 100.0

	// Availability issues rank highest
	if p.Kind == domain.ProblemKindAvailability {
		score += 500.0
	}

	// Traffic percentage weighting
	if p.Impact.TrafficPct > 0 {
		score += p.Impact.TrafficPct * 5.0
	}

	// Evidence quality
	if p.Cause != nil {
		switch p.Cause.Strength {
		case domain.EvidenceStrengthStrong:
			score += 50.0
		case domain.EvidenceStrengthModerate:
			score += 25.0
		case domain.EvidenceStrengthWeak:
			score += 10.0
		}
	}

	return score
}
