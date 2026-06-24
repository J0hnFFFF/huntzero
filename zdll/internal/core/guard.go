package core

import (
	"fmt"
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
