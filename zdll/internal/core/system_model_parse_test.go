package core

import (
	"context"
	"path/filepath"
	"testing"

	"zdll/internal/eventbus"
)

func TestParseAndApply_SystemModel(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	resp := `{
  "hypotheses": [],
  "tasks": [],
  "findings": [],
  "system_model": {
    "trust_boundaries": [
      {"id":"b1","name":"HTTP","trusted_side":"app","untrusted_side":"internet","description":"frontier"}
    ],
    "invariants": [
      {"id":"i1","statement":"files never reach shell","tested":false}
    ]
  },
  "untested_assumptions": ["only admins reach /admin"]
}`

	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, NewPlainSkillFS(dir), dir, dir, bus)
	_, _, _, err := engine.parseAndApply(context.Background(), bm, resp)
	if err != nil {
		t.Fatalf("parseAndApply: %v", err)
	}

	snap := bm.Snapshot()
	if snap.SystemModel == nil {
		t.Fatal("system model not persisted")
	}
	if len(snap.SystemModel.TrustBoundaries) != 1 {
		t.Errorf("expected 1 trust boundary, got %d", len(snap.SystemModel.TrustBoundaries))
	}
	if len(snap.SystemModel.Invariants) != 1 {
		t.Errorf("expected 1 invariant, got %d", len(snap.SystemModel.Invariants))
	}
	if len(snap.SystemModel.UntestedAssumptions) != 1 {
		t.Errorf("expected 1 assumption, got %d", len(snap.SystemModel.UntestedAssumptions))
	}
}
