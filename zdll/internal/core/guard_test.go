package core

import (
	"testing"
	"time"
)

func TestTerminationGuard_CheckSoftTermination(t *testing.T) {
	g := NewTerminationGuard(10, 100, time.Hour, 3)

	// Empty blackboard should not terminate.
	bb := &Blackboard{}
	if r := g.CheckSoftTermination("analysis is complete", bb, 0); r.Stop {
		t.Fatalf("unexpected soft termination on empty blackboard")
	}

	// Build a blackboard where all hypotheses are terminal.
	bb = &Blackboard{
		Hypotheses: map[string]*HypothesisNode{
			"H1": {ID: "H1", Status: HypothesisConfirmed, Confidence: 0.9},
			"H2": {ID: "H2", Status: HypothesisDiscarded, Confidence: 0.1},
			"H3": {ID: "H3", Status: HypothesisConfirmed, Confidence: 0.8},
			"H4": {ID: "H4", Status: HypothesisDiscarded, Confidence: 0.05},
			"H5": {ID: "H5", Status: HypothesisConfirmed, Confidence: 0.85},
		},
	}
	if r := g.CheckSoftTermination("analysis is complete", bb, 0); !r.Stop {
		t.Fatalf("expected soft termination when all hypotheses are terminal")
	}

	// Coverage below 80% should not terminate.
	bb = &Blackboard{
		Hypotheses: map[string]*HypothesisNode{
			"H1": {ID: "H1", Status: HypothesisConfirmed, Confidence: 0.9},
			"H2": {ID: "H2", Status: HypothesisActive, Confidence: 0.5},
			"H3": {ID: "H3", Status: HypothesisActive, Confidence: 0.5},
		},
	}
	if r := g.CheckSoftTermination("analysis is complete", bb, 0); r.Stop {
		t.Fatalf("unexpected soft termination with low coverage")
	}

	// Active work should block soft termination.
	bb = &Blackboard{
		Hypotheses: map[string]*HypothesisNode{
			"H1": {ID: "H1", Status: HypothesisConfirmed, Confidence: 0.9},
			"H2": {ID: "H2", Status: HypothesisDiscarded, Confidence: 0.1},
		},
	}
	if r := g.CheckSoftTermination("analysis is complete", bb, 1); r.Stop {
		t.Fatalf("unexpected soft termination with active work")
	}

	// No natural-language completion signal should block termination.
	bb = &Blackboard{
		Hypotheses: map[string]*HypothesisNode{
			"H1": {ID: "H1", Status: HypothesisConfirmed, Confidence: 0.9},
			"H2": {ID: "H2", Status: HypothesisDiscarded, Confidence: 0.1},
		},
	}
	if r := g.CheckSoftTermination("still investigating", bb, 0); r.Stop {
		t.Fatalf("unexpected soft termination without completion signal")
	}
}

func TestTerminationGuard_CheckSoftTermination_Stagnation(t *testing.T) {
	g := NewTerminationGuard(10, 100, time.Hour, 3)

	bb := &Blackboard{
		Hypotheses: map[string]*HypothesisNode{
			"H1": {ID: "H1", Status: HypothesisConfirmed, Confidence: 0.9},
			"H2": {ID: "H2", Status: HypothesisConfirmed, Confidence: 0.8},
			"H3": {ID: "H3", Status: HypothesisConfirmed, Confidence: 0.85},
			"H4": {ID: "H4", Status: HypothesisDiscarded, Confidence: 0.1},
			"H5": {ID: "H5", Status: HypothesisActive, Confidence: 0.5},
		},
	}
	// Coverage is 80%, one hypothesis is still active, so soft termination relies on stagnation.
	for i := 0; i < 4; i++ {
		if r := g.CheckSoftTermination("analysis is complete", bb, 0); r.Stop && i < 3 {
			t.Fatalf("unexpected soft termination at iteration %d", i)
		} else if !r.Stop && i >= 3 {
			t.Fatalf("expected soft termination after stagnation, got stop=%v at iteration %d", r.Stop, i)
		}
	}
}
