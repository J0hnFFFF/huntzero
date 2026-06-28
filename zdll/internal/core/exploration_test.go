package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"zdll/internal/eventbus"
)

func TestSpawnExplorationDrones_TriggersEveryFiveRounds(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "auth"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "upload"), 0o755)

	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init(dir)

	cfg := &EngineConfig{
		Workers:   1,
		MaxRounds: 10,
		MaxTasks:  10,
		MaxTime:   5 * time.Second,
		Scanners:  []Scanner{fakeScanner{}},
	}
	engine := NewEngine(cfg, &fakeRunner{}, dir, dir, dir, bus)
	bm.SetRound(5)

	pool := NewDronePool(1, &fakeRunner{}, dir, dir, "", bm, bus)
	engine.spawnExplorationDrones(context.Background(), bm, pool)
	pool.Wait()

	snap := bm.Snapshot()
	var found bool
	for _, tsk := range snap.Tasks {
		if tsk.ExplorationTarget != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected an exploration task to be scheduled, got tasks: %+v", snap.Tasks)
	}
}
