package core

import (
	"context"
	"path/filepath"
	"testing"

	"zdll/internal/eventbus"
)

func TestIntegrateFalsificationResult_Disproven(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	hid, _ := bm.AddHypothesis("SQL injection in login", 0.7, nil)
	tid, _ := bm.AddTask(hid, "falsification test", "devils-advocate")
	_ = bm.MarkTaskFalsification(tid)

	result := `REACHABLE: no
EXPLOITABLE: no
MITIGATED: yes
FINDING: No anomaly detected
SEVERITY: none
CONFIDENCE: 0.8
EVIDENCE: The query uses parameterized statements.
`
	_ = bm.UpdateTask(tid, TaskDone, &result, nil)

	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, dir, dir, dir, bus)
	engine.integrateResults(context.Background(), bm)

	h := bm.Snapshot().Hypotheses[hid]
	if h.FalsificationAttempts != 1 {
		t.Errorf("expected 1 falsification attempt, got %d", h.FalsificationAttempts)
	}
	if h.Confidence >= 0.7 {
		t.Errorf("expected confidence to drop after disproof, got %.2f", h.Confidence)
	}
	stats := bm.Stats()
	if stats["total_findings"] != 0 {
		t.Errorf("falsification task should not create a finding, got %d", stats["total_findings"])
	}
}

func TestIntegrateFalsificationResult_Survived(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init("./testdata")

	hid, _ := bm.AddHypothesis("SQL injection in login", 0.75, nil)
	tid, _ := bm.AddTask(hid, "falsification test", "devils-advocate")
	_ = bm.MarkTaskFalsification(tid)

	result := `REACHABLE: yes
EXPLOITABLE: yes
MITIGATED: no
FINDING: SQL injection in login handler
SEVERITY: high
CONFIDENCE: 0.9
EVIDENCE: query := "SELECT * FROM users WHERE name='" + name + "'"
`
	_ = bm.UpdateTask(tid, TaskDone, &result, nil)

	engine := NewEngine(&EngineConfig{}, &fakeRunner{}, dir, dir, dir, bus)
	engine.integrateResults(context.Background(), bm)

	h := bm.Snapshot().Hypotheses[hid]
	if h.FalsificationAttempts != 1 {
		t.Errorf("expected 1 falsification attempt, got %d", h.FalsificationAttempts)
	}
	if h.Confidence <= 0.75 {
		t.Errorf("expected confidence to rise after surviving disproof, got %.2f", h.Confidence)
	}
}
