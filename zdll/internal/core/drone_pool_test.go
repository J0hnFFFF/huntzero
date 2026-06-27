package core

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/llm"
)

type countingFakeRunner struct {
	calls int
}

func (f *countingFakeRunner) Run(ctx context.Context, cfg llm.AgentConfig, task string) (<-chan llm.Event, error) {
	f.calls++
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: "text", Content: "fake drone result for " + task[:min(len(task), 40)]}
	ch <- llm.Event{Type: "done", Content: ""}
	close(ch)
	return ch, nil
}

func TestDronePool_Submit_ExecutesAndPublishesEvents(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init(dir)

	hid, _ := bm.AddHypothesis("test hypothesis", 0.8, nil)
	tid, _ := bm.AddTask(hid, "verify something", "evidence-collector")
	task := bm.Snapshot().Tasks[tid]

	runner := &countingFakeRunner{}
	pool := NewDronePool(2, runner, dir, dir, "", bm, bus)

	events := bus.Subscribe()
	defer bus.Unsubscribe(events)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pool.Submit(ctx, task); err != nil {
		t.Fatalf("submit: %v", err)
	}
	pool.Wait()

	if runner.calls != 1 {
		t.Fatalf("expected runner to be called once, got %d", runner.calls)
	}

	// Collect events for a short window to account for async publishing.
	var launched, completed bool
	collectCtx, collectCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer collectCancel()

collect:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break collect
			}
			switch ev.Type {
			case event.DroneLaunched:
				launched = true
			case event.DroneCompleted:
				completed = true
			}
			if launched && completed {
				break collect
			}
		case <-collectCtx.Done():
			break collect
		}
	}

	if !launched {
		t.Fatal("expected DroneLaunched event")
	}
	if !completed {
		t.Fatal("expected DroneCompleted event")
	}
}

type flakyFakeRunner struct {
	failFirst int
	calls     int
}

func (f *flakyFakeRunner) Run(ctx context.Context, cfg llm.AgentConfig, task string) (<-chan llm.Event, error) {
	f.calls++
	if f.failFirst > 0 {
		f.failFirst--
		return nil, errors.New("transient llm failure")
	}
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: "text", Content: "success after retries"}
	ch <- llm.Event{Type: "done", Content: ""}
	close(ch)
	return ch, nil
}

func TestDronePool_Submit_RetriesTransientFailures(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init(dir)

	hid, _ := bm.AddHypothesis("test hypothesis", 0.8, nil)
	tid, _ := bm.AddTask(hid, "verify something", "evidence-collector")
	task := bm.Snapshot().Tasks[tid]

	runner := &flakyFakeRunner{failFirst: 2}
	pool := NewDronePool(2, runner, dir, dir, "", bm, bus)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := pool.Submit(ctx, task); err != nil {
		t.Fatalf("submit: %v", err)
	}
	pool.Wait()

	if runner.calls != 3 {
		t.Fatalf("expected 3 runner calls (2 failures + 1 success), got %d", runner.calls)
	}

	snap := bm.Snapshot()
	if snap.Tasks[tid].Status != TaskDone {
		t.Fatalf("expected task status %v, got %v", TaskDone, snap.Tasks[tid].Status)
	}
}

func TestDronePool_Submit_EventuallyFails(t *testing.T) {
	dir := t.TempDir()
	bus := eventbus.NewLocal()
	store := NewJSONStore(dir)
	bm := NewBlackboardManager(filepath.Join(dir, "ws"), store, bus)
	_ = bm.Init(dir)

	hid, _ := bm.AddHypothesis("test hypothesis", 0.8, nil)
	tid, _ := bm.AddTask(hid, "always fails", "evidence-collector")
	task := bm.Snapshot().Tasks[tid]

	runner := &flakyFakeRunner{failFirst: 10}
	pool := NewDronePool(2, runner, dir, dir, "", bm, bus)

	// Subscribe before submitting so we do not miss the failure event.
	events := bus.Subscribe()
	defer bus.Unsubscribe(events)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := pool.Submit(ctx, task); err != nil {
		t.Fatalf("submit: %v", err)
	}
	pool.Wait()

	if runner.calls != 3 { // initial + 2 retries
		t.Fatalf("expected 3 runner calls, got %d", runner.calls)
	}

	snap := bm.Snapshot()
	if snap.Tasks[tid].Status != TaskFailed {
		t.Fatalf("expected task status %v, got %v", TaskFailed, snap.Tasks[tid].Status)
	}

	// Verify a DroneFailed event was published.

	found := false
	collectCtx, collectCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer collectCancel()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event channel closed before DroneFailed")
			}
			if ev.Type == event.DroneFailed {
				found = true
			}
		case <-collectCtx.Done():
			if !found {
				t.Fatal("expected DroneFailed event")
			}
			return
		}
		if found {
			return
		}
	}
}
