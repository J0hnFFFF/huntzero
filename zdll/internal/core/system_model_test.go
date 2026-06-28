package core

import (
	"path/filepath"
	"testing"

	"zdll/internal/eventbus"
)

func TestUpdateSystemModel_MergesAssumptions(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	model := &SystemModel{
		TrustBoundaries: []Boundary{{ID: "b1", Name: "HTTP", TrustedSide: "app", UntrustedSide: "internet"}},
		UntestedAssumptions: []Assumption{
			{Text: "only admins reach /admin"},
			{Text: " uploads are not executable"},
		},
	}
	if err := bm.UpdateSystemModel(model); err != nil {
		t.Fatalf("UpdateSystemModel: %v", err)
	}

	snap := bm.Snapshot()
	if len(snap.SystemModel.TrustBoundaries) != 1 {
		t.Errorf("expected 1 trust boundary, got %d", len(snap.SystemModel.TrustBoundaries))
	}
	if len(snap.SystemModel.UntestedAssumptions) != 2 {
		t.Errorf("expected 2 assumptions, got %d", len(snap.SystemModel.UntestedAssumptions))
	}

	// Duplicate assumption should be ignored.
	model2 := &SystemModel{
		UntestedAssumptions: []Assumption{{Text: "only admins reach /admin"}},
	}
	if err := bm.UpdateSystemModel(model2); err != nil {
		t.Fatalf("UpdateSystemModel: %v", err)
	}
	snap = bm.Snapshot()
	if len(snap.SystemModel.UntestedAssumptions) != 2 {
		t.Errorf("expected assumptions to remain 2 after duplicate, got %d", len(snap.SystemModel.UntestedAssumptions))
	}
}

func TestMarkAssumptionTested(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	id, _ := bm.AddAssumption("the parser rejects nested tags", 1)
	if err := bm.MarkAssumptionTested(id, "T-123"); err != nil {
		t.Fatalf("MarkAssumptionTested: %v", err)
	}

	snap := bm.Snapshot()
	var tested *Assumption
	for i := range snap.SystemModel.UntestedAssumptions {
		if snap.SystemModel.UntestedAssumptions[i].ID == id {
			tested = &snap.SystemModel.UntestedAssumptions[i]
			break
		}
	}
	if tested == nil {
		t.Fatal("assumption not found")
	}
	if !tested.Tested {
		t.Error("expected assumption to be marked tested")
	}
	if tested.Conclusion != AssumptionConclusionUnknown {
		t.Errorf("expected conclusion unknown when only marked tested, got %q", tested.Conclusion)
	}
	if len(tested.TestedBy) != 1 || tested.TestedBy[0] != "T-123" {
		t.Errorf("unexpected tested_by: %v", tested.TestedBy)
	}
}

func TestSetAssumptionConclusion(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	id, _ := bm.AddAssumption("only admins reach /admin", 1)
	if err := bm.SetAssumptionConclusion(id, AssumptionConclusionViolated, "T-999"); err != nil {
		t.Fatalf("SetAssumptionConclusion: %v", err)
	}

	snap := bm.Snapshot()
	var a *Assumption
	for i := range snap.SystemModel.UntestedAssumptions {
		if snap.SystemModel.UntestedAssumptions[i].ID == id {
			a = &snap.SystemModel.UntestedAssumptions[i]
			break
		}
	}
	if a == nil {
		t.Fatal("assumption not found")
	}
	if !a.Tested {
		t.Error("expected assumption to be marked tested")
	}
	if a.Conclusion != AssumptionConclusionViolated {
		t.Errorf("expected conclusion violated, got %q", a.Conclusion)
	}
	if len(a.TestedBy) != 1 || a.TestedBy[0] != "T-999" {
		t.Errorf("unexpected tested_by: %v", a.TestedBy)
	}

	// Re-setting the same task ID should not duplicate TestedBy.
	_ = bm.SetAssumptionConclusion(id, AssumptionConclusionViolated, "T-999")
	snap = bm.Snapshot()
	for i := range snap.SystemModel.UntestedAssumptions {
		if snap.SystemModel.UntestedAssumptions[i].ID == id {
			a = &snap.SystemModel.UntestedAssumptions[i]
			break
		}
	}
	if len(a.TestedBy) != 1 {
		t.Errorf("expected tested_by not to duplicate, got %v", a.TestedBy)
	}
}
