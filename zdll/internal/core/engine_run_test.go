package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"zdll/internal/llm"
)

func TestEngineRun_SmallProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	workDir := t.TempDir()
	bm := NewBlackboardManager(workDir, nil, nil)
	_ = bm.Init("test")

	cfg := &EngineConfig{
		Workers:     1,
		MaxRounds:   1,
		MaxTasks:    0,
		MaxTime:     time.Minute,
		Stagnation:  1,
		AutoApprove: true,
		Model:       "test",
		APIKey:      "fake",
		BaseURL:     "http://localhost",
		Scanners:    []Scanner{},
	}
	runner := &llm.FakeRunner{Response: `{"hypotheses":[],"tasks":[],"findings":[]}`}

	e := NewEngine(cfg, runner, "./skills", ".", dir, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.Run(ctx, bm); err != nil {
		t.Fatalf("engine run: %v", err)
	}
	if bm.Snapshot().Active {
		t.Error("expected blackboard to be inactive after run")
	}
}
