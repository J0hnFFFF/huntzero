package core

import (
	"os"
	"path/filepath"
	"testing"

	"zdll/internal/eventbus"
)

func TestBlackboardManager_AddHypothesis(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	s := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), s, bus)
	if err := bm.Init("https://github.com/test/repo"); err != nil {
		t.Fatalf("init: %v", err)
	}

	id1, err := bm.AddHypothesis("SQL injection in login handler", 0.85, nil)
	if err != nil {
		t.Fatalf("add hypothesis: %v", err)
	}
	if id1 == "" {
		t.Fatal("expected hypothesis id")
	}

	stats := bm.Stats()
	if stats["total_hypotheses"] != 1 {
		t.Fatalf("expected 1 hypothesis, got %d", stats["total_hypotheses"])
	}

	// Duplicate should merge.
	id2, err := bm.AddHypothesis("SQL injection in login handler", 0.80, nil)
	if err != nil {
		t.Fatalf("add duplicate: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("expected duplicate merge, got %s vs %s", id2, id1)
	}

	stats = bm.Stats()
	if stats["total_hypotheses"] != 1 {
		t.Fatalf("expected still 1 hypothesis, got %d", stats["total_hypotheses"])
	}
}

func TestBlackboardManager_TaskAndFinding(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	s := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), s, bus)
	_ = bm.Init("./testdata")

	hid, _ := bm.AddHypothesis("Buffer overflow in parser", 0.75, nil)
	tid, err := bm.AddTask(hid, "Read parser.c around line 42 and confirm bounds check", "evidence-collector")
	if err != nil {
		t.Fatalf("add task: %v", err)
	}

	r := "FINDING: missing bounds check in parser.c\nSEVERITY: high\nCONFIDENCE: 0.85\nEVIDENCE: if (offset >= len) return; missing\nDETAIL: The parser does not validate offset before accessing bytes."
	if err := bm.UpdateTask(tid, TaskDone, &r, nil); err != nil {
		t.Fatalf("update task: %v", err)
	}

	fid, err := bm.AddFinding(hid, "Missing bounds check", "parser.c lacks bounds check", "high", "if (offset >= len) return; missing")
	if err != nil {
		t.Fatalf("add finding: %v", err)
	}
	if fid == "" {
		t.Fatal("expected finding id")
	}

	stats := bm.Stats()
	if stats["total_findings"] != 1 {
		t.Fatalf("expected 1 finding, got %d", stats["total_findings"])
	}
	if stats["confirmed"] != 1 {
		t.Fatalf("expected 1 confirmed hypothesis, got %d", stats["confirmed"])
	}
}

func TestBlackboardManager_Persistence(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	s := NewJSONStore(dir)
	workDir := filepath.Join(dir, "ws")
	bm := NewBlackboardManager(workDir, s, bus)
	_ = bm.Init("./testdata")
	hid, _ := bm.AddHypothesis("Race condition in auth", 0.6, nil)
	_ = bm.SetRound(3)

	// Re-load with a new manager.
	bm2 := NewBlackboardManager(workDir, s, bus)
	if err := bm2.Init(""); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if bm2.Snapshot().Round != 3 {
		t.Fatalf("expected round 3, got %d", bm2.Snapshot().Round)
	}
	if _, ok := bm2.Snapshot().Hypotheses[hid]; !ok {
		t.Fatalf("expected hypothesis %s after resume", hid)
	}
}

func TestWorkspaceJSON_LoadMissing(t *testing.T) {
	dir := t.TempDir()
	s := NewJSONStore(dir)
	_, err := s.Load(filepath.Join(dir, "missing"))
	if !os.IsNotExist(err) {
		t.Fatalf("expected not exist, got %v", err)
	}
}
