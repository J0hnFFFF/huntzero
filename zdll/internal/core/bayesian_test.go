package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBayesianEngine_PropagateAll_RootUnchanged(t *testing.T) {
	e := NewBayesianEngine(t.TempDir())
	h := &HypothesisNode{ID: "H1", Confidence: 0.8}
	posteriors := e.PropagateAll(map[string]*HypothesisNode{"H1": h})
	if got := posteriors["H1"]; got != 0.8 {
		t.Fatalf("expected root confidence unchanged, got %v", got)
	}
}

func TestBayesianEngine_PropagateAll_ChildUsesDefaultStrength(t *testing.T) {
	e := NewBayesianEngine(t.TempDir())
	parentID := "H1"
	h1 := &HypothesisNode{ID: "H1", Confidence: 0.9}
	h2 := &HypothesisNode{ID: "H2", Confidence: 0.5, ParentID: &parentID}

	posteriors := e.PropagateAll(map[string]*HypothesisNode{"H1": h1, "H2": h2})
	// propagated = 0.9 * 0.85 = 0.765; posterior = 0.6*0.765 + 0.4*0.5 = 0.659
	if got := posteriors["H2"]; absFloat(got-0.659) > 0.001 {
		t.Fatalf("expected child posterior ~0.659, got %v", got)
	}
}

func TestBayesianEngine_RecordOutcome_StoresPair(t *testing.T) {
	e := NewBayesianEngine(t.TempDir())
	parentID := "H1"
	h1 := &HypothesisNode{ID: "H1", Confidence: 0.9}
	h2 := &HypothesisNode{ID: "H2", Confidence: 0.5, ParentID: &parentID}

	// Within a single scan each hypothesis is recorded once.
	e.RecordOutcome(h1, true)
	e.RecordOutcome(h2, true)

	entry, ok := e.strengthMatrix["H1->H2"]
	if !ok {
		t.Fatal("expected strength entry for H1->H2")
	}
	if entry.N != 1 {
		t.Fatalf("expected n=1 for first encounter, got %d", entry.N)
	}
	// Not enough samples yet, so effective strength falls back to default.
	posteriors := e.PropagateAll(map[string]*HypothesisNode{"H1": h1, "H2": h2})
	if got := posteriors["H2"]; absFloat(got-0.659) > 0.001 {
		t.Fatalf("expected child posterior ~0.659 with default strength, got %v", got)
	}
}

func TestBayesianEngine_LearnedStrengthAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	// Simulate a previous run that collected 3 samples for the same pair.
	path := filepath.Join(dir, ".bayesian_strength.json")
	data := []byte(`{
  "strength_matrix": {
    "H1->H2": {"strength": 0.98, "n": 3, "base_rate": 0.75}
  },
  "meta": {"total_outcomes": 3, "default_strength": 0.85}
}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write strength file: %v", err)
	}

	e := NewBayesianEngine(dir)
	parentID := "H1"
	h1 := &HypothesisNode{ID: "H1", Confidence: 0.9}
	h2 := &HypothesisNode{ID: "H2", Confidence: 0.5, ParentID: &parentID}

	posteriors := e.PropagateAll(map[string]*HypothesisNode{"H1": h1, "H2": h2})
	// propagated = 0.9 * 0.98 = 0.882; posterior = 0.6*0.882 + 0.4*0.5 = 0.7292
	if got := posteriors["H2"]; absFloat(got-0.7292) > 0.001 {
		t.Fatalf("expected child posterior ~0.7292 with learned strength, got %v", got)
	}
}

func TestBayesianEngine_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	e1 := NewBayesianEngine(dir)
	parentID := "H1"
	h1 := &HypothesisNode{ID: "H1", Confidence: 0.9}
	h2 := &HypothesisNode{ID: "H2", Confidence: 0.5, ParentID: &parentID}
	for i := 0; i < 4; i++ {
		e1.RecordOutcome(h1, true)
		e1.RecordOutcome(h2, true)
	}
	if err := e1.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	e2 := NewBayesianEngine(dir)
	entry, ok := e2.strengthMatrix["H1->H2"]
	if !ok {
		t.Fatal("expected strength matrix to be loaded")
	}
	if entry.N != e1.strengthMatrix["H1->H2"].N {
		t.Fatalf("loaded n mismatch: got %d want %d", entry.N, e1.strengthMatrix["H1->H2"].N)
	}
}

func TestBayesianEngine_CycleHandled(t *testing.T) {
	e := NewBayesianEngine(t.TempDir())
	id1 := "H1"
	id2 := "H2"
	h1 := &HypothesisNode{ID: "H1", Confidence: 0.8, ParentID: &id2}
	h2 := &HypothesisNode{ID: "H2", Confidence: 0.6, ParentID: &id1}

	// Should not panic or recurse infinitely.
	posteriors := e.PropagateAll(map[string]*HypothesisNode{"H1": h1, "H2": h2})
	if _, ok := posteriors["H1"]; !ok {
		t.Fatal("expected posterior for H1")
	}
	if _, ok := posteriors["H2"]; !ok {
		t.Fatal("expected posterior for H2")
	}
}

func TestBayesianEngine_UpdateConfidences(t *testing.T) {
	dir := t.TempDir()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, nil)
	_ = bm.Init(dir)

	parentID, _ := bm.AddHypothesis("parent", 0.9, nil)
	childID, _ := bm.AddHypothesis("child", 0.5, &parentID)

	e := NewBayesianEngine(dir)
	ph := bm.Snapshot().Hypotheses[parentID]
	ch := bm.Snapshot().Hypotheses[childID]
	e.RecordOutcome(ph, true)
	e.RecordOutcome(ch, true)

	if n := e.UpdateConfidences(bm); n == 0 {
		t.Fatal("expected at least one confidence update")
	}
	snap := bm.Snapshot()
	if snap.Hypotheses[parentID].Confidence != 0.9 {
		t.Fatalf("expected parent confidence unchanged, got %v", snap.Hypotheses[parentID].Confidence)
	}
}
