package core

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/llm"
)

// DronePool limits concurrent drone executions and optionally reuses pooled sessions.
type DronePool struct {
	capacity  int
	sem       chan struct{}
	wg        WaitGroupWithCounter
	bus       eventbus.Bus
	runner    llm.AgentRunner
	pool      *DroneSessionPool
	rootDir   string
	targetDir string
	bm        *BlackboardManager
}

// NewDronePool creates a pool with the given capacity.
func NewDronePool(capacity int, runner llm.AgentRunner, targetDir, rootDir string, bm *BlackboardManager, bus eventbus.Bus) *DronePool {
	if capacity <= 0 {
		capacity = 3
	}
	return &DronePool{
		capacity:  capacity,
		sem:       make(chan struct{}, capacity),
		runner:    runner,
		rootDir:   rootDir,
		targetDir: targetDir,
		bm:        bm,
		bus:       bus,
	}
}

// NewDronePoolWithSession creates a pool that prefers reusable sessions.
func NewDronePoolWithSession(capacity int, pool *DroneSessionPool, runner llm.AgentRunner, targetDir, rootDir string, bm *BlackboardManager, bus eventbus.Bus) *DronePool {
	p := NewDronePool(capacity, runner, targetDir, rootDir, bm, bus)
	p.pool = pool
	return p
}

// SetCapacity resizes the pool capacity (applies to future acquires).
func (p *DronePool) SetCapacity(n int) {
	if n <= 0 {
		n = 3
	}
	p.capacity = n
}

// Active returns the number of currently running drones.
func (p *DronePool) Active() int {
	return int(p.wg.Active())
}

// Submit runs a task asynchronously, bounded by capacity.
func (p *DronePool) Submit(ctx context.Context, t *DroneTask) error {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer func() { <-p.sem }()

		p.publish(event.DroneLaunched, map[string]any{
			"task_id": t.ID,
			"role":    t.DroneRole,
		})

		_ = p.bm.UpdateTask(t.ID, TaskRunning, nil, nil)

		drone := NewDrone(t.ID, t.Description, t.DroneRole, p.targetDir, p.rootDir)

		var result string
		var err error
		if p.pool != nil {
			if session, sandboxDir, release, acquireErr := p.pool.Acquire(ctx, t.DroneRole); acquireErr == nil {
				result, err = drone.ExecuteWithSession(ctx, session, sandboxDir)
				release()
			} else {
				// Fallback to on-demand runner when pool is exhausted.
				result, err = drone.Execute(ctx, p.runner)
			}
		} else {
			result, err = drone.Execute(ctx, p.runner)
		}

		if err != nil {
			errStr := err.Error()
			_ = p.bm.UpdateTask(t.ID, TaskFailed, nil, &errStr)
			p.publish(event.DroneFailed, map[string]any{
				"task_id": t.ID,
				"error":   errStr,
			})
		} else {
			_ = p.bm.UpdateTask(t.ID, TaskDone, &result, nil)
			p.publish(event.DroneCompleted, map[string]any{
				"task_id": t.ID,
				"role":    t.DroneRole,
			})
		}
	}()

	return nil
}

// Wait blocks until all submitted tasks finish.
func (p *DronePool) Wait() {
	p.wg.Wait()
}

func (p *DronePool) publish(typ string, data map[string]any) {
	if p.bus == nil {
		return
	}
	p.bus.Publish(event.Event{Type: typ, Data: data, Timestamp: time.Now()})
}

// WaitGroupWithCounter tracks active goroutines.
type WaitGroupWithCounter struct {
	wg     sync.WaitGroup
	active int32
}

func (w *WaitGroupWithCounter) Add(delta int) {
	atomic.AddInt32(&w.active, int32(delta))
	w.wg.Add(delta)
}

func (w *WaitGroupWithCounter) Done() {
	atomic.AddInt32(&w.active, -1)
	w.wg.Done()
}

func (w *WaitGroupWithCounter) Wait() {
	w.wg.Wait()
}

func (w *WaitGroupWithCounter) Active() int32 {
	return atomic.LoadInt32(&w.active)
}
