package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"zdll/internal/event"
	"zdll/internal/eventbus"
	"zdll/internal/llm"
)

// DronePool limits concurrent drone executions.
type DronePool struct {
	capacity     int
	sem          chan struct{}
	wg           WaitGroupWithCounter
	bus          eventbus.Bus
	runner       llm.AgentRunner
	rootDir      string
	targetDir    string
	artifactsDir string
	skillFS      SkillFS
	bm           *BlackboardManager
}

// NewDronePool creates a pool with the given capacity.
func NewDronePool(capacity int, runner llm.AgentRunner, targetDir, rootDir, artifactsDir string, bm *BlackboardManager, bus eventbus.Bus) *DronePool {
	if capacity <= 0 {
		capacity = 3
	}
	return &DronePool{
		capacity:     capacity,
		sem:          make(chan struct{}, capacity),
		runner:       runner,
		rootDir:      rootDir,
		targetDir:    targetDir,
		artifactsDir: artifactsDir,
		bm:           bm,
		bus:          bus,
	}
}

// WithSkillFS attaches the SkillFS used to load skill content into drones.
func (p *DronePool) WithSkillFS(skillFS SkillFS) *DronePool {
	p.skillFS = skillFS
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
	log.Printf("[drone-pool] submitting task %s (role=%s, active=%d, cap=%d)", t.ID, t.DroneRole, p.Active(), p.capacity)
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		log.Printf("[drone-pool] submit cancelled for task %s: %v", t.ID, ctx.Err())
		return ctx.Err()
	}

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer func() { <-p.sem }()
		const maxRetries = 2
		var finalErr error

		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				backoff := time.Duration(attempt*2) * time.Second
				log.Printf("[drone-pool] retrying task %s (attempt=%d/%d, backoff=%s)", t.ID, attempt, maxRetries, backoff)
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					finalErr = ctx.Err()
					break
				}
			}

			func() {
				defer func() {
					if rec := recover(); rec != nil {
						finalErr = fmt.Errorf("drone panic: %v", rec)
						log.Printf("[drone-pool] task %s panic on attempt %d: %v", t.ID, attempt, rec)
					}
				}()

				log.Printf("[drone-pool] task %s launched (role=%s, attempt=%d)", t.ID, t.DroneRole, attempt)
				p.publish(event.DroneLaunched, map[string]any{
					"task_id": t.ID,
					"role":    t.DroneRole,
					"attempt": attempt,
				})

				if err := p.bm.UpdateTask(t.ID, TaskRunning, nil, nil); err != nil {
					log.Printf("[drone-pool] task %s UpdateTask running failed: %v", t.ID, err)
				}

				drone := NewDrone(t.ID, t.Description, t.DroneRole, p.targetDir, p.rootDir, p.artifactsDir, p.skillFS)
				result, err := drone.Execute(ctx, p.runner)
				if err == nil {
					log.Printf("[drone-pool] task %s completed (result=%d chars)", t.ID, len(result))
					_ = p.bm.UpdateTask(t.ID, TaskDone, &result, nil)
					p.publish(event.DroneCompleted, map[string]any{
						"task_id": t.ID,
						"role":    t.DroneRole,
					})
					finalErr = nil
					return
				}

				finalErr = err
				if errors.Is(err, context.Canceled) {
					return
				}
			}()

			if finalErr == nil {
				return
			}
			if errors.Is(finalErr, context.Canceled) {
				break
			}
		}

		errStr := finalErr.Error()
		log.Printf("[drone-pool] task %s failed after retries: %v", t.ID, errStr)
		_ = p.bm.UpdateTask(t.ID, TaskFailed, nil, &errStr)
		p.publish(event.DroneFailed, map[string]any{
			"task_id": t.ID,
			"error":   errStr,
		})
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
