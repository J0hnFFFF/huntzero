package core

import (
	"fmt"
	"strings"
	"time"
)

// TerminationGuard decides when the analysis should stop.
type TerminationGuard struct {
	MaxRounds   int
	MaxTasks    int
	MaxWallTime time.Duration
	Stagnation  int

	start          time.Time
	lastFindings   int
	lastEvidence   int
	stagnantRounds int
}

// GuardResult is the outcome of a termination check.
type GuardResult struct {
	Stop   bool
	Reason string
}

func NewTerminationGuard(maxRounds, maxTasks int, maxWallTime time.Duration, stagnation int) *TerminationGuard {
	return &TerminationGuard{
		MaxRounds:   maxRounds,
		MaxTasks:    maxTasks,
		MaxWallTime: maxWallTime,
		Stagnation:  stagnation,
		start:       time.Now(),
	}
}

func (g *TerminationGuard) Check(round int, bb *Blackboard, activeDrones int) GuardResult {
	// Budget checks.
	if round > g.MaxRounds {
		return GuardResult{Stop: true, Reason: fmt.Sprintf("budget: max_rounds=%d reached", g.MaxRounds)}
	}
	if bb.TotalTasks >= g.MaxTasks {
		return GuardResult{Stop: true, Reason: fmt.Sprintf("budget: max_tasks=%d reached", g.MaxTasks)}
	}
	if time.Since(g.start) >= g.MaxWallTime {
		return GuardResult{Stop: true, Reason: fmt.Sprintf("budget: max_wall_time=%s reached", g.MaxWallTime)}
	}

	stats := bb.Stats()
	confirmed := stats["confirmed"]
	discarded := stats["discarded"]
	total := stats["total_hypotheses"]

	// Convergence: all hypotheses terminal and no active work (only meaningful once work has started).
	if total > 0 && confirmed+discarded == total && activeDrones == 0 && len(bb.Tasks) == 0 {
		return GuardResult{Stop: true, Reason: "converged: all hypotheses terminal and no active work"}
	}

	// Stagnation: findings + evidence unchanged for N rounds when no active work.
	if activeDrones == 0 {
		currentEvidence := 0
		for _, h := range bb.Hypotheses {
			currentEvidence += len(h.Evidence)
		}
		if confirmed == g.lastFindings && currentEvidence == g.lastEvidence {
			g.stagnantRounds++
		} else {
			g.stagnantRounds = 0
			g.lastFindings = confirmed
			g.lastEvidence = currentEvidence
		}
		if g.stagnantRounds >= g.Stagnation {
			return GuardResult{Stop: true, Reason: fmt.Sprintf("stagnation: no progress for %d rounds", g.Stagnation)}
		}
	}

	return GuardResult{Stop: false}
}

// CheckSoftTermination implements the Python _check_soft_termination heuristic.
// It triggers when the latest model output contains a natural-language completion
// signal, hypothesis coverage is high (>=80%), no work is in flight, and either
// all hypotheses are terminal or findings have stalled for several rounds.
func (g *TerminationGuard) CheckSoftTermination(text string, bb *Blackboard, activeDrones int) GuardResult {
	if activeDrones > 0 || len(bb.Tasks) > 0 {
		return GuardResult{Stop: false}
	}

	total := len(bb.Hypotheses)
	if total == 0 {
		return GuardResult{Stop: false}
	}
	terminal := 0
	for _, h := range bb.Hypotheses {
		if h.Status == HypothesisConfirmed || h.Status == HypothesisDiscarded {
			terminal++
		}
	}
	coverage := float64(terminal) / float64(total)
	if coverage < 0.8 {
		return GuardResult{Stop: false}
	}
	if !hasCompletionSignal(text) {
		return GuardResult{Stop: false}
	}

	nonTerminal := total - terminal
	if nonTerminal <= 0 {
		return GuardResult{Stop: true, Reason: fmt.Sprintf("soft-termination: coverage=%.0f%%, all hypotheses terminal", coverage*100)}
	}

	stats := bb.Stats()
	confirmed := stats["confirmed"]
	currentEvidence := 0
	for _, h := range bb.Hypotheses {
		currentEvidence += len(h.Evidence)
	}
	if confirmed == g.lastFindings && currentEvidence == g.lastEvidence {
		g.stagnantRounds++
	} else {
		g.stagnantRounds = 0
		g.lastFindings = confirmed
		g.lastEvidence = currentEvidence
	}
	if g.stagnantRounds >= 3 {
		return GuardResult{Stop: true, Reason: fmt.Sprintf("soft-termination: coverage=%.0f%%, findings stalled for %d rounds", coverage*100, g.stagnantRounds)}
	}
	return GuardResult{Stop: false}
}

var completionPhrases = []string{
	"analysis is complete",
	"analysis complete",
	"investigation is complete",
	"no further action",
	"no additional action",
	"no more action",
	"we can conclude",
	"i conclude",
	"concluded that",
	"search is complete",
	"review is complete",
	"all hypotheses",
	"all pending hypotheses",
	"no remaining hypotheses",
	"sufficient evidence",
	"we are done",
}

func hasCompletionSignal(text string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range completionPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
